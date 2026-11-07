package lobstertable_test

import (
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
)

// The three descriptor rows have one verdict under both contracts. Each id in
// docs/roadmap-evidence/lobster-descriptors.sexp names its clauses by tests
// that exist and carries a tested-revision. The minimum int64 literal and the
// packet-union descriptors are the names descriptors.go actually contains.
func TestLobsterDescriptorOverlayNamesEveryClause(t *testing.T) {
	t.Parallel()
	const overlayPath = "docs/roadmap-evidence/lobster-descriptors.sexp"
	data, err := os.ReadFile(filepath.Join(root, overlayPath))
	if err != nil {
		t.Fatal(err)
	}
	rows := parseDescriptorOverlay(t, string(data))
	required := map[string][]string{
		"port/lobster/vt/reflection": {
			"internal/codegen/lobstertable/descriptors_test.go: TestLobsterDescriptors",
			"internal/codegen/lobstertable/descriptors_test.go: TestLobsterDescriptorsNegativeControl",
			"internal/codegen/lobstertable/reflection_names_test.go: TestLobsterUnitViewGeneratedNames",
			"internal/codegen/lobstertable/reflection_names_test.go: TestLobsterUnitViewVocabularyMemberNames",
		},
		"port/lobster/vt/doc-tags": {
			"internal/codegen/lobstertable/descriptors_test.go: TestLobsterDescriptors",
		},
		"port/lobster/vt/unit-registry": {
			"internal/codegen/lobstertable/unit_view_test.go: TestLobsterUnitViewIncludesUnreachedFiles",
			"internal/codegen/lobstertable/unit_view_test.go: TestLobsterUnitViewCorpus",
			"internal/codegen/lobstertable/unit_view_test.go: TestLobsterUnitViewRuntimePackages",
			"internal/codegen/lobstertable/unit_view_test.go: TestLobsterUnitViewTableFreeRuntimePackages",
			"internal/codegen/lobstertable/view_listing_test.go: TestLobsterUnitViewListing",
			"internal/codegen/lobstertable/unit_view_vocabulary_test.go: TestLobsterUnitViewVocabulary",
			"internal/codegen/lobstertable/reflection_names_test.go: TestLobsterUnitViewGeneratedNames",
			"internal/codegen/lobstertable/reflection_names_test.go: TestLobsterUnitViewVocabularyMemberNames",
			"internal/codegen/lobstertable/runtime_package_names_test.go: TestLobsterRuntimePackageBindingScan",
			"internal/codegen/lobstertable/runtime_package_names_test.go: TestLobsterRuntimePackageBindingRefusal",
			"internal/codegen/lobstertable/unit_view_zero_cost_test.go: TestLobsterTableFreePacketIdentity",
			"internal/codegen/lobstertable/unit_view_zero_cost_test.go: TestLobsterAddingTableMovesNoPacketByte",
		},
	}
	var verdict string
	for id, clauses := range required {
		row, ok := rows[id]
		if !ok {
			t.Errorf("overlay lacks the %s row", id)
			continue
		}
		if row.count != 1 {
			t.Errorf("%s: expected one row, got %d", id, row.count)
		}
		if verdict == "" {
			verdict = row.state
		} else if row.state != verdict {
			t.Errorf("%s: state %s differs from %s; both contracts share one verdict", id, row.state, verdict)
		}
		if row.state != "done" {
			t.Errorf("%s: state is %s, want done", id, row.state)
		}
		if !fortyHex(row.revision) {
			t.Errorf("%s: tested-revision %q is not a forty-hex sha", id, row.revision)
		}
		have := map[string]bool{}
		for _, ev := range row.evidence {
			file, testsPart, ok := strings.Cut(ev, ": ")
			if !ok || !strings.HasSuffix(file, ".go") {
				t.Errorf("%s: evidence %q does not name a Go test", id, ev)
				continue
			}
			src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(file)))
			if err != nil {
				t.Errorf("%s: evidence file %s: %v", id, file, err)
				continue
			}
			defined := testFuncs(src)
			for _, token := range strings.Split(testsPart, ",") {
				name := strings.TrimSpace(token)
				base, _, _ := strings.Cut(name, "/")
				if base == "" || !defined[base] {
					t.Errorf("%s: evidence names %q, which is not a test in %s", id, base, file)
				}
			}
			have[ev] = true
		}
		for _, clause := range clauses {
			if !have[clause] {
				t.Errorf("%s: does not name %s", id, clause)
			}
		}
	}
	if len(rows) != len(required) {
		for id := range rows {
			if _, ok := required[id]; !ok {
				t.Errorf("overlay names %s, which is not one of the three descriptor rows", id)
			}
		}
	}
	docTags, err := os.ReadFile(filepath.Join(root, "internal/codegen/lobstertable/descriptors_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, clause := range []string{"typedoc", "typedtags", "tabledoc", "tabletags", "fielddoc", "fieldtags"} {
		if !strings.Contains(string(docTags), `"`+clause+`"`) {
			t.Errorf("doc-tags clause %s is not a name in descriptors_test.go", clause)
		}
	}
	assertDescriptorFixes(t)
}

