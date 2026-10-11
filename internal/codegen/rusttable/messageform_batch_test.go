package rusttable

// The MESSAGE FORM (form byte 2) is a fixed table's on the wire
// (docs/SPEC-TABLES.md §3.3): a batch of bitpacked bodies under one announced
// vocabulary, sized by MeasureMessages, written by SaveMessages and read back
// by LoadMessages. SPEC.md §6.1 spells those names per target ("the type name is
// in the function name in every target, for uniformity"); this leg's spelling is
// the snake-case free function `<root>_<verb>`.
//
// The Rust table emitter carries none of this today: the fixed form (form 3)
// has its own surface and its own reasons, but no form-2 batch surface at all.
// So every clause below fails at its first assertion, for the message-form-
// absent reason. This is the red-first checkpoint for the Rust leg; the clauses
// are what the port must satisfy, not a proof that it is done.

import (
	"sort"
	"strings"
	"testing"
)

// rustMessageFormSurface is the concatenated generated Rust for a fixed table
// root, which is the only root a message form rides (docs/SPEC-TABLES.md §3.3:
// "THE FORM IS A FIXED TABLE'S, and that is a declaration, not a derivation").
func rustMessageFormSurface(t *testing.T) string {
	t.Helper()
	files := generate(t, valueOnly)
	names := make([]string, 0, len(files))
	for name := range files {
		names = append(names, name)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, name := range names {
		all.Write(files[name])
	}
	return all.String()
}

func TestMessageFormBatchRust(t *testing.T) {
	t.Parallel()
	gen := rustMessageFormSurface(t)

	// "SaveMessages takes an array of bodies of ONE root and writes ONE buffer.
	//  LoadMessages reads one buffer into the caller's storage and allocates
	//  nothing. MeasureMessages sizes the buffer." (docs/SPEC-TABLES.md §3.3,
	//  THE PRIMITIVE IS A BATCH)
	t.Run("2.1_batch_roundtrip", func(t *testing.T) {
		t.Parallel()
		for _, verb := range []string{"measure_messages", "save_messages", "load_messages"} {
			if !strings.Contains(gen, verb) {
				t.Fatalf("generated Rust lacks %s: a fixed table's batch surface is MeasureMessages, SaveMessages, LoadMessages over the announced vocabulary (§3.3)", verb)
			}
		}
	})

	// "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
	//  decoded, no counter moves and malformed does not fire." (docs/SPEC-TABLES.md
	//  §3.3, the scope is the ANNOUNCEMENT)
	t.Run("2.2_no_vocabulary", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "load_messages") {
			t.Fatal("generated Rust lacks load_messages: a batch read without a resolved vocabulary is refused by name no_vocabulary (§3.3)")
		}
		if !strings.Contains(gen, "no_vocabulary") {
			t.Fatal("generated Rust never names no_vocabulary: a batch read without a resolved vocabulary is refused by name (§3.3)")
		}
	})

	// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME. MeasureMessages and
	//  SaveMessages both refuse and return -1 ... the reason batch_too_large on
	//  it, never an exception." / "M ABOVE THE CALLER'S CAPACITY ON THE READ SIDE
	//  IS A REFUSAL BY NAME ... LoadMessages compares it to the storage it was
	//  handed and refuses before it decodes anything." (docs/SPEC-TABLES.md §3.3)
	t.Run("2.2_batch_too_large", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "save_messages") {
			t.Fatal("generated Rust lacks save_messages: M above 256 on the write side is a refusal by name (§3.3)")
		}
		if !strings.Contains(gen, "batch_too_large") {
			t.Fatal("generated Rust never names batch_too_large on the message path: M above 256 on the write side and M above capacity on the read side are refusals by name (§3.3)")
		}
	})

	// "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1. The returned count states
	//  k - 1, ONE malformed counts, and nothing at or after body k is read." /
	//  "BYTES AFTER THE PAD ARE MALFORMED." (docs/SPEC-TABLES.md §3.3)
	t.Run("2.3_damage", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "load_messages") {
			t.Fatal("generated Rust lacks load_messages: damage inside body k delivers k-1 bodies and one malformed (§3.3)")
		}
		if !strings.Contains(gen, "malformed") {
			t.Fatal("generated Rust never reports malformed on the message path: damage inside a body and bytes after the pad are malformed (§3.3)")
		}
	})

	// "The FORM BYTE is a whole byte and is read FIRST ... so a form a reader
	//  does not know is a REFUSAL by name and never damage." A file reader that
	//  meets a message-form buffer answers message_form_as_file. (docs/SPEC-TABLES.md
	//  §3.3; the file reader's refusal is §3.4's own.)
	t.Run("2.3_forms", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "load_messages") {
			t.Fatal("generated Rust lacks load_messages: an unknown form byte is a refusal by name (§3.3)")
		}
		if !strings.Contains(gen, "newer_form") {
			t.Fatal("generated Rust never reports newer_form: a form byte the leg does not carry is a refusal by name (§3.3)")
		}
		if !strings.Contains(gen, "message_form_as_file") {
			t.Fatal("generated Rust never reports message_form_as_file: the file reader refuses a message-form buffer by name (§3.3)")
		}
	})
}
