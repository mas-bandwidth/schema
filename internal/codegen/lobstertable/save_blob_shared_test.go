package lobstertable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// blobSharedProgram loads one wire through the tolerant read and prints the
// report's blob_shared counter, then what save_reasoned answers: the refusal's
// name (or "ok") and the length it wrote.
func blobSharedProgram(g *loadUnit, rootName string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	fmt.Fprintf(&b, `let data = read_file("w0")
assert data
let rep = schema_rt.LoadReport {}
let rec = schema_rt.load_tolerant(%q, data, rep)
assert rec
let out, why = schema_rt.save_reasoned(rec)
print("shared {rep.blob_shared}")
print("save " + (if why == "": "ok" else: why) + " {length(out)}")
`, rootName)
	return b.String()
}

// runBlobShared generates tables/blobs, runs the program over the corpus's
// shared-blob instance and returns its two lines.
func runBlobShared(t *testing.T, patch func(src string) string) []string {
	t.Helper()
	units, _ := readManifest(t)
	paths, err := compiler.GatherPaths(prefix(units["blobdemo"]))
	if err != nil {
		t.Fatal(err)
	}
	g, err := generateLoadUnit(t, paths)
	if err != nil {
		t.Fatalf("blobdemo is refused: %v", err)
	}
	if patch != nil {
		path := filepath.Join(g.dir, "schema_save.lobster")
		src, err := os.ReadFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(patch(string(src))), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	wire, err := os.ReadFile(filepath.Join(root, "testdata", "wire", "tables", "blob_shared.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "main.lobster"), []byte(blobSharedProgram(g, "Catalog")), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(lobsterrun.Run(t, g.dir, "main.lobster", map[string][]byte{"w0": wire})), "\n"), "\n")
	if len(out) != 2 {
		t.Fatalf("the program printed %d lines, want 2: %q", len(out), out)
	}
	return out
}

// TestLobsterTableSaveRefusesASharedBlob holds docs/SPEC-TABLES.md §2.5: the
// TOLERANT read keeps a blob named from two slots as the ONE node it decoded
// and counts the extra naming, and every writer REFUSES that graph by the
// name blob_shared rather than write a copy per slot. The corpus's
// blob_shared instance is the witness.
func TestLobsterTableSaveRefusesASharedBlob(t *testing.T) {
	t.Parallel()
	out := runBlobShared(t, nil)
	if out[0] != "shared 1" {
		t.Fatalf("the tolerant read did not count the shared blob once: %q", out[0])
	}
	if out[1] != "save blob_shared 0" {
		t.Fatalf("the save did not refuse the shared blob by name: %q", out[1])
	}
}

// TestLobsterTableSaveRefusesASharedBlobNegativeControl plants the old
// behaviour back — the save graph never marks a second naming — and requires
// the refusal to go: the save then writes a copy per slot, which is what the
// test above refuses.
func TestLobsterTableSaveRefusesASharedBlobNegativeControl(t *testing.T) {
	t.Parallel()
	const anchor = "                g.blob_shared = true\n"
	out := runBlobShared(t, func(src string) string {
		if n := strings.Count(src, anchor); n != 1 {
			t.Fatalf("the blob_shared anchor is in the emitted runtime %d times, want 1", n)
		}
		return strings.Replace(src, anchor, "                g.blob_shared = false\n", 1)
	})
	if !strings.HasPrefix(out[1], "save ok ") || out[1] == "save ok 0" {
		t.Fatalf("the patched save still refuses: %q", out[1])
	}
	t.Logf("the refusal went under the patch: %s", out[1])
}
