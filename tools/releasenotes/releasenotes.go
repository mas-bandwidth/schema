// Command releasenotes writes the body of a GitHub release: the pull requests
// merged on main since the previous v tag, newest first, one per line, and the
// compare link.
//
//	releasenotes -version vX.Y.Z [-repo OWNER/NAME] [-out FILE]
//
// The body is
//
//   - <title> (#<n>)
//     ...
//     Full changelog: https://github.com/<repo>/compare/<prev>...<new>
//
// GitHub refuses a release body over 125,000 characters (HTTP 422). v3.0.14
// failed there with --generate-notes: 349 commits since v3.0.13. So the body
// here never passes MaxBody (100,000 characters): when the whole list would,
// it keeps the newest pull requests that fit and says how many were left out,
// pointing at the compare link.
//
// The pull requests are read from git, not the API: every commit on main's
// first-parent line between the previous v tag and the new one is a landed
// pull request, either a squash ("<title> (#<n>)") or a merge commit ("Merge
// pull request #<n> from ...", the title its body's first line). The previous
// v tag is the greatest vMAJOR.MINOR.PATCH tag less than the new version.
//
// .github/workflows/release.yml runs it after `make release` has pushed the
// tag and passes the file to `gh release create --notes-file`.
//
// Parsing and the body are pure functions of their arguments, unit-tested
// with fixed input: no git, no network and no clock. Only main execs git.
package main

import (
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// MaxBody is the longest body this writes, in characters; GitHub's limit is
// 125,000.
const MaxBody = 100000

// The record and field separators of the git log format main reads.
const (
	recordSep = "\x1e"
	fieldSep  = "\x1f"
)

// LogFormat is the `git log --format` main asks for: subject and body, one
// record per commit.
const LogFormat = "%s" + fieldSep + "%b" + recordSep

// The refusals, each matched with errors.Is.
var (
	ErrVersionGrammar = errors.New("version does not parse as vMAJOR.MINOR.PATCH")
	ErrNoPrevious     = errors.New("no v tag less than the version")
)

// PR is one landed pull request.
type PR struct {
	Number int
	Title  string
}

// Line is the PR's line in the notes.
func (p PR) Line() string { return fmt.Sprintf("* %s (#%d)", p.Title, p.Number) }

var (
	versionForm = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
	squash      = regexp.MustCompile(`^(.*\S)\s+\(#([0-9]+)\)$`)
	mergePR     = regexp.MustCompile(`^Merge pull request #([0-9]+) from (\S+)`)
)

type version [3]uint64

func parseVersion(s string) (version, bool) {
	m := versionForm.FindStringSubmatch(s)
	if m == nil {
		return version{}, false
	}
	var v version
	for i := range v {
		n, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return version{}, false
		}
		v[i] = n
	}
	return v, true
}

func (v version) less(w version) bool {
	for i := range v {
		if v[i] != w[i] {
			return v[i] < w[i]
		}
	}
	return false
}

// Previous is the greatest release tag among tags less than next; tags that
// are not vMAJOR.MINOR.PATCH are ignored.
func Previous(next string, tags []string) (string, error) {
	nv, ok := parseVersion(next)
	if !ok {
		return "", fmt.Errorf("%w: %q", ErrVersionGrammar, next)
	}
	best, found := "", false
	var bv version
	for _, t := range tags {
		v, ok := parseVersion(t)
		if !ok || !v.less(nv) {
			continue
		}
		if !found || bv.less(v) {
			best, bv, found = t, v, true
		}
	}
	if !found {
		return "", fmt.Errorf("%w: %s", ErrNoPrevious, next)
	}
	return best, nil
}

