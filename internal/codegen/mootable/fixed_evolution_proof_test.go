package mootable_test

// THE FIXED FORM'S LAYOUT, EVOLUTION, PLAN, RUNTIME AND HOSTILE ROWS ON THE MOO
// LEG (docs/FIXED-FORM-ALGORITHM.md §1, §4, §5; docs/SPEC-TABLES.md §3.4). moo
// proves each row through its own design under the per-language-design-proof
// ruling (docs/rulings.json): one runtime walk (runtime/table_fixed.moo) whose
// plans are compiled once per lineage entry from the static data the emitter
// lays down, behind a load that answers fresh values or nil. The behavior and
// the bytes are the reference's: every file a row reads is the C++
// reference's corpus (build/fixedform-corpus) or a file framed here from the
// compiler's own layout and hash, and every row that guards a runtime rule has
// a negative control that plants a break into the emitted table_fixed.moo and
// requires the row to go red.

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/mootable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// evoFile frames a form-3 file: the header, the layout, and each body behind
// the record hash.
func evoFile(hash uint64, layout []byte, bodies ...[]byte) []byte {
	out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
	out = binary.LittleEndian.AppendUint64(out, hash)
	out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
	out = append(out, layout...)
	for _, b := range bodies {
		out = binary.LittleEndian.AppendUint64(out, hash)
		out = append(out, b...)
	}
	return out
}

// evoEntry is the entry the unit's own build locks for root.
func evoEntry(t *testing.T, u *ir.Unit, root string) mootable.FixedLineageEntry {
	t.Helper()
	e, ok := mootable.FixedLineageOf(u, root)
	if !ok {
		t.Fatalf("no fixed root %s", root)
	}
	return e
}

