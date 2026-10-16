# How generated Elixir reads

Issue #547 asks that generated code read as the language's own: it is held to
the language's formatter and its analyzer at their default strictness, and the
gates go red on any finding. This page is the Elixir half of that promise. It
says what the generated Elixir looks like, which tools hold it, at which
versions, and what each gate does.

## The shape of the output

- **One module per declaration.** Every `type`, `enum` and `union`
  declaration becomes its own module, named `<Package>.<Name>`
  (`Example.Vec3`). A `union` adds a `<Name>Type` module holding its tag
  values. A table's value struct comes from the fixed form
  (`<Package>.<Table>`). Constants and `flags` are not modules: they are
  functions on the file module.
- **The file module.** Each `.schema` file becomes one module named after the
  file (`Example.Types` for `Types.schema`). It holds the file-scope surface:
  constants, flags masks, name helpers and the wire functions of every
  declaration in that file. If a declaration's module would have the same
  name, the declaration keeps it and the file module becomes
  `<Package>.<Base>File`.
- **Table surfaces sit beside them.** `<Base>Block.ex`, `<Base>Cook.ex`,
  `<Base>Fixed.ex` and the unit's `Table`, `TableMessage`, `BlockRuntime`,
  `CookRuntime`, `FixedRuntime` and `UnitView` modules. Generated Elixir is
  self-contained: there is no runtime dependency and no mix project. A test
  leg compiles the `.ex` files directly.
- **Every module has a `@moduledoc`.** A declaration's `///` block becomes its
  `@moduledoc`. A declaration without one gets a one-line description of what
  the module is. Generated modules are public API, so they are documented and
  never hidden with `@moduledoc false`.
- **Names.** Functions and struct fields are snake_case; modules are
  CamelCase. Each enum variant is a zero-arity function (`Example.Color.red()`),
  and `none` is 0.
- **No `@spec`.** The emitters write no typespecs, so none appear on the
  generated readers or writers.
- **Numbers.** Any decimal past 9999 is grouped in threes with underscores
  (`65_536`). That is the formatter's spelling from six digits on, and credo's
  from five. Floats past 9999 are grouped the way credo 1.7.19 reads them:
  the integer part, or, when there is an exponent, the digits after the point
  (`3.4_028_235_677_973_366e38`). The value does not change.

## The tools and their pins

| tool   | version | where it comes from |
|--------|---------|---------------------|
| Erlang/OTP | 29.0.5 (`ELIXIR_PIN_OTP`) | `dist/otp-29.0.5`, see the header of `make/elixir.mk` |
| Elixir and `mix format` | 1.20.4 (`ELIXIR_PIN_ELIXIR`) | `dist/elixir-1.20.4` |
| credo  | 1.7.19 (`ELIXIR_PIN_CREDO`) | `test/elixir-credo/mix.lock`, fetched into `dist/elixir-credo` |

`test/elixir-credo` is a minimal mix project. Its only dependency is credo.
Credo's own three dependencies (bunt, file_system, jason) are pinned with their
hashes in the committed `mix.lock`. Its `.credo.exs` is the file
`mix credo gen.config` writes, unchanged.

## The gates

- `make tables-elixir-emit-format-clean` runs `mix format --check-formatted`
  over the emitters' RAW output: every table unit, the fixed-form units, the
  paired and bench units and the cards unit, written into
  `build/elixir-emit-format/`. Nothing formats the files before the check. Its
  pending list, `ELIXIR_EMIT_FORMAT_PENDING`, is printed on every run.
- `make toolchain-elixir-credo` is the one step that uses the network. It runs
  `mix deps.get --check-locked` and `mix deps.compile` in `test/elixir-credo`,
  with the deps and build directories under `dist/elixir-credo`, once per host.
- `make tables-elixir-credo` first checks the pin and refuses unless the
  compiled credo is `ELIXIR_PIN_CREDO`. It then runs
  `tables-elixir-emit-format-clean` and `mix credo` at credo's DEFAULT
  strictness (no `--strict`, no check filter) over `generated/elixir/*.ex`
  and that raw corpus. It prints the issue count per check and fails on any
  issue. `build/elixir-credo/issues.json` holds the full listing.
- `make tables-elixir-credo-negative-control` plants `# TODO: planted by the
  credo negative control` in every generated cook module through the emitter.
  The gate must then go red and name `Credo.Check.Design.TagTODO`. The plant
  is a TODO and not an unused alias, for two reasons. Credo's default checks
  include no unused-alias check, because an unused alias is a compiler
  warning. And its alias checks report at low priority, which the default
  strictness hides.

Both credo targets run in `tables-elixir-release`, after
`toolchain-elixir-credo`. The control is also in the nightly `elixir-credo`
group of `make/negative-controls.json`. Neither is in `test-elixir`, because
the image used for each pull request has no credo.
`TestCredoGateRunsAtDefaultStrictness` (in
`internal/codegen/elixirtable/credo_test.go`) checks four things: the credo
line passes no strictness or check override, the pin runs first, neither
target can be reached from `test-elixir`, and both can be reached from
`tables-elixir-release`.

## Checks switched off

None. `test/elixir-credo/.credo.exs` is credo's default configuration, and no
directory or file is excluded. Any finding is a defect in the emitter that
wrote the construct, and the fix goes in the Go that writes it, with the check
named in a comment beside the fix.
