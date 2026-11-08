// Hostile JSON text refusals and surrogate escape handling in C (docs/SPEC-TABLES.md §16.2, §16.3, §16.12).
package ctable

import "testing"

func TestCJsonHostileTextRefusals(t *testing.T) {
	t.Parallel()
	const schema = `package probe

table Probe
{
    name  string(8)
    wide  wstring(8)
    blob  *string
    rules [..4]string(8)
    names map[string(8)]int32
    by_id map[uint16]int32
}
`
	runCProbe(t, schema, `#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include "ProbeTable.h"

#define CHECK(x) do { if (!(x)) { fprintf(stderr, "FAIL line %d: %s\n", __LINE__, #x); return 1; } } while (0)

typedef struct Case {
    const char * name;
    const char * text;
    int expect_ok;
    int32_t unknown;
    int32_t kind_mismatch;
    int32_t widened;
    int32_t clamped;
    int32_t duplicate;
    int strict_reason;
} Case;

int main(void)
{
    static const Case cases[] = {
        /* text_holds_zero (§16.3): U+0000 in any text-storage target refuses */
        {"zero-string", "{\"name\": \"a\\u0000b\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"zero-wstring", "{\"wide\": \"a\\u0000b\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"zero-blob", "{\"blob\": \"a\\u0000b\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"zero-text-list", "{\"rules\": [\"a\\u0000b\"]}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"zero-map-key", "{\"names\": {\"a\\u0000b\": 1}}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"zero-map-key-over-bound", "{\"names\": {\"aaaaaaaaa\\u0000\": 1}}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"zero-integer-map-key", "{\"by_id\": {\"1\\u0000\": 1}}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"zero-after-lone-lead", "{\"name\": \"\\ud800\\u0000\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        /* U+0000 in a skipped unknown key or its value stores nothing and is not text_holds_zero */
        {"zero-unknown-value", "{\"nope\": \"a\\u0000b\"}", 1, 1, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_OK},
        {"zero-object-key", "{\"a\\u0000b\": 1}", 1, 1, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_OK},
        {"one-escape-stored", "{\"name\": \"a\\u0001b\"}", 1, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_OK},
        /* string_not_utf8 (§16.3): ill-formed UTF-8 bytes refuse at every string site before any clamp */
        {"illformed-at-bound", "{\"name\": \"aaaaaaaa\xff\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-inside", "{\"name\": \"a\xff\" \"b\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-truncated", "{\"name\": \"a\xe2\x82\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-overlong", "{\"name\": \"\xc0\xaf\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-surrogate-bytes", "{\"name\": \"\xed\xa0\x80\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-unknown-value", "{\"nope\": \"\xff\"}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-object-key", "{\"\xff\": 1}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-map-key", "{\"names\": {\"a\xff\": 1}}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"illformed-text-list", "{\"rules\": [\"a\xff\"]}", 0, 0, 0, 0, 0, 0, SCHEMA_TABLE_REFUSE_WIRE_DAMAGED},
        {"well-formed-at-bound", "{\"name\": \"aaaaaaaa\xc3\xa9\"}", 1, 0, 0, 0, 1, 0, SCHEMA_TABLE_REFUSE_CLAMPED},
    };
    size_t i;
    for (i = 0; i < sizeof(cases) / sizeof(cases[0]); i++) {
        const Case * c = &cases[i];
        int64_t len = (int64_t)strlen(c->text);
        ProbeBuilder b;
        TableReport r;
        int ok;

        /* Tolerant read */
        CHECK(probe_builder_init(&b));
        memset(&r, 0, sizeof(r));
        ok = probe_from_json(&b, c->text, len, &r);
        if (c->expect_ok) {
            CHECK(ok && !r.refused);
            CHECK(r.unknown == c->unknown && r.kind_mismatch == c->kind_mismatch &&
                  r.widened == c->widened && r.clamped == c->clamped && r.duplicate == c->duplicate);
        } else {
            CHECK(!ok && r.refused);
        }
        probe_builder_shutdown(&b);

        /* Strict read */
        CHECK(probe_builder_init(&b));
        memset(&r, 0, sizeof(r));
        ok = probe_from_json(&b, c->text, len, &r);
        if (c->strict_reason == SCHEMA_TABLE_REFUSE_OK) {
            CHECK(ok && r.refuse_reason == SCHEMA_TABLE_REFUSE_OK);
        } else {
            CHECK(!ok && r.refuse_reason == c->strict_reason);
        }
        probe_builder_shutdown(&b);
    }

    /* Surrogate escapes (§16.2, §16.3, §16.12): well-formed pair decodes to U+1D11E;
       lone or out-of-order surrogate escape decodes to U+FFFD without swallowing the next unit. */
    {
        ProbeBuilder b;
        TableReport r;
        Probe * root;

        /* 1. Surrogate pair \uD834\uDD1E -> U+1D11E (UTF-8 F0 9D 84 9E, UTF-16 D834 DD1E) */
        const char * pair_json = "{\"name\": \"\\ud834\\udd1e\", \"wide\": \"\\ud834\\udd1e\"}";
        CHECK(probe_builder_init(&b));
        memset(&r, 0, sizeof(r));
        CHECK(probe_from_json(&b, pair_json, (int64_t)strlen(pair_json), &r));
        CHECK(r.refuse_reason == SCHEMA_TABLE_REFUSE_OK && r.clamped == 0);
        root = probe_builder_root(&b);
        CHECK(root->name_length == 4 && memcmp(root->name, "\xf0\x9d\x84\x9e", 4) == 0);
        CHECK(root->wide_length == 2 && root->wide[0] == 0xd834 && root->wide[1] == 0xdd1e);
        probe_builder_shutdown(&b);

        /* 2. Lone lead surrogate followed by escape \uD800\u0041 -> U+FFFD then 'A' */
        const char * lone_lead_json = "{\"name\": \"\\ud800\\u0041\", \"wide\": \"\\ud800\\u0041\"}";
        CHECK(probe_builder_init(&b));
        memset(&r, 0, sizeof(r));
        CHECK(probe_from_json(&b, lone_lead_json, (int64_t)strlen(lone_lead_json), &r));
        CHECK(r.refuse_reason == SCHEMA_TABLE_REFUSE_OK && !r.refused);
        root = probe_builder_root(&b);
        CHECK(root->name_length == 4 && memcmp(root->name, "\xef\xbf\xbd" "A", 4) == 0);
        CHECK(root->wide_length == 2 && root->wide[0] == 0xfffd && root->wide[1] == 0x0041);
        probe_builder_shutdown(&b);

        /* 3. Lone trail surrogate \uDC00 -> U+FFFD */
        const char * lone_trail_json = "{\"name\": \"\\udc00\", \"wide\": \"\\udc00\"}";
        CHECK(probe_builder_init(&b));
        memset(&r, 0, sizeof(r));
        CHECK(probe_from_json(&b, lone_trail_json, (int64_t)strlen(lone_trail_json), &r));
        CHECK(r.refuse_reason == SCHEMA_TABLE_REFUSE_OK && !r.refused);
        root = probe_builder_root(&b);
        CHECK(root->name_length == 3 && memcmp(root->name, "\xef\xbf\xbd", 3) == 0);
        CHECK(root->wide_length == 1 && root->wide[0] == 0xfffd);
        probe_builder_shutdown(&b);

        /* 4. Inverted surrogates \uDD1E\uD834 -> two U+FFFD */
        const char * inverted_json = "{\"name\": \"\\udd1e\\ud834\", \"wide\": \"\\udd1e\\ud834\"}";
        CHECK(probe_builder_init(&b));
        memset(&r, 0, sizeof(r));
        CHECK(probe_from_json(&b, inverted_json, (int64_t)strlen(inverted_json), &r));
        CHECK(r.refuse_reason == SCHEMA_TABLE_REFUSE_OK && !r.refused);
        root = probe_builder_root(&b);
        CHECK(root->name_length == 6 && memcmp(root->name, "\xef\xbf\xbd\xef\xbf\xbd", 6) == 0);
        CHECK(root->wide_length == 2 && root->wide[0] == 0xfffd && root->wide[1] == 0xfffd);
        probe_builder_shutdown(&b);
    }

    return 0;
}
`)
}
