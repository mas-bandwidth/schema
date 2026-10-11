package ctable

// TestFixedRoadmapCT08WritesConformance is the c leg's card
// fixed-c-t08-writes-conformance, rows union-guards, plan-execution,
// interoperability and hostile-input of docs/roadmap.sexp's node
// `fixed-tables`. One subtest per task this card proves, named for the task id.
//
// The laws are docs/FIXED-FORM-ALGORITHM.md §4.1 (guards answer to the outer
// tag), §5.4 (the plan's partition and split), §5.8 row 6 (the guard chain and
// the layout's 64), §5.9 #12 (the hash is the shared function's) and
// docs/SPEC-TABLES.md §3.4 (the fixed message form). The identity==compiled and
// write-read-write clauses are SPEC-TABLES §3.4's "the fixed form reads and
// writes the same body".

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/ir"
)

func TestFixedRoadmapCT08WritesConformance(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"c/W7", cT08ArgAndMetaTwoLanes},
		{"c/W16", cT08NestedUnionAnswersOuterTag},
		{"c/R28", cT08GuardChain},
		{"c/W6", cT08PlanPartitionSplit},
		{"c/P1", cT08IdentityEqualsCompiled},
		{"c/W13", cT08LayoutHashEqualsReference},
		{"c/P2", cT08WriteReadWriteIdentical},
		{"c/R27", cT08ManifestIsTheWire},
		{"c/P3", cT08HostileByteSweep},
		{"c/P4", cT08WrongPlanGoesRed},
	}
	for _, row := range rows {
		t.Run(row.id, func(t *testing.T) {
			t.Parallel()
			row.run(t)
		})
	}
}

// cT08UnionSchema is a union whose first arm carries a text field, so a leaf
// under that arm carries BOTH a guard's tag (Arg) and the text op's flavour
// (Meta) at once.
const cT08UnionSchema = `package probe
type Payload { label string(8) = "" }
type Other { n int32 = 0 }
union Pick {
    q Other
    p Payload
}
fixed table Root { pick Pick }
`

// c/W7 — "arg and meta are two lanes". FIXED-FORM-ALGORITHM §4.1: the guard's
// tag rides in the plan entry's arg lane; SPEC-TABLES §3.4: a text field's
// flavour is the op's own meta lane. A text field under a union arm is the one
// entry where both lanes are live, so the test builds exactly that and asserts
// each lane carries its own fact: Arg is the arm's ordinal and Meta the text
// flavour. A port that stamped the flavour over the tag (or the reverse) fails.
func cT08ArgAndMetaTwoLanes(t *testing.T) {
	u := unitFrom(t, cT08UnionSchema)
	st := u.Tables["Root"]
	plan, _ := ir.TableFixedBuildPlan(u, st)
	var leaf *ir.TableFixedLeaf
	for i := range plan {
		if plan[i].Op == ir.TableFixedOpText {
			leaf = &plan[i]
			break
		}
	}
	if leaf == nil {
		t.Fatal("the plan lost the arm's text op")
	}
	if leaf.Guard == ir.TableFixedNoGuard {
		t.Fatal("the text field under a union arm is unguarded: the guard lane was dropped")
	}
	if leaf.Arg != 2 {
		t.Fatalf("arg is the guard's tag and nothing else: want second arm ordinal 2, got %d", leaf.Arg)
	}
	if leaf.Meta != 1 {
		t.Fatalf("meta is the op's own argument: want utf8 flavour 1, got %d", leaf.Meta)
	}
}

// cT08NestedUnionSchema nests a union inside an arm of an outer union, so the
// inner arm's leaves carry both tags: the inner one on guard/arg and the outer
// one on guard2/arg2.
const cT08NestedUnionSchema = `package probe
type InnerA { x int32 = 0 }
type InnerB { n int32 = 0 }
union Inner {
    ia InnerA
    ib InnerB
}
type OuterA { inner Inner }
type OuterB { m int32 = 0 }
union Outer {
    oa OuterA
    ob OuterB
}
fixed table Root { o Outer }
`

