// schema bench — the Go runner.
//
// A port of bench/cpp/bench_main.cpp (the reference implementation) against
// the generated Go package and the serialize.go runtime: same benchmark set,
// same variant corpus, same golden + per-variant round-trip self-checks (a
// mismatch REFUSES to bench), same warmup + 7 measured runs +
// median/min/max/spread, same CSV row format with lang=go. See
// bench/README.md for the runner contract.
//
// Language-specific discipline:
//   - escape barriers: a package-level sink accumulator plus runtime.KeepAlive
//     on the decoded value (the stub's sanctioned equivalents of the C++
//     empty-asm clobber)
//   - streams are reused via Reset (the runtime's documented no-allocation
//     reuse path) — the Go equivalent of C++'s free stack construction
//   - the driver passes write/read as function values (one indirect call per
//     op, same as the Go way of writing this loop; Rust and C++ get this
//     inlined via generics — noted in the results)
//
// Run from bench/go (run.sh does): the wire goldens are at ../../testdata/wire.
package main

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"runtime/pprof"
	"sort"
	"strconv"
	"time"

	"bench"

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
var gVariantDir = "../../bench/corpus/variants"
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
// family is per ROW now (gen | bits — §5.1); linkage/checks/opt/inline
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
// the DATA-DRIVEN benchmark driver (issue #191)
// ------------------------------------------------------------------------------------------
//
// THE PROPERTY: nothing below names a field of the shape it measures. Shape
// knowledge lives in the committed variant DATA (bench/corpus/variants,
// emitted by bench/tools/variantgen) and in the generated codec, and nowhere
// else — so this driver cannot drift from another language's driver in what
// it measures, which is the whole reason the design exists. If a change here
// ever needs a field name, the design has failed and that is the finding.

// loadVariants loads <variant-dir>/<name>.variants.bin into the NumVariants
// §2.7-staggered slots and returns the record size, or -1. The records are
// fixed-width by construction (§2.7 pins every structure field), so the file
// needs no index: the record size IS file size / NumVariants, and a file that
// does not divide evenly is a refusal.
func loadVariants(name string) int64 {
	path := gVariantDir + "/" + name + ".variants.bin"
	packed, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "missing variant data %s — run `make bench-variants`, and run the bench from bench/go (or pass --variant-dir)\n", path)
		return -1
	}
	if len(packed) == 0 || len(packed)%NumVariants != 0 {
		fmt.Fprintf(os.Stderr, "variant data %s is %d bytes, not a multiple of %d records — refusing to bench data whose stride is not the record size\n",
			path, len(packed), NumVariants)
		return -1
	}
	record := len(packed) / NumVariants
	if record > BufferSize {
		fmt.Fprintf(os.Stderr, "variant data %s has %d-byte records, over the %d-byte buffer\n", path, record, BufferSize)
		return -1
	}
	for k := 0; k < NumVariants; k++ {
		copy(gVariants[k][:], packed[k*record:(k+1)*record])
	}
	// The variant data is corpus (§1.6): it defines the work inside the timed
	// loops, so it rides in corpus_id exactly as the wire goldens do. A run
	// against drifted variant data reports a different id and the tools refuse
	// the ratio, instead of publishing a number for different work.
	gGoldensLoaded[filepath.Base(path)] = packed
	return int64(record)
}

