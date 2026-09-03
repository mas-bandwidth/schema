// the tables bench — the Go runner.
//
// Measures ONE thing: a representative fixed table written and read on the
// TOLERANT WIRE (docs/SPEC-TABLES.md §3), through the generated table codec.
// That is the number a reader who knows protobuf or flatbuffers already has a
// comparison for, and it is the per-language release gate for the tables layer
// (bench/tables/README.md).
//
// It is a port of bench/tables/cpp/table_main.cpp — the reference
// implementation — and follows the same contract
// (BENCH-STANDARD.md): the committed variant corpus
// drives it, the golden gate runs before the clock, the loops are barriered
// against dead-code elimination, and the report is 1 warmup + 7 measured runs
// with the median beside min/max/spread.
//
// THIS FILE IS SHAPE-BLIND. It names the generated TYPE at one call site and
// nothing else: no field, no pinned value, no wire size. Shape knowledge lives
// in bench/corpus/BenchTable.schema, in the code generated from it, and in the
// committed data test/bench/table_main.cpp produced. `make shape-gate` holds
// that mechanically.
//
// WHAT GO SPELLS DIFFERENTLY, and the reason at each site. There is no
// `asm volatile` memory clobber: the write arm's buffer is a package-level
// array the codec writes THROUGH a pointer, so the store is already observable,
// and the returned length folds into a package-level sink beside a
// runtime.KeepAlive — which is what the C# leg does, for the same reason. And
// there is no optimisation level: the Go compiler has one configuration, so the
// CSV's opt column says `default` rather than naming a flag that does not
// exist.
//
// Output: a human table on stderr; with --csv, CSV v2 rows on stdout.
package main

import (
	"fmt"
	"os"
	"runtime"
	"sort"
	"strconv"
	"time"

	"benchtable"
)

// sink defeats dead-code elimination of the computed lengths. It is written
// every iteration and read once at the end.
var sink uint64

const (
	maxNumRuns  = 7     // median of 7 (N >= 5), after 1 warmup run
	numVariants = 64    // read-path variant buffers
	bufferSize  = 65536 // the ceiling the runner refuses past; the record size comes from the corpus
	// §2.7's variant stride, for the same reason and by the same arithmetic as
	// the type runner's: a power-of-two stride maps every head line into a
	// handful of L1 set groups and a memory-bound read then feels every
	// background conflict miss.
	variantStride = bufferSize + 64
)

var (
	numRuns    = maxNumRuns
	csv        = false
	wireDir    = "testdata/wire"
	variantDir = "bench/corpus/variants"
	failed     = false
)

// the CSV's own columns (BENCH-STANDARD.md §5.1). family `table` (§1.9): the
// tolerant table wire, a DIFFERENT wire over a different corpus, so a tools
// refusal to divide it against a `gen` row is correct and automatic. linkage
// pkg — the generated table codec is ordinary package code in this binary and
// names no runtime at all. checks contract — Go's bounds checks are on in every
// configuration and the reader's wire-contract validation is unconditional,
// which is §3.4's word for exactly this. opt default — Go has one optimisation
// configuration and no flag to name.
const csvSuffix = "pkg,contract,default,unknown"

var (
	csvRows       []string
	goldensLoaded = map[string][]byte{}
)

var (
	buffer   [bufferSize]byte
	twin     [bufferSize]byte
	variants []byte
)

func variant(k int) []byte {
	return variants[k*variantStride : k*variantStride+bufferSize]
}

func fnv1a64(h uint64, data []byte) uint64 {
	for _, b := range data {
		h ^= uint64(b)
		h *= 0x100000001b3
	}
	return h
}

func corpusID() string {
	names := make([]string, 0, len(goldensLoaded))
	for name := range goldensLoaded {
		names = append(names, name)
	}
	sort.Strings(names) // the C++ leg's std::map iterates in sorted basename order
	h := uint64(0xcbf29ce484222325)
	for _, name := range names {
		h = fnv1a64(h, []byte(name))
		h = fnv1a64(h, []byte{0})
		h = fnv1a64(h, goldensLoaded[name])
	}
	return fmt.Sprintf("%016x", h)
}

func fail(name, what string) {
	fmt.Fprintf(os.Stderr, "FAILED: %s: %s\n", name, what)
	failed = true
}

func flushCSV() {
	if !csv {
		return
	}
	if failed {
		// §1.5: a failing run emits NO rows.
		fmt.Fprintln(os.Stderr, "refusing to emit CSV rows from a failing run")
		return
	}
	id := corpusID()
	for _, row := range csvRows {
		fmt.Printf("%s,%s,table,%s\n", row, id, csvSuffix)
	}
}

