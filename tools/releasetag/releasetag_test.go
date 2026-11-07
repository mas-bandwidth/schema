package main

import (
	"errors"
	"fmt"
	"io"
	"slices"
	"strings"
	"testing"
)

const (
	mergeSHA = "0123456789abcdef0123456789abcdef01234567"
	otherSHA = "fedcba9876543210fedcba9876543210fedcba98"
)

// TestReleaseTagNamesAndRefusals pins the names and the refusals of both tag
// kinds docs/VERSIONING.md defines, as pure functions over their arguments: no
// git, no network, no clock.
func TestReleaseTagNamesAndRefusals(t *testing.T) {
	t.Parallel()

	// A promotion tag is main- and the commit's own first 12 hex digits.
	got, err := PromotionTag(mergeSHA)
	if err != nil || got != "main-0123456789ab" {
		t.Fatalf("PromotionTag(%s) = %q, %v; want main-0123456789ab", mergeSHA, got, err)
	}
	for _, bad := range []string{"", "0123456789ab", strings.ToUpper(mergeSHA), mergeSHA + "0", "g" + mergeSHA[1:]} {
		if _, err := PromotionTag(bad); !errors.Is(err, ErrNotACommit) {
			t.Errorf("PromotionTag(%q) err = %v; want ErrNotACommit", bad, err)
		}
	}

	// A commit that already carries a main- tag is skipped, not retagged; a
	// release tag on the same commit does not count as a promotion tag.
	if tag, skip, err := PromotionDecision(mergeSHA, []string{"v2.5.0"}); err != nil || skip || tag != "main-0123456789ab" {
		t.Errorf("PromotionDecision on a commit with only a v tag = %q, %v, %v; want main-0123456789ab, tag it", tag, skip, err)
	}
	if tag, skip, err := PromotionDecision(mergeSHA, []string{"main-0123456789ab"}); err != nil || !skip || tag != "main-0123456789ab" {
		t.Errorf("PromotionDecision on a tagged commit = %q, %v, %v; want skip", tag, skip, err)
	}

	// The version grammar: vMAJOR.MINOR.PATCH and nothing else.
	for _, good := range []string{"v2.6.0", "v0.0.1", "v10.20.30"} {
		if _, err := ParseVersion(good); err != nil {
			t.Errorf("ParseVersion(%q) = %v; want it to parse", good, err)
		}
	}
	for _, bad := range []string{"", "2.6.0", "v2.6", "v2.6.0-rc1", "v2.6.0+meta", "v02.6.0", "v2.06.0", "V2.6.0", "v2.6.0 ", "main-0123456789ab", "v2.5.0-120-gabcdef"} {
		if _, err := ReleaseTag(bad, mergeSHA, []string{"v2.5.0"}, []string{mergeSHA}); !errors.Is(err, ErrVersionGrammar) {
			t.Errorf("ReleaseTag(%q) err = %v; want ErrVersionGrammar", bad, err)
		}
	}

	// The newest v tag, numerically and ignoring every other tag.
	tags := []string{"v2.5.0", "v2.10.0", "v2.9.9", "main-0123456789ab", "v3.0.0-rc1", "latest"}
	newest, ok := NewestRelease(tags)
	if !ok || newest.String() != "v2.10.0" {
		t.Errorf("NewestRelease = %v, %v; want v2.10.0 (numeric order, non-release tags ignored)", newest, ok)
	}
	if _, ok := NewestRelease([]string{"main-0123456789ab"}); ok {
		t.Error("NewestRelease found a release among promotion tags only")
	}

	// Not greater than the newest v tag refuses, equal included.
	for _, v := range []string{"v2.10.0", "v2.9.10", "v1.99.99"} {
		if _, err := ReleaseTag(v, mergeSHA, tags, []string{mergeSHA}); !errors.Is(err, ErrNotGreater) {
			t.Errorf("ReleaseTag(%q) over newest v2.10.0 err = %v; want ErrNotGreater", v, err)
		}
	}

	// A commit not in main's ancestry refuses; a malformed one too.
	if _, err := ReleaseTag("v2.10.1", otherSHA, tags, []string{mergeSHA}); !errors.Is(err, ErrNotOnMain) {
		t.Errorf("ReleaseTag on a commit off main err = %v; want ErrNotOnMain", err)
	}
	if _, err := ReleaseTag("v2.10.1", "HEAD", tags, []string{mergeSHA}); !errors.Is(err, ErrNotACommit) {
		t.Errorf("ReleaseTag on a non-sha commit err = %v; want ErrNotACommit", err)
	}

	// And the one that passes names exactly the version.
	if got, err := ReleaseTag("v2.10.1", mergeSHA, tags, []string{otherSHA, mergeSHA}); err != nil || got != "v2.10.1" {
		t.Errorf("ReleaseTag(v2.10.1) = %q, %v; want v2.10.1", got, err)
	}
	if got, err := ReleaseTag("v0.1.0", mergeSHA, nil, []string{mergeSHA}); err != nil || got != "v0.1.0" {
		t.Errorf("ReleaseTag with no earlier release = %q, %v; want v0.1.0", got, err)
	}
}

