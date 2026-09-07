# Packet value defaults

This isolated fixture covers string, bytes and flags defaults without adding them
to the shared packet examples before every port supports them.

`Defaults.schema` declares the defaults. `Plain.schema` has the same shapes and no
specified defaults. The C++ checker constructs both at the same explicit values
and requires identical packet bytes and bit counts. It pins eight cases under
`testdata/wire/packet-defaults`. Later ports consume these same files.

Defaults initialize construction and never elide packet fields. Every union
selection constructs a fresh payload with its declared defaults, including when
the tag repeats; decoding then overlays the received fields. A direct non-union
read overlays only the received prefix and preserves unused tails. An untaken
conditional clears its fields to zero, including inside a freshly selected arm.
Counted arrays initialize all backing elements and start at the declared minimum
count. The `ZeroCount` case starts with count zero while every backing element
has its declared defaults; its two count bits are the complete wire (`00`).

Run the C++ oracle with `make packet-defaults-cpp`. Regenerate its pins explicitly
with `make packet-defaults-goldens`, then review both the bytes and consumed bits.
The repository's `make update-goldens` includes this corpus too. The normal test
targets only compare checked-in pins.

`make packet-defaults-go` and `make packet-defaults-c` check constructors, every C++ pin and read bit count,
and reads into reused storage. Short and empty union payloads distinguish fresh
default tails from both zero and retained poison, including on repeated tags.
The constructor probes also cover explicit empty defaults, short byte literals
with zero backing tails, and the unsigned bit-63 and full 64-bit flags masks.
`make packet-defaults-go-negative-control` and `make packet-defaults-c-negative-control`
use named edits in `tools/sabotage` to remove only the constructor byte copy
through an overlay. Each generated consumer must compile and fail the exact
constructor-bytes marker. C's zero fill, lengths and flags stay in place; the
byte copy alone is removed. Table-closure defaults are refused in C and Go.

The C fixture also reads and writes independently calculated 33-bit and 64-bit
flags cases. The 33-bit mask sits between three leading and two trailing bits:
`5 | ((1 << 32) << 3) | (2 << 36)` gives `05 00 00 00 28`, exactly 38 bits.
C string reads write a terminator after the received prefix; all other unused
tail bytes keep the ordinary reused-storage rule. Comparisons inspect fields
and the active union payload, never struct padding or inactive union storage.

Fast PR CI does not invoke these packet runtime checks or controls. Full
`certify.yml` invokes `make test`, whose `TEST_LEGS` loop calls `test-c` and
`test-go`; those targets depend on the corresponding positive and negative
checks here. The C++ oracle is also a direct dependency of `test`.

`make packet-defaults-rust` checks the same eight pins in debug and release,
including every constructor, explicit-empty and short literals, 33-bit and
64-bit masks, a defaultless twin, backing arrays and repeated union selection.
Rust's `Default` stays the zero form; `new()` applies declared defaults.
`make packet-defaults-rust-negative-control` removes only constructor byte
copies through an overlay and requires the compiled consumer to fail the
constructor-bytes marker. Both targets are dependencies of `test-rust`, and
table-closure defaults remain refused in Rust.

`make packet-defaults-cs` runs the eight C++ pins in Debug and Release. It
checks construction, allocation-free in-place `Init`, the distinct `Zero` form,
backing-array defaults, short and empty literals, unsigned 33/64-bit masks, a
defaultless twin, reused prefixes and repeated union selection. The reader
retains arm objects and buffers. `make packet-defaults-cs-negative-control`
removes the literal byte assignments through an overlay and requires the built
consumer to fail the constructor-bytes marker. Both checks ride `test-cs`;
table-closure defaults remain refused.

`make packet-defaults-java` compiles with Java 17 warnings treated as errors,
and runs with assertions enabled and disabled. It checks the eight C++ byte
and bit pins, constructors and in-place resets, backing arrays, independent
33/64-bit masks, a defaultless twin and both kinds of reused storage. Since
the reader API returns a verdict rather than a cursor, every pin must accept
its exact bit bound and refuse one bit less on a separate object. The named
constructor-byte control builds before failing its marker, and both checks
ride `test-java`. Table-closure defaults stay refused.
