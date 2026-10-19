package tabletext_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestEngineStringRefusesLoneSurrogate pins docs/SPEC-TABLES.md §16.3's ruling
// through the engine's Model.Read: a lone or out-of-order surrogate escape is
// REFUSED BY NAME at every string site — a field value, a key, a wstring —
// never silently replaced with U+FFFD. The read returns false with the report
// Malformed and Reason starting string_lone_surrogate; an astral character
// still reads clean.
func TestEngineStringRefusesLoneSurrogate(t *testing.T) {
	t.Parallel()
	m, root := loneSurrogateModel(t)
	// <key> is the decoded key as written (§16.3), not a table-qualified
	// field name: a known field's value is named by its key, as a skipped
	// value is.
	cases := []struct {
		label, text, reason string
	}{
		{"high alone", `{"s":"\ud800"}`, "string_lone_surrogate: s"},
		{"low alone", `{"s":"\udfaa"}`, "string_lone_surrogate: s"},
		{"high followed by plain", `{"s":"\ud800abc"}`, "string_lone_surrogate: s"},
		{"high followed by raw utf8", `{"s":"\ud888ሴ"}`, "string_lone_surrogate: s"},
		{"low then high", `{"s":"\udd1e\ud834"}`, "string_lone_surrogate: s"},
		{"two highs", `{"s":"\ud800\ud800\n"}`, "string_lone_surrogate: s"},
		{"key low alone", `{"\udfaa":0}`, "string_lone_surrogate: <root>"},
		{"skipped unknown value", `{"unknown":"\ud800"}`, "string_lone_surrogate: unknown"},
		{"skipped known wrong shape", `{"s":["\ud800"]}`, "string_lone_surrogate: s"},
		{"bad key after good field", `{"s":"a","\udfaa":0}`, "string_lone_surrogate: <root>"},
		{"bad key after skipped field", `{"unknown":1,"\udfaa":0}`, "string_lone_surrogate: <root>"},
		{"bad key in skipped object", `{"unknown":{"a":1,"\udfaa":2}}`, "string_lone_surrogate: unknown"},
		{"wstring high alone", `{"w":"\ud800"}`, "string_lone_surrogate: w"},
	}
	for _, tc := range cases {
		inst := m.New(root)
		var r tabletext.Report
		if m.Read(inst, []byte(tc.text), &r) {
			t.Errorf("%s: Read accepted %s", tc.label, tc.text)
			continue
		}
		if !r.Malformed || r.Reason != tc.reason {
			t.Errorf("%s: report %+v, want Malformed with Reason %q", tc.label, r, tc.reason)
		}
	}
	// a long key inside a SKIPPED object is not a clamp: the key only names a
	// refusal (§16.3), so valid JSON reads clean with no clamp counted.
	inst := m.New(root)
	var lr tabletext.Report
	long := strings.Repeat("k", 300)
	if !m.Read(inst, []byte(`{"unknown":{"`+long+`":1}}`), &lr) || lr.Malformed || lr.Clamped != 0 {
		t.Fatalf("long key in a skipped object: %+v", lr)
	}
	// the astral character reads clean, as bytes and as code units
	inst = m.New(root)
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"s":"𝄞"}`), &r) || r.Malformed {
		t.Fatalf("astral string did not read clean: %+v", r)
	}
	want := []byte{0xF0, 0x9D, 0x84, 0x9E}
	if !bytes.Equal(inst.Fields[0].Cell.Str, want) {
		t.Fatalf("astral string is %x, want %x", inst.Fields[0].Cell.Str, want)
	}
	inst = m.New(root)
	r = tabletext.Report{}
	if !m.Read(inst, []byte(`{"w":"𝄞"}`), &r) || r.Malformed {
		t.Fatalf("astral wstring did not read clean: %+v", r)
	}
	units := inst.Fields[1].Cell.Units
	if len(units) != 2 || units[0] != 0xD834 || units[1] != 0xDD1E {
		t.Fatalf("astral wstring units are %v, want [d834 dd1e]", units)
	}
}

func loneSurrogateModel(t *testing.T) (*tabletext.Model, *ir.Struct) {
	t.Helper()
	dir := t.TempDir()
	schema := `package probe

fixed table Root
{
    s string(16)
    w wstring(16)
}
`
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	return m, m.Lookup("Root")
}
