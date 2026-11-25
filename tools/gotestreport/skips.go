package main

// A SKIP IS A FAILURE. No test, helper or adapter below the root decides not
// to run its body: a test that is not to run is left out at the root (the one
// driver's query, tools/schema-test), and a test that runs and skips is red.
// So every skip event in the records is named and the row is red for it,
// whatever the test logged as its reason, and a skip on an earlier run of a
// test run twice (-count=2) is red though a later run passed.

import (
	"fmt"
	"sort"
	"strings"
)

// skipped is one skip a test reported.
type skipped struct {
	k key
}

// maxSkipLines is how many skips the report names before it gives the count
// of the rest: a lane that lost its toolchain skips hundreds, and the first
// lines say which.
const maxSkipLines = 40

// skipFindings names every skip in the records; any skip is red.
func (r *report) skipFindings(multi map[string]bool) findings {
	if len(r.skips) == 0 {
		return findings{}
	}
	all := append([]skipped(nil), r.skips...)
	sort.SliceStable(all, func(i, j int) bool {
		a, b := all[i].k, all[j].k
		if a.pkg != b.pkg {
			return a.pkg < b.pkg
		}
		if a.test != b.test {
			return a.test < b.test
		}
		return a.rec < b.rec
	})
	var b strings.Builder
	for i, sk := range all {
		if i == maxSkipLines {
			fmt.Fprintf(&b, "  SKIP            and %d more\n", len(all)-maxSkipLines)
			break
		}
		fmt.Fprintf(&b, "  SKIP            %s: a test skipped, and a skip is a failure (leave a test out at the root, tools/schema-test, never inside it)\n", r.name(sk.k, multi))
	}
	fmt.Fprintf(&b, "skips: RED, %d skip(s); no test may skip\n", len(all))
	return findings{text: b.String(), red: true}
}
