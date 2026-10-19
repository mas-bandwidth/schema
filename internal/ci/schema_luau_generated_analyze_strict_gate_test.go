package ci

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
)

// luauCorpusDirs are the schema directories the Makefile's generate targets
// read: SCHEMAS, SCHEMAS128, SCHEMAS_WIDE and the SCHEMAS_TABLES* wildcards
// (Makefile:80-108). test/tables is read file by file below, the way the same
// generate targets name its versioning rows.
var luauCorpusDirs = []string{
	"examples",
	"examples128",
	"examples-wide",
	"tables/examples",
	"tables/pointers",
	"tables/block",
	"tables/blockhome",
	"tables/messages",
	"tables/stream",
	"tables/blobs",
	"tables/scalars",
	"tables/maps",
	"tables/lists",
	"tables/arms",
	"tables/backend",
	"tables/vocab",
	"tables/vocab9",
}

// luauCorpusFiles are the bench corpus's units. bench/corpus holds more than
// one package, so it is generated one unit at a time exactly as the Makefile's
// generate targets name it (Makefile:3168-3183, 4439-4441).
var luauCorpusFiles = [][]string{
	{"bench/corpus/Bench.schema"},
	{"bench/corpus/RealWorld.schema"},
	{"bench/corpus/BenchTable.schema"},
	{"bench/corpus/Bench.schema", "bench/corpus/FixedTable.schema"},
}

// luauKnownRefusals names the corpus units the Luau target refuses by name and
// why. A refusal is not an analyzer finding: no file is generated to analyze.
// An unlisted refusal fails the gate, and a listed unit that starts generating
// fails too, so the list cannot rot.
var luauKnownRefusals = map[string]string{
	"tables/maps": "schema file Text.schema collides with the Luau runtime module text.luau the target emits beside the generated modules",
}

// TestLuauGeneratedAnalyzeStrictGate is the luau-analyze --mode=strict lens:
// it generates the schema corpus's Luau output (types, writers, readers and the
// runtime modules) and requires luau-analyze to accept every file with no
// errors. Every generated file must carry --!strict, because the analyzer's
// default mode is nonstrict and a file without the directive would only be
// linted.
func TestLuauGeneratedAnalyzeStrictGate(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	analyze := luauAnalyzer(t, root)

	var units [][]string
	for _, dir := range luauCorpusDirs {
		units = append(units, []string{dir})
	}
	units = append(units, luauCorpusFiles...)
	tables, err := filepath.Glob(filepath.Join(root, "test", "tables", "*.schema"))
	if err != nil {
		t.Fatal(err)
	}
	sort.Strings(tables)
	for _, path := range tables {
		units = append(units, []string{filepath.ToSlash(strings.TrimPrefix(path, root+string(filepath.Separator)))})
	}
	if len(units) == 0 {
		t.Fatal("the Luau corpus is empty — this gate would pass over nothing")
	}

	runtimeNames := map[string]bool{}
	for name := range luau.Runtime() {
		runtimeNames[name] = true
	}
	for name := range luautable.Runtime() {
		runtimeNames[name] = true
	}
	refusals := map[string]string{}
	var dirs []string
	analyzed, files := 0, 0
	for _, paths := range units {
		key := strings.Join(paths, "+")
		dir, n, err := luauGenerateUnit(t, root, paths)
		if err != nil {
			refusals[key] = err.Error()
			continue
		}
		dirs = append(dirs, dir)
		analyzed++
		files += n
	}

	// One analyzer process over every module: luau-analyze starts a fresh VM
	// per invocation, so one process per unit cost 139 s on the bench, while
	// one process over the same files costs seconds. Runtime modules are
	// identical copies in every unit; analyze one representative of each
	// distinct (name, content) pair, and every module itself.
	var modules, runtimes []string
	seenRuntime := map[string]bool{}
	for _, dir := range dirs {
		generated, err := filepath.Glob(filepath.Join(dir, "*.luau"))
		if err != nil {
			t.Fatal(err)
		}
		if len(generated) == 0 {
			t.Fatalf("%s has no generated .luau files", dir)
		}
		for _, path := range generated {
			data, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(data, []byte("--!strict\n")) {
				t.Errorf("%s does not open with --!strict", path)
			}
			if runtimeNames[filepath.Base(path)] {
				sum := sha256.Sum256(data)
				key := filepath.Base(path) + ":" + hex.EncodeToString(sum[:])
				if !seenRuntime[key] {
					seenRuntime[key] = true
					runtimes = append(runtimes, path)
				}
				continue
			}
			modules = append(modules, path)
		}
	}
	sort.Strings(modules)
	sort.Strings(runtimes)
	if out, err := luauAnalyzeFiles(analyze, root, append(modules, runtimes...)); err != nil {
		t.Fatalf("luau-analyze reports errors: %v\n%s", err, out)
	}
	for key, why := range refusals {
		if _, ok := luauKnownRefusals[key]; !ok && !isLuauKnownRefusal(key, why) {
			t.Errorf("unit %s is refused and the known-refusal list does not name it: %s", key, why)
		}
	}
	for key := range luauKnownRefusals {
		if _, ok := refusals[key]; !ok {
			t.Errorf("unit %s is listed as refused and now generates; take it off the list", key)
		}
	}
	if analyzed == 0 || files == 0 {
		t.Fatal("no Luau output was analyzed")
	}
	t.Logf("luau-analyze --mode=strict: %d units, %d generated .luau files, %d named refusals", analyzed, files, len(refusals))
}

