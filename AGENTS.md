# AGENTS.md — the one page to read before touching this repo

Every harness loads this file: Claude Code, OpenCode, Codex and anything else
that reads `AGENTS.md`. There is no per-harness copy. The long maintainer
context that used to live in `CLAUDE.md` is now
[docs/MAINTAINERS.md](docs/MAINTAINERS.md), unchanged.

**Who this repo is for.** Schema is the data language for games: you declare
constants, enums, flags, types and tables once, and the compiler generates
bit-packed serialization code in **nine languages** — C, C++, C#, Dart, Elixir,
Go, Java, JavaScript and Rust. The compiler is Go (`cmd/` + `internal/`, public
API `compiler/` and `ir/`). The compiler is AGPL-3.0; **the code it generates is
yours**, by an explicit permanent grant in [LICENSE](LICENSE).

## The first three commands

```
make                          # builds bin/schema, generates all nine, compiles and runs the legs
make test                     # the full matrix: wire goldens, diagnostics, fuzzers, soaks
bin/schema check <dir>        # read a directory of .schema files and say yes or no
```

`bin/schema generate --lang c|cpp|cs|dart|elixir|go|java|js|rust --out <dir> <dir>`
is the other verb you will want, and `make check` runs the compiler over the
corpus in `examples/`.

## The ten rules never to break

1. **The specs are normative.** [docs/SPEC.md](docs/SPEC.md) is the source of
   truth for the type wire and [docs/SPEC-TABLES.md](docs/SPEC-TABLES.md) for
   the table wire. Where the code and a spec disagree, one of them has a bug and
   the tests decide which. **Read the spec before the code.**
2. **C++ is the reference.** C++ writes the pins in `testdata/wire/` and
   `testdata/golden/`; every one of the other eight legs byte-compares against
   them. Cross-language wire identity is a standing gate, and so is
   fixed(I, F)/int128/uint128 identity in the ludicrous legs.
3. **The wire is law.** A change that moves shipped bytes is a deliberate,
   documented act ([docs/VERSIONING.md](docs/VERSIONING.md)), never a silent
   re-pin of a golden to make a leg go green.
4. **Write-side checks are DEBUG ONLY.** No runtime ever promises to keep write
   asserts in a release build — removing them is the point. Seven targets compile
   them out (`serialize_assert` under `NDEBUG`, `debug_assert!`,
   `Debug.Assert`/`[Conditional("DEBUG")]`, `assert` under `-ea` and
   `--enable-asserts`, the JS checked/production fork); Go and Elixir hold theirs
   in every build because their languages have no debug-only idiom, and that is
   deliberate. **No target panics and none throws.**
5. **The read side is untouched by that doctrine.** A read faces untrusted data
   and keeps every mandated check, in every build; the tolerant read IS the
   verifier, and the wire fuzzer is the gate on that claim.
6. **`make` is the one entry** for build, generate, test and bench. A leg is
   added as a target, never as a script somebody remembers to run.
7. **`schema fmt` is the only command that writes a `.schema` file.** Everything
   else reads and leaves your files alone, so a read-only checkout, a sandboxed
   build and an editor integration all work.
8. **The public Go API is `compiler/` and `ir/`; everything else stays
   `internal/`.** An export is a semver commitment, and it justifies itself on
   schema's own needs or it stays internal. `cmd/schema` is a client of that API.
9. **Never bench ungated, and never optimize on a vibe.** A runner byte-compares
   every pinned instance and round-trips it BEFORE producing a number, and
   refuses to bench on a mismatch. Unit test → soak → profile → optimize on a
   profile conviction, with predictions banked before measuring and paired
   before/after in one sitting. **A lever proven in one language is only a
   THEORY in the next.**
10. **The protocol layer stays out of the language.** Schema is types,
    bitpacking, enums, constants and tables. `message` and `object` are reserved
    words the parser refuses by name; build your own message types from the
    primitives.

**Two more that are about this tree, not the wire.** The corpus in `examples/`
must always compile under the spec as written — that invariant is `make check`
and it has caught a real gap every time it ran. And **this repo describes our own
work**: external collaborators, their people and their codebases are not named in
the tree; feedback is folded in as learnings and as evidence on a spec section.

## How work lands

Branch, open a pull request into `main`, and let the checks run — the CI badge in
[README.md](README.md) is the gate. **PRs into `main` get the full treatment.**
An optimization PR additionally carries the convicting profile or codegen
evidence, the predictions written before the measurement, the paired
before/after, and its refutations reported plainly; a wrong-magnitude prediction
is a refutation, not a rounding error.

Contributions are made under a Contributor Assignment Agreement — see
[docs/CONTRIBUTING.md](docs/CONTRIBUTING.md). Suspected vulnerabilities follow
[docs/SECURITY.md](docs/SECURITY.md).

## Where the docs are

| Document | What's in it |
|---|---|
| [docs/SPEC.md](docs/SPEC.md) | normative: the type wire, grammar, every edge case |
| [docs/SPEC-TABLES.md](docs/SPEC-TABLES.md) | normative: the table wire, the cook, the block form |
| [docs/TUTORIAL.md](docs/TUTORIAL.md) | empty directory to every feature, in fourteen parts |
| [docs/USAGE.md](docs/USAGE.md) | every language feature with the code it generates |
| [docs/PORTING.md](docs/PORTING.md) | the techniques register: a cell per language, with its gate |
| [ROADMAP.md](ROADMAP.md) | where every feature stands, language by language |
| [docs/MAINTAINERS.md](docs/MAINTAINERS.md) | the working session's context: the ledger, the performance program, what `make` proves in full |

Nothing in `notes/` is normative. Open questions are a numbered row in SPEC §9,
never an inline aside, and §9 rows keep their numbers forever because code and
corpus cite them.

## When the compiler refuses

The break-the-language diagnostics suite is 70-plus refusal cases, and a
diagnostic is meant to say what the input WANTS, not only what was wrong. Do
what it says rather than working around it. If it does not say enough, that is a
bug in the diagnostic: open an issue with the schema text, the command and the
exact sentence it printed.
