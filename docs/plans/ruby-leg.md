# A Ruby leg: the layers, bottom up

The request to support Ruby, recorded 2026-11-10, starts this plan. This page
sets out the layers of a new Ruby target, from the bottom up. Each layer has
what it guarantees, how that is checked, and what it depends on. It also lists
the decisions that need the owner before anything is built. The house rule is
that the layers of a new system are settled with the owner before building,
and each one is secured before anything above it starts. So this page is for
correcting first, and building second. The roadmap work-set is `port/ruby` in
`docs/roadmap.sexp`, one task per layer.

The leg is modeled on how the Python leg was brought to `main`: one design
page, one pinned toolchain built from source, a serialize-family sibling, then
the skeleton, the packet wire, the table forms, the conformance surfaces, the
analyzers and the documentation, each secured before the next begins.

It joins the registries the way docs/CONTRIBUTING.md "Adding a language"
describes: one file or one directory per language, discovered rather than
listed, and only the three tolerated shared edits.

It is built to the one-read contract from the first line:

- **One read.** Every read returns the whole value or refuses with no partial
  value.
- **The verdict is the cross-language contract.** A value is shown equal by
  writing it back byte-identical; reasons, counters and sizes are Ruby's own
  diagnostics.
- **Writes are deterministic.** The binary and JSON writers produce one
  encoding per value.

Ruby never has a tolerant read, so it has nothing to cut later.

## What is on main today, and what happens to it

Nothing. There is no Ruby target, emitter, runtime, generated tree,
conformance driver, bench leg or toolchain pin on `main`. The leg starts clean
at R0, with no seed to remove and no hand-written codec to replace.

## The layers

