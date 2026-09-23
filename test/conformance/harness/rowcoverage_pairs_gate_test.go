// gates/row-coverage/pairs (docs/roadmap.sexp): every applicable row/leg pair
// is covered or explicitly owed.
//
// THE ROWS are the roadmap's audit items and THE LEGS are the nine native
// harnesses, and both are enumerated from the tree — never a hard-coded list.
// A leg is a test/conformance/<lang>/driver, the registry's own discovery
// rule (registry.go), so a tenth harness lands in the matrix the day its
// driver does. A row is an :audit-item recorded by THE LEDGER,
// docs/roadmap.sexp — the row table ROADMAP.md is generated from. THE CORPUS
// is the per-leg probe under test/conformance/<leg>/rows/: "the assertion
// that proves it at the tip, in its own file".
//
// A pair is COVERED when a probe stands for it and the ledger entry does not
// contradict that, and EXPLICITLY OWED when the ledger says "owed" and no
// probe stands. Anything else is one red line naming the row and the leg:
//
//   - a pair whose ledger entry carries a HISTORICAL state
//     ("implemented-asserted", "weak") with no probe behind it is neither
//     covered nor explicitly owed: the historical report is not current
//     coverage ("current completion has not been reconciled", the entries'
//     own note), and probe-name presence alone is not semantic completion —
//     the probe and the ledger must AGREE;
//   - a probe standing over an "owed" entry is a STALE ledger entry: the debt
//     is paid and the ledger was never reconciled;
//   - a probe standing over an "inapplicable" entry is a WRONGLY EXEMPTED
//     one: the pair demonstrably applies;
//   - a probe the ledger does not name is an UNKNOWN pair, a probe naming a
//     row the ledger has never heard of is an UNKNOWN row, and a ledger entry
//     for a leg with no harness is an UNKNOWN ledger entry;
//   - two ledger entries for one pair, or two probes for one pair, are
//     DUPLICATES.
//
// Lock-only rows are excluded by their contract: shared/LOCK-L1..L7 are the
// lock's own rows (internal/lockfile, Go alone by design), they carry no
// :audit-item and no leg, and a per-leg enumeration never sees them.
package main

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// rowName is the shape of an audit item: the family letters, then the number.
var rowName = regexp.MustCompile(`^[A-Z]+[0-9]+$`)

// sexp is one node of the roadmap file: a string atom or a []sexp list.
type sexp any

// readSexp is a minimal s-expression reader, exactly enough for
// docs/roadmap.sexp: parentheses, "strings with \" escapes", atoms, and
// ; comments to end of line. It returns the toplevel forms.
func readSexp(path string) ([]sexp, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var toks []string
	for i := 0; i < len(data); {
		switch c := data[i]; {
		case c == ';':
			for i < len(data) && data[i] != '\n' {
				i++
			}
		case c == '(' || c == ')':
			toks = append(toks, string(c))
			i++
		case c == '"':
			i++
			var b strings.Builder
			for i < len(data) && data[i] != '"' {
				if data[i] == '\\' && i+1 < len(data) {
					i++
				}
				b.WriteByte(data[i])
				i++
			}
			if i >= len(data) {
				return nil, fmt.Errorf("%s: an unterminated string", path)
			}
			i++
			toks = append(toks, "\""+b.String())
		case c == ' ' || c == '\t' || c == '\n' || c == '\r':
			i++
		default:
			j := i
			for j < len(data) && !strings.ContainsRune("()\"; \t\n\r", rune(data[j])) {
				j++
			}
			toks = append(toks, string(data[i:j]))
			i = j
		}
	}
	stack := [][]sexp{nil}
	for _, tok := range toks {
		switch tok {
		case "(":
			stack = append(stack, nil)
		case ")":
			if len(stack) < 2 {
				return nil, fmt.Errorf("%s: a ) without its (", path)
			}
			top := stack[len(stack)-1]
			stack = stack[:len(stack)-1]
			stack[len(stack)-1] = append(stack[len(stack)-1], top)
		default:
			atom := tok
			if strings.HasPrefix(tok, "\"") {
				atom = tok[1:]
			}
			stack[len(stack)-1] = append(stack[len(stack)-1], atom)
		}
	}
	if len(stack) != 1 {
		return nil, fmt.Errorf("%s: an unbalanced (", path)
	}
	return stack[0], nil
}

// prop is plist access: the value after key, with ok false when it is absent
// or not the shape asked for.
func prop(list sexp, key string) (sexp, bool) {
	items, ok := list.([]sexp)
	if !ok {
		return nil, false
	}
	for i := 0; i+1 < len(items); i++ {
		if items[i] == key {
			return items[i+1], true
		}
	}
	return nil, false
}

func propString(list sexp, key string) string {
	v, ok := prop(list, key)
	if !ok {
		return ""
	}
	s, _ := v.(string)
	return s
}

// ledgerEntry is one row/leg pair as the ledger records it: the row, the leg
// the entry's id prefixes it with, and the entry's reported state.
type ledgerEntry struct {
	row   string
	leg   string
	state string
}

// readRowLedger reads docs/roadmap.sexp: every :task node carrying an
// :audit-item is one ledger entry. Lock-only rows (shared/LOCK-L1..L7) carry
// no :audit-item and no leg, so they are excluded here by their contract.
func readRowLedger(path string) (entries []ledgerEntry, duplicates []string, err error) {
	forms, err := readSexp(path)
	if err != nil {
		return nil, nil, err
	}
	seen := map[string]bool{}
	for _, form := range forms {
		nodes, ok := prop(form, ":nodes")
		if !ok {
			continue
		}
		list, ok := nodes.([]sexp)
		if !ok {
			continue
		}
		for _, node := range list {
			if propString(node, ":type") != ":task" {
				continue
			}
			row := propString(node, ":audit-item")
			if row == "" {
				continue
			}
			id := propString(node, ":id")
			leg, _, _ := strings.Cut(id, "/")
			if seen[id] {
				duplicates = append(duplicates, id)
			}
			seen[id] = true
			entries = append(entries, ledgerEntry{row: row, leg: leg, state: propString(node, ":reported-state")})
		}
	}
	return entries, duplicates, nil
}

