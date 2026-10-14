/* cards.h — the C owning facade for a typed work card (docs/SPEC-TABLES.md
   §2). It is the C spelling of the Go facade tables/cards/cards.go, parse.go
   and render.go: a Record that keeps its own defensive copy of the wire,
   checked writes that refuse before publishing, the two customer ceilings
   the schema deliberately leaves unbounded (§2.5, §2.9), and the brief text
   parsed once and rendered many times. The generated Card codec
   (CardTable.h) answers the wire and text forms; this facade is the thin
   owning layer a customer imports.
   The Go facade's calls, in C's own spelling:
     Load(wire)          -> cards_load(...)
     FromJSON(text)      -> cards_from_json(...)
     ToJSON(record)      -> cards_to_json(...)   (free with cards_free_buf)
     (r *Record) Wire()  -> cards_wire(...)     (free with cards_free_buf)
     Parse(text)         -> cards_parse(...)
     Render(record)      -> cards_render(...)   (free with cards_free_buf)
   A function returns 1 on success and 0 on refusal, writing the reason into
   the caller's error buffer. Every allocating call states its free:
   cards_record_clear releases a Record, cards_free_buf releases a buffer
   handed out by cards_to_json, cards_wire or cards_render. */
#ifndef CARDS_C_CARDS_H
#define CARDS_C_CARDS_H

#include <stddef.h>
#include <stdint.h>

#include "CardTable.h"

