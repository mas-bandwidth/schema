package pythontable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonPointerStreamManifest(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "both pinned Python interpreters and the stream corpus")
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	units, err := readManifestUnits(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range units {
		if entry.key == "streamdemo" {
			u, err := loadManifestUnit(repo, entry.paths)
			if err != nil {
				t.Fatal(err)
			}
			var found bool
			for _, field := range u.Tables["Feed"].Fields {
				if field.Name == "parts" {
					found = true
					if codecOf(field) != "pointer_array" {
						t.Fatalf("Feed.parts codec %s", codecOf(field))
					}
				}
			}
			if !found {
				t.Fatal("Feed.parts missing")
			}
			files, err := Generate(u)
			if err != nil {
				t.Fatal(err)
			}
			wire, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", "tables", "stream_parts.bin"))
			if err != nil {
				t.Fatal(err)
			}
			dir := t.TempDir()
			writeTree(t, dir, files)
			script := pyrun.ImportPrelude + `from streamdemo import Feed
with open("wire", "rb") as f: wire=f.read()
value=Feed.load(wire)
assert len(value.parts) > 0, "parts"
assert value.save() == wire, "roundtrip"
print("stream-parts")
`
			for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
				out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script), "wire": wire})
				if err != nil || strings.TrimSpace(out) != "stream-parts" {
					t.Fatalf("stream parts: %v %s %s", err, out, stderr)
				}
			}
			return
		}
	}
	t.Fatal("streamdemo absent from manifest")
}
