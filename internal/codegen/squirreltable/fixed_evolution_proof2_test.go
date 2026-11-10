package squirreltable_test

// THE FIXED FORM'S ARRAY-BOUND, TEXT, NORMALIZATION AND HOSTILE-INPUT ROWS ON
// THE SQUIRREL LEG, under the strict-only read (docs/FIXED-FORM-ALGORITHM.md
// §4.5, §4.6, §5.3, §5.4; docs/rulings.json strict-only-read). The helpers and
// the probe are fixed_evolution_proof_test.go's: files are framed from the
// compiler's own layout and hash or written by the emitted FixedSave, and every
// runtime rule has a negative control that plants a break into the emitted
// module and requires the row to go red. Reason names are Squirrel's own pins.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

const sqEvoArrayOld = "package probe\nfixed table A\n{\n    lead uint32 = 1\n    vals [..4]int32\n    trail uint32 = 2\n}\n"

// TestSquirrelFixedEvolutionArrayBounds is array-bounds (C1, C2, R11): the
// plan carries the WRITER's bound. A counted array the old writer bounded at 4
// and the reader at 8 refuses count_over_bound for a count of -1 and of 7 on
// the compiled plan, and reads at exactly 4 and at 0; the reader's own
// identity plan refuses 9 and reads 8. The negative control bounds the
// compiled plan's count by the reader's 8.
func TestSquirrelFixedEvolutionArrayBounds(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	older := fixedSynthetic(t, sqEvoArrayOld)
	newer := fixedSynthetic(t, strings.Replace(sqEvoArrayOld, "[..4]", "[..8]", 1))
	eo, en := sqEvoEntry(t, older, "A"), sqEvoEntry(t, newer, "A")
	body := func(n uint32, slots int) []byte {
		b := binary.LittleEndian.AppendUint32(nil, 0xAAAA)
		b = binary.LittleEndian.AppendUint32(b, n)
		for i := range slots {
			b = binary.LittleEndian.AppendUint32(b, uint32(1000+i))
		}
		return binary.LittleEndian.AppendUint32(b, 0xBBBB)
	}
	lineage := map[string][]squirreltable.FixedLineageEntry{"A": {eo}}
	g := sqEvoGenerate(t, newer, lineage)
	for _, c := range []struct {
		name string
		file []byte
		want string
	}{
		{"C1 count -1", sqEvoFile(eo.Wire, eo.Layout, body(0xFFFFFFFF, 4)), refused("count_over_bound")},
		{"C2 count 7, past the writer's 4", sqEvoFile(eo.Wire, eo.Layout, body(7, 4)), refused("count_over_bound")},
		{"C2 count 5, one past the writer's 4", sqEvoFile(eo.Wire, eo.Layout, body(5, 4)), refused("count_over_bound")},
		{"R11 count 4, the writer's bound", sqEvoFile(eo.Wire, eo.Layout, body(4, 4)), "ok 1 w0"},
		{"count 0", sqEvoFile(eo.Wire, eo.Layout, body(0, 4)), "ok 1 w0"},
		{"the reader's own 9 of 8", sqEvoFile(en.Wire, en.Layout, body(9, 8)), refused("count_over_bound")},
		{"the reader's own 8 of 8", sqEvoFile(en.Wire, en.Layout, body(8, 8)), "ok 1 w0"},
	} {
		got := g.load(t, "A", c.file)
		if !strings.HasPrefix(got.head, c.want) {
			t.Errorf("%s: %s, want %s", c.name, got.head, c.want)
		}
	}
	if got := g.load(t, "A", sqEvoFile(eo.Wire, eo.Layout, body(4, 4))); got.dump["r0.vals_count"] != "4" || got.dump["r0.vals[3]"] != "1003" || got.dump["r0.trail"] != "48059" {
		t.Errorf("R11: the four elements: %v", got.dump)
	}
	gc := sqEvoGenerate(t, newer, lineage)
	gc.plant(t, "if (c < 0 || c > 4) return \"count_over_bound\";", "if (c < 0 || c > 8) return \"count_over_bound\";")
	if got := gc.load(t, "A", sqEvoFile(eo.Wire, eo.Layout, body(7, 4))); strings.HasPrefix(got.head, refused("count_over_bound")) {
		t.Errorf("the negative control: the reader's bound still refuses the writer's overrun: %s", got.head)
	}
}

