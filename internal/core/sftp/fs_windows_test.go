package sftp

import (
	"bytes"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/rm4n0s/cave/internal/core/sshtest"
)

// These tests run under Wine (scripts/test-windows.sh), on the real file system of Windows with a
// temporary folder as the home.

// windowsSession is a session on the file system of Windows, with a temporary folder as the home.
type windowsSession struct {
	*testSession
	// home is the home folder as Windows names it, and sftpHome as the client sees it.
	home, sftpHome string
}

func startWindows(t *testing.T) *windowsSession {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fileSystem, err := NewFileSystem(home)
	if err != nil {
		t.Fatal(err)
	}
	return &windowsSession{testSession: start(t, fileSystem), home: home, sftpHome: fromWindows(home)}
}

// put writes a file of the test under the home folder.
func (s *windowsSession) put(name, content string) {
	s.t.Helper()
	full := filepath.Join(s.home, filepath.FromSlash(name))
	if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
		s.t.Fatal(err)
	}
	if err := os.WriteFile(full, []byte(content), 0o600); err != nil {
		s.t.Fatal(err)
	}
}

// status sends a request and returns the code and the message of its status.
func (s *windowsSession) status(kind byte, fields ...any) (uint32, string) {
	s.t.Helper()
	reply := s.Call(kind, fields...)
	if reply.Type != sshtest.SFTPStatus {
		return sshtest.SFTPOK, ""
	}
	return reply.Status()
}

// list returns the names of a folder.
func (s *windowsSession) list(path string) []string {
	s.t.Helper()
	dir := s.Call(sshtest.SFTPOpendir, path).Handle()
	defer s.Close(dir)
	var names []string
	for {
		reply := s.Call(sshtest.SFTPReaddir, dir)
		if reply.Type == sshtest.SFTPStatus {
			if code := reply.Code(); code != sshtest.SFTPEOF {
				s.t.Fatalf("readdir: status %d", code)
			}
			sort.Strings(names)
			return names
		}
		for _, entry := range reply.Names() {
			names = append(names, entry.Name)
		}
	}
}

func TestAFileOfAMegabyteGoesBothWays(t *testing.T) {
	s := startWindows(t)
	data := make([]byte, 1<<20)
	for i := range data {
		data[i] = byte(i * 7)
	}
	handle := s.Open("big.bin", sshtest.SFTPOpenWrite|sshtest.SFTPOpenCreate|sshtest.SFTPOpenTrunc, sshtest.SFTPAttrs{})
	const chunk = 32 << 10
	for offset := 0; offset < len(data); offset += chunk {
		if code := s.Write(handle, uint64(offset), data[offset:offset+chunk]); code != sshtest.SFTPOK {
			t.Fatalf("write at %d: status %d", offset, code)
		}
	}
	s.Close(handle)
	if disk, err := os.ReadFile(filepath.Join(s.home, "big.bin")); err != nil || !bytes.Equal(disk, data) {
		t.Fatalf("the file on the disk is not what was sent (error %v, %d bytes)", err, len(disk))
	}
	handle = s.Open("big.bin", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
	var got []byte
	for {
		part, code := s.Read(handle, uint64(len(got)), chunk)
		if code == sshtest.SFTPEOF {
			break
		}
		if code != sshtest.SFTPOK {
			t.Fatalf("read at %d: status %d", len(got), code)
		}
		got = append(got, part...)
	}
	s.Close(handle)
	if !bytes.Equal(got, data) {
		t.Errorf("%d bytes came back, and they are not the %d that were sent", len(got), len(data))
	}
}

func TestListFoldersOnWindows(t *testing.T) {
	s := startWindows(t)
	s.put("dir/file.txt", "12345")
	s.put("dir/with space.txt", "")
	s.put("dir/sub/inner", "x")
	got := s.list("dir")
	want := []string{".", "..", "file.txt", "sub", "with space.txt"}
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("listed %v, want %v", got, want)
	}
	reply := s.Call(sshtest.SFTPStat, "dir/file.txt")
	if attrs := reply.Attrs(); attrs.Size != 5 || attrs.Mode&modeTypeMask != modeRegular || attrs.UID != 0 || attrs.GID != 0 {
		t.Errorf("the attributes of the file are %+v", attrs)
	}
	reply = s.Call(sshtest.SFTPStat, "dir/sub")
	if attrs := reply.Attrs(); attrs.Mode&modeTypeMask != modeDir {
		t.Errorf("the attributes of the folder are %+v", attrs)
	}
}

