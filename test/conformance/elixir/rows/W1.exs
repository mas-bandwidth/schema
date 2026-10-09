# W1: write slack is template zeros
# SPEC: docs/FIXED-FORM-ALGORITHM.md:1724
# "Text and array slack - the writer writes length units and count elements
# onto the zeroed template and stops, never the caller's leftovers or an
# element's default image"
#
# TEST: Write a zero-form value and verify no non-zero bytes appear beyond
# the wire payload. The writer must not leave caller's template slack.

# Load generated Wire module using path relative to repo root
repo_root = Path.expand("../../../..", __DIR__)
Code.require_file(Path.join(repo_root, "generated/elixir/Wire.ex"))
Code.require_file(Path.join(repo_root, "generated/elixir/Render.ex"))

defmodule W1 do
  def main do
    # Write the zero-form render_sprite
    # zero_render_sprite/0 returns the zero form
    zero = Example.Render.zero_render_sprite()
    wire = Example.Render.write_render_sprite(zero)

    # Measure wire size
    bits = Example.Render.render_sprite_max_bits()
    bytes = div(bits + 7, 8)

    # Verify output size matches wire size exactly
    got_size = byte_size(wire)
    if got_size != bytes do
      IO.puts("FAIL: wire size #{got_size} != expected #{bytes}")
      System.halt(1)
    end

    # Verify no non-zero bytes beyond declared wire (none expected here)
    # This is the core check: the writer should not leave any template slack
    # that could carry caller's leftovers.
    non_zero =
      wire
      |> :binary.bin_to_list()
      |> Enum.count(fn b -> b != 0 end)

    if non_zero > 0 do
      IO.puts("FAIL: found non-zero bytes in output (slack not zeroed)")
      System.halt(1)
    end

    # Additional check: writing the same value twice should produce identical bytes
    wire2 = Example.Render.write_render_sprite(zero)
    if wire != wire2 do
      IO.puts("FAIL: non-deterministic write output")
      System.halt(1)
    end

    IO.puts("ok W1: write slack is template zeros")
  end
end

W1.main()
