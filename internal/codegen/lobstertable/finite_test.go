package lobstertable_test

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
)

// TABLES ARE FINITE ONLY (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27,
// "Tables go finite-only too, same rule as packets"; the shape of
// finite-tables #2226): every read, the tolerant one too, refuses a float
// whose raw bits are a NaN or an infinity as non_finite_float, naming
// <Owner>.<field>, and every writer refuses one. -0 and the subnormals ride
// bit for bit. These pins are this leg's own: the Go engine on main still
// carries a NaN until #2226 lands, so the expected answers are the rule's.

const finiteVariableSchema = `package finiteprobe

type Inner
{
    w float32
}

table Probe
{
    a     float32
    b     float64
    r     float32 = 0.0 | min = 0.0, max = 10.0, resolution = 0.5
    list  [3]float64
    inner Inner
}
`

const finiteFixedSchema = `package finitefixed

fixed table Row
{
    a float32
    b float64
}
`

// the sentinels the clean wire carries, one unique bit pattern per site.
const (
	sentA     = uint32(0x3FE00000)         // a = 1.75
	sentB     = uint64(0x3FF4000000000000) // b = 1.25
	sentR     = uint32(0x40200000)         // r = 2.5
	sentList  = uint64(0x400C000000000000) // list[1] = 3.5
	sentInner = uint32(0x40B00000)         // inner.w = 5.5
)

func finiteGenerate(t *testing.T, schema string) *loadUnit {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the finite probe: %v", err)
	}
	return g
}

const finiteHelpers = `
def fhex(b:string) -> string:
    var o = ""
    for(b) c:
        o += lowercase(number_to_string(c, 16, 2))
    return o
def fslot(rec:schema_rt.Record, name:string) -> schema_rt.Slot:
    for(rec.fields) sl:
        if sl.fd.name == name:
            return sl
    assert false
    return rec.fields[0]
def fclean() -> schema_rt.Record:
    let rec = schema_rt.new_record(schema_rt.table_of("Probe"))
    fslot(rec, "a").cell.f = 1.75
    fslot(rec, "b").cell.f = 1.25
    fslot(rec, "r").cell.f = 2.5
    let l = fslot(rec, "list")
    while length(l.elems) < 3:
        push(l.elems, schema_rt.Value {})
    l.elems[1].f = 3.5
    let i = fslot(rec, "inner")
    if not i.cell.tab:
        i.cell.tab = schema_rt.new_record(schema_rt.table_of("Inner"))
    i.cell.tab.fields[0].cell.f = 5.5
    return rec
`

func finiteRun(t *testing.T, g *loadUnit, body string, inputs map[string][]byte) []string {
	t.Helper()
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\nimport .strict_and_refusals\nimport .text_form\n")
	b.WriteString(finiteHelpers)
	b.WriteString(body)
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
}

