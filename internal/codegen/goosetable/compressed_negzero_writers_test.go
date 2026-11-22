package goosetable_test

// A COMPRESSED FLOAT'S -0 IS WRITTEN AS +0 BY EVERY GOOSE WRITER
// (docs/SPEC-TABLES.md §3.3, the special values; docs/rulings.json
// fixed-compressed-negzero): the variable save and its retaining twin, the
// fixed save, the message form (whose elision must not count a -0 as off its
// +0 default), the typed and the region cook writers, and
// the typed and the value text writers. A plain float's -0 rides bit-exact
// (the control). The value is normalised in every build: this is not a range
// check, so it is not a debug-only assert.
//
// TestGooseCompressedFloatStoresAreGuarded is the fast half: it reads the
// generated source and the runtime rather than running them, and holds every
// compressed-float store to the +0 guard and the plain float's store away
// from it. TestGooseCompressedNegZeroCorpus is the functional half, run in
// the Goose conformance lane (make/goose-compressed-negzero.mk): it runs the
// compressed -0 writer corpus (testdata/conformance/feature-witnesses/
// compressed-negzero-go) through the generated Goose code.

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goosetable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// negZeroCorpusDir is the compressed -0 writer corpus the Go reference engine
// pins (internal/tablewire TestCompressedNegZeroGoCorpus).
const negZeroCorpusDir = "../../../testdata/conformance/feature-witnesses/compressed-negzero-go"

