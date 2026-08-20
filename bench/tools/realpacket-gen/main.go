// realpacket-gen — emits the BENCH-STANDARD §1.7 realistic snapshot schema.
//
// §1.7's law: the corpus owes a realistic packet shape — 1000-2000 wire bits
// from hundreds of individually serialized small scalar fields of all scalar
// kinds, in a seeded-random mixed order, generated ONCE and then PINNED like
// every golden. This tool is that generator. It is deterministic: the same
// seed and field count produce the same bytes, forever — the RNG is the
// estate's own Knuth MMIX LCG, not a standard-library source whose sequence
// could move under a toolchain bump.
//
// The emitted shape (decided with Glenn, 2026-08-16):
//   - one flat type (package realworld, type RealPacket) of individually
//     serialized small scalar fields: ranged ints (2-21 bit widths, mixed
//     signed/unsigned), bits(N) with N in 1..32 plus one 48 and one 64, bool,
//     float32, compressed float (finite min/max/resolution triples), fixed
//     and ufixed (legal I+F storage widths, in-range bounds), float64,
//     full-width uint64/int64, and enum/flags fields referencing one small
//     enum and one small flags declared in the same file
//   - exactly four bool-gated if branches whose bodies hold 2-5 fields each;
//     the gate bools are STRUCTURE (§2.7): fixed defaults, held fixed during
//     variation so bytes/op is constant — two gates default true, two false,
//     so both branch outcomes are exercised
//   - ZERO string, bytes, wstring, or [N]uint8 fields: bulk share 0% by bits
//     (§1.7 rule 4)
//
// The tool computes the exact wire bits of the pinned all-defaults instance
// (every field at its declared default; branch bodies count only under gates
// whose default is true) using the compiler's own width formulas
// (internal/ir: BitsRequired, CompressedFloatBits) — one implementation, so
// the number printed here cannot drift from what the compiler advertises. It
// REFUSES to emit outside the hard bounds [1000, 2000] bits.
//
// Output is canonical schema fmt form (internal/format), so the pinned file
// is byte-stable under every schema command's format-in-place pass.
package main

import (
	"flag"
	"fmt"
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/internal/format"
	"github.com/mas-bandwidth/schema/ir"
)

const (
	hardMinBits = 1000 // §1.7 hard bounds — refuse outside
	hardMaxBits = 2000
	aimMinBits  = 1400 // the design's aim — warn outside
	aimMaxBits  = 1800
)

// ---- deterministic RNG: the estate's Knuth MMIX LCG ----

type lcg struct{ s uint64 }

func (r *lcg) next() uint64 {
	r.s = r.s*6364136223846793005 + 1442695040888963407
	return r.s
}

// n returns a value in [0, n). The LCG's low bits are weak; use the high half.
func (r *lcg) n(n int) int { return int((r.next() >> 33) % uint64(n)) }

// between returns a value in [lo, hi], inclusive.
func (r *lcg) between(lo, hi int) int { return lo + r.n(hi-lo+1) }

// ---- field specs ----

type field struct {
	kind string // name suffix: fNNN_<kind>
	decl string // everything after the name: type, attributes
	bits int64  // exact wire bits, from the compiler's own formulas
}

func genRangedInt(r *lcg) field {
	w := r.between(2, 21)
	if r.n(2) == 0 {
		// signed symmetric range [-h, h] with bitlen(2h) == w
		lo := int64(1) << uint(w-2)
		hi := int64(1)<<uint(w-1) - 1
		h := int64(r.between(int(lo), int(hi)))
		st := "int32"
		switch {
		case w <= 8:
			st = "int8"
		case w <= 16:
			st = "int16"
		}
		bits := ir.BitsRequired(big.NewInt(-h), big.NewInt(h))
		return field{"int", fmt.Sprintf("%s [min = %d, max = %d]", st, -h, h), bits}
	}
	// unsigned range [0, m] with bitlen(m) == w
	lo := int64(1) << uint(w-1)
	hi := int64(1)<<uint(w) - 1
	m := int64(r.between(int(lo), int(hi)))
	st := "uint32"
	switch {
	case w <= 8:
		st = "uint8"
	case w <= 16:
		st = "uint16"
	}
	bits := ir.BitsRequired(big.NewInt(0), big.NewInt(m))
	return field{"uint", fmt.Sprintf("%s [min = 0, max = %d]", st, m), bits}
}

func genBits(r *lcg) field {
	n := r.between(1, 32)
	return field{"bits", fmt.Sprintf("bits(%d)", n), int64(n)}
}

