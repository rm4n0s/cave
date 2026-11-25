package forward

import (
	"io"
	"net"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/server/servertest"
	"github.com/rm4n0s/cave/internal/core/settings"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// remoteForwards waits until the registry shows n ports Cave listens on for clients, and
// returns their addresses.
func (f *fixture) remoteForwards(n int) []string {
	f.t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		var addresses []string
		for _, conn := range f.Server.Connections() {
			for _, activity := range conn.Activities {
				if activity.Kind == server.ActivityRemoteForward {
					addresses = append(addresses, activity.Detail)
				}
			}
		}
		if len(addresses) == n {
			return addresses
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the registry shows the remote forwards %v, want %d", addresses, n)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// serveEcho makes the client's side of a forward an echo server: every connection that arrives
// through the forward gets back what it sends.
func serveEcho(listener net.Listener) {
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer func() { _ = conn.Close() }()
				_, _ = io.Copy(conn, conn)
				_, _ = io.WriteString(conn, "bye")
			}()
		}
	}()
}

// refused waits until nothing listens on an address any more: a connection there is refused. A
// connection that got in while the listener was being closed is reset instead, which is not
// yet the answer.
func refused(t *testing.T, address string) {
	t.Helper()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		conn, err := net.DialTimeout("tcp", address, time.Second)
		if errors.Is(err, errnoRefused) {
			return
		}
		if err == nil {
			_ = conn.Close()
		}
		if time.Now().After(deadline) {
			t.Fatalf("%s is still listened on (the last attempt: %v)", address, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

// request sends tcpip-forward for a host and a port, and returns whether it was granted and the
// port in the reply (0 when the reply has none).
func (f *fixture) request(kind, host string, port uint32) (bool, uint32) {
	f.t.Helper()
	ok, reply, err := f.client.SendRequest(kind, true, ssh.Marshal(forwardRequest{Host: host, Port: port}))
	if err != nil {
		f.t.Fatal(err)
	}
	var got struct{ Port uint32 }
	if len(reply) > 0 {
		if err := ssh.Unmarshal(reply, &got); err != nil {
			f.t.Fatalf("the reply %v: %v", reply, err)
		}
	}
	return ok, got.Port
}

func TestRemoteForward(t *testing.T) {
	f := newFixture(t, nil)
	// Port 0: the system chooses, and the reply tells the port.
	listener, err := f.client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if port == 0 {
		t.Fatal("the reply did not tell the port")
	}
	address := "127.0.0.1:" + strconv.Itoa(port)
	if got := f.remoteForwards(1); got[0] != address {
		t.Errorf("the registry shows %v, want %s", got, address)
	}

	// A connection to the port arrives at the client as a channel that knows where it came from.
	arrived := make(chan net.Conn, 1)
	go func() {
		conn, err := listener.Accept()
		if err == nil {
			arrived <- conn
		}
	}()
	outside, err := net.Dial("tcp", address)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outside.Close() }()
	var inside net.Conn
	select {
	case inside = <-arrived:
	case <-time.After(servertest.WaitFor):
		t.Fatal("the connection did not arrive at the client")
	}
	defer func() { _ = inside.Close() }()
	if got, want := inside.RemoteAddr().String(), outside.LocalAddr().String(); got != want {
		t.Errorf("the client is told the connection came from %s, want %s", got, want)
	}

	// Bytes both ways, and the end of one way passed on while the other still works.
	if _, err := io.WriteString(outside, "from outside"); err != nil {
		t.Fatal(err)
	}
	got := make([]byte, 12)
	if _, err := io.ReadFull(inside, got); err != nil || string(got) != "from outside" {
		t.Fatalf("the client read %q, %v", got, err)
	}
	if _, err := io.WriteString(inside, "from the client"); err != nil {
		t.Fatal(err)
	}
	got = make([]byte, 15)
	if _, err := io.ReadFull(outside, got); err != nil || string(got) != "from the client" {
		t.Fatalf("outside read %q, %v", got, err)
	}
	if err := outside.(*net.TCPConn).CloseWrite(); err != nil {
		t.Fatal(err)
	}
	if rest, err := io.ReadAll(inside); err != nil || len(rest) != 0 {
		t.Errorf("after the end from outside the client read %q, %v", rest, err)
	}
	if _, err := io.WriteString(inside, "last words"); err != nil {
		t.Fatal(err)
	}
	_ = inside.Close()
	if rest, err := io.ReadAll(outside); err != nil || string(rest) != "last words" {
		t.Errorf("after its own end, outside read %q, %v", rest, err)
	}

	// Cancelled: nothing listens there any more, and the registry is empty.
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	f.remoteForwards(0)
	refused(t, address)
	// The forwarded connection's goroutine logs its end a moment after both sides are done.
	deadline := time.Now().Add(servertest.WaitFor)
	for !strings.Contains(f.Log.String(), `"msg":"forwarded connection closed"`) && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	log := f.Log.String()
	for _, want := range []string{`"msg":"remote forward opened"`, `"msg":"remote forward closed"`, `"msg":"forwarded connection closed"`, `"address":"` + address + `"`} {
		if !strings.Contains(log, want) {
			t.Errorf("the log has no %s:\n%s", want, log)
		}
	}
}

func TestLoopbackIsForcedWithoutGatewayPorts(t *testing.T) {
	tests := []struct {
		name string
		// asked is the host the client asks for, and want the host Cave listens on.
		asked, want string
	}{
		{"every IPv4 interface", "0.0.0.0", "127.0.0.1"},
		{"every interface", "", "127.0.0.1"},
		{"a star", "*", "127.0.0.1"},
		{"localhost", "localhost", "127.0.0.1"},
		{"an address of another computer", "192.0.2.7", "127.0.0.1"},
		{"every IPv6 interface", "::", "::1"},
		{"an IPv6 address of another computer", "2001:db8::7", "::1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.want == "::1" {
				probe, err := net.Listen("tcp6", "[::1]:0")
				if err != nil {
					t.Skipf("this computer has no ::1: %v", err)
				}
				_ = probe.Close()
			}
			f := newFixture(t, nil)
			ok, port := f.request("tcpip-forward", tt.asked, 0)
			if !ok || port == 0 {
				t.Fatalf("tcpip-forward for %q: granted %v, port %d", tt.asked, ok, port)
			}
			// What Cave really listens on is in the registry: the loopback address, whatever
			// was asked.
			want := net.JoinHostPort(tt.want, strconv.Itoa(int(port)))
			if got := f.remoteForwards(1); got[0] != want {
				t.Errorf("asked for %q, Cave listens on %v, want %s", tt.asked, got, want)
			}
			// The client cancels with the host as it wrote it.
			if ok, _ := f.request("cancel-tcpip-forward", tt.asked, port); !ok {
				t.Error("the cancel was refused")
			}
			f.remoteForwards(0)
			refused(t, want)
		})
	}
}

