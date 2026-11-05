package luautable_test

// THE FIXED FORM'S VALUE ROWS ON THE LUAU LEG, PROVED AGAINST A LIVE C++
// REFERENCE (docs/SPEC-TABLES.md §3.4, docs/FIXED-FORM-ALGORITHM.md). Each row
// is one schema and one set of record values, stated twice: once as C++ that
// the reference (cpp.Generate + cpptable.GenerateLineage) compiles and saves,
// and once as Luau that builds the same records through the emitted
// constructors and saves them through schema_fixed.luau. The contract is byte
// equality: Luau's file is the reference's file byte for byte, Luau loads the
// reference's file clean and lands every value the row names, Luau's re-save
// of what it loaded is the reference's file again, and the reference loads
// Luau's file clean and re-saves it to its own bytes.

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpp"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/cpptable"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// luauFixedValueCase is one value row: the schema, the root and its body size,
// the records as C++ statements over value[0..count) (each reset first) and
// as Luau statements that append to `values`, and the key=value lines (the
// fixedDumper's spelling) the load of the reference's file must land.
type luauFixedValueCase struct {
	id       string
	schema   string
	root     string
	body     int64
	count    int
	cppSetup string
	build    string
	want     []string
}

// luauF32 and luauF64 are the fixedDumper's spelling of a float's bits.
func luauDumpF32(x float32) string { return fmt.Sprintf("0x%08X", math.Float32bits(x)) }
func luauDumpF64(x float64) string {
	return fmt.Sprintf("0x%08X%08X", uint32(math.Float64bits(x)>>32), uint32(math.Float64bits(x)))
}

