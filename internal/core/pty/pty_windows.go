package pty

import (
	"os"

	"github.com/rm4n0s/errors"
)

// Pty is a pseudo terminal.
//
// TODO(step 7.2): ConPTY (docs/SPEC.md §13.3).
type Pty struct{}

// Open makes a pseudo terminal of the size with the modes applied.
func Open(Size, []Mode) (*Pty, error) {
	return nil, errors.New(TagUnsupported, "pseudo terminals are not built for Windows yet")
}

// Name returns the terminal's name.
func (p *Pty) Name() string { return "" }

// Master returns the side that Cave reads the terminal's output from and writes its input to.
func (p *Pty) Master() *os.File { return nil }

// Slave returns the side that becomes the process's terminal.
func (p *Pty) Slave() *os.File { return nil }

// CloseSlave closes Cave's own handle of the slave side.
func (p *Pty) CloseSlave() {}

// Resize changes the terminal's size.
func (p *Pty) Resize(Size) error { return nil }

// Close closes the terminal.
func (p *Pty) Close() error { return nil }
