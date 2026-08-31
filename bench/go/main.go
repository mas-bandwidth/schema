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
	"strconv"
	"time"

	"bench"
	"bench/realworld"
	"example"

	"github.com/mas-bandwidth/serialize.go"
)

const MaxNumRuns = 7      // median of 7 (N >= 5), after 1 warmup run
var gQuick = false        // --quick: bench_mixed only, 3 measured runs
var gNumRuns = MaxNumRuns // --round K drops this to 1 (§2.4: one warmup + one
// measured run per round; the driver aggregates across rounds)
const NumVariants = 64 // read-path variant buffers

// buffers: write buffers must be a multiple of 8 bytes (qword-flush contract);
// variant arrays keep >= 7 bytes of backing slack past the packet for the
// reader's window loads. 4096 covers the largest pinned shape (2008 bytes) with slack.
const BufferSize = 4096

// §2.7 variant-buffer stride: the 64 rotating read buffers are allocated at
// BufferSize + 64 per slot, NOT packed at exact 4096. At stride 4096 every
// head line maps into one of 4 L1 set-groups on the M2 (set bits [13:6]:
// 4096 >> 6 = 64 sets per step, 64k mod 256 cycles {0,64,128,192}), and a
// fully-inlined memory-bound read feels every background conflict miss in
// those sets. At 4160 the step is 65 and gcd(65,256) = 1: 64 head lines,
// 64 distinct sets. Identical in all five runners. The slice handed to the
// streams stays [:BufferSize]; the pad is address spacing only.
const VariantStride = BufferSize + 64

var gBuffer [BufferSize]byte
var gTwin [BufferSize]byte
var gVariants [NumVariants][VariantStride]byte

var gSink uint64 // defeats dead code elimination of computed values
var gCsv = false
var gWireDir = "../../testdata/wire"
var failed = false

// ---- CSV v2 (BENCH-STANDARD.md §5.1) ----
// Rows are buffered and emitted at exit so every row carries the corpus_id
// (§1.6): FNV-1a-64 over the goldens THIS RUN actually loaded — for each file
// in sorted basename order, the basename bytes, a 0x00 byte, the contents.
// The per-runner constants: family gen (these are the generated-code
// benchmarks), linkage pkg (the harness lives in its own package against the
// serialize.go module — same-process Go packages), checks always (the
// runtime keeps bounds checks, range validation and the sticky error check
// in every build by design), opt default (Go has no optimization levels),
// inline unknown until the verdict pass (§4.2) backfills it.
// family is per ROW now (gen | rt | bits — §5.1); linkage/checks/opt/inline
// stay per-runner constants
const csvSuffix = "pkg,always,default,unknown"

type csvRow struct {
	row    string // the first 11 columns
	family string
}

var gCsvRows []csvRow
var gGoldensLoaded = map[string][]byte{}

