package luautable_test

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
)

// retainBoundsMutant is the wire fuzz's mutant of shapes_bounds_ends
// (luau.nightly.fuzz.wire-fuzz.retain, seed 20261125, random pass #54691,
// sha256 42e12b787a75f88047c6d30273cfcb57be89597045331c4041b72b47d135e3d2):
// a shapes.Bounds wire the strict read accepts with four fields this build
// cannot name, held by the retaining load and written back by SaveRetain.
const retainBoundsMutant = "01010acdcc4c3d020a0000403f030a00002040040e0406020102050e0403010500060e0d0d010a07040100000008060200090f0a030203000b0e041e020c0d0e0e1209020100000000000000060000000000000000d82017d348fe230a82f45c94c835f8b7a2bf3466129c7f9f59999e4ffe79e09992978f91fc10c17a2e6ce2bf1f6cec4c8cee01874cdc63afa5f101864cdd63afd8bb13c50dc40bbf8c9998c072e16c7dd64131387de97bfbc15085b819a3f3241da8a89cd8efee9d296e72e1a4d477900e00000000000000"

// TestLuauSaveRetainBoundsMutantMatchesTheOracle holds the found mutant's
// retaining round trip to the Go oracle's (docs/SPEC-TABLES.md §6.6): the
// leg's LoadRetain then SaveRetain must write the bytes tablewire's
// DecodeRegionRetain then EncodeRetain write, with the same two stores.
//
// The mutant's one element of recs ([1..4]Rec) carries only ids this build
// cannot name, so the value holds recs at its birth (one default Rec), which
// the plain save elides. The two retained fields sit inside that element, and
// A BODY THAT CARRIES A RETAINED FIELD DOES NOT ELIDE (§6.6): with room for
// their ids the retaining save writes recs with its element body and both
// fields (the leg dropped them), and with an id list of capacity 0 both are
// lost on the save and recs is at its birth again, so it elides.
func TestLuauSaveRetainBoundsMutantMatchesTheOracle(t *testing.T) {
	t.Parallel()
	wire, err := hex.DecodeString(retainBoundsMutant)
	if err != nil {
		t.Fatal(err)
	}
	src, err := os.ReadFile(filepath.Join("..", "..", "..", "test", "tables", "shapes", "Shapes.schema"))
	if err != nil {
		t.Fatal(err)
	}
	u := saveRetainCompile(t, string(src))
	m := tabletext.NewModel(u)
	g := generateFiles(t, map[string]string{"Shapes.schema": string(src)})
	const capacity = 1 << 20

	for _, tc := range []struct {
		name  string
		idCap int
		rides bool // the retained fields ride, so the save is longer than the plain one
	}{
		{"the retained fields ride", capacity / 8, true},
		{"no room for their ids", 0, false},
	} {
		inst := m.New(m.Lookup("Bounds"))
		keep := &tablewire.Retain{Capacity: capacity, IdCapacity: tc.idCap}
		var report tabletext.Report
		ok, err := tablewire.DecodeRegionRetain(m, inst, wire, keep, &report)
		if !ok || err != nil || report.Refused {
			t.Fatalf("%s: the oracle refused the mutant: %v %v %+v", tc.name, ok, err, report)
		}
		var saved tabletext.Report
		want, err := tablewire.EncodeRetain(m, inst, keep, &saved)
		if err != nil {
			t.Fatalf("%s: the oracle save failed: %v", tc.name, err)
		}
		plain, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the oracle's plain save failed: %v", tc.name, err)
		}
		if report.Retained != 2 || (len(want) > len(plain)) != tc.rides || (!tc.rides && saved.RetainLost != 2) {
			t.Fatalf("%s: the case is not the one it pins: the oracle retained %d fields, lost %d on the save, and its retaining save is %d bytes beside a plain save of %d",
				tc.name, report.Retained, saved.RetainLost, len(want), len(plain))
		}

		probe := luaurun.HexProbe + fmt.Sprintf(`local retain = require("./schema_retain")
local inputs = require("./inputs")
require("./ShapesTable")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local v, store = retain.LoadRetain("Bounds", inputs.wire, report, %d, %d)
if v == nil then print("LOADNIL") return end
local out = retain.SaveRetain("Bounds", v, store, report)
if out == nil then print("SAVENIL") return end
hex(out)
`, capacity, tc.idCap)
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire}))), "\n")
		last := lines[len(lines)-1]
		if last == "LOADNIL" || last == "SAVENIL" {
			t.Fatalf("%s: the leg did not round-trip the mutant the oracle reads: %s", tc.name, last)
		}
		got, err := hex.DecodeString(last)
		if err != nil {
			t.Fatalf("%s: the probe did not print hex: %v (%q)", tc.name, err, last)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: SaveRetain wrote %d bytes %x\nthe oracle wrote %d bytes %x", tc.name, len(got), got, len(want), want)
		}
	}
}

