package goosetable

import (
	"bytes"
	"encoding/binary"
	"encoding/hex"
	"fmt"
	"maps"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type gooseFixedValueCase struct {
	id       string
	schema   string
	root     string
	body     int64
	count    int
	cppSetup string
	main     string
	want     string
}

func fixedProofValueCases() []gooseFixedValueCase {
	return []gooseFixedValueCase{
		// 16. port/goose/fixed/bool-values
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { A: true, B: false };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w == probe::TFixedMeasure(2), " ", n, " ", schema_rt::fixed_refusal_name(rep.reason), " ", !back[0].A && back[0].B && back[1].A && !back[1].B);
}
`,
			want: "true 2 ok true",
		},
		// 17. port/goose/fixed/signed-integers
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { A: 127, B: 32767, C: 2147483647, D: 9223372036854775807 };
    var v2 = probe::T { A: -1, B: 0, C: 1, D: -2 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok0 = back[0].A == -128 && back[0].B == -32768 && back[0].C == -2147483648 && back[0].D == (-9223372036854775807 - 1);
    let ok1 = back[1].A == 127 && back[1].B == 32767 && back[1].C == 2147483647 && back[1].D == 9223372036854775807;
    let ok2 = back[2].A == -1 && back[2].B == 0 && back[2].C == 1 && back[2].D == -2;
    print(w > 0, " ", n, " ", ok0 && ok1 && ok2);
}
`,
			want: "true 3 true",
		},
		// 18. port/goose/fixed/unsigned-integers
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { A: 0, B: 0, C: 0, D: 0 };
    var v2 = probe::T { A: 1, B: 256, C: 65536, D: 4294967296 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok0 = back[0].A == 255 && back[0].B == 65535 && back[0].C == 4294967295 && back[0].D == 0xffffffffffffffff;
    let ok1 = back[1].A == 0 && back[1].B == 0 && back[1].C == 0 && back[1].D == 0;
    let ok2 = back[2].A == 1 && back[2].B == 256 && back[2].C == 65536 && back[2].D == 4294967296;
    print(w > 0, " ", n, " ", ok0 && ok1 && ok2);
}
`,
			want: "true 3 true",
		},
		// 19. port/goose/fixed/signed-128
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { A: schema_rt::Int128 { lo: 0xffffffffffffffff, hi: 0x7fffffffffffffff } };
    var v2 = probe::T { A: schema_rt::Int128 { lo: 1, hi: 0 } };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok0 = back[0].A.lo == 0 && back[0].A.hi == 0x8000000000000000;
    let ok1 = back[1].A.lo == 0xffffffffffffffff && back[1].A.hi == 0x7fffffffffffffff;
    let ok2 = back[2].A.lo == 1 && back[2].A.hi == 0;
    print(w > 0, " ", n, " ", ok0 && ok1 && ok2);
}
`,
			want: "true 3 true",
		},
		// 20. port/goose/fixed/unsigned-128
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { A: schema_rt::Int128 { lo: 0, hi: 0 } };
    var v2 = probe::T { A: schema_rt::Int128 { lo: 1, hi: 0 } };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok0 = back[0].A.lo == 0xffffffffffffffff && back[0].A.hi == 0xffffffffffffffff;
    let ok1 = back[1].A.lo == 0 && back[1].A.hi == 0;
    let ok2 = back[2].A.lo == 1 && back[2].A.hi == 0;
    print(w > 0, " ", n, " ", ok0 && ok1 && ok2);
}
`,
			want: "true 3 true",
		},
		// 21. port/goose/fixed/ranged-integers
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
			main: `fn main() {
    var v0 = probe::T { A: -5, B: -1000000000000, C: 0 };
    var v1 = probe::T { A: 5, B: 1000000000000, C: 1000 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].A == -5 && back[0].B == -1000000000000 && back[0].C == 0 && back[1].A == 5 && back[1].B == 1000000000000 && back[1].C == 1000;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 22. port/goose/fixed/bits-values
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
			main: `fn main() {
    var v0 = probe::T { A: 7, B: 4095, C: 1048575, D: 1099511627775 };
    var v1 = probe::T { A: 0, B: 0, C: 0, D: 0 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].A == 7 && back[0].B == 4095 && back[0].C == 1048575 && back[0].D == 1099511627775 && back[1].A == 0 && back[1].D == 0;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 23. port/goose/fixed/float32-values
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { A: -1024.25, B: 2048.75 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].A == 3.5 && back[0].B == -0.5 && back[1].A == -1024.25 && back[1].B == 2048.75;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 24. port/goose/fixed/float64-values
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { A: -987654321.125, B: 123456789.875 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].A == 2.5 && back[0].B == -1234.5 && back[1].A == -987654321.125 && back[1].B == 123456789.875;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 25. port/goose/fixed/compressed-floats
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
			main: `fn main() {
    var v0 = probe::T { A: 0.25, B: -0.5 };
    var v1 = probe::T { A: 0.0, B: -1.0 };
    var v2 = probe::T { A: 1.0, B: 1.0 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].A == 0.25 && back[0].B == -0.5 && back[1].A == 0.0 && back[1].B == -1.0 && back[2].A == 1.0 && back[2].B == 1.0;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 3 true",
		},
		// 26. port/goose/fixed/signed-fixed-point
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
			main: `fn main() {
    var v0 = probe::T { A: -8 << 16, B: -100 << 16, C: schema_rt::Int128 { lo: 0xffffffffff000000, hi: 0xffffffffffffffff } };
    var v1 = probe::T { A: 8 << 16, B: 100 << 16, C: schema_rt::Int128 { lo: 0x0000000000fa0000, hi: 0 } };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].A == (-8 << 16) && back[0].B == (-100 << 16) && back[1].A == (8 << 16) && back[1].B == (100 << 16) && back[1].C.lo == 0x0000000000fa0000;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 27. port/goose/fixed/unsigned-fixed-point
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
			main: `fn main() {
    var v0 = probe::T { A: 0, B: 0, C: schema_rt::Int128 { lo: 0, hi: 0 } };
    var v1 = probe::T { A: 16 << 16, B: 100 << 16, C: schema_rt::Int128 { lo: 1000 << 16, hi: 0 } };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].A == 0 && back[1].A == (16 << 16) && back[1].B == (100 << 16) && back[1].C.lo == (1000 << 16);
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 28. port/goose/fixed/flags-values
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
			main: `fn main() {
    var v0 = probe::T { Caps: 0 };
    var v1 = probe::T { Caps: 5 };
    var v2 = probe::T { Caps: 7 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].Caps == 0 && back[1].Caps == 5 && back[2].Caps == 7);
}
`,
			want: "true 3 true",
		},
		// 29. port/goose/fixed/enum-values
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
			main: `fn main() {
    var v0 = probe::T { G: 0 };
    var v1 = probe::T { G: 1 };
    var v2 = probe::T { G: 3 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].G == 0 && back[1].G == 1 && back[2].G == 3);
}
`,
			want: "true 3 true",
		},
		// 30. port/goose/fixed/utf8-values
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { S: [104, 101, 108, 108, 111, 0, 0, 0], SLength: 5 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].SLength == 0 && back[1].SLength == 5 && back[1].S[0] == 104 && back[1].S[4] == 111);
}
`,
			want: "true 2 true",
		},
		// 31. port/goose/fixed/utf16-values
		{
			id: "utf16-values",
			schema: `package probe
