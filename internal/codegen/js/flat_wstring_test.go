package js

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestJsFlatWStringWriteChecksAreDebugOnly verifies that the flat wstring
// writer's contract checks (length range and interior null) are emitted only
// in the checked/debug variant and are absent from the production variant.
func TestJsFlatWStringWriteChecksAreDebugOnly(t *testing.T) {
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

	// Extract the two writer variants
	writeProd := emittedFlatFunction(t, flatSrc, "writeWideFlatProduction")
	writeChecked := emittedFlatFunction(t, flatSrc, "writeWideFlatChecked")

	// The production writer must NOT contain the unconditional length check
	// or the interior-null check. Note: the field name is capitalized to TextLength.
	prodForbidden := []string{
		"!Number.isInteger(value.TextLength)",
		"v === 0",
	}
	for _, forbidden := range prodForbidden {
		if strings.Contains(writeProd, forbidden) {
			t.Errorf("production writer must not contain %q but it does:\n%s", forbidden, writeProd)
		}
	}

	// The checked writer MUST contain both checks (via guard, so they appear
	// as if statements with return -1).
	checkedRequired := []string{
		"!Number.isInteger(value.TextLength)",
		"v === 0",
	}
	for _, required := range checkedRequired {
		if !strings.Contains(writeChecked, required) {
			t.Errorf("checked writer must contain %q but it does not:\n%s", required, writeChecked)
		}
	}

	// Sanity: both writers should still have the loop structure
	if !strings.Contains(writeProd, "for (let wideIndex = 0; wideIndex < value.TextLength; wideIndex++)") {
		t.Error("production writer missing the wstring loop")
	}
	if !strings.Contains(writeChecked, "for (let wideIndex = 0; wideIndex < value.TextLength; wideIndex++)") {
		t.Error("checked writer missing the wstring loop")
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
