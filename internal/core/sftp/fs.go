package sftp

// FileSystem is what an SFTP session may do with files. Paths are the client's own text: a
// relative one starts in the home folder. Every method answers with a Status; the zero Status
// is success.
type FileSystem interface {
	// Open opens a file. flags are the Open bits of the protocol; attrs are the attributes the
	// client asks a new file to have (only the permissions are used).
	Open(path string, flags uint32, attrs Attrs) (File, Status)
	// Stat returns the attributes of what the path leads to, following links.
	Stat(path string) (Attrs, Status)
	// Lstat returns the attributes of the path itself: of the link, when it is one.
	Lstat(path string) (Attrs, Status)
	// SetStat changes the attributes that attrs.Flags names.
	SetStat(path string, attrs Attrs) Status
	// Remove removes a file that is not a folder.
	Remove(path string) Status
	// RealPath returns the absolute path without links, "." or "..". The last part of the path
	// need not exist.
	RealPath(path string) (string, Status)
}

// File is an open file of a session.
type File interface {
	// ReadAt reads at most len(p) bytes from the offset. It returns how many it read; when it
	// read none, the Status says why, and success means the end of the file.
	ReadAt(p []byte, offset uint64) (int, Status)
	// WriteAt writes all of p at the offset; a file opened for appending writes at its end.
	WriteAt(p []byte, offset uint64) Status
	// Stat returns the file's attributes.
	Stat() (Attrs, Status)
	// SetStat changes the attributes that attrs.Flags names.
	SetStat(attrs Attrs) Status
	// Close closes the file.
	Close() Status
}
