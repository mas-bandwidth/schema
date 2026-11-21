package main

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Scope is one unit of the sweep: the sources that are mutated and the test
// commands that should notice.
type Scope struct {
	Name string
	// Go holds the package directories whose non-test Go sources are mutated.
	Go []string
	// Rules is set for a target: the generator's string literals are emitted
	// code and its embedded runtime files are mutated as text.
	Rules *textRules
	// Min is the fewest mutants a sweep of this scope may run.
	Min    int
	Stages []Stage
	// Notes says what this scope cannot run, by name, so an absent stage is a
	// stated fact and not a silence.
	Notes []string
}

// Stage is one test command. A mutant is run through the stages in order and
// the first one that fails kills it.
type Stage struct {
	Name string
	// Sh is run by `sh -c` at the scratch tree's root.
	Sh string
	// Proof must match the stage's output on the unmutated tree, or the stage
	// ran nothing and the sweep refuses to start: a command that exits zero
	// without testing would otherwise call every mutant a survivor.
	Proof string
	// Counted is set for a `go test -v` stage: the tests it passes are counted
	// by name. The unmutated tree must pass at least one, and a mutant under
	// which one of those did not pass (skipped, filtered out, not reached) was
	// not tested by it, which is `unproven` and not a survivor. `ok` alone
	// proves nothing: go test prints it for a package whose every test was
	// filtered out or skipped.
	Counted bool
	// Deep stages cost minutes each and run only when asked for.
	Deep bool
}

// A go test that tested says `ok` for a package; the names of the tests it
// passed are the rest of the proof (Counted).
const goTestProof = `(?m)^ok\s+\S+`

// goTest is the stage command that runs go test over pkgs, verbose so the
// tests it passes can be counted.
func goTest(flags, pkgs string) string {
	return "go test -count=1 -v -p ${MUTATION_JOBS:-4} " + flags + pkgs
}

// coreScopes are the compiler's own packages, in the order the sweep takes
// them.
var coreScopes = []struct{ name, dir string }{
	{"check", "internal/check"},
	{"tablewire", "internal/tablewire"},
	{"tabletext", "internal/tabletext"},
	{"baseline", "internal/baseline"},
	{"tablepack", "internal/tablepack"},
	{"tablecook", "internal/tablecook"},
	{"ir", "ir"},
	{"cmd-schema", "cmd/schema"},
}

// targetScopes are the 18 targets: the conformance registry's name, the
// generator packages under internal/codegen (packet then table), and the
// language's delimiters for the text operators.
var targetScopes = []struct {
	lang     string
	packages []string
	rules    textRules
}{
	{"c", []string{"c", "ctable", "ccommon", "cqualify"}, textRules{"paren", "//", "false"}},
	{"cpp", []string{"cpp", "cpptable"}, textRules{"paren", "//", "false"}},
	{"cs", []string{"csharp", "cstable"}, textRules{"paren", "//", "false"}},
	{"unity", []string{"unity", "unitytable"}, textRules{"paren", "//", "false"}},
	{"dart", []string{"dart", "darttable"}, textRules{"paren", "//", "false"}},
	{"elixir", []string{"elixir", "elixirtable"}, textRules{"do", "#", "false"}},
	{"go", []string{"golang", "gotable"}, textRules{"brace", "//", "false"}},
	{"goose", []string{"goose", "goosetable"}, textRules{"brace", "//", "false"}},
	{"java", []string{"java", "javatable"}, textRules{"paren", "//", "false"}},
	{"js", []string{"js", "jstable"}, textRules{"paren", "//", "false"}},
	{"lobster", []string{"lobster", "lobstertable"}, textRules{"colon", "//", "false"}},
	{"lua", []string{"lua", "luatable"}, textRules{"then", "--", "false"}},
	{"luau", []string{"luau", "luautable"}, textRules{"then", "--", "false"}},
	{"moo", []string{"moo", "mootable"}, textRules{"then", "--", "false"}},
	{"python", []string{"python", "pythontable"}, textRules{"colon", "#", "False"}},
	{"ruby", []string{"ruby", "rubytable"}, textRules{"line", "#", "false"}},
	{"rust", []string{"rust", "rusttable"}, textRules{"brace", "//", "false"}},
	{"squirrel", []string{"squirrel", "squirreltable"}, textRules{"paren", "//", "false"}},
}

// The fewest mutants a sweep may run, per core package and per target.
const (
	minCore   = 40
	minTarget = 25
)

// runtimeExt are the embedded runtime files the text operators read.
var runtimeExt = map[string]bool{
	".lua": true, ".luau": true, ".moo": true, ".goose": true, ".lobster": true,
	".nut": true, ".rb": true, ".py": true,
}

