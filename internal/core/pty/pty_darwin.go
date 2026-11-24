package pty

import (
	"os"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/unix"
)

// The devices of the pseudo terminals. On macOS the slave's path is the system's answer: ptsDir
// is empty, and a folder given in its place (by a test) takes the slave's name from it.
const (
	ptmxPath = "/dev/ptmx"
	ptsDir   = ""
)

// keepSlave says whether CloseSlave keeps Cave's handle of the slave side. macOS drops the output still in a terminal when the last handle of its slave side closes, so Cave keeps its own handle until Release.
const keepSlave = true

// tcflag is the type of the terminal's flag words.
type tcflag = uint64

// The requests that read and write the terminal's settings, and the value of a control character
// that is turned off (_POSIX_VDISABLE).
const (
	termiosGet    = unix.TIOCGETA
	termiosSet    = unix.TIOCSETA
	posixVDisable = 0xff
)

// ptyNameSize is the size of the buffer TIOCPTYGNAME fills: 128 bytes in the system's headers.
const ptyNameSize = 128

// unlock grants and unlocks the master's slave and returns its path, such as "/dev/ttys003"
// (what grantpt, unlockpt and ptsname do in the C library). It returns the system's error as it
// is.
func unlock(master *os.File, pts string) (string, error) {
	var buf [ptyNameSize]byte
	err := control(master, func(fd int) error {
		if err := unix.IoctlSetInt(fd, unix.TIOCPTYGRANT, 0); err != nil {
			return err
		}
		if err := unix.IoctlSetInt(fd, unix.TIOCPTYUNLK, 0); err != nil {
			return err
		}
		// x/sys has no call for an ioctl that fills a buffer of this size, so it is made here,
		// as the C library's ptsname makes it.
		if _, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(fd), uintptr(unix.TIOCPTYGNAME), uintptr(unsafe.Pointer(&buf[0]))); errno != 0 {
			return errno
		}
		return nil
	})
	if err != nil {
		return "", err
	}
	name := unix.ByteSliceToString(buf[:])
	if pts != "" {
		name = filepath.Join(pts, filepath.Base(name))
	}
	return name, nil
}

// The characters of the terminal's line editing, by their opcodes in RFC 4254 §8.
var controlChars = map[uint8]int{
	1: unix.VINTR, 2: unix.VQUIT, 3: unix.VERASE, 4: unix.VKILL, 5: unix.VEOF, 6: unix.VEOL, 7: unix.VEOL2,
	8: unix.VSTART, 9: unix.VSTOP, 10: unix.VSUSP, 12: unix.VREPRINT, 13: unix.VWERASE, 14: unix.VLNEXT, 18: unix.VDISCARD,
}

// The flags of the terminal, by their opcodes in RFC 4254 §8. macOS has no IUCLC, XCASE or
// OLCUC (opcodes 37, 52 and 71), so those are skipped as unknown.
var flags = map[uint8]flag{
	30: {'i', unix.IGNPAR}, 31: {'i', unix.PARMRK}, 32: {'i', unix.INPCK}, 33: {'i', unix.ISTRIP}, 34: {'i', unix.INLCR},
	35: {'i', unix.IGNCR}, 36: {'i', unix.ICRNL}, 38: {'i', unix.IXON}, 39: {'i', unix.IXANY},
	40: {'i', unix.IXOFF}, 41: {'i', unix.IMAXBEL}, 42: {'i', unix.IUTF8},
	50: {'l', unix.ISIG}, 51: {'l', unix.ICANON}, 53: {'l', unix.ECHO}, 54: {'l', unix.ECHOE},
	55: {'l', unix.ECHOK}, 56: {'l', unix.ECHONL}, 57: {'l', unix.NOFLSH}, 58: {'l', unix.TOSTOP}, 59: {'l', unix.IEXTEN},
	60: {'l', unix.ECHOCTL}, 61: {'l', unix.ECHOKE}, 62: {'l', unix.PENDIN},
	70: {'o', unix.OPOST}, 72: {'o', unix.ONLCR}, 73: {'o', unix.OCRNL}, 74: {'o', unix.ONOCR},
	75: {'o', unix.ONLRET},
	92: {'c', unix.PARENB}, 93: {'c', unix.PARODD},
}
