package session

import (
	"reflect"
	"strings"
	"testing"
)

func TestBuildWindowsEnv(t *testing.T) {
	cave := []string{
		`SystemRoot=C:\Windows`, "SystemDrive=C:", `windir=C:\Windows`, `ComSpec=C:\Windows\system32\cmd.exe`,
		`Path=C:\Windows\system32;C:\Windows`, // Windows spells it Path
		"PATHEXT=.COM;.EXE;.BAT", `TEMP=C:\Temp`, `TMP=C:\Temp`, "USERNAME=ada", `USERPROFILE=C:\Users\ada`,
		"HOMEDRIVE=C:", `HOMEPATH=\Users\ada`, `APPDATA=C:\Users\ada\AppData\Roaming`, `LOCALAPPDATA=C:\Users\ada\AppData\Local`,
		`ProgramData=C:\ProgramData`, `ProgramFiles=C:\Program Files`, `ProgramFiles(x86)=C:\Program Files (x86)`,
		`ProgramW6432=C:\Program Files`, `CommonProgramFiles=C:\Program Files\Common Files`, "COMPUTERNAME=PC", "OS=Windows_NT",
		"PROCESSOR_ARCHITECTURE=AMD64", "NUMBER_OF_PROCESSORS=8",
		// None of these may reach a session.
		"SECRET_TOKEN=hunter2", "SSH_AUTH_SOCK=/secret/agent.sock", "TERM=xterm-kitty", "CAVE_PERSON=someone-else",
		"malformed entry without an equals sign", "=C:=C:\\",
	}
	listed := []string{
		`APPDATA=C:\Users\ada\AppData\Roaming`, "CAVE_PERSON=alice", `CommonProgramFiles=C:\Program Files\Common Files`,
		"COMPUTERNAME=PC", `ComSpec=C:\Windows\system32\cmd.exe`, "HOMEDRIVE=C:", `HOMEPATH=\Users\ada`,
		`LOCALAPPDATA=C:\Users\ada\AppData\Local`, "NUMBER_OF_PROCESSORS=8", "OS=Windows_NT",
		`PATH=C:\Windows\system32;C:\Windows`, "PATHEXT=.COM;.EXE;.BAT", "PROCESSOR_ARCHITECTURE=AMD64",
		`ProgramData=C:\ProgramData`, `ProgramFiles=C:\Program Files`, `ProgramFiles(x86)=C:\Program Files (x86)`,
		`ProgramW6432=C:\Program Files`, "SSH_CLIENT=203.0.113.7 50000 2222", "SSH_CONNECTION=203.0.113.7 50000 192.0.2.1 2222",
		"SystemDrive=C:", `SystemRoot=C:\Windows`, `TEMP=C:\Temp`, `TMP=C:\Temp`, `USERNAME=ada`, `USERPROFILE=C:\Users\ada`,
		`windir=C:\Windows`,
	}
	base := EnvInput{User: "ada", Home: `C:\Users\ada`, Cave: cave, Remote: tcp("203.0.113.7", 50000),
		Local: tcp("192.0.2.1", 2222), Person: "alice"}
	with := func(extra ...string) []string {
		out := append(append([]string{}, listed...), extra...)
		sortFold(out)
		return out
	}
	tests := []struct {
		name   string
		change func(*EnvInput)
		want   []string
	}{
		{"the listed variables, the connection and the person", nil, with()},
		{"a terminal and an agent", func(in *EnvInput) { in.Term, in.AuthSock = "xterm-256color", `\\.\pipe\cave-agent-1` },
			with("TERM=xterm-256color", `SSH_AUTH_SOCK=\\.\pipe\cave-agent-1`)},
		{"a client's variable is taken", func(in *EnvInput) { in.Client = map[string]string{"LANG": "el_GR", "Editor": "vim"} },
			with("LANG=el_GR", "Editor=vim")},
		{"a client cannot change who or where the session is", func(in *EnvInput) {
			in.Client = map[string]string{
				"userprofile": `C:\evil`, "PATH": `C:\evil`, "cave_person": "root", "Term": "evil", "ssh_auth_sock": "evil",
				"SSH_CLIENT": "evil", "ssh_connection": "evil", "ComSpec": `C:\evil.exe`,
			}
		}, with()},
		{"no connection addresses, no SSH_CLIENT", func(in *EnvInput) { in.Remote, in.Local = nil, nil },
			func() []string {
				var out []string
				for _, line := range with() {
					if !strings.HasPrefix(line, "SSH_C") {
						out = append(out, line)
					}
				}
				return out
			}()},
		{"a terminal type without a terminal is not asked for: none is given", func(in *EnvInput) { in.Term = "" }, with()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			if tt.change != nil {
				tt.change(&in)
			}
			got := BuildWindowsEnv(in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
			for _, line := range got {
				if strings.HasPrefix(line, "SECRET_TOKEN") || strings.Contains(line, "/secret/") || strings.Contains(line, "evil") && !strings.HasPrefix(line, "Editor") {
					t.Errorf("leaked: %s", line)
				}
			}
		})
	}
}

func TestWindowsEnvIsSortedIgnoringCase(t *testing.T) {
	got := BuildWindowsEnv(EnvInput{Person: "alice", Client: map[string]string{"zeta": "1", "Alpha": "2", "beta": "3"}})
	var names []string
	for _, line := range got {
		names = append(names, strings.ToUpper(strings.SplitN(line, "=", 2)[0]))
	}
	want := []string{"ALPHA", "BETA", "CAVE_PERSON", "ZETA"}
	if !reflect.DeepEqual(names, want) {
		t.Errorf("names = %v, want %v", names, want)
	}
}

// sortFold sorts "NAME=value" lines by the name in upper case, as BuildWindowsEnv does.
func sortFold(lines []string) {
	key := func(s string) string { return strings.ToUpper(strings.SplitN(s, "=", 2)[0]) }
	for i := 1; i < len(lines); i++ {
		for j := i; j > 0 && key(lines[j]) < key(lines[j-1]); j-- {
			lines[j], lines[j-1] = lines[j-1], lines[j]
		}
	}
}
