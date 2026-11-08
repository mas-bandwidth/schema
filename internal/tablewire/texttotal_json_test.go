package tablewire_test

import (
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"os"
	"path/filepath"
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
	m := listModel(t, string(schema))
	v := m.New(m.Lookup("Totals"))
	// THE CORPUS INPUT CARRIES LISTS PAST THEIR TOTAL AND THEIR BOUND, so both
	// the JSON read and the wire read refuse it (§2.13, §4); nothing is cut
	var r tabletext.Report
	if m.Read(m.New(m.Lookup("Totals")), input, &r) || !r.Refused {
		t.Fatalf("JSON report: %+v", r)
	}
	writer := listModel(t, strings.ReplaceAll(strings.ReplaceAll(strings.ReplaceAll(string(schema), " | total=5", ""), "string(5)", "string(16)"), "[..3]", "[..8]"))
	raw, err := tablewire.Encode(writer, place(t, writer, "Totals", string(input)))
	if err != nil {
		t.Fatal(err)
	}
	r = tabletext.Report{}
	if ok, err := tablewire.Decode(m, m.New(m.Lookup("Totals")), raw, &r); ok || !tablewire.ReadRefusal(err) || !r.Refused {
		t.Fatalf("wire past the total and the bound: %v %v %+v", ok, err, r)
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
