package unitytable_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/unitytable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/fixedvector"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// unityWriterRangeCorpus is the C++ reference's fixed-form corpus, built by
// `make tables-fixedform-corpus`; SCHEMA_REQUIRE_CORPUS=1 makes its absence a
// failure, as every leg's corpus test does.
func unityWriterRangeCorpus(t *testing.T) string {
	t.Helper()
	corpus := filepath.Join("..", "..", "..", "build", "fixedform-corpus")
	if _, err := os.Stat(filepath.Join(corpus, "manifest.txt")); err != nil {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the corpus is absent: %v", err)
		}
		t.Skipf("fixedform-corpus not built (%v)", err)
	}
	return corpus
}

// unityResavedAim: the one CfloatRangeWiden record the probe saved back holds
// aim's float32 bits exactly (the record ends aim@4, trail@8).
func unityResavedAim(resaved []byte, bits uint32) bool {
	return len(resaved) >= 8 && binary.LittleEndian.Uint32(resaved[len(resaved)-8:]) == bits
}

// TestUnityFixedVersioningWriterRangeHostile:THE WRITER'S RANGE IS THE BOUND
// (docs/FIXED-FORM-ALGORITHM.md §4.6; docs/rulings.json strict-only-read, which
// overturned schema#1164 statement B). The shared cross-version vectors every
// leg reads (fixedvector.WriterRange): an OLD writer's file with one ranged
// value forged past that writer's range, read by the NEW reader whose own range
// is wider — an integer scalar, one element of a ranged fixed array, a uint64
// at or above 2^63, and a compressed float compared in its own type (float32
// against the float32-rounded bound). Each refuses the whole load out_of_range,
// exactly as the C++ reference's NEW reader does. The controls: the unforged old
// compressed-float file (the writer's range narrower than the reader's) lands
// 0.5 exactly, and with the runtime's float32 range check planted out, the
// reader's own wider range lets the forged 1.5 land.
func TestUnityFixedVersioningWriterRangeHostile(t *testing.T) {
	slowtest.Gate(t, "dotnet")
	t.Parallel()
	corpus := unityWriterRangeCorpus(t)
	repo := filepath.Join("..", "..", "..")
	for _, vec := range fixedvector.WriterRange {
		t.Run(vec.Name, func(t *testing.T) {
			t.Parallel()
			want, err := fixedvector.Read(corpus, vec.File)
			if err != nil {
				t.Fatal(err)
			}
			if !want.Refused {
				t.Fatalf("the C++ reference landed %s: %+v", vec.File, want)
			}
			older := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_"+vec.Row+".schema")
			newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_"+vec.Row+".schema")
			wire, err := os.ReadFile(filepath.Join(corpus, vec.File))
			if err != nil {
				t.Fatal(err)
			}
			if got := unityRowLoad(t, newer, vec.Root, unityRowLineage(t, older, vec.Root), wire, nil); got.head() != "refused out_of_range" {
				t.Errorf("%s: %s %v, want refused out_of_range: one forged value refuses the whole load", vec.File, got.head(), got.fields)
			}
		})
	}
	t.Run("cfloat_controls", func(t *testing.T) {
		t.Parallel()
		older := fixedproof.LoadRepoUnit(t, repo, "test/tables/VOLD_cfloat_range_widen.schema")
		newer := fixedproof.LoadRepoUnit(t, repo, "test/tables/VNEW_cfloat_range_widen.schema")
		lin := unityRowLineage(t, older, "CfloatRangeWiden")
		old, err := os.ReadFile(filepath.Join(corpus, "old_cfloat_range_widen.bin"))
		if err != nil {
			t.Fatal(err)
		}
		got := unityRowLoad(t, newer, "CfloatRangeWiden", lin, old, nil)
		if got.head() != "ok 1 u0 w0" || got.fields["r0.lead"] != "1" || got.fields["r0.trail"] != "2" || !unityResavedAim(got.resaved, 0x3f000000) {
			t.Errorf("old_cfloat_range_widen.bin: %s %v, want ok with aim 0.5 exactly (0x3F000000 resaved)", got.head(), got.fields)
		}
		hostile, err := os.ReadFile(filepath.Join(corpus, fixedvector.CfloatRangeWidenHostile))
		if err != nil {
			t.Fatal(err)
		}
		nc := unityRowLoad(t, newer, "CfloatRangeWiden", lin, hostile,
			plantInFile(t, unitytable.RuntimeFile,
				"float maxV = BitConverter.Int32BitsToSingle(unchecked((int)(uint)c.Hi));",
				"float maxV = float.PositiveInfinity;"))
		if nc.head() != "ok 1 u0 w0" || !unityResavedAim(nc.resaved, 0x3fc00000) {
			t.Errorf("the negative control: with the float32 max check planted out, the forged 1.5 did not land: %s %v", nc.head(), nc.fields)
		}
	})
}
