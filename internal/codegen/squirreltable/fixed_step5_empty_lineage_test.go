package squirreltable_test

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/squirrel/sqrun"
)

// TestSquirrelFixedStep5EmptyLineageIsLayoutUnknown holds §5.3 step 5 with no
// variation (docs/FIXED-FORM-ALGORITHM.md §5.3, "STEP 5's ANSWER IS
// `layout_unknown`, IN EVERY LEG, AND `layout_newer` IS RETIRED";
// SPEC-TABLES.md §21.3): a header hash that no lineage entry holds is
// `layout_unknown`, and that includes a lineage with no entries at all. Only
// step 6, a hash the lineage HOLDS below the floor, is `layout_unsupported`.
// COMPILE never emits an empty lineage (§5.9 #2), so the test empties B's
// lineage in the generated module and reads A's file with it: the miss must
// still be `layout_unknown`. The Squirrel leg used to answer
// `layout_unsupported` here.
func TestSquirrelFixedStep5EmptyLineageIsLayoutUnknown(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	ab := fixedSynthetic(t, "package probe\nfixed table A\n{\n    lead uint32 = 0\n    x int32 = 0\n}\nfixed table B\n{\n    p int8 = 0\n    q int8 = 0\n}\n")

	// The populated lineage: a stranger's hash is layout_unknown.
	g := sqEvoGenerate(t, ab, nil)
	aFile := g.save(t, "A", "local v = T.A();\nv.Lead = 7;\nv.X = 11;\nvalues.append(v);\n")
	if got := g.load(t, "B", aFile); !strings.HasPrefix(got.head, refused("layout_unknown")) {
		t.Errorf("step 5, a populated lineage: B's reader on A's file: %s, want layout_unknown", got.head)
	}

	// The empty lineage: still step 5, still layout_unknown.
	ge := sqEvoGenerate(t, ab, nil)
	ge.plant(t, "_M.BFixedLineage <- [\n", "_M.BFixedLineage <- [];\n_M.BFixedLineageHeld <- [\n")
	if got := ge.load(t, "B", aFile); !strings.HasPrefix(got.head, refused("layout_unknown")) {
		t.Errorf("step 5, an empty lineage: B's reader on A's file: %s, want layout_unknown, never layout_unsupported", got.head)
	}
}
