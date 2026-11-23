package docgate

// THE OWED TABLE (docs/SPEC-CONFORMANCE.md §7). A rule decided in the spec
// before its code lands is linked there to the pull request that carries its
// code and its row. This file reads that table and holds it honest, offline,
// from git and the tree alone: an entry that names no PR is refused, and an
// `owed` entry is refused once a PR it names has merged into HEAD, or once
// every id its "not on main yet" column names is in the tree.

import (
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// OwedPage is the page that holds the owed table.
const OwedPage = "docs/SPEC-CONFORMANCE.md"

// OwedHeading opens the section that holds the table.
const OwedHeading = "## 7. Rules decided 2026-11-20..22, and the rows they owe"

// OwedHeader is the table header the parser requires, column for column.
const OwedHeader = "| entry | rule (spec) | held by, as decided | not on main yet | PR | state |"

// OwedEntry is one row of the owed table.
type OwedEntry struct {
	Line  int      // 1-based line in OwedPage
	Entry string   // the entry column: FL-nn, GAP-n, SF-nn, a ruling
	IDs   []string // the backticked ids of the "not on main yet" column
	PRs   []int    // every #N of the PR column
	State string   // the state column
}

var (
	owedPRRe   = regexp.MustCompile(`#([0-9]+)`)
	owedTickRe = regexp.MustCompile("`([^`]+)`")
	mergeRe    = regexp.MustCompile(`^Merge pull request #([0-9]+) from `)
	squashRe   = regexp.MustCompile(`\(#([0-9]+)\)$`)
)

// splitRow splits a markdown table row into its cells, keeping an escaped
// pipe inside a cell.
func splitRow(line string) []string {
	s := strings.TrimSpace(line)
	s = strings.TrimSuffix(strings.TrimPrefix(s, "|"), "|")
	var cells []string
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		switch {
		case s[i] == byte(92) && i+1 < len(s) && s[i+1] == byte(124):
			b.WriteString(s[i : i+2])
			i++
		case s[i] == byte(124):
			cells = append(cells, strings.TrimSpace(b.String()))
			b.Reset()
		default:
			b.WriteByte(s[i])
		}
	}
	return append(cells, strings.TrimSpace(b.String()))
}

// ParseOwed reads the owed table out of the text of OwedPage. A page with no
// such section, or a table whose header or rows are not the shape above, is
// an error naming the line: the check never passes over a table it could not
// read.
func ParseOwed(text string) ([]OwedEntry, error) {
	lines := strings.Split(text, "\n")
	at := -1
	for i, l := range lines {
		if l == OwedHeading {
			at = i
			break
		}
	}
	if at < 0 {
		return nil, fmt.Errorf("%s: no heading %q", OwedPage, OwedHeading)
	}
	hdr := -1
	for i := at + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "## ") {
			break
		}
		if strings.HasPrefix(lines[i], "| entry |") {
			hdr = i
			break
		}
	}
	if hdr < 0 {
		return nil, fmt.Errorf("%s: §7 holds no table starting %q", OwedPage, "| entry |")
	}
	if lines[hdr] != OwedHeader {
		return nil, fmt.Errorf("%s:%d: the owed table header is %q, want %q", OwedPage, hdr+1, lines[hdr], OwedHeader)
	}
	if hdr+1 >= len(lines) || !strings.HasPrefix(lines[hdr+1], "|---") {
		return nil, fmt.Errorf("%s:%d: the owed table header has no separator row", OwedPage, hdr+2)
	}
	var out []OwedEntry
	for i := hdr + 2; i < len(lines) && strings.HasPrefix(lines[i], "|"); i++ {
		c := splitRow(lines[i])
		if len(c) != 6 {
			return nil, fmt.Errorf("%s:%d: an owed row has %d cells, want 6", OwedPage, i+1, len(c))
		}
		e := OwedEntry{Line: i + 1, Entry: c[0], State: c[5]}
		for _, m := range owedTickRe.FindAllStringSubmatch(c[3], -1) {
			if !strings.ContainsAny(m[1], " \t") {
				e.IDs = append(e.IDs, m[1])
			}
		}
		for _, m := range owedPRRe.FindAllStringSubmatch(c[4], -1) {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				return nil, fmt.Errorf("%s:%d: PR %q: %v", OwedPage, i+1, m[0], err)
			}
			e.PRs = append(e.PRs, n)
		}
		out = append(out, e)
	}
	return out, nil
}

