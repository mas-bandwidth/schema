package goosetable

// THE MESSAGE FORM ON THE GOOSE LEG (docs/SPEC-TABLES.md §3.3; messageform.go).
//
// THE ORACLE IS THE GO REFERENCE AND THE TREE'S GOLDENS. For every `message`
// row of the conformance manifest whose root is a fixed table (the form is a
// fixed table's), the Go engine reads the golden batch under the connection's
// announcement (tablewire.AnnounceRead, tablewire.DecodeMessages); the value it
// lands is spelled into the Goose program as assignments onto a fresh value,
// and the Goose leg must:
//
//   - emit the announcement byte for byte as the connection's golden,
//   - SAVE that value to the golden's bytes, and MEASURE its length,
//   - LOAD the golden back to the same value (compared as record images, the
//     fixed form's own byte view), and save what it loaded to the golden again.
//
// Then the form's own rows over schemas of this file, each oracle'd by Go's
// tablewire.EncodeMessages: every payload shape the fixed closure can carry
// (ranged and raw integers, bits, flags, fixed point, compressed and plain
// floats, enums by variant reference, unions by arm reference, string, bytes
// and wstring with their aligns, fixed, counted and enum-keyed arrays, nested
// bodies and optionals), a batch of three, and the refusals by name.

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the manifest's message rows ----------------------------------------

type gooseMsgRow struct {
	name, unitDir, root, conn, message string
}

func gooseMsgRows(t *testing.T) []gooseMsgRow {
	t.Helper()
	f, err := os.Open(filepath.Join(fixedRoot, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	units := map[string]string{}
	conns := map[string][2]string{} // connection -> unit, announcement file
	var lines [][]string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) == 0 {
			continue
		}
		switch fs[0] {
		case "unit":
			units[fs[1]] = fs[2]
		case "connection":
			conns[fs[1]] = [2]string{fs[2], fs[4]}
		case "message":
			lines = append(lines, fs)
		}
	}
	var rows []gooseMsgRow
	for _, fs := range lines {
		c, ok := conns[fs[2]]
		if !ok {
			t.Fatalf("message %s names no connection %s", fs[1], fs[2])
		}
		rows = append(rows, gooseMsgRow{name: fs[1], unitDir: units[c[0]], root: fs[3], conn: c[1], message: fs[5]})
	}
	if len(rows) == 0 {
		t.Fatal("the manifest carries no message rows")
	}
	return rows
}

func gooseMsgUnitDir(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(fixedRoot, dir, "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("unit %s has no schema files: %v", dir, err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("load %s: %v", dir, err)
	}
	return u
}

func gooseMsgFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(fixedRoot, path))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

// isFixedRoot reports whether the root rides the message form (§3.3: the form
// is a fixed table's).
func isFixedRoot(u *ir.Unit, root string) bool {
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == root {
			return true
		}
	}
	return false
}

// ---- the value, as Goose assignments ------------------------------------

// gooseInstAssign spells a Go reference instance as assignments onto the
// Goose value at prefix (docs/PORT-GOOSE.md, THE TYPE MAPPING).
func gooseInstAssign(t *testing.T, prefix string, inst *tabletext.Instance) []string {
	t.Helper()
	var out []string
	for i := range inst.Fields {
		out = append(out, gooseFieldAssign(t, prefix, &inst.Fields[i], inst.Fields[i].Def.Name)...)
	}
	return out
}

func gooseFieldAssign(t *testing.T, prefix string, fv *tabletext.Field, name string) []string {
	t.Helper()
	f := fv.Def
	slot := prefix + "." + ir.GoExportName(name)
	var out []string
	if f.Type.Optional {
		out = append(out, fmt.Sprintf("%s.%s = %t", prefix, ir.GoExportName(name+"_present"), fv.Present))
		if !fv.Present {
			return out
		}
	}
	el := elemOf(f)
	switch {
	case f.Array != ir.ArrayNone || f.KeyEnum != "":
		if f.Array == ir.ArrayCounted {
			out = append(out, fmt.Sprintf("%sCount = %d", slot, fv.Count))
		}
		for i := range fv.Elems {
			if f.Array == ir.ArrayCounted && i >= fv.Count {
				break
			}
			out = append(out, gooseCellAssign(t, el, &fv.Elems[i], fmt.Sprintf("%s[%d]", slot, i))...)
		}
	case f.Type.Kind == ir.TString || f.Type.Kind == ir.TBytes:
		for i, b := range fv.Cell.Str {
			out = append(out, fmt.Sprintf("%s[%d] = %d", slot, i, b))
		}
		out = append(out, fmt.Sprintf("%sLength = %d", slot, len(fv.Cell.Str)))
	case f.Type.Kind == ir.TWString:
		for i, u := range fv.Cell.Units {
			out = append(out, fmt.Sprintf("%s[%d] = %d", slot, i, u))
		}
		out = append(out, fmt.Sprintf("%sLength = %d", slot, len(fv.Cell.Units)))
	default:
		out = append(out, gooseCellAssign(t, f, &fv.Cell, slot)...)
	}
	return out
}

