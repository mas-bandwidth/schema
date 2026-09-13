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
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"unicode"
	"unicode/utf8"
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

// rowRegistered reports whether the harness text registers `row` on an entry
// point the leg actually EXECUTES. It is deliberately scoped to REGISTRATION
// coverage, not to what the probe asserts.
//
// A row name in text is not evidence, and this function refuses the three
// false credits a bare regexp hands out for free:
//
//   - `void <row>_case();`, a DECLARATION, is not a call;
//   - `<row>_case();` in a helper the runner never invokes is not a probe;
//   - `{row: "<row>"}` in a table nobody ranges over is not a probe.
//
// So the two languages are read as STRUCTURE: a Go harness is parsed into AST
// and the entry must be an element of a table the Test* function ranges over
// (resolving the range identifier to its declaration, so shadowed locals or
// un-iterated tables never earn credit). The C++ reference's call must sit
// inside a verified runner invoked from fixedform_main.cpp's main
// (versioning_numbers_cases and versioning_lists_cases) or inside main itself.
// String and character literals and comments are masked so strings never match
// as calls, and function declarations are distinguished from call statements.

var (
	cppRunnersOnce   sync.Once
	cachedCppRunners map[string]bool
)

func getVerifiedCppRunners() map[string]bool {
	cppRunnersOnce.Do(func() {
		cachedCppRunners = map[string]bool{"main": true}
		paths := []string{
			"../test/tables/fixedform_main.cpp",
			"test/tables/fixedform_main.cpp",
		}
		var mainData []byte
		for _, p := range paths {
			if d, err := os.ReadFile(p); err == nil {
				mainData = d
				break
			}
		}
		if len(mainData) > 0 {
			masked := maskCppCommentsAndLiterals(string(mainData))
			fns := extractCppFunctions(masked)
			if mainBody, ok := fns["main"]; ok {
				for _, runner := range []string{"versioning_numbers_cases", "versioning_lists_cases"} {
					if isCppCall(mainBody, runner) {
						cachedCppRunners[runner] = true
					}
				}
			}
		}
	})
	return cachedCppRunners
}

// parseHarnessRegisteredRows parses a harness file once and extracts all
// registered row names.
func parseHarnessRegisteredRows(text string) map[string]bool {
	fset := token.NewFileSet()
	if file, err := parser.ParseFile(fset, "harness.go", text, 0); err == nil {
		return extractGoRegisteredRows(file)
	}
	return extractCppRegisteredRows(text)
}

func rowRegistered(text, row string) bool {
	return parseHarnessRegisteredRows(text)[row]
}

// extractGoRegisteredRows extracts rows registered in a Go harness. A row must
// be an entry of a table that an executed Go test function ranges over directly,
// resolving the ranged identifier to its definition.
func extractGoRegisteredRows(file *ast.File) map[string]bool {
	rows := make(map[string]bool)
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || !isGoTestFunc(fn) {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if _, ok := n.(*ast.FuncLit); ok {
				return false
			}
			rs, ok := n.(*ast.RangeStmt)
			if !ok {
				return true
			}
			collectRowsFromExpr(rs.X, rows)
			return true
		})
	}
	return rows
}

// isGoTestFunc reports whether fn is an executed Go test entrypoint:
// top-level function, no receiver, name matching Test[A-Z0-9_]*, signature
// func(*testing.T) with no returns.
func isGoTestFunc(fn *ast.FuncDecl) bool {
	if fn == nil || fn.Recv != nil || fn.Body == nil {
		return false
	}
	name := fn.Name.Name
	if !strings.HasPrefix(name, "Test") {
		return false
	}
	if len(name) > 4 {
		r, _ := utf8.DecodeRuneInString(name[4:])
		if unicode.IsLower(r) {
			return false
		}
	}
	if fn.Type.Params == nil || len(fn.Type.Params.List) != 1 {
		return false
	}
	field := fn.Type.Params.List[0]
	if len(field.Names) > 1 {
		return false
	}
	star, ok := field.Type.(*ast.StarExpr)
	if !ok {
		return false
	}
	switch x := star.X.(type) {
	case *ast.SelectorExpr:
		pkg, ok := x.X.(*ast.Ident)
		if !ok || pkg.Name != "testing" || x.Sel.Name != "T" {
			return false
		}
	case *ast.Ident:
		if x.Name != "T" {
			return false
		}
	default:
		return false
	}
	if fn.Type.Results != nil && len(fn.Type.Results.List) > 0 {
		return false
	}
	return true
}

