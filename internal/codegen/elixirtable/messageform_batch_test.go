package elixirtable

// THE MESSAGE FORM (form byte 2) FOR THE ELIXIR LEG (docs/SPEC-TABLES.md §3.3).
// A batch of fixed tables under ONE ANNOUNCED VOCABULARY, each body packed
// through the type's own codec; the surface is MeasureMessages, SaveMessages and
// LoadMessages over the previous card's vocabulary (SPEC.md §6.1).
//
// RED FIRST. This card ports a surface the leg does not carry today, so every
// clause below fails at the step's start because the emitted module names none
// of the three functions: the leg writes no message form.

import (
	"strings"
	"testing"
)

const messageBatchSchema = `package probe

fixed table Ping
{
    id      uint32
    weight  uint16
}
`

func messageSurface(t *testing.T) string {
	t.Helper()
	u := unitFrom(t, messageBatchSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var b strings.Builder
	for name, src := range files {
		b.WriteString("-- " + name + "\n")
		b.Write(src)
		b.WriteByte('\n')
	}
	return b.String()
}

// §3.3: "a BATCH of fixed tables under ONE ANNOUNCED BLOCK". Clause 2.1:
// SaveMessages gives the batch bytes back exactly and MeasureMessages equals the
// length; the emitted surface must name all three.
func TestMessageFormBatchElixir(t *testing.T) {
	t.Parallel()

	t.Run("2.1_batch_roundtrip", func(t *testing.T) {
		t.Parallel()
		out := messageSurface(t)
		for _, fn := range []string{"MeasureMessages", "SaveMessages", "LoadMessages"} {
			if !strings.Contains(out, fn) {
				t.Fatalf("the Elixir leg emits no message form: %s is absent (§3.3)", fn)
			}
		}
	})

	// §3.3: the write side refuses M above 256 with -1 and the reason
	// batch_too_large; the read side refuses M above capacity before decoding.
	t.Run("2.2_refusals", func(t *testing.T) {
		t.Parallel()
		out := messageSurface(t)
		if !strings.Contains(out, "MeasureMessages") {
			t.Fatalf("the Elixir leg emits no message form: MeasureMessages is absent (§3.3)")
		}
		if !strings.Contains(out, "batch_too_large") {
			t.Fatalf("the Elixir leg names no batch_too_large refusal (§3.3)")
		}
	})

	// §3.3: damage inside body k delivers the bodies before it with one
	// malformed; bytes after the pad are malformed; newer_form and
	// message_form_as_file are named refusals.
	t.Run("2.3_damage_and_refusals", func(t *testing.T) {
		t.Parallel()
		out := messageSurface(t)
		if !strings.Contains(out, "MeasureMessages") {
			t.Fatalf("the Elixir leg emits no message form: MeasureMessages is absent (§3.3)")
		}
		for _, reason := range []string{"malformed", "newer_form", "message_form_as_file"} {
			if !strings.Contains(out, reason) {
				t.Fatalf("the Elixir leg names no %s (§3.3)", reason)
			}
		}
	})
}
