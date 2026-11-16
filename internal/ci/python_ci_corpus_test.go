package ci

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestPythonHandtextCISelection(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make and Go test listing (hand-text support rows)")
	run := func(name string, args ...string) string {
		t.Helper()
		cmd := exec.Command(name, args...)
		cmd.Dir = repoRoot(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s %v: %v\n%s", name, args, err, out)
		}
		return string(out)
	}
	patterns := strings.Fields(run("make", "-s", "python-handtext-ci-patterns"))
	if len(patterns) != 4 {
		t.Fatalf("got %d hand-text rows, want four", len(patterns))
	}
	var expressions []*regexp.Regexp
	for _, pattern := range patterns {
		expressions = append(expressions, regexp.MustCompile(pattern))
	}
	var names []string
	for line := range strings.SplitSeq(run("go", "test", "-tags", "schema_leg_python", "-list", "^TestPythonHandtext", "./internal/codegen/pythontable/"), "\n") {
		if strings.HasPrefix(line, "TestPythonHandtext") {
			names = append(names, line)
		}
	}
	if len(names) == 0 {
		t.Fatal("hand-text test listing is empty")
	}
	missing := func(rows []*regexp.Regexp) int {
		bad := 0
		for _, name := range names {
			count := 0
			for _, row := range rows {
				if row.MatchString(name) {
					count++
				}
			}
			if count != 1 {
				bad++
			}
		}
		return bad
	}
	if bad := missing(expressions); bad != 0 {
		t.Fatalf("%d hand-text tests are missing or repeated in support rows", bad)
	}
	if missing(expressions[:3]) == 0 {
		t.Fatal("removing the control row escaped the selection check")
	}
}

func TestPythonCISupportCoversEveryPrerequisite(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "make (Python support prerequisite closure)")
	run := func(args ...string) string {
		t.Helper()
		cmd := exec.Command("make", args...)
		cmd.Dir = repoRoot(t)
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("make %v: %v\n%s", args, err, out)
		}
		return string(out)
	}
	db := map[string][]string{}
	for line := range strings.SplitSeq(run("-pn", "python-ci-support-list"), "\n") {
		if line == "" || strings.ContainsAny(line[:1], "#\t ") {
			continue
		}
		name, deps, ok := strings.Cut(line, ":")
		if ok && !strings.ContainsAny(name, " =") {
			db[name] = strings.Fields(deps)
		}
	}
	if len(db["test-python"]) == 0 {
		t.Fatal("make database contains no test-python prerequisites")
	}
	roots := strings.Fields(run("-s", "python-ci-support-list"))
	if missing := pythonSupportMissing(db, roots); len(missing) != 0 {
		t.Fatalf("test-python prerequisites absent from CI support rows: %v", missing)
	}
	// A new direct prerequisite must not silently fall outside the matrix.
	db["test-python"] = append(db["test-python"], "python-future-proof")
	if missing := pythonSupportMissing(db, roots); !slices.Contains(missing, "python-future-proof") {
		t.Fatalf("unregistered prerequisite escaped coverage: %v", missing)
	}
}

func pythonSupportMissing(db map[string][]string, roots []string) []string {
	seen := map[string]bool{}
	var walk func(string)
	walk = func(name string) {
		if seen[name] {
			return
		}
		seen[name] = true
		for _, dep := range db[name] {
			walk(dep)
		}
	}
	for _, root := range roots {
		walk(root)
	}
	var missing []string
	for _, dep := range db["test-python"] {
		if dep != "python-ci-support" && !seen[dep] {
			missing = append(missing, dep)
		}
	}
	return missing
}