// fakeGit answers the git commands the tool issues from a table and records
// every call. It never execs anything: no test here can make or push a tag.
type fakeGit struct {
	answers map[string]string
	calls   []string
}

func (f *fakeGit) Run(args ...string) (string, error) {
	cmd := strings.Join(args, " ")
	f.calls = append(f.calls, cmd)
	if out, ok := f.answers[cmd]; ok {
		return out, nil
	}
	if args[0] == "tag" && len(args) > 1 && args[1] != "--points-at" && args[1] != "--list" {
		return "", nil // creating a tag prints nothing
	}
	if args[0] == "push" {
		return "", nil
	}
	return "", fmt.Errorf("fake git: no answer for %q", cmd)
}

// writes is the calls that change a repository: making or pushing a tag.
func (f *fakeGit) writes() []string {
	var out []string
	for _, c := range f.calls {
		if strings.HasPrefix(c, "push") || (strings.HasPrefix(c, "tag ") && !strings.HasPrefix(c, "tag --points-at") && !strings.HasPrefix(c, "tag --list")) {
			out = append(out, c)
		}
	}
	return out
}

// TestPromoteAndReleaseDriveOnlyTheirOneTag pins what the make targets do to
// a repository, through a fake git: without push nothing is written; with push
// exactly one tag is made on exactly the one commit and only that tag is
// pushed; a refusal or a skip writes nothing.
func TestPromoteAndReleaseDriveOnlyTheirOneTag(t *testing.T) {
	t.Parallel()

	base := func() *fakeGit {
		return &fakeGit{answers: map[string]string{
			"rev-parse --verify HEAD^{commit}":             mergeSHA + "\n",
			"tag --points-at " + mergeSHA:                  "",
			"tag --list v*":                                "v2.4.0\nv2.5.0\n",
			"rev-list origin/main":                         mergeSHA + "\n" + otherSHA + "\n",
			"rev-parse --verify " + otherSHA + "^{commit}": otherSHA + "\n",
			"show v2.5.0:" + FormSource:                    formSrc(4),
			"show " + mergeSHA + ":" + FormSource:          formSrc(4),
		}}
	}

	// Promotion, dry: decides, writes nothing.
	g := base()
	if err := Promote(g, "HEAD", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if w := g.writes(); len(w) != 0 {
		t.Errorf("promotion without push wrote %q", w)
	}

	// Promotion, push: one lightweight tag, one push of that tag.
	g = base()
	if err := Promote(g, "HEAD", true, io.Discard); err != nil {
		t.Fatal(err)
	}
	want := []string{"tag main-0123456789ab " + mergeSHA, "push origin refs/tags/main-0123456789ab"}
	if w := g.writes(); !slices.Equal(w, want) {
		t.Errorf("promotion with push wrote %q; want %q", w, want)
	}

	// Promotion of an already tagged commit: skipped, nothing written.
	g = base()
	g.answers["tag --points-at "+mergeSHA] = "main-0123456789ab\n"
	if err := Promote(g, "HEAD", true, io.Discard); err != nil {
		t.Fatal(err)
	}
	if w := g.writes(); len(w) != 0 {
		t.Errorf("promotion of a tagged commit wrote %q", w)
	}

	// Release, push: one annotated tag on the commit, one push of that tag.
	g = base()
	if err := Release(g, "v2.6.0", "HEAD", "origin/main", true, io.Discard); err != nil {
		t.Fatal(err)
	}
	want = []string{"tag -a v2.6.0 -m schema v2.6.0 " + mergeSHA, "push origin refs/tags/v2.6.0"}
	if w := g.writes(); !slices.Equal(w, want) {
		t.Errorf("release with push wrote %q; want %q", w, want)
	}

	// Release, dry: nothing written.
	g = base()
	if err := Release(g, "v2.6.0", "HEAD", "origin/main", false, io.Discard); err != nil {
		t.Fatal(err)
	}
	if w := g.writes(); len(w) != 0 {
		t.Errorf("release without push wrote %q", w)
	}

	// Each refusal writes nothing, and names itself.
	for _, c := range []struct {
		version, commit string
		ancestry        string
		want            error
	}{
		{"2.6.0", "HEAD", mergeSHA, ErrVersionGrammar},
		{"v2.5.0", "HEAD", mergeSHA, ErrNotGreater},
		{"v2.6.0", "HEAD", otherSHA, ErrNotOnMain},
	} {
		g = base()
		g.answers["rev-list origin/main"] = c.ancestry + "\n"
		err := Release(g, c.version, c.commit, "origin/main", true, io.Discard)
		if !errors.Is(err, c.want) {
			t.Errorf("Release(%s) err = %v; want %v", c.version, err, c.want)
		}
		if w := g.writes(); len(w) != 0 {
			t.Errorf("refused release %s wrote %q", c.version, w)
		}
	}
}

// formSrc is a FormSource declaring form n, among the lines around it.
func formSrc(n int) string {
	return fmt.Sprintf("package ir\n\n// BuildVersionForm is the form.\nconst BuildVersionForm = %d\n\nfunc f() {}\n", n)
}

// TestFormGatePatchMinorMajor pins the build version's release policy
// (docs/VERSIONING.md, "The release policy") as a pure function: a patch
// release may not move the form; a minor and a major may; a patch that keeps
// the form passes.
func TestFormGatePatchMinorMajor(t *testing.T) {
	t.Parallel()
	v := func(s string) Version {
		t.Helper()
		x, err := ParseVersion(s)
		if err != nil {
			t.Fatal(err)
		}
		return x
	}
	for _, c := range []struct {
		next       string
		from, to   int
		wantRefuse bool
	}{
		{"v2.5.1", 3, 4, true},  // patch, form moved
		{"v2.5.1", 4, 3, true},  // patch, form moved back
		{"v2.5.1", 3, 3, false}, // patch, form kept
		{"v2.6.0", 3, 4, false}, // minor may, with the wire, announced
		{"v2.6.0", 3, 3, false},
		{"v3.0.0", 3, 4, false}, // major may
	} {
		err := FormGate(v("v2.5.0"), v(c.next), c.from, c.to)
		if got := errors.Is(err, ErrFormMoved); got != c.wantRefuse {
			t.Errorf("FormGate(v2.5.0 -> %s, form %d -> %d) = %v; want refusal %v", c.next, c.from, c.to, err, c.wantRefuse)
		}
	}

	if n, err := ParseBuildVersionForm(formSrc(4)); err != nil || n != 4 {
		t.Errorf("ParseBuildVersionForm = %d, %v; want 4", n, err)
	}
	for _, bad := range []string{"package ir\n", formSrc(3) + formSrc(4), "const BuildVersionForm = x\n"} {
		if _, err := ParseBuildVersionForm(bad); !errors.Is(err, ErrNoForm) {
			t.Errorf("ParseBuildVersionForm(%q) err = %v; want ErrNoForm", bad, err)
		}
	}
}

// TestReleaseFormGate drives the gate through Release and a fake git: the form
// is read out of FormSource at the newest v tag and at the commit, and a patch
// release that moves it refuses and writes nothing, while a minor and a major
// over the same move tag. release-form-gate-negative-control hands the gate
// the tag's form twice and requires this test red.
func TestReleaseFormGate(t *testing.T) {
	t.Parallel()
	for _, c := range []struct {
		version    string
		wantRefuse bool
	}{
		{"v2.5.1", true},
		{"v2.6.0", false},
		{"v3.0.0", false},
	} {
		g := &fakeGit{answers: map[string]string{
			"rev-parse --verify HEAD^{commit}":    mergeSHA + "\n",
			"tag --list v*":                       "v2.4.0\nv2.5.0\n",
			"rev-list origin/main":                mergeSHA + "\n",
			"show v2.5.0:" + FormSource:           formSrc(3),
			"show " + mergeSHA + ":" + FormSource: formSrc(4),
		}}
		err := Release(g, c.version, "HEAD", "origin/main", true, io.Discard)
		if got := errors.Is(err, ErrFormMoved); got != c.wantRefuse {
			t.Errorf("Release(%s) with the form moved 3 -> 4: err = %v; want refusal %v", c.version, err, c.wantRefuse)
		}
		if c.wantRefuse && len(g.writes()) != 0 {
			t.Errorf("refused release %s wrote %q", c.version, g.writes())
		}
		if !c.wantRefuse && len(g.writes()) != 2 {
			t.Errorf("release %s wrote %q; want its tag and its push", c.version, g.writes())
		}
	}
}
