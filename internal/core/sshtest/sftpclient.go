package sshtest

import (
	"encoding/binary"
	"io"
	"testing"
)

// The packet types of SFTP version 3 that tests name. The client knows the protocol on its own:
// it shares no code with Cave's server, so a mistake in one is not hidden by the other.
const (
	SFTPInit          = 1
	SFTPVersion       = 2
	SFTPOpen          = 3
	SFTPClose         = 4
	SFTPRead          = 5
	SFTPWrite         = 6
	SFTPLstat         = 7
	SFTPFstat         = 8
	SFTPSetstat       = 9
	SFTPFsetstat      = 10
	SFTPOpendir       = 11
	SFTPReaddir       = 12
	SFTPRemove        = 13
	SFTPMkdir         = 14
	SFTPRmdir         = 15
	SFTPRealpath      = 16
	SFTPStat          = 17
	SFTPRename        = 18
	SFTPReadlink      = 19
	SFTPSymlink       = 20
	SFTPStatus        = 101
	SFTPHandle        = 102
	SFTPData          = 103
	SFTPName          = 104
	SFTPAttrsReply    = 105
	SFTPExtended      = 200
	SFTPExtendedReply = 201
)

// The status codes of SFTP version 3.
const (
	SFTPOK               = 0
	SFTPEOF              = 1
	SFTPNoSuchFile       = 2
	SFTPPermissionDenied = 3
	SFTPFailure          = 4
	SFTPBadMessage       = 5
	SFTPOpUnsupported    = 8
)

// The flags of an open request, and of attributes.
const (
	SFTPOpenRead   = 0x01
	SFTPOpenWrite  = 0x02
	SFTPOpenAppend = 0x04
	SFTPOpenCreate = 0x08
	SFTPOpenTrunc  = 0x10
	SFTPOpenExcl   = 0x20

	SFTPAttrSize        = 0x01
	SFTPAttrUIDGID      = 0x02
	SFTPAttrPermissions = 0x04
	SFTPAttrTimes       = 0x08
	SFTPAttrExtended    = 0x80000000
)

// SFTPAttrs are the attributes of a file, as the protocol carries them.
type SFTPAttrs struct {
	Flags        uint32
	Size         uint64
	UID, GID     uint32
	Mode         uint32
	Atime, Mtime uint32
	// Extended are the pairs of extended attributes, name then value; sent only with the
	// SFTPAttrExtended flag.
	Extended []string
}

// SFTPEntry is one name of a NAME answer.
type SFTPEntry struct {
	Name, LongName string
	Attrs          SFTPAttrs
}

// SFTPReply is one packet from the server.
type SFTPReply struct {
	// Type is the packet's type, and ID the request it answers (0 for VERSION).
	Type byte
	ID   uint32
	// Body is what follows the id.
	Body []byte

	t testing.TB
}

// SFTPClient is a minimal SFTP client for tests: it sends the packets a test asks for, well
// formed or not, and reads the answers one by one.
type SFTPClient struct {
	t      testing.TB
	rw     io.ReadWriter
	lastID uint32
}

// NewSFTPClient makes a client on a stream to an SFTP server. It sends nothing: a test that
// wants a working session calls Init.
func NewSFTPClient(t testing.TB, rw io.ReadWriter) *SFTPClient {
	return &SFTPClient{t: t, rw: rw}
}

// SFTPPack encodes the fields of a packet: uint32 and uint64 as numbers, a string or []byte
// with its length before it, SFTPAttrs as attributes, and SFTPRaw as it is.
func SFTPPack(fields ...any) []byte {
	var b []byte
	for _, field := range fields {
		switch v := field.(type) {
		case uint32:
			b = binary.BigEndian.AppendUint32(b, v)
		case int:
			b = binary.BigEndian.AppendUint32(b, uint32(v))
		case uint64:
			b = binary.BigEndian.AppendUint64(b, v)
		case string:
			b = binary.BigEndian.AppendUint32(b, uint32(len(v)))
			b = append(b, v...)
		case []byte:
			b = binary.BigEndian.AppendUint32(b, uint32(len(v)))
			b = append(b, v...)
		case SFTPRaw:
			b = append(b, v...)
		case SFTPAttrs:
			b = binary.BigEndian.AppendUint32(b, v.Flags)
			if v.Flags&SFTPAttrSize != 0 {
				b = binary.BigEndian.AppendUint64(b, v.Size)
			}
			if v.Flags&SFTPAttrUIDGID != 0 {
				b = binary.BigEndian.AppendUint32(b, v.UID)
				b = binary.BigEndian.AppendUint32(b, v.GID)
			}
			if v.Flags&SFTPAttrPermissions != 0 {
				b = binary.BigEndian.AppendUint32(b, v.Mode)
			}
			if v.Flags&SFTPAttrTimes != 0 {
				b = binary.BigEndian.AppendUint32(b, v.Atime)
				b = binary.BigEndian.AppendUint32(b, v.Mtime)
			}
			if v.Flags&SFTPAttrExtended != 0 {
				b = binary.BigEndian.AppendUint32(b, uint32(len(v.Extended)/2))
				for _, s := range v.Extended {
					b = binary.BigEndian.AppendUint32(b, uint32(len(s)))
					b = append(b, s...)
				}
			}
		default:
			panic("sshtest: SFTPPack cannot encode this field")
		}
	}
	return b
}