// evoProbe loads inputs.file at root with a report every field of which is
// stale, so a load that does not reset it shows. It prints
// "ok <n> u k c w d <hash>" or "refused <reason> <malformed> <field> <hash>
// u k c w d", then the dump and the re-saved bytes as fixedLoadProbe does.
// capacity < 0 passes none.
func evoProbe(g *generatedUnit, mod, rootName string, capacity int) string {
	capLit := "nil"
	if capacity >= 0 {
		capLit = strconv.Itoa(capacity)
	}
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal F = require(\"./table_fixed\")\n" + g.requireAll() +
		fmt.Sprintf("local T = require(\"./%s\")\n", mod) + fixedDumper + fmt.Sprintf(`local d = T.%[1]sDescriptor
local fixed = T.%[1]sFixed
local rep: any = { unknown = 99, kindChanged = 99, outOfRange = 99, widened = 99, repeatedKey = 99, damaged = true, reason = "stale", field = "stale", layoutHash = "stale!!!" }
local values = F.load(d, fixed, inputs.file, rep, %[2]s)
local function counters(): string
	return string.format("u%%d k%%d c%%d w%%d d%%d", rep.unknown, rep.kindChanged, rep.outOfRange, rep.widened, rep.repeatedKey)
end
local function hashOf(): string
	return if rep.layoutHash ~= nil then V.idHex(rep.layoutHash) else "-"
end
if values == nil then
	print("refused " .. tostring(rep.reason) .. " " .. tostring(rep.damaged) .. " " .. tostring(rep.field) .. " " .. hashOf() .. " " .. counters())
	return
end
print(string.format("ok %%d ", #values) .. counters() .. " " .. hashOf())
for i, v in values do
	dumpRecord(d, v, "r" .. int(i - 1))
end
for _, l in lines do
	print(l)
end
local out, why = F.save(d, fixed, values)
if out == nil then
	print("SAVENIL " .. why.name .. " " .. tostring(why.field))
	return
end
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
end
print("x" .. table.concat(parts))
`, rootName, capLit)
}

// evoLoad runs evoProbe over file. A program that raises answers the head
// "raised" (a negative control's planted break may read past a record); a
// re-save the runtime refuses lands its refusal under dump["SAVENIL"].
func evoLoad(t *testing.T, g *generatedUnit, mod, rootName string, file []byte, capacity int) fixedRun {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.moo"), []byte(evoProbe(g, mod, rootName, capacity)), 0o644); err != nil {
		t.Fatal(err)
	}
	raw, err := moorun.RunErr(t, g.dir, "main.moo", map[string][]byte{"file": file})
	if err != nil {
		return fixedRun{head: "raised", dump: map[string]string{}}
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	r := fixedRun{head: lines[0], dump: map[string]string{}}
	for _, l := range lines[1:] {
		if strings.HasPrefix(l, "x") {
			var err error
			if r.saved, err = hexDecode(strings.TrimPrefix(l, "x")); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if rest, ok := strings.CutPrefix(l, "SAVENIL "); ok {
			r.dump["SAVENIL"] = rest
			continue
		}
		k, v, _ := strings.Cut(l, "=")
		r.dump[k] = v
	}
	return r
}

// evoSave runs setup (moo that fills `values`, with T the unit's table
// module) and saves values at root, answering the file.
func evoSave(t *testing.T, g *generatedUnit, mod, rootName, setup string) []byte {
	t.Helper()
	main := "--!strict\nlocal F = require(\"./table_fixed\")\nlocal V = require(\"./table_values\")\n" + g.requireAll() +
		fmt.Sprintf("local T = require(\"./%s\")\nlocal _ = V\nlocal values: { any } = {}\n", mod) + setup + fmt.Sprintf(`
local out, why = F.save(T.%[1]sDescriptor, T.%[1]sFixed, values)
if out == nil then
	print("SAVENIL " .. why.name .. " " .. tostring(why.field))
	return
end
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%%02x", buffer.readu8(out, i))
end
print(table.concat(parts))
`, rootName)
	out := g.run(t, main, nil)
	if strings.HasPrefix(out, "SAVENIL") {
		t.Fatalf("the save refused: %s", out)
	}
	b, err := hexDecode(out)
	if err != nil {
		t.Fatalf("the save printed %q: %v", out, err)
	}
	return b
}

// plant replaces anchor, which must sit in the emitted table_fixed.moo
// exactly once, and rewrites the unit's copy.
func plant(t *testing.T, g *generatedUnit, anchor, with string) {
	t.Helper()
	src := string(g.files["table_fixed.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatalf("the anchor %q is not in table_fixed.moo exactly once", anchor)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_fixed.moo"), []byte(strings.Replace(src, anchor, with, 1)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// plantEach replaces each anchor, every one of which must sit in the emitted
// table_fixed.moo exactly once, by its pair, in one rewrite of the unit's copy.
func plantEach(t *testing.T, g *generatedUnit, pairs ...string) {
	t.Helper()
	src := string(g.files["table_fixed.moo"])
	for i := 0; i+1 < len(pairs); i += 2 {
		if strings.Count(src, pairs[i]) != 1 {
			t.Fatalf("the anchor %q is not in table_fixed.moo exactly once", pairs[i])
		}
		src = strings.Replace(src, pairs[i], pairs[i+1], 1)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_fixed.moo"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
}

// dropRangeChecks is the range negative controls' break: neither the writer's
// range nor the reader's is held.
var dropRangeChecks = []string{"\tif n.wlo ~= nil and (", "\tif false and (", "\tif f.lo ~= nil and (", "\tif false and ("}

func clone(b []byte) []byte { return append([]byte(nil), b...) }

// evoHash is the "#<hex>" spelling the probe prints a hash in.
func evoHash(h uint64) string { return fmt.Sprintf("#%016x", h) }

// fileHash is a file's header hash.
func fileHash(b []byte) uint64 { return binary.LittleEndian.Uint64(b[8:16]) }

// refusedTotal is the head of a refusal that moved nothing.
func refusedTotal(reason string, malformed bool, hash string) string {
	return fmt.Sprintf("refused %s %v nil %s u0 k0 c0 w0 d0", reason, malformed, hash)
}

// refusedField is the head of a refusal that names the field it refused
// (out_of_range, wire_damaged): moo's own diagnostic, never compared across
// languages (SPEC-TABLES §6.5).
func refusedField(reason string, damaged bool, field string) string {
	return fmt.Sprintf("refused %s %v %s - u0 k0 c0 w0 d0", reason, damaged, field)
}

// evoUnitRefusal loads src and answers the compiler's refusal, or "".
func evoUnitRefusal(t *testing.T, src string) string {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := compiler.New().Load([]string{p}); err != nil {
		return err.Error()
	}
	return ""
}

const pointSchema = "package probe\nfixed table Point\n{\n    x int32 = 1\n    y int32 = 2\n}\n"

// TestMooFixedEvolutionFileEnvelope is file-envelope (F1, F2, F3, F4, F7, F8,
// F12) over the reference's fx1.bin: every form byte that is not 3 refuses by
// its direction, a layout length past the file and a short header and a
// ragged tail refuse by name, and a held hash over a second layout — one
// layout byte moved, or the length one short — is layout_malformed. Every
// refusal is total: nothing lands and every stale counter is reset to zero.
func TestMooFixedEvolutionFileEnvelope(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	oracle := corpusFile(t, corpus, "fx1.bin")
	g := fixedUnit(t, unitFromPath(t, "test/tables/FX1.schema"), nil)
	cut := func(f func(b []byte)) []byte { b := clone(oracle); f(b); return b }
	for _, c := range []struct {
		name string
		file []byte
		want string
	}{
		{"F1 form 0", cut(func(b []byte) { b[0] = 0 }), refusedTotal("newer_form", false, "-")},
		{"F1 form 1", cut(func(b []byte) { b[0] = 1 }), refusedTotal("previous_form", false, "-")},
		{"F2 form 2", cut(func(b []byte) { b[0] = 2 }), refusedTotal("message_form_as_file", false, "-")},
		{"F3 form 4", cut(func(b []byte) { b[0] = 4 }), refusedTotal("newer_form", false, "-")},
		{"F3 form 255", cut(func(b []byte) { b[0] = 255 }), refusedTotal("newer_form", false, "-")},
		{"F4 layout past the file", cut(func(b []byte) { binary.LittleEndian.PutUint32(b[16:], 0x7fff0000) }), refusedTotal("layout_malformed", false, "-")},
		{"F7 under 20 bytes", oracle[:19], refusedTotal("truncated", true, "-")},
		{"F8 ragged tail", oracle[:len(oracle)-1], refusedTotal("truncated", true, "-")},
		{"F12 a layout byte moved", cut(func(b []byte) { b[20] ^= 1 }), refusedTotal("layout_malformed", false, "-")},
		{"F12 the layout one short", cut(func(b []byte) { b[16]-- }), refusedTotal("layout_malformed", false, "-")},
	} {
		if got := evoLoad(t, g, "FX1Table", "FxRoot", c.file, -1).head; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if got := evoLoad(t, g, "FX1Table", "FxRoot", oracle, -1).head; !strings.HasPrefix(got, "ok 2 u0 k0 c0 w0 d0 -") {
		t.Errorf("the control, the reference's file: %s", got)
	}
}

// TestMooFixedEvolutionBatchCapacity is batch-capacity (F9): a file holding
// more records than the caller's capacity refuses batch_too_large before any
// record is looked at (a bad record hash behind it is not reached), nothing
// lands, no counter moves; a capacity the batch fits reads.
func TestMooFixedEvolutionBatchCapacity(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	g := fixedUnit(t, unitFromSource(t, pointSchema), nil)
	three := evoSave(t, g, "ProbeTable", "Point", "local v = T.NewPoint()\nv.X = 10\nv.Y = 20\nvalues = { v, v, v }\n")
	badLast := clone(three)
	badLast[len(badLast)-16] ^= 1 // the third record's hash
	for _, c := range []struct {
		name     string
		file     []byte
		capacity int
		want     string
	}{
		{"past the room", three, 2, refusedTotal("batch_too_large", false, "-")},
		{"past the room, before the record hashes", badLast, 2, refusedTotal("batch_too_large", false, "-")},
		{"the room exactly", three, 3, "ok 3 u0 k0 c0 w0 d0 -"},
		{"no capacity", three, -1, "ok 3 u0 k0 c0 w0 d0 -"},
		{"the bad hash in the room", badLast, 3, refusedTotal("no_layout", false, "-")},
	} {
		if got := evoLoad(t, g, "ProbeTable", "Point", c.file, c.capacity).head; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	// the negative control: the capacity check removed, the batch reads
	plant(t, g, "\tif capacity ~= nil and count > capacity then\n", "\tif false then\n")
	if got := evoLoad(t, g, "ProbeTable", "Point", three, 2).head; got == refusedTotal("batch_too_large", false, "-") {
		t.Fatalf("the planted break still refuses: the gate proves nothing")
	}
}

// TestMooFixedEvolutionPlanSelection is plan-selection (F10, R7, R8, R9, R12,
// R13, W15): the header's hash, taken as given, selects a lineage entry by
// comparison — the reader's own file and an older entry's both read; a hash no
// entry holds refuses layout_unknown reporting that hash and nothing else; a
// held hash over ANY other layout bytes (every byte of fx1's layout moved in
// turn) is layout_malformed; a record whose hash is not the header's refuses
// no_layout before anything lands, even the last of a batch. Every refusal is
// total. The runtime carries no hash function: the hash is never recomputed.
func TestMooFixedEvolutionPlanSelection(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	oracle := corpusFile(t, corpus, "fx1.bin")
	g := fixedUnit(t, unitFromPath(t, "test/tables/FX1.schema"), nil)
	if src := string(g.files["table_fixed.moo"]); strings.Contains(src, "0x100000001b3") || strings.Contains(src, "1099511628211") || strings.Contains(src, "cbf29ce484222325") {
		t.Errorf("R7: the runtime carries the FNV-1a constants: a hash is recomputed")
	}
	unknown := clone(oracle)
	unknown[8] ^= 1
	if got, want := evoLoad(t, g, "FX1Table", "FxRoot", unknown, -1).head, refusedTotal("layout_unknown", false, evoHash(fileHash(unknown))); got != want {
		t.Errorf("R8: %s, want %s", got, want)
	}
	// R9: every layout byte moved under the held hash, in one moo program
	layoutLen := int(binary.LittleEndian.Uint32(oracle[16:20]))
	main := "--!strict\nlocal inputs = require(\"./inputs\")\nlocal F = require(\"./table_fixed\")\n" + g.requireAll() + fmt.Sprintf(`local T = require("./FX1Table")
local bad = 0
for i = 20, %d do
	local b = buffer.create(buffer.len(inputs.file))
	buffer.copy(b, 0, inputs.file)
	buffer.writeu8(b, i, bit32.bxor(buffer.readu8(b, i), 0x5a))
	local rep: any = {}
	if F.load(T.FxRootDescriptor, T.FxRootFixed, b, rep) ~= nil or rep.reason ~= "layout_malformed" or rep.outOfRange ~= 0 then
		bad += 1
	end
end
print(bad)
`, 20+layoutLen-1)
	if got := g.run(t, main, map[string][]byte{"file": oracle}); got != "0" {
		t.Errorf("R9: %s layout bytes moved under the held hash did not refuse layout_malformed", got)
	}
	// F10, R12: the LAST record's hash names nothing
	_, rec := fx1Offsets(oracle)
	stride := (len(oracle) - rec) / 2
	last := clone(oracle)
	last[rec+stride] ^= 1
	if got, want := evoLoad(t, g, "FX1Table", "FxRoot", last, -1).head, refusedTotal("no_layout", false, "-"); got != want {
		t.Errorf("F10/R12: %s, want %s", got, want)
	}
	// an older lineage entry selects its own plan; the reader's own file selects
	// the last entry
	older, newer := readTestSchema(t, "VOLD_field_append"), readTestSchema(t, "VNEW_field_append")
	gv := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	if got := evoLoad(t, gv, "VNEW_field_appendTable", "Lineage", corpusFile(t, corpus, "old_field_append.bin"), -1); got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.w"] != "77" {
		t.Errorf("the older entry: %s %v", got.head, got.dump)
	}
	if got := evoLoad(t, gv, "VNEW_field_appendTable", "Lineage", corpusFile(t, corpus, "new_field_append.bin"), -1).head; got != "ok 1 u0 k0 c0 w0 d0 -" {
		t.Errorf("the reader's own file: %s", got)
	}
}

// TestMooFixedEvolutionCompiledPlans is compiled-plans (R1, R2, R23, F11,
// R25, R26, W14): the build lays the lineage down as static data, oldest
// first and its own layout last, each entry's record body the handed WHOLE
// record less the hash, once; a plan is compiled per entry and read through
// (an older u16 widens, an i32 sign-extends into an i64, a field the writer
// never wrote lands its declared default); a stale report is reset on entry and
// the hash reported only for layout_unknown and layout_unsupported; a layout
// past the runtime's plan bound refuses plan_too_large BY NAME on that entry
// alone; every emitted root's own plan compiles; and every member the save
// writes sits at the offset the compiler's layout walk gives it (moo's storage
// is its values, so the plan's constant offsets are the record's own).
func TestMooFixedEvolutionCompiledPlans(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u1 := unitFromSource(t, "package probe\nfixed table Lineage\n{\n    a uint16 = 1\n}\n")
	u2 := unitFromSource(t, "package probe\nfixed table Lineage\n{\n    a uint32 = 1\n    b int32 = 10\n}\n")
	u3 := unitFromSource(t, "package probe\nfixed table Lineage\n{\n    a uint32 = 1\n    b int64 = 10\n    c int32 = 99\n}\n")
	e1, e2, e3 := evoEntry(t, u1, "Lineage"), evoEntry(t, u2, "Lineage"), evoEntry(t, u3, "Lineage")
	if e1.Record != 8+2 || e2.Record != 8+8 || e3.Record != 8+16 {
		t.Fatalf("R2: a lineage entry's Record is the whole record: %d %d %d", e1.Record, e2.Record, e3.Record)
	}
	e0 := e1
	e0.Wire ^= 0xbeef
	e0.Retired = true
	// F11, R25: a layout past the runtime's plan bound (4096 entries), a root
	// table of 4100 int8 fields under a hash of its own; the compiler caps a
	// declared table's plan at the same bound, so only a lineage carries one
	const bigFields = 4100
	entry := func(id uint64, kind byte, size, children uint32) []byte {
		b := binary.LittleEndian.AppendUint64(nil, id)
		b = append(b, kind)
		b = binary.LittleEndian.AppendUint32(b, size)
		return binary.LittleEndian.AppendUint32(b, children)
	}
	bigLayout := binary.LittleEndian.AppendUint32(nil, bigFields+1)
	bigLayout = append(bigLayout, entry(0x1111, ir.TableKindTable, bigFields, bigFields)...)
	for i := range bigFields {
		bigLayout = append(bigLayout, entry(0x2000+uint64(i), ir.TableKindI8, 1, 0)...)
	}
	eBig := mootable.FixedLineageEntry{Wire: 0xb16b16b16b16b16b, Layout: bigLayout, Record: 8 + bigFields}
	lineage := map[string][]mootable.FixedLineageEntry{"Lineage": {e0, eBig, e1, e2}}
	g := fixedUnit(t, u3, lineage)
	src := string(g.files["ProbeTable.moo"])
	// R1: oldest first, own last; R2: each body is the handed record less eight
	at := strings.Index(src, "\tlineage = {\n")
	if at < 0 {
		t.Fatalf("R1: no lineage in the static data")
	}
	for _, e := range []mootable.FixedLineageEntry{e0, eBig, e1, e2, e3} {
		line := fmt.Sprintf("recordBytes = %d, retired = %v, layout = ", e.Record-8, e.Retired)
		i := strings.Index(src[at:], fmt.Sprintf("-- 0x%016x\n", e.Wire))
		if i < 0 || !strings.Contains(src[at:at+i], line) {
			t.Fatalf("R1/R2: the lineage entry 0x%016x is not laid down in order with %q", e.Wire, line)
		}
		at += i + 1
	}
	if !strings.Contains(src, "\tfloor = 1,\n") || !strings.Contains(src, "\trecordBytes = 16,\n") {
		t.Fatalf("R1: the floor or the own record body is not the static data's")
	}
	v1 := evoFile(e1.Wire, e1.Layout, []byte{7, 0}, []byte{8, 0})
	b2 := func(a uint32, b int32) []byte {
		x := binary.LittleEndian.AppendUint32(nil, a)
		return binary.LittleEndian.AppendUint32(x, uint32(b))
	}
	v2 := evoFile(e2.Wire, e2.Layout, b2(100, -20), b2(200, 30))
	for _, c := range []struct {
		name string
		file []byte
		head string
		dump map[string]string
	}{
		{"v1", v1, "ok 2 u0 k0 c0 w2 d0 -", map[string]string{"r0.a": "7", "r0.b": "10", "r0.c": "99", "r1.a": "8"}},
		{"v2", v2, "ok 2 u0 k0 c0 w2 d0 -", map[string]string{"r0.a": "100", "r0.b": "-20", "r0.c": "99", "r1.b": "30"}},
		{"retired", evoFile(e0.Wire, e0.Layout, []byte{1, 0}), refusedTotal("layout_unsupported", false, evoHash(e0.Wire)), nil},
		{"unknown", evoFile(0x123456789abcdef0, e3.Layout, make([]byte, 16)), refusedTotal("layout_unknown", false, evoHash(0x123456789abcdef0)), nil},
		{"truncated", evoFile(e3.Wire, e3.Layout, make([]byte, 15)), refusedTotal("truncated", true, "-"), nil},
		{"plan bound", evoFile(eBig.Wire, eBig.Layout), refusedTotal("plan_too_large", false, "-"), nil},
	} {
		got := evoLoad(t, g, "ProbeTable", "Lineage", c.file, -1)
		if got.head != c.head {
			t.Errorf("%s: %s, want %s", c.name, got.head, c.head)
		}
		for k, want := range c.dump {
			if got.dump[k] != want {
				t.Errorf("%s: %s landed %q, want %s", c.name, k, got.dump[k], want)
			}
		}
	}
	// R26: every lineage entry but the over-bound one compiles, and the check
	// names that one by its refusal
	check := "--!strict\nlocal F = require(\"./table_fixed\")\n" + g.requireAll() + "local T = require(\"./ProbeTable\")\nprint(tostring(F.check(T.LineageDescriptor, T.LineageFixed)))\n"
	if got := g.run(t, check, nil); got != "plan_too_large" {
		t.Errorf("R26: the lineage check: %s", got)
	}
	gOwn := fixedUnit(t, u3, map[string][]mootable.FixedLineageEntry{"Lineage": {e1, e2}})
	if got := gOwn.run(t, strings.ReplaceAll(check, "g.requireAll()", ""), nil); got != "nil" {
		t.Errorf("R26: every lawful entry and the own layout compile: %s", got)
	}
	// W14: every member the save writes at the compiler's offset
	uw := unitFromSource(t, "package probe\ntype Inner\n{\n    p int32\n    q int16\n}\nfixed table W\n{\n    a uint8\n    b int32\n    inner Inner\n    opt ?int32\n    arr [..3]int16\n    s string(5)\n    tail uint32\n}\n")
	gw := fixedUnit(t, uw, nil)
	file := evoSave(t, gw, "ProbeTable", "W", `local v = T.NewW()
v.A = 0xA1
v.B = 0x0B0B0B0B
v.Inner.P = 0x1E1E1E1E
v.Inner.Q = 0x2E2E
v.OptPresent = true
v.Opt = 0x0D0D0D0D
v.Arr = { 0x3E3E, 0x4E4E, 0 }
v.ArrCount = 2
v.S = "seven"
v.Tail = 0x7A7A7A7A
values = { v }
`)
	// the wire offset of each root field, summed from the compiler's own
	// layout walk (each field's subtree consumed whole)
	walk := ir.TableFixedWalkRoot(uw.Tables["W"])
	var span func(i int) int
	span = func(i int) int {
		n := 1
		for range walk[i].Children {
			n += span(i + n)
		}
		return n
	}
	offset := map[string]int{}
	names := []string{"a", "b", "inner", "opt", "arr", "s", "tail"}
	for i, k, off := 1, 0, 0; i < len(walk); k++ {
		offset[names[k]] = off
		off += int(walk[i].Size)
		i += span(i)
	}
	body := file[len(file)-int(ir.TableFixedTypeBytes(uw.Tables["W"])):]
	u32 := func(at int) uint32 { return binary.LittleEndian.Uint32(body[at:]) }
	for name, ok := range map[string]bool{
		"a":           body[offset["a"]] == 0xA1,
		"b":           u32(offset["b"]) == 0x0B0B0B0B,
		"inner":       u32(offset["inner"]) == 0x1E1E1E1E,
		"opt_present": body[offset["opt"]] == 1,
		"opt":         u32(offset["opt"]+1) == 0x0D0D0D0D,
		"arr_count":   u32(offset["arr"]) == 2,
		"arr":         u32(offset["arr"]+4) == 0x4E4E3E3E,
		"s_length":    u32(offset["s"]) == 5,
		"s":           string(body[offset["s"]+4:][:5]) == "seven",
		"tail":        u32(offset["tail"]) == 0x7A7A7A7A,
	} {
		if !ok {
			t.Errorf("W14: the member %s is not at the layout walk's offset: %v %x", name, offset, body)
		}
	}
}

// TestMooFixedEvolutionDefinitionHash is definition-hash (R4, R5, W11, W12):
// the hash moo lays down is the compiler's (ir.TableFixedLayoutHash) and is
// the one the reference's corpus file carries; the runtime holds hashes as
// handed constants and never derives one — a lineage entry handed under a
// hash that is NOT its layout's hash still selects its file; the digest moves
// with a range and a resolution and carries the resolution under 'Q'; bytes(N)
// is layout kind 14; the entry count word is inside the hash.
func TestMooFixedEvolutionDefinitionHash(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	const schema = "package probe\nfixed table T\n{\n    b bytes(6)\n    x int32 = 1 | min = -10, max = 10\n    f float32 = 0.0 | min = -1.0, max = 1.0, resolution = 0.25\n}\n"
	u := unitFromSource(t, schema)
	st := u.Tables["T"]
	walk := ir.TableFixedWalkRoot(st)
	if walk[1].Kind != ir.TableKindArray {
		t.Errorf("W11: bytes(6) is layout kind %d, want %d", walk[1].Kind, ir.TableKindArray)
	}
	e := evoEntry(t, u, "T")
	flipped := clone(e.Layout)
	flipped[0] ^= 1
	if ir.TableFixedLayoutHash(flipped, st) == e.Wire {
		t.Errorf("W12: the count word is not inside the hash")
	}
	if d := ir.TableFixedDefinitionsDigest(st); !bytes.Contains(d, []byte{'Q'}) {
		t.Errorf("R5: the digest does not carry the resolution under 'Q': %x", d)
	}
	for _, other := range []string{
		strings.Replace(schema, "max = 10", "max = 11", 1),
		strings.Replace(schema, "resolution = 0.25", "resolution = 0.125", 1),
		strings.Replace(schema, "bytes(6)", "bytes(8)", 1),
		strings.Replace(schema, "    x int32", "    y int32", 1),
	} {
		if evoEntry(t, unitFromSource(t, other), "T").Wire == e.Wire {
			t.Errorf("R5: the hash did not move:\n%s", other)
		}
	}
	g := fixedUnit(t, u, nil)
	src := string(g.files["ProbeTable.moo"]) + string(g.files["table_fixed.moo"])
	for _, c := range []string{"0x100000001b3", "1099511628211", "cbf29ce484222325", "14695981039346656037"} {
		if strings.Contains(src, c) {
			t.Errorf("R4: the emitted moo carries the FNV-1a constant %s", c)
		}
	}
	if !strings.Contains(src, "hash = "+mootable.IdLit(e.Wire)) {
		t.Errorf("R4: the emitted hash is not the compiler's 0x%016x", e.Wire)
	}
	// R4: an entry handed under a hash that is not its layout's still selects
	older := unitFromSource(t, "package probe\nfixed table T\n{\n    x int32 = 1\n}\n")
	eo := evoEntry(t, older, "T")
	eo.Wire = 0x0123456789abcdef
	gl := fixedUnit(t, u, map[string][]mootable.FixedLineageEntry{"T": {eo}})
	if got := evoLoad(t, gl, "ProbeTable", "T", evoFile(eo.Wire, eo.Layout, []byte{5, 0, 0, 0}), -1); got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.x"] != "5" {
		t.Errorf("R4: the handed hash: %s %v", got.head, got.dump)
	}
	// the corpus: moo's own hash and layout are the reference's
	corpus := fixedCorpus(t)
	for _, v := range fixedOracleVectors {
		uv := unitFromPath(t, v.schema)
		ev := evoEntry(t, uv, v.root)
		data := corpusFile(t, corpus, v.file)
		if fileHash(data) != ev.Wire || !bytes.Equal(data[20:20+len(ev.Layout)], ev.Layout) {
			t.Errorf("%s: the compiler's layout and hash are not the reference's", v.file)
		}
	}
}

// TestMooFixedEvolutionClosure is fixed-closure (R6, R14, R22, W10): a table
// past the 65536-byte record ceiling is named at compile and no fixed form is
// emitted for it (a handed lineage for it ignored) while its neighbour's is; a
// lineage entry with an entry past 65536, a subtree summing past it, or an
// entry reaching past the writer's own record refuses layout_record_too_large
// whole, and the entry ending exactly at the record reads; a fixed table nested
// by value, by array and by option round-trips; a variable table, pointer,
// map, unbounded array or guarded branch in the closure is a compile refusal
// naming the construct.
func TestMooFixedEvolutionClosure(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	t.Run("R6", func(t *testing.T) {
		t.Parallel()
		u := unitFromSource(t, "package probe\nfixed table Big\n{\n    blob bytes(70000)\n}\nfixed table Small\n{\n    x int32 = 7\n}\n")
		warns, errs := ir.TableFixedRecordBounds(u, 0)
		if len(errs) != 0 || len(warns) != 1 || !strings.Contains(warns[0], "Big") || !strings.Contains(warns[0], "65536") {
			t.Fatalf("R6: the ceiling is not named: %v %v", warns, errs)
		}
		if _, ok := mootable.FixedLineageOf(u, "Big"); ok {
			t.Fatalf("R6: Big has a fixed lineage entry")
		}
		small := evoEntry(t, u, "Small")
		g := fixedUnit(t, u, map[string][]mootable.FixedLineageEntry{"Big": {small}, "Small": {small}})
		src := string(g.files["ProbeTable.moo"])
		if strings.Contains(src, "BigFixed") || !strings.Contains(src, "SmallFixed") {
			t.Fatalf("R6: the fixed form is emitted for Big or not for Small")
		}
		file := evoSave(t, g, "ProbeTable", "Small", "local v = T.NewSmall()\nv.X = 42\nvalues = { v }\n")
		if got := evoLoad(t, g, "ProbeTable", "Small", file, -1); got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.x"] != "42" {
			t.Fatalf("R6: Small: %s %v", got.head, got.dump)
		}
	})
	t.Run("R14_W10", func(t *testing.T) {
		t.Parallel()
		u := unitFromSource(t, "package probe\nfixed table Target\n{\n    lead uint32 = 1\n    name string(8) = \"ok\"\n    trail uint32 = 2\n}\nfixed table Boundary\n{\n    lead uint32 = 1\n    name string(8) = \"ok\"\n}\n")
		base := evoEntry(t, u, "Target")
		bound := evoEntry(t, u, "Boundary")
		bound.Wire ^= 0x1111
		size := func(e *mootable.FixedLineageEntry, i int, n uint32) {
			binary.LittleEndian.PutUint32(e.Layout[4+17*i+9:], n)
		}
		mk := func(x uint64, f func(e *mootable.FixedLineageEntry)) mootable.FixedLineageEntry {
			e := base
			e.Wire ^= x
			e.Layout = clone(base.Layout)
			f(&e)
			e.Record = 8 + int64(binary.LittleEndian.Uint32(e.Layout[4+9:]))
			return e
		}
		hugeField := mk(0x0303, func(e *mootable.FixedLineageEntry) { size(e, 0, 65536); size(e, 2, 65537) })
		childSum := mk(0x0404, func(e *mootable.FixedLineageEntry) { size(e, 0, 65536); size(e, 2, 65530) })
		reach := mk(0x0505, func(e *mootable.FixedLineageEntry) { size(e, 0, 15) })
		g := fixedUnit(t, u, map[string][]mootable.FixedLineageEntry{"Boundary": {bound}, "Target": {hugeField, childSum, reach}})
		var bb [16]byte
		binary.LittleEndian.PutUint32(bb[0:], 9)
		binary.LittleEndian.PutUint32(bb[4:], 8)
		copy(bb[8:], "edgecase")
		if got := evoLoad(t, g, "ProbeTable", "Boundary", evoFile(bound.Wire, bound.Layout, bb[:]), -1); got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.name"] != `"edgecase"` {
			t.Errorf("W10: the entry ending at the record: %s %v", got.head, got.dump)
		}
		for name, e := range map[string]mootable.FixedLineageEntry{"hugeField": hugeField, "childSum": childSum, "reach": reach} {
			if got, want := evoLoad(t, g, "ProbeTable", "Target", evoFile(e.Wire, e.Layout), -1).head, refusedTotal("layout_record_too_large", false, "-"); got != want {
				t.Errorf("R14 %s: %s, want %s", name, got, want)
			}
		}
	})
	t.Run("R22", func(t *testing.T) {
		t.Parallel()
		u := unitFromSource(t, "package probe\nfixed table Inner\n{\n    a int32 = 10\n    b string(4) = \"hi\"\n}\nfixed table Outer\n{\n    head uint32 = 1\n    inner Inner\n    items [2]Inner\n    maybe ?Inner\n    tail uint32 = 2\n}\n")
		g := fixedUnit(t, u, nil)
		file := evoSave(t, g, "ProbeTable", "Outer", `local v = T.NewOuter()
v.Head = 7
v.Inner.A = 11
v.Inner.B = "one"
v.Items[2].A = 33
v.Items[2].B = "six"
v.MaybePresent = true
v.Maybe.A = 44
v.Tail = 9
values = { v }
`)
		got := evoLoad(t, g, "ProbeTable", "Outer", file, -1)
		for k, want := range map[string]string{"r0.head": "7", "r0.inner.a": "11", "r0.inner.b": `"one"`, "r0.items[1].a": "33", "r0.items[1].b": `"six"`, "r0.items[0].b": `"hi"`, "r0.maybe_present": "1", "r0.maybe.a": "44", "r0.tail": "9"} {
			if got.dump[k] != want {
				t.Errorf("R22: %s landed %q, want %s", k, got.dump[k], want)
			}
		}
		if got.head != "ok 1 u0 k0 c0 w0 d0 -" || !bytes.Equal(got.saved, file) {
			t.Errorf("R22: the nested closure does not round-trip: %s", got.head)
		}
		for _, c := range []struct{ field, want, src string }{
			{"Outer.child", "holds the variable table VarChild by value", "package probe\ntable VarChild { x int32 }\nfixed table Outer { child VarChild }\n"},
			{"Outer.ptr", "is a pointer", "package probe\ntable Node { x int32 }\nfixed table Outer { ptr *Node }\n"},
			{"Outer.m", "is a map", "package probe\nfixed table Outer { m map[uint32]int32 }\n"},
			{"Outer.arr", "is an unbounded array", "package probe\nfixed table Outer { arr []int32 }\n"},
			{"Sub.extra", "sits in an `if` branch", "package probe\ntype Sub {\n    gate bool\n    if gate {\n        extra int32\n    }\n}\nfixed table Outer { sub Sub }\n"},
		} {
			msg := evoUnitRefusal(t, c.src)
			if !strings.Contains(msg, c.field) || !strings.Contains(msg, c.want) || !strings.Contains(msg, "docs/SPEC-TABLES.md") {
				t.Errorf("R22 %s: the refusal is %q", c.field, msg)
			}
		}
		if evoUnitRefusal(t, "package probe\nfixed table T { next T }\n") == "" {
			t.Errorf("R22: a fixed table that reaches itself by value is not refused")
		}
	})
}

// TestMooFixedEvolutionRetirement is retirement (R3, R32): the floor laid down
// is 1 + the highest retired index (0 when none, the retired marks laid down
// beside each entry); below it a file refuses layout_unsupported reporting its
// hash, at and above it reads, a raised floor refuses yesterday's file, and
// the refusal is the same on every load.
func TestMooFixedEvolutionRetirement(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older, mid, newer := readTestSchema(t, "VOLD_floor"), readTestSchema(t, "VMID_floor"), readTestSchema(t, "VNEW_floor")
	for retire, floor := range []int{0, 1, 2} {
		g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older, mid}, retire))
		if src := string(g.files["VNEW_floorTable.moo"]); !strings.Contains(src, fmt.Sprintf("\tfloor = %d,\n", floor)) || strings.Count(src, "retired = true") != retire {
			t.Errorf("R3: %d retired lays down floor %d", retire, floor)
		}
		for i, file := range []string{"old_floor.bin", "mid_floor.bin", "new_floor.bin"} {
			data := corpusFile(t, corpus, file)
			want := "ok 1 u0 k0 c0 w0 d0 -"
			if i < floor {
				want = refusedTotal("layout_unsupported", false, evoHash(fileHash(data)))
			}
			for range 2 {
				if got := evoLoad(t, g, "VNEW_floorTable", "Floored", data, -1).head; !strings.HasPrefix(got, strings.Split(want, " w")[0]) {
					t.Errorf("floor %d, %s: %s, want %s", floor, file, got, want)
				}
			}
		}
	}
}

// TestMooFixedEvolutionRetiredRuntime is retired-runtime (R10, R15): the
// runtime never walks a stranger's layout — an older reader given the newer
// writer's file refuses layout_unknown with the file's hash, and the newer
// layout's bytes under the reader's own hash are layout_malformed, never
// parsed — and the four forward-read clamps are retired: a newer file that
// grows a counted array, widens a range, appends a variant or appends a field
// is refused by name before any record, never clamped, remapped or dropped.
func TestMooFixedEvolutionRetiredRuntime(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	for _, row := range []string{"field_append", "array_bounded_grow", "range_widen", "enum_append"} {
		older := readTestSchema(t, "VOLD_"+row)
		rootName := rootOf(t, older)
		g := fixedUnit(t, older, nil)
		data := corpusFile(t, corpus, "new_"+row+".bin")
		if got, want := evoLoad(t, g, "VOLD_"+row+"Table", rootName, data, -1).head, refusedTotal("layout_unknown", false, evoHash(fileHash(data))); got != want {
			t.Errorf("R15 %s: %s, want %s", row, got, want)
		}
		// the newer layout under the reader's own hash: compared, never walked
		// — a layout that differs is layout_malformed; one whose bytes are
		// the same (the edit moved only the digest) reaches the records,
		// whose hash still names the stranger
		own := evoEntry(t, older, rootName)
		stranger := clone(data)
		binary.LittleEndian.PutUint64(stranger[8:], own.Wire)
		want := refusedTotal("layout_malformed", false, "-")
		if L := int(binary.LittleEndian.Uint32(data[16:20])); bytes.Equal(data[20:20+L], own.Layout) {
			want = refusedTotal("no_layout", false, "-")
		}
		if got := evoLoad(t, g, "VOLD_"+row+"Table", rootName, stranger, -1).head; got != want {
			t.Errorf("R10 %s: the stranger's layout under the held hash: %s, want %s", row, got, want)
		}
	}
}

// TestMooFixedEvolutionNumericWidening is numeric-evolution (E3, C8, R20,
// R21): every widening row of the corpus reads its old_ file under the
// lineage with the manifest's values and the widened counter
// (TestMooFixedVersioningNewReadsOld), and the landing rules hold value by
// value: int16 -> int32 sign-extends, a widened float is the float's exact
// widening, an added field lands its declared default with no counter. The
// negative controls break the widened count and the sign extension.
func TestMooFixedEvolutionNumericWidening(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	older := unitFromSource(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int16 = 0\n    f float32 = 0.0\n    trail uint32 = 2\n}\n")
	newer := unitFromSource(t, "package probe\nfixed table N\n{\n    lead uint32 = 1\n    v int32 = 0\n    f float64 = 0.0\n    trail uint32 = 2\n    added int32 = 77\n}\n")
	eo := evoEntry(t, older, "N")
	body := binary.LittleEndian.AppendUint32(nil, 5)
	body = binary.LittleEndian.AppendUint16(body, 0xFFF9) // -7
	body = binary.LittleEndian.AppendUint32(body, 0xC49A5200)
	body = binary.LittleEndian.AppendUint32(body, 6)
	file := evoFile(eo.Wire, eo.Layout, body, body)
	want := map[string]string{"r0.lead": "5", "r0.v": "-7", "r0.f": "0xC0934A4000000000", "r0.trail": "6", "r0.added": "77", "r1.v": "-7"}
	run := func(g *generatedUnit) fixedRun { return evoLoad(t, g, "ProbeTable", "N", file, -1) }
	lineage := map[string][]mootable.FixedLineageEntry{"N": {eo}}
	got := run(fixedUnit(t, newer, lineage))
	if got.head != "ok 2 u0 k0 c0 w4 d0 -" {
		t.Errorf("the widening read: %s, want two widened entries per record", got.head)
	}
	for k, w := range want {
		if got.dump[k] != w {
			t.Errorf("%s landed %q, want %s", k, got.dump[k], w)
		}
	}
	g := fixedUnit(t, newer, lineage)
	plant(t, g, "\tif n.size < (f.fw or kw) then\n\t\trep.widened += 1\n", "\tif false then\n\t\trep.widened += 1\n")
	if run(g).head == got.head {
		t.Errorf("the negative control: an integer widening not counted still reads clean")
	}
	g = fixedUnit(t, newer, lineage)
	plant(t, g, "\t\treturn if signed then buffer.readi16(rec, off) else buffer.readu16(rec, off)\n", "\t\treturn buffer.readu16(rec, off)\n")
	if run(g).dump["r0.v"] == "-7" {
		t.Errorf("the negative control: a zero-extended int16 still lands -7")
	}
}

// TestMooFixedEvolutionOptionalValues is optional-values (E5, W2): ?T rides a
// present byte in front of T, so ?Inner beside a plain Inner is a 17-byte
// body; an absent optional's payload is NOT stored — its bytes are zero even
// when the value in memory holds one — and reads back absent; a writer that
// held the field plain is read by a ?T reader as present. The negative control
// stores the absent payload.
func TestMooFixedEvolutionOptionalValues(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	const types = "package probe\ntype Inner\n{\n    a int32 = 5\n    b int32 = 6\n}\n"
	u := unitFromSource(t, types+"fixed table T\n{\n    plain Inner\n    opt ?Inner\n}\n")
	if e := evoEntry(t, u, "T"); e.Record != 8+17 {
		t.Fatalf("E5: the record is %d, want 8 + 17", e.Record)
	}
	const setup = "local v = T.NewT()\nv.Plain.A = 11\nv.Plain.B = 22\nv.Opt.A = 0x12345678\nv.Opt.B = 0x23456789\nv.OptPresent = false\nvalues = { v }\n"
	g := fixedUnit(t, u, nil)
	file := evoSave(t, g, "ProbeTable", "T", setup)
	body := file[len(file)-17:]
	if body[8] != 0 || !bytes.Equal(body[9:], make([]byte, 8)) {
		t.Errorf("W2: the absent payload is stored: %x", body)
	}
	if got := evoLoad(t, g, "ProbeTable", "T", file, -1); got.dump["r0.opt_present"] != "0" || got.dump["r0.plain.b"] != "22" {
		t.Errorf("W2: the absent optional reads %v", got.dump)
	}
	plain := unitFromSource(t, types+"fixed table T\n{\n    plain Inner\n    opt Inner\n}\n")
	ep := evoEntry(t, plain, "T")
	pb := make([]byte, 16)
	binary.LittleEndian.PutUint32(pb[8:], 3)
	binary.LittleEndian.PutUint32(pb[12:], 4)
	gl := fixedUnit(t, u, map[string][]mootable.FixedLineageEntry{"T": {ep}})
	if got := evoLoad(t, gl, "ProbeTable", "T", evoFile(ep.Wire, ep.Layout, pb), -1); got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.opt_present"] != "1" || got.dump["r0.opt.b"] != "4" {
		t.Errorf("E5: the plain writer read as ?T: %s %v", got.head, got.dump)
	}
	gc := fixedUnit(t, u, nil)
	plant(t, gc, "\t\t\tbuffer.writeu8(out, p.n.off + dx, if present then 1 else 0)\n\t\t\tif not present then\n\t\t\t\tcontinue\n", "\t\t\tbuffer.writeu8(out, p.n.off + dx, if present then 1 else 0)\n\t\t\tif false then\n\t\t\t\tcontinue\n")
	if b := evoSave(t, gc, "ProbeTable", "T", setup); bytes.Equal(b, file) {
		t.Errorf("the negative control: a stored absent payload still writes zeros")
	}
}