func TestMakeAndRemoveOnWindows(t *testing.T) {
	s := startWindows(t)
	s.put("file", "x")
	if code, message := s.status(sshtest.SFTPMkdir, "made", sshtest.SFTPAttrs{}); code != sshtest.SFTPOK {
		t.Fatalf("mkdir: %d %q", code, message)
	}
	if info, err := os.Stat(filepath.Join(s.home, "made")); err != nil || !info.IsDir() {
		t.Errorf("the folder was not made: %v", err)
	}
	if code, _ := s.status(sshtest.SFTPMkdir, "made", sshtest.SFTPAttrs{}); code != sshtest.SFTPFailure {
		t.Errorf("mkdir of a folder that is there: %d", code)
	}
	if code, _ := s.status(sshtest.SFTPRmdir, "file"); code == sshtest.SFTPOK {
		t.Error("rmdir removed a file")
	}
	if code, _ := s.status(sshtest.SFTPRemove, "made"); code == sshtest.SFTPOK {
		t.Error("remove removed a folder")
	}
	if code, message := s.status(sshtest.SFTPRmdir, "made"); code != sshtest.SFTPOK {
		t.Errorf("rmdir: %d %q", code, message)
	}
	if code, message := s.status(sshtest.SFTPRemove, "file"); code != sshtest.SFTPOK {
		t.Errorf("remove: %d %q", code, message)
	}
	if code, _ := s.status(sshtest.SFTPRemove, "file"); code != sshtest.SFTPNoSuchFile {
		t.Errorf("remove of nothing: %d", code)
	}
	if code, message := s.status(sshtest.SFTPOpendir, "missing"); code != sshtest.SFTPNoSuchFile {
		t.Errorf("opendir of nothing: %d %q", code, message)
	}
	s.put("plain", "x")
	if code, _ := s.status(sshtest.SFTPOpendir, "plain"); code != sshtest.SFTPNoSuchFile {
		t.Errorf("opendir of a file: %d", code)
	}
}

func TestRenameOnWindows(t *testing.T) {
	s := startWindows(t)
	s.put("a", "from")
	s.put("b", "target")
	// SFTP's RENAME does not replace.
	if code, _ := s.status(sshtest.SFTPRename, "a", "b"); code != sshtest.SFTPFailure {
		t.Errorf("rename onto a file that is there: %d, want a failure", code)
	}
	if data, _ := os.ReadFile(filepath.Join(s.home, "b")); string(data) != "target" {
		t.Errorf("b was changed: %q", data)
	}
	// posix-rename replaces.
	if code, message := s.status(sshtest.SFTPExtended, "posix-rename@openssh.com", "a", "b"); code != sshtest.SFTPOK {
		t.Fatalf("posix-rename: %d %q", code, message)
	}
	if data, _ := os.ReadFile(filepath.Join(s.home, "b")); string(data) != "from" {
		t.Errorf("b is %q after posix-rename", data)
	}
	if _, err := os.Stat(filepath.Join(s.home, "a")); err == nil {
		t.Error("a is still there")
	}
	if code, _ := s.status(sshtest.SFTPRename, "b", "c"); code != sshtest.SFTPOK {
		t.Errorf("rename to a new name: %d", code)
	}
	if code, _ := s.status(sshtest.SFTPRename, "missing", "d"); code != sshtest.SFTPNoSuchFile {
		t.Errorf("rename of nothing: %d", code)
	}
}

func TestStatOfNothingOnWindows(t *testing.T) {
	s := startWindows(t)
	for _, call := range []byte{sshtest.SFTPStat, sshtest.SFTPLstat} {
		if code, _ := s.status(call, "missing"); code != sshtest.SFTPNoSuchFile {
			t.Errorf("request %d of a file that is not there: %d", call, code)
		}
	}
	// A path without a drive is no place on Windows.
	if code, message := s.status(sshtest.SFTPStat, "/Users"); code != sshtest.SFTPNoSuchFile || message != "no such drive" {
		t.Errorf("stat of /Users: %d %q", code, message)
	}
}

