package ctable

import (
	"path/filepath"
	"testing"
)

// ---- writer_bound_count, two lineage peers (§5.8 row 4) ----------------------
//
// The reader is VNEW_array_bounded_grow ([..8]int32) and it is handed TWO lineage
// peers with DIFFERENT bounds: VOLD_array_bounded_grow ([..4]int32), which WROTE
// the file, and VMID_array_bounded_grow ([..6]int32), the distractor, which did
// not. The number that makes this test worth having is 6: a reader that clamps
// to its own bound lands 8, a reader that clamps to whichever peer it saw last
// lands 6, and only a plan that carries the WRITING peer's bound lands 4.
func TestFixedVersioningWriterBoundCountTwoPeers(t *testing.T) {
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
                /* the forge */
                data[at+4] = 0x07;
                data[at+5] = 0x00;
                data[at+6] = 0x00;
                data[at+7] = 0x00;
            }
        }
        if ( !seen ) { printf( "the count word's needle (AA AA AA AA 04 00 00 00) never occurred\n" ); return 1; }
    }`

	body := `
    if ( n != 1 ) { printf( "writer_bound_count two peers: n == %lld, not 1\n", (long long) n ); return 1; }
    if ( back[0].vals_count != 4 ) { printf( "writer_bound_count two peers: vals_count == %d, not the writer's 4 (never the distractor's 6, never the reader's 8, never the forged 7)\n", back[0].vals_count ); return 1; }
    if ( back[0].vals[0] != 1000 || back[0].vals[1] != 1001 || back[0].vals[2] != 1002 || back[0].vals[3] != 1003 )
        { printf( "writer_bound_count two peers: the writer's values did not land: %d %d %d %d\n", back[0].vals[0], back[0].vals[1], back[0].vals[2], back[0].vals[3] ); return 1; }
    if ( back[0].vals[4] != 0 || back[0].vals[5] != 0 || back[0].vals[6] != 0 || back[0].vals[7] != 0 )
        { printf( "writer_bound_count two peers: the reader's grown slots are not the declared default 0: %d %d %d %d\n", back[0].vals[4], back[0].vals[5], back[0].vals[6], back[0].vals[7] ); return 1; }
    if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
        { printf( "writer_bound_count two peers: a bracket moved: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
    if ( r.clamped != 1 ) { printf( "writer_bound_count two peers: clamped == %d, not exactly 1\n", r.clamped ); return 1; }
    if ( r.unknown != 0 || r.kind_mismatch != 0 || r.widened != 0 || r.duplicate != 0 )
        { printf( "writer_bound_count two peers: a counter moved on a clean hostile read: u=%d km=%d w=%d d=%d\n", r.unknown, r.kind_mismatch, r.widened, r.duplicate ); return 1; }
    if ( r.malformed || r.refused ) { printf( "writer_bound_count two peers: a clean read refused: malformed=%d refused=%d\n", r.malformed, r.refused ); return 1; }
    if ( r.reason != 0 ) { printf( "writer_bound_count two peers: reason == %d, not 0\n", r.reason ); return 1; }
`

	out, err := cRunVersionProbe(t, newer, []string{writer, distractor}, 0,
		filepath.Join(corpus, "old_array_bounded_grow.bin"), body, "", pre)
	if err != nil {
		t.Fatalf("writer_bound_count, two peers: %v\n%s", err, out)
	}
}
