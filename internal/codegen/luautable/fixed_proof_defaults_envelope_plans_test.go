package luautable_test

// THE FIXED FORM'S GROUP D (DEFAULTS, ENVELOPE AND PLANS) ROWS ON THE LUAU LEG,
// PROVED AGAINST A LIVE C++ REFERENCE (docs/SPEC-TABLES.md §3.4,
// docs/FIXED-FORM-ALGORITHM.md). Each row is one schema and one set of record
// values, stated twice: once as C++ that the reference (cpp.Generate +
// cpptable.GenerateLineage) compiles and saves, and once as Luau that builds
// the same records through the emitted constructors and saves them through
// schema_fixed.luau. The contract is byte equality: Luau's file is the
// reference's file byte for byte, Luau loads the reference's file clean and
// lands every value the row names, Luau's re-save of what it loaded is the
// reference's file again, and the reference loads Luau's file clean and
// re-saves it to its own bytes.

import (
	"bytes"
	"fmt"
	"maps"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/compiler"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luau/luaurun"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/luautable"
	"github.com/mas-bandwidth/schema/v2/internal/fixedproof"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

type luauGroupDCase struct {
	id       string
	schema   string
	root     string
	body     int64
	count    int
	cppSetup string
	build    string
	want     []string
}

func luauGroupDDumpF32(x float32) string { return fmt.Sprintf("0x%08X", math.Float32bits(x)) }

func luauGroupDCases() []luauGroupDCase {
	return []luauGroupDCase{
		// port/luau/fixed/scalar-defaults
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
v1.F = -2.25
values = { v0, v1 }
`,
			want: []string{
				"r0.x=7", "r0.g=2", "r0.f=" + luauGroupDDumpF32(1.5),
				"r1.x=-9", "r1.g=3", "r1.f=" + luauGroupDDumpF32(-2.25),
			},
		},
		// port/luau/fixed/text-bytes-flags-defaults
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
buffer.fill(v1.S, 0, buffer.len(v1.S))
buffer.writestring(v1.S, 0, "xyz")
v1.SLength = 3
buffer.fill(v1.B, 0, buffer.len(v1.B))
buffer.writestring(v1.B, 0, "\x00\xff\x5a\x01")
v1.BLength = 4
v1.C = 4i
values = { v0, v1 }
`,
			want: []string{
				`r0.s="hi"`, "r0.s_length=2", "r0.b_length=2", "r0.b[0]=97", "r0.b[1]=98", "r0.c=3",
				`r1.s="xyz"`, "r1.s_length=3", "r1.b_length=4", "r1.b[0]=0", "r1.b[1]=255", "r1.b[2]=90", "r1.b[3]=1", "r1.c=4",
			},
		},
		// port/luau/fixed/fixed-file-roundtrip
		{
			id: "fixed-file-roundtrip",
			schema: `package probe
fixed table T {
    a int32 = 11
    b string(6) = "ok"
}
`,
			root:  "T",
			body:  14,
			count: 2,
			cppSetup: `
    value[1].a = 22;
    memset( value[1].b, 0, sizeof( value[1].b ) );
    memcpy( value[1].b, "test", 4 );
    value[1].b_length = 4;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = 22
buffer.fill(v1.B, 0, buffer.len(v1.B))
buffer.writestring(v1.B, 0, "test")
v1.BLength = 4
values = { v0, v1 }
`,
			want: []string{
				"r0.a=11", `r0.b="ok"`, "r0.b_length=2",
				"r1.a=22", `r1.b="test"`, "r1.b_length=4",
			},
		},
		// port/luau/fixed/record-measurement
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
		// port/luau/fixed/file-envelope
		{
			id: "file-envelope",
			schema: `package probe
fixed table T {
    x int32 = 1
    y int32 = 2
}
`,
			root:  "T",
			body:  8,
			count: 2,
			cppSetup: `
    value[1].x = 10;
    value[1].y = 20;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.X = 10
v1.Y = 20
values = { v0, v1 }
`,
			want: []string{"r0.x=1", "r0.y=2", "r1.x=10", "r1.y=20"},
		},
		// port/luau/fixed/batch-capacity
		{
			id: "batch-capacity",
			schema: `package probe
fixed table T {
    x int32 = 1
    y int32 = 2
}
`,
			root:  "T",
			body:  8,
			count: 3,
			cppSetup: `
    value[1].x = 3;
    value[1].y = 4;
    value[2].x = 5;
    value[2].y = 6;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.X = 3
v1.Y = 4
local v2 = T.NewT()
v2.X = 5
v2.Y = 6
values = { v0, v1, v2 }
`,
			want: []string{"r0.x=1", "r0.y=2", "r1.x=3", "r1.y=4", "r2.x=5", "r2.y=6"},
		},
		// port/luau/fixed/plan-selection
		{
			id: "plan-selection",
			schema: `package probe
fixed table T {
    x int32 = 11
    y int32 = 22
}
`,
			root:  "T",
			body:  8,
			count: 2,
			cppSetup: `
    value[1].x = 33;
    value[1].y = 44;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.X = 33
v1.Y = 44
values = { v0, v1 }
`,
			want: []string{"r0.x=11", "r0.y=22", "r1.x=33", "r1.y=44"},
		},
		// port/luau/fixed/compiled-plans
		{
			id: "compiled-plans",
			schema: `package probe
fixed table T {
    a uint32 = 1
    b int64 = 10
    c int32 = 99
}
`,
			root:  "T",
			body:  16,
			count: 2,
			cppSetup: `
    value[1].a = 100U;
    value[1].b = -20LL;
    value[1].c = 55;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.A = 100
v1.B = -20i
v1.C = 55
values = { v0, v1 }
`,
			want: []string{
				"r0.a=1", "r0.b=10", "r0.c=99",
				"r1.a=100", "r1.b=-20", "r1.c=55",
			},
		},
		// port/luau/fixed/definition-hash
		{
			id: "definition-hash",
			schema: `package probe
fixed table T {
    x int32 = 5
    b bytes(6) = "hi"
}
`,
			root:  "T",
			body:  14,
			count: 2,
			cppSetup: `
    value[1].x = 9;
    memset( value[1].b, 0, sizeof( value[1].b ) );
    memcpy( value[1].b, "abc", 3 );
    value[1].b_length = 3;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.X = 9
buffer.fill(v1.B, 0, buffer.len(v1.B))
buffer.writestring(v1.B, 0, "abc")
v1.BLength = 3
values = { v0, v1 }
`,
			want: []string{
				"r0.x=5", "r0.b_length=2", "r0.b[0]=104", "r0.b[1]=105",
				"r1.x=9", "r1.b_length=3", "r1.b[0]=97", "r1.b[1]=98", "r1.b[2]=99",
			},
		},
		// port/luau/fixed/fixed-closure
		{
			id: "fixed-closure",
			schema: `package probe
type Nested {
    a int16 = 3
    b int16 = 4
}
fixed table T {
    n Nested
    z int32 = 7
}
`,
			root:  "T",
			body:  8,
			count: 2,
			cppSetup: `
    value[1].n.a = 30;
    value[1].n.b = 40;
    value[1].z = 70;
`,
			build: `
local v0 = T.NewT()
local v1 = T.NewT()
v1.N.A = 30
v1.N.B = 40
v1.Z = 70
values = { v0, v1 }
`,
			want: []string{
				"r0.n.a=3", "r0.n.b=4", "r0.z=7",
				"r1.n.a=30", "r1.n.b=40", "r1.z=70",
			},
		},
	}
}

