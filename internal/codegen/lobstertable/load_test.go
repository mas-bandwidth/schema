package lobstertable_test

import (
	"bufio"
	"bytes"
	"encoding/hex"
	"fmt"
	"maps"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobstertable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// loadRow is one report or instance row of the tables conformance manifest.
type loadRow struct {
	kind, name, unit, root, wire string
	// data, when set, is the wire itself, built by the test rather than read
	// from the file wire names
	data []byte
	// saveData, when set, is the reference's save of the row's value where the
	// generic writer (internal/tablewire) cannot spell it: a present-empty
	// optional map and a required field at its default both ride although that
	// writer elides them (docs/SPEC-TABLES.md §2.3, §2.8).
	saveData []byte
}

// loadRows reads the report and instance lines of the tables manifest, in
// file order.
func loadRows(t *testing.T) []loadRow {
	t.Helper()
	f, err := os.Open(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var rows []loadRow
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		x := strings.Fields(sc.Text())
		if len(x) >= 5 && (x[0] == "report" || x[0] == "instance") {
			rows = append(rows, loadRow{kind: x[0], name: x[1], unit: x[2], root: x[3], wire: x[4]})
		}
	}
	if err := sc.Err(); err != nil {
		t.Fatal(err)
	}
	return rows
}

// loadUnit is one generated unit: the checked IR and the directory its
// Lobster modules were written into.
type loadUnit struct {
	u    *ir.Unit
	dir  string
	mods []string
}

// generateLoadUnit compiles a unit and writes its Lobster type and table
// surfaces (runtime/load.lobster among them) into a fresh directory; the
// error is the surface's refusal.
func generateLoadUnit(t *testing.T, paths []string) (*loadUnit, error) {
	t.Helper()
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the unit does not load: %v", err)
	}
	files, err := lobster.Generate(u)
	if err != nil {
		return nil, err
	}
	tables, err := lobstertable.Generate(u)
	if err != nil {
		return nil, err
	}
	maps.Copy(files, tables)
	g := &loadUnit{u: u, dir: t.TempDir()}
	for name, data := range files {
		if strings.HasSuffix(name, "Table.lobster") {
			g.mods = append(g.mods, strings.TrimSuffix(name, ".lobster"))
		}
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	sort.Strings(g.mods)
	return g, nil
}

// keyedRegistrations are the schema_rt.keyed_keys calls naming every
// enum-keyed array's key variants (docs/PORT-LOBSTER.md, THE LOAD): the
// descriptor does not carry them yet, so the program registers them.
func keyedRegistrations(u *ir.Unit) string {
	var b strings.Builder
	var names []string
	for n := range u.Tables {
		names = append(names, n)
	}
	for n := range u.Structs {
		if _, dup := u.Tables[n]; !dup {
			names = append(names, n)
		}
	}
	sort.Strings(names)
	for _, n := range names {
		st := u.Tables[n]
		if st == nil {
			st = u.Structs[n]
		}
		for _, f := range st.Fields {
			if f.KeyEnumRef == nil {
				continue
			}
			var vs []string
			for i := range f.KeyEnumRef.Variants {
				vs = append(vs, fmt.Sprintf("schema_rt.EnumVariant { name: %s, id: %s }",
					lobster.StringLit([]byte(f.KeyEnumRef.Variants[i])),
					lobster.IntLit(new(big.Int).SetUint64(ir.TableWireId(f.KeyEnumRef.VariantWireName(i))))))
			}
			fmt.Fprintf(&b, "schema_rt.keyed_keys(%s, %s, [%s]::schema_rt.EnumVariant)\n",
				lobster.StringLit([]byte(n)), lobster.StringLit([]byte(f.Name)), strings.Join(vs, ", "))
		}
	}
	return b.String()
}

// dumpLobster is the Lobster half of THE VALUE DUMP: one line per loaded
// record, every slot in descriptor order, printed exactly as dumpGo prints
// Go's tabletext.Instance. A node is labelled on its first visit (the root is
// 0) and named by label after, so a shared or cyclic graph prints finitely.
const dumpLobster = `var labels:[schema_rt.Record] = []
def hexs(b:string) -> string:
    var s = ""
    for(b) c:
        s += number_to_string(c, 16, 2)
    return lowercase(s)
def dfloat(x:float, f32:bool) -> string:
    if x != x:
        return "n" + lowercase(number_to_string(schema_rt.f64bits(x), 16, 16))
    if f32:
        return "g" + lowercase(number_to_string(schema_rt.f32bits(schema_rt.fround(x)), 16, 8))
    return "d" + lowercase(number_to_string(schema_rt.f64bits(x), 16, 16))
def dnode(rec:schema_rt.Record?) -> string:
    if not rec:
        return "null"
    for(labels) x, at:
        if x == rec:
            return "^{at}"
    let n = length(labels)
    push(labels, rec)
    return "@{n}" + drec(rec)
def dcell(v:schema_rt.Value, fd:schema_rt.FieldDescriptor, kind:int) -> string:
    let s = fd.s
    if s == "bool":
        return if v.b: "b1" else: "b0"
    if s == "int":
        if kind >= 18 and kind <= 29:
            return "w{v.w.hi}:{v.w.lo}"
        return "i{v.i}"
    if s == "float":
        return dfloat(v.f, fd.f32)
    if s == "str" or s == "text" or s == "bytes":
        return "s" + hexs(v.s)
    if s == "enum":
        return "e{v.i}"
    if s == "tab":
        let t = v.tab
        if t:
            return drec(t)
        return "nil"
    if s == "ptr":
        return dnode(v.node)
    if s == "sblob" or s == "bblob":
        let b = v.blob
        if b:
            return "x" + hexs(b)
        return "null"
    if s == "union":
        var out = "t{v.i}"
        let t = v.tab
        if t:
            out += drec(t)
        let a = v.arm
        if a:
            out += "(" + dslot(a) + ")"
        return out
    return "?" + s
def dslot(sl:schema_rt.Slot) -> string:
    let fd = sl.fd
    var out = fd.name + "="
    if fd.present != "":
        out += if sl.present: "+" else: "-"
    out += "#{sl.count}"
    if fd.s == "map":
        let es:[string] = []
        for(sl.entries) v:
            let t = v.tab
            if t:
                push(es, drec(t))
        out += "<" + concat_string(es, ",") + ">"
    elif fd.s == "arr":
        let e = schema_rt.elem_of(fd)
        let es:[string] = []
        for(sl.elems) v:
            push(es, dcell(v, e, fd.ek))
        out += "[" + concat_string(es, ",") + "]"
    else:
        out += dcell(sl.cell, fd, fd.k)
    return out
def drec(rec:schema_rt.Record) -> string:
    let fs:[string] = []
    for(rec.fields) sl:
        push(fs, dslot(sl))
    return "{{" + concat_string(fs, ";") + "}}"
def report_line(rep:schema_rt.LoadReport) -> string:
    let verdict = if rep.refused: "refused" else: "read"
    return "{rep.unknown},{rep.kind_mismatch},{rep.widened},{rep.clamped},{rep.duplicate},{rep.malformed},{verdict}"
def run(root:string, file:string):
    let data = read_file(file)
    assert data
    let rep = schema_rt.LoadReport {}
    let rec = schema_rt.load(root, data, rep)
    print(if rec: "read" else: "refused")
    labels = []
    var dump = "nil"
    if rec:
        push(labels, rec)
        dump = drec(rec)
    // the dump goes out in lines of at most 40000 bytes, after their count
    let n = (length(dump) + 39999) / 40000
    print("{n}")
    var i = 0
    while i < n:
        print(substring(dump, i * 40000, min(40000, length(dump) - i * 40000)))
        i++
    schema_rt.release(rec)
`

// loadProgram is the main for one unit: it imports every table module and
// the load, registers the keyed key variants, and loads each wire, printing
// the report line and the value dump.
func loadProgram(g *loadUnit, roots []string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\n")
	b.WriteString(keyedRegistrations(g.u))
	b.WriteString(dumpLobster)
	for i, r := range roots {
		fmt.Fprintf(&b, "run(%s, \"w%d\")\n", lobster.StringLit([]byte(r)), i)
	}
	return b.String()
}

// goLoad compares only the single read's acceptance and complete value.
func goLoad(u *ir.Unit, rootName string, wire []byte) (string, string) {
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup(rootName))
	var rep tabletext.Report
	ok, err := tablewire.Decode(m, inst, wire, &rep)
	if err != nil || !ok {
		return "refused", "nil"
	}
	d := &goDumper{labels: map[*tabletext.Instance]int{inst: 0}, next: 1}
	return "read", d.rec(inst)
}

