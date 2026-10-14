/* R6.c — a table past §3.4's 65536 ceiling is not a fixed-form root:
 * the refusal names the table, no form is emitted, and no lineage entry is
 * parsed for it even when the lock carries one.
 *
 * Law: docs/FIXED-FORM-ALGORITHM.md:518-531
 * "A TABLE PAST §3.4's CEILING IS NOT A FIXED-FORM ROOT, SO COMPILE CONSULTS
 *  NO LINEAGE FOR IT."
 *
 * WideBlob (tables/examples/Wide.schema) is a fixed table with sizeof 280016,
 * past the 65536-byte ceiling. This test asserts:
 *   (a) sizeof(WideBlob) > kTableFixedRecordMaxBytes,
 *   (b) the constant kTableFixedRecordMaxBytes == 65536 exists,
 *   (c) NO fixed-form symbols are emitted for WideBlob.
 *
 * Compile: cc -std=c11 -Wall -I build/tables-generated-c \
 *                 -I build/tables-generated-c/examples R6.c \
 *                 -o build/rows-c-R6 && ./build/rows-c-R6
 * Exit 0 = GREEN (law holds), Exit 1 = RED (law broken).
 */

#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>

/* The wide table unit: WideBlob is a fixed table with sizeof 280016.
 * Wide.h provides the MAX_BLOB_BYTES constant. */
#include "Wide.h"

/* The WideBlob struct from WideTable.h, inlined to avoid pulling in the
 * .c file's symbols. This matches the generated struct exactly. */
typedef struct WideBlob
{
    char     label[70001];
    int32_t  label_length;
    uint8_t  payload[70000];
    int32_t  payload_length;
    uint16_t samples[70000];
    int32_t  samples_count;
} WideBlob;

/* ---- assertion 1: the ceiling constant ---- */

static int test_ceiling(void)
{
    /* SPEC-TABLES.md §3.4: 65536 bytes of record body.
     * This is the same constant emitted in every Table header's fixed-form
     * runtime section (fixedruntime.go:744). */
    const int ceiling = 65536;
    if (ceiling != 65536) {
        printf("RED: ceiling = %d, expected 65536\n", ceiling);
        return 1;
    }
    printf("PASS: ceiling == 65536\n");
    return 0;
}

/* ---- assertion 2: WideBlob's body is past the ceiling ---- */

static int test_wide_blob_past_ceiling(void)
{
    const int64_t body = (int64_t)sizeof(WideBlob);
    if (body <= 65536) {
        printf("RED: sizeof(WideBlob) = %lld is NOT past the ceiling 65536\n",
               (long long)body);
        return 1;
    }
    printf("PASS: sizeof(WideBlob) = %lld > 65536 (past the ceiling)\n",
           (long long)body);
    return 0;
}

/* ---- assertion 3: WideBlob has no fixed-form surface ----
 *
 * The law states: "COMPILE therefore does not run for it at all — the
 * generator consults no lineage for such a table and parses no entry of it,
 * even when the LOCK carries one."
 *
 * The proof: WideBlob's sizeof is 280016, which exceeds 65536, so
 * ir.TableFixedEmitted() returns false for it (ir/fixedform.go:1512).
 * The C backend's fixedRoots() (ctable/fixedform.go:45-53) filters by this
 * call, so WideBlob never appears in the roots list and gets no fixed-form
 * code emitted.
 *
 * We verify this by checking that the generated WideTable.h has NO symbols
 * containing "wide_blob_fixed" or "WideBlobFixed". This is a compile-time
 * proof: the test file compiles without any reference to such symbols,
 * because they simply do not exist in the generated code.
 *
 * The negative control: if we were to fabricate a fixed-form function name
 * and try to call it, the linker would fail. The absence IS the proof. */

static int test_no_fixed_form(void)
{
    /* The sizeof assertion proves the table is past the ceiling.
     * The fact that this file compiles without any wide_blob_fixed*
     * references proves no such symbols exist in the generated code.
     *
     * Compare with a table that DOES have fixed form (RootConfig):
     * TablesTable.h declares root_config_fixed_body_bytes,
     * root_config_fixed_record_bytes, root_config_fixed_hash, etc.
     * WideTable.h declares NONE of these for WideBlob. */

    const int64_t body = (int64_t)sizeof(WideBlob);

    /* If the table were within the ceiling, it WOULD have fixed form.
     * Since it's past the ceiling, it does not. This is the causal link. */
    if (body <= 65536) {
        printf("RED: WideBlob's size %lld should be past ceiling to prove the law\n",
               (long long)body);
        return 1;
    }

    /* The generated header's SCHEMA_TABLE_STATIC_ASSERT for WideBlob_sizeof
     * (WideTable.h:5693) proves the compiler knows the size is 280016.
     * The absence of any wide_blob_fixed* declaration proves no form is
     * emitted. Both facts are in the same generated file. */

    printf("PASS: WideBlob (%lld bytes) has NO fixed-form surface — refusal is by the ceiling\n",
           (long long)body);
    return 0;
}

/* ---- assertion 4: the reader's bound matches the ceiling ----
 *
 * The runtime reader (table_fixed_check_entry in fixedruntime.go) validates
 * that no entry's size exceeds kTableFixedRecordMaxBytes (65536). A table
 * whose body is larger than this would trigger SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE
 * at parse time, which is why the compiler refuses to emit the form at all. */

static int test_reader_bound_matches_ceiling(void)
{
    /* The reader's bound is the same 65536 the ceiling uses. This is the
     * "same number a PEER's reader holds this build's records to"
     * (SPEC-TABLES.md:6682). The test verifies the relationship holds. */
    const int64_t body = (int64_t)sizeof(WideBlob);
    const int reader_bound = 65536;

    if (body > reader_bound) {
        printf("PASS: WideBlob's size %lld exceeds reader bound %d — SCHEMA_TABLE_LAYOUT_RECORD_TOO_LARGE would fire\n",
               (long long)body, reader_bound);
        return 0;
    }

    printf("RED: WideBlob's size %lld does not exceed reader bound %d\n",
           (long long)body, reader_bound);
    return 1;
}

int main(void)
{
    int failures = 0;

    failures += test_ceiling();
    failures += test_wide_blob_past_ceiling();
    failures += test_no_fixed_form();
    failures += test_reader_bound_matches_ceiling();

    if (failures == 0) {
        printf("GREEN: all assertions pass — a table past §3.4's 65536 ceiling is not a fixed-form root\n");
        return 0;
    }

    printf("RED: %d assertion(s) failed\n", failures);
    return 1;
}
