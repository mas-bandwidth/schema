defmodule SchemaCredo.MixProject do
  @moduledoc """
  The analyzer host for the Elixir leg (issue #547, the analyzer half): a
  mix project with one dependency, credo, so `mix credo` can run over the
  raw generated Elixir. Its deps and build directories live under dist/,
  which `make toolchain-elixir-credo` populates once per host; the gate
  `make tables-elixir-credo` never fetches.
  """
  use Mix.Project

  def project do
    [
      app: :schema_credo,
      version: "0.1.0",
      elixir: "~> 1.20",
      deps_path: System.get_env("SCHEMA_CREDO_DEPS", "../../dist/elixir-credo/deps"),
      build_path: System.get_env("SCHEMA_CREDO_BUILD", "../../dist/elixir-credo/_build"),
      lockfile: "mix.lock",
      deps: deps()
    ]
  end

  def application, do: []

  defp deps do
    [{:credo, "== 1.7.19", runtime: false}]
  end
end
