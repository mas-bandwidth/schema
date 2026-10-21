// The CLI's §4.6 target-name safety, per generated target: generate --lang go
// on a unit holding only `self` (a Rust reserved word) succeeds, and schema
// check — which names no target — refuses it.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestGenerateChecksOnlyItsTargetsWords(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()
	dir := filepath.Join(work, "unit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Self.schema")
	src := "package t\ntype T { self int32 }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	// generate --lang go succeeds: `self` is reserved in Rust, not Go.
	run(t, bin, "generate", "--lang", "go", "--out", filepath.Join(work, "out"), dir)
	// schema check names no target, so it checks every target's words and
	// refuses the unit.
	out, err := exec.Command(bin, "check", dir).CombinedOutput()
	if err == nil {
		t.Fatalf("schema check on a unit holding only `self` must exit 1\n%s", out)
	}
}

// schemaKeywords is the schema grammar's own keyword set (internal/scanner): a
// word in it is refused by the parser before any target reserved-word check, so
// it cannot be exercised as an identifier in a generated name position.
var schemaKeywords = map[string]bool{
	"package": true, "const": true, "enum": true, "type": true,
	"table": true, "map": true, "message": true, "object": true,
	"if": true, "else": true, "switch": true, "case": true,
	"align": true, "reserved": true, "bits": true, "bool": true,
	"float32": true, "float64": true, "string": true, "wstring": true,
	"bytes": true, "fixed": true, "ufixed": true,
	"int8": true, "int16": true, "int32": true, "int64": true,
	"uint8": true, "uint16": true, "uint32": true, "uint64": true,
	"int128": true, "uint128": true, "int": true, "uint": true,
}

// goReservedWords is the authoritative set of Go reserved words (the Go spec's
// keyword list) that the checker must refuse for --lang go. The test holds it
// by hand — the Go keyword set is fixed — and asserts the checker's own table
// reports each of them as Go-reserved, so a mutation that drops a word from
// check.go's Go list fails here: the word stops being reported as Go-reserved,
// and this test notices.
var goReservedWords = strings.Fields(`break byte case chan const continue default defer else fallthrough false for func go goto if import interface map package range ref return select struct switch true type unsafe var`)

// TestGenerateGoRefusesGoReservedWords pins the Go half of §4.6: every Go
// reserved word must be refused by generate --lang go (SPEC §4.6).
// Schema-keyword words (const, type, map, ...) are refused by the grammar
// before the reserved-word check, so they cannot be exercised as identifiers;
// they still must be reported as Go-reserved by check.ReservedWords().
func TestGenerateGoRefusesGoReservedWords(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)

	goReserved := map[string]bool{}
	for w, langs := range check.ReservedWords() {
		if slices.Contains(langs, "Go") {
			goReserved[w] = true
		}
	}

	for _, w := range goReservedWords {
		if !goReserved[w] {
			t.Errorf("check.ReservedWords() does not report Go-reserved word %q", w)
			continue
		}
		if schemaKeywords[w] {
			continue
		}
		w := w
		t.Run(w, func(t *testing.T) {
			t.Parallel()
			work := t.TempDir()
			dir := filepath.Join(work, "unit")
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(dir, "W.schema")
			src := fmt.Sprintf("package t\ntype %s {\n  a int32\n}\n", w)
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
				t.Fatal(err)
			}
			out, status := runFail(t, bin, "generate", "--lang", "go", "--out", filepath.Join(work, "out"), dir)
			if status != 1 || !strings.Contains(out, `"`+w+`" is a reserved word`) {
				t.Errorf("generate --lang go on Go-reserved word %q: exit %d, want 1 with a reserved-word refusal\n%s", w, status, out)
			}
		})
	}
}

