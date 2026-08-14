// schema bench — families rt and bits for the Go runner.
//
// Family rt (BENCH-STANDARD.md §1.3, §1.5): the serialize.go runtime API
// called BY HAND — the four Bench.schema shapes as hand-written packets over
// the Serialize* surface, the way a game would write them. The §1.5 oracle
// gate byte-compares the hand-written wire against the goldens the GENERATED
// code pinned (testdata/wire/bench_*.bin) and round-trips before any number.
// Unexported identifiers in the runner's own package per §3.1. Per §3.2 every
// benched op has EXACTLY two call sites: its untimed once-helper and its
// timed loop (//go:noinline, so the §4.1 verdict has a loop body to count).
//
// Family bits (§1.4): the raw BitWriter/BitReader with the 16-width table
// (227 bits/group) over a 65536-byte buffer. Values vary per pass through
// the LCG (widths are the structure and stay fixed; bytes/pass asserted
// constant); reads rotate 64 pre-written variant buffers, each verified to
// read back exactly what was written before any number is produced.
package main

import (
	"bytes"
	"time"

	"github.com/mas-bandwidth/serialize.go"
)

// ---- the four shapes, hand-written (Bench.schema §1.3) ----

type rtBenchPacket struct {
	a, b, c               int32
	bits7, bits13, bits23 uint32
	flag                  bool
	x, y, z               float32
	big                   uint64
	blob                  [17]byte
}

func writeRtPacket(s *serialize.WriteStream, p *rtBenchPacket) error {
	s.SerializeInt(&p.a, -100, 100)
	s.SerializeInt(&p.b, 0, 65535)
	s.SerializeInt(&p.c, -1000000, 1000000)
	s.SerializeBits(&p.bits7, 7)
	s.SerializeBits(&p.bits13, 13)
	s.SerializeBits(&p.bits23, 23)
	s.SerializeBool(&p.flag)
	s.SerializeFloat32(&p.x)
	s.SerializeFloat32(&p.y)
	s.SerializeFloat32(&p.z)
	s.SerializeUint64(&p.big)
	s.SerializeBytes(p.blob[:]) // aligns internally — the schema says `align` out loud
	return s.Err()
}

func readRtPacket(s *serialize.ReadStream, p *rtBenchPacket) error {
	s.SerializeInt(&p.a, -100, 100)
	s.SerializeInt(&p.b, 0, 65535)
	s.SerializeInt(&p.c, -1000000, 1000000)
	s.SerializeBits(&p.bits7, 7)
	s.SerializeBits(&p.bits13, 13)
	s.SerializeBits(&p.bits23, 23)
	s.SerializeBool(&p.flag)
	s.SerializeFloat32(&p.x)
	s.SerializeFloat32(&p.y)
	s.SerializeFloat32(&p.z)
	s.SerializeUint64(&p.big)
	s.SerializeBytes(p.blob[:])
	return s.Err()
}

type rtBenchInts struct {
	f0, f1, f2, f3, f4, f5, f6, f7, f8, f9 int32
}

func writeRtInts(s *serialize.WriteStream, f *rtBenchInts) error {
	s.SerializeInt(&f.f0, -100, 100)
	s.SerializeInt(&f.f1, 0, 65535)
	s.SerializeInt(&f.f2, -1000000, 1000000)
	s.SerializeInt(&f.f3, 0, 3)
	s.SerializeInt(&f.f4, -15, 15)
	s.SerializeInt(&f.f5, 0, 1000)
	s.SerializeInt(&f.f6, -2048, 2047)
	s.SerializeInt(&f.f7, 0, 255)
	s.SerializeInt(&f.f8, -600000, 600000)
	s.SerializeInt(&f.f9, 0, 100)
	return s.Err()
}

func readRtInts(s *serialize.ReadStream, f *rtBenchInts) error {
	s.SerializeInt(&f.f0, -100, 100)
	s.SerializeInt(&f.f1, 0, 65535)
	s.SerializeInt(&f.f2, -1000000, 1000000)
	s.SerializeInt(&f.f3, 0, 3)
	s.SerializeInt(&f.f4, -15, 15)
	s.SerializeInt(&f.f5, 0, 1000)
	s.SerializeInt(&f.f6, -2048, 2047)
	s.SerializeInt(&f.f7, 0, 255)
	s.SerializeInt(&f.f8, -600000, 600000)
	s.SerializeInt(&f.f9, 0, 100)
	return s.Err()
}

