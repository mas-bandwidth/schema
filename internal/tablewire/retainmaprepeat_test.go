package tablewire_test

import (
	"bytes"
	"os"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

func TestRetainedMapReplacementAfterDamage(t *testing.T) {
	m := listModel(t, `package mapdemo
 table Text {
 names map[string(8)]string(16)
 wide map[uint16]wstring(6)
 blobs map[int32]bytes(10)
 after int32
 }`)
	wire, err := os.ReadFile("../../testdata/wire/tables/fuzz-vectors/map_retained_replaced.bin")
	if err != nil {
		t.Fatal(err)
	}
	// The third field is a compatible replacement of names. Without it, the
	// earlier key damage has orphaned one retained body and the save loses it.
	if len(wire) != 168 || wire[66] != 1 || wire[67] != 14 || wire[68] != 36 {
		t.Fatal("replacement vector changed")
	}
	without := append(append([]byte(nil), wire[:66]...), wire[105:]...)
	for _, tc := range []struct {
		name string
		wire []byte
		lost int
	}{{"replacement", wire, 0}, {"damage only", without, 1}} {
		t.Run(tc.name, func(t *testing.T) {
			value := m.New(m.Lookup("Text"))
			keep := &tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var read, save tabletext.Report
			ok, err := tablewire.DecodeRetain(m, value, tc.wire, keep, &read)
			if !ok || err != nil || read.Retained != 2 || read.RetainLost != 0 {
				t.Fatalf("load: %v %v %+v", ok, err, read)
			}
			out, err := tablewire.EncodeRetain(m, value, keep, &save)
			if err != nil || len(out) == 0 || save.RetainLost != tc.lost {
				t.Fatalf("save: %v %+v, want lost %d", err, save, tc.lost)
			}
		})
	}
}

// An oversized map key drops the whole entry before its value is decoded.
// It cannot alias a shortened key or capture unknown fields under that key.
func TestRetainedMessageMapReplacementAfterDroppedKey(t *testing.T) {
	sender := listModel(t, `package mapkeep
 fixed table Item { number int32
 extra int32 }
 table Root { names map[string(8)]Item }
 `)
	reader := listModel(t, `package mapkeep
 fixed table Item { number int32 }
 table Root { names map[string(2)]Item }
 `)
	vocabulary := &tablewire.Vocabulary{}
	var announced tabletext.Report
	if err := vocabulary.AnnounceRead(tablewire.Announce(sender.Unit), &announced); err != nil {
		t.Fatal(err)
	}
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "dropped key", true: "replacement"}[replaced], func(t *testing.T) {
			expected := reader.New(reader.Lookup("Root"))
			source := place(t, sender, "Root", `{"names":{"long":{"number":1,"extra":7}}}`)
			if replaced {
				next := place(t, sender, "Root", `{"names":{"ok":{"number":2}}}`)
				// Repeat the same field definition in the encoder's value to spell two
				// legal occurrences without changing bit alignment by hand.
				source.Fields = append(source.Fields, next.Fields[0])
				expected = place(t, reader, "Root", `{"names":{"ok":{"number":2}}}`)
			}
			batch, err := tablewire.EncodeMessages(sender, []*tabletext.Instance{source})
			if err != nil {
				t.Fatal(err)
			}
			value := reader.New(reader.Lookup("Root"))
			store := &tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var read, save tabletext.Report
			n, ok, err := tablewire.DecodeRetainMessages(reader, []*tabletext.Instance{value}, batch, vocabulary, []*tablewire.Retain{store}, &read)
			if n != 1 || !ok || err != nil || read.Malformed || read.Unknown != 0 || read.Retained != 0 || read.RetainLost != 0 || read.Clamped != 1 {
				t.Fatalf("load: %d %v %v %+v", n, ok, err, read)
			}
			out, err := tablewire.EncodeRetain(reader, value, store, &save)
			want, wantErr := tablewire.Encode(reader, expected)
			if wantErr != nil {
				t.Fatal(wantErr)
			}
			lost := 0
			if err != nil || !bytes.Equal(out, want) || save.RetainLost != lost {
				t.Fatalf("save: %v %+v, want lost %d", err, save, lost)
			}
		})
	}
}