// A born counted array of more than one element, its retained field in an
// element that is not the first: recs is [3..5]Rec, so its birth is three
// default Recs, which the plain save elides. The newer build writes an extra
// field this build cannot name in one element; the value read by this build
// holds recs at its birth, and A BODY THAT CARRIES A RETAINED FIELD DOES NOT
// ELIDE (docs/SPEC-TABLES.md §6.6), so the retaining save must materialize all
// three born elements and put the field back in the element it came from,
// the last born one included. The expected bytes are the oracle's
// DecodeRegionRetain then EncodeRetain.
const bornOldSchema = `package probe
table Rec { n int32 }
table Root { recs [3..5]Rec }
`

const bornNewSchema = `package probe
table Rec { n int32
 extra string(32) }
table Root { recs [3..5]Rec }
`

func TestLuauSaveRetainBornArrayLaterElementMatchesTheOracle(t *testing.T) {
	t.Parallel()
	oldUnit := saveRetainCompile(t, bornOldSchema)
	newUnit := saveRetainCompile(t, bornNewSchema)
	m := tabletext.NewModel(oldUnit)
	const capacity = 1 << 20

	for _, tc := range []struct {
		name string
		json string
	}{
		{"the middle born element", `{"recs":[{},{"extra":"kept"},{}]}`},
		{"the last born element", `{"recs":[{},{},{"extra":"kept"}]}`},
	} {
		wire := saveRetainWire(t, newUnit, tc.json)
		inst := m.New(m.Lookup("Root"))
		keep := &tablewire.Retain{Capacity: capacity, IdCapacity: capacity / 8}
		var report tabletext.Report
		ok, err := tablewire.DecodeRegionRetain(m, inst, wire, keep, &report)
		if !ok || err != nil || report.Refused {
			t.Fatalf("%s: the oracle refused the newer wire: %v %v %+v", tc.name, ok, err, report)
		}
		var saved tabletext.Report
		want, err := tablewire.EncodeRetain(m, inst, keep, &saved)
		if err != nil {
			t.Fatalf("%s: the oracle save failed: %v", tc.name, err)
		}
		plain, err := tablewire.Encode(m, inst)
		if err != nil {
			t.Fatalf("%s: the oracle's plain save failed: %v", tc.name, err)
		}
		if report.Retained != 1 || saved.RetainLost != 0 || len(want) <= len(plain) {
			t.Fatalf("%s: the case is not the one it pins: the oracle retained %d fields, lost %d on the save, and its retaining save is %d bytes beside a plain save of %d",
				tc.name, report.Retained, saved.RetainLost, len(want), len(plain))
		}

		g := generateFiles(t, map[string]string{"Probe.schema": bornOldSchema})
		probe := luaurun.HexProbe + fmt.Sprintf(`local retain = require("./schema_retain")
local inputs = require("./inputs")
require("./ProbeTable")
local report = { malformed = false, refused = 0, unknown = 0, kindMismatch = 0, clamped = 0, widened = 0, duplicate = 0, retained = 0, retainLost = 0 }
local v, store = retain.LoadRetain("Root", inputs.wire, report, %d, %d)
if v == nil then print("LOADNIL") return end
local out = retain.SaveRetain("Root", v, store, report)
if out == nil then print("SAVENIL") return end
hex(out)
`, capacity, capacity/8)
		if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(probe), 0o644); err != nil {
			t.Fatal(err)
		}
		lines := strings.Split(strings.TrimSpace(string(luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"wire": wire}))), "\n")
		last := lines[len(lines)-1]
		if last == "LOADNIL" || last == "SAVENIL" {
			t.Fatalf("%s: the leg did not round-trip the wire the oracle reads: %s", tc.name, last)
		}
		got, err := hex.DecodeString(last)
		if err != nil {
			t.Fatalf("%s: the probe did not print hex: %v (%q)", tc.name, err, last)
		}
		if !bytes.Equal(got, want) {
			t.Fatalf("%s: SaveRetain wrote %d bytes %x\nthe oracle wrote %d bytes %x", tc.name, len(got), got, len(want), want)
		}
	}
}
