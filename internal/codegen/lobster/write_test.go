package lobster

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

var wireGoldenNames = []string{
	"shipcreate_flags", "rigidbody_moving", "rigidbody_at_rest", "chat",
	"probe_header", "compressed_probe", "probebits", "probecollider",
	"testdata", "inputpacket", "probearray", "degenerate", "clauses", "joins",
}

// TestLobsterTypeWireWritesTheGoldens generates examples/*.schema into a
// t.TempDir, builds each instance the Go cross-language test builds
// (test/go/main.go) before a goldenWire call, writes it in Lobster, and
// byte-compares against testdata/wire/<name>.bin (docs/PORT-LOBSTER.md; the
// C++ reference's bytes are the judge, never the Lobster leg's own output).
func TestLobsterTypeWireWritesTheGoldens(t *testing.T) {
	t.Parallel()
	u := unitFromDir(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", wireProbe)
	out := lobsterrun.Run(t, dir, "main.lobster", nil)
	lines := lobsterrun.HexLines(t, out)
	if len(lines) != len(wireGoldenNames) {
		t.Fatalf("probe produced %d wire lines, want %d:\n%s", len(lines), len(wireGoldenNames), out)
	}
	for i, name := range wireGoldenNames {
		t.Logf("golden %s", name)
		golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatal(err)
		}
		if !bytes.Equal(lines[i], golden) {
			t.Errorf("wire golden %s: lobster bytes %x != C++-pinned %x", name, lines[i], golden)
		}
	}
}