// Every targetReserved word the Go checker does not refuse must yield
// compilable Go: `generate --lang go` on a unit holding it as a declaration
// name, a field, an enum, an enum variant, and the package name exits 0, and
// the emitted package builds. This pins the Go reserved-word list complete: if
// a name the Go emitter cannot carry were missing from it, generate would exit
// 0 (it is not refused) and the emitted package would fail to compile. The
// serialize.go runtime is a stub here, in the job (its real sibling is not
// staged); the emitted code uses only the stream types and
// SerializeBits64/Err, which the stub provides.
func TestGenerateGoCompilesNonGoReservedWords(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "one go build per non-Go reserved word")
	bin := buildCLI(t)
	work := t.TempDir()

	// The non-Go words under test come from the checker's own table
	// (check.ReservedWords) — not from copies here — so this pins
	// internal/check's targetReserved against the Go emitter's real behavior: a
	// non-Go word that is not refused must emit Go that compiles. Schema-keyword
	// words never reach Go emission (the grammar refuses them), so they are
	// left out.
	reserved := check.ReservedWords()
	var words []string
	for w, langs := range reserved {
		goReserves := slices.Contains(langs, "Go")
		if !goReserves && !schemaKeywords[w] {
			words = append(words, w)
		}
	}
	sort.Strings(words)

	// The checkout, for the shared Go runtime a table unit imports
	// (tables/goruntime: shared is the default, docs/SPEC-TABLES.md §8.4).
	checkout, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	// A stub for the serialize.go runtime the generated Go imports.
	runtime := filepath.Join(work, "serialize")
	if err := os.MkdirAll(runtime, 0o755); err != nil {
		t.Fatal(err)
	}
	stub := `package serialize

type WriteStream struct{ err error }

func (s *WriteStream) SerializeBits(v *uint32, n int) {}
func (s *WriteStream) SerializeBits64(v *uint64, n int) {}
func (s *WriteStream) Err() error { return s.err }

type ReadStream struct{ err error }

func (s *ReadStream) SerializeBits(v *uint32, n int) {}
func (s *ReadStream) SerializeBits64(v *uint64, n int) {}
func (s *ReadStream) Err() error { return s.err }
`
	if err := os.WriteFile(filepath.Join(runtime, "serialize.go"), []byte(stub), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(runtime, "go.mod"), []byte("module github.com/mas-bandwidth/serialize.go\n\ngo 1.26\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Each form puts the word where a generated Go identifier lands: a type
	// declaration and field, a table declaration with a variant, an enum, and
	// the package name.
	forms := []struct {
		name string
		mk   func(w string) string
	}{
		{"type", func(w string) string {
			return fmt.Sprintf("package t\ntype %s {\n  a int32\n}\ntype F {\n  %s int32\n}\n", w, w)
		}},
		{"table", func(w string) string {
			return fmt.Sprintf("package t\ntable %s {\n  a int32\n}\nenum E { %s }\n", w, w)
		}},
		{"enum", func(w string) string { return fmt.Sprintf("package t\nenum %s { a }\n", w) }},
		{"package", func(w string) string { return fmt.Sprintf("package %s\ntype T { a int32 }\n", w) }},
	}

	// Bound concurrency so the many go builds do not spawn an unbounded
	// process tree (the sandbox's 256-process runaway).
	const maxParallel = 3
	sem := make(chan struct{}, maxParallel)
	var mu sync.Mutex
	var bad []string
	var wg sync.WaitGroup
	for _, w := range words {
		for _, f := range forms {
			w, f := w, f
			wg.Go(func() {
				sem <- struct{}{}
				defer func() { <-sem }()
				fail := func(format string, args ...any) {
					mu.Lock()
					bad = append(bad, fmt.Sprintf("word %q form %s: "+format, append([]any{w, f.name}, args...)...))
					mu.Unlock()
				}
				out := filepath.Join(work, "out", w, f.name)
				dir := filepath.Join(work, "units", w, f.name)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					fail("%v", err)
					return
				}
				path := filepath.Join(dir, "W.schema")
				if err := os.WriteFile(path, []byte(f.mk(w)), 0o644); err != nil {
					fail("%v", err)
					return
				}
				if outBytes, err := exec.Command(bin, "generate", "--lang", "go", "--out", out, dir).CombinedOutput(); err != nil {
					// Any generate failure is a defect: a non-Go word must not be
					// refused for --lang go. (Schema-keyword words were filtered
					// out above, so no grammar refusal should appear.)
					fail("generate failed for non-Go word %q: %s", w, outBytes)
					return
				}
				mod := fmt.Sprintf("module t\n\ngo 1.26\n\nrequire (\n\tgithub.com/mas-bandwidth/schema/v2 v2.0.0\n\tgithub.com/mas-bandwidth/serialize.go v0.0.0\n)\n\nreplace (\n\tgithub.com/mas-bandwidth/schema/v2 => %s\n\tgithub.com/mas-bandwidth/serialize.go => %s\n)\n", checkout, runtime)
				if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o644); err != nil {
					fail("%v", err)
					return
				}
				gb := exec.Command("go", "build", "./...")
				gb.Dir = out
				if _, err := gb.Output(); err != nil {
					fail("go build: %v", err)
				}
			})
		}
	}
	wg.Wait()
	if len(bad) > 0 {
		for _, b := range bad {
			t.Error(b)
		}
	}
}

// select, ulong and ushort are not C++ keywords, but the generated C++
// declares `namespace <name>` beside sys/select.h or sys/types.h, whose own
// declarations of the same name collide with it — so generate --lang cpp
// refuses them as package names (SPEC §4.6), while a non-C++ target does not.
func TestGenerateCppRefusesHeaderCollidingPackageNames(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	for _, w := range []string{"select", "ulong", "ushort"} {
		work := t.TempDir()
		dir := filepath.Join(work, "unit")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(dir, "W.schema")
		src := fmt.Sprintf("package %s\ntable W { a int32 }\n", w)
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		out, status := runFail(t, bin, "generate", "--lang", "cpp", "--out", filepath.Join(work, "out"), dir)
		if status != 1 {
			t.Errorf("generate --lang cpp on package %q exited %d, want 1\n%s", w, status, out)
		}
	}
}

// A target with no reserved-word list of its own has no words to narrow to, so
// generate on it checks every target's words rather than none: `class` is in
// the C++ and C# lists, and `generate --lang java` on a field named class still
// exits 1 rather than emitting Java that spells a method `class`.
func TestGenerateWithNoReservedListChecksEveryTarget(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()
	dir := filepath.Join(work, "unit")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Class.schema")
	src := "package t\ntable W { class int32 }\n"
	if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	for _, lang := range []string{"java", "c", "js", "elixir"} {
		out, status := runFail(t, bin, "generate", "--lang", lang, "--out", filepath.Join(work, "out-"+lang), dir)
		if status != 1 {
			t.Errorf("generate --lang %s exited %d, want 1\n%s", lang, status, out)
		}
		if !strings.Contains(out, `"class" is a reserved word`) {
			t.Errorf("generate --lang %s should refuse class as a reserved word, got %q", lang, out)
		}
	}
}
