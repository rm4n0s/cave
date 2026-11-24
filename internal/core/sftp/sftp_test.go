package sftp

import (
	"bytes"
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"log/slog"
	"math/rand"
	"path"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rm4n0s/cave/internal/core/errtest"
	"github.com/rm4n0s/cave/internal/core/sshtest"
	"github.com/rm4n0s/errors"
)

// stream is one direction of the connection between a test and the server. A write never
// waits, so a test may send several packets before it reads an answer.
type stream struct {
	mu     sync.Mutex
	cond   *sync.Cond
	buf    []byte
	closed bool
}

func newStream() *stream {
	s := &stream{}
	s.cond = sync.NewCond(&s.mu)
	return s
}

func (s *stream) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return 0, io.ErrClosedPipe
	}
	s.buf = append(s.buf, p...)
	s.cond.Broadcast()
	return len(p), nil
}

func (s *stream) Read(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for len(s.buf) == 0 {
		if s.closed {
			return 0, io.EOF
		}
		s.cond.Wait()
	}
	n := copy(p, s.buf)
	s.buf = s.buf[n:]
	return n, nil
}

func (s *stream) Close() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.closed = true
	s.cond.Broadcast()
}

// duplex is one end of a connection: it reads one stream and writes the other.
type duplex struct {
	in, out *stream
}

func (d duplex) Read(p []byte) (int, error)  { return d.in.Read(p) }
func (d duplex) Write(p []byte) (int, error) { return d.out.Write(p) }

// testSession is a session of the server with a test as its client.
type testSession struct {
	*sshtest.SFTPClient
	t *testing.T
	// toServer is what the client writes; closing it is how a client ends a session.
	toServer, toClient *stream
	// result gets what Serve returned.
	result chan error
	log    *bytes.Buffer
}

// serve starts a session on a file system; the client has not sent INIT yet.
func serve(t *testing.T, fileSystem FileSystem) *testSession {
	t.Helper()
	s := &testSession{t: t, toServer: newStream(), toClient: newStream(), result: make(chan error, 1), log: &bytes.Buffer{}}
	logger := slog.New(slog.NewJSONHandler(s.log, &slog.HandlerOptions{Level: slog.LevelDebug}))
	server := New(Options{FS: fileSystem, Logger: logger})
	go func() {
		err := server.Serve(context.Background(), duplex{in: s.toServer, out: s.toClient})
		s.toClient.Close()
		s.result <- err
	}()
	s.SFTPClient = sshtest.NewSFTPClient(t, duplex{in: s.toClient, out: s.toServer})
	t.Cleanup(func() { s.toServer.Close() })
	return s
}

// start starts a session whose client has sent INIT.
func start(t *testing.T, fileSystem FileSystem) *testSession {
	t.Helper()
	s := serve(t, fileSystem)
	if version, _ := s.Init(); version != Version {
		t.Fatalf("the server speaks version %d", version)
	}
	return s
}

// end ends the session as a client does and returns what Serve returned.
func (s *testSession) end() error {
	s.t.Helper()
	s.toServer.Close()
	return s.wait()
}

// wait returns what Serve returned, once it has.
func (s *testSession) wait() error {
	s.t.Helper()
	select {
	case err := <-s.result:
		s.result <- err
		return err
	case <-time.After(10 * time.Second):
		s.t.Fatal("the session did not end")
		return nil
	}
}

// memFS is a file system in memory: files by their path, no folders. It counts what is open
// and remembers the largest buffer a read was given.
type memFS struct {
	mu      sync.Mutex
	files   map[string]*memData
	open    int
	maxRead int
}

type memData struct {
	data  []byte
	attrs Attrs
}

func newMemFS() *memFS {
	return &memFS{files: map[string]*memData{}}
}

func (m *memFS) put(name string, data string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.files[name] = &memData{data: []byte(data), attrs: Attrs{Flags: AttrPermissions, Mode: 0o100644}}
}

func (m *memFS) content(name string) (string, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[name]
	if !ok {
		return "", false
	}
	return string(file.data), true
}

func (m *memFS) opened() int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.open
}

func (m *memFS) Open(name string, flags uint32, attrs Attrs) (File, Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[name]
	switch {
	case !ok && flags&OpenCreate == 0:
		return nil, Status{Code: StatusNoSuchFile}
	case ok && flags&OpenExcl != 0:
		return nil, Status{Code: StatusFailure, Message: "file exists"}
	case strings.HasPrefix(name, "/forbidden"):
		return nil, Status{Code: StatusPermissionDenied}
	case !ok:
		file = &memData{attrs: Attrs{Flags: AttrPermissions, Mode: 0o100000 | attrs.Mode&0o777}}
		m.files[name] = file
	}
	if flags&OpenTrunc != 0 {
		file.data = nil
	}
	m.open++
	return &memFile{fs: m, file: file, flags: flags}, Status{}
}

