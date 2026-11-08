# C# leg — guard audit, 2026-11-05 (card sca-cs.w1, report-only)

Scope audited at base `a007c180ba4b09c7d9d5404de6e072043e034777`: the C#
packet generator (`internal/codegen/csharp/**`), the C# table generator
(`internal/codegen/cstable/**`), the C# claimed-name registry
(`internal/tablenames/cs.go`), the emitted readers/writers under
`generated/cs/**` and `testdata/golden/cs/**`, the table goldens under
`testdata/golden/tables/examples-cs/**`, the hostile-corpus harness
(`test/cs-hostile/**`, `make/cs.mk`, `make/schema-cs-runs-hostile-corpus.mk`,
`internal/ci/hostilecorpus/corpus/**`) and the conformance leg
(`test/conformance/cs/**`). The `Serialize.cs` runtime itself is out of scope
(own card); only generated code and how it calls the runtime are audited here.
The cooked/block forms are the trusted class (build-version and integrity
gated, docs/SECURITY.md); their `Open` checks are still listed because they
parse caller-supplied bytes.

Method (report-only ruling): code reading only. No reproductions, no new
hostile inputs, no fuzz corpora were created. Every finding names the test,
corpus row or model that *would* pin a fix; none was created. No generator
change was made, so no fix carries red/green evidence and every finding stays
open. There is no sibling audit file at this base (`docs/security/` did not
exist); this file follows the entry-table-plus-findings shape the card
specifies and cites `file:line` throughout so every row is grep-verifiable.

Verdicts: **bounded** = the cited check refuses before the hostile value can
drive a copy, allocation, index or publish; **open** = a finding below owns
the row. Counts: 158 generated packet `Read*` functions in
`generated/cs` + `testdata/golden/cs` (`grep -c "public static bool Read"`)
and 180 table read-family functions in the `examples-cs` goldens alone; the
table below covers every *distinct* entry-point shape that emits them, with
the generator line that emits the shape and one generated instance.

## Entry-point table

