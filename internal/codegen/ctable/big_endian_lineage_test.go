package ctable

// TestCBigEndianFormThreeLineageStores holds the destination stores of the
// fixed plan's ordinal, widen and const ops. The wire is little-endian on every
// host (docs/SPEC-TABLES.md §3.4), so a big-endian host lands each op's value in
// native order: the copy path reverses its elements (big_endian_form3_test.go)
// and these three ops read their source through the explicit little-endian
// load, so their destinations hold the value at the destination width.
//
// The unit is a lineage read: a newer table carrying wide enum and union tags
// reads an older writer's file, so the plan runs an ORDINAL remap of an enum, a
// WIDEN of an int16 into an int32, and a CONST landing a union tag. The probe is
// compiled twice from the same header, native and with
// SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN; the forced build holds each field's byte
// image at its offsetof, which is what a big-endian host's struct would hold.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// A wide tag is two bytes on the wire, which is the smallest width whose byte
// image differs between the two host orders. Both generations of the enum are
// wide so the plan runs the ordinal remap rather than the enum widen rung, and
// both generations of the union are wide because a CONST lands at the reader's
// tag width.
const bigEndianLineageWide = 256

func bigEndianLineageEnumVariants() (old, new string) {
	old = "Silver, Gold, Bronze"
	new = "Bronze, Silver, Gold"
	for i := 3; i < bigEndianLineageWide; i++ {
		old += fmt.Sprintf(", F%03d", i)
		new += fmt.Sprintf(", F%03d", i)
	}
	return old, new
}

func bigEndianLineageUnionArms() (old, new string) {
	old = "    gamma Alpha\n    beta Beta\n    alpha Alpha\n"
	new = "    alpha Alpha\n    gamma Alpha\n    beta Beta\n"
	for i := 3; i < bigEndianLineageWide; i++ {
		old += fmt.Sprintf("    U%03d\n", i)
		new += fmt.Sprintf("    U%03d\n", i)
	}
	return old, new
}

func bigEndianLineageOldSchema() string {
	enums, _ := bigEndianLineageEnumVariants()
	arms, _ := bigEndianLineageUnionArms()
	return fmt.Sprintf(`package probe
enum Tier { %s }
type Alpha { m int32 = 0 }
type Beta { n int32 = 0 }
union Pick
{
%s}
fixed table Root
{
    tier Tier
    v    int16
    pick Pick
    seq  int32 = 0
}
`, enums, arms)
}

func bigEndianLineageNewSchema() string {
	_, enums := bigEndianLineageEnumVariants()
	_, arms := bigEndianLineageUnionArms()
	return fmt.Sprintf(`package probe
enum Tier { %s }
type Alpha { m int32 = 0 }
type Beta { n int32 = 0 }
union Pick
{
%s}
fixed table Root
{
    tier Tier
    v    int32
    pick Pick
    seq  int32 = 0
}
`, enums, arms)
}

