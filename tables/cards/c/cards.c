/* cards.c — the C owning facade for a typed work card. It ports the Go
   facade tables/cards/cards.go (DESIGN L1) and, in its second half,
   tables/cards/parse.go with tables/cards/render.go (DESIGN L2). Every
   function that implements a rule cites its design section. */
#include "cards.h"

#include <stdarg.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

/* ---- small helpers ---- */

static int fail(char *err, size_t err_cap, const char *msg) {
    if (err != NULL && err_cap > 0) {
        size_t n = strlen(msg);
        if (n >= err_cap) n = err_cap - 1;
        memcpy(err, msg, n);
        err[n] = '\0';
    }
    return 0;
}

static int failf(char *err, size_t err_cap, const char *fmt, ...) {
    char buf[256];
    va_list ap;
    va_start(ap, fmt);
    vsnprintf(buf, sizeof(buf), fmt, ap);
    va_end(ap);
    return fail(err, err_cap, buf);
}

/* utf8_valid reports whether s[0..n) is well-formed UTF-8 without NUL
   (docs/SPEC-TABLES.md §2.5). */
static int utf8_valid(const uint8_t *s, int64_t n) {
    int64_t i = 0;
    if (n < 0) return 0;
    while (i < n) {
        uint8_t c = s[i];
        uint32_t cp;
        int extra;
        if (c == 0) return 0;
        if (c < 0x80) {
            i++;
            continue;
        } else if (c >= 0xC2 && c <= 0xDF) {
            cp = c & 0x1F;
            extra = 1;
        } else if (c >= 0xE0 && c <= 0xEF) {
            cp = c & 0x0F;
            extra = 2;
        } else if (c >= 0xF0 && c <= 0xF4) {
            cp = c & 0x07;
            extra = 3;
        } else {
            return 0;
        }
        if (i + 1 + extra > n) return 0;
        {
            int k;
            for (k = 1; k <= extra; k++) {
                uint8_t cc = s[i + k];
                if ((cc & 0xC0) != 0x80) return 0;
                cp = (cp << 6) | (uint32_t)(cc & 0x3F);
            }
        }
        if (extra == 2) {
            if (cp < 0x800) return 0;
            if (cp >= 0xD800 && cp <= 0xDFFF) return 0;
        }
        if (extra == 3) {
            if (cp < 0x10000 || cp > 0x10FFFF) return 0;
        }
        i += extra + 1;
    }
    return 1;
}

static void *aligned_alloc64(int64_t n) {
    void *p = NULL;
    size_t size = (size_t)(n > 0 ? n : 1);
    size = (size + 63u) & ~(size_t)63u;
    p = aligned_alloc(64, size);
    return p;
}

void cards_free_buf(void *buf) { free(buf); }

void cards_record_clear(CardsRecord *record) {
    if (record == NULL) return;
    free(record->wire);
    free(record->region);
    memset(record, 0, sizeof(*record));
}

/* fixed_view clamps a generated string(N) buffer to its used bytes. */
static void fixed_view(const char *buf, int32_t len, int32_t cap,
                       const uint8_t **data, int64_t *n) {
    int32_t v = len;
    if (v > cap) v = cap;
    if (v < 0) v = 0;
    *data = (const uint8_t *)buf;
    *n = v;
}

/* ---- the customer bounds (docs/SPEC-TABLES.md §2.5, §2.9) ---- */

/* check_card enforces the customer bounds and the well-formedness of the
   opaque body and every collection element, before bytes are published.
   ctx names the builder arena for a mutable root and is NULL for a loaded
   self-relative region. It ports checkCard in tables/cards/cards.go. */
