package session

import (
	"net"
	"reflect"
	"strings"
	"testing"
)

func tcp(ip string, port int) net.Addr {
	return &net.TCPAddr{IP: net.ParseIP(ip), Port: port}
}

func TestBuildEnv(t *testing.T) {
	cave := []string{
		"PATH=/home/ada/bin:/usr/bin", "LANG=el_GR.UTF-8", "LC_TIME=en_GB.UTF-8", "XDG_RUNTIME_DIR=/run/user/1000",
		// None of these may reach a session.
		"SSH_AUTH_SOCK=/run/user/1000/keyring/ssh", "DISPLAY=:0", "WAYLAND_DISPLAY=wayland-0", "DBUS_SESSION_BUS_ADDRESS=unix:path=/x",
		"HOME=/home/someone-else", "USER=someone-else", "SHELL=/bin/zsh", "SECRET_TOKEN=hunter2", "TERM=xterm-kitty", "SSH_TTY=/dev/pts/9",
		"malformed entry without an equals sign",
	}
	base := EnvInput{User: "ada", Home: "/home/ada", Shell: "/bin/bash", Cave: cave, Remote: tcp("203.0.113.7", 50000),
		Local: tcp("192.0.2.1", 2222), Person: "alice"}
	tests := []struct {
		name   string
		change func(*EnvInput)
		want   []string
	}{
		{
			name: "without a PTY or an agent",
			want: []string{
				"CAVE_PERSON=alice", "HOME=/home/ada", "LANG=el_GR.UTF-8", "LC_TIME=en_GB.UTF-8", "LOGNAME=ada",
				"PATH=/home/ada/bin:/usr/bin", "SHELL=/bin/bash", "SSH_CLIENT=203.0.113.7 50000 2222",
				"SSH_CONNECTION=203.0.113.7 50000 192.0.2.1 2222", "USER=ada", "XDG_RUNTIME_DIR=/run/user/1000",
			},
		},
		{
			name: "with a PTY and an agent",
			change: func(in *EnvInput) {
				in.Term, in.TTY, in.AuthSock = "xterm-256color", "/dev/pts/3", "/run/user/1000/cave/a/agent.sock"
			},
			want: []string{
				"CAVE_PERSON=alice", "HOME=/home/ada", "LANG=el_GR.UTF-8", "LC_TIME=en_GB.UTF-8", "LOGNAME=ada",
				"PATH=/home/ada/bin:/usr/bin", "SHELL=/bin/bash", "SSH_AUTH_SOCK=/run/user/1000/cave/a/agent.sock",
				"SSH_CLIENT=203.0.113.7 50000 2222", "SSH_CONNECTION=203.0.113.7 50000 192.0.2.1 2222", "SSH_TTY=/dev/pts/3",
				"TERM=xterm-256color", "USER=ada", "XDG_RUNTIME_DIR=/run/user/1000",
			},
		},
		{
			name:   "a terminal type without a PTY is not set",
			change: func(in *EnvInput) { in.Term = "xterm" },
			want: []string{
				"CAVE_PERSON=alice", "HOME=/home/ada", "LANG=el_GR.UTF-8", "LC_TIME=en_GB.UTF-8", "LOGNAME=ada",
				"PATH=/home/ada/bin:/usr/bin", "SHELL=/bin/bash", "SSH_CLIENT=203.0.113.7 50000 2222",
				"SSH_CONNECTION=203.0.113.7 50000 192.0.2.1 2222", "USER=ada", "XDG_RUNTIME_DIR=/run/user/1000",
			},
		},
		{
			name: "what the client set, but not who or where the session is",
			change: func(in *EnvInput) {
				in.Client = map[string]string{
					"LANG": "en_US.UTF-8", "LC_ALL": "C", "EDITOR": "vi",
					// A client that was allowed to set anything still cannot set these.
					"HOME": "/root", "USER": "root", "LOGNAME": "root", "SHELL": "/bin/evil", "PATH": "/evil", "CAVE_PERSON": "bob",
					"SSH_AUTH_SOCK": "/evil.sock", "SSH_CLIENT": "evil", "SSH_CONNECTION": "evil", "SSH_TTY": "/dev/evil", "TERM": "evil",
					"XDG_RUNTIME_DIR": "/evil",
				}
			},
			want: []string{
				"CAVE_PERSON=alice", "EDITOR=vi", "HOME=/home/ada", "LANG=en_US.UTF-8", "LC_ALL=C", "LC_TIME=en_GB.UTF-8", "LOGNAME=ada",
				"PATH=/home/ada/bin:/usr/bin", "SHELL=/bin/bash", "SSH_CLIENT=203.0.113.7 50000 2222",
				"SSH_CONNECTION=203.0.113.7 50000 192.0.2.1 2222", "USER=ada", "XDG_RUNTIME_DIR=/run/user/1000",
			},
		},
		{
			name:   "Cave has no PATH, locale or runtime folder",
			change: func(in *EnvInput) { in.Cave = []string{"PATH=", "DISPLAY=:0"} },
			want: []string{
				"CAVE_PERSON=alice", "HOME=/home/ada", "LOGNAME=ada", "PATH=/usr/local/bin:/usr/bin:/bin", "SHELL=/bin/bash",
				"SSH_CLIENT=203.0.113.7 50000 2222", "SSH_CONNECTION=203.0.113.7 50000 192.0.2.1 2222", "USER=ada",
			},
		},
		{
			name: "IPv6, and an IPv4 address in its IPv6 form",
			change: func(in *EnvInput) {
				in.Cave = nil
				in.Remote, in.Local = tcp("2001:db8::7", 50000), tcp("::ffff:192.0.2.1", 2222)
			},
			want: []string{
				"CAVE_PERSON=alice", "HOME=/home/ada", "LOGNAME=ada", "PATH=/usr/local/bin:/usr/bin:/bin", "SHELL=/bin/bash",
				"SSH_CLIENT=2001:db8::7 50000 2222", "SSH_CONNECTION=2001:db8::7 50000 192.0.2.1 2222", "USER=ada",
			},
		},
		{
			name:   "no addresses",
			change: func(in *EnvInput) { in.Cave, in.Remote, in.Local = nil, nil, nil },
			want: []string{
				"CAVE_PERSON=alice", "HOME=/home/ada", "LOGNAME=ada", "PATH=/usr/local/bin:/usr/bin:/bin", "SHELL=/bin/bash", "USER=ada",
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			in := base
			if tt.change != nil {
				tt.change(&in)
			}
			got := BuildEnv(in)
			if !reflect.DeepEqual(got, tt.want) {
				t.Errorf("BuildEnv:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(tt.want, "\n"))
			}
		})
	}
}

func TestValidEnvName(t *testing.T) {
	tests := []struct {
		name string
		want bool
	}{
		{"LANG", true}, {"LC_ALL", true}, {"lower_case", true}, {"WITH SPACE", true},
		{"", false}, {"A=B", false}, {"=", false}, {"NUL\x00", false},
	}
	for _, tt := range tests {
		if got := validEnvName(tt.name); got != tt.want {
			t.Errorf("validEnvName(%q) = %v, want %v", tt.name, got, tt.want)
		}
	}
}

func TestPickShell(t *testing.T) {
	tests := []struct {
		name    string
		setting string
		user    User
		cave    []string
		want    string
	}{
		{"the setting wins", "/usr/bin/fish", User{Shell: "/bin/bash"}, []string{"SHELL=/bin/zsh"}, "/usr/bin/fish"},
		{"the account's shell", "", User{Shell: "/bin/bash"}, []string{"SHELL=/bin/zsh"}, "/bin/bash"},
		{"Cave's own SHELL", "", User{}, []string{"PATH=/bin", "SHELL=/bin/zsh"}, "/bin/zsh"},
		{"an empty SHELL is not one", "", User{}, []string{"SHELL="}, "/bin/sh"},
		{"nothing known", "", User{}, nil, "/bin/sh"},
	}
	for _, tt := range tests {
		if got := PickShell(tt.setting, tt.user, tt.cave); got != tt.want {
			t.Errorf("%s: PickShell = %q, want %q", tt.name, got, tt.want)
		}
	}
}

func TestPasswdShell(t *testing.T) {
	passwd := `root:x:0:0:Super User:/root:/bin/bash
# a comment
daemon:x:2:2:Daemon:/:/usr/sbin/nologin
ada:x:1000:1000:Ada Lovelace,,,:/home/ada:/usr/bin/zsh
short:x:1001
spaces:x:1002:1002::/home/spaces: /bin/dash
empty:x:1003:1003::/home/empty:
`
	tests := []struct{ uid, want string }{
		{"0", "/bin/bash"}, {"1000", "/usr/bin/zsh"}, {"2", "/usr/sbin/nologin"}, {"1002", "/bin/dash"},
		{"1003", ""}, {"1001", ""}, {"4242", ""}, {"100", ""}, {"", ""},
	}
	for _, tt := range tests {
		if got := passwdShell(strings.NewReader(passwd), tt.uid); got != tt.want {
			t.Errorf("passwdShell(uid %q) = %q, want %q", tt.uid, got, tt.want)
		}
	}
}

func TestLoginArgv0(t *testing.T) {
	tests := []struct{ shell, want string }{
		{"/bin/bash", "-bash"}, {"/usr/local/bin/fish", "-fish"}, {"sh", "-sh"}, {"/bin/", "-"},
	}
	for _, tt := range tests {
		if got := loginArgv0(tt.shell); got != tt.want {
			t.Errorf("loginArgv0(%q) = %q, want %q", tt.shell, got, tt.want)
		}
	}
}

func TestCurrentUser(t *testing.T) {
	u := CurrentUser([]string{"USER=from-env", "HOME=/from/env"})
	// The process has an account on every system the tests run on; the environment is only the
	// fallback, so the name is the account's and never empty.
	if u.Name == "" || u.Home == "" {
		t.Errorf("CurrentUser = %+v", u)
	}
}
