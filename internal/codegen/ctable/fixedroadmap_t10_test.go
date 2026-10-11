package ctable

// TestFixedRoadmapCT10FloatsUnions is the c leg's card
// schema-c-t10-floats-unions, rows enum-values, utf16-values, byte-buffers,
// keyed-arrays, nested-keyed-arrays, union-field-arms, payload-free-arms and
// union-arrays of docs/roadmap.sexp's node `fixed-tables`. One subtest per task
// id, table-driven, t.Parallel() first.
//
// A [capability] row is satisfied when the leg WRITES the construct with
// non-default and boundary values through the fixed form's own writer and READS
// it back through the fixed form's own reader, asserted by name. Each subtest
// GENERATES the C unit for a definition carrying the construct, compiles a
// probe that calls `<snake>_fixed_save` and then `<snake>_fixed_load`, and
// fails by name when a landed value disagrees with what was written.
//
// The fixed table header is self-contained for scalar, text, bytes, enum and
// union storage (only the 128-bit lane reaches serialize.h), so the probe needs
// no sibling runtime and no corpus: the capability is this leg's own writer and
// reader. Every assertion runs against the C this leg EMITS, not against a
// source grep.
//
// The law is docs/SPEC-TABLES.md §3.4: "A FIXED-TABLE RECORD IS AN EIGHT-BYTE
// HASH OF THE WRITER'S LAYOUT, AND THEN THE VALUES IN DECLARED ORDER, EVERY
// FIELD AT ITS BOUND", and the kind-by-kind constant-size table that follows it
// (enum ordinal, wstring code units, bytes, `[Enum]T`, a union's tag-then-arm
// and `[N]T`), plus §2.6 "An ARRAY OF UNIONS is a union slot per element".

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

// cT10Run generates the packet and table modules for an inline unit, writes a
// probe that saves and loads one record through the leg's own fixed form, and
// compiles and runs it. @S@ expands to the root's snake-case prefix. A nonzero
// probe exit is a failure by name, printed to stderr.
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
	}
	for _, r := range rows {
		t.Run(r.id, func(t *testing.T) {
			t.Parallel()
			r.run(t)
		})
	}
}

// enum-values/c/valid-data — "Enums and None: valid-data write/read
// acceptance" (docs/roadmap.sexp). §3.4's constant-size table: "an ENUM — the
// ordinal's storage width, 1, 2 or 4; the ORDINAL is the variant's POSITION IN
// THE LAYOUT, from 1, and 0 is None". The enum lands on the ZERO ORDINAL that
// names no variant (None) and on the last declared variant (Platinum), the
// scalar after it on INT32_MAX, so a mislaid ordinal width or a store that
// skips None moves a neighbour.
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

    v.tier = TIER_NONE;
    v.seq = 2147483646;
    n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
    if ( n < 0 ) { printf( "save failed\n" ); return 1; }
    if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
        { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
    if ( back[0].tier != TIER_NONE ) { printf( "none tier=%d\n", (int) back[0].tier ); return 1; }
    if ( back[0].seq != 2147483646 ) { printf( "none seq=%d\n", (int) back[0].seq ); return 1; }
`)
}

// utf16-values/c/valid-data — "Bounded UTF-16 string fields: valid-data
// write/read acceptance". §3.4: "`wstring(N)` — 4 (the length in CODE UNITS,
// int32 LE) + 2N bytes — the length is in UTF-16 CODE UNITS and the payload is
// two bytes each". The text fills the bound (16 code units), a surrogate pair
// first so a reader that treats code units as scalars moves a neighbour, and
// every other code unit distinct.
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
// acceptance". §3.4: "`bytes(N)` — 4 (the length in BYTES, int32 LE) + N bytes".
// The buffer fills the bound (16 bytes) and every byte is distinct, so a reader
// that truncates at a terminator or reads bytes as text moves one.
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
// acceptance" (contract §2.4; §3.4 record, [Enum]T). §3.4: "`[Enum]T` — slots
// * C(T) — every slot is written, in the key enum's declared order, and no key
// rides". EVERY slot of the `[Tier]Qty` array is live: all four slots carry
// distinct non-default values, so a leg that elided a slot or read a count
// moves one.
func cT10KeyedArrays(t *testing.T) {
	cT10Run(t, `package probe
enum Tier { Bronze, Silver, Gold, Platinum }
type Qty { n uint32 = 0 }
fixed table Lineage
{
    slots [Tier]Qty
    seq   int32 = 0
}
`, "Lineage", `
    {
        int k;
        static const uint32_t want[4] = { 0x11111111u, 0x22222222u, 0x33333333u, 0xFFFFFFFFu };
        for ( k = 0; k < 4; ++k ) { v.slots[k].n = want[k]; }
        v.seq = 2147483647;
        n = @S@_fixed_save( &v, 1, buf, (int64_t) sizeof( buf ) );
        if ( n < 0 ) { printf( "save failed\n" ); return 1; }
        if ( @S@_fixed_load( back, 1, buf, n, plan, 4096, NULL, &r ) != 1 )
            { printf( "load refused=%d malformed=%d reason=%d\n", r.refused, r.malformed, (int) r.reason ); return 1; }
        if ( back[0].seq != 2147483647 ) { printf( "seq=%d\n", (int) back[0].seq ); return 1; }
        for ( k = 0; k < 4; ++k )
            if ( back[0].slots[k].n != want[k] )
                { printf( "slot %d is %08x, want %08x\n", k, back[0].slots[k].n, want[k] ); return 1; }
    }
`)
}
