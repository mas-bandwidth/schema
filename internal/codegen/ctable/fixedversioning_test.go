package ctable

// THE VERSIONING FIXTURES ON THE C LEG (docs/FIXED-FORM-VERSIONING-TESTS.md,
// the procedures in docs/FIXED-FORM-ALGORITHM.md §5). Every row of that page is
// one definition change and owes TWO read columns:
//
//	NEW-READS-OLD    the widened reader reads the older writer's file: every
//	                 old value lands exactly, the reader's tail is its declared
//	                 default, and the counters are §5.4's — for an APPEND, all
//	                 of them zero, because an append the reader knows is not an
//	                 event.
//	OLD-REFUSES-NEW  the older reader given the widened writer's file refuses
//	                 `layout_newer` BEFORE any record, reporting THE FILE'S
//	                 HASH and nothing else, with no counter moved and
//	                 `malformed` clear (§5.3's joint answer).
//
// The bytes are the C++ reference's: `make tables-fixedform-corpus` writes
// `build/fixedform-corpus/old_<row>.bin` and `new_<row>.bin`. The two schemas
// of a row are `test/tables/VOLD_<row>.schema` and `VNEW_<row>.schema`; they
// carry ONE table name in two packages, because the two layouts are ONE
// LINEAGE.
//
// EACH SIDE IS ITS OWN TRANSLATION UNIT, which is the one place this leg's
// harness cannot copy the Go leg's. C has no namespace, so two generations of
// one table name have no spelling inside one binary (SPEC §6.1, and the
// FX1/FX2 precedent in make/c.mk). So a row is TWO PROBE BINARIES and not one:
// NEW-READS-OLD compiles the NEW generation alone and reads `old_<row>.bin`,
// OLD-REFUSES-NEW compiles the OLD generation alone and reads `new_<row>.bin`.
// The probe units are GENERATED, one per row per column, into the test's own
// temp dir — nothing is checked in that a schema edit could leave stale.
//
// THE LINEAGE IS DATA THE BUILD HANDS THE BACKEND (§5.2, COMPILE(lock, T)):
// the test plays the lock, handing the newer unit the older unit's locked entry
// — the wire hash, the layout bytes verbatim and the record size. A reader
// NEVER parses the layout a file carries; it matches the header's hash against
// the lineage and compares the bytes it already holds.

