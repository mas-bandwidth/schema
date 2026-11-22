package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// rangeWriteCorpusDir is the compressed-float range write corpus
// (docs/SPEC-TABLES.md §3.3, §3.4): a fixed-form save refuses a compressed
// float outside its declared range and writes one inside it.
const rangeWriteCorpusDir = "../../testdata/conformance/feature-witnesses/compressed-range-write"

// rangeWriteSet writes v into the cell a corpus path names: `field`,
// `field[index]`, `field[Variant]` for an enum-keyed slot, or `field.arm` for
// a union's set arm.
func rangeWriteSet(t *testing.T, in *tabletext.Instance, path string, v float64) {
	t.Helper()
	if name, arm, ok := strings.Cut(path, "."); ok {
		fv := oracleField(t, in, name)
		if fv.Cell.Arm == nil || fv.Cell.Arm.Def.Name != arm {
			t.Fatalf("%s: the base does not set arm %s", path, arm)
		}
		fv.Cell.Arm.Cell.F = v
		return
	}
	name, index, ok := strings.Cut(path, "[")
	if !ok {
		oracleField(t, in, name).Cell.F = v
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
	fv.Elems[slot].F = v
}

// TestCompressedRangeWriteCorpus runs the corpus on the Go reference engine
// and holds the corpus to it: a `refused` row's fixed save answers
// invalid_value and writes nothing; every other row's save is
// <expect>.fixed.bin byte for byte. The plain control is written, and at least
// one row of every refused shape has an in-range twin that is written.
//
// SCHEMA_UPDATE_RANGE_WRITE_CORPUS=1 writes the .bin files instead of
// comparing them; the comparison is the gate.
func TestCompressedRangeWriteCorpus(t *testing.T) {
	t.Parallel()
	paths, err := compiler.GatherPaths([]string{filepath.Join(rangeWriteCorpusDir, "Probe.schema")})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	m := tabletext.NewModel(u)
	src, err := os.ReadFile(filepath.Join(rangeWriteCorpusDir, "base.json"))
	if err != nil {
		t.Fatal(err)
	}
	text, err := os.ReadFile(filepath.Join(rangeWriteCorpusDir, "cases.tsv"))
	if err != nil {
		t.Fatal(err)
	}
	update := os.Getenv("SCHEMA_UPDATE_RANGE_WRITE_CORPUS") == "1"
	refused, written := map[string]int{}, map[string]int{}
	for i, line := range strings.Split(string(text), "\n") {
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		f := strings.Split(line, "\t")
		if len(f) != 5 {
			t.Fatalf("cases.tsv:%d: %d fields, want 5", i+1, len(f))
		}
		name, root, path, value, expect := f[0], f[1], f[2], f[3], f[4]
		v, err := strconv.ParseFloat(value, 64)
		if err != nil {
			t.Fatalf("%s: value %q: %v", name, value, err)
		}
		in := m.New(m.Lookup(root))
		var r tabletext.Report
		if !m.Read(in, src, &r) {
			t.Fatalf("base.json: the engine refuses it: %+v", r)
		}
		rangeWriteSet(t, in, path, v)
		shape, _, _ := strings.Cut(path, "[")
		shape, _, _ = strings.Cut(shape, ".")
		got, err := tablewire.EncodeFixed(m, in)
		if expect == "refused" {
			var bad *tabletext.InvalidValue
			if !errors.As(err, &bad) || got != nil {
				t.Errorf("%s: the fixed save of %s at %s is not refused invalid_value: %v, % x", name, value, path, err, got)
			}
			refused[shape]++
			continue
		}
		if err != nil {
			t.Errorf("%s: the fixed save of %s at %s refuses: %v", name, value, path, err)
			continue
		}
		written[shape]++
		file := filepath.Join(rangeWriteCorpusDir, expect+".fixed.bin")
		if update {
			if err := os.WriteFile(file, got, 0o644); err != nil {
				t.Fatal(err)
			}
			continue
		}
		want, err := os.ReadFile(file)
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(got, want) {
			t.Errorf("%s: the fixed save of %s at %s is not %s.fixed.bin:\n have %s\n want %s", name, value, path, expect, hex.EncodeToString(got), hex.EncodeToString(want))
		}
	}
	if written["p"] == 0 {
		t.Error("cases.tsv: no plain control row is written")
	}
	for shape := range refused {
		if written[shape] == 0 {
			t.Errorf("cases.tsv: shape %s is refused with no in-range twin written", shape)
		}
	}
	if len(refused) < 5 {
		t.Errorf("cases.tsv: %d refused shapes, want a, xs, ys, ks and pick", len(refused))
	}
}
