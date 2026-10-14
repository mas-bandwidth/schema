package cstable

// fixedroadmap_t09 — the cs leg's float-and-union value rows of
// docs/roadmap.sexp's node `fixed-tables` (ROADMAP.md "NEW Fixed Tables"). One
// subtest per task id; every clause asserted is the one the task's own title
// states, read off the C# this leg emits for a fixture that declares the
// construct. Law: docs/SPEC-TABLES.md §3.4 (the fixed record and its kinds),
// docs/FIXED-FORM-ALGORITHM.md §1 (the layout kinds) and §5.2 (the plan the
// record is read and written through).
//
// The leg's C# runtime is a gate-built target (test/cs-tables); these subtests
// hold the EMITTED codec to the construct: the value surface the record is
// written from and read into, the plan entry that copies it, and the bounds
// pass the read runs. A runtime execution needs the sibling `serialize.cs`
// checkout and dotnet, and is named as not run in the card's report.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapCSharpT09FloatsUnions(t *testing.T) {
	t.Parallel()
	tests := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"float32-values/cs/valid-data", t09Float32},
		{"float64-values/cs/valid-data", t09Float64},
		{"compressed-floats/cs/valid-data", t09Compressed},
		{"flags-values/cs/valid-data", t09Flags},
		{"utf16-values/cs/valid-data", t09UTF16},
		{"nested-types/cs/valid-data", t09Nested},
		{"keyed-arrays/cs/valid-data", t09Keyed},
		{"nested-keyed-arrays/cs/valid-data", t09NestedKeyed},
		{"union-field-arms/cs/valid-data", t09UnionFieldArms},
		{"union-arrays/cs/valid-data", t09UnionArrays},
	}
	for _, tc := range tests {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			tc.run(t)
		})
	}
}

// t09Float32: float32-values [capability] "32-bit floating-point fields"
// (SPEC-TABLES §3.4: "a float32 field ... is a 4-byte record slot"). The value
// surface is a `float`, the body is four bytes, and the plan's one entry COPIES
// those four bytes from the wire into the slot and back.
func t09Float32(t *testing.T) {
	src := generateCS(t, `package probe

fixed table T { f float32 }
`)
	t02Has(t, src, "public float F;", "float32-values: the value surface is a 32-bit float")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 4;", "float32-values: the record body is four bytes")
	t02Has(t, t02Norm(src), "new TableFixedEntry(0u, 0u, 4u, 0u, TableFixedWire.NoGuard, TableFixedWire.Copy, 0, 0, 0, 0, 1),",
		"float32-values: the plan copies the four-byte float at offset zero")
}

// t09Float64: float64-values [capability] "64-bit floating-point fields"
// (SPEC-TABLES §3.4). The value surface is a `double`, the body is eight bytes,
// and the plan copies those eight bytes.
func t09Float64(t *testing.T) {
	src := generateCS(t, `package probe

fixed table T { d float64 }
`)
	t02Has(t, src, "public double D;", "float64-values: the value surface is a 64-bit double")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 8;", "float64-values: the record body is eight bytes")
	t02Has(t, t02Norm(src), "new TableFixedEntry(0u, 0u, 8u, 0u, TableFixedWire.NoGuard, TableFixedWire.Copy, 0, 0, 0, 0, 1),",
		"float64-values: the plan copies the eight-byte double at offset zero")
}

// t09Compressed: compressed-floats [capability] "Compressed-float declarations
// stored as float32" (SPEC-TABLES §3.4: "it rides as the float, not as a
// quantized index"). The declaration stores a float32; the READ runs the
// declared min/max bounds pass over it, which is what a compressed float's
// bounds are in the fixed form.
func t09Compressed(t *testing.T) {
	src := generateCS(t, `package probe

fixed table T { q float32 | min = 0.0, max = 1.0, resolution = 0.01 }
`)
	t02Has(t, src, "public float Q;", "compressed-floats: the declaration is stored as a float32")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 4;", "compressed-floats: the body is the float's four bytes")
	body := csFn(src, "public static void TFixedClampBody(T value, ref int clamped)")
	t02Has(t, t02Norm(body), "if (value.Q < 0.0f) { value.Q = 0.0f; clamped++; }",
		"compressed-floats: the read clamps below the declared min")
	t02Has(t, t02Norm(body), "else if (value.Q > 1.0f) { value.Q = 1.0f; clamped++; }",
		"compressed-floats: the read clamps above the declared max")
}

