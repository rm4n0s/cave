// Package session serves the session channels of authenticated clients: it takes their requests
// (env, shell, exec, signal and the rest of docs/SPEC.md §9.4), builds the environment of §9.5,
// starts the shell or the command as the user who runs Cave, carries its input and output, and
// reports how it ended.
package session

import (
	"net"
	"sort"
	"strconv"
	"strings"
)

// defaultPath is the PATH of a session when Cave itself has none.
const defaultPath = "/usr/local/bin:/usr/bin:/bin"

// EnvInput is what the environment of a session is made from (docs/SPEC.md §9.5).
type EnvInput struct {
	// User and Home are the OS user who runs Cave and their home folder.
	User, Home string
	// Shell is the shell the session starts.
	Shell string
	// Cave is Cave's own environment, as os.Environ gives it. Only PATH, LANG, the LC_ variables
	// and XDG_RUNTIME_DIR are taken from it.
	Cave []string
	// Client holds the variables the client set with accepted env requests.
	Client map[string]string
	// Term is the terminal type of the PTY, and TTY its path; both empty without a PTY.
	Term, TTY string
	// Remote and Local are the two ends of the connection.
	Remote, Local net.Addr
	// AuthSock is the socket of the forwarded agent, or "".
	AuthSock string
	// Person is the name of the person who is connected.
	Person string
}

// BuildEnv returns the environment of a session, as "NAME=value" strings sorted by name. The
// session does not inherit Cave's environment: it gets what docs/SPEC.md §9.5 lists and nothing
// else. What the client set comes first, and Cave's own variables are put over it, so that a
// client cannot change who or where the session is.
func BuildEnv(in EnvInput) []string {
	cave := map[string]string{}
	for _, entry := range in.Cave {
		if name, value, ok := strings.Cut(entry, "="); ok {
			cave[name] = value
		}
	}
	env := map[string]string{}
	// Cave's own locale, for the names the client did not set.
	for name, value := range cave {
		if name == "LANG" || strings.HasPrefix(name, "LC_") {
			env[name] = value
		}
	}
	for name, value := range in.Client {
		env[name] = value
	}

	env["USER"] = in.User
	env["LOGNAME"] = in.User
	env["HOME"] = in.Home
	env["SHELL"] = in.Shell
	env["PATH"] = defaultPath
	if path := cave["PATH"]; path != "" {
		env["PATH"] = path
	}
	delete(env, "XDG_RUNTIME_DIR")
	if dir := cave["XDG_RUNTIME_DIR"]; dir != "" {
		env["XDG_RUNTIME_DIR"] = dir
	}
	env["CAVE_PERSON"] = in.Person

	// These exist only when the session has what they describe; a client cannot set them.
	for _, name := range []string{"TERM", "SSH_TTY", "SSH_AUTH_SOCK", "SSH_CLIENT", "SSH_CONNECTION"} {
		delete(env, name)
	}
	if in.TTY != "" {
		env["SSH_TTY"] = in.TTY
		if in.Term != "" {
			env["TERM"] = in.Term
		}
	}
	if in.AuthSock != "" {
		env["SSH_AUTH_SOCK"] = in.AuthSock
	}
	remoteHost, remotePort := hostPort(in.Remote)
	localHost, localPort := hostPort(in.Local)
	if remoteHost != "" && localHost != "" {
		env["SSH_CLIENT"] = remoteHost + " " + remotePort + " " + localPort
		env["SSH_CONNECTION"] = remoteHost + " " + remotePort + " " + localHost + " " + localPort
	}

	out := make([]string, 0, len(env))
	for name, value := range env {
		out = append(out, name+"="+value)
	}
	sort.Strings(out)
	return out
}

// hostPort splits an address into its host and port as sshd writes them; both are "" when the
// address is not one.
func hostPort(addr net.Addr) (host, port string) {
	if addr == nil {
		return "", ""
	}
	if tcp, ok := addr.(*net.TCPAddr); ok {
		ip := tcp.IP
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		return ip.String(), strconv.Itoa(tcp.Port)
	}
	host, port, err := net.SplitHostPort(addr.String())
	if err != nil {
		return "", ""
	}
	return host, port
}

// validEnvName says whether a client may name a variable so: not empty, and without "=" or NUL.
func validEnvName(name string) bool {
	return name != "" && !strings.ContainsAny(name, "=\x00")
}
