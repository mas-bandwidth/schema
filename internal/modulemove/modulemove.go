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
	"maps"
	"regexp"
	"slices"
	"strings"
)

// FirstV3ModuleTag is the first v3 release tag whose go.mod names the /v3
// module path, set by the module-path move (#4445, tools/modpath) in the
// same commit as go.mod. Every v3 tag below it (tags v3.0.0 to v3.0.6)
// was cut while go.mod still named /v2, so Go cannot resolve it, and pin
// refuses it. The release gate refuses a tag whose go.mod and this boundary
// disagree.
const FirstV3ModuleTag = "v3.0.7"

// The release gate's refusals, each matched with errors.Is.
var (
	ErrModuleMismatch = errors.New("go.mod's module path does not carry the major of the version being tagged")
	ErrBoundaryCovers = errors.New("pin's boundary refuses a tag whose go.mod Go can resolve")
	ErrBoundaryStale  = errors.New("pin's boundary is not the first v3 tag cut with the /v3 module path")
	ErrNoModule       = errors.New("go.mod has no module line")
)

// moduleBase is assembled from parts so the module-path move program leaves
// it alone: it names the base path under every major.
const moduleBase = "github.com/mas-bandwidth/" + "schema"

// ValidVersion reports whether v is a version Go resolves a module at: the
// grammar of golang.org/x/mod's semver.IsValid, held to the full canonical
// spelling (vMAJOR.MINOR.PATCH, never the v1 or v1.2 shorthand), written out
// here because this module takes no dependency for it. The three numbers are
// digits with no leading zero. A pre-release is dot-separated identifiers of
// [0-9A-Za-z-], none empty, an all-digit one with no leading zero. Build
// metadata is dot-separated identifiers of [0-9A-Za-z-], none empty. Nothing
// here is read as an integer, so a number of any length is a number.
func ValidVersion(v string) bool {
	rest, ok := strings.CutPrefix(v, "v")
	if !ok {
		return false
	}
	rest, build, hasBuild := strings.Cut(rest, "+")
	core, pre, hasPre := strings.Cut(rest, "-")
	nums := strings.Split(core, ".")
	if len(nums) != 3 {
		return false
	}
	for _, n := range nums {
		if !isNumeric(n) || (len(n) > 1 && n[0] == '0') {
			return false
		}
	}
	if hasPre {
		for id := range strings.SplitSeq(pre, ".") {
			if !isIdent(id) || (isNumeric(id) && len(id) > 1 && id[0] == '0') {
				return false
			}
		}
	}
	if hasBuild {
		for id := range strings.SplitSeq(build, ".") {
			if !isIdent(id) {
				return false
			}
		}
	}
	return true
}

// isIdent: a non-empty run of [0-9A-Za-z-].
func isIdent(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if (r < '0' || r > '9') && (r < 'A' || r > 'Z') && (r < 'a' || r > 'z') && r != '-' {
			return false
		}
	}
	return true
}

// pseudo is the shape Go reads as a pseudo-version, golang.org/x/mod/module's
// own pattern: vX.0.0-yyyymmddhhmmss-rev, vX.Y.Z-pre.0.yyyymmddhhmmss-rev and
// vX.Y.(Z+1)-0.yyyymmddhhmmss-rev. It is a shape only: a version in it is a
// commit to Go, never a tag, whether or not it is one Go can resolve
// (CheckPseudoVersion).
var pseudo = regexp.MustCompile(`^v[0-9]+\.(0\.0-|[0-9]+\.[0-9]+-([^+]*\.)?0\.)[0-9]{14}-[A-Za-z0-9]+$`)

// IsPseudoVersion reports whether v is in the shape of a Go pseudo-version:
// a commit, not a tag. A commit is never below the boundary, because whether
// it resolves depends on the go.mod at that commit, which pin cannot see
// offline.
func IsPseudoVersion(v string) bool { return ValidVersion(v) && pseudo.MatchString(v) }

