package javatable

// The MESSAGE FORM (form byte 2) is a fixed table's on the wire
// (docs/SPEC-TABLES.md §3.3): a batch of bitpacked bodies under one announced
// vocabulary, sized by MeasureMessages, written by SaveMessages and read back
// by LoadMessages (§6.1 names). The Java table emitter carries none of it
// today (javatable.go: "The TABLE WIRE itself — measure, save, load ... is not
// emitted for Java"), so every clause below fails at its first assertion.
//
// This is the red-first checkpoint for the Java leg; the tests are what the
// port must satisfy, not a proof that it is done.

import (
	"strings"
	"testing"
)

func messageFormBatchSurface(t *testing.T) string {
	t.Helper()
	files, err := Generate(unitFrom(t, `package probe

fixed table Probe
{
    a int32
    b string(8)
}
`))
	if err != nil {
		t.Fatal(err)
	}
	var all strings.Builder
	for _, data := range files {
		all.Write(data)
	}
	return all.String()
}

func TestMessageFormBatchJava(t *testing.T) {
	t.Parallel()
	gen := messageFormBatchSurface(t)

	// "SaveMessages takes an array of bodies of ONE root and writes ONE buffer.
	//  LoadMessages reads one buffer into the caller's storage ... MeasureMessages
	//  sizes the buffer." (docs/SPEC-TABLES.md §3.5, the MESSAGE FORM)
	t.Run("2.1_batch_roundtrip", func(t *testing.T) {
		t.Parallel()
		for _, name := range []string{"MeasureMessages", "SaveMessages", "LoadMessages"} {
			if !strings.Contains(gen, name) {
				t.Fatalf("generated Java lacks %s: a fixed table's batch surface is MeasureMessages, SaveMessages, LoadMessages over the announced vocabulary (§3.3)", name)
			}
		}
	})

	// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... LoadMessages
	//  compares it [the count] with the capacity ... and refuses before any
	//  body." (docs/SPEC-TABLES.md §3.5)
	t.Run("2.2_refusals", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "batch_too_large") {
			t.Fatal("generated Java never names batch_too_large: M above 256 on the write side and M above capacity on the read side are refusals by name (§3.5)")
		}
		if !strings.Contains(gen, "no_vocabulary") {
			t.Fatal("generated Java never names no_vocabulary: a batch read without a resolved vocabulary is refused by name (§3.3)")
		}
	})

	// "damage inside body k delivers k-1 ... bytes after the pad are malformed;
	//  newer_form, and message_form_as_file from the file reader."
	// (docs/SPEC-TABLES.md §3.3)
	t.Run("2.3_damage", func(t *testing.T) {
		t.Parallel()
		if !strings.Contains(gen, "malformed") {
			t.Fatal("generated Java never reports malformed: damage inside a body and bytes after the pad are malformed (§3.3)")
		}
		if !strings.Contains(gen, "newer_form") {
			t.Fatal("generated Java never reports newer_form for a form byte the leg does not carry (§3.3)")
		}
		if !strings.Contains(gen, "message_form_as_file") {
			t.Fatal("generated Java never reports message_form_as_file when the file reader meets a message-form buffer (§3.3)")
		}
	})
}
