package check

import (
	"strings"
	"testing"
)

// A refusal that cites a spec section keeps its cite on the message's own line
// and carries no version trailer (#458): the build's version line moved up to
// the batch, printed once after the error count by the CLI (and carried once
// at the end of compiler.Diagnostics.Error()). A check error is one line,
// whoever it cites. A refusal that cites nothing carries nothing either.
func TestIssue458RefusalCarriesTheBuildVersion(t *testing.T) {
	for _, tc := range []struct{ src, cite string }{
		{"package p\ntype A { again A }\n", "(SPEC §4.6)"},
		{"package p\ntype T { a []uint8 }\n", "(docs/SPEC-TABLES.md §2.9, §11)"},
	} {
		errs := runUnit(t, map[string]string{"T.schema": tc.src})
		if len(errs) != 1 {
			t.Fatalf("%q: got %v, want one refusal", tc.src, errs)
		}
		got := errs[0].Error()
		if !strings.HasSuffix(got, tc.cite) {
			t.Fatalf("%q: got %q, want the refusal to end at its cite %q with no version trailer", tc.src, got, tc.cite)
		}
		if strings.Contains(got, "\n") {
			t.Fatalf("%q: got %q, want a single-line refusal carrying no trailer", tc.src, got)
		}
	}
	errs := runUnit(t, map[string]string{"T.schema": "package p\ntype T { a uint8 = 300 }\n"})
	if len(errs) != 1 || strings.Contains(errs[0].Error(), "\n") {
		t.Fatalf("got %v, want one refusal on one line, citing no section", errs)
	}
}