// scopes builds every scope from the tree at root. A target's conformance
// targets come from the registry, test/conformance/<lang>/ci.json, which is
// what CI runs: a second copy here would drift from it.
func scopes(root string) ([]Scope, error) {
	var out []Scope
	for _, c := range coreScopes {
		pkg := "./" + c.dir + "/..."
		s := Scope{Name: c.name, Go: []string{c.dir}, Min: minCore}
		s.Stages = append(s.Stages, Stage{Name: "fast", Sh: goTest("", pkg), Proof: goTestProof, Counted: true})
		gated, err := hasSlowTests(root, s.Go)
		if err != nil {
			return nil, err
		}
		if gated {
			s.Stages = append(s.Stages, Stage{Name: "slow", Sh: "SCHEMA_SLOW=1 " + goTest("-timeout 60m ", pkg), Proof: goTestProof, Counted: true, Deep: true})
		}
		// the product's own packages: a core package is also held by the tests of
		// what imports it. The CI policy tests and the maintainer tools read the
		// workflows and the tree, not the product, so they are left out.
		s.Stages = append(s.Stages, Stage{Name: "repo", Sh: goTest("", "./compiler/... ./cmd/... ./ir/... $(go list ./internal/... | grep -v /internal/ci)"), Proof: goTestProof, Counted: true, Deep: true})
		out = append(out, s)
	}
	for _, t := range targetScopes {
		rules := t.rules
		s := Scope{Name: "target-" + t.lang, Rules: &rules, Min: minTarget}
		var pkgs []string
		for _, p := range t.packages {
			s.Go = append(s.Go, "internal/codegen/"+p)
			pkgs = append(pkgs, "./internal/codegen/"+p+"/...")
		}
		all := strings.Join(pkgs, " ")
		s.Stages = append(s.Stages, Stage{Name: "fast", Sh: goTest("", all), Proof: goTestProof, Counted: true})

		reg, err := readRegistry(root, t.lang)
		if err != nil {
			return nil, err
		}
		s.Stages = append(s.Stages, Stage{
			Name:  "conformance",
			Sh:    fmt.Sprintf("make -j${MUTATION_JOBS:-4} %s %s ${MUTATION_MAKE_ARGS:-} && env %s ${MUTATION_MAKE_ARGS:-} ./build/conformance-harness run --only %s", reg.Targets, reg.Env, reg.Env, t.lang),
			Proof: conformanceProof(t.lang),
		})
		hostile := "schema-" + t.lang + "-runs-hostile-corpus"
		has, err := hasMakeTarget(root, hostile)
		if err != nil {
			return nil, err
		}
		if has {
			st, err := hostileStage(t.lang, hostile)
			if err != nil {
				return nil, err
			}
			s.Stages = append(s.Stages, st)
		} else {
			s.Notes = append(s.Notes, "no make target "+hostile+": this target has no hostile runner to run")
		}
		s.Stages = append(s.Stages,
			Stage{Name: "pins", Sh: goTest("", "./internal/goldens"), Proof: goTestProof, Counted: true},
			Stage{Name: "slow", Sh: "SCHEMA_SLOW=1 " + goTest("-timeout 120m ", all), Proof: goTestProof, Counted: true, Deep: true},
		)
		out = append(out, s)
	}
	return out, nil
}

// conformanceProof is the harness's matrix with the target's column and at
// least one surface it passed cases on. The target's name alone is no proof:
// the stage's own command line holds it, and make echoes that.
func conformanceProof(lang string) string {
	return `(?m)^TABLES CONFORMANCE — surface x language$[\s\S]*^surface +` + regexp.QuoteMeta(lang) + ` *$[\s\S]*^\S+ +pass [1-9]`
}

// hostileProofs is the line each target's hostile runner prints when it ran
// cases and passed some, read off the runner's own output:
// testdata/hostile/<lang>.txt is the whole of it, and a test holds each proof
// to it. The runners print their summaries in their own words, so there is no
// default: a target with a hostile make target and no line here is refused.
var hostileProofs = map[string]string{
	"c":        `(?m)^c: [1-9]\d* cases: [1-9]\d* pass,`,
	"cpp":      `(?m)^cpp: [1-9]\d* cases: [1-9]\d* pass,`,
	"cs":       `(?m)^[1-9]\d* cases: [1-9]\d* pass,`,
	"unity":    `(?m)^[1-9]\d* cases: [1-9]\d* pass,`,
	"dart":     `(?m)^[1-9]\d* cases: [1-9]\d* pass,`,
	"elixir":   `(?m)^[1-9]\d* cases: [1-9]\d* pass,`,
	"java":     `(?m)^[1-9]\d* cases: [1-9]\d* pass,`,
	"go":       `(?m)^generated go: [1-9]\d* cases: [1-9]\d* pass,`,
	"rust":     `(?m)^rust \((?:release|debug)\): [1-9]\d* cases: [1-9]\d* pass,`,
	"js":       `(?m)^[1-9]\d* cases, [1-9]\d* rows: [1-9]\d* pass,`,
	"lobster":  `(?m)^lobster hostile corpus: [1-9]\d* pass, .* of [1-9]\d* cases$`,
	"goose":    `(?m)^goose hostile corpus: [1-9]\d* agree with the engine,`,
	"lua":      `(?m)^SLOW GATE schema-lua-runs-hostile-corpus: [1-9]\d* named gate\(s\) ran and PASS`,
	"luau":     `(?m)^SLOW GATE schema-luau-runs-hostile-corpus: [1-9]\d* named gate\(s\) ran and PASS`,
	"squirrel": `(?m)^SLOW GATE schema-squirrel-runs-hostile-corpus: [1-9]\d* named gate\(s\) ran and PASS`,
}

