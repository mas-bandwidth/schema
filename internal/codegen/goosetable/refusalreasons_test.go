package goosetable_test

// THE LOAD'S VERDICTS, for Goose, under the one strict read
// (docs/SPEC-TABLES.md §6.5, §2.9, §3, §4; docs/plans/strict-only.md): every
// row of testdata/load_refusal_reasons.tsv whose strict column is "ok" loads,
// and every other row is refused, no root. The verdict is all this test pins:
// which reason a refusal names is the leg's own, never compared across
// languages.
//
//	TestGooseLoadRefusalReasons  every pin row's verdict
//	TestGooseLoadRefusalReasonsNegativeControl  takes out, in turn, the
//	    wire walk (wire_check), the end-of-load refusal (load_error) and the
//	    count cap's refusal; each must turn exactly its own rows red

import (
	"fmt"
	"os"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
)

// reasonRow is one pinned file.
type reasonRow struct{ name, schema, wire, strict, form string }

// reasonSchemas are the pin table's two units, as its header states them.
var reasonSchemas = map[string]string{
	"measure": "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n",
	"fixed":   "package probe\n fixed table Root { a int32\n flag bool\n small int8 | max = 5\n f float32 }\n",
	"float":   "package probe\n table Root { f float32\n values []int32 }\n",
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
    if root < 0 { write_stdout(str(name, " refused\n")); } else { write_stdout(str(name, " ok\n")); }
}

fn main() {
`

// loadReasons is the gate's red list: one line per row whose Goose answer is
// not the pin.
func loadReasons(t *testing.T, override map[string][]byte) []string {
	t.Helper()
	rows := reasonRows(t)
	var bad []string
	for _, schema := range []string{"measure", "fixed", "float"} {
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
			if len(g) != 1 {
				t.Fatalf("%s: the probe printed %q", r.name, g)
			}
			want := "refused"
			if r.strict == "ok" {
				want = "ok"
			}
			if g[0] != want {
				bad = append(bad, fmt.Sprintf("%s: %s; the strict read %s", r.name, g[0], want))
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
	for _, want := range []string{"ok", "wire_damaged", "malformed", "unknown_form", "count_over_extent_cap", "kind_mismatch", "out_of_range", "non_finite_float"} {
		if !reasons[want] {
			t.Errorf("no pin row answers %s", want)
		}
	}
	for _, b := range loadReasons(t, nil) {
		t.Error(b)
	}
}

// sabotageAll applies each from/to pair to one runtime file, failing if any
// from is not there exactly once.
func sabotageAll(t *testing.T, file string, pairs ...string) map[string][]byte {
	t.Helper()
	src := string(goosetable.Runtime()[file])
	for i := 0; i+1 < len(pairs); i += 2 {
		if n := strings.Count(src, pairs[i]); n != 1 {
			t.Fatalf("sabotage: %s holds %q %d times, want once", file, pairs[i], n)
		}
		src = strings.Replace(src, pairs[i], pairs[i+1], 1)
	}
	return map[string][]byte{file: []byte(src)}
}

const (
	ctlWireWalk  = "    let wfail, wnon = wire_check(file, 1, first, first, count as! i64, false);\n"
	ctlNoWalk    = "    let wfail: i64 = 0;\n    let wnon = false;\n"
	ctlBodyDmg   = "    if report.body_damaged {\n        report.refused = true;\n        report.reason = LoadRefuseMalformed;\n        return -1;\n    }\n"
	ctlEndRefuse = "    let refusal = load_error(report, c.root_val);\n"
	ctlEndOk     = "    let refusal = LoadRefuseOk;\n"
)

// TestGooseLoadRefusalReasonsNegativeControl: each control takes refusals out
// of the runtime and names exactly the rows they alone hold.
//
//   - end-of-load: the strict refusal at the end of the load (load_error)
//     answers ok; the rows a counter alone refuses (a value out of range, a
//     kind this reader cannot land) load;
//   - damage: the wire walk, the body-damage return and load_error together
//     stop refusing; every refused row loads but those the load's prologue
//     refuses (no bytes, a form byte alone, a cut framing, a form this reader
//     does not carry, a fixed root's early end or repeated id) and the
//     non-finite floats, which the read refuses on its own (§3.3).
func TestGooseLoadRefusalReasonsNegativeControl(t *testing.T) {
	t.Parallel()
	for _, ctl := range []struct {
		name  string
		pairs []string
		red   []string
	}{
		{"end-of-load", []string{ctlEndRefuse, ctlEndOk},
			[]string{"fixed_clamped", "fixed_kind_mismatch", "values_f32"}},
		{"damage", []string{ctlWireWalk, ctlNoWalk, ctlBodyDmg, "", ctlEndRefuse, ctlEndOk},
			[]string{"entries_cap31", "entries_cap32", "entries_over_length", "fixed_bool_two", "fixed_clamped",
				"fixed_cut_payload", "fixed_kind_mismatch", "over_cap_then_nan", "values_cap31", "values_cap32",
				"values_f32", "values_over_length", "values_widened_over_cap"}},
	} {
		t.Run(ctl.name, func(t *testing.T) {
			t.Parallel()
			bad := loadReasons(t, sabotageAll(t, "wire_load.goose", ctl.pairs...))
			var red []string
			for _, b := range bad {
				name, _, _ := strings.Cut(b, ":")
				red = append(red, name)
			}
			slices.Sort(red)
			want := slices.Clone(ctl.red)
			slices.Sort(want)
			if !slices.Equal(red, want) {
				t.Fatalf("%s: red rows %v, want exactly %v", ctl.name, red, want)
			}
			t.Logf("%s: %d rows red", ctl.name, len(bad))
		})
	}
}