| # | Generated function (instance) | Untrusted input | Bound relied on (`file:line`) | Verdict |
|---|---|---|---|---|
| E1 | Packet `Read<T>(ReadStream, ref T)` per struct, e.g. `generated/cs/Clauses.cs:206` `ReadW13` | Packet bytes via `ReadStream` | Per-field emission `internal/codegen/csharp/functions.go:315` (`emitReadItems`), scalar dispatch `functions.go:1035`; every runtime call is early-out checked (`functions.go:467`) | bounded |
| E2 | Packet `Read<T>` per union + arm dispatch, e.g. `generated/cs/ArmDefaults.cs:253` | Tag byte + arm payload bytes | Tag read then `switch` with construction-default init per arm, `functions.go:445` and `functions.go:450` | bounded |
| E3 | Packet flat read runs (fused bit chunks) | Packet bytes | One `SerializeBits64` chunk read per chunk + single sticky-error test before any store, `internal/codegen/csharp/flat.go:450`; only vacuous-range pieces join a run (`flat.go:441`) | bounded |
| E4 | Counted-array element loop in `Read<T>` | Wire count | `SerializeInt(ref count, min, bound)` refusal precedes the loop, `functions.go:1021`; loop is `i < count`, `functions.go:1023` | bounded |
| E5 | Fixed-array element loop in `Read<T>` | Element bytes | Loop bound is the schema bound, never storage `Length`, `functions.go:1026` | bounded |
| E6 | `string(N)`/`bytes(N)` read in `Read<T>` | Wire length + payload | Ranged length read `functions.go:1167` is checked *before* the `AsSpan(0, length)` slice, `functions.go:1170` via `values.go:190`; length is in `[0, N]` on success | bounded |
| E7 | Bulk `[N]uint8` read | Payload bytes | `SerializeBytes(AsSpan(0, N))` with compile-time `N`, `functions.go:1015` | bounded |
| E8 | `string(N)` content validation (UTF-8 + interior NUL) | Decoded bytes in caller buffer | In-place UTF-8 state machine rejects overlong/surrogate/past-`U+10FFFF`, `internal/codegen/csharp/utf8.go:14`–`utf8.go:21`; NUL scan `functions.go:1177` | bounded |
| E9 | `wstring(N)` read | Wire length + 32-bit groups | Length ranged at `internal/codegen/csharp/wstring.go:26`; each group refused if `0` or `> 0xFFFF` (`wstring.go:31`) and surrogate pairing enforced (`wstring.go:34`–`wstring.go:36`) | bounded |
| E10 | Ranged integer / fixed / compressed-float reads | Encoded offset bits | Runtime ranged calls reject, never clamp (`functions.go:1100`, `functions.go:1109`, `functions.go:1064`); full-range unsigned path has a generated `> diff` refusal, `functions.go:1128` | bounded |
| E11 | Enum / flags reads | Wire ordinal / mask bits | Enum via `SerializeInt(ref v, 0, Max)`, `functions.go:1185`; flags via width-masked temp, `functions.go:1238` | bounded |
| E12 | `const` / `reserved` items on packet read | Wire bits | Generated equality / nonzero refusals, `functions.go:531` and flat form `flat.go:514` | bounded |
| E13 | Packet `Write<T>` / `Write<T>Batch` (caller-error side) | Caller values (not wire) | Write-side range/count contracts are `Debug.Assert` only and compile out in Release, `internal/codegen/csharp/csharp.go:147`–`csharp.go:153`; reads (E1–E12) re-check everything in every build | bounded (by design; see F-CS-06 note) |
| E14 | Table file ` <T>Load / <T>LoadVerdict`, e.g. `testdata/golden/tables/examples-cs/GuardedTable.cs:79` | Table-wire file bytes | Delegates to the descriptor walk `TableWire.Load`, emitted at `internal/codegen/cstable/wire.go:352`; framing damage verdict is `Damaged` and discards the load (docs/SECURITY.md) | bounded |
| E15 | Table `<T>LoadMeasure` (file form), e.g. `GuardedTable.cs:59` | Table-wire file bytes | Exact extent before any allocation: id-table cap 4096 and count-vs-length refusals, `internal/codegen/cstable/measure.go:119`–`measure.go:125`; per-field count caps `measure.go:28`–`measure.go:31` | bounded (fragile coupling: F-CS-01) |
| E16 | Native region `<T>Load(IntPtr, capacity, bytes)` + `<T>LoadBuilder` | Wire bytes + caller region/capacity | Emitted at `internal/codegen/cstable/region.go:33`–`region.go:34` over `LoadMeasureParts` (`region.go:659`); capacity is the caller's contract and the measure (E15) sizes it before the load | bounded |
| E17 | Retaining load `<T>LoadRetain` | Wire bytes from a newer build | Emitted at `region.go:37`–`region.go:38`; retention capacity refusals are generated at `internal/codegen/cstable/retain.go:83` | bounded |
| E18 | Message batch `<T>LoadMessages`, e.g. `testdata/golden/tables/examples-cs/KeyedTable.cs:259` | Message-wire bytes + vocabulary | Batch cap: `total > values.Length` refuses `batch_too_large`, `internal/codegen/cstable/messageread.go:278`; entry point emitted at `internal/codegen/cstable/message.go:85` | bounded |
| E19 | `AnnounceRead` (vocabulary announcement) | Announcement bytes | `MaxBytes`/`MaxEntries` refusals `vocabulary_too_large`, `message.go:224` and `message.go:242`; emitted at `message.go:13` | bounded |
| E20 | JSON text `<T>FromJson`, e.g. `GuardedTable.cs:134` | JSON text bytes | Generic walk `TableJson.Read`, entry `internal/codegen/cstable/json.go:1994`; recursion capped by `MaxDepth = 128` on read (`json.go:1896`), skip (`json.go:1279`) and write (`json.go:811`); key/number token caps `json.go:80`–`json.go:84` | bounded |
| E21 | Fixed-form `<T>FixedLoad` | Fixed-wire bytes + caller plan span | Emitted at `internal/codegen/cstable/fixedform.go:2223` (batch) and `fixedform.go:2226` (single); writer-side bounds are precomputed per lineage at `fixedbounds.go:253` | bounded |
| E22 | Cooked/block `Open` (trusted class) | Cook file bytes at a build version | Magic, byte-order, build-version, layout and count refusals before any deref, e.g. `internal/codegen/cstable/block.go:630`–`block.go:660` and `cook.go:649`–`cook.go:668` | bounded (trusted class) |
| E23 | Views: `UnitView()` + descriptors | No input (static generated data) | Factory emitted at `internal/codegen/cstable/view.go:151`; values are schema-derived literals, quoted at `view.go:54` | bounded |
| E24 | Hostile-corpus dispatch: `wire` / `text` / `message` / `announce*` carriers | Corpus payload bytes | Runner calls the public readers above per carrier, `test/cs-hostile/gen/main.go:264`–`main.go:308`; region arm refuses to allocate past the oversized guard, `test/cs-hostile/Runner.cs:86` | bounded (harness gaps: F-CS-02/03/04/08) |
| E25 | Compiler fuzz entry points (`FuzzPipeline`, `FuzzGeneratedCompiles`, `FuzzTableFrontEnd`) | Schema text (build-time input) | `internal/fuzz/fuzz_test.go:174`, `internal/fuzz/compile_test.go:150`, `internal/fuzz/tablefrontend_test.go:88`; schema input is the low-priority build-time surface (docs/SECURITY.md) | bounded |
| E26 | Conformance drivers (`test/conformance/cs/**`) | Pinned conformance vectors | Per-case drivers call the same public readers; a case with no C# answer is reported absent, never passed (`make/cs.mk`, `conformance-cs-absent` gate) | bounded |

