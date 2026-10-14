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
      {:error, e} -> check(name, false)
    end
  end

  def run do
    # R.count: a counted array past its declared bound raises
    raises("count past max raises — R.count guards write", fn ->
      Tabledemo.TablesFixed.loadout_config_fixed_write_body(
        %Tabledemo.LoadoutConfig{
          grade: 2, grades: [1, 2, 3, 4, 5],
          podium: [1, 2, 3], perks: 0,
          primary: %Tabledemo.WeaponConfig{damage: 10.0},
          backups: [], attachments: []
        }
      )
    end)

    # R.count with value within bounds succeeds
    try do
      Tabledemo.TablesFixed.loadout_config_fixed_write_body(
        %Tabledemo.LoadoutConfig{
          grade: 2, grades: [1, 2, 3],
          podium: [1, 2, 3], perks: 0,
          primary: %Tabledemo.WeaponConfig{damage: 10.0},
          backups: [], attachments: []
        }
      )
      check("count within bounds writes successfully", true)
    rescue
      _ -> check("count within bounds writes successfully", false)
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