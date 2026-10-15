/* cards_test.c — the plain C test program for the C card leg. One check per
   named check, exit status nonzero on any failure. It runs from the
   repository root. No real time, sockets, network or outside services. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cards.h"
#include "CardsView.h"
#include "sha256.h"

static int failures = 0;

static void check(int ok, const char *check_name, const char *what) {
    if (!ok) {
        printf("FAIL %s: %s\n", check_name, what);
        failures++;
    }
}

static uint8_t *read_file(const char *path, int64_t *len) {
    FILE *f = fopen(path, "rb");
    uint8_t *buf;
    long n;
    if (f == NULL) return NULL;
    fseek(f, 0, SEEK_END);
    n = ftell(f);
    fseek(f, 0, SEEK_SET);
    buf = (uint8_t *)malloc((size_t)(n > 0 ? n : 1));
    if (buf == NULL) {
        fclose(f);
        return NULL;
    }
    if (n > 0 && fread(buf, 1, (size_t)n, f) != (size_t)n) {
        free(buf);
        fclose(f);
        return NULL;
    }
    fclose(f);
    *len = n;
    return buf;
}

static int fixed_eq(const char *buf, int32_t len, const char *want) {
    size_t wn = strlen(want);
    if (len < 0 || (size_t)len != wn) return 0;
    return memcmp(buf, want, wn) == 0;
}

/* TestCCardRoundTripsThePopulatedFixture pins the populated fixture: Load
   compares every field with card-populated.json, the body byte for byte, and
   ToJSON and FromJSON reproduce both files exactly. It also mutates the
   caller's input and a Wire result and shows the record unchanged
   (docs/SPEC-TABLES.md §6.2). */
static void test_round_trip(void) {
    const char *name = "TestCCardRoundTripsThePopulatedFixture";
    int64_t wire_len = 0, json_len = 0;
    uint8_t *wire = read_file("tables/cards/fuzz/card-populated.wire", &wire_len);
    uint8_t *json = read_file("tables/cards/fuzz/card-populated.json", &json_len);
    uint8_t *input_copy;
    CardsRecord rec;
    TableReport report;
    char err[256];
    const Card *root;
    uint8_t *out = NULL;
    int64_t out_len = 0;
    err[0] = '\0';
    check(wire != NULL && json != NULL, name, "fixture files read");
    if (wire == NULL || json == NULL) {
        free(wire);
        free(json);
        return;
    }
    input_copy = (uint8_t *)malloc((size_t)wire_len);
    memcpy(input_copy, wire, (size_t)wire_len);
    check(cards_load(input_copy, wire_len, &rec, &report, err, sizeof(err)), name,
          err[0] ? err : "load");
    root = rec.root;
    check(root != NULL, name, "root present");
    if (root != NULL) {
        TableStringView body, done, source;
        TableSequenceCursor cur;
        const PathText *pe;
        const CardID *ne;
        int idx;
        static const char *const want_paths[2] = {"path/one", "path/two"};
        static const char *const want_needs[2] = {"need-one", "need-two"};
        check(fixed_eq(root->id, root->id_length, "populated"), name, "id field");
        check(fixed_eq(root->sha, root->sha_length,
                       "0123456789012345678901234567890123456789"),
              name, "sha field");
        check(root->tier == TIER_FLASH, name, "tier field");
        check(fixed_eq(root->repo, root->repo_length, "repo"), name, "repo field");
        check(fixed_eq(root->_base, root->_base_length, "base"), name, "base field");
        check(fixed_eq(root->kind, root->kind_length, "kind"), name, "kind field");
        check(root->priority == PRIORITY_CRITICAL, name, "priority field");
        check(fixed_eq(root->stream, root->stream_length, "stream"), name,
              "stream field");
        idx = 0;
        cur = card_paths_each(NULL, &root->paths);
        while ((pe = (const PathText *)table_sequence_next(&cur)) != NULL) {
            check(idx < 2 && fixed_eq(pe->s, pe->s_length, want_paths[idx]), name,
                  "paths element");
            idx++;
        }
        check(idx == 2, name, "paths count");
        idx = 0;
        cur = card_paths_each(NULL, &root->_new);
        while ((pe = (const PathText *)table_sequence_next(&cur)) != NULL) {
            check(idx < 1 && fixed_eq(pe->s, pe->s_length, "new/path"), name,
                  "new element");
            idx++;
        }
        check(idx == 1, name, "new count");
        idx = 0;
        cur = card_paths_each(NULL, &root->shared);
        while ((pe = (const PathText *)table_sequence_next(&cur)) != NULL) {
            check(idx < 1 && fixed_eq(pe->s, pe->s_length, "shared/path"), name,
                  "shared element");
            idx++;
        }
        check(idx == 1, name, "shared count");
        idx = 0;
        cur = card_needs_each(NULL, &root->needs);
        while ((ne = (const CardID *)table_sequence_next(&cur)) != NULL) {
            check(idx < 2 && fixed_eq(ne->s, ne->s_length, want_needs[idx]), name,
                  "needs element");
            idx++;
        }
        check(idx == 2, name, "needs count");
        check(fixed_eq(root->test, root->test_length, "fixture test"), name,
              "test field");
        done = table_string_at(NULL, &root->done);
        check(done.length == 9 && memcmp(done.data, "done when", 9) == 0, name,
              "done field");
        check(root->deadline_minutes == 45, name, "deadline field");
        source = table_string_at(NULL, &root->source);
        check(source.length > 0, name, "source field");
        check(root->epoch == 7, name, "epoch field");
        check(root->column == COLUMN_REVIEW, name, "column field");
        check(fixed_eq(root->holder, root->holder_length, "holder"), name,
              "holder field");
        check(root->attempt == 1, name, "attempt field");
        check(fixed_eq(root->head, root->head_length,
                       "0123456789012345678901234567890123456789"),
              name, "head field");
        check(fixed_eq(root->branch, root->branch_length, "branch/name"), name,
              "branch field");
        body = table_string_at(NULL, &root->body);
        check(body.length > 0, name, "body present");
        /* The body is compared byte for byte with the JSON fixture's body by
           the ToJSON comparison below. */
        check(root->extra.count == 0, name, "extra empty");
        check(root->consumers.count == 0, name, "consumers empty");
    }
    /* ToJSON reproduces the JSON file exactly. */
    check(cards_to_json(&rec, &out, &out_len, err, sizeof(err)), name,
          err[0] ? err : "to_json");
    check(out != NULL && out_len == json_len &&
              memcmp(out, json, (size_t)json_len) == 0,
          name, "ToJSON reproduces card-populated.json exactly");
    cards_free_buf(out);
    out = NULL;
    /* FromJSON reproduces the wire file exactly. */
    {
        CardsRecord rec2;
        check(cards_from_json(json, json_len, &rec2, err, sizeof(err)), name,
              err[0] ? err : "from_json");
        check(rec2.wire_len == wire_len &&
                  memcmp(rec2.wire, wire, (size_t)wire_len) == 0,
              name, "FromJSON reproduces card-populated.wire exactly");
        cards_record_clear(&rec2);
    }
    /* Mutating the caller's input leaves the record unchanged. */
    memset(input_copy, 0xFF, (size_t)wire_len);
    check(rec.wire_len == wire_len && memcmp(rec.wire, wire, (size_t)wire_len) == 0,
          name, "record unchanged after input mutation");
    /* Mutating a Wire result leaves the record unchanged. */
    check(cards_wire(&rec, &out, &out_len, err, sizeof(err)), name, "wire copy");
    if (out != NULL && out_len > 0) out[0] ^= 0xFF;
    check(rec.wire_len == wire_len && memcmp(rec.wire, wire, (size_t)wire_len) == 0,
          name, "record unchanged after Wire result mutation");
    cards_free_buf(out);
    cards_record_clear(&rec);
    free(input_copy);
    free(wire);
    free(json);
    if (failures == 0) printf("PASS %s\n", name);
}


