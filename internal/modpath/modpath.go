// Package modpath moves this repository's Go module path: it rewrites every
// spelling of one module path (From) to another (To) across the tree, and it
// checks that no spelling of From is left. tools/modpath is the command that
// runs it; internal/ci/modpath_test.go is the guard that keeps the old path
// from coming back.
//
// A module path is spelled three ways in this tree, and each is matched only
// as a whole path element, so `serialize.go/v2`, `serialize/v2` and a
// `schema/v20` never match:
//
//   - the full path, `github.com/<owner>/<repo>/vN`;
//   - the path without its host, `<owner>/<repo>/vN`;
//   - its last two elements, `<repo>/vN`, only when the last element is a
//     major-version suffix and nothing path-like stands before it.
//
// A spelling is followed by a subpath (`/cmd/schema`), an `@version`, a
// quote, a space, punctuation or the end of the line; anything that would
// continue the element (a letter, a digit, `_`, or `.`/`-` before a letter
// or digit) is not a match. Neither path may be the other with more elements
// after it (New refuses `x/example` to `x/example/v2`): the second run would
// rewrite the new path again.
//
// A version written after the path must carry the new path's major. In a
// go.mod line, and in every Go string or make recipe that writes one
// (`<path> v2.0.0`), a version whose major does not match becomes the zero
// pseudo-version of the new major (`v3.0.0-00010101000000-000000000000`; for
// a To with no major suffix, such as `github.com/mas-bandwidth/example`,
// `v0.0.0-00010101000000-000000000000`, and v0 and v1 versions fit), but only
// when the same file also writes a `replace` for the module: a replaced
// require never uses its version, and the zero pseudo-version is what the go
// command itself writes for one. Without a replace the version is a real pin,
// so the line is left as it is and reported ("a real pinned version; decide
// by hand"), which fails the run and the check. In a Markdown page a release
// pin (`@v2.5.0`) becomes the literal `vX.Y.Z`, because the first release
// under the new path is not numbered yet. A promotion pin
// (`@main-0123456789ab`, `@main`) keeps its form. An `@vN.x.y` pin with the
// wrong major outside Markdown is not rewritten: it is reported, because it
// names a release that would have to be chosen.
//
// In a go.sum, a line for From is removed when the go.mod beside it replaces
// the module (its hash is for the old path and a replaced module needs no
// go.sum line); without that replace the line is left and reported.
//
// What is walked is the repository, not the filesystem: the root is resolved
// through symlinks; a directory that the .gitignore files ignore (git's
// rules, read from every .gitignore on the way down) is skipped, and so is
// any directory below the root that holds a `.git` entry (a nested checkout
// or worktree). A run that does not see the root go.mod, or reads fewer than
// a floor number of files, fails instead of reporting a vacuous clean.
//
// The allowlist (Allowlist) is the records that state what ran under the old
// path; they keep their text. Exceptions is a short list of exact lines in
// live pages that name the old path on purpose.
//
// Not covered, by design; a later move handles these by hand:
//
//   - the module root written without its major, as a prefix:
//     internal/goname/respell.go:18 (`ownModule`),
//     tools/slowgatescan/main.go:1040, tools/slowgatescan/selection_test.go:39;
//   - the major and a subpath written without the repository name before
//     them, as a regex or filter fragment (`/vN/compiler$` in
//     tools/slowgatescan/shards_test.go);
//   - repository URLs (`https://github.com/<owner>/<repo>`), the repository
//     name in prose and generated comments, the binary name (`bin/schema`,
//     `cmd/schema`), the build tags (`schema_leg_*`) and the SCHEMA_*
//     environment variables;
//   - regex-escaped, concatenated, %s-built, JSON-escaped and
//     Windows-separator spellings, two-part `@v2` queries and versions in
//     prose (none exist in the tree today);
//   - DefaultFrom and DefaultTo, the Fixups and Exceptions tables, and the
//     expected strings in this package's own tests and in the guard: a later
//     move re-points them by hand;
//   - formatting: the program does not run gofmt, and an import path of a
//     different length can need it (run gofmt -l on the result).
package modpath

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// DefaultFrom and DefaultTo are the move this package exists for. The strings
// are split so that the program's own rewrite never touches them: neither
// half is a spelling of either path.
const (
	DefaultFrom = "github.com/mas-bandwidth/schema" + "/v2"
	DefaultTo   = "github.com/mas-bandwidth/schema" + "/v3"
)