func (m *memFS) Stat(name string) (Attrs, Status) {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[name]
	if !ok {
		return Attrs{}, Status{Code: StatusNoSuchFile}
	}
	attrs := file.attrs
	attrs.Flags |= AttrSize
	attrs.Size = uint64(len(file.data))
	return attrs, Status{}
}

func (m *memFS) Lstat(name string) (Attrs, Status) { return m.Stat(name) }

func (m *memFS) SetStat(name string, attrs Attrs) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	file, ok := m.files[name]
	if !ok {
		return Status{Code: StatusNoSuchFile}
	}
	file.set(attrs)
	return Status{}
}

func (d *memData) set(attrs Attrs) {
	if attrs.Flags&AttrSize != 0 && attrs.Size < 1<<20 {
		d.data = append(d.data[:min(len(d.data), int(attrs.Size))], make([]byte, max(0, int(attrs.Size)-len(d.data)))...)
	}
	if attrs.Flags&AttrPermissions != 0 {
		d.attrs.Mode = d.attrs.Mode&^0o7777 | attrs.Mode&0o7777
	}
	if attrs.Flags&AttrTimes != 0 {
		d.attrs.Flags |= AttrTimes
		d.attrs.Atime, d.attrs.Mtime = attrs.Atime, attrs.Mtime
	}
	if attrs.Flags&AttrUIDGID != 0 {
		d.attrs.Flags |= AttrUIDGID
		d.attrs.UID, d.attrs.GID = attrs.UID, attrs.GID
	}
}

func (m *memFS) Remove(name string) Status {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.files[name]; !ok {
		return Status{Code: StatusNoSuchFile}
	}
	delete(m.files, name)
	return Status{}
}

func (m *memFS) RealPath(name string) (string, Status) {
	if strings.Contains(name, "\x00") {
		return "", Status{Code: StatusFailure, Message: "invalid argument"}
	}
	return path.Join("/home/tester", name), Status{}
}

type memFile struct {
	fs     *memFS
	file   *memData
	flags  uint32
	closed bool
}

func (f *memFile) ReadAt(p []byte, offset uint64) (int, Status) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	f.fs.maxRead = max(f.fs.maxRead, len(p))
	if f.flags&OpenRead == 0 {
		return 0, Status{Code: StatusFailure, Message: "bad file descriptor"}
	}
	if offset >= uint64(len(f.file.data)) {
		return 0, Status{}
	}
	return copy(p, f.file.data[offset:]), Status{}
}

func (f *memFile) WriteAt(p []byte, offset uint64) Status {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if f.flags&OpenWrite == 0 {
		return Status{Code: StatusFailure, Message: "bad file descriptor"}
	}
	if f.flags&OpenAppend != 0 {
		offset = uint64(len(f.file.data))
	}
	if offset > 1<<20 {
		return Status{Code: StatusFailure, Message: "file too large"}
	}
	if end := int(offset) + len(p); end > len(f.file.data) {
		f.file.data = append(f.file.data, make([]byte, end-len(f.file.data))...)
	}
	copy(f.file.data[offset:], p)
	return Status{}
}

func (f *memFile) Stat() (Attrs, Status) {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	attrs := f.file.attrs
	attrs.Flags |= AttrSize
	attrs.Size = uint64(len(f.file.data))
	return attrs, Status{}
}

func (f *memFile) SetStat(attrs Attrs) Status {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	f.file.set(attrs)
	return Status{}
}

func (f *memFile) Close() Status {
	f.fs.mu.Lock()
	defer f.fs.mu.Unlock()
	if !f.closed {
		f.closed = true
		f.fs.open--
	}
	return Status{}
}

func TestInit(t *testing.T) {
	s := serve(t, newMemFS())
	// A client of a later version, with extensions of its own, is answered with version 3.
	s.SendPacket(sshtest.SFTPInit, sshtest.SFTPPack(uint32(6), "some-extension@example.com", "1"))
	reply := s.Recv()
	if reply.Type != sshtest.SFTPVersion || len(reply.Body) < 4 || binary.BigEndian.Uint32(reply.Body) != 3 {
		t.Fatalf("the answer to INIT is %+v", reply)
	}
	if err := s.end(); err != nil {
		t.Errorf("Serve returned %v", err)
	}
}

func TestTheVersionListsTheExtensions(t *testing.T) {
	s := serve(t, newMemFS())
	_, got := s.Init()
	want := map[string]string{}
	for _, extension := range extensions {
		want[extension.name] = extension.version
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("the extensions are %v, want %v", got, want)
	}
}

