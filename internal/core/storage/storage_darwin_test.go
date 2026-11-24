package storage

import (
	"os"
	"strconv"
	"testing"
)

func TestResolveOnMacOS(t *testing.T) {
	uid := strconv.Itoa(os.Geteuid())
	support := "/Users/ada/Library/Application Support/Cave"
	tests := []struct {
		name      string
		opts      Options
		env       map[string]string
		want      Dirs
		wantRoute string
	}{
		{
			name: "defaults under the home",
			env:  map[string]string{"HOME": "/Users/ada"},
			want: Dirs{Config: support, State: support + "/State", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "the temporary folder of the user",
			env:  map[string]string{"HOME": "/Users/ada", "TMPDIR": "/var/folders/xy/abc/T/"},
			want: Dirs{Config: support, State: support + "/State", Runtime: "/var/folders/xy/abc/T/cave-" + uid},
		},
		{
			name: "XDG folders are not used on macOS",
			env:  map[string]string{"HOME": "/Users/ada", "XDG_CONFIG_HOME": "/x/config", "XDG_STATE_HOME": "/x/state", "XDG_RUNTIME_DIR": "/x/run"},
			want: Dirs{Config: support, State: support + "/State", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "the CAVE_ variables win",
			env:  map[string]string{"HOME": "/Users/ada", "CAVE_CONFIG_DIR": "/c/config", "CAVE_STATE_DIR": "/c/state"},
			want: Dirs{Config: "/c/config", State: "/c/state", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "the flags win over the CAVE_ variables",
			opts: Options{ConfigDir: "/f/config", StateDir: "/f/state"},
			env:  map[string]string{"HOME": "/Users/ada", "CAVE_CONFIG_DIR": "/c/config", "CAVE_STATE_DIR": "/c/state"},
			want: Dirs{Config: "/f/config", State: "/f/state", Runtime: "/tmp/cave-" + uid},
		},
		{
			name: "one flag, the other by default",
			opts: Options{StateDir: "/f/state"},
			env:  map[string]string{"HOME": "/Users/ada"},
			want: Dirs{Config: support, State: "/f/state", Runtime: "/tmp/cave-" + uid},
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
