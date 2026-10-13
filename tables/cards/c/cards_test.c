/* cards_test.c — the plain C test program for the C card leg. One check per
   named check, exit status nonzero on any failure. It runs from the
   repository root. No real time, sockets, network or outside services. */
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "cards.h"

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

int main(void) {
    test_round_trip();
    if (failures != 0) {
        printf("cards_test: %d failure(s)\n", failures);
        return 1;
    }
    printf("cards_test: all checks pass\n");
    return 0;
}
