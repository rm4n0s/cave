package sftp

import (
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/rm4n0s/errors"
	"golang.org/x/sys/windows"
)

// windowsFS is the whole file system of Windows as the user sees it (docs/SPEC.md §13.4): its
// paths start with the drive, "/C:/Users/ada", and "/" lists the drives. It starts in the home
// folder.
type windowsFS struct {
	// home is the home folder as an SFTP path, or "/" when there is none.
	home string
	// now is the time, for the long names of a listing.
	now func() time.Time
	// drives lists the letters of the drives that exist, as "C:", "D:".
	drives func() []string
}

// NewFileSystem returns the file system of this computer for SFTP sessions: every path the OS
// user may reach, with relative paths starting in home (a folder of Windows, `C:\Users\ada`). A
// home that is not a path with a drive is the root.
func NewFileSystem(home string) (FileSystem, error) {
	sftpHome := "/"
	if winPath, isRoot := toWindows(fromWindows(home)); !isRoot && winPath != "" {
		sftpHome = fromWindows(winPath)
	}
	return &windowsFS{home: sftpHome, now: time.Now, drives: logicalDrives}, nil
}

// logicalDrives lists the drives of the computer: one bit of GetLogicalDrives for each letter.
func logicalDrives() []string {
	mask, err := windows.GetLogicalDrives()
	if err != nil {
		return nil
	}
	var drives []string
	for i := 0; i < 26; i++ {
		if mask&(1<<i) != 0 {
			drives = append(drives, string(rune('A'+i))+":")
		}
	}
	return drives
}

// statusOf turns an error of Windows into the status a client is told, with Windows' own
// message (docs/SPEC.md §13.4): a file that is not there is NO_SUCH_FILE; access denied and a
// sharing violation are PERMISSION_DENIED; what Windows does not do (a link without the
// privilege, a file system that has none) is OP_UNSUPPORTED; the rest is FAILURE.
func statusOf(err error) Status {
	if err == nil {
		return Status{}
	}
	status := Status{Code: StatusFailure}
	switch {
	case errors.Is(err, fs.ErrNotExist), errors.Is(err, syscall.ENOTDIR):
		status.Code = StatusNoSuchFile
	case errors.Is(err, fs.ErrPermission), errors.Is(err, windows.ERROR_SHARING_VIOLATION), errors.Is(err, windows.ERROR_LOCK_VIOLATION):
		status.Code = StatusPermissionDenied
	case errors.Is(err, syscall.ENOSYS), errors.Is(err, windows.ERROR_NOT_SUPPORTED), errors.Is(err, windows.ERROR_INVALID_FUNCTION),
		errors.Is(err, windows.ERROR_PRIVILEGE_NOT_HELD):
		status.Code = StatusOpUnsupported
	}
	var errno syscall.Errno
	if errors.As(err, &errno) {
		status.Message = strings.TrimRight(errno.Error(), ". \r\n")
	}
	return status
}

// The statuses of the paths that are not a place on a drive.
var (
	// noSuchPlace is a path that does not start with a drive.
	noSuchPlace = Status{Code: StatusNoSuchFile, Message: "no such drive"}
	// rootDenied is what is asked of "/", the list of the drives, that cannot be done to it.
	rootDenied = Status{Code: StatusPermissionDenied, Message: "the list of the drives cannot be changed"}
)

// resolve makes a client's path one of Windows: a relative path starts in the home folder.
// isRoot says that the path is "/". A path that is no place on Windows has the status in bad.
func (w *windowsFS) resolve(p string) (winPath string, isRoot bool, bad Status) {
	return w.resolveFor(p, false)
}

