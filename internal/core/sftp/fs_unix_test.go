//go:build linux || darwin

package sftp

import (
	"bytes"
	"fmt"
	"math/rand"
	"os"
	"os/user"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"syscall"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/sshtest"
	"golang.org/x/sys/unix"
)

// unixSession is a session on the real file system, with a temporary folder as the home.
type unixSession struct {
	*testSession
	home string
	// fs is the file system the session runs on.
	fs *unixFS
}

// startLinux starts a session whose home folder is a temporary folder of the test. Everything
// the tests name is inside it.
func startLinux(t *testing.T) *unixSession {
	t.Helper()
	// The path of a temporary folder may go through a link (macOS, some Linux setups).
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fileSystem, err := NewFileSystem(home)
	if err != nil {
		t.Fatal(err)
	}
	return &unixSession{testSession: start(t, fileSystem), home: home, fs: fileSystem.(*unixFS)}
}

// put writes a file of the test under the home folder.
func (s *unixSession) put(name, content string, mode os.FileMode) string {
	s.t.Helper()
	full := filepath.Join(s.home, name)
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), mode); err != nil {
		s.t.Fatal(err)
	}
	if err := os.Chmod(full, mode); err != nil { // whatever the umask is
		s.t.Fatal(err)
	}
	return full
}

// content reads a file under the home folder.
func (s *unixSession) content(name string) string {
	s.t.Helper()
	data, err := os.ReadFile(filepath.Join(s.home, name))
	if err != nil {
		s.t.Fatal(err)
	}
	return string(data)
}

// status sends a request and returns the code and the message of its status.
func (s *unixSession) status(kind byte, fields ...any) (uint32, string) {
	s.t.Helper()
	reply := s.Call(kind, fields...)
	if reply.Type != sshtest.SFTPStatus {
		return sshtest.SFTPOK, ""
	}
	return reply.Status()
}

// notRoot skips a test of permissions when the tests run as root, who is refused nothing.
func notRoot(t *testing.T) {
	t.Helper()
	if os.Getuid() == 0 {
		t.Skip("root is not refused by file permissions")
	}
}

// openFiles counts the files this process has open.
func openFiles(t *testing.T) int {
	t.Helper()
	entries, err := os.ReadDir(fdDir)
	if err != nil {
		t.Fatal(err)
	}
	return len(entries)
}

const (
	read      = sshtest.SFTPOpenRead
	write     = sshtest.SFTPOpenWrite
	appending = sshtest.SFTPOpenAppend
	create    = sshtest.SFTPOpenCreate
	trunc     = sshtest.SFTPOpenTrunc
	excl      = sshtest.SFTPOpenExcl
)

func TestOpenFlags(t *testing.T) {
	notRoot(t)
	tests := []struct {
		name  string
		path  string
		flags uint32
		// wantOpen is the status of the open; the rest is checked when it succeeds.
		wantOpen    uint32
		wantMessage string
		// canRead and canWrite say what the handle allows; wantAfter is the file's content
		// after writing "NEW" at offset 1 (when it can be written).
		canRead, canWrite bool
		wantAfter         string
	}{
		{name: "read", path: "old", flags: read, canRead: true},
		{name: "no flags at all reads", path: "old", flags: 0, canRead: true},
		{name: "write", path: "old", flags: write, canWrite: true, wantAfter: "0NEW456789"},
		{name: "read and write", path: "old", flags: read | write, canRead: true, canWrite: true, wantAfter: "0NEW456789"},
		{name: "append ignores the offset", path: "old", flags: write | appending, canWrite: true, wantAfter: "0123456789NEW"},
		{name: "read and append", path: "old", flags: read | write | appending, canRead: true, canWrite: true, wantAfter: "0123456789NEW"},
		{name: "truncate", path: "old", flags: write | trunc, canWrite: true, wantAfter: "\x00NEW"},
		{name: "create a new file", path: "new", flags: write | create, canWrite: true, wantAfter: "\x00NEW"},
		{name: "create a file that is there", path: "old", flags: write | create, canWrite: true, wantAfter: "0NEW456789"},
		{name: "create and truncate", path: "old", flags: read | write | create | trunc, canRead: true, canWrite: true, wantAfter: "\x00NEW"},
		{name: "create exclusively", path: "new", flags: write | create | excl, canWrite: true, wantAfter: "\x00NEW"},
		{name: "create exclusively a file that is there", path: "old", flags: write | create | excl, wantOpen: sshtest.SFTPFailure, wantMessage: "file exists"},
		{name: "exclusively without create", path: "old", flags: read | excl, canRead: true},
		{name: "a file that is not there", path: "new", flags: read, wantOpen: sshtest.SFTPNoSuchFile, wantMessage: "no such file or directory"},
		{name: "write a file that is not there", path: "new", flags: write | trunc, wantOpen: sshtest.SFTPNoSuchFile, wantMessage: "no such file or directory"},
		{name: "a file in a folder that is not there", path: "nowhere/new", flags: write | create, wantOpen: sshtest.SFTPNoSuchFile, wantMessage: "no such file or directory"},
		{name: "a file under a file", path: "old/new", flags: read, wantOpen: sshtest.SFTPNoSuchFile, wantMessage: "not a directory"},
		{name: "a file that may not be read", path: "secret", flags: read, wantOpen: sshtest.SFTPPermissionDenied, wantMessage: "permission denied"},
		{name: "a file that may not be written", path: "readonly", flags: write, wantOpen: sshtest.SFTPPermissionDenied, wantMessage: "permission denied"},
		{name: "a new file in a folder that may not be written", path: "locked/new", flags: write | create, wantOpen: sshtest.SFTPPermissionDenied, wantMessage: "permission denied"},
		{name: "a link that goes in circles", path: "loop", flags: read, wantOpen: sshtest.SFTPNoSuchFile, wantMessage: "too many levels of symbolic links"},
		{name: "an absolute path", path: "", flags: read, canRead: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := startLinux(t)
			old := s.put("old", "0123456789", 0o600)
			s.put("secret", "x", 0o000)
			s.put("readonly", "x", 0o400)
			s.put("locked/file", "x", 0o600)
			if err := os.Chmod(filepath.Join(s.home, "locked"), 0o500); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = os.Chmod(filepath.Join(s.home, "locked"), 0o700) }) // so that the folder can be removed
			if err := os.Symlink("loop", filepath.Join(s.home, "loop")); err != nil {
				t.Fatal(err)
			}
			path := tt.path
			if path == "" {
				path = old
			}
			reply := s.Call(sshtest.SFTPOpen, path, tt.flags, sshtest.SFTPAttrs{})
			if reply.Type == sshtest.SFTPStatus {
				code, message := reply.Status()
				if code != tt.wantOpen || message != tt.wantMessage || tt.wantOpen == sshtest.SFTPOK {
					t.Fatalf("open: status %d %q, want %d %q", code, message, tt.wantOpen, tt.wantMessage)
				}
				return
			}
			if tt.wantOpen != sshtest.SFTPOK {
				t.Fatalf("the open succeeded, want the status %d", tt.wantOpen)
			}
			handle := reply.Handle()
			code := s.Write(handle, 1, []byte("NEW"))
			if tt.canWrite != (code == sshtest.SFTPOK) {
				t.Errorf("write: status %d, want it to succeed: %v", code, tt.canWrite)
			}
			if tt.canWrite {
				if got := s.content(filepath.Base(path)); got != tt.wantAfter {
					t.Errorf("the file has %q, want %q", got, tt.wantAfter)
				}
			}
			data, code := s.Read(handle, 0, 100)
			if tt.canRead != (code == sshtest.SFTPOK) {
				t.Errorf("read: status %d, want it to succeed: %v", code, tt.canRead)
			}
			if tt.canRead && string(data) != s.content(filepath.Base(path)) {
				t.Errorf("read %q, the file has %q", data, s.content(filepath.Base(path)))
			}
			s.Close(handle)
		})
	}
}