func TestRequestsOnAFileSystemInMemory(t *testing.T) {
	fs := newMemFS()
	fs.put("/notes.txt", "0123456789")
	s := start(t, fs)

	// Open, read, the end of the file, close.
	handle := s.Open("/notes.txt", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
	if data, _ := s.Read(handle, 2, 4); string(data) != "2345" {
		t.Errorf("read %q", data)
	}
	if data, _ := s.Read(handle, 8, 100); string(data) != "89" {
		t.Errorf("a read past the end returned %q", data)
	}
	if _, code := s.Read(handle, 10, 100); code != sshtest.SFTPEOF {
		t.Errorf("a read at the end: status %d", code)
	}
	if _, code := s.Read(handle, 0, 0); code != sshtest.SFTPEOF {
		t.Errorf("a read of nothing: status %d", code)
	}
	if code := s.Write(handle, 0, []byte("x")); code != sshtest.SFTPFailure {
		t.Errorf("a write to a file opened for reading: status %d", code)
	}
	if got := s.Call(sshtest.SFTPFstat, handle).Attrs(); got.Size != 10 || got.Mode != 0o100644 {
		t.Errorf("fstat = %+v", got)
	}
	s.Close(handle)
	for _, kind := range []byte{sshtest.SFTPClose, sshtest.SFTPFstat} {
		if code, message := s.Call(kind, handle).Status(); code != sshtest.SFTPFailure || message != "invalid handle" {
			t.Errorf("request %d on a closed handle: %d %q", kind, code, message)
		}
	}
	if _, code := s.Read(handle, 0, 1); code != sshtest.SFTPFailure {
		t.Errorf("a read on a closed handle: status %d", code)
	}
	if code := s.Write(handle, 0, []byte("x")); code != sshtest.SFTPFailure {
		t.Errorf("a write on a closed handle: status %d", code)
	}
	if code := s.Call(sshtest.SFTPFsetstat, handle, sshtest.SFTPAttrs{}).Code(); code != sshtest.SFTPFailure {
		t.Errorf("fsetstat on a closed handle: status %d", code)
	}

	// Create, write, append, attributes.
	handle = s.Open("/new.txt", sshtest.SFTPOpenWrite|sshtest.SFTPOpenCreate|sshtest.SFTPOpenExcl, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrPermissions, Mode: 0o600})
	if code := s.Write(handle, 3, []byte("def")); code != sshtest.SFTPOK {
		t.Errorf("write: status %d", code)
	}
	if code := s.Write(handle, 0, []byte("abc")); code != sshtest.SFTPOK {
		t.Errorf("write: status %d", code)
	}
	if code := s.Call(sshtest.SFTPFsetstat, handle, sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrTimes, Atime: 11, Mtime: 22}).Code(); code != sshtest.SFTPOK {
		t.Errorf("fsetstat: status %d", code)
	}
	s.Close(handle)
	if got, _ := fs.content("/new.txt"); got != "abcdef" {
		t.Errorf("the file has %q", got)
	}
	for _, kind := range []byte{sshtest.SFTPStat, sshtest.SFTPLstat} {
		got := s.Call(kind, "/new.txt").Attrs()
		want := sshtest.SFTPAttrs{Flags: sshtest.SFTPAttrSize | sshtest.SFTPAttrPermissions | sshtest.SFTPAttrTimes, Size: 6, Mode: 0o100600, Atime: 11, Mtime: 22}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("request %d = %+v, want %+v", kind, got, want)
		}
	}
	if code := s.Call(sshtest.SFTPOpen, "/new.txt", sshtest.SFTPOpenWrite|sshtest.SFTPOpenCreate|sshtest.SFTPOpenExcl, sshtest.SFTPAttrs{}).Code(); code != sshtest.SFTPFailure {
		t.Errorf("an exclusive open of a file that is there: status %d", code)
	}
	handle = s.Open("/new.txt", sshtest.SFTPOpenWrite|sshtest.SFTPOpenAppend, sshtest.SFTPAttrs{})
	if code := s.Write(handle, 0, []byte("ghi")); code != sshtest.SFTPOK {
		t.Errorf("append: status %d", code)
	}
	s.Close(handle)
	if got, _ := fs.content("/new.txt"); got != "abcdefghi" {
		t.Errorf("after an append the file has %q", got)
	}
	handle = s.Open("/new.txt", sshtest.SFTPOpenWrite|sshtest.SFTPOpenTrunc, sshtest.SFTPAttrs{})
	s.Close(handle)
	if got, _ := fs.content("/new.txt"); got != "" {
		t.Errorf("after a truncating open the file has %q", got)
	}

	// Attributes by path, with extended ones that are dropped.
	attrs := sshtest.SFTPAttrs{
		Flags: sshtest.SFTPAttrSize | sshtest.SFTPAttrUIDGID | sshtest.SFTPAttrPermissions | sshtest.SFTPAttrTimes | sshtest.SFTPAttrExtended,
		Size:  4, UID: 1000, GID: 100, Mode: 0o640, Atime: 5, Mtime: 6, Extended: []string{"name", "value", "other", ""},
	}
	if code := s.Call(sshtest.SFTPSetstat, "/new.txt", attrs).Code(); code != sshtest.SFTPOK {
		t.Errorf("setstat: status %d", code)
	}
	got := s.Call(sshtest.SFTPStat, "/new.txt").Attrs()
	attrs.Flags, attrs.Extended, attrs.Mode = attrs.Flags&^sshtest.SFTPAttrExtended, nil, 0o100640
	if !reflect.DeepEqual(got, attrs) {
		t.Errorf("stat = %+v, want %+v", got, attrs)
	}

	// The path of a name.
	names := s.Call(sshtest.SFTPRealpath, "docs/../notes.txt").Names()
	if len(names) != 1 || names[0].Name != "/home/tester/notes.txt" || names[0].LongName != names[0].Name || names[0].Attrs.Flags != 0 {
		t.Errorf("realpath = %+v", names)
	}

	// Remove.
	if code := s.Call(sshtest.SFTPRemove, "/new.txt").Code(); code != sshtest.SFTPOK {
		t.Errorf("remove: status %d", code)
	}
	if _, ok := fs.content("/new.txt"); ok {
		t.Error("the file is still there")
	}

	if err := s.end(); err != nil {
		t.Errorf("Serve returned %v", err)
	}
}

