package rusttable

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The variable-shape cells under the STRICT read (one read: the whole value
// or a refusal): vt/fixed-class, vt/defaults-and-table-rename, vt/renames,
// vt/wstring, vt/variable-class, vt/enum-keyed-arrays, vt/unbounded-arrays,
// vt/pointer-arrays, vt/union-arrays, vt/optional-arrays and vt/maps for Rust.
// A fixed-class table (Spot, Config) rides by value inside a variable root,
// defaults elide, a renamed table/field/enum variant keeps its wire id,
// wstring rides kind 33, and every collection body shorter than its 2-byte
// header — plus an unreachable node, a second node table and an out-of-range node
// index — refuses the whole load (plain and retaining) and clears the
// retaining store. The fixed root in the same unit saves form 3 and its
// fixed read refuses form 1 as previous_form. Every wire's verdict is the
// Go oracle's (tablewire.Decode) before the generated Rust is asked.

const rustVariableShapeCellsSchema = `package vtshape

enum Slot
{
    alpha
    beta
    gamma | was = "third"
}

fixed table Spot
{
    x   float32
    y   float32 = 1.5
    tag uint16
}

fixed table Config
{
    scale   float32 = 1.0
    name    string(16) = "hi"
    caption wstring(8)
    spot    Spot
}

table Leaf | was = "Node"
{
    id   uint32
    name string(8)
    next *Leaf
}

union Choice
{
    xs      [..4]int32
    leaf    *Leaf
    spot    Spot
    caption wstring(8)
}

table Root
{
    spot    Spot
    cfg     Config
    renamed int32 | was = "former"
    text    string(16)
    caption wstring(16)
    mood    Slot = beta
    keyed   [Slot]int32
    xs      []int32
    raw     bytes(4)
    bounded [..3]Spot
    ptrs    []*Leaf
    choice  Choice
    choices []Choice
    opt     ?[..4]int32
    lookup  map[int32]int32
    head    *Leaf
    blob    *bytes
    note    *string
}
`

type vtShapeVector struct {
	name string
	wire []byte
	want bool
}

func vtShapeJSONWire(t *testing.T, u *ir.Unit, text string) []byte {
	t.Helper()
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	var rep tabletext.Report
	if !m.Read(inst, []byte(text), &rep) || rep.Refused {
		t.Fatalf("oracle did not read the rich text: %+v", rep)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("oracle did not encode: %v", err)
	}
	return wire
}

func buildVTShapeVectors(t *testing.T, u *ir.Unit) []vtShapeVector {
	t.Helper()
	m := tabletext.NewModel(u)

	makeWire := func(build func(ref func(uint64) []byte) []byte) []byte {
		var ids []uint64
		ref := func(id uint64) []byte {
			for i, v := range ids {
				if v == id {
					return binary.AppendUvarint(nil, uint64(i+1))
				}
			}
			ids = append(ids, id)
			return binary.AppendUvarint(nil, uint64(len(ids)))
		}
		unk := append(append(ref(ir.TableWireId("unknown_prefix")), byte(ir.TableKindI32)), 7, 0, 0, 0)
		body := make([]byte, len(unk))
		copy(body, unk)
		body = append(body, build(ref)...)
		body = append(body, 0)
		wire := append([]byte{1}, body...)
		for _, id := range ids {
			wire = binary.LittleEndian.AppendUint64(wire, id)
		}
		return binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	}
	framed := func(ref func(uint64) []byte, name string, kind byte, payload []byte) []byte {
		out := append(ref(ir.TableWireId(name)), kind)
		out = binary.AppendUvarint(out, uint64(len(payload)))
		return append(out, payload...)
	}
	ptrField := func(ref func(uint64) []byte, name string, index uint64) []byte {
		out := append(ref(ir.TableWireId(name)), byte(ir.TableKindPointer))
		return binary.AppendUvarint(out, index)
	}
	type rec struct {
		tid  uint64
		body []byte
	}
	nodeTable := func(ref func(uint64) []byte, recs ...rec) []byte {
		nt := binary.AppendUvarint(nil, uint64(len(recs)))
		for _, r := range recs {
			nt = append(nt, ref(r.tid)...)
			nt = binary.AppendUvarint(nt, uint64(len(r.body)))
			nt = append(nt, r.body...)
		}
		out := append(ref(math.MaxUint64), byte(ir.TableKindString))
		out = binary.AppendUvarint(out, uint64(len(nt)))
		return append(out, nt...)
	}
	leafTID := ir.TableWireId(u.Tables["Leaf"].WireName())

	vectors := []vtShapeVector{
		{name: "valid-rich", want: true, wire: vtShapeJSONWire(t, u, `{"spot":{"x":1.5,"y":2.5,"tag":7},"cfg":{"scale":2,"name":"cfg","caption":"wide","spot":{"x":3,"y":4,"tag":8}},"renamed":42,"text":"hello","caption":"мир","mood":"gamma","keyed":{"alpha":1,"beta":2,"gamma":3},"xs":[5,6],"raw":"AAECAw==","bounded":[{"x":1,"y":2,"tag":1}],"ptrs":[{"id":1,"name":"a","next":null},null],"choice":{"spot":{"x":9,"y":10,"tag":3}},"choices":[{"xs":[1,2]},{"caption":"hi"}],"opt":[7,8],"lookup":{"3":30,"5":50},"head":{"id":9,"name":"h","next":{"id":10,"name":"n","next":null}},"blob":"AAEC","note":"note text"}`)},
		{name: "valid-defaults", want: true, wire: vtShapeJSONWire(t, u, `{}`)},
		{name: "short-array-0", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "xs", byte(ir.TableKindArray), nil) })},
		{name: "short-array-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "xs", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
		})},
		{name: "short-unbounded-ptr-array-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "ptrs", byte(ir.TableKindArray), []byte{byte(ir.TableKindPointer)})
		})},
		{name: "short-bounded-fixed-array-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "bounded", byte(ir.TableKindArray), []byte{byte(ir.TableKindTable)})
		})},
		{name: "short-bytes-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "raw", byte(ir.TableKindArray), []byte{byte(ir.TableKindU8)})
		})},
		{name: "short-opt-array-0", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "opt", byte(ir.TableKindArray), nil) })},
		{name: "short-opt-array-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "opt", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
		})},
		{name: "short-keyed-0", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "keyed", byte(ir.TableKindKeyed), nil) })},
		{name: "short-keyed-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "keyed", byte(ir.TableKindKeyed), []byte{byte(ir.TableKindI32)})
		})},
		{name: "short-map-0", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return framed(ref, "lookup", byte(ir.TableKindArray), nil) })},
		{name: "short-map-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "lookup", byte(ir.TableKindArray), []byte{byte(ir.TableKindTable)})
		})},
		{name: "short-union-array-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			arm := append(ref(ir.TableWireId("xs")), byte(ir.TableKindArray), 1, byte(ir.TableKindI32))
			return append(append(ref(ir.TableWireId("choice")), byte(ir.TableKindUnion)), arm...)
		})},
		{name: "short-union-wstring-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			arm := append(ref(ir.TableWireId("caption")), byte(ir.TableKindWstring), 1, byte(ir.TableKindWstring))
			return append(append(ref(ir.TableWireId("choice")), byte(ir.TableKindUnion)), arm...)
		})},
		{name: "short-choice-array-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "choices", byte(ir.TableKindArray), []byte{byte(ir.TableKindUnion)})
		})},
		{name: "short-unknown-array-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "unknown_arr", byte(ir.TableKindArray), []byte{byte(ir.TableKindI32)})
		})},
		{name: "short-unknown-keyed-1", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			return framed(ref, "unknown_keyed", byte(ir.TableKindKeyed), []byte{byte(ir.TableKindI32)})
		})},
		{name: "unreachable-table-node", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return nodeTable(ref, rec{leafTID, []byte{0}}) })},
		{name: "unreachable-blob", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return nodeTable(ref, rec{ir.BytesWireTypeId, []byte{1, 2, 3}}) })},
		{name: "node-table-twice", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte {
			var b []byte
			b = append(b, ptrField(ref, "head", 2)...)
			b = append(b, nodeTable(ref, rec{leafTID, []byte{0}})...)
			b = append(b, nodeTable(ref, rec{leafTID, []byte{0}})...)
			return b
		})},
		{name: "node-index-out-of-range", want: false, wire: makeWire(func(ref func(uint64) []byte) []byte { return ptrField(ref, "head", 99) })},
	}

	for _, tc := range vectors {
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(u.Tables["Root"]), tc.wire, &rep)
		if ok != tc.want {
			t.Fatalf("oracle verdict mismatch on %s: got ok=%v err=%v rep=%+v, want %v", tc.name, ok, err, rep, tc.want)
		}
		if !tc.want && !tablewire.ReadRefusal(err) {
			t.Fatalf("oracle refusal on %s is not a ReadRefusal: %v", tc.name, err)
		}
	}
	return vectors
}

