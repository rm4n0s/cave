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
	TagUnsupported     = "SftpUnsupported"
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
	typeRemove   = 13
	typeRealpath = 16
	typeStat     = 17
	typeStatus   = 101
	typeHandle   = 102
	typeData     = 103
	typeName     = 104
	typeAttrs    = 105
)

// typeNames are the names of the requests, for the log.
var typeNames = map[byte]string{
	typeOpen: "open", typeClose: "close", typeRead: "read", typeWrite: "write", typeLstat: "lstat", typeFstat: "fstat",
	typeSetstat: "setstat", typeFsetstat: "fsetstat", typeRemove: "remove", typeRealpath: "realpath", typeStat: "stat",
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
	// handles are the open files, by the handle the client has for them.
	handles map[string]File
	// lastHandle is the number of the last handle given; a handle is never given twice.
	lastHandle uint64
}

// Serve runs one session over rw until the client ends it, and closes every file the session
// left open. It returns nil when the session ended at the end of a packet, and an error when
// the client broke the protocol in a way that cannot be answered, or rw failed.
func (s *Server) Serve(ctx context.Context, rw io.ReadWriter) error {
	c := &conn{s: s, rw: rw, handles: map[string]File{}}
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
	for id, file := range c.handles {
		_ = file.Close() // the session is over: there is nobody to tell
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

// extensions are the extensions Cave has. Step 3.2 adds them.
var extensions []extension

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

// file returns the open file of a handle.
func (c *conn) file(handle string) (File, Status) {
	file, ok := c.handles[handle]
	if !ok {
		return nil, Status{Code: StatusFailure, Message: "invalid handle"}
	}
	return file, Status{}
}

func (c *conn) open(id uint32, r *reader) ([]byte, string, Status) {
	path, flags, attrs := r.str(), r.uint32(), r.attrs()
	status := badMessage(r)
	if status.OK() && len(c.handles) >= MaxHandles {
		status = Status{Code: StatusFailure, Message: "too many open files in this session"}
	}
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	file, status := c.s.opts.FS.Open(path, flags, attrs)
	if !status.OK() {
		return statusPacket(id, status), path, status
	}
	c.lastHandle++
	handle := string(binary.BigEndian.AppendUint64(nil, c.lastHandle))
	c.handles[handle] = file
	b := newReply(typeHandle, id)
	b.str(handle)
	return b.packet(), path, Status{}
}

func (c *conn) close(r *reader) Status {
	handle := r.str()
	if status := badMessage(r); !status.OK() {
		return status
	}
	file, status := c.file(handle)
	if !status.OK() {
		return status
	}
	delete(c.handles, handle)
	return file.Close()
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
