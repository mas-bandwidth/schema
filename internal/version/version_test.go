package version

import (
	"strings"
	"testing"
)

// Version must never be empty. A binary that reports "" for its version is
// worse than one that reports "unknown" — it reads as a broken build, and it
// makes bug reports useless.
func TestVersionNeverEmpty(t *testing.T) {
	if got := Version(); got == "" {
		t.Fatal("Version() returned empty string")
	}
}

func TestUserAgentIncludesVersion(t *testing.T) {
	ua := UserAgent()
	if !strings.HasPrefix(ua, "schema ") {
		t.Errorf("UserAgent() = %q, want it to start with %q", ua, "schema ")
	}
	if !strings.Contains(ua, Version()) {
		t.Errorf("UserAgent() = %q, want it to contain the version %q", ua, Version())
	}
}

// The link-time stamp must win over the build-info fallback, or a release
// build would report the module version instead of the tag it was cut from.
func TestLinkerStampWins(t *testing.T) {
	saved := version
	defer func() { version = saved }()

	version = "v9.9.9"
	if got := Version(); got != "v9.9.9" {
		t.Errorf("Version() = %q with a linker stamp set, want %q", got, "v9.9.9")
	}
}