func TestPythonCIPlannerProvisionsFixedCorpus(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "Go package listing (Python CI matrix)")
	cmd := exec.Command("sh", "tools/python-ci-rows.sh")
	cmd.Dir = repoRoot(t)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("plan Python rows: %v\n%s", err, out)
	}
	var rows []struct {
		Target, Package, Test string
		Corpus                bool
	}
	if err := json.Unmarshal(out, &rows); err != nil {
		t.Fatal(err)
	}
	fixed, packages := 0, 0
	for _, row := range rows {
		if strings.HasPrefix(row.Target, "python-fixed-") || strings.Contains(row.Package, "/pythontable") {
			if !row.Corpus {
				t.Errorf("fixed row has no reference corpus: %+v", row)
			}
			if row.Target != "" {
				fixed++
			} else {
				packages++
			}
		}
	}
	if fixed < 19 || packages == 0 {
		t.Fatalf("fixed support=%d, table package rows=%d; want all fixed support and package rows", fixed, packages)
	}
	// The four complete shape tests are delegated out of package shards.
	// Derive the value rows from the actual test source so adding/removing a
	// shape cannot silently leave an unmatched or stale CI support slice.
	wantShapes := map[string]int{
		"python-fixed-proof-shapes-text":             0,
		"python-fixed-proof-shapes-array-bounds":     0,
		"python-fixed-proof-shapes-negative-control": 0,
	}
	for _, name := range pythonCIShapeRows(t) {
		wantShapes["python-fixed-proof-shapes-"+name] = 0
	}
	for _, row := range rows {
		if strings.HasPrefix(row.Target, "python-fixed-proof-shapes") {
			if _, exists := wantShapes[row.Target]; !exists {
				t.Errorf("unmatched or monolithic shape support row %s", row.Target)
			} else {
				wantShapes[row.Target]++
			}
		}
		if strings.HasSuffix(row.Package, "/internal/codegen/pythontable") {
			for _, name := range []string{"TestPythonFixedProofShapes", "TestPythonFixedProofShapesText", "TestPythonFixedProofShapesArrayBounds", "TestPythonFixedProofShapesNegativeControl"} {
				matched, err := regexp.MatchString("^"+row.Test+"$", name)
				if err != nil || matched {
					t.Errorf("delegated shape test %s reintroduced in package shard %+v: %v", name, row, err)
				}
			}
		}
	}
	for name, count := range wantShapes {
		if count != 1 {
			t.Errorf("shape support %s appears %d times, want exactly once", name, count)
		}
	}
	wf, err := os.ReadFile(filepath.Join(repoRoot(t), ".github/workflows/python-leg.yml"))
	if err != nil {
		t.Fatal(err)
	}
	jobs := yamlJobs(string(wf))
	for _, want := range []string{"timeout-minutes: 2", "sh tools/python-ci-corpus.sh build", "python-fixed-corpus-${{ github.sha }}"} {
		if !strings.Contains(jobs["fixed-corpus"], want) {
			t.Errorf("corpus producer lacks %q", want)
		}
	}
	consumer := jobs["test"]
	for _, want := range []string{"needs: [plan, fixed-corpus]", "sh tools/python-ci-corpus.sh restore", "if: matrix.corpus", "--old-file=build/fixedform-corpus/.stamp", "--old-file=build/conformance-python"} {
		if !strings.Contains(consumer, want) {
			t.Errorf("consumer lacks %q", want)
		}
	}
	if strings.Index(consumer, "sh tools/python-ci-corpus.sh restore") > strings.Index(consumer, "run the selected complete-leg slice") {
		t.Fatal("reference corpus is restored after the tests")
	}
}

