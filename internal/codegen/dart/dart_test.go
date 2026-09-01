package dart

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
)

// TestFixedDefaultIsRawNotDoubleScaled pins #168: ir.Field.DefInt for a
// fixed-point default is ALREADY the raw scaled integer (the C++ golden pins
// 2^30 for a Q2.30 default of 1.0), and the storage emitter must render it
// verbatim. The old emitter pushed DefInt through the whole-unit bounds
// scaler and emitted 2^60. Both storage branches are covered: the 64-bit
// int path and the 128-bit pair path.
func TestFixedDefaultIsRawNotDoubleScaled(t *testing.T) {
	src := "package t\n\n" +
		"type P\n{\n" +
		"    w fixed(2, 30)   = 1.0 | min = -1, max = 1\n" +
		"    r fixed(112, 16) = 2.0 | min = -1000, max = 1000\n" +
		"}\n"
	f, perrs := parser.Parse("Fixed.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Fixed.schema", Name: "Fixed.schema", Base: "Fixed",
		Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all strings.Builder
	for _, b := range files {
		all.Write(b)
	}
	out := all.String()

	// Q2.30, 1.0 -> raw 1 << 30
	if !strings.Contains(out, "int w = 1073741824;") {
		t.Errorf("64-bit fixed default: want the raw literal 1073741824 (1.0 in Q2.30), not found")
	}
	if strings.Contains(out, "1152921504606846976") {
		t.Errorf("64-bit fixed default double-scaled: 2^60 appears in the output")
	}
	// Q112.16, 2.0 -> raw 2 << 16 = 131072, rendered through the 128 pair
	if !strings.Contains(out, "131072") {
		t.Errorf("128-bit fixed default: want the raw literal 131072 (2.0 in Q112.16), not found")
	}
	if strings.Contains(out, "8589934592") { // 2 << 32 — the double-scale for F=16
		t.Errorf("128-bit fixed default double-scaled: 2<<32 appears in the output")
	}
}
