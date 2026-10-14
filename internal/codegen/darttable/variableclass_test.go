package darttable

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestVariableClassBeyondCardConstructsDart pins the Dart leg's carry of the
// variable class beyond the card constructs (docs/SPEC-TABLES.md §2.1, §2.5,
// §2.9, §3, §3.1, §16.7), every case held BYTE FOR BYTE against the Go oracle
// (internal/tablewire, internal/tabletext):
//
//   - the pointer corpus (tables/pointers): table pointers *T over the flat
//     node table — a tree, a shared node, an empty graph, a 260-node chain — a
//     variable table nested by value, a bounded array of variable tables and a
//     fixed table nested by value;
//   - the byte-buffer corpus (tables/blobs): *bytes and *string blobs, small,
//     empty, large, shared, and the eight- and sixteen-byte string edges;
//   - the unbounded-array corpus (tables/lists): []T of fixed tables, []*T
//     pointer arrays with a shared node and a null slot, []int32 scalars, a
//     pointer array numbered before a pointer field, lists nested in lists, a
//     pointed-at holder of a list, a bounded-to-unbounded migration, and a
//     list element with a 200-byte bytes(256);
//   - the rest of the scalar kinds (uint8..uint64, int8, int16 with a range,
//     float32, float64, bits(N), bool), bytes(N), declared defaults, arrays of
//     enums, bools, floats and narrow ints, a bounded array of a fixed table,
//     and a shared node defined and named through §16.7's `&node`, in a probe
//     unit whose wires and texts the oracle writes here;
//   - BLOB IDENTITY (§2.5, §3.1): two *string and two *bytes slots holding
//     equal bytes, empty and not, are two records, as the oracle writes them;
//     one blob two slots name (blob_shared, and a *string the oracle shares in
//     probe_shared_string) stays one record through a Dart load and save, and
//     the text writer REFUSES it, as the oracle's does (§16.7);
//   - the ESCAPE KIND 31 (§3): at a field the reader cannot name (unknown), at
//     a field it names and as an array's element kind (kind_mismatch each), the
//     report compared with the oracle's and the re-save with the oracle's;
//   - a DECLARED MINIMUM, [2..4]uint8, at 0, 1 and 4 elements (the bytes of a
//     [..4]) and at 5 from the wire and from the text (one clamped, the prefix
//     kept), each against the oracle's report and save;
//   - a DATA CYCLE (§3.1, §16.7): a Leaf naming itself and two naming each
//     other refuse the save, the measure and the text, as the oracle refuses
//     them, and a label named inside its own definition is a text the reader
//     refuses;
//   - a bytes(16384) of 127, 128, 129 and 16384 bytes: a bytes(N) body's L is
//     the body's own length, whatever width the count's LEB128 takes;
//   - LONG CHAINS of 5,000 and 100,000 Leaves, as the oracle saves them: the
//     load, the save and the measure never throw (the numbering and the text's
//     count hold their own stack), the bytes are the oracle's, and ToJson
//     answers null past the depth cap, as the oracle's writer refuses.
type vcCase struct {
	Name       string
	Root       string
	RefuseText bool
	Same       []string
	wire       []byte
	text       []byte
}

