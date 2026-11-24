package wine

import "golang.org/x/sys/windows"

// Running reports whether the program runs under Wine: Wine's ntdll.dll has the function
// wine_get_version, which Windows' does not.
func Running() bool {
	dll, err := windows.LoadLibrary("ntdll.dll")
	if err != nil {
		return false
	}
	defer func() { _ = windows.FreeLibrary(dll) }()
	_, err = windows.GetProcAddress(dll, "wine_get_version")
	return err == nil
}
