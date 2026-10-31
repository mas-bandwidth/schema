package lua

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

var goldenNames = []string{
	"shipcreate_flags", "rigidbody_moving", "rigidbody_at_rest", "chat",
	"probe_header", "compressed_probe", "probebits", "probecollider",
	"testdata", "inputpacket", "probearray", "degenerate", "clauses", "joins",
}

// TestLuaTypeWireWritesTheGoldens generates examples/*.schema into a
// t.TempDir, builds each instance the Go cross-language test builds
// (test/go/main.go) before a goldenWire call, writes it in Lua, and
// byte-compares against testdata/wire/<name>.bin (docs/PORT-LUA.md; the C++
// reference's bytes are the judge, never the Lua leg's own output).
func TestLuaTypeWireWritesTheGoldens(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(wireProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
	lines := luarun.HexLines(t, out)
	if len(lines) != len(goldenNames) {
		t.Fatalf("probe produced %d wire lines, want %d", len(lines), len(goldenNames))
	}
	for i, name := range goldenNames {
		t.Logf("golden %s", name)
		golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lines[i], golden) {
			t.Errorf("wire golden %s: lua bytes %x != C++-pinned %x", name, lines[i], golden)
		}
	}
}

// TestLuaTypeWireWriteNegativeControl patches the emitted write in memory (one
// anchor, matched exactly once) so one bool writes two bits, and requires the
// byte comparison to go red.
func TestLuaTypeWireWriteNegativeControl(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)

	types := files["Types.lua"]
	anchor := "stream:serializeBool({ value = v.HasFlags })"
	if strings.Count(string(types), anchor) != 1 {
		t.Fatalf("negative-control anchor %q matched %d times in emitted Types.lua", anchor, strings.Count(string(types), anchor))
	}
	patched := strings.Replace(string(types), anchor,
		"(function() stream:serializeBool({ value = v.HasFlags }); return stream:serializeBool({ value = v.HasFlags }) end)()", 1)
	files["Types.lua"] = []byte(patched)
	if err := os.WriteFile(filepath.Join(dir, "Types.lua"), files["Types.lua"], 0o644); err != nil {
		t.Fatal(err)
	}

	probe := `local hexline = require("hexline")
local serialize = require("serialize")
local Types = require("Types")
local Enums = require("Enums")
local inst = Types.NewShipCreate()
inst.HasFlags = true
inst.Flags = Enums.ShipFlagsBoosting | Enums.ShipFlagsAiming
local stream = serialize.WriteStream.new(2048)
assert(Types.WriteShipCreate(stream, inst))
stream:flush()
print(hexline(stream:data()))
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", nil)
	lines := luarun.HexLines(t, out)
	golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 1 && bytes.Equal(lines[0], golden) {
		t.Errorf("negative control: patched write produced the same bytes as the golden")
	}
}

const wireProbe = `
local serialize = require("serialize")
local Types = require("Types")
local Enums = require("Enums")
local Wire = require("Wire")
local Degenerate = require("Degenerate")
local Clauses = require("Clauses")
local Joins = require("Joins")

local function tohex(s)
  return (s:gsub(".", function(c) return string.format("%02x", c:byte()) end))
end
local function hex(fn)
  local stream = serialize.WriteStream.new(8192)
  assert(fn(stream))
  stream:flush()
  return tohex(stream:data())
end
local function emit(fn) print(hex(fn)) end

do -- shipcreate_flags
  local v = Types.NewShipCreate()
  v.ShipType = Enums.ShipType.Bomber
  v.Position = { X = 1000, Y = -2000, Z = 3000 }
  v.HasFlags = true
  v.Flags = Enums.ShipFlagsBoosting | Enums.ShipFlagsAiming
  v.Team = Enums.Team.Blue
  v.Health = 750
  v.Thrust = 55
  emit(function(s) return Types.WriteShipCreate(s, v) end)
end

do -- rigidbody_moving and rigidbody_at_rest
  local v = Types.NewRigidBody()
  v.Position = { X = 1.5, Y = -2.5, Z = 3.25 }
  v.Orientation = { X = 0.1, Y = 0.2, Z = 0.3, W = 0.9 }
  v.AtRest = false
  v.LinearVelocity = { X = 10.0, Y = 20.0, Z = -3.0 }
  v.AngularVelocity = { X = 0.25, Y = 0.5, Z = 0.75 }
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
  v.ProbeId = 0x1122334455667788
  emit(function(s) return Wire.WriteProbeHeader(s, v) end)
