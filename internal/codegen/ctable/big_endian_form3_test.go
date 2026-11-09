package ctable

// TestCBigEndianFormThreeRead is the C big-endian form-3 (fixed form) read
// cell. The wire is little-endian on every host (docs/SPEC-TABLES.md §3.4),
// but the C fixed reader's copy ops landed the wire bytes into native storage
// verbatim, so on a big-endian host every multi-byte value arrived
// byte-swapped — the defect the s390x soak named and skipped
// (test/c-tables/soak_main.c). The fix carries each copy entry's element
// width and, on a big-endian host, reverses each element as it lands; the
// widen, ordinal, const, count and text-length ops read their source through
// explicit little-endian loads.
//
// No big-endian host is needed to hold it: the probe is compiled twice from
// the same generated header, once native and once with
// SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN, which selects the big-endian copy
// path. In the forced build a copied field's native value is meaningless on
// this little-endian host, so the probe holds its BYTES: at each field's
// offsetof they must be the big-endian image of the saved value, which is
// exactly what a big-endian host's struct would hold. Fields the reader
// decodes by value (a text length, a count) are held by value in both
// builds. The strict half is in both builds too: a forged bool byte refuses
// the whole load and every record it wrote is reset — one read, the whole
// value or a refusal.

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

const bigEndianForm3Schema = `package probe
fixed table Root {
    lead uint32
    a uint16
    b uint32
    c uint64
    d int32
    e float32
    f float64
    vals [3]uint32
    label string(8)
    ok bool
}
`

const bigEndianForm3Probe = `#define SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN 1
#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { printf( "FAIL: line %d: %s\n", __LINE__, #x ); return 1; } } while (0)
static int be_at( const void * base, size_t off, const uint8_t * want, size_t n )
{
    return memcmp( (const uint8_t *) base + off, want, n ) == 0;
}
int main( void )
{
    Root value, loaded, fresh;
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[1024];
    int64_t need, n;
    static const uint8_t lead_be[4] = { 0x12, 0x34, 0x56, 0x78 };
    static const uint8_t a_be[2] = { 0x9A, 0xBC };
    static const uint8_t b_be[4] = { 0xDE, 0xAD, 0xBE, 0xEF };
    static const uint8_t c_be[8] = { 0x01, 0x23, 0x45, 0x67, 0x89, 0xAB, 0xCD, 0xEF };
    static const uint8_t d_be[4] = { 0xFF, 0xFF, 0xFF, 0xFE };
    static const uint8_t vals_be[12] = { 0, 0, 0, 1, 0, 0, 0, 2, 0, 0, 0, 3 };
    uint32_t layout_bytes;
    uint8_t * body;
    memset( &value, 0, sizeof( value ) );
    value.lead = 0x12345678u;
    value.a = 0x9ABC;
    value.b = 0xDEADBEEFu;
    value.c = 0x0123456789ABCDEFull;
    value.d = -2;
    value.e = 1.5f;
    value.f = -2.5;
    value.vals[0] = 1; value.vals[1] = 2; value.vals[2] = 3;
    memcpy( value.label, "hi", 2 );
    value.label_length = 2;
    value.ok = 1;
    need = root_fixed_measure( 1 );
    CHECK( need > 0 && need <= (int64_t) sizeof( file ) );
    CHECK( root_fixed_save( &value, 1, file, (int64_t) sizeof( file ) ) == need );
    /* THE FILE IS LITTLE-ENDIAN WHATEVER THE HOST: its first record body
       opens with lead's little-endian bytes, not this build's order. */
    layout_bytes = table_fixed_get32( file + kTableFixedHeaderBytes );
    body = file + kTableFixedHeaderBytes + 4 + layout_bytes + 8;
    CHECK( body[0] == 0x78 && body[1] == 0x56 && body[2] == 0x34 && body[3] == 0x12 );
    memset( &loaded, 0xAA, sizeof( loaded ) );
    memset( &report, 0, sizeof( report ) );
    n = root_fixed_load( &loaded, 1, file, need, plan, 64, NULL, &report );
    CHECK( n == 1 );
    CHECK( !report.refused );
    /* COPIED FIELDS LAND AS THE BIG-ENDIAN IMAGE OF THE SAVED VALUE. */
    CHECK( be_at( &loaded, offsetof( Root, lead ), lead_be, 4 ) );
    CHECK( be_at( &loaded, offsetof( Root, a ), a_be, 2 ) );
    CHECK( be_at( &loaded, offsetof( Root, b ), b_be, 4 ) );
    CHECK( be_at( &loaded, offsetof( Root, c ), c_be, 8 ) );
    CHECK( be_at( &loaded, offsetof( Root, d ), d_be, 4 ) );
    CHECK( be_at( &loaded, offsetof( Root, vals ), vals_be, 12 ) );
    /* A FLOAT RIDES THE SAME COPY: its bits are the value's, byte-reversed. */
    {
        float ef = 1.5f; double df = -2.5;
        uint8_t eb[4], db[8];
        int i;
        memcpy( eb, &ef, 4 ); memcpy( db, &df, 8 );
        for ( i = 0; i < 4; ++i ) { CHECK( ((uint8_t *) &loaded)[offsetof( Root, e ) + i] == eb[3 - i] ); }
        for ( i = 0; i < 8; ++i ) { CHECK( ((uint8_t *) &loaded)[offsetof( Root, f ) + i] == db[7 - i] ); }
    }
    /* DECODED-BY-VALUE FIELDS AND TEXT LAND BY VALUE IN EITHER ORDER. */
    CHECK( loaded.label_length == 2 && loaded.label[0] == 'h' && loaded.label[1] == 'i' && loaded.label[2] == 0 );
    CHECK( loaded.ok == 1 );
    /* THE STRICT READ: a forged bool byte (the body's last byte) refuses the
       WHOLE load, and every record it wrote is back at its defaults. */
    body[root_fixed_body_bytes - 1] = 0x02;
    memset( &fresh, 0, sizeof( fresh ) );
    root_reset( &fresh );
    memset( &loaded, 0, sizeof( loaded ) ); /* padding settled, as fresh's is: the memcmp below is about values */
    memset( &report, 0, sizeof( report ) );
    n = root_fixed_load( &loaded, 1, file, need, plan, 64, NULL, &report );
    CHECK( n == -1 );
    CHECK( report.refused && report.refuse_reason == SCHEMA_TABLE_REFUSE_WIRE_DAMAGED );
    CHECK( memcmp( &loaded, &fresh, sizeof( fresh ) ) == 0 );
    return 0;
}
`

