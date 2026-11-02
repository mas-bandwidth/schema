package rusttable

import (
	"encoding/binary"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestRustUnboundedTextNative(t *testing.T) {
	slowtest.Gate(t, "rustc (unbounded UTF-8 text extents)")
	keyedRequireRustc(t)
	schema, e := os.ReadFile("../../../testdata/conformance/feature-witnesses/unbounded-text/complete/Probe.schema")
	if e != nil {
		t.Fatal(e)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	long := strings.Repeat("é", 2048) + "tail"
	raw, e := json.Marshal(map[string]any{"capped": []string{"", "x", long}, "maybe_capped": []any{nil, "", "z"}, "open": []string{long, ""}, "maybe_open": []any{nil, long, ""}, "by_name": map[string]int{long: 9, "": 1}, "note": map[string]any{"lines": []string{long, "child"}}})
	if e != nil {
		t.Fatal(e)
	}
	var b strings.Builder
	for i, input := range []string{`{}`, string(raw), `{"by_name":{"same":1,"same":9},"maybe_open":[null,"",42],"capped":["a","b","c","tail"]}`, `{"open":["\ud800","\udfff","\udc00\ud800","\ud83d\ude00"],"by_name":{"\ud800":1,"\udfff":2}}`} {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		if !m.Read(v, []byte(input), &r) {
			t.Fatal(r)
		}
		wire, e := tablewire.Encode(m, v)
		if e != nil {
			t.Fatal(e)
		}
		text, e := m.Write(v)
		if e != nil {
			t.Fatal(e)
		}
		fmt.Fprintf(&b, `{let mut r=TableReport::default();let v=root_from_json(%q,&mut r).unwrap();assert_eq!((r.clamped,r.kind_mismatch,r.duplicate),(%d,%d,%d),"reports%d");assert_eq!(root_to_json(&v).trim_end(),%q,"JSON%d");let wire=vec!%s;assert_eq!(root_save_checked(&v).unwrap(),wire,"wire%d");let back=root_load(&wire,&mut TableReport::default()).unwrap();assert_eq!(root_save(&back),wire);let mut store=TableRetain{bytes:Vec::with_capacity(65536),ids:Vec::with_capacity(65536)};let back=root_load_retain(&wire,&mut store,&mut TableReport::default()).unwrap();assert_eq!(root_save_retain(&back,&mut store,&mut TableReport::default()),wire);`, input, r.Clamped, r.KindMismatch, r.Duplicate, i, string(text), i, rustByteVector(wire), i)
		measured, whole, refused := tablewire.FileRegionMeasure(u, u.Tables["Root"], wire)
		if !whole || refused {
			t.Fatal("measure", measured, whole, refused)
		}
		fmt.Fprintf(&b, "assert_eq!(root_load_measure(&wire),%d);\n", measured)
		for _, big := range []bool{false, true} {
			pin, e := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if e != nil {
				t.Fatal(e)
			}
			order := 1
			if big {
				order = 2
			}
			fmt.Fprintf(&b, `assert_eq!(root_cook_write(&v,%d).unwrap(),vec!%s,"cook%d");`, order, rustByteVector(pin), i)
		}
		b.WriteString("}\n")
	}

	for _, payload := range [][]byte{{12, 1, 1, 0}, {12, 1, 1, 255}, {12, 1, 2, 0xc0, 0x80}, {12, 1, 127}, {12, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 0xff, 2}} {
		wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("open"))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		fmt.Fprintf(&b, `{let bytes=vec!%s;let mut r=TableReport::default();assert!(root_load(&bytes,&mut r).is_none());let mut s=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};assert!(root_load_retain(&bytes,&mut s,&mut TableReport::default()).is_none());let _=root_load_measure(&bytes);}`, rustByteVector(wire))
	}
	fmt.Fprintf(&b, `{let mut v=Root::default();v.by_name.push(RootByNameEntry{key:%q.into(),value:9});let bytes=root_cook_write(&v,1).unwrap();assert_eq!((bytes.as_ptr() as usize)%%std::mem::align_of::<RootRow>(),0);let root=unsafe{&*(bytes.as_ptr().add(64) as *const RootRow)};assert_eq!(unsafe{root.by_name_find(%q.as_bytes())}.unwrap().value,9);assert!(unsafe{root.by_name_find(b"absent")}.is_none());}`, long, long)
	b.WriteString(`{let mut bad=Root::default();bad.open.push(RootOpenText{value:"a\0b".into()});assert!(root_save_checked(&bad).is_err());for order in [1,2]{assert!(root_cook_write(&bad,order).is_err());}}
 println!("unbounded ok");`)

	out, e := rustConstructProbe(t, u, b.String())
	if e != nil || !strings.Contains(out, "unbounded ok") {
		t.Fatalf("unbounded native: %v\n%s", e, out)
	}
}
