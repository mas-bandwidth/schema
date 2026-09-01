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

// TestReservedDeclarationNamesRefused pins #162: the shadow-hazard entries
// (String, List, Object, Endian, ByteData) exist for DECLARATION names, which
// emit verbatim as Dart class names — a `type List` would shadow dart:core.
// The old checkNames walked consts, variants and fields but never the
// declarations themselves, leaving those entries unreachable.
func TestReservedDeclarationNamesRefused(t *testing.T) {
	cases := []struct {
		src    string
		needle string
	}{
		{"package t\n\ntype List { x uint8 }\n", "List"},
		{"package t\n\nenum Endian { Little, Big }\n\ntype P { e Endian }\n", "Endian"},
		{"package t\n\nflags Object { A }\n\ntype P { o Object }\n", "Object"},
		{"package t\n\ntype Blob { x uint8 }\n\nunion String { blob Blob }\n", "String"},
		{"package t\n\ntype ByteData { x uint8 }\n", "ByteData"},
	}
	for _, c := range cases {
		f, perrs := parser.Parse("Res.schema", []byte(c.src))
		if len(perrs) > 0 {
			t.Fatalf("parse: %v", perrs[0])
		}
		u, cerrs := check.Unit([]check.SourceFile{{
			Path: "Res.schema", Name: "Res.schema", Base: "Res",
			Bytes: []byte(c.src), AST: f,
		}})
		if len(cerrs) > 0 {
			t.Fatalf("check: %v", cerrs[0])
		}
		_, err := Generate(u)
		if err == nil {
			t.Errorf("declaration %q: want a reserved-identifier refusal, got success", c.needle)
			continue
		}
		if !strings.Contains(err.Error(), c.needle) || !strings.Contains(err.Error(), "rename it") {
			t.Errorf("declaration %q: refusal does not name the identifier: %v", c.needle, err)
		}
	}
}
