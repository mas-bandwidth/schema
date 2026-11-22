File.write!(@@COMPILE_LOG@@, "Shared.ex\n", [:append])

# A shared module that names the build version at COMPILE time through a
# module attribute, as a generated Cook or Block module does.
defmodule Fixture.Shared do
  @build Fixture.BuildVersion.build_version()

  def build, do: @build

  def u32(data, at) do
    <<_::binary-size(at), v::little-unsigned-32, _::binary>> = data
    v
  end
end
