/* cell c/C9: union tag past arms -> None
   law: FIXED-FORM-ALGORITHM.md:945, SPEC-TABLES.md:6736
   production entrypoint: V1Table.h:6873 schema_tblv1_cfg_fixed_clamp_body_
   call site: V1Table.h:7524 cfg_fixed_load -> schema_tblv1_cfg_fixed_clamp_ -> clamp_body_
   writes a Cfg record with effect.type=WARD, corrupts the tag to 3 (past EFFECT_TYPE_MAX=2),
   loads through cfg_fixed_load, asserts the tag clamps to EFFECT_TYPE_NONE and
   the report counts one clamped event. */

#include "V1Table.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

int main(void)
{
    int failures = 0;

    int64_t need = cfg_fixed_measure(1);

    uint8_t * buf = (uint8_t *) malloc((size_t) need);
    if (!buf) { fprintf(stderr, "FAIL: malloc\n"); return 1; }

    Cfg value;
    memset(&value, 0, sizeof(value));
    cfg_reset(&value);
    value.effect.type = EFFECT_TYPE_WARD;
    value.effect.as.ward.charge = 3.14f;

    int64_t written = cfg_fixed_save(&value, 1, buf, need);
    if (written < 0) { fprintf(stderr, "FAIL: cfg_fixed_save\n"); free(buf); return 1; }

    /* The record body starts at body_offset + 8 (after the per-record hash).
       Effect.type is at body byte 137 (V1Table.h:6763). */
    int64_t body_offset = kTableFixedHeaderBytes + 4 + (int64_t) sizeof(cfg_fixed_layout);
    int64_t record_start = body_offset; /* start of this record (includes hash) */
    int64_t tag_offset = record_start + 8 + 137; /* skip hash, then body[137] = Effect.type */

    /* Verify the tag is WARD(2) before corruption */
    if (buf[tag_offset] != 2) { fprintf(stderr, "BUG: expected tag=2 before corruption, got %d\n", buf[tag_offset]); free(buf); return 1; }

    /* Corrupt the tag to 3 (past EFFECT_TYPE_MAX=2) */
    buf[tag_offset] = 3;

    /* Load it back */
    Cfg loaded;
    memset(&loaded, 0, sizeof(loaded));
    TableReport report;
    memset(&report, 0, sizeof(report));

    int64_t n = cfg_fixed_load(&loaded, 1, buf, written, NULL, 0, NULL, &report);
    if (n != 1) { fprintf(stderr, "FAIL: cfg_fixed_load returned %ld\n", (long)n); free(buf); return 1; }

    /* Assert: tag past max -> None */
    if (loaded.effect.type != EFFECT_TYPE_NONE) {
        fprintf(stderr, "FAIL: union tag past arms -> None: expected type=%d, got %d\n",
                EFFECT_TYPE_NONE, loaded.effect.type);
        failures++;
    } else {
        printf("PASS: union tag past arms -> None (tag 3 -> EFFECT_TYPE_NONE)\n");
    }

    /* Assert: clamped counter incremented */
    if (report.clamped < 1) {
        fprintf(stderr, "FAIL: expected clamped >= 1, got %d\n", report.clamped);
        failures++;
    } else {
        printf("PASS: clamped counter = %d\n", report.clamped);
    }

    /* Assert: arm payload is undisturbed (None arm has no payload) */
    if (loaded.effect.as.boost.power != 0) {
        fprintf(stderr, "FAIL: None arm payload should be zero, got %d\n",
                loaded.effect.as.boost.power);
        failures++;
    } else {
        printf("PASS: None arm payload is zero\n");
    }

    free(buf);

    if (failures) {
        fprintf(stderr, "FAIL: %d assertion(s) failed\n", failures);
        return 1;
    }
    return 0;
}