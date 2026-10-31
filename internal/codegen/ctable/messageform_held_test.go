package ctable

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// TestMessageFormHeldC asserts, one subtest per clause, the refusals and
// reports the MESSAGE FORM names on the generated C surface
// (docs/SPEC-TABLES.md §3.3). Each clause drives a unit whose root is a
// `fixed table` — the form is a fixed table's (§3.3) — and every assertion
// quotes the sentence it implements. The rows of
// testdata/conformance/tables/MANIFEST.txt pin the reference bytes below.
func TestMessageFormHeldC(t *testing.T) {
	t.Parallel()
	const schema = `package probe

fixed table Root {
 a int32
 b bool
 c uint8
}
`
	clauses := []struct {
		name  string
		probe string
	}{
		{"no_vocabulary", cMsgNoVocabularyProbe},
		{"second_announcement", cMsgSecondAnnouncementProbe},
		{"vocabulary_too_large", cMsgVocabularyTooLargeProbe},
		{"announcement_strict_checks", cMsgAnnouncementStrictProbe},
		{"batch_too_large", cMsgBatchTooLargeProbe},
		{"damage_inside_body", cMsgDamageInsideBodyProbe},
		{"bytes_after_pad", cMsgBytesAfterPadProbe},
		{"form_byte", cMsgFormByteProbe},
		{"skip_depth_above_128", cMsgSkipDepthProbe},
	}
	cMessageManifestRows(t)
	for _, clause := range clauses {
		t.Run(clause.name, func(t *testing.T) {
			t.Parallel()
			if clause.name == "skip_depth_above_128" {
				runCGenerated(t, schema, clause.probe, "-fsanitize=address,undefined", "-fno-omit-frame-pointer")
			} else {
				runCGenerated(t, schema, clause.probe)
			}
		})
	}
}

// cMsgShared is the vocabulary a probe opens: the unit's own announcement and
// the caller's resolved storage (docs/SPEC-TABLES.md §3.3).
const cMsgShared = `
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if(!(x)){ fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
`

// "A BODY FROM AN ENDPOINT THAT NEVER ANNOUNCED IS REFUSED BY NAME. Nothing is
// decoded, no counter moves and `malformed` does not fire." (docs/SPEC-TABLES.md §3.3).
const cMsgNoVocabularyProbe = cMsgShared + `
int main(void)
{
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    uint8_t wire[] = { 2, 0, 0 };
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages_tolerant(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 0);
    CHECK(report.refused && !report.malformed && report.reason == SCHEMA_TABLE_NO_VOCABULARY);
    return 0;
}
`

// "NO RE-ANNOUNCEMENT, EVER ... A second is refused by name and does not
// replace or amend anything." and "REFUSAL IS TERMINAL ... every announcement
// after it is refused as `second_announcement` ... every body after it is
// refused for want of one." (docs/SPEC-TABLES.md §3.3).
const cMsgSecondAnnouncementProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport first; memset(&first, 0, sizeof(first));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &first));
    CHECK(v.announced && v.count == kTableMessageEntriesHere);
    int64_t before = v.count;
    TableReport second; memset(&second, 0, sizeof(second));
    CHECK(!announce_read(&v, ann, (int64_t)sizeof(ann), &second));
    CHECK(second.refused && !second.malformed && second.reason == SCHEMA_TABLE_SECOND_ANNOUNCEMENT);
    CHECK(v.count == before);
    /* a refused FIRST announcement is terminal: every announcement after it is
       refused as second_announcement, and every body is refused for want of a
       vocabulary. */
    TableMessageEntry e2[kTableMessageEntriesHere];
    TableVocabulary w = table_vocabulary(e2, kTableMessageEntriesHere);
    uint8_t bad[] = { 9, 9, 9 };
    TableReport rejected; memset(&rejected, 0, sizeof(rejected));
    CHECK(!announce_read(&w, bad, (int64_t)sizeof(bad), &rejected));
    CHECK(rejected.refused && rejected.reason == SCHEMA_TABLE_NEWER_FORM && !w.announced);
    TableReport later; memset(&later, 0, sizeof(later));
    CHECK(!announce_read(&w, ann, (int64_t)sizeof(ann), &later));
    CHECK(later.reason == SCHEMA_TABLE_SECOND_ANNOUNCEMENT && !w.announced);
    uint8_t wire[] = { 2, 0, 0 };
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport body; memset(&body, 0, sizeof(body));
    CHECK(!root_load_messages_tolerant(storage, &count, &w, wire, (int64_t)sizeof(wire), &body));
    CHECK(count == 0 && body.reason == SCHEMA_TABLE_NO_VOCABULARY);
    return 0;
}
`

// "`vocabulary_too_large` covers both bounds, the entry count and the
// vocabulary's bytes." (docs/SPEC-TABLES.md §3.3).
const cMsgVocabularyTooLargeProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    /* the entry count past the caller's storage */
    TableMessageEntry small[1];
    TableVocabulary v = table_vocabulary(small, 1);
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!announce_read(&v, ann, (int64_t)sizeof(ann), &report));
    CHECK(report.refused && !report.malformed && report.reason == SCHEMA_TABLE_VOCABULARY_TOO_LARGE && !v.announced);
    /* the vocabulary bytes past MaxBytes */
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary b = table_vocabulary(entries, kTableMessageEntriesHere);
    b.max_bytes = 4;
    TableReport br; memset(&br, 0, sizeof(br));
    CHECK(!announce_read(&b, ann, (int64_t)sizeof(ann), &br));
    CHECK(br.refused && !br.malformed && br.reason == SCHEMA_TABLE_VOCABULARY_TOO_LARGE && !b.announced);
    return 0;
}
`

