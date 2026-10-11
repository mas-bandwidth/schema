package javatable

import (
	"strings"
	"testing"
)

// TestMessageFormBatchJava pins the MESSAGE FORM surface (docs/SPEC-TABLES.md
// §3.3) on the Java leg: the batch verbs MeasureMessages, SaveMessages and
// LoadMessages over the announced vocabulary, their refusal reasons, and their
// damage rules. The three subtests are STEP 2's three clauses, one subtest
// each, and every one fails red today for the same reason: the Java message
// form is unimplemented, so none of its spellings is in the generated source.
func TestMessageFormBatchJava(t *testing.T) {
	t.Parallel()

	// A message is a bitpacked body under a vocabulary announced once, and the
	// form is a fixed table's (docs/SPEC-TABLES.md §3.3): a `fixed table` root
	// is what rides here, so the probe is one.
	src := `package probe

enum Region { NA, EU, APAC, SA }

fixed table LoginRequest
{
    player_id     uint64 = 0
    session_token bytes(32)
    client_build  uint32 = 0
    region        Region
}
`
	files := generate(t, src)
	all := strings.Join(allSources(files), "\n")

	t.Run("surface", func(t *testing.T) {
		// "SaveMessages takes an array of bodies of ONE root and writes ONE
		// buffer. LoadMessages reads one buffer into the caller's storage and
		// allocates nothing. MeasureMessages sizes the buffer."
		// (docs/SPEC-TABLES.md §3.3, "The primitive is a BATCH")
		for _, verb := range []string{"measureMessages", "saveMessages", "loadMessages"} {
			if !strings.Contains(all, verb) {
				t.Fatalf("%q absent from the generated Java: the message form's three batch verbs are not emitted", verb)
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME" and "M ABOVE THE
		// CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME", both on the
		// reason batch_too_large; "A BODY FROM A PEER THAT NEVER ANNOUNCED IS
		// REFUSED BY NAME" on no_vocabulary (docs/SPEC-TABLES.md §3.3).
		for _, reason := range []string{"batch_too_large", "no_vocabulary"} {
			if !strings.Contains(all, reason) {
				t.Fatalf("refusal reason %q absent from the generated Java: the message form's refusal vocabulary is not emitted", reason)
			}
		}
	})

	t.Run("damage", func(t *testing.T) {
		// "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1" and "BYTES AFTER THE
		// PAD ARE MALFORMED", beside the refusal reasons newer_form and
		// message_form_as_file (docs/SPEC-TABLES.md §3.3).
		for _, reason := range []string{"newer_form", "message_form_as_file"} {
			if !strings.Contains(all, reason) {
				t.Fatalf("refusal reason %q absent from the generated Java: the message form's damage and refusal vocabulary is not emitted", reason)
			}
		}
	})
}

// allSources flattens one generated unit's files into a single string, one
// file's text after another, so a surface spelling can be searched across the
// whole unit without caring which Java file carries it.
func allSources(files map[string][]byte) []string {
	out := make([]string, 0, len(files))
	for _, data := range files {
		out = append(out, string(data))
	}
	return out
}