// Command is the command that fixes what Check reports; the guard test names
// it in its failure message.
const Command = "go run ./tools/modpath ."

// DocPin is what a release pin in a Markdown page becomes.
const DocPin = "vX.Y.Z"

// PinProblem is the report for a version the run will not choose.
const PinProblem = "a real pinned version; decide by hand"

// Allow is one allowlisted path, relative to the repository root with forward
// slashes. A path ending in "/" is a directory and covers everything under it.
type Allow struct {
	Path   string
	Reason string
}

// Allowlist is every path whose text is left as written. Each is a record of
// something that ran: rewriting it would make it say what did not happen.
var Allowlist = []Allow{
	{"bench/results/", "dated bench runs: each file records the run, its commands and its go test lines as they printed"},
	{"docs/measurements/", "dated measurements: each records a sitting as it ran"},
	{"docs/roadmap-briefs/", "briefs as they were handed out; docs/roadmap.sexp names them as evidence"},
	{"docs/roadmap-evidence/", "evidence captured when a roadmap item closed"},
	{"docs/notes/", "dated working notes"},
	{"notes/", "dated working notes"},
	{"working/", "records of past working sessions"},
	{"docs/plans/", "plans as they were written at the time"},
	{"internal/codegen/lobstertable/BENCH-RECORD.md", "a bench record that quotes the go test line of the 2026-11-07 run"},
}

// Except is one line in a live file that names the old path on purpose: a
// line of File that contains Substr is neither rewritten nor reported.
type Except struct {
	File   string
	Substr string
	Reason string
}

// Exceptions is kept to the lines that must say the old path. The substring
// is built from DefaultFrom so this table is never rewritten itself. It is
// matched within one line, so a reflow of the page must keep it on one line.
var Exceptions = []Except{
	{"docs/VERSIONING.md", "under the old path, `" + DefaultFrom + "`", "tells an adopter which path a pin made before the move resolves under, so they know what to search for"},
}

func (r *Rewriter) excepted(rel, line string) bool {
	for _, e := range Exceptions {
		if e.File == rel && strings.Contains(line, e.Substr) {
			return true
		}
	}
	return false
}

// Allowed answers the reason a path is allowlisted, or "".
func Allowed(rel string) string {
	rel = filepath.ToSlash(rel)
	for _, a := range Allowlist {
		if strings.HasSuffix(a.Path, "/") {
			if strings.HasPrefix(rel, a.Path) {
				return a.Reason
			}
		} else if rel == a.Path {
			return a.Reason
		}
	}
	return ""
}

// Fixup is a fixed edit to one page that goes with the default move: a
// sentence that the move makes untrue. It is applied only when From and To
// are the defaults. Marker is a phrase of New that no other text has; when
// the page holds it (compared with line breaks folded, so a reflowed
// paragraph still counts) the edit is done. Otherwise Old is replaced by New
// once; when neither is there the page has moved and the run notes it.
type Fixup struct {
	File   string
	Old    string
	New    string
	Marker string
}

// Fixups are the page edits of the default move.
var Fixups = []Fixup{
	{
		File: "docs/VERSIONING.md",
		Old:  "`TableElemKind`) is wire law and therefore frozen within a major.\n",
		New: "`TableElemKind`) is wire law and therefore frozen within a major. The\n" +
			"module path is `github.com/mas-bandwidth/schema/v3` from v3.0.3. The tags\n" +
			"v3.0.0, v3.0.1 and v3.0.2 carry a go.mod that says `/v2`, so\n" +
			"Go cannot resolve them under either path. A pin made on a promotion tag\n" +
			"before the move resolves as a `v2.5.1-0.<date>-<hash>` pseudo-version\n" +
			"under the old path, `" + DefaultFrom + "`.\n",
		Marker: "The module path is `github.com/mas-bandwidth/schema/v3` from v3.0.3.",
	},
	{
		File:   "docs/USAGE.md",
		Old:    "`v2.5.0` is the newest tag at this writing.\n",
		New:    "`vX.Y.Z` stands for the release you pin.\n",
		Marker: "`vX.Y.Z` stands for the release you pin.",
	},
}

// Problem is one thing in the tree the rewrite reports instead of changing.
type Problem struct {
	File string
	Line int
	Msg  string
}

