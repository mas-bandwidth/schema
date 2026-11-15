package mootable_test

// A cook reference is accepted only when every byte the reader then takes
// through it lies inside the region: the whole pointee record. A reference
// whose pointee would run past the region is refused, and the read does not
// raise and does not read past the region.
//
// The fixture is one cooked tables/pointers Scene naming a single ListNode.
// head is rewritten so that node starts on the region's last byte, and at
// each earlier offset from one byte short of a whole record down to that
// last byte. Both shapes are held: the region ending inside the buffer, and
// the region running to the buffer's end. The unmodified cook still reads.
// The verdict is the contract (docs/rulings.json refusal-verdict-only).
//
// The negative control restores that pointee need to one byte in the emitted
// copy of table_cookread.moo and requires the same gate to go red.

import (
	"encoding/binary"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tablecook"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cookPointerNeed is the pointee record's size, passed as target's need.
// The negative control restores a one-byte need in the emitted copy.
const cookPointerNeed = "local need: number = records[d.name].size"

const cookRefProbe = `--!strict
local inputs = require("./inputs")
REQUIRES
local C = require("./table_cookread")

local SLOT = @@SLOT@@
local DATAOFF = @@DATAOFF@@
local DATALEN = @@DATALEN@@
local SIZE = @@SIZE@@

local function verdict(data: buffer, extent: number): string
	local handle, why = C.open("Scene", data, 0, extent)
	if handle == nil then
		return "open " .. tostring(why)
	end
	local ok, value = pcall(function(): any
		return C.read(handle)
	end)
	if not ok then
		return "raise " .. string.gsub(tostring(value), "\n", " ")
	end
	if value == nil then
		return "refused"
	end
	return "read"
end

local cook = inputs.cook
print("plain " .. verdict(cook, buffer.len(cook)))

local function putDelta(b: buffer, k: number)
	local delta = DATALEN - k - (SLOT - DATAOFF)
	buffer.writeu32(b, SLOT, delta)
	buffer.writeu32(b, SLOT + 4, 0)
end

-- the cook, plus one record of zeros past the file, so a read that runs
-- past the region still has buffer bytes under it
local function inside(): buffer
	local b = buffer.create(buffer.len(cook) + SIZE)
	buffer.copy(b, 0, cook, 0, buffer.len(cook))
	return b
end

-- attribution length cleared and the buffer cut at the region's end
local function toEnd(): buffer
	local n = DATAOFF + DATALEN
	local b = buffer.create(n)
	buffer.copy(b, 0, cook, 0, n)
	buffer.writeu32(b, 32, 0)
	buffer.writeu32(b, 36, 0)
	return b
end

for k = SIZE - 1, 1, -1 do
	local b = inside()
	putDelta(b, k)
	print("inside " .. tostring(k) .. " " .. verdict(b, buffer.len(cook)))
	local e = toEnd()
	putDelta(e, k)
	print("end " .. tostring(k) .. " " .. verdict(e, buffer.len(e)))
end
`

func cookRefField(inst *tabletext.Instance, name string) *tabletext.Field {
	for i := range inst.Fields {
		if inst.Fields[i].Def.Name == name {
			return &inst.Fields[i]
		}
	}
	panic("no field " + name + " on " + inst.Def.Name)
}

func cookRefSetStr(inst *tabletext.Instance, name, v string) {
	f := cookRefField(inst, name)
	f.Cell.Str = []byte(v)
	f.Count = len(v)
}

func cookRefSetInt(inst *tabletext.Instance, name string, v int64) {
	f := cookRefField(inst, name)
	f.Cell.I, f.Cell.U = v, uint64(v)
}

func cookRefSetNode(inst *tabletext.Instance, name string, target *tabletext.Instance) {
	cookRefField(inst, name).Cell.Node = target
}

// plantCookPointerNeedOne restores a one-byte pointee need in the emitted
// copy. The runtime source is left as it is.
func plantCookPointerNeedOne(t *testing.T, g *generatedUnit) {
	t.Helper()
	path := filepath.Join(g.dir, "table_cookread.moo")
	src, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Count(string(src), cookPointerNeed) != 1 {
		t.Fatalf("the anchor %q is not in the emitted table_cookread.moo exactly once", cookPointerNeed)
	}
	repl := strings.Replace(string(src), cookPointerNeed, "local need: number = 1", 1)
	if err := os.WriteFile(path, []byte(repl), 0o644); err != nil {
		t.Fatal(err)
	}
}

// cookRefPastRegionFailures cooks one ListNode under a Scene and reports
// every reference that does not answer the rule. mutate, when set, edits the
// emitted reader first.
func cookRefPastRegionFailures(t *testing.T, mutate func(*testing.T, *generatedUnit)) []string {
	t.Helper()
	units, _ := readManifest(t)
	g := generateUnit(t, units, "graphdemo")
	if g.err != nil {
		t.Fatal(g.err)
	}
	if mutate != nil {
		mutate(t, g)
	}
	m := tabletext.NewModel(g.unit)
	scene := m.New(m.Lookup("Scene"))
	cookRefSetStr(scene, "name", "one")
	head := m.New(m.Lookup("ListNode"))
	cookRefSetInt(head, "value", 7)
	cookRefSetStr(head, "name", "head")
	cookRefSetNode(scene, "head", head)
	cooked, err := tablecook.Cook(m, scene, tablecook.Options{})
	if err != nil {
		t.Fatal(err)
	}
	u := g.unit
	sceneLayout := ir.RecordLayout(u, m.Lookup("Scene"))
	nodeLayout := ir.RecordLayout(u, m.Lookup("ListNode"))
	h, err := tablecook.ReadHeader(cooked, ir.BuildVersion(u), sceneLayout.Align)
	if err != nil {
		t.Fatal(err)
	}
	if h.ByteOrder != tablecook.ByteOrderLittle {
		t.Fatalf("the probe writes a little-endian delta and this cook is order %d", h.ByteOrder)
	}
	fl := sceneLayout.FieldByName("head")
	if fl == nil {
		t.Fatal("Scene has no head")
	}
	pieces := ir.FieldPieces(u, fl.Field, fl.Offset)
	if len(pieces) != 1 || pieces[0].Size != 8 {
		t.Fatalf("Scene.head is not one 8-byte reference: %+v", pieces)
	}
	size := nodeLayout.Size
	if size < 2 {
		t.Fatalf("ListNode is %d bytes: a record that short cannot hang past the region", size)
	}
	dataOff := h.DataOffset()
	slot := dataOff + pieces[0].Offset
	if slot < 0 || slot+8 > int64(len(cooked)) {
		t.Fatalf("head's reference at %d does not lie in the %d-byte cook", slot, len(cooked))
	}
	if h.DataLength >= 1<<31 || dataOff >= 1<<31 {
		t.Fatalf("data length %d at %d does not fit the probe's u32 delta", h.DataLength, dataOff)
	}
	// the shortest delta this probe writes: the pointee one byte short of the record
	minDelta := h.DataLength - (size - 1) - pieces[0].Offset
	if minDelta <= 0 {
		t.Fatalf("a moved head would be a null delta (%d): the fixture is too small", minDelta)
	}
	orig := int64(binary.LittleEndian.Uint64(cooked[slot : slot+8]))
	target := slot + orig
	regionEnd := dataOff + h.DataLength
	if orig == 0 || target < dataOff || target+size > regionEnd {
		t.Fatalf("the cooked head does not name a ListNode inside the region: delta %d target %d size %d region [%d,%d)", orig, target, size, dataOff, regionEnd)
	}

	probe := strings.ReplaceAll(cookRefProbe, "REQUIRES\n", g.requireAll())
	probe = strings.ReplaceAll(probe, "@@SLOT@@", strconv.FormatInt(slot, 10))
	probe = strings.ReplaceAll(probe, "@@DATAOFF@@", strconv.FormatInt(dataOff, 10))
	probe = strings.ReplaceAll(probe, "@@DATALEN@@", strconv.FormatInt(h.DataLength, 10))
	probe = strings.ReplaceAll(probe, "@@SIZE@@", strconv.FormatInt(size, 10))
	out := g.run(t, probe, map[string][]byte{"cook": cooked})

	want := map[string]string{"plain": "read"}
	var order []string
	order = append(order, "plain")
	for k := size - 1; k >= 1; k-- {
		ks := strconv.FormatInt(k, 10)
		for _, kind := range []string{"inside", "end"} {
			name := kind + " " + ks
			want[name] = "refused"
			order = append(order, name)
		}
	}
	got := map[string]string{}
	var fails []string
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		kind, key, verdict, ok := cookRefSplit(line)
		if !ok {
			fails = append(fails, fmt.Sprintf("unexpected line %q", line))
			continue
		}
		name := kind
		if key != "" {
			name = kind + " " + key
		}
		if _, dup := got[name]; dup {
			fails = append(fails, fmt.Sprintf("repeated %s", name))
		}
		got[name] = verdict
	}
	for _, name := range order {
		if got[name] != want[name] {
			fails = append(fails, fmt.Sprintf("%s: got %q, want %q", name, got[name], want[name]))
		}
	}
	for name, verdict := range got {
		if _, ok := want[name]; !ok {
			fails = append(fails, fmt.Sprintf("unexpected %s: %q", name, verdict))
		}
	}
	return fails
}

