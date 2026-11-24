// Package sshtest may use the insecure callback.
package sshtest

import "golang.org/x/crypto/ssh"

// Callback returns the callback that the harness may use.
func Callback() ssh.HostKeyCallback {
	return ssh.InsecureIgnoreHostKey()
}
