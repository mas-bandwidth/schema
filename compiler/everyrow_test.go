// Every row, every leg. `docs/FIXED-FORM-VERSIONING-TESTS.md` is the list of
// edge cases the fixed form's versioning owes — one definition change per row,
// each read two ways — and each leg's versioning harness probes those rows BY
// ROW NAME (`{row: "field_append"}` in the Go harnesses, `field_append_case()`
// in the C++ reference). Nothing, until this gate, said that a row is probed on
// EVERY leg: a row could be written into the page, probed on four legs, and
// nobody would see the other five. Glenn, today: "The edge cases must be
// probed, tested and known to hold, and stay held."
//
// TestEveryRowEveryLeg is that sentence as a test. It reads the page's row
// tables, takes the nine legs' harnesses, and fails naming every (row, leg)
// pair nobody probes — EXCEPT the pairs written down in
// `docs/FIXED-FORM-OWED-ROWS.md`, each with the PR or card that owes it. The
// owed file is a LEDGER and not an escape: the gate also fails when the ledger
// lists a pair that IS probed, a row the page does not have, or a leg outside
// the nine, so the list can only shrink. A row landing on a leg is one line
// deleted from that file; a row ADDED to the page is owed on nine legs the
// moment it is written, which is the asymmetry that keeps the edge cases held.
//
// A row whose NEW-READS-OLD column is `—` is LOCK-only: no file exists for a
// leg to read, the row's whole proof is the lock's refusal in
// `internal/lockfile`, and the nine read harnesses do not owe it. That is read
// off the page rather than listed here, so a row that GAINS a read column
// starts being owed on nine legs without anyone editing this file.
//
// TestEveryRowHasCorpusBytes is the second half: a row with a read column needs
// bytes on disk, and the corpus dump writes one `row=` per row into
// `build/fixedform-corpus/manifest.txt`. The corpus is a build product, so the
// test skips when it is absent and fails instead under SCHEMA_REQUIRE_CORPUS,
// which is how a lane that built it asks for the check it paid for. Its
// exceptions live in the SAME ledger, under the target `corpus` rather than a
// leg, so both halves shrink the one file.
package compiler

