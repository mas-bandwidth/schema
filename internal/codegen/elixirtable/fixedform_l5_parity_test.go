package elixirtable

import (
	"strings"
	"testing"
)

// TestFixedFormRefusalNamesElixir holds the generated fixed-form load to the
// refusal table of docs/FIXED-FORM-ALGORITHM.md §5.3, on three rows where the
// Elixir leg answered off-spec until the L5 parity sweep (2026-11-25):
//
//   - framing damage is `truncated` / `reserved_not_zero`, never `malformed`:
//     "a ragged tail: rest mod record_bytes != 0 | truncated", "no bytes at
//     all | truncated", "shorter than 20 bytes | truncated", "any reserved
//     byte 1..7 nonzero | reserved_not_zero". This form's report carries no
//     malformed flag;
//   - a hash in no lineage entry is `layout_unknown`, reporting the file's hash
//     and nothing else; `layout_newer` is retired (§5.3 step 5, SPEC-TABLES
//     §21.3);
//   - "a length past the plan's cap (§4.5) | length_over_bound": a string(N)
//     or bytes(N) length past N REFUSES, on the load and on the direct record
//     decode alike, and is never clamped.
func TestFixedFormRefusalNamesElixir(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package lfive\n\nfixed table T\n{\n    label string(8)\n    blob bytes(6)\n    n int32\n}\n")
	probe := `alias Lfive.ProbeFixed, as: F
check = fn label, got, want ->
  unless match?({:error, ^want, _}, got), do: raise(label <> ": want " <> inspect(want) <> ", got " <> inspect(got))
end
v = %Lfive.T{label: "abc", blob: <<0xAB, 0xCD>>, n: 7}
file = IO.iodata_to_binary(F.t_fixed_save([v]))
{:ok, [^v], _} = F.t_fixed_load(file)

# FRAMING: the ragged tail, the empty file, the short header, a reserved byte.
check.("ragged tail", F.t_fixed_load(file <> <<1, 2, 3>>), :truncated)
check.("no bytes at all", F.t_fixed_load(<<>>), :truncated)
check.("shorter than 20 bytes", F.t_fixed_load(binary_part(file, 0, 10)), :truncated)
<<form, _r1, reserved::binary-size(6), tail::binary>> = file
check.("reserved byte", F.t_fixed_load(<<form, 1, reserved::binary, tail::binary>>), :reserved_not_zero)
for bad <- [file <> <<1, 2, 3>>, <<>>, binary_part(file, 0, 10)] do
  {:error, why, report} = F.t_fixed_load(bad)
  if why == :malformed or Map.has_key?(report, :malformed), do: raise("framing damage answered malformed: " <> inspect({why, report}))
end

# STEP 5: a hash no lineage entry holds.
<<head::binary-size(8), _hash::binary-size(8), rest::binary>> = file
case F.t_fixed_load(head <> <<1, 2, 3, 4, 5, 6, 7, 8>> <> rest) do
  {:error, :layout_unknown, %{layout_hash: 0x0807060504030201}} -> :ok
  other -> raise("unknown hash: want layout_unknown with the file's hash, got " <> inspect(other))
end

# A LENGTH PAST THE CAP: string(8) said 9, bytes(6) said 7, and a negative one.
splice = fn bin, at, len -> binary_part(bin, 0, at) <> <<len::little-signed-32>> <> binary_part(bin, at + 4, byte_size(bin) - at - 4) end
full = IO.iodata_to_binary(F.t_fixed_save([%Lfive.T{label: "abcdefgh", blob: <<1, 2, 3, 4, 5, 6>>, n: 7}]))
{at_label, _} = :binary.match(full, <<8, 0, 0, 0, "abcdefgh">>)
{at_blob, _} = :binary.match(full, <<6, 0, 0, 0, 1, 2, 3, 4, 5, 6>>)
for {label, bad} <- [
      {"string(8) length 9", splice.(full, at_label, 9)},
      {"string(8) length -1", splice.(full, at_label, -1)},
      {"bytes(6) length 7", splice.(full, at_blob, 7)}
    ] do
  check.(label <> " load", F.t_fixed_load(bad), :length_over_bound)
  body = binary_part(bad, byte_size(bad) - F.t_fixed_body_bytes(), F.t_fixed_body_bytes())
  got = try do
    F.t_fixed_decode(body, 0)
  catch
    :throw, {:refuse, why} -> {:refused, why}
  end
  unless got == {:refused, :length_over_bound}, do: raise(label <> " decode: want a refusal, got " <> inspect(got))
end
IO.puts("FIXED_FORM_REFUSAL_NAMES_OK")
`
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "FIXED_FORM_REFUSAL_NAMES_OK") {
		t.Fatal(out)
	}
}

// TestFixedFormCountOverBoundElixir holds the generated fixed-form read to
// docs/FIXED-FORM-ALGORITHM.md §4.5's `count` op, "if v < 0 or v > size then
// REFUSE count_over_bound", and its §5.3 refusal table row "a count past the
// plan's bound (§4.5) | count_over_bound": a counted [A..B]'s count past B,
// below A or negative refuses BY THAT NAME on the load and on the direct record
// decode alike, and is never clamped. Until 2026-11-25 this leg clamped it and
// the load answered out_of_range.
func TestFixedFormCountOverBoundElixir(t *testing.T) {
	t.Parallel()
	u := unitFrom(t, "package lsix\n\nfixed table T\n{\n    vals [1..4]int32\n    n int32\n}\n")
	probe := `alias Lsix.ProbeFixed, as: F
v = %Lsix.T{vals: [11, 22], n: 7}
file = IO.iodata_to_binary(F.t_fixed_save([v]))
{:ok, [^v], _} = F.t_fixed_load(file)
# the count is the first four bytes of the body, the last body_bytes of a
# one-record file. Below Min the elements are zero, so no dead slot is nonzero
# and the count is the one thing wrong.
splice = fn f, n ->
  at = byte_size(f) - F.t_fixed_body_bytes()
  binary_part(f, 0, at) <> <<n::little-signed-32>> <> binary_part(f, at + 4, byte_size(f) - at - 4)
end
zeroes = IO.iodata_to_binary(F.t_fixed_save([%Lsix.T{vals: [0], n: 7}]))
for {label, bad} <- [{"past Max", splice.(file, 5)}, {"below Min", splice.(zeroes, 0)}, {"negative", splice.(file, -1)}] do
  got = F.t_fixed_load(bad)
  unless match?({:error, :count_over_bound, _}, got), do: raise("count " <> label <> " load: want count_over_bound, got " <> inspect(got))
  body = binary_part(bad, byte_size(bad) - F.t_fixed_body_bytes(), F.t_fixed_body_bytes())
  dec = try do
    F.t_fixed_decode(body, 0)
  catch
    :throw, {:refuse, why} -> {:refused, why}
  end
  unless dec == {:refused, :count_over_bound}, do: raise("count " <> label <> " decode: want a refusal, got " <> inspect(dec))
end
IO.puts("FIXED_FORM_COUNT_OVER_BOUND_OK")
`
	out := runCompleteUnitProbe(t, u, probe)
	if !strings.Contains(out, "FIXED_FORM_COUNT_OVER_BOUND_OK") {
		t.Fatal(out)
	}
}
