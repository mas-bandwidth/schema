# Packet text conformance

`Narrow.schema` reproduces the shared serialize corpus's `buffer_size = 16`
with a `string(15)` field. The Go harness reads the existing
`testdata/conformance/text/string.txt` directly. It checks every applicable
row against the generated C++ reader, including the thirteen UTF-8 refusals
and eight neighboring accepts, framing, interior NUL and truncation cases.
Bounds other than 16 are outside this fixture. There are 33 applicable rows.

Each accepted row also has a pinned payload, consumed bits and canonical
writer bytes. The harness flips every bit of every accepted packet, compares
each port with C++, and checks the decoded value and re-encoded wire of every
accepted mutation. The current corpus yields 496 bit flips, 192 refused.
Drivers run once per sweep, taking one hexadecimal wire per input line and
returning one result per line; no text decoder substitutes replacement bytes.

`make packet-utf8-c` runs debug and optimized NDEBUG readers. C reads validate
used bytes in place before the interior-null scan. Its destination is poisoned
so the terminator check requires a read-side store. The named overlay control
removes only UTF-8 read validation, compiles the consumer and requires the
mutation-only sweep to fail its exact verdict marker. The corpus's refusal
rows alone cannot satisfy that negative control. Both targets ride `test-c`.

Normal checks never regenerate the shared corpus or packet wire goldens.

`make packet-utf8-rust` runs debug and release against the same cases.
`std::str::from_utf8` borrows the used slice, validates strictly and allocates
nothing. A malformed payload returns `Error::Validation` in both modes;
the former writer-only debug assertion is removed. The compiled release
negative control must fail mutation agreement, and both checks ride
`test-rust`.