func TestRetainedMessageMapReplacementAfterKeyMismatch(t *testing.T) {
	sender := listModel(t, `package mapkeep
 fixed table Item { number int32
 extra int32 }
 table Root { names map[string(8)]Item }
 `)
	reader := listModel(t, `package mapkeep
 fixed table Item { number int32 }
 table Root { names map[int32]Item }
 `)
	vocabulary := &tablewire.Vocabulary{}
	if err := vocabulary.AnnounceRead(tablewire.Announce(sender.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	for _, replaced := range []bool{false, true} {
		t.Run(map[bool]string{false: "key mismatch", true: "replacement"}[replaced], func(t *testing.T) {
			// The first entry elides its default key and lands, capturing extra.
			// The second carries an incompatible key and empties the entire map.
			source := place(t, sender, "Root", `{"names":{"":{"extra":7},"long":{"number":1}}}`)
			expected := reader.New(reader.Lookup("Root"))
			if replaced {
				next := place(t, sender, "Root", `{"names":{"":{"number":2}}}`)
				source.Fields = append(source.Fields, next.Fields[0])
				expected = place(t, reader, "Root", `{"names":{"0":{"number":2}}}`)
			}
			batch, err := tablewire.EncodeMessages(sender, []*tabletext.Instance{source})
			if err != nil {
				t.Fatal(err)
			}
			value := reader.New(reader.Lookup("Root"))
			store := &tablewire.Retain{Capacity: 8192, IdCapacity: 1024}
			var read, save tabletext.Report
			n, ok, err := tablewire.DecodeRetainMessages(reader, []*tabletext.Instance{value}, batch, vocabulary, []*tablewire.Retain{store}, &read)
			if n != 1 || !ok || err != nil || read.Malformed || read.Unknown != 1 || read.Retained != 1 || read.RetainLost != 0 || read.KindMismatch != 1 {
				t.Fatalf("load: %d %v %v %+v", n, ok, err, read)
			}
			out, err := tablewire.EncodeRetain(reader, value, store, &save)
			want, wantErr := tablewire.Encode(reader, expected)
			if wantErr != nil {
				t.Fatal(wantErr)
			}
			lost := 1
			if replaced {
				lost = 0
			}
			if err != nil || !bytes.Equal(out, want) || save.RetainLost != lost {
				t.Fatalf("save: %v %+v, want lost %d, bytes %x want %x", err, save, lost, out, want)
			}
		})
	}
}

func TestMessageMapRepeatedKeyKeepsWidening(t *testing.T) {
	m := listModel(t, `package mapkeep
 table Root { names map[uint32]int32
 narrow map[uint8]int32 }
 `)
	source := place(t, m, "Root", `{"names":{"2":7},"narrow":{"1":0}}`)
	entry := source.Fields[0].Entries[0].Tab
	key := source.Fields[1].Entries[0].Tab.Fields[0]
	entry.Fields = append([]tabletext.Field{key}, entry.Fields...)
	source.Fields[1].Entries = nil
	source.Fields[1].Count = 0
	batch, err := tablewire.EncodeMessages(m, []*tabletext.Instance{source})
	if err != nil {
		t.Fatal(err)
	}
	vocabulary := &tablewire.Vocabulary{}
	if err := vocabulary.AnnounceRead(tablewire.Announce(m.Unit), &tabletext.Report{}); err != nil {
		t.Fatal(err)
	}
	value := m.New(m.Lookup("Root"))
	var report tabletext.Report
	n, ok, err := tablewire.DecodeMessages(m, []*tabletext.Instance{value}, batch, vocabulary, &report)
	if n != 1 || !ok || err != nil || report.Malformed || report.Widened != 1 {
		t.Fatalf("load: %d %v %v %+v", n, ok, err, report)
	}
	expected := place(t, m, "Root", `{"names":{"2":7}}`)
	got, err := tablewire.Encode(m, value)
	if err != nil {
		t.Fatal(err)
	}
	want, err := tablewire.Encode(m, expected)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("got %x want %x", got, want)
	}
}
