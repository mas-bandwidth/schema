package moo

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/check"
	"github.com/mas-bandwidth/schema/v2/internal/codegen/moo/moorun"
	"github.com/mas-bandwidth/schema/v2/internal/parser"
	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/ir"
)

// wireGoldenNames is every C++-pinned golden the probe writes, in its order.
var wireGoldenNames = []string{
	"shipcreate_flags", "rigidbody_moving", "rigidbody_at_rest", "chat",
	"probe_header", "compressed_probe", "probebits", "probecollider",
	"testdata", "inputpacket", "probearray", "degenerate", "clauses", "joins",
}

func repo(parts ...string) string {
	return filepath.Join(append([]string{"..", "..", ".."}, parts...)...)
}

func unitFromDir(t testing.TB, dir string) *ir.Unit {
	t.Helper()
	paths, err := filepath.Glob(filepath.Join(dir, "*.schema"))
	if err != nil || len(paths) == 0 {
		t.Fatalf("no schema files under %s: %v", dir, err)
	}
	sort.Strings(paths)
	return unitFromFiles(t, paths)
}

func unitFromFiles(t testing.TB, paths []string) *ir.Unit {
	t.Helper()
	var files []check.SourceFile
	for _, p := range paths {
		src, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(p)
		tree, errs := parser.Parse(name, src)
		if len(errs) != 0 {
			t.Fatalf("parse %s: %v", name, errs)
		}
		files = append(files, check.SourceFile{Path: p, Name: name, Base: strings.TrimSuffix(name, ".schema"), Bytes: src, AST: tree})
	}
	u, errs := check.Unit(files)
	if len(errs) != 0 {
		t.Fatalf("check: %v", errs)
	}
	return u
}

// generateInto generates u into a fresh temp dir and returns it.
func generateInto(t testing.TB, u *ir.Unit) (string, map[string][]byte) {
	t.Helper()
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir, files
}

