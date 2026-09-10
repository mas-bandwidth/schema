# The packet wire as an algorithm

**This is the document a port implements the packet wire from.** `docs/SPEC.md` §4.3–§4.12, §5 and §6
state the wire and its reasons; this page states the same wire as steps, in one notation, so nine
languages write the same code. Where the two disagree SPEC.md is the law and this page is the bug.
**The reference is not the specification**: it has accidents, and the ports that transliterated it
inherited them as law. Implement from this page, prove against the oracle (§6), open the C++ only
for a byte question.

This is the packet twin of the fixed-form algorithm page (`docs/FIXED-FORM-ALGORITHM.md` on #843).
Glenn: the pseudocode can be written for both. A packet carries **no form byte, no layout, no hash,
no kind byte, no self-describing framing, no terminator, no trailer**. Knowledge is in the generated
code on both ends. The bytes are the classic serialize twins named in SPEC §4.3, bit for bit.

## 0. The notation

```
u8 u16 u32 u64 i32 i64     unsigned/signed integers of that width
bitlen(x)                  the bit length of x; bitlen(0) is 0, bitlen(1) is 1
CEIL8(n)                   (n + 7) / 8
struct N { f : T ; ... }   a record of named fields
for i in 0 .. n            i takes 0, 1, ... n-1
REFUSE                     stop; decode nothing further; the output object is unspecified
ASSERT cond                write-side only; debug; gone in release where the language has that idiom
```

The stream is a bit cursor over a byte buffer, not a byte array of fields. Every integer that
occupies more than one byte on the wire is **little-endian in the serialize sense**: serialize
defines network byte order as little-endian, so a host-endian machine stores a scratch qword
as-is and a big-endian machine byte-swaps it. **Bit order is LSB-first**: bit `i` of the stream
lives in byte `i/8` at bit position `i%8`. Nothing is aligned or padded between fields unless the
schema writes `align`, or a `string`/`bytes` payload forces a byte boundary.

`REFUSE` is total: a read failure is terminal, nothing after it has a defined position, and the
output object is unspecified (SPEC §5). `ASSERT` is never a read-side check.

## 1. The stream

A **write buffer** is a multiple of 8 bytes, at least `T.MaxBytes`. A **read allocation** extends
**at least 8 bytes past the data**: the reader loads an unconditional 64-bit window at byte
granularity, and those slack bytes are loaded and never interpreted. Size a write buffer from
`MaxBytes`. Size a receive allocation at `n + 8`, not `n`.

```
struct WriteStream {
    data         : u8[]     // length multiple of 8
    scratch      : u64      // bits packed low to high
    scratch_bits : 0..63
    bits_written : i64
    word_index   : i64      // next qword store is at data + 8*word_index
}
struct ReadStream {
    data      : u8[]        // allocation extends 8 bytes past num_bytes
    num_bytes : i64         // the packet, not the allocation
    bits_read : i64
}
```

### 1.1 PUT_BITS and GET_BITS

The primitive width is `[1, 32]`. Wider values split into 32-bit groups, least-significant group
first (§1.4). A bit count of 0 is not a primitive: a degenerate range emits no call (SPEC §4.6).

```
PUT_BITS(w, value, n):                         // n in [1,32]; value in [0, 2^n - 1]
    ASSERT n in [1,32] and value fits in n bits
    ASSERT w.bits_written + n <= 8 * len(w.data)
    w.scratch |= u64(value) << w.scratch_bits
    new := w.scratch_bits + n
    if new >= 64:
        store LE qword w.scratch at w.data + 8*w.word_index
        w.word_index += 1
        w.scratch = u64(value) >> (64 - w.scratch_bits)
        w.scratch_bits = new - 64
    else:
        w.scratch_bits = new
    w.bits_written += n

GET_BITS(r, n) -> u32:                         // n in [1,32]
    if r.bits_read + n > 8 * r.num_bytes: REFUSE     // exhaustion is a read failure
    window := LE u64 loaded from r.data + (r.bits_read >> 3)   // may read 7 bytes of slack
    out := u32( window >> (r.bits_read & 7) ) & u32( (u64(1) << n) - 1 )
    r.bits_read += n
    return out
```

A successful `GET_BITS` **masks** the window: the decoded chunk is in `[0, 2^n - 1]` by
construction. A read-side range check is only needed when the declared span leaves headroom in
that encoded width.

```
FLUSH(w):
    if w.scratch_bits != 0:
        store LE qword w.scratch at w.data + 8*w.word_index
        // high unused bits of scratch are 0, so bytes past the written data in that qword are zeros
        w.scratch = 0
        w.scratch_bits = 0
        w.word_index += 1

BYTES_WRITTEN(w) := CEIL8(w.bits_written)      // call FLUSH first
```

**Always `FLUSH` after the last field**, then take `BYTES_WRITTEN`. Missing the flush drops the
last scratch qword. The logical length is `bits_written`; the packet size is `CEIL8(bits_written)`
bytes. Trailing bits in the last byte are zeros from the flush, and a reader that does not consume
them does not check them.

### 1.2 ALIGN

```
ALIGN_WRITE(w):
    rem := w.bits_written % 8
    if rem != 0: PUT_BITS(w, 0, 8 - rem)       // already aligned: zero bits

ALIGN_READ(r):
    rem := r.bits_read % 8
    if rem != 0:
        v := GET_BITS(r, 8 - rem)
        if v != 0: REFUSE                      // nonzero padding
```

### 1.3 BYTES

`string(N)` and `bytes(N)` ride as length, **align**, then the used bytes. The align is inside
the byte primitive, not a separate schema item:

```
PUT_BYTES(w, src, n):
    ALIGN_WRITE(w)
    if w.scratch_bits != 0:
        store LE qword w.scratch at w.data + 8*w.word_index
    copy n bytes of src to w.data at byte cursor w.bits_written/8
    w.bits_written += 8*n
    w.word_index = w.bits_written / 64
    reload the trailing partial qword into w.scratch, masked to its tail bits
    w.scratch_bits = w.bits_written % 64

GET_BYTES(r, dst, n):
    ALIGN_READ(r)
    if n > bits remaining / 8: REFUSE
    copy n bytes from r.data at byte cursor r.bits_read/8 into dst
    r.bits_read += 8*n
```

`PUT_BYTES`/`GET_BYTES` at a cursor that is **not** already byte-aligned insert padding the
schema did not declare. The bulk path for a fixed `[N]uint8` is legal **only** when a static
walk has proved the cursor is ≡ 0 (mod 8) at that field — after an `align`, after a
`string`/`bytes` payload, or any position so proved. Entry into a nested type is at an unknown
bit offset, so nothing before the first alignment-forcing item is ever bulk. A counted
`[..N]uint8` is not bulk. Off-boundary bulk is contract drift: it writes an align the per-byte
loop would not.

### 1.4 Groups wider than 32

All quantities wider than 32 bits go **low 32 bits first**, then the high remainder. The 128-bit
family and fixed point generalize the same rule: full 32-bit groups from the least significant
upward, the final group carrying the remainder, up to four groups.

```
PUT_VALUE(w, value, n):                        // n in [1,64]
    if n <= 32: PUT_BITS(w, u32(value), n)
    else:
        PUT_BITS(w, u32(value), 32)
        PUT_BITS(w, u32(value >> 32), n - 32)

GET_VALUE(r, n) -> u64:                        // n in [1,64]
    if n <= 32: return GET_BITS(r, n)
    lo := GET_BITS(r, 32)
    hi := GET_BITS(r, n - 32)
    return (u64(hi) << 32) | lo

PUT_GROUPS32(w, value, n):                     // n in [1,128]; value is the unsigned offset
    // group0 = low 32, group1 = next, group2, group3 = high
    emit groups of 32 from the bottom until n bits are placed; the last group is n % 32
    (or 32 if n is a multiple of 32)

GET_GROUPS32(r, n) -> unsigned:                // n in [1,128]
    read the same groups, low first; refuse if the stream ends mid-group
```

## 2. The writer

There is no generated measure. `Write` returns the actual size after `FLUSH`; `MaxBits` /
`MaxBytes` size the buffer (SPEC §6.1). Generated write is a straight line of field operations
in declared order, honest loops for arrays, honest branches for `if` (SPEC §6.2).

```
WRITE_PACKET(T, buf, value) -> bytes:
    w := WriteStream(buf)                      // len(buf) multiple of 8, >= T.MaxBytes
    WRITE(T, w, value)
    FLUSH(w)
    return BYTES_WRITTEN(w)

WRITE(T, w, value):
    for item in T.items in declared order:
        WRITE_ITEM(w, item, value)
```

An empty body writes nothing: presence is the payload (SPEC §4.6). A write of trusted in-range
values always produces the bytes. Buffer overrun is caller error (`ASSERT`). Out-of-range values,
a count past its bound, a length past `N`, a union tag past `Max`, and an interior 0x00 in a
`string`/`wstring` on write are **writer contracts**: `ASSERT` in debug, gone in release wherever
the language has that idiom, every-build only in Go and Elixir (SPEC §5). What a release build
writes from a broken contract is defined but unspecified. **No write-side check is a substitute
for the reader.**

### 2.1 bits_required

```
bits_required(min, max):
    if min == max: return 0
    return bitlen(max - min)                   // subtract in the unsigned domain of the range
```

Computed in the range's own width (32, 64, or 128). `min == max` costs **zero bits**, not one.

### 2.2 WRITE_ITEM

| item | write |
|---|---|
| `const(V, N)` | `PUT_VALUE(w, V, N)` |
| `reserved(N)` | `PUT_VALUE(w, 0, N)` |
| `align` | `ALIGN_WRITE(w)` |
| `if cond { A } else { B }` | evaluate `cond` (a previously written `bool`, optionally negated); write `A` or `B`; the branch itself costs no bits |
| a field | `WRITE_FIELD` below |

### 2.3 WRITE_FIELD

**Arrays.** `[N]T`: `N` elements, back to back, no count. `[Min..N]T` / `[..N]T`: the count is a
ranged integer over `[Min, N]` (Min is 0 for `[..N]T`), then that many elements. `ASSERT` the
count is in range, then:

```
PUT_RANGED(w, count, Min, N)
for i in 0 .. count: WRITE_SCALAR(element i)
```

A count below Min or above N in release is an out-of-bounds read of the caller's array and of
the stream buffer. The debug assert is where a bad count is meant to be caught (SPEC §4.6). The
count range is never degenerate: `[Min..N]T` with Min ≥ N is a compile error.

**A field that is not an array** is `WRITE_SCALAR`.

### 2.4 WRITE_SCALAR

`PUT_RANGED` is the integer wire:

```
PUT_RANGED(w, value, min, max):
    ASSERT min <= value <= max
    n := bits_required(min, max)
    if n == 0: return                          // the value IS the range; no call
    offset := unsigned(value) - unsigned(min)  // unsigned domain: ranges crossing 0, and spans > 2^31
    PUT_GROUPS32(w, offset, n)                 // 1..32 is PUT_BITS; 33..64 is PUT_VALUE; 65..128 is four groups
```

| field | write |
|---|---|
| `bool` | `PUT_BITS(w, 0 or 1, 1)` |
| `bits(N)` | `PUT_VALUE(w, value, N)`, N in [1,64] |
| bare `intN` / `uintN` | `N` raw bits. Signed width `< 64`: cast to the **same-width unsigned** first, then `PUT_VALUE` — a signed value converted to u64 sign-extends and the high bits corrupt the stream |
| ranged `intN` / `uintN` | `PUT_RANGED` |
| `uint128` (bare) | 128 raw bits, low 64-bit half first (`PUT_VALUE` of the low 64, then the high 64) |
| `int128 \| min, max` | `PUT_RANGED` over the 128-bit range; where the range fits 64 bits or fewer the bytes are identical to `int64` over the same bounds. Bare `int128` and ranged `uint128` do not compile |
| `fixed(I,F)` / `ufixed(I,F)` | storage is the raw scaled integer. Offset is `raw - (min << F)` in `bitlen(max - min) + F` bits, groups from the bottom. `min == max` costs **zero bits, not F**; `ASSERT raw == min << F` and emit nothing. Read rejects above the raw range, never clamps. Round trip is exact |
| `float32` | the IEEE-754 bit pattern as 32 bits, **no canonicalisation**: a signalling NaN and a NaN payload ride through. Move the pattern by bit surgery, never through a float conversion that would set the quiet bit |
| `float64` | the IEEE-754 bit pattern as 64 bits, low dword first, no canonicalisation |
| compressed `float32 \| min, max, resolution` | see §2.5 |
| an enum `E` | `PUT_RANGED(w, ordinal, 0, E.Max)` — `None = 0`, variants dense from 1, `Max` may exceed the variant count (`\| max = K` headroom; non-variant values are wire-legal) |
| a `flags` mask | `W` raw bits, `W` = variant count or the widened max. `ASSERT value < 2^W` when `W < 64` (a bit above the wire width is writer misuse, not silent truncation). Every pattern of `W` bits is legal |
| a nested `type` | `WRITE` of that type, in place |
| a `union` | §2.6 |
| `string(N)` / `bytes(N)` | `PUT_RANGED(w, length, 0, N)` then `PUT_BYTES(w, buf, length)`. `string`: `ASSERT` no 0x00 in the used bytes. `bytes`: no interior-null assert. Length 0 is a legal empty payload; `PUT_BYTES` still aligns |
| `wstring(N)` | `PUT_RANGED(w, length, 0, N)` then **one 32-bit group per code unit, and no align anywhere**. `ASSERT` no zero code unit among the used units. Surrogate pairing is **not** checked on write; a unit above `0xFFFF` cannot be stored |

### 2.5 Compressed float (write)

Classic `serialize_compressed_float` is the twin. Constants are schema facts, folded at
generation time with float32 arithmetic:

```
delta     := float32(max) - float32(min)
values    := delta / float32(res)
values    := clamp to [1, 4294967040]
steps     := ceil(values)                      // uint32
wire_bits := bits_required(0, steps)
```

A triple that is not finite in float32, or that overflows or requires that clamp, does not
compile (SPEC §4.6). On write:

```
ASSERT value is finite
normalized := (value - min) / delta
normalized := clamp to [0, 1]                  // NaN forced in via !>= / !<=
scaled := normalized * steps
FORCE_ROUND(scaled)                            // the product rounds to float32 BEFORE 0.5 is added
integer := floor(scaled + 0.5)
if integer > steps: integer := steps           // normative once steps >= 2^23
PUT_BITS(w, integer, wire_bits)
```

**Two roundings, never one FMA.** A fused multiply-add diverges by one quantization step at
boundary values. Do not fold `normalized * steps + 0.5` into one expression. Do not drop the
force-round barrier. The C and C++ conformance builds compile `-ffp-contract=off`; a port that
contracts this arithmetic writes different bytes on arm64 than on x86.

### 2.6 Union (write)

Tag in `bits_required(0, variant_count)` bits, then **the selected arm's payload only**. Tag 0
is `None` and costs the tag bits only. A payload-free arm is the tag alone. `ASSERT tag <= Max`
before it rides. Then:

```
switch tag:
    None:          PUT_RANGED(w, 0, 0, Max); return
    arm ordinal k: PUT_RANGED(w, k, 0, Max); WRITE of that arm's payload (nothing if void)
                   // k is 1, 2, ... in declared order; None is 0
    out of set:    no bits ride                           // C++ shape of unspecified; do not copy it
```

What a release build does with an out-of-set tag is **defined but unspecified** (SPEC §4.8): no
arm is selected, so the payload never rides, but C++ writes no tag bits at all while C writes the
tag unmasked. A reader may refuse the result or may accept some other message. That is the
caller's responsibility. Rust cannot build an out-of-set tag. Go and Elixir refuse it every
build, as they refuse every other write contract.

An empty union (`Max = 0`) holds only `None` and costs zero bits.

## 3. The reader

Reads validate **everything**, in every build, in all nine targets (SPEC §5): ranges, enum
bounds, union tags, alignment padding, constants, reserved bits, counts, lengths, interior
nulls, UTF-8, UTF-16 well-formedness, and buffer exhaustion. A value that controls iteration is
never used before it has been bounded. **Reject, never clamp.**

```
READ_PACKET(T, buf, n, out) -> ok, bits:
    // allocation of buf extends at least 8 bytes past n
    r := ReadStream(buf, n)
    if not READ(T, r, out): return fail
    return ok, r.bits_read
    // bytes remaining after the last field are the caller's concern, not a refusal
    // a stream framed over a union ends on the in-band None tag by design

READ(T, r, out):
    for item in T.items in declared order:
        READ_ITEM(r, item, out)
```

A refusal is terminal: generated code returns on the first failure, later items are not reached,
and `out` is unspecified (SPEC §5). Past-end, a ranged offset above the span, and nonzero align
padding **poison** the cursor in the runtime (one bit past the end is enough: every later
`GET_BITS` refuses, zero-width included). Content checks — UTF-8, interior null, unpaired
surrogate — return without a further stream operation. Callers use `out` only on success. Read
success fully initializes the received fields and used prefixes.

### 3.1 READ_ITEM

| item | read |
|---|---|
| `const(V, N)` | `v := GET_VALUE(r, N)`; if `v != V`: `REFUSE` |
| `reserved(N)` | `v := GET_VALUE(r, N)`; if `v != 0`: `REFUSE` |
| `align` | `ALIGN_READ(r)` — nonzero padding `REFUSE` |
| `if cond { A } else { B }` | evaluate `cond` already read; **read the taken side and ZERO the other** |

**Zero values, not specified defaults**, on the untaken side: 0, 0.0, false, empty bytes, zero
count, `None` for an enum, `None` for a union, recursively zeroed for a nested type,
element-wise zeroed for a fixed array. Ordinary construction and union selection apply declared
defaults. The wire stays a pure function of the encodings.

**The tail rule:** elements past a used count and bytes past a used length are **not rewritten**.
A union's unselected arms are not rewritten. A reused object keeps prior tail data. **One
stated exception:** in C and C++, a successful `string(N)` read writes the zero byte at index
`length`, and a successful `wstring(N)` read writes the zero unit at index `length`. Nothing
else past a used length or used count is written, in any target.

### 3.2 GET_RANGED

```
GET_RANGED(r, min, max) -> value:
    n := bits_required(min, max)
    if n == 0: return min                      // after the past-end check on a failed stream
    offset := GET_GROUPS32(r, n)
    if offset > unsigned(max) - unsigned(min): REFUSE     // only when the span leaves headroom
    return min + offset                        // add in the unsigned domain, then convert
```

The past-end check runs **before** the degenerate case, so a failed stream refuses a zero-bit
read too. Generated C++ materializes a degenerate field with no stream call, which is SPEC
§4.6's "no wire call at all"; it is safe only because a previous failure already returned.
A port may consult the failure latch on a zero-bit field. It must not emit a bit.

### 3.3 READ_FIELD / READ_SCALAR

**Arrays.** `[N]T`: read `N` elements. `[Min..N]T`: `count := GET_RANGED(r, Min, N)` — a count
outside `[Min, N]` fails **in every build** — then read `count` elements. Do not rewrite the
tail.

**Bulk `[N]uint8`:** `GET_BYTES` only at a proven byte boundary, the write's own condition.
Otherwise a per-byte loop of 8-bit reads. The two are byte-identical when already aligned
because the internal align contributes zero bits.

| field | read |
|---|---|
| `bool` | `GET_BITS(r, 1)` as 0 or 1. A 1-bit field cannot hold any other value |
| `bits(N)` | `GET_VALUE(r, N)` |
| bare `intN` / `uintN` | `N` raw bits, then a cast to the storage type. Signed: the bits are two's complement |
| ranged integer / `int128` | `GET_RANGED`; reject, never clamp |
| `uint128` | two `GET_VALUE` of 64, low then high |
| `fixed` / `ufixed` | read the offset in `bitlen(max-min)+F` bits (zero when `min == max`); if offset > raw span `REFUSE`; `raw := (min << F) + offset` |
| `float32` / `float64` | 32 / 64 raw bits as the IEEE-754 pattern, no repair |
| compressed `float32` | §3.4 |
| an enum | `GET_RANGED(r, 0, E.Max)`; a value in headroom stands |
| a `flags` mask | `GET_VALUE(r, W)`; every pattern legal |
| a nested `type` | `READ` of that type |
| a `union` | §3.5 |
| `bytes(N)` | `length := GET_RANGED(r, 0, N)` then `GET_BYTES`. No UTF-8 rule, no interior-null rule |
| `string(N)` | `length := GET_RANGED(r, 0, N)` then `GET_BYTES`. Then **interior null** over the used bytes, then **UTF-8** over the used bytes (Unicode Table 3-7). Either `REFUSE`. Then the C terminator at `[length]` in C and C++ only |
| `wstring(N)` | §3.6 |

### 3.4 Compressed float (read)

```
integer := GET_BITS(r, wire_bits)
if integer > steps: REFUSE
normalized := integer / float32(steps)
scaled := normalized * delta
FORCE_ROUND(scaled)                            // product rounds to float32 BEFORE min is added
value := scaled + min
```

Same two-rounding rule as write. An integer smuggled into the bit headroom above `steps` is
content the read refuses.

### 3.5 Union (read)

```
tag := GET_RANGED(r, 0, Max)                   // refuses a tag above the count, every build
out.type := tag
switch tag:
    None: return
    arm i, void: return
    arm i, payload:
        construct the selected arm with its declared defaults (recursively)
        READ of that arm into the selected payload
        return
REFUSE                                         // unreachable if GET_RANGED held; keep it
```

Construction runs on **every** selection, including when the tag repeats. Unselected arms are
unspecified. Consumers read the selected arm only. An empty union reads as `None` and consumes
no bits.

### 3.6 `wstring(N)` (read)

Length in `[0, N]`, then one 32-bit group per code unit, **no align**. The length is bounded
before the loop, so it never drives a copy it has not been bounded for. In one pass over the
groups:

1. `GET_BITS(r, 32)` — exhaustion mid-group `REFUSE`.
2. `group == 0` or `group > 0xFFFF`: `REFUSE` (interior null in code-unit terms; not a code unit).
3. Surrogate pairing: a high surrogate (`0xD800..0xDBFF`) sets `expect_low`; the next group must
   be a low surrogate (`0xDC00..0xDFFF`). A low without a high, a high not followed by a low, and
   a high as the **final** group all `REFUSE`. Well-formed pairs are valid.
4. Store `u16(group)` at index `i`.

After the loop, if `expect_low` still holds, `REFUSE`. Then the C terminator at `[length]` in C
and C++ only.

**What no reader enforces:** noncharacters (`0xFFFF` included), normalisation, case folding,
code-point count. A reader that adds a check here is as wrong as one that drops a check above.

### 3.7 `string(N)` content

Used bytes only, never the unused tail.

**Interior null:** any `0x00` in `[0, length)` `REFUSE`. NUL is valid UTF-8; this rule is
stricter and its own. The C++ word-wise scan (eight-byte steps, overlapping last word) is
**shape**. The contract is the verdict.

**UTF-8:** well-formed under Unicode Table 3-7. Overlong forms, surrogates encoded in UTF-8,
code points above `10FFFF`, truncated sequences, bare continuations, and the bytes `0xFE` /
`0xFF` all `REFUSE`. Generated-code validation in every target; no runtime primitive performs
the interior-null check, and three targets have no runtime at all.

A refusal is terminal. No target traps, panics or aborts on a malformed payload. Arbitrary
payloads use `bytes(N)`.

## 4. MaxBits, MaxBytes, alignment

`MaxBits` is the longest path through the type: branches take the larger side, counted arrays
and text take their bound, each `align` and each `string`/`bytes` alignment point costs a
worst-case **7** bits of pad. `wstring` has no padding term: `bits_required(0, N) + 32*N`.
A union is tag bits plus the largest arm; `None` and a void arm cost the tag only.

```
MaxBytes(bits) := align_up( align_up(bits, 8) / 8, 8 )
```

Rounded up to the **8-byte write-buffer granularity** every serialize runtime requires. A
constant advertised for sizing a write buffer must be directly usable as one. Conservative is
correct.

**There is no generated measure function.** `Write` returns the actual size. Alignment is
**stream-relative**: a type containing `align`, `string` or `bytes` has layout that depends on
its entry bit offset. Generated functions are correct at any entry offset; the same type works
standalone and nested. `MaxBits` covers the worst case.

**Canonical encoding is a contract:** equal post-quantization values produce identical bytes,
deterministically, across compiler versions. The golden-wire gate pins it. A canonicalization
slip is a correctness bug, not a size regression.

## 5. Trust

**Writes assume trusted data. Reads face untrusted bytes.** There is no exception to the tier
split (SPEC §5). A counted array's count and a union's tag are writer contracts like every
other range: debug-only where the language has the idiom, every-build only in Go and Elixir.
The read refuses the same numbers in every build, in all nine.

Text well-formedness is **not** a write-side contract. UTF-8 in `string(N)` and UTF-16 in
`wstring(N)` are read-side refusals in every target, so the guarantee rests on the reader
rather than on a write-side check some targets carry and others do not.

## 6. The oracle fixtures

Prove against these, in this order. A byte a port encodes differently is a byte that does not
come back. **Do not re-pin a golden under an unchanged schema** — that is stop-the-line
(SPEC §3.1, §7.2 gate 7).

| # | the proof |
|---|---|
| 1 | **The paired runner's packet fixtures.** `bench/corpus/Bench.schema` is the sole definition of `BenchMixed`. `testdata/wire/bench_mixed.bin` is the canonical packet golden; `testdata/wire/bench_mixed.bits` is its logical bit count. `bench/corpus/variants/bench_mixed.variants.bin` is 64 records, whole rotations, **the first record identical to the golden**. `test/bench/paired_main.cpp` reads those packet bytes, decodes each once, and requires that a table load/save round-trip rewritten onto the packet wire reproduce the original packet bytes. There is no second field list. This is the corpus the paired runner already times |
| 2 | **The rest of the packet wire goldens**, produced by generated C++ and pinned beside their `.bits` counts: `testdata/wire/bench_packet.bin`, `bench_ints.bin`, `bench_bits.bin`, `real_packet.bin` (the all-defaults `RealPacket`), `chat.bin`, `clauses.bin`, `joins.bin`, `inputpacket.bin`, `testdata.bin`, `probe_header.bin`, `probebits.bin`, `probearray.bin`, `probecollider.bin`, `compressed_probe.bin`, `degenerate.bin`, `degenerate_probe.bin`, `unsigned_probe.bin`, `rigidbody_at_rest.bin`, `rigidbody_moving.bin`, `shipcreate_flags.bin`, `ludicrous_state.bin`, `ludicrous_state_untargeted.bin`. **Read a file and write it back; the bytes and the bit count must be identical** |
| 3 | **`testdata/wire/packet-defaults/`** — `sample-defaults`, `sample-empty`, `sample-short`, `zero-count`, `conditional-on`, `conditional-off`, `choice-sample`, `batch-defaults`. Defaults initialize storage and do not elide fields on this wire |
| 4 | **Classic serialize's shared corpora**, the twins SPEC §4.3 names: `serialize/conformance/string.txt` (a `string(15)` field, 4-bit length, thirteen refusals and their accepting twins), `wstring.txt` (a `wstring(7)` field; the row's own golden is `wstring-worked-example`), plus `bits.txt`, `bool.txt`, `bytes.txt`, `int.txt`, `int64.txt`, `int128.txt`, `uint128.txt`, `float.txt`, `double.txt`, `compressed_float.txt`, `fixed.txt`, `align.txt`. Gate 3: generated C++ byte-identical to the hand-written twin, each decoding the other |
| 5 | **Selected-arm construction.** `examples/ArmDefaults.schema`: seven-bit independent packet oracle, byte `0x51` — arm `first`, zero used entries, marker `5`. Write the byte and the bit count, then read it twice. `make packet-arm-defaults-negative-controls` restores zero-only selection and requires the selected-payload default assertion to fail |
| 6 | **The packet check suites**, C++ the oracle, every other port a negative control: `make packet-void`, `make packet-defaults`, `make packet-text` / `packet-wide-nine` (UTF-8 and `wstring(N)`), `make packet-wire-nine` |
| 7 | **The negative controls, one per named refusal**: const mismatch; reserved nonzero; align nonzero; ranged offset above the span; count outside `[Min, N]`; length outside `[0, N]`; interior 0x00 in `string`/`wstring`; malformed UTF-8 (the thirteen vectors); `wstring` group above `0xFFFF`, unpaired surrogates, interior zero group; union tag above the count; compressed-float integer above `steps`; exhaustion mid-field. **Each refuses, nothing further decoded.** A validation nobody watched fail is a validation nobody has |
| 8 | **Bit-flip sweeps over valid packets** (SPEC §7.2 gate 5), under a sanitizer: every generated reader agrees on accept/reject, and on the decoded value when accepted. Exhaustion, a broken const, and a hostile count are three answers, never a fourth |
| 9 | **The examples corpus** as the language's proving ground: `examples/Wire.schema` (const, reserved, align, bits at 9/33/64, compressed float, nested `if`, counted `[1..8]`, union), `Types.schema`, `Enums.schema`, `Clauses.schema`, `Joins.schema`, `Degenerate.schema`, `ArmDefaults.schema`, `Render.schema`, `examples128/Ludicrous.schema`, `examples-wide/WideText.schema`. Gate 1 pins generated source; gate 7 pins the bytes |

