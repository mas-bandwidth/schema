// Native fuzzing over the WHOLE pipeline: parse -> check -> generate in all
// sixteen languages. The contract under test is simple and absolute: for ANY
// input bytes, the compiler either produces a unit or reports errors — it
// never panics, and it never hangs. A crash on malformed input is a compiler
// bug even when the input is nonsense, because "nonsense" is exactly what a
// hostile or mistaken author supplies — language hardening: find ways to
// break the language, then fix the class and keep the fuzzer.
//
// Why this exists alongside the reasoned adversarial corpus: reasoning finds
// the bugs someone thought of. Fuzzing finds the ones nobody did, and keeps
// finding them after everyone stops looking. Every crasher the fuzzer finds
// gets minimized into a named case in internal/check/diagnostics_test.go —
// the fuzzer discovers, the corpus remembers.
//
// Run briefly:   go test ./internal/fuzz -run xxx -fuzz FuzzPipeline -fuzztime 60s
// Run overnight: go test ./internal/fuzz -run xxx -fuzz FuzzPipeline -fuzztime 8h
// Seeds live in testdata/fuzz/<FuzzName>/ once the fuzzer writes a crasher;
// committed crashers become permanent regression inputs and run under plain
// `go test` forever after.
package fuzz_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/dart"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/elixir"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/java"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/python"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/ruby"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/unity"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type backend struct {
	name     string // the emitter package
	target   string // the compiler target it is registered as
	generate func(*ir.Unit) (map[string][]byte, error)
}

// backends is the corpus-complete emitter set shared with property_test.go
// and compile_test.go.
var backends = []backend{
	{"cpp", "cpp", cpp.Generate},
	{"csharp", "cs", csharp.Generate},
	{"golang", "go", golang.Generate},
	{"rust", "rust", rust.Generate},
	{"c", "c", cgen.Generate},
	// the refusal-capable emitters (#164): js and dart may return
	// generate-time refusals on checked units (reserved-word collisions and
	// similar) — drive already tolerates a reported error; the panic and the
	// malformed-output cases below are the bugs.
	{"js", "js", js.Generate},
	{"dart", "dart", dart.Generate},
	{"java", "java", java.Generate},
	{"elixir", "elixir", elixir.Generate},
	{"lua", "lua", lua.Generate},
	{"luau", "luau", luau.Generate},
	{"squirrel", "squirrel", squirrel.Generate},
	{"moo", "moo", moo.Generate},
	{"goose", "goose", goose.Generate},
	{"lobster", "lobster", lobster.Generate},
	{"python", "python", python.Generate},
	{"ruby", "ruby", ruby.Generate},
	{"unity", "unity", unity.Generate},
}

// fuzzBackends is the set FuzzPipeline and FuzzPipelineTwoFiles drive: every
// emitter, one per compiler target. TestFuzzPipelineDrivesEveryTarget holds it
// to the compiler's own registered list, so a target registered without a row
// here is red (ruby and unity were registered and undriven until that test
// read the list instead of restating it).
var fuzzBackends = backends

// tagRegisteredTargets are the targets only a tagged build registers; their
// emitters are plain packages and are driven here in every build. None today:
// every shipped target is built in (docs/rulings.json
// every-target-default-build), so the compiler's own list is the whole roster.
var tagRegisteredTargets []string

// unitOf runs parse+check over the named sources. A parse or check failure is
// a NORMAL outcome (most fuzz inputs are not valid schemas) and returns nil,
// exactly as the real compiler stops; a PANIC is the bug we are hunting.
func unitOf(srcs map[string]string) *ir.Unit {
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

// drive runs the full pipeline over the named sources. A unit that CHECKS
// must generate in every target — and every target is driven even when
// another errors, because "cpp refused this input" says nothing about whether
// rust survives it (the old early-return left four backends unfuzzed on any
// input the first backend rejected). Reaching generation with input that then
// panics a backend means the checker accepted something it should have
// rejected — the most valuable class this harness finds, because in
// production it is an accepted schema that breaks a build.
//
// Absence-of-panic is no longer the only oracle: every byte map a backend
// returns goes through checkGenerated (oracle_test.go), so an emitter that
// "succeeds" by producing a duplicate definition or unparseable Go now fails
// here instead of in a consumer's build.
func drive(t *testing.T, srcs map[string]string) {
	t.Helper()
	unit := unitOf(srcs)
	if unit == nil {
		return
	}
	for _, b := range fuzzBackends {
		out, err := b.generate(unit)
		if err != nil {
			// A reported error on a checked unit is tolerated (targets may
			// refuse target-specific shapes); the panic is the bug.
			continue
		}
		checkGenerated(t, b.name, out)
	}
}

// seedFromCorpus feeds the fuzzer every real schema in the repo, so it starts
// from valid grammar and mutates outward rather than wandering the space of
// random bytes hoping to hit a brace. A corpus directory that is missing, or
// holds no schema, fails the target: a seed run over no seeds runs nothing and
// a campaign from no seeds starts from random bytes.
func seedFromCorpus(f *testing.F, add func(string)) {
	f.Helper()
	for _, dir := range []string{"../../examples", "../../examples128"} {
		n, err := seedDir(dir, add)
		if err != nil {
			f.Fatalf("seed corpus %s: %v", dir, err)
		}
		if n == 0 {
			f.Fatalf("seed corpus %s holds no .schema file", dir)
		}
	}
}

// seedDir adds every .schema file directly under dir and returns how many.
func seedDir(dir string, add func(string)) (int, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return 0, err
	}
	n := 0
	for _, e := range entries {
		if e.IsDir() || filepath.Ext(e.Name()) != ".schema" {
			continue
		}
		b, err := os.ReadFile(filepath.Join(dir, e.Name()))
		if err != nil {
			return n, err
		}
		add(string(b))
		n++
	}
	return n, nil
}

