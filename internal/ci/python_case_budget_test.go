package ci

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

func pythonCaseBudgetCheck(array, pointer, makefile, planner []byte) error {
	readValues := func(source []byte, variable string) ([]*ast.CompositeLit, error) {
		f, err := parser.ParseFile(token.NewFileSet(), "proof.go", source, 0)
		if err != nil {
			return nil, err
		}
		var result []*ast.CompositeLit
		ast.Inspect(f, func(n ast.Node) bool {
			v, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range v.Names {
				if name.Name == variable && i < len(v.Values) {
					if c, ok := v.Values[i].(*ast.CompositeLit); ok {
						result = append(result, c)
					}
				}
			}
			return true
		})
		if len(result) != 1 {
			return nil, fmt.Errorf("proof variable %s occurs %d times", variable, len(result))
		}
		return result, nil
	}
	cases, err := readValues(array, "arrayWideningCases")
	if err != nil {
		return err
	}
	phases, err := readValues(pointer, "pointerArrayProofPhases")
	if err != nil {
		return err
	}
	pins, err := readValues(pointer, "pointerArrayDeepPins")
	if err != nil {
		return err
	}
	literal := func(n ast.Expr) (string, error) {
		v, ok := n.(*ast.BasicLit)
		if !ok || v.Kind != token.STRING {
			return "", fmt.Errorf("expected proof name literal")
		}
		return strconv.Unquote(v.Value)
	}
	var want []string
	for _, n := range cases[0].Elts {
		c, ok := n.(*ast.CompositeLit)
		if !ok || len(c.Elts) != 3 {
			return fmt.Errorf("invalid widening case")
		}
		old, e := literal(c.Elts[0])
		if e != nil {
			return e
		}
		newer, e := literal(c.Elts[1])
		if e != nil {
			return e
		}
		want = append(want, "TestPythonArrayWidening/"+old+"_to_"+newer)
	}
	for _, n := range phases[0].Elts {
		phase, e := literal(n)
		if e != nil {
			return e
		}
		if phase == "deep" {
			if len(pins[0].Elts) == 0 {
				return fmt.Errorf("empty deep proof pins")
			}
			for _, p := range pins[0].Elts {
				pin, e := literal(p)
				if e != nil {
					return e
				}
				want = append(want, "TestPythonVtPointerArrays/deep/"+pin)
			}
		} else {
			want = append(want, "TestPythonVtPointerArrays/"+phase)
		}
	}
	if len(cases[0].Elts) == 0 || len(phases[0].Elts) == 0 {
		return fmt.Errorf("empty proof cases")
	}
	recipes := regexp.MustCompile(`(?m)^\t[^\n]*-run '([^']+)'[^\n]*$`).FindAllSubmatch(makefile, -1)
	if len(recipes) != 7 {
		return fmt.Errorf("case recipes=%d, want 7 bounded groups", len(recipes))
	}
	used := make([]int, len(recipes))
	for _, name := range want {
		parts := strings.Split(name, "/")
		matches := 0
		for i, r := range recipes {
			pattern := strings.Split(strings.ReplaceAll(string(r[1]), "$$", "$"), "/")
			if len(pattern) != len(parts) {
				continue
			}
			good := true
			for j, p := range pattern {
				ok, e := regexp.MatchString(p, parts[j])
				if e != nil {
					return e
				}
				good = good && ok
			}
			if good {
				if parts[0] == "TestPythonVtPointerArrays" && !strings.Contains(string(r[0]), "SCHEMA_PYTHON_POINTER_ARRAY_PHASE="+parts[1]+" ") {
					return fmt.Errorf("pointer case %s has a mismatched phase selector", name)
				}
				matches++
				used[i]++
			}
		}
		if matches != 1 {
			return fmt.Errorf("proof case %s selected %d times", name, matches)
		}
	}
	for i, n := range used {
		if n == 0 {
			return fmt.Errorf("empty case recipe %s", recipes[i][1])
		}
	}
	for _, name := range []string{"TestPythonArrayWidening", "TestPythonVtPointerArrays"} {
		if strings.Count(string(planner), "/^"+name+"$/d") != 1 {
			return fmt.Errorf("monolithic %s must be delegated exactly once", name)
		}
	}
	return nil
}

func TestPythonCaseBudgetCoverage(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	read := func(path string) []byte {
		b, e := os.ReadFile(filepath.Join(root, path))
		if e != nil {
			t.Fatal(e)
		}
		return b
	}
	array := read("internal/codegen/pythontable/array_widen_test.go")
	pointer := read("internal/codegen/pythontable/pointerarrays_test.go")
	makefile := read("make/python-case-budget.mk")
	planner := read("tools/python-ci-rows.sh")
	if err := pythonCaseBudgetCheck(array, pointer, makefile, planner); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name              string
		makefile, planner []byte
		reason            string
	}{
		{"missing-case", []byte(strings.Replace(string(makefile), "int8_to_int16|", "", 1)), planner, "selected 0 times"},
		{"duplicate-case", []byte(strings.Replace(string(makefile), "(int64_to_int128|", "(int8_to_int16|int64_to_int128|", 1)), planner, "selected 2 times"},
		{"phase-selector", []byte(strings.Replace(string(makefile), "SCHEMA_PYTHON_POINTER_ARRAY_PHASE=typing", "SCHEMA_PYTHON_POINTER_ARRAY_PHASE=deep", 1)), planner, "mismatched phase selector"},
		{"missing-deep-pin", []byte(strings.Replace(string(makefile), "/floor$$'", "/missing$$'", 1)), planner, "selected 0 times"},
		{"monolith", makefile, []byte(strings.Replace(string(planner), "/^TestPythonArrayWidening$/d", "", 1)), "must be delegated"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if e := pythonCaseBudgetCheck(array, pointer, tc.makefile, tc.planner); e == nil || !strings.Contains(e.Error(), tc.reason) {
				t.Fatalf("case guard control: %v, want %s", e, tc.reason)
			}
		})
	}
}
