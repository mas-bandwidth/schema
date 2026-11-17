package ci

import (
	"crypto/sha256"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"
)

func goSupportUnits(t *testing.T) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make/go-support-units.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var units [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 5 {
			t.Fatalf("invalid support unit: %s", line)
		}
		units = append(units, fields)
	}
	return units
}

func TestGoGeneratedSupportPreservesCompleteUnitClosure(t *testing.T) {
	t.Parallel()
	units := goSupportUnits(t)
	groups, outputs, modules := map[string]int{}, map[string]bool{}, map[string]bool{}
	var tuples []string
	for _, unit := range units {
		group, out, module, schema, mode := unit[0], unit[1], unit[2], unit[3], unit[4]
		groups[group]++
		if outputs[out] || modules[module] {
			t.Fatalf("duplicate output or module: %v", unit)
		}
		outputs[out], modules[module] = true, true
		if _, err := os.Stat(filepath.Join(repoRoot(t), schema)); err != nil {
			t.Fatal(err)
		}
		if mode != "plain" && (mode != "variable-r1" || module != "tblr1variable") {
			t.Fatalf("unexpected generation mode: %v", unit)
		}
		tuples = append(tuples, strings.Join(unit[1:], " "))
	}
	if len(units) != 34 || len(groups) != 3 || groups["base"] != 11 || groups["evolution"] != 12 || groups["collections"] != 11 {
		t.Fatalf("incomplete producer partition: %v", groups)
	}
	// The old monolithic recipe's 33 output/module/source tuples and its R1
	// variable twin are frozen independently of their new producer assignment.
	slices.Sort(tuples)
	const oldClosure = "c4822365f5a231c52159814eee34a830f3fd2fee7a2515121954f721ba8de8d9"
	if got := fmt.Sprintf("%x", sha256.Sum256([]byte(strings.Join(tuples, "\n")+"\n"))); got != oldClosure {
		t.Fatalf("support closure moved: %s", got)
	}
	// Every generated module imported by the actual rows must be present in
	// the support manifest. Inspect imports only; never compile or run rows.
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "test/conformance/go/rows/*.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatal(err)
		}
		for _, imp := range file.Imports {
			name := strings.Trim(imp.Path.Value, "\"")
			if !strings.Contains(name, ".") && !strings.Contains(name, "/") && strings.HasSuffix(name, "demo") && !modules[name] {
				t.Fatalf("row import has no generated producer: %s", name)
			}
			if strings.HasPrefix(name, "tbl") && !modules[name] {
				t.Fatalf("row import has no generated producer: %s", name)
			}
		}
	}
}

func TestGoConformanceRecipesKeepEveryDeclaredRowReachable(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), "make/go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	var recipes []string
	for _, target := range []string{"conformance-go-rows-a", "conformance-go-rows-b"} {
		_, tail, ok := strings.Cut(string(data), "\n"+target+":")
		if !ok {
			t.Fatal("missing " + target)
		}
		_, tail, _ = strings.Cut(tail, "\n")
		var recipe strings.Builder
		for line := range strings.SplitSeq(tail, "\n") {
			if !strings.HasPrefix(line, "\t") {
				break
			}
			recipe.WriteString(line)
		}
		matches := regexp.MustCompile(`-run '([^']+)'`).FindStringSubmatch(recipe.String())
		if len(matches) != 2 || !strings.HasPrefix(matches[1], "^(") || !strings.HasSuffix(matches[1], ")$$") {
			t.Fatalf("missing anchored row selector: %s", target)
		}
		recipes = append(recipes, strings.ReplaceAll(matches[1], "$$", "$"))
	}
	declared, edges := map[string]bool{}, map[string][]string{}
	files, err := filepath.Glob(filepath.Join(repoRoot(t), "test/conformance/go/rows/*_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, path := range files {
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		for _, decl := range file.Decls {
			fn, ok := decl.(*ast.FuncDecl)
			if !ok || fn.Recv != nil || !strings.HasPrefix(fn.Name.Name, "TestRow") {
				continue
			}
			declared[fn.Name.Name] = true
			ast.Inspect(fn.Body, func(node ast.Node) bool {
				call, ok := node.(*ast.CallExpr)
				if !ok {
					return true
				}
				for _, arg := range call.Args {
					if id, ok := arg.(*ast.Ident); ok && strings.HasPrefix(id.Name, "TestRow") {
						edges[fn.Name.Name] = append(edges[fn.Name.Name], id.Name)
					}
				}
				return true
			})
		}
	}
	reachable := map[string]bool{}
	var visit func(string)
	visit = func(name string) {
		if reachable[name] {
			return
		}
		reachable[name] = true
		for _, child := range edges[name] {
			visit(child)
		}
	}
	for name := range declared {
		count := 0
		for _, recipe := range recipes {
			if regexp.MustCompile(recipe).MatchString(name) {
				count++
			}
		}
		if count > 1 {
			t.Fatalf("top-level row selected twice: %s", name)
		}
		if count == 1 {
			visit(name)
		}
	}
	for name := range declared {
		if !reachable[name] {
			t.Errorf("row omitted from both recipes and their aggregate children: %s", name)
		}
	}
}