func runRustVTShapeProbe(t *testing.T, edit func(string) string) (string, error) {
	t.Helper()
	u := unitFrom(t, rustVariableShapeCellsSchema)
	vectors := buildVTShapeVectors(t, u)

	var body strings.Builder
	body.WriteString("let mut failed = false;\n")
	for _, tc := range vectors {
		fmt.Fprintf(&body, `{
    let wire: &[u8] = &[%s];
    for retaining in [false, true] {
        let mut store = TableRetain { bytes: Vec::with_capacity(4096), ids: Vec::with_capacity(256) };
        let mut report = TableReport::default();
        let value = if retaining { root_load_retain(wire, &mut store, &mut report) } else { root_load(wire, &mut report) };
        let want = %t;
        if value.is_some() != want || (!want && !(report.damaged || report.refused)) || (want && (report.damaged || report.refused)) {
            println!("FAIL: %s (retaining={retaining}): some={} damaged={} refused={}", value.is_some(), report.damaged, report.refused);
            failed = true;
        }
        if !want && (!store.bytes.is_empty() || !store.ids.is_empty()) {
            println!("FAIL: %s (retaining={retaining}): store not cleared on refusal");
            failed = true;
        }
        if want {
            if let Some(v) = &value {
                if root_save(v) != wire {
                    println!("FAIL: %s (retaining={retaining}): save does not reproduce the wire");
                    failed = true;
                }
            }
        }
    }
}
`, rustBytes(tc.wire), tc.want, tc.name, tc.name, tc.name)
	}
	// The fixed root: save writes form 3, the fixed read reads it back, and
	// form 1 at that read is previous_form with nothing decoded.
	body.WriteString(`{
    let mut vals = [ConfigRow::default()];
    vals[0].scale = 2.5;
    vals[0].spot.tag = 9;
    let need = config_fixed_measure(1);
    let mut buf = vec![0u8; need];
    if config_fixed_save(&vals, &mut buf) != Some(need) || buf[0] != TABLE_FIXED_FORM {
        println!("FAIL: fixed-root-form3: save did not write form 3");
        failed = true;
    } else {
        let mut out = [ConfigRow::default()];
        let mut plan = vec![TableFixedEntry::default(); 4096];
        let mut remap = vec![0u16; 4096];
        let mut report = TableFixedReport::default();
        if config_fixed_load(&mut out, &buf, &mut plan, &mut remap, &mut report) != Some(1) || out[0].scale != 2.5 || out[0].spot.tag != 9 {
            println!("FAIL: fixed-root-form3: form-3 file did not read back");
            failed = true;
        }
        let mut form1 = buf.clone();
        form1[0] = 1;
        let mut out1 = [ConfigRow::default()];
        let mut freport = TableFixedReport::default();
        if config_fixed_load(&mut out1, &form1, &mut plan, &mut remap, &mut freport).is_some() || !freport.refused || freport.reason != TableFixedReason::PreviousForm {
            println!("FAIL: fixed-root-form1: form 1 was not refused as previous_form refused={} reason={:?}", freport.refused, freport.reason);
            failed = true;
        }
    }
}
`)
	body.WriteString("if failed { std::process::exit(1); }\nprintln!(\"VT_SHAPE_CELLS_OK\");\n")

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	if edit != nil {
		patched := 0
		for name, data := range files {
			if strings.HasSuffix(name, "_table.rs") || strings.HasSuffix(name, "_fixed.rs") {
				s := string(data)
				if ns := edit(s); ns != s {
					files[name] = []byte(ns)
					patched++
				}
			}
		}
		if patched == 0 {
			t.Fatal("negative control edit matched nothing in generated files")
		}
	}
	return runRustGeneratedFilesProbe(t, files, body.String())
}

