// Property tests over the compiler's structural guarantees — the promises
// that hold for EVERY input, not just the corpus. Crash-fuzzing (fuzz_test.go)
// asks "does it survive?"; these ask "does it keep its word?", which is the
// harder and more valuable question for a compiler whose entire value
// proposition is that five languages agree on the byte.
package fuzz_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/format"
	"github.com/mas-bandwidth/schema/internal/parser"
)

func corpusUnits(t *testing.T) map[string][]check.SourceFile {
	t.Helper()
	out := map[string][]check.SourceFile{}
	for _, dir := range []string{"../../examples", "../../examples128"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		var files []check.SourceFile
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".schema" {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				continue
			}
			ast, perrs := parser.Parse(e.Name(), b)
			if len(perrs) > 0 {
				t.Fatalf("%s/%s does not parse: %v", dir, e.Name(), perrs[0])
			}
			files = append(files, check.SourceFile{
				Path: e.Name(), Name: e.Name(),
				Base: strings.TrimSuffix(e.Name(), ".schema"), Bytes: b, AST: ast,
			})
		}
		if len(files) > 0 {
			out[dir] = files
		}
	}
	if len(out) == 0 {
		t.Fatal("no corpus units found")
	}
	return out
}

// Generated output must be byte-identical across repeated runs of the same
// input. Go randomizes map iteration on every run, so ANY map walk that
// reaches emission ordering shows up here — and for a compiler whose wire is
// byte-pinned across five languages and whose goldens are diffed in CI,
// nondeterministic output is a correctness bug, not a cosmetic one. Repeats
// are cheap; the randomization is per-process, so a real defect surfaces with
// high probability across this many passes.
func TestGeneratedOutputIsDeterministic(t *testing.T) {
	const passes = 12
	for dir, files := range corpusUnits(t) {
		unit, cerrs := check.Unit(files)
		if len(cerrs) > 0 {
			t.Fatalf("%s does not check: %v", dir, cerrs[0])
		}
		type gen struct {
			name string
			run  func() (map[string][]byte, error)
		}
		// ALL FIVE backends, from the shared backends table (fuzz_test.go):
		// this list said four while the file's own comment said five — the
		// ea88362 sweep renumbered the prose and missed the slice, and the C
		// backend (whose sortedDeps exists precisely because C output must be
		// byte-identical) shipped unheld to the invariant it was built for.
		// Deriving from the table instead of restating it means backend six
		// cannot repeat the miss.
		gens := make([]gen, 0, len(backends))
		for _, b := range backends {
			gens = append(gens, gen{b.name, func() (map[string][]byte, error) { return b.generate(unit) }})
		}
		for _, g := range gens {
			first, err := g.run()
			if err != nil {
				t.Fatalf("%s/%s: %v", dir, g.name, err)
			}
			for i := 1; i < passes; i++ {
				again, err := g.run()
				if err != nil {
					t.Fatalf("%s/%s pass %d: %v", dir, g.name, i, err)
				}
				if len(again) != len(first) {
					t.Fatalf("%s/%s pass %d: file COUNT changed %d -> %d",
						dir, g.name, i, len(first), len(again))
				}
				for name, want := range first {
					got, ok := again[name]
					if !ok {
						t.Fatalf("%s/%s pass %d: file %s vanished", dir, g.name, i, name)
					}
					if !bytes.Equal(want, got) {
						t.Fatalf("%s/%s pass %d: %s DIFFERS between runs — generated output "+
							"must be deterministic (a map walk reaching emission order?)",
							dir, g.name, i, name)
					}
				}
			}
		}
	}
}

// The protocol id is a hash of the source and rides on every wire: two peers
// that disagree about it cannot talk. It must be stable across runs for
// identical input — anything else silently breaks compatibility between two
// builds of the SAME schema.
func TestProtocolIdIsStable(t *testing.T) {
	for dir, files := range corpusUnits(t) {
		unit, cerrs := check.Unit(files)
		if len(cerrs) > 0 {
			t.Fatalf("%s does not check: %v", dir, cerrs[0])
		}
		want := unit.ProtocolId
		for i := 0; i < 8; i++ {
			again, cerrs := check.Unit(files)
			if len(cerrs) > 0 {
				t.Fatalf("%s pass %d does not check: %v", dir, i, cerrs[0])
			}
			if again.ProtocolId != want {
				t.Fatalf("%s: protocol id UNSTABLE across runs: %#x then %#x — "+
					"two builds of one schema would refuse to talk",
					dir, want, again.ProtocolId)
			}
		}
	}
}