static int check_card(const Card *root, const TableCtx *ctx, char *err,
                      size_t err_cap) {
    TableStringView body;
    int32_t i;
    if (root == NULL) return fail(err, err_cap, "cards: nil card");
    body = table_string_at(ctx, &root->body);
    if (root->body.value != 0) {
        if (body.length > CARDS_MAX_BODY_BYTES)
            return failf(err, err_cap,
                         "cards: body is %lld bytes, past the %lld-byte ceiling",
                         NULL, body.length, CARDS_MAX_BODY_BYTES);
        if (!utf8_valid((const uint8_t *)body.data, body.length))
            return fail(err, err_cap,
                        "cards: body is not well-formed UTF-8 without NUL");
    }
    /* The six collections share the entry ceiling (docs/SPEC-TABLES.md §2.9). */
    {
        const struct {
            const TableList *list;
            const char *name;
        } lists[6] = {{&root->paths, "paths"},   {&root->_new, "_new"},
                      {&root->shared, "shared"}, {&root->needs, "needs"},
                      {&root->extra, "extra"},   {&root->consumers, "consumers"}};
        int li;
        for (li = 0; li < 6; li++) {
            if (lists[li].list->count < 0 ||
                lists[li].list->count > CARDS_MAX_LIST_ENTRIES)
                return failf(err, err_cap,
                             "cards: %s has %lld entries, past the %lld-entry ceiling",
                             lists[li].name, lists[li].list->count,
                             CARDS_MAX_LIST_ENTRIES);
        }
    }
    /* Element well-formedness: every collection element is valid UTF-8
       without NUL (docs/SPEC-TABLES.md §2.9). */
    {
        TableSequenceCursor cur;
        const PathText *e;
        const TableList *sets[3] = {&root->paths, &root->_new, &root->shared};
        const char *names[3] = {"paths", "_new", "shared"};
        int si;
        for (si = 0; si < 3; si++) {
            cur = card_paths_each(ctx, sets[si]);
            while ((e = (const PathText *)table_sequence_next(&cur)) != NULL) {
                const uint8_t *d;
                int64_t n;
                fixed_view(e->s, e->s_length, 192, &d, &n);
                if (!utf8_valid(d, n))
                    return failf(err, err_cap,
                                 "cards: %s has an element that is not "
                                 "well-formed UTF-8 without NUL",
                                 names[si]);
            }
        }
        {
            const CardID *ce;
            cur = card_needs_each(ctx, &root->needs);
            while ((ce = (const CardID *)table_sequence_next(&cur)) != NULL) {
                const uint8_t *d;
                int64_t n;
                fixed_view(ce->s, ce->s_length, 64, &d, &n);
                if (!utf8_valid(d, n))
                    return fail(err, err_cap,
                                "cards: needs has an element that is not "
                                "well-formed UTF-8 without NUL");
            }
        }
        {
            const KeyValue *kv;
            cur = card_extra_each(ctx, &root->extra);
            while ((kv = (const KeyValue *)table_sequence_next(&cur)) != NULL) {
                const uint8_t *d;
                int64_t n;
                TableStringView v;
                fixed_view(kv->key, kv->key_length, 32, &d, &n);
                if (!utf8_valid(d, n))
                    return fail(err, err_cap,
                                "cards: extra has a key that is not "
                                "well-formed UTF-8 without NUL");
                /* The admitted header key grammar and the mapped vocabulary
                   (HEADER MAPPING, docs/SPEC-TABLES.md §2.9). */
                {
                    int64_t k;
                    int ok = n >= 1 && ((d[0] >= 'A' && d[0] <= 'Z') ||
                                        (d[0] >= 'a' && d[0] <= 'z'));
                    for (k = 1; ok && k < n; k++) {
                        uint8_t ch = d[k];
                        ok = (ch >= 'A' && ch <= 'Z') || (ch >= 'a' && ch <= 'z') ||
                             (ch >= '0' && ch <= '9') || ch == '-';
                    }
                    if (!ok) {
                        char kb[40];
                        int64_t cn = n < 32 ? n : 32;
                        memcpy(kb, d, (size_t)cn);
                        kb[cn] = '\0';
                        return failf(err, err_cap,
                                     "cards: extra key %s is not an admitted "
                                     "header key",
                                     kb);
                    }
                    {
                        static const char *const reserved[] = {
                            "RESULT", "tier",   "REPO",  "BASE",     "KIND",
                            "PRIORITY", "PATHS",  "NEW",   "SHARED", "DEPENDS-ON",
                            "TEST",   "DONE-WHEN", "DEADLINE", "SOURCE"};
                        size_t ri;
                        for (ri = 0; ri < sizeof(reserved) / sizeof(reserved[0]); ri++) {
                            if ((int64_t)strlen(reserved[ri]) == n &&
                                memcmp(d, reserved[ri], (size_t)n) == 0)
                                return fail(err, err_cap,
                                            "cards: extra key duplicates a "
                                            "mapped header key");
                        }
                    }
                }
                if (memchr(d, '\r', (size_t)n) != NULL ||
                    memchr(d, '\n', (size_t)n) != NULL)
                    return fail(err, err_cap,
                                "cards: extra has a key with a CR or LF");
                v = table_string_at(ctx, &kv->value);
                if (kv->value.value != 0) {
                    if (!utf8_valid((const uint8_t *)v.data, v.length))
                        return fail(err, err_cap,
                                    "cards: extra has a value that is not "
                                    "well-formed UTF-8 without NUL");
                    if (memchr(v.data, '\r', (size_t)v.length) != NULL ||
                        memchr(v.data, '\n', (size_t)v.length) != NULL)
                        return fail(err, err_cap,
                                    "cards: extra has a value with a CR or LF");
                }
            }
        }
        {
            const Consumer *co;
            cur = card_consumers_each(ctx, &root->consumers);
            while ((co = (const Consumer *)table_sequence_next(&cur)) != NULL) {
                const struct {
                    const char *buf;
                    int32_t len;
                    int32_t cap;
                    const char *name;
                } fields[6] = {{co->kind, co->kind_length, 16, "kind"},
                               {co->who, co->who_length, 32, "who"},
                               {co->route, co->route_length, 48, "route"},
                               {co->model, co->model_length, 64, "model"},
                               {co->outcome, co->outcome_length, 16, "outcome"},
                               {co->line, co->line_length, 200, "line"}};
                int fi;
                for (fi = 0; fi < 6; fi++) {
                    const uint8_t *d;
                    int64_t n;
                    fixed_view(fields[fi].buf, fields[fi].len, fields[fi].cap, &d, &n);
                    if (!utf8_valid(d, n))
                        return failf(err, err_cap,
                                     "cards: consumers has a %s that is not "
                                     "well-formed UTF-8 without NUL",
                                     fields[fi].name);
                }
            }
        }
    }
    (void)i;
    return 1;
}

