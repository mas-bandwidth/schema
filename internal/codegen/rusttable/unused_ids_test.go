package rusttable

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
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
