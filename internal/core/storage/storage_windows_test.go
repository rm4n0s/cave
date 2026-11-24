package storage

import (
	"testing"
)

func TestResolveOnWindows(t *testing.T) {
	envOf := func(vars map[string]string) Env {
		return func(name string) (string, bool) {
			value, ok := vars[name]
			return value, ok
		}
	}
	both := map[string]string{"AppData": `C:\Users\ada\AppData\Roaming`, "LocalAppData": `C:\Users\ada\AppData\Local`}
	tests := []struct {
		name      string
		opts      Options
		env       map[string]string
		want      Dirs
		wantRoute string
	}{
		{
			name: "defaults in the profile",
			env:  both,
			want: Dirs{Config: `C:\Users\ada\AppData\Roaming\Cave`, State: `C:\Users\ada\AppData\Local\Cave`},
		},
		{
			name: "the CAVE_ variables win",
			env: map[string]string{"AppData": both["AppData"], "LocalAppData": both["LocalAppData"],
				"CAVE_CONFIG_DIR": `D:\cave\config`, "CAVE_STATE_DIR": `D:\cave\state`},
			want: Dirs{Config: `D:\cave\config`, State: `D:\cave\state`},
		},
		{
			name: "the flags win over the CAVE_ variables",
			opts: Options{ConfigDir: `E:\config`, StateDir: `E:\state`},
			env:  map[string]string{"CAVE_CONFIG_DIR": `D:\cave\config`, "CAVE_STATE_DIR": `D:\cave\state`},
			want: Dirs{Config: `E:\config`, State: `E:\state`},
		},
		{
			name:      "no AppData",
			env:       map[string]string{"LocalAppData": both["LocalAppData"]},
			wantRoute: "Resolve->resolvePlatform.StorageAppDataUnknown",
		},
		{
			name:      "no LocalAppData",
			env:       map[string]string{"AppData": both["AppData"]},
			wantRoute: "Resolve->resolvePlatform.StorageLocalAppDataUnknown",
		},
		{
			name:      "a relative flag",
			opts:      Options{ConfigDir: `config`},
			env:       both,
			wantRoute: "Resolve.StorageDirNotAbsolute",
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
