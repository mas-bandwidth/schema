package squirreltable_test

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// textFormDocumentSchema mirrors the fixed Document contract of
// test/conformance/textform (docs/SPEC-TABLES.md §16.9).
const textFormDocumentSchema = `
package probe
enum State { unknown, done }
flags Features { Fast, Safe }
type Node {
  id string(16)
  title string(32)
  state State
}
type TextItem {
  line string(80)
}
union Choice {
  amount int32
  ping
}
fixed table Document {
  schema uint8 | min = 0, max = 3
  root string(32)
  title string(32)
  state State
  count int32 | min = 0, max = 10
  enabled bool
  text [..4]TextItem
  nodes [..3]Node
  pairs [2]int32
  optional ?[..4]int32
  wide wstring(4)
  blob bytes(4) | json = "bytes"
  low int128 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727, json = "signed"
  high uint128 | json = "unsigned"
  fraction fixed(16,16) | min = -32768, max = 32767
  real float32 | json = "float"
  time timestamp
  flags Features
  keyed [State]int32
  choice Choice
}
`

// documentPrelude prints, per vector, the base64 of the rewritten document and
// the value's JSON, or the refusal name. The Go side compares bytes exactly.
const documentPrelude = `local inputs = dofile("inputs.nut", true);
local run = function(key, text) {
  local limits = { bytes = 65536, tokens = 8192, nodes = 4096, alloc = 65536, depth = 128 };
  local report = { refused = false, name = null, offset = 0, path = null };
  local r = D.LoadTextForm(T, "Document", text, limits, report);
  if (r == null) { print(key + " refuse " + (report.name == null ? "null" : report.name) + "\n"); return; }
  local re = D.SaveTextForm(T, "Document", r);
  if (re == null) { print(key + " savenull\n"); return; }
  print(key + " ok " + re + "\n");
};
`

// TestSquirrelDocumentVectors runs the §16.9 document vectors through the
// Squirrel text form: the same text out, the same value, and the same named
// refusals as the Go oracle (internal/codegen/gotable/textform_test.go
// TestDocumentVectors, TestDocumentEdits, TestDocumentRefusals). It prints the
// number of vectors exercised.
func TestSquirrelDocumentVectors(t *testing.T) {
	t.Parallel()
	root, err := sqrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	vectors := filepath.Join(root, "test", "conformance", "textform")

	// the fixed Document unit from the conformance contract
	files, err := squirrel.Generate(unitFromSchema(t, textFormDocumentSchema))
	if err != nil {
		t.Fatalf("the type surface refused: %v", err)
	}
	tables, err := squirreltable.Generate(unitFromSchema(t, textFormDocumentSchema))
	if err != nil {
		t.Fatalf("the table surface refused: %v", err)
	}
	dir := t.TempDir()
	out := filepath.Join(dir, "probe")
	if err := os.MkdirAll(out, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		writeFile(t, filepath.Join(out, name), data)
	}
	for name, data := range tables {
		writeFile(t, filepath.Join(out, name), data)
	}

	stems := []string{"roadmap", "layout", "kinds"}
	inputs := map[string][]byte{}
	var p strings.Builder
	p.WriteString("local schema = dofile(\"probe/schema_module.nut\", true);\n")
	p.WriteString("local T = schema.require(\"DocumentTables\");\n")
	p.WriteString("local D = schema.require(\"schema_table_document\");\n")
	p.WriteString(documentPrelude)
	for _, stem := range stems {
		in, err := os.ReadFile(filepath.Join(vectors, stem+".input.sexp"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[stem] = in
		fmt.Fprintf(&p, "run(%q, inputs[%q]);\n", stem, stem)
	}
	// refusals by name
	names, err := filepath.Glob(filepath.Join(vectors, "refusals", "*.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range names {
		stem := strings.TrimSuffix(filepath.Base(name), ".sexp")
		if stem == "duplicate-map" || stem == "forward-reference" {
			continue // Graph's, run by the variable unit
		}
		in, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		inputs["r_"+stem] = in
		fmt.Fprintf(&p, "run(%q, inputs[%q]);\n", "r_"+stem, "r_"+stem)
	}
	writeFile(t, filepath.Join(dir, "main.nut"), []byte(p.String()))

	stdout := sqrun.Run(t, dir, "main.nut", inputs)
	lines := strings.Split(strings.TrimRight(string(stdout), "\n"), "\n")
	count := 0
	for _, line := range lines {
		f := strings.Fields(line)
		if len(f) < 2 {
			t.Fatalf("probe line %q is not a case and result", line)
		}
		stem := f[0]
		count++
		if f[1] != "ok" {
			want := ""
			if strings.HasPrefix(stem, "r_") {
				b, err := os.ReadFile(filepath.Join(vectors, "refusals", strings.TrimPrefix(stem, "r_")+".reason"))
				if err != nil {
					t.Fatal(err)
				}
				want = strings.TrimSpace(string(b))
			}
			t.Errorf("%s: refused as %s, want %s", stem, f[2], want)
			continue
		}
		want, err := base64.StdEncoding.DecodeString(f[2])
		if err != nil {
			t.Errorf("%s: rewrite is not base64: %v", stem, err)
			continue
		}
		in, err := os.ReadFile(filepath.Join(vectors, stem+".input.sexp"))
		if err != nil {
			t.Fatal(err)
		}
		if string(want) != string(in) {
			t.Errorf("%s: the rewrite is not the input byte for byte", stem)
		}
	}
	t.Logf("ran %d document vectors", count)
}

// unitFromSchema parses a schema file body into a unit.
func unitFromSchema(t *testing.T, body string) *ir.Unit {
	t.Helper()
	dir := t.TempDir()
	schema := filepath.Join(dir, "probe.schema")
	writeFile(t, schema, []byte(body))
	paths, err := compiler.GatherPaths([]string{schema})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	return u
}
