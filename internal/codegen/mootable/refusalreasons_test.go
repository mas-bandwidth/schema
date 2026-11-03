package mootable_test

// THE LOAD'S REFUSAL REASONS, for moo (docs/SPEC-TABLES.md §6.5, §2.9, §3,
// §4): a refused load names WHICH reason, by its spec name, and the name is
// the reference's for the same bytes. The rows are the shared pin table,
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv, whose
// answers the Go leg's generated reader holds (gotable's
// TestLoadRefusalReasonPins) and the Goose load holds
// (goosetable's TestGooseLoadRefusalReasons).
//
//	TestMooLoadRefusalReasons  every row, through table_load.moo's ReadStrict,
//	    moo's one strict read. It answers ONE name, on report.reason: the
//	    strict column's, so every form-byte refusal is unknown_form and every
//	    damaged wire is wire_damaged (§6.5). The tolerant load (loadTolerant)
//	    names the form column's refusal on its report, as Go's
//	    TableReport.Reason does
//	TestMooLoadRefusalReasonsNegativeControl  breaks the strict read's
//	    framing verdict, its body-damage name and its form-byte name, each of
//	    which must turn exactly its own rows red

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

// reasonProbe prints, per row, ReadStrict's one answer: "ok" for a value,
// else the reason's name before any ": <field>" suffix.
const reasonProbe = `local function probe(name: string)
	local rep = load.newReport()
	local v = load.ReadStrict("Root", inputs[name], rep)
	local answer = "ok"
	if v == nil then
		answer = string.split(tostring(rep.reason), ":")[1]
	end
	local tolerant = load.newReport()
	load.loadTolerant("Root", inputs[name], tolerant)
	print(name .. "\t" .. answer .. "\t" .. tostring(tolerant.reason))
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
		got, tolerant := map[string]string{}, map[string]string{}
		for line := range strings.SplitSeq(g.run(t, main.String(), in), "\n") {
			f := strings.Split(line, "\t")
			if len(f) != 3 {
				t.Fatalf("the probe printed %q", line)
			}
			got[f[0]], tolerant[f[0]] = f[1], f[2]
		}
		for _, r := range mine {
			want := r.strict
			if r.form != "-" && tolerant[r.name] != r.form {
				bad = append(bad, fmt.Sprintf("%s: the tolerant load's report names %q, want %q", r.name, tolerant[r.name], r.form))
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
	for _, want := range []string{"ok", "wire_damaged", "unknown_form", "kind_mismatch", "clamped", "non_finite_float"} {
		if !answered[want] {
			t.Errorf("no pin row answers %s", want)
		}
	}
	for _, b := range loadReasons(t, nil) {
		t.Error(b)
	}
	t.Logf("%d pinned rows answered", len(rows))
}

// TestMooLoadRefusalReasonsNegativeControl:
//
//   - the strict read's framing verdict names malformed instead of
//     wire_damaged: the rows its frame-and-terminator check refuses (a form
//     byte alone, a cut file, a terminator before the id table) go red, and
//     only they;
//   - damage in a body behind good framing is named malformed instead of
//     wire_damaged: the rows whose body is damaged behind good framing (a
//     count past its field's length, a fixed payload cut short) go red, and
//     only they;
//   - a form byte the read does not carry is named newer_form instead of
//     unknown_form: the seven form-byte rows go red, and only they.
func TestMooLoadRefusalReasonsNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_load.moo)")
	t.Parallel()
	for _, ctl := range []struct {
		name, from, to string
		red            []string
	}{
		{"framing-verdict",
			"\tif fr == nil or (top ~= nil and top.term + 1 ~= fr.e) then\n\t\trep.malformed = true\n\t\trep.reason = \"wire_damaged\"\n",
			"\tif fr == nil or (top ~= nil and top.term + 1 ~= fr.e) then\n\t\trep.malformed = true\n\t\trep.reason = \"malformed\"\n",
			[]string{"cut_last", "fixed_early_end", "form_only"}},
		{"body-damage-reason",
			"\t\trep.reason = \"wire_damaged\"\n\t\tif not rep.damaged then\n",
			"\t\trep.reason = if rep.damaged then \"wire_damaged\" else \"malformed\"\n\t\tif not rep.damaged then\n",
			[]string{"entries_over_length", "fixed_cut_payload", "values_over_length"}},
		{"form-byte-reason",
			"\tif form ~= 1 then\n\t\trep.reason = \"unknown_form\"\n",
			"\tif form ~= 1 then\n\t\trep.reason = \"newer_form\"\n",
			[]string{"form0", "form2", "form2_alone", "form3", "form9", "form255", "form255_alone"}},
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