// "THE ANNOUNCEMENT READS TOLERANTLY, WITH EXACTLY TWO STRICT CHECKS: the build
// version present, exactly once, kind `9`, eight bytes wide, and the vocabulary
// present, exactly once, kind `14`, element kind `6` ... A FAILED STRICT CHECK
// IS MALFORMED, not a refusal ... The build version is kept the moment it is
// read, refusal or not." (docs/SPEC-TABLES.md §3.3).
const cMsgAnnouncementStrictProbe = cMsgShared + `
static size_t put_lebs(uint8_t * out, uint64_t v)
{
    size_t n = 0;
    for(;;) { uint8_t c = (uint8_t)(v & 0x7f); v >>= 7; if(v) c |= 0x80; out[n++] = c; if(!v) return n; }
}
static size_t put_le64(uint8_t * out, uint64_t v)
{
    int i; for(i = 0; i < 8; i++) out[i] = (uint8_t)(v >> (8 * i)); return 8;
}
static size_t version_field(uint8_t * out, uint64_t v)
{
    out[0] = 1; out[1] = 9; put_le64(out + 2, v); return 10;
}
static size_t vocab_field(uint8_t * out, uint8_t elem, const uint8_t * vocab, size_t len)
{
    uint8_t sub[64]; size_t sn = 0, n = 2;
    sub[sn++] = elem; sn += put_lebs(sub + sn, (uint64_t)len);
    if(len) { memcpy(sub + sn, vocab, len); } sn += len;
    out[0] = 2; out[1] = 14; n += put_lebs(out + n, (uint64_t)sn);
    memcpy(out + n, sub, sn); return n + sn;
}
static size_t announcement(uint8_t * out, const uint8_t * fields, size_t fn)
{
    size_t n = 0;
    out[n++] = 1; if(fn) memcpy(out + n, fields, fn); n += fn; out[n++] = 0x00;
    n += put_le64(out + n, UINT64_C(0xfffffffffffffffe));
    n += put_le64(out + n, UINT64_C(0xfffffffffffffffd));
    n += put_le64(out + n, 2);
    return n;
}
static int refusal_is_malformed(const uint8_t * data, size_t bytes)
{
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport report; memset(&report, 0, sizeof(report));
    int ok = announce_read(&v, data, (int64_t)bytes, &report);
    return !ok && report.malformed && !report.refused && !v.announced;
}
int main(void)
{
    const uint64_t version = UINT64_C(0x1122334455667788);
    uint8_t fields[256], out[512], twice[64]; size_t fn, n;
    /* build version missing */
    fn = 0; fn += vocab_field(fields + fn, 6, NULL, 0);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* build version twice */
    fn = 0; fn += version_field(fields + fn, version); fn += version_field(fields + fn, version); fn += vocab_field(fields + fn, 6, NULL, 0);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* vocabulary missing */
    fn = 0; fn += version_field(fields + fn, version);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* vocabulary not kind 14 */
    fn = 0; fn += version_field(fields + fn, version);
    fields[fn++] = 2; fields[fn++] = 13;
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* vocabulary element not kind 6 */
    fn = 0; fn += version_field(fields + fn, version); fn += vocab_field(fields + fn, 9, NULL, 0);
    CHECK(refusal_is_malformed(out, announcement(out, fields, fn)));
    /* the build version is kept the moment it is read, refusal or not */
    (void)twice; (void)n;
    {
        TableMessageEntry entries[kTableMessageEntriesHere];
        TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
        fn = 0; fn += version_field(fields + fn, version);
        fields[fn++] = 2; fields[fn++] = 13;
        size_t bytes = announcement(out, fields, fn);
        TableReport report; memset(&report, 0, sizeof(report));
        CHECK(!announce_read(&v, out, (int64_t)bytes, &report));
        CHECK(report.malformed && v.build_version == version && !v.announced);
    }
    return 0;
}
`

