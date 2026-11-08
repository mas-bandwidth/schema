package rusttable

import (
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"strings"
	"testing"
)

func TestRustCompressedVariableNative(t *testing.T) {
	slowtest.Gate(t, "rustc (compressed float variable prerequisites)")
	keyedRequireRustc(t)
	source, e := os.ReadFile("../../../testdata/conformance/feature-witnesses/required/compressed-writers/Probe.schema")
	if e != nil {
		t.Fatal(e)
	}
	u := unitFrom(t, string(source))
	m := tabletext.NewModel(u)
	var b strings.Builder
	refused := 0
	for _, input := range []string{`{"x":0.75,"ys":[-0.5,0.5]}`, `{"x":-1,"ys":[-2,2]}`} {
		rawUnit := unitFrom(t, `package probe
 table R { x float32 | required
 ys [..2]float32 | required }
 `)
		rawModel := tabletext.NewModel(rawUnit)
		rawValue := rawModel.New(rawUnit.Tables["R"])
		if !rawModel.Read(rawValue, []byte(input), &tabletext.Report{}) {
			t.Fatal("raw float fixture")
		}
		rawWire, e := tablewire.Encode(rawModel, rawValue)
		if e != nil {
			t.Fatal(e)
		}

		v := m.New(u.Tables["R"])
		var r tabletext.Report
		if !m.Read(v, []byte(input), &r) {
			// STRICT-ONLY (docs/plans/strict-only.md): a value past the declared
			// range refuses the whole read, on the text and on the wire alike.
			fmt.Fprintf(&b, `{let mut r=TableReport::default();assert!(r_from_json(%q,&mut r).is_none());assert!(r.damaged);let mut w=TableReport::default();assert!(r_load(&vec!%s,&mut w).is_none());assert!(w.damaged);}
`, input, rustByteVector(rawWire))
			refused++
			continue
		}
		wire, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&b, `{let mut report=TableReport::default();let value=r_load(&vec!%s,&mut report).unwrap();assert!(!report.damaged);assert_eq!(r_save_checked(&value).unwrap(),vec!%s);}`, rustByteVector(rawWire), rustByteVector(wire))
		text, e := m.Write(v)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&b, `{let mut r=TableReport::default();let v=r_from_json(%q,&mut r).unwrap();assert!(!r.damaged);assert_eq!(r_save_checked(&v).unwrap(),vec!%s);assert_eq!(r_to_json_checked(&v).unwrap().trim_end(),%q);let back=r_load(&vec!%s,&mut TableReport::default()).unwrap();assert_eq!(r_save_checked(&back).unwrap(),vec!%s);`, input, rustByteVector(wire), string(text), rustByteVector(wire), rustByteVector(wire))
		for _, big := range []bool{false, true} {
			pin, e := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if e != nil {
				t.Fatal(e)
			}
			order := 1
			if big {
				order = 2
			}
			fmt.Fprintf(&b, `assert_eq!(r_cook_write(&v,%d).unwrap(),vec!%s);`, order, rustByteVector(pin))
		}
		b.WriteString("}\n")
	}
	if refused != 1 {
		t.Fatalf("the engine refused %d inputs, want exactly the out-of-range one", refused)
	}
	b.WriteString(`for v in [R{x:0.0,ys:vec![]},R{x:0.5,ys:vec![2.0]}]{assert!(r_save_checked(&v).is_err());assert!(r_to_json_checked(&v).is_none());assert!(r_cook_write(&v,1).is_err());}println!("compressed variable ok");`)
	out, e := rustConstructProbe(t, u, b.String())
	if e != nil || !strings.Contains(out, "compressed variable ok") {
		t.Fatalf("compressed variable: %v\n%s", e, out)
	}
}
