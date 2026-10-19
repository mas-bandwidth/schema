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
	cases := []struct {
		label, text, want string
	}{
		{"high alone", `{"s":"\ud800"}`, ""},
		{"low alone", `{"s":"\udfaa"}`, ""},
		{"high followed by plain", `{"s":"\ud800abc"}`, ""},
		{"high followed by raw utf8", `{"s":"\ud888ሴ"}`, ""},
		{"low then high", `{"s":"\udd1e\ud834"}`, ""},
		{"two highs", `{"s":"\ud800\ud800\n"}`, ""},
		{"key low alone", `{"\udfaa":0}`, "string_lone_surrogate: <root>"},
		{"wstring high alone", `{"w":"\ud800"}`, ""},
		{"unknown value high alone", `{"unknown":"\ud800"}`, "string_lone_surrogate: unknown"},
		{"nested unknown key", `{"unknown":{"a":1,"\udfaa":2}}`, "string_lone_surrogate: unknown"},
	}
	for _, tc := range cases {
		inst := m.New(root)
		var r tabletext.Report
		if m.Read(inst, []byte(tc.text), &r) {
			t.Errorf("%s: Read accepted %s", tc.label, tc.text)
			continue
		}
		if !r.Malformed || !strings.HasPrefix(r.Reason, "string_lone_surrogate") {
			t.Errorf("%s: report %+v, want Malformed with Reason starting string_lone_surrogate", tc.label, r)
		}
		if tc.want != "" && r.Reason != tc.want {
			t.Errorf("%s: Reason %q, want %q", tc.label, r.Reason, tc.want)
		}
	}
	// the astral character reads clean, as bytes and as code units
	inst := m.New(root)
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
