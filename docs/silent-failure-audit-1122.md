# Silent-failure audit, 2026-11-22

Glenn, 2026-11-22: "Nothing is allowed to fail silently."

**Scope.** The code generators and the runtimes they embed for all 18 targets
(`internal/codegen/*`). The audit looked for every place where a runtime or
generated code can lose an error without telling the caller. The patterns were:

- an empty catch or rescue;
- an ignored return;
- a clamp or a cut where the spec refuses;
- a default returned on malformed input;
- a field dropped on write;
- a missing debug assert in an assert leg;
- a discarded pcall;
- an unchecked allocation.

**Rules.**

- **Writes** follow SPEC.md §5.1. C, C++, C#, Rust, Java and Dart use debug asserts. Go, JS, Elixir, Lua, Luau, Squirrel, MOO and Goose refuse in every build, and so do the Lobster, Python and Ruby table writers.
- **Reads** refuse in every build (SPEC.md §5).
- **Lawful behaviour is not a finding.** Examples: eliding a default, an assert leg's release build, a read failure that leaves the output unspecified, a FILE read that keeps an unnamed flags bit.

**Ranking.** Rows are ranked by blast radius:

- **H**: a wrong value is accepted from the network.
- **M**: a wrong answer on an uncommon path, or a write that the contract refuses is accepted.
- **L**: tooling, diagnostics, or an edge that is unreachable in practice.

Line numbers are those on main at e9d4e49378 (the audit base).

Every fixed row lands three ways:

1. a test that is red when the old code is planted back;
2. the spec sentence;
3. a row in SPEC-CONFORMANCE.md under "Silent-failure rows".

## Findings per leg

| leg | found | fixed | open |
|---|---|---|---|
| C | 2 (+1 noticed) | 1 | 2 |
| C++ | 2 (+1 noticed) | 1 | 2 |
| C# | 0 | 0 | 0 |
| C# (unity) | 2 | 2 | 0 |
| Rust | 7 (+2 noticed) | 6 (SF-11 rejected) | 2 |
| Java | 1 | 1 | 0 |
| Dart | 1 | 0 | 1 |
| Go | 1 | 0 | 1 |
| Goose | 2 | 1 | 1 |
| JavaScript | 5 (+1 noticed) | 6 | 0 |
| Elixir | 4 | 4 | 0 |
| Lua | 0 | 0 | 0 |
| Luau | 1 | 0 | 1 |
| Squirrel | 0 | 0 | 0 |
| MOO | 1 (tooling) | 0 | 1 |
| Lobster | 4 (+1 noticed) | 2 | 3 |
| Python | 0 | 0 | 0 |
| Ruby | 0 | 0 | 0 |

"Noticed" rows were seen by a fixer while working the fixed rows. Nobody has verified them yet, so a fixer re-checks each one against the spec first.

## Fixed in this PR

