// schema bench — the Go runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated Go package and the serialize.go runtime: same benchmark set,
// same pinned corpus instances, same LCG and vary-function field mappings,
// same golden + round-trip self-checks (a mismatch REFUSES to bench), same
// warmup + 7 measured runs + median/min/max/spread, same CSV row format with
// lang=go. See bench/README.md for the runner contract.
//
// Language-specific discipline:
//   - escape barriers: a package-level sink accumulator plus runtime.KeepAlive
//     on the decoded value (the stub's sanctioned equivalents of the C++
//     empty-asm clobber)
//   - streams are reused via Reset (the runtime's documented no-allocation
//     reuse path) — the Go equivalent of C++'s free stack construction
//   - the driver passes write/read/vary as function values (one indirect call
//     per op, same as the Go way of writing this loop; Rust and C++ get this
//     inlined via generics — noted in the results)
//
// Run from bench/go (run.sh does): the wire goldens are at ../../testdata/wire.
package main

import (
	"bytes"
	"fmt"
	"os"
	"runtime"
	"sort"
	"time"

	"example"

	"github.com/mas-bandwidth/serialize.go"
)

const NumRuns = 7      // median of 7 (N >= 5), after 1 warmup run
const NumVariants = 64 // read-path variant buffers

// buffers: write buffers must be a multiple of 8 bytes (qword-flush contract);
// variant arrays keep >= 7 bytes of backing slack past the packet for the
// reader's window loads. 4096 covers MessageMaxBytes (2008) with slack.
const BufferSize = 4096

var gBuffer [BufferSize]byte
var gTwin [BufferSize]byte
var gVariants [NumVariants][BufferSize]byte

var gSink uint64 // defeats dead code elimination of computed values
var gCsv = false
var gWireDir = "../../testdata/wire"
var failed = false

// benchRng is the LCG every runner must use (Knuth MMIX, as in serialize bench.cpp).
func benchRng(rng uint64) uint64 {
	return rng*6364136223846793005 + 1442695040888963407
}

func fail(name, what string) {
	fmt.Fprintf(os.Stderr, "FAILED: %s: %s\n", name, what)
	failed = true
}

func checkGolden(name string, data []byte) bool {
	path := gWireDir + "/" + name + ".bin"
	expected, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "missing wire golden %s — run from bench/go (or pass --wire-dir)\n", path)
		return false
	}
	if !bytes.Equal(expected, data) {
		fmt.Fprintf(os.Stderr, "WIRE GOLDEN MISMATCH: %s (%d golden vs %d actual bytes) — refusing to bench code that does not match the corpus\n",
			name, len(expected), len(data))
		return false
	}
	return true
}

type runStats struct {
	median float64 // ops/sec
	min    float64
	max    float64
	spread float64 // (max - min) / median * 100
}

func stats(rates []float64) runStats {
	sort.Float64s(rates)
	n := len(rates)
	return runStats{
		median: rates[n/2],
		min:    rates[0],
		max:    rates[n-1],
		spread: (rates[n-1] - rates[0]) / rates[n/2] * 100.0,
	}
}

func report(bench, path string, iters int64, bytesPerOp int64, s runStats) {
	mbps := s.median * float64(bytesPerOp) / (1024.0 * 1024.0)
	fmt.Fprintf(os.Stderr, "%-18s %-5s %10.2f M msg/s %10.1f MB/s   (min %.2f, max %.2f, spread %.1f%%)\n",
		bench, path, s.median/1e6, mbps, s.min/1e6, s.max/1e6, s.spread)
	if gCsv {
		fmt.Printf("go,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f\n",
			bench, path, iters, bytesPerOp, NumRuns, s.median, s.min, s.max, mbps, s.spread)
	}
}

// ------------------------------------------------------------------------------------------
// the per-message benchmark driver
// ------------------------------------------------------------------------------------------

