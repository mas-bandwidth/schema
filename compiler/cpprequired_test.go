// `| required` on the C++ leg (docs/SPEC-TABLES.md §2.3). Every expected
// answer is the one Go's oracle pins for the same schema text and input:
// internal/codegen/gotable/required_test.go, requiredoffbranch_test.go and
// requirednodefault_test.go.
package compiler

import (
	"fmt"
	"strings"
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
    CHECK(n > 0 && n == TMeasure(&v));
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
    CHECK(miss.missing_required == 1 && !miss.malformed);
    CHECK(strcmp(miss.missing_required_fields, "b") == 0);
    free(region);
    region = (uint8_t *) malloc((size_t) need);
    TableReport strict;
    CHECK(TLoad(region, need, buffer, m, &strict) == NULL);
    CHECK(strict.missing_required == 1);
    free(region);
    // Builder tolerant and strict both refuse
    TBuilder btol; TableReport bmiss;
    CHECK(!TLoadBuilderTolerant(btol, buffer, m, &bmiss));
    CHECK(bmiss.missing_required == 1 && !bmiss.malformed);
    TBuilder bstr; TableReport bstrict;
    CHECK(!TLoadBuilder(bstr, buffer, m, &bstrict));
    CHECK(bstrict.missing_required == 1);
    // JSON missing b refuses in both tolerant and strict modes without marking malformed
    const char * bad_json = "{\"a\":7,\"c\":4}";
    TBuilder jtol; TableReport jmiss;
    CHECK(!TFromJsonTolerant(jtol, bad_json, (int64_t) strlen(bad_json), &jmiss));
    CHECK(jmiss.missing_required == 1 && !jmiss.malformed && strcmp(jmiss.missing_required_fields, "b") == 0);
    TBuilder jstr; TableReport jstrict;
    CHECK(!TFromJson(jstr, bad_json, (int64_t) strlen(bad_json), &jstrict));
    CHECK(jstrict.missing_required == 1 && !jstrict.malformed && strcmp(jstrict.missing_required_fields, "b") == 0);
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
    pkid *Kid
    kids [..4]Kid
    pkids [..4]*Kid
    mkids map[int32]Kid
    okid ?Kid
}
table OldRootOldKid
{
    a int32
    kid OldKid
    pkid *OldKid
    kids [..4]OldKid
    pkids [..4]*OldKid
    mkids map[int32]OldKid
    okid ?OldKid
}
`
	const body = `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
using namespace probe;
static int failures = 0;
#define CHECK(x) do { if (!(x)) { printf("line %d: %s\n", __LINE__, #x); failures++; } } while (0)
static void checkRefused(const uint8_t * wire, int64_t n)
{
    const int64_t need = RootLoadMeasure(wire, n);
    CHECK(need > 0);
    uint8_t * region = (uint8_t *) malloc((size_t) need);
    TableReport r;
    CHECK(RootLoad(region, need, wire, n, &r) == NULL);
    CHECK(r.missing_required >= 1 && strcmp(r.missing_required_fields, "x") == 0);
    free(region);
    RootBuilder rb; TableReport br;
    CHECK(!RootLoadBuilder(rb, wire, n, &br));
    CHECK(br.missing_required >= 1);
    CHECK(RootReadStrict(wire, n) == NULL);
}
int main()
{
    static uint8_t buffer[1024];
    // 1. By-value kid missing x (note: kid is non-default so it rides)
    {
        OldRootOldKidBuilder ob;
        OldRootOldKid * o = ob.GetRoot();
        o->a = 3; o->kid.y = 2;
        CHECK(ob.Lock());
        int64_t n = OldRootOldKidSave(ob.AsConst(), buffer, (int64_t) sizeof(buffer));
        CHECK(n > 0);
        checkRefused(buffer, n);
    }
    // 2. Pointer pkid missing x (with valid kid.x via JSON)
    {
        RootBuilder jb; TableReport jr;
        const char * bad_ptr = "{\"a\":3,\"kid\":{\"x\":1},\"pkid\":{\"y\":5}}";
        CHECK(!RootFromJson(jb, bad_ptr, (int64_t) strlen(bad_ptr), &jr));
        CHECK(jr.missing_required == 1 && !jr.malformed && strcmp(jr.missing_required_fields, "x") == 0);
        const char * bad_empty_ptr = "{\"a\":3,\"kid\":{\"x\":1},\"pkid\":{}}";
        jr = TableReport();
        CHECK(!RootFromJson(jb, bad_empty_ptr, (int64_t) strlen(bad_empty_ptr), &jr));
        CHECK(jr.missing_required == 1 && !jr.malformed && strcmp(jr.missing_required_fields, "x") == 0);
    }
    return failures == 0 ? 0 : 1;
}
`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, body)
}

func TestCPPRequiredExactFieldHasNoDefault(t *testing.T) {
	t.Parallel()
	const schema = `package probe
