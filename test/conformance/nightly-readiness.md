# Original-nine nightly readiness

Run `test/conformance/nightly-readiness <lang>` for exactly one of
`cpp c cs go rust java js dart elixir`. The nightly workflow must preserve
stdout and `build/nightly-readiness/<lang>/`, and write a passing readiness
receipt only after exit zero. Every phase is attempted; one failed phase keeps
the final result incomplete. A green conformance report or a manually completed
roadmap cell cannot substitute for an execution here.

This gate composes with the workflow's required canonical coverage, fixed-form,
BOM-entry, hostile-corpus and generated-freshness gates. The workflow writes the
canonical reference corpus once and distributes it to the ports. This script
does not add an all-pairs matrix or ask the reference to read each writer.

## Executed acceptance

| Phase | Required execution and evidence |
| --- | --- |
| Runner contracts | C++ executes the permanent Python regression suite once. Its phase and nonzero test count are mandatory; no-op invocation cannot stand in for execution. |
| Feature support | Build the current harness, then `implementation --lang <lang>`. Require exit zero and exactly one matching `IMPLEMENTATION_PREREQUISITES_PASS` line with positive construct and active-witness counts. The harness checks the compiler's mandatory construct registry, valid active instances and code generation independently of the 929-row claim manifest. This is a prerequisite, not a replacement for native execution. |
| Native compiler suites | `SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 go test -json -timeout=0 -count=1 -parallel=2 ./internal/codegen/<packet> ./internal/codegen/<table>`, executed as separate package phases. C++ also executes `./compiler` once. Each package's discovered tests/examples/fuzz seed tests must finish, at least one must pass, and the package must pass. The small internal/lockfile suite also runs to certify the replacements for retired layout-validation tests. |
| Packets | Execute the normal and ludicrous packet programs below, plus defaults, UTF-8, wide-text and their existing negative controls. Run the finite-value targets where registered. C++ executes its own wide, guard and random packet binaries and four wide/text controls. |
| External table executables | The existing fixed-form/paired/native targets listed below complement the package tests. Missing targets, missing corpora, failed builds or nonzero programs fail readiness. |
| Wire and retaining load | Build the existing all-roots driver. Run the shared differential `wire-fuzz` directly for plain and `--retain`, each at seeds 1, 2 and 3 with 100000 random mutants plus the complete enumerated pass. No `--finite-owed`, replay-only replacement, caller seed/count reduction or custom roster is accepted. |
| Wire/retention controls | Execute the existing planted wire controls and retention controls. C, Go and Rust's existing retention overlays are reproduced with only the outer Go-suite timeout changed to zero; the exact intended test failure and diagnostic remain mandatory. |

The packet/table package pairs are:

| Language | Packet package | Table package | Packet execution |
| --- | --- | --- | --- |
| C++ | cpp | cpptable | schema_test, guard, wide, random, ludicrous |
| C | c | ctable | normal, ASan, ludicrous, bench |
| C# | csharp | cstable | dotnet Debug and Release, normal and ludicrous |
| Go | golang | gotable | go run, normal and ludicrous |
| Rust | rust | rusttable | cargo Debug and Release, normal and ludicrous |
| Java | java | javatable | with and without -ea, normal and ludicrous |
| JavaScript | js | jstable | checked and NODE_ENV=production, normal and ludicrous |
| Dart | dart | darttable | assertions and compiled executable, normal and ludicrous |
| Elixir | elixir | elixirtable | normal and ludicrous |

External table targets are `tables-maps/lists/arms/retain/view/doctags/flat-wire/
fixed-matched/fixed-twin` for C++; `tables-c-fixedform/variable/usage`;
`tables-cs-leg` (Debug and Release); `tables-go-fixed-form`;
`tables-rust-fixedform/fixed-matched`; `tables-java-fixedform/fixedform-bench`;
`tables-js-fixed-form/fixed-matched`; `tables-dart-fixed-form`; and
`tables-elixir-fixed-form/fixed-bench`. Versioning and fixed-roadmap Go tests
already run in their complete packages, without the legacy filtered targets'
600-second or 20-minute suite limits.

