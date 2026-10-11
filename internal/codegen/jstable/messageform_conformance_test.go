package jstable

// THE MESSAGE FORM'S CONFORMANCE GATE ON THE JAVASCRIPT LEG
// (docs/SPEC-TABLES.md §3.3, test/conformance/README.md). The leg's driver
// answers the `message` surface — AnnounceRead the connection's announcement,
// LoadMessages the message-form batch against it, SaveMessages the bytes — and
// this test pins two things: that the driver's `list` names `message` (so the
// surface is not ABSENT), and that the harness's message rows pass with no
// `<case>.absent`. The second is the codec's own gate: a root this leg has no
// form-2 verbs for answers `<case>.absent`, and the harness prints `+Na`, which
// is the red this card's codec port turns green.

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// conformanceRoot is the repository root, which the driver contract names the
// working directory: every manifest path is repo-relative.
func conformanceRoot(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// buildHarness compiles the conformance harness once, into a temp binary, so
// the message gate runs the same `run` the Makefile's `make conformance` runs.
func buildHarness(t *testing.T, root string) string {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "conformance-harness")
	cmd := exec.Command("go", "build", "-o", bin, "./test/conformance/harness")
	cmd.Dir = root
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the conformance harness: %v\n%s", err, out)
	}
	return bin
}

// ensureGeneratedTree builds the JavaScript generated tree the driver reads its
// accelerators and (once the codec port lands) its message verbs from
// (make/js.mk's build/tables-generated-js/.stamp).
func ensureGeneratedTree(t *testing.T, root string) {
	t.Helper()
	stamp := filepath.Join(root, "build", "tables-generated-js", ".stamp")
	if _, err := os.Stat(stamp); err == nil {
		return
	}
	cmd := exec.Command("make", "build/tables-generated-js/.stamp")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NODE=node")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("building the JavaScript generated tree: %v\n%s", err, out)
	}
}

// TestMessageFormConformanceJavaScript pins the driver's message surface and
// the harness's message rows (test/conformance/README.md's `message` row).
func TestMessageFormConformanceJavaScript(t *testing.T) {
	t.Parallel()

	node := jsNode(t) // skips by name when the leg's toolchain is absent

	root := conformanceRoot(t)
	ensureGeneratedTree(t, root)
	driver := filepath.Join(root, "test", "conformance", "js", "main.mjs")

	// THE DRIVER SOURCE ANSWERS `message` (docs/SPEC-TABLES.md §3.3): the
	// surface is in `list`, and the message rows are answered by a surface
	// function, never by leaving the surface out of `list`.
	src, err := os.ReadFile(driver)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(src), `case "message":`) {
		t.Fatal("the JavaScript driver does not answer the `message` surface")
	}

	// THE DRIVER LISTS `message`, and its row list is the manifest's: run
	// `list` and require the surface by name, which is what keeps the harness
	// from printing ABSENT on the whole surface.
	listed := exec.Command(node, driver, filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"), "list")
	listed.Dir = root
	listOut, err := listed.Output()
	if err != nil {
		t.Fatalf("the JavaScript driver's `list` failed: %v", err)
	}
	hasMessage := false
	for _, line := range strings.Split(strings.TrimSpace(string(listOut)), "\n") {
		if strings.TrimSpace(line) == "message" {
			hasMessage = true
		}
	}
	if !hasMessage {
		t.Fatalf("the JavaScript driver's `list` does not name `message`:\n%s", listOut)
	}

	// THE HARNESS'S MESSAGE ROWS PASS (docs/SPEC-TABLES.md §3.3): run the one
	// leg and read its `message` cell, which must be `pass 13/13` — no absent,
	// no `+Na` — or the codec is answering `<case>.absent` for rows it owes.
	harness := buildHarness(t, root)
	work := t.TempDir()
	run := exec.Command(harness, "run", "--only", "js", "--work", work)
	run.Dir = root
	run.Env = append(os.Environ(), "NODE="+node, "SCHEMA_JS_GENERATED="+filepath.Join(root, "build", "tables-generated-js"))
	out, err := run.CombinedOutput()
	if err != nil {
		t.Fatalf("the conformance harness run failed:\n%s", out)
	}
	cell := messageCell(t, string(out))
	if cell != "pass 13/13" {
		t.Fatalf("the JavaScript leg's message rows are not all answered: cell is %q, want `pass 13/13`\n%s", cell, out)
	}
}

// messageCell reads the `message` row's js column out of the harness matrix.
// The surface column is padded to 14 and the language column to 18, so the
// cell is the text from column 14, trimmed.
func messageCell(t *testing.T, matrix string) string {
	t.Helper()
	for _, line := range strings.Split(matrix, "\n") {
		if !strings.HasPrefix(line, "message") {
			continue
		}
		if len(line) < 14 {
			continue
		}
		if cell := strings.TrimSpace(line[14:]); cell != "" {
			return cell
		}
	}
	t.Fatalf("the matrix prints no `message` row:\n%s", matrix)
	return ""
}