| layer | what | guarantees | checked by | depends on | size | roadmap task |
|---|---|---|---|---|---|---|
| R0 | **The design page and the decisions.** `docs/PORT-RUBY.md`, in the shape of PORT-PYTHON: the type mapping, number model, text and bytes, runtime layout, harness, register and card map. The decisions D1-D8, answered by the owner | The design is fixed before any code, and nothing in the tree contradicts it | A cold read with no memory of the drafting, its findings applied; the owner's answers; `TestRubyDesignPage`; `make test` green | the owner's answers | S | `port/ruby/r0-design` |
| R1 | **The toolchain pin.** `make/ruby-toolchain.mk`: `dist-ruby` builds the pinned floor and current CRuby from their ruby-lang.org source tarballs into `dist/ruby-<ver>/`, refusing any other sha256 by name. `toolchain-ruby` probes each pin; `TOOLCHAIN_LEGS += ruby`; `TOOLCHAIN_PINS_ruby` lists the floor, the current interpreter and the analyzer lock. The analyzers (RuboCop and Steep with RBS) install offline from `make/ruby-analyzers.lock`, one sha256 per gem. A `.github/actions/ruby` step caches the build keyed by the pin | Every Ruby process the build starts is the pinned interpreter or the analyzer lock, and a missing pin refuses by name; no PATH lookup, no `RUBYOPT`, no user gem directory | `make toolchain-ruby`; the toolchain negative control (each pin pointed at a missing path in turn must refuse naming that pin); a grep gate showing no bare `ruby` in `make/ruby*.mk` or the leg's tests; `ruby -w` clean over the runtime | R0 | M | `port/ruby/r1-toolchain` |
| R2 | **serialize.rb, the bit layer.** A sibling repository that ports `serialize.h` to format 2.0 (STANDARD.md), vendors the standard and the corpus at COMPATIBILITY.md's commit, releases through the release tooling, and joins the COMPATIBILITY table. Until the repository exists it is staged in `third_party/serialize.rb/`, laid out as its own repository, and vendored into `internal/codegen/ruby/runtime/` by sha256 with `SERIALIZE_PIN` and `make/ruby-serialize.mk` | The bits are right before schema stands on them: every family vector writes its bytes and every refuse vector refuses, including finite-only floats and canonical encodings | The family corpus in serialize.rb's own CI; in schema, `TestVendoredRubySerializeIsThePin` (the files and sums match the pin, and one int and one float round-trip against the vendored corpus), as `TestVendoredLuaSerializeIsThePin` does | R1; decision D4 | M | `port/ruby/r2-serialize` |
| R3 | **The skeleton.** `schema new-leg ruby`: `compiler/target_ruby.go` behind `schema_leg_ruby`, `internal/codegen/ruby` (the type surface: values, constructors, descriptors, no codec), `internal/codegen/rubytable` (the table surface, no codec), the `rubyrun` test helper, `internal/tablenames/ruby.go`, `compiler/tablesruby_test.go`, `make/ruby.mk` `test-ruby`, `test/conformance/ruby/{driver,ci.json,main.rb}` answering every surface absent, the PORTING.md column (every technique ✅, — with the platform's reason, or ❌ citing its roadmap entry), and the `ruby` column on all three roadmap tables with `port/ruby/*` cells, all `:todo` | The leg is discovered by every registry with no shared file edited, and every construct the target does not carry is refused by name at compile time | The registry gate (`test/conformance/harness/registry_test.go`); `make test-ruby`; identifier tests: Ruby keywords, core-constant and runtime-name collisions, dunder names, invalid constant spellings, generated-member collisions, package-constant collisions, and case-folding file collisions each refused or mangled by rule; the PORTING column gate | R1 (R2 for the vendored runtime directory) | M | `port/ruby/r3-skeleton` |
| R4 | **The packet wire.** `ruby/read.rb` and `ruby/write.rb` over the vendored serialize.rb; the generated tree under `generated/ruby`; the ruby legs of `test/packet-text`, `packet-wide`, `packet-defaults` and `packet-finite`; the ludicrous goldens; the analyzer gate over generated code; the packet bench leg, measured with YJIT on and off and against the C extension option of D1. The build tag is dropped and the target joins the builtins | Every packet type reads and writes the 14 C++-pinned goldens byte for byte, truncation and hostile values refuse, and writes refuse out-of-contract values (out-of-range integers, non-finite floats, a float32 that overflows, invalid UTF-8, text where bytes belong) | `TestRubyTypeWireReadsTheGoldens` and `...WritesTheGoldens`, each with a negative control; each packet gate with its control; the analyzer gate with its control; the packet-wire column at 100%; the YJIT-on and YJIT-off bench numbers | R2, R3 | L | `port/ruby/r4-packet` |
| R5 | **Fixed tables, form 3.** The fixed plan, lineage and plan selection by hash, the bounds pass, file save and load, measurement. Built to FIXED-FORM-ALGORITHM as the strict-only spec states it: the bounds pass and forged ordinals refuse `out_of_range`, a bool byte past 1 is `wire_damaged`, and there is no `malformed` flag | Ruby's fixed bytes equal C++'s for every row, and each side loads and re-saves the other's bytes byte-equal; every refusal the algorithm names refuses | The per-row fixed proof suite against a live C++ reference (the shared suite of remaining-legs-100.md X1), file and writer negative controls per row, `fixedversioning_ruby_test.go`; the fixed-tables column | R4; the cut's spec layer ruled | L | `port/ruby/r5-fixed` |
| R6 | **Variable tables: the one read.** Form 1 load and save, a port of `internal/tablewire`: the variable class and node table, optionals, keyed arrays, maps, blobs, unions and any-type arms, wide and 128-bit kinds, the id-table wire, unbounded arrays, wstring, defaults and renames, widening, and the report (`unknown`, `widened`, `retained`, `retain_lost`) on success only | Every manifest input is either read, with its value written back byte-identical to the canonical wire, or refused; a refusal leaves no partial value and does not touch the caller's objects | The read-then-write harness against the Go engine on the tables manifest; per-construct tests with negative controls; the `carries` tests (`target_ruby_carries_*_test.go`) | R5; **`strict-only/engine` and `strict-only/harness`** | L | `port/ruby/r6-variable` |
| R7 | **The message form and retain-unknown.** Form 2: the announcement, `load_messages`, `measure_messages` and `save_messages` (fixed tables only). Retention on load and save | The batch reads in phase order or refuses whole; retained unknowns are saved back byte-exact | The message and retain-save surfaces; the retain negative control | R6 | M | `port/ruby/r7-message-retain` |
| R8 | **The text forms.** JSON both ways, the hand-editable text and base64 text. The reader is a port of the engine's (`internal/tabletext`), not the `json` library (D7). The writer follows the deterministic-JSON rules | Every readable JSON input is written back to the canonical text byte-identical, and every input the dialect refuses is refused (duplicate keys, lone surrogates, non-finite numbers) | The json-read, json-write and json-hostile surfaces on read-then-write; the JSONTestSuite policy rows; a negative control on the field-key match | R6; the deterministic-JSON spec landed | L | `port/ruby/r8-text` |
| R9 | **Accelerators and reflection.** Block read and build, cook open, check and write (a Ruby `String` has no address, so `base_offset` is an argument, as in Lua), UnitView, descriptors, doc comments and tags | The open/refuse verdicts and the canonical dumps match the corpus, and cook bytes in both byte orders match `schema cook` | The block, block-foreign, block-dump, forgery, cook, cook-write, cook-foreign and cook-forgery surfaces; descriptor tests with controls | R6 (descriptors after R3) | M | `port/ruby/r9-accel` |
| R10 | **The conformance driver.** `test/conformance/ruby/main.rb` answering every surface the read-then-write harness asks, and `ci.json` with the `ruby` field | Ruby answers the full manifest with no absent case except where a named platform reason stands | The harness; `conformance-negative-control-ruby` registered in `make/negative-controls.json` | R6 to R9; `strict-only/harness` | M | `port/ruby/r10-conformance` |
| R11 | **The wire-fuzz differential.** `test/conformance/ruby/wirefuzz.rb`, one process over little-endian frames on standard input, plain and builder arms, against the Go engine on the verdict pipe | Zero divergences over the full roster: the same verdict, and on accept the same written-back bytes | `make tables-ruby-wire-fuzz`; a planted divergence that goes red | R10 | M | `port/ruby/r11-wirefuzz` |
| R12 | **The hostile corpus runner.** `internal/ci/schema_ruby_runs_hostile_corpus_test.go`, `make/schema-ruby-runs-hostile-corpus.mk` and its workflow | Every shared hostile row is accepted with its canonical bytes or refused, and no input crashes or hangs the leg | The runner and its control; resource-bound runs (`schema-ruby-resource-bounds`) | R6, R8 | S | `port/ruby/r12-hostile` |
| R13 | **Cards.** `tables/cards/ruby`: the facade and driver over the Card unit | The card record reads, edits and saves like every other leg | `make cards-ruby-conformance` on all nine surfaces with its controls (PORTING J6) | R10 | S | `port/ruby/r13-cards` |
| R14 | **Lint and negative controls.** `ruby -w` with no warning, RuboCop and Steep in strict mode over all generated packet and table output and the runtime, and every control registered and failing exactly its rows | No generated file has a warning or an analyzer finding, and no gate passes vacuously | `schema-ruby-generated-strict-gate`; `go test ./tools/negativecontrols` | R4 onward | M | `port/ruby/r14-lint-controls` |
| R15 | **CI and the release tier.** The ci-full and certify.yml steps keyed on `ci.json`'s `ruby` field, the nightly fuzz and resource-bounds workflows, the nightly-readiness entry, the bench legs (`bench/ruby`, `bench/tables/ruby`) and `tables-ruby-release` | Every gate above runs on every pull request or nightly, with no step running a system interpreter | `go test ./internal/ci`; the nightly-readiness acceptance shape; a full nightly receipt in `docs/validation/` | R10 to R14 | M | `port/ruby/r15-ci` |
| R16 | **Docs.** USAGE.md ruby fragments run as tests, the per-language prose in SPEC.md and SPEC-TABLES.md, the README language list and the register column filled cell by cell | A stranger can use the leg from the docs alone, and every example in them runs | The USAGE fragment gate; a cold read of PORT-RUBY and the USAGE section | R15 | S | `port/ruby/r16-docs` |

**Models.** The leg implements state machines the house has already modeled
(framing, read order, card versioning). It adds none of its own, so it needs no
new TLA+ module. The exception would be serialize.rb hand-rolling a fixed word
buffer with a flush (index arithmetic over a reused scratch). In that case R2
carries a layout model with a reversed witness, as the layout rule requires. A
Ruby `Integer` accumulator with no fixed width needs none.

**Tests.** `rubyrun` runs the pinned interpreter in `t.TempDir()`: inputs go in
as files, answers come out on stdout as hex lines, with no real time, no
sockets and no network, and every test calls `t.Parallel()`. Anything timed
(the bench, a soak) is a functional test. A test that runs the interpreter is
behind `SCHEMA_SLOW`, as the DONE-WHEN sets it.

**Order.** R0 waits only on the owner's answers. R1 and R2 follow, then R3.
R4 can proceed once the packet surface is settled. R5 waits for the cut's spec
layer, and R6 and everything above it wait for `strict-only/engine` and
`strict-only/harness`. Ruby then joins the cut's leg order as a new column,
with nothing to delete.

## Decisions for the owner

The same eight decisions as THE DECISIONS in `docs/PORT-RUBY.md`. Each is
marked **recommended, awaiting the ruling**; nothing is built on an unruled
decision.

**D1. What runs.** The house rule is the fastest correct implementation in
each language.

- (a) Pure Ruby, standard library only: generated per-type code over
  serialize.rb. One implementation, no build step for users, and the same
  source runs on every covered CRuby.
- (b) (a), plus the same source compiled into an optional C extension. One
  source, but a compiler for users and a binary per platform.
- (c) A hand-written or generated C extension, or the C leg's codecs reached
  through a foreign-function interface. Fastest, but it is a second
  implementation in C that has to agree with the first, and the call overhead
  per field cancels much of the win.

**Recommendation: (a), with (b) as a named follow-on that ships only if it
measures a real win.** Within (a), fastest means measured: in R4, emit two
shapes for one type (straight-line generated code with locals bound, and a
descriptor walk) and keep the faster, with YJIT on and off. The bench will say
exactly how far from the C leg it stands. **recommended, awaiting the ruling**

**D2. Which Ruby.**

- (a) One current CRuby, with no floor.
- (b) A floor and a current pin: the floor is the oldest CRuby series still in
  normal maintenance at pin time, and the current pin is the newest stable
  release at pin time.
- (c) A different implementation, such as JRuby or TruffleRuby.

**Recommendation: (b).** The generated code and runtime use nothing newer
than the floor, and the leg runs on both, as the Lua and Python version
differentials do. Both releases are confirmed on ruby-lang.org on the day they
are pinned, never recalled from an earlier reading. **recommended, awaiting
the ruling**

**D3. The toolchain pin.**

- (a) The ruby-lang.org source tarball, by sha256, `./configure && make` into
  `dist/`, cached in CI by the pin. This is the Python and Lua precedent.
- (b) A third party's prebuilt archives. Fast, but a third party's build.
- (c) The interpreter found on `PATH`. The house rule refuses it.

**Recommendation: (a).** Build from pinned source. The analyzers (RuboCop and
Steep with RBS) install offline from a hash-locked list into the pinned
interpreter; they add no second runtime and both run over the same generated
tree. **recommended, awaiting the ruling**

**D4. A serialize-family sibling, serialize.rb.** Every leg stands on its own
`serialize.<lang>`:

- each repository is listed in serialize's COMPATIBILITY.md at format 2.0;
- the older legs pin theirs by tag, and the newer legs vendor theirs by
  sha256.

Options:

- (a) A new public repository, `mas-bandwidth/serialize.rb`, ported against
  the family corpus and released before schema uses it.
- (b) No sibling: the schema runtime carries its own bit reader and writer.

**Recommendation: (a).** It is the family pattern. It secures the bit layer
under its own outside judge (the family corpus) before schema is built on top,
and the bit layer is useful to Ruby users without schema. Until the repository
exists it is staged in `third_party/serialize.rb/`. Creating the public
repository is a separate, later decision. **recommended, awaiting the ruling**

**D5. Packaging.**

- (a) Generated code is emitted with the runtime beside it in the output
  namespace (relative requires, serialize.rb vendored inside), as every other
  leg emits its runtime. Nothing to install.
- (b) A gem that generated code imports.
- (c) Both.

**Recommendation: (a) for the leg.** Publishing a gem (the runtime, or
serialize.rb) is a later decision, and the owner's: the accounts and tokens
are the owner's, never delegated. **recommended, awaiting the ruling**

**D6. Errors and the strict read.**

- (a) `T.load(data) -> T` raises the unit's `Refused` (a `StandardError`
  subclass) carrying `reason`, a diagnostic string that is never compared
  across languages. On success, an optional report argument receives
  `unknown`, `widened`, `retained` and `retain_lost`. Writers raise the unit's
  `InvalidValue` (a `StandardError` subclass) for an out-of-contract value. A
  refusal carries no value and does not touch the caller's objects.
