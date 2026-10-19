package check_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// An enum has no Count (SPEC §4.2, the 2026-09-13 ruling): its declared
// variant count is its Max, and E.Count is not an expression. Count is an
// ordinary variant and arm name on an enum and a union alike.
func TestEnumHasNoCount(t *testing.T) {
	t.Parallel()
	compile := func(source string) []error {
		t.Helper()
		f, perrs := parser.Parse("Probe.schema", []byte(source))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		_, cerrs := check.Unit([]check.SourceFile{{
			Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(source), AST: f,
		}})
		return cerrs
	}

	errs := compile("package probe\n\nenum E { A, B }\n\nconst N = E.Count\n")
	if len(errs) == 0 {
		t.Fatal("E.Count on an enum compiled — an enum has no Count (SPEC §4.2)")
	}
	found := false
	for _, err := range errs {
		text := err.Error()
		if strings.Contains(text, "E") && strings.Contains(text, "has no Count") {
			found = true
		}
	}
	if !found {
		t.Fatalf("no diagnostic names enum E and says an enum has no Count; got: %v", errs)
	}

	if errs := compile("package probe\n\nenum F { Count, Other }\n"); len(errs) > 0 {
		t.Errorf("enum F { Count, Other } was refused: %v", errs)
	}

	if errs := compile("package probe\n\ntable Box { x uint8 }\nunion U { Count int32 | min = 0, max = 100 }\ntable Root { u U }\n"); len(errs) > 0 {
		t.Errorf("union U { Count int32 } was refused: %v", errs)
	}
}
