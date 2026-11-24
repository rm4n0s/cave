package session

import (
	"os/user"
	"testing"
)

func TestDsclShell(t *testing.T) {
	tests := []struct {
		name string
		out  string
		want string
	}{
		{"the shell", "UserShell: /bin/zsh\n", "/bin/zsh"},
		{"spaces", "UserShell:   /opt/homebrew/bin/fish  \n", "/opt/homebrew/bin/fish"},
		{"no such attribute", "No such key: UserShell\n", ""},
		{"a relative path", "UserShell: zsh\n", ""},
		{"two lines", "UserShell:\n /bin/zsh\n /bin/bash\n", ""},
		{"nothing", "", ""},
	}
	for _, tt := range tests {
		if got := dsclShell(tt.out); got != tt.want {
			t.Errorf("%s: dsclShell = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestDirectoryShellOfThisUser(t *testing.T) {
	account, err := user.Current()
	if err != nil {
		t.Skip("no account:", err)
	}
	if got := directoryShell(account.Username); got == "" || got[0] != '/' {
		t.Errorf("directoryShell(%q) = %q, want the account's shell", account.Username, got)
	}
	for _, name := range []string{"", "../etc", "no-such-user-of-cave"} {
		if got := directoryShell(name); got != "" {
			t.Errorf("directoryShell(%q) = %q, want nothing", name, got)
		}
	}
}
