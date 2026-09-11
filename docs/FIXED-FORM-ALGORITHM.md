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
**the 4-byte count included** and nothing else; it is a wire identity, never a security claim (fix 8). **The closed
kind set is `1..30`, `32`, `33`, `35`** — `31` (§3's framing escape) and `34` (reserved) are not in it, and a
kind outside it names a form this reader never saw: `REFUSE layout_kind_unknown`, never stepped over.

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
| 3 | `L := LE(4, b+16)`; if `20 + L > bytes`, `REFUSE layout_malformed`. Then `h := fnv1a64(b+20, L)` |
| 4 | if `h` is this build's own hash, take the baked identity plan and record size; otherwise validate the layout (§1.1) — each rule under its own name — compile a plan (§4.2), and take `record_bytes = 8 + root.size` |
| 5 | **only now** check `LE(8, b+8) == h`, else `REFUSE layout_malformed`. Checked LAST of the three, so a broken layout is never reported as a lying header |
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
  whose storage image is its wire image — is ONE run however many elements it holds.
- **one whole-body copy into a RECORD IMAGE, then a straight-line scatter.** In the image domain the record's
  declared order IS the destination's order, so every run merges and the plan is one entry — the shape a language
  takes when its storage is not the wire's: a `string(N)` stored as `N+1` units, a union that is a real tagged enum.

Coalesce inside each half, **never across the split**. **For any other hash the SAME loop runs over a plan
compiled once from the writer's layout, and CACHED BY HASH.** (note b) The compiler walks the layout against the
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
lays down one plan per supported version as static data; `LOAD` runs per file and selects a plan by hash.
Nothing parses a stranger's layout at run time.

### 5.1 BASELINE(old, new): the monotone law, at commit

`old` is the last locked shape of the unit (SPEC §18); `new` is the unit being committed. For every fixed
table `T` in `new` and every definition `D` in `T`'s closure (§5.4), with `D0` the same definition in `old`
where it existed:

```
BASELINE(old, new):
  for T in new.fixed_tables:
    if T not in old:                    continue            -- a new table is a new lineage
    if old[T] is not fixed:             REFUSE "T: fixed added" -- a different form, not a version
    for D in closure(T):
      D0 := old.lookup(D.name); if none: continue           -- added: allowed
      WIDENS(D0, D) or REFUSE "T: D: <rule> (<old> -> <new>)"
  for T in old.fixed_tables:
    if T not in new or new[T] is not fixed: REFUSE "T: fixed removed"  -- deprecate; remove only when nothing live speaks it

WIDENS(a, b):                            -- b may replace a
  kind(a) == kind(b)                     or FAIL "kind changed"       -- includes string/wstring/bytes, [N]/[..N]/[Enum]
  match kind:
    table, type:   fields(a) is a PREFIX of fields(b) by (name, position); every a-field WIDENS into its b-field;
                   a deprecated field keeps its place          or FAIL "field removed | inserted | reordered | modified"
    enum:          variants(a) prefix of variants(b) by name   or FAIL "variant removed | inserted | reordered | renamed"
    union:         arms(a) prefix of arms(b) by name; each payload WIDENS   or FAIL "arm ..."
    flags:         flags(a) prefix of flags(b)                 or FAIL "flag removed | moved"
    [N], [..N], string(N), wstring(N), bytes(N):  N(a) <= N(b) or FAIL "bound narrowed"; element WIDENS
    [Enum]T:       Enum WIDENS (its own rule); element WIDENS
    int, float:    width(a) <= width(b), same ladder, same signedness   or FAIL "narrowed | ladder | signedness"
    ranged:        range(b) ⊇ range(a), or b unranged               or FAIL "range narrowed | added"
    bits(N):       N(a) <= N(b)
    fixed(I,F):    I(a) <= I(b) and F(a) == F(b)                     or FAIL "F changed"
    ?T vs T:       optional(a) implies optional(b)                     or FAIL "optional removed"
    default:       default(a) == default(b)                            or FAIL "default changed"   -- SPEC §18
    reader limit:  limit(a) <= limit(b)
```

Every FAIL names the table, the definition, the rule, and both values. The compiler refuses to generate
against a lock the schema contradicts; CI runs the same check.

### 5.2 COMPILE(lock, T): one plan per supported version, at build time

```
COMPILE(lock, T):
  y := T.layout                                   -- the current layout, its hash H(y)
  for each older layout x in lock.lineage(T) with index >= T.floor:
    plans[H(x)] := PLAN(x, y); known[H(x)] := bytes(x)
  plans[H(y)] := IDENTITY; known[H(y)] := bytes(y)
  emit plans, known as static data

PLAN(x, y):                                        -- x older, y the reader's own; x WIDENS into y by §5.1
  walk x and y side by side, entry by entry (x is a prefix of y at every level; where a merge reordered a
  list, match by NAME and emit a remap, identity when the lists agree):
    same width:            copy
    wider in y:            widen / widenf   (COUNT widened)
    enum, prefix:          copy (the ordinals are the reader's); reordered: ordinal remap by name
    absent in x:           default            (a field y added)
    deprecated in y:       skip x's bytes     (COUNT unknown, once per plan)
    text, bytes, arrays:   copy the writer's extent; the reader's slack is template zeros
    ?T where x has T:      present := 1, then the value
  the plan is a straight-line list of ops; no per-record decision
```

Because §5.1 held at every commit, PLAN never fails at build time; a lineage that is not monotone is a bug
in the lock, refused by name at build.

### 5.3 LOAD(R, file): select by hash, never parse a stranger

```
LOAD(R, file):                                     -- R the reader's static data for T
  h := file.layout_hash
  if h not in R.known:
    if h in lock.lineage(T) below floor:  REFUSE layout_unsupported
    else:                                 REFUSE layout_newer      -- an older reader given a newer file
  if file.layout_bytes != R.known[h]:     REFUSE layout_malformed  -- a lie about a known version
  if file.record_bytes != size(R.known[h]) or rest % record_bytes != 0: REFUSE layout_malformed
  for each record: run R.plans[h] (§4), then the hostile pass (§4.5, §4.6: a forged count past the WRITER'S
    OWN bound, an ordinal past the writer's own variant count, a tag past its arm count, a bool byte not 0/1)
```

REFUSE is total: no counter moves, nothing decoded. `layout_newer` carries the file's hash; the refusal text
names, where the language has text, the first definition the reader could not hold, read off the file's
layout against the reader's own (this is the one walk over a stranger's layout, and it happens only inside a
refusal, after the decision, with the seven §1.1 checks applied to it first).

