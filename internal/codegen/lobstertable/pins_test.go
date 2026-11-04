package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// lobsterPinSchemas are the units of internal/codegen/goosetable/testdata/
// load_refusal_reasons.tsv, by its schema column.
var lobsterPinSchemas = map[string]string{
	"measure": "package probe\n table Child { values []int32 }\n table Root { values []int32\n entries map[int32]int32\n child Child\n next *Root }\n",
	"fixed":   "package probe\n fixed table Root { a int32\n flag bool\n small int8 | max = 5\n f float32 }\n",
	"float":   "package probe\n table Root { f float32\n values []int32 }\n",
}

// lobsterPin is one row of the shared pin table.
type lobsterPin struct {
	name, strict, form, measure string
	wire                        []byte
}

// pinRun reads one wire through the strict read, the tolerant read and the
// region measure, and prints "<strict>|<tolerant report>|<tolerant
// reason>|<measure size>,<whole>,<refused>".
const pinRun = `
def pinrun(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec, reason = schema_rt.load_strict(root, data, rep)
    let tol = schema_rt.LoadReport {}
    schema_rt.load(root, data, tol)
    let size, whole, refused = schema_rt.load_measure(root, data)
    print((if reason == "": "ok" else: reason) + "|" + report_line(tol) + "|" + tol.reason + "|" + "{size},{whole},{refused}")
`

// lobsterPinFault answers how Lobster's answer differs from one pin row, or ""
// when it agrees. The strict column is load_strict's reason. A row whose strict
// answer is count_over_extent_cap also holds the tolerant report: no counter
// moved, malformed unset, refused (§2.9). The measure column is held as its
// verdict, since load_measure answers a size or not and not why: a size is a
// whole read the framing does not refuse. The form column is the tolerant
// report's reason.
func lobsterPinFault(p lobsterPin, line string) string {
	f := strings.Split(line, "|")
	if len(f) != 4 {
		return fmt.Sprintf("the program printed %q", line)
	}
	var bad []string
	if f[0] != p.strict {
		bad = append(bad, "strict "+f[0]+", want "+p.strict)
	}
	if p.strict == "count_over_extent_cap" && f[1] != "0,0,0,0,0,false,refused" {
		bad = append(bad, "tolerant report "+f[1]+", want 0,0,0,0,0,false,refused")
	}
	if p.form != "-" && f[2] != p.form {
		bad = append(bad, "form "+f[2]+", want "+p.form)
	}
	m := strings.Split(f[3], ",")
	sized := len(m) == 3 && m[1] == "true" && m[2] == "false"
	if p.measure != "-" && (p.measure == "ok") != sized {
		bad = append(bad, "measure "+f[3]+", want "+p.measure)
	}
	return strings.Join(bad, "; ")
}

