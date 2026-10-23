package gotable

import (
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

var goFuzzUnitDir = filepath.Join("internal", "codegen", "gotable", "fuzzunit")

// TestGoFuzzUnitIsCurrent pins internal/codegen/gotable/fuzzunit to what
// `schema generate --lang go` emits for tables/examples today, the way
// TestGoBenchTreesAreCurrent pins the bench trees. The fuzz targets in that
// directory read the committed package, so a stale copy would fuzz a reader
// the compiler no longer emits.
func TestGoFuzzUnitIsCurrent(t *testing.T) {
	t.Parallel()

	slowtest.Gate(t, "the Go toolchain (it regenerates the fuzz unit)")
	root := filepath.Clean(filepath.Join("..", "..", ".."))
	if _, err := os.Stat(filepath.Join(root, "go.mod")); err != nil {
		t.Fatalf("find repo root: %v", err)
	}
	out := t.TempDir()
	cmd := exec.Command("go", "run", "./cmd/schema", "generate", "--lang", "go", "--out", out, "tables/examples")
	cmd.Dir = root
	if data, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go run ./cmd/schema generate: %v\n%s", err, data)
	}
	filter := exec.Command("go", "run", "./"+filepath.ToSlash(filepath.Join(goFuzzUnitDir, "filter_generate.go")), out)
	filter.Dir = root
	if data, err := filter.CombinedOutput(); err != nil {
		t.Fatalf("filter generated output: %v\n%s", err, data)
	}

	emitted := map[string][]byte{}
	err := filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, ".go") {
			return err
		}
		rel, err := filepath.Rel(out, path)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		emitted[rel] = data
		return nil
	})
	if err != nil {
		t.Fatalf("walk generated output: %v", err)
	}
	if len(emitted) == 0 {
		t.Fatal("the compiler emitted no Go files for tables/examples")
	}
	names := make([]string, 0, len(emitted))
	for name := range emitted {
		names = append(names, name)
	}
	sort.Strings(names)
	dir := filepath.Join(root, goFuzzUnitDir)
	for _, name := range names {
		committed, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Errorf("%s: the compiler emits this file and it is not committed: %v", filepath.Join(goFuzzUnitDir, name), err)
			continue
		}
		if string(committed) != string(emitted[name]) {
			t.Errorf("%s: %s", filepath.Join(goFuzzUnitDir, name), goFuzzFirstDiff(committed, emitted[name]))
		}
	}
	err = filepath.WalkDir(dir, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") || d.Name() == "generate.go" || d.Name() == "filter_generate.go" {
			return nil
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if _, ok := emitted[rel]; !ok {
			t.Errorf("%s: committed .go file the compiler does not emit", filepath.Join(goFuzzUnitDir, rel))
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk %s: %v", goFuzzUnitDir, err)
	}
	t.Logf("fuzz unit compared with the in-process compiler: %d files", len(emitted))
}

func goFuzzFirstDiff(committed, emitted []byte) string {
	have := strings.Split(string(committed), "\n")
	want := strings.Split(string(emitted), "\n")
	for i := 0; i < len(have) && i < len(want); i++ {
		if have[i] != want[i] {
			return fmt.Sprintf("first differing line %d: committed %q, compiler emits %q", i+1, have[i], want[i])
		}
	}
	if len(have) != len(want) {
		return fmt.Sprintf("line counts differ: committed %d, compiler emits %d", len(have), len(want))
	}
	return "bytes differ with no differing line"
}
