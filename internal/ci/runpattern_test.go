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
	"runtime"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// runRecipe is one `go test ... -run <pattern>` recipe line.
type runRecipe struct {
	where   string // "make/go.mk:438"
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
			if before, ok := strings.CutSuffix(trimmed, "\\"); ok {
				sb.WriteString(before)
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

// listPackages compiles each package once with `go test -list .*`, which
// prints every top-level test, benchmark, fuzz test and example in it, and
// returns the names by package. The packages compile side by side, one per
// processor: a package listed once per (pattern, package) pair, one after
// another, was most of this gate's minutes.
func listPackages(t *testing.T, root string, pkgs []string) map[string][]string {
	t.Helper()
	type listing struct {
		pkg   string
		names []string
		err   error
		out   []byte
	}
	results := make([]listing, len(pkgs))
	sem := make(chan struct{}, runtime.GOMAXPROCS(0))
	var wg sync.WaitGroup
	for i, pkg := range pkgs {
		wg.Go(func() {
			sem <- struct{}{}
			defer func() { <-sem }()
			cmd := exec.Command("go", "test", "-list", ".*", pkg)
			cmd.Dir = root
			out, err := cmd.Output()
			r := listing{pkg: pkg, err: err, out: out}
			for line := range strings.SplitSeq(string(out), "\n") {
				line = strings.TrimSpace(line)
				if line == "" || strings.Contains(line, "\t") {
					continue
				}
				r.names = append(r.names, line)
			}
			results[i] = r
		})
	}
	wg.Wait()
	names := map[string][]string{}
	for _, r := range results {
		if r.err != nil {
			t.Fatalf("go test -list .* %s: %v\n%s", r.pkg, r.err, r.out)
		}
		names[r.pkg] = r.names
	}
	return names
}

// matchNames is what `go test -list pattern` prints of a package's listing:
// the names the whole pattern matches (-list does not split it at slashes).
// An empty result is the refusal the gate is built around.
func matchNames(t *testing.T, pattern string, listed []string) []string {
	t.Helper()
	re, err := regexp.Compile(pattern)
	if err != nil {
		t.Fatalf("go test -list %q: %v", pattern, err)
	}
	var names []string
	for _, n := range listed {
		if re.MatchString(n) {
			names = append(names, n)
		}
	}
	return names
}

// runPatternException is one -run recipe whose pattern cannot be resolved
// statically, and the reason it may stay. An exception is a choice somebody
// wrote down; the gate refuses an empty reason and a stale where, so a pattern
// cannot leave the gate silently.
type runPatternException struct {
	where string // "make/go.mk:191"
	why   string // why the pattern cannot be listed
}

// runPatternExceptions are the -run lines the gate cannot list: two run inside
// build/tables-generated-go/cards, a generated module that does not exist
// until `make cards-generate` builds it; seven run inside test/go-tables, a
// separate Go module whose generated replacements do not exist until `make`
// builds them; and two are $(3) templates with no value until the caller
// expands it; two pair an empty -run '^$' with -fuzz so only the fuzz target
// runs.
var runPatternExceptions = []runPatternException{
	{"make/go.mk:323", "cd build/tables-generated-go/cards && go test ... . — build/tables-generated-go/cards is a generated module that does not exist until make cards-generate builds it, so -list cannot run from the repo root"},
	{"make/go.mk:459", "same generated build/tables-generated-go/cards module, -run '^TestCardFixtureWrite$$'"},
	{"make/go.mk:531", "cd test/go-tables && go test ... . — test/go-tables is a separate Go module whose generated replacements do not exist until make builds them, so -list cannot run from the repo root"},
	{"make/go.mk:561", "same separate test/go-tables module, -run Fuzz"},
	{"make/go.mk:562", "same separate test/go-tables module, -run Fuzz under -race"},
	{"make/go.mk:599", "same separate test/go-tables module, and it runs under a -overlay built at recipe time"},
	{"make/go.mk:984", "same separate test/go-tables module, -run '^TestSoak$$'"},
	{"make/go.mk:1136", "same separate test/go-tables module, -run '^TestUsage$$'"},
	{"make/go.mk:1154", "same separate test/go-tables module, -run '^TestKeyed'"},
	{"make/go.mk:541", "-run '^$$' runs no test on purpose: the recipe runs the fuzz target FuzzTableFrontEnd alone"},
	{"make/go.mk:555", "-run '^$$' runs no test on purpose: the recipe runs a fuzz target alone"},
	{"make/moo-text.mk:28", "-run '$(MOO_TEXT_GATES)' is a make variable defined at the top of make/moo-text.mk; the gate reads the recipe's literal text, so the pattern has no value until make expands it"},
	{"make/moo-text.mk:32", "-run 'TestMoo(Json|JSON|Text).*$(MOO_TEXT_CONTROLS)' ends in a make variable defined at the top of make/moo-text.mk, so the pattern has no value until make expands it"},
	{"make/moo-tables.mk:18", "-run 'Moo' names tests in compiler/tablesmoo_test.go, which carry the schema_leg_moo build tag; the gate lists without tags, so it cannot see them"},
	{"make/moo-readiness.mk:35", "-run 'Moo' names tests in compiler/tablesmoo_test.go and compiler/tablesmoonames_test.go, which carry the schema_leg_moo build tag; the gate lists without tags, so it cannot see them"},
	{"Makefile:5870", "inside define message_form_control: $(3) is the caller's test name, so the pattern has no value until expansion"},
	{"make/js.mk:1513", "inside define js_jstable_control: $(3) is the caller's test pattern and $(4) its package, so the pattern has no value until expansion"},
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
	var todo []runRecipe
	var pkgs []string
	havePkg := map[string]bool{}
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
		todo = append(todo, r)
		if !havePkg[r.pkg] {
			havePkg[r.pkg] = true
			pkgs = append(pkgs, r.pkg)
		}
	}
	listings := listPackages(t, root, pkgs)
	var offenders []string
	for _, r := range todo {
		if got := matchNames(t, r.pattern, listings[r.pkg]); len(got) == 0 {
			offenders = append(offenders, fmt.Sprintf("%s: `go test ... -run %q` matches no test in %s", r.where, r.pattern, r.pkg))
		}
	}
	t.Logf("scanned %d -run recipes, listed %d distinct (pattern, package) pairs", scanned, listed)
	return offenders
}

func TestRunPatternGateRefusesAZeroMatchPattern(t *testing.T) {
	t.Parallel()
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
	// This gate checks several hundred distinct (pattern, package) pairs. It
	// compiles each package once, side by side, to list its tests, and matches
	// every pattern against that listing; one `go test -list` per pair, one
	// after another, was minutes under SCHEMA_SLOW=1 and ~116 s in a plain run
	// (measured 2026-11-09). It is
	// a functional test of the whole makefile set, not a workflow-lint check, so
	// it rides the SCHEMA_SLOW=1 lane (ci-full.yml's unit-rest row, which runs on
	// the merge and nightly triggers) instead of the per-commit lint job. The
	// fast per-commit smoke is TestRunPatternGateRefusesAZeroMatchPattern above.
	slowtest.Gate(t, "the Go toolchain (it compiles each package to list its tests)")
	root := repoRoot(t)
	offenders := checkRunPatterns(t, root, makefileFiles(t, root), runPatternExceptions)
	for _, o := range offenders {
		t.Errorf("%s", o)
	}
}

// TestPerCommitLegBuildsFitTheWall holds the three leg jobs the two-minute
// rule names to the shape that fits it: the toolchain-dependent build is
// restored from a cache (or handed over as an artifact), the make
// prerequisite is a real stamp rather than a phony regenerator, and the fuzz
// row runs the bounded per-commit smoke while the full campaign still runs
// nightly in certify.yml. A job that rebuilds its driver from nothing on
// every run spends its two minutes before it tests, which is the red this
// pins.
func TestPerCommitLegBuildsFitTheWall(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	wfs := workflows(t, root)
	wf, ok := wfs["ci-full.yml"]
	if !ok {
		t.Fatal("ci-full.yml is the workflow this gate reads")
	}
	jobs := yamlJobs(wf)
	if len(jobs) == 0 {
		t.Fatal("ci-full.yml has no jobs: this gate would pass over an empty set")
	}

	legs := []struct {
		job      string
		cacheID  string
		cacheKey string
		touch    []string
		build    string
		run      string
	}{
		{
			job:      "cs-fuzz",
			cacheID:  "cs-fuzz-cache",
			cacheKey: "cs-fuzz-driver-",
			touch: []string{
				"build/tables-generated-cs/.stamp",
				"build/tables-generated-cs/cards/.stamp",
			},
			build: `make -j"$(nproc)" build/conformance-harness build-conformance-cs`,
			run:   `make ${{ matrix.target }}-smoke`,
		},
		{
			job:      "dart-fuzz",
			cacheID:  "dart-fuzz-cache",
			cacheKey: "dart-fuzz-driver-",
			touch: []string{
				"build/tables-generated-dart/.stamp",
				"build/tables-generated-dart/cards/.stamp",
				"build/conformance-dart",
			},
			build: `make -j"$(nproc)" build/conformance-harness build/conformance-dart DART=dart`,
			run:   `make ${{ matrix.target }}-smoke`,
		},
	}
	for _, leg := range legs {
		body, ok := jobs[leg.job]
		if !ok {
			t.Errorf("ci-full.yml has no %s job: the leg this gate names cannot be checked", leg.job)
			continue
		}
		for _, want := range []string{
			"id: " + leg.cacheID,
			"key: " + leg.cacheKey,
			"actions/cache/save@",
			"cache-primary-key",
			leg.build,
			leg.run,
		} {
			if !strings.Contains(body, want) {
				t.Errorf("%s does not carry %q: the leg rebuilds or reruns the campaign the card moved", leg.job, want)
			}
		}
		for _, path := range leg.touch {
			if !strings.Contains(body, "touch "+path) {
				t.Errorf("%s does not touch %s after restoring the cached build, so make rebuilds it over the new checkout", leg.job, path)
			}
		}
		const hostedOnly = "if: runner.environment == 'github-hosted'\n        with:\n          path: ${{ env.GO_BUILD_CACHE }}"
		if strings.Contains(body, hostedOnly) {
			t.Errorf("%s restores the Go build cache only on a github-hosted runner, so a fleet runner starts cold", leg.job)
		}
	}

	conf, ok := jobs["conformance"]
	if !ok {
		t.Fatal("ci-full.yml has no conformance job: the C leg this gate names cannot be checked")
	}
	for _, want := range []string{
		"id: conformance-c-cache",
		"if: matrix.lang == 'c'",
		"key: conformance-c-driver-",
		"'test/c-tables/**'",
		"touch build/tables-generated-c/collections.stamp",
		"touch build/tables-generated-c/.stamp",
		"build/conformance-c-objs",
		"touch build/conformance-c",
		"actions/cache/save@",
		"cache-primary-key",
	} {
		if !strings.Contains(conf, want) {
			t.Errorf("conformance does not carry %q: the C leg rebuilds its driver over the new checkout", want)
		}
	}

	// The C driver links from build/conformance-c-objs. If either cache path
	// list omits that directory, a hit restores the linked driver without its
	// objects and make recompiles all 98 translation units; if the hit step
	// touches only the stamp and the driver, the restored objects stay older
	// than the checkout and make recompiles them anyway. Both path lists must
	// carry the objects, and the hit step must stand the generated sources and
	// every object newer than the generated stamp and older than the linked
	// driver: the generated units carry an empty stamp prerequisite, so a
	// stamp newer than them is remade at make time and recompiles its object
	// even when the object itself was touched.
	pathLines := 0
	for _, l := range strings.Split(conf, "\n") {
		if strings.TrimSpace(l) == "build/conformance-c-objs" {
			pathLines++
		}
	}
	if pathLines < 2 {
		t.Errorf("conformance lists build/conformance-c-objs on %d cache path lines, want it in both the restore and save lists: a hit restores the driver without its objects and make recompiles all of them", pathLines)
	}
	stampAt := strings.Index(conf, "touch build/tables-generated-c/.stamp")
	driverAt := strings.Index(conf, "touch build/conformance-c")
	generatedAt, objectsAt := -1, -1
	for _, l := range strings.Split(conf, "\n") {
		if strings.Contains(l, "find build/tables-generated-c ") &&
			strings.Contains(l, "-exec touch") {
			generatedAt = strings.Index(conf, l)
		}
		if strings.Contains(l, "find build/conformance-c-objs") &&
			strings.Contains(l, "-type f") && strings.Contains(l, "-exec touch") {
			objectsAt = strings.Index(conf, l)
		}
	}
	if generatedAt < 0 {
		t.Error("conformance does not refresh the restored generated C sources after touching the stamp: the empty stamp rule remakes them at make time and recompiles every generated unit's object")
	}
	if objectsAt < 0 {
		t.Error("conformance does not stand every restored object under build/conformance-c-objs newer than the checkout: make recompiles all of the C driver's translation units on a cache hit")
	}
	if stampAt < 0 || driverAt < 0 || generatedAt < 0 || objectsAt < 0 ||
		!(stampAt < generatedAt && generatedAt < objectsAt && objectsAt < driverAt) {
		t.Error("conformance refreshes the C driver inputs out of order: the stamp, then the generated sources, then the objects, then the linked driver must each stand newer than the last, or make recompiles or relinks on a hit")
	}

	dartMK := readCI(t, root, "make/dart.mk")
	if !strings.Contains(dartMK, "build/conformance-dart: build/tables-generated-dart/.stamp $(CARDS_DART_STAMP)") {
		t.Error("make/dart.mk: build/conformance-dart still depends on the phony cards-dart-generate, so a restored driver is rebuilt every run")
	}
	if !strings.Contains(dartMK, "$(CARDS_DART_STAMP): bin/schema") {
		t.Error("make/dart.mk: the cards stamp the driver depends on has no rule")
	}
	csMK := readCI(t, root, "make/cs.mk")
	if !strings.Contains(csMK, "build-conformance-cs: build/tables-generated-cs/.stamp $(CARDS_CS_STAMP)") {
		t.Error("make/cs.mk: build-conformance-cs still depends on the phony cards-cs-generate, so a restored driver is rebuilt every run")
	}
	if !strings.Contains(csMK, "$(CARDS_CS_STAMP): bin/schema") {
		t.Error("make/cs.mk: the cards stamp the driver depends on has no rule")
	}

	// The phony generator is an alias for the stamp, never a second copy of
	// its clean-and-regenerate recipe: under make -j the driver and the cards
	// test both reach the tree, and two recipes would delete it under each
	// other.
	if !strings.Contains(csMK, "cards-cs-generate: $(CARDS_CS_STAMP)") {
		t.Error("make/cs.mk: cards-cs-generate duplicates the stamp recipe instead of depending on it; make -j can run both and delete the tree under the other")
	}
	if strings.Count(csMK, "rm -rf build/tables-generated-cs/cards") != 1 {
		t.Error("make/cs.mk: the cards clean-and-regenerate recipe appears more than once, so parallel targets race on the tree")
	}
	if !strings.Contains(dartMK, "cards-dart-generate: $(CARDS_DART_STAMP)") {
		t.Error("make/dart.mk: cards-dart-generate duplicates the stamp recipe instead of depending on it; make -j can run both and delete the tree under the other")
	}
	if strings.Count(dartMK, "rm -rf build/tables-generated-dart/cards") != 1 {
		t.Error("make/dart.mk: the cards clean-and-regenerate recipe appears more than once, so parallel targets race on the tree")
	}

	cMK := readCI(t, root, "make/c.mk")
	sources := strings.Fields("test/c-tables/collections_fuzz_maps.c test/c-tables/collections_fuzz_lists.c test/c-tables/collections_fuzz_arms.c")
	prereqs := map[string]bool{}
	for _, p := range targetPrereqs(cMK, "build/conformance-c") {
		prereqs[p] = true
	}
	for _, src := range sources {
		if !prereqs[src] {
			t.Errorf("make/c.mk: build/conformance-c does not list %s as a prerequisite, so a change to it keeps a cached driver", src)
		}
	}

	cert := readCI(t, root, ".github/workflows/certify.yml")
	for _, target := range []string{"tables-cs-wire-fuzz", "tables-cs-region-fuzz", "tables-dart-wire-fuzz"} {
		if !strings.Contains(cert, target) {
			t.Errorf("certify.yml does not run the full %s campaign nightly: the per-commit smoke dropped its cases", target)
		}
	}
}

// TestCSharpCacheHitRefreshesEveryRestoredOutput pins the incremental compile
// the C# fuzz cache relies on. dotnet compares the freshly checked-out sources
// and csproj against every restored obj/bin output, so touching only the
// driver assembly leaves its inputs older and CoreCompile rebuilds the whole
// driver. A cache hit must stand every restored file under
// test/conformance/cs/obj and test/conformance/cs/bin newer than the checkout.
func TestCSharpCacheHitRefreshesEveryRestoredOutput(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	body, ok := yamlJobs(workflows(t, root)["ci-full.yml"])["cs-fuzz"]
	if !ok {
		t.Fatal("ci-full.yml has no cs-fuzz job: the C# cache-hit refresh cannot be checked")
	}
	if !strings.Contains(body, "if: steps.cs-fuzz-cache.outputs.cache-hit == 'true'") {
		t.Fatal("cs-fuzz has no cache-hit guard: the refresh this gate checks could run on a miss")
	}
	for _, dir := range []string{"test/conformance/cs/obj", "test/conformance/cs/bin"} {
		refreshed := false
		for _, l := range strings.Split(body, "\n") {
			if strings.Contains(l, "find") && strings.Contains(l, dir) &&
				strings.Contains(l, "-type f") && strings.Contains(l, "-exec touch") {
				refreshed = true
				break
			}
		}
		if !refreshed {
			t.Errorf("cs-fuzz does not stand every restored file under %s newer than the checkout: dotnet sees the sources as newer and recompiles the whole driver on a cache hit", dir)
		}
	}
}
