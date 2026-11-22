# Top-level code in an .ex file runs each time the file is compiled: the line
# below is the compile counter checkOffsetsCompileOnce reads (the test puts
# its log path where @@COMPILE_LOG@@ stands).
File.write!(@@COMPILE_LOG@@, "BuildVersion.ex\n", [:append])

defmodule Fixture.BuildVersion do
  def build_version, do: "offsets-compile-once"
end