func checkGolden(name string, data []byte) bool {
	path := wireDir + "/" + name + ".bin"
	expected, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "missing wire golden %s — run from the schema repo root (or pass --wire-dir)\n", path)
		return false
	}
	goldensLoaded[name+".bin"] = expected
	if len(expected) != len(data) || string(expected) != string(data) {
		fmt.Fprintf(os.Stderr, "WIRE GOLDEN MISMATCH: %s (%d golden vs %d actual bytes) — refusing to bench code that does not match the corpus\n",
			name, len(expected), len(data))
		return false
	}
	return true
}

type runStats struct {
	median, min, max, spreadPct float64
}

func stats(rates []float64) runStats {
	sorted := append([]float64(nil), rates...)
	sort.Float64s(sorted)
	s := runStats{median: sorted[len(sorted)/2], min: sorted[0], max: sorted[len(sorted)-1]}
	s.spreadPct = (s.max - s.min) / s.median * 100.0
	return s
}

func report(bench, path string, iters int64, bytesPerOp int64, s runStats) {
	mbps := s.median * float64(bytesPerOp) / (1024.0 * 1024.0)
	fmt.Fprintf(os.Stderr, "%-18s %-11s %10.3f M msg/s %10.1f MB/s   (min %.3f, max %.3f, spread %.1f%%)\n",
		bench, path, s.median/1e6, mbps, s.min/1e6, s.max/1e6, s.spreadPct)
	if csv {
		csvRows = append(csvRows, fmt.Sprintf("go,%s,%s,%d,%d,%d,%.0f,%.0f,%.0f,%.2f,%.2f",
			bench, path, iters, bytesPerOp, numRuns, s.median, s.min, s.max, mbps, s.spreadPct))
	}
}

// loadVariants loads <variant-dir>/<name>.variants.bin into the numVariants
// staggered slots and returns the record size, or -1. Records are fixed-width
// by construction — test/bench/table_main.cpp refuses to emit a corpus whose
// records differ — so the record size IS file size / numVariants.
func loadVariants(name string) int64 {
	path := variantDir + "/" + name + ".variants.bin"
	packed, err := os.ReadFile(path)
	if err != nil {
		fmt.Fprintf(os.Stderr, "missing variant data %s — run `make bench-table-corpus`, and run from the schema repo root (or pass --variant-dir)\n", path)
		return -1
	}
	if len(packed) == 0 || len(packed)%numVariants != 0 {
		fmt.Fprintf(os.Stderr, "variant data %s is %d bytes, not a multiple of %d records — refusing to bench data whose stride is not the record size\n",
			path, len(packed), numVariants)
		return -1
	}
	record := len(packed) / numVariants
	if record > bufferSize {
		fmt.Fprintf(os.Stderr, "variant data %s has %d-byte records, over the %d-byte buffer\n", path, record, bufferSize)
		return -1
	}
	variants = make([]byte, numVariants*variantStride)
	for k := 0; k < numVariants; k++ {
		copy(variant(k), packed[k*record:(k+1)*record])
	}
	goldensLoaded[name+".variants.bin"] = packed
	return int64(record)
}

