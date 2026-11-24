package pty

import (
	"os"
	"path/filepath"
	"strconv"
	"sync"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// The devices of the pseudo terminals.
const (
	ptmxPath = "/dev/ptmx"
	ptsDir   = "/dev/pts"
)

// Pty is a pseudo terminal: the master side, which Cave reads and writes, and the slave side,
// which is the terminal of the session's process.
type Pty struct {
	master *os.File
	name   string

	mu    sync.Mutex
	slave *os.File
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
	number, err := unlock(master)
	if err != nil {
		_ = master.Close() // nothing was done with it
		return nil, errors.NewErr(TagSetupFailed, err).SetMetadata("path", ptmx)
	}
	name := filepath.Join(pts, strconv.Itoa(number))
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

// unlock finds the number of the master's slave and unlocks it. It returns the system's error
// as it is.
func unlock(master *os.File) (int, error) {
	number := 0
	err := control(master, func(fd int) error {
		var err error
		if number, err = unix.IoctlGetInt(fd, unix.TIOCGPTN); err != nil {
			return err
		}
		return unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0)
	})
	return number, err
}

// Name returns the path of the slave side, such as "/dev/pts/3": the session's SSH_TTY.
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
// master then sees the end when the last process closes the terminal.
func (p *Pty) CloseSlave() {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.slave != nil {
		_ = p.slave.Close() // nothing was written through this handle
		p.slave = nil
	}
}

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
	// Closing the master is all there is to it; a master that is closed already is fine.
	_ = p.master.Close()
	return nil
}

// The characters of the terminal's line editing, by their opcodes in RFC 4254 §8.
var controlChars = map[uint8]int{
	1: unix.VINTR, 2: unix.VQUIT, 3: unix.VERASE, 4: unix.VKILL, 5: unix.VEOF, 6: unix.VEOL, 7: unix.VEOL2,
	8: unix.VSTART, 9: unix.VSTOP, 10: unix.VSUSP, 12: unix.VREPRINT, 13: unix.VWERASE, 14: unix.VLNEXT, 18: unix.VDISCARD,
}

// flag is one bit of one of the terminal's flag words.
type flag struct {
	// word is 'i', 'l', 'o' or 'c': the input, local, output or control flags.
	word byte
	bit  uint32
}

// The flags of the terminal, by their opcodes in RFC 4254 §8.
var flags = map[uint8]flag{
	30: {'i', unix.IGNPAR}, 31: {'i', unix.PARMRK}, 32: {'i', unix.INPCK}, 33: {'i', unix.ISTRIP}, 34: {'i', unix.INLCR},
	35: {'i', unix.IGNCR}, 36: {'i', unix.ICRNL}, 37: {'i', unix.IUCLC}, 38: {'i', unix.IXON}, 39: {'i', unix.IXANY},
	40: {'i', unix.IXOFF}, 41: {'i', unix.IMAXBEL}, 42: {'i', unix.IUTF8},
	50: {'l', unix.ISIG}, 51: {'l', unix.ICANON}, 52: {'l', unix.XCASE}, 53: {'l', unix.ECHO}, 54: {'l', unix.ECHOE},
	55: {'l', unix.ECHOK}, 56: {'l', unix.ECHONL}, 57: {'l', unix.NOFLSH}, 58: {'l', unix.TOSTOP}, 59: {'l', unix.IEXTEN},
	60: {'l', unix.ECHOCTL}, 61: {'l', unix.ECHOKE}, 62: {'l', unix.PENDIN},
	70: {'o', unix.OPOST}, 71: {'o', unix.OLCUC}, 72: {'o', unix.ONLCR}, 73: {'o', unix.OCRNL}, 74: {'o', unix.ONOCR},
	75: {'o', unix.ONLRET},
	92: {'c', unix.PARENB}, 93: {'c', unix.PARODD},
}

// The opcodes of the character sizes, and the value that turns a control character off.
const (
	opCS7 = 90
	opCS8 = 91
	// charDisabled is the value of a control character that the client has turned off.
	charDisabled = 255
)

// applyModes sets the terminal modes that Linux has on the slave side; the opcodes it does not
// have (and the speeds, which a pseudo terminal does not use) are skipped.
func applyModes(slave *os.File, modes []Mode) error {
	if len(modes) == 0 {
		return nil
	}
	err := control(slave, func(fd int) error {
		termios, err := unix.IoctlGetTermios(fd, unix.TCGETS)
		if err != nil {
			return err
		}
		for _, mode := range modes {
			setMode(termios, mode)
		}
		return unix.IoctlSetTermios(fd, unix.TCSETS, termios)
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
			value = 0 // _POSIX_VDISABLE on Linux
		}
		termios.Cc[index] = value
		return
	}
	if f, ok := flags[mode.Opcode]; ok {
		word := map[byte]*uint32{'i': &termios.Iflag, 'l': &termios.Lflag, 'o': &termios.Oflag, 'c': &termios.Cflag}[f.word]
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
