package elixirtable

// THE WIDE-TEXT ROW ON THE ELIXIR LEG (docs/roadmap.sexp node `fixed-tables`,
// utf16-values/elixir/valid-data). Law: docs/SPEC-TABLES.md §3.4's fixed record
// — a `wstring(N)` is a used length in CODE UNITS and then 2N bytes, each unit
// written little-endian — and docs/SPEC.md §4.12, whose content rule the reader
// enforces over the used run.
//
// The capability clause is a RUN: the harness generates the packet and fixed
// modules from an inline unit, spawns the leg's own Elixir (`elixirBinary`) and
// writes and reads the construct through the fixed form's own writer and reader
// (t09Run / t09Probe, fixedroadmap_t09_test.go). This test rides the Elixir
// toolchain, so it is gated behind slowtest.Gate and skipped by name in a
// default run.

import (
	"testing"

	"github.com/mas-bandwidth/schema/v2/internal/slowtest"
)

// TestFixedRoadmapElixirT19WideText proves utf16-values/elixir/valid-data: a
// `wstring(10)` table field carries the reference values — empty, a BMP unit, a
// BMP pair, the astral pair U+1F600 and the full declared capacity — unit for
// unit.
func TestFixedRoadmapElixirT19WideText(t *testing.T) {
	t.Parallel()
	slowtest.Gate(t, "the Elixir toolchain (it compiles and runs the generated module)")
	t.Run("utf16-values/elixir/valid-data", func(t *testing.T) {
		t.Parallel()
		u := unitFrom(t, `package probe

fixed table T
{
    w wstring(10)
}
`)
		t09Probe(t, u, "T", `  empty = <<>>
  {:ok, [re], rep0} = @@LOAD@@(@@SAVE@@([%Probe.T{w: empty}]))
  check(re.w == empty, "the empty string does not round-trip: #{inspect(re.w)}")
  check(rep0.malformed == false, "the empty string is malformed: #{inspect(rep0)}")
  bmp = <<0x41, 0x00>>
  {:ok, [rb], _} = @@LOAD@@(@@SAVE@@([%Probe.T{w: bmp}]))
  check(rb.w == bmp, "a BMP unit does not round-trip: #{inspect(rb.w)}")
  pair = <<0xE9, 0x00, 0x2D, 0x4E>>
  {:ok, [rp], _} = @@LOAD@@(@@SAVE@@([%Probe.T{w: pair}]))
  check(rp.w == pair, "a BMP pair does not round-trip: #{inspect(rp.w)}")
  astral = <<0x3D, 0xD8, 0x00, 0xDE>>
  {:ok, [ra], repa} = @@LOAD@@(@@SAVE@@([%Probe.T{w: astral}]))
  check(ra.w == astral, "the astral pair U+1F600 does not round-trip: #{inspect(ra.w)}")
  check(repa.malformed == false, "a well-formed astral pair is malformed: #{inspect(repa)}")
  full = :binary.copy(<<0x01, 0x00>>, 10)
  {:ok, [rf], repf} = @@LOAD@@(@@SAVE@@([%Probe.T{w: full}]))
  check(rf.w == full, "a string at the declared capacity does not round-trip: #{inspect(rf.w)}")
  check(repf.malformed == false, "a full-capacity string is malformed: #{inspect(repf)}")`)
	})
}
