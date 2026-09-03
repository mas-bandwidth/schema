# Contributing

Thanks for looking. This is a small project with a small team, and the most
useful thing you can do is tell us where it broke for you.

## What is most wanted

**Bug reports with a schema that reproduces it.** The compiler is fuzzed, but
fuzzing finds crashes, not wrong output. A schema that produces code that
compiles and is subtly wrong is the most valuable report we can get.

**Cross-language divergence.** If C++ and Go disagree on a single bit for the
same schema and the same values, that is the most serious class of bug this
project has, and it is worth interrupting anything else to fix. See
[SECURITY.md](SECURITY.md) — report it privately if the divergence affects a
read path.

**Documentation that lied to you.** If something in the docs did not match what
the compiler did, that is a bug in the docs and worth an issue.

**Cap'n Proto, Protobuf or FlatBuffers comparisons we got wrong.** The numbers
in [COMPARISON.md](COMPARISON.md) are measured by a committed script. If we
modelled one of those formats inefficiently, say so — the script is there so
the claim can be checked rather than believed.

## Building

Needs Go 1.26+, a C++17 compiler, a C99 compiler, and — for the full
cross-language test — the Rust, Go, Node.js, .NET, Dart, Java and Erlang/Elixir
toolchains (the Makefile pins Dart SDK 3.13.2, Temurin JDK 21.0.12.1 and
Erlang/OTP 29.0.5 + Elixir 1.20.4; unpack them under `dist/` per the Makefile's
`DART`, `JAVA`/`JAVAC` and `ELIXIR`/`MIX` notes, or set `DART=dart JAVA=java
JAVAC=javac ELIXIR=elixir MIX=mix` if compatible versions are on your PATH).

The serialize runtimes must be checked out as **siblings** of this
repository:

```bash
git clone https://github.com/mas-bandwidth/serialize.git
git clone https://github.com/mas-bandwidth/serialize.c.git
git clone https://github.com/mas-bandwidth/serialize.go.git
git clone https://github.com/mas-bandwidth/serialize.js.git
git clone https://github.com/mas-bandwidth/serialize.rs.git
git clone https://github.com/mas-bandwidth/serialize.cs.git
# no serialize.dart or serialize.java clone: generated Dart and Java are self-contained
git clone https://github.com/mas-bandwidth/schema.git
cd schema && make test
```

`make test` builds the compiler, generates the corpus in all nine languages,
compiles each, and compares the emitted wire against pinned goldens. That
cross-language bit-identity check is the property this project exists to
provide, so a change that breaks it is wrong until proven otherwise.

The Makefile's `SERIALIZE*` variables override the sibling paths if you keep
them elsewhere.

## The gates a change has to pass

CI runs on Linux and macOS, and both must be green:

- `make test` — the cross-language corpus and the goldens.
- `go test ./internal/fuzz/` — the seeded fuzz corpus, which re-runs every
  crasher ever found.

Run both locally before opening a pull request.

A third rides every pull request on Windows: **`msvc`** — cl
`/W4 /WX /std:c++17 /permissive-` over the generated C++ corpus, one
translation unit at a time, on a pinned `windows-2025` image, with a negative
control that must go red on a GNU extension. Visual C++ is a hard requirement
here, so what the compiler emits is compiled with cl before a change lands.

One gate is **not** in the pull request leg, and runs on push to main,
nightly, and on `gh workflow run ci.yml --ref <branch>`: the
**inline-budget gates**, which fire on compiler-version changes by design and
cost most of the wall clock.

So a change to the C++ emitters wants a dispatch run on its branch before
merging, not just a green pull request.

## Changing generated output

Any change to a backend's emitted code will move the goldens, and that is
expected — but **moving a golden is a claim that the new output is correct**,
not a step to get CI green. Say in the pull request why the wire changed and
whether it is a breaking change for anyone who has already shipped data.

If a change alters the wire, it changes the protocol id, which means every
deployment built on the old one has to redeploy both sides. That is a real cost
to somebody, so it needs to be worth it.

## Adding a language

