package ci

// A `go test -run` pattern that matches no test is still a green gate: go test
// exits 0 and prints `ok` when -run selects nothing, and the trailing
// `[no tests to run]` is the only evidence, on a line nobody re-reads.
// schema#1139 found exactly this on the js leg, whose tests are named
// TestJSFixedVersioning... while the documented gate said TestFixedVersioning;
// the fixed-form machinery is not on main, so this gate is the
// branch-independent half. The check is `go test -list`, which prints the
// matching test names and none when nothing matches, so an empty listing is
// the refusal. A pattern that cannot be listed is permitted only by an
// explicit exception carrying a reason, which the gate refuses to leave empty.
// The tree measured clean when this gate landed.

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// runRecipe is one `go test ... -run <pattern>` recipe line.
type runRecipe struct {
	where   string // "make/go.mk:399"
	pattern string // the regex handed to -run, with $$ folded to $
	pkg     string // the repo-relative package, or "" when unresolvable
}

// runArg matches the -run argument in its three spellings: single-quoted,
// double-quoted, and bare, where \S+ stops at a space or a `> redirect` alike.
var runArg = regexp.MustCompile(`-run\s+('[^']*'|"[^"]*"|\S+)`)

// runRecipes extracts every `go test ... -run` recipe from one makefile's text.
// A recipe may wrap across lines with a trailing backslash; the extractor joins
// those continuations the same way ci_test.go does and reports each -run at the
// physical line it sits on, so an exception can name `file:line` and stay
// pinned to it.
func runRecipes(name, text string) []runRecipe {
	physical := strings.Split(text, "\n")

	type logical struct {
		text     string
		segStart []int // byte offset in text where each source line begins
		segLine  []int // physical line number of each source line
	}
	var logicals []logical
	for i := 0; i < len(physical); {
		var sb strings.Builder
		var segStart, segLine []int
		for i < len(physical) {
			segStart = append(segStart, sb.Len())
			segLine = append(segLine, i+1)
			line := physical[i]
			trimmed := strings.TrimRight(line, " \t")
			if strings.HasSuffix(trimmed, "\\") {
				sb.WriteString(strings.TrimSuffix(trimmed, "\\"))
				sb.WriteString(" ")
				i++
				continue
			}
			sb.WriteString(line)
			i++
			break
		}
		logicals = append(logicals, logical{text: sb.String(), segStart: segStart, segLine: segLine})
	}

	var recipes []runRecipe
	for _, ll := range logicals {
		for _, m := range goInvocation.FindAllStringIndex(ll.text, -1) {
			inv := ll.text[m[0]:m[1]]
			fields := strings.Fields(inv)
			if len(fields) < 2 || fields[0] != "go" || fields[1] != "test" {
				continue
			}
			rm := runArg.FindStringSubmatchIndex(inv)
			if rm == nil {
				continue
			}
			pattern := inv[rm[2]:rm[3]]
			if len(pattern) >= 2 && ((pattern[0] == '\'' && pattern[len(pattern)-1] == '\'') ||
				(pattern[0] == '"' && pattern[len(pattern)-1] == '"')) {
				pattern = pattern[1 : len(pattern)-1]
			}
			pattern = strings.ReplaceAll(pattern, "$$", "$")

			var pkg string
			if pm := goPackageArg.FindStringSubmatch(inv); pm != nil {
				pkg = strings.TrimSuffix(pm[1], "/")
			}

			off := m[0] + rm[0]
			idx := sort.Search(len(ll.segStart), func(k int) bool { return ll.segStart[k] > off }) - 1
			recipes = append(recipes, runRecipe{
				where:   fmt.Sprintf("%s:%d", name, ll.segLine[idx]),
				pattern: pattern,
				pkg:     pkg,
			})
		}
	}
	return recipes
}

// makefileSet is the Makefile plus every file it includes, in include order.
// The list comes out of the Makefile's own include lines rather than a glob
// typed here, so a file included tomorrow is read tomorrow. makeSources at
// ci_test.go:237 globs make/*.mk and misses make/checks/, so this gate builds
// its own set.
func makefileSet(root string) ([]string, error) {
	top := filepath.Join(root, "Makefile")
	body, err := os.ReadFile(top)
	if err != nil {
		return nil, err
	}
	files := []string{top}
	seen := map[string]bool{top: true}
	for line := range strings.SplitSeq(string(body), "\n") {
		trimmed := strings.TrimSpace(line)
		rest, ok := strings.CutPrefix(trimmed, "include ")
		if !ok {
			rest, ok = strings.CutPrefix(trimmed, "-include ")
		}
		if !ok {
			continue
		}
		if inner, ok := strings.CutPrefix(strings.TrimSpace(rest), "$(wildcard"); ok {
			rest = strings.TrimSuffix(strings.TrimSpace(inner), ")")
		}
		for field := range strings.FieldsSeq(rest) {
			pattern := strings.TrimSpace(field)
			if pattern == "" || strings.Contains(pattern, "$") {
				return nil, fmt.Errorf("include line %q carries a variable this reader does not expand", trimmed)
			}
			matches, err := filepath.Glob(filepath.Join(root, pattern))
			if err != nil {
				return nil, err
			}
			sort.Strings(matches)
			for _, match := range matches {
				if !seen[match] {
					seen[match] = true
					files = append(files, match)
				}
			}
		}
	}
	return files, nil
}

