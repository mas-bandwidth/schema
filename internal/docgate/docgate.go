// Package docgate is the standing link-and-citation gate over the markdown
// pages (schema#337). It resolves two kinds of reference:
//
//   - every relative markdown link in a page to a file on disk and, where the
//     link carries an anchor, to the GitHub heading slug that anchor names;
//   - every "PAGE.md §N.M" citation found anywhere in the tree to a numbered
//     heading in that page.
//
// The resolver was scratch work when the docs/ move was proved (#336); this is
// the standing form. It reads the tree and returns findings; it writes nothing.
package docgate

import (
	"fmt"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"runtime"
	"slices"
	"sort"
	"strings"
	"sync"
	"unicode"
)

// Problem is one reference that does not resolve.
type Problem struct {
	File   string // repo-relative slash path of the file holding the reference
	Line   int    // 1-based line within File
	Ref    string // the reference text as written
	Reason string // link-file, link-anchor, cite-page, cite-heading, or fence-unclosed
}

func (p Problem) String() string {
	return fmt.Sprintf("%s:%d: %s: %s", p.File, p.Line, p.Reason, p.Ref)
}

// page is one markdown page and the anchors it offers.
type page struct {
	slugs   map[string]bool // GitHub heading slugs
	numbers map[string]bool // numbered section headings, "§" stripped
}

var (
	headingRe = regexp.MustCompile(`^#{1,6}[ \t]+(.*?)[ \t]*$`)
	numRe     = regexp.MustCompile(`^§?(\d+(?:\.\d+)*)[.\s:]`)
	itemRe    = regexp.MustCompile(`^(\d+)\.[ \t]`)
	citeRe    = regexp.MustCompile(`([A-Za-z0-9_][A-Za-z0-9_./-]*\.md)[ \t]*§[ \t]*(\d+(?:\.\d+)*)`)
	linkRe    = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	inlineRe  = regexp.MustCompile("`[^`]*`")
	schemeRe  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

// crossDocNumbers lists section numbers cited on a fixed-form page that live in
// its companion specification page (or as numbered rulings/lines inside it).
var crossDocNumbers = map[string][]string{
	"docs/FIXED-FORM-VERSIONING-TESTS.md": {"5.7", "5.8"},
	"docs/FIXED-FORM-ALGORITHM.md":        {"2.1", "2.2", "3.4", "9", "11", "42", "939"},
	"docs/SPEC-TABLES.md":                 {"4.3", "4.12", "5.3"},
}

// skipDir names directories whose pages are not this tree's. Most are not
// part of the tracked tree: dist is where the pinned toolchains unpack
// (`make dist-goose`; /dist in .gitignore), and their own pages link files the
// release does not ship. third_party is tracked, but each directory in it is a
// sibling repository staged as it will stand on its own (serialize.py,
// docs/PORT-PYTHON.md), and the family pages it vendors verbatim link files of
// the repository they came from.
var skipDir = map[string]bool{
	".git": true, "build": true, "bin": true, "dist": true, "node_modules": true,
	"obj": true, "__pycache__": true, "third_party": true,
}

// slugify reproduces GitHub's heading anchor: lowercase, drop everything that
// is not a letter, a digit, an underscore or a hyphen, then turn each space
// into a hyphen. Punctuation removal can leave adjacent spaces, and each one
// becomes its own hyphen — the em-dash headings in these pages rely on it.
func slugify(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		switch {
		case unicode.IsLetter(r) || unicode.IsDigit(r) || r == '_' || r == '-':
			b.WriteRune(r)
		case unicode.IsSpace(r):
			b.WriteByte(' ')
		}
	}
	return strings.ReplaceAll(b.String(), " ", "-")
}

// FindRoot walks up from dir until it finds the module root (the directory
// holding go.mod).
func FindRoot(dir string) (string, error) {
	abs, err := filepath.Abs(dir)
	if err != nil {
		return "", err
	}
	for {
		if _, err := os.Stat(filepath.Join(abs, "go.mod")); err == nil {
			return abs, nil
		}
		parent := filepath.Dir(abs)
		if parent == abs {
			return "", fmt.Errorf("no go.mod above %s", dir)
		}
		abs = parent
	}
}

