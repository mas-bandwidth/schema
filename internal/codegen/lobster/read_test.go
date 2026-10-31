// The type wire's read-side tests (docs/PORT-LOBSTER.md, THE HARNESS). The
// golden test generates examples/*.schema into a t.TempDir and, under the
// pinned interpreter, reads each C++-pinned wire golden
// (testdata/wire/<name>.bin) through the generated Read<Name> functions: the
// values read are the instance test/go/main.go builds (the untaken side of
// each branch zero, SPEC §5), the stream ends exactly there
// (serialize_end: no trailing byte, no set padding), and writing the value
// read gives the golden back byte for byte. The refusal rows corrupt a golden
// one rule at a time and name the reason the stream latched (serialize
// STANDARD.md, Refusal reasons). The negative controls patch one generated
// refusal out of the emitted read and require exactly its rows to go red.
package lobster

import (
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/codegen/lobster/lobsterrun"
)

// readRows is every row the read probe prints, and the result it must print:
// "ok" for a golden read (values, end and round trip all held), the latched
// refusal's name for a refusal row.
var readRows = map[string]string{
	"golden shipcreate_flags":  "ok",
	"golden rigidbody_moving":  "ok",
	"golden rigidbody_at_rest": "ok",
	"golden chat":              "ok",
	"golden probe_header":      "ok",
	"golden compressed_probe":  "ok",
	"golden probebits":         "ok",
	"golden probecollider":     "ok",
	"golden testdata":          "ok",
	"golden inputpacket":       "ok",
	"golden probearray":        "ok",
	"golden degenerate":        "ok",
	"golden clauses":           "ok",
	"golden joins":             "ok",
	// a const that is not its value, and a set reserved bit (SPEC §4.3)
	"refuse const":    "value_out_of_range",
	"refuse reserved": "value_out_of_range",
	// a string(N) payload with an interior NUL, and one that is not UTF-8
	// (SPEC §4.7); the NUL rule is checked first
	"refuse nul":     "embedded_nul",
	"refuse utf8":    "invalid_utf8",
	"refuse nulutf8": "embedded_nul",
	// a set bit in the align padding before the payload (SPEC §4.7)
	"refuse padding": "nonzero_padding",
	// a union tag past the arms (SPEC §4.8)
	"refuse tag": "value_out_of_range",
	// the stream ends where the packet ends (serialize STANDARD.md, end)
	"refuse trailing": "trailing_data",
	"refuse endpad":   "nonzero_padding",
	"refuse short":    "past_end",
	// a refusal is terminal: a later read refuses and keeps the first reason
	"refuse latched": "embedded_nul",
}

func TestLobsterTypeWireReadsTheGoldens(t *testing.T) {
	t.Parallel()
	got := runLobsterReadProbe(t, generateExamples(t))
	if bad := readRowsAgainst(got, readRows); len(bad) > 0 {
		t.Fatalf("read probe rows differ: %v\n%v", bad, got)
	}
}

// TestLobsterTypeWireReadNegativeControls patches one generated refusal out
// of the emitted read at a time (an anchor that must be present) and
// requires exactly the rows that refusal holds to go red, every other row
// unchanged: the probe rows are load-bearing, rule by rule.
func TestLobsterTypeWireReadNegativeControls(t *testing.T) {
	t.Parallel()
	for _, tc := range []struct {
		name, anchor, with string
		red                []string
	}{
		{"reserved", "if rv != 0: return stream.fail(serialize_refusal_value_out_of_range) // reserved(", "if false: return stream.fail(serialize_refusal_value_out_of_range) // reserved(", []string{"refuse reserved"}},
		{"const", "if rv != 171: return stream.fail(", "if false: return stream.fail(", []string{"refuse const"}},
		{"text", "if rt != serialize_refusal_none: return stream.fail(rt)", "if false: return stream.fail(rt)", []string{"refuse latched", "refuse nul", "refuse nulutf8", "refuse utf8"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			files := generateExamples(t)
			count := 0
			for name, data := range files {
				if n := strings.Count(string(data), tc.anchor); n > 0 {
					count += n
					files[name] = []byte(strings.ReplaceAll(string(data), tc.anchor, tc.with))
				}
			}
			if count == 0 {
				t.Fatalf("the refusal anchor %q is absent from the emitted read", tc.anchor)
			}
			got := runLobsterReadProbe(t, files)
			bad := readRowsAgainst(got, readRows)
			if !slices.Equal(bad, tc.red) {
				t.Fatalf("sabotaging %s turned rows %v red, want exactly %v:\n%v", tc.name, bad, tc.red, got)
			}
		})
	}
}

