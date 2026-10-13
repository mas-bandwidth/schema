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
                         (long long)body.length,
                         (long long)CARDS_MAX_BODY_BYTES);
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
                             lists[li].name,
                             (long long)lists[li].list->count,
                             (long long)CARDS_MAX_LIST_ENTRIES);
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

/* ---- parse once, render many (DESIGN L2) ---- */

/* DynBuf is a growable byte buffer for rendered text. */
typedef struct DynBuf {
    uint8_t *data;
    int64_t len;
    int64_t cap;
} DynBuf;

static int dyn_put(DynBuf *b, const void *p, int64_t n) {
    if (n <= 0) return 1;
    if (b->len + n > b->cap) {
        int64_t cap = b->cap > 0 ? b->cap : 256;
        uint8_t *nd;
        while (cap < b->len + n) cap *= 2;
        nd = (uint8_t *)realloc(b->data, (size_t)cap);
        if (nd == NULL) return 0;
        b->data = nd;
        b->cap = cap;
    }
    memcpy(b->data + b->len, p, (size_t)n);
    b->len += n;
    return 1;
}

static int dyn_str(DynBuf *b, const char *s) {
    return dyn_put(b, s, (int64_t)strlen(s));
}

/* StrList is a growable list of owned strings, in arrival order. */
typedef struct StrList {
    char **items;
    int count;
    int cap;
} StrList;

static int strlist_push(StrList *l, const uint8_t *s, int64_t n) {
    char *copy;
    if (l->count >= l->cap) {
        int cap = l->cap > 0 ? l->cap * 2 : 8;
        char **ni = (char **)realloc(l->items, (size_t)cap * sizeof(char *));
        if (ni == NULL) return 0;
        l->items = ni;
        l->cap = cap;
    }
    copy = (char *)malloc((size_t)n + 1);
    if (copy == NULL) return 0;
    memcpy(copy, s, (size_t)n);
    copy[n] = '\0';
    l->items[l->count++] = copy;
    return 1;
}

static void strlist_free(StrList *l) {
    int i;
    for (i = 0; i < l->count; i++) free(l->items[i]);
    free(l->items);
    l->items = NULL;
    l->count = l->cap = 0;
}

static char *dup_range(const uint8_t *s, int64_t n) {
    char *copy = (char *)malloc((size_t)n + 1);
    if (copy == NULL) return NULL;
    memcpy(copy, s, (size_t)n);
    copy[n] = '\0';
    return copy;
}

/* ParsedCard is the intermediate result of reading the header lines before
   any generated storage exists. It ports parsedCard in parse.go. */
typedef struct ParsedCard {
    char *id;
    char *sha;
    int tier;
    char *repo;
    char *base;
    char *kind;
    int priority;
    StrList paths;
    StrList news;
    StrList shared;
    StrList needs;
    char *test;
    char *done_when;
    char *source;
    int32_t deadline;
    StrList extra_keys;
    StrList extra_vals;
} ParsedCard;

static void parsed_free(ParsedCard *p) {
    free(p->id);
    free(p->sha);
    free(p->repo);
    free(p->base);
    free(p->kind);
    free(p->test);
    free(p->done_when);
    free(p->source);
    strlist_free(&p->paths);
    strlist_free(&p->news);
    strlist_free(&p->shared);
    strlist_free(&p->needs);
    strlist_free(&p->extra_keys);
    strlist_free(&p->extra_vals);
}

static int is_space_byte(uint8_t c) {
    return c == ' ' || c == '\t' || c == '\n' || c == '\v' || c == '\f' || c == '\r';
}

/* trim_range narrows [s,e) past surrounding whitespace, as strings.TrimSpace
   does for the ASCII spellings briefs carry. */
static void trim_range(const uint8_t **s, const uint8_t **e) {
    while (*s < *e && is_space_byte((*s)[0])) (*s)++;
    while (*e > *s && is_space_byte((*e)[-1])) (*e)--;
}

/* find_last finds the last occurrence of needle in hay[0..n), or -1. */
static int64_t find_last(const uint8_t *hay, int64_t n, const char *needle) {
    int64_t nl = (int64_t)strlen(needle);
    int64_t i;
    for (i = n - nl; i >= 0; i--) {
        if (memcmp(hay + i, needle, (size_t)nl) == 0) return i;
    }
    return -1;
}

static int range_eq(const uint8_t *s, int64_t n, const char *want) {
    return (int64_t)strlen(want) == n && memcmp(s, want, (size_t)n) == 0;
}