func TestStatuses(t *testing.T) {
	fs := newMemFS()
	fs.put("/forbidden/file", "x")
	s := start(t, fs)
	tests := []struct {
		name        string
		kind        byte
		fields      []any
		wantCode    uint32
		wantMessage string
	}{
		{"open a file that is not there", sshtest.SFTPOpen, []any{"/missing", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{}}, sshtest.SFTPNoSuchFile, "No such file"},
		{"open a file that may not be read", sshtest.SFTPOpen, []any{"/forbidden/file", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{}}, sshtest.SFTPPermissionDenied, "Permission denied"},
		{"stat", sshtest.SFTPStat, []any{"/missing"}, sshtest.SFTPNoSuchFile, "No such file"},
		{"lstat", sshtest.SFTPLstat, []any{"/missing"}, sshtest.SFTPNoSuchFile, "No such file"},
		{"setstat", sshtest.SFTPSetstat, []any{"/missing", sshtest.SFTPAttrs{}}, sshtest.SFTPNoSuchFile, "No such file"},
		{"remove", sshtest.SFTPRemove, []any{"/missing"}, sshtest.SFTPNoSuchFile, "No such file"},
		{"realpath with the system's message", sshtest.SFTPRealpath, []any{"a\x00b"}, sshtest.SFTPFailure, "invalid argument"},
		{"a request that does not exist", 99, []any{"whatever"}, sshtest.SFTPOpUnsupported, "Operation unsupported"},
		{"an extension Cave does not have", sshtest.SFTPExtended, []any{"nothing@example.com"}, sshtest.SFTPOpUnsupported, "Operation unsupported"},
		{"an answer sent as a request", sshtest.SFTPStatus, []any{uint32(0), "", ""}, sshtest.SFTPOpUnsupported, "Operation unsupported"},
		// Requests whose fields are not all there.
		{"open without flags", sshtest.SFTPOpen, []any{"/missing"}, sshtest.SFTPBadMessage, "Bad message"},
		{"open with a name cut short", sshtest.SFTPOpen, []any{sshtest.SFTPRaw{0, 0, 0, 9, 'a'}}, sshtest.SFTPBadMessage, "Bad message"},
		{"open with attributes cut short", sshtest.SFTPOpen, []any{"/missing", sshtest.SFTPOpenRead, sshtest.SFTPRaw{0, 0, 0, 1, 0, 0}}, sshtest.SFTPBadMessage, "Bad message"},
		{"open with extended attributes that are not there", sshtest.SFTPOpen, []any{"/missing", sshtest.SFTPOpenRead, sshtest.SFTPRaw{0x80, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}}, sshtest.SFTPBadMessage, "Bad message"},
		{"close without a handle", sshtest.SFTPClose, nil, sshtest.SFTPBadMessage, "Bad message"},
		{"read without a length", sshtest.SFTPRead, []any{"handle", uint64(0)}, sshtest.SFTPBadMessage, "Bad message"},
		{"write without data", sshtest.SFTPWrite, []any{"handle", uint64(0)}, sshtest.SFTPBadMessage, "Bad message"},
		{"write with data cut short", sshtest.SFTPWrite, []any{"handle", uint64(0), sshtest.SFTPRaw{0, 0, 1, 0, 'x'}}, sshtest.SFTPBadMessage, "Bad message"},
		{"stat without a path", sshtest.SFTPStat, nil, sshtest.SFTPBadMessage, "Bad message"},
		{"lstat without a path", sshtest.SFTPLstat, nil, sshtest.SFTPBadMessage, "Bad message"},
		{"fstat without a handle", sshtest.SFTPFstat, nil, sshtest.SFTPBadMessage, "Bad message"},
		{"setstat without attributes", sshtest.SFTPSetstat, []any{"/forbidden/file"}, sshtest.SFTPBadMessage, "Bad message"},
		{"fsetstat without attributes", sshtest.SFTPFsetstat, []any{"handle"}, sshtest.SFTPBadMessage, "Bad message"},
		{"remove without a path", sshtest.SFTPRemove, nil, sshtest.SFTPBadMessage, "Bad message"},
		{"realpath without a path", sshtest.SFTPRealpath, nil, sshtest.SFTPBadMessage, "Bad message"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			code, message := s.Call(tt.kind, tt.fields...).Status()
			if code != tt.wantCode || message != tt.wantMessage {
				t.Errorf("status %d %q, want %d %q", code, message, tt.wantCode, tt.wantMessage)
			}
		})
	}
	// The session is still good after all of them.
	if got := s.Call(sshtest.SFTPStat, "/forbidden/file").Attrs(); got.Size != 1 {
		t.Errorf("stat = %+v", got)
	}
	if fs.opened() != 0 {
		t.Errorf("%d files are open", fs.opened())
	}
}