// Counts is what one Check examined. A gate that found no problem among no
// pages, no links or no citations has checked nothing, so the caller holds
// these above zero.
type Counts struct {
	Pages     int // markdown pages read
	Links     int // relative markdown links resolved
	Citations int // "PAGE.md §N" citations resolved
}

// Check walks root and returns every reference that does not resolve, sorted.
func Check(root string) ([]Problem, error) {
	problems, _, err := CheckCounted(root)
	return problems, err
}

// CheckCounted is Check with the counts of what it examined.
func CheckCounted(root string) ([]Problem, Counts, error) {
	var counts Counts
	pages := map[string]*page{}
	byBase := map[string][]string{}
	var files []string

	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != root && skipDir[d.Name()] {
				return fs.SkipDir
			}
			return nil
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		files = append(files, rel)
		if strings.HasSuffix(rel, ".md") {
			data, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			if isBinary(data) {
				return nil
			}
			pg := &page{slugs: map[string]bool{}, numbers: map[string]bool{}}
			for _, extra := range crossDocNumbers[rel] {
				pg.numbers[extra] = true
			}
			var curSec string
			for line := range strings.SplitSeq(string(data), "\n") {
				trimmed := strings.TrimRight(line, "\r")
				if m := headingRe.FindStringSubmatch(trimmed); m != nil {
					pg.slugs[slugify(m[1])] = true
					if n := numRe.FindStringSubmatch(m[1]); n != nil {
						pg.numbers[n[1]] = true
						curSec = n[1]
					} else {
						curSec = ""
					}
					continue
				}
				if curSec != "" {
					if im := itemRe.FindStringSubmatch(trimmed); im != nil {
						pg.numbers[curSec+"."+im[1]] = true
					}
				}
			}
			pages[rel] = pg
			byBase[path.Base(rel)] = append(byBase[path.Base(rel)], rel)
		}
		return nil
	})
	if err != nil {
		return nil, counts, err
	}
	sort.Strings(files)
	counts.Pages = len(pages)

	// Every file is read and checked on its own, so the files are spread
	// over one worker per CPU; pages and byBase are only read here. The
	// results are gathered per file and summed in file order, then sorted
	// below, so the output is the serial walk exactly.
	type result struct {
		problems     []Problem
		links, cites int
		err          error
	}
	results := make([]result, len(files))
	next := make(chan int)
	var wg sync.WaitGroup
	for w := 0; w < runtime.GOMAXPROCS(0); w++ {
		wg.Go(func() {
			for i := range next {
				rel := files[i]
				data, err := os.ReadFile(filepath.Join(root, rel))
				if err != nil {
					results[i].err = err
					continue
				}
				if isBinary(data) {
					continue
				}
				text := string(data)
				if _, ok := pages[rel]; ok {
					found, n := checkLinks(rel, text, root, pages)
					results[i].problems = append(results[i].problems, found...)
					results[i].links = n
				}
				found, n := checkCitations(rel, text, pages, byBase)
				results[i].problems = append(results[i].problems, found...)
				results[i].cites = n
			}
		})
	}
	for i := range files {
		next <- i
	}
	close(next)
	wg.Wait()
	var problems []Problem
	for _, r := range results {
		if r.err != nil {
			return nil, counts, r.err
		}
		problems = append(problems, r.problems...)
		counts.Links += r.links
		counts.Citations += r.cites
	}
	sort.Slice(problems, func(i, j int) bool {
		if problems[i].File != problems[j].File {
			return problems[i].File < problems[j].File
		}
		if problems[i].Line != problems[j].Line {
			return problems[i].Line < problems[j].Line
		}
		return problems[i].Ref < problems[j].Ref
	})
	return problems, counts, nil
}

func isBinary(data []byte) bool {
	return slices.Contains(data, 0)
}

