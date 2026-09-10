package ctable

import (
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

const identityFlatSchema = `package probe
table Flat {
    a uint32
    b uint32
}
`

const identityHeldSchema = `package probe
table Held {
    marks [..4]int32
    label string(8)
}
`

const identityPadSchema = `package probe
table Pad {
    a uint8
    b uint32
}
`

func tableH(t *testing.T, src string) string {
	t.Helper()
	files := generate(t, src)
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			return string(data)
		}
	}
	t.Fatal("no Table.h")
	return ""
}

func TestIdentityPlanIsCopiesOnly(t *testing.T) {
	u := unitFrom(t, identityHeldSchema)
	st := u.Tables["Held"]
	raw, _ := ir.TableFixedBuildPlan(u, st)
	var sawCount, sawText bool
	for _, e := range raw {
		if e.Op == ir.TableFixedOpCount {
			sawCount = true
		}
		if e.Op == ir.TableFixedOpText {
			sawText = true
		}
	}
	if !sawCount || !sawText {
		t.Fatal("the schema compiler's identity plan must still name count and text so this rewrite has work to do")
	}
	plan, _ := tableFixedIdentityCopies(raw)
	for _, e := range plan {
		if e.Op != ir.TableFixedOpCopy {
			t.Fatalf("identity copies still carry op %d (%s)", e.Op, e.Note)
		}
	}
}

func TestIdentityLoadSkipsPrefill(t *testing.T) {
	h := tableH(t, identityHeldSchema)
	load := h[strings.Index(h, "held_fixed_load"):]
	end := strings.Index(load, "\n}\n")
	if end < 0 {
		t.Fatal("held_fixed_load not closed")
	}
	load = load[:end]
	if !strings.Contains(load, "if ( identity )") {
		t.Fatal("identity path not branched")
	}
	idStart := strings.Index(load, "if ( identity )")
	elseStart := strings.Index(load, "else")
	id := load[idStart:elseStart]
	if strings.Contains(id, "held_reset") {
		t.Fatal("identity path still prefills defaults")
	}
	if !strings.Contains(load[elseStart:], "held_reset") {
		t.Fatal("compiled path dropped the prefill")
	}
	if strings.Contains(id, "kTableFixedCount") || strings.Contains(id, "kTableFixedText") {
		t.Fatal("identity path still names count/text ops")
	}
	if !strings.Contains(id, "held_fixed_identity_clamps") {
		t.Fatal("identity path dropped the straight-line clamps")
	}
}

func TestIdentityFlatIsMemcpyEmptyScatter(t *testing.T) {
	u := unitFrom(t, identityFlatSchema)
	if !ir.TableFixedFlatType(u, u.Tables["Flat"]) {
		t.Fatal("two uint32s should be a C ABI that matches the wire")
	}
	h := tableH(t, identityFlatSchema)
	if !strings.Contains(h, "memcpy( values + k, at + 8, sizeof( values[k] ) )") {
		t.Fatal("flat identity read is not memcpy")
	}
	load := h[strings.Index(h, "flat_fixed_load"):]
	id := load[strings.Index(load, "if ( identity )"):strings.Index(load, "else")]
	if strings.Contains(id, "table_fixed_run") {
		t.Fatal("flat identity still scatters")
	}
	if strings.Contains(id, "flat_reset") {
		t.Fatal("flat identity still prefills")
	}
}

func TestIdentityPaddedDoesNotMemcpyTheStruct(t *testing.T) {
	u := unitFrom(t, identityPadSchema)
	if ir.TableFixedFlatType(u, u.Tables["Pad"]) {
		t.Fatal("uint8 then uint32 is padded; memcpy of the struct would be a wire-byte move")
	}
	h := tableH(t, identityPadSchema)
	load := h[strings.Index(h, "pad_fixed_load"):]
	id := load[strings.Index(load, "if ( identity )"):strings.Index(load, "else")]
	if strings.Contains(id, "memcpy( values + k, at + 8, sizeof( values[k] ) )") {
		t.Fatal("padded identity memcpy'd the struct; that is a wire-byte move")
	}
	if !strings.Contains(id, "table_fixed_run") {
		t.Fatal("padded identity dropped the copy plan")
	}
}

func TestIdentityHeldRoundTripAndClamp(t *testing.T) {
	runCGenerated(t, identityHeldSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Held value, loaded;
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[512];
    int64_t need, n;
    uint32_t layout_bytes;
    uint8_t * body;
    memset(&value, 0xAA, sizeof(value));
    value.marks[0] = 7;
    value.marks_count = 1;
    value.label[0] = 'h';
    value.label[1] = 'i';
    value.label_length = 2;
    need = held_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(file));
    CHECK(held_fixed_save(&value, 1, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = held_fixed_load(&loaded, 1, file, need, plan, 64, &report);
    CHECK(n == 1);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded.marks_count == 1 && loaded.marks[0] == 7);
    CHECK(loaded.marks[1] == 0 && loaded.marks[2] == 0 && loaded.marks[3] == 0);
    CHECK(loaded.label_length == 2 && loaded.label[0] == 'h' && loaded.label[1] == 'i' && loaded.label[2] == 0);
    layout_bytes = table_fixed_get32(file + kTableFixedHeaderBytes);
    body = file + kTableFixedHeaderBytes + 4 + layout_bytes + 8;
    table_fixed_put32(body, 99);
    table_fixed_put32(body + 4 + 16, 99);
    memset(&loaded, 0, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = held_fixed_load(&loaded, 1, file, need, plan, 64, &report);
    CHECK(n == 1);
    CHECK(report.clamped == 2);
    CHECK(loaded.marks_count == 4);
    CHECK(loaded.label_length == 8);
    CHECK(loaded.label[8] == 0);
    return 0;
}
`)
}

func TestIdentityFlatRoundTripFromDirty(t *testing.T) {
	runCGenerated(t, identityFlatSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Flat value, loaded;
    TableReport report;
    TableFixedEntry plan[8];
    uint8_t file[256];
    int64_t need, n;
    value.a = 1;
    value.b = 2;
    need = flat_fixed_measure(1);
    CHECK(flat_fixed_save(&value, 1, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = flat_fixed_load(&loaded, 1, file, need, plan, 8, &report);
    CHECK(n == 1);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(loaded.a == 1 && loaded.b == 2);
    return 0;
}
`)
}
