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
	"llms.txt",
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

// skipTaggedBuild skips a check about which generators are tagged in a build
// that registers a tagged one: under the tag that language is a built one.
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
// checker's one table, in its order, each by its prose name (LanguageName). C# is one row there for
// both C# generators, since they reserve the same words.
var reservedPages = []string{
	"docs/SPEC.md",
	"docs/USAGE.md",
}

func TestReservedWordLanguagesOnEveryPage(t *testing.T) {
	var names []string
	for _, lang := range check.ReservedLanguages() {
		names = append(names, LanguageName(lang))
	}
	want := englishList(names)
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
	allAll := All()
	if len(allAll) == 0 {
		t.Fatal("All() is empty: the loop below would check nothing")
	}
	for _, l := range allAll {
		if l.Name == "Unity" || l.Name == "C#" {
			t.Errorf("%s is listed as %q", l.Target, l.Name)
		}
	}
}

// ownerList is the owner's canonical prose list: eighteen targets, seventeen
// languages, since the two C# generators are one language.
const ownerList = "C, C++, C# (modern), C# (unity), Dart, Elixir, Go, Goose, Java, JavaScript, Lobster, Lua, Luau, Moo, Python, Ruby, Rust and Squirrel"

// TestSummaryBothTagStates injects the entry list for both states of the moo
// build tag: the generator behind schema_leg_moo, and the tag dropped. The
// list reads the same in both, with no clause left behind and no count, and
// the counts stay seventeen languages and eighteen targets.
func TestSummaryBothTagStates(t *testing.T) {
	var untagged []Language
	for _, target := range []string{"c", "cpp", "cs", "unity", "dart", "elixir", "go", "goose", "java", "js", "lobster", "lua", "luau", "moo", "python", "ruby", "rust", "squirrel"} {
		untagged = append(untagged, entry(target, ""))
	}
	tagged := slices.Clone(untagged)
	for i := range tagged {
		if tagged[i].Target == "moo" {
			tagged[i].Tag = TagPrefix + "moo"
		}
	}
	for _, c := range []struct {
		name    string
		entries []Language
	}{
		{"moo behind its tag", tagged},
		{"moo in the default build", untagged},
	} {
		t.Run(c.name, func(t *testing.T) {
			entries := slices.Clone(c.entries)
			slices.Reverse(entries)
			sortByName(entries)
			got := summary(entries)
			if got != ownerList {
				t.Errorf("summary = %q\nwant      %q", got, ownerList)
			}
			for _, bad := range []string{"eighteen languages", "18 languages", "schema_leg_", "-tags"} {
				if strings.Contains(got, bad) {
					t.Errorf("summary carries %q: %q", bad, got)
				}
			}
			if n := languages(entries); n != 17 {
				t.Errorf("languages = %d, want 17", n)
			}
			if n := len(entries); n != 18 {
				t.Errorf("targets = %d, want 18", n)
			}
		})
	}
}

// TestSummaryIsTheOwnerList pins the live derivation to the owner's list.
func TestSummaryIsTheOwnerList(t *testing.T) {
	if got := Summary(); got != ownerList {
		t.Errorf("Summary() = %q\nwant        %q", got, ownerList)
	}
	if Languages() != 17 || Targets() != 18 {
		t.Errorf("Languages() = %d, Targets() = %d; want 17 and 18", Languages(), Targets())
	}
}

// countPages state the list or talk about all of it; a count of languages on
// them is Languages() and a count of targets, generators, legs or backends is
// Targets(). Counts below six are left alone ("those three carry ...").
var countPages = []string{
	"AGENTS.md",
	"README.md",
	"llms.txt",
	"docs/AI.md",
	"docs/SPEC.md",
	"docs/CONTRIBUTING.md",
	"docs/FAQ.md",
	"cmd/schema/help/ai.md",
	"skills/schema/SKILL.md",
}

var numberWords = map[string]int{
	"six": 6, "seven": 7, "eight": 8, "nine": 9, "ten": 10, "eleven": 11,
	"twelve": 12, "thirteen": 13, "fourteen": 14, "fifteen": 15, "sixteen": 16,
	"seventeen": 17, "eighteen": 18, "nineteen": 19, "twenty": 20,
}

func TestNoPageMiscountsLanguages(t *testing.T) {
	word := `(?i)\b(six|seven|eight|nine|ten|eleven|twelve|thirteen|fourteen|fifteen|sixteen|seventeen|eighteen|nineteen|twenty|[6-9]|[1-9][0-9])\s+(?:generated\s+|target\s+|code\s+)?`
	langRe := regexp.MustCompile(word + `languages\b`)
	targetRe := regexp.MustCompile(word + `(?:targets|generators|legs|backends)\b`)
	space := regexp.MustCompile(`\s+`)
	value := func(s string) int {
		if n, ok := numberWords[strings.ToLower(s)]; ok {
			return n
		}
		n := 0
		for _, r := range s {
			n = n*10 + int(r-'0')
		}
		return n
	}
	for _, page := range countPages {
		text := space.ReplaceAllString(readPage(t, page), " ")
		for _, m := range langRe.FindAllStringSubmatch(text, -1) {
			if value(m[1]) != Languages() {
				t.Errorf("%s says %q; there are %d languages (%d targets)", page, m[0], Languages(), Targets())
			}
		}
		for _, m := range targetRe.FindAllStringSubmatch(text, -1) {
			if value(m[1]) != Targets() {
				t.Errorf("%s says %q; there are %d targets", page, m[0], Targets())
			}
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
