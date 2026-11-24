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
//	<row><TAB><why>
//
// `#` starts a comment line. <row> is the row file's name without its
// extension (for C#, the row's directory name). The ledger is matched as an
// EXACT SET against the rows that fail: a failing row it does not name is
// red, and a row it names that now passes is red until its line is removed,
// so the ledger can only shrink by a change that makes a row answer. A line
// naming a row file that does not exist is red too. A ledger with no line is
// an error: delete the file when its last row comes off.

// parseLedger reads one ledger. A malformed line, a line with no reason, a
// duplicate, or a ledger naming no row is an error.
func parseLedger(r io.Reader, path string) (map[string]string, error) {
	held := map[string]string{}
	sc := bufio.NewScanner(r)
	line := 0
	for sc.Scan() {
		line++
		text := sc.Text()
		if strings.TrimSpace(text) == "" || strings.HasPrefix(text, "#") {
			continue
		}
		row, why, ok := strings.Cut(text, "\t")
		row, why = strings.TrimSpace(row), strings.TrimSpace(why)
		if !ok || why == "" || row == "" || strings.ContainsAny(row, " \t/") {
			return nil, fmt.Errorf("%s:%d: want `<row><TAB><why>`, got %q", path, line, text)
		}
		if _, dup := held[row]; dup {
			return nil, fmt.Errorf("%s:%d: %s is listed twice", path, line, row)
		}
		held[row] = why
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("%s: %w", path, err)
	}
	if len(held) == 0 {
		return nil, fmt.Errorf("%s holds no row: delete the file", path)
	}
	return held, nil
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
