package cpp

import (
	"strings"
	"testing"
)

// writeClampSchema holds one of each length-guarded type: the wstring whose
// used length walks a buffer of N + 1 units (SPEC §4.12), and string(N) and
// bytes(N) beside it (§4.7).
const writeClampSchema = "package t\n\n" +
	"type P\n{\n" +
	"    caption wstring(7)\n" +
	"    text    string(32)\n" +
	"    data    bytes(16)\n" +
	"}\n"

func generateWriteClamp(t *testing.T) string {
	t.Helper()
	u := unitFromSources(t, map[string]string{"T.schema": writeClampSchema})
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all strings.Builder
	for _, b := range files {
		all.Write(b)
	}
	return all.String()
}

// TestWriteLengthGuardedSlicesAreClamped pins SPEC §5's "a debug-only check
// obliges the release path to be total" on the C++ writers. Every used length
// that guards a copy is held by serialize_assert, which compiles out under
// NDEBUG — and the emitted writer then walked the buffer by whatever length
// the caller set: no trap, but bytes read past the end, so the wire is not
// deterministic either. The release path clamps the used length into [0, N]
// once and writes THAT length. Red on the emitter #965 left behind.
func TestWriteLengthGuardedSlicesAreClamped(t *testing.T) {
	out := generateWriteClamp(t)
	for _, want := range []string{
		// the declared contract still fires first, in debug
		"serialize_assert( value.caption_length >= 0 && value.caption_length <= 7 );",
		"serialize_assert( value.text_length >= 0 && value.text_length <= 32 );",
		"serialize_assert( value.data_length >= 0 && value.data_length <= 16 );",
		// and what rides the wire is the clamped length, on all three
		"const int32_t clamped_length = value.caption_length < 0 ? 0 : ( value.caption_length > ( 7 ) ? ( 7 ) : value.caption_length );",
		"const int32_t clamped_length = value.text_length < 0 ? 0 : ( value.text_length > ( 32 ) ? ( 32 ) : value.text_length );",
		"const int32_t clamped_length = value.data_length < 0 ? 0 : ( value.data_length > ( 16 ) ? ( 16 ) : value.data_length );",
		"for ( int32_t i = 0; i < clamped_length; i++ )",
		"write_bytes( stream, value.text, clamped_length );",
		"write_bytes( stream, value.data, clamped_length );",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the generated C++ writer is missing %q (SPEC §5: the release path clamps)", want)
		}
	}
	// no write loop or copy may range over the caller's raw length any more
	for _, bad := range []string{
		"write_bytes( stream, value.text, value.text_length );",
		"write_bytes( stream, value.data, value.data_length );",
		"for ( int32_t i = 0; i < value.caption_length; i++ )\n    {\n        write_bits",
	} {
		if strings.Contains(out, bad) {
			t.Errorf("a C++ write still uses the caller's raw length: %q (SPEC §5)", bad)
		}
	}
}