// "`M` ABOVE 256 ON THE WRITE SIDE IS A REFUSAL BY NAME ... `MeasureMessages`
// and `SaveMessages` both refuse and return `-1`" and "`M` ABOVE THE CALLER'S
// CAPACITY ON THE READ SIDE IS A REFUSAL BY NAME ... The reader sets the
// returned count to the wire's `M` before it returns the refusal."
// (docs/SPEC-TABLES.md §3.3).
const cMsgBatchTooLargeProbe = cMsgShared + `
int main(void)
{
    Root values[257]; int i;
    for(i = 0; i < 257; i++) root_reset(values + i);
    TableReport measure; memset(&measure, 0, sizeof(measure));
    CHECK(root_measure_messages(values, 257, &measure) == -1);
    CHECK(measure.refused && measure.reason == SCHEMA_TABLE_BATCH_TOO_LARGE);
    uint8_t buf[16];
    TableReport save; memset(&save, 0, sizeof(save));
    CHECK(root_save_messages(values, 257, buf, (int64_t)sizeof(buf), &save) == -1);
    CHECK(save.refused && save.reason == SCHEMA_TABLE_BATCH_TOO_LARGE);
    /* the read side: two bodies into storage for one */
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    uint8_t wire[] = { 2, 1, 0 }; /* count minus one is 1: M is two */
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages_tolerant(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 2 && report.refused && !report.malformed && report.reason == SCHEMA_TABLE_BATCH_TOO_LARGE);
    CHECK(storage[0].a == 0 && storage[0].b == 0 && storage[0].c == 0);
    return 0;
}
`

// "DAMAGE ANYWHERE IN THE BATCH DISCARDS THE WHOLE BATCH" (docs/SPEC-TABLES.md
// §3.3, §4): no body is delivered, the returned count is 0, every body's
// storage is at its declared defaults, and the report says malformed.
const cMsgDamageInsideBodyProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    /* M is three: body 1's reference is 0, body 2's first reference is 1,
       field a, whose 32 bits run out after four, and body 3 is never read.
       The announced entry count E is 3, so a reference is two bits, low bit
       first: bits 0-1 of byte 2 are body 1's terminator, bits 2-3 body 2's
       reference. */
    uint8_t wire[] = { 2, 2, 0x04 };
    Root storage[3]; int i;
    for(i = 0; i < 3; i++) { root_reset(storage + i); storage[i].a = 7; }
    int64_t count = 3;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages_tolerant(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 0);
    for(i = 0; i < 3; i++) CHECK(storage[i].a == 0);
    CHECK(report.malformed && !report.refused);
    return 0;
}
`

// "BYTES AFTER THE PAD ARE MALFORMED ... the batch ends at the pad to the byte
// boundary, and a buffer with bytes left over describes no batch this reader
// can name." (docs/SPEC-TABLES.md §3.3).
const cMsgBytesAfterPadProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    uint8_t wire[] = { 2, 0, 0, 0 }; /* a valid one-body batch, one byte over */
    Root storage[1]; root_reset(storage); storage[0].a = 7;
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages_tolerant(storage, &count, &v, wire, (int64_t)sizeof(wire), &report));
    CHECK(count == 0 && storage[0].a == 0 && report.malformed && !report.refused);
    return 0;
}
`

