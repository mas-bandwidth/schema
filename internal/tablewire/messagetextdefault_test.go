package tablewire

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// TestMessageStringDefaultElidedOnlyWhenEqual holds the message form's text
// elision to its rule (docs/SPEC-TABLES.md §3.3): a string or bytes field is
// left out only when its length AND its bytes are the declared default's, and
// a reader gives the declared default for a field left out. The empty value
// under a non-empty default rides and reads back empty; a value of the
// default's length that differs from it rides; the default itself, and the
// empty value under no declared default, are elided.
func TestMessageStringDefaultElidedOnlyWhenEqual(t *testing.T) {
	t.Parallel()
	m := covModel(t, `package textdefault

fixed table Probe
{
    s string(8) = "abc"
    b bytes(4) = "ab"
    e string(8)
    eb bytes(4)
}
`)
	st := m.Lookup("Probe")
	if st == nil || len(st.Fields) != 4 {
		t.Fatalf("Probe: %v", st)
	}
	v := &Vocabulary{entries: MessageVocabulary(m.Unit), announced: true}
	defaults := map[int]string{0: "abc", 1: "ab", 2: "", 3: ""}

	base, err := EncodeMessage(m, m.New(st))
	if err != nil {
		t.Fatalf("save of the defaults: %v", err)
	}
	// A body at its defaults carries no field: the defaults' bytes are never
	// written, so the comparison below is against the elided body.
	for _, d := range []string{"abc", "ab"} {
		if bytes.Contains(base, []byte(d)) {
			t.Fatalf("the default %q was written, not elided: %x", d, base)
		}
	}

	cases := []struct {
		field  int
		value  string
		elided bool
	}{
		{0, "abc", true},
		{0, "", false},
		{0, "xyz", false},
		{1, "ab", true},
		{1, "", false},
		{1, "zz", false},
		{2, "", true},
		{2, "xyz", false},
		{3, "", true},
		{3, "zz", false},
	}
	for _, tc := range cases {
		name := fmt.Sprintf("%s=%q", st.Fields[tc.field].Name, tc.value)
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			inst := m.New(st)
			inst.Fields[tc.field].Cell.Str = []byte(tc.value)
			inst.Fields[tc.field].Count = len(tc.value)
			wire, err := EncodeMessage(m, inst)
			if err != nil {
				t.Fatalf("save: %v", err)
			}
			if elided := bytes.Equal(wire, base); elided != tc.elided {
				t.Fatalf("elided=%v, want %v: wire %x, defaults %x", elided, tc.elided, wire, base)
			}

			// The reader's storage holds other values first, so a field left
			// out must be set to its declared default, not kept.
			got := m.New(st)
			for i := range got.Fields {
				got.Fields[i].Cell.Str = []byte("q")
				got.Fields[i].Count = 1
			}
			var report tabletext.Report
			ok, err := DecodeMessage(m, got, wire, v, &report)
			if !ok || err != nil || report.Refused {
				t.Fatalf("load refused: ok=%v err=%v report=%+v wire=%x", ok, err, report, wire)
			}
			for i, fv := range got.Fields {
				want := defaults[i]
				if i == tc.field {
					want = tc.value
				}
				if string(fv.Cell.Str) != want || fv.Count != len(want) {
					t.Errorf("%s read %q (count %d), want %q", st.Fields[i].Name, fv.Cell.Str, fv.Count, want)
				}
			}
		})
	}
}
