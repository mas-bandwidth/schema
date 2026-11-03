// Copyright 2026 Glenn Lewis. All rights reserved.
//
// Use of this source code is governed by the Reticulum License
// that can be found in the LICENSE file.

package goosetable

import (
	"encoding/binary"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/goose/gooserun"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

type gooseFixedValueCase struct {
	id     string
	schema string
	root   string
	body   int64
	main   string
	want   string
}

// TestGooseFixedProofSuiteValues provides direct Goose runtime proof coverage
// for the 34 value, default, file-roundtrip, and record-measurement rows of
// port/goose/fixed/*. Each subtest verifies the IR layout walk, the emitted
// Goose fixed-form constants/functions, and a live Goose write-read round trip
// with boundary and non-default values.
func TestGooseFixedProofSuiteValues(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "goose")

	cases := []gooseFixedValueCase{
		// 16. port/goose/fixed/bool-values
		{
			id: "bool-values",
			schema: `package probe
fixed table T {
    a bool = false
    b bool = true
}
`,
			root: "T",
			body: 2,
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
			root: "T",
			body: 15,
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
			root: "T",
			body: 15,
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
			root: "T",
			body: 16,
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
			root: "T",
			body: 16,
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
			root: "T",
			body: 14,
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
			root: "T",
			body: 20,
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
			root: "T",
			body: 8,
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
			root: "T",
			body: 16,
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
			root: "T",
			body: 8,
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
			root: "T",
			body: 28,
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
			root: "T",
			body: 28,
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
			root: "T",
			body: 8,
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
			root: "T",
			body: 1,
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
			root: "T",
			body: 12,
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
			root: "T",
			body: 12,
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
			root: "T",
			body: 10,
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
			root: "T",
			body: 8,
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
			root: "T",
			body: 4,
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
			root: "T",
			body: 12,
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
			root: "T",
			body: 12,
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
			root: "T",
			body: 8,
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
			root: "T",
			body: 8,
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
			root: "T",
			body: 9,
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
			root: "T",
			body: 17,
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
			root: "T",
			body: 13,
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
			root: "T",
			body: 31,
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
			root: "T",
			body: 7,
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
			root: "T",
			body: 9,
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
			root: "T",
			body: 17,
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
			root: "T",
			body: 9,
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
			root: "T",
			body: 28,
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
			root: "T",
			body: 6,
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

	for _, tc := range cases {
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
			got := runCustomFixedGoose(t, u, nil, tc.main, nil)
			if got != tc.want {
				t.Fatalf("%s: got %q, want %q", tc.id, got, tc.want)
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
