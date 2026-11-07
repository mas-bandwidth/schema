package mootable_test

// THE FIXED FORM'S REMAINING VERSIONING AND HOSTILE ROWS ON THE MOO LEG
// (docs/FIXED-FORM-VERSIONING-TESTS.md, docs/FIXED-FORM-ALGORITHM.md §4.5,
// §5.4, §5.8): the rows the Go leg holds that fixedform_test.go does not —
// the lineage merge, the compile census over many records, the forged
// ordinal on both plans, the compressed float's reader-side bounds pass, and
// hostile bool and present bytes. The bytes are the C++ reference's corpus
// (SCHEMA_REQUIRE_CORPUS=1 makes its absence a failure) or a moo-saved record
// forged in place. Every generated unit passes moo-analyze with no output.

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// analyzeSilent holds a generated fixed unit to moo-analyze: no output at all.
func analyzeSilent(t *testing.T, g *generatedUnit) {
	t.Helper()
	if out := g.analyze(t); out != "" {
		t.Fatalf("moo-analyze is not silent:\n%s", out)
	}
}

// TestMooFixedUnitsAnalyzeSilent: every unit the fixed form reads — the nine
// corpus schemas and every versioning pair, each with its lineage — passes
// moo-analyze with no output, the table runtime included.
func TestMooFixedUnitsAnalyzeSilent(t *testing.T) {
	slowtest.Gate(t, "moo-analyze (it loads the generated fixed-form modules)")
	t.Parallel()
	for _, v := range fixedOracleVectors {
		t.Run(v.file, func(t *testing.T) {
			t.Parallel()
			analyzeSilent(t, fixedUnit(t, unitFromPath(t, v.schema), nil))
		})
	}
	for _, r := range mooVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			older := readTestSchema(t, "VOLD_"+r.row)
			newer := readTestSchema(t, "VNEW_"+r.row)
			analyzeSilent(t, fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0)))
		})
	}
}

// TestMooFixedVersioningLineageMerge: two branches append different fields;
// after the merge BOTH pre-merge files read on the merged build, the lineage
// being the base and the two branches (the name-subset rule).
func TestMooFixedVersioningLineageMerge(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	base := readTestSchema(t, "VOLD_lineage_merge")
	a, b := readTestSchema(t, "VBRA_lineage_merge"), readTestSchema(t, "VBRB_lineage_merge")
	merged := readTestSchema(t, "VNEW_lineage_merge")
	g := fixedUnit(t, merged, lineageOf(t, []*ir.Unit{base, a, b}, 0))
	analyzeSilent(t, g)
	rootName := rootOf(t, base)
	for _, file := range []string{"a_lineage_merge.bin", "b_lineage_merge.bin"} {
		got := runFixedLoad(t, g, "VNEW_lineage_mergeTable", rootName, corpusFile(t, corpus, file))
		ml := manifest[file]
		if !strings.HasPrefix(got.head, fmt.Sprintf("ok %d u0 k0 c0 w0", ml.records)) {
			t.Errorf("%s: both pre-merge files read on the merged build: %s", file, got.head)
		}
		if n := compareManifest(t, file, ml, got.dump); n == 0 {
			t.Errorf("%s: no manifest value compared", file)
		}
	}
	// the control: without the branches in the lineage the branch files refuse
	bare := fixedUnit(t, merged, lineageOf(t, []*ir.Unit{base}, 0))
	if got := runFixedLoad(t, bare, "VNEW_lineage_mergeTable", rootName, corpusFile(t, corpus, "a_lineage_merge.bin")).head; !strings.HasPrefix(got, "refused layout_unknown") {
		t.Errorf("the control, branch a absent from the lineage: %s", got)
	}
}

