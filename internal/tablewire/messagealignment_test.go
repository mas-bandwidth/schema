package tablewire_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// Payloads use their actual bit position in the batch. Nested text and byte
// arrays expose a misplaced pad.
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

// A nested body must be encoded once, even when elision is decided from its
// output. Allocation growth distinguishes a linear walk from recursive
// probe-and-rewrite without relying on wall-clock timing.
func TestMessageNestedEncodingWork(t *testing.T) {
	allocations := func(depth int) float64 {
		var schema strings.Builder
		schema.WriteString("package probe\ntype Leaf { text string(12) }\n")
		child := "Leaf"
		for i := range depth {
			name := fmt.Sprintf("Level%d", i)
			fmt.Fprintf(&schema, "type %s { child %s }\n", name, child)
			child = name
		}
		fmt.Fprintf(&schema, "table Root { child %s }\n", child)
		m := listModel(t, schema.String())
		value := place(t, m, "Root", strings.Repeat(`{"child":`, depth+1)+`{"text":"abc"}`+strings.Repeat("}", depth+1))
		return testing.AllocsPerRun(2, func() {
			wire, err := tablewire.EncodeMessages(m, []*tabletext.Instance{value})
			if err != nil || len(wire) == 0 {
				t.Fatalf("encode: %v", err)
			}
		})
	}
	shallow, deep := allocations(6), allocations(12)
	if shallow <= 0 || deep > 4*shallow {
		t.Fatalf("doubling nesting grew allocations from %.0f to %.0f", shallow, deep)
	}
}

// A string aligns against the enclosing stream, including the body count and
// all earlier fields. Elision probes at bit zero cannot supply its final bits.
func TestMessageNestedAlignment(t *testing.T) {
	m := listModel(t, `package alignment
 enum Key { A, B }
 table Child { text string(16) }
 table Root { lead bool
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
 table Child { number int32 }
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
 table Other { number int32 }
 table Root { head *Other }
 `)
	for _, tc := range []struct{ kind, text string }{{"Child", `{"head":{"number":7}}`}, {"bytes", `{"head":"YWJj"}`}, {"string", `{"head":"abc"}`}} {
		t.Run(tc.kind, func(t *testing.T) {
			sender := listModel(t, `package nodekeep
 table Child { number int32 }
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