func (c luauGroupDCase) toRowCase() fixedproof.RowCase {
	return fixedproof.RowCase{
		ID:       c.id,
		Schema:   c.schema,
		Root:     c.root,
		Body:     c.body,
		Count:    c.count,
		CppSetup: c.cppSetup,
	}
}

const luauGroupDFixedDumper = `local St = require("./schema_tables")
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
			local text = if length > 0 then buffer.readstring(v, 0, length) else ""
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

type luauGroupDGeneratedUnit struct {
	dir   string
	files map[string][]byte
}

func luauGroupDProgram(g *luauGroupDGeneratedUnit, c luauGroupDCase) string {
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
	b.WriteString(luauGroupDFixedDumper)
	b.WriteString("local values: { any } = {}\n")
	b.WriteString(c.build)
	fmt.Fprintf(&b, `local d = T.%[1]sDescriptor
local fixed = { hash = T.%[1]sFixedHash, recordBytes = T.%[1]sFixedRecordBytes, layout = T.%[1]sFixedLayout }
local emptyOut = F.save(d, fixed, {})
local out = F.save(d, fixed, values)
local m0 = buffer.len(emptyOut)
local mn = m0 + #values * (8 + fixed.recordBytes)
print(string.format("@m %%d %%d %%d %%d", fixed.recordBytes, m0, mn, buffer.len(out)))
print("@x " .. hex(out))
local report: any = {}
local back = F.read(d, fixed, inputs.cpp, report)
if back == nil then
	print("@refused")
	return
