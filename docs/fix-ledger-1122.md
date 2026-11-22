# Fix ledger, 2026-11-22

Every per-language fix made on 2026-11-22 lands three ways: a regression test
that goes red on the old code, the rule in the specification (SPEC.md or
SPEC-TABLES.md), and a shared conformance row listed in the addendum
(SPEC-CONFORMANCE.md, Regression rows (2026-11-22)). This ledger groups the
day's fixes by rule. Inputs: #4646, #4649, #4650, #4651, #4652, #4656, #4657,
#4660, #4661, #4662, #4663, #4665, #4668, #4669, #4670, #4671, #4673, #4674,
#4675, #4676, #4677, #4678, #4679, #4680, #4685, #4687, #4688, #4689 and its
fixers (#4692, #4693, #4695, #4696, #4698, #4699), #4690, #4691, #4694, #4700.

Not behaviour, so not on the ledger: #4677 (the two missing `.bits` pins) and
the C++ test fix in #4700 (`test_message_form_quantized` saved a value the
write contract does not admit).

Owned elsewhere and referenced, not duplicated: #4689 (deprecated enum element,
counted minimum, float32 bound) and R10 (counted minimum inside a union arm).

A row id in `code` is a manifest case of `testdata/conformance/tables/MANIFEST.txt`
unless the cell says otherwise. **ADDED** marks what this branch adds. A dash is
a rule the shared harness cannot ask yet; the reason is in the cell.

