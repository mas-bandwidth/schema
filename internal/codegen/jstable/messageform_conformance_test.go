package jstable

// THE MESSAGE FORM'S CONFORMANCE GATE (docs/SPEC-TABLES.md §3.3,
// test/conformance/README.md, "The surfaces"): the driver's `message` surface.
//
// Three clauses. The first holds the driver SOURCE to the contract: it
// registers the `message` surface in its `list`, its `messageRows` table is
// the manifest's own distinct (unit, root) pairs, and its handler carries the
// path that writes the saved bytes rather than only `<case>.absent`. The
// second RUNS the driver over the manifest's message rows against a generated
// tree and requires one file per row — the bytes when the leg's tree carries
// the form-2 verbs, `<case>.absent` when a row's unit or root the form refuses
// is not reachable — and at least one answer, so a built tree that answers
// nothing fails rather than skipping. It skips BY NAME when node is absent.
// The third holds the SHIPPED CONFORMANCE TARGET to the units the driver
// reads: `CONFORMANCE_LEGS` names only build/tables-generated-js/.stamp, so a
// row whose unit the stamp recipe does not generate can only ever answer
// `<case>.absent` in `make conformance`, however well the driver is wired.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
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

	t.Run("makefile_generates_the_message_units", func(t *testing.T) {
		t.Parallel()

		// THE CONFORMANCE TARGET BUILDS THE UNITS THE DRIVER READS. The
		// driver's message rows each name a generated unit; `make conformance`
		// builds only what `CONFORMANCE_LEGS` names, and for this leg that is
		// build/tables-generated-js/.stamp and nothing else (make/js.mk), so
		// the stamp recipe is the whole of the tree the harness hands the
		// driver (test/conformance/README.md, "ci.json"). A unit the recipe
		// does not generate cannot be imported by the driver's `load`, so every
		// row it carries is written `<case>.absent` and the matrix reads the
		// surface as the feature the leg lacks — the outcome this card exists
		// to replace.
		// EVERY UNIT THE DRIVER'S MESSAGE ROWS READ IS A UNIT THIS TARGET
		// MUST GENERATE, `backend` INCLUDED. The `backend` unit's Envelope
		// holds a table-side union, but the JavaScript fixed form CARRIES that
		// construct and refuses only the tables its own closure cannot hold
		// (docs/SPEC-TABLES.md §2.6, §3.4; compiler/target_javascript.go,
		// internal/codegen/jstable/fixedRefusal), so the unit is generated
		// whole and no row is exempt: an exemption here is exactly what let the
		// shipped recipe answer 5 rows and `<case>.absent` for 8 while the gate
		// stayed green.
		recipe := stampRecipe(t, "build/tables-generated-js/.stamp")
		carried := 0
		for _, dir := range driverMessageDirs(t) {
			carried++
			want := "--out build/tables-generated-js/" + dir + " tables/" + dir
			if !strings.Contains(recipe, want) {
				t.Errorf("the JavaScript conformance target does not generate the `%s` unit its message driver reads: want `./bin/schema generate --lang js %s` in the build/tables-generated-js/.stamp recipe (make/js.mk)", dir, want)
			}
		}
		if carried == 0 {
			t.Fatal("the driver's message rows name no unit this target carries — the clause would pass over the empty set")
		}
	})

	t.Run("harness_message_rows", func(t *testing.T) {
		t.Parallel()
		assertDriverAnswersMessages(t)
	})
}

// driverMessageDirs is the set of generated unit directories the driver's
// messageRows table reads, the first component of each row's module path
// (`backend/BackenddemoTable.js` -> `backend`), sorted.
func driverMessageDirs(t *testing.T) []string {
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
	re := regexp.MustCompile(`\["[A-Za-z0-9_]+", "[A-Za-z0-9_]+", "([A-Za-z0-9_]+)/`)
	seen := map[string]bool{}
	var dirs []string
	for _, m := range re.FindAllStringSubmatch(source[start:start+end], -1) {
		if !seen[m[1]] {
			seen[m[1]] = true
			dirs = append(dirs, m[1])
		}
	}
	sort.Strings(dirs)
	if len(dirs) == 0 {
		t.Fatalf("the messageRows table carries no row (test/conformance/js/main.mjs)")
	}
	return dirs
}

