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

// c/C14 "bounds pass walks live only": the read-side bounds pass is the one
// loop's `table_fixed_run`, and it clamps only the entries that LAND — an
// unselected arm's entries are skipped by `table_fixed_entry_lands` before a
// byte is read. §3.4's one loop, and the tag selects the live arm.
func cT07BoundsLiveOnly(t *testing.T) {
	h := tableH(t, cReadSchema(t, "VNEW_union_append"))
	if !strings.Contains(h, "table_fixed_entry_lands") {
		t.Fatal("the one read loop does not gate each entry on table_fixed_entry_lands: the bounds pass would walk an unselected arm's bytes")
	}
	if !strings.Contains(h, "table_fixed_apply") {
		t.Fatal("the one read loop's bounds pass (table_fixed_apply) is not emitted")
	}
}

// c/W4 "read slack unspecified": §3.4 line for bytes(N) — "ZERO on write past
// the used bytes, UNSPECIFIED on read, never a refusal". A used length shorter
// than the bound rides; the bytes past it may hold anything, and the read
// neither refuses nor clamps them.
func cT07ReadSlack(t *testing.T) {
	runCGenerated(t, t07BytesSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main(void)
{
    Blob value, loaded;
    TableReport report;
    int64_t need, n;
    uint8_t file[256];
    static TableFixedEntry plan[64];
    memset(&value, 0, sizeof(value));
    value.data[0] = 'a'; value.data[1] = 'b'; value.data_length = 2;
    need = blob_fixed_measure(1);
    if (blob_fixed_save(&value, 1, file, (int64_t)sizeof(file)) != need) { printf("save\n"); return 1; }
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = blob_fixed_load(&loaded, 1, file, need, plan, 64, NULL, &report);
    if (n != 1) { printf("n=%lld\n", (long long)n); return 1; }
    if (report.refused || report.malformed || report.clamped) { printf("refused/malformed/clamped on read slack\n"); return 1; }
    if (loaded.data_length != 2 || loaded.data[0] != 'a' || loaded.data[1] != 'b') { printf("payload\n"); return 1; }
    return 0;
}
`)
}

// c/W9 "prefill unwritten ranges only": §4.3 — "Prefill the bytes the plan does
// not write ... The plan compiler computes the UNWRITTEN RANGES ... the
// identity plan's list is EMPTY". The generated reader carries a fill list and
// the identity plan names no fill.
func cT07PrefillUnwritten(t *testing.T) {
	h := tableH(t, identityHeldSchema)
	if !strings.Contains(h, "TableFixedFill") {
		t.Fatal("the reader carries no fill list; the prefill is not a list of unwritten ranges (§4.3)")
	}
	if !strings.Contains(h, "fill_count") {
		t.Fatal("the plan carries no fill_count; the prefill is not the bytes the plan does not write (§4.3)")
	}
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
