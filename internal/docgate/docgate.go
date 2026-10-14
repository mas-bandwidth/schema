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
	"sort"
	"strings"
	"unicode"
)

// Problem is one reference that does not resolve.
type Problem struct {
	File   string // repo-relative slash path of the file holding the reference
	Line   int    // 1-based line within File
	Ref    string // the reference text as written
	Reason string // link-file, link-anchor, cite-page, or cite-heading
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
	citeRe    = regexp.MustCompile(`([A-Za-z0-9_][A-Za-z0-9_./-]*\.md)[ \t]*§[ \t]*(\d+(?:\.\d+)*)`)
	linkRe    = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)
	inlineRe  = regexp.MustCompile("`[^`]*`")
	schemeRe  = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9+.-]*:`)
)

// skipDir names directories that are not part of the tracked tree.
var skipDir = map[string]bool{
	".git": true, "build": true, "bin": true, "node_modules": true,
	"obj": true, "__pycache__": true,
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

// Check walks root and returns every reference that does not resolve, sorted.
func Check(root string) ([]Problem, error) {
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
			if err != nil || isBinary(data) {
				return nil
			}
			pg := &page{slugs: map[string]bool{}, numbers: map[string]bool{}}
			for _, line := range strings.Split(string(data), "\n") {
				m := headingRe.FindStringSubmatch(strings.TrimRight(line, "\r"))
				if m == nil {
					continue
				}
				pg.slugs[slugify(m[1])] = true
				if n := numRe.FindStringSubmatch(m[1]); n != nil {
					pg.numbers[n[1]] = true
				}
			}
			pages[rel] = pg
			byBase[path.Base(rel)] = append(byBase[path.Base(rel)], rel)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Strings(files)

	var problems []Problem
	for _, rel := range files {
		data, err := os.ReadFile(filepath.Join(root, rel))
		if err != nil || isBinary(data) {
			continue
		}
		text := string(data)
		if _, ok := pages[rel]; ok {
			problems = append(problems, checkLinks(rel, text, root, pages)...)
		}
		problems = append(problems, checkCitations(rel, text, pages, byBase)...)
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
	return problems, nil
}

func isBinary(data []byte) bool {
	for _, b := range data {
		if b == 0 {
			return true
		}
	}
	return false
}

// checkLinks scans one page line by line, skipping fenced code blocks so a Go
// call like errors.AsType[T](err) is not read as a markdown link.
func checkLinks(rel, text, root string, pages map[string]*page) []Problem {
	var problems []Problem
	inFence := false
	for i, raw := range strings.Split(text, "\n") {
		line := strings.TrimRight(raw, "\r")
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "```") || strings.HasPrefix(trimmed, "~~~") {
			inFence = !inFence
			continue
		}
		if inFence {
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
		}
	}
	return problems
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

func checkCitations(rel, text string, pages map[string]*page, byBase map[string][]string) []Problem {
	var problems []Problem
	for i, raw := range strings.Split(text, "\n") {
		for _, m := range citeRe.FindAllStringSubmatch(raw, -1) {
			pg := resolvePage(m[1], pages, byBase)
			switch {
			case pg == nil:
				problems = append(problems, Problem{File: rel, Line: i + 1, Ref: m[0], Reason: "cite-page"})
			case !pg.numbers[m[2]]:
				problems = append(problems, Problem{File: rel, Line: i + 1, Ref: m[0], Reason: "cite-heading"})
			}
		}
	}
	return problems
}

// resolvePage maps a citation's page token to a page. A token may be the
// repo-relative path, a path whose directory the writer dropped, or a basename
// that names exactly one page (bench/BENCH-STANDARD.md is cited bare).
func resolvePage(tok string, pages map[string]*page, byBase map[string][]string) *page {
	if pg, ok := pages[tok]; ok {
		return pg
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
