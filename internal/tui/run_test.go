package tui

import (
	"context"
	"io"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	appevents "github.com/rm4n0s/cave/internal/app"
	"github.com/rm4n0s/cave/internal/app/apptest"
	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/errors"
)

// syncBuffer is the terminal's output, written by the program and read by the test.
type syncBuffer struct {
	mu  sync.Mutex
	buf strings.Builder
}

func (b *syncBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *syncBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

// runOptions are the options of a program in a test: a terminal that is a pipe and a buffer, a
// size, and the environment of a terminal that has colors.
func runOptions(in io.Reader, out io.Writer, f *apptest.Fake) Options {
	return Options{In: in, Out: out, Env: []string{"TERM=xterm-256color"}, Clock: f.Clock, Width: 100, Height: 30}
}

// runAsync runs the program and returns the channel its result comes on.
func runAsync(ctx context.Context, a App, opts Options) <-chan error {
	done := make(chan error, 1)
	go func() { done <- Run(ctx, a, opts) }()
	return done
}

func waitDone(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(10 * time.Second):
		t.Fatal("Run did not return")
		return nil
	}
}

// waitOutput waits until the output holds text.
func waitOutput(t *testing.T, out *syncBuffer, text string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !strings.Contains(out.String(), text) {
		if time.Now().After(deadline) {
			t.Fatalf("the output never held %q:\n%q", text, out.String())
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunWritesOnlyToItsWriter(t *testing.T) {
	// The standard streams are replaced by pipes for the time of the test, and what arrives on
	// them is read: nothing may.
	stdout, stderr := os.Stdout, os.Stderr
	outR, outW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	errR, errW, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	os.Stdout, os.Stderr = outW, errW
	t.Cleanup(func() { os.Stdout, os.Stderr = stdout, stderr })

	f := apptest.Sample(t)
	var out syncBuffer
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	in, inW := io.Pipe()
	done := runAsync(runCtx, f, runOptions(in, &out, f))
	waitOutput(t, &out, "Overview") // the first frame is drawn
	if _, err := inW.Write([]byte("qy")); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
	os.Stdout, os.Stderr = stdout, stderr
	_ = outW.Close()
	_ = errW.Close()
	for name, r := range map[string]*os.File{"standard output": outR, "standard error": errR} {
		data, err := io.ReadAll(r)
		if err != nil {
			t.Fatal(err)
		}
		if len(data) != 0 {
			t.Errorf("the program wrote %q to its %s", data, name)
		}
	}
	if !strings.Contains(out.String(), "running on port 2222") {
		t.Errorf("the writer got %q", out.String())
	}
}

func TestRunEndsWhenTheContextIsCancelled(t *testing.T) {
	f := apptest.Sample(t)
	var out syncBuffer
	in, inW := io.Pipe()
	defer func() { _ = inW.Close() }() // a pipe of the test: nothing more to do when it fails
	runCtx, cancel := context.WithCancel(ctx)
	done := runAsync(runCtx, f, runOptions(in, &out, f))
	waitOutput(t, &out, "Overview")
	cancel()
	if err := waitDone(t, done); err != nil {
		t.Errorf("a cancelled context is not an error, got %v", err)
	}
	if f.Subscribers() != 0 {
		t.Errorf("%d subscribers are left after Run", f.Subscribers())
	}
}

// countingApp counts how many times the snapshot of the connections is read: the program reads
// all the snapshots together when it is told that they changed. The renderer writes only what
// changed on the screen, so the output is no way to see a read.
type countingApp struct {
	*apptest.Fake
	reads atomic.Int32
}

func (c *countingApp) Connections() []server.Connection {
	c.reads.Add(1)
	return c.Fake.Connections()
}

// waitReads waits until the snapshots were read more often than before.
func (c *countingApp) waitReads(t *testing.T, before int32, why string) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for c.reads.Load() <= before {
		if time.Now().After(deadline) {
			t.Fatalf("the snapshots were not read again: %s", why)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestRunSendsTheApplicationsEvents(t *testing.T) {
	f := apptest.Sample(t)
	app := &countingApp{Fake: f}
	var out syncBuffer
	in, inW := io.Pipe()
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	done := runAsync(runCtx, app, runOptions(in, &out, f))
	waitOutput(t, &out, "1 connected")
	// Subscribe is called by Run itself: wait for it, then change what the application shows.
	deadline := time.Now().Add(10 * time.Second)
	for f.Subscribers() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("Run did not subscribe")
		}
		time.Sleep(5 * time.Millisecond)
	}
	before := app.reads.Load()
	f.Emit(appevents.Accepted{Person: "alice"}) // not a Changed: nothing is read
	time.Sleep(100 * time.Millisecond)
	if app.reads.Load() != before {
		t.Error("an Accepted made the program read the snapshots")
	}
	f.Emit(appevents.Changed{Topics: appevents.TopicConnections | appevents.TopicServer})
	app.waitReads(t, before, "after a Changed")
	if _, err := inW.Write([]byte("qy")); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

func TestRunTicksWithTheClock(t *testing.T) {
	f := apptest.Sample(t)
	app := &countingApp{Fake: f}
	var out syncBuffer
	in, inW := io.Pipe()
	runCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	done := runAsync(runCtx, app, runOptions(in, &out, f))
	waitOutput(t, &out, "1 connected")
	before := app.reads.Load()
	// The ticker is made by Run: advance the clock until it has fired.
	deadline := time.Now().Add(10 * time.Second)
	for app.reads.Load() <= before {
		if time.Now().After(deadline) {
			t.Fatal("a tick of the clock did not make the program read the snapshots again")
		}
		f.Clock.Advance(tickEvery)
		time.Sleep(10 * time.Millisecond)
	}
	if _, err := inW.Write([]byte("qy")); err != nil {
		t.Fatal(err)
	}
	if err := waitDone(t, done); err != nil {
		t.Fatalf("Run: %v", err)
	}
}

// failingReader is a terminal whose input fails.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) {
	return 0, errors.New("InputBroken", "the input is broken")
}

func TestRunFailsWithItsOwnTag(t *testing.T) {
	f := apptest.Sample(t)
	var out syncBuffer
	runCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	err := waitDone(t, runAsync(runCtx, f, runOptions(failingReader{}, &out, f)))
	if err == nil {
		t.Fatal("Run returned no error for an input that fails")
	}
	appErr, ok := errors.FromError(err)
	if !ok || appErr.Tag != TagRunFailed {
		t.Errorf("Run returned %T %v, want the Tag %s", err, err, TagRunFailed)
	}
}