// hostileGoTests are the targets whose hostile make target is a bare go test,
// which prints only `ok <package>` without -v: they run verbose and counted.
var hostileGoTests = map[string]bool{"python": true, "ruby": true}

// hostileStage runs a target's hostile corpus, with what its runner prints
// only when it ran cases and passed some.
func hostileStage(lang, target string) (Stage, error) {
	st := Stage{Name: "hostile", Sh: "make " + target + " ${MUTATION_MAKE_ARGS:-}"}
	if hostileGoTests[lang] {
		st.Sh, st.Proof, st.Counted = "GOFLAGS=-v "+st.Sh, goTestProof, true
		return st, nil
	}
	proof, ok := hostileProofs[lang]
	if !ok {
		return st, fmt.Errorf("target %s has a make target %s and the sweep does not know what its runner prints when it ran: add its summary line to hostileProofs and its output to tools/mutation/testdata/hostile/%s.txt", lang, target, lang)
	}
	st.Proof = proof
	return st, nil
}

type registry struct {
	Targets string `json:"targets"`
	Env     string `json:"env"`
}

func readRegistry(root, lang string) (registry, error) {
	var r registry
	b, err := os.ReadFile(filepath.Join(root, "test", "conformance", lang, "ci.json"))
	if err != nil {
		return r, fmt.Errorf("target %s has no conformance registry entry: %w", lang, err)
	}
	if err := json.Unmarshal(b, &r); err != nil {
		return r, fmt.Errorf("test/conformance/%s/ci.json: %w", lang, err)
	}
	if strings.TrimSpace(r.Targets) == "" {
		return r, fmt.Errorf("test/conformance/%s/ci.json names no targets", lang)
	}
	return r, nil
}

// hasMakeTarget reports whether the Makefile or a fragment under make/
// defines the target.
func hasMakeTarget(root, target string) (bool, error) {
	files, err := filepath.Glob(filepath.Join(root, "make", "*.mk"))
	if err != nil {
		return false, err
	}
	files = append(files, filepath.Join(root, "Makefile"))
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			return false, err
		}
		for line := range strings.SplitSeq(string(b), "\n") {
			if strings.HasPrefix(line, target+":") {
				return true, nil
			}
		}
	}
	return false, nil
}

// hasSlowTests reports whether any test under the directories is behind the
// SCHEMA_SLOW gate, so a scope with none does not run its tests twice.
func hasSlowTests(root string, dirs []string) (bool, error) {
	found := false
	for _, d := range dirs {
		err := filepath.WalkDir(filepath.Join(root, d), func(path string, e fs.DirEntry, err error) error {
			if err != nil || e.IsDir() || !strings.HasSuffix(path, "_test.go") {
				return err
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			if strings.Contains(string(b), "slowtest.") {
				found = true
			}
			return nil
		})
		if err != nil {
			return false, err
		}
	}
	return found, nil
}

// enumerate lists every mutant of the scope in the tree at root, in a fixed
// order: files by path, then each file's mutants by position.
func (s *Scope) enumerate(root string) ([]Mutant, error) {
	var files []string
	for _, d := range s.Go {
		err := filepath.WalkDir(filepath.Join(root, d), func(path string, e fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if e.IsDir() {
				if e.Name() == "testdata" {
					return filepath.SkipDir
				}
				return nil
			}
			ext := filepath.Ext(path)
			if (ext == ".go" && !strings.HasSuffix(path, "_test.go")) || (s.Rules != nil && runtimeExt[ext]) {
				rel, err := filepath.Rel(root, path)
				if err != nil {
					return err
				}
				files = append(files, filepath.ToSlash(rel))
			}
			return nil
		})
		if err != nil {
			return nil, err
		}
	}
	sort.Strings(files)
	if len(files) == 0 {
		return nil, fmt.Errorf("scope %s: no source file under %v", s.Name, s.Go)
	}
	var muts []Mutant
	for _, f := range files {
		src, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(f)))
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(f, ".go") {
			if generatedFile(src) {
				continue
			}
			m, err := enumerateGo(f, src, s.Rules)
			if err != nil {
				return nil, fmt.Errorf("%s: %w", f, err)
			}
			muts = append(muts, m...)
			continue
		}
		muts = append(muts, finish(f, src, s.Rules.enumerate(0, string(src), false, true))...)
	}
	return muts, nil
}

var generatedMark = regexp.MustCompile(`(?m)^// Code generated .* DO NOT EDIT\.$`)

func generatedFile(src []byte) bool { return generatedMark.Match(src) }
