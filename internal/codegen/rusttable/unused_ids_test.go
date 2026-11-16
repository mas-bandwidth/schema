package rusttable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const unusedIDsSchema = `package probe
 enum Mode { A, B }
 table Node { mode Mode }
 table Inner { mode Mode }
 union Choice { code Mode }
 table Root {
  mode Mode
  child Inner
  choice Choice
  items [..2]Mode
  keyed [Mode]int32
  head *Node
  data *bytes
 }
`

func runUnusedIDsProbe(t *testing.T, plant bool) (string, error) {
	t.Helper()
	u := unitFrom(t, unusedIDsSchema)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var probe strings.Builder
	for _, tc := range []struct{ name, text string }{
		{"empty", `{}`}, {"enum", `{"mode":"B"}`}, {"nested", `{"child":{"mode":"B"}}`},
		{"union", `{"choice":{"code":"B"}}`}, {"array", `{"items":["B"]}`}, {"keyed", `{"keyed":{"B":7}}`},
		{"node", `{"head":{"mode":"B"}}`}, {"blob", `{"data":"aGk="}`},
	} {
		value := m.New(m.Lookup("Root"))
		var report tabletext.Report
		if !m.Read(value, []byte(tc.text), &report) {
			t.Fatalf("oracle JSON %s: %+v", tc.name, report)
		}
		wire, err := tablewire.Encode(m, value)
		if err != nil {
			t.Fatal(err)
		}
		count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
		bad := append([]byte(nil), wire[:len(wire)-8]...)
		bad = binary.LittleEndian.AppendUint64(bad, 0x1234567890abcdef)
		bad = binary.LittleEndian.AppendUint64(bad, count+1)
		var damaged tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), bad, &damaged); ok || tablewire.RefusalReason(err) != tablewire.ReasonWireDamaged {
			t.Fatalf("oracle accepted unused ID %s: %v", tc.name, err)
		}
		for _, candidate := range []struct {
			wire  []byte
			valid bool
		}{{wire, true}, {bad, false}} {
			fmt.Fprintf(&probe, `{
    let wire: &[u8]=&[%s];
    assert_eq!(root_load_measure(wire)>=0,%t,"measure %s");
    for retained in [false,true] {
     let mut report=TableReport::default();
     let mut store=TableRetain { bytes:Vec::with_capacity(1024),ids:Vec::with_capacity(64),nodes:Default::default() };
     let value=if retained {root_load_retain(wire,&mut store,&mut report)} else {root_load(wire,&mut report)};
     assert_eq!(value.is_some(),%t,"load %s retained={retained}");
     assert_eq!(report.damaged,!%t,"damage %s retained={retained}");
     if !%t {assert!(store.bytes.is_empty() && store.ids.is_empty(),"retention survived unused IDs");}
    }
   }
`, rustBytes(candidate.wire), candidate.valid, tc.name, candidate.valid, tc.name, candidate.valid, tc.name, candidate.valid)
		}
	}
	if plant {
		const from = "table_ids_all_used(&wire[1..end], ids) == Some(false)"
		replaced := 0
		for name, data := range files {
			if strings.HasSuffix(name, "_table.rs") {
				s := string(data)
				replaced += strings.Count(s, from)
				files[name] = []byte(strings.ReplaceAll(s, from, "false"))
			}
		}
		if replaced == 0 {
			t.Fatal("missing unused-ID gates")
		}
	}
	return runRustGeneratedFilesProbe(t, files, probe.String())
}

func TestRustUnusedIDsRefuse(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: unused ID-table entries")
	if out, err := runUnusedIDsProbe(t, false); err != nil {
		t.Fatalf("unused-ID proof: %v\n%s", err, out)
	}
}
func TestRustUnusedIDsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: planted unused-ID acceptance")
	out, err := runUnusedIDsProbe(t, true)
	if err == nil || !strings.Contains(out, "measure empty") {
		t.Fatalf("unused-ID plant did not fail the actual boundary: %v\n%s", err, out)
	}
}

// This compound pins the actual Go check order separately from Rust's legacy
// nonfinite-before-framing path: an unused transport ID is judged first.
func TestUnusedIDNonFiniteOracle(t *testing.T) {
	u := unitFrom(t, "package probe\ntable Root { value float32 }\n")
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(`{"value":1}`), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	if len(wire) < 8 || wire[2] != 10 {
		t.Fatal("float witness changed")
	}
	binary.LittleEndian.PutUint32(wire[3:7], 0x7fc00000)
	count := binary.LittleEndian.Uint64(wire[len(wire)-8:])
	bad := append([]byte(nil), wire[:len(wire)-8]...)
	bad = binary.LittleEndian.AppendUint64(bad, 0x1234567890abcdef)
	bad = binary.LittleEndian.AppendUint64(bad, count+1)
	ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), bad, &report)
	t.Logf("Go NaN plus unused ID: ok=%v reason=%s err=%v", ok, tablewire.RefusalReason(err), err)
	if ok || tablewire.RefusalReason(err) != tablewire.ReasonWireDamaged {
		t.Fatal("compound oracle differs", err)
	}
}

