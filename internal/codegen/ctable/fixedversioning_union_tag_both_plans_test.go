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
// BOTH HALVES OWE THE SAME VERDICT. A tag past the plan's arm count REFUSES
// out_of_range in the bounds pass, on the compiled plan exactly as on the
// identity one (docs/FIXED-FORM-ALGORITHM.md §4.6, the tag and ordinal rulings
// of 2026-11-03), and the strict read lands nothing (SPEC-TABLES §6.5). This row
// once pinned the retired clamp (`clamped == 1` on both plans, schema#1254);
// the equality of the two halves is still the row's whole proof, now on the
// refusal.

import (
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

	body := cRefusedBody("union_tag_both_plans", "SCHEMA_TABLE_REFUSE_OUT_OF_RANGE")

	t.Run("compiled", func(t *testing.T) {
		t.Parallel()
		// The NEW build reads the OLD file through the lineage.
		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, body, "", pre)
		if err != nil {
			t.Fatalf("union_tag_both_plans, the compiled plan: %v\n%s", err, out)
		}
	})
	t.Run("identity", func(t *testing.T) {
		t.Parallel()
		// The OLD build reads its own file on its own hash. THIS is the half
		// nothing guarded: delete the emitter's union tag bound and this
		// subtest goes red.
		out, err := cRunVersionProbe(t, older, nil, 0, file, body, "", pre)
		if err != nil {
			t.Fatalf("union_tag_both_plans, the identity plan: %v\n%s", err, out)
		}
	})
}
