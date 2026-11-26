// The ONE-SIDED INTEGER RANGE on the Luau table surface (docs/SPEC-TABLES.md
// §4.3, SPEC §4.6): `a int64 | min = 0` bounds only the low side, the checker
// fills the high side from the kind's own limit, and the descriptor's lo/hi
// pair is what schema_load.luau clamps a read against. The expected bytes and
// verdicts are computed by the schema-go engine (internal/tablewire over
// internal/tabletext), never typed by hand.
package luautable_test

import (
	"bytes"
	"encoding/hex"
	"maps"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const oneSidedSchema = `package probe

table OneSided
{
    a int64 | min = 0
    b uint16 | max = 9
}
`

// oneSidedRangeFree is the RANGE-FREE TWIN: the same names and kinds with no
// range, so the engine's writer (which refuses a value off its range,
// invalid_value) can still write the off-contract wire the reads are held to.
const oneSidedRangeFree = `package probe

table OneSided
{
    a int64
    b uint16
}
`

// The probe prints every Luau read's verdict (the plain load, the walk it is
// built on, the strict read), then, only when the walk keeps a value, that
// value's counters, fields and save.
const oneSidedProbe = `local inputs = require("./inputs")
local load = require("./schema_load")
local R = require("./schema_refusal")
local save = require("./schema_save")
local St = require("./schema_tables")
require("./ProbeTable")

local function fresh()
	return { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
end
local report = fresh()
local v = load.loadWalk("OneSided", inputs.wire, report)
local plain = load.load("OneSided", inputs.wire, fresh())
local strict = R.ReadStrict(nil, "OneSided", inputs.wire, fresh())
print("reads " .. tostring(plain ~= nil) .. " " .. tostring(v ~= nil) .. " " .. tostring(strict ~= nil))
if v == nil then return end
print("accepted " .. tostring(v ~= nil and not report.malformed and report.refused == 0))
print("clamped " .. tostring(report.clamped))
print("a " .. tostring(v.A))
print("b " .. tostring(v.B))
local out = save.save(St.lookup("OneSided"), v)
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
end
print("saved " .. table.concat(parts))
`

type oneSidedCase struct {
	name  string
	field string
	raw   uint64 // the wire value: two's complement for a signed field
}

// oneSidedUnit compiles the probe unit, emits the Luau type and table
// surfaces into a fresh directory, and returns the checked unit beside them.
func oneSidedUnit(t *testing.T) (*ir.Unit, *generatedUnit) {
	t.Helper()
	srcDir := t.TempDir()
	path := filepath.Join(srcDir, "Probe.schema")
	if err := os.WriteFile(path, []byte(oneSidedSchema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the one-sided probe does not load: %v", err)
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: t.TempDir(), files: files, loaded: true}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(g.dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return u, g
}

// oneSidedRangeFreeUnit compiles the range-free twin for the engine's writer.
func oneSidedRangeFreeUnit(t *testing.T) *ir.Unit {
	t.Helper()
	path := filepath.Join(t.TempDir(), "Probe.schema")
	if err := os.WriteFile(path, []byte(oneSidedRangeFree), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("the range-free twin does not load: %v", err)
	}
	return u
}

// oneSidedOracle writes one raw value on one field through the range-free
// twin (checking that the twin writes the ranged schema's own bytes wherever
// the ranged writer accepts the value), decodes that wire through schema-go's
// engine under the ranged schema, and returns its verdict and, when it reads
// the wire, the clamp count, field values and save of the loaded instance.
func oneSidedOracle(t *testing.T, model, rawModel *tabletext.Model, u, rawU *ir.Unit, tc oneSidedCase) (wire, saved []byte, accepted bool, clamped int, a int64, b uint64) {
	t.Helper()
	idx := 0
	if tc.field == "b" {
		idx = 1
	}
	raw := rawModel.New(rawU.Tables["OneSided"])
	raw.Fields[idx].Cell.I = int64(tc.raw)
	raw.Fields[idx].Cell.U = tc.raw
	wire, err := tablewire.Encode(rawModel, raw)
	if err != nil {
		t.Fatalf("%s: the range-free twin does not encode: %v", tc.name, err)
	}
	st := u.Tables["OneSided"]
	write := model.New(st)
	write.Fields[idx].Cell.I = int64(tc.raw)
	write.Fields[idx].Cell.U = tc.raw
	if ranged, err := tablewire.Encode(model, write); err == nil && !bytes.Equal(ranged, wire) {
		t.Fatalf("%s: the range-free twin wrote %x, the ranged schema %x", tc.name, wire, ranged)
	}
	read := model.New(st)
	var report tabletext.Report
	ok, err := tablewire.Decode(model, read, wire, &report)
	if !ok || err != nil || report.Refused {
		return wire, nil, false, 0, 0, 0
	}
	saved, err = tablewire.Encode(model, read)
	if err != nil {
		t.Fatalf("%s: re-encode: %v", tc.name, err)
	}
	return wire, saved, true, 0, read.Fields[0].Cell.I, read.Fields[1].Cell.U
}

// TestLuauOneSidedIntegerRange holds the one-sided range on every Luau read,
// strict only (docs/rulings.json strict-only-read): a value past its one
// bound, or past the bound at the kind's own limit, is refused by the Go engine
// and by every Luau read (load, loadWalk, R.ReadStrict), compared by verdict
// alone (refusal-verdict-only); a value at or inside the bounds, and the
// unbounded side at the kind's limit, reads to the engine's value and saves
// the engine's bytes.
func TestLuauOneSidedIntegerRange(t *testing.T) {
	t.Parallel()
	u, g := oneSidedUnit(t)
	model := tabletext.NewModel(u)
	rawU := oneSidedRangeFreeUnit(t)
	rawModel := tabletext.NewModel(rawU)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(oneSidedProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		oneSidedCase
		refused bool // the verdict the bound owes, held against the engine's
	}{
		{oneSidedCase{"a below min", "a", ^uint64(0)}, true},
		{oneSidedCase{"a at min", "a", 0}, false},
		{oneSidedCase{"a above min", "a", 1}, false},
		{oneSidedCase{"a unbounded side at the kind limit", "a", uint64(math.MaxInt64)}, false},
		{oneSidedCase{"b below max", "b", 8}, false},
		{oneSidedCase{"b at max", "b", 9}, false},
		{oneSidedCase{"b above max", "b", 10}, true},
		{oneSidedCase{"b above max at the kind limit", "b", uint64(math.MaxUint16)}, true},
		{oneSidedCase{"b unbounded side at the kind limit", "b", 0}, false},
	}
	for _, c := range cases {
		tc := c.oneSidedCase
		t.Run(tc.name, func(t *testing.T) {
			wire, wantSaved, wantAccepted, wantClamped, wantA, wantB := oneSidedOracle(t, model, rawModel, u, rawU, tc)
			if wantAccepted == c.refused {
				t.Fatalf("the Go engine's verdict is accepted=%v; the one-sided bound owes refused=%v", wantAccepted, c.refused)
			}
			out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
			lines := strings.Split(out, "\n")
			if !wantAccepted {
				if len(lines) != 1 || lines[0] != "reads false false false" {
					t.Fatalf("the Go engine refuses this wire, and every Luau read owes the refusal (load, loadWalk, ReadStrict):\n%s", out)
				}
				return
			}
			if len(lines) != 6 {
				t.Fatalf("the probe printed %d lines, want 6:\n%s", len(lines), out)
			}
			if lines[0] != "reads true true true" {
				t.Errorf("the Luau reads (load, loadWalk, ReadStrict) answer %q; the Go engine reads this wire", lines[0])
			}
			lines = lines[1:]
			if want := "accepted " + strconv.FormatBool(wantAccepted); lines[0] != want {
				t.Errorf("the Luau load verdict is %q, schema-go's is %q", lines[0], want)
			}
			if want := "clamped " + strconv.Itoa(wantClamped); lines[1] != want {
				t.Errorf("the Luau load's counter is %q, schema-go's is %q", lines[1], want)
			}
			if want := "a " + strconv.FormatInt(wantA, 10); lines[2] != want {
				t.Errorf("the Luau load's a is %q, schema-go's is %q", lines[2], want)
			}
			if want := "b " + strconv.FormatUint(wantB, 10); lines[3] != want {
				t.Errorf("the Luau load's b is %q, schema-go's is %q", lines[3], want)
			}
			if want := "saved " + hex.EncodeToString(wantSaved); lines[4] != want {
				t.Errorf("the Luau save is %q, schema-go's is %q", lines[4], want)
			}
		})
	}
}