// readRowsAgainst is the sorted rows whose result is not the wanted one,
// missing rows included.
func readRowsAgainst(got, want map[string]string) []string {
	var bad []string
	for _, row := range slices.Sorted(maps.Keys(want)) {
		if got[row] != want[row] {
			bad = append(bad, row)
		}
	}
	for row := range got {
		if _, ok := want[row]; !ok {
			bad = append(bad, row)
		}
	}
	slices.Sort(bad)
	return bad
}

func generateExamples(t *testing.T) map[string][]byte {
	t.Helper()
	u := unitFromDir(t, filepath.Join("..", "..", "..", "examples"))
	files, err := Generate(u)
	if err != nil {
		t.Fatal(err)
	}
	return files
}

// runLobsterReadProbe writes the generated modules, the runtime and the probe
// into a t.TempDir, feeds every golden in as a file, and returns the rows.
func runLobsterReadProbe(t *testing.T, files map[string][]byte) map[string]string {
	t.Helper()
	dir := t.TempDir()
	writeAll(t, dir, files)
	inputs := map[string][]byte{}
	for row := range readRows {
		name, ok := strings.CutPrefix(row, "golden ")
		if !ok {
			continue
		}
		data, err := os.ReadFile(filepath.Join("..", "..", "..", "testdata", "wire", name+".bin"))
		if err != nil {
			t.Fatalf("read wire golden %s: %v", name, err)
		}
		inputs[name+".bin"] = data
	}
	writeFile(t, dir, "main.lobster", lobsterReadProbe)
	out := string(lobsterrun.Run(t, dir, "main.lobster", inputs))
	rows := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimRight(out, "\n"), "\n") {
		i := strings.LastIndexByte(line, ' ')
		if i < 0 {
			t.Fatalf("read probe printed %q, not a row:\n%s", line, out)
		}
		rows[line[:i]] = line[i+1:]
	}
	return rows
}

// lobsterReadProbe reads each golden and checks the values test/go/main.go
// pins, then corrupts goldens one rule at a time.
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
        print("golden " + name + " ok")
    else:
        print("golden " + name + " BAD")

def load(name:string) -> string:
    let d = read_file(name)
    if d: return d
    return ""
def start(data:string) -> serialize_read_stream:
    return serialize_read_stream_padded(data)

// whole reads data with read and requires the stream to end exactly there.
def whole(data:string, read) -> bool:
    let s = start(data)
    if not read(s): return false
    return s.serialize_end()

// again writes with write and requires the golden's bytes back.
def again(data:string, write) -> bool:
    let w = serialize_write_stream(ensure_size("", 8192, 0))
    if not write(w): return false
    w.flush()
    return substring(w.buffer, 0, w.bytes_processed()) == data

def take_slice(data:string, off:int, n:int) -> string:
    return substring(data, off, n)

// the per-slice streams of clauses and joins; each must end exactly
var slices:[serialize_read_stream] = []
def all_ended() -> bool:
    var ok = true
    for(slices) r:
        if not r.serialize_end(): ok = false
    slices = []
    return ok

// patched copies data with byte i replaced by b (b < 0 appends -b - 1 bytes)
def patched(data:string, i:int, b:int) -> string:
    var c = ensure_size("", length(data), 0)
    c = write_substring(c, 0, data, false)
    if i >= 0:
        c = write_int8_le(c, i, b)
    return c

def refused(row:string, s:serialize_read_stream, ok:bool):
    if ok:
        print("refuse " + row + " none")
    else:
        print("refuse " + row + " " + s.refusal_name())

` + lobsterReadGoldens + lobsterReadRefusals

const lobsterReadGoldens = `// ---- shipcreate_flags ----
bad = 0
let shipData = load("shipcreate_flags.bin")
let ship = example.NewShipCreate()
ck(whole(shipData) s: example.ReadShipCreate(s, ship))
ck(ship.ShipType == example.ShipTypeBomber)
ck(ship.Position.X == 1000 and ship.Position.Y == -2000 and ship.Position.Z == 3000)
ck(ship.HasFlags)
ck(ship.Flags == (example.ShipFlagsBoosting | example.ShipFlagsAiming))
ck(ship.Team == example.TeamBlue and ship.Health == 750 and ship.Thrust == 55)
ck(again(shipData) w: example.WriteShipCreate(w, ship))
run("shipcreate_flags", true)

