File.write!(@@COMPILE_LOG@@, "Reader.ex\n", [:append])

# The call site that reaches the runtime's composition, as a generated block
# reader's offset_of does.
defmodule Fixture.Reader do
  alias Fixture.Runtime, as: R

  def offset_of(data), do: R.halves(data, 0)
end
