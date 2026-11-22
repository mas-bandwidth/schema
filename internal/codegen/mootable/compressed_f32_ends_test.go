package mootable_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// mooF32EndsSchema has a compressed float32 and a plain float32 in each form,
// under the corpus's ends (max = 0.1, min = -0.3).
const mooF32EndsSchema = `package f32src
table V
{
    r float32 | min = -0.3, max = 0.1, resolution = 0.01
    p float32
}
fixed table F
{
    r float32 | min = -0.3, max = 0.1, resolution = 0.01
    p float32
}
`

// The float32 nearest each end, widened exactly, as desc.go's f64Pair spells
// a binary64 ({ low word, high word }): -0.3f is 0xbfd3333340000000 and 0.1f
// is 0x3fb99999a0000000. The declared ends as float64 are 0xbfd3333333333333
// and 0x3fb999999999999a.
const (
	mooF32Lo   = "flo = { 0x40000000, 0xbfd33333 }"
	mooF32Hi   = "fhi = { 0xa0000000, 0x3fb99999 }"
	mooF64Hi   = "fhi = { 0x9999999a, 0x3fb99999 }"
	mooWrefSrc = "../../../test/conformance/moo/wref.moo"
)

// mooF32EndsFaults says what does not hold A COMPRESSED FLOAT'S RANGE AT
// FLOAT32 (docs/SPEC-TABLES.md §3.3), one line per fault: the descriptors every
// read and writer compares against (table_values.moo floatInRange), and the
// conformance driver's spelling of a write-refusal row's real.
func mooF32EndsFaults(generated, wref string) []string {
	var faults []string
	if strings.Count(generated, mooF32Lo) < 2 || strings.Count(generated, mooF32Hi) < 2 {
		faults = append(faults, "the descriptors of r (V and F) do not carry the float32 ends "+mooF32Lo+", "+mooF32Hi)
	}
	if strings.Contains(generated, mooF64Hi) {
		faults = append(faults, "a descriptor carries max = 0.1 as a float64")
	}
	// the control: a plain float32 carries no range, so exactly the two
	// compressed fields carry one
	if n := strings.Count(generated, "fhi = {"); n != 2 {
		faults = append(faults, "fhi appears on more or fewer fields than the two compressed ones")
	}
	// the driver spells a real through the unit's exact parser: moo's
	// tonumber holds a fraction only to 1/65536, so writef32(tonumber(text))
	// turned 0.1 into 0x3dccd000 and 2.5000002384185791 into 2.5
	if strings.Contains(wref, "buffer.writef32(b, 0, n)") || !strings.Contains(wref, ".parseFloat32(text)") {
		faults = append(faults, "wref.moo does not spell a float32 real through json.parseFloat32")
	}
	if strings.Contains(wref, "buffer.writef64(b, 0, n)") || !strings.Contains(wref, ".parseFloat64(text)") {
		faults = append(faults, "wref.moo does not spell a float64 real through json.parseFloat64")
	}
	return faults
}

// TestMooCompressedF32EndsSource pins the float32 ends in the emitted moo and
// the conformance driver's exact real without a toolchain;
// TestMooCompressedF32EndsCorpus runs the corpus itself. Its negative control
// puts the declared max back as a float64 and requires the fault named.
func TestMooCompressedF32EndsSource(t *testing.T) {
	t.Parallel()
	files, err := generate(unitFromSource(t, mooF32EndsSchema))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for name, b := range files {
		if strings.HasSuffix(name, "Table.moo") {
			all.Write(b)
		}
	}
	wref, err := os.ReadFile(filepath.FromSlash(mooWrefSrc))
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range mooF32EndsFaults(all.String(), string(wref)) {
		t.Error(f)
	}
	t.Run("NegativeControl", func(t *testing.T) {
		wide := strings.ReplaceAll(all.String(), mooF32Hi, mooF64Hi)
		if got := mooF32EndsFaults(wide, string(wref)); len(got) == 0 {
			t.Error("a descriptor holding max = 0.1 as a float64 went unnoticed")
		}
	})
}
