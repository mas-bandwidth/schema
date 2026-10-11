package ctable

// TestFixedRoadmapCT10FloatsUnions is the c leg's card
// schema-c-t10-floats-unions, rows enum-values, utf16-values, byte-buffers,
// keyed-arrays, nested-keyed-arrays, union-field-arms, payload-free-arms and
// union-arrays of docs/roadmap.sexp's node `fixed-tables`. One subtest per task
// id this card proves. Each subtest GENERATES the C unit for a definition that
// carries the construct, compiles a probe that WRITES the construct with
// non-default and boundary values (<snake>_fixed_save) and then READS it back
// (<snake>_fixed_load), and asserts every landed value by name.
//
// The fixed table header is self-contained for scalar/text/bytes/union storage
// (only the 128-bit lane reaches serialize.h), so the probe needs no sibling
// runtime and no corpus: the capability is this leg's own writer and reader.
//
// The laws are docs/SPEC-TABLES.md §3.4 (the record, prefill and the one read
// loop) and the task contract recorded on each node of docs/roadmap.sexp.
//
// Rows union-field-arms and payload-free-arms are NOT proved here: the shared
// gate ir/fixedform.go:237 (TableFixedSupported) refuses a union arm carrying
// text, bytes or an array, and refuses a payload-free (void) arm outright, so
// neither capability has a clause this leg's writer or reader can satisfy. They
// are reported `unknown` in $JOB/verdicts.txt, never faked green.

import (
	"fmt"
	"maps"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	cgen "github.com/mas-bandwidth/schema/v2/internal/codegen/c"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// cT10Run expands @S@ to the root's generated snake-case prefix and builds.
func cT10Run(t *testing.T, src, root, body string) {
	t.Helper()
	u := cUnitOf(t, src)
	files, err := cgen.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := GenerateLineage(u, map[string][]FixedLineageEntry{})
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var header string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		if strings.HasSuffix(name, "Table.h") {
			header = name
		}
	}
	if header == "" {
		t.Fatal("no Table.h was generated")
	}
	snake := ir.RustSnake(root)
	probe := fmt.Sprintf(`#include <stdio.h>
#include <stdint.h>
#include <string.h>
#include "%s"

int main( void )
{
    static uint8_t buf[1 << 20];
    static TableFixedEntry plan[4096];
    %s v;
    %s back[4];
    TableReport r;
    int64_t n;
    memset( &v, 0, sizeof( v ) );
    memset( back, 0x5A, sizeof( back ) );
    memset( &r, 0, sizeof( r ) );
%s
    return 0;
}
`, header, root, root, strings.ReplaceAll(body, "@S@", snake))
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}
	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	out, err := exec.Command(cc, "-std=c99", "-O1", "-I"+dir,
		filepath.Join(dir, "probe.c"), "-o", filepath.Join(dir, "probe"), "-lm").CombinedOutput()
	if err != nil {
		t.Fatalf("the probe did not compile: %v\n%s", err, out)
	}
	out, err = exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
	if err != nil {
		t.Fatalf("the probe failed: %v\n%s", err, out)
	}
}

func TestFixedRoadmapCT10FloatsUnions(t *testing.T) {
	t.Parallel()
	rows := []struct {
		id  string
		run func(t *testing.T)
	}{
		{"enum-values/c/valid-data", cT10EnumValues},
		{"utf16-values/c/valid-data", cT10UTF16Values},
		{"byte-buffers/c/valid-data", cT10ByteBuffers},
		{"keyed-arrays/c/valid-data", cT10KeyedArrays},
		{"nested-keyed-arrays/c/valid-data", cT10NestedKeyedArrays},
		{"union-arrays/c/valid-data", cT10UnionArrays},
	}
	for _, r := range rows {
		t.Run(r.id, func(t *testing.T) {
			t.Parallel()
			r.run(t)
		})
	}
}