// flit renders a float attribute value as a schema float literal.
func flit(v float64) string {
	s := strconv.FormatFloat(v, 'g', -1, 64)
	if !strings.ContainsAny(s, ".e") {
		s += ".0"
	}
	return s
}

var cfRanges = []float64{1, 2, 5, 10, 30, 45, 90, 100, 180, 250, 500, 1000, 2000, 5000, 10000}
var cfRes = []float64{0.001, 0.01, 0.02, 0.05, 0.1, 0.25, 0.5, 1.0}

func genCompressed(r *lcg) field {
	for {
		rge := cfRanges[r.n(len(cfRanges))]
		res := cfRes[r.n(len(cfRes))]
		var min, max float64
		if r.n(2) == 0 {
			min, max = -rge, rge
		} else {
			min, max = 0, rge
		}
		bits := ir.CompressedFloatBits(min, max, res)
		if bits < 4 || bits > 20 {
			continue // keep the field small, per the design; redraw deterministically
		}
		return field{"cf32", fmt.Sprintf("float32 [min = %s, max = %s, resolution = %s]",
			flit(min), flit(max), flit(res)), bits}
	}
}

// Q formats with I+F equal to a legal storage width (8/16/32), I >= 1.
var qFormats = [][2]int{
	{1, 7}, {2, 6}, {4, 4}, {4, 12}, {6, 10}, {8, 8}, {2, 14}, {10, 6},
	{12, 4}, {16, 16}, {8, 24}, {12, 20}, {20, 12}, {24, 8},
}

func genFixed(r *lcg, unsigned bool) field {
	for {
		q := qFormats[r.n(len(qFormats))]
		I, F := q[0], q[1]
		wireCap := 28 - F // keep bitlen(range) + F small — these are small scalars
		if wireCap < 1 {
			continue
		}
		if unsigned {
			dom := int64(1)<<uint(I) - 1
			capv := min(int64(1)<<uint(wireCap)-1, dom)
			if capv < 1 {
				continue
			}
			m := int64(r.between(1, int(capv)))
			bits := ir.BitsRequired(big.NewInt(0), big.NewInt(m)) + int64(F)
			return field{"ufixed", fmt.Sprintf("ufixed(%d, %d) [min = 0, max = %d]", I, F, m), bits}
		}
		if I < 2 {
			continue // signed whole-unit domain needs I >= 2 for a symmetric range
		}
		dom := int64(1)<<uint(I-1) - 1
		capv := min(
			// bitlen(2h) <= wireCap
			int64(1)<<uint(wireCap-1)-1, dom)
		if capv < 1 {
			continue
		}
		h := int64(r.between(1, int(capv)))
		bits := ir.BitsRequired(big.NewInt(-h), big.NewInt(h)) + int64(F)
		return field{"fixed", fmt.Sprintf("fixed(%d, %d) [min = %d, max = %d]", I, F, -h, h), bits}
	}
}

// ---- the enum and flags the packet references ----

var enumVariants = []string{"Idle", "Active", "Combat", "Docked", "Warping"}
var flagsVariants = []string{"Shielded", "Cloaked", "Overheated", "LowPower", "Jamming"}

// stats records everything the generator knows about the emitted shape, so
// the caller (and the determinism test) can assert it rather than re-derive it.
type stats struct {
	declCount   int      // serialized field declarations, gates included
	valueFields int      // declarations excluding the four gates
	totalBits   int64    // exact pinned all-defaults wire bits (taken branches only)
	untakenBits int64    // bits of branch bodies under false gates (the MaxBits residual)
	gates       []string // "fNNN_bool = true/false" in declaration order
	mixLine     string
}

