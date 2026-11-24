package server

import (
	"context"
	"io"
	"reflect"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// holdChannels registers a handler for the channel type "hold": it accepts the channel and keeps
// it until the client closes it or the connection ends. It returns the count of handlers that
// are running.
func (f *fixture) holdChannels() *atomic.Int64 {
	var running atomic.Int64
	f.server.HandleChannel("hold", func(ctx context.Context, _ *Conn, newChannel ssh.NewChannel) {
		running.Add(1)
		defer running.Add(-1)
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go ssh.DiscardRequests(requests)
		done := make(chan struct{})
		go func() {
			// The end of the channel's input is the client closing it.
			_, _ = io.Copy(io.Discard, channel)
			close(done)
		}()
		select {
		case <-done:
		case <-ctx.Done():
		}
		_ = channel.Close()
	})
	return &running
}

// waitConnections waits until the registry has n connections, and returns them.
func (f *fixture) waitConnections(n int) []Connection {
	f.t.Helper()
	deadline := time.Now().Add(waitFor)
	for {
		conns := f.server.Connections()
		if len(conns) == n {
			return conns
		}
		if time.Now().After(deadline) {
			f.t.Fatalf("the registry has %d connections after %v, want %d", len(conns), waitFor, n)
		}
		time.Sleep(time.Millisecond)
	}
}

// waitGone waits until a client's connection has ended.
func waitGone(t *testing.T, client *ssh.Client) {
	t.Helper()
	gone := make(chan struct{})
	go func() {
		_ = client.Wait() // how it ended is not what is tested
		close(gone)
	}()
	select {
	case <-gone:
	case <-time.After(waitFor):
		t.Fatal("the connection was not closed")
	}
}

// alive says whether a client's connection still answers.
func alive(client *ssh.Client) bool {
	_, _, err := client.SendRequest(requestKeepAlive, true, nil)
	return err == nil
}

// connect logs a person in with a key, failing the test when the server refuses, and waits
// until the connection is in the registry: the client knows that it is in a moment before the
// server has noted it.
func (f *fixture) connect(name string, key sshtest.Key) *ssh.Client {
	f.t.Helper()
	before := len(f.server.Connections())
	client, err := f.dial(f.config(name, key))
	if err != nil {
		f.t.Fatalf("%s was refused: %v", name, err)
	}
	f.waitConnections(before + 1)
	return client
}

func TestTheRegistry(t *testing.T) {
	f := newFixture(t)
	alice, aliceKey := f.person("alice")
	bob, bobKey := f.person("bob")
	if len(f.server.Connections()) != 0 {
		t.Fatal("the registry of a new server has connections")
	}
	var changes atomic.Int64
	f.server.Subscribe(func(change Change) {
		if change == ConnectionsChanged {
			changes.Add(1)
		}
	})

	first := f.connect("alice", aliceKey)
	conns := f.waitConnections(1)
	keys, _ := f.people.Get(alice.ID)
	want := Connection{
		ID: conns[0].ID, PersonID: alice.ID, PersonName: "alice", KeyID: keys.Keys[0].ID,
		KeyFingerprint: ssh.FingerprintSHA256(aliceKey.Public), Address: "127.0.0.1", ClientVersion: "SSH-2.0-Go", Since: start,
	}
	if !reflect.DeepEqual(conns[0], want) {
		t.Errorf("the connection is\n%+v\nwant\n%+v", conns[0], want)
	}
	if len(conns[0].ID) != 18 || conns[0].ID[:2] != "c_" {
		t.Errorf("the id is %q", conns[0].ID)
	}

	// The oldest first.
	f.clock.Advance(time.Minute)
	second := f.connect("bob", bobKey)
	f.clock.Advance(time.Minute)
	third := f.connect("alice", aliceKey)
	conns = f.waitConnections(3)
	var who []string
	for _, c := range conns {
		who = append(who, c.PersonName)
	}
	if !reflect.DeepEqual(who, []string{"alice", "bob", "alice"}) || conns[1].PersonID != bob.ID || !conns[2].Since.Equal(start.Add(2*time.Minute)) {
		t.Errorf("the registry is %+v", conns)
	}
	if conns[0].ID == conns[2].ID {
		t.Error("two connections share an id")
	}

	// A client that leaves is taken out.
	if err := second.Close(); err != nil {
		t.Fatal(err)
	}
	conns = f.waitConnections(2)
	if conns[0].PersonName != "alice" || conns[1].PersonName != "alice" {
		t.Errorf("after bob left: %+v", conns)
	}
	if got := changes.Load(); got < 4 {
		t.Errorf("listeners were told of %d changes of the connections, want at least 4", got)
	}
	if !alive(first) || !alive(third) {
		t.Error("another connection was closed when bob left")
	}
}

func TestDisconnect(t *testing.T) {
	f := newFixture(t)
	_, aliceKey := f.person("alice")
	_, bobKey := f.person("bob")
	a1 := f.connect("alice", aliceKey)
	f.clock.Advance(time.Second)
	b := f.connect("bob", bobKey)
	f.clock.Advance(time.Second)
	a2 := f.connect("alice", aliceKey)
	conns := f.waitConnections(3)

	if err := f.server.Disconnect(conns[0].ID); err != nil {
		t.Fatal(err)
	}
	waitGone(t, a1)
	if left := f.waitConnections(2); left[0].ID != conns[1].ID || left[1].ID != conns[2].ID {
		t.Errorf("the registry after the disconnect: %+v", left)
	}
	if !alive(b) || !alive(a2) {
		t.Error("Disconnect closed another connection")
	}
	meta := errtest.Route(t, f.server.Disconnect(conns[0].ID), "Server.Disconnect.ConnectionNotFound")
	if meta["id"] != conns[0].ID {
		t.Errorf("metadata = %v", meta)
	}
	errtest.Route(t, f.server.Disconnect("c_0000000000000000"), "Server.Disconnect.ConnectionNotFound")

	f.server.DisconnectAll()
	waitGone(t, b)
	waitGone(t, a2)
	f.waitConnections(0)
	// The server still runs, and takes new connections.
	if f.server.State() != Running {
		t.Errorf("the server is %v after DisconnectAll", f.server.State())
	}
	f.connect("alice", aliceKey)
	f.waitConnections(1)
}

func TestAChangeOfThePeopleEndsTheirConnections(t *testing.T) {
	type world struct {
		f                *fixture
		aliceID, bobID   string
		aliceKeys        []string // the ids of alice's two keys
		alice1, alice2   *ssh.Client
		bob              *ssh.Client
		aliceKey2, extra sshtest.Key
	}
	setup := func(t *testing.T) *world {
		f := newFixture(t)
		alice, key1 := f.person("alice")
		key2 := sshtest.NewKey(t, sshtest.Ed25519)
		added, err := f.people.AddKeys(ctx, alice.ID, key2.AuthorizedKey)
		if err != nil {
			t.Fatal(err)
		}
		bob, bobKey := f.person("bob")
		p, _ := f.people.Get(alice.ID)
		w := &world{f: f, aliceID: alice.ID, bobID: bob.ID, aliceKeys: []string{p.Keys[0].ID, added[0].ID}, aliceKey2: key2,
			extra: sshtest.NewKey(t, sshtest.Ed25519)}
		w.alice1 = f.connect("alice", key1)
		w.alice2 = f.connect("alice", key2)
		w.bob = f.connect("bob", bobKey)
		f.waitConnections(3)
		return w
	}
	tests := []struct {
		name   string
		change func(w *world) error
		// wantGone names which of alice1, alice2 and bob must be closed.
		wantGone [3]bool
	}{
		{"renaming a person", func(w *world) error { return w.f.people.Rename(ctx, w.aliceID, "ada") }, [3]bool{true, true, false}},
		{"disabling a person", func(w *world) error { return w.f.people.SetEnabled(ctx, w.aliceID, false) }, [3]bool{true, true, false}},
		{"removing a person", func(w *world) error { return w.f.people.Remove(ctx, w.aliceID) }, [3]bool{true, true, false}},
		{"removing a key", func(w *world) error { return w.f.people.RemoveKey(ctx, w.aliceID, w.aliceKeys[1]) }, [3]bool{false, true, false}},
		{"removing the other key", func(w *world) error { return w.f.people.RemoveKey(ctx, w.aliceID, w.aliceKeys[0]) }, [3]bool{true, false, false}},
		{"adding a key", func(w *world) error {
			_, err := w.f.people.AddKeys(ctx, w.aliceID, w.extra.AuthorizedKey)
			return err
		}, [3]bool{}},
		{"changing a comment", func(w *world) error { return w.f.people.SetKeyComment(ctx, w.aliceID, w.aliceKeys[0], "new") }, [3]bool{}},
		{"adding a person", func(w *world) error { _, err := w.f.people.Add(ctx, "carol"); return err }, [3]bool{}},
		{"enabling a person who is enabled", func(w *world) error { return w.f.people.SetEnabled(ctx, w.aliceID, true) }, [3]bool{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			w := setup(t)
			if err := tt.change(w); err != nil {
				t.Fatal(err)
			}
			clients := []*ssh.Client{w.alice1, w.alice2, w.bob}
			names := []string{"alice's first connection", "alice's second connection", "bob's connection"}
			left := 3
			for i, gone := range tt.wantGone {
				if gone {
					waitGone(t, clients[i])
					left--
				}
			}
			w.f.waitConnections(left)
			for i, gone := range tt.wantGone {
				if !gone && !alive(clients[i]) {
					t.Errorf("%s was closed", names[i])
				}
			}
		})
	}
}

func TestChannelsAreDispatchedAndLimited(t *testing.T) {
	f := newStopped(t)
	running := f.holdChannels()
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)

	// A type nobody serves, and one that is served.
	_, _, err := client.OpenChannel("nonsense", nil)
	var openErr *ssh.OpenChannelError
	if !errors.As(err, &openErr) || openErr.Reason != ssh.UnknownChannelType {
		t.Errorf("an unknown channel type: %v", err)
	}
	_, _, err = client.OpenChannel("session", nil)
	if !errors.As(err, &openErr) || openErr.Reason != ssh.UnknownChannelType {
		t.Errorf("a session without a handler: %v", err)
	}

	var open []ssh.Channel
	for i := 0; i < MaxChannels; i++ {
		channel, requests, err := client.OpenChannel("hold", nil)
		if err != nil {
			t.Fatalf("channel %d was refused: %v", i+1, err)
		}
		go ssh.DiscardRequests(requests)
		open = append(open, channel)
	}
	_, _, err = client.OpenChannel("hold", nil)
	if !errors.As(err, &openErr) || openErr.Reason != ssh.ResourceShortage {
		t.Errorf("channel %d: %v, want a resource shortage", MaxChannels+1, err)
	}
	// The limit is each connection's own.
	other := f.connect("alice", key)
	channel, requests, err := other.OpenChannel("hold", nil)
	if err != nil {
		t.Fatalf("another connection was held back by the first one's channels: %v", err)
	}
	go ssh.DiscardRequests(requests)
	open = append(open, channel)

	// A closed channel makes room.
	if err := open[0].Close(); err != nil {
		t.Fatal(err)
	}
	// The server notes the close a moment after the client made it.
	deadline := time.Now().Add(waitFor)
	for {
		channel, requests, err = client.OpenChannel("hold", nil)
		if err == nil {
			go ssh.DiscardRequests(requests)
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("no room after a channel was closed: %v", err)
		}
		time.Sleep(time.Millisecond)
	}
	_ = channel

	// When the connections end, every handler returns.
	f.server.DisconnectAll()
	f.waitConnections(0)
	if got := running.Load(); got != 0 {
		t.Errorf("%d handlers still run after their connections ended", got)
	}
}