// T — the generated message type — is named explicitly at the call site, as
// in the C++ reference. A TYPE name is not a field name; the driver still
// knows nothing about the shape's contents.
func benchDataDriven[T any](name, golden string, iters int64,
	writeFn func(*serialize.WriteStream, *T) error,
	readFn func(*serialize.ReadStream, *T) error) {

	bytesPerOp := loadVariants(name)
	if bytesPerOp < 0 {
		failed = true
		return
	}

	// gate 1 (§1.5): variant 0 IS the pinned instance, so the whole variant
	// file is bound to the wire golden by one byte-compare.
	if !checkGolden(golden, gVariants[0][:bytesPerOp]) {
		failed = true
		return
	}

	// gate 2: every variant decodes, re-encodes, and comes back byte-identical
	// at the same length. This is stronger than the pinned-instance-only gate
	// benchMessage applies — §1.5's named residual (the 64 varied buffers
	// length-checked but never value-checked) closes here, for every variant.
	instances := make([]T, NumVariants)
	for k := 0; k < NumVariants; k++ {
		rs := serialize.NewReadStream(gVariants[k][:bytesPerOp])
		if err := readFn(rs, &instances[k]); err != nil {
			fail(name, "decode of a variant failed")
			return
		}
		ws := serialize.NewWriteStream(gTwin[:])
		if err := writeFn(ws, &instances[k]); err != nil {
			fail(name, "re-encode of a decoded variant failed")
			return
		}
		ws.Flush()
		if int64(ws.BytesProcessed()) != bytesPerOp ||
			!bytes.Equal(gTwin[:bytesPerOp], gVariants[k][:bytesPerOp]) {
			fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus")
			return
		}
	}

	writeRates := make([]float64, gNumRuns)
	roundtripRates := make([]float64, gNumRuns)

	// WRITE: encode the 64 pre-decoded instances round-robin. Rotating the
	// instances is what §2.7's per-iteration LCG mutation bought — the encoder
	// never sees the same input twice in a row and cannot precompute scratch
	// words — with none of the per-language mutation code, and with bytes/op
	// constant by construction rather than by assertion. The sink is the byte
	// fold: every iteration's result is a value the loop cannot drop. The
	// stream is reused via Reset, the runtime's documented no-allocation path.
	ws := serialize.NewWriteStream(gBuffer[:])
	for run := -1; run < gNumRuns; run++ {
		start := time.Now()
		for i := int64(0); i < iters; i++ {
			ws.Reset(gBuffer[:])
			if err := writeFn(ws, &instances[i&(NumVariants-1)]); err != nil {
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

	// ROUND-TRIP: decode a variant buffer, then re-encode what came out. The
	// decode needs no sink discipline of its own — its output IS the encode's
	// input, so every decoded field is observed by construction, in every
	// language, with no per-language fold to audit (§2.7's read-side sink
	// problem dissolved rather than equalized). The decode target is hoisted
	// and reused, as everywhere else.
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
			ws.Reset(gBuffer[:])
			if err := writeFn(ws, &out); err != nil {
				fail(name, "re-write failed in loop")
				return
			}
			ws.Flush()
			gSink = gSink + uint64(ws.BytesProcessed())
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			roundtripRates[run] = float64(iters) / elapsed
		}
	}
	runtime.KeepAlive(gBuffer[:])

	w := stats(writeRates)
	rt := stats(roundtripRates)
	report(name, "write", iters, bytesPerOp, w, "gen")
	report(name, "round_trip", iters, bytesPerOp, rt, "gen")

	// READ is DERIVED, never measured: round-trip time minus write time. It
	// prints for continuity with the read rows the rest of the corpus still
	// reports and is NOT a CSV row — a derived number in the CSV would be
	// divided as if it had been measured.
	readTime := 1.0/rt.median - 1.0/w.median
	if readTime > 0 {
		fmt.Fprintf(os.Stderr, "%-18s %-5s %10.2f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n",
			name, "read", 1e-6/readTime)
	}

	// alloc note (proof of the reuse discipline, not a benchmark): allocs
	// during one extra untimed pass of each path — must be 0
	const allocOps = 4 * NumVariants
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	for i := int64(0); i < allocOps; i++ {
		ws.Reset(gBuffer[:])
		if err := writeFn(ws, &instances[i&(NumVariants-1)]); err != nil {
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
		ws.Reset(gBuffer[:])
		if err := writeFn(ws, &out); err != nil {
			fail(name, "re-write failed in alloc pass")
			return
		}
		ws.Flush()
		gSink = gSink + uint64(ws.BytesProcessed())
	}
	runtime.ReadMemStats(&after)
	roundtripAllocs := after.Mallocs - before.Mallocs
	fmt.Fprintf(os.Stderr, "alloc note: %s one pass (%d ops/path): write %d allocs, round_trip %d allocs\n",
		name, allocOps, writeAllocs, roundtripAllocs)
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
		case args[i] == "--variant-dir" && i+1 < len(args):
			i++
			gVariantDir = args[i]
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
		case args[i] == "--cpuprofile" && i+1 < len(args):
			// The iteration instrument, never a published one: it changes
			// nothing about what is measured, and no timed row is taken
			// under it. It is how the Go codec's ~86%-in-the-runtime
			// conviction was obtained, so it lives beside the leg it
			// profiles rather than in a scratch fork of it.
			i++
			pf, perr := os.Create(args[i])
			if perr != nil {
				fmt.Fprintf(os.Stderr, "--cpuprofile: %v\n", perr)
				os.Exit(1)
			}
			if perr := pprof.StartCPUProfile(pf); perr != nil {
				fmt.Fprintf(os.Stderr, "--cpuprofile: %v\n", perr)
				os.Exit(1)
			}
			defer pprof.StopCPUProfile()
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [--csv] [--round K] [--quick] [--wire-dir <dir>] [--variant-dir <dir>] [--cpuprofile <file>]\n", os.Args[0])
			os.Exit(1)
		}
	}
	if gQuick && gNumRuns == MaxNumRuns {
		gNumRuns = 3
	}

	if gQuick {
		// --quick: the iteration instrument, never the certification
		// instrument — bench_mixed only, 3 measured runs.
		fmt.Fprintf(os.Stderr, "schema bench (go, --quick: iteration instrument, not certification)\n")
	} else {
		fmt.Fprintf(os.Stderr, "schema bench (go)\n")
	}

	// family gen over the Bench corpus: BenchMixed through the generated code,
	// fed by the committed variant corpus — same goldens, same iteration count
	// in every runner (§2.1). No hand-written pin, vary or sink code
	// participates in this leg.
	benchDataDriven[bench.BenchMixed]("bench_mixed", "bench_mixed", 4000000, bench.WriteBenchMixed, bench.ReadBenchMixed)

	// family bits (§1.4): the one bitpacker workload in the estate
	if !gQuick {
		benchBitpacker(24576)
	}

	flushCsv() // rows carry the corpus_id of the goldens this run loaded

	if failed {
		fmt.Fprintf(os.Stderr, "BENCH FAILED (corpus_id %s)\n", corpusID())
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "OK (corpus_id %s)\n", corpusID())
	_ = gSink
}