func generate(seed uint64, nFields int) ([]byte, stats, error) {
	r := &lcg{s: seed}
	round := func(x float64) int { return int(math.Round(x)) }
	n := float64(nFields)

	nRint := round(0.30 * n)
	nBits := round(0.20 * n) // includes the one bits(48) and one bits(64)
	nBool := round(0.12 * n) // includes the four branch gates
	nF32 := round(0.10 * n)
	nCf := round(0.08 * n)
	nFx := round(0.08 * n) // alternating fixed/ufixed
	nF64 := round(0.05 * n)
	nW64 := round(0.04 * n) // alternating uint64/int64
	nEf := max(round(0.03*n),
		// at least two enum refs and one flags ref
		3)
	if nBool < 5 || nBits < 3 {
		return nil, stats{}, fmt.Errorf("field count too small: need at least 5 bools (4 gates) and 3 bits fields")
	}

	// build the value-field list (the four gate bools are placed separately)
	var list []field
	for range nRint {
		list = append(list, genRangedInt(r))
	}
	for i := 0; i < nBits-2; i++ {
		list = append(list, genBits(r))
	}
	list = append(list, field{"bits", "bits(48)", 48})
	list = append(list, field{"bits", "bits(64)", 64})
	for i := 0; i < nBool-4; i++ {
		list = append(list, field{"bool", "bool", 1})
	}
	for range nF32 {
		list = append(list, field{"f32", "float32", 32})
	}
	for range nCf {
		list = append(list, genCompressed(r))
	}
	for i := range nFx {
		list = append(list, genFixed(r, i%2 == 1))
	}
	for range nF64 {
		list = append(list, field{"f64", "float64", 64})
	}
	for i := range nW64 {
		if i%2 == 0 {
			list = append(list, field{"u64", "uint64", 64})
		} else {
			list = append(list, field{"i64", "int64", 64})
		}
	}
	enumBits := ir.BitsRequired(big.NewInt(0), big.NewInt(int64(len(enumVariants))))
	flagsBits := int64(len(flagsVariants))
	for i := range nEf {
		if i%2 == 0 {
			list = append(list, field{"enum", "PacketMode", enumBits})
		} else {
			list = append(list, field{"flags", "PacketFlags", flagsBits})
		}
	}

	// seeded-random mixed order (Fisher-Yates)
	for i := len(list) - 1; i > 0; i-- {
		j := r.n(i + 1)
		list[i], list[j] = list[j], list[i]
	}

	// carve exactly four branches, one per quarter, bodies of 2-5 consecutive
	// fields; two gates true, two false, assignment shuffled
	L := len(list)
	Q := L / 4
	taken := []bool{true, true, false, false}
	for i := len(taken) - 1; i > 0; i-- {
		j := r.n(i + 1)
		taken[i], taken[j] = taken[j], taken[i]
	}
	type branch struct {
		pos, size int
		taken     bool
	}
	var branches [4]branch
	for q := range 4 {
		s := r.between(2, 5)
		maxOff := Q - s - 1
		if maxOff < 1 {
			return nil, stats{}, fmt.Errorf("field count too small to carve four 2-5 field branches")
		}
		branches[q] = branch{pos: q*Q + 1 + r.n(maxOff), size: s, taken: taken[q]}
	}

	// ---- emit the type body, accounting exact pinned wire bits as we go ----
	var body strings.Builder
	counter := 0
	declCount := 0
	totalBits := int64(0)
	untakenBits := int64(0)
	var gateDesc []string
	bitsWord := func(b int64) string {
		if b == 1 {
			return "1 bit"
		}
		return fmt.Sprintf("%d bits", b)
	}
	emit := func(f field, depth int, rides bool) {
		counter++
		declCount++
		name := fmt.Sprintf("f%03d_%s", counter, f.kind)
		note := ""
		if rides {
			totalBits += f.bits
		} else {
			untakenBits += f.bits
			note = " (does not ride: gate false)"
		}
		fmt.Fprintf(&body, "%s%s %s // %s%s\n", strings.Repeat("    ", depth), name, f.decl, bitsWord(f.bits), note)
	}
	i := 0
	for q := range 4 {
		br := branches[q]
		for ; i < br.pos; i++ {
			emit(list[i], 1, true)
		}
		counter++
		declCount++
		gname := fmt.Sprintf("f%03d_bool", counter)
		totalBits++ // the gate bool itself always rides
		gateDesc = append(gateDesc, fmt.Sprintf("%s = %v", gname, br.taken))
		fmt.Fprintf(&body, "    %s bool = %v // 1 bit — branch gate: STRUCTURE (§2.7), held fixed during variation\n", gname, br.taken)
		fmt.Fprintf(&body, "    if %s {\n", gname)
		for k := 0; k < br.size; k++ {
			emit(list[i], 2, br.taken)
			i++
		}
		fmt.Fprintf(&body, "    }\n")
	}
	for ; i < L; i++ {
		emit(list[i], 1, true)
	}

	totalBytes := (totalBits + 7) / 8

	// ---- §1.7 gate: refuse outside the hard bounds ----
	if totalBits < hardMinBits || totalBits > hardMaxBits {
		return nil, stats{}, fmt.Errorf("REFUSING to emit: pinned wire is %d bits, outside the §1.7 hard bounds [%d, %d] — tune -fields",
			totalBits, hardMinBits, hardMaxBits)
	}
	if totalBits < aimMinBits || totalBits > aimMaxBits {
		fmt.Fprintf(os.Stderr, "warning: pinned wire is %d bits, outside the design aim [%d, %d]\n",
			totalBits, aimMinBits, aimMaxBits)
	}

	// ---- assemble the file ----
	var f strings.Builder
	fmt.Fprintf(&f, `// RealWorld.schema — BENCH-STANDARD §1.7's realistic snapshot shape: one flat
// packet of individually serialized small scalar fields in seeded-random mixed
// order. ZERO bulk — no string, bytes, wstring, or [N]uint8 fields, so the
// bulk share is 0%% of wire bits (§1.7 rule 4): every wire bit here flows
// through an individual serialize statement, which is the work this estate
// exists to measure.
//
//   generator:   bench/tools/realpacket-gen
//                (go run ./bench/tools/realpacket-gen -seed %d -fields %d)
//   seed:        %d
//   fields:      %d serialized field declarations (incl. 4 branch gate bools)
//   pinned wire: %d bits = %d bytes — the all-defaults instance: every field
//                at its declared default (zero / false / None / 0.0); the four
//                branch gates carry declared defaults (%s)
//                and are STRUCTURE (§2.7): held fixed during variation, so
//                bytes/op is constant. Branch bodies under a false gate do not
//                ride the pinned wire.
//   bulk share:  0%% by bits (§1.7 rule 4)
//
// PINNED, NEVER REGENERATED (§1.7 rules 1 and 3): this file is a golden. Any
// change — reseeding, retuning, editing a field — is a NEW file under a NEW
// name with its own corpus_id; regenerating in place would silently re-price
// every cross-era comparison.

package realworld

// The one small enum the packet's enum fields reference: 5 variants, wire
// range [0, 5], 3 bits.
enum PacketMode { %s }

// The one small flags the packet's flags fields reference: 5 named bits.
flags PacketFlags { %s }

type RealPacket {
%s}
`,
		seed, nFields, seed,
		declCount,
		totalBits, totalBytes,
		strings.Join(gateDesc, ", "),
		strings.Join(enumVariants, ", "),
		strings.Join(flagsVariants, ", "),
		body.String())

	// canonicalize: the pinned file must be byte-stable under schema fmt
	formatted, err := format.Format("RealWorld.schema", []byte(f.String()))
	if err != nil {
		return nil, stats{}, fmt.Errorf("generated schema does not format: %w\n---- raw ----\n%s", err, f.String())
	}

	return formatted, stats{
		declCount:   declCount,
		valueFields: L,
		totalBits:   totalBits,
		untakenBits: untakenBits,
		gates:       gateDesc,
		mixLine: fmt.Sprintf("%d rint, %d bits, %d bool, %d f32, %d cf32, %d fixed/ufixed, %d f64, %d w64, %d enum/flags",
			nRint, nBits, nBool, nF32, nCf, nFx, nF64, nW64, nEf),
	}, nil
}

