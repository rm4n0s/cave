package archtest

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// runScript runs a script of the scripts folder with the test's environment plus extra
// variables, and returns its standard output, its standard error and its exit status.
func runScript(t *testing.T, extraEnv []string, script string, args ...string) (string, string, int) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("the scripts are bash scripts for Linux")
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(moduleRoot(t), "scripts", script), args...)
	cmd.Env = append(os.Environ(), extraEnv...)
	var stdout, stderr bytes.Buffer
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	err := cmd.Run()
	status := 0
	if err != nil {
		exitErr, ok := err.(*exec.ExitError)
		if !ok {
			t.Fatalf("%s: %v", script, err)
		}
		status = exitErr.ExitCode()
	}
	return stdout.String(), stderr.String(), status
}

// envOf parses the output of env into a map.
func envOf(output string) map[string]string {
	vars := map[string]string{}
	for _, line := range strings.Split(output, "\n") {
		if name, value, ok := strings.Cut(line, "="); ok {
			vars[name] = value
		}
	}
	return vars
}

// statMode is the stat command that prints the mode of a file in octal, then its name: GNU
// stat on Linux, BSD stat on macOS.
var statMode = map[bool]string{true: `stat -f '%Lp %N'`, false: `stat -c '%a %n'`}[runtime.GOOS == "darwin"]

