package servertest

import (
	"os"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/core/server"
	"github.com/rm4n0s/cave/internal/core/settings"
)

func TestFixture(t *testing.T) {
	realHome, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("the test needs to know the real home: %v", err)
	}
	f := New(t)
	if f.Server.State() != server.Stopped || f.Addr != "" {
		t.Fatalf("a new fixture is %v at %q", f.Server.State(), f.Addr)
	}
	if f.Home == realHome || strings.HasPrefix(f.Dirs.Config, realHome) {
		t.Fatalf("the fixture uses the real home: %q, %q", f.Home, f.Dirs.Config)
	}
	f.Start()
	if !strings.HasPrefix(f.Addr, "127.0.0.1:") || strings.HasSuffix(f.Addr, ":0") {
		t.Errorf("the server listens on %q", f.Addr)
	}
	f.Set(string(settings.ServerKeepAliveSeconds), 0)
	if f.Settings.Snapshot().Int(settings.ServerKeepAliveSeconds) != 0 {
		t.Error("Set did not change the setting")
	}

	alice, key := f.Person("alice")
	client := f.Connect("alice", key)
	conns := f.WaitConnections(1)
	if conns[0].PersonID != alice.ID || !conns[0].Since.Equal(StartTime) {
		t.Errorf("the connection is %+v", conns[0])
	}
	if err := client.Close(); err != nil {
		t.Fatal(err)
	}
	f.WaitConnections(0)
	if !strings.Contains(f.Log.String(), `"msg":"connected"`) {
		t.Errorf("the log lacks the connection:\n%s", f.Log.String())
	}

	// The real ssh reaches the server too (skipped without OpenSSH).
	t.Run("OpenSSH", func(t *testing.T) {
		o := New(t)
		o.Start()
		_, key := o.Person("bob")
		if res := o.OpenSSH().SSH("", "bob", key, nil, "true"); strings.Contains(res.Stderr, "Permission denied") {
			t.Errorf("ssh: %+v", res)
		}
	})
}
