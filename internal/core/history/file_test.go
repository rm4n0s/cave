package history

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
)

// newAppender returns an appender of a file in a folder of the test, with a small limit.
func newAppender(t *testing.T, max int64) *appender {
	t.Helper()
	a := &appender{path: filepath.Join(t.TempDir(), FileName), max: &max, header: func() *line { return &line{V: lineVersion, Kind: kindState} }}
	t.Cleanup(func() {
		// A test may have broken the file on purpose; only a leak matters here.
		_ = a.close()
	})
	return a
}

// sample is a line to append.
var sample = &line{V: lineVersion, Kind: kindConnected, ID: "c_00000000000000a1"}

func TestFileModes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows keeps files private by the profile's ACLs; mode bits mean nothing there (SPEC §13.1)")
	}
	f := newFixture(t)
	f.maxBytes = 300
	c := f.conn("alice")
	f.Connected(c)
	for range 5 {
		f.AddTransfer(f.transfer(c, "/home/u/file", 1, 0, ResultDone))
	}
	for _, p := range []string{f.path, f.path + ".1"} {
		info, err := os.Stat(p)
		if err != nil {
			t.Fatal(err)
		}
		if mode := info.Mode().Perm(); mode != 0o600 {
			t.Errorf("%s has mode %o, want 600", filepath.Base(p), mode)
		}
	}
}

func TestOpenErrors(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := Open(cancelled, Options{Dirs: storagetest.Isolate(t), Clock: clock.NewFake(start), Logger: slog.New(slog.DiscardHandler)})
		errtest.Route(t, err, "Open.HistoryOpenCancelled")
	})
	t.Run("the file cannot be read", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		// A folder where the file goes: it opens, and reading it fails.
		if err := os.Mkdir(filepath.Join(dirs.State, FileName), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := Open(ctx, Options{Dirs: dirs, Clock: clock.NewFake(start), Logger: slog.New(slog.DiscardHandler)})
		meta := errtest.Route(t, err, "Open->reading.file.HistoryReadFailed")
		if meta["path"] != filepath.Join(dirs.State, FileName) {
			t.Errorf("metadata %v", meta)
		}
	})
}

func TestAppendErrors(t *testing.T) {
	t.Run("the file cannot be opened", func(t *testing.T) {
		a := newAppender(t, 1000)
		if err := os.Mkdir(a.path, 0o700); err != nil {
			t.Fatal(err)
		}
		errtest.Route(t, a.append(sample), "appender.append->appender.open.HistoryFileOpenFailed")
		if err := os.Remove(a.path); err != nil {
			t.Fatal(err)
		}
		if err := a.append(sample); err != nil {
			t.Errorf("once the way is clear: %v", err)
		}
	})
	t.Run("the file cannot be written", func(t *testing.T) {
		a := newAppender(t, 1000)
		if err := a.append(sample); err != nil {
			t.Fatal(err)
		}
		if err := a.file.Close(); err != nil { // behind the appender's back
			t.Fatal(err)
		}
		errtest.Route(t, a.append(sample), "appender.append->appender.raw.HistoryWriteFailed")
		if err := a.append(sample); err != nil {
			t.Errorf("the next line should open the file again: %v", err)
		}
		if got := kinds(t, a.path); len(got) != 3 || got[0] != kindState {
			t.Errorf("the file has %v", got)
		}
	})
	t.Run("the full file cannot be put aside", func(t *testing.T) {
		a := newAppender(t, 50)
		// A folder with something in it where the old file goes: the rename fails.
		if err := os.MkdirAll(filepath.Join(a.path+".1", "in-the-way"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := a.append(sample); err != nil {
			t.Fatal(err)
		}
		errtest.Route(t, a.append(sample), "appender.append.HistoryRotateFailed")
		if got := kinds(t, a.path); len(got) != 3 {
			t.Errorf("the file has %v; the line goes into the full file all the same", got)
		}
	})
	t.Run("a line that cannot be encoded", func(t *testing.T) {
		a := newAppender(t, 1000)
		far := time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)
		errtest.Route(t, a.append(&line{V: lineVersion, Kind: kindConnected, ConnectedAt: &far}), "appender.append->encode.HistoryEncodeFailed")
	})
	t.Run("a write that fails is logged and the record kept", func(t *testing.T) {
		f := newFixture(t)
		if err := os.Mkdir(f.path, 0o700); err != nil {
			t.Fatal(err)
		}
		c := f.conn("alice")
		f.Connected(c)
		if len(f.Connections()) != 1 {
			t.Error("the record was not kept")
		}
		var record struct {
			Err errors.ErrorJson `json:"err"`
		}
		if err := json.Unmarshal(bytes.SplitN(f.log.Bytes(), []byte("\n"), 2)[0], &record); err != nil {
			t.Fatalf("the log has %q", f.log.String())
		}
		if !record.Err.HasRoute("appender.open.HistoryFileOpenFailed") {
			t.Errorf("logged route %s", record.Err.Route())
		}
	})
}

func TestClearErrors(t *testing.T) {
	t.Run("cancelled", func(t *testing.T) {
		f := newFixture(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		errtest.Route(t, f.Clear(cancelled), "Store.Clear.HistoryClearCancelled")
	})
	t.Run("a file cannot be removed", func(t *testing.T) {
		f := newFixture(t)
		c := f.conn("alice")
		f.Connected(c)
		if err := os.MkdirAll(filepath.Join(f.path+".1", "in-the-way"), 0o700); err != nil {
			t.Fatal(err)
		}
		errtest.Route(t, f.Clear(ctx), "Store.Clear->Store.clearLocked.HistoryClearFailed")
		if len(f.Connections()) != 1 {
			t.Error("a clear that failed changed the history")
		}
	})
	t.Run("a policy clear that fails is logged", func(t *testing.T) {
		f := newFixture(t)
		if err := os.MkdirAll(filepath.Join(f.path+".1", "in-the-way"), 0o700); err != nil {
			t.Fatal(err)
		}
		if f.ApplyPolicy("onStart", true) {
			t.Error("reported a clear that failed")
		}
		var record struct {
			Err errors.ErrorJson `json:"err"`
		}
		if err := json.Unmarshal(bytes.SplitN(f.log.Bytes(), []byte("\n"), 2)[0], &record); err != nil || !record.Err.HasRoute("clearLocked.HistoryClearFailed") {
			t.Errorf("the log has %q", f.log.String())
		}
	})
}

func TestCloseError(t *testing.T) {
	f := newFixture(t)
	f.Connected(f.conn("alice"))
	if err := f.out.file.Close(); err != nil { // behind the store's back
		t.Fatal(err)
	}
	errtest.Route(t, f.Close(), "Store.Close->appender.close.HistoryCloseFailed")
}
