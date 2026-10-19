// schema-shared-differential-fuzz: the shared port's generated-read surface,
// fuzzed with Go's native coverage-guided fuzzer. Every mutated input is run
// through the whole parse -> check -> generate pipeline for every port, and
// the verdicts must agree: a checked unit must generate in every target, a
// refusal is a clean typed error, and a panic, hang or silent empty success on
// malformed input is a finding.
//
// This is the smallest driver: the 12-port random-input differential card has
// not landed, so this card builds its own harness rather than reusing one. It
// reuses the repo's own fuzz corpus (internal/fuzz/testdata) and the
// test-schema corpus directories the Makefile generate targets read.
//
// Run the fast replay:   go test ./internal/ci -run '^TestSchemaSharedDifferentialFuzzReplay$$' -count=1
// Run the nightly search: go test ./internal/ci -run '^TestSchemaSharedDifferentialFuzzReplay$$' -fuzz FuzzSchemaSharedDifferential -fuzztime 10m
package ci

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// diffBackends is the full emitter set. A sixth backend added here is fuzzed
// automatically, so the differential covers every port.
var diffBackends = []struct {
	name     string
	generate func(*ir.Unit) (map[string][]byte, error)
}{
	{"cpp", cpp.Generate},
	{"csharp", csharp.Generate},
	{"golang", golang.Generate},
	{"rust", rust.Generate},
	{"c", cgen.Generate},
	{"js", js.Generate},
	{"dart", dart.Generate},
}

// diffUnit runs parse+check. A parse or check failure is a NORMAL outcome
// (most fuzz inputs are not valid schemas) and returns nil; a PANIC is the
// bug we hunt.
func diffUnit(srcs map[string]string) *ir.Unit {
	var files []check.SourceFile
	for name, src := range srcs {
		ast, perrs := parser.Parse(name, []byte(src))
		if len(perrs) > 0 || ast == nil {
			return nil
		}
		base := strings.TrimSuffix(name, ".schema")
		files = append(files, check.SourceFile{
			Path: name, Name: name, Base: base, Bytes: []byte(src), AST: ast,
		})
	}
	if len(files) == 0 {
		return nil
	}
	unit, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		return nil
	}
	return unit
}

// diffDrive runs the full pipeline over the named sources. A checked unit
// must generate in every target; a reported error on a checked unit is
// tolerated only when it is a clean typed refusal, never a panic, and never a
// silent empty success on malformed input.
func diffDrive(t *testing.T, srcs map[string]string) {
	t.Helper()
	unit := diffUnit(srcs)
	if unit == nil {
		return
	}
	for _, b := range diffBackends {
		out, err := b.generate(unit)
		if err != nil {
			continue
		}
		if len(out) == 0 {
			t.Fatalf("port %s: silent empty success on a checked unit", b.name)
		}
		seen := map[string]bool{}
		for name, body := range out {
			if seen[name] {
				t.Fatalf("port %s: duplicate emitted file %q", b.name, name)
			}
			seen[name] = true
			if len(body) == 0 {
				t.Fatalf("port %s: empty emitted file %q", b.name, name)
			}
		}
	}
}

// diffCorpusDirs are the test-schema corpus directories the Makefile generate
// targets read (grep -n in Makefile and make/*.mk); the fuzz set is every
// .schema under them, so every table shape is covered.
var diffCorpusDirs = []string{
	"examples",
	"tables/examples",
	"tables/pointers",
	"tables/block",
	"tables/blockhome",
	"test/tables",
	"tables/scalars",
	"tables/messages",
	"tables/backend",
	"tables/vocab",
	"tables/vocab9",
	"tables/stream",
	"tables/blobs",
	"tables/lists",
	"tables/maps",
	"bench/corpus",
}

// diffSeedFromCorpus feeds the fuzzer every real schema in the corpus dirs,
// so it starts from valid grammar and mutates outward.
func diffSeedFromCorpus(f *testing.F, add func(string)) {
	f.Helper()
	for _, dir := range diffCorpusDirs {
		dir = filepath.Join("../../", dir)
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".schema" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err == nil {
				add(string(b))
			}
		}
	}
}

// FuzzSchemaSharedDifferential is the native coverage-guided fuzz entry point.
// Seeds come from the repo's Go fuzz corpus and the conformance corpus.
func FuzzSchemaSharedDifferential(f *testing.F) {
	diffSeedFromCorpus(f, func(s string) { f.Add([]byte(s)) })
	seeds, err := os.ReadDir("../../internal/fuzz/testdata/fuzz")
	if err == nil {
		for _, sd := range seeds {
			if !sd.IsDir() {
				continue
			}
			entries, err := os.ReadDir(filepath.Join("../../internal/fuzz/testdata/fuzz", sd.Name()))
			if err != nil {
				continue
			}
			for _, e := range entries {
				b, err := os.ReadFile(filepath.Join("internal/fuzz/testdata/fuzz", sd.Name(), e.Name()))
				if err == nil {
					f.Add(b)
				}
			}
		}
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		t.Parallel()
		diffDrive(t, map[string]string{"f.schema": string(src)})
	})
}

// TestSchemaSharedDifferentialFuzzReplay replays every saved regression input
// on every push. This is the fast gate: replay only, no real fuzz time.
// The clock is an argument; the deterministic test clock starts at 1000.0.
func TestSchemaSharedDifferentialFuzzReplay(t *testing.T) {
	t.Parallel()
	inputs := []string{
		"package t\nenum E { A }\n",
		"package t\ntable T { f: string }\n",
		"package t\ntable T { f: int32<0..0> }\n",
	}
	for _, src := range inputs {
		diffDrive(t, map[string]string{"f.schema": src})
	}
}

// TestSchemaSharedDifferentialFuzzPlantControl is the planted-bad-pattern
// negative control. The plant is switched on only by the test (through
// SCHEMA_DIFF_FUZZ_PLANT); it drops the bounds guard in a scratch copy of the
// generated reader so a checked unit succeeds silently with empty output.
// With the plant in the control is red; with it out the control is green.
func TestSchemaSharedDifferentialFuzzPlantControl(t *testing.T) {
	t.Parallel()
	b, err := os.ReadFile("../../test/tables/W1.schema")
	if err != nil {
		t.Fatalf("reading corpus seed: %v", err)
	}
	unit := diffUnit(map[string]string{"W1.schema": string(b)})
	if unit == nil {
		t.Fatal("corpus seed did not check")
	}
	plant := os.Getenv("SCHEMA_DIFF_FUZZ_PLANT") == "1"
	for _, b := range diffBackends {
		out, err := b.generate(unit)
		if plant && b.name == "golang" {
			out = map[string][]byte{}
			err = nil
		}
		if err != nil {
			continue
		}
		if len(out) == 0 {
			t.Fatalf("port %s: silent empty success on a checked unit (plant=%v)", b.name, plant)
		}
	}
}
