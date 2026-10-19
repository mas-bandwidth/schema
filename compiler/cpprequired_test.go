// `| required` on the C++ leg (docs/SPEC-TABLES.md §2.3). Every expected
// answer is the one Go's oracle pins for the same schema text and input:
// internal/codegen/gotable/required_test.go, requiredoffbranch_test.go and
// requirednodefault_test.go.
package compiler

import (
	"testing"
)

func TestCPPRequiredFieldRefusesWhenMissing(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table T
{
    a int32
    b int32 | required
    c int32 | required
}
table Old
{
    a int32
    c int32
}
`
	const body = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdlib.h>
using namespace probe;
static int failures = 0;
#define CHECK(x) do { if (!(x)) { printf("line %d: %s\n", __LINE__, #x); failures++; } } while (0)
int main()
{
    static uint8_t buffer[256];
    // required int32s riding as ZERO ride the wire and read back
    T v; TReset(v); v.a = 7;
    const int64_t n = TSave(&v, buffer, (int64_t) sizeof(buffer));
    CHECK(n > 0);
    int64_t need = TLoadMeasure(buffer, n);
    CHECK(need > 0);
    uint8_t * region = (uint8_t *) malloc((size_t) need);
    TableReport r;
    const T * back = TLoad(region, need, buffer, n, &r);
    CHECK(back != NULL && back->a == 7 && back->b == 0 && back->c == 0);
    CHECK(r.missing_required == 0);
    free(region);
    // an OLDER layout carries only a and c: only b is missing, and only b
    // is named (gotable/required_test.go TestGoTableRequiredFieldRefusesWhenMissing)
    Old old; OldReset(old); old.a = 7; old.c = 4;
    const int64_t m = OldSave(&old, buffer, (int64_t) sizeof(buffer));
    CHECK(m > 0);
    need = TLoadMeasure(buffer, m);
    region = (uint8_t *) malloc((size_t) need);
    TableReport miss;
    CHECK(TLoadTolerant(region, need, buffer, m, &miss) != NULL); // the root, with the count
    CHECK(miss.missing_required == 1);
    CHECK(strcmp(miss.missing_required_fields, "b") == 0);
    free(region);
    region = (uint8_t *) malloc((size_t) need);
    TableReport strict;
    CHECK(TLoad(region, need, buffer, m, &strict) == NULL);
    CHECK(strict.missing_required == 1);
    free(region);
    return failures == 0 ? 0 : 1;
}
`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, body)
}

func TestCPPRequiredRefusalReachesStrictCallers(t *testing.T) {
	t.Parallel()
	// A child body refused for a missing required field refuses every body
	// above it (gotable/required_test.go TestGoTableRequiredRefusalReachesStrictCallers).
	const schema = `package probe
table Kid
{
    x int32 | required
    y int32
}
table OldKid
{
    y int32
}
table Root
{
    a int32 | required
    kid Kid
}
table OldRootOldKid
{
    a int32
    kid OldKid
}
`
	const body = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
using namespace probe;
static int failures = 0;
#define CHECK(x) do { if (!(x)) { printf("line %d: %s\n", __LINE__, #x); failures++; } } while (0)
int main()
{
    static uint8_t buffer[512];
    OldRootOldKid old; OldRootOldKidReset(old); old.a = 3; old.kid.y = 2;
    const int64_t n = OldRootOldKidSave(&old, buffer, (int64_t) sizeof(buffer));
    CHECK(n > 0);
    const int64_t need = RootLoadMeasure(buffer, n);
    CHECK(need > 0);
    uint8_t * region = (uint8_t *) malloc((size_t) need);
    CHECK(region != NULL);
    TableReport r;
    CHECK(RootLoad(region, need, buffer, n, &r) == NULL);
    CHECK(r.missing_required == 1);
    CHECK(strcmp(r.missing_required_fields, "x") == 0);
    free(region);
    return failures == 0 ? 0 : 1;
}
`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, body)
}

func TestCPPRequiredInOffBranch(t *testing.T) {
	t.Parallel()
	// gotable/requiredoffbranch_test.go TestGoRequiredInOffBranch
	const schema = `package probe
table Current
{
    active bool
    if active
    {
        req int32 | required
    }
}
table Legacy
{
    active bool
    if active
    {
        x int32
    }
}
`
	const body = `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
using namespace probe;
static int failures = 0;
#define CHECK(x) do { if (!(x)) { printf("line %d: %s\n", __LINE__, #x); failures++; } } while (0)
static const Current * loadCurrent( const uint8_t * wire, int64_t bytes, TableReport * report, uint8_t ** region )
{
    const int64_t need = CurrentLoadMeasure( wire, bytes );
    if ( need <= 0 ) { return NULL; }
    *region = (uint8_t *) malloc( (size_t) need );
    return CurrentLoad( *region, need, wire, bytes, report );
}
int main()
{
    static uint8_t buffer[512];
    char text[1024];
    // branch OFF: no req rides and none is demanded
    Current c; CurrentReset(c);
    int64_t n = CurrentSave(&c, buffer, (int64_t) sizeof(buffer));
    CHECK(n > 0);
    TableReport r; uint8_t * region = NULL;
    const Current * got = loadCurrent(buffer, n, &r, &region);
    CHECK(got != NULL && r.missing_required == 0);
    free(region);
    // branch ON, present
    c.active = true; c.req = 7;
    n = CurrentSave(&c, buffer, (int64_t) sizeof(buffer));
    r = TableReport(); region = NULL;
    got = loadCurrent(buffer, n, &r, &region);
    CHECK(got != NULL && got->req == 7 && r.missing_required == 0);
    free(region);
    // branch ON, missing (the legacy layout)
    Legacy lg; LegacyReset(lg); lg.active = true; lg.x = 5;
    n = LegacySave(&lg, buffer, (int64_t) sizeof(buffer));
    r = TableReport(); region = NULL;
    CHECK(loadCurrent(buffer, n, &r, &region) == NULL);
    CHECK(r.missing_required == 1 && strcmp(r.missing_required_fields, "req") == 0);
    free(region);
    // JSON: off branch reads, on branch refuses the same absence
    Current off; CurrentReset(off);
    int64_t jn = CurrentToJson(&off, text, (int64_t) sizeof(text));
    CHECK(jn > 0);
    CurrentBuilder jb; TableReport jr;
    CHECK(CurrentFromJson(jb, text, jn, &jr));
    CHECK(jr.missing_required == 0);
    jn = LegacyToJson(&lg, text, (int64_t) sizeof(text));
    CHECK(jn > 0);
    CurrentBuilder nb; jr = TableReport();
    CHECK(!CurrentFromJson(nb, text, jn, &jr));
    CHECK(jr.missing_required == 1 && strcmp(jr.missing_required_fields, "req") == 0);
    return failures == 0 ? 0 : 1;
}
`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, body)
}