func TestVariableClassBeyondCardConstructsDart(t *testing.T) {
	t.Parallel()
	dartBin := dartBinary(t)
	repo, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()

	type vcUnit struct {
		name  string
		unit  *ir.Unit
		cases []*vcCase
	}
	corpus := func(name, root string, text bool, same ...string) *vcCase {
		wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		c := &vcCase{Name: name, Root: root, wire: wire, Same: same}
		if text {
			c.text, err = os.ReadFile(filepath.Join(repo, "testdata", "conformance", "tables", "json", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
		}
		return c
	}
	units := []*vcUnit{
		{"graphdemo", vcLoadUnit(t, repo, "tables/pointers"), []*vcCase{
			corpus("graph_tree", "Scene", true),
			corpus("graph_shared", "Scene", true, "head", "alias"),
			corpus("graph_empty", "Scene", true),
			corpus("graph_deep", "Scene", false),
		}},
		{"blobdemo", vcLoadUnit(t, repo, "tables/blobs"), []*vcCase{
			corpus("blob_small", "Catalog", true),
			corpus("blob_empty", "Catalog", true),
			corpus("blob_large", "Catalog", true),
			func() *vcCase {
				c := corpus("blob_shared", "Catalog", false, "thumb", "alias")
				c.RefuseText = true
				return c
			}(),
			corpus("blob_str8", "Catalog", true),
			corpus("blob_str16", "Catalog", true),
		}},
		{"listdemo", vcLoadUnit(t, repo, "tables/lists"), []*vcCase{
			corpus("list_tables", "Save", true),
			corpus("list_scalars", "Save", true),
			corpus("list_empty", "Save", true),
			corpus("list_erased", "Save", true),
			corpus("list_shared", "Album", true),
			corpus("list_before_pointer", "Album", true),
			corpus("list_nested", "Sheet", true),
			corpus("list_migrates", "Unbounded", true),
		}},
	}

	// THE CORPUS IS THE ORACLE'S: decode, re-encode the same bytes, and write
	// the committed text, so every byte the Dart leg is held to below is one
	// the Go oracle produces.
	for _, u := range units {
		m := tabletext.NewModel(u.unit)
		for _, c := range u.cases {
			inst := m.New(m.Lookup(c.Root))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, inst, c.wire, &rep); !ok || err != nil {
				t.Fatalf("%s: the oracle did not decode its own wire: %v", c.Name, err)
			}
			again, err := tablewire.Encode(m, inst)
			if err != nil || !bytes.Equal(again, c.wire) {
				t.Fatalf("%s: the oracle does not re-encode the committed wire (%v)", c.Name, err)
			}
			if c.RefuseText {
				if _, err := m.Write(inst); err == nil {
					t.Fatalf("%s: the oracle's writer did not refuse the value", c.Name)
				}
			}
			if c.text != nil {
				// the canonical text ends with exactly one newline (§16.1)
				text, err := m.Write(inst)
				if err != nil || !bytes.Equal(append(text, '\n'), c.text) {
					t.Fatalf("%s: the oracle does not write the committed text (%v)", c.Name, err)
				}
			}
		}
	}

	// THE PROBE UNIT: what the corpus does not reach, its wires and texts made
	// by the oracle from the texts below.
	probe := buildUnit(t, variableClassProbeSrc)
	pm := tabletext.NewModel(probe)
	probeCases := []*vcCase{}
	for _, pc := range variableClassProbeTexts {
		inst := pm.New(pm.Lookup("Probe"))
		var rep tabletext.Report
		if !pm.Read(inst, []byte(pc.text), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("%s: the oracle did not read the probe text cleanly: %+v", pc.name, rep)
		}
		wire, err := tablewire.Encode(pm, inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not encode: %v", pc.name, err)
		}
		text, err := pm.Write(inst)
		if err != nil {
			t.Fatalf("%s: the oracle did not write: %v", pc.name, err)
		}
		if strings.Contains(pc.name, "_strings") || strings.Contains(pc.name, "_bytes") {
			g, err := tablewire.Number(pm, inst)
			if err != nil || len(g.Records()) != 2 {
				t.Fatalf("%s: the oracle does not number two blob records for two equal slots (%v)", pc.name, err)
			}
		}
		probeCases = append(probeCases, &vcCase{Name: pc.name, Root: "Probe", wire: wire, text: append(text, '\n')})
	}
	// ONE *string BLOB TWO SLOTS NAME: the oracle shares the note's blob into
	// note2, numbers ONE record for the two, and its writer refuses the text
	// (§2.5, §16.7). A Dart load must preserve that sharing so its save writes
	// the blob once and its checked text writer refuses.
	{
		inst := pm.New(pm.Lookup("Probe"))
		var rep tabletext.Report
		if !pm.Read(inst, []byte(`{"note": "shared"}`), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("probe_shared_string: the oracle did not read the text cleanly: %+v", rep)
		}
		var note, note2 *tabletext.Field
		for i := range inst.Fields {
			switch inst.Fields[i].Def.Name {
			case "note":
				note = &inst.Fields[i]
			case "note2":
				note2 = &inst.Fields[i]
			}
		}
		note2.Cell.Blob = note.Cell.Blob
		g, err := tablewire.Number(pm, inst)
		if err != nil || len(g.Records()) != 1 {
			t.Fatalf("probe_shared_string: the oracle does not number one record for one shared blob (%v)", err)
		}
		wire, err := tablewire.Encode(pm, inst)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := pm.Write(inst); err == nil {
			t.Fatal("probe_shared_string: the oracle's writer did not refuse a shared blob")
		}
		probeCases = append(probeCases, &vcCase{Name: "probe_shared_string", Root: "Probe", wire: wire, RefuseText: true, Same: []string{"note", "note2"}})
	}
	// A LONG CHAIN (§3.1, §16.7): 5,000 and 100,000 Leaves, each naming the
	// next. The oracle saves them, and its text writer refuses past the depth
	// cap. A Dart load, save and measure must not throw on them — the numbering
	// holds its own stack — and ToJsonChecked answers null, as the oracle's
	// writer refuses.
	for _, n := range []int{5000, 100000} {
		probeCases = append(probeCases, variableClassChain(t, pm, n))
	}
	// A bytes(N) BODY'S L IS ITS OWN LENGTH (§2.5, §3): at 127 bytes the count
	// is one LEB128 byte, at 128 and past it two, and at 16384 three.
	for _, n := range []int{127, 128, 129, 16384} {
		data := make([]byte, n)
		for i := range data {
			data[i] = byte(i % 251)
		}
		wire := variableClassWideWire(t, probe, data)
		back := pm.New(pm.Lookup("Probe"))
		var backRep tabletext.Report
		if ok, err := tablewire.Decode(pm, back, wire, &backRep); !ok || err != nil || backRep != (tabletext.Report{}) {
			t.Fatalf("probe_wide_%d: the oracle does not read back the wire cleanly: %v %+v", n, err, backRep)
		}
		written, err := pm.Write(back)
		if err != nil {
			t.Fatal(err)
		}
		probeCases = append(probeCases, &vcCase{Name: fmt.Sprintf("probe_wide_%d", n), Root: "Probe", wire: wire, text: append(written, '\n')})
	}
	units = append(units, &vcUnit{"classprobe", probe, probeCases})

	// THE ESCAPE KIND (§3): one wire, kind 31 at three positions, and the
	// oracle's report and re-save for it.
	escape := variableClassEscapeWire(t, probe)
	escInst := pm.New(pm.Lookup("Probe"))
	var escRep tabletext.Report
	if ok, err := tablewire.Decode(pm, escInst, escape, &escRep); !ok || err != nil {
		t.Fatalf("the oracle refused the escape wire: %v", err)
	}
	if escRep.Unknown != 1 || escRep.KindMismatch != 2 || escRep.Malformed {
		t.Fatalf("the oracle's escape report is not the spec's (one unknown, two kind mismatches, nothing malformed): %+v", escRep)
	}
	escSaved, err := tablewire.Encode(pm, escInst)
	if err != nil {
		t.Fatal(err)
	}

	// FIVE ELEMENTS IN A [2..4]uint8 (§4): on the wire the bounded prefix is
	// kept and one clamped counts, and from the text the same.
	repCount := func(r tabletext.Report) string {
		return fmt.Sprintf("%d,%d,%d,%d,%d,%v", r.Unknown, r.KindMismatch, r.Widened, r.Clamped, r.Duplicate, r.Malformed)
	}
	leastWire := variableClassLeastWire(t, probe)
	leastInst := pm.New(pm.Lookup("Probe"))
	var leastRep tabletext.Report
	if ok, err := tablewire.Decode(pm, leastInst, leastWire, &leastRep); !ok || err != nil {
		t.Fatalf("the oracle refused the five-element wire: %v", err)
	}
	if leastRep.Clamped != 1 || leastRep.Malformed {
		t.Fatalf("the oracle does not clamp five elements of a [2..4] array once: %+v", leastRep)
	}
	leastSaved, err := tablewire.Encode(pm, leastInst)
	if err != nil {
		t.Fatal(err)
	}
	const leastText = `{"least": [1, 2, 3, 4, 5]}`
	leastTextInst := pm.New(pm.Lookup("Probe"))
	var leastTextRep tabletext.Report
	if !pm.Read(leastTextInst, []byte(leastText), &leastTextRep) || leastTextRep.Clamped != 1 {
		t.Fatalf("the oracle does not read five elements of a [2..4] array as one clamp: %+v", leastTextRep)
	}
	leastTextWire, err := tablewire.Encode(pm, leastTextInst)
	if err != nil {
		t.Fatal(err)
	}

	// A DATA CYCLE (§3.1, §16.7): the oracle refuses a Leaf whose next is itself
	// at save and at write, and so must the Dart leg.
	{
		inst := pm.New(pm.Lookup("Probe"))
		var rep tabletext.Report
		if !pm.Read(inst, []byte(`{"head": {"id": 1, "name": "a", "next": null}}`), &rep) {
			t.Fatalf("the oracle did not read the cycle's seed: %+v", rep)
		}
		var leaf *tabletext.Instance
		for i := range inst.Fields {
			if inst.Fields[i].Def.Name == "head" {
				leaf = inst.Fields[i].Cell.Node
			}
		}
		for i := range leaf.Fields {
			if leaf.Fields[i].Def.Name == "next" {
				leaf.Fields[i].Cell.Node = leaf
			}
		}
		if _, err := tablewire.Encode(pm, inst); err == nil {
			t.Fatal("the oracle saved a data cycle")
		}
		if _, err := pm.Write(inst); err == nil {
			t.Fatal("the oracle wrote a data cycle as text")
		}
	}

	// A LABEL NAMED INSIDE ITS OWN DEFINITION (§16.7):
	const selfRefText = `{"head": {"&node": 1, "id": 1, "next": {"&node": 1}}}`
	var selfRefRep tabletext.Report
	if pm.Read(pm.New(pm.Lookup("Probe")), []byte(selfRefText), &selfRefRep) || !selfRefRep.Malformed {
		t.Fatalf("the oracle did not refuse a label named inside its own definition: %+v", selfRefRep)
	}

	// THE DART PROBE: every unit's generated <Home>Wire.dart, one driver over
	// all of them.
	var imports, calls strings.Builder
	var want []string
	for _, u := range units {
		files, err := Generate(u.unit)
		if err != nil {
			t.Fatalf("%s: the variable class was refused: %v", u.name, err)
		}
		wireFile := runtimeHome(u.unit) + "Wire.dart"
		data, ok := files[wireFile]
		if !ok || len(data) == 0 {
			t.Fatalf("%s: no %s was generated", u.name, wireFile)
		}
		if err := os.WriteFile(filepath.Join(work, wireFile), data, 0o600); err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&imports, "import %q as %s;\n", wireFile, u.name)
		for _, c := range u.cases {
			wirePath := filepath.Join(work, c.Name+".wire")
			if err := os.WriteFile(wirePath, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, "WIRE "+c.Name+" OK", "MEASURE "+c.Name+" OK")
			jsonArg := "null"
			if c.text != nil {
				jsonPath := filepath.Join(work, c.Name+".json")
				if err := os.WriteFile(jsonPath, c.text, 0o600); err != nil {
					t.Fatal(err)
				}
				jsonArg = fmt.Sprintf("%q", jsonPath)
				want = append(want, "JSONW "+c.Name+" OK", "JSONR "+c.Name+" OK")
			}
			if c.RefuseText {
				want = append(want, "JSONREFUSE "+c.Name+" OK")
			}
			sameCheck := "null"
			if len(c.Same) == 2 {
				want = append(want, "SAME "+c.Name+" OK")
				sameCheck = fmt.Sprintf("(v) => v.%s != null && identical(v.%s, v.%s)",
					dartName(c.Same[0]), dartName(c.Same[0]), dartName(c.Same[1]))
			}
			droot := dartName(c.Root)
			fmt.Fprintf(&calls,
				"  runCase<%s.%s>(%q, %q, %s, %v, () => %s.TableWireReport(), "+
					"(w, r) => %s.%sWireLoadChecked(w, r as %s.TableWireReport), "+
					"%s.%sWireSaveChecked, %s.%sWireSaveMeasure, %s.%sToJsonChecked, "+
					"(s) { final r = %s.TableTextReport(); final v = %s.%sFromJson(s, r); return (v, _cleanText(r.unknown, r.kindMismatch, r.clamped, r.duplicate, r.malformed)); }, "+
					"(r) { final tr = r as %s.TableWireReport; return _cleanWire(tr.unknown, tr.kindMismatch, tr.widened, tr.clamped, tr.duplicate, tr.malformed, tr.refused); }, "+
					"%s);\n",
				u.name, c.Root, c.Name, wirePath, jsonArg, c.RefuseText, u.name,
				u.name, droot, u.name,
				u.name, droot, u.name, droot, u.name, droot,
				u.name, u.name, droot,
				u.name,
				sameCheck,
			)
		}
	}

	// bounded-to-unbounded migration check on listdemo: Bounded saved, loaded as Unbounded
	{
		m := tabletext.NewModel(units[2].unit)
		bInst := m.New(m.Lookup("Bounded"))
		var rep tabletext.Report
		if !m.Read(bInst, []byte(`{"tags": [10, 20, 30]}`), &rep) || rep != (tabletext.Report{}) {
			t.Fatalf("bounded_to_unbounded: oracle read failed: %+v", rep)
		}
		bWire, err := tablewire.Encode(m, bInst)
		if err != nil {
			t.Fatal(err)
		}
		bWirePath := filepath.Join(work, "bounded_to_unbounded.wire")
		if err := os.WriteFile(bWirePath, bWire, 0o600); err != nil {
			t.Fatal(err)
		}
		want = append(want, "MIGRATE bounded_to_unbounded OK")
		fmt.Fprintf(&calls, "  runMigrate(%q);\n", bWirePath)
	}

	escPath := filepath.Join(work, "escape.wire")
	escSavedPath := filepath.Join(work, "escape_saved.wire")
	if err := os.WriteFile(escPath, escape, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(escSavedPath, escSaved, 0o600); err != nil {
		t.Fatal(err)
	}
	want = append(want, fmt.Sprintf("ESCAPE %d,%d,%d,%d,%d,%v SAVED OK",
		escRep.Unknown, escRep.KindMismatch, escRep.Widened, escRep.Clamped, escRep.Duplicate, escRep.Malformed))

	leastFiles := map[string][]byte{
		"least5.wire": leastWire, "least5_saved.wire": leastSaved, "least5_text.wire": leastTextWire,
	}
	for name, data := range leastFiles {
		if err := os.WriteFile(filepath.Join(work, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	want = append(want,
		"LEASTWIRE "+repCount(leastRep)+" SAVED OK",
		"LEASTTEXT "+repCount(leastTextRep)+" SAVED OK",
		"CYCLE self save OK", "CYCLE self measure OK", "CYCLE self text OK",
		"CYCLE pair save OK", "CYCLE pair text OK",
		fmt.Sprintf("SELFREF malformed %v", selfRefRep.Malformed))

	fmt.Fprintf(&calls, "  runEscape(%q, %q);\n", escPath, escSavedPath)
	fmt.Fprintf(&calls, "  runLeast(%q, %q, %q, %q);\n",
		filepath.Join(work, "least5.wire"),
		filepath.Join(work, "least5_saved.wire"),
		leastText,
		filepath.Join(work, "least5_text.wire"))
	fmt.Fprintf(&calls, "  runCycles(%q);\n", selfRefText)

	var driver strings.Builder
	driver.WriteString("import 'dart:io';\nimport 'dart:typed_data';\n")
	driver.WriteString(imports.String())
	driver.WriteString(variableClassProbeDartHelpers)
	driver.WriteString("void main() {\n")
	driver.WriteString(calls.String())
	driver.WriteString("}\n")

	probePath := filepath.Join(work, "probe.dart")
	if err := os.WriteFile(probePath, []byte(driver.String()), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBin, "--enable-asserts", probePath)
	cmd.Dir = work
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("the variable class probe did not run: %v\n%s", err, out)
	}
	got := map[string]bool{}
	for _, line := range strings.Split(string(out), "\n") {
		got[strings.TrimSpace(line)] = true
	}
	for _, w := range want {
		if !got[w] {
			t.Errorf("the probe did not print %q", w)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	} else {
		t.Logf("probe output (%d expected lines, all present):\n%s", len(want), out)
	}
}

// vcLoadUnit parses one corpus unit directory through the parser and checker.
func vcLoadUnit(t *testing.T, repo, dir string) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(filepath.Join(repo, dir))
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		path := filepath.Join(repo, dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(e.Name(), data)
		if len(perrs) > 0 {
			t.Fatalf("parse %s: %v", path, perrs[0])
		}
		files = append(files, check.SourceFile{
			Path: path, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: data, AST: f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("check %s: %v", dir, cerrs[0])
	}
	return u
}

func variableClassEscapeWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	probe := u.Tables["Probe"]
	var tail, small *ir.Field
	for _, f := range probe.Fields {
		switch f.Name {
		case "tail":
			tail = f
		case "small":
			small = f
		}
	}
	if tail == nil || small == nil {
		t.Fatal("the probe declares no tail or small")
	}
	ids := []uint64{ir.TableWireId("from_a_later_major"), ir.TableFieldWireId(tail), ir.TableFieldWireId(small)}
	esc, arr := byte(ir.TableKindEscape), byte(ir.TableKindArray)
	w := []byte{byte(ir.TableWireForm)}
	w = append(w, 1, esc, 2, 0xAA, 0xBB) // unnameable: skipped by L, unknown
	w = append(w, 2, esc, 1, 0x00)       // named: kind_mismatch
	w = append(w, 3, arr, 3, esc, 1, 0)  // element kind 31: kind_mismatch
	w = append(w, 0)
	for _, id := range ids {
		w = binary.LittleEndian.AppendUint64(w, id)
	}
	return binary.LittleEndian.AppendUint64(w, uint64(len(ids)))
}

func variableClassChain(t *testing.T, pm *tabletext.Model, n int) *vcCase {
	t.Helper()
	inst := pm.New(pm.Lookup("Probe"))
	field := func(in *tabletext.Instance, name string) *tabletext.Field {
		for i := range in.Fields {
			if in.Fields[i].Def.Name == name {
				return &in.Fields[i]
			}
		}
		t.Fatalf("no field %s", name)
		return nil
	}
	var prev *tabletext.Instance
	for k := 0; k < n; k++ {
		leaf := pm.New(pm.Lookup("Leaf"))
		field(leaf, "id").Cell.U = uint64(k + 1)
		if prev == nil {
			field(inst, "head").Cell.Node = leaf
		} else {
			field(prev, "next").Cell.Node = leaf
		}
		prev = leaf
	}
	wire, err := tablewire.Encode(pm, inst)
	if err != nil {
		t.Fatalf("the oracle did not save a %d-node chain: %v", n, err)
	}
	g, err := tablewire.Number(pm, inst)
	if err != nil || len(g.Records()) != n {
		t.Fatalf("the oracle did not number %d records for a %d-node chain (%v)", n, n, err)
	}
	if _, err := pm.Write(inst); err == nil {
		t.Fatalf("the oracle's writer did not refuse a %d-node chain past the depth cap", n)
	}
	return &vcCase{Name: fmt.Sprintf("probe_chain_%d", n), Root: "Probe", wire: wire, RefuseText: true}
}

func variableClassLeastWire(t *testing.T, u *ir.Unit) []byte {
	t.Helper()
	var least *ir.Field
	for _, f := range u.Tables["Probe"].Fields {
		if f.Name == "least" {
			least = f
		}
	}
	if least == nil {
		t.Fatal("the probe declares no least")
	}
	w := []byte{byte(ir.TableWireForm)}
	w = append(w, 1, byte(ir.TableKindArray), 7, byte(ir.TableKindU8), 5, 1, 2, 3, 4, 5)
	w = append(w, 0)
	w = binary.LittleEndian.AppendUint64(w, ir.TableFieldWireId(least))
	return binary.LittleEndian.AppendUint64(w, 1)
}

func variableClassWideWire(t *testing.T, u *ir.Unit, data []byte) []byte {
	t.Helper()
	var wide *ir.Field
	for _, f := range u.Tables["Probe"].Fields {
		if f.Name == "wide" {
			wide = f
		}
	}
	if wide == nil {
		t.Fatal("the probe declares no wide")
	}
	appendLeb := func(b []byte, v uint64) []byte {
		for {
			c := byte(v & 0x7F)
			v >>= 7
			if v == 0 {
				return append(b, c)
			}
			b = append(b, c|0x80)
		}
	}
	body := []byte{byte(ir.TableKindU8)}
	body = appendLeb(body, uint64(len(data)))
	body = append(body, data...)
	w := []byte{byte(ir.TableWireForm), 1, byte(ir.TableKindArray)}
	w = appendLeb(w, uint64(len(body)))
	w = append(w, body...)
	w = append(w, 0)
	w = binary.LittleEndian.AppendUint64(w, ir.TableFieldWireId(wide))
	return binary.LittleEndian.AppendUint64(w, 1)
}

const variableClassProbeSrc = `package classprobe

enum Mood { calm, angry, sleepy }

fixed table Spot
{
    x   float32
    y   float32 = 1.5
    tag uint16
}

table Leaf
{
    id   uint32
    name string(8)
    next *Leaf
}

table Probe
{
    u8v    uint8 = 7
    u16v   uint16
    u32v   uint32
    u64v   uint64
    i8v    int8 = -3
    i16v   int16 | min = -100, max = 100
    f32v   float32 = 0.5
    f64v   float64
    flag   bool = true
    b10    bits(10)
    raw    bytes(6)
    blob   *bytes
    note   *string
    blob2  *bytes
    note2  *string
    mood   Mood = angry
    moods  []Mood
    flags  []bool
    nums   []float64
    small  []int8
    spots  [..3]Spot
    spot   Spot
    leaves []*Leaf
    head   *Leaf
    tail   int32
    least  [2..4]uint8
    wide   bytes(16384)
}
`

var variableClassProbeTexts = []struct{ name, text string }{
	{"probe_default", `{}`},
	{"probe_full", `{
  "u8v": 200, "u16v": 65535, "u32v": 4000000000, "u64v": 18446744073709551615,
  "i8v": -128, "i16v": -100, "f32v": 3.25, "f64v": -1e-300, "flag": false,
  "b10": 1023, "raw": "AQIDBAUG", "blob": "3q2+7w==", "note": "nöte",
  "mood": "sleepy", "moods": ["calm", "None", "angry"], "flags": [true, false, true],
  "nums": [0.1, -2.5, 1e300], "small": [-1, 0, 127],
  "spots": [{"x": 1, "y": 2, "tag": 3}, {"x": 0.5, "y": 1.5, "tag": 0}],
  "spot": {"x": -4, "y": 1.5, "tag": 9},
  "leaves": [{"&node": 1, "id": 1, "name": "one", "next": {"id": 2, "name": "two", "next": null}}, null, {"&node": 1}],
  "head": {"&node": 1},
  "tail": -7,
  "least": [1, 2, 3, 4]
}`},
	{"probe_least_one", `{"least": [9]}`},
	{"probe_equal_strings", `{"note": "hi", "note2": "hi"}`},
	{"probe_empty_strings", `{"note": "", "note2": ""}`},
	{"probe_equal_bytes", `{"blob": "AQI=", "blob2": "AQI="}`},
	{"probe_empty_bytes", `{"blob": "", "blob2": ""}`},
	{"probe_edges", `{
  "u8v": 0, "f32v": 0, "flag": true, "b10": 0, "raw": "", "blob": "", "note": "",
  "mood": "None", "nums": [], "spots": [{"x": 0, "y": 1.5, "tag": 0}],
  "spot": {"x": 0, "y": 1.5, "tag": 0},
  "leaves": [null], "head": {"id": 0, "name": "", "next": null},
  "least": []
}`},
}

const variableClassProbeDartHelpers = `
bool _sameBytes(Uint8List? a, Uint8List b) {
  if (a == null || a.length != b.length) return false;
  for (var i = 0; i < a.length; i++) {
    if (a[i] != b[i]) return false;
  }
  return true;
}

String _firstDiff(Uint8List? a, Uint8List b) {
  if (a == null) return 'null';
  final n = a.length < b.length ? a.length : b.length;
  for (var i = 0; i < n; i++) {
    if (a[i] != b[i]) {
      return 'at $i got ${a[i]} want ${b[i]} (len ${a.length} vs ${b.length})';
    }
  }
  if (a.length != b.length) {
    return 'len ${a.length} vs ${b.length}';
  }
  return 'equal';
}

bool _cleanWire(int u, int k, int w, int c, int d, bool m, bool r) =>
    u == 0 && k == 0 && w == 0 && c == 0 && d == 0 && !m && !r;

bool _cleanText(int u, int k, int c, int d, bool m) =>
    u == 0 && k == 0 && c == 0 && d == 0 && !m;

void runCase<T>(
  String name,
  String wirePath,
  String? jsonPath,
  bool refuseText,
  Object Function() makeReport,
  T? Function(Uint8List, Object) load,
  Uint8List? Function(T) save,
  int Function(T) measure,
  String? Function(T) toJson,
  (T, bool) Function(String) fromJson,
  bool Function(Object) isClean,
  bool Function(T)? sameCheck,
) {
  final wire = File(wirePath).readAsBytesSync();
  final rep = makeReport();
  final v = load(wire, rep);
  if (v == null) {
    stdout.writeln('WIRE $name REFUSED');
    return;
  }
  final saved = save(v);
  if (_sameBytes(saved, wire) && isClean(rep)) {
    stdout.writeln('WIRE $name OK');
  } else {
    stdout.writeln('WIRE $name DIFF ${_firstDiff(saved, wire)}');
  }
  final measured = measure(v);
  stdout.writeln('MEASURE $name ${saved != null && measured == saved.length ? "OK" : "DIFF $measured"}');
  if (sameCheck != null) {
    stdout.writeln('SAME $name ${sameCheck(v) ? "OK" : "DIFF"}');
  }
  if (refuseText) {
    final t = toJson(v);
    stdout.writeln('JSONREFUSE $name ${t == null ? "OK" : "WROTE ${t.length}"}');
  }
  if (jsonPath != null) {
    final want = File(jsonPath).readAsStringSync();
    final t = toJson(v);
    if (t == want) {
      stdout.writeln('JSONW $name OK');
    } else {
      stdout.writeln('JSONW $name DIFF\n--- got ---\n$t--- want ---\n$want');
    }
    final (v2, ok2) = fromJson(want);
    final s2 = save(v2);
    if (_sameBytes(s2, wire) && ok2) {
      stdout.writeln('JSONR $name OK');
    } else {
      stdout.writeln('JSONR $name DIFF ${_firstDiff(s2, wire)} clean=$ok2');
    }
  }
}

void runMigrate(String wirePath) {
  final wire = File(wirePath).readAsBytesSync();
  final r = listdemo.TableWireReport();
  final u = listdemo.unboundedWireLoadChecked(wire, r);
  if (u != null && u.tags.length == 3 && u.tags[0] == 10 && u.tags[1] == 20 && u.tags[2] == 30) {
    final s = listdemo.unboundedWireSaveChecked(u);
    if (_sameBytes(s, wire)) {
      stdout.writeln('MIGRATE bounded_to_unbounded OK');
      return;
    }
  }
  stdout.writeln('MIGRATE bounded_to_unbounded DIFF');
}

void runEscape(String wirePath, String savedPath) {
  final wire = File(wirePath).readAsBytesSync();
  final want = File(savedPath).readAsBytesSync();
  final r = classprobe.TableWireReport();
  final v = classprobe.probeWireLoadChecked(wire, r);
  if (v == null) {
    stdout.writeln('ESCAPE REFUSED');
  } else {
    final again = classprobe.probeWireSaveChecked(v);
    final counts = '${r.unknown},${r.kindMismatch},${r.widened},${r.clamped},${r.duplicate},${r.malformed}';
    stdout.writeln('ESCAPE $counts SAVED ${_sameBytes(again, want) ? "OK" : "DIFF ${_firstDiff(again, want)}"}');
  }
}

void runLeast(String wirePath, String savedPath, String text, String textWirePath) {
  final want = File(savedPath).readAsBytesSync();
  final r = classprobe.TableWireReport();
  final v = classprobe.probeWireLoadChecked(File(wirePath).readAsBytesSync(), r);
  if (v == null) {
    stdout.writeln('LEASTWIRE REFUSED');
  } else {
    final again = classprobe.probeWireSaveChecked(v);
    final counts = '${r.unknown},${r.kindMismatch},${r.widened},${r.clamped},${r.duplicate},${r.malformed}';
    stdout.writeln('LEASTWIRE $counts SAVED ${_sameBytes(again, want) ? "OK" : "DIFF ${_firstDiff(again, want)}"}');
  }
  final tw = File(textWirePath).readAsBytesSync();
  final r2 = classprobe.TableTextReport();
  final s2 = classprobe.probeWireSaveChecked(classprobe.probeFromJson(text, r2));
  final counts2 = '${r2.unknown},${r2.kindMismatch},0,${r2.clamped},${r2.duplicate},${r2.malformed}';
  stdout.writeln('LEASTTEXT $counts2 SAVED ${_sameBytes(s2, tw) ? "OK" : "DIFF ${_firstDiff(s2, tw)}"}');
}

void runCycles(String selfRef) {
  final p = classprobe.Probe();
  final a = classprobe.Leaf()..id = 1..name = 'a';
  a.next = a;
  p.head = a;
  stdout.writeln('CYCLE self save ${classprobe.probeWireSaveChecked(p) == null ? "OK" : "WROTE"}');
  stdout.writeln('CYCLE self measure ${classprobe.probeWireSaveMeasure(p) == -1 ? "OK" : "MEASURED"}');
  stdout.writeln('CYCLE self text ${classprobe.probeToJsonChecked(p) == null ? "OK" : "WROTE"}');
  final q = classprobe.Probe();
  final b = classprobe.Leaf(), c = classprobe.Leaf();
  b.next = c;
  c.next = b;
  q.leaves = [b];
  stdout.writeln('CYCLE pair save ${classprobe.probeWireSaveChecked(q) == null ? "OK" : "WROTE"}');
  stdout.writeln('CYCLE pair text ${classprobe.probeToJsonChecked(q) == null ? "OK" : "WROTE"}');
  final r = classprobe.TableTextReport();
  classprobe.probeFromJson(selfRef, r);
  stdout.writeln('SELFREF malformed ${r.malformed}');
}
`