func (p Problem) String() string {
	if p.Line > 0 {
		return fmt.Sprintf("%s:%d: %s", p.File, p.Line, p.Msg)
	}
	return fmt.Sprintf("%s: %s", p.File, p.Msg)
}

type spelling struct {
	from, to string
	// only the full spelling (it begins with the host) may follow a '/', as
	// in a URL; a shorter one after a '/' is the end of somebody else's path
	slashOK bool
}

// Rewriter rewrites From to To.
type Rewriter struct {
	From, To  string
	spellings []spelling // longest first
	toSpells  []spelling // spellings of To, to fix a version written after one
	toMajor   int        // 0 when To has no major suffix
	zero      string     // the zero pseudo-version of To's major
	fixups    bool
	replaceRE *regexp.Regexp
}

var (
	pathRE  = regexp.MustCompile(`^[a-z0-9][a-z0-9.-]*\.[a-z]+(/[A-Za-z0-9_.-]+)+$`)
	majorRE = regexp.MustCompile(`^v([0-9]+)$`)
)

// New builds a Rewriter. Both paths must look like a module path with a host
// (`example.com/owner/repo[/vN]`), they must differ, and neither may be the
// other with more elements after it.
func New(from, to string) (*Rewriter, error) {
	for _, p := range []string{from, to} {
		if !pathRE.MatchString(p) {
			return nil, fmt.Errorf("%q is not a module path with a host (example.com/owner/repo/vN)", p)
		}
	}
	if from == to {
		return nil, errors.New("-from and -to are the same path")
	}
	if strings.HasPrefix(to, from+"/") || strings.HasPrefix(from, to+"/") {
		return nil, fmt.Errorf("-from %q and -to %q: one is the other with more elements after it, so a second run would rewrite the new path again; move in two steps through a path that is neither", from, to)
	}
	r := &Rewriter{From: from, To: to, fixups: from == DefaultFrom && to == DefaultTo}
	fe, te := strings.Split(from, "/"), strings.Split(to, "/")
	r.spellings = append(r.spellings, spelling{from: from, to: to, slashOK: true})
	if len(fe) >= 3 && len(te) >= 3 {
		r.spellings = append(r.spellings, spelling{from: strings.Join(fe[1:], "/"), to: strings.Join(te[1:], "/")})
	}
	if majorRE.MatchString(fe[len(fe)-1]) && len(fe) >= 4 && len(te) >= 3 {
		r.spellings = append(r.spellings, spelling{
			from: strings.Join(fe[len(fe)-2:], "/"),
			to:   strings.Join(te[len(te)-2:], "/"),
		})
	}
	r.toSpells = []spelling{{from: to, slashOK: true}}
	if len(te) >= 3 {
		r.toSpells = append(r.toSpells, spelling{from: strings.Join(te[1:], "/")})
	}
	if m := majorRE.FindStringSubmatch(te[len(te)-1]); m != nil {
		n, err := strconv.Atoi(m[1])
		if err != nil {
			return nil, fmt.Errorf("%q: major version suffix %q: %w", to, te[len(te)-1], err)
		}
		r.toMajor = n
	}
	if r.toMajor >= 2 {
		r.zero = fmt.Sprintf("v%d.0.0-00010101000000-000000000000", r.toMajor)
	} else {
		r.zero = "v0.0.0-00010101000000-000000000000"
	}
	// a replace directive for the module, as a go.mod line, a block row, or
	// written inside a string: the path, an optional version, then =>
	r.replaceRE = regexp.MustCompile(`(?:` + regexp.QuoteMeta(from) + `|` + regexp.QuoteMeta(to) + `)(?:[ \t]+v[0-9][^ \t]*)?[ \t]+=>`)
	return r, nil
}

// hasReplace: the text writes a replace for From or To.
func (r *Rewriter) hasReplace(text []byte) bool {
	return r.replaceRE.Match(text)
}

func isWord(c byte) bool {
	return c == '_' || c >= '0' && c <= '9' || c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z'
}

