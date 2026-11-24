package sftp

import "encoding/binary"

// The status codes of the protocol.
const (
	StatusOK               = 0
	StatusEOF              = 1
	StatusNoSuchFile       = 2
	StatusPermissionDenied = 3
	StatusFailure          = 4
	StatusBadMessage       = 5
	StatusOpUnsupported    = 8
)

// statusTexts are the messages of the status codes, as OpenSSH words them.
var statusTexts = map[uint32]string{
	StatusOK: "Success", StatusEOF: "End of file", StatusNoSuchFile: "No such file", StatusPermissionDenied: "Permission denied",
	StatusFailure: "Failure", StatusBadMessage: "Bad message", StatusOpUnsupported: "Operation unsupported",
}

// Status is how a request ended: a code of the protocol, and a message for the person at the
// client. The zero Status is success. A FileSystem answers with a Status, not with an error: a
// file that is not there is an answer to the client, not a failure of Cave.
type Status struct {
	// Code is one of the Status constants.
	Code uint32
	// Message says more than the code does, such as the system's own words; empty for the
	// code's usual message.
	Message string
}

// OK says whether the request succeeded.
func (s Status) OK() bool {
	return s.Code == StatusOK
}

// Text returns the message a client is sent: Message, or the code's usual message.
func (s Status) Text() string {
	if s.Message != "" {
		return s.Message
	}
	if text, ok := statusTexts[s.Code]; ok {
		return text
	}
	return "Failure"
}

// The bits of the flags of an open request.
const (
	OpenRead   = 0x01
	OpenWrite  = 0x02
	OpenAppend = 0x04
	OpenCreate = 0x08
	OpenTrunc  = 0x10
	OpenExcl   = 0x20
)

// The bits of Attrs.Flags: which attributes are there.
const (
	AttrSize        = 0x01
	AttrUIDGID      = 0x02
	AttrPermissions = 0x04
	AttrTimes       = 0x08
	attrExtended    = 0x80000000
)

// Attrs are the attributes of a file, as the protocol carries them. Flags says which of the
// fields mean something.
type Attrs struct {
	Flags uint32
	// Size is the file's size in bytes (AttrSize).
	Size uint64
	// UID and GID are the owner and the group (AttrUIDGID).
	UID, GID uint32
	// Mode is the type and the permission bits, as st_mode of POSIX (AttrPermissions).
	Mode uint32
	// Atime and Mtime are the times of the last access and change, in seconds since 1970
	// (AttrTimes).
	Atime, Mtime uint32
}

// reader takes the fields of a packet apart. A field that is not all there sets bad, and every
// field after it reads as zero: the caller checks bad once, after its last field.
type reader struct {
	b   []byte
	bad bool
}

// take returns the next n bytes, or nil when they are not there.
func (r *reader) take(n uint64) []byte {
	if r.bad || uint64(len(r.b)) < n {
		r.bad, r.b = true, nil
		return nil
	}
	out := r.b[:n]
	r.b = r.b[n:]
	return out
}

func (r *reader) uint32() uint32 {
	b := r.take(4)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint32(b)
}

func (r *reader) uint64() uint64 {
	b := r.take(8)
	if b == nil {
		return 0
	}
	return binary.BigEndian.Uint64(b)
}

// bytes returns a field of bytes with its length before it. The bytes are those of the packet:
// they are valid until the next packet is read.
func (r *reader) bytes() []byte {
	n := r.uint32()
	return r.take(uint64(n))
}

// str returns a field of bytes as a string of its own.
func (r *reader) str() string {
	return string(r.bytes())
}

// attrs reads attributes. The extended ones, which Cave has no use for, are read and dropped.
func (r *reader) attrs() Attrs {
	a := Attrs{Flags: r.uint32()}
	if a.Flags&AttrSize != 0 {
		a.Size = r.uint64()
	}
	if a.Flags&AttrUIDGID != 0 {
		a.UID, a.GID = r.uint32(), r.uint32()
	}
	if a.Flags&AttrPermissions != 0 {
		a.Mode = r.uint32()
	}
	if a.Flags&AttrTimes != 0 {
		a.Atime, a.Mtime = r.uint32(), r.uint32()
	}
	if a.Flags&attrExtended != 0 {
		// Each pair takes at least 8 bytes of the packet, so the count cannot make this loop
		// longer than the packet is.
		for count := r.uint32(); count > 0 && !r.bad; count-- {
			r.bytes()
			r.bytes()
		}
	}
	a.Flags &^= attrExtended
	return a
}

// builder puts a packet together.
type builder struct {
	b []byte
}

// newPacket starts a packet of a type; its length is filled in by packet.
func newPacket(kind byte) *builder {
	return &builder{b: []byte{0, 0, 0, 0, kind}}
}

// newReply starts the answer to the request with the id.
func newReply(kind byte, id uint32) *builder {
	b := newPacket(kind)
	b.uint32(id)
	return b
}

func (b *builder) uint32(v uint32) {
	b.b = binary.BigEndian.AppendUint32(b.b, v)
}

func (b *builder) uint64(v uint64) {
	b.b = binary.BigEndian.AppendUint64(b.b, v)
}

func (b *builder) str(s string) {
	b.uint32(uint32(len(s)))
	b.b = append(b.b, s...)
}

// attrs writes attributes: the fields that Flags names.
func (b *builder) attrs(a Attrs) {
	flags := a.Flags &^ attrExtended
	b.uint32(flags)
	if flags&AttrSize != 0 {
		b.uint64(a.Size)
	}
	if flags&AttrUIDGID != 0 {
		b.uint32(a.UID)
		b.uint32(a.GID)
	}
	if flags&AttrPermissions != 0 {
		b.uint32(a.Mode)
	}
	if flags&AttrTimes != 0 {
		b.uint32(a.Atime)
		b.uint32(a.Mtime)
	}
}

// packet returns the packet with its length in front.
func (b *builder) packet() []byte {
	binary.BigEndian.PutUint32(b.b, uint32(len(b.b)-4))
	return b.b
}