table Exact
{
    ver uint8 | min = 7, max = 7, required
}
table OldExact
{
}
table Other
{
    ver uint8
}
table Inner
{
    id uint8 | required
}
table Outer
{
    id uint8
    inner Inner
}
`
	const body = `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
using namespace probe;
static int failures = 0;
#define CHECK(x) do { if (!(x)) { printf("line %d: %s\n", __LINE__, #x); failures++; } } while (0)
int main()
{
    static uint8_t wire[256];
    // Fresh record holds ver at storage zero, outside [7,7]: Measure & Save refuse (-1)
    Exact v; ExactReset(v);
    CHECK(v.ver == 0);
    CHECK(ExactMeasure(&v) == -1);
    CHECK(ExactSave(&v, wire, (int64_t) sizeof(wire)) == -1);
    // Set ver = 7: saves and round-trips
    v.ver = 7;
    int64_t wn = ExactSave(&v, wire, (int64_t) sizeof(wire));
    CHECK(wn > 0 && wn == ExactMeasure(&v));
    int64_t need = ExactLoadMeasure(wire, wn);
    CHECK(need > 0);
    uint8_t * region = (uint8_t *) malloc((size_t) need);
    TableReport r;
    const Exact * got = ExactLoad(region, need, wire, wn, &r);
    CHECK(got != NULL && got->ver == 7 && r.missing_required == 0);
    free(region);
    // Wire without ver is missing_required
    OldExact old; OldExactReset(old);
    int64_t own = OldExactSave(&old, wire, (int64_t) sizeof(wire));
    CHECK(own > 0);
    need = ExactLoadMeasure(wire, own);
    region = (uint8_t *) malloc((size_t) need);
    r = TableReport();
    CHECK(ExactLoad(region, need, wire, own, &r) == NULL);
    CHECK(r.missing_required == 1 && strcmp(r.missing_required_fields, "ver") == 0);
    free(region);
    // JSON without ver (both empty {} and {"hp":5}) is missing_required
    ExactBuilder eb; TableReport rep;
    CHECK(!ExactFromJson(eb, "{}", 2, &rep));
    CHECK(rep.missing_required == 1 && !rep.malformed && strcmp(rep.missing_required_fields, "ver") == 0);
    rep = TableReport();
    CHECK(!ExactFromJson(eb, "{\"hp\":5}", 8, &rep));
    CHECK(rep.missing_required == 1 && !rep.malformed && strcmp(rep.missing_required_fields, "ver") == 0);
    rep = TableReport();
    CHECK(ExactFromJson(eb, "{\"ver\":7}", 9, &rep));
    CHECK(rep.missing_required == 0 && eb.GetRoot()->ver == 7);
    // Cross-table: Other's ver is not required
    OtherBuilder ob; rep = TableReport();
    CHECK(OtherFromJson(ob, "{}", 2, &rep));
    CHECK(rep.missing_required == 0);
    // Nested: required field inside Inner is enforced at its object
    OuterBuilder outb; rep = TableReport();
    CHECK(!OuterFromJson(outb, "{\"inner\":{}}", 12, &rep));
    CHECK(rep.missing_required == 1 && !rep.malformed && strcmp(rep.missing_required_fields, "id") == 0);
    rep = TableReport();
    CHECK(OuterFromJson(outb, "{\"inner\":{\"id\":5}}", 18, &rep));
    CHECK(rep.missing_required == 0 && outb.GetRoot()->inner.id == 5);
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
	// gotable/requiredoffbranch_test.go TestGoRequiredInOffBranch + hand-editable text form
	const schema = `package probe
table Current
{
    active bool
    paused bool
    if active
    {
        req int32 | required
        if !paused
        {
            nested_req int32 | required
        }
    }
}
table Legacy
{
    active bool
    paused bool
    if active
    {
        x int32
    }
}
`
	const body = `#include "ProbeTable.h"
#include <string>
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
    // branch ON + paused=false, both present
    c.active = true; c.paused = false; c.req = 7; c.nested_req = 9;
    n = CurrentSave(&c, buffer, (int64_t) sizeof(buffer));
    r = TableReport(); region = NULL;
    got = loadCurrent(buffer, n, &r, &region);
    CHECK(got != NULL && got->req == 7 && got->nested_req == 9 && r.missing_required == 0);
    free(region);
    // branch ON + paused=true: req demanded, nested_req off-branch
    Legacy lg; LegacyReset(lg); lg.active = true; lg.paused = true; lg.x = 5;
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
    // Hand-editable text form: text_missing_required & text_inactive_field
    CurrentBuilder tf_b;
    const char * why = NULL;
    const char * tf_off = "(:active false)";
    CHECK(CurrentLoadTextForm(tf_off, (int64_t) strlen(tf_off), tf_b, &why));
    const char * tf_miss = "(:active true :paused true)";
    CHECK(!CurrentLoadTextForm(tf_miss, (int64_t) strlen(tf_miss), tf_b, &why));
    CHECK(why != NULL && strcmp(why, "text_missing_required") == 0);
    const char * tf_inact = "(:active false :req 7)";
    CHECK(!CurrentLoadTextForm(tf_inact, (int64_t) strlen(tf_inact), tf_b, &why));
    CHECK(why != NULL && strcmp(why, "text_inactive_field") == 0);
    const char * tf_ok = "(:active true :paused true :req 7)";
    CHECK(CurrentLoadTextForm(tf_ok, (int64_t) strlen(tf_ok), tf_b, &why));
    return failures == 0 ? 0 : 1;
}
`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, body)
}