/* Builds {"id":"x",<field>:[<elem>,...]} with count copies of elem. */
static uint8_t *build_list_json(const char *field, const char *elem, int count,
                                int64_t *out_len) {
    size_t cap = 128 + (strlen(elem) + 1) * (size_t)(count + 1);
    uint8_t *buf = (uint8_t *)malloc(cap);
    int n = snprintf((char *)buf, cap, "{\"id\":\"x\",\"%s\":[", field);
    int i;
    for (i = 0; i < count; i++) {
        n += snprintf((char *)buf + n, cap - (size_t)n, "%s%s", i ? "," : "", elem);
    }
    n += snprintf((char *)buf + n, cap - (size_t)n, "]}");
    *out_len = n;
    return buf;
}

static void expect_bound(const char *name, const char *field, const char *elem,
                         const char *want_field) {
    int64_t len = 0;
    uint8_t *json;
    uint8_t *before;
    CardsRecord rec;
    char err[256];
    err[0] = '\0';
    json = build_list_json(field, elem, 512, &len);
    before = (uint8_t *)malloc((size_t)len);
    memcpy(before, json, (size_t)len);
    check(cards_from_json(json, len, &rec, err, sizeof(err)), name,
          err[0] ? err : "512 entries accepted");
    check(memcmp(before, json, (size_t)len) == 0, name, "input unchanged on accept");
    cards_record_clear(&rec);
    free(json);
    free(before);
    err[0] = '\0';
    json = build_list_json(field, elem, 513, &len);
    before = (uint8_t *)malloc((size_t)len);
    memcpy(before, json, (size_t)len);
    check(!cards_from_json(json, len, &rec, err, sizeof(err)), name,
          "513 entries refused");
    check(strstr(err, want_field) != NULL && strstr(err, "512") != NULL, name,
          err[0] ? err : "refusal names the field and 512");
    check(memcmp(before, json, (size_t)len) == 0, name,
          "refused write leaves its input unchanged");
    free(json);
    free(before);
}

/* TestCCardBoundsRefuseBeforePublishing pins the customer ceilings:
   65536 body bytes accepted, 65537 refused naming body and 65536; 512 entries
   accepted and 513 refused naming the field, for each collection
   (docs/SPEC-TABLES.md §2.5, §2.9). */