// CheckOwed returns one finding per entry the table can no longer claim:
// an entry that names no PR, an entry not in the `owed` state, an owed entry
// a named PR of which has merged, and an owed entry every id of which is
// already in the tree.
func CheckOwed(entries []OwedEntry, merged func(pr int) bool, present func(id string) (bool, error)) ([]string, error) {
	var out []string
	for _, e := range entries {
		where := fmt.Sprintf("%s:%d: %s", OwedPage, e.Line, e.Entry)
		if len(e.PRs) == 0 {
			out = append(out, where+": the entry names no PR; every owed rule names the PR that carries its code and row")
		}
		if e.State != "owed" {
			out = append(out, fmt.Sprintf("%s: state %q; an entry is owed until it is linked, and a linked entry leaves the table", where, e.State))
			continue
		}
		for _, pr := range e.PRs {
			if merged(pr) {
				out = append(out, fmt.Sprintf("%s: #%d has merged and the entry still says owed: cite its row where the rule is stated, then delete the entry or take #%d off it", where, pr, pr))
			}
		}
		if len(e.IDs) == 0 {
			continue
		}
		all := true
		for _, id := range e.IDs {
			ok, err := present(id)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", where, id, err)
			}
			if !ok {
				all = false
				break
			}
		}
		if all {
			out = append(out, where+": every id the entry owes is in the tree and the entry still says owed: link it")
		}
	}
	return out, nil
}

// ErrShallow is returned by MergedPRs in a clone without its history.
var ErrShallow = errors.New("the clone is shallow, so the merged pull requests cannot be read from its history")

func git(root string, args ...string) (string, error) {
	c := exec.Command("git", append([]string{"-C", root}, args...)...)
	b, err := c.Output()
	return string(b), err
}

// MergedPRs reads every pull request merged into HEAD from the commit
// subjects HEAD reaches: a merge commit "Merge pull request #N from ..." and
// a squash commit ending "(#N)". It needs the history and nothing else.
func MergedPRs(root string) (map[int]bool, error) {
	sh, err := git(root, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, fmt.Errorf("git rev-parse --is-shallow-repository: %w", err)
	}
	if strings.TrimSpace(sh) == "true" {
		return nil, ErrShallow
	}
	log, err := git(root, "log", "--format=%s", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	out := map[int]bool{}
	for _, s := range strings.Split(log, "\n") {
		for _, re := range []*regexp.Regexp{mergeRe, squashRe} {
			if m := re.FindStringSubmatch(s); m != nil {
				n, err := strconv.Atoi(m[1])
				if err != nil {
					return nil, err
				}
				out[n] = true
			}
		}
	}
	if len(out) == 0 {
		return nil, errors.New("git log names no merged pull request at all; the history is not the repository")
	}
	return out, nil
}

// TreeIDs says, for each owed id, whether it is in the tracked tree outside
// docs/, by the rules of TreeHas, in one pass of git grep and one listing.
func TreeIDs(root string, ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	pat := map[string]string{} // grep text -> id
	var paths []string
	for _, id := range ids {
		switch {
		case strings.HasPrefix(id, "Test"):
			pat["func "+strings.SplitN(id, "/", 2)[0]+"("] = id
		case strings.Contains(id, "/"):
			paths = append(paths, id)
		default:
			pat[strings.TrimSuffix(id, "*")] = id
		}
	}
	if len(paths) > 0 {
		files, err := git(root, "ls-files")
		if err != nil {
			return nil, fmt.Errorf("git ls-files: %w", err)
		}
		for _, p := range paths {
			out[p] = strings.Contains(files, strings.TrimSuffix(p, "/"))
		}
	}
	if len(pat) == 0 {
		return out, nil
	}
	args := []string{"-C", root, "grep", "-h", "-o", "-F"}
	for p := range pat {
		args = append(args, "-e", p)
		out[pat[p]] = false
	}
	args = append(args, "--", ".", ":!docs")
	b, err := exec.Command("git", args...).Output()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
		return nil, fmt.Errorf("git grep: %w", err)
	}
	for _, m := range strings.Split(string(b), "\n") {
		if id, ok := pat[m]; ok {
			out[id] = true
		}
	}
	return out, nil
}

// TreeHas says whether an owed id is in the tracked tree outside docs/: a
// path-shaped id as part of a tracked path, a TestName as its func, a
// trailing-star id as its prefix, anything else as text.
func TreeHas(root, id string) (bool, error) {
	switch {
	case strings.HasPrefix(id, "Test"):
		name := strings.SplitN(id, "/", 2)[0]
		return gitGrep(root, "func "+name+"(")
	case strings.Contains(id, "/"):
		files, err := git(root, "ls-files")
		if err != nil {
			return false, fmt.Errorf("git ls-files: %w", err)
		}
		return strings.Contains(files, strings.TrimSuffix(id, "/")), nil
	default:
		return gitGrep(root, strings.TrimSuffix(id, "*"))
	}
}

func gitGrep(root, text string) (bool, error) {
	c := exec.Command("git", "-C", root, "grep", "-q", "-F", "-e", text, "--", ".", ":!docs")
	err := c.Run()
	if err == nil {
		return true, nil
	}
	var ee *exec.ExitError
	if errors.As(err, &ee) && ee.ExitCode() == 1 {
		return false, nil
	}
	return false, fmt.Errorf("git grep %q: %w", text, err)
}
