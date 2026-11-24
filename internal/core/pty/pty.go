// Package pty makes pseudo terminals for sessions, without cgo: devpts on Linux, and ConPTY on
// Windows from step 7.2. It also decodes the terminal modes a client sends with its pty-req
// (RFC 4254 §8) and applies those the platform has (docs/SPEC.md §9.6).
package pty

import (
	"encoding/binary"

	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors.
const (
	TagSizeInvalid      = "PtySizeInvalid"
	TagModesInvalid     = "PtyModesInvalid"
	TagOpenFailed       = "PtyOpenFailed"
	TagSetupFailed      = "PtySetupFailed"
	TagSlaveOpenFailed  = "PtySlaveOpenFailed"
	TagResizeFailed     = "PtyResizeFailed"
	TagModesApplyFailed = "PtyModesApplyFailed"
	TagInputPipeFailed  = "PtyInputPipeFailed"
	TagOutputPipeFailed = "PtyOutputPipeFailed"
	TagConsoleFailed    = "PtyConsoleFailed"
	TagClosed           = "PtyClosed"
)

// MaxCells is the largest number of columns or rows a terminal may have.
const MaxCells = 4096

// The opcodes of the encoded terminal modes that are not modes themselves.
const (
	// opEnd ends the list.
	opEnd = 0
	// opFirstUndefined is the first of the opcodes (160 to 255) that are not defined: the list
	// ends there, as RFC 4254 §8 says.
	opFirstUndefined = 160
)

// Size is the size of a terminal window, as a client says it. A dimension that is 0 was not
// said, and is ignored (RFC 4254 §6.2): OpenSSH sends 0 by 0 when it asks for a terminal without
// having one itself ("ssh -tt" in a script).
type Size struct {
	// Cols and Rows are the size in character cells, at most MaxCells; 0 when the client does
	// not say.
	Cols, Rows uint32
	// Width and Height are the size in pixels; 0 when the client does not say.
	Width, Height uint32
}

// Check refuses a size with more than MaxCells columns or rows.
func (s Size) Check() error {
	if s.Cols > MaxCells || s.Rows > MaxCells {
		return errors.New(TagSizeInvalid, "a terminal has at most 4096 columns and rows", "cols", s.Cols, "rows", s.Rows)
	}
	return nil
}

// Mode is one terminal mode of a pty-req: an opcode of RFC 4254 §8 and its value.
type Mode struct {
	Opcode uint8
	Value  uint32
}

// ParseModes decodes the terminal modes of a pty-req: opcodes of one byte, each followed by a
// 32-bit value, until the opcode 0 or an undefined opcode (160 and above) or the end. A list
// that stops in the middle of a value is malformed.
func ParseModes(encoded []byte) ([]Mode, error) {
	var modes []Mode
	for len(encoded) > 0 {
		opcode := encoded[0]
		if opcode == opEnd || opcode >= opFirstUndefined {
			break
		}
		if len(encoded) < 5 {
			return nil, errors.New(TagModesInvalid, "the terminal modes end in the middle of a value", "opcode", int(opcode))
		}
		modes = append(modes, Mode{Opcode: opcode, Value: binary.BigEndian.Uint32(encoded[1:5])})
		encoded = encoded[5:]
	}
	return modes, nil
}
