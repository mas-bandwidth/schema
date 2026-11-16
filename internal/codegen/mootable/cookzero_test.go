package mootable_test

import (
	"encoding/hex"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// cookZeroProbe loads inputs.wire through table_load.moo (table_fixed.moo for
// a fixed-form root), then fills every
// dead slot of the root's counted arrays with the first live element (a
// buffer's with 0xff), so the value's storage past each count is not zero,
// and cooks the value through table_cookwrite.moo in both byte orders,
// printing the little-endian file's hex and the big-endian file's hex. A
// refusal prints "refuse <name> <field>".
func cookZeroProbe(g *generatedUnit, rootName string) string {
	var b strings.Builder
	b.WriteString("--!strict\n" + moorunHex)
	b.WriteString(g.requireAll())
	b.WriteString("local inputs = require(\"./inputs\")\nlocal load = require(\"./table_load\")\nlocal F = require(\"./table_fixed\")\nlocal St = require(\"./schema_tables\")\nlocal W = require(\"./table_cookwrite\")\n")
	fmt.Fprintf(&b, `local d = St.lookup(%[1]q)
local report = load.newReport()
local value: any
if d.fixed then
	local vals = F.load(d, F.lookup(%[1]q) :: any, inputs.wire, report)
	value = if vals ~= nil then vals[1] else nil
else
	value = load.load(%[1]q, inputs.wire, report)
end
if value == nil then print("the wire did not load: " .. tostring(report.reason)) return end
local poisoned = 0
for _, fd in d.fields do
	if fd.s == "arr" and fd.array == "counted" then
		local count = value[fd.slot .. "Count"]
		local dst = value[fd.slot]
		for i = count + 1, fd.bound do
			if type(dst) == "buffer" then
				buffer.writeu8(dst, i - 1, 0xff)
				poisoned += 1
			elseif count > 0 then
				dst[i] = dst[1]
				poisoned += 1
			end
		end
	end
end
print(poisoned)
for _, big in { false, true } do
	local file, why = W.cook(%[1]q, value, big)
	if file == nil then print("refuse " .. why.name .. " " .. tostring(why.field)) return end
	hex(file)
end
`, rootName)
	return b.String()
}

// TestMooCookWriteZeroesDeadAndAbsentBytes: the Moo cook writer writes an
// absent optional's payload and every dead slot of a counted array as zero
// bytes, in both byte orders (docs/SPEC-TABLES.md §7.2; docs/rulings.json
// fixed-dead-bytes-are-zero), whatever the value's storage holds there: an
// absent optional's declared default (keyed_config) and a counted array's
// slots past its count, filled with a live element (root_full). Each cook
// equals the committed cook-write fixture byte for byte, and every byte the
// fixture holds zero is zero.
func TestMooCookWriteZeroesDeadAndAbsentBytes(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted table_cookwrite.moo)")
	t.Parallel()
	want := map[string]bool{"keyed_config": true, "root_full": true}
	units, _ := readManifest(t)
	seen := 0
	for _, cw := range cookWriteRows(t) {
		row := cw.row
		if !want[row.caseName] {
			continue
		}
		seen++
		g := generateUnit(t, units, row.unit)
		if g.err != nil {
			t.Fatalf("unit %s: %v", row.unit, g.err)
		}
		out := strings.Split(g.run(t, cookZeroProbe(g, row.root), map[string][]byte{"wire": rowWire(t, row)}), "\n")
		if len(out) != 3 {
			t.Errorf("%s: %s", row.caseName, strings.Join(out, " | "))
			continue
		}
		if row.caseName == "root_full" && out[0] == "0" {
			t.Errorf("root_full: no dead counted slot was filled, so the probe proves nothing")
		}
		for i, order := range []struct {
			name, path string
		}{{"little-endian", cw.le}, {"big-endian", cw.be}} {
			fixture := readFile(t, order.path)
			got, err := hex.DecodeString(out[1+i])
			if err != nil {
				t.Fatalf("%s %s: %v", row.caseName, order.name, err)
			}
			if len(got) != len(fixture) {
				t.Errorf("%s %s: the cook is %d bytes, the fixture %d", row.caseName, order.name, len(got), len(fixture))
				continue
			}
			for at := range fixture {
				if fixture[at] == 0 && got[at] != 0 {
					t.Errorf("%s %s: byte %d is 0x%02x where the fixture's dead or absent byte is zero", row.caseName, order.name, at, got[at])
					break
				}
				if fixture[at] != got[at] {
					t.Errorf("%s %s: byte %d is 0x%02x, the fixture's is 0x%02x", row.caseName, order.name, at, got[at], fixture[at])
					break
				}
			}
		}
	}
	if seen != len(want) {
		t.Fatalf("the manifest carries %d of the %d cook-write rows the test names", seen, len(want))
	}
}