// "The FORM BYTE is a whole byte and is read FIRST ... a form a reader does not
// know is a REFUSAL by name and never damage." and "a reader handed a form-`2`
// wire where a file was expected refuses by name."
// (docs/SPEC-TABLES.md §3.3).
const cMsgFormByteProbe = cMsgShared + `
int main(void)
{
    uint8_t ann[kTableAnnounceBytes];
    CHECK(announce(ann, (int64_t)sizeof(ann)) == kTableAnnounceBytes);
    TableMessageEntry entries[kTableMessageEntriesHere];
    TableVocabulary v = table_vocabulary(entries, kTableMessageEntriesHere);
    TableReport ar; memset(&ar, 0, sizeof(ar));
    CHECK(announce_read(&v, ann, (int64_t)sizeof(ann), &ar));
    uint8_t form1[] = { 1 };
    Root storage[1]; root_reset(storage);
    int64_t count = 1;
    TableReport report; memset(&report, 0, sizeof(report));
    CHECK(!root_load_messages_tolerant(storage, &count, &v, form1, (int64_t)sizeof(form1), &report));
    CHECK(count == 0 && report.refused && !report.malformed && report.reason == SCHEMA_TABLE_NEWER_FORM);
    /* a form-2 batch handed to the FILE reader -- the announcement's form-1
       reader -- refuses by name and moves no counter. */
    TableMessageEntry e2[kTableMessageEntriesHere];
    TableVocabulary w = table_vocabulary(e2, kTableMessageEntriesHere);
    uint8_t form2[] = { 2, 0, 0 };
    TableReport r2; memset(&r2, 0, sizeof(r2));
    CHECK(!announce_read(&w, form2, (int64_t)sizeof(form2), &r2));
    CHECK(r2.refused && !r2.malformed && r2.reason == SCHEMA_TABLE_MESSAGE_FORM_AS_FILE);
    return 0;
}
`

