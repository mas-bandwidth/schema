package rusttable

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// THE COMPRESSED -0 WRITER CORPUS ON THE RUST LEG (docs/SPEC-TABLES.md §3.3,
// docs/rulings.json fixed-compressed-negzero): a writer stores +0 for a
// compressed float's -0. The corpus is the Go reference engine's
// (testdata/conformance/feature-witnesses/compressed-negzero-go, pinned by
// internal/tablewire's TestCompressedNegZeroGoCorpus); this leg runs every row
// through the generated Rust crate: load <base>.json with the Rust text read,
// set <path> to -0 in the native value, save <form>, and owe
// <expect>.<form>.bin byte for byte. The base saved untouched owes
// <base>.<form>.bin: the leg writes the engine's bytes.
//
// Beside each row, every other Rust writer the row's root has is held to the
// row's +0 twin (the same base saved untouched): the variable save and its
// retaining twin, the fixed save and the message form (a fixed table's), the
// JSON writer and the cook writer. A compressed row's -0 output equals its
// twin's; the plain `p` rows are the control, whose -0 rides bit-exact, so
// their output differs from the twin's.
//
// The probe is built with -O, so no debug assert is on: the normalisation is
// held in the build a shipping crate uses, never by an assert. It runs rustc,
// so it is a slow test; the conformance lane runs it
// (make/rust-compressed-negzero.mk). TestRustCompressedFloatStoresAreGuarded is
// its fast twin.

const rustNegZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

type rustNegZeroRow struct {
	name, form, root, base, path, expect string
}

func rustNegZeroRows(t *testing.T) []rustNegZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(rustNegZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []rustNegZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, rustNegZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	if len(rows) != 15 {
		t.Fatalf("cases.tsv holds %d rows, want 15", len(rows))
	}
	return rows
}

// rustNegZeroWriters are the writers each root is held on.
var rustNegZeroWriters = map[string][]string{
	"Var": {"variable", "retain", "json", "cook"},
	"Fix": {"variable", "retain", "fixed", "message", "json", "cook"},
}

// rustNegZeroSet is the Rust statement that sets the cell a corpus path names
// on the value `v` to `nz`: `field`, `field[index]`, `field[Variant]` for an
// enum-keyed slot (key k is slot k-1; Level is the corpus's one key), or
// `field.arm` for a union's set arm (Pick is the corpus's one union).
func rustNegZeroSet(path string) string {
	if name, index, ok := strings.Cut(path, "["); ok {
		index = strings.TrimSuffix(index, "]")
		if index[0] < '0' || index[0] > '9' {
			index = "Level::" + index + " as usize - 1"
		}
		return "v." + name + "[" + index + "] = nz;"
	}
	if name, arm, ok := strings.Cut(path, "."); ok {
		return "v." + name + " = Pick::" + strings.ToUpper(arm[:1]) + arm[1:] + "(nz);"
	}
	return "v." + path + " = nz;"
}

const rustNegZeroPrelude = `#![allow(dead_code, unused_imports)]
mod nz;
use nz::*;

fn hex(b: &[u8]) -> String {
    b.iter().map(|c| format!("{c:02x}")).collect()
}

fn fresh_retain() -> TableRetain {
    TableRetain { bytes: Vec::with_capacity(1 << 16), ids: Vec::with_capacity(256), nodes: Default::default() }
}

fn var_out(w: &str, v: &Var, base: &[u8]) -> String {
    let out: Option<Vec<u8>> = match w {
        "variable" => var_save_checked(v).ok(),
        "retain" => {
            let mut retain = fresh_retain();
            let mut r = TableReport::default();
            var_load_retain(base, &mut retain, &mut r).map(|_| var_save_retain(v, &mut retain, &mut TableReport::default()))
        }
        "json" => var_to_json_checked(v).map(|s| s.into_bytes()),
        "cook" => var_cook_write(v, 1).ok(),
        _ => None,
    };
    out.map(|b| hex(&b)).unwrap_or_else(|| "error".to_string())
}

fn fix_out(w: &str, v: &Fix, base: &[u8]) -> String {
    let out: Option<Vec<u8>> = match w {
        "variable" => fix_save_checked(v).ok(),
        "retain" => {
            let mut retain = fresh_retain();
            let mut r = TableReport::default();
            fix_load_retain(base, &mut retain, &mut r).map(|_| fix_save_retain(v, &mut retain, &mut TableReport::default()))
        }
        "fixed" => {
            let rows = [FixRow::from(v)];
            let mut buf = vec![0u8; fix_fixed_measure(1)];
            fix_fixed_save(&rows, &mut buf).map(|n| buf[..n].to_vec())
        }
        "message" => {
            let rows = [FixRow::from(v)];
            let mut r = TableReport::default();
            let n = fix_measure_messages(&rows, &mut r);
            if n <= 0 {
                None
            } else {
                let mut buf = vec![0u8; n as usize];
                if fix_save_messages(&rows, &mut buf, &mut r) == n { Some(buf) } else { None }
            }
        }
        "json" => fix_to_json_checked(v).map(|s| s.into_bytes()),
        "cook" => fix_cook_write(v, 1).ok(),
        _ => None,
    };
    out.map(|b| hex(&b)).unwrap_or_else(|| "error".to_string())
}

fn main() {
    let nz = f32::from_bits(0x8000_0000);
`

