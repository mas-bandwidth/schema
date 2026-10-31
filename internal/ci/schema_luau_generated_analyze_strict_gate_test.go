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
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
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
//
// tables/maps is not here: Text.schema's unit is a table, so its module is
// TextTable.luau, not Text.luau, and it does not collide with the runtime's
// schema_text.luau (docs/SPEC-TABLES.md §11); the unit generates and is analyzed
// whole.
var luauKnownRefusals = map[string]string{}

// TestLuauGeneratedAnalyzeStrictGate is the luau-analyze --mode=strict lens:
// it generates the schema corpus's Luau output (types, writers, readers and the
// runtime modules) and requires luau-analyze to accept every file with no
// errors and no warnings: luau-analyze exits 0 on lint warnings, so the gate
// rejects any diagnostic output. Every generated file must carry --!strict,
// because the analyzer's default mode is nonstrict and a file without the
// directive would only be linted.
func TestLuauGeneratedAnalyzeStrictGate(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau-analyze (the pinned release)")
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
	// Every conformance manifest unit is analyzed, read from the manifest
	// itself so a unit added there cannot be missed here.
	seenUnit := map[string]bool{}
	for _, paths := range units {
		seenUnit[strings.Join(paths, "+")] = true
	}
	manifestUnits := luauManifestUnits(t, root)
	if len(manifestUnits) == 0 {
		t.Fatal("the conformance manifest names no units — this gate would skip them all")
	}
	for _, paths := range manifestUnits {
		if key := strings.Join(paths, "+"); !seenUnit[key] {
			seenUnit[key] = true
			units = append(units, paths)
		}
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
			for _, finding := range luauDirectiveFindings(path, data) {
				t.Error(finding)
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
	if finding := luauAnalyzerFinding(analyze, root, append(modules, runtimes...)); finding != "" {
		t.Fatal(finding)
	}
	for key, why := range refusals {
		if _, ok := luauKnownRefusals[key]; !ok {
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

// TestLuauGeneratedAnalyzeStrictGatePlantedPattern proves each of the gate's
// three refusals fires, on the same verdict functions the gate runs. Each row
// generates the examples unit into a scratch directory, confirms it passes,
// plants one fault in Wire.luau and requires the gate to refuse it by name:
//
//   - type_error: a strict type error; luau-analyze exits non-zero.
//   - lint_warning: an unused local; luau-analyze exits 0 and prints a
//     LocalUnused warning, so only warnings-as-errors catches it.
//   - nolint_directive: the same unused local under a --!nolint hot comment;
//     luau-analyze is silent, so only the directive refusal catches it.
//
// The plants live only in the scratch copies.
func TestLuauGeneratedAnalyzeStrictGatePlantedPattern(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "luau-analyze (the pinned release)")
	root := repoRoot(t)
	analyze := luauAnalyzer(t, root)

	const plant = "luau_generated_analyze_strict_gate_planted_pattern"
	rows := []struct {
		name string
		// header is inserted after the --!strict line, body before return _M.
		header, body string
		// analyzerExit is whether luau-analyze itself must exit non-zero.
		analyzerExit bool
		// silent is whether luau-analyze must print nothing, so that only the
		// directive refusal can catch the plant.
		silent bool
		// want is what the gate's finding must name.
		want string
	}{
		{"type_error", "", fmt.Sprintf("local %s: number = \"this string is not a number\"\n", plant), true, false, plant},
		{"lint_warning", "", fmt.Sprintf("do\n\tlocal %s = 1\nend\n", plant), false, false, "LocalUnused: Variable '" + plant + "'"},
		{"nolint_directive", "--!nolint LocalUnused\n", fmt.Sprintf("do\n\tlocal %s = 1\nend\n", plant), false, true, "--!nolint LocalUnused"},
	}
	for _, row := range rows {
		t.Run(row.name, func(t *testing.T) {
			t.Parallel()
			dir, _, err := luauGenerateUnit(t, root, []string{"examples"})
			if err != nil {
				t.Fatalf("generate examples: %v", err)
			}
			if finding := luauGateVerdict(analyze, dir); finding != "" {
				t.Fatalf("the clean generated unit does not pass the gate:\n%s", finding)
			}
			target := filepath.Join(dir, "Wire.luau")
			data, err := os.ReadFile(target)
			if err != nil {
				t.Fatal(err)
			}
			const strict = "--!strict\n"
			marker := []byte("\nreturn _M\n")
			at := bytes.LastIndex(data, marker)
			if !bytes.HasPrefix(data, []byte(strict)) || at < 0 {
				t.Fatalf("%s does not open with --!strict and close with return _M; nowhere to plant", target)
			}
			var planted []byte
			planted = append(planted, strict...)
			planted = append(planted, row.header...)
			planted = append(planted, data[len(strict):at+1]...)
			planted = append(planted, row.body...)
			planted = append(planted, data[at+1:]...)
			if err := os.WriteFile(target, planted, 0o644); err != nil {
				t.Fatal(err)
			}

			out, analyzeErr := luauAnalyzeFiles(analyze, dir, []string{target})
			if row.analyzerExit != (analyzeErr != nil) {
				t.Fatalf("luau-analyze exit on the %s plant: got error %v, want non-zero exit %v\n%s", row.name, analyzeErr, row.analyzerExit, out)
			}
			if row.silent != (len(bytes.TrimSpace(out)) == 0) {
				t.Fatalf("luau-analyze output on the %s plant: silent=%v, want silent=%v\n%s", row.name, len(bytes.TrimSpace(out)) == 0, row.silent, out)
			}
			finding := luauGateVerdict(analyze, dir)
			if finding == "" {
				t.Fatalf("the gate accepted the %s plant; it has no blade", row.name)
			}
			if !strings.Contains(finding, row.want) {
				t.Fatalf("the gate refused the %s plant but did not name %q:\n%s", row.name, row.want, finding)
			}
		})
	}
}

// luauAllowedDirectives are the only hot comments generated Luau may carry:
// --!strict (required, on line 1) and the code-generation hints, which change
// nothing the analyzer checks. --!nolint, --!nonstrict and --!nocheck would let
// a file pass this gate without meeting it, so any other hot comment is refused.
var luauAllowedDirectives = []string{"--!strict", "--!native", "--!optimize"}

// luauDirectiveFindings refuses a file that does not open with --!strict or
// that carries any hot comment the allowed list does not name.
func luauDirectiveFindings(path string, data []byte) []string {
	var findings []string
	if !bytes.HasPrefix(data, []byte("--!strict\n")) {
		findings = append(findings, fmt.Sprintf("%s does not open with --!strict", path))
	}
	for i, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "--!") {
			continue
		}
		directive := strings.TrimSpace(line)
		name := strings.Fields(directive)[0]
		allowed := false
		for _, a := range luauAllowedDirectives {
			if name == a {
				allowed = true
			}
		}
		if name == "--!strict" && i != 0 {
			allowed = false
		}
		if !allowed {
			findings = append(findings, fmt.Sprintf("%s:%d carries the hot comment %q; the gate allows only --!strict on line 1, --!native and --!optimize, never a suppression", path, i+1, directive))
		}
	}
	return findings
}

// luauAnalyzerFinding runs luau-analyze --mode=strict over files and returns
// the gate's refusal, or "" when the analyzer is clean. luau-analyze exits 0
// on lint warnings, so the exit code alone is not the gate: any diagnostic
// line is a finding (warnings are errors).
func luauAnalyzerFinding(analyze, dir string, files []string) string {
	out, err := luauAnalyzeFiles(analyze, dir, files)
	if err != nil {
		return fmt.Sprintf("luau-analyze reports errors: %v\n%s", err, out)
	}
	if len(bytes.TrimSpace(out)) != 0 {
		return fmt.Sprintf("luau-analyze reports warnings (warnings are errors here):\n%s", out)
	}
	return ""
}

// luauGateVerdict is the gate's whole verdict over one generated directory:
// the directive refusals of every file, then the analyzer's findings.
func luauGateVerdict(analyze, dir string) string {
	files, err := filepath.Glob(filepath.Join(dir, "*.luau"))
	if err != nil {
		return err.Error()
	}
	if len(files) == 0 {
		return dir + " has no .luau files"
	}
	sort.Strings(files)
	var findings []string
	for _, path := range files {
		data, err := os.ReadFile(path)
		if err != nil {
			return err.Error()
		}
		findings = append(findings, luauDirectiveFindings(path, data)...)
	}
	if finding := luauAnalyzerFinding(analyze, dir, files); finding != "" {
		findings = append(findings, finding)
	}
	return strings.Join(findings, "\n")
}

// luauManifestUnits is every unit line of the conformance manifest, as its
// schema paths.
func luauManifestUnits(t *testing.T, root string) [][]string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var units [][]string
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) >= 3 && f[0] == "unit" {
			units = append(units, f[2:])
		}
	}
	return units
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
	absolute := luauAbsolute(root, paths)
	return luauGenerateAbsolute(t, absolute)
}

func luauAbsolute(root string, paths []string) []string {
	absolute := make([]string, len(paths))
	for i, path := range paths {
		if filepath.IsAbs(path) {
			absolute[i] = path
		} else {
			absolute[i] = filepath.Join(root, filepath.FromSlash(path))
		}
	}
	return absolute
}

func luauGenerateAbsolute(t *testing.T, absolute []string) (string, int, error) {
	t.Helper()
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