func TestPythonCICorpusProvenanceAndDamageControls(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "shell (offline corpus artifact and refusal controls)")
	script := filepath.Join(repoRoot(t), "tools/python-ci-corpus.sh")
	for _, name := range []string{"round trip", "wrong source", "wrong runtime pin", "wrong producer runtime", "dirty source", "missing archive", "damaged bytes", "damaged generated units"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			root := t.TempDir()
			write := func(path, body string, mode os.FileMode) {
				t.Helper()
				path = filepath.Join(root, path)
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(body), mode); err != nil {
					t.Fatal(err)
				}
			}
			const pin = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
			write("SIBLINGS", "../serialize example v1 "+pin+"\n", 0o600)
			write("bin/git", `#!/bin/sh
if [ "$1" = -C ]; then
  case "$3" in rev-parse) echo "${TEST_CPP_HEAD:-bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb}" ;; status) : ;; esac
else
  case "$1" in rev-parse) echo "${TEST_SOURCE_HEAD:-aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa}" ;; status) printf %s "${TEST_SOURCE_DIRTY:-}" ;; esac
fi
`, 0o700)
			write("bin/make", "#!/bin/sh\nmkdir -p build/fixedform-corpus build/conformance-python\nprintf generated > build/conformance-python/unit.py\nprintf wire > build/fixedform-corpus/old_int_widen.bin\nprintf manifest > build/fixedform-corpus/manifest.txt\ntouch build/fixedform-corpus/.stamp\n", 0o700)
			run := func(mode string, vars ...string) ([]byte, error) {
				cmd := exec.Command("sh", script, mode)
				cmd.Dir = root
				cmd.Env = append(stripEnv(os.Environ(), "PATH"), "PATH="+filepath.Join(root, "bin")+":"+os.Getenv("PATH"))
				cmd.Env = append(cmd.Env, vars...)
				return cmd.CombinedOutput()
			}
			if out, err := run("build"); err != nil {
				t.Fatalf("build fixture artifact: %v\n%s", err, out)
			}
			var vars []string
			mode, want := "restore", ""
			switch name {
			case "wrong source":
				vars, want = []string{"TEST_SOURCE_HEAD=cccccccccccccccccccccccccccccccccccccccc"}, "source SHA mismatch"
			case "wrong runtime pin":
				write("SIBLINGS", "../serialize example v2 cccccccccccccccccccccccccccccccccccccccc\n", 0o600)
				want = "runtime pin mismatch"
			case "wrong producer runtime":
				mode, vars, want = "build", []string{"TEST_CPP_HEAD=cccccccccccccccccccccccccccccccccccccccc"}, "runtime does not match"
			case "dirty source":
				mode, vars, want = "build", []string{"TEST_SOURCE_DIRTY= M compiler/source.go"}, "source checkout is dirty"
			case "missing archive":
				if err := os.Remove(filepath.Join(root, "build/python-ci-fixed-corpus.tar")); err != nil {
					t.Fatal(err)
				}
				want = "Cannot open"
			case "damaged bytes", "damaged generated units":
				file := "fixedform-corpus/old_int_widen.bin"
				if name == "damaged generated units" {
					file = "conformance-python/unit.py"
				}
				cmd := exec.Command("sh", "-ec", "mkdir unpack; tar -xf build/python-ci-fixed-corpus.tar -C unpack; printf damaged > unpack/"+file+"; tar -cf build/python-ci-fixed-corpus.tar -C unpack .")
				cmd.Dir = root
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("damage fixture: %v\n%s", err, out)
				}
				want = "FAILED"
			}
			out, err := run(mode, vars...)
			if want == "" {
				if err != nil {
					t.Fatalf("restore valid artifact: %v\n%s", err, out)
				}
			} else if err == nil || !strings.Contains(string(out), want) {
				t.Fatalf("%s did not refuse by %q: err=%v\n%s", name, want, err, out)
			}
		})
	}
}

// pythonCIShapeRows reads the proof's own row selection, independently of the
// Makefile's shard list. The ordinary Go tests still run the original suite.
func pythonCIShapeRows(t *testing.T) []string {
	t.Helper()
	file, err := parser.ParseFile(token.NewFileSet(), filepath.Join(repoRoot(t), "internal/codegen/pythontable/fixed_proof_shapes_test.go"), nil, 0)
	if err != nil {
		t.Fatal(err)
	}
	for _, decl := range file.Decls {
		fn, ok := decl.(*ast.FuncDecl)
		if !ok || fn.Name.Name != "TestPythonFixedProofShapes" {
			continue
		}
		for _, stmt := range fn.Body.List {
			assign, ok := stmt.(*ast.AssignStmt)
			if !ok || len(assign.Lhs) != 1 || len(assign.Rhs) != 1 {
				continue
			}
			name, ok := assign.Lhs[0].(*ast.Ident)
			if !ok || name.Name != "rows" {
				continue
			}
			list, ok := assign.Rhs[0].(*ast.CompositeLit)
			if !ok || len(list.Elts) == 0 {
				t.Fatal("shape proof rows are not an explicit nonempty list")
			}
			var rows []string
			for _, elem := range list.Elts {
				literal, ok := elem.(*ast.BasicLit)
				if !ok {
					t.Fatal("shape proof row is not a literal")
				}
				value, err := strconv.Unquote(literal.Value)
				if err != nil || value == "" || slices.Contains(rows, value) {
					t.Fatalf("invalid or duplicate shape proof row %q: %v", literal.Value, err)
				}
				rows = append(rows, value)
			}
			return rows
		}
	}
	t.Fatal("shape proof row selection not found")
	return nil
}