// t09Flags: flags-values [capability] "Flags masks" (SPEC-TABLES §3.4: a flags
// field is one integer of its mask's storage width). The value surface is the
// mask integer and the plan copies its whole width.
func t09Flags(t *testing.T) {
	src := generateCS(t, `package probe

flags Marks { One, Two, Three }

fixed table T { mask Marks }
`)
	t02Has(t, src, "public ulong Mask;", "flags-values: the value surface is the mask integer")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 8;", "flags-values: the mask's storage is the record width")
	t02Has(t, t02Norm(src), "new TableFixedEntry(0u, 0u, 8u, 0u, TableFixedWire.NoGuard, TableFixedWire.Copy, 0, 0, 0, 0, 1),",
		"flags-values: the plan copies the whole mask")
}

// t09UTF16: utf16-values [capability] "Bounded UTF-16 string fields"
// (SPEC-TABLES §3.4; the bound is in code units). The value surface is a
// `char[]` of the bound plus its live length, and the plan carries the text op
// over the code units.
func t09UTF16(t *testing.T) {
	src := generateCS(t, `package probe

fixed table T { s wstring(8) }
`)
	t02Has(t, src, "public char[] S = new char[8];", "utf16-values: the value surface is a bounded char array")
	t02Has(t, src, "public int SLength;", "utf16-values: the value keeps its live code-unit count")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 20;", "utf16-values: the body is the bound's code units plus the count")
	t02Has(t, t02Norm(src), "new TableFixedEntry(0u, 0u, 16u, 1u, TableFixedWire.NoGuard, TableFixedWire.Text, 0, 0, 0, 2, 1),",
		"utf16-values: the plan reads and writes the text field")
}

// t09Nested: nested-types [capability] "Nested types by value" (SPEC-TABLES
// §2.2, §3.4: a nested type is laid in the record, not pointed at). The value
// surface holds the nested value in place and the plan carries its whole run.
func t09Nested(t *testing.T) {
	src := generateCS(t, `package probe

type Vec { x int32
    y int32 }

fixed table T { v Vec }
`)
	t02Has(t, src, "public Vec V = new Vec();", "nested-types: the nested value is held by value")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 8;", "nested-types: the body is the nested type's own bytes")
	t02Has(t, t02Norm(src), "new TableFixedEntry(0u, 0u, 4u, 0u, TableFixedWire.NoGuard, TableFixedWire.Copy, 0, 0, 0, 0, 1),",
		"nested-types: the plan lands the nested run's first slot")
}

// t09Keyed: keyed-arrays [capability] "Enum-keyed arrays" (SPEC-TABLES §2.4,
// §3.4: `[Enum]T` is one slot per named variant). The value surface is the
// keyed storage and the plan lays one run per variant.
func t09Keyed(t *testing.T) {
	src := generateCS(t, `package probe

enum Tier { Bronze, Silver, Gold }

fixed table T { slots [Tier]int32 }
`)
	t02Has(t, src, "public TableKeyed<int, Tier> Slots = new TableKeyed<int, Tier>(); // [Tier]: one slot per named variant, keyed by the value",
		"keyed-arrays: the value surface is one slot per named variant")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 12;", "keyed-arrays: the body is three variant slots")
	if n := countPlanCopies(t, src); n != 3 {
		t.Errorf("keyed-arrays: the plan carries %d slot runs, want one per named variant (3)", n)
	}
}

// t09NestedKeyed: nested-keyed-arrays [capability] "Nested enum-keyed arrays"
// (ALGORITHM §7). The keyed storage holds a nested value per variant, and the
// plan lays each nested run.
func t09NestedKeyed(t *testing.T) {
	src := generateCS(t, `package probe

type Row { a int32
    b int32 }

enum Tier { Bronze, Silver }

fixed table T { slots [Tier]Row }
`)
	t02Has(t, src, "public TableKeyed<Row, Tier> Slots = new TableKeyed<Row, Tier>(); // [Tier]: one slot per named variant, keyed by the value",
		"nested-keyed-arrays: the value surface is a nested slot per named variant")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 16;", "nested-keyed-arrays: the body is two nested rows")
}

