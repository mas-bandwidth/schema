// Package goldens pins the compiler's output (SPEC §7.2 gates 1 and 2): the
// corpus's generated source byte-for-byte, the protocol id exactly, and the
// corpus's formatter-canonical form. A change to
// any of these is loud by construction; a WIRE-affecting change under an
// unchanged schema is a stop-the-line event, never a quiet re-pin (SPEC §3.1).
//
// Regenerate deliberately with: make update-goldens
package goldens

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var update = flag.Bool("update", false, "rewrite the golden files from current output")

const (
	corpusDir = "../../examples"
	// the fixed-point + 128-bit unit: all nine targets, pinned like the main
	// corpus (the serialize ports all carry the phase-1 surface).
	corpus128Dir = "../../examples128"
	goldenDir    = "../../testdata/golden"
)

// schema is the driver every test here runs on — the public API, with the
// library's load policy: this harness measures the corpus, so it must never
// repair it (TestCorpusIsCanonical is the gate that would be repairing
// itself).
var schema = compiler.New()

func loadCorpus(t *testing.T) *ir.Unit { return loadCorpusDir(t, corpusDir) }

func loadCorpusDir(t *testing.T, dir string) *ir.Unit {
	t.Helper()
	paths, err := compiler.GatherPaths([]string{dir})
	if err != nil {
		t.Fatal(err)
	}
	u, err := schema.Load(paths)
	if err != nil {
		t.Fatalf("corpus does not compile: %v", err)
	}
	return u
}

// generate emits one target through the same registration door an external
// generator would come through.
func generate(t *testing.T, u *ir.Unit, target string, opts compiler.Options) map[string][]byte {
	t.Helper()
	files, err := schema.Generate(u, target, opts)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// TestCorpusIsCanonical is the fmt-drift gate: every corpus file must already
// be in schemafmt's one style (the compiler formats before processing, so a
// non-canonical file in git means someone bypassed the tool).
func TestCorpusIsCanonical(t *testing.T) {
	for _, dir := range []string{corpusDir, corpus128Dir} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatal(err)
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
				continue
			}
			p := filepath.Join(dir, e.Name())
			data, err := os.ReadFile(p)
			if err != nil {
				t.Fatal(err)
			}
			out, err := compiler.Format(p, data)
			if err != nil {
				t.Fatalf("%s: %v", p, err)
			}
			if string(out) != string(data) {
				t.Errorf("%s is not formatter-canonical — run: make fmt", p)
			}
		}
	}
}

// TestGoldenId pins the protocol id (SPEC §7.2 gate 2) — the tripwire on the
// §3.1 hash procedure: any change to how the id is computed breaks this
// loudly.
func TestGoldenId(t *testing.T) {
	u := loadCorpus(t)
	got := fmt.Sprintf("0x%016x\n", u.ProtocolId)
	path := filepath.Join(goldenDir, "id.txt")
	if *update {
		if err := os.MkdirAll(goldenDir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden id (run: make update-goldens): %v", err)
	}
	if got != string(want) {
		t.Errorf("protocol id moved: got %s want %s — if the schema files changed this is expected once (re-pin deliberately); if they did not, the §3.1 hash procedure changed and that is stop-the-line", got, string(want))
	}
}

// TestGoldenSource pins the generated C++ byte-for-byte (SPEC §7.2 gate 1).
func TestGoldenSource(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "cpp", nil)
	pinDir(t, filepath.Join(goldenDir, "cpp"), files)
}

// TestGoldenSourceGo pins the generated Go byte-for-byte (SPEC §7.2 gate 1,
// second target).
func TestGoldenSourceGo(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "go", nil)
	pinDir(t, filepath.Join(goldenDir, "go"), files)
}

// TestGoldenSourceRust pins the generated Rust byte-for-byte (SPEC §7.2
// gate 1, third target).
func TestGoldenSourceRust(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "rust", nil)
	pinDir(t, filepath.Join(goldenDir, "rust"), files)
}

// TestGoldenSourceC pins the generated C byte-for-byte (SPEC §7.2 gate 1,
// fifth target).
func TestGoldenSourceC(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "c", nil)
	pinDir(t, filepath.Join(goldenDir, "c"), files)
}

// TestGoldenSourceCs pins the generated C# byte-for-byte (SPEC §7.2 gate 1,
// fourth target).
func TestGoldenSourceCs(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "cs", nil)
	pinDir(t, filepath.Join(goldenDir, "cs"), files)
}

