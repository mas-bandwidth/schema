# The native gate

Generated code has to read as the target language's own. That is the law in
issue #547, and this directory is the half of it a machine can check: for every
port, the language's standard formatter in check mode and its standard analyzer
at default strictness, run over the generated code of both corpora, red on any
finding.

Two corpora, always both. `generated/<lang>` is what the packet backend emits
from `examples/`; `build/tables-generated-<lang>` is what the table backend
emits from `tables/`. They are different emitters, and one being clean says
nothing about the other.

## Where a leg lives

The check itself is `make native-<lang>`, and it lives in that port's
`make/<lang>.mk`, beside its test and conformance legs. The language's own file
is where the decision about which instruments a language is held to belongs.

This directory holds only what CI needs to run it:

    test/native/<lang>/ci.json    the port's CI row
    test/native/matrix            the command that prints the matrix from them

`ci.json` names the make target, the sibling runtime the analyzer needs on disk
to typecheck what the emitter wrote, the toolchain steps CI installs, and the
make overrides that point the leg at them. Every key is optional except
`targets`; a leg that needs no runtime leaves `runtime` out and CI clones
nothing.

Some legs also carry their instrument's own configuration here — the ESLint
config and its lockfile under `js/`, the Credo project under `elixir/`. A leg
whose analyzer needs no project of its own carries nothing but its row.

## Registering a port

Add `native-<lang>` to `make/<lang>.mk`, register it with
`NATIVE_LEGS += native-<lang>`, and add `test/native/<lang>/ci.json`. Nothing
else is edited: `.github/workflows/ci.yml` fans out over whatever the registry
prints.

A port with a conformance driver and no native row is a build failure, in
`test/native/matrix` and in `go test ./...`. That is deliberate. A port that
lands without its native gate is exactly the gap this registry exists to close.

## What is pinned

Every instrument, and none of them from the machine's PATH by accident:

| leg | formatter | analyzer |
|---|---|---|
| c | clang-format 18, `.clang-format` at the repository root | clang-tidy 18, default checks |
| cpp | clang-format 18, the same style file | clang-tidy 18, default checks |
| cs | `dotnet format whitespace --verify-no-changes` | the .NET analyzers, default mode, warnings as errors |
| dart | `dart format --set-exit-if-changed` | `dart analyze` |
| elixir | `mix format --check-formatted` | Credo, pinned in `elixir/mix.exs`, default strictness |
| go | `gofmt -l` | `go vet` |
| java | google-java-format, AOSP profile, pinned by version and sha256 | `javac -Xlint:all -Werror` |
| js | none — JavaScript has no standard formatter | ESLint, pinned by lockfile, its own recommended set |
| rust | `cargo fmt --check` | `cargo clippy -D warnings` |

The C and C++ style file is the one thing on that list a tool does not supply:
clang-format has no canonical style, so `.clang-format` at the repository root
is the style, written from the estate's own C and C++ and stating why each
option is what it is.