func TestGlobalRequests(t *testing.T) {
	f := newStopped(t)
	var got struct {
		sync.Mutex
		person  string
		payload []byte
	}
	f.server.HandleRequest("echo@cave.test", func(_ context.Context, conn *Conn, request *ssh.Request) (bool, []byte) {
		got.Lock()
		defer got.Unlock()
		got.person, got.payload = conn.PersonName(), request.Payload
		return true, append([]byte("reply to "), request.Payload...)
	})
	f.server.HandleRequest("refuse@cave.test", func(context.Context, *Conn, *ssh.Request) (bool, []byte) {
		return false, nil
	})
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)

	tests := []struct {
		request     string
		payload     []byte
		wantOK      bool
		wantPayload string
	}{
		{"keepalive@openssh.com", nil, true, ""},
		{"echo@cave.test", []byte("hello"), true, "reply to hello"},
		{"refuse@cave.test", nil, false, ""},
		{"tcpip-forward", nil, false, ""}, // no handler yet
		{"nonsense", []byte("x"), false, ""},
	}
	for _, tt := range tests {
		ok, payload, err := client.SendRequest(tt.request, true, tt.payload)
		if err != nil {
			t.Fatalf("%s: %v", tt.request, err)
		}
		if ok != tt.wantOK || string(payload) != tt.wantPayload {
			t.Errorf("%s: ok = %v, payload = %q; want %v, %q", tt.request, ok, payload, tt.wantOK, tt.wantPayload)
		}
	}
	got.Lock()
	if got.person != "alice" || string(got.payload) != "hello" {
		t.Errorf("the handler got %q from %q", got.payload, got.person)
	}
	got.Unlock()
	// A request without a reply wanted does not break the connection.
	if _, _, err := client.SendRequest("nonsense", false, nil); err != nil {
		t.Fatal(err)
	}
	if !alive(client) {
		t.Error("the connection ended")
	}
}

