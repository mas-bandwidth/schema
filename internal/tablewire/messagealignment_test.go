package tablewire_test

import (
	"bytes"
	"strings"
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

func TestRetainMessageNodeFields(t *testing.T) {
	old := `package nodekeep
 fixed table Child { number int32 }
 table Root { head *Child
 alias *Child }
 `
	newer := strings.Replace(old, "number int32", "number int32\n extra string(16)", 1)
	sender, reader := listModel(t, newer), listModel(t, old)
	source := place(t, sender, "Root", `{"head":{"&node":1,"number":7,"extra":"kept"},"alias":{"&node":1}}`)
	batch, err := tablewire.EncodeMessages(sender, []*tabletext.Instance{source})
	if err != nil {
		t.Fatal(err)
	}
	vocabulary := &tablewire.Vocabulary{}
	var report tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(sender.Unit), &report); err != nil {
		t.Fatal(err)
	}
	value := reader.New(reader.Lookup("Root"))
	store := &tablewire.Retain{Capacity: 1024, IdCapacity: 32}
	if n, ok, err := tablewire.DecodeRetainMessages(reader, []*tabletext.Instance{value}, batch, vocabulary, []*tablewire.Retain{store}, &report); n != 1 || !ok || err != nil || report.Malformed || report.Unknown != 1 || report.Retained != 1 || report.RetainLost != 0 {
		t.Fatalf("node retain %d %v %v %+v", n, ok, err, report)
	}
	got, err := tablewire.EncodeRetain(reader, value, store, &report)
	if err != nil {
		t.Fatal(err)
	}
	want, err := tablewire.Encode(sender, source)
	if err != nil || !bytes.Equal(got, want) || report.RetainLost != 0 {
		t.Fatalf("node rewrite %v %+v", err, report)
	}
}

func TestRetainMessageUnplaceableNodes(t *testing.T) {
	reader := listModel(t, `package nodekeep
 fixed table Other { number int32 }
 table Root { head *Other }
 `)
	for _, tc := range []struct{ kind, text string }{{"Child", `{"head":{"number":7}}`}, {"bytes", `{"head":"YWJj"}`}, {"string", `{"head":"abc"}`}} {
		t.Run(tc.kind, func(t *testing.T) {
			sender := listModel(t, `package nodekeep
 fixed table Child { number int32 }
 table Root { head *`+tc.kind+` }
 `)
			source := place(t, sender, "Root", tc.text)
			batch, err := tablewire.EncodeMessages(sender, []*tabletext.Instance{source})
			if err != nil {
				t.Fatal(err)
			}
			vocabulary := &tablewire.Vocabulary{}
			var report tabletext.Report
			if err := vocabulary.AnnounceRead(tablewire.Announce(sender.Unit), &report); err != nil {
				t.Fatal(err)
			}
			value := reader.New(reader.Lookup("Root"))
			store := &tablewire.Retain{Capacity: 1024, IdCapacity: 32}
			if n, ok, err := tablewire.DecodeRetainMessages(reader, []*tabletext.Instance{value}, batch, vocabulary, []*tablewire.Retain{store}, &report); n != 1 || !ok || err != nil || report.Malformed || report.Unknown != 1 || report.Retained != 0 || report.RetainLost != 1 {
				t.Fatalf("unplaceable node %d %v %v %+v", n, ok, err, report)
			}
		})
	}
}
