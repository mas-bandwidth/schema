// Command releasetag makes the two kinds of tag docs/VERSIONING.md defines,
// and is the only thing that makes them: nobody tags this repository by hand
// (Glenn, 2026-10-15).
//
//	releasetag promotion-tag [-commit REV] [-push]
//	    tags a commit on main main-<first 12 hex of its hash>; a commit that
//	    already carries a main- tag is skipped, never retagged.
//	    .github/workflows/promotion-tag.yml runs it, through
//	    `make promotion-tag PUSH=1`, on every push to main.
//
//	releasetag release -version vX.Y.Z [-commit REV] [-main REF] [-push]
//	    tags one commit vX.Y.Z (annotated). Refuses, by name, a version that
//	    does not parse, one not greater than the newest v tag, and a commit
//	    not on main (not in `git rev-list REF`, default origin/main).
//	    .github/workflows/release.yml runs it, through
//	    `make release VERSION=vX.Y.Z PUSH=1`, when dispatched.
//
// Without -push either command prints its decision and writes nothing. With
// -push it makes exactly one tag on exactly one commit and pushes only that
// tag.
//
// The names and the refusals are pure functions of their arguments
// (PromotionTag, PromotionDecision, ParseVersion, NewestRelease, ReleaseTag),
// unit-tested with no git; Promote and Release drive git through the Git
// interface, which the tests fake. Only main execs git.
package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
)

// The refusals, each matched with errors.Is.
var (
	ErrNotACommit     = errors.New("not a full 40-hex commit hash")
	ErrVersionGrammar = errors.New("version does not parse as vMAJOR.MINOR.PATCH")
	ErrNotGreater     = errors.New("version is not greater than the newest v tag")
	ErrNotOnMain      = errors.New("commit is not on main")
)

// PromotionPrefix begins every promotion tag.
const PromotionPrefix = "main-"

var (
	commitHash  = regexp.MustCompile(`^[0-9a-f]{40}$`)
	versionForm = regexp.MustCompile(`^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$`)
)

// PromotionTag names the promotion tag of a commit: main- and the first 12
// hex digits of the commit's own hash.
func PromotionTag(sha string) (string, error) {
	if !commitHash.MatchString(sha) {
		return "", fmt.Errorf("%w: %q", ErrNotACommit, sha)
	}
	return PromotionPrefix + sha[:12], nil
}

// PromotionDecision names the tag for a commit and says whether to skip it,
// given the tags already pointing at that commit: a commit that already carries
// any promotion tag is skipped, not retagged.
func PromotionDecision(sha string, pointsAt []string) (tag string, skip bool, err error) {
	tag, err = PromotionTag(sha)
	if err != nil {
		return "", false, err
	}
	for _, t := range pointsAt {
		if strings.HasPrefix(t, PromotionPrefix) {
			return tag, true, nil
		}
	}
	return tag, false, nil
}

// Version is a parsed release version.
type Version struct{ Major, Minor, Patch uint64 }

func (v Version) String() string { return fmt.Sprintf("v%d.%d.%d", v.Major, v.Minor, v.Patch) }

// Less orders versions numerically.
func (v Version) Less(w Version) bool {
	if v.Major != w.Major {
		return v.Major < w.Major
	}
	if v.Minor != w.Minor {
		return v.Minor < w.Minor
	}
	return v.Patch < w.Patch
}

// ParseVersion parses vMAJOR.MINOR.PATCH, with no leading zeros and no
// pre-release or build suffix.
func ParseVersion(s string) (Version, error) {
	m := versionForm.FindStringSubmatch(s)
	if m == nil {
		return Version{}, fmt.Errorf("%w: %q", ErrVersionGrammar, s)
	}
	var n [3]uint64
	for i := range n {
		v, err := strconv.ParseUint(m[i+1], 10, 64)
		if err != nil {
			return Version{}, fmt.Errorf("%w: %q: %v", ErrVersionGrammar, s, err)
		}
		n[i] = v
	}
	return Version{n[0], n[1], n[2]}, nil
}

// NewestRelease is the greatest release version among tags; tags that are
// not releases (promotion tags, pre-releases, anything else) are ignored.
func NewestRelease(tags []string) (Version, bool) {
	var newest Version
	found := false
	for _, t := range tags {
		v, err := ParseVersion(t)
		if err != nil {
			continue
		}
		if !found || newest.Less(v) {
			newest, found = v, true
		}
	}
	return newest, found
}