func TestTheRootListsTheDrives(t *testing.T) {
	s := startWindows(t)
	names := s.list("/")
	has := map[string]bool{}
	for _, name := range names {
		has[name] = true
	}
	if !has["C:"] || !has["."] || !has[".."] {
		t.Errorf("/ lists %v, want at least ., .. and C:", names)
	}
	if attrs := s.Call(sshtest.SFTPStat, "/").Attrs(); attrs.Mode&modeTypeMask != modeDir {
		t.Errorf("/ has the attributes %+v", attrs)
	}
	// A drive is a folder that opens.
	if code, _ := s.status(sshtest.SFTPOpendir, "/C:/"); code != sshtest.SFTPOK {
		// OpenDir answers with a handle, which status reports as OK.
		t.Errorf("opendir of /C:/: %d", code)
	}
	if len(s.list("/C:")) < 2 {
		t.Error("/C: lists nothing")
	}
}

func TestNothingIsChangedInTheRoot(t *testing.T) {
	s := startWindows(t)
	for name, call := range map[string]func() (uint32, string){
		"mkdir":  func() (uint32, string) { return s.status(sshtest.SFTPMkdir, "/new", sshtest.SFTPAttrs{}) },
		"rmdir":  func() (uint32, string) { return s.status(sshtest.SFTPRmdir, "/") },
		"remove": func() (uint32, string) { return s.status(sshtest.SFTPRemove, "/") },
		"rename": func() (uint32, string) { return s.status(sshtest.SFTPRename, "/", "/x") },
		"open": func() (uint32, string) {
			return s.status(sshtest.SFTPOpen, "/", uint32(sshtest.SFTPOpenWrite), sshtest.SFTPAttrs{})
		},
		"setstat": func() (uint32, string) {
			return s.status(sshtest.SFTPSetstat, "/", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions})
		},
		"symlink": func() (uint32, string) { return s.status(sshtest.SFTPSymlink, "/C:/x", "/y") },
	} {
		if code, _ := call(); code != sshtest.SFTPPermissionDenied {
			t.Errorf("%s in /: status %d, want PERMISSION_DENIED", name, code)
		}
	}
}

func TestRealpathOnWindows(t *testing.T) {
	s := startWindows(t)
	s.put("folder/file", "x")
	tests := []struct {
		path     string
		want     string
		wantCode uint32
	}{
		{"", s.sftpHome, sshtest.SFTPOK},
		{".", s.sftpHome, sshtest.SFTPOK},
		{"folder", s.sftpHome + "/folder", sshtest.SFTPOK},
		{"folder/../folder/./file", s.sftpHome + "/folder/file", sshtest.SFTPOK},
		{"new", s.sftpHome + "/new", sshtest.SFTPOK},
		{"/", "/", sshtest.SFTPOK},
		{"/..", "/", sshtest.SFTPOK},
		{"/C:/..", "/C:/", sshtest.SFTPOK},
		{"missing/new", "", sshtest.SFTPNoSuchFile},
		{"/Users", "", sshtest.SFTPNoSuchFile},
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
		if len(names) != 1 || !strings.EqualFold(names[0].Name, tt.want) || tt.wantCode != sshtest.SFTPOK {
			t.Errorf("realpath %q = %+v, want %q", tt.path, names, tt.want)
		}
	}
}

func TestSetstatOfPermissionsIsTheReadOnlyBit(t *testing.T) {
	s := startWindows(t)
	s.put("file", "x")
	full := filepath.Join(s.home, "file")
	setMode := func(mode uint32) uint32 {
		code, _ := s.status(sshtest.SFTPSetstat, "file", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: mode})
		return code
	}
	if code := setMode(0o444); code != sshtest.SFTPOK {
		t.Fatalf("setstat: %d", code)
	}
	if info, err := os.Stat(full); err != nil || info.Mode().Perm()&0o200 != 0 {
		t.Errorf("the file is not read-only: %v %v", info, err)
	}
	if attrs := s.Call(sshtest.SFTPStat, "file").Attrs(); attrs.Mode&0o777 != 0o444 {
		t.Errorf("stat says the permissions are %o", attrs.Mode&0o777)
	}
	if code, _ := s.status(sshtest.SFTPOpen, "file", uint32(sshtest.SFTPOpenWrite), sshtest.SFTPAttrs{}); code != sshtest.SFTPPermissionDenied {
		t.Errorf("opening a read-only file for writing: %d, want PERMISSION_DENIED", code)
	}
	if code := setMode(0o644); code != sshtest.SFTPOK {
		t.Fatalf("setstat: %d", code)
	}
	if info, err := os.Stat(full); err != nil || info.Mode().Perm()&0o200 == 0 {
		t.Errorf("the file is still read-only: %v %v", info, err)
	}
	// The owner and the group are accepted, and nothing is done.
	if code, _ := s.status(sshtest.SFTPSetstat, "file", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrUIDGID, UID: 1000, GID: 1000}); code != sshtest.SFTPOK {
		t.Errorf("setstat of the owner: %d", code)
	}
	if attrs := s.Call(sshtest.SFTPStat, "file").Attrs(); attrs.UID != 0 || attrs.GID != 0 {
		t.Errorf("the owner is %d:%d, want 0:0", attrs.UID, attrs.GID)
	}
}

