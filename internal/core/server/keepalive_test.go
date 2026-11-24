package server

import (
	"net"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/settings"
	"golang.org/x/crypto/ssh"
)

// quietClient is a connection of a client that the test drives by hand: it sees the questions
// the server asks, and answers them or not.
type quietClient struct {
	conn ssh.Conn
	// questions counts the keepalive requests that arrived.
	questions atomic.Int64
	// answer says whether the client answers them.
	answer atomic.Bool
	// gone is closed when the connection ended.
	gone chan struct{}
}

// quiet connects alice with a client that sends nothing by itself, with the keepalive at the
// given seconds.
func (f *fixture) quiet(seconds int, answer bool) *quietClient {
	f.t.Helper()
	if err := f.settings.Set(ctx, string(settings.ServerKeepAliveSeconds), seconds); err != nil {
		f.t.Fatal(err)
	}
	_, key := f.person("alice")
	raw, err := net.Dial("tcp", f.addr)
	if err != nil {
		f.t.Fatal(err)
	}
	conn, channels, requests, err := ssh.NewClientConn(raw, f.addr, f.config("alice", key))
	if err != nil {
		f.t.Fatal(err)
	}
	f.t.Cleanup(func() { _ = conn.Close() })
	q := &quietClient{conn: conn, gone: make(chan struct{})}
	q.answer.Store(answer)
	go ssh.DiscardRequests(nil)
	go func() {
		for channel := range channels {
			_ = channel.Reject(ssh.Prohibited, "the test opens no channels")
		}
	}()
	go func() {
		for request := range requests {
			if request.Type != requestKeepAlive {
				continue
			}
			if q.answer.Load() && request.WantReply {
				_ = request.Reply(true, nil)
			}
			q.questions.Add(1)
		}
	}()
	go func() {
		_ = conn.Wait()
		close(q.gone)
	}()
	f.waitConnections(1)
	return q
}

// keepAliveOf returns the keepalive of the one connection there is.
func (f *fixture) keepAliveOf() *keepAlive {
	f.t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		f.server.mu.Lock()
		var found *keepAlive
		for _, c := range f.server.connections {
			c.mu.Lock()
			found = c.keepAlive
			c.mu.Unlock()
		}
		f.server.mu.Unlock()
		if found != nil {
			return found
		}
		if time.Now().After(deadline) {
			f.t.Fatal("the connection has no keepalive")
		}
		time.Sleep(time.Millisecond)
	}
}

// sentSoFar returns how many questions the server has sent.
func (k *keepAlive) sentSoFar() int {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.sent
}

// armed says whether the keepalive has a timer on the clock.
func (k *keepAlive) armed() bool {
	k.mu.Lock()
	defer k.mu.Unlock()
	return k.timer != nil
}