func TestBindHost(t *testing.T) {
	tests := []struct {
		asked        string
		gatewayPorts bool
		want         string
		wantOK       bool
	}{
		// Off: only this computer, whatever is asked.
		{"", false, "127.0.0.1", true},
		{"*", false, "127.0.0.1", true},
		{"0.0.0.0", false, "127.0.0.1", true},
		{"localhost", false, "127.0.0.1", true},
		{"127.0.0.1", false, "127.0.0.1", true},
		{"192.0.2.7", false, "127.0.0.1", true},
		{"::", false, "::1", true},
		{"::1", false, "::1", true},
		{"2001:db8::7", false, "::1", true},
		{"::ffff:192.0.2.7", false, "127.0.0.1", true}, // an IPv4 address written the IPv6 way
		{"example.com", false, "", false},
		{"127.0.0.1 ", false, "", false},
		{"host\nname", false, "", false},
		// On: what is asked.
		{"", true, "", true},
		{"*", true, "", true},
		{"0.0.0.0", true, "0.0.0.0", true},
		{"::", true, "::", true},
		{"localhost", true, "127.0.0.1", true},
		{"127.0.0.1", true, "127.0.0.1", true},
		{"192.0.2.7", true, "192.0.2.7", true},
		{"::1", true, "::1", true},
		{"2001:db8::7", true, "2001:db8::7", true},
		{"example.com", true, "", false},
		{"256.1.1.1", true, "", false},
	}
	for _, tt := range tests {
		got, ok := bindHost(tt.asked, tt.gatewayPorts)
		if got != tt.want || ok != tt.wantOK {
			t.Errorf("bindHost(%q, %v) = %q, %v, want %q, %v", tt.asked, tt.gatewayPorts, got, ok, tt.want, tt.wantOK)
		}
	}
}

