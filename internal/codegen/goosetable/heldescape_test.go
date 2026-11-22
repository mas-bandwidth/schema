package goosetable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
)

// TestGooseKindHeldEscapeRefuses (R17: the newer reader reads old data, never
// the other way): kind 31 is held by name and never written, so a reader meets
// it only as damage, exactly as 33 and 34. The shared input carries a kind 31
// field the reader cannot name; the plain and the retaining load each discard
// the whole read as malformed. The old reader skipped it by its L and read
// the file.
func TestGooseKindHeldEscapeRefuses(t *testing.T) {
	t.Parallel()
	c := readWireCorpus(t)
	files := wireUnitFiles(t, "tblvarevo1", c.units["tblvarevo1"], nil)
	b, err := os.ReadFile(filepath.Join(root, "testdata/wire/tables/framing/varevo_kind31_escape.bin"))
	if err != nil {
		t.Fatal(err)
	}
	files["held.bin"] = b
	program := strings.NewReplacer("ListNode", "EvoDoc").Replace(gooseLoadProbe)
	files["main.goose"] = []byte(program + fmt.Sprintf("fn main() {\n    probe(%q);\n}\n", "held"))
	got := strings.TrimSpace(string(gooserun.Run(t, t.TempDir(), "main.goose", files)))
	const want = "held plain 0 1\nheld retain 0 1"
	if got != want {
		t.Errorf("kind 31 at an unnamed field: got\n%s\nwant\n%s", got, want)
	}
}
