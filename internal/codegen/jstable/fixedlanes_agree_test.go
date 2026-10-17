package jstable

import (
	"fmt"
	"os"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// ONE PLAN-ENTRY WIDTH, AND EVERY TEST THAT COUNTS LANES SAYS THE SAME ONE.
//
// A plan entry is TableFixedLanes int32 lanes of one flat Int32Array
// (fixedruntime.go; docs/FIXED-FORM-ALGORITHM.md §4.4: the run walks the plan
// entry by entry, so the stride IS the entry). The width is written down in
// several places that do not read each other: the emitted runtime's constant,
// its comment, and the lane counts fixedform_test.go, fixedroadmap_t02_test.go
// and fixedroadmap_t08_test.go assume when they slice an emitted plan. When the
// runtime grows a lane and one of those does not follow, a test either reads
// the wrong lane and passes by accident or fails far from the cause. This test
// reads the width from the EMITTED runtime, checks the emitted identity plan
// really is that wide, and holds every other statement of it to that number.
// laneDisagreements is the comparison, separate so the negative control below
// can hand it a runtime that disagrees and watch it answer.

// laneSite is one place a lane count is written down outside the runtime.
type laneSite struct {
	file string         // the test file, read from this package's directory
	what string         // what the site is, for the failure line
	re   *regexp.Regexp // its one capture group is the count, digits or a word
}

var laneSites = []laneSite{
	{"fixedform_test.go", "the pinned export", regexp.MustCompile(`export const TableFixedLanes = (\d+);`)},
	{"fixedform_test.go", "the pin's failure line", regexp.MustCompile(`a plan entry is (\w+) int32 lanes`)},
	{"fixedroadmap_t02_test.go", "W14's identity plan width", regexp.MustCompile(`the identity plan has %d lanes, want (\w+):`)},
	{"fixedroadmap_t08_test.go", "W6's comment on the guard lane", regexp.MustCompile(`TableFixedLanes \((\d+)\) int32 lanes`)},
	{"fixedroadmap_t08_test.go", "P1's comment on the emitted plan", regexp.MustCompile(`(\w+) int32 lanes \(TableFixedLanes\)`)},
	{"fixedroadmap_t08_test.go", "P1's length check", regexp.MustCompile(`if len\(lanes\) != (\d+) \{`)},
	{"fixedroadmap_t08_test.go", "P1's failure line", regexp.MustCompile(`the identity plan has %d lanes, want (\d+):`)},
	{"fixedroadmap_t08_test.go", "P1's lane slice", regexp.MustCompile(`nums := make\(\[\]int, (\d+)\)`)},
}

var laneWords = map[string]int{
	"seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11, "twelve": 12, "thirteen": 13,
}

func laneCount(s string) (int, bool) {
	if n, err := strconv.Atoi(s); err == nil {
		return n, true
	}
	n, ok := laneWords[strings.ToLower(s)]
	return n, ok
}

// laneDisagreements answers one line per statement of the width that is not
// `width`, and one per site that states nothing at all: a site whose pattern no
// longer matches has stopped being checked, which is a disagreement too.
func laneDisagreements(width int, files map[string]string, sites []laneSite) []string {
	var out []string
	for _, s := range sites {
		ms := s.re.FindAllStringSubmatch(files[s.file], -1)
		if len(ms) == 0 {
			out = append(out, fmt.Sprintf("%s: %s states no lane count (pattern %q matches nothing)", s.file, s.what, s.re))
			continue
		}
		for _, m := range ms {
			n, ok := laneCount(m[1])
			if !ok {
				out = append(out, fmt.Sprintf("%s: %s says %q, which is not a count", s.file, s.what, m[1]))
				continue
			}
			if n != width {
				out = append(out, fmt.Sprintf("%s: %s says %d lanes, the emitted runtime says %d", s.file, s.what, n, width))
			}
		}
	}
	return out
}

var (
	runtimeLanesRe   = regexp.MustCompile(`export const TableFixedLanes = (\d+);`)
	runtimeCommentRe = regexp.MustCompile(`A PLAN ENTRY is (\w+) int32 lanes`)
)

func TestJSFixedLanesAgree(t *testing.T) {
	t.Parallel()

	// THE WIDTH, FROM THE EMITTED RUNTIME and not from fixedruntime.go's text:
	// what the leg ships is what the tests must agree with.
	_, src := t02Table(t, t02Flat)
	m := runtimeLanesRe.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("the emitted runtime exports no TableFixedLanes")
	}
	width, _ := strconv.Atoi(m[1])
	if width < 1 {
		t.Fatalf("the emitted TableFixedLanes is %d", width)
	}

	// The runtime's own comment says the same width.
	if c := runtimeCommentRe.FindStringSubmatch(src); c == nil {
		t.Error("the emitted runtime no longer says how many lanes a plan entry is")
	} else if n, ok := laneCount(c[1]); !ok || n != width {
		t.Errorf("the emitted runtime's comment says %q lanes, its TableFixedLanes says %d", c[1], width)
	}

	// THE STRIDE IS REAL: the flat probe's identity plan is ONE entry (§5.2:
	// a flat type coalesces to one COPY), so the emitted array holds exactly
	// one entry's lanes. A constant that drifted from what the emitter lays
	// down goes red here even if every test agreed with the constant.
	lanes := strings.Split(t02Block(t, src, "const TFixedIdentity = new Int32Array([", "]);"), ",")
	if len(lanes) != width {
		t.Errorf("the emitted one-entry identity plan has %d lanes, TableFixedLanes says %d", len(lanes), width)
	}

	files := map[string]string{}
	for _, s := range laneSites {
		if _, ok := files[s.file]; ok {
			continue
		}
		b, err := os.ReadFile(s.file)
		if err != nil {
			t.Fatalf("read %s: %v", s.file, err)
		}
		files[s.file] = string(b)
	}
	for _, d := range laneDisagreements(width, files, laneSites) {
		t.Error(d)
	}

	// THE NEGATIVE CONTROL: the same sites against a runtime that grew two
	// lanes. Every site must answer, or the comparison above could pass by
	// matching nothing.
	if got := laneDisagreements(width+2, files, laneSites); len(got) < len(laneSites) {
		t.Errorf("NEGATIVE CONTROL: a runtime of %d lanes against tests of %d drew %d disagreements, want at least %d (one per site): %q",
			width+2, width, len(got), len(laneSites), got)
	}
	// and one site alone drifting is caught by name
	drift := map[string]string{}
	for k, v := range files {
		drift[k] = v
	}
	drift["fixedroadmap_t08_test.go"] = strings.Replace(drift["fixedroadmap_t08_test.go"],
		fmt.Sprintf("nums := make([]int, %d)", width), fmt.Sprintf("nums := make([]int, %d)", width+2), 1)
	if got := laneDisagreements(width, drift, laneSites); len(got) != 1 || !strings.Contains(got[0], "P1's lane slice") {
		t.Errorf("NEGATIVE CONTROL: one drifted site drew %q, want exactly P1's lane slice", got)
	}
}
