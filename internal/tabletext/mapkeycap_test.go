package tabletext_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A JSON map key is bounded by its own string(N) and by nothing else: the
// 255-byte cap is for schema names only and never reaches a map key
// (docs/SPEC-TABLES.md §16.3), and generated Go takes a 300-byte key.
func TestMapKeyBoundedOnlyByItsDeclaration(t *testing.T) {
	t.Parallel()
	dir := t.TempDir()
	const src = "package mapkeycap\n\ntable Maps\n{\n    names map[string(400)]int32\n}\n"
	if err := os.WriteFile(filepath.Join(dir, "MapKeyCap.schema"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, u := load(t, dir)
	m := tabletext.NewModel(u)
	key := strings.Repeat("x", 300)
	inst := m.New(m.Lookup("Maps"))
	var r tabletext.Report
	if !m.Read(inst, []byte(`{"names":{"`+key+`":7}}`), &r) || !r.Silent() {
		t.Fatalf("a 300-byte key under string(400) refused: %+v", r)
	}
	out, err := m.Write(inst)
	if err != nil || !strings.Contains(string(out), key) {
		t.Fatalf("write: %v", err)
	}
	var over tabletext.Report
	if m.Read(m.New(m.Lookup("Maps")), []byte(`{"names":{"`+strings.Repeat("x", 401)+`":7}}`), &over) || !over.Refused || !strings.HasPrefix(over.Reason, "length_over_bound") {
		t.Fatalf("a 401-byte key under string(400): %+v", over)
	}
}
