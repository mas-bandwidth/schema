package darttable

import (
	"encoding/binary"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// The Dart variable-shape cells under the STRICT read (one read: the whole
// value or a refusal): vt/fixed-class, vt/defaults-and-table-rename,
// vt/renames, vt/wstring, vt/variable-class, vt/enum-keyed-arrays,
// vt/unbounded-arrays, vt/pointer-arrays, vt/union-arrays, vt/optional-arrays
// and vt/maps for Dart. A fixed-class table (Spot, Config) rides by value
// inside a variable root, defaults elide, a renamed table/field/enum variant
// keeps its wire id, wstring rides kind 33, and every collection body shorter
// than its 2-byte header — plus an unreachable node, a second node table and
// an out-of-range node index — refuses the whole load (plain and retaining).
// The fixed root in the same unit saves form 3 and its fixed read refuses form
// 1 as previous_form. Every wire's verdict is the Go oracle's
// (tablewire.Decode) before the generated Dart is asked. This is a functional
// test: it compiles and runs the generated Dart (the leg's own toolchain).

const dartVariableShapeCellsSchema = `package vtshape

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

type dartShapeVector struct {
	name string
	wire []byte
	want bool
}

func dartShapeJSONWire(t *testing.T, u *ir.Unit, text string) []byte {
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

func buildDartShapeVectors(t *testing.T, u *ir.Unit) []dartShapeVector {
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
		body := append(unk, build(ref)...)
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

	vectors := []dartShapeVector{
		{name: "valid-rich", want: true, wire: dartShapeJSONWire(t, u, `{"spot":{"x":1.5,"y":2.5,"tag":7},"cfg":{"scale":2,"name":"cfg","caption":"wide","spot":{"x":3,"y":4,"tag":8}},"renamed":42,"text":"hello","caption":"мир","mood":"gamma","keyed":{"alpha":1,"beta":2,"gamma":3},"xs":[5,6],"raw":"AAECAw==","bounded":[{"x":1,"y":2,"tag":1}],"ptrs":[{"id":1,"name":"a","next":null},null],"choice":{"spot":{"x":9,"y":10,"tag":3}},"choices":[{"xs":[1,2]},{"caption":"hi"}],"opt":[7,8],"lookup":{"3":30,"5":50},"head":{"id":9,"name":"h","next":{"id":10,"name":"n","next":null}},"blob":"AAEC","note":"note text"}`)},
		{name: "valid-defaults", want: true, wire: dartShapeJSONWire(t, u, `{}`)},
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

func vtCellsDartBytes(wire []byte) string {
	var b strings.Builder
	b.WriteString("[")
	for i, x := range wire {
		if i > 0 {
			b.WriteString(",")
		}
		fmt.Fprintf(&b, "%d", x)
	}
	b.WriteString("]")
	return b.String()
}

func dartVariableCellsProbeBody(vectors []dartShapeVector) string {
	var body strings.Builder
	body.WriteString("import 'dart:io';\n")
	body.WriteString("import 'dart:typed_data';\n")
	body.WriteString("import 'VtshapeWire.dart';\n")
	body.WriteString("import 'VtshapeFixed.dart' as f;\n")
	body.WriteString("import 'ProbeFixed.dart' as p;\n")
	body.WriteString("var _fails = <String>[];\n")
	body.WriteString("void check(bool ok, String msg){if(!ok)_fails.add('FAIL: ' + msg);}\n")
	body.WriteString("void main(){\n")
	for _, tc := range vectors {
		fmt.Fprintf(&body, `{
  final wire = Uint8List.fromList(%s);
  final want = %t;
  for (final retaining in [false, true]) {
    final retain = TableRetain(4096, List<int>.filled(256, 0));
    final report = TableWireReport();
    final value = retaining ? rootWireLoadRetain(wire, retain, report) : rootWireLoad(wire, report);
    if ((!report.refused) != want) {
      check(false, '%s (retaining=$retaining): refused=${report.refused} want=$want');
    }
    if (want) {
      if (report.refused) check(false, '%s (retaining=$retaining): want read refused');
      final resaved = rootWireSave(value);
      if (resaved.length != wire.length) check(false, '%s (retaining=$retaining): length ${resaved.length} != ${wire.length}');
      for (var i = 0; i < wire.length; i++) {
        if (resaved[i] != wire[i]) check(false, '%s (retaining=$retaining): resaved differs at byte $i');
      }
    }
  }
}`, vtCellsDartBytes(tc.wire), tc.want, tc.name, tc.name, tc.name, tc.name)
		body.WriteString("\n")
	}
	body.WriteString(`{
  final rows = List<f.Config>.generate(1, (_) => f.Config());
  rows[0].scale = 2.5;
  rows[0].spot.tag = 9;
  final need = p.configFixedMeasure(1);
  final buf = Uint8List(need);
  if (p.configFixedSave(rows, 1, buf) != need || buf[0] != f.tableFixedForm) {
    check(false, 'fixed-root-form3: save did not write form 3');
  }
  final out = List<f.Config>.generate(1, (_) => f.Config());
  final plan = p.configFixedNewPlan();
  final freport = f.TableFixedReport();
  final n = p.configFixedLoad(out, 1, buf, buf.length, plan, freport);
  if (n != 1 || out[0].scale != 2.5 || out[0].spot.tag != 9) {
    check(false, 'fixed-root-form3: form-3 file did not read back');
  }
  final form1 = Uint8List.fromList(buf);
  form1[0] = 1;
  final out1 = List<f.Config>.generate(1, (_) => f.Config());
  final er = f.TableFixedReport();
  final n1 = p.configFixedLoad(out1, 1, form1, form1.length, plan, er);
  if (n1 != -1 || er.refused == f.TableFixedRefusal.none || f.TableFixedRefusal.name(er.refused) != 'previous_form') {
    check(false, 'fixed-root-form1: form 1 was not refused as previous_form refused=${er.refused} name=${f.TableFixedRefusal.name(er.refused)}');
  }
}
`)
	body.WriteString("if (_fails.isNotEmpty) { stderr.writeln(_fails.join('\\n')); exit(1); }\n")
	body.WriteString("stdout.writeln('VT_CELLS_OK');\n}\n")
	return body.String()
}

func runDartVariableCellsProbe(t *testing.T, edit func(name string, data []byte) []byte) (string, error) {
	t.Helper()
	u := unitFrom(t, dartVariableShapeCellsSchema)
	vectors := buildDartShapeVectors(t, u)
	probe := dartVariableCellsProbeBody(vectors)

	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	for name, data := range packetFiles(t, u) {
		files[name] = data
	}
	if edit != nil {
		patched := 0
		for name, data := range files {
			if strings.HasSuffix(name, "Wire.dart") || strings.HasSuffix(name, "Fixed.dart") || strings.HasSuffix(name, "ProbeFixed.dart") {
				if ns := edit(name, data); ns != nil {
					files[name] = ns
					patched++
				}
			}
		}
		if patched == 0 {
			t.Fatal("negative control edit matched nothing in generated files")
		}
	}

	dir := t.TempDir()
	writeFiles(t, dir, files)
	if err := os.WriteFile(filepath.Join(dir, "vtcells_main.dart"), []byte(probe), 0600); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(dartBinary(t), "--enable-asserts", "vtcells_main.dart")
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func TestDartVariableCellsStrict(t *testing.T) {
	slowtest.Gate(t, "dart (variable cells strict read)")
	out, err := runDartVariableCellsProbe(t, nil)
	if err != nil || !strings.Contains(out, "VT_CELLS_OK") {
		t.Fatalf("variable cells probe failed: %v\n%s", err, out)
	}
}

func TestDartVariableCellsStrictNegativeControl(t *testing.T) {
	slowtest.Gate(t, "dart (negative control for variable cells strict read)")

	if out, err := runDartVariableCellsProbe(t, nil); err != nil || !strings.Contains(out, "VT_CELLS_OK") {
		t.Fatalf("clean artifact should hold the strict read: %v\n%s", err, out)
	}

	// Plant the short-body tolerance back into the real generated table code:
	// disable the root-body framing walk in wireFramingBad, the guard that
	// refuses the whole load on a truncated collection. The plant must land in
	// the real generated artifact, and the gate must go red on the short-body
	// vectors it held.
	outShort, errShort := runDartVariableCellsProbe(t, func(name string, data []byte) []byte {
		if !strings.HasSuffix(name, "Wire.dart") {
			return nil
		}
		s := string(data)
		const guard = "if (!_wireBody(data.sublist(1, bodyEnd), true, sink, 0)) return true;"
		if !strings.Contains(s, guard) {
			t.Fatalf("%s: the root-body framing guard is not present in the artifact", name)
		}
		return []byte(strings.Replace(s, guard, "if (false) return true; // SABOTAGED: the root-body framing walk was disabled", 1))
	})
	if errShort == nil {
		t.Fatalf("probe stayed green when the short-body refusal was sabotaged:\n%s", outShort)
	}
	for _, wantFail := range []string{"short-array-0", "short-keyed-0", "short-map-0", "short-bytes-1"} {
		if !strings.Contains(outShort, "FAIL: "+wantFail) {
			t.Errorf("short-body sabotage did not fail on %s:\n%s", wantFail, outShort)
		}
	}
}