// goDumper is the Go half of THE VALUE DUMP (dumpLobster).
type goDumper struct {
	labels map[*tabletext.Instance]int
	next   int
}

func (d *goDumper) rec(inst *tabletext.Instance) string {
	parts := make([]string, len(inst.Fields))
	for i := range inst.Fields {
		parts[i] = d.slot(&inst.Fields[i], inst.Fields[i].Def)
	}
	return "{" + strings.Join(parts, ";") + "}"
}

func (d *goDumper) node(inst *tabletext.Instance) string {
	if inst == nil {
		return "null"
	}
	if n, ok := d.labels[inst]; ok {
		return fmt.Sprintf("^%d", n)
	}
	n := d.next
	d.labels[inst] = n
	d.next++
	return fmt.Sprintf("@%d", n) + d.rec(inst)
}

func (d *goDumper) slot(fv *tabletext.Field, f *ir.Field) string {
	out := f.Name + "="
	if f.Type.Optional {
		if fv.Present {
			out += "+"
		} else {
			out += "-"
		}
	}
	out += fmt.Sprintf("#%d", fv.Count)
	switch {
	case f.IsMap():
		var es []string
		for i := range fv.Entries {
			es = append(es, d.rec(fv.Entries[i].Tab))
		}
		return out + "<" + strings.Join(es, ",") + ">"
	case f.Array != ir.ArrayNone:
		e := *f
		e.Array = ir.ArrayNone
		e.KeyEnum = ""
		var es []string
		for i := range fv.Elems {
			es = append(es, d.cell(&fv.Elems[i], &e))
		}
		return out + "[" + strings.Join(es, ",") + "]"
	}
	return out + d.cell(&fv.Cell, f)
}

