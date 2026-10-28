// The ONE-SIDED INTEGER RANGE on the Squirrel table surface
// (docs/SPEC-TABLES.md §4.3, SPEC §4.6): `a int64 | min = 0` bounds only the
// low side, the checker fills the high side from the kind's own limit, and the
// descriptor's lo/hi pair is what the runtime clamps a read against. The
// expected bytes and verdicts are computed by the schema-go engine
// (internal/tablewire over internal/tabletext), never typed by hand.
package compiler

import (
	"encoding/hex"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// nonGoCarriers is the untagged registry test's table of non-Go carriers; this
// tagged feature joins it here, the way compiler/carries_test.go expects.
func init() {
	nonGoCarriers["squirrel"] = append(nonGoCarriers["squirrel"], carryOneSidedRange)
}

const squirrelOneSidedSchema = "package probe\n\ntable OneSided\n{\n    a int64 | min = 0\n    b uint16 | max = 9\n}\n"

const squirrelOneSidedProbe = `local inputs = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require("ProbeTables");
local L = schema.require("schema_table_load");
local S = schema.require("schema_table_save");
local report = {
  unknown = 0, kindMismatch = 0, widened = 0, clamped = 0,
  duplicate = 0, malformed = false, refused = false
};
local v = L.loadTolerant(T, "OneSided", inputs.wire, report);
print("accepted " + ((v != null && !report.malformed && !report.refused) ? "true" : "false") + "\n");
print("clamped " + report.clamped.tostring() + "\n");
print("a " + v.A.tostring() + "\n");
print("b " + v.B.tostring() + "\n");
local b = S.save(T, "OneSided", v);
local h = "";
for (local i = 0; i < b.len(); i++) h += format("%02x", b[i] & 0xff);
print("saved " + h + "\n");
`

type squirrelOneSidedCase struct {
	name  string
	field string
	raw   uint64
}

// squirrelOneSidedOracle writes one raw value on one field, decodes that wire
// through schema-go's engine, and returns its verdict, clamp count, field
// values and save of the loaded instance.
func squirrelOneSidedOracle(t *testing.T, model *tabletext.Model, u *ir.Unit, tc squirrelOneSidedCase) (wire, saved []byte, accepted bool, clamped int, a int64, b uint64) {
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

func TestSquirrelCarriesOneSidedIntegerRanges(t *testing.T) {
	t.Parallel()
	u := unitFromSource(t, squirrelOneSidedSchema)
	files, err := New().Generate(u, "squirrel", Options{})
	if err != nil {
		t.Fatalf("squirrel refused the one-sided range it carries: %v", err)
	}
	if !carries("squirrel", carryOneSidedRange) {
		t.Fatal("squirrel generated the construct without registering it as carried")
	}
	dir := t.TempDir()
	unitDir := filepath.Join(dir, "probe")
	if err := os.MkdirAll(unitDir, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(unitDir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.nut"), []byte(squirrelOneSidedProbe), 0o644); err != nil {
		t.Fatal(err)
	}

	model := tabletext.NewModel(u)
	cases := []squirrelOneSidedCase{
		{"a below min", "a", ^uint64(0)},
		{"a in range", "a", 5},
		{"a above min", "a", uint64(math.MaxInt64)},
		{"b below max", "b", 0},
		{"b in range", "b", 5},
		{"b above max", "b", 10},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wire, wantSaved, wantAccepted, wantClamped, wantA, wantB := squirrelOneSidedOracle(t, model, u, tc)
			out := strings.TrimSpace(string(sqrun.Run(t, dir, "main.nut", map[string][]byte{"wire": wire})))
			lines := strings.Split(out, "\n")
			if len(lines) != 5 {
				t.Fatalf("the probe printed %d lines, want 5:\n%s", len(lines), out)
			}
			if want := "accepted " + strconv.FormatBool(wantAccepted); lines[0] != want {
				t.Errorf("the Squirrel load verdict is %q, schema-go's is %q", lines[0], want)
			}
			if want := "clamped " + strconv.Itoa(wantClamped); lines[1] != want {
				t.Errorf("the Squirrel load's counter is %q, schema-go's is %q", lines[1], want)
			}
			if want := "a " + strconv.FormatInt(wantA, 10); lines[2] != want {
				t.Errorf("the Squirrel load's a is %q, schema-go's is %q", lines[2], want)
			}
			if want := "b " + strconv.FormatUint(wantB, 10); lines[3] != want {
				t.Errorf("the Squirrel load's b is %q, schema-go's is %q", lines[3], want)
			}
			if want := "saved " + hex.EncodeToString(wantSaved); lines[4] != want {
				t.Errorf("the Squirrel save is %q, schema-go's is %q", lines[4], want)
			}
		})
	}
}
