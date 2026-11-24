// Package hostkey is the key that proves to clients that they reached this Cave: an Ed25519 key
// made at the first start and kept, in OpenSSH's format, in the configuration folder
// (docs/SPEC.md §9.10).
package hostkey

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"io"
	"path/filepath"
	"strings"

	"github.com/rm4n0s/cave/internal/core/storage"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The Tags of this package's errors.
const (
	TagParseFailed     = "HostKeyParseFailed"
	TagTypeUnsupported = "HostKeyTypeUnsupported"
	TagGenerateFailed  = "HostKeyGenerateFailed"
	TagEncodeFailed    = "HostKeyEncodeFailed"
)

// The names of the host key's files in the configuration folder.
const (
	// PrivateFileName holds the private key, in OpenSSH's private key format, with mode 0600.
	PrivateFileName = "ssh_host_ed25519_key"
	// PublicFileName holds the public key as one authorized_keys line.
	PublicFileName = PrivateFileName + ".pub"
)

// comment is the comment of the key in its files.
const comment = "cave host key"

// maxFileBytes bounds the private key file; an Ed25519 key takes about 400 bytes.
const maxFileBytes = 64 << 10

// HostKey is Cave's host key.
type HostKey struct {
	signer ssh.Signer
}

// Signer returns the key as the SSH server uses it.
func (k *HostKey) Signer() ssh.Signer {
	return k.signer
}

// Fingerprint returns OpenSSH's "SHA256:..." fingerprint of the public key: what a client shows
// at its first connection.
func (k *HostKey) Fingerprint() string {
	return ssh.FingerprintSHA256(k.signer.PublicKey())
}

// AuthorizedKey returns the public key as "ssh-ed25519 <base64>", the form of a known_hosts
// line after the host's name.
func (k *HostKey) AuthorizedKey() string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(k.signer.PublicKey())))
}

// LoadOrCreate reads the host key of the configuration folder, and makes one when there is
// none. A key file that cannot be read or is not an Ed25519 private key is an error: Cave never
// replaces a host key by itself, because every client would then be told that the host changed.
func LoadOrCreate(ctx context.Context, dirs storage.Dirs) (*HostKey, error) {
	key, err := load(ctx, dirs)
	if errors.Is(err, storage.ErrNotExist) {
		return create(ctx, dirs, rand.Reader)
	}
	return key, err
}

// Create makes a new host key and puts it in place of the one of the configuration folder, if
// there is one.
func Create(ctx context.Context, dirs storage.Dirs) (*HostKey, error) {
	return create(ctx, dirs, rand.Reader)
}

// create is Create with a given source of randomness.
func create(ctx context.Context, dirs storage.Dirs, random io.Reader) (*HostKey, error) {
	_, private, err := ed25519.GenerateKey(random)
	if err != nil {
		return nil, errors.NewErr(TagGenerateFailed, err)
	}
	block, err := ssh.MarshalPrivateKey(private, comment)
	if err != nil {
		return nil, errors.NewErr(TagEncodeFailed, err)
	}
	if err := storage.WriteFileAtomic(ctx, filepath.Join(dirs.Config, PrivateFileName), pem.EncodeToMemory(block)); err != nil {
		return nil, err
	}
	// Reading it back proves that what is on the disk is the key, and writes the public file.
	return load(ctx, dirs)
}

// load reads the host key and writes its public file when that is missing or differs.
func load(ctx context.Context, dirs storage.Dirs) (*HostKey, error) {
	path := filepath.Join(dirs.Config, PrivateFileName)
	data, err := storage.ReadFile(ctx, path, maxFileBytes)
	if err != nil {
		return nil, err
	}
	signer, err := ssh.ParsePrivateKey(data)
	if err != nil {
		// The library's error says what is wrong with the file's format and holds no key data.
		return nil, errors.NewErr(TagParseFailed, err).SetMetadata("path", path)
	}
	if keyType := signer.PublicKey().Type(); keyType != ssh.KeyAlgoED25519 {
		return nil, errors.New(TagTypeUnsupported, "the host key must be an Ed25519 key", "path", path, "type", keyType)
	}
	key := &HostKey{signer: signer}

	publicPath := filepath.Join(dirs.Config, PublicFileName)
	want := key.AuthorizedKey() + " " + comment + "\n"
	have, err := storage.ReadFile(ctx, publicPath, maxFileBytes)
	if err != nil && !errors.Is(err, storage.ErrNotExist) {
		return nil, err
	}
	if string(have) != want {
		if err := storage.WriteFileAtomic(ctx, publicPath, []byte(want)); err != nil {
			return nil, err
		}
	}
	return key, nil
}