// CheckPseudoVersion refuses a version that is in the shape of a
// pseudo-version and is not one Go resolves, by everything that can be known
// without the repository (cmd/go refuses each as malformed):
//
//   - the timestamp is a real UTC time, yyyymmddhhmmss, each field in range
//     and the day one its month has. It is read as digits, never against a
//     clock.
//   - the base is one of the three legal forms. vX.Y.Z-0.time-rev stands for
//     the commit after release vX.Y.(Z-1), so its patch is not zero: no
//     release precedes vX.Y.0 in that form. (vX.0.0-time-rev has no base, and
//     vX.Y.Z-pre.0.time-rev follows the pre-release vX.Y.Z-pre.)
//   - the revision is twelve lower-case hex digits, git's short hash as Go
//     writes it.
//
// A version not in the shape is a tag, and passes; so does build metadata,
// which is not this check's business.
func CheckPseudoVersion(v string) error {
	v, _, _ = strings.Cut(v, "+")
	if !IsPseudoVersion(v) {
		return nil
	}
	core, pre, _ := strings.Cut(v[1:], "-")
	dash := strings.LastIndexByte(pre, '-')
	rev, lead := pre[dash+1:], pre[:dash]
	stamp, lead := lead[len(lead)-14:], lead[:len(lead)-14]
	if err := checkStamp(stamp); err != nil {
		return fmt.Errorf("the timestamp %s of this pseudo-version is not a time: %w", stamp, err)
	}
	if lead == "0." && strings.HasSuffix(core, ".0") {
		return fmt.Errorf("a pseudo-version of the form vX.Y.Z-0.%s-%s follows the release before patch Z, and patch 0 has none", stamp, rev)
	}
	if len(rev) != 12 || strings.Trim(rev, "0123456789abcdef") != "" {
		return fmt.Errorf("the revision %s of this pseudo-version is not twelve lower-case hex digits", rev)
	}
	return nil
}

// checkStamp holds fourteen digits to a calendar date and a time of day, as
// Go's time.Parse holds the layout 20060102150405: any year, month 01 to 12,
// a day the month has in that year, hour to 23, minute and second to 59 (no
// leap second).
func checkStamp(s string) error {
	num := func(from, to int) int {
		n := 0
		for _, r := range s[from:to] {
			n = n*10 + int(r-'0')
		}
		return n
	}
	year, month, day := num(0, 4), num(4, 6), num(6, 8)
	hour, minute, second := num(8, 10), num(10, 12), num(12, 14)
	if month < 1 || month > 12 {
		return fmt.Errorf("month %s", s[4:6])
	}
	days := [...]int{31, 28, 31, 30, 31, 30, 31, 31, 30, 31, 30, 31}[month-1]
	if month == 2 && year%4 == 0 && (year%100 != 0 || year%400 == 0) {
		days = 29
	}
	switch {
	case day < 1 || day > days:
		return fmt.Errorf("day %s of month %s, year %s", s[6:8], s[4:6], s[0:4])
	case hour > 23:
		return fmt.Errorf("hour %s", s[8:10])
	case minute > 59:
		return fmt.Errorf("minute %s", s[10:12])
	case second > 59:
		return fmt.Errorf("second %s", s[12:14])
	}
	return nil
}

// Major is the major of a version as it is written (v3.0.4 is "3"), or "" for
// a string that is not a version. It stays text: a major is compared and
// written into a module path, never counted, so none is too long.
func Major(v string) string {
	if !ValidVersion(v) {
		return ""
	}
	major, _, _ := strings.Cut(v[1:], ".")
	return major
}

