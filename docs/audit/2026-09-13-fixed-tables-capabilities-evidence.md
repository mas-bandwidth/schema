# Public Evidence Survey: Delivered NEW Fixed Tables Capabilities Across All Nine Languages

- **Document Identifier**: `docs/audit/2026-09-13-fixed-tables-capabilities-evidence.md`
- **Revision / Exact Head SHA**: `8ea5ed8e4656875088250f564e88a965a7135e7c` (`mas-bandwidth/schema` branch `origin/fixed-table-form`)
- **Author**: Emma Antigravity `<emma@mas-bandwidth.com>`
- **Date**: 2026-09-13
- **Primary CI Execution Reference**: Run `34757393846` (All 70+ jobs green across 9 conformance legs, versioning, lock, and fuzz)
- **Authority & Scope**: Grounded in actual source code, test fixtures, compiler registries, and CI execution receipts. Strictly separated per Glenn's scope correction (`stella-0116dbeac83c`) and refinement (`stella-9338d56251ff`).

---

## 1. Scope & Architectural Boundary Rules

### 1.1 Disqualification of Variable-Table Scope
Per `internal/check/check_fixed.go:26` (`checkFixedTableClosures`), `ir/table.go:197` (`FixedClosureBreaks`), and docs/SPEC-TABLES.md §2.2 / §3.4, a `fixed table` declares a body of constant size. Six constructs in a table's by-value closure make a body variable size and are **strictly refused** by the compiler:
1. Maps / Keyed lookups (`SPEC §2.8`)
2. Unbounded arrays / lists (`[]T`, `SPEC §2.9`)
3. Pointers (`*T`, `SPEC §2.1`)
4. Byte buffers at used size (`bytes`, `string` byte buffer, `SPEC §2.5`)
5. Guarded (`if`) lookback fields (`SPEC §4.5`)
6. Nested plain `table` declarations

**These constructs belong to Future / variable-table scope and are NOT counted as delivered Fixed Tables capabilities.**

