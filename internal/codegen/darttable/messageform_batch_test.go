package darttable

// THE MESSAGE FORM ON THE DART LEG, one subtest per clause of the batch card
// (docs/SPEC-TABLES.md §3.3). The Dart table backend today emits the FIXED form
// (form byte 3, §3.4) and the block and cook accelerators; the ID-TABLE wire
// (form 1) and the message form (form 2) are deferred to schema#514, so this
// backend carries NONE of the batch surface the clauses below name. The test is
// the red side of the card: every subtest fails because the generated Dart has
// no MeasureMessages, no SaveMessages and no LoadMessages to assert over.
//
// The clauses quote the page sentence each implements. When the port lands, the
// per-table verbs and their runtime make these asserts green, and the probes
// that exercise the wire bytes are the ones the card's STEP 2 names.

import (
	"strings"
	"testing"
)

// TestMessageFormBatchDart is STEP 2's one test, one subtest per clause. It is
// absent at the card's start commit: this backend has no message form at all.
func TestMessageFormBatchDart(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root
{
    a int32
    b bool
    c uint8
}
`
	src := generatedFixedDart(t, schema)

	// "SaveMessages takes an array of bodies of ONE root and writes ONE buffer.
	// LoadMessages reads one buffer into the caller's storage and allocates
	// nothing. MeasureMessages sizes the buffer." (docs/SPEC-TABLES.md §3.3)
	t.Run("2.1_batch_round_trip", func(t *testing.T) {
		t.Parallel()
		for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages"} {
			if !strings.Contains(src, verb) {
				t.Fatalf("the generated Dart carries no %s, so the batch surface of §3.3 is absent: the form-2 read and write cannot be driven", verb)
			}
		}
	})

	// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME. MeasureMessages and
	// SaveMessages both refuse and return -1 ... the reason batch_too_large"
	// and "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME ... the
	// reader says it holds no vocabulary for the peer" and "M ABOVE THE
	// CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ... the reader
	// sets the returned count to the wire's M" (docs/SPEC-TABLES.md §3.3).
	t.Run("2.2_refusals", func(t *testing.T) {
		t.Parallel()
		for _, reason := range []string{"batch_too_large", "no_vocabulary"} {
			if !strings.Contains(src, reason) {
				t.Fatalf("the generated Dart carries no %q refusal, so the batch refusals of §3.3 are absent", reason)
			}
		}
	})

	// "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1 ... ONE malformed
	// counts, and nothing at or after body k is read" and "BYTES AFTER THE PAD
	// ARE MALFORMED" and "a form a reader does not know is a REFUSAL by name"
	// (docs/SPEC-TABLES.md §3.3).
	t.Run("2.3_damage_and_forms", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(src, "LoadMessages") {
			t.Fatalf("the generated Dart carries no message-form batch reader, so damage inside a body, bytes after the pad, newer_form and message_form_as_file cannot be asserted (§3.3)")
		}
	})
}

// generatedFixedDart emits the unit's Dart table surface and returns it as one
// string, so a clause can assert over what this backend writes for a `fixed
// table` root (docs/SPEC-TABLES.md §3.3, "THE FORM IS A FIXED TABLE'S").
func generatedFixedDart(t *testing.T, schema string) string {
	t.Helper()
	u := unitFrom(t, schema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all strings.Builder
	for _, b := range files {
		all.Write(b)
	}
	if all.Len() == 0 {
		t.Fatal("this backend emitted nothing for a unit declaring a fixed table")
	}
	return all.String()
}
