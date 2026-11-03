package tablewire_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// rankSchema: Root read from OldRoot's bytes lacks its kid's required x and
// meets its flag after kid; Outer read from OldOuter's bytes meets its inner's
// flag before Outer's terminator, where it lacks its required a. A flag's
// bool byte set to 2 is damage (docs/SPEC-TABLES.md §3, §4).
const rankSchema = `package probe
table Kid { x int32 | required
 y int32 }
table OldKid { y int32 }
table Root { kid Kid
 flag bool }
table OldRoot { kid OldKid
 flag bool }
table Inner { flag bool }
table Outer { inner Inner
 a int32 | required }
table OldOuter { inner Inner }
`

// rankEncode is root's wire for text; damaged sets the one bool flag the text
// carries true, the last field of its body (reference, kind 1, payload 1,
// then the body's terminator), to the byte 2.
func rankEncode(t *testing.T, m *tabletext.Model, root, text string, damaged bool) []byte {
	t.Helper()
	inst := m.New(m.Lookup(root))
	var report tabletext.Report
	if !m.Read(inst, []byte(text), &report) {
		t.Fatalf("tabletext refused %s: %+v", text, report)
	}
	wire, err := tablewire.Encode(m, inst)
	if err != nil {
		t.Fatalf("encode %s: %v", text, err)
	}
	if !damaged {
		return wire
	}
	at := -1
	for i := 2; i+1 < len(wire); i++ {
		if wire[i-1] == 1 && wire[i] == 1 && wire[i+1] == 0 {
			if at >= 0 {
				t.Fatalf("%s: more than one bool field ending a body: %x", text, wire)
			}
			at = i
		}
	}
	if at < 0 {
		t.Fatalf("%s: no bool field ending a body: %x", text, wire)
	}
	out := bytes.Clone(wire)
	out[at] = 2
	return out
}

// TestDecodeRanksDamageFirst pins docs/SPEC-TABLES.md §6.5's ranking on the
// reference engine: damage outranks a missing required field, because damage
// discards the whole load (§4), and the walk reads on past a body that lacks
// a required field, so damage after it is still met. Both orders answer
// damage; a missing field alone answers MissingRequired.
func TestDecodeRanksDamageFirst(t *testing.T) {
	m := listModel(t, rankSchema)
	for _, tc := range []struct {
		name, root, wireRoot, text string
		damaged                    bool
		want                       string
	}{
		{"missing_only", "Root", "OldRoot", `{"kid":{"y":2},"flag":true}`, false, "missing_required"},
		{"damage_only", "Root", "Root", `{"kid":{"x":1,"y":2},"flag":true}`, true, "wire_damaged"},
		{"missing_then_damage", "Root", "OldRoot", `{"kid":{"y":2},"flag":true}`, true, "wire_damaged"},
		{"damage_then_missing", "Outer", "OldOuter", `{"inner":{"flag":true}}`, true, "wire_damaged"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			wire := rankEncode(t, m, tc.wireRoot, tc.text, tc.damaged)
			var r tabletext.Report
			ok, err := tablewire.Decode(m, m.New(m.Lookup(tc.root)), wire, &r)
			var missing *tabletext.MissingRequired
			got := "ok"
			switch {
			case errors.As(err, &missing):
				got = "missing_required"
			case !ok && r.Malformed:
				got = "wire_damaged"
			case err != nil || !ok:
				got = "refused"
			}
			if got != tc.want {
				t.Fatalf("Decode answered %s (ok=%v err=%v report=%+v), want %s", got, ok, err, r, tc.want)
			}
		})
	}
}
