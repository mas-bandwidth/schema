defmodule SchemaElixirStreamData.MixProject do
  use Mix.Project

  def project do
    [
      app: :schema_elixir_streamdata,
      version: "0.1.0",
      elixir: "~> 1.20",
      deps: deps()
    ]
  end

  defp deps do
    [{:stream_data, "== 1.4.0"}]
  end
end