// TestMooFixedEvolutionFieldEvolution is field-evolution (E6, E8, R19): a
// rename under `was` reads by the declared identity in both directions on one
// hash; a deprecate and an undeprecate move no hash byte and read both ways;
// and a clean NEW-READS-OLD of an appended field, variant, arm, flag, nested
// field or keyed slot moves no counter at all. The id match's negative control
// is TestMooFixedVersioningNegativeControl.
func TestMooFixedEvolutionFieldEvolution(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	older, newer := readTestSchema(t, "VOLD_rename_without_was"), readTestSchema(t, "VNEW_rename_without_was")
	if evoEntry(t, older, "Lineage").Wire != evoEntry(t, newer, "Lineage").Wire {
		t.Fatalf("E6: a rename under was moved the hash")
	}
	got := evoLoad(t, fixedUnit(t, newer, nil), "VNEW_rename_without_wasTable", "Lineage", corpusFile(t, corpus, "old_rename_without_was.bin"), -1)
	if want := manifest["old_rename_without_was.bin"].values["r0.a"]; got.head != "ok 1 u0 k0 c0 w0 d0 -" || want == "" || got.dump["r0.b"] != want {
		t.Errorf("E6: the renamed field: %s b=%q, the writer's a=%q", got.head, got.dump["r0.b"], want)
	}
	for _, row := range []string{"field_deprecate", "field_undeprecate"} {
		o, n := readTestSchema(t, "VOLD_"+row), readTestSchema(t, "VNEW_"+row)
		if evoEntry(t, o, "Lineage").Wire != evoEntry(t, n, "Lineage").Wire {
			t.Errorf("E8 %s: the hash moved", row)
		}
		for _, c := range []struct {
			u    *ir.Unit
			mod  string
			file string
		}{{n, "VNEW_" + row + "Table", "old_" + row + ".bin"}, {o, "VOLD_" + row + "Table", "new_" + row + ".bin"}} {
			if got := evoLoad(t, fixedUnit(t, c.u, nil), c.mod, "Lineage", corpusFile(t, corpus, c.file), -1).head; got != "ok 1 u0 k0 c0 w0 d0 -" {
				t.Errorf("E8 %s %s: %s", row, c.file, got)
			}
		}
	}
	for _, row := range []string{"field_append", "enum_append", "union_append", "flags_append", "nested_append", "keyed_array_enum_append"} {
		o, n := readTestSchema(t, "VOLD_"+row), readTestSchema(t, "VNEW_"+row)
		file := "old_" + row + ".bin"
		got := evoLoad(t, fixedUnit(t, n, lineageOf(t, []*ir.Unit{o}, 0)), "VNEW_"+row+"Table", rootOf(t, o), corpusFile(t, corpus, file), -1)
		if got.head != fmt.Sprintf("ok %d u0 k0 c0 w0 d0 -", manifest[file].records) {
			t.Errorf("R19 %s: %s", row, got.head)
		}
		if compareManifest(t, file, manifest[file], got.dump) == 0 {
			t.Errorf("R19 %s: no manifest value compared", row)
		}
	}
}