The `.bits` file is a decimal logical bit count, one integer, newline. The `.bin` file is
`CEIL8(bits)` bytes. Both are the oracle. Matching the bytes and missing the bit count is still
red.

## 7. Coverage matrix stub

Nine languages, one wire. Fill a cell when that port has proved the construct against the
fixture in §6; leave it blank until it has. **C++ is the reference writer of the goldens, not
an exemption from the proof.** This table is a stub: the fixtures exist, the per-language ticks
are the work.

| construct | fixture | cpp | c | go | cs | rust | java | js | dart | elixir |
|---|---|---|---|---|---|---|---|---|---|---|
| LSB-first bits, low-32-first >32 | `probebits`, `bench_bits` | golden | | | | | | | | |
| `const` / `reserved` / `align` | `probe_header` | golden | | | | | | | | |
| ranged int, incl. min≠0 | `testdata`, `bench_ints` | golden | | | | | | | | |
| degenerate min==max (zero bits) | `degenerate`, `degenerate_probe` | golden | | | | | | | | |
| full-range uint32 / uint64 | `unsigned_probe`, `probebits` | golden | | | | | | | | |
| `bool`, bare float, `bits(N)` | `bench_packet`, `bench_mixed` | golden | | | | | | | | |
| compressed float, two roundings | `compressed_probe` | golden | | | | | | | | |
| `if` / `else`, nested, zero of untaken | `rigidbody_*`, `packet-defaults/conditional-*` | golden | | | | | | | | |
| `[N]T` / `[Min..N]T` | `probearray`, `testdata` | golden | | | | | | | | |
| `string(N)` length + align + bytes | `chat` | golden | | | | | | | | |
| `string(N)` UTF-8 + interior null | `serialize/conformance/string.txt`, `packet-text` | | | | | | | | | |
| `bytes(N)` | `serialize/conformance/bytes.txt`; `Block` in `Wire.schema` | | | | | | | | | |
| `wstring(N)` groups, no align | `wstring.txt`, `packet-wide-nine` | | | | | | | | | |
| enum with `\| max` headroom | `probearray` (`Weapon`) | golden | | | | | | | | |
| `flags` | `shipcreate_flags`, `ProbeFlags` | golden | | | | | | | | |
| union tag + selected arm, None, void | `probecollider`, `packet-void`, `ArmDefaults` `0x51` | golden | | | | | | | | |
| `int128` / `uint128` / `fixed` / `ufixed` | `ludicrous_state`, `fixed.txt` | golden | | | | | | | | |
| specified defaults do not elide | `packet-defaults/` | golden | | | | | | | | |
| empty type (zero bits) | `Heartbeat` in `Wire.schema` | golden | | | | | | | | |
| NaN / signalling NaN bit-identity | PORTING.md float row; corpus pins | | | | | | | | | |
| paired 64-record `BenchMixed` | `bench_mixed.bin` + variants | golden | | | | | | | | |
| bit-flip agreement | SPEC §7.2 gate 5 | | | | | | | | | |
| write-side debug-only vs every-build | SPEC §5 tiers | | | | | | | | | |

