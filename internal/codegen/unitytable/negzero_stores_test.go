package unitytable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
)

// EVERY C# (unity) STORE OF A COMPRESSED FLOAT GOES THROUGH THE -0 → +0
// NORMALISATION (docs/SPEC-TABLES.md §3.3, ruled 2026-11-10
// fixed-compressed-negzero), read off the generated source for the corpus
// unit: the fast twin of TestUnityCompressedNegZeroGoCorpus, which runs the
// writers. Each write site names the store it must not make (the raw bits as
// held) and the store it must make. The normalisation tests the field's range
// (a compressed float), so a plain float's -0 rides.
func TestUnityCompressedFloatStoresAreNormalized(t *testing.T) {
	t.Parallel()
	schema, err := os.ReadFile(filepath.Join(unityNegZeroGoCorpusDir, "Probe.schema"))
	if err != nil {
		t.Fatal(err)
	}
	files, err := unityGenerateFiles(fixedproof.ParseCheckUnit(t, string(schema)), nil)
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	source := all.String()
	for _, s := range []struct{ site, asHeld, normalized string }{
		{"the variable writer", ": f.GetRaw(value, i), Width(f.Kind))", "StoredRaw(f.GetRaw(value, i), f), Width(f.Kind))"},
		{"the retaining variable writer", ": f.GetRaw(value.Obj, i), Width(f.Kind))", "StoredRaw(f.GetRaw(value.Obj, i), f), Width(f.Kind))"},
		{"the JSON writer", "WriteFloat(ref o, TableBitsToFloat(unchecked((uint)f.GetRaw(owner, index))), true)", "if (f.HasFloatRange && bits == 0x80000000u) { bits = 0; }"},
		{"the cook", "f.GetWide(value, index) : f.GetRaw(value, index)", "f.GetWide(value, index) : StoredRaw(f.GetRaw(value, index), f)"},
		{"the store's range test (a plain float rides)", "", "if (!f.HasFloatRange) { return raw; }"},
	} {
		if s.asHeld != "" && strings.Contains(source, s.asHeld) {
			t.Errorf("%s stores a compressed float's bits as held: %s", s.site, s.asHeld)
		}
		if !strings.Contains(source, s.normalized) {
			t.Errorf("%s does not store +0 for a compressed float's -0: no %s", s.site, s.normalized)
		}
	}
}
