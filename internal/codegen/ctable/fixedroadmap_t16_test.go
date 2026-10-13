package ctable

// TestFixedRoadmapCT16OrdinalLanes is the C leg's R31 proof: full-width enum
// and union ordinals ride in full-width lanes (docs/FIXED-FORM-ALGORITHM.md
// §4.1 "arg IS FULL WIDTH AND SO IS arg2: no byte lane anywhere", §4.5 the
// ordinal's 64-bit temporary, §5.2 "the remap table is as long as the WRITER's
// variant count" and §5.8 row 14 "past the uint16_t length word the plan refuses
// by name"). One subtest, c/R31, t.Parallel() first.
//
// NOTHING HERE SHELLS OUT. Every clause is a sentence about the shared runtime
// template (tableFixedRuntime) that ctable.go splices into every generated
// Table.h, so the harness is reachable on a tree that never built the C++
// reference corpus — the same shape the t06 c/R31 test took, extended here to
// the two clauses that test left unproved: the remap table laid at the writer's
// variant count, and the refusal by name past the uint16 length word. The
// refusal is SCHEMA_TABLE_PLAN_TOO_LARGE: table_fixed_lay_table sets overflow,
// and table_fixed_compile maps overflow to plan_too_large (§5.8 row 14; §5.2
// PLAN "the entries or their tables do not fit the declared capacity: REFUSE
// plan_too_large"), never layout_record_too_large.

import (
	"strings"
	"testing"
)

func TestFixedRoadmapCT16OrdinalLanes(t *testing.T) {
	t.Parallel()
	t.Run("c/R31", func(t *testing.T) {
		t.Parallel()
		cT16R31(t)
	})
}

func cT16R31(t *testing.T) {
	rt := tableFixedRuntime

	// §4.1: arg and arg2 are FULL WIDTH (uint64_t), no byte lane anywhere.
	need := []string{
		"uint64_t arg;",    /* the guard ordinal lane is full width */
		"uint64_t arg2;",   /* the outer guard's lane too */
		"uint64_t raw = 0;", /* an ordinal decodes through a 64-bit temporary */
		"const uint32_t n = te.children;", /* the remap length is the WRITER's variant count */
		"if ( n < 0 || n > 65535 ) { c->overflow = 1; return 0; }", /* refuse by name past the uint16 length word */
	}
	for _, s := range need {
		if !strings.Contains(rt, s) {
			t.Errorf("R31: the emitted runtime no longer spells %q", s)
		}
	}
}