end
print(string.format("@ok %%d u%%d k%%d c%%d w%%d", #back, report.unknown, report.kindMismatch, report.clamped, report.widened))
for i, v in back do
	dumpRecord(d, v, "r" .. int(i - 1))
end
for _, l in lines do
	print(l)
end
local re = F.save(d, fixed, back)
print("@y " .. hex(re))
`, c.root)
	return b.String()
}

type luauGroupDProof struct {
	measure        string
	saved, resaved []byte
	load           string
	dump           map[string]string
	raw            string
}

func luauGroupDHexDecode(s string) ([]byte, error) {
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

func runLuauGroupDProof(t *testing.T, g *luauGroupDGeneratedUnit, c luauGroupDCase, cppBytes []byte) luauGroupDProof {
	t.Helper()
	if err := os.WriteFile(filepath.Join(g.dir, "main.luau"), []byte(luauGroupDProgram(g, c)), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luaurun.Run(t, g.dir, "main.luau", map[string][]byte{"cpp": cppBytes})
	p := luauGroupDProof{dump: map[string]string{}, raw: string(out)}
	for l := range strings.SplitSeq(strings.TrimSpace(string(out)), "\n") {
		if l == "" {
			continue
		}
		var err error
		switch {
		case strings.HasPrefix(l, "@m "):
			p.measure = strings.TrimPrefix(l, "@m ")
		case strings.HasPrefix(l, "@x "):
			p.saved, err = luauGroupDHexDecode(strings.TrimPrefix(l, "@x "))
		case strings.HasPrefix(l, "@y "):
			p.resaved, err = luauGroupDHexDecode(strings.TrimPrefix(l, "@y "))
		case strings.HasPrefix(l, "@"):
			p.load = strings.TrimPrefix(l, "@")
		default:
			k, v, _ := strings.Cut(l, "=")
			p.dump[k] = v
		}
		if err != nil {
			t.Fatal(err)
		}
	}
	return p
}

func luauGroupDWantMismatch(want []string, dump map[string]string) string {
	for _, w := range want {
		k, v, _ := strings.Cut(w, "=")
		if got, ok := dump[k]; !ok || got != v {
			return fmt.Sprintf("%s: got %q, want %q", k, got, v)
		}
	}
	return ""
}

func luauGroupDFirstDiff(a, b []byte) string {
	for i := 0; i < len(a) && i < len(b); i++ {
		if a[i] != b[i] {
			return fmt.Sprintf("byte %d: got 0x%02x, want 0x%02x", i, a[i], b[i])
		}
	}
	return fmt.Sprintf("len %d, want %d", len(a), len(b))
}

func luauGroupDUnit(t *testing.T, c luauGroupDCase) (*ir.Unit, *luauGroupDGeneratedUnit) {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, "Probe.schema")
	if err := os.WriteFile(path, []byte(c.schema), 0o644); err != nil {
		t.Fatal(err)
	}
	u, err := compiler.New().Load([]string{path})
	if err != nil {
		t.Fatalf("%s: %v", c.id, err)
	}
	found := false
	for _, st := range ir.TableFixedFormRoots(u) {
		if st.Name == c.root {
			found = true
			if got := ir.TableFixedTypeBytes(st); got != c.body {
				t.Fatalf("%s: the record body is %d bytes, the row says %d", c.id, got, c.body)
			}
			break
		}
	}
	if !found {
		t.Fatalf("%s: %s is not a fixed root", c.id, c.root)
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
	g := &luauGroupDGeneratedUnit{dir: dir, files: files}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return u, g
}

// TestLuauFixedProofGroupD proves each port/luau/fixed/* row of group D
// (defaults_envelope_plans) against the live C++ reference: Luau's save of the
// row's records is the reference's file byte for byte; the measure is the
// header and layout plus one hash and body per record, and the body size is
// the row's; Luau loads the reference's file clean (no refusal, no counter)
// and lands every value the row names; Luau's re-save of what it loaded is the
// reference's file; and the reference loads Luau's file clean and re-saves it
// to its own bytes.
func TestLuauFixedProofGroupD(t *testing.T) {
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	t.Parallel()
	for _, c := range luauGroupDCases() {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			u, g := luauGroupDUnit(t, c)
			cppBytes, verifyInCpp := fixedproof.CppOracle(t, c.toRowCase(), u)
			p := runLuauGroupDProof(t, g, c, cppBytes)
			if p.saved == nil {
				t.Fatalf("luau did not save:\n%s", p.raw)
			}
			layoutLen := int(uint32(cppBytes[16]) | uint32(cppBytes[17])<<8 | uint32(cppBytes[18])<<16 | uint32(cppBytes[19])<<24)
			empty := 20 + layoutLen
			if want := fmt.Sprintf("%d %d %d %d", c.body, empty, empty+c.count*int(8+c.body), len(cppBytes)); p.measure != want {
				t.Fatalf("the measure (body, measure(0), measure(n), saved): %s, want %s", p.measure, want)
			}
			if !bytes.Equal(p.saved, cppBytes) {
				t.Fatalf("luau's save is not the C++ reference's bytes: %s\nluau=%x\ncpp=%x", luauGroupDFirstDiff(p.saved, cppBytes), p.saved, cppBytes)
			}
			if want := fmt.Sprintf("ok %d u0 k0 c0 w0", c.count); p.load != want {
				t.Fatalf("luau's load of the reference's file: %s, want %s", p.load, want)
			}
			if m := luauGroupDWantMismatch(c.want, p.dump); m != "" {
				t.Fatalf("luau's load of the reference's file: %s", m)
			}
			if !bytes.Equal(p.resaved, cppBytes) {
				t.Fatalf("luau's re-save of the reference's file differs: %s", luauGroupDFirstDiff(p.resaved, cppBytes))
			}
			if err := verifyInCpp(p.saved); err != nil {
				t.Fatalf("the C++ reference does not load luau's file back to its own bytes: %v", err)
			}
		})
	}
}

// TestLuauFixedProofGroupDNegativeControl breaks each row two ways and requires
// the gates TestLuauFixedProofGroupD holds green to go red. The FILE: both a
// corrupted form byte and a flipped first body byte on the last record of the
// reference's file — the C++ verifier refuses both, and Luau's load either
// refuses or lands a value the row's want lines and re-save byte gate reject.
// The WRITER: schema_fixed.luau's save patched to flip the last byte of each
// record body — Luau's file is no longer the reference's and the C++ verifier
// refuses it.
func TestLuauFixedProofGroupDNegativeControl(t *testing.T) {
	slowtest.Gate(t, "luau and c++ (it runs the emitted fixed-form runtime against a compiled C++ reference)")
	t.Parallel()
	const anchor = "\t\twriteStruct(d, root, v, rec)\n"
	const planted = anchor + "\t\tbuffer.writeu8(rec, fixed.recordBytes - 1, bit32.bxor(buffer.readu8(rec, fixed.recordBytes - 1), 1))\n"
	for _, c := range luauGroupDCases() {
		t.Run(c.id, func(t *testing.T) {
			t.Parallel()
			u, g := luauGroupDUnit(t, c)
			cppBytes, verifyInCpp := fixedproof.CppOracle(t, c.toRowCase(), u)

			corruptForm := bytes.Clone(cppBytes)
			corruptForm[0] ^= 0x01
			if err := verifyInCpp(corruptForm); err == nil {
				t.Fatal("the C++ verifier accepted a corrupted form byte")
			}
			if p := runLuauGroupDProof(t, g, c, corruptForm); p.load != "refused" {
				t.Fatalf("luau accepted a corrupted form byte: %s", p.load)
			}

			layoutLen := int(uint32(cppBytes[16]) | uint32(cppBytes[17])<<8 | uint32(cppBytes[18])<<16 | uint32(cppBytes[19])<<24)
			at := 20 + layoutLen + (c.count-1)*int(8+c.body) + 8
			mutated := bytes.Clone(cppBytes)
			mutated[at] ^= 0x01
			if err := verifyInCpp(mutated); err == nil {
				t.Fatal("the C++ verifier accepted a flipped body byte")
			}
			if p := runLuauGroupDProof(t, g, c, mutated); p.load == fmt.Sprintf("ok %d u0 k0 c0 w0", c.count) && luauGroupDWantMismatch(c.want, p.dump) == "" && bytes.Equal(p.resaved, cppBytes) {
				t.Fatalf("luau loaded a flipped body byte and every want line and re-save still hold: the row's values prove nothing")
			}

			src := string(g.files["schema_fixed.luau"])
			if strings.Count(src, anchor) != 1 {
				t.Fatal("the record write anchor is not in schema_fixed.luau exactly once")
			}
			if err := os.WriteFile(filepath.Join(g.dir, "schema_fixed.luau"), []byte(strings.Replace(src, anchor, planted, 1)), 0o644); err != nil {
				t.Fatal(err)
			}
			p := runLuauGroupDProof(t, g, c, cppBytes)
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
