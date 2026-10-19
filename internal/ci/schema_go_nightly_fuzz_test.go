package ci

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// TestNightlyFuzzCorpusIsListedAndReal holds the nightly fuzz corpus list to
// the repository: every entry is a directory, the set is not empty, and it
// names the shape corpora the Go generate targets read. A fuzz target over a
// corpus that is not there is a green light with no blade.
func TestNightlyFuzzCorpusIsListedAndReal(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	list := filepath.Join(root, "make", "schema-go-nightly-fuzz-corpus.txt")
	data, err := os.ReadFile(list)
	if err != nil {
		t.Fatalf("read the corpus list: %v", err)
	}
	var dirs []string
	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if info, err := os.Stat(filepath.Join(root, line)); err != nil || !info.IsDir() {
			t.Errorf("corpus entry %q is not a directory under the repository", line)
			continue
		}
		dirs = append(dirs, line)
	}
	if len(dirs) == 0 {
		t.Fatal("the corpus list names no directory")
	}
	want := map[string]bool{
		"tables/examples":  false,
		"tables/pointers":  false,
		"tables/block":     false,
		"tables/blockhome": false,
		"tables/messages":  false,
		"tables/stream":    false,
		"tables/blobs":     false,
		"tables/lists":     false,
		"tables/maps":      false,
		"tables/arms":      false,
		"tables/backend":   false,
		"tables/vocab":     false,
		"tables/vocab9":    false,
		"tables/scalars":   false,
		"examples-wide":    false,
	}
	for _, dir := range dirs {
		if _, ok := want[dir]; ok {
			want[dir] = true
		}
	}
	for dir, seen := range want {
		if !seen {
			t.Errorf("the shape corpus %q is not in the nightly fuzz set", dir)
		}
	}
}

// TestNightlyFuzzPlantedReaderIsCaught is the planted-pattern proof for the
// nightly harness. It builds a scratch generated reader twice from the same
// generator: once with a planted panic on every non-empty input, once without.
// The real fuzzer (go test -fuzz) must find the plant within a few seconds, the
// saved input must fail the replay test, and the unplanted reader must go
// green. It is the functional tier: SCHEMA_SLOW names it.
func TestNightlyFuzzPlantedReaderIsCaught(t *testing.T) {
	t.Parallel()
	if os.Getenv("SCHEMA_SLOW") == "" {
		t.Skip("functional tier: set SCHEMA_SLOW=1 to run the planted fuzz control")
	}
	root := repoRoot(t)
	gen := filepath.Join(root, "internal", "ci", "nightlyfuzzgen.go")

	run := func(t *testing.T, dir string, env ...string) (string, error) {
		t.Helper()
		cmd := exec.Command("go", "test", "-run", "^$", "-fuzz", "^FuzzLoad$", "-fuzztime=3s", "-timeout", "600s")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), env...)
		out, err := cmd.CombinedOutput()
		return string(out), err
	}

	scratch := func(t *testing.T, planted bool) string {
		t.Helper()
		dir := t.TempDir()
		if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module nightlyfuzz\n\ngo 1.23\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		pkg := filepath.Join(dir, "cells")
		if err := os.MkdirAll(pkg, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(pkg, "cells.go"), []byte(fakeReader(planted)), 0o644); err != nil {
			t.Fatal(err)
		}
		list := filepath.Join(dir, "corpus.txt")
		if err := os.WriteFile(list, []byte("cells\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "run", gen, list, dir)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate the scratch harness: %v\n%s", err, out)
		}
		return dir
	}

	t.Run("plant in", func(t *testing.T) {
		dir := scratch(t, true)
		out, err := run(t, dir)
		if err == nil {
			t.Fatalf("the fuzzer stayed green with the planted panic in the reader:\n%s", out)
		}
		saved, _ := filepath.Glob(filepath.Join(dir, "testdata", "fuzz", "FuzzLoad", "*"))
		if len(saved) == 0 {
			t.Fatalf("the fuzzer failed but saved no minimized input:\n%s", out)
		}
		regressions := filepath.Join(dir, "regressions", "Load")
		if err := os.MkdirAll(regressions, 0o755); err != nil {
			t.Fatal(err)
		}
		data, err := os.ReadFile(saved[0])
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(regressions, filepath.Base(saved[0])), data, 0o644); err != nil {
			t.Fatal(err)
		}
		cmd := exec.Command("go", "test", "-run", "^TestSavedInputsReplay$", "-count=1", "-timeout", "600s")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SCHEMA_GO_FUZZ_REGRESSIONS="+filepath.Join(dir, "regressions"))
		if replayOut, replayErr := cmd.CombinedOutput(); replayErr == nil {
			t.Fatalf("the saved input replayed green with the plant in:\n%s", replayOut)
		}
	})

	t.Run("plant out", func(t *testing.T) {
		dir := scratch(t, false)
		out, err := run(t, dir)
		if err != nil {
			t.Fatalf("the fuzzer went red with the plant out:\n%s", out)
		}
	})
}

// fakeReader is a generated-reader stand-in: the entry points the harness
// discovers, with the plant switched on only when planted is true. With the
// plant in, a load of any non-empty wire panics — the class the nightly oracle
// exists to catch.
func fakeReader(planted bool) string {
	plant := ""
	if planted {
		plant = "\tif len(wire) > 0 {\n\t\tpanic(\"planted reader bug\")\n\t}\n"
	}
	return `package cells

import "os"

type TableReport struct{}

type Cells struct{ Value uint32 }

func CellsReset(value *Cells) { value.Value = 0 }

func CellsMeasure(value *Cells, context ...struct{}) int64 { return 0 }

func CellsSave(value *Cells, buffer []byte, context ...struct{}) int64 { return 0 }

func CellsLoad(region, wire []byte, report *TableReport) *Cells {
` + plant + `	var value Cells
	if len(wire) >= 4 {
		value.Value = uint32(wire[0]) | uint32(wire[1])<<8 | uint32(wire[2])<<16 | uint32(wire[3])<<24
	}
	return &value
}

func CellsReadStrict(wire []byte) (*Cells, error) {
	if len(wire) < 4 {
		return nil, os.ErrInvalid
	}
	return CellsLoad(nil, wire, &TableReport{}), nil
}

func CellsLoadText(text string, report *TableReport) (*Cells, error) {
	return &Cells{}, nil
}

func CellsToJsonMeasure(value *Cells, allocator ...struct{}) int64 { return 2 }

func CellsToJson(value *Cells, buffer []byte, allocator ...struct{}) int64 {
	if len(buffer) < 2 {
		return -1
	}
	buffer[0] = '{'
	buffer[1] = '}'
	return 2
}
`
}