func TestRustVariableShapeCellsStrict(t *testing.T) {
	slowtest.Gate(t, "rustc (variable shape cells strict read)")
	keyedRequireRustc(t)
	out, err := runRustVTShapeProbe(t, nil)
	if err != nil || !strings.Contains(out, "VT_SHAPE_CELLS_OK") {
		t.Fatalf("variable shape cells probe failed: %v\n%s", err, out)
	}
}

func TestRustVariableShapeCellsStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "rustc (negative control for variable shape cells strict read)")
	keyedRequireRustc(t)

	// Clean first: the unmutated artifact must hold, or a red below is noise.
	if out, err := runRustVTShapeProbe(t, nil); err != nil || !strings.Contains(out, "VT_SHAPE_CELLS_OK") {
		t.Fatalf("clean artifact should hold the strict read: %v\n%s", err, out)
	}

	// Plant the short-body tolerance back into the real generated table code.
	outShort, errShort := runRustVTShapeProbe(t, func(src string) string {
		return strings.ReplaceAll(src, "} else { report.damaged = true; }", "} else {} // SABOTAGED")
	})
	if errShort == nil {
		t.Fatalf("probe stayed green when the short-body refusal was sabotaged:\n%s", outShort)
	}
	for _, wantFail := range []string{"short-array-0", "short-keyed-0", "short-map-0", "short-bytes-1"} {
		if !strings.Contains(outShort, "FAIL: "+wantFail) {
			t.Errorf("short-body sabotage did not fail on %s:\n%s", wantFail, outShort)
		}
	}

	// Plant the unreachable-node tolerance back into the real generated code.
	const unreachSig = "if !report.cycle_on_save && nodes.has_unreachable(&edges) { report.damaged = true; return None; }"
	outUnreach, errUnreach := runRustVTShapeProbe(t, func(src string) string {
		return strings.ReplaceAll(src, unreachSig, "if false && !report.cycle_on_save && nodes.has_unreachable(&edges) { report.damaged = true; return None; } // SABOTAGED")
	})
	if errUnreach == nil {
		t.Fatalf("probe stayed green when the unreachable-node refusal was sabotaged:\n%s", outUnreach)
	}
	for _, wantFail := range []string{"unreachable-table-node", "unreachable-blob"} {
		if !strings.Contains(outUnreach, "FAIL: "+wantFail) {
			t.Errorf("unreachable sabotage did not fail on %s:\n%s", wantFail, outUnreach)
		}
	}

	// Plant the fixed-root form confusion back into the real fixed code:
	// form 1 named newer_form instead of previous_form.
	outFixed, errFixed := runRustVTShapeProbe(t, func(src string) string {
		return strings.ReplaceAll(src, "1 => TableFixedReason::PreviousForm", "1 => TableFixedReason::NewerForm")
	})
	if errFixed == nil {
		t.Fatalf("probe stayed green when previous_form was renamed:\n%s", outFixed)
	}
	if !strings.Contains(outFixed, "FAIL: fixed-root-form1") {
		t.Errorf("fixed-form sabotage did not fail on fixed-root-form1:\n%s", outFixed)
	}
}
