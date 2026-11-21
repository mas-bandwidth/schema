package ctable

import (
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/testproc"
)

// The file, fixed and JSON read rules of the 2026-11-21 review for the C leg
// (c.md C-4; verify-values.md Across legs rules 1 to 3 and the counted-array
// rule; cpp.md CPP-1 checked on C). Every must-refuse case has an accepted
// twin, so a reader that cannot read the field at all fails the twin.
//
// The program is compiled with NDEBUG: the leg's write checks are debug
// assertions only (C is an assert leg), so a release writer puts a count below
// the declared Min on the wire, and every read check must still hold.
const cStrictReadsSchema = `package probe

fixed table Root {
	arr [2..4]uint8
	f   float32 = 0.5 | min = -1.0, max = 1.0, resolution = 0.01
	g   float32 = 0.0 | min = 0.0, max = 0.1, resolution = 0.001
}
`

const cStrictReadsMain = `#include "ProbeTable.h"
#include <pthread.h>
#include <stdio.h>
#include <stdint.h>
#include <stdlib.h>
#include <string.h>

static int failed = 0;
#define CHECK(name, x) do { if (x) { printf("PASS %s\n", name); } else { printf("FAIL %s: line %d: %s\n", name, __LINE__, #x); failed = 1; } } while (0)

static uint32_t bits_of(float f) { uint32_t b; memcpy(&b, &f, 4); return b; }

/* The one place the 4-byte little-endian pattern rides; -1 when it rides
   zero times or more than once. */
static int64_t find32(const uint8_t * p, int64_t n, uint32_t v)
{
    int64_t i, at = -1;
    for (i = 0; i + 4 <= n; ++i) {
        uint32_t got = (uint32_t)p[i] | ((uint32_t)p[i+1] << 8) | ((uint32_t)p[i+2] << 16) | ((uint32_t)p[i+3] << 24);
        if (got == v) { if (at >= 0) { return -1; } at = i; }
    }
    return at;
}

static void put32(uint8_t * p, uint32_t v) { p[0]=(uint8_t)v; p[1]=(uint8_t)(v>>8); p[2]=(uint8_t)(v>>16); p[3]=(uint8_t)(v>>24); }

/* CPP-1 on C: a form-1 file whose one unknown field is a chain of D unions,
   each arm a union (kind 15), loaded on a small stack. */
static uint8_t * chain_file(int64_t depth, int64_t * out)
{
    /* built back to front: U_0 = 00, U_k = 01 0F leb(len U_{k-1}) U_{k-1} */
    int64_t cap = depth * 8 + 64, len = 0, k, i;
    uint8_t * rev = (uint8_t *)malloc((size_t)cap);
    uint8_t * file;
    if (rev == NULL) { return NULL; }
    rev[len++] = 0x00;
    for (k = 0; k < depth; ++k) {
        uint8_t leb[10]; int nl = 0; uint64_t v = (uint64_t)len;
        do { uint8_t b = (uint8_t)(v & 0x7f); v >>= 7; if (v) { b |= 0x80; } leb[nl++] = b; } while (v);
        for (i = nl - 1; i >= 0; --i) { rev[len++] = leb[i]; }
        rev[len++] = 0x0F;
        rev[len++] = 0x01;
    }
    file = (uint8_t *)malloc((size_t)(len + 32));
    if (file == NULL) { free(rev); return NULL; }
    *out = 0;
    file[(*out)++] = 0x01;                 /* form 1 */
    file[(*out)++] = 0x01;                 /* ref 1 */
    file[(*out)++] = 0x0F;                 /* kind 15, a union */
    for (i = len - 1; i >= 0; --i) { file[(*out)++] = rev[i]; }
    file[(*out)++] = 0x00;                 /* the body's zero reference */
    for (i = 0; i < 8; ++i) { file[(*out)++] = (uint8_t)(0xA0 + i); } /* one id this reader does not declare */
    file[(*out)++] = 0x01;                 /* the id count */
    for (i = 0; i < 7; ++i) { file[(*out)++] = 0x00; }
    free(rev);
    return file;
}

struct chain_job { const uint8_t * wire; int64_t len; int ok; int refused; int unknown; };

static void * chain_run(void * arg)
{
    struct chain_job * job = (struct chain_job *)arg;
    Root v; TableReport r;
    root_reset(&v);
    memset(&r, 0, sizeof(r));
    job->ok = root_load(&v, job->wire, job->len, &r);
    job->refused = r.refused;
    job->unknown = r.unknown;
    return NULL;
}

static int chain_load(int64_t depth, struct chain_job * job)
{
    pthread_attr_t attr; pthread_t th; int64_t len = 0;
    uint8_t * wire = chain_file(depth, &len);
    if (wire == NULL) { return 0; }
    job->wire = wire; job->len = len;
    pthread_attr_init(&attr);
    pthread_attr_setstacksize(&attr, 256 * 1024); /* a job stack, far below the chain's unbounded need */
    if (pthread_create(&th, &attr, chain_run, job) != 0) { free(wire); return 0; }
    pthread_join(th, NULL);
    free(wire);
    return 1;
}

int main(void)
{
    static uint8_t wire[4096];
    static uint8_t other[4096];
    static uint8_t file[1 << 16];
    static TableFixedEntry plan[4096];
    Root v, w;
    Root back[4];
    TableReport r;
    int64_t n, m, need1, need2;
    int ok;

    /* A FRESH COUNTED ARRAY IS BORN AT ITS MIN (§3.3 counted arrays) */
    root_reset(&v);
    CHECK("fresh_counted_array_born_at_min", v.arr_count == 2 && v.arr[0] == 0 && v.arr[1] == 0);

    /* THE BIRTH VALUE IS ELIDED AND LANDS AGAIN AS THE BIRTH VALUE */
    root_reset(&v);
    n = root_save(&v, wire, sizeof(wire));
    root_reset(&w); w.arr_count = 4; w.arr[0] = 9;
    memset(&r, 0, sizeof(r));
    ok = root_load(&w, wire, n, &r);
    CHECK("birth_count_round_trips", n > 0 && ok && w.arr_count == 2 && w.arr[0] == 0 && w.arr[1] == 0);
    root_reset(&v); v.arr[1] = 5;
    m = root_save(&v, other, sizeof(other));
    root_reset(&w);
    memset(&r, 0, sizeof(r));
    ok = root_load(&w, other, m, &r);
    CHECK("birth_count_with_a_value_rides", m > n && ok && w.arr_count == 2 && w.arr[1] == 5);
    root_reset(&v); v.arr_count = 3; v.arr[2] = 6;
    m = root_save(&v, other, sizeof(other));
    root_reset(&w);
    memset(&r, 0, sizeof(r));
    ok = root_load(&w, other, m, &r);
    CHECK("count_past_birth_rides", m > n && ok && w.arr_count == 3 && w.arr[2] == 6);

    /* A COUNT BELOW MIN IS REFUSED ON THE FILE READ */
    root_reset(&v); v.arr_count = 1; v.arr[0] = 7;
    n = root_save(&v, wire, sizeof(wire));
    root_reset(&w);
    memset(&r, 0, sizeof(r));
    ok = root_load(&w, wire, n, &r);
    CHECK("file_count_below_min_refused", n > 0 && !ok && r.refused);
    root_reset(&v); v.arr[0] = 7; v.arr[1] = 8;
    n = root_save(&v, wire, sizeof(wire));
    root_reset(&w);
    memset(&r, 0, sizeof(r));
    ok = root_load(&w, wire, n, &r);
    CHECK("file_count_at_min_reads", ok && w.arr_count == 2 && w.arr[0] == 7 && w.arr[1] == 8);

    /* ... ON THE FIXED READ */
    root_reset(&v); v.arr_count = 1; v.arr[0] = 7;
    need1 = root_fixed_measure(1);
    n = root_fixed_save(&v, 1, file, sizeof(file));
    memset(&r, 0, sizeof(r));
    n = n == need1 ? root_fixed_load(back, 4, file, need1, plan, 4096, NULL, &r) : -2;
    CHECK("fixed_count_below_min_refused", n == -1 && r.refused);
    root_reset(&v); v.arr[0] = 7; v.arr[1] = 8;
    n = root_fixed_save(&v, 1, file, sizeof(file));
    memset(&r, 0, sizeof(r));
    n = n == need1 ? root_fixed_load(back, 4, file, need1, plan, 4096, NULL, &r) : -2;
    CHECK("fixed_count_at_min_reads", n == 1 && !r.refused && back[0].arr_count == 2 && back[0].arr[1] == 8);

    /* ... ON THE JSON READ */
    {
        static const char below[] = "{\"arr\":[7]}";
        static const char at[] = "{\"arr\":[7,8]}";
        root_reset(&w);
        ok = root_from_json(&w, below, (int64_t)strlen(below), &r);
        CHECK("json_count_below_min_refused", !ok && r.refused);
        root_reset(&w);
        ok = root_from_json(&w, at, (int64_t)strlen(at), &r);
        CHECK("json_count_at_min_reads", ok && w.arr_count == 2 && w.arr[1] == 8);
    }

    /* A FLOAT32 RANGE IS HELD AT FLOAT32 ON THE JSON READ: max = 0.1 */
    {
        static const char top[] = "{\"g\":0.1}";
        static const char past[] = "{\"g\":0.11}";
        char text[256];
        root_reset(&w);
        ok = root_from_json(&w, top, (int64_t)strlen(top), &r);
        CHECK("json_float32_at_max_reads", ok && bits_of(w.g) == bits_of(0.1f));
        root_reset(&w);
        ok = root_from_json(&w, past, (int64_t)strlen(past), &r);
        CHECK("json_float32_past_max_refused", !ok && r.refused);
        root_reset(&v); v.g = 0.1f;
        n = root_to_json(&v, text, (int64_t)sizeof(text) - 1);
        if (n > 0) { text[n] = 0; }
        root_reset(&w);
        ok = n > 0 && root_from_json(&w, text, n, &r);
        CHECK("json_float32_max_self_round_trip", ok && bits_of(w.g) == bits_of(0.1f));
    }

    /* A COMPRESSED FLOAT'S -0 IS +0: READ ... */
    root_reset(&v); v.f = 0.25f;
    n = root_save(&v, wire, sizeof(wire));
    {
        int64_t at = find32(wire, n, bits_of(0.25f));
        if (at >= 0) { put32(wire + at, 0x80000000u); }
        root_reset(&w);
        memset(&r, 0, sizeof(r));
        ok = at >= 0 && root_load(&w, wire, n, &r);
        CHECK("file_compressed_negzero_reads_as_plus_zero", ok && bits_of(w.f) == 0u && r.unknown == 0);
    }
    {
        static const char negz[] = "{\"f\":-0.0}";
        root_reset(&w);
        ok = root_from_json(&w, negz, (int64_t)strlen(negz), &r);
        CHECK("json_compressed_negzero_reads_as_plus_zero", ok && bits_of(w.f) == 0u);
    }
    /* ... AND WRITTEN AS +0, under a non-zero default */
    root_reset(&v); v.f = -0.0f;
    n = root_save(&v, wire, sizeof(wire));
    root_reset(&v); v.f = 0.0f;
    m = root_save(&v, other, sizeof(other));
    CHECK("file_compressed_negzero_written_as_plus_zero", n > 0 && n == m && memcmp(wire, other, (size_t)n) == 0);
    root_reset(&v); v.f = -0.0f;
    n = root_fixed_save(&v, 1, file, sizeof(file));
    root_reset(&v); v.f = 0.0f;
    m = root_fixed_save(&v, 1, file + need1, sizeof(file) - need1);
    CHECK("fixed_compressed_negzero_written_as_plus_zero", n == need1 && m == need1 && memcmp(file, file + need1, (size_t)need1) == 0);

    /* C-4: A REFUSAL ON RECORD 1'S HASH LANDS NOTHING */
    {
        Root two[2];
        int64_t i;
        root_reset(&two[0]); two[0].arr_count = 3; two[0].arr[2] = 9; two[0].f = 0.25f;
        root_reset(&two[1]); two[1].arr[0] = 4;
        need2 = root_fixed_measure(2);
        n = root_fixed_save(two, 2, file, sizeof(file));
        for (i = 0; i < 8; ++i) { file[need1 + i] = (uint8_t)~file[need1 + i]; } /* record 1's hash */
        memset(back, 0x5A, sizeof(back));
        memset(&r, 0, sizeof(r));
        n = n == need2 ? root_fixed_load(back, 4, file, need2, plan, 4096, NULL, &r) : -2;
        CHECK("c4_record1_hash_refused", n == -1 && r.refused && r.reason == SCHEMA_TABLE_NO_LAYOUT && r.widened == 0);
        CHECK("c4_record0_not_left_decoded", back[0].arr_count == 2 && back[0].arr[0] == 0 && back[0].arr[1] == 0 && bits_of(back[0].f) == bits_of(0.5f) && bits_of(back[0].g) == 0u);
        for (i = 0; i < 8; ++i) { file[need1 + i] = (uint8_t)~file[need1 + i]; }
        memset(&r, 0, sizeof(r));
        n = root_fixed_load(back, 4, file, need2, plan, 4096, NULL, &r);
        CHECK("c4_two_records_read", n == 2 && !r.refused && back[0].arr_count == 3 && back[0].arr[2] == 9 && back[1].arr[0] == 4);
    }

    /* CPP-1 ON C: union-in-union nesting is bounded before any walk */
    {
        struct chain_job job;
        memset(&job, 0, sizeof(job));
        ok = chain_load(3, &job);
        CHECK("cpp1_shallow_union_chain_reads", ok && job.ok && !job.refused && job.unknown > 0);
        memset(&job, 0, sizeof(job));
        ok = chain_load(200000, &job);
        CHECK("cpp1_deep_union_chain_refused", ok && !job.ok && job.refused);
    }
    return failed;
}
`