type descriptorRow struct {
	count    int
	state    string
	revision string
	evidence []string
}

var (
	descriptorIDRx       = regexp.MustCompile(`\(:id "([^"]+)"`)
	descriptorStateRx    = regexp.MustCompile(`:state\s+:([A-Za-z0-9_-]+)`)
	descriptorRevisionRx = regexp.MustCompile(`:tested-revision\s+"([^"]*)"`)
	descriptorQuotedRx   = regexp.MustCompile(`"((?:[^"\\]|\\.)*)"`)
)

func parseDescriptorOverlay(t *testing.T, src string) map[string]*descriptorRow {
	t.Helper()
	rows := map[string]*descriptorRow{}
	idxs := descriptorIDRx.FindAllStringSubmatchIndex(src, -1)
	if len(idxs) == 0 {
		t.Fatal("overlay names no rows")
	}
	for k, m := range idxs {
		end := len(src)
		if k+1 < len(idxs) {
			end = idxs[k+1][0]
		}
		seg := src[m[0]:end]
		id := src[m[2]:m[3]]
		row := rows[id]
		if row == nil {
			row = &descriptorRow{}
			rows[id] = row
		}
		row.count++
		if sm := descriptorStateRx.FindStringSubmatch(seg); sm != nil {
			row.state = sm[1]
		}
		if sm := descriptorRevisionRx.FindStringSubmatch(seg); sm != nil {
			row.revision = sm[1]
		}
		if ei := strings.Index(seg, ":evidence ("); ei >= 0 {
			body := seg[ei+len(":evidence ("):]
			if ci := strings.IndexByte(body, ')'); ci >= 0 {
				body = body[:ci]
			}
			for _, mm := range descriptorQuotedRx.FindAllStringSubmatch(body, -1) {
				row.evidence = append(row.evidence, mm[1])
			}
		}
	}
	return rows
}

func testFuncs(src []byte) map[string]bool {
	out := map[string]bool{}
	for _, m := range testDeclRe.FindAllSubmatch(src, -1) {
		out[string(m[1])] = true
	}
	return out
}

func fortyHex(s string) bool {
	if len(s) != 40 {
		return false
	}
	for _, r := range s {
		switch {
		case r >= '0' && r <= '9':
		case r >= 'a' && r <= 'f':
		default:
			return false
		}
	}
	return true
}

// assertDescriptorFixes reads descriptors.go. constInt spells every integer
// through lobster.IntLit, whose minimum int64 literal is the hex bit pattern
// (the decimal minimum is refused). A packet union is a *ir.Union that is not
// a table union, and it is registered by emitUnionView as view_union_arm.
func assertDescriptorFixes(t *testing.T) {
	t.Helper()
	srcb, err := os.ReadFile(filepath.Join(root, "internal/codegen/lobstertable/descriptors.go"))
	if err != nil {
		t.Fatal(err)
	}
	src := string(srcb)
	constInt := regexp.MustCompile(`(?s)func constInt\(c \*ir\.Const\) string \{.*?\n\}`)
	body := constInt.FindString(src)
	if body == "" || !strings.Contains(body, "return lobster.IntLit(c.Int)") {
		t.Errorf("constInt does not spell integers with lobster.IntLit: %q", body)
	}
	if strings.Contains(body, "Int64()") || strings.Contains(body, "-9223372036854775808") {
		t.Error("constInt spells an int64 decimal; the minimum overflows")
	}
	lit := lobster.IntLit(big.NewInt(math.MinInt64))
	if lit != "0x8000000000000000" {
		t.Errorf("minimum int64 literal = %s, want 0x8000000000000000", lit)
	}
	if !strings.Contains(src, "schema_rt.view_constant(") {
		t.Error("descriptors.go does not name schema_rt.view_constant")
	}
	if !strings.Contains(src, "func emitUnionView(") {
		t.Error("descriptors.go does not name emitUnionView")
	}
	if !strings.Contains(src, "schema_rt.view_union_arm(") {
		t.Error("descriptors.go does not name schema_rt.view_union_arm")
	}
	packet := regexp.MustCompile(`(?s)case \*ir\.Union:.*?emitUnionView\(g, d, base\)`)
	if !packet.MatchString(src) || !strings.Contains(src, "g.Unit.TableUnions") {
		t.Error("packet unions are not registered through emitUnionView apart from table unions")
	}
	if !strings.Contains(src, "emitUnionView(g, un, base)") {
		t.Error("descriptors.go does not name emitUnionView for table unions")
	}
}