import (
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// versioningTestsDoc is the page that lists the edge cases, relative to this
// package. It is the ONE list: the gate derives the rows from it and never
// carries a copy.
const versioningTestsDoc = "../docs/FIXED-FORM-VERSIONING-TESTS.md"

// owedRowsDoc is the ledger of (row, leg) pairs nobody probes yet.
const owedRowsDoc = "../docs/FIXED-FORM-OWED-ROWS.md"

// corpusManifest is what `fixedform_dump.cpp` writes, relative to this package.
const corpusManifest = "../build/fixedform-corpus/manifest.txt"

// versioningHarnesses is, per leg, the file(s) that probe the rows by name. The
// C++ reference is two translation units, every other leg one Go-driven
// harness; a path that does not exist is a leg that probes nothing, which the
// gate reports as owed rather than skipping.
var versioningHarnesses = map[string][]string{
	"cpp":    {"../test/tables/versioning_numbers.cpp", "../test/tables/versioning_lists.cpp"},
	"c":      {"../internal/codegen/ctable/fixedversioning_test.go"},
	"go":     {"../internal/codegen/gotable/fixedversioning_test.go"},
	"rust":   {"../internal/codegen/rusttable/fixedversioning_test.go"},
	"dart":   {"../internal/codegen/darttable/fixedversioning_test.go"},
	"js":     {"../internal/codegen/jstable/fixedversioning_test.go"},
	"java":   {"../internal/codegen/javatable/fixedversioning_test.go"},
	"cs":     {"../internal/codegen/cstable/fixedversioning_test.go"},
	"elixir": {"../internal/codegen/elixirtable/fixedversioning_test.go"},
}

// theNineLegs is the harness map's keys in the order a report reads best: the
// reference first, because the reference is written first.
var theNineLegs = []string{"cpp", "c", "go", "rust", "dart", "js", "java", "cs", "elixir"}

// corpusTarget is the ledger's tenth target: not a leg but the BYTES, for a row
// the dump does not write yet. It keeps the second gate's exceptions in the
// same ledger, under the same rule — a line may only be deleted.
const corpusTarget = "corpus"

// rowNamePattern is what a row name may be: the page spells one
// `fixed_I_grow`, so the capital is in the alphabet.
var rowNamePattern = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_]*$`)

// codeSpan pulls `name` out of a table cell that is exactly one code span.
var codeSpan = regexp.MustCompile("^`([^`]+)`$")

// leadingCodeSpan pulls `name` out of a cell that OPENS with a code span and
// may carry a reference after it — the divergence table writes
// "`unknown_census` (§5.8 row 11)".
var leadingCodeSpan = regexp.MustCompile("^`([^`]+)`")

// emDash is the page's "this column cannot exist for this row".
const emDash = "—"

// minimumRows guards against a page restructure silently emptying this gate: a
// gate over no rows proves nothing. The page carries 30-odd rows today.
const minimumRows = 20

// docRow is one row of the page: its name, the table it came from, the line it
// is written on, and whether any leg can read a file for it at all.
type docRow struct {
	name     string
	line     int
	lockOnly bool // NEW-READS-OLD is `—`: the lock proves it, no leg reads it
}

// markdownTable is one pipe table: its header cells and its body rows, with the
// line number of each.
type markdownTable struct {
	header []string
	rows   [][]string
	lines  []int
}

// splitRow cuts a pipe table line into trimmed cells. A leading and trailing
// pipe are the page's style; the empty fields they make are dropped.
func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimPrefix(s, "|")
	s = strings.TrimSuffix(s, "|")
	// A `\|` inside a cell (the page writes ranges as `int32 \| 0..100`) is not
	// a cell boundary.
	s = strings.ReplaceAll(s, `\|`, "\x00")
	cells := strings.Split(s, "|")
	for i := range cells {
		cells[i] = strings.TrimSpace(strings.ReplaceAll(cells[i], "\x00", `\|`))
	}
	return cells
}

// isDelimiterRow reports whether a line is a table's `|---|---|` rule.
func isDelimiterRow(cells []string) bool {
	if len(cells) == 0 {
		return false
	}
	for _, c := range cells {
		if c == "" || strings.Trim(c, ":- ") != "" {
			return false
		}
	}
	return true
}

// markdownTables finds every pipe table in a page.
func markdownTables(text string) []markdownTable {
	lines := strings.Split(text, "\n")
	var out []markdownTable
	for i := 0; i < len(lines); i++ {
		if !strings.HasPrefix(strings.TrimSpace(lines[i]), "|") {
			continue
		}
		if i+1 >= len(lines) || !isDelimiterRow(splitRow(lines[i+1])) {
			continue
		}
		t := markdownTable{header: splitRow(lines[i])}
		j := i + 2
		for ; j < len(lines) && strings.HasPrefix(strings.TrimSpace(lines[j]), "|"); j++ {
			t.rows = append(t.rows, splitRow(lines[j]))
			t.lines = append(t.lines, j+1)
		}
		out = append(out, t)
		i = j - 1
	}
	return out
}

// columnContaining returns the index of the first header cell holding want,
// case-insensitively, or -1.
func columnContaining(header []string, want string) int {
	for i, h := range header {
		if strings.Contains(strings.ToUpper(h), strings.ToUpper(want)) {
			return i
		}
	}
	return -1
}

// docRows reads the page's row tables. A row table is one whose first header
// cell is exactly `row` — the main table of definition changes and the
// divergence table today — so a table ABOUT the rows (the four columns, the
// floor and hash tests, the dump's manifest lines) is not mistaken for one.
func docRows(t *testing.T) []docRow {
	t.Helper()
	data, err := os.ReadFile(versioningTestsDoc)
	if err != nil {
		t.Fatalf("the page that lists the edge cases is unreadable: %v", err)
	}
	var rows []docRow
	seen := map[string]int{}
	for _, table := range markdownTables(string(data)) {
		if len(table.header) == 0 || !strings.EqualFold(table.header[0], "row") {
			continue
		}
		// A table with no NEW-READS-OLD column (the divergence table) owes
		// every leg: its rows name the read in prose instead.
		readCol := columnContaining(table.header, "NEW-READS-OLD")
		for k, cells := range table.rows {
			m := leadingCodeSpan.FindStringSubmatch(cells[0])
			if m == nil {
				t.Errorf("%s:%d: a row table's first cell does not open with a code span naming the row: %q",
					versioningTestsDoc, table.lines[k], cells[0])
				continue
			}
			name := m[1]
			if !rowNamePattern.MatchString(name) {
				t.Errorf("%s:%d: %q is not a row name", versioningTestsDoc, table.lines[k], name)
				continue
			}
			if prev, dup := seen[name]; dup {
				t.Errorf("%s:%d: row %s is listed twice (first at line %d)",
					versioningTestsDoc, table.lines[k], name, prev)
				continue
			}
			seen[name] = table.lines[k]
			lockOnly := false
			if readCol >= 0 && readCol < len(cells) {
				lockOnly = cells[readCol] == emDash
			}
			rows = append(rows, docRow{name: name, line: table.lines[k], lockOnly: lockOnly})
		}
	}
	if len(rows) < minimumRows {
		t.Fatalf("%s: only %d rows parsed (expected at least %d) — the page's row tables changed shape and this gate is now over nothing",
			versioningTestsDoc, len(rows), minimumRows)
	}
	return rows
}

// probePattern matches a row name used as a probe: the name on its own
// (`{row: "field_append"}`, `old_field_append.bin`'s row= line) or with the
// reference's suffixes (`field_append_case`, `enum_append_hostile_case`). It is
// deliberately anchored on both sides, so `fixed_I_grow` takes no credit from
// `fixed_I_grow_element` and `string_grow` none from `wstring_grow`.
func probePattern(row string) *regexp.Regexp {
	return regexp.MustCompile(`(^|[^A-Za-z0-9_])` + regexp.QuoteMeta(row) + `(_hostile)?(_case)?([^A-Za-z0-9_]|$)`)
}

// probed reads every leg's harness once and reports, per leg, the set of rows
// it names. A missing harness file is a leg that probes nothing: the gate then
// reports its pairs as owed, which is the true state for a leg whose harness is
// still in a PR.
func probed(t *testing.T, rows []docRow) map[string]map[string]bool {
	t.Helper()
	patterns := map[string]*regexp.Regexp{}
	for _, r := range rows {
		patterns[r.name] = probePattern(r.name)
	}
	out := map[string]map[string]bool{}
	for _, leg := range theNineLegs {
		paths, ok := versioningHarnesses[leg]
		if !ok {
			t.Fatalf("leg %q has no harness listed", leg)
		}
		out[leg] = map[string]bool{}
		for _, path := range paths {
			data, err := os.ReadFile(path)
			if err != nil {
				if !os.IsNotExist(err) {
					t.Fatalf("%s: %v", path, err)
				}
				continue // no harness yet: every row is owed
			}
			text := string(data)
			for name, pat := range patterns {
				if pat.MatchString(text) {
					out[leg][name] = true
				}
			}
		}
	}
	return out
}

// owedPair is one line of the ledger.
type owedPair struct {
	row, leg, owner, since string
	line                   int
}

// owedRows reads the ledger. Its shape is one table: row | leg | owed by |
// since. A missing file is an EMPTY ledger and not a skip — nothing may be owed
// by a file that does not exist.
func owedRows(t *testing.T) []owedPair {
	t.Helper()
	data, err := os.ReadFile(owedRowsDoc)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		t.Fatalf("%s: %v", owedRowsDoc, err)
	}
	var out []owedPair
	for _, table := range markdownTables(string(data)) {
		if len(table.header) < 4 || !strings.EqualFold(table.header[0], "row") ||
			!strings.EqualFold(table.header[1], "leg") {
			continue
		}
		for k, cells := range table.rows {
			if len(cells) < 4 {
				t.Errorf("%s:%d: a ledger line needs row | leg | owed by | since: %v",
					owedRowsDoc, table.lines[k], cells)
				continue
			}
			row := cells[0]
			if m := codeSpan.FindStringSubmatch(row); m != nil {
				row = m[1]
			}
			leg := strings.Trim(cells[1], "`")
			owner := cells[2]
			since := cells[3]
			if owner == "" || since == "" {
				t.Errorf("%s:%d: %s on %s is owed by nobody and since nothing — write the PR or card and the date (`unassigned` is a legal owner)",
					owedRowsDoc, table.lines[k], row, leg)
			}
			out = append(out, owedPair{row: row, leg: leg, owner: owner, since: since, line: table.lines[k]})
		}
	}
	return out
}