func writeFile(t testing.TB, dir, name, data string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// The probe's macros: moo has no float and no 64-bit integer, so the probe
// spells those values as macros the Go side expands into exact moo:
// F32(x) is float32(x)'s binary32 pattern, F64(x) the binary64 pattern as a
// serialize.Uint64, U64(n) / I64(n) a wide literal of n.
var macro = regexp.MustCompile(`\b(F32|F64|U64|I64)\(([-+0-9.eExXa-fA-F]+)\)`)

func expandMacros(t testing.TB, src string) string {
	t.Helper()
	return macro.ReplaceAllStringFunc(src, func(m string) string {
		parts := macro.FindStringSubmatch(m)
		switch parts[1] {
		case "F32", "F64":
			v, err := strconv.ParseFloat(parts[2], 64)
			if err != nil {
				t.Fatalf("probe macro %s: %v", m, err)
			}
			if parts[1] == "F32" {
				return fmt.Sprintf("0x%08x", float32Bits(v))
			}
			return wideNew("Uint64", new(big.Int).SetUint64(math.Float64bits(v)), 64)
		}
		n, ok := new(big.Int).SetString(parts[2], 0)
		if !ok {
			t.Fatalf("probe macro %s: not an integer", m)
		}
		class := "Uint64"
		if parts[1] == "I64" {
			class = "Int64"
		}
		return wideNew(class, n, 64)
	})
}

// TestMooTypeWireWritesTheGoldens generates examples/ into a temp dir, builds
// each instance the Go cross-language test builds (test/go/main.go) before a
// golden call, writes it in moo, and byte-compares against
// testdata/wire/<name>.bin: the C++ reference's bytes are the judge.
func TestMooTypeWireWritesTheGoldens(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, _ := generateInto(t, unitFromDir(t, repo("examples")))
	writeFile(t, dir, "main.moo", expandMacros(t, wireProbe))
	lines := moorun.HexLines(t, moorun.Run(t, dir, "main.moo", nil))
	if len(lines) != len(wireGoldenNames) {
		t.Fatalf("probe produced %d wire lines, want %d", len(lines), len(wireGoldenNames))
	}
	for i, name := range wireGoldenNames {
		golden, err := os.ReadFile(repo("testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lines[i], golden) {
			t.Errorf("wire golden %s: moo bytes\n  %x\n!= C++-pinned\n  %x", name, lines[i], golden)
		}
	}
}

// TestMooTypeWireWriteNegativeControl patches the emitted write (one anchor,
// matched exactly once) so one bool rides twice, and requires the byte
// comparison to go red: the golden gate can see a one-bit drift.
func TestMooTypeWireWriteNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs the generated modules under the pinned interpreter)")
	t.Parallel()
	dir, files := generateInto(t, unitFromDir(t, repo("examples")))
	types := string(files["Types.moo"])
	anchor := "\tBOOL.value = value.HasFlags\n"
	if strings.Count(types, anchor) != 1 {
		t.Fatalf("negative-control anchor matched %d times in Types.moo", strings.Count(types, anchor))
	}
	types = strings.Replace(types, anchor, anchor+"\tstream:serializeBool(BOOL)\n", 1)
	writeFile(t, dir, "Types.moo", types)
	writeFile(t, dir, "main.moo", expandMacros(t, wireProbe))
	lines := moorun.HexLines(t, moorun.Run(t, dir, "main.moo", nil))
	if len(lines) != len(wireGoldenNames) {
		t.Fatalf("negative control: the probe produced %d wire lines, want %d", len(lines), len(wireGoldenNames))
	}
	// EXACTLY ITS ROWS: the one golden that writes a ShipCreate goes red, and
	// every other golden still matches the C++ reference byte for byte.
	var red []string
	for i, name := range wireGoldenNames {
		golden, err := os.ReadFile(repo("testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lines[i], golden) {
			red = append(red, name)
		}
	}
	if strings.Join(red, ",") != "shipcreate_flags" {
		t.Fatalf("negative control: the doubled bool redded %v, want exactly [shipcreate_flags]", red)
	}
}

const wireProbe = `--!strict
local serialize = require("./serialize")
local Types = require("./Types")
local Enums = require("./Enums")
local Wire = require("./Wire")
local Degenerate = require("./Degenerate")
local Clauses = require("./Clauses")
local Joins = require("./Joins")
local U64 = serialize.Uint64

local function tohex(fn: (any) -> boolean): string
	local stream = serialize.WriteStream.new(buffer.create(8192))
	assert(fn(stream), "write refused")
	stream:flush()
	local b = stream:data()
	local parts = table.create(buffer.len(b))
	for k = 0, buffer.len(b) - 1 do
		parts[k + 1] = string.format("%02x", buffer.readu8(b, k))
	end
	return table.concat(parts)
end
local function emit(fn: (any) -> boolean) print(tohex(fn)) end
local function setbuf(buf: buffer, s: string): number
	if #s > 0 then buffer.writestring(buf, 0, s) end
	return #s
end

do -- shipcreate_flags
	local v = Types.NewShipCreate()
	v.ShipType = Enums.ShipType.Bomber
	v.Position.X = 1000; v.Position.Y = -2000; v.Position.Z = 3000
	v.HasFlags = true
	v.Flags = U64.bor(Enums.ShipFlagsBoosting, Enums.ShipFlagsAiming)
	v.Team = Enums.Team.Blue
	v.Health = 750
	v.Thrust = 55
	emit(function(s) return Types.WriteShipCreate(s, v) end)
end

do -- rigidbody_moving and rigidbody_at_rest
	local v = Types.NewRigidBody()
	v.Position.X = F64(1.5); v.Position.Y = F64(-2.5); v.Position.Z = F64(3.25)
	v.Orientation.X = F64(0.1); v.Orientation.Y = F64(0.2); v.Orientation.Z = F64(0.3); v.Orientation.W = F64(0.9)
	v.AtRest = false
	v.LinearVelocity.X = F64(10.0); v.LinearVelocity.Y = F64(20.0); v.LinearVelocity.Z = F64(-3.0)
	v.AngularVelocity.X = F64(0.25); v.AngularVelocity.Y = F64(0.5); v.AngularVelocity.Z = F64(0.75)
	emit(function(s) return Types.WriteRigidBody(s, v) end)
	v.AtRest = true
	emit(function(s) return Types.WriteRigidBody(s, v) end)
end

do -- chat
	local v = Wire.NewChat()
	v.Text = "wire parity"
	emit(function(s) return Wire.WriteChat(s, v) end)
end

do -- probe_header
	local v = Wire.NewProbeHeader()
	v.Version = 5
	v.ProbeId = U64(0x1122334455667788)
	emit(function(s) return Wire.WriteProbeHeader(s, v) end)
end

do -- compressed_probe
	local v = Wire.NewCompressedProbe()
	v.Boundary = F32(0.005)
	v.Offset = F32(-4.8585)
	emit(function(s) return Wire.WriteCompressedProbe(s, v) end)
end

do -- probebits
	local v = Wire.NewProbeBits()
	v.Small = 0x1FF
	v.Boundary = U64(0x1FFFFFFFF)
	v.Wide = U64(0xFEDCBA9876543210)
	v.Sensor = 4294967295
	v.Nonce = U64(0xffffffffffffffff)
	emit(function(s) return Wire.WriteProbeBits(s, v) end)
end

do -- probecollider
	local v = Wire.NewProbeCollider()
	v.Armor = 7
	v.Shape.Type = Wire.ProbeShapeType.Slab
	v.Shape.Slab.Width = 42
	v.Shape.Slab.Height = 9
	v.ExtrasCount = 1
	v.Extras[1].Type = Wire.ProbeShapeType.Ring
	v.Extras[1].Ring.Radius = 777
	emit(function(s) return Wire.WriteProbeCollider(s, v) end)
end

do -- testdata
	local v = Wire.NewTestData()
	v.A = -100
	v.B = 100
	v.C = 149
	v.D = 0x11
	v.E = 0x22
	v.F = 0x33
	v.G = true
	v.ItemsCount = 3
	v.Items[1] = 0
	v.Items[2] = 128
	v.Items[3] = 255
	v.FloatValue = F32(3.1415926)
	v.CompressedFloatValue = F32(2.5)
	v.DoubleValue = F64(0.3333333333333333)
	v.Int8Value = -128
	v.Int16Value = -32768
	v.Uint8Value = 255
	v.Uint16Value = 65535
	v.Uint32Value = 4294967295
	v.Uint64Value = U64(0xffffffffffffffff)
	v.Int64Full = I64(-0x8000000000000000)
	v.Int64Range = I64(-999999999999)
	for k = 1, 17 do buffer.writeu8(v.FixedBytes, k - 1, (k - 1) * 3) end
	v.Text = "the quick brown fox"
	emit(function(s) return Wire.WriteTestData(s, v) end)
end

do -- inputpacket
	local v = Types.NewInputPacket()
	v.SynchronizeSequence = 7
	v.CurrentFrame = U64(123456789)
	v.StartFrame = U64(123456780)
	v.InputsCount = 2
	v.Inputs[1].Throttle = F32(0.5)
	v.Inputs[1].Fire = true
	v.Inputs[2].StickX = F32(-0.25)
	v.Inputs[2].Boost = true
	emit(function(s) return Types.WriteInputPacket(s, v) end)
end

do -- probearray
	local v = Wire.NewProbeArray()
	v.Samples[1].Orientation = F32(90.0)
	v.Samples[1].RawDelta = -5
	v.Samples[1].BigDelta = I64(-1234567890123)
	v.Samples[1].Weapon = Wire.Weapon.Laser
	v.Samples[1].HasTarget = true
	v.Samples[1].TargetId = 777
	v.Samples[1].SamplesCount = 1
	v.Samples[1].Samples[1] = 42
	v.Samples[2].Active = false
	v.Samples[2].Orientation = F32(-45.5)
	v.Samples[2].RawDelta = 7
	v.Samples[2].BigDelta = I64(99)
	v.Samples[2].IdleTicks = 1000
	v.Samples[2].SamplesCount = 2
	v.Samples[2].Samples[1] = 7
	v.Samples[2].Samples[2] = 8
	v.Config.Retries = 3
	v.Config.Preferred = Wire.Weapon.Missile
	emit(function(s) return Wire.WriteProbeArray(s, v) end)
end

do -- degenerate
	local v1 = Degenerate.NewVec2(); v1.X = F64(1.5); v1.Y = F64(-2.25)
	local v2 = Degenerate.NewSpanF64(); v2.Values = { F64(3.5), F64(-4.75) }
	local v3 = Degenerate.NewSpanU64(); v3.Values = { U64(0xDEADBEEFCAFEBABE), U64(1) }
	local v4 = Degenerate.NewSpanI64(); v4.Values = { I64(-1234567890123), I64(42) }
	local v5 = Degenerate.NewSpanOne(); v5.Values = { U64(0x0123456789ABCDEF) }
	local v6 = Degenerate.NewSpanChunk(); v6.Values = { 0x1111, 0x2222, 0x3333, 0x4444 }
	local v7 = Degenerate.NewSpanTail(); v7.Values = { F64(6.125), F64(-7.0) }; v7.Tail = 0xFEEDFACE
	local v8 = Degenerate.NewSpanTwice(); v8.A = { F64(8.5), F64(9.5) }; v8.B = { F64(-10.5), F64(-11.5) }
	local v9 = Degenerate.NewTrio(); v9.A = 0xABCDE; v9.B = 0x12345; v9.C = 0xFFFFF
	local v10 = Degenerate.NewTrioSole(); v10.Inner.A = 1; v10.Inner.B = 2; v10.Inner.C = 3
	local v11 = Degenerate.NewTrioFirst(); v11.Inner.A = 0xAAAAA; v11.Inner.B = 0x55555; v11.Inner.C = 0xF0F0F; v11.Trailer = 0xBEEF
	local v12 = Degenerate.NewTrioStraddle()
	v12.Pad0 = U64(0x0011223344556677); v12.Pad1 = U64(0x8899AABBCCDDEEFF)
	v12.Pad2 = U64(0xffffffffffffffff); v12.Pad3 = U64(0); v12.Pad4 = U64(0x123456789ABCDEF0); v12.Pad5 = 0xABCDEF
	v12.Inner.A = 0x11111; v12.Inner.B = 0x22222; v12.Inner.C = 0x33333
	local parts = {}
	local function one(fn: (any) -> boolean) parts[#parts + 1] = tohex(fn) end
	one(function(s) return Degenerate.WriteVec2(s, v1) end)
	one(function(s) return Degenerate.WriteSpanF64(s, v2) end)
	one(function(s) return Degenerate.WriteSpanU64(s, v3) end)
	one(function(s) return Degenerate.WriteSpanI64(s, v4) end)
	one(function(s) return Degenerate.WriteSpanOne(s, v5) end)
	one(function(s) return Degenerate.WriteSpanChunk(s, v6) end)
	one(function(s) return Degenerate.WriteSpanTail(s, v7) end)
	one(function(s) return Degenerate.WriteSpanTwice(s, v8) end)
	one(function(s) return Degenerate.WriteTrio(s, v9) end)
	one(function(s) return Degenerate.WriteTrioSole(s, v10) end)
	one(function(s) return Degenerate.WriteTrioFirst(s, v11) end)
	one(function(s) return Degenerate.WriteTrioStraddle(s, v12) end)
	print(table.concat(parts))
end

do -- clauses
	local parts = {}
	local function one(fn: (any) -> boolean) parts[#parts + 1] = tohex(fn) end
	for _, c in { 0, 1, 3, 4, 5, 7, 12 } do
		local v = Clauses.NewW13(); v.ItemsCount = c
		for k = 1, c do v.Items[k] = 8191 - (k - 1) * 733 end
		one(function(s) return Clauses.WriteW13(s, v) end)
	end
	for _, c in { 0, 1, 2, 3, 4, 9 } do
		local v = Clauses.NewW17(); v.ItemsCount = c
		for k = 1, c do v.Items[k] = 131071 - (k - 1) * 11117 end
		one(function(s) return Clauses.WriteW17(s, v) end)
	end
	for _, c in { 0, 1, 2, 3, 6 } do
		local v = Clauses.NewW26(); v.ItemsCount = c
		for k = 1, c do v.Items[k] = 67108863 - (k - 1) * 5555555 end
		one(function(s) return Clauses.WriteW26(s, v) end)
	end
	for _, c in { 0, 1, 3, 4, 5, 20 } do
		local v = Clauses.NewW1(); v.ItemsCount = c
		for k = 1, c do v.Items[k] = (k - 1) % 2 end
		one(function(s) return Clauses.WriteW1(s, v) end)
	end
	for c = 0, 3 do
		local v = Clauses.NewW52(); v.ItemsCount = c
		for k = 1, c do v.Items[k] = U64.sub(U64(4503599627370495), U64.mul(k - 1, 123456789)) end
		one(function(s) return Clauses.WriteW52(s, v) end)
	end
	for c = 0, 3 do
		local v = Clauses.NewW50(); v.ItemsCount = c
		for k = 1, c do v.Items[k] = U64.sub(U64(1125899906842623), U64.mul(k - 1, 987654321)) end
		one(function(s) return Clauses.WriteW50(s, v) end)
	end
	do
		local v = Clauses.NewF13()
		for k = 1, 7 do v.Items[k] = 8191 - (k - 1) * 911 end
		one(function(s) return Clauses.WriteF13(s, v) end)
	end
	for _, c in { 0, 1, 3, 4, 5, 10 } do
		local v = Clauses.NewArrTri3(); v.ItemsCount = c
		for k = 1, c do v.Items[k].A = (k - 1) % 2; v.Items[k].B = (k - 1) % 4 end
		one(function(s) return Clauses.WriteArrTri3(s, v) end)
	end
	do
		local v = Clauses.NewArrEleven()
		for k = 1, 9 do v.Items[k].A = (k - 1) % 8; v.Items[k].B = 255 - (k - 1) * 17 end
		one(function(s) return Clauses.WriteArrEleven(s, v) end)
	end
	for k = 1, 3 do
		local v = Clauses.NewHoldsEmptyUnion(); v.Lead = 21; v.Tail = 99
		if k == 2 then v.U.Type = Clauses.EmptyUnionType.A end
		if k == 3 then v.U.Type = Clauses.EmptyUnionType.B end
		one(function(s) return Clauses.WriteHoldsEmptyUnion(s, v) end)
	end
	do
		local v = Clauses.NewStrs(); v.Lead = 21; v.Tail = 5
		one(function(s) return Clauses.WriteStrs(s, v) end)
		local full = Clauses.NewStrs(); full.Lead = 21; full.Tail = 5
		full.S = "abcdefgh"
		full.BLength = setbuf(full.B, "\xF0\xF1\xF2\xF3\xF4\xF5\xF6\xF7")
		one(function(s) return Clauses.WriteStrs(s, full) end)
		local part = Clauses.NewStrs(); part.Lead = 21; part.Tail = 5
		part.S = "xyz"
		part.BLength = setbuf(part.B, "\x01\x02\x03")
		one(function(s) return Clauses.WriteStrs(s, part) end)
	end
	for c = 0, 4 do
		local v = Clauses.NewArrNested(); v.Lead = 21; v.Tail = 5; v.ItemsCount = c
		for k = 1, c do v.Items[k].A = (k - 1) % 8; v.Items[k].B = 200 - (k - 1) * 7 end
		one(function(s) return Clauses.WriteArrNested(s, v) end)
	end
	do
		local v = Clauses.NewSole(); v.Only = 5555
		one(function(s) return Clauses.WriteSole(s, v) end)
	end
	print(table.concat(parts))
end

do -- joins
	local parts = {}
	local function one(fn: (any) -> boolean) parts[#parts + 1] = tohex(fn) end
	for f = 0, 1 do
		local flag = f ~= 0
		local v = Joins.NewArmsAgree(); v.Lead = 21; v.Flag = flag; v.A = 1234; v.B = 1500; v.Tail = 99
		one(function(s) return Joins.WriteArmsAgree(s, v) end)
		local d = Joins.NewArmsDisagree(); d.Lead = 21; d.Flag = flag; d.A = 1234; d.B = 5; d.Tail = 99
		one(function(s) return Joins.WriteArmsDisagree(s, d) end)
		local e = Joins.NewArmEmpty(); e.Lead = 21; e.Flag = flag; e.A = 456789; e.Tail = 99
		one(function(s) return Joins.WriteArmEmpty(s, e) end)
		local a = Joins.NewArmAlign(); a.Lead = 21; a.Flag = flag; a.S = "abcd"; a.B = 1000; a.Tail = 99
		one(function(s) return Joins.WriteArmAlign(s, a) end)
		local ae = Joins.NewArmAlign(); ae.Lead = 21; ae.Flag = flag; ae.B = 1000; ae.Tail = 99
		one(function(s) return Joins.WriteArmAlign(s, ae) end)
	end
	for o = 0, 1 do
		for n = 0, 1 do
			local v = Joins.NewArmsNested()
			v.Lead = 5; v.Outer = o ~= 0; v.Inner = n ~= 0
			v.X = 500000000; v.Y = 17; v.Z = 4000; v.Tail = 33
			one(function(s) return Joins.WriteArmsNested(s, v) end)
		end
	end
	for f = 0, 1 do
		for c = 0, 3 do
			local v = Joins.NewArmArray(); v.Lead = 21; v.Flag = f ~= 0; v.ItemsCount = c; v.B = 300; v.Tail = 99
			for k = 1, c do v.Items[k] = 8191 - (k - 1) * 777 end
			one(function(s) return Joins.WriteArmArray(s, v) end)
		end
	end
	do
		local v = Joins.NewHoldsUneven(); v.Lead = 21; v.Tail = 1500
		one(function(s) return Joins.WriteHoldsUneven(s, v) end)
		local n = Joins.NewHoldsUneven(); n.Lead = 21; n.Tail = 1500
		n.U.Type = Joins.UnevenType.Narrow; n.U.Narrow.N = 5
		one(function(s) return Joins.WriteHoldsUneven(s, n) end)
		local w = Joins.NewHoldsUneven(); w.Lead = 21; w.Tail = 1500
		w.U.Type = Joins.UnevenType.Wide; w.U.Wide.W = U64(123456789012)
		one(function(s) return Joins.WriteHoldsUneven(s, w) end)
	end
	for c = 0, 3 do
		local v = Joins.NewArrUneven(); v.Lead = 21; v.Tail = 5; v.ItemsCount = c
		for k = 1, c do
			if (k - 1) % 2 == 0 then
				v.Items[k].Type = Joins.UnevenType.Narrow
				v.Items[k].Narrow.N = (k - 1) % 8
			else
				v.Items[k].Type = Joins.UnevenType.Wide
				v.Items[k].Wide.W = U64.add(U64(99887766554), k - 1)
			end
		end
		one(function(s) return Joins.WriteArrUneven(s, v) end)
	end
	for c = 0, 3 do
		for sl = 0, 4, 4 do
			local v = Joins.NewRegainAfterAlign()
			v.Lead = 21; v.ItemsCount = c
			v.S = if sl ~= 0 then "wxyz" else ""
			v.P = 0xDEADBEEF; v.Q = 536870905; v.R = 524285; v.Tail = 9
			for k = 1, c do v.Items[k] = 8191 - (k - 1) * 999 end
			one(function(s) return Joins.WriteRegainAfterAlign(s, v) end)
		end
	end
	print(table.concat(parts))
end
`
