// THE FLOAT32 LITERAL CASE (SPEC.md §4.3, "A FLOAT LITERAL IS A float64
// FIRST, EVERYWHERE"). A float32 constant, default or range end is the float64
// of its declaration rounded once more to float32: 0.1 declares
// float32(0.1) = 0.10000000149011612, and -0.3 declares
// float32(-0.3) = -0.30000001192092896. A target whose float literals are
// doubles (JavaScript, Dart, Elixir, Lua, Luau and the rest) reads the decimal
// 0.1 as the double 0.1, which is no float32, so a generator that spells a
// float32 as its shortest decimal gives that target the wrong value.
//
// Every leg's driver answers five probes of BitPhase.schema's F32Const and
// F32Lit, one line each for each tier it carries:
//
//	F32LIT <tier> const     <bits>   F32Const
//	F32LIT <tier> default-d <bits>   a fresh F32Lit's d (default 0.1)
//	F32LIT <tier> default-e <bits>   a fresh F32Lit's e (default -0.3)
//	F32LIT <tier> write-min <answer> e set to float32(-0.3), written, read back
//	F32LIT <tier> write-max <answer> e set to float32(0.3), written, read back
//
// where <bits> is 16 hex digits, the float64 bits of the value, or 8, the
// float32 pattern itself (moo), and a write's <answer> is the bits read back,
// or "refused" when the write refused the value. Both ends of e lie OUTSIDE
// their decimals (float32(0.3) is above the double 0.3, float32(-0.3) below
// -0.3): each is in range, so the write must take it, and a write guard held
// at the decimal refuses it. The harness computes every answer itself and
// holds each tier to it bit for bit; a probe left out, answered twice, or
// answered for a tier the leg does not carry fails by name.
package main

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"

	"github.com/mas-bandwidth/schema/v2/ir"
)

// The F32Const and F32Lit declarations of BitPhase.schema
// (TestF32LitDeclarationAgrees holds these to the schema).
const (
	f32LitConst = 0.1
	f32LitDefD  = 0.1
	f32LitDefE  = -0.3
	f32LitMin   = -0.3
	f32LitMax   = 0.3
	f32LitRes   = 0.001
)

// The tokens a driver source carries for the float32 ends it writes; a
// driver whose language has a float32 type spells the decimal with its own
// float32 suffix, and the decimal is the float64 of the float32, so either
// reading gives exactly the float32. moo writes the pattern.
const (
	f32LitMinToken     = "@F32LIT_MIN@"
	f32LitMaxToken     = "@F32LIT_MAX@"
	f32LitMinBitsToken = "@F32LIT_MIN_BITS@"
	f32LitMaxBitsToken = "@F32LIT_MAX_BITS@"
)

// f32LitProbes are the probes every tier answers, in the order drivers print
// them.
var f32LitProbes = []string{"const", "default-d", "default-e", "write-min", "write-max"}

// f32LitDecimal spells the float64 value of float32(v): the decimal a double
// parser reads as exactly that float32.
func f32LitDecimal(v float64) string {
	return strconv.FormatFloat(float64(float32(v)), 'g', -1, 64)
}

func withF32LitTokens(src string) string {
	return strings.NewReplacer(
		f32LitMinToken, f32LitDecimal(f32LitMin),
		f32LitMaxToken, f32LitDecimal(f32LitMax),
		f32LitMinBitsToken, fmt.Sprintf("0x%08x", math.Float32bits(float32(f32LitMin))),
		f32LitMaxBitsToken, fmt.Sprintf("0x%08x", math.Float32bits(float32(f32LitMax))),
	).Replace(src)
}

// f32LitRoundTrip is SPEC §4.6's write of x into e, then its read, each step
// one float32 operation: the normalized offset clamped to [0, 1], the index
// floor(n * count + 0.5) clamped to the count, then the normative decode.
func f32LitRoundTrip(x float32) float32 {
	steps, _ := ir.CompressedFloatParams(f32LitMin, f32LitMax, f32LitRes)
	minF := float32(f32LitMin)
	delta := float32(float32(f32LitMax) - minF)
	count := float32(steps)
	n := float32(float32(x-minF) / delta)
	if n < 0 {
		n = 0
	}
	if n > 1 {
		n = 1
	}
	idx := uint64(math.Floor(float64(float32(float32(n*count) + 0.5))))
	if idx > steps {
		idx = steps
	}
	q := float32(float32(idx) / count)
	p := float32(q * delta)
	return float32(p + minF)
}

