package pythontable

import (
	"encoding/binary"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/python/pyrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// R14 — A UNION TAG GROWN BETWEEN WRITER AND READER READS THE WRITER'S RAW
// TAG AT THE WRITER'S TAG WIDTH, and the 1-to-8 growth compiles WHOLE
// (docs/FIXED-FORM-ALGORITHM.md §5.2 EMIT kind 15, ruled 2026-11-25).
//
// The Python leg compiles every lineage plan at generate time, in
// compilePythonFixedPlan (fixedform.go), and the runtime only runs it
// (_schema/fixedform.py: _check over the writer's spec, then _apply and
// _guarded over the record). So R14 drives that generator path with the
// shared vector and runs the leg's real runtime on the plan it lays:
//
//	THEIRS (writer): [1,13,5,1] [7,15,5,1] [9,8,4,0] — a record of 5, one
//	union whose tag is 1 byte over one u32 arm.
//	MINE(tag) (reader): [1,13,tag+4,1] [7,15,tag+4,1] [9,8,4,0] — the union
//	tag at image 0 (tag bytes), the u32 arm at image `tag`.
//
// In this leg the union's None const is `value 0`: it LANDS my_tag bytes and
// reads NO record byte (its source reach is nothing). The writer's raw tag is
// read by the arm guards, [src, their_tag, ordinal, eq], and by _check over the
// writer's spec at the writer's `tag`, both at THE WRITER'S width. A read at
// the reader's width (my_tag) would take the arm's payload into the tag.

// r14Layout is one lock layout block: a u32LE count, then 17-byte entries.
func r14Layout(entries ...[4]uint64) []byte {
	out := binary.LittleEndian.AppendUint32(nil, uint32(len(entries)))
	for _, e := range entries {
		out = binary.LittleEndian.AppendUint64(out, e[0])
		out = append(out, byte(e[1]))
		out = binary.LittleEndian.AppendUint32(out, uint32(e[2]))
		out = binary.LittleEndian.AppendUint32(out, uint32(e[3]))
	}
	return out
}

func r14Theirs() []byte {
	return r14Layout([4]uint64{1, 13, 5, 1}, [4]uint64{7, 15, 5, 1}, [4]uint64{9, 8, 4, 0})
}

// r14Mine is MINE(tag) as the reader spec compilePythonFixedPlan takes: the
// shape fixedSemanticRoot lays for a table of one union of one u32 arm.
func r14Mine(tag int64) map[string]any {
	arm := map[string]any{"id": uint64(9), "kind": 8, "shape": "int", "size": int64(4), "name": "v", "signed": false, "lo": 0, "hi": int64(0xFFFFFFFF), "default": 0}
	union := map[string]any{"id": uint64(7), "kind": 15, "shape": "union", "size": tag + 4, "name": "u", "target": "U", "tag": tag, "children": []any{arm}}
	return map[string]any{"id": uint64(1), "kind": 13, "shape": "table", "name": "T", "target": "T", "size": tag + 4, "children": []any{union}}
}

// r14WriterCheck is the writer's spec the load checks THEIRS' records with
// (entry["check"] in fixed_load): the union's tag at the writer's 1 byte.
func r14WriterCheck() map[string]any {
	arm := map[string]any{"shape": "int", "size": int64(4), "signed": false, "lo": 0, "hi": int64(0xFFFFFFFF)}
	union := map[string]any{"shape": "union", "size": int64(5), "tag": int64(1), "children": []any{arm}}
	return map[string]any{"shape": "table", "size": int64(5), "children": []any{union}}
}

// r14Reach is the furthest record byte one compiled op reads: its source
// bytes (a const reads none) and every guard's [at, width].
func r14Reach(op map[string]any) int64 {
	reach := int64(0)
	switch op["op"] {
	case "copy", "widen", "widenf", "ordinal":
		reach = fixedInt(op, "src") + fixedInt(op, "size")
	case "count":
		reach = fixedInt(op, "src") + 4
	case "text":
		reach = fixedInt(op, "src") + 4 + fixedInt(op, "size")
	}
	for _, g := range op["guards"].([]any) {
		gv := g.([]any)
		reach = max(reach, gv[0].(int64)+gv[1].(int64))
	}
	return reach
}

func TestPythonFixedR14(t *testing.T) {
	t.Parallel()
	writer, why := parsePythonFixedLayout(r14Theirs())
	if why != "" || writer == nil || writer.Size != 5 {
		t.Fatalf("R14: THEIRS is a lawful layout of a 5-byte record: %q", why)
	}
	plans := map[int64][]any{}
	for _, tag := range []int64{8, 1, 2} {
		fills := []any{}
		plan, reason := compilePythonFixedPlan(writer, r14Mine(tag), false, &fills)
		if reason != "" || len(plan) == 0 {
			t.Fatalf("R14: THEIRS vs MINE(%d) compiles whole, got refusal %q", tag, reason)
		}
		if len(fills) != 0 {
			t.Fatalf("R14: MINE(%d) names every writer field, no fills: %v", tag, fills)
		}
		for _, v := range plan {
			if r := r14Reach(v.(map[string]any)); r > writer.Size {
				t.Fatalf("R14: MINE(%d) op %v reaches %d past the writer's record of %d", tag, v, r, writer.Size)
			}
		}
		plans[tag] = plan
	}

	// THE CELL: the 1-to-8 growth. Three ops: the None const, the arm's tag
	// const and the u32's copy past the WRITER's 1-byte tag.
	plan := plans[8]
	if len(plan) != 3 {
		t.Fatalf("R14: the grown tag compiles to three ops, got %v", plan)
	}
	none := plan[0].(map[string]any)
	if none["op"] != "const" || fixedInt(none, "value") != 0 || len(none["guards"].([]any)) != 0 {
		t.Fatalf("R14: the first op is the unguarded None const, got %v", none)
	}
	if fixedInt(none, "src") != 0 || fixedInt(none, "dst") != 0 || fixedInt(none, "size") != 8 {
		t.Fatalf("R14: the None const at src 0 lands MY 8-byte tag at image 0, got %v", none)
	}
	arm := plan[1].(map[string]any)
	copyOp := plan[2].(map[string]any)
	want := []any{int64(0), int64(1), int64(1), "eq"}
	for _, op := range []map[string]any{arm, copyOp} {
		gs := op["guards"].([]any)
		if len(gs) != 1 || fixedLiteralOf(gs[0]) != fixedLiteralOf(want) {
			t.Fatalf("R14: the writer's tag is read at its offset 0 at THE WRITER'S width 1, never 8: %v", op)
		}
	}
	if arm["op"] != "const" || fixedInt(arm, "value") != 1 || fixedInt(arm, "size") != 8 {
		t.Fatalf("R14: arm 1 lands my 8-byte tag 1, got %v", arm)
	}
	if copyOp["op"] != "copy" || fixedInt(copyOp, "src") != 1 || fixedInt(copyOp, "dst") != 8 || fixedInt(copyOp, "size") != 4 {
		t.Fatalf("R14: the u32 is a copy of 4 bytes from src 1 (past the writer's 1-byte tag) to image 8, got %v", copyOp)
	}

	// THE RUN, on the leg's real runtime, in the order fixed_load runs it,
	// over an image POISONED with 0xFF so a const landing fewer than my
	// tag's bytes shows (a None arm leaves the u32 untouched, 0xFFFFFFFF):
	// _check over the writer's spec, the unguarded ops, then the guarded.
	u := loadUnit(t, "package rfourteen\nunion U\n{\n    v uint32\n}\nfixed table T { u U }\n")
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	literal := map[string]any{}
	for tag, p := range plans {
		literal[string(rune('0'+tag))] = p
	}
	probe := pyrun.ImportPrelude + `from rfourteen import Refused
from rfourteen._schema import fixedform as ff
PLANS = ` + fixedLiteralOf(literal) + `
CHECK = ` + fixedLiteralOf(r14WriterCheck()) + `
def run(tag, record):
    body = memoryview(bytes(record))
    ff._check(CHECK, body, 0)
    image = bytearray(b"\xff" * (tag + 4))
    plan = PLANS[str(tag)]
    for op in [p for p in plan if not p["guards"]] + [p for p in plan if p["guards"]]:
        if not op["guards"] or ff._guarded(op, body):
            ff._apply(op, body, image)
    return int.from_bytes(image[:tag], "little"), int.from_bytes(image[tag:], "little")
for tag in (8, 1, 2):
    got = run(tag, [1, 0x44, 0x33, 0x22, 0x11])
    if got != (1, 0x11223344):
        raise AssertionError("R14_arm tag=%d got %r" % (tag, got))
    got = run(tag, [0, 0, 0, 0, 0])
    if got != (0, 0xFFFFFFFF):
        raise AssertionError("R14_none tag=%d got %r" % (tag, got))
    try:
        run(tag, [2, 0, 0, 0, 0])
    except Refused as e:
        if e.reason != "out_of_range":
            raise AssertionError("R14_forged tag=%d refused %s" % (tag, e.reason))
    else:
        raise AssertionError("R14_forged tag=%d read" % tag)
print("R14 ok")
`
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, p.path(t), dir, "main.py", map[string][]byte{"main.py": []byte(probe)})
		if err != nil || strings.TrimSpace(out) != "R14 ok" {
			t.Fatalf("R14: the plan run on the %s pin: %v\n%s\n%s", p.name, err, out, stderr)
		}
	}
}

