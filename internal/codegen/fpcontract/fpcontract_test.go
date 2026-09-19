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
//
// Every assertion here is SITE-ANCHORED on purpose. A file-wide token grep
// ("does golang/functions.go contain `float32(`?") is vacuous in this tree:
// these emitters are full of unrelated float32 conversions, so the grep stays
// green after the wrap is deleted from the quantize product — the one place
// that matters. So each case first LOCATES its leg's fold (the statement that
// multiplies the normalized value by the step count and adds 0.5) inside a
// NAMED function, fails loudly if it cannot find it, and only then asserts the
// wrap on that expression.
package fpcontract_test

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
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

// funcBody returns the lines of the named top-level Go function, from its
// `func` line up to the next one. A nil return means the function is gone:
// callers report that as a LOST SITE rather than a pass, because a gate that
// cannot find the code it guards proves nothing.
func funcBody(src, name string) []string {
	lines := strings.Split(src, "\n")
	start := -1
	for i, l := range lines {
		if strings.HasPrefix(l, "func ") && strings.Contains(l, name+"(") {
			start = i
			break
		}
	}
	if start < 0 {
		return nil
	}
	end := len(lines)
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(lines[i], "func ") {
			end = i
			break
		}
	}
	return lines[start:end]
}

// ---------------------------------------------------------------------------
// The build-system half: every compile of C or C++ in this tree carries the flag
// ---------------------------------------------------------------------------

// compilerTok matches a command's FIRST token when that token is a C or C++
// compiler: the make variables this tree uses ($(CC), $(BE_CC), $(CXX)), the
// shell spellings the bench legs use ($CXX, ${CC:-cc}) and the bare driver
// names. Matching at the command position, rather than anywhere on the line,
// is what keeps `echo "cc failed"`, `command -v cc` and a sed script that
// happens to contain the letters out of the hit set.
const compilerVar = `(?:[A-Z0-9]+_)*C(?:C|XX)(?:_[A-Z0-9]+)*` // CC, CXX, BE_CC, CXX_BIN

var compilerTok = regexp.MustCompile(`^(?:` +
	`\$\(` + compilerVar + `\)` + // $(CC), $(BE_CC)
	`|\$\{` + compilerVar + `(?::-[^}]*)?\}` + // ${CC:-cc}
	`|\$` + compilerVar + // $CXX_BIN
	`|cc|c\+\+|clang|clang\+\+|gcc|g\+\+` + // bare drivers
	`)$`)

// cmdPrefix are tokens that may sit in front of a command without being one:
// shell keywords, make's recipe prefixes, and the leading brace of a group.
var cmdPrefix = map[string]bool{
	"if": true, "then": true, "else": true, "elif": true, "do": true,
	"while": true, "until": true, "!": true, "time": true, "exec": true,
	"{": true, "(": true, "&&": true, "||": true, "|": true,
}

