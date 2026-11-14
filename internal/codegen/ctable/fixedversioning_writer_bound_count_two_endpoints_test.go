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
// not. The count word is forged to 7 — past the WRITING endpoint's bound of 4.
// The OWNER RULE (2026-10-15) replaced the count clamp with a NAMED REFUSAL: the
// forged count refuses `count_over_bound` against the WRITER's bound carried by
// the plan, before any destination byte is written — the Go oracle's row
// (internal/codegen/gotable/fixedversioning_writer_bound_count_two_endpoints_test.go).
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
                /* the forge */
                data[at+4] = 0x07;
                data[at+5] = 0x00;
                data[at+6] = 0x00;
                data[at+7] = 0x00;
            }
        }
        if ( !seen ) { printf( "the count word's needle (AA AA AA AA 04 00 00 00) never occurred\n" ); return 1; }
    }`

	body := cCountOverBoundBody("writer_bound_count two endpoints")

	out, err := cRunVersionProbe(t, newer, []string{writer, distractor}, 0,
		filepath.Join(corpus, "old_array_bounded_grow.bin"), body, "", pre)
	if err != nil {
		t.Fatalf("writer_bound_count, two endpoints: %v\n%s", err, out)
	}
}
