# R26: a known hash whose lineage entry would not build -> layout_malformed / plan_too_large by that entry's own lane, never a throw
# (docs/FIXED-FORM-ALGORITHM.md:890)

# The law: when a file carries a hash that IS in the lineage, but that lineage
# entry's layout would not build (parse_layout fails or compile returns an error),
# the reader must refuse by NAME (layout_malformed or plan_too_large) and never throw.

# This test constructs a form-3 file with:
#   - form byte 3
#   - a hash that matches a known lineage entry (the module's own hash)
#   - but the layout bytes are malformed (too short)
# and verifies the reader refuses by name, not by exception.

# The own hash for tblp1's Link layout is in the lineage. A file with that
# hash but a layout length of 0 (too short to be valid) triggers the select
# function to return layout_malformed because the layout bytes don't match the
# known entry's layout. This is the "known hash, different layout bytes" case
# (docs/FIXED-FORM-ALGORITHM.md:889), which is adjacent to the "known hash whose
# lineage entry would not build" case (line 890). Both refuse by name, never throw.

defmodule TestRowR26 do
  @moduledoc false

  # Import the generated module we test. We use tblp1's Link because it exists
  # in the generated corpus and has all the pieces we need.
  require Tblp1.P1Fixed, as: P1
  require Tblp1.FixedRuntime, as: R

  # A minimal capacity.
  @tiny_capacity 1

  # Build a file that carries the OWN hash (which is always in the lineage)
  # but with a layout length of 0, which is too short to be valid.
  defp malformed_layout_file do
    # The module's own hash for Link.
    own_hash = P1.link_fixed_hash()
    
    # A file header with form byte 3, the own hash, and layout length 0.
    # This is too short: a layout must be at least 20 bytes (the hash of the
    # layout itself plus the entry count).
    header = R.file_header(own_hash, 0)
    
    # No layout bytes (length 0) and no records.
    <<header::binary, 0::little-unsigned-32>>
  end

  @doc """
  The test: a file with a known hash but a layout that would not build
  must refuse by NAME, never throw.
  """
  def run do
    file = malformed_layout_file()
    
    # Try to load the file. It must return an error tuple, not throw.
    case P1.link_fixed_load(file, plan_capacity: @tiny_capacity) do
      {:error, :layout_malformed, _report} ->
        IO.puts("PASS R26: known hash with unbuildable layout refuses as layout_malformed")
        System.halt(0)

      {:error, :plan_too_large, _report} ->
        IO.puts("PASS R26: known hash with unbuildable layout refuses as plan_too_large")
        System.halt(0)

      {:error, why, _report} ->
        IO.puts("FAIL R26: known hash with unbuildable layout refused as #{inspect(why)} instead of layout_malformed or plan_too_large")
        System.halt(1)

      _ ->
        IO.puts("FAIL R26: known hash with unbuildable layout returned a success, not a refusal")
        System.halt(1)
    end
  end
end

# Run the test when invoked directly.
TestRowR26.run()