static void test_bounds(void) {
    const char *name = "TestCCardBoundsRefuseBeforePublishing";
    int before_failures = failures;
    /* Body at and over the ceiling. */
    {
        size_t body_n = 65536;
        size_t cap = body_n + 128;
        uint8_t *json = (uint8_t *)malloc(cap);
        int n = snprintf((char *)json, cap, "{\"id\":\"x\",\"body\":\"");
        CardsRecord rec;
        char err[256];
        err[0] = '\0';
        memset(json + n, 'y', body_n);
        n += (int)body_n;
        n += snprintf((char *)json + n, cap - (size_t)n, "\"}");
        check(cards_from_json(json, n, &rec, err, sizeof(err)), name,
              err[0] ? err : "65536 body bytes accepted");
        cards_record_clear(&rec);
        free(json);
    }
    {
        size_t body_n = 65537;
        size_t cap = body_n + 128;
        uint8_t *json = (uint8_t *)malloc(cap);
        uint8_t *copy;
        int n = snprintf((char *)json, cap, "{\"id\":\"x\",\"body\":\"");
        CardsRecord rec;
        char err[256];
        err[0] = '\0';
        memset(json + n, 'y', body_n);
        n += (int)body_n;
        n += snprintf((char *)json + n, cap - (size_t)n, "\"}");
        copy = (uint8_t *)malloc((size_t)n);
        memcpy(copy, json, (size_t)n);
        check(!cards_from_json(json, n, &rec, err, sizeof(err)), name,
              "65537 body bytes refused");
        check(strstr(err, "body") != NULL && strstr(err, "65536") != NULL, name,
              err[0] ? err : "refusal names body and 65536");
        check(memcmp(copy, json, (size_t)n) == 0, name,
              "refused body write leaves its input unchanged");
        free(copy);
        free(json);
    }
    expect_bound(name, "paths", "{\"s\":\"p\"}", "paths");
    expect_bound(name, "_new", "{\"s\":\"n\"}", "_new");
    expect_bound(name, "shared", "{\"s\":\"s\"}", "shared");
    expect_bound(name, "needs", "{\"s\":\"d\"}", "needs");
    expect_bound(name, "extra", "{\"key\":\"K\",\"value\":\"v\"}", "extra");
    expect_bound(name, "consumers", "{\"kind\":\"k\"}", "consumers");
    if (failures == before_failures) printf("PASS %s\n", name);
}


/* TestCParseRenderMatchesGo pins parse once, render many: every parse case in
   the shared corpus parses to its expected wire, every render case renders to
   its expected text byte for byte, and the rejected case refuses with the
   stranded key (DESIGN L2). */
static void test_parse_render(void) {
    const char *name = "TestCParseRenderMatchesGo";
    int before_failures = failures;
    static const char *const render_cases[4] = {"card-populated", "initial-populated",
                                                "consumers-populated",
                                                "empty-consumers"};
    int ci;
    for (ci = 0; ci < 4; ci++) {
        char wire_path[256], want_path[256];
        int64_t wire_len = 0, want_len = 0;
        uint8_t *wire, *want;
        CardsRecord rec;
        TableReport report;
        char err[256];
        uint8_t *out = NULL;
        int64_t out_len = 0;
        err[0] = '\0';
        snprintf(wire_path, sizeof(wire_path), "tables/cards/conformance/cases/%s.wire",
                 render_cases[ci]);
        snprintf(want_path, sizeof(want_path), "tables/cards/conformance/expected/%s.txt",
                 render_cases[ci]);
        wire = read_file(wire_path, &wire_len);
        want = read_file(want_path, &want_len);
        check(wire != NULL && want != NULL, name, "render case files read");
        if (wire == NULL || want == NULL) { free(wire); free(want); continue; }
        check(cards_load(wire, wire_len, &rec, &report, err, sizeof(err)), name,
              err[0] ? err : "load render case");
        check(cards_render(&rec, &out, &out_len, err, sizeof(err)), name,
              err[0] ? err : "render case");
        check(out != NULL && out_len == want_len &&
                  memcmp(out, want, (size_t)want_len) == 0,
              name, "render matches expected text byte for byte");
        cards_free_buf(out);
        cards_record_clear(&rec);
        free(wire);
        free(want);
    }
    /* The accepted parse case parses to the expected wire and renders back. */
    {
        int64_t text_len = 0, want_len = 0;
        uint8_t *text = read_file("tables/cards/conformance/cases/normalized-valid.md", &text_len);
        uint8_t *want = read_file("tables/cards/conformance/expected/normalized-valid.wire", &want_len);
        CardsRecord rec;
        char err[256];
        uint8_t *out = NULL;
        int64_t out_len = 0;
        err[0] = '\0';
        check(text != NULL && want != NULL, name, "parse case files read");
        if (text != NULL && want != NULL) {
            check(cards_parse(text, text_len, &rec, err, sizeof(err)), name,
                  err[0] ? err : "parse normalized-valid");
            check(rec.wire_len == want_len && memcmp(rec.wire, want, (size_t)want_len) == 0,
                  name, "parse matches expected wire byte for byte");
            check(cards_render(&rec, &out, &out_len, err, sizeof(err)), name,
                  err[0] ? err : "render parsed card");
            check(out != NULL && out_len == text_len &&
                      memcmp(out, text, (size_t)text_len) == 0,
                  name, "render of parsed card reproduces the brief byte for byte");
            cards_free_buf(out);
            cards_record_clear(&rec);
        }
        free(text);
        free(want);
    }
    /* The rejected case refuses with the stranded key. */
    {
        int64_t text_len = 0;
        uint8_t *text = read_file("tables/cards/conformance/cases/rejected-legacy.md", &text_len);
        CardsRecord rec;
        char err[256];
        err[0] = '\0';
        check(text != NULL, name, "rejected case read");
        if (text != NULL) {
            check(!cards_parse(text, text_len, &rec, err, sizeof(err)), name,
                  "rejected-legacy refused");
            check(strstr(err, "stranded") != NULL && strstr(err, "PRIORITY") != NULL,
                  name, err[0] ? err : "refusal names the stranded key");
        }
        free(text);
    }
    if (failures == before_failures) printf("PASS %s\n", name);
}