// waitQuestions waits until the client has seen n questions.
func (q *quietClient) waitQuestions(t *testing.T, n int64) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for q.questions.Load() < n {
		if time.Now().After(deadline) {
			t.Fatalf("the client saw %d questions, want %d", q.questions.Load(), n)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitArrived waits until the server has counted bytes beyond before: the client's answer got
// there.
func (k *keepAlive) waitArrived(t *testing.T, before int64) {
	t.Helper()
	deadline := time.Now().Add(waitFor)
	for k.arrived.Load() == before {
		if time.Now().After(deadline) {
			t.Fatal("nothing arrived from the client")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestAnIdleClientThatAnswersStays(t *testing.T) {
	f := newFixture(t)
	q := f.quiet(60, true)
	k := f.keepAliveOf()

	// Not before the interval has passed.
	f.clock.Advance(59 * time.Second)
	if got := k.sentSoFar(); got != 0 {
		t.Fatalf("%d questions were sent before the interval passed", got)
	}
	// Hours of silence: a question, its answer, an interval that finds the answer and asks
	// nothing, a silent interval that asks again, and so on. The client stays.
	before := k.arrived.Load()
	f.clock.Advance(time.Second)
	for round := 1; round <= 20; round++ {
		if got := k.sentSoFar(); got != round {
			t.Fatalf("round %d: %d questions were sent", round, got)
		}
		q.waitQuestions(t, int64(round))
		k.waitArrived(t, before)
		f.clock.Advance(60 * time.Second) // finds the answer
		if got := k.sentSoFar(); got != round {
			t.Fatalf("round %d: a question was sent although the client had just answered", round)
		}
		before = k.arrived.Load()
		f.clock.Advance(60 * time.Second) // silence again: asks
	}
	select {
	case <-q.gone:
		t.Fatal("a client that answers was disconnected")
	default:
	}
	if conns := f.server.Connections(); len(conns) != 1 {
		t.Errorf("the registry has %d connections", len(conns))
	}
	if strings.Contains(f.log.String(), "connection timed out") {
		t.Errorf("the log says the connection timed out:\n%s", f.log.String())
	}
}

func TestAClientThatDoesNotAnswerIsClosedAfterThree(t *testing.T) {
	f := newFixture(t)
	q := f.quiet(60, false)
	k := f.keepAliveOf()
	// Three intervals of silence: the question is asked at the first and stays asked.
	for interval := 1; interval <= KeepAliveMax; interval++ {
		f.clock.Advance(60 * time.Second)
		if got := k.sentSoFar(); got != 1 {
			t.Fatalf("after %d silent intervals %d questions were sent, want the one that waits", interval, got)
		}
		q.waitQuestions(t, 1)
		select {
		case <-q.gone:
			t.Fatalf("the connection was closed after %d silent intervals", interval)
		default:
		}
	}
	// The next one closes it.
	f.clock.Advance(60 * time.Second)
	select {
	case <-q.gone:
	case <-time.After(waitFor):
		t.Fatal("a client that never answers is still connected")
	}
	f.waitConnections(0)
	log := f.log.String()
	if !strings.Contains(log, `"msg":"connection timed out"`) || !strings.Contains(log, `"silentIntervals":3`) || !strings.Contains(log, `"person":"alice"`) {
		t.Errorf("the log does not say why:\n%s", log)
	}
	// Nothing of the keepalive is left on the clock.
	if k.armed() {
		t.Error("the keepalive still has a timer")
	}
}

func TestTrafficPostponesTheQuestion(t *testing.T) {
	f := newFixture(t)
	q := f.quiet(60, false)
	k := f.keepAliveOf()
	// Something arrives in every interval: the server never asks, although the client would
	// not answer.
	for round := 0; round < 10; round++ {
		f.clock.Advance(59 * time.Second)
		before := k.arrived.Load()
		if _, _, err := q.conn.SendRequest("nonsense@example.com", true, nil); err != nil {
			t.Fatal(err)
		}
		if k.arrived.Load() == before {
			t.Fatal("the client's request was not counted")
		}
		f.clock.Advance(time.Second)
	}
	if got := k.sentSoFar(); got != 0 {
		t.Errorf("%d questions were sent to a client that was not idle", got)
	}
	// Then silence: the question, and what arrives after it starts the count again.
	f.clock.Advance(60 * time.Second)
	f.clock.Advance(60 * time.Second)
	if got := k.sentSoFar(); got != 1 {
		t.Fatalf("%d questions were sent in two silent intervals, want the one that waits", got)
	}
	if _, _, err := q.conn.SendRequest("nonsense@example.com", true, nil); err != nil {
		t.Fatal(err)
	}
	for interval := 0; interval < KeepAliveMax; interval++ {
		f.clock.Advance(60 * time.Second)
	}
	select {
	case <-q.gone:
		t.Fatal("the count of unanswered questions was not started again by traffic")
	default:
	}
	if conns := f.server.Connections(); len(conns) != 1 {
		t.Errorf("the registry has %d connections", len(conns))
	}
}

func TestZeroTurnsTheKeepAliveOff(t *testing.T) {
	f := newFixture(t)
	q := f.quiet(0, false)
	k := f.keepAliveOf()
	if k.armed() {
		t.Error("the keepalive has a timer although it is off")
	}
	f.clock.Advance(24 * time.Hour)
	if got := k.sentSoFar(); got != 0 {
		t.Errorf("%d questions were sent with the keepalive off", got)
	}
	select {
	case <-q.gone:
		t.Fatal("the connection was closed with the keepalive off")
	default:
	}
}

func TestAChangeOfTheSettingAppliesToLiveConnections(t *testing.T) {
	f := newFixture(t)
	// The client answers, so that every silent interval that follows an answer asks anew.
	q := f.quiet(0, true)
	k := f.keepAliveOf()
	set := func(seconds int) {
		t.Helper()
		if err := f.settings.Set(ctx, string(settings.ServerKeepAliveSeconds), seconds); err != nil {
			t.Fatal(err)
		}
	}
	// answered waits until the answer to the question that was just sent got to the server.
	answered := func(questions int64, before int64) {
		t.Helper()
		q.waitQuestions(t, questions)
		k.waitArrived(t, before)
	}

	// On, for a connection that began with it off: the first question is one interval from the
	// change.
	before := k.arrived.Load()
	set(10)
	f.clock.Advance(9 * time.Second)
	if got := k.sentSoFar(); got != 0 {
		t.Fatalf("%d questions were sent before the new interval passed", got)
	}
	f.clock.Advance(time.Second)
	if got := k.sentSoFar(); got != 1 {
		t.Fatalf("%d questions were sent after the new interval", got)
	}
	answered(1, before)

	// A longer interval: the wait starts again from the change.
	before = k.arrived.Load()
	set(100)
	f.clock.Advance(99 * time.Second)
	if got := k.sentSoFar(); got != 1 {
		t.Fatalf("%d questions were sent before the longer interval passed", got)
	}
	f.clock.Advance(time.Second)
	if got := k.sentSoFar(); got != 2 {
		t.Fatalf("%d questions were sent after the longer interval", got)
	}
	answered(2, before)

	// Off again: the questions stop.
	set(0)
	if k.armed() {
		t.Error("the keepalive has a timer after it was turned off")
	}
	f.clock.Advance(time.Hour)
	if got := k.sentSoFar(); got != 2 {
		t.Errorf("%d questions were sent after it was turned off", got)
	}

	// A change of another setting does not move the keepalive.
	before = k.arrived.Load()
	set(5)
	f.clock.Advance(4 * time.Second)
	if err := f.settings.Set(ctx, string(settings.SFTPEnabled), false); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Second)
	if got := k.sentSoFar(); got != 3 {
		t.Errorf("%d questions were sent, want 3: a change of another setting moved the keepalive", got)
	}
	answered(3, before)
	select {
	case <-q.gone:
		t.Fatal("the connection was closed")
	default:
	}
}

func TestTheEndOfTheConnectionEndsItsKeepAlive(t *testing.T) {
	f := newFixture(t)
	q := f.quiet(60, false)
	k := f.keepAliveOf()
	if !k.armed() {
		t.Fatal("the keepalive has no timer")
	}
	f.clock.Advance(60 * time.Second) // one question that will never be answered
	q.waitQuestions(t, 1)
	_ = q.conn.Close()
	f.waitConnections(0)
	if k.armed() {
		t.Error("the keepalive still has a timer after the connection ended")
	}
	// Neither time nor a change of the setting does anything to it afterwards.
	if err := f.settings.Set(ctx, string(settings.ServerKeepAliveSeconds), 5); err != nil {
		t.Fatal(err)
	}
	f.clock.Advance(time.Hour)
	if got := k.sentSoFar(); got != 1 {
		t.Errorf("%d questions were sent, want the one before the end", got)
	}
	if k.armed() {
		t.Error("the keepalive has a timer again")
	}
}

func TestCountingConn(t *testing.T) {
	here, there := net.Pipe()
	defer func() { _ = here.Close() }()
	var arrived atomic.Int64
	counting := countingConn{Conn: here, arrived: &arrived}
	go func() {
		_, _ = there.Write([]byte("twelve bytes"))
		_ = there.Close()
	}()
	buf := make([]byte, 5)
	total := 0
	for {
		n, err := counting.Read(buf)
		total += n
		if err != nil {
			break
		}
	}
	if total != 12 || arrived.Load() != 12 {
		t.Errorf("read %d bytes, counted %d", total, arrived.Load())
	}
}