func collectRowsFromExpr(expr ast.Expr, rows map[string]bool) {
	switch e := expr.(type) {
	case *ast.CompositeLit:
		collectRowsFromComposite(e, rows)
	case *ast.Ident:
		if e.Obj == nil {
			return
		}
		switch decl := e.Obj.Decl.(type) {
		case *ast.ValueSpec:
			idx := -1
			for i, name := range decl.Names {
				if name.Name == e.Name {
					idx = i
					break
				}
			}
			if idx >= 0 && idx < len(decl.Values) {
				collectRowsFromExpr(decl.Values[idx], rows)
			}
		case *ast.AssignStmt:
			idx := -1
			for i, lhs := range decl.Lhs {
				if id, ok := lhs.(*ast.Ident); ok && id.Name == e.Name {
					idx = i
					break
				}
			}
			if idx >= 0 && idx < len(decl.Rhs) {
				collectRowsFromExpr(decl.Rhs[idx], rows)
			}
		}
	}
}

func collectRowsFromComposite(cl *ast.CompositeLit, rows map[string]bool) {
	for _, elt := range cl.Elts {
		switch e := elt.(type) {
		case *ast.CompositeLit:
			collectRowsFromComposite(e, rows)
		case *ast.KeyValueExpr:
			if key, ok := e.Key.(*ast.Ident); ok && (key.Name == "row" || key.Name == "name") {
				if lit, ok := e.Value.(*ast.BasicLit); ok && lit.Kind == token.STRING {
					if s, err := strconv.Unquote(lit.Value); err == nil {
						rows[s] = true
					}
				}
			}
		}
	}
}

// maskCppCommentsAndLiterals replaces comment contents, string literals, and
// character literals with spaces (preserving newlines), ensuring comments and
// literal text cannot be misidentified as code.
func maskCppCommentsAndLiterals(text string) string {
	out := []byte(text)
	for i := 0; i < len(out); {
		switch {
		case i+1 < len(out) && out[i] == '/' && out[i+1] == '/':
			for i < len(out) && out[i] != '\n' {
				out[i] = ' '
				i++
			}
		case i+1 < len(out) && out[i] == '/' && out[i+1] == '*':
			out[i], out[i+1] = ' ', ' '
			for i += 2; i+1 < len(out) && !(out[i] == '*' && out[i+1] == '/'); i++ {
				if out[i] != '\n' {
					out[i] = ' '
				}
			}
			if i+1 < len(out) {
				out[i], out[i+1] = ' ', ' '
				i += 2
			}
		case out[i] == '"' || out[i] == '\'':
			quote := out[i]
			out[i] = ' '
			i++
			for i < len(out) {
				if out[i] == '\\' {
					out[i] = ' '
					if i+1 < len(out) {
						if out[i+1] != '\n' {
							out[i+1] = ' '
						}
						i += 2
					} else {
						i++
					}
					continue
				}
				if out[i] == quote {
					out[i] = ' '
					i++
					break
				}
				if out[i] != '\n' {
					out[i] = ' '
				}
				i++
			}
		default:
			i++
		}
	}
	return string(out)
}

func matchingBrace(code string, open int) int {
	depth := 0
	for i := open; i < len(code); i++ {
		switch code[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return i
			}
		}
	}
	return -1
}

// extractCppFunctions extracts function definitions from masked C++ source text.
func extractCppFunctions(maskedCode string) map[string]string {
	fnHeader := regexp.MustCompile(`(?m)(?:^|[;{}])\s*(?:[A-Za-z0-9_:<*&>\s]+?\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*\([^;{}]*\)\s*\{`)
	fns := make(map[string]string)
	for _, m := range fnHeader.FindAllStringSubmatchIndex(maskedCode, -1) {
		name := maskedCode[m[2]:m[3]]
		open := strings.IndexByte(maskedCode[m[0]:], '{')
		if open < 0 {
			continue
		}
		start := m[0] + open
		end := matchingBrace(maskedCode, start)
		if end > start {
			fns[name] = maskedCode[start : end+1]
		}
	}
	return fns
}

// isCppCall reports whether `name()` is called inside body, excluding type declarations.
func isCppCall(body, name string) bool {
	re := regexp.MustCompile(`(?:^|[^A-Za-z0-9_])(?:([A-Za-z0-9_]+)\s+)?` + regexp.QuoteMeta(name) + `\s*\(\s*\)`)
	matches := re.FindAllStringSubmatch(body, -1)
	for _, m := range matches {
		prefix := strings.TrimSpace(m[1])
		switch prefix {
		case "void", "int", "bool", "char", "auto", "float", "double", "unsigned", "signed", "struct", "extern":
			continue // declaration
		default:
			return true
		}
	}
	return false
}

