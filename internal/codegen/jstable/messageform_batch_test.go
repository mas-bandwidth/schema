package jstable

// TestMessageFormBatchJavaScript pins the JavaScript leg against the MESSAGE
// FORM, form byte `2` (docs/SPEC-TABLES.md §3.3): "FORM BYTE `2` IS THE MESSAGE
// FORM, and form byte `1` does not move." §3.3's batch surface is the three
// verbs the card names — "`SaveMessages` takes an array of bodies of ONE root
// and writes ONE buffer. `LoadMessages` reads one buffer into the caller's
// storage and allocates nothing. `MeasureMessages` sizes the buffer." — over a
// fixed-table root, "THE FORM IS A FIXED TABLE'S, and that is a declaration,
// not a derivation (§2.2)."
//
// THE RED REASON. This leg emits form 3 (docs/SPEC-TABLES.md §3.4) and the two
// accelerators only. Its emitter says so itself: jstable.go's GenerateLineage
// carries "Form 1 is still deferred to #516 and nothing here reads or writes
// one." §3.3's body "IS BITPACKED, which is to say it is a bit stream carrying
// references at the width the vocabulary needs and values at the widths their
// declarations state", and the form rides "the vocabulary the previous card
// emitted"; with no table-wire body codec and no announcement in this leg,
// `MeasureMessages`, `SaveMessages` and `LoadMessages` are emitted nowhere, so
// every clause below fails at its first assertion. See RESULT.md.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// jsMessageRow is one `instance message_*` line of
// testdata/conformance/tables/MANIFEST.txt: the unit, the root table and the
// form-1 wire file whose values a form-2 batch must decode to.
type jsMessageRow struct {
	name string
	root string
	wire string
}

// jsMessageRows reads every manifest `message` row for the messagedemo unit
// (docs/SPEC-TABLES.md §16): "instance <name> <unit> <root> <wire file>", the
// batch this form's `LoadMessages` must reproduce.
func jsMessageRows(t *testing.T) []jsMessageRow {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "conformance", "tables", "MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	var rows []jsMessageRow
	for _, line := range strings.Split(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) < 5 || f[0] != "instance" {
			continue
		}
		if !strings.HasPrefix(f[1], "message_") || f[2] != "messagedemo" {
			continue
		}
		rows = append(rows, jsMessageRow{name: f[1], root: f[3], wire: f[4]})
	}
	if len(rows) == 0 {
		t.Fatal("the manifest carries message rows for messagedemo")
	}
	return rows
}

// jsMessageUnit loads the messagedemo corpus (tables/messages/Messages.schema)
// the manifest's message rows name.
func jsMessageUnit(t *testing.T) *ir.Unit {
	t.Helper()
	return loadUnit(t, filepath.Join("..", "..", "..", "tables", "messages", "Messages.schema"))
}

// jsMessageEmitted joins every file this leg emits for the unit, so a surface
// name is looked for across the whole module set rather than one guessed file
// name.
func jsMessageEmitted(t *testing.T, u *ir.Unit) string {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var b strings.Builder
	for name, data := range files {
		b.WriteString("// " + name + "\n")
		b.Write(data)
	}
	return b.String()
}

// jsMessageVerb asserts one of §3.3's three verbs is emitted for a fixed-table
// root: "`SaveMessages` takes an array of bodies of ONE root and writes ONE
// buffer. `LoadMessages` reads one buffer into the caller's storage and
// allocates nothing. `MeasureMessages` sizes the buffer."
func jsMessageVerb(t *testing.T, emitted, verb, clause string) {
	t.Helper()
	if !strings.Contains(emitted, verb) {
		t.Fatalf("%s: the message form's `%s` is emitted nowhere — the JS leg carries no table-wire body codec (jstable.go: \"Form 1 is still deferred to #516 and nothing here reads or writes one\")", clause, verb)
	}
}

// TestMessageFormBatchJavaScript is one subtest per clause of the card's STEP 2.
func TestMessageFormBatchJavaScript(t *testing.T) {
	t.Parallel()
	u := jsMessageUnit(t)
	emitted := jsMessageEmitted(t, u)

	// 2.1 every manifest `message` row: LoadMessages of the batch gives the
	// values the form-1 instance decodes to, and SaveMessages gives the batch
	// bytes back exactly; MeasureMessages equals the length.
	t.Run("2.1_manifest_message_rows", func(t *testing.T) {
		t.Parallel()
		rows := jsMessageRows(t)
		for _, row := range rows {
			// §3.3: "FORM BYTE `2` IS THE MESSAGE FORM" over "a fixed table's"
			// root; the three verbs are the batch's whole surface.
			jsMessageVerb(t, emitted, "LoadMessages", row.name)
			jsMessageVerb(t, emitted, "SaveMessages", row.name)
			jsMessageVerb(t, emitted, "MeasureMessages", row.name)
		}
	})

	// 2.2 no_vocabulary; batch_too_large on the write side (M above 256, -1 with
	// the reason) and the read side (M above capacity, refused before decoding,
	// count set to M).
	t.Run("2.2_no_vocabulary_and_batch_too_large", func(t *testing.T) {
		t.Parallel()
		// §3.3: "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME.
		// Nothing is decoded, no counter moves and `malformed` does not fire."
		if !strings.Contains(emitted, "no_vocabulary") {
			t.Fatalf("no_vocabulary: the announcement's refusal is emitted nowhere")
		}
		// §3.3: "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME." ...
		// "The refusal reason is `batch_too_large` on both sides".
		if !strings.Contains(emitted, "batch_too_large") {
			t.Fatalf("batch_too_large: the write and read refusals are emitted nowhere")
		}
		jsMessageVerb(t, emitted, "MeasureMessages", "batch_too_large")
		jsMessageVerb(t, emitted, "SaveMessages", "batch_too_large")
		jsMessageVerb(t, emitted, "LoadMessages", "batch_too_large")
	})

	// 2.3 damage inside body k delivers k-1 with one `malformed`; bytes after the
	// pad are `malformed`; newer_form, and message_form_as_file from the file
	// reader.
	t.Run("2.3_damage_pad_newer_form_and_file_reader", func(t *testing.T) {
		t.Parallel()
		// §3.3: "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1`." ...
		// "BYTES AFTER THE PAD ARE MALFORMED".
		jsMessageVerb(t, emitted, "LoadMessages", "damage")
		// §3.3 and §3.1/§4: a form byte the reader does not know is a refusal
		// by name, and a message handed to the file reader is refused.
		if !strings.Contains(emitted, "newer_form") {
			t.Fatalf("newer_form: the form-byte refusal is emitted nowhere")
		}
		if !strings.Contains(emitted, "message_form_as_file") {
			t.Fatalf("message_form_as_file: the file reader's refusal is emitted nowhere")
		}
	})
}
