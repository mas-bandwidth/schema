package mootable_test

// THE FIXED FORM'S VALUE ROWS ON THE MOO LEG, PROVED AGAINST A LIVE C++
// REFERENCE (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md). Each row
// is one schema and one set of record values, stated twice: once as C++ that
// the reference (cpp.Generate + cpptable.GenerateLineage) compiles and saves,
// and once as moo that builds the same records through the emitted
// constructors and saves them through table_fixed.moo. The contract is byte
// equality: moo's file is the reference's file byte for byte, moo loads the
// reference's file clean and lands every value the row names, moo's re-save
// of what it loaded is the reference's file again, and the reference loads
// moo's file clean and re-saves it to its own bytes.

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// mooFixedValueCase is one value row: the schema, the root and its body size,
// the records as C++ statements over value[0..count) (each reset first) and
// as moo statements that append to `values`, and the key=value lines (the
// fixedDumper's spelling) the load of the reference's file must land.
type mooFixedValueCase struct {
	id       string
	schema   string
	root     string
	body     int64
	count    int
	cppSetup string
	build    string
	want     []string
}

// mooI64 and mooU64 are a 64-bit value as moo's serialize class constructor,
// its two 32-bit limbs least significant first.
func mooI64(x int64) string {
	return fmt.Sprintf("serialize.Int64.new(0x%08x, 0x%08x)", uint32(uint64(x)), uint32(uint64(x)>>32))
}

func mooU64(x uint64) string {
	return fmt.Sprintf("serialize.Uint64.new(0x%08x, 0x%08x)", uint32(x), uint32(x>>32))
}

// mooF32 is a float32 as moo stores it, its bit pattern; mooF64 a float64,
// its bit pattern as a Uint64.
func mooF32(x float32) string { return fmt.Sprintf("0x%08x", math.Float32bits(x)) }
func mooF64(x float64) string { return mooU64(math.Float64bits(x)) }

// dumpF32 and dumpF64 are the fixedDumper's spelling of a float's bits.
func dumpF32(x float32) string { return fmt.Sprintf("0x%08X", math.Float32bits(x)) }
func dumpF64(x float64) string { return fmt.Sprintf("0x%016X", math.Float64bits(x)) }

