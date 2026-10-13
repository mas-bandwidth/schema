/* driver.c — the C leg of the card conformance set
   (tables/cards/conformance/FORMAT.md). It answers the driver contract
   test/conformance/README.md states — `<driver> <manifest> list` and
   `<driver> <manifest> <surface> <outdir>` — over the five surfaces this
   card owns (load, save, render, parse, bounds), through the facade in
   tables/cards/c/cards.c. The upgrade, edit, taskhash and view surfaces
   belong to a later card and are answered absent (exit 2). It is the
   concrete side of tla/CardHops.tla: every language's driver must answer
   the same bytes.
   A test-only environment switch, CARDS_C_SABOTAGE=1, changes one rendered
   byte so the negative control can prove the render surface goes red while
   the other four stay green. */
#define _POSIX_C_SOURCE 200809L
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <sys/stat.h>

#include "cards.h"

static const char *const kSurfaces[5] = {"load", "save", "render", "parse", "bounds"};

typedef struct Case {
    char *surface;
    char *name;
    char *input;
} Case;

static uint8_t *read_file(const char *path, int64_t *len) {
    FILE *f = fopen(path, "rb");
    uint8_t *buf;
    long n;
    if (f == NULL) return NULL;
    fseek(f, 0, SEEK_END);
    n = ftell(f);
    fseek(f, 0, SEEK_SET);
    buf = (uint8_t *)malloc((size_t)(n > 0 ? n : 1));
    if (buf == NULL) {
        fclose(f);
        return NULL;
    }
    if (n > 0 && fread(buf, 1, (size_t)n, f) != (size_t)n) {
        free(buf);
        fclose(f);
        return NULL;
    }
    fclose(f);
    *len = n;
    return buf;
}

static int write_out(const char *outdir, const char *name, const char *ext,
                     const uint8_t *data, int64_t len) {
    char path[1024];
    FILE *f;
    snprintf(path, sizeof(path), "%s/%s.%s", outdir, name, ext);
    f = fopen(path, "wb");
    if (f == NULL) return 0;
    if (len > 0 && fwrite(data, 1, (size_t)len, f) != (size_t)len) {
        fclose(f);
        return 0;
    }
    fclose(f);
    return 1;
}

static int refuse(const char *outdir, const char *name, const char *cls) {
    char buf[128];
    int n = snprintf(buf, sizeof(buf), "%s\n", cls);
    return write_out(outdir, name, "refused", (const uint8_t *)buf, n);
}

/* refusal_class maps a facade error to one class of the closed list
   FORMAT.md fixes, never free text. */
static void refusal_class(const char *err, char *out, size_t cap) {
    if (strstr(err, "past the 65536-byte ceiling") != NULL) {
        snprintf(out, cap, "body-bound 65536");
        return;
    }
    if (strstr(err, "past the 512-entry ceiling") != NULL) {
        /* "cards: <field> has N entries, past the 512-entry ceiling ..." */
        const char *p = strstr(err, "cards: ");
        const char *h;
        size_t n = 0;
        p = p != NULL ? p + 7 : err;
        h = strstr(p, " has ");
        if (h != NULL) n = (size_t)(h - p);
        if (n >= cap - 16) n = cap - 17;
        {
            char field[64];
            if (n >= sizeof(field)) n = sizeof(field) - 1;
            memcpy(field, p, n);
            field[n] = '\0';
            snprintf(out, cap, "list-bound %s 512", field);
        }
        return;
    }
    if (strstr(err, "stranded header key") != NULL) {
        const char *a = strchr(err, '"');
        const char *b = a != NULL ? strchr(a + 1, '"') : NULL;
        if (a != NULL && b != NULL) {
            char key[64];
            size_t n = (size_t)(b - a - 1);
            if (n >= sizeof(key)) n = sizeof(key) - 1;
            memcpy(key, a + 1, n);
            key[n] = '\0';
            snprintf(out, cap, "stranded-key %s", key);
            return;
        }
    }
    snprintf(out, cap, "malformed");
}

static int write_report(const char *outdir, const char *name, const TableReport *r) {
    char buf[512];
    int n = snprintf(buf, sizeof(buf),
                     "Retained=%d\nRetainLost=%d\nWidened=%d\nUnknown=%d\n"
                     "KindMismatch=%d\nClamped=%d\nDuplicate=%d\nMalformed=%s\n",
                     r->retained, r->retain_lost, r->widened, r->unknown,
                     r->kind_mismatch, r->clamped, r->duplicate,
                     r->malformed ? "true" : "false");
    return write_out(outdir, name, "report", (const uint8_t *)buf, n);
}

static Case *read_manifest(const char *path, int *count) {
    int64_t len = 0;
    uint8_t *text = read_file(path, &len);
    Case *cases = NULL;
    int n = 0, cap = 0;
    int64_t pos = 0;
    if (text == NULL) {
        *count = -1;
        return NULL;
    }
    while (pos < len) {
        int64_t start = pos;
        char line[2048];
        int64_t ln;
        char *f1, *f2, *f3, *save = NULL;
        while (pos < len && text[pos] != '\n') pos++;
        ln = pos - start;
        if (ln >= (int64_t)sizeof(line)) ln = sizeof(line) - 1;
        memcpy(line, text + start, (size_t)ln);
        line[ln] = '\0';
        if (pos < len) pos++;
        /* trim */
        {
            char *s = line;
            while (*s == ' ' || *s == '\t' || *s == '\r') s++;
            if (*s == '\0' || *s == '#') continue;
            memmove(line, s, strlen(s) + 1);
        }
        f1 = strtok_r(line, " \t\r\n", &save);
        f2 = strtok_r(NULL, " \t\r\n", &save);
        f3 = strtok_r(NULL, " \t\r\n", &save);
        if (f1 == NULL || f2 == NULL || f3 == NULL) continue;
        if (n >= cap) {
            cap = cap > 0 ? cap * 2 : 16;
            cases = (Case *)realloc(cases, (size_t)cap * sizeof(Case));
        }
        cases[n].surface = strdup(f1);
        cases[n].name = strdup(f2);
        cases[n].input = strdup(f3);
        n++;
    }
    free(text);
    *count = n;
    return cases;
}