func TestEveryRowEveryLeg(t *testing.T) {
	rows := docRows(t)
	have := probed(t, rows)
	ledger := owedRows(t)

	byName := map[string]docRow{}
	for _, r := range rows {
		byName[r.name] = r
	}

	// The ledger first: a line that is wrong about the world is a failure, so
	// the list can only ever shrink.
	owed := map[string]owedPair{}
	for _, p := range ledger {
		key := p.row + "/" + p.leg
		if prev, dup := owed[key]; dup {
			t.Errorf("%s:%d: %s on %s is owed twice (first at line %d)",
				owedRowsDoc, p.line, p.row, p.leg, prev.line)
			continue
		}
		owed[key] = p
		if _, ok := byName[p.row]; !ok {
			t.Errorf("%s:%d: %s is not a row of %s — a ledger line cannot owe a row that does not exist",
				owedRowsDoc, p.line, p.row, versioningTestsDoc)
			continue
		}
		if _, ok := versioningHarnesses[p.leg]; !ok && p.leg != corpusTarget {
			t.Errorf("%s:%d: %q is neither one of the nine legs (%s) nor %q",
				owedRowsDoc, p.line, p.leg, strings.Join(theNineLegs, " "), corpusTarget)
			continue
		}
		if byName[p.row].lockOnly {
			t.Errorf("%s:%d: %s is LOCK-only on %s (its NEW-READS-OLD column is %s) — no leg owes it a read, so this line cannot be owed",
				owedRowsDoc, p.line, p.row, versioningTestsDoc, emDash)
			continue
		}
		if p.leg == corpusTarget {
			continue // TestEveryRowHasCorpusBytes owns this line
		}
		if have[p.leg][p.row] {
			t.Errorf("%s:%d: %s IS probed on %s now (owed by %s since %s) — delete this line; the ledger only shrinks",
				owedRowsDoc, p.line, p.row, p.leg, p.owner, p.since)
		}
	}

	// Then the world: every (row, leg) pair with no probe and no ledger line.
	var missing []string
	for _, r := range rows {
		if r.lockOnly {
			continue
		}
		for _, leg := range theNineLegs {
			if have[leg][r.name] {
				continue
			}
			if _, ok := owed[r.name+"/"+leg]; ok {
				continue
			}
			missing = append(missing, fmt.Sprintf("  %s on %s (%s:%d; probed by %s)",
				r.name, leg, versioningTestsDoc, r.line,
				strings.Join(versioningHarnesses[leg], " ")))
		}
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d (row, leg) pairs are neither probed nor owed:\n%s\n\nProbe the row in that leg's harness by name, or write the pair into %s with the PR or card that owes it.",
			len(missing), strings.Join(missing, "\n"), owedRowsDoc)
	}
}

