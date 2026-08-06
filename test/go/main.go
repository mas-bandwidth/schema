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

// foreignMessage satisfies the Message interface from outside the generated
// set — WriteMessage must refuse it without touching the stream.
type foreignMessage struct{}

func (foreignMessage) MessageType() example.MessageType { return example.MessageTypeChat }

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

	// ---- the Message dispatch surface: interface + type switch ----
	{
		chat := &example.Chat{}
		copy(chat.Text[:], "dispatch")
		chat.TextLength = 8
		test := &example.Test{TestB: 42}

		ws, _ := newWriteStream()
		checkErr(example.WriteMessage(ws, chat), "write Message chat")
		checkErr(example.WriteMessage(ws, test), "write Message test")
		checkErr(example.WriteMessage(ws, nil), "write Message terminator")
		ws.Flush()
		goldenWire("message_stream", ws.Data())

		// reads land in pre-allocated storage — no heap per message (SPEC §6.1);
		// the returned Message points into it, the union's own discipline
		storage := example.MessageStorage{}
		rs := serialize.NewReadStream(ws.Data())
		m1, err := example.ReadMessage(rs, &storage)
		checkErr(err, "read message 1")
		c, ok := m1.(*example.Chat)
		check(ok && c.TextLength == 8 && string(c.Text[:8]) == "dispatch", "message 1 is the chat")
		check(c == &storage.Chat, "the read message points into the caller's storage")
		m2, err := example.ReadMessage(rs, &storage)
		checkErr(err, "read message 2")
		t, ok := m2.(*example.Test)
		check(ok && t.TestB == 42, "message 2 is the test")
		m3, err := example.ReadMessage(rs, &storage)
		checkErr(err, "read message 3")
		check(m3 == nil, "message 3 is the None terminator")

		// the tag pair stands alone too
		ws2, _ := newWriteStream()
		checkErr(example.WriteMessageType(ws2, example.MessageTypeChat), "write message type")
		checkErr(example.WriteMessageType(ws2, example.MessageTypeNone), "write message type terminator")
		ws2.Flush()
		rs2 := serialize.NewReadStream(ws2.Data())
		tag := example.MessageTypeNone
		checkErr(example.ReadMessageType(rs2, &tag), "read message type")
		check(tag == example.MessageTypeChat, "tag round-trips")
		checkErr(example.ReadMessageType(rs2, &tag), "read message type terminator")
		check(tag == example.MessageTypeNone, "terminator tag round-trips")

		// a foreign Message implementation writes NOTHING — the stream cannot
		// be left with a tag and no payload (a desync), and the refusal is loud
		ws3, _ := newWriteStream()
		err = example.WriteMessage(ws3, foreignMessage{})
		check(err != nil, "a foreign Message implementation is refused")
		ws3.Flush()
		check(ws3.BytesProcessed() == 0, "and nothing was written")

		// a typed nil inside the interface is refused the same way — no tag,
		// no panic, nothing on the stream
		ws4, _ := newWriteStream()
		var nilChat *example.Chat
		err = example.WriteMessage(ws4, nilChat)
		check(err != nil, "a typed-nil message is refused")
		ws4.Flush()
		check(ws4.BytesProcessed() == 0, "and nothing was written for the typed nil")
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

	// ---- the object views: Quantize/Unquantize and the shallow wire ----
	{
		interp := example.ShipData_Interpolate{}
		interp.ShipType = example.ShipTypeCorvette
		interp.Position = example.Vec3{X: 1.5, Y: -2.25, Z: 100.0}
		interp.Rotation = example.Quat{X: 0.0, Y: 0.0, Z: 0.0, W: 1.0}
		interp.LinearVelocity = example.Vec3{X: 3.0, Y: 0.0, Z: -1.0}
		interp.Flags = example.ShipFlagsBoosting
		interp.Team = example.TeamRed
		interp.Health = 750 // wire-int domain (rule 5)
		interp.Thrust = 55

		q := example.ShipData_Shallow{}
		example.QuantizeShip(&interp, &q)
		check(q.PositionX == 1536, "1.5 * 1024 quantizes to 1536")
		check(q.PositionY == -2304, "-2.25 * 1024 quantizes to -2304")
		check(q.RotationW == 1024, "1.0 * 1024 quantizes to 1024")
		check(q.Health == 750 && q.Thrust == 55, "projected fields copy")
		check(q.Team == example.TeamRed && q.Flags == example.ShipFlagsBoosting, "discrete fields copy")

		ws, _ := newWriteStream()
		checkErr(example.WriteShipData_Shallow(ws, &q), "write ShipData_Shallow")
		ws.Flush()
		goldenWire("ship_shallow", ws.Data())

		q2 := example.ShipData_Shallow{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadShipData_Shallow(rs, &q2), "read ShipData_Shallow")
		check(q2 == q, "the shallow wire round-trips")

		back := example.ShipData_Interpolate{}
		example.UnquantizeShip(&q2, &back)
		check(back.Position.X == 1536.0/1024.0, "unquantize recovers x")
		check(back.Position.Y == -2304.0/1024.0, "unquantize recovers y")
		check(back.Rotation.W == 1.0, "unquantize recovers w")
		check(back.Health == 750 && back.Team == example.TeamRed, "discrete and projected copy back")
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

	// ---- ProbeReport: message-as-field, and the widened flags wire ----
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
		check(out == in, "ProbeReport round-trips — a message as an ordinary field")

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

	// ---- Missile: a second object family end to end ----
	{
		interp := example.MissileData_Interpolate{}
		interp.MissileType = example.MissileTypeTorpedo
		interp.Position = example.Vec3{X: -4.0, Y: 8.0, Z: 15.5}
		interp.Rotation = example.Quat{X: 0.0, Y: 0.0, Z: 0.0, W: 1.0}
		interp.LinearVelocity = example.Vec3{X: 1.0, Y: 2.0, Z: 3.0}
		interp.Team = example.TeamBlue
		interp.Flags = 0xF00F

		q := example.MissileData_Shallow{}
		example.QuantizeMissile(&interp, &q)
		check(q.PositionZ == 15872, "15.5 * 1024 quantizes to 15872")
		check(q.RotationW == 1024 && q.Team == example.TeamBlue && q.Flags == 0xF00F, "discrete fields copy")

		ws, _ := newWriteStream()
		checkErr(example.WriteMissileData_Shallow(ws, &q), "write MissileData_Shallow")
		ws.Flush()
		q2 := example.MissileData_Shallow{}
		rs := serialize.NewReadStream(ws.Data())
		checkErr(example.ReadMissileData_Shallow(rs, &q2), "read MissileData_Shallow")
		check(q2 == q, "the missile shallow wire round-trips")

		back := example.MissileData_Interpolate{}
		example.UnquantizeMissile(&q2, &back)
		check(back.Position.Z == 15872.0/1024.0, "unquantize recovers z")
	}

	// ---- the object tag pair ----
	{
		ws, _ := newWriteStream()
		checkErr(example.WriteObjectType(ws, example.ObjectTypeTurret), "write object type")
		checkErr(example.WriteObjectType(ws, example.ObjectTypeNone), "write object type sentinel")
		ws.Flush()
		rs := serialize.NewReadStream(ws.Data())
		tag := example.ObjectTypeNone
		checkErr(example.ReadObjectType(rs, &tag), "read object type")
		check(tag == example.ObjectTypeTurret, "object tag round-trips")
		checkErr(example.ReadObjectType(rs, &tag), "read object type sentinel")
		check(tag == example.ObjectTypeNone, "the None sentinel round-trips")
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
