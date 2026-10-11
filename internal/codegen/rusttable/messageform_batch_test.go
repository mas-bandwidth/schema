package rusttable

import (
	"sort"
	"strings"
	"testing"
)

// TestMessageFormBatchRust holds THE MESSAGE FORM (form byte 2, a batch of
// bitpacked bodies under one announced vocabulary, docs/SPEC-TABLES.md §3.3)
// on the Rust leg, which carries none of it today. Every clause below is one
// subtest, and each names the page sentence it implements.
//
// This is a SHAPE gate over the emitted Rust: the batch surface is a generated
// module, so what the emitter writes is what the leg owes. The behavioral half
// of clause 2.1 — every manifest `message` row decoding to the same values its
// form-1 instance does and re-saving byte-for-byte — needs the codec this card
// has not landed yet; it is reported not-done rather than weakened here. The
// RED line this file produced at the step's start is pasted in RESULT.md.
//
// The unit is a `fixed table` root, because §3.3 says the form is a fixed
// table's and nobody else's: "A message is a bitpacked body under a vocabulary
// announced once, so the shape a reader steps through has to be one the
// declaration fixes."
const messageBatchUnit = `package probe

fixed table Ping
{
    seq   uint32
    label string(16)
}
`

// messageBatchRust is all emitted Rust for the probe unit, lower-cased once so
// a spelling test reads the same whether the leg names a verb the packet
// emitter's way or the table emitter's.
func messageBatchRust(t *testing.T) string {
	t.Helper()
	out := generate(t, messageBatchUnit)
	names := make([]string, 0, len(out))
	for name := range out {
		names = append(names, name)
	}
	sort.Strings(names)
	var b strings.Builder
	for _, name := range names {
		b.Write(out[name])
		b.WriteString("\n")
	}
	return strings.ToLower(b.String())
}

// requireMessageForm stops a clause at the reason this card exists when the
// leg emits no message form at all: the surface words the fixed form also
// happens to spell (`malformed`, `256`) must never let a message clause pass
// vacuously.
func requireMessageForm(t *testing.T, all string) {
	t.Helper()
	for _, verb := range []string{"measuremessages", "savemessages", "loadmessages"} {
		if !strings.Contains(all, verb) {
			t.Fatalf("the Rust leg emits no message form: no %s (§3.3 batch surface)", verb)
		}
	}
}

func TestMessageFormBatchRust(t *testing.T) {
	t.Parallel()

	t.Run("surface", func(t *testing.T) {
		t.Parallel()
		// §3.3, The primitive is a BATCH: "SaveMessages takes an array of
		// bodies of ONE root and writes ONE buffer. LoadMessages reads one
		// buffer into the caller's storage and allocates nothing.
		// MeasureMessages sizes the buffer." The names are the card's, in
		// this language's naming convention (SPEC.md §6.1).
		all := messageBatchRust(t)
		requireMessageForm(t, all)
	})

	t.Run("batch_too_large_write", func(t *testing.T) {
		t.Parallel()
		// §3.3: "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME.
		// `MeasureMessages` and `SaveMessages` both refuse and return `-1`...
		// with its refusal verdict set and the reason `batch_too_large` on it."
		all := messageBatchRust(t)
		requireMessageForm(t, all)
		if !strings.Contains(all, "batch_too_large") {
			t.Error("the write side does not name the reason `batch_too_large` (§3.3)")
		}
		if !strings.Contains(all, "256") {
			t.Error("the write side does not carry the wire's `M` bound of 256 (§3.3)")
		}
	})

	t.Run("batch_too_large_read", func(t *testing.T) {
		t.Parallel()
		// §3.3: "`M` ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL
		// BY NAME... The reader sets the returned count to the wire's `M`
		// before it returns the refusal."
		all := messageBatchRust(t)
		requireMessageForm(t, all)
		if !strings.Contains(all, "batch_too_large") {
			t.Error("the read side does not name the reason `batch_too_large` (§3.3)")
		}
	})

	t.Run("no_vocabulary", func(t *testing.T) {
		t.Parallel()
		// §3.3: "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME.
		// Nothing is decoded, no counter moves and `malformed` does not fire."
		all := messageBatchRust(t)
		requireMessageForm(t, all)
		if !strings.Contains(all, "no_vocabulary") {
			t.Error("a body from a peer with no vocabulary is not refused by name (§3.3)")
		}
	})

	t.Run("damage_and_forms", func(t *testing.T) {
		t.Parallel()
		// §3.3: "DAMAGE INSIDE BODY `k` DELIVERS BODIES 1 TO `k - 1`... ONE
		// `malformed` counts"; "BYTES AFTER THE PAD ARE MALFORMED"; and §3's
		// named refusals "newer_form" and "message_form_as_file".
		all := messageBatchRust(t)
		requireMessageForm(t, all)
		for _, word := range []string{"malformed", "newer_form", "message_form_as_file"} {
			if !strings.Contains(all, word) {
				t.Errorf("the message reader does not carry %q (§3.3/§3)", word)
			}
		}
	})
}
