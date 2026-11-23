package docgate

// THE OWED TABLE (docs/SPEC-CONFORMANCE.md §7). A rule decided in the spec
// before its code lands is linked there to the pull request that carries its
// code and its row. This file reads that table and holds it honest, offline,
// from git and the tree alone:
//
//   - an entry that names no PR, or no id, or an id of no known kind, is
//     refused;
//   - an `owed` entry is refused once a PR it names has merged into HEAD
//     (first-parent history only: a merge commit "Merge pull request #N
//     from ..." or a squash commit ending "(#N)");
//   - an `owed` entry is refused once ANY id it owes is in the tree. Every
//     owed id is absent from main when the entry is written, so an id that
//     appears means its code landed, by whatever merge method, a rebase
//     merge that carries no PR number included.
//
// An id is found only where the repository defines that kind of id, and
// only whole: a row in a corpus registry, a Test func in a _test.go file, a
// path as a tracked file or directory.

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
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
	testIDRe   = regexp.MustCompile(`^Test[A-Za-z0-9_]+$`)
	rowIDRe    = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]*[A-Za-z0-9]$`)
	pathIDRe   = regexp.MustCompile(`^[A-Za-z0-9_.-]+(/[A-Za-z0-9_.-]+)+$`)
)

// The kinds of owed id.
const (
	KindTest = "test" // a Test func, found as "func Name(" in a _test.go file
	KindPath = "path" // a tracked file or directory, matched exactly
	KindRow  = "row"  // a corpus row, matched whole in a row registry
)

// IDKind names the kind of an owed id, or refuses it as malformed: a plain
// word that names no kind of id is never looked up.
func IDKind(id string) (string, error) {
	switch {
	case testIDRe.MatchString(id):
		return KindTest, nil
	case strings.Contains(id, "/"):
		if !pathIDRe.MatchString(id) {
			return "", fmt.Errorf("malformed id %q: a path names a file or directory, with no wildcard", id)
		}
		return KindPath, nil
	case rowIDRe.MatchString(id) && strings.ContainsAny(id, "_-") && !strings.Contains(id, "."):
		return KindRow, nil
	}
	return "", fmt.Errorf("malformed id %q: not a Test func, a path or a row id", id)
}

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
			e.IDs = append(e.IDs, m[1])
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

// CheckOwed returns one finding per entry the table can no longer claim. An
// id that is malformed is a finding, never looked up.
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
			out = append(out, where+": the entry owes no id, so a landing its PR number does not name (a rebase merge) could not be told: name a row or test the PR adds")
			continue
		}
		for _, id := range e.IDs {
			if _, err := IDKind(id); err != nil {
				out = append(out, fmt.Sprintf("%s: %v", where, err))
				continue
			}
			ok, err := present(id)
			if err != nil {
				return nil, fmt.Errorf("%s: %s: %w", where, id, err)
			}
			if ok {
				out = append(out, fmt.Sprintf("%s: %s is in the tree and the entry still says owed: its code landed, so link it", where, id))
			}
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

// MergedPRs reads every pull request merged into HEAD from the subjects of
// HEAD's FIRST-PARENT history only: a merge commit "Merge pull request #N
// from ..." and a squash commit ending "(#N)". A commit of a merged branch
// whose subject ends "(#N)" is not on the first-parent line and never
// counts. A rebase merge carries no number and is not seen here; the tree
// half of the check (any owed id present) is what catches it.
func MergedPRs(root string) (map[int]bool, error) {
	sh, err := git(root, "rev-parse", "--is-shallow-repository")
	if err != nil {
		return nil, fmt.Errorf("git rev-parse --is-shallow-repository: %w", err)
	}
	if strings.TrimSpace(sh) == "true" {
		return nil, ErrShallow
	}
	log, err := git(root, "log", "--first-parent", "--format=%s", "HEAD")
	if err != nil {
		return nil, fmt.Errorf("git log: %w", err)
	}
	return MergedFromSubjects(strings.Split(log, "\n"))
}

// MergedFromSubjects reads PR numbers out of first-parent subjects.
func MergedFromSubjects(subjects []string) (map[int]bool, error) {
	out := map[int]bool{}
	for _, s := range subjects {
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
		return nil, errors.New("the first-parent history names no merged pull request at all; it is not the repository")
	}
	return out, nil
}

// The row registries: where the repository defines a corpus row.
const (
	ManifestPath = "testdata/conformance/tables/MANIFEST.txt" // "<surface> <row> ..."
	HostilePath  = "internal/ci/hostilecorpus/corpus/manifest.tsv"
)

// Tree is the tracked files of one tree: the work tree (Ref "") or a commit.
type Tree struct {
	Root, Ref string
	paths     map[string]bool
	dirs      map[string]bool
}

// OpenTree lists the tracked files of ref, or of the work tree for "".
func OpenTree(root, ref string) (*Tree, error) {
	var out string
	var err error
	if ref == "" {
		out, err = git(root, "ls-files")
	} else {
		out, err = git(root, "ls-tree", "-r", "--name-only", ref)
	}
	if err != nil {
		return nil, fmt.Errorf("listing the tree %q: %w", ref, err)
	}
	t := &Tree{Root: root, Ref: ref, paths: map[string]bool{}, dirs: map[string]bool{}}
	for _, p := range strings.Split(strings.TrimSpace(out), "\n") {
		if p == "" {
			continue
		}
		t.paths[p] = true
		for d := filepath.ToSlash(filepath.Dir(p)); d != "." && !t.dirs[d]; d = filepath.ToSlash(filepath.Dir(d)) {
			t.dirs[d] = true
		}
	}
	if len(t.paths) == 0 {
		return nil, fmt.Errorf("the tree %q lists no file", ref)
	}
	return t, nil
}

func (t *Tree) read(p string) ([]byte, error) {
	if t.Ref == "" {
		return os.ReadFile(filepath.Join(t.Root, filepath.FromSlash(p)))
	}
	out, err := git(t.Root, "show", t.Ref+":"+p)
	return []byte(out), err
}

// HasPath says whether p is a tracked file or a directory holding one.
func (t *Tree) HasPath(p string) bool { return t.paths[p] || t.dirs[p] }

// Paths returns every tracked path, sorted.
func (t *Tree) Paths() []string {
	var out []string
	for p := range t.paths {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

// Rows returns every row id the registries define, and the surface names of
// the manifest: the manifest's second field, the hostile corpus's first, and
// the first field of every test/conformance/<surface>/rows.txt.
func (t *Tree) Rows() (rows, surfaces map[string]bool, err error) {
	rows, surfaces = map[string]bool{}, map[string]bool{}
	field := func(p string, n int, set map[string]bool, sep func(string) []string) error {
		b, err := t.read(p)
		if err != nil {
			return fmt.Errorf("reading the row registry %s: %w", p, err)
		}
		sc := bufio.NewScanner(strings.NewReader(string(b)))
		sc.Buffer(make([]byte, 1<<20), 1<<20)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" || strings.HasPrefix(l, "#") {
				continue
			}
			f := sep(l)
			if len(f) > n {
				set[f[n]] = true
				if n == 1 {
					surfaces[f[0]] = true
				}
			}
		}
		return sc.Err()
	}
	tab := func(s string) []string { return strings.Split(s, "\t") }
	if err := field(ManifestPath, 1, rows, strings.Fields); err != nil {
		return nil, nil, err
	}
	if err := field(HostilePath, 0, rows, tab); err != nil {
		return nil, nil, err
	}
	for p := range t.paths {
		if strings.HasPrefix(p, "test/conformance/") && strings.HasSuffix(p, "/rows.txt") {
			if err := field(p, 0, rows, strings.Fields); err != nil {
				return nil, nil, err
			}
		}
	}
	if len(rows) == 0 {
		return nil, nil, errors.New("the row registries define no row")
	}
	return rows, surfaces, nil
}

// TestFuncs returns which of names is declared "func Name(" at the start of
// a line of a tracked _test.go file.
func (t *Tree) TestFuncs(names []string) (map[string]bool, error) {
	out := map[string]bool{}
	if len(names) == 0 {
		return out, nil
	}
	args := []string{"-C", t.Root, "grep", "-h", "-o", "-E", "-e", `^func (` + strings.Join(names, "|") + `)\(`}
	if t.Ref != "" {
		args = append(args, t.Ref)
	}
	args = append(args, "--", "*_test.go")
	b, err := exec.Command("git", args...).Output()
	var ee *exec.ExitError
	if err != nil && !(errors.As(err, &ee) && ee.ExitCode() == 1) {
		return nil, fmt.Errorf("git grep for Test funcs: %w", err)
	}
	for _, m := range strings.Split(string(b), "\n") {
		m = strings.TrimSuffix(strings.TrimPrefix(m, "func "), "(")
		if m != "" {
			out[m] = true
		}
	}
	return out, nil
}

// Present says, for each id, whether the tree holds it by its kind. A
// malformed id is an error.
func (t *Tree) Present(ids []string) (map[string]bool, error) {
	out := map[string]bool{}
	var tests []string
	var rows map[string]bool
	for _, id := range ids {
		kind, err := IDKind(id)
		if err != nil {
			return nil, err
		}
		switch kind {
		case KindTest:
			tests = append(tests, id)
		case KindPath:
			out[id] = t.HasPath(id)
		case KindRow:
			if rows == nil {
				if rows, _, err = t.Rows(); err != nil {
					return nil, err
				}
			}
			out[id] = rows[id]
		}
	}
	found, err := t.TestFuncs(tests)
	if err != nil {
		return nil, err
	}
	for _, id := range tests {
		out[id] = found[id]
	}
	return out, nil
}
