package gotable

// TestFixedRoadmapGoT16OrdinalLanes is the GO leg's R31 proof: full-width enum
// and union ordinals ride in full-width lanes (docs/FIXED-FORM-ALGORITHM.md
// §4.1 "arg IS FULL WIDTH AND SO IS arg2: no byte lane anywhere", §4.5 the
// ordinal's 64-bit temporary, §5.2 "the remap table is as long as the WRITER's
// variant count" and §5.8 row 14 "past the uint16_t length word the plan refuses
// by name"). One subtest, LEG/R31, t.Parallel() first.
//
// NOTHING HERE SHELLS OUT. The four clauses are read off the shared runtime
// template (tableFixedRuntime) this leg splices into every generated Table.go,
// and the "full ordinal, no 8-bit truncation" clause is read off a 300-arm
// union's emitted identity plan, where arm 300's guard ordinal must land 300 —
// not 300 mod 256, which a byte lane would have stamped.
//
// A writer whose variant count passes the uint16 length word refuses at compile
// by the name the C leg uses, `plan_too_large`: the remap overflow sets
// c.overflow (not c.tooLarge), which tableFixedCompile maps to plan_too_large
// exactly as the C twin's table_fixed_lay_table maps its overflow to
// SCHEMA_TABLE_PLAN_TOO_LARGE (§5.8 row 14; §5.2 PLAN "the entries or their
// tables do not fit the declared capacity: REFUSE plan_too_large").

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

	// §4.5: the ordinal op reads through a 64-bit temporary, never a 32-bit one,
	// so an eight-byte ordinal keeps its top four bytes.
	ord := t02GoBlock(t, rt, "case tableFixedOrdinal:", "case tableFixedWiden:")
	t02GoHas(t, ord, "raw := tableFixedTagAt(src, p.Src, uint8(p.Size))",
		"R31 the ordinal op loads a 64-bit temporary at the ordinal's own width")
	if strings.Contains(ord, "tableFixedGet32") {
		t.Error("R31: the ordinal op goes through a 32-bit temporary; an eight-byte ordinal loses its top four bytes")
	}

	// §5.2: the remap table's length is the WRITER's variant count, never a
	// fixed 255.
	t02GoHas(t, rt, "n := te.Children", "R31 the remap table's length is the writer's variant count")

	// §5.8 row 14: a writer whose variant count passes the uint16 length word is
	// refused at compile, by name — overflow is plan_too_large, not
	// layout_record_too_large.
	t02GoHas(t, rt, "if n > tableFixedRemapMax { c.overflow = true return }",
		"R31 a writer past the length word refuses plan_too_large, never layout_record_too_large")
	t02GoHas(t, rt, "const tableFixedRemapMax uint32 = 0xFFFF",
		"R31 the length word is 16 bits")

	// A 300-ARM UNION'S GUARD ORDINAL LANDS FULL WIDTH: arm 300 stamps Arg 300,
	// not 300 mod 256. This is the positive proof the runtime text cannot give
	// on its own.
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
}
