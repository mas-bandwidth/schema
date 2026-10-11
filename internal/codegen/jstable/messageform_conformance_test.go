package jstable

// THE MESSAGE FORM'S CONFORMANCE GATE (docs/SPEC-TABLES.md §3.3,
// test/conformance/README.md): the driver's `message` surface.
//
// Two clauses. The first holds the driver SOURCE to the contract: it registers
// the `message` surface in its `list` and answers a message row rather than
// writing `<case>.absent`, so a row the manifest names is answered and not
// silently dropped. The second runs the harness's message rows — the
// round trip AnnounceRead / LoadMessages / SaveMessages against the pinned
// message-form goldens — when the leg's own toolchain (node) is present, and
// skips by name when it is not.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormConformanceJavaScript holds the JS leg's `message` surface to
// the manifest: the driver source answers `message` (no ABSENT for a message
// row) and the harness's message rows pass under the leg's own node.
func TestMessageFormConformanceJavaScript(t *testing.T) {
	t.Parallel()

	// ---- the driver SOURCE answers `message` (docs/SPEC-TABLES.md §3.3) ----
	//
	// A message row is answered, never dropped to ABSENT: the driver registers
	// the surface in `list` and its message surface names no `.absent` fallback,
	// which is what the README states for a port that carries the form ("the
	// driver writes ... the bytes" against "the message-form golden").
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "js", "main.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(src)
	if !strings.Contains(text, "\"message\\n") {
		t.Error("the driver's `list` does not register the `message` surface")
	}
	if !strings.Contains(text, "surfaceMessage") {
		t.Error("the driver wires no `message` surface function")
	}
	if strings.Contains(text, ".absent") {
		t.Error("the driver's message surface drops a message row to ABSENT")
	}

	// ---- the harness's message rows pass under the leg's own node ----
	//
	// The toolchain is the leg's own ($NODE, the same gate the fixed form's
	// versioning rows skip on). Without node there is nothing to run the
	// driver with, and the row is skipped by name rather than reported green.
	node := jsNode(t)

	repo := filepath.Join("..", "..", "..")
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("the conformance manifest is not at %s: %v", manifest, err)
	}
	// The driver answers the message surface from the generated tree the
	// harness points SCHEMA_JS_GENERATED at; this test generates the four
	// message units' tree and points the SAME driver at it.
	tree := filepath.Join(t.TempDir(), "tree")
	if err := writeMessageTree(t, tree); err != nil {
		t.Fatal(err)
	}
	outDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(repo, "test", "conformance", "js", "main.mjs"), manifest, "message", outDir)
	cmd.Dir = repo
	cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+tree)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the driver's message surface failed: %v\n%s", err, out)
	}
	// Every message row the manifest names is answered, and its answer is the
	// pinned message-form golden, byte for byte.
	rows := manifestMessageRows(t, manifest)
	if len(rows) == 0 {
		t.Fatal("the manifest names no message row")
	}
	for _, row := range rows {
		got, err := os.ReadFile(filepath.Join(outDir, row.name))
		if err != nil {
			t.Errorf("%s: the driver wrote no answer: %v", row.name, err)
			continue
		}
		want, err := os.ReadFile(filepath.Join(repo, row.wire))
		if err != nil {
			t.Errorf("%s: %v", row.name, err)
			continue
		}
		if string(got) != string(want) {
			t.Errorf("%s: the driver's message bytes differ from the pinned golden (got %d bytes, want %d)",
				row.name, len(got), len(want))
		}
	}
}

// writeMessageTree generates the four message units' JavaScript and lays it
// out the way make/js.mk does, one subdirectory per unit, so the driver's
// SCHEMA_JS_GENERATED points at it exactly as the harness's does.
func writeMessageTree(t *testing.T, root string) error {
	t.Helper()
	units := []struct{ dir, schemas string }{
		{"backend", "tables/backend"},
		{"vocab", "tables/vocab"},
		{"vocab9", "tables/vocab9"},
		{"pointers", "tables/pointers"},
	}
	for _, u := range units {
		unit, err := loadUnitDir(t, filepath.Join("..", "..", "..", u.schemas))
		if err != nil {
			return err
		}
		files, err := js.Generate(unit)
		if err != nil {
			return err
		}
		tables, err := GenerateLineage(unit, nil)
		if err != nil {
			return err
		}
		maps.Copy(files, tables)
		out := filepath.Join(root, u.dir)
		if err := os.MkdirAll(out, 0o755); err != nil {
			return err
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
				return err
			}
		}
	}
	return nil
}

// loadUnitDir reads one unit's every .schema file into an ir.Unit, the same
// lowering the compiler does.
func loadUnitDir(t *testing.T, dir string) (*ir.Unit, error) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	var sources []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		path := filepath.Join(dir, e.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			return nil, err
		}
		f, perrs := parser.Parse(e.Name(), data)
		if len(perrs) > 0 {
			return nil, fmt.Errorf("parse %s: %v", e.Name(), perrs[0])
		}
		sources = append(sources, check.SourceFile{
			Path: path, Name: e.Name(), Base: strings.TrimSuffix(e.Name(), ".schema"),
			Bytes: data, AST: f,
		})
	}
	u, cerrs := check.Unit(sources)
	if len(cerrs) > 0 {
		return nil, fmt.Errorf("check %s: %v", dir, cerrs[0])
	}
	return u, nil
}

type messageRow struct {
	name string
	wire string
}

// manifestMessageRows reads the committed manifest's `message` lines, name and
// message-form wire, which is the row list the driver's surface must answer.
func manifestMessageRows(t *testing.T, path string) []messageRow {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var rows []messageRow
	for _, raw := range strings.Split(string(data), "\n") {
		fields := strings.Fields(raw)
		if len(fields) < 6 || fields[0] != "message" {
			continue
		}
		rows = append(rows, messageRow{name: fields[1], wire: fields[5]})
	}
	return rows
}
