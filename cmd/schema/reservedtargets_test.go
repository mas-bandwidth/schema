// The CLI's §4.6 target-name safety, per generated target: generate --lang go
// on a unit holding only `self` (a Rust reserved word) succeeds, and schema
// check — which names no target — refuses it.
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
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

// Every word the §4.6 reserved-word tables carry that Go does not reserve must
// yield compilable Go: `generate --lang go` on a unit holding it as a type, a
// table, an enum, an enum variant and the package name exits 0, and the emitted
// packages build. The words come from targetReserved via check.ReservedWords,
// so this pins the checker's tables against the Go emitter: if a name the Go
// emitter cannot carry were missing from them, generate would exit 0 (it is not
// refused) and the emitted package would fail to compile. The serialize.go
// runtime is a stub here, in the job (its real sibling is not staged); the
// emitted code uses only the stream types and SerializeBits64/Err, which the
// stub provides.
func TestGenerateGoCompilesNonGoReservedWords(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()

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

	var words []string
	for w, langs := range check.ReservedWords() {
		goWord := false
		for _, l := range langs {
			if l == "Go" {
				goWord = true
			}
		}
		if !goWord {
			words = append(words, w)
		}
	}

	sem := make(chan struct{}, 4)
	var mu sync.Mutex
	var bad []string
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
			forms := []struct{ name, src string }{
				{"type", fmt.Sprintf("package t\ntype %s {\n  a int32\n}\n", w)},
				{"table", fmt.Sprintf("package t\ntable %s {\n  a int32\n}\n", w)},
				{"enum", fmt.Sprintf("package t\tenum %s {\n  A\n  B\n}\n", w)},
				{"variant", fmt.Sprintf("package t\tenum E {\n  %s\n  OtherB\n}\n", w)},
				{"package", fmt.Sprintf("package %s\ntype T { a int32 }\n", w)},
			}
			base := filepath.Join(work, "units", fmt.Sprintf("w%03d", i))
			outRoot := filepath.Join(base, "out")
			for _, fm := range forms {
				dir := filepath.Join(base, "unit-"+fm.name)
				if err := os.MkdirAll(dir, 0o755); err != nil {
					fail("%v", err)
					return
				}
				if err := os.WriteFile(filepath.Join(dir, "W.schema"), []byte(fm.src), 0o644); err != nil {
					fail("%v", err)
					return
				}
				out := filepath.Join(outRoot, fm.name)
				outBytes, err := exec.Command(bin, "generate", "--lang", "go", "--out", out, dir).CombinedOutput()
				if err != nil {
					// A word the schema language itself refuses (int, uint, bool,
					// object, fixed, ...) never reaches Go emission; it is not the
					// defect under test. A reserved-word refusal of a non-Go word
					// is.
					if strings.Contains(string(outBytes), "reserved word") {
						fail("generate refused as %s: %s", fm.name, outBytes)
					}
					return
				}
			}
			mod := fmt.Sprintf("module t\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\nreplace github.com/mas-bandwidth/serialize.go => %s\n", runtime)
			if err := os.WriteFile(filepath.Join(outRoot, "go.mod"), []byte(mod), 0o644); err != nil {
				fail("%v", err)
				return
			}
			gb := exec.Command("go", "build", "./...")
			gb.Dir = outRoot
			if _, err := gb.Output(); err != nil {
				fail("go build: %v", err)
			}
		}()
	}
	wg.Wait()
	for _, b := range bad {
		t.Error(b)
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

// The C++ package-name check refuses a package whose name collides with a libc
// identifier visible at C++ namespace scope beside sys/select.h and sys/types.h
// (select, ulong, ushort): generate --lang cpp on each as a package name must
// exit 1 rather than emit `namespace select/ulong/ushort` that g++ refuses.
func TestGenerateCppRefusesLibcPackageNames(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	for _, w := range []string{"select", "ulong", "ushort"} {
		w := w
		work := t.TempDir()
		dir := filepath.Join(work, "unit")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		src := fmt.Sprintf("package %s\ntype T { a int32 }\n", w)
		if err := os.WriteFile(filepath.Join(dir, "P.schema"), []byte(src), 0o644); err != nil {
			t.Fatal(err)
		}
		out, status := runFail(t, bin, "generate", "--lang", "cpp", "--out", filepath.Join(work, "out"), dir)
		if status != 1 {
			t.Errorf("generate --lang cpp on package %q exited %d, want 1\n%s", w, status, out)
		}
	}
}