func cookRefSplit(line string) (kind, key, verdict string, ok bool) {
	switch {
	case strings.HasPrefix(line, "plain "):
		return "plain", "", strings.TrimPrefix(line, "plain "), true
	case strings.HasPrefix(line, "inside "):
		rest := strings.TrimPrefix(line, "inside ")
		k, v, found := strings.Cut(rest, " ")
		if !found || k == "" {
			return "", "", "", false
		}
		return "inside", k, v, true
	case strings.HasPrefix(line, "end "):
		rest := strings.TrimPrefix(line, "end ")
		k, v, found := strings.Cut(rest, " ")
		if !found || k == "" {
			return "", "", "", false
		}
		return "end", k, v, true
	default:
		return "", "", "", false
	}
}

func TestMooCookRefusesAReferencePastTheRegion(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table_cookread.moo)")
	for _, f := range cookRefPastRegionFailures(t, nil) {
		t.Error(f)
	}
}

func TestMooCookRefusesAReferencePastTheRegionNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "moo (it runs the emitted table_cookread.moo)")
	fails := cookRefPastRegionFailures(t, plantCookPointerNeedOne)
	if len(fails) == 0 {
		t.Fatal("restoring the pointee need to one byte left the gate green")
	}
	t.Logf("restored need: %d findings; first %s", len(fails), fails[0])
}
