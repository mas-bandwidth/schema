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
// from their emitted runtimes and not one fixed-table test went red. Under the
// strict read both ends REFUSE the whole load `out_of_range` (§4.5, §4.6): C1
// forges the count to -1 and C2 to 9, past the reader's own 8, and neither
// lands a value or moves a counter.

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
    if ( n != -1 ) { printf( "count_clamp_ends C1: n == %lld, want -1 (refused)\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE )
        { printf( "count_clamp_ends C1: refused=%d reason=%d rr=%d, want out_of_range\n", r.refused, r.reason, r.refuse_reason ); return 1; }
    if ( r.unknown != 0 || r.widened != 0 || r.layout_hash != 0 )
        { printf( "count_clamp_ends C1: a counter moved on a refusal: u=%d w=%d hash=%llx\n", r.unknown, r.widened, (unsigned long long) r.layout_hash ); return 1; }
    if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 )
        { printf( "count_clamp_ends C1: a refused read kept a value: lead=%u count=%d trail=%u\n", back[0].lead, back[0].vals_count, back[0].trail ); return 1; }
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
    if ( n != -1 ) { printf( "count_clamp_ends C2: n == %lld, want -1 (refused)\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE )
        { printf( "count_clamp_ends C2: refused=%d reason=%d rr=%d, want out_of_range\n", r.refused, r.reason, r.refuse_reason ); return 1; }
    if ( r.unknown != 0 || r.widened != 0 || r.layout_hash != 0 )
        { printf( "count_clamp_ends C2: a counter moved on a refusal: u=%d w=%d hash=%llx\n", r.unknown, r.widened, (unsigned long long) r.layout_hash ); return 1; }
    if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 )
        { printf( "count_clamp_ends C2: a refused read kept a value: lead=%u count=%d trail=%u\n", back[0].lead, back[0].vals_count, back[0].trail ); return 1; }
`

		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, body, "", pre)
		if err != nil {
			t.Fatalf("count_clamp_ends C2: %v\n%s", err, out)
		}
	})
}