- (b) Return a `(value, error)` pair.
- (c) Return `nil` and fill a report.

**Recommendation: (a).** Exceptions are the Ruby idiom, and the shape leaves
no place for a partial value. The value model that goes with it: generated
classes whose fields are methods; `Integer` everywhere, with range checks on
write (128-bit is native); `Float` with float32 fields packed through `pack`
and checked; enums as `Integer` constants on a module with a frozen name
table, not an object per variant; UTF-8 `String` for text, validated strictly;
ASCII-8BIT `String` for bytes. **recommended, awaiting the ruling**

**D7. The JSON reader.**

- (a) Port the engine's reader.
- (b) Use Ruby's `json` library with hooks. Its dialect differs from the
  spec's: it accepts `NaN` and `Infinity`, keeps the last of duplicate keys,
  and decodes lone-surrogate escapes. Hooks can catch the first two but not
  the third.

**Recommendation: (a).** The JSON writer is the deterministic-JSON algorithm
in any case. Ruby's own Float formatting should be checked against it, not
assumed to match it. **recommended, awaiting the ruling**

**D8. Ruby and the v3.0.0 gate.**

- (a) Ruby joins that gate, so 3.0.0 waits for R16.
- (b) Ruby is a post-3.0.0 port, held to the same bar but not blocking the
  release.

**Recommendation: (b).** The other legs are mostly acceptance work away from
100%, and Ruby is the full stack. **recommended, awaiting the ruling**