Wire controls are the existing `tables[-<lang>]-wire-fuzz-negative-control`
targets, with C#'s pack control and Elixir's refusal control. Retention controls
are C++'s retaining differential pair; C's dropped retained field; C#'s
`tables-cs-retain-negative-control`; Go's file-count floor and unknown-node
controls; Rust's capacity-check overlay; Java's declared-default and node-record
controls; JavaScript's cards upgrade control alongside the full native RT1
suite; and Dart/Elixir's retained-node controls. The named controls' enumerated
runs may use N=0; they never stand in for the six positive release runs.

## Failure and timing rules

- Native JSON events, not a package's exit code alone, identify executed tests.
  Empty lists, missing terminal events, failing tests and skips are incomplete.
- The three named Go test retirements and 49 exact native-program retirement
  messages have explicit replacement test requirements in the script. Every
  replacement must actually pass in the current run, including the lockfile
  validation rules; a historical receipt is not acceptance. Retirements are
  printed as retired and are never counted as passed. Live owed rulings,
  UNPORTED rows, unsupported shapes and absent toolchains/corpora fail.
  Four old native controls still lack an established equivalent: C's
  fixed_fx2_bytes_row_control, Dart's fuCase, and Rust's two compiled
  text-under-an-arm halves. Those markers are not allowed exceptions.
- Each strict fuzz summary must name the requested arm/seed, nonempty roots and
  seeds, a nonempty enumerated pass, exactly 100000 random mutants, consistent
  totals and zero divergences. An absent seed or finite exemption is incomplete.
  A fixed root outside the retaining arm is the harness's normative exclusion,
  not a missing codec.
- No process wrapper here has an elapsed-time limit. Go suites use
  `-timeout=0`; finite corpus sizes and existing protocol/negative-control
  deadlines retain their meanings. Exactly two outer Go calls in the C++
  external targets (UnitView listing and fixed-twin) run through a private
  Makefile copy whose only changes are `-timeout=0`/uncached execution.
  The Go wide-packet recipe has the same narrowly copied outer invocation,
  preserving its native build, differential and negative-control dependencies.
  Exact source anchors and before/after hashes are recorded. An anchor change
  fails closed. Other package-only legacy targets are replaced by the full
  uncapped package runs. No global Go wrapper rewrites test flags, and no
  recursive make dry-run is treated as acceptance.
- Tool paths may come from the ordinary workflow environment. The acceptance
  language, phases, seeds, counts, package test selection and pass criteria
  have no override. GOFLAGS is fixed to build concurrency only; SCHEMA_SLOW and
  SCHEMA_REQUIRE_CORPUS are enforced. Inherited MAKEFLAGS, MFLAGS,
  GNUMAKEFLAGS, MAKEOVERRIDES, MAKEFILES, SCHEMA_SKIP_LEGS and all
  SCHEMA_UPDATE_* switches are cleared.
  Actual packet programs must print their unique existing success marker.
  A separate phase contract rejects missing/duplicate execution, and only a
  fully successful run emits `NIGHTLY_READINESS_PASS language=<lang> phases=<n>`.
- Concurrency is two build jobs/two Go test workers. Phases run serially.
  Logs, exact argv and per-phase statuses remain available even on failure.

## Known incomplete acceptance exposed by this entrypoint

This is intentionally not a retrospective claim that today's nine ports are
complete. Existing defaults let Java run no random mutants; Java, Dart and
several other release targets permit finite-value exemptions; JavaScript and
Elixir drivers acknowledge absent roots. Some native packages have live owed
or unsupported skips. Those conditions now fail this acceptance gate.

The shared wire roster also contains a known variable-root/message-form
mismatch (for example blobdemo.Catalog). This script does not silently delete
that input or relabel absence as supported. The roster/driver owners must
resolve the valid-form domain explicitly. There is no intentionally unmapped
execution entrypoint in this plan; future recipe drift fails for review.

The implementation prerequisite is currently expected to expose missing carried
constructs and/or active corpus witnesses. A successful registry declaration
alone cannot satisfy the remaining native, all-roots and control phases.
