package pythontable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

var pythonFixedCoreRows = []string{"record-measurement", "fixed-file-roundtrip", "file-envelope", "batch-capacity", "plan-selection", "compiled-plans", "definition-hash", "fixed-closure", "live-extents", "writing"}

func init() {
	setups := map[string]string{
		"record-measurement":   "rows[0].a=10; rows[0].b=20; rows[1].a=30; rows[1].b=40",
		"fixed-file-roundtrip": "rows[1].a=22; rows[1].b='test'",
		"file-envelope":        "rows[1].x=10; rows[1].y=20",
		"batch-capacity":       "rows[1].x=3; rows[1].y=4; rows[2].x=5; rows[2].y=6",
		"plan-selection":       "rows[1].x=33; rows[1].y=44",
		"compiled-plans":       "rows[1].a=100; rows[1].b=-20; rows[1].c=55",
		"definition-hash":      "rows[1].x=9; rows[1].b=b'abc'",
		"fixed-closure":        "rows[1].n.a=30; rows[1].n.b=40; rows[1].z=70",
		"live-extents":         "rows[1].arr=[1]; rows[1].opt=None; rows[1].tail=88",
		"writing":              "rows[1].arr=[0x1122]; rows[1].s='A'; rows[1].b=b'B'; rows[1].pick.type=1; rows[1].pick.narrow=7",
	}
	checks := map[string]string{
		"record-measurement":   "assert (rows[0].a,rows[0].b,rows[1].a,rows[1].b)==(10,20,30,40)",
		"fixed-file-roundtrip": "assert rows[0].a==11 and rows[0].b=='ok' and rows[1].a==22 and rows[1].b=='test'",
		"file-envelope":        "assert (rows[0].x,rows[0].y,rows[1].x,rows[1].y)==(1,2,10,20)",
		"batch-capacity":       "assert [(r.x,r.y) for r in rows]==[(1,2),(3,4),(5,6)]",
		"plan-selection":       "assert (rows[0].x,rows[0].y,rows[1].x,rows[1].y)==(11,22,33,44)",
		"compiled-plans":       "assert (rows[0].a,rows[0].b,rows[0].c)==(1,10,99) and (rows[1].a,rows[1].b,rows[1].c)==(100,-20,55)",
		"definition-hash":      "assert rows[0].x==5 and rows[0].b==b'hi' and rows[1].x==9 and rows[1].b==b'abc'",
		"fixed-closure":        "assert (rows[0].n.a,rows[0].n.b,rows[0].z)==(3,4,7) and (rows[1].n.a,rows[1].n.b,rows[1].z)==(30,40,70)",
		"live-extents":         "assert rows[0].arr==[] and rows[0].opt is None and rows[0].tail==77 and rows[1].arr==[1] and rows[1].opt is None and rows[1].tail==88",
		"writing":              "assert rows[0].arr==[] and rows[0].s=='' and rows[0].b==b'' and rows[1].arr==[0x1122] and rows[1].s=='A' and rows[1].b==b'B' and rows[1].pick.narrow==7",
	}
	for _, row := range pythonFixedCoreRows {
		if _, exists := pythonFixedSetups[row]; exists {
			panic("duplicate fixed-core setup " + row)
		}
		pythonFixedSetups[row] = setups[row]
		pythonFixedChecks[row] = checks[row]
	}
}

func TestPythonFixedCoreValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins and the live C++ byte oracle")
	fixedproof.RunValueRows(t, pythonFixedProofRunner{}, pythonFixedCoreRows...)
}

func TestPythonFixedCoreValuesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Python pins and the live C++ byte oracle")
	fixedproof.RunNegativeControlRows(t, pythonFixedProofRunner{}, pythonFixedCoreRows...)
}
