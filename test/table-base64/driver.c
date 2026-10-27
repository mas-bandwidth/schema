// Shared C/C++ driver; all assertions and independent expectations live in Go.
#include "BytesTable.h"
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#ifdef __cplusplus
using namespace base64test;
#endif

static void hex(const void *data, size_t n)
{
    const unsigned char *p = (const unsigned char *) data;
    if (!n) { putchar('-'); }
    for (size_t i = 0; i < n; ++i) { printf("%02x", p[i]); }
}

int main(void)
{
    char line[131072], text[65536], written[65536];
    while (fgets(line, sizeof(line), stdin)) {
        size_t n = strcspn(line, "\r\n") / 2;
        for (size_t i = 0; i < n; ++i) {
            unsigned int c;
            if (sscanf(line + i * 2, "%2x", &c) != 1) { return 1; }
            text[i] = (char) c;
        }
        base64test_blob_t value;
        base64test_table_report_t report;
#ifndef __cplusplus
        memset(&report, 0, sizeof(report));
#endif
#ifdef __cplusplus
        // The bound probes read tolerantly, and the tolerant read is the
        // named one: the plain BlobFromJson is strict (SPEC-TABLES §16.11).
        BlobFromJsonTolerant(value, text, (int64_t) n, &report);
#else
        base64test_blob_from_json(&value, text, (int64_t) n, &report);
#endif
        if (report.malformed) { puts("1 0 0 - -"); continue; }
#ifdef __cplusplus
        int64_t size = BlobToJson(value, written, sizeof(written));
#else
        int64_t size = base64test_blob_to_json(&value, written, sizeof(written));
#endif
        if (size < 0) { return 1; }
        printf("0 %d %d ", report.clamped, report.kind_mismatch);
        hex(value.payload, (size_t) value.payload_length);
        putchar(' '); hex(written, (size_t) size); putchar('\n');
    }
    return 0;
}