func TestStatusText(t *testing.T) {
	tests := []struct {
		status Status
		ok     bool
		text   string
	}{
		{Status{}, true, "Success"},
		{Status{Code: StatusEOF}, false, "End of file"},
		{Status{Code: StatusFailure}, false, "Failure"},
		{Status{Code: StatusFailure, Message: "no space left on device"}, false, "no space left on device"},
		{Status{Code: 77}, false, "Failure"},
	}
	for _, tt := range tests {
		if tt.status.OK() != tt.ok || tt.status.Text() != tt.text {
			t.Errorf("%+v: OK %v, text %q", tt.status, tt.status.OK(), tt.status.Text())
		}
	}
}

func TestTheHandleLimit(t *testing.T) {
	fs := newMemFS()
	fs.put("/file", "x")
	s := start(t, fs)
	var handles []string
	seen := map[string]bool{}
	for i := 0; i < MaxHandles; i++ {
		handle := s.Open("/file", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
		if seen[handle] {
			t.Fatalf("the handle %x was given twice", handle)
		}
		seen[handle] = true
		handles = append(handles, handle)
	}
	code, message := s.Call(sshtest.SFTPOpen, "/file", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{}).Status()
	if code != sshtest.SFTPFailure || message != "too many open files in this session" {
		t.Errorf("one more than the limit: %d %q", code, message)
	}
	if fs.opened() != MaxHandles {
		t.Errorf("%d files are open, want %d", fs.opened(), MaxHandles)
	}
	// There is room again after a close, and the new handle is not the old one.
	s.Close(handles[0])
	if handle := s.Open("/file", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{}); seen[handle] {
		t.Errorf("the handle %x was given twice", handle)
	}
	// The end of the session closes what it left open.
	if err := s.end(); err != nil {
		t.Errorf("Serve returned %v", err)
	}
	if fs.opened() != 0 {
		t.Errorf("%d files are open after the session", fs.opened())
	}
}

func TestAReadIsCutToTheLimit(t *testing.T) {
	fs := newMemFS()
	fs.put("/big", strings.Repeat("abcdefgh", 1<<17)) // 1 MiB
	s := start(t, fs)
	handle := s.Open("/big", sshtest.SFTPOpenRead, sshtest.SFTPAttrs{})
	for _, asked := range []uint32{MaxRead, MaxRead + 1, 1 << 20, 0xffffffff} {
		data, _ := s.Read(handle, 8, asked)
		if len(data) != MaxRead || !bytes.Equal(data[:16], []byte("abcdefghabcdefgh")) {
			t.Errorf("a read of %d bytes returned %d", asked, len(data))
		}
	}
	if fs.maxRead != MaxRead {
		t.Errorf("the largest buffer of a read had %d bytes, want %d", fs.maxRead, MaxRead)
	}
}

func TestThePacketLimit(t *testing.T) {
	fs := newMemFS()
	s := start(t, fs)
	handle := s.Open("/file", sshtest.SFTPOpenWrite|sshtest.SFTPOpenCreate, sshtest.SFTPAttrs{})
	// The longest packet there may be: a write whose data fills it.
	overhead := 1 + 4 + 4 + len(handle) + 8 + 4
	data := bytes.Repeat([]byte("z"), MaxPacket-overhead)
	if code := s.Write(handle, 0, data); code != sshtest.SFTPOK {
		t.Fatalf("a packet of exactly the limit: status %d", code)
	}
	if got, _ := fs.content("/file"); got != string(data) {
		t.Errorf("the file has %d bytes, want %d", len(got), len(data))
	}
	// One byte more ends the session, before anything of the packet is read.
	s.SendRaw(binary.BigEndian.AppendUint32(nil, MaxPacket+1))
	meta := errtest.Route(t, s.wait(), "Server.Serve->conn.readPacket.SftpPacketTooLarge")
	if meta["length"] != uint32(MaxPacket+1) || meta["limit"] != MaxPacket {
		t.Errorf("metadata = %v", meta)
	}
	if _, ok := s.TryRecv(); ok {
		t.Error("the server answered a packet over the limit")
	}
	if fs.opened() != 0 {
		t.Errorf("%d files are open after the session", fs.opened())
	}
}

// failing is a stream to the server that fails after what it has was read, or cannot be
// written.
type failing struct {
	in       io.Reader
	readErr  error
	writeErr error
}

func (f *failing) Read(p []byte) (int, error) {
	n, err := f.in.Read(p)
	if err == io.EOF && f.readErr != nil {
		return n, f.readErr
	}
	return n, err
}

func (f *failing) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func TestSessionsThatCannotGoOn(t *testing.T) {
	init := []byte{0, 0, 0, 5, sshtest.SFTPInit, 0, 0, 0, 3}
	tests := []struct {
		name      string
		input     []byte
		readErr   error
		writeErr  error
		wantRoute string
		wantCause error
	}{
		{name: "a packet without a type", input: append(append([]byte{}, init...), 0, 0, 0, 0), wantRoute: "Server.Serve->conn.readPacket.SftpPacketEmpty"},
		{name: "a packet over the limit", input: []byte{0xff, 0xff, 0xff, 0xff}, wantRoute: "Server.Serve->conn.readPacket.SftpPacketTooLarge"},
		{name: "a packet that is cut", input: []byte{0, 0, 0, 100, sshtest.SFTPInit, 0, 0}, wantRoute: "Server.Serve->conn.readPacket.SftpPacketTruncated", wantCause: io.ErrUnexpectedEOF},
		{name: "a packet with nothing after its length", input: []byte{0, 0, 0, 100}, wantRoute: "Server.Serve->conn.readPacket.SftpPacketTruncated", wantCause: io.EOF},
		{name: "a length that is cut", input: []byte{0, 0}, wantRoute: "Server.Serve->conn.readPacket.SftpReadFailed", wantCause: io.ErrUnexpectedEOF},
		{name: "a stream that fails", input: init, readErr: io.ErrNoProgress, wantRoute: "Server.Serve->conn.readPacket.SftpReadFailed", wantCause: io.ErrNoProgress},
		{name: "an answer that cannot be written", input: init, writeErr: io.ErrClosedPipe, wantRoute: "Server.Serve.SftpWriteFailed", wantCause: io.ErrClosedPipe},
		{name: "a request before INIT", input: []byte{0, 0, 0, 5, sshtest.SFTPStat, 0, 0, 0, 1}, wantRoute: "Server.Serve->conn.dispatch.SftpInitMissing"},
		{name: "an INIT without a version", input: []byte{0, 0, 0, 3, sshtest.SFTPInit, 0, 0}, wantRoute: "Server.Serve->conn.dispatch.SftpInitInvalid"},
		{name: "a second INIT", input: append(append([]byte{}, init...), init...), wantRoute: "Server.Serve->conn.dispatch.SftpInitRepeated"},
		{name: "a request without an id", input: append(append([]byte{}, init...), 0, 0, 0, 3, sshtest.SFTPStat, 0, 0), wantRoute: "Server.Serve->conn.dispatch.SftpRequestInvalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			server := New(Options{FS: newMemFS(), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
			err := server.Serve(context.Background(), &failing{in: bytes.NewReader(tt.input), readErr: tt.readErr, writeErr: tt.writeErr})
			errtest.Route(t, err, tt.wantRoute)
			if tt.wantCause != nil && !errors.Is(err, tt.wantCause) {
				t.Errorf("the cause is %v, want %v", err, tt.wantCause)
			}
		})
	}
}

func TestACancelledContextEndsTheSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	server := New(Options{FS: newMemFS(), Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))})
	// Nothing is read: the session was over before it began.
	if err := server.Serve(ctx, &failing{in: bytes.NewReader([]byte{0xff, 0xff, 0xff, 0xff})}); err != nil {
		t.Errorf("Serve returned %v", err)
	}
}

func TestTheLogHasEveryRequestWithItsPathMadeSafe(t *testing.T) {
	s := start(t, newMemFS())
	s.Call(sshtest.SFTPStat, "/missing\x1b[31m\nfile")
	s.Call(99, "x")
	if err := s.end(); err != nil {
		t.Fatal(err)
	}
	var lines []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(s.log.String()), "\n") {
		var entry map[string]any
		if err := json.Unmarshal([]byte(line), &entry); err != nil {
			t.Fatalf("the log line %q: %v", line, err)
		}
		lines = append(lines, entry)
	}
	if len(lines) != 2 {
		t.Fatalf("the log has %d lines: %s", len(lines), s.log.String())
	}
	if got := lines[0]; got["msg"] != "sftp request" || got["request"] != "stat" || got["status"] != float64(StatusNoSuchFile) {
		t.Errorf("the first line is %v", got)
	}
	if logged, _ := lines[0]["path"].(string); strings.ContainsAny(logged, "\x1b\n") || !strings.Contains(logged, "missing") {
		t.Errorf("the path in the log is %q", logged)
	}
	if got := lines[1]; got["request"] != "unknown" || got["type"] != float64(99) || got["status"] != float64(StatusOpUnsupported) {
		t.Errorf("the second line is %v", got)
	}
}

