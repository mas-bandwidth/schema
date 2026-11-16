package pythontable_test

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/pythontable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var pythonUnionOptionalRows = []string{
	"union-values", "union-field-arms", "payload-free-arms", "union-arrays", "union-guards",
	"optional-values", "optional-scalars", "optional-nesting", "optional-arrays",
}

func init() {
	maps.Copy(pythonFixedSetups, map[string]string{
		"union-values": `rows[0].body.type = 1
rows[0].body.v.x, rows[0].body.v.y = 11, -22
rows[1].body.type = 2
rows[1].body.t.n = 41
rows[2].body.type = 3
rows[2].body.n = 1073741821`,
		"union-field-arms": `rows[0].body.type = 1
rows[0].body.sa = -5
rows[1].body.type = 2
rows[1].body.ta = "abc"
rows[2].body.type = 3
rows[2].body.aa = [-1, 0, 1]`,
		"payload-free-arms": `rows[0].lead, rows[0].trail = 10, 20
rows[0].pick.type, rows[0].pick.alpha = 1, 99
rows[1].lead, rows[1].trail = 30, 40
rows[1].pick.alpha, rows[1].pick.type = 777, 2
rows[2].lead, rows[2].trail = 50, 60
rows[2].pick.alpha, rows[2].pick.type = 888, 3`,
		"union-arrays": `a, b, c = Pick(), Pick(), Pick()
a.type, a.a = 1, 3
b.type, b.b = 2, -1099511627776
c.type, c.a = 1, 0
rows[0].picks = [a, b, c]`,
		"union-guards": `rows[1].u.type = 2
rows[1].u.nested.x, rows[1].u.nested.y = 12, 0x12345678
rows[1].tail = 77`,
		"optional-values": `rows[1].plain.a, rows[1].plain.b = 11, 22
rows[1].opt = Inner()
rows[1].opt.a, rows[1].opt.b = 33, 44`,
		"optional-scalars": `rows[1].n = -123456
rows[1].g = 2`,
		"optional-nesting": `rows[1].p = Vec()
rows[1].p.x, rows[1].p.y = 99, -99`,
		"optional-arrays": `rows[1].xs = [7, -8, 9]`,
	})
	maps.Copy(pythonFixedChecks, map[string]string{
		"union-values": `assert (rows[0].body.type, rows[0].body.v.x, rows[0].body.v.y) == (1, 11, -22)
assert (rows[1].body.type, rows[1].body.t.n) == (2, 41)
assert (rows[2].body.type, rows[2].body.n) == (3, 1073741821)`,
		"union-field-arms": `assert (rows[0].body.type, rows[0].body.sa) == (1, -5)
assert (rows[1].body.type, rows[1].body.ta) == (2, "abc")
assert (rows[2].body.type, rows[2].body.aa) == (3, [-1, 0, 1])`,
		"payload-free-arms": `assert [(r.lead, r.pick.type, r.trail) for r in rows] == [(10,1,20),(30,2,40),(50,3,60)]`,
		"union-arrays":      `assert [(v.type, v.a if v.type == 1 else v.b) for v in rows[0].picks] == [(1,3),(2,-1099511627776),(1,0)]`,
		"union-guards": `assert rows[0].u.type == 0 and rows[0].tail == 9
assert (rows[1].u.type, rows[1].u.nested.x, rows[1].u.nested.y, rows[1].tail) == (2,12,0x12345678,77)`,
		"optional-values": `assert rows[0].opt is None
assert (rows[1].plain.a, rows[1].plain.b, rows[1].opt.a, rows[1].opt.b) == (11,22,33,44)`,
		"optional-scalars": `assert rows[0].n is None and rows[0].g is None
assert (rows[1].n, rows[1].g) == (-123456, 2)`,
		"optional-nesting": `assert rows[0].p is None
assert (rows[1].p.x, rows[1].p.y) == (99,-99)`,
		"optional-arrays": `assert rows[0].xs is None and rows[1].xs == [7,-8,9]`,
	})
}

type unionOptionalProofRunner struct{ pythonFixedProofRunner }

func (unionOptionalProofRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	files, err := pythontable.Generate(u)
	if err != nil {
		return nil, err
	}
	script := fmt.Sprintf(`from %s import *
try:
    with open("wire", "rb") as f:
        rows = %s.fixed_load(f.read(), %d)
    %s
except (Refused, AssertionError, AttributeError, IndexError) as err:
    print("FAIL", type(err).__name__, str(err))
else:
    with open("saved", "wb") as f:
        f.write(%s.fixed_save(rows))
    print("OK")
`, u.Package, tc.Root, tc.Count, strings.ReplaceAll(pythonFixedChecks[tc.ID], "\n", "\n    "), tc.Root)
	out, saved := pythonFixedRun(t, files, script, map[string][]byte{"wire": wire})
	if strings.TrimSpace(out) != "OK" {
		return nil, fmt.Errorf("decoded values: %s", strings.TrimSpace(out))
	}
	return saved, nil
}

func TestPythonFixedProofUnionsOptionals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and live C++ fixed-form oracle")
	runner := unionOptionalProofRunner{}
	t.Run("values", func(t *testing.T) { fixedproof.RunValueRows(t, runner, pythonUnionOptionalRows...) })
	t.Run("controls", func(t *testing.T) { fixedproof.RunNegativeControlRows(t, runner, pythonUnionOptionalRows...) })
	t.Run("evolution", func(t *testing.T) {
		fixedproof.RunEvolutionRows(t, "../../..", runner, "optional-values", "union-guards")
	})
	t.Run("types", func(t *testing.T) {
		for _, tc := range fixedproof.Cases() {
			if _, ok := pythonFixedSetups[tc.ID]; !ok {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				t.Parallel()
				u := fixedproof.ParseCheckUnit(t, tc.Schema)
				files, err := pythontable.Generate(u)
				if err != nil {
					t.Fatal(err)
				}
				dir := pythonFixedTree(t, files)
				for _, path := range []string{u.Package, u.Package + "/_schema"} {
					if out, err := pyrun.Analyze(t, dir, path); err != nil {
						t.Fatalf("mypy %s: %v\n%s", path, err, out)
					}
				}
			})
		}
	})
}
