package app

import (
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/archtest"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// moduleRoot returns the folder with go.mod.
func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the test")
		}
		dir = parent
	}
}

func TestMessage(t *testing.T) {
	f := newFixture(t, nil)
	a := f.open(func(o *Options) { o.NoStart = true })
	alice, err := a.AddPerson(ctx, "alice")
	if err != nil {
		t.Fatal(err)
	}
	bobKey := f.person(a, "bob")
	own := sshtest.NewKey(t, sshtest.Ed25519)
	private, err := os.ReadFile(own.PrivatePath)
	if err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(t.TempDir(), "big")
	if err := os.WriteFile(big, make([]byte, 1<<20+1), 0o600); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(t.TempDir(), "missing")

	// A port that somebody else holds, for the server's failure to listen.
	held, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = held.Close() })
	heldPort := held.Addr().(*net.TCPAddr).Port
	if err := a.Set(ctx, "server.port", heldPort); err != nil {
		t.Fatal(err)
	}
	listenErr := a.StartServer(ctx)
	if listenErr == nil {
		t.Fatal("the server listened on a port that is taken")
	}

	// A second application on the same folders.
	_, alreadyRunning := Open(ctx, f.options())

	_, flagsPort := ParseFlags("tcave", []string{"--port", "0"})
	_, flagsArgument := ParseFlags("tcave", []string{"extra"})
	_, flagsUnknown := ParseFlags("tcave", []string{"--nope"})
	_, flagsHelp := ParseFlags("tcave", []string{"--help"})
	_, flagsPage := ParseFlags("tcave", []string{"--page", "home"})
	_, root := Open(ctx, func() Options { o := f.options(); o.IsRoot = func() bool { return true }; return o }())

	_, badName := a.AddPerson(ctx, "-bad")
	_, takenName := a.AddPerson(ctx, "ALICE")
	_, notFoundKeyText := a.AddKeys(ctx, alice.ID, "")
	_, assigned := a.AddKeys(ctx, alice.ID, bobKey.AuthorizedKey)
	_, privateKey := a.AddKeys(ctx, alice.ID, string(private))
	_, notAKey := a.AddKeys(ctx, alice.ID, "# one\nssh-ed25519 not-base64\n")
	_, withOptions := a.AddKeys(ctx, alice.ID, `command="ls" `+own.AuthorizedKey)
	_, tooLarge := a.ImportKeys(ctx, alice.ID, big)
	_, noFile := a.ImportKeys(ctx, alice.ID, missing)

	tests := []struct {
		name string
		err  error
		want string
	}{
		{"no error", nil, ""},
		{"a bad name", badName, "A name has 1 to 32 letters, digits, dots, underscores or hyphens, and does not start with a dot or a hyphen."},
		{"a taken name", takenName, "There is already a person named alice."},
		{"a person that is not there", a.RenamePerson(ctx, "p_0000000000000000", "x"), "There is no such person any more."},
		{"a key that is not there", a.RemoveKey(ctx, alice.ID, "k_0000000000000000"), "This person has no such key any more."},
		{"no key in the text", notFoundKeyText, "There is no public key in the text. Paste the contents of a .pub file."},
		{"a key somebody has", assigned, "This key (" + ssh.FingerprintSHA256(bobKey.Public) + ") already belongs to bob."},
		{"a private key", privateKey, "This is a private key. Paste the public key instead: the file that ends in .pub."},
		{"text that is not a key", notAKey, "Line 2: this is not a public key. Paste the contents of a .pub file."},
		{"a key with options", withOptions, "Line 1: a key with options before its type, such as command=\"…\", is not supported. Paste only the key."},
		{"a file that is too large", tooLarge, "The file " + big + " is too large (the limit is 1048576 bytes)."},
		{"a file that is not there", noFile, "The file " + missing + " does not exist."},
		{"an attempt that is not there", a.RemoveAttempt(ctx, "a_0000000000000000"), "That entry is no longer in the list."},
		{"a connection that is not there", a.Disconnect(ctx, "nobody"), "That connection has already ended."},
		{"an unknown setting", a.Set(ctx, "no.such", 1), "There is no setting named no.such."},
		{"a setting out of range", a.Set(ctx, "server.port", 70000), "The value is not acceptable for server.port: it must be between 1 and 65535."},
		{"a port that is taken", listenErr, fmt.Sprintf("Cave cannot listen on 127.0.0.1:%[1]d: listen tcp 127.0.0.1:%[1]d: bind: address already in use. Choose another port in Settings.", heldPort)},
		{"a second Cave", alreadyRunning, "Cave is already running on these folders. Quit the other one first, or use other folders with --config-dir and --state-dir."},
		{"root", root, "Cave does not run as root, because every person it lets in would get a root shell. Start it as your own user."},
		{"a port flag out of range", flagsPort, "A port is a number from 1 to 65535, not 0."},
		{"an argument", flagsArgument, "Cave takes flags only, not extra. Run with --help to see them."},
		{"an unknown flag", flagsUnknown, "The command line is not right: flag provided but not defined: -nope. Run with --help to see the flags."},
		{"a page that does not exist", flagsPage, "There is no page named home. The pages are overview, people, attempts, connections and settings."},
		{"help", flagsHelp, "Run with --help to see the flags."},
		{"an error of another kind", io.EOF, "EOF"},
		{"a Tag without a sentence", errors.New("NoSentenceForThis", "what went wrong", "path", "/x"), "what went wrong"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Message(tt.err); got != tt.want {
				t.Errorf("Message = %q\n     want %q", got, tt.want)
			}
		})
	}
}

