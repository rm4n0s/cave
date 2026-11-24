// Package sftp is Cave's SFTP server: version 3 of the protocol (draft-ietf-secsh-filexfer-02)
// as OpenSSH's sftp-server speaks it (docs/SPEC.md §9.7). It runs inside Cave as the OS user,
// over a small file-system interface, so that each platform has its own implementation and the
// protocol is tested without touching real files.
package sftp

import (
	"context"
	"encoding/binary"
	"io"
	"log/slog"

	"github.com/rm4n0s/cave/internal/core/sanitize"
	"github.com/rm4n0s/errors"
)

// The Tags of this package's errors. Each of them ends a session; what a request is answered
// with is a Status, not an error.
const (
	TagPacketEmpty     = "SftpPacketEmpty"
	TagPacketTooLarge  = "SftpPacketTooLarge"
	TagPacketTruncated = "SftpPacketTruncated"
	TagReadFailed      = "SftpReadFailed"
	TagWriteFailed     = "SftpWriteFailed"
	TagInitMissing     = "SftpInitMissing"
	TagInitInvalid     = "SftpInitInvalid"
	TagInitRepeated    = "SftpInitRepeated"
	TagRequestInvalid  = "SftpRequestInvalid"
)

// The limits of a session (docs/SPEC.md §9.7).
const (
	// Version is the version of the protocol.
	Version = 3
	// MaxPacket is the longest packet a client may send, and the longest Cave sends. A longer
	// one ends the session.
	MaxPacket = 256 << 10
	// MaxRead is the most bytes one read request is answered with.
	MaxRead = 255 << 10
	// MaxHandles is how many files a session may have open at once.
	MaxHandles = 256
	// maxLogPath bounds a path in the log.
	maxLogPath = 256
)

// The types of the packets.
const (
	typeInit     = 1
	typeVersion  = 2
	typeOpen     = 3
	typeClose    = 4
	typeRead     = 5
	typeWrite    = 6
	typeLstat    = 7
	typeFstat    = 8
	typeSetstat  = 9
	typeFsetstat = 10
	typeOpendir  = 11
	typeReaddir  = 12
	typeRemove   = 13
	typeMkdir    = 14
	typeRmdir    = 15
	typeRealpath = 16
	typeStat     = 17
	typeRename   = 18
	typeReadlink = 19
	typeSymlink  = 20
	typeStatus   = 101
	typeHandle   = 102
	typeData     = 103
	typeName     = 104
	typeAttrs    = 105
	typeExtended = 200
	typeExtReply = 201
)

// The extensions of OpenSSH that Cave has.
const (
	extPosixRename = "posix-rename@openssh.com"
	extStatVFS     = "statvfs@openssh.com"
	extFstatVFS    = "fstatvfs@openssh.com"
	extHardlink    = "hardlink@openssh.com"
	extFsync       = "fsync@openssh.com"
	extLimits      = "limits@openssh.com"
)

const (
	// maxEntries is how many names one answer to READDIR has at most, as with OpenSSH.
	maxEntries = 100
	// maxNamesPacket is how long an answer to READDIR may get before it is sent.
	maxNamesPacket = MaxPacket / 2
	// maxWrite is the longest write a client is told it may send: a packet, less the room for
	// what comes before the data.
	maxWrite = MaxPacket - 1024
)

// typeNames are the names of the requests, for the log.
var typeNames = map[byte]string{
	typeOpen: "open", typeClose: "close", typeRead: "read", typeWrite: "write", typeLstat: "lstat", typeFstat: "fstat",
	typeSetstat: "setstat", typeFsetstat: "fsetstat", typeRemove: "remove", typeRealpath: "realpath", typeStat: "stat",
	typeOpendir: "opendir", typeReaddir: "readdir", typeMkdir: "mkdir", typeRmdir: "rmdir", typeRename: "rename",
	typeReadlink: "readlink", typeSymlink: "symlink", typeExtended: "extended",
}

// Options are what a Server needs. Every field is required.
type Options struct {
	// FS is the file system the clients see.
	FS FileSystem
	// Logger gets a line for every request at the debug level.
	Logger *slog.Logger
}

// Server answers the requests of SFTP sessions. One Server serves any number of sessions; each
// has its own handles.
type Server struct {
	opts Options
}