// TestSeedDirCountsWhatItAdds is the control on the seed count: a directory
// that is missing is an error and one with no schema counts zero, which
// seedFromCorpus turns into a failed target.
func TestSeedDirCountsWhatItAdds(t *testing.T) {
	dir := t.TempDir()
	if _, err := seedDir(filepath.Join(dir, "absent"), func(string) {}); err == nil {
		t.Fatal("a missing corpus directory seeded without error")
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.txt"), []byte("not a schema\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if n, err := seedDir(dir, func(string) {}); err != nil || n != 0 {
		t.Fatalf("a directory with no schema seeded %d (err %v), want 0", n, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "A.schema"), []byte("package t\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	var got []string
	if n, err := seedDir(dir, func(s string) { got = append(got, s) }); err != nil || n != 1 || len(got) != 1 {
		t.Fatalf("one schema seeded %d (err %v, added %d), want 1", n, err, len(got))
	}
	for _, real := range []string{"../../examples", "../../examples128"} {
		if n, err := seedDir(real, func(string) {}); err != nil || n == 0 {
			t.Fatalf("%s seeds %d schemas (err %v), want at least one", real, n, err)
		}
	}
}

// handSeeds are shapes aimed at the guards we have already had to add, so a
// regression in any of them is found in seconds rather than hours. Each is a
// construct that once crashed, hung, or misclassified.
var handSeeds = []string{
	"package t\n",
	"package t\nconst A = A + 1\n",
	"package t\nenum E | max = E.Max\n{ One }\n",
	"package t\ntype A { b B }\ntype B { a A }\n",
	"package t\ntype T { x fixed(48, 16) | min = -100, max = 100 }\n",
	"package t\ntype T { x ufixed(48, 16) | min = 0, max = 281474976710655 }\n",
	"package t\ntype T { x ufixed(16, 16) | min = -1, max = 100 }\n",
	"package t\ntype T { x ufixed(64, 0) | min = 0, max = 18446744073709551615 }\n",
	"package t\ntype T { n int32 | min = 0, max = 4294967295 }\n",
	"package t\ntype T { s string(0) }\n",
	"package t\ntype T { xs [0]uint8 }\n",
	"package t\ntype T { x float32 | min = 0, max = 1, resolution = 0 }\n",
	"package t\ntype T {\n    c bool\n    if c { x uint8 } else { x uint8 }\n}\n",
	"package t\ntype T { x uint64 | min = 0, max = 18446744073709551615 }\n",
	"package t\ntype T { x int128 | min = -1267650600228229401496703205376, max = 1267650600228229401496703205376 }\n",
	"package t\nenum E | max = 2147483648\n{ A }\n",
	"package t\ntype T { x bits(0) }\n",
	"package t\ntype T { x bits(65) }\n",
	// the integer extremes. INT64_MIN has no direct literal in C
	// or C++ (the literal half overflows long long before the unary minus
	// applies) and an above-INT64_MAX value has no signed rung to land on —
	// both must reach the compiler in their guarded spellings, the doubled
	// minus AT the extreme folding rather than rendering symbolically.
	"package t\nconst F = -9223372036854775808\ntype T { x int64 = -9223372036854775808 | min = --F, max = 100 }\n",
	"package t\nconst H uint64 = 18446744073709551615\ntype T { x uint64 = 1 | min = 1, max = 18446744073709551615 }\n",
	"package t\ntype R {\n    x int64 = -9223372036854775808 | min = -9223372036854775808, max = 100\n    y uint64 = 1 | min = 1, max = 18446744073709551614\n}\n",
	// a uint64 DEFAULT above INT64_MAX — the member-initializer
	// path must suffix it ull like every other fold of the value (an
	// unsuffixed decimal deduces unsigned, -Wimplicitly-unsigned-literal
	// under -Werror).
	"package t\ntype T { x uint64 = 18446744073709551615 }\ntype R { y uint64 = 18446744073709551615 }\n",
}

// FuzzPipeline drives a single file through the whole compiler.
func FuzzPipeline(f *testing.F) {
	seedFromCorpus(f, func(s string) { f.Add(s) })
	for _, s := range handSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		drive(t, map[string]string{"A.schema": src})
	})
}

// FuzzPipelineTwoFiles drives TWO files. This target exists because the two
// worst bugs found by hand were invisible to single-file input: rule 2b's
// composite classification and ir.FileDeps' missing range-bound edges both
// depended on one declaration living in a file that sorts AFTER its
// reference. A single-file fuzzer cannot express that, and the shipped
// corpus — single-file for every fixed-point case — is exactly why they
// survived so long.
func FuzzPipelineTwoFiles(f *testing.F) {
	f.Add("package t\nconst Lim = 100\n", "package t\ntype T { hp int32 | min = 0, max = Lim }\n")
	f.Add("package t\ntype T { hp int32 | min = 0, max = Lim }\n", "package t\nconst Lim = 100\n")
	f.Add("package t\nenum A | max = Z.Max\n{ One }\n", "package t\nenum Z | max = A.Max\n{ Two }\n")
	f.Add("package t\ntype A { b B }\n", "package t\ntype B { a A }\n")
	f.Fuzz(func(t *testing.T, a, b string) {
		// A_ and Z_ so the pair straddles the §3.1 name sort in both
		// directions as the fuzzer swaps content between them.
		drive(t, map[string]string{"A_one.schema": a, "Z_two.schema": b})
	})
}

// TestFuzzPipelineDrivesEveryTarget holds fuzzBackends to the compiler's own
// list, both ways: every registered target (and every target only a tagged
// build registers) has exactly one row, and every row names such a target. A
// hand-kept list of sixteen names stood here before and passed with ruby and
// unity registered and undriven.
func TestFuzzPipelineDrivesEveryTarget(t *testing.T) {
	for _, problem := range rosterProblems(fuzzBackends, append(compiler.New().Targets(), tagRegisteredTargets...)) {
		t.Error(problem)
	}

	// the control: the roster as it stood, without ruby and unity
	var before []backend
	for _, b := range fuzzBackends {
		if b.target != "ruby" && b.target != "unity" {
			before = append(before, b)
		}
	}
	problems := strings.Join(rosterProblems(before, compiler.New().Targets()), "\n")
	for _, want := range []string{"target ruby has no fuzz backend", "target unity has no fuzz backend"} {
		if !strings.Contains(problems, want) {
			t.Errorf("the roster without ruby and unity reports %q, want %q", problems, want)
		}
	}
	extra := append(append([]backend(nil), fuzzBackends...), backend{"zig", "zig", fuzzBackends[0].generate})
	if problems := strings.Join(rosterProblems(extra, append(compiler.New().Targets(), tagRegisteredTargets...)), "\n"); !strings.Contains(problems, "backend zig names target zig, which is not registered") {
		t.Errorf("a row for an unregistered target reports %q", problems)
	}

	// every row emits the minimal unit, the same bytes each time
	u := unitOf(map[string]string{"A.schema": "package t\n"})
	if u == nil {
		t.Fatal("minimal unit did not check")
	}
	for _, b := range fuzzBackends {
		first, err := b.generate(u)
		if err != nil {
			t.Errorf("%s.Generate on the minimal unit: %v", b.name, err)
			continue
		}
		for i := 1; i < 12; i++ {
			again, err := b.generate(u)
			if err != nil {
				t.Fatalf("%s.Generate pass %d: %v", b.name, i, err)
			}
			if len(again) != len(first) {
				t.Fatalf("%s.Generate pass %d: %d files, then %d", b.name, i, len(first), len(again))
			}
			for name, wantBytes := range first {
				if !bytes.Equal(wantBytes, again[name]) {
					t.Fatalf("%s.Generate pass %d: %s differs between runs", b.name, i, name)
				}
			}
		}
	}
}

func rosterProblems(roster []backend, targets []string) []string {
	rows := map[string]int{}
	var problems []string
	known := map[string]bool{}
	for _, target := range targets {
		known[target] = true
	}
	for _, b := range roster {
		if b.generate == nil {
			problems = append(problems, "backend "+b.name+" has no generate func")
		}
		if !known[b.target] {
			problems = append(problems, "backend "+b.name+" names target "+b.target+", which is not registered")
		}
		rows[b.target]++
	}
	for _, target := range targets {
		switch rows[target] {
		case 0:
			problems = append(problems, "target "+target+" has no fuzz backend: FuzzPipeline and FuzzPipelineTwoFiles never drive it")
		case 1:
		default:
			problems = append(problems, "target "+target+" has more than one fuzz backend")
		}
	}
	return problems
}