// TestLuauGeneratedAnalyzeStrictGatePlantedPattern proves the gate fires: it
// copies one generated unit into a scratch directory, confirms it analyzes
// clean, then plants a strict type error and requires luau-analyze to exit
// non-zero and name the plant. The plant lives only in the scratch copy.
func TestLuauGeneratedAnalyzeStrictGatePlantedPattern(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	analyze := luauAnalyzer(t, root)
	dir, _, err := luauGenerateUnit(t, root, []string{"examples"})
	if err != nil {
		t.Fatalf("generate examples: %v", err)
	}
	if out, err := luauAnalyzeRaw(analyze, dir); err != nil {
		t.Fatalf("the clean generated unit does not analyze: %v\n%s", err, out)
	}

	plant := "luau_generated_analyze_strict_gate_planted_pattern"
	target := filepath.Join(dir, "Wire.luau")
	data, err := os.ReadFile(target)
	if err != nil {
		t.Fatal(err)
	}
	planted := []byte(fmt.Sprintf("\nlocal %s: number = \"this string is not a number\"\n", plant))
	marker := []byte("\nreturn _M\n")
	at := bytes.LastIndex(data, marker)
	if at < 0 {
		t.Fatalf("%s has no final return _M to plant before", target)
	}
	data = append(append(append([]byte(nil), data[:at]...), planted...), data[at:]...)
	if err := os.WriteFile(target, data, 0o644); err != nil {
		t.Fatal(err)
	}
	out, err := luauAnalyzeRaw(analyze, dir)
	if err == nil {
		t.Fatalf("luau-analyze accepted the planted pattern %q; the gate has no blade", plant)
	}
	if !strings.Contains(string(out), plant) {
		t.Fatalf("luau-analyze rejected the plant but did not name it: %v\n%s", err, out)
	}
}

func luauAnalyzer(t *testing.T, root string) string {
	t.Helper()
	if p := os.Getenv("LUAU_ANALYZE"); p != "" {
		if _, err := exec.LookPath(p); err != nil {
			t.Fatalf("tool missing: luau-analyze ($LUAU_ANALYZE=%s does not resolve)", p)
		}
		return p
	}
	p := filepath.Join(root, "dist", "luau-"+luaurun.Version, "luau-analyze")
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("tool missing: luau-analyze (the pinned %s is not at %s; run make dist-luau)", luaurun.Version, p)
	}
	return p
}

// luauGenerateUnit loads one corpus unit and writes its Luau type surface, its
// table surface and both runtimes into a fresh directory.
func luauGenerateUnit(t *testing.T, root string, paths []string) (string, int, error) {
	t.Helper()
	absolute := make([]string, len(paths))
	for i, path := range paths {
		if filepath.IsAbs(path) {
			absolute[i] = path
		} else {
			absolute[i] = filepath.Join(root, filepath.FromSlash(path))
		}
	}
	gathered, err := compiler.GatherPaths(absolute)
	if err != nil {
		return "", 0, err
	}
	u, err := compiler.New().Load(gathered)
	if err != nil {
		return "", 0, err
	}
	files, err := luau.Generate(u)
	if err != nil {
		return "", 0, err
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		return "", 0, err
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, len(files), nil
}

func luauAnalyzeFiles(analyze, dir string, files []string) ([]byte, error) {
	if len(files) == 0 {
		return nil, fmt.Errorf("no files to analyze")
	}
	args := append([]string{"--mode=strict", "--formatter=gnu"}, files...)
	cmd := exec.Command(analyze, args...)
	cmd.Dir = dir
	return cmd.CombinedOutput()
}

func luauAnalyzeRaw(analyze, dir string) ([]byte, error) {
	files, err := filepath.Glob(filepath.Join(dir, "*.luau"))
	if err != nil {
		return nil, err
	}
	sort.Strings(files)
	return luauAnalyzeFiles(analyze, dir, files)
}

// isLuauKnownRefusal tolerates the directory form of a named refusal, whose
// key is the directory rather than the manifest name.
func isLuauKnownRefusal(key, why string) bool {
	return key == "tables/maps" && strings.Contains(why, "collides with the Luau runtime module text.luau")
}
