package rubytable_test

import (
	"fmt"
	"maps"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/ruby/rubyrun"
	"github.com/mas-bandwidth/schema/v3/internal/codegen/rubytable"
	"github.com/mas-bandwidth/schema/v3/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v3/internal/slowtest"
	"github.com/mas-bandwidth/schema/v3/ir"
)

var rubyUnionOptionalRows = []string{
	"union-values", "union-field-arms", "payload-free-arms", "union-arrays", "union-guards",
	"optional-values", "optional-scalars", "optional-nesting", "optional-arrays",
}

func init() {
	maps.Copy(rubyFixedSetups, map[string]string{
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
		"union-arrays": `a, b, c = Pick.new, Pick.new, Pick.new
a.type, a.a = 1, 3
b.type, b.b = 2, -1099511627776
c.type, c.a = 1, 0
rows[0].picks = [a, b, c]`,
		"union-guards": `rows[1].u.type = 2
rows[1].u.nested.x, rows[1].u.nested.y = 12, 0x12345678
rows[1].tail = 77`,
		"optional-values": `rows[1].plain.a, rows[1].plain.b = 11, 22
rows[1].opt = Inner.new
rows[1].opt.a, rows[1].opt.b = 33, 44`,
		"optional-scalars": `rows[1].n = -123456
rows[1].g = 2`,
		"optional-nesting": `rows[1].p = Vec.new
rows[1].p.x, rows[1].p.y = 99, -99`,
		"optional-arrays": `rows[1].xs = [7, -8, 9]`,
	})
	maps.Copy(rubyFixedChecks, map[string]string{
		"union-values": `raise "bad" unless [rows[0].body.type, rows[0].body.v.x, rows[0].body.v.y] == [1, 11, -22]
raise "bad" unless [rows[1].body.type, rows[1].body.t.n] == [2, 41]
raise "bad" unless [rows[2].body.type, rows[2].body.n] == [3, 1073741821]`,
		"union-field-arms": `raise "bad" unless [rows[0].body.type, rows[0].body.sa] == [1, -5]
raise "bad" unless [rows[1].body.type, rows[1].body.ta] == [2, "abc"]
raise "bad" unless [rows[2].body.type, rows[2].body.aa] == [3, [-1, 0, 1]]`,
		"payload-free-arms": `raise "bad" unless rows.map { |r| [r.lead, r.pick.type, r.trail] } == [[10, 1, 20], [30, 2, 40], [50, 3, 60]]`,
		"union-arrays":      `raise "bad" unless rows[0].picks.map { |v| [v.type, v.type == 1 ? v.a : v.b] } == [[1, 3], [2, -1099511627776], [1, 0]]`,
		"union-guards": `raise "bad" unless rows[0].u.type == 0 && rows[0].tail == 9
raise "bad" unless [rows[1].u.type, rows[1].u.nested.x, rows[1].u.nested.y, rows[1].tail] == [2, 12, 0x12345678, 77]`,
		"optional-values":  `raise "bad" unless rows[0].opt.nil? && rows[1].opt.a == 33 && rows[1].opt.b == 44`,
		"optional-scalars": `raise "bad" unless rows[0].n.nil? && rows[0].g.nil? && rows[1].n == -123456 && rows[1].g == 2`,
		"optional-nesting": `raise "bad" unless rows[0].p.nil? && rows[1].p.x == 99 && rows[1].p.y == -99`,
		"optional-arrays":  `raise "bad" unless rows[0].xs.nil? && rows[1].xs == [7, -8, 9]`,
	})
}

type rubyUnionOptionalRunner struct{ rubyFixedProofRunner }

func (rubyUnionOptionalRunner) LoadAndResave(t *testing.T, tc fixedproof.RowCase, u *ir.Unit, wire []byte) ([]byte, error) {
	t.Helper()
	files, err := rubytable.Generate(u)
	if err != nil {
		return nil, err
	}
	mod := ruby.ModuleName(u.Package)
	script := fmt.Sprintf(`require_relative %q
include %s
begin
  rows = %s.fixed_load(File.binread("wire"), %d)
  %s
rescue StandardError => err
  puts "FAIL #{err.class}: #{err.message}"
else
  File.binwrite("saved", %s.fixed_save(rows))
  puts "OK"
end
`, u.Package, mod, tc.Root, tc.Count, strings.ReplaceAll(rubyFixedChecks[tc.ID], "\n", "\n  "), tc.Root)
	out, saved := rubyFixedRun(t, files, script, map[string][]byte{"wire": wire})
	if strings.TrimSpace(out) != "OK" {
		return nil, fmt.Errorf("decoded values: %s", strings.TrimSpace(out))
	}
	return saved, nil
}

func TestRubyFixedProofUnionsOptionals(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and live C++ fixed-form oracle")
	runner := rubyUnionOptionalRunner{}
	t.Run("values", func(t *testing.T) { fixedproof.RunValueRows(t, runner, rubyUnionOptionalRows...) })
	t.Run("controls", func(t *testing.T) { fixedproof.RunNegativeControlRows(t, runner, rubyUnionOptionalRows...) })
	t.Run("evolution", func(t *testing.T) {
		fixedproof.RunEvolutionRows(t, "../../..", runner, "optional-values", "union-guards")
	})
	t.Run("types", func(t *testing.T) {
		for _, tc := range fixedproof.Cases() {
			if !slices.Contains(rubyUnionOptionalRows, tc.ID) {
				continue
			}
			t.Run(tc.ID, func(t *testing.T) {
				t.Parallel()
				u := fixedproof.ParseCheckUnit(t, tc.Schema)
				files, err := rubytable.Generate(u)
				if err != nil {
					t.Fatal(err)
				}
				dir := rubyFixedTree(t, files)
				rubyrun.Analyze(t, dir, u.Package)
			})
		}
	})
}
