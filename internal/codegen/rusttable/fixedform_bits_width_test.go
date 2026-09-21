package rusttable

import (
	"strings"
	"testing"
)

// TestFixedBitsWidthClampsToItsOwnWidth pins the fixed form's LAST bound: a
// `bits(N)` whose N is narrower than the four- or eight-byte lane it rides in
// (§3.4, §4; ir.TableFixedStorageBytes) clamps to 2^N - 1 on every read, over
// the storage a read can have written. A `bits(N)` that fills its lane exactly
// emits no clamp at all — a comparison that can never fire is one rustc does
// not compile cleanly, and the C++ twin reds it under the repo's own flags
// (tables/examples/Ranges.schema, `make tables-clamp-limits`).
func TestFixedBitsWidthClampsToItsOwnWidth(t *testing.T) {
	const src = `package probe

fixed table RangedWidths
{
    b8  bits(8)
    b12 bits(12)
    b32 bits(32)
    b48 bits(48)
    b64 bits(64)
}
`
	body := string(generate(t, src)["probe_fixed.rs"])

	// THE CLAMP: a narrower-than-lane bits(N) is held to 2^N - 1 on the high
	// side only, and counts one clamped when the forgery is real.
	for _, want := range []string{
		"*clamped += (value.b8 > 255) as i32; // bits(8)'s own width",
		"value.b8 = value.b8.min(255);",
		"*clamped += (value.b12 > 4095) as i32; // bits(12)'s own width",
		"value.b12 = value.b12.min(4095);",
		"*clamped += (value.b48 > 281474976710655) as i32; // bits(48)'s own width",
		"value.b48 = value.b48.min(281474976710655);",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("a bits(N) narrower than its storage lane must clamp to 2^N-1; missing %q", want)
		}
	}

	// CONTROL 1 (NOT VACUOUS): a bits(32) rides a 4-byte lane and a bits(64)
	// an 8-byte one — both fill theirs exactly, so NEITHER may emit a clamp. If
	// this test passed while the clamp were emitted unconditionally, the two
	// full-width fields would be clamped too and this assertion would catch it.
	if strings.Contains(body, "value.b32 >") || strings.Contains(body, "value.b32.min") {
		t.Error("bits(32) fills its storage lane exactly and must emit NO clamp")
	}
	if strings.Contains(body, "value.b64 >") || strings.Contains(body, "value.b64.min") {
		t.Error("bits(64) fills its storage lane exactly and must emit NO clamp")
	}
}