// TestMooFixedEvolutionReports is reports (E9, R16, R18) under the one read
// (docs/FIXED-FORM-ALGORITHM.md §5.4, §5.9 #6 and #30): the fixed form never
// raises duplicate; a check that cannot fire (an unranged field, a range on
// its width's own limits) refuses nothing over every byte value and moves no
// counter; and there is NO COMPILE CENSUS — VNEW_unknown_census, a
// deliberately UNLAWFUL lineage entry (Item.drop removed), refuses
// layout_malformed over its three records, twice through one report, with
// every counter reset and nothing landed. The negative control drops the
// unlawful-entry refusal and requires the three records to land.
func TestMooFixedEvolutionReports(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	g := fixedUnit(t, unitFromSource(t, "package probe\nfixed table R\n{\n    a int8 = 0 | min = -128, max = 127\n    b uint8\n}\n"), nil)
	if src := string(g.files["table_fixed.moo"]); strings.Contains(src, "duplicate +=") || strings.Contains(src, "outOfRange +=") || strings.Contains(src, "kindChanged +=") {
		t.Errorf("E9: the fixed runtime moves a counter the one read retires")
	}
	var bodies [][]byte
	for x := range 256 {
		bodies = append(bodies, []byte{byte(x), byte(x)})
	}
	e := evoEntry(t, g.unit, "R")
	if got := evoLoad(t, g, "ProbeTable", "R", evoFile(e.Wire, e.Layout, bodies...), -1).head; got != "ok 256 u0 k0 c0 w0 d0 -" {
		t.Errorf("R18: a check that cannot fire refused or moved: %s", got)
	}
	older, newer := readTestSchema(t, "VOLD_unknown_census"), readTestSchema(t, "VNEW_unknown_census")
	file := corpusFile(t, corpus, "many_unknown_census.bin")
	main := func(gc *generatedUnit) string {
		return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal F = require(\"./table_fixed\")\n" + gc.requireAll() + fmt.Sprintf(`local T = require("./VNEW_unknown_censusTable")
local rep: any = { unknown = 9, widened = 9 }
for _ = 1, 2 do
	local v = F.load(T.%[1]sDescriptor, T.%[1]sFixed, inputs.file, rep)
	print(if v == nil then "nil" else tostring(#v), tostring(rep.reason), rep.unknown, rep.outOfRange, rep.widened, rep.repeatedKey)
end
`, rootOf(t, older))
	}
	gc := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	if got := strings.Join(strings.Fields(gc.run(t, main(gc), map[string][]byte{"file": file})), " "); got != "nil layout_malformed 0 0 0 0 nil layout_malformed 0 0 0 0" {
		t.Errorf("R16: the unlawful entry over three records, loaded twice: %q", got)
	}
	gn := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	plant(t, gn, "\tif census.unlawful > 0 then\n", "\tif false then\n")
	if got := strings.Join(strings.Fields(gn.run(t, main(gn), map[string][]byte{"file": file})), " "); strings.Contains(got, "layout_malformed") {
		t.Errorf("the negative control: without the unlawful-entry refusal the entry still refuses: %q", got)
	}
}

// TestMooFixedEvolutionArrayBounds is array-bounds (C1, C2, R11): the plan
// carries the WRITER's bound — a counted array the old writer bounded at 4 and
// the reader at 8 refuses count_over_bound for a count of -1 and of 7, and
// reads at exactly 4. The negative control bounds the count by the reader's 8.
func TestMooFixedEvolutionArrayBounds(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_array_bounded_grow"), readTestSchema(t, "VNEW_array_bounded_grow")
	base := corpusFile(t, corpus, "old_array_bounded_grow.bin")
	countAt := 20 + int(binary.LittleEndian.Uint32(base[16:20])) + 8 + 4
	with := func(n uint32) []byte {
		b := clone(base)
		binary.LittleEndian.PutUint32(b[countAt:], n)
		return b
	}
	lineage := lineageOf(t, []*ir.Unit{older}, 0)
	g := fixedUnit(t, newer, lineage)
	for _, c := range []struct {
		name  string
		count uint32
		want  string
	}{
		{"C1 count -1", 0xFFFFFFFF, refusedTotal("count_over_bound", false, "-")},
		{"C2 count 7 past the writer's 4", 7, refusedTotal("count_over_bound", false, "-")},
		{"R11 count 4, the writer's bound", 4, "ok 1 u0 k0 c0 w0 d0 -"},
	} {
		if got := evoLoad(t, g, "VNEW_array_bounded_growTable", "ArrayBoundedGrow", with(c.count), -1).head; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	gc := fixedUnit(t, newer, lineage)
	plant(t, gc, "if count < 0 or count > writerBound or count > f.bound then", "if count < 0 or count > f.bound then")
	if got := evoLoad(t, gc, "VNEW_array_bounded_growTable", "ArrayBoundedGrow", with(7), -1).head; got == refusedTotal("count_over_bound", false, "-") {
		t.Errorf("the negative control: the reader's bound still refuses the writer's overrun")
	}
}

// TestMooFixedEvolutionText is text (C3, C4, C5, R24): a length past the
// field's capacity refuses length_over_bound; ill-formed UTF-8 or a zero in
// the USED bytes, and an unpaired surrogate in a wstring's used units, refuse
// text_ill_formed by name; a hostile byte in the slack past the used length is
// not read and not a refusal. The negative control drops the UTF-8 check.
func TestMooFixedEvolutionText(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table T\n{\n    s string(4)\n}\n")
	const setup = "local v = T.NewT()\nv.S = \"ab\"\nvalues = { v }\n"
	g := fixedUnit(t, u, nil)
	clean := evoSave(t, g, "ProbeTable", "T", setup)
	body := len(clean) - 8
	cut := func(f func(b []byte)) []byte { b := clone(clean); f(b); return b }
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"C3 length 5 of 4", cut(func(b []byte) { b[body] = 5 }), refusedTotal("length_over_bound", false, "-")},
		{"C4 0xff in the used bytes", cut(func(b []byte) { b[body+4] = 0xff }), refusedTotal("text_ill_formed", false, "-")},
		{"C4 a zero in the used bytes", cut(func(b []byte) { b[body+5] = 0 }), refusedTotal("text_ill_formed", false, "-")},
		{"R24 0xff in the slack", cut(func(b []byte) { b[body+6], b[body+7] = 0xff, 0xff }), "ok 1 u0 k0 c0 w0 d0 -"},
	}
	for _, c := range cases {
		if got := evoLoad(t, g, "ProbeTable", "T", c.file, -1).head; got != c.want {
			t.Errorf("%s: %s, want %s", c.name, got, c.want)
		}
	}
	if got := evoLoad(t, g, "ProbeTable", "T", clean, -1); got.dump["r0.s"] != `"ab"` {
		t.Errorf("the control: %v", got.dump)
	}
	plant(t, g, "\tif not V.utf8(rec, at, length) then\n", "\tif false then\n")
	if got := evoLoad(t, g, "ProbeTable", "T", cases[1].file, -1).head; got == cases[1].want {
		t.Errorf("the negative control: ill-formed text still refuses")
	}
}

