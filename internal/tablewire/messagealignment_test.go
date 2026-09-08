package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Elision probes start at zero; the payload must instead use its actual bit
// position in the batch. Nested text and byte arrays expose a misplaced pad.
func TestMessageNestedPayloadAlignment(t *testing.T) {
	m := listModel(t, `package probe
 enum Key { First, Second }
 type Item { text string(12)
 data [..3]uint8 }
 table Root { item Item
 keyed [Key]Item }
 `)
	for caseIndex, text := range []string{`{"item":{"text":"abc","data":[7]}}`, `{"keyed":{"First":{"text":"abc","data":[7]},"Second":{"data":[2,3]}}}`, `{"item":{"text":"abc","data":[7]},"keyed":{"First":{"text":"abc","data":[7]},"Second":{"data":[2,3]}}}`} {
		value := place(t, m, "Root", text)
		wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{value, value})
		if err != nil {
			t.Fatal(err)
		}
		// Independently generated C++ output for the combined case.
		if caseIndex == 2 && hex.EncodeToString(wire) != "02011303616263120740560c616263120760220203001303616263120740560c61626312076022020300" {
			t.Fatalf("C++ wire differs: %x", wire)
		}
		var vocabulary tablewire.Vocabulary
		var report tabletext.Report
		if err := vocabulary.AnnounceRead(tablewire.Announce(m.Unit), &report); err != nil {
			t.Fatal(err)
		}
		decoded := []*tabletext.Instance{m.New(m.Lookup("Root")), m.New(m.Lookup("Root"))}
		n, ok, err := tablewire.DecodeMessages(m, decoded, wire, &vocabulary, &report)
		if err != nil || !ok || n != 2 || !report.Silent() {
			t.Fatalf("%s: count=%d ok=%v err=%v report=%+v", text, n, ok, err, report)
		}
		want, err := m.Write(value)
		if err != nil {
			t.Fatal(err)
		}
		for _, v := range decoded {
			got, err := m.Write(v)
			if err != nil || !bytes.Equal(got, want) {
				t.Fatalf("got %s want %s: %v", got, want, err)
			}
		}
	}
}
