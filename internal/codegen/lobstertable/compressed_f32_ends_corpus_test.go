package lobstertable_test

// TestLobsterCompressedF32EndsCorpus runs the cross-port corpus for A
// COMPRESSED FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3) on the
// generated Lobster: testdata/conformance/feature-witnesses/compressed-f32-ends.
// Every cases.tsv row runs in one program through the runtime's own entry
// points: a read row loads the variable or fixed file (load_strict, which
// dispatches the fixed root F to its typed record codec) or the JSON
// (json_read) and, where it owes acceptance, saves the value again in the
// root's own form (save_reasoned); a write row reads ends.json, sets one float
// to the row's bits in the loaded record and saves the row's form. The
// program prints one line per row and the test holds each verdict, and each
// saved file byte for byte, to the row (internal/tablewire's
// TestCompressedF32EndsCorpus is the reference runner).

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type lobsterF32EndsRow struct {
	name, op, form, root, input, path, bits, expect string
}

const lobsterF32EndsCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-f32-ends"

func lobsterF32EndsRows(t *testing.T) []lobsterF32EndsRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(lobsterF32EndsCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []lobsterF32EndsRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", i+1, len(f))
		}
		rows = append(rows, lobsterF32EndsRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return rows
}

const lobsterF32EndsBody = `
def f32of(bits:int) -> float:
    return read_float32_le(write_int32_le(schema_rt.zeros(4), 0, bits), 0)

def again(name:string, back:schema_rt.Record?, reason:string):
    if back:
        let w, why = schema_rt.save_reasoned(back)
        if why != "":
            print(name + " refused save:" + why)
        else:
            print(name + " accepted " + fhex(w))
    else:
        print(name + " refused " + reason)

def read_bin(name:string, root:string, file:string):
    let data = read_file(file)
    assert data
    let back, reason = schema_rt.load_strict(root, data, schema_rt.LoadReport {})
    again(name, back, reason)

def read_json(name:string, root:string, file:string):
    let text = read_file(file)
    assert text
    let back = schema_rt.json_read(root, text, schema_rt.LoadReport {})
    again(name, back, "json")

def write(name:string, root:string, file:string, elem:int, bits:int):
    let text = read_file(file)
    assert text
    let rec = schema_rt.json_read(root, text, schema_rt.LoadReport {})
    if not rec:
        print(name + " loadfail")
        return
    if elem < 0:
        fslot(rec, "r").cell.f = f32of(bits)
    else:
        fslot(rec, "xs").elems[elem].f = f32of(bits)
    let w, why = schema_rt.save_reasoned(rec)
    if why != "":
        print(name + " refused " + why)
    else:
        print(name + " accepted " + fhex(w))
`

func TestLobsterCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	corpus, err := filepath.Abs(lobsterF32EndsCorpusDir)
	if err != nil {
		t.Fatal(err)
	}
	rows := lobsterF32EndsRows(t)
	g, err := generateLoadUnit(t, []string{filepath.Join(corpus, "Probe.schema")})
	if err != nil {
		t.Fatalf("the surface refuses the corpus unit: %v", err)
	}
	inputs := map[string][]byte{}
	for _, stem := range []string{"ends", "past-max", "past-min"} {
		for _, ext := range []string{".json", ".variable.bin", ".fixed.bin"} {
			b, err := os.ReadFile(filepath.Join(corpus, stem+ext))
			if err != nil {
				t.Fatal(err)
			}
			inputs[stem+ext] = b
		}
	}
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	var b strings.Builder
	b.WriteString("import .json\n")
	b.WriteString(lobsterF32EndsBody)
	for _, r := range rows {
		if formOf[r.root] == "" || (r.form != "json" && formOf[r.root] != r.form) {
			t.Fatalf("%s: form %s under root %s", r.name, r.form, r.root)
		}
		switch {
		case r.op == "read" && r.form == "json":
			fmt.Fprintf(&b, "read_json(%q, %q, %q)\n", r.name, r.root, r.input+".json")
		case r.op == "read":
			fmt.Fprintf(&b, "read_bin(%q, %q, %q)\n", r.name, r.root, r.input+"."+r.form+".bin")
		case r.op == "write":
			elem := -1
			switch r.path {
			case "r":
			case "xs[0]":
				elem = 0
			default:
				t.Fatalf("%s: path %q", r.name, r.path)
			}
			fmt.Fprintf(&b, "write(%q, %q, %q, %d, 0x%s)\n", r.name, r.root, r.input+".json", elem, r.bits)
		default:
			t.Fatalf("%s: op %q", r.name, r.op)
		}
	}
	out := finiteRun(t, g, b.String(), inputs)
	got := map[string][]string{}
	for _, line := range out {
		if w := strings.Fields(line); len(w) >= 2 {
			got[w[0]] = w[1:]
		}
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			g, ok := got[r.name]
			if !ok {
				t.Fatalf("the program printed no verdict for this row:\n%s", strings.Join(out, "\n"))
			}
			if g[0] == "loadfail" {
				t.Fatalf("%s.json: the reader refuses it", r.input)
			}
			if r.expect == "refused" {
				if g[0] != "refused" {
					t.Errorf("not refused: %s", strings.Join(g, " "))
				}
				return
			}
			if g[0] != "accepted" || len(g) != 2 {
				t.Fatalf("refused: %s", strings.Join(g, " "))
			}
			form := formOf[r.root]
			want, err := os.ReadFile(filepath.Join(corpus, r.expect+"."+form+".bin"))
			if err != nil {
				t.Fatal(err)
			}
			if have, err := hex.DecodeString(g[1]); err != nil || !bytes.Equal(have, want) {
				t.Errorf("the %s save:\n have %s\n want %s", form, g[1], hex.EncodeToString(want))
			}
		})
	}
}
