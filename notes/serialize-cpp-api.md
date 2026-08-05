# classic serialize (C++) — extracted API contract

*Extracted 2026-08-04 from github.com/mas-bandwidth/serialize serialize.h v1.5.0 (SERIALIZE_VERSION
"1.5.0"), as a design input for the schema compiler's C++ backend and wire-format IR. Re-verify
against source at implementation time. License of the library: BSD 3-Clause, Más Bandwidth LLC.*

## Bitpacker

- `serialize::BitWriter` — `Initialize(void* data, int64_t bytes)` asserts `bytes % 8 == 0`.
  `WriteBits(uint32_t value, int bits)` bits in [1,32]; 64-bit scratch, values fill LSB-first;
  each full qword stored little-endian via memcpy; spill bits carry. `WriteAlign()` zero-pads to
  byte boundary. `WriteBytes(...)` requires byte alignment. `FlushBits()` mandatory after last
  write (stores a full qword, zeros high). `GetBytesWritten() = (bitsWritten+7)/8` — send exactly
  this many bytes.
- `serialize::BitReader` — `Initialize(const void* data, int64_t bytes)`; **allocation must
  extend ≥ 8 bytes past end of data** (branchless 8-byte window loads). `ReadBits(int bits)`
  [1,32]; `ReadAlign()` returns false on nonzero padding; `WouldReadPastEnd(int bits)`;
  `ReadBytes` requires byte alignment.
- Helpers: `bits_required(min,max) = (min==max) ? 0 : 32 - clz(max-min)`;
  `bits_required64` analogous; compile-time `BitsRequired<min,max>::result`; zigzag helpers exist
  but are NOT used by any macro.

## Streams

- `WriteStream` (`IsWriting=1`): `SerializeInteger(value,min,max)`, `SerializeInteger64`,
  `SerializeBits(value,bits)` [1,32], `SerializeBytes` (aligns first), `SerializeAlign`,
  `Flush()`, `GetData()`, `GetBytesProcessed()`. Write path always returns true; misuse is
  debug-assert only.
- `ReadStream` (`IsReading=1`): same methods by reference; returns false on: read past end,
  decoded raw > max-min, nonzero align padding, negative byte count. No error latch/abort state;
  false propagates via macros; stream left mid-position.
- `MeasureStream` (`IsWriting=1`): counts bits only; `GetAlignBits()` always 7, so measures are
  **conservative**, not exact.
- `BaseStream`: `SetContext(void*)`/`GetContext()`, `SetAllocator(...)` — opaque, never serialized.

## Macro family (inside `template <typename Stream> bool Fn(Stream&)`; failure → `return false`)

- `serialize_int(stream, value, min, max)` — int32 range; read re-checks [min,max].
- `serialize_int64(stream, value, min, max)`.
- `serialize_bits(stream, value, bits)` — [1,64]; >32 = low 32 first, then high (bits−32).
- `serialize_bool` — 1 bit. `serialize_uint8/16/32/64` — serialize_bits at 8/16/32/64.
- `serialize_float` — raw 32 IEEE bits. `serialize_double` — raw 64 (low dword first).
- `serialize_compressed_float(stream, value, min, max, res)`:
  `delta = max-min`; `values = delta/res` clamped to [1.0f, 4294967040.0f];
  `maxIntegerValue = ceil(values)`; `bits = bits_required(0, maxIntegerValue)`.
  Write: `normalized = clamp((value-min)/delta, 0, 1)` (NaN→0);
  `integer = floor(normalized * maxIntegerValue + 0.5f)`.
  Read: reject `integer > maxIntegerValue`; `value = integer/maxIntegerValue * delta + min`.
- `serialize_bytes(stream, data, bytes)` — align, then raw bytes.
- `serialize_string(stream, s, buffer_size)` — length as `serialize_int(len, 0, buffer_size-1)`
  (**bit width depends on buffer_size — both peers must agree**), then serialize_bytes (aligns).
  No terminator on wire; read appends `'\0'`.
- `serialize_wstring` — length prefix, then 32 bits per character, **unaligned**; read fails on
  code point above platform wchar_t max.
- `serialize_align` — zero pad to byte; read verifies zeros.
- `serialize_object(stream, object)` — `object.Serialize(stream)`.
- `serialize_int_relative(stream, previous, current)` — requires previous < current;
  `diff = uint32(current) - uint32(previous)`. Wire = chain of 1-bit flags, first true wins:
  `diff==1` → done; `diff<=6` → int(2,6); `<=23` → int(7,23); `<=280` → int(24,280);
  `<=4377` → int(281,4377); `<=69914` → int(4378,69914); else six zero flags + raw 32-bit
  absolute `current` (read fails if absolute <= previous).
- One-direction families `read_*` / `write_*` exist with identical wire format.

## Wire-format invariants (a reimplementation must reproduce)

1. Bit order LSB-first; successive values OR'd in at increasing offsets; no per-value reversal.
2. Scratch flushed as 64-bit little-endian words; spill carries. Flat model: **bit i of the
   stream lives in byte i/8 at bit position i%8.**
3. Final flush writes a full 8-byte word (high zeros); logical length = ceil(bits/8) bytes.
4. All >32-bit quantities: low 32 bits FIRST, then high remainder (bits64, uint64, double,
   Integer64 when width > 32).
5. Ranged ints encode `value − min` unsigned in exactly `bits_required(min,max)` bits
   (`min==max` → 0 bits); streams require min < max; **no zigzag on the wire**.
6. Alignment pads with ZERO bits to the byte boundary; reader must verify zeros.
7. Byte arrays: raw memory-order bytes at byte-aligned position.
8. Strings: ranged-int length [0, buffer_size−1], align, raw bytes, no terminator.
   Wide strings: length, then 32 bits/char, unaligned.
9. Floats/doubles: raw IEEE-754 patterns; compressed floats use the exact formulas above
   (the ceil, the +0.5f rounding, the 4294967040.0f clamp, decode divides by maxIntegerValue).
10. int_relative: the exact flag-chain bucket scheme above (1/6/23/280/4377/69914/absolute-32).
11. The wire is identical little-endian layout on every platform; endianness handling is a
    compile-time host concern only.

## Buffer conventions

- Write buffer: multiple of 8 bytes (asserted); bytes past GetBytesWritten() up to the word
  boundary are written as zeros; send exactly GetBytesWritten().
- Read allocation: ≥ 8 bytes slack past packet data, required.
- No pointer-alignment requirements (all access via memcpy). Counters are int64_t.
