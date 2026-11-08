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

// TestMooFixedVersioningUnknownCensusMany (§5.4, §5.9 #30): there is no
// compile census to land. VNEW_unknown_census against VOLD_unknown_census is a
// DELIBERATELY UNLAWFUL lineage entry (Item.drop removed), so the newer reader
// lays no plan for it and its three-record file refuses layout_malformed,
// nothing landed; the older reader, whose own entry it is, reads the same
// three records whole.
func TestMooFixedVersioningUnknownCensusMany(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	older, newer := readTestSchema(t, "VOLD_unknown_census"), readTestSchema(t, "VNEW_unknown_census")
	file := corpusFile(t, corpus, "many_unknown_census.bin")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	if got := runFixedLoad(t, g, "VNEW_unknown_censusTable", rootOf(t, older), file); got.head != "refused layout_malformed nil false -" {
		t.Fatalf("an unlawful entry over three records: %s", got.head)
	}
	got := runFixedLoad(t, fixedUnit(t, older, nil), "VOLD_unknown_censusTable", rootOf(t, older), file)
	if !strings.HasPrefix(got.head, "ok 3 u0 k0 c0 w0") {
		t.Fatalf("the control, the writer's own reader: %s", got.head)
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

// TestMooFixedForgedOrdinalBothPlans (§4.6, §5.8 row 12; fix 14): the same
// forged ordinal read by the identity plan and a compiled one REFUSES THE
// WHOLE LOAD out_of_range on both, nothing landed; the unforged file lands.
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
		if got := runFixedLoad(t, g, mod, "Lineage", forged); got.head != "refused out_of_range Lineage.tier false -" {
			t.Errorf("%s plan: %s %v", name, got.head, got.dump)
		}
		if clean := runFixedLoad(t, g, mod, "Lineage", data); !strings.HasPrefix(clean.head, "ok 1 u0 k0 c0 w0") || clean.dump["r0.seq"] != "9" {
			t.Errorf("%s plan, the control, the unforged file: %s", name, clean.head)
		}
	}
}

// TestMooFixedVersioningCfloatRangeWidenBounds: a compressed float's range is
// the READER's on this form (the plan carries no writer range for a float,
// FIXED-FORM-ALGORITHM.md §5.8 row 4), and the bounds pass refuses, never
// clamps. The hostile file (aim forged to 1.5, past the old writer's max and
// inside the new reader's) lands whole; the past file (aim 5.0, outside the
// reader's range too) refuses the whole load out_of_range.
func TestMooFixedVersioningCfloatRangeWidenBounds(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	corpus := fixedCorpus(t)
	manifest := readFixedManifest(t, corpus)
	older, newer := readTestSchema(t, "VOLD_cfloat_range_widen"), readTestSchema(t, "VNEW_cfloat_range_widen")
	g := fixedUnit(t, newer, lineageOf(t, []*ir.Unit{older}, 0))
	got := runFixedLoad(t, g, "VNEW_cfloat_range_widenTable", "CfloatRangeWiden", corpusFile(t, corpus, "hostile_cfloat_range_widen.bin"))
	if got.head != "ok 1 u0 k0 c0 w0" || got.dump["r0.aim"] != "0x3FC00000" || got.dump["r0.lead"] != "1" || got.dump["r0.trail"] != "2" {
		t.Errorf("hostile_cfloat_range_widen.bin: %s %v, want ok aim 0x3FC00000", got.head, got.dump)
	}
	if got := runFixedLoad(t, g, "VNEW_cfloat_range_widenTable", "CfloatRangeWiden", corpusFile(t, corpus, "past_cfloat_range_widen.bin")); got.head != "refused out_of_range CfloatRangeWiden.aim false -" {
		t.Errorf("past_cfloat_range_widen.bin: %s, want the whole load refused out_of_range", got.head)
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

// TestMooFixedHostileBoolBytes (§4.5, fix 2): a bool or present byte that is
// not 0 or 1 REFUSES THE WHOLE LOAD wire_damaged, nothing landed and nothing
// counted, on the identity plan and on a compiled one; the same record with
// those bytes 0 and 1 lands.
func TestMooFixedHostileBoolBytes(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	older, newer := readTestSchema(t, "VOLD_hostile_bool"), readTestSchema(t, "VNEW_hostile_bool")
	file := hostileBoolFile(t, older)
	clean := cleanBoolFile(file)
	check := func(name string, g *generatedUnit, mod string) {
		if got := runFixedLoad(t, g, mod, "Hbb", file); got.head != "refused wire_damaged Hbb.flag true -" {
			t.Errorf("%s: a hostile bool byte: %s", name, got.head)
		}
		got := runFixedLoad(t, g, mod, "Hbb", clean)
		if !strings.HasPrefix(got.head, "ok 1 u0 k0 c0 w0") {
			t.Errorf("%s: the control, bytes of 0 and 1: %s", name, got.head)
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

// cleanBoolFile is hostileBoolFile with every forged byte put back to 1
// (flag, link's present byte, flags[0], flags[2], flags[3]).
func cleanBoolFile(file []byte) []byte {
	b := append([]byte(nil), file...)
	body := b[len(b)-22:]
	body[0], body[1] = 1, 1
	body[10], body[12], body[13] = 1, 1, 1
	return b
}

// TestMooFixedHostileBoolNegativeControl drops the bool and present checks
// and requires the hostile file to land: the 0-or-1 test is what the gate
// holds.
func TestMooFixedHostileBoolNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the emitted fixed-form runtime)")
	t.Parallel()
	older := readTestSchema(t, "VOLD_hostile_bool")
	file := hostileBoolFile(t, older)
	g := fixedUnit(t, older, nil)
	src := string(g.files["table_fixed.moo"])
	for _, anchor := range []string{
		"\t\tif b > 1 then\n\t\t\trefuse(\"wire_damaged\", true, fieldName(named))\n",
		"\t\t\t\tif b > 1 then\n\t\t\t\t\trefuse(\"wire_damaged\", true, fieldName(f))\n",
	} {
		if strings.Count(src, anchor) != 1 {
			t.Fatalf("the anchor %q is not in table_fixed.moo exactly once", anchor)
		}
		src = strings.Replace(src, anchor, strings.Replace(anchor, "if b > 1 then", "if false then", 1), 1)
	}
	if err := os.WriteFile(filepath.Join(g.dir, "table_fixed.moo"), []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := runFixedLoad(t, g, "VOLD_hostile_boolTable", "Hbb", file); !strings.HasPrefix(got.head, "ok 1 ") {
		t.Fatalf("without the 0-or-1 checks the hostile file still refuses: %s", got.head)
	}
}