// boundaryBefore: nothing that could continue a path element stands before
// s[i]; a '/' only when slashOK.
func boundaryBefore(s string, i int, slashOK bool) bool {
	if i == 0 {
		return true
	}
	c := s[i-1]
	// a written escape (`\t`, `\n`, `\r` in a Go string or a printf format)
	// ends whatever came before it
	if i >= 2 && s[i-2] == '\\' && (c == 't' || c == 'n' || c == 'r') {
		return true
	}
	if isWord(c) || c == '.' || c == '-' {
		return false
	}
	return c != '/' || slashOK
}

// boundaryAfter: the element ends at s[j].
func boundaryAfter(s string, j int) bool {
	if j >= len(s) {
		return true
	}
	c := s[j]
	if isWord(c) {
		return false
	}
	if (c == '.' || c == '-') && j+1 < len(s) && isWord(s[j+1]) {
		return false
	}
	return true
}

// versionAt parses a semantic version starting at s[i] ("v" MAJOR "." MINOR
// "." PATCH, then any pre-release or build suffix) and returns its end and
// major, or -1.
func versionAt(s string, i int) (end, major int) {
	if i >= len(s) || s[i] != 'v' {
		return -1, 0
	}
	j := i + 1
	parts := 0
	for parts < 3 {
		k := j
		for k < len(s) && s[k] >= '0' && s[k] <= '9' {
			k++
		}
		if k == j {
			return -1, 0
		}
		if parts == 0 {
			n, err := strconv.Atoi(s[j:k])
			if err != nil {
				return -1, 0 // a major too large for an int is not a version
			}
			major = n
		}
		parts++
		j = k
		if parts < 3 {
			if j >= len(s) || s[j] != '.' {
				return -1, 0
			}
			j++
		}
	}
	for j < len(s) && (isWord(s[j]) || s[j] == '.' || s[j] == '-' || s[j] == '+') {
		j++
	}
	// a trailing '.' is punctuation, not part of the version
	for j > i && s[j-1] == '.' {
		j--
	}
	return j, major
}

func (r *Rewriter) majorFits(major int) bool {
	if r.toMajor >= 2 {
		return major == r.toMajor
	}
	return major == 0 || major == 1
}

// fixVersion looks at what follows a path that ends at s[j] and, when a
// version with the wrong major is written there, answers the replacement
// span. hold is set when the line must be left exactly as it is; problem
// names why.
func (r *Rewriter) fixVersion(s string, j int, doc, replaced bool) (start, end int, repl string, hold bool, problem string) {
	// "<path> vX.Y.Z": separated by spaces, tabs, or a written \t
	k := j
gap:
	for k < len(s) {
		switch {
		case s[k] == ' ' || s[k] == '\t':
			k++
		case s[k] == '\\' && k+1 < len(s) && s[k+1] == 't':
			k += 2
		default:
			break gap
		}
	}
	if k > j {
		if e, major := versionAt(s, k); e > 0 && boundaryAfter(s, e) && !r.majorFits(major) {
			if !replaced {
				return -1, -1, "", true, PinProblem + fmt.Sprintf(" (%s with no replace for the module in this file)", s[k:e])
			}
			// a replaced require: the version is never used
			return k, e, r.zero, false, ""
		}
		return -1, -1, "", false, ""
	}
	// "<path>[/sub/path]@vX.Y.Z"
	k = j
	for k < len(s) && s[k] == '/' {
		k++
		for k < len(s) && (isWord(s[k]) || s[k] == '.' || s[k] == '-' || s[k] == '/') {
			k++
		}
	}
	if k < len(s) && s[k] == '@' {
		if e, major := versionAt(s, k+1); e > 0 && boundaryAfter(s, e) && !r.majorFits(major) {
			if doc {
				return k + 1, e, DocPin, false, ""
			}
			return -1, -1, "", false, fmt.Sprintf("pins release %s under the module path; choose a release of the new path and edit this line", s[k+1:e])
		}
	}
	return -1, -1, "", false, ""
}

