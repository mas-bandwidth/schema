package tablewire_test

import (
	"encoding/json"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestTextTotalJSONCorpusAndWriterRefusal(t *testing.T) {
	const corpus = "../../testdata/conformance/feature-witnesses/text-list-total"
	schema, err := os.ReadFile(filepath.Join(corpus, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	input, err := os.ReadFile(filepath.Join(corpus, "input.json"))
	if err != nil {
		t.Fatal(err)
	}
	expected, err := os.ReadFile(filepath.Join(corpus, "expected.json"))
	if err != nil {
		t.Fatal(err)
	}
	m := listModel(t, string(schema))
	v := m.New(m.Lookup("Totals"))
	var r tabletext.Report
	if !m.Read(v, input, &r) || r.Clamped != 8 || r.Reason != "clamped: Totals.a" || r.Malformed {
		t.Fatalf("JSON report: %+v", r)
	}
	got, err := m.Write(v)
	if err != nil {
		t.Fatal(err)
	}
	var a, b any
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(expected, &b); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("JSON=%s want %s", got, expected)
	}
	writer := listModel(t, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(string(schema), " | total=5", ""), "string(5)", "string(16)"), "[..3]", "[..8]"))
	raw, err := tablewire.Encode(writer, place(t, writer, "Totals", string(input)))
	if err != nil {
		t.Fatal(err)
	}
	back := m.New(m.Lookup("Totals"))
	r = tabletext.Report{}
	if ok, err := tablewire.Decode(m, back, raw, &r); !ok || err != nil || r.Clamped != 10 || r.Malformed {
		t.Fatalf("wire count-before-total: %v %v %+v", ok, err, r)
	}
	got, err = m.Write(back)
	if err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(a, b) {
		t.Fatalf("wire JSON=%s want %s", got, expected)
	}
	for i := range v.Fields {
		t.Run(v.Fields[i].Def.Name, func(t *testing.T) {
			x := m.New(m.Lookup("Totals"))
			f := &x.Fields[i]
			old := f.Def.TextTotal
			f.Def.TextTotal = 0
			var rr tabletext.Report
			ok := m.ReadValue(f, []byte(`["abc","abc"]`), &rr)
			f.Def.TextTotal = old
			if !ok || !rr.Silent() {
				t.Fatal(rr)
			}
			if wire, err := tablewire.Encode(m, x); err == nil || len(wire) != 0 {
				t.Fatalf("wire write accepted total overflow: %x %v", wire, err)
			}
			if text, err := m.Write(x); err == nil || len(text) != 0 {
				t.Fatalf("JSON write accepted total overflow: %s %v", text, err)
			}
			if text, err := m.WriteValue(f); err == nil || len(text) != 0 {
				t.Fatalf("field write accepted total overflow: %s %v", text, err)
			}
		})
	}
}