func goFloat(x float64, f32 bool) string {
	if math.IsNaN(x) {
		return fmt.Sprintf("n%016x", math.Float64bits(x))
	}
	if f32 {
		return fmt.Sprintf("g%08x", math.Float32bits(float32(x)))
	}
	return fmt.Sprintf("d%016x", math.Float64bits(x))
}

func goWide(v *big.Int) string {
	if v == nil {
		v = new(big.Int)
	}
	two128 := new(big.Int).Lsh(big.NewInt(1), 128)
	x := new(big.Int).Mod(v, two128)
	mask := new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), 64), big.NewInt(1))
	lo := new(big.Int).And(x, mask).Uint64()
	hi := new(big.Int).Rsh(x, 64).Uint64()
	return fmt.Sprintf("w%d:%d", int64(hi), int64(lo))
}

func (d *goDumper) cell(c *tabletext.Cell, f *ir.Field) string {
	t := f.Type
	switch {
	case t.Pointer && t.Blob():
		if c.Blob == nil {
			return "null"
		}
		return "x" + hex.EncodeToString(c.Blob.Data)
	case t.Pointer:
		return d.node(c.Node)
	case t.Kind == ir.TBool:
		if c.B {
			return "b1"
		}
		return "b0"
	case t.Kind == ir.TFloat32 || t.Kind == ir.TFloat64:
		return goFloat(c.F, t.Kind == ir.TFloat32)
	case t.Kind == ir.TString || t.Kind == ir.TBytes:
		return "s" + hex.EncodeToString(c.Str)
	}
	if un := tabletext.UnionOf(f); un != nil {
		out := fmt.Sprintf("t%d", c.U)
		if c.Tab != nil {
			out += d.rec(c.Tab)
		}
		if c.Arm != nil && c.U >= 1 && int(c.U) <= len(un.Variants) {
			armField := *c.Arm.Def
			armField.Name = un.Variants[c.U-1].Name
			out += "(" + d.slot(c.Arm, &armField) + ")"
		}
		return out
	}
	if tabletext.EnumOf(f) != nil {
		return fmt.Sprintf("e%d", c.U)
	}
	if tabletext.StructOf(f) != nil {
		if c.Tab == nil {
			return "nil"
		}
		return d.rec(c.Tab)
	}
	if ir.TableKindWide(ir.TableScalarKind(f)) {
		return goWide(c.Wide)
	}
	return fmt.Sprintf("i%d", int64(c.U))
}