// ---- rigidbody_moving / rigidbody_at_rest ----
bad = 0
let moveData = load("rigidbody_moving.bin")
let moving = example.NewRigidBody()
ck(whole(moveData) s: example.ReadRigidBody(s, moving))
ck(moving.Position.X == 1.5 and moving.Position.Y == -2.5 and moving.Position.Z == 3.25)
ck(moving.Orientation.X == 0.1 and moving.Orientation.W == 0.9)
ck(not moving.AtRest)
ck(moving.LinearVelocity.X == 10.0 and moving.LinearVelocity.Z == -3.0)
ck(moving.AngularVelocity.Y == 0.5)
ck(again(moveData) w: example.WriteRigidBody(w, moving))
run("rigidbody_moving", true)
bad = 0
let restData = load("rigidbody_at_rest.bin")
let rest = example.NewRigidBody()
ck(whole(restData) s: example.ReadRigidBody(s, rest))
ck(rest.AtRest)
ck(rest.LinearVelocity.X == 0.0 and rest.LinearVelocity.Y == 0.0 and rest.LinearVelocity.Z == 0.0)
ck(rest.AngularVelocity.X == 0.0 and rest.AngularVelocity.Y == 0.0 and rest.AngularVelocity.Z == 0.0)
ck(again(restData) w: example.WriteRigidBody(w, rest))
run("rigidbody_at_rest", true)

// ---- chat ----
bad = 0
let chatData = load("chat.bin")
let chat = example.NewChat()
ck(whole(chatData) s: example.ReadChat(s, chat))
ck(chat.TextLength == 11)
ck(substring(chat.Text, 0, 11) == "wire parity")
ck(again(chatData) w: example.WriteChat(w, chat))
run("chat", true)

// ---- probe_header: const, reserved, align; and the refusals ----
bad = 0
let phData = load("probe_header.bin")
let hdr = example.NewProbeHeader()
ck(whole(phData) s: example.ReadProbeHeader(s, hdr))
ck(hdr.Version == 5 and hdr.ProbeId == 0x1122334455667788)
ck(again(phData) w: example.WriteProbeHeader(w, hdr))
run("probe_header", true)

// ---- compressed_probe ----
bad = 0
let cpData = load("compressed_probe.bin")
let cp = example.NewCompressedProbe()
ck(whole(cpData) s: example.ReadCompressedProbe(s, cp))
let wantB = rf32(rf32(rf32(1.0) / rf32(1000.0)) * rf32(10.0))
let wantO = rf32(rf32(rf32(142.0) / rf32(10000.0)) * rf32(10.0)) - rf32(5.0)
ck(cp.Boundary - wantB < 0.0000005 and wantB - cp.Boundary < 0.0000005)
ck(cp.Offset - wantO < 0.0000005 and wantO - cp.Offset < 0.0000005)
ck(again(cpData) w: example.WriteCompressedProbe(w, cp))
run("compressed_probe", true)

// ---- probebits ----
bad = 0
let pbData = load("probebits.bin")
let pb = example.NewProbeBits()
ck(whole(pbData) s: example.ReadProbeBits(s, pb))
ck(pb.Small == 0x1FF and pb.Boundary == 0x1FFFFFFFF)
ck(pb.Wide == 0xFEDCBA9876543210)
ck(pb.Sensor == 4294967295 and pb.Nonce == 0xFFFFFFFFFFFFFFFF)
ck(again(pbData) w: example.WriteProbeBits(w, pb))
run("probebits", true)

// ---- probecollider ----
bad = 0
let pcData = load("probecollider.bin")
let pc = example.NewProbeCollider()
ck(whole(pcData) s: example.ReadProbeCollider(s, pc))
ck(pc.Armor == 7)
ck(pc.Shape.Type == example.ProbeShapeTypeSlab)
ck(pc.Shape.Slab.Width == 42 and pc.Shape.Slab.Height == 9)
ck(pc.Backup.Type == example.ProbeShapeTypeNone)
ck(pc.ExtrasCount == 1 and pc.Extras[0].Type == example.ProbeShapeTypeRing and pc.Extras[0].Ring.Radius == 777)
ck(again(pcData) w: example.WriteProbeCollider(w, pc))
run("probecollider", true)

