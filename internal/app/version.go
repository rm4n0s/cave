// Package app is the application behind both of Cave's user interfaces: it owns the stores and
// the SSH server, and gives the interfaces snapshots, commands and change events (docs/SPEC.md §10).
package app

import (
	"regexp"
	"runtime/debug"
)

// devVersion is the version being developed. It is what Version returns for a build that is not
// made from a release tag. Bump it when a version is published (CLAUDE.md "Git and GitHub").
const devVersion = "0.3.0-dev"

// releaseVersion matches the module version of a build made from a release tag, and nothing else:
// not "(devel)", not a pseudo-version, not a tag with "+dirty".
var releaseVersion = regexp.MustCompile(`^v(\d+\.\d+\.\d+)$`)

// Version returns Cave's version: the release tag the program was built from, without its "v",
// or the development version when it was built from anything else (docs/SPEC.md §16).
func Version() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return devVersion
	}
	return versionFrom(info.Main.Version)
}

// versionFrom is Version for a given module version.
func versionFrom(moduleVersion string) string {
	if m := releaseVersion.FindStringSubmatch(moduleVersion); m != nil {
		return m[1]
	}
	return devVersion
}
