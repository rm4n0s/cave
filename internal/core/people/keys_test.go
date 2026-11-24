package people

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/errors"
	"golang.org/x/crypto/ssh"
)

// testKey is a public key made for a test, with its authorized_keys line (without a comment).
type testKey struct {
	public ssh.PublicKey
	line   string
}

// fromPublic wraps a public key.
func fromPublic(t testing.TB, public ssh.PublicKey) testKey {
	t.Helper()
	return testKey{public: public, line: strings.TrimSpace(string(ssh.MarshalAuthorizedKey(public)))}
}

// fromCrypto wraps a key of the standard library.
func fromCrypto(t testing.TB, key any) testKey {
	t.Helper()
	public, err := ssh.NewPublicKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return fromPublic(t, public)
}

// fromWire parses a key from its wire encoding.
func fromWire(t testing.TB, wire []byte) testKey {
	t.Helper()
	public, err := ssh.ParsePublicKey(wire)
	if err != nil {
		t.Fatal(err)
	}
	return fromPublic(t, public)
}

// newEd25519 makes a new Ed25519 key.
func newEd25519(t testing.TB) testKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return fromCrypto(t, public)
}

// newECDSA makes a new ECDSA key on the curve.
func newECDSA(t testing.TB, curve elliptic.Curve) testKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(curve, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return fromCrypto(t, &private.PublicKey)
}

// newRSA makes a new RSA key of the size.
func newRSA(t testing.TB, bits int) testKey {
	t.Helper()
	private, err := rsa.GenerateKey(rand.Reader, bits)
	if err != nil {
		t.Fatal(err)
	}
	return fromCrypto(t, &private.PublicKey)
}

// newSKEd25519 makes the public key of a security key (FIDO) of the Ed25519 kind.
func newSKEd25519(t testing.TB) testKey {
	t.Helper()
	public, _, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return fromWire(t, ssh.Marshal(struct {
		Name        string
		Key         []byte
		Application string
	}{ssh.KeyAlgoSKED25519, public, "ssh:"}))
}

// newSKECDSA makes the public key of a security key (FIDO) of the ECDSA kind.
func newSKECDSA(t testing.TB) testKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	point, err := private.PublicKey.Bytes()
	if err != nil {
		t.Fatal(err)
	}
	return fromWire(t, ssh.Marshal(struct {
		Name        string
		Curve       string
		Key         []byte
		Application string
	}{ssh.KeyAlgoSKECDSA256, "nistp256", point, "ssh:"}))
}

// dsaLine is the line of a DSA key: its numbers are not a real key, only of the right sizes.
func dsaLine() string {
	p := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 1023), big.NewInt(1))
	q := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 159), big.NewInt(1))
	wire := ssh.Marshal(struct {
		Name       string
		P, Q, G, Y *big.Int
	}{"ssh-dss", p, q, big.NewInt(2), big.NewInt(3)})
	return "ssh-dss " + b64(wire)
}

// b64 encodes key data as an authorized_keys line has it.
func b64(data []byte) string {
	return base64.StdEncoding.EncodeToString(data)
}

// certificateLine is the line of an OpenSSH certificate for a new key.
func certificateLine(t testing.TB) string {
	t.Helper()
	_, caPrivate, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	ca, err := ssh.NewSignerFromKey(caPrivate)
	if err != nil {
		t.Fatal(err)
	}
	cert := &ssh.Certificate{Key: newEd25519(t).public, CertType: ssh.UserCert, KeyId: "ada", ValidPrincipals: []string{"ada"}, ValidBefore: ssh.CertTimeInfinity}
	if err := cert.SignCert(rand.Reader, ca); err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(ssh.MarshalAuthorizedKey(cert)))
}

// unknownTypeLine is the line of a key of a type that nothing knows.
func unknownTypeLine(name string) string {
	return name + " " + b64(ssh.Marshal(struct {
		Name string
		Data []byte
	}{name, []byte("some key data")}))
}

// privateKeyText is the private key file of a new key, which a user may paste by mistake.
func privateKeyText(t testing.TB) string {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "ada@laptop")
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(block))
}

