package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// lintProducer is one lint gate's tree as ci-fast.yml's produce job builds
// it: the gate's make file, the variable that names what the gate reads, the
// phony target that builds exactly that, and the produce matrix row that
// runs it. The schema-test-pr shards unpack the tree, so a lint row pays its
// lint and never the generation in front of it.
type lintProducer struct {
	gates    []string // the gates that read the tree
	file     string   // the make file that holds them
	inputs   string   // the variable naming what they read
	target   string   // the phony target that builds it
	producer string   // the produce matrix row
}

// tables-c-clean is not here: building its tree rewrites committed
// generated files, and produce.sh refuses to pack a dirty tree, so the
// gate builds its tree itself.
var lintProducers = []lintProducer{
	{[]string{"tables-cpp-format", "tables-cpp-warnings"}, "make/cpp.mk", "CPP_CLEAN_STAMPS", "tables-cpp-clean-inputs", "lint-cpp"},
	{[]string{"tables-js-clean"}, "make/js.mk", "TABLES_JS_CLEAN_INPUTS", "tables-js-clean-inputs", "lint-js"},
	{[]string{"tables-rust-clippy"}, "make/rust.mk", "TABLES_RUST_CLIPPY_INPUTS", "tables-rust-clippy-inputs", "lint-rust"},
	{[]string{"tables-lua-luacheck", "tables-lua-format"}, "make/schema-lua-generated-luacheck-gate.mk", "TABLES_LUA_LUACHECK_INPUTS", "tables-lua-luacheck-inputs", "lint-lua"},
}

// produceRow is one row of the produce job's matrix.
type produceRow struct {
	target string
	paths  []string
}

// produceRows reads the produce job's matrix: each row's target and the
// paths it packs.
func produceRows(job string) map[string]produceRow {
	out := map[string]produceRow{}
	blocks := strings.Split(job, "\n          - name: ")
	for _, b := range blocks[1:] {
		name, rest, _ := strings.Cut(b, "\n")
		var row produceRow
		for _, line := range strings.Split(rest, "\n") {
			if !strings.HasPrefix(line, "            ") || strings.HasPrefix(line, "             ") {
				break
			}
			k, v, _ := strings.Cut(strings.TrimSpace(line), ": ")
			switch k {
			case "target":
				row.target = v
			case "paths":
				row.paths = strings.Fields(v)
			}
		}
		out[strings.TrimSpace(name)] = row
	}
	return out
}

var (
	makeVarDef  = regexp.MustCompile(`(?m)^([A-Za-z_][A-Za-z0-9_]*)[ \t]*[:?]?=[ \t]*(.*)$`)
	makeVarRef  = regexp.MustCompile(`\$\(([A-Za-z_][A-Za-z0-9_]*)\)`)
	makeRuleOrd = regexp.MustCompile(`(?m)^([^\t#:=\n][^:=\n]*):([^=\n]*)\|([^=\n]*)$`)
)

// makeVars reads every one-line variable definition in the make files; a
// name defined more than once maps to "" so expanding it is refused.
func makeVars(mk map[string]string) map[string]string {
	vars := map[string]string{}
	seen := map[string]int{}
	for _, text := range mk {
		for _, m := range makeVarDef.FindAllStringSubmatch(text, -1) {
			seen[m[1]]++
			vars[m[1]] = m[2]
		}
	}
	for k, n := range seen {
		if n != 1 {
			vars[k] = ""
		}
	}
	return vars
}

// expandMake expands $(VAR) references from vars, naming any it cannot.
func expandMake(s string, vars map[string]string, depth int) (string, error) {
	if depth > 8 {
		return "", fmt.Errorf("%q expands too deep", s)
	}
	var err error
	out := makeVarRef.ReplaceAllStringFunc(s, func(ref string) string {
		name := makeVarRef.FindStringSubmatch(ref)[1]
		v, ok := vars[name]
		if !ok || v == "" {
			err = fmt.Errorf("$(%s) has no single one-line definition", name)
			return ref
		}
		e, e2 := expandMake(v, vars, depth+1)
		if e2 != nil {
			err = e2
		}
		return e
	})
	return out, err
}

// producedFiles is what a gate reading $(inputs) needs on disk: each word of
// the expansion, and, closed over, each order-only prerequisite of a rule
// for one of them (a stamp that names its tree's other half that way).
func producedFiles(inputs string, vars map[string]string, orderOnly map[string][]string) ([]string, error) {
	words, err := expandMake("$("+inputs+")", vars, 0)
	if err != nil {
		return nil, err
	}
	need := map[string]bool{}
	queue := strings.Fields(words)
	for len(queue) > 0 {
		w := queue[0]
		queue = queue[1:]
		if need[w] {
			continue
		}
		need[w] = true
		queue = append(queue, orderOnly[w]...)
	}
	var out []string
	for w := range need {
		out = append(out, w)
	}
	sort.Strings(out)
	return out, nil
}

// orderOnlyPrereqs maps each target to the order-only prerequisites its
// rules name, read across every make file.
func orderOnlyPrereqs(mk map[string]string, vars map[string]string) map[string][]string {
	orderOnly := map[string][]string{}
	for _, text := range mk {
		for _, m := range makeRuleOrd.FindAllStringSubmatch(text, -1) {
			targets, e1 := expandMake(m[1], vars, 0)
			after, e2 := expandMake(m[3], vars, 0)
			if e1 != nil || e2 != nil {
				continue // its names have no single expansion to match an input against
			}
			for _, t := range strings.Fields(targets) {
				orderOnly[t] = append(orderOnly[t], strings.Fields(after)...)
			}
		}
	}
	return orderOnly
}

