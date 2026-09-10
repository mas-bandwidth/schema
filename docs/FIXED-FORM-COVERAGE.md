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

- **`fx`** `fu` `fn` `wstr` `w` `bits` `s` `fl` `text` `frame` `p` `v` `neg`
  `layout` `fuzz` — the case functions of `test/tables/fixedform_main.cpp`.
- **`oracle`** — a pinned block of `testdata/conformance/tables/fixedform/records.dump`,
  written by `test/tables/fixedform_dump.cpp`.
- **`RED-n`** — covered by a fixture that is a NAMED KNOWN-RED: the fixture is in
  the shared set today, it fails today, and the fix it waits on is named. The
  make target prints every one of them on a green run. See *The known-red list*.
- **`GAP-n`** — no fixture. Every one is in *The gap register* with what it would
  take and why it is or is not filled here.
- **`—`** — the column does not apply to that row.

**THE SCHEMA PAIRS.** `FX1/FX2` the scalar edits; `FU1/FU2` text and a counted
array under a UNION ARM; `FN1/FN2` the bool, the optional, the enum ordinal, an
ARRAY OF UNIONS and a THREE-DEEP nesting; `V1/V2` the enum and keyed-array
edits; `W1/W2` `flags`, the text family's declared defaults and a TABLE renamed;
`Scalars/Scalars2` the widths; `F1/F2` the float bit patterns; `P1/P3` `?T`
against a plain nesting; `examples/Ranges` the `bits(N)` family.

---

## 1. The shapes: §3.4's `C` table, row by row

