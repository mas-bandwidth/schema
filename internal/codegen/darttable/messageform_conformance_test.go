package darttable

// THE MESSAGE FORM ON THE DART LEG, THROUGH THE CONFORMANCE DRIVER
// (docs/SPEC-TABLES.md §3.3, test/conformance/README.md, "The surfaces").
//
// The `message` surface reads TWO files — "AnnounceRead the connection's
// announcement, LoadMessages the message-form batch against it, SaveMessages,
// the bytes" — and the driver's row list is the leg's claim about which rows it
// can answer. This test holds that claim to the manifest's `message` rows: a
// row the manifest names and the driver does not is a case that silently reads
// ABSENT, and the reverse is a claim with no corpus behind it. The driver's
// `list` must name `message`, or every row would be an absence the matrix
// cannot tell from a feature the leg lacks.
//
// The manifest's message rows are read the way the harness reads them: a
// `message <name> <connection> <root> <form-1 wire> <message wire>` row, its
// unit taken from the `connection` line it names.

import (
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

func TestMessageFormConformanceDart(t *testing.T) {
	t.Parallel()

	t.Run("row list is the manifest's", func(t *testing.T) {
		t.Parallel()

		want := dartManifestMessageRoots(t)
		if len(want) == 0 {
			t.Fatal("the manifest names no message row — this gate would pass over the empty set")
		}
		got := dartDriverMessageRoots(t)
		if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
			t.Fatalf("the Dart driver's message row list is not the manifest's:\n driver   %v\n manifest %v", got, want)
		}

		// The driver's `list` announces `message`: a surface the driver answers
		// but does not list is ABSENT to the harness (README, "Exit codes").
		if !strings.Contains(dartDriverSource(t), `cook-forgery\nmessage\n`) {
			t.Error("the Dart driver's `list` does not name `message` (test/conformance/README.md, \"The surfaces\")")
		}
	})

	// THE HARNESS ROWS PASS WHEN THE LEG IS BUILT, skipping by name when it is
	// not. The driver is RUN, not read: the harness drives its `message`
	// surface over the manifest's own rows and holds the answers to the pins.
	// Rows this leg's fixed-class form cannot reach — the `backend` unit's
	// table-arm union does not compile and the pointered `Scene` is not a fixed
	// table — are counted as `<case>.absent`, which the matrix prints beside
	// the pass.
	t.Run("harness message rows", func(t *testing.T) {
		t.Parallel()
		dartAssertHarnessMessageRows(t)
	})
}

// dartDriverSource is the conformance driver's source, the row list it declares.
func dartDriverSource(t *testing.T) string {
	t.Helper()
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "conformance", "dart", "main.dart"))
	if err != nil {
		t.Fatal(err)
	}
	return string(src)
}

// dartManifestMessageRoots is the manifest's distinct (unit, root) pairs, one
// per `message` row, its unit from the `connection` line it names.
func dartManifestMessageRoots(t *testing.T) []string {
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

// dartDriverMessageRoots is the driver's own row list, the `messageRows` table
// in test/conformance/dart/main.dart: one `['<unit>', '<root>']` line a row.
func dartDriverMessageRoots(t *testing.T) []string {
	t.Helper()
	source := dartDriverSource(t)
	start := strings.Index(source, "final List<List<String>> messageRows = const [")
	if start < 0 {
		t.Fatal("the Dart driver declares no messageRows table (test/conformance/dart/main.dart)")
	}
	end := strings.Index(source[start:], "];")
	if end < 0 {
		t.Fatal("the messageRows table is not closed: no `];` follows its declaration")
	}
	block := source[start : start+end]
	re := regexp.MustCompile(`\['([A-Za-z0-9_]+)', '([A-Za-z0-9_]+)'\]`)
	var roots []string
	for _, m := range re.FindAllStringSubmatch(block, -1) {
		roots = append(roots, m[1]+"/"+m[2])
	}
	sort.Strings(roots)
	return roots
}

// dartAssertHarnessMessageRows runs the harness's `message` surface over the
// Dart leg and requires it to answer, not read `absent`. The leg's driver is
// AOT-compiled by `make conformance-dart`; when the executable or the Dart
// toolchain is absent the clause SKIPS BY NAME rather than passing silently.
func dartAssertHarnessMessageRows(t *testing.T) {
	t.Helper()
	if _, err := os.Stat(filepath.Join("..", "..", "..", "build", "conformance-dart")); err != nil {
		t.Skip("not run: build/conformance-dart is not built (make conformance-dart); the Dart conformance tree does not generate the message units either (make/dart.mk, outside this card)")
	}
	if dartBinary(t) == "" {
		t.Skip("not run: no Dart toolchain")
	}
	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	work := filepath.Join(t.TempDir(), "conformance")
	cmd := exec.Command("go", "run", "./test/conformance/harness", "run", "--only", "dart", "--work", work)
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "NOVA_TEST_NO_HOST=1")
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("harness run failed: %v\n%s", err, out)
	}
	cell, ok := dartMessageCell(string(out))
	if !ok {
		t.Fatalf("the harness printed no `message` row for the dart leg:\n%s", out)
	}
	if cell == "absent" || strings.HasPrefix(cell, "FAIL") {
		t.Fatalf("the dart leg's `message` surface is %q, not a pass (docs/SPEC-TABLES.md §3.3)", cell)
	}
	if !strings.HasPrefix(cell, "pass ") {
		t.Fatalf("the dart leg's `message` surface reads %q, not a pass (docs/SPEC-TABLES.md §3.3)", cell)
	}
}

// dartMessageCell returns the dart column's cell on the harness matrix's
// `message` row. The matrix prints surfaces one per line and one column per
// language, so the cell is the first non-empty field after the surface name.
func dartMessageCell(matrix string) (string, bool) {
	for _, line := range strings.Split(matrix, "\n") {
		if !strings.HasPrefix(line, "message ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return "", false
		}
		return strings.Join(fields[1:], " "), true
	}
	return "", false
}