var rowCallPat = regexp.MustCompile(`(?:^|[^A-Za-z0-9_])(?:([A-Za-z0-9_]+)\s+)?([A-Za-z0-9_]+?)(?:_hostile)?_case\s*\(\s*\)\s*;`)

// extractCppRegisteredRows extracts rows registered in a C++ harness. Calls
// must be within verified runners invoked from main or within main itself.
func extractCppRegisteredRows(text string) map[string]bool {
	verified := getVerifiedCppRunners()
	masked := maskCppCommentsAndLiterals(text)
	fns := extractCppFunctions(masked)

	allowed := make(map[string]bool)
	for k, v := range verified {
		allowed[k] = v
	}
	if mainBody, ok := fns["main"]; ok {
		for fnName := range fns {
			if isCppCall(mainBody, fnName) {
				allowed[fnName] = true
			}
		}
	}

	rows := make(map[string]bool)
	for fnName, body := range fns {
		if !allowed[fnName] {
			continue
		}
		matches := rowCallPat.FindAllStringSubmatch(body, -1)
		for _, m := range matches {
			prefix := strings.TrimSpace(m[1])
			if prefix != "" {
				continue // declaration
			}
			rowName := m[2]
			rows[rowName] = true
		}
	}
	return rows
}

// probed reads every leg's harness once and reports, per leg, the set of rows
// it names. A missing harness file is a leg that probes nothing: the gate then
// reports its pairs as owed, which is the true state for a leg whose harness is
// still in a PR.
func probed(t *testing.T, rows []docRow) map[string]map[string]bool {
	t.Helper()
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
			for rName := range parseHarnessRegisteredRows(string(data)) {
				out[leg][rName] = true
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
	// REPAIR (b): the manifest's `row=` fields were all this gate read, so a
	// manifest naming every row with every binary deleted still passed. Every
	// manifested file must exist, be a confined regular file and be non-empty,
	// and every row it claims bytes for must carry BOTH an old_ and a new_ side.
	for _, problem := range corpusFileProblems(string(data), filepath.Dir(corpusManifest), rows, inCorpus) {
		t.Error(problem)
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

// corpusFileProblems validates the manifest's file references: every line that
// names a file must name a CONFINED, regular, non-empty file, and every page
// row the manifest claims bytes for must carry BOTH an old_ and a new_ file. It
// is a shape check over the manifest and the filesystem, not a semantic proof
// of the bytes. The manifest path is used in messages only; `dir` is where the
// files live.
func corpusFileProblems(manifest, dir string, rows []docRow, inCorpus map[string]bool) []string {
	var problems []string
	sides := map[string]map[string]bool{}
	for n, line := range strings.Split(manifest, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		var file, row, side string
		for _, field := range strings.Fields(line) {
			k, v, ok := strings.Cut(field, "=")
			if !ok {
				continue
			}
			switch k {
			case "file":
				if file == "" {
					file = v
				}
			case "row":
				if row == "" {
					row = v
				}
			case "side":
				if side == "" {
					side = v
				}
			}
		}
		if file == "" || row == "" {
			continue
		}
		if file != filepath.Base(file) || file == "." || file == ".." {
			problems = append(problems, fmt.Sprintf("%s:%d: row %s names file=%s, which is not a confined base name",
				corpusManifest, n+1, row, file))
			continue
		}
		if sides[row] == nil {
			sides[row] = map[string]bool{}
		}
		sides[row][side] = true
		full := filepath.Join(dir, file)
		// Lstat, NOT Stat: Stat follows a symlink, so `old_x.bin` pointing at a
		// nonempty file outside the corpus dir used to pass the confinement
		// check. The generated corpus needs no links, so every entry must be a
		// regular file inside the corpus dir.
		fi, err := os.Lstat(full)
		if err != nil {
			problems = append(problems, fmt.Sprintf("%s:%d: row %s side %s names %s, which does not exist: %v",
				corpusManifest, n+1, row, side, file, err))
			continue
		}
		if fi.Mode()&os.ModeSymlink != 0 {
			problems = append(problems, fmt.Sprintf("%s:%d: %s is a symlink; corpus files must be regular files inside the corpus dir",
				corpusManifest, n+1, file))
		} else if !fi.Mode().IsRegular() {
			problems = append(problems, fmt.Sprintf("%s:%d: %s is not a regular file", corpusManifest, n+1, file))
		} else if fi.Size() == 0 {
			problems = append(problems, fmt.Sprintf("%s:%d: %s is empty", corpusManifest, n+1, file))
		}
	}
	for _, r := range rows {
		if r.lockOnly || !inCorpus[r.name] {
			continue
		}
		for _, side := range []string{"old", "new"} {
			if !sides[r.name][side] {
				problems = append(problems, fmt.Sprintf("%s: row %s is in the corpus but has no side=%s file",
					corpusManifest, r.name, side))
			}
		}
	}
	return problems
}

// TestCorpusFileProblems is the confinement control for corpusFileProblems.
// `os.Stat` FOLLOWS a symlink, so `old_x.bin` and `new_x.bin` pointing at a
// nonempty file outside the corpus dir used to pass with no problem. The
// generated corpus needs no links, so every manifest entry must be a regular
// file INSIDE the corpus dir; this control stays in the suite so the
// confinement cannot silently reopen.
func TestCorpusFileProblems(t *testing.T) {
	rows := []docRow{{name: "field_append"}}
	inCorpus := map[string]bool{"field_append": true}
	manifest := "file=old_field_append.bin row=field_append side=old\n" +
		"file=new_field_append.bin row=field_append side=new\n"

	write := func(t *testing.T, dir, name, data string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}

	t.Run("regular nonempty files pass", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "old_field_append.bin", "old")
		write(t, dir, "new_field_append.bin", "new")
		if got := corpusFileProblems(manifest, dir, rows, inCorpus); len(got) != 0 {
			t.Fatalf("two confined regular nonempty files produced problems: %v", got)
		}
	})

	t.Run("links to an outside file are rejected", func(t *testing.T) {
		dir := t.TempDir()
		outside := filepath.Join(t.TempDir(), "outside.bin")
		if err := os.WriteFile(outside, []byte("not the corpus"), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "old_field_append.bin")); err != nil {
			t.Skipf("symlinks are unavailable here: %v", err)
		}
		if err := os.Symlink(outside, filepath.Join(dir, "new_field_append.bin")); err != nil {
			t.Skipf("symlinks are unavailable here: %v", err)
		}
		got := corpusFileProblems(manifest, dir, rows, inCorpus)
		if joined := strings.Join(got, "\n"); !strings.Contains(joined, "symlink") {
			t.Fatalf("links to an outside nonempty file were confined; problems = %v", got)
		}
	})

	t.Run("an empty side is rejected", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "old_field_append.bin", "old")
		write(t, dir, "new_field_append.bin", "")
		if got := corpusFileProblems(manifest, dir, rows, inCorpus); len(got) == 0 {
			t.Fatal("an empty side was accepted")
		}
	})

	t.Run("a missing side is rejected", func(t *testing.T) {
		dir := t.TempDir()
		write(t, dir, "old_field_append.bin", "old")
		if got := corpusFileProblems(manifest, dir, rows, inCorpus); len(got) == 0 {
			t.Fatal("a missing new_ side was accepted")
		}
	})

	t.Run("an unconfined base name is rejected", func(t *testing.T) {
		dir := t.TempDir()
		unconfined := "file=../outside.bin row=field_append side=old\n" +
			"file=new_field_append.bin row=field_append side=new\n"
		write(t, dir, "new_field_append.bin", "new")
		if got := corpusFileProblems(unconfined, dir, rows, inCorpus); len(got) == 0 {
			t.Fatal("a path outside the corpus dir was accepted")
		}
	})
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

