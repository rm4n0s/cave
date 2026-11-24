package sftp

import (
	"testing"

	"github.com/rm4n0s/cave/internal/core/errtest"
)

func TestSFTPIsNotBuiltForWindowsYet(t *testing.T) {
	fileSystem, err := NewFileSystem(`C:\Users\ada`)
	meta := errtest.Route(t, err, "NewFileSystem.SftpUnsupported")
	if fileSystem != nil || meta["home"] != `C:\Users\ada` {
		t.Errorf("NewFileSystem = %v, metadata %v", fileSystem, meta)
	}
}