// c/W16 [owed] — "nested union answers outer tag". FIXED-FORM-ALGORITHM §4.1
// and §5.8 row 6: an arm inside an arm answers to the OUTER tag as well as its
// own, on a second lane that is a CONJUNCTION, not a replacement. The inner
// arm's leaf therefore carries guard/arg for the inner tag and guard2/arg2 for
// the outer. A port that stamped the outer tag over the inner one (so every
// inner arm fired the moment the outer rode) leaves guard2 unset.
func cT08NestedUnionAnswersOuterTag(t *testing.T) {
	u := unitFrom(t, cT08NestedUnionSchema)
	st := u.Tables["Root"]
	plan, _ := ir.TableFixedBuildPlan(u, st)
	var nested *ir.TableFixedLeaf
	for i := range plan {
		if plan[i].Guard != ir.TableFixedNoGuard && plan[i].Guard2 != ir.TableFixedNoGuard {
			nested = &plan[i]
			break
		}
	}
	if nested == nil {
		t.Fatal("no entry answers to two tags: the outer tag was stamped over the inner one")
	}
	if nested.Arg != 1 || nested.Arg2 != 1 {
		t.Fatalf("the inner arm's own ordinal and the outer arm's ordinal must both be 1: arg=%d arg2=%d", nested.Arg, nested.Arg2)
	}
	if nested.ArgW == 0 || nested.ArgW2 == 0 {
		t.Fatalf("each tag is compared at its own width: argw=%d argw2=%d", nested.ArgW, nested.ArgW2)
	}
	if nested.Guard == nested.Guard2 {
		t.Fatal("the outer tag was stamped at the inner tag's offset: the two conditions are not independent")
	}
}

// cT08TripleUnionSchema nests three unions on one path to a leaf: an arm of
// OuterMost holds Outer, whose arm holds Middle, whose arm holds Inner, whose
// arm is the leaf. The build walk refuses this third nested union by name.
const cT08TripleUnionSchema = `package probe
type Leaf { x int32 = 0 }
union Inner { a Leaf }
type Mid { i Inner }
union Middle { m Mid }
type Out { mm Middle }
union OuterMost { o Out }
fixed table Root { oo OuterMost }
`

// c/R28 [owed] — "the guard chain: the plan entry carries offset+count into a
// pool of (guard, arg, argw) links, outermost first, the only bound the
// layout's 64; an arm inside an arm answers to the OUTER tag". The OUTER-TAG
// half is landed as a CONJOINED second INLINE lane (docs/FIXED-FORM-ALGORITHM.md
// §5.8 row 6); the pooled chain is the follow-on and is NOT landed — the entry
// carries at most two inline lanes, and a THIRD nested union refuses by name in
// the build walk rather than being carried. This subtest pins exactly that
// reality: two lanes each carrying (guard, arg, argw) at its own width, and a
// third nested union refused, so a port that claims the chain while leaving the
// two-lane walk behind is caught. The full task's verdict stays unknown until
// the offset+count-into-a-pool lands.
func cT08GuardChain(t *testing.T) {
	u := unitFrom(t, cT08NestedUnionSchema)
	st := u.Tables["Root"]
	plan, _ := ir.TableFixedBuildPlan(u, st)
	lanes := 0
	for i := range plan {
		e := plan[i]
		if e.Guard == ir.TableFixedNoGuard {
			continue
		}
		if e.Arg < 1 || e.ArgW < 1 || e.Guard < 0 {
			t.Fatalf("a guarded entry carries (guard, arg, argw): guard=%d arg=%d argw=%d", e.Guard, e.Arg, e.ArgW)
		}
		lanes++
		if e.Guard2 != ir.TableFixedNoGuard {
			if e.Arg2 < 1 || e.ArgW2 < 1 {
				t.Fatalf("the outer lane carries (guard2, arg2, argw2): arg2=%d argw2=%d", e.Arg2, e.ArgW2)
			}
			lanes++
		}
	}
	if lanes < 2 {
		t.Fatal("an arm inside an arm answers to the OUTER tag: fewer than two lanes were stamped")
	}
	// THE POOLED CHAIN IS THE FOLLOW-ON, NOT LANDED (§5.8 row 6): a third nested
	// union refuses by name rather than being carried, so the two inline lanes
	// are the whole of the entry's guard today.
	func() {
		defer func() {
			if recover() == nil {
				t.Fatal("a third nested union is carried, but the guard chain is not landed: the build must refuse it by name")
			}
		}()
		three := unitFrom(t, cT08TripleUnionSchema)
		ir.TableFixedBuildPlan(three, three.Tables["Root"])
	}()
}