// envAssign is a `VAR=value` prefix, which shell allows in front of a command.
var envAssign = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*=`)

// compileEvidence is what separates a compile from a mention of a compiler: an
// output file, a -c, or a translation unit on the command line. Without it the
// sweep picks up `clang` inside a quoted progress string and inside an awk
// process-name alternation.
var compileEvidence = regexp.MustCompile(`(?:^| )-[oc](?: |$)|\.(?:c|cc|cpp|cxx|m|mm)(?:$|[ "'"'"'])`)

// cmdSep splits a logical line into command segments.
var cmdSep = regexp.MustCompile(`;|&&|\|\||\|`)

// assign matches a make or shell variable definition.
var assign = regexp.MustCompile(`(?s)^\s*(?:export\s+)?([A-Za-z_][A-Za-z0-9_]*)\s*(?::|\+|\?)?=(.*)$`)

// varRef finds every variable this text reads, in either make or shell spelling.
var varRef = regexp.MustCompile(`\$[({]?([A-Za-z_][A-Za-z0-9_]*)`)

const noContract = "-ffp-contract=off"

// logical is one build COMMAND: its continuations joined into a single line,
// the line number of its first physical line so a failure points at something
// editable, and that first line for the message.
type logical struct {
	num   int
	text  string
	first string
}

// logicalLines joins backslash continuations into one line, because the flag
// legitimately sits on a continuation in the multi-line recipes — and because a
// command split at its own continuations loses the `-o` that marks it a compile.
func logicalLines(src string) []logical {
	lines := strings.Split(src, "\n")
	var out []logical
	for i := 0; i < len(lines); i++ {
		j := i
		var parts []string
		for {
			parts = append(parts, strings.TrimSuffix(strings.TrimRight(lines[j], " \t"), `\`))
			if j+1 >= len(lines) || !strings.HasSuffix(strings.TrimRight(lines[j], " \t"), `\`) {
				break
			}
			j++
		}
		out = append(out, logical{num: i + 1, text: strings.Join(parts, " "), first: strings.TrimSpace(lines[i])})
		i = j
	}
	return out
}

// buildFiles is every file in this tree that can compile something: the root
// Makefile plus every makefile, shell script and bench leg under make/ and
// bench/, found RECURSIVELY. A non-recursive glob is how bench/tools/*.sh came
// to hold real compiles of generated C that no gate had ever read.
func buildFiles(t *testing.T, root string) []string {
	t.Helper()
	files := []string{filepath.Join(root, "Makefile")}
	for _, dir := range []string{"make", "bench"} {
		err := filepath.Walk(filepath.Join(root, dir), func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() {
				// results/ holds write-ups that quote flags; not builds
				if info.Name() == "results" {
					return filepath.SkipDir
				}
				return nil
			}
			base := info.Name()
			if base == "Makefile" || base == "leg" ||
				strings.HasSuffix(base, ".mk") || strings.HasSuffix(base, ".sh") {
				files = append(files, path)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", dir, err)
		}
	}
	sort.Strings(files)
	return files
}

// flagCarryingVars is the set of variables whose value holds the flag, closed
// under reference: TABLES_CXXFLAGS spells it out, so a line that says
// `$(CXX) $(TABLES_CXXFLAGS)` is flagged even though the line itself does not
// contain the token.
func flagCarryingVars(all map[string][]string) map[string]bool {
	marked := map[string]bool{}
	for name, vals := range all {
		for _, v := range vals {
			if strings.Contains(v, noContract) {
				marked[name] = true
			}
		}
	}
	for changed := true; changed; {
		changed = false
		for name, vals := range all {
			if marked[name] {
				continue
			}
			for _, v := range vals {
				for _, m := range varRef.FindAllStringSubmatch(v, -1) {
					if marked[m[1]] {
						marked[name] = true
						changed = true
					}
				}
			}
		}
	}
	return marked
}

// compilesHere reports whether this logical line runs a compiler. Version and
// capability probes are not compiles: they emit no code, so contraction cannot
// reach them.
func compilesHere(text string) bool {
	for _, seg := range cmdSep.Split(text, -1) {
		fields := strings.Fields(seg)
		// drop make's recipe prefixes from the front of the segment
		if len(fields) > 0 {
			fields[0] = strings.TrimLeft(fields[0], "@-+\t ")
			if fields[0] == "" {
				fields = fields[1:]
			}
		}
		for len(fields) > 0 && (cmdPrefix[fields[0]] || envAssign.MatchString(fields[0])) {
			fields = fields[1:]
		}
		if len(fields) == 0 || !compilerTok.MatchString(fields[0]) {
			continue
		}
		rest := " " + strings.Join(fields[1:], " ") + " "
		if strings.Contains(rest, " --version ") || strings.Contains(rest, " -E ") ||
			strings.Contains(rest, " -dumpversion ") {
			continue
		}
		// a compiler NAME inside a quoted string or an awk alternation is not a
		// compile; a compile names an output or a translation unit
		if !compileEvidence.MatchString(rest) {
			continue
		}
		return true
	}
	return false
}

// TestNoContractionFlagOnEveryCompile is the build-system half of §7.2: every
// compiler invocation in this tree must be flagged, whether or not it spells a
// -std= out, and wherever in make/ or bench/ it happens to live.
func TestNoContractionFlagOnEveryCompile(t *testing.T) {
	root := repoRoot(t)
	files := buildFiles(t, root)

	// pass 1: what every variable holds
	defs := map[string][]string{}
	srcs := map[string][]logical{}
	for _, f := range files {
		ls := logicalLines(read(t, f))
		srcs[f] = ls
		for _, l := range ls {
			if m := assign.FindStringSubmatch(l.text); m != nil {
				defs[m[1]] = append(defs[m[1]], m[2])
			}
		}
	}
	marked := flagCarryingVars(defs)

	// pass 2: every compile must reach the flag, literally or through one of them
	checked := 0
	for _, f := range files {
		rel, _ := filepath.Rel(root, f)
		for _, l := range srcs[f] {
			trimmed := strings.TrimLeft(l.text, " \t")
			// a comment quoting the flags is documentation, not a build
			if strings.HasPrefix(trimmed, "#") || strings.HasPrefix(trimmed, "//") {
				continue
			}
			if !compilesHere(l.text) {
				continue
			}
			checked++
			if strings.Contains(l.text, "-ffp-contract=fast") || strings.Contains(l.text, "-ffp-contract=on") {
				t.Errorf("%s:%d GRANTS contraction explicitly — the fused product writes a different quantize index (SPEC §7.2):\n\t%s", rel, l.num, l.first)
				continue
			}
			if strings.Contains(l.text, noContract) {
				continue
			}
			via := ""
			for _, m := range varRef.FindAllStringSubmatch(l.text, -1) {
				if marked[m[1]] {
					via = m[1]
					break
				}
			}
			if via != "" {
				continue
			}
			t.Errorf("%s:%d compiles C/C++ without %s and reads no variable that carries it — clang's default CONTRACTS and writes a different quantize index (SPEC §7.2):\n\t%s",
				rel, l.num, noContract, l.first)
		}
	}
	if checked < 150 {
		t.Fatalf("only %d compile lines found — the sweep has drifted and this gate is vacuous", checked)
	}
}

// ---------------------------------------------------------------------------
// The intrinsic half: no emitter reaches for a fused multiply-add
// ---------------------------------------------------------------------------

// emitterLang names the language each emitter directory writes. The FMA gate
// labels a hit by this, not by the language that happens to be attached to the
// needle: `Math.fma` is a legal spelling in both Java and JavaScript, so a hit
// in internal/codegen/js used to be reported as a Java bug.
var emitterLang = map[string]string{
	"rust": "Rust", "rusttable": "Rust",
	"java": "Java", "javatable": "Java",
	"csharp": "C#", "cstable": "C#",
	"js": "JavaScript", "jstable": "JavaScript",
	"dart": "Dart", "darttable": "Dart",
	"elixir": "Elixir", "elixirtable": "Elixir",
	"golang": "Go", "gotable": "Go",
}

// TestNoFusedMultiplyAddIntrinsics is the Rust, Java and C# half. Those three
// toolchains do not contract on their own: a fused product is reachable only
// through an explicit intrinsic. So the rule is simply that no emitter writes
// one into generated code.
func TestNoFusedMultiplyAddIntrinsics(t *testing.T) {
	root := repoRoot(t)
	// the forbidden spellings, across every language that has one
	forbidden := []string{"mul_add", "Math.fma", "FusedMultiplyAdd"}
	dirs := make([]string, 0, len(emitterLang))
	for d := range emitterLang {
		dirs = append(dirs, d)
	}
	sort.Strings(dirs)
	for _, d := range dirs {
		base := filepath.Join(root, "internal", "codegen", d)
		err := filepath.Walk(base, func(path string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			src := read(t, path)
			for _, needle := range forbidden {
				if strings.Contains(src, needle) {
					rel, _ := filepath.Rel(root, path)
					t.Errorf("%s: the %s emitter writes %q (a fused multiply-add) — a fused product writes a different quantize index (SPEC §7.2)", rel, emitterLang[d], needle)
				}
			}
			return nil
		})
		if err != nil {
			t.Fatalf("%s: %v", base, err)
		}
	}
}

// ---------------------------------------------------------------------------
// The source-level half, per leg, anchored on the quantize fold itself
// ---------------------------------------------------------------------------

// csharpLocal is the C# message codec's narrowing local, whitespace-tolerant
// because some of the five copies are emitted minified.
var csharpLocal = regexp.MustCompile(`float\s+scaled\s*=\s*normalized\s*\*`)

// foldSite is one leg's quantize fold: where it lives (file and the NAMED
// function that emits it), how to find the fold's anchor line inside that
// function, how many preceding lines belong to the same fold, and the wraps
// that must appear on it.
type foldSite struct {
	lang, file, fn string
	// anchor finds the fold's last statement inside fn's body
	anchor *regexp.Regexp
	// span is how many lines ABOVE the anchor still belong to the fold
	span int
	// wraps must all match within the anchor's span
	wraps []*regexp.Regexp
	// why names what is lost when a wrap goes
	why string
}

// TestNoContractionInEmittedCode is the source-level half, per leg. Each
// language forbids fusion by forcing an intermediate rounding, and the
// spelling differs per language; this asserts the spelling is still there ON
// THE QUANTIZE PRODUCT. The emitters are read as text rather than exercised,
// because the failure being guarded against is an editor "simplifying" the
// wrap away — which no runtime test on a non-fusing host would ever catch.
func TestNoContractionInEmittedCode(t *testing.T) {
	root := repoRoot(t)
	sites := []foldSite{
		{
			lang: "Go (packet form)", file: "internal/codegen/golang/functions.go", fn: "emitWriteCompressedFold",
			anchor: regexp.MustCompile(`\+ 0\.5`),
			wraps:  []*regexp.Regexp{regexp.MustCompile(`float32\(normalizedValue\s*\*\s*[^)]*\)\s*\+\s*0\.5`)},
			why:    "the float32() around the product is what stops Go fusing `x*y+z` on arm64",
		},
		{
			lang: "Go (flat form)", file: "internal/codegen/golang/flat.go", fn: "flatCompressedPiece",
			anchor: regexp.MustCompile(`\+ 0\.5`),
			wraps:  []*regexp.Regexp{regexp.MustCompile(`float32\(normalizedValue\s*\*\s*[^)]*\)\s*\+\s*0\.5`)},
			why:    "the float32() around the product is what stops Go fusing `x*y+z` on arm64",
		},
		{
			lang: "Go (table message form)", file: "internal/codegen/gotable/message.go", fn: "tableMessageWriteScalar",
			anchor: regexp.MustCompile(`scaled\s*\+\s*0\.5`),
			wraps: []*regexp.Regexp{
				regexp.MustCompile(`scaled\s*:?=\s*float32\(ratio\s*\*\s*float32\(s\.QCount\)\)`),
				regexp.MustCompile(`float32\(scaled\s*\+\s*0\.5\)`),
			},
			why: "this codec quantizes at RUNTIME in Go, so the two float32() conversions are the whole no-contraction guarantee for the message wire",
		},
		{
			lang: "Go (table message form, dequantize)", file: "internal/codegen/gotable/message.go", fn: "tableMessageReadScalar",
			anchor: regexp.MustCompile(`scaled\s*\+\s*s\.QMin`),
			wraps: []*regexp.Regexp{
				regexp.MustCompile(`scaled\s*:?=\s*float32\(ratio\s*\*\s*s\.QDelta\)`),
				regexp.MustCompile(`float32\(scaled\s*\+\s*s\.QMin\)`),
			},
			why: "the read side reconstructs with the same two roundings; fusing it returns a value the writer never wrote",
		},
		{
			lang: "JavaScript (flat form)", file: "internal/codegen/js/flat.go", fn: "emitWriteCompressedFloat",
			anchor: regexp.MustCompile(`\+ 0\.5`),
			wraps:  []*regexp.Regexp{regexp.MustCompile(`Math\.fround\(Math\.fround\(n\s*\*\s*[^)]*\)\s*\+\s*0\.5\)`)},
			why:    "JS has no float type: both frounds are the float32 rounding the wire is defined in",
		},
		{
			lang: "Dart", file: "internal/codegen/dart/functions.go", fn: "emitWriteCompressedFloat",
			anchor: regexp.MustCompile(`\+ 0\.5`),
			wraps:  []*regexp.Regexp{regexp.MustCompile(`_fround\(_fround\(n\s*\*\s*[^)]*\)\s*\+\s*0\.5\)`)},
			why:    "Dart doubles need the explicit _fround on BOTH the product and the sum",
		},
		{
			lang: "Elixir", file: "internal/codegen/elixir/functions.go", fn: "emitSupportHelpers",
			anchor: regexp.MustCompile(`floor\(fr\(scaled \+ 0\.5\)\)`),
			span:   2,
			wraps: []*regexp.Regexp{
				regexp.MustCompile(`scaled = fr\(normalized \* miv32\)`),
				regexp.MustCompile(`floor\(fr\(scaled \+ 0\.5\)\)`),
			},
			why: "BEAM floats are doubles; fr/1 on the product and on the sum is the float32 emulation",
		},
	}
	for _, s := range sites {
		body := funcBody(read(t, filepath.Join(root, s.file)), s.fn)
		if body == nil {
			t.Errorf("%s: %s no longer holds a function %s() — the quantize fold has moved and this gate has lost its site; point it at the new one rather than deleting it (SPEC §7.2)", s.lang, s.file, s.fn)
			continue
		}
		hits := 0
		for i, line := range body {
			if !s.anchor.MatchString(line) {
				continue
			}
			hits++
			lo := i - s.span
			if lo < 0 {
				lo = 0
			}
			fold := strings.Join(body[lo:i+1], "\n")
			for _, w := range s.wraps {
				if !w.MatchString(fold) {
					t.Errorf("%s: %s:%s() emits the quantize fold without %s — fusion is permitted again; %s (SPEC §7.2)\n\t%s",
						s.lang, s.file, s.fn, w, s.why, strings.TrimSpace(line))
				}
			}
		}
		if hits == 0 {
			t.Errorf("%s: %s:%s() no longer emits a quantize fold matching %s — the gate cannot find the expression it guards, so it is proving nothing (SPEC §7.2)", s.lang, s.file, s.fn, s.anchor)
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