static int is_surface(const char *s) {
    int i;
    for (i = 0; i < 5; i++) {
        if (strcmp(s, kSurfaces[i]) == 0) return 1;
    }
    return 0;
}

static int one(const char *surface, const Case *c, const char *outdir) {
    char err[512];
    err[0] = '\0';
    if (strcmp(surface, "load") == 0) {
        int64_t len = 0;
        uint8_t *wire = read_file(c->input, &len);
        CardsRecord rec;
        TableReport report;
        uint8_t *json = NULL;
        int64_t json_len = 0;
        int ok;
        if (wire == NULL) return 0;
        ok = cards_load(wire, len, &rec, &report, err, sizeof(err));
        free(wire);
        if (!ok) return 0;
        if (!cards_to_json(&rec, &json, &json_len, err, sizeof(err))) {
            cards_record_clear(&rec);
            return 0;
        }
        ok = write_out(outdir, c->name, "json", json, json_len);
        cards_free_buf(json);
        if (ok) ok = write_report(outdir, c->name, &report);
        cards_record_clear(&rec);
        return ok;
    }
    if (strcmp(surface, "save") == 0 || strcmp(surface, "bounds") == 0) {
        int64_t len = 0;
        uint8_t *text = read_file(c->input, &len);
        CardsRecord rec;
        char cls[128];
        uint8_t *wire = NULL;
        int64_t wire_len = 0;
        int ok;
        if (text == NULL) return 0;
        if (!cards_from_json(text, len, &rec, err, sizeof(err))) {
            free(text);
            refusal_class(err, cls, sizeof(cls));
            return refuse(outdir, c->name, cls);
        }
        free(text);
        if (!cards_wire(&rec, &wire, &wire_len, err, sizeof(err))) {
            cards_record_clear(&rec);
            return 0;
        }
        ok = write_out(outdir, c->name, "wire", wire, wire_len);
        cards_free_buf(wire);
        cards_record_clear(&rec);
        return ok;
    }
    if (strcmp(surface, "render") == 0) {
        int64_t len = 0;
        uint8_t *wire = read_file(c->input, &len);
        CardsRecord rec;
        TableReport report;
        uint8_t *text = NULL;
        int64_t text_len = 0;
        int ok;
        const char *sab;
        if (wire == NULL) return 0;
        ok = cards_load(wire, len, &rec, &report, err, sizeof(err));
        free(wire);
        if (!ok) return 0;
        if (!cards_render(&rec, &text, &text_len, err, sizeof(err))) {
            cards_record_clear(&rec);
            return 0;
        }
        cards_record_clear(&rec);
        sab = getenv("CARDS_C_SABOTAGE");
        if (sab != NULL && strcmp(sab, "1") == 0 && text_len > 0) {
            /* the negative control: change one rendered byte */
            text[0] = (uint8_t)(text[0] == 'R' ? 'S' : 'R');
        }
        ok = write_out(outdir, c->name, "txt", text, text_len);
        cards_free_buf(text);
        return ok;
    }
    if (strcmp(surface, "parse") == 0) {
        int64_t len = 0;
        uint8_t *text = read_file(c->input, &len);
        CardsRecord rec;
        char cls[128];
        uint8_t *wire = NULL;
        int64_t wire_len = 0;
        int ok;
        if (text == NULL) return 0;
        if (!cards_parse(text, len, &rec, err, sizeof(err))) {
            free(text);
            refusal_class(err, cls, sizeof(cls));
            return refuse(outdir, c->name, cls);
        }
        free(text);
        if (!cards_wire(&rec, &wire, &wire_len, err, sizeof(err))) {
            cards_record_clear(&rec);
            return 0;
        }
        ok = write_out(outdir, c->name, "wire", wire, wire_len);
        cards_free_buf(wire);
        cards_record_clear(&rec);
        return ok;
    }
    return 0;
}

int main(int argc, char **argv) {
    const char *manifest, *verb;
    Case *cases;
    int count = 0, i;
    if (argc < 3) {
        fprintf(stderr, "usage: driver <manifest> list\n"
                        "       driver <manifest> <surface> <outdir>\n");
        return 2;
    }
    manifest = argv[1];
    verb = argv[2];
    if (strcmp(verb, "list") == 0) {
        for (i = 0; i < 5; i++) printf("%s\n", kSurfaces[i]);
        return 0;
    }
    if (argc < 4) {
        fprintf(stderr, "usage: driver <manifest> <surface> <outdir>\n");
        return 2;
    }
    if (!is_surface(verb)) return 2; /* upgrade, edit, taskhash, view: absent */
    cases = read_manifest(manifest, &count);
    if (cases == NULL) return 1;
    mkdir(argv[3], 0755);
    for (i = 0; i < count; i++) {
        if (strcmp(cases[i].surface, verb) != 0) continue;
        if (!one(verb, &cases[i], argv[3])) {
            fprintf(stderr, "driver: case %s failed\n", cases[i].name);
            return 1;
        }
    }
    return 0;
}
