package session

import (
	"sort"
	"strings"
	"syscall"
	"unicode/utf16"
)

// windowsVariables are the variables of Cave's own environment that a session on Windows gets
// (docs/SPEC.md §13.2). Windows ignores the case of a name, so they are matched ignoring it.
var windowsVariables = []string{
	"SystemRoot", "SystemDrive", "windir", "ComSpec", "PATH", "PATHEXT", "TEMP", "TMP", "USERNAME",
	"USERPROFILE", "HOMEDRIVE", "HOMEPATH", "APPDATA", "LOCALAPPDATA", "ProgramData", "ProgramFiles",
	"ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles", "COMPUTERNAME", "OS",
	"PROCESSOR_ARCHITECTURE", "NUMBER_OF_PROCESSORS",
}

// BuildWindowsEnv returns the environment of a session on Windows, as "NAME=value" strings
// sorted by name ignoring case (the order Windows asks for). Like BuildEnv it makes the
// environment from scratch: the variables of windowsVariables that Cave has, what the client
// set with accepted env requests (never over a variable of Cave's), and the variables of the
// session itself, which a client cannot set: TERM (only with a terminal), SSH_CLIENT,
// SSH_CONNECTION, SSH_AUTH_SOCK and CAVE_PERSON. Nothing else of Cave's environment is passed on.
func BuildWindowsEnv(in EnvInput) []string {
	type entry struct{ name, value string }
	env := map[string]entry{} // by the name in upper case
	set := func(name, value string) { env[strings.ToUpper(name)] = entry{name, value} }
	del := func(name string) { delete(env, strings.ToUpper(name)) }

	for name, value := range in.Client {
		set(name, value)
	}
	for _, want := range windowsVariables {
		del(want) // the client does not change who or where the session is
	}
	for _, line := range in.Cave {
		name, value, ok := strings.Cut(line, "=")
		if !ok || name == "" {
			continue
		}
		for _, want := range windowsVariables {
			if strings.EqualFold(name, want) {
				set(want, value)
				break
			}
		}
	}

	for _, name := range []string{"TERM", "SSH_CLIENT", "SSH_CONNECTION", "SSH_AUTH_SOCK", "CAVE_PERSON"} {
		del(name)
	}
	set("CAVE_PERSON", in.Person)
	if in.Term != "" {
		set("TERM", in.Term)
	}
	if in.AuthSock != "" {
		set("SSH_AUTH_SOCK", in.AuthSock)
	}
	remoteHost, remotePort := hostPort(in.Remote)
	localHost, localPort := hostPort(in.Local)
	if remoteHost != "" && localHost != "" {
		set("SSH_CLIENT", remoteHost+" "+remotePort+" "+localPort)
		set("SSH_CONNECTION", remoteHost+" "+remotePort+" "+localHost+" "+localPort)
	}

	keys := make([]string, 0, len(env))
	for key := range env {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	out := make([]string, 0, len(keys))
	for _, key := range keys {
		out = append(out, env[key].name+"="+env[key].value)
	}
	return out
}

// windowsEnvBlock returns the environment as Windows' CreateProcess takes it: UTF-16, the
// "name=value" entries sorted by name ignoring case, each ended by a zero, and the block by one
// more. An entry with a zero in it cannot be written so and is refused with EINVAL, as os/exec
// refuses it.
func windowsEnvBlock(env []string) ([]uint16, error) {
	name := func(entry string) string { return strings.ToUpper(strings.SplitN(entry, "=", 2)[0]) }
	sorted := append([]string(nil), env...)
	sort.SliceStable(sorted, func(i, j int) bool { return name(sorted[i]) < name(sorted[j]) })
	var block []uint16
	for _, entry := range sorted {
		if strings.IndexByte(entry, 0) >= 0 {
			return nil, syscall.EINVAL
		}
		block = append(block, utf16.Encode([]rune(entry))...)
		block = append(block, 0)
	}
	if len(block) == 0 {
		block = append(block, 0) // an empty block is two zeros
	}
	return append(block, 0), nil
}
