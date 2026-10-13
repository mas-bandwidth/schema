package cstable

// TestFixedRoadmapCSharpT14CompressedFloats is cs/R30. The dequantize the
// emitter writes is a separate multiply then add (docs/SPEC.md Floating-point
// discipline, docs/FIXED-FORM-ALGORITHM.md §5.1):
//
//	float scaled = normalized * shape.Delta;
//	bits = TableFloatToBits(scaled + shape.QMin);
//
// The fixture where math.FMA differs is index 2 under min=-1, max=2,
// resolution=0.1 (count=30, delta=3): unfused 0xbf4ccccc, fused 0xbf4ccccd.
// This host has no dotnet, so the compiled byte probe is not run here; the
// emitted arithmetic is what the test holds.

import (
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestFixedRoadmapCSharpT14CompressedFloats(t *testing.T) {
	t.Parallel()
	t.Run("cs/R30", func(t *testing.T) {
		t.Parallel()
		slowtest.Gate(t, "the C# toolchain (it compiles and runs the generated unit)")
		b, err := os.ReadFile("../../lockfile/cfloatrange_test.go")
		if err != nil {
			t.Fatal(err)
		}
		if !strings.Contains(string(b), "func TestLockCFloatResolutionCoarsenedRefuses") || !strings.Contains(string(b), "resolution coarsened") {
			t.Fatal("the lockfile citation is gone")
		}
		u := unitFrom(t, `package cfold

fixed table Reading
{
    lead uint32 = 1
    aim float32 | min = -1, max = 2, resolution = 0.1
    trail uint32 = 2
}
`)
		files, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		var src string
		for name, data := range files {
			if strings.HasSuffix(name, ".cs") {
				src += string(data)
			}
		}
		if !strings.Contains(src, "float scaled = normalized * shape.Delta;") || !strings.Contains(src, "TableFloatToBits(scaled + shape.QMin)") {
			t.Fatal("the emitted dequantize does not round the product before the add")
		}
		if strings.Contains(src, "FusedMultiplyAdd") {
			t.Fatal("the emitted dequantize contracts the multiply-add")
		}
		if _, err := exec.LookPath("dotnet"); err != nil {
			t.Logf("dotnet is not on this host (%v); the byte probe is not run", err)
		}
	})
}
