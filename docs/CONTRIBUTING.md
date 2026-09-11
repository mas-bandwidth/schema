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

**`make test` refuses a missing pinned toolchain by name, and runs without a
leg only when you name the skip on purpose.** A pin the Makefile names
(`NODE`, `DART`, `JAVA`/`JAVAC`, `ELIXIR`/`MIX`/`ELIXIRC`, `DOTNET`) that does
not resolve stops the run before it starts. Every registered leg is probed, so
one run names every leg it would have skipped and the path each pin looked in
rather than sending you back for the next name; to run the chain without those
legs, name them in `SCHEMA_SKIP_LEGS`, which prints every skip by name and
which the refusal spells out ready to paste:

```bash
make test SCHEMA_SKIP_LEGS=dart,java
```

A gate that passes while its toolchain is missing is a gate with no blade, and
that is not a hypothetical here: a merge once deleted a clone's `dist` link,
`make test` passed over four legs in silence, and a red inside one of them rode
the green run (issue #599). Nothing under `.github` sets `SCHEMA_SKIP_LEGS`:
certification installs every toolchain and overrides the pins, which resolve
and pass the gate. `make toolchain` runs the gate alone, and
`make toolchain-negative-control` proves it still has its blade, on every pin
of every leg. That control runs inside `make test` and on every pull request,
in the `go-test` job of `ci-full.yml`.

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

One gate is **not** in the pull request leg: the **inline-budget gates**,
which fire on compiler-version changes by design and cost most of the wall
clock. They live in `certify.yml`, which runs nightly and on
`gh workflow run certify.yml --ref <branch>`, never on push (the owner's
per-commit rule: a job that runs on every commit finishes in two minutes).

So a change to the C++ emitters wants a dispatch run on its branch before
merging, not just a green pull request.

One more gate is not CI at all. A pull request is not merged until its authors
have signed the [Contributor Assignment Agreement](#the-contributor-assignment-agreement).

## Negative controls

A negative control breaks what a gate watches and requires the gate to go red.
It is how this repository proves a gate is watching something rather than
passing over an empty set. `go run ./tools/negativecontrols check` prints how
many there are, which is the only count that cannot go stale. Every one refuses
when its sabotage patches nothing, so a control whose `sed` pattern has drifted
off the line it aims at says so instead of reading as a pass.

**Every negative control runs on every pull request**, in the `negative
controls (<group>)` jobs, with the exceptions named below. A group is a set
of controls that share a toolchain and a compile and run in one `make -k`
invocation, so one refusal does not hide the ones behind it, and the jobs run
in parallel. The `base` groups need Go, a host C and C++ compiler and the
C/C++ serialize siblings; the `cs`, `js`, `dart`, `java`, `elixir` and `rust`
groups each add the one SDK their controls need, and `block-fuzz` needs two.

**The rule that decides where a control runs is the owner's rule for CI that
runs per commit: one minute, two at the most.** It is a rule about a job, and
a matrix row is a job, so each group is cut to fit two minutes on the runner
rather than the leg as a whole being cut to fit. That is why the map gate runs
as four groups, why the tolerant-wire family runs one control per job (each of
those rebuilds the compiler under a source overlay and then fuzzes the
sabotaged wire, which costs 55 to 75 seconds), and why there are more groups
than toolchains.

A control that does not fit the rule **on its own** is not made to fit by
grouping, so it runs nightly instead, in the `nightly` tier that `certify.yml`
runs on the schedule it already carries. Today that is one control:
`tables-message-form-negative-control`, which drives 49 sabotage rows one
submake each and takes 124 seconds. The message form's other blades stay on
the pull request. Each group in `make/negative-controls.json` names its tier in a
`when` field and says why in a `why` field, and `tools/negativecontrols`
refuses a group that names neither tier, so a control cannot leave the pull
request without landing on the nightly.

The other exceptions are the `excluded` list, and each carries a reason the
test requires to be non-empty. Four of them are umbrella targets with no recipe
of their own, whose leaves already run in groups:
`packet-arm-defaults-negative-controls`, `tables-maps-negative-controls`,
`tables-wire-fuzz-negative-control` and `tables-wire-fuzz-retain-negative-control`.
One is a parameterized worker with no sabotage of its own,
`tables-message-form-one-negative-control`. The last is
`tables-big-endian-negative-control`, which the `big-endian` job already runs
on every pull request: it cross-compiles the tables battery for s390x, and a
second cross-compile on this leg proves nothing the first does not.

**The target list is enumerated, not typed.** `tools/negativecontrols` reads
the Makefile and every file the Makefile includes, collects each explicit
target whose name carries `negative-control`, and holds that set against
`make/negative-controls.json`, which is the plan the leg's matrix comes from.
`go test ./tools/negativecontrols/` fails on any difference in either
direction: a control the makefiles define and the plan does not carry, and a
control the plan names and no makefile defines.

**So the name is the convention, and `-negative-control` is the spelling.** A
control called `<thing>-negative` is invisible to the enumerator, which is a
control in no group, in no exclusion and on no workflow, with every test here
green. A marked head the reader cannot resolve to names, one written through a
variable or as a pattern rule, is refused rather than dropped, for the same
reason.

The same package's tests hold two more joins nothing else in the tree holds:
the two legs are parsed as YAML, and each has to expand `${{ fromJSON(...) }}`
over the matrix job's output rather than a hand-typed include list; and the
toolchain versions in `make/negative-controls.json` have to match
`test/conformance/<lang>/ci.json`, so a runtime bump moves both or neither.

So adding a negative control costs one line in `make/negative-controls.json`,
in the group whose toolchain it needs and whose job still fits the rule with it
added, and forgetting that line is a red test rather than a control that runs
nowhere. A control that cannot run on either tier goes in the same file's
`excluded` list with a reason, which the test requires to be non-empty. Nothing
leaves the leg silently.

Locally:

```bash
go run ./tools/negativecontrols list             # every control, and the file that defines it
go run ./tools/negativecontrols check            # the plan against the makefiles
go run ./tools/negativecontrols matrix nightly   # the groups the nightly tier runs
make -k $(go run ./tools/negativecontrols targets base)
```

The whole set is about thirteen minutes of machine time measured one target at
a time, and no single group is more than a minute of that, so running the group
your change touches before opening a pull request is cheap.

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
| the build | `make/<lang>.mk` | the Makefile's wildcard include; the file registers its `test-<lang>` leg, its conformance build, its bench unit, its goldens and its pinned toolchain |
| the conformance leg | `test/conformance/<lang>/driver` and `ci.json` | the harness discovers the driver; `harness matrix` builds the pull-request matrix from the rows |
| the tables bench leg | `bench/tables/<lang>/leg` | `bench/tables/run.sh` runs every leg |
| the shape gate's exemptions | `bench/<lang>/SHAPE-GATE.allow`, `bench/tables/<lang>/SHAPE-GATE.allow` | the gate reads every ledger under the tree |
| the goldens | `testdata/golden/<lang>/`, `testdata/golden/tables/<unit>-<lang>/` | the tests that pin them |

`make registry` prints what the build discovered, and the registry gate
(`test/conformance/harness/registry_test.go`) plants a fake language in a copy
of the tree and requires the harness, the CI matrix, the bench pass and the
Makefile to find it with no shared file edited. If a port needs to edit a file
that lists languages, that is a defect in the registry, not a step.

A port with a pinned toolchain registers it in the same file and the same way:
`TOOLCHAIN_LEGS += <lang>`, `TOOLCHAIN_PINS_<lang> :=` every pin the leg
probes, and a `toolchain-<lang>` target carrying one
`$(call toolchain_probe,...)` per pin. That is what makes `make test` refuse
the leg by name instead of skipping it, and what makes
`SCHEMA_SKIP_LEGS=<lang>` a skip anyone can read in the log. The negative
control points each pin on that list at a path that does not exist IN TURN,
with the leg's other pins pointed at one that resolves, and requires the
refusal to name that pin. A pin the target probes but the list leaves out is
a probe nothing watches, and deleting it keeps the control green.

**Three shared edits are tolerated, and are the whole list.** The port's
column on [PORTING.md](PORTING.md), the techniques register, is written by
hand — every technique carried, cited or stated impossible — and its gate
reads the columns from the page and holds them to the discovered drivers, so
the column is the edit and no other file lists the language; a toolchain with
no step yet in `.github/workflows/ci-full.yml` (and the `test` job of
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

## The Contributor Assignment Agreement

Contributions require signing the Contributor Assignment Agreement, the CAA. A
workflow checks every human commit author on a pull request against a signature
ledger. The pull request stays red until each of them has signed, and it is not
merged before it is green. Maintainer accounts and bots are allowlisted and
never sign.

You sign once, on your first pull request to any Más Bandwidth repository, by
posting this exact sentence as a comment on that pull request:

> I have read the CAA and I hereby sign it, assigning copyright in my contributions to Más Bandwidth LLC.

Read [the agreement](https://github.com/mas-bandwidth/.github/blob/main/CAA.md)
before you sign it. It is the authority, and what follows is only a summary of
it. You assign the copyright in your contribution to Más Bandwidth LLC. You
keep a perpetual license to use your own work for any purpose. You grant a
patent license covering your contribution. You state that the contribution is
yours to submit, and that any employer with a claim on your work has cleared
it. Third-party material you include is not assigned, so identify it and its
license when you submit it. Section 3 of the agreement governs how Más
Bandwidth licenses what you assign.

Signatures are recorded in
[`signatures/caa.json`](https://github.com/mas-bandwidth/.github/blob/cla-signatures/signatures/caa.json)
on the `cla-signatures` branch of `mas-bandwidth/.github`, one entry per GitHub
user. The ledger covers every Más Bandwidth repository, so one signature is
enough for all of them.

The check is an exact string match. A signature that carries extra text, or one
posted on an issue rather than on a pull request, is still a valid signature,
but a maintainer has to enter it in the ledger by hand. Comment `recheck` on a
pull request to re-run the check once that is done. Any other open pull request
of yours stays red until it is rechecked too.

## License

The compiler ships under the license in [LICENSE](../LICENSE), and the
[README](../README.md#license) says what that means for you and for the code
the compiler generates. Contributions are assigned under the CAA above, and Más
Bandwidth licenses them under the project's license and any other license it
chooses (CAA section 3).