// TestGoldenSourceJs pins the generated JavaScript byte-for-byte (SPEC §7.2
// gate 1, sixth target).
func TestGoldenSourceJs(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "js", nil)
	pinDir(t, filepath.Join(goldenDir, "js"), files)
}

// TestGoldenSourceDart pins the generated Dart byte-for-byte (SPEC §7.2
// gate 1, seventh target).
func TestGoldenSourceDart(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "dart", nil)
	pinDir(t, filepath.Join(goldenDir, "dart"), files)
}

// TestGoldenSourceJava pins the generated Java byte-for-byte (SPEC §7.2
// gate 1, eighth target).
func TestGoldenSourceJava(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "java", nil)
	pinDir(t, filepath.Join(goldenDir, "java"), files)
}

// TestGoldenSourceElixir pins the generated Elixir byte-for-byte (SPEC §7.2
// gate 1, ninth target).
func TestGoldenSourceElixir(t *testing.T) {
	u := loadCorpus(t)
	files := generate(t, u, "elixir", nil)
	pinDir(t, filepath.Join(goldenDir, "elixir"), files)
}

// TestGoldenLudicrousId pins the fixed-point + 128-bit unit's protocol id.
func TestGoldenLudicrousId(t *testing.T) {
	u := loadCorpusDir(t, corpus128Dir)
	got := fmt.Sprintf("0x%016x\n", u.ProtocolId)
	path := filepath.Join(goldenDir, "ludicrous", "id.txt")
	if *update {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("missing golden id (run: make update-goldens): %v", err)
	}
	if got != string(want) {
		t.Errorf("ludicrous protocol id moved: got %s want %s — if the schema files changed this is expected once (re-pin deliberately); if they did not, the §3.1 hash procedure changed and that is stop-the-line", got, string(want))
	}
}

// TestGoldenLudicrousSource pins the fixed-point + 128-bit unit's generated
// source byte-for-byte for ALL NINE targets. Every serialize port carries the
// phase-1 surface — a backend erroring here is a loud failure — and the unit
// rides the same cross-language wire gates as the main corpus
// (test/{c,go,rust,cs}-ludicrous).
func TestGoldenLudicrousSource(t *testing.T) {
	u := loadCorpusDir(t, corpus128Dir)
	for _, target := range []string{"cpp", "go", "rust", "cs", "c", "js", "dart", "java", "elixir"} {
		pinDir(t, filepath.Join(goldenDir, "ludicrous", target), generate(t, u, target, nil))
	}
}

// pinDir compares (or, under -update, rewrites) one directory of goldens.
func pinDir(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	if *update {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		path := filepath.Join(dir, n)
		if *update {
			if err := os.WriteFile(path, files[n], 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("missing golden %s (run: make update-goldens): %v", path, err)
		}
		if string(files[n]) != string(want) {
			t.Errorf("%s: generated source diverged from its golden — deliberate emitter change? re-pin with make update-goldens; if the WIRE moved under an unchanged schema, that is stop-the-line (SPEC §3.1)", path)
		}
	}
}

// TestGoldenBuildVersion pins the BUILD VERSION and the COOK PROJECTION it
// hashes, per unit (SPEC-TABLES.md §20.8). The number is what a distributed
// store's tuple is keyed by and what a block's prologue carries, so a change
// to how it is computed has to break every pinned value loudly — and the TEXT
// is pinned beside it, so a port reproduces the projection and not only the
// number.
//
// A unit that declares no table is pinned too: its projection is its three
// header lines alone, which is the case a reader has to be able to check by
// eye.
func TestGoldenBuildVersion(t *testing.T) {
	units := []struct {
		name string
		dir  string
	}{
		{"examples", corpusDir},
		{"examples128", corpus128Dir},
		{"tables-examples", "../../tables/examples"},
		{"tables-pointers", "../../tables/pointers"},
		{"tables-block", "../../tables/block"},
		{"tables-blockhome", "../../tables/blockhome"},
	}
	for _, unit := range units {
		t.Run(unit.name, func(t *testing.T) {
			u := loadCorpusDir(t, unit.dir)
			got := fmt.Sprintf("0x%016x\n%s", ir.BuildVersion(u), ir.CookProjection(u))
			path := filepath.Join(goldenDir, "build-version", unit.name+".txt")
			if *update {
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
					t.Fatal(err)
				}
				return
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("missing golden build version (run: make update-goldens): %v", err)
			}
			if got != string(want) {
				t.Errorf("the build version or its cook projection moved for %s — if the schema files changed this is expected once (re-pin deliberately); if they did not, §20.2's procedure changed and that is stop-the-line", unit.name)
			}
		})
	}
}