end

do -- compressed_probe
  local v = Wire.NewCompressedProbe()
  v.Boundary = 0.005
  v.Offset = -4.8585
  emit(function(s) return Wire.WriteCompressedProbe(s, v) end)
end

do -- probebits
  local v = Wire.NewProbeBits()
  v.Small = 0x1FF
  v.Boundary = 0x1FFFFFFFF
  v.Wide = 0xFEDCBA9876543210
  v.Sensor = 4294967295
  v.Nonce = 0xffffffffffffffff
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
  v.FloatValue = 3.1415926
  v.CompressedFloatValue = 2.5
  v.DoubleValue = 1.0 / 3.0
  v.Int8Value = -128
  v.Int16Value = -32768
  v.Uint8Value = 255
  v.Uint16Value = 65535
  v.Uint32Value = 4294967295
  v.Uint64Value = 0xffffffffffffffff
  v.Int64Full = math.mininteger
  v.Int64Range = -999999999999
  for i = 1, 17 do v.FixedBytes[i] = (i - 1) * 3 end
  v.Text = "the quick brown fox"
  emit(function(s) return Wire.WriteTestData(s, v) end)
end

do -- inputpacket
  local v = Types.NewInputPacket()
  v.SynchronizeSequence = 7
  v.CurrentFrame = 123456789
  v.StartFrame = 123456780
  v.InputsCount = 2
  v.Inputs[1].Throttle = 0.5
  v.Inputs[1].Fire = true
  v.Inputs[2].StickX = -0.25
  v.Inputs[2].Boost = true
  emit(function(s) return Types.WriteInputPacket(s, v) end)
end

do -- probearray
  local v = Wire.NewProbeArray()
  v.Samples[1].Orientation = 90.0
  v.Samples[1].RawDelta = -5
  v.Samples[1].BigDelta = -1234567890123
  v.Samples[1].Weapon = Wire.Weapon.Laser
  v.Samples[1].HasTarget = true
  v.Samples[1].TargetId = 777
  v.Samples[1].SamplesCount = 1
  v.Samples[1].Samples[1] = 42
  v.Samples[2].Active = false
  v.Samples[2].Orientation = -45.5
  v.Samples[2].RawDelta = 7
  v.Samples[2].BigDelta = 99
  v.Samples[2].IdleTicks = 1000
  v.Samples[2].SamplesCount = 2
  v.Samples[2].Samples[1] = 7
  v.Samples[2].Samples[2] = 8
  v.Config.Retries = 3
  v.Config.Preferred = Wire.Weapon.Missile
  emit(function(s) return Wire.WriteProbeArray(s, v) end)
end