func runUnusedUnionDepth(t *testing.T, plant bool) (string, error) {
	t.Helper()
	u := unitFrom(t, unusedIDsSchema)
	m := tabletext.NewModel(u)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var body strings.Builder
	for _, depth := range []int{128, 129, 4096} {
		payload := []byte{2, 4, 4, 7, 0, 0, 0} // unknown arm, scalar int32
		for range depth {
			outer := []byte{2, 15}
			outer = binary.AppendUvarint(outer, uint64(len(payload)))
			payload = append(outer, payload...)
		}
		data := append([]byte{1, 15}, payload...)
		data = append(data, 0)
		wire := append([]byte{1}, data...)
		wire = binary.LittleEndian.AppendUint64(wire, 0x10)
		wire = binary.LittleEndian.AppendUint64(wire, 0x20)
		wire = binary.LittleEndian.AppendUint64(wire, 2)
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), wire, &rep)
		if ok != (depth == 128) {
			t.Fatalf("Go union depth %d: %v %+v", depth, err, rep)
		}
		want := "None"
		if depth == 128 {
			want = "Some(true)"
		}
		fmt.Fprintf(&body, "assert_eq!(table_ids_all_used(&[%s], &[0x10,0x20]), %s, \"union-depth-%d\");\n", rustBytes(data), want, depth)
	}
	if plant {
		const from = "matches!(arm_kind, 13 | 14 | 15 | 16)"
		name := VariableRuntimeModule + ".rs"
		if strings.Count(string(files[name]), from) != 1 {
			t.Fatal("missing union structural-depth gate")
		}
		files[name] = []byte(strings.Replace(string(files[name]), from, "matches!(arm_kind, 13 | 14 | 16)", 1))
	}
	return runRustGeneratedFilesProbe(t, files, body.String())
}

func TestRustUnusedIDUnionDepth(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: bounded union ID census")
	if out, err := runUnusedUnionDepth(t, false); err != nil {
		t.Fatalf("union depth: %v\n%s", err, out)
	}
}
func TestRustUnusedIDUnionDepthNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: missing union depth plant")
	out, err := runUnusedUnionDepth(t, true)
	if err == nil || !strings.Contains(out, "union-depth-129") {
		t.Fatalf("union depth plant did not fail safely at129: %v\n%s", err, out)
	}
}

func TestRustUnusedIDDiagnosticControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: standalone and compound diagnostic controls")
	u := unitFrom(t, "package probe\ntable Root { value float32\n xs []int32 }\n")
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(`{"value":1}`), &report) {
		t.Fatal(report)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	if wire[2] != 10 {
		t.Fatal("float witness changed")
	}
	binary.LittleEndian.PutUint32(wire[3:7], 0x7fc00000)
	compound := append([]byte(nil), wire[:len(wire)-8]...)
	compound = binary.LittleEndian.AppendUint64(compound, 0x1234567890abcdef)
	compound = binary.LittleEndian.AppendUint64(compound, 2)
	count := []byte{1, 1, 14, 6, 4, 0x80, 0x80, 0x80, 0x80, 8, 0}
	count = binary.LittleEndian.AppendUint64(count, ir.TableFieldWireId(u.Tables["Root"].Fields[1]))
	count = binary.LittleEndian.AppendUint64(count, 1)
	var body strings.Builder
	for _, tc := range []struct {
		name, reason string
		wire         []byte
	}{
		{"nonfinite", "non_finite_float", wire}, {"nonfinite-unused", "wire_damaged", compound}, {"count", "count_over_extent_cap", count},
	} {
		var rep tabletext.Report
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), tc.wire, &rep); ok || string(tablewire.RefusalReason(err)) != tc.reason {
			t.Fatalf("Go %s: %v %+v", tc.name, err, rep)
		}
		fmt.Fprintf(&body, "{ let mut r=TableReport::default(); assert!(root_load(&[%s], &mut r).is_none(), %q);\n", rustBytes(tc.wire), tc.name)
		if tc.reason == "wire_damaged" {
			fmt.Fprint(&body, "assert!(r.damaged);\n")
		} else {
			fmt.Fprintf(&body, "assert_eq!(r.reason.name(), %q, %q);\n", tc.reason, tc.name)
		}
		fmt.Fprint(&body, "}\n")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if out, err := runRustGeneratedFilesProbe(t, files, body.String()); err != nil {
		t.Fatalf("diagnostic controls: %v\n%s", err, out)
	}
}