A blank cell is not a skip. It is a cell nobody has ticked on this page yet.

## 8. What a port takes, and what it must not

**The CONTRACT must not drift; the SHAPE may.** The contract is everything this page states as
a bit, a byte, a refusal, a zero-bit degenerate, a two-rounding compressed float, a terminator
exception, or an invariant, and the one-path promise: two conforming ports write the same bits
for the same values and land the same values — or the same refusal — for the same bits. The
shape is how a port gets there: folded `bits_required` at generation time versus a runtime call;
bulk `PUT_BYTES` at a proven byte boundary versus a per-byte loop; a word-wise interior-null
scan versus a byte loop; inlining demands; how a bool is stored; how a union is laid out in
memory. Every good idea the nine ports have found on this wire was shape drift (C++'s folded
writes, the bulk `[N]uint8` path, the always-inline read demand) and every bug was contract
drift (an FMA in the compressed float, a sign-extended signed field, bulk bytes off a byte
boundary, a write-side check compiled into release). So: implement the codec the way your
language is good at, and let the oracle decide.

**Take NOTHING from the table wire** — no per-field reference, no kind byte, no length prefix
on a type, no terminator, no id table, no clamp, no `malformed` counter. If your table path is
where you started, you are on the wrong wire. The fixed form's shape advice is the reverse of
this page: it tells ports to take their shape from **this** codec.

