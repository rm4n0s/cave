package sftp

import (
	"path"
	"strings"
)

// isDrive says whether a path segment is a drive: a letter and a colon, such as "C:".
func isDrive(segment string) bool {
	if len(segment) != 2 || segment[1] != ':' {
		return false
	}
	c := segment[0] | 0x20 // lower case
	return c >= 'a' && c <= 'z'
}

// toWindows turns an SFTP path of Windows (docs/SPEC.md §13.4) into one of Windows: "/C:/Users/ada"
// is `C:\Users\ada`. "/" is the root, the list of the drives: isRoot is true and path is "". A
// path is cleaned first: "." and empty parts go, a ".." goes up one part but never above the
// drive, and a ".." at the root stays there. The letter of the drive is made upper case. A
// backslash is taken as a "/", since no name has one on Windows. A path that does not start with
// a drive has no place on Windows: path is "" and isRoot false.
func toWindows(sftpPath string) (winPath string, isRoot bool) {
	var stack []string // the drive, then the folders under it
	for _, segment := range strings.Split(strings.ReplaceAll(sftpPath, `\`, "/"), "/") {
		switch segment {
		case "", ".":
		case "..":
			if len(stack) > 1 {
				stack = stack[:len(stack)-1]
			}
		default:
			if len(stack) == 0 && !isDrive(segment) {
				return "", false
			}
			stack = append(stack, segment)
		}
	}
	if len(stack) == 0 {
		return "", true
	}
	return strings.ToUpper(stack[0]) + `\` + strings.Join(stack[1:], `\`), false
}

// fromWindows turns a path of Windows into an SFTP path: `C:\Users\ada` is "/C:/Users/ada" and
// `C:\` is "/C:/". The letter of the drive is made upper case. A path without a drive (a network
// path, say) only has its backslashes turned into "/".
func fromWindows(winPath string) string {
	p := strings.ReplaceAll(winPath, `\`, "/")
	if len(p) >= 2 && isDrive(p[:2]) {
		p = "/" + strings.ToUpper(p[:1]) + p[1:]
	}
	p = path.Clean(p)
	if len(p) == 3 && isDrive(p[1:]) {
		p += "/" // the root of a drive keeps its slash
	}
	return p
}

// isRootChild says whether an SFTP path names something directly in "/" that is not a drive, such
// as "/new": a name that cannot be made, as the root is the list of the drives and nothing else.
func isRootChild(sftpPath string) bool {
	var names []string
	for _, segment := range strings.Split(strings.ReplaceAll(sftpPath, `\`, "/"), "/") {
		switch segment {
		case "", ".":
		case "..":
			if len(names) > 0 {
				names = names[:len(names)-1]
			}
		default:
			names = append(names, segment)
		}
	}
	return len(names) == 1 && !isDrive(names[0])
}
