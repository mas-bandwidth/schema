package tablewire_test

import (
	"bytes"
	"encoding/base64"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A VARIABLE-FORM bytes(N) FIELD'S L IS ITS ARRAY BODY'S LENGTH
// (docs/SPEC-TABLES.md §3): the element-kind byte, the LEB128 count, then the
// count's bytes. From a count of 128 the count takes two LEB bytes, so an L
// computed as 2 + count comes out one short and the next field reads garbage.
// Every length round-trips with the field that follows it intact, and a
// re-save is byte-identical.
func TestBytesFieldFramingAcrossLebWidths(t *testing.T) {
	m := listModel(t, `package bytesdemo
 table Holder {
 blob bytes(300)
 after int32
 }`)
	for _, n := range []int{0, 1, 126, 127, 128, 129, 200, 300} {
		raw := make([]byte, n)
		for i := range raw {
			raw[i] = byte(i*7 + 1)
		}
		js := fmt.Sprintf(`{"blob":"%s","after":7}`, base64.StdEncoding.EncodeToString(raw))
		inst := place(t, m, "Holder", js)
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("n=%d encode: %v", n, err)
		}
		back := m.New(m.Lookup("Holder"))
		var rep tabletext.Report
		if _, err := tablewire.Decode(m, back, wire, &rep); err != nil {
			t.Fatalf("n=%d decode: %v", n, err)
		}
		if rep.Malformed || rep.Unknown != 0 || rep.KindMismatch != 0 {
			t.Errorf("n=%d decode report: malformed=%v unknown=%d kindmismatch=%d", n, rep.Malformed, rep.Unknown, rep.KindMismatch)
		}
		var got []byte
		var after int64
		for i := range back.Fields {
			switch back.Fields[i].Def.Name {
			case "blob":
				got = back.Fields[i].Cell.Str
			case "after":
				after = back.Fields[i].Cell.I
			}
		}
		if !bytes.Equal(got, raw) {
			t.Errorf("n=%d blob came back with %d bytes, want %d", n, len(got), n)
		}
		if after != 7 {
			t.Errorf("n=%d the following field read %d, want 7", n, after)
		}
		again, err := tablewire.Encode(m, back)
		if err != nil {
			t.Fatalf("n=%d re-encode: %v", n, err)
		}
		if !bytes.Equal(again, wire) {
			t.Errorf("n=%d re-save differs from the first save", n)
		}
	}
}
