# W9.exs  --  the schema matrix cell elixir/W9 "prefill unwritten ranges only".
#
# LAW: docs/FIXED-FORM-ALGORITHM.md §4.3  --  "Prefill the bytes the plan does not
# write. The plan compiler computes the UNWRITTEN RANGES  --  the destination bytes
# no entry covers  --  and only those take the declared defaults before the loop;
# for the identity plan that list is EMPTY, so the identity read pays no prefill
# (fix 15)."  The row's governing sentence (docs/FIXED-FORM-ALGORITHM.md:308):
# "**Prefill the bytes the plan does not write.**"
#
# This leg's production read is `Tblp1.P1Fixed.chain_fixed_load/1`, whose
# per-record engine is `Tblp1.FixedRuntime.run/4`  --  the load calls
# `R.run(plan, R.detach(body, copy), @chain_prefill, report)` at
# build/tables-generated-elixir/tblp1/P1Fixed.ex:315. `run` walks the plan's
# entries into WRITES and hands them to `assemble/2`, which splices the prefill
# into every GAP (unwritten range)  --  and whose first clause, for a single
# whole-body write, returns the body sub-binary without touching the prefill at
# all. That clause is the identity plan's empty unwritten list, asserted here.
#
# The test reaches the generated corpus the way the leg's driver does
# (test/conformance/elixir/driver): the precompiled .beam files under
# $EBIN / build/elixir-tables-ebin, placed on the code path. Exit 0 is green,
# exit 1 is red, one line per assertion.

Code.prepend_path(System.get_env("EBIN", "build/elixir-tables-ebin"))

{:module, _} = Code.ensure_loaded(Tblp1.P1Fixed)
{:module, _} = Code.ensure_loaded(Tblp1.FixedRuntime)

alias Tblp1.FixedRuntime, as: RT
alias Tblp1.P1Fixed, as: PF

# A form-3 FILE of one Chain record, header and layout from the build's own
# constants (docs/SPEC-TABLES.md §3): the twenty-byte header, the layout behind
# its u32 length, then the records  --  each the layout hash followed by its body.
build_file = fn ->
  hash = PF.chain_fixed_hash()
  layout = PF.chain_fixed_layout()
  body = :binary.copy(<<0x5A>>, PF.chain_fixed_body_bytes())
  RT.file_header(hash, byte_size(layout)) <> layout <> <<hash::little-unsigned-64>> <> body
end

checks = [
  {
    "W9 identity plan is one whole-body run  --  the unwritten list is EMPTY",
    fn ->
      plan = PF.chain_fixed_plan()
      bytes = PF.chain_fixed_body_bytes()
      plan == [{:copy, 0, 0, bytes}]
    end
  },
  {
    "W9 the identity read pays no prefill  --  a poisoned prefill never enters the image",
    fn ->
      body = :binary.copy(<<0x5A>>, PF.chain_fixed_body_bytes())
      poison = :binary.copy(<<0xA5>>, PF.chain_fixed_body_bytes())
      {image, report} = RT.run(PF.chain_fixed_plan(), body, poison, RT.report())
      image == body and report.clamped == 0 and report.malformed == false
    end
  },
  {
    "W9 production chain_fixed_load lands the raw body -- the prefill owned no byte",
    fn ->
      {:ok, [chain], report} = PF.chain_fixed_load(build_file.())
      # The raw sentinel body reached the projection untouched: the name and
      # tag hold the body's own bytes (0x5A is 'Z'), and the sentinel int32s
      # are all past their declared maxima, so all three clamp. A prefill the
      # identity read had paid would have landed the declared defaults (zeros)
      # and clamped nothing.
      chain.name == String.duplicate("Z", 16) and chain.link.tag == String.duplicate("Z", 8) and
        chain.link.value == 1000 and report.clamped == 3
    end
  },
  {
    "W9 a non-identity plan prefills ONLY the unwritten range",
    fn ->
      body = :binary.copy(<<0x5A>>, PF.chain_fixed_body_bytes())
      prefill = PF.chain_fixed_prefill()
      gap = PF.chain_fixed_body_bytes() - 4
      {image, _report} = RT.run([{:copy, 0, 0, 4}], body, prefill, RT.report())

      binary_part(image, 0, 4) == binary_part(body, 0, 4) and
        binary_part(image, 4, gap) == binary_part(prefill, 4, gap)
    end
  },
  {
    "W9 the unwritten range takes the declared defaults, and the write wins its own",
    fn ->
      body = :binary.copy(<<0x5A>>, PF.chain_fixed_body_bytes())
      poisoned = :binary.copy(<<0xCD>>, PF.chain_fixed_body_bytes())
      gap = PF.chain_fixed_body_bytes() - 4
      {image, _report} = RT.run([{:copy, 0, 0, 4}], body, poisoned, RT.report())

      binary_part(image, 0, 4) == binary_part(body, 0, 4) and
        binary_part(image, 4, gap) == binary_part(poisoned, 4, gap)
    end
  }
]

results =
  Enum.map(checks, fn {name, probe} ->
    try do
      {name, probe.()}
    rescue
      e -> {name, {:raised, e}}
    end
  end)

Enum.each(results, fn
  {name, true} -> IO.puts("ok   #{name}")
  {name, false} -> IO.puts("FAIL #{name}")
  {name, {:raised, e}} -> IO.puts("FAIL #{name} (raised #{inspect(e)})")
end)

if Enum.all?(results, fn {_name, r} -> r == true end) do
  IO.puts("elixir/W9 prefill unwritten ranges only: green")
  System.halt(0)
else
  IO.puts("elixir/W9 prefill unwritten ranges only: RED")
  System.halt(1)
end
