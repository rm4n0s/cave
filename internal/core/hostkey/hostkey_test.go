package hostkey

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/cave/internal/core/storage/storagetest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

var ctx = context.Background()

// paths returns the private and public files of the folders.
func paths(dirs storage.Dirs) (private, public string) {
	return filepath.Join(dirs.Config, PrivateFileName), filepath.Join(dirs.Config, PublicFileName)
}

// read returns a file's content.
func read(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// mode fails the test unless the file has the mode, on platforms with mode bits.
func mode(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != want {
		t.Errorf("mode of %s = %o, want %o", filepath.Base(path), got, want)
	}
}

func TestLoadOrCreate(t *testing.T) {
	dirs := storagetest.Isolate(t)
	private, public := paths(dirs)

	created, err := LoadOrCreate(ctx, dirs)
	if err != nil {
		t.Fatal(err)
	}
	if got := created.Signer().PublicKey().Type(); got != "ssh-ed25519" {
		t.Errorf("the key's type is %q", got)
	}
	if !strings.HasPrefix(created.Fingerprint(), "SHA256:") || len(created.Fingerprint()) != len("SHA256:")+43 {
		t.Errorf("Fingerprint() = %q", created.Fingerprint())
	}
	if !strings.HasPrefix(created.AuthorizedKey(), "ssh-ed25519 AAAAC3NzaC1lZDI1NTE5") || strings.ContainsAny(created.AuthorizedKey(), "\n") {
		t.Errorf("AuthorizedKey() = %q", created.AuthorizedKey())
	}

	// The private file is OpenSSH's format with mode 0600; the public file is the line.
	mode(t, private, 0o600)
	mode(t, public, 0o600)
	if content := read(t, private); !strings.HasPrefix(content, "-----BEGIN OPENSSH PRIVATE KEY-----\n") {
		t.Errorf("the private file starts with %q", content[:min(40, len(content))])
	}
	if got, want := read(t, public), created.AuthorizedKey()+" cave host key\n"; got != want {
		t.Errorf("the public file is %q, want %q", got, want)
	}
	if names, _ := os.ReadDir(dirs.Config); len(names) != 2 {
		t.Errorf("the configuration folder has %d files, want the two of the key", len(names))
	}

	// The key signs, and its public key verifies.
	signature, err := created.Signer().Sign(rand.Reader, []byte("hello"))
	if err != nil {
		t.Fatal(err)
	}
	if err := created.Signer().PublicKey().Verify([]byte("hello"), signature); err != nil {
		t.Errorf("the key's signature does not verify: %v", err)
	}

	// Loading again gives the same key and writes nothing.
	before, _ := os.Stat(private)
	for i := 0; i < 3; i++ {
		loaded, err := LoadOrCreate(ctx, dirs)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Fingerprint() != created.Fingerprint() || loaded.AuthorizedKey() != created.AuthorizedKey() {
			t.Errorf("load %d gave another key", i)
		}
	}
	after, _ := os.Stat(private)
	if !after.ModTime().Equal(before.ModTime()) {
		t.Error("loading rewrote the private file")
	}
}

func TestCreateReplaces(t *testing.T) {
	dirs := storagetest.Isolate(t)
	private, public := paths(dirs)
	first, err := LoadOrCreate(ctx, dirs)
	if err != nil {
		t.Fatal(err)
	}
	second, err := Create(ctx, dirs)
	if err != nil {
		t.Fatal(err)
	}
	if second.Fingerprint() == first.Fingerprint() {
		t.Error("the new key is the old key")
	}
	loaded, err := LoadOrCreate(ctx, dirs)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.Fingerprint() != second.Fingerprint() {
		t.Error("the file does not hold the new key")
	}
	if got := read(t, public); !strings.HasPrefix(got, second.AuthorizedKey()+" ") {
		t.Error("the public file does not hold the new key")
	}
	mode(t, private, 0o600)

	// Create on empty folders makes the key too.
	if _, err := Create(ctx, storagetest.Isolate(t)); err != nil {
		t.Fatal(err)
	}
}

func TestThePublicFileIsRepaired(t *testing.T) {
	dirs := storagetest.Isolate(t)
	_, public := paths(dirs)
	key, err := LoadOrCreate(ctx, dirs)
	if err != nil {
		t.Fatal(err)
	}
	want := read(t, public)
	for name, damage := range map[string]func() error{
		"missing": func() error { return os.Remove(public) },
		"wrong":   func() error { return os.WriteFile(public, []byte("ssh-ed25519 AAAA other key\n"), 0o600) },
	} {
		if err := damage(); err != nil {
			t.Fatal(err)
		}
		loaded, err := LoadOrCreate(ctx, dirs)
		if err != nil {
			t.Fatal(err)
		}
		if loaded.Fingerprint() != key.Fingerprint() {
			t.Errorf("%s public file: the key changed", name)
		}
		if got := read(t, public); got != want {
			t.Errorf("%s public file: now %q, want %q", name, got, want)
		}
	}
}

func TestBrokenKeyFilesAreErrorsAndAreNotReplaced(t *testing.T) {
	ecdsaKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(ecdsaKey, "another kind")
	if err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name      string
		content   []byte
		wantRoute string
		wantMeta  map[string]any
	}{
		{"empty", nil, "LoadOrCreate->load.HostKeyParseFailed", nil},
		{"not a key", []byte("this is not a key\n"), "LoadOrCreate->load.HostKeyParseFailed", nil},
		{"a cut key", []byte("-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEA\n-----END OPENSSH PRIVATE KEY-----\n"), "LoadOrCreate->load.HostKeyParseFailed", nil},
		{"a public key", []byte("ssh-ed25519 AAAAC3NzaC1lZDI1NTE5AAAAIBla host\n"), "LoadOrCreate->load.HostKeyParseFailed", nil},
		{"a key of another type", pem.EncodeToMemory(block), "LoadOrCreate->load.HostKeyTypeUnsupported", map[string]any{"type": "ecdsa-sha2-nistp256"}},
		{"too large", bytes.Repeat([]byte("x"), maxFileBytes+1), "LoadOrCreate->load->ReadFile.FileTooLarge", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dirs := storagetest.Isolate(t)
			private, public := paths(dirs)
			if err := os.WriteFile(private, tt.content, 0o600); err != nil {
				t.Fatal(err)
			}
			_, err := LoadOrCreate(ctx, dirs)
			meta := errtest.Route(t, err, tt.wantRoute)
			if meta["path"] != private {
				t.Errorf("metadata = %v", meta)
			}
			for key, want := range tt.wantMeta {
				if meta[key] != want {
					t.Errorf("metadata[%q] = %v, want %v", key, meta[key], want)
				}
			}
			if got := read(t, private); got != string(tt.content) {
				t.Error("the broken key file was changed")
			}
			if _, err := os.Stat(public); !os.IsNotExist(err) {
				t.Error("a public file was written for a broken key")
			}
		})
	}

	t.Run("a key file that cannot be read", func(t *testing.T) {
		if runtime.GOOS == "windows" || os.Geteuid() == 0 {
			t.Skip("needs mode bits that stop the reader")
		}
		dirs := storagetest.Isolate(t)
		private, _ := paths(dirs)
		if _, err := LoadOrCreate(ctx, dirs); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(private, 0); err != nil {
			t.Fatal(err)
		}
		_, err := LoadOrCreate(ctx, dirs)
		errtest.Route(t, err, "LoadOrCreate->load->ReadFile.StorageFileOpenFailed")
	})
}