fixed table T { w wstring(4) }
`,
			root:  "T",
			body:  12,
			count: 2,
			cppSetup: `
    value[1].w[0] = 0x0041;
    value[1].w[1] = 0x03b1;
    value[1].w[2] = 0xd83d;
    value[1].w[3] = 0xde00;
    value[1].w_length = 4;
`,
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { W: [0x0041, 0x03b1, 0xd83d, 0xde00], WLength: 4 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].WLength == 0 && back[1].WLength == 4 && back[1].W[1] == 0x03b1 && back[1].W[3] == 0xde00);
}
`,
			want: "true 2 true",
		},
		// 32. port/goose/fixed/byte-buffers
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { Blob: [0xff, 0x00, 0x5a, 1, 2, 3], BlobLength: 6 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].BlobLength == 0 && back[1].BlobLength == 6 && back[1].Blob[0] == 0xff && back[1].Blob[2] == 0x5a);
}
`,
			want: "true 2 true",
		},
		// 33. port/goose/fixed/nested-types
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
			main: `fn main() {
    var v = probe::T { Badge: probe::Badge { Title: [104, 105, 0, 0], TitleLength: 2 } };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v], out);
    var back: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].Badge.TitleLength == 2 && back[0].Badge.Title[0] == 104 && back[0].Badge.Title[1] == 105);
}
`,
			want: "true 1 true",
		},
		// 34. port/goose/fixed/nested-fixed-tables
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
			main: `fn main() {
    var v0 = probe::T { Child: probe::Child { N: -2147483648 } };
    var v1 = probe::T { Child: probe::Child { N: 2147483647 } };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].Child.N == -2147483648 && back[1].Child.N == 2147483647);
}
`,
			want: "true 2 true",
		},
		// 35. port/goose/fixed/fixed-arrays
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
			main: `fn main() {
    var v = probe::T { Tag: [-2147483648, 0, 2147483647] };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v], out);
    var back: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].Tag[0] == -2147483648 && back[0].Tag[1] == 0 && back[0].Tag[2] == 2147483647);
}
`,
			want: "true 1 true",
		},
		// 36. port/goose/fixed/counted-arrays
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
			main: `fn main() {
    var v0 = probe::T { Nums: [0, 0, 0, 0], NumsCount: 0 };
    var v1 = probe::T { Nums: [10, 65535, 32768, 0], NumsCount: 3 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].NumsCount == 0 && back[1].NumsCount == 3 && back[1].Nums[1] == 65535 && back[1].Nums[2] == 32768);
}
`,
			want: "true 2 true",
		},
		// 37. port/goose/fixed/keyed-arrays
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
			main: `fn main() {
    var v = probe::T { Slots: [-2147483648, 2147483647] };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v], out);
    var back: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].Slots[0] == -2147483648 && back[0].Slots[1] == 2147483647);
}
`,
			want: "true 1 true",
		},
		// 38. port/goose/fixed/nested-keyed-arrays
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
			main: `fn main() {
    var v = probe::T { Slots: [probe::Child { N: 11 }, probe::Child { N: -22 }] };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v], out);
    var back: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    print(w > 0, " ", n, " ", back[0].Slots[0].N == 11 && back[0].Slots[1].N == -22);
}
`,
			want: "true 1 true",
		},
		// 39. port/goose/fixed/union-values
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
			main: `fn main() {
    var v0 = probe::T { .. };
    v0.Body.Type = 1;
    v0.Body.V = probe::Vec { X: 11, Y: -22 };
    var v1 = probe::T { .. };
    v1.Body.Type = 2;
    v1.Body.T = probe::Tab { N: 41 };
    var v2 = probe::T { .. };
    v2.Body.Type = 3;
    v2.Body.N = 1073741821;
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].Body.Type == 1 && back[0].Body.V.X == 11 && back[0].Body.V.Y == -22 &&
        back[1].Body.Type == 2 && back[1].Body.T.N == 41 &&
        back[2].Body.Type == 3 && back[2].Body.N == 1073741821;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 3 true",
		},
		// 40. port/goose/fixed/union-field-arms
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
			main: `fn main() {
    var v0 = probe::T { .. };
    v0.Body.Type = 1;
    v0.Body.Sa = -5;
    var v1 = probe::T { .. };
    v1.Body.Type = 2;
    v1.Body.Ta = [97, 98, 99, 0];
    v1.Body.TaLength = 3;
    var v2 = probe::T { .. };
    v2.Body.Type = 3;
    v2.Body.Aa = [-1, 0, 1];
    v2.Body.AaCount = 3;
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].Body.Type == 1 && back[0].Body.Sa == -5 &&
        back[1].Body.Type == 2 && back[1].Body.TaLength == 3 && back[1].Body.Ta[2] == 99 &&
        back[2].Body.Type == 3 && back[2].Body.AaCount == 3 && back[2].Body.Aa[2] == 1;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 3 true",
		},
		// 41. port/goose/fixed/payload-free-arms
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
			main: `fn main() {
    var v0 = probe::T { Lead: 10, Pick: probe::Pick { Type: 1, Alpha: 99 }, Trail: 20 };
    var v1 = probe::T { Lead: 30, Pick: probe::Pick { Type: 2, Alpha: 777 }, Trail: 40 };
    var v2 = probe::T { Lead: 50, Pick: probe::Pick { Type: 3, Alpha: 888 }, Trail: 60 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1, v2], out);
    // Under payload-free arms (v1 and v2), the 4 arm payload bytes must be zero on wire.
    let rec1 = out.len - 2 * probe::TFixedRecordBytes;
    let arm1Zero = out[rec1 + 13] == 0 && out[rec1 + 14] == 0 && out[rec1 + 15] == 0 && out[rec1 + 16] == 0;
    var back: probe::T[3] = [probe::T { .. }; 3];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].Pick.Type == 1 && back[0].Pick.Alpha == 99 &&
        back[1].Pick.Type == 2 && back[1].Lead == 30 && back[1].Trail == 40 &&
        back[2].Pick.Type == 3 && back[2].Lead == 50 && back[2].Trail == 60;
    print(w > 0, " ", n, " ", arm1Zero && ok);
}
`,
			want: "true 3 true",
		},
		// 42. port/goose/fixed/union-arrays
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
			main: `fn main() {
    var v = probe::T { .. };
    v.PicksCount = 3;
    v.Picks[0].Type = 1;
    v.Picks[0].A = 3;
    v.Picks[1].Type = 2;
    v.Picks[1].B = -1099511627776;
    v.Picks[2].Type = 1;
    v.Picks[2].A = 0;
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v], out);
    var back: probe::T[1] = [probe::T { .. }; 1];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = back[0].PicksCount == 3 && back[0].Picks[0].A == 3 && back[0].Picks[1].B == -1099511627776 && back[0].Picks[2].Type == 1;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 1 true",
		},
		// 43. port/goose/fixed/optional-scalars
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { N: -123456, NPresent: true, G: 2, GPresent: true };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = !back[0].NPresent && !back[0].GPresent && back[1].NPresent && back[1].N == -123456 && back[1].GPresent && back[1].G == 2;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 44. port/goose/fixed/optional-nesting
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { P: probe::Vec { X: 99, Y: -99 }, PPresent: true };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = !back[0].PPresent && back[1].PPresent && back[1].P.X == 99 && back[1].P.Y == -99;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 45. port/goose/fixed/optional-arrays
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { Xs: [7, -8, 9], XsCount: 3, XsPresent: true };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok = !back[0].XsPresent && back[1].XsPresent && back[1].XsCount == 3 && back[1].Xs[2] == 9;
    print(w > 0, " ", n, " ", ok);
}
`,
			want: "true 2 true",
		},
		// 46. port/goose/fixed/scalar-defaults
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T { X: -9, G: 3, F: -2.25 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok0 = back[0].X == 7 && back[0].G == 2 && back[0].F == 1.5;
    let ok1 = back[1].X == -9 && back[1].G == 3 && back[1].F == -2.25;
    print(w > 0, " ", n, " ", ok0 && ok1);
}
`,
			want: "true 2 true",
		},
		// 47. port/goose/fixed/text-bytes-flags-defaults
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
			main: `fn main() {
    var v0 = probe::T { .. };
    var v1 = probe::T {
        S: [120, 121, 122, 0, 0, 0, 0, 0],
        SLength: 3,
        B: [0x00, 0xff, 0x5a, 0x01],
        BLength: 4,
        C: 4,
    };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    var back: probe::T[2] = [probe::T { .. }; 2];
    var rep = schema_rt::FixedReport { .. };
    let n = probe::TFixedLoad(back, out, rep);
    let ok0 = back[0].SLength == 2 && back[0].S[0] == 104 && back[0].S[1] == 105 &&
        back[0].BLength == 2 && back[0].B[0] == 97 && back[0].B[1] == 98 &&
        back[0].C == 3;
    let ok1 = back[1].SLength == 3 && back[1].S[0] == 120 && back[1].BLength == 4 && back[1].B[1] == 0xff && back[1].C == 4;
    print(w > 0, " ", n, " ", ok0 && ok1);
}
`,
			want: "true 2 true",
		},
		// 49. port/goose/fixed/record-measurement
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
			main: `fn main() {
    let m0 = probe::TFixedMeasure(0);
    let m2 = probe::TFixedMeasure(2);
    var v0 = probe::T { A: 10, B: 20 };
    var v1 = probe::T { A: 30, B: 40 };
    var out: u8[>..] = [];
    let w = probe::TFixedSave([v0, v1], out);
    let hdr = schema_rt::FixedHeaderBytes + 4 + probe::TFixedLayoutLength;
    print(probe::TFixedBodyBytes == 6, " ", probe::TFixedRecordBytes == 14, " ", m0 == hdr, " ", m2 == hdr + 28 && w == m2);
}
`,
			want: "true true true true",
		},
	}
}