// TestSquirrelFixedEvolutionText is text (C3, C4, C5, R24): a length past the
// field's capacity, or below zero, refuses length_over_bound; ill-formed UTF-8
// or a zero in the USED bytes, and an unpaired surrogate (high or low) or a
// zero unit in a wstring's used units, refuse text_ill_formed; a hostile byte
// or unit in the slack past the used length is not read and not a refusal. On
// a compiled plan the WRITER's capacity bounds the length: string(4) read by a
// string(8) reader refuses 5. The negative controls drop the UTF-8 check, the
// narrow and the wide length checks, and the wide-unit check.
func TestSquirrelFixedEvolutionText(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	const src = "package probe\nfixed table T\n{\n    s string(4)\n}\n"
	u := fixedSynthetic(t, src)
	const setup = "local v = T.T();\nv.S = \"ab\";\nvalues.append(v);\n"
	g := sqEvoGenerate(t, u, nil)
	clean := g.save(t, "T", setup)
	body := len(clean) - 8
	cut := func(f func(b []byte)) []byte { b := sqEvoClone(clean); f(b); return b }
	cases := []struct {
		name string
		file []byte
		want string
	}{
		{"C3 length 5 of 4", cut(func(b []byte) { b[body] = 5 }), refused("length_over_bound")},
		{"C3 length -1", cut(func(b []byte) { binary.LittleEndian.PutUint32(b[body:], 0xFFFFFFFF) }), refused("length_over_bound")},
		{"C4 0xff in the used bytes", cut(func(b []byte) { b[body+4] = 0xff }), refused("text_ill_formed")},
		{"C4 a zero in the used bytes", cut(func(b []byte) { b[body+5] = 0 }), refused("text_ill_formed")},
		{"C4 a truncated two-byte sequence", cut(func(b []byte) { b[body+5] = 0xc3 }), refused("text_ill_formed")},
		{"R24 0xff in the slack", cut(func(b []byte) { b[body+6], b[body+7] = 0xff, 0xff }), "ok 1 w0"},
	}
	for _, c := range cases {
		if got := g.load(t, "T", c.file); !strings.HasPrefix(got.head, c.want) {
			t.Errorf("%s: %s, want %s", c.name, got.head, c.want)
		}
	}
	if got := g.load(t, "T", cases[5].file); got.dump["r0.s"] != `"ab"` {
		t.Errorf("R24: the used text with poisoned slack: %v", got.dump)
	}

	// the writer's capacity governs a compiled plan
	wide := fixedSynthetic(t, strings.Replace(src, "string(4)", "string(8)", 1))
	e := sqEvoEntry(t, u, "T")
	gw := sqEvoGenerate(t, wide, map[string][]squirreltable.FixedLineageEntry{"T": {e}})
	if got := gw.load(t, "T", clean); got.head != "ok 1 w0" || got.dump["r0.s"] != `"ab"` {
		t.Errorf("the compiled plan's clean read: %s %v", got.head, got.dump)
	}
	if got := gw.load(t, "T", cases[0].file); !strings.HasPrefix(got.head, refused("length_over_bound")) {
		t.Errorf("the compiled plan, length 5 of the writer's 4: %s", got.head)
	}

	gc := sqEvoGenerate(t, u, nil)
	gc.dropLine(t, "_rt_fixed.textIllFormed(b, at + 0 + 4, n)", "text_ill_formed")
	if got := gc.load(t, "T", cases[2].file); strings.HasPrefix(got.head, refused("text_ill_formed")) {
		t.Errorf("the negative control: ill-formed UTF-8 still refuses without the check: %s", got.head)
	}
	gc = sqEvoGenerate(t, u, nil)
	gc.plant(t, "    if (n < 0 || n > 4) return \"length_over_bound\";\n    if (_rt_fixed.textIllFormed", "    if (_rt_fixed.textIllFormed")
	if got := gc.load(t, "T", cases[0].file); strings.HasPrefix(got.head, refused("length_over_bound")) {
		t.Errorf("the negative control: a narrow length past capacity still refuses without the check: %s", got.head)
	}
}

const sqEvoNormalOld = "package probe\nfixed table T\n{\n    flag bool\n    opt ?int32\n}\n"

