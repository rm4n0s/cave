package sftp

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// systemRenameNoReplace renames a file unless the new name is taken, in one step of the system
// (renameat2 with RENAME_NOREPLACE).
func systemRenameNoReplace(oldPath, newPath string) error {
	return unix.Renameat2(unix.AT_FDCWD, oldPath, unix.AT_FDCWD, newPath, unix.RENAME_NOREPLACE)
}

// atimeOf and mtimeOf are the times of a file's last access and change, in seconds.
func atimeOf(stat *syscall.Stat_t) int64 { return stat.Atim.Sec }
func mtimeOf(stat *syscall.Stat_t) int64 { return stat.Mtim.Sec }

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
