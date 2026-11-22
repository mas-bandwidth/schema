package mootable_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// THE WRITER REFUSES AN ID TABLE ITS READERS REFUSE (docs/SPEC-TABLES.md §3,
// ruling R8), for moo, which refuses in every build. The unit is
// tablewire.IdCapSchema. Wires the Go engine wrote for 4095 and 4096 ids are
// read strictly and saved back: both are written, with the count word in the
// trailer, and the saved file reads strictly again. Then the 4096-id value has
// its `y` set, which needs 4097: save refuses id_table_over_bound and
// measure answers nil.
func TestMooWriterRefusesMoreThan4096Ids(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table runtime)")
	t.Parallel()
	u := unitFromSource(t, tablewire.IdCapSchema())
	var keys []string
	for i := 0; i < tablewire.IdCapVariants; i++ {
		keys = append(keys, fmt.Sprintf("\"V%d\":1", i))
	}
	text := func(x int) string { return fmt.Sprintf(`{"keys":{%s},"x":%d}`, strings.Join(keys, ","), x) }
	files, err := generate(u)
	if err != nil {
		t.Fatal(err)
	}
	g := writeUnit(t, u, files)
	prog := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal save = require(\"./table_save\")\nlocal St = require(\"./schema_tables\")\n" +
		g.requireAll() + `local d = St.lookup("Root")
local function row(name: string, wire: buffer, sety: boolean)
	local v = load.ReadStrict("Root", wire, load.newReport())
	if v == nil then print(name .. " load refused") return end
	if sety then v.Y = 1 end
	local out, why = save.save(d, v)
	local n = save.measure(d, v)
	if out == nil then
		print(name .. " refused " .. tostring(why ~= nil and why.name) .. " measure " .. tostring(n))
		return
	end
	local count = buffer.readu32(out, buffer.len(out) - 8)
	local again = load.ReadStrict("Root", out, load.newReport())
	print(name .. " wrote " .. count .. " " .. tostring(again ~= nil) .. " " .. tostring(n == buffer.len(out)))
end
row("a", inputs.a, false)
row("b", inputs.b, false)
row("by", inputs.b, true)
`
	out := g.run(t, prog, map[string][]byte{
		"a": goOracleWire(t, u, "Root", text(0)),
		"b": goOracleWire(t, u, "Root", text(1)),
	})
	want := "a wrote 4095 true true\nb wrote 4096 true true\nby refused id_table_over_bound measure nil"
	if strings.TrimSpace(out) != want {
		t.Fatalf("id cap rows:\n%s\nwant:\n%s", out, want)
	}
}