// c/W6 [owed] — "plan partition / split". FIXED-FORM-ALGORITHM §5.4: the plan
// is PARTITIONED, every unguarded entry first and every guarded one after, and
// `guarded` is where the second half starts; neighbours are never merged across
// the split. The test asserts the index is in range, that the halves hold
// exactly the entries they claim, and that the coalescer did not merge an
// unguarded copy into a guarded one.
func cT08PlanPartitionSplit(t *testing.T) {
	u := unitFrom(t, cT08NestedUnionSchema)
	st := u.Tables["Root"]
	plan, guarded := ir.TableFixedBuildPlan(u, st)
	if guarded < 0 || guarded > len(plan) {
		t.Fatalf("the guarded split index %d is out of range for %d entries", guarded, len(plan))
	}
	for i := range plan {
		unguarded := plan[i].Guard == ir.TableFixedNoGuard
		if i < guarded && !unguarded {
			t.Fatalf("entry %d before the split is guarded", i)
		}
		if i >= guarded && unguarded {
			t.Fatalf("entry %d after the split is unguarded", i)
		}
	}
	// the split is real: the nested schema carries both halves
	if guarded == 0 || guarded == len(plan) {
		t.Fatalf("the partition has an empty half: guarded=%d of %d", guarded, len(plan))
	}
}

// c/P1 [owed] — "identity == compiled". SPEC-TABLES §3.4: the fixed form reads
// and writes one body, so the identity plan and a plan compiled against the
// reader's own layout are the same data walked by the same loop. The emitted C
// proves it: the load is the one table_fixed_run loop with no separate identity
// scatter and no whole-body memcpy.
func cT08IdentityEqualsCompiled(t *testing.T) {
	files := generate(t, cT08UnionSchema)
	var header string
	for name, data := range files {
		if strings.HasSuffix(name, "Table.h") {
			header += string(data)
		}
	}
	if !strings.Contains(header, "table_fixed_run") {
		t.Fatal("the load is not the one loop: identity and compiled plans are not one path")
	}
	if strings.Contains(header, "fixed_scatter") {
		t.Fatal("a scatter function survives: identity is a second path")
	}
	if strings.Contains(header, "memcpy( (void *) ( values + k ), at + 8") {
		t.Fatal("the load memcpys the whole body: identity is not the plan")
	}
}

// cT08LayoutSchema is W13's probe: a ranged scalar so the layout is non-trivial
// and the hash folds the definitions digest, beside two plain scalars.
const cT08LayoutSchema = `package probe
fixed table Root {
    lead uint32 = 1
    v    int32 = 0 | min = 0, max = 100
    tail uint32 = 2
}
`

