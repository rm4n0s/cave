package forward

import (
	"bytes"
	"context"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// echoServer listens on a loopback address and sends back what it receives. When a client stops
// sending, it sends "bye" and closes: a test sees that the end of one way went through while
// the other way still worked.
type echoServer struct {
	addr string
	port int
	// accepted counts the connections it got.
	accepted atomic.Int32
}

// startEcho starts an echo server on the loopback address of the network ("tcp4" or "tcp6"),
// on a port the system chooses. It skips the test when the computer has no such address.
func startEcho(t *testing.T, network string) *echoServer {
	t.Helper()
	address := "127.0.0.1:0"
	if network == "tcp6" {
		address = "[::1]:0"
	}
	listener, err := net.Listen(network, address)
	if err != nil {
		if network == "tcp6" {
			t.Skipf("this computer has no ::1: %v", err)
		}
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	e := &echoServer{addr: listener.Addr().String(), port: listener.Addr().(*net.TCPAddr).Port}
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			e.accepted.Add(1)
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
				_, _ = io.WriteString(conn, "bye")
			}()
		}
	}()
	return e
}

// fixture is a server that forwards, with alice connected.
type fixture struct {
	*servertest.Fixture
	t      *testing.T
	client *ssh.Client
	key    sshtest.Key
	// dials counts the connections Cave tried to make.
	dials atomic.Int32
	// lastTarget is the network and address of the last of them.
	mu         sync.Mutex
	lastTarget string
}

// newFixture starts a server with forwarding whose dialer is dial (nil: the system's, which the
// tests only ever point at loopback addresses) and connects alice.
func newFixture(t *testing.T, dial Dialer) *fixture {
	t.Helper()
	f := &fixture{Fixture: servertest.New(t), t: t}
	if dial == nil {
		dial = (&net.Dialer{}).DialContext
	}
	Register(f.Server, Options{Settings: f.Settings, Clock: f.Clock, Logger: f.Logger, Dial: func(ctx context.Context, network, address string) (net.Conn, error) {
		f.dials.Add(1)
		f.mu.Lock()
		f.lastTarget = network + " " + address
		f.mu.Unlock()
		return dial(ctx, network, address)
	}})
	f.Start()
	_, f.key = f.Person("alice")
	f.client = f.Connect("alice", f.key)
	return f
}

// target returns the network and address of the last connection Cave tried to make.
func (f *fixture) target() string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.lastTarget
}

