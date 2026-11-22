package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// negZeroGoCorpusDir is the Go reference engine's compressed -0 writer corpus
// (docs/SPEC-TABLES.md §3.3, ruled 2026-11-10 fixed-compressed-negzero).
const negZeroGoCorpusDir = "../../testdata/conformance/feature-witnesses/compressed-negzero-go"

// negZeroCorpusEncode is the engine's writer for one form of the corpus.
func negZeroCorpusEncode(t *testing.T, m *tabletext.Model, form string, in *tabletext.Instance) []byte {
	t.Helper()
	var out []byte
	var err error
	switch form {
	case "variable":
		out, err = tablewire.EncodeVariable(m, in)
	case "fixed":
		out, err = tablewire.EncodeFixed(m, in)
	default:
		t.Fatalf("form %q", form)
	}
	if err != nil {
		t.Fatalf("%s: %v", form, err)
	}
	return out
}

// negZeroCorpusSet writes -0 into the cell a corpus path names: `field`,
// `field[index]`, `field[Variant]` for an enum-keyed slot, or `field.arm` for
// a union's set arm.
func negZeroCorpusSet(t *testing.T, in *tabletext.Instance, path string) {
	t.Helper()
	if name, arm, ok := strings.Cut(path, "."); ok {
		fv := oracleField(t, in, name)
		if fv.Cell.Arm == nil || fv.Cell.Arm.Def.Name != arm {
			t.Fatalf("%s: the base does not set arm %s", path, arm)
		}
		fv.Cell.Arm.Cell.F = minusZero
		return
	}
	name, index, ok := strings.Cut(path, "[")
	if !ok {
		oracleField(t, in, name).Cell.F = minusZero
		return
	}
	index = strings.TrimSuffix(index, "]")
	fv := oracleField(t, in, name)
	slot, err := strconv.Atoi(index)
	if err != nil {
		slot = tabletext.KeyedValueSlot(fv.Def, tabletext.EnumValue(fv.Def.KeyEnumRef, index))
	}
	if slot < 0 || slot >= len(fv.Elems) {
		t.Fatalf("%s: slot %d of %d", path, slot, len(fv.Elems))
	}
	fv.Elems[slot].F = minusZero
}

// TestCompressedNegZeroGoCorpus runs the corpus on the Go reference engine and
// holds the corpus to it: every <stem>.<form>.bin a row names is the engine's
// encoding of <stem>.json; each row's -0, set in the native value after the
// load, saves to its <expect>; a compressed row's expect is its own base (the
// +0 twin), and a plain row's is not, and carries -0's bits.
func TestCompressedNegZeroGoCorpus(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(negZeroGoCorpusDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	text, err := os.ReadFile(filepath.Join(negZeroGoCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	load := func(root, stem string) *tabletext.Instance {
		t.Helper()
		src, err := os.ReadFile(filepath.Join(negZeroGoCorpusDir, stem+".json"))
		if err != nil {
			t.Fatal(err)
		}
		in := m.New(m.Lookup(root))
		var r tabletext.Report
		if !m.Read(in, src, &r) {
			t.Fatalf("%s.json: the engine refuses it: %+v", stem, r)
		}
		return in
	}
	pinnedRoot := map[string]string{}
	pin := func(form, root, stem string) []byte {
		t.Helper()
		name := stem + "." + form + ".bin"
		if r, ok := pinnedRoot[name]; ok && r != root {
			t.Fatalf("%s is read under %s and %s", name, r, root)
		}
		want, err := os.ReadFile(filepath.Join(negZeroGoCorpusDir, name))
		if err != nil {
			t.Fatal(err)
		}
		if _, ok := pinnedRoot[name]; !ok {
			pinnedRoot[name] = root
			if got := negZeroCorpusEncode(t, m, form, load(root, stem)); !bytes.Equal(got, want) {
				t.Errorf("%s is not the engine's %s encoding of %s.json:\n have %s\n want %s", name, form, stem, hex.EncodeToString(want), hex.EncodeToString(got))
			}
		}
		return want
	}
	negZero := []byte{0x00, 0x00, 0x00, 0x80}
	forms := map[string]int{}
	plains := 0
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 6 {
			t.Fatalf("cases.tsv:%d: %d fields, want 6", i+1, len(f))
		}
		name, form, root, base, path, expect := f[0], f[1], f[2], f[3], f[4], f[5]
		forms[form]++
		b, e := pin(form, root, base), pin(form, root, expect)
		plain := path == "p"
		switch {
		case plain && (bytes.Equal(b, e) || !bytes.Contains(e, negZero)):
			t.Errorf("%s: the control's expect must carry -0's bits where its base does not", name)
		case plain:
			plains++
		case base != expect:
			t.Errorf("%s: a compressed row owes its +0 twin, its own base; it names %s and %s", name, base, expect)
		}
		in := load(root, base)
		negZeroCorpusSet(t, in, path)
		if got := negZeroCorpusEncode(t, m, form, in); !bytes.Equal(got, e) {
			t.Errorf("%s: the %s save of -0 at %s is not %s.%s.bin:\n have %s\n want %s", name, form, path, expect, form, hex.EncodeToString(got), hex.EncodeToString(e))
		}
	}
	if forms["variable"] == 0 || forms["fixed"] == 0 || plains < 2 {
		t.Fatalf("cases.tsv: forms %v, %d plain controls", forms, plains)
	}
}