func TestTheModeOfANewFile(t *testing.T) {
	s := startLinux(t)
	// The permissions asked are cut by the umask, as with any program.
	umask := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(umask) })
	tests := []struct {
		name  string
		attrs sshtest.SFTPAttrs
		want  os.FileMode
	}{
		{"none asked", sshtest.SFTPAttrs{}, 0o644},
		{"private", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o600}, 0o600},
		{"with the type bits a client sends", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o100640}, 0o640},
		{"more than the umask allows", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o777}, 0o755},
		{"other attributes are not used", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize | sshtest.SFTPAttrTimes, Size: 99, Atime: 1, Mtime: 1}, 0o644},
	}
	for i, tt := range tests {
		name := "file" + string(rune('a'+i))
		s.Close(s.Open(name, write|create|excl, tt.attrs))
		info, err := os.Stat(filepath.Join(s.home, name))
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != tt.want || info.Size() != 0 {
			t.Errorf("%s: the file has the mode %o and %d bytes, want %o and none", tt.name, info.Mode().Perm(), info.Size(), tt.want)
		}
	}
}

func TestReadAndWriteAtOffsets(t *testing.T) {
	s := startLinux(t)
	s.put("file", "0123456789", 0o600)
	handle := s.Open("file", read|write, sshtest.SFTPAttrs{})
	tests := []struct {
		offset   uint64
		length   uint32
		want     string
		wantCode uint32
	}{
		{0, 4, "0123", sshtest.SFTPOK},
		{6, 100, "6789", sshtest.SFTPOK},
		{10, 1, "", sshtest.SFTPEOF},
		{1 << 40, 1, "", sshtest.SFTPEOF},
		{0, 0, "", sshtest.SFTPEOF},
		{1 << 63, 1, "", sshtest.SFTPFailure}, // more than the system's offsets go to
	}
	for _, tt := range tests {
		data, code := s.Read(handle, tt.offset, tt.length)
		if string(data) != tt.want || code != tt.wantCode {
			t.Errorf("read %d at %d: %q, status %d", tt.length, tt.offset, data, code)
		}
	}
	// A write past the end leaves a hole of zeros.
	if code := s.Write(handle, 12, []byte("AB")); code != sshtest.SFTPOK {
		t.Errorf("write: status %d", code)
	}
	if code := s.Write(handle, 1<<63, []byte("x")); code != sshtest.SFTPFailure {
		t.Errorf("a write at an offset the system does not have: status %d", code)
	}
	if code := s.Write(handle, 0, nil); code != sshtest.SFTPOK {
		t.Errorf("a write of nothing: status %d", code)
	}
	if got := s.content("file"); got != "0123456789\x00\x00AB" {
		t.Errorf("the file has %q", got)
	}
	s.Close(handle)
}

func TestALargeFileInChunksOutOfOrder(t *testing.T) {
	s := startLinux(t)
	rng := rand.New(rand.NewSource(31))
	const size = 5<<20 + 12345
	want := make([]byte, size)
	rng.Read(want)

	const chunk = 32 << 10
	var offsets []int
	for offset := 0; offset < size; offset += chunk {
		offsets = append(offsets, offset)
	}
	rng.Shuffle(len(offsets), func(i, j int) { offsets[i], offsets[j] = offsets[j], offsets[i] })
	handle := s.Open("big.bin", write|create|excl, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o600})
	for _, offset := range offsets {
		end := min(offset+chunk, size)
		if code := s.Write(handle, uint64(offset), want[offset:end]); code != sshtest.SFTPOK {
			t.Fatalf("write at %d: status %d", offset, code)
		}
	}
	s.Close(handle)
	if got := s.content("big.bin"); got != string(want) {
		t.Fatalf("the file on the disk has %d bytes that differ from the %d written", len(got), size)
	}

	// Read it back in another order, asking for more than a read gives.
	got := make([]byte, size)
	rng.Shuffle(len(offsets), func(i, j int) { offsets[i], offsets[j] = offsets[j], offsets[i] })
	handle = s.Open("big.bin", read, sshtest.SFTPAttrs{})
	for _, offset := range offsets {
		data, code := s.Read(handle, uint64(offset), chunk)
		if code != sshtest.SFTPOK || len(data) != min(chunk, size-offset) {
			t.Fatalf("read at %d: %d bytes, status %d", offset, len(data), code)
		}
		copy(got[offset:], data)
	}
	if !bytes.Equal(got, want) {
		t.Error("what was read differs from what was written")
	}
	// Reading from the start as a client does when it gets less than it asked for.
	read := 0
	for {
		data, code := s.Read(handle, uint64(read), 1<<20)
		if code == sshtest.SFTPEOF {
			break
		}
		if len(data) > MaxRead || !bytes.Equal(data, want[read:read+len(data)]) {
			t.Fatalf("read at %d: %d bytes", read, len(data))
		}
		read += len(data)
	}
	if read != size {
		t.Errorf("read %d bytes in a row, want %d", read, size)
	}
	s.Close(handle)
}