func fnv1a64(h uint64, data []byte) uint64 {
	for _, b := range data {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

func corpusID() string {
	names := make([]string, 0, len(gGoldensLoaded))
	for name := range gGoldensLoaded {
		names = append(names, name)
	}
	sort.Strings(names)
	h := uint64(0xcbf29ce484222325)
	for _, name := range names {
		h = fnv1a64(h, []byte(name))
		h = fnv1a64(h, []byte{0})
		h = fnv1a64(h, gGoldensLoaded[name])
	}
	return fmt.Sprintf("%016x", h)
}

func flushCsv() {
	if !gCsv {
		return
	}
	if failed {
		// §1.5: a failing run emits NO rows — the exit code and stderr are
		// the whole output. Numbers from a run whose gate refused are not
		// numbers.
		fmt.Fprintf(os.Stderr, "refusing to emit CSV rows from a failing run\n")
		return
	}
	id := corpusID()
	for _, r := range gCsvRows {
		fmt.Printf("%s,%s,%s,%s\n", r.row, id, r.family, csvSuffix)
	}
}

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
	gGoldensLoaded[name+".bin"] = expected
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

func report(bench, path string, iters int64, bytesPerOp int64, s runStats, family string) {
	mbps := s.median * float64(bytesPerOp) / (1024.0 * 1024.0)
	fmt.Fprintf(os.Stderr, "%-18s %-5s %10.2f M msg/s %10.1f MB/s   (min %.2f, max %.2f, spread %.1f%%)\n",
		bench, path, s.median/1e6, mbps, s.min/1e6, s.max/1e6, s.spread)
	if gCsv {
		gCsvRows = append(gCsvRows, csvRow{fmt.Sprintf("go,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
			bench, path, iters, bytesPerOp, gNumRuns, s.median, s.min, s.max, mbps, s.spread), family})
	}
}

// ------------------------------------------------------------------------------------------
// the per-shape benchmark driver
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
		vs := serialize.NewWriteStream(gVariants[k][:BufferSize])
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

	writeRates := make([]float64, gNumRuns)
	readRates := make([]float64, gNumRuns)

	// write path: 1 warmup + NumRuns measured (stream reused via Reset — the
	// runtime's documented no-allocation path)
	for run := -1; run < gNumRuns; run++ {
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
	for run := -1; run < gNumRuns; run++ {
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

	report(name, "write", iters, bytesPerOp, stats(writeRates), "gen")
	report(name, "read", iters, bytesPerOp, stats(readRates), "gen")

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

func varyProbeHeader(m *example.ProbeHeader, rng uint64) {
	m.Version = uint32(rng) & 7 // 3 wire bits
	m.ProbeId = rng
}

func varyProbeBits(m *example.ProbeBits, rng uint64) {
	m.Small = uint32(rng) & 511        // 9 bits
	m.Boundary = rng & ((1 << 33) - 1) // 33 bits
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

// real_packet — BENCH-STANDARD.md §1.7's realistic snapshot, measured through
// the GENERATED code (bench/corpus/RealWorld.schema ->
// generated/bench/go/realworld). The pinned instance is the ALL-DEFAULTS
// instance: realworld.NewRealPacket() serialized unmodified, 1629 bits = 204
// wire bytes, pinned to testdata/wire/real_packet.bin by test/bench/main.cpp.
// The four branch gates (f012 true, f043 false, f050 true, f074 false) are
// STRUCTURE (§2.7): they keep their schema defaults here, so the same branch
// bodies ride every iteration and bytes/op is constant. The field mappings
// are bench/cpp/bench_main.cpp's vary_real_packet exactly — fields under the
// false gates do not ride and are not varied; every mapping keeps its field
// inside its declared wire range (comments give the bound it stays within).
func varyRealPacket(m *realworld.RealPacket, rng uint64) {
	// ranged ints, assorted widths, signed and unsigned
	m.F001Int = int32((rng>>8)&0xFFFFF) - 0x80000    // +/-2^19 within +/-805495
	m.F003Int = int32((rng>>12)&0xFFFFF) - 0x80000   // within +/-835897
	m.F005Uint = uint16((rng >> 20) & 0xFFF)         // <=4095 within [0, 7316]
	m.F006Int = int16(int32((rng>>26)&0x7FF) - 1024) // +/-1024 within +/-1513
	m.F009Int = int8(int32((rng>>33)&31) - 16)       // +/-16 within +/-22
	m.F033Uint = uint32((rng >> 37) & 0x1FFFF)       // <=131071 within [0, 142780]
	m.F041Int = int8(int32((rng>>42)&63) - 32)       // +/-32 within +/-55
	m.F062Uint = uint16((rng >> 47) & 255)           // <=255 within [0, 503]
	m.F088Int = int16(int32((rng>>52)&0x3FF) - 512)  // +/-512 within +/-694
	m.F090Uint = uint8((rng >> 57) & 127)            // <=127 within [0, 214]
	// bits(N), narrow and wide
	m.F011Bits = uint32(rng) & 0x3FF         // 10 bits
	m.F023Bits = uint32(rng>>5) & 0x1FFFFFF  // 25 bits
	m.F042Bits = uint32(rng>>3) & 0x3FFFFFFF // 30 bits
	m.F081Bits = uint32(rng>>7) & 0x1FFFFFFF // 29 bits
	m.F089Bits = rng & 0xFFFFFFFFFFFF        // 48 bits
	m.F093Bits = rng ^ 0x5555555555555555    // 64 bits
	m.F097Bits = uint32(rng>>11) & 0xFFF     // 12 bits
	// bools (NEVER the four branch gates — those are structure, §2.7)
	m.F037Bool = (rng & 1) != 0
	m.F055Bool = (rng & 2) != 0
	m.F092Bool = (rng & 4) != 0
	// float32 / float64
	m.F007F32 = float32(uint32(rng) & 0xFFFF)
	m.F020F32 = float32(uint32(rng>>16)&0xFFFF) * 0.5
	m.F058F32 = float32(uint32(rng>>24)&0xFFFF) * 0.25
	m.F002F64 = float64(int64(rng>>8)&0xFFFFFF) * 0.5
	m.F059F64 = float64(int64(rng>>16)&0xFFFFFF) * 0.25
	m.F087F64 = float64(int64(rng>>24)&0xFFFFFF) * 0.125
	// compressed floats (in range by construction)
	m.F004Cf32 = float32(uint32(rng)&0x3FFF) * 0.1          // <=1638.3 within [0, 2000]
	m.F061Cf32 = -90.0 + float32(uint32(rng>>9)&255)*0.5    // within [-90, 90] (max 37.5)
	m.F067Cf32 = -100.0 + float32(uint32(rng>>18)&511)*0.25 // within [-100, 100] (max 27.75)
	m.F072Cf32 = float32(uint32(rng>>27)&8191) * 0.01       // <=81.91 within [0, 100]
	// fixed / ufixed (raw storage scaled by 2^F; bounds are whole units)
	m.F016Fixed = int32((rng>>10)&0x3FFFFFF) - 0x2000000  // +/-2^25 within +/-36*2^20
	m.F025Fixed = int16(int32((rng>>18)&0x7FFF) - 0x4000) // +/-2^14 within +/-119*2^8
	m.F095Fixed = int32((rng>>22)&0x7FFFFFF) - 0x4000000  // +/-2^26 within +/-1577*2^16
	m.F021Ufixed = uint32(rng>>30) & 0x3FFFFFF            // <=2^26-1 within 25141*2^12
	m.F049Ufixed = uint16((rng >> 36) & 0x7FFF)           // <=32767 within 3*2^14
	m.F084Ufixed = uint8((rng >> 44) & 0x7F)              // <=127 within 1*2^7
	// enum / flags (wire-valid by construction)
	m.F036Enum = realworld.PacketMode(uint32(rng>>30) & 3) // within wire range [0, 5]
	m.F083Enum = realworld.PacketMode(uint32(rng>>34) & 3)
	m.F091Flags = realworld.PacketFlags(rng & 31) // 5 wire bits
	// full-width 64-bit
	m.F008U64 = rng
	m.F029I64 = int64(rng * 3)
	m.F063I64 = int64(rng * 5)
	// fields riding inside the TAKEN branches (f012 true, f050 true)
	m.F013F32 = float32(uint32(rng>>4) & 0xFFFF)
	m.F014Uint = uint16((rng >> 21) & 511)     // <=511 within [0, 775]
	m.F015Int = int8(int32((rng>>40)&31) - 16) // +/-16 within +/-21
	m.F017Uint = uint16((rng >> 29) & 0xFFF)   // <=4095 within [0, 4606]
	m.F051Bool = (rng & 8) != 0
	m.F052Int = int8(int32((rng>>38)&63) - 32) // +/-32 within +/-57
	m.F053F32 = float32(uint32(rng>>40)&0xFFFF) * 0.125
	m.F054Int = int8(int32((rng>>45)&63) - 32) // +/-32 within +/-35
}

// ------------------------------------------------------------------------------------------
// family gen over the Bench corpus (issue #177): the four Bench.schema shapes
// measured through the GENERATED code (generated/bench/go, module bench) —
// the gen twins of the rt rows, which serialize the same shapes BY HAND
// against the runtime API (rt.go). Same golden files, same pinned values,
// same LCG field mappings, same benchMessage discipline as every gen row
// above; the family column carries the subject, and relative.go refuses
// gen-vs-rt ratios. Generated best case per the profiling doctrine (#170):
// the plain default optimized build, no PGO.
// ------------------------------------------------------------------------------------------

func pinGenPacket() bench.BenchPacket {
	var in bench.BenchPacket
	in.A = -37
	in.B = 12345
	in.C = 987654
	in.Bits7 = 97
	in.Bits13 = 5000
	in.Bits23 = 1234567
	in.Flag = true
	in.X = 1.5
	in.Y = -3.25
	in.Z = 100.125
	in.Big = 0x123456789ABCDEF0
	for i := 0; i < 17; i++ {
		in.Blob[i] = uint8(i * 31)
	}
	return in
}

func pinGenInts() bench.BenchInts {
	var in bench.BenchInts
	in.F0 = -37
	in.F1 = 12345
	in.F2 = 987654
	in.F3 = 2
	in.F4 = -15
	in.F5 = 777
	in.F6 = -2048
	in.F7 = 200
	in.F8 = -543210
	in.F9 = 99
	return in
}

func pinGenBits() bench.BenchBits {
	var in bench.BenchBits
	in.B7 = 97
	in.B13 = 5000
	in.B23 = 1234567
	in.B3 = 5
	in.B32 = 0xDEADBEEF
	in.B11 = 1024
	in.B19 = 333333
	in.B48 = 0xFEDCBA987654
	return in
}

// BenchMixed — THE canonical benchmark shape (issue #184). The pin is
// test/bench/main.cpp's, transcribed exactly; STRUCTURE fields (the two array
// counts, the two used lengths, the union tag, the `if` gate) are set here and
// never touched by vary*, so bytes/op is constant (§2.7).
func pinGenMixed() bench.BenchMixed {
	var in bench.BenchMixed
	in.Sequence = 52428
	in.AckSequence = 12345
	in.AckBits = 0xA5A5A5A5
	in.SessionId = 0x123456789ABCDEF0
	in.ClientId = 0xDEADBEEF
	in.Nonce = 0xFEDCBA9876543210
	in.WorldTime = -987654321000
	in.FrameTick = 0x123456789ABC
	in.ServerTime = 12345678
	in.EntitiesCount = 8
	for i := 0; i < 8; i++ {
		e := &in.Entities[i]
		e.EntityId = uint32(2049 + i*17)
		e.PosX = int32(-16383 + i*4096)
		e.PosY = int32(16383 - i*4096)
		e.PosZ = int32(-1 + i*2048)
		e.Yaw = uint32(511 - i*64)
		e.Pitch = uint32(i * 73)
		e.VelX = int32(-2048 + i*512)
		e.VelY = int32(2047 - i*512)
		e.VelZ = int32(-1024 + i*256)
		e.Health = int32(1000 - i*100)
		e.Weapon = bench.MixedWeapon(1 + i)
		e.Damage = bench.MixedDamage(0x5A + i)
		e.Moving = i%2 == 0
		e.Firing = i%3 == 0
	}
	in.StatsCount = 80
	for i := 0; i < 80; i++ {
		in.Stats[i].StatId = uint32((i * 3) % 256)
		in.Stats[i].Delta = int32(-512 + (i*13)%1024)
	}
	in.GameEvent.Type = bench.MixedEventTypeHit
	in.GameEvent.Hit.TargetId = 4095
	in.GameEvent.Hit.Damage = 4095
	in.GameEvent.Hit.HitKind = 7
	in.GameEvent.Hit.Crit = true
	copy(in.Loadout[:], []byte{0x11, 0x22, 0x33, 0x44})
	copy(in.PlayerName[:], "Rowan_01")
	in.PlayerNameLength = 8
	copy(in.Payload[:], []byte{0xDE, 0xAD, 0xBE, 0xEF, 0x01, 0x02, 0x03, 0x04})
	in.PayloadLength = 8
	in.AimX = 0.5
	in.AimY = -0.25
	in.AimZ = 0.75
	in.Recoil = 1.5
	in.Drift = -3.25
	in.WideKey = serialize.Uint128{Lo: 0xFEDCBA9876543210, Hi: 0x0123456789ABCDEF}
	in.Flux = serialize.Int128{Lo: 7, Hi: 0x800000000} // 2^99 + 7
	in.Ping = 12345
	in.CrcHint = 0xABCDEF
	in.HasExtra = true
	in.Extra = 200
	return in
}

func varyGenPacket(p *bench.BenchPacket, rng uint64) {
	p.A = int32((rng>>8)&63) - 32
	p.B = int32(uint32(rng>>16) & 65535)
	p.C = int32((rng>>24)&0xFFFFF) - 500000
	p.Bits7 = uint32(rng) & 127
	p.Bits13 = uint32(rng>>3) & 8191
	p.Bits23 = uint32(rng>>5) & 8388607
	p.Flag = (rng & 1) != 0
	p.X = float32(uint32(rng) & 0xFFFF)
	p.Big = rng
	p.Blob[0] = uint8(rng >> 32)
}

func varyGenInts(f *bench.BenchInts, rng uint64) {
	f.F0 = int32((rng>>8)&63) - 32
	f.F1 = int32(uint32(rng>>16) & 65535)
	f.F2 = int32((rng>>24)&0xFFFFF) - 500000
	f.F3 = int32(uint32(rng>>2) & 3)
	f.F4 = int32((rng>>11)&15) - 8
	f.F5 = int32(uint32(rng>>22) & 511)
	f.F6 = int32((rng>>33)&2047) - 1024
	f.F7 = int32(uint32(rng>>40) & 255)
	f.F8 = int32((rng>>30)&0xFFFFF) - 500000
	f.F9 = int32(uint32(rng>>57) & 63)
}

func varyGenBits(f *bench.BenchBits, rng uint64) {
	f.B7 = uint32(rng) & 127
	f.B13 = uint32(rng>>3) & 8191
	f.B23 = uint32(rng>>5) & 8388607
	f.B3 = uint32(rng>>29) & 7
	f.B32 = uint32(rng >> 16)
	f.B11 = uint32(rng>>37) & 2047
	f.B19 = uint32(rng>>44) & 524287
	f.B48 = rng & 0xFFFFFFFFFFFF
}

// The LCG field mapping for BenchMixed, identical in every runner. VALUE
// fields only: every count, used length, union tag and branch gate is
// STRUCTURE (§2.7). All 8 entities vary; the 80 stats vary Delta (StatId
// stays pinned) — the family convention of varying a representative subset.
func varyGenMixed(f *bench.BenchMixed, rng uint64) {
	f.Sequence = uint32(rng>>8) & 65535
	f.AckSequence = int32(uint32(rng>>24) & 65535)
	f.AckBits = uint32(rng >> 16)
	f.SessionId = rng
	f.ClientId = uint32(rng >> 32)
	f.Nonce = rng ^ 0xA5A5A5A5A5A5A5A5
	f.WorldTime = int64((rng>>12)&0xFFFFFFFFF) - 34359738368
	f.FrameTick = rng & 0xFFFFFFFFFFFF
	f.ServerTime = int32((rng >> 20) & 0x7FFFFF)
	for i := 0; i < 8; i++ {
		e := &f.Entities[i]
		e.EntityId = uint32((rng >> uint(i)) & 4095)
		e.PosX = int32((rng>>uint(i+4))&16383) - 8192
		e.PosY = int32((rng>>uint(i+12))&16383) - 8192
		e.Health = int32((rng >> uint(i+20)) & 511)
		e.Weapon = bench.MixedWeapon((rng >> uint(i+40)) & 15)
		e.Damage = bench.MixedDamage((rng >> uint(i+28)) & 255)
		e.Moving = (rng>>uint(i))&1 != 0
	}
	for i := 0; i < 80; i++ {
		f.Stats[i].Delta = int32((rng>>uint(i&31))&1023) - 512
	}
	f.GameEvent.Hit.TargetId = uint32((rng >> 6) & 4095)
	f.GameEvent.Hit.Damage = int32((rng >> 18) & 4095)
	f.GameEvent.Hit.HitKind = int32((rng >> 30) & 7)
	f.GameEvent.Hit.Crit = rng&4 != 0
	f.Loadout[0] = uint8(rng >> 56)
	f.PlayerName[7] = byte(65 + ((rng >> 50) & 15))
	f.Payload[0] = uint8(rng >> 48)
	f.AimX = float32(uint32(rng>>2)&255)*(1.0/256.0) - 0.5
	f.AimY = float32(uint32(rng>>10)&255)*(1.0/256.0) - 0.5
	f.AimZ = float32(uint32(rng>>18)&255)*(1.0/256.0) - 0.5
	f.Recoil = float32(uint32(rng) & 0xFFFF)
	f.Drift = float64(int64((rng>>8)&0xFFFFFF)) * 0.5
	f.WideKey = serialize.Uint128{Lo: rng, Hi: rng >> 1}
	f.Flux = serialize.Int128From64(int64(rng >> 16))
	f.Ping = uint16((rng >> 40) & 0x7FFF)
	f.CrcHint = uint32((rng >> 24) & 0xFFFFFF)
	f.Extra = int32((rng >> 52) & 255)
}

// ------------------------------------------------------------------------------------------

func main() {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--csv":
			gCsv = true
		case args[i] == "--wire-dir" && i+1 < len(args):
			i++
			gWireDir = args[i]
		case args[i] == "--round" && i+1 < len(args):
			// §2.4: one warmup + one measured run of every benchmark, then
			// exit. K only identifies the round to the interleaved driver,
			// which aggregates max/median/min/spread across rounds itself.
			i++
			if k, err := strconv.Atoi(args[i]); err != nil || k < 0 {
				fmt.Fprintf(os.Stderr, "--round takes a non-negative integer, got %q\n", args[i])
				os.Exit(1)
			}
			gNumRuns = 1
		case args[i] == "--quick":
			gQuick = true
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [--csv] [--round K] [--quick] [--wire-dir <dir>]\n", os.Args[0])
			os.Exit(1)
		}
	}
	if gQuick && gNumRuns == MaxNumRuns {
		gNumRuns = 3
	}

	if gQuick {
		// --quick: bench_mixed only, 3 measured runs — the iteration
		// instrument, never the certification instrument. Golden gate
		// unconditional (benchRt gates before timing).
		// The gen row is the schema subject (the blended table's row); the
		// rt row rides beside it as the hand-written-usage subject.
		fmt.Fprintf(os.Stderr, "schema bench (go, --quick: iteration instrument, not certification)\n")
		benchMessage("bench_mixed", "bench_mixed", 4000000, pinGenMixed(), bench.WriteBenchMixed, bench.ReadBenchMixed, varyGenMixed)
		benchRt("bench_mixed", 4000000, pinRtMixed(), rtOnceWriteMixed, rtOnceReadMixed, rtBenchMixedWriteLoop, rtBenchMixedReadLoop, varyRtMixed)
		flushCsv()
		if failed {
			fmt.Fprintf(os.Stderr, "BENCH FAILED (corpus_id %s)\n", corpusID())
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "OK (corpus_id %s)\n", corpusID())
		return
	}

	fmt.Fprintf(os.Stderr, "schema bench (go)\n")

	// rigidbody_at_rest: the pinned at-rest twin of rigidbody_moving
	atRest := pinRigidBodyMoving()
	atRest.AtRest = true

	benchMessage("rigidbody_moving", "rigidbody_moving", 24000000, pinRigidBodyMoving(), example.WriteRigidBody, example.ReadRigidBody, varyRigidBody)
	benchMessage("rigidbody_at_rest", "rigidbody_at_rest", 32000000, atRest, example.WriteRigidBody, example.ReadRigidBody, varyRigidBodyAtRest)
	benchMessage("chat", "chat", 48000000, pinChat(), example.WriteChat, example.ReadChat, varyChat)
	benchMessage("test", "", 192000000, example.Test{}, example.WriteTest, example.ReadTest, varyTest)
	benchMessage("inputpacket", "inputpacket", 16000000, pinInputPacket(), example.WriteInputPacket, example.ReadInputPacket, varyInputPacket)
	benchMessage("shipcreate", "shipcreate_flags", 32000000, pinShipCreate(), example.WriteShipCreate, example.ReadShipCreate, varyShipCreate)
	benchMessage("probe_header", "probe_header", 256000000, pinProbeHeader(), example.WriteProbeHeader, example.ReadProbeHeader, varyProbeHeader)
	benchMessage("probebits", "probebits", 128000000, pinProbeBits(), example.WriteProbeBits, example.ReadProbeBits, varyProbeBits)
	benchMessage("probearray", "probearray", 20000000, pinProbeArray(), example.WriteProbeArray, example.ReadProbeArray, varyProbeArray)
	benchMessage("testdata", "testdata", 8000000, pinTestData(), example.WriteTestData, example.ReadTestData, varyTestData)

	// real_packet (§1.7): the realistic snapshot — ~93 riding individually
	// serialized small fields, 204 wire bytes, 0% bulk share by bits. The pin
	// is the ALL-DEFAULTS instance (NewRealPacket — the C++ RealPacket{}),
	// golden-gated like every row above. Iteration count sized in the C++
	// reference (§2.1).
	benchMessage("real_packet", "real_packet", 8000000, realworld.NewRealPacket(), realworld.WriteRealPacket, realworld.ReadRealPacket, varyRealPacket)

	// family gen over the Bench corpus (issue #177): the generated twins of
	// the rt rows below — same shapes, same goldens, same pins, same vary
	// mappings, same iteration counts (fixed and identical across all five
	// runners, §2.1); only the subject differs, and the family column says so.
	benchMessage("bench_packet", "bench_packet", 32000000, pinGenPacket(), bench.WriteBenchPacket, bench.ReadBenchPacket, varyGenPacket)
	benchMessage("bench_ints", "bench_ints", 40000000, pinGenInts(), bench.WriteBenchInts, bench.ReadBenchInts, varyGenInts)
	benchMessage("bench_bits", "bench_bits", 48000000, pinGenBits(), bench.WriteBenchBits, bench.ReadBenchBits, varyGenBits)
	benchMessage("bench_mixed", "bench_mixed", 4000000, pinGenMixed(), bench.WriteBenchMixed, bench.ReadBenchMixed, varyGenMixed)

	// family rt (§1.3/§1.5): the runtime API by hand, oracle-gated against
	// the goldens the generated code pinned. Iteration counts are fixed and
	// identical across all five runners (§2.1; sized in the C++ reference).
	benchRt("bench_packet", 32000000, pinRtPacket(), rtOnceWritePacket, rtOnceReadPacket, rtBenchPacketWriteLoop, rtBenchPacketReadLoop, varyRtPacket)
	benchRt("bench_ints", 40000000, pinRtInts(), rtOnceWriteInts, rtOnceReadInts, rtBenchIntsWriteLoop, rtBenchIntsReadLoop, varyRtInts)
	benchRt("bench_bits", 48000000, pinRtBits(), rtOnceWriteBits, rtOnceReadBits, rtBenchBitsWriteLoop, rtBenchBitsReadLoop, varyRtBits)
	benchRt("bench_mixed", 4000000, pinRtMixed(), rtOnceWriteMixed, rtOnceReadMixed, rtBenchMixedWriteLoop, rtBenchMixedReadLoop, varyRtMixed)

	// family bits (§1.4): the one bitpacker workload in the estate
	benchBitpacker(24576)

	flushCsv() // rows carry the corpus_id of the goldens this run loaded

	if failed {
		fmt.Fprintf(os.Stderr, "BENCH FAILED (corpus_id %s)\n", corpusID())
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "OK (corpus_id %s)\n", corpusID())
	_ = gSink
}
