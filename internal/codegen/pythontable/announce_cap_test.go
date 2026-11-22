package pythontable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE ANNOUNCEMENT WRITER REFUSES A VOCABULARY ITS READERS REFUSE
// (docs/SPEC-TABLES.md §3.3, R8 follow-up): a receiver refuses an announcement
// of more than 4096 entries (its default maximum) as vocabulary_too_large, so
// announce() and announce_measure() raise Refused for a unit with 4097 and
// write the one with exactly 4096, which announce_read accepts. Python refuses
// in every build. Red when the refusal is removed.
func TestPythonAnnouncementRefusesMoreThan4096Entries(t *testing.T) {
	t.Parallel()
	for _, row := range []struct {
		entries int
		refused bool
	}{{4096, false}, {4097, true}} {
		schema := strings.Replace(tablewire.AnnounceSchema(row.entries), "table Root", "fixed table Root", 1)
		files, err := Generate(loadUnit(t, schema))
		if err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		writeTree(t, dir, files)
		probe := pyrun.ImportPrelude + `import idcap
`
		if row.refused {
			probe += `for call in (idcap.announce, idcap.announce_measure):
    try:
        call()
    except idcap.Refused as e:
        assert e.reason == "vocabulary_too_large", e.reason
    else:
        raise AssertionError("4097 entries: the announcement was written")
print("ok")
`
		} else {
			probe += `raw = idcap.announce()
assert idcap.announce_measure() == len(raw) > 0
v = idcap.MessageVocabulary()
idcap.announce_read(v, raw)
print("ok")
`
		}
		for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
			out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
			if err != nil || strings.TrimSpace(out) != "ok" {
				t.Fatalf("%d entries: %v\n%s\n%s", row.entries, err, out, stderr)
			}
		}
	}
}