func TestNoMoreSessions(t *testing.T) {
	f := newStopped(t)
	f.holdChannels()
	// The handler of "hold" also serves sessions here.
	f.server.mu.Lock()
	hold := f.server.channelHandlers["hold"]
	f.server.mu.Unlock()
	f.server.HandleChannel("session", hold)
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)

	if _, _, err := client.OpenChannel("session", nil); err != nil {
		t.Fatalf("a session before the request: %v", err)
	}
	if ok, _, err := client.SendRequest("no-more-sessions@openssh.com", true, nil); err != nil || !ok {
		t.Fatalf("the request: ok = %v, err = %v", ok, err)
	}
	_, _, err := client.OpenChannel("session", nil)
	var openErr *ssh.OpenChannelError
	if !errors.As(err, &openErr) || openErr.Reason != ssh.Prohibited {
		t.Errorf("a session after the request: %v", err)
	}
	// Other channels are still allowed, and so is a session on another connection.
	if _, _, err := client.OpenChannel("hold", nil); err != nil {
		t.Errorf("another channel after the request: %v", err)
	}
	if _, _, err := f.connect("alice", key).OpenChannel("session", nil); err != nil {
		t.Errorf("a session on another connection: %v", err)
	}
}

func TestActivities(t *testing.T) {
	f := newStopped(t)
	release := make(chan struct{})
	f.server.HandleChannel("work", func(ctx context.Context, conn *Conn, newChannel ssh.NewChannel) {
		channel, requests, err := newChannel.Accept()
		if err != nil {
			return
		}
		go ssh.DiscardRequests(requests)
		remove := conn.AddActivity(ActivityExec, string(newChannel.ExtraData()))
		defer remove()
		select {
		case <-release:
		case <-ctx.Done():
		}
		remove() // removing twice is fine
		_ = channel.Close()
	})
	f.start()
	_, key := f.person("alice")
	client := f.connect("alice", key)
	long := "tar czf - /very/long/path\x1b[31m with an escape and more text than the registry keeps, which is eighty characters"
	for _, command := range []string{"uptime", long} {
		if _, _, err := client.OpenChannel("work", []byte(command)); err != nil {
			t.Fatal(err)
		}
	}
	deadline := time.Now().Add(waitFor)
	var activities []Activity
	for len(activities) != 2 {
		if time.Now().After(deadline) {
			t.Fatalf("the registry shows %d activities, want 2", len(activities))
		}
		time.Sleep(time.Millisecond)
		activities = f.waitConnections(1)[0].Activities
	}
	if activities[0] != (Activity{Kind: ActivityExec, Detail: "uptime"}) {
		t.Errorf("the first activity is %+v", activities[0])
	}
	detail := []rune(activities[1].Detail)
	if activities[1].Kind != ActivityExec || len(detail) != MaxActivityDetail || detail[len(detail)-1] != '…' {
		t.Errorf("the second activity is %+v (%d characters)", activities[1], len(detail))
	}
	for _, r := range activities[1].Detail {
		if r == 0x1b {
			t.Error("the detail holds an escape character")
		}
	}
	// The result is a copy.
	activities[0].Detail = "changed"
	if f.server.Connections()[0].Activities[0].Detail != "uptime" {
		t.Error("the registry was changed through a copy")
	}

	close(release)
	deadline = time.Now().Add(waitFor)
	for len(f.waitConnections(1)[0].Activities) != 0 {
		if time.Now().After(deadline) {
			t.Fatal("the activities were not removed")
		}
		time.Sleep(time.Millisecond)
	}
}