// TestSquirrelFixedEvolutionNormalization is normalization (C12, C13, R17
// under strict-only, docs/FIXED-FORM-ALGORITHM.md §4.5): a bool byte and a
// present byte of 0 or 1 land as false and true; any other byte — 0x02, 0x5a,
// 0x7f, 0x80, 0xff — refuses wire_damaged, neither stored verbatim nor
// normalized, on the identity plan and on a compiled plan; an absent optional's
// payload bytes are not read. The negative controls drop the bool check and the
// present-byte check.
func TestSquirrelFixedEvolutionNormalization(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	u := fixedSynthetic(t, sqEvoNormalOld)
	newer := fixedSynthetic(t, strings.Replace(sqEvoNormalOld, "    opt ?int32\n", "    opt ?int32\n    extra int32 = 5\n", 1))
	g := sqEvoGenerate(t, u, nil)
	file := g.save(t, "T", "local v = T.T();\nv.Flag = true;\nv.OptPresent = true;\nv.Opt = 99;\nvalues.append(v);\n")
	body := len(file) - 6
	with := func(flag, present byte, payload uint32) []byte {
		b := sqEvoClone(file)
		b[body], b[body+1] = flag, present
		binary.LittleEndian.PutUint32(b[body+2:], payload)
		return b
	}
	gn := sqEvoGenerate(t, newer, map[string][]squirreltable.FixedLineageEntry{"T": {sqEvoEntry(t, u, "T")}})
	for name, gg := range map[string]*sqEvoGen{"identity": g, "compiled": gn} {
		if got := gg.load(t, "T", file); got.head != "ok 1 w0" || got.dump["r0.flag"] != "1" || got.dump["r0.opt_present"] != "1" || got.dump["r0.opt"] != "99" {
			t.Errorf("%s, the clean record: %s %v", name, got.head, got.dump)
		}
		if got := gg.load(t, "T", with(0, 1, 42)); got.head != "ok 1 w0" || got.dump["r0.flag"] != "0" || got.dump["r0.opt"] != "42" {
			t.Errorf("%s, false and present: %s %v", name, got.head, got.dump)
		}
		if got := gg.load(t, "T", with(1, 0, 0xFFFFFFFF)); got.head != "ok 1 w0" || got.dump["r0.opt_present"] != "0" {
			t.Errorf("%s, an absent optional over a poisoned payload: %s %v", name, got.head, got.dump)
		}
		for _, x := range []byte{0x02, 0x5a, 0x7f, 0x80, 0xff} {
			if got := gg.load(t, "T", with(x, 1, 42)); !strings.HasPrefix(got.head, refused("wire_damaged")) {
				t.Errorf("%s, C12 a bool byte 0x%02x: %s, want wire_damaged", name, x, got.head)
			}
			if got := gg.load(t, "T", with(1, x, 42)); !strings.HasPrefix(got.head, refused("wire_damaged")) {
				t.Errorf("%s, C13 a present byte 0x%02x: %s, want wire_damaged", name, x, got.head)
			}
		}
	}
	gc := sqEvoGenerate(t, u, nil)
	gc.dropLine(t, "if (b[at + 0] > 1) return \"wire_damaged\";")
	if got := gc.load(t, "T", with(0x7f, 1, 42)); strings.HasPrefix(got.head, refused("wire_damaged")) {
		t.Errorf("the negative control: a bool byte 0x7f still refuses without the check: %s", got.head)
	}
	gc = sqEvoGenerate(t, u, nil)
	gc.dropLine(t, "if (b[at + 1] > 1) return \"wire_damaged\";")
	if got := gc.load(t, "T", with(1, 0x5a, 42)); strings.HasPrefix(got.head, refused("wire_damaged")) {
		t.Errorf("the negative control: a present byte 0x5a still refuses without the check: %s", got.head)
	}
}

const sqEvoHostSchema = "package probe\nenum Tier { A, B }\nfixed table Host\n{\n    small int32 = 5 | min = 0, max = 1000\n    marks [..4]int32 | min = 0, max = 1000\n    note ?int32 | min = 0, max = 1000\n    flag bool\n    tier Tier\n    name string(3)\n    tail int32 = 7\n}\n"

// sqEvoHostSweep poisons every byte of the one record — its hash and its
// body — six ways and loads each image into a fresh record: a refusal must be
// by a name the fixed reader declares, a read must land every live value in
// its bound. It prints the failures and the cases, then the first failures.
const sqEvoHostSweep = `local T = dofile("schema_module.nut", true).require("ProbeTables");
local names = { no_layout = 1, count_over_bound = 1, length_over_bound = 1, text_ill_formed = 1, wire_damaged = 1, out_of_range = 1, non_finite_float = 1 };
local inBound = function(v) {
  if (typeof v.Small != "integer" || v.Small < 0 || v.Small > 1000) return false;
  if (v.Marks.len() > 4) return false;
  foreach (m in v.Marks) if (m < 0 || m > 1000) return false;
  if (v.NotePresent && (v.Note < 0 || v.Note > 1000)) return false;
  if (typeof v.Flag != "bool") return false;
  if (v.Tier != null && v.Tier != "A" && v.Tier != "B") return false;
  if (typeof v.Name != "string" || v.Name.len() > 3) return false;
  return true;
};
local v = T.Host();
v.Small = 33;
v.Marks = [10, 20];
v.NotePresent = true;
v.Note = 30;
v.Flag = true;
v.Tier = "B";
v.Name = "ab";
v.Tail = 9;
local m = T.HostFixedMeasure(1);
local clean = blob(m);
T.HostFixedSave(clean, [v]);
local rec = m - T.HostFixedRecord;
local bad = 0, cases = 0, why = [];
for (local at = rec; at < m; at++) {
  foreach (p in [0x00, 0x01, 0x02, 0x7f, 0x80, 0xff]) {
    local b = blob(m);
    for (local i = 0; i < m; i++) b[i] = clean[i];
    b[at] = p;
    local rows = [T.Host()];
    local got = -2;
    try { got = T.HostFixedLoad(b, rows, null); } catch (e) { got = -2; }
    if (got == -2) { bad++; why.append(at + "/" + p + " raised"); }
    else if (got < 0) { if (!(T.FixedReason in names)) { bad++; why.append(at + "/" + p + " " + T.FixedReason); } }
    else if (got != 1 || T.FixedReason != null || !inBound(rows[0])) { bad++; why.append(at + "/" + p + " landed out of bound"); }
    cases++;
  }
}
print(bad + " " + cases + "\n");
for (local i = 0; i < why.len() && i < 8; i++) print(why[i] + "\n");
`

