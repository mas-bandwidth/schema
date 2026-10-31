package mootable_test

import (
	"fmt"
	"maps"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE MOO FORM OF THE ACCESSOR AND DESCRIPTOR AGREEMENT GATE (docs/PORTING.md
// J1).
//
// WHAT THE MOO LEG HAS INSTEAD OF A GENERATED ACCESSOR. moo generates no
// accessor: a cook node is read by the one reflective walk
// (table_cookread.moo) at the offsets each <Base>Table.moo registers for its
// records (cookform.go, C.record), so there is no second read path to drift
// from the descriptor. What the runtime reads at is the layout AS REGISTERED
// in the loaded modules, and what every other leg's cook and the build
// version agree on is the compiler's model (ir.RecordLayout). This gate loads
// every cook-carrying unit of the corpus, asks the runtime for EVERY record it
// registered (C.layout), and requires it equal to the compiler's model, record
// by record and field by field: the size, the alignment, the field count and
// each field's offset. A layout emitted wrong, registered under another name,
// or registered twice by two modules is seen here, without a pinned dump that
// happens to cover the field.
//
// REVERSED WITNESS (TestMooLayoutAgreementNegativeControl): TreeNode.left's
// registered offset moved eight in the emitted GraphTable.moo must red that
// field and nothing else.

const mooLayoutProbe = `local C = require("./table_cookread")
REQUIRES
for _, name in { NAMES } do
	local l = C.layout(name)
	if l ~= nil then
		print("rec " .. name .. " " .. l.size .. " " .. l.align .. " " .. #l.fields)
		for i, f in l.fields do
			print("fld " .. name .. " " .. i .. " " .. (if #f.p > 0 then f.p[1] else -1))
		end
	end
end
`

type mooLayoutUnit struct {
	key   string
	g     *generatedUnit
	names []string
}

func mooLayoutLines(t *testing.T, g *generatedUnit, names []string) []string {
	t.Helper()
	var lits []string
	for _, n := range names {
		lits = append(lits, strconv.Quote(n))
	}
	main := strings.Replace(mooLayoutProbe, "REQUIRES\n", g.requireAll(), 1)
	main = strings.Replace(main, "{ NAMES }", "{ "+strings.Join(lits, ", ")+" }", 1)
	out := g.run(t, main, nil)
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// compilerLayout is the compiler's model of one record, in the probe's
// spelling.
func compilerLayout(u *ir.Unit, name string) []string {
	st := u.Tables[name]
	if st == nil {
		st = u.Structs[name]
	}
	if st == nil {
		return nil
	}
	l := ir.RecordLayout(u, st)
	out := []string{fmt.Sprintf("rec %s %d %d %d", name, l.Size, l.Align, len(l.Fields))}
	for i, f := range l.Fields {
		out = append(out, fmt.Sprintf("fld %s %d %d", name, i+1, f.Offset))
	}
	return out
}

func mooLayoutUnits(t *testing.T) []mooLayoutUnit {
	t.Helper()
	units, _ := readManifest(t)
	var out []mooLayoutUnit
	for _, key := range sortedKeys(units) {
		g := generateUnit(t, units, key)
		if g.err != nil || g.files["table_cookread.moo"] == nil {
			continue
		}
		names := slices.Sorted(maps.Keys(g.unit.Tables))
		for n := range g.unit.Structs {
			if g.unit.Tables[n] == nil {
				names = append(names, n)
			}
		}
		out = append(out, mooLayoutUnit{key, g, names})
	}
	if len(out) == 0 {
		t.Fatal("no manifest unit carries the cook reader: the gate is blind")
	}
	return out
}

// mooLayoutDisagreements answers every line on which the registered layout
// and the compiler's differ (by record and field), and how many records were
// compared.
func mooLayoutDisagreements(lu mooLayoutUnit, lines []string) (bad []string, records int) {
	got := map[string][]string{}
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) >= 2 {
			got[f[1]] = append(got[f[1]], line)
		}
	}
	for _, name := range slices.Sorted(maps.Keys(got)) {
		want := compilerLayout(lu.g.unit, name)
		if want == nil {
			bad = append(bad, fmt.Sprintf("%s: the runtime registers %s, which the compiler does not declare", lu.key, name))
			continue
		}
		records++
		have := got[name]
		for i := 0; i < max(len(have), len(want)); i++ {
			h, w := "", ""
			if i < len(have) {
				h = have[i]
			}
			if i < len(want) {
				w = want[i]
			}
			if h != w {
				bad = append(bad, fmt.Sprintf("%s: runtime %q, compiler %q", lu.key, h, w))
			}
		}
	}
	return bad, records
}

func TestMooLayoutAgreesWithTheCompiler(t *testing.T) {
	slowtest.Gate(t, "moo (it loads the emitted cook layouts)")
	t.Parallel()
	total := 0
	for _, lu := range mooLayoutUnits(t) {
		bad, records := mooLayoutDisagreements(lu, mooLayoutLines(t, lu.g, lu.names))
		total += records
		for _, b := range bad {
			t.Error(b)
		}
	}
	if total == 0 {
		t.Fatal("no record was compared: the gate is blind")
	}
	t.Logf("%d records held to ir.RecordLayout", total)
}

var treeNodeRecord = regexp.MustCompile(`(\.record\("TreeNode", \{ size = \d+, align = \d+, fields = \{ \{ p = \{[^}]*\}[^}]*\}, \{ p = \{ )(\d+)`)

func TestMooLayoutAgreementNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it loads the emitted cook layouts)")
	t.Parallel()
	for _, lu := range mooLayoutUnits(t) {
		if lu.key != "graphdemo" {
			continue
		}
		src := string(lu.g.files["GraphTable.moo"])
		m := treeNodeRecord.FindStringSubmatchIndex(src)
		if m == nil {
			t.Fatal("GraphTable.moo registers no TreeNode cook layout to plant in: the control would be vacuous")
		}
		off, _ := strconv.Atoi(src[m[4]:m[5]])
		files := maps.Clone(lu.g.files)
		files["GraphTable.moo"] = []byte(src[:m[4]] + strconv.Itoa(off+8) + src[m[5]:])
		planted := mooLayoutUnit{lu.key, writeUnit(t, lu.g.unit, files), lu.names}
		bad, _ := mooLayoutDisagreements(planted, mooLayoutLines(t, planted.g, planted.names))
		if len(bad) != 1 || !strings.Contains(bad[0], "fld TreeNode 2 ") {
			t.Fatalf("TreeNode.left moved eight: want exactly that field red, got %d lines: %v", len(bad), bad)
		}
		t.Logf("TreeNode.left moved eight: %s", bad[0])
		return
	}
	t.Fatal("graphdemo does not carry the cook reader: the control has nothing to plant in")
}
