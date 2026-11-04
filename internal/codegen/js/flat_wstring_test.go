package js

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestJsFlatWStringWriteChecksRefuseInEveryBuild verifies that the flat
// wstring writer's contract checks (length range and interior null) are in the
// one writer the module exports, which every build runs (docs/SPEC.md §5.1:
// JavaScript has no assert idiom, so its writer refuses in every build), and
// that no NODE_ENV fork selects a writer without them.
func TestJsFlatWStringWriteChecksRefuseInEveryBuild(t *testing.T) {
	t.Parallel()

	src := `package wstringprobe

type Wide
{
    text wstring(2)
}
`
	ast, perrs := parser.Parse("Wide.schema", []byte(src))
	if len(perrs) != 0 {
		t.Fatalf("parse: %v", perrs)
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Wide.schema", Name: "Wide.schema", Base: "Wide", Bytes: []byte(src), AST: ast,
	}})
	if len(cerrs) != 0 {
		t.Fatalf("check: %v", cerrs)
	}
	out, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	flat, ok := out["WideFlat.js"]
	if !ok {
		t.Fatalf("no flat module: %v", keys(out))
	}
	flatSrc := string(flat)

	// One writer, exported directly: no production variant and no build switch.
	for _, forbidden := range []string{"writeWideFlatProduction", "writeWideFlatChecked", "PRODUCTION", "process.env"} {
		if strings.Contains(flatSrc, forbidden) {
			t.Errorf("the flat module must not carry %q: one writer serves every build", forbidden)
		}
	}
	writeChecked := emittedFlatFunction(t, flatSrc, "WriteWideFlat")

	// The writer MUST contain both checks (via guard, so they appear as if
	// statements with return -1).
	checkedRequired := []string{
		"!Number.isInteger(value.textLength)",
		"v === 0",
	}
	for _, required := range checkedRequired {
		if !strings.Contains(writeChecked, required) {
			t.Errorf("checked writer must contain %q but it does not:\n%s", required, writeChecked)
		}
	}
	// The interior-null refusal sits in the loop body, at the same indent as
	// the unit load. g.guard at the for-statement indent emits a malformed
	// body that a substring check still accepts.
	if got, want := lineIndent(writeChecked, "if (v === 0)"), lineIndent(writeChecked, "v = value.text[wideIndex];"); got != want {
		t.Errorf("interior-null guard indent %q, want loop-body indent %q:\n%s", got, want, writeChecked)
	}

	// Sanity: the writer still has the loop structure
	if !strings.Contains(writeChecked, "for (let wideIndex = 0; wideIndex < value.textLength; wideIndex++)") {
		t.Error("the writer is missing the wstring loop")
	}
}

func emittedFlatFunction(t *testing.T, source, name string) string {
	t.Helper()
	start := strings.Index(source, "function "+name+"(")
	if start < 0 {
		t.Fatalf("missing function %s", name)
	}
	end := strings.Index(source[start:], "\n}\n")
	if end < 0 {
		t.Fatalf("missing end of function %s", name)
	}
	return source[start : start+end+3]
}

func lineIndent(source, needle string) string {
	idx := strings.Index(source, needle)
	if idx < 0 {
		return ""
	}
	lineStart := strings.LastIndex(source[:idx], "\n") + 1
	return source[lineStart:idx]
}