## Findings (all open; report-only — no fix attempted)

### F-CS-01 — `Enum.Parse` on a runtime reason string in generated `LoadMeasure`
- Where: `internal/codegen/cstable/wire.go:341` and `internal/codegen/cstable/region.go:36`.
- What untrusted input can reach/do: table-wire bytes select the runtime's
  refusal string (`count_over_length`, `count_over_extent_cap`,
  `unknown_form`, `integrity_mismatch` — `internal/codegen/cstable/measure.go:28`,
  `measure.go:115`, `measure.go:116`). The generated wrapper converts it with
  `Enum.Parse<TableRefuseReason>(why)`. Today the string set and the enum at
  `internal/codegen/cstable/cstable.go:256` agree, so the parse cannot fail;
  the coupling is by spelling only, so any future reason string without an
  enum member turns a hostile file into an `ArgumentException` escaping a
  read that promised a refusal verdict.
- Severity: **low** (no present failure; a latent exception-instead-of-refusal
  on the highest-priority surface if the two lists drift).
- Suggested reduction: map the closed string set with a generated `switch`
  that defaults to a generic refusal member instead of parsing by name.
- Pin that would fix it: a generator test generating a variable root,
  feeding a count-past-length file through the `out TableRefuseReason`
  overload, and asserting a refusal value rather than an exception; plus a
  drift test asserting every `reason = "..."` literal in the measure source
  names an enum member. Named only — not created under report-only.

### F-CS-02 — Corpus manifest text is interpolated into generated C# dispatch without escaping
- Where: `test/cs-hostile/gen/main.go:128` (case key written as a C# string
  literal) with table/schema substitution at `test/cs-hostile/gen/main.go:255`.