func TestGatewayPortsOnListensWhereAsked(t *testing.T) {
	// The test asks only for loopback addresses: a test never listens anywhere else. That the
	// address asked is the one used shows in the IPv4 address being kept for an IPv4 request,
	// where "off" would give the same, and in a host that is not an address being refused.
	f := newFixture(t, nil)
	f.Set(string(settings.ForwardingGatewayPorts), true)
	ok, port := f.request("tcpip-forward", "127.0.0.1", 0)
	if !ok || port == 0 {
		t.Fatalf("granted %v, port %d", ok, port)
	}
	if got, want := f.remoteForwards(1), "127.0.0.1:"+strconv.Itoa(int(port)); got[0] != want {
		t.Errorf("Cave listens on %v, want %s", got, want)
	}
	if ok, _ := f.request("tcpip-forward", "example.com", 0); ok {
		t.Error("a host that is not an address was granted")
	}
}

func TestAPortTheClientChooses(t *testing.T) {
	// A port the system gave out a moment ago, free again.
	probe, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	port := uint32(probe.Addr().(*net.TCPAddr).Port)
	if err := probe.Close(); err != nil {
		t.Fatal(err)
	}
	f := newFixture(t, nil)
	// The reply has no port: the client knows it.
	ok, replied := f.request("tcpip-forward", "127.0.0.1", port)
	if !ok || replied != 0 {
		t.Fatalf("granted %v, the reply has the port %d", ok, replied)
	}
	address := "127.0.0.1:" + strconv.Itoa(int(port))
	if got := f.remoteForwards(1); got[0] != address {
		t.Errorf("Cave listens on %v, want %s", got, address)
	}
	// The same port again is taken, by this very forward.
	if ok, _ := f.request("tcpip-forward", "127.0.0.1", port); ok {
		t.Error("the same port was granted twice")
	}
	logged := f.LoggedError("remote forward failed")
	if logged.Tag != TagListenFailed || !logged.HasRoute("handler.listen.ForwardListenFailed") || logged.Metadata["host"] != "127.0.0.1" {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	if !addressInUse(logged.OriginalErr) {
		t.Errorf("the cause is %q", logged.OriginalErr)
	}
	// A cancel for another port, or another host, closes nothing.
	if ok, _ := f.request("cancel-tcpip-forward", "127.0.0.1", port+1); ok {
		t.Error("a cancel for a port that is not forwarded was granted")
	}
	if ok, _ := f.request("cancel-tcpip-forward", "localhost", port); ok {
		t.Error("a cancel for a host that was not asked was granted")
	}
	f.remoteForwards(1)
	if ok, _ := f.request("cancel-tcpip-forward", "127.0.0.1", port); !ok {
		t.Error("the cancel was refused")
	}
	f.remoteForwards(0)
}

func TestRemoteForwardingTurnedOff(t *testing.T) {
	f := newFixture(t, nil)
	// A forward that is there when it is turned off stays.
	listener, err := f.client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveEcho(listener)
	f.Set(string(settings.ForwardingRemote), false)

	if ok, port := f.request("tcpip-forward", "127.0.0.1", 0); ok || port != 0 {
		t.Errorf("tcpip-forward with forwarding.remote off: granted %v, port %d", ok, port)
	}
	if got := f.remoteForwards(1); got[0] != listener.Addr().String() {
		t.Errorf("the registry shows %v", got)
	}
	if log := f.Log.String(); !strings.Contains(log, `"msg":"remote forward refused"`) || !strings.Contains(log, "forwarding.remote is off") {
		t.Errorf("the log does not say why:\n%s", log)
	}
	outside, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outside.Close() }()
	exchange(t, outside, "still forwarded")

	f.Set(string(settings.ForwardingRemote), true)
	if ok, port := f.request("tcpip-forward", "127.0.0.1", 0); !ok || port == 0 {
		t.Errorf("on again: granted %v, port %d", ok, port)
	}
}