/* adopt_builder locks a filled builder, saves its wire and hands the packed
   region and wire to out (docs/SPEC-TABLES.md §6.2). */
static int adopt_builder(CardBuilder *b, CardsRecord *out, char *err,
                         size_t err_cap) {
    TableReport report;
    const Card *root;
    int64_t size;
    uint8_t *wire;
    memset(&report, 0, sizeof(report));
    if (!card_builder_lock(b)) {
        card_builder_shutdown(b);
        return fail(err, err_cap, "cards: packing failed");
    }
    root = (const Card *)(const void *)b->region;
    size = card_measure(NULL, root);
    if (size < 0) {
        card_builder_shutdown(b);
        return fail(err, err_cap, "cards: measure refused the built card");
    }
    wire = (uint8_t *)malloc((size_t)(size > 0 ? size : 1));
    if (wire == NULL) {
        card_builder_shutdown(b);
        return fail(err, err_cap, "cards: allocation failed");
    }
    if (card_save(NULL, root, wire, size) != size) {
        free(wire);
        card_builder_shutdown(b);
        return fail(err, err_cap, "cards: save did not fill the measured wire");
    }
    memset(out, 0, sizeof(*out));
    out->wire = wire;
    out->wire_len = size;
    out->region = b->region;
    out->region_len = b->region_bytes;
    out->root = (const Card *)(const void *)out->region;
    b->region = NULL;
    b->region_bytes = 0;
    card_builder_shutdown(b);
    return 1;
}

/* cards_load ports Load in tables/cards/cards.go (docs/SPEC-TABLES.md §6.2):
   wire to record and report, refusing malformed input, never upgrading
   implicitly. The record owns a heap copy of its wire. */