// unitResult is one unit's rows run under Lobster and under the reference.
type unitResult struct {
	rows   []loadRow
	lob    []string // acceptance, complete value dump, per row
	goLine []string
	goDump []string
}

// runUnit generates one unit, loads every row's wire under the pinned
// toolchain in one program, and loads the same wires through the Go
// reference engine.
func runUnit(t *testing.T, g *loadUnit, rows []loadRow) unitResult {
	t.Helper()
	var roots []string
	inputs := map[string][]byte{}
	res := unitResult{rows: rows}
	for i, row := range rows {
		wire := row.data
		if wire == nil {
			var err error
			if wire, err = os.ReadFile(filepath.Join(root, row.wire)); err != nil {
				t.Fatalf("%s: %v", row.name, err)
			}
		}
		inputs[fmt.Sprintf("w%d", i)] = wire
		roots = append(roots, row.root)
		line, dump := goLoad(g.u, row.root, wire)
		res.goLine = append(res.goLine, line)
		res.goDump = append(res.goDump, dump)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(loadProgram(g, roots)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", inputs)), "\n"), "\n")
	// each row prints its report line, then the count of dump lines and the
	// dump in that many pieces
	for range rows {
		var n int
		if len(out) < 2 {
			t.Fatalf("unit %s: the program's output ends early", rows[0].unit)
		}
		if _, err := fmt.Sscanf(out[1], "%d", &n); err != nil || len(out) < 2+n {
			t.Fatalf("unit %s: the program's output is not report, count, dump: %q", rows[0].unit, out[1])
		}
		res.lob = append(res.lob, out[0], strings.Join(out[2:2+n], ""))
		out = out[2+n:]
	}
	if len(out) != 0 {
		t.Fatalf("unit %s: %d lines past the last row", rows[0].unit, len(out))
	}
	return res
}

// Every manifest wire either supplies the complete reference value or refuses.
// Local diagnostic spellings, counters and discovery order are not compared.
func TestLobsterTableLoadMatchesTheReports(t *testing.T) {
	t.Parallel()
	units, _ := readManifest(t)
	byUnit := map[string][]loadRow{}
	var order []string
	for _, row := range loadRows(t) {
		if _, ok := units[row.unit]; !ok {
			t.Fatalf("row %s names undeclared unit %s", row.name, row.unit)
		}
		if _, seen := byUnit[row.unit]; !seen {
			order = append(order, row.unit)
		}
		byUnit[row.unit] = append(byUnit[row.unit], row)
	}
	accepted, refused := 0, 0
	for _, key := range order {
		paths, err := compiler.GatherPaths(prefix(units[key]))
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		g, err := generateLoadUnit(t, paths)
		if err != nil {
			t.Fatalf("unit %s: %v", key, err)
		}
		res := runUnit(t, g, byUnit[key])
		for i, row := range res.rows {
			verdict, dump := res.lob[2*i], res.lob[2*i+1]
			if verdict != res.goLine[i] {
				t.Errorf("%s %s: acceptance %s, reference %s", row.kind, row.name, verdict, res.goLine[i])
			}
			if dump != res.goDump[i] {
				t.Errorf("%s %s: value differs at %s", row.kind, row.name, divergence(dump, res.goDump[i]))
			}
			if verdict == "read" {
				accepted++
			} else {
				refused++
				if dump != "nil" {
					t.Errorf("%s publishes a partial value", row.name)
				}
			}
		}
	}
	if accepted == 0 || refused == 0 {
		t.Fatalf("vacuous contract coverage: accepted=%d refused=%d", accepted, refused)
	}
	t.Logf("%d accepted complete values; %d whole-load refusals", accepted, refused)
}

// Changing a lawful scalar value must be detected even when acceptance is unchanged.
func TestLobsterTableLoadNegativeControl(t *testing.T) {
	t.Parallel()
	g := probeUnit(t)
	row := loadRow{kind: "probe", name: "scalar", unit: "probe", root: "Root", data: probeWire(t, g)}
	control := runUnit(t, g, []loadRow{row})
	if control.lob[0] != "read" || control.lob[1] != control.goDump[0] {
		t.Fatal("unmodified scalar does not match")
	}
	path := filepath.Join(g.dir, "load.lobster")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "    cell.i = value\n    return true\n"
	if strings.Count(string(src), anchor) != 1 {
		t.Fatal("scalar assignment anchor not unique")
	}
	patched := strings.Replace(string(src), anchor, "    cell.i = value + 1\n    return true\n", 1)
	if err := os.WriteFile(path, []byte(patched), 0644); err != nil {
		t.Fatal(err)
	}
	result := runUnit(t, g, []loadRow{row})
	if result.lob[0] != "read" || result.lob[1] == result.goDump[0] {
		t.Fatalf("value mutation was not detected: %v", result.lob)
	}
}

// divergence names the first byte two dumps differ at, with context.
func divergence(a, b string) string {
	i := 0
	for i < len(a) && i < len(b) && a[i] == b[i] {
		i++
	}
	from := max(0, i-120)
	return fmt.Sprintf("%d:\nlobster: ...%s\ngo:      ...%s", i, a[from:min(len(a), i+80)], b[from:min(len(b), i+80)])
}

// bytesPastItsBody is a variable table whose bytes field has a neighbour on
// each side, so a bytes read that runs past its body's L would take the
// trailing field's bytes and the id table as payload.
const bytesPastItsBody = `package bytespast

table Holder
{
    lead  uint32 = 1
    blob  bytes(16)
    trail uint32 = 2
}
`

// A bytes count past its own body refuses the whole value, including its
// valid neighbors. The intact input still returns all three fields.
func TestLobsterTableLoadBytesCountPastItsBody(t *testing.T) {
	t.Parallel()
	src := filepath.Join(t.TempDir(), "BytesPast.schema")
	if err := os.WriteFile(src, []byte(bytesPastItsBody), 0o644); err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, []string{src})
	if err != nil {
		t.Fatalf("the surface refuses the unit: %v", err)
	}
	m := tabletext.NewModel(g.u)
	inst := m.New(m.Lookup("Holder"))
	for i := range inst.Fields {
		switch inst.Fields[i].Def.Name {
		case "lead":
			inst.Fields[i].Cell.U = 7
		case "blob":
			inst.Fields[i].Cell.Str = []byte{0xaa, 0xbb}
			inst.Fields[i].Count = 2
		case "trail":
			inst.Fields[i].Cell.U = 9
		}
	}
	intact, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatal(err)
	}
	// the count's one LEB byte sits right before the payload
	at := bytes.Index(intact, []byte{0x02, 0xaa, 0xbb})
	if at < 0 || bytes.Count(intact, []byte{0x02, 0xaa, 0xbb}) != 1 {
		t.Fatalf("the encoded wire %x does not carry the bytes body once", intact)
	}
	raised := bytes.Clone(intact)
	raised[at] = 0x10
	if line, _ := goLoad(g.u, "Holder", raised); line != "refused" {
		t.Fatalf("the reference accepted the raised count (%s)", line)
	}
	rows := []loadRow{
		{kind: "instance", name: "bytes_intact", unit: "bytespast", root: "Holder", data: intact},
		{kind: "instance", name: "bytes_count_past_body", unit: "bytespast", root: "Holder", data: raised},
	}
	res := runUnit(t, g, rows)
	for i, row := range rows {
		line, dump := res.lob[2*i], res.lob[2*i+1]
		if line != res.goLine[i] {
			t.Errorf("%s: the report is not the reference's:\nlobster: %s\ngo:      %s", row.name, line, res.goLine[i])
			continue
		}
		if dump != res.goDump[i] {
			t.Errorf("%s: the loaded value is not the reference's at byte %s", row.name, divergence(dump, res.goDump[i]))
			continue
		}
		t.Logf("%s: %x -> %s, value equal to Go or discarded as damage", row.name, row.data, line)
	}
}