// RewriteLine rewrites one line. doc is true for a Markdown page; replaced is
// true when the file writes a replace for the module.
func (r *Rewriter) RewriteLine(line string, doc, replaced bool) (string, []string) {
	var b strings.Builder
	var problems []string
	i := 0
	changed := false
	for i < len(line) {
		matched := false
		for _, sp := range r.spellings {
			if !strings.HasPrefix(line[i:], sp.from) {
				continue
			}
			j := i + len(sp.from)
			if !boundaryBefore(line, i, sp.slashOK) || !boundaryAfter(line, j) {
				continue
			}
			b.WriteString(sp.to)
			changed = true
			i = j
			matched = true
			break
		}
		if !matched {
			for _, ts := range r.toSpells {
				if strings.HasPrefix(line[i:], ts.from) && boundaryBefore(line, i, ts.slashOK) && boundaryAfter(line, i+len(ts.from)) {
					b.WriteString(ts.from)
					i += len(ts.from)
					matched = true
					break
				}
			}
		}
		if !matched {
			b.WriteByte(line[i])
			i++
			continue
		}
		// a path was written (new or rewritten): its version must fit
		start, end, repl, hold, problem := r.fixVersion(line, i, doc, replaced)
		if problem != "" {
			problems = append(problems, problem)
		}
		if hold {
			return line, problems
		}
		if start >= 0 {
			b.WriteString(line[i:start])
			b.WriteString(repl)
			i = end
			changed = true
		}
	}
	if !changed {
		return line, problems
	}
	return b.String(), problems
}

// Result is what one file's rewrite did.
type Result struct {
	Out      []byte
	Lines    []int // 1-based lines of the input that change or go
	Problems []Problem
	// Notes are page edits (Fixups) that found neither their old nor their
	// new text. They are printed by a rewrite and never fail a check: the
	// page edits are a one-time part of the move, and a page reworded later
	// must not turn the guard red.
	Notes []Problem
}

func flat(s string) string { return strings.Join(strings.Fields(s), " ") }

// RewriteFile rewrites one file's content. rel is its path relative to the
// repository root, with forward slashes. sibling is, for a go.sum, the go.mod
// beside it (nil when there is none); it is ignored for every other file.
func (r *Rewriter) RewriteFile(rel string, data, sibling []byte) Result {
	var res Result
	rel = filepath.ToSlash(rel)
	doc := strings.HasSuffix(rel, ".md")
	gosum := path.Base(rel) == "go.sum"
	if !r.mayTouch(rel, data) {
		res.Out = data
		return res
	}
	replaced := r.hasReplace(data)
	if gosum {
		replaced = r.hasReplace(sibling)
	}
	text := string(data)
	lines := strings.SplitAfter(text, "\n")
	var out strings.Builder
	for n, ln := range lines {
		if ln == "" {
			continue
		}
		body := strings.TrimRight(ln, "\r\n")
		eol := ln[len(body):]
		if r.excepted(rel, body) {
			out.WriteString(ln)
			continue
		}
		if gosum && (strings.HasPrefix(body, r.From+" ") || strings.HasPrefix(body, r.To+" ")) {
			f := strings.Fields(body)
			if len(f) >= 2 && (f[0] == r.From || f[0] == r.To && !r.versionFits(f[1])) {
				if replaced {
					res.Lines = append(res.Lines, n+1)
					continue
				}
				res.Problems = append(res.Problems, Problem{File: rel, Line: n + 1, Msg: PinProblem + " (go.sum line with no replace for the module in the go.mod beside it)"})
				out.WriteString(ln)
				continue
			}
		}
		nb, problems := r.RewriteLine(body, doc, replaced)
		for _, p := range problems {
			res.Problems = append(res.Problems, Problem{File: rel, Line: n + 1, Msg: p})
		}
		if nb != body {
			res.Lines = append(res.Lines, n+1)
		}
		out.WriteString(nb)
		out.WriteString(eol)
	}
	result := out.String()
	if r.fixups {
		for _, f := range Fixups {
			if f.File != rel {
				continue
			}
			switch {
			case strings.Contains(flat(result), flat(f.Marker)):
			case strings.Contains(result, f.Old):
				at := strings.Index(result, f.Old)
				res.Lines = append(res.Lines, strings.Count(result[:at], "\n")+1)
				result = strings.Replace(result, f.Old, f.New, 1)
			default:
				res.Notes = append(res.Notes, Problem{File: rel, Msg: fmt.Sprintf("the page edit for %q finds neither its old nor its new text; the page moved, so update the Fixups table in internal/modpath", firstLine(f.Old))})
			}
		}
	}
	sort.Ints(res.Lines)
	res.Lines = uniq(res.Lines)
	res.Out = []byte(result)
	return res
}