// TestRowCoveragePairsGate is the pairs gate itself: the rows and the nine
// legs enumerated from the tree, one red line per pair that is neither
// covered nor explicitly owed, and one per stale, unknown, duplicate or
// wrongly exempted ledger entry.
func TestRowCoveragePairsGate(t *testing.T) {
	root := "../../../"

	// THE LEGS, discovered: a leg is a <lang>/driver, the committed
	// registry's own rule — so this is the nine native harnesses today and
	// follows the tree when a tenth registers.
	drivers, err := discoverDrivers(filepath.Join(root, "test", "conformance"))
	if err != nil {
		t.Fatal(err)
	}
	var legs []string
	knownLeg := map[string]bool{}
	for _, d := range drivers {
		legs = append(legs, d.lang)
		knownLeg[d.lang] = true
	}
	sort.Strings(legs)

	// THE LEDGER: the row table, docs/roadmap.sexp.
	entries, dupEntries, err := readRowLedger(filepath.Join(root, "docs", "roadmap.sexp"))
	if err != nil {
		t.Fatal(err)
	}
	if len(entries) == 0 {
		t.Fatal("docs/roadmap.sexp names no row/leg ledger entries — the gate would pass on nothing")
	}
	byPair := map[[2]string]ledgerEntry{}
	ledgerRows := map[string]bool{}
	for _, e := range entries {
		byPair[[2]string{e.leg, e.row}] = e
		ledgerRows[e.row] = true
	}

	// THE CORPUS: one probe per covered pair under each leg's rows/.
	probes := map[[2]string]string{}
	probeFiles := map[[2]string][]string{}
	for _, leg := range legs {
		dir := filepath.Join(root, "test", "conformance", leg, "rows")
		list, err := os.ReadDir(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		for _, ent := range list {
			name := ent.Name()
			row := name
			if i := strings.IndexAny(name, "._"); i >= 0 {
				row = name[:i]
			}
			pair := [2]string{leg, row}
			probeFiles[pair] = append(probeFiles[pair], name)
			probes[pair] = filepath.Join("test", "conformance", leg, "rows", name)
		}
	}

	var failures []string
	fail := func(format string, args ...any) {
		failures = append(failures, fmt.Sprintf(format, args...))
	}

	// the ledger's own hygiene: duplicates, unknown legs, unknown row names
	for _, id := range dupEntries {
		leg, row, _ := strings.Cut(id, "/")
		fail("row %s leg %s: DUPLICATE ledger entry %q", row, leg, id)
	}
	for _, e := range entries {
		if !knownLeg[e.leg] {
			fail("row %s leg %s: UNKNOWN ledger entry — no harness named %q has a driver", e.row, e.leg, e.leg)
		}
		if !rowName.MatchString(e.row) {
			fail("row %s leg %s: UNKNOWN row name %q — an audit item is the family letters and the number", e.row, e.leg, e.row)
		}
	}
	// the corpus's own hygiene: duplicate probes, unknown rows
	for pair, files := range probeFiles {
		if len(files) > 1 {
			fail("row %s leg %s: DUPLICATE probes %s — one pair, one file", pair[1], pair[0], strings.Join(files, ", "))
		}
		if !ledgerRows[pair[1]] {
			fail("row %s leg %s: UNKNOWN row — %s names a row the ledger does not", pair[1], pair[0], probes[pair])
		}
		if !rowName.MatchString(pair[1]) {
			fail("row %s leg %s: UNKNOWN row name in %s — a probe is <ROW>.<ext> of an audit item", pair[1], pair[0], probes[pair])
		}
	}

	// THE PAIRS: every ledgered row against every discovered leg.
	var rows []string
	for row := range ledgerRows {
		rows = append(rows, row)
	}
	sort.Strings(rows)
	for _, row := range rows {
		for _, leg := range legs {
			pair := [2]string{leg, row}
			entry, ledgered := byPair[pair]
			probe, probed := probes[pair]
			switch {
			case ledgered && entry.state == "inapplicable" && probed:
				fail("row %s leg %s: WRONGLY EXEMPTED — the ledger calls the pair inapplicable and %s covers it", row, leg, probe)
			case ledgered && entry.state == "owed" && probed:
				fail("row %s leg %s: STALE ledger entry — it says owed and %s covers the pair", row, leg, probe)
			case probed && !ledgered:
				fail("row %s leg %s: UNKNOWN pair — %s covers it and the ledger names no such pair", row, leg, probe)
			case !probed && !ledgered:
				fail("row %s leg %s: no ledger entry and no probe — neither covered nor explicitly owed", row, leg)
			case !probed && entry.state != "owed" && entry.state != "inapplicable":
				fail("row %s leg %s: the ledger says %q and no probe covers it — neither covered nor explicitly owed", row, leg, entry.state)
			}
		}
	}

	if len(failures) > 0 {
		sort.Strings(failures)
		t.Errorf("gates/row-coverage/pairs: %d row/leg pair(s) are neither covered nor explicitly owed, or carry a rejected ledger entry:\n%s",
			len(failures), strings.Join(failures, "\n"))
	}
}
