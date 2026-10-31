package mootable

import (
	"encoding/binary"
	"fmt"
	"math"
	"math/big"
	"math/rand/v2"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
	"github.com/mas-bandwidth/schema/v2/internal/tabletext"
)

// The float half of the text form, held to the compiler's own engine
// (internal/tabletext) value for value: moo has no float, so json.moo spells a
// binary32 / binary64 pattern and reads a decimal token in exact integer
// arithmetic, and these tests are what say that arithmetic is the engine's
// (docs/SPEC-TABLES.md §16.1 THE FLOAT SPELLING; docs/PORTING.md I13).

const floatProbeSchema = "package probe\ntable Root\n{\n    f float32\n    d float64\n}\n"

// floatSeed is the differential's seed: fixed, so a find reproduces.
const floatSeed = 20261028

func float32Corpus(n int) []uint32 {
	out := []uint32{
		0, 0x80000000, 1, 0x80000001, 0x007fffff, 0x00800000, 0x7f7fffff, 0xff7fffff,
		math.Float32bits(1), math.Float32bits(0.1), math.Float32bits(1.0 / 3), math.Float32bits(-266744.625),
		math.Float32bits(16777216), math.Float32bits(1e10), math.Float32bits(1e-10), math.Float32bits(123456789),
		math.Float32bits(0.0001), math.Float32bits(0.00001), math.Float32bits(999999.94), math.Float32bits(9999999),
	}
	for k := -149; k <= 127; k++ {
		out = append(out, math.Float32bits(float32(math.Ldexp(1, k))))
	}
	for k := -45; k <= 38; k++ {
		out = append(out, math.Float32bits(float32(math.Pow(10, float64(k)))))
	}
	r := rand.New(rand.NewPCG(floatSeed, 32))
	for len(out) < n {
		b := r.Uint32()
		if b&0x7f800000 == 0x7f800000 {
			continue
		}
		out = append(out, b)
		// a short decimal's float32, the values a person writes
		out = append(out, math.Float32bits(float32(float64(r.IntN(2000000)-1000000)/math.Pow(10, float64(r.IntN(12))))))
	}
	return out
}

func float64Corpus(n int) []uint64 {
	out := []uint64{
		0, 1 << 63, 1, 1<<63 | 1, 0x000fffffffffffff, 0x0010000000000000, 0x7fefffffffffffff,
		math.Float64bits(1), math.Float64bits(0.1), math.Float64bits(1.0 / 3), math.Float64bits(5e-324),
		math.Float64bits(1e21), math.Float64bits(1e22), math.Float64bits(9007199254740993), math.Float64bits(0.3),
		math.Float64bits(2.5), math.Float64bits(100), math.Float64bits(123456789012345678),
	}
	for k := -1074; k <= 1023; k += 7 {
		out = append(out, math.Float64bits(math.Ldexp(1, k)))
	}
	for k := -323; k <= 308; k += 3 {
		out = append(out, math.Float64bits(math.Pow(10, float64(k))))
	}
	r := rand.New(rand.NewPCG(floatSeed, 64))
	for len(out) < n {
		b := r.Uint64()
		if b&0x7ff0000000000000 == 0x7ff0000000000000 {
			continue
		}
		out = append(out, b)
		out = append(out, math.Float64bits(float64(r.IntN(2000000000)-1000000000)/math.Pow(10, float64(r.IntN(20)))))
	}
	return out
}

// TestMooJsonFloatWriteMatchesEngine: every pattern of the corpus, written by
// json.moo and by the engine, is the same text byte for byte.
func TestMooJsonFloatWriteMatchesEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs json.moo's float writer)")
	t.Parallel()
	got32, got64, want32, want64 := floatWriteRun(t, nil)
	compareFloatTexts(t, "float32", got32, want32)
	compareFloatTexts(t, "float64", got64, want64)
}

// TestMooJsonFloatWriteNegativeControl breaks the tie rule (half away from
// zero instead of half to even, the mistake I13 names) and requires the same
// differential to go red on a tie, and only on the writer's text.
func TestMooJsonFloatWriteNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	src := string(Runtime()["json.moo"])
	const tie = "if nextDigit > 5 or (nextDigit == 5 and (rest or lastOdd)) then"
	if !strings.Contains(src, tie) {
		t.Fatal("the control's site moved: json.moo no longer rounds at " + tie)
	}
	broken := strings.Replace(src, tie, "if nextDigit >= 5 then", 1)
	got32, got64, want32, want64 := floatWriteRun(t, map[string][]byte{"json.moo": []byte(broken)})
	diff := 0
	for i := range want32 {
		if got32[i] != want32[i] {
			diff++
		}
	}
	for i := range want64 {
		if got64[i] != want64[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("the sabotaged tie rule wrote every text the engine writes: the differential cannot see a tie")
	}
	t.Logf("the sabotaged tie rule differs on %d of %d texts", diff, len(want32)+len(want64))
}

