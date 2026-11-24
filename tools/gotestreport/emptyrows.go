package main

// A ROW THAT PASSED NO TEST IS RED (nothing fails silently: no 0/0 pass).
//
// A record whose go test runs all skipped, or matched no test, exits 0, and
// the report used to read it as green: "26 skipped ... PASS" checked nothing.
// So a row whose records hold go test runs with no test passed is red, by
// name, unless the empty-row ledger (.github/empty-test-rows.txt) declares it:
//
//	empty <row> <reason>
//
// A declared row is held to its declaration the other way too: a row the
// ledger calls empty that passed a test is red, so a stale line cannot sit
// there covering a row that would later go quiet. A row with no record at
// all is -require-record's business, not this check's.

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// emptyRow is one line of the empty-row ledger.
type emptyRow struct {
	reason string
	line   int
}

// readEmptyRows parses the empty-row ledger. A line that is not
// `empty <row> <reason>` and a row named twice are errors.
func readEmptyRows(path string) (map[string]*emptyRow, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() { _ = f.Close() }() // errdiscard: read-only; a read error is sc.Err's
	rows := map[string]*emptyRow{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		word, rest, _ := strings.Cut(line, " ")
		name, reason, ok := strings.Cut(strings.TrimSpace(rest), " ")
		reason = strings.TrimSpace(reason)
		if word != "empty" || !ok || name == "" || reason == "" {
			return nil, fmt.Errorf("%s:%d: want `empty <row> <reason>`, got %q", path, n, line)
		}
		if prev := rows[name]; prev != nil {
			return nil, fmt.Errorf("%s:%d: row %q is already declared on line %d", path, n, name, prev.line)
		}
		rows[name] = &emptyRow{reason: reason, line: n}
	}
	if err := sc.Err(); err != nil {
		return nil, err
	}
	return rows, nil
}

// zeroPassFindings is the verdict on a row's passed-test count: red when no
// test passed and the row is not declared empty, red when a declared row
// passed one.
func zeroPassFindings(passed int, row, ledger string, declared *emptyRow) findings {
	switch {
	case passed == 0 && declared == nil:
		where := "no -row was named, so no empty-row declaration applies"
		if row != "" {
			where = fmt.Sprintf("%s does not declare row %q", ledger, row)
		}
		return findings{text: fmt.Sprintf("  NO TEST PASSED  this row's go test ran and no test passed (all skipped, or none matched): it checked nothing, and %s\n", where), red: true}
	case passed == 0:
		return findings{text: fmt.Sprintf("  DECLARED EMPTY  row %q passed no test, as %s:%d declares: %s\n", row, ledger, declared.line, declared.reason)}
	case declared != nil:
		return findings{text: fmt.Sprintf("  STALE EMPTY     row %q passed %d test(s), but %s:%d declares it empty: delete that line\n", row, passed, ledger, declared.line), red: true}
	}
	return findings{}
}
