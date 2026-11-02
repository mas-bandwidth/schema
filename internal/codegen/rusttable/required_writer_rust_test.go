package rusttable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestRustRequiredDefaultWritersNative(t *testing.T) {
	slowtest.Gate(t, "rustc (sixteen Required default writers)")
	keyedRequireRustc(t)
	fixture := "../../../testdata/conformance/feature-witnesses/required/default-writers/"
	src, e := os.ReadFile(fixture + "Probe.schema")
	if e != nil {
		t.Fatal(e)
	}
	payloads, e := os.ReadFile(fixture + "payloads.txt")
	if e != nil {
		t.Fatal(e)
	}
	u := unitFrom(t, string(src))
	m := tabletext.NewModel(u)
	wire := []byte{1}
	for i, p := range strings.Fields(string(payloads)) {
		data, e := hex.DecodeString(p)
		if e != nil {
			t.Fatal(e)
		}
		wire = append(wire, byte(i+1), byte(ir.TableWireFieldKind(u.Tables["All"].Fields[i])))
		wire = append(wire, data...)
	}
	wire = append(wire, 0)
	for _, f := range u.Tables["All"].Fields {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
	}
	wire = binary.LittleEndian.AppendUint64(wire, 16)
	v := m.New(u.Tables["All"])
	oracle, e := tablewire.Encode(m, v)
	if e != nil || !bytes.Equal(wire, oracle) {
		t.Fatalf("hand payload oracle: %v got%x want%x", e, oracle, wire)
	}
	var b strings.Builder
	for _, root := range []string{"All", "RequiredEmpty"} {
		v := m.New(u.Tables[root])
		pin, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		fn := ir.RustSnake(root)
		fmt.Fprintf(&b, `{let v=%s::default();let expected=vec!%s;assert_eq!(%s_save_checked(&v).unwrap(),expected,"required defaults %s");let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};assert_eq!(%s_save_retain(&v,&mut store,&mut TableReport::default()),expected);assert!(%s_load(&expected,&mut TableReport::default()).is_some());}`, root, rustByteVector(pin), fn, root, fn, fn)
	}
	b.WriteString(`println!("required default writers ok");`)
	out, e := rustConstructProbe(t, u, b.String())
	if e != nil || !strings.Contains(out, "required default writers ok") {
		t.Fatalf("required defaults: %v\n%s", e, out)
	}
}
