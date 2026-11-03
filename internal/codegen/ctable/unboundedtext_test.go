// Unbounded text elements and map keys (docs/SPEC-TABLES.md §2.13.1) and the
// populated 11/11 feature witnesses in C.
package ctable

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCTableUnboundedTextListsAndMapKeys(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Where
{
    rules []string
    first [..4]?string
    capped [..2]string
    byname map[string]int32
}

table Bounded
{
    rules []string(8)
    first [..4]?string(8)
    capped [..2]string(8)
    byname map[string(8)]int32
}

table AllFive
{
    open_list []string
    open_opt []?string
    bounded_list [..3]string
    bounded_opt [..3]?string
    by_key map[string]int32
}
`
	runCProbe(t, schema, `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "FAIL line %d: %s\n", __LINE__, #x); return 1; } } while (0)

static const char * kWhereGoldenHex =
    "01010e060c0201610162020e0a0d0205030c0178000100040e040c010163050e1c0d020c060c026b31030401000000000c060c026b320304020000000000c05351286a4f1267411d6f997fedd789ea0ce83094fde47ce86f9207409840f48f702bcb07334de7ec105b36194ac93d0600000000000000";

static int hex_nibble(char c)
{
    if (c >= '0' && c <= '9') return c - '0';
    if (c >= 'a' && c <= 'f') return c - 'a' + 10;
    if (c >= 'A' && c <= 'F') return c - 'A' + 10;
    return -1;
}

static int64_t decode_hex(const char * hex, uint8_t * out, size_t cap)
{
    size_t n = strlen(hex), i;
    if ((n & 1) != 0 || n / 2 > cap) return -1;
    for (i = 0; i < n / 2; i++) {
        int hi = hex_nibble(hex[i * 2]);
        int lo = hex_nibble(hex[i * 2 + 1]);
        if (hi < 0 || lo < 0) return -1;
        out[i] = (uint8_t)((hi << 4) | lo);
    }
    return (int64_t)(n / 2);
}

int main(void)
{
    uint8_t golden[256];
    int64_t golden_len = decode_hex(kWhereGoldenHex, golden, sizeof(golden));
    CHECK(golden_len == 118);

    /* 1. Short values on Where vs Bounded golden wire */
    {
        WhereBuilder wb;
        BoundedBuilder bb;
        Where * wroot;
        Bounded * broot;
        TableCtx ctx;
        TableReport report;
        uint8_t wwire[512], bwire[512], rwire[512];
        int64_t wn, bn, rmeas, jn, cn;
        uint8_t * region;
        const Where * loaded;
        char json[1024];
        int32_t len = -1;
        const char * s;
        int32_t * vp;
        const int32_t * cvp;

        CHECK(where_builder_init(&wb));
        wroot = where_builder_root(&wb);
        CHECK(wroot != NULL);
        CHECK(where_rules_append(&wb.main, wroot, "a", 1));
        CHECK(where_rules_append(&wb.main, wroot, "b", 1));
        CHECK(where_first_append(&wb.main, wroot, "x", 1));
        CHECK(where_first_append_absent(&wb.main, wroot));
        CHECK(where_capped_append(&wb.main, wroot, "c", 1));
        vp = where_byname_insert(&wb.main, &wroot->byname, "k2");
        CHECK(vp != NULL); *vp = 2;
        vp = where_byname_insert(&wb.main, &wroot->byname, "k1");
        CHECK(vp != NULL); *vp = 1;

        memset(&ctx, 0, sizeof(ctx)); ctx.arena = &wb.arena;
        s = where_rules_text_at(&ctx, wroot, 0, &len);
        CHECK(s != NULL && len == 1 && strcmp(s, "a") == 0);
        s = where_rules_text_at(&ctx, wroot, 1, &len);
        CHECK(s != NULL && len == 1 && strcmp(s, "b") == 0);
        s = where_first_text_at(&ctx, wroot, 0, &len);
        CHECK(s != NULL && len == 1 && strcmp(s, "x") == 0);
        s = where_first_text_at(&ctx, wroot, 1, &len);
        CHECK(s == NULL && len == 0);
        s = where_capped_text_at(&ctx, wroot, 0, &len);
        CHECK(s != NULL && len == 1 && strcmp(s, "c") == 0);
        vp = where_byname_find_mut(&wb.arena, &wroot->byname, "k1");
        CHECK(vp != NULL && *vp == 1);
        vp = where_byname_find_mut(&wb.arena, &wroot->byname, "k2");
        CHECK(vp != NULL && *vp == 2);

        wn = where_measure(&ctx, wroot);
        CHECK(wn == golden_len);
        CHECK(where_save(&ctx, wroot, wwire, sizeof(wwire)) == wn);
        CHECK(memcmp(wwire, golden, (size_t)wn) == 0);

        CHECK(bounded_builder_init(&bb));
        broot = bounded_builder_root(&bb);
        CHECK(bounded_rules_append(&bb.main, broot, "a", 1));
        CHECK(bounded_rules_append(&bb.main, broot, "b", 1));
        CHECK(bounded_first_append(broot, "x", 1));
        CHECK(bounded_first_append_absent(broot));
        CHECK(bounded_capped_append(broot, "c", 1));
        vp = bounded_byname_insert(&bb.main, &broot->byname, "k1");
        CHECK(vp != NULL); *vp = 1;
        vp = bounded_byname_insert(&bb.main, &broot->byname, "k2");
        CHECK(vp != NULL); *vp = 2;
        {
            TableCtx bctx;
            memset(&bctx, 0, sizeof(bctx)); bctx.arena = &bb.arena;
            bn = bounded_measure(&bctx, broot);
            CHECK(bn == wn);
            CHECK(bounded_save(&bctx, broot, bwire, sizeof(bwire)) == bn);
            CHECK(memcmp(bwire, wwire, (size_t)wn) == 0);
        }
        bounded_builder_shutdown(&bb);

        /* Region load + save + JSON + retain + cook */
        rmeas = where_load_measure(wwire, wn);
        CHECK(rmeas > 0);
        region = (uint8_t *)calloc(1, (size_t)rmeas + 64);
        CHECK(region != NULL);
        memset(&report, 0, sizeof(report));
        loaded = where_load(region, rmeas, wwire, wn, &report);
        CHECK(loaded != NULL && !report.malformed && report.clamped == 0);

        s = where_rules_text_at(NULL, loaded, 0, &len);
        CHECK(s != NULL && len == 1 && strcmp(s, "a") == 0);
        s = where_first_text_at(NULL, loaded, 1, &len);
        CHECK(s == NULL && len == 0);
        cvp = where_byname_find(&loaded->byname, "k2");
        CHECK(cvp != NULL && *cvp == 2);
        CHECK(where_measure(NULL, loaded) == wn);
        CHECK(where_save(NULL, loaded, rwire, sizeof(rwire)) == wn);
        CHECK(memcmp(rwire, wwire, (size_t)wn) == 0);

        jn = where_to_json_measure(loaded);
        CHECK(jn > 0 && jn < (int64_t)sizeof(json));
        CHECK(where_to_json(loaded, json, sizeof(json)) == jn);
        {
            WhereBuilder jb;
            TableCtx jctx;
            CHECK(where_builder_init(&jb));
            memset(&report, 0, sizeof(report));
            CHECK(where_from_json(&jb, json, jn, &report));
            CHECK(!report.malformed && report.clamped == 0);
            memset(&jctx, 0, sizeof(jctx)); jctx.arena = &jb.arena;
            CHECK(where_save(&jctx, where_builder_root(&jb), rwire, sizeof(rwire)) == wn);
            CHECK(memcmp(rwire, wwire, (size_t)wn) == 0);
            where_builder_shutdown(&jb);
        }

        cn = where_cook_measure(NULL, loaded);
        CHECK(cn == rmeas + 64);
        {
            uint8_t * raw_cook = (uint8_t *)calloc(1, (size_t)cn + 128);
            uint8_t * aligned_cook = (uint8_t *)(((uintptr_t)raw_cook + 63u) & ~(uintptr_t)63u);
            const Where * cooked;
            CHECK(where_cook(NULL, loaded, aligned_cook, (uint64_t)cn, TableByteOrder_Little));
            cooked = where_open(aligned_cook, (uint64_t)cn);
            CHECK(cooked != NULL);
            CHECK(where_save(NULL, cooked, rwire, sizeof(rwire)) == wn);
            CHECK(memcmp(rwire, wwire, (size_t)wn) == 0);
            free(raw_cook);
        }

        free(region);
        where_builder_shutdown(&wb);
    }

    /* 2. Long 70 KB strings across all 5 spellings + map index + retain + cook */
    {
        const int32_t kLong = 70000;
        char * long_a = (char *)malloc((size_t)kLong + 1);
        char * long_b = (char *)malloc((size_t)kLong + 1);
        char * short_prefix = (char *)malloc((size_t)kLong);
        AllFiveBuilder b;
        AllFive * root;
        TableCtx ctx;
        TableReport report;
        int32_t * vp;
        const int32_t * cvp;
        int32_t len = -1;
        const char * s;
        int64_t wn, rmeas, jn, cn;
        uint8_t * wire;
        uint8_t * region;
        char * json;
        const AllFive * loaded;

        CHECK(long_a != NULL && long_b != NULL && short_prefix != NULL);
        memset(long_a, 'a', (size_t)kLong);
        long_a[kLong - 1] = 'X';
        long_a[kLong] = 0;
        memset(long_b, 'a', (size_t)kLong);
        long_b[kLong - 1] = 'Y';
        long_b[kLong] = 0;
        memset(short_prefix, 'a', (size_t)kLong - 1);
        short_prefix[kLong - 1] = 0;

        CHECK(all_five_builder_init(&b));
        root = all_five_builder_root(&b);
        CHECK(all_five_open_list_append(&b.main, root, "", 0));
        CHECK(all_five_open_list_append(&b.main, root, long_a, kLong));
        CHECK(all_five_open_opt_append_absent(&b.main, root));
        CHECK(all_five_open_opt_append(&b.main, root, "", 0));
        CHECK(all_five_open_opt_append(&b.main, root, long_b, kLong));
        CHECK(all_five_bounded_list_append(&b.main, root, long_a, kLong));
        CHECK(all_five_bounded_list_append(&b.main, root, "", 0));
        CHECK(all_five_bounded_opt_append_absent(&b.main, root));
        CHECK(all_five_bounded_opt_append(&b.main, root, long_b, kLong));
        CHECK(all_five_bounded_opt_append(&b.main, root, "", 0));

        /* Refuse invalid UTF-8, embedded NUL, and past-bound count */
        CHECK(!all_five_bounded_opt_append(&b.main, root, "extra", 5));
        CHECK(!all_five_open_list_append(&b.main, root, "a\0b", 3));
        CHECK(!all_five_open_list_append(&b.main, root, "\xff", 1));
        CHECK(all_five_by_key_insert(&b.main, &root->by_key, "\xc3\x28") == NULL);

        vp = all_five_by_key_insert(&b.main, &root->by_key, long_b);
        CHECK(vp != NULL); *vp = 20;
        vp = all_five_by_key_insert(&b.main, &root->by_key, long_a);
        CHECK(vp != NULL); *vp = 10;
        vp = all_five_by_key_insert(&b.main, &root->by_key, "");
        CHECK(vp != NULL); *vp = 5;

        memset(&ctx, 0, sizeof(ctx)); ctx.arena = &b.arena;
        vp = all_five_by_key_find_mut(&b.arena, &root->by_key, long_a);
        CHECK(vp != NULL && *vp == 10);
        vp = all_five_by_key_find_mut(&b.arena, &root->by_key, long_b);
        CHECK(vp != NULL && *vp == 20);
        vp = all_five_by_key_find_mut(&b.arena, &root->by_key, "");
        CHECK(vp != NULL && *vp == 5);
        CHECK(all_five_by_key_find_mut(&b.arena, &root->by_key, short_prefix) == NULL);

        wn = all_five_measure(&ctx, root);
        CHECK(wn > kLong * 6);
        wire = (uint8_t *)malloc((size_t)wn);
        CHECK(wire != NULL);
        CHECK(all_five_save(&ctx, root, wire, wn) == wn);

        rmeas = all_five_load_measure(wire, wn);
        CHECK(rmeas > 0);
        region = (uint8_t *)calloc(1, (size_t)rmeas + 64);
        CHECK(region != NULL);
        memset(&report, 0, sizeof(report));
        loaded = all_five_load(region, rmeas, wire, wn, &report);
        CHECK(loaded != NULL && !report.malformed && report.clamped == 0 && report.duplicate == 0);

        s = all_five_open_list_text_at(NULL, loaded, 0, &len);
        CHECK(s != NULL && len == 0 && strcmp(s, "") == 0);
        s = all_five_open_list_text_at(NULL, loaded, 1, &len);
        CHECK(s != NULL && len == kLong && memcmp(s, long_a, (size_t)kLong) == 0 && s[kLong] == 0);
        s = all_five_open_opt_text_at(NULL, loaded, 0, &len);
        CHECK(s == NULL && len == 0);
        s = all_five_open_opt_text_at(NULL, loaded, 1, &len);
        CHECK(s != NULL && len == 0 && strcmp(s, "") == 0);
        s = all_five_open_opt_text_at(NULL, loaded, 2, &len);
        CHECK(s != NULL && len == kLong && memcmp(s, long_b, (size_t)kLong) == 0);
        s = all_five_bounded_list_text_at(NULL, loaded, 0, &len);
        CHECK(s != NULL && len == kLong && memcmp(s, long_a, (size_t)kLong) == 0);
        s = all_five_bounded_list_text_at(NULL, loaded, 1, &len);
        CHECK(s != NULL && len == 0 && strcmp(s, "") == 0);
        s = all_five_bounded_opt_text_at(NULL, loaded, 0, &len);
        CHECK(s == NULL && len == 0);
        s = all_five_bounded_opt_text_at(NULL, loaded, 1, &len);
        CHECK(s != NULL && len == kLong && memcmp(s, long_b, (size_t)kLong) == 0);
        s = all_five_bounded_opt_text_at(NULL, loaded, 2, &len);
        CHECK(s != NULL && len == 0 && strcmp(s, "") == 0);

        /* Map find + index_find on loaded region */
        cvp = all_five_by_key_find(&loaded->by_key, "");
        CHECK(cvp != NULL && *cvp == 5);
        cvp = all_five_by_key_find(&loaded->by_key, long_a);
        CHECK(cvp != NULL && *cvp == 10);
        cvp = all_five_by_key_find(&loaded->by_key, long_b);
        CHECK(cvp != NULL && *cvp == 20);
        CHECK(all_five_by_key_find(&loaded->by_key, short_prefix) == NULL);
        {
            int64_t idx_bytes = all_five_by_key_index_measure(&loaded->by_key);
            void * idx_mem = calloc(1, (size_t)idx_bytes + 16);
            void * idx_aligned = (void *)(((uintptr_t)idx_mem + 7u) & ~(uintptr_t)7u);
            TableMapIndex idx;
            idx = all_five_by_key_index(&loaded->by_key, idx_aligned, idx_bytes);
            CHECK(idx.good);
            cvp = all_five_by_key_index_find(&idx, &loaded->by_key, "");
            CHECK(cvp != NULL && *cvp == 5);
            cvp = all_five_by_key_index_find(&idx, &loaded->by_key, long_a);
            CHECK(cvp != NULL && *cvp == 10);
            cvp = all_five_by_key_index_find(&idx, &loaded->by_key, long_b);
            CHECK(cvp != NULL && *cvp == 20);
            CHECK(all_five_by_key_index_find(&idx, &loaded->by_key, short_prefix) == NULL);
            free(idx_mem);
        }

        /* JSON round-trip with 70 KB keys and elements */
        jn = all_five_to_json_measure(loaded);
        CHECK(jn > kLong * 6);
        json = (char *)malloc((size_t)jn + 1);
        CHECK(json != NULL);
        CHECK(all_five_to_json(loaded, json, jn + 1) == jn);
        {
            AllFiveBuilder jb;
            TableCtx jctx;
            uint8_t * jwire = (uint8_t *)malloc((size_t)wn);
            CHECK(all_five_builder_init(&jb));
            memset(&report, 0, sizeof(report));
            CHECK(all_five_from_json(&jb, json, jn, &report));
            CHECK(!report.malformed && report.clamped == 0);
            memset(&jctx, 0, sizeof(jctx)); jctx.arena = &jb.arena;
            CHECK(all_five_save(&jctx, all_five_builder_root(&jb), jwire, wn) == wn);
            CHECK(memcmp(jwire, wire, (size_t)wn) == 0);
            free(jwire);
            all_five_builder_shutdown(&jb);
        }

        /* Retain round-trip with 70 KB keys and elements */
        {
            uint8_t * rstore = (uint8_t *)calloc(1, 1 << 20);
            TableRetainId * rids = (TableRetainId *)calloc(1 << 14, sizeof(TableRetainId));
            TableRetain retain = { 0 };
            uint8_t * rregion = (uint8_t *)calloc(1, (size_t)rmeas + 64);
            uint8_t * rwire = (uint8_t *)malloc((size_t)wn);
            const AllFive * rroot;
            retain.bytes = rstore; retain.capacity = 1 << 20;
            retain.ids = rids; retain.id_capacity = 1 << 14;
            memset(&report, 0, sizeof(report));
            rroot = all_five_load_retain(rregion, rmeas, wire, wn, &retain, &report);
            CHECK(rroot != NULL && !report.malformed);
            CHECK(all_five_measure_retain(rroot, &retain) == wn);
            CHECK(all_five_save_retain(rroot, &retain, rwire, wn, &report) == wn);
            CHECK(memcmp(rwire, wire, (size_t)wn) == 0);
            free(rwire); free(rregion); free(rids); free(rstore);
        }

        /* Cook round-trip with 70 KB keys and elements */
        cn = all_five_cook_measure(NULL, loaded);
        CHECK(cn == rmeas + 64);
        {
            uint8_t * raw_cook = (uint8_t *)calloc(1, (size_t)cn + 128);
            uint8_t * aligned_cook = (uint8_t *)(((uintptr_t)raw_cook + 63u) & ~(uintptr_t)63u);
            uint8_t * cwire = (uint8_t *)malloc((size_t)wn);
            const AllFive * cooked;
            CHECK(all_five_cook(NULL, loaded, aligned_cook, (uint64_t)cn, TableByteOrder_Little));
            cooked = all_five_open(aligned_cook, (uint64_t)cn);
            CHECK(cooked != NULL);
            CHECK(all_five_save(NULL, cooked, cwire, wn) == wn);
            CHECK(memcmp(cwire, wire, (size_t)wn) == 0);
            free(cwire); free(raw_cook);
        }

        free(json);
        free(region);
        free(wire);
        all_five_builder_shutdown(&b);
        free(short_prefix);
        free(long_b);
        free(long_a);
    }

    return 0;
}
`)
}

func TestCTableFeatureWitnesses(t *testing.T) {
	t.Parallel()
	repoRoot, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	schemaBytes, err := os.ReadFile(filepath.Join(repoRoot, "testdata/conformance/feature-witnesses/full/Features.schema"))
	if err != nil {
		t.Fatal(err)
	}
	runCProbe(t, string(schemaBytes), `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "FAIL line %d: %s\n", __LINE__, #x); return 1; } } while (0)

static uint8_t * slurp_file(const char * path, int64_t * out_len)
{
    FILE * f = fopen(path, "rb");
    long n;
    uint8_t * buf;
    if (!f) return NULL;
    fseek(f, 0, SEEK_END);
    n = ftell(f);
    fseek(f, 0, SEEK_SET);
    buf = (uint8_t *)malloc((size_t)(n > 0 ? n : 1) + 1);
    if (!buf) { fclose(f); return NULL; }
    if (n > 0 && fread(buf, 1, (size_t)n, f) != (size_t)n) { fclose(f); free(buf); return NULL; }
    buf[n] = 0;
    fclose(f);
    *out_len = (int64_t)n;
    return buf;
}

int main(void)
{
    const char * repo = "`+filepath.ToSlash(repoRoot)+`";
    char path[1024];
    int64_t root_wire_len = 0, root_json_len = 0, root_in_len = 0;
    int64_t arr_wire_len = 0, arr_json_len = 0;
    uint8_t * root_wire;
    uint8_t * root_json;
    uint8_t * root_in;
    uint8_t * arr_wire;
    uint8_t * arr_json;

    snprintf(path, sizeof(path), "%s/testdata/conformance/feature-witnesses/full/Root.bin", repo);
    root_wire = slurp_file(path, &root_wire_len);
    CHECK(root_wire != NULL && root_wire_len == 313);

    snprintf(path, sizeof(path), "%s/testdata/conformance/tables/json/feature_full.json", repo);
    root_json = slurp_file(path, &root_json_len);
    CHECK(root_json != NULL && root_json_len > 0);

    snprintf(path, sizeof(path), "%s/testdata/conformance/feature-witnesses/full/Root.input.json", repo);
    root_in = slurp_file(path, &root_in_len);
    CHECK(root_in != NULL && root_in_len > 0);

    snprintf(path, sizeof(path), "%s/testdata/conformance/feature-witnesses/full/ArrayRoot.bin", repo);
    arr_wire = slurp_file(path, &arr_wire_len);
    CHECK(arr_wire != NULL && arr_wire_len == 35);

    snprintf(path, sizeof(path), "%s/testdata/conformance/tables/json/feature_array_root.json", repo);
    arr_json = slurp_file(path, &arr_json_len);
    CHECK(arr_json != NULL && arr_json_len > 0);

    /* 1. Root populated witness */
    {
        int64_t rmeas = root_load_measure(root_wire, root_wire_len);
        uint8_t * region = (uint8_t *)calloc(1, (size_t)(rmeas > 0 ? rmeas : 1) + 64);
        TableReport report = { 0 };
        const Root * loaded;
        uint8_t saved[512];
        char jbuf[1024];
        int64_t jn, cn;
        int32_t len = -1;
        const char * s;
        const int32_t * vp;

        if (rmeas <= 0) { fprintf(stderr, "rmeas=%lld\n", (long long)rmeas); }
        CHECK(rmeas > 0);
        loaded = root_load(region, rmeas, root_wire, root_wire_len, &report);
        if (loaded == NULL || report.malformed || report.refused || report.clamped != 0) {
            fprintf(stderr, "loaded=%p malformed=%d refused=%d reason=%d clamped=%d unknown=%d mismatch=%d widened=%d\n",
                (const void *)loaded, report.malformed, report.refused, report.reason, report.clamped, report.unknown, report.kind_mismatch, report.widened);
        }
        CHECK(loaded != NULL && !report.malformed && !report.refused && report.clamped == 0);
        CHECK(loaded->revision == 7);
        CHECK(loaded->lower == 9);
        CHECK(loaded->tags_present == 1 && loaded->tags.count == 2);
        vp = root_tags_find(&loaded->tags, "a");
        CHECK(vp != NULL && *vp == 3);
        vp = root_tags_find(&loaded->tags, "z");
        CHECK(vp != NULL && *vp == 11);
        CHECK(loaded->optional_present == 1);
        CHECK(leaf_at(NULL, &loaded->optional.child) != NULL);
        CHECK(leaf_at(NULL, &loaded->optional.child)->value == 17);
        CHECK(loaded->labels_count == 2);
        s = root_labels_text_at(loaded, 1, &len);
        CHECK(s != NULL && len == 6 && strcmp(s, "schema") == 0);
        CHECK(loaded->optional_labels.count == 3);
        CHECK(root_optional_labels_text_at(NULL, loaded, 0, &len) == NULL);
        s = root_optional_labels_text_at(NULL, loaded, 1, &len);
        CHECK(s != NULL && len == 0 && strcmp(s, "") == 0);
        s = root_optional_labels_text_at(NULL, loaded, 2, &len);
        CHECK(s != NULL && len == 6 && strcmp(s, "active") == 0);
        CHECK(loaded->words.count == 1);
        s = root_words_text_at(NULL, loaded, 0, &len);
        CHECK(s != NULL && strcmp(s, "unbounded text is populated") == 0);
        vp = root_by_name_find(&loaded->by_name, "a key longer than eight bytes");
        CHECK(vp != NULL && *vp == 23);

        CHECK(root_measure(NULL, loaded) == root_wire_len);
        CHECK(root_save(NULL, loaded, saved, sizeof(saved)) == root_wire_len);
        CHECK(memcmp(saved, root_wire, (size_t)root_wire_len) == 0);

        jn = root_to_json_measure(loaded);
        CHECK(jn == root_json_len);
        CHECK(root_to_json(loaded, jbuf, sizeof(jbuf)) == jn);
        CHECK(memcmp(jbuf, root_json, (size_t)jn) == 0);

        {
            RootBuilder jb;
            TableCtx jctx;
            CHECK(root_builder_init(&jb));
            memset(&report, 0, sizeof(report));
            CHECK(root_from_json(&jb, (const char *)root_in, root_in_len, &report));
            memset(&jctx, 0, sizeof(jctx)); jctx.arena = &jb.arena;
            CHECK(root_save(&jctx, root_builder_root(&jb), saved, sizeof(saved)) == root_wire_len);
            CHECK(memcmp(saved, root_wire, (size_t)root_wire_len) == 0);
            root_builder_shutdown(&jb);
        }

        cn = root_cook_measure(NULL, loaded);
        CHECK(cn == rmeas + 64);
        {
            uint8_t * raw_cook = (uint8_t *)calloc(1, (size_t)cn + 128);
            uint8_t * aligned_cook = (uint8_t *)(((uintptr_t)raw_cook + 63u) & ~(uintptr_t)63u);
            const Root * cooked;
            CHECK(root_cook(NULL, loaded, aligned_cook, (uint64_t)cn, TableByteOrder_Little));
            cooked = root_open(aligned_cook, (uint64_t)cn);
            CHECK(cooked != NULL);
            CHECK(root_save(NULL, cooked, saved, sizeof(saved)) == root_wire_len);
            CHECK(memcmp(saved, root_wire, (size_t)root_wire_len) == 0);
            free(raw_cook);
        }

        free(region);
    }

    /* 2. ArrayRoot populated witness */
    {
        int64_t rmeas = array_root_load_measure(arr_wire, arr_wire_len);
        uint8_t * region = (uint8_t *)calloc(1, (size_t)(rmeas > 0 ? rmeas : 1) + 64);
        TableReport report = { 0 };
        const ArrayRoot * loaded;
        uint8_t saved[128];
        char jbuf[128];
        int64_t jn;

        CHECK(rmeas > 0);
        loaded = array_root_load(region, rmeas, arr_wire, arr_wire_len, &report);
        CHECK(loaded != NULL && !report.malformed && loaded->values.count == 3);
        CHECK(*array_root_values_at(&loaded->values, 0) == 3);
        CHECK(*array_root_values_at(&loaded->values, 1) == 17);
        CHECK(*array_root_values_at(&loaded->values, 2) == -5);
        CHECK(array_root_save(NULL, loaded, saved, sizeof(saved)) == arr_wire_len);
        CHECK(memcmp(saved, arr_wire, (size_t)arr_wire_len) == 0);

        jn = array_root_to_json_measure(loaded);
        CHECK(jn == arr_json_len);
        CHECK(array_root_to_json(loaded, jbuf, sizeof(jbuf)) == jn);
        CHECK(memcmp(jbuf, arr_json, (size_t)jn) == 0);

        {
            ArrayRootBuilder jb;
            TableCtx jctx;
            CHECK(array_root_builder_init(&jb));
            memset(&report, 0, sizeof(report));
            CHECK(array_root_from_json(&jb, (const char *)arr_json, arr_json_len, &report));
            memset(&jctx, 0, sizeof(jctx)); jctx.arena = &jb.arena;
            CHECK(array_root_save(&jctx, array_root_builder_root(&jb), saved, sizeof(saved)) == arr_wire_len);
            CHECK(memcmp(saved, arr_wire, (size_t)arr_wire_len) == 0);
            array_root_builder_shutdown(&jb);
        }

        free(region);
    }

    free(arr_json);
    free(arr_wire);
    free(root_in);
    free(root_json);
    free(root_wire);
    return 0;
}
`)
}