// c/W13 [verify] — "layout+hash == C++ reference". FIXED-FORM-ALGORITHM §1 (the
// layout's bytes) and §5.9 #12 ("THE HASH IS THE SHARED FUNCTION'S:
// ir.TableFixedLayoutHash, whose signature has no digest argument for a leg to
// omit"). The C++ reference's layout and hash are the shared functions' answers
// (ir.TableFixedLayoutBytes / ir.TableFixedLayoutHash); the emitted C header
// spells them out as `<snake>_fixed_layout` and `<snake>_fixed_hash`, and the
// test reads those back out of the header and asserts they equal the shared
// functions' answers byte for byte, so the C leg cannot have drifted from the
// reference's layout or hash.
func cT08LayoutHashEqualsReference(t *testing.T) {
	u := unitFrom(t, cT08LayoutSchema)
	st := u.Tables["Root"]
	if st == nil {
		t.Fatal("the unit declares no table Root")
	}
	layout := ir.TableFixedLayoutBytes(ir.TableFixedWalkRoot(st))
	hash := ir.TableFixedLayoutHash(layout, st)
	text := t03Header(t, u)
	snake := ir.RustSnake(st.Name)
	if got := t03Hash(t, text, snake); got != hash {
		t.Fatalf("the emitted hash 0x%016x is not the C++ reference's 0x%016x (ir.TableFixedLayoutHash)", got, hash)
	}
	if got := t03Layout(t, text, snake); !bytes.Equal(got, layout) {
		t.Fatalf("the emitted layout is %d bytes, not the C++ reference's %d (ir.TableFixedLayoutBytes)", len(got), len(layout))
	}
}