type rtBenchBits struct {
	b7, b13, b23, b3, b32, b11, b19 uint32
	b48                             uint64
}

func writeRtBits(s *serialize.WriteStream, f *rtBenchBits) error {
	s.SerializeBits(&f.b7, 7)
	s.SerializeBits(&f.b13, 13)
	s.SerializeBits(&f.b23, 23)
	s.SerializeBits(&f.b3, 3)
	s.SerializeBits(&f.b32, 32)
	s.SerializeBits(&f.b11, 11)
	s.SerializeBits(&f.b19, 19)
	s.SerializeBits64(&f.b48, 48)
	return s.Err()
}

func readRtBits(s *serialize.ReadStream, f *rtBenchBits) error {
	s.SerializeBits(&f.b7, 7)
	s.SerializeBits(&f.b13, 13)
	s.SerializeBits(&f.b23, 23)
	s.SerializeBits(&f.b3, 3)
	s.SerializeBits(&f.b32, 32)
	s.SerializeBits(&f.b11, 11)
	s.SerializeBits(&f.b19, 19)
	s.SerializeBits64(&f.b48, 48)
	return s.Err()
}

type rtBenchMixed struct {
	sequence          int32
	ackBits, entityID uint32
	posX, posY, posZ  int32
	yaw               uint32
	moving, firing    bool
	timestamp         uint64
	weapon            int32
}

func writeRtMixed(s *serialize.WriteStream, f *rtBenchMixed) error {
	s.SerializeInt(&f.sequence, 0, 65535)
	s.SerializeBits(&f.ackBits, 32)
	s.SerializeBits(&f.entityID, 12)
	s.SerializeInt(&f.posX, -16384, 16383)
	s.SerializeInt(&f.posY, -16384, 16383)
	s.SerializeInt(&f.posZ, -16384, 16383)
	s.SerializeBits(&f.yaw, 9)
	s.SerializeBool(&f.moving)
	s.SerializeBool(&f.firing)
	s.SerializeBits64(&f.timestamp, 48)
	s.SerializeInt(&f.weapon, 0, 15)
	return s.Err()
}

func readRtMixed(s *serialize.ReadStream, f *rtBenchMixed) error {
	s.SerializeInt(&f.sequence, 0, 65535)
	s.SerializeBits(&f.ackBits, 32)
	s.SerializeBits(&f.entityID, 12)
	s.SerializeInt(&f.posX, -16384, 16383)
	s.SerializeInt(&f.posY, -16384, 16383)
	s.SerializeInt(&f.posZ, -16384, 16383)
	s.SerializeBits(&f.yaw, 9)
	s.SerializeBool(&f.moving)
	s.SerializeBool(&f.firing)
	s.SerializeBits64(&f.timestamp, 48)
	s.SerializeInt(&f.weapon, 0, 15)
	return s.Err()
}

// ---- pinned instances: test/bench/main.cpp (the golden producer), verbatim ----

func pinRtPacket() rtBenchPacket {
	p := rtBenchPacket{
		a: -37, b: 12345, c: 987654,
		bits7: 97, bits13: 5000, bits23: 1234567,
		flag: true,
		x:    1.5, y: -3.25, z: 100.125,
		big: 0x123456789ABCDEF0,
	}
	for i := 0; i < 17; i++ {
		p.blob[i] = byte(i * 31)
	}
	return p
}

func pinRtInts() rtBenchInts {
	return rtBenchInts{f0: -37, f1: 12345, f2: 987654, f3: 2, f4: -15,
		f5: 777, f6: -2048, f7: 200, f8: -543210, f9: 99}
}

func pinRtBits() rtBenchBits {
	return rtBenchBits{b7: 97, b13: 5000, b23: 1234567, b3: 5,
		b32: 0xDEADBEEF, b11: 1024, b19: 333333, b48: 0xFEDCBA987654}
}

