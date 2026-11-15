package squirreltable_test

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/internal/tablewire"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// uint64MapWire is a hand-made form-1 body for `table Root { ids
// map[uint64]int32 }` (docs/SPEC-TABLES.md §2.8, §3): kind 14 over element kind
// 13, each entry an L-framed `{ key, value }` body, in the order given.
func uint64MapWire(t *testing.T, entries [][2]uint64) []byte {
	t.Helper()
	fieldID := ir.TableWireId("ids")
	body := []byte{byte(ir.TableKindTable)}
	body = binary.AppendUvarint(body, uint64(len(entries)))
	for _, e := range entries {
		var entry []byte
		entry = binary.AppendUvarint(entry, 2) // key's id reference
		entry = append(entry, byte(ir.TableKindU64))
		entry = binary.LittleEndian.AppendUint64(entry, e[0])
		entry = binary.AppendUvarint(entry, 3) // value's id reference
		entry = append(entry, byte(ir.TableKindI32))
		entry = binary.LittleEndian.AppendUint32(entry, uint32(e[1]))
		entry = append(entry, 0)
		body = binary.AppendUvarint(body, uint64(len(entry)))
		body = append(body, entry...)
	}
	wire := []byte{1}
	wire = binary.AppendUvarint(wire, 1) // ids' id reference
	wire = append(wire, byte(ir.TableKindArray))
	wire = binary.AppendUvarint(wire, uint64(len(body)))
	wire = append(wire, body...)
	wire = append(wire, 0)
	ids := []uint64{fieldID, ir.MapKeyWireId, ir.MapValueWireId}
	for _, id := range ids {
		wire = binary.LittleEndian.AppendUint64(wire, id)
	}
	wire = binary.LittleEndian.AppendUint64(wire, uint64(len(ids)))
	return wire
}

func addMapEntry(t *testing.T, m *tabletext.Model, f *tabletext.Field, key uint64, signed bool, value int32) {
	t.Helper()
	e := m.NewMapEntry(f.Def)
	if signed {
		e.Fields[0].Cell = tabletext.Cell{I: int64(key), U: key}
	} else {
		e.Fields[0].Cell = tabletext.Cell{U: key}
	}
	e.Fields[1].Cell = tabletext.Cell{I: int64(value), U: uint64(value)}
	f.Entries = append(f.Entries, tabletext.Cell{Tab: e})
}

func mapKeys(f *tabletext.Field) []uint64 {
	var out []uint64
	for _, c := range f.Entries {
		k := tabletext.MapKeyOf(f.Def, c.Tab)
		out = append(out, k.U)
	}
	return out
}

