package cstable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
)

// TestCSharpWideClampComparesThroughTheBCLType pins the CS9342 fix in the
// paired bench's source, the bounds pass (cstable/fixedbounds.go): a ranged
// 128-bit leaf of a PACKET closure class (Bench.cs's MixedRow.Flux, stored as
// the serialize runtime's Int128Value) compared bare against a BCL Int128
// literal has two user-defined operator< candidates — Int128Value's and
// Int128's, each reachable by an implicit conversion — and csc refuses it as
// ambiguous. The emitter compares through the field's BCL type, so the
// comparison names one operator; the assignment still lands through the
// implicit conversion.
func TestCSharpWideClampComparesThroughTheBCLType(t *testing.T) {
	t.Parallel()

	c := compiler.New()
	u, err := c.Load([]string{"../../../bench/corpus/Bench.schema", "../../../bench/corpus/FixedTable.schema"})
	if err != nil {
		t.Fatalf("load bench/corpus: %v", err)
	}
	files, err := c.Generate(u, "cs", compiler.Options{})
	if err != nil {
		t.Fatalf("generate cs: %v", err)
	}
	src := string(files["BenchTable.cs"])
	if src == "" {
		t.Fatal("bench/corpus emits no BenchTable.cs")
	}
	if !strings.Contains(src, "if (((System.Int128)value.Flux) < unchecked((Int128)") {
		t.Error("the low end of MixedRow.Flux's clamp does not compare through System.Int128")
	}
	if !strings.Contains(src, "else if (((System.Int128)value.Flux) > unchecked((Int128)") {
		t.Error("the high end of MixedRow.Flux's clamp does not compare through System.Int128")
	}
	for _, bare := range []string{"(value.Flux < unchecked(", "(value.Flux > unchecked("} {
		if strings.Contains(src, bare) {
			t.Errorf("BenchTable.cs still compares %q bare: CS9342 against the packet's Int128Value", bare)
		}
	}
}
