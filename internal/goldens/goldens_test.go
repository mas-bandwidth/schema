// Package goldens pins the compiler's output (SPEC §7.2 gates 1 and 2): the
// corpus's generated source byte-for-byte, the protocol id exactly, and the
// corpus's formatter-canonical form. A change to
// any of these is loud by construction; a WIRE-affecting change under an
// unchanged schema is a stop-the-line event, never a quiet re-pin (SPEC §3.1).
//
// Regenerate deliberately with: make update-goldens
package goldens

import (
	"crypto/sha256"
	"encoding/binary"
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

// The enum `Count` export moves NEITHER WIRE. Count is generated-code
// surface: it enters no wire-shape projection and no cook projection, so the
// packet wire's protocol id and the table wire's build version must both
// stand exactly where they stood before it existed. The numbers are written
// here as literals rather than read from testdata/golden — a re-pin of those
// files is precisely the mistake this refuses, and a gate that re-pins with
// them would say nothing. What legitimately moves them is a change to a
// PROJECTION itself — a wire-shape edit, or a cook form-version bump — which
// moves the corpus goldens in the same commit; a literal here that has to move
// alone is the defect this names.
func TestExportedSurfaceMovesNeitherWire(t *testing.T) {
	for _, unit := range []struct {
		name         string
		dir          string
		protocolId   uint64
		buildVersion uint64
	}{
		{"examples", corpusDir, 0x682e2a15a56b78bf, 0x68ee62213126f184},
		{"examples128", corpus128Dir, 0x3a9a972a02c9e7ca, 0x44a8123c94d09353},
	} {
		t.Run(unit.name, func(t *testing.T) {
			u := loadCorpusDir(t, unit.dir)
			if u.ProtocolId != unit.protocolId {
				t.Errorf("protocol id = 0x%016x, want 0x%016x — the packet wire moved under an unchanged corpus (SPEC §3.1); a generated-code export must never reach it", u.ProtocolId, unit.protocolId)
			}
			if got := ir.BuildVersion(u); got != unit.buildVersion {
				t.Errorf("build version = 0x%016x, want 0x%016x — the table wire moved under an unchanged corpus (docs/SPEC-TABLES.md §20); a generated-code export must never reach it", got, unit.buildVersion)
			}
		})
	}
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
// source byte-for-byte for EVERY registered target — the list is the
// compiler's own, so a tenth language pins its goldens without an edit here.
// Every serialize port carries the phase-1 surface — a backend erroring here
// is a loud failure — and the unit rides the same cross-language wire gates as
// the main corpus
// (test/{c,go,rust,cs}-ludicrous).
func TestGoldenLudicrousSource(t *testing.T) {
	u := loadCorpusDir(t, corpus128Dir)
	for _, target := range compiler.New().Targets() {
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

// pinnedUnits is every compilation unit this package pins, by the name its
// golden files are stored under.
var pinnedUnits = []struct {
	name string
	dir  string
}{
	{"examples", corpusDir},
	{"examples128", corpus128Dir},
	{"tables-examples", "../../tables/examples"},
	{"tables-pointers", "../../tables/pointers"},
	{"tables-block", "../../tables/block"},
	{"tables-blockhome", "../../tables/blockhome"},
	{"tables-messages", "../../tables/messages"},
	{"tables-stream", "../../tables/stream"},
	{"tables-blobs", "../../tables/blobs"},
	{"tables-scalars", "../../tables/scalars"},
}

// TestWireLawBumpMovesEveryId holds the promise the codec law line is for
// (SPEC §3.1): a compiler change that moves the BYTES under an unchanged
// rendering bumps ir.WireLaw, and EVERY id in existence moves with it. The
// constant cannot be changed from a test, so the equivalent is proven over the
// artifact the id is taken from: for every unit here the id is exactly the
// digest over its projection text, and the same text under the next law
// number digests differently. No unit can sit out a bump.
func TestWireLawBumpMovesEveryId(t *testing.T) {
	law := fmt.Sprintf("schema-wire-law %d\n", ir.WireLaw)
	next := fmt.Sprintf("schema-wire-law %d\n", ir.WireLaw+1)
	for _, unit := range pinnedUnits {
		t.Run(unit.name, func(t *testing.T) {
			u := loadCorpusDir(t, unit.dir)
			text := ir.WireProjection(u)
			if !strings.Contains(text, law) {
				t.Fatalf("%s does not carry the codec law line — a rounding-rule change could not reach its id", unit.name)
			}
			if got := digest(text); got != u.ProtocolId {
				t.Fatalf("%s: the id is not the digest over its projection (0x%016x vs 0x%016x) — §3.1's procedure has moved", unit.name, u.ProtocolId, got)
			}
			if digest(strings.Replace(text, law, next, 1)) == u.ProtocolId {
				t.Errorf("%s kept its id across a codec law bump", unit.name)
			}
		})
	}
}

// digest is §3.1's procedure: the low 64 bits of SHA-256 over the projection,
// the final eight bytes big-endian.
func digest(projection string) uint64 {
	sum := sha256.Sum256([]byte(projection))
	return binary.BigEndian.Uint64(sum[24:])
}

// TestGoldenBuildVersion pins the BUILD VERSION and the COOK PROJECTION it
// hashes, per unit (docs/SPEC-TABLES.md §20.8). The number is what a distributed
// store's tuple is keyed by and what a block's prologue carries, so a change
// to how it is computed has to break every pinned value loudly — and the TEXT
// is pinned beside it, so a port reproduces the projection and not only the
// number.
//
// A unit that declares no table is pinned too: its projection is its three
// header lines alone, which is the case a reader has to be able to check by
// eye.
func TestGoldenBuildVersion(t *testing.T) {
	for _, unit := range pinnedUnits {
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

// TestUsagePageBuildVersion holds the ONE build version docs/USAGE.md prints.
// The page shows a worked `schema build-version tables/block/` run, and a
// number pasted into prose moves with nothing — a reader who runs the command
// beside the page has to get the page's answer, so the page is read back here
// and compared against the unit itself.
func TestUsagePageBuildVersion(t *testing.T) {
	const command = "$ schema build-version tables/block/"
	page, err := os.ReadFile("../../docs/USAGE.md")
	if err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(string(page), "\n")
	printed := ""
	for i, line := range lines {
		if strings.TrimSpace(line) == command && i+1 < len(lines) {
			printed = strings.TrimSpace(lines[i+1])
			break
		}
	}
	if printed == "" {
		t.Fatalf("docs/USAGE.md no longer shows a %q run with its answer beneath it — this gate names a line that has moved", command)
	}
	want := fmt.Sprintf("0x%016x", ir.BuildVersion(loadCorpusDir(t, "../../tables/block")))
	if printed != want {
		t.Errorf("docs/USAGE.md prints %s for tables/block and the unit's build version is %s — paste the current one", printed, want)
	}
}