// TestEveryRowHasCorpusBytes is the second gate: a row a leg must READ needs
// bytes, and the dump says which rows it wrote in the manifest's `row=` fields.
// The corpus is a build product: absent, the test skips; absent under
// SCHEMA_REQUIRE_CORPUS, it fails, which is how a lane that built the corpus
// asks for the check it paid for.
func TestEveryRowHasCorpusBytes(t *testing.T) {
	required := os.Getenv("SCHEMA_REQUIRE_CORPUS") != ""
	data, err := os.ReadFile(corpusManifest)
	if err != nil {
		if !os.IsNotExist(err) {
			t.Fatalf("%s: %v", corpusManifest, err)
		}
		if required {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and %s does not exist — build the fixed-form corpus first", corpusManifest)
		}
		t.Skipf("no corpus at %s (set SCHEMA_REQUIRE_CORPUS to make this a failure)", corpusManifest)
	}
	rowField := regexp.MustCompile(`(^|\s)row=([A-Za-z][A-Za-z0-9_]*)`)
	inCorpus := map[string]bool{}
	for _, m := range rowField.FindAllStringSubmatch(string(data), -1) {
		inCorpus[m[2]] = true
	}
	if len(inCorpus) == 0 {
		t.Fatalf("%s carries no row= field — the manifest changed shape and this gate is now over nothing", corpusManifest)
	}
	rows := docRows(t)
	byName := map[string]docRow{}
	for _, r := range rows {
		byName[r.name] = r
	}
	owed := map[string]owedPair{}
	for _, p := range owedRows(t) {
		if p.leg != corpusTarget {
			continue
		}
		owed[p.row] = p
		if r, ok := byName[p.row]; ok && !r.lockOnly && inCorpus[p.row] {
			t.Errorf("%s:%d: %s IS in the corpus now (owed by %s since %s) — delete this line; the ledger only shrinks",
				owedRowsDoc, p.line, p.row, p.owner, p.since)
		}
	}
	var missing []string
	for _, r := range rows {
		if r.lockOnly || inCorpus[r.name] {
			continue
		}
		if _, ok := owed[r.name]; ok {
			continue
		}
		missing = append(missing, fmt.Sprintf("  %s (%s:%d)", r.name, versioningTestsDoc, r.line))
	}
	sort.Strings(missing)
	if len(missing) > 0 {
		t.Errorf("%d rows with a read column have no bytes in the corpus:\n%s\n\nEither the dump writes the pair and names it `row=<row>` in %s, or the row is LOCK-only and its NEW-READS-OLD column says %s, or the row is written into %s against the target %q with the PR or card that owes it.",
			len(missing), strings.Join(missing, "\n"), corpusManifest, emDash, owedRowsDoc, corpusTarget)
	}
}

