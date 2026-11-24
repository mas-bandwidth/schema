# elixir/W5 "write checks DEBUG only" (docs/FIXED-FORM-ALGORITHM.md:208)
#
# Write-side bound checks are DEBUG ONLY, by rule. count <= Max and length <= N
# are a caller contract; a release build removes them exactly as it removes
# assert, pays nothing, and clamps nothing. The BEAM has no compile-out assert,
# so this port raises ArgumentError — always on, never clamping.

defmodule W5 do
  def check(what, true), do: IO.puts("  ok   #{what}")
  def check(what, false) do
    IO.puts("  FAIL #{what}")
    :persistent_term.put(:w5_failed, true)
  end

  def raises(name, f) do
    case try(do: {:ok, f.()}, rescue: (e -> {:error, e})) do
      {:error, %ArgumentError{}} -> check(name, true)
      {:ok, _} -> check(name, false)
      {:error, _} -> check(name, false)
    end
  end

  # raises ArgumentError whose message names the guard that tripped, so a
  # value that is wrong some OTHER way (an enum spelled as an integer, a
  # fixed array short) cannot pass for the guard under test.
  def raises_naming(name, needle, f) do
    case try(do: {:ok, f.()}, rescue: (e -> {:error, e})) do
      {:error, %ArgumentError{message: m}} -> check("#{name} (#{inspect(m)})", String.contains?(m, needle))
      {:ok, _} -> check(name, false)
      {:error, e} -> check("#{name} (raised #{inspect(e)})", false)
    end
  end

  # A LoadoutConfig that is valid in every field but the one a case varies:
  # enums are atoms (Tabledemo.Grade.to_int/1 raises on an integer), podium
  # is exactly [3]Grade and backups exactly [2]WeaponConfig.
  def loadout(grades) do
    %Tabledemo.LoadoutConfig{
      grade: :silver, grades: grades,
      podium: [:bronze, :silver, :gold], perks: 0,
      primary: %Tabledemo.WeaponConfig{damage: 10.0},
      backups: [%Tabledemo.WeaponConfig{}, %Tabledemo.WeaponConfig{}], attachments: []
    }
  end

  def run do
    # R.count: a counted array past its declared bound raises — grades is
    # [..4]Grade, so five elements trip R.count(v_grades, 0, 4) and nothing else
    raises_naming("count past max raises — R.count guards write",
      "an array of 5 elements is outside the declared 0..4", fn ->
      Tabledemo.TablesFixed.loadout_config_fixed_write_body(loadout([:bronze, :silver, :gold, :gold, :none]))
    end)

    # R.count with value within bounds succeeds, at the bound itself
    try do
      Tabledemo.TablesFixed.loadout_config_fixed_write_body(loadout([:bronze, :silver, :gold, :gold]))
      check("count within bounds writes successfully", true)
    rescue
      e -> check("count within bounds writes successfully (raised #{inspect(e)})", false)
    end

    # R.ranged: an integer past its declared range raises
    raises("range past max raises — R.ranged guards write", fn ->
      Tblp1.P1Fixed.link_fixed_write_body(%Tblp1.Link{value: 1001, tag: "hello"})
    end)

    raises("range below min raises — R.ranged guards write", fn ->
      Tblp1.P1Fixed.link_fixed_write_body(%Tblp1.Link{value: -1, tag: "hello"})
    end)

    # R.ranged within bounds succeeds
    try do
      Tblp1.P1Fixed.link_fixed_write_body(%Tblp1.Link{value: 500, tag: "hi"})
      check("ranged value within bounds writes successfully", true)
    rescue
      _ -> check("ranged value within bounds writes successfully", false)
    end

    # R.text_slack: text past its declared bound raises
    raises("text past bound raises — R.text_slack guards write", fn ->
      Tblp1.P1Fixed.link_fixed_write_body(%Tblp1.Link{value: 0, tag: "far too long"})
    end)

    # R.text_slack at its declared bound succeeds
    try do
      Tblp1.P1Fixed.link_fixed_write_body(%Tblp1.Link{value: 0, tag: "12345678"})
      check("text at declared bound writes successfully", true)
    rescue
      _ -> check("text at declared bound writes successfully", false)
    end

    # VERDICT
    if :persistent_term.get(:w5_failed, false) do
      IO.puts("\nFAILED: write checks DEBUG only — one or more assertions did not hold")
      System.halt(1)
    else
      IO.puts("\nOK: write checks DEBUG only — write-side bound checks exist and raise on violation")
    end
  end
end

W5.run()