func gooseCellAssign(t *testing.T, f *ir.Field, c *tabletext.Cell, slot string) []string {
	t.Helper()
	switch f.Type.Kind {
	case ir.TBool:
		return []string{fmt.Sprintf("%s = %t", slot, c.B)}
	case ir.TFloat32:
		return []string{fmt.Sprintf("%s = schema_rt::fixed_bits_f32(0x%08x)", slot, math.Float32bits(float32(c.F)))}
	case ir.TFloat64:
		return []string{fmt.Sprintf("%s = schema_rt::fixed_bits_f64(0x%016x)", slot, math.Float64bits(c.F))}
	case ir.TFixed, ir.TInt:
		if ir.TableFixedStorageBytes(f.Type) > 8 {
			lo, hi := lanes128(c.Wide)
			return []string{slot + ".lo = " + lo, slot + ".hi = " + hi}
		}
		if f.Type.Kind == ir.TFixed {
			if c.Wide == nil {
				return []string{slot + " = 0"}
			}
			return []string{fmt.Sprintf("%s = %s", slot, gooseInt(t, c.Wide.String()))}
		}
		if f.Type.Signed {
			return []string{fmt.Sprintf("%s = %s", slot, gooseInt(t, strconv.FormatInt(c.I, 10)))}
		}
		return []string{fmt.Sprintf("%s = %d", slot, c.U)}
	case ir.TBits:
		return []string{fmt.Sprintf("%s = %d", slot, c.U)}
	case ir.TNamed:
		switch r := f.Type.Ref.(type) {
		case *ir.Enum, *ir.Flags:
			return []string{fmt.Sprintf("%s = %d", slot, c.U)}
		case *ir.Struct:
			if c.Tab == nil {
				return nil
			}
			return gooseInstAssign(t, slot, c.Tab)
		case *ir.Union:
			out := []string{fmt.Sprintf("%s.Type = %d", slot, c.U)}
			if c.U == 0 {
				return out
			}
			v := r.Variants[c.U-1]
			switch {
			case v.Void():
			case c.Tab != nil:
				out = append(out, gooseInstAssign(t, slot+"."+ir.GoExportName(v.Name), c.Tab)...)
			case c.Arm != nil:
				out = append(out, gooseFieldAssign(t, slot, c.Arm, v.Name)...)
			}
			return out
		}
	}
	t.Fatalf("%s: a field kind this test cannot spell", f.Name)
	return nil
}

// ---- the program --------------------------------------------------------

type gooseMsgCase struct {
	unit     *ir.Unit
	root     string
	values   [][]string // one assignment list per body
	data     []byte     // the batch the leg loads
	capacity int
	// announce is the PEER's announcement the load resolves against
	// (AnnounceRead); nil reads under the unit's own (`<Root>Announce`)
	announce []byte
}

type gooseMsgResult struct {
	announce        string
	saved           string
	saveN, measureN int64
	saveReason      string
	loadN           int64
	loadReason      string
	loadMalformed   bool
	loadClamped     int64
	loadCount       int64
	loadUnknown     int64
	loadMismatch    int64
	loadWidened     int64
	announced       string
	got, want       []string
	resaved         string
	raw             string
}

