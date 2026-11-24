package session

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

// dsclPath is the program that reads macOS's directory service.
const dsclPath = "/usr/bin/dscl"

// dsclTimeout is how long the directory service is waited for.
const dsclTimeout = 5 * time.Second

// directoryShell asks macOS's directory service for the shell of the account; "" when it does
// not answer.
func directoryShell(name string) string {
	if name == "" || strings.ContainsAny(name, "/\x00") {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), dsclTimeout)
	defer cancel()
	out, err := exec.CommandContext(ctx, dsclPath, ".", "-read", "/Users/"+name, "UserShell").Output()
	if err != nil {
		return ""
	}
	return dsclShell(string(out))
}

// dsclShell reads the shell from what "dscl . -read /Users/<name> UserShell" prints
// ("UserShell: /bin/zsh"); "" when it is not an absolute path.
func dsclShell(out string) string {
	value, ok := strings.CutPrefix(strings.TrimSpace(out), "UserShell:")
	if !ok {
		return ""
	}
	shell := strings.TrimSpace(value)
	if !strings.HasPrefix(shell, "/") || strings.ContainsAny(shell, "\n\r") {
		return ""
	}
	return shell
}