int cards_load(const uint8_t *wire, int64_t wire_len, CardsRecord *out,
               TableReport *report, char *err, size_t err_cap) {
    TableReport rep;
    int64_t n;
    uint8_t *region;
    const Card *root;
    memset(&rep, 0, sizeof(rep));
    memset(out, 0, sizeof(*out));
    n = card_load_measure(wire, wire_len);
    if (n < 0) {
        if (report != NULL) *report = rep;
        return fail(err, err_cap, "cards: wire is not a card");
    }
    region = (uint8_t *)aligned_alloc64(n);
    if (region == NULL) return fail(err, err_cap, "cards: allocation failed");
    root = card_load(region, n, wire, wire_len, &rep);
    if (root == NULL) {
        free(region);
        if (report != NULL) *report = rep;
        return fail(err, err_cap, "cards: wire is malformed");
    }
    if (!check_card(root, NULL, err, err_cap)) {
        free(region);
        if (report != NULL) *report = rep;
        return 0;
    }
    out->wire = (uint8_t *)malloc((size_t)(wire_len > 0 ? wire_len : 1));
    if (out->wire == NULL) {
        free(region);
        return fail(err, err_cap, "cards: allocation failed");
    }
    memcpy(out->wire, wire, (size_t)wire_len);
    out->wire_len = wire_len;
    out->region = region;
    out->region_len = n;
    out->root = root;
    out->report = rep;
    if (report != NULL) *report = rep;
    return 1;
}

/* cards_from_json ports FromJSON in tables/cards/cards.go
   (docs/SPEC-TABLES.md §16): JSON text to record, refusing loss events and
   the customer bounds before any record is published. */
int cards_from_json(const uint8_t *text, int64_t text_len, CardsRecord *out,
                    char *err, size_t err_cap) {
    CardBuilder b;
    TableReport report;
    TableCtx ctx;
    memset(out, 0, sizeof(*out));
    memset(&report, 0, sizeof(report));
    if (!card_builder_init(&b))
        return fail(err, err_cap, "cards: arena initialization failed");
    if (!card_from_json(&b, (const char *)text, text_len, &report) ||
        report.malformed) {
        card_builder_shutdown(&b);
        return fail(err, err_cap, "cards: JSON is malformed");
    }
    if (report.unknown > 0 || report.kind_mismatch > 0 || report.clamped > 0) {
        card_builder_shutdown(&b);
        return fail(err, err_cap,
                    "cards: JSON has loss events: refused, never truncated");
    }
    ctx.arena = &b.arena;
    if (!check_card(card_builder_root(&b), &ctx, err, err_cap)) {
        card_builder_shutdown(&b);
        return 0;
    }
    if (!adopt_builder(&b, out, err, err_cap)) return 0;
    out->report = report;
    return 1;
}

/* cards_to_json ports ToJSON in tables/cards/cards.go
   (docs/SPEC-TABLES.md §16): the known-field JSON view of the record. */
int cards_to_json(const CardsRecord *record, uint8_t **out, int64_t *out_len,
                  char *err, size_t err_cap) {
    int64_t size;
    uint8_t *buf;
    if (record == NULL || record->root == NULL)
        return fail(err, err_cap, "cards: nil record");
    size = card_to_json_measure(record->root);
    if (size < 0)
        return fail(err, err_cap, "cards: the card has no JSON spelling");
    buf = (uint8_t *)malloc((size_t)(size > 0 ? size : 1));
    if (buf == NULL) return fail(err, err_cap, "cards: allocation failed");
    if (card_to_json(record->root, (char *)buf, size) != size) {
        free(buf);
        return fail(err, err_cap, "cards: JSON write did not fill the measured text");
    }
    *out = buf;
    *out_len = size;
    return 1;
}

/* cards_wire ports (r *Record) Wire in tables/cards/cards.go: a copy the
   caller owns, so mutating it leaves the record unchanged. */
int cards_wire(const CardsRecord *record, uint8_t **out, int64_t *out_len,
               char *err, size_t err_cap) {
    uint8_t *buf;
    if (record == NULL) return fail(err, err_cap, "cards: nil record");
    buf = (uint8_t *)malloc((size_t)(record->wire_len > 0 ? record->wire_len : 1));
    if (buf == NULL) return fail(err, err_cap, "cards: allocation failed");
    memcpy(buf, record->wire, (size_t)record->wire_len);
    *out = buf;
    *out_len = record->wire_len;
    return 1;
}
