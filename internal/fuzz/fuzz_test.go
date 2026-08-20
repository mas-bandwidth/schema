// Native fuzzing over the WHOLE pipeline: parse -> check -> generate in all
// five languages. The contract under test is simple and absolute: for ANY
// input bytes, the compiler either produces a unit or reports errors — it
// never panics, and it never hangs. A crash on malformed input is a compiler
// bug even when the input is nonsense, because "nonsense" is exactly what a
// hostile or mistaken author supplies (Glenn, 2026-08-13: "think of this
// like language hardening... Find ways to break the language, and then fix
// them" / "Or loop the compiler forever, or crash it and so on. Any failure
// mode.").
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
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	cgen "github.com/mas-bandwidth/schema/internal/codegen/c"
	"github.com/mas-bandwidth/schema/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/internal/codegen/csharp"
	"github.com/mas-bandwidth/schema/internal/codegen/golang"
	"github.com/mas-bandwidth/schema/internal/codegen/rust"
	"github.com/mas-bandwidth/schema/internal/parser"
	"github.com/mas-bandwidth/schema/ir"
)

// backends is the full emitter set, shared by drive and the compile fuzz
// (compile_test.go) so a sixth backend added here is fuzzed automatically.
var backends = []struct {
	name     string
	generate func(*ir.Unit) (map[string][]byte, error)
}{
	{"cpp", func(u *ir.Unit) (map[string][]byte, error) { return cpp.Generate(u, cpp.Options{}) }},
	{"csharp", csharp.Generate},
	{"golang", golang.Generate},
	{"rust", rust.Generate},
	{"c", cgen.Generate},
}

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
	for _, b := range backends {
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
// random bytes hoping to hit a brace.
func seedFromCorpus(f *testing.F, add func(string)) {
	f.Helper()
	for _, dir := range []string{"../../examples", "../../examples128"} {
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

// handSeeds are shapes aimed at the guards we have already had to add, so a
// regression in any of them is found in seconds rather than hours. Each is a
// construct that once crashed, hung, or misclassified.
var handSeeds = []string{
	"package t\n",
	"package t\nconst A = A + 1\n",
	"package t\nenum E [max = E.Max] { One }\n",
	"package t\ntype A { b B }\ntype B { a A }\n",
	"package t\ntype T { x fixed(48, 16) [min = -100, max = 100] }\n",
	"package t\ntype T { x ufixed(48, 16) [min = 0, max = 281474976710655] }\n",
	"package t\ntype T { x ufixed(16, 16) [min = -1, max = 100] }\n",
	"package t\ntype T { x ufixed(64, 0) [min = 0, max = 18446744073709551615] }\n",
	"package t\ntype T { n int32 [min = 0, max = 4294967295] }\n",
	"package t\ntype T { s string(0) }\n",
	"package t\ntype T { xs [0]uint8 }\n",
	"package t\ntype T { x float32 [min = 0, max = 1, resolution = 0] }\n",
	"package t\nobject O { p V [interpolate, quantize = 0, max = 1] \n b bool }\ntype V { x float64 }\n",
	"package t\ntype T {\n    c bool\n    if c { x uint8 } else { x uint8 }\n}\n",
	"package t\ntype T { x uint64 [min = 0, max = 18446744073709551615] }\n",
	"package t\ntype T { x int128 [min = -1267650600228229401496703205376, max = 1267650600228229401496703205376] }\n",
	"package t\nenum E [max = 2147483648] { A }\n",
	"package t\ntype T { x bits(0) }\n",
	"package t\ntype T { x bits(65) }\n",
	// issue #95: the integer extremes. INT64_MIN has no direct literal in C
	// or C++ (the literal half overflows long long before the unary minus
	// applies) and an above-INT64_MAX value has no signed rung to land on —
	// both must reach the compiler in their guarded spellings, the doubled
	// minus AT the extreme folding rather than rendering symbolically.
	"package t\nconst F = -9223372036854775808\ntype T { x int64 [min = --F, max = 100] = -9223372036854775808 }\n",
	"package t\nconst H uint64 = 18446744073709551615\ntype T { x uint64 [min = 1, max = 18446744073709551615] }\n",
	"package t\ntable R {\n    x int64 [min = -9223372036854775808, max = 100] = -9223372036854775808\n    y uint64 [min = 1, max = 18446744073709551614]\n}\n",
	// issue #100: a uint64 DEFAULT above INT64_MAX — the member-initializer
	// path must suffix it ull like every other fold of the value (an
	// unsuffixed decimal deduces unsigned, -Wimplicitly-unsigned-literal
	// under -Werror), in plain and table structs both.
	"package t\ntype T { x uint64 = 18446744073709551615 }\ntable R { y uint64 = 18446744073709551615 }\n",
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
	f.Add("package t\nconst Lim = 100\n", "package t\ntype T { hp int32 [min = 0, max = Lim] }\n")
	f.Add("package t\ntype T { hp int32 [min = 0, max = Lim] }\n", "package t\nconst Lim = 100\n")
	f.Add("package t\nenum A [max = Z.Max] { One }\n", "package t\nenum Z [max = A.Max] { Two }\n")
	f.Add("package t\nobject O { p V [interpolate, quantize = 1024] \n b bool }\n",
		"package t\ntype V { x fixed(2, 30) [min = -1, max = 1] }\n")
	f.Add("package t\ntype A { b B }\n", "package t\ntype B { a A }\n")
	f.Fuzz(func(t *testing.T, a, b string) {
		// A_ and Z_ so the pair straddles the §3.1 name sort in both
		// directions as the fuzzer swaps content between them.
		drive(t, map[string]string{"A_one.schema": a, "Z_two.schema": b})
	})
}