func negZeroCorpusUnit(t *testing.T) *ir.Unit {
	t.Helper()
	src, err := os.ReadFile(filepath.Join(negZeroCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	return probeUnit(t, string(src))
}

func TestGooseCompressedFloatStoresAreGuarded(t *testing.T) {
	t.Parallel()
	files, err := goosetable.Generate(negZeroCorpusUnit(t))
	if err != nil {
		t.Fatal(err)
	}
	// THE GENERATED STORES: the fixed save puts every float through
	// fixed_f32_bits or fixed_cf32_bits, and the typed cook writer through
	// accel_f32_bits or accel_cf32_bits. The plain float `p` is the one field
	// that takes the bare spelling.
	store := regexp.MustCompile(`(fixed_put\(out, schema_rt::fixed_c?f32_bits|let xb\d* = schema_rt::accel_c?f32_bits)\(([^)]*)\)`)
	plainOf := func(arg string) bool { return strings.HasSuffix(arg, ".P") }
	seen := map[string][2]int{} // spelling -> guarded, plain
	for name, data := range files {
		for i, line := range strings.Split(string(data), "\n") {
			m := store.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			kind := "fixed save"
			if strings.Contains(m[1], "accel") {
				kind = "typed cook"
			}
			guarded := strings.Contains(m[1], "cf32_bits")
			plain := plainOf(m[2])
			n := seen[kind]
			switch {
			case plain && guarded:
				t.Errorf("%s:%d: the plain float's -0 is normalised: %s", name, i+1, strings.TrimSpace(line))
			case plain:
				n[1]++
			case !guarded:
				t.Errorf("%s:%d: a compressed float is stored as held: %s", name, i+1, strings.TrimSpace(line))
			default:
				n[0]++
			}
			seen[kind] = n
		}
	}
	// THE MESSAGE FORM quantizes a compressed float's -0 to the index of 0,
	// but its elision asks "not the declared default" first: a compressed
	// float asks it by value (-0 against a +0 default does not ride), and the
	// plain float by its bits (its -0 rides).
	bitsDiffer := regexp.MustCompile(`schema_rt::fixed_f32_bits\(v\.([A-Za-z0-9_.\[\]]+)\) != schema_rt::fixed_f32_bits\(d\.`)
	valueDiffer := regexp.MustCompile(`if v\.Z != d\.Z \{`)
	var plainBits, valueCompare int
	for name, data := range files {
		for i, line := range strings.Split(string(data), "\n") {
			if valueDiffer.MatchString(line) {
				valueCompare++
			}
			m := bitsDiffer.FindStringSubmatch(line)
			if m == nil {
				continue
			}
			if m[1] == "P" {
				plainBits++
				continue
			}
			t.Errorf("%s:%d: the message form elides a compressed float by its bits, so its -0 rides: %s", name, i+1, strings.TrimSpace(line))
		}
	}
	if plainBits == 0 || valueCompare == 0 {
		t.Errorf("the message form has %d value elisions of z and %d bit elisions of p; want both", valueCompare, plainBits)
	}
	for _, kind := range []string{"fixed save", "typed cook"} {
		if n := seen[kind]; n[0] == 0 || n[1] == 0 {
			t.Errorf("the %s has %d guarded compressed stores and %d plain ones; want both", kind, n[0], n[1])
		}
	}
	// THE RUNTIME: the helpers the generated stores call, the region cook
	// writer, the value save (both the plain save and the retaining one put
	// through put_value) and the text writer.
	for file, wants := range map[string][]string{
		"fixed.goose":      {"fn fixed_cf32_bits(x: f32) -> u32 {\n    if x == 0.0 { return 0; }\n    return fixed_f32_bits(x);\n}"},
		"block_form.goose": {"fn accel_cf32_bits(x: f32) -> u64 {\n    if x == 0.0 { return 0; }\n    return accel_f32_bits(x);\n}"},
		"cook_write.goose": {"if d.frange && (bits == 0x80000000 && d.is_f32 || bits == 0x8000000000000000 && !d.is_f32) { bits = 0; }\n        cook_put_at(c, at, bits, col.w);"},
		"wire_save.goose":  {"if x == 0.0 { v = Int128 { lo: 0, hi: 0 }; }"},
		"schema_json.goose": {
			"fn json_write_float(out: u8[>..]&, v: f32, f: const FieldDesc&) -> bool {\n    var x = v;\n    if f.frange && json_finite(x) {\n        if !frange_holds(x, f) { return false; }\n        if x == 0.0 { x = 0.0; }",
			"fn json_write_float(out: u8[>..]&, v: f64, f: const FieldDesc&) -> bool {\n    var x = v;\n    if f.frange && json_finite(x) {\n        if !frange_holds(x, f) { return false; }\n        if x == 0.0 { x = 0.0; }",
		},
	} {
		src, err := os.ReadFile(filepath.Join("runtime", file))
		if err != nil {
			t.Fatal(err)
		}
		for _, want := range wants {
			if !strings.Contains(string(src), want) {
				t.Errorf("runtime/%s does not carry the compressed -0 guard:\n%s", file, want)
			}
		}
	}
}

// negZeroRow is one cases.tsv row.
type negZeroRow struct {
	name, form, root, base, path, expect string
}

func negZeroRows(t *testing.T) []negZeroRow {
	t.Helper()
	text, err := os.ReadFile(filepath.Join(negZeroCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []negZeroRow
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		rows = append(rows, negZeroRow{f[0], f[1], f[2], f[3], f[4], f[5]})
	}
	return rows
}

// negZeroGoosePath is the Goose spelling of a corpus path on a value `v`:
// `field`, `field[index]`, `field[Variant]` (the variant's slot) or
// `field.arm`.
func negZeroGoosePath(t *testing.T, u *ir.Unit, root, path string) string {
	t.Helper()
	upper := func(s string) string { return strings.ToUpper(s[:1]) + s[1:] }
	if name, arm, ok := strings.Cut(path, "."); ok {
		return "." + upper(name) + "." + upper(arm)
	}
	name, index, ok := strings.Cut(path, "[")
	if !ok {
		return "." + upper(name)
	}
	index = strings.TrimSuffix(index, "]")
	if _, err := strconv.Atoi(index); err != nil {
		st := u.Tables[root]
		slot := -1
		for _, f := range st.Fields {
			if f.Name == name && f.KeyEnum != "" {
				for i, v := range u.Enums[f.KeyEnum].Variants {
					if v == index {
						slot = i
					}
				}
			}
		}
		if slot < 0 {
			t.Fatalf("%s: no keyed slot %s on %s", path, index, root)
		}
		index = strconv.Itoa(slot)
	}
	return "." + upper(name) + "[" + index + "]"
}

const negZeroCorpusMain = `
fn negz() -> f32 { return schema_rt::fixed_bits_f32(0x80000000); }

fn var_save(v: const nzgo::Var&) -> u8[>..] {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var out: u8[>..<] = [];
    let at = nzgo::VarToLoaded(v, conformance_unit(), data, vals);
    if at < 0 || schema_rt::save_table(conformance_unit(), "Var", data, vals, nodes, at, out) != 0 { write_stdout("bad var_save\n"); exit(1); }
    var r: u8[>..] = [];
    for b in out { r.push(b); }
    return r;
}

fn fix_save(v: const nzgo::Fix&) -> u8[>..] {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var out: u8[>..<] = [];
    let at = nzgo::FixToLoaded(v, conformance_unit(), data, vals);
    if at < 0 || schema_rt::save_table(conformance_unit(), "Fix", data, vals, nodes, at, out) != 0 { write_stdout("bad fix_save\n"); exit(1); }
    var r: u8[>..] = [];
    for b in out { r.push(b); }
    return r;
}

fn var_retain(v: const nzgo::Var&) -> u8[>..] {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var out: u8[>..<] = [];
    var bytes: u8[>..<] = [];
    var recs: schema_rt::RetainRec[>..<] = [];
    var ids: u64[>..<] = [];
    var store = schema_rt::RetainStore { capacity: -1, id_capacity: -1, used: 0, bytes: bytes, recs: recs, ids: ids };
    var report = schema_rt::LoadReport { .. };
    let at = nzgo::VarToLoaded(v, conformance_unit(), data, vals);
    if at < 0 || schema_rt::save_table_retain(conformance_unit(), "Var", data, vals, nodes, at, store, report, out) != 0 { write_stdout("bad var_retain\n"); exit(1); }
    var r: u8[>..] = [];
    for b in out { r.push(b); }
    return r;
}

fn var_json(v: const nzgo::Var&) -> u8[>..] {
    var out: u8[>..] = [];
    if nzgo::VarToJson(v, out) < 0 { write_stdout("bad var_json\n"); exit(1); }
    return out;
}

fn fix_json(v: const nzgo::Fix&) -> u8[>..] {
    var out: u8[>..] = [];
    if nzgo::FixToJson(v, out) < 0 { write_stdout("bad fix_json\n"); exit(1); }
    return out;
}

fn var_jsonrt(v: const nzgo::Var&) -> u8[>..] {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var out: u8[>..] = [];
    let at = nzgo::VarToLoaded(v, conformance_unit(), data, vals);
    if at < 0 || schema_rt::json_save_table(nzgo::TableUnit, nzgo::TextUnit, "Var", data, vals, nodes, at, out) < 0 { write_stdout("bad var_jsonrt\n"); exit(1); }
    return out;
}

fn fix_jsonrt(v: const nzgo::Fix&) -> u8[>..] {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    var out: u8[>..] = [];
    let at = nzgo::FixToLoaded(v, conformance_unit(), data, vals);
    if at < 0 || schema_rt::json_save_table(nzgo::TableUnit, nzgo::TextUnit, "Fix", data, vals, nodes, at, out) < 0 { write_stdout("bad fix_jsonrt\n"); exit(1); }
    return out;
}

fn var_cook(v: const nzgo::Var&) -> u8[>..] {
    var data: u8[>..] = [];
    var vals: schema_rt::LoadVal[>..] = [];
    var nodes: schema_rt::LoadNode[>..] = [];
    let at = nzgo::VarToLoaded(v, conformance_unit(), data, vals);
    let need = if at < 0 { -1 } else { nzgo::VarCookMeasure(data, vals, nodes, at) };
    var out: u8[>..] = [];
    for i in need { out.push(0); }
    if need < 0 || !nzgo::VarCook(data, vals, nodes, at, out, false) { write_stdout("bad var_cook\n"); exit(1); }
    return out;
}

fn fix_cook(v: const nzgo::Fix&) -> u8[>..] {
    var out: u8[>..] = [];
    for i in nzgo::FixCookMeasure() { out.push(0); }
    if !nzgo::FixCook(v, out, false) { write_stdout("bad fix_cook\n"); exit(1); }
    return out;
}

fn fix_fixed(v: const nzgo::Fix&) -> u8[>..] {
    var values: nzgo::Fix[1] = [v];
    var out: u8[>..] = [];
    if nzgo::FixFixedSave(values[0..1], out) < 0 { write_stdout("bad fix_fixed\n"); exit(1); }
    return out;
}

fn fix_message(v: const nzgo::Fix&) -> u8[>..] {
    var values: nzgo::Fix[1] = [v];
    var report = schema_rt::MsgReport { .. };
    var out: u8[>..] = [];
    if nzgo::FixSaveMessages(values[0..1], out, report) < 0 { write_stdout("bad fix_message\n"); exit(1); }
    return out;
}

fn load_var(file: const u8[:]) -> nzgo::Var {
    var v = nzgo::NewVar();
    var report = schema_rt::json_report();
    if !nzgo::VarFromJson(v, input(file), report) || report.damaged { write_stdout(str("bad json ", file, "\n")); exit(1); }
    return v;
}

fn load_fix(file: const u8[:]) -> nzgo::Fix {
    var v = nzgo::NewFix();
    var report = schema_rt::json_report();
    if !nzgo::FixFromJson(v, input(file), report) || report.damaged { write_stdout(str("bad json ", file, "\n")); exit(1); }
    return v;
}
`

// TestGooseCompressedNegZeroCorpus runs the compressed -0 writer corpus on
// the generated Goose code. Each row loads <base>.json through the typed text
// read, sets <path> to -0 IN THE NATIVE VALUE, and saves <form>: the bytes
// must be <expect>.<form>.bin, byte for byte. Beside each row, every other
// writer the root has saves the -0 value and its +0 twin (the base itself):
// a compressed row owes the twin's bytes, and the plain row `p` owes bytes
// that differ (a plain float's -0 rides).
func TestGooseCompressedNegZeroCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Goose toolchain (it builds and runs the compressed -0 corpus over the generated unit)")
	u := negZeroCorpusUnit(t)
	files := probeFiles(t, u, nil)
	rows := negZeroRows(t)
	writers := map[string][]string{
		"Var": {"var_save", "var_retain", "var_json", "var_jsonrt", "var_cook"},
		"Fix": {"fix_save", "fix_fixed", "fix_json", "fix_jsonrt", "fix_cook", "fix_message"},
	}
	formWriter := map[string]string{"Var/variable": "var_save", "Fix/variable": "fix_save", "Fix/fixed": "fix_fixed"}
	var main strings.Builder
	main.WriteString(probePrelude)
	main.WriteString(negZeroCorpusMain)
	main.WriteString("\nfn main() {\n")
	inputs := map[string][]byte{}
	for i, r := range rows {
		file := r.base + ".json"
		data, err := os.ReadFile(filepath.Join(negZeroCorpusDir, file))
		if err != nil {
			t.Fatal(err)
		}
		inputs[file] = data
		load := map[string]string{"Var": "load_var", "Fix": "load_fix"}[r.root]
		if load == "" {
			t.Fatalf("%s: root %s", r.name, r.root)
		}
		fw := formWriter[r.root+"/"+r.form]
		if fw == "" {
			t.Fatalf("%s: no %s writer for %s", r.name, r.form, r.root)
		}
		fmt.Fprintf(&main, "    var plus%d = %s(%q);\n", i, load, file)
		fmt.Fprintf(&main, "    var minus%d = %s(%q);\n", i, load, file)
		fmt.Fprintf(&main, "    minus%d%s = negz();\n", i, negZeroGoosePath(t, u, r.root, r.path))
		fmt.Fprintf(&main, "    print(\"row %s \", hex(%s(minus%d)));\n", r.name, fw, i)
		for _, w := range writers[r.root] {
			fmt.Fprintf(&main, "    print(\"twin %s %s \", hex(%s(minus%d)), \" \", hex(%s(plus%d)));\n", r.name, w, w, i, w, i)
		}
	}
	main.WriteString("}\n")
	files["main.goose"] = []byte(main.String())
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := string(gooserun.Run(t, dir, "main.goose", inputs))
	got := map[string][]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 {
			continue
		}
		switch f[0] {
		case "row":
			if len(f) != 3 {
				t.Fatalf("probe line %q", line)
			}
			got["row "+f[1]] = f[2:]
		case "twin":
			if len(f) != 5 {
				t.Fatalf("probe line %q", line)
			}
			got["twin "+f[1]+" "+f[2]] = f[3:]
		default:
			t.Fatalf("probe line %q", line)
		}
	}
	plains, twins := 0, 0
	for _, r := range rows {
		plain := r.path == "p"
		if plain == (r.base == r.expect) {
			t.Errorf("%s: a compressed row owes its own base and the plain control another file", r.name)
		}
		if plain {
			plains++
		}
		want, err := os.ReadFile(filepath.Join(negZeroCorpusDir, r.expect+"."+r.form+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		g, ok := got["row "+r.name]
		if !ok {
			t.Fatalf("%s: the probe printed no row", r.name)
		}
		if have := probeHex(t, g[0]); !bytes.Equal(have, want) {
			t.Errorf("%s: the %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", r.name, r.form, r.path, r.expect, r.form, hex.EncodeToString(have), hex.EncodeToString(want))
		}
		for _, w := range writers[r.root] {
			g, ok := got["twin "+r.name+" "+w]
			if !ok {
				t.Fatalf("%s: the probe printed no %s twin", r.name, w)
			}
			twins++
			minus, plus := probeHex(t, g[0]), probeHex(t, g[1])
			switch {
			case plain && bytes.Equal(minus, plus):
				t.Errorf("%s: %s: a plain float's -0 did not ride: %x", r.name, w, minus)
			case !plain && !bytes.Equal(minus, plus):
				t.Errorf("%s: %s: a compressed float's -0 was not written as +0:\n -0 %x\n +0 %x", r.name, w, minus, plus)
			}
		}
	}
	if len(rows) != 15 || plains != 2 || twins == 0 {
		t.Fatalf("cases.tsv: %d rows, %d plain controls, %d twin checks; want 15 rows and 2 controls", len(rows), plains, twins)
	}
}