func TestTheListenerLimit(t *testing.T) {
	f := newFixture(t, nil)
	var ports []uint32
	for i := 0; i < MaxListeners; i++ {
		ok, port := f.request("tcpip-forward", "127.0.0.1", 0)
		if !ok {
			t.Fatalf("forward %d was refused", i+1)
		}
		ports = append(ports, port)
	}
	if ok, _ := f.request("tcpip-forward", "127.0.0.1", 0); ok {
		t.Error("one more than the limit was granted")
	}
	f.remoteForwards(MaxListeners)
	// Room again after a cancel.
	if ok, _ := f.request("cancel-tcpip-forward", "127.0.0.1", ports[0]); !ok {
		t.Fatal("the cancel was refused")
	}
	if ok, _ := f.request("tcpip-forward", "127.0.0.1", 0); !ok {
		t.Error("after a cancel there was no room")
	}
	// The limit is per connection: another connection of the same person has its own.
	other := f.Connect("alice", f.key)
	ok, _, err := other.SendRequest("tcpip-forward", true, ssh.Marshal(forwardRequest{Host: "127.0.0.1"}))
	if err != nil || !ok {
		t.Errorf("another connection: granted %v, %v", ok, err)
	}
}

func TestTheEndOfTheConnectionClosesItsListeners(t *testing.T) {
	for name, end := range map[string]func(f *fixture){
		"the client leaves":       func(f *fixture) { _ = f.client.Close() },
		"the user disconnects it": func(f *fixture) { f.Server.DisconnectAll() },
		"the server stops":        func(f *fixture) { f.stop() },
	} {
		t.Run(name, func(t *testing.T) {
			f := newFixture(t, nil)
			var addresses []string
			for i := 0; i < 3; i++ {
				ok, port := f.request("tcpip-forward", "127.0.0.1", 0)
				if !ok {
					t.Fatal("the forward was refused")
				}
				addresses = append(addresses, "127.0.0.1:"+strconv.Itoa(int(port)))
			}
			// A connection that arrived and is open ends too.
			listener, err := f.client.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			serveEcho(listener)
			outside, err := net.Dial("tcp", listener.Addr().String())
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = outside.Close() }()
			exchange(t, outside, "hello")

			end(f)
			for _, address := range append(addresses, listener.Addr().String()) {
				refused(t, address)
			}
			if err := outside.SetReadDeadline(time.Now().Add(servertest.WaitFor)); err != nil {
				t.Fatal(err)
			}
			if _, err := outside.Read(make([]byte, 1)); err == nil || errors.Is(err, syscall.ETIMEDOUT) || isTimeout(err) {
				t.Errorf("the connection that arrived is still open: %v", err)
			}
		})
	}
}

// isTimeout says whether a read ended because its deadline passed.
func isTimeout(err error) bool {
	var netErr net.Error
	return errors.As(err, &netErr) && netErr.Timeout()
}

func TestAChannelTheClientRefuses(t *testing.T) {
	f := newFixture(t, nil)
	// The client asks for the forward and then refuses what arrives: it has no listener for it.
	ok, port := f.request("tcpip-forward", "127.0.0.1", 0)
	if !ok {
		t.Fatal("the forward was refused")
	}
	outside, err := net.Dial("tcp", "127.0.0.1:"+strconv.Itoa(int(port)))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = outside.Close() }()
	if err := outside.SetReadDeadline(time.Now().Add(servertest.WaitFor)); err != nil {
		t.Fatal(err)
	}
	// Cave closes the connection that arrived; nothing was sent to it.
	if got, _ := io.ReadAll(outside); len(got) != 0 {
		t.Errorf("the connection read %q", got)
	}
}

