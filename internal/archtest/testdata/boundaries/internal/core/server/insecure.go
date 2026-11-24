// Package server breaks rule 6.
package server

import "golang.org/x/crypto/ssh"

// Callback returns the callback that only tests may use.
func Callback() ssh.HostKeyCallback {
	return ssh.InsecureIgnoreHostKey()
}