// c/P2 [owed] — "write-read-write byte-identical". SPEC-TABLES §3.4: a saved
// fixed image read back and saved again is the same bytes. The probe writes a
// value with a non-default count and text, reads it into a dirty destination,
// writes again, and memcmps the two images byte for byte.
func cT08WriteReadWriteIdentical(t *testing.T) {
	runCGenerated(t, identityHeldSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Held value, loaded;
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t first[512], second[512];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value.marks[0] = 7;
    value.marks_count = 1;
    value.label[0] = 'h';
    value.label[1] = 'i';
    value.label_length = 2;
    need = held_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(first));
    CHECK(held_fixed_save(&value, 1, first, (int64_t)sizeof(first)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = held_fixed_load(&loaded, 1, first, need, plan, 64, NULL, &report);
    CHECK(n == 1);
    CHECK(!report.malformed && !report.refused && report.clamped == 0);
    CHECK(held_fixed_save(&loaded, 1, second, (int64_t)sizeof(second)) == need);
    CHECK(memcmp(first, second, (size_t)need) == 0);
    return 0;
}
`, identityCCFlags()...)
}

// c/R27 [verify] — "the manifest is the wire". FIXED-FORM-ALGORITHM §5.9
// #32/#37: build/fixedform-corpus/manifest.txt is the SINGLE VALUE ORACLE every
// leg reads its expected values from, never from the bytes under test and never
// from a hard-coded literal. The manifest's stated lead/trail for
// old_array_bounded_grow.bin are read off the manifest and asserted to BE the
// bytes actually on the wire — the record body's first and last four. A
// manifest that went missing (the oracle is the .bin again) or that drifted
// from the dump that wrote the bytes fails by name.
func cT08ManifestIsTheWire(t *testing.T) {
	corpus := cFixedCorpus(t)
	lead, trail, ok := cManifestBrackets(t, corpus, "old_array_bounded_grow.bin")
	if !ok {
		t.Fatal("the manifest is the single value oracle: it carries no brackets for old_array_bounded_grow.bin")
	}
	data, err := os.ReadFile(filepath.Join(corpus, "old_array_bounded_grow.bin"))
	if err != nil || len(data) < 20 {
		t.Fatalf("the corpus row is not readable: %v", err)
	}
	root := cFixedRoot(t, cUnitOf(t, cReadSchema(t, "VOLD_array_bounded_grow")))
	bodyBytes := ir.TableFixedTypeBytes(root)
	at := 20 + int(binary.LittleEndian.Uint32(data[16:20])) + 8
	if int64(at)+bodyBytes > int64(len(data)) {
		t.Fatal("the corpus row is shorter than its own record")
	}
	body := data[at : int64(at)+bodyBytes]
	first := binary.LittleEndian.Uint32(body[:4])
	last := binary.LittleEndian.Uint32(body[len(body)-4:])
	if first != lead || last != trail {
		t.Fatalf("the manifest is not the wire: manifest lead=%d trail=%d, the wire's own first=%d last=%d", lead, trail, first, last)
	}
}

// cT08HostileSchema is P3's probe unit: a ranged scalar, a bounded array, an
// optional and a trailing scalar, so the byte-flip sweep can forge a count, a
// present byte and a ranged value at once.
const cT08HostileSchema = `package probe
fixed table Host {
    small int32 = 5 | min = 0, max = 1000
    marks [..4]int32 | min = 0, max = 1000
    note ?int32 | min = 0, max = 1000
    tail int32 = 7
}
`

// c/P3 [verify] — "hostile bytes, sweep + sanitizer". FIXED-FORM-ALGORITHM §7
// item 5: a byte-flip fuzz over the whole record, under a sanitizer. Every byte
// of a saved record is poisoned six ways and the reader must answer ONE OF
// THREE ways and never a fourth: a named refusal, a malformed read, or a read
// that lands values. The fourth answer — a read that returns while a ranged
// field rode past its declared bound — fails by name. The probe compiles under
// -fsanitize=address,undefined so an out-of-bounds step aborts the probe and
// reds the test; the sanitizer is the claim the reader never leaves the buffer.
func cT08HostileByteSweep(t *testing.T) {
	runCGenerated(t, cT08HostileSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
static const uint8_t poison[6] = { 0x00, 0x01, 0x02, 0x7f, 0x80, 0xff };
int main(void)
{
    Host value, back;
    TableReport r;
    TableFixedEntry plan[256];
    uint8_t clean[4096], hit[4096];
    int64_t need, n;
    int at, p, i;
    memset(&value, 0, sizeof(value));
    value.small = 5;
    value.marks_count = 2;
    value.marks[0] = 10;
    value.marks[1] = 20;
    value.note_present = 1;
    value.note = 30;
    value.tail = 7;
    need = host_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(clean));
    CHECK(host_fixed_save(&value, 1, clean, (int64_t)sizeof(clean)) == need);
    for (at = 0; at < (int)host_fixed_record_bytes; ++at)
    {
        for (p = 0; p < 6; ++p)
        {
            memcpy(hit, clean, (size_t)need);
            hit[need - host_fixed_record_bytes + at] = poison[p];
            memset(&back, 0x5A, sizeof(back));
            memset(&r, 0, sizeof(r));
            n = host_fixed_load(&back, 1, hit, need, plan, 256, NULL, &r);
            if (n < 0)
            {
                CHECK(!(r.refused && r.malformed));
                if (!r.refused && !r.malformed)
                    { fprintf(stderr, "byte %d poison 0x%02x: a negative read is NEITHER a refusal NOR malformed\n", at, poison[p]); return 1; }
                continue;
            }
            CHECK(!r.refused && !r.malformed && r.reason == 0);
            if (back.small < 0 || back.small > 1000)
                { fprintf(stderr, "byte %d poison 0x%02x: success with small=%d out of bound\n", at, poison[p], (int)back.small); return 1; }
            if (back.marks_count < 0 || back.marks_count > 4)
                { fprintf(stderr, "byte %d poison 0x%02x: success with marks_count=%d out of bound\n", at, poison[p], (int)back.marks_count); return 1; }
            for (i = 0; i < back.marks_count; ++i)
                if (back.marks[i] < 0 || back.marks[i] > 1000)
                    { fprintf(stderr, "byte %d poison 0x%02x: success with marks[%d]=%d out of bound\n", at, poison[p], i, (int)back.marks[i]); return 1; }
            if (back.note_present && (back.note < 0 || back.note > 1000))
                { fprintf(stderr, "byte %d poison 0x%02x: success with note=%d out of bound\n", at, poison[p], (int)back.note); return 1; }
        }
    }
    return 0;
}
`, append([]string{"-fsanitize=address,undefined", "-fno-sanitize-recover=all", "-fno-omit-frame-pointer", "-g"}, identityCCFlags()...)...)
}

