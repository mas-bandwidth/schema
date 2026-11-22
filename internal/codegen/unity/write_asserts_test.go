package unity

import (
	"strings"
	"testing"
)

// TestUnityPacketWriteAssertsRows8And10 holds the C# (unity) packet Write to
// docs/SPEC.md §5.1's C# packet `Write` line ("1, 3–8 and 10 debug"), which the
// unity leg shares with C# (modern): a string(N)'s used bytes are asserted to
// be well-formed UTF-8 with no zero byte (row 8), and a compressed float32 is
// asserted finite and inside [min, max] before it is quantized (row 10). Both
// are Debug.Assert, the leg's write-side idiom, placed before the serialize
// call they guard. The unity package has no harness that compiles its output,
// so the emitted text is the check.
func TestUnityPacketWriteAssertsRows8And10(t *testing.T) {
	text := generateText(t, `package probe

type Probe
{
    name  string(8)
    scale float32 | min = -2, max = 2, resolution = 0.01
}
`)
	write := text
	if i := strings.Index(text, "Write("); i >= 0 {
		write = text[i:]
	} else {
		t.Fatalf("no Write in the generated text:\n%s", text)
	}

	row8 := `Debug.Assert(ValidUtf8Text(value.Name.AsSpan().Slice(0, value.NameLength)), "value.Name holds a zero byte or is not well-formed UTF-8");`
	row10 := `Debug.Assert(float.IsFinite(compressedValue) && compressedValue >= -2.0f && compressedValue <= 2.0f, "value.Scale out of range [-2.0f, 2.0f]");`
	for _, c := range []struct {
		row, assert, call string
	}{
		{"row 8", row8, "SerializeBytes(value.Name"},
		{"row 10", row10, "SerializeCompressedFloatPrecomputed(ref compressedValue"},
	} {
		a := strings.Index(write, c.assert)
		if a < 0 {
			t.Errorf("%s: the packet Write does not emit\n  %s\ngenerated:\n%s", c.row, c.assert, write)
			continue
		}
		k := strings.Index(write, c.call)
		if k < 0 || k < a {
			t.Errorf("%s: the assert does not precede %s", c.row, c.call)
		}
	}
	if !strings.Contains(text, "static bool "+utf8Helper+"(") {
		t.Errorf("the row 8 assert names %s, but the unit does not define it", utf8Helper)
	}
}
