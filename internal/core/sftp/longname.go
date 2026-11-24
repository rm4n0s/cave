package sftp

import (
	"fmt"
	"time"
)

// The bits of a mode that say what kind of file it is, as POSIX has them.
const (
	modeTypeMask = 0o170000
	modeSocket   = 0o140000
	modeLink     = 0o120000
	modeRegular  = 0o100000
	modeBlock    = 0o060000
	modeDir      = 0o040000
	modeChar     = 0o020000
	modeFIFO     = 0o010000
	modeSetUID   = 0o4000
	modeSetGID   = 0o2000
	modeSticky   = 0o1000
)

// halfYear is how old a file may be for its long name to show the time of day instead of the
// year.
const halfYear = 365 * 24 * time.Hour / 2

// modeString is a mode as "ls -l" shows it: the kind of file and the nine permissions, such as
// "drwxr-xr-x".
func modeString(mode uint32) string {
	kind := byte('?')
	switch mode & modeTypeMask {
	case modeRegular:
		kind = '-'
	case modeDir:
		kind = 'd'
	case modeLink:
		kind = 'l'
	case modeChar:
		kind = 'c'
	case modeBlock:
		kind = 'b'
	case modeFIFO:
		kind = 'p'
	case modeSocket:
		kind = 's'
	}
	out := []byte{kind, '-', '-', '-', '-', '-', '-', '-', '-', '-'}
	for i, letter := range []byte("rwxrwxrwx") {
		if mode&(1<<(8-i)) != 0 {
			out[1+i] = letter
		}
	}
	// The special bits take the place of the three "x".
	for _, special := range []struct {
		bit      uint32
		at       int
		with, no byte
	}{{modeSetUID, 3, 's', 'S'}, {modeSetGID, 6, 's', 'S'}, {modeSticky, 9, 't', 'T'}} {
		if mode&special.bit == 0 {
			continue
		}
		if out[special.at] == 'x' {
			out[special.at] = special.with
		} else {
			out[special.at] = special.no
		}
	}
	return string(out)
}

// longName is the line "ls -l" prints for a file, in the words of OpenSSH's sftp-server, which
// clients show as it is: the mode, the number of links, the owner and the group, the size, the
// time of the last change (the time of day for a file of the last half year, else the year) and
// the name. The times are in the zone of mtime.
func longName(name string, mode uint32, links uint64, owner, group string, size uint64, mtime, now time.Time) string {
	date := mtime.Format("Jan _2  2006")
	if !mtime.After(now) && now.Sub(mtime) < halfYear {
		date = mtime.Format("Jan _2 15:04")
	}
	return fmt.Sprintf("%s  %3d %-8s %-8s %8d %s %s", modeString(mode), links, owner, group, size, date, name)
}
