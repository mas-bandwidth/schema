package jstable

import (
	"encoding/hex"
	"encoding/json"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// TestJsSaveElidesACountedArrayAtItsBirth holds the JavaScript variable Save to
// the engine's bytes (internal/tablewire encodeArray) for a counted array whose
// Min is above 0: a value holds such an array at its BIRTH — Min elements, each
// at its default — and the writer elides it exactly as it elides any field at
// its default (docs/SPEC-TABLES.md §3). An array at Min with one element off
// its default, and an array past Min, ride. Every expected wire is the
// engine's encoding of the same text.
func TestJsSaveElidesACountedArrayAtItsBirth(t *testing.T) {
	t.Parallel()
	const schema = `package probe

enum Tone { Low, Mid, High }

fixed table Rec
{
    a int32
    b uint8
}

table Bounds
{
    octets [2..8]uint8
    shorts [1..5]int16
    recs   [1..4]Rec
    tones  []Tone
}
`
	u := unitFrom(t, schema)
	m := tabletext.NewModel(u)
	type row struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Wire string `json:"wire"`
	}
	var rows []row
	for _, c := range []struct{ name, text string }{
		{"all-at-birth", `{}`},
		{"octets-off-default", `{"octets": [7, 8]}`},
		{"octets-past-min", `{"octets": [0, 0, 0]}`},
		{"shorts-off-default", `{"shorts": [5]}`},
		{"recs-off-default", `{"recs": [{"a": 1}]}`},
		{"recs-past-min", `{"recs": [{}, {}]}`},
	} {
		inst := m.New(m.Lookup("Bounds"))
		var rep tabletext.Report
		if !m.Read(inst, []byte(c.text), &rep) {
			t.Fatalf("%s: the engine refused %s: %+v", c.name, c.text, rep)
		}
		wire, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: %v", c.name, err)
		}
		rows = append(rows, row{Name: c.name, Text: c.text, Wire: hex.EncodeToString(wire)})
	}
	data, err := json.Marshal(rows)
	if err != nil {
		t.Fatal(err)
	}
	jsDiscardRun(t, u, map[string][]byte{"rows.json": data}, `
const hex = (b) => Array.from(b, (x) => x.toString(16).padStart(2, "0")).join("");
for (const row of JSON.parse(readFileSync("rows.json", "utf8"))) {
  const r = new m.TableVariableReport();
  const v = m.BoundsFromJson(row.text, r);
  if (v === null) { fail(row.name + ": FromJson refused " + row.text); continue; }
  const saved = m.BoundsSave(v);
  if (saved === null) { fail(row.name + ": Save refused"); continue; }
  if (hex(saved) !== row.wire) { fail(row.name + ": saved " + hex(saved) + ", the engine wrote " + row.wire); }
  // the engine's bytes load back to the same value: an elided array is born
  // at Min, and saving it elides it again
  const bytes = Uint8Array.from(row.wire.match(/../g).map((x) => parseInt(x, 16)));
  const lr = new m.TableVariableReport();
  const loaded = m.BoundsLoad(bytes, lr);
  if (loaded === null || lr.err() !== null) { fail(row.name + ": Load refused the engine's bytes " + JSON.stringify(lr)); continue; }
  if (m.BoundsToJson(loaded) !== m.BoundsToJson(v)) { fail(row.name + ": loaded " + m.BoundsToJson(loaded) + ", read " + m.BoundsToJson(v)); }
  const again = m.BoundsSave(loaded);
  if (again === null || hex(again) !== row.wire) { fail(row.name + ": the loaded value saved " + (again === null ? "null" : hex(again))); }
}
`)
}
