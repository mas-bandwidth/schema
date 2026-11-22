package goosetable_test

// TestGooseCompressedF32EndsCorpus runs the cross-port corpus for A COMPRESSED
// FLOAT'S RANGE IS HELD AT FLOAT32 (docs/SPEC-TABLES.md §3.3) on the generated
// Goose: testdata/conformance/feature-witnesses/compressed-f32-ends. Every
// cases.tsv row runs in one program. The variable root V reads through the
// flat value table (load_table, json_load_table) and saves through
// save_table; a write row loads ends.json into the typed V, sets one float to
// the row's bits IN THE NATIVE VALUE and saves it through VToLoaded and
// save_table. The fixed root F reads and writes its typed record (FFixedLoad,
// FFromJson, FFixedSave). The program prints one line per row and the test
// holds each verdict, and each saved file byte for byte, to the row
// (internal/tablewire's TestCompressedF32EndsCorpus is the reference runner).
//
// Unit tests here use no real time, no socket and no network: the run is
// `goose run` of a generated program in t.TempDir().

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type gooseF32EndsRow struct {
	name, op, form, root, input, path, bits, expect string
}

func gooseF32EndsRows(t *testing.T, dir string) []gooseF32EndsRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(dir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []gooseF32EndsRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 8 {
			t.Fatalf("cases.tsv:%d: %d fields, want 8", i+1, len(f))
		}
		rows = append(rows, gooseF32EndsRow{f[0], f[1], f[2], f[3], f[4], f[5], f[6], f[7]})
	}
	return rows
}

const gooseF32EndsMain = `import os;
import .all;

fn hex(b: const u8[:]) -> u8[>..] {
    let digits = "0123456789abcdef";
    var line: u8[>..] = [];
    for x in b {
        line.push(digits[(x >> 4) as! i64]);
        line.push(digits[(x & 15) as! i64]);
    }
    if line.len == 0 { line.push('-'); }
    return line;
}

fn input(name: const u8[:]) -> u8[>..] {
    var data: u8[>..] = [];
    if !read_file(name, data) { write_stderr(str("probe: cannot read ", name, "\n")); exit(1); }
    return data;
}

// v_again saves a read V and says the verdict.
fn v_again(name: const u8[:], data: const u8[:], vals: schema_rt::LoadVal[>..]&, nodes: schema_rt::LoadNode[>..]&, root: i64) {
    var out: u8[>..<] = [];
    let r = schema_rt::save_table(f32ends::TableUnit, "V", data, vals, nodes, root, out);
    if r != 0 { print(name, " refused save:", schema_rt::save_refusal_name(r)); return; }
    print(name, " accepted ", hex(out));
}

fn read_v_bin(name: const u8[:], file: const u8[:]) {
    let data = input(file);
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var rep = schema_rt::LoadReport { .. };
    let root = schema_rt::load_table(f32ends::TableUnit, "V", data, vals, nodes, rep);
    if root < 0 || rep.body_damaged { print(name, " refused load"); return; }
    v_again(name, data, vals, nodes, root);
}

fn read_v_json(name: const u8[:], file: const u8[:]) {
    let text = input(file);
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var r = schema_rt::json_report();
    let root = schema_rt::json_load_table(f32ends::TableUnit, f32ends::TextUnit, "V", text, data, vals, nodes, r);
    if root < 0 || r.damaged { print(name, " refused json"); return; }
    v_again(name, data, vals, nodes, root);
}

fn f_again(name: const u8[:], v: const f32ends::F&) {
    var out: u8[>..] = [];
    let batch: f32ends::F[1] = [v];
    if f32ends::FFixedSave(batch, out) < 0 { print(name, " refused save"); return; }
    print(name, " accepted ", hex(out));
}

fn read_f_bin(name: const u8[:], file: const u8[:]) {
    let data = input(file);
    var values: f32ends::F[1] = [f32ends::NewF()];
    var rep = schema_rt::FixedReport { .. };
    if f32ends::FFixedLoad(values, data, rep) != 1 { print(name, " refused load"); return; }
    f_again(name, values[0]);
}

fn read_f_json(name: const u8[:], file: const u8[:]) {
    let text = input(file);
    var v = f32ends::NewF();
    var r = schema_rt::json_report();
    if !f32ends::FFromJson(v, text, r) || r.damaged { print(name, " refused json"); return; }
    f_again(name, v);
}

fn write_v(name: const u8[:], file: const u8[:], elem: i64, bits: u32) {
    let text = input(file);
    var v = f32ends::NewV();
    var r = schema_rt::json_report();
    if !f32ends::VFromJson(v, text, r) || r.damaged { print(name, " loadfail"); return; }
    if elem < 0 { v.R = schema_rt::fixed_bits_f32(bits); } else { v.Xs[elem] = schema_rt::fixed_bits_f32(bits); }
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    let root = f32ends::VToLoaded(v, f32ends::TableUnit, data, vals);
    if root < 0 { print(name, " refused pack"); return; }
    v_again(name, data, vals, nodes, root);
}

fn write_f(name: const u8[:], file: const u8[:], elem: i64, bits: u32) {
    let text = input(file);
    var v = f32ends::NewF();
    var r = schema_rt::json_report();
    if !f32ends::FFromJson(v, text, r) || r.damaged { print(name, " loadfail"); return; }
    if elem < 0 { v.R = schema_rt::fixed_bits_f32(bits); } else { v.Xs[elem] = schema_rt::fixed_bits_f32(bits); }
    f_again(name, v);
}

fn main() {
`