func pinRtMixed() rtBenchMixed {
	return rtBenchMixed{sequence: 52428, ackBits: 0xA5A5A5A5, entityID: 2049,
		posX: -16384, posY: 16383, posZ: -1,
		yaw: 511, moving: true, firing: false,
		timestamp: 0x123456789ABC, weapon: 15}
}

// ---- vary functions: bench/cpp/bench_main.cpp's rt mappings exactly ----

func varyRtPacket(p *rtBenchPacket, rng uint64) {
	p.a = int32((rng>>8)&63) - 32
	p.b = int32(uint32(rng>>16) & 65535)
	p.c = int32((rng>>24)&0xFFFFF) - 500000
	p.bits7 = uint32(rng) & 127
	p.bits13 = uint32(rng>>3) & 8191
	p.bits23 = uint32(rng>>5) & 8388607
	p.flag = (rng & 1) != 0
	p.x = float32(uint32(rng) & 0xFFFF)
	p.big = rng
	p.blob[0] = byte(rng >> 32)
}

func varyRtInts(f *rtBenchInts, rng uint64) {
	f.f0 = int32((rng>>8)&63) - 32
	f.f1 = int32(uint32(rng>>16) & 65535)
	f.f2 = int32((rng>>24)&0xFFFFF) - 500000
	f.f3 = int32(uint32(rng>>2) & 3)
	f.f4 = int32((rng>>11)&15) - 8
	f.f5 = int32(uint32(rng>>22) & 511)
	f.f6 = int32((rng>>33)&2047) - 1024
	f.f7 = int32(uint32(rng>>40) & 255)
	f.f8 = int32((rng>>30)&0xFFFFF) - 500000
	f.f9 = int32(uint32(rng>>57) & 63)
}

func varyRtBits(f *rtBenchBits, rng uint64) {
	f.b7 = uint32(rng) & 127
	f.b13 = uint32(rng>>3) & 8191
	f.b23 = uint32(rng>>5) & 8388607
	f.b3 = uint32(rng>>29) & 7
	f.b32 = uint32(rng >> 16)
	f.b11 = uint32(rng>>37) & 2047
	f.b19 = uint32(rng>>44) & 524287
	f.b48 = rng & 0xFFFFFFFFFFFF
}

func varyRtMixed(f *rtBenchMixed, rng uint64) {
	f.sequence = int32(uint32(rng>>8) & 65535)
	f.ackBits = uint32(rng >> 16)
	f.entityID = uint32(rng) & 4095
	f.posX = int32((rng>>20)&32767) - 16384
	f.posY = int32((rng>>25)&32767) - 16384
	f.posZ = int32((rng>>30)&32767) - 16384
	f.yaw = uint32(rng>>3) & 511
	f.moving = (rng & 1) != 0
	f.firing = (rng & 2) != 0
	f.timestamp = rng & 0xFFFFFFFFFFFF
	f.weapon = int32(uint32(rng>>60) & 15)
}

// ---- the single untimed call sites (§3.2), one pair per shape ----

