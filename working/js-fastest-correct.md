# The JavaScript throughput path — productization design

**STATUS: PROPOSAL — DO NOT MERGE AS LAW. The decision is Glenn's.** This document
designs how schema ships the measured winner of the JavaScript race. No code rides
this branch; the design stands on the race's evidence and names every decision with
its reason. The governing ruling, verbatim:

> Whichever correct implementation is fastest is the one we use for JavaScript.

*(2026-08-18. Evidence: the flattened-JS probe, 3 interleaved rounds, best-of,
golden-gated every run — artifacts in `~/rowan-working/scratch-flatjs/`; the wasm
boundary experiment — artifacts in `~/rowan-working/scratch-wasm-serialize/`; the
generated-JS baseline from era sixlang-air-1. All on the M2 Air, node 26.7. The
ruling is a measurement, so this design is era-stamped like any other measured
claim: a future engine that inverts the verdict re-opens the race, not the law.)*

---

## §0 The verdict that binds

Three contenders were raced at the generated altitude, all golden-gated before any
timing (serialize.c bench pins for mixed_packet; the 204-byte
`testdata/wire/real_packet.bin` golden plus full 64-variant cross-validation against
the generated JS + serialize.js runtime for real_packet — byte-identical writes,
field-identical reads, both directions; zero gate failures across the session):

| contender | mixed read (21 B) | real read (204 B) | real read ×-off-C++ |
|---|---|---|---|
| **flattened pure JS** (default flags) | 66.8 M/s (bimodal, floor 44.4) | **5.31 M/s** | **5.1x** |
| flattened pure JS (V8 inline-budget trio) | 74.0 M/s | 5.42 M/s | 5.0x |
| flattened pure JS, **loop-fused batch read** | **134.7 M/s** | — | — |
| wasm whole-packet (generated-C-in-wasm, 1 crossing/packet) | 76.9 M/s | unmeasured (no build) | — |
| generated JS + serialize.js runtime calls | 5.4 M/s | 0.63 M/s | 43x |