func TestParseKeysAcceptsEveryType(t *testing.T) {
	tests := []struct {
		name     string
		key      testKey
		wantType string
	}{
		{"Ed25519", newEd25519(t), "ssh-ed25519"},
		{"ECDSA P-256", newECDSA(t, elliptic.P256()), "ecdsa-sha2-nistp256"},
		{"ECDSA P-384", newECDSA(t, elliptic.P384()), "ecdsa-sha2-nistp384"},
		{"ECDSA P-521", newECDSA(t, elliptic.P521()), "ecdsa-sha2-nistp521"},
		{"RSA 2048", newRSA(t, 2048), "ssh-rsa"},
		{"security key Ed25519", newSKEd25519(t), "sk-ssh-ed25519@openssh.com"},
		{"security key ECDSA", newSKECDSA(t), "sk-ecdsa-sha2-nistp256@openssh.com"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := ParseKeys(tt.key.line + " ada@laptop\n")
			if err != nil {
				t.Fatal(err)
			}
			if len(keys) != 1 {
				t.Fatalf("%d keys, want 1", len(keys))
			}
			if got := keys[0].Public.Type(); got != tt.wantType {
				t.Errorf("type = %q, want %q", got, tt.wantType)
			}
			if string(keys[0].Public.Marshal()) != string(tt.key.public.Marshal()) {
				t.Error("the parsed key is another key")
			}
			if keys[0].Comment != "ada@laptop" {
				t.Errorf("comment = %q", keys[0].Comment)
			}
		})
	}
}

func TestParseKeysText(t *testing.T) {
	a, b, c := newEd25519(t), newEd25519(t), newECDSA(t, elliptic.P256())
	tests := []struct {
		name         string
		text         string
		wantKeys     []testKey
		wantComments []string
	}{
		{"one key without a comment", a.line, []testKey{a}, []string{""}},
		{"a comment with spaces", a.line + "   ada lovelace's  laptop  ", []testKey{a}, []string{"ada lovelace's  laptop"}},
		{"several lines", a.line + " one\n" + b.line + " two\n" + c.line + "\n", []testKey{a, b, c}, []string{"one", "two", ""}},
		{"blank lines and comments are skipped", "\n# my keys\n\n" + a.line + "\n   \n#" + b.line + "\n", []testKey{a}, []string{""}},
		{"Windows line ends", a.line + " one\r\n" + b.line + "\r\n", []testKey{a, b}, []string{"one", ""}},
		{"leading and trailing space", "  \t" + a.line + "\t \n", []testKey{a}, []string{""}},
		{"a key twice is one key", a.line + " first\n" + b.line + "\n" + a.line + " again\n", []testKey{a, b}, []string{"first", ""}},
		{"a hostile comment is made safe", a.line + " \x1b[31mred\u202E", []testKey{a}, []string{"\uFFFD[31mred\uFFFD"}},
		{"a long comment is cut", a.line + " " + strings.Repeat("x", 200), []testKey{a}, []string{strings.Repeat("x", 63) + "…"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := ParseKeys(tt.text)
			if err != nil {
				t.Fatal(err)
			}
			if len(keys) != len(tt.wantKeys) {
				t.Fatalf("%d keys, want %d", len(keys), len(tt.wantKeys))
			}
			for i, key := range keys {
				if string(key.Public.Marshal()) != string(tt.wantKeys[i].public.Marshal()) {
					t.Errorf("key %d is another key", i)
				}
				if key.Comment != tt.wantComments[i] {
					t.Errorf("comment %d = %q, want %q", i, key.Comment, tt.wantComments[i])
				}
			}
		})
	}
}

