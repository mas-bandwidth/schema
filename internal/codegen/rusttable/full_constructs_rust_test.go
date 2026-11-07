package rusttable

import (
	"bytes"
	"fmt"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
	"os"
	"strings"
	"testing"
)

func TestRustFullConstructWitnessNative(t *testing.T) {
	slowtest.Gate(t, "rustc (all eleven constructs populated together)")
	keyedRequireRustc(t)
	base := "../../../testdata/conformance/feature-witnesses/full/"
	schema, e := os.ReadFile(base + "Features.schema")
	if e != nil {
		t.Fatal(e)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	var b strings.Builder
	for _, root := range []string{"Root", "ArrayRoot"} {
		input, e := os.ReadFile(base + root + ".input.json")
		if e != nil {
			t.Fatal(e)
		}
		pin, e := os.ReadFile(base + root + ".bin")
		if e != nil {
			t.Fatal(e)
		}
		v := m.New(u.Tables[root])
		if !m.Read(v, input, &tabletext.Report{}) {
			t.Fatal("fixture JSON", root)
		}
		wire, e := tablewire.Encode(m, v)
		if e != nil || !bytes.Equal(wire, pin) {
			t.Fatalf("independent pin %s: %v", root, e)
		}
		text, e := m.Write(v)
		if e != nil {
			t.Fatal(e)
		}
		n := ir.RustSnake(root)
		fmt.Fprintf(&b, `{let v=%s_from_json(%q,&mut TableReport::default()).unwrap();let pin=vec!%s;assert_eq!(%s_save_checked(&v).unwrap(),pin);assert_eq!(%s_to_json_checked(&v).unwrap().trim_end(),%q);let mut ret=TableRetain{bytes:Vec::with_capacity(65536),ids:Vec::with_capacity(65536)};let back=%s_load_retain(&pin,&mut ret,&mut TableReport::default()).unwrap();assert_eq!(%s_save_retain(&back,&mut ret,&mut TableReport::default()),pin);`, n, string(input), rustByteVector(pin), n, n, string(text), n, n)
		for _, big := range []bool{false, true} {
			cook, e := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if e != nil {
				t.Fatal(e)
			}
			order := 1
			if big {
				order = 2
			}
			fmt.Fprintf(&b, `assert_eq!(%s_cook_write(&v,%d).unwrap(),vec!%s);`, n, order, rustByteVector(cook))
		}
		b.WriteString("}\n")
	}
	b.WriteString(`println!("full constructs ok");`)
	out, e := rustConstructProbe(t, u, b.String())
	if e != nil || !strings.Contains(out, "full constructs ok") {
		t.Fatalf("full constructs %v\n%s", e, out)
	}
}
