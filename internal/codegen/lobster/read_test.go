// The type wire's read-side tests (docs/PORT-LOBSTER.md, THE HARNESS): the
// golden test generates examples/*.schema into a t.TempDir, reads each
// C++-pinned wire golden (testdata/wire/<name>.bin) through the generated
// Read<Name> functions under the pinned interpreter, and holds the values read
// to the instance test/go/main.go builds for it (the untaken side of each
// branch zero, SPEC §5; float32 leaves exact at float32; the compressed values
// the reference pins reconstruct). The whole golden must be consumed. The
// negative control patches one generated refusal (the reserved-bit check) out
// of the emitted read and requires the refusal test to go red while every
// golden read stays green.
package lobster

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
	"github.com/mas-bandwidth/schema/v2/ir"
)

var lobsterGoldenNames = []string{
	"shipcreate_flags", "rigidbody_moving", "rigidbody_at_rest", "chat",
	"probe_header", "compressed_probe", "probebits", "probecollider",
	"testdata", "inputpacket", "probearray", "degenerate", "clauses", "joins",
}

func TestLobsterTypeWireReadsTheGoldens(t *testing.T) {
	t.Parallel()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	out := runLobsterReadProbe(t, files, u, repo)
	for _, name := range append(lobsterGoldenNames, "refusals") {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if !strings.Contains(out, name+" ok\n") {
				t.Errorf("%s: the read probe did not report ok:\n%s", name, out)
			}
			if strings.Contains(out, name+" BAD") {
				t.Errorf("%s: a value or refusal mismatch:\n%s", name, out)
			}
		})
	}
}