/* ---- L3/L4 checks ---- */

static int hex_eq(const uint8_t *hex, const uint8_t *want) {
    return memcmp(hex, want, 64) == 0;
}

/* TestCUpgradeMatchesGo pins the retained upgrade against the shared corpus:
   every upgrade case yields the expected wire and report byte for byte or the
   expected refusal; upgrading an already-upgraded record is byte-identical;
   one retained id short refuses with retain-lost and leaves the input alone
   (DESIGN L3, tla/CardVersioning.tla NoFieldLost, BodyUnchanged,
   RefusalKeepsOriginal). */
/* report_text is the conformance report spelling (FORMAT.md "The read
   report"), written here so the check does not trust a facade formatter. */
static int report_text(const TableReport *r, char *buf, size_t cap) {
    return snprintf(buf, cap,
                    "Retained=%d\nRetainLost=%d\nWidened=%d\nUnknown=%d\n"
                    "KindMismatch=%d\nClamped=%d\nDuplicate=%d\nMalformed=%s\n",
                    r->retained, r->retain_lost, r->widened, r->unknown,
                    r->kind_mismatch, r->clamped, r->duplicate,
                    r->malformed ? "true" : "false");
}

static void test_upgrade(void) {
    const char *name = "TestCUpgradeMatchesGo";
    int before = failures;
    static const char *const ok_cases[4] = {"initial-populated",
                                            "consumers-populated",
                                            "empty-consumers", "future-note"};
    static const char *const bad_cases[1] = {"truncated"};
    int i;
    for (i = 0; i < 4; i++) {
        char path[256], want[256];
        int64_t wl = 0, rl = 0, gl = 0;
        uint8_t *wire, *want_rep, *got = NULL;
        int64_t got_len = 0;
        TableReport report;
        char err[256];
        err[0] = '\0';
        snprintf(path, sizeof(path), "tables/cards/conformance/cases/%s.wire",
                 ok_cases[i]);
        snprintf(want, sizeof(want), "tables/cards/conformance/expected/%s.report",
                 ok_cases[i]);
        wire = read_file(path, &wl);
        want_rep = read_file(want, &rl);
        check(wire != NULL && want_rep != NULL, name, "upgrade case files read");
        if (wire == NULL || want_rep == NULL) {
            free(wire);
            free(want_rep);
            continue;
        }
        check(cards_upgrade(wire, wl, &got, &got_len, &report, err, sizeof(err)),
              name, err[0] ? err : "upgrade");
        {
            char rb[512];
            int rn = report_text(&report, rb, sizeof(rb));
            check(rn > 0 && (int64_t)rn == rl &&
                      memcmp(rb, want_rep, (size_t)rl) == 0,
                  name, "upgrade report matches expected byte for byte");
        }
        snprintf(want, sizeof(want), "tables/cards/conformance/expected/%s.wire",
                 ok_cases[i]);
        {
            uint8_t *want_wire = read_file(want, &gl);
            check(want_wire != NULL && got != NULL && got_len == gl &&
                      memcmp(got, want_wire, (size_t)gl) == 0,
                  name, "upgrade wire matches expected byte for byte");
            free(want_wire);
        }
        /* idempotence: upgrade(upgrade(w)) == upgrade(w). */
        {
            uint8_t *again = NULL;
            int64_t again_len = 0;
            TableReport r2;
            char e2[256];
            e2[0] = '\0';
            check(cards_upgrade(got, got_len, &again, &again_len, &r2, e2,
                                sizeof(e2)),
                  name, e2[0] ? e2 : "second upgrade");
            check(again != NULL && again_len == got_len &&
                      memcmp(again, got, (size_t)got_len) == 0,
                  name, "upgrade is byte-idempotent");
            cards_free_buf(again);
        }
        cards_free_buf(got);
        free(wire);
        free(want_rep);
    }
    for (i = 0; i < 1; i++) {
        char path[256];
        int64_t wl = 0;
        uint8_t *wire;
        uint8_t *copy;
        uint8_t *got = NULL;
        int64_t got_len = 0;
        TableReport report;
        char err[256];
        err[0] = '\0';
        snprintf(path, sizeof(path), "tables/cards/conformance/cases/%s.wire",
                 bad_cases[i]);
        wire = read_file(path, &wl);
        check(wire != NULL, name, "refusal case file read");
        if (wire == NULL) continue;
        copy = (uint8_t *)malloc((size_t)wl);
        memcpy(copy, wire, (size_t)wl);
        check(!cards_upgrade(wire, wl, &got, &got_len, &report, err, sizeof(err)),
              name, "malformed wire refused");
        check(strstr(err, "malformed") != NULL, name,
              err[0] ? err : "refusal names malformed");
        check(memcmp(copy, wire, (size_t)wl) == 0, name,
              "refused upgrade leaves the input unchanged");
        free(copy);
        free(wire);
    }
    /* one retained id short refuses with retain-lost and leaves the input. */
    {
        int64_t wl = 0;
        uint8_t *wire = read_file(
            "tables/cards/conformance/cases/future-note.wire", &wl);
        uint8_t *copy;
        uint8_t *got = NULL;
        int64_t got_len = 0;
        TableReport report;
        char err[256];
        err[0] = '\0';
        check(wire != NULL, name, "retention case file read");
        if (wire != NULL) {
            copy = (uint8_t *)malloc((size_t)wl);
            memcpy(copy, wire, (size_t)wl);
            check(!cards_upgrade_ex(wire, wl, &got, &got_len, &report, 1 << 20, 0,
                                    err, sizeof(err)),
                  name, "one id short refuses");
            check(strstr(err, "retain") != NULL, name,
                  err[0] ? err : "refusal names retain-lost");
            check(memcmp(copy, wire, (size_t)wl) == 0, name,
                  "retain-lost refusal leaves the input unchanged");
            free(copy);
            free(wire);
        }
    }
    if (failures == before) printf("PASS %s\n", name);
}

