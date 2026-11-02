package tablewire_test

import (
	"bytes"
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestRequiredNeutralPresence(t *testing.T) {
	m := listModel(t, `package probe
table Root {
 a int32
 b int32 | required
 c int32 | required
}
`)
	// Independent hand framing: required zero scalars must ride, with their IDs
	// interned in declaration order. The neutral encoder supplies no expected bytes.
	wire := []byte{1, 1, 4, 0, 0, 0, 0, 2, 4, 0, 0, 0, 0, 0}
	for _, f := range m.Lookup("Root").Fields[1:] {
		wire = binary.LittleEndian.AppendUint64(wire, ir.TableFieldWireId(f))
	}
	wire = binary.LittleEndian.AppendUint64(wire, 2)
	t.Run("write_zero", func(t *testing.T) {
		got, err := tablewire.Encode(m, m.New(m.Lookup("Root")))
		if err != nil || !bytes.Equal(got, wire) {
			t.Fatalf("got %x error %v; want %x", got, err, wire)
		}
	})
	for _, tc := range []struct {
		name, text, reason string
		mismatch           int
	}{
		{"none", `{}`, "missing_required: b, c", 0},
		{"one", `{"a":9,"c":0}`, "missing_required: b", 0},
		{"all", `{"b":0,"c":0}`, "", 0},
		{"wrong_shape_is_named", `{"b":"bad","c":0}`, "", 1},
	} {
		t.Run("json/"+tc.name, func(t *testing.T) {
			v := m.New(m.Lookup("Root"))
			var r tabletext.Report
			ok := m.Read(v, []byte(tc.text), &r)
			if ok != (tc.reason == "") || r.Reason != tc.reason || r.Malformed || r.KindMismatch != tc.mismatch {
				t.Fatalf("ok=%v report=%+v", ok, r)
			}
			if !ok {
				a, _ := v.FieldByKey("a")
				if a.Cell.I != 0 || a.Cell.U != 0 {
					t.Fatal("refused prefix survived")
				}
			}
		})
	}
	for _, tc := range []struct {
		name   string
		data   []byte
		reason string
	}{
		{"none", []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}, "missing_required: b, c"},
		{"all", wire, ""},
	} {
		t.Run("wire/"+tc.name, func(t *testing.T) {
			v := m.New(m.Lookup("Root"))
			var r tabletext.Report
			ok, err := tablewire.Decode(m, v, tc.data, &r)
			if ok != (tc.reason == "") || r.Reason != tc.reason || r.Malformed || (err != nil) != (tc.reason != "") {
				t.Fatalf("ok=%v err=%v report=%+v", ok, err, r)
			}
		})
	}
}

func TestRequiredNeutralNestedAndGuarded(t *testing.T) {
	m := listModel(t, `package probe
table Kid { x int32 | required }
table Root {
 enabled bool
 if enabled { a int32 | required }
 kid Kid
}
`)
	for _, tc := range []struct{ input, reason string }{
		{`{}`, ""},
		{`{"enabled":true}`, "missing_required: a"},
		{`{"kid":{}}`, "missing_required: x"},
		{`{"enabled":true,"a":0,"kid":{"x":0}}`, ""},
	} {
		t.Run(tc.input, func(t *testing.T) {
			v := m.New(m.Lookup("Root"))
			var r tabletext.Report
			ok := m.Read(v, []byte(tc.input), &r)
			if ok != (tc.reason == "") || r.Reason != tc.reason || r.Malformed {
				t.Fatalf("ok=%v report=%+v", ok, r)
			}
		})
	}
}

func TestRequiredNeutralWriterShapes(t *testing.T) {
	m := listModel(t, `package probe
enum Key { First, Second }
union Choice { number int32 }
table ReqEmpty { plain int32 }
table ReqScalar { value int32 | required }
table ReqText { value string(8) | required }
table ReqWide { value wstring(8) | required }
table ReqBytes { value bytes(8) | required }
table ReqFixedArray { value [2]int32 | required }
table ReqCountedArray { value [..2]int32 | required }
table ReqKeyed { value [Key]int32 | required }
table ReqUnion { value Choice | required }
table ReqNested { value ReqEmpty | required }
table ReqMap { value map[uint8]int32 | required }
table ReqList { value []int32 | required }
table ReqPointer { value *ReqEmpty | required }
table ReqBlob { value *bytes | required }
`)
	for _, name := range []string{"ReqScalar", "ReqText", "ReqWide", "ReqBytes", "ReqFixedArray", "ReqCountedArray", "ReqKeyed", "ReqUnion", "ReqNested", "ReqMap", "ReqList", "ReqPointer", "ReqBlob"} {
		t.Run(name, func(t *testing.T) {
			v := m.New(m.Lookup(name))
			wire, err := tablewire.Encode(m, v)
			if err != nil || len(wire) <= 10 {
				t.Fatalf("required empty field elided: %x %v", wire, err)
			}
			back := m.New(m.Lookup(name))
			var rep tabletext.Report
			if ok, err := tablewire.Decode(m, back, wire, &rep); !ok || err != nil || !rep.Silent() {
				t.Fatalf("file roundtrip: %v %v %+v", ok, err, rep)
			}
			if wire[2] == 14 || wire[2] == 16 {
				bad := append([]byte(nil), wire...)
				bad[4] = 1 // a bool element is incompatible with every shape here
				rep = tabletext.Report{}
				if ok, err := tablewire.Decode(m, back, bad, &rep); ok || err == nil || rep.KindMismatch != 1 || rep.MissingRequired != 1 || rep.Malformed {
					t.Fatalf("incompatible element satisfied required: %v %v %+v", ok, err, rep)
				}
			}
			empty := []byte{1, 0, 0, 0, 0, 0, 0, 0, 0, 0}
			rep = tabletext.Report{}
			if ok, _ := tablewire.Decode(m, back, empty, &rep); ok || rep.Reason != "missing_required: value" {
				t.Fatalf("absence accepted: %v %+v", ok, rep)
			}

		})
	}
}

