package lobstertable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/announcefit"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// announce_measure answers -1 and announce answers nothing for a unit whose
// announced vocabulary has 4097, and both write the one with exactly 4096,
// which announce_read accepts. Red when the refusal is removed.
func TestLobsterAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Lobster toolchain")
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		schema := announcefit.Schema(t, row.entries)
		g, err := generateLoadUnit(t, []string{writeSchema(t, "Probe", schema)})
		if err != nil {
			t.Fatal(err)
		}
		probe := `import .ProbeTable

print("measure " + schema_rt.announce_measure())
print("length " + length(schema_rt.announce()))
let v = schema_rt.new_vocabulary(4096)
print("read " + schema_rt.announce_read(v, schema_rt.announce(), schema_rt.LoadReport {}))
`
		if err := os.WriteFile(filepath.Join(g.dir, "cap.lobster"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		got := strings.TrimSpace(string(lobsterrun.Run(t, g.dir, "cap.lobster", nil)))
		if row.refused {
			if !strings.Contains(got, "measure -1") || !strings.Contains(got, "length 0") {
				t.Fatalf("%d entries: the writer did not refuse:\n%s", row.entries, got)
			}
			continue
		}
		if strings.Contains(got, "measure -1") || strings.Contains(got, "length 0") || !strings.Contains(got, "read 1") {
			t.Fatalf("%d entries: the writer refused or the reader did:\n%s", row.entries, got)
		}
	}
}
