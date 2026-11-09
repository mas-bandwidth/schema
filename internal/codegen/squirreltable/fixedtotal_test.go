package squirreltable_test

// TOTAL OVER THE BATCH on the Squirrel leg (docs/FIXED-FORM-ALGORITHM.md §5.3,
// "TOTAL OVER THE BATCH"; tla/FixedBatch.tla): the emitted <Root>FixedLoad
// checks every record before the first one lands, so a refusal on the LAST
// record of a batch leaves every row of the caller's storage exactly as the
// caller poisoned it — out[0..k-1] as untouched as out[k]. One clean
// three-record file is written by the emitted FixedSave; each case forges ONE
// condition into the last record only. The negative control plants the
// interleaved loop back (decode record k straight into out[k]) and requires
// the rows to read touched.

import (
	"encoding/binary"
	"fmt"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirrel/sqrun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/squirreltable"
)

// The body, in declared order at declared storage widths (§3.4):
//
//	n      int32       0..4    ranged [0, 100]
//	on     bool        4
//	opt    ?int32      5 present byte, 6..10 payload
//	f      float32     10..14
//	shade  Shade       14      ordinal, 1-based, two variants
//	s      string(4)   15..19 length, 19..23 bytes
//	m      [..3]int32  23..27 count, 27..39 elements
const sqTotalSchema = "package tot\nenum Shade { Dark, Light }\nfixed table TR\n{\n" +
	"    n     int32 = 5 | min = 0, max = %d\n" +
	"    on    bool\n" +
	"    opt   ?int32\n" +
	"    f     float32 = 1.5\n" +
	"    shade Shade = Dark\n" +
	"    s     string(4) = \"x\"\n" +
	"    m     [..3]int32\n" +
	"}\n"

// sqTotalFill writes three records the clean file holds: every field away
// from its default, so a landed record reads differently from the poison.
const sqTotalFill = `for (local k = 0; k < 3; k++) {
  local r = T.TR(); r.N = 10 + k; r.On = true; r.Opt = 7 + k; r.OptPresent = true;
  r.F = 2.5; r.Shade = "Light"; r.S = "ab"; r.M = [1, 2 + k];
  values.append(r);
}
`

// sqTotalProbe poisons three caller rows, dumps them, loads inputs["file"],
// and prints "ok <n>" with the landed rows, or "refused <reason> w<widened>
// untouched|touched" from a fresh dump of every row.
func sqTotalProbe(g *sqEvoGen) string {
	return sqEvoDumper(g) + `local rows = [];
for (local k = 0; k < 3; k++) {
  local r = T.TR(); r.N = 77; r.On = false; r.Opt = 55; r.OptPresent = true;
  r.F = 9.25; r.Shade = "Dark"; r.S = "zz"; r.M = [9, 9, 9];
  rows.append(r);
}
for (local k = 0; k < 3; k++) dump("r" + k, rows[k]);
local before = lines;
lines = [];
local got = T.TRFixedLoad(inputs["file"], rows, null);
for (local k = 0; k < 3; k++) dump("r" + k, rows[k]);
if (got < 0) {
  local same = before.len() == lines.len();
  for (local i = 0; same && i < lines.len(); i++) if (before[i] != lines[i]) same = false;
  print("refused " + T.FixedReason + " w" + T.FixedWidened + (same ? " untouched" : " touched") + "\n");
  return;
}
print("ok " + got + "\n");
foreach (l in lines) print(l + "\n");
`
}

type sqTotalCase struct {
	name   string
	reason string
	forge  func(b []byte, rec int) // rec: the offset of the last record's body
}

func sqTotalCases() []sqTotalCase {
	u32 := func(b []byte, at int, v uint32) { binary.LittleEndian.PutUint32(b[at:], v) }
	return []sqTotalCase{
		{"the record hash", "no_layout", func(b []byte, rec int) { b[rec-8] ^= 1 }},
		{"a ranged int past the reader's max", "out_of_range", func(b []byte, rec int) { u32(b, rec+0, 101) }},
		{"a bool byte of 2", "wire_damaged", func(b []byte, rec int) { b[rec+4] = 2 }},
		{"a present byte of 2", "wire_damaged", func(b []byte, rec int) { b[rec+5] = 2 }},
		{"an infinite float", "non_finite_float", func(b []byte, rec int) { u32(b, rec+10, 0x7F800000) }},
		{"a forged enum ordinal", "out_of_range", func(b []byte, rec int) { b[rec+14] = 3 }},
		{"a text length past its bound", "length_over_bound", func(b []byte, rec int) { u32(b, rec+15, 5) }},
		{"ill-formed text", "text_ill_formed", func(b []byte, rec int) { b[rec+19], b[rec+20] = 0xC0, 0xAF }},
		{"a live count past its bound", "count_over_bound", func(b []byte, rec int) { u32(b, rec+23, 4) }},
	}
}