func TestTooManyForwardedConnectionsAtOnce(t *testing.T) {
	f := newFixture(t, nil)
	listener, err := f.client.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	serveEcho(listener)
	var open []net.Conn
	defer func() {
		for _, conn := range open {
			_ = conn.Close()
		}
	}()
	for i := 0; i < MaxForwarded; i++ {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		open = append(open, conn)
	}
	// Each of them is carried: the last one answers.
	exchange(t, open[len(open)-1], "the last one that fits")
	exchange(t, open[0], "the first one")

	// One more is closed as it arrives.
	extra, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = extra.Close() }()
	if err := extra.SetReadDeadline(time.Now().Add(servertest.WaitFor)); err != nil {
		t.Fatal(err)
	}
	if got, err := io.ReadAll(extra); len(got) != 0 || isTimeout(err) {
		t.Errorf("one connection more than the limit read %q, %v", got, err)
	}
	// Room again when one of them ends.
	_ = open[0].Close()
	deadline := time.Now().Add(servertest.WaitFor)
	for {
		conn, err := net.Dial("tcp", listener.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		open = append(open, conn)
		answered := make(chan bool, 1)
		go func() {
			_, _ = io.WriteString(conn, "x")
			_, err := io.ReadFull(conn, make([]byte, 1))
			answered <- err == nil
		}()
		if <-answered {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("no room after a forwarded connection ended")
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// brokenListener is a listener whose Accept fails.
type brokenListener struct {
	net.Listener
}

func (brokenListener) Accept() (net.Conn, error) {
	return nil, syscall.EMFILE
}

func TestAListenerThatFailsIsClosed(t *testing.T) {
	f := &fixture{Fixture: servertest.New(t), t: t}
	Register(f.Server, Options{Settings: f.Settings, Clock: f.Clock, Logger: f.Logger, Listen: func(network, address string) (net.Listener, error) {
		listener, err := net.Listen(network, address)
		if err != nil {
			return nil, err
		}
		return brokenListener{listener}, nil
	}})
	f.Start()
	_, f.key = f.Person("alice")
	f.client = f.Connect("alice", f.key)
	ok, port := f.request("tcpip-forward", "127.0.0.1", 0)
	if !ok || port == 0 {
		t.Fatalf("granted %v, port %d", ok, port)
	}
	// The forward is taken out of the registry, and the port is free again.
	f.remoteForwards(0)
	refused(t, "127.0.0.1:"+strconv.Itoa(int(port)))
	logged := f.LoggedError("remote forward failed")
	if logged.Tag != TagAcceptFailed || !logged.HasRoute("handler.accept.ForwardAcceptFailed") || !strings.Contains(logged.OriginalErr, "too many open files") {
		t.Errorf("logged %+v with the route %s", logged, logged.Route())
	}
	if ok, _ := f.request("cancel-tcpip-forward", "127.0.0.1", port); ok {
		t.Error("a cancel for a forward that failed was granted")
	}
}

func TestRemoteForwardRequestsThatAreNotValid(t *testing.T) {
	f := newFixture(t, nil)
	tests := []struct {
		name    string
		kind    string
		payload []byte
	}{
		{"a port that does not exist", "tcpip-forward", ssh.Marshal(forwardRequest{Host: "127.0.0.1", Port: 65536})},
		{"a host that is not an address", "tcpip-forward", ssh.Marshal(forwardRequest{Host: "example.com"})},
		{"a host with an escape sequence", "tcpip-forward", ssh.Marshal(forwardRequest{Host: "127.0.0.1\x1b[31m"})},
		{"a host that is too long", "tcpip-forward", ssh.Marshal(forwardRequest{Host: strings.Repeat("1", 256)})},
		{"a request cut short", "tcpip-forward", []byte{0, 0, 0, 9, '1'}},
		{"no request at all", "tcpip-forward", nil},
		{"a cancel cut short", "cancel-tcpip-forward", []byte{0, 0}},
		{"a cancel of nothing", "cancel-tcpip-forward", ssh.Marshal(forwardRequest{Host: "127.0.0.1", Port: 1})},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ok, reply, err := f.client.SendRequest(tt.kind, true, tt.payload)
			if err != nil || ok || len(reply) != 0 {
				t.Errorf("granted %v, reply %v, %v", ok, reply, err)
			}
		})
	}
	f.remoteForwards(0)
	if log := f.Log.String(); strings.Contains(log, "[31m") {
		t.Errorf("the log has the client's text:\n%s", log)
	}
}