func gooseMsgProbe(t *testing.T, c gooseMsgCase) gooseMsgResult {
	t.Helper()
	files, err := goose.Generate(c.unit)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := Generate(c.unit)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	for k, v := range tables {
		files[k] = v
	}
	var mods []string
	for name := range files {
		if strings.HasSuffix(name, ".goose") {
			mods = append(mods, strings.TrimSuffix(name, ".goose"))
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(c.unit)
	root := ns + "::" + c.root
	if c.capacity < 1 {
		c.capacity = len(c.values)
	}
	cap1 := max(c.capacity, len(c.values), 1)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	fmt.Fprintf(&b, "fn image(v: const %s&) {\n    var img: u8[>..] = [];\n    %s::_%sFixedWrite(img, v);\n    hex(img);\n}\n\n", root, ns, c.root)
	b.WriteString("fn main() {\n")
	fmt.Fprintf(&b, "    print(\"announce\");\n    hex(%s::%sAnnounce);\n", ns, c.root)
	fmt.Fprintf(&b, "    var vals: %s[%d] = [%s { .. }; %d];\n", root, cap1, root, cap1)
	for k, as := range c.values {
		for _, a := range as {
			fmt.Fprintf(&b, "    vals[%d]%s;\n", k, a)
		}
	}
	nv := len(c.values)
	b.WriteString("    var rep = schema_rt::MsgReport { .. };\n")
	if nv > 0 {
		fmt.Fprintf(&b, "    let m = %s::%sMeasureMessages(vals[0..%d], rep);\n", ns, c.root, nv)
		b.WriteString("    var out: u8[>..] = [];\n")
		fmt.Fprintf(&b, "    let s = %s::%sSaveMessages(vals[0..%d], out, rep);\n", ns, c.root, nv)
		b.WriteString("    print(\"save \", m, \" \", s, \" \", schema_rt::msg_reason_name(rep.reason));\n    hex(out);\n")
	}
	if c.data != nil {
		fmt.Fprintf(&b, "    var back: %s[%d] = [%s { .. }; %d];\n", root, c.capacity, root, c.capacity)
		b.WriteString("    var data: u8[>..] = [];\n    if !read_file(\"in.bin\", data) { print(\"read-failed\"); return; }\n")
		b.WriteString("    var voc = schema_rt::MsgVocabulary { .. };\n    var vrep = schema_rt::MsgReport { .. };\n")
		if c.announce != nil {
			b.WriteString("    var peer: u8[>..] = [];\n    if !read_file(\"announce.bin\", peer) { print(\"read-failed\"); return; }\n")
			b.WriteString("    let announced = schema_rt::announce_read(voc, peer, vrep);\n")
		} else {
			fmt.Fprintf(&b, "    let announced = schema_rt::announce_read(voc, %s::%sAnnounce, vrep);\n", ns, c.root)
		}
		b.WriteString("    print(\"announced \", (if announced { 1 } else { 0 }), \" \", schema_rt::msg_reason_name(vrep.reason), \" \", (if vrep.malformed { 1 } else { 0 }), \" \", vrep.unknown);\n")
		b.WriteString("    var lrep = schema_rt::MsgReport { .. };\n")
		fmt.Fprintf(&b, "    let n = %s::%sLoadMessages(back[0..%d], voc, data, lrep);\n", ns, c.root, c.capacity)
		b.WriteString("    print(\"load \", n, \" \", schema_rt::msg_reason_name(lrep.reason), \" \", (if lrep.malformed { 1 } else { 0 }), \" \", lrep.clamped, \" \", lrep.count, \" \", lrep.unknown, \" \", lrep.kind_mismatch, \" \", lrep.widened);\n")
		b.WriteString("    var k: i64 = 0;\n    while k < n {\n        print(\"got\");\n        image(back[k]);\n        print(\"want\");\n        image(vals[k]);\n        k += 1;\n    }\n")
		b.WriteString("    if n > 0 {\n        var again: u8[>..] = [];\n        var arep = schema_rt::MsgReport { .. };\n")
		fmt.Fprintf(&b, "        let a = %s::%sSaveMessages(back[0..n], again, arep);\n", ns, c.root)
		b.WriteString("        print(\"resave \", a);\n        hex(again);\n    }\n")
	}
	b.WriteString("}\n")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	inputs := map[string][]byte{}
	if c.data != nil {
		inputs["in.bin"] = c.data
	}
	if c.announce != nil {
		inputs["announce.bin"] = c.announce
	}
	out := string(gooserun.Run(t, dir, "main.goose", inputs))
	return gooseMsgParse(t, out)
}

func gooseMsgParse(t *testing.T, out string) gooseMsgResult {
	t.Helper()
	r := gooseMsgResult{raw: out}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	next := func(i int) string {
		if i+1 >= len(lines) {
			return "" // an empty hex line, trimmed with the output's end
		}
		return lines[i+1]
	}
	for i := 0; i < len(lines); i++ {
		l := lines[i]
		f := strings.Fields(l)
		switch {
		case l == "announce":
			r.announce = next(i)
			i++
		case strings.HasPrefix(l, "save "):
			r.measureN, _ = strconv.ParseInt(f[1], 10, 64)
			r.saveN, _ = strconv.ParseInt(f[2], 10, 64)
			r.saveReason = f[3]
			r.saved = next(i)
			i++
		case strings.HasPrefix(l, "load "):
			r.loadN, _ = strconv.ParseInt(f[1], 10, 64)
			r.loadReason = f[2]
			r.loadMalformed = f[3] == "1"
			r.loadClamped, _ = strconv.ParseInt(f[4], 10, 64)
			r.loadCount, _ = strconv.ParseInt(f[5], 10, 64)
			r.loadUnknown, _ = strconv.ParseInt(f[6], 10, 64)
			r.loadMismatch, _ = strconv.ParseInt(f[7], 10, 64)
			r.loadWidened, _ = strconv.ParseInt(f[8], 10, 64)
		case strings.HasPrefix(l, "announced "):
			r.announced = strings.TrimPrefix(l, "announced ")
		case l == "got":
			r.got = append(r.got, next(i))
			i++
		case l == "want":
			r.want = append(r.want, next(i))
			i++
		case strings.HasPrefix(l, "resave "):
			r.resaved = next(i)
			i++
		}
	}
	return r
}

// gooseGoDecode is the Go reference's read of a batch under the unit's
// announcement: the values the bytes carry.
func gooseGoDecode(t *testing.T, u *ir.Unit, root string, announce, batch []byte, bodies int) []*tabletext.Instance {
	t.Helper()
	m := tabletext.NewModel(u)
	v := &tablewire.Vocabulary{}
	var rep tabletext.Report
	if err := v.AnnounceRead(announce, &rep); err != nil {
		t.Fatalf("the Go reference refuses the announcement: %v", err)
	}
	st := fixedRootStruct(t, u, root)
	insts := make([]*tabletext.Instance, bodies)
	for i := range insts {
		insts[i] = m.New(st)
	}
	n, ok, err := tablewire.DecodeMessages(m, insts, batch, v, &rep)
	if err != nil || !ok || n != bodies {
		t.Fatalf("the Go reference reads the batch as n=%d ok=%v err=%v", n, ok, err)
	}
	return insts
}

func gooseBatchCount(batch []byte) int { return int(batch[1]) + 1 }

// owes holds a round trip to the oracle: the save is the golden, the measure
// its length, the load lands the value, and the loaded value saves to it.
func (r gooseMsgResult) owes(t *testing.T, golden []byte, bodies int) {
	t.Helper()
	want := hex.EncodeToString(golden)
	if r.saveReason != "ok" || r.saved != want {
		t.Fatalf("SaveMessages wrote\n got  %s (%s)\n want %s", r.saved, r.saveReason, want)
	}
	if r.saveN != int64(len(golden)) || r.measureN != int64(len(golden)) {
		t.Fatalf("SaveMessages returned %d and MeasureMessages %d, want %d", r.saveN, r.measureN, len(golden))
	}
	if r.loadN != int64(bodies) || r.loadReason != "ok" || r.loadMalformed {
		t.Fatalf("LoadMessages answered %d %s malformed=%v, want %d clean:\n%s", r.loadN, r.loadReason, r.loadMalformed, bodies, r.raw)
	}
	for k := range r.got {
		if r.got[k] != r.want[k] {
			t.Fatalf("body %d loaded\n got  %s\n want %s", k, r.got[k], r.want[k])
		}
	}
	if len(r.got) != bodies {
		t.Fatalf("%d bodies compared, want %d", len(r.got), bodies)
	}
	if r.resaved != want {
		t.Fatalf("the loaded batch saved\n got  %s\n want %s", r.resaved, want)
	}
}

// agrees is owes when the batch loaded is not the bytes the values save to:
// the save of the values is want, and the load lands the values.
func (r gooseMsgResult) agrees(t *testing.T, want []byte, bodies int) {
	t.Helper()
	w := hex.EncodeToString(want)
	if r.saveReason != "ok" || r.saved != w {
		t.Fatalf("the writers disagree\n goose %s (%s)\n go    %s", r.saved, r.saveReason, w)
	}
	if r.saveN != int64(len(want)) || r.measureN != int64(len(want)) {
		t.Fatalf("SaveMessages returned %d and MeasureMessages %d, want %d", r.saveN, r.measureN, len(want))
	}
	if r.loadN != int64(bodies) || r.loadReason != "ok" || r.loadMalformed {
		t.Fatalf("LoadMessages answered %d %s malformed=%v, want %d clean:\n%s", r.loadN, r.loadReason, r.loadMalformed, bodies, r.raw)
	}
	if len(r.got) != bodies {
		t.Fatalf("%d bodies compared, want %d", len(r.got), bodies)
	}
	for k := range r.got {
		if r.got[k] != r.want[k] {
			t.Fatalf("body %d loaded\n got  %s\n want %s", k, r.got[k], r.want[k])
		}
	}
	if r.resaved != w {
		t.Fatalf("the loaded batch saved\n got  %s\n want %s", r.resaved, w)
	}
}

// ---- the goldens --------------------------------------------------------

// TestGooseMessageFormMatchesTheGoldens holds every fixed-root `message` row
// of the conformance manifest: the announcement, the save, the measure, the
// load and the re-save, against the tree's goldens and the Go reference's
// read of them.
func TestGooseMessageFormMatchesTheGoldens(t *testing.T) {
	t.Parallel()
	carried := 0
	for _, row := range gooseMsgRows(t) {
		row := row
		u := gooseMsgUnitDir(t, row.unitDir)
		if !isFixedRoot(u, row.root) {
			// a VARIABLE root (graph_tree's Scene): the variable class is
			// the id-table card's, and the row is named here, not counted
			t.Logf("%s: %s is a variable root, owed with the id-table wire (#2236)", row.name, row.root)
			continue
		}
		carried++
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			announce := gooseMsgFile(t, row.conn)
			golden := gooseMsgFile(t, row.message)
			bodies := gooseBatchCount(golden)
			insts := gooseGoDecode(t, u, row.root, announce, golden, bodies)
			var values [][]string
			for _, inst := range insts {
				values = append(values, gooseInstAssign(t, "", inst))
			}
			// the read resolves against the CONNECTION's announcement, read
			// through AnnounceRead as an endpoint reads its peer's
			r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: row.root, values: values, data: golden, announce: announce})
			if r.announced != "1 ok 0 0" {
				t.Fatalf("AnnounceRead of %s answered %q", row.conn, r.announced)
			}
			if r.announce != hex.EncodeToString(announce) {
				t.Fatalf("%sAnnounce is not the connection's golden %s", row.root, row.conn)
			}
			r.owes(t, golden, bodies)
		})
	}
	if carried == 0 {
		t.Fatal("no fixed-root message row was carried")
	}
}

