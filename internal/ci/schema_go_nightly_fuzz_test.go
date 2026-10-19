package ci

import (
	"bytes"
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

// TestNightlyFuzzRegressionCorpusIsPresent pins the per-push replay corpus. At
// attempt 1's head the directory did not exist and the replay test continued
// past a missing ReadDir, so it passed on zero inputs. An absent kind is now a
// red here and in the generated replay test.
func TestNightlyFuzzRegressionCorpusIsPresent(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	for _, kind := range []string{"Load", "ReadStrict", "TextJson", "FromJson"} {
		dir := filepath.Join(root, "internal", "ci", "testdata", "nightly-fuzz-corpus", kind)
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("the committed %s regression corpus is absent: %v", kind, err)
		}
		inputs := 0
		for _, entry := range entries {
			if !entry.IsDir() {
				inputs++
			}
		}
		if inputs == 0 {
			t.Fatalf("the committed %s regression corpus has no saved input", kind)
		}
	}
}

// TestNightlyFuzzGeneratorEmitsJSONAndFailFastOracle pins the two harness
// defects at attempt 1's head: text/JSON parse only ever reached LoadText (a
// base64 wire decode) and oracle violations were log lines, not failures.
func TestNightlyFuzzGeneratorEmitsJSONAndFailFastOracle(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	gen := filepath.Join(root, "internal", "ci", "nightlyfuzzgen.go")

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "go.mod"), []byte("module nightlyfuzz\n\ngo 1.23\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	pkg := filepath.Join(dir, "cells")
	if err := os.MkdirAll(pkg, 0o755); err != nil {
		t.Fatal(err)
	}
	fake := `package cells

type TableReport struct{}

func (r *TableReport) Err() error { return nil }

type Cells struct{ Value uint32 }

func CellsReset(value *Cells) {}

func CellsLoad(value *Cells, data []byte, report *TableReport) bool { return false }

func CellsReadStrict(wire []byte) (*Cells, error) { return nil, nil }
`
	if err := os.WriteFile(filepath.Join(pkg, "cells.go"), []byte(fake), 0o644); err != nil {
		t.Fatal(err)
	}
	list := filepath.Join(dir, "corpus.txt")
	if err := os.WriteFile(list, []byte(pkg+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command("go", "run", gen, list, dir)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), "GOFLAGS=-mod=readonly")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate the scratch harness: %v\n%s", err, out)
	}
	src, err := os.ReadFile(filepath.Join(dir, "nightly_test.go"))
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"func FuzzFromJson(",
		".json(data[1:])",
		"CellsFromJson(&v, data, report)",
		"errors.As(err, &oracle)",
		"checkRefusal(t, err)",
		"func decodeSaved(",
	} {
		if !bytes.Contains(src, []byte(want)) {
			t.Errorf("the generated harness is missing %q", want)
		}
	}
	if bytes.Contains(src, []byte(`t.Logf("typed refusal`)) {
		t.Error("the generated harness still logs an oracle error instead of failing")
	}
}

