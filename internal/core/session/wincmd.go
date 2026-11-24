package session

import "strings"

// windowsCommandLine returns the command line of a session's process on Windows
// (docs/SPEC.md §13.2): a shell request is the shell alone; a command is run as
// "<shell> /d /c <command>" with the command exactly as the client wrote it, or, for PowerShell,
// as "-NoLogo -NoProfile -Command <command>". It is a pure function so that a table tests it on
// every platform. The shell's own path is quoted when it needs it.
func windowsCommandLine(shell, command string, login bool) string {
	if login {
		return quoteWindowsArg(shell)
	}
	if isPowerShell(shell) {
		return quoteWindowsArg(shell) + " -NoLogo -NoProfile -Command " + quoteWindowsArg(command)
	}
	return quoteWindowsArg(shell) + " /d /c " + command
}

// isPowerShell says whether the shell is powershell.exe or pwsh.exe, by its file name, ignoring
// the folder and the case.
func isPowerShell(shell string) bool {
	name := shell
	if i := strings.LastIndexAny(name, `\/`); i >= 0 {
		name = name[i+1:]
	}
	name = strings.ToLower(name)
	return name == "powershell.exe" || name == "pwsh.exe"
}

// quoteWindowsArg quotes an argument by the rules of the C runtime that Windows programs parse
// their command line with: it is wrapped in double quotes when it is empty or has a space, a tab
// or a quote, the quotes inside it get a backslash, and the backslashes before them are doubled.
func quoteWindowsArg(arg string) string {
	if arg != "" && !strings.ContainsAny(arg, " \t\"") {
		return arg
	}
	var b strings.Builder
	b.WriteByte('"')
	backslashes := 0
	for i := 0; i < len(arg); i++ {
		c := arg[i]
		switch c {
		case '\\':
			backslashes++
		case '"':
			b.WriteString(strings.Repeat(`\`, backslashes*2+1))
			backslashes = 0
		default:
			b.WriteString(strings.Repeat(`\`, backslashes))
			backslashes = 0
		}
		if c != '\\' {
			b.WriteByte(c)
		}
	}
	b.WriteString(strings.Repeat(`\`, backslashes*2)) // before the closing quote
	b.WriteByte('"')
	return b.String()
}
