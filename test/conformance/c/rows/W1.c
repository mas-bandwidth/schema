/* W1.c: test that write slack is template zeros for c/W1.
 * 
 * LAW (docs/FIXED-FORM-ALGORITHM.md §3.1): Text and array slack — the writer 
 * writes `length` units and `count` elements onto the zeroed template and 
 * stops, never the caller's leftovers or an element's default image.
 *
 * This test verifies that when saving a Fleet record with default values, 
 * only the live bytes are written to the wire, and any slack (unused capacity)
 * remains zeroed in the template.
 */

#include "W1Table.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#define EXPECT(a, b, msg) do { \
    if ((a) != (b)) { \
        fprintf(stderr, "FAIL: %s: expected %lld, got %lld\n", msg, (long long)(b), (long long)(a)); \
        return 1; \
    } \
} while (0)

int main(void)
{
    /* Create a Fleet with all default values */
    Fleet fleet;
    fleet_reset(&fleet);
    
    /* Allocate a buffer - larger than any expected wire size */
    uint8_t *buffer = (uint8_t *)malloc(256);
    if (!buffer) {
        fprintf(stderr, "FAIL: malloc\n");
        return 1;
    }
    memset(buffer, 0xFF, 256);  /* Fill with non-zero to catch any extra writes */
    
    /* Save using table_graph_save which writes the full wire format */
    int64_t saved = table_graph_save(NULL, &fleet, &schema_tblw1_fleet_node_type_, buffer, 256, table_default_allocator());
    if (saved < 0) {
        fprintf(stderr, "FAIL: table_graph_save\n");
        free(buffer);
        return 1;
    }
    
    /* Compare against the golden w1_fleet_default.bin */
    FILE *golden = fopen("testdata/wire/tables/w1_fleet_default.bin", "rb");
    if (!golden) {
        fprintf(stderr, "FAIL: cannot open golden\n");
        free(buffer);
        return 1;
    }
    
    uint8_t golden_buf[256];
    size_t golden_read = fread(golden_buf, 1, sizeof(golden_buf), golden);
    fclose(golden);
    
    /* Verify that the saved size matches the golden */
    if (saved != (int64_t)golden_read) {
        fprintf(stderr, "FAIL: saved size %lld != golden %zu\n", (long long)saved, golden_read);
        free(buffer);
        return 1;
    }
    
    /* Verify that the saved bytes match the golden */
    if (memcmp(buffer, golden_buf, (size_t)saved) != 0) {
        fprintf(stderr, "FAIL: saved bytes do not match golden\n");
        free(buffer);
        return 1;
    }
    
    /* Verify slack bytes were not written (still 0xFF) */
    if (buffer[saved] != 0xFF) {
        fprintf(stderr, "FAIL: slack byte at offset %lld was written (expected 0xFF)\n", (long long)saved);
        free(buffer);
        return 1;
    }
    
    free(buffer);
    
    /* PASS: all assertions passed */
    return 0;
}
