package ctable

import (
	"path/filepath"
	"testing"
)

// ---- writer_bound_count, two lineage endpoints (§5.8 row 4) ----------------------
//
// The reader is VNEW_array_bounded_grow ([..8]int32) and it is handed TWO lineage
// endpoints with DIFFERENT bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE
// the file, and VMID_array_bounded_grow ([..6]int32), the distractor, which did
// not. The number that makes this test worth having is 5: a reader that uses its
// own bound (8) or the distractor's (6) would LAND a count of 5, and only a plan
// that carries the WRITING endpoint's bound (4) refuses it `out_of_range`.
func TestFixedVersioningWriterBoundCountTwoRemotes(t *testing.T) {
	corpus := cFixedCorpus(t)
	newer := cReadSchema(t, "VNEW_array_bounded_grow")
	writer := cReadSchema(t, "VOLD_array_bounded_grow")
	distractor := cReadSchema(t, "VMID_array_bounded_grow")

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
                /* the forge: 5, past the writer's 4 but inside the distractor's 6 */
                data[at+4] = 0x05;
                data[at+5] = 0x00;
                data[at+6] = 0x00;
                data[at+7] = 0x00;
            }
        }
        if ( !seen ) { printf( "the count word's needle (AA AA AA AA 04 00 00 00) never occurred\n" ); return 1; }
    }`

	body := `
    if ( n != -1 ) { printf( "writer_bound_count two endpoints: n == %lld, want -1 (refused)\n", (long long) n ); return 1; }
    if ( !r.refused || r.reason != 0 || r.refuse_reason != SCHEMA_TABLE_REFUSE_OUT_OF_RANGE )
        { printf( "writer_bound_count two endpoints: refused=%d reason=%d rr=%d, want out_of_range\n", r.refused, r.reason, r.refuse_reason ); return 1; }
    if ( r.unknown != 0 || r.widened != 0 || r.layout_hash != 0 )
        { printf( "writer_bound_count two endpoints: a counter moved on a refusal: u=%d w=%d hash=%llx\n", r.unknown, r.widened, (unsigned long long) r.layout_hash ); return 1; }
    if ( memcmp( &back[0], &fresh, sizeof( fresh ) ) != 0 )
        { printf( "writer_bound_count two endpoints: a refused read kept a value: lead=%u count=%d trail=%u\n", back[0].lead, back[0].vals_count, back[0].trail ); return 1; }
`

	out, err := cRunVersionProbe(t, newer, []string{writer, distractor}, 0,
		filepath.Join(corpus, "old_array_bounded_grow.bin"), body, "", pre)
	if err != nil {
		t.Fatalf("writer_bound_count, two endpoints: %v\n%s", err, out)
	}
}
