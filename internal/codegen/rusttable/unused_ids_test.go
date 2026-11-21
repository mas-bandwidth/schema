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

func runUnusedIDsProbe(t *testing.T, plant, reasonPlant bool) (string, error) {
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
		// the oracle's VERDICT (docs/rulings.json refusal-verdict-only):
		// refused; its reason is its own diagnostic, never Rust's
		if ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), bad, &damaged); ok || !tablewire.ReadRefusal(err) {
			t.Fatalf("oracle did not refuse unused ID %s: %v", tc.name, err)
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
     if %t {assert!(!report.damaged,"damage %s retained={retained}");} else {assert!(report.damaged || report.refused,"refusal %s retained={retained}: {report:?}");}
     if !%t {assert!(store.bytes.is_empty() && store.ids.is_empty(),"retention survived unused IDs");}
    }
   }
`, rustBytes(candidate.wire), candidate.valid, tc.name, candidate.valid, tc.name, candidate.valid, tc.name, tc.name, candidate.valid)
		}
	}
	if reasonPlant {
		plantUnusedIDsReason(t, files)
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

// plantUnusedIDsReason changes only WHY the Rust read refuses an unused
// ID-table entry: a refusal by reason in place of damage. The verdict, no
// value and nothing retained, is unchanged.
func plantUnusedIDsReason(t *testing.T, files map[string][]byte) {
	t.Helper()
	const from = "        report.damaged = true;\n        if let Some(store) = retain { store.bytes.clear(); store.ids.clear(); store.nodes.clear(); }\n"
	const to = "        report.refused = true;\n        if let Some(store) = retain { store.bytes.clear(); store.ids.clear(); store.nodes.clear(); }\n"
	replaced := 0
	for name, data := range files {
		if strings.HasSuffix(name, "_table.rs") {
			s := string(data)
			replaced += strings.Count(s, from)
			files[name] = []byte(strings.ReplaceAll(s, from, to))
		}
	}
	if replaced == 0 {
		t.Fatal("the reason plant found no unused-ID refusal")
	}
}

// plantReasonNames renames the refusal names Rust's reports spell: why a
// read refused is per-language diagnostic detail and moves no verdict.
func plantReasonNames(t *testing.T, files map[string][]byte) {
	t.Helper()
	replaced := 0
	for name, data := range files {
		if !strings.HasSuffix(name, ".rs") {
			continue
		}
		s := string(data)
		for _, reason := range []string{"non_finite_float", "count_over_extent_cap", "wire_damaged"} {
			from := "=> \"" + reason + "\","
			replaced += strings.Count(s, from)
			s = strings.ReplaceAll(s, from, "=> \"renamed_by_the_plant_"+reason+"\",")
		}
		files[name] = []byte(s)
	}
	if replaced == 0 {
		t.Fatal("the reason plant found no refusal name")
	}
}

func TestRustUnusedIDsRefuse(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: unused ID-table entries")
	if out, err := runUnusedIDsProbe(t, false, false); err != nil {
		t.Fatalf("unused-ID proof: %v\n%s", err, out)
	}
}

// TestRustUnusedIDsRefuseReasonIsDiagnostic: Rust refusing an unused ID by a
// reason rather than as damage is the same verdict, and passes.
func TestRustUnusedIDsRefuseReasonIsDiagnostic(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: unused ID-table entries, reason planted")
	if out, err := runUnusedIDsProbe(t, false, true); err != nil {
		t.Fatalf("unused-ID proof under the reason plant: %v\n%s", err, out)
	}
}
func TestRustUnusedIDsNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: planted unused-ID acceptance")
	out, err := runUnusedIDsProbe(t, true, false)
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
		const from = "matches!(arm_kind, 13..=16)"
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

// TestRustUnusedIDDiagnosticControls holds three hostile wires (a NaN, a NaN
// beside an unused ID, a count over the extent cap) to the oracle's VERDICT:
// refused by both. Which reason each names, and in what order each checks,
// is per-language detail (docs/rulings.json refusal-verdict-only): Rust's
// reason is printed, never compared.
func TestRustUnusedIDDiagnosticControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: standalone and compound diagnostic controls")
	runUnusedIDDiagnosticControls(t, false)
}

// TestRustUnusedIDDiagnosticControlsReasonIsDiagnostic runs the controls with
// Rust's refusal names renamed and its unused-ID damage turned into a
// refusal by reason: the verdicts do not move.
func TestRustUnusedIDDiagnosticControlsReasonIsDiagnostic(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "rustc: diagnostic controls, reasons planted")
	runUnusedIDDiagnosticControls(t, true)
}

func runUnusedIDDiagnosticControls(t *testing.T, reasonPlant bool) {
	t.Helper()
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
		name string
		wire []byte
	}{
		{"nonfinite", wire}, {"nonfinite-unused", compound}, {"count", count},
	} {
		var rep tabletext.Report
		ok, err := tablewire.Decode(m, m.New(m.Lookup("Root")), tc.wire, &rep)
		if ok || !tablewire.ReadRefusal(err) {
			t.Fatalf("Go %s: ok=%v %v %+v, want refused", tc.name, ok, err, rep)
		}
		t.Logf("Go %s: refused %s", tc.name, tablewire.RefusalReason(err))
		fmt.Fprintf(&body, "{ let mut r=TableReport::default(); assert!(root_load(&[%s], &mut r).is_none(), %q);\n", rustBytes(tc.wire), tc.name)
		fmt.Fprintf(&body, "assert!(r.damaged || r.refused, \"%s: no refusal reported: {r:?}\");\n", tc.name)
		fmt.Fprintf(&body, "println!(\"%s: Rust refused {} damaged={}\", r.reason.name(), r.damaged);\n", tc.name)
		fmt.Fprint(&body, "}\n")
	}
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	if reasonPlant {
		plantReasonNames(t, files)
		plantUnusedIDsReason(t, files)
	}
	if out, err := runRustGeneratedFilesProbe(t, files, body.String()); err != nil {
		t.Fatalf("diagnostic controls: %v\n%s", err, out)
	}
}
