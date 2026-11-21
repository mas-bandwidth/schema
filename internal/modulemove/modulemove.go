// Package modulemove holds the one boundary between the v3 tags cut while
// go.mod still named the /v2 module path and the v3 tags cut after the module
// moved to /v3 (PR #4445). Go resolves a tag vN.x.y (N >= 2) only under the
// module path ending /vN, and only when that tag's go.mod names it; a v3 tag
// whose go.mod names /v2 resolves under neither path. `schema pin` refuses
// every tag below the boundary, and the release gate (tools/releasetag, with
// internal/ci's go.mod check on every pull request) refuses a tag whose go.mod
// disagrees with its major unless the boundary covers it, so the two cannot
// drift apart.
package modulemove

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// FirstV3ModuleTag is the first v3 release tag whose go.mod names the /v3
// module path. Empty means there is none yet: go.mod on main still names /v2,
// so every v3 tag (v3.0.0 to v3.0.4 are cut so) is one Go
// cannot resolve, and all of them are refused.
//
// #4445 SETS THIS IN THE SAME COMMIT AS THE go.mod MOVE, to the version the
// first release after the move will carry (for example "v3.1.0"). Until then
// the release gate lets a v3 tag go out with a /v2 go.mod, because pin
// refuses it; after it, a tag whose go.mod and boundary disagree is refused.
const FirstV3ModuleTag = ""

// The release gate's refusals, each matched with errors.Is.
var (
	ErrModuleMismatch = errors.New("go.mod's module path does not carry the major of the version being tagged")
	ErrBoundaryCovers = errors.New("pin's boundary refuses a tag whose go.mod Go can resolve")
	ErrNoModule       = errors.New("go.mod has no module line")
)

// moduleBase is assembled from parts so the module-path move program leaves
// it alone: it names the base path under every major.
const moduleBase = "github.com/mas-bandwidth/" + "schema"

// pseudo is Go's pseudo-version grammar (golang.org/x/mod/module), all three
// forms: vX.0.0-yyyymmddhhmmss-hash, vX.Y.(Z+1)-0.yyyymmddhhmmss-hash and
// vX.Y.Z-pre.0.yyyymmddhhmmss-hash.
var pseudo = regexp.MustCompile(`^v[0-9]+\.(0\.0-|[0-9]+\.[0-9]+-([^+]*\.)?0\.)[0-9]{14}-[0-9a-f]{12}$`)

// IsPseudoVersion reports whether v is a Go pseudo-version: a commit, not a
// tag. A commit is never below the boundary, because whether it resolves
// depends on the go.mod at that commit, which pin cannot see offline.
func IsPseudoVersion(v string) bool { return pseudo.MatchString(v) }

// Major is the major number of a semver version (v3.0.4 is 3), or -1.
func Major(v string) int {
	s, ok := strings.CutPrefix(v, "v")
	if !ok {
		return -1
	}
	s, _, _ = strings.Cut(s, ".")
	n, err := strconv.Atoi(s)
	if err != nil {
		return -1
	}
	return n
}

// PreModuleMove reports whether version, a semver tag with no build metadata,
// is a v3 tag below boundary: cut while go.mod still named /v2, so Go cannot
// resolve it. An empty boundary means every v3 tag is. A pseudo-version is a
// commit and never counts as below; neither does any other major.
func PreModuleMove(version, boundary string) bool {
	if Major(version) != 3 || IsPseudoVersion(version) {
		return false
	}
	return boundary == "" || Compare(version, boundary) < 0
}

