// Package ids makes the identifiers of Cave's records: a prefix that says what the record is
// ("p_" for a person, "k_" for a key, "a_" for an attempts entry, "c_" for a connection) and 16
// hexadecimal digits from the system's random source.
package ids

import (
	"crypto/rand"
	"encoding/hex"
)

// randomBytes is how many random bytes an identifier has: 16 hexadecimal digits.
const randomBytes = 8

// New returns a new identifier with the prefix.
func New(prefix string) string {
	var b [randomBytes]byte
	// crypto/rand.Read never returns an error: when the system has no randomness, it stops the
	// program instead.
	_, _ = rand.Read(b[:])
	return prefix + hex.EncodeToString(b[:])
}

// Valid says whether id has the form New gives for the prefix.
func Valid(prefix, id string) bool {
	if len(id) != len(prefix)+2*randomBytes || id[:len(prefix)] != prefix {
		return false
	}
	for _, r := range id[len(prefix):] {
		if (r < '0' || r > '9') && (r < 'a' || r > 'f') {
			return false
		}
	}
	return true
}
