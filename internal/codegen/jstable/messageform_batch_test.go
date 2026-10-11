package jstable

// THE MESSAGE FORM ON THE JAVASCRIPT LEG (docs/SPEC-TABLES.md §3.3): a batch of
// bitpacked bodies under one announced vocabulary, form byte 2. The three verbs
// this card ports — MeasureMessages, SaveMessages, LoadMessages — are the batch
// surface, PLURAL because the primitive is a batch and a single message is the
// batch of one ("a caller with one message passes one and pays a count of eight
// bits for it", §3.3). The bytes are the C++ reference's, pinned by
// testdata/conformance/tables/MANIFEST.txt, and the announcement is the
// compile-time constant ir.TableAnnouncement derives from the unit's closure.
//
// THE RED IS THE ABSENCE: this leg carries form 3 and nothing else (jstable.go
// "Form 1 is still deferred to #516"), so no table-wire body codec exists to
// port the form-2 batch onto, and GenerateLineage emits no message surface.
// Each clause below fails on that absence before it can test a value.

import (
	"fmt"
	"maps"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/js"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// messageSource generates the backenddemo unit (the §3.3 corpus) and returns
// the whole emitted JavaScript as one string, so a clause can assert the
// surface the form owes without knowing which module carries it.
func messageSource(t *testing.T) string {
	t.Helper()
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	files, err := js.Generate(u)
	if err != nil {
		t.Fatalf("packet emitter: %v", err)
	}
	tables, err := GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("table emitter: %v", err)
	}
	maps.Copy(files, tables)
	var b strings.Builder
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		fmt.Fprintf(&b, "// --- %s ---\n%s\n", name, data)
	}
	return b.String()
}

// announcementHex is the compile-time constant announcement for the unit, as
// the emitter must carry it (docs/SPEC-TABLES.md §3.3: "the whole announcement
// is a compile-time constant of the unit ... so a backend emits Announce and
// AnnounceMeasure as a constant byte array and its length").
func announcementHex(t *testing.T) string {
	t.Helper()
	u := loadUnit(t, "../../../tables/backend/Backend.schema")
	return fmt.Sprintf("%x", ir.TableAnnouncement(u))
}

func TestMessageFormBatchJavaScript(t *testing.T) {
	t.Parallel()

	// 2.1 "every manifest `message` row: LoadMessages of the batch gives the
	// values the form-1 instance decodes to, and SaveMessages gives the batch
	// bytes back exactly; MeasureMessages equals the length" — the round trip
	// the message row pins (MANIFEST.txt "What the pair pins is a ROUND TRIP:
	// loading either form and saving the other reproduces the other's pinned
	// bytes"). The surface is three verbs over the announced vocabulary, and the
	// announcement is the unit's constant.
	t.Run("batch_round_trip_surface", func(t *testing.T) {
		t.Parallel()
		src := messageSource(t)
		announce := announcementHex(t)
		for _, verb := range []string{
			"LoginRequestMeasureMessages",
			"LoginRequestSaveMessages",
			"LoginRequestLoadMessages",
			"TableVocabulary",
			"AnnounceRead",
			"AnnounceMeasure",
			"TableMessageBatchMax",
		} {
			if !strings.Contains(src, verb) {
				t.Fatalf("the message form is absent from the JavaScript leg: no %s in the generated unit (§3.3 \"The three suffixes MeasureMessages, SaveMessages and LoadMessages\")", verb)
			}
		}
		if !strings.Contains(src, announce) {
			t.Fatalf("the generated unit does not carry the compile-time announcement %s (§3.3 \"the whole announcement is a compile-time constant of the unit\")", announce)
		}
	})

	// 2.2 "no_vocabulary; batch_too_large on the write side (M above 256, -1
	// with the reason) and the read side (M above capacity, refused before
	// decoding, count set to M)" — the batch's five answers, each a refusal by
	// name carried on the TableReport each verb takes (§3.3 "The batch surface's
	// five answers").
	t.Run("refusal_reasons", func(t *testing.T) {
		t.Parallel()
		src := messageSource(t)
		for _, reason := range []string{
			"no_vocabulary",
			"batch_too_large",
		} {
			if !strings.Contains(src, reason) {
				t.Fatalf("the message form is absent from the JavaScript leg: no %q refusal reason (§3.3 \"The refusal reason values no_vocabulary ... batch_too_large ...\")", reason)
			}
		}
		if !strings.Contains(src, "TableMessageBatchMax = 256") {
			t.Fatalf("no TableMessageBatchMax = 256 in the generated unit (§3.3 \"256 IS A WIRE CONSTANT OF THIS FORM\")")
		}
	})

	// 2.3 "damage inside body k delivers k-1 with one malformed; bytes after
	// the pad are malformed; newer_form, and message_form_as_file from the file
	// reader" — damage is terminal for the batch, and the form byte is read
	// first so an unknown form is a refusal by name, never damage (§3.3). The
	// batch reader is the one verb whose whole job is these three refusals:
	// LoadMessages reads the form byte before any body and walks bodies until
	// the pad, so a damaged body k and a trailing byte are its malformed lanes.
	t.Run("damage_and_form_byte", func(t *testing.T) {
		t.Parallel()
		src := messageSource(t)
		if !strings.Contains(src, "LoginRequestLoadMessages") {
			t.Fatalf("the message form is absent from the JavaScript leg: no LoginRequestLoadMessages, the batch reader whose job is the three refusals of this clause (§3.3 \"damage ... is TERMINAL for the batch\" and \"the form byte is a whole byte and is read FIRST\")")
		}
	})
}
