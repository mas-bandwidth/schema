package c

// A fixed [N]uint8 field after a union field has no statically known bit
// position: the union's tag width plus its selected arm is the exit, and the
// arm is not fixed at generation time. The bulk-bytes path aligns to the next
// byte first, so it may only be used where the position is provably already
// aligned. This test pins the emission and the wire for a [4]uint8 field
// following a union at None, where the tag is one bit (SPEC §4.3, §4.8).

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

const unionByteArraySource = `package unionbytes

type Small { v uint8 | min = 0, max = 127 }
union U { a Small }
type T {
    align
    u U
    data [4]uint8
}
`

// unionByteArrayLayout is the SPEC's per-byte layout for T with the union at
// None and data = {1, 2, 3, 4}. The one-bit tag occupies bit 0, so data[0]
// straddles bits 1..8, data[1] bits 9..16, and so on; the final seven bits are
// zero padding. Bit i of the stream lives in byte i/8 at bit i%8 (§4.3).
var unionByteArrayLayout = [5]byte{0x02, 0x04, 0x06, 0x08, 0x00}

func TestFixedByteArrayAfterUnionWireC(t *testing.T) {
	u := unitFromSource(t, "UnionBytes.schema", unionByteArraySource)
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	wire := string(files["UnionBytesWire.h"])

	for _, d := range u.Files[0].Decls {
		st, ok := d.(*ir.Struct)
		if !ok || st.Name != "T" {
			continue
		}
		var data *ir.Field
		for _, f := range st.Fields {
			if f.Name == "data" {
				data = f
			}
		}
		if data == nil {
			t.Fatal("T has no data field")
		}
		if ir.AlignedFixedByteArrays(st)[data] {
			t.Error("the alignment analysis must not mark data after a union field")
		}
	}

	body := func(name string) string {
		t.Helper()
		i := strings.Index(wire, "int "+name+"( ")
		if i < 0 {
			t.Fatalf("generated C has no %s function:\n%s", name, wire)
		}
		b, _, ok := strings.Cut(wire[i:], "\n}\n")
		if !ok {
			t.Fatalf("%s body is unterminated", name)
		}
		return b
	}

	writeBody := body("write_t")
	if strings.Contains(writeBody, "serialize_write_bytes( stream, value->data") {
		t.Errorf("write_t bulk-copied data after a union field, which moves its bytes:\n%s", writeBody)
	}
	if !strings.Contains(writeBody, "serialize_write_bits( stream, (serialize_uint32_t) value->data[i], 8 )") {
		t.Errorf("write_t must encode data one byte at a time:\n%s", writeBody)
	}

	readBody := body("read_t")
	if strings.Contains(readBody, "serialize_read_bytes( stream, value->data") {
		t.Errorf("read_t bulk-copied data after a union field, which moves its bytes:\n%s", readBody)
	}
	if !strings.Contains(readBody, "serialize_read_bits( stream, &raw, 8 )") {
		t.Errorf("read_t must decode data one byte at a time:\n%s", readBody)
	}

	t.Run("round trip through the C runtime", func(t *testing.T) {
		t.Parallel()
		out, err := runUnionByteArrayProbe(t, files)
		if err != nil {
			t.Fatalf("%v\n%s", err, out)
		}
		if !strings.Contains(string(out), "T round trip: 5 bytes") {
			t.Fatalf("the probe did not report its round trip:\n%s", out)
		}
	})
}