// cT08WrongPlanSchema is P4's pair: Fxnew widens `narrow`, renames `renamed`
// and appends `added`, so Fxold's identity plan run over an Fxnew record body
// lands the moved fields at the wrong offsets.
const cT08WrongPlanSchema = `package probe
fixed table Fxold {
    keep    uint32 = 7
    narrow  uint16 = 3
    renamed int32 = 5
}
fixed table Fxnew {
    keep       uint32 = 7
    narrow     uint32 = 3
    renamed_to int32 = 5
    added      int32 = 11
}
`

// c/P4 [verify] — "wrong plan goes red". FIXED-FORM-ALGORITHM §7 item 4: the
// loader refuses a file whose hash it does not lock (layout_newer, no counter),
// and Fxold's identity plan run straight over Fxnew's record body must NOT
// reproduce the record — the path select-by-hash exists to prevent. The
// legitimate value the rule admits (Fxold reading its own record) is asserted
// too, so neither half passes vacuously.
func cT08WrongPlanGoesRed(t *testing.T) {
	runCGenerated(t, cT08WrongPlanSchema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Fxnew two;
    Fxold wrong, one, back;
    TableReport r;
    TableFixedEntry plan[1024];
    uint8_t w2[4096], w1[4096];
    int64_t need;
    int32_t clamped = 0, widened = 0;
    const uint8_t * body;
    memset(&two, 0, sizeof(two));
    two.keep = 5150;
    two.narrow = 70000;
    two.renamed_to = 808;
    two.added = 909;
    need = fxnew_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(w2));
    CHECK(fxnew_fixed_save(&two, 1, w2, (int64_t)sizeof(w2)) == need);

    /* (2) the loader refuses: Fxold locks no lineage entry for Fxnew's hash. */
    memset(&back, 0x5A, sizeof(back));
    memset(&r, 0, sizeof(r));
    CHECK(fxold_fixed_load(&back, 1, w2, need, plan, 1024, NULL, &r) == -1);
    CHECK(r.refused && r.reason == SCHEMA_TABLE_LAYOUT_NEWER);
    CHECK(!r.malformed && r.unknown == 0 && r.kind_mismatch == 0 && r.clamped == 0 && r.widened == 0);

    /* (1) THE WRONG PLAN IS WRONG: Fxold's identity plan over Fxnew's body. */
    body = w2 + need - fxnew_fixed_body_bytes;
    memset(&wrong, 0x5A, sizeof(wrong));
    memset(&r, 0, sizeof(r));
    table_fixed_run(fxold_fixed_plan, fxold_fixed_plan_count, fxold_fixed_plan_guarded, body, (uint8_t *) &wrong, &clamped, &widened);
    if (wrong.renamed == 808)
        { fprintf(stderr, "NEGATIVE CONTROL FAILED: Fxold's identity plan reproduced an Fxnew record (renamed=%d)\n", (int)wrong.renamed); return 1; }

    /* the legitimate value the rule admits: Fxold reads its own record whole. */
    memset(&one, 0, sizeof(one));
    one.keep = 4242;
    one.narrow = 40000;
    one.renamed = 321;
    need = fxold_fixed_measure(1);
    CHECK(need > 0 && need <= (int64_t)sizeof(w1));
    CHECK(fxold_fixed_save(&one, 1, w1, (int64_t)sizeof(w1)) == need);
    memset(&back, 0x5A, sizeof(back));
    memset(&r, 0, sizeof(r));
    CHECK(fxold_fixed_load(&back, 1, w1, need, plan, 1024, NULL, &r) == 1);
    CHECK(back.renamed == 321 && back.narrow == 40000 && back.keep == 4242);
    return 0;
}
`, identityCCFlags()...)
}