// f32LitWant is each probe's answer: a float32, widened.
func f32LitWant() map[string]float32 {
	return map[string]float32{
		"const":     float32(f32LitConst),
		"default-d": float32(f32LitDefD),
		"default-e": float32(f32LitDefE),
		"write-min": f32LitRoundTrip(float32(f32LitMin)),
		"write-max": f32LitRoundTrip(float32(f32LitMax)),
	}
}

// checkF32Lit holds a driver's F32LIT lines to the case: every tier the leg
// carries answers every probe once, with exactly the float32 the declaration
// names.
func checkF32Lit(lang, out string) error {
	tiers := cfDecodeTiers(lang)
	answers := map[string]map[string]string{}
	for _, t := range tiers {
		answers[t] = map[string]string{}
	}
	isProbe := map[string]bool{}
	for _, p := range f32LitProbes {
		isProbe[p] = true
	}
	var bad []string
	for n, line := range strings.Split(out, "\n") {
		line = strings.TrimRight(line, "\r")
		if !strings.HasPrefix(line, "F32LIT") {
			continue
		}
		f := strings.Fields(line)
		if len(f) != 4 || f[0] != "F32LIT" || !isProbe[f[2]] {
			bad = append(bad, fmt.Sprintf("line %d is not F32LIT <tier> <%s> <answer>: %q", n+1, strings.Join(f32LitProbes, "|"), line))
			continue
		}
		got, ok := answers[f[1]]
		if !ok {
			bad = append(bad, fmt.Sprintf("line %d answers the tier %q, and %s carries %s", n+1, f[1], lang, strings.Join(tiers, ", ")))
			continue
		}
		if _, dup := got[f[2]]; dup {
			bad = append(bad, fmt.Sprintf("line %d answers %s %s a second time", n+1, f[1], f[2]))
			continue
		}
		got[f[2]] = f[3]
	}
	want := f32LitWant()
	for _, t := range tiers {
		var missing []string
		for _, p := range f32LitProbes {
			a, ok := answers[t][p]
			if !ok {
				missing = append(missing, p)
				continue
			}
			w := want[p]
			if a == "refused" && strings.HasPrefix(p, "write-") {
				end := float32(f32LitMin)
				if p == "write-max" {
					end = float32(f32LitMax)
				}
				bad = append(bad, fmt.Sprintf("tier %s %s: the write refused float32 %08x (%v), the declared end itself", t, p, math.Float32bits(end), end))
				continue
			}
			if len(a) != 16 && len(a) != 8 {
				bad = append(bad, fmt.Sprintf("tier %s %s answers %q, not 16 or 8 hex digits", t, p, a))
				continue
			}
			bits, err := strconv.ParseUint(a, 16, 64)
			if err != nil {
				bad = append(bad, fmt.Sprintf("tier %s %s answers %q, not hex digits", t, p, a))
				continue
			}
			if len(a) == 8 {
				// the float32 pattern: widen it here, exactly, as the other legs do
				bits = math.Float64bits(float64(math.Float32frombits(uint32(bits))))
			}
			if wantBits := math.Float64bits(float64(w)); bits != wantBits {
				v := math.Float64frombits(bits)
				if float64(float32(v)) != v {
					bad = append(bad, fmt.Sprintf("tier %s %s reads %016x (%v), which is no float32; the declaration names float32 %08x (%v)", t, p, bits, v, math.Float32bits(w), w))
				} else {
					bad = append(bad, fmt.Sprintf("tier %s %s reads float32 %08x (%v); the declaration names %08x (%v)", t, p, math.Float32bits(float32(v)), float32(v), math.Float32bits(w), w))
				}
			}
		}
		if len(missing) > 0 {
			sort.Strings(missing)
			bad = append(bad, fmt.Sprintf("tier %s leaves %s unanswered", t, strings.Join(missing, ", ")))
		}
	}
	if len(bad) > 0 {
		return fmt.Errorf("FAILED: f32lit %s:\n  %s", lang, strings.Join(bad, "\n  "))
	}
	return nil
}