func TestStatLstatAndFstat(t *testing.T) {
	s := startLinux(t)
	full := s.put("file", "12345", 0o640)
	when := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	if err := os.Chtimes(full, when.Add(time.Hour), when); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(s.home, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("nowhere", filepath.Join(s.home, "broken")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(s.home, "folder"), 0o750); err != nil {
		t.Fatal(err)
	}
	all := uint32(sshtest.SFTPAttrSize | sshtest.SFTPAttrUIDGID | sshtest.SFTPAttrPermissions | sshtest.SFTPAttrTimes)
	file := sshtest.SFTPAttrs{
		Flags: all, Size: 5, UID: uint32(os.Getuid()), GID: uint32(os.Getgid()), Mode: syscall.S_IFREG | 0o640,
		Atime: uint32(when.Add(time.Hour).Unix()), Mtime: uint32(when.Unix()),
	}
	if got := s.Call(sshtest.SFTPStat, "file").Attrs(); got.Extended != nil || !reflect.DeepEqual(got, file) {
		t.Errorf("stat of the file = %+v, want %+v", got, file)
	}
	if got := s.Call(sshtest.SFTPLstat, "file").Attrs(); got.Extended != nil || !reflect.DeepEqual(got, file) {
		t.Errorf("lstat of the file = %+v, want %+v", got, file)
	}
	// stat follows a link, lstat shows the link itself.
	if got := s.Call(sshtest.SFTPStat, "link").Attrs(); got.Extended != nil || !reflect.DeepEqual(got, file) {
		t.Errorf("stat of the link = %+v, want %+v", got, file)
	}
	if got := s.Call(sshtest.SFTPLstat, "link").Attrs(); got.Mode&syscall.S_IFMT != syscall.S_IFLNK || got.Size != 4 {
		t.Errorf("lstat of the link = %+v", got)
	}
	if code, message := s.status(sshtest.SFTPStat, "broken"); code != sshtest.SFTPNoSuchFile || message != "no such file or directory" {
		t.Errorf("stat of a broken link: %d %q", code, message)
	}
	if got := s.Call(sshtest.SFTPLstat, "broken").Attrs(); got.Mode&syscall.S_IFMT != syscall.S_IFLNK {
		t.Errorf("lstat of a broken link = %+v", got)
	}
	if got := s.Call(sshtest.SFTPStat, "folder").Attrs(); got.Mode != syscall.S_IFDIR|0o750 {
		t.Errorf("stat of the folder: mode %o", got.Mode)
	}
	for _, kind := range []byte{sshtest.SFTPStat, sshtest.SFTPLstat} {
		if code, message := s.status(kind, "missing"); code != sshtest.SFTPNoSuchFile || message != "no such file or directory" {
			t.Errorf("request %d of a missing file: %d %q", kind, code, message)
		}
	}
	// The empty path and "." are the home folder.
	for _, path := range []string{"", "."} {
		if got := s.Call(sshtest.SFTPStat, path).Attrs(); got.Mode&syscall.S_IFMT != syscall.S_IFDIR {
			t.Errorf("stat of %q = %+v", path, got)
		}
	}
	handle := s.Open("link", read, sshtest.SFTPAttrs{})
	if got := s.Call(sshtest.SFTPFstat, handle).Attrs(); got.Extended != nil || !reflect.DeepEqual(got, file) {
		t.Errorf("fstat = %+v, want %+v", got, file)
	}
	s.Close(handle)
}

func TestSetstatAndFsetstat(t *testing.T) {
	for name, set := range map[string]func(s *unixSession, attrs sshtest.SFTPAttrs) (uint32, string){
		"setstat": func(s *unixSession, attrs sshtest.SFTPAttrs) (uint32, string) {
			return s.status(sshtest.SFTPSetstat, "file", attrs)
		},
		"fsetstat": func(s *unixSession, attrs sshtest.SFTPAttrs) (uint32, string) {
			handle := s.Open("file", read|write, sshtest.SFTPAttrs{})
			defer s.Close(handle)
			return s.status(sshtest.SFTPFsetstat, handle, attrs)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s := startLinux(t)
			full := s.put("file", "0123456789", 0o600)
			stat := func() (os.FileInfo, *syscall.Stat_t) {
				info, err := os.Stat(full)
				if err != nil {
					t.Fatal(err)
				}
				return info, info.Sys().(*syscall.Stat_t)
			}

			if code, message := set(s, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize, Size: 4}); code != sshtest.SFTPOK {
				t.Errorf("size: %d %q", code, message)
			}
			if got := s.content("file"); got != "0123" {
				t.Errorf("after a smaller size the file has %q", got)
			}
			if code, _ := set(s, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize, Size: 6}); code != sshtest.SFTPOK || s.content("file") != "0123\x00\x00" {
				t.Errorf("a larger size: status %d, the file has %q", code, s.content("file"))
			}
			if code, message := set(s, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize, Size: 1 << 63}); code != sshtest.SFTPFailure || message != "invalid argument" {
				t.Errorf("a size the system does not have: %d %q", code, message)
			}

			if code, message := set(s, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: syscall.S_IFREG | 0o4751}); code != sshtest.SFTPOK {
				t.Errorf("permissions: %d %q", code, message)
			}
			if info, _ := stat(); info.Mode().Perm() != 0o751 || info.Mode()&os.ModeSetuid == 0 {
				t.Errorf("the mode is %v", info.Mode())
			}
			if code, _ := set(s, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o600}); code != sshtest.SFTPOK {
				t.Errorf("permissions back: status %d", code)
			}

			if code, message := set(s, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrTimes, Atime: 1000000000, Mtime: 1234567890}); code != sshtest.SFTPOK {
				t.Errorf("times: %d %q", code, message)
			}
			if _, st := stat(); atimeOf(st) != 1000000000 || mtimeOf(st) != 1234567890 {
				t.Errorf("the times are %d and %d", atimeOf(st), mtimeOf(st))
			}

			// The owner: to who owns it already is allowed, to somebody else is not.
			own := sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrUIDGID, UID: uint32(os.Getuid()), GID: uint32(os.Getgid())}
			if code, message := set(s, own); code != sshtest.SFTPOK {
				t.Errorf("the owner it has: %d %q", code, message)
			}
			if os.Getuid() != 0 {
				other := sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrUIDGID, UID: 0, GID: 0}
				if code, message := set(s, other); code != sshtest.SFTPPermissionDenied || message != "operation not permitted" {
					t.Errorf("another owner: %d %q", code, message)
				}
				// Everything is tried, and the first failure is the status.
				mixed := sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize | sshtest.SFTPAttrUIDGID | sshtest.SFTPAttrPermissions, Size: 2, Mode: 0o640}
				if code, _ := set(s, mixed); code != sshtest.SFTPPermissionDenied {
					t.Errorf("a change with one part refused: status %d", code)
				}
				if info, _ := stat(); info.Size() != 2 || info.Mode().Perm() != 0o640 {
					t.Errorf("the parts that were allowed: %d bytes, mode %v", info.Size(), info.Mode())
				}
			}
			// No attributes at all changes nothing.
			if code, _ := set(s, sshtest.SFTPAttrs{}); code != sshtest.SFTPOK {
				t.Errorf("no attributes: status %d", code)
			}
		})
	}
	s := startLinux(t)
	if code, message := s.status(sshtest.SFTPSetstat, "missing", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o600}); code != sshtest.SFTPNoSuchFile || message != "no such file or directory" {
		t.Errorf("setstat of a missing file: %d %q", code, message)
	}
}

