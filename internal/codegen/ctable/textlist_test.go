// Text lists and their | total = N byte cap (docs/SPEC-TABLES.md §2.13).
package ctable

import "testing"

func TestCTextListsAndTotals(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Handover
{
    rules [..4]string(6)
    first [..4]?string(6)
    repos []string(6)
    bases []?string(6)
    capped_rules [..4]string(6) | total = 8
    capped_repos []?string(6)   | total = 8
}

table Uncapped
{
    capped_rules [..4]string(6)
    capped_repos []?string(6)
}
`
	runCProbe(t, schema, `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "FAIL line %d: %s\n", __LINE__, #x); return 1; } } while (0)

int main(void)
{
    uint8_t wire[4096];
    SCHEMA_C_ALIGN16 uint8_t region[65536];
    SCHEMA_C_ALIGN16 uint8_t cook_buf[65536];
    char json[4096];
    HandoverBuilder b;
    Handover * root;
    const Handover * locked;
    const Handover * loaded;
    const Handover * cooked;
    TableCtx ctx;
    TableReport report;
    int64_t n, jn, cn;
    int32_t len = -1;
    const char * s;

    /* 1. Builder + Append + AppendAbsent + TextAt across all 4 spellings */
    CHECK(handover_builder_init(&b));
    root = handover_builder_root(&b);
    CHECK(root != NULL);

    CHECK(handover_rules_append(root, "alpha", 5));
    CHECK(handover_rules_append(root, "beta", 4));
    CHECK(!handover_rules_append(root, "toolong", 7)); /* > M=6 refused */
    CHECK(!handover_rules_append(root, "a\0b", 3));     /* embedded NUL refused */
    CHECK(!handover_rules_append(root, "\xff", 1));    /* invalid UTF-8 refused */

    CHECK(handover_first_append(root, "one", 3));
    CHECK(handover_first_append_absent(root));
    CHECK(handover_first_append(root, "", 0));

    CHECK(handover_repos_append(&b.main, root, "r1", 2));
    CHECK(handover_repos_append(&b.main, root, "r22", 3));
    CHECK(!handover_repos_append(&b.main, root, "1234567", 7));

    CHECK(handover_bases_append_absent(&b.main, root));
    CHECK(handover_bases_append(&b.main, root, "b1", 2));

    /* total = 8 enforcement in append */
    CHECK(handover_capped_rules_append(root, "abcd", 4));
    CHECK(handover_capped_rules_append(root, "efgh", 4));
    CHECK(!handover_capped_rules_append(root, "i", 1)); /* 4+4+1 = 9 > 8 refused */
    CHECK(root->capped_rules_count == 2);

    CHECK(handover_capped_repos_append(&b.main, root, "12345", 5));
    CHECK(handover_capped_repos_append_absent(&b.main, root)); /* absent adds 0 bytes */
    CHECK(handover_capped_repos_append(&b.main, root, "678", 3));
    CHECK(!handover_capped_repos_append(&b.main, root, "9", 1)); /* 5+0+3+1 = 9 > 8 refused */
    CHECK(root->capped_repos.count == 3);

    s = handover_rules_text_at(root, 0, &len);
    CHECK(s != NULL && len == 5 && strcmp(s, "alpha") == 0);
    s = handover_first_text_at(root, 0, &len);
    CHECK(s != NULL && len == 3 && strcmp(s, "one") == 0);
    s = handover_first_text_at(root, 1, &len);
    CHECK(s == NULL && len == 0);
    s = handover_first_text_at(root, 2, &len);
    CHECK(s != NULL && len == 0 && strcmp(s, "") == 0);

    memset(&ctx, 0, sizeof(ctx)); ctx.arena = &b.arena;
    s = handover_repos_text_at(&ctx, root, 1, &len);
    CHECK(s != NULL && len == 3 && strcmp(s, "r22") == 0);
    s = handover_bases_text_at(&ctx, root, 0, &len);
    CHECK(s == NULL && len == 0);
    s = handover_bases_text_at(&ctx, root, 1, &len);
    CHECK(s != NULL && len == 2 && strcmp(s, "b1") == 0);

    CHECK(handover_builder_lock(&b));
    locked = (const Handover *)b.region;

    /* 2. Wire Measure + Save + Load + Cook + Open */
    n = handover_measure(NULL, locked);
    CHECK(n > 0 && n <= (int64_t)sizeof(wire));
    CHECK(handover_save(NULL, locked, wire, sizeof(wire)) == n);

    memset(&report, 0, sizeof(report));
    loaded = handover_load(region, sizeof(region), wire, n, &report);
    CHECK(loaded != NULL && !report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_OK);
    s = handover_rules_text_at(loaded, 1, &len);
    CHECK(s != NULL && len == 4 && strcmp(s, "beta") == 0);
    s = handover_repos_text_at(NULL, loaded, 0, &len);
    CHECK(s != NULL && len == 2 && strcmp(s, "r1") == 0);
    s = handover_bases_text_at(NULL, loaded, 0, &len);
    CHECK(s == NULL && len == 0);
    s = handover_bases_text_at(NULL, loaded, 1, &len);
    CHECK(s != NULL && len == 2 && strcmp(s, "b1") == 0);

    cn = handover_cook_measure(NULL, locked);
    CHECK(cn > 0 && cn <= (int64_t)sizeof(cook_buf));
    CHECK(handover_cook(NULL, locked, cook_buf, (uint64_t)cn, TableByteOrder_Little));
    cooked = handover_open(cook_buf, (uint64_t)cn);
    CHECK(cooked != NULL);
    s = handover_capped_rules_text_at(cooked, 1, &len);
    CHECK(s != NULL && len == 4 && strcmp(s, "efgh") == 0);
    handover_builder_shutdown(&b);

    /* 3. JSON Write + Read round-trip */
    jn = handover_to_json_measure(loaded);
    CHECK(jn > 0 && jn < (int64_t)sizeof(json));
    CHECK(handover_to_json(loaded, json, sizeof(json)) == jn);
    json[jn] = 0;
    CHECK(strstr(json, "\"rules\": [\n    \"alpha\",\n    \"beta\"\n  ]") != NULL);
    CHECK(strstr(json, "\"first\": [\n    \"one\",\n    null,\n    \"\"\n  ]") != NULL);

    CHECK(handover_builder_init(&b));
    memset(&report, 0, sizeof(report));
    CHECK(handover_from_json(&b, json, jn, &report));
    root = handover_builder_root(&b);
    memset(&ctx, 0, sizeof(ctx)); ctx.arena = &b.arena;
    s = handover_first_text_at(root, 1, &len);
    CHECK(s == NULL);
    s = handover_first_text_at(root, 2, &len);
    CHECK(s != NULL && len == 0);
    s = handover_capped_repos_text_at(&ctx, root, 2, &len);
    CHECK(s != NULL && len == 3 && strcmp(s, "678") == 0);
    handover_builder_shutdown(&b);

    /* 4. JSON total = 8 over-cap refuses the whole load */
    {
        const char * over_json = "{\"capped_rules\":[\"aaaa\",\"bbbb\",\"cc\",\"cccccccccccc\"],\"capped_repos\":[\"1234\",null,\"5678\",\"99\",\"9999999999\"]}";
        CHECK(handover_builder_init(&b));
        memset(&report, 0, sizeof(report));
        CHECK(!handover_from_json(&b, over_json, (int64_t)strlen(over_json), &report));
        CHECK(report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_OUT_OF_RANGE);
        root = handover_builder_root(&b);
        CHECK(root->capped_rules_count == 0 && root->capped_repos.count == 0);
        handover_builder_shutdown(&b);
    }

    /* 5. Writer refusal when stored total exceeds total = 8 */
    {
        CHECK(handover_builder_init(&b));
        root = handover_builder_root(&b);
        CHECK(handover_capped_rules_append(root, "aaaa", 4));
        CHECK(handover_capped_rules_append(root, "bbbb", 4));
        memcpy(root->capped_rules[2].value, "c", 2);
        root->capped_rules[2].value_length = 1;
        root->capped_rules_count = 3;
        memset(&ctx, 0, sizeof(ctx)); ctx.arena = &b.arena;
        CHECK(handover_measure(&ctx, root) == -1);
        CHECK(handover_save(&ctx, root, wire, sizeof(wire)) == -1);
        CHECK(handover_builder_lock(&b));
        locked = (const Handover *)b.region;
        CHECK(handover_to_json_measure(locked) == -1);
        CHECK(handover_to_json(locked, json, sizeof(json)) == -1);
        handover_builder_shutdown(&b);
    }

    /* 6. Wire total = 8 prefix clamp when loading a writer without total */
    {
        UncappedBuilder ub;
        Uncapped * ur;
        CHECK(uncapped_builder_init(&ub));
        ur = uncapped_builder_root(&ub);
        CHECK(uncapped_capped_rules_append(ur, "aaaa", 4));
        CHECK(uncapped_capped_rules_append(ur, "bbbb", 4));
        CHECK(uncapped_capped_rules_append(ur, "cccc", 4));
        CHECK(uncapped_capped_repos_append(&ub.main, ur, "1234", 4));
        CHECK(uncapped_capped_repos_append_absent(&ub.main, ur));
        CHECK(uncapped_capped_repos_append(&ub.main, ur, "5678", 4));
        CHECK(uncapped_capped_repos_append(&ub.main, ur, "90", 2));
        CHECK(uncapped_builder_lock(&ub));
        n = uncapped_save(NULL, (const Uncapped *)ub.region, wire, sizeof(wire));
        CHECK(n > 0);
        uncapped_builder_shutdown(&ub);

        memset(&report, 0, sizeof(report));
        CHECK(handover_load(region, sizeof(region), wire, n, &report) == NULL);
        CHECK(report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_OUT_OF_RANGE);
    }

    return 0;
}
`)
}
