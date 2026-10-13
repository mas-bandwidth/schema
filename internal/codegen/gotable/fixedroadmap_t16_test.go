package gotable

// TestFixedRoadmapGoT16OrdinalLanes is the GO leg's R31 proof: full-width enum
// and union ordinals ride in full-width lanes (docs/FIXED-FORM-ALGORITHM.md
// §4.1 "arg IS FULL WIDTH AND SO IS arg2: no byte lane anywhere", §4.5 the
// ordinal's 64-bit temporary, §5.2 "the remap table is as long as the WRITER's
// variant count" and §5.8 row 14 "past the uint16_t length word the plan refuses
// by name"). One subtest, go/R31, t.Parallel() first.
//
// The fixture the card mandates is BUILT here, not read off the runtime text:
// a 300-arm union whose guard ordinal 300 must land whole in the emitted plan's
// Arg lane (a byte lane would stamp 300 mod 256), and an enum whose variant
// count passes the uint16 length word, which the compiled plan refuses by name
// plan_too_large. The runtime-text clauses (arg/arg2 full width, the 64-bit
// temporary, the writer-counted remap) are read off the emitted runtime, and the
// refusal is exercised end-to-end by compiling and running the generated unit.

import (
	"fmt"
	"strings"
	"testing"
)

func TestFixedRoadmapGoT16OrdinalLanes(t *testing.T) {
	t.Parallel()
	t.Run("go/R31", func(t *testing.T) {
		t.Parallel()
		t16GoR31(t)
	})
}

func t16GoR31(t *testing.T) {
	rt := t02GoNorm(tableFixedRuntime)

	// §4.1: arg and arg2 are FULL WIDTH (u64), no byte lane anywhere.
	t02GoHas(t, rt, "Arg uint64", "R31 the plan entry's arg lane is full width (u64)")
	t02GoHas(t, rt, "Arg2 uint64", "R31 the plan entry's arg2 lane is full width (u64)")

	// §4.5: the ordinal op reads through a 64-bit temporary, never a 32-bit one.
	ord := t02GoBlock(t, rt, "case tableFixedOrdinal:", "case tableFixedWiden:")
	t02GoHas(t, ord, "raw := tableFixedTagAt(src, p.Src, uint8(p.Size))",
		"R31 the ordinal op loads a 64-bit temporary at the ordinal's own width")
	if strings.Contains(ord, "tableFixedGet32") {
		t.Error("R31: the ordinal op goes through a 32-bit temporary; an eight-byte ordinal loses its top four bytes")
	}

	// §5.2: the remap table's length is the WRITER's variant count.
	t02GoHas(t, rt, "n := te.Children", "R31 the remap table's length is the writer's variant count")

	// §5.8 row 14: a writer whose variant count passes the uint16 length word is
	// refused at compile, by name — overflow is plan_too_large, not
	// layout_record_too_large.
	t02GoHas(t, rt, "if n > tableFixedRemapMax { c.overflow = true return }",
		"R31 a writer past the length word refuses plan_too_large, never layout_record_too_large")
	t02GoHas(t, rt, "const tableFixedRemapMax uint32 = 0xFFFF",
		"R31 the length word is 16 bits")

	// A 300-ARM UNION'S GUARD ORDINAL LANDS FULL WIDTH: arm 300 stamps Arg 300,
	// not 300 mod 256. This is the positive proof the runtime text cannot give.
	var b strings.Builder
	b.WriteString("package probe\n\nunion Pick\n{\n")
	for i := 0; i < 300; i++ {
		fmt.Fprintf(&b, "    a%d int32\n", i)
	}
	b.WriteString("}\n\nfixed table T\n{\n    p Pick\n}\n")
	src := t02GoEmit(t, unitFrom(t, b.String()))
	leaves := t02GoBlock(t, src, "func TFixedLeaves(", "func ")
	if !strings.Contains(leaves, "Arg = 300") {
		t.Errorf("R31: arm 300's guard ordinal is not stamped full width in the emitted plan; a byte lane would have landed 300 mod 256")
	}

	// AN ENUM WHOSE VARIANT COUNT PASSES THE UINT16 LENGTH WORD IS REFUSED BY
	// NAME, end-to-end: the writer declares 65536 variants, the reader fewer, and
	// the compiled plan for the writer's layout refuses plan_too_large rather
	// than silently dropping the 65536th variant on.
	t16GoEnumRefusal(t)
}

// t16GoEnumRefusal builds a writer with an enum past the uint16 length word and
// a reader with fewer variants, hands the writer's layout to the reader as its
// lineage, and asserts the compiled plan refuses by name. It compiles and runs
// the generated unit, so it is a functional test behind slowtest.Gate.
func t16GoEnumRefusal(t *testing.T) {
	t.Helper()
	var w strings.Builder
	w.WriteString("package probe\n\nenum Big\n{\n")
	for i := 0; i < 65536; i++ {
		fmt.Fprintf(&w, "    v%d\n", i)
	}
	w.WriteString("}\n\nfixed table T\n{\n    e Big\n}\n")
	reader := "package probe\n\nenum Big\n{\n    v0\n}\n\nfixed table T\n{\n    e Big\n}\n"
	out, err := runVersionProbeRetired(t, reader, []string{w.String()}, 0, `package probe
import "testing"

func TestEnumPastLengthWordRefuses(t *testing.T) {
	if len(TFixedLineagePlans) != 2 {
		t.Fatalf("the lineage has %d plans, want 2", len(TFixedLineagePlans))
	}
	if TFixedLineagePlans[0].Why != "plan_too_large" {
		t.Fatalf("a writer past the uint16 length word owes plan_too_large, got %q", TFixedLineagePlans[0].Why)
	}
}
`)
	if err != nil {
		t.Fatalf("R31 enum past the length word: %v\n%s", err, out)
	}
}