// makefileFiles reads every file in the set, keyed by its repo-relative path.
func makefileFiles(t *testing.T, root string) map[string]string {
	t.Helper()
	paths, err := makefileSet(root)
	if err != nil {
		t.Fatal(err)
	}
	files := make(map[string]string, len(paths))
	for _, p := range paths {
		rel, err := filepath.Rel(root, p)
		if err != nil {
			rel = p
		}
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		files[rel] = string(data)
	}
	return files
}

// listNames shells out to `go test -list`, which compiles the package and
// prints the matching test names, and none when nothing matches. An empty
// result is the refusal the gate is built around.
func listNames(t *testing.T, root, pattern, pkg string) []string {
	t.Helper()
	cmd := exec.Command("go", "test", "-list", pattern, pkg)
	cmd.Dir = root
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go test -list %q %s: %v\n%s", pattern, pkg, err, out)
	}
	var names []string
	for line := range strings.SplitSeq(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.Contains(line, "\t") {
			continue
		}
		names = append(names, line)
	}
	return names
}

// runPatternException is one -run recipe whose pattern cannot be resolved
// statically, and the reason it may stay. An exception is a choice somebody
// wrote down; the gate refuses an empty reason and a stale where, so a pattern
// cannot leave the gate silently.
type runPatternException struct {
	where string // "make/go.mk:163"
	why   string // why the pattern cannot be listed
}

// runPatternExceptions are the -run lines the gate cannot list: six run inside
// test/go-tables, a separate Go module whose generated replacements do not
// exist until `make` builds them, and one is a $(3) template with no value
// until the caller expands it.
var runPatternExceptions = []runPatternException{
	{"make/go.mk:163", "cd test/go-tables && go test ... . — test/go-tables is a separate Go module whose generated replacements do not exist until make builds them, so -list cannot run from the repo root"},
	{"make/go.mk:168", "same separate test/go-tables module, -run Fuzz"},
	{"make/go.mk:169", "same separate test/go-tables module, -run Fuzz under -race"},
	{"make/go.mk:206", "same separate test/go-tables module, and it runs under a -overlay built at recipe time"},
	{"make/go.mk:447", "same separate test/go-tables module, -run '^TestSoak$$'"},
	{"make/go.mk:462", "same separate test/go-tables module, -run '^TestUsage$$'"},
	{"Makefile:4934", "inside define message_form_control: $(3) is the caller's test name, so the pattern has no value until expansion"},
}

// checkRunPatterns is the shared code path for both tests. It extracts every
// go test -run recipe from files, refuses an empty set, holds the exception
// list tight, dedupes the (pattern, package) pairs, and lists each one. It
// returns one message per recipe whose pattern matches no test.
func checkRunPatterns(t *testing.T, root string, files map[string]string, exceptions []runPatternException) []string {
	t.Helper()
	var recipes []runRecipe
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		recipes = append(recipes, runRecipes(n, files[n])...)
	}
	if len(recipes) == 0 {
		t.Fatal("no go test -run recipes found in the makefile set: this gate is looking at the wrong text")
	}

	byWhere := map[string]bool{}
	for _, r := range recipes {
		byWhere[r.where] = true
	}
	except := map[string]bool{}
	for _, e := range exceptions {
		if strings.TrimSpace(e.why) == "" {
			t.Errorf("the -run exception at %s carries no reason: a pattern that cannot be listed may not leave the gate silently", e.where)
		}
		if !byWhere[e.where] {
			t.Errorf("the -run exception at %s no longer names a go test -run recipe line: drop it or update the line", e.where)
		}
		except[e.where] = true
	}

	type pair struct{ pattern, pkg string }
	seen := map[pair]bool{}
	var offenders []string
	scanned, listed := 0, 0
	for _, r := range recipes {
		scanned++
		if except[r.where] {
			continue
		}
		p := pair{r.pattern, r.pkg}
		if seen[p] {
			continue
		}
		seen[p] = true
		listed++
		if got := listNames(t, root, r.pattern, r.pkg); len(got) == 0 {
			offenders = append(offenders, fmt.Sprintf("%s: `go test ... -run %q` matches no test in %s", r.where, r.pattern, r.pkg))
		}
	}
	t.Logf("scanned %d -run recipes, listed %d distinct (pattern, package) pairs", scanned, listed)
	return offenders
}

func TestRunPatternGateRefusesAZeroMatchPattern(t *testing.T) {
	root := repoRoot(t)
	const fixture = `go test ./internal/ci -run '^TestEveryActionIsPinnedToACommit$$' -count=1
go test ./internal/ci -run '^TestNoSuchTestExistsAnywhere$$' -count=1
go test ./internal/ci \
	-run '^TestEveryPackageTheBuildRunsIsCommitted$$' -count=1
`
	offenders := checkRunPatterns(t, root, map[string]string{"fixture.mk": fixture}, nil)
	if len(offenders) != 1 {
		t.Fatalf("want exactly one offender, got %d: %v", len(offenders), offenders)
	}
	if !strings.Contains(offenders[0], "TestNoSuchTestExistsAnywhere") {
		t.Fatalf("the offender does not name the zero-match pattern: %s", offenders[0])
	}
}

func TestEveryRunPatternMatchesATest(t *testing.T) {
	root := repoRoot(t)
	offenders := checkRunPatterns(t, root, makefileFiles(t, root), runPatternExceptions)
	for _, o := range offenders {
		t.Errorf("%s", o)
	}
}
