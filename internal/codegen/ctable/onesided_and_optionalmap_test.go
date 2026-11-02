package ctable

import (
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

func TestCOneSidedIntegerRange(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	src := "package probe\n\n" +
		"fixed table FixedOneSided\n{\n" +
		"    a int64 | min = 0\n" +
		"    b uint16 | max = 9\n" +
		"    c int8 | min = -3\n" +
		"}\n\n" +
		"table VarOneSided\n{\n" +
		"    d int64 | min = 0\n" +
		"    e uint16 | max = 9\n" +
		"    f int8 | min = -3\n" +
		"}\n"
	u := unitFrom(t, src)
	out, err := cHeldRun(t, u, `
    #define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
    const TableTypeInfo * info = fixed_one_sided_table_type();
    const TableTypeInfo * vinfo = var_one_sided_table_type();
    FixedOneSided v, upper, lower, w, loaded;
    uint8_t fixed_buf[256];
    int64_t fn;
    TableReport report, r2, r3, r4;
    const char * text = "{\"a\":-5,\"b\":20,\"c\":-100}";
    const char * high = "{\"c\":200}";
    const char * low = "{\"b\":-1}";

    CHECK(info->fields[0].has_range == 1 && info->fields[0].range_min == 0.0 && info->fields[0].range_max == 9.223372036854776e+18);
    CHECK(info->fields[1].has_range == 1 && info->fields[1].range_min == 0.0 && info->fields[1].range_max == 9.0);
    CHECK(info->fields[2].has_range == 1 && info->fields[2].range_min == -3.0 && info->fields[2].range_max == 127.0);
    CHECK(vinfo->fields[0].has_range == 1 && vinfo->fields[0].range_min == 0.0 && vinfo->fields[0].range_max == 9.223372036854776e+18);
    CHECK(vinfo->fields[1].has_range == 1 && vinfo->fields[1].range_min == 0.0 && vinfo->fields[1].range_max == 9.0);
    CHECK(vinfo->fields[2].has_range == 1 && vinfo->fields[2].range_min == -3.0 && vinfo->fields[2].range_max == 127.0);

    memset(&report, 0, sizeof(report));
    CHECK(fixed_one_sided_from_json_tolerant(&v, text, (int64_t)strlen(text), &report));
    CHECK(v.a == 0);
    CHECK(v.b == 9);
    CHECK(v.c == -3);
    CHECK(report.clamped >= 3);

    memset(&r3, 0, sizeof(r3));
    CHECK(fixed_one_sided_from_json_tolerant(&upper, high, (int64_t)strlen(high), &r3));
    CHECK(upper.c == 127);
    CHECK(r3.clamped == 1);

    memset(&r4, 0, sizeof(r4));
    CHECK(fixed_one_sided_from_json_tolerant(&lower, low, (int64_t)strlen(low), &r4));
    CHECK(lower.b == 0);
    CHECK(r4.clamped == 1);

    fixed_one_sided_reset(&w);
    w.a = -12345;
    w.b = 999;
    w.c = -128;
    fn = fixed_one_sided_fixed_save(&w, 1, fixed_buf, (int64_t)sizeof(fixed_buf));
    CHECK(fn > 0);
    memset(&r2, 0, sizeof(r2));
    CHECK(fixed_one_sided_fixed_load_tolerant(&loaded, 1, fixed_buf, fn, NULL, 0, NULL, &r2) == 1);
    CHECK(loaded.a == 0);
    CHECK(loaded.b == 9);
    CHECK(loaded.c == -3);
    CHECK(r2.clamped >= 3 && !r2.refused);
    #undef CHECK
`)
	if err != nil {
		t.Fatalf("one-sided probe failed: %v\n%s", err, out)
	}
}

const cMapJsonNullSchema = `package probe

table V
{
    marker int32 = 7
}

table M
{
    id   int32
    tags ?map[uint32]V
    tail int32 = 9
}
`

const cMapJsonNullProbe = `#include "ProbeTable.h"
#include <stddef.h>
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr,"%s\n",#x); return 1; } } while(0)

static int reset_case( const char * text, int repeated )
{
    const char * fresh = "{\"id\":1}";
    MBuilder b, fb;
    const M * v;
    const M * f;
    TableReport report, fresh_report;
    const uint8_t zero_slot[16] = {0};
    SCHEMA_C_ALIGN16 static uint8_t wire[1<<16];
    SCHEMA_C_ALIGN16 static uint8_t fresh_wire[1<<16];
    int64_t n, fn;

    CHECK(m_builder_init(&b));
    memset(&report, 0, sizeof(report));
    if ( !m_from_json_tolerant(&b, text, (int64_t)strlen(text), &report) )
    {
        fprintf( stderr, "tags: from_json refused\n" );
        m_builder_shutdown(&b);
        return 1;
    }
    if ( report.malformed || report.refused || report.kind_mismatch != 0 || report.unknown != 0 )
    {
        fprintf( stderr, "tags: from_json report malformed=%d refused=%d kind_mismatch=%d unknown=%d\n",
                 (int)report.malformed, (int)report.refused, (int)report.kind_mismatch, (int)report.unknown );
        m_builder_shutdown(&b);
        return 1;
    }
    if ( report.duplicate != ( repeated ? 1 : 0 ) )
    {
        fprintf( stderr, "tags: duplicate %d\n", (int)report.duplicate );
        m_builder_shutdown(&b);
        return 1;
    }
    v = m_builder_root(&b);
    CHECK(v != NULL);
    if ( v->tags_present )
    {
        fprintf( stderr, "tags: present after null\n" );
        m_builder_shutdown(&b);
        return 1;
    }
    if ( v->tags.count != 0 )
    {
        fprintf( stderr, "tags: count %d after null\n", (int)v->tags.count );
        m_builder_shutdown(&b);
        return 1;
    }
    if ( memcmp( (const void *)&v->tags, zero_slot, sizeof(v->tags) ) != 0 )
    {
        fprintf( stderr, "tags: the empty map slot is not sixteen zero bytes\n" );
        m_builder_shutdown(&b);
        return 1;
    }

    CHECK(m_builder_init(&fb));
    memset(&fresh_report, 0, sizeof(fresh_report));
    CHECK( m_from_json_tolerant(&fb, fresh, (int64_t)strlen(fresh), &fresh_report) );
    f = m_builder_root(&fb);
    CHECK(f != NULL);
    if ( v->tags_present != f->tags_present || v->tail != f->tail )
    {
        fprintf( stderr, "tags: the bytes after the map slot moved\n" );
        m_builder_shutdown(&fb);
        m_builder_shutdown(&b);
        return 1;
    }
    CHECK( m_builder_lock(&b) );
    CHECK( m_builder_lock(&fb) );
    n = m_save(NULL, (const M *)(const void *)b.region, wire, sizeof(wire));
    fn = m_save(NULL, (const M *)(const void *)fb.region, fresh_wire, sizeof(fresh_wire));
    m_builder_shutdown(&fb);
    m_builder_shutdown(&b);
    if ( n < 0 || fn < 0 || n != fn || memcmp( wire, fresh_wire, (size_t)n ) != 0 )
    {
        fprintf( stderr, "tags: Save differs from a value that never had the key\n" );
        return 1;
    }
    return 0;
}

int main(void)
{
    if ( reset_case( "{\"id\":1,\"tags\":{\"1\":{\"marker\":5},\"2\":{\"marker\":6}},\"tags\":null}", 1 ) ) { return 1; }
    if ( reset_case( "{\"id\":1,\"tags\":{\"1\":{},\"2\":{}},\"tags\":null}", 1 ) ) { return 1; }
    if ( reset_case( "{\"id\":1,\"tags\":null}", 0 ) ) { return 1; }
    return 0;
}
`

func cMapJsonNullRun(t *testing.T, cc string, revert bool) (string, error) {
	t.Helper()
	u := unitFrom(t, cMapJsonNullSchema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatalf("cgen: %v", err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatalf("generate: %v", err)
	}
	maps.Copy(files, tables)
	if revert {
		old := "if ( f->sequence == 2 ) { memset( (uint8_t *) base + f->offset, 0, 16 ); }\n                else if ( f->table != NULL )"
		planted := false
		for file, data := range files {
			if strings.Contains(string(data), old) {
				files[file] = []byte(strings.Replace(string(data), old, "if ( f->table != NULL )", 1))
				planted = true
			}
		}
		if !planted {
			t.Fatalf("the generated source no longer holds the map branch this witness reverts: %q", old)
		}
	}
	dir := t.TempDir()
	for file, data := range files {
		if err := os.WriteFile(filepath.Join(dir, file), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	main := filepath.Join(dir, "main.c")
	if err := os.WriteFile(main, []byte(cMapJsonNullProbe), 0600); err != nil {
		t.Fatal(err)
	}
	bin := filepath.Join(dir, "probe")
	args := []string{"-std=c99", "-O1", "-Wall", "-Wextra", "-Werror"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, main)
	for file := range files {
		if strings.HasSuffix(file, ".c") {
			args = append(args, filepath.Join(dir, file))
		}
	}
	args = append(args, "-lm", "-o", bin)
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, out)
	}
	out, err := exec.Command(bin).CombinedOutput()
	return string(out), err
}

func TestCOptionalMapJsonNullResetsToEmpty(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	cc, err := exec.LookPath("cc")
	if err != nil {
		t.Skip("the generated C is compiled and run: no cc on PATH")
	}
	if out, err := cMapJsonNullRun(t, cc, false); err != nil {
		t.Fatalf("the null reset probe failed: %v\n%s", err, out)
	}
	t.Run("reversed witness", func(t *testing.T) {
		out, err := cMapJsonNullRun(t, cc, true)
		if err == nil {
			t.Fatalf("the probe passed with the map branch reverted, so it does not pin the branch:\n%s", out)
		}
		if !strings.Contains(out, "tags") {
			t.Fatalf("the reverted probe failed without naming the field tags:\n%s", out)
		}
		t.Logf("reverted branch red: %s", strings.TrimSpace(out))
	})
}

func TestCMapOptionalValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the C compiler")
	src := "package probe\n\n" +
		"table Child\n{\n" +
		"    n int32\n" +
		"}\n\n" +
		"table Root\n{\n" +
		"    lookup map[string(8)]?int32\n" +
		"    ints   map[uint32]?Child\n" +
		"    names  map[string(8)]?Child\n" +
		"}\n"
	u := unitFrom(t, src)
	out, err := cHeldRun(t, u, `
    #define CHECK(x) do { if (!(x)) { fprintf(stderr,"line %d: %s\n",__LINE__,#x); return 1; } } while(0)
    RootBuilder b;
    const Root * v;
    const RootLookupEntry * e;
    const RootIntsEntry * ie;
    const RootNamesEntry * ne;
    TableReport report;
    static char json_buf[4096];
    SCHEMA_C_ALIGN16 static uint8_t wire_buf[4096];
    SCHEMA_C_ALIGN16 static uint8_t load_region[4096];
    int64_t jn, wn;
    const Root * loaded;

    /* 1. Explicit null vs present integer in map[string(8)]?int32 */
    CHECK(root_builder_init(&b));
    memset(&report, 0, sizeof(report));
    { const char * s = "{\"lookup\":{\"a\":null,\"b\":7}}"; CHECK(root_from_json(&b, s, (int64_t)strlen(s), &report)); }
    CHECK(!report.malformed && !report.refused && report.kind_mismatch == 0);
    CHECK(root_builder_lock(&b));
    v = (const Root *)(const void *)b.region;
    e = root_lookup_find(&v->lookup, "a");
    CHECK(e != NULL && e->value_present == 0 && e->value == 0);
    e = root_lookup_find(&v->lookup, "b");
    CHECK(e != NULL && e->value_present == 1 && e->value == 7);
    jn = root_to_json(v, json_buf, (int64_t)sizeof(json_buf));
    CHECK(jn > 0);
    json_buf[jn] = 0;
    CHECK(strstr(json_buf, "\"a\": null") != NULL);
    CHECK(strstr(json_buf, "\"b\": 7") != NULL);
    root_builder_shutdown(&b);

    /* 2. Non-null wrong-shape value sets value_present = 1 with default value 0 */
    CHECK(root_builder_init(&b));
    memset(&report, 0, sizeof(report));
    { const char * s = "{\"lookup\":{\"a\":\"bad\"}}"; CHECK(root_from_json_tolerant(&b, s, (int64_t)strlen(s), &report)); }
    CHECK(!report.malformed && report.kind_mismatch == 1);
    CHECK(root_builder_lock(&b));
    v = (const Root *)(const void *)b.region;
    e = root_lookup_find(&v->lookup, "a");
    CHECK(e != NULL && e->value_present == 1 && e->value == 0);
    jn = root_to_json(v, json_buf, (int64_t)sizeof(json_buf));
    CHECK(jn > 0);
    json_buf[jn] = 0;
    CHECK(strstr(json_buf, "\"a\": 0") != NULL);
    root_builder_shutdown(&b);

    /* 3. Duplicate key where second occurrence has wrong shape -> present default 0 */
    CHECK(root_builder_init(&b));
    memset(&report, 0, sizeof(report));
    { const char * s = "{\"lookup\":{\"a\":5,\"a\":\"bad\"}}"; CHECK(root_from_json_tolerant(&b, s, (int64_t)strlen(s), &report)); }
    CHECK(!report.malformed && report.duplicate == 1 && report.kind_mismatch == 1);
    CHECK(root_builder_lock(&b));
    v = (const Root *)(const void *)b.region;
    e = root_lookup_find(&v->lookup, "a");
    CHECK(e != NULL && e->value_present == 1 && e->value == 0);
    root_builder_shutdown(&b);

    /* 4. Duplicate key where second occurrence is null -> absent default 0 */
    CHECK(root_builder_init(&b));
    memset(&report, 0, sizeof(report));
    { const char * s = "{\"lookup\":{\"a\":5,\"a\":null}}"; CHECK(root_from_json_tolerant(&b, s, (int64_t)strlen(s), &report)); }
    CHECK(!report.malformed && report.duplicate == 1 && report.kind_mismatch == 0);
    CHECK(root_builder_lock(&b));
    v = (const Root *)(const void *)b.region;
    e = root_lookup_find(&v->lookup, "a");
    CHECK(e != NULL && e->value_present == 0 && e->value == 0);
    root_builder_shutdown(&b);

    /* 5. map[K]?Child JSON -> wire -> load -> JSON round-trip */
    CHECK(root_builder_init(&b));
    memset(&report, 0, sizeof(report));
    {
        const char * doc = "{\"ints\":{\"5\":null,\"2\":{\"n\":7}},\"names\":{\"a\":null,\"z\":{}}}";
        CHECK(root_from_json(&b, doc, (int64_t)strlen(doc), &report));
    }
    CHECK(root_builder_lock(&b));
    v = (const Root *)(const void *)b.region;
    wn = root_save(NULL, v, wire_buf, (int64_t)sizeof(wire_buf));
    CHECK(wn > 0);
    memset(&report, 0, sizeof(report));
    loaded = root_load(load_region, (int64_t)sizeof(load_region), wire_buf, wn, &report);
    CHECK(loaded != NULL && !report.malformed && !report.refused);
    ie = root_ints_find(&loaded->ints, 5u);
    CHECK(ie != NULL && ie->value_present == 0);
    ie = root_ints_find(&loaded->ints, 2u);
    CHECK(ie != NULL && ie->value_present == 1 && ie->value.n == 7);
    ne = root_names_find(&loaded->names, "a");
    CHECK(ne != NULL && ne->value_present == 0);
    ne = root_names_find(&loaded->names, "z");
    CHECK(ne != NULL && ne->value_present == 1 && ne->value.n == 0);
    jn = root_to_json(loaded, json_buf, (int64_t)sizeof(json_buf));
    CHECK(jn > 0);
    json_buf[jn] = 0;
    CHECK(strstr(json_buf, "\"5\": null") != NULL);
    CHECK(strstr(json_buf, "\"a\": null") != NULL);
    root_builder_shutdown(&b);
    #undef CHECK
`)
	if err != nil {
		t.Fatalf("optional map values probe failed: %v\n%s", err, out)
	}
}