#ifdef __cplusplus
extern "C" {
#endif

/* MaxBodyBytes is the customer ceiling on one card's opaque body. The schema
   declares body *string with no bound (docs/SPEC-TABLES.md §2.5); this facade
   refuses a larger body before any bytes are published. */
#define CARDS_MAX_BODY_BYTES 65536

/* MaxListEntries is the customer ceiling on each of a card's collection
   fields (paths/new/shared/needs/extra/consumers), which the schema leaves
   unbounded (docs/SPEC-TABLES.md §2.9). */
#define CARDS_MAX_LIST_ENTRIES 512

/* Record owns a private defensive copy of the wire and the measured region
   that backs the loaded card. The wire is the caller's bytes copied, and
   cards_wire returns a fresh copy (docs/SPEC-TABLES.md §6.2). */
typedef struct CardsRecord {
    uint8_t *wire;
    int64_t wire_len;
    uint8_t *region;
    int64_t region_len;
    const Card *root;
    TableReport report;
} CardsRecord;

/* cards_load reads wire into a Record using the generated measured load
   (docs/SPEC-TABLES.md §6.2). Malformed wire refuses; tolerant events ride
   in the report. The ORIGINAL wire is copied, never rewritten. */
int cards_load(const uint8_t *wire, int64_t wire_len, CardsRecord *out,
               TableReport *report, char *err, size_t err_cap);

/* cards_from_json builds a Record from JSON text through the generated
   builder and text read (docs/SPEC-TABLES.md §16), then the generated save.
   Unknown, mismatched, clamped and malformed inputs refuse, as do the
   customer bounds. A refused write publishes nothing and mutates nothing. */
int cards_from_json(const uint8_t *text, int64_t text_len, CardsRecord *out,
                    char *err, size_t err_cap);

/* cards_to_json writes the record's known-field JSON text. The caller owns
   the returned buffer and frees it with cards_free_buf. */
int cards_to_json(const CardsRecord *record, uint8_t **out, int64_t *out_len,
                  char *err, size_t err_cap);

/* cards_wire returns a defensive copy of the record's wire bytes. The caller
   owns the returned buffer and frees it with cards_free_buf. */
int cards_wire(const CardsRecord *record, uint8_t **out, int64_t *out_len,
               char *err, size_t err_cap);

/* cards_parse reads admitted brief text and publishes a typed Record only on
   complete checked success (docs/SPEC-TABLES.md §2, §6.2). */
int cards_parse(const uint8_t *text, int64_t text_len, CardsRecord *out,
                char *err, size_t err_cap);

/* cards_render returns the record's informational brief text. The caller owns
   the returned buffer and frees it with cards_free_buf. */
int cards_render(const CardsRecord *record, uint8_t **out, int64_t *out_len,
                 char *err, size_t err_cap);

/* CARDS_MAX_RETAIN_BYTES and CARDS_MAX_RETAIN_IDS are the customer ceilings
   on the retained-record store (docs/SPEC-TABLES.md §6.6); Upgrade fixes them
   and never grows. */
#define CARDS_MAX_RETAIN_BYTES (1 << 20)
#define CARDS_MAX_RETAIN_IDS 4096

/* cards_retain_load/measure/save are the generated retained round trip,
   re-published by retain_bridge.c from the file-local generated functions
   (docs/SPEC-TABLES.md §6.6). The caller owns the region, retain bytes and
   retain id list. */
const Card *cards_retain_load(uint8_t *region, int64_t region_bytes,
                              const uint8_t *wire, int64_t wire_bytes,
                              TableRetain *retain, TableReport *report);
int64_t cards_retain_measure(const Card *root, TableRetain *retain);
int64_t cards_retain_save(const Card *root, TableRetain *retain,
                          uint8_t *buffer, int64_t capacity,
                          TableReport *report);

/* cards_upgrade rewrites wire through the generated retained region path
   (tla/CardVersioning.tla NoFieldLost, BodyUnchanged, RefusalKeepsOriginal;
   docs/SPEC-TABLES.md §6.6): every field the current declaration cannot name
   is kept at the end of its own body. Any loss event returns 0 with a named
   error and no output; the input is never mutated. */
int cards_upgrade(const uint8_t *wire, int64_t wire_len, uint8_t **out,
                  int64_t *out_len, TableReport *report, char *err,
                  size_t err_cap);

/* cards_upgrade_ex is the retention seam Upgrade fixes at the customer
   capacities; the refusal probe drives smaller caller-owned storage through
   the same path so a tiny capacity can be shown to name retain-lost while the
   input is preserved. */
int cards_upgrade_ex(const uint8_t *wire, int64_t wire_len, uint8_t **out,
                     int64_t *out_len, TableReport *report, int64_t retain_bytes,
                     int retain_ids, char *err, size_t err_cap);

/* cards_task_hash writes 64 lowercase hex digits plus a terminator into
   out_hex for the record's task fields (DESIGN L3; edit.go). A lossy record
   is refused, never hashed. */
int cards_task_hash(const CardsRecord *record, uint8_t out_hex[65], char *err,
                    size_t err_cap);

/* The six Set* edits replace one field atomically (DESIGN L4; edit.go §139).
   Each validates through the shared policy, rebuilds independently and
   publishes only on complete success; a refusal leaves the wire byte-equal. */
int cards_set_needs(CardsRecord *record, const char *const *values, int count,
                    char *err, size_t err_cap);
int cards_set_paths(CardsRecord *record, const char *const *values, int count,
                    char *err, size_t err_cap);
int cards_set_tier(CardsRecord *record, Tier tier, char *err, size_t err_cap);
int cards_set_priority(CardsRecord *record, Priority priority, char *err,
                       size_t err_cap);
int cards_set_column(CardsRecord *record, Column column, char *err,
                     size_t err_cap);
int cards_set_holder(CardsRecord *record, const char *holder, char *err,
                     size_t err_cap);

/* cards_view writes the unit's vocabulary, one line per field of every table,
   tables in the generated view's order and fields in declaration order
   (DESIGN L4; docs/SPEC-TABLES.md §8). The caller owns the buffer and frees
   it with cards_free_buf. */
int cards_view(uint8_t **out, int64_t *out_len, char *err, size_t err_cap);

/* cards_record_clear releases every allocation a Record owns. */
void cards_record_clear(CardsRecord *record);

/* cards_free_buf releases a buffer handed out by this facade. */
void cards_free_buf(void *buf);

#ifdef __cplusplus
}
#endif

#endif /* CARDS_C_CARDS_H */
