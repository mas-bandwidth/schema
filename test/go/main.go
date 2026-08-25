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
