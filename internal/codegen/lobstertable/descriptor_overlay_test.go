package lobstertable_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The descriptor overlay records the three descriptor-surface rows the Lobster
// lane accepts (docs/roadmap-evidence/lobster-descriptors.sexp). Every row names
// tests that exist, in the files that declare them, and carries a tested
// revision. The minimum int64 literal and the packet-union descriptors are
// named by the tests that hold them.
func TestLobsterDescriptorOverlayNamesEveryClause(t *testing.T) {
	t.Parallel()
	const overlayPath = "docs/roadmap-evidence/lobster-descriptors.sexp"
	data, err := os.ReadFile(filepath.Join(root, overlayPath))
	if err != nil {
		t.Fatal(err)
	}
	src := string(data)
	required := []string{
		"port/lobster/vt/reflection",
		"port/lobster/vt/doc-tags",
		"port/lobster/vt/unit-registry",
	}
	for _, id := range required {
		if !strings.Contains(src, `(:id "`+id+`"`) {
			t.Errorf("overlay lacks the %s row", id)
		}
	}
	byFile, existing := testFunctionsByFile(t)
	rows := parseOverlayRows(t, src)
	if len(rows) == 0 {
		t.Fatal("overlay names no rows")
	}
	revisionRe := regexp.MustCompile(`:tested-revision "[0-9a-f]{40}"`)
	rowNames := map[string]map[string]string{}
	rowSegments := splitOverlayRows(src)
	for _, row := range rows {
		if !revisionRe.MatchString(rowSegments[row.id]) {
			t.Errorf("%s: row carries no tested-revision", row.id)
		}
		if len(row.evidence) == 0 {
			t.Errorf("%s: row names no evidence", row.id)
		}
		rowNames[row.id] = map[string]string{}
		for _, ev := range row.evidence {
			file := evidenceFile(ev)
			names := testNamesFromEvidence(ev)
			if len(names) == 0 {
				t.Errorf("%s: evidence %q names no test", row.id, ev)
			}
			for _, name := range names {
				if file == "" {
					t.Errorf("%s: evidence %q names tests but no .go file", row.id, ev)
					continue
				}
				if !byFile[file][name] {
					if existing[name] {
						t.Errorf("%s: evidence names %q in %s, but it is declared in another file", row.id, name, file)
					} else {
						t.Errorf("%s: evidence names %q, which is not a test in the tree", row.id, name)
					}
				}
				rowNames[row.id][name] = file
			}
		}
	}
	const dir = "internal/codegen/lobstertable/"
	want := map[string]map[string]string{
		"port/lobster/vt/reflection": {
			"TestLobsterDescriptors":                   dir + "descriptors_test.go",
			"TestLobsterDescriptorsNegativeControl":    dir + "descriptors_test.go",
			"TestLobsterUnitViewGeneratedNames":        dir + "reflection_names_test.go",
			"TestLobsterUnitViewVocabularyMemberNames": dir + "reflection_names_test.go",
		},
		"port/lobster/vt/doc-tags": {
			"TestLobsterDescriptors": dir + "descriptors_test.go",
		},
		"port/lobster/vt/unit-registry": {
			"TestLobsterUnitViewIncludesUnreachedFiles": dir + "unit_view_test.go",
			"TestLobsterUnitViewCorpus":                 dir + "unit_view_test.go",
			"TestLobsterUnitViewListing":                dir + "view_listing_test.go",
			"TestLobsterUnitViewVocabulary":             dir + "unit_view_vocabulary_test.go",
			"TestLobsterUnitViewGeneratedNames":         dir + "reflection_names_test.go",
		},
	}
	for id, names := range want {
		for name, file := range names {
			if rowNames[id][name] != file {
				t.Errorf("%s does not name %s in %s", id, name, file)
			}
		}
	}
	// The minimum int64 literal is held by the unit-registry test that reads a
	// constant back as 0x8000000000000000.
	unitView, err := os.ReadFile(filepath.Join(root, dir+"unit_view_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, lit := range []string{"-9223372036854775808", "0x8000000000000000"} {
		if !strings.Contains(string(unitView), lit) {
			t.Errorf("unit_view_test.go no longer holds the minimum int64 literal %s", lit)
		}
	}
	// The packet-union descriptors: a union with a packet arm is listed by the
	// unit view, and its generated descriptor name is guarded.
	for file, lits := range map[string][]string{
		dir + "unit_view_vocabulary_test.go": {"union Choice { one Payload }"},
		dir + "reflection_names_test.go":     {"union Bar { one Payload }"},
	} {
		b, err := os.ReadFile(filepath.Join(root, file))
		if err != nil {
			t.Fatal(err)
		}
		for _, lit := range lits {
			if !strings.Contains(string(b), lit) {
				t.Errorf("%s no longer holds the packet-union case %q", file, lit)
			}
		}
	}
}

func splitOverlayRows(src string) map[string]string {
	out := map[string]string{}
	idxs := overlayRowRe.FindAllStringIndex(src, -1)
	for k, m := range idxs {
		end := len(src)
		if k+1 < len(idxs) {
			end = idxs[k+1][0]
		}
		seg := src[m[0]:end]
		out[overlayRowRe.FindStringSubmatch(seg)[1]] = seg
	}
	return out
}
