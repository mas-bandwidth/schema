# The common packet — four-arm prototype challenge, "the fixed table reads backward, never forward"

One packet, four arms: two Common Lisp, two Haskell; two by Rowan, two by Stella. Every arm receives
EXACTLY the inputs below and nothing else. Assembled read-only; nothing here was posted anywhere.

## 0. What this is, said plainly

**This is a CALIBRATION on KNOWN SEMANTICS. It is NOT the unseen-bill experiment.** The slice's law is
already written down, already merged, and the two documents in §1 state it; an arm is not discovering a
contract, it is implementing one that is on the record. What the run measures is the cost and the shape of
a boundary implementation across two languages and two coordinators, and whether the checks in §3 can tell
a conforming reader from a plausible wrong one.

**Prior familiarity is disclosed to all four arms.** Rowan and Stella have both worked this slice and its
lane for days; the algorithm text, the fixture set and the refusal names are familiar to both. The arms are
told so. No arm is being presented as a cold read, and no result from this run may be reported as one.

**The bounded slice** (Stella's bound, accepted): ONE scalar widening case read forward in time by a newer
reader, plus the boundaries that sit immediately around it —
- the newer reader reading the older file where one definition widened (the scalar ladder, and an append);
- the older reader given the newer file, refusing it BY NAME;
- a known layout whose bytes disagree, and a per-record hash that disagrees, each refused by its own name;
- a modified or removed definition refused at commit, by name, with both values.

Everything else in Fixed Tables is out of scope for this run: no bitpacked form, no variable table, no
block or cooked projection, no message form, no lock-file tooling beyond what §3 C9 names.

**Timebox: ONE CLOCK of 30 minutes per arm, starting when the packet is delivered, with debugging
inside it; the result is FROZEN at the 30-minute mark** (see §6). The boundary above is sized for that.
**Every arm ships one executable, `challenge/run-checks`, so an adjudicator can run the held-out cases
without reading any arm's source** (see §7).

## 1. The repo, the commit, and the documents an arm may read

| fact | value |
|---|---|
| repo | `git@github-rowan:mas-bandwidth/schema.git` (`mas-bandwidth/schema`) |
| commit | `b7ab66a88b6023f4016ef577fd163b84e45e692e` |
| branch at that commit | `fixed-table-form` (lane head; merge of PR #986) |
| the bill | schema PR **#896**, MERGED 2026-09-11T01:51:43Z into `fixed-table-form`, head `rowan/bill-reads-backward`, title "docs: the bill, spec §21 and algorithm §5; the fixed table reads backward, never forward". It touched exactly three files: `docs/FIXED-FORM-ALGORITHM.md`, `docs/FIXED-FORM-BILL-READS-BACKWARD.md`, `docs/SPEC-TABLES.md` |

**THE TWO DOCUMENTS. An arm reads these and nothing else from the repo's docs or spec.**

| # | document | path at the commit | line range | lines | bytes | SHA-256 of the extracted range | SHA-256 of the whole file |
|---|---|---|---|---|---|---|---|
| D1 | ALGORITHM §5, "Evolution: the fixed table reads backward, never forward" | `docs/FIXED-FORM-ALGORITHM.md` | **358–1600** inclusive (§5 opens at 358; `## 6. The bounds` is line 1601 and is NOT included) | 1243 | 116906 | `277ceb8f21ab7ce6a3f0fa99b219148dcd51b0baf0be2dab4b648d00e17cd201` | `116326ca5904a37f9b1f29ff7e264589c3f695733a14c14e8faa5908f8667ec9` |
| D2 | SPEC-TABLES §21, "The fixed table reads backward, never forward" | `docs/SPEC-TABLES.md` | **16379–16448** inclusive (§21 to end of file) | 70 | 5163 | `67e2cb8ce0cb98af648710fbc4ffa2e27e6ebfde1ea3a0b34c8f18958440f6a8` | `c10389298d4d9f0e364b8c092b10567443435858e385aad6f2193f9310b968d0` |

**Documents: 2.** Both are handed to every arm as the extracted range, byte for byte, so the four arms read
identical bytes and not merely identical line numbers. The extracts sit beside this packet as
`ALGORITHM-5.md` and `SPEC-TABLES-21.md`; their SHA-256s are the two in the table above. Reproduce either
with `sed -n '<range>p' <path>` at the commit.

**Both documents are read WHOLE.** D1 is §5.1 through §5.9 — BASELINE, COMPILE, PLAN/MATCH/EMIT, LOAD, the
counters, the closure, what is retired, the tests, the reference's fourteen debts, and the forty-eight
rulings. D2 is §21.1 through §21.6. D1 is the algorithm; D2 is the spec's statement of the same law.

**An open item for Rowan, not settled here:** D1 and D2 both cite `docs/FIXED-FORM-BILL-READS-BACKWARD.md`
(the bill, the third file of PR #896) by section throughout — §12.3, §12.4, §12.5, §12.8, §13 and more. The
brief says "nothing else from the spec", so the bill is listed in §5 as NOT GIVEN. Recommendation: leave it
out. Every expectation in §3 is derived from D1/D2 text alone, deliberately, so an arm never needs the bill
to justify a check; the bill's sections read as citations an arm can ignore.

## 2. The corpus, and the split

**Where the fixtures are.** The versioning corpus's SOURCE OF TRUTH on this lane is the schema fixture set
in `test/tables/`, named `V<GEN>_<row>.schema` — one row per definition change, `VOLD_` the older
generation and `VNEW_` the widened one, plus `VMID_floor` and `VBRA_`/`VBRB_lineage_merge` for the three
rows that are not a simple pair. The Makefile's `SCHEMAS_VERSIONING` (lines 194–228) is the list the build
generates from. **The `.bin` files are NOT in the repo**: `make tables-fixedform-corpus` writes them and
`build/fixedform-corpus/manifest.txt` beside them, from these schemas, deterministically at this commit.

**The set is exactly** `test/tables/V{OLD,NEW,MID,BRA,BRB}_*.schema` — **57 files**.

**THE SPLIT RULE, verbatim and reproducible.** Sort the 57 paths ascending as byte strings (`LC_ALL=C
sort`). Number them 1..57. **Every third file is HELD OUT: index `i` is HELD OUT when `i mod 3 == 0`,
TRAINING otherwise.** That is 38 TRAINING and 19 HELD-OUT. Reproduce with:

```
LC_ALL=C ls test/tables/V{OLD,NEW,MID,BRA,BRB}_*.schema | LC_ALL=C sort | \
  awk '{n++; print (n%3==0 ? "HELDOUT" : "TRAIN"), $0}'
```

**A property of this split, checked and kept:** the sorted list puts the 27 `VNEW_` files at indices 4..30
and the 27 `VOLD_` files at 31..57, both blocks starting at an index `≡ 1 (mod 3)`. The held-out positions
therefore fall on the SAME row in both blocks, so **no OLD/NEW pair is split across the two sets** — 18
complete TRAINING pairs and 9 held-out rows. The one row the split does cut is `lineage_merge`, whose
`VBRA_`/`VBRB_` branches land in TRAINING while its `VOLD_`/`VNEW_` land held out; that row is therefore
NOT a usable training row, and §3 names no check on it.

### 2.1 TRAINING — an arm may read these (38 files)

| # | path | bytes | SHA-256 |
|---|---|---|---|
| 1 | `test/tables/VBRA_lineage_merge.schema` | 435 | `8bbf3118baf7cadfd28911d859f7409decb39ec1f76e93dbabc6005484e2ef85` |
| 2 | `test/tables/VBRB_lineage_merge.schema` | 435 | `755df0e7d9a9b1e4537b60b877b12eec7b94cce4c0411bd23e9fa96695939704` |
| 4 | `test/tables/VNEW_array_bounded_grow.schema` | 572 | `3d4ddc7cd5d6abdfc82ec7bf468edd72adce14aefb0dc64b3a6c8571d7b7bb88` |
| 5 | `test/tables/VNEW_array_elem_widen.schema` | 597 | `b293ad982c191c9383e930d929e16d9dcb046b7e0d9e37d7001b2f666816023f` |
| 7 | `test/tables/VNEW_bits_grow.schema` | 530 | `605b1219bd0839b022fcf9050a9188a9f5a45a7a435abee3a6503299d74439e7` |
| 8 | `test/tables/VNEW_bytes_grow.schema` | 535 | `0805d65f9bc4f759479f888e556301bd5601618f81d2c5b9edfd54c40c8590a0` |
| 10 | `test/tables/VNEW_enum_append.schema` | 787 | `670c4d2e73bd1dbc18820c7c2786c6c81c9a388b7a3cfa26d6096a3d704f0455` |
| 11 | `test/tables/VNEW_enum_width.schema` | 2426 | `3b32de8dca69da2b88c5a09064b520c0efff59127cd6ca78950398a0a155a31b` |
| 13 | `test/tables/VNEW_field_deprecate.schema` | 938 | `dc3db351d0b465efdeaa21555030b5454dce62fcd3020bfb29e4afd87d76c261` |
| 14 | `test/tables/VNEW_field_undeprecate.schema` | 784 | `df26202a9ea8dd196a040b30a3a1875b52b5ace629f2e2ac6765e52ad549ce7f` |
| 16 | `test/tables/VNEW_flags_append.schema` | 787 | `d2b3a9fbabf8e2eab93a916041042c4eabf465428d9ba82a450ca864f304a5d1` |
| 17 | `test/tables/VNEW_float_widen.schema` | 534 | `5d2131ef36b957afa5c6681397dac77e3dfc987bfbca4e0707c1f3b64e9e55fc` |
| 19 | `test/tables/VNEW_int_widen.schema` | 591 | `b915c96297eb52ee34ef3d890d96cfe48e9b1e5c404da89030fe5aa34c3bd0a9` |
| 20 | `test/tables/VNEW_keyed_array_enum_append.schema` | 903 | `e17a08a3f74ce16d2d4495557ebdb7ab99670b2378ea3ed5f9f0533c503828cb` |
| 22 | `test/tables/VNEW_nested_append.schema` | 939 | `9fe7d8907dde006ffbc7c2e79e6ae2d47ceaa959cdafadbee8fdf3a860e529da` |
| 23 | `test/tables/VNEW_optional_add.schema` | 634 | `31f152b97f63fd57e141f7acf6e3b24f5f3b451e81699ad5012961b755b75064` |
| 25 | `test/tables/VNEW_rename_without_was.schema` | 1051 | `d185a9a5ecfa3f22bb925495e7730549016031cebc6d55b1d6f32074a79cd372` |
| 26 | `test/tables/VNEW_string_grow.schema` | 540 | `ce6d7fbeb9f556df96595ee6f66c7cb5cfdedba75328c0c3ba9d7c4a5ce63a38` |
| 28 | `test/tables/VNEW_union_append.schema` | 885 | `cdf7b9e94a7cd4e6c1c7fb14cfe62acd1882f88b75c8bfc90eb97f4ce25822a9` |
| 29 | `test/tables/VNEW_union_arm_payload_widen.schema` | 1064 | `71450e7081c4ac04513f398a0219fd608bdcb911199b082f84a6fdc563427b92` |
| 31 | `test/tables/VOLD_array_bounded_grow.schema` | 560 | `b2658be31f09a5a8a2a23c1a30b6d3ffb65479f639af7763b0620d1cc5bdc2e4` |
| 32 | `test/tables/VOLD_array_elem_widen.schema` | 554 | `fee15369efe1d4e839f21154acbae11bd7d98bc6c6b733667dd0b901ff27689f` |
| 34 | `test/tables/VOLD_bits_grow.schema` | 528 | `6e856d17eb3fe690fd7efad41a6d40a3a97b7419ec8fcc9e3b34384e541936e1` |
| 35 | `test/tables/VOLD_bytes_grow.schema` | 533 | `040d73a8df21756cbcc31ba8d231443f2445a114450f8eda1498372350029984` |
| 37 | `test/tables/VOLD_enum_append.schema` | 852 | `75bd6a0faf21d3d042ed22ddb06ae193f94511bb2437052ae6e9128a7c7cca32` |
| 38 | `test/tables/VOLD_enum_width.schema` | 2354 | `5c523d9c2e0e9efcc5aad1e92b6ae2bd55f71fda88438944924960c6cf9acda6` |
| 40 | `test/tables/VOLD_field_deprecate.schema` | 772 | `b529b4674f084aecfa8a16f47d605c94705e69609f81637075ec60a8efdbe976` |
| 41 | `test/tables/VOLD_field_undeprecate.schema` | 797 | `d4b80b6dee6684ac630b8988a833992d98f163a74e42606b742b54837168bc58` |
| 43 | `test/tables/VOLD_flags_append.schema` | 782 | `6c747415d1fba85a4f30b68ac9171fa75efc98573c0b11ab6ed048e45c1ddeac` |
| 44 | `test/tables/VOLD_float_widen.schema` | 562 | `eba6f245a1ca875d965e0cd9d675bc3aaef3d782a163f004012fb551c17e9cc5` |
| 46 | `test/tables/VOLD_int_widen.schema` | 568 | `256acada2d2afcfe7254cb5ba30caf0a84ddc110f00508235d71a47803f908b6` |
| 47 | `test/tables/VOLD_keyed_array_enum_append.schema` | 893 | `b0f2a0a1aacdd830895d45db583af068a66b32f204d63f3b219cf8ebf19cd552` |
| 49 | `test/tables/VOLD_nested_append.schema` | 841 | `6a8dc76da0409d72562ae12f16cb44758efb59134b2a29e4de1416447f681edb` |
| 50 | `test/tables/VOLD_optional_add.schema` | 626 | `17895a5f3d431c11210d3531cf268d71e33713ad9ed08df9d50ee122f8794338` |
| 52 | `test/tables/VOLD_rename_without_was.schema` | 810 | `2c17ad8171ba41f887202be6fbd8997c3257f698f6c278f65fffcb9d73c18020` |
| 53 | `test/tables/VOLD_string_grow.schema` | 538 | `8ed885ca90738df6c3fb866dc5e441b153ba4b7a03947ee513c43ba72c6d8873` |
| 55 | `test/tables/VOLD_union_append.schema` | 842 | `21c844f56902835d38fd2f913ed2471ff5a32a3123b966831aff6667748acce3` |
| 56 | `test/tables/VOLD_union_arm_payload_widen.schema` | 893 | `2f10f666822a5b0f02adfa400e5c9606d32873770dc68c07cf6a5b313e32c0dc` |

### 2.2 HELD OUT — an arm may NOT read these (19 files)

Adjudication only. The held-out rows are `array_fixed_grow`, `constant_grow`, `field_append`,
`fixed_I_grow`, `floor` (all three generations), `lineage_merge` (base and merged), `range_widen`,
`uint_widen`, `wstring_grow` — the same boundary as the training rows, in definitions the arm has not seen:
another scalar ladder (`uint_widen`, zero-extending where the training case sign-extends), a bound grown, a
plain field appended, a range widened, and the floor's three cases.

| # | path | bytes | SHA-256 |
|---|---|---|---|
| 3 | `test/tables/VMID_floor.schema` | 429 | `a0fbe28b2c7d9003184c61dbd602b23f284d107caf168d7720603b509d87633f` |
| 6 | `test/tables/VNEW_array_fixed_grow.schema` | 1044 | `e8bebdcfb01267a16387dcdfc7d10fab140602761ade9108d0348ce14f187468` |
| 9 | `test/tables/VNEW_constant_grow.schema` | 592 | `028c6bc4487ccacb479a2d8fca74f080bb7020c4ecbb46497f30d672f91ae692` |
| 12 | `test/tables/VNEW_field_append.schema` | 920 | `17a482f45d2e8d97eda3944456a986bae819d8fd82133cdbfca95515ed1713db` |
| 15 | `test/tables/VNEW_fixed_I_grow.schema` | 611 | `260734d9576f7cad96bb7daa07f5e1625a46174750521cba50a26dc5c7ab7f01` |
| 18 | `test/tables/VNEW_floor.schema` | 463 | `cb4c0d5dfd1b2f3e99ebecc0636cb89d97cd897842815408fe962c4f2e83c07a` |
| 21 | `test/tables/VNEW_lineage_merge.schema` | 476 | `cc64ff3bb54e5ec255393e4570f130e36da4ccfe86e12fb6c94aa6a5bfb77e2e` |
| 24 | `test/tables/VNEW_range_widen.schema` | 558 | `4a4215772a4a7bde0b1976218e83e108b6f4b5f2fc6aeb35c002626344c61c80` |
| 27 | `test/tables/VNEW_uint_widen.schema` | 548 | `af728004c4b504f0891fbfd0a4a22d0ac0bc368da34397b67318fb8b7f04950d` |
| 30 | `test/tables/VNEW_wstring_grow.schema` | 545 | `762a777669229bd1bbda2f07657f98450e6a25630713254eca7955476f7980e3` |
| 33 | `test/tables/VOLD_array_fixed_grow.schema` | 1022 | `67b5c4941419f663c040ecd016e5da304064f5b2bde16fdc1e5181b7bfe216e4` |
| 36 | `test/tables/VOLD_constant_grow.schema` | 577 | `987304ca38f9438feb7f152d11dc21040f2ad720cb5929d23580d02c44750130` |
| 39 | `test/tables/VOLD_field_append.schema` | 759 | `431915c442c71ff9da76e2da9b8024b7f84b2afb335dbeb028ca65d3a6f8c4c6` |
| 42 | `test/tables/VOLD_fixed_I_grow.schema` | 563 | `69aa4cb0cce2e1505c09aece50ddaa131eed6ec52de4d2f53c9ded7a87b92ea4` |
| 45 | `test/tables/VOLD_floor.schema` | 425 | `dd60974ea4d8f030489b6d99a76083ae0a6fac2de63e2e1716105d99b9b331b4` |
| 48 | `test/tables/VOLD_lineage_merge.schema` | 405 | `0b107d02da4d755a796806464fc1263fa8ef92ab2b06888db5bd34ea61fbad99` |
| 51 | `test/tables/VOLD_range_widen.schema` | 564 | `963880f4b2dc27cdf117a9de1daaa8e80c3cd19255920aed8f93c38076db0bac` |
| 54 | `test/tables/VOLD_uint_widen.schema` | 533 | `0485d9a48aa713f733c818ea75523124a3149d7f0978c07f7abe28a14b5821d6` |
| 57 | `test/tables/VOLD_wstring_grow.schema` | 567 | `f87f447ca2a2d8e5ab78e5388107eff1e3875e410897e7014ecdd1fae6ab44b7` |

**Training fixtures: 38. Held-out fixtures: 19.**

### 2.3 The bounded slice's own fixtures, and the value oracle

Of the 18 complete TRAINING pairs, the checks in §3 name five rows:

| row | the one definition change | both files in TRAINING |
|---|---|---|
| `int_widen` | `v int16` → `v int32`; `lead uint32 = 1` and `trail uint32 = 2` bracket it | yes (indices 46, 19) |
| `nested_append` | `Vec {x,y,z}` → `Vec {x,y,z,w}`, `w int32 = 88`, inside `Lineage { v Vec, seq int32 }` | yes (49, 22) |
| `rename_without_was` | `a int32` → `b int32 \| was = "a"` — moves no layout byte and no digest byte | yes (52, 25) |
| `field_deprecate` | `{a,b,c}` → `{a, b deprecated, c}` — the slot stays in place | yes (40, 13) |
| `enum_append` | `Tier {Bronze,Silver,Gold}` → `+ Platinum`, with `seq int32` after the enum | yes (37, 10) |

**THE VALUE ORACLE IS THE CORPUS MANIFEST, NEVER THE DUMP AND NEVER THE SCHEMA DEFAULT** (D1 §5.9 #32,
#37). An arm builds the corpus at this commit — `make tables-fixedform-corpus` — and asserts the `values=`
of the manifest line for the file it read: `file=<name> row=<row> side=old|new|mid|a|b|none root=<Table>
records=<n> values=<field>=<value>[,...]`, `values=` last and running to the end of the line. The manifest
is derived from the pinned commit, so it is the same bytes for all four arms. Two numbers D1 states verbatim
and the packet therefore pins directly: **`int_widen`'s `lead` is `2863311530` and its `trail` is
`3149642683`** on the wire, while the schema says 1 and 2 — which is exactly why the manifest and not the
schema is the oracle. A field absent from a manifest line carries its schema default.

`test/tables/fixedform_dump.cpp`, `versioning_lists.cpp`, `versioning_numbers.cpp` and
`fixedform_properties.cpp` are **NOT readable by an arm** (§5). The manifest is. The fixture schemas in §2.1
are.

## 3. The observables and the checks

Every check below is derived from D1/D2 text alone and says HOW, so an arm can justify its expectation
without opening the reference. Twelve checks. **N = 12 on training.**

**The observables a reader must expose, on every read** (D1 §5.3's joint table, §5.4, §5.9 #15, #29):
`returns` (`n` or `-1`); `refused` (bool); `reason` (a NAME from D1 §5.3's condition table); `malformed`
(bool); `layout_hash` (the file's header hash, LAST on the report, zero on every path but the two layout
refusals); the counters `unknown`, `kind_mismatch`, `widened`, `clamped`; and the decoded value surface.
**`refused`+`reason` and `malformed` are NEVER both set.** A refusal by name writes NOT ONE destination
byte, the prefill included.

| # | check | input | expected |
|---|---|---|---|
| **C1** | identity read, own file | `new_int_widen.bin` read by the `VNEW_int_widen` build | reads: `returns ==` the manifest's `records=`, `lead == 2863311530`, `trail == 3149642683`, `v` == the manifest's value; `refused == false`, `malformed == false`, `layout_hash == 0`, **all four counters `0`** |
| **C2** | **THE SCALAR WIDENING — the slice's centre.** newer reads older | `old_int_widen.bin` (writer `v int16`) read by the `VNEW_int_widen` build (reader `v int32`) | reads: `lead == 2863311530` and `trail == 3149642683` exact; **`v` SIGN-EXTENDED exactly** — the OLD file's `-1`, `INT16_MIN` (`-32768`) and `INT16_MAX` (`32767`) land as `-1`, `-32768`, `32767` in `int32`, per the manifest's `values=`; **`widened == 1` per record, EXACTLY 1 and never `>= 1`**; `unknown == 0`, `kind_mismatch == 0`, `clamped == 0`; `refused == false`, `malformed == false` |
| **C3** | the reader's tail on an append | `old_nested_append.bin` (`Vec {x,y,z}`) read by the `VNEW_nested_append` build (`Vec {x,y,z,w}`), root `Lineage` | reads: `v.x`, `v.y`, `v.z`, `seq` exact per the manifest; **`v.w == 88`**, its declared default; **all four counters `0`** |
| **C4** | **OLD REFUSES NEW, BY NAME** | `new_int_widen.bin` read by the `VOLD_int_widen` build, destination poisoned `0x5A` first | `refused == true`, **`reason == layout_newer`**, `layout_hash ==` the file's header hash **and nothing else set**, `malformed == false`, `returns == -1`, all four counters `0`, and **every poisoned destination byte still `0x5A`** |
| **C5** | the hash, and only the hash, is the version | `old_rename_without_was.bin` and `new_rename_without_was.bin`, each read by BOTH builds — four reads | all four READ, none refuses, all four take the IDENTITY plan; the field paired by WIRE ID (`b was = "a"` against `a`) lands the same value in every direction; `seq` exact; all four counters `0` in all four reads |
| **C6** | a deprecation is not a version | `new_field_deprecate.bin` read by `VOLD_field_deprecate`, and `old_field_deprecate.bin` read by `VNEW_field_deprecate` | BOTH read; `a`, `b`, `c` exact per the manifest **in both directions** — `b` is still written and still read; `unknown == 0` on both, all four counters `0` on both |
| **C7** | a KNOWN hash whose layout bytes disagree | `old_int_widen.bin` with ONE byte of the layout region (`file+20 .. file+20+L`) flipped, the header hash at `file+8` untouched, read by `VNEW_int_widen` | `refused == true`, **`reason == layout_malformed`**, `malformed == false`, `returns == -1`, all counters `0`, nothing written |
| **C8** | the per-record hash gate runs BEFORE the prefill | `old_nested_append.bin` with one record's per-record hash forged, the header hash untouched, read by `VNEW_nested_append` with the destination poisoned `0x5A` | `refused == true`, **`reason == no_layout`**, `malformed == false`, `returns == -1`, all counters `0`, **every poisoned byte still `0x5A` — `Vec.w == 88` NOWHERE**, because the hash check precedes the prefill |
| **C9** | **a MODIFIED or REMOVED definition is refused at commit, by name, with both values** | the `int_widen` pair's text, edited six ways and run through the monotone check against the lock: (a) `v int16 → int8`; (b) `→ uint16`; (c) `→ float32`; (d) `IntWiden {lead, v, trail}` → `{lead, trail}`; (e) `→ {lead, trail, v}`; (f) `→ {lead, v2, v, trail}` | each REFUSED, naming the table `IntWiden`, the definition, the rule phrase and BOTH values: (a) `narrowed (int16 -> int8)`; (b) `signedness`; (c) `ladder`; (d) `field removed`; (e) `fields reordered`; (f) `field inserted not at the end`. And the widening ITSELF (`int16 → int32`) is ACCEPTED |
| **C10** | the two answers are never both set | `old_int_widen.bin` truncated to 19 bytes; and the same file with one stray byte appended (a ragged tail) | both: `refused == false`, `reason` untouched, **`malformed == true`**, `returns == -1`, all counters `0` |
| **C11** | nothing on the load path parses a layout or compiles a plan | structural, the arm's own reader | no plan-compiler entry point is reachable from the load path; every hash the runtime holds is a HANDED CONSTANT and the reader computes none from layout bytes, on the identity lane or anywhere else; the load path cannot fail for want of a plan. The arm states in prose which of D1 §5.9 #3's two conforming shapes it took (plans emitted as source, or built once at package initialization from the lock's bytes) |
| **C12** | an append the reader knows moves nothing | `old_enum_append.bin` (`Tier {Bronze,Silver,Gold}`) read by `VNEW_enum_append` (`+ Platinum`) | reads: the writer's ordinals land as the reader's (the writer's list is a PREFIX of the reader's), a `Gold` record reads `Gold`, `seq` exact; **all four counters `0`** — the ordinal width did not grow, so `widened == 0` too |

### 3.1 Where each expectation comes from — the derivation, independent of the reference

- **C1** — D1 §5.3 steps 4–5 select by the header's hash TAKEN AS GIVEN; the reader's own hash resolves to
  its own entry, which is an INDEX COMPARISON, so the IDENTITY plan runs and §5.2's `fills` are empty. D1
  §5.4's last row: a clean read of a file this build wrote moves nothing. `layout_hash` is zero on every
  path but the two layout refusals (§5.9 #15).
- **C2** — D1 §5.2 `EMIT`: `te.kind != me.kind` and `LADDER(te.kind, me.kind)` holds for `int16` into
  `int32`, so the entry is `widen` with `sign := te.kind is a signed integer` — **sign by the WRITER's
  kind**, which D1 states as a rule and warns against inferring from the reader's. D1 §5.4's `widen` row:
  `widened` once per ENTRY per record; `v` is one scalar, so the number is exactly 1, and §5.9 #33 is why
  the fixture asserts an exact count. `lead` and `trail` are same-kind, same-size, so they are `copy`,
  which moves no counter. `clamped == 0` because neither schema declares a range on `v` and D1 §5.4's
  bounds row only clamps a RANGED scalar off its end. The values are the manifest's (§5.9 #32, #37); D1
  states `lead`/`trail` verbatim.
- **C3** — D1 §5.2 `MATCH`: "A reader field the writer does not carry gets no entry either, and the prefill
  answers it." §5.2's prefill: `fills := COVER(y) MINUS every byte the entries land`, and the image is "the
  reader's OWN FRESH VALUE", so `w` lands its DECLARED default. The declared default is in the training
  fixture text: `w int32 = 88`. D1 §5.4's last row and §5.9 #10: an append the reader knows is not an
  event, so every counter is zero.
- **C4** — D1 §5.3 step 5: the NEW file's hash is in no entry of the OLD build's lineage (that build's
  lineage is its own single layout, §5.9 #2), so `REFUSE layout_newer, reporting h AND NOTHING ELSE`. The
  joint table: a refusal by name sets `refused` and `reason` and leaves `malformed` FALSE, returns `-1`,
  moves no counter. "REFUSE is total: ... not one destination byte is written — the prefill included",
  which is what the `0x5A` poison tests; §5.9 #35 gives the three conforming poison targets and the arm
  says which it laid.
- **C5** — D1 §5.2: `id` is `fnv1a64(wire name)` and `was =` matches by the OLD name, so the rename moves
  no layout byte; §5.2's digest table lists what the digest carries and a name is not among it, so no
  digest byte moves either. Equal hashes, so §5.3 step 5 resolves each reader to its OWN entry and both
  take the identity plan. D1 §5.7: "A row whose edit moves no layout byte and no digest byte ... has no
  second column ... which is the test that the hash, and only the hash, is the version." §5.9 #41: any
  harness pairs the two generations' fields by WIRE ID, never by name.
- **C6** — D2 §21.1: a field "deprecated in place" is a widening; D2 §21.2: "A field the reader
  deprecated: dropped". D1 §5.1: "A deprecation is not a version either — the slot is still written and
  still read." Both hashes are therefore equal and it reads in both directions. `unknown` stays zero
  because D1 §5.4 moves it only for a writer field NO reader field names, and the reader still names `b`.
- **C7** — D1 §5.3 step 7: a known hash with a different layout length or different layout bytes is
  `REFUSE layout_malformed`. §5.3's note: "The seven §1.1 malformations under a KNOWN hash all come back as
  one name, `layout_malformed`." The check does not need to know §1.1 at all, which is the point.
- **C8** — D1 §5.3 step 11, in order: `if LE(8, at) != h: REFUSE no_layout` stands BEFORE `PREFILL`. §5.3:
  REFUSE writes nothing, the prefill included. **Note for adjudication:** D1 §5.8 row 9 records that the
  C++ reference runs the prefill BEFORE this check, so a reference-shaped implementation FAILS C8. That is
  deliberate — C8 is one of the checks that distinguishes "implemented the page" from "copied the
  reference".
- **C9** — D1 §5.1 `WIDENS`, the `int, float` row: `width(a) <= width(b), same ladder, same signedness or
  FAIL "narrowed | ladder | signedness"`. The `table, type` row: `fields(a) is a SUBSET of fields(b) by
  NAME and a SUBSEQUENCE of it by position ... or FAIL "field removed | inserted | reordered | modified"`.
  And §5.1's closing sentence: "Every FAIL names the table, the definition, the rule, and both values."
  D2 §21.1's table is the same law stated as a table.
- **C10** — D1 §5.3 step 1 (`len(file) < 20`) and step 9 (`rest mod record_bytes != 0`) both set
  `malformed` and return `-1` with no name; D1 §5.3's joint table makes `refused`/`reason` and `malformed`
  mutually exclusive, and says a port that sets `malformed` beside a reason fails the joint assertion even
  though it refused correctly.
- **C11** — D1 §5.3's "EVERY HASH A RUNTIME HOLDS WAS HANDED TO IT, AND A RUNTIME NEVER DERIVES ONE"; §5.9
  #47; §5.9 #3's contract: "nothing on the load path compiles, nothing on the load path parses a layout,
  and the load path cannot fail for want of a plan." D2 §21.5: "The reader never parses a layout it has not
  seen before." §5.9 #3 also names the two conforming shapes, which is why C11 asks the arm to say which.
- **C12** — D1 §5.2 `EMIT` kind 30: the `widen` is emitted only when `te.size < me.size`; three variants
  and four variants both fit one byte, so the ordinal width did not grow and the entry is an `ordinal`
  remap whose table maps each writer variant to the same reader position. D1 §5.4: `ordinal` moves no
  counter. D2 §21.2: "An older enum: its ordinals are the reader's, the list being a prefix."

### 3.2 THE ONE DELIBERATE WRONG IMPLEMENTATION the checks must reject

Described in prose, to be built by the adjudicator (not by any arm) and run against the same twelve checks,
so the checks are proven able to FAIL:

> **THE CLAMPING READER.** It is a reader that treats a widening as a VALUE-DOMAIN operation instead of a
> REPRESENTATION one. On the older `int16` field read into its own `int32` field it reads the writer's two
> bytes and ZERO-extends them, then clamps the result into what it believes the field's domain to be, and
> moves `clamped` when it does. It is otherwise a careful reader: it selects a plan by the header's hash, it
> compares the layout bytes, it lands `lead` and `trail` exactly, it prefills appended fields with their
> declared defaults, and it refuses a hash it does not hold. In the same spirit it is LENIENT about the
> definitions: given a lineage entry whose field list is a strict superset of its own — a definition the
> newer generation removed — it matches what it can by name and reads on, counting the missing field under
> `unknown` rather than refusing the lineage at commit.

**The checks that reject it, and how:**

| check | how it fires |
|---|---|
| **C2** | the writer's `-1` (`0xFFFF`) ZERO-extends to `65535` instead of sign-extending to `-1`, and `INT16_MIN` lands `32768` instead of `-32768` — the manifest's values do not match. Independently: `clamped == 1` where C2 requires `clamped == 0`, and `widened` is not `1`. Three separate failures on one check, from three different columns of the report |
| **C9** | the lenient half is refused here: (d) `{lead, v, trail}` → `{lead, trail}` must FAIL `"field removed"` at commit, and this reader ACCEPTS the narrowing and reports it as a run-time `unknown` instead. D1 §5.9 #30 is the reason this is not a near miss: on a LAWFUL lineage `unknown` is STRUCTURALLY ZERO, so a reader that moves it has admitted an unlawful one |
| **C1** | the clamp fires on the reader's OWN file too if the reader's domain belief is narrower than the field, so the identity read's `clamped == 0` and "all four counters `0`" can also catch it |

That is a wrong implementation that passes a naive "does it read?" test — it returns `n`, it lands `lead`
and `trail`, it refuses `layout_newer` correctly — and is rejected by the packet's checks on three
independent columns. **A check set that cannot reject this reader is not adequate and the run should be
re-scoped rather than reported.**

## 4. The property tests THE PROCESS names for this slice

THE PROCESS names three properties; they EXIST in the repo, at `test/tables/fixedform_properties.cpp`, and
the file's own header (lines 1–16) states them. **The three are named here as the properties an arm's
prototype should satisfy; the FILE ITSELF IS NOT READABLE BY AN ARM** (§5).

| # | property | as the repo states it |
|---|---|---|
| **P1** | identity == compiled | the identity plan and a plan compiled from this build's own layout land the same fields and move the same counters |
| **P2** | round trip | write, read, write again: the bytes are identical |
| **P3** | hostile bytes | every byte of every record, mutated to `{00,01,02,7f,80,ff}`, on both paths: every landed field is within its bound or the read is a NAMED refusal, and a correction moves a counter |

For the bounded slice, the property an arm owes is **P1 over the training rows in §2.3** plus the
**PAIR direction** D1 §5.7 adds to it: FORWARD, the newer reader compiles the older file through its lineage
and returns one record; REVERSE, the older reader given the newer file refuses `layout_newer` BY NAME. P2
and P3 are stated for completeness and are OUTSIDE the 30-minute clock. The gate's own record check is
the reader's COMPILED HASH CONSTANT, never a hash of the layout bytes it just read (D1 §5.7).

One drift worth recording, found while assembling: D1 names the fixture pair map as
`ir.TableFixedFixtureLineage` (`ir/fixedform.go:685-693`) in three places, and at this commit no such
symbol exists — the map is `fixtureLineage` in the reference's own generator. It is a stale citation in D1,
and it points into the reference, so it is a path an arm must NOT follow. No expectation in §3 depends on it.

## 5. What is deliberately NOT GIVEN

1. **The reference implementation's source, in whole.** `internal/codegen/cpptable/` (including
   `lineage.go`), `ir/fixedform.go`, the C++ and C fixed-form runtimes (`fixedruntime.go` and its emitted
   text), `tools/fixedtwin`, `compiler/lineage.go`, `internal/lockfile/` — none of it. D1 cites these by
   file and line throughout; **those citations are provenance, not reading list.** D1 says it plainly: a
   port implements THIS PAGE, and D1 §5.8's fourteen rows are the reference's DEBTS — "a leg that
   reproduces a divergence to match the reference has ported the bug".
2. **The HELD-OUT set** — the 19 fixture files in §2.2, and any generated `.bin` or manifest line derived
   from them. An arm builds the corpus but reads only the training rows' files and manifest lines.
3. **The existing test and dump sources**: `test/tables/fixedform_dump.cpp`, `versioning_lists.cpp`,
   `versioning_numbers.cpp`, `fixedform_properties.cpp`, `fixedform_hostile_bytes.cpp`,
   `fixedform_main.cpp`, `fixedform_runcopy.{c,cpp}`. The manifest is the value oracle precisely so the
   dump need not be read (D1 §5.9 #32, #37).
4. **The nine existing ports.** Go, C, Rust, Java, C#, JavaScript, Dart, Elixir and the C++ reference are
   all on this lane. No arm reads any of them. This is the whole point of a fresh two-language pair.
5. **The bill**, `docs/FIXED-FORM-BILL-READS-BACKWARD.md`. See §1's open item; the recommendation is to
   keep it out, and every §3 expectation is derived without it.
6. **The rest of the spec.** `docs/SPEC.md`, `docs/SPEC-TABLES.md` outside lines 16379–16448,
   `docs/VERSIONING.md`, `docs/FIXED-FORM-VERSIONING-TESTS.md`, `docs/PORTING.md`, `ROADMAP.md`,
   `CLAUDE.md`. **`docs/FIXED-FORM-VERSIONING-TESTS.md` is the one to name twice**: it is the test DESIGN
   for exactly this slice, it tabulates every row's expected landing and refusal, and handing it over would
   hand over most of §3's answers.
7. **Rowan's and Stella's own working notes, queues and bus traffic** on the slice.

## 6. The clock, and the measurement fields each arm reports

### 6.1 ONE CLOCK. The result is FROZEN at 30 minutes.

**The clock starts when the packet is DELIVERED to the arm and stops 30 minutes later. That is the WHOLE
budget: reading D1 and D2, setup, the corpus build, writing the reader, running the checks and DEBUGGING are
all inside it.** There is no implementation budget and there is no separate debug allowance. At the
30-minute mark the arm STOPS, records the state of the tree, and reports — whatever is passing is passing
and whatever is red is red.

**`debug_minutes` is a SPLIT OF THE 30, NEVER AN ADDITION TO IT.** Every minutes field in §6.3 marked "in
the box" is a SLICE of the one 30-minute clock, and those slices **SUM TO AT MOST 30**. A report whose
in-box minutes sum past 30 is a reporting error to be corrected, not a longer run to be accepted.

**Anything after the freeze is a REPAIR PHASE.** It is named as such, timed separately, reported APART, and
**never merged into the frozen result**: no check that first passed during repair may be counted in
`checks_passed_training`; no in-box minutes may be revised upward after the freeze; and a repair phase's
numbers are **NOT COMPARABLE ACROSS ARMS** — the four arms are compared on the frozen result and on nothing
else. A repair phase is allowed and usually worth doing, because it is where an arm learns what it got
wrong; it is simply a second, separately labelled row that never touches the first.

**A frozen PARTIAL result is a legitimate outcome, and the packet expects several.** An arm at the freeze
with C1, C2 and C4 green, C3 red and C5–C12 not yet written reports `checks_passed_training = 3 of 12`,
names C3's red, and marks C5–C12 not reached. **That is a result.** An arm that ran to 45 minutes and
reports 9 of 12 has no result at all under this packet, and its row is void rather than better.

### 6.2 What the coordinator owes the clock

One recorded delivery moment per arm: the UTC timestamp at which that arm was handed this branch. Four
arms, four recorded starts, four freezes, and the freeze is arithmetic — `delivered_at + 30 minutes` — not
a judgement anyone makes at the time.

An arm blocked on something the COORDINATOR owes it — a toolchain that will not install, a corpus build that
will not run, an access problem — **pauses the clock, with the pause and its reason recorded**, and reports
the total as `blocked_minutes`. A pause is for an impediment outside the arm's work; it is never a way to
buy working time, and an arm that pauses to think has not paused.

### 6.3 The fields

One row per arm, the same fields, reported at the freeze:

| field | unit | in the box? | note |
|---|---|---|---|
| `arm` | — | — | language (Common Lisp \| Haskell) and coordinator (Rowan \| Stella) |
| `delivered_at` | UTC timestamp | — | the recorded delivery moment; the clock's zero |
| `frozen_at` | UTC timestamp | — | `delivered_at` + 30 minutes, plus any recorded `blocked_minutes`. Arithmetic, not judgement |
| `setup_minutes` | minutes | **YES** | clone, toolchain, corpus build |
| `reading_minutes` | minutes | **YES** | D1 and D2, and this packet |
| `implementation_minutes` | minutes | **YES** | writing the reader and the twelve checks |
| `debug_minutes` | minutes | **YES** | making written checks pass. **A SLICE OF THE 30, NOT A SECOND BUDGET** |
| `review_minutes` | minutes | **YES** | the arm's own read of its work before handing over |
| `in_box_total` | minutes | — | the sum of the five rows above. **MUST BE ≤ 30** |
| `blocked_minutes` | minutes | no | recorded pauses on a coordinator-owed impediment, each with its reason |
| `elapsed_minutes` | minutes | — | `delivered_at` to `frozen_at`, pauses included |
| `tokens` | count | — | if the harness exposes them: per model, naming the harness and the model. `n/a` when it does not — never an estimate presented as a measurement |
| `checks_passed_training` | `k of 12` | — | §3's twelve on the TRAINING rows, **as frozen** |
| `runner_path` | — | — | the executable §7 requires, and that it runs from a clean checkout |
| `poison_form` | — | — | which of D1 §5.9 #35's three conforming poison targets the arm laid (byte pointer, plan record image, or the value surface field by field) |
| `plan_shape` | — | — | which of D1 §5.9 #3's two conforming shapes the arm took (plans emitted as source, or built once at package initialization) — C11's prose answer |
| `reds_named` | list | — | every red the arm leaves, named; plus any red it did NOT write and had to route around (D1 §5.9 #31: a red nobody wrote down is a red nobody owes) |
| `heldout_passed` / `heldout_applicable` / `heldout_unknown` | counts | — | **FILLED BY THE ADJUDICATOR, NEVER BY THE ARM.** §7.4's counting |
| `repair_minutes` | minutes | **no** | after the freeze, timed separately |
| `repair_checks_passed` | `k of 12` | **no** | reported apart and **NEVER folded into `checks_passed_training`** |

No arm reports a check as passed that it did not run. No arm reports the held-out columns at all. No arm
reports a repair number inside the box.

## 7. The runner: one invocation, so an adjudicator never reads an arm's source

Four arms in two languages cannot be compared by reading four implementations. **Every arm ships ONE
EXECUTABLE with the shape below**, and the adjudicator runs the held-out cases through it without opening a
line of the arm's code — which is also what keeps the held-out set held out: the arm never sees the
fixtures the adjudicator names.

### 7.1 The invocation

```
<arm-repo-root>/challenge/run-checks  <manifest-path>  <fixture>
```

**The path is fixed: `challenge/run-checks`, relative to the arm's own repository root.** It is DIRECTLY
EXECUTABLE — the adjudicator invokes it with `execve` and argv only. **The contract is SHELL-FREE**: no
`sh -c`, no shell quoting, no word splitting, no environment variable an arm expects to be set, no working
directory other than the arm's repo root, no wrapper script the adjudicator must compose. A compiled binary
is fine; a script with a `#!` line is fine, because the kernel handles that and no shell is involved.

| argv | what it is |
|---|---|
| `argv[1]` | an ABSOLUTE path to a corpus manifest — `build/fixedform-corpus/manifest.txt`. The runner reads the manifest at THIS path and nowhere else, and takes the fixture `.bin` files from the manifest's own directory |
| `argv[2]` | the fixture. **If it contains a `/` or ends in `.bin`, it is a PATH to a fixture file; otherwise it is a ROW ID** (`int_widen`, `nested_append`, …) resolved against the manifest's `row=` field. That rule is the whole disambiguation and needs no flag |

No other arguments. No flags, no subcommands, no config file, no environment.

### 7.2 The output: twelve lines, one per check

To **stdout**, one line per check, **in order C1 through C12**:

```
CHECK <id> <status> <detail>
```

| token | form |
|---|---|
| `CHECK` | literally that, at the start of the line |
| `<id>` | exactly `C1` … `C12`, matching §3's numbering |
| `<status>` | exactly one of **`pass`**, **`fail`**, **`refuse:<class>`**, **`n/a`** |
| `<detail>` | everything after the third space, running to the end of the line; free text, never empty — write `-` when there is nothing to say |

Single ASCII spaces separate the first three tokens; `<detail>` may contain spaces. One line per check,
twelve lines, nothing else on stdout — no banner, no summary, no totals.

**The four statuses:**

| status | means |
|---|---|
| `pass` | the check's expectation in §3 was met IN FULL. **A check whose expectation IS a refusal (C4, C7, C8) prints `pass` when the observed class is the expected one** — the expected refusal is a pass, not a refusal |
| `fail` | the expectation was not met and the reader did not refuse. `<detail>` names WHICH column disagreed and gives the observed value, e.g. `v landed 65535 expected -1` or `widened=0 expected 1` |
| `refuse:<class>` | the reader REFUSED where this check did not expect that refusal — either it expected a read, or it expected a different class. `<class>` is the reason NAME from D1 §5.3: `layout_newer`, `layout_unsupported`, `layout_malformed`, `no_layout`, `previous_form`, `message_form_as_file`, `newer_form`, `batch_too_large`, `plan_too_large`, `layout_record_too_large`, or `malformed` for the unnamed residue. This status exists so the adjudicator sees WHICH refusal fired without reading source |
| `n/a` | the check does not apply to this fixture, **or** the arm never wrote it. The two are separated by the DETAIL and not by a fifth status: **a check the arm did not reach by the freeze begins its detail with the token `not-reached`**; anything else is a genuine inapplicability, and the detail says why (`no enum in this row`, `row has no byte-identical pair`) |

`n/a` is the load-bearing status. **A held-out case that a training check does not exercise is `n/a`** — C12
asks about an appended enum variant and `uint_widen` has no enum; C3 asks for `Vec.w == 88` and no other
row has that field; C5 and C6 need a pair whose edit moves no layout byte. Marking those `fail` would
punish an arm for the packet's own shape.

### 7.3 Exit status, streams, determinism

- **Exit `0` whenever all twelve lines were printed**, whatever they say. The exit code reports the
  RUNNER's health, never the checks' verdict; an adjudicator that reads pass/fail from `$?` has read the
  wrong thing.
- **Non-zero only when the runner could not run at all** — an unreadable manifest, a fixture neither path
  nor row id resolves, the arm's reader failing to load as a program — with a diagnostic on **stderr**.
  **stderr is never parsed** and may carry anything.
- **Deterministic**: the same two arguments produce the same twelve lines, byte for byte. No timestamps, no
  absolute paths, no addresses, no run-to-run ordering in the detail text.
- **No framework and no dependency**: the language's standard library plus the arm's own reader, and
  nothing else. No test framework output, no TAP, no JUnit XML, no JSON, no colour. Twelve lines.

### 7.4 How the adjudicator counts — applicable and unknown cells, never a forced `k of 12`

Per held-out fixture the adjudicator runs the runner once and reads twelve cells. Across the held-out set:

| quantity | how |
|---|---|
| `heldout_applicable` | cells whose status is `pass`, `fail` or `refuse:<class>` |
| `heldout_passed` | cells whose status is `pass` |
| `heldout_unknown` | cells whose status is `n/a`, **reported in two parts**: `not-reached` cells and genuinely-inapplicable cells |

**The headline is `heldout_passed of heldout_applicable`, and `heldout_unknown` is reported BESIDE it,
never folded into either number.** There is no `k of 12` on the held-out set: forcing one would either
credit an arm for a check that could not apply or penalise it for the same, and both are measurement errors.

**An `n/a` the adjudicator believes IS applicable is itself a finding.** It is recorded by fixture and
check id and reported as a discrepancy — never silently converted to a `fail`, and never silently accepted.
A pattern of over-broad `n/a` is the one way this contract can be gamed, so it is read for.

**Two facts the adjudicator establishes before counting anything:** that the runner builds and runs from a
CLEAN CHECKOUT of the arm's branch at the frozen commit, and that it was FROZEN — the commit is at or
before `frozen_at`. A runner repaired after the freeze is adjudicated in the repair row, not the frozen one.

## 8. Counts

| | |
|---|---|
| documents | **2** (ALGORITHM §5, lines 358–1600; SPEC-TABLES §21, lines 16379–16448) |
| training fixtures | **38** |
| held-out fixtures | **19** |
| checks | **12** |
| property tests named | 3 (P1, P2, P3; P1 + the pair direction inside the timebox) |
| deliberate wrong implementations the checks must reject | 1 (the clamping reader) |
| the clock | **ONE, 30 minutes from delivery, debugging included; frozen** |
| the runner | `challenge/run-checks <manifest> <fixture>`, twelve `CHECK` lines, shell-free |
| repo / commit | `mas-bandwidth/schema` @ `b7ab66a88b6023f4016ef577fd163b84e45e692e` |
