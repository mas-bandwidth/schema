File.write!(@@COMPILE_LOG@@, "Runtime.ex\n", [:append])

# The runtime file: the one file each variant replaces and compiles again.
# Its last line is the composition the variants rewrite (cases.tsv `variant`).
defmodule Fixture.Runtime do
  def halves(data, at) do
    lo = Fixture.Shared.u32(data, at)
    hi = Fixture.Shared.u32(data, at + 4)
    hi * 0x100000000 + lo
  end
end