do -- degenerate
  local v1 = Degenerate.NewVec2(); v1.X = 1.5; v1.Y = -2.25
  local v2 = Degenerate.NewSpanF64(); v2.Values = { 3.5, -4.75 }
  local v3 = Degenerate.NewSpanU64(); v3.Values = { 0xDEADBEEFCAFEBABE, 1 }
  local v4 = Degenerate.NewSpanI64(); v4.Values = { -1234567890123, 42 }
  local v5 = Degenerate.NewSpanOne(); v5.Values = { 0x0123456789ABCDEF }
  local v6 = Degenerate.NewSpanChunk(); v6.Values = { 0x1111, 0x2222, 0x3333, 0x4444 }
  local v7 = Degenerate.NewSpanTail(); v7.Values = { 6.125, -7.0 }; v7.Tail = 0xFEEDFACE
  local v8 = Degenerate.NewSpanTwice(); v8.A = { 8.5, 9.5 }; v8.B = { -10.5, -11.5 }
  local v9 = Degenerate.NewTrio(); v9.A = 0xABCDE; v9.B = 0x12345; v9.C = 0xFFFFF
  local v10 = Degenerate.NewTrioSole(); v10.Inner.A = 1; v10.Inner.B = 2; v10.Inner.C = 3
  local v11 = Degenerate.NewTrioFirst(); v11.Inner.A = 0xAAAAA; v11.Inner.B = 0x55555; v11.Inner.C = 0xF0F0F; v11.Trailer = 0xBEEF
  local v12 = Degenerate.NewTrioStraddle()
  v12.Pad0 = 0x0011223344556677; v12.Pad1 = 0x8899AABBCCDDEEFF
  v12.Pad2 = 0xffffffffffffffff; v12.Pad3 = 0; v12.Pad4 = 0x123456789ABCDEF0; v12.Pad5 = 0xABCDEF
  v12.Inner.A = 0x11111; v12.Inner.B = 0x22222; v12.Inner.C = 0x33333
  local parts = {}
  local function one(fn) parts[#parts + 1] = hex(fn) end
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
  local function one(fn) parts[#parts + 1] = hex(fn) end
  for _, c in ipairs({ 0, 1, 3, 4, 5, 7, 12 }) do
    local v = Clauses.NewW13(); v.ItemsCount = c
    for i = 1, c do v.Items[i] = 8191 - (i - 1) * 733 end
    one(function(s) return Clauses.WriteW13(s, v) end)
  end
  for _, c in ipairs({ 0, 1, 2, 3, 4, 9 }) do
    local v = Clauses.NewW17(); v.ItemsCount = c
    for i = 1, c do v.Items[i] = 131071 - (i - 1) * 11117 end
    one(function(s) return Clauses.WriteW17(s, v) end)
  end
  for _, c in ipairs({ 0, 1, 2, 3, 6 }) do
    local v = Clauses.NewW26(); v.ItemsCount = c
    for i = 1, c do v.Items[i] = 67108863 - (i - 1) * 5555555 end
    one(function(s) return Clauses.WriteW26(s, v) end)
  end
  for _, c in ipairs({ 0, 1, 3, 4, 5, 20 }) do
    local v = Clauses.NewW1(); v.ItemsCount = c
    for i = 1, c do v.Items[i] = (i - 1) % 2 end
    one(function(s) return Clauses.WriteW1(s, v) end)
  end
  for c = 0, 3 do
    local v = Clauses.NewW52(); v.ItemsCount = c
    for i = 1, c do v.Items[i] = 4503599627370495 - (i - 1) * 123456789 end
    one(function(s) return Clauses.WriteW52(s, v) end)
  end
  for c = 0, 3 do
    local v = Clauses.NewW50(); v.ItemsCount = c
    for i = 1, c do v.Items[i] = 1125899906842623 - (i - 1) * 987654321 end
    one(function(s) return Clauses.WriteW50(s, v) end)
  end
  do
    local v = Clauses.NewF13()
    for i = 1, 7 do v.Items[i] = 8191 - (i - 1) * 911 end
    one(function(s) return Clauses.WriteF13(s, v) end)
  end
  for _, c in ipairs({ 0, 1, 3, 4, 5, 10 }) do
    local v = Clauses.NewArrTri3(); v.ItemsCount = c
    for i = 1, c do v.Items[i].A = (i - 1) % 2; v.Items[i].B = (i - 1) % 4 end
    one(function(s) return Clauses.WriteArrTri3(s, v) end)
  end
  do
    local v = Clauses.NewArrEleven()
    for i = 1, 9 do v.Items[i].A = (i - 1) % 8; v.Items[i].B = 255 - (i - 1) * 17 end
    one(function(s) return Clauses.WriteArrEleven(s, v) end)
  end
  for i = 1, 3 do
    local v = Clauses.NewHoldsEmptyUnion(); v.Lead = 21; v.Tail = 99
    if i == 2 then v.U.Type = Clauses.EmptyUnionType.A end
    if i == 3 then v.U.Type = Clauses.EmptyUnionType.B end
    one(function(s) return Clauses.WriteHoldsEmptyUnion(s, v) end)
  end
  do
    local v = Clauses.NewStrs(); v.Lead = 21; v.Tail = 5
    one(function(s) return Clauses.WriteStrs(s, v) end)
    local full = Clauses.NewStrs(); full.Lead = 21; full.Tail = 5; full.S = "abcdefgh"
    local b = {}
    for i = 1, 8 do b[i] = string.char(0xF0 + i - 1) end
    full.B = table.concat(b)
    one(function(s) return Clauses.WriteStrs(s, full) end)
    local part = Clauses.NewStrs(); part.Lead = 21; part.Tail = 5; part.S = "xyz"
    part.B = string.char(1, 2, 3)
    one(function(s) return Clauses.WriteStrs(s, part) end)
  end
  for c = 0, 4 do
    local v = Clauses.NewArrNested(); v.Lead = 21; v.Tail = 5; v.ItemsCount = c
    for i = 1, c do v.Items[i].A = (i - 1) % 8; v.Items[i].B = 200 - (i - 1) * 7 end
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
  local function one(fn) parts[#parts + 1] = hex(fn) end
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
    for i = 0, 1 do
      local v = Joins.NewArmsNested()
      v.Lead = 5; v.Outer = o ~= 0; v.Inner = i ~= 0
      v.X = 500000000; v.Y = 17; v.Z = 4000; v.Tail = 33
      one(function(s) return Joins.WriteArmsNested(s, v) end)
    end
  end
  for f = 0, 1 do
    for c = 0, 3 do
      local v = Joins.NewArmArray(); v.Lead = 21; v.Flag = f ~= 0; v.ItemsCount = c; v.B = 300; v.Tail = 99
      for i = 1, c do v.Items[i] = 8191 - (i - 1) * 777 end
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
    w.U.Type = Joins.UnevenType.Wide; w.U.Wide.W = 123456789012
    one(function(s) return Joins.WriteHoldsUneven(s, w) end)
  end
  for c = 0, 3 do
    local v = Joins.NewArrUneven(); v.Lead = 21; v.Tail = 5; v.ItemsCount = c
    for i = 1, c do
      if (i - 1) % 2 == 0 then
        v.Items[i].Type = Joins.UnevenType.Narrow
        v.Items[i].Narrow.N = (i - 1) % 8
      else
        v.Items[i].Type = Joins.UnevenType.Wide
        v.Items[i].Wide.W = 99887766554 + (i - 1)
      end
    end
    one(function(s) return Joins.WriteArrUneven(s, v) end)
  end
  for c = 0, 3 do
    for sl = 0, 4, 4 do
      local v = Joins.NewRegainAfterAlign()
      v.Lead = 21; v.ItemsCount = c
      v.S = sl ~= 0 and "wxyz" or ""
      v.P = 0xDEADBEEF; v.Q = (1 << 29) - 7; v.R = (1 << 19) - 3; v.Tail = 9
      for i = 1, c do v.Items[i] = 8191 - (i - 1) * 999 end
      one(function(s) return Joins.WriteRegainAfterAlign(s, v) end)
    end
  end
  print(table.concat(parts))
end
`

// TestLuaWStringComposesTheClassicBytes holds wstring(N) to SPEC §4.12 now that
// serialize format 2.0 removed wstring from the runtime and the generated code
// composes it (the length over [0, N] with no alignment, then one 32-bit group
// per UTF-16 code unit). The expected bytes are format 1.1's
// serialize_wstring over a buffer of N + 1, the normative twin, as serialize.lua
// v1.1 emitted them for the same values; then the Lua reader takes them back,
// and the writer refuses a zero unit, a unit above 0xFFFF and an over-long
// value.
func TestLuaWStringComposesTheClassicBytes(t *testing.T) {
	t.Parallel()
	src := t.TempDir()
	schema := "package wide\n\ntype Wide\n{\n    a bool\n    w wstring(4)\n    b bool\n    e wstring(4)\n}\n"
	if err := os.WriteFile(filepath.Join(src, "Wide.schema"), []byte(schema), 0o644); err != nil {
		t.Fatal(err)
	}
	files, err := Generate(luarun.LoadUnit(t, src))
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteHexProbe(t, dir)
	probe := `local hexline = require("hexline")
local serialize = require("serialize")
local W = require("Wide")

local function write(v)
  local s = serialize.WriteStream.new(64)
  local ok = W.WriteWide(s, v)
  s:flush()
  return ok, s:data()
end

local v = W.NewWide()
v.A, v.W, v.B, v.E = true, { 0x41, 0xD83D, 0xDE00 }, true, {}
local ok, wire = write(v)
assert(ok, "write refused a well-formed wstring")
hexline(wire)

local got = W.NewWide()
assert(W.ReadWide(serialize.ReadStream.new(wire), got), "read refused the classic bytes")
print(#got.W, got.W[1], got.W[2], got.W[3], #got.E, tostring(got.B))

for _, bad in ipairs({ { 0x41, 0 }, { 0x10000 }, { 1, 2, 3, 4, 5 } }) do
  local b = W.NewWide()
  b.W = bad
  print(tostring((write(b))))
end
`
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(probe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := strings.Split(strings.TrimRight(string(luarun.Run(t, dir, "main.lua", nil)), "\n"), "\n")
	want := []string{
		"17040000d0830d0000e00d0010",
		"3\t65\t55357\t56832\t0\ttrue",
		"false", "false", "false",
	}
	if strings.Join(out, "\n") != strings.Join(want, "\n") {
		t.Errorf("wstring probe printed\n%s\nwant\n%s", strings.Join(out, "\n"), strings.Join(want, "\n"))
	}
}