// TestEveryRowProbeNameIsAnchored is the gate's own red: a probe pattern that
// matched loosely would hand a leg credit for a row it never wrote, which is
// exactly the silence this file exists to end. The pairs below are the page's
// own near-misses — `string_grow` inside `wstring_grow`, `fixed_I_grow` inside
// `fixed_I_grow_element`, `enum_append` inside `keyed_array_enum_append`, a row
// name inside a corpus FILE name — beside the spellings that must count.
func TestEveryRowProbeNameIsAnchored(t *testing.T) {
	cases := []struct {
		row, text string
		want      bool
	}{
		{"field_append", `{row: "field_append"},`, true},
		{"field_append", "static void field_append_case()", true},
		{"enum_append", "static void enum_append_hostile_case()", true},
		{"array_bounded_grow", "void array_bounded_grow_case()", true},
		{"string_grow", `{row: "string_grow"},`, true},
		{"string_grow", `{row: "wstring_grow"},`, false},
		{"fixed_I_grow", `{row: "fixed_I_grow_element"},`, false},
		{"fixed_I_grow_element", `{row: "fixed_I_grow_element"},`, true},
		{"enum_append", `{row: "keyed_array_enum_append"},`, false},
		{"array_bounded_grow", `"hostile_array_bounded_grow.bin"`, false},
		{"union_append", `{row: "union_arm_payload_widen"},`, false},
	}
	for _, c := range cases {
		if got := probePattern(c.row).MatchString(c.text); got != c.want {
			t.Errorf("probePattern(%q) on %q = %v, want %v", c.row, c.text, got, c.want)
		}
	}
}
