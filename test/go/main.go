// The Go cross-language wire test: the generated Go package writes the SAME
// pinned instances the C++ test pins in testdata/wire/*.bin and byte-compares
// against those files — cross-language wire identity is the §7.2 gate this
// binary carries (goal 3: byte-identical wire output across all targets, and
// the readers agree on what they reject). Plus round-trips through the Go
// reader, the §5 branch-zeroing checks, and the specified-defaults checks.
//
// Prints OK and exits 0, exactly like its C++ twins. Run from test/go
// (the Makefile does): the wire goldens are at ../../testdata/wire.
package main

import (
	"bytes"
	"fmt"
	"os"

	"example"

	"github.com/mas-bandwidth/serialize.go"
)

var failed bool

func check(ok bool, what string) {
	if !ok {
		fmt.Printf("FAILED: %s\n", what)
		failed = true
	}
}

func checkErr(err error, what string) {
	if err != nil {
		fmt.Printf("FAILED: %s: %v\n", what, err)
		failed = true
	}
}

// goldenWire byte-compares written wire against the C++-pinned golden.
func goldenWire(name string, data []byte) {
	golden, err := os.ReadFile("../../testdata/wire/" + name + ".bin")
	checkErr(err, "read wire golden "+name)
	if err != nil {
		return
	}
	check(bytes.Equal(data, golden), "wire golden "+name+" — Go bytes must equal the C++-pinned bytes")
}

func newWriteStream() (*serialize.WriteStream, []byte) {
	buffer := make([]byte, 2048)
	return serialize.NewWriteStream(buffer), buffer
}

