package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

const utf8RefusalSchema = `package probe

fixed table Root
{
    s string(16)
    w wstring(16)
}
`

func utf8RefusalModel(t *testing.T) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.schema"), []byte(utf8RefusalSchema), 0o644); err != nil {
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
	return tabletext.NewModel(u)
}

// A BYTE SEQUENCE THAT IS NOT WELL-FORMED UTF-8 IS REFUSED BY NAME AT EVERY
// STRING SITE (docs/SPEC-TABLES.md §16.3): a field value, a key, a skipped
// value, a wstring. The read returns false, the report is Malformed, and its
// Reason starts `string_not_utf8`. A well-formed sequence still reads clean
// and exact.
func TestEngineStringRefusesInvalidUTF8(t *testing.T) {
	t.Parallel()
	m := utf8RefusalModel(t)
	root := m.Lookup("Root")
	refusals := []string{
		`{"s":"` + "\xff" + `"}`,
		`{"s":"` + "\xed\xa0\x80" + `"}`,
		`{"s":"` + "\xc0\xaf" + `"}`,
		`{"` + "\xff" + `":1}`,
		`{"w":"` + "\xff" + `"}`,
		`{"s":"` + "\xf4\x90\x80\x80" + `"}`,
		`{"s":"` + "\xe0\xff" + `"}`,
	}
	for _, text := range refusals {
		inst := m.New(root)
		var r tabletext.Report
		if m.Read(inst, []byte(text), &r) {
			t.Errorf("%s: Read accepted ill-formed UTF-8", text)
			continue
		}
		if !r.Malformed || !strings.HasPrefix(r.Reason, "string_not_utf8") {
			t.Errorf("%s: report %+v, want Malformed with Reason starting string_not_utf8", text, r)
		}
	}

	// a narrow field's well-formed three-byte sequence reads clean and exact
	inst := m.New(root)
	var r tabletext.Report
	cleanNarrow := `{"s":"` + "\xf0\x9f\x98\x80" + `"}` // U+1F600
	if !m.Read(inst, []byte(cleanNarrow), &r) || r.Malformed {
		t.Fatalf("narrow clean read %s: report %+v", cleanNarrow, r)
	}
	if fv, ok := inst.FieldByKey("s"); !ok || string(fv.Cell.Str) != "\xf0\x9f\x98\x80" {
		t.Fatalf("narrow: got %q", fv.Cell.Str)
	}

	// a wide field's well-formed two-byte sequence reads clean and exact
	inst = m.New(root)
	r = tabletext.Report{}
	cleanWide := `{"w":"` + "\xe2\x82\xac" + `"}` // U+20AC EURO SIGN
	if !m.Read(inst, []byte(cleanWide), &r) || r.Malformed {
		t.Fatalf("wide clean read %s: report %+v", cleanWide, r)
	}
	if fv, ok := inst.FieldByKey("w"); !ok || len(fv.Cell.Units) != 1 || fv.Cell.Units[0] != 0x20ac {
		t.Fatalf("wide: got %v", fv.Cell.Units)
	}
}
