package pythontable_test

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Functional: make builds the exact generated manifest units first. Every
// fixed-write recipe must answer, including each lawful boundary witness.
func TestPythonFixedFormWriteRefusal(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and generated conformance units")
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	generated := filepath.Join(repo, "build", "conformance-python")
	if _, err := os.Stat(generated); err != nil {
		t.Fatalf("fixed conformance units are absent; run make build/conformance-python: %v", err)
	}
	manifest := filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt")
	data, err := os.ReadFile(manifest)
	if err != nil {
		t.Fatal(err)
	}
	cases := map[string]string{}
	for line := range strings.SplitSeq(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 0 && fields[0] == "write-refusal-fixed" {
			cases[fields[1]] = fields[len(fields)-1]
		}
	}
	if len(cases) == 0 {
		t.Fatal("fixed-write manifest has no recipes")
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		answers := filepath.Join(dir, "answers")
		script := pyrun.ImportPrelude + fmt.Sprintf(`import importlib.util, os, sys
os.environ['PYTHON_CONFORMANCE_GENERATED']=%q
os.chdir(%q)
path=%q
sys.path.insert(0,os.path.dirname(path))
spec=importlib.util.spec_from_file_location('fixed_driver',path)
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
raise SystemExit(module.main(['driver',%q,'write-refusal-fixed',%q]))
`, generated, repo, filepath.Join(repo, "test", "conformance", "python", "main.py"), manifest, answers)
		out, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err != nil {
			t.Fatalf("fixed-write driver: %v\n%s\n%s", err, out, stderr)
		}
		for name, want := range cases {
			result, err := os.ReadFile(filepath.Join(answers, name))
			if err != nil {
				missing, _ := os.ReadFile(filepath.Join(answers, name+".absent"))
				t.Errorf("%s %s has no answer: %v %s", pin, name, err, missing)
				continue
			}
			if strings.TrimSpace(string(result)) != want {
				t.Errorf("%s %s: %q, want %s", pin, name, result, want)
			}
		}
	}
}

// Functional: corrupt the real conformance surface's refusal answer and require
// the same manifest assertion to turn red under both pinned interpreters.
func TestPythonFixedFormWriteRefusalNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the pinned interpreters and generated conformance units")
	repo, err := pyrun.RepoRoot()
	if err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{}
	for _, name := range []string{"main.py", "surfaces/_common.py", "surfaces/write-refusal-fixed.py"} {
		data, err := os.ReadFile(filepath.Join(repo, "test", "conformance", "python", name))
		if err != nil {
			t.Fatal(err)
		}
		if name == "surfaces/write-refusal-fixed.py" {
			const anchor = `write_bytes(outdir, case, b"refused\n")`
			if strings.Count(string(data), anchor) != 1 {
				t.Fatal("fixed-driver refusal control lost its anchor")
			}
			data = []byte(strings.Replace(string(data), anchor, `write_bytes(outdir, case, b"wrote\n")`, 1))
		}
		files[filepath.Join("driver", name)] = data
	}
	for _, pin := range []string{pyrun.Interpreter(t), pyrun.Floor(t)} {
		dir := t.TempDir()
		script := pyrun.ImportPrelude + fmt.Sprintf(`import importlib.util, os, sys
os.environ['PYTHON_CONFORMANCE_GENERATED']=%q
path=%q
os.chdir(%q)
spec=importlib.util.spec_from_file_location('fixed_driver',path)
module=importlib.util.module_from_spec(spec)
spec.loader.exec_module(module)
module.main(['driver',%q,'write-refusal-fixed',%q])
with open(%q,'rb') as f: answer=f.read()
if answer!=b'refused\n': raise SystemExit('fixed_driver_control')
`, filepath.Join(repo, "build", "conformance-python"), filepath.Join(dir, "driver", "main.py"), repo, filepath.Join(repo, "testdata", "conformance", "tables", "MANIFEST.txt"), filepath.Join(dir, "answers"), filepath.Join(dir, "answers", "fref_range_high"))
		for name, data := range files {
			p := filepath.Join(dir, name)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		_, stderr, err := pyrun.Try(t, pin, dir, "main.py", map[string][]byte{"main.py": []byte(script)})
		if err == nil || !strings.Contains(stderr, "fixed_driver_control") {
			t.Fatalf("fixed-driver sabotage did not turn its named manifest probe red: %v\n%s", err, stderr)
		}
	}
}