// ---- the fixed corpus, crossed with the reference --------------------------

// gooseMsgFromFixed loads a fixed-form file with the Goose fixed reader and
// saves what it landed as ONE message batch, returning the batch's bytes, or
// nil and the refusal's name.
func gooseMsgFromFixed(t *testing.T, u *ir.Unit, root string, file []byte, records int, finite bool) ([]byte, string) {
	t.Helper()
	files, err := goose.Generate(u)
	if err != nil {
		t.Fatalf("goose type surface: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("goose table surface: %v", err)
	}
	for k, v := range tables {
		files[k] = v
	}
	var mods []string
	for name := range files {
		if strings.HasSuffix(name, ".goose") {
			mods = append(mods, strings.TrimSuffix(name, ".goose"))
		}
	}
	sort.Strings(mods)
	ns := goose.Namespace(u)
	var b strings.Builder
	b.WriteString("import os;\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "import .%s;\n", m)
	}
	b.WriteString("\n" + gooserun.HexProbe + "\n")
	b.WriteString("fn main() {\n    var bytes: u8[>..] = [];\n    if !read_file(\"in.bin\", bytes) { print(\"read-failed\"); return; }\n")
	fmt.Fprintf(&b, "    var values: %s::%s[%d] = [%s::%s { .. }; %d];\n", ns, root, records, ns, root, records)
	b.WriteString("    var report = schema_rt::FixedReport { .. };\n")
	fmt.Fprintf(&b, "    let n = %s::%sFixedLoad(values[0..%d], bytes, report);\n", ns, root, records)
	b.WriteString("    if n < 1 { print(\"fixed-load \", n, \" \", schema_rt::fixed_refusal_name(report.reason)); return; }\n")
	if finite {
		// the root's top-level floats made finite, so the rest of the value
		// still crosses the reference
		for _, f := range fixedRootStruct(t, u, root).Fields {
			slot := "values[k]." + ir.GoExportName(f.Name)
			if f.Array != ir.ArrayNone || f.Type.Optional {
				continue
			}
			switch f.Type.Kind {
			case ir.TFloat32:
				fmt.Fprintf(&b, "    var k%s: i64 = 0;\n    while k%s < n {\n        let k = k%s;\n        if !schema_rt::msg_finite32(schema_rt::fixed_f32_bits(%s)) { %s = 0.0; }\n        k%s += 1;\n    }\n", f.Name, f.Name, f.Name, slot, slot, f.Name)
			case ir.TFloat64:
				fmt.Fprintf(&b, "    var k%s: i64 = 0;\n    while k%s < n {\n        let k = k%s;\n        if !schema_rt::msg_finite64(schema_rt::fixed_f64_bits(%s)) { %s = 0.0; }\n        k%s += 1;\n    }\n", f.Name, f.Name, f.Name, slot, slot, f.Name)
			}
		}
	}
	b.WriteString("    var out: u8[>..] = [];\n    var rep = schema_rt::MsgReport { .. };\n")
	fmt.Fprintf(&b, "    let s = %s::%sSaveMessages(values[0..n], out, rep);\n", ns, root)
	b.WriteString("    if s < 0 { print(\"save-refused \", schema_rt::msg_reason_name(rep.reason)); return; }\n    hex(out);\n}\n")
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(b.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"in.bin": file})))
	if refused, ok := strings.CutPrefix(out, "save-refused "); ok {
		return nil, refused
	}
	if strings.HasPrefix(out, "fixed-load ") {
		return nil, out // the fixed reader refused the file: no values to save
	}
	batch, err := hex.DecodeString(out)
	if err != nil {
		t.Fatalf("the Goose leg did not write a batch: %s", out)
	}
	return batch, ""
}

