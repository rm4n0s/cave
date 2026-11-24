package app

import "testing"

func TestVersionFrom(t *testing.T) {
	tests := []struct {
		name          string
		moduleVersion string
		want          string
	}{
		{"release tag", "v1.2.3", "1.2.3"},
		{"release tag with big numbers", "v10.20.300", "10.20.300"},
		{"development build", "(devel)", devVersion},
		{"empty", "", devVersion},
		{"pseudo-version", "v0.0.0-20261124101500-2a89f09c1d2e", devVersion},
		{"dirty tree on a tag", "v1.2.3+dirty", devVersion},
		{"pre-release", "v1.2.3-rc1", devVersion},
		{"no v", "1.2.3", devVersion},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := versionFrom(tt.moduleVersion); got != tt.want {
				t.Errorf("versionFrom(%q) = %q, want %q", tt.moduleVersion, got, tt.want)
			}
		})
	}
}

func TestVersionIsNotEmpty(t *testing.T) {
	// A test binary is never built from a release tag of the main module.
	if got := Version(); got != devVersion {
		t.Errorf("Version() = %q, want %q", got, devVersion)
	}
}