A backend is a Go package under `internal/codegen/` that walks the same IR the
existing nine consume, plus one file in `compiler/` — `target_<lang>.go` —
that registers it as a `compiler.Generator`, the public registration interface
and the only way any target reaches the driver. The cross-language harness is
what makes this tractable: generate the corpus in your language, encode the
same values, and the goldens tell you immediately whether you agree with the
other targets bit for bit.

**Every registry a port joins is one file or one directory per language,
discovered rather than listed**, so a port touches nothing another port
touches and two ports landing in one week do not conflict:

| what | the language's own file | how it is found |
|---|---|---|
| the target | `compiler/target_<lang>.go` (`target_javascript.go`: a `_js` suffix is a Go build constraint) | its `init` registers the generator |
| the runtime's claimed names | `internal/tablenames/<lang>.go` | its `init` defines the backend's bit and names |
| the compiler's tests | `compiler/tables<lang>_test.go` | the package |
| the build | `make/<lang>.mk` | the Makefile's wildcard include; the file registers its `test-<lang>` leg, its conformance build, its bench unit and its goldens |
| the conformance leg | `test/conformance/<lang>/driver` and `ci.json` | the harness discovers the driver; `harness matrix` builds the pull-request matrix from the rows |
| the tables bench leg | `bench/tables/<lang>/leg` | `bench/tables/run.sh` runs every leg |
| the shape gate's exemptions | `bench/<lang>/SHAPE-GATE.allow`, `bench/tables/<lang>/SHAPE-GATE.allow` | the gate reads every ledger under the tree |
| the goldens | `testdata/golden/<lang>/`, `testdata/golden/tables/<unit>-<lang>/` | the tests that pin them |

`make registry` prints what the build discovered, and the registry gate
(`test/conformance/harness/registry_test.go`) plants a fake language in a copy
of the tree and requires the harness, the CI matrix, the bench pass and the
Makefile to find it with no shared file edited. If a port needs to edit a file
that lists languages, that is a defect in the registry, not a step.

**Three shared edits are tolerated, and are the whole list.** The port's
column on [PORTING.md](PORTING.md), the techniques register, is written by
hand — every technique carried, cited or stated impossible — and its gate
reads the columns from the page and holds them to the discovered drivers, so
the column is the edit and no other file lists the language; a toolchain with
no step yet in `.github/workflows/ci.yml` (and the `test` job of
`certify.yml`) adds one step, keyed on a new `ci.json` field; and the
per-language prose in [SPEC.md](SPEC.md), [SPEC-TABLES.md](SPEC-TABLES.md)
and [USAGE.md](USAGE.md) is prose, written by hand where the language's
section sits. Those pages have no generated table today; when one exists it
will come from the conformance matrix.

You do not have to be in this repository to try one. `compiler.Generator` is
public, so a generator can live in your own module, register on a
`compiler.Compiler`, and read the same `ir` the built-in backends read — see
[Embedding the compiler](USAGE.md#embedding-the-compiler). That is the cheap way
to prototype a target before proposing it here.

That mechanism is real, and so is the work. Before starting, open an issue —
a backend that lands and then goes unmaintained is worse for users than no
backend, because the docs will claim support that has quietly rotted.

## Fuzzing

```bash
go test ./internal/fuzz/ -fuzz FuzzPipeline -fuzztime 60s
```

If you find a crasher, the input file belongs in `testdata/fuzz/` as a
permanent regression case. Please include it in the pull request — the corpus
is more valuable than the individual fix, because it stops the whole class
coming back.

## Style

Match the surrounding code. The Go follows standard `gofmt`; the comments in
this codebase tend to explain *why* rather than *what*, and often reference the
SPEC section that governs the rule. That is deliberate — [SPEC.md](SPEC.md) is
the normative document, and a comment that points at it survives longer than
one that restates the code.

If a change and the SPEC disagree, one of them is wrong and the pull request
should say which.

## Licence

The compiler is AGPL-3.0, with an explicit exception for the code it generates
(see [LICENSE](../LICENSE)). Contributions are accepted under those terms. There
is no CLA.
