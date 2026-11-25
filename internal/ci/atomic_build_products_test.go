package ci

// SHARED BUILD PRODUCTS ARE WRITTEN ATOMICALLY. Two schema-test runs in one
// checkout that both make bin/schema (or the fixed-form dump, its corpus, or
// the C conformance driver) raced: one run's link wrote the binary in place
// while the other was executing it ("Text file busy"), and the corpus build
// cleared and refilled one shared .next directory under another build's
// feet. Each product is now written under a name of its build's own (the
// shell's pid) and renamed into place, so a reader never sees it half written
// and a running process keeps the inode it started from.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// atomicBinary is a shared binary and the make file whose rule links it.
type atomicBinary struct{ file, target string }

var atomicBinaries = []atomicBinary{
	{"Makefile", "bin/schema"},
	{"Makefile", "build/schema_test_fixedform_dump"},
	{filepath.Join("make", "c.mk"), "build/conformance-c"},
}

// The corpus rule is the one the goal tables-fixedform-corpus depends on,
// read from the Makefile (corpusTarget), so this test names no build output
// as a path of its own: it reads make files, not the corpus.
const atomicCorpusFile, atomicCorpusGoal = "Makefile", "tables-fixedform-corpus"

// corpusTarget is the one prerequisite of the corpus goal's rule in src.
func corpusTarget(src string) (string, bool) {
	for _, line := range strings.Split(src, "\n") {
		head, prereqs, ok := strings.Cut(line, ":")
		if ok && head == atomicCorpusGoal && !strings.HasPrefix(prereqs, "=") {
			if f := strings.Fields(prereqs); len(f) == 1 {
				return f[0], true
			}
		}
	}
	return "", false
}

// ruleRecipe is the recipe of the rule in src whose target list is exactly
// target and which has one, its continuation lines joined; column-0 comment
// lines inside it are make's comments and are passed over.
func ruleRecipe(src, target string) (string, bool) {
	lines := strings.Split(src, "\n")
	for i, line := range lines {
		head, _, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(line, "\t") || strings.TrimSpace(head) != target || strings.HasPrefix(line[len(head):], ":=") {
			continue
		}
		var recipe []string
		for j := i + 1; j < len(lines); j++ {
			l := lines[j]
			if strings.HasPrefix(l, "#") {
				continue
			}
			if !strings.HasPrefix(l, "\t") {
				break
			}
			recipe = append(recipe, strings.TrimSpace(l))
		}
		if len(recipe) > 0 {
			return strings.ReplaceAll(strings.Join(recipe, "\n"), "\\\n", " "), true
		}
	}
	return "", false
}

var (
	// a link straight onto the target: -o $@ followed by a space or the end
	linkInPlace = regexp.MustCompile(`-o \$@(\s|$)`)
	// the shared .next or .prev directory, not one named for its build
	corpusSharedTemp = regexp.MustCompile(`build/fixedform-corpus\.(next|prev)(\s|/|;|$)`)
)

// atomicWriteProblems names every shared build product in files (make file
// path to its text) that is not written under its build's own name and
// renamed into place.
func atomicWriteProblems(files map[string]string) []string {
	var bad []string
	for _, b := range atomicBinaries {
		recipe, ok := ruleRecipe(files[b.file], b.target)
		switch {
		case !ok:
			bad = append(bad, b.file+": no rule with a recipe makes "+b.target)
		case linkInPlace.MatchString(recipe):
			bad = append(bad, b.file+": "+b.target+" is linked in place (-o $@), so a run executing it sees Text file busy")
		case !strings.Contains(recipe, "-o $@.tmp.$$$$") || !strings.Contains(recipe, "mv -f $@.tmp.$$$$ $@"):
			bad = append(bad, b.file+": "+b.target+" is not linked to $@.tmp.$$$$ and renamed onto $@ with mv -f")
		}
	}
	target, ok := corpusTarget(files[atomicCorpusFile])
	if !ok {
		return append(bad, atomicCorpusFile+": "+atomicCorpusGoal+" has no rule naming the one corpus rule it depends on")
	}
	recipe, ok := ruleRecipe(files[atomicCorpusFile], target)
	switch {
	case !ok:
		bad = append(bad, atomicCorpusFile+": no rule with a recipe makes "+target)
	case corpusSharedTemp.MatchString(recipe):
		bad = append(bad, atomicCorpusFile+": "+target+" writes a shared build/fixedform-corpus.next or .prev, which another build clears under it")
	case !strings.Contains(recipe, "next=build/fixedform-corpus.next.$$$$") || !strings.Contains(recipe, "rename($$ARGV[0], $$ARGV[1])"):
		bad = append(bad, atomicCorpusFile+": "+target+" does not write build/fixedform-corpus.next.$$$$ and rename(2) it into place")
	}
	return bad
}

func readAtomicMakeFiles(t *testing.T) map[string]string {
	t.Helper()
	root := repoRoot(t)
	files := map[string]string{}
	for _, f := range []string{"Makefile", filepath.Join("make", "c.mk")} {
		b, err := os.ReadFile(filepath.Join(root, f))
		if err != nil {
			t.Fatal(err)
		}
		files[f] = string(b)
	}
	return files
}

func TestSharedBuildProductsAreWrittenAtomically(t *testing.T) {
	files := readAtomicMakeFiles(t)
	if bad := atomicWriteProblems(files); len(bad) > 0 {
		t.Fatalf("shared build products written in place:\n%s", strings.Join(bad, "\n"))
	}

	// The controls: each product's atomic write planted out, back to the
	// in-place write it replaced, is named by its target.
	plants := []struct{ file, from, to, want string }{
		{"Makefile", `-o $@.tmp.$$$$ ./cmd/schema && mv -f $@.tmp.$$$$ $@ || { rm -f $@.tmp.$$$$; exit 1; }`, `-o $@ ./cmd/schema`, "bin/schema"},
		{"Makefile", `test/tables/fixedform_dump.cpp -o $@.tmp.$$$$ && mv -f $@.tmp.$$$$ $@ || { rm -f $@.tmp.$$$$; exit 1; }`, `test/tables/fixedform_dump.cpp -o $@`, "build/schema_test_fixedform_dump"},
		{filepath.Join("make", "c.mk"), `-o $@.tmp.$$$$ -lm && mv -f $@.tmp.$$$$ $@ || { rm -f $@.tmp.$$$$; exit 1; }`, `-o $@ -lm`, "build/conformance-c"},
		{"Makefile", `next=build/fixedform-corpus.next.$$$$;`, `next=build/fixedform-corpus.next;`, "fixedform-corpus/.stamp"},
	}
	for _, p := range plants {
		planted := map[string]string{}
		for k, v := range files {
			planted[k] = v
		}
		if strings.Count(files[p.file], p.from) != 1 {
			t.Fatalf("plant for %s: %q is not in %s exactly once", p.want, p.from, p.file)
		}
		planted[p.file] = strings.Replace(files[p.file], p.from, p.to, 1)
		bad := atomicWriteProblems(planted)
		if len(bad) != 1 || !strings.Contains(bad[0], p.want) {
			t.Errorf("%s written in place: want one problem naming it, got %q", p.want, bad)
		}
	}
}
