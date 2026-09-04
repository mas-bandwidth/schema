# The Elixir native gate's Credo project (issue #547). It carries no source of
# its own: it exists so that `mix credo` has a project to run in, and so the
# analyzer's version is pinned here rather than taken from whatever a machine
# happens to have installed.
defmodule SchemaNative.MixProject do
  use Mix.Project

  def project do
    [
      app: :schema_native,
      version: "0.0.0",
      elixir: "~> 1.17",
      elixirc_paths: [],
      deps: deps()
    ]
  end

  # Credo, at an exact version: a release that adds a check has to be a
  # deliberate bump here, not a Tuesday.
  defp deps do
    [{:credo, "1.7.19", only: [:dev, :test], runtime: false}]
  end
end