// TestLobsterTypeWireWriteNegativeControl patches the emitted write in memory
// (one anchor, matched exactly once) so one bool writes two bits, and
// requires the byte comparison to go red.
func TestLobsterTypeWireWriteNegativeControl(t *testing.T) {
	t.Parallel()
	u := unitFromDir(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	types := files["Types.lobster"]
	anchor := "s.serialize_bool(v.HasFlags)"
	if strings.Count(string(types), anchor) != 1 {
		t.Fatalf("negative-control anchor %q matched %d times in emitted Types.lobster", anchor, strings.Count(string(types), anchor))
	}
	patched := strings.Replace(string(types), anchor,
		"s.serialize_bits((if v.HasFlags: 1 else: 0), 2)", 1)
	files["Types.lobster"] = []byte(patched)
	dir := t.TempDir()
	writeAll(t, dir, files)
	writeFile(t, dir, "main.lobster", `import .Types
import .serialize_stream
`+lobsterrun.HexProbe+`
let v = example.NewShipCreate()
v.HasFlags = true
v.Flags = example.ShipFlagsBoosting | example.ShipFlagsAiming
let w = serialize_write_stream(ensure_size("", 2048, 0))
assert example.WriteShipCreate(w, v)
w.flush()
hex(substring(w.buffer, 0, w.bytes_processed()))
`)
	out := lobsterrun.Run(t, dir, "main.lobster", nil)
	lines := lobsterrun.HexLines(t, out)
	golden, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", "shipcreate_flags.bin"))
	if err != nil {
		t.Fatal(err)
	}
	if len(lines) == 1 && bytes.Equal(lines[0], golden) {
		t.Errorf("negative control: patched write produced the same bytes as the golden")
	}
}

const wireProbe = `import .Types
import .Wire
import .Degenerate
import .Clauses
import .Joins
import .schema_runtime
import .serialize_stream
` + lobsterrun.HexProbe + `
def done(s:serialize_write_stream) -> string:
    s.flush()
    return substring(s.buffer, 0, s.bytes_processed())

def newstream() -> serialize_write_stream:
    return serialize_write_stream(ensure_size("", 8192, 0))

def put(v:string, s:string, n:int) -> string:
    return schema_rt.put(v, s)

// ---- shipcreate_flags
let ship = example.NewShipCreate()
ship.ShipType = example.ShipTypeBomber
ship.Position.X = 1000
ship.Position.Y = -2000
ship.Position.Z = 3000
ship.HasFlags = true
ship.Flags = example.ShipFlagsBoosting | example.ShipFlagsAiming
ship.Team = example.TeamBlue
ship.Health = 750
ship.Thrust = 55
let ws1 = newstream()
assert example.WriteShipCreate(ws1, ship)
hex(done(ws1))

// ---- rigidbody_moving and rigidbody_at_rest
let body = example.NewRigidBody()
body.Position.X = 1.5
body.Position.Y = -2.5
body.Position.Z = 3.25
body.Orientation.X = 0.1
body.Orientation.Y = 0.2
body.Orientation.Z = 0.3
body.Orientation.W = 0.9
body.AtRest = false
body.LinearVelocity.X = 10.0
body.LinearVelocity.Y = 20.0
body.LinearVelocity.Z = -3.0
body.AngularVelocity.X = 0.25
body.AngularVelocity.Y = 0.5
body.AngularVelocity.Z = 0.75
let ws2 = newstream()
assert example.WriteRigidBody(ws2, body)
hex(done(ws2))
body.AtRest = true
let ws3 = newstream()
assert example.WriteRigidBody(ws3, body)
hex(done(ws3))

// ---- chat
let chat = example.NewChat()
chat.Text = put(chat.Text, "wire parity", 11)
chat.TextLength = 11
let ws4 = newstream()
assert example.WriteChat(ws4, chat)
hex(done(ws4))

// ---- probe_header
let header = example.NewProbeHeader()
header.Version = 5
header.ProbeId = 0x1122334455667788
let ws5 = newstream()
assert example.WriteProbeHeader(ws5, header)
hex(done(ws5))

// ---- compressed_probe
let compressed = example.NewCompressedProbe()
compressed.Boundary = 0.005
compressed.Offset = -4.8585
let ws6 = newstream()
assert example.WriteCompressedProbe(ws6, compressed)
hex(done(ws6))

// ---- probebits
let bits = example.NewProbeBits()
bits.Small = 0x1FF
bits.Boundary = 0x1FFFFFFFF
bits.Wide = 0xFEDCBA9876543210
bits.Sensor = 4294967295
bits.Nonce = 0xffffffffffffffff
let ws7 = newstream()
assert example.WriteProbeBits(ws7, bits)
hex(done(ws7))

// ---- probecollider
let collider = example.NewProbeCollider()
collider.Armor = 7
collider.Shape.Type = example.ProbeShapeTypeSlab
collider.Shape.Slab.Width = 42
collider.Shape.Slab.Height = 9
collider.ExtrasCount = 1
collider.Extras[0].Type = example.ProbeShapeTypeRing
collider.Extras[0].Ring.Radius = 777
let ws8 = newstream()
assert example.WriteProbeCollider(ws8, collider)
hex(done(ws8))

// ---- testdata
let td = example.NewTestData()
td.A = -100
td.B = 100
td.C = 149
td.D = 0x11
td.E = 0x22
td.F = 0x33
td.G = true
td.ItemsCount = 3
td.Items[0] = 0
td.Items[1] = 128
td.Items[2] = 255
td.FloatValue = 3.1415926
td.CompressedFloatValue = 2.5
td.DoubleValue = 1.0 / 3.0
td.Int8Value = -128
td.Int16Value = -32768
td.Uint8Value = 255
td.Uint16Value = 65535
td.Uint32Value = 4294967295
td.Uint64Value = 0xffffffffffffffff
td.Int64Full = 0x8000000000000000
td.Int64Range = -999999999999
for(17) k:
    td.FixedBytes = write_int8_le(td.FixedBytes, k, k * 3)
td.Text = put(td.Text, "the quick brown fox", 19)
td.TextLength = 19
let ws9 = newstream()
assert example.WriteTestData(ws9, td)
hex(done(ws9))

// ---- inputpacket
let packet = example.NewInputPacket()
packet.SynchronizeSequence = 7
packet.CurrentFrame = 123456789
packet.StartFrame = 123456780
packet.InputsCount = 2
packet.Inputs[0].Throttle = 0.5
packet.Inputs[0].Fire = true
packet.Inputs[1].StickX = -0.25
packet.Inputs[1].Boost = true
let ws10 = newstream()
assert example.WriteInputPacket(ws10, packet)
hex(done(ws10))

// ---- probearray
let array = example.NewProbeArray()
array.Samples[0].Orientation = 90.0
array.Samples[0].RawDelta = -5
array.Samples[0].BigDelta = -1234567890123
array.Samples[0].Weapon = example.WeaponLaser
array.Samples[0].HasTarget = true
array.Samples[0].TargetId = 777
array.Samples[0].SamplesCount = 1
array.Samples[0].Samples[0] = 42
array.Samples[1].Active = false
array.Samples[1].Orientation = -45.5
array.Samples[1].RawDelta = 7
array.Samples[1].BigDelta = 99
array.Samples[1].IdleTicks = 1000
array.Samples[1].SamplesCount = 2
array.Samples[1].Samples[0] = 7
array.Samples[1].Samples[1] = 8
array.Config.Retries = 3
array.Config.Preferred = example.WeaponMissile
let ws11 = newstream()
assert example.WriteProbeArray(ws11, array)
hex(done(ws11))

// ---- degenerate
var degenerate = ""
let v1 = example.NewVec2()
v1.X = 1.5
v1.Y = -2.25
let w1 = newstream()
assert example.WriteVec2(w1, v1)
degenerate += done(w1)
let v2 = example.NewSpanF64()
v2.Values[0] = 3.5
v2.Values[1] = -4.75
let w2 = newstream()
assert example.WriteSpanF64(w2, v2)
degenerate += done(w2)
let v3 = example.NewSpanU64()
v3.Values[0] = 0xDEADBEEFCAFEBABE
v3.Values[1] = 1
let w3 = newstream()
assert example.WriteSpanU64(w3, v3)
degenerate += done(w3)
let v4 = example.NewSpanI64()
v4.Values[0] = -1234567890123
v4.Values[1] = 42
let w4 = newstream()
assert example.WriteSpanI64(w4, v4)
degenerate += done(w4)
let v5 = example.NewSpanOne()
v5.Values[0] = 0x0123456789ABCDEF
let w5 = newstream()
assert example.WriteSpanOne(w5, v5)
degenerate += done(w5)
let v6 = example.NewSpanChunk()
v6.Values[0] = 0x1111
v6.Values[1] = 0x2222
v6.Values[2] = 0x3333
v6.Values[3] = 0x4444
let w6 = newstream()
assert example.WriteSpanChunk(w6, v6)
degenerate += done(w6)
let v7 = example.NewSpanTail()
v7.Values[0] = 6.125
v7.Values[1] = -7.0
v7.Tail = 0xFEEDFACE
let w7 = newstream()
assert example.WriteSpanTail(w7, v7)
degenerate += done(w7)
let v8 = example.NewSpanTwice()
v8.A[0] = 8.5
v8.A[1] = 9.5
v8.B[0] = -10.5
v8.B[1] = -11.5
let w8 = newstream()
assert example.WriteSpanTwice(w8, v8)
degenerate += done(w8)
let v9 = example.NewTrio()
v9.A = 0xABCDE
v9.B = 0x12345
v9.C = 0xFFFFF
let w9 = newstream()
assert example.WriteTrio(w9, v9)
degenerate += done(w9)
let v10 = example.NewTrioSole()
v10.Inner.A = 1
v10.Inner.B = 2
v10.Inner.C = 3
let w10 = newstream()
assert example.WriteTrioSole(w10, v10)
degenerate += done(w10)
let v11 = example.NewTrioFirst()
v11.Inner.A = 0xAAAAA
v11.Inner.B = 0x55555
v11.Inner.C = 0xF0F0F
v11.Trailer = 0xBEEF
let w11 = newstream()
assert example.WriteTrioFirst(w11, v11)
degenerate += done(w11)
let v12 = example.NewTrioStraddle()
v12.Pad0 = 0x0011223344556677
v12.Pad1 = 0x8899AABBCCDDEEFF
v12.Pad2 = 0xffffffffffffffff
v12.Pad3 = 0
v12.Pad4 = 0x123456789ABCDEF0
v12.Pad5 = 0xABCDEF
v12.Inner.A = 0x11111
v12.Inner.B = 0x22222
v12.Inner.C = 0x33333
let w12 = newstream()
assert example.WriteTrioStraddle(w12, v12)
degenerate += done(w12)
hex(degenerate)

// ---- clauses
var clauses = ""
for([0, 1, 3, 4, 5, 7, 12]) c:
    let v = example.NewW13()
    v.ItemsCount = c
    for(c) k:
        v.Items[k] = 8191 - k * 733
    let w = newstream()
    assert example.WriteW13(w, v)
    clauses += done(w)
for([0, 1, 2, 3, 4, 9]) c:
    let v = example.NewW17()
    v.ItemsCount = c
    for(c) k:
        v.Items[k] = 131071 - k * 11117
    let w = newstream()
    assert example.WriteW17(w, v)
    clauses += done(w)
for([0, 1, 2, 3, 6]) c:
    let v = example.NewW26()
    v.ItemsCount = c
    for(c) k:
        v.Items[k] = 67108863 - k * 5555555
    let w = newstream()
    assert example.WriteW26(w, v)
    clauses += done(w)
for([0, 1, 3, 4, 5, 20]) c:
    let v = example.NewW1()
    v.ItemsCount = c
    for(c) k:
        v.Items = write_int8_le(v.Items, k, k % 2)
    let w = newstream()
    assert example.WriteW1(w, v)
    clauses += done(w)
for([0, 1, 2, 3]) c:
    let v = example.NewW52()
    v.ItemsCount = c
    for(c) k:
        v.Items[k] = 4503599627370495 - k * 123456789
    let w = newstream()
    assert example.WriteW52(w, v)
    clauses += done(w)
for([0, 1, 2, 3]) c:
    let v = example.NewW50()
    v.ItemsCount = c
    for(c) k:
        v.Items[k] = 1125899906842623 - k * 987654321
    let w = newstream()
    assert example.WriteW50(w, v)
    clauses += done(w)
let f13 = example.NewF13()
for(7) k:
    f13.Items[k] = 8191 - k * 911
let wf13 = newstream()
assert example.WriteF13(wf13, f13)
clauses += done(wf13)
for([0, 1, 3, 4, 5, 10]) c:
    let v = example.NewArrTri3()
    v.ItemsCount = c
    for(c) k:
        v.Items[k].A = k % 2
        v.Items[k].B = k % 4
    let w = newstream()
    assert example.WriteArrTri3(w, v)
    clauses += done(w)
let eleven = example.NewArrEleven()
for(9) k:
    eleven.Items[k].A = k % 8
    eleven.Items[k].B = 255 - k * 17
let we = newstream()
assert example.WriteArrEleven(we, eleven)
clauses += done(we)
for([0, 1, 2]) k:
    let v = example.NewHoldsEmptyUnion()
    v.Lead = 21
    v.Tail = 99
    if k == 1:
        v.U.Type = example.EmptyUnionTypeA
    elif k == 2:
        v.U.Type = example.EmptyUnionTypeB
    let w = newstream()
    assert example.WriteHoldsEmptyUnion(w, v)
    clauses += done(w)
let strs = example.NewStrs()
strs.Lead = 21
strs.Tail = 5
let wstrs = newstream()
assert example.WriteStrs(wstrs, strs)
clauses += done(wstrs)
let full = example.NewStrs()
full.Lead = 21
full.Tail = 5
full.S = put(full.S, "abcdefgh", 8)
full.SLength = 8
full.B = put(full.B, "\xf0\xf1\xf2\xf3\xf4\xf5\xf6\xf7", 8)
full.BLength = 8
let wfull = newstream()
assert example.WriteStrs(wfull, full)
clauses += done(wfull)
let part = example.NewStrs()
part.Lead = 21
part.Tail = 5
part.S = put(part.S, "xyz", 3)
part.SLength = 3
part.B = put(part.B, "\x01\x02\x03", 3)
part.BLength = 3
let wpart = newstream()
assert example.WriteStrs(wpart, part)
clauses += done(wpart)
for([0, 1, 2, 3, 4]) c:
    let v = example.NewArrNested()
    v.Lead = 21
    v.Tail = 5
    v.ItemsCount = c
    for(c) k:
        v.Items[k].A = k % 8
        v.Items[k].B = 200 - k * 7
    let w = newstream()
    assert example.WriteArrNested(w, v)
    clauses += done(w)
let sole = example.NewSole()
sole.Only = 5555
let wsole = newstream()
assert example.WriteSole(wsole, sole)
clauses += done(wsole)
hex(clauses)

// ---- joins
var joins = ""
for([0, 1]) f:
    let flag = f != 0
    let v = example.NewArmsAgree()
    v.Lead = 21
    v.Flag = flag
    v.A = 1234
    v.B = 1500
    v.Tail = 99
    let w = newstream()
    assert example.WriteArmsAgree(w, v)
    joins += done(w)
    let d = example.NewArmsDisagree()
    d.Lead = 21
    d.Flag = flag
    d.A = 1234
    d.B = 5
    d.Tail = 99
    let wd = newstream()
    assert example.WriteArmsDisagree(wd, d)
    joins += done(wd)
    let e = example.NewArmEmpty()
    e.Lead = 21
    e.Flag = flag
    e.A = 456789
    e.Tail = 99
    let wempty = newstream()
    assert example.WriteArmEmpty(wempty, e)
    joins += done(wempty)
    let a = example.NewArmAlign()
    a.Lead = 21
    a.Flag = flag
    a.S = put(a.S, "abcd", 4)
    a.SLength = 4
    a.B = 1000
    a.Tail = 99
    let wa = newstream()
    assert example.WriteArmAlign(wa, a)
    joins += done(wa)
    let ae = example.NewArmAlign()
    ae.Lead = 21
    ae.Flag = flag
    ae.B = 1000
    ae.Tail = 99
    let wae = newstream()
    assert example.WriteArmAlign(wae, ae)
    joins += done(wae)
for([0, 1]) o:
    for([0, 1]) n:
        let v = example.NewArmsNested()
        v.Lead = 5
        v.Outer = o != 0
        v.Inner = n != 0
        v.X = 500000000
        v.Y = 17
        v.Z = 4000
        v.Tail = 33
        let w = newstream()
        assert example.WriteArmsNested(w, v)
        joins += done(w)
for([0, 1]) f:
    for([0, 1, 2, 3]) c:
        let v = example.NewArmArray()
        v.Lead = 21
        v.Flag = f != 0
        v.ItemsCount = c
        v.B = 300
        v.Tail = 99
        for(c) k:
            v.Items[k] = 8191 - k * 777
        let w = newstream()
        assert example.WriteArmArray(w, v)
        joins += done(w)
let uneven = example.NewHoldsUneven()
uneven.Lead = 21
uneven.Tail = 1500
let wu1 = newstream()
assert example.WriteHoldsUneven(wu1, uneven)
joins += done(wu1)
let nun = example.NewHoldsUneven()
nun.Lead = 21
nun.Tail = 1500
nun.U.Type = example.UnevenTypeNarrow
nun.U.Narrow.N = 5
let wu2 = newstream()
assert example.WriteHoldsUneven(wu2, nun)
joins += done(wu2)
let wide = example.NewHoldsUneven()
wide.Lead = 21
wide.Tail = 1500
wide.U.Type = example.UnevenTypeWide
wide.U.Wide.W = 123456789012
let wu3 = newstream()
assert example.WriteHoldsUneven(wu3, wide)
joins += done(wu3)
for([0, 1, 2, 3]) c:
    let v = example.NewArrUneven()
    v.Lead = 21
    v.Tail = 5
    v.ItemsCount = c
    for(c) k:
        if k % 2 == 0:
            v.Items[k].Type = example.UnevenTypeNarrow
            v.Items[k].Narrow.N = k % 8
        else:
            v.Items[k].Type = example.UnevenTypeWide
            v.Items[k].Wide.W = 99887766554 + k
    let w = newstream()
    assert example.WriteArrUneven(w, v)
    joins += done(w)
for([0, 1, 2, 3]) c:
    for([0, 4]) sl:
        let v = example.NewRegainAfterAlign()
        v.Lead = 21
        v.ItemsCount = c
        if sl != 0:
            v.S = put(v.S, "wxyz", 4)
            v.SLength = 4
        v.P = 0xDEADBEEF
        v.Q = 536870905
        v.R = 524285
        v.Tail = 9
        for(c) k:
            v.Items[k] = 8191 - k * 999
        let w = newstream()
        assert example.WriteRegainAfterAlign(w, v)
        joins += done(w)
hex(joins)
`