// ReleaseTag checks a release and names its tag: version must parse, must be
// greater than the newest release among tags, and commit must be a full hash
// in mainAncestry (the commits of main).
func ReleaseTag(version, commit string, tags, mainAncestry []string) (string, error) {
	v, err := ParseVersion(version)
	if err != nil {
		return "", err
	}
	if newest, ok := NewestRelease(tags); ok && !newest.Less(v) {
		return "", fmt.Errorf("%w: %s is not greater than %s", ErrNotGreater, v, newest)
	}
	if !commitHash.MatchString(commit) {
		return "", fmt.Errorf("%w: %q", ErrNotACommit, commit)
	}
	onMain := false
	for _, c := range mainAncestry {
		if c == commit {
			onMain = true
			break
		}
	}
	if !onMain {
		return "", fmt.Errorf("%w: %s", ErrNotOnMain, commit)
	}
	return v.String(), nil
}

// Git runs one git command and returns its standard output.
type Git interface {
	Run(args ...string) (string, error)
}

func fields(s string) []string { return strings.Fields(s) }

func resolve(g Git, rev string) (string, error) {
	out, err := g.Run("rev-parse", "--verify", rev+"^{commit}")
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(out), nil
}

// Promote tags rev with its promotion tag, unless it already carries one. It
// writes nothing unless push is set.
func Promote(g Git, rev string, push bool, out io.Writer) error {
	sha, err := resolve(g, rev)
	if err != nil {
		return err
	}
	pointsAt, err := g.Run("tag", "--points-at", sha)
	if err != nil {
		return err
	}
	tag, skip, err := PromotionDecision(sha, fields(pointsAt))
	if err != nil {
		return err
	}
	if skip {
		fmt.Fprintf(out, "promotion-tag: %s already carries a promotion tag; skipped, not retagged\n", sha)
		return nil
	}
	if !push {
		fmt.Fprintf(out, "promotion-tag: would tag %s %s (dry run; PUSH=1 to tag and push)\n", tag, sha)
		return nil
	}
	if _, err := g.Run("tag", tag, sha); err != nil {
		return err
	}
	if _, err := g.Run("push", "origin", "refs/tags/"+tag); err != nil {
		return err
	}
	fmt.Fprintf(out, "promotion-tag: tagged and pushed %s %s\n", tag, sha)
	return nil
}

// Release checks and makes release version on rev. mainRef is the ref whose
// ancestry is main. It writes nothing unless push is set, and nothing on a
// refusal.
func Release(g Git, version, rev, mainRef string, push bool, out io.Writer) error {
	if _, err := ParseVersion(version); err != nil {
		return err // before any git call: the cheapest refusal first
	}
	sha, err := resolve(g, rev)
	if err != nil {
		return err
	}
	tags, err := g.Run("tag", "--list", "v*")
	if err != nil {
		return err
	}
	ancestry, err := g.Run("rev-list", mainRef)
	if err != nil {
		return err
	}
	tag, err := ReleaseTag(version, sha, fields(tags), fields(ancestry))
	if err != nil {
		return err
	}
	if !push {
		fmt.Fprintf(out, "release: would tag %s %s (dry run; PUSH=1 to tag and push)\n", tag, sha)
		return nil
	}
	if _, err := g.Run("tag", "-a", tag, "-m", "schema "+tag, sha); err != nil {
		return err
	}
	if _, err := g.Run("push", "origin", "refs/tags/"+tag); err != nil {
		return err
	}
	fmt.Fprintf(out, "release: tagged and pushed %s %s\n", tag, sha)
	return nil
}

// execGit is the real git, used only by main.
type execGit struct{}

func (execGit) Run(args ...string) (string, error) {
	cmd := exec.Command("git", args...)
	cmd.Stderr = os.Stderr
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("git %s: %w", strings.Join(args, " "), err)
	}
	return string(out), nil
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: releasetag promotion-tag [-commit REV] [-push]")
	fmt.Fprintln(os.Stderr, "       releasetag release -version vX.Y.Z [-commit REV] [-main REF] [-push]")
	os.Exit(2)
}

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	fs := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	commit := fs.String("commit", "HEAD", "the commit to tag")
	push := fs.Bool("push", false, "make the tag and push it (default: print the decision only)")
	var err error
	switch os.Args[1] {
	case "promotion-tag":
		_ = fs.Parse(os.Args[2:])
		err = Promote(execGit{}, *commit, *push, os.Stdout)
	case "release":
		version := fs.String("version", "", "the release version, vMAJOR.MINOR.PATCH")
		mainRef := fs.String("main", "origin/main", "the ref whose ancestry is main")
		_ = fs.Parse(os.Args[2:])
		err = Release(execGit{}, *version, *commit, *mainRef, *push, os.Stdout)
	default:
		usage()
	}
	if err != nil {
		fmt.Fprintf(os.Stderr, "releasetag %s: refused: %v\n", os.Args[1], err)
		os.Exit(1)
	}
}