// fenceRun is the run of three or more backticks or tildes a line opens with,
// and what follows it. A line that opens with no such run returns "".
func fenceRun(trimmed string) (run, rest string) {
	if trimmed == "" || (trimmed[0] != '`' && trimmed[0] != '~') {
		return "", ""
	}
	n := 0
	for n < len(trimmed) && trimmed[n] == trimmed[0] {
		n++
	}
	if n < 3 {
		return "", ""
	}
	return trimmed[:n], trimmed[n:]
}

// checkLinks scans one page line by line, skipping fenced code blocks so a Go
// call like errors.AsType[T](err) is not read as a markdown link. It returns
// the problems and the count of relative links it resolved.
//
// A fence closes on a run of the same character at least as long as the one
// that opened it, with nothing after it (CommonMark), so a four-backtick block
// that shows a three-backtick fence is one block. A fence still open at the
// end of the page is a problem by name: every link after it went unread, and
// the old toggle on any fence-looking line let that pass in silence.
func checkLinks(rel, text, root string, pages map[string]*page) ([]Problem, int) {
	var problems []Problem
	links := 0
	open, openLine := "", 0
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		run, rest := fenceRun(trimmed)
		if open != "" {
			if run != "" && run[0] == open[0] && len(run) >= len(open) && strings.TrimSpace(rest) == "" {
				open = ""
			}
			continue
		}
		if run != "" {
			open, openLine = run, i+1
			continue
		}
		line = inlineRe.ReplaceAllString(line, "")
		for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
			target := m[1]
			if schemeRe.MatchString(target) {
				continue
			}
			ref := m[0][2 : len(m[0])-1]
			problems = append(problems, resolveLink(rel, i+1, ref, target, root, pages)...)
			links++
		}
	}
	if open != "" {
		problems = append(problems, Problem{File: rel, Line: openLine, Ref: open, Reason: "fence-unclosed"})
	}
	return problems, links
}

func resolveLink(rel string, line int, ref, target, root string, pages map[string]*page) []Problem {
	loc, anchor, _ := strings.Cut(target, "#")
	var at string
	if loc == "" {
		at = rel
	} else {
		at = path.Clean(path.Join(path.Dir(rel), loc))
		if _, err := os.Stat(filepath.Join(root, filepath.FromSlash(at))); err != nil {
			return []Problem{{File: rel, Line: line, Ref: ref, Reason: "link-file"}}
		}
	}
	if anchor == "" {
		return nil
	}
	pg, ok := pages[at]
	if !ok || !pg.slugs[strings.ToLower(anchor)] {
		return []Problem{{File: rel, Line: line, Ref: ref, Reason: "link-anchor"}}
	}
	return nil
}

func checkCitations(rel, text string, pages map[string]*page, byBase map[string][]string) ([]Problem, int) {
	var problems []Problem
	cites := 0
	for i, raw := range strings.Split(text, "\n") {
		for _, m := range citeRe.FindAllStringSubmatch(raw, -1) {
			cites++
			pg := resolvePage(m[1], pages, byBase)
			switch {
			case pg == nil:
				problems = append(problems, Problem{File: rel, Line: i + 1, Ref: m[0], Reason: "cite-page"})
			case !pg.numbers[m[2]]:
				problems = append(problems, Problem{File: rel, Line: i + 1, Ref: m[0], Reason: "cite-heading"})
			}
		}
	}
	return problems, cites
}

// resolvePage maps a citation's page token to a page. A token may be the
// repo-relative path, a path whose directory the writer dropped, or a basename
// that names exactly one page (bench/BENCH-STANDARD.md is cited bare).
func resolvePage(tok string, pages map[string]*page, byBase map[string][]string) *page {
	if pg, ok := pages[tok]; ok {
		return pg
	}
	if tok == "ALGORITHM.md" {
		if pg, ok := pages["docs/FIXED-FORM-ALGORITHM.md"]; ok {
			return pg
		}
	}
	for _, prefix := range []string{"docs/", "bench/"} {
		if pg, ok := pages[prefix+tok]; ok {
			return pg
		}
	}
	if list := byBase[path.Base(tok)]; len(list) == 1 {
		return pages[list[0]]
	}
	return nil
}
