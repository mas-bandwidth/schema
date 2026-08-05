# serialize.rs — extracted API contract

*Extracted 2026-08-04 from github.com/mas-bandwidth/serialize.rs, as a design input for the schema
compiler's Rust backend. Re-verify against source at implementation time. License of the library:
MBSL (BSD 3-Clause + credit clause).*

## Crate

Crate `serialize`; Rust port of C++ serialize, bit-for-bit wire compatible with C++ and Go.
Zero deps, `#![forbid(unsafe_code)]`, MSRV 1.85. **No macros** — the C++ `serialize_*` macros
become trait methods plus `?` propagation. `S::IS_WRITING` / `S::IS_READING` are associated
consts, so direction branches resolve at monomorphization (the C++ template analog).

```rust
pub trait Serialize {
    fn serialize<S: Stream>(&mut self, stream: &mut S) -> Result;
}
```

Write: `WriteStream::new(&mut buffer)` → serialize → `stream.flush()` → `bytes_processed()`.
Read: `ReadStream::new(&buffer, bytes_written)` → serialize.

## Bitpacker

`BitWriter<'a>`: `new(data: &'a mut [u8])` (panics unless len % 8 == 0); `write_bits(value: u32,
bits: u32)` [1,32]; `write_align()`; `write_bytes(&[u8])` (byte-aligned first); `flush_bits()`
required; `bytes_written() -> u64`; `data() -> &[u8]`.

`BitReader<'a>` (branchless; `Clone` = position snapshot for speculative reads): `new(buffer:
&'a [u8], bytes: usize)`; `read_bits(bits: u32) -> u32`; `read_bits_group<const N>(&[u32; N])`
batched; `read_align() -> bool`; `read_bytes(&mut [u8])`; `read_byte_slice(bytes) -> &'a [u8]`
zero-copy; `would_read_past_end(bits) -> bool`.

Const helpers: `bits_required(min, max) -> u32` (0 if min==max), `bits_required64`,
zigzag pair (not used on the wire by streams).

## Stream trait

```rust
pub trait Stream {
    const IS_WRITING: bool;   // true for WriteStream AND MeasureStream
    const IS_READING: bool;
    fn serialize_bits(&mut self, value: &mut u32, bits: u32) -> Result;
    fn serialize_bytes(&mut self, data: &mut [u8]) -> Result;      // aligns first
    fn serialize_align(&mut self) -> Result;
    fn serialize_string(&mut self, value: &mut String, buffer_size: usize) -> Result;
    fn serialize_wide_string(&mut self, value: &mut String, buffer_size: usize) -> Result;
    fn align_bits(&self) -> u32;              // MeasureStream: always 7 (conservative)
    fn bits_processed(&self) -> u64;
    fn bytes_processed(&self) -> u64;
    fn context(&self) -> Option<&dyn Any>;
    // provided:
    fn serialize_int(&mut self, value: &mut i32, min: i32, max: i32) -> Result;
    fn serialize_int64(&mut self, value: &mut i64, min: i64, max: i64) -> Result;
    fn serialize_bits64(&mut self, value: &mut u64, bits: u32) -> Result;  // low dword first
    fn serialize_bool(&mut self, value: &mut bool) -> Result;
    fn serialize_u8/u16/u32/u64(...) -> Result;
    fn serialize_f32(&mut self, value: &mut f32) -> Result;
    fn serialize_f64(&mut self, value: &mut f64) -> Result;
    fn serialize_compressed_float(&mut self, value: &mut f32, min: f32, max: f32, resolution: f32) -> Result;
    fn serialize_int_relative(&mut self, previous: i32, current: &mut i32) -> Result;
}
```

Concrete: `WriteStream<'a>` (`new` panics unless len % 8 == 0; `flush()` required; `data()`),
`ReadStream<'a>` (`new(buffer, bytes)`; `Clone` = snapshot; failed reads leave destination
unmodified), `MeasureStream<'a>` (counts bits; every align counts 7 — conservative;
IS_WRITING = true so it shares the write branch).

## Errors

`pub type Result<T = ()> = core::result::Result<T, Error>;`
`#[non_exhaustive] pub enum Error { Overflow, ValueOutOfRange, Align, InvalidString }`.
Read path never panics on malicious data. Panics = API misuse only (bits out of range,
min >= max, resolution <= 0, write buffer not multiple of 8, reader bytes > buffer.len()).
`InvalidString`: non-UTF-8 bytes / invalid wide code point (Rust `String` validates; only valid
UTF-8 interoperates with C++ raw-byte strings).

## Buffers / wire

- 64-bit scratch stored little-endian; bits right-to-left; wire identical across platforms.
- Write buffers multiple of 8 (panic). Read: ≥8 bytes slack past packet data keeps the
  branchless fast path; correct without, slower. Sizes u64 internally.
- int_relative buckets: 1-bit fast path, then [2,6], [7,23], [24,280], [281,4377],
  [4378,69914], else full 32-bit absolute — matches C++ exactly.
- CI: golden bytes from the C++ suite; head-to-head C++ build with byte-identity and
  cross-decode; differential + hostile-read fuzzing; Miri; big-endian s390x under qemu.