// TestGooseMessageFormAgreesWithTheReference crosses the Goose message form
// with the Go reference over every fixed root of the C++ reference's fixed
// corpus — nested bodies, counted and enum-keyed arrays, optionals, text and
// bytes, floats, unions, packed scalars. The values are the corpus's, loaded
// by the Goose fixed reader and saved as one batch, and the Go reference reads
// that batch under the unit's announcement. Then the two legs must agree on
// what the Go reference read: the Goose save of those values is the Go
// reference's encode of them, byte for byte, and the Goose load of the batch
// lands them. (A save and a read are compared on the READ values because the
// form does not round-trip every value: an empty string elides and reads back
// as its declared default, in every leg, §3.3.) A corpus file carrying a
// non-finite float is refused by name on the save, as the Go writer refuses it.
func TestGooseMessageFormAgreesWithTheReference(t *testing.T) {
	t.Parallel()
	corpus := fixedCorpus(t)
	records := fixedManifestRecords(t, corpus)
	manifest := gooseVersionManifest(t, corpus)
	for _, v := range fixedVectors {
		v := v
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			u := fixedUnit(t, v.schema)
			n := records[v.file]
			if n < 1 {
				t.Fatalf("the corpus manifest gives %s no records", v.file)
			}
			file, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				t.Fatal(err)
			}
			batch, refused := gooseMsgFromFixed(t, u, v.root, file, n, false)
			if batch == nil {
				nonFinite := false
				for _, val := range manifest.line(t, v.file).values {
					low := strings.ToLower(val.value)
					nonFinite = nonFinite || strings.HasPrefix(low, "nan") || strings.HasPrefix(low, "inf") || strings.HasPrefix(low, "-inf") || strings.HasPrefix(low, "+inf")
				}
				if refused != "non_finite_float" || !nonFinite {
					t.Fatalf("the save refused %s on a file whose manifest carries no non-finite float", refused)
				}
				// and the rest of the value still crosses the reference
				batch, refused = gooseMsgFromFixed(t, u, v.root, file, n, true)
				if batch == nil {
					t.Fatalf("the save refused %s with its floats made finite", refused)
				}
			}
			insts := gooseGoDecode(t, u, v.root, ir.TableAnnouncement(u), batch, n)
			want, err := tablewire.EncodeMessages(tabletext.NewModel(u), insts)
			if err != nil {
				t.Fatalf("the Go reference refuses to encode what it read: %v", err)
			}
			var values [][]string
			for _, inst := range insts {
				values = append(values, gooseInstAssign(t, "", inst))
			}
			r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: v.root, values: values, data: batch})
			r.agrees(t, want, n)
		})
	}
}

