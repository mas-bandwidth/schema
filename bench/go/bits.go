// schema bench — family bits for the Go runner.
//
// Family bits (BENCH-STANDARD.md §1.4): the raw BitWriter/BitReader with the
// 16-width table (227 bits/group) over a 65536-byte buffer. Values vary per
// pass through the LCG (widths are the structure and stay fixed; bytes/pass
// asserted constant); reads rotate 64 pre-written variant buffers, each
// verified to read back exactly what was written before any number is
// produced. Unexported identifiers in the runner's own package per §3.1;
// the timed loops are //go:noinline so the §4.1 verdict has a loop body to
// count.
package main

import (
	"time"

	"github.com/mas-bandwidth/serialize.go"
)

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
