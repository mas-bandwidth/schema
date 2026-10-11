package jstable

import (
	"strings"
	"testing"
)

// TestMessageFormBatchJavaScript pins §3.3's form-2 batch surface in the
// JavaScript leg: MeasureMessages, SaveMessages and LoadMessages over the
// vocabulary the fixed table's announcement carries. It is RED for the reason
// the previous attempt reported and this one starts from: this backend carries
// no table-wire body codec (jstable.go: "Table wire codecs are deferred to
// #516", "Form 1 is still deferred to #516 and nothing here reads or writes
// one"), so the form-2 batch surface has no bitpacked-body machinery to port
// onto. Each clause asserts the §3.3 sentence it implements; it turns green
// the day the message form lands.

// messageProbeSchema is a fixed table whose closure exercises the vocabulary's
// kinds: a ranged integer, a keyed array, a union and a string. The message
// form is a FIXED table's (docs/SPEC-TABLES.md §3.3): `fixed table Root` may
// ride here, a plain `table` may not.
const messageProbeSchema = `package probe
enum Grade
{
    Gold
    Silver
}

union Choice
{
    number uint32
    pair [2]int16
    ping
}

fixed table Root
{
    yes bool
    small int16 | min = -5, max = 14
    raw uint64
    text string(15)
    items [..3]int32
    grades [Grade]uint8
    choice Choice
}
`

// messageSurfaceNames is §6.1's three verbs plus the announcement the batch
// resolves against, the surface §3.3 names: "SaveMessages takes an array of
// bodies of ONE root and writes ONE buffer. LoadMessages reads one buffer into
// the caller's storage and allocates nothing. MeasureMessages sizes the
// buffer." The announcement ("THE ANNOUNCEMENT IS AN ORDINARY FORM-1 FILE")
// carries the vocabulary every one of the three verbs reads through.
var messageSurfaceNames = []string{
	"MeasureMessages",
	"SaveMessages",
	"LoadMessages",
	"AnnounceRead",
}

// messageGeneratedJS generates the unit's JavaScript and concatenates every
// module, so a surface scan sees the whole leg's output for the unit.
func messageGeneratedJS(t *testing.T) string {
	t.Helper()
	u := unitFrom(t, messageProbeSchema)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var b strings.Builder
	for _, data := range files {
		b.Write(data)
	}
	return b.String()
}

func TestMessageFormBatchJavaScript(t *testing.T) {
	t.Run("round trip", func(t *testing.T) {
		t.Parallel()
		// §3.3 "THE PRIMITIVE IS A BATCH": SaveMessages writes one buffer,
		// LoadMessages reads it back, MeasureMessages sizes it. The surface is
		// what the three verbs ride on, and it is absent while the form is.
		src := messageGeneratedJS(t)
		for _, name := range messageSurfaceNames {
			if !strings.Contains(src, name) {
				t.Fatalf("the JS leg emits no %s: §3.3's form-2 batch surface has no bitpacked-body machinery to port onto (jstable.go: \"Form 1 is still deferred to #516\")", name)
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		// §3.3 "M ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME" and "M
		// ABOVE THE CALLER'S CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME",
		// reason batch_too_large on both sides; and "A BODY FROM A PEER THAT
		// NEVER ANNOUNCED IS REFUSED BY NAME", reason no_vocabulary.
		src := messageGeneratedJS(t)
		for _, reason := range []string{"batch_too_large", "no_vocabulary"} {
			if !strings.Contains(src, reason) {
				t.Fatalf("the JS leg emits no refusal reason %s: §3.3's named refusals have no batch surface to answer on (jstable.go: \"Form 1 is still deferred to #516\")", reason)
			}
		}
	})

	t.Run("damage", func(t *testing.T) {
		t.Parallel()
		// §3.3 "DAMAGE INSIDE BODY k DELIVERS BODIES 1 TO k - 1" with one
		// malformed, "BYTES AFTER THE PAD ARE MALFORMED", and §3's form byte
		// answered by name — newer_form, and message_form_as_file from the
		// file reader. None of these has a reader to land in while the form is
		// absent.
		src := messageGeneratedJS(t)
		for _, name := range []string{"newer_form", "message_form_as_file"} {
			if !strings.Contains(src, name) {
				t.Fatalf("the JS leg emits no refusal name %s: §3.3's damage and form-byte rules have no message reader to land in (jstable.go: \"Form 1 is still deferred to #516\")", name)
			}
		}
	})
}