// PreModuleMove reports whether version, a semver tag with no build metadata,
// is a v3 tag below boundary: cut while go.mod still named /v2, so Go cannot
// resolve it. An empty boundary means every v3 tag is. A pseudo-version is a
// commit and never counts as below; neither does any other major.
func PreModuleMove(version, boundary string) bool {
	if Major(version) != "3" || IsPseudoVersion(version) {
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
// base path for v0 and v1, the base path plus /vN for major N from 2 on, N
// as the version writes it.
func PathFor(version string) string {
	switch major := Major(version); major {
	case "", "0", "1":
		return moduleBase
	default:
		return moduleBase + "/v" + major
	}
}

// ReleaseGate checks a release tag against the go.mod at the commit it names
// and pin's boundary. A tag whose go.mod does not carry its major is one Go
// cannot resolve, and is let through only when the boundary covers it (pin
// refuses it, so no adopter is handed it); a tag whose go.mod does carry its
// major is refused when the boundary covers it, because pin would then refuse
// a tag that resolves.
//
// The boundary is held against history as well (BoundaryGate): tagModules is
// the module path each release tag already cut declares in its own go.mod. And
// the first v3 release whose go.mod names /v3 must BE the boundary: a boundary
// set to anything else is stale before the tag is cut.
func ReleaseGate(version, goMod, boundary string, tagModules map[string]string) error {
	have, err := ModulePath(goMod)
	if err != nil {
		return fmt.Errorf("%s: %w", version, err)
	}
	if err := BoundaryGate(boundary, tagModules); err != nil {
		return fmt.Errorf("tagging %s: %w", version, err)
	}
	want := PathFor(version)
	pre := PreModuleMove(version, boundary)
	switch {
	case have != want && !pre:
		return fmt.Errorf("%w: tagging %s needs go.mod to name %s, and it names %s, so Go cannot resolve the tag; pin's boundary (internal/modulemove.FirstV3ModuleTag = %q) does not cover it, so pin would hand it to adopters", ErrModuleMismatch, version, want, have, boundary)
	case have == want && pre:
		return fmt.Errorf("%w: go.mod names %s, so Go resolves %s, but pin's boundary (internal/modulemove.FirstV3ModuleTag = %q) refuses it; set the boundary to the first tag cut with the /v3 module path", ErrBoundaryCovers, have, version, boundary)
	case have == want && Major(version) == "3" && version != boundary && firstV3Module(tagModules):
		return fmt.Errorf("%w: %s would be the first v3 tag cut with a go.mod naming %s, and pin's boundary (internal/modulemove.FirstV3ModuleTag) is %q; set the boundary to %s, or release as %s", ErrBoundaryStale, version, have, boundary, version, boundary)
	}
	return nil
}

// BoundaryGate holds the boundary against the release tags already cut.
// tagModules maps each existing tag to the module path the go.mod AT that tag
// declares (tools/releasetag reads it from git; the rule itself needs none).
// With a boundary set, every v3 tag whose go.mod does not name /v3 must lie
// below it, or pin would hand out a published tag Go cannot resolve; and every
// v3 tag whose go.mod does name /v3 must lie at or above it, or pin would
// refuse a tag that resolves. An unset boundary refuses every v3 tag, so
// history cannot contradict it here (TreeGate refuses it on a /v3 go.mod).
func BoundaryGate(boundary string, tagModules map[string]string) error {
	if boundary == "" {
		return nil
	}
	for _, tag := range slices.Sorted(maps.Keys(tagModules)) {
		if Major(tag) != "3" || IsPseudoVersion(tag) {
			continue
		}
		have, below := tagModules[tag], Compare(tag, boundary) < 0
		switch {
		case have != PathFor(tag) && !below:
			return fmt.Errorf("%w: %s is already cut with a go.mod naming %s, so Go cannot resolve it, and pin's boundary (internal/modulemove.FirstV3ModuleTag = %q) does not refuse it; the boundary must be greater than every v3 tag cut before the module moved", ErrBoundaryStale, tag, have, boundary)
		case have == PathFor(tag) && below:
			return fmt.Errorf("%w: %s is already cut with a go.mod naming %s, so Go resolves it, and pin's boundary (internal/modulemove.FirstV3ModuleTag = %q) refuses it; the boundary is the FIRST tag cut with the /v3 module path", ErrBoundaryCovers, tag, have, boundary)
		}
	}
	return nil
}

// firstV3Module reports whether no v3 tag already cut declares /v3: the
// release being checked would be the first that Go resolves under /v3.
func firstV3Module(tagModules map[string]string) bool {
	for tag, have := range tagModules {
		if Major(tag) == "3" && !IsPseudoVersion(tag) && have == PathFor(tag) {
			return false
		}
	}
	return true
}

// TreeGate checks a tree's go.mod against the boundary before any version is
// chosen, so a pull request that moves one without the other fails in fast
// CI: a /v2 go.mod with a boundary set would let pin hand out v3 tags cut from
// it, and a /v3 go.mod with none would make pin refuse the first tag that
// resolves. With a boundary set, the release of the boundary tag itself from
// this tree must pass ReleaseGate. It sees no tags (a pull request's checkout
// has none): the boundary is held against the tags already cut at release
// time, by ReleaseGate in tools/releasetag.
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
		return ReleaseGate(boundary, goMod, boundary, nil)
	}
	return nil
}