/* match_header recognizes a header key line under parse.go's key grammar
   (matchHeader, line 171): a key of one letter then letters, digits and
   hyphens, a colon, optional whitespace, and the rest as the value. Leading
   whitespace is not a key. */
static int match_header(const uint8_t *line, int64_t n, const uint8_t **key,
                        int64_t *key_len, const uint8_t **val, int64_t *val_len) {
    int64_t i = 0;
    if (n < 1) return 0;
    if (!((line[0] >= 'A' && line[0] <= 'Z') || (line[0] >= 'a' && line[0] <= 'z')))
        return 0;
    i = 1;
    while (i < n) {
        uint8_t c = line[i];
        if ((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') ||
            (c >= '0' && c <= '9') || c == '-')
            i++;
        else
            break;
    }
    if (i >= n || line[i] != ':') return 0;
    *key = line;
    *key_len = i;
    i++;
    while (i < n && is_space_byte(line[i])) i++;
    *val = line + i;
    *val_len = n - i;
    return 1;
}

static int is_blank_line(const uint8_t *s, int64_t n) {
    int64_t i;
    for (i = 0; i < n; i++) {
        if (s[i] != ' ' && s[i] != '\t' && s[i] != '\r') return 0;
    }
    return 1;
}

static int is_hex_str(const char *s) {
    size_t i, n = strlen(s);
    if (n == 0) return 0;
    for (i = 0; i < n; i++) {
        char c = s[i];
        if (!((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f') ||
              (c >= 'A' && c <= 'F')))
            return 0;
    }
    return 1;
}

static int parse_tier_str(const char *s, int *out) {
    if (strcmp(s, "flash") == 0) { *out = TIER_FLASH; return 1; }
    if (strcmp(s, "pro") == 0) { *out = TIER_PRO; return 1; }
    if (strcmp(s, "heavy") == 0) { *out = TIER_HEAVY; return 1; }
    if (strcmp(s, "frontier") == 0) { *out = TIER_FRONTIER; return 1; }
    return 0;
}

static int parse_priority_str(const char *s, int *out) {
    if (strcmp(s, "normal") == 0) { *out = PRIORITY_NORMAL; return 1; }
    if (strcmp(s, "high") == 0) { *out = PRIORITY_HIGH; return 1; }
    if (strcmp(s, "critical") == 0) { *out = PRIORITY_CRITICAL; return 1; }
    if (strcmp(s, "blocker") == 0) { *out = PRIORITY_BLOCKER; return 1; }
    return 0;
}

/* check_str_bound enforces one declared byte bound, as parse.go states it. */
static int check_str_bound(const char *field, const uint8_t *s, int64_t n,
                           int64_t bound, char *err, size_t err_cap) {
    (void)s;
    if (n > bound)
        return failf(err, err_cap, "cards: %s is %lld bytes, past the %lld-byte bound",
                     field, (long long)n, (long long)bound);
    return 1;
}

/* split_list_checked splits a comma-separated list value under parse.go's
   rules: trimmed elements, empty ones dropped, the element bound and the
   entry ceiling enforced. A whole value of "-" or empty is an empty list;
   PATHS alone also takes "none". */
static int split_list_checked(const uint8_t *val, int64_t val_len, int64_t bound,
                              int none_sentinel, const char *field, int num,
                              StrList *out, char *err, size_t err_cap) {
    const uint8_t *s = val, *e = val + val_len;
    const uint8_t *p;
    trim_range(&s, &e);
    if (s == e || (e - s == 1 && s[0] == '-')) return 1;
    if (none_sentinel && range_eq(s, e - s, "none")) return 1;
    p = s;
    for (;;) {
        const uint8_t *comma = p;
        const uint8_t *es, *ee;
        while (comma < e && *comma != ',') comma++;
        es = p;
        ee = comma;
        trim_range(&es, &ee);
        if (es < ee) {
            if (!check_str_bound(field, es, ee - es, bound, err, err_cap)) return 0;
            if (!strlist_push(out, es, ee - es))
                return fail(err, err_cap, "cards: allocation failed");
        }
        if (comma >= e) break;
        p = comma + 1;
    }
    if (out->count > CARDS_MAX_LIST_ENTRIES)
        return failf(err, err_cap,
                     "cards: %s on line %d has %d entries, past the %d-entry ceiling",
                     field, num, out->count, CARDS_MAX_LIST_ENTRIES);
    return 1;
}

/* parse_deadline reads "finish within N minutes." with N in [0, 100000]. */
static int parse_deadline(const uint8_t *val, int64_t val_len, int32_t *out,
                          char *err, size_t err_cap) {
    static const char pre[] = "finish within ";
    static const char suf[] = " minutes.";
    const uint8_t *s = val, *e = val + val_len;
    int64_t n = 0;
    int digits = 0;
    trim_range(&s, &e);
    if (e - s < (int64_t)(sizeof(pre) - 1 + sizeof(suf) - 1) ||
        memcmp(s, pre, sizeof(pre) - 1) != 0 ||
        memcmp(e - (sizeof(suf) - 1), suf, sizeof(suf) - 1) != 0)
        return fail(err, err_cap,
                    "cards: DEADLINE is not of the form \"finish within N minutes.\"");
    s += sizeof(pre) - 1;
    e -= sizeof(suf) - 1;
    while (s < e) {
        if (*s < '0' || *s > '9')
            return fail(err, err_cap,
                        "cards: DEADLINE is not of the form \"finish within N minutes.\"");
        n = n * 10 + (*s - '0');
        if (n > 1000000) return fail(err, err_cap, "cards: minutes out of range");
        digits = 1;
        s++;
    }
    if (!digits || n > 100000)
        return fail(err, err_cap, "cards: minutes out of range");
    *out = (int32_t)n;
    return 1;
}

static int is_stranded_key(const uint8_t *key, int64_t n) {
    static const char *const keys[] = {"KIND", "PATHS", "TEST", "LEGS", "SOURCE",
                                       "START", "STOP", "DEPENDS-ON", "PRIORITY",
                                       "NEW", "SHARED", "REPO", "BASE", "tier",
                                       "RESULT"};
    size_t i;
    for (i = 0; i < sizeof(keys) / sizeof(keys[0]); i++) {
        if (range_eq(key, n, keys[i])) return 1;
    }
    return 0;
}

static int is_reserved_key(const uint8_t *key, int64_t n) {
    static const char *const keys[] = {"RESULT", "tier", "REPO", "BASE", "KIND",
                                       "PRIORITY", "PATHS", "NEW", "SHARED",
                                       "DEPENDS-ON", "TEST", "DONE-WHEN",
                                       "DEADLINE", "SOURCE"};
    size_t i;
    for (i = 0; i < sizeof(keys) / sizeof(keys[0]); i++) {
        if (range_eq(key, n, keys[i])) return 1;
    }
    return 0;
}

/* assign_fixed copies a bounded string into a generated string(N) buffer. */
static void assign_fixed(char *dst, int32_t *len, const char *s) {
    size_t n = strlen(s);
    memcpy(dst, s, n);
    dst[n] = '\0';
    *len = (int32_t)n;
}

/* cards_parse ports Parse in tables/cards/parse.go (DESIGN L2): the header
   block of KEY: value lines under the RESULT line, known keys into typed
   fields, unknown keys into extra in arrival order, the body the rest byte
   for byte, and a stranded key below the first prose line refused. */
int cards_parse(const uint8_t *text, int64_t text_len, CardsRecord *out,
                char *err, size_t err_cap) {
    ParsedCard p;
    int64_t i;
    int result = 0;
    /* Physical lines: content ranges, terminator ends and line numbers. */
    int64_t max_lines = 1;
    int64_t nlines = 0;
    const uint8_t **lstart = NULL, **lend = NULL;
    int64_t *lterm = NULL, *lnum = NULL;
    int64_t last_header_end;
    int prose = 0;
    /* seen keys for duplicate refusal */
    char **seen_keys = NULL;
    int *seen_nums = NULL;
    int seen_count = 0, seen_cap = 0;
    const uint8_t *body;
    int64_t body_len;
    CardBuilder b;
    TableCtx ctx;
    Card *root;
    int k;

    memset(&p, 0, sizeof(p));
    memset(out, 0, sizeof(*out));
    if (!utf8_valid(text, text_len) && memchr(text, 0, (size_t)text_len) == NULL) {
        /* invalid UTF-8 without NUL still refuses below on the same terms */
    }
    if (memchr(text, 0, (size_t)text_len) != NULL)
        return fail(err, err_cap, "cards: brief contains a NUL byte");
    if (!utf8_valid(text, text_len))
        return fail(err, err_cap, "cards: brief is not valid UTF-8");

    for (i = 0; i < text_len; i++) {
        if (text[i] == '\n') max_lines++;
    }
    lstart = (const uint8_t **)malloc((size_t)max_lines * sizeof(*lstart));
    lend = (const uint8_t **)malloc((size_t)max_lines * sizeof(*lend));
    lterm = (int64_t *)malloc((size_t)max_lines * sizeof(*lterm));
    lnum = (int64_t *)malloc((size_t)max_lines * sizeof(*lnum));
    if (lstart == NULL || lend == NULL || lterm == NULL || lnum == NULL) {
        fail(err, err_cap, "cards: allocation failed");
        goto done;
    }
    {
        int64_t pos = 0;
        int64_t num = 1;
        while (pos < text_len) {
            int64_t start = pos;
            int64_t nl = start;
            int64_t term;
            while (nl < text_len && text[nl] != '\n') nl++;
            term = nl < text_len ? nl + 1 : text_len;
            /* a trailing CR rides with the terminator, as parse.go treats it */
            {
                int64_t cend = nl;
                if (cend > start && text[cend - 1] == '\r') cend--;
                lstart[nlines] = text + start;
                lend[nlines] = text + cend;
            }
            lterm[nlines] = term;
            lnum[nlines] = num++;
            nlines++;
            pos = term;
        }
    }
    if (nlines == 0) {
        fail(err, err_cap, "cards: brief has no RESULT line");
        goto done;
    }
    /* The RESULT line. */
    {
        const uint8_t *key, *val;
        int64_t klen, vlen;
        const uint8_t *vs, *ve;
        int64_t ti, si;
        char *tier_str, *id_str, *sha_str;
        if (!match_header(lstart[0], lend[0] - lstart[0], &key, &klen, &val, &vlen) ||
            !range_eq(key, klen, "RESULT")) {
            fail(err, err_cap, "cards: line 1 is not a RESULT line");
            goto done;
        }
        vs = val;
        ve = val + vlen;
        ti = find_last(vs, ve - vs, " tier:");
        if (ti < 0) {
            fail(err, err_cap, "cards: RESULT line has no \" tier:\"");
            goto done;
        }
        {
            const uint8_t *ts = vs + ti + 6, *te = ve;
            trim_range(&ts, &te);
            tier_str = dup_range(ts, te - ts);
        }
        si = find_last(vs, ti, " sha=");
        if (si < 0) {
            free(tier_str);
            fail(err, err_cap, "cards: RESULT line has no \" sha=\"");
            goto done;
        }
        {
            const uint8_t *is = vs, *ie = vs + si;
            trim_range(&is, &ie);
            id_str = dup_range(is, ie - is);
            sha_str = dup_range(vs + si + 5, (vs + ti) - (vs + si + 5));
        }
        if (id_str == NULL || sha_str == NULL || tier_str == NULL) {
            free(id_str);
            free(sha_str);
            free(tier_str);
            fail(err, err_cap, "cards: allocation failed");
            goto done;
        }
        if (!check_str_bound("id", (const uint8_t *)id_str, (int64_t)strlen(id_str),
                             64, err, err_cap)) {
            free(id_str);
            free(sha_str);
            free(tier_str);
            goto done;
        }
        if (!is_hex_str(sha_str) ||
            (strlen(sha_str) != 12 && strlen(sha_str) != 40)) {
            failf(err, err_cap, "cards: RESULT sha %s is not 12 or 40 hex digits",
                  sha_str);
            free(id_str);
            free(sha_str);
            free(tier_str);
            goto done;
        }
        if (!parse_tier_str(tier_str, &p.tier)) {
            failf(err, err_cap, "cards: RESULT tier: unknown tier %s", tier_str);
            free(id_str);
            free(sha_str);
            free(tier_str);
            goto done;
        }
        p.id = id_str;
        p.sha = sha_str;
        free(tier_str);
    }
    /* seen: RESULT on line 1 */
    seen_cap = 16;
    seen_keys = (char **)malloc((size_t)seen_cap * sizeof(char *));
    seen_nums = (int *)malloc((size_t)seen_cap * sizeof(int));
    if (seen_keys == NULL || seen_nums == NULL) {
        fail(err, err_cap, "cards: allocation failed");
        goto done;
    }
    seen_keys[0] = dup_range((const uint8_t *)"RESULT", 6);
    seen_nums[0] = 1;
    seen_count = 1;

    last_header_end = lterm[0];
    for (k = 1; k < nlines; k++) {
        const uint8_t *line = lstart[k];
        int64_t llen = lend[k] - lstart[k];
        const uint8_t *key, *val;
        int64_t klen, vlen;
        if (is_blank_line(line, llen)) continue;
        if (!match_header(line, llen, &key, &klen, &val, &vlen)) {
            prose = 1;
            continue;
        }
        if (prose) {
            if (is_stranded_key(key, klen)) {
                char kb[40];
                int64_t cn = klen < 32 ? klen : 32;
                memcpy(kb, key, (size_t)cn);
                kb[cn] = '\0';
                failf(err, err_cap, "cards: stranded header key \"%s\" on line %lld",
                      kb, (long long)lnum[k]);
                goto done;
            }
            continue;
        }
        /* A header line before prose. */
        if (memchr(val, '\r', (size_t)vlen) != NULL ||
            memchr(val, '\n', (size_t)vlen) != NULL) {
            fail(err, err_cap, "cards: header value has a CR or LF");
            goto done;
        }
        if (range_eq(key, klen, "tier")) {
            int prev = 0, s2;
            int t;
            char *tstr;
            for (s2 = 0; s2 < seen_count; s2++) {
                if (strcmp(seen_keys[s2], "tier") == 0) prev = seen_nums[s2];
            }
            if (prev != 0) {
                failf(err, err_cap,
                      "cards: duplicate header key \"tier\" on lines %d and %lld",
                      prev, (long long)lnum[k]);
                goto done;
            }
            {
                const uint8_t *ts = val, *te = val + vlen;
                trim_range(&ts, &te);
                tstr = dup_range(ts, te - ts);
            }
            if (tstr == NULL || !parse_tier_str(tstr, &t)) {
                free(tstr);
                fail(err, err_cap, "cards: tier has no header spelling");
                goto done;
            }
            free(tstr);
            if (t != p.tier) {
                failf(err, err_cap, "cards: tier on line %lld disagrees with RESULT",
                      (long long)lnum[k]);
                goto done;
            }
            if (seen_count >= seen_cap) {
                seen_cap *= 2;
                seen_keys = (char **)realloc(seen_keys, (size_t)seen_cap * sizeof(char *));
                seen_nums = (int *)realloc(seen_nums, (size_t)seen_cap * sizeof(int));
                if (seen_keys == NULL || seen_nums == NULL) {
                    fail(err, err_cap, "cards: allocation failed");
                    goto done;
                }
            }
            seen_keys[seen_count] = dup_range(key, klen);
            seen_nums[seen_count] = (int)lnum[k];
            seen_count++;
            last_header_end = lterm[k];
            continue;
        }
        if (range_eq(key, klen, "RESULT")) {
            failf(err, err_cap,
                  "cards: duplicate header key \"RESULT\" on lines %d and %lld",
                  seen_nums[0], (long long)lnum[k]);
            goto done;
        }
        if (is_reserved_key(key, klen)) {
            int prev = 0, s2;
            for (s2 = 0; s2 < seen_count; s2++) {
                if ((int64_t)strlen(seen_keys[s2]) == klen &&
                    memcmp(seen_keys[s2], key, (size_t)klen) == 0)
                    prev = seen_nums[s2];
            }
            if (prev != 0) {
                char kb[40];
                int64_t cn = klen < 32 ? klen : 32;
                memcpy(kb, key, (size_t)cn);
                kb[cn] = '\0';
                failf(err, err_cap,
                      "cards: duplicate header key \"%s\" on lines %d and %lld", kb,
                      prev, (long long)lnum[k]);
                goto done;
            }
            if (seen_count >= seen_cap) {
                seen_cap *= 2;
                seen_keys = (char **)realloc(seen_keys, (size_t)seen_cap * sizeof(char *));
                seen_nums = (int *)realloc(seen_nums, (size_t)seen_cap * sizeof(int));
                if (seen_keys == NULL || seen_nums == NULL) {
                    fail(err, err_cap, "cards: allocation failed");
                    goto done;
                }
            }
            seen_keys[seen_count] = dup_range(key, klen);
            seen_nums[seen_count] = (int)lnum[k];
            seen_count++;
            /* Assign the mapped field. */
            if (range_eq(key, klen, "REPO") || range_eq(key, klen, "BASE") ||
                range_eq(key, klen, "KIND") || range_eq(key, klen, "TEST")) {
                int64_t bound = range_eq(key, klen, "KIND") ? 24
                                : range_eq(key, klen, "TEST") ? 192
                                                               : 96;
                char **dst = range_eq(key, klen, "REPO")   ? &p.repo
                             : range_eq(key, klen, "BASE") ? &p.base
                             : range_eq(key, klen, "KIND") ? &p.kind
                                                           : &p.test;
                char kb[16];
                int64_t cn = klen < 15 ? klen : 15;
                memcpy(kb, key, (size_t)cn);
                kb[cn] = '\0';
                if (!check_str_bound(kb, val, vlen, bound, err, err_cap)) goto done;
                *dst = dup_range(val, vlen);
                if (*dst == NULL) {
                    fail(err, err_cap, "cards: allocation failed");
                    goto done;
                }
            } else if (range_eq(key, klen, "PRIORITY")) {
                const uint8_t *ts = val, *te = val + vlen;
                char *tstr;
                trim_range(&ts, &te);
                tstr = dup_range(ts, te - ts);
                if (tstr == NULL || !parse_priority_str(tstr, &p.priority)) {
                    free(tstr);
                    fail(err, err_cap, "cards: PRIORITY has no header spelling");
                    goto done;
                }
                free(tstr);
            } else if (range_eq(key, klen, "PATHS")) {
                if (!split_list_checked(val, vlen, 192, 1, "PATHS", (int)lnum[k],
                                        &p.paths, err, err_cap))
                    goto done;
            } else if (range_eq(key, klen, "NEW")) {
                if (!split_list_checked(val, vlen, 192, 0, "NEW", (int)lnum[k],
                                        &p.news, err, err_cap))
                    goto done;
            } else if (range_eq(key, klen, "SHARED")) {
                if (!split_list_checked(val, vlen, 192, 0, "SHARED", (int)lnum[k],
                                        &p.shared, err, err_cap))
                    goto done;
            } else if (range_eq(key, klen, "DEPENDS-ON")) {
                if (!split_list_checked(val, vlen, 64, 0, "DEPENDS-ON", (int)lnum[k],
                                        &p.needs, err, err_cap))
                    goto done;
            } else if (range_eq(key, klen, "DONE-WHEN")) {
                p.done_when = dup_range(val, vlen);
                if (p.done_when == NULL) {
                    fail(err, err_cap, "cards: allocation failed");
                    goto done;
                }
            } else if (range_eq(key, klen, "DEADLINE")) {
                if (!parse_deadline(val, vlen, &p.deadline, err, err_cap)) goto done;
            } else if (range_eq(key, klen, "SOURCE")) {
                p.source = dup_range(val, vlen);
                if (p.source == NULL) {
                    fail(err, err_cap, "cards: allocation failed");
                    goto done;
                }
            }
            last_header_end = lterm[k];
            continue;
        }
        /* An unknown key rides into extra in arrival order. */
        if (!check_str_bound("extra key", key, klen, 32, err, err_cap)) goto done;
        if (!strlist_push(&p.extra_keys, key, klen) ||
            !strlist_push(&p.extra_vals, val, vlen)) {
            fail(err, err_cap, "cards: allocation failed");
            goto done;
        }
        last_header_end = lterm[k];
    }

    body = text + last_header_end;
    body_len = text_len - last_header_end;

    /* Build through the generated builder and checked write (§6.2). */
    if (!card_builder_init(&b)) {
        fail(err, err_cap, "cards: arena initialization failed");
        goto done;
    }
    root = card_builder_root(&b);
    assign_fixed(root->id, &root->id_length, p.id != NULL ? p.id : "");
    assign_fixed(root->sha, &root->sha_length, p.sha != NULL ? p.sha : "");
    root->tier = (Tier)p.tier;
    assign_fixed(root->repo, &root->repo_length, p.repo != NULL ? p.repo : "");
    assign_fixed(root->_base, &root->_base_length, p.base != NULL ? p.base : "");
    assign_fixed(root->kind, &root->kind_length, p.kind != NULL ? p.kind : "");
    root->priority = (Priority)p.priority;
    assign_fixed(root->stream, &root->stream_length, "");
    {
        int e2;
        for (e2 = 0; e2 < p.paths.count; e2++) {
            PathText *row = card_paths_add(&b.main, &root->paths);
            if (row == NULL) { card_builder_shutdown(&b); fail(err, err_cap, "cards: builder refused a paths element"); goto done; }
            assign_fixed(row->s, &row->s_length, p.paths.items[e2]);
        }
        for (e2 = 0; e2 < p.news.count; e2++) {
            PathText *row = card_new_add(&b.main, &root->_new);
            if (row == NULL) { card_builder_shutdown(&b); fail(err, err_cap, "cards: builder refused a new element"); goto done; }
            assign_fixed(row->s, &row->s_length, p.news.items[e2]);
        }
        for (e2 = 0; e2 < p.shared.count; e2++) {
            PathText *row = card_shared_add(&b.main, &root->shared);
            if (row == NULL) { card_builder_shutdown(&b); fail(err, err_cap, "cards: builder refused a shared element"); goto done; }
            assign_fixed(row->s, &row->s_length, p.shared.items[e2]);
        }
        for (e2 = 0; e2 < p.needs.count; e2++) {
            CardID *row = card_needs_add(&b.main, &root->needs);
            if (row == NULL) { card_builder_shutdown(&b); fail(err, err_cap, "cards: builder refused a needs element"); goto done; }
            assign_fixed(row->s, &row->s_length, p.needs.items[e2]);
        }
        for (e2 = 0; e2 < p.extra_keys.count; e2++) {
            KeyValue *row = card_extra_add(&b.main, &root->extra);
            if (row == NULL) { card_builder_shutdown(&b); fail(err, err_cap, "cards: builder refused an extra element"); goto done; }
            assign_fixed(row->key, &row->key_length, p.extra_keys.items[e2]);
            table_string_emplace(&b.main, &row->value, p.extra_vals.items[e2],
                                 (int64_t)strlen(p.extra_vals.items[e2]));
        }
    }
    assign_fixed(root->test, &root->test_length, p.test != NULL ? p.test : "");
    if (p.done_when != NULL && p.done_when[0] != '\0')
        table_string_emplace(&b.main, &root->done, p.done_when,
                             (int64_t)strlen(p.done_when));
    root->deadline_minutes = p.deadline;
    if (p.source != NULL && p.source[0] != '\0')
        table_string_emplace(&b.main, &root->source, p.source,
                             (int64_t)strlen(p.source));
    if (body_len > 0)
        table_string_emplace(&b.main, &root->body, (const char *)body, body_len);
    ctx.arena = &b.arena;
    if (!check_card(root, &ctx, err, err_cap)) {
        card_builder_shutdown(&b);
        goto done;
    }
    result = adopt_builder(&b, out, err, err_cap);
done:
    {
        int s2;
        for (s2 = 0; s2 < seen_count; s2++) free(seen_keys[s2]);
    }
    free(seen_keys);
    free(seen_nums);
    free(lstart);
    free(lend);
    free(lterm);
    free(lnum);
    parsed_free(&p);
    return result;
}

/* ---- render ---- */

static const char *tier_name(int t) {
    switch (t) {
    case TIER_FLASH: return "flash";
    case TIER_PRO: return "pro";
    case TIER_HEAVY: return "heavy";
    case TIER_FRONTIER: return "frontier";
    }
    return NULL;
}

static const char *priority_name(int t) {
    switch (t) {
    case PRIORITY_NONE: return "";
    case PRIORITY_NORMAL: return "normal";
    case PRIORITY_HIGH: return "high";
    case PRIORITY_CRITICAL: return "critical";
    case PRIORITY_BLOCKER: return "blocker";
    }
    return NULL;
}

static void emit_fixed(DynBuf *out, const char *key, const char *buf, int32_t len,
                       int32_t cap) {
    const uint8_t *d;
    int64_t n;
    fixed_view(buf, len, cap, &d, &n);
    if (n == 0) return;
    dyn_str(out, key);
    dyn_str(out, ": ");
    dyn_put(out, d, n);
    dyn_put(out, "\n", 1);
}

static void emit_ptr(DynBuf *out, const char *key, const TableRef *ref) {
    TableStringView v;
    if (ref->value == 0) return;
    v = table_string_at(NULL, ref);
    dyn_str(out, key);
    dyn_str(out, ": ");
    dyn_put(out, v.data, v.length);
    dyn_put(out, "\n", 1);
}

static void emit_list_paths(DynBuf *out, const char *key, const TableList *list,
                            int is_id) {
    TableSequenceCursor cur;
    const void *e;
    int first = 1;
    int32_t idx;
    if (list->count == 0) return;
    dyn_str(out, key);
    dyn_str(out, ": ");
    if (is_id) {
        cur = card_needs_each(NULL, list);
        (void)cur;
        for (idx = 0; idx < list->count; idx++) {
            const CardID *el = card_needs_at(list, idx);
            const uint8_t *d;
            int64_t n;
            if (el == NULL) continue;
            fixed_view(el->s, el->s_length, 64, &d, &n);
            if (!first) dyn_str(out, ", ");
            dyn_put(out, d, n);
            first = 0;
        }
    } else {
        for (idx = 0; idx < list->count; idx++) {
            const PathText *el = card_paths_at(list, idx);
            const uint8_t *d;
            int64_t n;
            if (el == NULL) continue;
            fixed_view(el->s, el->s_length, 192, &d, &n);
            if (!first) dyn_str(out, ", ");
            dyn_put(out, d, n);
            first = 0;
        }
    }
    (void)e;
    dyn_put(out, "\n", 1);
}

/* cards_render ports Render in tables/cards/render.go: RESULT first, the
   mapped keys in fixed order, extras in stored order, then the body bytes
   verbatim (docs/SPEC-TABLES.md §6.2, §16). */
int cards_render(const CardsRecord *record, uint8_t **out, int64_t *out_len,
                 char *err, size_t err_cap) {
    TableReport rep;
    int64_t n;
    uint8_t *region;
    const Card *root;
    DynBuf buf;
    const char *tn, *pn;
    if (record == NULL || record->wire == NULL)
        return fail(err, err_cap, "cards: nil record");
    memset(&rep, 0, sizeof(rep));
    n = card_load_measure(record->wire, record->wire_len);
    if (n < 0) return fail(err, err_cap, "cards: wire is not a card");
    region = (uint8_t *)aligned_alloc64(n);
    if (region == NULL) return fail(err, err_cap, "cards: allocation failed");
    root = card_load(region, n, record->wire, record->wire_len, &rep);
    if (root == NULL) {
        free(region);
        return fail(err, err_cap, "cards: wire is malformed");
    }
    memset(&buf, 0, sizeof(buf));
    tn = tier_name(root->tier);
    if (tn == NULL) {
        free(region);
        free(buf.data);
        return failf(err, err_cap, "cards: tier %d has no header spelling",
                     (int)root->tier);
    }
    dyn_str(&buf, "RESULT: ");
    {
        const uint8_t *d;
        int64_t m;
        fixed_view(root->id, root->id_length, 64, &d, &m);
        dyn_put(&buf, d, m);
    }
    dyn_str(&buf, " sha=");
    {
        const uint8_t *d;
        int64_t m;
        fixed_view(root->sha, root->sha_length, 40, &d, &m);
        dyn_put(&buf, d, m);
    }
    dyn_str(&buf, " tier: ");
    dyn_str(&buf, tn);
    dyn_put(&buf, "\n", 1);
    emit_fixed(&buf, "REPO", root->repo, root->repo_length, 96);
    emit_fixed(&buf, "BASE", root->_base, root->_base_length, 96);
    emit_fixed(&buf, "KIND", root->kind, root->kind_length, 24);
    pn = priority_name(root->priority);
    if (pn == NULL) {
        free(region);
        free(buf.data);
        return failf(err, err_cap, "cards: priority %d has no header spelling",
                     (int)root->priority);
    }
    if (pn[0] != '\0') {
        dyn_str(&buf, "PRIORITY: ");
        dyn_str(&buf, pn);
        dyn_put(&buf, "\n", 1);
    }
    emit_list_paths(&buf, "PATHS", &root->paths, 0);
    emit_list_paths(&buf, "NEW", &root->_new, 0);
    emit_list_paths(&buf, "SHARED", &root->shared, 0);
    emit_list_paths(&buf, "DEPENDS-ON", &root->needs, 1);
    emit_fixed(&buf, "TEST", root->test, root->test_length, 192);
    emit_ptr(&buf, "DONE-WHEN", &root->done);
    if (root->deadline_minutes != 0) {
        char tmp[64];
        snprintf(tmp, sizeof(tmp), "DEADLINE: finish within %d minutes.\n",
                 root->deadline_minutes);
        dyn_str(&buf, tmp);
    }
    emit_ptr(&buf, "SOURCE", &root->source);
    {
        int32_t idx;
        for (idx = 0; idx < root->extra.count; idx++) {
            const KeyValue *e = card_extra_at(&root->extra, idx);
            const uint8_t *d;
            int64_t m;
            TableStringView v;
            if (e == NULL) continue;
            fixed_view(e->key, e->key_length, 32, &d, &m);
            dyn_put(&buf, d, m);
            dyn_str(&buf, ": ");
            if (e->value.value != 0) {
                v = table_string_at(NULL, &e->value);
                dyn_put(&buf, v.data, v.length);
            }
            dyn_put(&buf, "\n", 1);
        }
    }
    if (root->body.value != 0) {
        TableStringView v = table_string_at(NULL, &root->body);
        dyn_put(&buf, v.data, v.length);
    }
    free(region);
    if (buf.data == NULL) {
        buf.data = (uint8_t *)malloc(1);
        buf.len = 0;
    }
    *out = buf.data;
    *out_len = buf.len;
    return 1;
}
