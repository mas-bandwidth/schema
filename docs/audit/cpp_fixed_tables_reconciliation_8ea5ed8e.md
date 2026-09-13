# C++ Leg Fixed Tables Reconciliation (Schema #898)

- **Audit Baseline**: Rowan's Group A Opus audit at `b7ab66a88b6023f4016ef577fd163b84e45e692e` ([comment 5648020292](https://github.com/mas-bandwidth/schema/issues/898#issuecomment-5648020292)).
- **Integration Target**: `mas-bandwidth/schema` branch `fixed-table-form` at HEAD `8ea5ed8e4656875088250f564e88a965a7135e7c` (head of draft PR #836). Note: `origin/main` is at `c0b1d83c` awaiting final landing.
- **Rulings Applied**: Glenn's reconciliation rulings in [comment 5650150766](https://github.com/mas-bandwidth/schema/issues/898#issuecomment-5650150766), R2/S6 ruling (comment 5651586566), and R9 witness strengthening ([commit 070dcb99](https://github.com/mas-bandwidth/schema/commit/070dcb99cd12ca3293b84c9f74887fca17eacce6) in PR #1002).
- **Machine-Readable Files**:
  * TSV: `/Users/glenn/emma-working/scratch/cpp_fixed_tables_reconciled_92rows.tsv`
  * JSON: `/Users/glenn/emma-working/scratch/cpp_fixed_tables_reconciled_92rows.json`
  * S3 Receipt: `/Users/glenn/emma-working/scratch/schema-retirement-idempotence-receipt.json`

---

## 1. Mutually Exclusive Status Partition (Exactly 92 Keys)

Every key belongs to exactly one category ($1 + 55 + 18 + 17 + 1 = 92$):

| Status Class | Definition | C++ Leg (86) | Shared `S` (6) | Total (92) |
|---|---|---|---|---|
| **`verified`** | Independently proven with semantic proof (anti-mutation, poison verification, mutant controls) | 1 (`R9`) | 0 | **1** |
| **`implemented-asserted`** | Positive assertions present and passing in gate, without mutant/trap verification | 51 | 4 (`S1, S2, S4, S5`) | **55** |
| **`weak`** | Partial assertion, loose comparison, macro dependency, or known pending defect/RED | 17 | 1 (`S3`) | **18** |
| **`owed`** | Probe or harness absent, or unmerged PR | 16 | 1 (`S6`) | **17** |
| **`inapplicable`** | Retired or superseded by ruling | 1 (`C6`) | 0 | **1** |
| **Sum** | | **86** | **6** | **92** |

### Status Class Definitions & Upgrades
1. **`R9` (`verified`)**: Narrowed semantic coverage of the seven known-hash corruption cases in `hash_known_bytes_differ` (`test/tables/versioning_numbers.cpp:1149-1172`). Commit `070dcb99` (PR #1002) pre-poisons destination with `0xA5` / `101, 202, 303`, asserts `!r.malformed`, `reason == vnew_floor::layout_malformed`, zero report counters, and asserts `std::memcmp(&back, &fresh, sizeof(back)) == 0`. All 3 mutant build/runs failed the strengthened witness.
2. **`S3` (`weak`)**: Retained as `weak` (partial). Empirical test confirms same-reason retirement preserves exact bytes and mtime (`rewrote: false`), but a changed reason silently overwrites the reason in the lockfile (`rewrote: true`) and modifies mtime. Zero unit tests for `Retire`.
3. **`R5` (`owed`)**: Empty-L reservation verified in `ir/` (`TestTableFixedDefinitionsDigestLReservedUntilALimitExists`), but range/Q/flags ordering and runtime consumption on the legs remain unasserted on C++. Retained as `owed`.
4. **`L1-L7` (`implemented-asserted`)**: At runtime (§5.6), layout walks are retired; C++ runtime maps known-hash mismatch to `R9` and unknown to `R8`/`R10`. The 7 shared layout rules are verified in `internal/lockfile` (PR #999/#1000). The C++ row in `fixedform_main.cpp:419` asserts `layout_malformed` for 12 breaks.

---

## 2. Gate Execution Receipts on `8ea5ed8e`

Executed in `/Users/glenn/emma-working/scratch/schema-review-worktree`:

| Gate | Command | Duration | Result | Observations |
|---|---|---|---|---|
| `G-CPP` | `make tables-fixedform` | ~25s | **exit 0** | `manifest: 68 lines, 68 corpus files, 24 distinct roots` (+2 lines from `fixed_I_grow_element`). `versioning numbers: 0 RED, 0 failure(s)`. `versioning lists: green`. `fixed form: versioning conformance green`. Hostile bytes: 2 pre-existing REDs by design (`bool_byte_two`, `present_byte_two`). |
| `G-LOCK` | `go test ./internal/lockfile/ -count=1` | 1.140s | **PASS** | 133 PASS / 0 SKIP. Includes `LOCK-L1` through `LOCK-L7` tests from PR #999/#1000. |
| `G-CPPLIN` | `go test ./internal/codegen/cpptable/ -run 'TestCompileLineageComesFromTheLock\|TestCompileFromLockIgnoresFixturePeerConvention'` | 0.253s | **PASS** | 2 PASS / 0 SKIP. |
| `G-COMP` | `go test ./compiler/ -run 'TestFixedRecord\|TestWalkedElementArray\|TestFlatElementArray\|TestDeclaredFixedTable'` | 0.335s | **PASS** | 9 PASS / 0 SKIP. |

---

## 3. S3 Retire Idempotence Empirical Receipt

File: `/Users/glenn/emma-working/scratch/schema-retirement-idempotence-receipt.json`

```json
{
  "target": "Floored",
  "step1_initial_retire": {
    "command": "schema lock --verbose --retire=Floored --reason=\"reason A\" VNEW_floor.schema",
    "stdout": "retired Floored in /Users/glenn/emma-working/scratch/test_retire/schema.lock",
    "exit_code": 0,
    "rewrote": true,
    "bytes_changed": true
  },
  "step2_same_reason_idempotence": {
    "command": "schema lock --verbose --retire=Floored --reason=\"reason A\" VNEW_floor.schema",
    "stdout": "Floored is already retired in /Users/glenn/emma-working/scratch/test_retire/schema.lock",
    "exit_code": 0,
    "is_already_retired": true,
    "same_bytes": true,
    "mtime_preserved": true
  },
  "step3_changed_reason_overwrite": {
    "command": "schema lock --verbose --retire=Floored --reason=\"reason B\" VNEW_floor.schema",
    "stdout": "retired Floored in /Users/glenn/emma-working/scratch/test_retire/schema.lock",
    "exit_code": 0,
    "rewrote_reported": true,
    "bytes_changed": true,
    "mtime_changed": true
  },
  "verdict": "CONFIRMED: same-reason retirement is strictly idempotent (preserves exact bytes and mtime; reports \"already retired\"); changed-reason overwrites the existing retirement reason, rewriting the file with new mtime. S3 status remains WEAK / PARTIAL."
}
```

---

## 4. Complete 92-Row Reconciled Inventory

| Leg | Item | Reconciled Status | Baseline Status | Evidence Path | Gate / Receipt | Reconciled Notes |
|---|---|---|---|---|---|---|
| cpp | L1-L7 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:419 | G-CPP | 12 breaks under layout_malformed; runtime walk retired §5.6; shared LOCK checks verified in lockfile |
| cpp | F1 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:305 | G-CPP | previous_form refusal |
| cpp | F2 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:305 | G-CPP | message_form_as_file refusal |
| cpp | F3 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:305 | G-CPP | newer_form refusal |
| cpp | F4 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:526 | G-CPP | fewer bytes than header refuses layout_malformed |
| cpp | F5 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:322 | G-CPP | header hash never locked refuses layout_newer; runtime hash recompute retired §5.6 |
| cpp | F6 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:334 | G-CPP | newer hash refused before plan compiled |
| cpp | F7 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:526 | G-CPP | fewer bytes than header is layout_malformed |
| cpp | F8 | owed | owed | - | - | no ragged-tail case in test/tables/ |
| cpp | F9 | owed | owed | - | - | no batch_too_large case |
| cpp | F10 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1033 | G-CPP | no_layout refusal with destination preserved |
| cpp | F11 | weak | weak | test/tables/fixedform_main.cpp:334 | G-CPP | tiny[1] plan asserts layout_newer; plan_too_large unreachable on this path |
| cpp | F12 | owed | owed | - | - | no second layout under held hash |
| cpp | E2 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:124 | G-CPP | forward-read retired; new-reads-old defaulting covered |
| cpp | E3 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:2332 | G-CPP | widened counted per record |
| cpp | E4 | weak | weak | test/tables/fixedform_properties.cpp:1436 | G-CPP | one probe, not kind_mismatch pair table |
| cpp | E5 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:228 | G-CPP | ?T reads T |
| cpp | E6 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:122 | G-CPP | renamed field via was = |
| cpp | E7 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:190 | G-CPP | inserted union arm remapped by name |
| cpp | E8 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:206 | G-CPP | slack zeros, append/deprecate |
| cpp | E9 | owed | owed | - | - | duplicate appears in no assertion |
| cpp | C1 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1731 | G-CPP | count below zero clamps to 0 |
| cpp | C2 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1744 | G-CPP | count past Max clamps to Max |
| cpp | C3 | owed | owed | - | - | no forged text-length case |
| cpp | C4 | weak | weak | test/tables/fixedform_main.cpp:1383 | G-CPP | asserts r.malformed instead of text_ill_formed; PR #971 open |
| cpp | C5 | weak | weak | test/tables/fixedform_main.cpp:1422 | G-CPP | utf8 bytes, not wide code units |
| cpp | C6 | inapplicable | inapplicable | docs/FIXED-FORM-ALGORITHM.md §5.8 row 7 | - | superseded by R31 |
| cpp | C7 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1107 | G-CPP | range past max lands at max |
| cpp | C8 | weak | weak | test/tables/versioning_numbers.cpp:803 | G-CPP | bits(N) growth probed; F-shift and 2^N-1 ceiling unprobed |
| cpp | C9 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1196 | G-CPP | tag past arm count lands None |
| cpp | C10 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1214 | G-CPP | ordinal past top enum value lands None |
| cpp | C12 | weak | weak | test/tables/fixedform_hostile_bytes.cpp:93 | G-CPP | RAN AND RED by design (bool_byte_two UB) |
| cpp | C13 | weak | weak | test/tables/fixedform_hostile_bytes.cpp:115 | G-CPP | RAN AND RED by design (present_byte_two UB) |
| cpp | C14 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1239 | G-CPP | live count identity clamps |
| cpp | W1 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:632 | G-CPP | uninitialized body padding zeroed |
| cpp | W2 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1307 | G-CPP | present twin padding zeroed |
| cpp | W3 | owed | owed | - | - | no narrower-arm zero case |
| cpp | W4 | weak | weak | test/tables/fixedform_main.cpp:663 | G-CPP | asserts zeros where §5.3 says read slack unspecified |
| cpp | W5 | owed | owed | - | - | no -DNDEBUG run in gate |
| cpp | W6 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1863 | G-CPP | unguarded before guarded |
| cpp | W7 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:776 | G-CPP | two lanes second arm ordinal |
| cpp | W8 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:737 | G-CPP | bytes(N) buffer and length |
| cpp | W9 | weak | weak | test/tables/fixedform_main.cpp:1035 | G-CPP | proves refusal writes nothing, not prefill touches only unwritten |
| cpp | W10 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1609 | G-CPP | not partly compiled |
| cpp | W11 | weak | weak | test/tables/fixedform_main.cpp:700 | G-CPP | asserts plan row, not layout kind 14 |
| cpp | W12 | weak | weak | test/tables/fixedform_main.cpp:428 | G-CPP | hash includes count by construction, not isolated |
| cpp | W13 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:2153 | G-CPP | manifest dumped and verified |
| cpp | W14 | owed | owed | - | - | no struct offsetof comparison |
| cpp | W15 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:393 | G-CPP | layout refusal sets/counts nothing |
| cpp | W16 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1902 | G-CPP | guarded entry of nested union |
| cpp | P1 | implemented-asserted | implemented-asserted | test/tables/fixedform_properties.cpp:517 | G-CPP | identity and compiled read compared |
| cpp | P2 | implemented-asserted | implemented-asserted | test/tables/fixedform_properties.cpp:455 | G-CPP | save/load/save property |
| cpp | P3 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:574 | G-CPP | fuzz refusal never damage |
| cpp | P4 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:259 | G-CPP | wrong plan negative control |
| cpp | R1 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:958 | G-CPP | floor at lineage index 1 |
| cpp | R2 | weak | weak | internal/codegen/cpptable/fixedform.go:395 | G-CPP | backend adds 8; interim reference path per Glenn ruling |
| cpp | R3 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:973 | G-CPP | floor below refuses layout_unsupported |
| cpp | R4 | weak | weak | ir/fixedform.go:650 | G-CPP | digest built in IR, legs compare indices |
| cpp | R5 | owed | owed | ir/fixedform.go:599 | ir/ | empty-L reserved per §5.2; range/Q/flags and leg consumption owed |
| cpp | R6 | owed | owed | compiler/fixedrecordsize_test.go:186 | G-COMP | compiler half green in S5; leg assertion owed |
| cpp | R7 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:1010 | G-CPP | hash selects identity plan |
| cpp | R8 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:158 | G-CPP | layout_newer before any record |
| cpp | R9 | verified | implemented-asserted | test/tables/versioning_numbers.cpp:1149 | G-CPP | PR #1002 / 070dcb99: semantic proof of 7 corruption cases against destination mutation and counter leakage |
| cpp | R10 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:281 | G-CPP | newer hash is layout_newer by name |
| cpp | R11 | owed | owed | - | - | no writer_bound_count probe |
| cpp | R12 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1035 | G-CPP | refusal writes nothing (poison verified) |
| cpp | R13 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:393 | G-CPP | layout refusal sets no counters |
| cpp | R14 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1609 | G-CPP | entry past root.size refuses layout_record_too_large |
| cpp | R15 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:158 | G-CPP | 14 rows OLD_REFUSES_NEW |
| cpp | R16 | weak | weak | test/tables/fixedform_main.cpp:130 | G-CPP | unknown counted per record |
| cpp | R17 | weak | weak | test/tables/fixedform_hostile_bytes.cpp:93 | G-CPP | bool byte 2 UB (same as C12) |
| cpp | R18 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1151 | G-CPP | bounds whole set |
| cpp | R19 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:206 | G-CPP | bound growth moves no counter |
| cpp | R20 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:123 | G-CPP | missing field takes default |
| cpp | R21 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:2317 | G-CPP | NaN quiet bit preserved |
| cpp | R22 | owed | owed | internal/lockfile/lockclosure_test.go | - | asserted in lockfile, not run on C++ leg |
| cpp | R23 | implemented-asserted | implemented-asserted | internal/codegen/cpptable/fixedruntime.go:1320 | G-CPP | struct layout matches member for member |
| cpp | R24 | owed | owed | - | - | text_ill_formed absent; PR #971 open |
| cpp | R25 | weak | weak | test/tables/fixedform_main.cpp:334 | G-CPP | tiny plan asserts layout_newer, not plan_too_large |
| cpp | R26 | owed | owed | - | - | unbuildable lineage entry unasserted |
| cpp | R27 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:2142 | G-CPP | manifest lines equal files |
| cpp | R28 | weak | weak | internal/codegen/cpptable/fixedruntime.go:127 | G-CPP | guard2/arg2/argw2 two levels; PR #968 open |
| cpp | R29 | owed | owed | - | - | 65536 ceiling widening unasserted |
| cpp | R30 | owed | owed | - | - | cfloat rows unprobed |
| cpp | R31 | implemented-asserted | implemented-asserted | test/tables/fixedform_main.cpp:1007 | G-CPP | uint64_t arg and arg2 |
| cpp | R32 | implemented-asserted | implemented-asserted | test/tables/versioning_numbers.cpp:997 | G-CPP | floor raise live |
| - | S1 | implemented-asserted | implemented-asserted | internal/lockfile/monotone.go:426 | G-LOCK | monotone checks; 1.140s |
| - | S2 | implemented-asserted | implemented-asserted | internal/lockfile/lineage.go:155 | G-LOCK | lineage rollup/digest |
| - | S3 | weak | weak | cmd/schema/main.go:200 | CLI receipt | partial: same-reason idempotent; changed-reason overwrites; zero tests |
| - | S4 | implemented-asserted | implemented-asserted | internal/codegen/cpptable/lineage_test.go:16 | G-CPPLIN | reads from lock, not fixture convention; 0.253s |
| - | S5 | implemented-asserted | implemented-asserted | compiler/fixedrecordsize_test.go:72 | G-COMP | leaf bounds & wire ceiling; 0.335s |
| - | S6 | owed | owed | internal/codegen/cpptable/fixedform.go:395 | - | C++ backend adds 8; compiler handoff owed |

---

## 5. Smallest Next Genuinely Owed Packet to Land PR #836

1. **R2 / S6 Compiler Handoff Unification**:
   - In `compiler/lineage.go`, supply whole record size (`e.Record + 8`) for C++ just as done for the other 8 targets.
   - In `internal/codegen/cpptable/lineage.go:257`, remove the backend addition `8 + e.Record`.
2. **C12 / C13 Hostile Byte Normalisation**:
   - In C++ reader, normalise bool byte (`b != 0 ? 1 : 0`) and present byte before loading into `bool` to prevent Clang/UBSan trap on invalid value 2.
   - Clears the only 2 REDs in `fixedform_hostile_bytes.cpp`.
3. **C4 / R24 Text Content Refusal (`text_ill_formed`)**:
   - Land PR #971 to return `text_ill_formed` on invalid UTF-8 instead of generic `r.malformed`.
4. **F8 & F9 Boundary Probes**:
   - Add concise negative controls in `test/tables/fixedform_main.cpp` for ragged tail (< 1 record remaining past header) and batch count exceeds buffer bound.
5. **W3 Narrower Arm Zeroing**:
   - Assert in `fixedform_main.cpp` that writer zeroes trailing bytes when switching to a narrower union arm.