// cMsgSkipDepthProbe is the message_skip_depth boundary (docs/SPEC-TABLES.md,
// limit value 128). A nested body, a union arm and a walked array element are
// accepted at 128 and at 127, and a complete zero-padded batch whose only fault
// is depth 129 is malformed. Verdicts are the spec's, not this emitter's.
// Truncation drops the root terminator of the at-limit body; 16 + 256 bits is
// a whole number of bytes, so a pad bit cannot stand in for that terminator.
const cMsgSkipDepthProbe = cMsgShared + `
static void fill_entry(TableMessageEntry * e, uint8_t kind)
{
    memset(e, 0, sizeof(*e));
    e->value_bits = e->elem_value_bits = -1;
    e->id = 1;
    e->kind = kind;
}
static TableVocabulary vocab_of(TableMessageEntry * entries, int64_t count, int64_t bits)
{
    TableVocabulary v = table_vocabulary(entries, count);
    v.count = count;
    v.ref_bits = bits;
    v.announced = 1;
    return v;
}
static int64_t pack_refs(uint8_t * buf, int cap, int width, int nlead, uint64_t lead, int nmid, uint64_t mid, int ntail)
{
    int at = 16, i, b, n;
    if (cap < 2) return -1;
    memset(buf, 0, (size_t)cap);
    buf[0] = 2;
    buf[1] = 0;
    n = nlead + nmid + ntail;
    for (i = 0; i < n; i++) {
        uint64_t ref = i < nlead ? lead : (i < nlead + nmid ? mid : 0);
        for (b = 0; b < width; b++) {
            if ((at >> 3) >= cap) return -1;
            if ((ref >> b) & 1) buf[at >> 3] |= (uint8_t)(1u << (at & 7));
            at++;
        }
    }
    return (int64_t)(at + 7) / 8;
}
static int load_one(const TableVocabulary * v, const uint8_t * buf, int64_t n, Root * storage, int64_t * count, TableReport * report)
{
    root_reset(storage);
    *count = 1;
    memset(report, 0, sizeof(*report));
    return root_load_messages_tolerant(storage, count, v, buf, n, report);
}
static int expect_ok(const char * row, const TableVocabulary * v, const uint8_t * buf, int64_t n)
{
    Root storage; int64_t count; TableReport report; int ok;
    ok = load_one(v, buf, n, &storage, &count, &report);
    if (!(ok && !report.malformed && report.unknown == 1 && storage.a == 0 && count == 1)) {
        fprintf(stderr, "%s: ok=%d malformed=%d unknown=%d a=%d count=%lld\n",
            row, ok, report.malformed, (int)report.unknown, (int)storage.a, (long long)count);
        return 1;
    }
    return 0;
}
static int expect_bad(const char * row, const TableVocabulary * v, const uint8_t * buf, int64_t n)
{
    Root storage; int64_t count; TableReport report; int ok;
    ok = load_one(v, buf, n, &storage, &count, &report);
    if (!(!ok && report.malformed && count == 0)) {
        fprintf(stderr, "%s: ok=%d malformed=%d count=%lld\n", row, ok, report.malformed, (long long)count);
        return 1;
    }
    return 0;
}
int main(void)
{
    uint8_t buf[128];
    int64_t n;
    TableMessageEntry body, arm, arr[2], keyed6[2], keyed32[2], keyed13[2];
    TableVocabulary body_v, arm_v, arr_v, keyed6_v, keyed32_v, keyed13_v;
    fill_entry(&body, 13);
    body_v = vocab_of(&body, 1, 1);
    fill_entry(&arm, 15);
    arm_v = vocab_of(&arm, 1, 1);
    fill_entry(&arr[0], 13);
    fill_entry(&arr[1], 14);
    arr[1].id = 2;
    arr[1].min = 1;
    arr[1].max = 1;
    arr[1].elem_kind = 13;
    arr_v = vocab_of(arr, 2, 2);
    fill_entry(&keyed6[0], 13);
    fill_entry(&keyed6[1], 16);
    keyed6[1].id = 2;
    keyed6[1].max = 3;
    keyed6[1].elem_kind = 6;
    keyed6[1].elem_value_bits = 8;
    keyed6_v = vocab_of(keyed6, 2, 2);
    fill_entry(&keyed32[0], 13);
    fill_entry(&keyed32[1], 16);
    keyed32[1].id = 2;
    keyed32[1].max = 3;
    keyed32[1].elem_kind = 32;
    keyed32_v = vocab_of(keyed32, 2, 2);
    fill_entry(&keyed13[0], 13);
    fill_entry(&keyed13[1], 16);
    keyed13[1].id = 2;
    keyed13[1].max = 3;
    keyed13[1].elem_kind = 13;
    keyed13_v = vocab_of(keyed13, 2, 2);
    n = pack_refs(buf, 128, 1, 127, 1, 0, 0, 128);
    if (n < 0 || expect_ok("skip_depth_body_below_128", &body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 128, 1, 0, 0, 129);
    if (n < 0 || expect_ok("skip_depth_body_at_128", &body_v, buf, n)) return 1;
    if (expect_ok("skip_depth_continuation", &body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 128, 1, 0, 0, 128);
    if (n < 0 || n * 8 != 16 + 256) { fprintf(stderr, "skip_depth_truncation alignment\n"); return 1; }
    if (expect_bad("skip_depth_truncation", &body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 129, 1, 0, 0, 130);
    if (n < 0 || expect_bad("skip_depth_above_128 body", &body_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 128, 1, 0, 0, 2);
    if (n < 0 || expect_ok("skip_depth_union_below_128", &arm_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 129, 1, 0, 0, 2);
    if (n < 0 || expect_ok("skip_depth_union_at_128", &arm_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 1, 130, 1, 0, 0, 2);
    if (n < 0 || expect_bad("skip_depth_above_128 union", &arm_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 125, 1, 1, 2, 127);
    if (n < 0 || expect_ok("skip_depth_array_below_128", &arr_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 126, 1, 1, 2, 128);
    if (n < 0 || expect_ok("skip_depth_array_at_128", &arr_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 127, 1, 1, 2, 129);
    if (n < 0 || expect_bad("skip_depth_above_128 array", &arr_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 128, 1, 2, 2, 139);
    if (n < 0 || expect_ok("skip_depth_keyed_fixed_at_128", &keyed6_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 128, 1, 2, 2, 131);
    if (n < 0 || expect_ok("skip_depth_keyed_nopayload_at_128", &keyed32_v, buf, n)) return 1;
    n = pack_refs(buf, 128, 2, 127, 1, 2, 2, 132);
    if (n < 0 || expect_bad("skip_depth_above_128 keyed", &keyed13_v, buf, n)) return 1;
    return 0;
}
`