func TestRandomPacketsNeverBreakTheServer(t *testing.T) {
	// Seeded, so that a failure can be repeated. The file system is in memory: random requests
	// never reach real files.
	rng := rand.New(rand.NewSource(20261124))
	fs := newMemFS()
	fs.put("/a", "some content")
	paths := []string{"/a", "/b", "/forbidden/x", "", "a\x00b", strings.Repeat("p", 300)}
	sessions, answers := 0, 0
	for round := 0; round < 40; round++ {
		s := start(t, fs)
		sessions++
		var handles []string
		sent := 0
		for i := 0; i < 300; i++ {
			kind := byte(rng.Intn(256))
			if rng.Intn(4) != 0 {
				kind = byte(2 + rng.Intn(20)) // mostly the requests there are; never INIT
			}
			if kind == sshtest.SFTPInit {
				continue
			}
			var body []byte
			switch rng.Intn(5) {
			case 0: // random bytes; now and then too few for an id, which ends the session
				body = make([]byte, 4+rng.Intn(64))
				if rng.Intn(40) == 0 {
					body = body[:rng.Intn(4)]
				}
				rng.Read(body)
			case 1: // an id and random bytes
				body = make([]byte, 4+rng.Intn(64))
				rng.Read(body)
			case 2: // a handle, and numbers that are as large as they can be
				handle := "nonsense"
				if len(handles) > 0 {
					handle = handles[rng.Intn(len(handles))]
				}
				body = sshtest.SFTPPack(uint32(i), handle, uint64(rng.Uint64()), uint32(0xffffffff), uint32(0xffffffff))
			case 3: // a path with flags and attributes
				body = sshtest.SFTPPack(uint32(i), paths[rng.Intn(len(paths))], uint32(rng.Intn(64)), sshtest.SFTPAttrs{Flags: uint32(rng.Intn(16)), Size: rng.Uint64(), Mode: rng.Uint32()})
			default: // lengths that promise more than there is
				body = sshtest.SFTPPack(uint32(i), sshtest.SFTPRaw{0xff, 0xff, 0xff, 0xf0}, "x")
			}
			s.SendPacket(kind, body)
			sent++
			if len(body) < 4 {
				break // a request without an id ends the session
			}
			reply := s.Recv()
			answers++
			if len(reply.Body)+5 > MaxPacket {
				t.Fatalf("an answer of %d bytes", len(reply.Body)+5)
			}
			if reply.Type == sshtest.SFTPHandle {
				handles = append(handles, reply.Handle())
			}
		}
		s.toServer.Close()
		if err := s.wait(); err != nil {
			errtest.Route(t, err, "conn.dispatch.SftpRequestInvalid")
		}
		if fs.opened() != 0 {
			t.Fatalf("%d files are open after a session", fs.opened())
		}
		if sent == 0 {
			t.Fatal("a round sent nothing")
		}
	}
	if fs.maxRead > MaxRead {
		t.Errorf("a read was given a buffer of %d bytes", fs.maxRead)
	}
	t.Logf("%d sessions, %d answers", sessions, answers)
}

