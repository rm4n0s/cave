package session

import "testing"

func TestWindowsCommandLine(t *testing.T) {
	tests := []struct {
		name           string
		shell, command string
		login          bool
		want           string
	}{
		{"cmd runs the command as written", "cmd.exe", `echo "a b"   c`, false, `cmd.exe /d /c echo "a b"   c`},
		{"the shell request is the shell alone", "cmd.exe", "", true, "cmd.exe"},
		{"a shell with a folder", `C:\Windows\System32\cmd.exe`, "ver", false, `C:\Windows\System32\cmd.exe /d /c ver`},
		{"a shell in a folder with a space is quoted", `C:\Program Files\Shell\sh.exe`, "ver", false, `"C:\Program Files\Shell\sh.exe" /d /c ver`},
		{"powershell", "powershell.exe", "Get-Date", false, "powershell.exe -NoLogo -NoProfile -Command Get-Date"},
		{"powershell with a folder", `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, "Get-Date", false,
			`C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe -NoLogo -NoProfile -Command Get-Date`},
		{"pwsh in capitals", "PWSH.EXE", "1+1", false, "PWSH.EXE -NoLogo -NoProfile -Command 1+1"},
		{"pwsh with slashes", "C:/Program Files/PowerShell/7/pwsh.exe", "1+1", false,
			`"C:/Program Files/PowerShell/7/pwsh.exe" -NoLogo -NoProfile -Command 1+1`},
		{"a command for powershell is quoted for its parser", "pwsh.exe", `echo "a b"`, false,
			`pwsh.exe -NoLogo -NoProfile -Command "echo \"a b\""`},
		{"a shell request of powershell is the shell alone", "pwsh.exe", "", true, "pwsh.exe"},
		{"a name that only ends like powershell is another shell", "notpowershell.exe", "ver", false, "notpowershell.exe /d /c ver"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := windowsCommandLine(tt.shell, tt.command, tt.login); got != tt.want {
				t.Errorf("got  %s\nwant %s", got, tt.want)
			}
		})
	}
}

func TestQuoteWindowsArg(t *testing.T) {
	tests := []struct{ arg, want string }{
		{"plain", "plain"},
		{"", `""`},
		{"a b", `"a b"`},
		{`a"b`, `"a\"b"`},
		{`a\b`, `a\b`},
		{`a b\`, `"a b\\"`},
		{`a\"b c`, `"a\\\"b c"`},
		{"tab\there", "\"tab\there\""},
	}
	for _, tt := range tests {
		if got := quoteWindowsArg(tt.arg); got != tt.want {
			t.Errorf("quoteWindowsArg(%q) = %s, want %s", tt.arg, got, tt.want)
		}
	}
}
