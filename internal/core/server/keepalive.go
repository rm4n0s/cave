package server

import (
	"net"
	"sync"
	"sync/atomic"
	"time"

	"github.com/rm4n0s/cave/internal/core/clock"
	"github.com/rm4n0s/cave/internal/core/history"
	"github.com/rm4n0s/cave/internal/core/settings"
)

// KeepAliveMax is how many intervals in a row a client may stay silent, after it was asked
// whether it is there, before its connection is closed (docs/SPEC.md §9.9).
const KeepAliveMax = 3

// countingConn is a connection that counts the bytes that arrive on it and that are sent on it.
type countingConn struct {
	net.Conn
	arrived, sent *atomic.Int64
}

// Read reads from the connection and counts what arrived.
func (c countingConn) Read(p []byte) (int, error) {
	n, err := c.Conn.Read(p)
	c.arrived.Add(int64(n))
	return n, err
}

// Write writes to the connection and counts what was sent.
func (c countingConn) Write(p []byte) (int, error) {
	n, err := c.Conn.Write(p)
	c.sent.Add(int64(n))
	return n, err
}

// keepAlive asks a client from which nothing arrives whether it is still there, and closes the
// connection of one that does not answer. Every server.keepAliveSeconds it looks at the bytes
// that arrived: when there are new ones, the client is there; when there are none, it asks, with
// keepalive@openssh.com wanting a reply, and when KeepAliveMax looks in a row found nothing, the
// next one closes the connection. Any byte counts as an answer, so a client that does not know
// the request and refuses it is as alive as one that grants it. A question that is not answered
// stays asked: the protocol answers global requests in their order, so a second one would only
// wait behind the first.
type keepAlive struct {
	conn *Conn
	// arrived counts the bytes that arrived on the connection.
	arrived *atomic.Int64
	// unsubscribe ends the watch of the setting.
	unsubscribe func()

	mu sync.Mutex
	// timer is the next look; nil when the keepalive is off or stopped.
	timer clock.Timer
	// last is what arrived had at the last look.
	last int64
	// silent counts the looks in a row that found nothing new.
	silent int
	// asked says that a question is waiting for its answer.
	asked bool
	// sent counts every question sent, for tests.
	sent    int
	stopped bool
}

// startKeepAlive starts the keepalive of a connection. A change of the setting applies at once:
// the next look is one new interval from the change.
func (c *Conn) startKeepAlive(arrived *atomic.Int64) *keepAlive {
	k := &keepAlive{conn: c, arrived: arrived, last: arrived.Load()}
	k.mu.Lock()
	k.arm()
	k.mu.Unlock()
	k.unsubscribe = c.server.opts.Settings.Subscribe(func(key string) {
		if key != string(settings.ServerKeepAliveSeconds) {
			return
		}
		k.mu.Lock()
		defer k.mu.Unlock()
		k.last, k.silent = k.arrived.Load(), 0
		k.arm()
	})
	return k
}

// arm sets the next look, one interval from now, in place of the one that was set; with the
// setting at 0 there is none. The caller holds the lock.
func (k *keepAlive) arm() {
	if k.timer != nil {
		k.timer.Stop()
		k.timer = nil
	}
	if k.stopped {
		return
	}
	seconds := k.conn.server.opts.Settings.Snapshot().Int(settings.ServerKeepAliveSeconds)
	if seconds > 0 {
		k.timer = k.conn.server.opts.Clock.AfterFunc(time.Duration(seconds)*time.Second, k.look)
	}
}

// look is what happens every interval.
func (k *keepAlive) look() {
	k.mu.Lock()
	defer k.mu.Unlock()
	if k.stopped {
		return
	}
	switch arrived := k.arrived.Load(); {
	case arrived != k.last:
		k.last, k.silent = arrived, 0
	case k.silent >= KeepAliveMax:
		k.stopped = true
		k.conn.setEnd(history.ReasonKeepAlive)
		k.conn.server.opts.Logger.Info("connection timed out", "connection", k.conn.info.ID, "person", k.conn.info.PersonName, "silentIntervals", k.silent)
		_ = k.conn.ssh.Close() // the connection's loops see the close
		return
	default:
		k.silent++
		k.ask()
	}
	k.arm()
}

// ask sends the question, unless one is waiting for its answer already. The answer, when it
// comes, is bytes that arrived: nothing else is done with it. The request waits for it on a
// goroutine of its own, which the connection's end frees. The caller holds the lock.
func (k *keepAlive) ask() {
	if k.asked {
		return
	}
	k.asked = true
	k.sent++
	k.conn.handlers.Add(1)
	go func() {
		defer k.conn.handlers.Done()
		_, _, _ = k.conn.ssh.SendRequest(requestKeepAlive, true, nil)
		k.mu.Lock()
		k.asked = false
		k.mu.Unlock()
	}()
}

// stop ends the keepalive: nothing is sent or closed by it afterwards.
func (k *keepAlive) stop() {
	k.unsubscribe()
	k.mu.Lock()
	defer k.mu.Unlock()
	k.stopped = true
	k.arm()
}
