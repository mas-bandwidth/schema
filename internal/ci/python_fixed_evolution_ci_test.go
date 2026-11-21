package ci

import (
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"regexp"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type fixedEvolutionRow struct{ name, top, sub string }

func fixedEvolutionCases(source []byte, function string) ([]string, error) {
	file, err := parser.ParseFile(token.NewFileSet(), "fixed_proof_evolution_test.go", source, 0)
	if err != nil {
		return nil, err
	}
	var result []string
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != function {
			continue
		}
		ast.Inspect(fn.Body, func(n ast.Node) bool {
			if function == "TestPythonFixedProofEvolutionWideningCorpus" {
				r, ok := n.(*ast.RangeStmt)
				if !ok {
					return true
				}
				c, ok := r.X.(*ast.CompositeLit)
				if !ok {
					return true
				}
				for _, item := range c.Elts {
					e, ok := item.(*ast.CompositeLit)
					if !ok || len(e.Elts) == 0 {
						continue
					}
					v, ok := e.Elts[0].(*ast.BasicLit)
					if !ok || v.Kind != token.STRING {
						continue
					}
					s, err := strconv.Unquote(v.Value)
					if err == nil {
						result = append(result, s)
					}
				}
				return false
			}
			a, ok := n.(*ast.AssignStmt)
			if !ok || len(a.Lhs) != 1 || len(a.Rhs) != 1 {
				return true
			}
			id, ok := a.Lhs[0].(*ast.Ident)
			if !ok || id.Name != "selected" {
				return true
			}
			c, ok := a.Rhs[0].(*ast.CompositeLit)
			if !ok {
				return true
			}
			for _, item := range c.Elts {
				kv, ok := item.(*ast.KeyValueExpr)
				if !ok {
					continue
				}
				v, ok := kv.Key.(*ast.BasicLit)
				if !ok || v.Kind != token.STRING {
					continue
				}
				s, err := strconv.Unquote(v.Value)
				if err == nil {
					result = append(result, s)
				}
			}
			return false
		})
	}
	if len(result) == 0 {
		return nil, fmt.Errorf("no cases discovered in %s", function)
	}
	seen := map[string]bool{}
	for _, name := range result {
		if seen[name] {
			return nil, fmt.Errorf("duplicate discovered case %s", name)
		}
		seen[name] = true
	}
	return result, nil
}

func fixedEvolutionCoverage(names []string, aggregate string, rows []fixedEvolutionRow, subcases map[string][]string) error {
	a, err := regexp.Compile(aggregate)
	if err != nil {
		return err
	}
	used := make([]int, len(rows))
	wanted := 0
	for _, name := range names {
		if !a.MatchString(name) {
			continue
		}
		wanted++
		cases := subcases[name]
		if len(cases) == 0 {
			cases = []string{""}
		}
		for _, sub := range cases {
			matches := 0
			for i, row := range rows {
				top, err := regexp.Compile(row.top)
				if err != nil {
					return err
				}
				if !top.MatchString(name) {
					continue
				}
				if row.sub != "" {
					re, err := regexp.Compile(row.sub)
					if err != nil {
						return err
					}
					if sub == "" || !re.MatchString(sub) {
						continue
					}
				}
				matches++
				used[i]++
			}
			if matches != 1 {
				return fmt.Errorf("%s/%s selected %d times", name, sub, matches)
			}
		}
	}
	if wanted == 0 || len(rows) == 0 {
		return fmt.Errorf("empty evolution selection")
	}
	for i, count := range used {
		if count == 0 {
			return fmt.Errorf("row %s selects no discovered case", rows[i].name)
		}
	}
	return nil
}

func TestPythonFixedEvolutionCICoverage(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Go discovery and fixed evolution CI selection")
	run := func(name string, args ...string) []byte {
		t.Helper()
		c := exec.Command(name, args...)
		c.Dir = "../.."
		out, err := c.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return out
	}
	listed := strings.Fields(string(run("go", "test", "-list", "^TestPythonFixedProofEvolution", "./internal/codegen/pythontable")))
	var aggregate string
	var rows []fixedEvolutionRow
	for line := range strings.SplitSeq(strings.TrimSpace(string(run("make", "-s", "python-fixed-evolution-ci-patterns"))), "\n") {
		parts := strings.Fields(line)
		if len(parts) != 2 {
			t.Fatalf("bad pattern row %q", line)
		}
		if parts[0] == "aggregate" {
			aggregate = parts[1]
			continue
		}
		pattern := strings.Split(parts[1], "/")
		if len(pattern) > 2 {
			t.Fatalf("bad selector %q", parts[1])
		}
		r := fixedEvolutionRow{name: parts[0], top: pattern[0]}
		if len(pattern) == 2 {
			r.sub = pattern[1]
		}
		rows = append(rows, r)
	}
	source, err := os.ReadFile("../codegen/pythontable/fixed_proof_evolution_test.go")
	if err != nil {
		t.Fatal(err)
	}
	subs := map[string][]string{}
	for _, name := range []string{"TestPythonFixedProofEvolutionStrict", "TestPythonFixedProofEvolutionWideningCorpus"} {
		subs[name], err = fixedEvolutionCases(source, name)
		if err != nil {
			t.Fatal(err)
		}
	}
	if err := fixedEvolutionCoverage(listed, aggregate, rows, subs); err != nil {
		t.Fatal(err)
	}
	if len(rows) < 2 || fixedEvolutionCoverage(listed, aggregate, rows[1:], subs) == nil {
		t.Fatal("omitted-row control did not fail")
	}
	if fixedEvolutionCoverage(listed, aggregate, append(append([]fixedEvolutionRow{}, rows...), rows[0]), subs) == nil {
		t.Fatal("duplicate-row control did not fail")
	}
	mutated := make(map[string][]string, len(subs))
	for k, v := range subs {
		mutated[k] = append([]string{}, v...)
	}
	mutated["TestPythonFixedProofEvolutionStrict"] = append(mutated["TestPythonFixedProofEvolutionStrict"], "missing-case")
	if fixedEvolutionCoverage(listed, aggregate, rows, mutated) == nil {
		t.Fatal("new-subcase control did not fail")
	}
	var plan []struct {
		Target, Package, Test string
		Corpus                bool
	}
	if err := json.Unmarshal(pythonCIPlan(t), &plan); err != nil {
		t.Fatal(err)
	}
	counts := map[string]int{}
	for _, p := range plan {
		counts[p.Target]++
		if strings.HasPrefix(p.Target, "python-fixed-proof-evolution-row-") && !p.Corpus {
			t.Fatalf("%s lost corpus prerequisite", p.Target)
		}
		if p.Target == "" && strings.HasSuffix(p.Package, "/internal/codegen/pythontable") {
			re, err := regexp.Compile(p.Test)
			if err != nil {
				t.Fatal(err)
			}
			a := regexp.MustCompile(aggregate)
			for _, name := range listed {
				if a.MatchString(name) && re.MatchString(name) {
					t.Fatalf("%s still selected automatically", name)
				}
			}
		}
	}
	for _, row := range rows {
		target := "python-fixed-proof-evolution-row-" + row.name
		if counts[target] != 1 {
			t.Errorf("%s appears %d times", target, counts[target])
		}
	}
	if counts["python-fixed-proof-evolution"] != 0 || counts["python-fixed-proof-evolution-negative-control"] != 1 {
		t.Fatal("aggregate or negative-control registration changed")
	}
}