func TestFsetstatOfTheTimesFollowsTheOpenFile(t *testing.T) {
	s := startLinux(t)
	full := s.put("file", "x", 0o600)
	handle := s.Open("file", read|write, sshtest.SFTPAttrs{})
	// The file gets another name while it is open: the handle is still the file.
	moved := filepath.Join(s.home, "moved")
	if err := os.Rename(full, moved); err != nil {
		t.Fatal(err)
	}
	if code, message := s.status(sshtest.SFTPFsetstat, handle, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrTimes, Atime: 111, Mtime: 222}); code != sshtest.SFTPOK {
		t.Fatalf("fsetstat: %d %q", code, message)
	}
	s.Close(handle)
	info, err := os.Stat(moved)
	if err != nil {
		t.Fatal(err)
	}
	if st := info.Sys().(*syscall.Stat_t); atimeOf(st) != 111 || mtimeOf(st) != 222 {
		t.Errorf("the times are %d and %d", atimeOf(st), mtimeOf(st))
	}
}

func TestRemove(t *testing.T) {
	notRoot(t)
	s := startLinux(t)
	s.put("file", "x", 0o600)
	s.put("locked/file", "x", 0o600)
	if err := os.Chmod(filepath.Join(s.home, "locked"), 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(filepath.Join(s.home, "locked"), 0o700) })
	if err := os.Mkdir(filepath.Join(s.home, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("file", filepath.Join(s.home, "link")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path        string
		wantCode    uint32
		wantMessage string
	}{
		{"link", sshtest.SFTPOK, "Success"}, // the link, not what it leads to
		{"file", sshtest.SFTPOK, "Success"},
		{"file", sshtest.SFTPNoSuchFile, "no such file or directory"},
		{"folder", removeFolderCode, removeFolderMessage},
		{"locked/file", sshtest.SFTPPermissionDenied, "permission denied"},
		{"file/under", sshtest.SFTPNoSuchFile, "no such file or directory"},
	}
	for _, tt := range tests {
		if code, message := s.status(sshtest.SFTPRemove, tt.path); code != tt.wantCode || message != tt.wantMessage {
			t.Errorf("remove %q: %d %q, want %d %q", tt.path, code, message, tt.wantCode, tt.wantMessage)
		}
	}
	if _, err := os.Stat(filepath.Join(s.home, "folder")); err != nil {
		t.Errorf("the folder is gone: %v", err)
	}
	if _, err := os.Stat(filepath.Join(s.home, "locked", "file")); err != nil {
		t.Errorf("the file in the locked folder is gone: %v", err)
	}
}

func TestRealpath(t *testing.T) {
	s := startLinux(t)
	s.put("folder/file", "x", 0o600)
	if err := os.Symlink("folder", filepath.Join(s.home, "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("loop", filepath.Join(s.home, "loop")); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path     string
		want     string
		wantCode uint32
	}{
		{"", s.home, sshtest.SFTPOK},
		{".", s.home, sshtest.SFTPOK},
		{"folder", s.home + "/folder", sshtest.SFTPOK},
		{"folder/", s.home + "/folder", sshtest.SFTPOK},
		{"folder/./file", s.home + "/folder/file", sshtest.SFTPOK},
		{"folder/../folder/file", s.home + "/folder/file", sshtest.SFTPOK},
		{"link/file", s.home + "/folder/file", sshtest.SFTPOK},
		{"link/..", s.home, sshtest.SFTPOK},
		{"..", filepath.Dir(s.home), sshtest.SFTPOK},
		{"/", "/", sshtest.SFTPOK},
		{"/..", "/", sshtest.SFTPOK},
		{s.home + "/folder", s.home + "/folder", sshtest.SFTPOK},
		// The last part need not be there; a client asks before it makes a file.
		{"new", s.home + "/new", sshtest.SFTPOK},
		{"link/new", s.home + "/folder/new", sshtest.SFTPOK},
		{"folder/new/", s.home + "/folder/new", sshtest.SFTPOK},
		{"missing/new", "", sshtest.SFTPNoSuchFile},
		{"missing/..", "", sshtest.SFTPNoSuchFile},
		{"folder/file/under", "", sshtest.SFTPNoSuchFile},
		{"loop", "", sshtest.SFTPNoSuchFile},
	}
	for _, tt := range tests {
		reply := s.Call(sshtest.SFTPRealpath, tt.path)
		if reply.Type == sshtest.SFTPStatus {
			if code := reply.Code(); code != tt.wantCode || tt.wantCode == sshtest.SFTPOK {
				t.Errorf("realpath %q: status %d, want %d", tt.path, code, tt.wantCode)
			}
			continue
		}
		names := reply.Names()
		if len(names) != 1 || names[0].Name != tt.want || names[0].LongName != tt.want || tt.wantCode != sshtest.SFTPOK {
			t.Errorf("realpath %q = %+v, want %q", tt.path, names, tt.want)
		}
	}
}

func TestAHomeThatIsNotAbsoluteIsTheRoot(t *testing.T) {
	for _, home := range []string{"", "relative/home"} {
		fileSystem, err := NewFileSystem(home)
		if err != nil {
			t.Fatal(err)
		}
		if got, status := fileSystem.RealPath("."); got != "/" || !status.OK() {
			t.Errorf("home %q: the start is %q, %+v", home, got, status)
		}
	}
}

func TestFoldersAndPipesAreNotFiles(t *testing.T) {
	s := startLinux(t)
	if err := os.Mkdir(filepath.Join(s.home, "folder"), 0o700); err != nil {
		t.Fatal(err)
	}
	// A folder opens, as the system lets it, and cannot be read as a file.
	handle := s.Open("folder", read, sshtest.SFTPAttrs{})
	if code, message := s.status(sshtest.SFTPRead, handle, uint64(0), uint32(10)); code != sshtest.SFTPFailure || message != "is a directory" {
		t.Errorf("reading a folder: %d %q", code, message)
	}
	s.Close(handle)
	if code, message := s.status(sshtest.SFTPOpen, "folder", uint32(write), sshtest.SFTPAttrs{}); code != sshtest.SFTPFailure || message != "is a directory" {
		t.Errorf("opening a folder for writing: %d %q", code, message)
	}

	// A pipe nobody writes to: the open does not wait for a writer, and so cannot hang the
	// session.
	pipe := filepath.Join(s.home, "pipe")
	if err := unix.Mkfifo(pipe, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		reply := s.Call(sshtest.SFTPOpen, "pipe", uint32(read), sshtest.SFTPAttrs{})
		if reply.Type != sshtest.SFTPHandle {
			return
		}
		handle := reply.Handle()
		if code, _ := s.status(sshtest.SFTPRead, handle, uint64(0), uint32(10)); code != sshtest.SFTPFailure {
			t.Errorf("reading a pipe: status %d", code)
		}
		s.Close(handle)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("opening a pipe hangs the session")
	}
}

func TestTheEndOfASessionClosesItsFiles(t *testing.T) {
	before := openFiles(t)
	s := startLinux(t)
	s.put("file", "x", 0o600)
	for i := 0; i < 10; i++ {
		s.Open("file", read, sshtest.SFTPAttrs{})
	}
	if during := openFiles(t); during < before+10 {
		t.Fatalf("%d files are open, %d before the session", during, before)
	}
	if err := s.end(); err != nil {
		t.Errorf("Serve returned %v", err)
	}
	if after := openFiles(t); after != before {
		t.Errorf("%d files are open after the session, %d before", after, before)
	}
}

func TestStatusOf(t *testing.T) {
	tests := []struct {
		err  error
		want Status
	}{
		{nil, Status{}},
		{syscall.ENOENT, Status{Code: StatusNoSuchFile, Message: "no such file or directory"}},
		{syscall.ENOTDIR, Status{Code: StatusNoSuchFile, Message: "not a directory"}},
		{syscall.ELOOP, Status{Code: StatusNoSuchFile, Message: "too many levels of symbolic links"}},
		{syscall.EACCES, Status{Code: StatusPermissionDenied, Message: "permission denied"}},
		{syscall.EPERM, Status{Code: StatusPermissionDenied, Message: "operation not permitted"}},
		{syscall.ENOSYS, Status{Code: StatusOpUnsupported, Message: "function not implemented"}},
		{syscall.ENOTSUP, Status{Code: StatusOpUnsupported, Message: "operation not supported"}},
		{syscall.ENOSPC, Status{Code: StatusFailure, Message: "no space left on device"}},
		{syscall.EEXIST, Status{Code: StatusFailure, Message: "file exists"}},
		{&os.PathError{Op: "open", Path: "/secret/path", Err: syscall.EISDIR}, Status{Code: StatusFailure, Message: "is a directory"}},
		// An error that is not the system's: the code's own message, and nothing of the error.
		{os.ErrClosed, Status{Code: StatusFailure}},
	}
	for _, tt := range tests {
		if got := statusOf(tt.err); got != tt.want {
			t.Errorf("statusOf(%v) = %+v, want %+v", tt.err, got, tt.want)
		}
	}
}

func TestAttrsOfAFileInfoThatIsNotTheSystems(t *testing.T) {
	when := time.Unix(1700000000, 0)
	got := attrsOf(fakeInfo{size: 12, when: when})
	if want := (Attrs{Flags: AttrSize | AttrTimes, Size: 12, Atime: 1700000000, Mtime: 1700000000}); got != want {
		t.Errorf("attrsOf = %+v, want %+v", got, want)
	}
	// Times the protocol cannot say are cut to what it can.
	if seconds(-5) != 0 || seconds(1<<40) != 0xffffffff || seconds(77) != 77 {
		t.Errorf("seconds: %d %d %d", seconds(-5), seconds(1<<40), seconds(77))
	}
}

// fakeInfo is a FileInfo that does not come from the system.
type fakeInfo struct {
	size int64
	when time.Time
}

func (f fakeInfo) Name() string       { return "fake" }
func (f fakeInfo) Size() int64        { return f.size }
func (f fakeInfo) Mode() os.FileMode  { return 0 }
func (f fakeInfo) ModTime() time.Time { return f.when }
func (f fakeInfo) IsDir() bool        { return false }
func (f fakeInfo) Sys() any           { return nil }

// list returns every entry of a folder, by name.
func (s *unixSession) list(path string) map[string]sshtest.SFTPEntry {
	s.t.Helper()
	dir := s.Call(sshtest.SFTPOpendir, path).Handle()
	defer s.Close(dir)
	entries := map[string]sshtest.SFTPEntry{}
	for {
		reply := s.Call(sshtest.SFTPReaddir, dir)
		if reply.Type == sshtest.SFTPStatus {
			if code := reply.Code(); code != sshtest.SFTPEOF {
				s.t.Fatalf("readdir: status %d", code)
			}
			return entries
		}
		for _, entry := range reply.Names() {
			if _, twice := entries[entry.Name]; twice {
				s.t.Errorf("the name %q was listed twice", entry.Name)
			}
			entries[entry.Name] = entry
		}
	}
}

// me returns the names of the user and the group the tests run as, as a listing shows them.
func me(t *testing.T) (owner, group string) {
	t.Helper()
	owner, group = strconv.Itoa(os.Getuid()), strconv.Itoa(os.Getgid())
	if u, err := user.LookupId(owner); err == nil {
		owner = u.Username
	}
	if g, err := user.LookupGroupId(group); err == nil {
		group = g.Name
	}
	return owner, group
}

func TestListAFolder(t *testing.T) {
	s := startLinux(t)
	now := time.Date(2026, 11, 24, 12, 0, 0, 0, time.Local)
	s.fs.now = func() time.Time { return now }
	recent, old := now.Add(-48*time.Hour), now.AddDate(-2, 0, 0)
	s.put("dir/file.txt", "12345", 0o640)
	s.put("dir/.hidden", "", 0o600)
	s.put("dir/sub/inner", "x", 0o600)
	if err := os.Symlink("file.txt", filepath.Join(s.home, "dir", "link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(s.home, "dir", "sub"), 0o750); err != nil {
		t.Fatal(err)
	}
	for name, when := range map[string]time.Time{"file.txt": recent, ".hidden": old, "sub": recent} {
		if err := os.Chtimes(filepath.Join(s.home, "dir", name), when, when); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(filepath.Join(s.home, "dir"), recent, recent); err != nil {
		t.Fatal(err)
	}

	entries := s.list("dir")
	var names []string
	for name := range entries {
		names = append(names, name)
	}
	sort.Strings(names)
	if want := []string{".", "..", ".hidden", "file.txt", "link", "sub"}; !reflect.DeepEqual(names, want) {
		t.Fatalf("listed %v, want %v", names, want)
	}
	owner, group := me(t)
	want := map[string]string{
		"file.txt": fmt.Sprintf("-rw-r-----    1 %-8s %-8s        5 %s file.txt", owner, group, recent.Format("Jan _2 15:04")),
		".hidden":  fmt.Sprintf("-rw-------    1 %-8s %-8s        0 %s .hidden", owner, group, old.Format("Jan _2  2006")),
	}
	for name, line := range want {
		if got := entries[name].LongName; got != line {
			t.Errorf("the long name of %s:\n got %q\nwant %q", name, got, line)
		}
	}
	// The attributes are those of the name itself: of the link, not of what it points to.
	if got := entries["file.txt"].Attrs; got.Size != 5 || got.Mode != syscall.S_IFREG|0o640 || got.UID != uint32(os.Getuid()) || got.Mtime != uint32(recent.Unix()) {
		t.Errorf("the attributes of the file are %+v", got)
	}
	if got := entries["link"]; got.Attrs.Mode&syscall.S_IFMT != syscall.S_IFLNK || got.LongName[0] != 'l' {
		t.Errorf("the link is listed as %+v", got)
	}
	if got := entries["sub"]; got.Attrs.Mode != syscall.S_IFDIR|0o750 || got.LongName[:10] != "drwxr-x---" {
		t.Errorf("the folder is listed as %+v", got)
	}
	// "." is the folder and ".." the one above, as OpenSSH lists them.
	if got := entries["."]; got.Attrs.Mode&syscall.S_IFMT != syscall.S_IFDIR || got.Attrs.Mtime != uint32(recent.Unix()) {
		t.Errorf(". is listed as %+v", got)
	}
	if got := entries[".."]; got.Attrs.Mode&syscall.S_IFMT != syscall.S_IFDIR {
		t.Errorf(".. is listed as %+v", got)
	}
}

func TestListAFolderOfManyFiles(t *testing.T) {
	s := startLinux(t)
	want := map[string]bool{".": true, "..": true}
	for i := 0; i < 250; i++ {
		name := fmt.Sprintf("file-%03d", i)
		s.put("many/"+name, "", 0o600)
		want[name] = true
	}
	got := s.list("many")
	if len(got) != len(want) {
		t.Errorf("listed %d names, want %d", len(got), len(want))
	}
	for name := range want {
		if _, ok := got[name]; !ok {
			t.Errorf("%s was not listed", name)
		}
	}
	// An empty folder has the two names every folder has.
	if err := os.Mkdir(filepath.Join(s.home, "empty"), 0o700); err != nil {
		t.Fatal(err)
	}
	if got := s.list("empty"); len(got) != 2 {
		t.Errorf("an empty folder lists %d names", len(got))
	}
}

func TestOpendirRefusals(t *testing.T) {
	notRoot(t)
	s := startLinux(t)
	s.put("file", "x", 0o600)
	if err := os.Mkdir(filepath.Join(s.home, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		path        string
		wantCode    uint32
		wantMessage string
	}{
		{"file", sshtest.SFTPNoSuchFile, "not a directory"},
		{"missing", sshtest.SFTPNoSuchFile, "no such file or directory"},
		{"locked", sshtest.SFTPPermissionDenied, "permission denied"},
	}
	for _, tt := range tests {
		if code, message := s.status(sshtest.SFTPOpendir, tt.path); code != tt.wantCode || message != tt.wantMessage {
			t.Errorf("opendir %q: %d %q, want %d %q", tt.path, code, message, tt.wantCode, tt.wantMessage)
		}
	}
}

func TestMkdirAndRmdir(t *testing.T) {
	s := startLinux(t)
	umask := unix.Umask(0o022)
	t.Cleanup(func() { unix.Umask(umask) })
	s.put("file", "x", 0o600)
	s.put("full/inner", "x", 0o600)
	steps := []struct {
		name        string
		kind        byte
		fields      []any
		wantCode    uint32
		wantMessage string
	}{
		{"mkdir", sshtest.SFTPMkdir, []any{"new", sshtest.SFTPAttrs{}}, sshtest.SFTPOK, "Success"},
		{"mkdir with a mode", sshtest.SFTPMkdir, []any{"private", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: syscall.S_IFDIR | 0o700}}, sshtest.SFTPOK, "Success"},
		{"mkdir of a name that is taken", sshtest.SFTPMkdir, []any{"new", sshtest.SFTPAttrs{}}, sshtest.SFTPFailure, "file exists"},
		{"mkdir over a file", sshtest.SFTPMkdir, []any{"file", sshtest.SFTPAttrs{}}, sshtest.SFTPFailure, "file exists"},
		{"mkdir in a folder that is not there", sshtest.SFTPMkdir, []any{"nowhere/new", sshtest.SFTPAttrs{}}, sshtest.SFTPNoSuchFile, "no such file or directory"},
		{"rmdir of a folder that is not empty", sshtest.SFTPRmdir, []any{"full"}, sshtest.SFTPFailure, "directory not empty"},
		{"rmdir of a file", sshtest.SFTPRmdir, []any{"file"}, sshtest.SFTPNoSuchFile, "not a directory"},
		{"rmdir of nothing", sshtest.SFTPRmdir, []any{"missing"}, sshtest.SFTPNoSuchFile, "no such file or directory"},
		{"rmdir", sshtest.SFTPRmdir, []any{"new"}, sshtest.SFTPOK, "Success"},
		{"remove of a folder is not rmdir", sshtest.SFTPRemove, []any{"private"}, removeFolderCode, removeFolderMessage},
	}
	modes := map[string]os.FileMode{}
	for _, step := range steps {
		code, message := s.Call(step.kind, step.fields...).Status()
		if code != step.wantCode || message != step.wantMessage {
			t.Errorf("%s: %d %q, want %d %q", step.name, code, message, step.wantCode, step.wantMessage)
		}
		for _, name := range []string{"new", "private"} {
			if info, err := os.Stat(filepath.Join(s.home, name)); err == nil && info.IsDir() {
				modes[name] = info.Mode().Perm()
			}
		}
	}
	if modes["new"] != 0o755 || modes["private"] != 0o700 {
		t.Errorf("the folders were made with the modes %v", modes)
	}
	for name, want := range map[string]bool{"new": false, "private": true, "full/inner": true, "file": true} {
		if _, err := os.Stat(filepath.Join(s.home, name)); (err == nil) != want {
			t.Errorf("%s is there: %v, want %v", name, err == nil, want)
		}
	}
}

func TestRename(t *testing.T) {
	for name, noReplace := range map[string]func(oldPath, newPath string) error{
		"in one step": nil,
		// A file system that cannot rename without replacing: Cave looks first, then renames.
		"on a file system without it":  func(string, string) error { return unix.EINVAL },
		"on a system that has not got": func(string, string) error { return unix.ENOSYS },
	} {
		t.Run(name, func(t *testing.T) {
			s := startLinux(t)
			if noReplace != nil {
				s.fs.renameNoReplace = noReplace
			}
			s.put("a", "content of a", 0o600)
			s.put("taken", "content of taken", 0o600)
			s.put("dir/inner", "x", 0o600)
			s.put("other/inner", "y", 0o600)
			if err := os.Symlink("a", filepath.Join(s.home, "link")); err != nil {
				t.Fatal(err)
			}
			steps := []struct {
				name        string
				from, to    string
				wantCode    uint32
				wantMessage string
			}{
				{"onto a file that is there", "a", "taken", sshtest.SFTPFailure, "file exists"},
				{"a folder onto a folder that is there", "dir", "other", sshtest.SFTPFailure, "file exists"},
				{"onto itself", "a", "a", sshtest.SFTPFailure, "file exists"},
				{"a file that is not there", "missing", "b", sshtest.SFTPNoSuchFile, "no such file or directory"},
				{"into a folder that is not there", "a", "nowhere/b", sshtest.SFTPNoSuchFile, "no such file or directory"},
				{"a file", "a", "b", sshtest.SFTPOK, "Success"},
				{"a folder", "dir", "moved", sshtest.SFTPOK, "Success"},
				{"a link, not what it points to", "link", "link2", sshtest.SFTPOK, "Success"},
				{"into another folder", "b", "moved/b", sshtest.SFTPOK, "Success"},
			}
			for _, step := range steps {
				if code, message := s.Call(sshtest.SFTPRename, step.from, step.to).Status(); code != step.wantCode || message != step.wantMessage {
					t.Errorf("%s: %d %q, want %d %q", step.name, code, message, step.wantCode, step.wantMessage)
				}
			}
			if got := s.content("taken"); got != "content of taken" {
				t.Errorf("the file whose name was asked for has %q", got)
			}
			if got := s.content("moved/b"); got != "content of a" {
				t.Errorf("the renamed file has %q", got)
			}
			if got := s.content("moved/inner") + s.content("other/inner"); got != "xy" {
				t.Errorf("the folders have %q", got)
			}
			if target, err := os.Readlink(filepath.Join(s.home, "link2")); err != nil || target != "a" {
				t.Errorf("the renamed link points to %q, %v", target, err)
			}
			for _, gone := range []string{"a", "b", "dir", "link"} {
				if _, err := os.Lstat(filepath.Join(s.home, gone)); err == nil {
					t.Errorf("%s is still there", gone)
				}
			}
		})
	}
}

func TestRenameWhenTheNewNameCannotBeLookedAt(t *testing.T) {
	notRoot(t)
	s := startLinux(t)
	s.fs.renameNoReplace = func(string, string) error { return unix.EINVAL }
	s.put("a", "x", 0o600)
	if err := os.Mkdir(filepath.Join(s.home, "locked"), 0o000); err != nil {
		t.Fatal(err)
	}
	// Whether the name is taken cannot be seen: the rename is refused, not tried.
	if code, message := s.status(sshtest.SFTPRename, "a", "locked/b"); code != sshtest.SFTPPermissionDenied || message != "permission denied" {
		t.Errorf("rename: %d %q", code, message)
	}
	if got := s.content("a"); got != "x" {
		t.Errorf("the file has %q", got)
	}
}

func TestPosixRenameReplaces(t *testing.T) {
	s := startLinux(t)
	s.put("a", "content of a", 0o600)
	s.put("taken", "content of taken", 0o600)
	rename := func(from, to string) (uint32, string) {
		return s.Call(sshtest.SFTPExtended, "posix-rename@openssh.com", from, to).Status()
	}
	if code, message := rename("a", "taken"); code != sshtest.SFTPOK {
		t.Errorf("posix-rename: %d %q", code, message)
	}
	if got := s.content("taken"); got != "content of a" {
		t.Errorf("the file has %q", got)
	}
	if _, err := os.Lstat(filepath.Join(s.home, "a")); err == nil {
		t.Error("the old name is still there")
	}
	if code, message := rename("missing", "b"); code != sshtest.SFTPNoSuchFile || message != "no such file or directory" {
		t.Errorf("posix-rename of nothing: %d %q", code, message)
	}
}

func TestSymlinkAndReadlink(t *testing.T) {
	s := startLinux(t)
	s.put("file", "content", 0o600)
	steps := []struct {
		name         string
		target, link string
		wantCode     uint32
		wantMessage  string
	}{
		// What the link points to comes first, and is kept as it is written.
		{"a link to a file", "file", "link", sshtest.SFTPOK, "Success"},
		{"a link to nothing", "../nowhere/at all", "dangling", sshtest.SFTPOK, "Success"},
		{"a link with an absolute target", "/etc/hostname", "absolute", sshtest.SFTPOK, "Success"},
		{"a name that is taken", "file", "link", sshtest.SFTPFailure, "file exists"},
		{"in a folder that is not there", "file", "nowhere/link", sshtest.SFTPNoSuchFile, "no such file or directory"},
	}
	for _, step := range steps {
		if code, message := s.Call(sshtest.SFTPSymlink, step.target, step.link).Status(); code != step.wantCode || message != step.wantMessage {
			t.Errorf("%s: %d %q, want %d %q", step.name, code, message, step.wantCode, step.wantMessage)
		}
	}
	if got := s.content("link"); got != "content" {
		t.Errorf("through the link the file has %q", got)
	}
	for link, want := range map[string]string{"link": "file", "dangling": "../nowhere/at all", "absolute": "/etc/hostname"} {
		if target, err := os.Readlink(filepath.Join(s.home, link)); err != nil || target != want {
			t.Errorf("%s points to %q, %v", link, target, err)
		}
		names := s.Call(sshtest.SFTPReadlink, link).Names()
		if len(names) != 1 || names[0].Name != want || names[0].LongName != want || names[0].Attrs.Flags != 0 {
			t.Errorf("readlink %s = %+v", link, names)
		}
	}
	if code, message := s.status(sshtest.SFTPReadlink, "file"); code != sshtest.SFTPFailure || message != "invalid argument" {
		t.Errorf("readlink of a file: %d %q", code, message)
	}
	if code, message := s.status(sshtest.SFTPReadlink, "missing"); code != sshtest.SFTPNoSuchFile || message != "no such file or directory" {
		t.Errorf("readlink of nothing: %d %q", code, message)
	}
}

func TestHardlink(t *testing.T) {
	s := startLinux(t)
	full := s.put("file", "content", 0o600)
	s.put("taken", "x", 0o600)
	if err := os.Mkdir(filepath.Join(s.home, "dir"), 0o700); err != nil {
		t.Fatal(err)
	}
	link := func(from, to string) (uint32, string) {
		return s.Call(sshtest.SFTPExtended, "hardlink@openssh.com", from, to).Status()
	}
	if code, message := link("file", "twin"); code != sshtest.SFTPOK {
		t.Fatalf("hardlink: %d %q", code, message)
	}
	first, err := os.Stat(full)
	if err != nil {
		t.Fatal(err)
	}
	second, err := os.Stat(filepath.Join(s.home, "twin"))
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(first, second) || first.Sys().(*syscall.Stat_t).Nlink != 2 {
		t.Error("the new name is not the same file")
	}
	tests := []struct {
		from, to    string
		wantCode    uint32
		wantMessage string
	}{
		{"file", "taken", sshtest.SFTPFailure, "file exists"},
		{"missing", "other", sshtest.SFTPNoSuchFile, "no such file or directory"},
		{"dir", "dir2", sshtest.SFTPPermissionDenied, "operation not permitted"},
	}
	for _, tt := range tests {
		if code, message := link(tt.from, tt.to); code != tt.wantCode || message != tt.wantMessage {
			t.Errorf("hardlink %s %s: %d %q, want %d %q", tt.from, tt.to, code, message, tt.wantCode, tt.wantMessage)
		}
	}
}

func TestFsyncAndStatvfs(t *testing.T) {
	s := startLinux(t)
	s.put("file", "content", 0o600)
	handle := s.Open("file", read|write, sshtest.SFTPAttrs{})
	if code, message := s.Call(sshtest.SFTPExtended, "fsync@openssh.com", handle).Status(); code != sshtest.SFTPOK {
		t.Errorf("fsync: %d %q", code, message)
	}

	var stat unix.Statfs_t
	if err := unix.Statfs(s.home, &stat); err != nil {
		t.Fatal(err)
	}
	// What does not change while the test runs: the sizes, the totals, the id, the longest name.
	check := func(name string, got []uint64) {
		t.Helper()
		if len(got) != 11 {
			t.Fatalf("%s answered %d numbers, want 11", name, len(got))
		}
		if got[0] != uint64(stat.Bsize) || got[1] == 0 || got[2] != stat.Blocks || (fixedFileTotal && got[5] != stat.Files) || got[10] != vfsOf(&stat).NameMax {
			t.Errorf("%s = %v, the system says %+v", name, got, stat)
		}
		if got[3] > got[2] || got[4] > got[3] || got[7] != got[6] || got[9]&^3 != 0 {
			t.Errorf("%s = %v", name, got)
		}
	}
	check("statvfs", s.Call(sshtest.SFTPExtended, "statvfs@openssh.com", ".").Uint64s())
	check("fstatvfs", s.Call(sshtest.SFTPExtended, "fstatvfs@openssh.com", handle).Uint64s())
	if code, message := s.Call(sshtest.SFTPExtended, "statvfs@openssh.com", "missing").Status(); code != sshtest.SFTPNoSuchFile || message != "no such file or directory" {
		t.Errorf("statvfs of nothing: %d %q", code, message)
	}
	s.Close(handle)
}

func TestTheNamesOfUsersAndGroups(t *testing.T) {
	fileSystem, err := NewFileSystem("/")
	if err != nil {
		t.Fatal(err)
	}
	l := fileSystem.(*unixFS)
	asked := 0
	lookup := func(id string) (string, error) {
		asked++
		if id == "7" {
			return "seven", nil
		}
		return "", os.ErrNotExist
	}
	cache := map[uint32]string{}
	// A name is asked for once; an id without a name is shown as its number.
	for i := 0; i < 3; i++ {
		if got := l.name(cache, 7, lookup); got != "seven" {
			t.Errorf("the name of 7 is %q", got)
		}
		if got := l.name(cache, 4000000000, lookup); got != "4000000000" {
			t.Errorf("the name of an id nobody has is %q", got)
		}
	}
	if asked != 2 {
		t.Errorf("the system was asked %d times, want 2", asked)
	}
	// The real lookups: the user the tests run as, and an id nobody has.
	owner, group := me(t)
	if got := l.name(l.owners, uint32(os.Getuid()), userName); got != owner {
		t.Errorf("the user is %q, want %q", got, owner)
	}
	if got := l.name(l.groups, uint32(os.Getgid()), groupName); got != group {
		t.Errorf("the group is %q, want %q", got, group)
	}
	if got := l.name(l.owners, 4000000001, userName); got != "4000000001" {
		t.Errorf("a user nobody has is %q", got)
	}
	if got := l.name(l.groups, 4000000001, groupName); got != "4000000001" {
		t.Errorf("a group nobody has is %q", got)
	}
}
