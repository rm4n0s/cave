package sftp

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// darwinNameMax is the longest name of a file on the file systems of macOS (APFS and HFS+):
// statfs does not tell it.
const darwinNameMax = 255

// systemRenameNoReplace renames a file unless the new name is taken, in one step of the system
// (renamex_np with RENAME_EXCL).
func systemRenameNoReplace(oldPath, newPath string) error {
	return unix.RenamexNp(oldPath, newPath, unix.RENAME_EXCL)
}

// atimeOf and mtimeOf are the times of a file's last access and change, in seconds.
func atimeOf(stat *syscall.Stat_t) int64 { return stat.Atimespec.Sec }
func mtimeOf(stat *syscall.Stat_t) int64 { return stat.Mtimespec.Sec }

// vfsOf turns what the system says about a file system into what statvfs answers: macOS has
// one block size, and its own numbers for the flags.
func vfsOf(stat *unix.Statfs_t) VFS {
	var flags uint64
	if stat.Flags&unix.MNT_RDONLY != 0 {
		flags |= VFSReadOnly
	}
	if stat.Flags&unix.MNT_NOSUID != 0 {
		flags |= VFSNoSUID
	}
	return VFS{
		BlockSize: uint64(stat.Bsize), FragmentSize: uint64(stat.Bsize),
		Blocks: stat.Blocks, BlocksFree: stat.Bfree, BlocksAvail: stat.Bavail,
		Files: stat.Files, FilesFree: stat.Ffree, FilesAvail: stat.Ffree,
		FSID:    uint64(uint32(stat.Fsid.Val[0])) | uint64(uint32(stat.Fsid.Val[1]))<<32,
		Flags:   flags,
		NameMax: darwinNameMax,
	}
}
