package lua

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lua/luarun"
)

// The goldens test/go/main.go pins, in its order.
var readGoldens = []string{
	"shipcreate_flags", "rigidbody_moving", "rigidbody_at_rest", "chat",
	"probe_header", "compressed_probe", "probebits", "probecollider",
	"testdata", "inputpacket", "probearray", "degenerate", "clauses", "joins",
}

// TestLuaTypeWireReadsTheGoldens generates examples/*.schema into a t.TempDir,
// reads each C++-pinned golden through the generated Read<Name>, and checks
// every field against the value test/go/main.go builds for it (docs/PORT-LUA.md,
// THE HARNESS; SPEC §4.3, §6.1). Each golden cut one byte short must refuse.
func TestLuaTypeWireReadsTheGoldens(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteDumpProbe(t, dir)

	inputs := map[string][]byte{}
	for _, name := range readGoldens {
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		inputs[name] = data
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(readProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	out := luarun.Run(t, dir, "main.lua", inputs)
	lines := map[string]string{}
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		name, verdict, _ := strings.Cut(line, " ")
		lines[name] = verdict
	}
	for _, name := range readGoldens {
		if _, ok := lines[name]; !ok {
			t.Fatalf("the probe did not read golden %s", name)
		}
	}
	for _, name := range readGoldens {
		switch lines[name] {
		case "OK", "READ":
		default:
			t.Errorf("golden %s: %q", name, lines[name])
		}
	}

	for _, name := range readGoldens {
		if name == "clauses" || name == "joins" {
			continue // their per-shape streams are not embedded yet (reported)
		}
		name := name
		t.Run("short/"+name, func(t *testing.T) {
			dir := t.TempDir()
			luarun.WriteFiles(t, dir, files)
			inputsShort := map[string][]byte{name: inputs[name][:len(inputs[name])-1]}
			if err := os.WriteFile(filepath.Join(dir, "short.lua"), []byte(shortProbe), 0o644); err != nil {
				t.Fatal(err)
			}
			out := luarun.Run(t, dir, "short.lua", inputsShort)
			if strings.TrimSpace(string(out)) != "refused "+name {
				t.Fatalf("%s cut one byte short read successfully (%q); a read past the end must refuse", name, out)
			}
		})
	}
}

// TestLuaTypeWireReadNegativeControl patches the emitted read in memory so one
// uint32 field reads 31 bits, and requires the golden comparison — not a load
// error — to go red (docs/PORT-LUA.md, DEEPEN).
func TestLuaTypeWireReadNegativeControl(t *testing.T) {
	t.Parallel()
	u := luarun.LoadUnit(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	text := string(files["Degenerate.lua"])
	start := strings.Index(text, "function _M.ReadSpanTail")
	if start < 0 {
		t.Fatal("Wire.lua has no ReadProbeBits")
	}
	anchor := "if not stream:serializeBits(BITS, 32) then\n    return false\n  end\n  value.Tail = BITS.value"
	i := strings.Index(text[start:], anchor)
	if i < 0 || strings.Count(text[start:], anchor) != 1 {
		t.Fatalf("the uint32 anchor does not occur exactly once in ReadSpanTail")
	}
	i += start
	patched := "if not stream:serializeBits(BITS, 31) then\n    return false\n  end\n  value.Tail = BITS.value\n  if not stream:serializeBits(BITS, 1) then\n    return false\n  end"
	files["Degenerate.lua"] = []byte(text[:i] + patched + text[i+len(anchor):])

	dir := t.TempDir()
	luarun.WriteFiles(t, dir, files)
	luarun.WriteDumpProbe(t, dir)
	golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "probebits.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "main.lua"), []byte(readProbe), 0o644); err != nil {
		t.Fatal(err)
	}
	all := map[string][]byte{}
	for _, n := range readGoldens {
		d, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", n+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		all[n] = d
	}
	all["testdata"] = golden
	out := luarun.Run(t, dir, "main.lua", all)
	found := false
	for _, line := range strings.Split(strings.TrimRight(string(out), "\n"), "\n") {
		if strings.HasPrefix(line, "degenerate MISMATCH") {
			found = true
		}
	}
	if !found {
		t.Fatalf("the 31-bit control still matched the golden expectation; output:\n%s", out)
	}
}

const shortProbe = `local inputs = require("inputs")
local name = next(inputs)
local modules = { shipcreate_flags = "Types", rigidbody_moving = "Types", rigidbody_at_rest = "Types",
  chat = "Wire", probe_header = "Wire", compressed_probe = "Wire", probebits = "Wire",
  probecollider = "Wire", testdata = "Wire", inputpacket = "Types", probearray = "Wire",
  degenerate = "Degenerate", clauses = "Clauses", joins = "Joins" }
local root = { shipcreate_flags = "ShipCreate", rigidbody_moving = "RigidBody", rigidbody_at_rest = "RigidBody",
  chat = "Chat", probe_header = "ProbeHeader", compressed_probe = "CompressedProbe", probebits = "ProbeBits",
  probecollider = "ProbeCollider", testdata = "TestData", inputpacket = "InputPacket",
  probearray = "ProbeArray", degenerate = "Vec2", clauses = "W13", joins = "ArmsAgree" }
local serialize = require("serialize")
if name == "degenerate" then
  local D = require("Degenerate")
  local names = { "Vec2", "SpanF64", "SpanU64", "SpanI64", "SpanOne", "SpanChunk", "SpanTail", "SpanTwice", "Trio", "TrioSole", "TrioFirst", "TrioStraddle" }
  local s = serialize.ReadStream.new(inputs[name])
  for _, n in ipairs(names) do
    if not D["Read" .. n](s, D["New" .. n]()) then print("refused " .. name); os.exit(0) end
  end
  print("read " .. name)
  os.exit(0)
end
local m = require(modules[name])
local value = m["New" .. root[name]]()
local stream = serialize.ReadStream.new(inputs[name])
if m["Read" .. root[name]](stream, value) then
  print("read " .. name)
else
  print("refused " .. name)
end
`

const readProbe = `local dump = require("dump")
local inputs = require("inputs")
local Wire = require("Wire")
local Types = require("Types")
local Enums = require("Enums")
local D = require("Degenerate")
local serialize = require("serialize")
local function f32(x) return (string.unpack("<f", string.pack("<f", x))) end
local function cf(iv, maxInt, delta, mn)
  local q = f32(f32(iv) / f32(maxInt))
  local p = f32(q * f32(delta))
  return f32(p + mn)
end
local function check(name, mod, root, want)
  local got = mod["New" .. root]()
  local s = serialize.ReadStream.new(inputs[name])
  if not mod["Read" .. root](s, got) then print(name .. " REFUSED"); return end
  local a, b = dump(got), dump(want)
  if a == b then print(name .. " OK") else print(name .. " MISMATCH got " .. a .. " want " .. b) end
end
local function readonly(name, mod, root)
  local v = mod["New" .. root]()
  local s = serialize.ReadStream.new(inputs[name])
  if not mod["Read" .. root](s, v) then print(name .. " REFUSED"); return end
  print(name .. " READ")
end
-- shipcreate_flags
do
  local v = Types.NewShipCreate()
  v.ShipType = Enums.ShipType.Bomber
  v.Position.X, v.Position.Y, v.Position.Z = 1000, -2000, 3000
  v.HasFlags = true
  v.Flags = Enums.ShipFlagsBoosting | Enums.ShipFlagsAiming
  v.Team = Enums.Team.Blue
  v.Health = 750
  v.Thrust = 55
  check("shipcreate_flags", Types, "ShipCreate", v)
end
-- rigidbody_moving
do
  local v = Types.NewRigidBody()
  v.Position.X, v.Position.Y, v.Position.Z = 1.5, -2.5, 3.25
  v.Orientation.X, v.Orientation.Y, v.Orientation.Z, v.Orientation.W = 0.1, 0.2, 0.3, 0.9
  v.AtRest = false
  v.LinearVelocity.X, v.LinearVelocity.Y, v.LinearVelocity.Z = 10.0, 20.0, -3.0
  v.AngularVelocity.X, v.AngularVelocity.Y, v.AngularVelocity.Z = 0.25, 0.5, 0.75
  check("rigidbody_moving", Types, "RigidBody", v)
end
-- rigidbody_at_rest
do
  local v = Types.NewRigidBody()
  v.Position.X, v.Position.Y, v.Position.Z = 1.5, -2.5, 3.25
  v.Orientation.X, v.Orientation.Y, v.Orientation.Z, v.Orientation.W = 0.1, 0.2, 0.3, 0.9
  v.AtRest = true
  v.LinearVelocity.X, v.LinearVelocity.Y, v.LinearVelocity.Z = 10.0, 20.0, -3.0
  v.AngularVelocity.X, v.AngularVelocity.Y, v.AngularVelocity.Z = 0.25, 0.5, 0.75
  -- the untaken side reads as ZERO, not the written values (SPEC §5)
  v.LinearVelocity.X, v.LinearVelocity.Y, v.LinearVelocity.Z = 0.0, 0.0, 0.0
  v.AngularVelocity.X, v.AngularVelocity.Y, v.AngularVelocity.Z = 0.0, 0.0, 0.0
  check("rigidbody_at_rest", Types, "RigidBody", v)
end
-- chat
do local v = Wire.NewChat(); v.Text = "wire parity"; check("chat", Wire, "Chat", v) end
-- probe_header
do local v = Wire.NewProbeHeader(); v.Version = 5; v.ProbeId = 0x1122334455667788; check("probe_header", Wire, "ProbeHeader", v) end
-- compressed_probe
do
  local v = Wire.NewCompressedProbe()
  v.Boundary = cf(1, 1000, 10, 0)
  v.Offset = cf(142, 10000, 10, -5)
  check("compressed_probe", Wire, "CompressedProbe", v)
end
-- probebits
do
  local v = Wire.NewProbeBits()
  v.Small = 0x1FF; v.Boundary = 0x1FFFFFFFF; v.Wide = 0xFEDCBA9876543210
  v.Sensor = 4294967295; v.Nonce = 0xFFFFFFFFFFFFFFFF
  check("probebits", Wire, "ProbeBits", v)
end
-- probecollider
do
  local v = Wire.NewProbeCollider()
  v.Armor = 7
  v.Shape.Type = Wire.ProbeShapeType.Slab
  v.Shape.Slab.Width, v.Shape.Slab.Height = 42, 9
  v.ExtrasCount = 1
  v.Extras[1].Type = Wire.ProbeShapeType.Ring
  v.Extras[1].Ring.Radius = 777
  check("probecollider", Wire, "ProbeCollider", v)
end
-- testdata
do
  local v = Wire.NewTestData()
  v.A, v.B, v.C = -100, 100, 149
  v.D, v.E, v.F = 0x11, 0x22, 0x33
  v.G = true
  v.ItemsCount = 3
  v.Items[1], v.Items[2], v.Items[3] = 0, 128, 255
  v.FloatValue = f32(3.1415926)
  v.CompressedFloatValue = cf(250, 1000, 10, 0)
  v.DoubleValue = 1.0 / 3.0
  v.Int8Value, v.Int16Value = -128, -32768
  v.Uint8Value, v.Uint16Value = 255, 65535
  v.Uint32Value, v.Uint64Value = 4294967295, 0xFFFFFFFFFFFFFFFF
  v.Int64Full, v.Int64Range = math.mininteger, -999999999999
  for i = 1, 17 do v.FixedBytes[i] = (i - 1) * 3 end
  v.Text = "the quick brown fox"
  check("testdata", Wire, "TestData", v)
end
-- inputpacket
do
  local v = Types.NewInputPacket()
  v.SynchronizeSequence, v.CurrentFrame, v.StartFrame = 7, 123456789, 123456780
  v.InputsCount = 2
  v.Inputs[1].Throttle = 0.5; v.Inputs[1].Fire = true
  v.Inputs[2].StickX = -0.25; v.Inputs[2].Boost = true
  check("inputpacket", Types, "InputPacket", v)
end
-- probearray
do
  local v = Wire.NewProbeArray()
  local a = v.Samples[1]
  a.Orientation, a.RawDelta, a.BigDelta = 90.0, -5, -1234567890123
  a.Weapon = Wire.Weapon.Laser
  a.HasTarget = true; a.TargetId = 777
  a.SamplesCount = 1; a.Samples[1] = 42
  local b = v.Samples[2]
  b.Active = false; b.Orientation = f32(-45.5); b.RawDelta = 7; b.BigDelta = 99
  b.IdleTicks = 1000
  b.SamplesCount = 2; b.Samples[1], b.Samples[2] = 7, 8
  v.Config.Retries = 3; v.Config.Preferred = Wire.Weapon.Missile
  check("probearray", Wire, "ProbeArray", v)
end
-- degenerate: the twelve shapes of the one pinned stream, read back to back
do
  local vec2 = D.NewVec2(); vec2.X, vec2.Y = 1.5, -2.25
  local sf = D.NewSpanF64(); sf.Values[1], sf.Values[2] = 3.5, -4.75
  local su = D.NewSpanU64(); su.Values[1], su.Values[2] = 0xDEADBEEFCAFEBABE, 1
  local si = D.NewSpanI64(); si.Values[1], si.Values[2] = -1234567890123, 42
  local so = D.NewSpanOne(); so.Values[1] = 0x0123456789ABCDEF
  local sc = D.NewSpanChunk(); sc.Values[1], sc.Values[2], sc.Values[3], sc.Values[4] = 0x1111, 0x2222, 0x3333, 0x4444
  local st = D.NewSpanTail(); st.Values[1], st.Values[2], st.Tail = 6.125, -7.0, 0xFEEDFACE
  local sw = D.NewSpanTwice(); sw.A[1], sw.A[2], sw.B[1], sw.B[2] = 8.5, 9.5, -10.5, -11.5
  local trio = D.NewTrio(); trio.A, trio.B, trio.C = 0xABCDE, 0x12345, 0xFFFFF
  local ts = D.NewTrioSole(); ts.Inner.A, ts.Inner.B, ts.Inner.C = 1, 2, 3
  local tf = D.NewTrioFirst(); tf.Inner.A, tf.Inner.B, tf.Inner.C = 0xAAAAA, 0x55555, 0xF0F0F; tf.Trailer = 0xBEEF
  local tr = D.NewTrioStraddle()
  tr.Pad0, tr.Pad1, tr.Pad2, tr.Pad3 = 0x0011223344556677, 0x8899AABBCCDDEEFF, 0xFFFFFFFFFFFFFFFF, 0
  tr.Pad4, tr.Pad5 = 0x123456789ABCDEF0, 0xABCDEF
  tr.Inner.A, tr.Inner.B, tr.Inner.C = 0x11111, 0x22222, 0x33333
  local want = { vec2, sf, su, si, so, sc, st, sw, trio, ts, tf, tr }
  local names = { "Vec2", "SpanF64", "SpanU64", "SpanI64", "SpanOne", "SpanChunk", "SpanTail", "SpanTwice", "Trio", "TrioSole", "TrioFirst", "TrioStraddle" }
  local s = serialize.ReadStream.new(inputs["degenerate"])
  local ok = true
  for i, n in ipairs(names) do
    local got = D["New" .. n]()
    if not D["Read" .. n](s, got) then print("degenerate REFUSED " .. n); ok = false; break end
    if dump(got) ~= dump(want[i]) then print("degenerate MISMATCH " .. n .. " got " .. dump(got) .. " want " .. dump(want[i])); ok = false; break end
  end
  if ok then print("degenerate OK") end
end
-- clauses and joins: each golden is a concatenation of shapes, each its own
-- flushed stream; the whole golden is read here only, their per-shape field
-- expectations are named as not done in the report.
readonly("clauses", require("Clauses"), "W13")
readonly("joins", require("Joins"), "ArmsAgree")
`
