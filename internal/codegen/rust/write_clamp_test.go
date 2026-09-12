package rust

import (
	"strings"
	"testing"
)

// writeClampSchema holds one of each length-guarded type: the wstring whose
// used length guards a slice of an N-unit buffer (SPEC §4.12), and string(N)
// and bytes(N) beside it (§4.7).
const writeClampSchema = "package t\n\n" +
	"type P\n{\n" +
	"    caption wstring(7)\n" +
	"    text    string(32)\n" +
	"    data    bytes(16)\n" +
	"}\n"

func generateWriteClamp(t *testing.T) string {
	t.Helper()
	u := unitFromSource(t, "T.schema", writeClampSchema)
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

// TestWriteWStringClampsTheUsedLength pins SPEC §5's "a debug-only check
// obliges the release path to be total" on the Rust wstring WRITER. The
// contract on the used length is debug_assert!, compiled out of a release
// build; the emitted writer then sliced `&value.caption[..length as usize]`
// with whatever length the caller set, and Rust PANICS on an oversized or
// negative slice — an unwinding write, the one thing §5 says no leg does
// outside Elixir. #965 fixed this for string(N)/bytes(N) and left wstring(N)
// unclamped; this test is red on that emitter.
func TestWriteWStringClampsTheUsedLength(t *testing.T) {
	out := generateWriteClamp(t)
	for _, want := range []string{
		// the declared contract still fires first, in debug
		`debug_assert!(length >= 0 && length <= 7`,
		// and the length that rides the wire is the clamped one
		"length = length.clamp(0, 7);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the generated Rust wstring writer is missing %q (SPEC §5: the release path clamps)", want)
		}
	}
	// nothing may slice the buffer by the caller's raw length any more
	if strings.Contains(out, "&value.caption[..value.caption_length as usize]") {
		t.Errorf("the wstring writer still slices by the caller's raw length — a release write panics (SPEC §5)")
	}
}

// TestWriteLengthGuardedSlicesAreAllClamped holds the rule across all three
// length-guarded families on this leg: every used length that guards a slice
// is clamped into [0, N] exactly once before anything is written.
func TestWriteLengthGuardedSlicesAreAllClamped(t *testing.T) {
	out := generateWriteClamp(t)
	for _, want := range []string{
		"length = length.clamp(0, 7);",
		"let clamped_length = value.text_length.clamp(0, 32);",
		"let clamped_length = value.data_length.clamp(0, 16);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the generated Rust is missing %q", want)
		}
	}
}
