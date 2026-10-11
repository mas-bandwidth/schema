package jstable

// THE MESSAGE FORM'S CONFORMANCE GATE (docs/SPEC-TABLES.md §3.3,
// test/conformance/README.md, "The surfaces"): the driver's `message` surface.
//
// Two clauses. The first holds the driver SOURCE to the contract: it registers
// the `message` surface in its `list`, its `messageRows` table is the
// manifest's own distinct (unit, root) pairs, and its handler carries the
// path that writes the saved bytes rather than only `<case>.absent`. The
// second RUNS the driver over the manifest's message rows against a generated
// tree and requires one file per row — the bytes when the leg's tree carries
// the form-2 verbs, `<case>.absent` when a row's unit or root the form refuses
// is not reachable — and at least one answer, so a built tree that answers
// nothing fails rather than skipping. It skips BY NAME when node is absent.

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
)

// TestMessageFormConformanceJavaScript holds the JavaScript conformance
// driver's MESSAGE row list to the manifest's and runs the surface over the
// manifest's own rows (docs/SPEC-TABLES.md §3.3, form byte 2).
func TestMessageFormConformanceJavaScript(t *testing.T) {
	t.Parallel()

	t.Run("row_list_is_the_manifest", func(t *testing.T) {
		t.Parallel()

		// THE ROW LIST IS THE MANIFEST'S: the manifest's `message` lines are
		// the corpus's claim, the driver's `messageRows` table is the leg's,
		// and the two must be one set. A row the manifest names and the driver
		// does not is a case that silently reads ABSENT; a row the driver names
		// and the manifest does not is a claim with no corpus behind it.
		want := manifestMessageRoots(t)
		if len(want) == 0 {
			t.Fatal("the manifest names no message row — this gate would pass over the empty set")
		}
		got := driverMessageRoots(t)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("the JavaScript driver's message row list is not the manifest's:\n driver   %v\n manifest %v", got, want)
		}

		// THE SURFACE IS ANNOUNCED. The driver's `list` names the surfaces it
		// implements; `message` must be among them, or every row above is an
		// absence the matrix cannot distinguish from a feature the leg lacks
		// (test/conformance/README.md, "Exit codes").
		source := driverSource(t)
		if !strings.Contains(source, "message\\n") {
			t.Error("the JavaScript driver does not list `message` among its surfaces (test/conformance/README.md, \"The surfaces\")")
		}

		// THE DRIVER SOURCE ANSWERS A ROW, rather than declaring the surface
		// and refusing every case. The surface's answer path writes the bytes
		// `SaveMessages` produced; `<case>.absent` is only the fallback for a
		// row this leg cannot reach. A handler with no byte-writing path would
		// make the row list a claim nothing could ever satisfy.
		start := strings.Index(source, "async function surfaceMessage")
		if start < 0 {
			t.Fatal("the JavaScript driver declares no surfaceMessage handler (test/conformance/js/main.mjs)")
		}
		if block := surfaceBlock(source[start:]); !strings.Contains(block, "writeFileSync(join(outDir, f[1]), buffer)") {
			t.Error("the JavaScript driver's message handler has no path that writes the saved bytes: every row could only ever answer ABSENT")
		}
	})

	t.Run("harness_message_rows", func(t *testing.T) {
		t.Parallel()
		assertDriverAnswersMessages(t)
	})
}

// manifestMessageRoots is the manifest's distinct (unit, root) pairs, its unit
// taken from the `connection` line each `message` row names, sorted.
func manifestMessageRoots(t *testing.T) []string {
	t.Helper()
	_, connections, rows := parseMessageManifest(t)
	seen := map[string]bool{}
	var roots []string
	for _, row := range rows {
		conn, ok := connections[row.connection]
		if !ok {
			t.Fatalf("message %s names connection %s, which no connection row declares", row.name, row.connection)
		}
		key := conn.unit + "/" + row.root
		if !seen[key] {
			seen[key] = true
			roots = append(roots, key)
		}
	}
	sort.Strings(roots)
	return roots
}

