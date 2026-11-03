package goosetable_test

// THE LOAD'S REFUSAL REASONS, for Goose (docs/SPEC-TABLES.md §6.5, §2.9, §3,
// §4): a refused load names WHICH reason, by its spec name, and the name is
// the reference's for the same bytes.
//
//	TestGooseLoadRefusalReasons  every row of
//	    testdata/load_refusal_reasons.tsv, whose answers the Go leg's
//	    generated reader holds (gotable's TestLoadRefusalReasonPins): the
//	    strict read's one reason (load_error, Go's <T>ReadStrict) and the
//	    form refusal the report names (report.reason, Go's
//	    TableReport.Reason)
//	TestGooseLoadRefusalReasonsNegativeControl  breaks the verdict's damage,
//	    the form refusal's mapping and the count cap's refusal, each of which
//	    must turn exactly its own rows red
//
// The rows reach every reason Go's TestMeasureRefusalReasons proves that has
// a load-time meaning. The file a measure refuses count_over_length is a
// load's malformed body, and the one it refuses count_over_extent_cap is a
// load that refuses by that same name, malformed not set (§2.9's table:
// "damage: malformed counts" and the builder's count_over_extent_cap);
// wire_damaged and unknown_form are the load's own. The other
// three are a writer's or an allocator's and read no wire: data_cycle, "the
// AUTHORING side's -1 and the one value here that is not about a wire" (a
// cyclic WIRE is damage, §3.1); invalid_value, "a writer handed a value
// outside the declaration"; allocation_failed, "an allocation the codec asked
// for was refused" (§7.6), and the Goose load asks for none.

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// reasonRow is one pinned file.
type reasonRow struct{ name, schema, wire, strict, form string }

// reasonSchemas are the pin table's two units, as its header states them.
var reasonSchemas = map[string]string{
	"measure": "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n",
	"fixed":   "package probe\n fixed table Root { a int32\n flag bool\n small int8 | max = 5\n f float32 }\n",
}

func reasonRows(t *testing.T) []reasonRow {
	t.Helper()
	data, err := os.ReadFile("testdata/load_refusal_reasons.tsv")
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

const reasonProbeMain = `
fn probe(name: const u8[:]) {
    let data = input(str(name, ".bin"));
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var report = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(conformance_unit(), "Root", data, vals, nodes, report);
    write_stdout(str(name, " ", schema_rt::load_refusal_name(schema_rt::load_error(report, root)), "_ ",
        schema_rt::load_refusal_name(report.reason), "_\n"));
}

fn main() {
`

// loadReasons is the gate's red list: one line per row whose Goose answer is
// not the pin.
func loadReasons(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	rows := reasonRows(t)
	var bad []string
	for _, schema := range []string{"measure", "fixed"} {
		files := probeFiles(t, probeUnit(t, reasonSchemas[schema]), override)
		var main strings.Builder
		main.WriteString(probePrelude)
		main.WriteString(reasonProbeMain)
		var mine []reasonRow
		for _, r := range rows {
			if r.schema != schema {
				continue
			}
			mine = append(mine, r)
			files[r.name+".bin"] = probeHex(t, r.wire)
			fmt.Fprintf(&main, "    probe(%q);\n", r.name)
		}
		main.WriteString("}\n")
		files["main.goose"] = []byte(main.String())
		got := probeLines(gooserun.Run(t, t.TempDir(), "main.goose", files))
		for _, r := range mine {
			g := got[r.name]
			if len(g) != 2 {
				t.Fatalf("%s: the probe printed %q", r.name, g)
			}
			strict, form := strings.TrimSuffix(g[0], "_"), strings.TrimSuffix(g[1], "_")
			if strict == "" {
				strict = "ok"
			}
			if r.form == "-" {
				form = "-"
			}
			if strict != r.strict || form != r.form {
				bad = append(bad, fmt.Sprintf("%s: strict %s, form %s; the reference %s, %s", r.name, strict, form, r.strict, r.form))
			}
		}
	}
	return bad
}

func TestGooseLoadRefusalReasons(t *testing.T) {
	t.Parallel()
	reasons := map[string]bool{}
	for _, r := range reasonRows(t) {
		reasons[r.strict] = true
	}
	for _, want := range []string{"ok", "wire_damaged", "malformed", "unknown_form", "count_over_extent_cap", "kind_mismatch", "clamped", "non_finite_float"} {
		if !reasons[want] {
			t.Errorf("no pin row answers %s", want)
		}
	}
	for _, b := range loadReasons(t, nil) {
		t.Error(b)
	}
}

// TestGooseLoadRefusalReasonsNegativeControl:
//
//   - the verdict's damage no longer names wire_damaged: the rows whose
//     framing failed, and the fixed root's damage its walk read past, answer
//     malformed and go red; the count-cap rows, refused by name and not
//     damaged, stay green;
//   - a form byte this reader does not carry no longer answers unknown_form:
//     the form rows go red, and only they;
//   - a count past the int32 storage cap no longer answers
//     count_over_extent_cap: the four cap rows fall to the strict read's
//     wire_damaged and go red, and only they.
func TestGooseLoadRefusalReasonsNegativeControl(t *testing.T) {
	t.Parallel()
	for _, ctl := range []struct {
		name, from, to string
		red            []string
	}{
		{"damage-verdict", "    if report.damaged { return LoadRefuseWireDamaged; }\n", "",
			[]string{"cut_last", "empty", "fixed_bool_two", "fixed_dup_ids", "fixed_early_end", "form_only"}},
		{"form-reason", "        LoadRefuseUnknownForm => \"unknown_form\",\n", "",
			[]string{"form0", "form255", "form255_alone", "form2", "form2_alone", "form3", "form9"}},
		{"count-cap", "    if report.count_refused { return LoadRefuseCountOverExtentCap; }\n", "",
			[]string{"entries_cap31", "entries_cap32", "values_cap31", "values_cap32"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := loadReasons(t, sabotage(t, "wire_load.goose", ctl.from, ctl.to))
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
