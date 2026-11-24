package pty

import (
	"os"
	"path/filepath"
	"strconv"

	"golang.org/x/sys/unix"
)

// The devices of the pseudo terminals.
const (
	ptmxPath = "/dev/ptmx"
	ptsDir   = "/dev/pts"
)

// keepSlave says whether CloseSlave keeps Cave's handle of the slave side. Linux keeps the output of a terminal when its slave side closes, so Cave lets go of its handle at once.
const keepSlave = false

// tcflag is the type of the terminal's flag words.
type tcflag = uint32

// The requests that read and write the terminal's settings, and the value of a control character
// that is turned off (_POSIX_VDISABLE).
const (
	termiosGet    = unix.TCGETS
	termiosSet    = unix.TCSETS
	posixVDisable = 0
)

// unlock finds the number of the master's slave, unlocks it, and returns the slave's path in
// the folder pts. It returns the system's error as it is.
func unlock(master *os.File, pts string) (string, error) {
	number := 0
	err := control(master, func(fd int) error {
		var err error
		if number, err = unix.IoctlGetInt(fd, unix.TIOCGPTN); err != nil {
			return err
		}
		return unix.IoctlSetPointerInt(fd, unix.TIOCSPTLCK, 0)
	})
	if err != nil {
		return "", err
	}
	return filepath.Join(pts, strconv.Itoa(number)), nil
}

// The characters of the terminal's line editing, by their opcodes in RFC 4254 §8.
var controlChars = map[uint8]int{
	1: unix.VINTR, 2: unix.VQUIT, 3: unix.VERASE, 4: unix.VKILL, 5: unix.VEOF, 6: unix.VEOL, 7: unix.VEOL2,
	8: unix.VSTART, 9: unix.VSTOP, 10: unix.VSUSP, 12: unix.VREPRINT, 13: unix.VWERASE, 14: unix.VLNEXT, 18: unix.VDISCARD,
}

// The flags of the terminal, by their opcodes in RFC 4254 §8.
var flags = map[uint8]flag{
	30: {'i', unix.IGNPAR}, 31: {'i', unix.PARMRK}, 32: {'i', unix.INPCK}, 33: {'i', unix.ISTRIP}, 34: {'i', unix.INLCR},
	35: {'i', unix.IGNCR}, 36: {'i', unix.ICRNL}, 37: {'i', unix.IUCLC}, 38: {'i', unix.IXON}, 39: {'i', unix.IXANY},
	40: {'i', unix.IXOFF}, 41: {'i', unix.IMAXBEL}, 42: {'i', unix.IUTF8},
	50: {'l', unix.ISIG}, 51: {'l', unix.ICANON}, 52: {'l', unix.XCASE}, 53: {'l', unix.ECHO}, 54: {'l', unix.ECHOE},
	55: {'l', unix.ECHOK}, 56: {'l', unix.ECHONL}, 57: {'l', unix.NOFLSH}, 58: {'l', unix.TOSTOP}, 59: {'l', unix.IEXTEN},
	60: {'l', unix.ECHOCTL}, 61: {'l', unix.ECHOKE}, 62: {'l', unix.PENDIN},
	70: {'o', unix.OPOST}, 71: {'o', unix.OLCUC}, 72: {'o', unix.ONLCR}, 73: {'o', unix.OCRNL}, 74: {'o', unix.ONOCR},
	75: {'o', unix.ONLRET},
	92: {'c', unix.PARENB}, 93: {'c', unix.PARODD},
}
