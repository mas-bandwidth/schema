/* F9: batch_too_large — the fixed form refuses a batch past the caller's capacity
 * (docs/FIXED-FORM-ALGORITHM.md step 6: rest/record_bytes > capacity => REFUSE).
 * The test constructs a two-record fixed-form wire from V1's Cfg, then loads it
 * with capacity=1.  The refusal must name batch_too_large; with capacity >= 2
 * the same wire must load cleanly (the behavioural control). */
#include "V1Table.h"
#include <stdio.h>
#include <stdlib.h>

int main(void)
{
    TableReport report;
    Cfg values[2];
    uint8_t *buffer;
    int64_t need, loaded;
    int fail = 0;

    cfg_reset(&values[0]);
    cfg_reset(&values[1]);

    need = cfg_fixed_measure(2);
    buffer = (uint8_t *)malloc((size_t)need);
    if (!buffer) { printf("FAIL: allocation\n"); return 1; }
    if (cfg_fixed_save(values, 2, buffer, need) != need)
    {
        printf("FAIL: save\n"); free(buffer); return 1;
    }

    /* capacity=1, wire has 2 records => batch_too_large */
    memset(&report, 0, sizeof(report));
    cfg_reset(&values[0]);
    loaded = cfg_fixed_load(values, 1, buffer, need, NULL, 0, NULL, &report);
    free(buffer);

    if (loaded != -1 || report.refused != 1)
    {
        printf("FAIL: batch_too_large not refused (loaded=%lld refused=%d)\n",
               (long long)loaded, report.refused);
        fail++;
    }
    else { printf("batch_too_large: two records, capacity 1 => refused (loaded=%lld, refused=%d)\n", (long long)loaded, report.refused); }

    /* control: same wire, capacity=2 => must load 2 records */
    buffer = (uint8_t *)malloc((size_t)need);
    if (!buffer) { printf("FAIL: allocation\n"); return 1; }
    if (cfg_fixed_save(values, 2, buffer, need) != need)
    { printf("FAIL: save\n"); free(buffer); return 1; }
    memset(&report, 0, sizeof(report));
    cfg_reset(&values[0]); cfg_reset(&values[1]);
    loaded = cfg_fixed_load(values, 2, buffer, need, NULL, 0, NULL, &report);
    free(buffer);
    if (loaded != 2 || report.refused != 0)
    {
        printf("FAIL: control: capacity 2 should load 2 records (got loaded=%lld refused=%d)\n",
               (long long)loaded, report.refused);
        fail++;
    }
    else { printf("control: two records, capacity 2 => loaded %lld, refused %d\n", (long long)loaded, report.refused); }

    return fail ? 1 : 0;
}