// New makes a Server.
func New(opts Options) *Server {
	return &Server{opts: opts}
}

// conn is one session.
type conn struct {
	s  *Server
	rw io.ReadWriter
	// in is the buffer of the packet being read, and out the buffer of the answer to a read;
	// both are kept between requests and never grow past MaxPacket.
	in, out []byte
	// initDone says whether the client has sent its INIT.
	initDone bool
	// handles are the open files and folders, by the handle the client has for them.
	handles map[string]*handle
	// lastHandle is the number of the last handle given; a handle is never given twice.
	lastHandle uint64
}

// handle is an open file or an open folder.
type handle struct {
	// file is the open file; nil for a folder.
	file File
	// dir is the open folder; nil for a file.
	dir Dir
	// pending are entries of the folder that were read and not sent yet.
	pending []Entry
	// listed says that "." and ".." and every entry were handed out.
	listed bool
}

// close closes what the handle has open.
func (h *handle) close() Status {
	if h.dir != nil {
		return h.dir.Close()
	}
	return h.file.Close()
}

// Serve runs one session over rw until the client ends it, and closes every file the session
// left open. It returns nil when the session ended at the end of a packet, and an error when
// the client broke the protocol in a way that cannot be answered, or rw failed.
func (s *Server) Serve(ctx context.Context, rw io.ReadWriter) error {
	c := &conn{s: s, rw: rw, handles: map[string]*handle{}}
	defer c.closeAll()
	for ctx.Err() == nil {
		kind, body, err := c.readPacket()
		if err != nil {
			return err
		}
		if body == nil {
			return nil // the client ended the session
		}
		reply, err := c.dispatch(ctx, kind, body)
		if err != nil {
			return err
		}
		if _, err := rw.Write(reply); err != nil {
			return errors.NewErr(TagWriteFailed, err)
		}
	}
	return nil
}

// closeAll closes the files a session left open.
func (c *conn) closeAll() {
	for id, h := range c.handles {
		_ = h.close() // the session is over: there is nobody to tell
		delete(c.handles, id)
	}
}

// readPacket reads one packet and returns its type and what follows it. The body is nil, with
// no error, when the stream ended before a packet began. It stays valid until the next call.
func (c *conn) readPacket() (kind byte, body []byte, err error) {
	var header [4]byte
	if _, err := io.ReadFull(c.rw, header[:]); err != nil {
		if err == io.EOF {
			return 0, nil, nil
		}
		return 0, nil, errors.NewErr(TagReadFailed, err)
	}
	length := binary.BigEndian.Uint32(header[:])
	if length == 0 {
		return 0, nil, errors.New(TagPacketEmpty, "a packet without a type")
	}
	if length > MaxPacket {
		return 0, nil, errors.New(TagPacketTooLarge, "a packet longer than the limit", "length", length, "limit", MaxPacket)
	}
	if uint32(cap(c.in)) < length {
		c.in = make([]byte, length)
	}
	packet := c.in[:length]
	if _, err := io.ReadFull(c.rw, packet); err != nil {
		return 0, nil, errors.NewErr(TagPacketTruncated, err).SetMetadata("length", length)
	}
	return packet[0], packet[1:], nil
}

// dispatch answers one packet. An error ends the session.
func (c *conn) dispatch(ctx context.Context, kind byte, body []byte) ([]byte, error) {
	r := &reader{b: body}
	if kind == typeInit {
		if c.initDone {
			return nil, errors.New(TagInitRepeated, "a second INIT")
		}
		r.uint32() // the client's version: Cave speaks version 3 whatever it is
		if r.bad {
			return nil, errors.New(TagInitInvalid, "an INIT without a version")
		}
		c.initDone = true
		return c.version(), nil
	}
	if !c.initDone {
		return nil, errors.New(TagInitMissing, "a request before INIT", "type", int(kind))
	}
	id := r.uint32()
	if r.bad {
		return nil, errors.New(TagRequestInvalid, "a request without an id", "type", int(kind))
	}
	reply, path, status := c.request(kind, id, r)
	if c.s.opts.Logger.Enabled(ctx, slog.LevelDebug) {
		name, known := typeNames[kind]
		if !known {
			name = "unknown"
		}
		c.s.opts.Logger.Debug("sftp request", "request", name, "type", int(kind), "path", sanitize.Display(path, maxLogPath), "status", int(status.Code))
	}
	return reply, nil
}

