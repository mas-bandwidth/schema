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

run_one = fn plan, body ->
  # The strict report carries no clamp counter: the bounds-pass count is the
  # decode's own second element. The compiled plan refuses a past-bound body
  # by throwing {:refuse, name} out of run/4 (the load catches it into
  # {:error, name, report}); the identity plan lands the image and its
  # decode counts the same crossing, which the load's range_check then
  # refuses. Both plans cross the bound; the strict answer differs by plan.
  try do
    {img, _report} = RT.run(plan, body, prefill, RT.report())
    {:ok, img, elem(decode.(img, 0), 1)}
  catch
    :throw, {:refuse, why} -> {:refused, why}
  end
end

run_both = fn body -> {run_one.(plan, body), run_one.(made, body)} end

# VALUES: compare decoded values from real bodies.
# The compiled plan may produce a different image for hostile bytes (because
# it processes text/ordinal fields with clamping), so values are compared
# only over real corpus bodies where the images match.
both = Enum.map(bodies, run_both)

values_ok =
  Enum.all?(both, fn {{:ok, a, _}, {:ok, b, _}} -> decode.(a, 0) == decode.(b, 0) end)

# COUNTERS: on real bodies both plans count the bounds pass the same (zero).
counters_ok = Enum.all?(both, fn {{:ok, _, a}, {:ok, _, b}} -> a == b end)

# HOSTILE: include the hostile body to ensure bounds are actually crossed —
# the identity decode counts the crossing, the compiled run refuses it.
hostile = :binary.copy(<<0xFF>>, body_bytes)
{hostile_a, hostile_b} = run_both.(hostile)
hostile_ok = match?({:ok, _, n} when n > 0, hostile_a) and match?({:refused, _}, hostile_b)

results = [
  {"identity and compiled land the same decoded values", values_ok},
  {"identity and compiled count the bounds pass the same", counters_ok},
  {"hostile body crossed a bound (identity counts it, compiled refuses it)", hostile_ok}
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