// mayTouch is a fast reject: a file that holds no spelling of either path and
// is not a page with an edit cannot change.
func (r *Rewriter) mayTouch(rel string, data []byte) bool {
	if bytes.Contains(data, []byte(r.spellings[len(r.spellings)-1].from)) ||
		bytes.Contains(data, []byte(r.toSpells[len(r.toSpells)-1].from)) {
		return true
	}
	if r.fixups {
		for _, f := range Fixups {
			if f.File == rel {
				return true
			}
		}
	}
	return false
}

func (r *Rewriter) versionFits(v string) bool {
	v = strings.TrimSuffix(v, "/go.mod")
	e, major := versionAt(v, 0)
	return e > 0 && r.majorFits(major)
}

func firstLine(s string) string {
	first, _, _ := strings.Cut(s, "\n")
	return first
}

func uniq(xs []int) []int {
	out := xs[:0]
	for i, x := range xs {
		if i == 0 || x != xs[i-1] {
			out = append(out, x)
		}
	}
	return out
}

// ignoreRule is one line of a .gitignore, with the directory (relative to
// the root, "" for the root) whose .gitignore it came from.
type ignoreRule struct {
	base     string
	segs     []string // the pattern's path elements, for an anchored rule
	name     string   // the pattern, for an unanchored rule (matches the last element)
	anchored bool
	dirOnly  bool
	negate   bool
}