// TestPythonFixedR14Lineage is R14's lineage lane: a lock entry whose union
// tag is 1 byte, generated into a build whose tag grew to 8, lays a READY plan
// (no refusal), and fixed_load reads a writer file through it: arm 1 lands an
// 8-byte tag of exactly 1 and the u32 whole, tag 0 is None, and a forged tag
// 2 refuses out_of_range. An 8-byte tag needs a union past 2^32 arms, so the
// reader's union is widened in the IR by hand (Max), the one forged fact.
func TestPythonFixedR14Lineage(t *testing.T) {
	t.Parallel()
	const src = "package rfourteen\nunion U\n{\n    v uint32\n}\nfixed table T { u U }\n"
	old := loadUnit(t, src)
	newer := loadUnit(t, src)
	un, _ := newer.Tables["T"].Fields[0].Type.Ref.(*ir.Union)
	if un == nil || ir.TableFixedUnionTagBytes(un) != 1 {
		t.Fatal("R14: the writer's union tag is 1 byte")
	}
	un.Max, un.StorageBits = 1<<32, 64
	if ir.TableFixedUnionTagBytes(un) != 8 || ir.TableFixedTypeBytes(newer.Tables["T"]) != 12 {
		t.Fatalf("R14: the reader's tag grew to 8 (record %d)", ir.TableFixedTypeBytes(newer.Tables["T"]))
	}
	st := old.Tables["T"]
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	entry := FixedLineageEntry{Wire: ir.TableFixedLayoutHash(layout, st), Layout: layout, Digest: ir.TableFixedDefinitionsDigest(st), Record: 8 + ir.TableFixedTypeBytes(st), Writer: st}
	if entry.Record != 13 {
		t.Fatalf("R14: the writer's record is 8 + 5, got %d", entry.Record)
	}
	files, err := GenerateLineage(newer, map[string][]FixedLineageEntry{"T": {entry}})
	if err != nil {
		t.Fatalf("R14: the grown tag generates: %v", err)
	}
	file := func(body ...byte) []byte {
		out := []byte{3, 0, 0, 0, 0, 0, 0, 0}
		out = binary.LittleEndian.AppendUint64(out, entry.Wire)
		out = binary.LittleEndian.AppendUint32(out, uint32(len(layout)))
		out = append(out, layout...)
		out = binary.LittleEndian.AppendUint64(out, entry.Wire)
		return append(out, body...)
	}
	dir := t.TempDir()
	writeTree(t, dir, files)
	probe := pyrun.ImportPrelude + `from rfourteen import T, Refused
def load(name):
    with open(name, "rb") as f:
        return T.fixed_load(f.read())
rows = load("arm")
if len(rows) != 1 or rows[0].u.type != 1 or rows[0].u.v != 0x11223344:
    raise AssertionError("R14_lineage_arm %r" % ((rows[0].u.type, rows[0].u.v),))
image = T.fixed_save(rows)
if image[-12:-4] != (1).to_bytes(8, "little") or image[-4:] != (0x11223344).to_bytes(4, "little"):
    raise AssertionError("R14_lineage_image %s" % image[-12:].hex())
rows = load("none")
if len(rows) != 1 or rows[0].u.type != 0:
    raise AssertionError("R14_lineage_none")
try:
    load("forged")
except Refused as e:
    if e.reason != "out_of_range":
        raise AssertionError("R14_lineage_forged " + e.reason)
else:
    raise AssertionError("R14_lineage_forged read")
print("R14 lineage ok")
`
	inputs := map[string][]byte{
		"main.py": []byte(probe),
		"arm":     file(1, 0x44, 0x33, 0x22, 0x11),
		"none":    file(0, 0, 0, 0, 0),
		"forged":  file(2, 0, 0, 0, 0),
	}
	for _, p := range []struct {
		name string
		path func(testing.TB) string
	}{{"current", pyrun.Interpreter}, {"floor", pyrun.Floor}} {
		out, stderr, err := pyrun.Try(t, p.path(t), dir, "main.py", inputs)
		if err != nil || strings.TrimSpace(out) != "R14 lineage ok" {
			t.Fatalf("R14: the lineage lane on the %s pin: %v\n%s\n%s", p.name, err, out, stderr)
		}
	}
}
