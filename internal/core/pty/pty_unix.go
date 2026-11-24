//go:build linux || darwin

package pty

import (
	"os"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// Pty is a pseudo terminal: the master side, which Cave reads and writes, and the slave side,
// which is the terminal of the session's process.
type Pty struct {
	master *os.File
	name   string

	mu    sync.Mutex
	slave *os.File
	// held is the slave side after CloseSlave on a system that loses the output still in the
	// terminal when its last handle closes (macOS); Release closes it.
	held *os.File
}

// Open makes a pseudo terminal of the size with the modes applied.
func Open(size Size, modes []Mode) (*Pty, error) {
	return open(ptmxPath, ptsDir, size, modes)
}

// open is Open with given devices, so that tests can make each step fail.
func open(ptmx, pts string, size Size, modes []Mode) (*Pty, error) {
	if err := size.Check(); err != nil {
		return nil, err
	}
	master, err := os.OpenFile(ptmx, os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		return nil, errors.NewErr(TagOpenFailed, err).SetMetadata("path", ptmx)
	}
	name, err := unlock(master, pts)
	if err != nil {
		_ = master.Close() // nothing was done with it
		return nil, errors.NewErr(TagSetupFailed, err).SetMetadata("path", ptmx)
	}
	slave, err := os.OpenFile(name, os.O_RDWR|unix.O_NOCTTY|unix.O_CLOEXEC, 0)
	if err != nil {
		_ = master.Close() // as above
		return nil, errors.NewErr(TagSlaveOpenFailed, err).SetMetadata("path", name)
	}
	p := &Pty{master: master, slave: slave, name: name}
	if err := applyModes(slave, modes); err != nil {
		_ = p.Close() // the modes' error is the one that matters
		return nil, err
	}
	if err := p.Resize(size); err != nil {
		_ = p.Close() // as above
		return nil, err
	}
	return p, nil
}

// control runs fn with the file's descriptor. It is used instead of File.Fd, which would take the
// file out of the runtime's poller: a read of the master could then no longer be ended by
// closing it, and a descriptor that is closed meanwhile could be one of another file.
func control(f *os.File, fn func(fd int) error) error {
	conn, err := f.SyscallConn()
	if err != nil {
		return err
	}
	var result error
	if err := conn.Control(func(fd uintptr) { result = fn(int(fd)) }); err != nil {
		return err
	}
	return result
}

// Name returns the path of the slave side, such as "/dev/pts/3" (Linux) or "/dev/ttys003"
// (macOS): the session's SSH_TTY.
func (p *Pty) Name() string {
	return p.name
}

// Master returns the side that Cave reads the terminal's output from and writes its input to.
func (p *Pty) Master() *os.File {
	return p.master
}

// Output returns what Cave reads the terminal's output from: the master side.
func (p *Pty) Output() *os.File {
	return p.master
}

// Input returns what Cave writes the terminal's input to: the master side.
func (p *Pty) Input() *os.File {
	return p.master
}

// Slave returns the side that becomes the process's terminal; nil after CloseSlave.
func (p *Pty) Slave() *os.File {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.slave
}

// CloseSlave closes Cave's own handle of the slave side, once the process has its own: the
// master then sees the end when the last process closes the terminal. On macOS the handle is
// kept until Release, because the system drops what the master did not read yet when the last
// handle of the slave closes.
func (p *Pty) CloseSlave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.slave == nil {
		return
	}
	if keepSlave {
		p.held = p.slave
	} else {
		_ = p.slave.Close() // nothing was written through this handle
	}
	p.slave = nil
}

// Release is called when the process of the session has ended. On macOS it waits, at most wait,
// until the output still in the terminal has been read from the master, and then closes the
// handle of the slave side that CloseSlave kept, so that the master sees the end when no other
// process has the terminal. Elsewhere it does nothing.
func (p *Pty) Release(wait time.Duration) {
	p.mu.Lock()
	held := p.held
	p.held = nil
	p.mu.Unlock()
	if held == nil {
		return
	}
	deadline := time.Now().Add(wait)
	for time.Now().Before(deadline) {
		queued := 0
		err := control(held, func(fd int) error {
			var err error
			queued, err = unix.IoctlGetInt(fd, unix.TIOCOUTQ)
			return err
		})
		if err != nil || queued == 0 {
			break
		}
		time.Sleep(releasePoll)
	}
	_ = held.Close() // nothing was written through this handle
}