// benchTable is the data-driven table driver.
//
// THE READ ARM RESETS BEFORE IT LOADS, and that is not overhead the runner
// added: the tolerant wire ELIDES a field at its default (§3), so `Load` fills
// only what actually rode and a reused instance would otherwise keep the
// previous record's values in the elided fields. Resetting is part of a correct
// read into reused storage, in every language, so it is inside the clock rather
// than hidden outside it.
func benchTable[T any](name, golden string, baseIters int64,
	reset func(*T), save func(*T, []byte) int64, load func(*T, []byte) bool) {
	iters := baseIters

	bytesPerOp := loadVariants(name)
	if bytesPerOp < 0 {
		failed = true
		return
	}

	// gate 1 (§1.5): variant 0 IS the pinned instance.
	if !checkGolden(golden, variant(0)[:bytesPerOp]) {
		failed = true
		return
	}

	// gate 2: every variant loads, re-saves, and comes back byte-identical at
	// the same length — before any clock starts.
	instances := make([]T, numVariants)
	for k := 0; k < numVariants; k++ {
		reset(&instances[k])
		if !load(&instances[k], variant(k)[:bytesPerOp]) {
			fail(name, "load of a variant failed")
			return
		}
		wrote := save(&instances[k], twin[:])
		if wrote != bytesPerOp || string(twin[:bytesPerOp]) != string(variant(k)[:bytesPerOp]) {
			fail(name, "variant round-trip bytes differ — refusing to bench a codec that does not reproduce the corpus")
			return
		}
	}

	writeRates := make([]float64, 0, numRuns)
	roundTripRates := make([]float64, 0, numRuns)

	// WRITE: save the 64 pre-loaded instances round-robin. Rotating the
	// instances is the §2.7 variation: the encoder never sees the same input
	// twice in a row, and bytes/op is constant by construction rather than by
	// assertion. The sink is the byte fold.
	for run := -1; run < numRuns; run++ {
		start := time.Now()
		for i := int64(0); i < iters; i++ {
			wrote := save(&instances[i&(numVariants-1)], buffer[:])
			if wrote != bytesPerOp {
				fail(name, "save failed in loop")
				return
			}
			runtime.KeepAlive(&buffer)
			sink += uint64(wrote)
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			writeRates = append(writeRates, float64(iters)/elapsed)
		}
	}

	// ROUND-TRIP: reset, load a variant buffer, then re-save what came out. The
	// load needs no sink discipline of its own — its output IS the save's
	// input, so every loaded field is observed by construction (§2.7's
	// read-side sink problem dissolved rather than equalized).
	var out T
	for run := -1; run < numRuns; run++ {
		start := time.Now()
		for i := int64(0); i < iters; i++ {
			reset(&out)
			if !load(&out, variant(int(i & (numVariants - 1)))[:bytesPerOp]) {
				fail(name, "load failed in loop")
				return
			}
			wrote := save(&out, buffer[:])
			if wrote != bytesPerOp {
				fail(name, "re-save failed in loop")
				return
			}
			runtime.KeepAlive(&buffer)
			sink += uint64(wrote)
		}
		elapsed := time.Since(start).Seconds()
		if run >= 0 {
			roundTripRates = append(roundTripRates, float64(iters)/elapsed)
		}
	}

	w := stats(writeRates)
	rt := stats(roundTripRates)
	report(name, "write", iters, bytesPerOp, w)
	report(name, "round_trip", iters, bytesPerOp, rt)

	// READ is DERIVED, never measured: round-trip time minus write time. It
	// prints for continuity and is NOT a CSV row — a derived number in the CSV
	// would be divided as if it had been measured (§2.9).
	readTime := 1.0/rt.median - 1.0/w.median
	if readTime > 0 {
		fmt.Fprintf(os.Stderr, "%-18s %-11s %10.3f M msg/s   (DERIVED: round-trip minus write, informational — not a measured row)\n",
			name, "read", 1e-6/readTime)
	}
}

func main() {
	args := os.Args[1:]
	for i := 0; i < len(args); i++ {
		switch {
		case args[i] == "--csv":
			csv = true
		case args[i] == "--wire-dir" && i+1 < len(args):
			i++
			wireDir = args[i]
		case args[i] == "--variant-dir" && i+1 < len(args):
			i++
			variantDir = args[i]
		case args[i] == "--round" && i+1 < len(args):
			i++
			if k, err := strconv.ParseInt(args[i], 10, 64); err != nil || k < 0 {
				fmt.Fprintf(os.Stderr, "--round takes a non-negative integer, got '%s'\n", args[i])
				os.Exit(1)
			}
			numRuns = 1
		default:
			fmt.Fprintf(os.Stderr, "usage: %s [--csv] [--round K] [--wire-dir <dir>] [--variant-dir <dir>]\n", os.Args[0])
			os.Exit(1)
		}
	}

	fmt.Fprintln(os.Stderr, "schema tables bench (go)")

	if csv {
		fmt.Println("lang,bench,path,iters,bytes_per_op,runs,median_msgs_per_sec,min_msgs_per_sec,max_msgs_per_sec,median_mb_per_sec,spread_pct,corpus_id,family,linkage,checks,opt,inline")
	}

	// The one measured shape, named once — the generated type at the call site
	// and nothing else about it (bench/SHAPE-GATE.allow).
	benchTable[benchtable.TableMixed](
		"bench_table", "bench_table", 400000,
		benchtable.TableMixedReset,
		benchtable.TableMixedSave,
		func(v *benchtable.TableMixed, b []byte) bool {
			var report benchtable.TableReport
			return benchtable.TableMixedLoad(v, b, &report) && !report.Malformed
		})

	flushCSV()

	if failed {
		fmt.Fprintf(os.Stderr, "TABLES BENCH FAILED (corpus_id %s)\n", corpusID())
		os.Exit(1)
	}

	fmt.Fprintf(os.Stderr, "OK (corpus_id %s)\n", corpusID())
	_ = sink
}
