package modulemove

import (
	"fmt"
	"strings"
	"testing"
	"time"
)

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
		// the tags cut with /v2 stay refused under a boundary just above them
		{"v3.0.5", "v3.0.6", true},
		{"v3.0.6", "v3.0.6", false},
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

// TestFirstV3ModuleTagParses: the boundary, once set, is a release tag of
// major 3, with no pre-release or build suffix.
func TestFirstV3ModuleTagParses(t *testing.T) {
	t.Parallel()
	if FirstV3ModuleTag == "" {
		t.Skip("FirstV3ModuleTag is unset: go.mod has not moved to /v3, so there is no boundary tag to parse")
	}
	if Major(FirstV3ModuleTag) != "3" || strings.ContainsAny(FirstV3ModuleTag, "-+") || strings.Count(FirstV3ModuleTag, ".") != 2 {
		t.Errorf("FirstV3ModuleTag = %q; want a v3.Y.Z release tag", FirstV3ModuleTag)
	}
}

// The spellings the reviewer's CLI comparison found, shared by the tests
// below: each is one Go's golang.org/x/mod refuses as a version.
var badVersions = []string{
	"v3.0.1-rc..0.20261121185943-f397f5f7765d", // an empty pre-release identifier
	"v2.5.1-beta.01",                        // a numeric pre-release identifier with a leading zero
	"v02.5.1-0.20261121185943-f397f5f7765d", // a pseudo-version with a leading-zero major
	"v02.0.0-20261121185943-f397f5f7765d",
	"v2.05.1", "v2.5.01",
	"v2.5.1-", "v2.5.1-.rc", "v2.5.1-rc.", "v2.5.1-rc_1", "v2.5.1-00",
	"v2.5.1+", "v2.5.1+a..b", "v2.5.1-rc.1+",
	"v2.5", "v2", "v", "2.5.1", "V2.5.1", "v2.5.1.0", "", "main",
	" v2.5.1", "v2.5.1\n",
}

// TestValidVersion: the grammar is Go's (semver.IsValid on a version already
// in canonical form): no leading zeros in a number, no empty identifier.
func TestValidVersion(t *testing.T) {
	t.Parallel()
	for _, v := range badVersions {
		if ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = true; Go refuses it", v)
		}
		if IsPseudoVersion(v) {
			t.Errorf("IsPseudoVersion(%q) = true; it is not a version at all", v)
		}
	}
	for _, v := range []string{
		"v2.5.1-beta.1",
		"v2.5.1-0.20261121185943-f397f5f7765d", // the real /v2 pseudo-version of main
		"v0.0.0", "v1.16.0", "v2.5.1", "v3.0.4", "v10.20.30",
		"v2.5.1-0", "v2.5.1-rc.1", "v2.5.1-0a", "v2.5.1-a-b.0-1", "v2.5.1--",
		"v2.5.1+incompatible", "v2.5.1+build.007", "v2.5.1-rc.1+build",
		"v9223372036854775808.0.0", // past int64: still a version
		"v3.0.0-20261121000000-abcdef123456",
		"v3.1.0-rc.1.0.20261121000000-abcdef123456",
	} {
		if !ValidVersion(v) {
			t.Errorf("ValidVersion(%q) = false; Go accepts it", v)
		}
	}
	for v, want := range map[string]bool{
		"v2.5.1-0.20261121185943-f397f5f7765d":      true,
		"v3.0.0-20261121000000-abcdef123456":        true,
		"v3.1.0-rc.1.0.20261121000000-abcdef123456": true,
		"v2.5.1-beta.1":                      false,
		"v2.5.1":                             false,
		"v2.5.1-20261121185943-f397f5f7765d": false, // the vX.0.0 form on a later patch
	} {
		if got := IsPseudoVersion(v); got != want {
			t.Errorf("IsPseudoVersion(%q) = %v; want %v", v, got, want)
		}
	}
}

// TestPathForKeepsTheMajorAsWritten: the path's major is the version's own
// digits, never a number that can overflow into the unversioned path.
func TestPathForKeepsTheMajorAsWritten(t *testing.T) {
	t.Parallel()
	for v, want := range map[string]string{
		"v0.9.0":                               moduleBase,
		"v1.16.0":                              moduleBase,
		"v2.5.1":                               moduleBase + "/v2",
		"v3.0.4":                               moduleBase + "/v3",
		"v2.5.1-0.20261121185943-f397f5f7765d": moduleBase + "/v2",
		"v9223372036854775808.0.0":             moduleBase + "/v9223372036854775808",
		"v340282366920938463463374607431768211456.1.2": moduleBase + "/v340282366920938463463374607431768211456",
	} {
		if got := PathFor(v); got != want {
			t.Errorf("PathFor(%s) = %s; want %s", v, got, want)
		}
	}
	// and a major past int64 is not a v3 tag, nor any tag below the boundary
	if PreModuleMove("v9223372036854775808.0.0", "") {
		t.Error("a major past int64 counts as a v3 tag below the boundary")
	}
}

