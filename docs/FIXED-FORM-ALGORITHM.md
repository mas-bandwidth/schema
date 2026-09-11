# The fixed form as an algorithm

**This is the document a port implements form `3` from.** §3.4 of `docs/SPEC-TABLES.md` states the wire and its
reasons; this page states the same wire as steps, in one notation, so nine languages write the same code. Where
the two disagree §3.4 is the law and this page is the bug — except at the footnotes, which are rulings neither
§3.4 nor the C++ reference has caught up to. **The reference is not the specification**: it has accidents, and
the ports that transliterated it inherited them as law. Implement from this page, prove against the oracle (§7),
open the C++ only for a byte question.

## 0. The notation

```
u8 u16 u32 u64 i32 i64     unsigned/signed integers of that width
LE(w, p) / SLE(w, p)       the unsigned / signed integer in the w bytes at p, little-endian
PUT(w, p, v)               store v into the w bytes at p, little-endian
COPY(dst, src, n)          move n bytes; dst and src never overlap
ZERO(p, n)                 write n zero bytes at p
struct N { f : T ; ... }   a record of named fields
for i in 0 .. n            i takes 0, 1, ... n-1
REFUSE name                stop; decode nothing; move no counter; report `name`
COUNT c                    add one to a §4 counter
```

Every integer is little-endian; nothing is aligned or padded between fields. `REFUSE` is total: no plan compiled,
no value written, `malformed` not set. The §4 counters are `unknown`, `kind_mismatch`, `widened`, `clamped`,
`duplicate` and `malformed`; this form raises all but `duplicate`.

## 1. The layout

The writer's whole type, flattened. Sent once per carrier, never in a record.

```
struct Entry { id : u64 ; kind : u8 ; size : u32 ; children : u32 }   // 17 bytes
layout := u32 count , count * Entry
```

Entry `i` begins at `4 + 17*i`; within it `id` at +0 (8), `kind` at +8 (1), `size` at +9 (4), `children` at +13
(4). `id` is `fnv1a64(wire name)`, §5's id and nothing else; `children` is the IMMEDIATE child count, never a
subtree extent. **There is no version byte inside a layout** — the form byte versions the layout's own format.
**The walk is pre-order**: entry `0` is the root, kind `13`, and each entry is followed immediately by its
`children` children, each followed by its own — so a child's index always exceeds its parent's, **the
representation cannot express a cycle**, and there is no cycle rule to write.

| kind | children, in declared order |
|---|---|
| `13` table | its fields |
| `14` array | one: the element |
| `15` union | its arms; an arm's ordinal is its position from `1` |
| `16` enum-keyed array | two: the key enum (kind `30`), then the element |
| `30` enum | its variants, each kind `32` size `0`; ordinal is position from `1` |
| `35` optional wrapper | one: the payload |
| anything else | none |

**`bytes(N)` is walked as an ARRAY OF `u8`** — kind `14` with one synthetic child at kind `6`, size `1` — never
as a text kind; only `string(N)` (`12`) and `wstring(N)` (`33`) are text kinds in a layout. **The hash** is
`h := 0xcbf29ce484222325; for each byte v: h ^= v; h *= 0x100000001b3` over the layout's bytes as written,
**the 4-byte count included**, and then over the DEFINITIONS DIGEST (§5.2) — the facts a range, a `bits(N)`, a
`fixed(I,F)` split or a `flags` bit count states and the layout's sizes do not, which never ride the wire. An
empty digest leaves the hash the layout's alone. It is a wire identity, never a security claim (fix 8). **The closed
kind set is `1..30`, `32`, `33`, `35`** — `31` (§3's framing escape) and `34` (reserved) are not in it, and a
kind outside it names a form this reader never saw: `REFUSE layout_kind_unknown`, never stepped over.

**THE KIND CODES, the whole closed set.** A port needs the NUMBERS and not only the set's shape, and they are
wire format and frozen (`ir/tablekind.go:17-62`, `ir/tablewire.go:117-127`, `ir/buildversion.go:56`,
`ir/fixedform.go:40`; the ladder rungs at the reference: fixedruntime.go:84-95).

| code | kind | code | kind |
|---|---|---|---|
| `1` | bool | `18` | i128 |
| `2` `3` `4` `5` | i8, i16, i32, i64 | `19` | u128 |
| `6` `7` `8` `9` | u8, u16, u32, u64 | `20`–`24` | `fixed(I,F)` SIGNED, at 8/16/32/64/128 storage bits |
| `10` `11` | f32, f64 | `25`–`29` | `ufixed(I,F)` UNSIGNED, the same five widths |
| `12` | string(N) | `30` | enum |
| `13` | table | `31` | §3's framing escape — **NOT in the closed set** |
| `14` | array — and `bytes(N)`, as an array of `6` | `32` | a variant, or a payload-free arm: size `0`, no children |
| `15` | union | `33` | wstring(N) |
| `16` | enum-keyed array | `34` | reserved (float16) — **NOT in the closed set** |
| `17` | a `*T` pointer index | `35` | the OPTIONAL wrapper — a LAYOUT kind, the one §3.4 adds to §3's set |

`0` is the reserved value no declaration spells. `17`, `18`, `19` and both fixed-point runs ARE in the closed
set and a stranger may send them; a pointer inside a fixed table's own closure is a compile refusal (§5.5), so
`17` reaches a reader only from a peer, where it is a kind mismatch against whatever the reader names there.
**The widening ladder runs INSIDE a family and upward only** — `2..5`, `6..9`, `20..24`, `25..29`, and
`10 -> 11` — so `6 -> 4` (u8 into i32) is a kind that MOVED and is reported, not a widen.

### 1.1 The seven rules, before a single record byte

| # | rule | refusal |
|---|---|---|
| 1 | `4 + 17*count` equals the layout's stated length, and `count != 0` | `layout_count_mismatch` |
| 2 | every `kind` is in the closed set | `layout_kind_unknown` |
| 3 | a `size` matches its kind (§1.2) | `layout_size_mismatch` |
| 4 | a kind is used as its definition allows (§1.2) | `layout_kind_invalid` |
| 5 | the pre-order walk consumes exactly `count` entries, ending on the last | `layout_tree_unclosed` |
| 6 | no entry's size, and no partial sum of a parent's children, passes 65536 | `layout_record_too_large` |
| 7 | nesting does not pass the reader's own walk bound (the reference states 64) | `layout_too_deep` |

Bytes that are not a layout at all — shorter than the 4-byte header, or absent — are `layout_malformed`, the
residue after the seven and not a bucket they fall into, and a layout that fails any rule **sets nothing**. **The
order is load-bearing**: rule 1; the root's kind (`13`, else rule 4) and size (nonzero, within 65536, else rule
6); then the walk — per entry, index in range (5), depth (7), **kind known first** (2), own size within 65536
(6), recurse, then the size and shape rules. Keep the FIRST reason and stop. **The subtree walk is iterative**,
or a chain of single-child entries is a stack depth the wire chooses.

### 1.2 What a size and a shape must be

`sum` totals an entry's children's sizes, `widest` is the largest, `elem` is the one child's size, and an ordinal width is one of `1, 2, 4, 8`.

| kind | admitted size | shape |
|---|---|---|
| every LEAF (`1`–`11`, `17`–`29`) | the width its kind fixes — §3's `C` table — except `6`/`7`, which admit `4` as well as `1`/`2` because a `bits(N)` rides at its declared storage width | no children |
| `12` string(N) / `33` wstring(N) | `>= 4`; for `33`, `size-4` also even | no children |
| `13` table | `== sum` | — |
| `14` array | `size % elem == 0`, **or** `size >= 4 and (size-4) % elem == 0` | 1 child, `elem != 0` |
| `15` union | `size > widest`, and `size - widest` is an ordinal width | at least 1 child |
| `16` enum-keyed array | `size % elem == 0` and `size / elem >= key.children` | 2 children, first kind `30` |
| `30` enum / `32` variant or empty arm | an ordinal width / `0` | `30`'s children are all kind `32`, or none; `32` has none |
| `35` optional | `== sum + 1` | exactly 1 child |

`>= key.children` and not `==`: an enum widened by an explicit `max` has more slots than names.

## 2. The file and the framings

**The form byte and the layout each appear ONCE PER CARRIER; a record carries only its hash** — a record is never self-describing, in any carrier.

```
a FILE:
  0        form byte, 3
  1 .. 7   reserved, written zero
  8 .. 15  the layout hash (u64 LE)
  16 .. 19 the layout's LENGTH IN BYTES (u32 LE)
  20 ..    the layout — whose OWN first four bytes are its entry count — then
           records, back to back, to the end of the file
a record:  u64 hash , the root's body
```

Two `u32`s meet at 16 — the layout's byte length, then its entry count; confusing them is a port bug. **The
load, in order:**

| # | step |
|---|---|
| 1 | fewer than 20 bytes: `malformed` |
| 2 | `b[0] != 3` — refuse **by direction**: `1` is `previous_form` (the variable form is OLDER), `2` is `message_form_as_file`, anything else `newer_form`. From the other end, a form-`1` reader given `3` refuses `newer_form` |
| 3 | `L := LE(4, b+16)`; if `20 + L > bytes`, `REFUSE layout_malformed`. **The hash is `LE(8, b+8)`, the header's own** — §5.3 took the recompute out, the definitions digest not being on the wire |
| 4 | **`h` selects the plan (§5.3)**: this build's own hash takes the baked identity plan and record size; another hash of the lineage takes that entry's plan, its layout bytes compared and its record size read from the lock; a hash in no entry is `layout_newer` |
| 5 | **RETIRED by §5.3**: there is no third thing to check. The byte comparison against the lock's own layout holds the header and the layout together, and a known hash over different bytes is `layout_malformed` |
| 6 | `rest := bytes - 20 - L`; if `record_bytes <= 8` or `rest % record_bytes != 0`, `malformed` — bytes left over means the two ends of the file have met. If `rest / record_bytes` passes the caller's capacity, `REFUSE batch_too_large` |
| 7 | per record: if `LE(8, at) != h`, `REFUSE no_layout`; then §4.4 |

**A STREAM (§3.3)** framed the form byte, so a record is hash + body alone and the layout rides the
announcement, once per hash, before the first record carrying it. **A peer may announce more than one layout,
one per hash** — the form's one departure from §3.3's "no re-announcement, ever", a layout being NAMED BY ITS
HASH; a second for a hash already held is refused by name. **THE MESSAGE FORM (§3.3), planned**: one form byte
per batch, the layout on its announcement. **Inside a packet no form byte is written at all**, the one exception.

## 3. The record and the writer

```
body := the type's fields, IN DECLARED ORDER, each at its constant size
```

No per-field reference, no kind byte, no length, no terminator, no trailer. Every field rides as its **declared
storage image, little-endian, at its declared storage width**, nothing padded between fields. The width is the
DECLARATION's, which SPEC.md fixes identically in every port, so the record is a language fact and not a
backend's. `C(f)` is a field's constant size, `C(T)` the sum of a type's fields'; both are compile-time
constants, so measuring never reads a value.

| field | `C` | order on the wire |
|---|---|---|
| `bool` | 1 | `0` or `1` on write |
| `int8`..`uint64`, and a RANGED integer | the declared storage width | the bounds do not ride |
| `int128` / `uint128` | 16 | the low 64-bit half, then the high |
| `bits(N)` | 4 for `N <= 32`, else 8 | — |
| `float32` / `float64`, and a COMPRESSED float | 4 / 8, and 4 | the IEEE-754 bit pattern, no canonicalisation; a compressed float rides as the float and never as a quantized index |
| `fixed(I,F)` / `ufixed(I,F)`, `flags` | the storage width, and 8 | the raw scaled integer; the raw mask |
| an ENUM | the ordinal width `1/2/4/8`, from the enum's top wire value (note a) | the ordinal is the variant's POSITION FROM `1`; `0` is `None` |
| a nested `table` / `type` `T` | `C(T)` | inline; no length, no terminator |
| `[N]T` | `N * C(T)` | no count rides; every element is live |
| `[Min..Max]T` | `4 + Max*C(T)` | **the count (i32 LE) THEN `Max` elements** |
| `string(N)`, `bytes(N)` / `wstring(N)` | `4 + N` / `4 + 2N` | the length (i32 LE) THEN the payload — in BYTES for the first two, in CODE UNITS for `wstring` |
| a UNION | `tag width + max(C(arm))` | **the tag THEN the widest arm**; tag `0` is `None` |
| `?T` | `1 + C(T)` | **the present flag THEN the payload, which rides WHOLE** |
| `[Enum]T` | `slots * C(T)` | every slot live, in the key enum's declared order; no key rides |

No text field carries a terminator on the wire, however the language stores one. A pointer, a map, an unbounded
`[]T` and a guarded branch are refused in a fixed table by name (§2.2, §11). **Kind `35` is a LAYOUT kind, not a
wire kind**: §2.3 makes `?T` and a plain `T` wire-identical on form `1`, but **here they are one byte apart**, so
the edit reads as `kind_mismatch` rather than every byte after it sliding by one.

### 3.1 The write

```
WRITE( values, out ):
    header: form byte, seven zero bytes, the hash, the layout length, the layout
    per record: PUT(8, at, HASH) ; ZERO(at+8, C(root)) ; store each field at its constant offset
```

The template is a compile-time constant — the hash, then **zero everywhere a value lands** — which zero-fills
every byte of declared slack without the writer touching it. One copy and a straight line of stores: no
measuring pass, no id interning, no trailer, no second walk. A count, length, present flag or union tag always
stands IN FRONT of the slack behind it, so **the slack rule is one rule**: write the live extent onto the zeroed
template and stop. **Arrays** write `count` elements and **text and bytes** `length` units, never all `Max` —
which would put an unused slot's STORAGE on the wire, for an element with declared defaults its default image, a
value nobody wrote (fix 1). **A union** writes the tag and the taken arm only, zero behind a narrower one. **An
absent optional** writes flag `0` and **skips the payload store**: a declared default under a clear flag is
meaning too (fix 13).

**Write-side bound checks are DEBUG ONLY, by rule.** `count <= Max` and `length <= N` are a caller contract; a
release build removes them exactly as it removes `assert`, pays nothing, and **clamps nothing**. The READ side
keeps the wire safe: it checks the same number in every build and counts the clamp. **Slack is UNSPECIFIED on
read and not a refusal**: a reader validates the USED UNITS only, and non-zero slack is not `malformed`, not a
refusal, and moves no counter.

## 4. The reader: ONE path

One loop over one plan, whether the writer is this build or a stranger: no strict flag, no fast path, no second
reader.

### 4.1 The plan

```
struct Op {
    src   : u32   // a byte offset into the RECORD's body
    dst   : u32   // a byte offset into the READER's storage, or into its record image
    size  : u32   // bytes, or a bound — per op, below
    aux   : u32   // a SECOND destination, a side-table offset, or a constant
    guard : u32   // the offset of the TAG BYTE this entry is conditional on, or NO_GUARD
    op : u8 ; arg : u8 ; meta : u8 ; dstsize : u8 ; sign : u8
}
// arg is THE GUARD'S ORDINAL and nothing else; meta is THE OP'S OWN ARGUMENT (a text flavour).
```

**`arg` and `meta` ARE TWO LANES BECAUSE THEY ARE TWO FACTS, and must never share one.** (fix 12) A `string(N)`
under a union arm needs both, and one lane gives whichever was stamped last: a byte string under arm `2` read as
WIDE, or an entry that runs only when the tag equals the flavour — **under the wrong arm**. Neither is visible
to this build's own records; only to a peer's.

| op | `src` | `dst` | `size` | `aux` | other |
|---|---|---|---|---|---|
| `copy` | source offset | destination offset | bytes | — | — |
| `count` | the count field | the count's storage | **the reader's own bound, in ELEMENTS** | — | — |
| `text` | the length; payload at `src+4` | the length's storage | the payload's BYTE span, `min(mine, theirs)` | **the buffer's offset** | `meta` = flavour |
| `ordinal` | the source ordinal | the destination | the source ordinal's width | the remap table's offset | `dstsize` |
| `widen` | source | destination | the source width | — | `dstsize`, `sign` |
| `widenf` | source (4) | destination (8) | — | — | f32 into f64 |
| `const` | — | the tag's storage | the tag width | **the constant**: this reader's own arm ordinal | `guard`, `arg` |

Flavours: `1` utf8, `2` wide, `3` bytes. An **identity plan carries only `copy`, `count` and `text`** — an enum
ordinal and a union tag ride inside plain `copy` runs there, so the range closing of §4.5 must live in the
SCATTER and not only in `ordinal`. **A `bytes(N)` takes the ARRAY's row, not a text field's — the count to `aux`
and the elements to `dst`**, where a text field's row is the other way round (fix 10). Backwards, a plan compiled
from a stranger's layout gets a count destination that is the BUFFER'S FIRST FOUR BYTES and an element
destination that is the LENGTH FIELD, and no record this build wrote can see it.

**The plan is PARTITIONED: every unguarded entry first, then every guarded one, and the plan states where the
second half starts.** Entries
are independent and a union's arms mutually exclusive, so the order is free, and the entries that are nearly all
of a plan never test a guard. That, **the run copy as bounded branch-free word moves and not a call**, and **the
loop body inline in both halves** are three requirements and not optimizations, each bought with a measurement
(63.5 ns against 55, 3.1x against 1.37x, and 87 ns against 55) (fix 6). **A union's guard is stamped over everything
its arm produced** — an arm inside an arm answers to the OUTER tag — and a nested union's own arm selection must
survive that stamp.

### 4.2 Which plan, and where it comes from

**The identity plan — the build's own hash — is BAKED.** The schema compiler does the walk once and every backend
lays the finished array down as static data — one answer for every port, and the only way a language with no
compile-time evaluation carries this form. Two shapes are permitted, both the coalescing rule taken to its end:

