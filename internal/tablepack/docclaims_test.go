package tablepack_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tablepack"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// A DOC CLAIM is one sentence a page makes about what the reader does with
// old or bad data, bound to an actual read through the engine (review finding
// F-173: the tutorial and the usage page described clamping, last-wins and
// partial outcomes for saves the reader refuses whole).
//
// Each row writes a value under one declaration, reads the bytes under
// another, and states the outcome: the load returned, with these counters, or
// it was refused, for this named reason. The row also quotes the page: `says`
// is text the page must carry, so a sentence cannot be edited away from the
// read that proves it, and the read cannot change under the sentence.
//
// The wire of a row is always one a writer produced. Where the claim is about
// bytes no conforming writer of the READER's declaration emits (map entries
// out of order, a count past the reader's bound), the writer is a twin
// declaration whose bytes are the same shape: an array of key/value pairs for
// a map, a wider bound for a count.
type docClaim struct {
	name   string
	page   string // repository-relative page that makes the claim
	says   string // text the page carries, compared with whitespace folded
	writer string // the declaration the bytes were written under
	value  string // the root's JSON text under the writer's declaration
	reader string // the declaration the bytes are read under; empty is the writer's
	root   string

	refused bool   // the read refused the whole load
	reason  string // the refusal's named reason, when refused
	unknown int    // the report's counters, when the read returned
	widened int
}

// docClaimModel compiles one declaration into the engine's model.
func docClaimModel(t *testing.T, schema string) *tabletext.Model {
	t.Helper()
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "claim.schema"), []byte(schema), 0o600); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	unit, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatalf("the declaration does not compile: %v\n%s", err, schema)
	}
	return tabletext.NewModel(unit)
}

// docClaimWire packs the row's value under the writer's declaration.
func docClaimWire(t *testing.T, c docClaim) []byte {
	t.Helper()
	tree := t.TempDir()
	if err := os.WriteFile(filepath.Join(tree, c.root+".json"), []byte(c.value), 0o600); err != nil {
		t.Fatal(err)
	}
	wire, _, report, err := tablepack.Pack(docClaimModel(t, c.writer), c.root, tree)
	if err != nil {
		t.Fatalf("the writer refused the row's own value: %v", err)
	}
	if report.Refused {
		t.Fatalf("the writer's text read refused the row's own value: %s", report.Reason)
	}
	return wire
}

// docClaimFold folds every run of white space to one space, so a page may
// wrap a sentence anywhere.
func docClaimFold(s string) string { return strings.Join(strings.Fields(s), " ") }

// docClaimPages caches the pages a run reads.
func docClaimPage(t *testing.T, pages map[string]string, rel string) string {
	t.Helper()
	if text, ok := pages[rel]; ok {
		return text
	}
	data, err := os.ReadFile(filepath.Join("..", "..", filepath.FromSlash(rel)))
	if err != nil {
		t.Fatal(err)
	}
	pages[rel] = docClaimFold(string(data))
	return pages[rel]
}

// runDocClaims holds every row to the engine and to its page.
func runDocClaims(t *testing.T, claims []docClaim) {
	t.Helper()
	pages := map[string]string{}
	seen := map[string]bool{}
	for _, c := range claims {
		if seen[c.name] {
			t.Fatalf("two doc claims are named %q", c.name)
		}
		seen[c.name] = true
		page := docClaimPage(t, pages, c.page)
		t.Run(c.name, func(t *testing.T) {
			if !strings.Contains(page, docClaimFold(c.says)) {
				t.Errorf("%s no longer says %q: the sentence this read proves is gone, so move the row with it", c.page, c.says)
			}
			wire := docClaimWire(t, c)
			reader := c.reader
			if reader == "" {
				reader = c.writer
			}
			report, err := tablepack.ReadReport(docClaimModel(t, reader), c.root, wire)
			refused, why := report.Refused, report.Reason
			if err != nil {
				refused, why = true, strings.TrimPrefix(err.Error(), "schema: ")
			}
			switch {
			case refused != c.refused && refused:
				t.Fatalf("%s says the load returns, and the reader refused it: %s", c.page, why)
			case refused != c.refused:
				t.Fatalf("%s says the reader refuses (%s), and the load returned: unknown=%d widened=%d", c.page, c.reason, report.Unknown, report.Widened)
			case refused:
				if name, _, _ := strings.Cut(why, ":"); name != c.reason {
					t.Fatalf("%s names the refusal %s, and the reader named it %q", c.page, c.reason, why)
				}
			default:
				if report.Unknown != c.unknown || report.Widened != c.widened {
					t.Fatalf("%s says unknown=%d widened=%d, and the read counted unknown=%d widened=%d",
						c.page, c.unknown, c.widened, report.Unknown, report.Widened)
				}
			}
		})
	}
}
