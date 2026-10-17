// Package jsontestsuite holds schema's JSON readers to JSONTestSuite, the
// outside oracle docs/SPEC-TABLES.md §16.11 rules over. It reads the policy
// table out of the standard itself (the one source; no test carries a copy),
// checks every vendored file against SHA256SUMS, and compares a reader's
// verdicts with the table's. Standard library only.
package jsontestsuite

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// The paths under the repository root, as §16.11 names them.
const (
	SpecPath    = "docs/SPEC-TABLES.md"
	CorpusDir   = "testdata/jsontestsuite/test_parsing"
	SumsPath    = "testdata/jsontestsuite/SHA256SUMS"
	PendingPath = "testdata/jsontestsuite/PENDING.tsv"
)

const (
	sectionHeading = "### 16.11 "
	verdictAccept  = "accept"
	verdictDialect = "accept (dialect)"
	verdictRefuse  = "refuse"
)

// Row is one line of §16.11's table.
type Row struct {
	File    string
	Accept  bool
	Dialect bool // `accept (dialect)`: an n_ file the §16.2 dialect accepts
	Reason  string
}

// Suite is the corpus and its policy, resolved: every file's expected verdict.
type Suite struct {
	Files   map[string][]byte // file name -> its bytes, every one checked against SHA256SUMS
	Rows    map[string]Row    // §16.11's table, keyed by file
	Pending map[string]string // file -> the card that meets it (PENDING.tsv)
	Accept  map[string]bool   // file -> the expected verdict, true = accepted
}

// Names lists the corpus files in byte order.
func (s *Suite) Names() []string {
	names := make([]string, 0, len(s.Files))
	for n := range s.Files {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// Load reads the policy table out of docs/SPEC-TABLES.md §16.11, every corpus
// file (each checked against SHA256SUMS) and PENDING.tsv under root, and
// resolves each file's expected verdict.
func Load(root string) (*Suite, error) {
	spec, err := os.ReadFile(filepath.Join(root, SpecPath))
	if err != nil {
		return nil, err
	}
	rows, err := ParseTable(spec)
	if err != nil {
		return nil, err
	}
	files, err := readCorpus(root)
	if err != nil {
		return nil, err
	}
	pendingText, err := os.ReadFile(filepath.Join(root, PendingPath))
	if err != nil {
		return nil, err
	}
	pending, err := ParsePending(pendingText)
	if err != nil {
		return nil, err
	}
	return Resolve(files, rows, pending)
}

// readCorpus reads every file of the corpus directory and refuses any byte
// SHA256SUMS does not pin: a file missing from the sums, a sum naming no file,
// and a sum that does not match each name the file.
func readCorpus(root string) (map[string][]byte, error) {
	dir := filepath.Join(root, CorpusDir)
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}
	files := map[string][]byte{}
	for _, e := range entries {
		if e.IsDir() {
			return nil, fmt.Errorf("jsontestsuite: %s: a directory in the corpus", e.Name())
		}
		data, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return nil, err
		}
		files[e.Name()] = data
	}
	sums, err := os.ReadFile(filepath.Join(root, SumsPath))
	if err != nil {
		return nil, err
	}
	want := map[string]string{}
	sc := bufio.NewScanner(bytes.NewReader(sums))
	for sc.Scan() {
		line := sc.Text()
		sum, name, ok := strings.Cut(line, "  ")
		if !ok || len(sum) != 64 {
			return nil, fmt.Errorf("jsontestsuite: SHA256SUMS: not a sum line: %q", line)
		}
		want[name] = sum
	}
	var errs []error
	for name, data := range files {
		sum, ok := want[name]
		if !ok {
			errs = append(errs, fmt.Errorf("%s: not in SHA256SUMS", name))
			continue
		}
		got := sha256.Sum256(data)
		if hex.EncodeToString(got[:]) != sum {
			errs = append(errs, fmt.Errorf("%s: sha256 does not match SHA256SUMS", name))
		}
	}
	for name := range want {
		if _, ok := files[name]; !ok {
			errs = append(errs, fmt.Errorf("%s: in SHA256SUMS but not in the corpus", name))
		}
	}
	if len(errs) > 0 {
		return nil, joined(errs)
	}
	return files, nil
}

// ParseTable reads §16.11's table out of the standard's text: the rows of the
// first markdown table after the `### 16.11` heading and before the next
// heading, each `| `file` | verdict | reason |`.
func ParseTable(spec []byte) (map[string]Row, error) {
	text := string(spec)
	start := strings.Index(text, "\n"+sectionHeading)
	if start < 0 {
		return nil, errors.New("jsontestsuite: docs/SPEC-TABLES.md has no §16.11")
	}
	body := text[start+1:]
	if next := strings.Index(body[len(sectionHeading):], "\n#"); next >= 0 {
		body = body[:len(sectionHeading)+next]
	}
	rows := map[string]Row{}
	seenHeader := false
	for _, line := range strings.Split(body, "\n") {
		if !strings.HasPrefix(line, "|") {
			continue
		}
		cells := strings.Split(strings.Trim(line, "|"), "|")
		for i := range cells {
			cells[i] = strings.TrimSpace(cells[i])
		}
		if !seenHeader {
			if len(cells) != 3 || cells[0] != "file" || cells[1] != "verdict" || cells[2] != "reason" {
				return nil, fmt.Errorf("jsontestsuite: §16.11's table header is not | file | verdict | reason |: %q", line)
			}
			seenHeader = true
			continue
		}
		if strings.HasPrefix(cells[0], "---") {
			continue
		}
		// a reason may itself carry a `|` only inside backticks, which the
		// table never does; three cells is the shape
		if len(cells) != 3 {
			return nil, fmt.Errorf("jsontestsuite: §16.11 row is not three cells: %q", line)
		}
		file := strings.Trim(cells[0], "`")
		if file == cells[0] || file == "" {
			return nil, fmt.Errorf("jsontestsuite: §16.11 row's file is not one backticked name: %q", line)
		}
		row := Row{File: file, Reason: cells[2]}
		switch cells[1] {
		case verdictAccept:
			row.Accept = true
		case verdictDialect:
			row.Accept, row.Dialect = true, true
		case verdictRefuse:
		default:
			return nil, fmt.Errorf("jsontestsuite: %s: verdict %q is not accept, accept (dialect) or refuse", file, cells[1])
		}
		if row.Reason == "" {
			return nil, fmt.Errorf("jsontestsuite: %s: a row with no reason", file)
		}
		if row.Dialect && !strings.HasPrefix(file, "n_") {
			return nil, fmt.Errorf("jsontestsuite: %s: accept (dialect) is for n_ files only", file)
		}
		if _, dup := rows[file]; dup {
			return nil, fmt.Errorf("jsontestsuite: %s: two rows", file)
		}
		rows[file] = row
	}
	if !seenHeader {
		return nil, errors.New("jsontestsuite: §16.11 has no policy table")
	}
	return rows, nil
}