func floatWriteRun(t *testing.T, runtime map[string][]byte) (got32, got64, want32, want64 []string) {
	t.Helper()
	m := oracleModel(t, floatProbeSchema)
	st := oracleRoot(t, m)
	c32, c64 := float32Corpus(3000), float64Corpus(3000)
	var in32, in64 []byte
	for _, b := range c32 {
		in32 = binary.LittleEndian.AppendUint32(in32, b)
		inst := m.New(st)
		inst.Fields[0].Cell.F = float64(math.Float32frombits(b))
		text, ok := oracleValueText(t, m, onlyField(inst, 0))
		if !ok {
			t.Fatalf("the engine refused float32 %08x", b)
		}
		want32 = append(want32, text)
	}
	for _, b := range c64 {
		in64 = binary.LittleEndian.AppendUint64(in64, b)
		inst := m.New(st)
		inst.Fields[1].Cell.F = math.Float64frombits(b)
		text, ok := oracleValueText(t, m, onlyField(inst, 1))
		if !ok {
			t.Fatalf("the engine refused float64 %016x", b)
		}
		want64 = append(want64, text)
	}
	main := probePrelude + `
local f32 = inputs.f32
for i = 0, buffer.len(f32) - 4, 4 do
	hexs(J.formatFloat32(buffer.readu32(f32, i)) or "nil")
end
local f64 = inputs.f64
for i = 0, buffer.len(f64) - 8, 8 do
	hexs(J.formatFloat64(buffer.readu32(f64, i + 4), buffer.readu32(f64, i)) or "nil")
end
`
	lines := jsonProbe(t, main, map[string][]byte{"f32": in32, "f64": in64}, runtime)
	if len(lines) != len(c32)+len(c64) {
		t.Fatalf("the probe printed %d lines, want %d", len(lines), len(c32)+len(c64))
	}
	return lines[:len(c32)], lines[len(c32):], want32, want64
}

// onlyField keeps the instance's one field i, so the engine's text is that
// field's alone and oracleValueText finds it first.
func onlyField(inst *tabletext.Instance, i int) *tabletext.Instance {
	inst.Fields = inst.Fields[i : i+1]
	return inst
}