func benchMessage[T any](name, golden string, iters int64, pinned T,
	writeFn func(*serialize.WriteStream, *T) error,
	readFn func(*serialize.ReadStream, *T) error,
	varyFn func(*T, uint64)) {

	// self-check 1: the pinned instance matches its wire golden byte-for-byte
	base := pinned
	ws := serialize.NewWriteStream(gBuffer[:])
	if err := writeFn(ws, &base); err != nil {
		fail(name, "write of pinned instance failed")
		return
	}
	ws.Flush()
	bytesPerOp := ws.BytesProcessed()
	if golden != "" && !checkGolden(golden, gBuffer[:bytesPerOp]) {
		failed = true
		return
	}

	// self-check 2: round-trip write -> read -> re-write -> identical bytes
	{
		var out T
		rs := serialize.NewReadStream(gBuffer[:bytesPerOp])
		if err := readFn(rs, &out); err != nil {
			fail(name, "read of pinned instance failed")
			return
		}
		tws := serialize.NewWriteStream(gTwin[:])
		if err := writeFn(tws, &out); err != nil {
			fail(name, "re-write of decoded instance failed")
			return
		}
		tws.Flush()
		if tws.BytesProcessed() != bytesPerOp ||
			!bytes.Equal(gBuffer[:bytesPerOp], gTwin[:bytesPerOp]) {
			fail(name, "round-trip bytes differ")
			return
		}
	}

	// variant buffers for the read path (and proof that variation keeps bytes/op constant)
	rng := uint64(1)
	for k := 0; k < NumVariants; k++ {
		rng = benchRng(rng)
		varyFn(&base, rng)
		vs := serialize.NewWriteStream(gVariants[k][:])
		if err := writeFn(vs, &base); err != nil {
			fail(name, "write of varied instance failed")
			return
		}
		vs.Flush()
		if vs.BytesProcessed() != bytesPerOp {
			fail(name, "variation changed bytes/op — vary must keep structure fields fixed")
			return
		}
	}

	writeRates := make([]float64, NumRuns)
	readRates := make([]float64, NumRuns)

	// write path: 1 warmup + NumRuns measured (stream reused via Reset — the
	// runtime's documented no-allocation path)
	for run := -1; run < NumRuns; run++ {
		start := time.Now()
		for i := int64(0); i < iters; i++ {
			rng = benchRng(rng)
			varyFn(&base, rng)
			ws.Reset(gBuffer[:])
			if err := writeFn(ws, &base); err != nil {
				fail(name, "write failed in loop")
				return
			}
			ws.Flush()
			gSink = gSink + uint64(ws.BytesProcessed())
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			writeRates[run] = float64(iters) / elapsed
		}
	}
	runtime.KeepAlive(gBuffer[:])

	// read path: 1 warmup + NumRuns measured; ONE decode instance hoisted out
	// of the loop and reused, matching the write loop's hoisted base — `var
	// out T` per iteration escapes through the opaque readFn value and is
	// heap-allocated + zeroed every message (harness overhead, not serialize
	// work; verified by profile: mallocgc+GC ~27% cum of the v1 read path).
	// Every field a read decodes is overwritten every iteration; structure
	// fields are fixed across variants, so reuse decodes identically.
	var out T
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for run := -1; run < NumRuns; run++ {
		start := time.Now()
		for i := int64(0); i < iters; i++ {
			rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
			if err := readFn(rs, &out); err != nil {
				fail(name, "read failed in loop")
				return
			}
			runtime.KeepAlive(&out) // every decoded field is observed
			gSink = gSink + 1
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			readRates[run] = float64(iters) / elapsed
		}
	}

	report(name, "write", iters, bytesPerOp, stats(writeRates))
	report(name, "read", iters, bytesPerOp, stats(readRates))

	// alloc note (proof of the reuse discipline, not a benchmark): allocs
	// during one extra untimed pass of each path — must be 0
	const allocOps = 4 * NumVariants
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := int64(0); i < allocOps; i++ {
		rng = benchRng(rng)
		varyFn(&base, rng)
		ws.Reset(gBuffer[:])
		if err := writeFn(ws, &base); err != nil {
			fail(name, "write failed in alloc pass")
			return
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	runtime.ReadMemStats(&after)
	writeAllocs := after.Mallocs - before.Mallocs
	runtime.ReadMemStats(&before)
	for i := int64(0); i < allocOps; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if err := readFn(rs, &out); err != nil {
			fail(name, "read failed in alloc pass")
			return
		}
		gSink = gSink + 1
	}
	runtime.ReadMemStats(&after)
	readAllocs := after.Mallocs - before.Mallocs
	fmt.Fprintf(os.Stderr, "alloc note: %s one pass (%d ops/path): write %d allocs, read %d allocs\n",
		name, allocOps, writeAllocs, readAllocs)
}

// ------------------------------------------------------------------------------------------
// pinned corpus instances — the C++ reference's pin_* functions exactly
// (the same instances test/go/main.go pins to the wire goldens)
// ------------------------------------------------------------------------------------------

func pinRigidBodyMoving() example.RigidBody {
	var in example.RigidBody
	in.Position = example.Vec3{X: 1.5, Y: -2.5, Z: 3.25}
	in.Orientation = example.Quat{X: 0.1, Y: 0.2, Z: 0.3, W: 0.9}
	in.AtRest = false
	in.LinearVelocity = example.Vec3{X: 10.0, Y: 20.0, Z: -3.0}
	in.AngularVelocity = example.Vec3{X: 0.25, Y: 0.5, Z: 0.75}
	return in
}

func pinChat() example.Chat {
	var in example.Chat
	copy(in.Text[:], "wire parity")
	in.TextLength = 11
	return in
}

func pinInputPacket() example.InputPacket {
	var in example.InputPacket
	in.SynchronizeSequence = 7
	in.CurrentFrame = 123456789
	in.StartFrame = 123456780
	in.InputsCount = 2
	in.Inputs[0].Throttle = 0.5
	in.Inputs[0].Fire = true
	in.Inputs[1].StickX = -0.25
	in.Inputs[1].Boost = true
	return in
}

func pinShipCreate() example.ShipCreate {
	var in example.ShipCreate
	in.ShipType = example.ShipTypeBomber
	in.Position = example.QuantizedPosition{X: 1000, Y: -2000, Z: 3000}
	in.HasFlags = true
	in.Flags = example.ShipFlagsBoosting | example.ShipFlagsAiming
	in.Team = example.TeamBlue
	in.Health = 750
	in.Thrust = 55
	return in
}

func pinShipShallow() example.ShipData_Shallow {
	var interp example.ShipData_Interpolate
	interp.ShipType = example.ShipTypeCorvette
	interp.Position = example.Vec3{X: 1.5, Y: -2.25, Z: 100.0}
	interp.Rotation = example.Quat{X: 0.0, Y: 0.0, Z: 0.0, W: 1.0}
	interp.LinearVelocity = example.Vec3{X: 3.0, Y: 0.0, Z: -1.0}
	interp.Flags = example.ShipFlagsBoosting
	interp.Team = example.TeamRed
	interp.Health = 750
	interp.Thrust = 55
	var q example.ShipData_Shallow
	example.QuantizeShip(&interp, &q)
	return q
}

func pinProbeHeader() example.ProbeHeader {
	var h example.ProbeHeader
	h.Version = 5
	h.ProbeId = 0x1122334455667788
	return h
}

func pinProbeBits() example.ProbeBits {
	var in example.ProbeBits
	in.Small = 0x1FF
	in.Boundary = 0x1FFFFFFFF
	in.Wide = 0xFEDCBA9876543210
	in.Sensor = 4294967295
	in.Nonce = 18446744073709551615
	return in
}

func pinProbeArray() example.ProbeArray {
	in := example.NewProbeArray() // defaults: samples active, retries -1 — as the C++ default ctor
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
	return in
}

func pinTestData() example.TestData {
	var in example.TestData
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
	for i := 0; i < 17; i++ {
		in.FixedBytes[i] = uint8(i * 3)
	}
	copy(in.Text[:], "the quick brown fox")
	in.TextLength = 19
	return in
}

// ------------------------------------------------------------------------------------------
// vary functions — the C++ reference's vary_* field mappings exactly: VALUE
// fields mutate within wire ranges through the LCG; structure fields (counts,
// lengths, branch bools) stay fixed so bytes/op is constant.
// ------------------------------------------------------------------------------------------

func varyRigidBody(m *example.RigidBody, rng uint64) {
	m.Position.X = float64(int64(rng>>8)&0xFFFF) * 0.25
	m.Position.Y = float64(int64(rng>>16)&0xFFFF) * 0.5
	m.Position.Z = float64(int64(rng>>24)&0xFFFF) * 0.125
	m.Orientation.X = float64(int64(rng)&0xFF) * 0.001
	m.LinearVelocity.X = float64(int64(rng>>32)&0xFFF) * 0.25
	m.AngularVelocity.Z = float64(int64(rng>>40)&0xFFF) * 0.125
}

func varyRigidBodyAtRest(m *example.RigidBody, rng uint64) {
	m.Position.X = float64(int64(rng>>8)&0xFFFF) * 0.25
	m.Position.Y = float64(int64(rng>>16)&0xFFFF) * 0.5
	m.Orientation.X = float64(int64(rng)&0xFF) * 0.001
}

func varyChat(m *example.Chat, rng uint64) {
	for i := int32(0); i < m.TextLength; i++ {
		m.Text[i] = byte('a' + ((rng >> (i & 7)) & 15)) // never zero
	}
}

func varyTest(m *example.Test, rng uint64) {
	m.TestA = uint16(rng)
	m.TestB = int16((rng >> 16) & 511) // within [0, 1000]
	m.TestC = int16((rng >> 25) & 511)
	m.TestD = int16((rng >> 34) & 511)
}

func varyInputPacket(m *example.InputPacket, rng uint64) {
	m.SynchronizeSequence = uint16(rng)
	m.CurrentFrame = rng
	m.StartFrame = rng >> 1
	m.Inputs[0].Throttle = float32(uint32(rng)&0xFF) / 256.0
	m.Inputs[0].Fire = (rng & 1) != 0
	m.Inputs[1].StickX = float32(uint32(rng>>8)&0xFF)/256.0 - 0.5
	m.Inputs[1].Boost = (rng & 2) != 0
}

func varyShipCreate(m *example.ShipCreate, rng uint64) {
	m.Position.X = int32((rng>>8)&0xFFFFF) - 0x80000 // within [-8388608, 8388608]
	m.Position.Y = int32((rng>>16)&0xFFFFF) - 0x80000
	m.Position.Z = int32((rng>>24)&0xFFFFF) - 0x80000
	m.Rotation.X = int16(int32(rng&0x7FF) - 1024) // within [-1024, 1024]
	m.LinearVelocity.X = int32((rng>>32)&0x3FFFFF) - 2097152
	m.Flags = example.ShipFlags(rng & 15) // 4 wire bits, has_flags stays true
	m.Health = int16((rng >> 5) & 511)    // within [0, 1000]
	m.Thrust = int8((rng >> 14) & 63)     // within [0, 100]
}

func varyShipShallow(m *example.ShipData_Shallow, rng uint64) {
	m.PositionX = int32((rng>>8)&0xFFFFF) - 0x80000
	m.PositionY = int32((rng>>16)&0xFFFFF) - 0x80000
	m.PositionZ = int32((rng>>24)&0xFFFFF) - 0x80000
	m.RotationX = int16(int32(rng&0x7FF) - 1024)
	m.RotationW = int16(int32((rng>>11)&0x7FF) - 1024)
	m.LinearVelocityX = int32((rng>>32)&0x3FFFFF) - 2097152
	m.Flags = example.ShipFlags(rng & 15)
	m.Health = uint16((rng >> 5) & 511)
	m.Thrust = uint8((rng >> 14) & 63)
}

func varyProbeHeader(m *example.ProbeHeader, rng uint64) {
	m.Version = uint32(rng) & 7 // 3 wire bits
	m.ProbeId = rng
}

func varyProbeBits(m *example.ProbeBits, rng uint64) {
	m.Small = uint32(rng) & 511          // 9 bits
	m.Boundary = rng & ((1 << 33) - 1)   // 33 bits
	m.Wide = rng * 3
	m.Sensor = uint32(rng >> 16)
	m.Nonce = rng ^ 0x5555555555555555
}

func varyProbeArray(m *example.ProbeArray, rng uint64) {
	m.Samples[0].Orientation = -180.0 + float32(uint32(rng)&0x3FFF)*0.02
	m.Samples[0].RawDelta = int32(uint32(rng >> 8))
	m.Samples[0].BigDelta = int64(rng * 5)
	m.Samples[0].TargetId = uint16(rng >> 24)
	m.Samples[0].Samples[0] = uint16(rng >> 40)
	m.Samples[1].Orientation = -180.0 + float32(uint32(rng>>3)&0x3FFF)*0.02
	m.Samples[1].IdleTicks = uint32(rng >> 32)
	m.Samples[1].Samples[0] = uint16(rng >> 4)
	m.Samples[1].Samples[1] = uint16(rng >> 12)
	m.Config.Retries = int32(uint32(rng >> 20))
}

func varyTestData(m *example.TestData, rng uint64) {
	m.A = int32(rng&127) - 64 // within [-100, 100]
	m.B = int32((rng>>7)&127) - 64
	m.C = int32((rng>>14)&127) - 64 // within [-100, 150]
	m.D = uint32(rng) & 255
	m.E = uint32(rng>>8) & 255
	m.F = uint32(rng>>16) & 255
	m.Items[0] = int32(rng & 255) // items_count stays 3
	m.Items[1] = int32((rng >> 8) & 255)
	m.Items[2] = int32((rng >> 16) & 255)
	m.FloatValue = float32(uint32(rng) & 0xFFFF)
	m.CompressedFloatValue = float32(uint32(rng)&1023) * 0.005 // within [0, 10] (max 5.115)
	m.DoubleValue = float64(int64(rng>>16)&0xFFFFFF) * 0.5
	m.Int8Value = int8(rng)
	m.Int16Value = int16(rng >> 8)
	m.Uint8Value = uint8(rng >> 16)
	m.Uint16Value = uint16(rng >> 24)
	m.Uint32Value = uint32(rng >> 32)
	m.Uint64Value = rng * 7
	m.Int64Full = int64(rng * 11)
	m.Int64Range = int64((rng>>24)&((1<<37)-1)) - (1 << 36) // within +/- 1e12
	m.FixedBytes[0] = uint8(rng)
	m.FixedBytes[16] = uint8(rng >> 8)
	for i := int32(0); i < m.TextLength; i++ {
		m.Text[i] = byte('a' + ((rng >> (i & 7)) & 15)) // never zero
	}
}

// ------------------------------------------------------------------------------------------
// the synthetic steady-state batch: NumBatchMessages messages through the
// Message dispatch surface (WriteMessage/ReadMessage) plus the None
// terminator, mixed types driven by the LCG — the C++ batch builder exactly.
// ------------------------------------------------------------------------------------------

const NumBatchMessages = 4096
const BatchPasses = 800

func buildBatch(batchBuffer []byte) ([]example.Message, int64) {
	messages := make([]example.Message, NumBatchMessages)

	rng := uint64(12345)
	for k := 0; k < NumBatchMessages; k++ {
		rng = benchRng(rng)
		pick := int((rng >> 32) % 20)
		switch {
		case pick < 5: // 25% Chat
			m := &example.Chat{}
			m.TextLength = 16 + int32(rng&15)
			for i := int32(0); i < m.TextLength; i++ {
				m.Text[i] = byte('a' + ((rng >> (i & 7)) & 15))
			}
			messages[k] = m
		case pick < 10: // 25% Test
			m := &example.Test{}
			m.TestA = uint16(rng)
			m.TestB = int16((rng >> 16) & 511)
			m.TestC = int16((rng >> 25) & 511)
			m.TestD = int16((rng >> 34) & 511)
			messages[k] = m
		case pick < 13: // 15% Synchronize
			m := &example.Synchronize{}
			m.SyncFrame = rng
			m.SyncSequence = uint16(rng >> 8)
			messages[k] = m
		case pick < 16: // 15% Timescale
			m := &example.Timescale{}
			m.Scale = float64(uint32(rng)&0xFFFF) / 65536.0
			m.FrameA = uint32(rng >> 16)
			m.FrameB = uint32(rng >> 24)
			messages[k] = m
		case pick < 18: // 10% Heartbeat
			messages[k] = &example.Heartbeat{}
		default: // 10% Block
			m := &example.Block{}
			m.DataLength = 64 + int32(rng&127)
			for i := int32(0); i < m.DataLength; i++ {
				m.Data[i] = uint8(rng >> (uint(i) & 31))
			}
			messages[k] = m
		}
	}

	ws := serialize.NewWriteStream(batchBuffer)
	for k := 0; k < NumBatchMessages; k++ {
		if err := example.WriteMessage(ws, messages[k]); err != nil {
			fail("message_batch", "batch write failed during setup")
			return nil, 0
		}
	}
	if err := example.WriteMessage(ws, nil); err != nil { // nil is the None terminator
		fail("message_batch", "terminator write failed during setup")
		return nil, 0
	}
	ws.Flush()
	return messages, ws.BytesProcessed()
}

func benchBatch() {
	// worst case is NumBatchMessages * MessageMaxBytes; actual is far less.
	// + 8 read slack, and the size is a multiple of 8 (write contract).
	batchBufferSize := (NumBatchMessages+1)*example.MessageMaxBytes + 8
	batchBuffer := make([]byte, batchBufferSize)

	messages, batchBytes := buildBatch(batchBuffer)
	if messages == nil {
		return
	}

	const passes = int64(BatchPasses)
	totalMsgs := passes * NumBatchMessages

	writeRates := make([]float64, NumRuns)
	readRates := make([]float64, NumRuns)
	rng := uint64(999)

	// write path: whole batch per pass; one message mutates per pass so the
	// batch is never loop-invariant
	ws := serialize.NewWriteStream(batchBuffer)
	for run := -1; run < NumRuns; run++ {
		start := time.Now()
		for pass := int64(0); pass < passes; pass++ {
			rng = benchRng(rng)
			switch m := messages[(rng>>16)%NumBatchMessages].(type) {
			case *example.Synchronize:
				m.SyncFrame = rng
			case *example.Test:
				m.TestA = uint16(rng)
			}
			ws.Reset(batchBuffer)
			for k := 0; k < NumBatchMessages; k++ {
				if err := example.WriteMessage(ws, messages[k]); err != nil {
					fail("message_batch", "write failed in loop")
					return
				}
			}
			example.WriteMessage(ws, nil)
			ws.Flush()
			gSink = gSink + uint64(ws.BytesProcessed())
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			writeRates[run] = float64(totalMsgs) / elapsed
		}
	}

	// the read buffer: rebuild once from the deterministic batch state so bytes match
	if m, _ := buildBatch(batchBuffer); m == nil {
		return
	}

	// read path: read messages until the None terminator, whole batch per pass;
	// reads land in pre-allocated storage — no heap per message
	var storage example.MessageStorage
	rs := serialize.NewReadStream(batchBuffer[:batchBytes])
	for run := -1; run < NumRuns; run++ {
		start := time.Now()
		for pass := int64(0); pass < passes; pass++ {
			rs.Reset(batchBuffer[:batchBytes])
			count := int64(0)
			for {
				m, err := example.ReadMessage(rs, &storage)
				if err != nil {
					fail("message_batch", "read failed in loop")
					return
				}
				if m == nil {
					break
				}
				runtime.KeepAlive(m)
				count++
			}
			if count != NumBatchMessages {
				fail("message_batch", "batch message count mismatch on read")
				return
			}
			gSink = gSink + uint64(count)
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			readRates[run] = float64(totalMsgs) / elapsed
		}
	}

	bytesPerMsg := batchBytes / NumBatchMessages
	report("message_batch", "write", totalMsgs, bytesPerMsg, stats(writeRates))
	report("message_batch", "read", totalMsgs, bytesPerMsg, stats(readRates))

	// allocation note (optimization seed, not a benchmark): allocs during one
	// extra untimed pass of each path
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	ws.Reset(batchBuffer)
	for k := 0; k < NumBatchMessages; k++ {
		example.WriteMessage(ws, messages[k])
	}
	example.WriteMessage(ws, nil)
	ws.Flush()
	runtime.ReadMemStats(&after)
	writeAllocs := after.Mallocs - before.Mallocs

	runtime.ReadMemStats(&before)
	rs.Reset(batchBuffer[:batchBytes])
	for {
		m, err := example.ReadMessage(rs, &storage)
		if err != nil || m == nil {
			break
		}
	}
	runtime.ReadMemStats(&after)
	readAllocs := after.Mallocs - before.Mallocs
	fmt.Fprintf(os.Stderr, "alloc note: message_batch one pass (%d msgs): write %d allocs, read %d allocs\n",
		NumBatchMessages, writeAllocs, readAllocs)
}

// ------------------------------------------------------------------------------------------

// the message_stream golden: dispatch wire self-check (not a benchmark)
func checkMessageStreamGolden() {
	chat := &example.Chat{}
	copy(chat.Text[:], "dispatch")
	chat.TextLength = 8
	test := &example.Test{TestB: 42}

	ws := serialize.NewWriteStream(gBuffer[:])
	if err := example.WriteMessage(ws, chat); err != nil {
		fail("message_stream", "dispatch write failed")
		return
	}
	if err := example.WriteMessage(ws, test); err != nil {
		fail("message_stream", "dispatch write failed")
		return
	}
	ws.Flush()
	if !checkGolden("message_stream", gBuffer[:ws.BytesProcessed()]) {
		failed = true
	}
}

func main() {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--csv":
			gCsv = true
		case args[i] == "--wire-dir" && i+1 < len(args):
			i++
			gWireDir = args[i]
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [--csv] [--wire-dir <dir>]\n", os.Args[0])
			os.Exit(1)
		}
	}

	fmt.Fprintf(os.Stderr, "schema bench (go)\n")

	checkMessageStreamGolden()

	// rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
	atRest := pinRigidBodyMoving()
	atRest.AtRest = true

	benchMessage("rigidbody_moving", "rigidbody_moving", 2000000, pinRigidBodyMoving(), example.WriteRigidBody, example.ReadRigidBody, varyRigidBody)
	benchMessage("rigidbody_at_rest", "rigidbody_at_rest", 4000000, atRest, example.WriteRigidBody, example.ReadRigidBody, varyRigidBodyAtRest)
	benchMessage("chat", "chat", 4000000, pinChat(), example.WriteChat, example.ReadChat, varyChat)
	benchMessage("test", "", 16000000, example.Test{}, example.WriteTest, example.ReadTest, varyTest)
	benchMessage("inputpacket", "inputpacket", 2000000, pinInputPacket(), example.WriteInputPacket, example.ReadInputPacket, varyInputPacket)
	benchMessage("shipcreate", "shipcreate_flags", 4000000, pinShipCreate(), example.WriteShipCreate, example.ReadShipCreate, varyShipCreate)
	benchMessage("ship_shallow", "ship_shallow", 4000000, pinShipShallow(), example.WriteShipData_Shallow, example.ReadShipData_Shallow, varyShipShallow)
	benchMessage("probe_header", "probe_header", 16000000, pinProbeHeader(), example.WriteProbeHeader, example.ReadProbeHeader, varyProbeHeader)
	benchMessage("probebits", "probebits", 4000000, pinProbeBits(), example.WriteProbeBits, example.ReadProbeBits, varyProbeBits)
	benchMessage("probearray", "probearray", 2000000, pinProbeArray(), example.WriteProbeArray, example.ReadProbeArray, varyProbeArray)
	benchMessage("testdata", "testdata", 1000000, pinTestData(), example.WriteTestData, example.ReadTestData, varyTestData)

	benchBatch()

	if failed {
		fmt.Fprintf(os.Stderr, "BENCH FAILED\n")
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "OK\n")
	_ = gSink
}