// activities waits until alice's connection shows n activities, and returns them.
func (f *fixture) activities(n int) []server.Activity {
	f.t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		activities := f.WaitConnections(1)[0].Activities
		if len(activities) == n {
			return activities
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the connection shows %+v, want %d activities", activities, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// refusal returns why the server refused a channel; the test stops when err is not a refusal.
func refusal(t *testing.T, err error) *ssh.OpenChannelError {
	t.Helper()
	var refused *ssh.OpenChannelError
	if !errors.As(err, &refused) {
		t.Fatalf("expected the channel to be refused, got %v", err)
	}
	return refused
}

// exchange writes a message through a forward and reads its echo. The connections of the SSH
// client have no deadlines, so the wait is the test's own.
func exchange(t *testing.T, conn io.ReadWriter, message string) {
	t.Helper()
	result := make(chan error, 1)
	got := make([]byte, len(message))
	go func() {
		if _, err := io.WriteString(conn, message); err != nil {
			result <- err
			return
		}
		_, err := io.ReadFull(conn, got)
		result <- err
	}()
	select {
	case err := <-result:
		if err != nil || string(got) != message {
			t.Fatalf("the echo is %q, %v, want %q", got, err, message)
		}
	case <-time.After(servertest.WaitFor):
		t.Fatalf("no echo of %q", message)
	}
}

func TestDirectTCPIP(t *testing.T) {
	for _, network := range []string{"tcp4", "tcp6"} {
		t.Run(network, func(t *testing.T) {
			echo := startEcho(t, network)
			f := newFixture(t, nil)
			conn, err := f.client.Dial("tcp", echo.addr)
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = conn.Close() }()
			exchange(t, conn, "first message")
			exchange(t, conn, "second message")
			if got := f.target(); got != "tcp "+echo.addr {
				t.Errorf("Cave dialed %q, want %q", got, "tcp "+echo.addr)
			}

			// The registry shows the forward while it is open.
			activities := f.activities(1)
			if activities[0].Kind != server.ActivityLocalForward || activities[0].Detail != echo.addr {
				t.Errorf("activities = %+v", activities)
			}

			// The client stops sending: the other side sees the end and can still answer.
			if err := conn.(ssh.Channel).CloseWrite(); err != nil {
				t.Fatal(err)
			}
			rest, err := io.ReadAll(conn)
			if err != nil || string(rest) != "bye" {
				t.Errorf("after the client's end the other side sent %q, %v", rest, err)
			}
			f.activities(0)
			log := f.Log.String()
			for _, want := range []string{`"msg":"forward opened"`, `"msg":"forward closed"`, `"sent":27`, `"received":30`, `"person":"alice"`} {
				if !strings.Contains(log, want) {
					t.Errorf("the log has no %s:\n%s", want, log)
				}
			}
		})
	}
}

func TestALargeTransferThroughAForward(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	conn, err := f.client.Dial("tcp", echo.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	data := make([]byte, 4<<20)
	for i := range data {
		data[i] = byte(i * 31)
	}
	go func() {
		_, _ = conn.Write(data)
		_ = conn.(ssh.Channel).CloseWrite()
	}()
	got, err := io.ReadAll(conn)
	if err != nil || !bytes.Equal(got, append(data, "bye"...)) {
		t.Errorf("%d bytes came back, %v, want the %d sent and the goodbye", len(got), err, len(data))
	}
}

func TestSeveralForwardsAtOnce(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	var conns []net.Conn
	for i := 0; i < 5; i++ {
		conn, err := f.client.Dial("tcp", echo.addr)
		if err != nil {
			t.Fatal(err)
		}
		conns = append(conns, conn)
	}
	for i, conn := range conns {
		exchange(t, conn, "forward "+strconv.Itoa(i))
	}
	f.activities(5)
	for _, conn := range conns {
		_ = conn.Close()
	}
	f.activities(0)
}

func TestLocalForwardingTurnedOff(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, nil)
	// A forward that is open when it is turned off goes on.
	open, err := f.client.Dial("tcp", echo.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = open.Close() }()
	f.Set(string(settings.ForwardingLocal), false)
	dialsBefore := f.dials.Load()

	_, err = f.client.Dial("tcp", echo.addr)
	refused := refusal(t, err)
	if refused.Reason != ssh.Prohibited || refused.Message != "port forwarding is turned off" {
		t.Errorf("refused with %v %q, want administratively prohibited", refused.Reason, refused.Message)
	}
	// Nothing was connected to: the refusal comes before any attempt.
	if f.dials.Load() != dialsBefore || echo.accepted.Load() != 1 {
		t.Errorf("Cave dialed %d times and the echo server got %d connections", f.dials.Load()-dialsBefore, echo.accepted.Load())
	}
	if log := f.Log.String(); !strings.Contains(log, `"msg":"forward refused"`) || !strings.Contains(log, "forwarding.local is off") {
		t.Errorf("the log does not say why:\n%s", log)
	}
	exchange(t, open, "still open")

	f.Set(string(settings.ForwardingLocal), true)
	again, err := f.client.Dial("tcp", echo.addr)
	if err != nil {
		t.Fatalf("on again: %v", err)
	}
	defer func() { _ = again.Close() }()
	exchange(t, again, "on again")
}

func TestAPortNobodyListensOn(t *testing.T) {
	// A port that was free a moment ago: the system gave it out, and the listener is closed.
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	closed := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, nil)
	_, err = f.client.Dial("tcp", closed)
	refused := refusal(t, err)
	if refused.Reason != ssh.ConnectionFailed || refused.Message != "connect failed: connection refused" {
		t.Errorf("refused with %v %q", refused.Reason, refused.Message)
	}
	logged := f.LoggedError("forward failed")
	if logged.Tag != TagDialFailed || !logged.HasRoute("handler.dial.ForwardDialFailed") || logged.Metadata["target"] != closed {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	if activities := f.WaitConnections(1)[0].Activities; len(activities) != 0 {
		t.Errorf("activities = %+v", activities)
	}
}

// stuckDialer is a dialer that never connects: it waits until it is told to give up.
type stuckDialer struct {
	started chan struct{}
	// gaveUp gets the reason the attempt was ended with.
	gaveUp chan error
}

func newStuckDialer() *stuckDialer {
	return &stuckDialer{started: make(chan struct{}, 1), gaveUp: make(chan error, 1)}
}

func (d *stuckDialer) dial(ctx context.Context, _, _ string) (net.Conn, error) {
	d.started <- struct{}{}
	<-ctx.Done()
	d.gaveUp <- ctx.Err()
	return nil, ctx.Err()
}

func TestTheConnectionAttemptIsLimited(t *testing.T) {
	dialer := newStuckDialer()
	f := newFixture(t, dialer.dial)
	result := make(chan error, 1)
	go func() {
		_, err := f.client.Dial("tcp", "127.0.0.1:9")
		result <- err
	}()
	<-dialer.started
	// The limit is a timer of the clock: wait until it is set, then let most of it pass.
	deadline := time.Now().Add(servertest.WaitFor)
	for f.Clock.Pending() == 0 {
		if time.Now().After(deadline) {
			t.Fatal("no timer was set for the attempt")
		}
		time.Sleep(time.Millisecond)
	}
	f.Clock.Advance(DialTimeout - time.Second)
	select {
	case err := <-result:
		t.Fatalf("the attempt ended before the limit: %v", err)
	case <-time.After(50 * time.Millisecond):
	}
	f.Clock.Advance(time.Second)
	refused := refusal(t, <-result)
	if refused.Reason != ssh.ConnectionFailed || refused.Message != "connect failed: timed out" {
		t.Errorf("refused with %v %q", refused.Reason, refused.Message)
	}
	if err := <-dialer.gaveUp; !errors.Is(err, context.Canceled) {
		t.Errorf("the dialer was ended with %v", err)
	}
	logged := f.LoggedError("forward failed")
	if logged.Tag != TagDialTimedOut || !logged.HasRoute("handler.dial.ForwardDialTimedOut") || logged.Metadata["limit"] != "15s" || logged.Metadata["target"] != "127.0.0.1:9" {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
}

func TestTheEndOfTheConnectionEndsAnAttempt(t *testing.T) {
	dialer := newStuckDialer()
	f := newFixture(t, dialer.dial)
	go func() { _, _ = f.client.Dial("tcp", "127.0.0.1:9") }()
	<-dialer.started
	_ = f.client.Close()
	select {
	case err := <-dialer.gaveUp:
		if !errors.Is(err, context.Canceled) {
			t.Errorf("the dialer was ended with %v", err)
		}
	case <-time.After(servertest.WaitFor):
		t.Fatal("the attempt goes on after the connection ended")
	}
	f.WaitConnections(0)
}

func TestTheEndOfTheConnectionEndsItsForwards(t *testing.T) {
	for name, end := range map[string]func(f *fixture){
		"the client leaves":       func(f *fixture) { _ = f.client.Close() },
		"the user disconnects it": func(f *fixture) { f.Server.DisconnectAll() },
	} {
		t.Run(name, func(t *testing.T) {
			// A server that only waits for its client to go: the forward ends when it sees it.
			listener, err := net.Listen("tcp4", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = listener.Close() }()
			gone := make(chan struct{})
			go func() {
				conn, err := listener.Accept()
				if err != nil {
					return
				}
				_, _ = io.Copy(io.Discard, conn)
				_ = conn.Close()
				close(gone)
			}()
			f := newFixture(t, nil)
			conn, err := f.client.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			if _, err := io.WriteString(conn, "hello"); err != nil {
				t.Fatal(err)
			}
			f.activities(1)
			end(f)
			select {
			case <-gone:
			case <-time.After(servertest.WaitFor):
				t.Fatal("the forward is still open after the connection ended")
			}
			f.WaitConnections(0)
		})
	}
}

func TestRequestsThatAreNotValid(t *testing.T) {
	f := newFixture(t, nil)
	type request struct {
		Host       string
		Port       uint32
		OriginHost string
		OriginPort uint32
	}
	tests := []struct {
		name    string
		payload []byte
	}{
		{"port 0", ssh.Marshal(request{"127.0.0.1", 0, "127.0.0.1", 1234})},
		{"a port that does not exist", ssh.Marshal(request{"127.0.0.1", 65536, "127.0.0.1", 1234})},
		{"no host", ssh.Marshal(request{"", 80, "127.0.0.1", 1234})},
		{"a host with a space", ssh.Marshal(request{"127.0.0.1 evil", 80, "127.0.0.1", 1234})},
		{"a host with an escape sequence", ssh.Marshal(request{"host\x1b[31m", 80, "127.0.0.1", 1234})},
		{"a host with a line feed", ssh.Marshal(request{"host\nname", 80, "127.0.0.1", 1234})},
		{"a host that is not ASCII", ssh.Marshal(request{"h\u00f6st", 80, "127.0.0.1", 1234})},
		{"a host that is too long", ssh.Marshal(request{strings.Repeat("a", 256), 80, "127.0.0.1", 1234})},
		{"a request cut short", []byte{0, 0, 0, 9, '1', '2', '7'}},
		{"no request at all", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := f.client.OpenChannel("direct-tcpip", tt.payload)
			refused := refusal(t, err)
			if refused.Reason != ssh.ConnectionFailed || refused.Message != "invalid forwarding request" {
				t.Errorf("refused with %v %q", refused.Reason, refused.Message)
			}
		})
	}
	// None of them was connected to, and none is in the log as the client wrote it.
	if f.dials.Load() != 0 {
		t.Errorf("Cave dialed %d times", f.dials.Load())
	}
	if log := f.Log.String(); strings.Contains(log, "evil") || strings.Contains(log, "[31m") {
		t.Errorf("the log has the client's text:\n%s", log)
	}
}

func TestValidHost(t *testing.T) {
	tests := []struct {
		host string
		want bool
	}{
		{"127.0.0.1", true},
		{"::1", true},
		{"fe80::1%eth0", true},
		{"example.com", true},
		{"xn--hst-sna.example", true},
		{strings.Repeat("a", 255), true},
		{strings.Repeat("a", 256), false},
		{"", false},
		{"a b", false},
		{"a\tb", false},
		{"a\x00b", false},
		{"a\x7fb", false},
		{"h\u00f6st", false},
	}
	for _, tt := range tests {
		if got := validHost(tt.host); got != tt.want {
			t.Errorf("validHost(%q) = %v, want %v", tt.host, got, tt.want)
		}
	}
}

func TestReason(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"refused", &net.OpError{Op: "dial", Err: errnoRefused}, "connection refused"},
		{"a name nobody has", &net.DNSError{Err: "no such host", Name: "nowhere.invalid", IsNotFound: true}, "no such host"},
		{"a name server that fails", &net.DNSError{Err: "server misbehaving", Name: "nowhere.invalid"}, "the connection could not be made"},
		{"timed out", errors.NewErr(TagDialTimedOut, context.Canceled), "timed out"},
		{"anything else", io.ErrUnexpectedEOF, "the connection could not be made"},
	}
	for _, tt := range tests {
		if got := reason(tt.err); got != tt.want {
			t.Errorf("%s: reason = %q, want %q", tt.name, got, tt.want)
		}
	}
}

// plainConn is a connection that cannot end one way only, as a dialer that is not the system's
// may return.
type plainConn struct {
	net.Conn
}

func TestAConnectionThatCannotEndOneWayIsClosed(t *testing.T) {
	echo := startEcho(t, "tcp4")
	f := newFixture(t, func(ctx context.Context, network, address string) (net.Conn, error) {
		conn, err := (&net.Dialer{}).DialContext(ctx, network, address)
		if err != nil {
			return nil, err
		}
		return plainConn{conn}, nil
	})
	conn, err := f.client.Dial("tcp", echo.addr)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = conn.Close() }()
	exchange(t, conn, "hello")
	if err := conn.(ssh.Channel).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	// The whole connection is closed, so the goodbye does not arrive, and the forward ends.
	if rest, _ := io.ReadAll(conn); len(rest) != 0 {
		t.Errorf("after the client's end %q arrived", rest)
	}
	f.activities(0)
}