func TestTheSessionsBuffersStayWithinTheLimit(t *testing.T) {
	fs := newMemFS()
	fs.put("/a", strings.Repeat("x", 1000))
	c := &conn{s: New(Options{FS: fs, Logger: slog.New(slog.NewJSONHandler(io.Discard, nil))}), handles: map[string]File{}}
	var input bytes.Buffer
	packet := func(kind byte, body []byte) {
		input.Write(binary.BigEndian.AppendUint32(nil, uint32(len(body)+1)))
		input.WriteByte(kind)
		input.Write(body)
	}
	packet(typeInit, sshtest.SFTPPack(uint32(3)))
	packet(typeOpen, sshtest.SFTPPack(uint32(1), "/a", uint32(OpenRead), sshtest.SFTPAttrs{}))
	packet(typeRead, sshtest.SFTPPack(uint32(2), string(binary.BigEndian.AppendUint64(nil, 1)), uint64(0), uint32(0xffffffff)))
	packet(typeWrite, append(sshtest.SFTPPack(uint32(3), "nonsense", uint64(0)), sshtest.SFTPPack(bytes.Repeat([]byte("w"), MaxPacket-64))...))
	c.rw = &failing{in: &input}
	for {
		kind, body, err := c.readPacket()
		if err != nil {
			t.Fatal(err)
		}
		if body == nil {
			break
		}
		if _, err := c.dispatch(context.Background(), kind, body); err != nil {
			t.Fatal(err)
		}
	}
	if cap(c.in) > MaxPacket || cap(c.out) > MaxPacket {
		t.Errorf("the buffers hold %d and %d bytes, the limit is %d", cap(c.in), cap(c.out), MaxPacket)
	}
	if cap(c.in) < MaxPacket-64 {
		t.Errorf("the packet buffer holds %d bytes: the large write was not read", cap(c.in))
	}
}

func TestAttributesRoundTrip(t *testing.T) {
	tests := []Attrs{
		{},
		{Flags: AttrSize, Size: 1 << 40},
		{Flags: AttrUIDGID, UID: 1000, GID: 100},
		{Flags: AttrPermissions, Mode: 0o100644},
		{Flags: AttrTimes, Atime: 1, Mtime: 0xffffffff},
		{Flags: AttrSize | AttrUIDGID | AttrPermissions | AttrTimes, Size: 7, UID: 1, GID: 2, Mode: 0o40755, Atime: 3, Mtime: 4},
	}
	for _, want := range tests {
		b := &builder{}
		b.attrs(want)
		r := &reader{b: b.b}
		if got := r.attrs(); got != want || r.bad || len(r.b) != 0 {
			t.Errorf("%+v came back as %+v (bad %v, %d bytes left)", want, got, r.bad, len(r.b))
		}
	}
	// The flag of extended attributes is never sent: Cave has none.
	b := &builder{}
	b.attrs(Attrs{Flags: attrExtended | AttrSize, Size: 9})
	if got := binary.BigEndian.Uint32(b.b); got != AttrSize {
		t.Errorf("the flags sent are %#x", got)
	}
}

func TestTheReader(t *testing.T) {
	r := &reader{b: sshtest.SFTPPack(uint32(7), uint64(1<<33), "text", []byte{1, 2})}
	if a, b, c, d := r.uint32(), r.uint64(), r.str(), r.bytes(); a != 7 || b != 1<<33 || c != "text" || !bytes.Equal(d, []byte{1, 2}) || r.bad {
		t.Errorf("read %d %d %q %v, bad %v", a, b, c, d, r.bad)
	}
	// Past the end, everything reads as zero and stays bad.
	if a, b, c, d := r.uint32(), r.uint64(), r.str(), r.bytes(); a != 0 || b != 0 || c != "" || d != nil || !r.bad {
		t.Errorf("past the end: %d %d %q %v, bad %v", a, b, c, d, r.bad)
	}
	// A length that promises more than there is.
	r = &reader{b: []byte{0xff, 0xff, 0xff, 0xff, 'a'}}
	if got := r.str(); got != "" || !r.bad {
		t.Errorf("read %q, bad %v", got, r.bad)
	}
	// An empty string is not bad.
	r = &reader{b: []byte{0, 0, 0, 0}}
	if got := r.bytes(); len(got) != 0 || r.bad {
		t.Errorf("read %v, bad %v", got, r.bad)
	}
}