func TestRequiredNeutralRangeAndReportReuse(t *testing.T) {
	m := listModel(t, `package probe
table Root { value int64 | required, min = 9007199254740993 }
`)
	v := m.New(m.Lookup("Root"))
	if _, err := tablewire.Encode(m, v); err == nil {
		t.Error("wire writer accepted unset required range")
	}

	if _, err := m.Write(v); err == nil {
		t.Error("JSON writer accepted unset required range")
	}
	var rep tabletext.Report
	if m.Read(v, []byte(`{}`), &rep) {
		t.Fatal("missing accepted")
	}
	before := rep.MissingRequired
	if before != 1 {
		t.Fatalf("missing bodies=%d", before)
	}
	if !m.Read(v, []byte(`{"value":9007199254740993}`), &rep) || rep.MissingRequired != before {
		t.Fatalf("old refusal poisoned valid JSON: %+v", rep)
	}
	wire, err := tablewire.Encode(m, v)
	if err != nil {
		t.Fatal(err)
	}
	if ok, err := tablewire.Decode(m, v, wire, &rep); !ok || err != nil || rep.MissingRequired != before {
		t.Fatalf("old refusal poisoned valid file: %v %v %+v", ok, err, rep)
	}
	var sum tabletext.Report
	sum.Add(rep)
	if sum.MissingRequired != 1 || sum.Silent() {
		t.Fatalf("required refusal lost in fold: %+v", sum)
	}
}

func TestRequiredNeutralNestedRefusalDiscardsWholeLoad(t *testing.T) {
	const schema = `package probe
table Kid { x int32 | required
 extra int32
}
table Root {
 marker int32
 kid Kid
 rows []Kid
 by_id map[uint8]Kid
 link *Kid
}
`
	m := listModel(t, schema)
	old := listModel(t, strings.ReplaceAll(schema, " | required", ""))
	for _, input := range []string{
		`{"marker":7,"kid":{"extra":9}}`,
		`{"marker":7,"rows":[{"extra":9}]}`,
		`{"marker":7,"by_id":{"1":{"extra":9}}}`,
		`{"marker":7,"link":{"extra":9}}`,
		`{"marker":7,"link":{}}`,
	} {
		t.Run(input, func(t *testing.T) {
			v := m.New(m.Lookup("Root"))
			var rep tabletext.Report
			if m.Read(v, []byte(input), &rep) || rep.MissingRequired != 1 || rep.Reason != "missing_required: x" || rep.Malformed {
				t.Fatalf("JSON refusal: %+v", rep)
			}
			before := place(t, old, "Root", input)
			wire, err := tablewire.Encode(old, before)
			if err != nil {
				t.Fatal(err)
			}
			for _, retain := range []bool{false, true} {
				v = m.New(m.Lookup("Root"))
				rep = tabletext.Report{}
				store := tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
				var ok bool
				if retain {
					ok, err = tablewire.DecodeRetain(m, v, wire, &store, &rep)
				} else {
					ok, err = tablewire.Decode(m, v, wire, &rep)
				}
				if ok || err == nil || rep.MissingRequired != 1 || rep.Reason != "missing_required: x" || rep.Malformed {
					t.Fatalf("wire retain=%v: %v %v %+v", retain, ok, err, rep)
				}
				marker, _ := v.FieldByKey("marker")
				if marker.Cell.U != 0 || store.Used() != 0 {
					t.Fatal("refusal published decoded or retained prefix")
				}
			}
		})
	}
}

func TestRequiredNeutralFieldEntries(t *testing.T) {
	m := listModel(t, `package probe
table Kid { x int32 | required }
table Root { child Kid
 children [2]Kid
 amount int64 | min=7, required
}
`)
	v := m.New(m.Lookup("Root"))
	child, _ := v.FieldByKey("child")
	children, _ := v.FieldByKey("children")
	amount, _ := v.FieldByKey("amount")
	var r tabletext.Report
	if m.ReadValue(child, []byte(`{}`), &r) || r.Reason != "missing_required: x" || r.Malformed {
		t.Fatalf("field refusal: %+v", r)
	}
	r = tabletext.Report{}
	if m.ReadElement(children, 0, []byte(`{}`), &r) || r.Reason != "missing_required: x" || r.Malformed {
		t.Fatalf("element refusal: %+v", r)
	}
	if _, err := m.WriteValue(amount); err == nil {
		t.Fatal("field writer accepted unset required range")
	}
}