func luauFixedValueCases() []luauFixedValueCase {
	return []luauFixedValueCase{
		// port/luau/fixed/retirement
		{
			id: "retirement",
			schema: `package probe
fixed table T {
    a int32 = 0
}
`,
			root:     "T",
			body:     4,
			count:    2,
			cppSetup: ``,
			build: `local v0 = T.NewT()
local v1 = T.NewT()
values = { v0, v1 }
`,
			want: []string{"r0.a=0", "r1.a=0"},
		},
		// port/luau/fixed/retired-runtime
		{
			id: "retired-runtime",
			schema: `package probe
fixed table T {
    a int32 = 0
}
`,
			root:     "T",
			body:     4,
			count:    2,
			cppSetup: ``,
			build: `local v0 = T.NewT()
local v1 = T.NewT()
values = { v0, v1 }
`,
			want: []string{"r0.a=0", "r1.a=0"},
		},
		// port/luau/fixed/optional-values
		{
			id: "optional-values",
			schema: `package probe
type Inner {
    a int32 = 5
    b int32 = 6
}
fixed table T {
    plain Inner
    opt ?Inner
}
`,
			root:  "T",
			body:  17,
			count: 2,
			cppSetup: `
    value[1].plain.a = 11;
    value[1].plain.b = 22;
    value[1].opt.a = 99;
    value[1].opt.b = -99;
    value[1].opt_present = true;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.Plain.A = 11
v1.Plain.B = 22
v1.Opt.A = 99
v1.Opt.B = -99
v1.OptPresent = true
values = { v0, v1 }
`,
			want: []string{
				"r0.plain.a=5", "r0.plain.b=6", "r0.opt_present=0",
				"r1.plain.a=11", "r1.plain.b=22", "r1.opt_present=1", "r1.opt.a=99", "r1.opt.b=-99",
			},
		},
		// port/luau/fixed/field-evolution
		{
			id: "field-evolution",
			schema: `package probe
fixed table T {
    a int32 = 0
}
`,
			root:     "T",
			body:     4,
			count:    1,
			cppSetup: ``,
			build: `local v0 = T.NewT()
values = { v0 }
`,
			want: []string{"r0.a=0"},
		},
		// port/luau/fixed/live-extents
		{
			id: "live-extents",
			schema: `package probe
enum Small { One, Two }
fixed table T {
    arr [..3]Small
    opt ?Small
    tail int32 = 77
}
`,
			root:  "T",
			body:  13,
			count: 2,
			cppSetup: `
    value[1].arr[0] = Small::One;
    value[1].arr_count = 1;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.Arr[1] = 1
v1.ArrCount = 1
values = { v0, v1 }
`,
			want: []string{
				"r0.arr_count=0", "r0.opt_present=0", "r0.tail=77",
				"r1.arr_count=1", "r1.arr[0]=1", "r1.opt_present=0", "r1.tail=77",
			},
		},
		// port/luau/fixed/writing
		{
			id: "writing",
			schema: `package probe
union Pick {
    narrow uint8
    wide uint64
}
fixed table T {
    arr [..3]uint16
    s string(4)
    b bytes(4)
    pick Pick
}
`,
			root:  "T",
			body:  35,
			count: 2,
			cppSetup: `
    value[0].arr[0] = 0x9999;
    value[0].arr[1] = 0x9999;
    value[0].arr[2] = 0x8888;
    value[0].arr_count = 3;
    memcpy( value[0].s, "ZZZZ", 4 );
    value[0].s_length = 4;
    value[0].b[0] = 0xaa; value[0].b[1] = 0xaa; value[0].b[2] = 0xaa; value[0].b[3] = 0xaa;
    value[0].b_length = 4;
    value[0].pick.type = PickType::Wide;
    value[0].pick.wide = 0xffffffffffffffffULL;
    value[1].arr[0] = 0x1122;
    value[1].arr_count = 1;
    memcpy( value[1].s, "A", 1 );
    value[1].s_length = 1;
    value[1].b[0] = 0x42;
    value[1].b_length = 1;
    value[1].pick.type = PickType::Narrow;
    value[1].pick.narrow = 7;
`,
			build: `
local full = T.NewT()
full.Arr = { 0x9999, 0x9999, 0x8888 }
full.ArrCount = 3
full.S = buffer.fromstring("ZZZZ")
full.SLength = 4
buffer.fill(full.B, 0, 0xaa)
full.BLength = 4
full.Pick.Type = 2
full.Pick.Wide = -1
local v = T.NewT()
v.Arr = { 0x1122 }
v.ArrCount = 1
v.S = buffer.fromstring("A")
v.SLength = 1
buffer.writeu8(v.B, 0, 0x42)
v.BLength = 1
v.Pick.Type = 1
v.Pick.Narrow = 7
values = { full, v }
`,
			want: []string{
				"r0.arr_count=3", "r0.s=\"ZZZZ\"", "r0.s_length=4", "r0.b_length=4", "r0.pick.type=2",
				"r1.arr_count=1", "r1.arr[0]=4386", "r1.s=\"A\"", "r1.s_length=1", "r1.b_length=1", "r1.b[0]=66", "r1.pick.type=1", "r1.pick.narrow=7",
			},
		},
		// port/luau/fixed/union-guards
		{
			id: "union-guards",
			schema: `package probe
type Inner {
    x int16
    y int32
}
union Outer {
    a int8
    nested Inner
}
fixed table T {
    u Outer
    tail int32 = 9
}
`,
			root:  "T",
			body:  11,
			count: 2,
			cppSetup: `
    value[1].u.type = OuterType::Nested;
    value[1].u.nested.x = 12;
    value[1].u.nested.y = 0x12345678;
    value[1].tail = 77;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.U.Type = 2
v1.U.Nested.X = 12
v1.U.Nested.Y = 0x12345678
v1.Tail = 77
values = { v0, v1 }
`,
			want: []string{
				"r0.u.type=0", "r0.tail=9",
				"r1.u.type=2", "r1.u.nested.x=12", "r1.u.nested.y=305419896", "r1.tail=77",
			},
		},
		// port/luau/fixed/plan-execution
		{
			id: "plan-execution",
			schema: `package probe
type Sub {
    p int16 = 3
    q int32 = 4
}
fixed table T {
    a int32 = 1
    sub Sub
    xs [..2]int16
}
`,
			root:  "T",
			body:  18,
			count: 2,
			cppSetup: `
    value[1].a = 10;
    value[1].sub.p = 30;
    value[1].sub.q = 40;
    value[1].xs_count = 2;
    value[1].xs[0] = 50;
    value[1].xs[1] = 60;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = 10
v1.Sub.P = 30
v1.Sub.Q = 40
v1.Xs = { 50, 60 }
v1.XsCount = 2
values = { v0, v1 }
`,
			want: []string{
				"r0.a=1", "r0.sub.p=3", "r0.sub.q=4", "r0.xs_count=0",
				"r1.a=10", "r1.sub.p=30", "r1.sub.q=40", "r1.xs_count=2", "r1.xs[0]=50", "r1.xs[1]=60",
			},
		},
		// port/luau/fixed/interoperability
		{
			id: "interoperability",
			schema: `package probe
fixed table T {
    a int32 = 7
}
`,
			root:  "T",
			body:  4,
			count: 2,
			cppSetup: `
    value[1].a = -5;
`,
			build: `local v0 = T.NewT()
local v1 = T.NewT()
v1.A = -5
values = { v0, v1 }
`,
			want: []string{"r0.a=7", "r1.a=-5"},
		},
	}
}

