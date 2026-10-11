package darttable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormBatchDart pins the MESSAGE FORM on the Dart leg: form byte 2,
// a batch of bitpacked bodies under one announced vocabulary
// (docs/SPEC-TABLES.md §3.3). The surface is MeasureMessages, SaveMessages and
// LoadMessages over the vocabulary the form emits for a `fixed table` root.
//
// RED FIRST. The Dart leg carries none of the form-2 wire today: the table wire
// itself is deferred to schema#514 (darttable.go's package note), and Generate
// emits no file spelling any of the three batch verbs. Each clause below fails
// at this revision for that reason, and the row it names is the manifest
// `message` row the reader contract owes (testdata/conformance/tables/
// MANIFEST.txt).
func TestMessageFormBatchDart(t *testing.T) {
	t.Parallel()
	u := loadUnit(t, "../../../tables/messages/Messages.schema")

	t.Run("2.1 manifest message row round trips", func(t *testing.T) {
		t.Parallel()
		joined := generatedDart(t, u)
		// §3.3: "`SaveMessages` takes an array of bodies of ONE root and writes
		// ONE buffer. `LoadMessages` reads one buffer into the caller's storage
		// and allocates nothing. `MeasureMessages` sizes the buffer."
		for _, verb := range []string{"MeasureMessages", "SaveMessages", "LoadMessages"} {
			if !strings.Contains(joined, verb) {
				t.Fatalf("the Dart leg emits no %s: a `fixed table` root's batch verbs are absent from every generated file (docs/SPEC-TABLES.md §3.3)", verb)
			}
		}
	})

	t.Run("2.2 no vocabulary and batch too large", func(t *testing.T) {
		t.Parallel()
		joined := generatedDart(t, u)
		// §3.3: "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ...
		// with its refusal verdict set and the reason `batch_too_large` on it"
		// and "`M` ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY
		// NAME ... The reader sets the returned count to the wire's `M` before
		// it returns the refusal."
		for _, token := range []string{"LoadMessages", "batchTooLarge"} {
			if !strings.Contains(joined, token) {
				t.Fatalf("the Dart leg emits no %s: §3.3's no-vocabulary and batch_too_large refusals ride the message path and are not emitted (docs/SPEC-TABLES.md §3.3)", token)
			}
		}
	})

	t.Run("2.3 damage newer form and message form as file", func(t *testing.T) {
		t.Parallel()
		joined := generatedDart(t, u)
		// §3.3: "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1` ... ONE
		// `malformed` counts" and "BYTES AFTER THE PAD ARE MALFORMED" and the
		// form byte's own `newer_form` / `message_form_as_file` refusals.
		for _, token := range []string{"LoadMessages"} {
			if !strings.Contains(joined, token) {
				t.Fatalf("the Dart leg emits no %s: §3.3's damage, newer_form and message_form_as_file verdicts ride the message reader and are not emitted (docs/SPEC-TABLES.md §3.3)", token)
			}
		}
	})
}

// generatedDart returns every generated file of the unit joined, so a clause
// asserts on the surface this backend emits rather than on one file's name.
func generatedDart(t *testing.T, u *ir.Unit) string {
	t.Helper()
	out, err := Generate(u)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}
	var b strings.Builder
	for name, src := range out {
		b.WriteString(name)
		b.WriteByte('\n')
		b.Write(src)
		b.WriteByte('\n')
	}
	return b.String()
}