// bigEndianForm3Run generates the probe unit, optionally sabotaging the
// generated header (the real artifact), compiles it and runs it. It answers
// the probe's FAIL lines and its whole output.
func bigEndianForm3Run(t *testing.T, sabotage [2]string) ([]string, string) {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	u := unitFrom(t, bigEndianForm3Schema)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	if sabotage[0] != "" {
		patched := false
		for name, data := range files {
			if src := string(data); strings.Contains(src, sabotage[0]) {
				files[name] = []byte(strings.ReplaceAll(src, sabotage[0], sabotage[1]))
				patched = true
			}
		}
		if !patched {
			t.Fatalf("the sabotage patches nothing: no emitted file carries %q", sabotage[0])
		}
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "main.c"), []byte(bigEndianForm3Probe), 0o600); err != nil {
		t.Fatal(err)
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O1"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, "-I", dir, filepath.Join(dir, "main.c"))
	for name := range files {
		if strings.HasSuffix(name, ".c") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	args = append(args, "-lm", "-o", filepath.Join(dir, "probe"))
	if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		t.Fatalf("compile: %v\n%s", err, output)
	}
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()
	output, _ := exec.CommandContext(ctx, filepath.Join(dir, "probe")).CombinedOutput()
	var fails []string
	for line := range strings.SplitSeq(string(output), "\n") {
		if strings.HasPrefix(line, "FAIL: ") {
			fails = append(fails, line)
		}
	}
	return fails, string(output)
}

func TestCBigEndianFormThreeRead(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	if fails, out := bigEndianForm3Run(t, [2]string{}); len(fails) != 0 {
		t.Fatalf("the C big-endian form-3 read does not hold:\n%s", out)
	}
	// The native build of the same unit round-trips by value, so the forced
	// build's byte image is held against a reader that also works at home.
	runCGenerated(t, bigEndianForm3Schema, `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
#define CHECK(x) do { if (!(x)) { fprintf(stderr, "line %d: %s\n", __LINE__, #x); return 1; } } while (0)
int main(void)
{
    Root value, loaded;
    TableReport report;
    TableFixedEntry plan[64];
    uint8_t file[1024];
    int64_t need, n;
    memset(&value, 0, sizeof(value));
    value.lead = 0x12345678u; value.a = 0x9ABC; value.b = 0xDEADBEEFu;
    value.c = 0x0123456789ABCDEFull; value.d = -2; value.e = 1.5f; value.f = -2.5;
    value.vals[0] = 1; value.vals[1] = 2; value.vals[2] = 3;
    memcpy(value.label, "hi", 2); value.label_length = 2; value.ok = 1;
    need = root_fixed_measure(1);
    CHECK(need > 0 && root_fixed_save(&value, 1, file, (int64_t)sizeof(file)) == need);
    memset(&loaded, 0xAA, sizeof(loaded));
    memset(&report, 0, sizeof(report));
    n = root_fixed_load(&loaded, 1, file, need, plan, 64, NULL, &report);
    CHECK(n == 1 && !report.refused);
    CHECK(loaded.lead == 0x12345678u && loaded.a == 0x9ABC && loaded.b == 0xDEADBEEFu);
    CHECK(loaded.c == 0x0123456789ABCDEFull && loaded.d == -2 && loaded.e == 1.5f && loaded.f == -2.5);
    CHECK(loaded.vals[0] == 1 && loaded.vals[1] == 2 && loaded.vals[2] == 3);
    CHECK(loaded.label_length == 2 && loaded.label[0] == 'h' && loaded.label[1] == 'i' && loaded.ok == 1);
    return 0;
}
`, identityCCFlags()...)
}

// TestCBigEndianFormThreeReadNegativeControl plants the pre-fix defect in
// the real artifact — the generated header's big-endian copy, put back to a
// host-order byte copy — and requires the big-endian probe to go red. On a
// little-endian host the same sabotage is invisible, which is the whole
// argument for forcing the big-endian path in the probe.
func TestCBigEndianFormThreeReadNegativeControl(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	sabotage := [2]string{
		"dst[i + j] = src[i + elem - 1 - j];",
		"dst[i + j] = src[i + j]; /* SABOTAGED: host order */",
	}
	fails, out := bigEndianForm3Run(t, sabotage)
	if len(fails) == 0 {
		t.Fatalf("NEGATIVE CONTROL FAILED: a host-order copy left the big-endian form-3 probe green:\n%s", out)
	}
}