func runCStrictProgram(t *testing.T, schema, main string) (string, error) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	u := unitFrom(t, schema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var cfiles []string
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, ".c") {
			cfiles = append(cfiles, p)
		}
	}
	mainPath := filepath.Join(dir, "main.c")
	if err := os.WriteFile(mainPath, []byte(main), 0o600); err != nil {
		t.Fatal(err)
	}
	ser, ok := cSerializeDir(t)
	if !ok {
		t.Fatalf("serialize.h is not staged at %s (set SERIALIZE_C, or run make deps)", ser)
	}
	bin := filepath.Join(dir, "strict")
	args := []string{"-std=c99", "-D_POSIX_C_SOURCE=200809L", "-DNDEBUG", "-Wall", "-Wextra", "-Werror", "-O1", "-fsanitize=undefined", "-fno-sanitize-recover=all", "-I", dir, "-I", ser, mainPath}
	args = append(args, cfiles...)
	args = append(args, "-lm", "-lpthread", "-o", bin)
	if out, err := testproc.Command(t, cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile C failed: %v\n%s", err, out)
	}
	out, err := testproc.Command(t, bin).CombinedOutput()
	return string(out), err
}

// TestCStrictReads holds the C file, fixed and JSON reads (and the writers'
// -0 store) to the review's rules: a counted array is born at its Min and a
// count below Min is refused on every read path; a float32 range is held at
// float32; a compressed float's -0 lands and is written as +0; a refusal on a
// later record's hash lands nothing (C-4); a deep union-in-union chain is
// refused without exhausting a small stack (CPP-1 checked on C).
func TestCStrictReads(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the c compiler (it compiles and runs the generated strict-read probe)")
	out, err := runCStrictProgram(t, cStrictReadsSchema, cStrictReadsMain)
	if err != nil {
		t.Errorf("the strict-read probe failed: %v", err)
	}
	for _, name := range []string{
		"fresh_counted_array_born_at_min", "birth_count_round_trips", "birth_count_with_a_value_rides", "count_past_birth_rides",
		"file_count_below_min_refused", "file_count_at_min_reads", "fixed_count_below_min_refused", "fixed_count_at_min_reads",
		"json_count_below_min_refused", "json_count_at_min_reads",
		"json_float32_at_max_reads", "json_float32_past_max_refused", "json_float32_max_self_round_trip",
		"file_compressed_negzero_reads_as_plus_zero", "json_compressed_negzero_reads_as_plus_zero",
		"file_compressed_negzero_written_as_plus_zero", "fixed_compressed_negzero_written_as_plus_zero",
		"c4_record1_hash_refused", "c4_record0_not_left_decoded", "c4_two_records_read",
		"cpp1_shallow_union_chain_reads", "cpp1_deep_union_chain_refused",
	} {
		if !strings.Contains(out, "PASS "+name+"\n") {
			t.Errorf("missing PASS for %s", name)
		}
	}
	if t.Failed() {
		t.Logf("probe output:\n%s", out)
	}
}