func main() {
	// ---- ShipCreate: the bool-gated flags branch, both ways ----
	{
		in := example.ShipCreate{}
		in.ShipType = example.ShipTypeBomber
		in.Position = example.QuantizedPosition{X: 1000, Y: -2000, Z: 3000}
		in.HasFlags = true
		in.Flags = example.ShipFlagsBoosting | example.ShipFlagsAiming
		in.Team = example.TeamBlue
		in.Health = 750
		in.Thrust = 55

		ws, _ := newWriteStream()
		checkErr(example.WriteShipCreate(ws, &in), "write ShipCreate")
		ws.Flush()
		goldenWire("shipcreate_flags", ws.Data())

		out := example.ShipCreate{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadShipCreate(rs, &out), "read ShipCreate")
		check(out == in, "ShipCreate round-trips")

		// untaken branch: flags must read back ZERO (SPEC §5) — into the same
		// out value, so stale flags would be caught
		in.HasFlags = false
		ws2, _ := newWriteStream()
		checkErr(example.WriteShipCreate(ws2, &in), "write ShipCreate no-flags")
		ws2.Flush()
		rs2 := serialize.NewReadStream(ws2.Data())
		checkErr(example.ReadShipCreate(rs2, &out), "read ShipCreate no-flags")
		check(!out.HasFlags && out.Flags == 0, "untaken branch reads as zero (SPEC §5)")
	}

	// ---- RigidBody: the back-reference example, both branch sides ----
	{
		in := example.RigidBody{}
		in.Position = example.Vec3{X: 1.5, Y: -2.5, Z: 3.25}
		in.Orientation = example.Quat{X: 0.1, Y: 0.2, Z: 0.3, W: 0.9}
		in.AtRest = false
		in.LinearVelocity = example.Vec3{X: 10.0, Y: 20.0, Z: -3.0}
		in.AngularVelocity = example.Vec3{X: 0.25, Y: 0.5, Z: 0.75}

		ws, _ := newWriteStream()
		checkErr(example.WriteRigidBody(ws, &in), "write RigidBody moving")
		ws.Flush()
		goldenWire("rigidbody_moving", ws.Data())

		in.AtRest = true
		ws2, _ := newWriteStream()
		checkErr(example.WriteRigidBody(ws2, &in), "write RigidBody at rest")
		ws2.Flush()
		goldenWire("rigidbody_at_rest", ws2.Data())

		// the at-rest read must ZERO both velocities (SPEC §5), even though
		// the written value had them set
		out := example.RigidBody{}
		rs := serialize.NewReadStream(ws2.Data())
		checkErr(example.ReadRigidBody(rs, &out), "read RigidBody at rest")
		check(out.AtRest, "at_rest reads true")
		check(out.LinearVelocity == example.Vec3{} && out.AngularVelocity == example.Vec3{},
			"velocities read as zero under the taken at-rest branch (SPEC §5)")
	}

	// ---- Chat: the string framing == classic serialize_string over N + 1 ----
	{
		in := example.Chat{}
		copy(in.Text[:], "wire parity")
		in.TextLength = 11

		ws, _ := newWriteStream()
		checkErr(example.WriteChat(ws, &in), "write Chat")
		ws.Flush()
		goldenWire("chat", ws.Data())

		out := example.Chat{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadChat(rs, &out), "read Chat")
		check(out == in, "Chat round-trips")
	}

	// ---- ProbeHeader: const/reserved/align on the wire; corruption rejected ----
	{
		in := example.ProbeHeader{Version: 5, ProbeId: 0x1122334455667788}
		ws, _ := newWriteStream()
		checkErr(example.WriteProbeHeader(ws, &in), "write ProbeHeader")
		ws.Flush()
		data := ws.Data()
		check(data[0] == 0xAB, "const(0xAB, 8) leads the wire")
		goldenWire("probe_header", data)

		out := example.ProbeHeader{}
		rs := serialize.NewReadStream(data)
		checkErr(example.ReadProbeHeader(rs, &out), "read ProbeHeader")
		check(out == in, "ProbeHeader round-trips")

		corrupt := append([]byte(nil), data...)
		corrupt[0] = 0xAC
		rs2 := serialize.NewReadStream(corrupt)
		check(example.ReadProbeHeader(rs2, &out) != nil, "a corrupted wire constant is REJECTED (SPEC §4.3)")
	}

	// ---- InputPacket: counted array of nested structs ----
	{
		in := example.InputPacket{}
		in.SynchronizeSequence = 7
		in.CurrentFrame = 123456789
		in.StartFrame = 123456780
		in.InputsCount = 2
		in.Inputs[0].Throttle = 0.5
		in.Inputs[0].Fire = true
		in.Inputs[1].StickX = -0.25
		in.Inputs[1].Boost = true

		ws, _ := newWriteStream()
		checkErr(example.WriteInputPacket(ws, &in), "write InputPacket")
		ws.Flush()

		out := example.InputPacket{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadInputPacket(rs, &out), "read InputPacket")
		check(out == in, "InputPacket round-trips")
	}

	// ---- TestData: the vanilla library's own test type, deterministic values ----
	{
		in := testDataInstance()

		ws, _ := newWriteStream()
		checkErr(example.WriteTestData(ws, &in), "write TestData")
		ws.Flush()

		out := example.TestData{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadTestData(rs, &out), "read TestData")
		check(out == in, "TestData round-trips — signed narrows, full-range ints, align, fixed bytes, string")
	}

	// ---- CompressedProbe: the FMA-boundary vectors (SPEC §7.2 gate 7) ----
	// 0.005 quantizes to 1 under the float32 two-rounding law (a fused or
	// double build says 0); -4.8585 over the non-zero-min range quantizes to
	// 142 (a double build says 141). Same pinned instance as the C++ leg,
	// against the same golden.
	{
		in := example.CompressedProbe{}
		in.Boundary = 0.005
		in.Offset = -4.8585

		ws, _ := newWriteStream()
		checkErr(example.WriteCompressedProbe(ws, &in), "write CompressedProbe")
		ws.Flush()
		goldenWire("compressed_probe", ws.Data())

		out := example.CompressedProbe{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadCompressedProbe(rs, &out), "read CompressedProbe")
		// through variables, not constants: Go folds constant float
		// expressions in arbitrary precision, which is NOT the float32
		// per-op arithmetic the reader performs
		maxIntBoundary := float32(1000)
		maxIntOffset := float32(10000)
		check(out.Boundary == float32(1)/maxIntBoundary*float32(10), "boundary reconstructs integer 1")
		check(out.Offset == float32(142)/maxIntOffset*float32(10)-float32(5), "offset reconstructs integer 142")
	}

	// ---- specified defaults: New* carries them; the zero value stays zero ----
	{
		sample := example.NewProbeSample()
		check(sample.Active, "ProbeSample.active defaults true")
		check(example.ProbeSample{}.Active == false, "the plain zero value stays zero")
		config := example.NewProbeConfig()
		check(config.Retries == -1, "ProbeConfig.retries defaults -1")
		check(config.Preferred == example.WeaponRailgun, "ProbeConfig.preferred defaults Railgun")
	}

	// ---- ProbeBits: the full-range uint32/uint64 paths, C++-pinned ----
	{
		in := example.ProbeBits{}
		in.Small = 0x1FF
		in.Boundary = 0x1FFFFFFFF
		in.Wide = 0xFEDCBA9876543210
		in.Sensor = 4294967295
		in.Nonce = 18446744073709551615

		ws, _ := newWriteStream()
		checkErr(example.WriteProbeBits(ws, &in), "write ProbeBits")
		ws.Flush()
		goldenWire("probebits", ws.Data())

		out := example.ProbeBits{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadProbeBits(rs, &out), "read ProbeBits")
		check(out == in, "ProbeBits round-trips — 9/33/64-bit and full-range paths")
	}

	// ---- ProbeCollider: first-class one-of (SPEC §4.8) — C++-pinned wire,
	// round trip, the None arm, an array of unions, and the refusal
	// negative controls ----
	{
		in := example.ProbeCollider{}
		check(in.Shape.Type == example.ProbeShapeTypeNone, "the zero value is the empty union")
		check(example.ProbeShapeMaxBits == 2+16, "ProbeShapeMaxBits is tag + the largest arm")

		in.Armor = 7
		in.Shape.Type = example.ProbeShapeTypeSlab
		in.Shape.Slab.Width = 42
		in.Shape.Slab.Height = 9
		// in.Backup stays None — the empty arm costs the tag bits only
		in.ExtrasCount = 1
		in.Extras[0].Type = example.ProbeShapeTypeRing
		in.Extras[0].Ring.Radius = 777

		ws, _ := newWriteStream()
		checkErr(example.WriteProbeCollider(ws, &in), "write ProbeCollider")
		ws.Flush()
		goldenWire("probecollider", ws.Data())

		out := example.ProbeCollider{}
		out.Backup.Type = example.ProbeShapeTypeRing // dirty — the read must restore None
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadProbeCollider(rs, &out), "read ProbeCollider")
		check(out.Armor == 7, "ProbeCollider.armor round-trips")
		check(out.Shape.Type == example.ProbeShapeTypeSlab, "the selected arm round-trips")
		check(out.Shape.Slab.Width == 42 && out.Shape.Slab.Height == 9, "the arm payload round-trips")
		check(out.Backup.Type == example.ProbeShapeTypeNone, "the None arm reads back empty")
		check(out.ExtrasCount == 1 && out.Extras[0].Type == example.ProbeShapeTypeRing && out.Extras[0].Ring.Radius == 777,
			"the union array round-trips")

		// NEGATIVE CONTROL — perturb the tag: 2 bits at bit offset 8, range
		// [0, 2]; forcing both bits makes it 3 and the read must refuse
		corrupt := make([]byte, len(ws.Data()))
		copy(corrupt, ws.Data())
		corrupt[1] |= 0x03
		bad := example.ProbeCollider{}
		check(example.ReadProbeCollider(serialize.NewReadStream(corrupt), &bad) != nil,
			"an out-of-range union tag is refused (SPEC §4.8)")

		// NEGATIVE CONTROL — corrupt the arm payload: width rides 7 bits at
		// bit offset 10 with range [0, 100]; all seven bits decode 127
		copy(corrupt, ws.Data())
		corrupt[1] |= 0xFC
		corrupt[2] |= 0x01
		check(example.ReadProbeCollider(serialize.NewReadStream(corrupt), &bad) != nil,
			"a corrupt union arm payload is refused (SPEC §4.8)")

		// the write side validates the tag BEFORE it rides
		rogue := example.ProbeShape{Type: example.ProbeShapeType(3)}
		ws2, _ := newWriteStream()
		check(example.WriteProbeShape(ws2, &rogue) != nil,
			"an out-of-set union tag writes nothing (SPEC §4.8)")
	}

	// ---- TestData and InputPacket against their C++ pins ----
	{
		in := testDataInstance()
		ws, _ := newWriteStream()
		checkErr(example.WriteTestData(ws, &in), "write TestData (pin)")
		ws.Flush()
		goldenWire("testdata", ws.Data())

		packet := example.InputPacket{}
		packet.SynchronizeSequence = 7
		packet.CurrentFrame = 123456789
		packet.StartFrame = 123456780
		packet.InputsCount = 2
		packet.Inputs[0].Throttle = 0.5
		packet.Inputs[0].Fire = true
		packet.Inputs[1].StickX = -0.25
		packet.Inputs[1].Boost = true
		ws2, _ := newWriteStream()
		checkErr(example.WriteInputPacket(ws2, &packet), "write InputPacket (pin)")
		ws2.Flush()
		goldenWire("inputpacket", ws2.Data())
	}

	// ---- ProbeSample: the nested if/else wire, both ways, and §5 zeroing ----
	{
		in := example.NewProbeSample() // active = true
		in.Orientation = 90.0
		in.RawDelta = -5
		in.BigDelta = -1234567890123
		in.Weapon = example.WeaponLaser
		in.HasTarget = true
		in.TargetId = 777
		in.IdleTicks = 12345 // untaken side on the wire — must read back ZERO
		in.SamplesCount = 1
		in.Samples[0] = 42

		ws, _ := newWriteStream()
		checkErr(example.WriteProbeSample(ws, &in), "write ProbeSample active")
		ws.Flush()
		out := example.ProbeSample{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadProbeSample(rs, &out), "read ProbeSample active")
		check(out.Active && out.Weapon == example.WeaponLaser && out.HasTarget && out.TargetId == 777,
			"the taken branch round-trips, nested branch included")
		check(out.IdleTicks == 0, "the untaken else side reads as zero (SPEC §5)")
		check(out.Orientation == 90.0, "compressed float round-trips exactly at its resolution")

		in.Active = false
		in.HasTarget = false
		ws2, _ := newWriteStream()
		checkErr(example.WriteProbeSample(ws2, &in), "write ProbeSample idle")
		ws2.Flush()
		rs2 := serialize.NewReadStream(ws2.Data())
		checkErr(example.ReadProbeSample(rs2, &out), "read ProbeSample idle")
		check(!out.Active && out.IdleTicks == 12345, "the else branch round-trips")
		check(out.Weapon == example.WeaponNone && !out.HasTarget && out.TargetId == 0,
			"the whole untaken then side reads as zero, nested branch included (SPEC §5)")
	}

	// ---- ProbeArray: transitive defaults and its C++ pin ----
	{
		fresh := example.NewProbeArray()
		check(fresh.Samples[0].Active && fresh.Samples[1].Active, "defaults reach through a fixed array")
		check(fresh.Config.Retries == -1 && fresh.Config.Preferred == example.WeaponRailgun,
			"defaults reach through a plain member")

		in := example.NewProbeArray()
		in.Samples[0].Orientation = 90.0
		in.Samples[0].RawDelta = -5
		in.Samples[0].BigDelta = -1234567890123
		in.Samples[0].Weapon = example.WeaponLaser
		in.Samples[0].HasTarget = true
		in.Samples[0].TargetId = 777
		in.Samples[0].SamplesCount = 1
		in.Samples[0].Samples[0] = 42
		in.Samples[1].Active = false
		in.Samples[1].Orientation = -45.5
		in.Samples[1].RawDelta = 7
		in.Samples[1].BigDelta = 99
		in.Samples[1].IdleTicks = 1000
		in.Samples[1].SamplesCount = 2
		in.Samples[1].Samples[0] = 7
		in.Samples[1].Samples[1] = 8
		in.Config.Retries = 3
		in.Config.Preferred = example.WeaponMissile

		ws, _ := newWriteStream()
		checkErr(example.WriteProbeArray(ws, &in), "write ProbeArray")
		ws.Flush()
		goldenWire("probearray", ws.Data())

		out := example.ProbeArray{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadProbeArray(rs, &out), "read ProbeArray")
		check(!out.Samples[1].Active && out.Samples[1].IdleTicks == 1000, "nested else branch round-trips")
		check(out.Samples[1].Weapon == example.WeaponNone && !out.Samples[1].HasTarget,
			"nested untaken side reads as zero (SPEC §5)")
		check(out.Config.Retries == 3 && out.Config.Preferred == example.WeaponMissile, "config round-trips")
	}

	// ---- ProbeReport: nested composition, and the widened flags wire ----
	{
		in := example.ProbeReport{}
		in.Header.Version = 3
		in.Header.ProbeId = 0xCAFEBABE
		in.Flags = example.ProbeFlagsArmed | example.ProbeFlagsDamaged
		in.Echo.TestA = 555
		in.Echo.TestB = 1000

		ws, _ := newWriteStream()
		checkErr(example.WriteProbeReport(ws, &in), "write ProbeReport")
		ws.Flush()
		out := example.ProbeReport{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadProbeReport(rs, &out), "read ProbeReport")
		check(out == in, "ProbeReport round-trips — a named type as an ordinary field")

		// a mask bit above the widened 8-bit wire is refused, not truncated
		in.Flags = 1 << 9
		ws2, _ := newWriteStream()
		check(example.WriteProbeReport(ws2, &in) != nil, "a mask bit above the flags wire width is refused")
	}

	// ---- Block: the bytes(N) framing ----
	{
		in := example.Block{}
		for i := 0; i < 100; i++ {
			in.Data[i] = byte(i)
		}
		in.DataLength = 100

		ws, _ := newWriteStream()
		checkErr(example.WriteBlock(ws, &in), "write Block")
		ws.Flush()
		out := example.Block{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadBlock(rs, &out), "read Block")
		check(out == in, "Block round-trips — bytes(N) framing")
	}

	// ---- the readers agree on what they REJECT (goal 3's second half) ----
	{
		// an interior null in a string is content the read refuses
		chatGolden, err := os.ReadFile("../../testdata/wire/chat.bin")
		checkErr(err, "read chat golden for corruption")
		corrupt := append([]byte(nil), chatGolden...)
		corrupt[4] = 0 // inside the text bytes (length rides bytes 0-1, align pads to byte 2)
		out := example.Chat{}
		rs := serialize.NewReadStream(corrupt)
		check(example.ReadChat(rs, &out) == example.ErrValidation, "an interior null is rejected as validation")

		// a truncated stream is the stream's own error, never a content verdict
		truncated := append([]byte(nil), chatGolden[:3]...)
		out2 := example.Chat{}
		rs2 := serialize.NewReadStream(truncated)
		err = example.ReadChat(rs2, &out2)
		check(err != nil && err != example.ErrValidation, "truncation surfaces as the stream error")

		// a nonzero reserved bit is rejected
		probeGolden, err := os.ReadFile("../../testdata/wire/probe_header.bin")
		checkErr(err, "read probe golden for corruption")
		corrupt2 := append([]byte(nil), probeGolden...)
		corrupt2[1] |= 0x08 // the first reserved bit above version's 3
		out3 := example.ProbeHeader{}
		rs3 := serialize.NewReadStream(corrupt2)
		check(example.ReadProbeHeader(rs3, &out3) == example.ErrValidation, "a nonzero reserved bit is rejected")

		// an out-of-range array count is refused before any element rides —
		// corrupt the count bits INSIDE a complete valid wire (the preamble is
		// 16+64+64 = 144 bits, so the 5-bit count sits at byte 18 bits 0-4),
		// so the refusal is the RANGE check, not a truncation overflow
		packetGolden, err := os.ReadFile("../../testdata/wire/inputpacket.bin")
		checkErr(err, "read inputpacket golden for corruption")
		corrupt3 := append([]byte(nil), packetGolden...)
		corrupt3[18] = (corrupt3[18] &^ 0x1F) | 17 // count 2 -> 17, over [0, 16]
		out4 := example.InputPacket{}
		rs4 := serialize.NewReadStream(corrupt3)
		err = example.ReadInputPacket(rs4, &out4)
		check(err != nil && err != example.ErrValidation, "an out-of-range count is refused before the loop")
	}

	// ---- RigidBody: the moving branch read back whole ----
	{
		in := example.RigidBody{}
		in.Position = example.Vec3{X: 1.5, Y: -2.5, Z: 3.25}
		in.Orientation = example.Quat{X: 0.1, Y: 0.2, Z: 0.3, W: 0.9}
		in.AtRest = false
		in.LinearVelocity = example.Vec3{X: 10.0, Y: 20.0, Z: -3.0}
		in.AngularVelocity = example.Vec3{X: 0.25, Y: 0.5, Z: 0.75}

		ws, _ := newWriteStream()
		checkErr(example.WriteRigidBody(ws, &in), "write RigidBody moving (read-back)")
		ws.Flush()
		out := example.RigidBody{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadRigidBody(rs, &out), "read RigidBody moving")
		check(out == in, "the moving branch round-trips with velocities intact")
	}

	// ---- Degenerate.schema: the degenerate arrangements (issue #203) ----
	//
	// Twelve shapes written back to back into ONE stream against the one
	// C++-pinned golden, in the C++ test's order. A fixed scalar array whose
	// elements this emitter placed TWICE is the defect these types exist to
	// catch, and it is invisible to a Go-to-Go round trip: only the byte
	// compare against another language's bytes names it.
	{
		vec2 := example.Vec2{X: 1.5, Y: -2.25}
		spanF64 := example.SpanF64{Values: [2]float64{3.5, -4.75}}
		spanU64 := example.SpanU64{Values: [2]uint64{0xDEADBEEFCAFEBABE, 1}}
		spanI64 := example.SpanI64{Values: [2]int64{-1234567890123, 42}}
		spanOne := example.SpanOne{Values: [1]uint64{0x0123456789ABCDEF}}
		spanChunk := example.SpanChunk{Values: [4]uint16{0x1111, 0x2222, 0x3333, 0x4444}}
		spanTail := example.SpanTail{Values: [2]float64{6.125, -7.0}, Tail: 0xFEEDFACE}
		spanTwice := example.SpanTwice{A: [2]float64{8.5, 9.5}, B: [2]float64{-10.5, -11.5}}
		trio := example.Trio{A: 0xABCDE, B: 0x12345, C: 0xFFFFF}
		trioSole := example.TrioSole{Inner: example.Trio{A: 1, B: 2, C: 3}}
		trioFirst := example.TrioFirst{Inner: example.Trio{A: 0xAAAAA, B: 0x55555, C: 0xF0F0F}, Trailer: 0xBEEF}
		straddle := example.TrioStraddle{
			Pad0:  0x0011223344556677,
			Pad1:  0x8899AABBCCDDEEFF,
			Pad2:  0xFFFFFFFFFFFFFFFF,
			Pad3:  0,
			Pad4:  0x123456789ABCDEF0,
			Pad5:  0xABCDEF,
			Inner: example.Trio{A: 0x11111, B: 0x22222, C: 0x33333},
		}

		ws, _ := newWriteStream()
		checkErr(example.WriteVec2(ws, &vec2), "write Vec2")
		checkErr(example.WriteSpanF64(ws, &spanF64), "write SpanF64")
		checkErr(example.WriteSpanU64(ws, &spanU64), "write SpanU64")
		checkErr(example.WriteSpanI64(ws, &spanI64), "write SpanI64")
		checkErr(example.WriteSpanOne(ws, &spanOne), "write SpanOne")
		checkErr(example.WriteSpanChunk(ws, &spanChunk), "write SpanChunk")
		checkErr(example.WriteSpanTail(ws, &spanTail), "write SpanTail")
		checkErr(example.WriteSpanTwice(ws, &spanTwice), "write SpanTwice")
		checkErr(example.WriteTrio(ws, &trio), "write Trio")
		checkErr(example.WriteTrioSole(ws, &trioSole), "write TrioSole")
		checkErr(example.WriteTrioFirst(ws, &trioFirst), "write TrioFirst")
		checkErr(example.WriteTrioStraddle(ws, &straddle), "write TrioStraddle")
		check(ws.BitsProcessed() == 128+128+128+128+64+64+160+256+64+64+80+408,
			"the twelve degenerate shapes ride their declared widths and nothing more")
		ws.Flush()
		goldenWire("degenerate", ws.Data())

		var rVec2 example.Vec2
		var rSpanF64 example.SpanF64
		var rSpanU64 example.SpanU64
		var rSpanI64 example.SpanI64
		var rSpanOne example.SpanOne
		var rSpanChunk example.SpanChunk
		var rSpanTail example.SpanTail
		var rSpanTwice example.SpanTwice
		var rTrio example.Trio
		var rTrioSole example.TrioSole
		var rTrioFirst example.TrioFirst
		var rStraddle example.TrioStraddle

		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadVec2(rs, &rVec2), "read Vec2")
		checkErr(example.ReadSpanF64(rs, &rSpanF64), "read SpanF64")
		checkErr(example.ReadSpanU64(rs, &rSpanU64), "read SpanU64")
		checkErr(example.ReadSpanI64(rs, &rSpanI64), "read SpanI64")
		checkErr(example.ReadSpanOne(rs, &rSpanOne), "read SpanOne")
		checkErr(example.ReadSpanChunk(rs, &rSpanChunk), "read SpanChunk")
		checkErr(example.ReadSpanTail(rs, &rSpanTail), "read SpanTail")
		checkErr(example.ReadSpanTwice(rs, &rSpanTwice), "read SpanTwice")
		checkErr(example.ReadTrio(rs, &rTrio), "read Trio")
		checkErr(example.ReadTrioSole(rs, &rTrioSole), "read TrioSole")
		checkErr(example.ReadTrioFirst(rs, &rTrioFirst), "read TrioFirst")
		checkErr(example.ReadTrioStraddle(rs, &rStraddle), "read TrioStraddle")

		check(rVec2 == vec2, "Vec2 round-trips")
		check(rSpanF64 == spanF64, "SpanF64 round-trips")
		check(rSpanU64 == spanU64, "SpanU64 round-trips")
		check(rSpanI64 == spanI64, "SpanI64 round-trips")
		check(rSpanOne == spanOne, "SpanOne round-trips")
		check(rSpanChunk == spanChunk, "SpanChunk round-trips")
		check(rSpanTail == spanTail, "SpanTail round-trips")
		check(rSpanTwice == spanTwice, "SpanTwice round-trips")
		check(rTrio == trio, "Trio round-trips")
		check(rTrioSole == trioSole, "TrioSole round-trips")
		check(rTrioFirst == trioFirst, "TrioFirst round-trips")
		check(rStraddle == straddle, "TrioStraddle round-trips")
	}

	// ---- Clauses.schema / Joins.schema: the mid-byte arrangements ----
	//
	// Degenerate.schema is every-type-a-whole-number-of-bytes by construction,
	// so no clause boundary in it lands mid-byte. These two units are chosen
	// so they do: at 13 bits a write clause takes four elements and a read
	// clause three, and both boundaries fall inside a byte. Each shape is
	// written to its OWN stream and flushed, and the golden is those
	// concatenated — the shapes are not byte-aligned, so a shared stream
	// would not equal the concatenation every emitter can produce.
	{
		var stream []byte
		emit := func(name string, bits int, w func(*serialize.WriteStream) error) {
			ws, _ := newWriteStream()
			checkErr(w(ws), "write "+name)
			check(ws.BitsProcessed() == int64(bits), name+" rides its declared width")
			ws.Flush()
			stream = append(stream, ws.Data()...)
		}
		off := 0
		consume := func(name string, bits int, r func(*serialize.ReadStream) error) {
			n := (bits + 7) / 8
			buf := make([]byte, n, n+8) // read allocations extend past the data
			copy(buf, stream[off:off+n])
			checkErr(r(serialize.NewReadStream(buf)), "read "+name)
			off += n
		}

		// ---- Clauses.schema ----

		w13Counts := []int{0, 1, 3, 4, 5, 7, 12}
		w13At := func(c int) example.W13 {
			v := example.W13{ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = uint16(8191 - i*733)
			}
			return v
		}
		for _, c := range w13Counts {
			v := w13At(c)
			emit("W13", 4+13*c, func(ws *serialize.WriteStream) error { return example.WriteW13(ws, &v) })
		}

		w17Counts := []int{0, 1, 2, 3, 4, 9}
		w17At := func(c int) example.W17 {
			v := example.W17{ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = uint32(131071 - i*11117)
			}
			return v
		}
		for _, c := range w17Counts {
			v := w17At(c)
			emit("W17", 4+17*c, func(ws *serialize.WriteStream) error { return example.WriteW17(ws, &v) })
		}

		w26Counts := []int{0, 1, 2, 3, 6}
		w26At := func(c int) example.W26 {
			v := example.W26{ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = uint32(67108863 - i*5555555)
			}
			return v
		}
		for _, c := range w26Counts {
			v := w26At(c)
			emit("W26", 3+26*c, func(ws *serialize.WriteStream) error { return example.WriteW26(ws, &v) })
		}

		w1Counts := []int{0, 1, 3, 4, 5, 20}
		w1At := func(c int) example.W1 {
			v := example.W1{ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = uint8(i % 2)
			}
			return v
		}
		for _, c := range w1Counts {
			v := w1At(c)
			emit("W1", 5+c, func(ws *serialize.WriteStream) error { return example.WriteW1(ws, &v) })
		}

		w52At := func(c int) example.W52 {
			v := example.W52{ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = 4503599627370495 - uint64(i)*123456789
			}
			return v
		}
		for c := 0; c <= 3; c++ {
			v := w52At(c)
			emit("W52", 2+52*c, func(ws *serialize.WriteStream) error { return example.WriteW52(ws, &v) })
		}

		w50At := func(c int) example.W50 {
			v := example.W50{ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = 1125899906842623 - uint64(i)*987654321
			}
			return v
		}
		for c := 0; c <= 3; c++ {
			v := w50At(c)
			emit("W50", 2+50*c, func(ws *serialize.WriteStream) error { return example.WriteW50(ws, &v) })
		}

		f13 := example.F13{}
		for i := 0; i < 7; i++ {
			f13.Items[i] = uint16(8191 - i*911)
		}
		emit("F13", 91, func(ws *serialize.WriteStream) error { return example.WriteF13(ws, &f13) })

		triCounts := []int{0, 1, 3, 4, 5, 10}
		triAt := func(c int) example.ArrTri3 {
			v := example.ArrTri3{ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = example.Tri3{A: uint32(i % 2), B: uint32(i % 4)}
			}
			return v
		}
		for _, c := range triCounts {
			v := triAt(c)
			emit("ArrTri3", 4+3*c, func(ws *serialize.WriteStream) error { return example.WriteArrTri3(ws, &v) })
		}

		arrEleven := example.ArrEleven{}
		for i := 0; i < 9; i++ {
			arrEleven.Items[i] = example.Eleven{A: uint32(i % 8), B: uint32(255 - i*17)}
		}
		emit("ArrEleven", 99, func(ws *serialize.WriteStream) error { return example.WriteArrEleven(ws, &arrEleven) })

		// lead 5 + tag 2 + tail 7 — a zero-bit arm behind a tag costs the tag
		emptyUnions := []example.HoldsEmptyUnion{
			{Lead: 21, Tail: 99},
			{Lead: 21, Tail: 99, U: example.EmptyUnion{Type: example.EmptyUnionTypeA}},
			{Lead: 21, Tail: 99, U: example.EmptyUnion{Type: example.EmptyUnionTypeB}},
		}
		for i := range emptyUnions {
			v := emptyUnions[i]
			emit("HoldsEmptyUnion", 14, func(ws *serialize.WriteStream) error { return example.WriteHoldsEmptyUnion(ws, &v) })
		}

		// lead 5 + s_length 4 = 9, the align pads 7 to 16; then 8*s bytes,
		// b_length 4, an align pad of 4, 8*b bytes and a 3-bit tail. The
		// 5-bit lead is what puts the align at a non-zero offset.
		strsEmpty := example.Strs{Lead: 21, Tail: 5}
		strsFull := example.Strs{Lead: 21, Tail: 5, SLength: 8, BLength: 8}
		copy(strsFull.S[:], "abcdefgh")
		for i := 0; i < 8; i++ {
			strsFull.B[i] = uint8(0xF0 + i)
		}
		strsPart := example.Strs{Lead: 21, Tail: 5, SLength: 3, BLength: 3}
		copy(strsPart.S[:], "xyz")
		for i := 0; i < 3; i++ {
			strsPart.B[i] = uint8(i + 1)
		}
		strs := []struct {
			v    example.Strs
			bits int
		}{{strsEmpty, 27}, {strsFull, 155}, {strsPart, 75}}
		for i := range strs {
			v := strs[i].v
			emit("Strs", strs[i].bits, func(ws *serialize.WriteStream) error { return example.WriteStrs(ws, &v) })
		}

		nestedAt := func(c int) example.ArrNested {
			v := example.ArrNested{Lead: 21, Tail: 5, ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				v.Items[i] = example.Eleven{A: uint32(i % 8), B: uint32(200 - i*7)}
			}
			return v
		}
		for c := 0; c <= 4; c++ {
			v := nestedAt(c)
			emit("ArrNested", 11+11*c, func(ws *serialize.WriteStream) error { return example.WriteArrNested(ws, &v) })
		}

		sole := example.Sole{Only: 5555}
		emit("Sole", 13, func(ws *serialize.WriteStream) error { return example.WriteSole(ws, &sole) })

		goldenWire("clauses", stream)

		// Read each shape back out of its own slice. A clause that decodes a
		// different number of elements than the writer encoded shows up here
		// even where the byte compare above happens to pass.
		for _, c := range w13Counts {
			var r example.W13
			consume("W13", 4+13*c, func(rs *serialize.ReadStream) error { return example.ReadW13(rs, &r) })
			check(r == w13At(c), "W13 round-trips")
		}
		for _, c := range w17Counts {
			var r example.W17
			consume("W17", 4+17*c, func(rs *serialize.ReadStream) error { return example.ReadW17(rs, &r) })
			check(r == w17At(c), "W17 round-trips")
		}
		for _, c := range w26Counts {
			var r example.W26
			consume("W26", 3+26*c, func(rs *serialize.ReadStream) error { return example.ReadW26(rs, &r) })
			check(r == w26At(c), "W26 round-trips")
		}
		for _, c := range w1Counts {
			var r example.W1
			consume("W1", 5+c, func(rs *serialize.ReadStream) error { return example.ReadW1(rs, &r) })
			check(r == w1At(c), "W1 round-trips")
		}
		for c := 0; c <= 3; c++ {
			var r example.W52
			consume("W52", 2+52*c, func(rs *serialize.ReadStream) error { return example.ReadW52(rs, &r) })
			check(r == w52At(c), "W52 round-trips")
		}
		for c := 0; c <= 3; c++ {
			var r example.W50
			consume("W50", 2+50*c, func(rs *serialize.ReadStream) error { return example.ReadW50(rs, &r) })
			check(r == w50At(c), "W50 round-trips")
		}
		{
			var r example.F13
			consume("F13", 91, func(rs *serialize.ReadStream) error { return example.ReadF13(rs, &r) })
			check(r == f13, "F13 round-trips")
		}
		for _, c := range triCounts {
			var r example.ArrTri3
			consume("ArrTri3", 4+3*c, func(rs *serialize.ReadStream) error { return example.ReadArrTri3(rs, &r) })
			check(r == triAt(c), "ArrTri3 round-trips")
		}
		{
			var r example.ArrEleven
			consume("ArrEleven", 99, func(rs *serialize.ReadStream) error { return example.ReadArrEleven(rs, &r) })
			check(r == arrEleven, "ArrEleven round-trips")
		}
		for i := range emptyUnions {
			var r example.HoldsEmptyUnion
			consume("HoldsEmptyUnion", 14, func(rs *serialize.ReadStream) error { return example.ReadHoldsEmptyUnion(rs, &r) })
			check(r == emptyUnions[i], "HoldsEmptyUnion round-trips")
		}
		for i := range strs {
			var r example.Strs
			consume("Strs", strs[i].bits, func(rs *serialize.ReadStream) error { return example.ReadStrs(rs, &r) })
			check(r == strs[i].v, "Strs round-trips")
		}
		for c := 0; c <= 4; c++ {
			var r example.ArrNested
			consume("ArrNested", 11+11*c, func(rs *serialize.ReadStream) error { return example.ReadArrNested(rs, &r) })
			check(r == nestedAt(c), "ArrNested round-trips")
		}
		{
			var r example.Sole
			consume("Sole", 13, func(rs *serialize.ReadStream) error { return example.ReadSole(rs, &r) })
			check(r == sole, "Sole round-trips")
		}
		check(off == len(stream), "the clauses reads consume the whole golden")

		// ---- Joins.schema ----
		//
		// Every branch is written on BOTH arms, so no path is pinned by
		// omission. The expected value after a round trip is not the value
		// written: the untaken side reads back as zero (SPEC §5).

		stream = nil
		off = 0

		type joinShape struct {
			name string
			bits int
			w    func(*serialize.WriteStream) error
			r    func(*serialize.ReadStream) error
			ok   func() bool
		}
		var shapes []joinShape

		for f := 0; f < 2; f++ {
			flag := f != 0
			// the arms agree on WIDTH but not on value, so a join that keeps
			// the wrong arm is a value mismatch and not just a width one
			agree := example.ArmsAgree{Lead: 21, Flag: flag, A: 1234, B: 1500, Tail: 99}
			var rAgree example.ArmsAgree
			shapes = append(shapes, joinShape{"ArmsAgree", 24,
				func(ws *serialize.WriteStream) error { return example.WriteArmsAgree(ws, &agree) },
				func(rs *serialize.ReadStream) error { return example.ReadArmsAgree(rs, &rAgree) },
				func() bool {
					want := example.ArmsAgree{Lead: 21, Flag: flag, Tail: 99}
					if flag {
						want.A = 1234
					} else {
						want.B = 1500
					}
					return rAgree == want
				}})

			disagree := example.ArmsDisagree{Lead: 21, Flag: flag, A: 1234, B: 5, Tail: 99}
			var rDisagree example.ArmsDisagree
			shapes = append(shapes, joinShape{"ArmsDisagree", map[bool]int{true: 24, false: 16}[flag],
				func(ws *serialize.WriteStream) error { return example.WriteArmsDisagree(ws, &disagree) },
				func(rs *serialize.ReadStream) error { return example.ReadArmsDisagree(rs, &rDisagree) },
				func() bool {
					want := example.ArmsDisagree{Lead: 21, Flag: flag, Tail: 99}
					if flag {
						want.A = 1234
					} else {
						want.B = 5
					}
					return rDisagree == want
				}})

			armEmpty := example.ArmEmpty{Lead: 21, Flag: flag, A: 456789, Tail: 99}
			var rArmEmpty example.ArmEmpty
			shapes = append(shapes, joinShape{"ArmEmpty", map[bool]int{true: 32, false: 13}[flag],
				func(ws *serialize.WriteStream) error { return example.WriteArmEmpty(ws, &armEmpty) },
				func(rs *serialize.ReadStream) error { return example.ReadArmEmpty(rs, &rArmEmpty) },
				func() bool {
					want := example.ArmEmpty{Lead: 21, Flag: flag, Tail: 99}
					if flag {
						want.A = 456789
					}
					return rArmEmpty == want
				}})

			alignStr := example.ArmAlign{Lead: 21, Flag: flag, SLength: 4, B: 1000, Tail: 99}
			copy(alignStr.S[:], "abcd")
			var rAlignStr example.ArmAlign
			shapes = append(shapes, joinShape{"ArmAlign", map[bool]int{true: 55, false: 23}[flag],
				func(ws *serialize.WriteStream) error { return example.WriteArmAlign(ws, &alignStr) },
				func(rs *serialize.ReadStream) error { return example.ReadArmAlign(rs, &rAlignStr) },
				func() bool {
					want := example.ArmAlign{Lead: 21, Flag: flag, Tail: 99}
					if flag {
						want.SLength = 4
						copy(want.S[:], "abcd")
					} else {
						want.B = 1000
					}
					return rAlignStr == want
				}})

			alignEmpty := example.ArmAlign{Lead: 21, Flag: flag, B: 1000, Tail: 99}
			var rAlignEmpty example.ArmAlign
			shapes = append(shapes, joinShape{"ArmAlignEmptyStr", 23,
				func(ws *serialize.WriteStream) error { return example.WriteArmAlign(ws, &alignEmpty) },
				func(rs *serialize.ReadStream) error { return example.ReadArmAlign(rs, &rAlignEmpty) },
				func() bool {
					want := example.ArmAlign{Lead: 21, Flag: flag, Tail: 99}
					if !flag {
						want.B = 1000
					}
					return rAlignEmpty == want
				}})
		}

		for o := 0; o < 2; o++ {
			for i := 0; i < 2; i++ {
				outer, inner := o != 0, i != 0
				bits := 23
				if outer {
					bits = 16
					if inner {
						bits = 40
					}
				}
				v := example.ArmsNested{Lead: 5, Outer: outer, Inner: inner, X: 500000000, Y: 17, Z: 4000, Tail: 33}
				var r example.ArmsNested
				shapes = append(shapes, joinShape{"ArmsNested", bits,
					func(ws *serialize.WriteStream) error { return example.WriteArmsNested(ws, &v) },
					func(rs *serialize.ReadStream) error { return example.ReadArmsNested(rs, &r) },
					func() bool {
						want := example.ArmsNested{Lead: 5, Outer: outer, Tail: 33}
						if outer {
							want.Inner = inner
							if inner {
								want.X = 500000000
							} else {
								want.Y = 17
							}
						} else {
							want.Z = 4000
						}
						return r == want
					}})
			}
		}

		for f := 0; f < 2; f++ {
			for c := 0; c <= 3; c++ {
				flag, count := f != 0, c
				bits := 22
				if flag {
					bits = 15 + 13*count
				}
				v := example.ArmArray{Lead: 21, Flag: flag, ItemsCount: int32(count), B: 300, Tail: 99}
				for i := 0; i < count; i++ {
					v.Items[i] = uint16(8191 - i*777)
				}
				var r example.ArmArray
				shapes = append(shapes, joinShape{"ArmArray", bits,
					func(ws *serialize.WriteStream) error { return example.WriteArmArray(ws, &v) },
					func(rs *serialize.ReadStream) error { return example.ReadArmArray(rs, &r) },
					func() bool {
						want := example.ArmArray{Lead: 21, Flag: flag, Tail: 99}
						if flag {
							want.ItemsCount = int32(count)
							for i := 0; i < count; i++ {
								want.Items[i] = uint16(8191 - i*777)
							}
						} else {
							want.B = 300
						}
						return r == want
					}})
			}
		}

		// lead 5 + tag 2 + arm + tail 11 — the arms are 0, 3 and 37 bits
		unevens := []struct {
			v    example.HoldsUneven
			bits int
		}{
			{example.HoldsUneven{Lead: 21, Tail: 1500}, 18},
			{example.HoldsUneven{Lead: 21, Tail: 1500, U: example.Uneven{Type: example.UnevenTypeNarrow, Narrow: example.Narrow{N: 5}}}, 21},
			{example.HoldsUneven{Lead: 21, Tail: 1500, U: example.Uneven{Type: example.UnevenTypeWide, Wide: example.Wide{W: 123456789012}}}, 55},
		}
		for i := range unevens {
			v := unevens[i].v
			var r example.HoldsUneven
			want := unevens[i].v
			shapes = append(shapes, joinShape{"HoldsUneven", unevens[i].bits,
				func(ws *serialize.WriteStream) error { return example.WriteHoldsUneven(ws, &v) },
				func(rs *serialize.ReadStream) error { return example.ReadHoldsUneven(rs, &r) },
				func() bool { return r == want }})
		}

		// alternating arms: item i is Narrow (2 + 3) when even, Wide (2 + 37)
		unevenItemBits := []int{0, 5, 44, 49}
		arrUnevenAt := func(c int) example.ArrUneven {
			v := example.ArrUneven{Lead: 21, Tail: 5, ItemsCount: int32(c)}
			for i := 0; i < c; i++ {
				if i%2 == 0 {
					v.Items[i] = example.Uneven{Type: example.UnevenTypeNarrow, Narrow: example.Narrow{N: uint32(i % 8)}}
				} else {
					v.Items[i] = example.Uneven{Type: example.UnevenTypeWide, Wide: example.Wide{W: 99887766554 + uint64(i)}}
				}
			}
			return v
		}
		for c := 0; c <= 3; c++ {
			count := c
			v := arrUnevenAt(count)
			var r example.ArrUneven
			shapes = append(shapes, joinShape{"ArrUneven", 10 + unevenItemBits[count],
				func(ws *serialize.WriteStream) error { return example.WriteArrUneven(ws, &v) },
				func(rs *serialize.ReadStream) error { return example.ReadArrUneven(rs, &r) },
				func() bool { return r == arrUnevenAt(count) }})
		}

		// lead 5 + count 2 + 13*c + s_length 3, an align to the byte, 8*s,
		// then a 32 + 29 + 19 + 4 static run after the align regains it
		regainAt := func(c, sl int) example.RegainAfterAlign {
			v := example.RegainAfterAlign{Lead: 21, ItemsCount: int32(c), SLength: int32(sl),
				P: 0xDEADBEEF, Q: (1 << 29) - 7, R: (1 << 19) - 3, Tail: 9}
			if sl != 0 {
				copy(v.S[:], "wxyz")
			}
			for i := 0; i < c; i++ {
				v.Items[i] = uint16(8191 - i*999)
			}
			return v
		}
		for c := 0; c <= 3; c++ {
			for sl := 0; sl <= 4; sl += 4 {
				count, slen := c, sl
				v := regainAt(count, slen)
				var r example.RegainAfterAlign
				afterAlign := ((5 + 2 + 13*count + 3) + 7) / 8 * 8
				shapes = append(shapes, joinShape{"RegainAfterAlign", afterAlign + 8*slen + 84,
					func(ws *serialize.WriteStream) error { return example.WriteRegainAfterAlign(ws, &v) },
					func(rs *serialize.ReadStream) error { return example.ReadRegainAfterAlign(rs, &r) },
					func() bool { return r == regainAt(count, slen) }})
			}
		}

		for _, s := range shapes {
			emit(s.name, s.bits, s.w)
		}
		goldenWire("joins", stream)
		for _, s := range shapes {
			consume(s.name, s.bits, s.r)
			check(s.ok(), s.name+" round-trips (untaken sides zeroed, SPEC §5)")
		}
		check(off == len(stream), "the joins reads consume the whole golden")
	}

	if failed {
		os.Exit(1)
	}
	fmt.Printf("OK\n")
}

// testDataInstance is the deterministic TestData the C++ test pins — the
// values must stay mirrored on both sides.
func testDataInstance() example.TestData {
	in := example.TestData{}
	in.A = -100
	in.B = 100
	in.C = 149
	in.D = 0x11
	in.E = 0x22
	in.F = 0x33
	in.G = true
	in.ItemsCount = 3
	in.Items[0] = 0
	in.Items[1] = 128
	in.Items[2] = 255
	in.FloatValue = 3.1415926
	in.CompressedFloatValue = 2.5
	in.DoubleValue = 1.0 / 3.0
	in.Int8Value = -128
	in.Int16Value = -32768
	in.Uint8Value = 255
	in.Uint16Value = 65535
	in.Uint32Value = 4294967295
	in.Uint64Value = 18446744073709551615
	in.Int64Full = -9223372036854775808
	in.Int64Range = -999999999999
	for i := range in.FixedBytes {
		in.FixedBytes[i] = uint8(i * 3)
	}
	copy(in.Text[:], "the quick brown fox")
	in.TextLength = 19
	return in
}
