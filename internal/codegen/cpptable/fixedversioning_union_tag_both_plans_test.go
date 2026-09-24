package cpptable

// union_tag_both_plans: a forged union TAG is a different emitter site from a
// forged ENUM ordinal on every leg but dart, and it was guarded by NOTHING. The
// forge sets the ONE-BYTE `pick.type` tag of `old_union_append.bin`'s single
// record to 9 — past the OLD writer's two arms and the NEW reader's three —
// located by the nine-byte needle 01 07 00 00 00 0F 00 00 00, asserted to occur
// EXACTLY ONCE before the write. `clamped` is `== 1` and never `>= 1`: a leg
// that counts the tag in the plan's op AND again in the decode projection lands
// 2, and the looser read is precisely what this row exists to catch.
//
// MEASURED 2026-09-19 ON THE GATE RIG AT 380f1cad, one leg at a time, each edit
// counted to exactly 1 and each file restored and proved restored: the union tag
// bound was deleted from the emitter of ALL NINE LEGS and not one landed
// fixed-table test went red — c 0 of 72, cpp 0 of 36, cs 0 of 70, dart 0 of 69,
// elixir 0 of 72, go 0 of 68, java 0 of 41, js 0 of 70, rust 0 of 70.
//
// THIS FILE HOLDS THE COMPILED HALF ONLY, and it asserts clamped == 1 because
// that half counts ONE on this leg: the count comes from the RUNTIME's
// kTableFixedConst counter (bill §12.5, fixedruntime.go), not from the
// scatter-side clamp body — which is why CONTROL 3 (deleting the emitter's
// union tag bound) does NOT move the compiled half. c's compiled half counts
// ZERO there (schema#1254); cpp is not that leg. The identity half is a NAMED
// RESIDUAL, not a fake: cppRunVersionProbe(t, row, file, body, forge) takes NO
// reader and NO lineage — it always compiles the NEW generation and finds the
// VOLD_ peer through the nil-lock sibling-filename convention — so there is no
// identity-plan call on this leg without a reader argument of the kind ctable's
// cRunVersionProbe already has. What is owed: that reader argument, then the
// identity half asserting the same landing (None, seq 15, clamped == 1) over
// the same forged bytes; the emitter site CONTROL 3 guards is the identity
// half's guard, and it is the half nothing on this leg can put to the question.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := cppFixedCorpus(t)
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE FORGE: the record body is the file's last nine bytes — the eight
	// before it are that record's layout hash. The tag is ONE byte, pick.type
	// = 1 (the alpha arm), then alpha.m = 7 (little-endian int32) and seq = 15,
	// so the nine-byte needle is 01 07 00 00 00 0F 00 00 00. Find it, assert it
	// occurs EXACTLY once — a search that is not a locator is not a forge — and
	// write 9 over the first byte: past the old writer's two arms AND the new
	// reader's three.
	forge := `
    {
        static const uint8_t needle[9] = { 0x01, 0x07, 0x00, 0x00, 0x00, 0x0F, 0x00, 0x00, 0x00 };
        int64_t found = -1;
        for ( int64_t i = 0; i + 9 <= len; ++i )
        {
            if ( memcmp( data + i, needle, 9 ) == 0 )
            {
                if ( found >= 0 ) { printf( "the locator (pick.type=alpha, alpha.m=7, seq=15) matched twice\n" ); return 1; }
                found = i;
            }
        }
        if ( found < 0 ) { printf( "the locator (pick.type=alpha, alpha.m=7, seq=15) matched nowhere\n" ); return 1; }
        data[found] = 9; /* the forge */
    }
`

	body := `
    if ( n != 1 ) { printf( "the forged file reads one record, not %lld\n", (long long) n ); return 1; }
    if ( r.refused || r.malformed || r.reason != newer_form )
        { printf( "the forged read is not a refusal: refused=%d malformed=%d reason=%d\n", (int) r.refused, (int) r.malformed, (int) r.reason ); return 1; }
    if ( back[0].pick.type != PickType::None ) { printf( "the forged tag lands None (0), not %d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].seq != 15 ) { printf( "the scalar after the union must stand at 15, not %d\n", (int) back[0].seq ); return 1; }
    if ( r.clamped != 1 ) { printf( "clamped is the bounds pass's count, once per field: %d\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "only clamped may move on a forged tag: unknown=%d kind_mismatch=%d widened=%d duplicate=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
`

	out, err := cppRunVersionProbe(t, "union_append", file, body, forge)
	if err != nil {
		t.Fatalf("union_tag_both_plans, the compiled plan: %v\n%s", err, out)
	}
}