func rtOnceWritePacket(p *rtBenchPacket, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtPacket(ws, p) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadPacket(p *rtBenchPacket, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtPacket(rs, p) == nil
}

func rtOnceWriteInts(f *rtBenchInts, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtInts(ws, f) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadInts(f *rtBenchInts, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtInts(rs, f) == nil
}

func rtOnceWriteBits(f *rtBenchBits, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtBits(ws, f) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadBits(f *rtBenchBits, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtBits(rs, f) == nil
}

func rtOnceWriteMixed(f *rtBenchMixed, buf []byte) int {
	ws := serialize.NewWriteStream(buf)
	if writeRtMixed(ws, f) != nil {
		return -1
	}
	ws.Flush()
	return int(ws.BytesProcessed())
}

func rtOnceReadMixed(f *rtBenchMixed, buf []byte) bool {
	rs := serialize.NewReadStream(buf)
	return readRtMixed(rs, f) == nil
}

// ---- the timed loops, one symbol per (shape, path) so the §4.1 verdict is a
// direct transitive call count over the loop body. Streams reuse via Reset —
// the runtime's documented no-allocation path, as the gen benches do. ----

//go:noinline
func rtBenchPacketWriteLoop(base *rtBenchPacket, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtPacket(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtPacket(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchPacketReadLoop(out *rtBenchPacket, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtPacket(rs, out) != nil {
			return false
		}
		gSink = gSink + 1
	}
	return true
}

//go:noinline
func rtBenchIntsWriteLoop(base *rtBenchInts, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtInts(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtInts(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchIntsReadLoop(out *rtBenchInts, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtInts(rs, out) != nil {
			return false
		}
		gSink = gSink + 1
	}
	return true
}

//go:noinline
func rtBenchBitsWriteLoop(base *rtBenchBits, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtBits(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtBits(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchBitsReadLoop(out *rtBenchBits, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtBits(rs, out) != nil {
			return false
		}
		gSink = gSink + 1
	}
	return true
}

//go:noinline
func rtBenchMixedWriteLoop(base *rtBenchMixed, iters int64, rng *uint64) bool {
	ws := serialize.NewWriteStream(gBuffer[:])
	for i := int64(0); i < iters; i++ {
		*rng = benchRng(*rng)
		varyRtMixed(base, *rng)
		ws.Reset(gBuffer[:])
		if writeRtMixed(ws, base) != nil {
			return false
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	return true
}

//go:noinline
func rtBenchMixedReadLoop(out *rtBenchMixed, iters int64, bytesPerOp int) bool {
	rs := serialize.NewReadStream(gVariants[0][:bytesPerOp])
	for i := int64(0); i < iters; i++ {
		rs.Reset(gVariants[i&(NumVariants-1)][:bytesPerOp])
		if readRtMixed(rs, out) != nil {
			return false
		}
		gSink = gSink + 1
	}
	return true
}

// ---- the family rt driver: §1.5 oracle gate, then the timed loops ----

func benchRt[T any](name string, baseIters int64, pinned T,
	onceWrite func(*T, []byte) int,
	onceRead func(*T, []byte) bool,
	writeLoop func(*T, int64, *uint64) bool,
	readLoop func(*T, int64, int) bool,
	vary func(*T, uint64)) {

	iters := baseIters

	// oracle 1: the hand-written wire must equal the generated-code golden
	base := pinned
	bytesPerOp := onceWrite(&base, gBuffer[:])
	if bytesPerOp < 0 {
		fail(name, "write of pinned instance failed")
		return
	}
	if !checkGolden(name, gBuffer[:bytesPerOp]) {
		failed = true
		return
	}

	// oracle 2: round-trip write -> read -> re-write -> identical bytes
	var out T
	if !onceRead(&out, gBuffer[:bytesPerOp]) {
		fail(name, "read of pinned instance failed")
		return
	}
	if onceWrite(&out, gTwin[:]) != bytesPerOp ||
		!bytes.Equal(gBuffer[:bytesPerOp], gTwin[:bytesPerOp]) {
		fail(name, "round-trip bytes differ")
		return
	}

	// variant buffers (and proof that variation keeps bytes/op constant)
	rng := uint64(1)
	for k := 0; k < NumVariants; k++ {
		rng = benchRng(rng)
		vary(&base, rng)
		if onceWrite(&base, gVariants[k][:]) != bytesPerOp {
			fail(name, "variation changed bytes/op — vary must keep structure fields fixed")
			return
		}
	}

	writeRates := make([]float64, gNumRuns)
	readRates := make([]float64, gNumRuns)

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !writeLoop(&base, iters, &rng) {
			fail(name, "write failed in loop")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			writeRates[run] = float64(iters) / elapsed
		}
	}

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !readLoop(&out, iters, bytesPerOp) {
			fail(name, "read failed in loop")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			readRates[run] = float64(iters) / elapsed
		}
	}

	report(name, "write", iters, int64(bytesPerOp), stats(writeRates), "rt")
	report(name, "read", iters, int64(bytesPerOp), stats(readRates), "rt")
}

// ------------------------------------------------------------------------------------------
// family bits (§1.4)
// ------------------------------------------------------------------------------------------

const BitsNumWidths = 16
const BitsBufferSize = 65536

var bitsWidths = [BitsNumWidths]int{1, 32, 7, 13, 3, 25, 8, 19, 4, 28, 11, 16, 2, 30, 6, 22} // 227 bits/group

var gBitsBuffer [BitsBufferSize]byte
var gBitsVariants [NumVariants][BitsBufferSize]byte

func bitsMask(width int) uint32 {
	if width == 32 {
		return 0xFFFFFFFF
	}
	return (1 << width) - 1
}

// the per-pass value variation: one LCG step per pass, values from its bits
func varyBitsValues(values *[BitsNumWidths]uint32, rng uint64) {
	for i := 0; i < BitsNumWidths; i++ {
		values[i] = uint32(rng>>i) & bitsMask(bitsWidths[i])
	}
}

// the single untimed WriteBits call site (§3.2)
func bitsWritePass(buffer []byte, values *[BitsNumWidths]uint32) int64 {
	w := serialize.NewBitWriter(buffer)
	for w.BitsAvailable() >= 256 {
		for i := 0; i < BitsNumWidths; i++ {
			w.WriteBits(values[i], bitsWidths[i])
		}
	}
	w.FlushBits()
	return w.BytesWritten()
}

// the single untimed ReadBits call site (§3.2): the buffer must read back
// exactly the values written — the bits family's refusal gate
func bitsReadVerify(buffer []byte, values *[BitsNumWidths]uint32) bool {
	r := serialize.NewBitReader(buffer)
	for r.BitsRemaining() >= 256 {
		for i := 0; i < BitsNumWidths; i++ {
			if r.ReadBits(bitsWidths[i]) != values[i] {
				return false
			}
		}
	}
	return true
}

//go:noinline
func bitpackerWriteLoop(passes int64, bytesPerPass int64, rng *uint64, values *[BitsNumWidths]uint32) bool {
	w := serialize.NewBitWriter(gBitsBuffer[:])
	for pass := int64(0); pass < passes; pass++ {
		*rng = benchRng(*rng)
		varyBitsValues(values, *rng)
		w.Reset(gBitsBuffer[:])
		for w.BitsAvailable() >= 256 {
			for i := 0; i < BitsNumWidths; i++ {
				w.WriteBits(values[i], bitsWidths[i])
			}
		}
		w.FlushBits()
		if w.BytesWritten() != bytesPerPass {
			return false // the bytes_per_op assertion (§2.7)
		}
		gSink = gSink + uint64(w.BytesWritten())
	}
	return true
}

//go:noinline
func bitpackerReadLoop(passes int64) bool {
	r := serialize.NewBitReader(gBitsVariants[0][:])
	for pass := int64(0); pass < passes; pass++ {
		r.Reset(gBitsVariants[pass&(NumVariants-1)][:])
		sum := uint64(0)
		for r.BitsRemaining() >= 256 {
			for i := 0; i < BitsNumWidths; i++ {
				sum += uint64(r.ReadBits(bitsWidths[i]))
			}
		}
		gSink = gSink + sum
	}
	return true
}

func benchBitpacker(basePasses int64) {
	passes := basePasses
	var values [BitsNumWidths]uint32

	rng := uint64(1)
	bytesPerPass := int64(-1)
	for k := 0; k < NumVariants; k++ {
		rng = benchRng(rng)
		varyBitsValues(&values, rng)
		wrote := bitsWritePass(gBitsVariants[k][:], &values)
		if bytesPerPass < 0 {
			bytesPerPass = wrote
		}
		if wrote != bytesPerPass {
			fail("bitpacker", "variation changed bytes/pass — widths are the structure and must stay fixed")
			return
		}
		if !bitsReadVerify(gBitsVariants[k][:], &values) {
			fail("bitpacker", "read-back disagrees with written values — refusing to bench")
			return
		}
	}

	writeRates := make([]float64, gNumRuns)
	readRates := make([]float64, gNumRuns)

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !bitpackerWriteLoop(passes, bytesPerPass, &rng, &values) {
			fail("bitpacker", "bytes/pass changed in the timed loop (§2.7 assertion)")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			writeRates[run] = float64(passes) / elapsed
		}
	}

	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		if !bitpackerReadLoop(passes) {
			fail("bitpacker", "read loop failed")
			return
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			readRates[run] = float64(passes) / elapsed
		}
	}

	report("bitpacker", "write", passes, bytesPerPass, stats(writeRates), "bits")
	report("bitpacker", "read", passes, bytesPerPass, stats(readRates), "bits")
}