// ParsePending reads PENDING.tsv: `<file>\t<card id>` per line.
func ParsePending(text []byte) (map[string]string, error) {
	pending := map[string]string{}
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" {
			continue
		}
		file, card, ok := strings.Cut(line, "\t")
		if !ok || file == "" || card == "" || strings.Contains(card, "\t") {
			return nil, fmt.Errorf("jsontestsuite: PENDING.tsv line %d is not <file>\\t<card>: %q", i+1, line)
		}
		if _, dup := pending[file]; dup {
			return nil, fmt.Errorf("jsontestsuite: PENDING.tsv: %s: two lines", file)
		}
		pending[file] = card
	}
	return pending, nil
}

// Resolve gives every file its expected verdict (docs/SPEC-TABLES.md §16.11):
// its row, else accept for y_ and refuse for n_. An i_ file with no row, a row
// naming no file, a file of no known prefix and a PENDING line naming no file
// are errors, each naming the file.
func Resolve(files map[string][]byte, rows map[string]Row, pending map[string]string) (*Suite, error) {
	s := &Suite{Files: files, Rows: rows, Pending: pending, Accept: map[string]bool{}}
	var errs []error
	for name := range files {
		if row, ok := rows[name]; ok {
			s.Accept[name] = row.Accept
			continue
		}
		switch {
		case strings.HasPrefix(name, "y_"):
			s.Accept[name] = true
		case strings.HasPrefix(name, "n_"):
			s.Accept[name] = false
		case strings.HasPrefix(name, "i_"):
			errs = append(errs, fmt.Errorf("%s: an i_ file with no row in §16.11", name))
		default:
			errs = append(errs, fmt.Errorf("%s: not a y_, n_ or i_ file", name))
		}
	}
	for name := range rows {
		if _, ok := files[name]; !ok {
			errs = append(errs, fmt.Errorf("%s: a §16.11 row naming no file", name))
		}
	}
	for name := range pending {
		if _, ok := files[name]; !ok {
			errs = append(errs, fmt.Errorf("%s: a PENDING line naming no file", name))
		}
	}
	if len(errs) > 0 {
		return nil, joined(errs)
	}
	return s, nil
}

// Check compares a reader's verdicts with the suite's. It returns the
// mismatches, each naming its file, and the pending files that disagree as
// PENDING allows. A pending file that AGREES is a mismatch, "met: remove it
// from PENDING", so the list cannot outlive the gap it names; a file the
// reader gave no verdict for, or one the suite does not hold, is a mismatch.
func Check(s *Suite, got map[string]bool) (mismatches, pending []string) {
	for _, name := range s.Names() {
		want := s.Accept[name]
		have, ok := got[name]
		if !ok {
			mismatches = append(mismatches, name+": no verdict from the reader")
			continue
		}
		card, isPending := s.Pending[name]
		switch {
		case have == want && isPending:
			mismatches = append(mismatches, fmt.Sprintf("%s: %s as §16.11 rules; met: remove it from PENDING (%s)", name, word(have), card))
		case have != want && isPending:
			pending = append(pending, fmt.Sprintf("%s: want %s, got %s; pending %s", name, word(want), word(have), card))
		case have != want:
			mismatches = append(mismatches, fmt.Sprintf("%s: want %s, got %s", name, word(want), word(have)))
		}
	}
	var extra []string
	for name := range got {
		if _, ok := s.Files[name]; !ok {
			extra = append(extra, name+": a verdict for a file the suite does not hold")
		}
	}
	sort.Strings(extra)
	return append(mismatches, extra...), pending
}

// Clone copies a suite's tables so a witness can edit them; the file bytes are
// shared, read-only.
func (s *Suite) Clone() *Suite {
	c := &Suite{Files: s.Files, Rows: map[string]Row{}, Pending: map[string]string{}, Accept: map[string]bool{}}
	for k, v := range s.Rows {
		c.Rows[k] = v
	}
	for k, v := range s.Pending {
		c.Pending[k] = v
	}
	for k, v := range s.Accept {
		c.Accept[k] = v
	}
	return c
}

func word(accept bool) string {
	if accept {
		return "accept"
	}
	return "refuse"
}

func joined(errs []error) error {
	msgs := make([]string, len(errs))
	for i, e := range errs {
		msgs[i] = e.Error()
	}
	sort.Strings(msgs)
	return errors.New("jsontestsuite: " + strings.Join(msgs, "; "))
}