const bigEndianLineageWriter = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
int main( int argc, char ** argv )
{
    Root value;
    uint8_t file[1 << 16];
    int64_t need;
    FILE * f;
    if ( argc != 2 ) { return 1; }
    memset( &value, 0, sizeof( value ) );
    value.tier = TIER_BRONZE;
    value.v = -2;
    value.pick.type = PICK_TYPE_ALPHA;
    value.pick.as.alpha.m = 444;
    value.seq = 7;
    need = root_fixed_measure( 1 );
    if ( need <= 0 || need > (int64_t) sizeof( file ) ) { return 1; }
    if ( root_fixed_save( &value, 1, file, (int64_t) sizeof( file ) ) != need ) { return 1; }
    f = fopen( argv[1], "wb" );
    if ( f == NULL ) { return 1; }
    if ( fwrite( file, 1, (size_t) need, f ) != (size_t) need ) { return 1; }
    fclose( f );
    return 0;
}
`

const bigEndianLineageReader = `#include "ProbeTable.h"
#include <stdio.h>
#include <string.h>
static int fails = 0;
#define CHECK(x) do { if (!(x)) { printf( "FAIL: line %%d: %%s\n", __LINE__, #x ); fails++; } } while (0)
#ifdef SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN
static int be_at( const void * base, size_t off, const uint8_t * want, size_t n )
{
    return memcmp( (const uint8_t *) base + off, want, n ) == 0;
}
#endif
int main( void )
{
    static uint8_t data[1 << 16];
    Root back[8];
    TableReport r;
    static TableFixedEntry plan[4096];
    int64_t len, n;
    FILE * f = fopen( %q, "rb" );
    if ( f == NULL ) { printf( "FAIL: the lineage file is missing\n" ); return 1; }
    len = (int64_t) fread( data, 1, sizeof( data ), f );
    fclose( f );
    memset( back, 0, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
    n = root_fixed_load( back, 8, data, len, plan, 4096, NULL, &r );
    CHECK( n == 1 );
    CHECK( !r.refused );
#ifdef SCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN
    {
        /* Each expected value's big-endian image, which is what a big-endian
           host's struct holds and what the forced copy path writes. */
        uint16_t tier = (uint16_t) TIER_BRONZE;
        uint16_t pick = (uint16_t) PICK_TYPE_ALPHA;
        uint32_t widened = (uint32_t) (int32_t) -2;
        uint8_t tier_be[2], pick_be[2], widened_be[4];
        int i;
        for ( i = 0; i < 2; ++i ) { tier_be[i] = (uint8_t) ( tier >> ( 8 * ( 1 - i ) ) ); }
        for ( i = 0; i < 2; ++i ) { pick_be[i] = (uint8_t) ( pick >> ( 8 * ( 1 - i ) ) ); }
        for ( i = 0; i < 4; ++i ) { widened_be[i] = (uint8_t) ( widened >> ( 8 * ( 3 - i ) ) ); }
        CHECK( be_at( &back[0], offsetof( Root, tier ), tier_be, 2 ) );
        CHECK( be_at( &back[0], offsetof( Root, v ), widened_be, 4 ) );
        CHECK( be_at( &back[0], offsetof( Root, pick ), pick_be, 2 ) );
    }
#else
    CHECK( back[0].tier == TIER_BRONZE );
    CHECK( back[0].v == -2 );
    CHECK( back[0].pick.type == PICK_TYPE_ALPHA );
    CHECK( back[0].pick.as.alpha.m == 444 );
    CHECK( back[0].seq == 7 );
#endif
    return fails != 0;
}
`

func bigEndianLineageWriteFiles(t *testing.T, dir string, files map[string][]byte) {
	t.Helper()
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func bigEndianLineageCArgs(cc, dir, source string, files map[string][]byte, extra ...string) []string {
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wshadow", "-O1"}
	args = append(args, gccFortifySilence(cc)...)
	args = append(args, extra...)
	args = append(args, "-I", dir, filepath.Join(dir, source))
	for name := range files {
		if strings.HasSuffix(name, ".c") && !strings.Contains(name, "/") {
			args = append(args, filepath.Join(dir, name))
		}
	}
	return append(args, "-lm")
}

func bigEndianLineageCC(t *testing.T) string {
	t.Helper()
	cc := os.Getenv("CC")
	if cc == "" {
		var err error
		cc, err = exec.LookPath("cc")
		if err != nil {
			t.Skip("generated C execution requires cc")
		}
	}
	return cc
}

func TestCBigEndianFormThreeLineageStores(t *testing.T) {
	slowtest.Gate(t, "the C compiler (cc)")
	t.Parallel()
	cc := bigEndianLineageCC(t)
	ctx, cancel := slowtest.ProbeContext(t)
	defer cancel()

	oldUnit := unitFrom(t, bigEndianLineageOldSchema())
	newUnit := unitFrom(t, bigEndianLineageNewSchema())

	lineage := map[string][]FixedLineageEntry{}
	for _, st := range ir.TableFixedRoots(oldUnit) {
		e, ok := FixedLineageOf(oldUnit, st.Name)
		if !ok {
			t.Fatalf("no lineage entry for %s", st.Name)
		}
		lineage[st.Name] = append(lineage[st.Name], e)
	}

	oldFiles, err := cgen.Generate(oldUnit)
	if err != nil {
		t.Fatal(err)
	}
	oldTables, err := Generate(oldUnit)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(oldFiles, oldTables)
	oldDir := t.TempDir()
	bigEndianLineageWriteFiles(t, oldDir, oldFiles)
	if err := os.WriteFile(filepath.Join(oldDir, "writer.c"), []byte(bigEndianLineageWriter), 0o600); err != nil {
		t.Fatal(err)
	}
	oldArgs := bigEndianLineageCArgs(cc, oldDir, "writer.c", oldFiles, "-o", filepath.Join(oldDir, "writer"))
	if output, err := exec.Command(cc, oldArgs...).CombinedOutput(); err != nil {
		t.Fatalf("compile the older writer: %v\n%s", err, output)
	}
	file := filepath.Join(t.TempDir(), "lineage.bin")
	if output, err := exec.CommandContext(ctx, filepath.Join(oldDir, "writer"), file).CombinedOutput(); err != nil {
		t.Fatalf("run the older writer: %v\n%s", err, output)
	}

	newFiles, err := cgen.Generate(newUnit)
	if err != nil {
		t.Fatal(err)
	}
	newTables, err := GenerateLineage(newUnit, lineage)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(newFiles, newTables)
	newDir := t.TempDir()
	bigEndianLineageWriteFiles(t, newDir, newFiles)
	reader := fmt.Sprintf(bigEndianLineageReader, file)
	if err := os.WriteFile(filepath.Join(newDir, "reader.c"), []byte(reader), 0o600); err != nil {
		t.Fatal(err)
	}

	for _, tc := range []struct {
		name  string
		extra []string
	}{
		{name: "native"},
		{name: "forced_big_endian", extra: []string{"-DSCHEMA_TABLE_FIXED_FORCE_BIG_ENDIAN=1"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			out := filepath.Join(newDir, "reader_"+tc.name)
			args := bigEndianLineageCArgs(cc, newDir, "reader.c", newFiles, append(tc.extra, "-o", out)...)
			if output, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
				t.Fatalf("compile the %s reader: %v\n%s", tc.name, err, output)
			}
			output, err := exec.CommandContext(ctx, out).CombinedOutput()
			if err != nil {
				t.Fatalf("the %s reader does not hold:\n%s", tc.name, output)
			}
			if fails := strings.Contains(string(output), "FAIL: "); fails {
				t.Fatalf("the %s reader does not hold:\n%s", tc.name, output)
			}
		})
	}
}
