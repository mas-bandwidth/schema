// The ONE-SIDED INTEGER RANGE on the Luau table surface (docs/SPEC-TABLES.md
// §4.3, SPEC §4.6): `a int64 | min = 0` bounds only the low side, the checker
// fills the high side from the kind's own limit, and the descriptor's lo/hi
// pair is what schema_load.luau clamps a read against. The expected bytes and
// verdicts are computed by the schema-go engine (internal/tablewire over
// internal/tabletext), never typed by hand.
package luautable_test

import (
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

const oneSidedProbe = `local inputs = require("./inputs")
local load = require("./schema_load")
local save = require("./schema_save")
local St = require("./schema_tables")
require("./ProbeTable")

local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0 }
local v = load.loadTolerant("OneSided", inputs.wire, report)
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

// oneSidedOracle writes one raw value on one field, decodes that wire through
// schema-go's engine, and returns its verdict, clamp count, field values, wire
// and save of the loaded instance.
func oneSidedOracle(t *testing.T, model *tabletext.Model, u *ir.Unit, tc oneSidedCase) (wire, saved []byte, accepted bool, clamped int, a int64, b uint64) {
	t.Helper()
	st := u.Tables["OneSided"]
	idx := 0
	if tc.field == "b" {
		idx = 1
	}
	write := model.New(st)
	write.Fields[idx].Cell.I = int64(tc.raw)
	write.Fields[idx].Cell.U = tc.raw
	wire, err := tablewire.Encode(model, write)
	if err != nil {
		t.Fatalf("%s: encode: %v", tc.name, err)
	}
	read := model.New(st)
	var report tabletext.Report
	var ok bool
	if ok, err = tablewire.Decode(model, read, wire, &report); !ok || err != nil {
		t.Fatalf("%s: the schema-go engine refused its own wire: ok=%v err=%v", tc.name, ok, err)
	}
	saved, err = tablewire.Encode(model, read)
	if err != nil {
		t.Fatalf("%s: re-encode: %v", tc.name, err)
	}
	return wire, saved, ok, report.Clamped, read.Fields[0].Cell.I, read.Fields[1].Cell.U
}

func TestLuauOneSidedIntegerRange(t *testing.T) {
	t.Parallel()
	u, g := oneSidedUnit(t)
	model := tabletext.NewModel(u)
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(oneSidedProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := []oneSidedCase{
		{"a below min", "a", ^uint64(0)},
		{"a at min", "a", 0},
		{"a above min", "a", 1},
		{"a unbounded side at the kind limit", "a", uint64(math.MaxInt64)},
		{"b below max", "b", 8},
		{"b at max", "b", 9},
		{"b above max", "b", 10},
		{"b above max at the kind limit", "b", uint64(math.MaxUint16)},
		{"b unbounded side at the kind limit", "b", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire, wantSaved, wantAccepted, wantClamped, wantA, wantB := oneSidedOracle(t, model, u, tc)
			out := strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire})))
			lines := strings.Split(out, "\n")
			if len(lines) != 5 {
				t.Fatalf("the probe printed %d lines, want 5:\n%s", len(lines), out)
			}
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