// resolveSerializeInclude finds the sibling serialize checkout directory if
// present, or writes a minimal 128-bit storage header into fallbackDir so C++
// reference probes for 128-bit and fixed-point schemas compile in any layout.
func resolveSerializeInclude(fallbackDir string) string {
	candidates := []string{
		os.Getenv("SERIALIZE"),
		"../../../../serialize",
		"../../../serialize",
		"../../serialize",
		"../serialize",
	}
	for _, c := range candidates {
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

// buildAndRunCppFixedOracle compiles a C++ reference program from the same
// schema unit u using cpp.Generate(u) and cpptable.GenerateLineage(u, nil),
// runs it in "save" mode to produce the reference Form 3 bytes (cppBytes), and
// returns cppBytes along with a function that runs the compiled C++ oracle in
// "verify" mode against any candidate Form 3 byte slice (returning nil iff C++
// FixedLoad succeeds with zero counters/refusals and re-saves to cppBytes).
func buildAndRunCppFixedOracle(t *testing.T, id string, u *ir.Unit, root string, count int, cppSetup string) ([]byte, func([]byte) error) {
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

	outDir := t.TempDir()
	var tableHeader string
	for name, data := range files {
		if strings.Contains(name, "/") {
			continue
		}
		if err := os.WriteFile(filepath.Join(outDir, name), data, 0o600); err != nil {
			t.Fatalf("%s: write %s: %v", id, name, err)
		}
		if strings.HasSuffix(name, "Table.h") {
			tableHeader = name
		}
	}
	if tableHeader == "" {
		t.Fatalf("%s: no Table.h generated", id)
	}

	serDir := resolveSerializeInclude(outDir)
	probeSrc := fmt.Sprintf(`#include <cstdio>
#include <cstdlib>
#include <cstring>
#include "%[4]s"

using namespace probe;

static void fill_records( %[1]s * value )
{
    for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( value[i] );
%[3]s
}

int main( int argc, char ** argv )
{
    static uint8_t buffer[1 << 18];
    static uint8_t cand_buf[1 << 18];
    static uint8_t re_buf[1 << 18];
    static TableFixedEntry plan[4096];
    TableReport r;

    %[1]s value[%[2]d];
    fill_records( value );

    const int64_t need = %[1]sFixedMeasure( %[2]d );
    const int64_t saved = %[1]sFixedSave( value, %[2]d, buffer, need );
    if ( saved != need ) {
        printf( "cpp save: need=%%lld saved=%%lld\n", (long long) need, (long long) saved );
        return 1;
    }

    %[1]s back[%[2]d];
    for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( back[i] );
    memset( &r, 0, sizeof( r ) );
    const int64_t n = %[1]sFixedLoad( back, %[2]d, buffer, saved, plan, 4096, NULL, &r );
    if ( n != %[2]d || r.clamped || r.unknown || r.kind_mismatch || r.widened || r.duplicate || r.malformed || r.refused ) {
        printf( "cpp self-load failed: n=%%lld reason=%%d c=%%d u=%%d km=%%d w=%%d m=%%d x=%%d\n",
            (long long) n, (int) r.reason, r.clamped, r.unknown, r.kind_mismatch, r.widened, (int) r.malformed, (int) r.refused );
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
        int64_t clen = (int64_t) fread( cand_buf, 1, sizeof( cand_buf ), f );
        fclose( f );
        %[1]s from_cand[%[2]d];
        for ( int i = 0; i < %[2]d; ++i ) %[1]sReset( from_cand[i] );
        memset( &r, 0, sizeof( r ) );
        const int64_t cn = %[1]sFixedLoad( from_cand, %[2]d, cand_buf, clen, plan, 4096, NULL, &r );
        if ( cn != %[2]d || r.clamped || r.unknown || r.kind_mismatch || r.widened || r.duplicate || r.malformed || r.refused ) {
            printf( "cpp load of candidate failed: cn=%%lld reason=%%d c=%%d u=%%d km=%%d w=%%d m=%%d x=%%d\n",
                (long long) cn, (int) r.reason, r.clamped, r.unknown, r.kind_mismatch, r.widened, (int) r.malformed, (int) r.refused );
            return 4;
        }
        const int64_t re_saved = %[1]sFixedSave( from_cand, %[2]d, re_buf, need );
        if ( re_saved != saved || memcmp( re_buf, buffer, (size_t) saved ) != 0 ) {
            printf( "cpp re-save after loading candidate differed from reference\n" );
            return 5;
        }
        if ( clen != saved || memcmp( cand_buf, buffer, (size_t) saved ) != 0 ) {
            printf( "candidate bytes differed from cpp reference bytes\n" );
            return 6;
        }
        return 0;
    }
    return 1;
}
`, root, count, cppSetup, tableHeader)

	probeCpp := filepath.Join(outDir, "probe.cpp")
	probeBin := filepath.Join(outDir, "probe")
	if err := os.WriteFile(probeCpp, []byte(probeSrc), 0o600); err != nil {
		t.Fatalf("%s: write probe.cpp: %v", id, err)
	}
	cxx := os.Getenv("CXX")
	if cxx == "" {
		cxx = "c++"
	}
	args := []string{"-std=c++17", "-O1", "-I" + outDir, "-I" + serDir, probeCpp, "-o", probeBin}
	if out, err := gooserun.Command(t, cxx, args...).CombinedOutput(); err != nil {
		t.Fatalf("%s: compile C++ reference oracle failed: %v\n%s", id, err, out)
	}

	cppBinPath := filepath.Join(outDir, "cpp.bin")
	if out, err := gooserun.Command(t, probeBin, "save", cppBinPath).CombinedOutput(); err != nil {
		t.Fatalf("%s: run C++ reference oracle save failed: %v\n%s", id, err, out)
	}
	cppBytes, err := os.ReadFile(cppBinPath)
	if err != nil {
		t.Fatalf("%s: read cpp.bin: %v", id, err)
	}

	verifyFn := func(cand []byte) error {
		candPath := filepath.Join(outDir, "cand.bin")
		if err := os.WriteFile(candPath, cand, 0o600); err != nil {
			return err
		}
		out, err := gooserun.Command(t, probeBin, "verify", candPath).CombinedOutput()
		if err != nil {
			return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
		}
		return nil
	}
	return cppBytes, verifyFn
}

// withGooseCrossCheck appends a cross-check tail to a Goose main() body that:
//  1. Prints the hex of `out` (the Form 3 bytes saved by Goose FixedSave).
//  2. Loads `cpp.bin` (the Form 3 bytes saved by the C++ reference FixedSave)
//     via Goose FixedLoad, verifies a clean load of `count` records, re-saves
//     via Goose FixedSave, and prints the hex of the re-saved bytes.
func withGooseCrossCheck(mainBody, root string, count int) string {
	idx := strings.LastIndex(mainBody, "}")
	if idx < 0 {
		panic("mainBody missing closing brace")
	}
	tail := fmt.Sprintf(`    hex(out);
    var cppIn: u8[>..] = [];
    if !read_file("cpp.bin", cppIn) { print("cpp-read-failed"); return; }
    var cppBack: probe::%[1]s[%[2]d] = [probe::%[1]s { .. }; %[2]d];
    var cppRep = schema_rt::FixedReport { .. };
    let cppN = probe::%[1]sFixedLoad(cppBack, cppIn, cppRep);
    var cppReOut: u8[>..] = [];
    let cppW = probe::%[1]sFixedSave(cppBack, cppReOut);
    print(cppN == %[2]d && cppRep.reason == 0 && cppRep.clamped == 0 && cppW == out.len);
    hex(cppReOut);
}
`, root, count)
	return mainBody[:idx] + tail
}

// TestGooseFixedProofSuiteValues provides direct Goose runtime proof coverage
// for the 34 value, default, file-roundtrip, and record-measurement rows of
// port/goose/fixed/*. Each of the 33 value/default/measurement subtests:
//  1. Verifies the IR layout walk, body byte size, and emitted Goose symbols.
//  2. Compiles and runs the C++ reference implementation (cpp.Generate +
//     cpptable.GenerateLineage) on the identical schema and record values to
//     obtain reference Form 3 bytes (cppBytes).
//  3. Runs the Goose implementation to obtain Goose Form 3 bytes (gooseBytes),
//     verifies byte-for-byte equality with cppBytes, verifies that Goose
//     FixedLoad loads cppBytes cleanly and re-saves to cppBytes, and verifies
//     that C++ FixedLoad loads gooseBytes cleanly and re-saves to cppBytes.
func TestGooseFixedProofSuiteValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")

	for _, tc := range fixedProofValueCases() {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			u := parseCheckUnit(t, tc.schema)
			st := fixedRootStruct(t, u, tc.root)
			layout, hash, body := fixedRootFacts(st)
			if body != tc.body {
				t.Fatalf("%s: body=%d, want %d", tc.id, body, tc.body)
			}
			if len(layout) == 0 || hash == 0 {
				t.Fatalf("%s: empty layout or zero hash", tc.id)
			}
			files, err := goose.Generate(u)
			if err != nil {
				t.Fatalf("%s: goose.Generate: %v", tc.id, err)
			}
			tables, err := Generate(u)
			if err != nil {
				t.Fatalf("%s: goosetable.Generate: %v", tc.id, err)
			}
			tableSrc := string(tables["ProbeTable.goose"])
			for _, sym := range []string{
				tc.root + "FixedBodyBytes",
				tc.root + "FixedRecordBytes",
				tc.root + "FixedHash",
				tc.root + "FixedLayout",
				tc.root + "FixedMeasure",
				tc.root + "FixedSave",
				tc.root + "FixedLoad",
				tc.root + "FixedLoadTolerant",
			} {
				if !strings.Contains(tableSrc, sym) {
					t.Fatalf("%s: ProbeTable.goose missing %s", tc.id, sym)
				}
			}
			_ = files

			cppBytes, verifyInCpp := buildAndRunCppFixedOracle(t, tc.id, u, tc.root, tc.count, tc.cppSetup)
			wrappedMain := withGooseCrossCheck(tc.main, tc.root, tc.count)
			rawOut := runCustomFixedGoose(t, u, nil, wrappedMain, map[string][]byte{"cpp.bin": cppBytes})
			lines := strings.Split(strings.TrimSpace(rawOut), "\n")
			if len(lines) != 4 {
				t.Fatalf("%s: expected 4 output lines, got %d:\n%s", tc.id, len(lines), rawOut)
			}
			if got := strings.TrimSpace(lines[0]); got != tc.want {
				t.Fatalf("%s: got %q, want %q", tc.id, got, tc.want)
			}
			gooseBytes, err := hex.DecodeString(strings.TrimSpace(lines[1]))
			if err != nil {
				t.Fatalf("%s: decode goose hex: %v", tc.id, err)
			}
			if !bytes.Equal(gooseBytes, cppBytes) {
				t.Fatalf("%s: Goose FixedSave bytes != C++ reference FixedSave bytes:\ngoose=%x\ncpp  =%x", tc.id, gooseBytes, cppBytes)
			}
			if crossOk := strings.TrimSpace(lines[2]); crossOk != "true" {
				t.Fatalf("%s: Goose FixedLoad of C++ reference bytes failed: %s", tc.id, crossOk)
			}
			reSavedBytes, err := hex.DecodeString(strings.TrimSpace(lines[3]))
			if err != nil {
				t.Fatalf("%s: decode goose re-saved hex: %v", tc.id, err)
			}
			if !bytes.Equal(reSavedBytes, cppBytes) {
				t.Fatalf("%s: Goose re-save after loading C++ bytes differed:\ngot =%x\nwant=%x", tc.id, reSavedBytes, cppBytes)
			}
			if err := verifyInCpp(gooseBytes); err != nil {
				t.Fatalf("%s: C++ FixedLoad verification of Goose bytes failed: %v", tc.id, err)
			}
		})
	}

	// 48. port/goose/fixed/fixed-file-roundtrip
	t.Run("fixed-file-roundtrip", func(t *testing.T) {
		t.Parallel()
		corpus := fixedCorpus(t)
		records := fixedManifestRecords(t, corpus)
		for _, v := range fixedVectors {
			want := records[v.file]
			data, err := os.ReadFile(filepath.Join(corpus, v.file))
			if err != nil {
				t.Fatalf("read %s: %v", v.file, err)
			}
			u := fixedUnit(t, v.schema)
			st := fixedRootStruct(t, u, v.root)
			layout, hash, body := fixedRootFacts(st)
			if got := binary.LittleEndian.Uint64(data[8:16]); got != hash {
				t.Fatalf("%s: header hash 0x%016x != 0x%016x", v.file, got, hash)
			}
			if len(data)-20-len(layout) != want*int(8+body) {
				t.Fatalf("%s: record byte size mismatch", v.file)
			}
			dir := t.TempDir()
			main := fixedProgram(t, u, v.root, dir, []string{"corpus.bin"}, want)
			out := gooserun.Run(t, dir, main, map[string][]byte{"corpus.bin": data})
			a := fixedAnswers(t, out, 1)[0]
			if a.n != want || a.reason != "ok" || a.clamped != 0 || a.written != len(data) || a.bytes != hex.EncodeToString(data) {
				t.Fatalf("%s: roundtrip failed: n=%d reason=%s clamped=%d written=%d", v.file, a.n, a.reason, a.clamped, a.written)
			}
		}
	})
}