func TestIsolatedScript(t *testing.T) {
	dirty := []string{
		"SSH_AUTH_SOCK=/home/someone/agent.sock", "SSH_AGENT_PID=4242", "SSH_ASKPASS=/usr/bin/ask",
		"HOME=/home/someone", "XDG_CONFIG_HOME=/home/someone/.config", "XDG_RUNTIME_DIR=/run/user/4242",
		"CAVE_CONFIG_DIR=/home/someone/.config/cave", "CAVE_STATE_DIR=/home/someone/.local/state/cave",
	}

	t.Run("the environment points into a private folder that is removed", func(t *testing.T) {
		out, _, status := runScript(t, dirty, "isolated.sh", "--", "env")
		if status != 0 {
			t.Fatalf("status %d", status)
		}
		vars := envOf(out)
		root := vars["CAVE_ISOLATED_ROOT"]
		if root == "" || strings.HasPrefix(root, "/home/someone") {
			t.Fatalf("CAVE_ISOLATED_ROOT = %q", root)
		}
		for name, sub := range map[string]string{
			"CAVE_CONFIG_DIR": "config", "CAVE_STATE_DIR": "state", "HOME": "home",
			"XDG_CONFIG_HOME": "xdg-config", "XDG_STATE_HOME": "xdg-state", "XDG_CACHE_HOME": "xdg-cache",
			"XDG_DATA_HOME": "xdg-data", "XDG_RUNTIME_DIR": "run", "TMPDIR": "tmp",
		} {
			if want := filepath.Join(root, sub); vars[name] != want {
				t.Errorf("%s = %q, want %q", name, vars[name], want)
			}
		}
		for _, name := range []string{"SSH_AUTH_SOCK", "SSH_AGENT_PID", "SSH_ASKPASS"} {
			if value, ok := vars[name]; ok {
				t.Errorf("%s=%q is still set", name, value)
			}
		}
		if _, err := os.Stat(root); !os.IsNotExist(err) {
			t.Errorf("%s is still there after the command: %v", root, err)
		}
	})

	t.Run("the folders exist and are private while the command runs", func(t *testing.T) {
		out, _, status := runScript(t, dirty, "isolated.sh", "--", "sh", "-c",
			statMode+` "$CAVE_CONFIG_DIR" "$CAVE_STATE_DIR" "$HOME" "$XDG_RUNTIME_DIR" "$TMPDIR" "$CAVE_ISOLATED_ROOT"`)
		if status != 0 {
			t.Fatalf("status %d", status)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if len(lines) != 6 {
			t.Fatalf("output:\n%s", out)
		}
		for _, line := range lines {
			if !strings.HasPrefix(line, "700 ") {
				t.Errorf("%s: want mode 700", line)
			}
		}
	})

	t.Run("a fixture becomes the configuration", func(t *testing.T) {
		out, _, status := runScript(t, dirty, "isolated.sh", "--config", fixture("isolatedconfig"), "--",
			"sh", "-c", `cat "$CAVE_CONFIG_DIR/settings.json"; `+statMode+` "$CAVE_CONFIG_DIR/settings.json"`)
		if status != 0 {
			t.Fatalf("status %d", status)
		}
		if !strings.Contains(out, `"server.port":2022`) {
			t.Errorf("the fixture was not copied:\n%s", out)
		}
		lines := strings.Split(strings.TrimSpace(out), "\n")
		if mode, _, _ := strings.Cut(lines[len(lines)-1], " "); !strings.HasSuffix(mode, "00") {
			t.Errorf("the copied file is open to others:\n%s", out)
		}
		// The fixture itself is untouched.
		if _, err := os.Stat(filepath.Join(fixture("isolatedconfig"), "settings.json")); err != nil {
			t.Error(err)
		}
	})

	t.Run("the command's exit status is kept", func(t *testing.T) {
		if _, _, status := runScript(t, nil, "isolated.sh", "--", "sh", "-c", "exit 7"); status != 7 {
			t.Errorf("status %d, want 7", status)
		}
	})

	t.Run("--keep leaves the folder and says where", func(t *testing.T) {
		out, errOut, status := runScript(t, nil, "isolated.sh", "--keep", "--", "sh", "-c", `printf %s "$CAVE_ISOLATED_ROOT"`)
		if status != 0 {
			t.Fatalf("status %d", status)
		}
		root := out
		t.Cleanup(func() {
			if strings.Contains(root, "cave-iso.") {
				if err := os.RemoveAll(root); err != nil {
					t.Error(err)
				}
			}
		})
		if info, err := os.Stat(root); err != nil || !info.IsDir() {
			t.Errorf("%q was not kept: %v", root, err)
		}
		if !strings.Contains(errOut, "kept "+root) {
			t.Errorf("standard error does not say where: %q", errOut)
		}
	})

	usage := []struct {
		name string
		args []string
	}{
		{"no command", []string{"--"}},
		{"no separator", []string{"env"}},
		{"--config without a folder", []string{"--config"}},
		{"a fixture that does not exist", []string{"--config", "/nonexistent/fixture", "--", "env"}},
	}
	for _, tt := range usage {
		t.Run(tt.name, func(t *testing.T) {
			if _, _, status := runScript(t, nil, "isolated.sh", tt.args...); status != 64 {
				t.Errorf("status %d, want 64", status)
			}
		})
	}
}

func TestTranscriptScript(t *testing.T) {
	out := filepath.Join(t.TempDir(), "proof.txt")
	// As a test prints it: t.TempDir() is cleaned (the TMPDIR of macOS ends with a slash).
	tmp := filepath.Clean(os.TempDir())
	script := `echo "=== RUN   TestX"
echo "--- PASS: TestX (0.42s)"
echo "listening on 127.0.0.1:43211 and [::1]:5022"
echo "home is ` + tmp + `/TestX123456/001/home"
echo "to stderr" >&2
printf 'ok  \tgithub.com/rm4n0s/cave/internal/core/storage\t1.234s\n'`
	_, _, status := runScript(t, nil, "transcript.sh", out, "--", "sh", "-c", script)
	if status != 0 {
		t.Fatalf("status %d", status)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	got := string(data)
	want := []string{
		"$ sh -c ", // the command line comes first
		"=== RUN   TestX\n",
		"--- PASS: TestX\n",
		"listening on 127.0.0.1:<port> and [::1]:<port>\n",
		"home is <tmp>/home\n",
		"to stderr\n",
		"ok  \tgithub.com/rm4n0s/cave/internal/core/storage\n",
	}
	for _, w := range want {
		if !strings.Contains(got, w) {
			t.Errorf("the transcript lacks %q:\n%s", w, got)
		}
	}
	if !strings.HasPrefix(got, "$ sh -c ") {
		t.Errorf("the transcript does not start with the command line:\n%s", got)
	}

	t.Run("a failing command writes nothing", func(t *testing.T) {
		out := filepath.Join(t.TempDir(), "proof.txt")
		_, errOut, status := runScript(t, nil, "transcript.sh", out, "--", "sh", "-c", "echo broken; exit 3")
		if status != 3 {
			t.Errorf("status %d, want 3", status)
		}
		if _, err := os.Stat(out); !os.IsNotExist(err) {
			t.Error("the file was written")
		}
		if !strings.Contains(errOut, "broken") {
			t.Errorf("the output of the failed command is not shown: %q", errOut)
		}
	})
	t.Run("a usage mistake", func(t *testing.T) {
		if _, _, status := runScript(t, nil, "transcript.sh", "out.txt", "echo"); status != 64 {
			t.Errorf("status %d, want 64", status)
		}
	})
}
