package rusttable

import (
	"encoding/binary"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRustTextTotalNative(t *testing.T) {
	slowtest.Gate(t, "rustc (streaming total text budgets)")
	keyedRequireRustc(t)
	read := func(name string) string {
		b, e := os.ReadFile(filepath.Join("../../../testdata/conformance/feature-witnesses/text-list-total", name))
		if e != nil {
			t.Fatal(e)
		}
		return string(b)
	}
	source, input := read("Probe.schema"), read("input.json")
	u := unitFrom(t, source)
	m := tabletext.NewModel(u)
	// STRICT-ONLY (docs/plans/strict-only.md): the input runs past its
	// streaming total, so the whole read refuses in every form; nothing is
	// clamped. Only the verdict is pinned, and the Go engine is the oracle.
	v := m.New(u.Tables["Totals"])
	var r tabletext.Report
	inputOK := m.Read(v, []byte(input), &r)
	if inputOK {
		t.Fatalf("the Go engine accepted an input past its total: %+v", r)
	}
	expected := m.New(u.Tables["Totals"])
	if !m.Read(expected, []byte(read("expected.json")), &tabletext.Report{}) {
		t.Fatal("expected")
	}
	pin, err := tablewire.Encode(m, expected)
	if err != nil {
		t.Fatal(err)
	}
	wide := unitFrom(t, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(source, "[..3]", "[..8]"), "string(5)", "string(32)"), "| total=5", ""))
	wm := tabletext.NewModel(wide)
	raw := wm.New(wide.Tables["Totals"])
	if !wm.Read(raw, []byte(input), &tabletext.Report{}) {
		t.Fatal("wide")
	}
	wire, err := tablewire.Encode(wm, raw)
	if err != nil {
		t.Fatal(err)
	}
	wireV := m.New(u.Tables["Totals"])
	wireOK, _ := tablewire.Decode(m, wireV, wire, &tabletext.Report{})
	var b strings.Builder
	fmt.Fprintf(&b, `let expected=vec!%s;let mut r=TableReport::default();assert!(totals_from_json(%q,&mut r).is_none(),"JSON streaming total refuses");assert!(r.damaged);
let wire=vec!%s;let mut r=TableReport::default();assert_eq!(totals_load(&wire,&mut r).is_some(),%t,"wire past its total");
let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};let mut r=TableReport::default();let back=totals_load_retain(&wire,&mut store,&mut r);assert_eq!(back.is_some(),%t,"retained wire past its total");if back.is_none(){assert!(store.bytes.is_empty()&&store.ids.is_empty(),"refusal retained state");}
let mut r=TableReport::default();let v=totals_load(&expected,&mut r).expect("expected value");assert!(!r.damaged);assert_eq!(totals_save(&v),expected);
`, rustByteVector(pin), input, rustByteVector(wire), wireOK, wireOK)
	for _, big := range []bool{false, true} {
		p, e := tablecook.Cook(m, expected, tablecook.Options{Big: big})
		if e != nil {
			t.Fatal(e)
		}
		order := 1
		if big {
			order = 2
		}
		fmt.Fprintf(&b, `assert_eq!(totals_cook_write(&v,%d).unwrap(),vec!%s);`, order, rustByteVector(p))
	}
	for _, field := range []string{"a", "b", "c", "d"} {
		one, two := `"aaaaa".into()`, `"b".into()`
		if field == "b" || field == "d" {
			one = "Some(" + one + ")"
			two = "Some(" + two + ")"
		}
		fmt.Fprintf(&b, `{let mut bad=Totals::default();bad.%s=vec![Totals%sText{value:%s},Totals%sText{value:%s}];assert!(totals_save_checked(&bad).is_err());let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};let mut report=TableReport::default();assert!(std::panic::catch_unwind(std::panic::AssertUnwindSafe(||totals_save_retain(&bad,&mut store,&mut report))).is_err());assert!(totals_to_json_checked(&bad).is_none());for order in [1,2]{assert!(totals_cook_write(&bad,order).is_err());}}`, field, strings.ToUpper(field), one, strings.ToUpper(field), two)
	}
	payload := append([]byte{12, 3, 5}, []byte("aaaaa")...)
	payload = append(payload, 1, 'x', 1, 0)
	tail := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
	tail = append(tail, 0)
	tail = binary.LittleEndian.AppendUint64(tail, ir.TableWireId("c"))
	tail = binary.LittleEndian.AppendUint64(tail, 1)
	const tailText = `{"c":["aaaaa","x","\u0000"]}`
	tailTextOK := m.Read(m.New(u.Tables["Totals"]), []byte(tailText), &tabletext.Report{})
	tailWireOK, _ := tablewire.Decode(m, m.New(u.Tables["Totals"]), tail, &tabletext.Report{})
	fmt.Fprintf(&b, `{let mut r=TableReport::default();assert_eq!(totals_from_json(%q,&mut r).is_some(),%t,"tail text verdict");let mut r=TableReport::default();assert_eq!(totals_load(&vec!%s,&mut r).is_some(),%t,"tail wire verdict");}println!("total ok");`, tailText, tailTextOK, rustByteVector(tail), tailWireOK)
	out, e := rustConstructProbe(t, u, b.String())
	if e != nil || !strings.Contains(out, "total ok") {
		t.Fatalf("total native: %v\n%s", e, out)
	}
}
