package session

import (
	"bufio"
	"io"
	"os"
	"os/user"
	"strings"
)

// passwdPath is the system's account list.
const passwdPath = "/etc/passwd"

// fallbackShell is the shell of a session when nothing says another.
const fallbackShell = "/bin/sh"

// User is the OS user that sessions run as: the one who runs Cave.
type User struct {
	// Name is the login name of the OS user.
	Name string
	// Home is the home folder.
	Home string
	// Shell is the user's shell in the system's account list, or "" when it is not known.
	Shell string
}

// PickShell decides the shell of a session (docs/SPEC.md §9.5): the setting session.shell when
// it is set, else the user's shell in the system's account list, else the SHELL of Cave's own
// environment, else /bin/sh.
func PickShell(setting string, user User, caveEnv []string) string {
	if setting != "" {
		return setting
	}
	if user.Shell != "" {
		return user.Shell
	}
	for _, entry := range caveEnv {
		if value, ok := strings.CutPrefix(entry, "SHELL="); ok && value != "" {
			return value
		}
	}
	return fallbackShell
}

// passwdShell reads the shell of the account with the uid from text in the format of
// /etc/passwd ("name:password:uid:gid:comment:home:shell"); "" when the account is not there.
func passwdShell(passwd io.Reader, uid string) string {
	scanner := bufio.NewScanner(passwd)
	for scanner.Scan() {
		line := scanner.Text()
		if strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Split(line, ":")
		if len(fields) >= 7 && fields[2] == uid {
			return strings.TrimSpace(fields[6])
		}
	}
	return ""
}

// loginArgv0 returns the name a login shell is started under: "-" and the shell's file name.
func loginArgv0(shell string) string {
	return "-" + shell[strings.LastIndex(shell, "/")+1:]
}

// CurrentUser finds the OS user who runs Cave: the name and the home folder from the system's
// account of the process, else from USER and HOME of the environment given, and the shell from
// the system's account list when the account is in it.
func CurrentUser(environ []string) User {
	lookup := func(name string) string {
		for _, entry := range environ {
			if value, ok := strings.CutPrefix(entry, name+"="); ok {
				return value
			}
		}
		return ""
	}
	u := User{Name: lookup("USER"), Home: lookup("HOME")}
	account, err := user.Current()
	if err != nil {
		// Without an account, the environment's word is all there is.
		return u
	}
	u.Name, u.Home = account.Username, account.HomeDir
	passwd, err := os.Open(passwdPath)
	if err != nil {
		// No account list (Windows, or a system that keeps accounts elsewhere): no shell known.
		return u
	}
	u.Shell = passwdShell(passwd, account.Uid)
	_ = passwd.Close() // a file that was only read loses nothing when its close fails
	if u.Shell == "" {
		// macOS keeps its accounts in its directory service, not in the account list.
		u.Shell = directoryShell(account.Username)
	}
	return u
}