// TestEveryRowProbedNeedsARegistration is repair (a)'s own red. A row name is
// not a probe: the harness must REGISTER the row on the entry point the leg
// actually EXECUTES — a `{row: "..."}` / `{name: "..."}` entry in the
// package-level table the test ranges over, or the reference's
// `<row>_case();` invoked from the runner `main` calls. The HOLD named three
// false credits a bare regexp hands out for free, and all three must be
// refused:
//
//   - `void <row>_case();`, a DECLARATION, is not a call;
//   - `<row>_case();` in a helper the runner never invokes is not a probe;
//   - `{row: "<row>"}` in a table nobody ranges over is not a probe.
//
// The positive half reads every one of the nine REAL harnesses, so the control
// cannot drift from the tree it guards.
func TestEveryRowProbedNeedsARegistration(t *testing.T) {
	negatives := []struct {
		what, name, text string
	}{
		{"a declaration", "field_append", "void field_append_case();"},
		{"an uncalled helper", "field_append", "void unused() { field_append_case(); }"},
		{"an un-iterated table", "field_append",
			"package probe\nfunc unused() { _ = []struct{ row string }{{row: \"field_append\"}} }"},
		{"Go table ranged only in unused()", "field_append",
			"package probe\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc unused(){for _,r:=range rows{_=r}}"},
		{"Go shadowed rows inside TestX", "field_append",
			"package probe\nimport \"testing\"\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc TestX(t *testing.T){rows:=[]int{1}; for _,r:=range rows{_=r}}"},
		{"Go lowercase test helper without signature", "field_append",
			"package probe\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc Testhelper(){for _,r:=range rows{_=r}}"},
		{"Go uncalled function literal inside TestX", "field_append",
			"package probe\nimport \"testing\"\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc TestX(t *testing.T){ _ = func(){for _,r:=range rows{_=r}} }"},
		{"C++ string literal in main", "field_append",
			`int main(){const char *s="field_append_case();";return 0;}`},
		{"C++ arbitrary unused_cases", "field_append",
			`int unused_cases(){field_append_case();return 0;}`},
		{"C++ local declaration in main", "field_append",
			`int main(){void field_append_case();return 0;}`},
	}
	for _, c := range negatives {
		if rowRegistered(c.text, c.name) {
			t.Errorf("rowRegistered(%q, %q) = true, want false: %s is not a registration on the executed test table or runner",
				c.text, c.name, c.what)
		}
	}

	positives := []struct {
		what, name, text string
	}{
		{"a ranged Go table", "fixed_I_grow_element",
			"package probe\nvar rows = []struct{ row string }{{row: \"fixed_I_grow_element\"}}\nfunc TestX(t *testing.T) { for _, r := range rows { _ = r } }\n"},
		{"a ranged Java-style table", "fixed_I_grow_element",
			"package probe\nvar rows = []struct{ name string }{{name: \"fixed_I_grow_element\"}}\nfunc TestX(t *testing.T) { for _, r := range rows { _ = r } }\n"},
		{"a C++ main call", "fixed_I_grow_element",
			"int main() {\n    fixed_I_grow_element_case();\n    return 0;\n}\n"},
		{"a C++ runner call from main", "fixed_I_grow_element",
			"int main() {\n    versioning_cases();\n    return 0;\n}\nint versioning_cases() {\n    fixed_I_grow_element_case();\n    return 0;\n}\n"},
		{"a C++ verified numbers runner call", "fixed_I_grow_element",
			"int versioning_numbers_cases() {\n    fixed_I_grow_element_case();\n    return 0;\n}\n"},
		{"a C++ verified lists runner call", "field_append",
			"int versioning_lists_cases() {\n    field_append_case();\n    return 0;\n}\n"},
	}
	for _, c := range positives {
		if !rowRegistered(c.text, c.name) {
			t.Errorf("rowRegistered(%q, %q) = false, want true: %s on the executed entry point", c.text, c.name, c.what)
		}
	}

	// The nine real harnesses: each registers field_append, and the helper must
	// see the registration rather than the harness's spelling.
	for _, leg := range theNineLegs {
		found := false
		for _, path := range versioningHarnesses[leg] {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("%s: %v", path, err)
			}
			if rowRegistered(string(data), "field_append") {
				found = true
			}
		}
		if !found {
			t.Errorf("leg %s registers field_append in %v, but rowRegistered does not see it", leg, versioningHarnesses[leg])
		}
	}
}

