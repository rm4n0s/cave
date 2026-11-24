package sftp

import (
	"testing"

	"github.com/rm4n0s/cave/internal/core/sshtest"

	"golang.org/x/sys/unix"
)

// What a client is told when it removes a folder with REMOVE: the system's unlink fails with
// EISDIR.
const (
	removeFolderCode    = sshtest.SFTPFailure
	removeFolderMessage = "is a directory"
)

// fdDir lists the files this process has open.
const fdDir = "/proc/self/fd"

func TestVFSOf(t *testing.T) {
	stat := &unix.Statfs_t{Bsize: 4096, Frsize: 0, Blocks: 100, Bfree: 60, Bavail: 50, Files: 10, Ffree: 4, Namelen: 255, Flags: 1 | 2 | 1024}
	stat.Fsid.Val = [2]int32{-1, 2}
	want := VFS{
		BlockSize: 4096, FragmentSize: 4096, Blocks: 100, BlocksFree: 60, BlocksAvail: 50, Files: 10, FilesFree: 4, FilesAvail: 4,
		FSID: 0xffffffff | 2<<32, Flags: VFSReadOnly | VFSNoSUID, NameMax: 255,
	}
	if got := vfsOf(stat); got != want {
		t.Errorf("vfsOf = %+v, want %+v", got, want)
	}
	stat.Frsize = 512
	if got := vfsOf(stat); got.FragmentSize != 512 {
		t.Errorf("the fragment size is %d", got.FragmentSize)
	}
}

// fixedFileTotal says whether the number of files a file system can hold stays the same while
// the test runs.
const fixedFileTotal = true