// t09UnionFieldArms: union-field-arms [capability] "Union arms holding scalar,
// text or array fields" (SPEC-TABLES §2.6, §3.4 constant arm storage). The
// shared fixed-form gate admits a DIRECT SCALAR arm — it refuses only a direct
// pointer/map/list/array/text/optional payload (ir/fixedform.go:263-264) — so
// the first fixture declares two direct scalar arms and their values ride at
// their own bounds behind the tag. The gate refuses a direct text or direct
// array arm payload, and the roadmap calls text nested inside a named type arm
// a different supported case (docs/roadmap.sexp:6685); that nested shape is the
// second fixture. The direct text and direct array arm clauses the title names
// are NOT proved by this subtest.
func t09UnionFieldArms(t *testing.T) {
	scalar := generateCS(t, `package probe

union Body {
    sa int32
    sb int32
}

fixed table T { body Body }
`)
	t02Has(t, t02Norm(scalar), "public const long TFixedBodyBytes = 5;",
		"union-field-arms: the body is the tag plus the broadest direct scalar arm")
	t02Has(t, t02Norm(scalar), "new TableFixedEntry(1u, 1u, 4u, 0u, 0u, TableFixedWire.Copy, 1, 0, 0, 0, 1),",
		"union-field-arms: the direct scalar arm sa int32 is stored at its bound")
	t02Has(t, t02Norm(scalar), "new TableFixedEntry(1u, 2u, 4u, 0u, 0u, TableFixedWire.Copy, 2, 0, 0, 0, 1),",
		"union-field-arms: the direct scalar arm sb int32 is stored at its bound")

	nested := generateCS(t, `package probe

type ScalarArm { n int32 }

type TextArm { s wstring(8) }

type ArrayArm { a [4]int32 }

union Body {
    sa ScalarArm
    ta TextArm
    aa ArrayArm
}

fixed table T { body Body }
`)
	t02Has(t, t02Norm(nested), "public const long TFixedBodyBytes = 21;",
		"union-field-arms: the body is the tag plus the widest arm's bound bytes")
	t02Has(t, t02Norm(nested), "new TableFixedEntry(1u, 1u, 4u, 0u, 0u, TableFixedWire.Copy, 1, 0, 0, 0, 1),",
		"union-field-arms: a scalar nested in a type arm is stored at its bound")
	t02Has(t, t02Norm(nested), "new TableFixedEntry(1u, 2u, 16u, 3u, 0u, TableFixedWire.Text, 2, 0, 0, 2, 1),",
		"union-field-arms: text nested in a type arm is stored as bounded text")
	t02Has(t, t02Norm(nested), "new TableFixedEntry(1u, 4u, 16u, 0u, 0u, TableFixedWire.Flat, 3, 0, 0, 0, 1),",
		"union-field-arms: an array nested in a type arm is stored as a fixed run")
}

// t09UnionArrays: union-arrays [capability] "Arrays of unions" (SPEC-TABLES
// §2.6, §3.4: `[N]T`). The value surface is an array of unions and the plan
// lays each element's tag and arms.
func t09UnionArrays(t *testing.T) {
	src := generateCS(t, `package probe

type Cell { n int32 }

union Pick {
    a Cell
    b Cell
}

fixed table T { p [4]Pick }
`)
	t02Has(t, src, "public Pick[] P = new Pick[4];", "union-arrays: the value surface is an array of unions")
	t02Has(t, t02Norm(src), "public const long TFixedBodyBytes = 20;", "union-arrays: the body is four tag-and-arm elements")
}

// countPlanCopies is the number of Copy entries in the fixture's one fixed plan.
func countPlanCopies(t *testing.T, src string) int {
	t.Helper()
	block := t02Block(t, src, "TFixedPlan = new TableFixedPlan(new TableFixedEntry[] {", "});")
	return strings.Count(block, "TableFixedWire.Copy")
}