// Formatting must be idempotent: fmt(fmt(x)) == fmt(x). A formatter that
// keeps changing its own output makes "run fmt" an unstable operation and
// turns every commit into a diff fight. Checked over the whole corpus.
func TestFormatIsIdempotent(t *testing.T) {
	for _, dir := range []string{"../../examples", "../../examples128"} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || filepath.Ext(e.Name()) != ".schema" {
				continue
			}
			path := filepath.Join(dir, e.Name())
			src, err := os.ReadFile(path)
			if err != nil {
				continue
			}
			once, err := format.Format(path, src)
			if err != nil {
				t.Fatalf("%s: format failed: %v", path, err)
			}
			twice, err := format.Format(path, once)
			if err != nil {
				t.Fatalf("%s: reformat failed: %v", path, err)
			}
			if !bytes.Equal(once, twice) {
				t.Errorf("%s: formatting is NOT idempotent — fmt(fmt(x)) != fmt(x)", path)
			}
		}
	}
}

// FuzzFormatIdempotent pushes the same property over arbitrary input: any
// source the formatter accepts must be a fixed point after one pass — and it
// must mean the same thing. Idempotence alone is a trap: a formatter that
// silently DROPPED an attribute (the exact class of the 9468155 crash, a
// `[min = 0]` on a wrapped attribute list) is perfectly idempotent while
// changing the wire of every field it touched. The protocol id is the low 64
// bits of SHA-256 over the wire-shape projection, so "same id" is precisely
// "same wire": for any source that checks, format() must preserve it.
func FuzzFormatIdempotent(f *testing.F) {
	seedFromCorpus(f, func(s string) { f.Add(s) })
	for _, s := range handSeeds {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, src string) {
		once, err := format.Format("A.schema", []byte(src))
		if err != nil {
			return // rejecting malformed input is correct
		}
		twice, err := format.Format("A.schema", once)
		if err != nil {
			t.Fatalf("formatter accepted its own output's input but rejected the output: %v", err)
		}
		if !bytes.Equal(once, twice) {
			t.Fatalf("formatting not idempotent:\n--- once ---\n%s\n--- twice ---\n%s", once, twice)
		}
		before := unitOf(map[string]string{"A.schema": src})
		if before == nil {
			return // formats but does not check: idempotence was all there was to hold
		}
		after := unitOf(map[string]string{"A.schema": string(once)})
		if after == nil {
			t.Fatalf("source checks but its FORMATTED form does not — the formatter broke the program:\n--- source ---\n%s\n--- formatted ---\n%s",
				src, once)
		}
		if after.ProtocolId != before.ProtocolId {
			t.Fatalf("format() changed the protocol id %#x -> %#x — the formatter changed the WIRE:\n--- source ---\n%s\n--- formatted ---\n%s",
				before.ProtocolId, after.ProtocolId, src, once)
		}
	})
}

// Diagnostics must come out in the SAME ORDER every run. Go randomizes map
// iteration per process, so any check that walks a map to report errors will
// shuffle its output — one input printing its errors three different ways.
// The exit code and the error SET survive that, which is why it hid for so
// long, but it defeats golden-diagnostic comparison and makes CI logs
// irreproducible. checkTables was the one that did this.
func TestDiagnosticOrderIsDeterministic(t *testing.T) {
	// every component is a table-wire-illegal kind, so each names its own
	// error and the ordering is observable
	const src = `package t

type Aaa { x fixed(16, 16) [min = -100, max = 100] }
type Bbb { y int128 [min = -10, max = 10] }
type Ccc { z bits(96) }

table Root {
    a Aaa
    b Bbb
    c Ccc
}
`
	var want []string
	for pass := 0; pass < 16; pass++ {
		ast, perrs := parser.Parse("T.schema", []byte(src))
		if len(perrs) > 0 {
			t.Fatalf("corpus does not parse: %v", perrs[0])
		}
		_, cerrs := check.Unit([]check.SourceFile{{
			Path: "T.schema", Name: "T.schema", Base: "T",
			Bytes: []byte(src), AST: ast,
		}})
		if len(cerrs) == 0 {
			t.Fatal("expected diagnostics from a table closure over illegal kinds")
		}
		got := make([]string, len(cerrs))
		for i, e := range cerrs {
			got[i] = e.Error()
		}
		if pass == 0 {
			want = got
			continue
		}
		if len(got) != len(want) {
			t.Fatalf("pass %d: diagnostic COUNT changed %d -> %d", pass, len(want), len(got))
		}
		for i := range got {
			if got[i] != want[i] {
				t.Fatalf("pass %d: diagnostic ORDER is nondeterministic at index %d\n  first run: %s\n  this run:  %s",
					pass, i, want[i], got[i])
			}
		}
	}
}
