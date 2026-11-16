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

func runNestedUnionExtentProbe(t *testing.T, plant bool) (string, error) {
	t.Helper()
	u := unitFrom(t, `package probe
 union Inner { value uint32 }
 union Outer { nested Inner }
 table Root { body Outer }
 `)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var report tabletext.Report
	if !m.Read(v, []byte(`{"body":{"nested":{"value":7}}}`), &report) {
		t.Fatalf("JSON oracle: %+v", report)
	}
	exact, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	// This deliberately tiny shape has one field and two union arm headers.
	if len(exact) < 14 || string(exact[:9]) != string([]byte{1, 1, 15, 2, 15, 7, 3, 8, 4}) {
		t.Fatalf("unexpected oracle framing: %x", exact)
	}
	inner := append([]byte(nil), exact[:13]...)
	inner = append(inner, 0xee)
	inner = append(inner, exact[13:]...)
	inner[5]++
	count := binary.LittleEndian.Uint64(exact[len(exact)-8:])
	end := len(exact) - 8 - int(count)*8
	outer := append([]byte(nil), exact[:end]...)
	outer = append(outer, 0xee)
	outer = append(outer, exact[end:]...)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	var probe strings.Builder
	for _, tc := range []struct {
		name  string
		wire  []byte
		valid bool
	}{{"exact", exact, true}, {"inner trailing", inner, false}, {"root trailing", outer, false}} {
		var r tabletext.Report
		ok, e := tablewire.Decode(m, m.New(m.Lookup("Root")), tc.wire, &r)
		if ok != tc.valid || (!tc.valid && tablewire.RefusalReason(e) != tablewire.ReasonWireDamaged) {
			t.Fatalf("Go oracle %s: ok=%v err=%v", tc.name, ok, e)
		}
		fmt.Fprintf(&probe, `{let wire:&[u8]=&[%s];
   for retained in [false,true] {
    let mut report=TableReport::default();
    let mut store=TableRetain{bytes:Vec::with_capacity(1024),ids:Vec::with_capacity(64),nodes:Default::default()};
    let value=if retained{root_load_retain(wire,&mut store,&mut report)}else{root_load(wire,&mut report)};
    assert_eq!(value.is_some(),%t,"%s retained={retained}");
    assert_eq!(report.damaged,!%t,"damage %s retained={retained}");
    if !%t {assert!(store.bytes.is_empty()&&store.ids.is_empty(),"retained damaged arm");}
   }}
`, rustBytes(tc.wire), tc.valid, tc.name, tc.valid, tc.name, tc.valid)
	}
	if plant {
		hits := 0
		for name, data := range files {
			s := string(data)
			hits += strings.Count(s, "inner_ok && *off == aend")
			files[name] = []byte(strings.ReplaceAll(s, "inner_ok && *off == aend", "inner_ok"))
		}
		if hits == 0 {
			t.Fatal("nested arm boundary sabotage matched nothing")
		}
	}
	return runRustGeneratedFilesProbe(t, files, probe.String())
}

func TestRustNestedUnionArmExtent(t *testing.T) {
	slowtest.Gate(t, "rustc: nested union arm extent")
	if out, err := runNestedUnionExtentProbe(t, false); err != nil {
		t.Fatalf("nested arm probe: %v\n%s", err, out)
	}
}

func TestRustNestedUnionArmExtentNegativeControl(t *testing.T) {
	slowtest.Gate(t, "rustc: nested union arm extent mutation")
	out, err := runNestedUnionExtentProbe(t, true)
	if err == nil || !strings.Contains(out, "inner trailing retained=false") {
		t.Fatalf("boundary omission did not fail named inner-arm witness: %v\n%s", err, out)
	}
}
