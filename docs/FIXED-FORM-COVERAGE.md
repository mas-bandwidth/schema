# The fixed form's coverage matrix

**WHAT THIS PAGE IS.** Every shape docs/SPEC-TABLES.md §3.4 admits and every
rule it states, against the four ways this form can be got wrong, with the
fixture that holds each cell or the reason there is none. It exists because the
question that produced it was not *"is the fixed form tested?"* but *"what is it
missing tests for?"*, and those are different questions: the first is answered by
a green gate and the second only by an enumeration somebody has to write down.

**THE FOUR COLUMNS ARE THE FOUR WAYS TO BE WRONG**, and a shape covered in one
of them is not covered in the others:

| column | what it asks |
|---|---|
| **IDENTITY** | the reader's own hash, so the plan is the static constant the compiler wrote — every value must come back exactly |
| **COMPILED (older)** | a record written by an EARLIER generation, read through a plan compiled from that generation's layout |
| **COMPILED (newer)** | a record written by a LATER generation — the direction where a name this reader does not have has to be stepped over rather than guessed |
| **WRITE** | the BYTES this build produces, pinned, so a second port can be held to them |

**THERE ARE TWO ORACLES AND NEITHER IS THE OTHER'S COPY.**
`test/tables/fixedform_dump.cpp` writes one form-3 FILE per root into `build/`,
which is what a PORT diffs against at test time; `make tables-fixedform-oracle`
checks a REVIEWABLE TEXT pin, committed, which is what a PERSON diffs when a
byte moves. A binary corpus nobody committed cannot say a byte changed between
two builds of the reference; a text pin no leg reads cannot say a port
disagrees. Both run under `tables-fixedform-corpus`, from the same writer.

**THE WRITE COLUMN IS THE ONE THAT DID NOT EXIST.** Until
`testdata/conformance/tables/fixedform/records.dump` the fixed form had no pinned
bytes anywhere, while every other wire in this project has some. What the
versioning conformance set held is that a READER makes the right values out of a
writer; nothing held that a WRITER made the right bytes. **A leg whose writer
and reader are wrong in the same direction passes every round trip there is and
cannot exchange one record with anybody**, and that is the exact shape of the
hole this page was written to find.

---

## The legend

- **`fx`** `fu` `fn` `wstr` `w` `bits` `nk` `cf` `plan` `s` `fl` `text` `frame`
  `p` `v` `neg` `layout` `fuzz` — the case functions of `test/tables/fixedform_main.cpp`.
- **`oracle`** — a pinned block of `testdata/conformance/tables/fixedform/records.dump`,
  written by `test/tables/fixedform_pin.cpp`.
- **`RED-n`** — covered by a fixture that is a NAMED KNOWN-RED: the fixture is in
  the shared set today, it fails today, and the fix it waits on is named. The
  make target prints every one of them on a green run. See *The known-red list*.
- **`GAP-n`** — no fixture. Every one is in *The gap register* with what it would
  take and why it is or is not filled here.
- **`—`** — the column does not apply to that row.

**THE SCHEMA PAIRS.** `FX1/FX2` the scalar edits; `FU1/FU2` text and a counted
array under a UNION ARM; `FN1/FN2` the bool, the optional, the enum ordinal, an
ARRAY OF UNIONS and a THREE-DEEP nesting; `NK1/NK2` the plain narrow integer
kinds spelled as themselves; `FC1/FC2` a compressed float as IEEE, not as a
quantized index; `RW2` a second generation of `examples/Ranges` `RangedWidths`;
`V1/V2` the enum and keyed-array edits; `W1/W2` `flags`, the text family's
declared defaults and a TABLE renamed; `Scalars/Scalars2` the widths; `F1/F2`
the float bit patterns; `P1/P3` `?T` against a plain nesting; `examples/Ranges`
the `bits(N)` family; `KM1/KM2` a kind that moved on a 12-byte body, off RED-3's
window; `WC1/WC2/WC3` a `was =` chain that keeps the first wire name.

---

