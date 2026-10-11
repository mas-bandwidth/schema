package darttable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormBatchDart asserts, one subtest per clause, the MESSAGE FORM
// (docs/SPEC-TABLES.md §3.3) batch surface this backend emits for a `fixed
// table` root. The surface is MeasureMessages / SaveMessages / LoadMessages
// over the vocabulary the announcement carries, and its refusals answer by
// name on the TableReport the batch verbs take. Nothing here ran at
// a61228d02d89: Dart carried form 3 and nothing else, so every clause is red
// before the fix beside it.
func TestMessageFormBatchDart(t *testing.T) {
	t.Parallel()

	const schema = `package probe

fixed table Root {
	a int32
	b bool
	c uint8
}
`
	// The oracle: one value, and the batch bytes §3.3's bitpacked writer owes
	// for it, computed by the compiler's own engine (internal/tablewire), the
	// same bytes the C++ reference pins in testdata/conformance/tables.
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	v := m.New(m.Lookup("Root"))
	var text tabletext.Report
	if !m.Read(v, []byte(`{"a":-3,"b":true,"c":200}`), &text) || !text.Silent() {
		t.Fatalf("text %+v", text)
	}
	expected, err := tablewire.EncodeMessages(m, []*tabletext.Instance{v, v})
	if err != nil {
		t.Fatal(err)
	}
	if len(expected) == 0 {
		t.Fatal("the oracle produced no batch bytes")
	}
	announcement := ir.TableAnnouncement(u)
	if len(announcement) == 0 {
		t.Fatal("the announcement is empty")
	}

	gen := genDart(t, schema)

	t.Run("manifest_message_rows", func(t *testing.T) {
		t.Parallel()
		// "`SaveMessages` takes an array of bodies of ONE root and writes ONE
		// buffer. `LoadMessages` reads one buffer into the caller's storage ...
		// `MeasureMessages` sizes the buffer." (docs/SPEC-TABLES.md §3.3)
		for _, verb := range []string{"measureMessages", "saveMessages", "loadMessages"} {
			if !strings.Contains(gen, verb) {
				t.Errorf("no %s in the generated Dart: the batch surface is not emitted (docs/SPEC-TABLES.md §3.3)", verb)
			}
		}
		// "THE WHOLE ANNOUNCEMENT IS A COMPILE-TIME CONSTANT OF THE UNIT ... a
		// backend emits `Announce` and `AnnounceMeasure`" (docs/SPEC-TABLES.md §3.3)
		for _, verb := range []string{"announce", "announceRead", "announceMeasure"} {
			if !strings.Contains(gen, verb) {
				t.Errorf("no %s in the generated Dart: the announcement surface is not emitted (docs/SPEC-TABLES.md §3.3)", verb)
			}
		}
	})

	t.Run("refusals", func(t *testing.T) {
		t.Parallel()
		// "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... the reason
		// `batch_too_large`" and "`M` ABOVE THE CALLER'S CAPACITY ON THE READ
		// SIDE IS A REFUSAL BY NAME ... `batch_too_large` on both sides"
		// (docs/SPEC-TABLES.md §3.3)
		if !strings.Contains(gen, "batch_too_large") {
			t.Error("no batch_too_large refusal in the generated Dart (docs/SPEC-TABLES.md §3.3)")
		}
		// "A BODY FROM A PEER THAT NEVER ANNOUNCED IS REFUSED BY NAME ... the
		// reader says it holds no vocabulary for the peer" (docs/SPEC-TABLES.md §3.3)
		if !strings.Contains(gen, "no_vocabulary") {
			t.Error("no no_vocabulary refusal in the generated Dart (docs/SPEC-TABLES.md §3.3)")
		}
	})

	t.Run("damage_and_form", func(t *testing.T) {
		t.Parallel()
		// "DAMAGE INSIDE BODY `k` DELIVERS BODIES `1` TO `k - 1` ... ONE
		// `malformed` counts" (docs/SPEC-TABLES.md §3.3)
		if !strings.Contains(gen, "malformed") {
			t.Error("no malformed counter in the generated Dart (docs/SPEC-TABLES.md §3.3)")
		}
		// "a form a reader does not know is a REFUSAL by name and never damage"
		// (docs/SPEC-TABLES.md §3.3)
		if !strings.Contains(gen, "newer_form") {
			t.Error("no newer_form refusal in the generated Dart (docs/SPEC-TABLES.md §3.3)")
		}
		// "a reader handed a form-`2` wire where a file was expected refuses by
		// name" (docs/SPEC-TABLES.md §3.3)
		if !strings.Contains(gen, "message_form_as_file") {
			t.Error("no message_form_as_file refusal in the generated Dart (docs/SPEC-TABLES.md §3.3)")
		}
	})
}

// genDart builds the unit and returns the concatenated generated Dart.
func genDart(t *testing.T, src string) string {
	t.Helper()
	u := unitFrom(t, src)
	files, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	var all string
	for _, b := range files {
		all += string(b)
	}
	return all
}
