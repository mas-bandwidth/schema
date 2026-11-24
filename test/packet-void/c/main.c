/* The C leg of test/packet-void (SPEC §4.8): answers every case of
   testdata/wire/packet-void/cases.txt, one "case <name> ok|FAILED: <why>"
   line each, for tools/casegate. The pins are the Go oracle's bytes, never
   this writer's. "read" or "write" as the second argument runs one direction
   only: the make/checks/packet-void.mk negative controls sabotage one
   direction and require the other to stay green. */
#include "VoidWire.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

enum kind { K_MIXED, K_TAGS, K_SINGLE, K_EMPTY, K_FRAME, K_BATCH, K_UNKNOWN };

typedef struct value_t
{
    enum kind kind;
    packetvoid_mixed_t mixed;
    packetvoid_tags_t tags;
    packetvoid_single_t single;
    packetvoid_empty_t empty;
    packetvoid_frame_t frame;
    packetvoid_batch_t batch;
} value_t;

static int g_read = 1, g_write = 1;
static char g_why[256];

#define FAIL(...) do { snprintf(g_why, sizeof(g_why), __VA_ARGS__); return 0; } while (0)

static enum kind kind_of(const char *type)
{
    if (strcmp(type, "Mixed") == 0) return K_MIXED;
    if (strcmp(type, "Tags") == 0) return K_TAGS;
    if (strcmp(type, "Single") == 0) return K_SINGLE;
    if (strcmp(type, "Empty") == 0) return K_EMPTY;
    if (strcmp(type, "Frame") == 0) return K_FRAME;
    if (strcmp(type, "Batch") == 0) return K_BATCH;
    return K_UNKNOWN;
}

static void set_mixed(packetvoid_mixed_t *m, packetvoid_mixed_type_t type)
{
    memset(m, 0, sizeof(*m));
    if (type == PACKETVOID_MIXED_TYPE_PAYLOAD)
        packetvoid_mixed_select_payload(m);
    else
        m->type = type;
}

/* The value of each accept case, by name; 0 for a name this driver does not
   know, which is FAILED and never written with another case's value. */
static int build(const char *name, value_t *v)
{
    memset(v, 0, sizeof(*v));
    if (strcmp(name, "mixed-none") == 0) { v->kind = K_MIXED; set_mixed(&v->mixed, PACKETVOID_MIXED_TYPE_NONE); return 1; }
    if (strcmp(name, "mixed-ping") == 0) { v->kind = K_MIXED; set_mixed(&v->mixed, PACKETVOID_MIXED_TYPE_PING); return 1; }
    if (strcmp(name, "mixed-payload") == 0) { v->kind = K_MIXED; set_mixed(&v->mixed, PACKETVOID_MIXED_TYPE_PAYLOAD); return 1; }
    if (strcmp(name, "tags-none") == 0) { v->kind = K_TAGS; v->tags.type = PACKETVOID_TAGS_TYPE_NONE; return 1; }
    if (strcmp(name, "tags-ping") == 0) { v->kind = K_TAGS; v->tags.type = PACKETVOID_TAGS_TYPE_PING; return 1; }
    if (strcmp(name, "tags-pong") == 0) { v->kind = K_TAGS; v->tags.type = PACKETVOID_TAGS_TYPE_PONG; return 1; }
    if (strcmp(name, "single-none") == 0) { v->kind = K_SINGLE; v->single.type = PACKETVOID_SINGLE_TYPE_NONE; return 1; }
    if (strcmp(name, "single-ack") == 0) { v->kind = K_SINGLE; v->single.type = PACKETVOID_SINGLE_TYPE_ACK; return 1; }
    if (strcmp(name, "empty-none") == 0) { v->kind = K_EMPTY; v->empty.type = PACKETVOID_EMPTY_TYPE_NONE; return 1; }
    if (strcmp(name, "frame-ping") == 0)
    {
        v->kind = K_FRAME; v->frame.lead = 5; set_mixed(&v->frame.choice, PACKETVOID_MIXED_TYPE_PING); v->frame.tail = 6;
        return 1;
    }
    if (strcmp(name, "batch-empty") == 0) { v->kind = K_BATCH; v->batch.values_count = 0; return 1; }
    if (strcmp(name, "batch-ping") == 0)
    {
        v->kind = K_BATCH; v->batch.values_count = 1; set_mixed(&v->batch.values[0], PACKETVOID_MIXED_TYPE_PING);
        return 1;
    }
    if (strcmp(name, "batch-ping-payload") == 0)
    {
        v->kind = K_BATCH; v->batch.values_count = 2;
        set_mixed(&v->batch.values[0], PACKETVOID_MIXED_TYPE_PING);
        set_mixed(&v->batch.values[1], PACKETVOID_MIXED_TYPE_PAYLOAD);
        return 1;
    }
    return 0;
}

/* Every read lands on storage dirtied first: a selected payload must be
   constructed with its declared defaults, whatever the storage held. */
