# Credo's config for the native gate (issue #547). One check set, the default
# one Credo ships, over the generated Elixir of both corpora and nothing else.
# `mix credo` runs it at DEFAULT strictness; --strict is a different question
# and not the one the law asks.
%{
  configs: [
    %{
      name: "default",
      files: %{
        included: [
          "../../../generated/elixir/",
          "../../../generated/elixir-ludicrous/",
          "../../../build/tables-generated-elixir/"
        ],
        excluded: []
      },
      strict: false,
      checks: %{}
    }
  ]
}
