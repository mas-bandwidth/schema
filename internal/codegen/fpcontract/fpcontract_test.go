// Package fpcontract_test is SPEC §7.2's no-contraction gate, written as a
// grep over the emitters and the build system rather than as a runtime check.
//
// The quantize fold is `normalized * steps + 0.5` with TWO roundings. A
// compiler that contracts the multiply and the add into one fused
// multiply-add keeps the extra precision and writes a DIFFERENT step index:
// 0.005 over [0, 10] at resolution 0.01 quantizes to 1 under the law and to 0
// fused. That is a wrong-bytes bug, not a rounding nicety, and clang's DEFAULT
// for C on arm64 takes the permission — so an unflagged C build in this tree
// is a build that writes the wrong wire.
//
// Contraction cannot be tested away after the fact: it is a permission, and
// the only durable gate is that no build grants it and no emitter asks for it.
// These tests are that gate. They read source, so they cost nothing and hold
// on every platform, including the ones whose toolchain is not installed here.
package fpcontract_test

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot walks up from this package to the tree root (the directory holding
// go.mod), so the test runs from any working directory.
func repoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 8; i++ {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		dir = filepath.Dir(dir)
	}
	t.Fatal("go.mod not found above the test's working directory")
	return ""
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(b)
}

// stdFlag finds a compiler invocation by its -std= token. A line carrying one
// is compiling C or C++; every such line in this tree compiles generated or
// conformance sources, so every one of them must carry -ffp-contract=off.
var stdFlag = regexp.MustCompile(`-std=c(\+\+)?[0-9x]+`)

// csharpLocal is the C# message codec's narrowing local, whitespace-tolerant
// because some of the five copies are emitted minified.
var csharpLocal = regexp.MustCompile(`float\s+scaled\s*=\s*normalized\s*\*`)