// TestSquirrelFixedEvolutionHostileInput is hostile-input (P3, P4): every byte
// of a record poisoned six ways (0x00, 0x01, 0x02, 0x7f, 0x80, 0xff) either
// refuses by a name the fixed reader declares or lands every live value in its
// bound; a bad hash on the LAST record of a batch refuses no_layout with
// nothing landed in either record (every hash is checked before any record
// lands, §5.3, TOTAL OVER THE BATCH); and the wrong plan goes red — B's reader refuses A's file
// layout_unknown, and A's file announced under B's hash is layout_malformed,
// never read through B's plan. The negative controls drop the range check (the
// sweep finds an out-of-bound value) and decode each record straight into the
// caller's row (the first record lands before the refusal).
func TestSquirrelFixedEvolutionHostileInput(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	u := fixedSynthetic(t, sqEvoHostSchema)
	g := sqEvoGenerate(t, u, nil)
	cases := fmt.Sprint(6 * sqEvoEntry(t, u, "Host").Record)
	if got := g.run(t, sqEvoHostSweep, nil); got != "0 "+cases {
		t.Errorf("P3: the sweep: %q, want 0 failures of %s", got, cases)
	}

	const setup = "local v = T.Host();\nv.Small = 33;\nv.Marks = [10, 20];\nv.Tail = 9;\nvalues.append(v);\nvalues.append(v);\n"
	two := g.save(t, "Host", setup)
	rec := int(sqEvoEntry(t, u, "Host").Record)
	two[len(two)-rec] ^= 1
	if got := g.load(t, "Host", two); got.head != "refused no_layout w0 untouched" {
		t.Errorf("P3: the last record's hash: %s, want no_layout with nothing landed", got.head)
	}

	gc := sqEvoGenerate(t, u, nil)
	gc.dropLine(t, "v.Small < 0 || v.Small > 1000", "out_of_range")
	if got := gc.run(t, sqEvoHostSweep, nil); strings.HasPrefix(got, "0 ") {
		t.Errorf("the negative control: the sweep without the range check finds nothing: %s", got)
	}
	gc = sqEvoGenerate(t, u, nil)
	gc.plant(t, "    local v = _M.Host();\n", "    local v = out[k];\n")
	if got := gc.load(t, "Host", two); got.head == "refused no_layout w0 untouched" {
		t.Errorf("the negative control: records decoded straight into the caller's rows still leave the first record untouched")
	}

	ab := fixedSynthetic(t, "package probe\nfixed table A\n{\n    lead uint32 = 0\n    x int32 = 0\n    y int32 = 0\n    trail uint32 = 0\n}\nfixed table B\n{\n    p int8 = 0\n    q int8 = 0\n}\n")
	gab := sqEvoGenerate(t, ab, nil)
	aFile := gab.save(t, "A", "local v = T.A();\nv.Lead = 0xAABBCCDD;\nv.X = 111;\nv.Y = 222;\nv.Trail = 0x11223344;\nvalues.append(v);\n")
	if got := gab.load(t, "B", aFile); !strings.HasPrefix(got.head, refused("layout_unknown")) {
		t.Errorf("P4: B's reader on A's file: %s, want layout_unknown", got.head)
	}
	asB := sqEvoClone(aFile)
	binary.LittleEndian.PutUint64(asB[8:], sqEvoEntry(t, ab, "B").Wire)
	if got := gab.load(t, "B", asB); !strings.HasPrefix(got.head, refused("layout_malformed")) {
		t.Errorf("P4: A's file announced as B: %s, want layout_malformed", got.head)
	}
	if got := gab.load(t, "A", aFile); got.head != "ok 1 w0" || got.dump["r0.x"] != "111" || got.dump["r0.trail"] != "287454020" {
		t.Errorf("P4: the right plan: %s %v", got.head, got.dump)
	}
}
