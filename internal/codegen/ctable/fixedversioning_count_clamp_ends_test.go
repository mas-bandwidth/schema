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
// from their emitted runtimes and not one fixed-table test went red. The OWNER
// RULE (2026-10-15) replaced the count clamp with a NAMED REFUSAL
// (docs/FIXED-FORM-ALGORITHM.md §4.5, the `count` op): C1 forges the count to
// -1 and C2 to 9, past the writer's 4 and the reader's 8, and each owes the
// refusal before any destination byte is written — nothing landed, no counter
// moved.

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

		body := cCountOverBoundBody("count_clamp_ends C1")

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

		body := cCountOverBoundBody("count_clamp_ends C2")

		out, err := cRunVersionProbe(t, newer, []string{older}, 0, file, body, "", pre)
		if err != nil {
			t.Fatalf("count_clamp_ends C2: %v\n%s", err, out)
		}
	})
}
