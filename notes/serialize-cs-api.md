# serialize.cs — extracted API contract

*Extracted 2026-08-05 from github.com/mas-bandwidth/serialize.cs (private; ported 2026-08-05,
32/32 tests, byte-identical interop vs C++ both directions), as a design input for the schema
compiler's C# backend. Re-verify against source at implementation time. License: AGPL-3.0
(temporary, owner's word).*

## Package

Namespace `Serialize`, single file `src/Serialize.cs` (family style). Multi-targets
**net8.0 and net10.0**; zero package references; `TreatWarningsAsErrors`. No test framework —
console test runner (exit code = verdict), per the family zero-deps rule.

## Bitpacker

```csharp
public sealed class BitWriter {
    BitWriter(byte[] buffer)              // buffer length must be multiple of 8
    void Reset(byte[] buffer)
    void WriteBits(uint value, int bits)  // bits [1,32]
    void WriteAlign()
    void WriteBytes(ReadOnlySpan<byte> data)  // byte-aligned first
    void FlushBits()                      // mandatory; ends the write
    int AlignBits { get; }  long BitsWritten { get; }  long BitsAvailable { get; }
    long BytesWritten { get; }            // (bits+7)/8
    ReadOnlySpan<byte> Data { get; }      // buffer[..BytesWritten]
}
public sealed class BitReader {
    BitReader(byte[] buffer, int bytes)   // full buffer + packet length
    BitReader(byte[] buffer)
    void Reset(byte[] buffer, int bytes)
    bool WouldReadPastEnd(int bits)
    uint ReadBits(int bits)               // bits [1,32]
    bool ReadAlign()                      // false on nonzero padding
    void ReadBytes(Span<byte> data)       // byte-aligned first
    int AlignBits { get; }  long BitsRead { get; }  long BitsRemaining { get; }
}
```

Helpers in `public static class SerializeUtil`: `BitsRequired(uint min, uint max)`,
`BitsRequired64(ulong, ulong)`, `SignedToUnsigned`/`UnsignedToSigned` (zigzag, not on the
wire), and the loop guards as extension methods on the stream interface:
`Continue(this IBitStream, ref bool more)`, `Until(this IBitStream, ref bool done)`.

## Streams

`public interface IBitStream` implemented by sealed `WriteStream`, `ReadStream`,
`MeasureStream` (`IsWriting` true for measure, per family). All serialize methods take
**`ref` parameters and return `bool`** (false = failure, C++-style early-out) — **and the
error also latches**: `SerializeError Error { get; }` (enum: None/Overflow/ValueOutOfRange/
Align/InvalidString...) is sticky Go-style, so generated code early-outs on bool and callers
can latch-check. `object? Context { get; set; }` mirrors the family context slot.

```csharp
bool SerializeBits(ref uint value, int bits)        // [1,32]
bool SerializeBits64(ref ulong value, int bits)     // [1,64]; >32 low dword first
bool SerializeInt(ref int value, int min, int max)  // read guarantees [min,max]
bool SerializeInt64(ref long value, long min, long max)
bool SerializeByte(ref byte) / SerializeUInt16 / SerializeUInt32 / SerializeUInt64
bool SerializeBool(ref bool value)
bool SerializeFloat(ref float value) / SerializeDouble(ref double value)
bool SerializeCompressedFloat(ref float value, float min, float max, float resolution)
bool SerializeBytes(Span<byte> data)                // aligns; length NOT sent
bool SerializeString(ref string value, int bufferSize)
bool SerializeWideString(ref string value, int bufferSize)
bool SerializeAlign()
bool SerializeObject(ISerializer obj) / SerializeObject<T>(ref T) where T : ISerializer
bool SerializeIntRelative(int previous, ref int current)
```

`WriteStream`: `Flush()` mandatory before `Data`. `ReadStream(byte[] buffer, int bytes)`.
`public interface ISerializer { bool Serialize(IBitStream stream); }`

## Errors and trust

- Read path **never throws on hostile packet data** — returns false + latched error
  (verified by a 2000-seed hostile-read no-throw suite and bit-flip sweeps).
- API misuse **throws in all build modes** (ArgumentException class — bits out of range,
  min >= max, buffer not multiple of 8): the Go-panic/Rust-assert analog. Ranges are trusted
  inputs; generated code must never feed attacker-influenced values as min/max.
- The DoS rule is inherited: any value controlling iteration is checked before use —
  `Continue`/`Until` fold the check into the loop condition.

## Wire / buffers

- Same wire as the family: 64-bit scratch, LSB-first, little-endian qword flushes, low dword
  first for >32-bit, strings = ranged length + align + raw bytes, int_relative 6-bucket chain.
- Golden wire test carries the family's 72 bytes verbatim (byte-equal against the pins in
  serialize.h, serialize_test.go, serialize.rs — mechanically verified at port time).
- Write buffers: multiple of 8 bytes. Reader takes (buffer, bytes); no slack required for
  correctness (guarded window assembly at the end; perf note on file).
- **Normative wire is strict IEEE for compressed floats** — found at port time: default-flags
  C++ on ARM64 FMA-contracts the write and diverges by one quantization step at boundary
  values. The interop harness mandates `-ffp-contract=off` and carries an FMA-boundary value
  (`0.005f` in `[0,10]` res `0.01`) so a contracted build fails the gate loudly. C# evaluates
  strictly by default, matching Rust.
- Hot methods carry `[MethodImpl(MethodImplOptions.AggressiveInlining)]` (the serialize.rs
  2-8x inlining cliff, pre-empted).
