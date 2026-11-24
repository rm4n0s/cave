package sftp

import (
	"testing"

	"github.com/rm4n0s/cave/internal/core/sshtest"

	"golang.org/x/sys/unix"
)

// What a client is told when it removes a folder with REMOVE: the system's unlink fails with
// EPERM.
const (
	removeFolderCode    = sshtest.SFTPPermissionDenied
	removeFolderMessage = "operation not permitted"
)

// fdDir lists the files this process has open.
const fdDir = "/dev/fd"

func TestVFSOf(t *testing.T) {
	stat := &unix.Statfs_t{Bsize: 4096, Blocks: 100, Bfree: 60, Bavail: 50, Files: 10, Ffree: 4,
		Flags: unix.MNT_RDONLY | unix.MNT_NOSUID | unix.MNT_LOCAL}
	stat.Fsid.Val = [2]int32{-1, 2}
	want := VFS{
		BlockSize: 4096, FragmentSize: 4096, Blocks: 100, BlocksFree: 60, BlocksAvail: 50, Files: 10, FilesFree: 4, FilesAvail: 4,
		FSID: 0xffffffff | 2<<32, Flags: VFSReadOnly | VFSNoSUID, NameMax: 255,
	}
	if got := vfsOf(stat); got != want {
		t.Errorf("vfsOf = %+v, want %+v", got, want)
	}
	stat.Flags = unix.MNT_LOCAL
	if got := vfsOf(stat); got.Flags != 0 {
		t.Errorf("the flags are %#x", got.Flags)
	}
}

// fixedFileTotal says whether the number of files a file system can hold stays the same while
// the test runs: APFS counts the files there are and the ones there is room for, which changes
// as other programs make and remove files.
const fixedFileTotal = false
