package storage

import (
	"os"
	"strconv"
	"testing"
)

func TestResolveOnLinux(t *testing.T) {
	uid := strconv.Itoa(os.Geteuid())
	tests := []struct {
		name      string
		opts      Options
		env       map[string]string
		want      Dirs
		wantRoute string
	}{
		{
			name: "defaults under the home",
			env:  map[string]string{"HOME": "/home/ada"},
			want: Dirs{Config: "/home/ada/.config/cave", State: "/home/ada/.local/state/cave", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "the XDG folders",
			env: map[string]string{"HOME": "/home/ada", "XDG_CONFIG_HOME": "/x/config", "XDG_STATE_HOME": "/x/state",
				"XDG_RUNTIME_DIR": "/run/user/1000"},
			want: Dirs{Config: "/x/config/cave", State: "/x/state/cave", Runtime: "/run/user/1000/cave"},
		},
		{
			name: "relative and empty XDG folders are ignored",
			env: map[string]string{"HOME": "/home/ada", "XDG_CONFIG_HOME": "relative", "XDG_STATE_HOME": "",
				"XDG_RUNTIME_DIR": "run"},
			want: Dirs{Config: "/home/ada/.config/cave", State: "/home/ada/.local/state/cave", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "the CAVE_ variables win over XDG",
			env: map[string]string{"HOME": "/home/ada", "XDG_CONFIG_HOME": "/x/config", "XDG_STATE_HOME": "/x/state",
				"CAVE_CONFIG_DIR": "/c/config", "CAVE_STATE_DIR": "/c/state"},
			want: Dirs{Config: "/c/config", State: "/c/state", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "the flags win over the CAVE_ variables",
			opts: Options{ConfigDir: "/f/config", StateDir: "/f/state"},
			env:  map[string]string{"HOME": "/home/ada", "CAVE_CONFIG_DIR": "/c/config", "CAVE_STATE_DIR": "/c/state"},
			want: Dirs{Config: "/f/config", State: "/f/state", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "one flag, the other by default",
			opts: Options{StateDir: "/f/state"},
			env:  map[string]string{"HOME": "/home/ada"},
			want: Dirs{Config: "/home/ada/.config/cave", State: "/f/state", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "the temporary folder of TMPDIR",
			env:  map[string]string{"HOME": "/home/ada", "TMPDIR": "/var/tmp"},
			want: Dirs{Config: "/home/ada/.config/cave", State: "/home/ada/.local/state/cave", Runtime: "/var/tmp/cave-" + uid},
		},
		{
			name: "no home, but both folders given",
			opts: Options{ConfigDir: "/f/config"},
			env:  map[string]string{"CAVE_STATE_DIR": "/c/state"},
			want: Dirs{Config: "/f/config", State: "/c/state", Runtime: "/tmp/cave-" + uid},
		},
		{name: "no home", env: map[string]string{}, wantRoute: "Resolve->resolvePlatform.StorageHomeUnknown"},
		{name: "a relative home", env: map[string]string{"HOME": "home"}, wantRoute: "Resolve->resolvePlatform.StorageHomeUnknown"},
		{
			name:      "no home and only one folder given",
			opts:      Options{ConfigDir: "/f/config"},
			env:       map[string]string{},
			wantRoute: "Resolve->resolvePlatform.StorageHomeUnknown",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := Resolve(tt.opts, envOf(tt.env))
			if tt.wantRoute != "" {
				wantRoute(t, err, tt.wantRoute)
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if got != tt.want {
				t.Errorf("Resolve = %+v, want %+v", got, tt.want)
			}
		})
	}
}