// enum-values/c/valid-data — "Enums and None: valid-data write/read
// acceptance" (docs/roadmap.sexp). The enum lands on a NON-DEFAULT variant (the
// last declared, Platinum) and the scalar after it on INT32_MAX, so a mislaid
// ordinal width moves a neighbour.
func cT10EnumValues(t *testing.T) {
	cT10Run(t, `package probe
enum Tier { Bronze, Silver, Gold, Platinum }
fixed table Lineage
{
    tier Tier
    seq  int32 = 0
}
`, "Lineage", `
    v.tier = TIER_PLATINUM;
    v.seq = 2147483647;
    n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
    if ( n < 0 ) { printf( "save failed\n" ); return 1; }
    if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
        { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
    if ( back[0].tier != TIER_PLATINUM ) { printf( "tier=%d\n", (int) back[0].tier ); return 1; }
    if ( back[0].seq != 2147483647 ) { printf( "seq=%d\n", (int) back[0].seq ); return 1; }
`)
}

// utf16-values/c/valid-data — "Bounded UTF-16 string fields: valid-data
// write/read acceptance". The text is the bound (16 code units), a surrogate
// pair first so a reader that treats code units as scalars moves a neighbour,
// and every other code unit distinct.
func cT10UTF16Values(t *testing.T) {
	cT10Run(t, `package probe
fixed table WstringGrow
{
    lead  uint32 = 1
    text  wstring(16)
    trail uint32 = 2
}
`, "WstringGrow", `
    {
        int k;
        v.lead = 0xAAAAAAAAu;
        v.trail = 0xBBBBBBBBu;
        v.text[0] = 0xD83D; v.text[1] = 0xDE00;
        for ( k = 2; k < 16; ++k ) { v.text[k] = (uint16_t) ( 0x100 + k ); }
        v.text_length = 16;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
            { printf( "the row moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
        if ( back[0].text_length != 16 ) { printf( "text_length=%d\n", (int) back[0].text_length ); return 1; }
        if ( back[0].text[0] != 0xD83D || back[0].text[1] != 0xDE00 )
            { printf( "the surrogate pair came apart: %04x %04x\n", back[0].text[0], back[0].text[1] ); return 1; }
        for ( k = 2; k < 16; ++k )
            if ( back[0].text[k] != (uint16_t) ( 0x100 + k ) )
                { printf( "code unit %d is %04x\n", k, back[0].text[k] ); return 1; }
    }
`)
}

// byte-buffers/c/valid-data — "Bounded byte buffers: valid-data write/read
// acceptance". The buffer is the bound (16 bytes) and every byte distinct, so a
// reader that truncates at a terminator or reads bytes as text moves one.
func cT10ByteBuffers(t *testing.T) {
	cT10Run(t, `package probe
fixed table BytesGrow
{
    lead  uint32 = 1
    blob  bytes(16)
    trail uint32 = 2
}
`, "BytesGrow", `
    {
        int k;
        v.lead = 0xAAAAAAAAu;
        v.trail = 0xBBBBBBBBu;
        for ( k = 0; k < 16; ++k ) { v.blob[k] = (uint8_t) ( 0x80 + k ); }
        v.blob_length = 16;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].lead != 0xAAAAAAAAu || back[0].trail != 0xBBBBBBBBu )
            { printf( "the row moved a neighbour: lead=%u trail=%u\n", back[0].lead, back[0].trail ); return 1; }
        if ( back[0].blob_length != 16 ) { printf( "blob_length=%d\n", (int) back[0].blob_length ); return 1; }
        for ( k = 0; k < 16; ++k )
            if ( back[0].blob[k] != (uint8_t) ( 0x80 + k ) )
                { printf( "byte %d is %02x\n", k, back[0].blob[k] ); return 1; }
    }
`)
}

