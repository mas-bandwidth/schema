package elixirtable

// THE MESSAGE FORM ON THE ELIXIR LEG, one subtest per clause of the batch card
// (docs/SPEC-TABLES.md §3.3): form byte 2, a batch of bitpacked bodies under
// one announced vocabulary. The surface is measure_messages, save_messages and
// load_messages over the vocabulary the form emits for a `fixed table` root
// (SPEC.md §6.1's snake_case spelling of §3.3's MeasureMessages, SaveMessages,
// LoadMessages).
//
// RED FIRST. The Elixir table backend today emits the FIXED form (form byte 3,
// §3.4) and the block and cook accelerators; the id-table wire (form 1) and the
// message form (form 2) are deferred to schema#515, so the generated Elixir
// carries none of the batch surface the clauses below name. Each subtest fails
// at the step's start for the reason it states, and the probes that exercise
// the wire bytes are the ones STEP 2 names once the port lands.

import (
	"strings"
	"testing"
)

// TestMessageFormBatchElixir is STEP 2's one test, one subtest per clause. It
// is absent at the card's start commit: this backend has no message form at all.
func TestMessageFormBatchElixir(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root
{
    a int32
    b bool
    c uint8
}
`
	src := generatedFixedElixir(t, schema)

	// "SaveMessages takes an array of bodies of ONE root and writes ONE buffer.
	// LoadMessages reads one buffer into the caller's storage and allocates
	// nothing. MeasureMessages sizes the buffer." (docs/SPEC-TABLES.md §3.3)
	t.Run("2.1_batch_round_trip", func(t *testing.T) {
		t.Parallel()
		for _, verb := range []string{"measure_messages", "save_messages", "load_messages"} {
			if !strings.Contains(src, verb) {
				t.Fatalf("the generated Elixir carries no %s, so the batch surface of §3.3 is absent: the form-2 read and write cannot be driven", verb)
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
		for _, reason := range []string{":batch_too_large", ":no_vocabulary"} {
			if !strings.Contains(src, reason) {
				t.Fatalf("the generated Elixir carries no %q refusal, so the batch refusals of §3.3 are absent", reason)
			}
		}
	})

	// "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1 ... ONE malformed
	// counts, and nothing at or after body k is read" and "BYTES AFTER THE PAD
	// ARE MALFORMED" and "a form a reader does not know is a REFUSAL by name"
	// and "a reader handed a form-2 wire where a file was expected refuses by
	// name" (docs/SPEC-TABLES.md §3.3).
	t.Run("2.3_damage_and_forms", func(t *testing.T) {
		t.Parallel()
		for _, refusal := range []string{":newer_form", ":message_form_as_file"} {
			if !strings.Contains(src, refusal) {
				t.Fatalf("the generated Elixir carries no %q refusal, so a batch handed to the file reader, or a form the reader does not know, cannot be refused by name (§3.3)", refusal)
			}
		}
		if !strings.Contains(src, "load_messages") {
			t.Fatalf("the generated Elixir carries no message-form batch reader, so damage inside a body and bytes after the pad cannot be asserted (§3.3)")
		}
	})
}

// generatedFixedElixir emits the unit's Elixir table surface and returns it as
// one string, so a clause can assert over what this backend writes for a `fixed
// table` root (docs/SPEC-TABLES.md §3.3, "THE FORM IS A FIXED TABLE'S").
func generatedFixedElixir(t *testing.T, schema string) string {
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
