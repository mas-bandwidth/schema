package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// countedMinShapesDir is the counted-minimum shapes corpus: a [Min..N]T of
// union and of pointer elements (docs/SPEC-TABLES.md §3, §4), read by every
// leg's test that runs it.
const countedMinShapesDir = "../../testdata/conformance/feature-witnesses/counted-minimum-shapes"

// TestCountedMinimumShapesWitnessPins holds the corpus to the reference: each
// save row is the engine's save of its text byte for byte (a field at its
// birth elided), each text row is the engine's refusal, and each read row's
// verdict is the engine's own over the committed wire.
func TestCountedMinimumShapesWitnessPins(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(countedMinShapesDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	root := m.Lookup("R")
	text, err := os.ReadFile(filepath.Join(countedMinShapesDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	wire := func(name string) []byte {
		t.Helper()
		b, err := os.ReadFile(filepath.Join(countedMinShapesDir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		return b
	}
	kinds := map[string]int{}
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv:%d: %d fields, want 5", i+1, len(f))
		}
		kind, name, value, verdict, want := f[0], f[1], f[2], f[3], f[4]
		kinds[kind+" "+verdict]++
		switch {
		case kind == "save" && verdict == "wrote":
			in := m.New(root)
			var r tabletext.Report
			if !m.Read(in, []byte(value), &r) {
				t.Fatalf("%s: the engine refuses %s: %+v", name, value, r)
			}
			got, err := tablewire.Encode(m, in)
			if err != nil {
				t.Errorf("%s: the engine refuses the save: %v", name, err)
			} else if b := wire(want); !bytes.Equal(got, b) {
				t.Errorf("%s: %s.bin is %s, the engine writes %s", name, want, hex.EncodeToString(b), hex.EncodeToString(got))
			}
		case kind == "text" && verdict == "refused":
			var r tabletext.Report
			if m.Read(m.New(root), []byte(value), &r) {
				t.Errorf("%s: the engine reads %s, the row says refused", name, value)
			}
		case kind == "read":
			var r tabletext.Report
			ok, err := tablewire.Decode(m, m.New(root), wire(value), &r)
			got := "refused"
			if err == nil && ok && !r.Refused {
				got = "read"
			}
			if got != verdict {
				t.Errorf("%s: the engine's verdict on %s.bin is %s (%v), the row says %s", name, value, got, err, verdict)
			}
		default:
			t.Errorf("%s: row %q/%q", name, kind, verdict)
		}
	}
	for _, k := range []string{"save wrote", "text refused", "read read", "read refused"} {
		if kinds[k] == 0 {
			t.Errorf("cases.tsv has no %q row: a corpus without its control holds nothing", k)
		}
	}
}