- **coalesced runs straight into the reader's storage.** Merge two entries only when both are `copy`,
  `guard`/`arg`/`meta` agree, and `src` and `dst` both advance by `size`. An array of a FLAT element type — one
  whose storage image is its wire image — is ONE run however many elements it holds. **The fold holds only where
  the writer's element width and the reader's are EQUAL: across a widening there is no fold, and each element is
  its own widen entry** (§5.9 #33).
- **one whole-body copy into a RECORD IMAGE, then a straight-line scatter.** In the image domain the record's
  declared order IS the destination's order, so every run merges and the plan is one entry — the shape a language
  takes when its storage is not the wire's: a `string(N)` stored as `N+1` units, a union that is a real tagged enum.

Coalesce inside each half, **never across the split**. **For any other hash the SAME loop runs over a plan
compiled once from the writer's layout, and CACHED BY HASH.** (note b) **§5 moves that compile to build time and
the layout it walks to the LOCK's own bytes** — a file's layout is compared, never parsed — so read the two
paragraphs below as what a plan IS, and §5.2 as where it comes from. The compiler walks the layout against the
reader's own descriptors, resolves each id to a field of its own or to nothing, and emits one entry per landing
field with the op the pair calls for. **Every evolution decision of §5 is made HERE, once per peer** — the whole
`unknown` census included, which is why the read loop never raises `unknown` again per record.

**The plan's storage is the CALLER's, declared by capacity; the codec never allocates**, and a plan that does not fit
is `REFUSE plan_too_large`. **The plan is compiled only after the layout has passed every rule of §1.1**, so no
arithmetic here runs on a size nobody checked. And **every compiled entry is bounded by the WRITER'S OWN
DECLARED RECORD SIZE** — `src + size`, plus 4 for a text entry's length word, and a `guard` offset too, all
within `root.size`. A layout reaching past it is **refused WHOLE and never partly compiled**, under
`layout_record_too_large` (fix 3): the read side's whole defence.

### 4.3 The prefill

**Prefill the bytes the plan does not write.** The plan compiler computes the UNWRITTEN RANGES — the destination
bytes no entry covers — and only those take the declared defaults before the loop; **for the identity plan that
list is EMPTY**, so the identity read pays no prefill (fix 15). That answers "absent field" — no plan entry, so
the field keeps what the prefill put there — and "unknown field": a field this reader cannot name is never a
source, its bytes stepped over because the next entry's `src` is past them. **Skipping is not an act.**

### 4.4 The run

```
READ( plan, split, record, out ):
    prefill the unwritten ranges of out
    for i in 0 .. split          : APPLY( plan[i] )
    for i in split .. plan.count : if record[plan[i].guard] == plan[i].arg : APPLY( plan[i] )
```

`split` is the number of UNGUARDED entries — the index the second half starts at, not a count of guarded ones.

### 4.5 The scatter

The reader validates on load in **every** build, release included, and a clamp counts ONCE for the field.

| case | what it does, and what it validates |
|---|---|
| `copy` | `COPY(out+dst, record+src, size)`. Nothing to validate |
| `count` | `v := SLE(4, record+src)`; if `v < 0` then `v := 0, COUNT clamped`, else if `v > size` then `v := size, COUNT clamped`; `PUT(4, out+dst, v)`. The bound is the READER's own `Max`, in ELEMENTS |
| `text` | `unit := (meta == wide) ? 2 : 1`; `cap := size / unit`; `v := SLE(4, record+src)` clamped into `[0, cap]`, `COUNT clamped` if it fired. Copy the payload, and terminate at the used length where the language stores a terminator — never for `bytes`. **The CONTENT RULES apply to the USED UNITS and nothing else**: UTF-8 validity over `v` bytes, never over `N`; wide code units over `v`, never `2N`, an astral pair counting two (fix 7). **A content violation REFUSES BY NAME, the verdict the packet reader gives**, this form having no `L` to continue past (fix 11) |
| `ordinal` | `raw := LE(size, record+src)` **through a 64-bit temporary**, an ordinal width of `8` being admissible; the remap table's first entry is its length `n`; `v := (raw != 0 and raw <= n) ? table[raw] : 0`; `PUT(dstsize, out+dst, v)` |
| `widen` / `widenf` | `raw := LE(size, record+src)`, sign-extended from `size*8` bits when `sign`, then `PUT(dstsize, out+dst, raw)`; `widenf` is the f32 at `src` as an f64 at `dst`. Both `COUNT widened`, and both are exact by construction, NaN payloads included |
| `const` | `PUT(size, out+dst, aux)` — the guarded entry landing THIS reader's arm ordinal when the writer's tag says that arm rode |
| a `bool` or a PRESENT FLAG | lands as **`byte != 0`**, normalised to the language's own true. `0x02` is not a bool a reader stores verbatim (fix 2) |

### 4.6 The bounds pass

**A record is a positional image and the read loop moves bytes: it asks nothing about what they mean.** Two
things a declaration bounds are held by nobody in §4.5 — a RANGED SCALAR's min and max, and an ORDINAL's set: a
union tag past the arm count, an enum ordinal past the enum's top value. They are held by **STRAIGHT-LINE CODE
AFTER THE PLAN RUN, and NOT BY PLAN ENTRIES**, an entry per bounded field being a test on every read of every
record. **THE PASS RUNS OVER STORAGE, which is what makes ONE pass cover BOTH plans**: both land values in the
same places, so a compiled plan needs no op of its own. It walks only **what a read can have written** — a
counted array's LIVE elements and never its slack, an optional's payload only when the present byte says so —
because the prefill's defaults are in range by construction, and clamping storage nobody wrote would count a
clamp on every clean read. A type that bounds nothing emits no pass at all.

- **A RANGED SCALAR** clamps to its declared min and max, `COUNT clamped`. **A fixed-point field's bounds are in
  VALUE UNITS and its storage is raw**, so both ends are shifted by `F` first; a `bits(N)` clamps to `2^N - 1`.
- **A BOUNDED FLOAT'S LOW TEST IS `!(v >= min)`, NEVER `v < min`.** IEEE makes every ordered comparison against
  a NaN false, so a NaN in a `float32 | min, max` — or in a COMPRESSED float, which rides here as the float it
  is — fails `v < min` and `v > max` both, and the integer shape would let it land WHOLE and count NOTHING.
  **A NaN LANDS `min` AND COUNTS ONE, exactly as `-inf` does**, quiet and signalling alike; the bits are not
  inspected, only the comparison. And `-0.0` against a `min` of `+0.0` compares EQUAL, so it is in range and
  lands AS WRITTEN with its sign bit, counting nothing. A leg whose `clamp`/`min`/`max` intrinsic PROPAGATES a
  NaN (Rust's `f32::clamp`) may not use it here.
- **A UNION TAG past the arm count, or an ENUM ORDINAL past the enum's top value, lands `None`** — the same
  nothing an unset union holds — and `COUNT clamped` (fixes 5, 9 and 14). There is no `| max = K` headroom on
  this wire: a variant is identified by the hash of its name, so a value with no name has no meaning here, and the
  compiler refuses a headroom enum the moment a table reaches it (Glenn, 2026-09-10: dead text, not a rule).

## 5. Evolution: the fixed table reads backward, never forward

The bill is `docs/FIXED-FORM-BILL-READS-BACKWARD.md`; the spec is SPEC-TABLES §21. This section is the
algorithm. Glenn, 2026-09-10: "Newer versions of the fixed table should be able to read OLD versions. But
old versions CANNOT read new versions, and complain loudly and refuse." The order of work: basic versioning
on the fixed table first (this section), then the bitpacked fixed form; the variable table is HELD.

Three procedures. `BASELINE` runs at commit and holds the law; `COMPILE` runs at build time from the lock and
lays the lineage down as static data; `LOAD` runs per file and **selects by hash**. **Nothing parses a
stranger's layout, on any path.** A hash the lineage does not hold is a refusal, and a hash it does hold is
read under the layout bytes THE LOCK recorded — the file's own layout bytes are never walked, only compared.

This section is written FROM the green reference (`internal/codegen/cpptable`, `ir/fixedform.go`) and it
SUPERSEDES §2's load table at steps 3 to 5: the header's hash is **taken as given**, not recomputed from the
layout behind it, and the layout is held to a byte comparison instead of to §1.1's seven rules. §5.8 lists
where the reference still owes this page; a port implements THIS page.

### 5.1 BASELINE(old, new): the monotone law, at commit

`old` is the last locked shape of the unit (`schema.lock`, SPEC §2.10 as superseded by the bill's §6 and §12.1; the comparison is MONOTONE per row, on EVALUATED values); `new` is the unit being committed. For every fixed
table `T` in `new` and every definition `D` in `T`'s closure (§5.5), with `D0` the same definition in `old`
where it existed:

```
BASELINE(old, new):                      -- at a merge commit, run once per parent lock (bill §11.3)
  for T in new.fixed_tables:
    if T not in old:                    continue            -- a new table is a new lineage
    if old[T] is not fixed:             REFUSE "T: fixed added" -- a different form, not a version
    for D in closure(T):
      D0 := old.lookup(D.name); if none: continue           -- added: allowed
      WIDENS(D0, D) or REFUSE "T: D: <rule> (<old> -> <new>)"
  for T in old.fixed_tables:
    if T not in new or new[T] is not fixed:
      if not retired(old[T]):           REFUSE "T: fixed removed"  -- retire first (bill §11.5); the lineage stays

WIDENS(a, b):                            -- b may replace a
  LADDER(a, b) or kind(a) == kind(b)     or FAIL "kind changed"       -- LADDER: int into wider int same signedness, f32 into f64,
                                                                       -- ordinal/tag width wider, bits(N) into wider storage,
                                                                       -- fixed(I,F) into wider I at equal F (bill §12.2);
                                                                       -- string/wstring/bytes and [N]/[..N]/[Enum] never cross
  match kind:
    table, type:   fields(a) is a SUBSET of fields(b) by NAME and a SUBSEQUENCE of it by position (append-only per
                   branch, any interleaving after a merge; bill §11.3); every a-field WIDENS into its b-field;
                   a deprecated field keeps its place          or FAIL "field removed | inserted | reordered | modified"
    enum:          variants(a) prefix of variants(b) by name   or FAIL "variant removed | inserted | reordered | renamed"
    union:         arms(a) prefix of arms(b) by name; each payload WIDENS   or FAIL "arm ..."
    flags:         flags(a) prefix of flags(b)                 or FAIL "flag removed | moved"
    [N], [..N], string(N), wstring(N), bytes(N):  N(a) <= N(b) or FAIL "bound narrowed"; element WIDENS
    [Enum]T:       Enum WIDENS (its own rule); element WIDENS
    int, float:    width(a) <= width(b), same ladder, same signedness   or FAIL "narrowed | ladder | signedness"
    ranged:        range(b) ⊇ range(a), or b unranged               or FAIL "range narrowed | added"
    bits(N):       N(a) <= N(b)                                     or FAIL "bits narrowed"
    fixed(I,F):    I(a) <= I(b) and F(a) == F(b)                     or FAIL "F changed"
    ?T vs T:       optional(a) implies optional(b)                     or FAIL "optional removed"
    default:       default(a) == default(b)                            or FAIL "default changed"   -- SPEC §18
    reader limit:  limit(a) <= limit(b)
    plan cap:      for every supported older entry x, PLAN(x, b) fits plan_too_large and the leaf cap
                                                                    or FAIL "plan too large for lineage entry x" (bill §12.10)
    deprecated:    deprecated(a) implies deprecated(b)                 or FAIL "undeprecated" (one way, bill §12.3)
```

Every FAIL names the table, the definition, the rule, and both values. The compiler refuses to generate
against a lock the schema contradicts; CI runs the same check. **A rename THROUGH `was =` is not a version
at all**: an id is the hash of the wire name and `was` keeps the old one, so the layout bytes do not move and
the two generations share one hash. **A deprecation is not a version either** — the slot is still written
and still read (bill §12.3).

### 5.2 COMPILE(lock, T): the lineage as static data, at build time

**What the lock must provide, per fixed table `T`** (bill §11.7):

| the lock gives | COMPILE uses it for |
|---|---|
| a LINEAGE, one entry per locked layout, **OLDEST FIRST**, the current one LAST | the index a hash resolves to, and the order the floor cuts at |
| per entry: the WIRE hash — `HASH(layout bytes, definitions digest)` | `R.lineage[i]`, the only thing a file is matched on |
| per entry: the LAYOUT BYTES, verbatim | `R.known[i].layout`, what LOAD compares and what PLAN walks |
| per entry: the DEFINITIONS DIGEST | the hash's second input; it never rides the wire |
| per entry: the record BODY size | `R.known[i].record_bytes` as **`8 + body`**: the lock stores the BODY, COMPILE adds the eight hash bytes ONCE, and a BACKEND ADDS NOTHING. Never read off the file |
| per entry: a RETIRED mark and its reason (`schema lock --retire T@<hash>`) | the floor, and `layout_unsupported` |
| the defaults, the deprecation marks, the closure | the prefill image, §5.1, §5.5 |

**`record_bytes` IS THE WHOLE RECORD AND THE LOCK'S NUMBER IS THE BODY** — two numbers, one addition, and the
addition happens in exactly one place. §5.2's `record_bytes` is `8 + body`, the record's own eight hash bytes
and then the body, because that is the number §5.3 step 9 divides the tail behind the layout by. The lock
stores the BODY (`lockfile.LineageEntry.Record`), **COMPILE adds the eight ONCE, for every backend it hands
entries to** (`compiler/lineage.go`), and **NO BACKEND ADDS ANYTHING** — what a leg is handed is already the
whole number its static data needs, and the reader compares the file's `record_bytes` arithmetic to that whole
number. A leg that adds the eight a second time ships every older entry eight bytes long, and a driver that
passes the lock's number through ships them eight bytes SHORT. `TestFixedLineageRecordSizeIsTheWholeRecord`
(`compiler/fixedlineageship_test.go`) asserts the handed number per target, read back out of the emitted
source.

**WHAT THE LOCK NOW PROVIDES** (#909, merged into `fixed-table-form`). `internal/lockfile/lineage.go` is the
home bill §11.7 named, and a backend reads exactly three calls: `lockfile.Open(paths)` locates and parses the
unit's lock — `ok` false for a unit that was never locked is not an error, because a unit that has never been
locked promises nothing; `lockfile.Lineage(lock, T)` is the slice COMPILE walks, **OLDEST FIRST, the current
layout last**, and nil for a name the lock carries as a nested `type` rather than a fixed table; and
`lockfile.Floor(lock, T)` is §5.2's one number — one past the highest `Retired` index, `0` when none is. A
`LineageEntry` carries the five facts the table above asks for and the two the operator writes: **`Wire`**
(the eight bytes the header and every record carry — the lineage's key and the only fact a file is matched
on), **`Layout`** (the bytes verbatim: a u32 entry count and a run of seventeen-byte entries, what LOAD
memcmps and what PLAN walks), **`Digest`** (§13, empty where a table carries none of those facts),
**`Record`** (the BODY size, the one fact a backend never takes raw: `record_bytes` is `8 + Record` and COMPILE
adds the eight), **`Retired`**, and **`Reason`** (the operator's sentence). Nothing changed about
the FILE: the digest still never rides the wire.

The text is one line per entry — `lineage wire=0x… record=<n> bytes=<hex> [digest=<hex>] [retired]
[reason=<sentence>]`, the wire hash first because it is what a file is matched on and the reason last because
it is the one token that holds a sentence — and **the line is ONE STATEMENT MADE TWICE**: the parse recomputes
fnv1a64 over `bytes` and then `digest` and refuses a line whose `wire` disagrees, the way `layout=` is held to
the field lines. **The SET is bound too**: `lineage=0x…` on the `fixed table` line is one hash over every
entry's wire hash and retired mark, in order (#912), so a hand that DELETES, reorders or re-marks a lineage
line is caught by name — and the remedy it names is never "write it again", because no command can recover the
layout bytes of a record nobody declares any more: restore the file from version control. A layout stops being
served by being RETIRED, `schema lock --retire T@0x<hash> --reason "…"`, which keeps the entry and moves the
floor. A lock written before the lineage existed is SALVAGED and never deleted (bill §11.8): its single layout
becomes the first entry, and every field line, default and deprecation mark is carried forward unchanged.

**§5.1 has its implementation too**: `internal/lockfile/monotone.go`, reached from the lock's own diff — one
comparison per recorded fact, in the order that names the change best, the first difference is the finding,
and the rule phrase is the clause a person greps for.

**The reference reads none of it yet.** `internal/codegen/cpptable/lineage.go` builds its lineage from SIBLING
SCHEMA FILES at generate time by filename convention — `VOLD_`/`VNEW_`, the numbered evolution sets, and
`ir.TableFixedFixtureLineage` for the pairs no convention can name (`Scalars2` lives in a different directory
from `Scalars`) — and its floor is hard-coded for one test package. **That is the interim, and it is named as
an interim**: the convention goes away the day COMPILE reads `lockfile.Lineage` and `lockfile.Floor`, and
until then §5.8 holds it as a divergence. A port implements THIS page against the lock, never the convention.

```
COMPILE(lock, T):
  y := T's own layout ; Hy := HASH(bytes(y), T)            -- the digest is computed AT the hash, from T
  R.own_hash := Hy
  R.identity, R.split, R.cover := the baked identity plan of §4.2, its split, the type's value bytes
  i := 0
  for each entry x in lock.lineage(T), OLDEST FIRST:            -- the current layout is the last of them
    R.lineage[i] := x.hash ; R.known[i] := { x.hash, x.layout_bytes, 8 + x.record_body }
                                                                -- the lock stores the BODY; the eight hash
                                                                -- bytes are added HERE, once, and never again
                                                                -- by a backend
    R.plans[i]   := (x.hash == Hy) ? IDENTITY : PLAN(x.layout_bytes, bytes(y))
    i := i + 1
  R.floor := 1 + the highest index marked RETIRED, or 0 when none is
  emit R as static data: the hashes, the layout bytes, the record sizes, the plans, the floor
```

**A TABLE PAST §3.4's CEILING IS NOT A FIXED-FORM ROOT, SO COMPILE CONSULTS NO LINEAGE FOR IT.** SPEC-TABLES
§3.4 says a fixed table whose record body is past 65536 bytes has NO FORM EMITTED and is NAMED: it keeps form
`1`, which it never lost. COMPILE therefore **does not run for it at all** — the generator consults no lineage
for such a table and **parses no entry of it, even when the LOCK carries one** — and the lock does carry one,
because the lock records every fixed table's layout and is one file five legs read. **That entry is neither an
error nor data**: not an error, because the lock is correct and #8's "a bug in the lock, and the BUILD FAILS"
is about an entry of THIS FORM; not data, because there is no form for it to be data of. The only thing the
generated module owes such a table is the line naming why the form is not there. **The hurt is the JavaScript
leg** (#931): its ceiling refusal did not name the table, so `WideBlob` was a fixed-form ROOT there and nowhere
else, and the lineage parse then held the lock's CORRECT entry to the form's rules — where the root's size is
past the 65536 a reader caps a record at, so the layout "did not parse" and #8 FAILED THE BUILD over a lock
that was right. `TestJSFixedNoFormMeansNoLineageToParse`
(`internal/codegen/jstable/fixedform_test.go`) is the pin: the table is not a root, the refusal is named, a
lineage entry handed in for it does not fail the build, and nothing of it is emitted.

**The ENTRY POINT a backend receives the lineage through, what "build time" means in a language with no
`constexpr`, and how a plan's storage is sized are §5.9 #1 to #4** — the first three things the pilot port had
to guess, and now rules. **A leg with NEITHER a package initializer NOR a constant initializer is §5.9 #20,
and how such a leg sizes a static plan with no allocator is #21.**

**THE STATIC DATA HAS MEMBER NAMES, and the gate is what forced them.** One entry of `R.known` is
`TableFixedKnownLayout` and it carries FOUR members in this order — **`hash`, `layout`, `layout_bytes`,
`record_bytes`** — the byte length riding BESIDE the pointer rather than inside it, the way §4.1 names the plan
entry's lanes. `tools/fixedtwin` holds the C and C++ runtimes to that text member for member, so a leg writing
the struct from this page alone and landing a different spelling breaks a gate rather than a test (§5.9 #19).
**The file's hash lands on the report as `layout_hash`**, last on the report and zero on every other path
(§5.9 #15).

**The floor is one number and the lineage is one array**, so "retired" is an index cut and the operator's two
answers stay distinct: below the floor is `layout_unsupported` (upgrade the client), outside the lineage is
`layout_newer` (ship the reader). A retired entry stays in the lineage forever.

**THE HASH.** The eight bytes a file's header and every record carry. **The digest is computed AT THE HASH
SITE, from the schema** — the reference's signature is `ir.TableFixedLayoutHash(layout, st)` and there is no
digest argument for a leg to omit. That is the point of the shape: a leg that forgot the second argument, or
passed an empty one, would hash the layout bytes alone and publish a number that collides with every sibling
the digest exists to separate. A LINEAGE ENTRY is the one caller with no live schema behind it — a historical
layout has no `*Struct` — so it stores the two runs and hashes the concatenation, which is the same walk
because FNV is sequential and a nil schema is an empty second run:

```
HASH(layout_bytes, T):
  h := 0xcbf29ce484222325
  for v in layout_bytes:   h ^= v ; h *= 0x100000001b3
  for v in DIGEST(T):      h ^= v ; h *= 0x100000001b3   -- computed here; never an argument a caller supplies
  return h
```

**THE DEFINITIONS DIGEST** (bill §13) is every fact of §5.1 that is not wire shape. Without it
`int32 | 0..100` and `| 0..200` hash identically and an old reader cannot refuse the widening. It is the
compiler's, it is recorded in the lock beside the layout bytes, and **it never rides the wire**: the hash
binds it. Walk `T`'s closure as §1.1 walks it — declared field order, depth first, each NAMED type emitted
once — and append, per field:

| the fact | the bytes |
|---|---|
| an integer, float or fixed-point RANGE | `'R'`, then min and max, each as an i64 LE |
| `bits(N)` | `'B'`, then `N` as u32 LE |
| `fixed(I,F)` / `ufixed(I,F)` | `'X'`, then `I` u32 LE, `F` u32 LE, then `1` signed or `0` unsigned |
| a `flags` type | `'F'`, its WIRE BIT COUNT as u32 LE, then per flag `'f'` and `fnv1a64(name)` as u64 LE |
| a reader-side limit | `'L'`, then the limit as u64 LE. **RESERVED:** no table spelling of a reader-side limit exists; `--fixed-record-limit` is outside the law (§6) and does not emit this row. The row stays empty until a table-declared limit exists. The pin is `TestTableFixedDefinitionsDigestLReservedUntilALimitExists`. |
| a nested `table` / `type` | recurse, once per type name |
| a union | recurse into each arm's payload, in declared order |

Nothing else. **An empty digest leaves the hash equal to a hash of the layout bytes alone**, so a table
carrying none of these facts does not move the day the digest lands.

**THAT TABLE IS THE CONTRACT.** The reference emits `'R'` for every range (integer, float, fixed-point) and
`'F'` once by name. `'L'` is reserved: no table spelling exists, so that row is empty. **Structs, flags and
unions share one `seen` map keyed by bare name.**

**The ORDER inside one field is fixed**: `'R'` FIRST when the field has a range, then the KIND TAG — `'B'`
for `bits(N)`, `'X'` for `fixed(I,F)`/`ufixed(I,F)` — then the REFERENCE: `'F'` for a flags type, a RECURSE
for a nested `table`/`type`, a recurse into each arm's payload in declared order for a union. A field spends
none, one, two or three of those, in that order and never another.

**A flags type's WIRE BIT COUNT falls back to `len(Variants)` when it is `0`**: a `flags` declared with no
explicit width spends one bit per flag, and the digest records the count either way.

**`EMIT` switches on a KIND CODE**, and §1's kind-code table is the whole numbering — the fixed-point rungs
`20..24` and `25..29` included. A port that invents its own numbering for the same set emits different layout
bytes and a different hash, and nothing else it does can recover.

```
PLAN(x, y):                                        -- x the WRITER's layout bytes FROM THE LOCK, y the reader's own
  parse x and y under §1.1; either failing is a bug in the lock, not a wire event: REFUSE layout_malformed
                                                   -- at GENERATE, naming the table and the entry; and the
                                                   -- emitted entry is a LANE carrying that name, never a
                                                   -- throw a first load discovers (§5.9 #36)
  record := x.root.size                            -- EVERY entry is bounded by this, and by nothing the file said
  pass 1: MATCH(x, 0, y, 0, guard = NONE) keeping only UNGUARDED entries, counting the census
  split := the entries so far
  pass 2: MATCH(x, 0, y, 0, guard = NONE) keeping only GUARDED entries, COUNTING NOTHING
                                                   -- the CENSUS LOOP too, not only the entries (§5.9 #43)
  coalesce copies inside each half, never across the split (§4.2's rule, plus `arg` and the guard WIDTH equal)
                                                   -- the partition is owed even by a leg whose loop tests the
                                                   -- guard per entry: it is what makes this illegal (§5.9 #42)
  fills := COVER(y) MINUS every byte the entries land               -- §4.3; for IDENTITY this is empty
  if an entry reached past `record`:  REFUSE layout_record_too_large
  if the entries or their tables do not fit the declared capacity: REFUSE plan_too_large
                                                   -- owed by every leg, including one whose plan object also
                                                   -- owns the image and the hole list (§5.9 #45)

MATCH(x, ti, y, mi, guard, arg):                   -- two TABLE entries, side by side
  for each child `mc` of y[mi], in declared order:
    find the child `tc` of x[ti] with tc.id == mc.id, summing the writer's child sizes to reach its offset
    if found: EMIT(tc, its offset, mc, its storage offset, guard, arg)
  for each child `tc` of x[ti] no child of y[mi] names:   COUNT unknown      -- once per peer, never per record,
                                                                             -- and ONCE, in pass 1 (§5.9 #43)
```

`id` is `fnv1a64(wire name)`, so `was =` matches by the OLD name and a reorder after a merge matches by NAME
and never by position. A writer field the reader cannot name gets no entry: **skipping is not an act**. A
reader field the writer does not carry gets no entry either, and the prefill answers it.

```
EMIT(te, their_at, me, my_at, guard, arg):
  if depth > 64:                                         REFUSE layout_malformed      -- the wire picks no recursion
  if me.kind == 35 and te.kind != 35:                    -- T into ?T (bill §12.8)
      emit `present`  at the reader's present byte, under `guard`/`arg`
      EMIT(te, their_at, the wrapper's payload, my_at, guard, arg) ; return
  if te.kind != me.kind:
      if LADDER(te.kind, me.kind): emit `widenf` when te is f32 else `widen`,
                                   sign := te.kind is a signed integer or a signed fixed-point ; return
      COUNT kind_mismatch ; return                       -- a kind that MOVED is reported, never reinterpreted
  by me.kind:
    35 optional: `copy` 1 byte (the present flag), then EMIT the payload
    13 table:    MATCH
    14 array:    head := 4 when the reader's array carries a count
                 their_n := (te.size - head) / their element size ; my_n := (me.size - head) / my element size
                 when counted: emit `count`, size := THE WRITER'S their_n (bill §12.5)
                 for i in 0 .. min(their_n, my_n): EMIT the element at their_at+head+i*their elem, at my stride
                 the reader's elements past that get NO entry, so the prefill lands THE ELEMENT'S DEFAULTS
    16 keyed:    per reader slot, find the writer's key variant with the same id; EMIT the element at its slot
    15 union:    their_tag := te.size - widest arm ; my_tag := me.size - widest arm
                 FIRST, UNGUARDED: `const 0` of my_tag bytes into the reader's tag -- None, and it stands when no arm matches
                 per reader arm, matched to the writer's arm by id:
                   `const` of the READER's ordinal (position from 1), guarded by THE WRITER'S TAG OFFSET,
                      at the writer's tag WIDTH, with the writer's ordinal as the guard value
                   EMIT the arm's payload under that same guard and ordinal
                 a PAYLOAD-FREE arm (kind 32) has a LAYOUT ENTRY and NO BYTES: the tag names it and there is
                      nothing to land, so it takes the `const` of its ordinal and no payload EMIT at all. It
                      is an entry rather than nothing because the arm list is what the layout is COMPARED by
                      — an arm appended AFTER a payload-free one must move the hash
    30 enum:     if te.size < me.size: emit `widen`, unsigned                      -- a grown ordinal width (bill §12.7)
                 else: emit `ordinal` with a remap table — entry j is the reader's position of the writer's
                       variant j, or 0 when the reader has no such name
    12 / 33 text: emit `text`, span := min(me.size, te.size) - 4 BYTES, dst := the LENGTH word,
                  aux := the BUFFER, flavour from the READER's field (1 utf8, 2 wide, 3 bytes)
    every other leaf: te.size == me.size -> `copy` ; te.size < me.size and me.size <= 8 -> `widen` ;
                      otherwise COUNT kind_mismatch
```

**THE AUX LANE.** Every plan entry names TWO destinations: `dst`, the field's own storage offset, and `aux`,
its COMPANION — and which of the two an op writes is contract, never an implementation choice. The compiler
hands both down beside the reader's layout entry (`Dst` and `Aux` on `TableFixedDstSpec`,
`ir/fixedform.go:385-402`), and the compile step derives the pair at its top as `my_at + d.dst` and
`my_at + d.aux` — the parent's storage base plus the row's own offsets (the reference: fixedruntime.go:973).

| the READER's kind | `dst` | `aux` | which the op writes |
|---|---|---|---|
| `35` optional | the payload's own storage; the wrapper row adds none | the PRESENT byte | `T` into `?T` lands `present`, a CONSTANT `1` — constant in its VALUE and still carrying the row's own `guard`/`arg` (§5.9 #13) — at `aux` (971-980); `?T` into `?T` lands the writer's flag with a one-byte `copy` at `aux` (1008) |
| a COUNTED array `[..N]T` | the element run | the live COUNT word | `count` writes its four bytes at `aux` (1024) |
| `bytes(N)` | the BUFFER | the LENGTH word | the same `count` op, at `aux` — `bytes(N)` is an ARRAY row (fix 10) |
| `12` / `33` text | the LENGTH word | the BUFFER | `text` puts the length at `dst` and copies the payload to `aux` (1137, 281-295) |
| `15` union | the union's own storage | the TAG — **`dst` PLUS the tag's offset inside that storage**, the `AuxExtendsDst` row | both `const`s write at `aux`: the unguarded `None` (1074) and each arm's ordinal (1090) |
| `13` table, `14` uncounted, `16` keyed | the field's storage | unused, `0` | nothing; these recurse |
| every LEAF | the field's storage | unused, `0` | `copy`, `widen`, `widenf`, `ordinal` write at `dst` |

**Text and `bytes(N)` are the two rows the other way round**, and that is fix 10: text is length-at-`dst`,
buffer-at-`aux`; `bytes(N)` is buffer-at-`dst`, length-at-`aux`. A port that lands `bytes(N)` under the text
convention hands a compiled plan a count destination that is the buffer's first four bytes and an element
destination that is the length field — and the IDENTITY plan reads neither column, so only the compiled path
ever sees it.

**How a port DERIVES `aux`**: from its OWN storage, the same way it derives `dst` — the `offsetof` of the
companion member, `_present` / `_count` / `_length` / the union's `type` — and the build-time assertion §7
names covers both lanes, not just `dst`. It is never derived from the wire: a companion is storage the wire
does not carry.

**THE TEXT OP, in full** (the reference: fixedruntime.go:281-295). `span` is BYTES and the clamp is UNITS, and
they are not the same number:

```
TEXT(p):
  unit := 2 when the flavour is WIDE, else 1                  -- wstring(N) rides as UTF-16 code units
  cap  := p.span / unit                                       -- the CAP is in UNITS, the SPAN is in BYTES
  v    := LE(4, src + p.src) read as SIGNED
  if v < 0:    v := 0   ; COUNT clamped
  if v > cap:  v := cap ; COUNT clamped
  PUT(4, dst + p.dst, v)                                      -- the length lands at DST
  COPY(dst + p.aux, src + p.src + 4, p.span)                  -- THE WHOLE SPAN, never v * unit
  if the flavour is not BYTES:                                -- terminate AT THE USED LENGTH
      ZERO(dst + p.aux + v * unit, unit)
```

Three facts, and a port gets them wrong one at a time. **The cap is `span / unit`**: a `wstring(64)` whose span
is 128 bytes clamps at 64, and a port that clamps at 128 lets a length past the buffer through. **The copy is
the WHOLE SPAN and not the used length**: the writer wrote `length` units onto a ZEROED template and stopped
(fix 1), so the bytes behind the length are the writer's zeros, and copying them is how read-then-save-back
comes out byte-identical (§7 item 1). A port that copies `v * unit` passes every value check and fails the
dump identity — and **on a POISONED destination it fails louder**, because the `0x5A` behind the terminator
survives into the buffer and the saved file carries it back out. **The terminator is a STORE of ONE UNIT at
the used length**, which the storage has room for because text storage is one unit longer than the bound. So
the bytes a text entry LANDS are `[aux, aux + span + unit)` for a text flavour and `[aux, aux + span)` for
`bytes` (the reference: fixedruntime.go:436-437) — which is what the prefill's cover subtracts. The reader's
own span past `min(me.size, te.size) - 4` gets NO entry at all: a reader whose bound GREW takes the writer's
units and the PREFILL answers the rest.

**A WIDEN'S SIGN IS THE LADDER'S, and the SAME-KIND widen has none.** There are two ways to reach `widen` and
they extend differently. Across the LADDER — `te.kind != me.kind` and `TableFixedWidens` holds — the source
SIGN-extends when the WRITER's kind is signed, `i8..i64` or a signed `fixed(I,F)`, and zero-extends otherwise
(the reference: fixedruntime.go:993, over 92-95). Within ONE kind — `te.kind == me.kind`, `te.size < me.size`,
`me.size <= 8` — the source ZERO-extends: the entry's sign is left at zero (1152-1159), and so is the grown
enum ordinal's (1115). That is right today, because the only same-kind rows admitting two widths are `6`/`7`
carrying a `bits(N)` at its declared storage width and kind `30`, all unsigned. But a port that infers "extend
by the kind's signedness" has written a DIFFERENT RULE that agrees by accident, and it diverges the first day
a signed kind admits two widths. Take it as stated: **ladder widen, sign by the WRITER's kind; same-kind widen
and enum widen, ZERO.** Both count `widened`, once per entry per record — **per ENTRY, so a folded element run
counts ONCE and an unfolded one once per element — and because a fold across a widen is forbidden, the number a
fixture asserts on a widened run is the EXACT `min(their_n, my_n)`** (§5.9 #33).

**THE GUARD'S WIDTH, and the remap table's shape.** Two small structures a port must match exactly.

**`argw` is the guard's width in BYTES, clamped to `1..8` at both ends, and `0` reads as `1`.** At compile a
union stamps `their_tag` onto every entry beneath it, taking `1` for anything outside `1..8` (the reference:
fixedruntime.go:1063); the push re-stamps the current union's width and reads a zero as one (856-857); and at
run time the tag load clamps again — `0` to `1`, anything past `8` to `8` (171). **Zero means one** because
one is the width a tag had when this lane did not exist, so a plan laid down before it still reads. The guard
is tested at its FULL width and never at its first byte: one byte fires arm `1` on a foreign tag of `0x0101`,
an ordinal no arm of this build names (§7, fix 12). And the guard's own reach, `guard + argw`, is bounded by
the writer's record like every other offset (858-862).

**The remap table carries its own LENGTH in slot `0`.** `TableFixedLayTable` writes `n` at `table[0]` and the
`n` values at `table[1 .. n]` (the reference: fixedruntime.go:880-885); the entry's `aux` is the table's BYTE
OFFSET from the plan's base; and the `ordinal` op reads
`v := (raw != 0 and raw <= table[0]) ? table[raw] : 0` (305-310) — so slot `0` is the length, and an ordinal
of `0` is `None` BY CONSTRUCTION and never an index. Entry `j` is the reader's position of the WRITER's
variant `j`, or `0` when the reader has no such name. **The reference CAPS the table at 255 entries**,
`n := min(te.children, 255)` (1119-1120): a writer's enum past 255 variants silently loses the 256th on, each
remapping to `None` as though the reader did not name it. That is a divergence and not this page's rule —
§5.8 row 14 — and this page says the table is **as long as the WRITER's variant count**, with a length word as
wide as it needs to be.

**The TWO-PASS SPLIT gates the POOL, not only the entries.** The walk runs twice, unguarded first, and each
pass drops the entries that are not its half (849). A kind that ALLOCATES from the plan's pool BEFORE it
pushes — the enum, whose remap table `TableFixedLayTable` lays — must test the half FIRST, or the discarded
pass still spends the pool and a plan that fits comes back `plan_too_large`. The reference tests it at the top
of the enum case, before it builds the map (1113). The pool is the caller's own plan storage: remap tables and
prefill ranges grow DOWNWARD from the top while entries grow up, and the two meeting is `plan_too_large`.

**Every entry is bounded by the WRITER'S OWN declared record size**, `x.root.size`: `src + size`, plus `4`
for a text entry's length word, and `guard + the guard's width` too. One entry past it refuses the plan
WHOLE — never partly compiled. **The plan also carries the WRITER'S bounds for the hostile pass** (bill
§12.5): that peer's count bound, variant count, arm count and range, so a value forged past what the WRITER
could have written clamps and counts, and a value the reader merely widened does not.

**The prefill's image is the reader's OWN FRESH VALUE** — a zeroed destination with the type's `Reset` over
it — so every default §5.1 let through reaches the bytes the plan does not write: an appended field, an
element past the writer's count, a keyed slot an appended key opened. A union inside that image is reset
**arm by arm, at each arm's own overlay storage**, and only then the tag to `None`: an arm is not the member
it looks like, because a pointer arm is a reference and not its pointee, a counted array or a text arm rides
beside a companion, and a whole-value assignment over an overlay resets one arm's worth of bytes and calls it
all of them. An arm whose defaults the image does not carry is a default that silently becomes zero for every
field the plan leaves alone. **THAT SENTENCE IS AN INSTRUCTION AND NOT A WARNING** (§5.9 #38): in one flat image
the arms OVERWRITE each other, in DECLARED ORDER, at their overlay storage, and the tag lands `None` last — four
lines, conforming, and the only thing the overlay cannot hold is a default under a byte two arms both claim.

**The test of that is a POISON and not a reset.** Fill the destination with `0x5A` through a byte pointer,
load, and read the tail: it must be the DECLARED default. A `Reset` before the load only proves the load did
not clobber what was there, and a zero-fill looks exactly like a zero default, so neither can tell a prefill
that ran from one that never did. `keyed_array_enum_append`'s element is a fixed table with a nonzero default
for this reason (§5.7). **Where there is no byte pointer the poison has two other conforming targets — the
PLAN'S RECORD IMAGE, and the unit's own value surface field by field — and a leg says which it laid**
(§5.9 #35).

Because §5.1 held at every commit, `PLAN` never fails on a monotone lineage; a lineage that is not monotone
is a bug in the lock, refused by name at build.

### 5.3 LOAD(R, file): select by hash, never parse a stranger

```
LOAD(R, file, out, capacity, report):                  -- R the static data COMPILE laid down for T
  1  if file is absent or len(file) < 20:               report.malformed := true ; return -1
  2  if file[0] != 3:                                   REFUSE by DIRECTION: 1 previous_form,
                                                            2 message_form_as_file, otherwise newer_form
  3  L := LE(4, file+16) ; if 20 + L > len(file):       REFUSE layout_malformed
     layout := file + 20
  4  h := LE(8, file+8)                                 -- THE HEADER'S HASH, TAKEN AS GIVEN.
                                                        -- Nothing is recomputed from the wire: the digest is
                                                        -- not on the wire, so the hash cannot be re-derived.
  5  i := the FIRST index with R.lineage[i] == h
     if none:                                           REFUSE layout_newer, reporting h AND NOTHING ELSE
  6  if i < R.floor:                                    REFUSE layout_unsupported, reporting h
  7  k := R.known[i]
     if L != k.layout_length or the L bytes at `layout` differ from k.layout:
                                                        REFUSE layout_malformed
  8  plan, split, fills := R.plans[i] ; record_bytes := k.record_bytes  -- k is TableFixedKnownLayout (§5.9 #19,
                                                                       -- #34 on its two admitted divergences)
     record_bytes IS THE WHOLE RECORD, 8 + body, handed down already added (§5.2)
     for the identity entry the fills are EMPTY and record_bytes is 8 + C(root)
  9  rest := len(file) - 20 - L
     if record_bytes <= 8 or rest mod record_bytes != 0: report.malformed := true ; return -1
                                                        -- 8 is the per-record hash; a body of nothing behind
                                                        -- it would make `n` the file's length (fixedform.go:563)
 10  n := rest / record_bytes ; if n > capacity:         REFUSE batch_too_large
 11  per record, at `at`:
       if LE(8, at) != h:                               REFUSE no_layout        -- BEFORE any byte is landed
       PREFILL(out[rec], fills)                         -- §4.3
       RUN(plan, split, at+8, out[rec])                 -- §4.4, one loop, either plan
       BOUNDS(out[rec])                                 -- §4.5, §4.6, against THE PLAN'S bounds
       at := at + record_bytes
     return n
```

**The order is load-bearing and it is not §1.1's order.** A layout arriving on the wire is no longer walked,
so the seven rules do not fire at run time: the hash is looked up, then the floor, then the bytes are
compared. **The seven §1.1 malformations under a KNOWN hash all come back as one name**, `layout_malformed`
— "a lie about a known version" (bill §12.4, §13).

**EVERY HASH A RUNTIME HOLDS WAS HANDED TO IT, AND A RUNTIME NEVER DERIVES ONE.** Step 4 takes the header's
hash as given, and the rule behind that is wider than step 4: **the compiler hands every reader its own wire
hash and every known hash as CONSTANTS** — `R.own_hash` and `R.lineage[i]`, laid down by COMPILE — and **a
runtime NEVER computes a hash from layout bytes it holds**, not for the IDENTITY LANE (step 8, where the
selected entry being the reader's own is an INDEX COMPARISON and never a recomputation), not for a gate's
record check (§5.7), not for anything else. The reason is the digest: it is not on the wire and it is not in
the layout bytes, so a hash a runtime computed itself is a hash of the layout bytes ALONE — it matches a
layout that agrees in BYTES and disagrees in DEFINITIONS, which is the one thing §5.2's second input exists
to separate. **The hurt is the Elixir leg** (#928): it hashed its own layout bytes to find itself in the
lineage, matched NOTHING, and took a compiled plan on every read of its own files — a read that was correct in
its values and wrong in every way that matters, because the lane that is supposed to be free was the lane
nothing could reach.

| condition | refusal | what it reports |
|---|---|---|
| shorter than 20 bytes, or no bytes at all | — | `malformed`, and `-1` |
| form byte `1` / `2` / anything else | `previous_form` / `message_form_as_file` / `newer_form` | the name |
| `20 + L` past the file | `layout_malformed` | the name |
| the hash is in no lineage entry | `layout_newer` | **the file's hash, and nothing else** |
| the hash is in the lineage, below the floor | `layout_unsupported` | the file's hash |
| a known hash, different layout length or bytes | `layout_malformed` | the name |
| a known hash whose LINEAGE ENTRY would not build | `layout_malformed` / `plan_too_large` | the name the entry's own lane carries, never a throw (§5.9 #36) |
| the plan does not fit the caller's capacity | `plan_too_large` | the name (owed by every leg — §5.9 #45) |
| an entry past the writer's record size | `layout_record_too_large` | the name |
| a record size of `8` or less — the per-record hash and no body behind it | — | `malformed`, and `-1` |
| a ragged tail: `rest mod record_bytes != 0` | — | `malformed`, and `-1` |
| more records than the caller's capacity | `batch_too_large` | the name |
| a record hash that is not the file's | `no_layout` | the name |
| ill-formed text in the USED units | the name fix 11 owes (§4.5) | nothing decoded past it; the reference sets `malformed` instead — §5.8 |

**REFUSE is total: no counter moves, nothing is decoded, and not one destination byte is written** — the
prefill included. `malformed` is the residue and not a bucket a named rule falls into.

**THE REPORT IS TWO ANSWERS, AND EVERY FIXTURE ASSERTS THEM JOINTLY** (`test/tables/versioning_lists.cpp:178`,
the three lines `refuses_newer` shares with every OLD-REFUSES-NEW column). `refused` plus `reason` is one
answer; `malformed` is the other; they are NEVER both set, and the counters are a third thing again.

| the outcome | `refused` | `reason` | `malformed` | returns | the counters |
|---|---|---|---|---|---|
| a REFUSAL BY NAME — every row above that HAS a name | `true` | that name; `layout_newer` ALSO sets `layout_hash` to THE FILE'S hash and nothing else | **`false`** | `-1` | all zero, and not one destination byte written |
| a MALFORMED read — under 20 bytes, `record_bytes <= 8`, a ragged tail | **`false`** | untouched | `true` | `-1` | all zero |
| a read that lands values | `false` | untouched | `false` | `n` | §5.4's |

**`layout_unsupported` fills `layout_hash` too** — both layout refusals report the file's hash, and what belongs
to `layout_newer` alone is "and nothing else" (§5.9 #7). **The field is called `layout_hash`** and it is the
last member of the report (§5.9 #15). **The COMPILE census lands ONCE, after step 11's loop, on a read that
returns** (§5.9 #6), which is how REFUSE stays total — and on a LAWFUL lineage it lands zero, always, which is
§5.9 #30.

**THE NAMES ARE THE CONTRACT AND THE INTEGERS ARE EACH LEG PAIR'S OWN** (§5.9 #14). A peer is refused by the
same NAME whichever leg reads it; the value behind that name is the leg's, because one leg spells a reason as a
string and has no integer to agree with. The C and C++ pair's values are **`18` `layout_newer` and `19`
`layout_unsupported`**, taken in the order §5.3 names them — the order is the contract too, so a second leg
pair numbering its own set numbers them in the same order.

**STEP 9 IS ABOUT A FILE, NEVER ABOUT THE LOCK.** A record size of `8` or less comes off the FILE's arithmetic
— `record_bytes` is the BUILD's, the lock's body with the eight hash bytes already added (§5.2), but `rest`,
the tail behind the layout, is the file's. **A LINEAGE ENTRY whose
recorded record size is wrong is a LOCK BUG and the build fails** (§5.9 #26); a leg that routes it into step 9
has read a lock bug as a wire event.

So a port that sets `malformed` BESIDE a reason fails the joint assertion even though it refused correctly,
and a port that refuses with no reason fails it too. That is exactly what §5.8 row 8 is: ill-formed text
zeroes the field and sets `malformed`, where the ruling is a refusal by name.

### 5.4 The counters, per op and per condition

The fixtures assert these exactly, so a port that moves a different counter on the same bytes is wrong even
when every value lands.

| where | counter | when |
|---|---|---|
| `MATCH`, at COMPILE | `unknown` | once per writer field no reader field names, **once per peer and never per record** |
| `EMIT`, at COMPILE | `kind_mismatch` | once per pair whose kinds moved off every ladder |
| `widen`, `widenf` | `widened` | once per entry per record — a grown integer, `f32` into `f64`, a grown `fixed(I,F)`, **a grown enum ordinal width**. **Per ENTRY**: a folded element run is ONE and an unfolded one is `min(their_n, my_n)`, and a widened run is never folded, so a fixture asserts that EXACT count (§5.9 #33) |
| `count`, `text` | `clamped` | once per entry per record, when the length or count was out of range |
| the bounds pass | `clamped` | once per field: a ranged scalar off its end, **a NaN in a bounded float, which lands `min` and counts ONE as `-inf` does (§4.6) while `-0.0` against a `min` of `+0.0` is in range and counts nothing**, a `bits(N)` past `2^N - 1`, a union tag past the arm count, an enum ordinal past the top variant, **and a FORGED ordinal remapped to `None`** — one past the WRITER's own variant count, which the `ordinal` op lands as `0` and this pass counts, **on the COMPILED plan exactly as on the identity one** (§4.6, the bill's rule; the reference counts on neither compiled path — §5.8 row 12) |
| `copy`, `const`, `present`, `ordinal` | none | the op lands its value and moves nothing; the remap to `None` is the BOUNDS PASS's to count, on BOTH plans, and a port that counts in the op as well counts twice |
| a clean NEW-READS-OLD of an appended field, variant, arm, flag or keyed slot | none | **every counter stays at zero**: an append the reader knows is not an event |

A `bool` or a present byte that is not `0` or `1` is normalised to the language's own true and **counts
nothing** (bill §12.12).

**A clamp that cannot fire is not emitted, and nothing moves.** An ordinal whose extent FILLS its storage
width — 255 variants in a byte, 65535 in two — has no read-side clamp: the comparison would be `> extent`
against a value of that very width, which no value satisfies. The elided check never clamped, so the counter
it would have moved never moved either; a port that emits the tautology instead is still conforming, and a
port whose warnings-as-errors build refuses a tautology is free to drop it. The same rule already applies to a
ranged scalar whose declared end sits on its width's limit.

### 5.5 The closure

`closure(T)` is `T`, every `table` or `type` it reaches by value, every enum, union, flags and constant any
of them names, recursively. Every table or type in it is declared `fixed`; a pointer, map or unbounded array
in it is a compile refusal; `T` is never in its own closure.

### 5.6 What is retired by this section

Reading a layout the lock has never seen. The run-time walk of a stranger's layout — the seven §1.1 checks
stay as the LOCK's validation of what it records and the oracle's validation of the corpus. The recompute of
the header's hash from the layout behind it, and with it §2's step 5: there is no third thing to check, the
byte comparison holds the header and the layout together, and the digest could not be re-derived from the
wire anyway. The count clamp across bounds, the range clamp across versions, the remap of an unknown variant
to `None`, the drop-and-count of an unknown field — all forward reads, now `layout_newer`. Every hostile
check on a KNOWN layout's records stays (§4.5, §4.6).

### 5.7 The tests

One test per row of §5.1, red first, in the compiler: the baseline REFUSES the narrowing and names it, and
ACCEPTS the widening. One fixture per row of §5.2 in the reference, ported to every leg: the LIST rows in
`test/tables/versioning_lists.cpp` (`field_append`, `field_deprecate`, `field_undeprecate`, `enum_append`,
`enum_width`, `union_append`, `union_arm_payload_widen`, `flags_append`, `keyed_array_enum_append`,
`nested_append`, `rename_without_was`) and the NUMBER rows in `versioning_numbers.cpp` (the widenings, the
grown bounds, `array_fixed_grow` with a NONZERO element default, `optional_add`, the floor and the hash
cases). Each row is ONE definition change, two schemas of one table name in two packages, a `lead` and a
`trail` bracketing the row so a mislaid size moves a neighbour.

Every row owes two columns. **NEW-READS-OLD**: every old value lands exactly, the reader's tail is its
declared default, the counters are §5.4's. **THE VALUES ARE THE MANIFEST'S** — `build/fixedform-corpus/manifest.txt`,
the line the dump wrote for that file — and never a number read out of `fixedform_dump.cpp` or restated in the
test (§5.9 #32, #37). **The tail's one exception is the `present` companion, which owes `1`** and not its fresh
value's `false` (§5.9 #40). **And a harness pairs two generations' fields by the WIRE ID, never by name**, so a
`was =` rename compares (§5.9 #41). **OLD-REFUSES-NEW**: `layout_newer` before any record, with the
FILE's hash in the report, no counter moved, nothing marked malformed, and the destination still every field
of a fresh value. A row whose edit moves no layout byte and no digest byte (`rename_without_was`,
`field_deprecate`, `field_undeprecate`) has no second column: the hashes are equal, both sides take the
identity plan, and it READS in both directions — **which is the test that the hash, and only the hash, is
the version.** The floor owes three: a file AT the floor reads, a file ONE BELOW refuses
`layout_unsupported`, and the floor raised by one makes yesterday's file refuse today.

**The properties gate carries the pairs too.** `test/tables/fixedform_properties.cpp` runs its save / load /
compare properties over every fixture — `P1` among them — and then ONE PAIR RUN per lineage pair: `FX1`/`FX2`,
`P1`/`P3`, `FN1`/`FN2`, `FM1`/`FM2`, `V1`/`V2`, **`UT1`/`UT2`** (the guard and flavour lanes, which §7 item 6
already names as a fixture) and `Scalars`/`Scalars2` — **SEVEN pairs**, and `ir.TableFixedFixtureLineage`
(`ir/fixedform.go:685-693`) is the list, the one place to read it. That is §5.7's two columns inside the
gate rather than only inside the versioning cases. FORWARD: the newer reader compiles the older file through
its lineage and returns one record. REVERSE: the older reader given the newer file refuses, and the refusal is
`layout_newer` BY NAME. **The old contract's both-ways compiled read is retired, not bent** — a reader that
compiled a stranger's layout in either direction was the thing this section replaced. The pairs resolve
through `ir.TableFixedFixtureLineage`, the map from a newer fixture's basename to its older files, which is
what lets a pair no filename convention can name (`Scalars2`, in a different directory from `Scalars`) join
the gate at all.

**The gate's own record check is the READER'S COMPILED HASH CONSTANT**, never a hash of the layout bytes it
just read. The digest is not on the wire, so `hash_of(layout)` is the wrong identity for any table carrying a
range, a `bits(N)`, a `fixed(I,F)` or a flags type — a harness that recomputes it is asserting the one number
§5.2 says cannot be re-derived from a file.

**`keyed_array_enum_append`'s element is a fixed table with a NONZERO default**, `fixed table Qty { n int32 = 7 }`,
so the slot the appended key opens reads `7` and not `0`. Its NEW-READS-OLD destination, and
`array_fixed_grow`'s, are POISONED with `0x5A` through a byte pointer before the load: with a zero default and
a zeroed destination the row passes whether the prefill ran or not, and this is the row whose whole subject is
that it ran. OLD-REFUSES-NEW reads the same way from the other side — the destination holds `7`, the fresh
value's own default, because REFUSE wrote nothing. **That column compares VALUES and not a struct's slack**
(§5.9 #17): a generated struct's padding is not a value a reset covers, so a leg comparing bytes zeroes both
values before it resets them — which is the same reason the generated load memsets its default image first.
**Where a leg's generated value derives no equality at all**, the comparison is the byte view the poison is
already laid through, and that is conforming. **Where there is no byte view either**, the poison and the
comparison are the PLAN'S RECORD IMAGE or the unit's own value surface field by field — the three conforming
forms, and a leg says which it laid (§5.9 #35).

**Three notes a porter needs before reading the fixtures as an oracle.**

**`union_arm_payload_widen` IS NOT A WIDEN**, and its name misleads. The row appends a field `y` INSIDE arm
`alpha`; nothing grows a width, so it is an APPEND and it counts `widened == 0` and `unknown == 0` like every
other one — `reads_clean( "union_arm_payload_widen", r, 0, 0 )`, `test/tables/versioning_lists.cpp:611`. A
port reading the name and looking for a `widen` entry in the plan is looking for one that is not there.

**NO FIXTURE EXERCISES A NONZERO `unknown`, AND THE ROW THAT WOULD IS WITHDRAWN.** Every `reads_clean` call in
both files passes `0` for it, so §5.4's "once per peer, never per record" — and §5.8 row 11's divergence, which
counts once per ELEMENT of an array of tables — is asserted by NO row today. This page used to call such a row
OWED: a row whose OLD schema carries a field the new one dropped, over an array of tables. **That row cannot be
written and the debt is WITHDRAWN** (§5.9 #30): §5.1 refuses a removal, refuses a rename without `was` and
refuses a kind off every ladder, so on a LAWFUL lineage a newer reader names every field of every writer it can
select, and `unknown` and `kind_mismatch` are structurally zero. The census is the report of an UNLAWFUL or
HOSTILE entry only, and exercising it wants a deliberately unlawful lineage entry handed in by a test, not a
schema pair. A leg still carries the counters and still lands them once per returning read; what it no longer
owes is a fixture that moves them. §5.8 row 11 stays a divergence in its own right — a miscount is wrong
whether or not a lawful pair can reach it.

**A STALE COMMENT at `test/tables/versioning_numbers.cpp:1001`** (the block's own preamble, 998-1002), over the seven broken-layout cases under a
known hash, says "Today every one of them comes back with its §1.1 name instead". That is the reader §5.3
retired: a §1.1 malformation under a KNOWN hash is ONE name, `layout_malformed`, and the block's own
assertions now check exactly that (`r.reason == layout_malformed`, line 1046). The cases are right; the
comment describes a reader that no longer exists. **Noted for Johnny**, with the C++ green phase.

**The C leg's port list, from the twin.** `tools/fixedtwin` holds the C and C++ fixed-form runtimes to one
canonical text and fails on ANY difference, so a row C++ has and C does not would break the gate. Those rows
are recorded as **OWED BY THE C LEG** and stripped, each naming the §5 section the C card implements from
(`bench/paired/TWIN.md`). They are not divergences from this page — they are this page's port list for C:

| OWED by C | what it is | from |
|---|---|---|
| `kTableFixedPresent`: the enumerator, the Apply case (`dst[p.dst] = 1`), and the compile of `T` into `?T` (`me.kind == 35 && te.kind != 35`) | the present op — a constant `1` into the reader's present byte, **GUARDED by the row's own guard and ordinal** where the optional sits under a union arm and unguarded at top level (§5.9 #13, and this row said UNGUARDED before that ruling), then the payload under the same guard and ordinal | §5.2 EMIT, bill §12.8 |
| `TableFixedWidens` rungs `20..24` and `25..29`, and `TableFixedSignedKind` over `20..24` | the signed and unsigned `fixed(I,F)` ladders, and a signed fixed-point's sign for the widening | §5.1 `fixed(I,F)` |
| the enum case `te.size < me.size` emitted as `kTableFixedWiden` | a grown ordinal or tag width is a `widen`, unsigned, and counts `widened` | §5.2 EMIT kind 30, §5.4 |
| `struct TableFixedKnownLayout` | the known-layout table itself: per entry the hash, the layout bytes, the record size | §5.2, §5.3 |
| `layout_newer`, `layout_unsupported` and the floor, in the per-type codec rather than the shared extract | the version-range gates: refuse a hash the lineage does not hold, and a hash below the floor | §5.3 |

Five rows, and the C leg takes each from THIS PAGE rather than from the C++ text beside it. The twin's own
note says so: do not port the C from the C++. **All five are CLOSED as of #917**, and what closing them
exposed is that **the twin's map has TWO SIDES** (§5.9 #22): the gate fails on ANY difference, so the day a leg
implements a §5 row the reference still owes, the gate goes red on the leg that obeyed the page. Those rows are
stripped FROM THE OTHER SIDE, named, each pointing at the §5.8 row the reference owes — three of them today,
all three §5.8 row 3's: the hash select, the refusal that carries the file's hash, and one lineage entry's
plan.

**PORTING A LEG, IN ORDER.** The pilot port (#914, the Go leg, written from this section with the reference
unopened) ran it this way and nothing in it is optional.

| # | the step |
|---|---|
| 1 | **The fixtures FIRST, and RED.** `make tables-fixedform-corpus` writes the rows; the versioning corpus is `old_<row>.bin` / `new_<row>.bin`, one pair per row of §5.7, the SAME bytes the reference wrote. **The rows that are not a pair have their own names and only the dump states them** (§5.9 #28): the floor is `old_floor.bin` / `mid_floor.bin` / `new_floor.bin` — below it, AT it, the reader's own — and the branch case is `old_`, `a_`, `b_` and `new_lineage_merge.bin`, the two pre-merge writers beside the oldest and the merged build's. Bring every row over with BOTH columns and watch them fail before a line of the reader exists. A row that was green before the reader was written is a row asserting nothing. **WHAT EACH FILE HOLDS IS IN `build/fixedform-corpus/manifest.txt`**, written by the dump from the same values it writes into the bytes, one line per file: `file=<name> row=<row> side=old\|new\|mid\|a\|b\|none root=<Table> records=<n> values=<field>=<value>[,...]` — the root when a schema declares two tables, the record count, and every value the dump set, records as `r<i>.`, nested fields dotted, arrays indexed, text quoted, a float by its digits AND its bits. `values=` is the last field and **runs to the end of the line**, and a quoted value may carry spaces, so split the head on spaces and take the rest whole; `row=` is the per-file name while `root=` is shared by a lineage pair. **ASSERT THE MANIFEST, NEVER READ THE DUMP** (§5.9 #32, #37): the declared default is not the value on the wire — `int_widen`'s `lead` and `trail` are `2863311530` and `3149642683` there while the schema says 1 and 2 — and a field absent from a line carries its schema default |
| 2 | **LOAD, §5.3's eleven steps IN ORDER.** The framing checks, the header's hash TAKEN AS GIVEN, the lineage select, the floor, the byte comparison, the per-record hash BEFORE the prefill. Every refusal by its own name, nothing decoded, no counter moved. This is the half the negative controls watch. **EVERY HASH THE RUNTIME HOLDS IS A HANDED CONSTANT** — the leg computes none from layout bytes, on the identity lane or anywhere else (§5.9 #47) |
| 3 | **The static data**: the lineage from the lock, oldest first, the current layout last (§5.9 #1, #2); the floor as one number; every plan laid down OFF THE LOAD PATH (§5.9 #3, #4). Nothing here reads a file at run time. **`record_bytes` ARRIVES WHOLE**, `8 + body`, and the leg adds nothing to it (§5.9 #46). **A TABLE PAST §3.4's CEILING GETS NO STATIC DATA AT ALL** — no lineage consulted, no entry parsed, only the line naming the missing form (§5.9 #48) |
| 4 | **PLAN / MATCH / EMIT**, §5.2, against §1's kind-code table — the ladder rungs `20..24` and `25..29` included, the aux lane's seven rows, the text op's three facts, the widen's two signs, the guard's width, the remap table's length word, the two-pass split before the pool spends |
| 5 | **The three tests §5.6 RETIRES, by name.** The leg's twins of `TestFixedFormPlanPath`, `TestFixedFormArgLaneTextUnderSecondArm` and `TestFixedFormLayoutRules` asserted the run-time walk of a stranger's layout and a forward read; under this section each file comes back `layout_newer`. **SKIP them by name with the sentence that says where the coverage is owed** — the plan path moves to the lineage harness, §1.1's seven rules move to the LOCK's validation of what it records — and never delete them: a deleted test is a coverage claim nobody can audit. **A leg whose suite has no skip prints one instead** (§5.9 #23): a line at the CALL SITE naming the function, §5.6 and where the coverage is owed, plus a count at the end, and the function stays in the tree. **A NEGATIVE CONTROL PINNED TO A RETIRED CASE MOVES WITH IT**, to wherever the coverage went, and the PR names which of its controls moved (§5.9 #39) |
| 6 | **The gate**, both halves, hung off that leg's `test-<lang>`: the BYTE gate against the reference's corpus and the VERSIONING gate over §5.7's rows, each with a negative control that moves one byte and proves the gate goes red (§5.9 #11). **The versioning gate's PROBE UNITS live beside the leg's generator** (§5.9 #18), one generated probe per row per COLUMN, built and run with the leg's own toolchain: two generations of one table name have no spelling inside one unit in every language, so the column is the unit — except where a language's own namespacing gives the two generations a spelling, and then one build and one run is conforming and the leg says so. **The versioning gate's control is a sabotage that reds exactly one column**: the wrong refusal name on a file outside the lineage (§5.9 #39) |
| 7 | **§5.8 ROW 4's probe, `writer_bound_count`** — the forged count `7` in `hostile_array_bounded_grow.bin` (the writer's `[..4]`, the reader's `[..8]`) lands `vals_count == 4` and `clamped == 1` EXACTLY, the plan's bound being the WRITER's; its three sibling lanes are the range, the variant count and the arm count, and the number is `1` on each (`FIXED-FORM-VERSIONING-TESTS.md`, the divergence rows) |
| 8 | **§5.8 ROW 9's probe, `refuse_writes_nothing`** — `nolayout_nested_append.bin`, whose per-record hash is forged under an untouched header hash, read with the caller's storage POISONED `0x5A` in #35's form the leg laid: `no_layout`, `malformed` FALSE, every counter `0`, and every poisoned byte still `0x5A` — the prefill's nonzero `Vec.w = 88` nowhere, because the hash check runs BEFORE the prefill |
| 9 | **§5.8 ROW 11's probe, `unknown_census`** — the deliberately UNLAWFUL pair `VOLD_/VNEW_unknown_census`, its entry HANDED IN through #1's `GenerateLineage` rather than read from a lock, over an array of FOUR tables with one dropped field: `unknown == 1`, once per FIELD per peer, and `4` is the divergence. The only unlawful entry any leg builds, and the one row #30 leaves reachable |
| 10 | **§5.8 ROW 12's probe, `forged_ordinal_both_plans`** — `hostile_enum_append.bin` read TWICE, by the NEW build's COMPILED plan and by the OLD build's IDENTITY plan: `None` and `clamped == 1` on both, the same number on both, counted in the BOUNDS pass and not in the `ordinal` op as well (§5.4's "counts twice") |

**What to SKIP, by name**: nothing that §5.3 gives a name to. §5.8's fourteen rows are the REFERENCE's debts and
not a port's work list — a leg that reproduces a divergence to match the reference has ported the bug, and the
one row that must be waited on rather than matched is row 10, the digest (§5.9 #12).

**What to leave RED, named rather than faked**: the hostile pass under the WRITER's bounds until the plan
carries them (§5.8 row 4); the remap table past 255 entries (row 14); the clamp count on a forged ordinal under
a compiled plan (row 12). **A nonzero `unknown` is no longer on this list** — it is not owed at all, because no
lawful lineage can move it (§5.9 #30). A leg reports these as owed, in its own words, in the PR that lands it.
**A red nobody wrote down is a red nobody owes** — including a red the leg did not write: a pre-existing
failure a port has to route around is named and CARDED, never quietly carried (§5.9 #31). **Rows 4 and 12 now have
PROBES of their own** (steps 7 and 10), so a leg that finds either GREEN says so rather than leaving a red
nobody owes: the count lane's bound is the WRITER's on the branch today (`e.size = their_n`,
`fixedruntime.go:1045`, and `TableFixedKnownRange`'s `lo`/`hi` are the writer's too) while §5.8 row 4 still
reads `e.size = my_n`, so row 4 owes a RE-READ and the probe is what settles it — **at `clamped == 1` and
never `>= 1`**, which is the half no reference case asserts today.

**What to mark UNPORTED rather than RED**: a row whose KIND the leg's language surface cannot hold at all — kind
`33` in a JavaScript table, refused before §5 is reached — is ⚪ in both columns with the refusal that blocks it
cited, skipped by name, and never folded into a green (§5.9 #44). A leg's row table carries three marks: green,
red-and-named, and unported-with-the-reason.

### 5.8 What the green reference owes this section

Every row is a divergence that STILL STANDS on `fixed-table-form` with the C++ green phase merged (#910) and
the lock's lineage merged (#909), re-read against the branch. **Implement this page, not the reference** — for
each row the bill's ruling is the second column, and **the reference owes this**.

| # | what the reference does | what this page says — the reference owes this |
|---|---|---|
| 1 | the lineage is read from SIBLING SCHEMA FILES at generate time, by filename convention plus a fixture map (`cpptable/lineage.go`, `ir.TableFixedFixtureLineage`) | the lineage comes from the lock — `lockfile.Lineage`, with a retired mark and a reason per entry. The convention is the INTERIM, named as one (§5.2) |
| 2 | the floor is hard-coded to one index for one test package (`lineageFloor`) | the floor is `1 +` the highest retired index, from `lockfile.Floor` |
| 3 | a plan for an older hash is compiled AT FIRST LOAD from the trusted bytes and kept in a caller-supplied cache keyed by hash | `COMPILE` lays every plan down at BUILD TIME (bill §12.5); nothing compiles at run time, and there is no cache to miss |
| 4 | the `count` op's bound, the ranges, the variant and arm counts are the READER's (`e.size = my_n`) | the bounds are the WRITER's, per plan (bill §12.5) |
| 5 | `arg`, the guard's ordinal, is still a BYTE lane (`uint8_t`), so a union past 255 arms cannot express its guard value — the tag is READ at its own width and the remap table is `uint16_t`, so only this lane is short | no byte lane anywhere (bill §12.7): the ordinal is full width on both sides |
| 6 | a nested union's arm entries carry the INNER tag's guard only, and its `None` entry carries the outer guard at the inner tag's WIDTH | an arm inside an arm answers to the OUTER tag too (§4.1) |
| 7 | the `ordinal` op reads through a 32-BIT temporary (`uint32_t raw`) | a 64-bit temporary: an ordinal width of `8` is admissible (§4.5) |
| 8 | ill-formed text zeroes the field, restores its default and counts `malformed` | a content violation REFUSES BY NAME (§4.5, fix 11) |
| 9 | the prefill runs BEFORE the per-record hash check, so `no_layout` has already written the caller's storage | the hash check is first; REFUSE writes nothing (§5.3) |
| 10 | the digest carries only INTEGER ranges (`HasIntRange`) — a float range and a reader-side limit are still not in it, and a FLAGS type is re-emitted once per naming field because `seen` covers structs only | every range and every limit, tag `'L'` (bill §13), or the widening cannot be refused — **and a flags type deduped by NAME, once, as a struct is** (§5.2). **An INTEROP BREAK, not a missed refusal: it moves the hash, so the reference owes it BEFORE any leg ports** |
| 11 | the `unknown` census is counted once per ELEMENT of an array of tables | once per field per peer |
| 12 | a forged ordinal remaps to `None` and counts NOTHING on a compiled plan, while the identity plan counts `clamped` | `COUNT clamped` on both (§4.6) |
| 13 | an entry reaching past the writer's declared record comes back through the compile's failure path as `layout_malformed`; the name `layout_record_too_large` is wired only to the 65536 bound and to a zero root size | `layout_record_too_large` for the entry too (fix 3) |
| 14 | the enum remap table is CAPPED AT 255 entries, `n := min( te.children, 255 )`, and its length word is a `uint16_t` | the table is as long as the WRITER's variant count (§5.2): today a writer's 256th variant and beyond remap to `None` as though the reader did not name them — a SILENT wrong value, not a refusal |
| 15 | there is no GENERATOR-SIDE COMPILE: every leg that cannot run a plan compiler at build carries the walk as emitted runtime text in its own language, so the plan is built from the lock's bytes once per process instead of being laid down as source | the plan is laid down AS DATA by the TOOLCHAIN, one shared COMPILE in the generator's language feeding every leg's emitter (§5.9 #20). **This row is the TOOLCHAIN's debt and not the C++ runtime's**, and it is the follow-on the C leg's shape names: until it exists, a leg with neither a package initializer nor a constant initializer owes the TIMING and the thread safety of its one build flag in its PR, and that is conforming |

**What #910 and #909 settled, and this list no longer carries.** `BASELINE` exists: the monotone law is
`internal/lockfile/monotone.go`, the lock holds a lineage with a retired mark and a reason per entry, and
`schema lock --retire T@0x<hash> --reason "…"` is the one non-append edit the file takes — so the row that
said "no lineage in the lock, no monotone check, no `--retire`" is closed, and what remains of it is rows 1
and 2 above, which are about the BACKEND not reading what the lock now holds. The digest is no longer a
second argument a leg can omit — it is computed inside the hash from the schema (§5.2) — and the reference's
properties gate now reads its cross-schema pairs through the fixture lineage map with the reverse direction
named `layout_newer` rather than compiled both ways (§5.7).

### 5.9 The twelve the pilot port had to guess, answered — and thirty-six the legs after it found

The PILOT PORT (#914) wrote the Go leg from this section alone, with `internal/codegen/cpptable` and the C++
runtime unopened, and went green on every fixture row. It also listed twelve places where this page was SILENT
and a porter had to choose. Each is answered here so the next eight legs choose nothing. Where the pilot's
choice is right it is the rule; where the bill overrules it, the bill's answer is the rule and the pilot's is
named as the wrong turn. Each row says what the MERGED reference does today, because a leg reading the
reference beside this page needs to know which of the two it is looking at.

**#13 to #31 come from the SECOND and THIRD legs** — the C leg (#917) and the Rust leg (#918), each written from
§5 alone with `internal/codegen/cpptable` and the C++ runtime unopened, the merged Go pilot their only worked
example. Two legs asking the same question twice is the page's own bug report, so every one of these is checked
against the bill, against the merged C++ and against all three ports, and the rule written is the one the three
now share. **#32 to #45 come from the corpus manifest and the FOURTH through SEVENTH legs** — Java, C#,
JavaScript and Dart — and the block that opens them says so. **#46 to #48 are the three the WIRING found** —
the driver handing the lock's lineage to nine legs at once (#920, #928, #931) — and each of them is a rule about
what a leg is HANDED rather than about what it walks.

**1. THE ENTRY POINT BY WHICH A BACKEND RECEIVES THE LINEAGE.** A backend takes the lineage AS DATA, through a
SECOND entry point beside the plain one: `Generate(u)` is the NO-LINEAGE case — a unit that was never locked
promises nothing, so it emits the identity plan and a lineage of one — and `GenerateLineage(u, lineage)` is the
one COMPILE runs, where `lineage` is per fixed table the lock's entries OLDEST FIRST, each carrying §5.2's six
facts (`lockfile.LineageEntry`: `Wire`, `Layout`, `Digest`, `Record`, `Retired`, `Reason`), plus the floor.
**The backend opens no file**: `lockfile.Open`, `lockfile.Lineage` and `lockfile.Floor` are the CALLER's three
calls, so the disk is read in one place and a test can play the lock in one line. The pilot's shape is the
rule. The reference has no such entry point — `cpptable.Generate(u *ir.Unit)` (`cpptable.go:1465`) builds its
lineage by opening sibling schema files itself (`cpptable/lineage.go`, `loadLineagePeers`), which is §5.8 row
1's interim; the reference owes the second entry point too.

**2. THE CURRENT LAYOUT IS ALWAYS THE LAST ENTRY.** `R.lineage` is never empty. COMPILE appends the table's own
layout when the lock's last entry is not already it, so `R.own_hash` always resolves to an index and the
IDENTITY plan is always reachable — including on a build with no lock at all, whose lineage is that one entry.
The pilot's choice is the rule; the reference does the same.

**3. WHAT "BUILD TIME" MEANS FOR A LEG WITH NO `constexpr`.** **Plans built at package initialization from THE
LOCK'S BYTES are build time for this page, and they are conforming.** That is not a run-time walk of a
stranger's layout: the bytes came from the lock, the walk happens once per process, off every load path, and a
plan that will not build is a BUILD failure and never a refusal at the first file that needs it. What bill
§12.5 and §5.8 row 3 forbid is the other thing — compiling a plan FROM A FILE'S BYTES when a load asks for it,
and keeping it in a cache a load can miss. Emitting every older plan as source is equally conforming and is the
better shape where the language has a constant initializer; which of the two a leg picks is SHAPE in §7's
sense, and a leg says in its PR which it took. **Contract: nothing on the load path compiles, nothing on the
load path parses a layout, and the load path cannot fail for want of a plan.** The reference compiles at first
load into a caller-supplied cache, which is the row this page is holding it to.

**4. HOW A PLAN'S STORAGE IS SIZED.** The build sizes it BY CONSTRUCTION — the plan is laid down, so its entry
count, its pool and its remap tables are all known before anything runs. How the build discovers that size is
shape: the pilot grew `256 → 1024 → … → 2^18` and retried, and a leg that computes the size up front is no
different. **Contract: the DECLARED CAPACITY a build holds an entry to is real, and an entry whose plan
exceeds it records `plan_too_large` ON THAT ENTRY** — BASELINE refuses the widening at commit under §5.1's cap
row (bill §12.10), and LOAD reports `plan_too_large` by name if a file ever selects that entry. The reference
sizes no plan at build: the caller passes `plan_capacity` and the plan is compiled into the caller's buffer
(`fixedruntime.go:1174-1187`), §5.8 row 3 again.

**5. WHAT THE CALLER'S PLAN SLICE MEANS ONCE PLANS ARE STATIC.** It stays a **CAPACITY DECLARATION**, and it is
no longer written through. The selected plan's entry count is checked against it and refuses `plan_too_large`
by name — so §5.3's row stands exactly as written and §7's negative control, a one-entry plan slice, stays red.
The pilot's choice is the rule. A leg whose API carries no such argument still owes the refusal, from the
plan's entry count against its OWN declared cap: the name is the contract, the argument is not.

**6. WHERE THE COMPILE CENSUS LANDS IN LOAD.** `unknown` and `kind_mismatch` are the PLAN'S OWN NUMBERS, fixed
when the plan was built; the load carries them onto the report **ONCE, AFTER step 11's record loop, and only on
a read that RETURNS `n`**. Not inside the loop — §5.4 says once per peer, never per record. Not before it — a
refusal that came after would have moved a counter, and **REFUSE IS TOTAL**. The pilot's choice is the rule.
The reference counts them into the live report at compile (`fixedruntime.go:948`, `998`, and `1194`'s "counting
it twice would lie"), and because it compiles at first load they land on whichever load happened to compile the
plan and on no other — a second read of the same peer reports `unknown == 0`. Under this page every returning
read of that peer reports the same census.

**7. WHETHER `layout_unsupported` SETS THE HASH FIELD.** **YES.** Both named layout refusals report the file's
hash: §5.3's condition table says `layout_unsupported` reports it, and the bill's ruling that "the retired
hashes are emitted for `layout_unsupported`" (§12, ruling 9) is what the field is FOR — an operator who must
decide between "upgrade the client" and "ship the reader" needs the number in both answers. What belongs to
`layout_newer` alone is the words **AND NOTHING ELSE** (bill §12.4): its report carries the hash and no other
fact. The joint-answer table's phrasing is about what else a row may carry, not about which rows fill the hash
field. The pilot's choice is the rule. (The ruling is bill §12.4 with §12's ruling 9; §12.12 is the bool and
present-byte normalisation, a different row.)

**8. A LINEAGE ENTRY THAT IS NOT A PARSEABLE LAYOUT.** The primary answer is the one §5.2 already gives and a
leg must not soften it: **it is a bug in the lock and the BUILD FAILS**, naming the table and the entry's hash.
Where a leg cannot fail a build because the lineage arrived at run time — a test or a host handing entries in —
the entry carries `layout_malformed` and LOAD refuses by that name if a file ever matches its hash, with
nothing decoded and no counter moved. The pilot's choice is the rule for that second path only; a leg that
takes it at BUILD time has turned a lock bug into a wire event.

**9. WHICH FIXED TABLE IS A FILE'S ROOT WHEN A SCHEMA DECLARES TWO.** **Every declared `fixed table` is a root**
(`ir.TableFixedRoots`) with its own layout, its own hash, its own lineage and its own files — a nested fixed
table is not a lesser table, it is one THIS file is not. **The one exception is a table with no form at all**:
past §3.4's 65536-byte ceiling there is no fixed-form root and no lineage to consult (#48,
`ir.TableFixedFormRoots`). Nothing in a file names its root: the reader chooses
the root and **the HASH is what resolves**, so a file written from `Vec` and read as `Lineage` is
`layout_newer`, which is the right answer and not a near miss. For the corpus rows the writer names the root
explicitly — `test/tables/fixedform_dump.cpp` writes `nested_append` from `Lineage`, `optional_add` from
`OptionalAdd`, `p1`/`p3` from `Chain` — and in every row it is the OUTER table, the fixed table no other fixed
table of the unit names by value. The pilot's heuristic gives that same answer on every row; **the rule is the
dump's**, and a leg reads each corpus file with the root the dump wrote it from.

**10. THE COUNTERS A ROW OWES, PER ROW.** An APPEND row owes **every counter at zero** — an append the reader
knows is not an event (§5.4's last line). A WIDTH row owes **`widened` nonzero and every other counter zero**.
The exact number is §5.4's rule multiplied out: `widened` moves once per widen entry per record, so a row's
count is its plan's widen entries times its record count, and a leg that knows its plan asserts the number
while one that does not asserts nonzero. **No clean row asserts `clamped` at all.** `union_arm_payload_widen`
is an APPEND and owes `0, 0` despite its name (§5.7). The pilot asserted the floor of this and it stands; the
multiple is the addition.

**11. THE LEG'S BYTE GATE EXISTS, AND A LEG OWES TWO GATES.** `make tables-go-fixed-form` **is in the tree** —
`make/go.mk:475`, hung off `test-go`, with a negative control at `make/go.mk:489` that adds one to every text
length through `go build -overlay` and demands the gate go red. It is the WRITE-side byte gate: the Go leg's
layout and the whole 80915-byte file against the C++ reference's corpus, byte for byte. The VERSIONING half is
a second target, `make tables-go-fixedform` (`make/go.mk:58`), `go test ./internal/codegen/gotable -run
TestFixedForm`. **The pilot guessed wrong here, and the way it guessed wrong is the lesson**: it grepped
`Makefile` alone, and this repository's per-language targets live in `make/<lang>.mk`. So: **every leg owes
both gates** — bytes against the reference's corpus, and §5.7's rows against the versioning corpus — each hung
off that leg's `test-<lang>`, and each with a negative control, because a byte comparison nobody has seen fail
may be comparing a file with itself.

**12. THE DIGEST IS THE SHARED FUNCTION'S, AND A LINEAGE WAITS ON ROW 10.** A leg computes every hash through
the compiler's own `ir.TableFixedLayoutHash(layout, st)` and never through a private re-derivation — §5.2's
signature has no digest argument for a leg to omit, and a leg that reaches for the digest separately has
already diverged. The pilot did this and its hashes move WITH the reference the day §5.8 row 10 lands. **The
consequence a porter needs stated: a lineage entry pinned TODAY for a table carrying a float range or a
reader-side limit is a hash no build after row 10 can reproduce.** So do not lock a lineage for such a table
until row 10 is in — which is the same sentence as §5.2's "the reference owes it BEFORE ANY LEG PORTS", read
from the operator's side.

**13. THE `present` CONSTANT'S GUARD.** **GUARDED by the arm's guard where the optional sits under a union arm,
UNGUARDED at top level** — which is one rule and not two: the present entry carries the `guard`/`arg` it was
COMPILED UNDER, and at top level that pair is empty. It is the only reading that never sets a present byte for
an arm the tag did not name, and a present byte standing for an unselected arm is a value the reader would then
believe. §5.2's EMIT already said "under `guard`/`arg`" and §5.7's C-leg row said UNGUARDED; the row was wrong
and is fixed. What "constant" means in both sentences is the VALUE — a `1` that comes from nowhere on the wire.
The reference agrees in code (`e.guard = guard; e.arg = arg` at `fixedruntime.go:980`, under a comment that
says unguarded), and so do all three ports.

**14. THE TWO NEW REFUSALS' NUMBERS.** **The NAMES are the contract and the integers are each leg pair's own**,
which is the rule the ten names before these two already state. "Pick the numbers the pilot uses" has no source:
the Go leg spells a reason as a STRING, so there is no integer there to agree with. The C and C++ pair's values
are **18 `layout_newer` and 19 `layout_unsupported`**, in the order §5.3 names them — **the ORDER is the
contract**, so a leg pair numbering its own set lands the same two in the same sequence and a reader diffing two
legs' headers sees one shape.

**15. WHERE THE FILE'S HASH LANDS ON THE REPORT.** **`layout_hash`**, LAST on the report, zero on every other
path — a new member at the end rather than a field inserted among the counters, because the report is a struct
callers already hold. §5.3 requires both layout refusals to report the file's hash and never said where it went,
so the pilot and the C leg each invented the name and happened to agree (`LayoutHash` where the language
capitalises). The agreement is now the rule and not a coincidence.

**16. A LEG WHOSE API CARRIES A PLAN CACHE.** **§5.6 retires MECHANISMS, not API.** A signature carrying a cache
argument keeps it: the argument is accepted and not read, with the reason in a comment at the parameter, and the
plan slice beside it stays what #5 says it is — a CAPACITY DECLARATION, checked and never written through. A leg
that deletes the argument has broken every caller to express a change this section did not ask for; a leg that
starts using it again has un-retired §5.8 row 3.

**17. WHAT THE OLD-REFUSES-NEW COLUMN COMPARES.** **VALUES, never a struct's slack.** "The destination is still
every field of a fresh value" is about the fields: a generated struct's padding is not a value a reset covers,
so two fresh values can differ in their slack and a byte comparison of a refused-into destination against a
fresh one would compare that difference. A leg comparing bytes **zeroes both values before it resets them**,
which is the same reason the generated load memsets its default image first. And where a generated value derives
no equality and no printing at all, **the byte view is the conforming comparison** — it is the same view §5.7
already lays the `0x5A` poison through.

**18. WHERE A LEG'S PROBE UNITS LIVE.** **Beside the leg's own generator, one generated probe per row per
COLUMN**, compiled and run with that leg's toolchain from the versioning test itself. The column is the unit
because two generations of ONE table name have no spelling inside one translation unit in any language: the C
leg generates a probe per row per column and builds it with `$(CC)`, the Rust leg generates a workspace of probe
crates and runs one `cargo test`. What the page owes is the SHAPE, not the build system: a probe is generated,
not hand-written, so a row and its negative control cost the same.

**19. THE KNOWN-LAYOUT ENTRY'S MEMBERS.** **`hash`, `layout`, `layout_bytes`, `record_bytes`, in that order**,
with the byte length riding BESIDE the pointer. The struct is `TableFixedKnownLayout` where a leg pair shares a
text, and `tools/fixedtwin` holds that pair to it member for member — so this is not a naming preference, it is
a gate, and a leg writing the struct from §5.2's prose alone lands a different spelling and goes red. §5.2's
static data now names them the way §4.1 names the plan entry's lanes.

**20. A LEG WITH NEITHER A PACKAGE INITIALIZER NOR A CONSTANT INITIALIZER.** #3 offers two conforming shapes —
plans built at package init, or every plan emitted AS SOURCE — and **a language can have neither**. C has no
package init, and emitting a plan as source needs a COMPILE in the GENERATOR's language, which this repository
has only as emitted runtime text. The rule for such a leg: **build each older plan ONCE PER PROCESS from THE
LOCK'S OWN BYTES into static storage, at the first load that selects a non-identity entry, off the record loop**
— and say so in the PR, with the thread safety of that one flag. #3's contract is what holds: nothing on the
load path compiles a FILE's layout, there is no cache a load can miss, and a load cannot fail for want of a
plan. **What such a leg OWES is the TIMING** — "off every load path" is the contract's word and a first-load
build is beside it, not on it. **The remedy is a SHARED GENERATOR-SIDE COMPILE and no leg can write it alone**:
it is §5.8 row 15, the toolchain's debt, and until it lands this shape is conforming and named.

**21. SIZING A STATIC PLAN WITH NO ALLOCATOR.** #4 says the build sizes it BY CONSTRUCTION and calls the
discovery shape — and a leg with no allocator cannot discover by growing, so the cap is a **FORMULA the leg
states**. The rule: **state the formula in the PR, derive it from the reader's leaf and layout-entry counts, and
refuse `plan_too_large` BY NAME on the entry that exceeds it** (#4's contract, #5's name). The two legs' numbers
are examples and not the rule — the Rust leg's `body + 2·entries + 256` for the plan and `1024 + 32·entries` for
the remap pool, the C leg's from the same two counts. **A formula too small is a SILENT `plan_too_large` on a row
that should read**, so it is the kind of constant that wants a reader, which is why it goes in the PR and not
only in the source.

**22. A LEG AHEAD OF THE REFERENCE NEEDS ITS OWN STRIP LIST.** A twin gate fails on ANY difference, so the day a
leg implements a §5 row the REFERENCE still owes, the gate goes red **on the leg that obeyed the page**. The
rule: **the map has two sides**, and a row the leg has and the reference does not is stripped from the other
side, NAMED, pointing at the §5.8 row the reference owes — never by walking the leg back to the reference's
shape. Three such rows stand today, all §5.8 row 3's.

**23. "SKIP BY NAME" IN A SUITE WITH NO SKIP.** Step 5's rule is that a retired test is skipped with the
sentence that says where the coverage went and **never deleted**. Where the suite is a `main` that counts
failures and has no skip verb, the skip is **a printed line at the CALL SITE** naming the function, §5.6 and
where the coverage is owed, plus a printed count at the end, and the function stays in the tree and stays
compiling. The rule being protected is the same one: a deleted test is a coverage claim nobody can audit.

**24. A LEG'S OTHER CALLER BUFFER.** #5 rules on the plan slice; a signature may carry a second scratch lane
beside it — a remap buffer, say — which has no owner once the plans are the build's. **It is held to #5's rule,
not to a rule of its own**: it stays in the signature as a capacity declaration and stops being written through,
and the leg says so in its PR. Same reasoning as #16: the API is not what §5.6 retires.

**25. THE PER-RECORD REFUSAL'S NAME IN A LEG WHOSE VOCABULARY IS "BLOCK".** §5.3's name is **`no_layout`** and
the name is the contract (#14). A leg carrying form 1's words — `no_block`, `block_malformed` — **keeps its
spelling until the whole-leg rename and names the divergence in its PR**: the rename moves a dozen tests and is
its own change, and a leg that renames only the one name a fixed read raises has left two vocabularies in one
runtime. What the leg may NOT do is let the difference go unsaid, because a peer refused under a name no other
leg knows is a peer an operator cannot triage.

**26. A LINEAGE ENTRY WHOSE RECORD SIZE IS WRONG.** #8 rules on an entry that is not a parseable LAYOUT. A
recorded RECORD SIZE that is wrong — `8` or less, or not what the entry's own layout accounts for — is the same
class and takes the same answer: **a bug in the lock, and the BUILD FAILS**, naming the table and the entry's
hash. It is NOT §5.3 step 9: step 9 is arithmetic over a FILE's tail, and routing a bad lock entry into it
reports a lock bug as a wire event, with `malformed` and no name. Where the lineage arrived at run time the
entry carries `layout_malformed` and LOAD refuses by that name, exactly as #8 says.

**27. A COUNTER THE OP ALREADY MOVES.** §5.4 puts the forged ordinal's `clamped` in the BOUNDS PASS, on both
plans, and says a port counting in the op as well counts twice. A leg that already counts it IN THE OP, with a
green fixture asserting the number, has **the OP to move and not the fixture**: the counter's PLACE is the
contract, because it is what makes the compiled and identity plans agree, and a fixture pinned to the wrong
place pins the bug. The leg re-pins the fixture after the move, in the same change, and says in its PR that the
number did not change — or why it did.

**28. THE CORPUS'S OWN FILE NAMES.** `old_<row>.bin` / `new_<row>.bin` is the pair, and the rows that are not a
pair have names only the dump states: **`old_floor.bin` / `mid_floor.bin` / `new_floor.bin`** for §5.7's three
floor rows — below the floor, AT it, the reader's own — and **`old_`, `a_`, `b_`, `new_lineage_merge.bin`** for
the branch case, the two pre-merge writers beside the oldest file and the merged build's own.
`test/tables/fixedform_dump.cpp` is the list; §5.7 step 1 now carries it so a leg does not have to read the dump
to know what to open.

**29. A REPORT WITH FEWER COUNTERS THAN §5.4 LISTS.** §5.4 names the counters a fixed read can MOVE; a leg's
report carries whatever its form-1 surface already carried, and the two sets need not be equal. `duplicate` is
the TEXT form's — **the fixed wire never raises it** — so a leg whose report has no such member is not missing
one, and **a fixture asserts the counters that EXIST on that leg's report**, all of them, by name. A leg that
invents a member to match a list has added a field that is zero forever.

**30. THE CENSUS CANNOT BE NONZERO ON A LAWFUL LINEAGE.** `unknown` and `kind_mismatch` are **structurally zero
on every lawful read**, and §5.7's owed row — "a row whose OLD schema carries a field the new one dropped, over
an array of tables" — **is WITHDRAWN**. §5.1 refuses a field removal, refuses a rename without `was`, keeps a
deprecated field's slot, and refuses a kind that moved off every ladder: so a newer reader NAMES every field of
every writer its lineage can select, and there is nothing left for the census to count. A nonzero census needs
either a lock that is not monotone — a BUILD bug — or a peer outside the lineage, which is `layout_newer` before
a plan exists. **The counters therefore remain the report of an UNLAWFUL or HOSTILE entry only**: machinery that
is right, carried, landed once per returning read (#6), and correctly zero. A leg still implements it; what no
leg owes is a schema pair that moves it. Exercising it wants a DELIBERATELY UNLAWFUL lineage entry handed in by
a test, which is the only shape that can reach it and is nobody's port work. §5.8 row 11 stands on its own: a
miscount is wrong whether or not a lawful pair can reach it.

**31. A RED THE LEG DID NOT WRITE.** A port will find failures that are not §5's — a stale byte pin another
change moved and missed, a link rule that does not build in this bench's tree, a feature-flag combination whose
const asserts were already red. The rule is §5.7's, extended: **name it, say it is not green, say it is not
yours, and card it** — and where a probe has to route around it, say which flag the route uses. Re-pinning a
byte gate is itself the kind of edit that wants a reader, so it is called out in the PR rather than folded into
a green. **A red nobody wrote down is a red nobody owes**, and a red written down as somebody else's is still
written down.

**32. A PORT ASSERTS THE MANIFEST'S VALUES, NEVER READS THE DUMP.** Every leg ported from this section — Go
(#914), C (#917), Rust (#918) and Dart — opened `test/tables/fixedform_dump.cpp`, the one file §5.7 forbids, for
the same four facts: what values a row's records actually carry, which table is the ROOT where a schema declares
two, how many RECORDS a file has, and the names of the rows that are not a pair. A rule four legs in a row had
to break is not the legs' bug. So the emitter SAYS what it wrote: `make tables-fixedform-corpus` writes
`build/fixedform-corpus/manifest.txt` beside the bytes, one plain-text line per file, no dependency to parse it
—

```
file=<name> row=<row> side=old|new|mid|a|b|none root=<Table> records=<n> values=<field>=<value>[,<field>=<value>...]
```

— where `row` and `side` come from the FILE NAME, `root` from the record's own TYPE and `records` from the
vector the dump saved, and every value is stored through one helper that performs the assignment AND records the
line from the same expression (`MS` / `MSI` / `MSE` / `MSEI` / `MSTR` / `MSTRI` / `MWCPY` — the `I` pair take the
SUBSCRIPT the call site is looping over, so the path holds the index the bytes hold and never the variable's
name), so the manifest cannot drift from the corpus: a changed value changes both or neither. `values=` is the
LAST field and runs to the end of the line: split the head on spaces, then take everything after `values=` whole
— a quoted value may carry spaces, and the commas inside one are escaped (`\,`). `root=` is NOT unique across
rows (FU1's and FU2's is `FuRoot`, a lineage pair's two sides share one name by construction); `row=` is the
per-file name. A record's values are prefixed `r<i>.`, nested fields are
dotted, array and keyed slots carry the index the bytes carry, text is quoted (`u"…"` for wide, `\uXXXX` for
anything not printable ASCII), a float is given as digits AND bits (`nan|0x7F8ABCDE` — a signalling NaN's
payload is the value), and a field the dump did not set carries its schema default. **The manifest is the
card's source and the dump is off limits**; `manifest_case` in `test/tables/fixedform_main.cpp` is what keeps it
honest — every `.bin` in the corpus has a line, and every `root=` names a table the generator emitted — and
nothing under `build/` is committed.

**#32 LANDED WITH THE CORPUS MANIFEST** (#924), and it is the ruling the four legs below lean on. **#33 to #45
come from the FOURTH through SEVENTH legs** — Java (#920), C# (#921), JavaScript (#922) and Dart (#923), each
written from §5 alone with `internal/codegen/cpptable` and the C++ runtime unopened. Four legs asking a question
the first three did not is the page's own bug report twice over, so every one of these is checked against the
bill, against the merged C++, and against all seven ports, and the rule written is the one they now share.

**33. THE FLAT-ELEMENT FOLD AND A WIDENING ARE INCOMPATIBLE.** §4.2's coalescing rule folds an array of a FLAT
element type — one whose storage image IS its wire image — into ONE run however many elements it holds, and §6's
leaf cap counts it as one leaf. **That fold holds only where the writer's element width and the reader's are
EQUAL.** Across a widening the premise is gone — the writer's `int16` run is not the reader's `int32` storage —
and §5.2's EMIT for kind 14 has no fold in it at all: it emits the element `min(their_n, my_n)` times. A leg that
folds first and widens never emitted NOTHING for the run, landed the reader's declared defaults over values the
writer sent, silently, and left `widened` at zero (the C# leg, found by the fixture and named at `157268a6`).
**The rule: a leg folds a run only when the two element widths are equal; across a widen EACH ELEMENT IS ITS OWN
WIDEN ENTRY.** A leg that keeps a folded run across the widen owes an entry that widens element by element into
the span its setter takes — the C# leg's `FlatWiden`, sign `0` zero-extending, `1` sign-extending by the WRITER's
kind, `2` an f32 into an f64, which is §5.2's two widen signs over a run — and a folded run whose kinds moved off
every ladder COUNTS `kind_mismatch` rather than vanishing.

**The number every leg asserts is EXACTLY `min(their_n, my_n)`, and on the corpus row that is `4`.** `widened`
moves once per ENTRY per record (§5.4), and a fold across a widen is what the rule above FORBIDS — so
`array_elem_widen` has no folded shape on any conforming leg: `[..4]int16` into `[..4]int32`, four elements sent,
is four widen entries and counts `4` per record, on every leg. **That is the assertion a twin gate and every
leg's fixture share** (the C# leg's fixture asserts `4`). The C++ reference's `r.widened > 0`
(`test/tables/versioning_numbers.cpp:345`, "a widened element counts (§5.2)") is the REFERENCE FIXTURE'S LOOSER
assertion, written before the fold's incompatibility was named, and a leg that copies it cannot tell a run that
widened element by element from the folded read this ruling exists to catch.

**34. THE STATIC-DATA STRUCT'S NAME IS THE CONTRACT; ITS MEMBER COUNT CAN BE THE LANGUAGE'S.** The name on this
page is **`TableFixedKnownLayout`** with #19's members in #19's order. Two divergences are now admitted, both
named rather than silent. **A leg-local SPELLING is allowed where no twin gate binds the leg**: the Go leg's
`TableFixedKnown` and the Rust leg's `TableFixedKnown` are conforming — `tools/fixedtwin` holds the C and C++
pair to one text member for member, and that gate is what makes the name load-bearing THERE — but the page's name
is the contract, and a leg spelling it otherwise says so in its PR. **A MANAGED leg carries THREE members**: where
the language's byte array carries its own length, `layout_bytes` is a second spelling of `layout.length` and the
leg drops it, keeping the same ORDER (the C# leg, `fixedruntime.go:141`); Java, JavaScript and Dart kept the
member explicit and are equally conforming. **What no leg may move is the hash's place or the record size's
source** — the record size is the LOCK's, never the file's (§5.3 step 8) — and a leg whose language has no
sixty-four-bit integer carries the hash as two lanes there and says which form the report uses (§5.9 #15).

**35. THE `0x5A` POISON ON A DESTINATION WITH NO BYTE VIEW.** §5.7 poisons "the destination through a byte
pointer" and #17 rules on the COMPARISON in a language with no byte view. The POISON has the same three
conforming forms, and this is the third. **(i)** The byte view where the language has one. **(ii)** The PLAN'S
RECORD IMAGE where the prefill's destination and the caller's value are two different objects — JavaScript's
`plan.image`, Dart's record image — which is the only instrument that can tell a prefill that ran from one that
never did, and the image is then the poison's named target. **(iii)** THE UNIT'S OWN VALUE SURFACE, field by
field, where there is neither — a C# sealed class, a Java object reached by reflection: every field set to the
poison value its type can hold, and every field compared after. All three are the same test and all three catch
the row they exist for (`array_fixed_grow`'s and `keyed_array_enum_append`'s element defaults). **A leg says
which of the three it laid**, and a leg that can reach the image proves the poison BITES by switching the prefill
off and watching the slots come back `0x5A5A5A5A` (the Dart leg does).

**36. A LINEAGE ENTRY THAT WILL NOT BUILD IS A LANE, NEVER A THROW.** #8's primary answer stands and is
sharpened: **an entry whose layout does not parse fails the BUILD at GENERATE, naming the table and the entry's
hash** — it is a lock bug, and the generator is where a lock bug is caught. What #8 did not say is what the
emitted code may do with an entry whose plan the build could not lay down, and four legs found four ways to turn
it into a crash: Java's `ExceptionInInitializerError`, C#'s `TypeInitializationException`, Dart's lazy `final`
re-evaluated at every touch, a JavaScript module-init throw. **Every one of those is a REFUSAL THAT ESCAPED ITS
LANE**, and in a static-initializer language it takes the whole type down on the first load of ANY file, lawful
ones included. **The rule: a per-leg static-init failure is unreachable by construction. Every lineage entry
becomes a lane CARRYING ITS OWN REFUSAL REASON** — `layout_malformed` for a layout that would not parse,
`plan_too_large` for a plan that would not fit — which LOAD reports by name if and only if a file selects that
entry (#4's contract, #5's name). Nothing on the load path throws, and an entry nobody selects costs nothing
but the lane.

**37. THE NEW-READS-OLD VALUE ORACLE IS THE CORPUS MANIFEST.** §5.7 says every old value lands exactly and #9
says which root to read each file with; neither gives a leg a number. Four legs invented an oracle and four legs
opened `fixedform_dump.cpp`, the one file §5.7 forbids, because the values are not derivable from the schema — a
row's `lead` and `trail` are `0xAAAAAAAA` / `0xBBBBBBBB` on the wire while the schema declares `1` and `2`.
**The oracle is `manifest.txt` (#32) and nothing else.** In particular it is **NOT the leg's own older build**:
that shape — read `old_<row>.bin` twice, once with the old build's identity plan and once with the new build's
lineage, and compare — is a CROSS-CHECK and a good one (it covers every field of every row for one extra probe
run, where a hand-written check covers two), but as an oracle it is the leg agreeing with itself, and a leg whose
identity read and whose compiled read are wrong the same way goes green. **A leg may run the older build as a
second column; the values it ASSERTS are the manifest's.** Three consequences the manifest also settles, each a
thing a leg otherwise guessed: a row's record COUNT, a row's ROOT table where a schema declares two (#9's rule,
now written down per file), and the names of the rows that are not a pair (#28).

**38. "ARM BY ARM" IS AN INSTRUCTION, NOT A WARNING.** §5.2's prefill sentence — the image's union reset arm by
arm, at each arm's own overlay storage, and only then the tag to `None` — was read by two legs as a HAZARD
NOTICE, and both went looking for a per-arm mechanism the page does not have: one left
`union_arm_payload_widen`'s reader-appended field at `0` where its declaration said `55`, the other spent the row
on per-arm images before finding the four lines. **Read it as the instruction it is: in ONE FLAT image the arms
OVERWRITE EACH OTHER, in DECLARED ORDER, at their overlay storage, and the tag lands `None` last.** That is four
lines and it is conforming — an arm's default survives wherever no later arm covers it, which is every byte that
matters for a field the plan leaves alone. **What the overlay cannot do is carry two arms' defaults at one byte**:
a default under a byte two arms both claim is a hazard no flat image can avoid, and it is the one case that wants
either per-arm images selected by the landed tag or a GUARDED prefill entry in the plan. A leg may take either;
the flat walk in declared order is the rule, and the shared byte is the exception a leg names.

**39. A NEGATIVE CONTROL MOVES WITH THE CASE IT WATCHES.** §5.7 step 5 rules on the retired test and says nothing
about the control that watched it. **A control pinned to a case §5.6 retires moves with that case, and the PR
NAMES IT.** The C# and Java legs each found a `plan_too_large` control that could no longer fire — with plans
static and a stranger's file refused before any plan is selected, a one-entry plan slice is unreachable — so the
control travels to the lineage harness with the coverage; controls that survive survive by reaching their case
some other way (a case reading through `parse`/`compile`/`run` rather than through `load`), and that is luck, not
conformance, so it is stated too. **And every VERSIONING GATE OWES ITS OWN CONTROL** (#11's second gate): one
sabotage that reds exactly one column and nothing else. The shape two legs landed: **swap the refusal NAME a file
OUTSIDE the lineage owes for the one a file BELOW THE FLOOR owes**, through the leg's own overlay mechanism, and
every OLD-REFUSES-NEW row must go red by that assertion and no other. A `select` returning the first entry
instead of the matching one is the same control from the other side.

**40. THE PRESENT COMPANION IS THE ONE NEWER-ONLY FIELD THAT IS NOT A DEFAULT.** §5.7's "the reader's tail is its
declared default" is true of every appended thing but one. On `optional_add` the reader gains a `present`
companion beside the payload, and **it owes `1`, not the fresh value's `false`** (bill §12.8): the writer sent a
`T`, so the `?T` the reader declares IS present, and the constant is the plan's (`present`, §5.2's EMIT, under
the row's own guard and ordinal, #13). The payload beside it lands the writer's value, not a default. A leg that
prefills the companion and emits no `present` entry reads a value the writer sent as absent — values intact,
presence lost — and every value check passes.

**41. A `was =` RENAME IS PAIRED BY WIRE ID IN ANY HARNESS, NEVER BY NAME.** §5.2 matches a field by
`fnv1a64(wire name)`, so `b was = a` matches the OLD name while the VALUE SURFACE is spelled with the NEW one.
§5.7 calls `rename_without_was` a both-directions row and never says how a harness pairs the two generations'
fields — and a name-keyed comparison reds a correct read on every renamed field. **The rule: a harness pairs by
the WIRE ID, translating the newer build's names through the rename map, which is §5.1's own sentence read from
the test's side.** The manifest (#32) names the field as the wire names it, so a leg asserting the manifest gets
this for free; a leg running its older build as a second column owes the translation.

**42. THE SPLIT IS A PROPERTY OF THE PLAN'S ORDER, OWED BY EVERY LEG.** §5.2's PLAN partitions the entries —
every unguarded one first, then every guarded one, and the plan states where the second half starts — and §4.4's
`READ` takes `split` as an argument. A leg whose run loop tests `guard != NONE` per entry is correct either way
and asked whether it still owes the partition. **It does.** The partition is not only what makes §4.2's inline
two-half loop possible (one of three requirements bought with a measurement, fix 6): **it is what makes
COALESCING ACROSS THE SPLIT ILLEGAL.** Two `copy` entries that merge while one is guarded and the other is not
produce a run that lands a guarded arm's bytes unconditionally, and the rule "coalesce inside each half, never
across the split" has no meaning in a plan that is not partitioned. It is also what gates the POOL: a kind that
allocates before it pushes must test its half first, or the discarded pass spends the pool and a plan that fits
comes back `plan_too_large`. **A leg carries `split`, partitions the entries, and may still test the guard per
entry in its loop** — what it may not do is skip the partition.

**43. THE CENSUS COUNTS ONCE, IN MATCH, AND NEVER AGAIN IN THE SECOND PASS.** §5.2 says pass 2 counts NOTHING
and §5.4 says `unknown` is once per peer — but MATCH's own census loop runs over the SAME pairs in both passes,
so a leg reading §5.2 literally (one report, both passes) DOUBLES every `unknown` and every `kind_mismatch`.
**"Pass 2 counts nothing" governs the census loop exactly as it governs the entries.** The conforming shapes: hand
pass 2 a DEAD report (the JavaScript leg), or run the census in pass 1 only. This is invisible on a lawful lineage
— the census is structurally zero there (#30) — and it is the whole answer on the unlawful entry that is the only
thing able to move it, which is why it has to be right in a leg no fixture can red.

**44. A ROW A LEG CANNOT GENERATE AT ALL IS UNPORTED, NOT RED.** #31 covers a red the leg did not write and §5.7
covers a red left named; neither covers a row whose KIND THE LEG'S LANGUAGE SURFACE CANNOT HOLD. `wstring_grow`
is not a §5 failure on the JavaScript leg: kind `33` is refused in a JavaScript table (`refuseWideText`, #366), so
there is no probe to generate and no reader to write. **Such a row is marked UNPORTED and skipped BY NAME, with
the refusal that blocks it cited** — ⚪ in both columns and not 🔴 — because a red is a debt this section owes and
an unported row is a debt another section owes. A leg's row table therefore carries three marks, not two: green,
red-and-named, and unported-with-the-reason. **What a leg may never do is fold it into a green**, and what a
reviewer may never read a ⚪ as is "this row passed".

**45. `plan_too_large` IS OWED BY EVERY LEG, INCLUDING ONE WHOSE PLAN OBJECT OWNS MORE THAN THE ENTRIES.** #5 says
the caller's plan slice stops being written through and becomes a capacity declaration; #24 rules on a second
scratch lane. Two legs found a plan OBJECT that carries the entries AND the record image, the hole list, the remap
pool and a conversion lane — and "stops being written through" cannot mean all of it, because the read genuinely
writes the image. **The rule: the ENTRIES are the build's and are never written through; whatever an `ordinal`
entry's `aux` INDEXES is part of the plan and moves into static data with it** (§5.2 says `aux` is the table's
byte offset from the plan's base, which assumes the entries and the pool share one allocation — where they do
not, a lane's entries are meaningless without the pool that travels with them); **the IMAGE and the conversion
lanes stay the caller's and are written through every record. And the refusal is owed either way**: the selected
entry's count against the declared capacity, reported `plan_too_large` BY NAME, on the entry. A leg whose API
carries no capacity argument owes it from its own declared cap (#5); a leg whose compiler has ONE failure code for
"my own layout did not parse" and "the pool overflowed" owes two, because the first is `layout_malformed` at
GENERATE (#36, #8) and only the second is `plan_too_large` — the Dart leg reports the wrong name for a
reader-side bug today and says so.

**46. `record_bytes` IS THE WHOLE RECORD, AND THE ADDITION HAPPENS IN EXACTLY ONE PLACE.** §5.2 defines
`record_bytes` as `8 + body` and the LOCK stores the BODY, so somebody adds the eight — and three legs each
picked a different somebody. **The rule: the COMPILER adds it, once, and hands the backend the WHOLE number**
(`compiler/lineage.go`, and `TestFixedLineageRecordSizeIsTheWholeRecord` in
`compiler/fixedlineageship_test.go` asserts it per target, read out of the emitted source); **a BACKEND ADDS
NOTHING**; and the reader compares the file's arithmetic — `rest mod record_bytes`, `rest / record_bytes` — to
that whole number. Both wrong turns are silent where it hurts: a leg that adds the eight again ships every
older entry eight bytes LONG, a driver that passes the lock's number through ships them eight bytes SHORT, and
in both cases every record an older peer wrote is misread or the read is refused as ragged. It is invisible in
every harness fixture because none of them carries a lock, and invisible for the CURRENT layout because the
leg's own entry — spelled `8 + body` by every leg — wins for that one. **A leg that wants to check its own
arithmetic checks an OLDER entry of a REAL LOCK**, never its own.

**47. A RUNTIME IS HANDED EVERY HASH IT HOLDS, AND DERIVES NONE.** §5.3 step 4 takes the header's hash as
given and §5.6 retires the recompute, both for one reason: the DEFINITIONS DIGEST is not on the wire, so a hash
cannot be re-derived from a file. **The consequence is a rule about the READER's own side too, and it is wider
than step 4**: the compiler hands every reader its own wire hash and every known hash as CONSTANTS (`R.own_hash`,
`R.lineage[i]`), and a runtime **NEVER** computes a hash from layout bytes it holds — not for the IDENTITY LANE
(§5.3 step 8, where "is this entry my own" is an INDEX comparison), not for a gate's record check (§5.7), not
for anything. A hash a runtime computed itself is a hash of the layout BYTES ALONE, and it matches a layout that
agrees in bytes and disagrees in DEFINITIONS — the single case the digest exists to separate. **The hurt is the
Elixir leg** (#928): it hashed its own layout bytes to locate itself in the lineage, matched NOTHING, and ran a
COMPILED PLAN on every read of its own files. Every value landed, so no fixture could see it; the free lane was
simply unreachable. A leg whose hash appears anywhere outside its generated constants has this bug.

**48. PAST §3.4's CEILING THERE IS NO FORM, SO THERE IS NO LINEAGE TO CONSULT.** A fixed table whose record
body is past 65536 bytes has NO FORM EMITTED and is NAMED (SPEC-TABLES §3.4): it keeps form `1`, which it never
lost. **Such a table is NOT a fixed-form root, so COMPILE consults no lineage for it and parses no entry of it,
even when the LOCK carries one** — and the lock does carry one, because it records every fixed table's layout
and is one file five legs read. **That entry is neither an error nor data**: not an error, because #8's "a bug
in the lock, and the BUILD FAILS" rules on an entry of THIS FORM and this is not one; not data, because there
is no form for it to be data of. What the module owes the table is the line saying the form is not there, and
nothing else — no known layout, no plan, no record size. **The hurt is the JavaScript leg** (#931): its ceiling
refusal did not name the table, so `WideBlob` was a root there and nowhere else, the lineage parse held the
lock's CORRECT entry to the form's rules, the root's size was past the 65536 a reader caps a record at, and #8
failed the BUILD over a lock that was right. The pin is `TestJSFixedNoFormMeansNoLineageToParse`. **This is the
one place #9's "every declared `fixed table` is a root" is read too far**: every declared fixed table is a root
of its own LINEAGE and its own files, and a table with no form is a root of neither.

## 6. The bounds

| bound | verdict |
|---|---|
| **4096 bytes of record body** | a WARNING, always on, naming the table and the size. Nothing about the wire changes there; it is where a fixed table stops being a small thing |
| **65536 bytes of record body** | a COMPILE REFUSAL for a DECLARED fixed table, by name, naming the table and the size. A wire fact: a reader holds an untrusted peer's layout to the same 65536 (`layout_record_too_large`), so the two sides agree by construction. A table merely DERIVED into the form is warned and keeps form `1` |
| **`--fixed-record-limit N`** | a project's own policy, off by default, and it only ever LOWERS — it cannot raise the 65536, because a gate a stranger does not honour is not a wire bound |
| **the LEAF CAP** | **A REFUSAL BY NAME, NEVER A SILENT DROP** (fix 4), on the 65536's own split: a DECLARED fixed table past it does not compile, naming the table and its leaf count; one merely DERIVED into the form is warned and keeps form `1`. The cap bounds the identity plan a backend lays down as STATIC DATA — source a consumer's compiler parses on every build. **THE FLAT-ELEMENT FOLD says what spends a leaf**: an array whose element's storage image IS its wire image, a scalar or a struct of them, is ONE leaf however long it is, so `[..8192]int32` costs two — the count and the run. What reaches the cap is a big array of a type this form must walk element by element: one carrying text, a count, a union or an optional. At run time, against the caller's own buffer, the same question is `plan_too_large` |

## 7. What a port takes, and what it must not

**The CONTRACT must not drift; the SHAPE may.** The contract is everything this page states as a byte, a
counter, a refusal name or an invariant, and the one-path promise of §4.2: two conforming ports write the same
bytes for the same values and land the same values and counters for the same bytes. The shape is how a port
gets there: one copy into a record image and a straight-line scatter, or coalesced runs into storage; the
clamps inline or after; how a plan is cached. Every good idea the nine ports found tonight was shape drift
(Rust's one-copy identity, Dart's separate flavour lane, Java's scatter) and every bug was contract drift
(a byte copied into a `bool`, residue in slack, one lane meaning two things, a private layout walk). So:
implement the reader the way your language is good at, and let the oracle decide.


**Take the SHAPE from your own PACKET codec**: a straight line of stores by field name into a buffer the caller
owns, a reader that is the writer mirrored, one ranged load per value, and the language's own allocation and
safety policy. **Take NOTHING from form `1`** — no per-field reference, no kind byte, no length, no terminator, no
id table, no tolerant probe. If your form-`1` path is where you started, you are on the wrong wire.

**Where the compiler hands you data, CONSUME IT — do not re-derive the walk.** The layout bytes and the identity
plan are compiler output, laid down as static data; a port that walks the type again has a second wire that agrees
today and drifts tomorrow. The Rust leg is the example: its private record twin re-derived the layout, and what
caught it was a Go test comparing the emitted layout arrays and hash constants against the reference entry for
entry (`compiler.TestRustFixedFormBlockIsTheCppReferenceByteForByte`). **Every port owes that test**, and the
build-time assertion that the plan's destinations equal its own `offsetof` and `sizeof`.

**Storage is not the wire.** A struct is padded and a body is not; a count or length precedes its payload ON THE
WIRE and follows the buffer IN C++ STORAGE, so never infer wire order from member order; text storage is one unit
longer than the bound where the wire is not. Two live accidents beside them: the reference's small-copy routine
is unsound for runs of 17..31 bytes, and its guard test compares one byte, so a union past 255 arms wraps. **Do
not transliterate.** Then **prove against the oracle, in this order**:

| # | the proof |
|---|---|
| 1 | **`make tables-fixedform-corpus`** writes nine form-`3` files into `build/fixedform-corpus` from the reference, values set by hand: `fx1`/`fx2` (the versioning pair, carrying a short string, a wholly unused string, a partly-used `[..4]int32` and a partly-used `bytes(6)`), `p1`/`p3` (a value against a `?T`, present and absent), `keyed` (keyed arrays nesting keyed arrays), `pack` (counted arrays, an enum off its declared default, optionals inside elements), `fxw` (the wide-text unit), `fu1`/`fu2` (text under a union arm, and §4's TWO WIDENING RUNGS — `mark int16` at `-1`, `INT16_MIN` and `INT16_MAX` into FU2's `int32`, and `heat float32` as two signalling NaNs and an ordinary `1.5` into FU2's `float64`; schema#876 cards 15 and 16). **Read a file and save it back; the bytes must be identical** — a byte a port encodes differently is a byte that does not come back. `p3` sets an absent link's payload in STORAGE on purpose and the file's bytes for it are the template's zeros, which is exactly the check |
| 2 | **read a file written under ANOTHER schema's layout** — `fx1`/`fx2` both directions, `p1` into `p3`. That is the plan path, and the whole of what the versioning invariant is worth |
| 3 | **the negative controls, one per named rule and one per named refusal**: each of §1.1's seven, over a file that reads clean and is broken in exactly one place; `layout_malformed` for a header shorter than a layout and for a header hash that is not the hash of the layout behind it; `plan_too_large` for a one-entry plan slice; `batch_too_large` for a batch past the caller's room; `previous_form`, `message_form_as_file` and `newer_form` for form bytes `1`, `2` and `6`; `no_layout` for a record hash naming nothing; `malformed` for a ragged tail. **Each comes back under ITS OWN NAME, nothing decoded, no counter moved.** A validation nobody watched fail is a validation nobody has |
| 4 | **the wrong plan must go red**, and so must each fix's own bug: this build's identity plan over another schema's record, the swapped `bytes(N)` row, the shared `arg`/`meta` lane, a whole-span copy over stained slack |
| 5 | **a byte-flip fuzz over the whole file, under a sanitizer, and it is not optional** — every offset is arithmetic over sizes a stranger wrote down, so every byte, one bit at a time, is answered one of three ways and never a fourth: a refusal by name, a `malformed` read, or a read that lands values |
| 6 | **the fixtures**, beside the corpus: `test/tables/FX1`/`FX2` (widen, `was =`, unknown field, unknown nested type, slack, `bytes(6)`), `V1`/`V2` (a variant and an arm inserted mid-list, keyed slots sliding, an optional, a moved kind), `P1`/`P3` (value against `?T`), `UT1`/`UT2` (the guard and flavour lanes), and `bench/corpus/FixedTable.schema` |

## 8. Coverage matrix stub

Nine languages, one wire. Fill a cell when that port has proved the construct against the
fixture in §7; leave it blank until it has. **C++ is the reference writer of the dump corpus,
not an exemption from the proof.** This table is a stub: the fixtures exist, the per-language
ticks are the work. It is not a second coverage page; every cell points at a fixture §7
already names.

| construct | fixture | cpp | c | go | cs | rust | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|---|---|
| dump identity of bytes | `build/fixedform-corpus` `fx1`/`fx2`/`p1`/`p3`/`keyed`/`pack`/`fxw`/`fu1`/`fu2` | golden | | | | | | | | |
| dump plan path, both directions | `fx1`↔`fx2`, `p1` into `p3`, `fu1` into FU2 (NEW-READS-OLD) | golden | | | | | | | | |
| the two widening rungs asserted after the compiled read | `fu1` into FU2: `mark int16`→`int32` SIGN-EXTENDED, `heat float32`→`float64` on the payload bits | golden | | | | | | | | |
| OLD-REFUSES-NEW | `fu2` under FU1: `layout_newer`, the file's hash, nothing decoded (COMPILE from the lock) | golden | | | | | | | | |
| widen, `was =`, unknown field, unknown nested type, slack, `bytes(6)` | `test/tables/FX1`/`FX2` | golden | | | | | | | | |
| variant/arm inserted mid-list, keyed slots sliding, optional, moved kind | `V1`/`V2` | golden | | | | | | | | |
| `?T` against a plain nesting | `P1`/`P3` | golden | | | | | | | | |
| guard ordinal and text flavour as two lanes | `UT1`/`UT2` | golden | | | | | | | | |
| keyed arrays nesting keyed arrays | dump `keyed` | golden | | | | | | | | |
| counted arrays, enum off default, optionals in elements | dump `pack` | golden | | | | | | | | |
| `FixedTable` wrapping `BenchMixed` | `bench/corpus/FixedTable.schema` | golden | | | | | | | | |
| seven layout rules, each by own name | §7 item 3 | golden | | | | | | | | |
| form bytes `1`/`2`/`6` | `previous_form`, `message_form_as_file`, `newer_form` | golden | | | | | | | | |
| `plan_too_large` / `no_layout` / ragged tail | §7 item 3 | golden | | | | | | | | |
| wrong plan, swapped `bytes(N)`, shared `arg`/`meta` | §7 item 4 | golden | | | | | | | | |
| byte-flip fuzz, sanitizer, three answers never a fourth | §7 item 5 | | | | | | | | | |

A blank cell is not a skip. It is a cell nobody has ticked on this page yet.

## Reference fixes pending

Where this page and the C++ reference or §3.4 disagree, **this page is the ruling**. Four landed while it was written.

| fix | what it is | where it stands |
|---|---|---|
| 1 | **Text and array slack** — the writer writes `length` units and `count` elements onto the zeroed template and stops, never the caller's leftovers or an element's default image | LANDED, `860d9f6f` |
| 2 | **Bool and present flags** land as `byte != 0`; the reference lands both with a plain one-byte copy, so `0x02` becomes a `bool` holding `2` | in flight (Go, Rust) |
| 3 | **Layout overrun** — an entry reaching past the writer's declared record size owes `layout_record_too_large`, and a failure to parse the reader's OWN layout owes its own name, not `plan_too_large` | validation LANDED, `3e3da58d`; the names pending |
| 4 | **The leaf cap** refuses BY NAME instead of dropping the form in silence, on the 65536's declared/derived split, and the flat-element fold says what spends a leaf | LANDED, `eeb5563b` |
| 5 | **The ranged clamp** — a ranged integer clamps on load and counts, fixed-point on the raw scale | LANDED, `a17e1b0c` — §4.6 |
| 6 | **The measurement file** — `test/bench/fixedform_measure.cpp` no longer compiles against the generated plan and is in no test target, so the ratios §3.4 quotes are unverifiable today | pending |
| 7 | **Wide kinds**, two halves — §15's refusal of the 128-bit and fixed-point families is owed by the ACCELERATORS and not by this wire, which names no kind on its emitted path; and the wide TEXT flavour has NO oracle bytes anywhere, so a leg counting BYTES where it owes UTF-16 CODE UNITS still passes | the first LANDED, `22a161c5`; the oracle in flight (Dart) |
| 8 | **Name claims** — `internal/tablenames/cpp.go` claims none of the fixed form's module-scope names where the C backend claims twenty-three, so a schema can collide with the runtime; and the layout hash is a wire identity, never a security claim | in flight (JS) |
| 9 | **The Envelope clamp count** — a union tag past the last arm landed RAW on the identity path, where it was a plain one-byte copy, and counted nothing | LANDED, `a17e1b0c` — §4.6 |
| 10 | **The `bytes(N)` row swap** — it lands through the ARRAY case, count to `aux` and elements to `dst`, where it had been written under the text convention | LANDED, `79e34542` |
| 11 | **Text content** — the reference performs NO UTF-8 or surrogate validation here, and a length past the bound clamps where §3.4 says `malformed`. The ruling: the length CLAMPS and counts, the CONTENT refuses by name as the packet reader does | pending |
| 12 | **The arg lane** — `arg` is the guard's ordinal and nothing else; `meta` is the op's own argument | LANDED, `8c15973d` |
| 13 | **Absent-optional zero** — the writer stored an optional's payload unconditionally, so a caller's untouched storage rode behind a flag that said absent | LANDED, `db52975f` |
| 14 | **The tag and ordinal rulings** — a tag past the last arm and an ordinal past the last variant land `None` and `COUNT clamped`, on the identity path as well as the compiled one, and by a straight-line pass rather than a plan entry | LANDED, `a17e1b0c` — §4.6 |
| 15 | **The prefill** — the reference resets the whole destination before every record, on both paths. Glenn's ruling: prefill the bytes the plan does not write; identity's list is empty | in flight (C #838, Rust #837) |

Three smaller divergences, recorded rather than fixed here. **(a)** §3.4's `C` table gives an enum's ordinal
width as `1, 2 or 4`, where the compiler and the reader both admit `8`, derived from the enum's top wire value.
**(b)** §3.4 says a compiled plan is "cached by hash", and the reference DOES cache it — `TableFixedPlanCache`,
the CALLER's own storage, sixty-four slots keyed by the file's hash
(`internal/codegen/cpptable/fixedruntime.go:1252-1281`, looked up and filled at
`internal/codegen/cpptable/fixedform.go:524-545`), so a second load under the same hash compiles nothing. The
line that stood here, saying the reference recompiles on every load with a non-matching hash, was FALSE and is
withdrawn. What remains is not a cache miss but §5.8 row 3: **nothing should compile at run time at all**, and
then there is no cache to keep. **(c)** The compile-time guard is the tables baseline of §18 — there is no §2.10 on the tip.
