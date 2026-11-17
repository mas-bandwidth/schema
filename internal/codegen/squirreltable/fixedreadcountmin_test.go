package squirreltable_test

// A counted array's count below its declared minimum is refused on read
// (docs/SPEC.md §5.1 row 3; docs/SPEC-TABLES.md §3.4): the reader answers the
// same verdict the Go engine does, and never clamps. The writer stores two of
// [2..3]uint16, the count is forged to one, and the slot it leaves dead is
// zeroed, so only the low bound can decide the read. The control verdict is the
// load's answer, not the per-leg reason name (docs/rulings.json
// refusal-verdict-only).

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
)

func TestSquirrelFixedReadRefusesCountBelowMin(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	const src = "package probe\nfixed table Root\n{\n    ca [2..3]uint16\n}\n"
	u := fixedSynthetic(t, src)
	g := sqEvoGenerate(t, u, nil)
	clean := g.save(t, "Root", "local v = T.Root();\nv.Ca = [7, 65535];\nvalues.append(v);\n")

	body := len(clean) - 10
	forged := sqEvoClone(clean)
	binary.LittleEndian.PutUint32(forged[body:], 1)
	binary.LittleEndian.PutUint16(forged[body+6:], 0)

	if got := g.load(t, "Root", forged); !strings.HasPrefix(got.head, "refused ") {
		t.Errorf("a count of one below [2..3]: %s, want refused", got.head)
	}
	if got := g.load(t, "Root", clean); got.head != "ok 1 w0" || got.dump["r0.ca_count"] != "2" || got.dump["r0.ca[1]"] != "65535" {
		t.Errorf("the count of two at the minimum: %s %v", got.head, got.dump)
	}

	gc := sqEvoGenerate(t, u, nil)
	gc.plant(t, "    if (c < 2) return \"count_over_bound\";\n", "")
	if got := gc.load(t, "Root", forged); strings.HasPrefix(got.head, "refused ") {
		t.Errorf("the negative control: without the low-bound check the forged count still refuses: %s", got.head)
	}
}