| shape (`C`) | IDENTITY | COMPILED (older) | COMPILED (newer) | WRITE |
|---|---|---|---|---|
| `bool` | `fn` + **RED-7** | `fn` | `fn` | `oracle fn1/full` |
| `int8`/`uint8` … `int64`/`uint64` | `fx`, `s` | `fx` | `fx` | `oracle fx1/root` |
| — the plain narrow kinds, spelled as themselves | **GAP-1** | **GAP-1** | **GAP-1** | **GAP-1** |
| `bits(N)`, N ≤ 32 and above | `bits` | **GAP-2** | **GAP-2** | `oracle bits/widths` |
| a RANGED integer | `fx`, `s` | `s` + **RED-3** | `s` | `oracle scalars/simstate` |
| `float32`, `float64` | `fl` | `fl` (widened) | `fl` | `oracle f1/floats` |
| a COMPRESSED float | **GAP-3** | **GAP-3** | **GAP-3** | **GAP-3** |
| `int128`/`uint128` | `s` | `s` | `s` | `oracle scalars/simstate` |
| `fixed(I,F)`, `ufixed(I,F)` | `s` + **RED-5** | `s` + **RED-5** | `s` | `oracle scalars/simstate` |
| `flags` | `w` | `w` (table renamed) | **GAP-4** | `oracle w1/vessel` |
| an ENUM, ordinal from `1`, `0` is `None` | `fn`, `v` | `fn`, `v` | `fn`, `v` (unknown variant) | `oracle fn1/full`, `v1/cfg` |
| — an ordinal PAST the last variant | **RED-8** | **RED-8** | **RED-8** | — |
| a nested `table`/`type`, INLINE | `fx`, `fn` | `fx`, `fn` | `fx` (unknown type), `fn` | `oracle fn1/full` |
| — nested THREE DEEP | `fn` | `fn` | `fn` | `oracle fn1/full` |
| `[N]T` | `s`, `fn` | `s`, `fn` | `s`, `fn` | `oracle scalars/simstate` |
| `[Min..Max]T`, count then MAX elements | `fu`, `fn`, `s` + **RED-5** | `fu`, `fn` | `fu`, `fn` | `oracle fu1/list`, `fn1/full` |
| — a count PAST the reader's `Max`, and a negative one | `fn` | `fn` | — | — |
| `string(N)`, length in BYTES | `text`, `v`, `fn` | `v`, `fn` (`was`) | `v`, `fn` | `oracle p1/{empty,short,full}` |
| `wstring(N)`, length in CODE UNITS, `2N` payload | `wstr` | **RED-1** | **RED-2** | `oracle wide/stamp`, `fu1/wide` |
| `bytes(N)`, length in BYTES | `w`, `fu` | `fu` + **RED-1** | **GAP-5** | `oracle fu1/raw`, `w1/vessel` |
| — TEXT OF ANY FLAVOUR UNDER A UNION ARM | **RED-1** | **RED-2** | **RED-2** | `oracle fu1/{wide,narrow,raw}` |
| a UNION: tag then the WIDEST ARM | `fu`, `fn`, `v` | `fu`, `fn`, `v` | `fu`, `fn` (unknown arm) | `oracle fu1/*`, `fn1/full` |
| — tag `0` is `None` | `fu` | `fu` | `fu` | `oracle fu1/none` |
| — a tag PAST the last arm | **RED-9** | **RED-9** | **RED-9** | — |
| — an ARRAY of unions, the guard re-tested per element | `fn` | `fn` | `fn` | `oracle fn1/full` |
| — an arm holding an ARRAY, and a narrower arm's slack | `fu`, `fn` | `fu`, `fn` | `fu`, `fn` | `oracle fn1/full` |
| `?T`: present flag + the payload WHOLE | `s`, `fn`, `v`, `p` | `fn`, `s` | `fn` | `oracle fn1/{full,absent}` |
| — an ABSENT optional over NON-ZERO residue | **RED-6** | **RED-6** | — | `oracle fn1/absent` |
| — a present byte that is neither `0` nor `1` | **RED-7** | **RED-7** | — | — |
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
| **content rules over the USED UNITS and nothing else** | `text` + **RED-4**, `wstr` | — | — | — |
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
| **op `clamp`** | — | **RED-3** (the op does not exist) | — | — |
| **op `ordinal`** | **RED-8** | `v`, `fn` | `v`, `fn` | — |
| a kind that MOVED is skipped and never misdecoded | `p` | `v`, `s` + **RED-10** | `s` | — |
| **the plan is PARTITIONED**: unguarded first, then the arms' | **GAP-13** | **GAP-13** | **GAP-13** | — |
| **the PREFILL answers "absent field"** | — | `fx`, `fn`, `v`, `w` | — | — |
| **the ABSENCE of an entry answers "unknown field"** | — | — | `fx`, `fn`, `s`, `fu` | — |
| an unknown NESTED TYPE stepped over by its whole size | — | — | `fx` | — |
| **the identity plan is a static constant, with ADJACENT RUNS COALESCED** | **RED-5**, **GAP-14** | — | — | — |
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
| TEXT UNDER A UNION ARM | the plan entry's `arg` lane carries an arm ORDINAL for a guarded entry and a text FLAVOUR for a text entry — and a text field under an arm needs both. No fixture in the set had text under an arm, so neither half was reachable. | `FU1/FU2`, `fu` — one arm per flavour in ordinal order, so ordinal and flavour disagree at every arm but the first. **RED-1**, **RED-2** |
| `wstring` HAS NO ORACLE BYTES ANYWHERE | flavour 2's length is in CODE UNITS and its payload is `2N` bytes — the one text row whose two numbers differ — and nothing pinned a byte of it | `wstr` + `oracle wide/stamp` (at the root) and `oracle fu1/wide` (under an arm), including a lone surrogate |
| `bytes(N)` UNDER A COMPILED PLAN | — | `fu` (the `raw` arm, read by both generations) |
| A TAG PAST THE LAST ARM | — | `fu`, `fn` (per element of an array). **RED-9** |
| AN ENUM ORDINAL PAST THE LAST VARIANT | — | `fn`. **RED-8** |
| A PRESENT BYTE OF `7` | — | `fn`. **RED-7** |
| A BOOL BYTE OF `2` | nothing in the set had a `bool` in a fixed root at all | `fn`. **RED-7** |
| OVER-`Max` COUNTS AND LENGTHS | — | `fn` (a count of 99 and a negative one), `text`, `wstr` |
| TEXT CONTENT VIOLATIONS | — | `text` (ill-formed UTF-8 and an interior zero, inside the USED bytes). **RED-4** |
| AN ABSENT OPTIONAL WITH NON-ZERO RESIDUE | — | `fn`. **RED-6** |
| AN ARRAY OF UNIONS | the arm guard has to be re-tested PER ELEMENT | `FN1/FN2`, `fn` |
| AN ARM THAT IS A NESTED TYPE WITH AN ARRAY | — | `FU1` `MarkList`, `FN1` `CellB` |
| THREE-DEEP NESTING | two deep was the deepest anything reached | `FN1/FN2`, `fn`, with the INNERMOST type resized in FN2 |
| RECORD COUNTS `0` AND MANY | every fixture read a file of exactly one record, leaving the framing arithmetic untested in both directions | `frame` — zero, three, every truncation, and one byte left over |
| `flags`, AND THE TEXT FAMILY'S DECLARED DEFAULTS | §3.4's `flags` row had no fixture, and neither did a `string`/`bytes`/`flags` default that is not zero | `W1/W2`, `w` + `oracle w1/vessel` |
| `bits(N)` AT ITS DECLARED STORAGE WIDTH | the row where this form spends bytes on purpose | `bits` + `oracle bits/widths` |
| A TABLE RENAMED UNDER `was` | the field-level pairs cannot reach a rename at the table's own level | `w` (a W1 `Vessel` read into a W2 `Ship`) |

