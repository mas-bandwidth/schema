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

// Malformed wire must refuse. Writer refusal is checked with an invalid
// graph built directly in memory, never recovered from damaged input.
func blobSharedProgram(g *loadUnit, rootName string) string {
	var b strings.Builder
	for _, m := range g.mods {
		fmt.Fprintf(&b, "import .%s\n", m)
	}
	b.WriteString("import .load\nimport .schema_save\n")
	fmt.Fprintf(&b, `let data = read_file("w0")
assert data
let rejected = schema_rt.load(%q, data, schema_rt.LoadReport {})
print("read " + (if rejected: "accepted" else: "refused"))
schema_rt.release(rejected)
let rec = schema_rt.new_record(schema_rt.table_of(%q))
let shared = schema_rt.Node { kind: 3, rec: nil, blob: "abc" }
for(rec.fields) sl:
    if sl.fd.name == "thumb" or sl.fd.name == "alias":
        sl.cell.blob = shared.blob
        sl.cell.blob_node = shared
let out, why = schema_rt.save_reasoned(rec)
print("save " + (if why == "": "accepted" else: "refused") + " {length(out)}")
for(rec.fields) sl:
    if sl.fd.name == "alias":
        sl.cell.blob_node = schema_rt.Node { kind: 3, rec: nil, blob: "abc" }
let separate, separate_reason = schema_rt.save_reasoned(rec)
assert separate_reason == "" and length(separate) > 0
let valid = schema_rt.load(%q, separate, schema_rt.LoadReport {})
assert valid and schema_rt.save(valid) == separate
schema_rt.release(valid)
schema_rt.release(rec)
print("independent blobs accepted")
`, rootName, rootName, rootName)
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
	if len(out) != 3 {
		t.Fatalf("the program printed %d lines, want 3: %q", len(out), out)
	}
	return out
}

// Both reads and writes refuse shared blobs. Equal, separately owned blobs
// retain their values and canonical output.
func TestLobsterTableSaveRefusesASharedBlob(t *testing.T) {
	t.Parallel()
	out := runBlobShared(t, nil)
	if out[0] != "read refused" || out[1] != "save refused 0" || out[2] != "independent blobs accepted" {
		t.Fatalf("blob ownership contract: %q", out)
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
	if !strings.HasPrefix(out[1], "save accepted ") || out[1] == "save accepted 0" {
		t.Fatalf("the patched save still refuses: %q", out[1])
	}
	t.Logf("the refusal went under the patch: %s", out[1])
}
