package sftp

import (
	"io"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// linuxFS is the whole file system, as the OS user sees it, starting in the home folder.
type linuxFS struct {
	home string
	// now is the time, for the long names of a listing.
	now func() time.Time
	// renameNoReplace renames a file unless the new name is taken, in one step of the system.
	renameNoReplace func(oldPath, newPath string) error

	// owners and groups are the names of the users and groups that listings have shown.
	mu             sync.Mutex
	owners, groups map[uint32]string
}

// NewFileSystem returns the file system of this computer for SFTP sessions: every path the OS
// user may reach, with relative paths starting in home.
func NewFileSystem(home string) (FileSystem, error) {
	if !filepath.IsAbs(home) {
		home = "/"
	}
	return &linuxFS{
		home: home, now: time.Now, owners: map[uint32]string{}, groups: map[uint32]string{},
		renameNoReplace: func(oldPath, newPath string) error {
			return unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE)
		},
	}, nil
}

// statusOf turns an error of the system into the status a client is told, with the system's own
// message (docs/SPEC.md §9.7).
func statusOf(err error) Status {
	if err == nil {
		return Status{}
	}
	status := Status{Code: StatusFailure}
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR), errors.Is(err, syscall.ELOOP):
		status.Code = StatusNoSuchFile
	case errors.Is(err, fs.ErrPermission):
		status.Code = StatusPermissionDenied
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, syscall.ENOTSUP):
		status.Code = StatusOpUnsupported
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		status.Message = errno.Error()
	}
	return status
}

// resolve makes a client's path one of the system: a relative path starts in the home folder.
// The path is not tidied, so that ".." after a link means what the system says it means.
func (l *linuxFS) resolve(path string) string {
	if path == "" {
		path = "."
	}
	if strings.HasPrefix(path, "/") {
		return path
	}
	return strings.TrimSuffix(l.home, "/") + "/" + path
}

// fileMode turns permission bits of POSIX into Go's.
func fileMode(mode uint32) os.FileMode {
	out := os.FileMode(mode & 0o777)
	if mode&unix.S_ISUID != 0 {
		out |= os.ModeSetuid
	}
	if mode&unix.S_ISGID != 0 {
		out |= os.ModeSetgid
	}
	if mode&unix.S_ISVTX != 0 {
		out |= os.ModeSticky
	}
	return out
}

// attrsOf returns the attributes of a file from what the system says about it.
func attrsOf(info os.FileInfo) Attrs {
	stat, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		// Not from the system: only what every FileInfo has.
		return Attrs{Flags: AttrSize | AttrTimes, Size: uint64(info.Size()), Atime: seconds(info.ModTime().Unix()), Mtime: seconds(info.ModTime().Unix())}
	}
	return Attrs{
		Flags: AttrSize | AttrUIDGID | AttrPermissions | AttrTimes,
		Size:  uint64(stat.Size), UID: stat.Uid, GID: stat.Gid, Mode: stat.Mode,
		Atime: seconds(stat.Atim.Sec), Mtime: seconds(stat.Mtim.Sec),
	}
}

func (l *linuxFS) Open(path string, flags uint32, attrs Attrs) (File, Status) {
	// Never wait for the other end of a pipe, and never take a terminal as Cave's own.
	mode := unix.O_NONBLOCK | unix.O_NOCTTY
	switch {
	case flags&OpenRead != 0 && flags&OpenWrite != 0:
		mode |= os.O_RDWR
	case flags&OpenWrite != 0:
		mode |= os.O_WRONLY
	default:
		mode |= os.O_RDONLY
	}
	if flags&OpenAppend != 0 {
		mode |= os.O_APPEND
	}
	if flags&OpenCreate != 0 {
		mode |= os.O_CREATE
	}
	if flags&OpenTrunc != 0 {
		mode |= os.O_TRUNC
	}
	if flags&OpenExcl != 0 {
		mode |= os.O_EXCL
	}
	perm := os.FileMode(0o666)
	if attrs.Flags&AttrPermissions != 0 {
		perm = fileMode(attrs.Mode)
	}
	file, err := os.OpenFile(l.resolve(path), mode, perm)
	if err != nil {
		return nil, statusOf(err)
	}
	return &linuxFile{file: file, appending: flags&OpenAppend != 0}, Status{}
}

func (l *linuxFS) Stat(path string) (Attrs, Status) {
	info, err := os.Stat(l.resolve(path))
	if err != nil {
		return Attrs{}, statusOf(err)
	}
	return attrsOf(info), Status{}
}

func (l *linuxFS) Lstat(path string) (Attrs, Status) {
	info, err := os.Lstat(l.resolve(path))
	if err != nil {
		return Attrs{}, statusOf(err)
	}
	return attrsOf(info), Status{}
}

