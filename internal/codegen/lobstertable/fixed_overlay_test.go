package lobstertable

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
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

// The eight rows the shared proof suites do not reach, landed on the strict
// read after the one-read cut (LOB-2). Each is held by its own top-level test
// in fixed_evolution_rows_test.go, TestLobsterFixedRow<Row>, which carries
// the row's negative controls inline, so its evidence names that test rather
// than a proof-suite subtest.
var lobsterFixedCutRows = map[string]string{
	"numeric-evolution": "TestLobsterFixedRowNumericEvolution",
	"reports":           "TestLobsterFixedRowReports",
	"array-bounds":      "TestLobsterFixedRowArrayBounds",
	"text":              "TestLobsterFixedRowText",
	"scalar-bounds":     "TestLobsterFixedRowScalarBounds",
	"ordinals":          "TestLobsterFixedRowOrdinals",
	"normalization":     "TestLobsterFixedRowNormalization",
	"hostile-input":     "TestLobsterFixedRowHostileInput",
}

var (
	overlayIDRx    = regexp.MustCompile(`^\(:id\s+"([^"]+)"`)
	overlayStateRx = regexp.MustCompile(`:state\s+:([A-Za-z0-9_-]+)`)
	goTestFuncRx   = regexp.MustCompile(`(?m)^func\s+(Test[A-Za-z0-9_]+)\s*\(`)
)

func fixedOverlayPath(t *testing.T) (root, path string) {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir, filepath.Join(dir, "docs", "roadmap-evidence", "lobster-fixed.sexp")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("repo root not found")
		}
		dir = parent
	}
}

// TestLobsterFixedOverlayNamesEveryRow parses docs/roadmap-evidence/lobster-fixed.sexp
// and fails unless each of the 57 port/lobster/fixed/<row> ids appears exactly
// once, nothing else appears, and every :done row names a test that exists in
// the package: a proof-suite row under that row's own subtest, one of the
// eight strict-read rows by its own TestLobsterFixedRow<Row>.
func TestLobsterFixedOverlayNamesEveryRow(t *testing.T) {
	t.Parallel()

	root, path := fixedOverlayPath(t)
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	counts := map[string]int{}
	states := map[string]string{}
	lines := map[string]string{}
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
		lines[id] = trimmed
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
	for row := range lobsterFixedCutRows {
		id := "port/lobster/fixed/" + row
		if counts[id] != 1 {
			t.Errorf("%s: expected exactly one overlay row, got %d", id, counts[id])
		}
	}
	for id, count := range counts {
		row := strings.TrimPrefix(id, "port/lobster/fixed/")
		if _, cut := lobsterFixedCutRows[row]; !cut && !slices.Contains(lobsterFixedRows, row) {
			t.Errorf("%s: not a lobster fixed row", id)
		}
		if count != 1 {
			t.Errorf("%s: expected exactly one overlay row, got %d", id, count)
		}
	}

	for id, state := range states {
		if state != "done" {
			continue
		}
		row := strings.TrimPrefix(id, "port/lobster/fixed/")
		evidence := overlayEvidence(lines[id])
		if len(evidence) == 0 {
			t.Errorf("%s: :done row has no :evidence strings", id)
			continue
		}
		for _, ev := range evidence {
			filePath, testsPart, ok := strings.Cut(ev, ": ")
			if !ok {
				t.Errorf("%s: evidence %q is not '<file>: <TestName>'", id, ev)
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
			if own, cut := lobsterFixedCutRows[row]; cut {
				// "<file>: TestLobsterFixedRow<Row> (what it holds)"
				name, _, _ := strings.Cut(testsPart, " (")
				if name != own {
					t.Errorf("%s: evidence %q does not name %s", id, ev, own)
				} else if !defined[own] {
					t.Errorf("%s: Go test %s not found in %s", id, own, filePath)
				}
				continue
			}
			for token := range strings.SplitSeq(testsPart, ",") {
				name := strings.TrimSpace(token)
				base, sub, hasSub := strings.Cut(name, "/")
				if !defined[base] {
					t.Errorf("%s: Go test %s not found in %s", id, base, filePath)
					continue
				}
				if !hasSub {
					t.Errorf("%s: evidence %q does not name the %s subtest", id, name, row)
					continue
				}
				if sub != row && !strings.HasPrefix(sub, row+"/") {
					t.Errorf("%s: evidence %q does not name the %s subtest", id, name, row)
				}
			}
		}
	}
}

// overlayEvidence is the quoted strings of a row's :evidence list. It reads
// the list string by string, so a ')' inside an evidence string (a
// parenthetical describing what the test holds) does not end the list.
func overlayEvidence(line string) []string {
	_, rest, ok := strings.Cut(line, ":evidence (")
	if !ok {
		return nil
	}
	var out []string
	for {
		rest = strings.TrimLeft(rest, " \t")
		if !strings.HasPrefix(rest, `"`) {
			return out
		}
		end := strings.IndexByte(rest[1:], '"')
		if end < 0 {
			return out
		}
		out = append(out, rest[1:1+end])
		rest = rest[2+end:]
	}
}