// TestMooFixedEvolutionScalarBounds is scalar-bounds (C7, R30) under the one
// read (docs/FIXED-FORM-ALGORITHM.md §4.6): a ranged int32, int128 and uint16
// read below or above its range REFUSES THE WHOLE LOAD out_of_range, each
// field on its own, nothing landed and no counter moved; at its ends and in
// range it lands. A compressed float rides as the float32 itself — saved
// unquantized, a value past its range refused out_of_range on read, -0.0
// landing as written — and its resolution is in the digest under 'Q', so a
// finer or a dropped triple reads the old writer's bits unchanged. The
// negative control drops the reader's range check and requires the forged
// value to land.
func TestMooFixedEvolutionScalarBounds(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nfixed table T\n{\n    x int32 = 0 | min = -5, max = 5\n    s int128 = 0 | min = -500, max = 500\n    u uint16 = 10 | min = 10, max = 1000\n}\n")
	e := evoEntry(t, u, "T")
	body := func(x int32, s int64, v uint16) []byte {
		b := binary.LittleEndian.AppendUint32(nil, uint32(x))
		b = binary.LittleEndian.AppendUint64(b, uint64(s))
		hi := uint64(0)
		if s < 0 {
			hi = ^uint64(0)
		}
		b = binary.LittleEndian.AppendUint64(b, hi)
		return binary.LittleEndian.AppendUint16(b, v)
	}
	if e.Record != 8+int64(len(body(0, 0, 0))) {
		t.Fatalf("the record body is %d, the test frames %d", e.Record-8, len(body(0, 0, 0)))
	}
	g := fixedUnit(t, u, nil)
	for _, c := range []struct {
		name    string
		b       []byte
		head    string
		x, s, v string
	}{
		{"in range", body(3, -42, 500), "ok 1 u0 k0 c0 w0 d0 -", "3", "-42", "500"},
		{"at the low ends", body(-5, -500, 10), "ok 1 u0 k0 c0 w0 d0 -", "-5", "-500", "10"},
		{"at the high ends", body(5, 500, 1000), "ok 1 u0 k0 c0 w0 d0 -", "5", "500", "1000"},
		{"x below", body(-6, 0, 10), refusedField("out_of_range", false, "T.x"), "", "", ""},
		{"x above", body(6, 0, 10), refusedField("out_of_range", false, "T.x"), "", "", ""},
		{"int128 below", body(0, -501, 10), refusedField("out_of_range", false, "T.s"), "", "", ""},
		{"int128 above", body(0, 501, 10), refusedField("out_of_range", false, "T.s"), "", "", ""},
		{"uint16 below", body(0, 0, 9), refusedField("out_of_range", false, "T.u"), "", "", ""},
		{"uint16 above", body(0, 0, 1001), refusedField("out_of_range", false, "T.u"), "", "", ""},
	} {
		got := evoLoad(t, g, "ProbeTable", "T", evoFile(e.Wire, e.Layout, c.b), -1)
		if got.head != c.head || got.dump["r0.x"] != c.x || got.dump["r0.s"] != c.s || got.dump["r0.u"] != c.v {
			t.Errorf("C7 %s: %s x=%s s=%s u=%s, want %s %s %s %s", c.name, got.head, got.dump["r0.x"], got.dump["r0.s"], got.dump["r0.u"], c.head, c.x, c.s, c.v)
		}
	}
	gc := fixedUnit(t, u, nil)
	plantEach(t, gc, dropRangeChecks...)
	if got := evoLoad(t, gc, "ProbeTable", "T", evoFile(e.Wire, e.Layout, body(-6, 0, 10)), -1); !strings.HasPrefix(got.head, "ok 1 ") || got.dump["r0.x"] != "-6" {
		t.Errorf("the negative control: without the range check the forged x still refuses: %s %v", got.head, got.dump)
	}

	const cf = "package probe\nfixed table Reading\n{\n    lead uint32 = 1\n    aim float32 = 0.0%s\n    trail uint32 = 2\n}\n"
	old := unitFromSource(t, fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.1"))
	if d := ir.TableFixedDefinitionsDigest(old.Tables["Reading"]); !bytes.Contains(d, []byte{'Q'}) {
		t.Errorf("R30: the digest does not carry the resolution under 'Q'")
	}
	gr := fixedUnit(t, old, nil)
	saved := evoSave(t, gr, "ProbeTable", "Reading", "local v = T.NewReading()\nv.Aim = 0x3e99999a\nvalues = { v }\n")
	at := len(saved) - 8
	if binary.LittleEndian.Uint32(saved[at:]) != 0x3e99999a {
		t.Errorf("R30: the compressed float is not saved as its float32 bits: %x", saved[at:])
	}
	for _, c := range []struct {
		bits      uint32
		head, aim string
	}{
		{0xc0000000, refusedField("out_of_range", false, "Reading.aim"), ""},
		{0x40800000, refusedField("out_of_range", false, "Reading.aim"), ""},
		{0xbf800000, "ok 1 u0 k0 c0 w0 d0 -", "0xBF800000"},
		{0x40000000, "ok 1 u0 k0 c0 w0 d0 -", "0x40000000"},
		{0x80000000, "ok 1 u0 k0 c0 w0 d0 -", "0x80000000"},
	} {
		f := clone(saved)
		binary.LittleEndian.PutUint32(f[at:], c.bits)
		if got := evoLoad(t, gr, "ProbeTable", "Reading", f, -1); got.head != c.head || got.dump["r0.aim"] != c.aim {
			t.Errorf("R30 0x%08x: %s aim=%s, want %s %s", c.bits, got.head, got.dump["r0.aim"], c.head, c.aim)
		}
	}
	lin := map[string][]mootable.FixedLineageEntry{"Reading": {evoEntry(t, old, "Reading")}}
	for name, src := range map[string]string{"finer": fmt.Sprintf(cf, " | min = -1.0, max = 2.0, resolution = 0.01"), "dropped": fmt.Sprintf(cf, "")} {
		gn := fixedUnit(t, unitFromSource(t, src), lin)
		if got := evoLoad(t, gn, "ProbeTable", "Reading", saved, -1); got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.aim"] != "0x3E99999A" {
			t.Errorf("R30 %s: %s aim=%s", name, got.head, got.dump["r0.aim"])
		}
	}
}

