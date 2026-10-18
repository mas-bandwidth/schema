// The CLI's §4.6 target-name safety, per generated target: generate --lang go
// on a unit holding only `self` (a Rust reserved word) succeeds, and schema
// check — which names no target — refuses it.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
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
	bin := buildCLI(t)
	work := t.TempDir()

	goWords := map[string]bool{}
	for _, w := range strings.Fields(`break byte case chan const continue default defer else fallthrough false for func go goto if import interface map package range ref return select struct switch true type unsafe var`) {
		goWords[w] = true
	}
	// The words under test come from the checker's own table (check.ReservedWords)
	// — not from copies here — so this pins internal/check's targetReserved
	// against the Go emitter's real behavior. A word the Go emitter cannot
	// carry as an identifier must be refused for --lang go; a non-Go word that
	// is not refused must emit Go that compiles.
	var words []string
	for w := range check.ReservedWords() {
		if !goWords[w] {
			words = append(words, w)
		}
	}
	sort.Strings(words)

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
		{"type", func(w string) string { return fmt.Sprintf("package t\ntype %s {\n  a int32\n}\ntype F {\n  %s int32\n}\n", w, w) }},
		{"table", func(w string) string { return fmt.Sprintf("package t\ntable %s {\n  a int32\n}\nenum E { %s }\n", w, w) }},
		{"enum", func(w string) string { return fmt.Sprintf("package t\tenum %s { a }\n", w) }},
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
			wg.Add(1)
			go func() {
				defer wg.Done()
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
					// A word the schema language itself refuses (int, uint, bool,
					// object, fixed, ...) never reaches Go emission; it is not the
					// defect under test. A reserved-word refusal of a non-Go word
					// is.
					if strings.Contains(string(outBytes), "reserved word") {
						fail("generate refused %q: %s", w, outBytes)
					}
					return
				}
				mod := fmt.Sprintf("module t\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %s\n", runtime)
				if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o644); err != nil {
					fail("%v", err)
					return
				}
				gb := exec.Command("go", "build", "./...")
				gb.Dir = out
				if _, err := gb.Output(); err != nil {
					fail("go build: %v", err)
				}
			}()
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