// lobsterPinRows reads the shared pin table, by its schema column.
func lobsterPinRows(t *testing.T) map[string][]lobsterPin {
	t.Helper()
	data, err := os.ReadFile("../goosetable/testdata/load_refusal_reasons.tsv")
	if err != nil {
		t.Fatal(err)
	}
	bySchema := map[string][]lobsterPin{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || line[0] == '#' {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 || lobsterPinSchemas[f[1]] == "" {
			t.Fatalf("a malformed pin row: %q", line)
		}
		var wire []byte
		if f[2] != "-" {
			if wire, err = hex.DecodeString(f[2]); err != nil {
				t.Fatal(err)
			}
		}
		bySchema[f[1]] = append(bySchema[f[1]], lobsterPin{name: f[0], strict: f[3], form: f[4], measure: f[5], wire: wire})
	}
	if len(bySchema) != len(lobsterPinSchemas) {
		t.Fatalf("the pin table names %d schemas, want %d", len(bySchema), len(lobsterPinSchemas))
	}
	return bySchema
}

// lobsterPinFaults runs one schema's rows, with edit applied to the emitted
// runtime when it is set, and answers each row's fault by name.
func lobsterPinFaults(t *testing.T, schema string, pins []lobsterPin, edit func(dir string)) map[string]string {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(lobsterPinSchemas[schema]), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the %s unit: %v", schema, err)
	}
	if edit != nil {
		edit(g.dir)
	}
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .region_measure\nimport .strict_and_refusals\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	b.WriteString(pinRun)
	inputs := map[string][]byte{}
	for i, p := range pins {
		name := fmt.Sprintf("w%d", i)
		inputs[name] = p.wire
		fmt.Fprintf(&b, "pinrun(%s, %q)\n", lobster.StringLit([]byte("Root")), name)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.SplitSeq(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n") {
		// the compiler's warnings about the names pinrun reads and does not
		// use
		if !strings.Contains(line, ": warning: ") {
			out = append(out, line)
		}
	}
	if len(out) != len(pins) {
		t.Fatalf("%d lines for %d rows: %q", len(out), len(pins), out)
	}
	faults := map[string]string{}
	for i, p := range pins {
		if fault := lobsterPinFault(p, out[i]); fault != "" {
			faults[p.name] = fault
		}
	}
	return faults
}

// TestLobsterLoadRefusalPins holds the Lobster load to every row of
// internal/codegen/goosetable/testdata/load_refusal_reasons.tsv, the table Go,
// Goose and MOO are held to (docs/SPEC-TABLES.md §2.9, §6.5): the strict read's
// one reason, the form refusal and the measure verdict. A `[]` or map count
// above the int32 storage cap refuses as count_over_extent_cap with no counter
// moved, a widening element kind included; of two refusals in one read, the
// first in read order names it.
func TestLobsterLoadRefusalPins(t *testing.T) {
	t.Parallel()
	for schema, pins := range lobsterPinRows(t) {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			for name, fault := range lobsterPinFaults(t, schema, pins, nil) {
				t.Errorf("%s: %s", name, fault)
			}
		})
	}
}

// TestLobsterLoadRefusalPinsNegativeControl plants two faults in the emitted
// runtime and requires each to turn exactly its own rows red: a widening
// counted before the cap check turns values_widened_over_cap red, and a load
// that never names the verdict's damage turns the wire_damaged rows red.
func TestLobsterLoadRefusalPinsNegativeControl(t *testing.T) {
	t.Parallel()
	rows := lobsterPinRows(t)
	plant := func(from, to string, times int) func(string) {
		return func(dir string) {
			path := filepath.Join(dir, "load.lobster")
			src, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if n := strings.Count(string(src), from); n != times {
				t.Fatalf("the emitted load.lobster holds %q %d times, want %d", from, n, times)
			}
			if err := os.WriteFile(path, []byte(strings.ReplaceAll(string(src), from, to)), 0o644); err != nil {
				t.Fatal(err)
			}
		}
	}
	for _, c := range []struct {
		name, schema string
		edit         func(string)
		want         []string
	}{
		{"widened", "measure", plant("        let widened = ek != f.ek\n", "        let widened = false\n        if ek != f.ek:\n            rep.widened++\n", 1),
			[]string{"values_widened_over_cap"}},
		{"damaged", "measure", plant("    if rep.damaged:\n        return \"wire_damaged\"\n", "", 1),
			[]string{"cut_last", "empty", "form_only"}},
		{"damaged", "fixed", plant("    if rep.damaged:\n        return \"wire_damaged\"\n", "", 1),
			[]string{"fixed_bool_two", "fixed_dup_ids", "fixed_early_end"}},
	} {
		t.Run(c.name+"/"+c.schema, func(t *testing.T) {
			t.Parallel()
			faults := lobsterPinFaults(t, c.schema, rows[c.schema], c.edit)
			var got []string
			for name := range faults {
				got = append(got, name)
			}
			sort.Strings(got)
			if strings.Join(got, ",") != strings.Join(c.want, ",") {
				t.Errorf("the %s control turned %v red, want %v", c.name, got, c.want)
			}
		})
	}
}
