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
	// OpenDir opens a folder to list it.
	OpenDir(path string) (Dir, Status)
	// Mkdir makes a folder; attrs are the attributes asked (only the permissions are used).
	Mkdir(path string, attrs Attrs) Status
	// Rmdir removes an empty folder.
	Rmdir(path string) Status
	// Rename gives a file or a folder another name. With replace, a file that has the new name
	// is replaced, as rename(2) does; without, the request fails when the new name is taken.
	Rename(oldPath, newPath string, replace bool) Status
	// ReadLink returns where a link points.
	ReadLink(path string) (string, Status)
	// Symlink makes linkPath a link that points to target; target is kept as it is written.
	Symlink(target, linkPath string) Status
	// Link makes newPath another name of the file oldPath.
	Link(oldPath, newPath string) Status
	// StatVFS says how much room the file system of the path has.
	StatVFS(path string) (VFS, Status)
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
	// Sync writes what the system still holds of the file to the disk.
	Sync() Status
	// StatVFS says how much room the file's file system has.
	StatVFS() (VFS, Status)
	// Close closes the file.
	Close() Status
}

// Dir is an open folder of a session.
type Dir interface {
	// Read returns the next entries, at most max of them; none, with success, at the end.
	Read(max int) ([]Entry, Status)
	// Close closes the folder.
	Close() Status
}

// Entry is one name of a folder.
type Entry struct {
	// Name is the name, without the folder's path.
	Name string
	// LongName is the line "ls -l" would print for it.
	LongName string
	// Attrs are its attributes: of the link itself, when it is one.
	Attrs Attrs
}

// VFS is what statvfs says about a file system.
type VFS struct {
	// BlockSize and FragmentSize are the sizes of a block, and of the unit the counts of blocks
	// are in.
	BlockSize, FragmentSize uint64
	// Blocks, BlocksFree and BlocksAvail count all blocks, the free ones, and the free ones a
	// user who is not root may take.
	Blocks, BlocksFree, BlocksAvail uint64
	// Files, FilesFree and FilesAvail count the files there may be, and how many more.
	Files, FilesFree, FilesAvail uint64
	// FSID is the id of the file system.
	FSID uint64
	// Flags has VFSReadOnly and VFSNoSUID.
	Flags uint64
	// NameMax is the longest name a file may have.
	NameMax uint64
}

// The bits of VFS.Flags.
const (
	VFSReadOnly = 1
	VFSNoSUID   = 2
)
