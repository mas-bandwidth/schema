package cpptable

// THE T12 ROADMAP SUITE (docs/roadmap.sexp, node fixed-tables) on the CPP leg:
// cpp/E3/fixed-array-element (docs/roadmap.sexp:2040), the versioning row
// `fixed_I_grow_element` (docs/FIXED-FORM-VERSIONING-TESTS.md:57) in BOTH of
// its columns. fixedversioning_test.go carried the row with no check body and
// no OLD-REFUSES-NEW column; this file is where both live.
//
// cpp/R9/seven-corruptions (docs/roadmap.sexp:835) is proven in C++, not here:
// test/tables/fixedform_main.cpp's layout_validation() through refuses(), run by
// `make tables-fixedform`, which poisons the destination and sweeps it.

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestFixedRoadmapCppT12PlansEvolution is table-free: two subtests, one per
// clause of cpp/E3/fixed-array-element, each opening with t.Parallel().
func TestFixedRoadmapCppT12PlansEvolution(t *testing.T) {
	t.Parallel()
	corpus := cppFixedCorpus(t)
	const row = "fixed_I_grow_element"
	vold := filepath.Join("..", "..", "..", "test", "tables", "VOLD_"+row+".schema")

	// cpp/E3/fixed-array-element/new-reads-old — clause one. The NEW reader
	// ([4]fixed(28,4)) reads the OLD writer's [4]fixed(12,4) file through its
	// lineage, and every slot widens "as the scalar row, per slot"
	// (FIXED-FORM-VERSIONING-TESTS.md:57; ALGORITHM §5.4's widen op). The raw
	// values are the C leg's (ctable/fixedversioning_test.go, the
	// fixed_I_grow_element check body): -1, -128, 0, 112. The brackets are the
	// corpus oracle's (cppBrackets, §5.9 #32), never a constant.
	t.Run("cpp/E3/fixed-array-element/new-reads-old", func(t *testing.T) {
		t.Parallel()
		older := loadUnit(t, vold)
		root := cppFixedRoot(t, older)
		lead, trail := cppBrackets(t, corpus, row, ir.TableFixedTypeBytes(root))
		body := fmt.Sprintf(`
    if ( n != 1 ) { printf( "the fixed_I_grow_element file carries one record, not %%lld (reason=%%d)\n", (long long) n, (int) r.reason ); return 1; }
    if ( r.reason != newer_form || r.malformed || r.refused )
        { printf( "a clean NEW-READS-OLD is not a refusal: reason=%%d malformed=%%d refused=%%d\n", (int) r.reason, (int) r.malformed, (int) r.refused ); return 1; }
    {
        const int32_t want[4] = { -1, -128, 0, 112 };
        for ( int k = 0; k < 4; ++k )
            if ( (int32_t) back[0].vals[k] != want[k] )
                { printf( "slot %%d widened wrong: %%d, want %%d\n", k, (int) back[0].vals[k], (int) want[k] ); return 1; }
    }
    if ( r.widened == 0 ) { printf( "a widening row moved no widened counter\n" ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 )
        { printf( "counters moved on a clean widening read: u=%%d km=%%d c=%%d d=%%d\n", r.unknown, r.kind_mismatch, r.clamped, r.duplicate ); return 1; }
    if ( back[0].lead != 0x%08Xu || back[0].trail != 0x%08Xu )
        { printf( "the row moved a neighbour: lead=%%u trail=%%u\n", back[0].lead, back[0].trail ); return 1; }
`, lead, trail)
		out, err := cppRunVersionProbe(t, row, filepath.Join(corpus, "old_"+row+".bin"), body, "")
		if err != nil {
			t.Fatalf("cpp/E3/fixed-array-element/new-reads-old: %v\n%s", err, out)
		}
	})

	// cpp/E3/fixed-array-element/old-refuses-new — clause two. The OLD
	// generation is compiled ALONE (cppRunCapability's cppRunUnit copies only
	// the VOLD_ file, so its lineage is the identity entry and nothing else) and handed
	// the NEW writer's file. The NEW hash is in no lineage entry: REFUSE
	// layout_newer, reporting the file's hash AND NOTHING ELSE (ALGORITHM §5.3
	// step 5 and its outcome table). REFUSE is total — no counter moves and not
	// one destination byte is written, the prefill included (ALGORITHM §5.3,
	// "REFUSE is total"): the storage is poisoned 0x5A AFTER the Reset and must
	// be 0x5A in every byte after the load.
	t.Run("cpp/E3/fixed-array-element/old-refuses-new", func(t *testing.T) {
		t.Parallel()
		older := loadUnit(t, vold)
		root := cppFixedRootName(t, older)
		file := filepath.Join(corpus, "new_"+row+".bin")
		if _, err := os.Stat(file); err != nil {
			t.Fatalf("the corpus carries no NEW-side file for %s: %v", row, err)
		}
		body := fmt.Sprintf(`
    static uint8_t data[1 << 20];
    FILE * f = fopen( %[2]q, "rb" );
    if ( f == NULL ) { printf( "the corpus file is missing\n" ); return 1; }
    const int64_t len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    if ( len < 20 ) { printf( "the corpus file is short\n" ); return 1; }
    const uint64_t want = TableFixedGet64( data + kTableFixedHashAt ); // the file's header hash
    if ( want == %[1]sFixedHash ) { printf( "the NEW file carries the OLD hash: the row proves nothing\n" ); return 1; }
    %[1]s back[8];
    for ( int k = 0; k < 8; ++k ) %[1]sReset( back[k] );
    memset( back, 0x5A, sizeof( back ) ); // THE POISON goes after the Reset, before the load
    TableReport r;
    static TableFixedEntry plan[4096];
    const int64_t n = %[1]sFixedLoad( back, 8, data, len, plan, 4096, NULL, &r );
    if ( n != -1 ) { printf( "the older reader did not refuse the newer writer's file: n=%%lld\n", (long long) n ); return 1; }
    if ( !r.refused ) { printf( "refused is false\n" ); return 1; }
    if ( r.reason != layout_newer ) { printf( "a hash in no lineage entry owes layout_newer, not %%d\n", (int) r.reason ); return 1; }
    if ( r.malformed ) { printf( "a refusal by name never sets malformed too (§5.3, the joint answer)\n" ); return 1; }
    if ( r.layout_hash != want )
        { printf( "layout_newer reports THE FILE'S hash: got %%016llx want %%016llx\n", (unsigned long long) r.layout_hash, (unsigned long long) want ); return 1; }
    if ( r.widened != 0 || r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 || r.retained != 0 || r.retain_lost != 0 )
        { printf( "REFUSE is total, no counter moves: w=%%d u=%%d km=%%d c=%%d d=%%d\n", r.widened, r.unknown, r.kind_mismatch, r.clamped, r.duplicate ); return 1; }
    {
        const uint8_t * p = (const uint8_t *) back;
        for ( size_t i = 0; i < sizeof( back ); ++i )
            if ( p[i] != 0x5A ) { printf( "byte %%zu is 0x%%02x: REFUSE wrote a destination byte\n", i, p[i] ); return 1; }
    }
`, root, file)
		// cppRunUnit is cppRunCapability with the sibling serialize checkout on
		// the include path: this unit's header names serialize.h's 128-bit
		// storage type (cpptable.go's unitHas128), the same directory
		// cppRunVersionProbe passes. It copies the VOLD_ file alone all the same.
		serialize, err := filepath.Abs("../../../../serialize")
		if err != nil {
			t.Fatal(err)
		}
		out, err := cppRunUnit(t, vold, older.Package, body, []string{serialize})
		if err != nil {
			t.Fatalf("cpp/E3/fixed-array-element/old-refuses-new: %v\n%s", err, out)
		}
	})
}
