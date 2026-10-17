/* sha256.h — a small SHA-256 for the task hash (DESIGN L3).
   The C standard library has no digest and the card licenses no dependency,
   so the facade carries FIPS 180-4 §6.2 directly. The test checks the three
   published vectors before any hash is trusted. */
#ifndef CARDS_C_SHA256_H
#define CARDS_C_SHA256_H

#include <stddef.h>
#include <stdint.h>

/* cards_sha256 writes 32 bytes into out for data[0..len). */
void cards_sha256(uint8_t out[32], const uint8_t *data, size_t len);

#endif /* CARDS_C_SHA256_H */
