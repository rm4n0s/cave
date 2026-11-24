//go:build ignore

// Command freeport prints a TCP port of 127.0.0.1 that the system gave out a moment ago and that
// is free again. The proof scripts use it so that nothing listens on a port that was chosen by
// hand (CLAUDE.md "Don't"): go run scripts/freeport.go
package main

import (
	"fmt"
	"net"
	"os"
)

func main() {
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		fmt.Fprintln(os.Stderr, "freeport:", err)
		os.Exit(1)
	}
	port := listener.Addr().(*net.TCPAddr).Port
	if err := listener.Close(); err != nil {
		fmt.Fprintln(os.Stderr, "freeport:", err)
		os.Exit(1)
	}
	fmt.Println(port)
}
