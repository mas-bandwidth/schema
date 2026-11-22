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

// negZeroWitnessDir is the compressed -0 writer corpus (docs/SPEC-TABLES.md
// §3.3, ruled 2026-11-10 fixed-compressed-negzero), read by every leg's writer
// test that runs it.
const negZeroWitnessDir = "../../testdata/conformance/feature-witnesses/compressed-negzero-writers"

// TestCompressedNegZeroWriterWitnessPins holds the corpus to the reference:
// every .bin a row names is the engine's variable-form encoding of the .json beside it, a
// compressed row owes its own base (the +0 twin), and a plain row's expect is
// not its base and carries -0's bits (the control a normalizing writer fails).
func TestCompressedNegZeroWriterWitnessPins(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(negZeroWitnessDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	text, err := os.ReadFile(filepath.Join(negZeroWitnessDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	negZero := []byte{0x00, 0x00, 0x00, 0x80}
	pinned := map[string]bool{}
	pin := func(root, name string) []byte {
		t.Helper()
		want, err := os.ReadFile(filepath.Join(negZeroWitnessDir, name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if pinned[name] {
			return want
		}
		pinned[name] = true
		src, err := os.ReadFile(filepath.Join(negZeroWitnessDir, name+".json"))
		if err != nil {
			t.Fatal(err)
		}
		in := m.New(m.Lookup(root))
		var r tabletext.Report
		if !m.Read(in, src, &r) {
			t.Fatalf("%s.json: the engine refuses it", name)
		}
		got, err := tablewire.EncodeVariable(m, in)
		if err != nil {
			t.Fatalf("%s.json: %v", name, err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s.bin is not the engine's encoding of %s.json:\n have %s\n want %s", name, name, hex.EncodeToString(want), hex.EncodeToString(got))
		}
		return want
	}
	rows := 0
	carriers := map[string]int{}
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		name, carrier, root, base, path, expect := f[0], f[1], f[2], f[3], f[4], f[5]
		if carrier != "value" && carrier != "region" {
			t.Errorf("%s: carrier %q", name, carrier)
		}
		carriers[carrier]++
		rows++
		b, e := pin(root, base), pin(root, expect)
		plain := path == "p"
		switch {
		case !plain && base != expect:
			t.Errorf("%s: a compressed row owes its +0 twin, its own base; it names %s and %s", name, base, expect)
		case plain && (bytes.Equal(b, e) || !bytes.Contains(e, negZero)):
			t.Errorf("%s: the control's expect must carry -0's bits where its base does not", name)
		}
	}
	if rows == 0 || carriers["value"] == 0 || carriers["region"] == 0 {
		t.Fatalf("cases.tsv: %d rows, carriers %v", rows, carriers)
	}
}