// packedBy reports whether a needed file is in one of the packed paths. A
// stamp stands for its whole directory, so the directory must be packed, not
// the stamp alone.
func packedBy(file string, paths []string) bool {
	want := file
	if filepath.Base(file) == ".stamp" {
		want = filepath.Dir(file)
	}
	for _, p := range paths {
		if want == p || strings.HasPrefix(want, p+"/") {
			return true
		}
	}
	return false
}

// lintProducerProblems names each gate whose tree no produce row builds,
// whose inputs target is not what the gate reads, or whose row does not
// pack all of what the gate reads.
func lintProducerProblems(workflow string, mk map[string]string) []string {
	var problems []string
	rows := produceRows(yamlJobs(workflow)["produce"])
	vars := makeVars(mk)
	orderOnly := orderOnlyPrereqs(mk, vars)
	for _, p := range lintProducers {
		row, ok := rows[p.producer]
		if !ok || row.target != p.target {
			problems = append(problems, fmt.Sprintf("ci-fast.yml's produce matrix has no row %s building %s (it builds %q)", p.producer, p.target, row.target))
		}
		if ok {
			files, err := producedFiles(p.inputs, vars, orderOnly)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s: $(%s) cannot be read: %v", p.file, p.inputs, err))
			}
			for _, f := range files {
				if !packedBy(f, row.paths) {
					problems = append(problems, fmt.Sprintf("ci-fast.yml's produce row %s does not pack %s, which %s reads", p.producer, f, strings.Join(p.gates, " and ")))
				}
			}
		}
		text := mk[p.file]
		if !regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(p.target) + `: \$\(` + p.inputs + `\)$`).MatchString(text) {
			problems = append(problems, fmt.Sprintf("%s: %s does not build $(%s) alone", p.file, p.target, p.inputs))
		}
		for _, g := range p.gates {
			rule := regexp.MustCompile(`(?m)^` + regexp.QuoteMeta(g) + `:([^\n]*)$`).FindStringSubmatch(text)
			uses := "$(" + p.inputs + ")"
			if g == "tables-lua-format" {
				uses = "$(LUA_LUACHECK_CORPUS)/.stamp" // the corpus half of the inputs
			}
			if rule == nil || !strings.Contains(rule[1], uses) {
				problems = append(problems, fmt.Sprintf("%s: %s does not read %s, so the produced tree is not what it lints", p.file, g, uses))
			}
		}
	}
	return problems
}

// TestEveryLintGateReadsAProducedTree: each lint gate's generated tree is
// built by a phony inputs target, ci-fast.yml's produce job has a row that
// runs it, and the row packs every directory the gate reads (with the
// order-only halves of its stamps), so the shards unpack the tree instead of
// generating it. The plants are a produce row gone, a row building another
// target, a row packing less than its gate reads, a gate that stops reading
// its inputs, and tables-lua-format no longer reading the corpus: each is
// red by name.
func TestEveryLintGateReadsAProducedTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := filepath.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		t.Fatal(err)
	}
	files = append(files, filepath.Join(root, "Makefile"))
	mk := map[string]string{}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		rel, err := filepath.Rel(root, f)
		if err != nil {
			t.Fatal(err)
		}
		mk[filepath.ToSlash(rel)] = string(b)
	}
	for _, p := range lintProducers {
		if _, ok := mk[p.file]; !ok {
			t.Fatalf("%s is not among the make files read", p.file)
		}
	}
	wf := string(data)
	for _, e := range lintProducerProblems(wf, mk) {
		t.Error(e)
	}
	plants := []struct {
		name, file, old, new, want string
	}{
		{"producer row gone", "", "          - name: lint-rust\n            target: tables-rust-clippy-inputs\n", "", "no row lint-rust building tables-rust-clippy-inputs"},
		{"producer builds the gate", "", "          - name: lint-js\n            target: tables-js-clean-inputs\n", "          - name: lint-js\n            target: tables-js-clean\n", "no row lint-js building tables-js-clean-inputs"},
		{"row packs less than its gate reads", "", "            paths: bin/schema build/tables-generated-rust build/tables-generated-rust-witnesses\n", "            paths: bin/schema build/tables-generated-rust\n", "produce row lint-rust does not pack build/tables-generated-rust-witnesses/.stamp, which tables-rust-clippy reads"},
		{"gate stops reading its inputs", "make/js.mk", "tables-js-clean: toolchain-js $(TABLES_JS_CLEAN_INPUTS)", "tables-js-clean: toolchain-js build/tables-generated-js/.stamp", "tables-js-clean does not read $(TABLES_JS_CLEAN_INPUTS)"},
		{"lua format stops reading the corpus", "make/schema-lua-generated-luacheck-gate.mk", "tables-lua-format: dist-stylua $(LUA_LUACHECK_CORPUS)/.stamp\n", "tables-lua-format: dist-stylua\n", "tables-lua-format does not read $(LUA_LUACHECK_CORPUS)/.stamp"},
	}
	for _, pl := range plants {
		w, m := wf, map[string]string{}
		for k, v := range mk {
			m[k] = v
		}
		src := &w
		if pl.file != "" {
			s := m[pl.file]
			src = &s
		}
		if strings.Count(*src, pl.old) != 1 {
			t.Errorf("%s: the plant's anchor is not there exactly once", pl.name)
			continue
		}
		*src = strings.Replace(*src, pl.old, pl.new, 1)
		if pl.file != "" {
			m[pl.file] = *src
		}
		if got := strings.Join(lintProducerProblems(w, m), "\n"); !strings.Contains(got, pl.want) {
			t.Errorf("%s: the check reported %q, want %q", pl.name, got, pl.want)
		}
	}
}