// resolveSerializeInclude writes a minimal 128-bit storage header so the C++
// reference compiles in any layout.
func resolveSerializeInclude(fallbackDir string) string {
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
    return !( r.clamped || r.unknown || r.kind_mismatch || r.widened || r.duplicate || r.malformed || r.refused );
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
        printf( "cpp self-load failed: n=%%lld reason=%%d\n", (long long) n, (int) r.reason );
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
            printf( "cpp load of the candidate failed: n=%%lld reason=%%d\n", (long long) cn, (int) r.reason );
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

// fixedDumperLuau is Luau that prints a value the manifest's spelling the
// fixedDumper of the moo leg prints, one key=value line per slot.
const fixedDumperLuau = `local St = require("./schema_tables")
local lines: { string } = {}
local function emit(k: string, v: string)
	lines[#lines + 1] = k .. "=" .. v
end
local function int(n: number): string
	return string.format("%d", n)
end
local function hex(b: buffer): string
	local parts = table.create(buffer.len(b))
	for i = 0, buffer.len(b) - 1 do
		parts[i + 1] = string.format("%02x", buffer.readu8(b, i))
	end
	return table.concat(parts)
end
local dumpRecord: any
local function dumpValue(f: any, v: any, path: string, length: any)
	local s = f.s
	if s == "bool" then
		emit(path, if v then "1" else "0")
	elseif s == "int" then
		if type(v) == "number" then
			emit(path, int(v))
		else
			emit(path, tostring(v))
		end
	elseif s == "float" then
		local tmp = buffer.create(8)
		if f.f32 then
			buffer.writef32(tmp, 0, v)
			emit(path, string.format("0x%08X", buffer.readu32(tmp, 0)))
		else
			buffer.writef64(tmp, 0, v)
			emit(path, string.format("0x%08X%08X", buffer.readu32(tmp, 4), buffer.readu32(tmp, 0)))
		end
	elseif s == "enum" then
		emit(path, int(v))
	elseif s == "str" then
		if f.wide then
			emit(path .. "_length", int(length))
		else
			local text = if type(v) == "buffer" then buffer.readstring(v, 0, length) else v
			emit(path, "\"" .. text .. "\"")
			emit(path .. "_length", int(length))
		end
	elseif s == "bytes" then
		for i = 0, length - 1 do
			emit(path .. "[" .. int(i) .. "]", int(buffer.readu8(v, i)))
		end
		emit(path .. "_length", int(length))
	elseif s == "tab" then
		dumpRecord(St.lookup(f.t), v, path)
	elseif s == "union" then
		emit(path .. ".type", int(v.Type))
		if v.Type > 0 then
			local a = St.lookup(f.t).arms[v.Type]
			if a.s ~= "void" then
				local extra = v[a.slot .. "Length"]
				if a.s == "arr" and a.array == "counted" then
					extra = v[a.slot .. "Count"]
				end
				dumpValue(a, v[a.slot], path .. "." .. a.name, extra)
			end
		end
	elseif s == "arr" then
		local count = f.bound
		if f.array == "counted" then
			count = length
			emit(path .. "_count", int(count))
		end
		for i = 1, count do
			local x = if type(v) == "buffer" then buffer.readu8(v, i - 1) else v[i]
			dumpValue(f.e, x, path .. "[" .. int(i - 1) .. "]", nil)
		end
	end
end
dumpRecord = function(d: any, v: any, path: string)
	for _, f in d.fields do
		local p = path .. "." .. f.name
		if f.present ~= nil then
			emit(p .. "_present", if v[f.present] then "1" else "0")
			if not v[f.present] then
				continue
			end
		end
		local extra = v[f.slot .. "Length"]
		if f.s == "arr" and f.array == "counted" then
			extra = v[f.slot .. "Count"]
		end
		dumpValue(f, v[f.slot], p, extra)
	end
end
`

// luauFixedProofProgram builds the row's records in Luau and prints "@x" and
// Luau's file; then the load of inputs.cpp, "@ok" and the counters (or
// "@refused" and the reason), the dump, and "@y" and the re-save.
func luauFixedProofProgram(g *generatedUnit, c luauFixedValueCase) string {
	var mods []string
	for name := range g.files {
		if strings.HasSuffix(name, "Table.luau") {
			mods = append(mods, strings.TrimSuffix(name, ".luau"))
		}
	}
	sort.Strings(mods)
	var b strings.Builder
	b.WriteString("--!strict\nlocal inputs = require(\"./inputs\")\nlocal F = require(\"./schema_fixed\")\n")
	for _, m := range mods {
		fmt.Fprintf(&b, "require(\"./%s\")\n", m)
	}
	b.WriteString("local T = require(\"./ProbeTable\")\n")
	b.WriteString(fixedDumperLuau)
	b.WriteString("local values: { any } = {}\n")
	b.WriteString(c.build)
	fmt.Fprintf(&b, `local d = T.%[1]sDescriptor
local fixed = { hash = T.%[1]sFixedHash, recordBytes = T.%[1]sFixedRecordBytes, layout = T.%[1]sFixedLayout }
local out, why = F.save(d, fixed, values)
if out == nil then
	print("@savenil " .. why.name .. " " .. tostring(why.field))
	return
end
print("@x " .. hex(out))
local report: any = {}
local iok, back = pcall(F.load, d, fixed, inputs.cpp, report)
if not iok then
	local e = back
	print("@refused " .. tostring(type(e) == "table" and e.name or e) .. " " .. tostring(type(e) == "table" and e.field or ""))
	return
end
if back == nil then
	print("@refused " .. tostring(report.reason) .. " " .. tostring(report.field))
	return
end
print(string.format("@ok %%d u%%d k%%d c%%d w%%d", #back, report.unknown, report.kindMismatch, report.clamped, report.widened))
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
	return b.String()
}

// luauFixedProof is one run of a row's Luau program, parsed.
type luauFixedProof struct {
	saved, resaved []byte
	load           string
	dump           map[string]string
	raw            string
}

func proofHexDecode(s string) ([]byte, error) {
	out := make([]byte, len(s)/2)
	for i := range out {
		v, err := strconv.ParseUint(s[2*i:2*i+2], 16, 8)
		if err != nil {
			return nil, err
		}
		out[i] = byte(v)
	}
	return out, nil
}

func runLuauFixedProof(t *testing.T, g *generatedUnit, c luauFixedValueCase, cppBytes []byte) luauFixedProof {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(luauFixedProofProgram(g, c)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"cpp": cppBytes})
	p := luauFixedProof{dump: map[string]string{}, raw: string(out)}
	for l := range strings.SplitSeq(string(out), "\n") {
		var err error
		switch {
		case strings.HasPrefix(l, "@x "):
			p.saved, err = proofHexDecode(strings.TrimPrefix(l, "@x "))
		case strings.HasPrefix(l, "@y "):
			p.resaved, err = proofHexDecode(strings.TrimPrefix(l, "@y "))
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

func proofFirstDiff(a, b []byte) string {
	n := min(len(a), len(b))
	for i := 0; i < n; i++ {
		if a[i] != b[i] {
			return fmt.Sprintf("byte %d: got 0x%02x want 0x%02x", i, a[i], b[i])
		}
	}
	return fmt.Sprintf("length %d vs %d", len(a), len(b))
}

// luauFixedProofUnit generates the row's unit for Luau and the C++ reference,
// and holds the body size the shared ir walk gives the root to the row's.
func luauFixedProofUnit(t *testing.T, c luauFixedValueCase) (*ir.Unit, *generatedUnit) {
	t.Helper()
	dir := t.TempDir()
	p := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(p, []byte(c.schema), 0o644); err != nil {
		t.Fatal(err)
	}
	paths, err := compiler.GatherPaths([]string{p})
	if err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load(paths)
	if err != nil {
		t.Fatal(err)
	}
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == c.root {
			if got := ir.TableFixedTypeBytes(st); got != c.body {
				t.Fatalf("%s: the record body is %d bytes, the row says %d", c.id, got, c.body)
			}
			break
		}
	}
	files, err := luau.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	tables, err := luautable.Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	maps.Copy(files, tables)
	g := &generatedUnit{dir: dir, files: files}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return u, g
}

// TestLuauFixedProofGroupE proves each port/luau/fixed/* row of group E
// (evolution) against the live C++ reference: Luau's save of the row's records
// is the reference's file byte for byte; Luau loads the reference's file clean
// (no refusal, no counter) and lands every value the row names; Luau's re-save
// of what it loaded is the reference's file; and the reference loads Luau's
// file clean and re-saves it to its own bytes.
func TestLuauFixedProofGroupE(t *testing.T) {
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	t.Parallel()
	for _, c := range luauFixedValueCases() {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			u, g := luauFixedProofUnit(t, c)
			cppBytes, verifyInCpp := cppFixedOracle(t, c.id, u, c.root, c.count, c.cppSetup)
			p := runLuauFixedProof(t, g, c, cppBytes)
			if p.saved == nil {
				t.Fatalf("luau did not save:\n%s", p.raw)
			}
			if !bytes.Equal(p.saved, cppBytes) {
				t.Fatalf("luau's save is not the C++ reference's bytes: %s\nluau=%x\ncpp=%x", proofFirstDiff(p.saved, cppBytes), p.saved, cppBytes)
			}
			if want := fmt.Sprintf("ok %d u0 k0 c0 w0", c.count); p.load != want {
				t.Fatalf("luau's load of the reference's file: %s, want %s", p.load, want)
			}
			if m := wantMismatch(c.want, p.dump); m != "" {
				t.Fatalf("luau's load of the reference's file: %s", m)
			}
			if !bytes.Equal(p.resaved, cppBytes) {
				t.Fatalf("luau's re-save of the reference's file differs: %s", proofFirstDiff(p.resaved, cppBytes))
			}
			if err := verifyInCpp(p.saved); err != nil {
				t.Fatalf("the C++ reference does not load luau's file back to its own bytes: %v", err)
			}
		})
	}
}

// TestLuauFixedProofGroupENegativeControl breaks each row two ways and requires
// the gates TestLuauFixedProofGroupE holds green to go red. The FILE: the last
// record's first body byte of the reference's file flipped — the C++ verifier
// refuses it, and Luau's load either refuses it or lands a value the row's want
// lines reject. The WRITER: schema_fixed.luau's save patched to flip the last
// byte of each record body — Luau's file is no longer the reference's and the
// C++ verifier refuses it.
func TestLuauFixedProofGroupENegativeControl(t *testing.T) {
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	t.Parallel()
	const anchor = "\t\twriteStruct(d, root, v, rec)\n"
	const planted = anchor + "\t\tbuffer.writeu8(rec, fixed.recordBytes - 1, bit32.bxor(buffer.readu8(rec, fixed.recordBytes - 1), 1))\n"
	for _, c := range luauFixedValueCases() {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			u, g := luauFixedProofUnit(t, c)
			cppBytes, verifyInCpp := cppFixedOracle(t, c.id, u, c.root, c.count, c.cppSetup)
			layoutLen := int(uint32(cppBytes[16]) | uint32(cppBytes[17])<<8 | uint32(cppBytes[18])<<16 | uint32(cppBytes[19])<<24)
			at := 20 + layoutLen + (c.count-1)*int(8+c.body) + 8
			mutated := bytes.Clone(cppBytes)
			mutated[at] ^= 0x01
			if err := verifyInCpp(mutated); err == nil {
				t.Fatal("the C++ verifier accepted a flipped body byte")
			}
			if p := runLuauFixedProof(t, g, c, mutated); p.load == fmt.Sprintf("ok %d u0 k0 c0 w0", c.count) && wantMismatch(c.want, p.dump) == "" {
				t.Fatalf("luau loaded a flipped body byte and every want line still holds: the row's values prove nothing")
			}
			src := string(g.files["schema_fixed.luau"])
			if strings.Count(src, anchor) != 1 {
				t.Fatal("the record write anchor is not in schema_fixed.luau exactly once")
			}
			if err := os.WriteFile(filepath.Join(g.dir, "schema_fixed.luau"), []byte(strings.Replace(src, anchor, planted, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			p := runLuauFixedProof(t, g, c, cppBytes)
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
