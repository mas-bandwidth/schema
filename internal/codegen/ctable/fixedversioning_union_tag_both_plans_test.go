package ctable

// union_tag_both_plans: a forged union TAG is a different emitter site from a
// forged ENUM ORDINAL on every leg but elixir, and the identity plan's half of
// it was guarded by NOTHING. The forge sets the one-byte `pick.type` tag of
// `old_union_append.bin`'s single record to 9 — past the OLD writer's two arms
// and the NEW reader's three — located by the nine-byte needle
// 01 07 00 00 00 0F 00 00 00, asserted to occur EXACTLY ONCE. Both plans read
// the same forged bytes: the compiled plan (VNEW reader, VOLD in its lineage)
// and the identity plan (VOLD reader on its own hash).
//
// MEASURED 2026-09-19 ON THE GATE RIG AT 380f1cad, one leg at a time, each edit
// counted to exactly 1 and each file restored and proved restored: the union tag
// bound was deleted from the emitter of ALL NINE LEGS and not one of 36 to 72
// landed fixed-table tests went red on any of them. `forged_ordinal_both_plans`
// (§5.8 row 12) forges an ENUM ordinal, which is a different site; darttable's
// `TestFixedCompiledPlanTagPastArmSet` reads the COMPILED plan only. Nothing
// anywhere read the identity half.
//
// BOTH HALVES ASSERT `clamped == 1`, AND THAT IS schema#1254 CLOSED ON THIS LEG.
// When this row first landed the compiled half asserted the LANDING and not the
// count, because over these same forged bytes the identity plan counted ONE and
// the compiled plan counted ZERO — while both of this repo's own comments say
// they must agree: fixedruntime.go's kTableFixedOrdinal ("the compiled plan
// counts it exactly as the identity plan's bounds pass does, so the SAME forged
// bytes land the SAME clamped == 1 on either plan") and darttable/fixeddart.go's
// union decode ("the same landing the compiled plan gives it through its own
// remap, so the identity path and a stranger's plan agree about a hostile tag").
// dart and cpp held the invariant; c did not.
//
// THE CAUSE WAS TWO MISSING PIECES, NOT ONE, AND cpptable ALREADY CARRIED BOTH.
// The compiled plan lands a tag naming no shared arm through the UNGUARDED None
// `kTableFixedConst` entry. (a) That entry never carried the WRITER'S ARM COUNT
// in `dstsize`, so nothing downstream could tell a tag past the old set from one
// inside it; (b) `case kTableFixedConst:` was a bare memcpy with no counter,
// where the enum path's kTableFixedOrdinal counts. Both are now ctable's, copied
// from cpptable, and the negative control below is what proves they are live.
//
// MEASURED, each edit counted to exactly 1 and the file restored and proved
// byte-for-byte: with `if ( raw > (uint64_t) p->dstsize ) { (*clamped)++; }`
// turned into `if ( 0 && raw > ... )` the whole leg is 74 leaves and EXACTLY ONE
// red — this row's `compiled` subtest. Nothing else on the leg reads that block.

import (
	"fmt"
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionTagBothPlans(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_union_append")
	older := cReadSchema(t, "VOLD_union_append")
	file := filepath.Join(corpus, "old_union_append.bin")

	// THE LOCATOR, and it must match EXACTLY ONCE: a search that is not a
	// locator is not a forge. The record body is the file's last nine bytes —
	// the eight before it are that record's layout hash.
	pre := `
    {
        int seen = 0;
        int64_t at;
        for ( at = 0; at + 9 <= len; ++at )
        {
            if ( data[at] == 0x01 && data[at+1] == 0x07 && data[at+2] == 0x00 && data[at+3] == 0x00 &&
                 data[at+4] == 0x00 && data[at+5] == 0x0F && data[at+6] == 0x00 && data[at+7] == 0x00 && data[at+8] == 0x00 )
            {
                if ( seen ) { printf( "the record body (pick.type=alpha, alpha.m=7, seq=15) occurs more than once\n" ); return 1; }
                seen = 1;
                data[at] = 9; /* the forge: a tag past the old writer's two arms and the new reader's three */
            }
        }
        if ( !seen ) { printf( "the record body (pick.type=alpha, alpha.m=7, seq=15) is not in the file\n" ); return 1; }
    }`

	// clampedOnce is `== 1` and never `>= 1`: a leg that counts the clamp in the
	// plan's op AND again in the decode bounds lands 2, and the looser read is
	// exactly what this row exists to catch.
	clampedOnce := `
    if ( r.clamped != 1 ) { printf( "union_tag_both_plans: clamped is the bounds pass's count, once per field: %d\n", r.clamped ); return 1; }`

	body := func(clamped string) string {
		return fmt.Sprintf(`
    if ( n != 1 ) { printf( "union_tag_both_plans: the forged file reads one record, not %%lld\n", (long long) n ); return 1; }
    if ( r.refused || r.malformed || r.reason != 0 )
        { printf( "union_tag_both_plans: a forged tag is a clamp and nothing else: refused=%%d malformed=%%d reason=%%d\n", r.refused, r.malformed, r.reason ); return 1; }
    if ( back[0].pick.type != PICK_TYPE_NONE ) { printf( "union_tag_both_plans: the forged tag lands None (0), not %%d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].seq != 15 ) { printf( "union_tag_both_plans: the scalar after the union must stand at 15, not %%d\n", (int) back[0].seq ); return 1; }%s
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "union_tag_both_plans: only clamped may move on a forged tag: u=%%d km=%%d w=%%d d=%%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
`, clamped)
	}

	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		// The NEW build reads the OLD file through the lineage, and it counts
		// the clamp the identity plan counts: schema#1254, fixed above.
		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, body(clampedOnce), "", pre)
		if err != nil {
			t.Fatalf("union_tag_both_plans, the compiled plan: %v\n%s", err, out)
		}
	})
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		// The OLD build reads its own file on its own hash. THIS is the half
		// nothing guarded: delete the emitter's union tag bound and this
		// subtest, and only this subtest, goes red.
		out, err := cRunVersionProbe(t, older, nil, 0, file, body(clampedOnce), "", pre)
		if err != nil {
			t.Fatalf("union_tag_both_plans, the identity plan: %v\n%s", err, out)
		}
	})
}
