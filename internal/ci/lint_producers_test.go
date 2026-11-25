package ci

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
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

// produceRows reads the produce job's matrix: each row's name and target.
func produceRows(job string) map[string]string {
	out := map[string]string{}
	re := regexp.MustCompile(`(?m)^          - name: (\S+)\n            target: (\S+)$`)
	for _, m := range re.FindAllStringSubmatch(job, -1) {
		out[m[1]] = m[2]
	}
	return out
}

// lintProducerProblems names each gate whose tree no produce row builds, or
// whose inputs target is not what the gate reads.
func lintProducerProblems(workflow string, mk map[string]string) []string {
	var problems []string
	rows := produceRows(yamlJobs(workflow)["produce"])
	for _, p := range lintProducers {
		if got, ok := rows[p.producer]; !ok || got != p.target {
			problems = append(problems, fmt.Sprintf("ci-fast.yml's produce matrix has no row %s building %s (it builds %q)", p.producer, p.target, got))
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
// built by a phony inputs target, and ci-fast.yml's produce job has a row
// that runs it, so the shards unpack the tree instead of generating it. The
// plants are a produce row gone, a row building another target, and a gate
// that stops reading its inputs: each is red by name.
func TestEveryLintGateReadsAProducedTree(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	data, err := os.ReadFile(filepath.Join(root, ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	mk := map[string]string{}
	for _, p := range lintProducers {
		b, err := os.ReadFile(filepath.Join(root, p.file))
		if err != nil {
			t.Fatal(err)
		}
		mk[p.file] = string(b)
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
		{"gate stops reading its inputs", "make/js.mk", "tables-js-clean: toolchain-js $(TABLES_JS_CLEAN_INPUTS)", "tables-js-clean: toolchain-js build/tables-generated-js/.stamp", "tables-js-clean does not read $(TABLES_JS_CLEAN_INPUTS)"},
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
