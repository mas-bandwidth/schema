package python

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
)

// A keyed array in a type is positional on the packet wire (SPEC §3.1,
// SPEC-TABLES §2.4); it has the same extent and bytes as [E.Max]T.
func TestPythonPacketKeyedArray(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	schema := `package keyedpacket

enum Team { Red, Blue, Green }

type Keyed
{
    scores [Team]uint8
}

type Positional
{
    scores [Team.Max]uint8
}
`
	if err := os.WriteFile(filepath.Join(src, "Keyed.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u := loadPythonUnit(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	probe := pyrun.ImportPrelude + `
from keyedpacket import InvalidValue, Keyed, Positional

expected = bytes.fromhex("112233")
for cls in (Keyed, Positional):
    value = cls()
    assert len(value.scores) == 3
    value.scores = [0x11, 0x22, 0x33]
    assert value.write() == expected
    assert cls.read(expected).scores == value.scores
    value.scores = [1, 2]
    try:
        value.write()
    except InvalidValue:
        pass
    else:
        raise AssertionError("short keyed/positional array accepted")
print("ok")
`
	for _, run := range []struct {
		name string
		fn   func(testing.TB, string, string, map[string][]byte) []byte
	}{{"current", pyrun.Run}, {"floor", pyrun.RunFloor}} {
		t.Run(run.name, func(t *testing.T) {
			dir := t.TempDir()
			writeTree(t, dir, files)
			if got := strings.TrimSpace(string(run.fn(t, dir, "main.py", map[string][]byte{"main.py": []byte(probe)}))); got != "ok" {
				t.Fatalf("probe: %s", got)
			}
		})
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	if out, err := pyrun.Analyze(t, dir, u.Package); err != nil || !strings.Contains(out, "Success: no issues found") {
		t.Fatalf("mypy --strict: %v\n%s", err, out)
	}
}
