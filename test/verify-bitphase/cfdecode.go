// THE COMPRESSED-FLOAT DECODE CASE (SPEC.md §4.6, "The decode is
// normative"). Every leg's driver reads BitPhase.schema's CfDecode at every
// quantum index, 0 to cfDecodeSteps, from the two bytes [i & 0xff, i >> 8],
// and prints one line per index for each tier it carries:
//
//	CFDECODE <tier> <index> <bits>
//
// where <bits> is 16 hex digits, the float64 bits of the value read, or 8, the
// float32 pattern itself (moo holds a float32 as its binary32 pattern and has
// no double to widen it to).
//
// The harness computes the decode itself, q = f32(i) / f32(count), then
// p = q * delta, then x = p + min, each one float32 operation, and holds every
// tier's answer to it bit for bit. A tier that leaves an index unanswered,
// answers one twice or answers outside the range, a tier the leg does not
// carry, and a line outside the grammar all fail by name: no leg passes by
// printing less.
//
// min = -3.3 is not a float32. The decode adds float32(-3.3), which is
// -3.299999952316284, and a reader that adds the double -3.3 instead is one bit
// off at 2,535 of the 11,001 indices, index 2 first (0xc053126f where the decode
// gives 0xc053126e). The JavaScript flat tier, Dart and Elixir did exactly that
// until their generators spelled min as its float32.
package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
)

// The CfDecode declaration of BitPhase.schema: min, max and the step count
// ir.CompressedFloatParams derives from them and resolution 0.001
// (TestCfDecodeDeclarationAgrees holds the three to the schema).
const (
	cfDecodeMin   = -3.3
	cfDecodeMax   = 7.7
	cfDecodeRes   = 0.001
	cfDecodeSteps = 11000
)

// cfDecodeStepsToken is the placeholder a driver source carries for the last
// index it reads; withCfDecodeSteps fills it in, with the float32 literal
// case's tokens (f32lit.go), since every driver source passes through it.
const cfDecodeStepsToken = "@CFDECODE_STEPS@"

func withCfDecodeSteps(src string) string {
	return withF32LitTokens(strings.ReplaceAll(src, cfDecodeStepsToken, strconv.Itoa(cfDecodeSteps)))
}

// cfDecodeSpec is SPEC.md §4.6's decode of index i, each step one float32
// operation: the explicit conversions round every step and forbid fusing the
// multiply and the add (Go spec, "Floating-point operators").
func cfDecodeSpec(i int) float32 {
	minF := float32(cfDecodeMin)
	maxF := float32(cfDecodeMax)
	delta := float32(maxF - minF)
	q := float32(float32(i) / float32(cfDecodeSteps))
	p := float32(q * delta)
	return float32(p + minF)
}

// cfDecodeTiers names the tiers a leg answers for: JavaScript carries the
// stream tier and the flat tier, every other leg one tier named for itself.
func cfDecodeTiers(lang string) []string {
	if lang == "js" {
		return []string{"stream", "flat"}
	}
	return []string{lang}
}

// stripCfDecode drops the CFDECODE lines, so a failure prints what else the
// driver said rather than 11,001 lines per tier.
func stripCfDecode(out string) string {
	var keep []string
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "CFDECODE") {
			keep = append(keep, line)
		}
	}
	return strings.Join(keep, "\n")
}

// checkCfDecode holds a driver's CFDECODE lines to the decode: every tier the
// leg carries answers every index once, with the float32 the decode gives.
func checkCfDecode(lang, out string) error {
	tiers := cfDecodeTiers(lang)
	answers := map[string]map[int]uint64{}
	for _, t := range tiers {
		answers[t] = map[int]uint64{}
	}
	var bad []string
	for n, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "CFDECODE") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "CFDECODE" || (len(f[3]) != 16 && len(f[3]) != 8) {
			bad = append(bad, fmt.Sprintf("line %d is not CFDECODE <tier> <index> <16 or 8 hex digits>: %q", n+1, line))
			continue
		}
		got, ok := answers[f[1]]
		if !ok {
			bad = append(bad, fmt.Sprintf("line %d answers the tier %q, and %s carries %s", n+1, f[1], lang, strings.Join(tiers, ", ")))
			continue
		}
		i, err := strconv.Atoi(f[2])
		if err != nil || i < 0 || i > cfDecodeSteps {
			bad = append(bad, fmt.Sprintf("line %d answers the index %q, outside 0..%d", n+1, f[2], cfDecodeSteps))
			continue
		}
		bits, err := strconv.ParseUint(f[3], 16, 64)
		if err != nil {
			bad = append(bad, fmt.Sprintf("line %d: %q is not hex digits", n+1, f[3]))
			continue
		}
		if len(f[3]) == 8 {
			// the float32 pattern: widen it here, exactly, as the other legs do
			bits = math.Float64bits(float64(math.Float32frombits(uint32(bits))))
		}
		if _, dup := got[i]; dup {
			bad = append(bad, fmt.Sprintf("line %d answers %s index %d a second time", n+1, f[1], i))
			continue
		}
		got[i] = bits
	}
	for _, t := range tiers {
		var missing []int
		var wrong []string
		nWrong := 0
		for i := 0; i <= cfDecodeSteps; i++ {
			bits, ok := answers[t][i]
			if !ok {
				missing = append(missing, i)
				continue
			}
			want := cfDecodeSpec(i)
			v := math.Float64frombits(bits)
			if float64(float32(v)) != v && !math.IsNaN(v) {
				nWrong++
				if len(wrong) < 5 {
					wrong = append(wrong, fmt.Sprintf("index %d read %016x, which is not a float32 (the decode gives float32 %08x)", i, bits, math.Float32bits(want)))
				}
				continue
			}
			if math.Float32bits(float32(v)) != math.Float32bits(want) {
				nWrong++
				if len(wrong) < 5 {
					wrong = append(wrong, fmt.Sprintf("index %d read float32 %08x, the decode gives %08x", i, math.Float32bits(float32(v)), math.Float32bits(want)))
				}
			}
		}
		if len(missing) > 0 {
			sort.Ints(missing)
			show := missing
			if len(show) > 5 {
				show = show[:5]
			}
			bad = append(bad, fmt.Sprintf("tier %s leaves %d of %d indices unanswered (first %v)", t, len(missing), cfDecodeSteps+1, show))
		}
		if nWrong > 0 {
			bad = append(bad, fmt.Sprintf("tier %s decodes %d of %d indices off SPEC §4.6's float32 decode: %s", t, nWrong, cfDecodeSteps+1, strings.Join(wrong, "; ")))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("FAILED: cfdecode %s:\n  %s", lang, strings.Join(bad, "\n  "))
	}
	return nil
}