// runUnionByteArrayProbe writes the generated files and a probe that writes T
// with the union at None and data = {1, 2, 3, 4}, checks the wire bytes
// against unionByteArrayLayout, reads them back, and re-encodes.
func runUnionByteArrayProbe(t *testing.T, files map[string][]byte) ([]byte, error) {
	t.Helper()
	slowtest.Gate(t, "the c compiler and the serialize.c sibling")
	serialize, ok := setterSerializeDir(t)
	if !ok {
		if os.Getenv("SCHEMA_REQUIRE_CORPUS") != "" {
			t.Fatalf("serialize.h is not staged at %s (SERIALIZE_C, else the sibling checkout); SCHEMA_REQUIRE_CORPUS says this probe must run", serialize)
		}
		t.Skipf("serialize.h is not staged at %s (SERIALIZE_C, else the sibling checkout): the probe cannot compile", serialize)
	}

	dir := t.TempDir()
	for name, data := range files {
		if strings.Contains(name, "/") || !strings.HasSuffix(name, ".h") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	var expected strings.Builder
	for i, b := range unionByteArrayLayout {
		if i > 0 {
			expected.WriteString(", ")
		}
		fmt.Fprintf(&expected, "0x%02X", b)
	}
	probe := strings.NewReplacer("{{EXPECTED}}", expected.String()).Replace(unionByteArrayProbe)
	if err := os.WriteFile(filepath.Join(dir, "probe.c"), []byte(probe), 0o600); err != nil {
		t.Fatal(err)
	}

	cc := os.Getenv("CC")
	if cc == "" {
		cc = "cc"
	}
	args := []string{"-std=c99", "-Wall", "-Wextra", "-Werror", "-Wtype-limits", "-O2", "-ffp-contract=off",
		"-I" + dir, "-I" + serialize, filepath.Join(dir, "probe.c"), filepath.Join(serialize, "serialize.c"),
		"-o", filepath.Join(dir, "probe"), "-lm"}
	if out, err := exec.Command(cc, args...).CombinedOutput(); err != nil {
		return out, fmt.Errorf("the union byte-array probe did not compile: %w", err)
	}
	return exec.Command(filepath.Join(dir, "probe")).CombinedOutput()
}

const unionByteArrayProbe = `
#include <stdio.h>
#include <string.h>
#include <stdint.h>
#include "UnionBytesWire.h"

static int failed = 0;

static void check( int condition, const char * message )
{
    if ( !condition )
    {
        printf( "FAILED: %s\n", message );
        failed = 1;
    }
}

int main( void )
{
    T value, back;
    serialize_write_stream_t w;
    serialize_read_stream_t r;
    uint64_t sent[4], again[4];
    const uint8_t expected[5] = { {{EXPECTED}} };
    int sent_bytes;

    memset( &value, 0, sizeof( value ) );
    value.u.type = U_TYPE_NONE;
    value.data[0] = 1;
    value.data[1] = 2;
    value.data[2] = 3;
    value.data[3] = 4;

    memset( sent, 0, sizeof( sent ) );
    serialize_write_stream_init( &w, (uint8_t *) sent, sizeof( sent ) );
    check( write_t( &w, &value ), "T writes" );
    serialize_write_flush( &w );
    sent_bytes = serialize_write_bytes_processed( &w );
    check( sent_bytes == 5, "T is five bytes on the wire (1 tag bit + 32 data bits + 7 pad)" );
    check( memcmp( sent, expected, sizeof( expected ) ) == 0, "T writes the SPEC per-byte layout" );

    memset( &back, 0xA5, sizeof( back ) );
    serialize_read_stream_init( &r, (const uint8_t *) sent, sent_bytes );
    check( read_t( &r, &back ), "T reads back" );
    check( back.u.type == U_TYPE_NONE, "the union reads back at None" );
    check( memcmp( back.data, value.data, sizeof( value.data ) ) == 0, "data reads back" );

    memset( again, 0, sizeof( again ) );
    serialize_write_stream_init( &w, (uint8_t *) again, sizeof( again ) );
    check( write_t( &w, &back ), "T re-writes" );
    serialize_write_flush( &w );
    check( serialize_write_bytes_processed( &w ) == sent_bytes && memcmp( again, sent, sent_bytes ) == 0, "T round-trips byte-identical" );

    if ( failed )
    {
        return 1;
    }
    printf( "T round trip: %d bytes\n", sent_bytes );
    return 0;
}
`
