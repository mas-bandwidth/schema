package cstable

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// EVERY C# (modern) STORE OF A COMPRESSED FLOAT GOES THROUGH THE -0 → +0
// NORMALISATION (docs/SPEC-TABLES.md §3.3, ruled 2026-11-10
// fixed-compressed-negzero), read off the generated source for the corpus
// unit: the fast twin of TestCSharpCompressedNegZeroGoCorpus, which runs the
// writers. Each write site names the store it must not make (the raw bits as
// held) and the store it must make. The normalisation tests the field's range
// (a compressed float), so a plain float's -0 rides.
func TestCSharpCompressedFloatStoresAreNormalized(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile(filepath.Join(csNegZeroGoCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := Generate(unitFrom(t, string(schema)))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	csCheckNegZeroStores(t, all.String(), []csNegZeroStore{
		{"the variable writer (descriptor walk)", ": f.GetRaw(value, i), Width(f.Kind))", "StoredRaw(f.GetRaw(value, i), f), Width(f.Kind))"},
		{"the variable writer (native region)", ": value.Raw(f, i), Width(f.Kind))", "StoredRaw(value.Raw(f, i), f), Width(f.Kind))"},
		{"the variable writer (typed leaf)", "BitConverter.SingleToUInt32Bits(v.A)", "(v.A + 0.0f)"},
		{"the JSON writer", "WriteFloat(ref o, TableBitsToFloat(unchecked((uint)f.GetRaw(owner, index))), true)", "if (f.HasRange && bits == 0x80000000u) { bits = 0; }"},
		{"the cook (value)", "f.GetWide(value, index) : f.GetRaw(value, index)", "f.GetWide(value, index) : StoredRaw(f.GetRaw(value, index), f)"},
		{"the cook (native region)", "w.Put(at, NativeWord(value.Slot(f, index)", "if (f.Kind == 10) { word = StoredRaw(unchecked((ulong)word), f); }"},
		{"the store's range test (a plain float rides)", "", "return f.Kind == 10 && f.HasRange && (uint)raw == 0x80000000u ? 0 : raw;"},
	})
}

type csNegZeroStore struct{ site, asHeld, normalized string }

func csCheckNegZeroStores(t *testing.T, source string, stores []csNegZeroStore) {
	t.Helper()
	for _, s := range stores {
		if s.asHeld != "" && strings.Contains(source, s.asHeld) {
			t.Errorf("%s stores a compressed float's bits as held: %s", s.site, s.asHeld)
		}
		if !strings.Contains(source, s.normalized) {
			t.Errorf("%s does not store +0 for a compressed float's -0: no %s", s.site, s.normalized)
		}
	}
}
