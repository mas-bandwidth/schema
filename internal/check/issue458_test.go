package check

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/version"
)

// A refusal that cites a spec section carries the build's version as its
// last line (#458): the section is cited as it stands in the build that
// printed the refusal, so a message pasted from a stale binary dates itself
// instead of contradicting the page as it reads today. The cite stays where
// it was, at the end of the message's own line, so a test matching the
// refusal's text still matches. A refusal that cites nothing carries no
// trailer.
func TestIssue458RefusalCarriesTheBuildVersion(t *testing.T) {
	trailer := "\nschema " + version.Version()
	for _, tc := range []struct{ src, cite string }{
		{"package p\ntype A { again A }\n", "(SPEC §4.6)"},
		{"package p\ntype T { a []uint8 }\n", "(docs/SPEC-TABLES.md §2.9, §11)"},
	} {
		errs := runUnit(t, map[string]string{"T.schema": tc.src})
		if len(errs) != 1 {
			t.Fatalf("%q: got %v, want one refusal", tc.src, errs)
		}
		got := errs[0].Error()
		if !strings.HasSuffix(got, tc.cite+trailer) {
			t.Fatalf("%q: got %q, want the cite %q then the trailer %q", tc.src, got, tc.cite, trailer)
		}
		if strings.Count(got, "\n") != 1 {
			t.Fatalf("%q: got %q, want the trailer as the one added line", tc.src, got)
		}
	}
	errs := runUnit(t, map[string]string{"T.schema": "package p\ntype T { a uint8 = 300 }\n"})
	if len(errs) != 1 || strings.Contains(errs[0].Error(), "\n") {
		t.Fatalf("got %v, want one refusal on one line, citing no section and carrying no trailer", errs)
	}
}