import (
	"encoding/binary"
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// ---- the rows ---------------------------------------------------------------

type cVersionRow struct {
	row string
	// sameHash is a row whose edit moves no layout byte and no digest byte
	// (`rename_without_was`, `field_deprecate`, `field_undeprecate`): the two
	// hashes are equal, both sides take the identity plan and the file READS in
	// BOTH directions — which is the test that the hash, and only the hash, is
	// the version (§5.7).
	sameHash bool
	// widens is a row that grows a WIDTH, so `widened` moves at least once;
	// every other row is an APPEND and owes all six counters at zero (§5.9 #10).
	widens bool
	// check is C asserting the landed values, over `back`.
	check string
	// poison writes 0x5A over every byte of `back` AFTER the reset and BEFORE
	// the load (§5.7, and the C++ reference's own harness does it). A row that
	// asserts an ELEMENT DEFAULT cannot prove the prefill WRITES unless the
	// destination held something else first: against zeroed storage "the slot
	// reads 7" and "the slot was never touched" are the same sentence when the
	// default is 0, and for a nonzero default they are the same sentence the
	// day the prefill's ranges are wrong and the zeros are the memset's. 0x5A
	// is neither a default nor a written value, so every byte the read owes is
	// a byte that must CHANGE.
	poison bool
}

var cVersionRows = []cVersionRow{
	{row: "array_bounded_grow"},
	{row: "array_elem_widen", widens: true},
	{row: "array_fixed_grow", poison: true, check: `
    /* THE GROWN SLOTS ARE THE ELEMENT'S DECLARED DEFAULTS, not zeros and not the
       0x5A the storage held a line ago: [4]Vec -> [8]Vec, so slots 4..7 are
       §3.4's PREFILL and the law reads x = 7, y = 9 (the row's own comment). */
    {
        int k;
        for ( k = 4; k < 8; ++k )
        {
            if ( back[0].vals[k].x != 7 || back[0].vals[k].y != 9 )
                { printf( "the grown slot %d is not the ELEMENT'S defaults: x=%d y=%d\n", k, back[0].vals[k].x, back[0].vals[k].y ); return 1; }
        }
        /* and the slots THE WRITER carried were landed by the plan, over the poison */
        for ( k = 0; k < 4; ++k )
        {
            if ( back[0].vals[k].x == 0x5A5A5A5A || back[0].vals[k].y == 0x5A5A5A5A )
                { printf( "the old writer's slot %d kept the poison: the plan landed nothing there\n", k ); return 1; }
        }
    }
    if ( back[0].lead == 0x5A5A5A5Au || back[0].trail == 0x5A5A5A5Au )
        { printf( "a bracketing field kept the poison: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }`},
	{row: "bits_grow", widens: true},
	{row: "bytes_grow"},
	{row: "constant_grow"},
	{row: "enum_append"},
	{row: "enum_width", widens: true},
	{row: "field_append", check: `
    if ( back[0].x != 11 || back[0].y != 22 || back[0].z != 33 )
        { printf( "the old writer's values did not land: %d %d %d\n", back[0].x, back[0].y, back[0].z ); return 1; }
    if ( back[0].w != 77 )
        { printf( "the appended field is not its declared default: %d\n", back[0].w ); return 1; }`},
	{row: "field_deprecate", sameHash: true},
	{row: "field_undeprecate", sameHash: true},
	{row: "fixed_I_grow", widens: true},
	{row: "flags_append"},
	{row: "float_widen", widens: true},
	{row: "int_widen", widens: true, check: `
    if ( n != 3 ) { printf( "the int_widen file carries three records, not %lld\n", (long long) n ); return 1; }
    {
        const int32_t want[3] = { -1, -32768, 32767 };
        int k;
        for ( k = 0; k < 3; ++k )
        {
            if ( (int32_t) back[k].v != want[k] ) { printf( "record %d widened wrong\n", k ); return 1; }
            if ( back[k].lead != 0xAAAAAAAAu || back[k].trail != 0xBBBBBBBBu ) { printf( "record %d moved a neighbour\n", k ); return 1; }
        }
    }`},
	{row: "keyed_array_enum_append", poison: true, check: `
    /* ONE VARIANT APPENDED TO THE KEY, so the array gains its last slot and that
       slot is §3.4's PREFILL: Qty.n = 7, over 0x5A and not over zeros. */
    if ( back[0].slots[TIER_MAX - 1].n != 7 )
        { printf( "the appended key's slot is not Qty's declared default: %d\n", back[0].slots[TIER_MAX - 1].n ); return 1; }
    {
        int k;
        for ( k = 0; k < TIER_MAX - 1; ++k )
        {
            if ( back[0].slots[k].n == 0x5A5A5A5A )
                { printf( "the old writer's slot %d kept the poison: the plan landed nothing there\n", k ); return 1; }
        }
    }
    if ( back[0].seq == 0x5A5A5A5A ) { printf( "seq kept the poison\n" ); return 1; }`},
	{row: "nested_append"},
	{row: "optional_add"},
	{row: "range_widen"},
	{row: "rename_without_was", sameHash: true},
	{row: "string_grow"},
	{row: "uint_widen", widens: true},
	{row: "union_append"},
	{row: "union_arm_payload_widen"},
	{row: "wstring_grow"},
}

// ---- NEW-READS-OLD ----------------------------------------------------------

func TestFixedVersioningNewReadsOld(t *testing.T) {
	corpus := cFixedCorpus(t)
	for _, r := range cVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			newer := cReadSchema(t, "VNEW_"+r.row)
			older := cReadSchema(t, "VOLD_"+r.row)
			counters := `if ( r.widened != 0 ) { printf( "widened %d, and an append is not an event\n", r.widened ); return 1; }`
			if r.widens {
				counters = `if ( r.widened == 0 ) { printf( "a widening row moved no widened counter\n" ); return 1; }`
			}
			body := fmt.Sprintf(`
    if ( n < 1 ) { printf( "the newer reader refused the older writer's file: n=%%lld reason=%%d\n", (long long) n, r.reason ); return 1; }
    if ( r.reason != 0 || r.malformed || r.refused ) { printf( "a clean NEW-READS-OLD is not a refusal: reason=%%d malformed=%%d\n", r.reason, r.malformed ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "counters moved on a clean backward read: u=%%d km=%%d c=%%d d=%%d\n", r.unknown, r.kind_mismatch, r.clamped, r.duplicate ); return 1; }
    %s
    %s
`, counters, r.check)
			poison := ""
			if r.poison {
				// §5.7'S 0x5A, and it goes in AFTER the reset: the harness
				// resets `back` so a refusal's "wrote not one byte" check has
				// something to compare against, and this row's claim is the
				// opposite one — that the prefill WRITES — so the poison has to
				// survive to the load.
				poison = `    memset( back, 0x5A, sizeof( back ) ); /* §5.7: every byte the read owes must CHANGE */`
			}
			out, err := cRunVersionProbe(t, newer, []string{older}, 0,
				filepath.Join(corpus, "old_"+r.row+".bin"), body, poison)
			if err != nil {
				t.Fatalf("NEW-READS-OLD %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- OLD-REFUSES-NEW --------------------------------------------------------

func TestFixedVersioningOldRefusesNew(t *testing.T) {
	corpus := cFixedCorpus(t)
	for _, r := range cVersionRows {
		t.Run(r.row, func(t *testing.T) {
			t.Parallel()
			older := cReadSchema(t, "VOLD_"+r.row)
			file := filepath.Join(corpus, "new_"+r.row+".bin")
			var body string
			if r.sameHash {
				// A row whose edit moves no layout byte and no digest byte has
				// NO second column: the hashes are equal and the file reads in
				// both directions (§5.7).
				body = `
    if ( n < 1 ) { printf( "a row that moved no layout byte must READ in both directions: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
    if ( r.reason != 0 || r.malformed ) { printf( "an equal hash is not a version: reason=%d\n", r.reason ); return 1; }
`
			} else {
				body = `
    if ( n != -1 ) { printf( "the older reader did not refuse the newer writer's file: n=%lld\n", (long long) n ); return 1; }
    if ( r.reason != SCHEMA_TABLE_LAYOUT_NEWER ) { printf( "a hash in no lineage entry owes layout_newer, not %d\n", r.reason ); return 1; }
    if ( r.layout_hash != want ) { printf( "layout_newer reports THE FILE'S hash\n" ); return 1; }
    if ( r.malformed ) { printf( "a refusal by name never sets malformed too (§5.3, the joint answer)\n" ); return 1; }
    if ( r.widened != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "REFUSE is total: no counter moves\n" ); return 1; }
    if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 ) { printf( "REFUSE wrote destination bytes\n" ); return 1; }
`
			}
			out, err := cRunVersionProbe(t, older, nil, 0, file, body, "")
			if err != nil {
				t.Fatalf("OLD-REFUSES-NEW %s: %v\n%s", r.row, err, out)
			}
		})
	}
}

// ---- the floor --------------------------------------------------------------
//
// The floor is ONE NUMBER and the lineage is ONE ARRAY, so "retired" is an
// index cut and the operator's two answers stay distinct: below the floor is
// `layout_unsupported` (upgrade the client), outside the lineage is
// `layout_newer` (ship the reader). §5.2, §5.7's three floor rows.

func TestFixedVersioningFloor(t *testing.T) {
	corpus := cFixedCorpus(t)
	older := cReadSchema(t, "VOLD_floor")
	mid := cReadSchema(t, "VMID_floor")
	newer := cReadSchema(t, "VNEW_floor")
	oldFile := filepath.Join(corpus, "old_floor.bin")
	midFile := filepath.Join(corpus, "mid_floor.bin")

	t.Run("floor_at", func(t *testing.T) {
		t.Parallel()
		// Nothing retired: the OLDEST file is at the floor and it reads.
		out, err := cRunVersionProbe(t, newer, []string{older, mid}, 0, oldFile, `
    if ( n < 1 || r.reason != 0 || r.malformed ) { printf( "a file AT the floor must read: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("floor_at: %v\n%s", err, out)
		}
	})
	t.Run("floor_below", func(t *testing.T) {
		t.Parallel()
		// Entry 0 retired: the floor is 1, and the file one below it refuses
		// layout_unsupported — nothing decoded, no counter moved.
		out, err := cRunVersionProbe(t, newer, []string{older, mid}, 1, oldFile, `
    if ( n != -1 || r.reason != SCHEMA_TABLE_LAYOUT_UNSUPPORTED )
        { printf( "a file below the floor owes layout_unsupported: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
    if ( r.malformed || r.widened != 0 || r.unknown != 0 || r.clamped != 0 ) { printf( "REFUSE is total\n" ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("floor_below: %v\n%s", err, out)
		}
	})
	t.Run("floor_raise_live", func(t *testing.T) {
		t.Parallel()
		// The floor raised by one: the file that read yesterday refuses today.
		out, err := cRunVersionProbe(t, newer, []string{older, mid}, 2, midFile, `
    if ( n != -1 || r.reason != SCHEMA_TABLE_LAYOUT_UNSUPPORTED )
        { printf( "the floor raised by one: yesterday's file must refuse: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("floor_raise_live: %v\n%s", err, out)
		}
	})
}

// ---- the hash ---------------------------------------------------------------

func TestFixedVersioningHash(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_field_append")
	older := cReadSchema(t, "VOLD_field_append")

	// hash_unknown: a hash in no lineage refuses layout_newer, on the FILE's
	// hash alone. The probe stamps a hash nothing can hold over the header.
	t.Run("hash_unknown", func(t *testing.T) {
		t.Parallel()
		out, err := cRunVersionProbe(t, newer, []string{older}, 0,
			filepath.Join(corpus, "old_field_append.bin"), `
    if ( n != -1 || r.reason != SCHEMA_TABLE_LAYOUT_NEWER || r.layout_hash != 0xDEADBEEFCAFEF00Dull || r.malformed )
        { printf( "hash_unknown: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
`, "", `
    memcpy( data + kTableFixedHashAt, &(uint64_t){ 0xDEADBEEFCAFEF00Dull }, 8 );
`)
		if err != nil {
			t.Fatalf("hash_unknown: %v\n%s", err, out)
		}
	})

	// hash_known_bytes_differ: a KNOWN hash whose layout bytes differ from the
	// lock's is ONE name, layout_malformed — "a lie about a known version". The
	// seven §1.1 malformations under a known hash all land here, never in a
	// runtime walk (§5.3).
	t.Run("hash_known_bytes_differ", func(t *testing.T) {
		t.Parallel()
		out, err := cRunVersionProbe(t, newer, []string{older}, 0,
			filepath.Join(corpus, "old_field_append.bin"), `
    if ( n != -1 || r.reason != SCHEMA_TABLE_LAYOUT_MALFORMED || r.malformed )
        { printf( "hash_known_bytes_differ: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
`, "", `
    data[kTableFixedHeaderBytes + 4] ^= 0xFF;
`)
		if err != nil {
			t.Fatalf("hash_known_bytes_differ: %v\n%s", err, out)
		}
	})

	// hash_identity: the reader's own hash selects the identity plan.
	t.Run("hash_identity", func(t *testing.T) {
		t.Parallel()
		out, err := cRunVersionProbe(t, newer, []string{older}, 0,
			filepath.Join(corpus, "new_field_append.bin"), `
    if ( n < 1 || r.reason != 0 || r.malformed ) { printf( "hash_identity: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
    if ( back[0].w != 777 ) { printf( "the identity plan lost a value: %d\n", back[0].w ); return 1; }
`, "")
		if err != nil {
			t.Fatalf("hash_identity: %v\n%s", err, out)
		}
	})
}

// ---- lineage_merge ----------------------------------------------------------
//
// Two branches append different fields; after the merge BOTH pre-merge files
// read on the merged build (the name-subset rule, bill §8a.1).

func TestFixedVersioningLineageMerge(t *testing.T) {
	corpus := cFixedCorpus(t)
	merged := cReadSchema(t, "VNEW_lineage_merge")
	a := cReadSchema(t, "VBRA_lineage_merge")
	b := cReadSchema(t, "VBRB_lineage_merge")
	base := cReadSchema(t, "VOLD_lineage_merge")
	for _, name := range []string{"a_lineage_merge.bin", "b_lineage_merge.bin"} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			out, err := cRunVersionProbe(t, merged, []string{base, a, b}, 0,
				filepath.Join(corpus, name), `
    if ( n < 1 || r.reason != 0 || r.malformed )
        { printf( "both pre-merge files read on the merged build: n=%lld reason=%d\n", (long long) n, r.reason ); return 1; }
`, "")
			if err != nil {
				t.Fatalf("lineage_merge %s: %v\n%s", name, err, out)
			}
		})
	}
}

// ---- the harness ------------------------------------------------------------

func cFixedCorpus(t *testing.T) string {
	t.Helper()
	dir, err := filepath.Abs("../../../build/fixedform-corpus")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "old_field_append.bin")); err != nil {
		// THE SKIP IS A FAILURE WHEN SOMETHING PROMISED THE CORPUS. A bare
		// `go test ./...` on a tree that never built the oracle has nothing to
		// read and says so; `make tables-c-versioning` builds the corpus first
		// and sets SCHEMA_REQUIRE_CORPUS=1, so under that target a missing file
		// means the build did not do what the target says it did — and a suite
		// that skips itself there would report green over §5 having never run.
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("SCHEMA_REQUIRE_CORPUS is set and the reference's byte oracle is not in %s: %v", dir, err)
		}
		t.Skip("the reference's byte oracle is not built: make tables-fixedform-corpus")
	}
	return dir
}

func cReadSchema(t *testing.T, base string) string {
	t.Helper()
	path := filepath.Join("..", "..", "..", "test", "tables", base+".schema")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func cUnitOf(t *testing.T, src string) *ir.Unit {
	t.Helper()
	f, perrs := parser.Parse("Probe.schema", []byte(src))
	if len(perrs) > 0 {
		t.Fatalf("parse: %v", perrs[0])
	}
	u, cerrs := check.Unit([]check.SourceFile{{
		Path: "Probe.schema", Name: "Probe.schema", Base: "Probe", Bytes: []byte(src), AST: f,
	}})
	if len(cerrs) > 0 {
		t.Fatalf("check: %v", cerrs[0])
	}
	return u
}

// cFixedRootName is §5.9 #9's answer for a corpus row: the file's root is the
// OUTER table, the fixed table no other fixed table of the unit names by value.
func cFixedRootName(t *testing.T, u *ir.Unit) string {
	t.Helper()
	roots := ir.TableFixedRoots(u)
	if len(roots) == 0 {
		t.Fatal("a versioning row declares a fixed root")
	}
	named := map[string]bool{}
	for _, st := range roots {
		for _, f := range st.Fields {
			named[f.Type.Name] = true
		}
	}
	for _, st := range roots {
		if !named[st.Name] {
			return st.Name
		}
	}
	return roots[len(roots)-1].Name
}

// cRunVersionProbe generates the READER's unit as C with the OLDER units'
// locked entries handed to it as its lineage — oldest first, the reader's own
// layout last — marks the first `retire` entries retired, writes a probe main
// that loads `file`, compiles the pair and runs it. `pre` is C that may damage
// the bytes before the load (the hash cases), and `poison` is C that runs AFTER
// the reset and immediately before the load — §5.7's 0x5A over the destination,
// for the rows whose claim is that the PREFILL WRITES.
func cRunVersionProbe(t *testing.T, reader string, older []string, retire int, file, body, poison string, pre ...string) ([]byte, error) {
	t.Helper()
	u := cUnitOf(t, reader)
	lineage := map[string][]FixedLineageEntry{}
	for i, src := range older {
		o := cUnitOf(t, src)
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
	damage := strings.Join(pre, "\n")
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "%s"

int main( void )
{
    static uint8_t data[1 << 20];
    int64_t len;
    FILE * f = fopen( %q, "rb" );
    %s back[8];
    %s fresh;
    uint64_t want;
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t n;
    int k;
    if ( f == NULL ) { printf( "the corpus file is missing\n" ); return 1; }
    len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    if ( len < 20 ) { printf( "the corpus file is short\n" ); return 1; }
    memcpy( &want, data + 8, 8 );
%s
    /* THE PADDING IS ZEROED ON BOTH SIDES BEFORE RESET, which is what makes the
       OLD-REFUSES-NEW byte comparison a statement at all: a generated struct's
       padding is not a value reset covers, so two fresh values differ in their
       slack unless the slack is settled first — the same reason the generated
       load memsets its default image before it resets it (§5.3's "REFUSE writes
       not one destination byte" is about VALUES, and this makes the check see
       only those). */
    memset( &fresh, 0, sizeof( fresh ) );
    memset( back, 0, sizeof( back ) );
    %s( &fresh );
    for ( k = 0; k < 8; ++k ) { %s( &back[k] ); }
%s
    memset( &r, 0, sizeof( r ) );
    n = %s( back, 8, data, len, plan, 4096, NULL, &r );
    (void) want; (void) fresh;
%s
    return 0;
}
`, header, file, root, root, damage, snake+"_reset", snake+"_reset", poison, snake+"_fixed_load", body)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	serialize, err := filepath.Abs("../../../../serialize.c")
	if err != nil {
		t.Fatal(err)
	}
	args := append([]string{"-std=c99", "-O1", "-I" + dir, "-I" + serialize}, gccFortifySilence(cc)...)
	args = append(args, filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe"), "-lm")
	build := exec.Command(cc, args...)
	if out, err := build.CombinedOutput(); err != nil {
		return out, fmt.Errorf("the probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

var _ = binary.LittleEndian

// ---- THE ONCE-FLAG IS PUBLISHED AFTER THE LOOP ------------------------------
//
// This one asserts an ORDER IN THE EMITTED C and needs no corpus, no compiler
// and no thread, because the bug it guards is not a timing bug in a test's eye:
// the flag used to be raised BEFORE the walk that fills the plans, so a second
// thread racing the first older-peer load returned from the build with
// <name>_fixed_lineage still zeros — entries NULL, count 0, reason 0 — and step
// 8 of §5.3 ACCEPTS that shape. The read would land a prefill-only record where
// it owed a refusal. A race is a hard thing to catch twice; the order that makes
// it impossible is one sentence about the generated text, so it is asserted as
// one.
func TestFixedLineageOnceFlagPublishedLast(t *testing.T) {
	t.Parallel()
	newer := cUnitOf(t, cReadSchema(t, "VNEW_field_append"))
	older := cUnitOf(t, cReadSchema(t, "VOLD_field_append"))
	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(older) {
		e, ok := FixedLineageOf(older, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}
	files, err := GenerateLineage(newer, lineage)
	if err != nil {
		t.Fatal(err)
	}
	header := ""
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") && !strings.Contains(name, "/") {
			header = string(data)
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	// the build function's own text, and nothing else's
	const open = "_fixed_lineage_build( void )\n{\n"
	at := strings.Index(header, open)
	if at < 0 {
		t.Fatal("the lazy build of the older plans is not in the emitted C")
	}
	end := strings.Index(header[at:], "\n}\n")
	if end < 0 {
		t.Fatal("the lazy build has no end")
	}
	body := header[at+len(open) : at+end]

	loop := strings.Index(body, "for ( i = 0; i < ")
	publish := strings.Index(body, "table_fixed_once_publish(")
	switch {
	case loop < 0:
		t.Fatal("the build no longer walks the lineage")
	case publish < 0:
		t.Fatal("the build never publishes the once-flag: nothing can tell a built plan from a zeroed one")
	case publish < loop:
		t.Fatalf("THE FLAG IS PUBLISHED BEFORE THE LOOP, so a racing load reads a zeroed "+
			"TableFixedLineagePlan and lands a prefill-only record instead of refusing (§5.9 #7):\n%s", body)
	}
	// and it is published ONCE, after the loop — not also before it
	if n := strings.Count(body, "table_fixed_once_publish("); n != 1 {
		t.Fatalf("the once-flag is published %d times; the order is only a claim if there is one store", n)
	}
	// nothing else writes the flag: no plain assignment anywhere in the body
	if strings.Contains(body, "_fixed_lineage_ready = ") {
		t.Fatalf("the once-flag is assigned directly inside the build — the publish is the only store that may\n%s", body)
	}
	// a load that arrives before the publication WAITS for it rather than
	// reading what is not there (the refuse-or-wait obligation, discharged by
	// waiting: the build is a bounded walk over the lock's own bytes)
	if !strings.Contains(body, "table_fixed_once_claim(") || !strings.Contains(body, "table_fixed_once_wait(") {
		t.Fatalf("a racing load must claim the build or wait for it, never read the zeroed plan\n%s", body)
	}
	if claim, wait := strings.Index(body, "table_fixed_once_claim("), strings.Index(body, "table_fixed_once_wait("); claim > wait || wait > loop {
		t.Fatalf("the claim comes first, then the wait, then the walk\n%s", body)
	}
}