// ParseLog reads the output of `git log --first-parent --format=LogFormat`,
// newest first, into the pull requests it names, in the same order. A commit
// that names no pull request is left out.
func ParseLog(log string) []PR {
	var prs []PR
	for rec := range strings.SplitSeq(log, recordSep) {
		rec = strings.TrimLeft(rec, "\n")
		if rec == "" {
			continue
		}
		subject, body, _ := strings.Cut(rec, fieldSep)
		subject = strings.TrimSpace(subject)
		if m := mergePR.FindStringSubmatch(subject); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				continue
			}
			title := m[2]
			for line := range strings.SplitSeq(body, "\n") {
				if line = strings.TrimSpace(line); line != "" {
					title = line
					break
				}
			}
			prs = append(prs, PR{n, title})
			continue
		}
		if m := squash.FindStringSubmatch(subject); m != nil {
			n, err := strconv.Atoi(m[2])
			if err != nil {
				continue
			}
			prs = append(prs, PR{n, m[1]})
		}
	}
	return prs
}

// CompareURL is the GitHub compare page from prev to next.
func CompareURL(repo, prev, next string) string {
	return fmt.Sprintf("https://github.com/%s/compare/%s...%s", repo, prev, next)
}

// Notes is the release body: the PRs, newest first as given, then the compare
// link. A body that would pass max characters keeps the newest PRs that fit,
// with a line saying how many were left out.
func Notes(prs []PR, repo, prev, next string, max int) string {
	compare := CompareURL(repo, prev, next)
	tail := "Full changelog: " + compare + "\n"
	render := func(kept int) string {
		var b strings.Builder
		for _, p := range prs[:kept] {
			b.WriteString(p.Line())
			b.WriteByte('\n')
		}
		if left := len(prs) - kept; left > 0 {
			noun := "pull requests"
			if left == 1 {
				noun = "pull request"
			}
			fmt.Fprintf(&b, "\n%d earlier %s not listed here; the full changelog has every change: %s\n", left, noun, compare)
		}
		b.WriteByte('\n')
		b.WriteString(tail)
		return b.String()
	}
	if body := render(len(prs)); chars(body) <= max {
		return body
	}
	// the omission line's length depends on the count, so search down from
	// the most that could fit
	kept, size := 0, chars(render(0))
	for kept < len(prs) && size+chars(prs[kept].Line())+1 <= max {
		size += chars(prs[kept].Line()) + 1
		kept++
	}
	for ; kept > 0; kept-- {
		if body := render(kept); chars(body) <= max {
			return body
		}
	}
	return render(0)
}

func chars(s string) int { return len([]rune(s)) }

func git(args ...string) (string, error) {
	out, err := exec.Command("git", args...).Output()
	if err != nil {
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			return "", fmt.Errorf("git %s: %w: %s", strings.Join(args, " "), err, ee.Stderr)
		}
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func run() error {
	next := flag.String("version", "", "the release version, vMAJOR.MINOR.PATCH; its tag must exist")
	repo := flag.String("repo", "mas-bandwidth/schema", "the GitHub repository, OWNER/NAME")
	out := flag.String("out", "", "write the notes here (default standard output)")
	flag.Parse()
	if flag.NArg() != 0 {
		return fmt.Errorf("unexpected arguments: %q", flag.Args())
	}
	tags, err := git("tag", "--list", "v*")
	if err != nil {
		return err
	}
	prev, err := Previous(*next, strings.Fields(tags))
	if err != nil {
		return err
	}
	log, err := git("log", "--first-parent", "--format="+LogFormat, prev+".."+*next+"^{commit}")
	if err != nil {
		return err
	}
	prs := ParseLog(log)
	body := Notes(prs, *repo, prev, *next, MaxBody)
	if *out == "" {
		_, err = os.Stdout.WriteString(body)
		return err
	}
	if err := os.MkdirAll(filepath.Dir(*out), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(*out, []byte(body), 0o644); err != nil {
		return err
	}
	fmt.Fprintf(os.Stderr, "releasenotes: %s since %s: %d pull requests, %d characters, in %s\n", *next, prev, len(prs), chars(body), *out)
	return nil
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "releasenotes:", err)
		os.Exit(1)
	}
}