### 5.4 The closure

`closure(T)` is `T`, every `table` or `type` it reaches by value, every enum, union, flags and constant any
of them names, recursively. Every table or type in it is declared `fixed`; a pointer, map or unbounded array
in it is a compile refusal; `T` is never in its own closure.

### 5.5 What is retired by this section

The plan compiler at run time (§4.1–§4.4 become the build-time PLAN of §5.2); reading a layout the lock has
never seen; the count clamp across bounds, the range clamp across versions, the remap of an unknown variant
to `None`, the drop-and-count of an unknown field (all forward reads, now `layout_newer`). The seven §1.1
checks stay as the lock's validation of what it records and as the walk inside a refusal. Every hostile
check on a KNOWN layout's records stays (§4.5, §4.6).

### 5.6 The tests

One test per row of §5.1, red first, in the compiler: the baseline REFUSES the narrowing and names it, and
ACCEPTS the widening. One fixture per row of §5.2 in the reference (an older file read by the current
build, the landed values and counters asserted), ported to every leg. One fixture per refusal of §5.3
(`layout_newer`, `layout_unsupported`, `layout_malformed` on a known hash), reference first, every leg.

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
| 1 | **`make tables-fixedform-corpus`** writes six form-`3` files into `build/fixedform-corpus` from the reference, values set by hand: `fx1`/`fx2` (the versioning pair, carrying a short string, a wholly unused string, a partly-used `[..4]int32` and a partly-used `bytes(6)`), `p1`/`p3` (a value against a `?T`, present and absent), `keyed` (keyed arrays nesting keyed arrays), `pack` (counted arrays, an enum off its declared default, optionals inside elements). **Read a file and save it back; the bytes must be identical** — a byte a port encodes differently is a byte that does not come back. `p3` sets an absent link's payload in STORAGE on purpose and the file's bytes for it are the template's zeros, which is exactly the check |
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
| dump identity of bytes | `build/fixedform-corpus` `fx1`/`fx2`/`p1`/`p3`/`keyed`/`pack` | golden | | | | | | | | |
| dump plan path, both directions | `fx1`↔`fx2`, `p1` into `p3` | golden | | | | | | | | |
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
**(b)** §3.4 says a compiled plan is "cached by hash"; the reference recompiles on every load with a
non-matching hash. **(c)** The compile-time guard is the tables baseline of §18 — there is no §2.10 on the tip.
