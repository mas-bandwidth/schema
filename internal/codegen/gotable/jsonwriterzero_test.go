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
}