- What untrusted input can reach/do: a manifest field containing `"` or `\`
  breaks out of the string literal in the generated `Dispatch.cs`; the
  manifest is committed and review-gated, so this is code generation from a
  trusted file, but the corpus is exactly the file future hostile cases are
  added to by hand.
- Severity: **low** (build-time test tooling; no shipped reader affected).
- Suggested reduction: quote case keys with the same quoting the generator
  uses for string literals, and validate schema/table fields against the
  identifier grammar before substitution.
- Pin: a `gen` unit test with a quote-bearing manifest row asserting the
  emitted dispatch still compiles/parses as one case. Named only.

### F-CS-03 — Hostile runner resolves payload paths from the manifest without containment
- Where: `test/cs-hostile/Runner.cs:136` (`Path.Combine(corpus, input)`).
- What untrusted input can reach/do: an `input` field of `../…` reads a file
  outside the corpus directory and hands its bytes to a generated reader.
  The bytes still only reach bounds-checked readers, so the impact is
  reading an unintended local file as a corpus payload in a test run.
- Severity: **info** (test harness, local files only).
- Suggested reduction: refuse payload paths that escape the corpus root.
- Pin: a runner test with a `../` manifest row expecting a named error.
  Named only.

### F-CS-04 — Packet accept-path write buffer size can overflow `int` in the runner generator
- Where: `test/cs-hostile/gen/main.go:473` (`d.Length * 8`).
- What untrusted input can reach/do: a payload over ~256 MiB wraps the size
  negative; `Math.Max` then picks the 64 KiB floor and the writer refuses
  cleanly, so the case reports a writer refusal instead of a digest. No
  out-of-bounds access is reachable.
- Severity: **info** (wrong verdict text for oversized inputs, fail-closed).
- Suggested reduction: compute the buffer size in `long` and cap it.
- Pin: a dispatch unit test over a synthetic oversized length. Named only.

### F-CS-05 — Table reads clamp where packet reads refuse (divergence is spec'd, worth restating)
- Where: report counters emitted at `internal/codegen/cstable/cstable.go:679`–`cstable.go:695`
  (`Clamped` beside `Refused`/`Malformed`).
- What untrusted input can reach/do: an out-of-range scalar on the *table*
  wire is clamped and counted, while the same value on the *packet* wire is
  refused (E10). docs/SECURITY.md makes cross-language divergence a security
  bug, but this split is the table form's documented permissive contract
  (SPEC-TABLES report ledger), applied uniformly by the descriptor walk, so
  no language diverges from the *table* contract.
- Severity: **info** (verified design, recorded so the packet/table
  asymmetry is a decision on record, not an oversight).
- Suggested reduction: none. Keep the SPEC-TABLES wording and this row in
  sync if the table contract ever moves to refusal.
- Pin: the existing conformance/report surfaces already pin the counts;
  no new pin proposed.

### F-CS-06 — Generator emits identifiers with no escaping; all protection is checker-side
- Where: `internal/codegen/csharp/csharp.go:109`–`csharp.go:111` (`m()` is
  the identity), type names emitted verbatim (e.g. `csharp.go:410`), package
  as namespace verbatim (`csharp.go:174`).
- What untrusted input can reach/do: a hostile *schema* (the build-time
  surface) whose names collide with C# keywords, runtime types (`Schema`,
  `TableWire`, …) or each other produces uncompilable C# rather than a
  checker diagnostic *if* the checker misses the name. The bounds relied on
  are real today: identifiers are ASCII-only by construction
  (`internal/scanner/scanner.go:298`–`scanner.go:302`, so no Unicode
  identifier reaches the emitter), package reserved words are refused at
  `internal/check/check.go:296`, and generated/runtime names are claimed in
  `internal/tablenames/cs.go` and enforced by the checker's claim walk. The
  failure mode is therefore fail-closed at `dotnet build` (warnings as
  errors), never a silently misreading binary.
- Severity: **low** (defence-in-depth gap on a build-time input; worst case
  is uncompilable output).
- Suggested reduction: keep as is, or add one emitter-side assertion that
  every emitted identifier matches the claimed-name set. No escaping
  machinery (SPEC §4.6 forbids it).
- Pin: the existing claimed-name tests; a hostile-schema case naming a type
  `Schema` asserting a checker (not compiler) refusal would pin the
  boundary. Named only.

### F-CS-07 — Doc text quoted with Go escaping in C-family literals
- Where: `ir/doctags.go:40` (`QuoteDoc` is `strconv.Quote`), consumed for
  descriptor `Doc` strings (e.g. `internal/codegen/cstable/view.go:49`).
- What untrusted input can reach/do: schema doc text containing sequences
  Go and C# escape differently (notably `\x`, which C# reads with variable
  length) can change the *value* of a generated `Doc` string. Doc strings
  are data (reflection/documentation), never executed and never parsed as
  wire data.
- Severity: **info** (data fidelity only).
- Suggested reduction: none required; if exact doc fidelity is ever
  promised, spell a C-family quoter beside `QuoteDocElixir`.
- Pin: a golden with `\x`-bearing doc text. Named only.

### F-CS-08 — Shared hostile corpus carries no `packet` or `wire_retain` rows for any port's manifest slice
- Where: `internal/ci/hostilecorpus/corpus/manifest.tsv` (186 lines:
  168 `text`, 9 `wire`, 3 `message*`, 4 `announce*`, 0 `packet`,
  0 `wire_retain` at this base).
- What it means: the C# packet readers (E1–E12, the highest-priority
  surface in docs/SECURITY.md) are *not* exercised by the shared corpus;
  their hostile evidence is the conformance leg (E26) and packet-defaults
  gates instead. The runner already implements both missing carriers
  (`test/cs-hostile/gen/main.go:275` and `main.go:300`), so this is corpus
  content, not harness capability.
- Severity: **info** (coverage shape, recorded; adding rows is corpus work
  outside this report-only card).
- Suggested reduction: a future corpus card adds packet/wire_retain rows.
- Pin: those future rows themselves. Named only.

## Generator audit notes (no finding)

- File/path/command construction from schema input: output file names are
  `<Base>.cs` from the schema file base (`internal/codegen/csharp/csharp.go:71`,
  `internal/codegen/cstable/cstable.go:154`); the compiler CLI — not the
  generators in scope — owns directory creation. The hostile `gen` tool
  builds its compiler command from flags, with the schema path as a single
  argument (`test/cs-hostile/gen/main.go:176`), and sanitizes unit directory
  names (`main.go:153`). No shell string is built from schema text.
- String literals emitted from schema names use Go `%q` (e.g.
  `internal/codegen/csharp/csharp.go:297`); schema identifiers are
  ASCII-only (F-CS-06), so the Go/C# quoting overlap is exact for every
  reachable name.
- Doc comments reach generated code only as `//` line comments, one source
  line per comment line (`ir/doctags.go:22`), so doc text cannot break out
  of the comment.
- Write paths were read beside the read paths: every generated writer in
  scope either delegates range enforcement to the runtime's ranged calls or
  carries the Debug-only caller contract at E13; no writer weakens a read
  check, and no read path was found that trusts a writer-side assert.

## Model

`tla/CsReadReportIso.tla` models one generated C# read as the code behaves:
a cursor over a finite buffer, a damage/refusal condition that ends the
read, a destination that is only published whole on acceptance, a retained
partial destination on damage (the table report's documented behaviour),
and report counters that the next read starts from zero — the isolation
the shared `Ignored` report reset implements
(`internal/codegen/cstable/region.go:685`). `MCCsReadReportIso.cfg` is the
checked configuration; `MCCsReadReportIsoBroken.cfg` removes the cursor
bound check and must be rejected by TLC with a `CursorBounded`
counterexample. TLC execution is Linux-bench work and is recorded as
NOT RUN for this card (see the return evidence); no row was added to
`tla/RUNS.tsv`, because recording an unrun result would fabricate it.
