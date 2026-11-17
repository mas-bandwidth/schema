package gotable

import "testing"

func TestGoJSONWriterRefusesZeroByte(t *testing.T) {
	t.Parallel()
	runGeneratedTables(t, `package probe
table Root {
    name string(8)
    labels []string(8)
    keys map[string(8)]int32
}
`, `package probe

import (
	"bytes"
	"testing"
)

func zeroByteRoot(t *testing.T) (*RootBuilder, *Root) {
	t.Helper()
	b := &RootBuilder{}
	if !b.Init() { t.Fatal("builder init") }
	r := b.GetRoot()
	copy(r.Name[:], "ab")
	r.NameLength = 2
	if !RootLabelsAppend(&b.Main, r, "ab") { t.Fatal("append text list element") }
	if RootKeysInsert(&b.Main, &r.Keys, "ab") == nil { t.Fatal("insert map key") }
	if !b.Lock() { t.Fatal("builder lock") }
	return b, b.AsConst()
}

func TestJSONWriterZeroByte(t *testing.T) {
	cases := []struct {
		name string
		set func(*Root) bool
	}{
		{"string", func(v *Root) bool { v.NameLength = 3; copy(v.Name[:], "a\x00b"); return true }},
		{"text list element", func(v *Root) bool { p := v.Labels.At(0); if p == nil { return false }; p.ValueLength = 3; copy(p.Value[:], "a\x00b"); return true }},
		{"map key", func(v *Root) bool { p := v.Keys.At(0); if p == nil { return false }; p.KeyLength = 3; copy(p.Key[:], "a\x00b"); return true }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			b, v := zeroByteRoot(t)
			defer b.Shutdown()
			if !tc.set(v) { t.Fatal("set zero-byte value") }
			if n := RootToJSONMeasure(v); n != -1 { t.Fatalf("ToJSONMeasure = %d, want -1", n) }
			buf := bytes.Repeat([]byte{0xa5}, 256)
			before := bytes.Clone(buf)
			if n := RootToJSON(v, buf); n != -1 { t.Fatalf("ToJSON = %d, want -1", n) }
			if !bytes.Equal(buf, before) { t.Fatal("ToJSON wrote a prefix before refusing") }
			if _, err := RootSaveText(v); err == nil { t.Fatal("SaveText accepted zero-byte text") }
		})
	}

	b, v := zeroByteRoot(t)
	defer b.Shutdown()
	v.NameLength = 3
	copy(v.Name[:], "a\x01b")
	n := RootToJSONMeasure(v)
	if n < 0 { t.Fatalf("ToJSONMeasure refused U+0001: %d", n) }
	text := make([]byte, n)
	if got := RootToJSON(v, text); got != n { t.Fatalf("ToJSON = %d, want %d", got, n) }
	if !bytes.Contains(text, []byte("\\u0001")) { t.Fatalf("U+0001 was not escaped: %s", text) }
	var back RootBuilder
	if !back.Init() { t.Fatal("round-trip builder init") }
	defer back.Shutdown()
	var report TableReport
	if !RootFromJSON(&back, text, &report) || report.Err() != nil { t.Fatalf("FromJSON: %+v", report) }
	if got := back.GetRoot(); got.NameLength != 3 || !bytes.Equal(got.Name[:3], []byte("a\x01b")) { t.Fatalf("round-trip name: %q", got.Name[:got.NameLength]) }
}
`)
	runGeneratedTables(t, `package probe
table Root {
    note *string
    raw *bytes
}
`, `package probe

import (
	"bytes"
	"testing"
)

func TestJSONWriterZeroByteBlob(t *testing.T) {
	t.Parallel()
	b := &RootBuilder{}
	if !b.Init() { t.Fatal("builder init") }
	defer b.Shutdown()
	r := b.GetRoot()
	if TableStringEmplace(&b.Main, &r.Note, []byte("a\x00b")) == nil { t.Fatal("emplace string") }
	if !b.Lock() { t.Fatal("builder lock") }
	v := b.AsConst()
	if n := RootToJSONMeasure(v); n != -1 { t.Fatalf("ToJSONMeasure = %d, want -1", n) }
	buf := bytes.Repeat([]byte{0xa5}, 256)
	before := bytes.Clone(buf)
	if n := RootToJSON(v, buf); n != -1 { t.Fatalf("ToJSON = %d, want -1", n) }
	if !bytes.Equal(buf, before) { t.Fatal("ToJSON wrote a prefix before refusing") }
	if _, err := RootSaveText(v); err == nil { t.Fatal("SaveText accepted a zero byte in a string blob") }

	bytesBuilder := &RootBuilder{}
	if !bytesBuilder.Init() { t.Fatal("bytes builder init") }
	defer bytesBuilder.Shutdown()
	br := bytesBuilder.GetRoot()
	raw := TableBytesEmplace(&bytesBuilder.Main, &br.Raw, 3)
	if raw == nil { t.Fatal("emplace bytes") }
	copy(raw, []byte{0x61, 0x00, 0x62})
	if !bytesBuilder.Lock() { t.Fatal("bytes builder lock") }
	bv := bytesBuilder.AsConst()
	text, err := RootSaveText(bv)
	if err != nil { t.Fatalf("SaveText refused a zero byte in a bytes blob: %v", err) }
	var report TableReport
	loaded, err := RootLoadText(text, &report)
	if err != nil || report.Err() != nil { t.Fatalf("LoadText: %v %+v", err, report) }
	view := TableBytesAt(&loaded.Raw)
	if view.Length != 3 || !bytes.Equal(view.Data, []byte{0x61, 0x00, 0x62}) {
		t.Fatalf("bytes blob round-trip: %q", view.Data)
	}

	one := &RootBuilder{}
	if !one.Init() { t.Fatal("control builder init") }
	defer one.Shutdown()
	or := one.GetRoot()
	if TableStringEmplace(&one.Main, &or.Note, []byte("a\x01b")) == nil { t.Fatal("emplace U+0001") }
	if !one.Lock() { t.Fatal("control lock") }
	ov := one.AsConst()
	n := RootToJSONMeasure(ov)
	if n < 0 { t.Fatalf("ToJSONMeasure refused U+0001 in a string blob: %d", n) }
	out := make([]byte, n)
	if got := RootToJSON(ov, out); got != n { t.Fatalf("ToJSON = %d, want %d", got, n) }
	if !bytes.Contains(out, []byte("\\u0001")) { t.Fatalf("U+0001 was not escaped: %s", out) }
	saved, err := RootSaveText(ov)
	if err != nil { t.Fatalf("SaveText refused U+0001: %v", err) }
	var backReport TableReport
	back, err := RootLoadText(saved, &backReport)
	if err != nil || backReport.Err() != nil { t.Fatalf("LoadText U+0001: %v %+v", err, backReport) }
	gotNote := TableStringAt(&back.Note)
	if gotNote.Length != 3 || !bytes.Equal(gotNote.Data, []byte("a\x01b")) { t.Fatalf("U+0001 round-trip: %q", gotNote.Data) }
	var jsonBack RootBuilder
	if !jsonBack.Init() { t.Fatal("json builder init") }
	defer jsonBack.Shutdown()
	var jsonReport TableReport
	if !RootFromJSON(&jsonBack, out, &jsonReport) || jsonReport.Err() != nil { t.Fatalf("FromJSON: %+v", jsonReport) }
	live := TableStringAt(&jsonBack.GetRoot().Note, &jsonBack.Arena)
	if live.Length != 3 || !bytes.Equal(live.Data, []byte("a\x01b")) { t.Fatalf("FromJSON note: %q", live.Data) }
}
`)
}
