package pty

import (
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/wine"
)

// openOrSkip opens a pseudo console; a Wine that cannot make one skips the test (docs/SPEC.md
// §13.6), a Windows that cannot fails it.
func openOrSkip(t *testing.T, size Size) *Pty {
	t.Helper()
	p, err := Open(size, nil)
	if err != nil {
		if wine.Running() {
			t.Skipf("Wine's ConPTY: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

func TestOpenAndCloseOnWindows(t *testing.T) {
	for _, size := range []Size{{Cols: 80, Rows: 24}, {}, {Cols: 132}, {Rows: 50}, {Cols: 4096, Rows: 4096}} {
		p := openOrSkip(t, size)
		if p.Console() == 0 || p.Input() == nil || p.Output() == nil || p.Name() != "" || p.Slave() != nil {
			t.Errorf("%+v: console %v, input %v, output %v, name %q", size, p.Console(), p.Input(), p.Output(), p.Name())
		}
		if err := p.Close(); err != nil {
			t.Errorf("%+v: Close: %v", size, err)
		}
		if err := p.Close(); err != nil { // twice is fine
			t.Errorf("%+v: second Close: %v", size, err)
		}
		if p.Console() != 0 {
			t.Errorf("%+v: the console is still there after Close", size)
		}
	}
}

func TestResizeOnWindows(t *testing.T) {
	p := openOrSkip(t, Size{Cols: 80, Rows: 24})
	for _, size := range []Size{{Cols: 132, Rows: 43}, {Cols: 100}, {Rows: 30}, {}} {
		if err := p.Resize(size); err != nil {
			t.Errorf("Resize(%+v): %v", size, err)
		}
	}
	if p.cols != 100 || p.rows != 30 {
		t.Errorf("the size is %dx%d, want 100x30: a dimension of 0 keeps the current one", p.cols, p.rows)
	}
	err := p.Resize(Size{Cols: MaxCells + 1})
	errtest.Route(t, err, "Pty.Resize->Size.Check.PtySizeInvalid")
	_ = p.Close()
	errtest.Route(t, p.Resize(Size{Cols: 80}), "Resize.PtyClosed")
}

func TestTheOutputEndsWhenTheConsoleIsClosed(t *testing.T) {
	p := openOrSkip(t, Size{Cols: 80, Rows: 24})
	done := make(chan error, 1)
	go func() {
		_, err := p.Output().Read(make([]byte, 64))
		done <- err
	}()
	p.CloseConsole()
	select {
	case err := <-done:
		if err == nil {
			t.Error("a read of the output of a closed console returned data")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the output did not end when the console was closed")
	}
	p.CloseConsole() // twice is fine
}

func TestOpenWithAnOversizedSizeOnWindows(t *testing.T) {
	_, err := Open(Size{Cols: MaxCells + 1, Rows: 24}, nil)
	errtest.Route(t, err, "Open->Size.Check.PtySizeInvalid")
}
