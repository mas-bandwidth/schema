package luatable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// A LAWFUL SPARSE ENUM-KEYED MESSAGE KEEPS OMITTED SLOT DEFAULTS
// (docs/SPEC-TABLES.md §3.3): keyed message bodies carry only the riding
// entries. LoadMessages therefore starts from the generated value's declared
// defaults before applying those entries, so its result remains a complete
// fixed/keyed array that the ordinary SaveMessages API accepts.
func TestMessageEnumKeyedArrayKeepsOmittedDefaults(t *testing.T) {
	t.Parallel()
	unit := mqUnit(t, "KeyedDefaults", `package keyeddefaults

enum Slot { Alpha, Beta, Gamma }

fixed table Root
{
    scores [Slot]int32
}
`)
	model := tabletext.NewModel(unit)
	inst := mqInstance(t, unit, `{"scores":{"Beta":22}}`)
	batch, err := tablewire.EncodeMessages(model, []*tabletext.Instance{inst})
	if err != nil {
		t.Fatal(err)
	}

	probe := `
local T = require("` + ModuleName(unit) + `")
local inputs = require("inputs")
local report = {}
local vocab = {}
assert(T.AnnounceRead(vocab, inputs.announce, report), "announce")
local values = assert(T.LoadMessages("Root", vocab, inputs.batch, report), "load")
assert(#values == 1, "count")
assert(values[1].scores[1] == 0, "Alpha default")
assert(values[1].scores[2] == 22, "Beta value")
assert(values[1].scores[3] == 0, "Gamma default")
assert(T.SaveMessages("Root", vocab, values), "save")
print("keyed-defaults-ok")
`
	got := strings.TrimSpace(mqaProbe(t, unit, tablewire.Announce(unit), batch, probe))
	if got != "keyed-defaults-ok" {
		t.Fatalf("ordinary keyed message round trip = %q", got)
	}
}