// failingReader is a source of randomness that has none.
type failingReader struct{}

func (failingReader) Read([]byte) (int, error) { return 0, io.ErrUnexpectedEOF }

func TestCreateErrors(t *testing.T) {
	t.Run("no randomness", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		_, err := create(ctx, dirs, failingReader{})
		errtest.Route(t, err, "create.HostKeyGenerateFailed")
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Errorf("the cause is %v", errors.Unwrap(err))
		}
		if names, _ := os.ReadDir(dirs.Config); len(names) != 0 {
			t.Error("a file was written")
		}
	})
	t.Run("the folder is gone", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		if err := os.RemoveAll(dirs.Config); err != nil {
			t.Fatal(err)
		}
		_, err := LoadOrCreate(ctx, dirs)
		errtest.Route(t, err, "LoadOrCreate->create->WriteFileAtomic->writeFileAtomic.StorageWriteCreateFailed")
		_, err = Create(ctx, dirs)
		errtest.Route(t, err, "Create->create->WriteFileAtomic->writeFileAtomic.StorageWriteCreateFailed")
	})
	t.Run("a cancelled context", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		cancelled, cancel := context.WithCancel(ctx)
		cancel()
		_, err := LoadOrCreate(cancelled, dirs)
		errtest.Route(t, err, "LoadOrCreate->load->ReadFile.StorageReadCancelled")
		_, err = Create(cancelled, dirs)
		errtest.Route(t, err, "Create->create->WriteFileAtomic->writeFileAtomic.StorageWriteCancelled")
	})
	t.Run("a folder where the public file should be", func(t *testing.T) {
		dirs := storagetest.Isolate(t)
		_, public := paths(dirs)
		// A folder with something in it where the public file goes.
		if err := os.MkdirAll(filepath.Join(public, "in-the-way"), 0o700); err != nil {
			t.Fatal(err)
		}
		_, err := LoadOrCreate(ctx, dirs)
		errtest.Route(t, err, "LoadOrCreate->create->load->ReadFile.StorageFileReadFailed")
	})
}

// sshKeygen runs OpenSSH's ssh-keygen, or skips the test when it is not installed.
func sshKeygen(t *testing.T, args ...string) string {
	t.Helper()
	path, err := exec.LookPath("ssh-keygen")
	if err != nil {
		t.Skip("ssh-keygen is not installed")
	}
	runCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	out, err := exec.CommandContext(runCtx, path, args...).CombinedOutput()
	if err != nil {
		t.Fatalf("ssh-keygen %v: %v\n%s", args, err, out)
	}
	return string(out)
}

func TestOpenSSHAgreesWithTheKey(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the test runs OpenSSH's ssh-keygen of the host")
	}
	dirs := storagetest.Isolate(t)
	private, public := paths(dirs)
	key, err := LoadOrCreate(ctx, dirs)
	if err != nil {
		t.Fatal(err)
	}
	// ssh-keygen prints "256 SHA256:... cave host key (ED25519)" for the public file.
	listing := sshKeygen(t, "-l", "-f", public)
	fields := strings.Fields(listing)
	if len(fields) < 2 || fields[0] != "256" || fields[1] != key.Fingerprint() || !strings.Contains(listing, "(ED25519)") {
		t.Errorf("ssh-keygen -l says %q, Cave says %q", strings.TrimSpace(listing), key.Fingerprint())
	}
	// ssh-keygen reads the private file and derives the same public key.
	derived := strings.Fields(sshKeygen(t, "-y", "-f", private))
	if len(derived) < 2 || derived[0]+" "+derived[1] != key.AuthorizedKey() {
		t.Errorf("ssh-keygen -y gives %q, Cave has %q", strings.Join(derived, " "), key.AuthorizedKey())
	}
}