func TestGoGeneratedSupportArtifactMetadata(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	revision, err := exec.Command("git", "-C", root, "rev-parse", "HEAD").Output()
	if err != nil {
		t.Fatal(err)
	}
	pins, err := os.ReadFile(filepath.Join(root, "SIBLINGS"))
	if err != nil {
		t.Fatal(err)
	}
	pin := ""
	for line := range strings.SplitSeq(string(pins), "\n") {
		f := strings.Fields(line)
		if len(f) == 4 && f[0] == "../serialize.go" {
			pin = f[3]
		}
	}
	if pin == "" {
		t.Fatal("missing Go runtime pin")
	}
	units := goSupportUnits(t)
	for _, mode := range []string{"valid", "wrong-revision", "wrong-runtime", "changed-source", "missing-source", "extra-file", "missing-group", "wrong-module"} {
		t.Run(mode, func(t *testing.T) {
			t.Parallel()
			dir := t.TempDir()
			write := func(name string, data []byte) {
				t.Helper()
				path := filepath.Join(dir, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			inventory := map[string]string{}
			for _, unit := range units {
				relative := strings.TrimPrefix(unit[1], "build/")
				// Opaque text fixtures exercise only artifact bookkeeping.
				for _, file := range []struct{ name, text string }{{"Support.go", "opaque generated source\n"}, {"go.mod", "module " + unit[2] + "\n"}} {
					path := relative + "/" + file.name
					write(path, []byte(file.text))
					inventory[unit[0]] += fmt.Sprintf("%x  %s\n", sha256.Sum256([]byte(file.text)), path)
				}
			}
			for _, group := range []string{"base", "evolution", "collections"} {
				write("go-support-"+group+".revision", revision)
				write("go-support-"+group+".runtime", []byte(pin+"\n"))
				write("go-support-"+group+".sha256", []byte(inventory[group]))
			}
			group := "base"
			switch mode {
			case "wrong-revision":
				write("go-support-base.revision", []byte("wrong\n"))
			case "wrong-runtime":
				write("go-support-base.runtime", []byte("wrong\n"))
			case "changed-source":
				write("tables-generated-go/examples/Support.go", []byte("changed\n"))
			case "missing-source":
				if err := os.Remove(filepath.Join(dir, "tables-generated-go/examples/Support.go")); err != nil {
					t.Fatal(err)
				}
			case "extra-file":
				write("tables-generated-go/examples/Extra.go", []byte("extra\n"))
			case "missing-group":
				group = "collections"
				if err := os.Remove(filepath.Join(dir, "go-support-collections.revision")); err != nil {
					t.Fatal(err)
				}
			case "wrong-module":
				write("tables-generated-go/examples/go.mod", []byte("module other\n"))
			}
			cmd := exec.Command("sh", "tools/go-ci-support.sh", "verify", group, dir)
			cmd.Dir = root
			out, err := cmd.CombinedOutput()
			if (err == nil) != (mode == "valid") {
				t.Fatalf("%s: %v: %s", mode, err, out)
			}
		})
	}
}

func TestGoGeneratedSupportDependencyAndWorkflowContract(t *testing.T) {
	t.Parallel()
	data, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/ci-fast.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := yamlJobs(string(data))
	producer, consumer := jobs["go-generated-support"], jobs["go-test-touched"]
	for _, want := range []string{"group: [base, evolution, collections]", "tools/go-ci-support.sh produce", "timeout-minutes: 2", "go-generated-support-${{ github.run_id }}-${{ github.sha }}-${{ matrix.group }}", "if-no-files-found: error"} {
		if !strings.Contains(producer, want) {
			t.Errorf("producer misses %s", want)
		}
	}
	for _, want := range []string{"needs: [plan, go-fixed-corpus, go-generated-support]", "merge-multiple: true", "for group in base evolution collections", "tools/go-ci-support.sh verify", "make -o build/tables-generated-go/.stamp conformance-go-rows-a", "make -o build/tables-generated-go/.stamp conformance-go-rows-b"} {
		if !strings.Contains(consumer, want) {
			t.Errorf("consumer misses %s", want)
		}
	}
	if strings.Index(consumer, "tools/go-ci-support.sh verify") > strings.Index(consumer, "make -o build/tables-generated-go/.stamp") {
		t.Error("test execution precedes support verification")
	}
	event := ciEvent{Name: "pull_request", Branch: "main", HeadRef: "lang/go", Repo: "mas-bandwidth/schema", HeadRepo: "mas-bandwidth/schema", PlanPkgs: "./internal/codegen/gotable", PlanRows: `[{"make":"conformance-go-rows-a"}]`}
	for _, state := range []string{"success", "skipped", "failure", "cancelled"} {
		event.GoSupportResult = state
		if got, err := jobRunsInWorkflow(string(data), "go-test-touched", event); err != nil || got != (state == "success") {
			t.Errorf("producer %s: got=%v err=%v", state, got, err)
		}
	}
	event.GoSupportResult = "skipped"
	event.PlanRows = "[]"
	event.PlanPkgs = "./compiler"
	if got, err := jobRunsInWorkflow(string(data), "go-test-touched", event); err != nil || !got {
		t.Errorf("unrelated row blocked by skipped support: %v %v", got, err)
	}
}