// Compare orders two semver versions by precedence (semver.org §11): -1, 0
// or 1. Build metadata is ignored. Both are assumed well formed.
func Compare(a, b string) int {
	a, _, _ = strings.Cut(strings.TrimPrefix(a, "v"), "+")
	b, _, _ = strings.Cut(strings.TrimPrefix(b, "v"), "+")
	ac, apre, _ := strings.Cut(a, "-")
	bc, bpre, _ := strings.Cut(b, "-")
	an, bn := strings.Split(ac, "."), strings.Split(bc, ".")
	for i := range 3 {
		if c := compareNumeric(at(an, i), at(bn, i)); c != 0 {
			return c
		}
	}
	switch {
	case apre == bpre:
		return 0
	case apre == "":
		return 1 // a release outranks its pre-releases
	case bpre == "":
		return -1
	}
	ap, bp := strings.Split(apre, "."), strings.Split(bpre, ".")
	for i := 0; i < len(ap) && i < len(bp); i++ {
		if c := compareIdent(ap[i], bp[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(ap) < len(bp):
		return -1
	case len(ap) > len(bp):
		return 1
	}
	return 0
}

func at(s []string, i int) string {
	if i < len(s) {
		return s[i]
	}
	return "0"
}

func isNumeric(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// compareNumeric orders two digit strings with no leading zeros by value,
// at any length.
func compareNumeric(a, b string) int {
	if len(a) != len(b) {
		if len(a) < len(b) {
			return -1
		}
		return 1
	}
	return strings.Compare(a, b)
}

// compareIdent orders two pre-release identifiers: numeric ones by value,
// below every alphanumeric one, and alphanumeric ones in ASCII order.
func compareIdent(a, b string) int {
	an, bn := isNumeric(a), isNumeric(b)
	switch {
	case an && bn:
		return compareNumeric(a, b)
	case an:
		return -1
	case bn:
		return 1
	}
	return strings.Compare(a, b)
}

// ModulePath reads the module path out of go.mod's source.
func ModulePath(goMod string) (string, error) {
	for line := range strings.SplitSeq(goMod, "\n") {
		f := strings.Fields(line)
		if len(f) >= 2 && f[0] == "module" {
			return strings.Trim(f[1], `"`), nil
		}
	}
	return "", ErrNoModule
}

// PathFor is the module path Go requires for a version of this module: the
// base path for v0 and v1, the base path plus /vN for major N from 2 on.
func PathFor(version string) string {
	if m := Major(version); m >= 2 {
		return moduleBase + "/v" + strconv.Itoa(m)
	}
	return moduleBase
}

// ReleaseGate checks a release tag against the go.mod at the commit it names
// and pin's boundary. A tag whose go.mod does not carry its major is one Go
// cannot resolve, and is let through only when the boundary covers it (pin
// refuses it, so no adopter is handed it); a tag whose go.mod does carry its
// major is refused when the boundary covers it, because pin would then refuse
// a tag that resolves.
func ReleaseGate(version, goMod, boundary string) error {
	have, err := ModulePath(goMod)
	if err != nil {
		return fmt.Errorf("%s: %w", version, err)
	}
	want := PathFor(version)
	pre := PreModuleMove(version, boundary)
	switch {
	case have != want && !pre:
		return fmt.Errorf("%w: tagging %s needs go.mod to name %s, and it names %s, so Go cannot resolve the tag; pin's boundary (internal/modulemove.FirstV3ModuleTag = %q) does not cover it, so pin would hand it to adopters", ErrModuleMismatch, version, want, have, boundary)
	case have == want && pre:
		return fmt.Errorf("%w: go.mod names %s, so Go resolves %s, but pin's boundary (internal/modulemove.FirstV3ModuleTag = %q) refuses it; set the boundary to the first tag cut with the /v3 module path", ErrBoundaryCovers, have, version, boundary)
	}
	return nil
}

// TreeGate checks a tree's go.mod against the boundary before any version is
// chosen, so a pull request that moves one without the other fails in fast
// CI: a /v2 go.mod with a boundary set would let pin hand out v3 tags cut from
// it, and a /v3 go.mod with none would make pin refuse the first tag that
// resolves. With a boundary set, the release of the boundary tag itself from
// this tree must pass ReleaseGate.
func TreeGate(goMod, boundary string) error {
	have, err := ModulePath(goMod)
	if err != nil {
		return err
	}
	switch {
	case have == PathFor("v3.0.0") && boundary == "":
		return fmt.Errorf("%w: go.mod names %s but internal/modulemove.FirstV3ModuleTag is unset, so pin refuses every v3 tag; set it to the first tag cut with this go.mod in the same commit", ErrBoundaryCovers, have)
	case boundary != "" && (have == PathFor("v2.0.0") || have == moduleBase):
		return fmt.Errorf("%w: internal/modulemove.FirstV3ModuleTag = %q but go.mod still names %s; unset it until go.mod moves to /v3", ErrModuleMismatch, boundary, have)
	}
	if boundary != "" && have == PathFor(boundary) {
		return ReleaseGate(boundary, goMod, boundary)
	}
	return nil
}
