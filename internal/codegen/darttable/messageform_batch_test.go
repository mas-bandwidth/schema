// THE MESSAGE FORM'S BATCH SURFACE (docs/SPEC-TABLES.md §3.3), form byte 2,
// in Dart: one subtest per clause of the card, red first. The Dart leg carries
// none of this surface today — the fixed form (§3.4) is the only table wire it
// has — so every clause fails for the stated reason until the port lands.
//
// The three verbs §3.3 names — MeasureMessages, SaveMessages, LoadMessages —
// spell measureMessages, saveMessages, loadMessages in Dart's lowerCamelCase
// convention (SPEC.md §6.1, dartName), over the vocabulary a fixed-table root's
// unit announces once (docs/SPEC-TABLES.md §3.3, THE SCOPE IS THE ANNOUNCEMENT).
package darttable

import (
	"sort"
	"strings"
	"testing"
)

// messageSurface generates the Dart for the message corpus unit
// (tables/messages, the fixed-table root ToolMessage of the manifest's
// message rows) and returns every emitted file's text joined, so an assertion
// on a surface name searches the whole unit rather than one file whose name the
// port would otherwise fix.
func messageSurface(t *testing.T) string {
	t.Helper()
	u := loadUnit(t, "../../../tables/messages/Messages.schema")
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate the message unit: %v", err)
	}
	names := make([]string, 0, len(files))
	for k := range files {
		names = append(names, k)
	}
	sort.Strings(names)
	var all strings.Builder
	for _, k := range names {
		all.Write(files[k])
	}
	return all.String()
}

func TestMessageFormBatchDart(t *testing.T) {
	t.Parallel()

	t.Run("manifest_message_rows_round_trip", func(t *testing.T) {
		t.Parallel()
		// §3.3: "SaveMessages takes an array of bodies of ONE root and writes
		// ONE buffer. LoadMessages reads one buffer into the caller's storage
		// and allocates nothing. MeasureMessages sizes the buffer."
		all := messageSurface(t)
		for _, verb := range []string{"measureMessages", "saveMessages", "loadMessages"} {
			if !strings.Contains(all, verb) {
				t.Fatalf("the Dart leg carries none of the form-2 batch surface: no %q is emitted for the message unit's fixed-table root, so LoadMessages cannot decode the batch, SaveMessages cannot write it and MeasureMessages cannot size it (docs/SPEC-TABLES.md §3.3)", verb)
			}
		}
	})

	t.Run("no_vocabulary_and_batch_too_large", func(t *testing.T) {
		t.Parallel()
		// §3.3: "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME";
		// "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... the reason
		// batch_too_large"; "M ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS
		// A REFUSAL BY NAME ... the reader sets the returned count to the
		// wire's M before it returns the refusal."
		all := messageSurface(t)
		for _, name := range []string{"TableVocabulary", "announceRead", "tableAnnouncement"} {
			if !strings.Contains(all, name) {
				t.Fatalf("the Dart leg carries none of the form-2 batch surface: no %q is emitted, so no_vocabulary and batch_too_large have no carrier to refuse by name (docs/SPEC-TABLES.md §3.3)", name)
			}
		}
	})

	t.Run("damage_and_form_refusals", func(t *testing.T) {
		t.Parallel()
		// §3.3: "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1";
		// "BYTES AFTER THE PAD ARE MALFORMED"; the form byte is read FIRST, so
		// an unknown form is newer_form and form 2 handed to a file reader is
		// message_form_as_file (docs/SPEC-TABLES.md §3, §3.3).
		all := messageSurface(t)
		if !strings.Contains(all, "loadMessages") {
			t.Fatalf("the Dart leg carries none of the form-2 batch surface: no %q is emitted, so damage inside body k cannot deliver k - 1, bytes after the pad cannot be malformed, and newer_form / message_form_as_file have no reader to answer (docs/SPEC-TABLES.md §3.3)", "loadMessages")
		}
	})
}