| id | leg | file and line | what is lost | spec section | fix | blast | test |
|---|---|---|---|---|---|---|---|
| SF-03 | C | internal/codegen/ctable/codecs.go:509 (`emitTableWriteElement`) | The variable `save`/`measure` had no check of rows 1–2 on a field that is not `\| required`. A debug build wrote an off-range value or a too-wide `bits(N)` and returned success. | SPEC.md §5.1 rows 1–2; the C variable Save line ("1–4 and 8 debug") | `schema_assert` on the raw range and the `bits(N)` mask | M | `TestCVariableWriteRangeAssert`, `TestCVariableWriteRangeAssertCompiled` |
| SF-04 | C++ | internal/codegen/cpptable/codecs.go:1100 (`emitRequiredRangeCheck`) | The same gap in `Save`, `Measure` and the builder and region `Save`. | SPEC.md §5.1 rows 1–2; the C++ variable Save line ("1–2 and 8 debug") | `schema_assert` for non-required fields | M | `TestCppVariableWriteRangeAssert`, `TestCppVariableWriteRangeAssertCompiled` |
| SF-07 | C# (unity) | internal/codegen/unity/functions.go:429 | The packet `Write` of a `string(N)` asserted only the length. Ill-formed UTF-8 or a zero byte was sent. | SPEC.md §5.1 row 8; the C# packet Write line ("1, 3–8 and 10 debug") | `Debug.Assert(ValidUtf8Text(...))` | M | `TestUnityPacketWriteAssertsRows8And10` |
| SF-08 | C# (unity) | internal/codegen/unity/functions.go:419 | A compressed float32 was quantized with no finite or range assert. | SPEC.md §5.1 row 10 | `Debug.Assert(float.IsFinite(v) && v >= min && v <= max)` | M | `TestUnityPacketWriteAssertsRows8And10` |
| SF-09 | Rust | internal/codegen/rusttable/variablecodec.go:988 | A plain `bytes(N)` was written at its full length, with no assert. | SPEC.md §5.1 row 4 | `debug_assert!(len <= N)` | M | `TestRustWriteRefusalReachesEveryCarrier/SF-09` (+ `...Native`, slow) |
| SF-10 | Rust | internal/codegen/rusttable/mapsarms.go:247 | An optional scalar skipped `scalarSaveRefusal` (rows 1, 2 and 10). | SPEC.md §5.1 rows 1, 2, 10 | `scalarSaveRefusal` on `(*opt)` | M | `.../SF-10` |
| SF-12 | Rust | internal/codegen/rusttable/mapsarms.go:553 (`emitElementWrite`) | Array elements had a float assert only; nothing asserted the integer range or the bits width. | SPEC.md §5.1 rows 1–2 | `scalarSaveRefusal` per element | M | `.../SF-12` |
| SF-13 | Rust | internal/codegen/rusttable/mapsarms.go:524 | `len().min(Max)` silently cut an over-long optional array, arm array or arm `bytes(N)`. | SPEC.md §5.1 rows 3–4 | `debug_assert!(len <= Max/N)`; the clamp stays for storage | M | `.../SF-13` |
| SF-14 | Rust | internal/codegen/rusttable/mapsarms.go:499 | A union-arm scalar had no range or bits assert. A `string(N)` arm had no length or zero-byte assert. | SPEC.md §5.1 rows 1, 2, 4, 8 | `scalarSaveRefusal` and the string asserts | M | `.../SF-14` |
| SF-15 | Rust | internal/codegen/rusttable/mapsarms.go:340 | A keyed-array scalar slot had no range or bits assert. A string slot cannot be declared (SPEC-TABLES §2.13). | SPEC.md §5.1 rows 1–2 | `scalarSaveRefusal` per slot | M | `.../SF-15` |
| SF-17 | Java | internal/codegen/javatable/messageform.go:1983 (`messageCheckValue`) | `SaveMessages` had no row-10 assert. An off-range float was clamped to an endpoint, and the reader accepted it. | SPEC.md §5.1 row 10; strict-only decision 8 | `assert !isFinite(v) \|\| (lo <= v && v <= hi)` | M | `TestJavaSaveMessagesWriteContract` (row10 case, slow) |
| SF-18 | Goose | internal/codegen/goosetable/messageform.go:269 | `SaveMessages` and `MeasureMessages` masked a flags bit above the declared width. The batch read back a different mask. | SPEC-TABLES §3.3; ruling `message-flags-width-is-caller-error`; SPEC.md §5.1 row 9 | refuse `MsgRefuseInvalidValue` | M | `TestGooseMessageFormRefusesFlagsBitAboveWidth` |
| SF-19 | JavaScript | internal/codegen/jstable/messagebatch.go:636 | `SaveMessages` masked a flags value with `BigInt.asUintN`, including a negative value. | as SF-18 | refuse `x < 0n \|\| x > 2^W−1` | M | `TestJsWritersRefuseSilentDrops` |
| SF-20 | JavaScript | internal/codegen/jstable/fixedjs.go:438, :460; messagebatch.go:572 | `FixedSave` and `SaveMessages` dropped the elements of a `[N]T` or `[E]T` past `N`. `FixedFromText` cut or padded them. | SPEC.md §5.1 (no writer silently writes a value off the contract) | refuse unless the length is exactly `N`, the variable Save's rule | M | `TestJsWritersRefuseSilentDrops` |
| SF-21 | JavaScript | internal/codegen/jstable/fixedjs.go:446 | `FixedSave` accepted a count below `Min`. | SPEC.md §5.1 row 3 | `count < ArrayMin` refuses | M | `TestJsWritersRefuseSilentDrops` |
| SF-25 | Lobster | internal/codegen/lobstertable/runtime/cook.lobster:462 (`cw_str`), :559 (`cw_array`) | `Cook` never ran the write contract. It cut an over-long string mid code point, wrote an over-bound count into the next field, wrote no arm for an out-of-set tag, and returned success. | SPEC-TABLES §7.6; SPEC.md §5.1 rows 3, 4, 6 | the `wc_body` walk before cook and measure; no clamp | M | `TestLobsterCookHoldsTheWriteContract` (slow) |
| SF-26 | Lobster | internal/codegen/lobstertable/runtime/schema_save.lobster:873 | The variable `save` wrote a deprecated enum variant. | SPEC-TABLES §2.15 WRITE | `wc_value` refuses a deprecated variant with `invalid_value` | M | `TestLobsterCookHoldsTheWriteContract` |
| SF-01 | JavaScript | internal/codegen/jstable/variableruntime.go (`TableVariableReadArrayBody`), fixedjs.go (`emitCheckPayload`, `emitDecodeCount`), fixedruntime.go (`TableFixedOpCount`) | A count below `Min` in `[Min..N]T` loaded as success on the binary body read (the harness row passed only because an unrelated cycle check damaged the load), on the fixed file read and on the evolved plan. The text read was fixed with SF-02 (#4689). | SPEC-TABLES §22.2 | refuse `count_over_bound` by name before any element is read; the plan row carries the reader minimum A | H | `TestJsReadersRefuseCountBelowMin`; corpus `framing` `sp_count_min_*_form1`, `report` `sp_count_min_evolved_*` |
| SF-02 | Elixir | internal/codegen/elixirtable/variableform.go, fixedelixir.go, cookwrite.go (`min_count`) | The same as SF-01 for the binary, JSON, fixed and evolved reads. The descriptor carried no `min_count`. Landed with #4689 (cherry-picked here so the rows run); this PR adds the test. | as SF-01 | emit `min_count`; refuse below it on every read | H | `TestElixirCountedMinimumHoldsOnEveryPath` |
| SF-22 | Elixir | internal/codegen/elixirtable/variableform.go (`check_field` `:list`) | `save/2` and `to_json` wrote a counted array below its `Min`. | SPEC.md §5.1 row 3 | refuse `n < min_count` | M | `TestElixirCountedMinimumHoldsOnEveryPath`; corpus `write-refusal` `wref_count_min`, `write-refusal-fixed` `fref_count_min` |
| SF-23 | Elixir | internal/codegen/elixirtable/messageform.go (`check_field`, `messageScopedRuleRows`) | `save_messages` held an integer to the union of every declaration that shares its field name and kind, so `A.hp = 6` passed on the strength of `B.hp`. | SPEC.md §5.1 row 1; SPEC-TABLES §3.3 | a rule set per declaration scope (a table name or `{:union, name}`); a field the bound record does not declare keeps the merged rule | M | `TestElixirSaveMessagesHoldsEachDeclarationToItsOwnRange` |
| SF-24 | JavaScript | internal/codegen/js/functions.go (`emitWriteField`) | The bulk `[N]uint8` packet write handed over `subarray(0, N)`, and a storage shorter than N made a short field on the wire with no error. Traced end to end: a short `Uint8Array` is written short. | SPEC.md §5.1 (storage rule) | refuse unless it is a `Uint8Array` of length at least N | M | `TestBulkByteArrayWriteRefusesShortStorage` |
| SF-28 | Elixir | internal/codegen/elixirtable/variableform.go (`json_field` `:list`) | `to_json` wrote a list field holding a non-list as `[]` and succeeded. | SPEC.md §5.1; SPEC-TABLES §16.3 | `nil` writes `[]`, anything else refuses `invalid_value` | L | `TestElixirCountedMinimumHoldsOnEveryPath` |
| SF-38 | JavaScript | internal/codegen/jstable/fixedjs.go (`emitFromTextField`) | Verified: a text-placed array field that is a number, a string or an object with no `length` fell back to the fresh defaults and the writer wrote them. | SPEC.md §5.1 | place a present, non-null, non-array-like value as held so `FixedCheckBody` refuses it | L | `TestJsWritersRefuseSilentDrops` (SF-38 cases) |

SF-11 (Rust `?string(N)` and `?bytes(N)` length asserts) was rejected: the checker refuses those declarations (SPEC-TABLES §15), so the emitter branch is unreachable.

## Still open, ranked by blast radius

| id | leg | file and line | what is lost | spec section | fix | blast |
|---|---|---|---|---|---|---|
| SF-05 | C | internal/codegen/ctable/fixedform.go:1215 (`writeAdmits` gated on `TableFixedEmitted`); message_save.go:73 | A `fixed table` past the size ceiling still gets `save_messages` but no row 1, 2 or 10 assert, and `table_bit_put` masks the value. | SPEC.md §5.1 rows 1, 2, 10 | gate `writeAdmits` on `FixedDeclared`, or assert per value in `messageScalar` | M |
| SF-06 | C++ | internal/codegen/cpptable/fixedform.go:1305; messagecodec.go:526; message.go:177 | The same as SF-05. | as SF-05 | as SF-05 | M |
| SF-16 | Dart | internal/codegen/darttable/wireform.go:3848 (`<root>WireLoad`, `report ??= TableWireReport()`) | A caller who passes no report gets the defaults on any refusal and no signal. `<root>FromJson` throws in the same case. | SPEC-TABLES §4, §6.5 | throw `FormatException(reason)` when no report was passed and the load refused | M |
| SF-27 | Lobster | internal/codegen/lobstertable/runtime/text_form.lobster:516 | The text read gives a deprecated variant its own ordinal, not the field's default. | SPEC-TABLES §2.15 READ | map a deprecated match to the field default (0 for an element) | M |
| SF-34 | Rust | internal/codegen/rusttable (fixed `[N]T` stored as `Vec`, `take(N)`) | A `[N]T` longer than N is cut on save with no assert (noticed, unverified). | SPEC.md §5.1; SPEC-TABLES §3 "never cut at N" | `debug_assert!(len == N)` | M |
| SF-35 | Rust | internal/codegen/rusttable (keyed `[E]T` save, `if j >= len break`) | Values past the enum's variant count are dropped (noticed, unverified). | as SF-34 | `debug_assert!` on the length | M |
| SF-36 | C, C++ | internal/codegen/ctable/codecs.go, cpptable/codecs.go | A non-required ranged float64 has no row-10 write check (noticed, unverified). The leg lines' "10 every build" covers `\| required` only. | SPEC.md §5.1 row 10 | `schema_assert` of the float range; correct both leg lines | M |
| SF-37 | Lobster | internal/codegen/lobstertable/runtime/cook.lobster (contract walk) | The cook walk lets a NaN or an infinity in a plain float through. Save catches it in its encoder; cook does not (noticed, unverified). | SPEC.md §5.1 row 7 | refuse non-finite in the walk | M |
| SF-29 | Goose | internal/codegen/goosetable/runtime/cook_write.goose:133 (`cook_live`) | The region cook clamps a counted length to `[0, bound]`, writes it and succeeds. Reaching it needs a caller-edited `LoadVal`. | SPEC-TABLES §7.6 | `cook_refuse` below 0, below `Min` or above the bound | L |
| SF-30 | Go | internal/codegen/gotable/json.go:110 (builder `FromJSONWith`) | On a locked arena it returns `false` before the report is reset, so a reused report keeps a stale success. | SPEC-TABLES §6.5, §16.2 | reset, then refuse into the report | L |
| SF-31 | Lobster | internal/codegen/lobstertable/runtime/block_form.lobster:419, :451 | `block_build` bounds a count by an `assert` only, which `--runtime-no-asserts` removes. A counted string or bytes overruns its slot. | SPEC-TABLES §19.1 (`Begin` does not clamp; it refuses); SPEC.md §5.1 | a named refusal returning `""` | L |
| SF-32 | MOO (tooling) | internal/codegen/moo/moorun/moorun.go:184 (`out, _ := cmd.CombinedOutput()`) | moo-analyze dying with no output counts as clean. | brief rule; not a wire rule | fail on `err != nil` with empty output | L |
| SF-33 | Luau | internal/codegen/luau/runtime/serialize_bytes.luau:41 (`pcall(Stream.family("bitpacker"))`) | A broken (not absent) bitpacker silently switches to the fallback path. | brief rule (no discarded pcall) | re-raise anything but the "absent" error | L |

## Fan-out groups (the open rows by leg)

- **C, C++**: SF-05, SF-06, SF-36.
- **Rust**: SF-34, SF-35.
- **Dart**: SF-16.
- **Lobster**: SF-27, SF-31, SF-37.
- **Go, Goose**: SF-29, SF-30.
- **Tooling (MOO, Luau)**: SF-32, SF-33.
- **Corpus (all legs)**: shared corpus cases are owed for SF-18/19 (a message-form flags value above its width, on `write-refusal-message`), SF-20 (fixed-array length on `write-refusal-fixed` and `write-refusal-message`) and SF-03/04 (a non-required ranged field on a variable root, on `write-refusal`). Until a surface carries them, each rule is held per port by the tests named above.

## Rejected as lawful (sample)

- **Read-side clamps that set `refused`.** C, C++, Java, Rust, Elixir and Go all have them, and each refuses the whole load. SPEC.md §5: "a refusal is terminal."
- **No write check in an assert leg's release build.** SPEC.md §5.1: "none is a defect."
- **A FILE read keeps an unnamed flags bit.** SPEC.md §5.1 row 9.
- **Write-side quantizer clamps** (Python, Ruby, Squirrel, Goose, Elixir packet). They run after a guard has already refused an off-range value.
- **Lua, Luau and MOO pcalls.** Each one re-raises or maps its failure to a named refusal.
