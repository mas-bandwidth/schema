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
| C | 2 (+1 noticed) | 2 (SF-36 rejected) | 0 |
| C++ | 2 (+1 noticed) | 2 (SF-36 rejected) | 0 |
| C# | 0 | 0 | 0 |
| C# (unity) | 2 | 2 | 0 |
| Rust | 7 (+2 noticed) | 8 (SF-11 rejected) | 0 |
| Java | 1 | 1 | 0 |
| Dart | 1 | 1 | 0 |
| Go | 1 | 1 | 0 |
| Goose | 2 | 2 | 0 |
| JavaScript | 5 (+1 noticed) | 6 | 0 |
| Elixir | 4 | 4 | 0 |
| Lua | 0 | 0 | 0 |
| Luau | 1 | 1 (SF-33: re-vendored) | 0 |
| Squirrel | 0 | 0 | 0 |
| MOO | 1 (tooling) | 1 | 0 |
| Lobster | 4 (+1 noticed) | 5 | 0 |
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
| SF-22 | Elixir | internal/codegen/elixirtable/variableform.go (`check_field` `:list`; descriptor `min_count`) | `save/2` and `to_json` wrote a counted array below its `Min`. | SPEC.md §5.1 row 3 | `n < min_count` refuses `invalid_value`; the descriptor emits `min_count` for a counted array (the same key SF-02 reads, so the two PRs meet on that one line) | M | `TestElixirVariableWritersRefuseCountBelowMinAndNonList/save-min` |
| SF-23 | Elixir | internal/codegen/elixirtable/messageform.go (`write_owner_rows`, `check_field/4`) | `save_messages` held an integer to the union of every declaration sharing its field name and kind, so `A.hp = 6` passed on `B.hp`'s range. | SPEC.md §5.1 row 1; SPEC-TABLES §3.3 | the rules are also emitted per owning declaration (a table, a struct or `{:union, name}`) and checked by owner; the merged rule remains only for a field its owner declares no rule for | M | `TestElixirSaveMessagesHoldsEachDeclarationToItsOwnRange` |
| SF-27 | Lobster | internal/codegen/lobstertable/runtime/text_form.lobster (`t_set_scalar`, enum) | The text read gave a deprecated variant its own ordinal. | SPEC-TABLES §2.15 READ | a deprecated match reads the field default (an element: None), as the JSON read does | M | `TestLobsterTextReadHoldsDeprecatedVariants` |
| SF-28 | Elixir | internal/codegen/elixirtable/variableform.go (`json_field` `:list`) | `to_json` wrote a list field holding a non-list as `[]`. | SPEC.md §5.1; SPEC-TABLES §16.3 | `nil -> "[]"`, anything else `throw(:invalid_value)` | L | `.../json-non-list` |
| SF-29 | Goose | internal/codegen/goosetable/runtime/cook_write.goose (`cook_live`) | The region cook clamped a counted length to the bound and wrote it. | SPEC-TABLES §7.6 | `c.refused` below 0, below `Min` or above the bound | L | `TestGooseCookRegionWriterNegativeControl/count-bound` (the bounds program's `count` check) |
| SF-31 | Lobster | internal/codegen/lobstertable/runtime/block_form.lobster (`block_build`) | An array count, a counted string or bytes bounded by an `assert` only, which `--runtime-no-asserts` removes. | SPEC-TABLES §19.1; SPEC.md §5.1 rows 3, 4 | a named refusal (`block_refusal`), the builder answers `""` | L | `TestLobsterBlockBuildRefusesByName` |
| SF-32 | MOO (tooling) | internal/codegen/moo/moorun/moorun.go (`Analyze`) | moo-analyze dying with no output counted as clean. | brief rule | `AnalyzeOutcome` fails a failed run with empty output | L | `TestAnalyzeOutcomeRefusesSilentFailure` |
| SF-37 | Lobster | internal/codegen/lobstertable/runtime/cook.lobster, schema_save.lobster (`wc_non_finite`) | The cook walk let a NaN or an infinity in a plain float through. Verified, not only noticed. | SPEC.md §5.1 row 7 | the walk records it, `cook_contract_holds` refuses | M | `TestLobsterCookHoldsTheWriteContract` (`float_nan`, `float_inf`) |

| SF-05 | C | internal/codegen/ctable/message_save.go (`emitMessageScalarWrite`) | A `fixed table` past the size ceiling got `save_messages` with no row 1, 2 or 10 assert. | SPEC.md §5.1 rows 1, 2, 10 | assert row 1, 2 and 10 per scalar on fixed tables past the ceiling | M | `TestCMessageCeilingWriteAsserts` |
| SF-06 | C++ | internal/codegen/cpptable/messagecodec.go | The same as SF-05. | as SF-05 | as SF-05 | M | `TestCppMessageCeilingWriteAsserts` |
| SF-16 | Dart | internal/codegen/darttable/wireform.go (`emitLoad`, `<root>WireLoad`) | A caller who passed no report got the defaults on any refusal and no signal. | SPEC-TABLES §4, §6.5 | the public load reads into a local report and throws `FormatException(reason)` when none was passed and the read refused | M | `TestWireLoadWithNoReportThrowsOnRefusal`; the Dart `wire` driver (every case twice) |
| SF-30 | Go | internal/codegen/gotable/json.go:110 (builder `FromJSONWith`) | On a locked arena it returned `false` before the report was reset, so a reused report kept a stale success. | SPEC-TABLES §6.5, §16.2 | reset the report, then refuse `allocation_failed` into it | L | `TestBuilderFromJSONLockedRefusesIntoTheReport` |
| SF-34 | Rust | internal/codegen/rusttable/mapsarms.go (`emitFixedArraySave`, `emitArrayBodyWrite`) | A `[N]T` held as a `Vec` was cut at N or padded on save with no assert. Verified. | SPEC.md §5.1; SPEC-TABLES §3 | `debug_assert!(len == N)` before the elision test and in the body write | M | `TestRustWriteRefusalReachesEveryCarrier/SF-34` (+ `...Native`, slow) |
| SF-35 | Rust | internal/codegen/rusttable/mapsarms.go (`emitKeyedSave`) | A keyed `[E]T` whose length was off the variant count dropped its extra values or skipped its missing slots. Verified. | as SF-34 | `debug_assert!(len == variants)` | M | `.../SF-35` |
| SF-01 | JavaScript | internal/codegen/jstable/variableruntime.go (`TableVariableReadArrayBody`), fixedjs.go (`emitCheckPayload`, `emitDecodeCount`), fixedruntime.go (`TableFixedOpCount`) | A count below `Min` in `[Min..N]T` loaded as success on the binary body read, on the fixed file read and on the evolved plan. | SPEC-TABLES §22.2 | refuse `count_over_bound` by name before any element is read; the plan row carries the reader minimum A | H | `TestJsReadersRefuseCountBelowMin`; corpus `framing` `sp_count_min_*_form1`, `report` `sp_count_min_evolved_*` |
| SF-02 | Elixir | internal/codegen/elixirtable/variableform.go, fixedelixir.go, cookwrite.go (`min_count`) | The same as SF-01 for the binary, JSON, fixed and evolved reads. | as SF-01 | emit `min_count`; refuse below it on every read | H | `TestElixirCountedMinimumHoldsOnEveryPath` |
| SF-24 | JavaScript | internal/codegen/js/functions.go (`emitWriteField`) | The bulk `[N]uint8` packet write handed over `subarray(0, N)` without checking shorter storage. | SPEC.md §5.1 (storage rule) | refuse unless it is a `Uint8Array` of length at least N | M | `TestBulkByteArrayWriteRefusesShortStorage` |
| SF-38 | JavaScript | internal/codegen/jstable/fixedjs.go (`emitFromTextField`) | A text-placed array field that is a non-array-like value fell back to fresh defaults. | SPEC.md §5.1 | place a present, non-null, non-array-like value as held so `FixedCheckBody` refuses it | L | `TestJsWritersRefuseSilentDrops` (SF-38 cases) |

SF-11 (Rust `?string(N)` and `?bytes(N)` length asserts) was rejected: the checker refuses those declarations (SPEC-TABLES §15), so the emitter branch is unreachable.

SF-36 (C, C++ non-required ranged float64) was rejected: the checker refuses a float64 range (SPEC §4.3, `the compressed float is float32`), so no such field can be declared. Its second half was real and is fixed: the C and C++ variable `Save` lines now distinguish `| required` every-build checks from debug asserts (SPEC.md §5.1).

## Still open, ranked by blast radius

| id | leg | file and line | what is lost | spec section | fix | blast |
|---|---|---|---|---|---|---|

## Fan-out groups (the open rows by leg)

- **Tooling (Luau)**: SF-33 (rejected here, see below).
- **Corpus (all legs)**: the variable-form Save's `bits(N)` width on a plain field, an optional and an array element is held by the shared `write-refusal` rows `sfv_bits_*`, `sfv_opt_bits_*` and `sfv_elem_bits_*` (SF-03, SF-04, SF-10, SF-12). Still owed, each for a named reason in the "Silent-failure rows" section of SPEC-CONFORMANCE.md: SF-09, SF-13, SF-14, SF-15 (no corpus unit has the shape on a variable root or the walkers cannot reach it), SF-17 and SF-08's float (no corpus unit declares a ranged float), SF-18/19 (no message-capable flags root with a connection), SF-20/21 (`count` on a `[N]T`; no `[Min..N]T` unit) and SF-25/26 (no cook refusal verdict; no deprecated variant). Until a row carries them, each rule is held per port by the tests named above.

## Rejected as lawful (sample)

- **SF-33 (Luau, `pcall(Stream.family("bitpacker"))`)**: fixed upstream, no PR needed. serialize.luau v2.1.4 (dc94987) has no probe at all; `SERIALIZE_PIN` now names it and the runtime is re-vendored verbatim. `TestVendoredLuauHasNoDiscardedBitpackerProbe` is red when the old probe is planted back; `TestVendoredLuauBitpackerFamilyLoads` holds the family loads.
- **SF-38 (JavaScript, `FixedFromText`, an enum-keyed value with no `length`)**: rejected. The variable text read builds an enum-keyed value as an `Array` sized to the slots (`TableVariableTextKeyed`, `fd.slots.map`), so it always has a `length`; a field absent from the text keeps its declared default, which is lawful (SPEC-TABLES §4). Nothing falls back silently.

- **Read-side clamps that set `refused`.** C, C++, Java, Rust, Elixir and Go all have them, and each refuses the whole load. SPEC.md §5: "a refusal is terminal."
- **No write check in an assert leg's release build.** SPEC.md §5.1: "none is a defect."
- **A FILE read keeps an unnamed flags bit.** SPEC.md §5.1 row 9.
- **Write-side quantizer clamps** (Python, Ruby, Squirrel, Goose, Elixir packet). They run after a guard has already refused an off-range value.
- **Lua, Luau and MOO pcalls.** Each one re-raises or maps its failure to a named refusal.