func TestParseKeysRefusals(t *testing.T) {
	good := newEd25519(t)
	weak := newRSA(t, 1024)
	tests := []struct {
		name      string
		text      string
		wantRoute string
		wantMeta  map[string]any
	}{
		{"empty text", "", "ParseKeys.KeyTextEmpty", nil},
		{"only blank lines and comments", "\n# nothing here\n   \n", "ParseKeys.KeyTextEmpty", nil},
		{"words", "hello there", "ParseKeys.KeyParseFailed", map[string]any{"line": 1}},
		{"one word", "ssh-ed25519", "ParseKeys.KeyParseFailed", map[string]any{"line": 1}},
		{"a type and bad base64", "ssh-ed25519 not*base64", "ParseKeys.KeyParseFailed", map[string]any{"line": 1}},
		{"a cut key", good.line[:len(good.line)-12], "ParseKeys.KeyParseFailed", map[string]any{"line": 1}},
		{"a type that is not the data's", "ssh-rsa " + strings.Fields(good.line)[1], "ParseKeys.KeyParseFailed", map[string]any{"line": 1}},
		{"a fingerprint instead of a key", "SHA256:" + strings.Repeat("A", 43), "ParseKeys.KeyParseFailed", map[string]any{"line": 1}},
		{"the second line is bad", good.line + "\nnot a key\n", "ParseKeys.KeyParseFailed", map[string]any{"line": 2}},
		{"the line number counts skipped lines", "# keys\n\n" + good.line + "\n\nbroken line", "ParseKeys.KeyParseFailed", map[string]any{"line": 5}},
		{"options before the type", `command="uptime",no-pty ` + good.line, "ParseKeys.KeyOptionsUnsupported", map[string]any{"line": 1}},
		{"a single option", "restrict " + good.line, "ParseKeys.KeyOptionsUnsupported", map[string]any{"line": 1}},
		{"an RSA key of 1024 bits", weak.line, "ParseKeys.KeyTooWeak", map[string]any{"line": 1, "minBits": 2048}},
		{"a DSA key", dsaLine(), "ParseKeys.KeyTypeUnsupported", map[string]any{"line": 1, "type": "ssh-dss"}},
		{"a certificate", certificateLine(t), "ParseKeys.KeyTypeUnsupported", map[string]any{"line": 1, "type": "ssh-ed25519-cert-v01@openssh.com"}},
		{"a type nobody knows", unknownTypeLine("ssh-xmss@openssh.com"), "ParseKeys.KeyTypeUnsupported", map[string]any{"line": 1, "type": "ssh-xmss@openssh.com"}},
		{"a good key, then an unsupported one", good.line + "\n" + dsaLine(), "ParseKeys.KeyTypeUnsupported", map[string]any{"line": 2}},
		{"a private key", privateKeyText(t), "ParseKeys.KeyIsPrivate", nil},
		{"a private key after a public one", good.line + "\n" + privateKeyText(t), "ParseKeys.KeyIsPrivate", nil},
		{"an old-style private key", "-----BEGIN RSA PRIVATE KEY-----\nMIIEow\n-----END RSA PRIVATE KEY-----\n", "ParseKeys.KeyIsPrivate", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			keys, err := ParseKeys(tt.text)
			if keys != nil {
				t.Errorf("keys were returned with the error: %d", len(keys))
			}
			meta := errtest.Route(t, err, tt.wantRoute)
			for key, want := range tt.wantMeta {
				if meta[key] != want {
					t.Errorf("metadata[%q] = %v, want %v", key, meta[key], want)
				}
			}
		})
	}
}

func TestPrivateKeyNeverLeaksIntoTheError(t *testing.T) {
	text := privateKeyText(t)
	// A piece of the key's body that would be in the error if the text were.
	lines := strings.Split(strings.TrimSpace(text), "\n")
	secret := lines[len(lines)/2]
	_, err := ParseKeys(text)
	appErr, ok := errors.FromError(err)
	if !ok {
		t.Fatalf("expected *errors.Error, got %T", err)
	}
	logged, jsonErr := json.Marshal(appErr.ToJson())
	if jsonErr != nil {
		t.Fatal(jsonErr)
	}
	for name, where := range map[string]string{
		"the message":     appErr.Error(),
		"the full print":  fmt.Sprintf("%+v %v", appErr, appErr.Metadata),
		"the logged form": string(logged),
	} {
		if strings.Contains(where, secret) || strings.Contains(where, "PRIVATE KEY") {
			t.Errorf("%s holds the private key", name)
		}
	}
	if !strings.Contains(appErr.Message, ".pub") {
		t.Errorf("the message does not say what to paste: %q", appErr.Message)
	}
}

func TestAHostileTypeNameIsMadeSafe(t *testing.T) {
	name := "ssh-\x1b[2Jevil"
	_, err := ParseKeys(unknownTypeLine(name))
	meta := errtest.Route(t, err, "ParseKeys.KeyTypeUnsupported")
	if got := meta["type"]; got != "ssh-\uFFFD[2Jevil" {
		t.Errorf("type in the metadata = %q", got)
	}
}
