package tablewire_test

import (
	"bytes"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A string aligns against the enclosing stream, including the body count and
// all earlier fields. Elision probes at bit zero cannot supply its final bits.
func TestMessageNestedAlignment(t *testing.T) {
	m := listModel(t, `package alignment
 enum Key { A, B }
 fixed table Child { text string(16) }
 fixed table Root { lead bool
 child Child
 keyed [Key]Child
 tail uint32 }
 `)
	for _, text := range []string{
		`{"child":{"text":"abc"},"tail":7}`,
		`{"lead":true,"child":{"text":"nested"},"tail":9}`,
		`{"keyed":{"A":{"text":"first"},"B":{"text":"second"}},"tail":11}`,
		`{"lead":true,"child":{"text":"nested"},"keyed":{"A":{"text":"first"},"B":{"text":"second"}},"tail":13}`,
	} {
		t.Run(text, func(t *testing.T) {
			source := place(t, m, "Root", text)
			batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{source, source})
			if err != nil {
				t.Fatal(err)
			}
			vocabulary := &tablewire.Vocabulary{}
			var report tabletext.Report
			if err := vocabulary.AnnounceRead(tablewire.Announce(m.Unit), &report); err != nil {
				t.Fatal(err)
			}
			values := []*tabletext.Instance{m.New(m.Lookup("Root")), m.New(m.Lookup("Root"))}
			if n, ok, err := tablewire.DecodeMessages(m, values, batch, vocabulary, &report); n != 2 || !ok || err != nil || !report.Silent() {
				t.Fatalf("read %d %v %v %+v", n, ok, err, report)
			}
			want, err := tablewire.Encode(m, source)
			if err != nil {
				t.Fatal(err)
			}
			for _, v := range values {
				got, err := tablewire.Encode(m, v)
				if err != nil || !bytes.Equal(got, want) {
					t.Fatalf("nested payload lost: %v", err)
				}
			}
		})
	}
}