**Storage is not the wire.** A struct is padded and a body is not; a count or length precedes
its payload on the wire and follows the buffer in C++ storage, so never infer wire order from
member order; text storage is one unit longer than the bound in C and C++ where the wire is
not; a `bool` in memory is not a license to write more than one bit. **Do not transliterate.**
The C++ reference's accidents, which are not law:

- `SCHEMA_WRITE_INLINE` / `SCHEMA_READ_INLINE` — an inlining demand. Shape.
- Generator-folded `PUT_VALUE` of a compile-time bit count, instead of `SerializeInteger`.
  Byte-identical; shape. Do not resurrect a runtime `bits_required` on the write of a schema
  constant.
- Bulk `PUT_BYTES` for `[N]uint8` at a statically proved byte boundary. Byte-identical **there**;
  contract drift anywhere else.
- The word-wise interior-null scan. Verdict is law; the eight-byte idiom is shape.
- `Write` returning `bool` that is `true` for every trusted struct. C++ idiom. Go returns
  `ErrValueOutOfRange` on a write-side range in every build, because Go has no debug-only
  assert. That split is SPEC §5, not a port bug.
- An out-of-set union tag on write: C++ emits no tag bits, C emits the tag unmasked. Both are
  unspecified. Do not make either one the second language's law.

Then **prove against the oracle** (§6), in that order. A port that walks the type again in a
private twin has a second wire that agrees today and drifts tomorrow. Consume the schema the
compiler already walked.
