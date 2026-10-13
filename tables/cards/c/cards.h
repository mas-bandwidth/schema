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

/* cards_record_clear releases every allocation a Record owns. */
void cards_record_clear(CardsRecord *record);

/* cards_free_buf releases a buffer handed out by this facade. */
void cards_free_buf(void *buf);

#ifdef __cplusplus
}
#endif

#endif /* CARDS_C_CARDS_H */
