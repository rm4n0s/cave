package pty

import (
	"bufio"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"golang.org/x/sys/unix"
)

// openPty opens a pseudo terminal that is closed when the test ends.
func openPty(t *testing.T, size Size, modes []Mode) *Pty {
	t.Helper()
	p, err := Open(size, modes)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = p.Close() })
	return p
}

// termios reads the terminal's settings from its slave side.
func termios(t *testing.T, p *Pty) *unix.Termios {
	t.Helper()
	got, err := unix.IoctlGetTermios(int(p.Slave().Fd()), unix.TCGETS)
	if err != nil {
		t.Fatal(err)
	}
	return got
}

// winsize reads the terminal's size from its slave side, as a process on it would.
func winsize(t *testing.T, p *Pty) unix.Winsize {
	t.Helper()
	ws, err := unix.IoctlGetWinsize(int(p.Slave().Fd()), unix.TIOCGWINSZ)
	if err != nil {
		t.Fatal(err)
	}
	return *ws
}

// openFiles counts the files this process has open.
func openFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

func TestOpen(t *testing.T) {
	p := openPty(t, Size{Cols: 132, Rows: 43, Width: 1056, Height: 688}, nil)
	if !regexp.MustCompile(`^/dev/pts/\d+$`).MatchString(p.Name()) {
		t.Errorf("Name() = %q", p.Name())
	}
	// The name is the slave's own.
	slaveInfo, err := p.Slave().Stat()
	if err != nil {
		t.Fatal(err)
	}
	nameInfo, err := os.Stat(p.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(slaveInfo, nameInfo) {
		t.Errorf("%s is not the slave side", p.Name())
	}
	if got := winsize(t, p); got != (unix.Winsize{Row: 43, Col: 132, Xpixel: 1056, Ypixel: 688}) {
		t.Errorf("the size is %+v", got)
	}

	// What is written to one side is read on the other.
	if err := p.Master().SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Master().WriteString("typed\n"); err != nil {
		t.Fatal(err)
	}
	line, err := bufio.NewReader(p.Slave()).ReadString('\n')
	if err != nil || line != "typed\n" {
		t.Errorf("the slave read %q, %v", line, err)
	}
	if _, err := p.Slave().WriteString("printed\n"); err != nil {
		t.Fatal(err)
	}
	// The terminal echoes what was typed, then shows what was printed, with its line ends.
	reader := bufio.NewReader(p.Master())
	var got []string
	for len(got) < 2 {
		line, err := reader.ReadString('\n')
		if err != nil {
			t.Fatalf("reading the master: %v (got %q so far)", err, got)
		}
		got = append(got, line)
	}
	if got[0] != "typed\r\n" || got[1] != "printed\r\n" {
		t.Errorf("the master read %q", got)
	}
}

func TestResize(t *testing.T) {
	p := openPty(t, Size{Cols: 80, Rows: 24}, nil)
	if err := p.Resize(Size{Cols: 200, Rows: 50, Width: 70000, Height: 3}); err != nil {
		t.Fatal(err)
	}
	// A pixel size past what the system keeps is cut to the most it keeps.
	if got := winsize(t, p); got != (unix.Winsize{Row: 50, Col: 200, Xpixel: 65535, Ypixel: 3}) {
		t.Errorf("the size is %+v", got)
	}
	errtest.Route(t, p.Resize(Size{Cols: 0, Rows: 50}), "Pty.Resize->Size.Check.PtySizeInvalid")
	errtest.Route(t, p.Resize(Size{Cols: 80, Rows: 5000}), "Pty.Resize->Size.Check.PtySizeInvalid")
	if got := winsize(t, p); got.Col != 200 || got.Row != 50 {
		t.Errorf("a refused size changed the terminal: %+v", got)
	}
	if err := p.Close(); err != nil {
		t.Fatal(err)
	}
	meta := errtest.Route(t, p.Resize(Size{Cols: 80, Rows: 24}), "Pty.Resize.PtyResizeFailed")
	if meta["pty"] != p.Name() {
		t.Errorf("metadata = %v", meta)
	}
}

func TestModesAreApplied(t *testing.T) {
	plain := termios(t, openPty(t, Size{Cols: 80, Rows: 24}, nil))
	if plain.Lflag&unix.ECHO == 0 || plain.Lflag&unix.ICANON == 0 || plain.Iflag&unix.ICRNL == 0 {
		t.Fatalf("a new terminal does not echo or edit lines: %+v", plain)
	}

	modes := []Mode{
		{53, 0},                    // ECHO off
		{51, 0},                    // ICANON off
		{36, 0},                    // ICRNL off
		{42, 1},                    // IUTF8 on
		{58, 1},                    // TOSTOP on
		{72, 0},                    // ONLCR off
		{3, 8},                     // VERASE is backspace
		{1, 255},                   // VINTR is turned off
		{5, 4},                     // VEOF is ^D
		{128, 38400}, {129, 38400}, // speeds: skipped
		{11, 25}, {17, 20}, {150, 1}, // opcodes Linux does not have: skipped
	}
	got := termios(t, openPty(t, Size{Cols: 80, Rows: 24}, modes))
	checks := []struct {
		name string
		ok   bool
	}{
		{"ECHO off", got.Lflag&unix.ECHO == 0},
		{"ICANON off", got.Lflag&unix.ICANON == 0},
		{"ICRNL off", got.Iflag&unix.ICRNL == 0},
		{"IUTF8 on", got.Iflag&unix.IUTF8 != 0},
		{"TOSTOP on", got.Lflag&unix.TOSTOP != 0},
		{"ONLCR off", got.Oflag&unix.ONLCR == 0},
		{"VERASE is 8", got.Cc[unix.VERASE] == 8},
		{"VINTR is off", got.Cc[unix.VINTR] == 0},
		{"VEOF is 4", got.Cc[unix.VEOF] == 4},
		// What no mode named is as it was.
		{"ISIG as before", got.Lflag&unix.ISIG == plain.Lflag&unix.ISIG},
		{"OPOST as before", got.Oflag&unix.OPOST == plain.Oflag&unix.OPOST},
		{"VQUIT as before", got.Cc[unix.VQUIT] == plain.Cc[unix.VQUIT]},
	}
	for _, c := range checks {
		if !c.ok {
			t.Errorf("%s: not so (%+v)", c.name, got)
		}
	}

}

// TestSetModeOnTheControlFlags checks the modes that a pseudo terminal does not keep (the
// system gives every one of them eight bits and no parity) on the settings themselves.
func TestSetModeOnTheControlFlags(t *testing.T) {
	tests := []struct {
		name  string
		start uint32
		modes []Mode
		want  uint32
	}{
		{"seven bits", unix.CS8 | unix.CREAD, []Mode{{90, 1}}, unix.CS7 | unix.CREAD},
		{"eight bits", unix.CS7 | unix.CREAD, []Mode{{91, 1}}, unix.CS8 | unix.CREAD},
		{"a size with the value 0 is not asked for", unix.CS8, []Mode{{90, 0}, {91, 0}}, unix.CS8},
		{"parity on", unix.CS8, []Mode{{92, 1}, {93, 1}}, unix.CS8 | unix.PARENB | unix.PARODD},
		{"parity off", unix.CS8 | unix.PARENB | unix.PARODD, []Mode{{92, 0}, {93, 0}}, unix.CS8},
		{"an opcode Linux does not have", unix.CS8, []Mode{{11, 1}, {17, 1}, {150, 1}, {128, 9600}}, unix.CS8},
	}
	for _, tt := range tests {
		settings := &unix.Termios{Cflag: tt.start}
		for _, mode := range tt.modes {
			setMode(settings, mode)
		}
		if settings.Cflag != tt.want {
			t.Errorf("%s: the control flags are %#o, want %#o", tt.name, settings.Cflag, tt.want)
		}
		if settings.Iflag != 0 || settings.Oflag != 0 || settings.Lflag != 0 {
			t.Errorf("%s: another flag word changed: %+v", tt.name, settings)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	good := Size{Cols: 80, Rows: 24}
	t.Run("a size that is not acceptable", func(t *testing.T) {
		before := openFiles(t)
		_, err := Open(Size{Cols: 0, Rows: 24}, nil)
		errtest.Route(t, err, "Open->open->Size.Check.PtySizeInvalid")
		if after := openFiles(t); after != before {
			t.Errorf("%d files are open, %d before", after, before)
		}
	})
	t.Run("no ptmx device", func(t *testing.T) {
		missing := filepath.Join(t.TempDir(), "ptmx")
		_, err := open(missing, ptsDir, good, nil)
		meta := errtest.Route(t, err, "open.PtyOpenFailed")
		if meta["path"] != missing {
			t.Errorf("metadata = %v", meta)
		}
	})
	t.Run("a ptmx that is not one", func(t *testing.T) {
		file := filepath.Join(t.TempDir(), "ptmx")
		if err := os.WriteFile(file, nil, 0o600); err != nil {
			t.Fatal(err)
		}
		before := openFiles(t)
		_, err := open(file, ptsDir, good, nil)
		errtest.Route(t, err, "open.PtySetupFailed")
		if after := openFiles(t); after != before {
			t.Errorf("%d files are open, %d before: the master was left open", after, before)
		}
	})
	t.Run("no slave device", func(t *testing.T) {
		before := openFiles(t)
		_, err := open(ptmxPath, filepath.Join(t.TempDir(), "pts"), good, nil)
		meta := errtest.Route(t, err, "open.PtySlaveOpenFailed")
		if path, _ := meta["path"].(string); !strings.Contains(path, "pts") {
			t.Errorf("metadata = %v", meta)
		}
		if after := openFiles(t); after != before {
			t.Errorf("%d files are open, %d before: the master was left open", after, before)
		}
	})
	t.Run("modes on a file that is not a terminal", func(t *testing.T) {
		file, err := os.CreateTemp(t.TempDir(), "not-a-tty")
		if err != nil {
			t.Fatal(err)
		}
		defer func() { _ = file.Close() }()
		errtest.Route(t, applyModes(file, []Mode{{53, 0}}), "applyModes.PtyModesApplyFailed")
		// No modes: nothing to apply, whatever the file is.
		if err := applyModes(file, nil); err != nil {
			t.Errorf("no modes: %v", err)
		}
	})
}

func TestCloseLeavesNothingOpen(t *testing.T) {
	before := openFiles(t)
	for i := 0; i < 20; i++ {
		p, err := Open(Size{Cols: 80, Rows: 24}, []Mode{{53, 0}})
		if err != nil {
			t.Fatal(err)
		}
		if i%2 == 0 {
			p.CloseSlave()
			p.CloseSlave() // twice is fine
			if p.Slave() != nil {
				t.Error("Slave() after CloseSlave is not nil")
			}
		}
		if err := p.Close(); err != nil {
			t.Fatal(err)
		}
		if err := p.Close(); err != nil {
			t.Errorf("a second Close: %v", err)
		}
	}
	if after := openFiles(t); after != before {
		t.Errorf("%d files are open, %d before", after, before)
	}
}

func TestTheMasterSeesTheEndWhenTheSlaveIsClosed(t *testing.T) {
	p := openPty(t, Size{Cols: 80, Rows: 24}, nil)
	if _, err := p.Slave().WriteString("last words\n"); err != nil {
		t.Fatal(err)
	}
	p.CloseSlave()
	if err := p.Master().SetReadDeadline(time.Now().Add(10 * time.Second)); err != nil {
		t.Fatal(err)
	}
	reader := bufio.NewReader(p.Master())
	line, err := reader.ReadString('\n')
	if err != nil || line != "last words\r\n" {
		t.Fatalf("the master read %q, %v", line, err)
	}
	// With no process on the terminal, reading the master fails: that is the end of a session.
	if _, err := reader.ReadByte(); err == nil {
		t.Error("the master still reads after the slave was closed")
	}
}

func TestCloseEndsAReadThatIsWaiting(t *testing.T) {
	// A process that outlives its session keeps the slave side open: only closing the master
	// ends the read of its output. Resize must not take the master out of the runtime's poller.
	p := openPty(t, Size{Cols: 80, Rows: 24}, []Mode{{53, 0}})
	if err := p.Resize(Size{Cols: 100, Rows: 30}); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		_, err := p.Master().Read(make([]byte, 16))
		done <- err
	}()
	time.Sleep(50 * time.Millisecond) // let the read begin; the test holds either way
	if p.Slave() == nil {
		t.Fatal("the slave side is closed")
	}
	// Only the master is closed here: the slave side stays open, as a process would hold it.
	if err := p.Master().Close(); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-done:
		if err == nil {
			t.Error("the read succeeded on a closed master")
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the read is still waiting after the master was closed")
	}
}