/* TBuf is the test's own byte vector, written independently of the facade. */
typedef struct TBuf {
    uint8_t *data;
    int64_t len, cap;
} TBuf;

static int tb_put(TBuf *b, const void *p, int64_t n) {
    if (b->len + n > b->cap) {
        int64_t cap = b->cap ? b->cap : 64;
        uint8_t *nd;
        while (cap < b->len + n) cap *= 2;
        nd = (uint8_t *)realloc(b->data, (size_t)cap);
        if (nd == NULL) return 0;
        b->data = nd;
        b->cap = cap;
    }
    if (n > 0) memcpy(b->data + b->len, p, (size_t)n);
    b->len += n;
    return 1;
}

static int tb_u64(TBuf *b, uint64_t v) {
    uint8_t raw[8];
    int i;
    for (i = 0; i < 8; i++) raw[i] = (uint8_t)(v >> (8 * i));
    return tb_put(b, raw, 8);
}

static int tb_text(TBuf *b, const char *s) {
    size_t n = s == NULL ? 0 : strlen(s);
    return tb_u64(b, (uint64_t)n) && tb_put(b, s, (int64_t)n);
}

/* TestCTaskHashFramesLogicalFields pins the frame with an independently
   written byte vector, shows body "a"/paths ["bc"] and body "ab"/paths ["c"]
   hash differently, refuses a lossy record, and matches every expected .hex
   (DESIGN L3, edit.go TaskHash). It also checks the FIPS 180-4 vectors the
   facade's SHA-256 must reproduce. */
static void test_task_hash(void) {
    const char *name = "TestCTaskHashFramesLogicalFields";
    int before = failures;
    static const char *const hex_cases[3] = {"card-populated",
                                             "initial-populated",
                                             "empty-consumers"};
    int i;
    /* FIPS 180-4 vectors. */
    {
        uint8_t d[32];
        static const char *const empty_hex =
            "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855";
        static const char *const abc_hex =
            "ba7816bf8f01cfea414140de5dae2223b00361a396177a9cb410ff61f20015ad";
        static const char *const long_hex =
            "248d6a61d20638b8e5c026930c3e6039a33ce45964ff2167f6ecedd419db06c1";
        char hex[65];
        int j;
        static const char long_msg[] =
            "abcdbcdecdefdefgefghfghighijhijkijkljklmklmnlmnomnopnopq";
        cards_sha256(d, (const uint8_t *)"", 0);
        for (j = 0; j < 32; j++) snprintf(hex + j * 2, 3, "%02x", d[j]);
        check(memcmp(hex, empty_hex, 64) == 0, name, "SHA-256 empty vector");
        cards_sha256(d, (const uint8_t *)"abc", 3);
        for (j = 0; j < 32; j++) snprintf(hex + j * 2, 3, "%02x", d[j]);
        check(memcmp(hex, abc_hex, 64) == 0, name, "SHA-256 abc vector");
        cards_sha256(d, (const uint8_t *)long_msg, strlen(long_msg));
        for (j = 0; j < 32; j++) snprintf(hex + j * 2, 3, "%02x", d[j]);
        check(memcmp(hex, long_hex, 64) == 0, name, "SHA-256 448-bit vector");
    }
    /* expected .hex per corpus wire. */
    for (i = 0; i < 3; i++) {
        char path[256], hexpath[256];
        int64_t wl = 0, hl = 0;
        uint8_t *wire = NULL, *want = NULL;
        CardsRecord rec;
        TableReport report;
        uint8_t hex[65];
        char err[256];
        err[0] = '\0';
        snprintf(path, sizeof(path), "tables/cards/conformance/cases/%s.wire",
                 hex_cases[i]);
        snprintf(hexpath, sizeof(hexpath),
                 "tables/cards/conformance/expected/%s.hex", hex_cases[i]);
        wire = read_file(path, &wl);
        want = read_file(hexpath, &hl);
        check(wire != NULL && want != NULL, name, "hex case files read");
        if (wire == NULL || want == NULL) {
            free(wire);
            free(want);
            continue;
        }
        check(cards_load(wire, wl, &rec, &report, err, sizeof(err)), name,
              err[0] ? err : "load hex case");
        check(cards_task_hash(&rec, hex, err, sizeof(err)), name,
              err[0] ? err : "task hash");
        check(hl >= 64 && hex_eq(hex, want), name,
              "task hash matches expected .hex");
        cards_record_clear(&rec);
        free(wire);
        free(want);
    }
    /* frame boundary: "a"+"bc" vs "ab"+"c" must differ; pin A independently. */
    {
        const char *json_a = "{\"id\":\"x\",\"body\":\"a\",\"paths\":[{\"s\":\"bc\"}]}";
        const char *json_b = "{\"id\":\"x\",\"body\":\"ab\",\"paths\":[{\"s\":\"c\"}]}";
        CardsRecord a, b;
        char err[256];
        uint8_t hex_a[65], hex_b[65];
        uint8_t digest[32];
        char want_hex[65];
        TBuf f;
        int j;
        err[0] = '\0';
        memset(&f, 0, sizeof(f));
        check(cards_from_json((const uint8_t *)json_a, (int64_t)strlen(json_a),
                              &a, err, sizeof(err)),
              name, err[0] ? err : "build A");
        check(cards_from_json((const uint8_t *)json_b, (int64_t)strlen(json_b),
                              &b, err, sizeof(err)),
              name, err[0] ? err : "build B");
        check(cards_task_hash(&a, hex_a, err, sizeof(err)), name, "hash A");
        check(cards_task_hash(&b, hex_b, err, sizeof(err)), name, "hash B");
        check(memcmp(hex_a, hex_b, 64) != 0, name,
              "body/paths boundary does not collide");
        /* independently written frame: body, paths, new, shared, test, done. */
        tb_text(&f, "a");
        tb_u64(&f, 1);
        tb_text(&f, "bc");
        tb_u64(&f, 0); /* new */
        tb_u64(&f, 0); /* shared */
        tb_text(&f, ""); /* test */
        tb_text(&f, ""); /* done */
        cards_sha256(digest, f.data, (size_t)f.len);
        for (j = 0; j < 32; j++) snprintf(want_hex + j * 2, 3, "%02x", digest[j]);
        check(memcmp(hex_a, want_hex, 64) == 0, name,
              "hash matches the independently written frame");
        free(f.data);
        cards_record_clear(&a);
        cards_record_clear(&b);
    }
    /* a lossy record is refused, never hashed. */
    {
        int64_t wl = 0;
        uint8_t *wire = read_file(
            "tables/cards/conformance/cases/future-note.wire", &wl);
        CardsRecord rec;
        TableReport report;
        uint8_t hex[65];
        char err[256];
        err[0] = '\0';
        check(wire != NULL, name, "lossy case read");
        if (wire != NULL) {
            check(cards_load(wire, wl, &rec, &report, err, sizeof(err)), name,
                  err[0] ? err : "load lossy");
            err[0] = '\0';
            check(!cards_task_hash(&rec, hex, err, sizeof(err)), name,
                  "lossy record refused by the hash");
            check(err[0] != '\0', name, "refusal carries an error");
            cards_record_clear(&rec);
            free(wire);
        }
    }
    if (failures == before) printf("PASS %s\n", name);
}

