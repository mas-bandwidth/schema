package ctable

// TestFixedRoadmapCT06Bounds is the c leg's card schema-c-t06-bounds, the rows
// scalar-bounds, ordinals, normalization and array-bounds of docs/roadmap.sexp's
// node `fixed-tables`. One subtest per task id, each driving the GENERATED C
// reader (`<snake>_fixed_load`) over a record THIS TEST BUILDS with the same
// unit's `<snake>_fixed_save` and then forges — no corpus file and no sibling
// serialize runtime are needed, because the fixed form's Table header is
// self-contained for value-only units (docs/SPEC-TABLES.md §3.4).
//
// The laws are docs/FIXED-FORM-ALGORITHM.md §4.5 (the scatter: count, bool and
// present clamp/normalise) and §4.6 (the bounds pass: a ranged scalar's min and
// max, a union tag past the arm count, an enum ordinal past the top variant),
// and docs/SPEC-TABLES.md §3.4. Every counter is asserted EXACTLY and never
// `>= 1`, because the right number counted in the wrong place is the bug.
//
// A forged byte that lands normalised (bool/present) owes `clamped == 0`; a
// forged value that a bound catches (count, range, ordinal, tag) owes
// `clamped == 1` — the one number, on the one counter, once.

import (
	"bytes"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cRunSelfBuilt generates the READER's C (with the OLDER units' locked entries
// as lineage where given), writes a probe that builds its own one-record file
// through `setup` and `_fixed_save`, damages it through `forge`, loads it back
// and asserts through `body`, then compiles and runs the pair. `reader` and the
// `older` entries are the corpus base names of test/tables (e.g.
// "VNEW_array_bounded_grow"), read by this helper. The identity plan is what a
// single-generation probe exercises; handing `older` turns the same probe into
// the compiled-plan read, exactly as cRunVersionProbe does.
func cRunSelfBuilt(t *testing.T, reader string, older []string, retire int, setup, forge, body string) ([]byte, error) {
	t.Helper()
	u := cUnitOf(t, cReadSchema(t, reader))
	lineage := map[string][]FixedLineageEntry{}
	for i, base := range older {
		o := cUnitOf(t, cReadSchema(t, base))
		for _, st := range ir.TableFixedRoots(o) {
			e, ok := FixedLineageOf(o, st.Name)
			if !ok {
				t.Fatalf("no lineage entry for %s", st.Name)
			}
			e.Retired = i < retire
			if e.Retired {
				e.Reason = "retired by the test's lock"
			}
			lineage[st.Name] = append(lineage[st.Name], e)
		}
	}
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	root := cFixedRootName(t, u)
	snake := ir.RustSnake(root)
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

/* THE FORGE LOCATOR AND ITS TWO STORES. Record 0's body sits past the 16-byte
   header, the four-byte layout length, the layout bytes and the record's own
   eight-byte hash (docs/SPEC-TABLES.md §3.4). */
#define RECORD0() ( data + kTableFixedHeaderBytes + 4 + %s_fixed_layout_bytes + 8 )
#define FORGE_U8( OFF, V ) do { *( RECORD0() + (OFF) ) = (uint8_t)( V ); } while ( 0 )
#define FORGE_U32( OFF, V ) do { uint32_t v_ = (uint32_t)( V ); memcpy( RECORD0() + (OFF), &v_, 4 ); } while ( 0 )

int main( void )
{
    static uint8_t data[1 << 20];
    int64_t written;
    %s v;
    %s back[8];
    %s fresh;
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t n;
    int k;
    memset( &v, 0, sizeof( v ) );
    memset( &fresh, 0, sizeof( fresh ) );
    memset( back, 0, sizeof( back ) );
    %s( &fresh );
    for ( k = 0; k < 8; ++k ) { %s( &back[k] ); }
    /* THE RECORD THIS TEST BUILDS, one value, set by the subtest. */
%s
    written = %s( &v, 1, data, (int64_t) sizeof( data ) );
    if ( written < 0 ) { printf( "the self-built save failed\n" ); return 1; }
    /* THE FORGE, against the body the save just laid down. */
%s
    memset( &r, 0, sizeof( r ) );
    n = %s( back, 8, data, written, plan, 4096, NULL, &r );
    (void) fresh; (void) v;
    /* THE VERDICT, the subtest's own. */
%s
    return 0;
}
`, header, snake, root, root, root, snake+"_reset", snake+"_reset", setup, snake+"_fixed_save", forge, snake+"_fixed_load", body)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	args := append([]string{"-std=c99", "-O1", "-I" + dir}, gccFortifySilence(cc)...)
	args = append(args, filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe"), "-lm")
	build := exec.Command(cc, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

// cT06Header generates one schema's Table.h and returns its text, for the
// tasks whose clause is a sentence about the emitted C itself rather than a
// value a read lands.
func cT06Header(t *testing.T, base string) string {
	t.Helper()
	u := cUnitOf(t, cReadSchema(t, base))
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") && !strings.Contains(name, "/") {
			return string(data)
		}
	}
	t.Fatal("no Table.h was generated")
	return ""
}

func TestFixedRoadmapCT06Bounds(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/C7", cT06RangedScalarClamp},
		{"c/R30", cT06CompressedFloat},
		{"c/C9", cT06UnionTagPastArms},
		{"c/C10", cT06EnumOrdinalPastTop},
		{"c/R31", cT06FullWidthLanes},
		{"c/C12", cT06BoolByte},
		{"c/C13", cT06PresentByte},
		{"c/R17", cT06NormaliseCountsNothing},
		{"c/C1", cT06CountClampLow},
		{"c/C2", cT06CountClampHigh},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// c/C7 — "ranged scalar clamp". Algorithm §4.6: a RANGED SCALAR clamps to the
// min and max and COUNTS clamped, held by straight-line code after the plan
// run. Two forges over VNEW_range_widen's `v int32 | min = 0, max = 200`: past
// the max lands 200, below the min lands 0, each counted exactly once, and the
// brackets `lead`/`trail` never move.
func cT06RangedScalarClamp(t *testing.T) {
	setup := `
    v.lead = 0xAAAAAAAAu;
    v.v = 50;
    v.trail = 0xBBBBBBBBu;`
	check := func(label string, want int32) string {
		return fmt.Sprintf(`
    if ( n != 1 ) { printf( "C7: n=%%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].v != %d ) { printf( "C7: %s: %%d\n", (int) back[0].v ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "C7: a clamp moved a neighbour: lead=%%u trail=%%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( r.clamped != 1 ) { printf( "C7: clamped == %%d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "C7: a counter moved on a clean clamp: u=%%d km=%%d w=%%d d=%%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "C7: a clean read refused\n" ); return 1; }`, want, label)
	}
	forgeMax := `
    FORGE_U32( offsetof( RangeWiden, v ), 300 ); /* past max */`
	forgeMin := `
    FORGE_U32( offsetof( RangeWiden, v ), 0xFFFFFFFFu ); /* below min: -1 */`
	if out, err := cRunSelfBuilt(t, "VNEW_range_widen", nil, 0, setup, forgeMax, check("the forged 300 did not clamp to the max 200", 200)); err != nil {
		t.Fatalf("C7 past-max: %v\n%s", err, out)
	}
	if out, err := cRunSelfBuilt(t, "VNEW_range_widen", nil, 0, setup, forgeMin, check("the forged -1 did not clamp to the min 0", 0)); err != nil {
		t.Fatalf("C7 below-min: %v\n%s", err, out)
	}
}

// c/R30 — "compressed float rides as the float". Two clauses on the C leg. (1)
// the fixed-form write body stores the float32's bits with putf32 — never a
// quantized index, which is the variable form's encoding and nowhere in this
// unit's fixed form (SPEC-TABLES §3.4: "it rides as the float"). (2) min, max
// and resolution are DEFINITIONS, and the step lives in the digest under 'Q',
// not on the wire: the definitions digest carries one 'Q' for the compressed
// float's resolution (bill §13).
//
// The title's three widening clauses — "finer widens", "coarser refuses by
// name", "dropping the triple widens" — are the LOCK's monotone law
// (internal/lockfile/monotone.go), held by internal/lockfile/cfloatrange_test.go,
// a package this card's PATHS do not cover and this test does not run; and
// "fp-contract off on every leg" is a build discipline in make/c.mk, not a value
// a unit read lands. They are filed `unknown` on this card, not asserted here.
func cT06CompressedFloat(t *testing.T) {
	h := cT06Header(t, "VNEW_cfloat_range_widen")
	// THE FIXED FORM STORES THE FLOAT'S BITS, the variable form beside it
	// quantizes the same field into an index — one unit, two encodings, and the
	// fixed form's is the raw float (SPEC-TABLES §3.4: "it rides as the float").
	if !strings.Contains(h, "table_fixed_putf32( b + 4, value->aim )") {
		t.Fatal("R30: the fixed form's write body no longer stores aim's float32 bits with putf32")
	}
	if !strings.Contains(h, "table_message_quantize(value->aim") {
		t.Fatal("R30: the contrast is gone — the variable form no longer quantizes aim, so 'rides as the float' is untestable")
	}
	// min, max and resolution are DEFINITIONS: the step is in the digest under
	// 'Q', not on the wire (bill §13).
	u := cUnitOf(t, cReadSchema(t, "VNEW_cfloat_range_widen"))
	root := cFixedRoot(t, u)
	d := ir.TableFixedDefinitionsDigest(root)
	if n := bytes.Count(d, []byte{'Q'}); n != 1 {
		t.Fatalf("R30: one compressed float emits one 'Q' in the digest, got %d in %x", n, d)
	}
}

// c/C9 — "union tag past arms → None". Algorithm §4.6: a union tag past the
// arm count lands None and COUNTS clamped. VNEW_union_append's Pick has three
// arms; a tag forged to 4 lands PICK_TYPE_NONE, never an arm, and seq stands.
func cT06UnionTagPastArms(t *testing.T) {
	setup := `
    v.pick.type = 1; /* alpha */
    v.pick.as.alpha.m = 42;
    v.seq = 9;`
	forge := `
    FORGE_U8( offsetof( Lineage, pick ) + offsetof( Pick, type ), 4 ); /* past the three arms */`
	body := `
    if ( n != 1 ) { printf( "C9: n=%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].pick.type != PICK_TYPE_NONE )
        { printf( "C9: the forged tag 4 did not land None: %d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].seq != 9 ) { printf( "C9: the scalar after the union moved: %d\n", (int) back[0].seq ); return 1; }
    if ( r.clamped != 1 ) { printf( "C9: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "C9: a counter moved on a forged tag: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "C9: a forged tag is not a refusal\n" ); return 1; }`
	out, err := cRunSelfBuilt(t, "VNEW_union_append", nil, 0, setup, forge, body)
	if err != nil {
		t.Fatalf("C9: %v\n%s", err, out)
	}
}

// c/C10 — "enum ordinal past top → None". Algorithm §4.6: an enum ordinal past
// the enum's top value lands None and COUNTS clamped. VNEW_enum_append's Tier
// has four variants; an ordinal forged to 5 lands TIER_NONE, never Platinum,
// and seq stands.
func cT06EnumOrdinalPastTop(t *testing.T) {
	setup := `
    v.tier = 3; /* Gold */
    v.seq = 9;`
	forge := `
    FORGE_U8( offsetof( Lineage, tier ), 5 ); /* past the top ordinal 4 */`
	body := `
    if ( n != 1 ) { printf( "C10: n=%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].tier != TIER_NONE )
        { printf( "C10: the forged ordinal 5 did not land None: %d\n", (int) back[0].tier ); return 1; }
    if ( back[0].seq != 9 ) { printf( "C10: the scalar after the enum moved: %d\n", (int) back[0].seq ); return 1; }
    if ( r.clamped != 1 ) { printf( "C10: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "C10: a counter moved on a forged ordinal: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "C10: a forged ordinal is not a refusal\n" ); return 1; }`
	out, err := cRunSelfBuilt(t, "VNEW_enum_append", nil, 0, setup, forge, body)
	if err != nil {
		t.Fatalf("C10: %v\n%s", err, out)
	}
}

// c/R31 — "full-width lanes". The four strings below are the SHARED runtime
// template (tableFixedRuntime) that ctable.go splices into every generated
// Table.h, so they prove the RUNTIME carries the full-width `arg`/`arg2` lanes
// (uint64_t), the compare at the tag's own width through `argw`, and the 64-bit
// ordinal temporary (§5.8 rows 5 and 7). What they do NOT prove is the title's
// last clause — the remap table as long as the WRITER's variant count, refusing
// by name past the length word (§5.8 row 14) — because no unit in this tree
// declares a writer past 255 variants, so that clause is filed `unknown` on this
// card rather than asserted here.
func cT06FullWidthLanes(t *testing.T) {
	h := cT06Header(t, "VNEW_union_append")
	need := []string{
		"uint64_t arg;",     /* the guard ordinal lane is full width */
		"uint64_t arg2;",    /* the outer guard's lane too */
		"uint8_t argw;",     /* and the compare is at the tag's own width */
		"uint64_t raw = 0;", /* an ordinal decodes through a 64-bit temporary */
	}
	for _, s := range need {
		if !strings.Contains(h, s) {
			t.Fatalf("R31: the emitted plan no longer spells %q", s)
		}
	}
	if !strings.Contains(h, "raw <= (uint64_t) table[0]") {
		t.Fatal("R31: the remap table's length word is not the ordinal's bound")
	}
}

// c/C12 — "bool byte != 0". Algorithm §4.5: a bool lands as byte != 0,
// normalised to the language's own true; a forged 0x02 is not a bool holding 2.
func cT06BoolByte(t *testing.T) {
	setup := `
    v.flag = 0;
    v.link_present = 0;
    v.lead = 0xAAAAAAAAu;
    v.trail = 0xBBBBBBBBu;
    v.extra = 0xCCCCCCCCu;`
	forge := `
    FORGE_U8( offsetof( Hbb, flag ), 0x02 );`
	body := `
    if ( n != 1 ) { printf( "C12: n=%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].flag != 1 ) { printf( "C12: a forged 0x02 did not land the normalised true: %d\n", (int) back[0].flag ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "C12: the bool normalisation moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( r.clamped != 0 ) { printf( "C12: a bool normalises silently, clamped == %d\n", r.clamped ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "C12: a clean read refused\n" ); return 1; }`
	out, err := cRunSelfBuilt(t, "VNEW_hostile_bool", nil, 0, setup, forge, body)
	if err != nil {
		t.Fatalf("C12: %v\n%s", err, out)
	}
}

// c/C13 — "present flag byte != 0". Algorithm §4.5: a present flag lands as
// byte != 0, normalised to the language's own true; a forged 0x02 marks the
// optional present, never a byte holding 2.
func cT06PresentByte(t *testing.T) {
	setup := `
    v.link_present = 0;
    v.lead = 0xAAAAAAAAu;
    v.trail = 0xBBBBBBBBu;`
	forge := `
    FORGE_U8( 4, 0x02 ); /* the present byte rides at body offset 4, right after lead */`
	body := `
    if ( n != 1 ) { printf( "C13: n=%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].link_present != 1 )
        { printf( "C13: a forged 0x02 did not land the normalised present: %d\n", (int) back[0].link_present ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "C13: the present normalisation moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( r.clamped != 0 ) { printf( "C13: a present flag normalises silently, clamped == %d\n", r.clamped ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "C13: a clean read refused\n" ); return 1; }`
	out, err := cRunSelfBuilt(t, "VNEW_optional_add", nil, 0, setup, forge, body)
	if err != nil {
		t.Fatalf("C13: %v\n%s", err, out)
	}
}

// c/R17 — "a bool or present byte that is not 0/1 normalises and counts
// nothing". The two clauses of C12 and C13 together: 0x02 normalises to 1 AND
// no counter moves — the bool/present op is not a bound, so it owes clamped == 0
// and widened == 0 while the bool array's eight forged 0x02 bytes all land 1.
func cT06NormaliseCountsNothing(t *testing.T) {
	setup := `
    v.flag = 1;
    v.link_present = 1;
    v.lead = 0xAAAAAAAAu;
    v.trail = 0xBBBBBBBBu;
    v.extra = 0xCCCCCCCCu;`
	forge := `
    {
        int i;
        FORGE_U8( 0, 0x02 );         /* flag: body offset 0 */
        FORGE_U8( 1, 0x02 );         /* link_present: body offset 1 */
        for ( i = 0; i < 8; ++i ) { FORGE_U8( 10 + i, 0x02 ); } /* flags[i]: body offset 10+i */
    }`
	body := `
    if ( n != 1 ) { printf( "R17: n=%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].flag != 1 || back[0].link_present != 1 )
        { printf( "R17: a forged 0x02 did not normalise to 1: flag=%d present=%d\n", (int) back[0].flag, (int) back[0].link_present ); return 1; }
    {
        int i;
        for ( i = 0; i < 8; ++i ) if ( back[0].flags[i] != 1 ) { printf( "R17: flags[%d] did not normalise: %d\n", i, (int) back[0].flags[i] ); return 1; }
    }
    if ( r.clamped != 0 ) { printf( "R17: normalisation counts nothing, clamped == %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "R17: a counter moved on normalisation: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "R17: a clean read refused\n" ); return 1; }`
	out, err := cRunSelfBuilt(t, "VNEW_hostile_bool", nil, 0, setup, forge, body)
	if err != nil {
		t.Fatalf("R17: %v\n%s", err, out)
	}
}

// c/C1 — "count clamp v<0". Algorithm §4.5: a count below zero lands 0 and
// COUNTS clamped. VNEW_array_bounded_grow's count word forged to -1 lands 0.
func cT06CountClampLow(t *testing.T) {
	setup := `
    v.lead = 0xAAAAAAAAu;
    v.vals_count = 4;
    v.vals[0] = 1000; v.vals[1] = 1001; v.vals[2] = 1002; v.vals[3] = 1003;
    v.trail = 0xBBBBBBBBu;`
	forge := `
    FORGE_U32( 4, 0xFFFFFFFFu ); /* -1: the count word rides at body offset 4, right after lead */`
	body := `
    if ( n != 1 ) { printf( "C1: n=%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].vals_count != 0 ) { printf( "C1: the negative count did not clamp to 0: %d\n", back[0].vals_count ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "C1: a clamp moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( r.clamped != 1 ) { printf( "C1: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "C1: a counter moved on a clean clamp: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "C1: a clean read refused\n" ); return 1; }`
	out, err := cRunSelfBuilt(t, "VNEW_array_bounded_grow", nil, 0, setup, forge, body)
	if err != nil {
		t.Fatalf("C1: %v\n%s", err, out)
	}
}

// c/C2 — "count clamp v>Max". Algorithm §4.5: a count past the bound lands the
// bound and COUNTS clamped. VNEW_array_bounded_grow's count word forged to 9
// lands the identity plan's bound 8, never the forged 9.
func cT06CountClampHigh(t *testing.T) {
	setup := `
    v.lead = 0xAAAAAAAAu;
    v.vals_count = 4;
    v.vals[0] = 1000; v.vals[1] = 1001; v.vals[2] = 1002; v.vals[3] = 1003;
    v.trail = 0xBBBBBBBBu;`
	forge := `
    FORGE_U32( 4, 9 ); /* past the bound 8: the count word rides at body offset 4 */`
	body := `
    if ( n != 1 ) { printf( "C2: n=%lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].vals_count != 8 ) { printf( "C2: the forged 9 did not clamp to the bound 8: %d\n", back[0].vals_count ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "C2: a clamp moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( r.clamped != 1 ) { printf( "C2: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "C2: a counter moved on a clean clamp: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused || r.reason != 0 ) { printf( "C2: a clean read refused\n" ); return 1; }`
	out, err := cRunSelfBuilt(t, "VNEW_array_bounded_grow", nil, 0, setup, forge, body)
	if err != nil {
		t.Fatalf("C2: %v\n%s", err, out)
	}
}
