package server

import (
	"fmt"
	"math/rand"
	"net"
	"runtime"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/sshtest"
)

// The sizes of the flood: every input is bounded (CLAUDE.md "Memory (OOM)").
const (
	floodConnections = 3000
	floodAtOnce      = 50
	floodLimit       = 2 * time.Minute
)

// floodDial opens a connection from one of the loopback addresses 127.0.0.1 to 127.0.0.9: the
// server lets only 8 connections that are not yet authenticated wait from one address, and a flood
// of 50 at once from a single address would be nothing but refusals.
func floodDial(addr string, n int) (net.Conn, error) {
	local := &net.TCPAddr{IP: net.IPv4(127, 0, 0, byte(1+n%9))}
	dialer := net.Dialer{LocalAddr: local, Timeout: 10 * time.Second}
	conn, err := dialer.Dial("tcp", addr)
	if err != nil {
		return nil, err
	}
	if err := conn.SetDeadline(time.Now().Add(20 * time.Second)); err != nil {
		_ = conn.Close() // it is not used
		return nil, err
	}
	return conn, nil
}

// flood makes the connection number n, of one of four kinds, and says what the client saw.
func flood(f *fixture, n int, rnd *rand.Rand, stranger sshtest.Key) {
	conn, err := floodDial(f.addr, n)
	if err != nil {
		return // the system had no room for it: the server has nothing to count
	}
	defer func() { _ = conn.Close() }() // as the flooders do
	switch n % 4 {
	case 0:
		// A port scanner: it connects and goes.
	case 1:
		// Random bytes instead of a handshake, up to 1 KiB.
		junk := make([]byte, 1+rnd.Intn(1024))
		rnd.Read(junk)
		_, _ = conn.Write(junk) // a server that closes first is fine
	case 2:
		// A login name that is nobody's.
		config := sshtest.ClientConfig(fmt.Sprintf("ghost-%04d", n), f.hostKey.Signer().PublicKey(), stranger)
		if c, _, _, err := ssh.NewClientConn(conn, f.addr, config); err == nil {
			_ = c.Close() // cannot happen: the server refuses it
		}
	case 3:
		// A name that is a person's, with a key that is not theirs.
		config := sshtest.ClientConfig("alice", f.hostKey.Signer().PublicKey(), stranger)
		if c, _, _, err := ssh.NewClientConn(conn, f.addr, config); err == nil {
			_ = c.Close() // cannot happen: the server refuses it
		}
	}
}

// TestAFloodOfConnectionsIsBoundedAndTheServerGoesOn throws 3 000 connections of four kinds at the
// server, 50 at a time: nothing may get in; the list counts every one of them and stays within its
// limit; no goroutines and not much memory are left behind; and a good client still connects.
func TestAFloodOfConnectionsIsBoundedAndTheServerGoesOn(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("the test connects from several loopback addresses, which Linux has without setup")
	}
	if testing.Short() {
		t.Skip("the flood takes a few seconds")
	}
	f := newFixture(t)
	_, aliceKey := f.person("alice")
	stranger := sshtest.NewKey(t, sshtest.Ed25519)

	runtime.GC()
	var before runtime.MemStats
	runtime.ReadMemStats(&before)
	goroutinesBefore := runtime.NumGoroutine()

	started := time.Now()
	jobs := make(chan int)
	var workers sync.WaitGroup
	for w := 0; w < floodAtOnce; w++ {
		workers.Add(1)
		go func(seed int64) {
			defer workers.Done()
			rnd := rand.New(rand.NewSource(seed))
			for n := range jobs {
				flood(f, n, rnd, stranger)
			}
		}(int64(w))
	}
	for n := 0; n < floodConnections; n++ {
		jobs <- n
	}
	close(jobs)
	workers.Wait()
	if elapsed := time.Since(started); elapsed > floodLimit {
		t.Fatalf("the flood took %v, more than %v", elapsed, floodLimit)
	}

	// The server counts what it saw: every connection is one attempt, and the list has not grown
	// past its limit. Connections that the system could not make are not counted by anybody, so
	// the count is the number that arrived: the flood makes them all, in practice.
	deadline := time.Now().Add(floodLimit)
	for f.total() < floodConnections {
		if time.Now().After(deadline) {
			t.Fatalf("the list counted %d attempts after the flood, want %d", f.total(), floodConnections)
		}
		time.Sleep(10 * time.Millisecond)
	}
	entries := f.attempts.Entries()
	if f.total() != floodConnections {
		t.Errorf("the list counted %d attempts, want %d", f.total(), floodConnections)
	}
	if len(entries) > attempts.MaxEntries {
		t.Errorf("the list has %d entries, more than its limit of %d", len(entries), attempts.MaxEntries)
	}
	counts := map[attempts.Outcome]int64{}
	for _, e := range entries {
		counts[e.Outcome] += e.Count
		if e.Outcome == attempts.Accepted {
			t.Errorf("the flood got in: %+v", e)
		}
	}
	t.Logf("%d attempts in %d entries; the outcomes are %v; the flood took %v", f.total(), len(entries), counts, time.Since(started))
	// The flood is of four kinds, so the outcomes are among these; the ones the limits turn away
	// are refused, and what the others come to depends on how many wait at once.
	for outcome, count := range counts {
		switch outcome {
		case attempts.HandshakeFailed, attempts.UnknownName, attempts.WrongKey, attempts.Refused, attempts.NoKey:
		default:
			t.Errorf("%d attempts have the outcome %q, which no kind of the flood can have", count, outcome)
		}
	}
	if counts[attempts.UnknownName] == 0 || counts[attempts.WrongKey] == 0 || counts[attempts.HandshakeFailed] == 0 {
		t.Errorf("the outcomes are %v: every kind of the flood should show", counts)
	}

	// A good client still connects, quickly.
	began := time.Now()
	client, err := f.dial(f.config("alice", aliceKey))
	if err != nil {
		t.Fatalf("a good client was refused after the flood: %v", err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("a good client needed %v to connect after the flood, want under 2 s", took)
	}
	if err := client.Close(); err != nil {
		t.Logf("closing the good client: %v", err)
	}

	// Nothing is left behind: the goroutines of the connections are gone, and the heap has not
	// grown by more than 64 MiB.
	deadline = time.Now().Add(30 * time.Second)
	for runtime.NumGoroutine() > goroutinesBefore+20 {
		if time.Now().After(deadline) {
			buf := make([]byte, 1<<16)
			t.Fatalf("%d goroutines before the flood and %d after it:\n%s", goroutinesBefore, runtime.NumGoroutine(), buf[:runtime.Stack(buf, true)])
		}
		time.Sleep(50 * time.Millisecond)
	}
	runtime.GC()
	var after runtime.MemStats
	runtime.ReadMemStats(&after)
	if grown := int64(after.HeapAlloc) - int64(before.HeapAlloc); grown > 64<<20 {
		t.Errorf("the heap grew by %d MiB, want less than 64", grown>>20)
	}
}