## 1. The shapes: §3.4's `C` table, row by row

| shape (`C`) | IDENTITY | COMPILED (older) | COMPILED (newer) | WRITE |
|---|---|---|---|---|
| `bool` | `fn` + **RED-5** | `fn` | `fn` | `oracle fn1/full` |
| `int8`/`uint8` … `int64`/`uint64` | `fx`, `s` | `fx` | `fx` | `oracle fx1/root` |
| — the plain narrow kinds, spelled as themselves | `nk` | `nk` | `nk` | `oracle nk1/narrow` |
| `bits(N)`, N ≤ 32 and above | `bits` | `bits` | `bits` | `oracle bits/widths` |
| a RANGED integer | `fx`, `s` | `s` + **RED-1** | `s` | `oracle scalars/simstate` |
| `float32`, `float64` | `fl` | `fl` (widened) | `fl` | `oracle f1/floats` |
| a COMPRESSED float | `cf` | `cf` | `cf` | `oracle fc1/probe` |
| `int128`/`uint128` | `s` | `s` | `s` | `oracle scalars/simstate` |
| `fixed(I,F)`, `ufixed(I,F)` | `s` + **RED-3** | `s` + **RED-3** | `s` | `oracle scalars/simstate` |
| `flags` | `w` | `w` (table renamed) | `w` | `oracle w1/vessel`, `w2/ship` |
| an ENUM, ordinal from `1`, `0` is `None` | `fn`, `v` | `fn`, `v` | `fn`, `v` (unknown variant) | `oracle fn1/full`, `v1/cfg` |
| — an ordinal PAST the last variant | **RED-6** | **RED-6** | **RED-6** | — |
| a nested `table`/`type`, INLINE | `fx`, `fn` | `fx`, `fn` | `fx` (unknown type), `fn` | `oracle fn1/full` |
| — nested THREE DEEP | `fn` | `fn` | `fn` | `oracle fn1/full` |
| `[N]T` | `s`, `fn` | `s`, `fn` | `s`, `fn` | `oracle scalars/simstate` |
| `[Min..Max]T`, count then MAX elements | `fu`, `fn`, `s` + **RED-3** | `fu`, `fn` | `fu`, `fn` | `oracle fu1/list`, `fn1/full` |
| — a count PAST the reader's `Max`, and a negative one | `fn` | `fn` | — | — |
| `string(N)`, length in BYTES | `text`, `v`, `fn` | `v`, `fn` (`was`) | `v`, `fn` | `oracle p1/{empty,short,full}` |
| `wstring(N)`, length in CODE UNITS, `2N` payload | `wstr`, `fu` | `fu` | `fu` | `oracle wide/stamp`, `fu1/wide` |
| `bytes(N)`, length in BYTES | `w`, `fu` | `fu` + **RED-9** | `fu` + **RED-9** | `oracle fu1/raw`, `fu2/raw`, `w1/vessel` |
| — TEXT OF ANY FLAVOUR UNDER A UNION ARM | `fu` | `fu` | `fu` | `oracle fu1/{wide,narrow,raw}` |
| a UNION: tag then the WIDEST ARM | `fu`, `fn`, `v` | `fu`, `fn`, `v` | `fu`, `fn` (unknown arm) | `oracle fu1/*`, `fn1/full` |
| — tag `0` is `None` | `fu` | `fu` | `fu` | `oracle fu1/none` |
| — a tag PAST the last arm | **RED-7** | **RED-7** | **RED-7** | — |
| — an ARRAY of unions, the guard re-tested per element | `fn` | `fn` | `fn` | `oracle fn1/full` |
| — an arm holding an ARRAY, and a narrower arm's slack | `fu`, `fn` | `fu`, `fn` | `fu`, `fn` | `oracle fn1/full` |
| `?T`: present flag + the payload WHOLE | `s`, `fn`, `v`, `p` | `fn`, `s` | `fn` | `oracle fn1/{full,absent}` |
| — an ABSENT optional over NON-ZERO residue | **RED-4** | **RED-4** | — | `oracle fn1/absent` |
| — a present byte that is neither `0` nor `1` | **RED-5** | **RED-5** | — | — |
| `[Enum]T`: every slot, no key rides | `s`, `v` | `v` (a key that SLID) | `v` (a key with no name here) | `oracle scalars/simstate`, `v1/cfg` |
| `const`, `reserved`, `align` | — | — | — | refused in a table body already (§11) |