// gooseMsgShapes is every payload shape the corpus roots above do not reach,
// in one fixed root: a compressed float and a ranged float clamp, a plain
// float64, raw signed integers at every width, bits(N), a flags mask, an
// array of uint8 (which aligns), an array of enums, an array of unions with a
// scalar arm and a payload-free arm, a nested type, and an optional table.
const gooseMsgShapes = `package msgshapes

enum Mode { Idle, Run, Halt }
flags Caps { Read, Write, Exec }

type Inner
{
    a int16 = -3
    b bool
}

union Choice
{
    count  uint16
    word   string(6)
    idle
    inner  Inner
}

fixed table Opt { x int32 = 1 }

fixed table Shapes
{
    aim    float32 = 0 | min = -1, max = 1, resolution = 0.01
    exact  float64
    tiny   int8
    big    int64
    flags4 bits(4)
    caps   Caps
    level  uint8 = 3 | min = 1, max = 9
    raw    [5]uint8
    modes  [..3]Mode
    picks  [3]Choice
    inner  Inner
    opt    ?Opt
    tail   int32 = 7
}
`

// TestGooseMessageFormCarriesEveryShape builds two values in Goose, saves them
// as one batch, and holds the batch to the Go reference: the reference reads
// it, its encode of what it read is the Goose save byte for byte, and the
// Goose load lands what it read.
func TestGooseMessageFormCarriesEveryShape(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	path := filepath.Join(dir, "Shapes.schema")
	if err := os.WriteFile(path, []byte(gooseMsgShapes), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("load the shapes schema: %v", err)
	}
	values := [][]string{
		{
			".Aim = schema_rt::fixed_bits_f32(0x3f000000)", // 0.5
			".Exact = schema_rt::fixed_bits_f64(0x400921fb54442d18)",
			".Tiny = -128", ".Big = (-9223372036854775807 - 1)",
			".Flags4 = 9", ".Caps = 5", ".Level = 9",
			".Raw[0] = 1", ".Raw[1] = 255", ".Raw[4] = 7",
			".ModesCount = 3", ".Modes[0] = 1", ".Modes[1] = 0", ".Modes[2] = 3",
			".Picks[0].Type = 1", ".Picks[0].Count = 65535",
			".Picks[1].Type = 3",
			".Picks[2].Type = 2", ".Picks[2].Word[0] = 104", ".Picks[2].Word[1] = 105", ".Picks[2].WordLength = 2",
			".Inner.A = 300", ".Inner.B = true",
			".OptPresent = true", ".Opt.X = 1",
			".Tail = -7",
		},
		{
			".Aim = schema_rt::fixed_bits_f32(0xbf800000)", // -1, the bottom of the range
			".Tiny = 127", ".Big = 9223372036854775807",
			".Picks[1].Type = 4", ".Picks[1].Inner.A = -1",
		},
	}
	saved := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "Shapes", values: values})
	if saved.saveReason != "ok" {
		t.Fatalf("the save refused %s:\n%s", saved.saveReason, saved.raw)
	}
	batch, err := hex.DecodeString(saved.saved)
	if err != nil {
		t.Fatal(err)
	}
	insts := gooseGoDecode(t, u, "Shapes", ir.TableAnnouncement(u), batch, len(values))
	want, err := tablewire.EncodeMessages(tabletext.NewModel(u), insts)
	if err != nil {
		t.Fatalf("the Go reference refuses to encode what it read: %v", err)
	}
	var back [][]string
	for _, inst := range insts {
		back = append(back, gooseInstAssign(t, "", inst))
	}
	r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "Shapes", values: back, data: batch})
	r.agrees(t, want, len(values))
	if saved.saved != hex.EncodeToString(want) {
		t.Fatalf("the Goose save of its own values is not the reference's\n goose %s\n go    %x", saved.saved, want)
	}
}