func TestStopEndsEveryConnection(t *testing.T) {
	f := newStopped(t)
	running := f.holdChannels()
	f.start()
	_, key := f.person("alice")
	var clients []*ssh.Client
	for i := 0; i < 5; i++ {
		client := f.connect("alice", key)
		if _, _, err := client.OpenChannel("hold", nil); err != nil {
			t.Fatal(err)
		}
		clients = append(clients, client)
	}
	f.waitConnections(5)
	if err := f.server.Stop(ctx); err != nil {
		t.Fatal(err)
	}
	// Stop returned: the registry is empty and every handler has returned, without waiting.
	if n := len(f.server.Connections()); n != 0 {
		t.Errorf("the registry has %d connections after Stop", n)
	}
	if got := running.Load(); got != 0 {
		t.Errorf("%d handlers still run after Stop", got)
	}
	for _, client := range clients {
		waitGone(t, client)
	}
}

func TestAPersonChangedDuringTheHandshakeIsNotLetStay(t *testing.T) {
	f := newFixture(t)
	alice, key := f.person("alice")
	// The change comes after the key was accepted and before the connection is in the registry:
	// here, from inside the signing, which is after the server's check of the key.
	signer := signerFunc{Signer: key.Signer, before: func() {
		if err := f.people.SetEnabled(ctx, alice.ID, false); err != nil {
			t.Error(err)
		}
	}}
	client, err := f.dial(f.withSigner("alice", signer))
	if err != nil {
		// Refused outright is fine too: the change may have come before the signature was checked.
		return
	}
	waitGone(t, client)
	f.waitConnections(0)
}

// signerFunc runs a function before it signs.
type signerFunc struct {
	ssh.Signer
	before func()
}

func (s signerFunc) Sign(rand io.Reader, data []byte) (*ssh.Signature, error) {
	s.before()
	return s.Signer.Sign(rand, data)
}