const ordinalOld = "package probe\nenum Tier { Bronze, Silver }\nunion Pick\n{\n    a int32\n    b int32\n}\nfixed table Lineage\n{\n    tier Tier\n    pick Pick\n    seq int32\n}\n"

// TestMooFixedEvolutionOrdinals is ordinals (C9, C10, R31) under the one read
// (docs/FIXED-FORM-ALGORITHM.md §4.6, fixes 9 and 14): an enum ordinal past the
// writer's variants, and a union tag past its arms, each REFUSE THE WHOLE LOAD
// out_of_range on the identity plan and on a compiled plan whose reader
// declares MORE variants and arms (the writer's count governs: 3 names a
// variant and an arm the new reader has, and still refuses). The last
// writer variant and arm land. The negative controls drop the tag refusal and
// the ordinal refusal in turn and require the forged file to land.
func TestMooFixedEvolutionOrdinals(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	uOld := unitFromSource(t, ordinalOld)
	uNew := unitFromSource(t, strings.Replace(strings.Replace(ordinalOld, "Silver }", "Silver, Gold }", 1), "    b int32\n", "    b int32\n    c int32\n", 1))
	eo := evoEntry(t, uOld, "Lineage")
	tier := evoFile(eo.Wire, eo.Layout, []byte{3, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	tag := evoFile(eo.Wire, eo.Layout, []byte{2, 3, 1, 0, 0, 0, 42, 0, 0, 0})
	clean := evoFile(eo.Wire, eo.Layout, []byte{2, 2, 9, 0, 0, 0, 42, 0, 0, 0})
	lin := map[string][]mootable.FixedLineageEntry{"Lineage": {eo}}
	for name, g := range map[string]*generatedUnit{"identity": fixedUnit(t, uOld, nil), "compiled": fixedUnit(t, uNew, lin)} {
		if got := evoLoad(t, g, "ProbeTable", "Lineage", tier, -1).head; got != refusedField("out_of_range", false, "Lineage.tier") {
			t.Errorf("%s, C9 the ordinal: %s", name, got)
		}
		if got := evoLoad(t, g, "ProbeTable", "Lineage", tag, -1).head; got != refusedField("out_of_range", false, "Lineage.pick") {
			t.Errorf("%s, C10 the tag: %s", name, got)
		}
		if c := evoLoad(t, g, "ProbeTable", "Lineage", clean, -1); c.head != "ok 1 u0 k0 c0 w0 d0 -" || c.dump["r0.tier"] != "2" || c.dump["r0.pick.b"] != "9" || c.dump["r0.seq"] != "42" {
			t.Errorf("%s, the control: %s %v", name, c.head, c.dump)
		}
	}
	g := fixedUnit(t, uNew, lin)
	plant(t, g, "\t\t\trefuse(\"out_of_range\", false, fieldName(named))\n\t\t\treturn u, nil, nil\n", "\t\t\treturn u, nil, nil\n")
	if got := evoLoad(t, g, "ProbeTable", "Lineage", tag, -1); !strings.HasPrefix(got.head, "ok 1 ") {
		t.Errorf("the negative control, the tag: without the refusal it still refuses: %s", got.head)
	}
	ge := fixedUnit(t, uNew, lin)
	plant(t, ge, "\t\t\trefuse(\"out_of_range\", false, fieldName(named))\n\t\t\treturn 0, nil, nil\n", "\t\t\treturn 0, nil, nil\n")
	if got := evoLoad(t, ge, "ProbeTable", "Lineage", tier, -1); !strings.HasPrefix(got.head, "ok 1 ") {
		t.Errorf("the negative control, the ordinal: without the refusal it still refuses: %s", got.head)
	}
}

// TestMooFixedEvolutionNormalization is normalization (C12, C13, R17) under
// the one read (docs/FIXED-FORM-ALGORITHM.md §4.5, fix 2), beside
// TestMooFixedHostileBoolBytes: NOTHING NORMALIZES. A bool byte 0x7f, and a
// present byte 0x5a, each refuse the whole load wire_damaged, nothing landed
// and no counter moved; bytes of 0 and 1 land false, true, absent and present.
// The negative control drops the bool check and requires 0x7f to land.
func TestMooFixedEvolutionNormalization(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	g := fixedUnit(t, unitFromSource(t, "package probe\nfixed table T\n{\n    flag bool\n    opt ?int32\n}\n"), nil)
	file := evoSave(t, g, "ProbeTable", "T", "local v = T.NewT()\nv.Opt = 99\nvalues = { v }\n")
	body := len(file) - 6
	with := func(flag, present byte) []byte {
		b := clone(file)
		b[body], b[body+1], b[body+2] = flag, present, 42
		return b
	}
	for _, c := range []struct {
		name          string
		file          []byte
		head          string
		flag, present string
	}{
		{"C12 a bool byte 0x7f", with(0x7f, 1), refusedField("wire_damaged", true, "T.flag"), "", ""},
		{"C12 a bool byte 0x02", with(2, 1), refusedField("wire_damaged", true, "T.flag"), "", ""},
		{"C13 a present byte 0x5a", with(1, 0x5a), refusedField("wire_damaged", true, "T.opt"), "", ""},
		{"0 and 0", with(0, 0), "ok 1 u0 k0 c0 w0 d0 -", "0", "0"},
		{"1 and 1", with(1, 1), "ok 1 u0 k0 c0 w0 d0 -", "1", "1"},
	} {
		got := evoLoad(t, g, "ProbeTable", "T", c.file, -1)
		if got.head != c.head || got.dump["r0.flag"] != c.flag || got.dump["r0.opt_present"] != c.present {
			t.Errorf("%s: %s %v", c.name, got.head, got.dump)
		}
	}
	if got := evoLoad(t, g, "ProbeTable", "T", with(1, 1), -1); got.dump["r0.opt"] != "42" {
		t.Errorf("R17: the payload behind a present byte of 1: %v", got.dump)
	}
	gc := fixedUnit(t, g.unit, nil)
	plant(t, gc, "\t\tif b > 1 then\n\t\t\trefuse(\"wire_damaged\", true, fieldName(named))\n", "\t\tif false then\n\t\t\trefuse(\"wire_damaged\", true, fieldName(named))\n")
	if got := evoLoad(t, gc, "ProbeTable", "T", with(0x7f, 1), -1); !strings.HasPrefix(got.head, "ok 1 ") {
		t.Errorf("the negative control: without the bool check 0x7f still refuses: %s", got.head)
	}
}

// TestMooFixedEvolutionLiveExtents is live-extents (C14, W4, W9): the bounds
// pass walks the LIVE extent only — out-of-range ordinals in a counted array's
// slack and in an absent optional's payload are not read, clamped or counted —
// and a field the writer never wrote lands the reader's declared default.
func TestMooFixedEvolutionLiveExtents(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	g := fixedUnit(t, unitFromSource(t, "package probe\nenum Small { One, Two }\nfixed table T\n{\n    arr [..3]Small\n    opt ?Small\n    tail int32 = 77\n}\n"), nil)
	file := evoSave(t, g, "ProbeTable", "T", "local v = T.NewT()\nv.Arr[1] = 1\nv.ArrCount = 1\nvalues = { v }\n")
	body := len(file) - 13
	file[body+5], file[body+6], file[body+8] = 99, 99, 99
	if got := evoLoad(t, g, "ProbeTable", "T", file, -1); got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.arr_count"] != "1" || got.dump["r0.arr[0]"] != "1" || got.dump["r0.opt_present"] != "0" || got.dump["r0.tail"] != "77" {
		t.Errorf("C14/W4: %s %v", got.head, got.dump)
	}
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_field_append"), readTestSchema(t, "VNEW_field_append")
	gv := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	if got := evoLoad(t, gv, "VNEW_field_appendTable", "Lineage", corpusFile(t, corpus, "old_field_append.bin"), -1); got.dump["r0.w"] != "77" {
		t.Errorf("W9: the unwritten field: %v", got.dump)
	}
}

// TestMooFixedEvolutionWriting is writing (W1, W3, W5, W8): every byte the
// save does not write is zero — a counted array's slack, a string's and a
// bytes(N)'s slack, the bytes behind a narrower union arm — on every record of
// a batch, whatever an earlier record left; bytes(N) takes its u32 length and
// N bytes. moo checks the write contract on every save (W5's checks are
// release-on here): an out-of-contract value refuses by name and writes
// nothing. The negative control drops the per-record zeroing.
func TestMooFixedEvolutionWriting(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, "package probe\nunion Pick\n{\n    narrow uint8\n    wide uint64\n}\nfixed table T\n{\n    arr [..3]uint16\n    s string(4)\n    b bytes(4)\n    pick Pick\n}\n")
	const setup = `local full = T.NewT()
full.Arr = { 0x9999, 0x9999, 0x8888 }
full.ArrCount = 3
full.S = "ZZZZ"
buffer.fill(full.B, 0, 0xaa)
full.BLength = 4
full.Pick.Type = 2
full.Pick.Wide = V.Uint64.new(0xffffffff, 0xffffffff)
local v = T.NewT()
v.Arr = { 0x1122, 0x9999, 0x8888 }
v.ArrCount = 1
v.S = "A"
buffer.fill(v.B, 0, 0xaa)
buffer.writeu8(v.B, 0, 0x42)
v.BLength = 1
v.Pick.Type = 1
v.Pick.Narrow = 7
v.Pick.Wide = V.Uint64.new(0xffffffff, 0xffffffff)
values = { full, v }
`
	g := fixedUnit(t, u, nil)
	file := evoSave(t, g, "ProbeTable", "T", setup)
	body := file[len(file)-35:]
	want := []byte{1, 0, 0, 0, 0x22, 0x11, 0, 0, 0, 0, 1, 0, 0, 0, 'A', 0, 0, 0, 1, 0, 0, 0, 0x42, 0, 0, 0, 1, 7, 0, 0, 0, 0, 0, 0, 0}
	if !bytes.Equal(body, want) {
		t.Errorf("W1/W3/W8: the second record is\n%x, want\n%x", body, want)
	}
	refuse := g.run(t, "--!strict\nlocal F = require(\"./table_fixed\")\n"+g.requireAll()+`local T = require("./ProbeTable")
local v = T.NewT()
v.ArrCount = 4
local out, why = F.save(T.TDescriptor, T.TFixed, { v })
print(tostring(out) .. " " .. why.name .. " " .. tostring(why.field))
`, nil)
	if refuse != "nil invalid_value T.arr" {
		t.Errorf("W5: an out-of-contract count: %s", refuse)
	}
	gc := fixedUnit(t, u, nil)
	plant(t, gc, "\t\t\tbuffer.fill(rec, 0, 0)\n", "\t\t\tlocal _ = rec\n")
	if got := evoSave(t, gc, "ProbeTable", "T", setup); bytes.Equal(got, file) {
		t.Errorf("the negative control: an unzeroed record image still writes zero slack")
	}
}

