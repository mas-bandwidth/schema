# cards_test.exs — the ExUnit tests for the Elixir card facade
# (tables/cards/elixir/cards.ex). Each test is named exactly as the card's
# DONE-WHEN names it. The tests run the Elixir toolchain over files in the
# checkout and nothing else.
ExUnit.start()

defmodule Cards.CardsTest do
  use ExUnit.Case, async: true

  alias Cards.Cards

  @fuzz_wire "tables/cards/fuzz/card-populated.wire"
  @fuzz_json "tables/cards/fuzz/card-populated.json"
  @conformance "tables/cards/conformance"

  # TestCardRoundTripsThePopulatedFixture loads the committed populated wire and
  # compares every field with card-populated.json, proves ToJSON and FromJSON
  # reproduce both files exactly, and shows the record is unchanged when the
  # caller's input and a Wire result are mutated (docs/SPEC-TABLES.md §6.2, §16).
  test "TestCardRoundTripsThePopulatedFixture" do
    wire = File.read!(@fuzz_wire)
    json = File.read!(@fuzz_json)

    {:ok, record, report} = Cards.load(wire)
    assert report.unknown == 0
    assert report.kind_mismatch == 0
    assert report.clamped == 0
    assert report.malformed == false

    # ToJSON reproduces card-populated.json byte for byte.
    {:ok, text, _} = Cards.to_json(record)
    assert text == json

    # FromJSON reproduces the wire byte for byte.
    {:ok, record2} = Cards.from_json(json)
    assert Cards.wire(record2) == wire

    # ToJSON of the FromJSON record reproduces the JSON again.
    {:ok, text2, _} = Cards.to_json(record2)
    assert text2 == json

    # The record owns its wire: mutating the caller's input leaves it unchanged.
    mutated_input = mutate(wire)
    assert Cards.wire(record) == wire
    assert Cards.wire(record) != mutated_input

    # Mutating a Wire result leaves the record unchanged.
    returned = Cards.wire(record)
    mutated_return = mutate(returned)
    assert Cards.wire(record) == wire
    assert Cards.wire(record) != mutated_return
  end

  # TestCardBoundsRefuseBeforePublishing proves the customer ceilings: 65536 body
  # bytes are accepted and 65537 refused naming body and 65536; 512 entries are
  # accepted and 513 refused naming the field, for each collection
  # (docs/SPEC-TABLES.md §2.5, §2.9).
  test "TestCardBoundsRefuseBeforePublishing" do
    body_json = fn len ->
      ~s({"id":"x","body":"#{String.duplicate("y", len)}"})
    end

    assert {:ok, _} = Cards.from_json(body_json.(65_536))

    assert {:error, reason} = Cards.from_json(body_json.(65_537))
    assert reason =~ "body"
    assert reason =~ "65536"

    list_json = fn field, entries, elem ->
      elems = Enum.map_join(1..entries, ",", fn _ -> ~s({"s":"#{elem}"}) end)
      ~s({"id":"x","#{field}":[#{elems}]})
    end

    for {field, elem} <- [{"paths", "p"}, {"_new", "n"}, {"shared", "s"}, {"needs", "d"}] do
      assert {:ok, _} = Cards.from_json(list_json.(field, 512, elem))
      assert {:error, reason} = Cards.from_json(list_json.(field, 513, elem))
      assert reason =~ field
      assert reason =~ "512"
    end

    consumers_json = fn entries ->
      elems =
        Enum.map_join(1..entries, ",", fn _ ->
          ~s({"kind":"k","who":"w","route":"r","model":"m","started":0,"ended":0,"outcome":"o","line":"l","cost_micro_usd":0})
        end)

      ~s({"id":"x","consumers":[#{elems}]})
    end

    assert {:ok, _} = Cards.from_json(consumers_json.(512))
    assert {:error, reason} = Cards.from_json(consumers_json.(513))
    assert reason =~ "consumers"
    assert reason =~ "512"
  end

  # TestParseRenderMatchesGo parses every parse case in the conformance corpus to
  # the expected wire and renders every render case to the expected text byte for
  # byte; the rejected case refuses with the stranded key (tables/cards/parse.go,
  # tables/cards/render.go).
  test "TestParseRenderMatchesGo" do
    # parse: normalized-valid parses to the expected wire.
    brief = File.read!(Path.join([@conformance, "cases", "normalized-valid.md"]))
    expected_wire = File.read!(Path.join([@conformance, "expected", "normalized-valid.wire"]))
    assert {:ok, record} = Cards.parse(brief)
    assert Cards.wire(record) == expected_wire

    # parse: rejected-legacy refuses with the stranded key PRIORITY.
    rejected = File.read!(Path.join([@conformance, "cases", "rejected-legacy.md"]))
    assert {:error, reason} = Cards.parse(rejected)
    assert reason =~ "stranded header key"
    assert reason =~ "PRIORITY"

    # render: every render case renders to the expected text byte for byte.
    for name <- ~w(card-populated initial-populated consumers-populated empty-consumers) do
      wire = File.read!(Path.join([@conformance, "cases", "#{name}.wire"]))
      expected_txt = File.read!(Path.join([@conformance, "expected", "#{name}.txt"]))
      {:ok, record, _} = Cards.load(wire)
      {:ok, txt, _} = Cards.render(record)
      assert txt == expected_txt, "render of #{name} differs"
    end
  end

  # TestSaveSurfaceAcceptsEmptyConsumers pins the shared-corpus save surface:
  # the JSON spelling of a nil enum is "None" (docs/SPEC-TABLES.md §16), and the
  # generated Elixir text reader must round-trip it exactly as the Go leg does,
  # writing empty-consumers.wire. The generated reader maps "None" to the nil
  # variant, exactly as the Go reference does (internal/tabletext/value.go
  # EnumValue), so the save surface writes the wire byte for byte
  # (docs/SPEC-TABLES.md §4.2).
  test "TestSaveSurfaceAcceptsEmptyConsumers" do
    json = File.read!(Path.join([@conformance, "cases", "empty-consumers.json"]))
    expected_wire = File.read!(Path.join([@conformance, "expected", "empty-consumers.wire"]))

    assert {:ok, record} = Cards.from_json(json)
    assert Cards.wire(record) == expected_wire
  end

  # mutate returns a copy of the binary with its first byte flipped, standing in
  # for a caller mutating its own buffer (docs/SPEC-TABLES.md §6.2).
  defp mutate(<<b, rest::binary>>), do: <<Bitwise.bxor(b, 0xFF), rest::binary>>
  defp mutate(<<>>), do: <<>>
end
