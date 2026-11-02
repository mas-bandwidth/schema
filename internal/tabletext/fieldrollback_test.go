package tabletext_test

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"reflect"
	"testing"
)

func TestMalformedFieldFileDiscardsDecodedState(t *testing.T) {
	m := textZeroModel(t)
	for _, tc := range []struct{ key, good, bad string }{
		{"n", `41`, `41 trailing`},
		{"inner", `{"s":"ok"}`, `{"s":"ok"} trailing`},
		{"list", `["a","b"]`, `["a","b"] trailing`},
		{"m", `{"a":5}`, `{"a":5,"b":`},
		{"s", `"ok"`, `"ok" trailing`},
	} {
		t.Run(tc.key, func(t *testing.T) {
			inst := m.New(m.Lookup("Root"))
			fv := field(t, inst, tc.key)
			var good tabletext.Report
			if !m.ReadValue(fv, []byte(tc.good), &good) || good.Malformed {
				t.Fatalf("valid control refused: %+v", good)
			}
			want := field(t, m.New(inst.Def), tc.key)
			if reflect.DeepEqual(fv, want) {
				t.Fatal("valid control did not place a value")
			}
			var report tabletext.Report
			if m.ReadValue(fv, []byte(tc.bad), &report) || !report.Malformed {
				t.Fatalf("malformed field accepted: %+v", report)
			}
			if !reflect.DeepEqual(fv, want) {
				t.Error("malformed field file left decoded state")
			}
		})
	}
}

func TestMalformedElementFileDiscardsOnlyDecodedElement(t *testing.T) {
	m := textZeroModel(t)
	inst := m.New(m.Lookup("Root"))
	fv := field(t, inst, "list")
	var good tabletext.Report
	if !m.ReadValue(fv, []byte(`["left","right"]`), &good) || good.Malformed {
		t.Fatalf("valid control refused: %+v", good)
	}
	sibling, count := fv.Elems[1], fv.Count
	var report tabletext.Report
	if m.ReadElement(fv, 0, []byte(`"changed" trailing`), &report) || !report.Malformed {
		t.Fatalf("malformed element accepted: %+v", report)
	}
	want := field(t, m.New(inst.Def), "list").Elems[0]
	if !reflect.DeepEqual(fv.Elems[0], want) {
		t.Error("malformed element file left decoded state")
	}
	if !reflect.DeepEqual(fv.Elems[1], sibling) || fv.Count != count {
		t.Fatal("element refusal changed an unrelated element or array count")
	}
}