func parseIgnore(base string, data []byte) []ignoreRule {
	var rules []ignoreRule
	for ln := range strings.SplitSeq(string(data), "\n") {
		ln = strings.TrimRight(ln, " \r\t")
		if ln == "" || strings.HasPrefix(ln, "#") {
			continue
		}
		var ru ignoreRule
		ru.base = base
		if strings.HasPrefix(ln, "!") {
			ru.negate = true
			ln = ln[1:]
		}
		ln = strings.TrimPrefix(ln, `\`)
		if strings.HasSuffix(ln, "/") {
			ru.dirOnly = true
			ln = strings.TrimSuffix(ln, "/")
		}
		if ln == "" {
			continue
		}
		if strings.Contains(ln, "/") {
			ru.anchored = true
			ru.segs = strings.Split(strings.TrimPrefix(ln, "/"), "/")
		} else {
			ru.name = ln
		}
		rules = append(rules, ru)
	}
	return rules
}

func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, _ := path.Match(pat[0], segs[0]); !ok {
		return false
	}
	return matchSegs(pat[1:], segs[1:])
}

// ignored applies git's rule: the last matching pattern, from the root's
// .gitignore down to the deepest, decides.
func ignored(rules []ignoreRule, rel string, isDir bool) bool {
	out := false
	for _, ru := range rules {
		sub := rel
		if ru.base != "" {
			if !strings.HasPrefix(rel, ru.base+"/") {
				continue
			}
			sub = rel[len(ru.base)+1:]
		}
		if ru.dirOnly && !isDir {
			continue
		}
		var m bool
		if ru.anchored {
			m = matchSegs(ru.segs, strings.Split(sub, "/"))
		} else {
			m, _ = path.Match(ru.name, path.Base(sub))
		}
		if m {
			out = !ru.negate
		}
	}
	return out
}

// Files walks the repository under root and calls fn with each regular file's
// path relative to root (forward slashes). It skips the root .git, any lower
// directory holding a .git entry (a nested checkout or worktree), what the
// .gitignore files ignore, and the allowlist. Symlinks are not followed; an
// entry that vanishes during the walk is skipped.
func Files(root string, fn func(rel string) error) error {
	var rules []ignoreRule
	load := func(dir, rel string) {
		if data, err := os.ReadFile(filepath.Join(dir, ".gitignore")); err == nil {
			rules = append(rules, parseIgnore(rel, data)...)
		}
	}
	return filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, fs.ErrNotExist) {
				return nil
			}
			return err
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if d.IsDir() {
			if rel == "." {
				load(p, "")
				return nil
			}
			if d.Name() == ".git" && rel == ".git" {
				return filepath.SkipDir
			}
			if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil {
				return filepath.SkipDir
			}
			if ignored(rules, rel, true) || Allowed(rel+"/") != "" {
				return filepath.SkipDir
			}
			load(p, rel)
			return nil
		}
		// a worktree's root .git is a file that points at the main checkout
		if rel == ".git" || !d.Type().IsRegular() || Allowed(rel) != "" || ignored(rules, rel, false) {
			return nil
		}
		return fn(rel)
	})
}

// Change is one file the run changed, or would change under -check.
type Change struct {
	File  string
	Lines []int
}

// Report is a whole run.
type Report struct {
	Root     string // the resolved root
	Files    int    // files read
	Changes  []Change
	Problems []Problem
	Notes    []Problem
}

// Clean is true when nothing would change and nothing is reported.
func (rep Report) Clean() bool { return len(rep.Changes) == 0 && len(rep.Problems) == 0 }

// Run rewrites (or, with check, only inspects) every file under root. root is
// resolved through symlinks first. It fails, before writing anything, unless
// the walk finds the root go.mod and at least minFiles files: a wrong or empty
// root must never pass as clean.
func (r *Rewriter) Run(root string, check bool, minFiles int) (Report, error) {
	var rep Report
	abs, err := filepath.Abs(root)
	if err != nil {
		return rep, err
	}
	resolved, err := filepath.EvalSymlinks(abs)
	if err != nil {
		return rep, err
	}
	rep.Root = resolved
	var files []string
	sawGoMod := false
	if err := Files(resolved, func(rel string) error {
		files = append(files, rel)
		if rel == "go.mod" {
			sawGoMod = true
		}
		return nil
	}); err != nil {
		return rep, err
	}
	if !sawGoMod || len(files) < minFiles {
		return rep, fmt.Errorf("%s (from %s) is not the repository root: the walk saw %d files (the floor is %d) and the root go.mod %s", resolved, root, len(files), minFiles, map[bool]string{true: "was there", false: "was not there"}[sawGoMod])
	}
	for _, rel := range files {
		p := filepath.Join(resolved, filepath.FromSlash(rel))
		data, err := os.ReadFile(p)
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return rep, err
		}
		rep.Files++
		if bytes.IndexByte(data[:min(len(data), 8192)], 0) >= 0 {
			continue // binary
		}
		var sibling []byte
		if path.Base(rel) == "go.sum" {
			sibling, _ = os.ReadFile(filepath.Join(filepath.Dir(p), "go.mod"))
		}
		res := r.RewriteFile(rel, data, sibling)
		rep.Problems = append(rep.Problems, res.Problems...)
		rep.Notes = append(rep.Notes, res.Notes...)
		if bytes.Equal(res.Out, data) {
			continue
		}
		rep.Changes = append(rep.Changes, Change{File: rel, Lines: res.Lines})
		if check {
			continue
		}
		info, err := os.Stat(p)
		if err != nil {
			return rep, err
		}
		if err := os.WriteFile(p, res.Out, info.Mode().Perm()); err != nil {
			return rep, err
		}
	}
	return rep, nil
}

// Print writes a run's report: one line per file changed and a total, or,
// under check, one line per offending file:line.
func (rep Report) Print(w io.Writer, check bool) {
	for _, c := range rep.Changes {
		if check {
			for _, n := range c.Lines {
				_, _ = fmt.Fprintf(w, "%s:%d: spells the old module path (or a version that does not fit the new one)\n", c.File, n)
			}
			if len(c.Lines) == 0 {
				_, _ = fmt.Fprintf(w, "%s: would change\n", c.File)
			}
			continue
		}
		_, _ = fmt.Fprintf(w, "rewrote %s (%d lines)\n", c.File, len(c.Lines))
	}
	for _, p := range rep.Problems {
		_, _ = fmt.Fprintf(w, "PROBLEM %s\n", p)
	}
	if !check {
		for _, p := range rep.Notes {
			_, _ = fmt.Fprintf(w, "NOTE %s\n", p)
		}
	}
	switch {
	case check && rep.Clean():
		_, _ = fmt.Fprintf(w, "modpath: clean, nothing spells the old module path (%d files read under %s)\n", rep.Files, rep.Root)
	case check:
		_, _ = fmt.Fprintf(w, "modpath: %d files to rewrite, %d problems (%d files read); run: %s\n", len(rep.Changes), len(rep.Problems), rep.Files, Command)
	case len(rep.Changes) == 0:
		_, _ = fmt.Fprintf(w, "modpath: nothing to rewrite, the tree is already moved (%d files read)\n", rep.Files)
	default:
		_, _ = fmt.Fprintf(w, "modpath: rewrote %d files (%d files read)\n", len(rep.Changes), rep.Files)
	}
}
