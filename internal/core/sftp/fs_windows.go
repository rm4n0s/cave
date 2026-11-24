package sftp

import "github.com/rm4n0s/errors"

// NewFileSystem returns the file system of this computer for SFTP sessions. On Windows it is
// built by step 7.3 (docs/SPEC.md §13.4); until then there is none, and the subsystem is
// refused.
func NewFileSystem(home string) (FileSystem, error) {
	return nil, errors.New(TagUnsupported, "SFTP is not built for Windows yet", "home", home)
}
