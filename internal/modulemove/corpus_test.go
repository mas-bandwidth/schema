package modulemove

import (
	"errors"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v3/internal/modulemove/pincorpus"
)

// goModSuffix is a go.mod declaring this module under a corpus suffix: "/vN",
// "base" for the unversioned path, "missing" for no module line. The module
// line is assembled from parts so the module-path move program leaves it
// alone: the corpus names a major on purpose.
func goModSuffix(suffix string) string {
	switch suffix {
	case "missing":
		return "go 1.26\n"
	case "base":
		suffix = ""
	}
	return "module " + moduleBase + suffix + "\n\ngo 1.26\n\nrequire pgregory.net/rapid v1.3.0\n"
}

// corpusPath is the module path a corpus suffix stands for.
func corpusPath(suffix string) string {
	if suffix == "base" {
		return moduleBase
	}
	return moduleBase + suffix
}

// TestPinCorpusReleases runs every release-history case of the corpus
// (pincorpus, releases/*.json) through the gate it names: a tag's go.mod must
// carry its major unless pin's boundary covers the tag, a tag that resolves
// must not be covered, and a boundary, once set, is held against the tags
// already cut. The rules read no git: the history is the case's own map.
func TestPinCorpusReleases(t *testing.T) {
	t.Parallel()
	cases, err := pincorpus.Releases()
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]error{
		"accept":          nil,
		"module_mismatch": ErrModuleMismatch,
		"boundary_covers": ErrBoundaryCovers,
		"boundary_stale":  ErrBoundaryStale,
		"no_module":       ErrNoModule,
	}
	ran := 0
	for _, c := range cases {
		var tags map[string]string
		if c.Tags != nil {
			tags = map[string]string{}
			for tag, suffix := range c.Tags {
				tags[tag] = corpusPath(suffix)
			}
		}
		var err error
		switch c.Gate {
		case "release":
			err = ReleaseGate(c.Version, goModSuffix(c.GoMod), c.Boundary, tags)
			if err != nil && !strings.Contains(err.Error(), c.Version) {
				t.Errorf("%s: the refusal does not name the version: %v", c.ID, err)
			}
		case "boundary":
			err = BoundaryGate(c.Boundary, tags)
		case "tree":
			err = TreeGate(goModSuffix(c.GoMod), c.Boundary)
		}
		ran++
		if !errors.Is(err, want[c.Verdict]) || (c.Verdict == "accept") != (err == nil) {
			t.Errorf("%s (%s, %s): the %s gate = %v; want %s", c.ID, c.Finding, c.File, c.Gate, err, c.Verdict)
		}
	}
	if ran != len(cases) || ran == 0 {
		t.Errorf("ran %d of %d release cases", ran, len(cases))
	}
}

// TestPinCorpusSpellingsGrammar runs every version spelling of the corpus
// (versions/*.json) through the rules pin is built from. An accepted
// spelling is a version, a lawful pseudo-version if it is in that shape,
// takes the module path the case names, and is not below the case's
// boundary. A refused one fails at least one of those, or carries build
// metadata, which pin refuses on its own. cmd/schema runs the same cases
// through pin's decision and holds the refusal to its reason.
func TestPinCorpusSpellingsGrammar(t *testing.T) {
	t.Parallel()
	cases, err := pincorpus.Spellings()
	if err != nil {
		t.Fatal(err)
	}
	ran := 0
	for _, c := range cases {
		canonical, meta, _ := strings.Cut(c.Version, "+")
		lawful := ValidVersion(c.Version) && CheckPseudoVersion(c.Version) == nil
		accepted := lawful && meta == "" && !PreModuleMove(canonical, c.Boundary)
		ran++
		if accepted != (c.Verdict == "accept") {
			t.Errorf("%s (%s, %s): %s under boundary %q is accepted = %v; want %s", c.ID, c.Finding, c.File, c.Version, c.Boundary, accepted, c.Verdict)
			continue
		}
		if accepted {
			if got := PathFor(c.Version); got != moduleBase+c.Path {
				t.Errorf("%s: PathFor(%s) = %s; want the base path plus %q", c.ID, c.Version, got, c.Path)
			}
		}
	}
	if ran != len(cases) || ran == 0 {
		t.Errorf("ran %d of %d spellings", ran, len(cases))
	}
}
