package main

import (
	"bufio"
	"fmt"
	"io"
	"sort"
	"strings"
)

// THE OWED-ROW LEDGER. test/conformance/<leg>/rows-owed.tsv names, one line
// each, the row files of that leg's rows directory the leg cannot answer
// today, and why:
//
//	<row><TAB>owed: <cause>[<TAB><note>]
//
// `#` starts a comment line. <row> is the row file's name without its
// extension (for C#, the row's directory name). <cause> is the cause the run
// prints for the row, character for character: the runner compares the two
// and a mismatch is red, naming the row and both causes, so a ledger line can
// never describe a failure the row no longer has. <note>, optional, is what a
// person adds (whether the leg or the row is wrong, and the spec line); the
// runner keeps it and never compares it.
//
// The ledger is matched as an EXACT SET against the rows that fail: a failing
// row it does not name is red, and a row it names that now passes is red
// until its line is removed, so the ledger can only shrink by a change that
// makes a row answer. A line naming a row file that does not exist is red
// too. A ledger with no line is an error: delete the file when its last row
// comes off.

// parseLedger reads one ledger: each row's cause column, and the notes. A
// malformed line, a line with no cause, an empty note, a duplicate, or a
// ledger naming no row is an error.
func parseLedger(r io.Reader, path string) (causes, notes map[string]string, err error) {
	causes, notes = map[string]string{}, map[string]string{}
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		fields := strings.Split(text, "\t")
		row := strings.TrimSpace(fields[0])
		why, note := "", ""
		if len(fields) > 1 {
			why = strings.TrimSpace(fields[1])
		}
		if len(fields) > 2 {
			note = strings.TrimSpace(fields[2])
		}
		if len(fields) < 2 || len(fields) > 3 || why == "" || (len(fields) == 3 && note == "") || row == "" || strings.ContainsAny(row, " /") {
			return nil, nil, fmt.Errorf("%s:%d: want `<row><TAB>owed: <cause>[<TAB><note>]`, got %q", path, line, text)
		}
		if _, dup := causes[row]; dup {
			return nil, nil, fmt.Errorf("%s:%d: %s is listed twice", path, line, row)
		}
		causes[row] = plainQuotes(why)
		if note != "" {
			notes[row] = note
		}
	}
	if err := sc.Err(); err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(causes) == 0 {
		return nil, nil, fmt.Errorf("%s holds no row: delete the file", path)
	}
	return causes, notes, nil
}

// causeMismatches compares each owed row that ran and failed with the cause
// its ledger line states: the line must be `owed: ` and the cause the run
// printed, exactly. Each mismatch names the row and both causes.
func causeMismatches(ran []string, failed map[string]bool, ledger, causes map[string]string, ledgerPath string) []string {
	var out []string
	for _, r := range ran {
		held, owed := ledger[r]
		if !owed || !failed[r] {
			continue
		}
		if want := "owed: " + causes[r]; held != want {
			out = append(out, fmt.Sprintf("%s: %s says %q, the run printed %q", r, ledgerPath, held, want))
		}
	}
	sort.Strings(out)
	return out
}

// verdict holds the rows that ran against the ledger. all is every row file
// of the leg (shard or not), ran the rows this run executed, failed the ones
// of those that failed. Ledger lines for rows outside ran are not judged
// (another shard runs them), except that a line naming no row file at all
// is red in every shard.
func verdict(all, ran []string, failed map[string]bool, ledger map[string]string, ledgerPath string) error {
	exists := map[string]bool{}
	for _, r := range all {
		exists[r] = true
	}
	inRun := map[string]bool{}
	for _, r := range ran {
		inRun[r] = true
	}
	var unlisted, stale, unknown []string
	for _, r := range ran {
		_, owed := ledger[r]
		switch {
		case failed[r] && !owed:
			unlisted = append(unlisted, r)
		case !failed[r] && owed:
			stale = append(stale, r)
		}
	}
	for r := range ledger {
		if !exists[r] {
			unknown = append(unknown, r)
		}
	}
	sort.Strings(unlisted)
	sort.Strings(stale)
	sort.Strings(unknown)
	var msg []string
	if len(unlisted) > 0 {
		where := "a row that cannot be answered is a failure until it is ledgered"
		if ledger != nil {
			where = "not in " + ledgerPath
		}
		msg = append(msg, fmt.Sprintf("%d rows failed (%s): %s", len(unlisted), where, strings.Join(unlisted, ", ")))
	}
	if len(stale) > 0 {
		msg = append(msg, fmt.Sprintf("%s lists %d rows that now pass; remove them: %s", ledgerPath, len(stale), strings.Join(stale, ", ")))
	}
	if len(unknown) > 0 {
		msg = append(msg, fmt.Sprintf("%s lists %d rows with no row file: %s", ledgerPath, len(unknown), strings.Join(unknown, ", ")))
	}
	if len(msg) > 0 {
		return fmt.Errorf("%s", strings.Join(msg, "\n"))
	}
	return nil
}

// shardOf returns the rows of shard i of n: the sorted rows, every n-th from
// i. Every row is in exactly one shard.
func shardOf(rows []string, i, n int) []string {
	sorted := append([]string(nil), rows...)
	sort.Strings(sorted)
	var out []string
	for k, r := range sorted {
		if k%n == i {
			out = append(out, r)
		}
	}
	return out
}
