package languages

import (
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
)

// root is the repository root, relative to this package.
const root = "../.."

// listPages are the hand-written pages that state the language list. Each
// must carry Summary() verbatim (line wrapping aside), so a language added,
// renamed or moved out from behind its tag fails here until the prose says so.
var listPages = []string{
	"README.md",
	"docs/SPEC.md",
	"docs/CONTRIBUTING.md",
	"docs/FAQ.md",
}

func readPage(t *testing.T, page string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join(root, page))
	if err != nil {
		t.Fatalf("read %s: %v", page, err)
	}
	return string(b)
}

// skipTaggedBuild skips a page check in a build that registers a tagged
// generator: the pages state the DEFAULT build, and under the tag that
// language is a built one.
func skipTaggedBuild(t *testing.T) {
	t.Helper()
	targets := compiler.New().Targets()
	for _, tag := range sourceTagged(t) {
		if slices.Contains(targets, tag) {
			t.Skipf("this build registers %s (tag %s%s); the pages state the default build", tag, TagPrefix, tag)
		}
	}
}

// sourceTagged returns every target whose compiler/target_<t>.go is behind a
// schema_leg_<t> build constraint, read from the sources.
func sourceTagged(t *testing.T) []string {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(root, "compiler", "target_*.go"))
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`^target_([a-z0-9]+)\.go$`)
	var out []string
	for _, f := range files {
		m := re.FindStringSubmatch(filepath.Base(f))
		if m == nil {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		first, _, _ := strings.Cut(string(b), "\n")
		if first == "//go:build "+TagPrefix+m[1] {
			out = append(out, m[1])
		}
	}
	slices.Sort(out)
	return out
}

func TestSummaryOnEveryListPage(t *testing.T) {
	skipTaggedBuild(t)
	want := Summary()
	space := regexp.MustCompile(`\s+`)
	for _, page := range listPages {
		got := space.ReplaceAllString(readPage(t, page), " ")
		if !strings.Contains(got, want) {
			t.Errorf("%s does not state the language list as the registry does; it must contain, verbatim:\n%s", page, want)
		}
	}
}

// reservedPages state the reserved-word languages (SPEC §4.6): the rows of the
// checker's one table, by display name, in its order. C# is one row there for
// both C# generators, since they reserve the same words.
var reservedPages = []string{
	"docs/SPEC.md",
	"docs/USAGE.md",
}

func TestReservedWordLanguagesOnEveryPage(t *testing.T) {
	want := englishList(check.ReservedLanguages())
	space := regexp.MustCompile(`\s+`)
	for _, page := range reservedPages {
		got := space.ReplaceAllString(readPage(t, page), " ")
		if !strings.Contains(got, want) {
			t.Errorf("%s does not list the reserved-word languages as internal/check's table does; it must contain, verbatim:\n%s", page, want)
		}
	}
}

// TestSpecRuntimeTableNamesEveryLanguage holds SPEC §1's target/runtime table
// to the same list: one row per language, by its prose name.
func TestSpecRuntimeTableNamesEveryLanguage(t *testing.T) {
	skipTaggedBuild(t)
	spec := readPage(t, "docs/SPEC.md")
	_, rest, ok := strings.Cut(spec, "| target | runtime library |\n|---|---|\n")
	if !ok {
		t.Fatal("docs/SPEC.md: the §1 table header `| target | runtime library |` is gone")
	}
	table, _, _ := strings.Cut(rest, "\n\n")
	var got []string
	for line := range strings.SplitSeq(table, "\n") {
		cells := strings.Split(line, "|")
		if len(cells) < 3 {
			t.Fatalf("docs/SPEC.md §1 table: malformed row %q", line)
		}
		got = append(got, strings.TrimSpace(cells[1]))
	}
	var want []string
	for _, l := range All() {
		want = append(want, l.Name)
	}
	slices.Sort(got)
	slices.Sort(want)
	if !slices.Equal(got, want) {
		t.Errorf("docs/SPEC.md §1 table rows %v; the registry's languages are %v", got, want)
	}
}

// TestTaggedLanguagesAreTheTaggedSources: in the default build, a language the
// reserved-word table names but no generator registers is exactly one whose
// target file sits behind its schema_leg_ tag. A row with no generator at all
// would otherwise be printed as a language that exists behind a tag.
func TestTaggedLanguagesAreTheTaggedSources(t *testing.T) {
	skipTaggedBuild(t)
	var got []string
	for _, l := range All() {
		if l.Tag != "" {
			got = append(got, l.Target)
		}
	}
	slices.Sort(got)
	if want := sourceTagged(t); !slices.Equal(got, want) {
		t.Errorf("languages with no registered generator %v; target files behind a %s tag %v", got, TagPrefix, want)
	}
}

// TestUnityIsNeverALanguage pins the naming ruling: the two C# generators are
// "C# (modern)" and "C# (unity)", and "Unity" alone is never listed.
func TestUnityIsNeverALanguage(t *testing.T) {
	names := map[string]string{}
	for _, l := range All() {
		names[l.Target] = l.Name
	}
	if names["cs"] != "C# (modern)" || names["unity"] != "C# (unity)" {
		t.Errorf("cs is %q and unity is %q; want \"C# (modern)\" and \"C# (unity)\"", names["cs"], names["unity"])
	}
	for _, l := range All() {
		if l.Name == "Unity" || l.Name == "C#" {
			t.Errorf("%s is listed as %q", l.Target, l.Name)
		}
	}
}

func TestEnglishList(t *testing.T) {
	for _, c := range []struct {
		in   []string
		want string
	}{
		{nil, ""},
		{[]string{"C"}, "C"},
		{[]string{"C", "Go"}, "C and Go"},
		{[]string{"C", "Go", "Rust"}, "C, Go and Rust"},
	} {
		if got := englishList(c.in); got != c.want {
			t.Errorf("englishList(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
