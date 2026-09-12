// THE READ BUFFER CONTRACT, on the pages. The 8 bytes of slack a reader is
// allowed past the payload had been nine per-target facts in docs/SPEC.md
// §6.3 and a per-language list in docs/USAGE.md, with nothing normative in §4
// saying what the CALLER owes — so the question was asked again and again.
// It is law now, stated once in §4.3 (Glenn, 2026-09-11: the 8 bytes are the
// design intent, so an implementation may load 64 bits at a time and still be
// correct). This gate holds the three sentences a reader takes away: the law
// is in §4, the per-target table reads as a RECORD under it rather than as
// nine contracts, and USAGE.md points at the law instead of teaching the
// per-language numbers as the contract.
package compiler

import (
	"os"
	"strings"
	"testing"
)

func TestPagesStateTheReadBufferContractAsLaw(t *testing.T) {
	spec := readPage(t, "../docs/SPEC.md")
	usage := readPage(t, "../docs/USAGE.md")

	// The law itself, in §4, in one sentence.
	const law = "**A caller hands a reader a buffer with AT LEAST 8 BYTES READABLE PAST THE\n> PAYLOAD'S LOGICAL LENGTH.**"
	if !strings.Contains(spec, law) {
		t.Error("docs/SPEC.md no longer states the read buffer contract as law in §4.3")
	}
	for _, half := range []string{
		"**MAY read up to 8 bytes past the payload**",
		"**MUST NOT depend on more**",
		"reads nothing past the payload is **conforming too**",
		"**The slack's contents are\nnever interpreted.**",
	} {
		if !strings.Contains(spec, half) {
			t.Errorf("docs/SPEC.md §4.3 dropped a half of the read buffer contract: %q", half)
		}
	}

	// A short buffer is the caller's error, not a malformed payload — the
	// clarification §5's terminal-refusal rule needs to stay true as written.
	if !strings.Contains(spec, "**A buffer short of the read buffer contract is a CALLER error, not a\nmalformed payload.**") {
		t.Error("docs/SPEC.md §5 no longer separates a short buffer from a malformed payload")
	}

	// §6.3 is a record of what each target reads, under the one law.
	if !strings.Contains(spec, "**The buffer-contract row is WHAT EACH TARGET READS TODAY, under §4.3's read\nbuffer contract — it is not nine contracts.**") {
		t.Error("docs/SPEC.md §6.3 no longer frames the per-target buffer row as a record under §4.3")
	}
	for _, stale := range []string{
		"no slack required",
		"read buffers need NO slack past the payload",
		"≥7 bytes read slack",
		"read allocations extend ≥8 bytes past packet data (required)",
	} {
		if strings.Contains(spec, stale) {
			t.Errorf("docs/SPEC.md still states the slack as a per-target contract: %q", stale)
		}
	}
	if !strings.Contains(spec, "`RangeError` off the DataView") {
		t.Error("docs/SPEC.md §6.3's JavaScript row no longer says what a short buffer does there")
	}

	// USAGE.md points at the law rather than teaching nine numbers.
	if !strings.Contains(usage, "**The slack is ONE number in all nine languages: 8.**") {
		t.Error("docs/USAGE.md no longer points the reader at §4.3's one number")
	}
	if strings.Contains(usage, "the buffer-slack contract above is the one\nthing that differs per language") {
		t.Error("docs/USAGE.md still calls the slack the one thing that differs per language")
	}
}

func readPage(t *testing.T, path string) string {
	t.Helper()
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("%s: %v", path, err)
	}
	return string(body)
}