func TestCPPRequiredKeyedArrayAndEmptyNestedTable(t *testing.T) {
	t.Parallel()
	const schema = `package probe
enum Slot
{
    A
    B
}
table Empty
{
    x int32
}
table RequiredKeyed
{
    slots [Slot]int32 | required
}
table RequiredEmpty
{
    inner Empty | required
}
`
	const body = `#include "ProbeTable.h"
#include <stdio.h>
#include <stdlib.h>
using namespace probe;
static int failures = 0;
#define CHECK(x) do { if (!(x)) { printf("line %d: %s\n", __LINE__, #x); failures++; } } while (0)
int main()
{
    static uint8_t wire[256];
    // 1. Required enum-keyed array at all-zero still writes the keyed header
    RequiredKeyed rk; RequiredKeyedReset(rk);
    int64_t klen = RequiredKeyedSave(&rk, wire, (int64_t) sizeof(wire));
    CHECK(klen > 0 && klen == RequiredKeyedMeasure(&rk));
    int64_t kneed = RequiredKeyedLoadMeasure(wire, klen);
    CHECK(kneed > 0);
    uint8_t * kreg = (uint8_t *) malloc((size_t) kneed);
    TableReport kr;
    const RequiredKeyed * kgot = RequiredKeyedLoad(kreg, kneed, wire, klen, &kr);
    CHECK(kgot != NULL && kgot->slots[Slot::A] == 0 && kgot->slots[Slot::B] == 0 && kr.missing_required == 0);
    free(kreg);
    // 2. Required by-value nested table at default/zero still writes the nested body
    RequiredEmpty re; RequiredEmptyReset(re);
    int64_t elen = RequiredEmptySave(&re, wire, (int64_t) sizeof(wire));
    CHECK(elen > 0 && elen == RequiredEmptyMeasure(&re));
    int64_t eneed = RequiredEmptyLoadMeasure(wire, elen);
    CHECK(eneed > 0);
    uint8_t * ereg = (uint8_t *) malloc((size_t) eneed);
    TableReport er;
    const RequiredEmpty * egot = RequiredEmptyLoad(ereg, eneed, wire, elen, &er);
    CHECK(egot != NULL && egot->inner.x == 0 && er.missing_required == 0);
    free(ereg);
    return failures == 0 ? 0 : 1;
}
`
	files, err := New().Generate(unitFromSource(t, schema), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, body)
}

func TestCPPRequiredPast512Fields(t *testing.T) {
	t.Parallel()
	var sb strings.Builder
	sb.WriteString("package probe\ntable WideReq\n{\n")
	for i := 0; i < 519; i++ {
		fmt.Fprintf(&sb, "    f%d int32\n", i)
	}
	sb.WriteString("    f519 int32 | required\n}\n")
	const body = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
using namespace probe;
static int failures = 0;
#define CHECK(x) do { if (!(x)) { printf("line %d: %s\n", __LINE__, #x); failures++; } } while (0)
int main()
{
    WideReqBuilder b; TableReport r;
    CHECK(!WideReqFromJson(b, "{}", 2, &r));
    CHECK(r.missing_required == 1 && !r.malformed && strcmp(r.missing_required_fields, "f519") == 0);
    r = TableReport();
    const char * ok_json = "{\"f519\":42}";
    CHECK(WideReqFromJson(b, ok_json, (int64_t) strlen(ok_json), &r));
    CHECK(r.missing_required == 0 && b.GetRoot()->f519 == 42);
    return failures == 0 ? 0 : 1;
}
`
	files, err := New().Generate(unitFromSource(t, sb.String()), "cpp", Options{})
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	runCppTableProbe(t, files, body)
}