static int load_case(const char *file, CardsRecord *rec, char *err, size_t cap) {
    int64_t wl = 0;
    uint8_t *wire = read_file(file, &wl);
    TableReport report;
    int ok;
    if (wire == NULL) return 0;
    ok = cards_load(wire, wl, rec, &report, err, cap);
    free(wire);
    return ok;
}

static int wire_equals_file(const CardsRecord *rec, const char *file) {
    int64_t wl = 0;
    uint8_t *want = read_file(file, &wl);
    int ok;
    if (want == NULL) return 0;
    ok = rec->wire_len == wl && memcmp(rec->wire, want, (size_t)wl) == 0;
    free(want);
    return ok;
}

/* TestCFieldEditLeavesTheTaskHashAlone pins the atomic edits (DESIGN L4,
   edit.go Set*): needs, tier, priority, column and holder change the wire and
   not the task hash; a paths edit changes both; the body is byte-equal
   throughout; every refusal leaves the Wire byte-equal; every corpus edit
   matches its expected wire or refusal. */
static void test_edits(void) {
    const char *name = "TestCFieldEditLeavesTheTaskHashAlone";
    int before = failures;
    const char *base = "tables/cards/conformance/cases/card-populated.wire";
    CardsRecord rec;
    char err[256];
    char h0[65];
    TableStringView body0;
    uint8_t *body_copy;
    int64_t body_len;
    err[0] = '\0';
    check(load_case(base, &rec, err, sizeof(err)), name,
          err[0] ? err : "load base");
    check(cards_task_hash(&rec, (uint8_t *)h0, err, sizeof(err)), name,
          "base hash");
    body0 = table_string_at(NULL, &rec.root->body);
    body_len = body0.length;
    body_copy = (uint8_t *)malloc((size_t)(body_len > 0 ? body_len : 1));
    if (body_len > 0) memcpy(body_copy, body0.data, (size_t)body_len);
    {
        const char *vals[2] = {"a", "b"};
        char h1[65];
        check(cards_set_needs(&rec, vals, 2, err, sizeof(err)), name,
              "set needs");
        check(cards_task_hash(&rec, (uint8_t *)h1, err, sizeof(err)), name,
              "hash after needs");
        check(memcmp(h0, h1, 64) == 0, name, "needs edit leaves the hash alone");
        check(wire_equals_file(&rec,
                               "tables/cards/conformance/expected/needs.wire"),
              name, "needs edit wire matches expected");
        check(table_string_at(NULL, &rec.root->body).length == body_len &&
                  memcmp(table_string_at(NULL, &rec.root->body).data, body_copy,
                         (size_t)body_len) == 0,
              name, "needs edit leaves the body alone");
        {
            const char *one[1] = {"z"};
            char h2[65];
            check(cards_set_needs(&rec, one, 1, err, sizeof(err)), name,
                  "set needs to a different count");
            check(cards_task_hash(&rec, (uint8_t *)h2, err, sizeof(err)), name,
                  "hash after a different needs");
            check(memcmp(h0, h2, 64) == 0, name,
                  "needs change leaves the hash alone");
        }
    }
    /* tier */
    {
        char h1[65];
        cards_record_clear(&rec);
        check(load_case(base, &rec, err, sizeof(err)), name, "reload for tier");
        check(cards_set_tier(&rec, TIER_HEAVY, err, sizeof(err)), name, "set tier");
        check(cards_task_hash(&rec, (uint8_t *)h1, err, sizeof(err)), name,
              "hash after tier");
        check(memcmp(h0, h1, 64) == 0, name, "tier edit leaves the hash alone");
        check(wire_equals_file(&rec,
                               "tables/cards/conformance/expected/tier.wire"),
              name, "tier edit wire matches expected");
    }
    /* priority */
    {
        char h1[65];
        cards_record_clear(&rec);
        check(load_case(base, &rec, err, sizeof(err)), name, "reload for priority");
        check(cards_set_priority(&rec, PRIORITY_BLOCKER, err, sizeof(err)), name,
              "set priority");
        check(cards_task_hash(&rec, (uint8_t *)h1, err, sizeof(err)), name,
              "hash after priority");
        check(memcmp(h0, h1, 64) == 0, name,
              "priority edit leaves the hash alone");
        check(wire_equals_file(&rec,
                               "tables/cards/conformance/expected/priority.wire"),
              name, "priority edit wire matches expected");
    }
    /* column */
    {
        char h1[65];
        cards_record_clear(&rec);
        check(load_case(base, &rec, err, sizeof(err)), name, "reload for column");
        check(cards_set_column(&rec, COLUMN_LANDED, err, sizeof(err)), name,
              "set column");
        check(cards_task_hash(&rec, (uint8_t *)h1, err, sizeof(err)), name,
              "hash after column");
        check(memcmp(h0, h1, 64) == 0, name, "column edit leaves the hash alone");
        check(wire_equals_file(&rec,
                               "tables/cards/conformance/expected/column.wire"),
              name, "column edit wire matches expected");
    }
    /* holder */
    {
        char h1[65];
        cards_record_clear(&rec);
        check(load_case(base, &rec, err, sizeof(err)), name, "reload for holder");
        check(cards_set_holder(&rec, "h", err, sizeof(err)), name, "set holder");
        check(cards_task_hash(&rec, (uint8_t *)h1, err, sizeof(err)), name,
              "hash after holder");
        check(memcmp(h0, h1, 64) == 0, name, "holder edit leaves the hash alone");
        check(wire_equals_file(&rec,
                               "tables/cards/conformance/expected/holder.wire"),
              name, "holder edit wire matches expected");
    }
    /* paths changes both the wire and the hash. */
    {
        CardsRecord pr;
        char h1[65];
        const char *vals[2] = {"x", "y"};
        check(load_case(base, &pr, err, sizeof(err)), name, "reload base for paths");
        check(cards_set_paths(&pr, vals, 2, err, sizeof(err)), name,
              "set paths");
        check(cards_task_hash(&pr, (uint8_t *)h1, err, sizeof(err)), name,
              "hash after paths");
        check(memcmp(h0, h1, 64) != 0, name, "paths edit changes the hash");
        check(wire_equals_file(&pr,
                               "tables/cards/conformance/expected/paths.wire"),
              name, "paths edit wire matches expected");
        cards_record_clear(&pr);
    }
    /* empty-to-growing: a paths edit on the empty card. */
    {
        CardsRecord er;
        const char *vals[1] = {"x"};
        check(load_case("tables/cards/conformance/cases/empty-consumers.wire", &er,
                        err, sizeof(err)),
              name, "load empty-consumers");
        check(cards_set_paths(&er, vals, 1, err, sizeof(err)), name,
              "empty-to-growing set paths");
        check(wire_equals_file(
                  &er, "tables/cards/conformance/expected/empty-to-growing.wire"),
              name, "empty-to-growing wire matches expected");
        cards_record_clear(&er);
    }
    /* a refusal leaves the wire byte-equal: unknown field, over-bound holder. */
    {
        CardsRecord ur;
        int64_t wl;
        uint8_t *copy;
        const char *vals[1] = {"a"};
        check(load_case("tables/cards/conformance/cases/future-note.wire", &ur, err,
                        sizeof(err)),
              name, "load future-note");
        wl = ur.wire_len;
        copy = (uint8_t *)malloc((size_t)wl);
        memcpy(copy, ur.wire, (size_t)wl);
        check(!cards_set_needs(&ur, vals, 1, err, sizeof(err)), name,
              "edit over an unknown field refuses");
        check(strstr(err, "unknown") != NULL, name,
              err[0] ? err : "refusal names unknown");
        check(ur.wire_len == wl && memcmp(ur.wire, copy, (size_t)wl) == 0, name,
              "unknown refusal leaves the wire byte-equal");
        free(copy);
        cards_record_clear(&ur);
    }
    cards_record_clear(&rec);
    free(body_copy);
    if (failures == before) printf("PASS %s\n", name);
}