static void dirty(value_t *v, enum kind kind)
{
    int i;
    memset(v, 0xA5, sizeof(*v));
    v->kind = kind;
    v->mixed.type = PACKETVOID_MIXED_TYPE_PAYLOAD;
    v->mixed.as.payload.entries_count = 2;
    v->mixed.as.payload.entries[0].retries = 99;
    v->mixed.as.payload.entries[1].retries = 99;
    for (i = 0; i < 2; ++i)
    {
        v->batch.values[i].type = PACKETVOID_MIXED_TYPE_PAYLOAD;
        v->batch.values[i].as.payload.entries[0].retries = 99;
        v->batch.values[i].as.payload.entries[1].retries = 99;
    }
}

static int write_value(serialize_write_stream_t *w, const value_t *v)
{
    switch (v->kind)
    {
    case K_MIXED: return packetvoid_write_mixed(w, &v->mixed);
    case K_TAGS: return packetvoid_write_tags(w, &v->tags);
    case K_SINGLE: return packetvoid_write_single(w, &v->single);
    case K_EMPTY: return packetvoid_write_empty(w, &v->empty);
    case K_FRAME: return packetvoid_write_frame(w, &v->frame);
    case K_BATCH: return packetvoid_write_batch(w, &v->batch);
    default: return 0;
    }
}

/* body: the value's bits only (the generated read without its packet end);
   otherwise the public one-packet read. */
static int read_value(serialize_read_stream_t *r, value_t *v, int body)
{
    switch (v->kind)
    {
    case K_MIXED: return body ? schema_packetvoid_read_mixed_body_(r, &v->mixed) : packetvoid_read_mixed(r, &v->mixed);
    case K_TAGS: return body ? schema_packetvoid_read_tags_body_(r, &v->tags) : packetvoid_read_tags(r, &v->tags);
    case K_SINGLE: return body ? schema_packetvoid_read_single_body_(r, &v->single) : packetvoid_read_single(r, &v->single);
    case K_EMPTY: return body ? schema_packetvoid_read_empty_body_(r, &v->empty) : packetvoid_read_empty(r, &v->empty);
    case K_FRAME: return body ? schema_packetvoid_read_frame_body_(r, &v->frame) : packetvoid_read_frame(r, &v->frame);
    case K_BATCH: return body ? schema_packetvoid_read_batch_body_(r, &v->batch) : packetvoid_read_batch(r, &v->batch);
    default: return 0;
    }
}

static int mixed_equal(const packetvoid_mixed_t *got, const packetvoid_mixed_t *want)
{
    int i;
    if (got->type != want->type) return 0;
    if (want->type != PACKETVOID_MIXED_TYPE_PAYLOAD) return 1;
    if (got->as.payload.entries_count != want->as.payload.entries_count || got->as.payload.marker != want->as.payload.marker) return 0;
    /* every backing entry, used or not, holds what the payload's
       construction gave it: a read constructs the arm before decoding */
    for (i = 0; i < 2; ++i)
        if (got->as.payload.entries[i].retries != want->as.payload.entries[i].retries) return 0;
    return 1;
}

static int value_equal(const value_t *got, const value_t *want)
{
    int i;
    switch (want->kind)
    {
    case K_MIXED: return mixed_equal(&got->mixed, &want->mixed);
    case K_TAGS: return got->tags.type == want->tags.type;
    case K_SINGLE: return got->single.type == want->single.type;
    case K_EMPTY: return got->empty.type == want->empty.type;
    case K_FRAME:
        return got->frame.lead == want->frame.lead && got->frame.tail == want->frame.tail &&
               mixed_equal(&got->frame.choice, &want->frame.choice);
    case K_BATCH:
        if (got->batch.values_count != want->batch.values_count) return 0;
        for (i = 0; i < want->batch.values_count; ++i)
            if (!mixed_equal(&got->batch.values[i], &want->batch.values[i])) return 0;
        return 1;
    default: return 0;
    }
}

/* A pin in a padded, aligned allocation; its logical length stays exact. */
static uint64_t g_pin_words[16];

static int load(const char *dir, const char *name, int64_t *bytes)
{
    char path[512];
    FILE *f;
    size_t n;
    snprintf(path, sizeof(path), "%.300s/%.120s.bin", dir, name);
    memset(g_pin_words, 0, sizeof(g_pin_words));
    f = fopen(path, "rb");
    if (!f) FAIL("cannot open %.200s", path);
    n = fread(g_pin_words, 1, sizeof(g_pin_words), f);
    fclose(f);
    if (n >= sizeof(g_pin_words)) FAIL("%.200s is longer than the driver buffer", path);
    *bytes = (int64_t) n;
    return 1;
}