// TestNoContractionFlagOnEveryCompile is the build-system half of §7.2. It
// reads the LOGICAL line (backslash continuations joined), because the flag
// legitimately sits on a continuation in the multi-line recipes.
func TestNoContractionFlagOnEveryCompile(t *testing.T) {
	root := repoRoot(t)
	var files []string
	files = append(files, filepath.Join(root, "Makefile"))
	for _, pat := range []string{"make/*.mk", "make/checks/*.mk", "bench/*.sh", "bench/tables/*.sh", "bench/tables/*/leg"} {
		m, err := filepath.Glob(filepath.Join(root, pat))
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, m...)
	}
	checked := 0
	for _, f := range files {
		lines := strings.Split(read(t, f), "\n")
		for i := 0; i < len(lines); i++ {
			// a comment quoting the flags is documentation, not a build
			trimmed := strings.TrimLeft(lines[i], " \t")
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if !stdFlag.MatchString(lines[i]) {
				continue
			}
			j := i
			for j < len(lines) && strings.HasSuffix(strings.TrimRight(lines[j], " \t"), `\`) {
				j++
			}
			logical := strings.Join(lines[i:min(j+1, len(lines))], "\n")
			checked++
			if !strings.Contains(logical, "-ffp-contract=off") {
				rel, _ := filepath.Rel(root, f)
				t.Errorf("%s:%d compiles C/C++ without -ffp-contract=off — clang's default CONTRACTS and writes a different quantize index (SPEC §7.2):\n\t%s",
					rel, i+1, strings.TrimSpace(lines[i]))
			}
			i = j
		}
	}
	if checked < 20 {
		t.Fatalf("only %d compile lines found — the glob has drifted and this gate is vacuous", checked)
	}
}

// TestNoFusedMultiplyAddIntrinsics is the Rust, Java and C# half. Those three
// toolchains do not contract on their own: a fused product is reachable only
// through an explicit intrinsic. So the rule is simply that no emitter writes
// one into generated code.
func TestNoFusedMultiplyAddIntrinsics(t *testing.T) {
	root := repoRoot(t)
	// table-driven so a new forbidden spelling is one line
	forbidden := []struct{ lang, needle string }{
		{"Rust", "mul_add"},
		{"Java", "Math.fma"},
		{"C#", "FusedMultiplyAdd"},
		{"JavaScript", "Math.fma"},
	}
	dirs := []string{"rust", "rusttable", "java", "javatable", "csharp", "cstable", "js", "jstable", "dart", "darttable", "elixir", "elixirtable", "golang", "gotable"}
	for _, d := range dirs {
		base := filepath.Join(root, "internal", "codegen", d)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			src := read(t, path)
			for _, f := range forbidden {
				if strings.Contains(src, f.needle) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s: emits %q (%s fused multiply-add) — a fused product writes a different quantize index (SPEC §7.2)", rel, f.needle, f.lang)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
	}
}

// TestNoContractionInEmittedCode is the source-level half, per leg. Each
// language forbids fusion by forcing an intermediate rounding, and the
// spelling differs per language; this asserts the spelling is still there.
// The emitters are read as text rather than exercised, because the failure
// being guarded against is an editor "simplifying" the wrap away — which no
// runtime test on a non-fusing host would ever catch.
func TestNoContractionInEmittedCode(t *testing.T) {
	root := repoRoot(t)
	cases := []struct {
		lang, file string
		// wrap is the conversion that forces the intermediate rounding
		wrap string
	}{
		{"Go (packet form)", "internal/codegen/golang/functions.go", "float32("},
		{"Go (flat form)", "internal/codegen/golang/flat.go", "float32("},
		{"JavaScript (flat form)", "internal/codegen/js/flat.go", "Math.fround("},
		{"Dart", "internal/codegen/dart/functions.go", "_fround("},
		{"Elixir", "internal/codegen/elixir/functions.go", "fr("},
	}
	for _, c := range cases {
		src := read(t, filepath.Join(root, c.file))
		if !strings.Contains(src, c.wrap) {
			t.Errorf("%s (%s): the %q wrap that forces the intermediate rounding is gone — fusion is permitted again (SPEC §7.2)", c.lang, c.file, c.wrap)
		}
	}

	// C and C++ fixed form: the fold must go through a NAMED float object, so
	// the assignment forces the rounding in the CONSUMER's build too, whose
	// flags are not ours. Verified on arm64 clang under -ffp-contract=fast:
	// with the named object the index is 1, with one expression it is 0.
	// C, C++ and C# all spell the guard the same way: a named `float` object.
	// C# is here rather than above because RyuJIT does not contract at all —
	// the named object is what keeps the three legs' SOURCE identical, which is
	// how a reader checks them against each other.
	// C# narrows through a `float`-typed local in FIVE near-duplicate copies;
	// a sixth copy added without the local is exactly the drift this catches.
	csharpCopies := []string{"message.go", "regionmessagewrite.go", "messageread.go", "regionmessage.go", "retainmessage.go"}
	for _, f := range csharpCopies {
		src := read(t, filepath.Join(root, "internal", "codegen", "cstable", f))
		// whitespace-tolerant: some of the five copies are emitted minified
		if !csharpLocal.MatchString(src) {
			t.Errorf("internal/codegen/cstable/%s: the quantize fold no longer narrows through a `float`-typed local (SPEC §7.2)", f)
		}
	}

	for _, f := range []string{"internal/codegen/ctable/message.go", "internal/codegen/cpptable/message.go"} {
		src := read(t, filepath.Join(root, f))
		if !strings.Contains(src, "float scaled = normalized *") {
			t.Errorf("%s: the quantize fold no longer goes through a named `float scaled` object — an assignment to a float lvalue is what forbids contraction at the source level, whatever the consumer's flags are (SPEC §7.2)", f)
		}
		if !strings.Contains(src, "scaled + 0.5f") {
			t.Errorf("%s: the +0.5f no longer reads the named `scaled` object — folding it back into one expression re-permits the fused multiply-add (SPEC §7.2)", f)
		}
		// the second barrier: an empty __asm__ that pins `scaled` into a float
		// register, with a volatile-float fallback off gcc and clang. Belt to
		// the named object's braces, for the consumer build we do not control.
		if !strings.Contains(src, "FLOAT_FORCE_ROUND") {
			t.Errorf("%s: the FORCE_ROUND barrier is gone — the named float object is then the only thing standing between a consumer's -ffp-contract=fast build and the wrong quantize index (SPEC §7.2)", f)
		}
	}
}

// TestTheDeterminismCaseStillDiscriminates keeps the witness honest. These are
// the values where a fused multiply-add changes the index, found by an
// EXHAUSTIVE sweep of every float32 in each range (not by sampling), so the
// list is complete for the ranges named. If the arithmetic below ever stops
// discriminating, the pinned wire vectors have stopped proving anything.
func TestTheDeterminismCaseStillDiscriminates(t *testing.T) {
	for _, c := range []struct {
		name            string
		value, min, max float64
		resolution      float64
		plain, fused    uint32
	}{
		// the case named in internal/codegen/golang/functions.go
		{"0.005 over [0,10] @ 0.01", 0.005, 0, 10, 0.01, 1, 0},
		// two more, same shape, different step counts
		{"0.049999997 over [0,100] @ 0.1", 0.049999997, 0, 100, 0.1, 1, 0},
		{"0.000499999966 over [0,1] @ 0.001", 0.000499999966, 0, 1, 0.001, 1, 0},
	} {
		min32 := float32(c.min)
		delta := float32(c.max) - min32
		var steps float32
		{
			// ceil((max-min)/resolution), the step count the emitters fold
			q := (c.max - c.min) / c.resolution
			n := uint32(q)
			if float64(n) < q {
				n++
			}
			steps = float32(n)
		}
		n := (float32(c.value) - min32) / delta
		if !(n >= 0) {
			n = 0
		} else if !(n <= 1) {
			n = 1
		}
		// the law: two roundings. Go forbids fusion across the conversion.
		plain := uint32(float32(n*steps) + 0.5)
		if plain != c.plain {
			t.Errorf("%s: two-rounding index = %d, want %d — the law itself has moved", c.name, plain, c.plain)
		}
		// the fused answer, computed explicitly so the gate does not depend on
		// whether THIS host's compiler would have fused
		fused := uint32(fma32(n, steps, 0.5))
		if fused != c.fused {
			t.Errorf("%s: fused index = %d, want %d", c.name, fused, c.fused)
		}
		if plain == fused {
			t.Errorf("%s: STOPPED DISCRIMINATING — fused and unfused now agree (%d), so this vector no longer proves the no-contraction rule (SPEC §7.2)", c.name, plain)
		}
	}
}

// fma32 is the fused product, computed in float64 and narrowed once. For the
// magnitudes here (a normalized value in [0,1] times a step count under 2^24)
// float64 carries the exact product, so one narrowing is exactly what a
// hardware FMA does.
func fma32(x, y, z float32) float32 {
	return float32(float64(x)*float64(y) + float64(z))
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