func mooFixedValueCases() []mooFixedValueCase {
	return []mooFixedValueCase{
		// port/moo/fixed/bool-values
		{
			id: "bool-values",
			schema: `package probe
fixed table T {
    a bool = false
    b bool = true
}
`,
			root:  "T",
			body:  2,
			count: 2,
			cppSetup: `
    value[1].a = true;
    value[1].b = false;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = true
v1.B = false
values = { v0, v1 }
`,
			want: []string{"r0.a=0", "r0.b=1", "r1.a=1", "r1.b=0"},
		},
		// port/moo/fixed/signed-integers
		{
			id: "signed-integers",
			schema: `package probe
fixed table T {
    a int8 = -128
    b int16 = -32768
    c int32 = -2147483648
    d int64 = -9223372036854775808
}
`,
			root:  "T",
			body:  15,
			count: 3,
			cppSetup: `
    value[1].a = 127;
    value[1].b = 32767;
    value[1].c = 2147483647;
    value[1].d = 9223372036854775807LL;
    value[2].a = -1;
    value[2].b = 0;
    value[2].c = 1;
    value[2].d = -2LL;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = 127
v1.B = 32767
v1.C = 2147483647
v1.D = ` + mooI64(math.MaxInt64) + `
local v2 = T.NewT()
v2.A = -1
v2.B = 0
v2.C = 1
v2.D = ` + mooI64(-2) + `
values = { v0, v1, v2 }
`,
			want: []string{
				"r0.a=-128", "r0.b=-32768", "r0.c=-2147483648", "r0.d=-9223372036854775808",
				"r1.a=127", "r1.b=32767", "r1.c=2147483647", "r1.d=9223372036854775807",
				"r2.a=-1", "r2.b=0", "r2.c=1", "r2.d=-2",
			},
		},
		// port/moo/fixed/unsigned-integers
		{
			id: "unsigned-integers",
			schema: `package probe
fixed table T {
    a uint8 = 255
    b uint16 = 65535
    c uint32 = 4294967295
    d uint64 = 18446744073709551615
}
`,
			root:  "T",
			body:  15,
			count: 3,
			cppSetup: `
    value[1].a = 0;
    value[1].b = 0;
    value[1].c = 0;
    value[1].d = 0ULL;
    value[2].a = 1;
    value[2].b = 256;
    value[2].c = 65536U;
    value[2].d = 4294967296ULL;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = 0
v1.B = 0
v1.C = 0
v1.D = ` + mooU64(0) + `
local v2 = T.NewT()
v2.A = 1
v2.B = 256
v2.C = 65536
v2.D = ` + mooU64(4294967296) + `
values = { v0, v1, v2 }
`,
			want: []string{
				"r0.a=255", "r0.b=65535", "r0.c=4294967295", "r0.d=18446744073709551615",
				"r1.a=0", "r1.b=0", "r1.c=0", "r1.d=0",
				"r2.a=1", "r2.b=256", "r2.c=65536", "r2.d=4294967296",
			},
		},
		// port/moo/fixed/signed-128
		{
			id: "signed-128",
			schema: `package probe
fixed table T {
    a int128 = -170141183460469231731687303715884105728 | min = -170141183460469231731687303715884105728, max = 170141183460469231731687303715884105727
}
`,
			root:  "T",
			body:  16,
			count: 3,
			cppSetup: `
    value[1].a = serialize::int128_t( ( serialize::uint128_t( 0x7fffffffffffffffull ) << 64 ) | serialize::uint128_t( 0xffffffffffffffffull ) );
    value[2].a = serialize::int128_t( 1 );
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = serialize.Int128.new(0xffffffff, 0xffffffff, 0xffffffff, 0x7fffffff)
local v2 = T.NewT()
v2.A = serialize.Int128.new(1, 0, 0, 0)
values = { v0, v1, v2 }
`,
			want: []string{
				"r0.a=-170141183460469231731687303715884105728",
				"r1.a=170141183460469231731687303715884105727",
				"r2.a=1",
			},
		},
		// port/moo/fixed/unsigned-128
		{
			id: "unsigned-128",
			schema: `package probe
fixed table T {
    a uint128 = 340282366920938463463374607431768211455
}
`,
			root:  "T",
			body:  16,
			count: 3,
			cppSetup: `
    value[1].a = serialize::uint128_t( 0 );
    value[2].a = serialize::uint128_t( 1 );
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = serialize.Uint128.new(0, 0, 0, 0)
local v2 = T.NewT()
v2.A = serialize.Uint128.new(1, 0, 0, 0)
values = { v0, v1, v2 }
`,
			want: []string{"r0.a=340282366920938463463374607431768211455", "r1.a=0", "r2.a=1"},
		},
		// port/moo/fixed/ranged-integers
		{
			id: "ranged-integers",
			schema: `package probe
fixed table T {
    a int32 | min = -5, max = 5
    b int64 | min = -1000000000000, max = 1000000000000
    c uint16 | min = 0, max = 1000
}
`,
			root:  "T",
			body:  14,
			count: 2,
			cppSetup: `
    value[0].a = -5;
    value[0].b = -1000000000000LL;
    value[0].c = 0;
    value[1].a = 5;
    value[1].b = 1000000000000LL;
    value[1].c = 1000;
`,
			build: `
local v0 = T.NewT()
v0.A = -5
v0.B = ` + mooI64(-1000000000000) + `
v0.C = 0
local v1 = T.NewT()
v1.A = 5
v1.B = ` + mooI64(1000000000000) + `
v1.C = 1000
values = { v0, v1 }
`,
			want: []string{"r0.a=-5", "r0.b=-1000000000000", "r0.c=0", "r1.a=5", "r1.b=1000000000000", "r1.c=1000"},
		},
		// port/moo/fixed/bits-values
		{
			id: "bits-values",
			schema: `package probe
fixed table T {
    a bits(3)
    b bits(12)
    c bits(20)
    d bits(40)
}
`,
			root:  "T",
			body:  20,
			count: 2,
			cppSetup: `
    value[0].a = 7;
    value[0].b = 4095;
    value[0].c = 1048575U;
    value[0].d = 1099511627775ULL;
    value[1].a = 0;
    value[1].b = 0;
    value[1].c = 0;
    value[1].d = 0ULL;
`,
			build: `
local v0 = T.NewT()
v0.A = 7
v0.B = 4095
v0.C = 1048575
v0.D = ` + mooU64(1099511627775) + `
local v1 = T.NewT()
values = { v0, v1 }
`,
			want: []string{"r0.a=7", "r0.b=4095", "r0.c=1048575", "r0.d=1099511627775", "r1.a=0", "r1.b=0", "r1.c=0", "r1.d=0"},
		},
		// port/moo/fixed/float32-values
		{
			id: "float32-values",
			schema: `package probe
fixed table T {
    a float32 = 3.5
    b float32 = -0.5
}
`,
			root:  "T",
			body:  8,
			count: 2,
			cppSetup: `
    value[1].a = -1024.25f;
    value[1].b = 2048.75f;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = ` + mooF32(-1024.25) + `
v1.B = ` + mooF32(2048.75) + `
values = { v0, v1 }
`,
			want: []string{"r0.a=" + dumpF32(3.5), "r0.b=" + dumpF32(-0.5), "r1.a=" + dumpF32(-1024.25), "r1.b=" + dumpF32(2048.75)},
		},
		// port/moo/fixed/float64-values
		{
			id: "float64-values",
			schema: `package probe
fixed table T {
    a float64 = 2.5
    b float64 = -1234.5
}
`,
			root:  "T",
			body:  16,
			count: 2,
			cppSetup: `
    value[1].a = -987654321.125;
    value[1].b = 123456789.875;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = ` + mooF64(-987654321.125) + `
v1.B = ` + mooF64(123456789.875) + `
values = { v0, v1 }
`,
			want: []string{"r0.a=" + dumpF64(2.5), "r0.b=" + dumpF64(-1234.5), "r1.a=" + dumpF64(-987654321.125), "r1.b=" + dumpF64(123456789.875)},
		},
		// port/moo/fixed/compressed-floats
		{
			id: "compressed-floats",
			schema: `package probe
fixed table T {
    a float32 = 0.5 | min = 0.0, max = 1.0, resolution = 0.01
    b float32 = 0.5 | min = -1.0, max = 1.0, resolution = 0.001
}
`,
			root:  "T",
			body:  8,
			count: 3,
			cppSetup: `
    value[0].a = 0.25f;
    value[0].b = -0.5f;
    value[1].a = 0.0f;
    value[1].b = -1.0f;
    value[2].a = 1.0f;
    value[2].b = 1.0f;
`,
			build: `
local v0 = T.NewT()
v0.A = ` + mooF32(0.25) + `
v0.B = ` + mooF32(-0.5) + `
local v1 = T.NewT()
v1.A = ` + mooF32(0) + `
v1.B = ` + mooF32(-1) + `
local v2 = T.NewT()
v2.A = ` + mooF32(1) + `
v2.B = ` + mooF32(1) + `
values = { v0, v1, v2 }
`,
			want: []string{
				"r0.a=" + dumpF32(0.25), "r0.b=" + dumpF32(-0.5),
				"r1.a=" + dumpF32(0), "r1.b=" + dumpF32(-1),
				"r2.a=" + dumpF32(1), "r2.b=" + dumpF32(1),
			},
		},
		// port/moo/fixed/signed-fixed-point
		{
			id: "signed-fixed-point",
			schema: `package probe
fixed table T {
    a fixed(16, 16) | min = -8, max = 8
    b fixed(48, 16) | min = -100, max = 100
    c fixed(112, 16) | min = -1000, max = 1000
}
`,
			root:  "T",
			body:  28,
			count: 2,
			cppSetup: `
    value[0].a = -( 8 << 16 );
    value[0].b = -( 100LL << 16 );
    value[0].c = serialize::int128_t( ( serialize::uint128_t( 0xffffffffffffffffull ) << 64 ) | serialize::uint128_t( 0xffffffffff000000ull ) );
    value[1].a = ( 8 << 16 );
    value[1].b = ( 100LL << 16 );
    value[1].c = serialize::int128_t( 0x0000000000fa0000ull );
`,
			// a and b are moo numbers, the Q48.16 value itself; c is the raw
			// Q112.16 integer in an Int128
			build: `
local v0 = T.NewT()
v0.A = -8
v0.B = -100
v0.C = serialize.Int128.new(0xff000000, 0xffffffff, 0xffffffff, 0xffffffff)
local v1 = T.NewT()
v1.A = 8
v1.B = 100
v1.C = serialize.Int128.new(0x00fa0000, 0, 0, 0)
values = { v0, v1 }
`,
			want: []string{
				"r0.a=raw=-524288", "r0.b=raw=-6553600", "r0.c=raw=-16777216",
				"r1.a=raw=524288", "r1.b=raw=6553600", "r1.c=raw=16384000",
			},
		},
		// port/moo/fixed/unsigned-fixed-point
		{
			id: "unsigned-fixed-point",
			schema: `package probe
fixed table T {
    a ufixed(16, 16) | min = 0, max = 16
    b ufixed(48, 16) | min = 0, max = 100
    c ufixed(112, 16) | min = 0, max = 1000
}
`,
			root:  "T",
			body:  28,
			count: 2,
			cppSetup: `
    value[0].a = 0;
    value[0].b = 0ULL;
    value[0].c = serialize::uint128_t( 0 );
    value[1].a = ( 16U << 16 );
    value[1].b = ( 100ULL << 16 );
    value[1].c = serialize::uint128_t( 1000ULL << 16 );
`,
			build: `
local v0 = T.NewT()
v0.A = 0
v0.B = 0
v0.C = serialize.Uint128.new(0, 0, 0, 0)
local v1 = T.NewT()
v1.A = 16
v1.B = 100
v1.C = serialize.Uint128.new(0x03e80000, 0, 0, 0)
values = { v0, v1 }
`,
			want: []string{
				"r0.a=raw=0", "r0.b=raw=0", "r0.c=raw=0",
				"r1.a=raw=1048576", "r1.b=raw=6553600", "r1.c=raw=65536000",
			},
		},
		// port/moo/fixed/flags-values
		{
			id: "flags-values",
			schema: `package probe
flags Caps { Jump, Fly, Swim }
fixed table T { caps Caps }
`,
			root:  "T",
			body:  8,
			count: 3,
			cppSetup: `
    value[0].caps = 0;
    value[1].caps = 5;
    value[2].caps = 7;
`,
			build: `
local v0 = T.NewT()
v0.Caps = ` + mooU64(0) + `
local v1 = T.NewT()
v1.Caps = ` + mooU64(5) + `
local v2 = T.NewT()
v2.Caps = ` + mooU64(7) + `
values = { v0, v1, v2 }
`,
			want: []string{"r0.caps=0", "r1.caps=5", "r2.caps=7"},
		},
		// port/moo/fixed/enum-values
		{
			id: "enum-values",
			schema: `package probe
enum Grade { Gold, Silver, Bronze }
fixed table T { g Grade }
`,
			root:  "T",
			body:  1,
			count: 3,
			cppSetup: `
    value[0].g = Grade::None;
    value[1].g = Grade::Gold;
    value[2].g = Grade::Bronze;
`,
			build: `
local v0 = T.NewT()
v0.G = 0
local v1 = T.NewT()
v1.G = 1
local v2 = T.NewT()
v2.G = 3
values = { v0, v1, v2 }
`,
			want: []string{"r0.g=0", "r1.g=1", "r2.g=3"},
		},
		// port/moo/fixed/utf8-values
		{
			id: "utf8-values",
			schema: `package probe
fixed table T { s string(8) }
`,
			root:  "T",
			body:  12,
			count: 2,
			cppSetup: `
    memcpy( value[1].s, "hello", 5 );
    value[1].s_length = 5;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.S = "hello"
values = { v0, v1 }
`,
			want: []string{`r0.s=""`, "r0.s_length=0", `r1.s="hello"`, "r1.s_length=5"},
		},
		// port/moo/fixed/byte-buffers
		{
			id: "byte-buffers",
			schema: `package probe
fixed table T { blob bytes(6) }
`,
			root:  "T",
			body:  10,
			count: 2,
			cppSetup: `
    value[1].blob[0] = 0xff;
    value[1].blob[1] = 0x00;
    value[1].blob[2] = 0x5a;
    value[1].blob[3] = 1;
    value[1].blob[4] = 2;
    value[1].blob[5] = 3;
    value[1].blob_length = 6;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
buffer.writestring(v1.Blob, 0, "\xff\x00\x5a\x01\x02\x03")
v1.BlobLength = 6
values = { v0, v1 }
`,
			want: []string{
				"r0.blob_length=0", "r1.blob_length=6",
				"r1.blob[0]=255", "r1.blob[1]=0", "r1.blob[2]=90", "r1.blob[3]=1", "r1.blob[4]=2", "r1.blob[5]=3",
			},
		},
		// port/moo/fixed/nested-types
		{
			id: "nested-types",
			schema: `package probe
type Badge { title string(4) }
fixed table T { badge Badge }
`,
			root:  "T",
			body:  8,
			count: 1,
			cppSetup: `
    value[0].badge.title[0] = 'h';
    value[0].badge.title[1] = 'i';
    value[0].badge.title_length = 2;
`,
			build: `
local v = T.NewT()
v.Badge.Title = "hi"
values = { v }
`,
			want: []string{`r0.badge.title="hi"`, "r0.badge.title_length=2"},
		},
		// port/moo/fixed/nested-fixed-tables
		{
			id: "nested-fixed-tables",
			schema: `package probe
fixed table Child { n int32 }
fixed table T { child Child }
`,
			root:  "T",
			body:  4,
			count: 2,
			cppSetup: `
    value[0].child.n = -2147483647 - 1;
    value[1].child.n = 2147483647;
`,
			build: `
local v0 = T.NewT()
v0.Child.N = -2147483648
local v1 = T.NewT()
v1.Child.N = 2147483647
values = { v0, v1 }
`,
			want: []string{"r0.child.n=-2147483648", "r1.child.n=2147483647"},
		},
		// port/moo/fixed/fixed-arrays
		{
			id: "fixed-arrays",
			schema: `package probe
fixed table T { tag [3]int32 }
`,
			root:  "T",
			body:  12,
			count: 1,
			cppSetup: `
    value[0].tag[0] = -2147483647 - 1;
    value[0].tag[1] = 0;
    value[0].tag[2] = 2147483647;
`,
			build: `
local v = T.NewT()
v.Tag[1] = -2147483648
v.Tag[2] = 0
v.Tag[3] = 2147483647
values = { v }
`,
			want: []string{"r0.tag[0]=-2147483648", "r0.tag[1]=0", "r0.tag[2]=2147483647"},
		},
		// port/moo/fixed/counted-arrays
		{
			id: "counted-arrays",
			schema: `package probe
fixed table T { nums [..4]uint16 }
`,
			root:  "T",
			body:  12,
			count: 2,
			cppSetup: `
    value[1].nums[0] = 10;
    value[1].nums[1] = 65535;
    value[1].nums[2] = 32768;
    value[1].nums_count = 3;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.Nums[1] = 10
v1.Nums[2] = 65535
v1.Nums[3] = 32768
v1.NumsCount = 3
values = { v0, v1 }
`,
			want: []string{"r0.nums_count=0", "r1.nums_count=3", "r1.nums[0]=10", "r1.nums[1]=65535", "r1.nums[2]=32768"},
		},
		// port/moo/fixed/keyed-arrays
		{
			id: "keyed-arrays",
			schema: `package probe
enum Key { A, B }
fixed table T { slots [Key]int32 }
`,
			root:  "T",
			body:  8,
			count: 1,
			cppSetup: `
    value[0].slots[Key::A] = -2147483647 - 1;
    value[0].slots[Key::B] = 2147483647;
`,
			build: `
local v = T.NewT()
v.Slots[1] = -2147483648
v.Slots[2] = 2147483647
values = { v }
`,
			want: []string{"r0.slots[0]=-2147483648", "r0.slots[1]=2147483647"},
		},
		// port/moo/fixed/nested-keyed-arrays
		{
			id: "nested-keyed-arrays",
			schema: `package probe
enum Key { A, B }
fixed table Child { n int32 }
fixed table T { slots [Key]Child }
`,
			root:  "T",
			body:  8,
			count: 1,
			cppSetup: `
    value[0].slots[Key::A].n = 11;
    value[0].slots[Key::B].n = -22;
`,
			build: `
local v = T.NewT()
v.Slots[1].N = 11
v.Slots[2].N = -22
values = { v }
`,
			want: []string{"r0.slots[0].n=11", "r0.slots[1].n=-22"},
		},
		// port/moo/fixed/union-values
		{
			id: "union-values",
			schema: `package probe
type Vec {
    x int32
    y int32
}
fixed table Tab { n int32 }
union Body {
    v Vec
    t Tab
    n int32
}
fixed table T { body Body }
`,
			root:  "T",
			body:  9,
			count: 3,
			cppSetup: `
    value[0].body.type = BodyType::V;
    value[0].body.v.x = 11;
    value[0].body.v.y = -22;
    value[1].body.type = BodyType::T;
    value[1].body.t.n = 41;
    value[2].body.type = BodyType::N;
    value[2].body.n = 1073741821;
`,
			build: `
local v0 = T.NewT()
v0.Body.Type = 1
v0.Body.V.X = 11
v0.Body.V.Y = -22
local v1 = T.NewT()
v1.Body.Type = 2
v1.Body.T.N = 41
local v2 = T.NewT()
v2.Body.Type = 3
v2.Body.N = 1073741821
values = { v0, v1, v2 }
`,
			want: []string{
				"r0.body.type=1", "r0.body.v.x=11", "r0.body.v.y=-22",
				"r1.body.type=2", "r1.body.t.n=41",
				"r2.body.type=3", "r2.body.n=1073741821",
			},
		},
		// port/moo/fixed/union-field-arms
		{
			id: "union-field-arms",
			schema: `package probe
union Body {
    sa int32
    ta string(4)
    aa [..3]int32
}
fixed table T { body Body }
`,
			root:  "T",
			body:  17,
			count: 3,
			cppSetup: `
    value[0].body.type = BodyType::Sa;
    value[0].body.sa = -5;
    value[1].body.type = BodyType::Ta;
    memcpy( value[1].body.ta.value, "abc", 3 );
    value[1].body.ta.value_length = 3;
    value[2].body.type = BodyType::Aa;
    value[2].body.aa.value[0] = -1;
    value[2].body.aa.value[1] = 0;
    value[2].body.aa.value[2] = 1;
    value[2].body.aa.value_count = 3;
`,
			build: `
local v0 = T.NewT()
v0.Body.Type = 1
v0.Body.Sa = -5
local v1 = T.NewT()
v1.Body.Type = 2
v1.Body.Ta = "abc"
local v2 = T.NewT()
v2.Body.Type = 3
v2.Body.Aa[1] = -1
v2.Body.Aa[2] = 0
v2.Body.Aa[3] = 1
v2.Body.AaCount = 3
values = { v0, v1, v2 }
`,
			want: []string{
				"r0.body.type=1", "r0.body.sa=-5",
				"r1.body.type=2", `r1.body.ta="abc"`, "r1.body.ta_length=3",
				"r2.body.type=3", "r2.body.aa_count=3", "r2.body.aa[0]=-1", "r2.body.aa[1]=0", "r2.body.aa[2]=1",
			},
		},
		// port/moo/fixed/payload-free-arms: the payload-free arms' records
		// carry a stale Alpha in moo as in C++, so byte equality holds that
		// both writers zero an unselected arm's payload.
		{
			id: "payload-free-arms",
			schema: `package probe
union Pick {
    alpha int32
    quiet
    signal
}
fixed table T {
    lead int32
    pick Pick
    trail int32
}
`,
			root:  "T",
			body:  13,
			count: 3,
			cppSetup: `
    value[0].lead = 10;
    value[0].pick.type = PickType::Alpha;
    value[0].pick.alpha = 99;
    value[0].trail = 20;
    value[1].lead = 30;
    value[1].pick.alpha = 777;
    value[1].pick.type = PickType::Quiet;
    value[1].trail = 40;
    value[2].lead = 50;
    value[2].pick.alpha = 888;
    value[2].pick.type = PickType::Signal;
    value[2].trail = 60;
`,
			build: `
local v0 = T.NewT()
v0.Lead = 10
v0.Pick.Type = 1
v0.Pick.Alpha = 99
v0.Trail = 20
local v1 = T.NewT()
v1.Lead = 30
v1.Pick.Alpha = 777
v1.Pick.Type = 2
v1.Trail = 40
local v2 = T.NewT()
v2.Lead = 50
v2.Pick.Alpha = 888
v2.Pick.Type = 3
v2.Trail = 60
values = { v0, v1, v2 }
`,
			want: []string{
				"r0.lead=10", "r0.pick.type=1", "r0.pick.alpha=99", "r0.trail=20",
				"r1.lead=30", "r1.pick.type=2", "r1.trail=40",
				"r2.lead=50", "r2.pick.type=3", "r2.trail=60",
			},
		},
		// port/moo/fixed/union-arrays
		{
			id: "union-arrays",
			schema: `package probe
union Pick {
    a int32
    b int64
}
fixed table T { picks [..3]Pick }
`,
			root:  "T",
			body:  31,
			count: 1,
			cppSetup: `
    value[0].picks_count = 3;
    value[0].picks[0].type = PickType::A;
    value[0].picks[0].a = 3;
    value[0].picks[1].type = PickType::B;
    value[0].picks[1].b = -1099511627776LL;
    value[0].picks[2].type = PickType::A;
    value[0].picks[2].a = 0;
`,
			build: `
local v = T.NewT()
v.PicksCount = 3
v.Picks[1].Type = 1
v.Picks[1].A = 3
v.Picks[2].Type = 2
v.Picks[2].B = ` + mooI64(-1099511627776) + `
v.Picks[3].Type = 1
v.Picks[3].A = 0
values = { v }
`,
			want: []string{
				"r0.picks_count=3",
				"r0.picks[0].type=1", "r0.picks[0].a=3",
				"r0.picks[1].type=2", "r0.picks[1].b=-1099511627776",
				"r0.picks[2].type=1", "r0.picks[2].a=0",
			},
		},
		// port/moo/fixed/optional-scalars
		{
			id: "optional-scalars",
			schema: `package probe
enum Grade { Gold, Silver }
fixed table T {
    n ?int32
    g ?Grade
}
`,
			root:  "T",
			body:  7,
			count: 2,
			cppSetup: `
    value[1].n = -123456;
    value[1].n_present = true;
    value[1].g = Grade::Silver;
    value[1].g_present = true;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.N = -123456
v1.NPresent = true
v1.G = 2
v1.GPresent = true
values = { v0, v1 }
`,
			want: []string{"r0.n_present=0", "r0.g_present=0", "r1.n_present=1", "r1.n=-123456", "r1.g_present=1", "r1.g=2"},
		},
		// port/moo/fixed/optional-nesting
		{
			id: "optional-nesting",
			schema: `package probe
type Vec {
    x int32
    y int32
}
fixed table T { p ?Vec }
`,
			root:  "T",
			body:  9,
			count: 2,
			cppSetup: `
    value[1].p.x = 99;
    value[1].p.y = -99;
    value[1].p_present = true;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.P.X = 99
v1.P.Y = -99
v1.PPresent = true
values = { v0, v1 }
`,
			want: []string{"r0.p_present=0", "r1.p_present=1", "r1.p.x=99", "r1.p.y=-99"},
		},
		// port/moo/fixed/optional-arrays
		{
			id: "optional-arrays",
			schema: `package probe
fixed table T { xs ?[..3]int32 }
`,
			root:  "T",
			body:  17,
			count: 2,
			cppSetup: `
    value[1].xs[0] = 7;
    value[1].xs[1] = -8;
    value[1].xs[2] = 9;
    value[1].xs_count = 3;
    value[1].xs_present = true;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.Xs[1] = 7
v1.Xs[2] = -8
v1.Xs[3] = 9
v1.XsCount = 3
v1.XsPresent = true
values = { v0, v1 }
`,
			want: []string{"r0.xs_present=0", "r1.xs_present=1", "r1.xs_count=3", "r1.xs[0]=7", "r1.xs[1]=-8", "r1.xs[2]=9"},
		},
		// port/moo/fixed/scalar-defaults: record 0 is the constructor's
		// defaults, untouched
		{
			id: "scalar-defaults",
			schema: `package probe
enum Grade { Gold, Silver, Bronze }
fixed table T {
    x int32 = 7
    g Grade = Silver
    f float32 = 1.5
}
`,
			root:  "T",
			body:  9,
			count: 2,
			cppSetup: `
    value[1].x = -9;
    value[1].g = Grade::Bronze;
    value[1].f = -2.25f;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.X = -9
v1.G = 3
v1.F = ` + mooF32(-2.25) + `
values = { v0, v1 }
`,
			want: []string{"r0.x=7", "r0.g=2", "r0.f=" + dumpF32(1.5), "r1.x=-9", "r1.g=3", "r1.f=" + dumpF32(-2.25)},
		},
		// port/moo/fixed/text-bytes-flags-defaults: record 0 is the
		// constructor's defaults, untouched
		{
			id: "text-bytes-flags-defaults",
			schema: `package probe
flags Caps { Jump, Fly, Swim }
fixed table T {
    s string(8) = "hi"
    b bytes(4) = "ab"
    c Caps = { Jump, Fly }
}
`,
			root:  "T",
			body:  28,
			count: 2,
			cppSetup: `
    memset( value[1].s, 0, sizeof( value[1].s ) );
    memcpy( value[1].s, "xyz", 3 );
    value[1].s_length = 3;
    value[1].b[0] = 0x00;
    value[1].b[1] = 0xff;
    value[1].b[2] = 0x5a;
    value[1].b[3] = 0x01;
    value[1].b_length = 4;
    value[1].c = 4;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.S = "xyz"
buffer.writestring(v1.B, 0, "\x00\xff\x5a\x01")
v1.BLength = 4
v1.C = ` + mooU64(4) + `
values = { v0, v1 }
`,
			want: []string{
				`r0.s="hi"`, "r0.s_length=2", "r0.b_length=2", "r0.b[0]=97", "r0.b[1]=98", "r0.c=3",
				`r1.s="xyz"`, "r1.s_length=3", "r1.b_length=4", "r1.b[0]=0", "r1.b[1]=255", "r1.b[2]=90", "r1.b[3]=1", "r1.c=4",
			},
		},
		// port/moo/fixed/record-measurement: the measure line is held for
		// every row; this row's schema is the plain two-field record
		{
			id: "record-measurement",
			schema: `package probe
fixed table T {
    a uint32 = 1
    b uint16 = 2
}
`,
			root:  "T",
			body:  6,
			count: 2,
			cppSetup: `
    value[0].a = 10;
    value[0].b = 20;
    value[1].a = 30;
    value[1].b = 40;
`,
			build: `
local v0 = T.NewT()
v0.A = 10
v0.B = 20
local v1 = T.NewT()
v1.A = 30
v1.B = 40
values = { v0, v1 }
`,
			want: []string{"r0.a=10", "r0.b=20", "r1.a=30", "r1.b=40"},
		},
	}
}