// plainError is an error that is not of this program's kind, like the ones of the system.
type plainError string

func (e plainError) Error() string { return string(e) }

func TestMessageIsSafe(t *testing.T) {
	hostile := "a\x1b[31mb\u202Ec"
	tests := []struct {
		name string
		err  error
	}{
		{"a value in a sentence", errors.New("PersonNameTaken", "x", "takenBy", hostile)},
		{"the cause in a sentence", errors.NewErr("LogOpenFailed", plainError(hostile)).SetMetadata("path", "/x")},
		{"a Tag without a sentence", errors.New("NoSentenceForThis", hostile)},
		{"an error of another kind", plainError(hostile)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Message(tt.err)
			for _, r := range got {
				if r < ' ' || r == 0x7f || r == '\u202E' {
					t.Fatalf("Message = %q holds the character %U", got, r)
				}
			}
			if !strings.Contains(got, "a\uFFFD[31mb\uFFFDc") {
				t.Errorf("Message = %q does not hold the text with its unsafe characters replaced", got)
			}
		})
	}
}

func TestMessageIsCutShort(t *testing.T) {
	long := strings.Repeat("x", 1000)
	for name, err := range map[string]error{
		"a value":     errors.New("PersonNameTaken", "x", "takenBy", long),
		"a message":   errors.New("NoSentenceForThis", long),
		"another one": io.ErrUnexpectedEOF,
	} {
		if got := Message(err); len([]rune(got)) > 200 {
			t.Errorf("%s: the sentence has %d characters", name, len([]rune(got)))
		}
	}
}

// tagsWithSentences are the folders whose errors a user interface can show: what the commands,
// the opening of the application, the server's listening, the settings, the log and the files
// give back. The Tags of sessions, SFTP, forwarding and the pty are only logged.
var tagsWithSentences = []string{
	"internal/app/",
	"internal/core/attempts/",
	"internal/core/hostkey/",
	"internal/core/logging/",
	"internal/core/people/",
	"internal/core/server/",
	"internal/core/settings/",
	"internal/core/storage/",
}

func TestEveryTagHasAMessage(t *testing.T) {
	report, err := archtest.ScanTags(moduleRoot(t))
	if err != nil {
		t.Fatal(err)
	}
	all := map[string]bool{}
	var missing []string
	for _, use := range report.Uses {
		all[use.Tag] = true
		for _, prefix := range tagsWithSentences {
			if strings.HasPrefix(use.File, prefix) {
				if _, ok := messages[use.Tag]; !ok {
					missing = append(missing, fmt.Sprintf("%s (%s:%d)", use.Tag, use.File, use.Line))
				}
				break
			}
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("these Tags have no sentence in messages:\n\t%s", strings.Join(missing, "\n\t"))
	}
	var stale []string
	for name := range messages {
		if !all[name] {
			stale = append(stale, name)
		}
	}
	sort.Strings(stale)
	if len(stale) > 0 {
		t.Errorf("these sentences are for Tags that no error has any more: %v", stale)
	}
}

// placeholders returns the keys in the braces of a sentence.
func placeholders(sentence string) []string {
	var keys []string
	for {
		open := strings.IndexByte(sentence, '{')
		if open < 0 {
			return keys
		}
		end := strings.IndexByte(sentence[open:], '}')
		if end < 0 {
			return append(keys, "<unclosed>")
		}
		keys = append(keys, sentence[open+1:open+end])
		sentence = sentence[open+end+1:]
	}
}

func TestMessagesHaveNoLeftoverPlaceholder(t *testing.T) {
	for name, sentence := range messages {
		appErr := errors.NewErr(name, io.EOF)
		for _, key := range placeholders(sentence) {
			if key == "" || strings.ContainsAny(key, " {") || key == "<unclosed>" {
				t.Errorf("%s: a placeholder is not well formed: %q", name, sentence)
			}
			_ = appErr.SetMetadata(key, "VALUE")
		}
		got := Message(appErr)
		if strings.ContainsAny(got, "{}") || strings.Contains(got, "?") != strings.Contains(sentence, "?") {
			t.Errorf("%s: filled in, the sentence is %q", name, got)
		}
		if !strings.HasSuffix(got, ".") && !strings.HasSuffix(got, "?") {
			t.Errorf("%s: the sentence does not end with a full stop: %q", name, got)
		}
		if sentence != strings.TrimSpace(sentence) || strings.Contains(sentence, "  ") {
			t.Errorf("%s: odd spacing in %q", name, sentence)
		}
	}
	// A key the error does not have shows as a question mark.
	if got := Message(errors.New("PersonNameTaken", "x")); got != "There is already a person named ?." {
		t.Errorf("a missing value gives %q", got)
	}
}