// TestStellaExecutionRoot verifies the execution-root false positive cases
// identified in the review of card 1022c and 1022d.
func TestStellaExecutionRoot(t *testing.T) {
	negatives := []struct {
		name, text string
	}{
		{
			"Go package table ranged only in unused()",
			"package probe\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc unused(){for _,r:=range rows{_=r}}",
		},
		{
			"Go shadowed rows inside TestX",
			"package probe\nimport \"testing\"\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc TestX(t *testing.T){rows:=[]int{1}; for _,r:=range rows{_=r}}",
		},
		{
			"Go lowercase test helper without signature",
			"package probe\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc Testhelper(){for _,r:=range rows{_=r}}",
		},
		{
			"Go uncalled function literal inside TestX",
			"package probe\nimport \"testing\"\nvar rows=[]struct{row string}{{row:\"field_append\"}}\nfunc TestX(t *testing.T){ _ = func(){for _,r:=range rows{_=r}} }",
		},
		{
			"C++ string literal in main",
			`int main(){const char *s="field_append_case();";return 0;}`,
		},
		{
			"C++ arbitrary unused_cases",
			`int unused_cases(){field_append_case();return 0;}`,
		},
		{
			"C++ local declaration in main",
			`int main(){void field_append_case();return 0;}`,
		},
	}
	for _, c := range negatives {
		if rowRegistered(c.text, "field_append") {
			t.Errorf("rowRegistered returned true, want false for %s:\n%s", c.name, c.text)
		}
	}
}
