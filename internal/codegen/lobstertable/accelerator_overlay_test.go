package lobstertable_test

import (
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The accelerator overlay records the five descriptor and accelerator rows the
// Lobster lane accepts (docs/roadmap-evidence/lobster-accelerators.sexp).
// every :done row names tests that exist, and the cook-write row names the
// unbounded-text extent test from the consolidated JSON and cook card.
func TestLobsterAcceleratorOverlayNamesEveryClause(t *testing.T) {
	t.Parallel()
	const overlayPath = "docs/roadmap-evidence/lobster-accelerators.sexp"
	data, err := os.ReadFile(filepath.Join(root, overlayPath))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	required := []string{
		"port/lobster/vt/block-read",
		"port/lobster/vt/block-build",
		"port/lobster/vt/build-version",
		"port/lobster/vt/cook-open",
		"port/lobster/vt/cook-write",
	}
	for _, id := range required {
		if !strings.Contains(src, `(:id "`+id+`"`) {
			t.Errorf("overlay lacks the %s row", id)
		}
	}
	existing := existingTestFunctions(t)
	rows := parseOverlayRows(t, src)
	if len(rows) == 0 {
		t.Fatal("overlay names no rows")
	}
	cookWriteTests := map[string]bool{}
	for _, row := range rows {
		if !row.done {
			continue
		}
		for _, ev := range row.evidence {
			for _, name := range testNamesFromEvidence(ev) {
				if !existing[name] {
					t.Errorf("%s: evidence names %q, which is not a test in the tree", row.id, name)
				}
				if row.id == "port/lobster/vt/cook-write" {
					cookWriteTests[name] = true
				}
			}
		}
	}
	if !cookWriteTests["TestLobsterCook"] {
		t.Error("cook-write does not name the unbounded-text extent test TestLobsterCook, the row that holds feature_full's unbounded text")
	}
}

type overlayRow struct {
	id       string
	done     bool
	evidence []string
}

var overlayRowRe = regexp.MustCompile(`\(:id "([^"]+)"`)

func parseOverlayRows(t *testing.T, src string) []overlayRow {
	t.Helper()
	var rows []overlayRow
	idxs := overlayRowRe.FindAllStringIndex(src, -1)
	for k, m := range idxs {
		end := len(src)
		if k+1 < len(idxs) {
			end = idxs[k+1][0]
		}
		seg := src[m[0]:end]
		row := overlayRow{
			id:   overlayRowRe.FindStringSubmatch(seg)[1],
			done: strings.Contains(seg, ":state :done"),
		}
		if ei := strings.Index(seg, ":evidence ("); ei >= 0 {
			body := seg[ei+len(":evidence ("):]
			if ci := strings.IndexByte(body, ')'); ci >= 0 {
				body = body[:ci]
			}
			for _, mm := range quotedStringRe.FindAllStringSubmatch(body, -1) {
				row.evidence = append(row.evidence, mm[1])
			}
		}
		rows = append(rows, row)
	}
	return rows
}

var quotedStringRe = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
var testTokenRe = regexp.MustCompile(`Test[A-Za-z0-9_]+`)

func testNamesFromEvidence(ev string) []string {
	i := strings.Index(ev, ".go:")
	if i < 0 {
		return nil
	}
	var out []string
	for _, tok := range testTokenRe.FindAllString(ev[i+4:], -1) {
		if j := strings.IndexByte(tok, '/'); j >= 0 {
			tok = tok[:j]
		}
		out = append(out, tok)
	}
	return out
}

var testDeclRe = regexp.MustCompile(`(?m)^func (Test[A-Za-z0-9_]+)\(`)

func existingTestFunctions(t *testing.T) map[string]bool {
	t.Helper()
	got := map[string]bool{}
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") {
			return nil
		}
		b, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		for _, m := range testDeclRe.FindAllSubmatch(b, -1) {
			got[string(m[1])] = true
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return got
}