// version is the answer to INIT: the version, and the extensions Cave has.
func (c *conn) version() []byte {
	b := newPacket(typeVersion)
	b.uint32(Version)
	for _, extension := range extensions {
		b.str(extension.name)
		b.str(extension.version)
	}
	return b.packet()
}

// extension is one extension of the protocol that Cave has, as VERSION announces it.
type extension struct {
	name, version string
}

// extensions are the extensions Cave has, with the versions OpenSSH gives them.
var extensions = []extension{
	{extPosixRename, "1"}, {extStatVFS, "2"}, {extFstatVFS, "2"}, {extHardlink, "1"}, {extFsync, "1"}, {extLimits, "1"},
}

// request answers one request. It also returns the path the request named, if any, and the
// status it ended with, for the log.
func (c *conn) request(kind byte, id uint32, r *reader) (reply []byte, path string, status Status) {
	switch kind {
	case typeOpen:
		return c.open(id, r)
	case typeClose:
		status = c.close(r)
	case typeRead:
		return c.read(id, r)
	case typeWrite:
		status = c.write(r)
	case typeLstat, typeStat:
		return c.stat(kind, id, r)
	case typeFstat:
		return c.fstat(id, r)
	case typeSetstat:
		path, status = c.setstat(r)
	case typeFsetstat:
		status = c.fsetstat(r)
	case typeRemove:
		path = r.str()
		if status = badMessage(r); status.OK() {
			status = c.s.opts.FS.Remove(path)
		}
	case typeRealpath:
		return c.realpath(id, r)
	case typeOpendir:
		return c.opendir(id, r)
	case typeReaddir:
		return c.readdir(id, r)
	case typeMkdir:
		var attrs Attrs
		path, attrs = r.str(), r.attrs()
		if status = badMessage(r); status.OK() {
			status = c.s.opts.FS.Mkdir(path, attrs)
		}
	case typeRmdir:
		path = r.str()
		if status = badMessage(r); status.OK() {
			status = c.s.opts.FS.Rmdir(path)
		}
	case typeRename:
		var newPath string
		path, newPath = r.str(), r.str()
		if status = badMessage(r); status.OK() {
			status = c.s.opts.FS.Rename(path, newPath, false)
		}
	case typeReadlink:
		return c.readlink(id, r)
	case typeSymlink:
		// OpenSSH's order, which every client follows: what the link points to, then the link.
		var target string
		target, path = r.str(), r.str()
		if status = badMessage(r); status.OK() {
			status = c.s.opts.FS.Symlink(target, path)
		}
	case typeExtended:
		return c.extended(id, r)
	default:
		status = Status{Code: StatusOpUnsupported}
	}
	return statusPacket(id, status), path, status
}

// badMessage is the status of a request whose fields could not be read, and OK otherwise.
func badMessage(r *reader) Status {
	if r.bad {
		return Status{Code: StatusBadMessage}
	}
	return Status{}
}

// statusPacket is the answer that carries only a status.
func statusPacket(id uint32, status Status) []byte {
	b := newReply(typeStatus, id)
	b.uint32(status.Code)
	b.str(status.Text())
	b.str("") // the language tag
	return b.packet()
}

// file returns the open file of a handle; the handle of a folder is not one.
func (c *conn) file(id string) (File, Status) {
	h, ok := c.handles[id]
	if !ok || h.file == nil {
		return nil, Status{Code: StatusFailure, Message: "invalid handle"}
	}
	return h.file, Status{}
}

// addHandle keeps an open file or folder and returns the answer with its handle.
func (c *conn) addHandle(id uint32, h *handle) []byte {
	c.lastHandle++
	name := string(binary.BigEndian.AppendUint64(nil, c.lastHandle))
	c.handles[name] = h
	b := newReply(typeHandle, id)
	b.str(name)
	return b.packet()
}

// room is the status of a request that opens something: a failure when the session has as many
// handles as it may.
func (c *conn) room(r *reader) Status {
	status := badMessage(r)
	if status.OK() && len(c.handles) >= MaxHandles {
		status = Status{Code: StatusFailure, Message: "too many open files in this session"}
	}
	return status
}