const guardOld = "package probe\nunion Inner\n{\n    x int16\n    y int32\n}\nunion Outer\n{\n    inner Inner\n    plain int32\n}\nfixed table T\n{\n    outer Outer\n    tail int32 = 9\n}\n"

// TestMooFixedEvolutionUnionGuards is union-guards (W7, W16, R28): an arm
// inside an arm answers to the OUTER tag — when the outer union selects
// `plain`, the bytes under the inner union's tag (8, past its arms) are never
// read, widened or clamped — and the selected nested arm widens (int16 ->
// int32) exactly once.
func TestMooFixedEvolutionUnionGuards(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	uOld := unitFromSource(t, guardOld)
	uNew := unitFromSource(t, strings.Replace(guardOld, "x int16", "x int32", 1))
	eo := evoEntry(t, uOld, "T")
	file := evoFile(eo.Wire, eo.Layout, []byte{1, 1, 0xf9, 0xff, 0, 0, 100, 0, 0, 0}, []byte{2, 8, 3, 2, 1, 0, 200, 0, 0, 0})
	got := evoLoad(t, fixedUnit(t, uNew, map[string][]mootable.FixedLineageEntry{"T": {eo}}), "ProbeTable", "T", file, -1)
	if got.head != "ok 2 u0 k0 c0 w1 d0 -" {
		t.Errorf("%s", got.head)
	}
	for k, want := range map[string]string{"r0.outer.type": "1", "r0.outer.inner.type": "1", "r0.outer.inner.x": "-7", "r0.tail": "100", "r1.outer.type": "2", "r1.outer.plain": "16909064", "r1.tail": "200"} {
		if got.dump[k] != want {
			t.Errorf("%s landed %q, want %s", k, got.dump[k], want)
		}
	}
	if _, ok := got.dump["r1.outer.inner.type"]; ok {
		t.Errorf("the unselected inner union landed")
	}
}

const planSchema = "package probe\nenum Kind { A, B }\nunion Choice\n{\n    n int32\n    s string(4)\n}\nfixed table T\n{\n    head int32 = 1\n    opt ?int32\n    arr [..2]Kind\n    ch Choice\n}\n"

// TestMooFixedEvolutionPlanExecution is plan-execution (W6, P1): the identity
// read and a compiled plan over the same layout under another hash land the
// same values, the same counters and save the same bytes. moo's plan is one
// walk with no partition: an entry under a guard is reached only through its
// union's selected arm (TestMooFixedEvolutionUnionGuards).
func TestMooFixedEvolutionPlanExecution(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, planSchema)
	own := evoEntry(t, u, "T")
	forged := own
	forged.Wire ^= 1
	g := fixedUnit(t, u, map[string][]mootable.FixedLineageEntry{"T": {forged}})
	idBytes := evoSave(t, g, "ProbeTable", "T", "local v = T.NewT()\nv.Head = 42\nv.Opt = -99\nv.OptPresent = true\nv.Arr = { 2, 0 }\nv.ArrCount = 1\nv.Ch.Type = 2\nv.Ch.S = \"hi\"\nvalues = { v, v }\n")
	planBytes := clone(idBytes)
	planBytes[8] ^= 1
	stride := int(own.Record)
	for at := len(planBytes) - 2*stride; at < len(planBytes); at += stride {
		planBytes[at] ^= 1
	}
	id := evoLoad(t, g, "ProbeTable", "T", idBytes, -1)
	plan := evoLoad(t, g, "ProbeTable", "T", planBytes, -1)
	if id.head != "ok 2 u0 k0 c0 w0 d0 -" || plan.head != id.head || fmt.Sprint(id.dump) != fmt.Sprint(plan.dump) || !bytes.Equal(id.saved, idBytes) || !bytes.Equal(plan.saved, idBytes) {
		t.Errorf("P1: identity %s %v, compiled %s %v", id.head, id.dump, plan.head, plan.dump)
	}
	if id.dump["r0.ch.s"] != `"hi"` || id.dump["r1.opt"] != "-99" {
		t.Errorf("P1: the values: %v", id.dump)
	}
}

// TestMooFixedEvolutionInteroperability is interoperability (W13, P2, R27): for
// every vector of the reference's corpus the static data moo emits carries the
// file's hash and layout bytes exactly, the file reads with the manifest's
// record count and values, and the save writes the file back byte for byte
// (TestMooFixedFormAgreesWithTheByteOracle holds every value). The negative
// control moves one byte of a record body and requires the round trip to
// notice.
func TestMooFixedEvolutionInteroperability(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	for _, v := range fixedOracleVectors {
		t.Run(strings.TrimSuffix(v.file, ".bin"), func(t *testing.T) {
			t.Parallel()
			g := fixedUnit(t, unitFromPath(t, v.schema), nil)
			mod := strings.TrimSuffix(filepath.Base(v.schema), ".schema") + "Table"
			data := corpusFile(t, corpus, v.file)
			main := "--!strict\nlocal F = require(\"./table_fixed\")\nlocal V = require(\"./table_values\")\n" + g.requireAll() + fmt.Sprintf(`local T = require("./%s")
local fixed = T.%sFixed
local own = fixed.lineage[#fixed.lineage]
local parts = {}
for i = 1, #own.layout do
	parts[i] = string.format("%%02x", string.byte(own.layout, i) :: number)
end
print(V.idHex(fixed.hash) .. " " .. table.concat(parts))
local _ = F
`, mod, v.root)
			L := int(binary.LittleEndian.Uint32(data[16:20]))
			want := fmt.Sprintf("%s %x", evoHash(fileHash(data)), data[20:20+L])
			if got := g.run(t, main, nil); got != want {
				t.Fatalf("W13: the emitted static data is not the reference's header")
			}
			ml := manifest[v.file]
			got := evoLoad(t, g, mod, v.root, data, -1)
			if got.head != fmt.Sprintf("ok %d u0 k0 c0 w0 d0 -", ml.records) || compareManifest(t, v.file, ml, got.dump) == 0 || !bytes.Equal(got.saved, data) {
				t.Fatalf("P2/R27: %s", got.head)
			}
			moved := clone(data)
			moved[20+L+8] ^= 1
			if m := evoLoad(t, g, mod, v.root, moved, -1); strings.HasPrefix(m.head, "ok ") && bytes.Equal(m.saved, data) {
				t.Fatalf("the negative control: a moved body byte round-trips to the reference's file")
			}
		})
	}
}