// TestGooseFixedProofSuiteNegativeControl proves that the C++ reference byte
// oracle and bidirectional load/save checks in TestGooseFixedProofSuiteValues
// reject both trailer-corrupted wire images and re-signed payload-mutated wire
// images across all 33 value rows.
func TestGooseFixedProofSuiteNegativeControl(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")

	for _, tc := range fixedProofValueCases() {
		t.Run(tc.id, func(t *testing.T) {
			t.Parallel()
			u := parseCheckUnit(t, tc.schema)
			st := fixedRootStruct(t, u, tc.root)
			layout, _, body := fixedRootFacts(st)
			cppBytes, verifyInCpp := buildAndRunCppFixedOracle(t, tc.id, u, tc.root, tc.count, tc.cppSetup)

			// 1. Corrupting a byte without updating the CRC-32C trailer must fail
			// C++ FixedLoad verification.
			corruptTrailer := bytes.Clone(cppBytes)
			corruptTrailer[0] ^= 0x01
			if err := verifyInCpp(corruptTrailer); err == nil {
				t.Fatalf("%s: negative control failed: C++ FixedLoad accepted corrupt trailer", tc.id)
			}

			// 2. Flipping a live record body byte and re-signing the CRC-32C trailer
			// must be caught by the C++ reference oracle comparison.
			mutatedPayload := bytes.Clone(cppBytes)
			rec0BodyStart := 16 + len(layout) + 8
			if rec0BodyStart >= len(mutatedPayload)-4 || body <= 0 {
				t.Fatalf("%s: unexpected record body offset %d", tc.id, rec0BodyStart)
			}
			// Mutate the last record's first body byte (which is non-default in all
			// multi-record cases) and re-sign the CRC-32C trailer.
			lastRecBodyStart := 16 + len(layout) + (tc.count-1)*int(8+body) + 8
			mutatedPayload[lastRecBodyStart] ^= 0x01
			if bytes.Equal(mutatedPayload, cppBytes) {
				t.Fatalf("%s: negative control mutation produced identical bytes", tc.id)
			}
			if err := verifyInCpp(mutatedPayload); err == nil {
				t.Fatalf("%s: negative control failed: C++ oracle accepted mutated payload byte", tc.id)
			}
		})
	}

	// 3. Mutating the generated Goose FixedSave emitter in ProbeTable.goose
	// (while still producing a valid Form 3 envelope and CRC-32C trailer) must
	// be caught by both the byte-oracle equality check and C++ FixedLoad verify.
	t.Run("mutated-goose-writer", func(t *testing.T) {
		t.Parallel()
		tc := fixedProofValueCases()[2] // unsigned-integers
		u := parseCheckUnit(t, tc.schema)
		cppBytes, verifyInCpp := buildAndRunCppFixedOracle(t, tc.id, u, tc.root, tc.count, tc.cppSetup)

		files, err := goose.Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		tables, err := Generate(u)
		if err != nil {
			t.Fatal(err)
		}
		maps.Copy(files, tables)
		// Mutate ProbeTable.goose so TFixedSave flips one byte before computing CRC-32C.
		tableCode := string(files["ProbeTable.goose"])
		orig := "_TFixedWrite(out, values[k]);"
		if !strings.Contains(tableCode, orig) {
			t.Fatalf("ProbeTable.goose missing expected _TFixedWrite call")
		}
		mutatedCode := strings.Replace(tableCode, orig, orig+"\n        out[out.len - 1] = out[out.len - 1] ^ 1;", 1)
		files["ProbeTable.goose"] = []byte(mutatedCode)

		dir := t.TempDir()
		for name, data := range files {
			if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
				t.Fatal(err)
			}
		}
		mainSrc := "import os;\nimport .Probe;\nimport .ProbeTable;\nimport .schema_rt;\n\n" + gooserun.HexProbe + "\n" + withGooseCrossCheck(tc.main, tc.root, tc.count)
		if err := os.WriteFile(filepath.Join(dir, "main.goose"), []byte(mainSrc), 0o644); err != nil {
			t.Fatal(err)
		}
		rawOut := strings.TrimSpace(string(gooserun.Run(t, dir, "main.goose", map[string][]byte{"cpp.bin": cppBytes})))
		lines := strings.Split(rawOut, "\n")
		if len(lines) < 2 {
			t.Fatalf("unexpected output: %s", rawOut)
		}
		mutatedGooseBytes, err := hex.DecodeString(strings.TrimSpace(lines[1]))
		if err != nil {
			t.Fatal(err)
		}
		if bytes.Equal(mutatedGooseBytes, cppBytes) {
			t.Fatalf("negative control failed: mutated Goose writer produced identical bytes to C++ oracle")
		}
		if err := verifyInCpp(mutatedGooseBytes); err == nil {
			t.Fatalf("negative control failed: C++ oracle accepted bytes from mutated Goose writer")
		}
	})
}