// resolveFor is resolve for a path that is made (creating): a name directly in "/" is refused
// as the root is, since nothing can be made there.
func (w *windowsFS) resolveFor(p string, creating bool) (winPath string, isRoot bool, bad Status) {
	if p == "" {
		p = "."
	}
	if !strings.HasPrefix(p, "/") && !strings.HasPrefix(p, `\`) {
		p = strings.TrimSuffix(w.home, "/") + "/" + p
	}
	winPath, isRoot = toWindows(p)
	if winPath == "" && !isRoot {
		if creating && isRootChild(p) {
			return "", false, rootDenied
		}
		return "", false, noSuchPlace
	}
	return winPath, isRoot, Status{}
}

// rootAttrs are the attributes of "/": a folder nobody can write to.
func (w *windowsFS) rootAttrs() Attrs {
	now := seconds(w.now().Unix())
	return Attrs{Flags: AttrSize | AttrUIDGID | AttrPermissions | AttrTimes, Mode: modeDir | 0o555, Atime: now, Mtime: now}
}

// attrsOf returns the attributes of a file from what Windows says about it: the permissions as
// Go reports them, the owner and the group 0.
func attrsOf(info os.FileInfo) Attrs {
	mode := uint32(info.Mode().Perm())
	switch {
	case info.IsDir():
		mode |= modeDir
	case info.Mode()&os.ModeSymlink != 0:
		mode |= modeLink
	default:
		mode |= modeRegular
	}
	atime := info.ModTime()
	if data, ok := info.Sys().(*syscall.Win32FileAttributeData); ok {
		atime = time.Unix(0, data.LastAccessTime.Nanoseconds())
	}
	return Attrs{
		Flags: AttrSize | AttrUIDGID | AttrPermissions | AttrTimes,
		Size:  uint64(info.Size()), Mode: mode,
		Atime: seconds(atime.Unix()), Mtime: seconds(info.ModTime().Unix()),
	}
}

// fileMode turns permission bits of POSIX into Go's. Only the bit of writing means anything on
// Windows: without it a file is read-only.
func fileMode(mode uint32) os.FileMode {
	return os.FileMode(mode & 0o777)
}

func (w *windowsFS) Open(p string, flags uint32, attrs Attrs) (File, Status) {
	winPath, isRoot, bad := w.resolveFor(p, flags&OpenCreate != 0)
	if isRoot {
		return nil, rootDenied
	}
	if !bad.OK() {
		return nil, bad
	}
	mode := 0
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
	file, err := os.OpenFile(winPath, mode, perm)
	if err != nil {
		return nil, statusOf(err)
	}
	if info, err := file.Stat(); err == nil && info.IsDir() {
		_ = file.Close() // a folder is listed, not read
		return nil, Status{Code: StatusFailure, Message: "is a directory"}
	}
	return &windowsFile{file: file, appending: flags&OpenAppend != 0}, Status{}
}

func (w *windowsFS) Stat(p string) (Attrs, Status) {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return w.rootAttrs(), Status{}
	}
	if !bad.OK() {
		return Attrs{}, bad
	}
	info, err := os.Stat(winPath)
	if err != nil {
		return Attrs{}, statusOf(err)
	}
	return attrsOf(info), Status{}
}

func (w *windowsFS) Lstat(p string) (Attrs, Status) {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return w.rootAttrs(), Status{}
	}
	if !bad.OK() {
		return Attrs{}, bad
	}
	info, err := os.Lstat(winPath)
	if err != nil {
		return Attrs{}, statusOf(err)
	}
	return attrsOf(info), Status{}
}

func (w *windowsFS) SetStat(p string, attrs Attrs) Status {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	return setAttrs(attrs,
		func(size int64) error { return os.Truncate(winPath, size) },
		func(mode os.FileMode) error { return os.Chmod(winPath, mode) },
		func(atime, mtime time.Time) error { return os.Chtimes(winPath, atime, mtime) },
		func(int, int) error { return nil }, // Windows has no owner and group to set: accepted, and nothing is done
	)
}

func (w *windowsFS) Remove(p string) Status {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	if info, err := os.Lstat(winPath); err == nil && info.IsDir() {
		// os.Remove would remove an empty folder: a folder is removed with its own request.
		return Status{Code: StatusFailure, Message: "is a directory"}
	}
	return statusOf(os.Remove(winPath))
}

func (w *windowsFS) RealPath(p string) (string, Status) {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return "/", Status{}
	}
	if !bad.OK() {
		return "", bad
	}
	resolved, err := filepath.EvalSymlinks(winPath)
	if err == nil {
		return fromWindows(resolved), Status{}
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return "", statusOf(err)
	}
	// The last part may be missing, as with OpenSSH: a client asks where a file it is about
	// to make will be.
	cut := strings.LastIndex(winPath, `\`)
	if cut < 0 || cut == len(winPath)-1 {
		return "", statusOf(err)
	}
	parent, parentErr := filepath.EvalSymlinks(winPath[:cut+1])
	if parentErr != nil {
		return "", statusOf(parentErr)
	}
	return fromWindows(strings.TrimSuffix(parent, `\`) + `\` + winPath[cut+1:]), Status{}
}

func (w *windowsFS) OpenDir(p string) (Dir, Status) {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return &driveList{fs: w, names: append([]string{".", ".."}, w.drives()...)}, Status{}
	}
	if !bad.OK() {
		return nil, bad
	}
	file, err := os.Open(winPath)
	if err != nil {
		return nil, statusOf(err)
	}
	if info, err := file.Stat(); err != nil || !info.IsDir() {
		_ = file.Close() // not a folder
		if err == nil {
			err = syscall.ENOTDIR
		}
		return nil, statusOf(err)
	}
	return &windowsDir{fs: w, file: file, path: winPath}, Status{}
}

func (w *windowsFS) Mkdir(p string, _ Attrs) Status {
	winPath, isRoot, bad := w.resolveFor(p, true)
	if isRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	// The permissions mean nothing for a folder on Windows.
	return statusOf(os.Mkdir(winPath, 0o777))
}

func (w *windowsFS) Rmdir(p string) Status {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	if info, err := os.Lstat(winPath); err == nil && !info.IsDir() {
		// os.Remove would remove a file: a file is removed with its own request.
		return Status{Code: StatusFailure, Message: "not a directory"}
	}
	return statusOf(os.Remove(winPath))
}

func (w *windowsFS) Rename(oldPath, newPath string, replace bool) Status {
	from, fromRoot, bad := w.resolve(oldPath)
	if fromRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	to, toRoot, bad := w.resolveFor(newPath, true)
	if toRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	if !replace {
		// os.Rename replaces a file that has the new name: look first. Another program may take
		// the name in between; OpenSSH has the same gap.
		if _, err := os.Lstat(to); err == nil {
			return Status{Code: StatusFailure, Message: "file already exists"}
		} else if !errors.Is(err, fs.ErrNotExist) {
			return statusOf(err)
		}
	}
	return statusOf(os.Rename(from, to))
}

func (w *windowsFS) ReadLink(p string) (string, Status) {
	winPath, isRoot, bad := w.resolve(p)
	if isRoot {
		return "", Status{Code: StatusFailure, Message: "not a link"}
	}
	if !bad.OK() {
		return "", bad
	}
	target, err := os.Readlink(winPath)
	if err != nil {
		return "", statusOf(err)
	}
	return fromWindows(target), Status{}
}

func (w *windowsFS) Symlink(target, linkPath string) Status {
	winPath, isRoot, bad := w.resolveFor(linkPath, true)
	if isRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	// The target is kept as it is written, in the form of Windows when it names a place on a
	// drive, and as the client wrote it when it is relative.
	if winTarget, _ := toWindows(target); strings.HasPrefix(target, "/") && winTarget != "" {
		target = winTarget
	}
	return statusOf(os.Symlink(target, winPath)) // without the privilege Windows refuses, and statusOf says OP_UNSUPPORTED
}

func (w *windowsFS) Link(oldPath, newPath string) Status {
	from, fromRoot, bad := w.resolve(oldPath)
	if fromRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	to, toRoot, bad := w.resolveFor(newPath, true)
	if toRoot {
		return rootDenied
	}
	if !bad.OK() {
		return bad
	}
	return statusOf(os.Link(from, to)) // as in Symlink
}

func (w *windowsFS) StatVFS(string) (VFS, Status) {
	return VFS{}, Status{Code: StatusOpUnsupported, Message: "statvfs is not supported on Windows"}
}

// entry is one name of a listing, with its long name: the owner and the group are 0.
func (w *windowsFS) entry(name string, info os.FileInfo) Entry {
	attrs := attrsOf(info)
	return Entry{Name: name, Attrs: attrs, LongName: longName(name, attrs.Mode, 1, "0", "0", attrs.Size, info.ModTime(), w.now())}
}

// driveList is "/" opened: ".", ".." and the drives.
type driveList struct {
	fs    *windowsFS
	names []string
}

func (d *driveList) Read(max int) ([]Entry, Status) {
	var entries []Entry
	for len(entries) < max && len(d.names) > 0 {
		name := d.names[0]
		d.names = d.names[1:]
		attrs := d.fs.rootAttrs()
		entries = append(entries, Entry{Name: name, Attrs: attrs, LongName: longName(name, attrs.Mode, 1, "0", "0", 0, d.fs.now(), d.fs.now())})
	}
	return entries, Status{}
}

func (d *driveList) Close() Status { return Status{} }

// windowsDir is an open folder.
type windowsDir struct {
	fs   *windowsFS
	file *os.File
	path string
	// dots says that "." and ".." were handed out.
	dots bool
}

func (d *windowsDir) Read(max int) ([]Entry, Status) {
	var entries []Entry
	if !d.dots {
		// The listing of Go leaves them out; OpenSSH's has them, and clients expect them. The
		// ".." of the root of a drive does not exist, and is left out.
		d.dots = true
		for _, name := range []string{".", ".."} {
			if info, err := os.Lstat(strings.TrimSuffix(d.path, `\`) + `\` + name); err == nil {
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

func (d *windowsDir) Close() Status {
	return statusOf(d.file.Close())
}

// windowsFile is an open file.
type windowsFile struct {
	file *os.File
	// appending says that writes go to the end of the file whatever offset they name.
	appending bool
}

func (f *windowsFile) ReadAt(p []byte, offset uint64) (int, Status) {
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

func (f *windowsFile) WriteAt(p []byte, offset uint64) Status {
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

func (f *windowsFile) Stat() (Attrs, Status) {
	info, err := f.file.Stat()
	if err != nil {
		return Attrs{}, statusOf(err)
	}
	return attrsOf(info), Status{}
}

func (f *windowsFile) SetStat(attrs Attrs) Status {
	return setAttrs(attrs, f.file.Truncate, f.file.Chmod, f.chtimes, func(int, int) error { return nil })
}

// chtimes sets the times of the open file, whatever its name is by now.
func (f *windowsFile) chtimes(atime, mtime time.Time) error {
	conn, err := f.file.SyscallConn()
	if err != nil {
		return err
	}
	var result error
	access, modified := windows.NsecToFiletime(atime.UnixNano()), windows.NsecToFiletime(mtime.UnixNano())
	if err := conn.Control(func(fd uintptr) { result = windows.SetFileTime(windows.Handle(fd), nil, &access, &modified) }); err != nil {
		return err
	}
	return result
}

func (f *windowsFile) Sync() Status {
	return statusOf(f.file.Sync())
}

func (f *windowsFile) StatVFS() (VFS, Status) {
	return VFS{}, Status{Code: StatusOpUnsupported, Message: "statvfs is not supported on Windows"}
}

func (f *windowsFile) Close() Status {
	return statusOf(f.file.Close())
}