// TestNightlyFuzzPlantedReaderIsCaught is the planted-pattern proof for the
// nightly harness. It builds a scratch copy of the REAL generated examples
// reader, drops the Room(n) check from TableReader.Body in the copy, and runs
// the real fuzzer: FuzzLoad must find it inside the nightly budget, the saved
// input must fail replay with the plant in and pass with it out, and the same
// generated reader without the plant must go green. It is the functional tier:
// test/slowgate/proof sets SCHEMA_SLOW and requires the --- PASS line.
func TestNightlyFuzzPlantedReaderIsCaught(t *testing.T) {
	t.Parallel()
	if os.Getenv("SCHEMA_SLOW") == "" {
		t.Skip("functional tier: run through make schema-go-nightly-fuzz-negative-control")
	}
	root := repoRoot(t)
	gen := filepath.Join(root, "internal", "ci", "nightlyfuzzgen.go")
	filter := filepath.Join(root, "internal", "codegen", "gotable", "fuzzunit", "filter_generate.go")

	runFuzz := func(t *testing.T, dir string) (string, error) {
		t.Helper()
		cmd := exec.Command("go", "test", "-run", "^$", "-fuzz", "^FuzzLoad$", "-fuzztime=8s", "-parallel", "2", "-timeout", "600s")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(), "SCHEMA_GO_FUZZ_REGRESSIONS=")
		out, err := cmd.CombinedOutput()
		return string(out), err
	}
	runReplay := func(t *testing.T, dir string) (string, error) {
		t.Helper()
		cmd := exec.Command("go", "test", "-run", "^TestSavedInputsReplay$", "-count=1", "-timeout", "600s")
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"SCHEMA_GO_FUZZ_SEEDS=",
			"SCHEMA_GO_FUZZ_REGRESSIONS="+filepath.Join(dir, "regressions"))
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
		cmd := exec.Command("go", "run", "./cmd/schema", "generate", "--lang", "go", "--runtime", "standalone", "--out", pkg, "tables/examples")
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate the real examples reader: %v\n%s", err, out)
		}
		cmd = exec.Command("go", "run", filter, pkg)
		cmd.Dir = root
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("filter the generated reader: %v\n%s", err, out)
		}
		if planted {
			patchGeneratedReader(t, pkg)
		}
		list := filepath.Join(dir, "corpus.txt")
		if err := os.WriteFile(list, []byte(pkg+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		cmd = exec.Command("go", "run", gen, list, dir)
		cmd.Dir = dir
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("generate the scratch harness: %v\n%s", err, out)
		}
		for _, kind := range []string{"Load", "ReadStrict", "TextJson", "FromJson"} {
			if err := os.MkdirAll(filepath.Join(dir, "regressions", kind), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		for _, kind := range []string{"ReadStrict", "TextJson", "FromJson"} {
			if err := os.WriteFile(filepath.Join(dir, "regressions", kind, "refusal-ff"), []byte("go test fuzz v1\n[]byte(\"\\x00\\xff\")\n"), 0o644); err != nil {
				t.Fatal(err)
			}
		}
		return dir
	}

	var savedName string
	var savedData []byte

	t.Run("plant in", func(t *testing.T) {
		dir := scratch(t, true)
		out, err := runFuzz(t, dir)
		if err == nil {
			t.Fatalf("the fuzzer stayed green with Room(n) dropped from TableReader.Body:\n%s", out)
		}
		saved, _ := filepath.Glob(filepath.Join(dir, "testdata", "fuzz", "FuzzLoad", "*"))
		if len(saved) == 0 {
			t.Fatalf("the fuzzer failed but saved no minimized input:\n%s", out)
		}
		data, err := os.ReadFile(saved[0])
		if err != nil {
			t.Fatal(err)
		}
		savedName = filepath.Base(saved[0])
		savedData = data
		if err := os.WriteFile(filepath.Join(dir, "regressions", "Load", savedName), data, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr == nil {
			t.Fatalf("the saved input replayed green with the plant in:\n%s", replayOut)
		}
	})

	t.Run("plant out", func(t *testing.T) {
		if len(savedData) == 0 {
			t.Fatal("no saved input from the plant-in run")
		}
		dir := scratch(t, false)
		if err := os.WriteFile(filepath.Join(dir, "regressions", "Load", savedName), savedData, 0o644); err != nil {
			t.Fatal(err)
		}
		if replayOut, replayErr := runReplay(t, dir); replayErr != nil {
			t.Fatalf("the saved input failed replay with the plant out:\n%s", replayOut)
		}
		if out, err := runFuzz(t, dir); err != nil {
			t.Fatalf("the fuzzer went red with the plant out:\n%s", out)
		}
	})
}

// patchGeneratedReader drops the Room(n) bounds check from TableReader.Body in
// the scratch copy of the generated reader. That is the exact class the nightly
// oracle exists to catch: a length that is read from the wire and then trusted
// past the input.
func patchGeneratedReader(t *testing.T, pkg string) {
	t.Helper()
	files, err := filepath.Glob(filepath.Join(pkg, "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	patched := 0
	for _, file := range files {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		const check = "if !ok || !r.Room(n) {"
		if !bytes.Contains(data, []byte(check)) {
			continue
		}
		data = bytes.Replace(data, []byte(check), []byte("if !ok {"), 1)
		if err := os.WriteFile(file, data, 0o644); err != nil {
			t.Fatal(err)
		}
		patched++
	}
	if patched == 0 {
		t.Fatal("the generated reader has no Room(n) check in TableReader.Body to drop")
	}
}

// TestNightlyFuzzRulesLiveInOwnMakeFragment pins the card's file layout: the
// campaign's rules are in make/schema-go-nightly-fuzz.mk, which the Makefile's
// make/*.mk wildcard includes, and make/go.mk carries none of them. The
// regression fixtures live under internal/ci/testdata, not beside the test.
func TestNightlyFuzzRulesLiveInOwnMakeFragment(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	fragmentPath := filepath.Join(root, "make", "schema-go-nightly-fuzz.mk")
	fragment, err := os.ReadFile(fragmentPath)
	if err != nil {
		t.Fatalf("the campaign's make fragment is not at %s: %v", fragmentPath, err)
	}
	for _, want := range []string{
		"schema-go-nightly-fuzz:",
		"schema-go-nightly-fuzz-generate:",
		"schema-go-nightly-fuzz-replay:",
		"schema-go-nightly-fuzz-negative-control:",
		"SCHEMA_GO_NIGHTLY_FUZZ_FUZZTIME ?= 10m",
		"internal/ci/testdata/nightly-fuzz-corpus",
		"tool missing: go",
	} {
		if !strings.Contains(string(fragment), want) {
			t.Errorf("make/schema-go-nightly-fuzz.mk does not carry %q", want)
		}
	}
	goMake, err := os.ReadFile(filepath.Join(root, "make", "go.mk"))
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(goMake), "schema-go-nightly-fuzz") {
		t.Error("make/go.mk still carries the campaign's rules; they belong in make/schema-go-nightly-fuzz.mk")
	}
	if _, err := os.Stat(filepath.Join(root, "tools", "schema-go-nightly-fuzz", "nightly.mk")); !os.IsNotExist(err) {
		t.Errorf("tools/schema-go-nightly-fuzz/nightly.mk still exists: %v", err)
	}
}