const hostSchema = "package probe\nfixed table Host\n{\n    small int32 = 5 | min = 0, max = 1000\n    marks [..4]int32 | min = 0, max = 1000\n    note ?int32 | min = 0, max = 1000\n    tail int32 = 7\n}\n"

// hostSweep poisons every byte of the record six ways and loads each image:
// a refusal must be by a name the runtime declares with nothing moved, a read
// must land every live value in its bound. It prints the failures and the
// cases.
const hostSweep = `local F = require("./table_fixed")
local T = require("./ProbeTable")
local function inBound(v: any): boolean
	if v.Small < 0 or v.Small > 1000 then return false end
	if v.MarksCount < 0 or v.MarksCount > 4 then return false end
	for i = 1, v.MarksCount do
		if v.Marks[i] < 0 or v.Marks[i] > 1000 then return false end
	end
	if v.NotePresent and (v.Note < 0 or v.Note > 1000) then return false end
	return true
end
local v = T.NewHost()
v.Small = 5
v.Marks = { 10, 20, 0, 0 }
v.MarksCount = 2
v.NotePresent = true
v.Note = 30
local clean = F.save(T.HostDescriptor, T.HostFixed, { v }) :: buffer
local n = buffer.len(clean)
local rec = n - (8 + T.HostFixed.recordBytes)
local bad, cases = 0, 0
for at = rec, n - 1 do
	for _, p in { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff } do
		local b = buffer.create(n)
		buffer.copy(b, 0, clean)
		buffer.writeu8(b, at, p)
		local rep: any = {}
		local got = F.load(T.HostDescriptor, T.HostFixed, b, rep)
		if got == nil then
			if not F.Names[rep.reason] or rep.outOfRange ~= 0 or rep.widened ~= 0 then bad += 1 end
		elseif rep.reason ~= nil or not inBound((got :: any)[1]) then
			bad += 1
		end
		cases += 1
	end
end
print(bad, cases)
`

// TestMooFixedEvolutionHostileInput is hostile-input (P3, P4): every byte of a
// record poisoned six ways (0x00, 0x01, 0x02, 0x7f, 0x80, 0xff) either refuses
// by a declared name with nothing moved or lands every live value in its
// bound; a bad hash on the LAST record of a batch refuses no_layout with
// nothing landed; and the wrong plan goes red — B's reader refuses A's file
// layout_unknown with A's hash, and A's file announced under B's hash is
// layout_malformed, never read through B's plan. The negative control drops
// the range clamp and requires the sweep to find out-of-bound values.
func TestMooFixedEvolutionHostileInput(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	u := unitFromSource(t, hostSchema)
	g := fixedUnit(t, u, nil)
	cases := fmt.Sprint(6 * evoEntry(t, u, "Host").Record)
	if got := g.run(t, "--!strict\n"+g.requireAll()+hostSweep, nil); got != "0\t"+cases {
		t.Errorf("P3: the sweep: %q, want 0 failures of %s", got, cases)
	}
	two := evoSave(t, g, "ProbeTable", "Host", "local v = T.NewHost()\nvalues = { v, v }\n")
	two[len(two)-int(evoEntry(t, u, "Host").Record)] ^= 1
	if got := evoLoad(t, g, "ProbeTable", "Host", two, -1).head; got != refusedTotal("no_layout", false, "-") {
		t.Errorf("P3: the last record's hash: %s", got)
	}
	gc := fixedUnit(t, u, nil)
	plantEach(t, gc, dropRangeChecks...)
	if got := gc.run(t, "--!strict\n"+gc.requireAll()+hostSweep, nil); strings.HasPrefix(got, "0\t") {
		t.Errorf("the negative control: the sweep without the range check finds nothing: %s", got)
	}

	ab := unitFromSource(t, "package probe\nfixed table A\n{\n    lead uint32 = 0\n    x int32 = 0\n    y int32 = 0\n    trail uint32 = 0\n}\nfixed table B\n{\n    p int8 = 0\n    q int8 = 0\n}\n")
	gab := fixedUnit(t, ab, nil)
	aFile := evoSave(t, gab, "ProbeTable", "A", "local v = T.NewA()\nv.Lead = 0xAABBCCDD\nv.X = 111\nv.Y = 222\nv.Trail = 0x11223344\nvalues = { v }\n")
	if got, want := evoLoad(t, gab, "ProbeTable", "B", aFile, -1).head, refusedTotal("layout_unknown", false, evoHash(evoEntry(t, ab, "A").Wire)); got != want {
		t.Errorf("P4: B's reader on A's file: %s, want %s", got, want)
	}
	asB := clone(aFile)
	binary.LittleEndian.PutUint64(asB[8:], evoEntry(t, ab, "B").Wire)
	if got, want := evoLoad(t, gab, "ProbeTable", "B", asB, -1).head, refusedTotal("layout_malformed", false, "-"); got != want {
		t.Errorf("P4: A's file announced as B: %s, want %s", got, want)
	}
	if got := evoLoad(t, gab, "ProbeTable", "A", aFile, -1); got.dump["r0.x"] != "111" || got.dump["r0.trail"] != "287454020" {
		t.Errorf("P4: the right plan: %v", got.dump)
	}
}

// cppRangeWidenProbe is the C++ reference's strict load of one range_widen
// file (the headers make tables-fixedform-corpus generates, whose static data
// carries each lineage entry's writer ranges): "<n> <v> <out_of_range>", n -1
// and v 0 when the read refused.
const cppRangeWidenProbe = `#include "VNEW_range_widenTable.h"
#include <cstdio>
#include <vector>
int main( int argc, char ** argv )
{
    std::FILE * f = std::fopen( argv[1], "rb" );
    std::vector<uint8_t> data( 4096 );
    const size_t n = std::fread( data.data(), 1, data.size(), f );
    std::fclose( f );
    vnew_range_widen::RangeWiden back;
    vnew_range_widen::TableReport r;
    std::vector<vnew_range_widen::TableFixedEntry> plan( 1024 );
    const int64_t got = vnew_range_widen::RangeWidenFixedLoad( &back, 1, data.data(), (int64_t) n, plan.data(), 1024, NULL, &r );
    std::printf( "%lld %d %d\n", (long long) got, got == 1 ? (int) back.v : 0, r.refuse_reason == vnew_range_widen::out_of_range ? 1 : 0 );
    return 0;
}
`

// TestMooFixedEvolutionWriterRange: the bounds pass holds the WRITER's range
// for the lineage entry being read (docs/FIXED-FORM-ALGORITHM.md §4.6), and it
// REFUSES. The old writer declared v in [0, 100], the new reader [0, 200]; the
// reference's old_range_widen.bin forged to 150 — inside the reader's range,
// outside the writer's, so damage — refuses the whole load out_of_range, as
// do -5 and 250; 0, 42 and the writer's own max 100 land. Each forged file is
// also loaded by the C++ reference's strict read, built here from its
// generated headers, and the two verdicts are the same. The negative control
// holds the reader's range alone and requires 150 to land.
func TestMooFixedEvolutionWriterRange(t *testing.T) {
	slowtest.Gate(t, "moo and g++ (they run the emitted fixed-form readers)")
	t.Parallel()
	corpus := fixedCorpus(t)
	base := corpusFile(t, corpus, "old_range_widen.bin")
	older, newer := readTestSchema(t, "VOLD_range_widen"), readTestSchema(t, "VNEW_range_widen")
	// the file's one record body is its last bytes, and v follows lead
	at := len(base) - int(ir.TableFixedTypeBytes(ir.TableFixedFormRoots(older)[0])) + 4
	forge := func(v int32) []byte {
		b := clone(base)
		binary.LittleEndian.PutUint32(b[at:], uint32(v))
		return b
	}
	headers := filepath.Join(root, "build", "tables-generated", "vnum")
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "probe.cpp"), []byte(cppRangeWidenProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	if out, err := exec.Command("g++", "-std=c++17", "-ffp-contract=off", "-I"+headers, "-I"+resolveSerializeInclude(dir), filepath.Join(dir, "probe.cpp"), "-o", bin).CombinedOutput(); err != nil {
		t.Fatalf("the C++ reference probe does not build: %v\n%s", err, out)
	}
	lineage := lineageOf(t, []*ir.Unit{older}, 0)
	g := fixedUnit(t, newer, lineage)
	for _, c := range []struct {
		forged int32
		v      string // "" refuses
	}{{150, ""}, {-5, ""}, {250, ""}, {101, ""}, {42, "42"}, {0, "0"}, {100, "100"}} {
		file := forge(c.forged)
		got := evoLoad(t, g, "VNEW_range_widenTable", "RangeWiden", file, -1)
		cpp := "1 " + c.v + " 0"
		if c.v == "" {
			if got.head != refusedField("out_of_range", false, "RangeWiden.v") {
				t.Errorf("v forged %d: %s, want the whole load refused out_of_range", c.forged, got.head)
			}
			cpp = "-1 0 1"
		} else if got.head != "ok 1 u0 k0 c0 w0 d0 -" || got.dump["r0.v"] != c.v {
			t.Errorf("v forged %d: %s v=%s, want v=%s", c.forged, got.head, got.dump["r0.v"], c.v)
		}
		p := filepath.Join(dir, "in.bin")
		if err := os.WriteFile(p, file, 0o644); err != nil {
			t.Fatal(err)
		}
		ref, err := exec.Command(bin, p).Output()
		if err != nil {
			t.Fatal(err)
		}
		if strings.TrimSpace(string(ref)) != cpp {
			t.Errorf("v forged %d: the C++ reference loads %q, moo's verdict is %s", c.forged, strings.TrimSpace(string(ref)), cpp)
		}
	}
	gc := fixedUnit(t, newer, lineage)
	plant(t, gc, "\tif n.wlo ~= nil and (", "\tif false and (")
	if got := evoLoad(t, gc, "VNEW_range_widenTable", "RangeWiden", forge(150), -1); got.dump["r0.v"] != "150" {
		t.Errorf("the negative control: the reader's range alone still refuses the writer's overrun: %s", got.head)
	}
}