func TestLobsterTypeWireReadNegativeControl(t *testing.T) {
	t.Parallel()
	root, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	repo := filepath.Join(root, "..", "..", "..")
	u := unitFromDir(t, filepath.Join(repo, "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	const anchor = "if rv != 0: return false // reserved("
	count := 0
	for _, data := range files {
		count += strings.Count(string(data), anchor)
	}
	if count == 0 {
		t.Fatalf("the reserved-bit refusal anchor %q is absent from the emitted read", anchor)
	}
	for name, data := range files {
		if strings.Contains(string(data), anchor) {
			files[name] = []byte(strings.Replace(string(data), anchor,
				"if false: return false // reserved(", 1))
		}
	}
	out := runLobsterReadProbe(t, files, u, repo)
	if !strings.Contains(out, "refusals BAD") {
		t.Fatalf("the corrupted const/reserved refusal stayed green after the sabotage:\n%s", out)
	}
	for _, name := range lobsterGoldenNames {
		if !strings.Contains(out, name+" ok\n") {
			t.Fatalf("%s went red under a refusal-only sabotage:\n%s", name, out)
		}
	}
}

// runLobsterReadProbe writes the generated modules, the runtime and the probe
// into a t.TempDir, feeds every golden in as a file, and returns stdout.
func runLobsterReadProbe(t *testing.T, files map[string][]byte, u *ir.Unit, repo string) string {
	t.Helper()
	dir := t.TempDir()
	if err := os.MkdirAll(filepath.Join(dir, "unused"), 0o755); err != nil {
		_ = err
	}
	for name, data := range files {
		writeFile(t, dir, name, string(data))
	}
	inputs := map[string][]byte{}
	for _, name := range lobsterGoldenNames {
		data, err := os.ReadFile(filepath.Join(repo, "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatalf("read wire golden %s: %v", name, err)
		}
		inputs[name+".bin"] = data
	}
	writeFile(t, dir, "main.lobster", lobsterReadProbe)
	return string(lobsterrun.Run(t, dir, "main.lobster", inputs))
}

// lobsterReadProbe reads each golden and checks the values test/go/main.go
// pins. It prints "<golden> ok" when every check in that block held and
// "<golden> BAD" otherwise; a failed read also prints its path.
const lobsterReadProbe = `import .Types
import .Wire
import .Enums
import .Degenerate
import .Clauses
import .Joins
import .serialize
import .schema_runtime

var bad = 0
def ck(ok:bool):
    if not ok:
        bad += 1
def rf32(x:float) -> float:
    return schema_rt.fround(x)

def run(name:string, ok:bool):
    if ok and bad == 0:
        print(name + " ok")
    else:
        print(name + " BAD")

def load(name:string) -> string:
    let d = read_file(name)
    if d: return d
    return ""
def start(data:string) -> serialize_read_stream:
    return serialize_read_stream_padded(data)

def take_slice(data:string, off:int, n:int) -> string:
    return substring(data, off, n)

// ---- shipcreate_flags ----
bad = 0
let shipData = load("shipcreate_flags.bin")
let ship = example.NewShipCreate()
let shipStream = start(shipData)
ck(example.ReadShipCreate(shipStream, ship))
ck(ship.ShipType == example.ShipTypeBomber)
ck(ship.Position.X == 1000 and ship.Position.Y == -2000 and ship.Position.Z == 3000)
ck(ship.HasFlags)
ck(ship.Flags == (example.ShipFlagsBoosting | example.ShipFlagsAiming))
ck(ship.Team == example.TeamBlue and ship.Health == 750 and ship.Thrust == 55)
ck(shipStream.bytes_processed() == length(shipData))
run("shipcreate_flags", true)

// ---- rigidbody_moving / rigidbody_at_rest ----
bad = 0
let moveData = load("rigidbody_moving.bin")
let moving = example.NewRigidBody()
ck(example.ReadRigidBody(start(moveData), moving))
ck(moving.Position.X == 1.5 and moving.Position.Y == -2.5 and moving.Position.Z == 3.25)
ck(moving.Orientation.X == 0.1 and moving.Orientation.W == 0.9)
ck(not moving.AtRest)
ck(moving.LinearVelocity.X == 10.0 and moving.LinearVelocity.Z == -3.0)
ck(moving.AngularVelocity.Y == 0.5)
run("rigidbody_moving", true)
bad = 0
let restData = load("rigidbody_at_rest.bin")
let rest = example.NewRigidBody()
ck(example.ReadRigidBody(start(restData), rest))
ck(rest.AtRest)
ck(rest.LinearVelocity.X == 0.0 and rest.LinearVelocity.Y == 0.0 and rest.LinearVelocity.Z == 0.0)
ck(rest.AngularVelocity.X == 0.0 and rest.AngularVelocity.Y == 0.0 and rest.AngularVelocity.Z == 0.0)
run("rigidbody_at_rest", true)

// ---- chat ----
bad = 0
let chatData = load("chat.bin")
let chat = example.NewChat()
ck(example.ReadChat(start(chatData), chat))
ck(chat.TextLength == 11)
ck(substring(chat.Text, 0, 11) == "wire parity")
run("chat", true)

// ---- probe_header: const, reserved, align; and the refusals ----
bad = 0
let phData = load("probe_header.bin")
let hdr = example.NewProbeHeader()
ck(example.ReadProbeHeader(start(phData), hdr))
ck(hdr.Version == 5 and hdr.ProbeId == 0x1122334455667788)
run("probe_header", true)

// ---- compressed_probe ----
bad = 0
let cpData = load("compressed_probe.bin")
let cp = example.NewCompressedProbe()
ck(example.ReadCompressedProbe(start(cpData), cp))
let wantB = rf32(rf32(rf32(1.0) / rf32(1000.0)) * rf32(10.0))
let wantO = rf32(rf32(rf32(142.0) / rf32(10000.0)) * rf32(10.0)) - rf32(5.0)
ck(cp.Boundary - wantB < 0.0000005 and wantB - cp.Boundary < 0.0000005)
ck(cp.Offset - wantO < 0.0000005 and wantO - cp.Offset < 0.0000005)
run("compressed_probe", true)

// ---- probebits ----
bad = 0
let pbData = load("probebits.bin")
let pb = example.NewProbeBits()
ck(example.ReadProbeBits(start(pbData), pb))
ck(pb.Small == 0x1FF and pb.Boundary == 0x1FFFFFFFF)
ck(pb.Wide == 0xFEDCBA9876543210)
ck(pb.Sensor == 4294967295 and pb.Nonce == 0xFFFFFFFFFFFFFFFF)
run("probebits", true)

// ---- probecollider ----
bad = 0
let pcData = load("probecollider.bin")
let pc = example.NewProbeCollider()
ck(example.ReadProbeCollider(start(pcData), pc))
ck(pc.Armor == 7)
ck(pc.Shape.Type == example.ProbeShapeTypeSlab)
ck(pc.Shape.Slab.Width == 42 and pc.Shape.Slab.Height == 9)
ck(pc.Backup.Type == example.ProbeShapeTypeNone)
ck(pc.ExtrasCount == 1 and pc.Extras[0].Type == example.ProbeShapeTypeRing and pc.Extras[0].Ring.Radius == 777)
run("probecollider", true)

// ---- testdata, inputpacket, probearray ----
bad = 0
let tdData = load("testdata.bin")
let td = example.NewTestData()
ck(example.ReadTestData(start(tdData), td))
run("testdata", true)
bad = 0
let ipData = load("inputpacket.bin")
let ip = example.NewInputPacket()
ck(example.ReadInputPacket(start(ipData), ip))
ck(ip.SynchronizeSequence == 7 and ip.CurrentFrame == 123456789 and ip.StartFrame == 123456780)
ck(ip.InputsCount == 2 and ip.Inputs[0].Throttle == 0.5 and ip.Inputs[0].Fire)
ck(ip.Inputs[1].StickX == -0.25 and ip.Inputs[1].Boost)
run("inputpacket", true)
bad = 0
let paData = load("probearray.bin")
let pa = example.NewProbeArray()
ck(example.ReadProbeArray(start(paData), pa))
ck(not pa.Samples[1].Active and pa.Samples[1].IdleTicks == 1000)
ck(pa.Samples[1].Weapon == example.WeaponNone and not pa.Samples[1].HasTarget)
ck(pa.Config.Retries == 3 and pa.Config.Preferred == example.WeaponMissile)
run("probearray", true)

// ---- degenerate: twelve shapes back to back in one stream ----
bad = 0
let dgData = load("degenerate.bin")
let dg = start(dgData)
let vec2 = example.NewVec2()
let spanF64 = example.NewSpanF64()
let spanU64 = example.NewSpanU64()
let spanI64 = example.NewSpanI64()
let spanOne = example.NewSpanOne()
let spanChunk = example.NewSpanChunk()
let spanTail = example.NewSpanTail()
let spanTwice = example.NewSpanTwice()
let trio = example.NewTrio()
let trioSole = example.NewTrioSole()
let trioFirst = example.NewTrioFirst()
let straddle = example.NewTrioStraddle()
ck(example.ReadVec2(dg, vec2))
ck(example.ReadSpanF64(dg, spanF64))
ck(example.ReadSpanU64(dg, spanU64))
ck(example.ReadSpanI64(dg, spanI64))
ck(example.ReadSpanOne(dg, spanOne))
ck(example.ReadSpanChunk(dg, spanChunk))
ck(example.ReadSpanTail(dg, spanTail))
ck(example.ReadSpanTwice(dg, spanTwice))
ck(example.ReadTrio(dg, trio))
ck(example.ReadTrioSole(dg, trioSole))
ck(example.ReadTrioFirst(dg, trioFirst))
ck(example.ReadTrioStraddle(dg, straddle))
ck(dg.bits_read == length(dgData) * 8)
ck(vec2.X == 1.5 and vec2.Y == -2.25)
ck(spanF64.Values[0] == 3.5 and spanF64.Values[1] == -4.75)
ck(spanU64.Values[0] == 0xDEADBEEFCAFEBABE and spanU64.Values[1] == 1)
ck(spanI64.Values[0] == -1234567890123 and spanI64.Values[1] == 42)
ck(spanOne.Values[0] == 0x0123456789ABCDEF)
ck(spanChunk.Values[0] == 0x1111 and spanChunk.Values[3] == 0x4444)
ck(spanTail.Values[0] == 6.125 and spanTail.Tail == 0xFEEDFACE)
ck(spanTwice.A[0] == 8.5 and spanTwice.B[1] == -11.5)
ck(trio.A == 0xABCDE and trio.C == 0xFFFFF)
ck(trioSole.Inner.B == 2)
ck(trioFirst.Inner.A == 0xAAAAA and trioFirst.Trailer == 0xBEEF)
ck(straddle.Pad0 == 0x0011223344556677 and straddle.Inner.C == 0x33333)
run("degenerate", true)

// ---- clauses: each shape read from its own flushed slice ----
bad = 0
let clData = load("clauses.bin")
var off = 0
def take(name:string, nbytes:int) -> serialize_read_stream:
    let r = serialize_read_stream_padded(take_slice(clData, off, nbytes))
    off += nbytes
    return r

let w13Counts = [0, 1, 3, 4, 5, 7, 12]
for(w13Counts) c:
    let v = example.NewW13()
    ck(example.ReadW13(take("W13", (4 + 13 * c + 7) / 8), v))
    ck(v.ItemsCount == c)
    for(c) i:
        ck(v.Items[i] == 8191 - i * 733)
let w17Counts = [0, 1, 2, 3, 4, 9]
for(w17Counts) c:
    let v = example.NewW17()
    ck(example.ReadW17(take("W17", (4 + 17 * c + 7) / 8), v))
    ck(v.ItemsCount == c)
    for(c) i:
        ck(v.Items[i] == 131071 - i * 11117)
let w26Counts = [0, 1, 2, 3, 6]
for(w26Counts) c:
    let v = example.NewW26()
    ck(example.ReadW26(take("W26", (3 + 26 * c + 7) / 8), v))
    ck(v.ItemsCount == c)
let w1Counts = [0, 1, 3, 4, 5, 20]
for(w1Counts) c:
    let v = example.NewW1()
    ck(example.ReadW1(take("W1", (5 + c + 7) / 8), v))
    ck(v.ItemsCount == c)
for(4) c:
    let v = example.NewW52()
    ck(example.ReadW52(take("W52", (2 + 52 * c + 7) / 8), v))
    ck(v.ItemsCount == c)
for(4) c:
    let v = example.NewW50()
    ck(example.ReadW50(take("W50", (2 + 50 * c + 7) / 8), v))
    ck(v.ItemsCount == c)
let f13 = example.NewF13()
ck(example.ReadF13(take("F13", (91 + 7) / 8), f13))
ck(f13.Items[0] == 8191 and f13.Items[6] == 8191 - 6 * 911)
for([0, 1, 3, 4, 5, 10]) c:
    let v = example.NewArrTri3()
    ck(example.ReadArrTri3(take("ArrTri3", (4 + 3 * c + 7) / 8), v))
    ck(v.ItemsCount == c)
let ae = example.NewArrEleven()
ck(example.ReadArrEleven(take("ArrEleven", (99 + 7) / 8), ae))
ck(ae.Items[0].B == 255 and ae.Items[8].A == 0)
for(3) _u:
    let v = example.NewHoldsEmptyUnion()
    ck(example.ReadHoldsEmptyUnion(take("HoldsEmptyUnion", (14 + 7) / 8), v))
    ck(v.Lead == 21 and v.Tail == 99)
for([27, 155, 75]) bits:
    let v = example.NewStrs()
    ck(example.ReadStrs(take("Strs", (bits + 7) / 8), v))
    ck(v.Lead == 21 and v.Tail == 5)
for(5) c:
    let v = example.NewArrNested()
    ck(example.ReadArrNested(take("ArrNested", (11 + 11 * c + 7) / 8), v))
    ck(v.Lead == 21 and v.Tail == 5 and v.ItemsCount == c)
let sole = example.NewSole()
ck(example.ReadSole(take("Sole", (13 + 7) / 8), sole))
ck(sole.Only == 5555)
ck(off == length(clData))
run("clauses", true)

// ---- refusals: a corrupted const, a nonzero reserved bit, an interior NUL ----
bad = 0
let phRaw = load("probe_header.bin")
var phCorrupt = ensure_size("", length(phRaw), 0)
phCorrupt = write_substring(phCorrupt, 0, phRaw, false)
phCorrupt = write_int8_le(phCorrupt, 1, read_uint8_le(phCorrupt, 1) | 0x08)
let phBad = example.NewProbeHeader()
ck(not example.ReadProbeHeader(start(phCorrupt), phBad))
var phConst = ensure_size("", length(phRaw), 0)
phConst = write_substring(phConst, 0, phRaw, false)
phConst = write_int8_le(phConst, 0, 0xAC)
let phBad2 = example.NewProbeHeader()
ck(not example.ReadProbeHeader(start(phConst), phBad2))
let chatRaw = load("chat.bin")
var chatCorrupt = ensure_size("", length(chatRaw), 0)
chatCorrupt = write_substring(chatCorrupt, 0, chatRaw, false)
chatCorrupt = write_int8_le(chatCorrupt, 4, 0)
let chatBad = example.NewChat()
ck(not example.ReadChat(start(chatCorrupt), chatBad))
run("refusals", true)

// ---- joins: both arms of every branch, each its own flushed slice ----
bad = 0
let jData = load("joins.bin")
var joff = 0
def takeJ(nbytes:int) -> serialize_read_stream:
    let r = serialize_read_stream_padded(substring(jData, joff, nbytes))
    joff += nbytes
    return r
for(2) f:
    let flag = f != 0
    let agree = example.NewArmsAgree()
    ck(example.ReadArmsAgree(takeJ(3), agree))
    ck(agree.Lead == 21 and agree.Flag == flag and agree.Tail == 99)
    if flag:
        ck(agree.A == 1234 and agree.B == 0)
    else:
        ck(agree.B == 1500 and agree.A == 0)
    let dis = example.NewArmsDisagree()
    ck(example.ReadArmsDisagree(takeJ(if flag: 3 else: 2), dis))
    ck(dis.Lead == 21 and dis.Flag == flag and dis.Tail == 99)
    if flag:
        ck(dis.A == 1234 and dis.B == 0)
    else:
        ck(dis.B == 5 and dis.A == 0)
    let emp = example.NewArmEmpty()
    ck(example.ReadArmEmpty(takeJ(if flag: 4 else: 2), emp))
    ck(emp.Lead == 21 and emp.Flag == flag and emp.Tail == 99)
    if flag:
        ck(emp.A == 456789)
    else:
        ck(emp.A == 0)
    let al = example.NewArmAlign()
    ck(example.ReadArmAlign(takeJ(if flag: 7 else: 3), al))
    ck(al.Lead == 21 and al.Flag == flag and al.Tail == 99)
    if flag:
        ck(al.SLength == 4 and substring(al.S, 0, 4) == "abcd")
    else:
        ck(al.B == 1000)
    let al2 = example.NewArmAlign()
    ck(example.ReadArmAlign(takeJ(3), al2))
    ck(al2.Lead == 21 and al2.Flag == flag and al2.Tail == 99)
    if not flag:
        ck(al2.B == 1000)
for(2) o:
    for(2) i:
        let outer = o != 0
        let inner = i != 0
        let bits = if outer: (if inner: 40 else: 16) else: 23
        let v = example.NewArmsNested()
        ck(example.ReadArmsNested(takeJ((bits + 7) / 8), v))
        ck(v.Lead == 5 and v.Outer == outer and v.Tail == 33)
        if outer:
            ck(v.Inner == inner)
            if inner:
                ck(v.X == 500000000)
            else:
                ck(v.Y == 17)
        else:
            ck(v.Z == 4000)
for(2) f:
    for(4) c:
        let flag = f != 0
        let bits = if flag: 15 + 13 * c else: 22
        let v = example.NewArmArray()
        ck(example.ReadArmArray(takeJ((bits + 7) / 8), v))
        ck(v.Lead == 21 and v.Flag == flag and v.Tail == 99)
        if flag:
            ck(v.ItemsCount == c)
        else:
            ck(v.B == 300)
for([18, 21, 55]) bits:
    let v = example.NewHoldsUneven()
    ck(example.ReadHoldsUneven(takeJ((bits + 7) / 8), v))
    ck(v.Lead == 21 and v.Tail == 1500)
for(4) c:
    let bits = 10 + [0, 5, 44, 49][c]
    let v = example.NewArrUneven()
    ck(example.ReadArrUneven(takeJ((bits + 7) / 8), v))
    ck(v.Lead == 21 and v.Tail == 5 and v.ItemsCount == c)
for(4) c:
    for([0, 4]) sl:
        let after = ((5 + 2 + 13 * c + 3) + 7) / 8 * 8
        let bits = after + 8 * sl + 84
        let v = example.NewRegainAfterAlign()
        ck(example.ReadRegainAfterAlign(takeJ((bits + 7) / 8), v))
        ck(v.Lead == 21 and v.ItemsCount == c and v.P == 0xDEADBEEF and v.Tail == 9)
ck(joff == length(jData))
run("joins", true)
`