func (l *linuxFS) SetStat(path string, attrs Attrs) Status {
	full := l.resolve(path)
	return setAttrs(attrs,
		func(size int64) error { return os.Truncate(full, size) },
		func(mode os.FileMode) error { return os.Chmod(full, mode) },
		func(atime, mtime time.Time) error { return os.Chtimes(full, atime, mtime) },
		func(uid, gid int) error { return os.Chown(full, uid, gid) },
	)
}

func (l *linuxFS) Remove(path string) Status {
	// unlink, not os.Remove: a folder is removed with its own request.
	return statusOf(unix.Unlink(l.resolve(path)))
}

func (l *linuxFS) RealPath(path string) (string, Status) {
	full := l.resolve(path)
	resolved, err := filepath.EvalSymlinks(full)
	if err == nil {
		return resolved, Status{}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		// Links that go in circles are an error of the library's own; the system's, which a
		// client can be told, is what a stat of the path fails with.
		if _, statErr := os.Stat(full); statErr != nil {
			return "", statusOf(statErr)
		}
		return "", statusOf(err)
	}
	// The last part may be missing, as with OpenSSH: a client asks where a file it is about
	// to make will be.
	trimmed := strings.TrimRight(full, "/")
	cut := strings.LastIndex(trimmed, "/")
	last := trimmed[cut+1:]
	if cut < 0 || last == "." || last == ".." {
		return "", statusOf(err)
	}
	parent, parentErr := filepath.EvalSymlinks(trimmed[:cut+1])
	if parentErr != nil {
		return "", statusOf(parentErr)
	}
	return filepath.Join(parent, last), Status{}
}

func (l *linuxFS) OpenDir(path string) (Dir, Status) {
	full := l.resolve(path)
	file, err := os.OpenFile(full, os.O_RDONLY|unix.O_DIRECTORY|unix.O_NONBLOCK|unix.O_NOCTTY, 0)
	if err != nil {
		return nil, statusOf(err)
	}
	return &linuxDir{fs: l, file: file, path: full}, Status{}
}

func (l *linuxFS) Mkdir(path string, attrs Attrs) Status {
	perm := os.FileMode(0o777)
	if attrs.Flags&AttrPermissions != 0 {
		perm = fileMode(attrs.Mode)
	}
	return statusOf(os.Mkdir(l.resolve(path), perm))
}

func (l *linuxFS) Rmdir(path string) Status {
	// rmdir, not os.Remove: a file is removed with its own request.
	return statusOf(unix.Rmdir(l.resolve(path)))
}

func (l *linuxFS) Rename(oldPath, newPath string, replace bool) Status {
	from, to := l.resolve(oldPath), l.resolve(newPath)
	if replace {
		return statusOf(os.Rename(from, to))
	}
	err := l.renameNoReplace(from, to)
	if err != unix.EINVAL && err != unix.ENOSYS {
		return statusOf(err)
	}
	// The file system cannot do it in one step: look first, then rename. Another program may
	// take the name in between; OpenSSH has the same gap.
	if _, statErr := os.Lstat(to); statErr == nil {
		return statusOf(syscall.EEXIST)
	} else if !errors.Is(statErr, fs.ErrNotExist) {
		return statusOf(statErr)
	}
	return statusOf(os.Rename(from, to))
}

func (l *linuxFS) ReadLink(path string) (string, Status) {
	target, err := os.Readlink(l.resolve(path))
	return target, statusOf(err)
}

func (l *linuxFS) Symlink(target, linkPath string) Status {
	return statusOf(os.Symlink(target, l.resolve(linkPath)))
}

func (l *linuxFS) Link(oldPath, newPath string) Status {
	return statusOf(os.Link(l.resolve(oldPath), l.resolve(newPath)))
}

func (l *linuxFS) StatVFS(path string) (VFS, Status) {
	var stat unix.Statfs_t
	if err := unix.Statfs(l.resolve(path), &stat); err != nil {
		return VFS{}, statusOf(err)
	}
	return vfsOf(&stat), Status{}
}

// vfsOf turns what the system says about a file system into what statvfs answers.
func vfsOf(stat *unix.Statfs_t) VFS {
	fragment := uint64(stat.Frsize)
	if fragment == 0 {
		fragment = uint64(stat.Bsize)
	}
	return VFS{
		BlockSize: uint64(stat.Bsize), FragmentSize: fragment,
		Blocks: stat.Blocks, BlocksFree: stat.Bfree, BlocksAvail: stat.Bavail,
		Files: stat.Files, FilesFree: stat.Ffree, FilesAvail: stat.Ffree,
		FSID:    uint64(uint32(stat.Fsid.Val[0])) | uint64(uint32(stat.Fsid.Val[1]))<<32,
		Flags:   uint64(stat.Flags) & (VFSReadOnly | VFSNoSUID),
		NameMax: uint64(stat.Namelen),
	}
}