// TestGooseMessageFormRefusesByName holds the batch surface's answers
// (docs/SPEC-TABLES.md §3.3, THE BATCH SURFACE'S FIVE ANSWERS) to the Go
// engine's names: M above 256 on the write, M above the caller's capacity on
// the read (nothing decoded, the wire's count reported), a form byte this
// reader does not carry, bytes after the pad, and damage inside a body
// delivering the bodies before it.
func TestGooseMessageFormRefusesByName(t *testing.T) {
	t.Parallel()
	var row gooseMsgRow
	for _, r := range gooseMsgRows(t) {
		if r.name == "store_full" {
			row = r
		}
	}
	if row.name == "" {
		t.Fatal("the manifest carries no store_full message row")
	}
	u := gooseMsgUnitDir(t, row.unitDir)
	golden := gooseMsgFile(t, row.message)
	one := gooseInstAssign(t, "", gooseGoDecode(t, u, row.root, gooseMsgFile(t, row.conn), golden, 1)[0])

	t.Run("write_batch_too_large", func(t *testing.T) {
		t.Parallel()
		r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: row.root, values: make([][]string, 257)})
		if r.saveN != -1 || r.measureN != -1 || r.saveReason != "batch_too_large" {
			t.Fatalf("257 bodies: save %d measure %d %s, want -1 batch_too_large", r.saveN, r.measureN, r.saveReason)
		}
	})
	three := gooseMsgProbe(t, gooseMsgCase{unit: u, root: row.root, values: [][]string{one, one, one}})
	batch3, err := hex.DecodeString(three.saved)
	if err != nil || three.saveReason != "ok" {
		t.Fatalf("a batch of three did not save: %s", three.raw)
	}
	cases := []struct {
		name      string
		data      []byte
		capacity  int
		n         int64
		reason    string
		malformed bool
		count     int64
	}{
		{"read_capacity_short", batch3, 2, -1, "batch_too_large", false, 3},
		{"read_three", batch3, 3, 3, "ok", false, 0},
		{"newer_form", append([]byte{1}, golden[1:]...), 1, -1, "newer_form", false, 0},
		{"bytes_after_the_pad", append(append([]byte(nil), golden...), 0), 1, 1, "ok", true, 0},
		{"damage_in_body_one", golden[:len(golden)-2], 1, 0, "ok", true, 0},
		{"damage_in_body_three", batch3[:len(batch3)-2], 3, 2, "ok", true, 0},
		{"empty", []byte{}, 1, -1, "ok", true, 0},
	}
	for _, c := range cases {
		c := c
		t.Run(c.name, func(t *testing.T) {
			t.Parallel()
			r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: row.root, values: [][]string{one, one, one}, data: c.data, capacity: c.capacity})
			if r.loadN != c.n || r.loadReason != c.reason || r.loadMalformed != c.malformed || r.loadCount != c.count {
				t.Fatalf("LoadMessages answered n=%d %s malformed=%v count=%d, want n=%d %s malformed=%v count=%d",
					r.loadN, r.loadReason, r.loadMalformed, r.loadCount, c.n, c.reason, c.malformed, c.count)
			}
			for k := range r.got {
				if r.got[k] != r.want[k] {
					t.Fatalf("body %d of the bodies delivered is not the value\n got  %s\n want %s", k, r.got[k], r.want[k])
				}
			}
		})
	}
}

