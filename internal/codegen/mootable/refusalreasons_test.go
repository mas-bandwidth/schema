package mootable_test

// THE LOAD'S VERDICTS ON THE SHARED REFUSAL PINS, for moo (docs/SPEC-TABLES.md
// §6.5, §2.9, §3, §4). The rows are the shared pin table,
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv. THE VERDICT
// IS THE CONTRACT (docs/rulings.json refusal-verdict-only): moo's one read
// (table_load.moo's ReadStrict) must return a value for every row the table
// answers ok and refuse every other row. Which reason it names is a
// per-language diagnostic and is not compared.
//
//	TestMooLoadRefusalReasons  every row, through ReadStrict
//	TestMooLoadRefusalReasonsNegativeControl  removes the strict read's
//	    framing verdict, the value-only root's damage bit and the form-2
//	    refusal, each of which must turn exactly its own rows red

import (
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// reasonRow is one pinned file.
type reasonRow struct{ name, schema, wire, strict, form string }

// reasonSchemas are the pin table's three units, as its header states them.
var reasonSchemas = map[string]string{
	"measure": "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n",
	"fixed":   "package probe\n fixed table Root { a int32\n flag bool\n small int8 | max = 5\n f float32 }\n",
	"float":   "package probe\n table Root { f float32\n values []int32 }\n",
}

func reasonRows(t *testing.T) []reasonRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "goosetable", "testdata", "load_refusal_reasons.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []reasonRow
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 || reasonSchemas[f[1]] == "" {
			t.Fatalf("a malformed pin row: %q", line)
		}
		rows = append(rows, reasonRow{f[0], f[1], f[2], f[3], f[4]})
	}
	return rows
}

// reasonProbe prints, per row, ReadStrict's verdict: "ok" for a value, else
// "refused".
const reasonProbe = `local function probe(name: string)
	local rep = load.newReport()
	local v = load.ReadStrict("Root", inputs[name], rep)
	print(name .. "\t" .. (if v ~= nil and rep.reason == nil then "ok" else "refused"))
end
`

// loadReasons is the gate's red list: one line per row whose moo answer is
// not the one this file expects of it. mutate, when set, edits the emitted
// table_load.moo first.
func loadReasons(t *testing.T, mutate func(string) string) []string {
	t.Helper()
	rows := reasonRows(t)
	var bad []string
	for _, schema := range []string{"measure", "fixed", "float"} {
		g := generateFiles(t, map[string]string{"Probe.schema": reasonSchemas[schema]})
		if mutate != nil {
			files := maps.Clone(g.files)
			files["table_load.moo"] = []byte(mutate(string(files["table_load.moo"])))
			g = writeUnit(t, g.unit, files)
		}
		var main strings.Builder
		main.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\n" + g.requireAll() + reasonProbe)
		in := map[string][]byte{}
		var mine []reasonRow
		for _, r := range rows {
			if r.schema != schema {
				continue
			}
			mine = append(mine, r)
			wire := []byte{}
			if r.wire != "-" {
				b, err := hex.DecodeString(r.wire)
				if err != nil {
					t.Fatalf("%s: %v", r.name, err)
				}
				wire = b
			}
			in[r.name] = wire
			fmt.Fprintf(&main, "probe(%q)\n", r.name)
		}
		got := map[string]string{}
		for line := range strings.SplitSeq(g.run(t, main.String(), in), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 2 {
				t.Fatalf("the probe printed %q", line)
			}
			got[f[0]] = f[1]
		}
		for _, r := range mine {
			want := "refused"
			if r.strict == "ok" && r.form == "-" {
				want = "ok"
			}
			if got[r.name] != want {
				bad = append(bad, fmt.Sprintf("%s: moo answers %q, want %q (pin: strict %s, form %s)", r.name, got[r.name], want, r.strict, r.form))
			}
		}
	}
	return bad
}

func TestMooLoadRefusalReasons(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	rows := reasonRows(t)
	answered := map[string]bool{}
	for _, r := range rows {
		answered[r.strict] = true
	}
	if !answered["ok"] || len(answered) < 2 {
		t.Errorf("the pin table needs rows that read and rows that refuse: %v", answered)
	}
	for _, b := range loadReasons(t, nil) {
		t.Error(b)
	}
	t.Logf("%d pinned rows answered", len(rows))
}

// TestMooLoadRefusalReasonsNegativeControl removes one refusal at a time
// from the emitted table_load.moo and requires exactly the rows it guards to
// go red (read where the pin refuses):
//
//   - the refusing counters (a value out of range, a kind mismatch). Damage
//     is the walk's own discard, held by TestMooDamageDiscardNegativeControls.
func TestMooLoadRefusalReasonsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	for _, ctl := range []struct {
		name, from, to string
		red            []string
	}{
		{"refusing-counters",
			"local REFUSING = { \"missingRequired\", \"bodyDamaged\", \"blobShared\", \"outOfRange\", \"kindChanged\" }",
			"local REFUSING = { \"missingRequired\", \"bodyDamaged\", \"blobShared\" }",
			[]string{"fixed_clamped", "fixed_kind_mismatch", "values_f32"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := loadReasons(t, func(src string) string {
				if strings.Count(src, ctl.from) != 1 {
					t.Fatalf("%s: table_load.moo carries the anchor %d times, want 1: the control would be vacuous", ctl.name, strings.Count(src, ctl.from))
				}
				return strings.Replace(src, ctl.from, ctl.to, 1)
			})
			var red []string
			for _, b := range bad {
				red = append(red, b[:strings.Index(b, ":")])
			}
			slices.Sort(red)
			want := slices.Clone(ctl.red)
			slices.Sort(want)
			if !slices.Equal(red, want) {
				t.Fatalf("%s: red rows %v, want exactly %v", ctl.name, red, want)
			}
			t.Logf("%s: %d rows red, first %s", ctl.name, len(bad), bad[0])
		})
	}
}
