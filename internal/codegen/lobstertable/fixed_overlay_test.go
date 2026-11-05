package lobstertable

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var lobsterFixedRows = []string{
	"file-envelope",
	"batch-capacity",
	"plan-selection",
	"compiled-plans",
	"definition-hash",
	"fixed-closure",
	"retirement",
	"retired-runtime",
	"optional-values",
	"field-evolution",
	"live-extents",
	"writing",
	"union-guards",
	"plan-execution",
	"interoperability",
	"bool-values",
	"signed-integers",
	"unsigned-integers",
	"signed-128",
	"unsigned-128",
	"ranged-integers",
	"bits-values",
	"float32-values",
	"float64-values",
	"compressed-floats",
	"signed-fixed-point",
	"unsigned-fixed-point",
	"flags-values",
	"enum-values",
	"utf8-values",
	"utf16-values",
	"byte-buffers",
	"nested-types",
	"nested-fixed-tables",
	"fixed-arrays",
	"counted-arrays",
	"keyed-arrays",
	"nested-keyed-arrays",
	"union-values",
	"union-field-arms",
	"payload-free-arms",
	"union-arrays",
	"optional-scalars",
	"optional-nesting",
	"optional-arrays",
	"scalar-defaults",
	"text-bytes-flags-defaults",
	"fixed-file-roundtrip",
	"record-measurement",
}

// The eight rows gated on the one-read cut (the fixed rows that carry a
// numeric-evolution, reports, array-bounds, text, scalar-bounds, ordinals,
// normalization or hostile-input clause) are left out of the fixed overlay.
var lobsterFixedCutRows = []string{
	"numeric-evolution",
	"reports",
	"array-bounds",
	"text",
	"scalar-bounds",
	"ordinals",
	"normalization",
	"hostile-input",
}

var (
	overlayIDRx    = regexp.MustCompile(`^\(:id\s+"([^"]+)"`)
	overlayStateRx = regexp.MustCompile(`:state\s+:([A-Za-z0-9_-]+)`)
	overlayEvRx    = regexp.MustCompile(`:evidence\s+\(([^)]*)\)`)
	goTestFuncRx    = regexp.MustCompile(`(?m)^func\s+(Test[A-Za-z0-9_]+)\s*\(`)
)

func fixedOverlayPath(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "docs", "roadmap-evidence", "lobster-fixed.sexp")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

// TestLobsterFixedOverlayNamesEveryRow parses docs/roadmap-evidence/lobster-fixed.sexp
// and fails unless each of the 49 port/lobster/fixed/<row> ids appears exactly
// once, no row of the eight cut-gated ones appears, and every :done row names
// a test that exists in this package.
func TestLobsterFixedOverlayNamesEveryRow(t *testing.T) {
	t.Parallel()

	path := fixedOverlayPath(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	counts := map[string]int{}
	states := map[string]string{}
	var lines []string
	for line := range strings.SplitSeq(string(b), "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" || strings.HasPrefix(trimmed, ";") {
			continue
		}
		m := overlayIDRx.FindStringSubmatch(trimmed)
		if m == nil {
			t.Fatalf("malformed overlay line: %q", trimmed)
		}
		id := m[1]
		counts[id]++
		lines = append(lines, trimmed)
		sm := overlayStateRx.FindStringSubmatch(trimmed)
		if sm == nil {
			t.Fatalf("%s: missing :state", id)
		}
		states[id] = sm[1]
	}

	for _, row := range lobsterFixedRows {
		id := "port/lobster/fixed/" + row
		if counts[id] != 1 {
			t.Errorf("%s: expected exactly one overlay row, got %d", id, counts[id])
		}
	}
	for _, row := range lobsterFixedCutRows {
		id := "port/lobster/fixed/" + row
		if counts[id] != 0 {
			t.Errorf("%s: cut-gated row must not appear in the fixed overlay, got %d", id, counts[id])
		}
	}
	for id, count := range counts {
		if count != 1 {
			t.Errorf("%s: expected exactly one overlay row, got %d", id, count)
		}
	}

	root := filepath.Dir(filepath.Dir(path))
	for id, state := range states {
		if state != "done" {
			continue
		}
		var line string
		for _, l := range lines {
			if strings.Contains(l, id) {
				line = l
				break
			}
		}
		ev := overlayEvRx.FindStringSubmatch(line)
		if len(ev) < 2 || strings.TrimSpace(ev[1]) == "" {
			t.Errorf("%s: :done row has no :evidence strings", id)
			continue
		}
		filePath, testsPart, ok := strings.Cut(strings.TrimSpace(ev[1]), ": ")
		if !ok {
			t.Errorf("%s: evidence %q is not '<file>: <TestName>'", id, ev[1])
			continue
		}
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(filePath)))
		if err != nil {
			t.Errorf("%s: read cited file %s: %v", id, filePath, err)
			continue
		}
		defined := map[string]bool{}
		for _, mm := range goTestFuncRx.FindAllStringSubmatch(string(src), -1) {
			defined[mm[1]] = true
		}
		for _, token := range strings.Split(testsPart, ",") {
			name := strings.TrimSpace(token)
			if slash := strings.Index(name, "/"); slash >= 0 {
				name = name[:slash]
			}
			if !defined[name] {
				t.Errorf("%s: Go test %s not found in %s", id, name, filePath)
			}
		}
	}
}
