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
// compilable Go: `generate --lang go` on a unit holding it as a field and as a
// declaration name exits 0, and the emitted package builds. This pins the Go
// reserved-word list complete: if a name the Go emitter cannot carry were
// missing from it, generate would exit 0 (it is not refused) and the emitted
// package would fail to compile. The serialize.go runtime is a stub here, in
// the job (its real sibling is not staged); the emitted code uses only the
// stream types and SerializeBits64/Err, which the stub provides.
func TestGenerateGoCompilesNonGoReservedWords(t *testing.T) {
	t.Parallel()
	bin := buildCLI(t)
	work := t.TempDir()

	goWords := map[string]bool{}
	for _, w := range strings.Fields(`break byte case chan const continue default defer else fallthrough false for func go goto if import interface map package range ref return select struct switch true type unsafe var`) {
		goWords[w] = true
	}
	var words []string
	seen := map[string]bool{}
	addAll := func(all string) {
		for _, w := range strings.Fields(all) {
			if !goWords[w] && !seen[w] {
				seen[w] = true
				words = append(words, w)
			}
		}
	}
	addAll(`alignas alignof and and_eq asm auto bitand bitor bool break case catch char
		char8_t char16_t char32_t class compl concept const consteval constexpr constinit
		const_cast continue co_await co_return co_yield decltype default delete do double
		dynamic_cast else enum explicit export extern false float for friend goto if inline
		int long mutable namespace new noexcept not not_eq nullptr operator or or_eq out private
		protected public register reinterpret_cast requires return short signed sizeof static
		static_assert static_cast struct switch template this thread_local throw true try
		typedef typeid typename union unsigned using virtual void volatile wchar_t while xor xor_eq`)
	addAll(`abstract as base bool break byte case catch char checked class const continue
		decimal default delegate do double else enum event explicit extern false finally fixed
		float for foreach goto if implicit in int interface internal is lock long namespace
		new null object operator out override params private protected public readonly ref
		return sbyte sealed short sizeof stackalloc static string struct switch this throw
		true try typeof uint ulong unchecked unsafe ushort using virtual void volatile while`)
	addAll(`abstract as async await become box break char const continue crate do dyn
		else enum extern false final fn for if impl in let loop macro match mod move mut
		override priv pub ref return self Self static struct super trait true try type typeof
		unsafe unsized use virtual where while yield`)

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

	var mu sync.Mutex
	var bad []string
	var wg sync.WaitGroup
	for i, w := range words {
		w, i := w, i
		wg.Add(1)
		go func() {
			defer wg.Done()
			fail := func(format string, args ...any) {
				mu.Lock()
				bad = append(bad, fmt.Sprintf("word %q: "+format, append([]any{w}, args...)...))
				mu.Unlock()
			}
			out := filepath.Join(work, "out", fmt.Sprintf("w%03d", i), w)
			dir := filepath.Join(work, "units", fmt.Sprintf("w%03d", i))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				fail("%v", err)
				return
			}
			src := fmt.Sprintf("package t\ntype %s {\n  a int32\n}\ntype F {\n  %s int32\n}\n", w, w)
			path := filepath.Join(dir, "W.schema")
			if err := os.WriteFile(path, []byte(src), 0o644); err != nil {
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
	wg.Wait()
	if len(bad) > 0 {
		for _, b := range bad {
			t.Error(b)
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