// TestMooFixedVersioningUnknownCensusMany (§5.4): the compile census lands
// once per endpoint and never per record: three records, unknown exactly 1.
func TestMooFixedVersioningUnknownCensusMany(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_unknown_census"), readTestSchema(t, "VNEW_unknown_census")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	got := runFixedLoad(t, g, "VNEW_unknown_censusTable", rootOf(t, older), corpusFile(t, corpus, "many_unknown_census.bin"))
	if !strings.HasPrefix(got.head, "ok 3 u1 k0 c0 w0") {
		t.Fatalf("three records, one endpoint: %s", got.head)
	}
	for i, want := range [3][4]int{{10, 11, 12, 13}, {20, 21, 22, 23}, {30, 31, 32, 33}} {
		if got.dump[fmt.Sprintf("r%d.lead", i)] != "1" || got.dump[fmt.Sprintf("r%d.trail", i)] != "2" {
			t.Errorf("record %d: the bracketing fields moved: %v", i, got.dump)
		}
		for k, w := range want {
			if v := got.dump[fmt.Sprintf("r%d.items[%d].a", i, k)]; v != fmt.Sprint(w) {
				t.Errorf("record %d items[%d].a = %s, want %d", i, k, v, w)
			}
		}
	}
}

// TestMooFixedForgedOrdinalBothPlans (§5.8 row 12): the same forged ordinal
// read by the identity plan and a compiled one lands the zero ordinal and
// counts clamped exactly once.
func TestMooFixedForgedOrdinalBothPlans(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_enum_append"), readTestSchema(t, "VNEW_enum_append")
	data := corpusFile(t, corpus, "old_enum_append.bin")
	needle := []byte{0x03, 0x09, 0x00, 0x00, 0x00}
	if !bytes.HasSuffix(data, needle) || bytes.Count(data, needle) != 1 {
		t.Fatalf("the record body (tier=Gold, seq=9) is not the tail of the file: %x", data[len(data)-5:])
	}
	forged := append([]byte(nil), data...)
	forged[len(forged)-5] = 4 // past the writer's three variants
	for name, g := range map[string]*generatedUnit{
		"compiled": fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0)),
		"identity": fixedUnit(t, older, nil),
	} {
		mod := "VNEW_enum_appendTable"
		if name == "identity" {
			mod = "VOLD_enum_appendTable"
		}
		got := runFixedLoad(t, g, mod, "Lineage", forged)
		if !strings.HasPrefix(got.head, "ok 1 u0 k0 c1 w0") || got.dump["r0.tier"] != "0" || got.dump["r0.seq"] != "9" {
			t.Errorf("%s plan: %s %v", name, got.head, got.dump)
		}
		if clean := runFixedLoad(t, g, mod, "Lineage", data); !strings.HasPrefix(clean.head, "ok 1 u0 k0 c0 w0") {
			t.Errorf("%s plan, the control, the unforged file: %s", name, clean.head)
		}
	}
}

// TestMooFixedVersioningCfloatRangeWidenBounds: the bounds pass is the
// READER's. The hostile file (aim forged to 1.5, past the old writer's max
// and inside the new reader's) lands whole with clamped 0; the past file
// (aim 5.0) clamps to the reader's own max 2.0, counted exactly once.
func TestMooFixedVersioningCfloatRangeWidenBounds(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	older, newer := readTestSchema(t, "VOLD_cfloat_range_widen"), readTestSchema(t, "VNEW_cfloat_range_widen")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	for _, c := range []struct{ file, aim, head string }{
		{"hostile_cfloat_range_widen.bin", "0x3FC00000", "ok 1 u0 k0 c0 w0"},
		{"past_cfloat_range_widen.bin", "0x40000000", "ok 1 u0 k0 c1 w0"},
	} {
		got := runFixedLoad(t, g, "VNEW_cfloat_range_widenTable", "CfloatRangeWiden", corpusFile(t, corpus, c.file))
		if got.head != c.head || got.dump["r0.aim"] != c.aim || got.dump["r0.lead"] != "1" || got.dump["r0.trail"] != "2" {
			t.Errorf("%s: %s %v, want %s aim %s", c.file, got.head, got.dump, c.head, c.aim)
		}
	}
	// the hostile file's forged value is the manifest's own
	if want := manifest["hostile_cfloat_range_widen.bin"]; want.records != 1 {
		t.Errorf("the corpus manifest does not carry the hostile file: %+v", want)
	}
}

