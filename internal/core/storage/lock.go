package storage

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/rm4n0s/errors"
)

// lockFileName is the name of the lock file in the state folder.
const lockFileName = "lock"

// holderMaxBytes bounds what is read of a lock file to learn who holds it.
const holderMaxBytes = 256

// Lock is the hold of one instance of Cave on its folders (docs/SPEC.md §5.4). The operating
// system drops it when the process ends, however it ends.
type Lock struct {
	file *os.File
	ops  lockOps
}

// lockOps are the steps of taking the lock that tests replace to make them fail. The functions
// return the operating system's errors as they are.
type lockOps struct {
	// tryLock takes the exclusive lock without waiting; held is true when another holds it.
	tryLock func(f *os.File) (held bool, err error)
	// unlock drops the lock.
	unlock func(f *os.File) error
	// writeHolder replaces the file's content with the holder's program and PID.
	writeHolder func(f *os.File, program string, pid int) error
}

// Acquire takes the lock of the state folder for program ("cave" or "tcave"), and writes the
// program and its PID into the lock file. When another instance holds it, the error has the Tag
// AlreadyRunning (errors.Is matches ErrAlreadyRunning) and the holder's "program" and "pid" in
// its metadata.
func Acquire(ctx context.Context, dirs Dirs, program string) (*Lock, error) {
	return acquire(ctx, dirs, program, lockOps{tryLock: tryLock, unlock: unlock, writeHolder: writeHolder})
}

// acquire is Acquire with given steps.
func acquire(ctx context.Context, dirs Dirs, program string, ops lockOps) (*Lock, error) {
	if err := ctx.Err(); err != nil {
		return nil, errors.NewErr(TagLockCancelled, err)
	}
	path := filepath.Join(dirs.State, lockFileName)
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, errors.NewErr(TagLockOpenFailed, err).SetMetadata("path", path)
	}
	held, err := ops.tryLock(f)
	if err != nil {
		_ = f.Close() // nothing was written; the lock's error is the one that matters
		return nil, errors.NewErr(TagLockFailed, err).SetMetadata("path", path)
	}
	if held {
		holder, pid := readHolder(f)
		_ = f.Close() // nothing was written
		return nil, errors.New(TagAlreadyRunning, "Cave is already running on these folders",
			"path", path, "program", holder, "pid", pid)
	}
	if err := ops.writeHolder(f, program, os.Getpid()); err != nil {
		_ = ops.unlock(f) // closing the file drops the lock anyway
		_ = f.Close()
		return nil, errors.NewErr(TagLockWriteFailed, err).SetMetadata("path", path)
	}
	return &Lock{file: f, ops: ops}, nil
}

// Release drops the lock. A second call does nothing.
func (l *Lock) Release() error {
	if l.file == nil {
		return nil
	}
	f := l.file
	l.file = nil
	unlockErr := l.ops.unlock(f)
	closeErr := f.Close()
	if unlockErr != nil {
		return errors.NewErr(TagLockReleaseFailed, unlockErr).SetMetadata("path", f.Name())
	}
	if closeErr != nil {
		// Closing the file dropped the lock or the process's end will; the error is still told.
		return errors.NewErr(TagLockCloseFailed, closeErr).SetMetadata("path", f.Name())
	}
	return nil
}

// writeHolder replaces the lock file's content with "program pid".
func writeHolder(f *os.File, program string, pid int) error {
	if err := f.Truncate(0); err != nil {
		return err
	}
	if _, err := f.WriteAt([]byte(program+" "+strconv.Itoa(pid)+"\n"), 0); err != nil {
		return err
	}
	return f.Sync()
}

// readHolder reads who holds the lock from the lock file: the program and its PID, or "" and 0
// when the file does not say.
func readHolder(f *os.File) (program string, pid int) {
	data, err := io.ReadAll(io.NewSectionReader(f, 0, holderMaxBytes))
	if err != nil {
		return "", 0
	}
	fields := strings.Fields(string(data))
	if len(fields) != 2 {
		return "", 0
	}
	pid, err = strconv.Atoi(fields[1])
	if err != nil || pid <= 0 {
		return "", 0
	}
	return fields[0], pid
}
