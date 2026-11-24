package sftp

import (
	"math"
	"os"
	"syscall"
	"time"
)

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