// hostileBoolFile is a record moo saved for the older Hbb with its bool and
// present bytes forged: flag 2, link's present byte 3, flags 5 0 2 0 ... The
// body at the end of the file is flag@0, present@1, link.n@2, lead@6,
// flags@10..17, trail@18 (22 bytes after the 8-byte record hash).
func hostileBoolFile(t *testing.T, older *ir.Unit) []byte {
	t.Helper()
	g := fixedUnit(t, older, nil)
	main := "--!strict\nlocal F = require(\"./table_fixed\")\nlocal T = require(\"./VOLD_hostile_boolTable\")\n" + `local v = T.NewHbb()
v.Lead = 0xAAAAAAAA
v.Trail = 0xBBBBBBBB
local out = F.save(T.HbbDescriptor, T.HbbFixed, { v }) :: buffer
local parts = table.create(buffer.len(out))
for i = 0, buffer.len(out) - 1 do
	parts[i + 1] = string.format("%02x", buffer.readu8(out, i))
end
print(table.concat(parts))
`
	b, err := hexDecode(g.run(t, main, nil))
	if err != nil {
		t.Fatal(err)
	}
	body := b[len(b)-22:]
	if body[6] != 0xAA || body[18] != 0xBB {
		t.Fatalf("the body offsets moved: %x", body)
	}
	body[0], body[1] = 2, 3
	body[10], body[11], body[12], body[13] = 5, 0, 2, 0xFF
	return b
}

// TestMooFixedHostileBoolBytes (§4.5): a bool or present byte that is not 0 or
// 1 normalises to a true, silently — no counter moves — and the neighbours
// stand, on the identity plan and on a compiled one.
func TestMooFixedHostileBoolBytes(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	older, newer := readTestSchema(t, "VOLD_hostile_bool"), readTestSchema(t, "VNEW_hostile_bool")
	file := hostileBoolFile(t, older)
	check := func(name string, g *generatedUnit, mod string) {
		got := runFixedLoad(t, g, mod, "Hbb", file)
		if !strings.HasPrefix(got.head, "ok 1 u0 k0 c0 w0") {
			t.Errorf("%s: a hostile bool byte is not an event: %s", name, got.head)
			return
		}
		for k, want := range map[string]string{
			"r0.flag": "1", "r0.link_present": "1", "r0.lead": "2863311530", "r0.trail": "3149642683",
			"r0.flags[0]": "1", "r0.flags[1]": "0", "r0.flags[2]": "1", "r0.flags[3]": "1", "r0.flags[4]": "0",
		} {
			if got.dump[k] != want {
				t.Errorf("%s: %s landed %q, want %s", name, k, got.dump[k], want)
			}
		}
	}
	check("identity", fixedUnit(t, older, nil), "VOLD_hostile_boolTable")
	check("compiled", fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0)), "VNEW_hostile_boolTable")
}

// TestMooFixedHostileBoolNegativeControl breaks the bool read to `== 1` and
// requires the hostile file's flag to land false: the nonzero test is what
// the gate holds.
func TestMooFixedHostileBoolNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	older := readTestSchema(t, "VOLD_hostile_bool")
	file := hostileBoolFile(t, older)
	g := fixedUnit(t, older, nil)
	const anchor = "\t\treturn buffer.readu8(rec, off) ~= 0\n"
	src := string(g.files["table_fixed.moo"])
	if strings.Count(src, anchor) != 1 {
		t.Fatal("the bool-read anchor is not in table_fixed.moo exactly once")
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_fixed.moo"), []byte(strings.Replace(src, anchor, "\t\treturn buffer.readu8(rec, off) == 1\n", 1)), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFixedLoad(t, g, "VOLD_hostile_boolTable", "Hbb", file); got.dump["r0.flag"] != "0" {
		t.Fatalf("the broken bool read still lands the hostile byte true: %v", got.dump)
	}
}
