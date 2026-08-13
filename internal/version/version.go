// Package version reports which build of the compiler is running.
//
// The value is stamped at link time by the Makefile (`-X
// .../internal/version.version=$(git describe --tags)`). When that stamp is
// absent — `go run`, `go install`, a bare `go build` — it falls back to the
// module version and VCS stamps the Go toolchain embeds automatically, so a
// binary installed straight from a module proxy still reports something true
// rather than "dev".
//
// Deliberately NOT recorded in generated output. Stamping the compiler version
// into emitted files would mean every release churns every generated file in
// every downstream repository, and would break the goldens on version bumps
// alone — a diff that says nothing about whether the wire changed. Generated
// code carries the protocol id instead, which is the thing that actually
// governs compatibility. See VERSIONING.md.
package version

import (
	"runtime/debug"
	"strings"
)

// version is overwritten at link time; see the Makefile.
var version = ""

// Version returns the compiler version, as close to the truth as the build
// allows: the linker stamp if there was one, otherwise the module version, and
// otherwise a VCS revision. Never empty.
func Version() string {
	if version != "" {
		return version
	}

	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}

	// A module-proxy install carries the tagged version here.
	if v := info.Main.Version; v != "" && v != "(devel)" {
		return v
	}

	// A local build carries VCS stamps instead, when the tree is a git
	// checkout and the toolchain was not told to omit them.
	var revision, modified string
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			revision = s.Value
		case "vcs.modified":
			modified = s.Value
		}
	}
	if revision != "" {
		if len(revision) > 12 {
			revision = revision[:12]
		}
		if modified == "true" {
			return "devel-" + revision + "-dirty"
		}
		return "devel-" + revision
	}

	return "devel"
}

// UserAgent is the one-line form printed by `schema version`, and the form to
// quote in a bug report.
func UserAgent() string {
	v := Version()
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "schema " + v
	}
	goVersion := strings.TrimPrefix(info.GoVersion, "go")
	return "schema " + v + " (go" + goVersion + ")"
}
