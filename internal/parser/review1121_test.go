// Parser regressions from the 2026-11-21 review: a flat operator chain is
// bounded by count, as nesting is by depth (FRONTEND-9); and a body brace on
// the qualification line is read the same for every declaration kind
// (FRONTEND-10) — type, table and union always took it, enum and flags now do.
package parser

import (
	"strings"
	"testing"
)

func TestOperatorChainIsBounded(t *testing.T) {
	t.Parallel()
	long := "package p\nconst C = 1" + strings.Repeat(" + 1", maxExprOperators+1) + "\n"
	_, errs := Parse("T.schema", []byte(long))
	if len(errs) != 1 || !strings.Contains(errs[0].Error(), "operators") {
		t.Fatalf("want one operator-bound diagnostic, got %d: %v", len(errs), errs)
	}
	ok := "package p\nconst C = 1" + strings.Repeat(" * 2 + 1", maxExprOperators/2) + "\n"
	if _, errs := Parse("T.schema", []byte(ok)); len(errs) > 0 {
		t.Fatalf("a chain at the bound was refused: %v", errs[0])
	}
	// the bound is per expression: two long expressions in one file both pass
	two := "package p\nconst A = 1" + strings.Repeat(" + 1", maxExprOperators) + "\nconst B = 1" + strings.Repeat(" + 1", maxExprOperators) + "\n"
	if _, errs := Parse("T.schema", []byte(two)); len(errs) > 0 {
		t.Fatalf("the bound leaked across expressions: %v", errs[0])
	}
}

func TestBodyBraceOnTheQualificationLine(t *testing.T) {
	t.Parallel()
	for _, src := range []string{
		"package p\ntype T | tag {\n    a int32\n}\n",
		"package p\ntable T | tag {\n    a int32\n}\n",
		"package p\nenum E | tag {\n    A\n    B\n}\n",
		"package p\nflags F | tag {\n    A, B\n}\n",
	} {
		if _, errs := Parse("T.schema", []byte(src)); len(errs) > 0 {
			t.Errorf("refused:\n%s\n%v", src, errs[0])
		}
	}
}
