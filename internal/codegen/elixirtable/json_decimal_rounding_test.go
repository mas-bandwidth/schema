package elixirtable

import (
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// Check the decoded value itself, before any wire writer can round it for us.
// Exact decimal midpoints and their neighbours distinguish direct binary32
// rounding from the lossy decimal -> binary64 -> binary32 route.
func TestJSONDecimalBinary32Elixir(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain")
	u := unitFrom(t, `package decimalprobe
table Root {
 value float32
 values []float32
}
`)
	tokens := []string{"0.0", "-0.0", "1e-100000", "-1e-100000", "7.0064923216240853547e-46", "1.1754942807573643e-38", "3.4028234663852886e38"}
	// Large integer tokens have the same double-rounding risk as decimals.
	mid := new(big.Int).Add(new(big.Int).Lsh(big.NewInt(1), 64), new(big.Int).Lsh(big.NewInt(1), 40))
	for _, offset := range []int64{-1, 0, 1} {
		token := new(big.Int).Add(mid, big.NewInt(offset)).String()
		tokens = append(tokens, token, "-"+token)
	}
	delta := new(big.Rat).SetFrac(big.NewInt(1), new(big.Int).Exp(big.NewInt(10), big.NewInt(200), nil))
	for _, bits := range []uint32{0, 1, 2, 0x007ffffe, 0x007fffff, 0x00800000, 0x3effffff, 0x3f800000, 0x3f800001, 0x4b000000, 0x7f7ffffd} {
		a := new(big.Rat).SetFloat64(float64(math.Float32frombits(bits)))
		b := new(big.Rat).SetFloat64(float64(math.Float32frombits(bits + 1)))
		midpoint := new(big.Rat).Quo(new(big.Rat).Add(a, b), big.NewRat(2, 1))
		for _, n := range []*big.Rat{new(big.Rat).Sub(midpoint, delta), midpoint, new(big.Rat).Add(midpoint, delta)} {
			token := n.FloatString(200)
			tokens = append(tokens, token, "-"+token)
		}
	}
	var probe strings.Builder
	probe.WriteString(`
defmodule Probe do
 def check(true, _), do: :ok
 def check(false, what), do: raise(what)
 def bits(value) do
  <<bits::unsigned-64>> = <<value::float-64>>
  bits
 end
end
`)
	for i, token := range tokens {
		f, err := strconv.ParseFloat(token, 32)
		if err != nil {
			t.Fatalf("finite decimal %q: %v", token, err)
		}
		want := math.Float64bits(f)
		text := fmt.Sprintf(`{"value":%s,"values":[%s]}`, token, token)
		fmt.Fprintf(&probe, "{:ok, value, report} = Decimalprobe.Table.from_json(\"Root\", %s)\n", elixirString(text))
		fmt.Fprintf(&probe, "Probe.check(Probe.bits(value[\"value\"]) == %d, \"decimal %d scalar\")\n", want, i)
		fmt.Fprintf(&probe, "Probe.check(Enum.map(value[\"values\"], &Probe.bits/1) == [%d], \"decimal %d array\")\n", want, i)
		fmt.Fprintf(&probe, "Probe.check(true and true, \"decimal %d report\")\n", i)
	}
	probe.WriteString("IO.puts(\"ok\")\n")
	out, err := runArmProbe(t, elixirBinary(t), generateElixir(t, u), probe.String())
	if err != nil || !strings.Contains(out, "ok") {
		t.Fatalf("direct binary32 decimal rounding: %v\n%s", err, out)
	}
}
