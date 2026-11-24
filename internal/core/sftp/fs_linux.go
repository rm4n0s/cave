package sftp

import (
	"io"
	"io/fs"
	"math"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/unix"
)

// linuxFS is the whole file system, as the OS user sees it, starting in the home folder.
type linuxFS struct {
	home string
}

// NewFileSystem returns the file system of this computer for SFTP sessions: every path the OS
// user may reach, with relative paths starting in home.
func NewFileSystem(home string) (FileSystem, error) {
	if !filepath.IsAbs(home) {
		home = "/"
	}
	return &linuxFS{home: home}, nil
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

// seconds fits a time into the 32 bits the protocol has for it.
func seconds(t int64) uint32 {
	if t < 0 {
		return 0
	}
	if t > math.MaxUint32 {
		return math.MaxUint32
	}
	return uint32(t)
}

// offsetOf fits an offset of the protocol into what the system takes.
func offsetOf(offset uint64) (int64, Status) {
	if offset > math.MaxInt64 {
		return 0, statusOf(syscall.EINVAL)
	}
	return int64(offset), Status{}
}

// setAttrs changes the attributes that attrs.Flags names, each with its function, in the order
// OpenSSH uses. Every one is tried; the status is that of the first that failed.
func setAttrs(attrs Attrs, truncate func(int64) error, chmod func(os.FileMode) error, chtimes func(atime, mtime time.Time) error, chown func(uid, gid int) error) Status {
	var first Status
	keep := func(err error) {
		if status := statusOf(err); first.OK() {
			first = status
		}
	}
	if attrs.Flags&AttrSize != 0 {
		if size, status := offsetOf(attrs.Size); status.OK() {
			keep(truncate(size))
		} else {
			first = status
		}
	}
	if attrs.Flags&AttrPermissions != 0 {
		keep(chmod(fileMode(attrs.Mode)))
	}
	if attrs.Flags&AttrTimes != 0 {
		keep(chtimes(time.Unix(int64(attrs.Atime), 0), time.Unix(int64(attrs.Mtime), 0)))
	}
	if attrs.Flags&AttrUIDGID != 0 {
		keep(chown(int(attrs.UID), int(attrs.GID)))
	}
	return first
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

func (f *linuxFile) Close() Status {
	return statusOf(f.file.Close())
}
