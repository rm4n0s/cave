package apptest

// shell is the shell that the sessions of a test run.
const shell = "/bin/sh"

// environ is Cave's own environment, as the real application of a test is given it.
func environ(home string) []string { return []string{"PATH=/usr/bin:/bin", "HOME=" + home} }