func (c *conn) open(id uint32, r *reader) ([]byte, string, Status) {
	path, flags, attrs := r.str(), r.uint32(), r.attrs()
	status := c.room(r)
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	file, status := c.s.opts.FS.Open(path, flags, attrs)
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	return c.addHandle(id, &handle{file: file}), path, Status{}
}

func (c *conn) opendir(id uint32, r *reader) ([]byte, string, Status) {
	path := r.str()
	status := c.room(r)
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	dir, status := c.s.opts.FS.OpenDir(path)
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	return c.addHandle(id, &handle{dir: dir}), path, Status{}
}

// readdir answers with the next names of a folder: at most maxEntries, and no more than fit a
// packet. The names that were read and did not fit wait for the next request.
func (c *conn) readdir(id uint32, r *reader) ([]byte, string, Status) {
	name := r.str()
	status := badMessage(r)
	var h *handle
	if status.OK() {
		var ok bool
		if h, ok = c.handles[name]; !ok || h.dir == nil {
			status = Status{Code: StatusFailure, Message: "invalid handle"}
		}
	}
	if status.OK() && len(h.pending) == 0 && !h.listed {
		h.pending, status = h.dir.Read(maxEntries)
		h.listed = status.OK() && len(h.pending) == 0
	}
	if status.OK() && len(h.pending) == 0 {
		status = Status{Code: StatusEOF}
	}
	if !status.OK() {
		return statusPacket(id, status), "", status
	}
	b := newReply(typeName, id)
	b.uint32(0) // the count, filled in below
	count := 0
	for len(h.pending) > 0 {
		entry := h.pending[0]
		size := 4 + len(entry.Name) + 4 + len(entry.LongName) + 32
		if len(b.b)+size > maxNamesPacket {
			if count > 0 {
				break
			}
			// A name that no packet has room for: it cannot be told, and is left out.
			h.pending = h.pending[1:]
			continue
		}
		b.str(entry.Name)
		b.str(entry.LongName)
		b.attrs(entry.Attrs)
		h.pending = h.pending[1:]
		count++
	}
	binary.BigEndian.PutUint32(b.b[9:], uint32(count))
	return b.packet(), "", Status{}
}

func (c *conn) readlink(id uint32, r *reader) ([]byte, string, Status) {
	path := r.str()
	status := badMessage(r)
	var target string
	if status.OK() {
		target, status = c.s.opts.FS.ReadLink(path)
	}
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	b := newReply(typeName, id)
	b.uint32(1)
	b.str(target)
	b.str(target)
	b.attrs(Attrs{})
	return b.packet(), path, Status{}
}

// extended answers a request of an extension.
func (c *conn) extended(id uint32, r *reader) (reply []byte, path string, status Status) {
	name := r.str()
	if status = badMessage(r); !status.OK() {
		return statusPacket(id, status), "", status
	}
	switch name {
	case extPosixRename:
		var newPath string
		path, newPath = r.str(), r.str()
		if status = badMessage(r); status.OK() {
			status = c.s.opts.FS.Rename(path, newPath, true)
		}
	case extHardlink:
		var newPath string
		path, newPath = r.str(), r.str()
		if status = badMessage(r); status.OK() {
			status = c.s.opts.FS.Link(path, newPath)
		}
	case extFsync:
		handle := r.str()
		if status = badMessage(r); status.OK() {
			var file File
			if file, status = c.file(handle); status.OK() {
				status = file.Sync()
			}
		}
	case extStatVFS:
		path = r.str()
		var vfs VFS
		if status = badMessage(r); status.OK() {
			vfs, status = c.s.opts.FS.StatVFS(path)
		}
		if status.OK() {
			return vfsPacket(id, vfs), path, status
		}
	case extFstatVFS:
		handle := r.str()
		var vfs VFS
		if status = badMessage(r); status.OK() {
			var file File
			if file, status = c.file(handle); status.OK() {
				vfs, status = file.StatVFS()
			}
		}
		if status.OK() {
			return vfsPacket(id, vfs), "", status
		}
	case extLimits:
		b := newReply(typeExtReply, id)
		for _, limit := range []uint64{MaxPacket, MaxRead, maxWrite, MaxHandles} {
			b.uint64(limit)
		}
		return b.packet(), "", Status{}
	default:
		status = Status{Code: StatusOpUnsupported}
	}
	return statusPacket(id, status), path, status
}