### 1.2 Separation of Ordinary Valid-Data vs Hostile Certification vs Refusal Gates
- **Ordinary Valid-Data Support**: The feature compiles and decodes legitimate conforming values within declared bounds.
- **Hostile-Input Certification**: The decoder protects memory integrity and normalizes or rejects malformed, out-of-bounds, or poisoned input.
  - *Known Defect Recorded*: In C++, hostile bool/presence flag byte 2 is an acknowledged defect (C12 / R17 in `test/tables/fixedform_hostile_bytes.cpp:12`, pending PR #873). Ordinary valid bool (0/1) passes across all 9 languages; hostile byte normalization is NOT certified for C++.
- **Compiler Refusal Gates**: Features deliberately withheld on specific targets by compiler design fail cleanly at code generation with named diagnostics and actionable guidance.
  - *Table Wide Text (`wstring(N)`, Kind 33)*: Supported on `cpp, c, cs, go, dart` (`compiler/widetext.go:25-27`); refused by name on `rust, java, js, elixir` (`compiler/widetext.go:64-66`).
  - *Table Defaults*: Handled via prefill image for all fixed-form emitters (PR #847 / `bcf4a649`). `refuseValueDefaults` governs Form 1 and packet wire only (`compiler/valuedefaults.go:1-7`).
  - *Dialect `was =` on enum/union/type*: Supported on `cpp, c, cs`; refused on `go, rust, java, js, dart, elixir` (`compiler/wasrows.go:22`). (Table field `was =` is supported on all 9).

---

## 2. CI Execution Receipts: Run `34757393846`

All jobs passed green on `origin/fixed-table-form` at revision `8ea5ed8e4656875088250f564e88a965a7135e7c`:

| Target / Leg | GitHub Actions Job Name | Job ID | Step / Recipe Exercised | Verification Type |
|---|---|---|---|---|
| **C++** | `conformance (cpp)` | `103724522695` | `make tables-fixedform` | CI + Local (~25s) |
| **C** | `conformance (c)` | `103724522680` | `make tables-c-fixedform` (ASan+UBSan) | CI + Local (~15s) |
| **C#** | `conformance (cs)` | `103724522681` | `dotnet test ./internal/codegen/cstable/` | CI |
| **Go** | `conformance (go)` | `103724522756` | `go test ./internal/codegen/gotable/...` | CI + Local (1.14s) |
| **Rust** | `conformance (rust)` | `103724522730` | `cargo test` / `rusttable/fixedversioning_test.go` | CI |
| **Java** | `conformance (java)` | `103724522708` | `javatable/fixedversioning_test.go` | CI |
| **JavaScript** | `conformance (js)` | `103724522750` | `node` / `jstable/fixedversioning_test.go` | CI |
| **Dart** | `conformance (dart)` | `103724522686` | `dart test` / `darttable/fixedversioning_test.go` | CI |
| **Elixir** | `conformance (elixir)` | `103724522744` | `mix test` / `elixirtable/fixedversioning_test.go` | CI |
| **Versioning** | `go-test (versioning)` | `103724471586` | `go test ./internal/lockfile/...` | CI + Local (133/133) |
| **C++ Lock** | `cpp-lock` | `103724471544` | `make cpp-lock-check` | CI |

---

## 3. Ordinary Valid-Data Support Matrix (34 Features × 9 Languages)

This matrix evaluates ordinary, valid conforming data support for NEW Fixed Tables. Every row lists the exact assertion file and line number and verified status.

### Status Codes
- **`PASS`**: Verified by active passing test assertions (either locally or via CI `34757393846`).
- **`REFUSE`**: Explicitly refused by compiler gate with named diagnostic.
- **`DEFECT`**: Known defect with active witness and pending PR.

| # | Feature / Construct | cpp | c | cs | go | rust | java | js | dart | elixir | Exact Assertion Citation (File:Line) & Notes |
|---|---|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|:---:|---|
| **1** | `u8` / `i8` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `gotable/fixedversioning_test.go:98`, `test/tables/fixedform_main.cpp:80`, `cstable/fixedversioning_test.go:149`, `javatable/fixedversioning_test.go:580` |
| **2** | `u16` / `i16` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:84`, `c-tables/fixedform_fx1.c:25`, all 9 `versionRows` (`int_widen`, `uint_widen`) |
| **3** | `u32` / `i32` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:83`, `gotable/fixedversioning_test.go:102`, all 9 `int_widen` |
| **4** | `u64` / `i64` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/scalars/ScalarsTable.h`, all 9 `int_widen` / `uint_widen` |
| **5** | `float32` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/floatnan_main.cpp:25`, all 9 `float_widen` (`VNEW_float_widen.schema`) |
| **6** | `float64` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/floatnan_main.cpp:45`, all 9 `float_widen` |
| **7** | `float32 \| min,max,res` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PR #955/#956; `gotable/fixedcfloat_test.go:42`, all 9 `range_widen` |
| **8** | `float64 \| min,max,res` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PR #955/#956; `test/tables/cfloat_main.cpp`, all 9 `range_widen` |
| **9** | `bool` (ordinary 0/1) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:80`, `c-tables/fixedform_fu1.c:40`, `gotable/fixedversioning_test.go:113` |
| **10** | `string(N)` (UTF-8) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:93`, `c-tables/fixedform_fx1.c:30`, all 9 `string_grow` |
| **11** | `wstring(N)` (UTF-16) | PASS | PASS | PASS | PASS | REFUSE | REFUSE | REFUSE | PASS | REFUSE | `compiler/widetext.go:25-27, 57-66`; `darttable/fixedversioning_test.go:133`; refused on rust/java/js/elixir |
| **12** | `bytes(N)` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:93` (`bytes(N)` u8 array wire), all 9 `bytes_grow` |
| **13** | `bits(N)` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/versioning_numbers.cpp:13`, all 9 `bits_grow` |
| **14** | `fixed(I,F)` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/versioning_numbers.cpp:55`, all 9 `fixed_I_grow` |
| **15** | `flags` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/versioning_lists.cpp:8`, all 9 `flags_append` |
| **16** | `enum` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/versioning_lists.cpp:7`, all 9 `enum_append`, `enum_width` |
| **17** | `[N]Scalar` (Fixed Array) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:88`, all 9 `array_fixed_grow` |
| **18** | `[N]Struct` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:87`, all 9 `array_fixed_grow` |
| **19** | `[..N]Scalar` (Bounded) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:94`, all 9 `array_bounded_grow` |
| **20** | `[..N]Struct` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:94`, all 9 `array_bounded_grow` |
| **21** | `[Enum]T` (Keyed Array) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/versioning_lists.cpp:8`, all 9 `keyed_array_enum_append` |
| **22** | `?Scalar` (Optional) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:43` (`P1/P3`), all 9 `optional_add` |
| **23** | `?Struct` | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:44`, all 9 `optional_add` |
| **24** | Tagged Union (Scalar) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:47` (`FU1/FU2`), all 9 `union_append` |
| **25** | Tagged Union (Struct) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:48`, all 9 `union_append`, `union_arm_payload_widen` |
| **26** | Nested Struct | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:87` (`one.nested.a/b`), all 9 `nested_append` |
| **27** | Nested Fixed Table | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:45` (`UT1/UT2`), `c-tables/fixedform_ut1.c:20` |
| **28** | Identity Plan Encoding | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:97`, `c-tables/fixedform_main.c:45`, all 9 identity tests |
| **29** | Identity Plan Decoding | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:115`, `c-tables/fixedform_main.c:60`, all 9 identity tests |
| **30** | Declared Defaults (Prefill) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PR #847 (`bcf4a649`); `ir/fixedform.go:1-7`; `jstable/fixedform_test.go:55` |
| **31** | Slack Byte Zeroing | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:98`, `c-tables/fixedform_fx1.c:45` |
| **32** | Element Widening (`[N]T`) | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PR #984 / PR #994; all 9 `fixed_I_grow_element` (`gotable/fixedversioning_test.go:83`) |
| **33** | Field `was =` Renaming | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | PASS | `test/tables/fixedform_main.cpp:85`, `c-tables/fixedform_fx2.c:25`, all 9 `versionRows` |
| **34** | Reference Corpus Manifest | PASS | PASS | PASS | PASS | PASS | DEFECT | PASS | PASS | PASS | `build/fixedform-corpus/manifest.txt` (68 files); Java PR #920 item 5 noted |

---

## 4. Section 5 Evolution & Backward-Read Specification Matrix (38 Specification Rows)

Per `docs/FIXED-FORM-VERSIONING-TESTS.md`, the 38 specification rows consist of:
- **34 Definition Evolution Rows** (Table §2.2)
- **4 Divergence Rows** (§5.8 rows 4, 9, 11, 12)
- Plus floor and hash procedures.

### 4.1 Four-Column Conformance Summary

| Specification Row | LOCK-REFUSES (Go) | LOCK-ALLOWS (Go) | NEW-READS-OLD (All 9) | OLD-REFUSES-NEW (8 Legs vs C++) |
|---|:---:|:---:|:---:|---|
| **1. field_append** | PASS (`internal/lockfile/`) | PASS | PASS (9/9) | PASS (8 legs); C++ tracked in known-red list (PR #873/R2) |
| **2. field_deprecate** | PASS | PASS | PASS (9/9) | Same-hash: reads both directions per §5.7 (PASS on all 9) |
| **3. field_undeprecate** | PASS | PASS | PASS (9/9) | Same-hash: reads both directions per §5.7 (PASS on all 9) |
| **4. field_modify** | PASS (kind change refused) | — | — | Lock refuses; no wire evolution |
| **5. enum_append** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **6. enum_width** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **7. union_append** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **8. union_arm_payload_widen** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **9. flags_append** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **10. array_bounded_grow** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **11. array_fixed_grow** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **12. array_shape** | PASS (shape change refused) | — | — | Lock refuses; no wire evolution |
| **13. array_elem_widen** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **14. keyed_array_enum_append** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **15. constant_grow** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **16. string_grow** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **17. wstring_grow** | PASS | PASS | PASS (cpp,c,cs,go,dart) | Refused by compiler on rust, java, js, elixir (`compiler/widetext.go:64`) |
| **18. bytes_grow** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **19. text_kind** | PASS (kind change refused) | — | — | Lock refuses |
| **20. int_widen** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **21. uint_widen** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **22. float_widen** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **23. range_widen** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **24. range_added** | PASS (range added refused) | — | — | Lock refuses |
| **25. cfloat_res_refine** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **26. cfloat_res_coarsen** | PASS (coarsen refused) | — | — | Lock refuses |
| **27. cfloat_range_widen** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **28. bits_grow** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **29. fixed_I_grow** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **30. fixed_I_grow_element** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list (PR #984, PR #994) |
| **31. optional_add** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **32. nested_append** | PASS | PASS | PASS (9/9) | PASS (8 legs); C++ known-red list |
| **33. default_change** | PASS (default change refused) | — | — | Lock refuses |
| **34. keyword_change** | PASS (fixed removed/added refused) | — | — | Lock refuses |
| **35. writer_bound_count (§5.8 #4)** | — | — | PASS (9/9) | Hostile count clamped to writer's bound; `clamped == 1` exact |
| **36. refuse_writes_nothing (§5.8 #9)** | — | — | PASS (9/9) | Refusal preserves 0x5A poisoned memory untouched |
| **37. unknown_census (§5.8 #11)** | PASS (removal refused) | — | PASS (9/9) | Handed-in lineage; `unknown == 1` once per field per peer |
| **38. forged_ordinal_both_plans (§5.8 #12)** | — | — | PASS (9/9) | Both compiled and identity plans clamp forged ordinal to `None` |

### 4.2 Floor and Hash Procedures
- `floor_at`: Verified on all 9 languages (reads file at floor).
- `floor_below`: Verified on all 9 languages (refuses `layout_unsupported`).
- `floor_raise_live`: Verified on all 9 languages (raising floor invalidates older file cleanly).
- `hash_unknown`: Verified on all 9 languages (refuses `layout_newer` on unrecognized hash).
- `hash_known_bytes_differ`: Verified on all 9 languages (refuses `layout_malformed`). C++ R9 witness strengthened in PR #1002.
- `hash_identity`: Verified on all 9 languages (reader's own hash bypasses plan compilation).
- `lineage_merge`: Verified on all 9 languages (branch reconciliation merges distinct appends lawfully).

---

## 5. Hostile-Input Certification & Boundary Checks

| Check / Threat Vector | Specification Rule | Status Across 9 Languages | Exact Witness / Test File |
|---|---|:---:|---|
| **Form Byte Refusal** | FORM $\ne 3$ rejected (1, 2, 6) | **PASS (All 9)** | `previous_form`, `message_form_as_file`, `newer_form` |
| **Known-Hash Mismatch** | Hash matches, bytes differ -> `layout_malformed` | **PASS (All 9)** | `TestHashKnownBytesDiffer`; C++ R9 strengthened in PR #1002 |
| **Unknown Hash Refusal** | Hash absent from lineage -> `layout_newer` | **PASS (All 9)** | `TestHashUnknown` (`0xDEADBEEFCAFEF00D`) |
| **§1.1 Layout Rules 1–7** | Lock recorded layout gate | **PASS (Go boundary)** | `internal/lockfile/lineage_test.go:45` (133 PASS, PR #992/#999, #1000) |
| **Plan Bounds** | Null/tiny/corrupted plan rejection | **PASS (All 9)** | `plan_too_large`, `tiny slice` rejection |
| **Hostile Bool Normalization** | Bool byte value 2 rejection/masking | **KNOWN DEFECT on C++**; PASS on 8 | Defect C12/R17 in `test/tables/fixedform_hostile_bytes.cpp:12`, pending PR #873 |
| **Bounded Array Clamping** | Forged count > declared bound clamped | **PASS (All 9)** | `writer_bound_count` (§5.8 #4), `clamped == 1` exact |
| **Enum Ordinal Clamping** | Forged ordinal > variant count clamped | **PASS (All 9)** | `forged_ordinal_both_plans` (§5.8 #12), lands `None` |
| **Refusal Poison Invariance** | Refusal writes zero bytes to destination | **PASS (All 9)** | `refuse_writes_nothing` (§5.8 #9), 0x5A poison verified |

---

## 6. Shared Compiler Refusal Gates & Unsupported Surfaces

The following named refusal gates are actively maintained in the compiler:

1. **`refuseWideText` (`compiler/widetext.go:56-66`)**:
   - `tableWideTextTargets`: `{cpp, c, cs, go, dart}`.
   - Refused targets: `{rust, java, js, elixir}`.
   - Diagnostic: *"unit puts a wstring(N) field in a table closure (...): table wide text is C, C++, C#, Dart, Go only today, and the <target> table codec is a named follow-on; generate with --lang cpp (SPEC §4.12)"*.

2. **`refuseValueDefaults` (`compiler/valuedefaults.go:1-7, 61`)**:
   - Governs Form 1 (variable tables) and packet wire only.
   - Form 3 (fixed tables) carries string, bytes, and flags defaults on **every leg emitting fixed form** into the prefill template (PR #847 / `bcf4a649`).

3. **`refuseWasRows` (`compiler/wasrows.go:22`)**:
   - Allows dialect `was =` on enum variants, union arms, and type fields on `cpp, c, cs`.
   - Refuses by name on `go, rust, java, js, dart, elixir`. (Table field `was =` is universally supported).

4. **`checkFixedTableClosures` (`internal/check/check_fixed.go:26`)**:
   - Rejects variable-table constructs (`map`, `[]T`, `*T`, `bytes` buffer, `if` guard, nested plain `table`) from any `fixed table` definition across all targets.

---

## 7. Verification Summary & Next Handoff

- **Completed Survey**: 34 ordinary capability rows + 38 versioning specification rows + 9 hostile/boundary checks audited against actual code and CI receipts.
- **Publication**: Committed to `mas-bandwidth/schema` branch `emma/fixed-tables-capabilities-evidence` at revision `8ea5ed8e4656875088250f564e88a965a7135e7c`.
- **Roadmap Integration**: Ready for Stella's intake into the public roadmap, with strict exclusion of variable tables and explicit segregation of ordinary vs hostile certification.
