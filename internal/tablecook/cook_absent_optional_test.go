package tablecook_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// THE PAYLOAD OF AN ABSENT OPTIONAL IS DEAD BYTES (docs/SPEC-TABLES.md §7.2;
// docs/rulings.json `fixed-dead-bytes-are-zero`): it is zero on write and
// refused on read when not zero. A field that is not supplied READS its
// declared default — that is a read rule, and it does not put the default into
// dead storage. The Go reference cook must therefore write zero for an absent
// optional's payload, whatever the element's declared default is.
func TestCookAbsentOptionalPayloadZero(t *testing.T) {
	dir := t.TempDir()
	src := "package probe\n\n" +
		"type Pose\n{\n" +
		"    x float32 = 0.5\n" +
		"}\n\n" +
		"fixed table Root\n{\n" +
		"    spawn ?Pose\n" +
		"}\n"
	if err := os.WriteFile(filepath.Join(dir, "Probe.schema"), []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	u := unit(t, dir)
	m := tabletext.NewModel(u)
	inst := m.New(m.Lookup("Root"))
	// a fresh instance keeps the optional ABSENT: present is false and nothing
	// sets it, which is the value the read side returns the declared default for
	for _, big := range []bool{false, true} {
		cooked, err := tablecook.Cook(m, inst, tablecook.Options{Big: big})
		if err != nil {
			t.Fatalf("big=%v: cook: %v", big, err)
		}
		h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u))
		if err != nil {
			t.Fatalf("big=%v: read header: %v", big, err)
		}
		// the record's only field is the absent optional, so the whole data
		// part must be zero — the presence byte and every byte of the payload
		for i, b := range h.Data(cooked) {
			if b != 0 {
				t.Fatalf("big=%v: an absent optional's payload holds 0x%02x at data offset %d, want zero", big, b, i)
			}
		}
	}
}
