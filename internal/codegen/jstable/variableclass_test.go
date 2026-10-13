package jstable

import (
	"bytes"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestVariableClassBeyondCardConstructsJavaScript pins the JavaScript leg's
// carry of the variable class beyond the card constructs (docs/SPEC-TABLES.md
// §2.1, §2.5, §2.9, §3, §3.1, §16.7), every case held BYTE FOR BYTE against the
// Go oracle (internal/tablewire, internal/tabletext):
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
//     pointed-at holder of a list, and a bounded-to-unbounded migration;
//   - the rest of the scalar kinds (uint8..uint64, int8, int16 with a range,
//     float32, float64, bits(N), bool), bytes(N), declared defaults, arrays of
//     enums, bools, floats and narrow ints, a bounded array of a fixed table,
//     and a shared node defined and named through §16.7's `&node`, in a probe
//     unit whose wires and texts the oracle writes here;
//   - BLOB IDENTITY (§2.5, §3.1): two *string and two *bytes slots holding
//     equal bytes, empty and not, are two records, as the oracle writes them;
//     one blob two slots name (blob_shared, and a *string the oracle shares in
//     probe_shared_string) stays one record through a JavaScript load and save,
//     and the text writer REFUSES it, as the oracle's does (§16.7);
//   - the ESCAPE KIND 31 (§3): at a field the reader cannot name (unknown), at a
//     field it names and as an array's element kind (kind_mismatch each), the
//     report compared with the oracle's and the re-save with the oracle's;
//   - a DECLARED MINIMUM, [2..4]uint8, at 0, 1 and 4 elements (the bytes of a
//     [..4]) and at 5 from the wire and from the text (one clamped, the prefix
//     kept), each against the oracle's report and save;
//   - a DATA CYCLE (§3.1, §16.7): a Leaf naming itself and two naming each other
//     refuse the save, the measure and the text, as the oracle refuses them, and
//     a label named inside its own definition is a text the reader refuses;
//   - LONG CHAINS of 5,000 and 100,000 Leaves, as the oracle saves them: the
//     load, the save and the measure never throw (the numbering and the text's
//     count hold their own stack), the bytes are the oracle's, and ToJson
//     answers null past the depth cap, as the oracle's writer refuses.
//
// Each wire is loaded and saved back (the bytes must not move and the report
// must be clean), written as text (the text must be the oracle's), and read
// back from the text and saved (the bytes must be the wire again). A corpus
// wire is first held to the oracle itself: the oracle decodes it, re-encodes
// the same bytes, and writes the committed text.
// vcCase is one instance the JavaScript probe loads, saves and writes.
type vcCase struct {
	Name       string `json:"name"`
	Module     string `json:"module"`
	Root       string `json:"root"`
	Wire       string `json:"wire"`
	Text       string `json:"text"` // "" for a wire-only instance (no-text)
	RefuseText bool   `json:"refuseText"`
	// Same names two slots that must land as ONE object after the load (the
	// shared node or blob the wire numbers once, §3.1)
	Same []string `json:"same"`
	wire []byte
	text []byte
}

func TestVariableClassBeyondCardConstructsJavaScript(t *testing.T) {
	t.Parallel()
	node := jsNode(t)
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
		{"graphdemo", loadUnitPaths(t, []string{filepath.Join(repo, "tables", "pointers")}), []*vcCase{
			corpus("graph_tree", "Scene", true),
			corpus("graph_shared", "Scene", true, "head", "alias"),
			corpus("graph_empty", "Scene", true),
			corpus("graph_deep", "Scene", false),
		}},
		{"blobdemo", loadUnitPaths(t, []string{filepath.Join(repo, "tables", "blobs")}), []*vcCase{
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
		{"listdemo", loadUnitPaths(t, []string{filepath.Join(repo, "tables", "lists")}), []*vcCase{
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
	// the committed text, so every byte the JavaScript leg is held to below is
	// one the Go oracle produces.
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
	probe := unitFrom(t, variableClassProbeSrc)
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
			// the case is only worth its name if the oracle writes TWO blob
			// records for the two equal slots (§3.1)
			g, err := tablewire.Number(pm, inst)
			if err != nil || len(g.Records()) != 2 {
				t.Fatalf("%s: the oracle does not number two blob records for two equal slots (%v)", pc.name, err)
			}
		}
		probeCases = append(probeCases, &vcCase{Name: pc.name, Root: "Probe", wire: wire, text: append(text, '\n')})
	}
	// ONE *string BLOB TWO SLOTS NAME: the oracle shares the note's blob into
	// note2, numbers ONE record for the two, and its writer refuses the text
	// (§2.5, §16.7). A JavaScript load must hand both slots one String object,
	// or its save would write the blob twice.
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
	// cap. A JavaScript load, save and measure must not throw on them — the
	// numbering holds its own stack — and ToJson answers null, as the oracle's
	// writer refuses.
	for _, n := range []int{5000, 100000} {
		probeCases = append(probeCases, variableClassChain(t, pm, n))
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

	// FIVE ELEMENTS IN A [2..4]uint8 (§4): on the wire the bounded prefix is kept
	// and one clamped counts, and from the text the same; both against the
	// oracle's report and the oracle's save of what it kept.
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
	// at save and at write, and so must the JavaScript leg (built in the driver)
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

	// A LABEL NAMED INSIDE ITS OWN DEFINITION is the cycle the text spells, and
	// the reader refuses it (§16.7): the oracle's verdict, and the leg's below
	const selfRefText = `{"head": {"&node": 1, "id": 1, "next": {"&node": 1}}}`
	var selfRefRep tabletext.Report
	if pm.Read(pm.New(pm.Lookup("Probe")), []byte(selfRefText), &selfRefRep) || !selfRefRep.Malformed {
		t.Fatalf("the oracle did not refuse a label named inside its own definition: %+v", selfRefRep)
	}

	// THE JAVASCRIPT PROBE: every unit's generated module, one driver over all
	// of them, the cases as data.
	var all []*vcCase
	var want []string
	for _, u := range units {
		files, err := generateVariableFiles(u.unit, nil)
		if err != nil {
			t.Fatalf("%s: the variable class was refused: %v", u.name, err)
		}
		module := filepath.Join(work, u.name, runtimeHome(u.unit)+"Variable.js")
		if err := os.MkdirAll(filepath.Dir(module), 0o700); err != nil {
			t.Fatal(err)
		}
		data, ok := files[runtimeHome(u.unit)+"Variable.js"]
		if !ok {
			t.Fatalf("%s: no variable module was generated", u.name)
		}
		if err := os.WriteFile(module, data, 0o600); err != nil {
			t.Fatal(err)
		}
		for _, c := range u.cases {
			c.Module = module
			c.Wire = filepath.Join(work, c.Name+".wire")
			if err := os.WriteFile(c.Wire, c.wire, 0o600); err != nil {
				t.Fatal(err)
			}
			want = append(want, "WIRE "+c.Name+" OK", "MEASURE "+c.Name+" OK")
			if c.text != nil {
				c.Text = filepath.Join(work, c.Name+".json")
				if err := os.WriteFile(c.Text, c.text, 0o600); err != nil {
					t.Fatal(err)
				}
				want = append(want, "JSONW "+c.Name+" OK", "JSONR "+c.Name+" OK")
			}
			if c.RefuseText {
				want = append(want, "JSONREFUSE "+c.Name+" OK")
			}
			if len(c.Same) != 0 {
				want = append(want, "SAME "+c.Name+" OK")
			}
			all = append(all, c)
		}
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
	files := map[string][]byte{
		"least5.wire": leastWire, "least5_saved.wire": leastSaved, "least5_text.wire": leastTextWire,
	}
	for name, data := range files {
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

	probeModule := filepath.Join(work, "classprobe", runtimeHome(probe)+"Variable.js")
	spec, err := json.Marshal(map[string]any{
		"cases":  all,
		"escape": map[string]string{"module": probeModule, "wire": escPath, "saved": escSavedPath},
		"least": map[string]string{"module": probeModule, "wire": filepath.Join(work, "least5.wire"),
			"saved": filepath.Join(work, "least5_saved.wire"), "text": leastText, "textWire": filepath.Join(work, "least5_text.wire")},
		"cycle": map[string]string{"module": probeModule, "selfRef": selfRefText},
	})
	if err != nil {
		t.Fatal(err)
	}
	specPath := filepath.Join(work, "spec.json")
	if err := os.WriteFile(specPath, spec, 0o600); err != nil {
		t.Fatal(err)
	}
	script := filepath.Join(work, "probe.mjs")
	if err := os.WriteFile(script, []byte(variableClassProbeDriver), 0o600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, script, specPath)
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

// variableClassEscapeWire builds one Probe wire carrying the escape kind 31
// (docs/SPEC-TABLES.md §3) at three positions: a field this reader cannot name
// (unknown), the named int32 `tail` (kind_mismatch), and the element kind of
// the named []int8 `small` (kind_mismatch).
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

// variableClassChain is a Probe whose head is a chain of n Leaves (id k, each
// naming the next), as the ORACLE saves it: the case's wire is
// tablewire.Encode's, and the oracle's text writer must refuse the chain
// past the depth cap, so the case expects the JavaScript ToJson to refuse too.
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

// variableClassLeastWire builds one Probe wire whose [2..4]uint8 `least`
// carries FIVE elements (docs/SPEC-TABLES.md §4): kind 14, element kind u8, a
// count of 5, then the five bytes.
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

// variableClassProbeSrc is the variable class the corpus does not reach.
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
}
`

// variableClassProbeTexts are the probe instances, as the oracle reads them.
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
	// A DECLARED MINIMUM MOVES NOTHING ON THE TABLE WIRE: [2..4]T holding one
	// element is [..4]T's bytes (the oracle reads ArrayMin nowhere); none is
	// probe_edges, four is probe_full, and five is the clamp case below
	{"probe_least_one", `{"least": [9]}`},
	// A BLOB IS A NODE, NUMBERED BY OBJECT (docs/SPEC-TABLES.md §2.5, §3.1):
	// two slots holding EQUAL bytes in two blobs are two records, which the
	// oracle writes and a by-content numbering would merge into one
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

// variableClassProbeDriver runs every case against the generated modules and
// prints one line per verdict: WIRE (load, save, the bytes and a clean
// report), JSONW (the text), JSONR (the text read back and saved), JSONREFUSE
// (the text writer refuses a shared blob), SAME (two slots naming one node or
// blob are one object after the load), and ESCAPE (the escape kind's report and
// re-save).
const variableClassProbeDriver = `
import { readFileSync } from "node:fs";
import { pathToFileURL } from "node:url";

const spec = JSON.parse(readFileSync(process.argv[2], "utf8"));
const same = (a, b) => a !== null && a.length === b.length && a.every((x, i) => x === b[i]);
const clean = (r) => r.unknown === 0 && r.kindMismatch === 0 && r.widened === 0 && r.clamped === 0 && r.duplicate === 0 && !r.malformed;
const firstDiff = (a, b) => { if (a === null) { return "null"; } for (let i = 0; i < Math.max(a.length, b.length); i++) { if (a[i] !== b[i]) { return "at " + i + " got " + a[i] + " want " + b[i] + " (len " + a.length + " vs " + b.length + ")"; } } return "equal"; };

for (const c of spec.cases) {
  const m = await import(pathToFileURL(c.module).href);
  const wire = new Uint8Array(readFileSync(c.wire));
  const rep = new m.TableVariableReport();
  const v = m[c.root + "Load"](wire, rep);
  if (v === null) { console.log("WIRE " + c.name + " REFUSED " + JSON.stringify(rep)); continue; }
  const saved = m[c.root + "Save"](v);
  if (same(saved, wire) && clean(rep)) { console.log("WIRE " + c.name + " OK"); }
  else { console.log("WIRE " + c.name + " DIFF " + firstDiff(saved, wire) + " report " + JSON.stringify(rep)); }
  const measured = m[c.root + "Measure"](v);
  console.log("MEASURE " + c.name + (saved !== null && measured === saved.length ? " OK" : " DIFF " + measured));
  if (c.same !== null && c.same.length === 2) {
    const a = v[c.same[0]], b = v[c.same[1]];
    console.log("SAME " + c.name + (a !== null && a === b ? " OK" : " DIFF"));
  }
  if (c.refuseText) {
    const t = m[c.root + "ToJson"](v);
    console.log("JSONREFUSE " + c.name + (t === null ? " OK" : " WROTE " + t.length));
  }
  if (c.text !== "") {
    const want = readFileSync(c.text, "utf8");
    const t = m[c.root + "ToJson"](v);
    if (t === want) { console.log("JSONW " + c.name + " OK"); }
    else { console.log("JSONW " + c.name + " DIFF\n--- got ---\n" + t + "--- want ---\n" + want); }
    const r2 = new m.TableVariableReport();
    const v2 = m[c.root + "FromJson"](want, r2);
    const s2 = m[c.root + "Save"](v2);
    if (same(s2, wire) && clean(r2)) { console.log("JSONR " + c.name + " OK"); }
    else { console.log("JSONR " + c.name + " DIFF " + firstDiff(s2, wire) + " report " + JSON.stringify(r2)); }
  }
}

{
  const e = spec.escape;
  const m = await import(pathToFileURL(e.module).href);
  const wire = new Uint8Array(readFileSync(e.wire));
  const want = new Uint8Array(readFileSync(e.saved));
  const r = new m.TableVariableReport();
  const v = m.ProbeLoad(wire, r);
  if (v === null) { console.log("ESCAPE REFUSED"); }
  else {
    const again = m.ProbeSave(v);
    console.log("ESCAPE " + [r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed].join(",") + " SAVED " + (same(again, want) ? "OK" : "DIFF " + firstDiff(again, want)));
  }
}

const counts = (r) => [r.unknown, r.kindMismatch, r.widened, r.clamped, r.duplicate, r.malformed].join(",");

{
  // five elements of a [2..4]uint8: from the wire and from the text
  const e = spec.least;
  const m = await import(pathToFileURL(e.module).href);
  const want = new Uint8Array(readFileSync(e.saved));
  const r = new m.TableVariableReport();
  const v = m.ProbeLoad(new Uint8Array(readFileSync(e.wire)), r);
  if (v === null) { console.log("LEASTWIRE REFUSED"); }
  else { const again = m.ProbeSave(v); console.log("LEASTWIRE " + counts(r) + " SAVED " + (same(again, want) ? "OK" : "DIFF " + firstDiff(again, want))); }
  const tw = new Uint8Array(readFileSync(e.textWire));
  const r2 = new m.TableVariableReport();
  const s2 = m.ProbeSave(m.ProbeFromJson(e.text, r2));
  console.log("LEASTTEXT " + counts(r2) + " SAVED " + (same(s2, tw) ? "OK" : "DIFF " + firstDiff(s2, tw)));
}

{
  // a data cycle refuses the save and the text; a label named inside its own
  // definition is a text the reader refuses
  const e = spec.cycle;
  const m = await import(pathToFileURL(e.module).href);
  const p = new m.Probe();
  const a = new m.Leaf();
  a.id = 1; a.name = "a"; a.next = a;
  p.head = a;
  console.log("CYCLE self save " + (m.ProbeSave(p) === null ? "OK" : "WROTE"));
  console.log("CYCLE self measure " + (m.ProbeMeasure(p) === -1 ? "OK" : "MEASURED"));
  console.log("CYCLE self text " + (m.ProbeToJson(p) === null ? "OK" : "WROTE"));
  const q = new m.Probe();
  const b = new m.Leaf(), c = new m.Leaf();
  b.next = c; c.next = b;
  q.leaves = [b];
  console.log("CYCLE pair save " + (m.ProbeSave(q) === null ? "OK" : "WROTE"));
  console.log("CYCLE pair text " + (m.ProbeToJson(q) === null ? "OK" : "WROTE"));
  const r = new m.TableVariableReport();
  m.ProbeFromJson(e.selfRef, r);
  console.log("SELFREF malformed " + r.malformed);
}
`
