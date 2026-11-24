package server

import (
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestCallback(t *testing.T) {
	_ = ssh.InsecureIgnoreHostKey()
}
