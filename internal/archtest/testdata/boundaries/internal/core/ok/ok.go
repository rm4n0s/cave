// Package ok breaks no rule.
package ok

import (
	"fmt"

	_ "github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
	_ "golang.org/x/crypto/ssh"
)

// Hello formats without making an error.
func Hello() (string, error) {
	return fmt.Sprintf("hello"), errors.New("OkTag", "ok")
}