Writes: every JS write leg is vary-bound (the harness's data generation dominates);
vary-subtracted residuals put flattened real write at ~143 ns/packet (~7.0 M/s
pure-serialize) vs generated ~1424 ns (~0.70 M/s) — flattening is ~10x on write cost
too, and at parity with wasm on the mixed write leg.

**The winner is flattened pure JavaScript.** Per-call it is within 4% of wasm's best
read number (74.0 vs 76.9) and ahead of wasm's every other row; with the per-packet
call fused into a batch loop it is 1.75x FASTER than wasm (134.7 vs 76.9); against
the shipped generated-JS tier it is 8.4x on real-packet read and ~10x on write cost.
The flag-delta mystery is closed: flattening removes every per-field call, and the
one remaining cost under default flags is the per-packet call into a function that
exceeds V8's default inline budgets *by construction* — visible only on small
packets (~9 ns at 21 B), erased entirely by batch fusion, invisible at 204 B.

Under the ruling, that makes the flattening emitter THE JavaScript path. Everything
below is how schema ships it.

---

## §1 Artifact shape — a second emission surface in the js backend (Q1)

**DECISION: the flat tier is a second surface emitted by the existing
`internal/codegen/js` backend — pure generated JavaScript, no new backend, no new
language, no wasm.** One `schema generate --lang js` invocation emits both surfaces,
always:

```
Constants.schema  ->  Constants.js       (existing: classes + runtime-call Write/Read)
Wire.schema       ->  Wire.js
                      WireFlat.js        (new: the flat tier for Wire.schema's types)
```

Per schema file `Base.schema`, `BaseFlat.js` is emitted beside `Base.js` whenever the
file declares types, messages, or the message dispatch surface. The collision rule
mirrors the existing Table refusal (`js.go`): a unit containing both `X.schema` and
`XFlat.schema` is refused at generation with a rename instruction.

**What a flat module contains, per type and message `Name`:**

- `Write<Name>Flat(value, view)` → **bytes written** (`>= 0`), or `-1` on a checked-
  mode contract refusal. `value` is the SAME generated class instance the runtime
  tier uses (`Base.js`'s classes — one storage model, two codecs); `view` is a
  caller-owned `DataView`. The body is the probe's shape exactly: the two-lane
  scratch discipline of serialize.js `src/bitpacker.js` inlined at every field,
  compile-time-constant widths and masks, **zero function calls** — nested types
  inline into their parent rather than call (the defining property; a call would
  rebuild the budget wall the probe measured).
- `Read<Name>Flat(value, view, numBits)` → **bool**, the family's read verdict.
  Bounds checks fused one per straight-line run (`br + runBits > numBits`),
  headroom rejects kept per field wherever range < 2^bits−1, const/reserved
  verification, interior-null refusal on strings, untaken branch sides zeroed
  inline — C's release read semantics, the reader obligations of STANDARD.md in
  full. Wire trust boundary never comes off.
- `Write<Name>FlatArray(values, count, view)` / `Read<Name>FlatArray(values, count,
  view, numBits)` — **batch entry points, emitted for messages** (v1; see §8 for
  the extension lever). Back-to-back packets in one buffer, the whole loop inside
  one function: this is the shape that measured 134.7 M/s and beat wasm by 1.75x,
  and it cannot be recovered by a caller-side loop (the caller's loop pays the
  non-inlined per-packet call — the exact cost the probe isolated). Batch entries
  ride for messages only because messages are the packet-rate surface; nested
  types are already inlined inside them.
- `WriteMessageFlat(stream-less: message, view)` / `ReadMessageFlat(storage,
  message, view, numBits)` — the dispatch pair, tag framing identical to the
  runtime tier's (instanceof chain on write, tag-switch into `MessageStorage` on
  read, `null` = None terminator).
- `export const FLAT_READ_SLACK = 8;` and the allocation contract in the module
  header (see below). The `MaxBits`/`MaxBytes` constants stay in `Base.js` — one
  definition, imported.

**Coverage: the full §4/§5 wire construct set for types and messages.** The probe
proved the inline form for ranged/bare ints, bits, bools, floats, doubles, 64-bit,
flags, enums, compressed floats, fixed-point, and wire branches with zeroing; the
remaining constructs are mechanical in the same discipline: strings/bytes as a
length write plus an aligned bulk copy (`Uint8Array.set` / fused byte loop),
`align` as pad-write/pad-check, arrays as runtime loops over inlined element
bodies (bounded by schema constants, never unrolled — code size), 128-bit as four
32-bit lanes. A flat tier that covered a subset and fell back to runtime calls
per-field would reintroduce the exact wall the race was run to remove — coverage
is all-or-nothing per struct. **Objects (view/state families) and the table layer
stay on the runtime tier in v1** — tables are load-time, not 60Hz, and objects
were not in the race's corpus; extending flat to them is the same mechanical
emission when a measured need names it (§8).

**Allocation contract (the C++ stance, measured and stated by the probe):**

- Write: the buffer behind `view` must be at least `<Name>MaxBytes` — already
  rounded to the 8-byte write-buffer granularity, which covers the final
  whole-word flush exactly.
- Read: the buffer must extend **at least 8 bytes past the payload**
  (`FLAT_READ_SLACK`): 64-bit windows load unconditionally, the buffer contract
  STANDARD.md names accepted best practice. Receive paths that get exactly-sized
  buffers (every browser and node network API) copy into a persistent
  `MessageMaxBytes + 8` scratch first — one bulk copy per packet, the documented
  idiom (§6).

**Why always-emit rather than a flag:** the ruling makes flat THE JavaScript path —
it cannot be opt-in without shipping the slow path by default. Both surfaces are
deterministic to the byte, both are covered by regen-match, and the runtime tier
must be emitted anyway because it is the flat tier's CI oracle (§5). One generate
step, two surfaces, explicit import picks (§3).

**Emitter home:** `internal/codegen/js`, a sibling file set to `functions.go`
(`flat.go` + `flatops.go` or similar), sharing the ir walk, `GoExportName`, the
fold machinery, and the collision registry. The probe's `gen_flat.mjs` is the
reference for the op table and the two-lane merge/read kernels — its output is
pinned by the same goldens, so porting it into Go is a transliteration with a
byte-identical target, not a reinvention.

---

## §2 Toolchain — nothing new, and why that decides more than convenience (Q2)

**DECISION: schema's generate step gains zero dependencies.** The flat emitter is Go
code in the existing backend; the flat output is plain ES modules importing nothing
external (module-scoped `DataView` scratch, sibling generated files only). No
emscripten, no npm, no install step, no binary artifacts in the repo.

For the record, the wasm options that were on the table, and what each would have
cost had wasm won:

1. **emscripten as a schema build-time dependency** — a heavy, non-hermetic,
   version-pinned toolchain in every generate environment; wasm bytes in
   regen-match; per-host reproducibility burden. The worst fit for a compiler
   whose whole output discipline is "deterministic to the byte anywhere".
2. **Prebuilt runtime-core .wasm shipped in serialize.js, schema emits only a JS
   driver** — keeps generate light but forces the per-field boundary (measured:
   15.5x off C, the worst whole-packet-era row) because packet shapes are
   schema-defined and cannot live in a precompiled fixed core.
3. **schema emits C, the consumer compiles with emcc** — pushes the toolchain onto
   every downstream user; the generate step stays light by exporting the pain.

The race dissolved the question: the pure-JS winner beats the best wasm
configuration where it matters (read, and every batch shape) with none of these
costs, and additionally escapes wasm's structural risks — memory growth detaching
views, view re-fetch discipline, module instantiation lifecycle, CSP/bundler
friction with .wasm assets. The wasm artifacts stay archived in
`~/rowan-working/scratch-wasm-serialize/` as the measured alternative; nothing
ships from them.

---

## §3 The two-tier story — explicit import, no magic (Q3)

**DECISION: two named tiers over one storage model, selected per call site by
explicit import path. No environment variable, no auto-detection, no re-export
shim that swaps codecs behind a user's back.**

| | **flat tier** (THE JS path) | **runtime tier** (compat/debug/reference) |
|---|---|---|
| import | `./WireFlat.js` | `./Wire.js` + serialize.js streams |
| storage | the generated classes (shared) | the generated classes (shared) |
| speed | 8–10x the runtime tier, contests native C within 3.2–5.1x | the measured baseline |
| dependencies | none | serialize.js |
| diagnostics | bool / -1 verdicts only | sticky latched errors: WHICH operation failed and why |
| checked mode | load-time fork (§4) | serialize.js NODE_ENV fork, per-op validation |
| sizing | MaxBytes constants | MeasureStream (exact measure pass) |
| coverage | types, messages, dispatch | everything: + objects, tables |

Both tiers emit the same bytes for the same values — a standing CI gate, not a
promise (§5) — so mixing is legal per call site: debug a failing stream by
re-reading the same buffer through the runtime tier and reading the latched error.
That is the documented debugging story, and the reason the runtime tier is not
legacy: it is the reference surface (readable, stream-based, the direct §6.3
translation of the C++ shape), the oracle the flat tier is verified against, and
the only tier for objects and tables in v1.

USAGE.md teaches the flat tier as the default for production packet paths (the
ruling: fastest correct is the one we use), the runtime tier for development
diagnostics and the surfaces flat does not cover. The generated module headers
say the same thing in two sentences each.

---

## §4 The check model in the winner — the frozen fork (Q5)

The probe's semantics stance, now stated as the shipped design:

- **Read side: never configurable.** Reader obligations are format (STANDARD.md:
  "two implementations that disagree about refusal disagree about the format").
  The flat reader carries them in every mode, fused but complete: per-run bounds
  checks, headroom rejects wherever the range does not fill the width,
  const/reserved verification, interior-null refusal, branch zeroing. One reader
  body, emitted once.
- **Write side: the JavaScript #ifdef, exactly where the family already put it.**
  JS has no compiler to strip asserts, so serialize.js froze the fork at module
  load (`src/mode.js`: NODE_ENV read exactly once, whole variants selected at
  export, call sites monomorphic). The flat module does the same, itself — it has
  no stream object to inherit mode from:

  ```js
  const PRODUCTION = typeof process !== "undefined"
    && process.env && process.env.NODE_ENV === "production";
  export const WriteWirePacketFlat = PRODUCTION
    ? writeWirePacketFlatProduction   // trusted writer: zero caller validation;
                                      // width masks stay (wire arithmetic)
    : writeWirePacketFlatChecked;     // the generated fold-guard set: refuse
                                      // out-of-contract (-1), Number.isInteger
                                      // in the Number domain, asUintN reinterp
                                      // in the BigInt domain — byte-identical
                                      // wire for every in-contract value
  ```

  Both variants are emitted (deterministic, golden-pinned); the selection is
  frozen — the semantics of a compiled build, the family's established answer.
  Bundlers statically replace `process.env.NODE_ENV`, so browser production
  bundles tree-shake the checked writer out entirely: the "compile-out assert"
  the language lacks, recovered at bundle time for free.
- **The guards are the runtime tier's guards, verbatim in meaning.** The checked
  flat writer refuses exactly what the runtime-call generated writer refuses
  (same fold-guard set, same verdict discipline: refusal returns without
  latching — there is no latch here at all). CI holds the two tiers to identical
  accept/refuse verdicts on the same inputs (§5), so "checked flat" is not a
  third semantics — it is the family's checked write semantics at flat speed.

What is deliberately NOT in the flat tier: the sticky error latch and per-op
failure identification. That diagnostic surface costs the exact per-field call
structure flattening exists to remove; it lives one import away in the runtime
tier, over the same bytes (§3).

---

## §5 Wire identity forever — the golden legs run THROUGH the flat path (Q6)

The wire law: golden pins prove everything. The flat tier enters the same harness
that already holds six languages byte-identical, as a first-class leg:

1. **Golden legs through flat.** The node wire test (the `make` chain's js leg)
   writes every pinned corpus instance through the FLAT writer and byte-compares
   against the same C++-pinned wire goldens (`testdata/wire/`), and reads every
   golden back through the FLAT reader with field-exact comparison — both
   directions, beside the existing runtime-tier legs, every instance including
   examples128's fixed/128-bit pins.
2. **Cross-tier equivalence, standing.** The probe's 64-variant cross-validation
   becomes a permanent gate: randomized instances written through both tiers must
   be byte-identical; every buffer read through both tiers must be field-identical
   AND verdict-identical. This is the gate that catches a flat-emitter bug the
   fixed goldens happen to miss.
3. **Refusal vectors through flat.** The break-the-language diagnostics suite and
   the hostile-stream vectors run against the flat reader too: every stream a
   conforming reader must reject, the flat reader must reject (refusal rules are
   part of the format). Checked-mode write refusals are asserted equal across
   tiers.
4. **Regen-match.** `*Flat.js` modules in `generated/` are committed and CI
   regenerates and diffs, like every backend — the emitter is deterministic to
   the byte, no formatter in the path.
5. **Both mode variants tested.** The checked/production write fork is exercised
   in CI the way serialize.js's own is: production legs prove the wire, checked
   legs prove the refusals, and a cross-mode write of in-contract values proves
   byte identity between the two writers.

Nothing about the existing six-language identity gates changes; the flat tier is a
seventh set of hands held to the same pins.

---

## §6 Browser reality — what is proven, what is owed, what cannot break (Q4)

**Every number in §0 is node/V8 on the M2 Air.** The design keeps browsers open by
construction and names what must be re-verified before any browser claim:

**Structurally safe by design (no re-verification needed):**
- No node APIs in flat modules: pure ESM, `DataView`/`Uint8Array`/`Math`/`BigInt`
  only; the single `process` touch is existence-guarded (§4). Imports are
  relative, extension-full, static — the shape every bundler and native browser
  ESM loads without configuration.
- No wasm: the memory-growth/view-detach/re-fetch risk class from the wasm
  experiment is absent, not mitigated. Buffers are caller-owned; the generated
  code never allocates or grows anything.
- No flags required for correctness: the V8 inline-budget trio is a performance
  option on one engine, never a semantic input. Wire identity gates are
  engine-independent JavaScript.

**Owed before browser claims (the re-verification list):**
1. **Correctness in JSC and SpiderMonkey**: the golden + cross-tier gates (§5)
   run in Safari and Firefox via a headless harness (playwright or a plain test
   page). Expected pass — everything used is spec'd ECMAScript — but expected is
   not proven, and the gate harness is the proof mechanism we already trust.
2. **Performance re-race per engine**: DataView throughput and inline budgets
   differ across engines (JSC's DataView history specifically); the probe's
   bimodality is a V8 artifact. The bench (§7) runs per engine before any
   cross-engine number is cited. If an engine inverts flat vs runtime-call —
   unlikely at 8x margins — the ruling's mechanism is a re-race, era-stamped.
3. **Bundler pass-through**: esbuild/rollup/webpack over a generated unit —
   NODE_ENV static replacement verified (checked writer tree-shaken in
   production bundles), no import rewriting breakage, size measured (the probe's
   97-field packet: 80 KB source, **4.3 KB gzipped**; a full unit with checked
   variants and batch entries projects to tens of KB gzipped — cited from
   measurement once the emitter lands).
4. **The exactly-sized-buffer idiom**: browser and node receive paths hand over
   exact buffers; the documented pattern (one persistent `MessageMaxBytes +
   FLAT_READ_SLACK` scratch, `set()` the payload in, read from the scratch) is
   written in USAGE.md with the copy priced honestly in the bench.

---

## §7 BENCH-STANDARD and the runners — the js leg measures what ships (Q7)

Per the Implementation Law (serialize.js STANDARD.md: the job is the fastest
correct implementation; speed is normative) and BENCH-STANDARD's own premise, the
benched path must be the shipped path. Changes:

1. **The js gen-family and real_packet rows switch to the flat tier** — per-call
   flat functions, because §3.2 (call sites the same on both sides) makes
   per-packet calls the cross-language-comparable shape every sibling leg uses.
   These rows are THE js numbers.
2. **A `codec` field distinguishes js rows**: `codec=flat` for the shipped path;
   the runtime-call generated rows may ride as labeled supplementary rows
   (`codec=runtime`) so the compat tier stays observable and its regressions
   visible — but they never stand as the js number.
3. **The rt family stays on serialize.js** — it measures the runtime library
   artifact itself, which still ships as the runtime tier; nothing changes there.
4. **Flags**: standard js rows run default node flags (§3.3: consumer flags are
   the honest shape — no consumer runs a tuned V8). The inline-budget trio rides
   as a flagged supplementary row where wanted; the known default-flag
   bimodality on small packets is exactly what §2.3's spread policy exists to
   surface — record it, and the trio row is the stability instrument.
5. **Batch rows are supplementary and excluded from cross-language ratios** until
   sibling languages expose the same batch call shape — a fused-loop row breaks
   §3.2 comparability, so it is reported (it is the fastest real shape and users
   should see it) but never enters the ×-off-C columns.
6. **§3.5 provenance**: flat rows carry no runtime version (the tier imports no
   runtime); the CSV records the schema commit as the generated-code provenance,
   and the runtime-paths guard applies only to `codec=runtime` and rt rows.
7. **The golden gate stays law**: the js runner's oracle gate (§1.5) binds the
   flat leg to the corpus pins before any timing, as every leg is bound — a gate
   failure reports nothing.

---

## §8 Open questions — named, not smuggled

1. **Objects and tables in flat.** V1 scope is types + messages + dispatch (the
   raced surface). Extending to object state/view families is the same emission
   discipline; it should follow a measured need (the space-game object path is
   the likely one), not ride now.
2. **Batch entries beyond messages.** Messages-only in v1. If profiling shows a
   hot batched TYPE shape (snapshot interiors), the same emission applies; the
   lever is named here so the decision is a one-liner later.
3. **The supplementary `codec=runtime` rows** — permanent, or retired once the
   flat tier has a few eras of history? Bench-noise budget says retire
   eventually; observability says keep. Glenn's call when it costs something.
4. **Per-engine policy.** If a browser engine's race (§6.2) inverts the verdict,
   the ruling's own mechanism applies — re-race, era-stamped, one shipped path
   per language chosen by measurement. The design deliberately does not
   pre-authorize per-engine forks; two shipped codecs selected by user-agent is
   complexity no measurement has yet asked for.

---

## Appendix — the full verdict table (era: flatjs probe, 2026-08-18, M2 Air, node 26.7)

mixed_packet (21 B; same-session native C: 149.0 W / 425.4 R M/s):

| contender | write M/s | read M/s | read ×-off-C |
|---|---|---|---|
| flattened JS (default) | 21.6 (vary-bound) | 66.8 (bimodal, floor 44.4) | 6.4x (floor 9.6x) |
| flattened JS (V8 trio) | 23.2 (vary-bound) | 74.0 | 5.7x |
| flattened JS, loop-fused read (default flags) | — | **134.7** | 3.2x |
| wasm whole-packet (c3) | 20.7 (vary-bound) | 76.9 | 5.5x |
| wasm per-field (c2) | 11.0 | 27.5 | 15.5x |
| runtime-call JS (default/trio) | 3.5 / 4.7 | 5.4 / 8.8 | 78x / 48x |

real_packet (204 B; cited C++ 21.7/27.2, C 26.3/30.9 M/s):

| contender | write M/s | read M/s | read ×-off-C++ |
|---|---|---|---|
| flattened JS (default) | 1.37 (vary-bound; varyonly 1.71) | 5.31 | 5.1x |
| flattened JS (trio) | 1.92 (vary-bound; varyonly 2.64) | 5.42 | 5.0x |
| generated JS (default/trio) | 0.50 / 0.79 | 0.63 / 1.07 | 43x / 25x |

Vary-subtracted write residuals: flattened ~143 ns/packet (~7.0 M/s pure-serialize,
identical under both flag modes); generated ~1424 ns (~0.70 M/s). Load avg 3.2–3.5
throughout; cross-round spreads <3% on every leg except the named bimodal one; wasm
and gen_real legs reproduce their era-pinned values within noise. Probe artifacts:
`~/rowan-working/scratch-flatjs/` (emitter, flattened modules, gated harness, three
rounds of CSVs + gate transcripts + load records).