// SFTPRaw are bytes that SFTPPack puts into a packet as they are.
type SFTPRaw []byte

// SendRaw writes bytes to the server as they are: a packet a test built itself, or part of one.
func (c *SFTPClient) SendRaw(data []byte) {
	c.t.Helper()
	if _, err := c.rw.Write(data); err != nil {
		c.t.Fatalf("sshtest: writing to the SFTP server: %v", err)
	}
}

// SendPacket writes one packet: its length, its type and the body.
func (c *SFTPClient) SendPacket(kind byte, body []byte) {
	c.t.Helper()
	packet := binary.BigEndian.AppendUint32(nil, uint32(len(body)+1))
	packet = append(packet, kind)
	c.SendRaw(append(packet, body...))
}

// Send writes a request with a new id and the fields (SFTPPack), and returns the id.
func (c *SFTPClient) Send(kind byte, fields ...any) uint32 {
	c.t.Helper()
	c.lastID++
	c.SendPacket(kind, append(binary.BigEndian.AppendUint32(nil, c.lastID), SFTPPack(fields...)...))
	return c.lastID
}

// TryRecv reads one packet from the server; ok is false when the server ended the stream
// instead.
func (c *SFTPClient) TryRecv() (reply SFTPReply, ok bool) {
	c.t.Helper()
	var header [4]byte
	if _, err := io.ReadFull(c.rw, header[:]); err != nil {
		return SFTPReply{}, false
	}
	length := binary.BigEndian.Uint32(header[:])
	if length < 1 || length > 1<<20 {
		c.t.Fatalf("sshtest: the SFTP server sent a packet of %d bytes", length)
	}
	packet := make([]byte, length)
	if _, err := io.ReadFull(c.rw, packet); err != nil {
		c.t.Fatalf("sshtest: the SFTP server's packet of %d bytes was cut: %v", length, err)
	}
	reply = SFTPReply{Type: packet[0], Body: packet[1:], t: c.t}
	if reply.Type != SFTPVersion {
		if len(reply.Body) < 4 {
			c.t.Fatalf("sshtest: the SFTP server's packet of type %d has no id", reply.Type)
		}
		reply.ID, reply.Body = binary.BigEndian.Uint32(reply.Body), reply.Body[4:]
	}
	return reply, true
}

// Recv reads one packet from the server.
func (c *SFTPClient) Recv() SFTPReply {
	c.t.Helper()
	reply, ok := c.TryRecv()
	if !ok {
		c.t.Fatalf("sshtest: the SFTP server ended the stream")
	}
	return reply
}

// Call sends a request and reads its answer.
func (c *SFTPClient) Call(kind byte, fields ...any) SFTPReply {
	c.t.Helper()
	id := c.Send(kind, fields...)
	reply := c.Recv()
	if reply.ID != id {
		c.t.Fatalf("sshtest: the answer is for the request %d, want %d", reply.ID, id)
	}
	return reply
}

// Init sends INIT and returns the server's version and its extensions by name.
func (c *SFTPClient) Init() (version uint32, extensions map[string]string) {
	c.t.Helper()
	c.SendPacket(SFTPInit, SFTPPack(uint32(3)))
	reply := c.Recv()
	if reply.Type != SFTPVersion {
		c.t.Fatalf("sshtest: the answer to INIT has the type %d", reply.Type)
	}
	f := &sftpFields{t: c.t, b: reply.Body}
	version = f.uint32()
	extensions = map[string]string{}
	for len(f.b) > 0 {
		name := f.str()
		extensions[name] = f.str()
	}
	return version, extensions
}

// sftpFields takes an answer apart; a field that is not there fails the test.
type sftpFields struct {
	t testing.TB
	b []byte
}

func (f *sftpFields) take(n int) []byte {
	f.t.Helper()
	if len(f.b) < n {
		f.t.Fatalf("sshtest: the SFTP answer is %d bytes short", n-len(f.b))
	}
	out := f.b[:n]
	f.b = f.b[n:]
	return out
}

func (f *sftpFields) uint32() uint32 {
	f.t.Helper()
	return binary.BigEndian.Uint32(f.take(4))
}

func (f *sftpFields) uint64() uint64 {
	f.t.Helper()
	return binary.BigEndian.Uint64(f.take(8))
}

func (f *sftpFields) str() string {
	f.t.Helper()
	return string(f.take(int(f.uint32())))
}

