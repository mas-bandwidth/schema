# serialize.go — extracted API contract

*Extracted 2026-08-04 from github.com/mas-bandwidth/serialize.go, as a design input for the schema
compiler's Go backend. Re-verify against source at implementation time. License of the library:
MBSL (BSD 3-Clause + credit clause).*

## Package

Package `serialize`, import `github.com/mas-bandwidth/serialize.go`. Pure Go port of C++
serialize; **bit-for-bit identical wire output**, pinned by a golden wire-format test with bytes
copied from the C++ test suite. Zero allocations on serialization paths.

## Bitpacker (panics on misuse; no errors)

```go
func NewBitWriter(buffer []byte) *BitWriter          // write buffer len must be multiple of 8
func (w *BitWriter) WriteBits(value uint32, bits int) // bits [1,32]
func (w *BitWriter) WriteAlign()
func (w *BitWriter) WriteBytes(data []byte)           // byte-aligned first
func (w *BitWriter) FlushBits()                       // mandatory; ends the write
func (w *BitWriter) BytesWritten() int64
func (w *BitWriter) Data() []byte

func NewBitReader(data []byte) *BitReader
func (r *BitReader) ReadBits(bits int) uint32
func (r *BitReader) WouldReadPastEnd(bits int) bool
func (r *BitReader) ReadAlign() bool                  // false on nonzero padding
func (r *BitReader) ReadBytes(data []byte)
```

Helpers: `BitsRequired(min, max uint32) int` (0 if min==max), `BitsRequired64`,
`SignedToUnsigned`/`UnsignedToSigned` (zigzag; not used on the wire by streams).

## Streams

`Stream` interface implemented by `WriteStream`, `ReadStream`, `MeasureStream` (measure exists;
`IsWriting()` true for it). Concrete types usable directly to skip interface dispatch (6–8%).

```go
func NewWriteStream(buffer []byte) *WriteStream   // len multiple of 8
func (s *WriteStream) Flush()                     // mandatory before Data()
func (s *WriteStream) Data() []byte
func NewReadStream(data []byte) *ReadStream
func NewMeasureStream() *MeasureStream
```

Interface methods (identical on all three):

```go
IsWriting() bool / IsReading() bool
SerializeBits(value *uint32, bits int) error      // [1,32]
SerializeBits64(value *uint64, bits int) error    // [1,64]; >32: low dword first
SerializeInt(value *int32, min, max int32) error  // read guarantees [min,max]
SerializeInt64(value *int64, min, max int64) error
SerializeUint8/16/32/64(value *...) error
SerializeBool(value *bool) error
SerializeFloat32(value *float32) error
SerializeFloat64(value *float64) error
SerializeCompressedFloat32(value *float32, min, max, resolution float32) error
SerializeBytes(data []byte) error                 // aligns; length NOT sent
SerializeString(value *string, bufferSize int) error
SerializeWideString(value *string, bufferSize int) error
SerializeAlign() error
SerializeObject(object Serializer) error
SerializeIntRelative(previous int32, current *int32) error
AlignBits() int                                   // MeasureStream: always 7 (conservative)
BitsProcessed() int64 / BytesProcessed() int64
Err() error                                       // first latched error or nil
SetContext(context any) / Context() any
```

`type Serializer interface { Serialize(stream Stream) error }`

Loop helpers (fold stream error into the loop condition): `Continue(stream, more *bool) bool`,
`Until(stream, done *bool) bool`.

## Errors

- Stream errors are **sticky**: first failure latches; later calls no-op returning the same
  error, leaving values unmodified. Pattern: serialize fields, `return stream.Err()`.
- Sentinels: `ErrOverflow`, `ErrValueOutOfRange`, `ErrAlign`.
- API misuse panics (bits out of range, min >= max, buffer not multiple of 8, unaligned byte
  ops, raw bitpacker overflow) — the C++ debug-assert analog.
- **Untrusted-data rule: any value controlling loop iteration must have its error checked
  before use** (a truncated packet otherwise spins — DoS vector). Generated code must check
  counts before looping.

## Buffers / wire

- 64-bit scratch flushed as little-endian qwords; bits fill right-to-left; identical output to
  C++ serialize. Bit counts int64.
- Write buffers: multiple of 8 bytes. Read: any size; ≥7 bytes of slack in the backing array
  (via cap) keeps reads fully branchless — correct without it, slower.
- `SerializeString`: length in [0, bufferSize−1] (width from bufferSize), align, raw bytes.
