package main

import (
	"errors"
	"io"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/modulemove"
	"github.com/mas-bandwidth/schema/v2/internal/modulemove/pincorpus"
)

// TestPinCorpusReleaseTagger drives the release-history cases of the
// committed corpus (internal/modulemove/pincorpus, releases/*.json, the ones
// marked for the tagger) through release and a fake git that answers from
// the case: the tag list, and the go.mod AT each tag. The pure rule runs the
// whole file in internal/modulemove; this pins that the tagger feeds it the
// real tags, reads go.mod at every v3 tag and no other, and writes nothing
// on a refusal. The reviewer's case is among them: boundary v3.0.3, tags
// through v3.0.5 cut with /v2, the first /v3 release going out as v3.0.6.
func TestPinCorpusReleaseTagger(t *testing.T) {
	t.Parallel()
	cases, err := pincorpus.Releases()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]error{
		"accept":          nil,
		"module_mismatch": modulemove.ErrModuleMismatch,
		"boundary_covers": modulemove.ErrBoundaryCovers,
		"boundary_stale":  modulemove.ErrBoundaryStale,
		"no_module":       modulemove.ErrNoModule,
	}
	suffix := func(s string) string { return strings.TrimPrefix(s, "/v") }
	ran := 0
	for _, c := range cases {
		if !c.Tagger {
			continue
		}
		ran++
		var tags []string
		for tag := range c.Tags {
			tags = append(tags, tag)
		}
		slices.Sort(tags)
		newest, ok := NewestRelease(tags)
		if !ok {
			t.Fatalf("%s: a tagger case needs a release already cut", c.ID)
		}
		g := &fakeGit{answers: map[string]string{
			"rev-parse --verify HEAD^{commit}":           mergeSHA + "\n",
			"tag --list v*":                              strings.Join(tags, "\n") + "\nv3.0.5-promote\n",
			"rev-list origin/main":                       mergeSHA + "\n",
			"show " + newest.String() + ":" + FormSource: formSrc(4),
			"show " + mergeSHA + ":" + FormSource:        formSrc(4),
			"show " + mergeSHA + ":go.mod":               goModSrc(suffix(c.GoMod)),
		}}
		// only a v3 release tag is read: a v2 tag, and the tag that is not
		// a release, have no answer and would fail the run
		for tag, mod := range c.Tags {
			if strings.HasPrefix(tag, "v3.") {
				g.answers["show "+tag+":go.mod"] = goModSrc(suffix(mod))
			}
		}
		err := release(g, c.Version, "HEAD", "origin/main", true, io.Discard, c.Boundary)
		if !errors.Is(err, want[c.Verdict]) || (c.Verdict == "accept") != (err == nil) {
			t.Errorf("%s (%s): release %s with boundary %q = %v; want %s", c.ID, c.Finding, c.Version, c.Boundary, err, c.Verdict)
		}
		if c.Verdict != "accept" && len(g.writes()) != 0 {
			t.Errorf("%s: the refused release wrote %q", c.ID, g.writes())
		}
		if c.Verdict == "accept" && len(g.writes()) != 2 {
			t.Errorf("%s: the release wrote %q; want its tag and its push", c.ID, g.writes())
		}
	}
	if ran == 0 {
		t.Error("the corpus holds no release case for the tagger")
	}
}
