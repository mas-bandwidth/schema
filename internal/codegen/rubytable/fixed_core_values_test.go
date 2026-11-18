package rubytable_test

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

var rubyFixedCoreRows = []string{"record-measurement", "fixed-file-roundtrip", "file-envelope", "batch-capacity", "plan-selection", "compiled-plans", "definition-hash", "fixed-closure", "live-extents", "writing"}

func init() {
	setups := map[string]string{
		"record-measurement":   "rows[0].a = 10; rows[0].b = 20; rows[1].a = 30; rows[1].b = 40",
		"fixed-file-roundtrip": "rows[1].a = 22; rows[1].b = \"test\"",
		"file-envelope":        "rows[1].x = 10; rows[1].y = 20",
		"batch-capacity":       "rows[1].x = 3; rows[1].y = 4; rows[2].x = 5; rows[2].y = 6",
		"plan-selection":       "rows[1].x = 33; rows[1].y = 44",
		"compiled-plans":       "rows[1].a = 100; rows[1].b = -20; rows[1].c = 55",
		"definition-hash":      "rows[1].x = 9; rows[1].b = \"abc\".b",
		"fixed-closure":        "rows[1].n.a = 30; rows[1].n.b = 40; rows[1].z = 70",
		"live-extents":         "rows[1].arr = [1]; rows[1].opt = nil; rows[1].tail = 88",
		"writing":              "rows[1].arr = [0x1122]; rows[1].s = \"A\"; rows[1].b = \"B\".b; rows[1].pick.type = 1; rows[1].pick.narrow = 7",
	}
	checks := map[string]string{
		"record-measurement":   "raise \"bad\" unless [rows[0].a, rows[0].b, rows[1].a, rows[1].b] == [10, 20, 30, 40]",
		"fixed-file-roundtrip": "raise \"bad\" unless rows[0].a == 11 && rows[0].b == \"ok\" && rows[1].a == 22 && rows[1].b == \"test\"",
		"file-envelope":        "raise \"bad\" unless [rows[0].x, rows[0].y, rows[1].x, rows[1].y] == [1, 2, 10, 20]",
		"batch-capacity":       "raise \"bad\" unless rows.map { |r| [r.x, r.y] } == [[1, 2], [3, 4], [5, 6]]",
		"plan-selection":       "raise \"bad\" unless [rows[0].x, rows[0].y, rows[1].x, rows[1].y] == [11, 22, 33, 44]",
		"compiled-plans":       "raise \"bad\" unless [rows[0].a, rows[0].b, rows[0].c] == [1, 10, 99] && [rows[1].a, rows[1].b, rows[1].c] == [100, -20, 55]",
		"definition-hash":      "raise \"bad\" unless rows[0].x == 5 && rows[0].b == \"hi\".b && rows[1].x == 9 && rows[1].b == \"abc\".b",
		"fixed-closure":        "raise \"bad\" unless [rows[0].n.a, rows[0].n.b, rows[0].z] == [3, 4, 7] && [rows[1].n.a, rows[1].n.b, rows[1].z] == [30, 40, 70]",
		"live-extents":         "raise \"bad\" unless rows[0].arr == [] && rows[0].opt.nil? && rows[0].tail == 77 && rows[1].arr == [1] && rows[1].opt.nil? && rows[1].tail == 88",
		"writing":              "raise \"bad\" unless rows[0].arr == [] && rows[0].s == \"\" && rows[0].b == \"\".b && rows[1].arr == [0x1122] && rows[1].s == \"A\" && rows[1].b == \"B\".b && rows[1].pick.narrow == 7",
	}
	for _, row := range rubyFixedCoreRows {
		rubyFixedSetups[row] = setups[row]
		rubyFixedChecks[row] = checks[row]
	}
}

func TestRubyFixedCoreValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Ruby pins and the live C++ byte oracle")
	fixedproof.RunValueRows(t, rubyFixedProofRunner{}, rubyFixedCoreRows...)
}

func TestRubyFixedCoreValuesNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both Ruby pins and the live C++ byte oracle")
	fixedproof.RunNegativeControlRows(t, rubyFixedProofRunner{}, rubyFixedCoreRows...)
}