// ---- testdata, inputpacket, probearray ----
bad = 0
let tdData = load("testdata.bin")
let td = example.NewTestData()
ck(whole(tdData) s: example.ReadTestData(s, td))
ck(again(tdData) w: example.WriteTestData(w, td))
run("testdata", true)
bad = 0
let ipData = load("inputpacket.bin")
let ip = example.NewInputPacket()
ck(whole(ipData) s: example.ReadInputPacket(s, ip))
ck(ip.SynchronizeSequence == 7 and ip.CurrentFrame == 123456789 and ip.StartFrame == 123456780)
ck(ip.InputsCount == 2 and ip.Inputs[0].Throttle == 0.5 and ip.Inputs[0].Fire)
ck(ip.Inputs[1].StickX == -0.25 and ip.Inputs[1].Boost)
ck(again(ipData) w: example.WriteInputPacket(w, ip))
run("inputpacket", true)
bad = 0
let paData = load("probearray.bin")
let pa = example.NewProbeArray()
ck(whole(paData) s: example.ReadProbeArray(s, pa))
ck(not pa.Samples[1].Active and pa.Samples[1].IdleTicks == 1000)
ck(pa.Samples[1].Weapon == example.WeaponNone and not pa.Samples[1].HasTarget)
ck(pa.Config.Retries == 3 and pa.Config.Preferred == example.WeaponMissile)
ck(again(paData) w: example.WriteProbeArray(w, pa))
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
ck(dg.serialize_end())
def write_degenerate(w:serialize_write_stream) -> bool:
    example.WriteVec2(w, vec2)
    example.WriteSpanF64(w, spanF64)
    example.WriteSpanU64(w, spanU64)
    example.WriteSpanI64(w, spanI64)
    example.WriteSpanOne(w, spanOne)
    example.WriteSpanChunk(w, spanChunk)
    example.WriteSpanTail(w, spanTail)
    example.WriteSpanTwice(w, spanTwice)
    example.WriteTrio(w, trio)
    example.WriteTrioSole(w, trioSole)
    example.WriteTrioFirst(w, trioFirst)
    example.WriteTrioStraddle(w, straddle)
    return true
ck(again(dgData) w: write_degenerate(w))
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
    slices.push(r)
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
for(3):
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
ck(all_ended())
run("clauses", true)

// ---- joins: both arms of every branch, each its own flushed slice ----
bad = 0
let jData = load("joins.bin")
var joff = 0
def takeJ(nbytes:int) -> serialize_read_stream:
    let r = serialize_read_stream_padded(substring(jData, joff, nbytes))
    joff += nbytes
    slices.push(r)
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
ck(all_ended())
run("joins", true)
`

const lobsterReadRefusals = `
// ---- refusals: one rule at a time, each latched with its reason ----
let phRaw = load("probe_header.bin")
let rConst = start(patched(phRaw, 0, 0xAC))
refused("const", rConst, example.ReadProbeHeader(rConst, example.NewProbeHeader()))
let rReserved = start(patched(phRaw, 1, read_uint8_le(phRaw, 1) | 0x08))
refused("reserved", rReserved, example.ReadProbeHeader(rReserved, example.NewProbeHeader()))
let chatRaw = load("chat.bin")
let rNul = start(patched(chatRaw, 4, 0))
refused("nul", rNul, example.ReadChat(rNul, example.NewChat()))
let rUtf8 = start(patched(chatRaw, 4, 0xFF))
refused("utf8", rUtf8, example.ReadChat(rUtf8, example.NewChat()))
let rBoth = start(patched(patched(chatRaw, 3, 0xFF), 9, 0))
refused("nulutf8", rBoth, example.ReadChat(rBoth, example.NewChat()))
let rPad = start(patched(chatRaw, 1, read_uint8_le(chatRaw, 1) | 0x02))
refused("padding", rPad, example.ReadChat(rPad, example.NewChat()))
let pcRaw = load("probecollider.bin")
let rTag = start(patched(pcRaw, 1, read_uint8_le(pcRaw, 1) | 0x03))
refused("tag", rTag, example.ReadProbeCollider(rTag, example.NewProbeCollider()))
let rTrail = start(chatRaw + "\x00")
refused("trailing", rTrail, example.ReadChat(rTrail, example.NewChat()) and rTrail.serialize_end())
let rEnd = start(patched(pcRaw, 5, read_uint8_le(pcRaw, 5) | 0x80))
refused("endpad", rEnd, example.ReadProbeCollider(rEnd, example.NewProbeCollider()) and rEnd.serialize_end())
let rShort = start(substring(chatRaw, 0, length(chatRaw) - 1))
refused("short", rShort, example.ReadChat(rShort, example.NewChat()))
let rLatch = start(patched(chatRaw, 4, 0) + phRaw)
let latchFirst = example.ReadChat(rLatch, example.NewChat())
refused("latched", rLatch, latchFirst or example.ReadProbeHeader(rLatch, example.NewProbeHeader()))
`