// lastBody is the offset of the last record's body in a clean file of three.
func sqTotalLastBody(t *testing.T, clean []byte) int {
	t.Helper()
	L := int(binary.LittleEndian.Uint32(clean[16:20]))
	rb := (len(clean) - 20 - L) / 3
	if rb != 8+39 || 20+L+3*rb != len(clean) {
		t.Fatalf("the clean file is not three records of 47 bytes: len %d, layout %d", len(clean), L)
	}
	return 20 + L + 2*rb + 8
}

func TestSquirrelFixedLoadIsTotalOverTheBatch(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	u := fixedSynthetic(t, fmt.Sprintf(sqTotalSchema, 100))
	g := sqEvoGenerate(t, u, nil)
	clean := g.save(t, "TR", sqTotalFill)
	rec := sqTotalLastBody(t, clean)

	// the clean batch lands every record
	out := g.run(t, sqTotalProbe(g), map[string][]byte{"file": clean})
	if !strings.HasPrefix(out, "ok 3\n") || !strings.Contains(out, "r2.N=12") || !strings.Contains(out, "r0.S=\"ab\"") {
		t.Fatalf("the clean batch did not land: %s", out)
	}

	for _, c := range sqTotalCases() {
		file := sqEvoClone(clean)
		c.forge(file, rec)
		got := g.run(t, sqTotalProbe(g), map[string][]byte{"file": file})
		if want := "refused " + c.reason + " w0 untouched"; got != want {
			t.Errorf("%s on the last record: %q, want %q", c.name, got, want)
		}
	}

	// THE WRITER'S RANGE (§4.6) through a compiled plan: an older writer that
	// bounded n to [0, 50]; the last record holds 60, inside this reader's
	// range and outside the writer's.
	older := fixedSynthetic(t, fmt.Sprintf(sqTotalSchema, 50))
	eo := sqEvoEntry(t, older, "TR")
	gw := sqEvoGenerate(t, u, map[string][]squirreltable.FixedLineageEntry{"TR": {eo}})
	oldClean := sqEvoGenerate(t, older, nil).save(t, "TR", sqTotalFill)
	if out := gw.run(t, sqTotalProbe(gw), map[string][]byte{"file": oldClean}); !strings.HasPrefix(out, "ok 3\n") {
		t.Fatalf("the older writer's clean batch did not land: %s", out)
	}
	ranged := sqEvoClone(oldClean)
	binary.LittleEndian.PutUint32(ranged[sqTotalLastBody(t, oldClean):], 60)
	if got, want := gw.run(t, sqTotalProbe(gw), map[string][]byte{"file": ranged}), "refused out_of_range w0 untouched"; got != want {
		t.Errorf("a value past the writer's range on the last record: %q, want %q", got, want)
	}
}

// TestSquirrelFixedLoadIsTotalNegativeControl plants the interleaved loop
// back: each record decodes straight into the caller's out[k], so a refusal
// on the last record leaves the first two landed. Every case must read
// touched.
func TestSquirrelFixedLoadIsTotalNegativeControl(t *testing.T) {
	sqrun.Require(t)
	t.Parallel()
	u := fixedSynthetic(t, fmt.Sprintf(sqTotalSchema, 100))
	g := sqEvoGenerate(t, u, nil)
	clean := g.save(t, "TR", sqTotalFill)
	rec := sqTotalLastBody(t, clean)
	g.plant(t, "    local v = _M.TR();\n", "    local v = out[k];\n")
	for _, c := range sqTotalCases() {
		file := sqEvoClone(clean)
		c.forge(file, rec)
		got := g.run(t, sqTotalProbe(g), map[string][]byte{"file": file})
		if !strings.HasSuffix(got, " touched") {
			t.Errorf("the negative control: %s on the last record still reads %q under the interleaved loop", c.name, got)
		}
	}
}