func main() {
	seed := flag.Uint64("seed", 20260816, "generator seed (the fixed default is the §1.7 decision date)")
	nFields := flag.Int("fields", 95, "target serialized field declarations, tuned so the default seed lands in 1400-1800 bits")
	out := flag.String("o", "-", "output path (- = stdout)")
	flag.Parse()

	formatted, st, err := generate(*seed, *nFields)
	if err != nil {
		fatal(err.Error())
	}

	if *out == "-" {
		if _, err := os.Stdout.Write(formatted); err != nil {
			fatal(err.Error())
		}
	} else {
		if err := os.WriteFile(*out, formatted, 0644); err != nil {
			fatal(err.Error())
		}
	}

	fmt.Fprintf(os.Stderr, "seed:        %d\n", *seed)
	fmt.Fprintf(os.Stderr, "fields:      %d declarations (%d value fields + 4 gates)\n", st.declCount, st.valueFields)
	fmt.Fprintf(os.Stderr, "pinned wire: %d bits = %d bytes (all-defaults instance, gates %s)\n",
		st.totalBits, (st.totalBits+7)/8, strings.Join(st.gates, ", "))
	fmt.Fprintf(os.Stderr, "mix:         %s\n", st.mixLine)
}

func fatal(msg string) {
	fmt.Fprintln(os.Stderr, "realpacket-gen: "+msg)
	os.Exit(1)
}
