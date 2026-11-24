package server

import (
	"strings"
	"testing"
	"unicode"
	"unicode/utf8"

	"golang.org/x/crypto/ssh"

	"github.com/rm4n0s/cave/internal/core/attempts"
	"github.com/rm4n0s/cave/internal/core/sshtest"
)

func TestOnlyPublicKeyIsOffered(t *testing.T) {
	f := newFixture(t)
	f.person("alice")
	stranger := sshtest.NewKey(t, sshtest.Ed25519)
	var passwordAsked, interactiveAsked bool
	config := f.config("alice", stranger)
	// The client offers every method the library has; it uses one only when the server lists it.
	config.Auth = []ssh.AuthMethod{
		ssh.PasswordCallback(func() (string, error) { passwordAsked = true; return "hunter2", nil }),
		ssh.KeyboardInteractive(func(string, string, []string, []bool) ([]string, error) {
			interactiveAsked = true
			return nil, nil
		}),
		ssh.PublicKeys(stranger.Signer),
	}
	_, err := f.dial(config)
	if err == nil {
		t.Fatal("a stranger got in")
	}
	if passwordAsked || interactiveAsked {
		t.Errorf("the server asked for a password (%v) or for keyboard-interactive (%v)", passwordAsked, interactiveAsked)
	}
	// "none" is how a client learns the methods, and publickey is the only one that follows.
	if !strings.Contains(err.Error(), "attempted methods [none publickey]") {
		t.Errorf("the client's error is %q, want only the methods none and publickey", err)
	}
	// A client that can offer nothing but a password is not even tried.
	config = f.config("alice")
	config.Auth = []ssh.AuthMethod{ssh.Password("hunter2")}
	_, err = f.dial(config)
	if err == nil || !strings.Contains(err.Error(), "attempted methods [none], no supported methods remain") {
		t.Errorf("a client with only a password got %v, want 'attempted methods [none], no supported methods remain'", err)
	}
	entries := f.waitTotal(2)
	for _, e := range entries {
		if e.Outcome == attempts.Accepted {
			t.Errorf("an attempt was accepted: %+v", e)
		}
	}
}

func TestAHostileClientVersionIsBoundedAndMadeSafe(t *testing.T) {
	tests := []struct {
		name    string
		version string
		// handshake says that the library refuses the version line itself, before Cave sees it.
		handshake bool
	}{
		{"an escape sequence", "SSH-2.0-\x1b[31mevil\x07", true},
		{"a newline in it", "SSH-2.0-evil\nsecond line", true},
		{"a text direction override and a zero-width space", "SSH-2.0-a\u202eb\u200bc", false},
		{"as long as the protocol allows", "SSH-2.0-" + strings.Repeat("A", 240), false},
		{"long, with the override in it", "SSH-2.0-" + strings.Repeat("\u202e", 50) + strings.Repeat("B", 90), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t)
			f.person("alice")
			stranger := sshtest.NewKey(t, sshtest.Ed25519)
			config := f.config("alice", stranger)
			config.ClientVersion = tt.version
			if _, err := f.dial(config); err == nil {
				t.Fatal("a stranger got in")
			}
			entries := f.waitTotal(1)
			got := entries[0].ClientVersion
			if tt.handshake {
				// Nothing of the line is kept: the attempt is a handshake that failed.
				if entries[0].Outcome != attempts.HandshakeFailed || got != "" {
					t.Errorf("the entry is %+v, want a failed handshake with no version", entries[0])
				}
			} else if got == "" || utf8.RuneCountInString(got) > 64 {
				t.Errorf("the client version is %q (%d characters), want it there and cut to 64", got, utf8.RuneCountInString(got))
			}
			for _, r := range got {
				if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
					t.Errorf("the client version %q holds the character %U", got, r)
				}
			}
			log := f.log.String()
			for _, bad := range []string{"\x1b", "\u202e", "\u200b", "\x07"} {
				if strings.Contains(log, bad) {
					t.Errorf("the log holds %q as the client sent it:\n%s", bad, log)
				}
			}
		})
	}
}

func TestAnIdentificationLineThatIsTooLongIsAHandshakeFailure(t *testing.T) {
	f := newFixture(t)
	f.person("alice")
	conn := f.raw("")
	if banner(t, conn) == "" {
		t.Fatal("the server sent no identification")
	}
	// The protocol allows 255 characters; a client that sends far more is cut off, and the
	// server does not keep what it sent.
	if _, err := conn.Write([]byte("SSH-2.0-" + strings.Repeat("B", 100_000) + "\r\n")); err != nil {
		t.Logf("the server closed before all of it was sent: %v", err) // fine: it stopped reading
	}
	waitClosed(t, conn)
	entries := f.waitTotal(1)
	if entries[0].Outcome != attempts.HandshakeFailed || utf8.RuneCountInString(entries[0].ClientVersion) > 64 {
		t.Errorf("the entry is %+v", entries[0])
	}
}
