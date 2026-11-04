package lobstertable_test

import (
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
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

// The shared corpus supplies bytes; only the strict reference verdict and
// complete values are compared. Diagnostic reason columns are intentionally local.
const pinRun = `
def pinrun(root:string, file:string):
    let data = read_file(file)
    assert data
    let rec = schema_rt.load(root, data, schema_rt.LoadReport {})
    if not rec:
        print("refused|nil")
        return
    labels = []
    push(labels, rec)
    print("read|" + drec(rec))
`

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
func lobsterPinFaults(t *testing.T, schema string, pins []lobsterPin, sabotage bool) (map[string]string, int) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(lobsterPinSchemas[schema]), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the %s unit: %v", schema, err)
	}
	if sabotage {
		path := filepath.Join(g.dir, "load.lobster")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		const from = "            read_refuse(report, \"wire_damaged\")\n        return nil\n    return rec"
		const to = "            read_refuse(report, \"wire_damaged\")\n        return new_record(table_of(root))\n    return rec"
		const fixedFrom = "        return fixed_record_codec(root).read_fixed(data, report)"
		const fixedTo = "        let rec = fixed_record_codec(root).read_fixed(data, report)\n        if not rec:\n            return new_record(table_of(root))\n        return rec"
		if strings.Count(string(src), from) != 1 || strings.Count(string(src), fixedFrom) != 1 {
			t.Fatal("single read refusal boundary missing")
		}
		mutant := strings.Replace(string(src), from, to, 1)
		mutant = strings.Replace(mutant, fixedFrom, fixedTo, 1)
		if err := os.WriteFile(path, []byte(mutant), 0o644); err != nil {
			t.Fatal(err)
		}
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
	refused := 0
	for i, p := range pins {
		verdict, value := goLoad(g.u, "Root", p.wire)
		if verdict == "refused" {
			refused++
		}
		want := verdict + "|" + value
		if out[i] != want {
			faults[p.name] = fmt.Sprintf("got %s, want %s", out[i], want)
		}
	}
	return faults, refused
}

// Every existing reason-pin wire remains covered after reason comparison retires.
func TestLobsterLoadRefusalPins(t *testing.T) {
	t.Parallel()
	for schema, pins := range lobsterPinRows(t) {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			faults, _ := lobsterPinFaults(t, schema, pins, false)
			for name, fault := range faults {
				t.Errorf("%s: %s", name, fault)
			}
		})
	}
}

// Returning a default value on refusal must fail the unchanged value/verdict oracle.
func TestLobsterLoadRefusalPinsNegativeControl(t *testing.T) {
	t.Parallel()
	for schema, pins := range lobsterPinRows(t) {
		t.Run(schema, func(t *testing.T) {
			t.Parallel()
			faults, refused := lobsterPinFaults(t, schema, pins, true)
			if refused == 0 || len(faults) != refused {
				t.Fatalf("default-on-refusal sabotage changed %d rows, want %d refused rows", len(faults), refused)
			}
		})
	}
}