| id | rule | per-language test (PR) | spec | shared row |
|---|---|---|---|---|
| FL-01 | A compressed float's -0 is stored as +0 by every writer (variable, retain, fixed, message, JSON, cook) | `TestDartCompressedNegZeroWriters` (#4649); `TestSquirrelCompressedNegZeroCorpus`, `TestMooCompressedNegZeroCorpus` (#4657); `TestJs/TestRubyCompressedNegZeroCorpus` (#4661); `TestPython/TestLuaCompressedNegZeroCorpus` (#4662); `TestRust/TestJavaCompressedNegZeroCorpus` (#4665) | SPEC-TABLES §3.3; rulings `fixed-compressed-negzero` | `feature-witnesses/compressed-negzero-go` (15 rows) |
| FL-02 | Every read zeroes the report the caller hands it | C JSON pin (#4646); `TestJsVariableReadZeroesTheReport` (#4652); `csunity_verdicts_test` (#4669); `java_report_reset_damage_cycle_test`, `report_refusals_test` (#4670); `codec_verdicts_rust_test` (#4673) | SPEC-TABLES §6.5 | — the harness hands every case a fresh report; no surface reuses one |
| FL-03 | After any refusal every value the call was handed holds its declared defaults (variable, JSON, message batch, fixed batch; a fixed batch checks every record before any lands) (R4) | C fixed batch (#4646); `refused_stores_test` Goose, Lobster `json_read_into` (#4668); C# (unity) `LoadMessages` (#4669); every leg (#4690); `TestCppFixedLoadWalkResetsEveryRowOnRefusal`, Goose `*_refused` (#4694); Rust F8 (#4700) | SPEC-TABLES §6.5 (THE CALLER'S VALUES AFTER A REFUSAL), §3.4 load steps 11-12, §3.3 | — per-leg rows `test/conformance/<leg>/rows/{F8,F10,R8,R26,W4}` and `tla/FixedBatch.tla`; no harness surface hands a value in |
| FL-04 | A writer refuses a bare `intN`/`uintN` value outside its width | `TestJsFixedWriterRefusesBareIntOffWidth` (#4652) | SPEC.md §5.1 row 11 | — a typed leg cannot build the value |
| FL-05 | An enum's `null` and `"None"` are one value, and both write the same bytes | `TestJsEnumNoneSpellingsWriteTheSameBytes` (#4652) | SPEC-TABLES §16.2 (enum row) | `sp-enum-none-name` (#4688) |
| FL-06 | The text writer refuses a flags value that is not a mask of named bits | `TestJsJsonFlagsWriterRefusesNonMasks` (#4652) | SPEC.md §5.1 row 9; SPEC-TABLES §16.3 | — a typed leg's mask cannot hold a non-mask |
| FL-07 | The bytes inside a JSON comment are well-formed UTF-8 with no NUL | `TestJsJsonStringEntryRefusesLoneSurrogateInComment` (#4652) | SPEC-TABLES §16.2 (COMMENTS) | **ADDED** `rr-comment-illformed-utf8`, `rr-comment-lone-surrogate`, `rr-comment-nul`, `rr-comment-wellformed-utf8` |
| FL-08 | An announcement over the receiver's entry capacity refuses `vocabulary_too_large` before any entry is written | `TestJsAnnounceReadOverCapacityTouchesNoEntry` (#4652) | SPEC-TABLES §3.3 (the announcement) | hostile corpus `announce-entries-too-large` (`internal/ci/hostilecorpus`) |
| FL-09 | A string map key is bounded by its own `string(N)`, never by the 255-byte name cap | `TestPythonJsonMapKeyBoundAndBytesEntry`, `TestRubyJsonMapKeyBytesAndWriteDepth` (#4650); `csunity_verdicts_test` (#4669); `TestMapKeyBoundedOnlyByItsDeclaration` (#4671); `TestTextMapKeyAboveNameCapReadsUnderItsOwnBound` (#4688) | SPEC-TABLES §16.2 (A SCHEMA-NAME STRING IS CAPPED AT 255 BYTES; map row) | `sp-map-key-256-wide`, `sp-map-key-300-wide`, `sp-map-key-301-wide` (#4688) |
| FL-10 | The JSON read takes bytes and holds them to the same UTF-8 rules as text | `TestPythonJsonMapKeyBoundAndBytesEntry`, `TestRubyJsonMapKeyBytesAndWriteDepth` (#4650) | SPEC-TABLES §16.2, §16.3 | `str-illformed-at-bound`, `str-raw-non-ascii`, `str-lone-lead-surrogate` (every driver reads the tree as bytes) |
| FL-11 | The text writer's depth cap counts table and type values only (W2) | `TestRubyJsonMapKeyBytesAndWriteDepth` (#4650) | SPEC-TABLES §16.2, §16.14 W2 | — no registered root writes a container at depth 129 |
| FL-12 | The text read's depth cap counts every container it walks, placed or skipped: maps, unions, keyed objects, arrays, flags | `codec_verdicts_rust_test` (#4673); **ADDED** `TestReadDepthCapCountsPlacedContainers` (the Go reference engine, this branch) | SPEC-TABLES §16.2 | **ADDED** `rr-depth-placed-union-map-128`, `-129`, `rr-depth-placed-keyed-map-128`, `-129`; `sp-skip-depth-129`, `-130` (#4688) |
| FL-13 | The sign of a zero survives a number token of any length | `json_negzero_locale_size_test` (#4651); C guard (#4660) | **ADDED** SPEC-TABLES §16.2 | **ADDED** `rr-num-negative-zero-long-token` |
| FL-14 | Float text never reads the process locale; every conversion runs under the C locale on the calling thread | `json_negzero_locale_size_test` (#4651); C locale test (#4660) | **ADDED** SPEC-TABLES §16.1 | — the process locale is not a harness input |
| FL-15 | A text writer into a caller's buffer writes the bytes it returns and nothing past them | `json_negzero_locale_size_test` (#4651); C guard (#4660) | **ADDED** SPEC-TABLES §16.1 | — a caller-buffer API (C, C++); the harness reads files |
| FL-16 | A null `*T \| required` pointer is written (index 0) and reads back null and present | Lua required null pointer (#4656) | **ADDED** SPEC-TABLES §2.3 (`\| required`) | — no registered root declares `*T \| required` |
| FL-17 | A fixed load with no stated capacity reads the file's own records; no port invents a default capacity | Lua fixed load capacity (#4656) | **ADDED** SPEC-TABLES §3.4 | — every driver states its own capacity |
| FL-18 | `SaveMessages` writes under the sender's own vocabulary with no announcement in hand, and a refused `SaveMessages` writes nothing | Lua `saveMessages` (#4656); Dart measure-first `SaveMessages` (#4670) | **ADDED** SPEC-TABLES §3.3 (two sentences) | — no surface drives a sender without an announcement |
| FL-19 | A deprecated enum variant reads as the field's default (the element's default in an array or map), and a writer refuses one | Luau (#4656); `savechecks_test` (#4663); `csunity_verdicts_test` (#4669); `codec_verdicts_rust_test` (#4673); #4692, #4693, #4695, #4696, #4698, #4699 | SPEC-TABLES §2.15 | owned by #4689: `sp-enum-elem-deprecated-array` |
| FL-20 | An `&node` label is its exact unsigned 64-bit value, never a double | Luau exact labels (#4656) | **ADDED** SPEC-TABLES §16.7 | **ADDED** `rr-label-past-2-53-distinct`, `rr-label-past-2-53-undefined`; `label-overflow` |
| FL-21 | Every field the value carries is checked on write, `required` or not | `savechecks_test` (#4663) | SPEC.md §5.1 | `wref_range_high`, `wref_count`, `wref_length` and the rest of `write-refusal` (non-required fields) |
| FL-22 | A map holding one key twice, a fixed array handed more than `N` elements, and an enum value naming no variant or a deprecated one are refused by every writer that can be handed them | `savechecks_test` (#4663); Goose duplicate-key save (#4668) | **ADDED** SPEC.md §5.1 | — a typed leg cannot hold these shapes |
| FL-23 | A float32 field holding a value past float32's range is refused by every writer, the text writer included | `json_float32_round_test` Lobster (#4668); **ADDED** `TestJsJsonWriterRefusesFloat32PastRange` (this branch) | **ADDED** SPEC-TABLES §16.3 | **ADDED** `rr_wref_f32_past_range`, `rr_wref_f32_max`, `rr_fref_f32_past_range`, `rr_fref_f32_max`, `rr_jref_f32_past_range`, `rr_jref_f32_max` |
| FL-24 | The hand-editable text form holds its limits (128 lists, bytes, tokens, nodes, allocation), needs a gap between atoms, and refuses an ill-formed comment | `text_limits_test` Lobster (#4668); `TestCPPTextFormDocumentVectors` (C++ fixed on this branch) | SPEC-TABLES §16.9.1, §16.9.4 | **ADDED** `test/conformance/textform/refusals/comment-illformed`; the limits have no isolating vector (a 129-deep text also refuses for its kind) |
| FL-25 | A counted array is born at its minimum and elides there | the #4687 lane (`counted_birth_engine_test` per leg); `csunity_verdicts_test` (#4669) | SPEC-TABLES §22.2, §2.13 | `test/verify-tablegen` op 7 pin matrix; `feature-witnesses/counted-minimum-message`; `sp-count-min-*` (#4689) |
| FL-26 | A map whose binary entries repeat a key is damage | `csunity_verdicts_test` (#4669) | SPEC-TABLES §3 (a map's entries ascend with no key twice), §2.8 | **ADDED** `rr_map_key_repeated`; `map_descending` |
| FL-27 | Base64 that is not the canonical spelling refuses | `csunity_verdicts_test` (#4669) | SPEC-TABLES §16.8, §16.2 | `base64-padding-*`, `base64-unpadded`; `sp-b64-escaped-noncanonical` (#4688) |
| FL-28 | A cook handed a cyclic pointer graph refuses `data_cycle` in every build | `java_report_reset_damage_cycle_test` (#4670) | SPEC-TABLES §7.6, §3.1 | — no read the harness drives can deliver a cycle |
| FL-29 | A read refuses through its report, never by throwing | `report_refusals_test` Dart (#4670) | SPEC-TABLES §6.5 | — the harness judges the verdict; a caught throw reads as a refusal |
| FL-30 | Every construct is carried or refused by name; nothing is silently omitted | `codec_verdicts_rust_test` (#4673) | SPEC-TABLES §11 | the harness absence rule (SPEC-CONFORMANCE §2) |
| FL-31 | `schema check` refuses every name some target cannot build | `buildable_names_test` (#4691); Rust std-named tables (#4673) | SPEC.md §4.6 | `internal/check` buildable-names cases (one checker for every target, #4691) |
| FL-32 | A refusal carries its own reason name or code (C# (unity) message codes, Java `wire_damaged`, `layout_unknown`) | `TestUnityMessageRefusalCodes` (#4679); #4670; C++ R15 (#4700) | SPEC-TABLES §6.5 (names are recommended) | not judged: SPEC-CONFORMANCE §4 compares verdicts only |
| FL-33 | R2: a writer's `?T` read into a reader's bare `T` refuses `kind_mismatch` | `TestFixedLineageOptionalIntoBareRefusesKindMismatch`, `TestRuling*` (#4680) | SPEC-TABLES §3.4 (the presence entry) | `internal/ci` `TestRulingsR2R5R9PortRows` (a row per port); no harness row (a hand-forged lineage) |
| FL-34 | R5: `LoadMeasure` reports data and attribution separately | `TestRulingsR2R5R9PortRows` (#4680) | SPEC-TABLES §6.3 | `TestRulingsR2R5R9PortRows`; no harness row |
| FL-35 | R9: a caller bound of 0 bytes refuses and never means the default | `MaxBytes = 0` cases in Go, JS, Rust, Java (#4680) | SPEC-TABLES §3.3 (the announcement bounds) | `TestRulingsR2R5R9PortRows`; no harness row (no caller bound) |
| FL-36 | R3: strict announcement mode, one shape in every port | `TestMessageFormHeld*/strict_mode` (#4674); six per-language probes (#4675) | SPEC-TABLES §3.3 (STRICT MODE IS OPT-IN) | — no announcement-strict surface |
| FL-37 | R7: labels inside a skipped JSON value obey the label rules | #4676 | SPEC-TABLES §16.7 | `skipped-label-*` (#4676) |
| FL-38 | R8: no writer writes more than 4096 ids or announces more than 4096 entries; a 4097th retained id is lost and counted | one test per port on `tablewire.IdCapSchema` (#4678); `tablewire.AnnounceSchema` (#4685) | SPEC-TABLES §3 (the id table); **ADDED** §3.3 (the sender) | `frame_entries_4096`, `frame_entries_4097` (the reader's half); the writer's half runs per port on the shared schemas |
| FL-39 | A fixed-form file holds at least one record | C rows F4, F10, F12 (#4700) | SPEC-TABLES §3.4 (load step 9) | **ADDED** `rr_fixed_zero_records` |
| FL-40 | An assert leg's write checks are debug asserts and nothing else | Dart `carry_required_test` (#4670); Rust `to_json` (#4673) | SPEC.md §5.1 | the `write-refusal` surfaces (assert legs answer in their debug build) |

## Counts

- 40 rules.
- 13 had all three before this branch: FL-01, 05, 08, 09, 10, 19, 21, 25, 27, 30, 31, 37 and 40 (FL-05, 09 and 27 through the open #4688; FL-19 through #4689; FL-37 through #4676).
- 11 spec sentences added (FL-13, 14, 15, 16, 17, 18 twice, 20, 22, 23, 38).
- 20 shared rows added: 19 manifest rows (FL-07, 12, 13, 20, 23, 26, 39) and one text-form vector (FL-24).
- Two engine fixes the new rows found: the Go reference text reader did not count a placed map's or union's object toward the depth cap (FL-12), and the C++ text form read an ill-formed comment (FL-24). The JS text writer wrote a float32 past its range (FL-23).

## Rules still without a shared harness row

FL-02, 03, 04, 06, 11, 14, 15, 16, 17, 18, 22, 28, 29 and 36 have no harness
surface to ask; FL-33, 34, 35 and 38 have a shared per-port table but no
harness row. Each needs a harness surface the driver contract does not have
(a reused report, a value handed in, a caller bound or capacity, a sender
without an announcement, a strict announcement, a typed-impossible value) or
a registered root it does not have. FL-32 is outside the suite by design.

## Port failures on the added rows

Run: `harness run --manifest <the added rows> --only <lang>` for all 18 legs on
main at the branch base, plus this branch's Go engine and JS fixes.

| row | failing legs | reason |
|---|---|---|
| `rr-depth-placed-union-map-129` | C++, C, C#, C# (unity), Goose, Java, JS, Lobster, MOO, Rust, Squirrel | the JSON read does not count a placed union's object and a map's object toward the depth cap |
| `rr-depth-placed-keyed-map-129` | C#, C# (unity), Goose, Java, JS, Lobster, MOO, Rust, Squirrel | the JSON read does not count a placed map's object toward the depth cap |
| `rr-num-negative-zero-long-token` | C++ | fixed by #4651 (open) |
| `rr_wref_f32_past_range`, `rr_fref_f32_past_range`, `rr_jref_f32_past_range` | Dart, Goose | the writers test the double for finiteness, not the float32 it rounds to |

Every other (leg, row) pair passes; Dart answers the four `mapdemo` rows ABSENT.

Rows from open PRs land with them: `skipped-label-*` (#4676), `TestRulingsR2R5R9PortRows` (#4680), `IdCapSchema` (#4678), `AnnounceSchema` (#4685), the `sp-*` rows (#4688, #4689); `rr-num-negative-zero-long-token` is green on C++ once #4651 lands.