func compareFloatTexts(t *testing.T, kind string, got, want []string) {
	t.Helper()
	bad := 0
	for i := range want {
		if got[i] != want[i] {
			bad++
			if bad <= 10 {
				t.Errorf("%s #%d: moo wrote %q, the engine %q", kind, i, got[i], want[i])
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%s: %d of %d texts differ", kind, bad, len(want))
	}
}

// exactDecimal is the dyadic value x * 2^k written out exactly in decimal.
func exactDecimal(f *big.Float) string {
	s := f.Text('f', 1200)
	if strings.Contains(s, ".") {
		s = strings.TrimRight(strings.TrimRight(s, "0"), ".")
	}
	return s
}

// floatTokens is the read corpus: decimals a person writes, exact midpoints
// between neighbouring floats and their decimal neighbours (where a reader
// that rounds twice parts company with one that rounds once), the band past
// FLT_MAX, the subnormal end, and tokens far longer than any float.
func floatTokens(n int) []string {
	out := []string{
		"0", "-0", "0e0", "1E+2", "1e-0", "0.1", "1.5", "-266744.625", "3.4028235e38", "3.4028236e38",
		"340282356779733661637539395458142568448", "340282356779733661637539395458142568447",
		"1e-45", "7e-46", "1e-46", "1e39", "1e308", "1.8e308", "5e-324", "2e-324", "3e-324",
		"1." + strings.Repeat("0", 600), "1" + strings.Repeat("0", 40), "0." + strings.Repeat("0", 400) + "1e400",
		"123456789012345678901234567890e-30", "9007199254740993", "1e999999999", "-1e999999999", "1e-999999999",
		"2.4703282292062327e-324", "2.4703282292062328e-324", "1.401298464324817e-45", "7.006492321624085e-46",
	}
	r := rand.New(rand.NewPCG(floatSeed, 7))
	for len(out) < n {
		switch r.IntN(4) {
		case 0:
			digits := fmt.Sprint(r.Int64N(1 << 62))[:1+r.IntN(18)]
			out = append(out, fmt.Sprintf("%s%se%d", sign(r), digits, r.IntN(90)-45))
		case 1:
			x := math.Float32frombits(r.Uint32() & 0x7f7fffff)
			next := math.Nextafter32(x, float32(math.Inf(1)))
			mid := new(big.Float).SetPrec(300).Add(big.NewFloat(float64(x)), big.NewFloat(float64(next)))
			mid.Quo(mid, big.NewFloat(2))
			out = append(out, sign(r)+exactDecimal(mid))
		case 2:
			x := math.Float64frombits(r.Uint64() & 0x7fefffffffffffff)
			next := math.Nextafter(x, math.Inf(1))
			mid := new(big.Float).SetPrec(300).Add(new(big.Float).SetFloat64(x), new(big.Float).SetFloat64(next))
			mid.Quo(mid, big.NewFloat(2))
			s := exactDecimal(mid)
			// the midpoint, or one unit off in a far digit
			if r.IntN(2) == 0 && len(s) > 30 {
				b := []byte(s[:30])
				for i := len(b) - 1; i >= 0; i-- {
					if b[i] >= '0' && b[i] < '9' {
						b[i]++
						break
					}
				}
				s = string(b)
			}
			out = append(out, sign(r)+s)
		default:
			out = append(out, fmt.Sprintf("%s%d.%de%d", sign(r), r.IntN(100000), r.IntN(100000), r.IntN(700)-350))
		}
	}
	return out
}

func sign(r *rand.Rand) string {
	if r.IntN(3) == 0 {
		return "-"
	}
	return ""
}

// TestMooJsonFloatReadMatchesEngine: every token of the read corpus lands
// the same pattern in json.moo as in the engine at both widths, and a token
// the engine refuses as an infinity (kind_mismatch) is an infinity in moo.
func TestMooJsonFloatReadMatchesEngine(t *testing.T) {
	slowtest.Gate(t, "moo (it runs json.moo's float reader)")
	t.Parallel()
	m := oracleModel(t, floatProbeSchema)
	st := oracleRoot(t, m)
	tokens := floatTokens(1500)
	main := probePrelude + `
for _, token in string.split(buffer.tostring(inputs.tokens), "\n") do
	local hi, lo = J.parseFloat64(token)
	hexs(string.format("%08x %08x%08x", J.parseFloat32(token), hi, lo))
end
`
	lines := jsonProbe(t, main, map[string][]byte{"tokens": []byte(strings.Join(tokens, "\n"))}, nil)
	if len(lines) != len(tokens) {
		t.Fatalf("the probe printed %d lines, want %d", len(lines), len(tokens))
	}
	bad := 0
	for i, token := range tokens {
		inst := m.New(st)
		var r tabletext.Report
		if !m.Read(inst, []byte(`{"f": `+token+`, "d": `+token+`}`), &r) {
			t.Fatalf("the engine refused %q as malformed", token)
		}
		var f32, f64 string
		if r.KindMismatch > 0 {
			// which width overflowed: the engine counted one, or both
			f32 = "inf"
			f64 = fmt.Sprintf("%016x", math.Float64bits(inst.Fields[1].Cell.F))
			if r.KindMismatch == 2 {
				f64 = "inf"
			}
		} else {
			f32 = fmt.Sprintf("%08x", math.Float32bits(float32(inst.Fields[0].Cell.F)))
			f64 = fmt.Sprintf("%016x", math.Float64bits(inst.Fields[1].Cell.F))
		}
		parts := strings.Fields(lines[i])
		g32, g64 := parts[0], parts[1]
		if f32 == "inf" && (g32 == "7f800000" || g32 == "ff800000") {
			g32 = "inf"
		}
		if f64 == "inf" && (g64 == "7ff0000000000000" || g64 == "fff0000000000000") {
			g64 = "inf"
		}
		if g32 != f32 || g64 != f64 {
			bad++
			if bad <= 10 {
				t.Errorf("token %q: moo %s %s, the engine %s %s", token, g32, g64, f32, f64)
			}
		}
	}
	if bad > 0 {
		t.Fatalf("%d of %d tokens differ", bad, len(tokens))
	}
}

// TestMooJsonFloatReadNegativeControl rounds a tie away from even in the one
// rounding every read takes, and requires the read differential to go red:
// the exact midpoints of the corpus are what see it.
func TestMooJsonFloatReadNegativeControl(t *testing.T) {
	slowtest.Gate(t, "moo (it runs a sabotaged json.moo)")
	t.Parallel()
	src := string(Runtime()["json.moo"])
	const tie = "up = c > 0 or (c == 0 and ql % 2 == 1)"
	if !strings.Contains(src, tie) {
		t.Fatal("the control's site moved: json.moo no longer rounds at " + tie)
	}
	broken := strings.Replace(src, tie, "up = c >= 0", 1)
	tokens := floatTokens(1500)
	main := probePrelude + `
for _, token in string.split(buffer.tostring(inputs.tokens), "\n") do
	local hi, lo = J.parseFloat64(token)
	hexs(string.format("%08x %08x%08x", J.parseFloat32(token), hi, lo))
end
`
	in := map[string][]byte{"tokens": []byte(strings.Join(tokens, "\n"))}
	good := jsonProbe(t, main, in, nil)
	bad := jsonProbe(t, main, in, map[string][]byte{"json.moo": []byte(broken)})
	diff := 0
	for i := range good {
		if good[i] != bad[i] {
			diff++
		}
	}
	if diff == 0 {
		t.Fatal("the sabotaged tie rule read every token as the true reader does: the corpus holds no midpoint")
	}
	t.Logf("the sabotaged tie rule differs on %d of %d tokens", diff, len(tokens))
}