/* TestCViewWalksEveryField compares the generated view with an explicit
   vocabulary written here (not derived from the descriptors) and with the
   corpus view.txt, and walks a loaded and an edited record through the
   descriptors (DESIGN L4, docs/SPEC-TABLES.md §8). */
static void test_view(void) {
    const char *name = "TestCViewWalksEveryField";
    int before = failures;
    static const char *const vocabulary =
        "Card id string value 64\n"
        "Card sha string value 40\n"
        "Card tier Tier value -\n"
        "Card repo string value 96\n"
        "Card _base string value 96\n"
        "Card kind string value 24\n"
        "Card priority Priority value -\n"
        "Card stream string value 32\n"
        "Card paths PathText list -\n"
        "Card _new PathText list -\n"
        "Card shared PathText list -\n"
        "Card needs CardID list -\n"
        "Card test string value 192\n"
        "Card done string blob -\n"
        "Card deadline_minutes int32 value -\n"
        "Card source string blob -\n"
        "Card epoch int32 value -\n"
        "Card column Column value -\n"
        "Card holder string value 32\n"
        "Card attempt int32 value -\n"
        "Card head string value 40\n"
        "Card branch string value 128\n"
        "Card body string blob -\n"
        "Card extra KeyValue list -\n"
        "Card consumers Consumer list -\n"
        "CardID s string value 64\n"
        "Consumer kind string value 16\n"
        "Consumer who string value 32\n"
        "Consumer route string value 48\n"
        "Consumer model string value 64\n"
        "Consumer started int64 value -\n"
        "Consumer ended int64 value -\n"
        "Consumer outcome string value 16\n"
        "Consumer line string value 200\n"
        "Consumer cost_micro_usd int64 value -\n"
        "KeyValue key string value 32\n"
        "KeyValue value string blob -\n"
        "PathText s string value 192\n";
    uint8_t *out = NULL;
    int64_t out_len = 0;
    char err[256];
    err[0] = '\0';
    check(cards_view(&out, &out_len, err, sizeof(err)), name,
          err[0] ? err : "view");
    check(out != NULL && out_len == (int64_t)strlen(vocabulary) &&
              memcmp(out, vocabulary, (size_t)out_len) == 0,
          name, "view matches the explicit vocabulary");
    {
        int64_t vl = 0;
        uint8_t *want = read_file("tables/cards/conformance/expected/view.txt", &vl);
        check(want != NULL && out_len == vl &&
                  memcmp(out, want, (size_t)vl) == 0,
              name, "view matches the corpus view.txt");
        free(want);
    }
    cards_free_buf(out);
    /* Walk a loaded and an edited record through the descriptors. */
    {
        const UnitViewInfo *v = unit_view();
        const TableTypeInfo *card = NULL;
        int t, f;
        for (t = 0; t < v->num_tables; t++) {
            if (strcmp(v->tables[t].name, "Card") == 0)
                card = v->tables[t].type;
        }
        check(card != NULL, name, "Card descriptor present");
        if (card != NULL) {
            CardsRecord rec;
            const TableFieldInfo *tier_f = NULL, *body_f = NULL, *paths_f = NULL;
            err[0] = '\0';
            for (f = 0; f < card->num_fields; f++) {
                if (strcmp(card->fields[f].name, "tier") == 0)
                    tier_f = &card->fields[f];
                if (strcmp(card->fields[f].name, "body") == 0)
                    body_f = &card->fields[f];
                if (strcmp(card->fields[f].name, "paths") == 0)
                    paths_f = &card->fields[f];
            }
            check(tier_f != NULL && body_f != NULL && paths_f != NULL, name,
                  "named descriptors resolve");
            check(load_case("tables/cards/conformance/cases/card-populated.wire",
                            &rec, err, sizeof(err)),
                  name, err[0] ? err : "load for view walk");
            if (tier_f != NULL) {
                const uint8_t *p = (const uint8_t *)rec.root + tier_f->offset;
                check(*p == TIER_FLASH, name, "scalar walked at its offset");
                check(cards_set_tier(&rec, TIER_HEAVY, err, sizeof(err)), name,
                      "edit for view walk");
                p = (const uint8_t *)rec.root + tier_f->offset;
                check(*p == TIER_HEAVY, name,
                      "edited scalar walked at its offset");
            }
            if (body_f != NULL) {
                const TableRef *ref = (const TableRef *)((const uint8_t *)rec.root +
                                                          body_f->offset);
                TableBytesView bv = table_bytes_at(NULL, ref);
                TableStringView sv = table_string_at(NULL, &rec.root->body);
                check(body_f->is_pointer && bv.length == sv.length &&
                          memcmp(bv.data, sv.data, (size_t)bv.length) == 0,
                      name, "body walked through the descriptor");
            }
            if (paths_f != NULL) {
                const TableList *list =
                    (const TableList *)((const uint8_t *)rec.root + paths_f->offset);
                check(paths_f->sequence == 1 && list->count == 2, name,
                      "list walked through the descriptor");
            }
            cards_record_clear(&rec);
        }
    }
    if (failures == before) printf("PASS %s\n", name);
}

int main(void) {
    test_round_trip();
    test_bounds();
    test_parse_render();
    test_upgrade();
    test_task_hash();
    test_edits();
    test_view();
    if (failures != 0) {
        printf("cards_test: %d failure(s)\n", failures);
        return 1;
    }
    printf("cards_test: all checks pass\n");
    return 0;
}
