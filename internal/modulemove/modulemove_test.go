package modulemove

import (
	"errors"
	"strings"
	"testing"
)

// The module lines are assembled from parts so the module-path move program
// leaves these fixtures alone: they name a major on purpose.
func goModAt(suffix string) string {
	return "module " + moduleBase + suffix + "\n\ngo 1.26\n\nrequire pgregory.net/rapid v1.3.0\n"
}

// TestPreModuleMove: with no boundary every v3 tag is below it, v3.0.3 and
// v3.0.4 included; once a boundary is set, tags from it on are not; a
// pseudo-version and every other major never are.
func TestPreModuleMove(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		version, boundary string
		want              bool
	}{
		{"v3.0.0", "", true},
		{"v3.0.3", "", true},
		{"v3.0.4", "", true},
		{"v3.9.9", "", true},
		{"v3.1.0-rc.1", "", true},
		{"v3.0.4", "v3.1.0", true},
		{"v3.1.0-rc.1", "v3.1.0", true},
		{"v3.1.0", "v3.1.0", false},
		{"v3.1.1", "v3.1.0", false},
		{"v3.10.0", "v3.9.0", false},
		{"v2.9.0", "", false},
		{"v1.16.0", "", false},
		{"v4.0.0", "", false},
		// commits are never below: the window after #4445 lands and before
		// the first /v3 tag is cut pins /v3 commits
		{"v3.0.4-0.20261121000000-abcdef123456", "", false},
		{"v3.0.0-20261121000000-abcdef123456", "", false},
		{"v3.1.0-rc.1.0.20261121000000-abcdef123456", "v3.1.0", false},
	} {
		if got := PreModuleMove(c.version, c.boundary); got != c.want {
			t.Errorf("PreModuleMove(%s, %q) = %v; want %v", c.version, c.boundary, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	t.Parallel()
	ordered := []string{"v1.0.0-alpha", "v1.0.0-alpha.1", "v1.0.0-alpha.beta", "v1.0.0-beta", "v1.0.0-beta.2", "v1.0.0-beta.11", "v1.0.0-rc.1", "v1.0.0", "v1.0.1", "v1.2.0", "v1.10.0", "v3.0.3", "v3.1.0", "v10.0.0"}
	for i := range ordered {
		for j := range ordered {
			want := 0
			if i < j {
				want = -1
			} else if i > j {
				want = 1
			}
			if got := Compare(ordered[i], ordered[j]); got != want {
				t.Errorf("Compare(%s, %s) = %d; want %d", ordered[i], ordered[j], got, want)
			}
		}
	}
}

// TestReleaseGate is the consistency check, against go.mod fixtures: a tag's
// go.mod must carry its major, unless pin's boundary covers the tag; and a tag
// whose go.mod does carry it must not be covered.
func TestReleaseGate(t *testing.T) {
	t.Parallel()
	v2, v3 := goModAt("/v2"), goModAt("/v3")
	for _, c := range []struct {
		name, version, goMod, boundary string
		want                           error
	}{
		{"a v2 tag on a /v2 go.mod", "v2.9.0", v2, "", nil},
		{"v3.0.4 on a /v2 go.mod, no boundary: pin refuses it", "v3.0.4", v2, "", nil},
		{"v3.0.4 on a /v2 go.mod, boundary above it", "v3.0.4", v2, "v3.1.0", nil},
		{"the first /v3 tag, at the boundary", "v3.1.0", v3, "v3.1.0", nil},
		{"a later /v3 tag", "v3.2.0", v3, "v3.1.0", nil},
		{"a /v2 go.mod tagged at or past the boundary", "v3.1.0", v2, "v3.1.0", ErrModuleMismatch},
		{"a /v3 go.mod with no boundary set", "v3.1.0", v3, "", ErrBoundaryCovers},
		{"a /v3 go.mod tagged below the boundary", "v3.0.9", v3, "v3.1.0", ErrBoundaryCovers},
		{"a v4 tag on a /v3 go.mod", "v4.0.0", v3, "v3.1.0", ErrModuleMismatch},
		{"a v2 tag on a /v3 go.mod", "v2.10.0", v3, "v3.1.0", ErrModuleMismatch},
		{"a v1 tag on a /v2 go.mod", "v1.17.0", v2, "", ErrModuleMismatch},
		{"no module line", "v2.9.0", "go 1.26\n", "", ErrNoModule},
	} {
		err := ReleaseGate(c.version, c.goMod, c.boundary)
		if !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
			t.Errorf("%s: ReleaseGate(%s, boundary %q) = %v; want %v", c.name, c.version, c.boundary, err, c.want)
		}
		if err != nil && !strings.Contains(err.Error(), c.version) {
			t.Errorf("%s: the refusal does not name the version: %v", c.name, err)
		}
	}
}

// TestFirstV3ModuleTagParses: the boundary, once set, is a release tag of
// major 3, with no pre-release or build suffix.
func TestFirstV3ModuleTagParses(t *testing.T) {
	t.Parallel()
	if FirstV3ModuleTag == "" {
		return
	}
	if Major(FirstV3ModuleTag) != 3 || strings.ContainsAny(FirstV3ModuleTag, "-+") || strings.Count(FirstV3ModuleTag, ".") != 2 {
		t.Errorf("FirstV3ModuleTag = %q; want a v3.Y.Z release tag", FirstV3ModuleTag)
	}
}

// TestTreeGate is the pull-request side of the same check, against go.mod
// fixtures: a /v2 go.mod with a boundary set, or a /v3 go.mod without one, is
// a tree whose next release pin and Go would disagree about.
func TestTreeGate(t *testing.T) {
	t.Parallel()
	v2, v3 := goModAt("/v2"), goModAt("/v3")
	for _, c := range []struct {
		name, goMod, boundary string
		want                  error
	}{
		{"today: /v2, no boundary", v2, "", nil},
		{"after #4445: /v3 with its boundary", v3, "v3.1.0", nil},
		{"#4445 without the boundary", v3, "", ErrBoundaryCovers},
		{"a boundary set on a /v2 tree", v2, "v3.1.0", ErrModuleMismatch},
		{"a /v4 tree", goModAt("/v4"), "v3.1.0", nil},
	} {
		err := TreeGate(c.goMod, c.boundary)
		if !errors.Is(err, c.want) || (c.want == nil) != (err == nil) {
			t.Errorf("%s: TreeGate(boundary %q) = %v; want %v", c.name, c.boundary, err, c.want)
		}
	}
}
