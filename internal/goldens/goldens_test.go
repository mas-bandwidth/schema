// Package goldens pins the compiler's output (SPEC §7.2 gates 1 and 2): the
// corpus's generated source byte-for-byte in both C++ message modes, the
// protocol id exactly, and the corpus's formatter-canonical form. A change to
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

	"github.com/mas-bandwidth/schema/internal/check"
	"github.com/mas-bandwidth/schema/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/internal/format"
	"github.com/mas-bandwidth/schema/internal/ir"
	"github.com/mas-bandwidth/schema/internal/parser"
)

var update = flag.Bool("update", false, "rewrite the golden files from current output")

const (
	corpusDir = "../../examples"
	goldenDir = "../../testdata/golden"
)

func loadCorpus(t *testing.T) *ir.Unit {
	t.Helper()
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	var files []check.SourceFile
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		p := filepath.Join(corpusDir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		f, perrs := parser.Parse(p, data)
		if len(perrs) > 0 {
			t.Fatalf("corpus does not parse: %v", perrs[0])
		}
		files = append(files, check.SourceFile{
			Path:  p,
			Name:  e.Name(),
			Base:  strings.TrimSuffix(e.Name(), ".schema"),
			Bytes: data,
			AST:   f,
		})
	}
	u, cerrs := check.Unit(files)
	if len(cerrs) > 0 {
		t.Fatalf("corpus does not check: %v", cerrs[0])
	}
	return u
}

// TestCorpusIsCanonical is the fmt-drift gate: every corpus file must already
// be in schemafmt's one style (the compiler formats before processing, so a
// non-canonical file in git means someone bypassed the tool).
func TestCorpusIsCanonical(t *testing.T) {
	entries, err := os.ReadDir(corpusDir)
	if err != nil {
		t.Fatal(err)
	}
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".schema") {
			continue
		}
		p := filepath.Join(corpusDir, e.Name())
		data, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		out, err := format.Format(p, data)
		if err != nil {
			t.Fatalf("%s: %v", p, err)
		}
		if string(out) != string(data) {
			t.Errorf("%s is not formatter-canonical — run: bin/schema fmt examples", p)
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

// TestGoldenSource pins the generated C++ byte-for-byte, both message
// representations (SPEC §7.2 gate 1).
func TestGoldenSource(t *testing.T) {
	u := loadCorpus(t)
	for _, mode := range []string{"union", "variant"} {
		files, err := cpp.Generate(u, cpp.Options{MessageRepr: mode})
		if err != nil {
			t.Fatal(err)
		}
		dir := filepath.Join(goldenDir, mode)
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
				t.Errorf("%s/%s: generated source diverged from its golden — deliberate emitter change? re-pin with make update-goldens; if the WIRE moved under an unchanged schema, that is stop-the-line (SPEC §3.1)", mode, n)
			}
		}
	}
}
