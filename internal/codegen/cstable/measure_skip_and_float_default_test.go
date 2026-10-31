package cstable

import (
	"strings"
	"testing"
)

// TestLoadMeasureEndsTheRecordWhenASkipFails pins the region measure's scan to
// the reference's (internal/tablewire/extent.go) at a framed scalar: a value
// the skip cannot honour ENDS the record. The C# scan ignored the skip's
// result and resumed inside the broken payload, measuring a nested map the
// framing never placed (pinned vector region_map_skip_past_value, mapdemo.Fleet,
// seed 24845619678: "LoadMeasure asks for 528 region bytes; the framing
// commands exactly 512").
func TestLoadMeasureEndsTheRecordWhenASkipFails(t *testing.T) {
	t.Parallel()
	want := "if (!framed || !r.Skip(f.Kind)) { r.Offset = r.Buffer.Length; }"
	if !strings.Contains(tableLoadMeasureSource, want) {
		t.Errorf("the C# region measure ignores a failed skip: want %q", want)
	}
}

// TestPlainFloatElisionComparesBitPatterns pins docs/SPEC-TABLES.md §3.3: a
// plain float's default elision compares bits, so -0 under a +0 default rides.
// The C# writer compared the float as a value (-0 == +0) and dropped the field
// (pinned vector list_float_minus_zero_default, listdemo.Save, seed
// 24845619678: the leg saved 57 bytes, the oracle 71).
func TestPlainFloatElisionComparesBitPatterns(t *testing.T) {
	t.Parallel()
	src := tableWireSource
	if strings.Contains(src, "TableBitsToFloat((uint)raw) == TableBitsToFloat((uint)f.DefaultRaw)") ||
		strings.Contains(src, "TableBitsToDouble(raw) == TableBitsToDouble(f.DefaultRaw)") {
		t.Error("the C# default elision compares a float as a value, so -0 equals +0")
	}
	if !strings.Contains(src, "if (f.Kind == 10) { return (uint)raw == (uint)f.DefaultRaw; }") {
		t.Error("the C# default elision does not compare a float32's bit pattern")
	}
}
