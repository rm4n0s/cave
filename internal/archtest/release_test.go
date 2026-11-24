//go:build cave_release

package archtest

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"context"
	"debug/buildinfo"
	"debug/elf"
	"debug/macho"
	"debug/pe"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
	"time"
)

// TestBuildReleaseMakesEveryArchive runs scripts/build-release.sh in a temporary dist and looks
// at what it made: the six archives with the files they should have, programs for the right
// system and processor, builds made without cgo, and SHA256SUMS
// that `sha256sum -c` accepts. It is heavy (it builds twelve programs), so it has the tag
// cave_release: scripts/heavy.sh --alone -- go test -tags cave_release -run Release ./internal/archtest
func TestBuildReleaseMakesEveryArchive(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the scripts are bash scripts for Linux")
	}
	for _, tool := range []string{"tar", "zip", "sha256sum"} {
		if _, err := exec.LookPath(tool); err != nil {
			t.Skipf("%s is not installed", tool)
		}
	}
	const version = "0.0.0-test"
	dist := t.TempDir()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	cmd := exec.CommandContext(ctx, filepath.Join(moduleRoot(t), "scripts", "build-release.sh"), "--version", version, "--dist", dist)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build-release.sh: %v\n%s", err, out)
	}
	out := filepath.Join(dist, version)
	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	want := []string{
		"SHA256SUMS", "cave-0.0.0-test-linux-amd64.tar.gz", "cave-0.0.0-test-linux-arm64.tar.gz",
		"cave-0.0.0-test-macos-amd64.tar.gz", "cave-0.0.0-test-macos-arm64.tar.gz",
		"cave-0.0.0-test-windows-amd64.zip", "cave-0.0.0-test-windows-arm64.zip",
	}
	if strings.Join(names, " ") != strings.Join(want, " ") {
		t.Fatalf("the release has %v, want %v", names, want)
	}
	check := exec.Command("sha256sum", "-c", "SHA256SUMS")
	check.Dir = out
	if sums, err := check.CombinedOutput(); err != nil {
		t.Errorf("sha256sum -c SHA256SUMS: %v\n%s", err, sums)
	}

	for arch, machine := range map[string]elf.Machine{"amd64": elf.EM_X86_64, "arm64": elf.EM_AARCH64} {
		name := "cave-" + version + "-linux-" + arch
		files := untar(t, filepath.Join(out, name+".tar.gz"))
		for _, file := range []string{"cave", "tcave", "cave.desktop", "icons/cave-16.png", "icons/cave-256.png", "README.md", "LICENSE"} {
			if _, ok := files[name+"/"+file]; !ok {
				t.Errorf("%s has no %s", name, file)
			}
		}
		for _, program := range []string{"cave", "tcave"} {
			f, err := elf.NewFile(bytes.NewReader(files[name+"/"+program]))
			if err != nil {
				t.Errorf("%s/%s is not a Linux program: %v", name, program, err)
				continue
			}
			if f.Machine != machine {
				t.Errorf("%s/%s is for %v, want %v", name, program, f.Machine, machine)
			}
			noCgo(t, name+"/"+program, files[name+"/"+program])
		}
	}
	for arch, cpu := range map[string]macho.Cpu{"amd64": macho.CpuAmd64, "arm64": macho.CpuArm64} {
		name := "cave-" + version + "-macos-" + arch
		files := untar(t, filepath.Join(out, name+".tar.gz"))
		for _, file := range []string{"cave", "tcave", "README.md", "LICENSE"} {
			if _, ok := files[name+"/"+file]; !ok {
				t.Errorf("%s has no %s", name, file)
			}
		}
		for _, program := range []string{"cave", "tcave"} {
			f, err := macho.NewFile(bytes.NewReader(files[name+"/"+program]))
			if err != nil {
				t.Errorf("%s/%s is not a macOS program: %v", name, program, err)
				continue
			}
			if f.Cpu != cpu {
				t.Errorf("%s/%s is for %v, want %v", name, program, f.Cpu, cpu)
			}
			noCgo(t, name+"/"+program, files[name+"/"+program])
		}
	}
	for arch, machine := range map[string]uint16{"amd64": pe.IMAGE_FILE_MACHINE_AMD64, "arm64": pe.IMAGE_FILE_MACHINE_ARM64} {
		name := "cave-" + version + "-windows-" + arch
		files := unzip(t, filepath.Join(out, name+".zip"))
		for _, file := range []string{"cave.exe", "tcave.exe", "README.md", "LICENSE"} {
			if _, ok := files[name+"/"+file]; !ok {
				t.Errorf("%s has no %s", name, file)
			}
		}
		for _, program := range []string{"cave.exe", "tcave.exe"} {
			f, err := pe.NewFile(bytes.NewReader(files[name+"/"+program]))
			if err != nil {
				t.Errorf("%s/%s is not a Windows program: %v", name, program, err)
				continue
			}
			if f.Machine != machine {
				t.Errorf("%s/%s is for %#x, want %#x", name, program, f.Machine, machine)
			}
			noCgo(t, name+"/"+program, files[name+"/"+program])
		}
	}
}

// noCgo checks in the build information of a program that it was built without cgo. (The desktop
// program on Linux is not a static file all the same: Glutter loads the system's graphics
// libraries when it runs, without a C compiler, so the file asks for a dynamic loader.)
func noCgo(t *testing.T, name string, program []byte) {
	t.Helper()
	info, err := buildinfo.Read(bytes.NewReader(program))
	if err != nil {
		t.Errorf("%s has no build information: %v", name, err)
		return
	}
	for _, setting := range info.Settings {
		if setting.Key == "CGO_ENABLED" {
			if setting.Value != "0" {
				t.Errorf("%s was built with CGO_ENABLED=%s", name, setting.Value)
			}
			return
		}
	}
	t.Errorf("%s does not say whether it was built with cgo", name)
}

// untar returns the files of a .tar.gz by their names.
func untar(t *testing.T, path string) map[string][]byte {
	t.Helper()
	file, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = file.Close() }() // only read
	zr, err := gzip.NewReader(file)
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	tr := tar.NewReader(zr)
	for {
		header, err := tr.Next()
		if err == io.EOF {
			return files
		}
		if err != nil {
			t.Fatal(err)
		}
		if header.Typeflag != tar.TypeReg {
			continue
		}
		data, err := io.ReadAll(tr)
		if err != nil {
			t.Fatal(err)
		}
		files[header.Name] = data
	}
}

// unzip returns the files of a .zip by their names.
func unzip(t *testing.T, path string) map[string][]byte {
	t.Helper()
	zr, err := zip.OpenReader(path)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = zr.Close() }() // only read
	files := map[string][]byte{}
	for _, f := range zr.File {
		if f.FileInfo().IsDir() {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			t.Fatal(err)
		}
		data, err := io.ReadAll(rc)
		_ = rc.Close() // only read
		if err != nil {
			t.Fatal(err)
		}
		files[f.Name] = data
	}
	return files
}
