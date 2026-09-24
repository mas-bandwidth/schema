package ctable

import (
	"path/filepath"
	"testing"
)

// count_clamp_ends is the roadmap's c/C1 and c/C2, whose titles are "count
// clamp v<0" and "count clamp v>Max" — quoted here from docs/roadmap.sexp.
// Row 4's forge of 7 sits between the writer's 4 and the reader's 8, so it
// exercises neither end. The negative arm was measured on 2026-09-19 to be
// guarded by NOTHING on five legs (c, cs, dart, elixir and js): it was deleted
// from their emitted runtimes and not one fixed-table test went red. C1 forges
// the count to -1 and owes a landed 0; C2 forges it to 9 and owes the WRITER's
// 4, never the reader's own 8 and never the forged 9. `clamped` is asserted
// == 1 and never >= 1: a leg that counts the clamp in the `count` op and again
// in a bounds pass lands 2 and passes a looser read this row exists to catch.

func TestFixedVersioningCountClampEnds(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_array_bounded_grow")
	older := cReadSchema(t, "VOLD_array_bounded_grow")
	file := filepath.Join(corpus, "old_array_bounded_grow.bin")

	t.Run("C1_negative", func(t *testing.T) {
		pre := `
    {
        int seen = 0;
        int64_t at;
        for ( at = 0; at + 8 <= len; ++at )
        {
            if ( data[at] == 0xAA && data[at+1] == 0xAA && data[at+2] == 0xAA && data[at+3] == 0xAA &&
                 data[at+4] == 0x04 && data[at+5] == 0x00 && data[at+6] == 0x00 && data[at+7] == 0x00 )
            {
                if ( seen ) { printf( "the count word's needle matched twice: the forge has no single locator\n" ); return 1; }
                seen = 1;
                /* the forge: -1, little-endian int32 */
                data[at+4] = 0xFF;
                data[at+5] = 0xFF;
                data[at+6] = 0xFF;
                data[at+7] = 0xFF;
            }
        }
        if ( !seen ) { printf( "the count word's needle (AA AA AA AA 04 00 00 00) never occurred\n" ); return 1; }
    }`

		body := `
    if ( n != 1 ) { printf( "count_clamp_ends C1: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "count_clamp_ends C1: a forged count moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].vals_count != 0 ) { printf( "count_clamp_ends C1: the negative count did not clamp to zero: %d\n", back[0].vals_count ); return 1; }
    if ( r.clamped != 1 ) { printf( "count_clamp_ends C1: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "count_clamp_ends C1: a counter moved on a clean hostile read: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "count_clamp_ends C1: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "count_clamp_ends C1: reason == %d, not 0\n", r.reason ); return 1; }
`

		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, body, "", pre)
		if err != nil {
			t.Fatalf("count_clamp_ends C1: %v\n%s", err, out)
		}
	})

	t.Run("C2_past_reader_bound", func(t *testing.T) {
		pre := `
    {
        int seen = 0;
        int64_t at;
        for ( at = 0; at + 8 <= len; ++at )
        {
            if ( data[at] == 0xAA && data[at+1] == 0xAA && data[at+2] == 0xAA && data[at+3] == 0xAA &&
                 data[at+4] == 0x04 && data[at+5] == 0x00 && data[at+6] == 0x00 && data[at+7] == 0x00 )
            {
                if ( seen ) { printf( "the count word's needle matched twice: the forge has no single locator\n" ); return 1; }
                seen = 1;
                /* the forge: 9, past the writer's 4 and past the reader's 8 */
                data[at+4] = 0x09;
                data[at+5] = 0x00;
                data[at+6] = 0x00;
                data[at+7] = 0x00;
            }
        }
        if ( !seen ) { printf( "the count word's needle (AA AA AA AA 04 00 00 00) never occurred\n" ); return 1; }
    }`

		body := `
    if ( n != 1 ) { printf( "count_clamp_ends C2: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "count_clamp_ends C2: a forged count moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( back[0].vals_count != 4 ) { printf( "count_clamp_ends C2: vals_count == %d, not the writer's 4\n", back[0].vals_count ); return 1; }
    if ( back[0].vals[0] != 1000 || back[0].vals[1] != 1001 || back[0].vals[2] != 1002 || back[0].vals[3] != 1003 )
        { printf( "count_clamp_ends C2: the writer's values did not land: %d %d %d %d\n", back[0].vals[0], back[0].vals[1], back[0].vals[2], back[0].vals[3] ); return 1; }
    if ( back[0].vals[4] != 0 || back[0].vals[5] != 0 || back[0].vals[6] != 0 || back[0].vals[7] != 0 )
        { printf( "count_clamp_ends C2: the reader's grown slots are not the declared default 0: %d %d %d %d\n", back[0].vals[4], back[0].vals[5], back[0].vals[6], back[0].vals[7] ); return 1; }
    if ( r.clamped != 1 ) { printf( "count_clamp_ends C2: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "count_clamp_ends C2: a counter moved on a clean hostile read: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "count_clamp_ends C2: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "count_clamp_ends C2: reason == %d, not 0\n", r.reason ); return 1; }
`

		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, body, "", pre)
		if err != nil {
			t.Fatalf("count_clamp_ends C2: %v\n%s", err, out)
		}
	})
}