// stampRecipe is the recipe lines of one make target, the tab-indented lines
// that follow its `target:` line up to the next column-zero line, so an
// assertion about what the target builds reads the recipe and not a comment
// beside it.
func stampRecipe(t *testing.T, target string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "make", "js.mk"))
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(data), "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, target+":") {
			start = i
			break
		}
	}
	if start < 0 {
		t.Fatalf("make/js.mk declares no %s target", target)
	}
	var b strings.Builder
	for _, l := range lines[start+1:] {
		if !strings.HasPrefix(l, "\t") {
			break
		}
		b.WriteString(l)
		b.WriteString("\n")
	}
	return b.String()
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
	// bytes or `<case>.absent`, so its output names the row list exactly. A row
	// the emitted tree CARRIES the form-2 verb for MUST answer with the bytes —
	// its absence would be the driver dropping a row it can reach, not the leg
	// lacking a feature (test/conformance/README.md, "Exit codes": absence is a
	// missing FEATURE, never a failing test). A row whose tree carries no verb
	// (the form is fixed-class only, docs/SPEC-TABLES.md §3.3) is the one that
	// may be `<case>.absent`.
	_, connections, rows := parseMessageManifest(t)
	tables := driverMessageTablePaths(t)
	answered, absent := 0, 0
	for _, row := range rows {
		conn := connections[row.connection]
		verb := "export function " + row.root + "LoadMessages"
		carries := false
		if data, err := os.ReadFile(filepath.Join(tree, tables[conn.unit+"/"+row.root])); err == nil {
			carries = strings.Contains(string(data), verb)
		}
		path := filepath.Join(out, row.name)
		if _, err := os.Stat(path + ".absent"); err == nil {
			if carries {
				t.Errorf("%s: the emitted tree exports %s, so the driver must answer the bytes, not `<case>.absent`", row.name, verb)
			}
			absent++
			continue
		}
		if !carries {
			t.Errorf("%s: the driver answered bytes for a row whose emitted tree exports no %s", row.name, verb)
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

// driverMessageTablePaths is the driver's messageRows table keyed by
// `<unit>/<root>`, the generated table module each row's form-2 verbs live in
// (test/conformance/js/main.mjs), so an assertion about what the emitted tree
// carries reads the module the driver loads and not a copy of its layout.
func driverMessageTablePaths(t *testing.T) map[string]string {
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
	re := regexp.MustCompile(`\["([A-Za-z0-9_]+)", "([A-Za-z0-9_]+)", "[A-Za-z0-9_./]+", "([A-Za-z0-9_]+/[A-Za-z0-9_]+\.js)"\]`)
	out := map[string]string{}
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		out[m[1]+"/"+m[2]] = m[3]
	}
	if len(out) == 0 {
		t.Fatal("the messageRows table carries no row with a table module (test/conformance/js/main.mjs)")
	}
	return out
}

// writeConformanceTree generates each named unit's whole JavaScript tree into
// `root/<dir>` THE WAY THE SHIPPED `build/tables-generated-js/.stamp` RECIPE
// DOES: by running the schema command, which applies the compiler's own
// refusals BEFORE any emitter runs (compiler/target_javascript.go). Calling the
// emitters directly here BYPASSES those refusals — which is how an earlier cut
// of this gate generated a `backend` unit the shipped target refused and logged
// 12 answers while the shipped tree answered 5: a gate that does not measure the
// shipped leg. A unit the target refuses now FAILS the command and this test —
// the backend gap this card exists to close — and is never skipped in silence.
func writeConformanceTree(t *testing.T, root string, units [][2]string) {
	t.Helper()
	repo, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	for _, u := range units {
		out := filepath.Join(root, u[0])
		if err := os.MkdirAll(out, 0o755); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "run", "./cmd/schema", "generate", "--lang", "js", "--out", out, u[1])
		cmd.Dir = repo
		if data, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("the shipped target refused the `%s` unit its message driver reads: %v\n%s", u[1], err, data)
		}
	}
}