func TestGooseCompressedF32EndsCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it compiles and runs the generated unit)")
	corpus := filepath.Join(root, "testdata", "conformance", "feature-witnesses", "compressed-f32-ends")
	rows := gooseF32EndsRows(t, corpus)
	_, files := cookUnitFiles(t, []string{filepath.Join(corpus, "Probe.schema")}, nil)
	formOf := map[string]string{"V": "variable", "F": "fixed"}
	// Every file a row names goes into the run, read off cases.tsv.
	for _, r := range rows {
		name := r.input + ".json"
		if r.op == "read" && r.form != "json" {
			name = r.input + "." + r.form + ".bin"
		}
		b, err := os.ReadFile(filepath.Join(corpus, name))
		if err != nil {
			t.Fatal(err)
		}
		files[name] = b
	}
	var b strings.Builder
	b.WriteString(gooseF32EndsMain)
	for _, r := range rows {
		if formOf[r.root] == "" || (r.form != "json" && formOf[r.root] != r.form) {
			t.Fatalf("%s: form %s under root %s", r.name, r.form, r.root)
		}
		root := strings.ToLower(r.root)
		switch {
		case r.op == "read" && r.form == "json":
			fmt.Fprintf(&b, "    read_%s_json(%q, %q);\n", root, r.name, r.input+".json")
		case r.op == "read":
			fmt.Fprintf(&b, "    read_%s_bin(%q, %q);\n", root, r.name, r.input+"."+r.form+".bin")
		case r.op == "write":
			elem := -1
			switch r.path {
			case "r":
			case "xs[0]":
				elem = 0
			default:
				t.Fatalf("%s: path %q", r.name, r.path)
			}
			fmt.Fprintf(&b, "    write_%s(%q, %q, %d, 0x%s);\n", root, r.name, r.input+".json", elem, r.bits)
		default:
			t.Fatalf("%s: op %q", r.name, r.op)
		}
	}
	b.WriteString("}\n")
	files["main.goose"] = []byte(b.String())
	out := string(gooserun.Run(t, t.TempDir(), "main.goose", files))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		if w := strings.Fields(line); len(w) >= 2 {
			got[w[0]] = w[1:]
		}
	}
	for _, r := range rows {
		t.Run(r.name, func(t *testing.T) {
			g, ok := got[r.name]
			if !ok {
				t.Fatalf("the program printed no verdict for this row:\n%s", out)
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