// finiteCleanWire is the probe's wire as this leg's writer spells it, with
// every float at its sentinel; the Go engine reads it back as the same
// values (held below), so the patched wires are the reference's spelling too.
func finiteCleanWire(t *testing.T, g *loadUnit) []byte {
	t.Helper()
	out := finiteRun(t, g, "let w, why = schema_rt.save_reasoned(fclean())\nprint(why)\nprint(fhex(w))\n", nil)
	if len(out) != 2 || out[0] != "" {
		t.Fatalf("the clean probe does not save: %q", out)
	}
	wire, err := hex.DecodeString(out[1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := goSave(g, "Probe", wire); err != nil {
		t.Fatalf("the Go engine refuses the clean probe wire: %v", err)
	}
	return wire
}

func patch32(t *testing.T, wire []byte, from, to uint32) []byte {
	t.Helper()
	f := binary.LittleEndian.AppendUint32(nil, from)
	if bytes.Count(wire, f) != 1 {
		t.Fatalf("sentinel %08x is in the wire %d times, want 1", from, bytes.Count(wire, f))
	}
	return bytes.Replace(wire, f, binary.LittleEndian.AppendUint32(nil, to), 1)
}

func patch64(t *testing.T, wire []byte, from, to uint64) []byte {
	t.Helper()
	f := binary.LittleEndian.AppendUint64(nil, from)
	if bytes.Count(wire, f) != 1 {
		t.Fatalf("sentinel %016x is in the wire %d times, want 1", from, bytes.Count(wire, f))
	}
	return bytes.Replace(wire, f, binary.LittleEndian.AppendUint64(nil, to), 1)
}

type finiteReadRow struct {
	name  string
	wire  []byte
	field string // "" for a finite row
}

func finiteReadRows(t *testing.T, clean []byte) []finiteReadRow {
	return []finiteReadRow{
		{"f32_quiet_nan", patch32(t, clean, sentA, 0x7FC00001), "Probe.a"},
		{"f32_signalling_nan", patch32(t, clean, sentA, 0xFFA5A5A5), "Probe.a"},
		{"f32_plus_inf", patch32(t, clean, sentA, 0x7F800000), "Probe.a"},
		{"f64_nan", patch64(t, clean, sentB, 0x7FF00DEFACED0001), "Probe.b"},
		{"f64_minus_inf", patch64(t, clean, sentB, 0xFFF0000000000000), "Probe.b"},
		{"compressed_nan_before_bounds", patch32(t, clean, sentR, 0x7FC00000), "Probe.r"},
		{"compressed_inf_before_bounds", patch32(t, clean, sentR, 0x7F800000), "Probe.r"},
		{"array_element_inf", patch64(t, clean, sentList, 0x7FF0000000000000), "Probe.list"},
		{"nested_type_nan", patch32(t, clean, sentInner, 0x7F800001), "Inner.w"},
		{"f32_minus_zero", patch32(t, clean, sentA, 0x80000000), ""},
		{"f32_subnormal", patch32(t, clean, sentA, 0x00000001), ""},
		{"f64_minus_zero", patch64(t, clean, sentB, 0x8000000000000000), ""},
		{"f64_subnormal", patch64(t, clean, sentB, 0x0000000000000001), ""},
	}
}

const finiteReadBody = `
def fread(file:string):
    let data = read_file(file)
    assert data
    let srep = schema_rt.LoadReport {}
    let srec, reason = schema_rt.load_strict("Probe", data, srep)
    assert (srec == nil) == (reason != "")
    let trep = schema_rt.LoadReport {}
    let trec = schema_rt.load_tolerant("Probe", data, trep)
    let prep = schema_rt.LoadReport {}
    let prec = schema_rt.load("Probe", data, prep)
    print(if reason == "": "ok" else: reason)
    print(if trec: "tolerant:record" else: "tolerant:nil " + trep.reason)
    print(if prec: "plain:record" else: "plain:nil")
    if trec:
        let out, why = schema_rt.save_reasoned(trec)
        print(why + ":" + fhex(out))
    else:
        print("-")
`

func runFiniteReads(t *testing.T, g *loadUnit, rows []finiteReadRow) [][]string {
	t.Helper()
	inputs := map[string][]byte{}
	var body strings.Builder
	body.WriteString(finiteReadBody)
	for i, r := range rows {
		inputs[fmt.Sprintf("w%d", i)] = r.wire
		fmt.Fprintf(&body, "fread(\"w%d\")\n", i)
	}
	out := finiteRun(t, g, body.String(), inputs)
	if len(out) != 4*len(rows) {
		t.Fatalf("the read program printed %d lines for %d rows: %q", len(out), len(rows), out)
	}
	var res [][]string
	for i := range rows {
		res = append(res, out[4*i:4*i+4])
	}
	return res
}

// finiteReadMismatches is every row whose answer the rule does not give.
func finiteReadMismatches(t *testing.T, g *loadUnit, rows []finiteReadRow, res [][]string) []string {
	t.Helper()
	var bad []string
	for i, r := range rows {
		got := res[i]
		if r.field != "" {
			want := []string{"non_finite_float", "tolerant:nil non_finite_float: " + r.field, "plain:nil", "-"}
			if strings.Join(got, "|") != strings.Join(want, "|") {
				bad = append(bad, fmt.Sprintf("%s: got %q, want %q", r.name, got, want))
			}
			continue
		}
		// -0 and a subnormal are finite: every read lands them, and the save
		// writes them bit for bit (elision compares bits, so -0 under the +0
		// default rides), byte for byte the Go engine's re-encoding
		want, err := goSave(g, "Probe", r.wire)
		if err != nil {
			t.Fatalf("%s: the Go engine refuses a finite row: %v", r.name, err)
		}
		if got[0] != "ok" || got[1] != "tolerant:record" || got[2] != "plain:record" || got[3] != ":"+hex.EncodeToString(want) {
			bad = append(bad, fmt.Sprintf("%s: got %q, want ok/record/record and the save %x", r.name, got, want))
		}
		if !bytes.Equal(want, r.wire) {
			t.Fatalf("%s: the Go engine does not re-encode the wire it read, so the row proves nothing", r.name)
		}
	}
	return bad
}

// TestLobsterTablesAreFiniteOnly holds every form to the finite rule: the
// variable read (strict, tolerant and the plain name) at f32, f64, a
// compressed float (tested before its bounds), an array element and a nested
// type's field; the variable save; the text form; the block form; and the
// fixed form's read, checked save and plain save.
func TestLobsterTablesAreFiniteOnly(t *testing.T) {
	t.Parallel()
	g := finiteGenerate(t, finiteVariableSchema)
	clean := finiteCleanWire(t, g)
	rows := finiteReadRows(t, clean)
	if bad := finiteReadMismatches(t, g, rows, runFiniteReads(t, g, rows)); len(bad) > 0 {
		t.Errorf("variable read:\n%s", strings.Join(bad, "\n"))
	}

	// THE VARIABLE SAVE refuses a NaN or an infinity in any float, a double
	// past float32's range in a float32 field included (it narrows to one)
	saves := []struct{ name, set string }{
		{"f32_nan", `fslot(rec, "a").cell.f = 0.0 / 0.0`},
		{"f32_past_range", `fslot(rec, "a").cell.f = 1.0e300`},
		{"f64_inf", `fslot(rec, "b").cell.f = 1.0 / 0.0`},
		{"compressed_nan", `fslot(rec, "r").cell.f = 0.0 / 0.0`},
		{"element_minus_inf", `fslot(rec, "list").elems[2].f = -1.0 / 0.0`},
		{"nested_nan", `let it = fslot(rec, "inner").cell.tab
    assert it
    it.fields[0].cell.f = 0.0 / 0.0`},
		{"finite_minus_zero", `fslot(rec, "a").cell.f = -0.0`},
	}
	var body strings.Builder
	for i, s := range saves {
		fmt.Fprintf(&body, "def fsave%d():\n    let rec = fclean()\n    %s\n    let out, why = schema_rt.save_reasoned(rec)\n    print(why + \" \" + (if out == \"\": \"empty\" else: \"bytes\"))\nfsave%d()\n", i, s.set, i)
	}
	out := finiteRun(t, g, body.String(), nil)
	for i, s := range saves {
		want := "non_finite_float empty"
		if strings.HasPrefix(s.name, "finite") {
			want = " bytes"
		}
		if i >= len(out) || out[i] != want {
			t.Errorf("variable save %s: got %q, want %q", s.name, out, want)
		}
	}

	// THE TEXT FORM: a token past its storage is text_bound (Go's resolver)
	texts := []struct{ name, doc, want string }{
		{"f32_past_range", `(:a 1e39)`, "nil"},
		{"f64_past_range", `(:b 1e999)`, "nil"},
		{"compressed_past_bounds", `(:r 11.0)`, "nil"},
		{"finite", `(:a -0.0 :b 4.9e-324)`, "doc"},
	}
	body.Reset()
	inputs := map[string][]byte{}
	for i, x := range texts {
		inputs[fmt.Sprintf("t%d", i)] = []byte(x.doc)
		fmt.Fprintf(&body, "let d%d = read_file(\"t%d\")\nassert d%d\nprint(if schema_rt.text_load(\"Probe\", d%d): \"doc\" else: \"nil\")\n", i, i, i, i)
	}
	out = finiteRun(t, g, body.String(), inputs)
	for i, x := range texts {
		if i >= len(out) || out[i] != x.want {
			t.Errorf("text %s: got %q, want %s", x.name, out, x.want)
		}
	}

	finiteFixed(t)
	finiteBlock(t)
}

// TestLobsterTablesAreFiniteOnlyNegativeControl removes the read's raw-bits
// test from the emitted runtime and requires exactly the non-finite rows to
// go red, then removes the save's and requires the save refusal to go.
func TestLobsterTablesAreFiniteOnlyNegativeControl(t *testing.T) {
	t.Parallel()
	g := finiteGenerate(t, finiteVariableSchema)
	clean := finiteCleanWire(t, g)
	rows := finiteReadRows(t, clean)
	path := filepath.Join(g.dir, "load.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	anchors := []string{
		"        if not finite_f32_bits(bits):\n            // a NaN or an infinity is refused on its raw bits",
		"        if not finite_f64_bits(bits):\n            refuse_non_finite(rep, fd)",
	}
	patched := string(src)
	for _, a := range anchors {
		if n := strings.Count(patched, a); n != 1 {
			t.Fatalf("the read anchor is in the emitted runtime %d times, want 1: %q", n, a)
		}
		patched = strings.Replace(patched, a, strings.Replace(a, "if not ", "if false and not ", 1), 1)
	}
	if err := os.WriteFile(path, []byte(patched), 0o644); err != nil {
		t.Fatal(err)
	}
	bad := finiteReadMismatches(t, g, rows, runFiniteReads(t, g, rows))
	red := map[string]bool{}
	for _, b := range bad {
		red[strings.SplitN(b, ":", 2)[0]] = true
	}
	for _, r := range rows {
		if (r.field != "") != red[r.name] {
			t.Errorf("the read control: row %s red=%v, want red exactly on the non-finite rows", r.name, red[r.name])
		}
	}

	path = filepath.Join(g.dir, "schema_save.lobster")
	src, err = os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const saveAnchor = "    if e.non_finite != \"\":\n        return \"\", \"non_finite_float\"\n"
	if n := strings.Count(string(src), saveAnchor); n != 1 {
		t.Fatalf("the save anchor is in the emitted runtime %d times, want 1", n)
	}
	if err := os.WriteFile(path, []byte(strings.Replace(string(src), saveAnchor, "", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := finiteRun(t, g, "let rec = fclean()\nfslot(rec, \"b\").cell.f = 1.0 / 0.0\nlet out, why = schema_rt.save_reasoned(rec)\nprint(why + \".\")\n", nil)
	if len(out) != 1 || out[0] == "non_finite_float." {
		t.Errorf("the save control did not go red: %q", out)
	}
}

// finiteFixed holds the fixed form: a hand-made record whose float is a NaN
// or an infinity is refused by FixedLoad and FixedLoadTolerant as
// non_finite_float: Row.<field>; the checked save refuses one as
// invalid_value (Go's FixedCheckBody) and the plain save answers "".
func finiteFixed(t *testing.T) {
	t.Helper()
	u, files := finiteFixedFiles(t)
	_ = u
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	out := finiteFixedRun(t, dir, files, nil)
	if len(out) != 6 {
		t.Fatalf("the fixed program printed %q", out)
	}
	want := []string{
		"save_checked -1 invalid_value",
		"save_plain empty",
		"save_finite bytes",
		"load_nan -1 non_finite_float: Row.a non_finite_float",
		"tolerant_inf -1 non_finite_float: Row.b",
		"load_minus_zero 1 80000000",
	}
	for i := range want {
		if out[i] != want[i] {
			t.Errorf("fixed %d: got %q, want %q", i, out[i], want[i])
		}
	}
}

func finiteFixedFiles(t *testing.T) (string, map[string][]byte) {
	t.Helper()
	src := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(src, []byte(finiteFixedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{src})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	files, err := lobster.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	return u.Package, files
}

const finiteFixedMain = `import .ProbeTable
import .schema_runtime

def make_plan(n:int) -> [schema_rt.TableFixedEntry]:
    let out:[schema_rt.TableFixedEntry] = []
    for(n): push(out, schema_rt.TableFixedEntry {})
    return out

def fixed_with(a:float, b:float) -> string:
    let v = finitefixed.NewRow()
    v.A = a
    v.B = b
    return finitefixed.RowFixedSave([v])

let bad = finitefixed.NewRow()
bad.A = 0.0 / 0.0
let buf = schema_rt.zeros(finitefixed.RowFixedMeasure(1))
let n, _, why = finitefixed.RowFixedSaveChecked([bad], buf)
print("save_checked {n} {why}")
print("save_plain " + (if finitefixed.RowFixedSave([bad]) == "": "empty" else: "bytes"))
let good = fixed_with(1.75, 1.25)
print("save_finite " + (if good != "": "bytes" else: "empty"))

// a NaN and an infinity spelled into the good file's bytes by hand
let at = length(good) - 12
let nanfile = write_int32_le(copy(good), at, 0x7FC00001)
let inffile = write_int64_le(copy(good), at + 4, 0x7FF0000000000000)
let back = [finitefixed.NewRow(), finitefixed.NewRow()]
let plan = make_plan(4096)
let r1 = schema_rt.TableReport {}
let n1 = finitefixed.RowFixedLoad(back, nanfile, plan, r1)
print("load_nan {n1} {r1.reason} " + schema_rt.fixed_report_err(r1))
let r2 = schema_rt.TableReport {}
let n2 = finitefixed.RowFixedLoadTolerant(back, inffile, plan, r2)
print("tolerant_inf {n2} {r2.reason}")
let mz = write_int32_le(copy(good), at, 0x80000000)
let r3 = schema_rt.TableReport {}
let n3 = finitefixed.RowFixedLoad(back, mz, plan, r3)
print("load_minus_zero {n3} " + lowercase(number_to_string(schema_rt.f32bits(back[0].A), 16, 8)))
`

func finiteFixedRun(t *testing.T, dir string, _ map[string][]byte, inputs map[string][]byte) []string {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "main.lobster"), []byte(finiteFixedMain), 0o644); err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimRight(string(lobsterrun.Run(t, dir, "main.lobster", inputs)), "\n"), "\n")
}

// finiteBlock holds the block form on the Padded block (its float64 value):
// a block whose float is an infinity is refused by block_read with the field
// named, and the builder answers "" for a record holding a NaN.
func finiteBlock(t *testing.T) {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{filepath.Join(root, "tables/block")})
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("the block unit: %v", err)
	}
	main := `import .RenderTable
import .PaddedTable
import .block_form

def bslot(rec:schema_rt.Record, name:string) -> schema_rt.Slot?:
    for(rec.fields) sl:
        if sl.fd.name == name:
            return sl
    return nil

// the first float64 slot the projection or one of its rows holds
def first_float(rec:schema_rt.Record) -> schema_rt.Slot?:
    for(rec.fields) sl:
        if sl.fd.s == "float":
            return sl
    for(rec.fields) sl:
        for(sl.elems) v:
            let row = v.tab
            if row:
                let f = first_float(row)
                if f:
                    return f
    return nil

let rec = schema_rt.new_record(schema_rt.table_of("PaddedFrame"))
let sl = first_float(rec)
assert sl
sl.cell.f = 6.25
let good = schema_rt.block_build("PaddedFrame", rec)
print("build_finite " + (if good != "": "bytes" else: "empty"))
print("read_finite " + (if schema_rt.block_read("PaddedFrame", good): "record" else: "nil"))
// 6.25 is 0x4019000000000000: the one place it sits in the image
var at = -1
var i = 0
while i + 8 <= length(good):
    if read_int64_le(good, i) == 0x4019000000000000:
        at = i
    i++
assert at >= 0
let infblock = write_int64_le(good, at, 0x7FF0000000000000)
print("read_inf " + (if schema_rt.block_read("PaddedFrame", infblock): "record" else: "nil " + schema_rt.block_non_finite))
sl.cell.f = 0.0 / 0.0
print("build_nan " + (if schema_rt.block_build("PaddedFrame", rec) == "": "empty " + schema_rt.block_non_finite else: "bytes"))
`
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(main), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", nil)), "\n"), "\n")
	if len(out) != 4 || out[0] != "build_finite bytes" || out[1] != "read_finite record" ||
		!strings.HasPrefix(out[2], "read_inf nil ") || !strings.HasPrefix(out[3], "build_nan empty ") ||
		strings.TrimPrefix(out[2], "read_inf nil ") != strings.TrimPrefix(out[3], "build_nan empty ") ||
		!strings.Contains(out[2], ".") {
		t.Errorf("block: got %q, want the finite block built and read, the infinity refused by the read and the NaN by the builder, one field named", out)
	}
}