// keyed-arrays/c/valid-data — "Enum-keyed arrays: valid-data write/read
// acceptance" (docs/roadmap.sexp), contract "§2.4; §3.4 record, [Enum]T".
// C's storage IS the array (ctable.go:233), so every variant's slot holds a
// DISTINCT non-default value: slot j is 1000+j for j = 0..TIER_MAX-1 and the
// scalar after the array is a boundary value. A reader that leaves a slot at
// its declared default 7, or walks one slot short, names the slot it missed.
func cT10KeyedArrays(t *testing.T) {
	cT10Run(t, `package probe
enum Tier { Bronze, Silver, Gold, Platinum }
fixed table Qty
{
    n int32 = 7
}
fixed table Lineage
{
    slots [Tier]Qty
    seq   int32 = 0
}
`, "Lineage", `
    {
        int k;
        for ( k = 0; k < TIER_MAX; ++k ) { v.slots[k].n = 1000 + k; }
        v.seq = 2147483647;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        for ( k = 0; k < TIER_MAX; ++k )
            if ( back[0].slots[k].n != 1000 + k )
                { printf( "keyed slot %d is %d, not %d\n", k, back[0].slots[k].n, 1000 + k ); return 1; }
        if ( back[0].seq != 2147483647 ) { printf( "seq=%d\n", (int) back[0].seq ); return 1; }
    }
`)
}

// nested-keyed-arrays/c/valid-data — "Nested enum-keyed arrays: valid-data
// write/read acceptance", contract "ALGORITHM §7 keyed corpus". A keyed array
// (`rows`) is nested inside the element table of another keyed array, so both
// levels land exact values: row i slot j is 1000*i+j. A reader that flattens
// the two levels, or reuses one index for both, names the pair it moved.
func cT10NestedKeyedArrays(t *testing.T) {
	cT10Run(t, `package probe
enum Tier { Bronze, Silver, Gold }
fixed table Cell
{
    v int32 = 5
}
fixed table Row
{
    cells [Tier]Cell
}
fixed table Grid
{
    rows [Tier]Row
    seq  int32 = 0
}
`, "Grid", `
    {
        int i;
        int j;
        for ( i = 0; i < TIER_MAX; ++i )
        {
            for ( j = 0; j < TIER_MAX; ++j ) { v.rows[i].cells[j].v = 1000 * i + j; }
        }
        v.seq = 2147483647;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        for ( i = 0; i < TIER_MAX; ++i )
        {
            for ( j = 0; j < TIER_MAX; ++j )
                if ( back[0].rows[i].cells[j].v != 1000 * i + j )
                    { printf( "nested slot %d,%d is %d, not %d\n", i, j, back[0].rows[i].cells[j].v, 1000 * i + j ); return 1; }
        }
        if ( back[0].seq != 2147483647 ) { printf( "seq=%d\n", (int) back[0].seq ); return 1; }
    }
`)
}

// union-arrays/c/valid-data — "Arrays of unions: valid-data write/read
// acceptance", contract "§2.6; §3.4 [N]T / [Min..Max]T". Three union slots take
// the two direct scalar/struct arms in distinct order and each arm's payload is
// a distinct non-default value, so a reader that reads one tag for the array or
// one arm's payload for another names the slot it moved.
func cT10UnionArrays(t *testing.T) {
	cT10Run(t, `package probe
type Alpha { m int32 = 0 }
type Beta  { n int32 = 0 }
union Pick
{
    alpha Alpha
    beta  Beta
}
fixed table Lineage
{
    picks [3]Pick
    seq   int32 = 0
}
`, "Lineage", `
    {
        v.picks[0].type = PICK_TYPE_ALPHA; v.picks[0].as.alpha.m = 111;
        v.picks[1].type = PICK_TYPE_BETA;  v.picks[1].as.beta.n  = 222;
        v.picks[2].type = PICK_TYPE_ALPHA; v.picks[2].as.alpha.m = 333;
        v.seq = 2147483647;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].picks[0].type != PICK_TYPE_ALPHA || back[0].picks[0].as.alpha.m != 111 )
            { printf( "pick 0\n" ); return 1; }
        if ( back[0].picks[1].type != PICK_TYPE_BETA || back[0].picks[1].as.beta.n != 222 )
            { printf( "pick 1\n" ); return 1; }
        if ( back[0].picks[2].type != PICK_TYPE_ALPHA || back[0].picks[2].as.alpha.m != 333 )
            { printf( "pick 2\n" ); return 1; }
        if ( back[0].seq != 2147483647 ) { printf( "seq=%d\n", (int) back[0].seq ); return 1; }
    }
`)
}