// rustNegZeroCrate lays the generated table modules out as the module `nz`
// beside main.rs: every module the generator emits, the fixed records'
// value conversions included, each re-exported as the crate's lib.rs does.
func rustNegZeroCrate(t *testing.T, dir string, modules map[string][]byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Join(dir, "nz"), 0o755); err != nil {
		t.Fatal(err)
	}
	var names []string
	for file := range modules {
		if strings.HasSuffix(file, ".rs") && file != "lib.rs" {
			names = append(names, file)
		}
	}
	sort.Strings(names)
	var mod strings.Builder
	for _, file := range names {
		body := bytes.ReplaceAll(modules[file], []byte("use crate::*;"), []byte("use super::*;"))
		if err := os.WriteFile(filepath.Join(dir, "nz", file), body, 0o600); err != nil {
			t.Fatal(err)
		}
		m := strings.TrimSuffix(file, ".rs")
		fmt.Fprintf(&mod, "#[allow(dead_code, unused_imports)]\npub mod %s;\n#[allow(unused_imports)]\npub use %s::*;\n", m, m)
	}
	if err := os.WriteFile(filepath.Join(dir, "nz", "mod.rs"), []byte(mod.String()), 0o600); err != nil {
		t.Fatal(err)
	}
}

// TestRustCompressedNegZeroCorpus runs the corpus on the Rust leg.
func TestRustCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc (it compiles and runs the compressed -0 writer corpus)")
	keyedRequireRustc(t)
	src, err := os.ReadFile(filepath.Join(rustNegZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	modules, err := Generate(unitFrom(t, string(src)))
	if err != nil {
		t.Fatal(err)
	}
	read := func(name string) []byte {
		t.Helper()
		data, err := os.ReadFile(filepath.Join(rustNegZeroCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		return data
	}

	rows := rustNegZeroRows(t)
	var b strings.Builder
	b.WriteString(rustNegZeroPrelude)
	for _, r := range rows {
		writers := rustNegZeroWriters[r.root]
		if writers == nil {
			t.Fatalf("%s: root %s", r.name, r.root)
		}
		if r.form != "variable" && (r.form != "fixed" || r.root != "Fix") {
			t.Fatalf("%s: form %s at %s", r.name, r.form, r.root)
		}
		lower := strings.ToLower(r.root)
		json := strings.TrimSpace(string(read(r.base + ".json")))
		fmt.Fprintf(&b, "    { // %s\n", r.name)
		fmt.Fprintf(&b, "        let base = %s_from_json(%q, &mut TableReport::default()).expect(%q);\n", lower, json, r.name+": the base does not load")
		fmt.Fprintf(&b, "        let saved = %s_save_checked(&base).expect(%q);\n", lower, r.name+": the base does not save")
		b.WriteString("        let mut v = base.clone();\n")
		fmt.Fprintf(&b, "        %s\n", rustNegZeroSet(r.path))
		fmt.Fprintf(&b, "        for w in [\"%s\"] {\n", strings.Join(writers, `", "`))
		fmt.Fprintf(&b, "            println!(\"%s {} {} {}\", w, %s_out(w, &v, &saved), %s_out(w, &base, &saved));\n", r.name, lower, lower)
		b.WriteString("        }\n    }\n")
	}
	b.WriteString("}\n")
	main := b.String()

	dir := t.TempDir()
	rustNegZeroCrate(t, dir, modules)
	if err := os.WriteFile(filepath.Join(dir, "main.rs"), []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	build := testproc.Command(t, "rustc", "--edition", "2024", "--cfg", `feature="cook"`, "-O", "-A", "warnings", "-o", "run", "main.rs")
	build.Dir = dir
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("native Rust compile: %v\n%s", err, out)
	}
	run := testproc.Command(t, filepath.Join(dir, "run"))
	run.Dir = dir
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}

	type pair struct{ minus, plus string }
	got := map[string]map[string]pair{}
	for line := range strings.SplitSeq(strings.TrimRight(string(out), "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) != 4 {
			t.Fatalf("the probe printed %q, not a row, a writer and two outputs", line)
		}
		if got[f[0]] == nil {
			got[f[0]] = map[string]pair{}
		}
		got[f[0]][f[1]] = pair{f[2], f[3]}
	}

	held, twins := 0, 0
	for _, r := range rows {
		ws := got[r.name]
		if len(ws) != len(rustNegZeroWriters[r.root]) {
			t.Errorf("%s: the probe answered %d writers, want %d", r.name, len(ws), len(rustNegZeroWriters[r.root]))
			continue
		}
		plain := r.path == "p"
		// THE CORPUS ROW: the -0 save of <form> is <expect>.<form>.bin, and
		// the +0 base's is <base>.<form>.bin
		p := ws[r.form]
		want := hex.EncodeToString(read(r.expect + "." + r.form + ".bin"))
		wantBase := hex.EncodeToString(read(r.base + "." + r.form + ".bin"))
		switch {
		case p.plus != wantBase:
			t.Errorf("%s: the Rust %s save of the base is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.base, r.form, p.plus, wantBase)
		case p.minus != want:
			t.Errorf("%s: the Rust %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.path, r.expect, r.form, p.minus, want)
		default:
			held++
		}
		// THE TWINS: every writer the root has
		for _, w := range rustNegZeroWriters[r.root] {
			p := ws[w]
			if p.minus == "error" || p.plus == "error" {
				t.Errorf("%s/%s: a writer refused: -0 %s, +0 %s", r.name, w, p.minus, p.plus)
				continue
			}
			switch {
			case plain && p.minus == p.plus:
				t.Errorf("%s/%s: a plain float's -0 did not ride: %s", r.name, w, p.minus)
			case !plain && p.minus != p.plus:
				t.Errorf("%s/%s: a compressed float's -0 was not written as +0:\n -0 %s\n +0 %s", r.name, w, p.minus, p.plus)
			default:
				twins++
			}
		}
	}
	t.Logf("%d of %d corpus rows held; %d writer twins held", held, len(rows), twins)
}