---

## 2. The rules §3.4 states

| rule | IDENTITY | COMPILED (older) | COMPILED (newer) | WRITE |
|---|---|---|---|---|
| **form byte `3`**, and a refusal by name per direction | `neg` | — | — | `oracle` (every case's form byte) |
| a header hash that is not the layout's is `layout_malformed` | `neg` | — | — | `oracle` (both hashes printed) |
| **the layout's entry format and PRE-ORDER walk** | `layout` | `neg` | `neg` | `oracle` (length + hash) |
| **kind `35`, the optional wrapper**, one byte apart from a plain nesting | `fn`, `p` (`kind_mismatch`) | `fn` | `fn` | `oracle fn1/*` |
| **the seven layout validation rules**, each refused BY ITS OWN NAME | `layout` | — | — | — |
| a layout that is not one at all: `layout_malformed` | `layout` | — | — | — |
| **65536**: a record size past it is refused | `layout` | — | — | **GAP-6** (the COMPILE refusal) |
| **4096**: a warning, always on | — | — | — | **GAP-7** |
| **`--fixed-record-limit N`** | — | — | — | **GAP-8** |
| record := hash + body, DECLARED ORDER, each at its constant size | every case | every case | every case | `oracle` |
| **the declared STORAGE IMAGE, LE, declared width, nothing padded** | `bits`, `s`, `wstr` | — | — | `oracle` |
| **slack: ZERO ON WRITE** | — | — | — | `oracle fu1/none`, `fn1/absent` |
| **slack: UNSPECIFIED ON READ**, not `malformed`, no counter | `text` | — | — | — |
| **content rules over the USED UNITS and nothing else** | `text` + **RED-2**, `wstr` | — | — | — |
| a length PAST the field's own bound | `text`, `wstr`, `fn` | `fn` | — | — |
| **a DEPRECATED field keeps its slot forever** | **GAP-9** | **GAP-9** | **GAP-9** | **GAP-9** |
| what a fixed table CANNOT carry: pointer, map, `[]T`, a guarded branch | — | — | — | **GAP-10** |
| **selection is by the keyword**; a `table` is form `1` | — | — | — | **GAP-11** |
| the READ SIDE still accepts form `1` and form `3` | **GAP-12** | — | — | — |
| **op `copy`** | every case | every case | every case | — |
| **op `count`** | `fu`, `fn`, `s` | `fn` | `fn` | — |
| **op `text`** | `text`, `wstr`, `fu` | `fu` | `fu` | — |
| **op `union`** | `fu`, `fn`, `v` | `fu`, `fn`, `v` | `fu`, `fn` | — |
| **op `widen`** | — | `fx` (u16→u32), `fl` (f32→f64) | — | — |
| **op `clamp`** | — | **RED-1** (the op does not exist) | — | — |
| **op `ordinal`** | **RED-6** | `v`, `fn` | `v`, `fn` | — |
| a kind that MOVED is skipped and never misdecoded | `p`, `km` | `v`, `s` + **RED-8**, `km` | `s`, `km` | `oracle km1/probe` |
| **the plan is PARTITIONED**: unguarded first, then the arms' | `plan` | `plan` | `plan` | — |
| **the PREFILL answers "absent field"** | — | `fx`, `fn`, `v`, `w` | — | — |
| **the ABSENCE of an entry answers "unknown field"** | — | — | `fx`, `fn`, `s`, `fu` | — |
| an unknown NESTED TYPE stepped over by its whole size | — | — | `fx` | — |
| **the identity plan is a static constant, with ADJACENT RUNS COALESCED** | `plan` + **RED-3** | — | — | — |
| a compiled plan is CACHED BY HASH | **GAP-15** | **GAP-15** | **GAP-15** | — |
| the plan's storage is the CALLER's; `plan_too_large` | `neg` | `neg` | `neg` | — |
| **the write is a TEMPLATE `memcpy` then constant stores**; `MeasureBody` is `constexpr` | — | — | — | `bits`, `oracle` |
| **the FILE's framing**: header at 0/8/16, records to the end | `frame`, `oracle` | — | — | `oracle` |
| **BYTES LEFT OVER ARE `malformed`** | `frame` | — | — | — |
| a file of ZERO records, and of many | `frame` | — | — | — |
| a caller capacity BELOW the file's record count | `frame` + **GAP-18** | — | — | — |
| the STREAM carrier (§3.3's announcement) | **GAP-16** | **GAP-16** | **GAP-16** | **GAP-16** |
| the MESSAGE carrier (one form byte per batch) | **GAP-17** | **GAP-17** | **GAP-17** | **GAP-17** |
| **a byte-flip fuzz over a whole file, under a sanitizer** | `fuzz` | `fuzz` | `fuzz` | — |
| **the WRONG-PLAN negative control** | `neg` | — | — | — |
| the plan's destinations asserted against the language's own ABI | the generated `static_assert`s | — | — | — |
| the GENERATOR BOUND on the identity plan's size | **GAP-19** | — | — | — |

---

## The gap register

**A GAP IS FILLED HERE ONLY IF A LEG COULD GET IT WRONG SILENTLY.** That is the
bar the work was scoped to, and it is why some rows below are left open on
purpose rather than left open by omission: a rule whose breach is a compile
error, or a refusal by name, is a rule the next port finds out about on its
first build. A rule whose breach is a wrong value in a clean read is not.

### Filled

| gap | what it was | what fills it |
|---|---|---|
| TEXT UNDER A UNION ARM | the plan entry's `arg` lane carries an arm ORDINAL for a guarded entry and a text FLAVOUR for a text entry — and a text field under an arm needs both. No fixture in the set had text under an arm, so neither half was reachable. | `FU1/FU2`, `fu` — one arm per flavour in ordinal order, so ordinal and flavour disagree at every arm but the first. **FIXED ON THE BRANCH** by `tables: the guard's ordinal and the text op's flavour are two lanes`, and the two reds that named it were deleted in the same commit as these rows |
| `wstring` HAS NO ORACLE BYTES ANYWHERE | flavour 2's length is in CODE UNITS and its payload is `2N` bytes — the one text row whose two numbers differ — and nothing pinned a byte of it | `wstr` + `oracle wide/stamp` (at the root) and `oracle fu1/wide` (under an arm), including a lone surrogate |
| `bytes(N)` UNDER A COMPILED PLAN | — | `fu` (the `raw` arm, read by both generations) |
| A TAG PAST THE LAST ARM | — | `fu`, `fn` (per element of an array). **RED-7** |
| AN ENUM ORDINAL PAST THE LAST VARIANT | — | `fn`. **RED-6** |
| A PRESENT BYTE OF `7` | — | `fn`. **RED-5** |
| A BOOL BYTE OF `2` | nothing in the set had a `bool` in a fixed root at all | `fn`. **RED-5** |
| OVER-`Max` COUNTS AND LENGTHS | — | `fn` (a count of 99 and a negative one), `text`, `wstr` |
| TEXT CONTENT VIOLATIONS | — | `text` (ill-formed UTF-8 and an interior zero, inside the USED bytes). **RED-2** |
| AN ABSENT OPTIONAL WITH NON-ZERO RESIDUE | — | `fn`. **RED-4** |
| AN ARRAY OF UNIONS | the arm guard has to be re-tested PER ELEMENT | `FN1/FN2`, `fn` |
| AN ARM THAT IS A NESTED TYPE WITH AN ARRAY | — | `FU1` `MarkList`, `FN1` `CellB` |
| THREE-DEEP NESTING | two deep was the deepest anything reached | `FN1/FN2`, `fn`, with the INNERMOST type resized in FN2 |
| RECORD COUNTS `0` AND MANY | every fixture read a file of exactly one record, leaving the framing arithmetic untested in both directions | `frame` — zero, three, every truncation, and one byte left over |
| `flags`, AND THE TEXT FAMILY'S DECLARED DEFAULTS | §3.4's `flags` row had no fixture, and neither did a `string`/`bytes`/`flags` default that is not zero | `W1/W2`, `w` + `oracle w1/vessel` |
| `bits(N)` AT ITS DECLARED STORAGE WIDTH | the row where this form spends bytes on purpose | `bits` + `oracle bits/widths` |
| A TABLE RENAMED UNDER `was` | the field-level pairs cannot reach a rename at the table's own level | `w` (a W1 `Vessel` read into a W2 `Ship`) |
| THE PLAIN NARROW KINDS SPELLED AS THEMSELVES | storage image was exercised (`fixed(4,4)` is an int8) and the DECLARATION was not | `NK1/NK2`, `nk` + `oracle nk1/narrow` — C is 22 with no pad; the C++ ABI of the same fields is 24 |
| A `bits(N)` EDIT ACROSS GENERATIONS | `examples/Ranges` had one generation; N cannot move without moving the kind | `RW2`, `bits` — a `bits(24)` and a tail appended, so both compiled directions skip or default |
| A COMPRESSED FLOAT | §3.4: "rides as the float, not as a quantized index". The one silent-class gap the first pass left | `FC1/FC2`, `cf` + `oracle fc1/probe` — 2.5 is IEEE `00 00 20 40`, not integer 250; 1.234 is not snapped |
| A `flags` FIELD UNDER A NEWER WRITER | W2 renamed the table and the first pass only ran W1→W2 | `w` — a W2 `Ship` read as a W1 `Vessel`. Gaining a bit is still not a wire event (the mask is raw) |
| `bytes(N)` UNDER A COMPILED PLAN | identity of the raw arm is green; both compiled directions walk kind 14 | `fu` + **RED-9** — FU1↔FU2 `raw`. The identity plan is a `text` op; a compiled plan sees layout kind 14 and the dst/aux lanes are the counted-array's, swapped for `bytes(N)` (reference-fix 10) |
| THE PLAN'S PARTITION | `FnRootFixedPlanGuarded` was emitted and unasserted | `plan` — unguarded entries first, then the arms, on the identity plan and on a plan compiled in each direction |
| ADJACENT RUNS COALESCED | only RED-3 was watching, and it is the copy implementation | `plan` — no two adjacent copy entries inside one half would still merge. **RED-3** still watches the 17..31-byte copy |
| A KIND THAT MOVED, OFF RED-3'S WINDOW | RED-8's `angle` sits inside the 17..31-byte run that clobbers it, so a kind mismatch and a clobber could not be told apart | `KM1/KM2`, `km` — `fixed(16, 16)` respelled `int32` on a 12-byte body. The compile path skips and counts; the declared default stands. **RED-8** remains on Scalars until the run copy is fixed |
| A `was =` CHAIN | `ir.Field.WasName` is a SINGLE name, which is how a chain is spelled: the FIRST wire name, forever (USAGE). Aiming the third spelling at the intermediate name hashes a name no file carried | `WC1/WC2/WC3`, `wc` — `label` → `caption \| was = "label"` → `title \| was = "label"`. A WC1 record resolves at WC3 |

### Open, and why a fixture cannot be written

A GAP IS LEFT OPEN HERE ONLY IF A SHARED C++ ORACLE FIXTURE CANNOT HOLD IT.
The first pass left some of these as "low value" or "the batch ran out"; those
are filled above. What remains cannot be a fixture of this set today.

| gap | why a fixture cannot be written |
|---|---|
| **GAP-6/7/8** the 65536 COMPILE refusal, the 4096 warning, `--fixed-record-limit` | All three are COMPILER verdicts and none exists in this tree yet: there is no `fixed table` keyword (§3.4 names #823) and no flag. The READ side's own 65536 bound is covered by `layout`. A missing compile refusal is loud on first build. **Cannot: no keyword, no flag, nothing to compile-fail against.** |
| **GAP-9** a DEPRECATED slot | §3.4 names #823 and #825 as the marker and its baseline lock; the compiler has no `deprecated` at all today. **Cannot: nothing to write a fixture against.** |
| **GAP-10** what a fixed table cannot carry | The compiler has no `fixed table` keyword, so pointer, map, `[]T` and a guarded branch select form 1 by derived mode (G1 is that unit) rather than refusing. **Cannot: a shared C++ oracle is a form-3 value on the wire, and those shapes have none. The keyword that would refuse is on unmerged `fixed-table-keyword` (#823).** |
| **GAP-11/12** selection by the keyword, and the read side accepting both forms | Both wait on #823. Until then the compiler selects by the DERIVED mode, which §3.4 says in as many words. **Cannot: the keyword is on `fixed-table-keyword` and is not merged.** |
| **GAP-15** a compiled plan CACHED BY HASH | The C++ reference's `FixedLoad` compiles per call from the caller's plan storage; there is no cache to test. **Cannot: there is no cache. A cache is a performance property; the measurement gate is where it belongs, when one exists.** |
| **GAP-16/17** the STREAM and MESSAGE carriers | `FixedSave`/`FixedLoad` write and read the FILE carrier only. `SaveMessages`/`LoadMessages` still emit form 2's bitpacked batch (form byte 2, no layout hash, no hash+body record). There is no stream announcement of a layout hash and no reader that consumes a hash+body without a file header. **Cannot: form 3 has no stream or message writer. The largest remaining hole, and it is the message form's first card, not this set's.** |
| **GAP-18** a caller capacity below the file's record count | §3.4 states NOTHING about it. The reference refuses `batch_too_large` by precedent from §3.3, and `frame` now pins that — but a pin is not a rule, and §3.3 also requires the reader to hand the caller the count it was short by, which `FixedLoad` has no way to do. **Cannot: a question for the spec, not a missing test of a stated rule.** |
| **GAP-19** the GENERATOR BOUND on the identity plan | §3.4 names it as the generator's and not the wire's: a type whose leaves do not fit one plan does not carry the form. Reaching it means a large array of a type carrying text, a count, a union or an optional, which then does not emit the form. **Cannot: a fixture that does not compile is not an oracle of the form.** |
| **A TABLE EXACTLY AT 65536** | The READ side's bound is covered (`layout` refuses 65537 and a size that would wrap). Exactly-at-the-bound on the WRITE side needs the compile refusal of GAP-6. **Cannot: waits on GAP-6.** |

---

## The known-red list

**A FIXTURE WRITTEN FOR A BUG THE REFERENCE STILL HAS IS THE ONLY KIND OF
FIXTURE THAT PROVES THE BUG IS REAL**, so it goes into the shared set the day it
is written and not the day the fix lands. The list is held from BOTH ENDS: a
listed case that starts PASSING turns the run red, because a known-red nobody
deletes is a gate that has stopped covering something. Deleting the entry is part
of landing the fix.

| | key | waits on |
|---|---|---|
| **RED-1** | `clamp-op/compiled/tightened-bounds-do-not-clamp` | a `clamp` op in the reference's read loop (§3.4's op table names one; the op set has none) |
| **RED-2** | `text-content/identity/invalid-utf8-is-not-malformed` | UTF-8 validation in the reference's `text` op |
| **RED-3** | `run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours` | the run copy's 17..31-byte branch, `internal/codegen/cpptable/fixedruntime.go` and ctable's twin |
| **RED-4** | `optional/absent-payload-residue-is-copied` | the `?T` payload gated on the present byte (§3.4: "IGNORED on read") |
| **RED-5** | `bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool` | a ruling on what a bool byte outside `{0, 1}` means, and a normalise to match it |
| **RED-6** | `ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant` | a ruling: §3.4 does not say what an ordinal naming no variant means, and the two plans answer differently |
| **RED-7** | `ordinal-bound/paths-disagree/union-tag-past-the-last-arm` | the same ruling, for a tag naming no arm |
| **RED-8** | `kind-mismatch/compiled/a-moved-kind-is-decoded-anyway` | the run copy first — `angle` sits inside the window RED-3 clobbers. **KM1/KM2 holds the same respelling off that window** |
| **RED-9** | `bytes-compiled/dst-aux-swap/kind-14-walks-bytes-as-an-array` | TableFixedDst for `bytes(N)` vs the array compile path (reference-fix 10) |

### RED-3 is not like the others

**IT IS AN OUT-OF-BOUNDS READ, AND IT IS SILENT.** §3.4 requires the run copy to
be "OVERLAPPING UNALIGNED WORD MOVES AND NOT A CALL", and the reference's branch
for a run of more than sixteen bytes performs a sixteen, a second sixteen, and
then a THIRTY-TWO anchored at the run's END. For a run of 17..31 bytes that last
move is anchored BEFORE the run: it reads `32 - n` bytes in front of the source,
writes `32 - n` bytes in front of the destination, and reads up to `32 - n` bytes
PAST the record body — which for the last record of a file is past the buffer.
The sanitizer names it a `heap-buffer-overflow` inside `TableFixedRun`.

It wears five faces on `Scalars` alone — `span`'s high half, `weights_count`,
`seeds_count`, `pose.heading` and `spawn_present` — plus `tilt` at destination
offset zero on the compiled path, and every one of them comes back with **six
zero counters and a clean verdict**. A conformance suite that checked only the
report is blind to all six.

**AND THE DIVERGENCE POINTS THE WRONG WAY.** The branch is emitted for C++ and
for C and for nobody else, so a leg whose run copy is a plain `memcpy` — or that
has no such micro-optimization at all — is GREEN where the reference is red. The
reference is the odd one out, and only a shared conformance set says so.

**THE SANITIZED TWIN SKIPS THAT ONE FIXTURE**, because a halted process reports
nothing behind it, and the fault is WATCHED SEPARATELY instead of dropped:
`make tables-fixedform` runs the sanitized binary again with
`SCHEMA_FIXEDFORM_FAULT=1`, which puts the fixture back, and REQUIRES the
sanitizer to name the overflow inside `TableFixedRun`. The day the run copy is
fixed that gate goes red, and the skip and the gate are deleted together. A skip
nobody watches fail is a skip that has quietly become a hole.

---

## The count

| | |
|---|---|
| cells COVERED | 141 |
| cells filled by the first pass | 47, in 17 gaps |
| cells filled by the second pass | 16, in 7 gaps (**GAP-1, 2, 3, 4, 5, 13, 14**) |
| cells filled by this pass | 2 — a `was =` chain (`WC1/WC2/WC3`) and a kind-mismatch pair off RED-3's window (`KM1/KM2`). Neither was a numbered GAP |
| cells still open | 8, in 12 gaps — none of which can take a shared C++ oracle fixture today. 2 of them are the largest thing left (**GAP-16/17**, the stream and message carriers) |
| KNOWN-REDS | 9, each named and printed on a green run — 2 were DELETED when fix 12 landed on the branch; **RED-9** is new, the compiled `bytes(N)` walk. **RED-8** remains on Scalars; `km` holds the same cell off that window |
| KNOWN-FAULTS | 1, watched by a gate that goes red when it stops happening |
| oracle cases pinned | 34 |

---

## What moves this page

The same rule the goldens have. **A cell that changes from `GAP` to a fixture
changes here in the same commit as the fixture**, and a cell that changes the
other way is stop-the-line: a shape that lost its fixture is a shape nobody is
holding. A `RED` deleted from `test/tables/fixedform_main.cpp`'s `known_red[]` is
deleted from the table above in the same commit, which is what makes landing a
fix a two-file edit and not a one-file one.