// name returns the name of a user or a group for a listing: from the cache, else from lookup,
// else the number.
func (l *linuxFS) name(cache map[uint32]string, id uint32, lookup func(id string) (string, error)) string {
	l.mu.Lock()
	defer l.mu.Unlock()
	if name, ok := cache[id]; ok {
		return name
	}
	number := strconv.FormatUint(uint64(id), 10)
	name, err := lookup(number)
	if err != nil || name == "" {
		name = number // a user or a group the system has no name for
	}
	cache[id] = name
	return name
}

// userName and groupName ask the system for the name of an id.
func userName(id string) (string, error) {
	u, err := user.LookupId(id)
	if err != nil {
		return "", err
	}
	return u.Username, nil
}

func groupName(id string) (string, error) {
	g, err := user.LookupGroupId(id)
	if err != nil {
		return "", err
	}
	return g.Name, nil
}

// entry is one name of a listing, with its long name.
func (l *linuxFS) entry(name string, info os.FileInfo) Entry {
	attrs := attrsOf(info)
	links, owner, group := uint64(1), "?", "?"
	if stat, ok := info.Sys().(*syscall.Stat_t); ok {
		links = uint64(stat.Nlink)
		owner, group = l.name(l.owners, stat.Uid, userName), l.name(l.groups, stat.Gid, groupName)
	}
	return Entry{Name: name, Attrs: attrs, LongName: longName(name, attrs.Mode, links, owner, group, attrs.Size, info.ModTime(), l.now())}
}

// linuxDir is an open folder.
type linuxDir struct {
	fs   *linuxFS
	file *os.File
	path string
	// dots says that "." and ".." were handed out.
	dots bool
}

func (d *linuxDir) Read(max int) ([]Entry, Status) {
	var entries []Entry
	if !d.dots {
		// The system's listing leaves them out; OpenSSH's has them, and clients expect them.
		d.dots = true
		for _, name := range []string{".", ".."} {
			if info, err := os.Lstat(d.path + "/" + name); err == nil {
				entries = append(entries, d.fs.entry(name, info))
			}
		}
	}
	for len(entries) < max {
		items, err := d.file.ReadDir(max - len(entries))
		for _, item := range items {
			info, infoErr := item.Info()
			if infoErr != nil {
				continue // removed since it was listed
			}
			entries = append(entries, d.fs.entry(item.Name(), info))
		}
		if err == io.EOF || len(items) == 0 && err == nil {
			break
		}
		if err != nil {
			if len(entries) == 0 {
				return nil, statusOf(err)
			}
			break // what was read is told first; the next read meets the error again
		}
	}
	return entries, Status{}
}

func (d *linuxDir) Close() Status {
	return statusOf(d.file.Close())
}

// linuxFile is an open file.
type linuxFile struct {
	file *os.File
	// appending says that writes go to the end of the file whatever offset they name.
	appending bool
}

func (f *linuxFile) ReadAt(p []byte, offset uint64) (int, Status) {
	at, status := offsetOf(offset)
	if !status.OK() {
		return 0, status
	}
	n, err := f.file.ReadAt(p, at)
	if n > 0 || err == io.EOF {
		return n, Status{}
	}
	return 0, statusOf(err)
}

func (f *linuxFile) WriteAt(p []byte, offset uint64) Status {
	if f.appending {
		_, err := f.file.Write(p)
		return statusOf(err)
	}
	at, status := offsetOf(offset)
	if !status.OK() {
		return status
	}
	_, err := f.file.WriteAt(p, at)
	return statusOf(err)
}

func (f *linuxFile) Stat() (Attrs, Status) {
	info, err := f.file.Stat()
	if err != nil {
		return Attrs{}, statusOf(err)
	}
	return attrsOf(info), Status{}
}

func (f *linuxFile) SetStat(attrs Attrs) Status {
	return setAttrs(attrs, f.file.Truncate, f.file.Chmod, f.chtimes, f.file.Chown)
}

// chtimes sets the times of the open file, whatever its name is by now.
func (f *linuxFile) chtimes(atime, mtime time.Time) error {
	conn, err := f.file.SyscallConn()
	if err != nil {
		return err
	}
	var result error
	times := []unix.Timeval{unix.NsecToTimeval(atime.UnixNano()), unix.NsecToTimeval(mtime.UnixNano())}
	if err := conn.Control(func(fd uintptr) { result = unix.Futimes(int(fd), times) }); err != nil {
		return err
	}
	return result
}

func (f *linuxFile) Sync() Status {
	return statusOf(f.file.Sync())
}

func (f *linuxFile) StatVFS() (VFS, Status) {
	conn, err := f.file.SyscallConn()
	if err != nil {
		return VFS{}, statusOf(err)
	}
	var stat unix.Statfs_t
	var result error
	if err := conn.Control(func(fd uintptr) { result = unix.Fstatfs(int(fd), &stat) }); err != nil {
		return VFS{}, statusOf(err)
	}
	if result != nil {
		return VFS{}, statusOf(result)
	}
	return vfsOf(&stat), Status{}
}

func (f *linuxFile) Close() Status {
	return statusOf(f.file.Close())
}
