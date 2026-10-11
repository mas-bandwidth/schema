package jstable

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
// driver's MESSAGE row list to the manifest's (docs/SPEC-TABLES.md §3.3,
// test/conformance/README.md, "The surfaces").
//
// THE SURFACE, OWED TO §11's CLAIMED SET: "The three suffixes `MeasureMessages`,
// `SaveMessages` and `LoadMessages` in `tableGeneratedVerbs`." The manifest's
// `message <name> <connection> <root> <file-form wire> <message-form wire>`
// rows are the corpus's claim; the driver's `messageRows` table is the leg's,
// and this test is where the two are required to be the same set. A row the
// manifest names and the driver does not is a case that silently reads ABSENT;
// a row the driver names and the manifest does not is a claim with no corpus
// behind it.
func TestMessageFormConformanceJavaScript(t *testing.T) {
	t.Parallel()

	t.Run("row list is the manifest's", func(t *testing.T) {
		t.Parallel()
		want := manifestMessageRoots(t)
		if len(want) == 0 {
			t.Fatal("the manifest names no message row — this gate would pass over the empty set")
		}

		got := driverMessageRoots(t)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("the JavaScript driver's message row list is not the manifest's:\n driver   %v\n manifest %v", got, want)
		}

		// THE SURFACE IS ANNOUNCED. The driver's `list` names the surfaces it
		// implements, and a row resolved per case instead of per surface is
		// said the finer-grained way `<case>.absent` (test/conformance/
		// README.md). `message` must be among the listed surfaces, or every
		// row above is an absence the matrix cannot distinguish from a
		// feature the leg lacks.
		const listed = `cook\ncook-foreign\nblock\nblock-foreign\nblock-dump\nforgery\ncook-forgery\nmessage\n`
		source := driverSource(t)
		if !strings.Contains(source, listed) {
			t.Error("the JavaScript driver does not list `message` among its surfaces (test/conformance/README.md, \"The surfaces\")")
		}

		// THE DRIVER SOURCE ANSWERS A ROW, rather than declaring the surface
		// and refusing every case. The surface's one answer path writes the
		// bytes `SaveMessages` produced (`writeFileSync(join(outDir, f[1]),
		// bytes)`); `<case>.absent` is only the fallback for a row this leg
		// cannot reach. A driver whose handler carried no byte-writing path at
		// all would make the row list above a claim nothing could ever satisfy
		// (test/conformance/README.md, "Exit codes").
		start := strings.Index(source, "async function surfaceMessage")
		if start < 0 {
			t.Fatal("the JavaScript driver declares no surfaceMessage handler (test/conformance/js/main.mjs)")
		}
		block := surfaceBlock(source[start:])
		if !strings.Contains(block, "writeFileSync(join(outDir, f[1]), bytes)") {
			t.Error("the JavaScript driver's message handler has no path that writes the saved bytes: every row could only ever answer ABSENT")
		}
	})

	// THE HARNESS ROWS PASS WHEN THE LEG'S TOOLCHAIN IS PRESENT, skipping by
	// name when it is not. The driver is RUN, not read: it answers the surface
	// over the manifest's own rows, and only a leg whose generated tree
	// carries the form-2 verbs has answers to compare. When node or the
	// generated tree is absent the clause SKIPS BY NAME rather than passing
	// silently; when the tree IS present and no row answered, the clause FAILS
	// rather than skipping, because a built tree that answers nothing is the
	// missing codec and not a missing toolchain.
	t.Run("harness message rows", func(t *testing.T) {
		t.Parallel()
		assertDriverAnswersMessages(t)
	})
}

// manifestMessageRoots is the manifest's distinct (unit, root) pairs, read the
// way test/conformance/README.md reads a derived manifest: one row per
// `message` line, its unit taken from the `connection` line it names.
func manifestMessageRoots(t *testing.T) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	unitOf := map[string]string{}
	seen := map[string]bool{}
	var roots []string
	for _, raw := range strings.Split(string(data), "\n") {
		line := strings.TrimSpace(raw)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Fields(line)
		switch f[0] {
		case "connection":
			unitOf[f[1]] = f[2]
		case "message":
			unit, ok := unitOf[f[2]]
			if !ok {
				t.Fatalf("%s: message row names connection %q before the connection line", f[1], f[2])
			}
			key := unit + "/" + f[3]
			if !seen[key] {
				seen[key] = true
				roots = append(roots, key)
			}
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

// assertDriverAnswersMessages RUNS the driver's message surface over the
// committed manifest and requires a file per row — the bytes when the leg's
// generated tree carries the form-2 verbs, and `<case>.absent` when it does
// not (test/conformance/README.md, "Exit codes"). It SKIPS BY NAME when node
// is not on PATH or the generated tree is not built; when the tree IS built and
// no case carried bytes it FAILS, because that is the missing codec and not a
// missing toolchain.
func assertDriverAnswersMessages(t *testing.T) {
	t.Helper()
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("node is not on PATH: the JavaScript leg's toolchain is not present")
	}

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// The driver imports the generated tree at module load (test/conformance/
	// js/main.mjs), so without it there is nothing to run. The tree is the
	// JavaScript leg's toolchain: absent, the clause SKIPS BY NAME.
	if _, err := os.Stat(filepath.Join(root, "build", "tables-generated-js", "block", "RenderBlock.js")); err != nil {
		t.Skip("build/tables-generated-js is not built: the JavaScript leg's generated tree is not present")
	}
	manifest := filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt")
	out := t.TempDir()

	cmd := exec.Command(node, filepath.Join(root, "test", "conformance", "js", "main.mjs"), manifest, "message", out)
	cmd.Dir = root
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("the driver's message surface failed: %v\n%s", err, data)
	}

	entries, err := os.ReadDir(out)
	if err != nil {
		t.Fatal(err)
	}
	answered, absent := 0, 0
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".absent") {
			absent++
		} else {
			answered++
		}
	}
	switch {
	case answered == 0 && absent == 0:
		t.Fatalf("the driver wrote no file for any message row")
	case answered == 0:
		t.Fatalf("the JavaScript leg's generated tree carries no form-2 verbs: the driver answered %d row(s) absent", absent)
	}
	t.Logf("the driver answered %d message row(s), %d absent", answered, absent)
}
