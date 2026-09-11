package csharp

import (
	"strings"
	"testing"
)

// writeClampSchema holds one of each length-guarded type: the wstring whose
// used length indexes a char[N] (SPEC §4.12), and string(N) and bytes(N)
// beside it (§4.7).
const writeClampSchema = "package t\n\n" +
	"type P\n{\n" +
	"    caption wstring(7)\n" +
	"    text    string(32)\n" +
	"    data    bytes(16)\n" +
	"}\n"

func generateWriteClamp(t *testing.T) string {
	t.Helper()
	files := generateCs(t, "T", writeClampSchema)
	var all strings.Builder
	for _, b := range files {
		all.Write(b)
	}
	return all.String()
}

// TestWriteWStringClampsTheUsedLength pins SPEC §5's "a debug-only check
// obliges the release path to be total" on the C# wstring WRITER. The contract
// on the used length is Debug.Assert, gone from a Release build; the emitted
// writer then looped to the caller's raw value.CaptionLength and indexed a
// char[N], which THROWS IndexOutOfRangeException — an unwinding write, the one
// thing §5 says no leg does outside Elixir. #965 fixed this for
// string(N)/bytes(N) and left wstring(N) unclamped; this test is red on that
// emitter.
func TestWriteWStringClampsTheUsedLength(t *testing.T) {
	out := generateWriteClamp(t)
	for _, want := range []string{
		// the declared contract still fires first, in debug
		`Debug.Assert(value.CaptionLength >= 0 && value.CaptionLength <= 7`,
		// and the length that rides the wire, and bounds the loop, is clamped
		"int clampedLength = Math.Clamp(value.CaptionLength, 0, 7);",
		"for (int wideIndex = 0; wideIndex < clampedLength; wideIndex++)",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the generated C# wstring writer is missing %q (SPEC §5: the release path clamps)", want)
		}
	}
}

// TestWriteLengthGuardedSlicesAreAllClamped holds the rule across all three
// length-guarded families on this leg.
func TestWriteLengthGuardedSlicesAreAllClamped(t *testing.T) {
	out := generateWriteClamp(t)
	for _, want := range []string{
		"int clampedLength = Math.Clamp(value.CaptionLength, 0, 7);",
		"int clampedLength = Math.Clamp(value.TextLength, 0, 32);",
		"int clampedLength = Math.Clamp(value.DataLength, 0, 16);",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("the generated C# is missing %q", want)
		}
	}
}