static int accept_case(const char *dir, const char *name, const char *type, int64_t bits)
{
    const unsigned char *pin = (const unsigned char *) g_pin_words;
    uint64_t out_words[8];
    value_t want, got;
    serialize_write_stream_t w;
    serialize_read_stream_t r;
    int64_t bytes;
    if (!build(name, &want)) FAIL("unknown accept case");
    if (want.kind != kind_of(type)) FAIL("case type %s, the driver builds another", type);
    if (!load(dir, name, &bytes)) return 0;
    if (g_write)
    {
        memset(out_words, 0, sizeof(out_words));
        serialize_write_stream_init(&w, (uint8_t *) out_words, sizeof(out_words));
        if (!write_value(&w, &want)) FAIL("write refused");
        if (serialize_write_bits_processed(&w) != bits)
            FAIL("write has %lld bits, want %lld", (long long) serialize_write_bits_processed(&w), (long long) bits);
        serialize_write_flush(&w);
        if (serialize_write_bytes_processed(&w) != bytes || memcmp(out_words, pin, (size_t) bytes) != 0)
            FAIL("write bytes differ from the pin");
    }
    if (g_read)
    {
        dirty(&got, want.kind);
        serialize_read_stream_init(&r, pin, bytes);
        if (!read_value(&r, &got, 1)) FAIL("read refused");
        if (serialize_read_bits_processed(&r) != bits)
            FAIL("read has %lld bits, want %lld", (long long) serialize_read_bits_processed(&r), (long long) bits);
        if (!value_equal(&got, &want)) FAIL("read value differs");
        /* The public read is the one-packet read: the same body, then the
           packet end consumes the final byte's zero padding (SPEC §5). */
        dirty(&got, want.kind);
        serialize_read_stream_init(&r, pin, bytes);
        if (!read_value(&r, &got, 0)) FAIL("one-packet read refused");
        if (serialize_read_bits_processed(&r) != bytes * 8)
            FAIL("one-packet read has %lld bits, want %lld", (long long) serialize_read_bits_processed(&r), (long long) (bytes * 8));
        if (!value_equal(&got, &want)) FAIL("one-packet read value differs");
    }
    return 1;
}

static int refuse_case(const char *dir, const char *name, const char *type)
{
    value_t got;
    serialize_read_stream_t r;
    int64_t bytes;
    enum kind kind = kind_of(type);
    if (kind == K_UNKNOWN) FAIL("unknown type %s", type);
    if (!load(dir, name, &bytes)) return 0;
    if (g_read)
    {
        dirty(&got, kind);
        serialize_read_stream_init(&r, (const uint8_t *) g_pin_words, bytes);
        if (read_value(&r, &got, 0)) FAIL("read accepted");
    }
    return 1;
}

int main(int argc, char **argv)
{
    char path[512], line[512];
    FILE *cases;
    if (argc == 3 && strcmp(argv[2], "read") == 0) g_write = 0;
    else if (argc == 3 && strcmp(argv[2], "write") == 0) g_read = 0;
    else if (argc != 2)
    {
        fprintf(stderr, "usage: %s <testdata/wire/packet-void> [read|write]\n", argv[0]);
        return 2;
    }
    /* The generated surface beside the wire: tag extents, debug names and
       maximum sizes. A miss exits non-zero, which tools/casegate holds red. */
    if (PACKETVOID_MIXED_TYPE_MAX != 2 || PACKETVOID_TAGS_TYPE_MAX != 2 || PACKETVOID_SINGLE_TYPE_MAX != 1 ||
        PACKETVOID_EMPTY_TYPE_MAX != 0 ||
        strcmp(packetvoid_enum_name_mixed_type(PACKETVOID_MIXED_TYPE_PING), "Ping") != 0 ||
        strcmp(packetvoid_enum_name_tags_type(PACKETVOID_TAGS_TYPE_PONG), "Pong") != 0 ||
        PACKETVOID_MIXED_MAX_BITS != 23 || PACKETVOID_TAGS_MAX_BITS != 2 || PACKETVOID_SINGLE_MAX_BITS != 1 ||
        PACKETVOID_EMPTY_MAX_BITS != 0)
    {
        printf("FAILED: packet-void tag extents, debug names or maximum bits\n");
        return 1;
    }
    snprintf(path, sizeof(path), "%.400s/cases.txt", argv[1]);
    cases = fopen(path, "r");
    if (!cases)
    {
        fprintf(stderr, "cannot open %s\n", path);
        return 2;
    }
    while (fgets(line, sizeof(line), cases))
    {
        char name[128], type[64], verdict[64], bits[64];
        int fields = sscanf(line, " %127s %63s %63s %63s", name, type, verdict, bits);
        int ok;
        if (fields < 1 || name[0] == '#') continue;
        g_why[0] = 0;
        if (fields < 4) { snprintf(g_why, sizeof(g_why), "a case line needs <name> <type> <verdict> <bits>"); ok = 0; }
        else if (strcmp(verdict, "accept") == 0) ok = accept_case(argv[1], name, type, strtoll(bits, NULL, 10));
        else if (strcmp(verdict, "refuse") == 0) ok = refuse_case(argv[1], name, type);
        else { snprintf(g_why, sizeof(g_why), "verdict %s", verdict); ok = 0; }
        if (ok) printf("case %s ok\n", name);
        else printf("case %s FAILED: %s\n", name, g_why);
    }
    fclose(cases);
    return 0;
}
