// A text list's TOTAL on the read (docs/SPEC-TABLES.md §2.13): the engine
// keeps the longest prefix whose byte lengths sum within the reader's
// `| total = N`, drops the first element that would carry the sum past it and
// every element after, and counts `clamped` once, naming the field. The
// writer declares no total, which is how a wire past the reader's total
// exists at all. `make tables-wire-text-total-negative-control` removes the
// clamp and requires this test to go red.
package tablewire_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// totalUnit is one text list in each of the four spellings a total applies
// to, under the declaration a total is added to.
func totalUnit(total, bound string) string {
	return fmt.Sprintf(`package save

table Save
{
    names [..%[2]s]string(16)%[1]s
    maybe [..%[2]s]?string(16)%[1]s
    repos []string(16)%[1]s
    bases []?string(16)%[1]s
}
`, total, bound)
}

// texts reads one list field of a decoded Save back as its elements, an
// absent element of a `?` list spelled "-".
func texts(inst *tabletext.Instance, field int) []string {
	fv := &inst.Fields[field]
	n := len(fv.Elems)
	if fv.Def.Array == ir.ArrayCounted {
		n = fv.Count // a bounded list's live count; its slots run to the bound
	}
	out := make([]string, 0, n)
	for i := 0; i < n; i++ {
		rec := fv.Elems[i].Tab
		if rec == nil || (rec.Fields[0].Def.Type.Optional && !rec.Fields[0].Present) {
			out = append(out, "-")
			continue
		}
		out = append(out, string(rec.Fields[0].Cell.Str))
	}
	return out
}

func TestTextTotalClampsTheReadToAPrefix(t *testing.T) {
	const wrote = `{"names":["abcde","fghij","k","lmno"],` +
		`"maybe":["abcde",null,"fghij","k"],` +
		`"repos":["abcde","fghij","k","lmno"],` +
		`"bases":[null,"abcde","fghij","","k"]}`
	writer := listModel(t, totalUnit("", "8"))
	wire, err := tablewire.Encode(writer, place(t, writer, "Save", wrote))
	if err != nil {
		t.Fatal(err)
	}

	for _, c := range []struct {
		name, total, bound string
		clamped            int
		want               [4]string
	}{
		// within the total: every element kept, in silence
		{"within", " | total = 16", "8", 0, [4]string{"abcde fghij k lmno", "abcde - fghij k", "abcde fghij k lmno", "- abcde fghij  k"}},
		// past it: the prefix within 10, "k" dropped although it would fit
		// after "lmno" went, and ONE clamped per field
		{"past", " | total = 10", "8", 4, [4]string{"abcde fghij", "abcde - fghij", "abcde fghij", "- abcde fghij "}},
		// a count clamped against [..3] first, then the total: both counted
		// on names; maybe's three kept elements sum to 10, so only its count
		// clamps
		{"count and total", " | total = 10", "3", 5, [4]string{"abcde fghij", "abcde - fghij", "abcde fghij", "- abcde fghij "}},
	} {
		t.Run(c.name, func(t *testing.T) {
			m := listModel(t, totalUnit(c.total, c.bound))
			back := m.New(m.Lookup("Save"))
			var r tabletext.Report
			ok, err := tablewire.Decode(m, back, wire, &r)
			if err != nil || !ok {
				t.Fatalf("decode: ok=%v err=%v", ok, err)
			}
			if r.Clamped != c.clamped || r.Malformed || r.Unknown != 0 || r.KindMismatch != 0 {
				t.Errorf("report %+v, want clamped %d and nothing else", r, c.clamped)
			}
			if c.clamped > 0 && r.Reason != "clamped: Save.names" {
				t.Errorf("reason %q, want the first clamped field named, clamped: Save.names", r.Reason)
			}
			for i, want := range c.want {
				if got := strings.Join(texts(back, i), " "); got != want {
					t.Errorf("%s kept %q, want %q", back.Fields[i].Def.Name, got, want)
				}
			}
		})
	}
}
