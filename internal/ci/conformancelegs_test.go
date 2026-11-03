package ci

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// TestConformanceLegsRebuildFromTheirInputs pins each conformance leg's
// generated tree to its inputs (test/conformance/README.md): the compiler's Go
// sources, the runtimes the leg's emitters embed, and every .schema file the
// tables manifest's units name. A leg whose tree depends on the manifest alone
// keeps answering from the OLD runtime after an edit to it, and the harness
// then measures code the source no longer holds. The answer is make's own:
// the prerequisite graph `make -p` prints, closed transitively from each leg's
// conformance target.
//
// The reversed witness puts back build/conformance-goose's manifest-only rule
// in a copy of make/goose.mk, included in place of the real one, and holds
// that exactly the goose leg goes red.
func TestConformanceLegsRebuildFromTheirInputs(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("this gate reads make's prerequisite graph: %v", err)
	}
	if red := conformanceLegGaps(t, nil); len(red) > 0 {
		for _, r := range red {
			t.Error(r)
		}
	}
}

func TestConformanceLegsRebuildNegativeControl(t *testing.T) {
	t.Parallel()
	if _, err := exec.LookPath("make"); err != nil {
		t.Skipf("this gate reads make's prerequisite graph: %v", err)
	}
	src, err := os.ReadFile(filepath.Join(repoRootDir(t), "make", "goose.mk"))
	if err != nil {
		t.Fatal(err)
	}
	const fixed = "build/conformance-goose: $(GOOSE_MANIFEST) make/goose.mk $(GO_SOURCES) \\\n" +
		"\t$(filter internal/codegen/goose/% internal/codegen/goosetable/%,$(EMBED_SOURCES)) \\\n" +
		"\t$(CONFORMANCE_UNIT_SCHEMAS)\n"
	if strings.Count(string(src), fixed) != 1 {
		t.Fatalf("the goose conformance rule does not stand once in make/goose.mk:\n%s", fixed)
	}
	old := strings.Replace(string(src), fixed, "build/conformance-goose: $(GOOSE_MANIFEST) make/goose.mk\n", 1)
	sabotaged := filepath.Join(t.TempDir(), "goose.mk")
	if err := os.WriteFile(sabotaged, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	red := conformanceLegGaps(t, map[string]string{"make/goose.mk": sabotaged})
	var legs []string
	for _, r := range red {
		leg, _, _ := strings.Cut(r, ":")
		if !slices.Contains(legs, leg) {
			legs = append(legs, leg)
		}
	}
	if strings.Join(legs, " ") != "goose" {
		t.Fatalf("the manifest-only goose rule is red on legs %q, want exactly [goose]:\n%s", legs, strings.Join(red, "\n"))
	}
}

// conformanceLeg is one leg's conformance target and the codegen packages
// whose embedded runtimes it generates from.
type conformanceLeg struct {
	name, target string
	packages     []string
}

var conformanceLegs = []conformanceLeg{
	{"c", "build/conformance-c", []string{"ctable"}},
	{"cpp", "build/conformance-cpp", []string{"cpptable"}},
	{"cs", "build-conformance-cs", []string{"cstable"}},
	{"dart", "build/conformance-dart", []string{"darttable"}},
	{"elixir", "build-conformance-elixir", []string{"elixirtable"}},
	{"go", "build/conformance-go", []string{"gotable"}},
	{"goose", "build/conformance-goose", []string{"goose", "goosetable"}},
	{"java", "build-conformance-java", []string{"javatable"}},
	{"js", "build/tables-generated-js/.stamp", []string{"jstable"}},
	{"lobster", "build/conformance-lobster", []string{"lobster", "lobstertable"}},
	{"lua", "build/conformance-lua", []string{"lua", "luatable"}},
	{"luau", "build/conformance-luau", []string{"luau", "luautable"}},
	{"moo", "build/conformance-moo", []string{"moo", "mootable"}},
	{"rust", "build/conformance-rust", []string{"rusttable"}},
	{"squirrel", "build/conformance-squirrel", []string{"squirrel", "squirreltable"}},
}

func repoRootDir(t *testing.T) string {
	t.Helper()
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	return root
}

// conformanceLegGaps answers one line per missing input, `<leg>: <what>`.
// replace swaps an included make/*.mk for another file.
func conformanceLegGaps(t *testing.T, replace map[string]string) []string {
	t.Helper()
	root := repoRootDir(t)
	graph := makeGraph(t, root, replace)
	schemas := manifestUnitSchemas(t, root)
	var gaps []string
	for _, leg := range conformanceLegs {
		if graph[leg.target] == nil {
			gaps = append(gaps, leg.name+": make has no rule for "+leg.target)
			continue
		}
		reach := map[string]bool{}
		queue := []string{leg.target}
		for len(queue) > 0 {
			n := queue[0]
			queue = queue[1:]
			for _, p := range graph[n] {
				if !reach[p] {
					reach[p] = true
					queue = append(queue, p)
				}
			}
		}
		// a compiler source no emitter owns stands for GO_SOURCES
		if !reach["internal/tabletext/read.go"] || !reach["compiler/target_goose.go"] {
			gaps = append(gaps, leg.name+": the compiler's Go sources are not an input of "+leg.target)
		}
		for _, pkg := range leg.packages {
			files, err := filepath.Glob(filepath.Join(root, "internal", "codegen", pkg, "runtime", "*"))
			if err != nil {
				t.Fatal(err)
			}
			for _, f := range files {
				rel, _ := filepath.Rel(root, f)
				if st, err := os.Stat(f); err == nil && st.Mode().IsRegular() && !strings.HasSuffix(rel, ".go") && !reach[filepath.ToSlash(rel)] {
					gaps = append(gaps, leg.name+": the runtime "+filepath.ToSlash(rel)+" is not an input of "+leg.target)
				}
			}
		}
		for _, s := range schemas {
			if !reach[s] {
				gaps = append(gaps, leg.name+": the unit schema "+s+" is not an input of "+leg.target)
			}
		}
	}
	return gaps
}

// makeGraph is make's prerequisite graph over the real Makefile: target to
// its prerequisites (normal and order-only), as `make -p` prints them.
func makeGraph(t *testing.T, root string, replace map[string]string) map[string][]string {
	t.Helper()
	makefile := filepath.Join(root, "Makefile")
	if len(replace) > 0 {
		src, err := os.ReadFile(makefile)
		if err != nil {
			t.Fatal(err)
		}
		const include = "include $(wildcard make/*.mk)\n"
		if strings.Count(string(src), include) != 1 {
			t.Fatalf("the Makefile's include line does not stand once: %q", include)
		}
		var out, with []string
		for from, to := range replace {
			out = append(out, from)
			with = append(with, to)
		}
		line := "include $(filter-out " + strings.Join(out, " ") + ",$(wildcard make/*.mk)) " + strings.Join(with, " ") + "\n"
		makefile = filepath.Join(t.TempDir(), "Makefile")
		if err := os.WriteFile(makefile, []byte(strings.Replace(string(src), include, line, 1)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	// -q runs no recipe; the target names nothing, so make stops after it
	// has printed the database
	cmd := exec.Command("make", "--no-print-directory", "-f", makefile, "-p", "-q", "conformance-leg-inputs-no-such-target")
	cmd.Dir = root
	out, _ := cmd.Output()
	graph := map[string][]string{}
	sc := bufio.NewScanner(strings.NewReader(string(out)))
	sc.Buffer(make([]byte, 1<<20), 1<<26)
	files := false
	for sc.Scan() {
		line := sc.Text()
		if strings.HasPrefix(line, "# Files") {
			files = true
			continue
		}
		if !files || line == "" || line[0] == '#' || line[0] == '\t' || line[0] == ' ' {
			continue
		}
		target, rest, ok := strings.Cut(line, ":")
		if !ok || strings.HasPrefix(rest, "=") || strings.Contains(target, "=") {
			continue
		}
		rest = strings.TrimPrefix(rest, ":")
		rest = strings.ReplaceAll(rest, "|", " ")
		for _, tgt := range strings.Fields(target) {
			graph[tgt] = append(graph[tgt], strings.Fields(rest)...)
		}
	}
	if len(graph) == 0 {
		t.Fatalf("make printed no prerequisite graph:\n%.2000s", out)
	}
	return graph
}

// manifestUnitSchemas is every .schema file the manifest's units name, read
// here rather than through the Makefile's own list.
func manifestUnitSchemas(t *testing.T, root string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 3 || f[0] != "unit" {
			continue
		}
		for _, p := range f[2:] {
			if strings.HasSuffix(p, ".schema") {
				out = append(out, p)
				continue
			}
			found, err := filepath.Glob(filepath.Join(root, p, "*.schema"))
			if err != nil {
				t.Fatal(err)
			}
			for _, s := range found {
				rel, _ := filepath.Rel(root, s)
				out = append(out, filepath.ToSlash(rel))
			}
		}
	}
	if len(out) == 0 {
		t.Fatal("the manifest names no unit schema")
	}
	slices.Sort(out)
	return slices.Compact(out)
}
