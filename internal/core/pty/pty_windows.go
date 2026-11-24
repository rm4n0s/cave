package pty

import (
	"os"
	"sync"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/windows"
)

// The size of a pseudo console whose client did not say one (docs/SPEC.md §13.3).
const (
	defaultCols = 80
	defaultRows = 24
)

// Pty is a pseudo console (ConPTY): Cave writes the console's input and reads its output through
// two pipes, and the process on the console is started with it (the session's
// startOnConsole). Terminal modes mean nothing to it and are ignored.
type Pty struct {
	input  *os.File // Cave writes what the client typed here
	output *os.File // Cave reads what the console draws here

	mu         sync.Mutex
	console    windows.Handle // 0 once closed
	cols, rows uint32
}

// Open makes a pseudo console of the size; a dimension that is 0 was not said and is 80 columns
// or 24 rows. The modes are ignored.
func Open(size Size, _ []Mode) (*Pty, error) {
	if err := size.Check(); err != nil {
		return nil, err
	}
	cols, rows := size.Cols, size.Rows
	if cols == 0 {
		cols = defaultCols
	}
	if rows == 0 {
		rows = defaultRows
	}
	// The console reads from inRead and writes to outWrite; Cave keeps the other ends.
	var inRead, inWrite, outRead, outWrite windows.Handle
	if err := windows.CreatePipe(&inRead, &inWrite, nil, 0); err != nil {
		return nil, errors.NewErr(TagInputPipeFailed, err)
	}
	if err := windows.CreatePipe(&outRead, &outWrite, nil, 0); err != nil {
		_ = windows.CloseHandle(inRead) // nothing was done with them
		_ = windows.CloseHandle(inWrite)
		return nil, errors.NewErr(TagOutputPipeFailed, err)
	}
	var console windows.Handle
	err := windows.CreatePseudoConsole(windows.Coord{X: int16(cols), Y: int16(rows)}, inRead, outWrite, 0, &console)
	// The console has its own hold of its two ends, whether it was made or not.
	_ = windows.CloseHandle(inRead)
	_ = windows.CloseHandle(outWrite)
	if err != nil {
		_ = windows.CloseHandle(inWrite)
		_ = windows.CloseHandle(outRead)
		return nil, errors.NewErr(TagConsoleFailed, err)
	}
	return &Pty{
		input: os.NewFile(uintptr(inWrite), "conpty-input"), output: os.NewFile(uintptr(outRead), "conpty-output"),
		console: console, cols: cols, rows: rows,
	}, nil
}

// Name returns "": a pseudo console has no name that a program could open (there is no SSH_TTY on
// Windows).
func (p *Pty) Name() string { return "" }

// Output returns what Cave reads the console's output from. It ends when the console is closed.
func (p *Pty) Output() *os.File { return p.output }

// Input returns what Cave writes the console's input to.
func (p *Pty) Input() *os.File { return p.input }

// Console returns the handle of the pseudo console, for starting a process on it; 0 once it is
// closed.
func (p *Pty) Console() windows.Handle {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.console
}

// Slave returns nil: there is no slave side, the console is given to the process when it starts.
func (p *Pty) Slave() *os.File { return nil }

// CloseSlave does nothing, as there is no slave side.
func (p *Pty) CloseSlave() {}

// Release does nothing on Windows: the pseudo console has no slave side of Cave's to let go.
func (p *Pty) Release(time.Duration) {}

// Resize changes the console's size. Columns or rows that are 0 were not said and stay as they
// are.
func (p *Pty) Resize(size Size) error {
	if err := size.Check(); err != nil {
		return err
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	cols, rows := p.cols, p.rows
	if size.Cols != 0 {
		cols = size.Cols
	}
	if size.Rows != 0 {
		rows = size.Rows
	}
	if p.console == 0 {
		return errors.New(TagClosed, "the pseudo console is closed")
	}
	if err := windows.ResizePseudoConsole(p.console, windows.Coord{X: int16(cols), Y: int16(rows)}); err != nil {
		return errors.NewErr(TagResizeFailed, err)
	}
	p.cols, p.rows = cols, rows
	return nil
}

// CloseConsole closes the pseudo console, which ends the output once what the console still has
// is read, and ends the processes that are on it. The pipes stay open for what is still to read.
// It can be called again.
func (p *Pty) CloseConsole() {
	p.mu.Lock()
	console := p.console
	p.console = 0
	p.mu.Unlock()
	if console != 0 {
		windows.ClosePseudoConsole(console)
	}
}

// Close closes the console and both pipes. A second call does nothing.
func (p *Pty) Close() error {
	p.CloseConsole()
	// A pipe that is closed already is fine.
	_ = p.input.Close()
	_ = p.output.Close()
	return nil
}
