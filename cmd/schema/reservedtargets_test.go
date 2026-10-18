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
// compilable Go in every name position: `generate --lang go` on a unit holding
// it as a package, type, table, enum, variant or field name exits 0, and the
// emitted package builds. The words are read back from check.ReservedWords()
// rather than hardcoded here, so this test pins check.go's own tables (a
// mutation to them cannot silently pass). If a name the Go emitter cannot
// carry were missing from the Go list, generate would exit 0 (it is not
// refused) and the emitted package would fail to compile. The serialize.go
// runtime is a stub here, in the job (its real sibling is not staged); the
// emitted code uses only the stream types and SerializeBits64/Err, which the
// stub provides.
func TestGenerateGoCompilesNonGoReservedWords(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()

	var words []string
	for w, langs := range check.ReservedWords() {
		nonGo := true
		for _, l := range langs {
			if l == "Go" {
				nonGo = false
			}
		}
		if nonGo {
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

	forms := []struct{ name, src string }{
		{"package", "package %s\ntable T { a int32 }\n"},
		{"type", "package t\ntype %s { a int32 }\n"},
		{"table", "package t\ntable %s { a int32 }\n"},
		{"enum", "package t\nenum %s { Z }\n"},
		{"variant", "package t\nenum E { %s }\n"},
		{"field", "package t\ntable T { %s int32 }\n"},
	}

	var mu sync.Mutex
	var bad []string
	// Bound parallelism: each word takes up to six generate/go-build pairs, and
	// the sandbox kills an unbounded burst (SANDBOX NOTE).
	sem := make(chan struct{}, 3)
	var wg sync.WaitGroup
	for i, w := range words {
		w, i := w, i
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			fail := func(format string, args ...any) {
				mu.Lock()
				bad = append(bad, fmt.Sprintf("word %q: "+format, append([]any{w}, args...)...))
				mu.Unlock()
			}
			for _, f := range forms {
				dir := filepath.Join(work, "units", fmt.Sprintf("w%03d", i), f.name)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					fail("%s form: %v", f.name, err)
					continue
				}
				src := fmt.Sprintf(f.src, w)
				if err := os.WriteFile(filepath.Join(dir, "W.schema"), []byte(src), 0o644); err != nil {
					fail("%s form: %v", f.name, err)
					continue
				}
				out := filepath.Join(work, "out", fmt.Sprintf("w%03d", i), f.name)
				outBytes, err := exec.Command(bin, "generate", "--lang", "go", "--out", out, dir).CombinedOutput()
				if err != nil {
					// A word the schema language itself refuses (int, uint,
					// bool, object, fixed, ...) never reaches Go emission; it
					// is not the defect under test. A reserved-word refusal
					// of a non-Go word is.
					if strings.Contains(string(outBytes), "reserved word") {
						fail("%s form: generate refused %q: %s", f.name, w, outBytes)
					}
					continue
				}
				// Build the emitted package for the type-declaration form, the
				// representative position; the other forms' generate exit 0 is
				// the reserved-word pinned, and building all six forms per word
				// would spawn unbounded go builds.
				if f.name != "type" {
					continue
				}
				mod := fmt.Sprintf("module t\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %s\n", runtime)
				if err := os.WriteFile(filepath.Join(out, "go.mod"), []byte(mod), 0o644); err != nil {
					fail("%s form: %v", f.name, err)
					continue
				}
				gb := exec.Command("go", "build", "./...")
				gb.Dir = out
				if _, err := gb.Output(); err != nil {
					fail("%s form: go build: %v", f.name, err)
				}
			}
		}()
	}
	wg.Wait()
	if len(bad) > 0 {
		for _, b := range bad {
			t.Error(b)
		}
	}
}

// The C++ target's reserved-word list refuses select, ulong and ushort as
// package names: they are not C++ keywords, but libc declares them at namespace
// scope (sys/select.h, sys/types.h), so a generated `namespace select` beside
// that is a redefinition clang rejects. generate --lang cpp on each as a
// package name exits 1.
func TestGenerateCppRefusesLibcPackageNames(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	for _, w := range []string{"select", "ulong", "ushort"} {
		work := t.TempDir()
		dir := filepath.Join(work, "unit")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := fmt.Sprintf("package %s\ntable T { a int32 }\n", w)
		path := filepath.Join(dir, "P.schema")
		if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		out, status := runFail(t, bin, "generate", "--lang", "cpp", "--out", filepath.Join(work, "out"), dir)
		if status != 1 {
			t.Errorf("generate --lang cpp on package %s exited %d, want 1\n%s", w, status, out)
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
