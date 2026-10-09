package ctable

// The union_unselected_arm row (docs/FIXED-FORM-VERSIONING-TESTS.md, schema#1157,
// 2026-09-19, LAWFUL AS IS): after a read that RETURNS, an arm the landed tag did
// not select is UNDEFINED — one word, for every target (Glenn, 2026-09-19: "as
// designed it is 'undefined'"). A union read DEFINES the tag and the SELECTED arm
// and nothing else. THIS ROW'S CONTENT IS THE ASSERTION IT REFUSES TO MAKE: it
// does not compare pick.beta or pick.gamma. That this leg OVERLAYS its arms — a
// real C union has no other arm to reset — is an OBSERVATION AND NOT A GUARANTEE,
// and nothing may be relied on it; whatever this leg does with an unselected arm
// is lawful. A reader who "completes" this test by
// asserting pick.gamma.p == 0 has reversed a ruling and should read the issue
// first. Named *_arm_row_test.go, not *_arm_test.go: Go reads a trailing _arm
// before _test.go as a GOARCH build constraint and silently skips the file.

import (
	"path/filepath"
	"testing"
)

func TestFixedVersioningUnionUnselectedArm(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_union_append")
	older := cReadSchema(t, "VOLD_union_append")
	file := filepath.Join(corpus, "old_union_append.bin")

	// The selected arm and the tag are the whole of what a union read promises.
	// Nothing here names beta or gamma.
	body := `
    if ( n != 1 ) { printf( "the file carries one record, not %lld\n", (long long) n ); return 1; }
    if ( back[0].pick.type != PICK_TYPE_ALPHA ) { printf( "pick.type is the writer's alpha arm (1), not %d\n", (int) back[0].pick.type ); return 1; }
    if ( back[0].pick.as.alpha.m != 7 ) { printf( "pick.alpha.m is the writer's 7, not %d\n", (int) back[0].pick.as.alpha.m ); return 1; }
    if ( back[0].seq != 15 ) { printf( "seq is the writer's 15, not %d\n", (int) back[0].seq ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.clamped != 0 || r.duplicate != 0 || r.widened != 0 || r.retained != 0 || r.retain_lost != 0 )
        { printf( "a clean backward read moved a counter: unknown=%d kind_mismatch=%d clamped=%d duplicate=%d widened=%d retained=%d retain_lost=%d\n", r.unknown, r.kind_mismatch, r.clamped, r.duplicate, r.widened, r.retained, r.retain_lost ); return 1; }
    if ( r.malformed ) { printf( "a clean backward read is not malformed\n" ); return 1; }
    if ( r.refused || r.reason != 0 ) { printf( "a clean backward read is not a refusal: refused=%d reason=%d\n", r.refused, r.reason ); return 1; }
`

	// probe 1, the compiled column: reader VNEW, older VOLD.
	if out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, body, ""); err != nil {
		t.Fatalf("union_unselected_arm, compiled column: %v\n%s", err, out)
	}

	// probe 2, the identity column: reader VOLD, no older. Its own hash, its own
	// plan, the same file.
	if out, err := cRunVersionProbe(t, older, nil, 0, file, body, ""); err != nil {
		t.Fatalf("union_unselected_arm, identity column: %v\n%s", err, out)
	}
}