// vfsPacket is the answer of statvfs and fstatvfs.
func vfsPacket(id uint32, vfs VFS) []byte {
	b := newReply(typeExtReply, id)
	for _, value := range []uint64{
		vfs.BlockSize, vfs.FragmentSize, vfs.Blocks, vfs.BlocksFree, vfs.BlocksAvail,
		vfs.Files, vfs.FilesFree, vfs.FilesAvail, vfs.FSID, vfs.Flags, vfs.NameMax,
	} {
		b.uint64(value)
	}
	return b.packet()
}

func (c *conn) close(r *reader) Status {
	name := r.str()
	if status := badMessage(r); !status.OK() {
		return status
	}
	h, ok := c.handles[name]
	if !ok {
		return Status{Code: StatusFailure, Message: "invalid handle"}
	}
	delete(c.handles, name)
	return h.close()
}

func (c *conn) read(id uint32, r *reader) ([]byte, string, Status) {
	handle, offset, length := r.str(), r.uint64(), r.uint32()
	status := badMessage(r)
	var file File
	if status.OK() {
		file, status = c.file(handle)
	}
	if !status.OK() {
		return statusPacket(id, status), "", status
	}
	if length > MaxRead {
		length = MaxRead
	}
	// The answer is built in the session's buffer: its header, then the bytes read in place.
	const header = 4 + 1 + 4 + 4
	if c.out == nil {
		c.out = make([]byte, header+MaxRead)
	}
	n, status := file.ReadAt(c.out[header:header+int(length)], offset)
	if n == 0 {
		// Nothing was read: the end of the file, or the reason.
		if status.OK() {
			status = Status{Code: StatusEOF}
		}
		return statusPacket(id, status), "", status
	}
	binary.BigEndian.PutUint32(c.out[0:], uint32(header-4+n))
	c.out[4] = typeData
	binary.BigEndian.PutUint32(c.out[5:], id)
	binary.BigEndian.PutUint32(c.out[9:], uint32(n))
	return c.out[:header+n], "", Status{}
}

func (c *conn) write(r *reader) Status {
	handle, offset, data := r.str(), r.uint64(), r.bytes()
	if status := badMessage(r); !status.OK() {
		return status
	}
	file, status := c.file(handle)
	if !status.OK() {
		return status
	}
	return file.WriteAt(data, offset)
}

func (c *conn) stat(kind byte, id uint32, r *reader) ([]byte, string, Status) {
	path := r.str()
	status := badMessage(r)
	var attrs Attrs
	if status.OK() {
		if kind == typeLstat {
			attrs, status = c.s.opts.FS.Lstat(path)
		} else {
			attrs, status = c.s.opts.FS.Stat(path)
		}
	}
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	b := newReply(typeAttrs, id)
	b.attrs(attrs)
	return b.packet(), path, Status{}
}

func (c *conn) fstat(id uint32, r *reader) ([]byte, string, Status) {
	handle := r.str()
	status := badMessage(r)
	var file File
	if status.OK() {
		file, status = c.file(handle)
	}
	var attrs Attrs
	if status.OK() {
		attrs, status = file.Stat()
	}
	if !status.OK() {
		return statusPacket(id, status), "", status
	}
	b := newReply(typeAttrs, id)
	b.attrs(attrs)
	return b.packet(), "", Status{}
}

func (c *conn) setstat(r *reader) (string, Status) {
	path, attrs := r.str(), r.attrs()
	if status := badMessage(r); !status.OK() {
		return path, status
	}
	return path, c.s.opts.FS.SetStat(path, attrs)
}

func (c *conn) fsetstat(r *reader) Status {
	handle, attrs := r.str(), r.attrs()
	if status := badMessage(r); !status.OK() {
		return status
	}
	file, status := c.file(handle)
	if !status.OK() {
		return status
	}
	return file.SetStat(attrs)
}

func (c *conn) realpath(id uint32, r *reader) ([]byte, string, Status) {
	path := r.str()
	status := badMessage(r)
	var resolved string
	if status.OK() {
		resolved, status = c.s.opts.FS.RealPath(path)
	}
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	// One name, without attributes: the long name is the name, as OpenSSH answers.
	b := newReply(typeName, id)
	b.uint32(1)
	b.str(resolved)
	b.str(resolved)
	b.attrs(Attrs{})
	return b.packet(), path, Status{}
}
