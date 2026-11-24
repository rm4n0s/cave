// Package people holds the people who may connect to Cave and their public keys: it parses and
// validates keys, keeps the people in their file, and answers the question authentication asks:
// is this key a key of the person with this login name? (docs/SPEC.md §7)
package people

import (
	"crypto/rsa"
	"encoding/base64"
	"encoding/binary"
	"strings"

	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// The Tags of the errors of parsing keys.
const (
	TagKeyIsPrivate          = "KeyIsPrivate"
	TagKeyOptionsUnsupported = "KeyOptionsUnsupported"
	TagKeyTypeUnsupported    = "KeyTypeUnsupported"
	TagKeyTooWeak            = "KeyTooWeak"
	TagKeyParseFailed        = "KeyParseFailed"
	TagKeyTextEmpty          = "KeyTextEmpty"
)

// Limits of keys.
const (
	// minRSABits is the smallest RSA key Cave accepts.
	minRSABits = 2048
	// MaxCommentLength is how many characters a key's comment keeps.
	MaxCommentLength = 64
	// maxTypeLength bounds a key type's name in an error's metadata.
	maxTypeLength = 64
)

// acceptedTypes are the key types Cave accepts (docs/SPEC.md §7.2).
var acceptedTypes = map[string]bool{
	ssh.KeyAlgoED25519:    true,
	ssh.KeyAlgoECDSA256:   true,
	ssh.KeyAlgoECDSA384:   true,
	ssh.KeyAlgoECDSA521:   true,
	ssh.KeyAlgoRSA:        true,
	ssh.KeyAlgoSKED25519:  true,
	ssh.KeyAlgoSKECDSA256: true,
}

// ParsedKey is a public key read from text, before it belongs to anybody.
type ParsedKey struct {
	// Public is the key.
	Public ssh.PublicKey
	// Comment is the text after the key on its line, made safe to show and cut to
	// MaxCommentLength characters.
	Comment string
}

// lineProblem is what is wrong with one line of key text.
type lineProblem int

const (
	lineOK lineProblem = iota
	lineOptions
	lineUnsupported
	lineWeak
	lineUnparsable
)

// ParseKeys reads public keys from text in the authorized_keys format: one key per line as
// "<type> <base64> [comment]"; blank lines and lines that start with "#" are skipped. Every line
// must be an acceptable key, or the whole text is refused with the line's number in the error's
// metadata. A key that appears twice is returned once. The private key text of a user who pasted
// the wrong file is never put in an error.
func ParseKeys(text string) ([]ParsedKey, error) {
	if strings.Contains(text, "PRIVATE KEY-----") {
		return nil, errors.New(TagKeyIsPrivate, "this is a private key; paste the public key, the file that ends in .pub")
	}
	var keys []ParsedKey
	seen := map[string]bool{}
	for i, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(strings.TrimSuffix(line, "\r"))
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, keyType, problem := parseLine(line)
		number := i + 1
		switch problem {
		case lineOptions:
			return nil, errors.New(TagKeyOptionsUnsupported, "a key with options before its type is not supported", "line", number)
		case lineUnsupported:
			return nil, errors.New(TagKeyTypeUnsupported, "this type of key is not supported", "line", number, "type", keyType)
		case lineWeak:
			return nil, errors.New(TagKeyTooWeak, "an RSA key must have at least 2048 bits", "line", number, "minBits", minRSABits)
		case lineUnparsable:
			return nil, errors.New(TagKeyParseFailed, "this is not a public key", "line", number)
		}
		blob := string(key.Public.Marshal())
		if seen[blob] {
			continue
		}
		seen[blob] = true
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil, errors.New(TagKeyTextEmpty, "there is no public key in the text")
	}
	return keys, nil
}

// parseLine reads the one key of a line that is not blank or a comment. With a problem, it
// returns the key's type when the line says it.
func parseLine(line string) (ParsedKey, string, lineProblem) {
	public, comment, options, rest, err := ssh.ParseAuthorizedKey([]byte(line))
	if err != nil || len(rest) != 0 {
		// The library does not know every type; a line whose key data names a type that Cave
		// does not accept is an unsupported key, not garbage.
		if keyType, ok := declaredType(line); ok && !acceptedTypes[keyType] {
			return ParsedKey{}, keyType, lineUnsupported
		}
		return ParsedKey{}, "", lineUnparsable
	}
	if len(options) != 0 {
		return ParsedKey{}, "", lineOptions
	}
	keyType := public.Type()
	if !acceptedTypes[keyType] {
		return ParsedKey{}, sanitize.Display(keyType, maxTypeLength), lineUnsupported
	}
	if keyType == ssh.KeyAlgoRSA {
		crypto, ok := public.(ssh.CryptoPublicKey)
		if !ok {
			return ParsedKey{}, "", lineUnparsable
		}
		rsaKey, ok := crypto.CryptoPublicKey().(*rsa.PublicKey)
		if !ok {
			return ParsedKey{}, "", lineUnparsable
		}
		if rsaKey.N.BitLen() < minRSABits {
			return ParsedKey{}, keyType, lineWeak
		}
	}
	return ParsedKey{Public: public, Comment: cleanComment(comment)}, keyType, lineOK
}

// declaredType returns the key type that the base64 data of a line starts with, when the line
// is "<type> <base64> ..." and the two agree.
func declaredType(line string) (string, bool) {
	fields := strings.Fields(line)
	if len(fields) < 2 {
		return "", false
	}
	data, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil || len(data) < 4 {
		return "", false
	}
	n := binary.BigEndian.Uint32(data)
	if n == 0 || n > maxTypeLength || int(n) > len(data)-4 {
		return "", false
	}
	keyType := string(data[4 : 4+n])
	if keyType != fields[0] {
		return "", false
	}
	return sanitize.Display(keyType, maxTypeLength), true
}

// cleanComment makes a comment safe to show and cuts it.
func cleanComment(comment string) string {
	return sanitize.Display(strings.TrimSpace(comment), MaxCommentLength)
}

// authorizedKey formats a key as "<type> <base64>", the form the file keeps.
func authorizedKey(public ssh.PublicKey) string {
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))
}
