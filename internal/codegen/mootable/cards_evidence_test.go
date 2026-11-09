package mootable_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The moo cards leg is proven by make targets that run on a Linux bench, so
// the evidence overlay is the record of that proof. This test reads the
// overlay row port/moo/vt/cards and holds it to the contract: all nine card
// surfaces and the three negative controls are named by make targets that
// exist in make/moo.mk, and the row carries the revision it was tested at.

var mooCardsSurfaces = []string{"load", "save", "render", "parse", "bounds", "upgrade", "edit", "taskhash", "view"}

var mooCardsTargets = []string{
	"cards-moo-conformance",
	"cards-moo-conformance-negative-control",
	"cards-moo-conformance-upgrade-negative-control",
	"cards-moo-conformance-hash-negative-control",
}

func TestMooCardsRowNamesEverySurface(t *testing.T) {
	t.Parallel()
	root := filepath.Join("..", "..", "..")
	evidence, err := os.ReadFile(filepath.Join(root, "docs", "roadmap-evidence", "moo.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	mk, err := os.ReadFile(filepath.Join(root, "make", "moo.mk"))
	if err != nil {
		t.Fatal(err)
	}

	var row string
	for line := range strings.SplitSeq(string(evidence), "\n") {
		if strings.HasPrefix(line, `(:id "port/moo/vt/cards" `) {
			row = line
			break
		}
	}
	if row == "" {
		t.Fatal(`docs/roadmap-evidence/moo.sexp has no overlay row (:id "port/moo/vt/cards" ...)`)
	}

	if !regexp.MustCompile(`:tested-revision "[0-9a-f]{40}"`).MatchString(row) {
		t.Error("the port/moo/vt/cards row carries no :tested-revision of 40 hex digits")
	}

	// The make targets must be defined in make/moo.mk, as a rule.
	for _, target := range mooCardsTargets {
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(target) + `:`).Match(mk) {
			t.Errorf("make/moo.mk defines no rule for %s", target)
		}
		if !strings.Contains(row, "make "+target) && !strings.Contains(row, target+" ") && !strings.Contains(row, target+")") && !strings.Contains(row, target+",") {
			t.Errorf("the port/moo/vt/cards row does not name the make target %s", target)
		}
	}
	if !regexp.MustCompile(`(?m)^cards-moo-test:`).Match(mk) {
		t.Error("make/moo.mk defines no rule for cards-moo-test")
	}
	if !strings.Contains(row, "cards-moo-test") {
		t.Error("the port/moo/vt/cards row does not name the make target cards-moo-test")
	}

	// Each surface is named with its case count, "<surface> <n>", as the
	// other legs' rows do, so a bare word elsewhere in the note does not count.
	for _, s := range mooCardsSurfaces {
		if !regexp.MustCompile(`\b` + s + ` [0-9]+\b`).MatchString(row) {
			t.Errorf("the port/moo/vt/cards row does not name the surface %q with its case count", s)
		}
	}
	if !strings.Contains(string(mk), "CARDS_MOO_REQUIRE := "+strings.Join(mooCardsSurfaces, ",")) {
		t.Error("make/moo.mk CARDS_MOO_REQUIRE does not require all nine surfaces in order")
	}
}
