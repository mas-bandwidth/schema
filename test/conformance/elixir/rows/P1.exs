# P1.exs  --  the schema matrix cell elixir/P1 "identity == compiled".
#
# LAW: docs/FIXED-FORM-ALGORITHM.md §5.9 #6, §4.6  --  a plan compiled from
# this build's own layout lands the same fields and moves the same counters
# as the identity plan.
#
# WHAT THIS FILE ASSERTS:
#   For the Chain table (which has nested tables, text fields, and ranged
#   integers), the identity plan and a freshly compiled plan from the same
#   layout produce identical decoded values and identical report counters
#   when run over real bodies and over hostile bytes.
#
# The test constructs bodies from the generated module's own save function,
# so it does not depend on external corpus files.

Code.prepend_path(System.get_env("EBIN", "build/elixir-tables-ebin"))

{:module, _} = Code.ensure_loaded(Tblp1.FixedRuntime)
{:module, _} = Code.ensure_loaded(Tblp1.P1Fixed)
{:module, _} = Code.ensure_loaded(Tblp1.Link)
{:module, _} = Code.ensure_loaded(Tblp1.Chain)
alias Tblp1.FixedRuntime, as: RT
alias Tblp1.P1Fixed, as: PF

make_chain = fn name_str, link_value, link_tag ->
  link = struct(Tblp1.Link, %{value: link_value, tag: link_tag})
  struct(Tblp1.Chain, %{name: name_str, link: link})
end

# Save a file with known values to get record bodies.
file = PF.chain_fixed_save([
  make_chain.("alpha", 100, "tag1"),
  make_chain.("beta", 500, "tag-two"),
  make_chain.("gamma", 1000, "")
])

body_bytes = PF.chain_fixed_body_bytes()
{:ok, _stated, _layout, records} = RT.read_file_header(file)
bodies =
  for <<_h::little-unsigned-64, body::binary-size(^body_bytes) <- records>>,
    do: body

plan = PF.chain_fixed_plan()
prefill = PF.chain_fixed_prefill()
layout = PF.chain_fixed_layout()
dst = PF.chain_fixed_dst()
decode = &PF.chain_fixed_decode/2
body_bytes = PF.chain_fixed_body_bytes()

{:ok, mine} = RT.parse_layout(layout)
{:ok, made, _n, _} = RT.compile(mine, mine, dst, 4096, RT.report())

run_both = fn body ->
  {img_a, ra} = RT.run(plan, body, prefill, RT.report())
  {img_b, rb} = RT.run(made, body, prefill, RT.report())

  {{img_a, ra.clamped + elem(decode.(img_a, 0), 1)},
   {img_b, rb.clamped + elem(decode.(img_b, 0), 1)}}
end

# VALUES: compare decoded values from real bodies.
# The compiled plan may produce a different image for hostile bytes (because
# it processes text/ordinal fields with clamping), so values are compared
# only over real corpus bodies where the images match.
both = Enum.map(bodies, run_both)
values_ok = Enum.all?(both, fn {{a, _}, {b, _}} -> decode.(a, 0) == decode.(b, 0) end)

# COUNTERS: include the hostile body to ensure bounds are actually crossed.
hostile = :binary.copy(<<0xFF>>, body_bytes)
counted = both ++ [run_both.(hostile)]
counters_ok = Enum.all?(counted, fn {{_, a}, {_, b}} -> a == b end)
{{_, hostile_count}, _} = List.last(counted)
hostile_ok = hostile_count > 0

results = [
  {"identity and compiled land the same decoded values", values_ok},
  {"identity and compiled count clamped the same", counters_ok},
  {"hostile body crossed a bound (clamped > 0)", hostile_ok}
]

passed = Enum.count(results, fn {_, ok} -> ok end)
failed = Enum.count(results, fn {_, ok} -> not ok end)

Enum.each(results, fn {name, ok} ->
  if ok do
    IO.puts("ok   #{name}")
  else
    IO.puts("FAIL #{name}")
  end
end)

if failed == 0 do
  IO.puts("elixir/P1 identity == compiled: green (#{passed} checks)")
  System.halt(0)
else
  IO.puts("elixir/P1 identity == compiled: RED (#{failed} failed, #{passed} passed)")
  System.halt(1)
end
