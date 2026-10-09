# THE PAIRED CORPUS (docs/SPEC-TABLES.md §3.4): the bench's own sixty-four
# logical records, on the PACKET wire and on this one.
#
# The C++ reference wrote the form-3 file out of the canonical packet corpus, so
# the two files carry the same sixty-four values in two wires. This leg reads
# each of them with the port's own codec for that wire and requires the values
# to agree field for field, then writes the fixed file back and requires the
# bytes to be the reference's exactly.
#
# THE TWO DECODERS SHARE NO CODE. The packet one is a bit cursor over 32-bit
# groups and 40-bit read windows; the fixed one is one plan and one binary
# pattern match over byte-width fields. An offset mistake that a writer and a
# reader of the SAME wire would round trip perfectly is one this comparison
# still catches, which is why the values are stated by the other wire rather
# than by a file beside this one.
#
# IT IS ALSO WHERE THE WIDE KINDS ARE MEASURED. BenchMixed carries `int128`,
# `uint128`, `fixed(24, 8)` and `ufixed(8, 8)`, which the two ACCELERATORS
# refuse in this backend (schema#366) and which §3.4's constant-size table
# carries fine — an `int128` as sixteen bytes low half first, a fixed-point
# field as the raw scaled integer at its storage width.

[variants, fixed_path] = System.argv()

packet = File.read!(variants)
count = 64
stride = div(byte_size(packet), count)

if stride * count != byte_size(packet) do
  IO.puts("FAILED: #{variants} is not #{count} records of one stride")
  System.halt(1)
end

want = File.read!(fixed_path)

expected =
  for k <- 0..(count - 1)//1 do
    record = binary_part(packet, k * stride, stride)

    case Bench.Bench.read_bench_mixed(record, stride * 8) do
      {:ok, value} -> value
      :error -> raise "the packet corpus record #{k} did not read"
    end
  end

case Bench.WrapFixed.fixed_table_fixed_load(want) do
  {:ok, values, report} ->
    if length(values) != count do
      IO.puts("FAILED: the fixed corpus holds #{length(values)} records, not #{count}")
      System.halt(1)
    end

    # A CLEAN READ OF THIS BUILD'S OWN RECORDS MOVES NO COUNTER.
    if {report.unknown, report.kind_mismatch, report.clamped, report.widened, report.malformed} !=
         {0, 0, 0, 0, false} do
      IO.puts("FAILED: a same-schema read moved a counter: #{inspect(report)}")
      System.halt(1)
    end

    mismatched =
      Enum.filter(Enum.zip(values, expected), fn {fixed, packet_value} ->
        fixed.value != packet_value
      end)

    if mismatched != [] do
      {fixed, packet_value} = hd(mismatched)
      IO.puts("FAILED: #{length(mismatched)} of #{count} records disagree with the packet wire")
      IO.puts("  fixed  #{inspect(fixed.value, limit: 40)}")
      IO.puts("  packet #{inspect(packet_value, limit: 40)}")
      System.halt(1)
    end

    got = Bench.WrapFixed.fixed_table_fixed_save(values)

    if got != want do
      n = min(byte_size(got), byte_size(want))
      at = Enum.find(0..(n - 1)//1, fn i -> :binary.at(got, i) != :binary.at(want, i) end)
      IO.puts("FAILED: the fixed corpus did not come back byte for byte")

      IO.puts(
        "  first byte differing from the C++ reference at offset #{inspect(at)}; " <>
          "#{byte_size(got)} bytes vs #{byte_size(want)}"
      )

      System.halt(1)
    end

    IO.puts(
      "fixed form paired corpus: #{count}/#{count} records agree with the packet wire, " <>
        "#{byte_size(want)} bytes identical to the C++ reference " <>
        "(body #{Bench.WrapFixed.fixed_table_fixed_body_bytes()}, " <>
        "hash 0x#{Integer.to_string(Bench.WrapFixed.fixed_table_fixed_hash(), 16)})"
    )

  other ->
    IO.puts("FAILED: the fixed corpus did not read: #{inspect(other)}")
    System.halt(1)
end