// releasePoll is how often Release looks at the output that is still in the terminal.
const releasePoll = 5 * time.Millisecond

// Resize changes the terminal's size, which tells the process in the foreground (SIGWINCH).
// Columns or rows that are 0 were not said and stay as they are: unknown (0) in a new terminal.
func (p *Pty) Resize(size Size) error {
	if err := size.Check(); err != nil {
		return err
	}
	err := control(p.master, func(fd int) error {
		ws, err := unix.IoctlGetWinsize(fd, unix.TIOCGWINSZ)
		if err != nil {
			return err
		}
		if size.Cols != 0 {
			ws.Col = uint16(size.Cols)
		}
		if size.Rows != 0 {
			ws.Row = uint16(size.Rows)
		}
		ws.Xpixel, ws.Ypixel = clamp16(size.Width), clamp16(size.Height)
		return unix.IoctlSetWinsize(fd, unix.TIOCSWINSZ, ws)
	})
	if err != nil {
		return errors.NewErr(TagResizeFailed, err).SetMetadata("pty", p.name)
	}
	return nil
}

// clamp16 fits a pixel size into the 16 bits the system keeps.
func clamp16(v uint32) uint16 {
	if v > 0xffff {
		return 0xffff
	}
	return uint16(v)
}

// Close closes the terminal: the processes that have it get SIGHUP from the system. A second
// call does nothing.
func (p *Pty) Close() error {
	p.CloseSlave()
	p.mu.Lock()
	held := p.held
	p.held = nil
	p.mu.Unlock()
	if held != nil {
		_ = held.Close() // as in CloseSlave
	}
	// Closing the master is all there is to it; a master that is closed already is fine.
	_ = p.master.Close()
	return nil
}

// flag is one bit of one of the terminal's flag words.
type flag struct {
	// word is 'i', 'l', 'o' or 'c': the input, local, output or control flags.
	word byte
	bit  tcflag
}

// The opcodes of the character sizes, and the value that turns a control character off.
const (
	opCS7 = 90
	opCS8 = 91
	// charDisabled is the value of a control character that the client has turned off.
	charDisabled = 255
)

// applyModes sets the terminal modes that the system has on the slave side; the opcodes it does not
// have (and the speeds, which a pseudo terminal does not use) are skipped.
func applyModes(slave *os.File, modes []Mode) error {
	if len(modes) == 0 {
		return nil
	}
	err := control(slave, func(fd int) error {
		termios, err := unix.IoctlGetTermios(fd, termiosGet)
		if err != nil {
			return err
		}
		for _, mode := range modes {
			setMode(termios, mode)
		}
		return unix.IoctlSetTermios(fd, termiosSet, termios)
	})
	if err != nil {
		return errors.NewErr(TagModesApplyFailed, err)
	}
	return nil
}

// setMode changes one mode in the terminal's settings.
func setMode(termios *unix.Termios, mode Mode) {
	if index, ok := controlChars[mode.Opcode]; ok {
		value := uint8(mode.Value)
		if mode.Value == charDisabled {
			value = posixVDisable
		}
		termios.Cc[index] = value
		return
	}
	if f, ok := flags[mode.Opcode]; ok {
		word := map[byte]*tcflag{'i': &termios.Iflag, 'l': &termios.Lflag, 'o': &termios.Oflag, 'c': &termios.Cflag}[f.word]
		if mode.Value != 0 {
			*word |= f.bit
		} else {
			*word &^= f.bit
		}
		return
	}
	switch mode.Opcode {
	case opCS7:
		if mode.Value != 0 {
			termios.Cflag = termios.Cflag&^unix.CSIZE | unix.CS7
		}
	case opCS8:
		if mode.Value != 0 {
			termios.Cflag = termios.Cflag&^unix.CSIZE | unix.CS8
		}
	}
}
