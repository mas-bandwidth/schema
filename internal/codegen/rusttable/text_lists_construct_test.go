package rusttable

import (
	"encoding/binary"
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

func TestRustTextListsNative(t *testing.T) {
	slowtest.Gate(t, "rustc (four bounded text-list shapes)")
	keyedRequireRustc(t)
	schema, err := os.ReadFile("../../../testdata/conformance/feature-witnesses/text-lists/Probe.schema")
	if err != nil {
		t.Fatal(err)
	}
	cases, err := os.ReadFile("../../../testdata/conformance/feature-witnesses/text-lists/cases.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	u := unitFrom(t, string(schema))
	m := tabletext.NewModel(u)
	var b strings.Builder
	for i, input := range strings.Split(strings.TrimSpace(string(cases)), "\n") {
		v := m.New(u.Tables["Root"])
		var r tabletext.Report
		ok := m.Read(v, []byte(input), &r)
		if !ok {
			fmt.Fprintf(&b, `assert!(root_from_json(%q,&mut TableReport::default()).is_none(),"invalid JSON %d");`, input, i)
			continue
		}
		wire, err := tablewire.Encode(m, v)
		if err != nil {
			t.Fatal(err)
		}
		text, err := m.Write(v)
		if err != nil {
			t.Fatal(err)
		}
		fmt.Fprintf(&b, `{let mut r=TableReport::default();let v=root_from_json(%q,&mut r).unwrap();assert_eq!((r.clamped,r.kind_mismatch),(%d,%d),"JSON counters %d");assert_eq!(root_to_json(&v).trim_end(),%q,"flat JSON %d");let wire=vec!%s;assert_eq!(root_save_checked(&v).unwrap(),wire,"wire pin %d");let back=root_load(&wire,&mut TableReport::default()).unwrap();assert_eq!(root_save(&back),wire);assert!(root_load_measure(&wire)>0);let mut store=TableRetain{bytes:Vec::with_capacity(4096),ids:Vec::with_capacity(4096)};let back=root_load_retain(&wire,&mut store,&mut TableReport::default()).unwrap();assert_eq!(root_save_retain(&back,&mut store,&mut TableReport::default()),wire);`, input, r.Clamped, r.KindMismatch, i, string(text), i, rustByteVector(wire), i)
		for _, big := range []bool{false, true} {
			pin, err := tablecook.Cook(m, v, tablecook.Options{Big: big})
			if err != nil {
				t.Fatal(err)
			}
			order := 1
			if big {
				order = 2
			}
			fmt.Fprintf(&b, `assert_eq!(root_cook_write(&v,%d).unwrap(),vec!%s,"cook pin %d");`, order, rustByteVector(pin), i)
		}
		b.WriteString("}\n")
	}
	for _, payload := range [][]byte{append([]byte{12, 1, 9}, []byte("123456789")...), append([]byte{12, 1, 10}, []byte("ééééé")...), {12, 1, 1, 0}, {12, 1, 1, 255}, {12, 127}} {
		wire := append([]byte{1, 1, 14, byte(len(payload))}, payload...)
		wire = append(wire, 0)
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableWireId("names"))
		wire = binary.LittleEndian.AppendUint64(wire, 1)
		bad := len(payload) < 4 || payload[3] == 0 || payload[3] == 255
		fmt.Fprintf(&b, `{let wire=vec!%s;let mut r=TableReport::default();let v=root_load(&wire,&mut r);assert_eq!(r.malformed,%t);assert_eq!(v.is_none(),%t);if let Some(v)=v {assert_eq!(v.names.len(),1);assert_eq!(r.clamped,1);assert_eq!(r.clamped_field,"Root.names");assert!(v.names[0].value.len()<=8);}}`, rustByteVector(wire), bad, bad)
	}
	for _, input := range []string{`{"names":["\udc00"]}`, `{"names":["\ud800\u0041"]}`, `{"\ud800":1}`} {
		fmt.Fprintf(&b, `assert!(root_from_json(%q,&mut TableReport::default()).is_some());`, input)
	}
	b.WriteString(`{let v=root_from_json(r#"{"names":["\ud83d\ude42"]}"#,&mut TableReport::default()).unwrap();assert_eq!(v.names[0].value,"🙂");}`)
	b.WriteString(`assert_eq!(root_from_json(r#"{"maybe":["\ud800"]}"#,&mut TableReport::default()).unwrap().maybe[0].value.as_deref(),Some("�"));
for bad in ["123456789","a\0b"] {let mut v=Root::default();v.names.push(RootNamesText{value:bad.into()});assert!(root_save_checked(&v).is_err());for order in [1,2] {assert!(root_cook_write(&v,order).is_err());}if bad.contains('\0') {assert!(root_to_json(&v).contains("\\u0000"));}}
let mut v=Root::default();v.names=vec![RootNamesText{value:"x".into()};4];assert!(root_save_checked(&v).is_err());
let mut r=TableReport::default();let _=root_from_json(r#"{"names":["123456789"]}"#,&mut r);assert_eq!(r.clamped_field,"Root.names");

println!("text lists ok");`)
	out, err := rustConstructProbe(t, u, b.String())
	if err != nil || !strings.Contains(out, "text lists ok") {
		t.Fatalf("text-list native: %v\n%s", err, out)
	}
}
