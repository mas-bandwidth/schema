defmodule PacketDefaultTest do
  import Bitwise

  alias Packetdefaults.{
    Sample,
    Batch,
    ZeroCount,
    Conditional,
    Choice,
    EmptyOnly,
    Prefix,
    WideMask,
    SplitMask
  }

  alias Packetdefaults.Defaults, as: D
  alias Packetplain.Plain, as: P

  def check(true, _name), do: :ok
  def check(false, name), do: raise("FAILED: " <> name)

  def zero do
    %Sample{name: <<>>, token: <<>>, caps: 0, empty_name: <<>>, empty_token: <<>>, empty_caps: 0}
  end

  def sample do
    %Sample{
      name: <<195, 169, 240, 144, 128, 128>>,
      token: <<92, 110, 92, 116>>,
      caps: 5,
      empty_name: <<>>,
      empty_token: <<>>,
      empty_caps: 0
    }
  end

  def short do
    %Sample{
      name: <<65>>,
      token: <<0, 255>>,
      caps: 2,
      empty_name: <<>>,
      empty_token: <<>>,
      empty_caps: 0
    }
  end

  def pin(dir, name, value, want, type) do
    bytes = File.read!(Path.join(dir, name <> ".bin"))
    bits = File.read!(Path.join(dir, name <> ".bits")) |> String.trim() |> String.to_integer()
    check(apply(D, String.to_atom("write_" <> type), [value]) == bytes, name <> " C++ bytes")
    check(apply(D, String.to_atom("measure_" <> type), [value]) == bits, name <> " C++ bits")

    check(
      apply(D, String.to_atom("read_" <> type), [bytes, bits]) == {:ok, want},
      name <> " exact-bit read values"
    )

    check(
      apply(D, String.to_atom("read_" <> type), [bytes, bits - 1]) == :error,
      name <> " one-bit-short refusal"
    )
  end

  def run(dir) do
    check(%Sample{} == sample(), "packet-default constructor bytes")
    check(D.zero_sample() == zero() and D.zero_sample() != %Sample{}, "zero form is distinct")
    check(%EmptyOnly{} == %EmptyOnly{name: <<>>, token: <<>>, caps: 0}, "explicit empty defaults")

    check(
      %Prefix{} == %Prefix{name: <<195, 169>>, token: <<92, 110>>},
      "short literals use their byte lengths"
    )

    wide = %WideMask{}
    check(wide.high == 1 <<< 63 and wide.all == (1 <<< 64) - 1, "bit63 and all64 masks")
    wide_bytes = <<0, 0, 0, 0, 0, 0, 0, 128, 255, 255, 255, 255, 255, 255, 255, 255>>

    check(
      D.write_wide_mask(wide) == wide_bytes and D.measure_wide_mask(wide) == 128,
      "independent 64-bit wire"
    )

    check(D.read_wide_mask(wide_bytes, 128) == {:ok, wide}, "64-bit read")
    split = %SplitMask{lead: 5, mask: 1 <<< 32, tail: 2}

    check(
      D.write_split_mask(split) == <<5, 0, 0, 0, 40>> and D.measure_split_mask(split) == 38,
      "independent 33-bit wire"
    )

    check(D.read_split_mask(<<5, 0, 0, 0, 40>>, 38) == {:ok, split}, "33-bit read")

    plain = %Packetplain.Sample{
      name: <<195, 169, 240, 144, 128, 128>>,
      token: <<92, 110, 92, 116>>,
      caps: 5
    }

    check(
      P.write_sample(plain) == D.write_sample(%Sample{}) and
        P.measure_sample(plain) == D.measure_sample(%Sample{}),
      "defaultless twin"
    )

    pin(dir, "sample-defaults", %Sample{}, sample(), "sample")
    batch = %Batch{}

    check(
      batch.head == sample() and batch.items == [sample(), sample()] and
        batch.counted == [sample()],
      "nested, fixed and born-count defaults"
    )

    pin(dir, "batch-defaults", batch, batch, "batch")
    # Lists and binaries contain only their used data; no unused backing tail
    # exists in the immutable representation. A zero-count list is empty.
    check(%ZeroCount{}.items == [], "zero-count list")
    pin(dir, "zero-count", %ZeroCount{}, %ZeroCount{}, "zero_count")
    check(%Conditional{} == %Conditional{enabled: true, value: sample()}, "conditional defaults")
    pin(dir, "conditional-on", %Conditional{}, %Conditional{}, "conditional")
    off = %Conditional{enabled: false}
    off_want = %Conditional{enabled: false, value: zero()}
    pin(dir, "conditional-off", off, off_want, "conditional")
    check(%Choice{}.type == 0, "union starts None")
    choice = %Choice{type: 1}
    pin(dir, "choice-sample", choice, choice, "choice")
    pin(dir, "sample-short", short(), short(), "sample")
    pin(dir, "sample-empty", zero(), zero(), "sample")

    for sent <- [short(), zero()] do
      value = %Choice{type: 1, sample: sent}
      bytes = D.write_choice(value)

      for _attempt <- 1..2 do
        check(
          D.read_choice(bytes, D.measure_choice(value)) == {:ok, value},
          "selected payload replaces defaults on each immutable read"
        )
      end
    end

    off_choice = %Choice{type: 2, conditional: off}

    for _attempt <- 1..2 do
      check(
        D.read_choice(D.write_choice(off_choice), D.measure_choice(off_choice)) ==
          {:ok, %Choice{type: 2, conditional: off_want}},
        "untaken branch zeros selected defaults"
      )
    end

    IO.puts(
      "packet defaults Elixir: constructors, eight C++ goldens and immutable read values OK"
    )
  end
end