// TestSquirrelMapKeyOrderUnsigned pins docs/SPEC-TABLES.md §2.8's key order for
// a map[uint64] field: ascending by the UNSIGNED value, the order
// internal/tabletext.MapKeyOrder and internal/tablewire decide. A key at or
// above 2^63 reads negative in Squirrel, so the save and its order check must
// compare the bits unsigned; a signed key kind keeps the signed order.
func TestSquirrelMapKeyOrderUnsigned(t *testing.T) {
	t.Parallel()

	const src = `package probemap
table Root { ids map[uint64]int32 }
table Signed { ids map[int64]int32 }
`
	u, model, _ := nfLoadNamed(t, src)
	const (
		low  = uint64(1)
		high = uint64(1) << 63
		max  = ^uint64(0)
	)
	unsignedOrder := [][2]uint64{{low, 10}, {high, 20}, {max, 30}}

	t.Run("go", func(t *testing.T) {
		v := model.New(model.Lookup("Root"))
		f := &v.Fields[0]
		for _, e := range [][2]uint64{{max, 30}, {high, 20}, {low, 10}} {
			addMapEntry(t, model, f, e[0], false, int32(e[1]))
		}
		engine, err := tablewire.Encode(model, v)
		if err != nil {
			t.Fatal(err)
		}

		got := model.New(model.Lookup("Root"))
		var report tabletext.Report
		if ok, err := tablewire.Decode(model, got, engine, &report); err != nil || !ok {
			t.Fatalf("the engine refused its own ascending unsigned bytes: %v (%+v)", err, report)
		}
		if keys := mapKeys(&got.Fields[0]); fmt.Sprint(keys) != fmt.Sprint([]uint64{low, high, max}) {
			t.Fatalf("the engine wrote keys %v, want ascending unsigned %v", keys, []uint64{low, high, max})
		}

		ascending := uint64MapWire(t, unsignedOrder)
		var sane tabletext.Report
		if ok, err := tablewire.Decode(model, model.New(model.Lookup("Root")), ascending, &sane); err != nil || !ok {
			t.Fatalf("the hand-built ascending wire is not readable: %v (%+v)", err, sane)
		}
		signedOrder := uint64MapWire(t, [][2]uint64{{high, 20}, {max, 30}, {low, 10}})
		var refused tabletext.Report
		if _, err := tablewire.Decode(model, model.New(model.Lookup("Root")), signedOrder, &refused); err == nil || !tablewire.ReadRefusal(err) {
			t.Fatalf("the engine read the signed-order bytes: err=%v report=%+v", err, refused)
		}
	})

	// THE ENGINE'S BYTES for the unsigned map, built from entries in reverse
	// so the save proves it sorted them.
	uv := model.New(model.Lookup("Root"))
	uf := &uv.Fields[0]
	for _, e := range [][2]uint64{{max, 30}, {high, 20}, {low, 10}} {
		addMapEntry(t, model, uf, e[0], false, int32(e[1]))
	}
	engineWire, err := tablewire.Encode(model, uv)
	if err != nil {
		t.Fatal(err)
	}
	signedOrderWire := uint64MapWire(t, [][2]uint64{{high, 20}, {max, 30}, {low, 10}})

	// THE ENGINE'S BYTES for the signed map, entries built in a third order.
	sv := model.New(model.Lookup("Signed"))
	sf := &sv.Fields[0]
	for _, e := range [][2]uint64{{5, 50}, {^uint64(0), 20}, {high, 10}} {
		addMapEntry(t, model, sf, e[0], true, int32(e[1]))
	}
	signedWire, err := tablewire.Encode(model, sv)
	if err != nil {
		t.Fatal(err)
	}

	// THE COOK ORACLE for the unsigned map, in both byte orders: the cook lays
	// out the value it is given, so it is given the engine's own ascending
	// entries (tablecook does not sort; the wire's load does).
	cookValue := model.New(model.Lookup("Root"))
	var cookReport tabletext.Report
	if ok, err := tablewire.Decode(model, cookValue, engineWire, &cookReport); err != nil || !ok {
		t.Fatalf("the engine refused its own bytes before the cook: %v (%+v)", err, cookReport)
	}
	cookLittle, err := tablecook.Cook(model, cookValue, tablecook.Options{Big: false})
	if err != nil {
		t.Fatal(err)
	}
	cookBig, err := tablecook.Cook(model, cookValue, tablecook.Options{Big: true})
	if err != nil {
		t.Fatal(err)
	}

	dir := t.TempDir()
	mod := emitUnit(t, dir, "probe", u)
	probe := fmt.Sprintf(`local inputs = dofile("inputs.nut", true);
local schema = dofile("probe/schema_module.nut", true);
local T = schema.require(%q);
local hex = function(b) {
  local s = "";
  for (local i = 0; i < b.len(); i++) s += format("%%02x", b[i] & 0xff);
  return s;
};
local build = function(table, pairs) {
  local d = T.Descriptors[table].fields[0];
  local v = T.Descriptors[table].make();
  foreach (p in pairs) {
    local e = T.Descriptors[d.entry].make();
    e[d.mapkey.member] = p[0];
    e[d.mapvalue.member] = p[1];
    v[d.member].append(e);
  }
  return v;
};
local keys = function(table, v) {
  local d = T.Descriptors[table].fields[0];
  local out = [];
  foreach (e in v[d.member]) out.append(e[d.mapkey.member]);
  return out;
};
local sameKeys = function(got, want) {
  if (got.len() != want.len()) return false;
  for (local i = 0; i < got.len(); i++) if (got[i] != want[i]) return false;
  return true;
};

local engine = inputs["engine"];
local orders = [
  [[1, 10], [0x8000000000000000, 20], [0xffffffffffffffff, 30]],
  [[0xffffffffffffffff, 30], [0x8000000000000000, 20], [1, 10]],
  [[0x8000000000000000, 20], [0xffffffffffffffff, 30], [1, 10]]
];
foreach (pairs in orders) {
  local b = T.RootSave(build("Root", pairs));
  if (b == null) throw "save refused the unsigned map";
  if (hex(b) != hex(engine)) throw "save wrote " + hex(b) + ", the engine " + hex(engine);
}

local loaded = T.RootLoad(engine);
if (loaded == null) throw "load refused the engine's unsigned bytes";
if (!sameKeys(keys("Root", loaded), [1, 0x8000000000000000, 0xffffffffffffffff])) throw "load keys are not ascending unsigned";
local vd = T.Descriptors.Root.fields[0].mapvalue.member;
local lv = loaded[T.Descriptors.Root.fields[0].member];
if (lv[0][vd] != 10 || lv[1][vd] != 20 || lv[2][vd] != 30) throw "load values moved";

if (T.RootLoad(inputs["signedOrder"]) != null) throw "load accepted signed-order bytes";

local signedEngine = inputs["signedEngine"];
local sv = build("Signed", [[5, 50], [0xffffffffffffffff, 20], [0x8000000000000000, 10]]);
local sb = T.SignedSave(sv);
if (sb == null) throw "save refused the signed map";
if (hex(sb) != hex(signedEngine)) throw "signed save wrote " + hex(sb) + ", the engine " + hex(signedEngine);
local sl = T.SignedLoad(signedEngine);
if (sl == null) throw "load refused the engine's signed bytes";
if (!sameKeys(keys("Signed", sl), [0x8000000000000000, 0xffffffffffffffff, 5])) throw "signed load keys are not ascending signed";

local cv = build("Root", [[0xffffffffffffffff, 30], [0x8000000000000000, 20], [1, 10]]);
for (local order = 1; order <= 2; order++) {
  local want = inputs["cook" + order];
  local n = T.CookMeasure("Root", cv);
  if (n != want.len()) throw "cook measure " + n + ", the engine " + want.len();
  local b = blob(n);
  if (!T.Cook("Root", cv, b, order)) throw "cook refused the unsigned map";
  if (hex(b) != hex(want)) throw "cook wrote " + hex(b) + ", the engine " + hex(want);
}
print("ok\n");
`, mod)
	writeFile(t, dir+"/main.nut", []byte(probe))
	inputs := map[string][]byte{
		"engine":       engineWire,
		"signedOrder":  signedOrderWire,
		"signedEngine": signedWire,
		"cook1":        cookLittle,
		"cook2":        cookBig,
	}
	if out := string(sqrun.Run(t, dir, "main.nut", inputs)); strings.TrimSpace(out) != "ok" {
		t.Fatalf("the Squirrel map-key probes answered %q, want ok", strings.TrimSpace(out))
	}
}