// cMessageManifestRows holds the announcement and batch bytes of §3.3 against
// every connection and message row the conformance manifest names.
func cMessageManifestRows(t *testing.T) {
	t.Helper()
	root := filepath.Join("..", "..", "..")
	data, err := os.ReadFile(filepath.Join(root, "testdata/conformance/tables/MANIFEST.txt"))
	if err != nil {
		t.Fatal(err)
	}
	units, connections := map[string]string{}, map[string][]string{}
	var rows [][]string
	// ONE COMPILE PER UNIT, shared by every row of that unit: the 18 rows name
	// five units, and each row used to regenerate and recompile its unit's
	// whole C surface beside a three-line main. The unit's objects are built
	// once, on first use, and each row compiles only its own main against them.
	builds := newCMessageUnitBuilds(t)
	for line := range strings.SplitSeq(string(data), "\n") {
		f := strings.Fields(line)
		if len(f) == 0 || strings.HasPrefix(f[0], "#") {
			continue
		}
		switch f[0] {
		case "unit":
			units[f[1]] = f[2]
		case "connection":
			connections[f[1]] = f
			rows = append(rows, f)
		case "message":
			rows = append(rows, f)
		}
	}
	for _, row := range rows {
		t.Run(row[0]+"/"+row[1], func(t *testing.T) {
			t.Parallel()
			connection := row
			if row[0] == "message" {
				connection = connections[row[2]]
			}
			path := filepath.Join(root, units[connection[2]])
			paths := []string{path}
			if !strings.HasSuffix(path, ".schema") {
				var err error
				paths, err = filepath.Glob(filepath.Join(path, "*.schema"))
				if err != nil {
					t.Fatal(err)
				}
			}
			var sources []check.SourceFile
			for _, path := range paths {
				data, err := os.ReadFile(path)
				if err != nil {
					t.Fatal(err)
				}
				ast, diags := parser.Parse(path, data)
				if len(diags) > 0 {
					t.Fatal(diags)
				}
				name := filepath.Base(path)
				sources = append(sources, check.SourceFile{Path: path, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: data, AST: ast})
			}
			unit, diags := check.Unit(sources)
			if len(diags) > 0 {
				t.Fatal(diags)
			}
			expected, err := os.ReadFile(filepath.Join(root, connection[4]))
			if err != nil {
				t.Fatal(err)
			}
			source := "#include <stdint.h>\n#include <stdlib.h>\n#include <string.h>\n"
			files, err := Generate(unit)
			if err != nil {
				t.Fatal(err)
			}
			for name := range files {
				if strings.HasSuffix(name, "Table.h") {
					source += fmt.Sprintf("#include %q\n", name)
				}
			}
			source += fmt.Sprintf("static const uint8_t expected[] = {%s};\n", cMessageBytes(expected))
			if row[0] == "connection" {
				// "The announcement is a compile-time constant byte array with a length." (§3.3)
				source += "int main(void){uint8_t out[sizeof(expected)];return announce(out,sizeof(out))!=sizeof(expected)||memcmp(out,expected,sizeof(out));}\n"
			} else if row[5] == "-" {
				// a NAMED REFUSAL row: its root must be a variable table, which
				// carries no message form (docs/SPEC-TABLES.md §3.3); that no
				// backend emits one is compiler/messageformlock_test.go's
				if st := unit.Tables[row[3]]; st == nil || st.FixedDeclared {
					t.Fatalf("message row %s is a refusal row over %s, which is not a variable table", row[1], row[3])
				}
				return
			} else {
				wire, err := os.ReadFile(filepath.Join(root, row[5]))
				if err != nil {
					t.Fatal(err)
				}
				source += fmt.Sprintf("static const uint8_t wire[] = {%s};\n", cMessageBytes(wire))
				fn := ""
				for i, r := range row[3] {
					if r >= 'A' && r <= 'Z' {
						if i > 0 {
							fn += "_"
						}
						fn += strings.ToLower(string(r))
					} else {
						fn += string(r)
					}
				}
				source += "int main(void){TableMessageEntry entries[kTableMessageEntriesHere];TableVocabulary v=table_vocabulary(entries,kTableMessageEntriesHere);TableReport report={0};int64_t count=1;uint8_t out[sizeof(wire)];if(!announce_read(&v,expected,sizeof(expected),&report))return 1;"
				// Only a fixed table rides the message form (§3.3): a row whose
				// root is a variable table names a codec C does not emit.
				if st := unit.Tables[row[3]]; st == nil || !st.FixedDeclared {
					t.Fatalf("message row %s: %s is not a fixed table, and the message form is fixed tables only", row[1], row[3])
				}
				source += fmt.Sprintf("%s values[1];if(!%s_load_messages_tolerant(values,&count,&v,wire,sizeof(wire),&report))return 4;", row[3], fn)
				// "A batch of bodies is the unit this form reads and writes." (§3.3)
				source += fmt.Sprintf("if(count!=1||%s_measure_messages(values,count,&report)!=sizeof(wire)||%s_save_messages(values,count,out,sizeof(out),&report)!=sizeof(wire))return 5;return memcmp(out,wire,sizeof(wire));}\n", fn, fn)
			}
			runCMessageUnit(t, builds.of(t, connection[2], unit), source)
		})
	}
}