// driverMessageRoots is the driver's own row list, the `messageRows` table in
// test/conformance/js/main.mjs: one `["<unit>", "<root>", ...]` line a row.
func driverMessageRoots(t *testing.T) []string {
	t.Helper()
	source := driverSource(t)
	start := strings.Index(source, "const messageRows = [")
	if start < 0 {
		t.Fatalf("the JavaScript driver declares no messageRows table (test/conformance/js/main.mjs)")
	}
	end := strings.Index(source[start:], "];")
	if end < 0 {
		t.Fatal("the messageRows table is not closed: no `];` follows `const messageRows = [`")
	}
	block := source[start : start+end]
	re := regexp.MustCompile(`\["([A-Za-z0-9_]+)", "([A-Za-z0-9_]+)"`)
	var roots []string
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		roots = append(roots, m[1]+"/"+m[2])
	}
	sort.Strings(roots)
	if len(roots) == 0 {
		t.Fatalf("the messageRows table carries no row (test/conformance/js/main.mjs)")
	}
	return roots
}

// surfaceBlock is the text of one top-level function in the driver, up to the
// next column-zero `}` line, so an assertion about the handler is not satisfied
// by a byte-writing call living in a different surface.
func surfaceBlock(from string) string {
	if end := strings.Index(from, "\n}\n"); end >= 0 {
		return from[:end]
	}
	return from
}

func driverSource(t *testing.T) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "js", "main.mjs"))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

// assertDriverAnswersMessages generates the driver's generated tree, RUNS the
// driver's message surface over the committed manifest, and requires one file
// per manifest message row — the bytes when the leg's tree carries the form-2
// verbs, `<case>.absent` when it does not — with at least one answer, so a
// built tree that answers nothing FAILS rather than skipping. It SKIPS BY NAME
// when node is absent.
func assertDriverAnswersMessages(t *testing.T) {
	t.Helper()
	node := jsNode(t)

	repo := filepath.Join("..", "..", "..")
	manifest, err := filepath.Abs(filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("the conformance manifest is not at %s: %v", manifest, err)
	}
	// The driver imports block, PaddedBlock and the pointers cook at module
	// load, so the tree must carry them beside the message units the harness
	// points SCHEMA_JS_GENERATED at.
	tree := t.TempDir()
	writeConformanceTree(t, tree, [][2]string{
		{"block", "tables/block"},
		{"pointers", "tables/pointers"},
		{"vocab", "tables/vocab"},
		{"vocab9", "tables/vocab9"},
		{"backend", "tables/backend"},
	})
	out := t.TempDir()
	absRepo, err := filepath.Abs(repo)
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(node, filepath.Join(absRepo, "test", "conformance", "js", "main.mjs"), manifest, "message", out)
	cmd.Dir = absRepo
	cmd.Env = append(os.Environ(), "SCHEMA_JS_GENERATED="+tree)
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the driver's message surface failed: %v\n%s", err, data)
	}

	// ONE FILE PER ROW: the driver answers every manifest message row, with the
	// bytes or `<case>.absent`, so its output names the row list exactly.
	_, _, rows := parseMessageManifest(t)
	answered, absent := 0, 0
	for _, row := range rows {
		path := filepath.Join(out, row.name)
		if _, err := os.Stat(path + ".absent"); err == nil {
			absent++
			continue
		}
		got, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("%s: the driver wrote no answer and no `<case>.absent`: %v", row.name, err)
		}
		want, err := os.ReadFile(row.batch)
		if err != nil {
			t.Fatalf("%s: %v", row.name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s: the driver's message bytes differ from the pinned golden (got %d bytes, want %d)", row.name, len(got), len(want))
		}
		answered++
	}
	if answered == 0 {
		t.Fatalf("the JavaScript leg's generated tree carries no form-2 verbs: the driver answered all %d row(s) absent", absent)
	}
	t.Logf("the driver answered %d message row(s), %d absent", answered, absent)
}

// writeConformanceTree generates each named unit's whole JavaScript tree — the
// packet modules and the table modules — into `root/<dir>`, the layout the
// Makefile writes and the driver's SCHEMA_JS_GENERATED reads. A unit this
// backend cannot generate (docs/SPEC-TABLES.md §11) is skipped: the driver
// answers its rows `<case>.absent`.
func writeConformanceTree(t *testing.T, root string, units [][2]string) {
	t.Helper()
	for _, u := range units {
		unit := loadUnitPaths(t, []string{repoPath(u[1])})
		files, err := js.Generate(unit)
		if err != nil {
			continue
		}
		tables, err := GenerateLineage(unit, nil)
		if err != nil {
			continue
		}
		maps.Copy(files, tables)
		out := filepath.Join(root, u[0])
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		for name, data := range files {
			if strings.Contains(name, "/") {
				continue
			}
			if err := os.WriteFile(filepath.Join(out, name), data, 0o600); err != nil {
				t.Fatal(err)
			}
		}
	}
}
