package ctable

// TestFixedRoadmapCT07Writes is the c leg's card schema-c-t07-writes, the rows
// live-extents and writing of docs/roadmap.sexp's node `fixed-tables`. One
// subtest per task id; each drives the generated C fixed form (the package's
// own harness: runCGenerated for the writer, tableH for the emitted text).
//
// The laws: docs/FIXED-FORM-ALGORITHM.md §4.3 (the prefill) and §4.1 (writes),
// docs/FIXED-FORM-VERSIONING-TESTS.md (the writing rows), and
// docs/SPEC-TABLES.md §3.4.

import (
	"strings"
	"testing"
)

const (
	t07ArraySchema = `package probe
fixed table Grow {
    vals [..4]int32
}
`
	t07UnionSchema = `package probe
type Narrow { x uint8 = 0 }
type Wide { y uint64 = 0 }
union Pick
{
    narrow Narrow
    wide   Wide
}
fixed table Box {
    pick Pick
}
`
	t07BytesSchema = `package probe
fixed table Blob {
    data bytes(4)
}
`
	t07ClampSchema = `package probe
fixed table Rung {
    vals [..4]uint8 | min = 0, max = 100
}
`
)

func TestFixedRoadmapCT07Writes(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/C14", cT07BoundsLiveOnly},
		{"c/W4", cT07ReadSlack},
		{"c/W9", cT07PrefillUnwritten},
		{"c/W1", cT07WriteSlackZeros},
		{"c/W3", cT07ZeroBehindArm},
		{"c/W5", cT07WriteChecksDebug},
		{"c/W8", cT07BytesArrayRow},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// c/C14 "bounds pass walks live only": the read-side bounds pass is the
// generated `<name>_fixed_clamp_body`, and it walks a counted array's LIVE
// elements (`i < value-><name>_count`) and never its slack — an UNWRITTEN slot
// is not held to the declared bound (§3.4, the clamp pass "walks only what a
// read can have written: a counted array's LIVE elements and never its
// slack"). The plan copies the whole array storage, so the test forges the
// file's slack bytes out of range and a pass that walked the fixed bound would
// clamp and count them.
func cT07BoundsLiveOnly(t *testing.T) {
	runCGenerated(t, t07ClampSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main(void)
{
    Rung value, loaded;
    TableReport report;
    int64_t need, n, layout;
    uint8_t file[256];
    uint8_t * body;
    static TableFixedEntry plan[64];
    /* LIVE ONLY: count = 1 with the live element in range, and the file's three
       SLACK slots forged past the declared max. The plan copies the whole array
       storage, so the slack lands in loaded — a bounds pass that walked the
       fixed bound would clamp those three forged slack bytes and count 3. */
    memset(&value, 0, sizeof(value));
    value.vals_count = 1;
    value.vals[0] = 7;
    need = rung_fixed_measure(1);
    if (rung_fixed_save(&value, 1, file, (int64_t)sizeof(file)) != need) { printf("save\n"); return 1; }
    layout = table_fixed_get32(file + kTableFixedHeaderBytes);
    body = file + kTableFixedHeaderBytes + 4 + layout + 8;
    body[5] = 200; body[6] = 200; body[7] = 200;
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = rung_fixed_load(&loaded, 1, file, need, plan, 64, NULL, &report);
    if (n != 1) { printf("n=%lld\n", (long long)n); return 1; }
    if (report.clamped != 0) { printf("the bounds pass walked a slack slot: clamped=%d\n", report.clamped); return 1; }
    /* AND LIVE IS WALKED: count = 2, the second element live and past the max,
       so exactly one clamp lands and the value moves to the bound. */
    value.vals_count = 2;
    value.vals[0] = 7;
    value.vals[1] = 200;
    need = rung_fixed_measure(1);
    if (rung_fixed_save(&value, 1, file, (int64_t)sizeof(file)) != need) { printf("save2\n"); return 1; }
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = rung_fixed_load(&loaded, 1, file, need, plan, 64, NULL, &report);
    if (n != 1) { printf("n2=%lld\n", (long long)n); return 1; }
    if (report.clamped != 1) { printf("a live out-of-range element was not clamped exactly once: clamped=%d\n", report.clamped); return 1; }
    if (loaded.vals[1] != 100) { printf("the clamped value is %d, not the max 100\n", (int)loaded.vals[1]); return 1; }
    return 0;
}
`)
}

// c/W4 "read slack unspecified": §3.4 line for bytes(N) — "ZERO on write past
// the used bytes, UNSPECIFIED on read, never a refusal". A used length shorter
// than the bound rides; the bytes past it may hold ANY value, so the test puts
// non-zero bytes into the record's slack and the read must neither refuse nor
// clamp them.
func cT07ReadSlack(t *testing.T) {
	runCGenerated(t, t07BytesSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main(void)
{
    Blob value, loaded;
    TableReport report;
    int64_t need, n, layout;
    uint8_t file[256];
    uint8_t * body;
    static TableFixedEntry plan[64];
    memset(&value, 0, sizeof(value));
    value.data[0] = 'a'; value.data[1] = 'b'; value.data_length = 2;
    need = blob_fixed_measure(1);
    if (blob_fixed_save(&value, 1, file, (int64_t)sizeof(file)) != need) { printf("save\n"); return 1; }
    /* THE SLACK IS UNSPECIFIED ON READ: the save left the two slack bytes zero
       (the template); write non-zero bytes there, where a reader that wrongly
       validated slack would see them. */
    layout = table_fixed_get32(file + kTableFixedHeaderBytes);
    body = file + kTableFixedHeaderBytes + 4 + layout + 8;
    body[6] = 0xAB; body[7] = 0xAB;
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = blob_fixed_load(&loaded, 1, file, need, plan, 64, NULL, &report);
    if (n != 1) { printf("n=%lld\n", (long long)n); return 1; }
    if (report.refused || report.malformed || report.clamped) { printf("non-zero read slack was refused=%d malformed=%d clamped=%d\n", report.refused, report.malformed, report.clamped); return 1; }
    if (loaded.data_length != 2 || loaded.data[0] != 'a' || loaded.data[1] != 'b') { printf("payload\n"); return 1; }
    return 0;
}
`)
}

// c/W9 "prefill unwritten ranges only": §4.3 — "Prefill the bytes the plan does
// not write ... the identity plan's list is EMPTY". The test drives the runtime
// subtraction `<name>_fixed_fills`: against the identity plan the list is empty,
// and a plan that lands only part of the value bytes yields EXACTLY the bytes it
// does not write.
func cT07PrefillUnwritten(t *testing.T) {
	runCGenerated(t, identityFlatSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main(void)
{
    TableFixedFill out[64];
    TableFixedEntry hole[1];
    int32_t n;
    /* THE IDENTITY PLAN'S LIST IS EMPTY: its destinations ARE the value bytes
       (<name>_fixed_cover), so there is nothing left to subtract (§4.3). */
    n = table_fixed_fills( flat_fixed_plan, flat_fixed_plan_count,
                           flat_fixed_cover, flat_fixed_cover_count, out );
    if (n != 0) { printf("the identity plan's prefill list is not empty: %d ranges\n", n); return 1; }
    /* AND THE LIST IS EXACTLY THE BYTES THE PLAN DOES NOT WRITE: a plan landing
       only the cover's first four bytes leaves the remaining bytes as fill. */
    memset(hole, 0, sizeof(hole));
    hole[0].src = 0;
    hole[0].dst = flat_fixed_cover[0].dst;
    hole[0].size = 4;
    hole[0].op = kTableFixedCopy;
    n = table_fixed_fills( hole, 1, flat_fixed_cover, flat_fixed_cover_count, out );
    if (n != 1) { printf("a plan landing four bytes subtrahends to %d ranges, not 1\n", n); return 1; }
    if (out[0].dst != flat_fixed_cover[0].dst + 4 || out[0].size != flat_fixed_cover[0].size - 4)
        { printf("the fill is not the unwritten bytes: dst=%u size=%u\n", out[0].dst, out[0].size); return 1; }
    return 0;
}
`)
}

// c/W1 "write slack is template zeros": the save memsets the record body to
// zero (the template) and the write body stores only the live bytes, so a
// counted array's bytes past the used ones are the template's zeros (§3.4).
func cT07WriteSlackZeros(t *testing.T) {
	runCGenerated(t, t07ArraySchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main(void)
{
    Grow value;
    int64_t need;
    uint8_t file[256];
    int64_t layout;
    uint8_t * body;
    int i;
    memset(&value, 0x7F, sizeof(value));
    value.vals_count = 1;
    value.vals[0] = 42;
    value.vals[1] = 0x7F7F7F7F; value.vals[2] = 0x7F7F7F7F; value.vals[3] = 0x7F7F7F7F;
    need = grow_fixed_measure(1);
    if (grow_fixed_save(&value, 1, file, (int64_t)sizeof(file)) != need) { printf("save\n"); return 1; }
    layout = table_fixed_get32(file + kTableFixedHeaderBytes);
    body = file + kTableFixedHeaderBytes + 4 + layout + 8;
    if (table_fixed_get32(body) != 1) { printf("count\n"); return 1; }
    for (i = 1; i < 4; ++i) {
        uint8_t * slot = body + 4 + i * 4;
        if (slot[0] || slot[1] || slot[2] || slot[3]) { printf("slack slot %d is not the template's zeros\n", i); return 1; }
    }
    if (value.vals[1] != 0x7F7F7F7F) { printf("the write mutated the value it read\n"); return 1; }
    return 0;
}
`)
}

// c/W3 "zero behind a narrower arm": §4.1 — "A union writes the tag and the
// taken arm only, zero behind a narrower one." The template's zeros stand
// behind a narrow arm, so a wider arm's bytes are zero when the narrow one rode.
func cT07ZeroBehindArm(t *testing.T) {
	runCGenerated(t, t07UnionSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main(void)
{
    Box value;
    int64_t need, layout;
    uint8_t file[256];
    uint8_t * body;
    int i;
    memset(&value, 0x7F, sizeof(value));
    value.pick.type = PICK_TYPE_NARROW;
    value.pick.as.narrow.x = 0xAB;
    need = box_fixed_measure(1);
    if (box_fixed_save(&value, 1, file, (int64_t)sizeof(file)) != need) { printf("save\n"); return 1; }
    layout = table_fixed_get32(file + kTableFixedHeaderBytes);
    body = file + kTableFixedHeaderBytes + 4 + layout + 8;
    if (body[0] != PICK_TYPE_NARROW) { printf("tag=%d\n", body[0]); return 1; }
    if (body[1] != 0xAB) { printf("narrow arm=%x\n", body[1]); return 1; }
    for (i = 2; i < 9; ++i) {
        if (body[i]) { printf("byte behind the narrow arm is 0x%02x, not the template's zero\n", body[i]); return 1; }
    }
    return 0;
}
`)
}

// c/W5 "write checks DEBUG only": the write-side bound is `schema_assert`,
// which NDEBUG removes exactly as it removes `assert` — nothing aborts behind
// it (§3.4). The generated writer carries the assert and no fatal.
func cT07WriteChecksDebug(t *testing.T) {
	h := tableH(t, t07ArraySchema)
	if !strings.Contains(h, "schema_assert( value->vals_count >= 0 && value->vals_count <= 4 )") {
		t.Fatal("the write bound is not schema_assert: it would not be removed by NDEBUG")
	}
	if !strings.Contains(h, "#define schema_assert assert") {
		t.Fatal("schema_assert is not defined as assert: NDEBUG would not remove the write check")
	}
}

// c/W8 "bytes(N) takes the array row": §3.4's bytes(N) row — the used length
// then N bytes; the writer stores the length and copies the used bytes onto the
// template's zeros, the array row's shape.
func cT07BytesArrayRow(t *testing.T) {
	h := tableH(t, t07BytesSchema)
	if !strings.Contains(h, "table_fixed_put32( b + 0, (uint32_t) value->data_length )") {
		t.Fatal("the bytes(N) writer does not store the used length first: it does not take the array row")
	}
	if !strings.Contains(h, "memcpy( b + 4, value->data, (size_t) ( value->data_length ) )") {
		t.Fatal("the bytes(N) writer does not copy the used bytes onto the template's zeros")
	}
}
