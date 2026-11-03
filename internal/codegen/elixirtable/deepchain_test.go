package elixirtable

import (
	"fmt"
	"slices"
	"strings"
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// A CHAIN OF NESTED TYPES COSTS GENERATED CODE LINEAR IN ITS DEPTH. Each level
// of deepChainSchema holds a bool, the next level and a byte, so the packet
// wire is unaligned at every level and the fixed form is a run of leaves all
// the way down. The root is a fixed table, at depth 0, and at depth n the last
// type, L<n-2>, holds v at depth n, so the chain at 64 nests exactly to the
// fixed form's bound (docs/SPEC-TABLES.md §3.4's fixed_max_depth). A holder
// inlines a nested type only a bounded number of levels deep and calls the
// type's helper past that, so each type costs about the same code at any
// depth.

func deepChainSchema(depth int) string {
	var b strings.Builder
	b.WriteString("package deepchain\n\n")
	fmt.Fprintf(&b, "type L%d\n{\n    v int32 = 0 | min = 0, max = 200\n}\n", depth-2)
	for k := depth - 3; k >= 0; k-- {
		fmt.Fprintf(&b, "\ntype L%d\n{\n    flag bool\n    d    L%d\n    tail uint8\n}\n", k, k+1)
	}
	b.WriteString("\nfixed table Root\n{\n    lead  uint32 = 1\n    d     L0\n    trail uint32 = 2\n}\n")
	return b.String()
}

// TestDeepChainGeneratesLinearly generates the chain at three depths and holds
// every module's growth from 32 to 64 levels to twice its growth from 16 to 32,
// which is what code linear in depth grows by. A module that inlined every
// nested type whole grows with the square of the depth and fails here: the
// packet module grew about five times and the fixed module about three and a
// half times as much over the second step.
func TestDeepChainGeneratesLinearly(t *testing.T) {
	sizes := map[int]map[string]int{}
	for _, depth := range []int{16, 32, 64} {
		files := generateElixir(t, unitFrom(t, deepChainSchema(depth)))
		sizes[depth] = map[string]int{}
		for name, data := range files {
			sizes[depth][name] = len(data)
		}
	}
	var names []string
	for name := range sizes[64] {
		names = append(names, name)
	}
	slices.Sort(names)
	for _, name := range names {
		first := sizes[32][name] - sizes[16][name]
		second := sizes[64][name] - sizes[32][name]
		// 2% over twice the first step, and a line or two of slack for a
		// module whose growth is a few bytes of numbering
		if limit := 2*first + first/50 + 256; second > limit {
			t.Errorf("%s grows %d bytes from 16 to 32 levels and %d from 32 to 64, past %d: generated code is not linear in nesting depth",
				name, first, second, limit)
		}
		t.Logf("%s: %d, %d, %d bytes at 16, 32, 64 levels", name, sizes[16][name], sizes[32][name], sizes[64][name])
	}
}

// deepChainProbe builds the 63 levels of the chain at depth 64, writes it through the packet wire
// and the fixed form, and holds both to the bytes the schema spells: the
// packet wire packs every flag, then v, then every tail, least significant bit
// first; the fixed image is one byte per flag, v's four bytes and one byte per
// tail. Both read back to the value, a short packet read refuses, and a v past
// its range is refused on the packet wire and clamped and counted in the fixed
// form.
const deepChainProbe = `defmodule Check do
  def check(true, _), do: :ok
  def check(false, msg) do
    IO.puts("FAIL " <> msg)
    System.halt(1)
  end
end

levels = 63
flag = fn k -> rem(k, 3) == 0 end
tail = fn k -> rem(k * 37 + 11, 256) end
v = 123

value =
  Enum.reduce((levels - 2)..0//-1, struct!(Module.concat(Deepchain, "L#{levels - 1}"), v: v), fn k, inner ->
    struct!(Module.concat(Deepchain, "L#{k}"), flag: flag.(k), d: inner, tail: tail.(k))
  end)

pack = fn v ->
  fields =
    Enum.map(0..(levels - 2), &{if(flag.(&1), do: 1, else: 0), 1}) ++
      [{v, 8}] ++ Enum.map((levels - 2)..0//-1, &{tail.(&1), 8})

  {acc, bits} = Enum.reduce(fields, {0, 0}, fn {x, w}, {acc, pos} -> {acc ||| x <<< pos, pos + w} end)
  bytes = div(bits + 7, 8)
  {<<acc::little-size(bytes)-unit(8)>>, bits}
end

{want, bits} = pack.(v)
wire = Deepchain.Probe.write_l0!(value)
Check.check(wire == want, "the packet wire is not the schema's bits: #{inspect(wire)} want #{inspect(want)}")
Check.check(Deepchain.Probe.read_l0_bits(wire, bits) == {:ok, value, bits}, "the packet wire does not read back")
Check.check(Deepchain.Probe.read_l0(wire, bits - 1) == :error, "a short packet read does not refuse")
{forged, _} = pack.(201)
Check.check(Deepchain.Probe.read_l0(forged, bits) == :error, "a packet v past its range is not refused")

image = fn v ->
  flags = for k <- 0..(levels - 2), into: <<>>, do: <<if(flag.(k), do: 1, else: 0)>>
  tails = for k <- (levels - 2)..0//-1, into: <<>>, do: <<tail.(k)>>
  flags <> <<v::little-signed-32>> <> tails
end

body = Deepchain.ProbeFixed.l0_fixed_write_body(value)
Check.check(body == image.(v), "the fixed image is not the schema's bytes: #{inspect(body)}")
Check.check(Deepchain.ProbeFixed.l0_fixed_decode(body, 0) == {value, 0, false}, "the fixed image does not read back")
clamped = put_in(value, Enum.map(1..(levels - 1), fn _ -> Access.key!(:d) end) ++ [Access.key!(:v)], 200)
Check.check(Deepchain.ProbeFixed.l0_fixed_decode(image.(201), 0) == {clamped, 1, false}, "a fixed v past its range is not clamped and counted")
IO.puts("ok")
`

func TestDeepChainRoundTrip(t *testing.T) {
	slowtest.Gate(t, "the Elixir toolchain (it compiles and runs the 64-level chain)")
	elixirBin := elixirBinary(t)
	files := generateElixir(t, unitFrom(t, deepChainSchema(64)))
	dir := t.TempDir()
	names := writeModuleDir(t, dir, files)
	out := runProbe(t, elixirBin, dir, names, "import Bitwise\n"+deepChainProbe)
	if !strings.Contains(out, "ok") || strings.Contains(out, "FAIL") {
		t.Fatalf("probe:\n%s", out)
	}
}