func TestSetstatOfTheSizeAndTheTimes(t *testing.T) {
	s := startWindows(t)
	s.put("file", "0123456789")
	when := uint32(1700000000)
	code, message := s.status(sshtest.SFTPSetstat, "file", sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize | sshtest.SFTPAttrTimes, Size: 4, Atime: when, Mtime: when})
	if code != sshtest.SFTPOK {
		t.Fatalf("setstat: %d %q", code, message)
	}
	attrs := s.Call(sshtest.SFTPStat, "file").Attrs()
	if attrs.Size != 4 || attrs.Mtime != when {
		t.Errorf("the attributes are %+v", attrs)
	}
	// By handle too.
	handle := s.Open("file", sshtest.SFTPOpenRead|sshtest.SFTPOpenWrite, sshtest.SFTPAttrs{})
	defer s.Close(handle)
	later := when + 1000
	if code, message := s.status(sshtest.SFTPFsetstat, handle, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize | sshtest.SFTPAttrTimes, Size: 2, Atime: later, Mtime: later}); code != sshtest.SFTPOK {
		t.Fatalf("fsetstat: %d %q", code, message)
	}
	if attrs := s.Call(sshtest.SFTPStat, "file").Attrs(); attrs.Size != 2 || attrs.Mtime != later {
		t.Errorf("the attributes after fsetstat are %+v", attrs)
	}
}

func TestWhatWindowsDoesNotDo(t *testing.T) {
	s := startWindows(t)
	s.put("file", "x")
	handle := s.Open("file", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
	defer s.Close(handle)
	if code, _ := s.status(sshtest.SFTPExtended, "statvfs@openssh.com", "file"); code != sshtest.SFTPOpUnsupported {
		t.Errorf("statvfs: %d, want OP_UNSUPPORTED", code)
	}
	if code, _ := s.status(sshtest.SFTPExtended, "fstatvfs@openssh.com", handle); code != sshtest.SFTPOpUnsupported {
		t.Errorf("fstatvfs: %d, want OP_UNSUPPORTED", code)
	}
}

func TestLinksOnWindows(t *testing.T) {
	s := startWindows(t)
	s.put("target", "x")
	// Whether Windows lets the user make links depends on the privilege and the file system: it
	// does, or the answer is OP_UNSUPPORTED, and never another failure.
	code, message := s.status(sshtest.SFTPSymlink, "target", "link")
	switch code {
	case sshtest.SFTPOK:
		reply := s.Call(sshtest.SFTPReadlink, "link")
		if names := reply.Names(); len(names) != 1 || !strings.HasSuffix(names[0].Name, "target") {
			t.Errorf("readlink: %+v", names)
		}
	case sshtest.SFTPOpUnsupported:
	default:
		t.Errorf("symlink: %d %q", code, message)
	}
	code, message = s.status(sshtest.SFTPExtended, "hardlink@openssh.com", "target", "hard")
	if code != sshtest.SFTPOK && code != sshtest.SFTPOpUnsupported {
		t.Errorf("hardlink: %d %q", code, message)
	}
	if code == sshtest.SFTPOK {
		if data, err := os.ReadFile(filepath.Join(s.home, "hard")); err != nil || string(data) != "x" {
			t.Errorf("the hard link holds %q (%v)", data, err)
		}
	}
}

func TestStatusOfOnWindows(t *testing.T) {
	tests := []struct {
		err  error
		want uint32
	}{
		{nil, sshtest.SFTPOK},
		{os.ErrNotExist, sshtest.SFTPNoSuchFile},
		{os.ErrPermission, sshtest.SFTPPermissionDenied},
		{os.ErrInvalid, sshtest.SFTPFailure},
	}
	for _, tt := range tests {
		if got := statusOf(tt.err).Code; got != tt.want {
			t.Errorf("statusOf(%v) = %d, want %d", tt.err, got, tt.want)
		}
	}
}