// resolveSerializeInclude is the sibling serialize checkout holding
// serialize.h, or fallbackDir with a minimal 128-bit storage header written
// into it, so the reference compiles in any layout.
func resolveSerializeInclude(fallbackDir string) string {
	for _, c := range []string{os.Getenv("SERIALIZE"), filepath.Join(root, "..", "serialize")} {
		if c == "" {
			continue
		}
		if abs, err := filepath.Abs(c); err == nil {
			if _, err := os.Stat(filepath.Join(abs, "serialize.h")); err == nil {
				return abs
			}
		}
	}
	const minSerialize = `#pragma once
#include <stdint.h>
namespace serialize {
typedef __int128 int128_t;
typedef unsigned __int128 uint128_t;
}
`
	_ = os.WriteFile(filepath.Join(fallbackDir, "serialize.h"), []byte(minSerialize), 0o600)
	return fallbackDir
}

// cppFixedOracle compiles the C++ reference for unit u (cpp.Generate and
// cpptable.GenerateLineage with no lineage), fills count records of root with
// cppSetup, saves them, and answers the reference's file and a verifier: the
// verifier loads a candidate file through the reference, requires a clean
// load of count records, requires the re-save to be the reference's own
// bytes, and requires the candidate to be those bytes.
func cppFixedOracle(t *testing.T, id string, u *ir.Unit, rootName string, count int, cppSetup string) ([]byte, func([]byte) error) {
	t.Helper()
	files, err := cpp.Generate(u)
	if err != nil {
		t.Fatalf("%s: cpp.Generate: %v", id, err)
	}
	tables, err := cpptable.GenerateLineage(u, nil)
	if err != nil {
		t.Fatalf("%s: cpptable.GenerateLineage: %v", id, err)
	}
	maps.Copy(files, tables)
	dir := t.TempDir()
	var tableHeader string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o600); err != nil {
			t.Fatalf("%s: write %s: %v", id, name, err)
		}
		if strings.HasSuffix(name, "Table.h") {
			tableHeader = name
		}
	}
	if tableHeader == "" {
		t.Fatalf("%s: no Table.h generated", id)
	}
	serDir := resolveSerializeInclude(dir)
	src := fmt.Sprintf(`#include <cstdio>
#include <cstdlib>
#include <cstring>
#include "%[4]s"

using namespace probe;

static void fill_records( %[1]s * value )
{
    for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( value[i] );
%[3]s
}

static bool clean( const TableReport & r )
{
    return !( r.damaged || r.refused || r.refuse_reason != ok || r.unknown || r.widened );
}

int main( int argc, char ** argv )
{
    static uint8_t buffer[1 << 18];
    static uint8_t cand_buf[1 << 18];
    static uint8_t re_buf[1 << 18];
    static TableFixedEntry plan[4096];
    TableReport r;

    static %[1]s value[%[2]d];
    fill_records( value );
    const int64_t need = %[1]sFixedMeasure( %[2]d );
    const int64_t saved = %[1]sFixedSave( value, %[2]d, buffer, need );
    if ( saved != need ) {
        printf( "cpp save: need=%%lld saved=%%lld\n", (long long) need, (long long) saved );
        return 1;
    }
    static %[1]s back[%[2]d];
    for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( back[i] );
    memset( &r, 0, sizeof( r ) );
    const int64_t n = %[1]sFixedLoad( back, %[2]d, buffer, saved, plan, 4096, NULL, &r );
    if ( n != %[2]d || !clean( r ) ) {
        printf( "cpp self-load failed: n=%%lld reason=%%d\n", (long long) n, (int) r.refuse_reason );
        return 1;
    }
    if ( argc >= 3 && strcmp( argv[1], "save" ) == 0 ) {
        FILE * f = fopen( argv[2], "wb" );
        if ( !f ) return 2;
        if ( (int64_t) fwrite( buffer, 1, (size_t) saved, f ) != saved ) { fclose( f ); return 3; }
        fclose( f );
        return 0;
    }
    if ( argc >= 3 && strcmp( argv[1], "verify" ) == 0 ) {
        FILE * f = fopen( argv[2], "rb" );
        if ( !f ) return 2;
        const int64_t clen = (int64_t) fread( cand_buf, 1, sizeof( cand_buf ), f );
        fclose( f );
        static %[1]s from_cand[%[2]d];
        for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( from_cand[i] );
        memset( &r, 0, sizeof( r ) );
        const int64_t cn = %[1]sFixedLoad( from_cand, %[2]d, cand_buf, clen, plan, 4096, NULL, &r );
        if ( cn != %[2]d || !clean( r ) ) {
            printf( "cpp load of the candidate failed: n=%%lld reason=%%d\n", (long long) cn, (int) r.refuse_reason );
            return 4;
        }
        const int64_t re_saved = %[1]sFixedSave( from_cand, %[2]d, re_buf, need );
        if ( re_saved != saved || memcmp( re_buf, buffer, (size_t) saved ) != 0 ) {
            printf( "cpp re-save of the candidate is not the reference's bytes\n" );
            return 5;
        }
        if ( clen != saved || memcmp( cand_buf, buffer, (size_t) saved ) != 0 ) {
            printf( "the candidate is not the reference's bytes\n" );
            return 6;
        }
        return 0;
    }
    return 1;
}
`, rootName, count, cppSetup, tableHeader)
	probeCpp := filepath.Join(dir, "probe.cpp")
	probeBin := filepath.Join(dir, "probe")
	if err := os.WriteFile(probeCpp, []byte(src), 0o600); err != nil {
		t.Fatalf("%s: write probe.cpp: %v", id, err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	if out, err := exec.Command(cxx, "-std=c++17", "-O1", "-I"+dir, "-I"+serDir, probeCpp, "-o", probeBin).CombinedOutput(); err != nil {
		t.Fatalf("%s: the C++ reference does not compile: %v\n%s", id, err, out)
	}
	cppBin := filepath.Join(dir, "cpp.bin")
	if out, err := exec.Command(probeBin, "save", cppBin).CombinedOutput(); err != nil {
		t.Fatalf("%s: the C++ reference's save failed: %v\n%s", id, err, out)
	}
	cppBytes, err := os.ReadFile(cppBin)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	verify := func(cand []byte) error {
		candPath := filepath.Join(dir, "cand.bin")
		if err := os.WriteFile(candPath, cand, 0o600); err != nil {
			return err
		}
		out, err := exec.Command(probeBin, "verify", candPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return cppBytes, verify
}

// mooFixedProofProgram builds the row's records in moo and prints: "@m" with
// the record body size, measure(0), measure(#values) and the saved length;
// "@x" and moo's file; then the load of inputs.cpp, "@ok" and the counters
// (or "@refused" and the reason), the dump, and "@y" and the re-save.
func mooFixedProofProgram(g *generatedUnit, c mooFixedValueCase) string {
	return "--!strict\nlocal inputs = require(\"./inputs\")\nlocal F = require(\"./table_fixed\")\nlocal serialize = require(\"./serialize\")\n" +
		g.requireAll() + "local T = require(\"./ProbeTable\")\n" + fixedDumper + `local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local values: { any } = {}
` + c.build + fmt.Sprintf(`local d = T.%[1]sDescriptor
local fixed = T.%[1]sFixed
local out, why = F.save(d, fixed, values)
if out == nil then
	print("@savenil " .. why.name .. " " .. tostring(why.field))
	return
end
print("@m " .. int(fixed.recordBytes) .. " " .. int(F.measure(d, fixed, 0)) .. " " .. int(F.measure(d, fixed, #values)) .. " " .. int(buffer.len(out)))
print("@x " .. hex(out))
local report: any = {}
local back = F.load(d, fixed, inputs.cpp, report)
if back == nil then
	print("@refused " .. tostring(report.reason) .. " " .. tostring(report.field))
	return
end
print(string.format("@ok %%d u%%d k%%d c%%d w%%d", #back, report.unknown, report.kindChanged, report.outOfRange, report.widened))
for i, v in back do
	dumpRecord(d, v, "r" .. int(i - 1))
end
for _, l in lines do
	print(l)
end
local re, rwhy = F.save(d, fixed, back)
if re == nil then
	print("@resavenil " .. rwhy.name .. " " .. tostring(rwhy.field))
	return
end
print("@y " .. hex(re))
`, c.root)
}

// mooFixedProof is one run of a row's moo program, parsed.
type mooFixedProof struct {
	measure string
	saved   []byte
	load    string
	dump    map[string]string
	resaved []byte
	raw     string
}

func runMooFixedProof(t *testing.T, g *generatedUnit, c mooFixedValueCase, cppBytes []byte) mooFixedProof {
	t.Helper()
	out := g.run(t, mooFixedProofProgram(g, c), map[string][]byte{"cpp": cppBytes})
	p := mooFixedProof{dump: map[string]string{}, raw: out}
	for l := range strings.SplitSeq(out, "\n") {
		var err error
		switch {
		case strings.HasPrefix(l, "@m "):
			p.measure = strings.TrimPrefix(l, "@m ")
		case strings.HasPrefix(l, "@x "):
			p.saved, err = hexDecode(strings.TrimPrefix(l, "@x "))
		case strings.HasPrefix(l, "@y "):
			p.resaved, err = hexDecode(strings.TrimPrefix(l, "@y "))
		case strings.HasPrefix(l, "@"):
			p.load = strings.TrimPrefix(l, "@")
		default:
			k, v, _ := strings.Cut(l, "=")
			p.dump[k] = v
		}
		if err != nil {
			t.Fatalf("%s: %v", c.id, err)
		}
	}
	return p
}

// wantMismatch is the first want line the dump does not land, or "".
func wantMismatch(want []string, dump map[string]string) string {
	for _, w := range want {
		k, v, _ := strings.Cut(w, "=")
		if got, ok := dump[k]; !ok || got != v {
			return fmt.Sprintf("%s landed %q, want %q", k, got, v)
		}
	}
	return ""
}

// mooFixedProofUnit generates the row's unit for moo and holds the body size
// the shared ir walk gives the root to the row's.
func mooFixedProofUnit(t *testing.T, c mooFixedValueCase) (*ir.Unit, *generatedUnit) {
	t.Helper()
	u := unitFromSource(t, c.schema)
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == c.root {
			if got := ir.TableFixedTypeBytes(st); got != c.body {
				t.Fatalf("%s: the record body is %d bytes, the row says %d", c.id, got, c.body)
			}
			return u, fixedUnit(t, u, nil)
		}
	}
	t.Fatalf("%s: %s is not a fixed root", c.id, c.root)
	return nil, nil
}

// TestMooFixedProofSuiteValues proves each value row of port/moo/fixed/*
// against the live C++ reference: moo's save of the row's records is the
// reference's file byte for byte; the measure is the header and layout plus
// one hash and body per record, and the body size is the row's; moo loads the
// reference's file clean (no refusal, no counter) and lands every value the
// row names; moo's re-save of what it loaded is the reference's file; and the
// reference loads moo's file clean and re-saves it to its own bytes. The
// fixed-file-roundtrip row is TestMooFixedFormAgreesWithTheByteOracle over
// the reference's corpus.
func TestMooFixedProofSuiteValues(t *testing.T) {
	slowtest.Gate(t, "moo and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	t.Parallel()
	for _, c := range mooFixedValueCases() {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			u, g := mooFixedProofUnit(t, c)
			cppBytes, verifyInCpp := cppFixedOracle(t, c.id, u, c.root, c.count, c.cppSetup)
			p := runMooFixedProof(t, g, c, cppBytes)
			if p.saved == nil {
				t.Fatalf("moo did not save:\n%s", p.raw)
			}
			layoutLen := int(uint32(cppBytes[16]) | uint32(cppBytes[17])<<8 | uint32(cppBytes[18])<<16 | uint32(cppBytes[19])<<24)
			empty := 20 + layoutLen
			if want := fmt.Sprintf("%d %d %d %d", c.body, empty, empty+c.count*int(8+c.body), len(cppBytes)); p.measure != want {
				t.Fatalf("the measure (body, measure(0), measure(n), saved): %s, want %s", p.measure, want)
			}
			if !bytes.Equal(p.saved, cppBytes) {
				t.Fatalf("moo's save is not the C++ reference's bytes: %s\nmoo=%x\ncpp=%x", firstDiff(p.saved, cppBytes), p.saved, cppBytes)
			}
			if want := fmt.Sprintf("ok %d u0 k0 c0 w0", c.count); p.load != want {
				t.Fatalf("moo's load of the reference's file: %s, want %s", p.load, want)
			}
			if m := wantMismatch(c.want, p.dump); m != "" {
				t.Fatalf("moo's load of the reference's file: %s", m)
			}
			if !bytes.Equal(p.resaved, cppBytes) {
				t.Fatalf("moo's re-save of the reference's file differs: %s", firstDiff(p.resaved, cppBytes))
			}
			if err := verifyInCpp(p.saved); err != nil {
				t.Fatalf("the C++ reference does not load moo's file back to its own bytes: %v", err)
			}
		})
	}
}

// TestMooFixedProofSuiteNegativeControl breaks each row two ways and requires
// the gates TestMooFixedProofSuiteValues holds green to go red. The FILE: the
// last record's first body byte of the reference's file flipped — the C++
// verifier refuses it, and moo's load either refuses it or lands a value the
// row's want lines reject. The WRITER: table_fixed.moo's save patched to flip
// the last byte of each record body — moo's file is no longer the reference's
// and the C++ verifier refuses it.
func TestMooFixedProofSuiteNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	t.Parallel()
	const anchor = "\t\t\twriteStruct(d, plan.root, 0, v, rec)\n"
	const planted = anchor + "\t\t\tbuffer.writeu8(rec, fixed.recordBytes - 1, bit32.bxor(buffer.readu8(rec, fixed.recordBytes - 1), 1))\n"
	for _, c := range mooFixedValueCases() {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			u, g := mooFixedProofUnit(t, c)
			cppBytes, verifyInCpp := cppFixedOracle(t, c.id, u, c.root, c.count, c.cppSetup)

			layoutLen := int(uint32(cppBytes[16]) | uint32(cppBytes[17])<<8 | uint32(cppBytes[18])<<16 | uint32(cppBytes[19])<<24)
			at := 20 + layoutLen + (c.count-1)*int(8+c.body) + 8
			mutated := bytes.Clone(cppBytes)
			mutated[at] ^= 0x01
			if err := verifyInCpp(mutated); err == nil {
				t.Fatal("the C++ verifier accepted a flipped body byte")
			}
			if p := runMooFixedProof(t, g, c, mutated); p.load == fmt.Sprintf("ok %d u0 k0 c0 w0", c.count) && wantMismatch(c.want, p.dump) == "" {
				t.Fatalf("moo loaded a flipped body byte and every want line still holds: the row's values prove nothing")
			}

			src := string(g.files["table_fixed.moo"])
			if strings.Count(src, anchor) != 1 {
				t.Fatal("the record write anchor is not in table_fixed.moo exactly once")
			}
			if err := os.WriteFile(filepath.Join(g.dir, "table_fixed.moo"), []byte(strings.Replace(src, anchor, planted, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			p := runMooFixedProof(t, g, c, cppBytes)
			if p.saved == nil {
				t.Fatalf("the planted writer did not save:\n%s", p.raw)
			}
			if bytes.Equal(p.saved, cppBytes) {
				t.Fatal("the planted writer still saves the reference's bytes: the byte gate proves nothing")
			}
			if err := verifyInCpp(p.saved); err == nil {
				t.Fatal("the C++ verifier accepted the planted writer's file")
			}
		})
	}
}