### Open, and why

| gap | why it is open |
|---|---|
| **GAP-1** the plain narrow integer kinds (`int8`, `int16`, `uint8`) spelled as themselves | Their STORAGE IMAGE is exercised — a `fixed(4,4)` is an `int8` and a `bits(16)` sits in a `uint32` — so what is missing is the DECLARATION and not the wire. Low value beside the cost of a tenth schema pair. |
| **GAP-2** a `bits(N)` edit across generations | `examples/Ranges` has one generation. A second would be a new pair for a kind whose storage width is fixed by the declaration and cannot move without moving the kind. |
| **GAP-3** a COMPRESSED float | §3.4 says it "rides as the float, not as a quantized index". No fixed-form unit in the tree declares one, so this needs a schema. **A leg could get this wrong silently** — it is the one open gap that meets the bar, and it is open only because the batch ran out. |
| **GAP-4** a `flags` edit across generations | W2 renames the TABLE and leaves `caps` alone. A flags field gaining a bit is not a wire event — the mask is raw — so the silent-wrongness bar is not met. |
| **GAP-5** a `bytes(N)` edit across generations | `bytes(N)` against `*bytes` is a kind mismatch §4 already covers on form 1; on this form the bound is the only thing that can move and a bound is not wire identity. |
| **GAP-6/7/8** the 65536 COMPILE refusal, the 4096 warning, `--fixed-record-limit` | All three are COMPILER verdicts and none exists in this tree yet: there is no `fixed table` keyword (§3.4 names #823) and no flag. The READ side's own 65536 bound is covered by `layout`. A missing compile refusal is loud on first build, not silent. |
| **GAP-9** a DEPRECATED slot | §3.4 names #823 and #825 as the marker and its baseline lock; the compiler has no `deprecated` at all today. Nothing to write a fixture against. |
| **GAP-10** what a fixed table cannot carry | Four compile refusals by name. Loud, not silent. |
| **GAP-11/12** selection by the keyword, and the read side accepting both forms | Both wait on #823. Until then the compiler selects by the DERIVED mode, which §3.4 says in as many words. |
| **GAP-13** the plan's PARTITION | `FnRootFixedPlanGuarded` is emitted and nothing asserts it is the boundary it claims. §3.4 calls the partition "A REQUIREMENT AND NOT AN OPTIMIZATION", so an unasserted requirement is exactly the shape of a rule that quietly stops holding — **but a broken partition is a wrong VALUE, which the value assertions already catch**, and its cost is a benchmark's job. |
| **GAP-14** ADJACENT RUNS COALESCED | Nothing asserts the identity plan's entry count is the coalesced one. **RED-5** is a consequence of coalescing and is the only thing watching it. Worth an assertion against `FixedPlanCount` once the run copy is fixed. |
| **GAP-15** a compiled plan CACHED BY HASH | The C++ reference's `FixedLoad` compiles per call from the caller's plan storage; there is no cache to test. A cache is a performance property, and the measurement gate is where it belongs. |
| **GAP-16/17** the STREAM and MESSAGE carriers | §3.4's framing table has three carriers and every fixture here is a FILE. Both are real gaps with real silent failure modes — an announcement sent twice, a batch whose bodies carry a hash the announcement never named — and both need §3.3's machinery wired to form `3` first. **The largest open gap on this page.** |
| **GAP-18** a caller capacity below the file's record count | §3.4 states NOTHING about it. The reference refuses `batch_too_large` by precedent from §3.3, and `frame` now pins that — but a pin is not a rule, and §3.3 also requires the reader to hand the caller the count it was short by, which `FixedLoad` has no way to do. **A question for the spec, not a bug.** |
| **GAP-19** the GENERATOR BOUND on the identity plan | §3.4 names it as the generator's and not the wire's: a type whose leaves do not fit one plan does not carry the form. No fixture reaches the bound, and reaching it means a large array of a type carrying text, a count, a union or an optional. |
| **A `was =` CHAIN** | `ir.Field.WasName` is a SINGLE name, so a field renamed twice — `label` → `caption` → `title` — carries the SECOND name's hash and a first-generation record no longer resolves. One hop is covered (`fx`, `fn`, `w`); a chain **cannot be expressed**, and that is a declaration-side answer rather than a missing test. Worth a ruling: it is not fixed-form-specific, but the fixed form is where losing a field costs a whole positional slot. |
| **A TABLE EXACTLY AT 65536** | The READ side's bound is covered (`layout` refuses 65537 and a size that would wrap). Exactly-at-the-bound on the WRITE side needs the compile refusal of GAP-6. |

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
| **RED-1** | `arg-lane/identity/text-flavour-under-an-arm` | reference fix 12 (the plan entry's `arg` lane) |
| **RED-2** | `arg-lane/compiled/text-dropped-under-an-arm` | reference fix 12 (the plan entry's `arg` lane) |
| **RED-3** | `clamp-op/compiled/tightened-bounds-do-not-clamp` | a `clamp` op in the reference's read loop (§3.4's op table names one; the op set has none) |
| **RED-4** | `text-content/identity/invalid-utf8-is-not-malformed` | UTF-8 validation in the reference's `text` op |
| **RED-5** | `run-copy/identity/a-17-to-31-byte-run-clobbers-its-neighbours` | the run copy's 17..31-byte branch, `internal/codegen/cpptable/fixedruntime.go` and ctable's twin |
| **RED-6** | `optional/absent-payload-residue-is-copied` | the `?T` payload gated on the present byte (§3.4: "IGNORED on read") |
| **RED-7** | `bool-domain/a-byte-outside-0-and-1-lands-in-the-caller-s-bool` | a ruling on what a bool byte outside `{0, 1}` means, and a normalise to match it |
| **RED-8** | `ordinal-bound/paths-disagree/enum-ordinal-past-the-last-variant` | a ruling: §3.4 does not say what an ordinal naming no variant means, and the two plans answer differently |
| **RED-9** | `ordinal-bound/paths-disagree/union-tag-past-the-last-arm` | the same ruling, for a tag naming no arm |
| **RED-10** | `kind-mismatch/compiled/a-moved-kind-is-decoded-anyway` | the run copy first, then a re-read — `angle` sits inside the window RED-5 clobbers and the two cannot be told apart until it is fixed |

### RED-5 is not like the others

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
| cells COVERED | 118 |
| cells filled by this pass | 47, in 17 gaps |
| cells still open | 24, in 19 gaps — 1 of which meets the silent-wrongness bar (**GAP-3**) and 2 of which are the largest thing left (**GAP-16/17**, the stream and message carriers) |
| KNOWN-REDS | 10, each named and printed on a green run |
| KNOWN-FAULTS | 1, watched by a gate that goes red when it stops happening |
| oracle cases pinned | 22 |

---

## What moves this page

The same rule the goldens have. **A cell that changes from `GAP` to a fixture
changes here in the same commit as the fixture**, and a cell that changes the
other way is stop-the-line: a shape that lost its fixture is a shape nobody is
holding. A `RED` deleted from `test/tables/fixedform_main.cpp`'s `known_red[]` is
deleted from the table above in the same commit, which is what makes landing a
fix a two-file edit and not a one-file one.