func (f *sftpFields) attrs() SFTPAttrs {
	f.t.Helper()
	a := SFTPAttrs{Flags: f.uint32()}
	if a.Flags&SFTPAttrSize != 0 {
		a.Size = f.uint64()
	}
	if a.Flags&SFTPAttrUIDGID != 0 {
		a.UID, a.GID = f.uint32(), f.uint32()
	}
	if a.Flags&SFTPAttrPermissions != 0 {
		a.Mode = f.uint32()
	}
	if a.Flags&SFTPAttrTimes != 0 {
		a.Atime, a.Mtime = f.uint32(), f.uint32()
	}
	if a.Flags&SFTPAttrExtended != 0 {
		for count := f.uint32(); count > 0; count-- {
			a.Extended = append(a.Extended, f.str(), f.str())
		}
	}
	return a
}

// want fails the test when the answer is not of the type.
func (r SFTPReply) want(kind byte) *sftpFields {
	r.t.Helper()
	if r.Type != kind {
		if r.Type == SFTPStatus {
			code, message := r.Status()
			r.t.Fatalf("sshtest: the SFTP answer is the status %d (%s), want the type %d", code, message, kind)
		}
		r.t.Fatalf("sshtest: the SFTP answer has the type %d, want %d", r.Type, kind)
	}
	return &sftpFields{t: r.t, b: r.Body}
}

// Status returns the code and the message of a STATUS answer.
func (r SFTPReply) Status() (code uint32, message string) {
	r.t.Helper()
	if r.Type != SFTPStatus {
		r.t.Fatalf("sshtest: the SFTP answer has the type %d, want a status", r.Type)
	}
	f := &sftpFields{t: r.t, b: r.Body}
	code, message = f.uint32(), f.str()
	f.str() // the language tag
	if len(f.b) != 0 {
		r.t.Fatalf("sshtest: %d bytes follow the status", len(f.b))
	}
	return code, message
}

// Code returns the status code of a STATUS answer, and SFTPOK for any other answer: those
// carry what was asked for.
func (r SFTPReply) Code() uint32 {
	r.t.Helper()
	if r.Type != SFTPStatus {
		return SFTPOK
	}
	code, _ := r.Status()
	return code
}

// Handle returns the handle of a HANDLE answer.
func (r SFTPReply) Handle() string {
	r.t.Helper()
	return r.want(SFTPHandle).str()
}

// Data returns the bytes of a DATA answer.
func (r SFTPReply) Data() []byte {
	r.t.Helper()
	return []byte(r.want(SFTPData).str())
}

// Attrs returns the attributes of an ATTRS answer.
func (r SFTPReply) Attrs() SFTPAttrs {
	r.t.Helper()
	return r.want(SFTPAttrsReply).attrs()
}

// Names returns the entries of a NAME answer.
func (r SFTPReply) Names() []SFTPEntry {
	r.t.Helper()
	f := r.want(SFTPName)
	var entries []SFTPEntry
	for count := f.uint32(); count > 0; count-- {
		entries = append(entries, SFTPEntry{Name: f.str(), LongName: f.str(), Attrs: f.attrs()})
	}
	return entries
}

// Open opens a file and returns its handle; it fails the test when the server refuses.
func (c *SFTPClient) Open(path string, flags uint32, attrs SFTPAttrs) string {
	c.t.Helper()
	return c.Call(SFTPOpen, path, flags, attrs).Handle()
}

// Close closes a handle; it fails the test when the server refuses.
func (c *SFTPClient) Close(handle string) {
	c.t.Helper()
	if code, message := c.Call(SFTPClose, handle).Status(); code != SFTPOK {
		c.t.Fatalf("sshtest: closing an SFTP handle: status %d (%s)", code, message)
	}
}

// Read asks for length bytes at the offset. It returns the bytes, and the status when there
// were none (SFTPEOF at the end of the file).
func (c *SFTPClient) Read(handle string, offset uint64, length uint32) ([]byte, uint32) {
	c.t.Helper()
	reply := c.Call(SFTPRead, handle, offset, length)
	if reply.Type == SFTPStatus {
		return nil, reply.Code()
	}
	return reply.Data(), SFTPOK
}

// Write writes the bytes at the offset and returns the status.
func (c *SFTPClient) Write(handle string, offset uint64, data []byte) uint32 {
	c.t.Helper()
	return c.Call(SFTPWrite, handle, offset, data).Code()
}

// Uint64s returns the numbers of an EXTENDED_REPLY answer that is made of 64-bit numbers, as
// those of statvfs and limits are.
func (r SFTPReply) Uint64s() []uint64 {
	r.t.Helper()
	f := r.want(SFTPExtendedReply)
	if len(f.b)%8 != 0 {
		r.t.Fatalf("sshtest: the SFTP answer has %d bytes, not a row of 64-bit numbers", len(f.b))
	}
	var out []uint64
	for len(f.b) > 0 {
		out = append(out, f.uint64())
	}
	return out
}
