package sftp

import "testing"

func TestToWindows(t *testing.T) {
	tests := []struct {
		in       string
		want     string
		wantRoot bool
	}{
		{"/", "", true},
		{"", "", true},
		{"//", "", true},
		{"/C:", `C:\`, false},
		{"/C:/", `C:\`, false},
		{"/C:/Users/ada", `C:\Users\ada`, false},
		{"/c:/x", `C:\x`, false},
		{"//C://x", `C:\x`, false},
		{"/C:/a/../b", `C:\b`, false},
		{"/C:/..", `C:\`, false},
		{"/C:/../..", `C:\`, false},
		{"/C:/a/b/../../..", `C:\`, false},
		{"/../..", "", true},
		{"/..", "", true},
		{"/.", "", true},
		{"/C:/./a/./b", `C:\a\b`, false},
		{"/C:/with space/file name.txt", `C:\with space\file name.txt`, false},
		{"/C:/Ελληνικά/ü.txt", `C:\Ελληνικά\ü.txt`, false},
		{"/d:/x/", `D:\x`, false},
		{`/C:\Users\ada`, `C:\Users\ada`, false},
		{"/Users/ada", "", false},
		{"/C/Users", "", false},
		{"/:/x", "", false},
		{"/1:/x", "", false},
		{"/CC:/x", "", false},
		{"/../C:/x", `C:\x`, false},
	}
	for _, tt := range tests {
		got, root := toWindows(tt.in)
		if got != tt.want || root != tt.wantRoot {
			t.Errorf("toWindows(%q) = %q, %v; want %q, %v", tt.in, got, root, tt.want, tt.wantRoot)
		}
	}
}

func TestFromWindows(t *testing.T) {
	tests := []struct{ in, want string }{
		{`C:\Users\ada`, "/C:/Users/ada"},
		{`C:\`, "/C:/"},
		{`C:`, "/C:/"},
		{`c:\x`, "/C:/x"},
		{`C:\Users\ada\`, "/C:/Users/ada"},
		{`C:\a\..\b`, "/C:/b"},
		{`C:\with space\ü.txt`, "/C:/with space/ü.txt"},
		{`C:/already/slashes`, "/C:/already/slashes"},
		{`\\server\share\x`, "/server/share/x"},
	}
	for _, tt := range tests {
		if got := fromWindows(tt.in); got != tt.want {
			t.Errorf("fromWindows(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestThePathsGoBackAndForth(t *testing.T) {
	for _, p := range []string{"/C:/", "/C:/Users", "/D:/a/b c/d", "/Z:/ü"} {
		win, root := toWindows(p)
		if root || fromWindows(win) != p {
			t.Errorf("%q -> %q -> %q", p, win, fromWindows(win))
		}
	}
}

func TestIsRootChild(t *testing.T) {
	tests := []struct {
		in   string
		want bool
	}{
		{"/new", true}, {"/new/", true}, {"/a/../new", true}, {`/new`, true}, {"/./x", true},
		{"/", false}, {"", false}, {"/C:", false}, {"/c:/", false}, {"/C:/x", false}, {"/a/b", false}, {"/..", false}, {"/x/..", false},
	}
	for _, tt := range tests {
		if got := isRootChild(tt.in); got != tt.want {
			t.Errorf("isRootChild(%q) = %v, want %v", tt.in, got, tt.want)
		}
	}
}