// TestGooseMessageFormCarriesWideKinds crosses the wide kinds with the Go
// reference: the fixed-point family at every width, ranged, the 128-bit
// fixed and integer lanes (a negative value's borrow and carry across them),
// a ranged int64, and fixed-point arrays — Scalars2's SimState, two bodies.
func TestGooseMessageFormCarriesWideKinds(t *testing.T) {
	t.Parallel()
	u := fixedUnit(t, "test/tables/Scalars2.schema")
	values := [][]string{
		{
			".Tilt = -128", ".Angle = -180", ".Position = -65536000",
			".Reach.lo = 18446744041941551616", ".Reach.hi = 18446744073709551615", // -500000 x 2^16
			".Ratio = 255", ".Speed = 655360", ".Frames = 4294967295",
			".Mass.lo = 131071967232", ".Mass.hi = 0", // 1999999.5 x 2^16
			".Flux = -5000000000",
			".Energy.lo = 4999999999", ".Energy.hi = 0",
			".Samples[0] = -524288", ".Samples[2] = 524288",
			".WeightsCount = 2", ".Weights[0] = 25600", ".Weights[1] = 1",
		},
		{
			// THE 128-BIT CLAMP: both lanes past the declared max, an int128
			// (6000000000 over 5000000000) and a fixed(112, 16) (1000001 x 2^16
			// over 1000000 x 2^16); the offsets ride, and the read clamps each
			".Energy.lo = 6000000000", ".Energy.hi = 0",
			".Reach.lo = 65536065536", ".Reach.hi = 0",
		},
		{
			".Energy.lo = 18446744068709551616", ".Energy.hi = 18446744073709551615", // -5000000000
			".Reach.lo = 65536000000", ".Reach.hi = 0",
		},
	}
	saved := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "SimState", values: values})
	if saved.saveReason != "ok" {
		t.Fatalf("the save refused %s:\n%s", saved.saveReason, saved.raw)
	}
	batch, err := hex.DecodeString(saved.saved)
	if err != nil {
		t.Fatal(err)
	}
	insts := gooseGoDecode(t, u, "SimState", ir.TableAnnouncement(u), batch, len(values))
	want, err := tablewire.EncodeMessages(tabletext.NewModel(u), insts)
	if err != nil {
		t.Fatalf("the Go reference refuses to encode what it read: %v", err)
	}
	// Ratio's raw 255 is 15.9375 under a declared max of 15: the offset rides
	// (its width spells it) and both readers clamp it to 240 (§3.3, a ranged
	// offset above the sender's max reconstructs and clamps), so the save of
	// what was READ is the reference's encode, and the Goose load of the
	// original batch lands the clamped value with the clamp counted.
	var back [][]string
	for _, inst := range insts {
		back = append(back, gooseInstAssign(t, "", inst))
	}
	r := gooseMsgProbe(t, gooseMsgCase{unit: u, root: "SimState", values: back, data: batch})
	r.agrees(t, want, len(values))
	if r.loadClamped != 3 {
		t.Fatalf("the load counted %d clamps, want 3 (Ratio, and the 128-bit Energy and Reach)", r.loadClamped)
	}
}
