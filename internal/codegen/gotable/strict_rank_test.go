package gotable

import (
	"bytes"
	"fmt"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// goRankSchema: Root read from OldRoot's bytes lacks its kid's required x and
// meets its flag after kid; Outer read from OldOuter's bytes meets its inner's
// flag before Outer's terminator, where it lacks its required a. A flag's
// bool byte set to 2 is damage (docs/SPEC-TABLES.md §3, §4).
const goRankSchema = `package probe
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

// goRankWire is root's wire for text from the reference engine; damaged sets
// the one bool flag the text carries true, the last field of its body, to 2.
func goRankWire(t *testing.T, m *tabletext.Model, root, text string, damaged bool) []byte {
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

// TestGoStrictReadRanksDamageFirst pins docs/SPEC-TABLES.md §6.5's ranking on
// the generated Go reader: damage outranks a missing required field, because
// damage discards the whole load (§4), and the walk reads on past a body that
// lacks a required field, so damage after it is still met. Both orders answer
// wire_damaged through ReadStrict and the plain Load's report.Err(); a missing
// field alone answers TableMissingRequired.
func TestGoStrictReadRanksDamageFirst(t *testing.T) {
	t.Parallel()
	m := tabletext.NewModel(unitOf(t, goRankSchema))
	missingOnly := goRankWire(t, m, "OldRoot", `{"kid":{"y":2},"flag":true}`, false)
	damageOnly := goRankWire(t, m, "Root", `{"kid":{"x":1,"y":2},"flag":true}`, true)
	missingThenDamage := goRankWire(t, m, "OldRoot", `{"kid":{"y":2},"flag":true}`, true)
	damageThenMissing := goRankWire(t, m, "OldOuter", `{"inner":{"flag":true}}`, true)
	body := fmt.Sprintf(`package probe
import ("errors"; "testing")
func TestRank(t *testing.T) {
	for _, c := range []struct{ name string; outer bool; wire []byte; missing bool }{
		{"missing_only", false, %#v, true},
		{"damage_only", false, %#v, false},
		{"missing_then_damage", false, %#v, false},
		{"damage_then_missing", true, %#v, false},
	} {
		var err error
		if c.outer { _, err = OuterReadStrict(c.wire) } else { _, err = RootReadStrict(c.wire) }
		var m TableMissingRequired
		if c.missing {
			if !errors.As(err, &m) { t.Errorf("%%s: ReadStrict error %%v, want TableMissingRequired", c.name, err) }
			continue
		}
		if !errors.Is(err, TableRefuseWireDamaged) { t.Errorf("%%s: ReadStrict error %%v, want wire_damaged", c.name, err) }
	}
}
`, missingOnly, damageOnly, missingThenDamage, damageThenMissing)
	runGenerated(t, goRankSchema, body)
}