// TestCheckPseudoVersion: a version in the shape of a pseudo-version must be
// one Go resolves (cmd/go refuses the rest as malformed, the reviewer's runs
// of `go list -m` against a real revision): its timestamp a real UTC time,
// its base one of the three legal forms, its revision twelve lower-case hex
// digits. A version that is not in that shape is a tag, and passes.
func TestCheckPseudoVersion(t *testing.T) {
	t.Parallel()
	for _, v := range []string{
		// the three forms
		"v2.0.0-20261121185943-f397f5f7765d",        // vX.0.0-date-hash: no base
		"v2.5.1-0.20261121185943-f397f5f7765d",      // vX.Y.(Z+1)-0.date-hash: base v2.5.0 (the real /v2 pseudo-version of main)
		"v2.5.0-rc.1.0.20261121185943-f397f5f7765d", // vX.Y.Z-pre.0.date-hash: base v2.5.0-rc.1
		"v2.0.0-rc.1.0.20261121185943-f397f5f7765d", // the pre form may sit on patch zero
		"v2.0.0-0.0.20261121185943-f397f5f7765d",    // base v2.0.0-0
		"v2.5.0-a-b.0.20261121185943-f397f5f7765d",
		"v0.0.0-20240229235959-0123456789ab", // 29 February in a leap year, the last second of a day
		"v2.0.1-0.20000229000000-0123456789ab",
		"v2.5.1-0.00010101000000-0123456789ab", // year 1
		// tags, not in the shape at all
		"v2.5.1", "v2.5.1-beta.1", "v2.5.1-20261999185943-f397f5f7765d", "v2.5.1-0.2026-f397f5f7765d",
	} {
		if err := CheckPseudoVersion(v); err != nil {
			t.Errorf("CheckPseudoVersion(%s) = %v; Go resolves it", v, err)
		}
	}
	for v, why := range map[string]string{
		"v2.5.1-0.20261999185943-f397f5f7765d":      "month",  // F-161
		"v2.5.1-0.20260019185943-f397f5f7765d":      "month",  // month 00
		"v2.5.1-0.20261100185943-f397f5f7765d":      "day",    // day 00
		"v2.5.1-0.20261131185943-f397f5f7765d":      "day",    // 31 November
		"v2.5.1-0.20260229185943-f397f5f7765d":      "day",    // 29 February, 2026 is no leap year
		"v2.5.1-0.21000229185943-f397f5f7765d":      "day",    // nor is 2100
		"v2.5.1-0.20261121245943-f397f5f7765d":      "hour",   // 24
		"v2.5.1-0.20261121186043-f397f5f7765d":      "minute", // 60
		"v2.5.1-0.20261121185960-f397f5f7765d":      "second", // 60: Go takes no leap second
		"v2.0.0-20261321185943-f397f5f7765d":        "month",
		"v2.5.0-rc.1.0.20261121185999-f397f5f7765d": "second",
		"v2.0.0-0.20261121185943-f397f5f7765d":      "patch", // F-162
		"v3.0.0-0.20261121185943-f397f5f7765d":      "patch",
		"v3.4.0-0.20261121185943-f397f5f7765d":      "patch",
		"v2.5.1-0.20261121185943-f397f5f7765":       "revision",
		"v2.5.1-0.20261121185943-f397f5f7765d0":     "revision",
		"v2.5.1-0.20261121185943-F397F5F7765D":      "revision",
		"v2.5.1-0.20261121185943-g397f5f7765d":      "revision",
		"v2.0.0-20261121185943-f397f5f7765dabcdef":  "revision",
	} {
		err := CheckPseudoVersion(v)
		if err == nil {
			t.Errorf("CheckPseudoVersion(%s) = nil; Go refuses it (%s)", v, why)
			continue
		}
		if !strings.Contains(err.Error(), why) {
			t.Errorf("CheckPseudoVersion(%s) = %v; want it to name the %s", v, err, why)
		}
	}
}

// TestPseudoTimeAgreesWithTheCalendar sweeps the timestamp field by field
// against Go's own reading of the same layout (time.Parse is the parser
// cmd/go uses for it; it reads no clock): every month and day of a leap
// year, a common year and the two century cases, and every value of each
// time field.
func TestPseudoTimeAgreesWithTheCalendar(t *testing.T) {
	t.Parallel()
	check := func(stamp string) {
		_, perr := time.Parse("20060102150405", stamp)
		err := CheckPseudoVersion("v2.5.1-0." + stamp + "-0123456789ab")
		if (perr == nil) != (err == nil) {
			t.Errorf("timestamp %s: CheckPseudoVersion = %v; Go's parse = %v", stamp, err, perr)
		}
	}
	for _, year := range []int{1, 1900, 2000, 2024, 2026, 2100, 9999} {
		for month := range 100 {
			for day := range 100 {
				check(fmt.Sprintf("%04d%02d%02d120000", year, month, day))
			}
		}
	}
	for n := range 100 {
		check(fmt.Sprintf("20261121%02d0000", n))
		check(fmt.Sprintf("2026112100%02d00", n))
		check(fmt.Sprintf("202611210000%02d", n))
	}
	check("00000101000000")
}