// cMessageBytes embeds the reference bytes without changing their pins (§3.3).
func cMessageBytes(data []byte) string {
	parts := make([]string, len(data))
	for i, b := range data {
		parts[i] = fmt.Sprint(b)
	}
	return strings.Join(parts, ",")
}

// cMessageUnitBuild is one unit's generated C surface, written and compiled
// to objects once and shared by every manifest row of that unit.
type cMessageUnitBuild struct {
	once sync.Once
	dir  string
	objs []string
	err  error
}

// cMessageUnitBuilds holds the shared builds for one run of the manifest
// rows, under a directory the parent test removes when every row is done.
type cMessageUnitBuilds struct {
	root string
	mu   sync.Mutex
	by   map[string]*cMessageUnitBuild
}

func newCMessageUnitBuilds(t *testing.T) *cMessageUnitBuilds {
	t.Helper()
	// the parent's TempDir outlives its parallel subtests: they run before
	// the parent's cleanups do
	return &cMessageUnitBuilds{root: t.TempDir(), by: map[string]*cMessageUnitBuild{}}
}

// of returns the unit's shared build, compiling it on first use.
func (b *cMessageUnitBuilds) of(t *testing.T, name string, u *ir.Unit) *cMessageUnitBuild {
	t.Helper()
	b.mu.Lock()
	build, ok := b.by[name]
	if !ok {
		build = &cMessageUnitBuild{dir: filepath.Join(b.root, name)}
		b.by[name] = build
	}
	b.mu.Unlock()
	build.once.Do(func() { build.err = build.compile(u) })
	if build.err != nil {
		t.Fatal(build.err)
	}
	return build
}

func (b *cMessageUnitBuild) compile(u *ir.Unit) error {
	cc, err := exec.LookPath("cc")
	if err != nil {
		return fmt.Errorf("C message conformance requires cc")
	}
	files, err := cgen.Generate(u)
	if err != nil {
		return err
	}
	tables, err := Generate(u)
	if err != nil {
		return err
	}
	maps.Copy(files, tables)
	if err := os.MkdirAll(b.dir, 0o700); err != nil {
		return err
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(b.dir, name), data, 0600); err != nil {
			return err
		}
	}
	for name := range files {
		if !strings.HasSuffix(name, ".c") {
			continue
		}
		obj := filepath.Join(b.dir, strings.TrimSuffix(name, ".c")+".o")
		args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O2", "-I", b.dir, "-c", filepath.Join(b.dir, name), "-o", obj}
		if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
			return fmt.Errorf("compile %s: %w\n%s", name, err, out)
		}
		b.objs = append(b.objs, obj)
	}
	return nil
}

// runCMessageUnit executes the generated C announcement and batch surface
// (§3.3): the row's own main, compiled against its unit's shared objects.
func runCMessageUnit(t *testing.T, build *cMessageUnitBuild, source string) {
	t.Helper()
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Fatal("C message conformance requires cc")
	}
	dir := t.TempDir()
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(source), 0600); err != nil {
		t.Fatal(err)
	}
	binary := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-O2", "-I", build.dir, main}
	args = append(args, build.objs...)
	args = append(args, "-lm", "-o", binary)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	if out, err := exec.Command(binary).CombinedOutput(); err != nil {
		t.Fatalf("§3.3 reference bytes differ: %v\n%s", err, out)
	}
}
