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

`make packet-utf8-go` checks the same corpus and mutations. `utf8.Valid`
validates the used byte slice without allocation; malformed content returns
`ErrValidation` after any stream error has been surfaced. Its compiled
negative control must fail mutation agreement. Both checks ride `test-go`.

`make packet-utf8-cs` runs Debug and Release over the same cases, with a
scalar-range validator over the existing byte array. The generated read
allocates zero bytes over 10,000 repeated valid reads. Content validation
returns `false`, following the existing schema-verdict convention; stream
failures retain the runtime's latched error. A compiled release control skips
only the UTF-8 scan and must fail mutation agreement. Both checks ride
`test-cs`.

`make packet-utf8-java` checks the same cases with assertions enabled and
disabled. The Java byte loads are unsigned before scalar validation. Since
its API returns a verdict without a cursor, accepted values must read at
their measured exact bit bound and refuse one bit less. Both positive and
negative consumers compile with Java 17 warnings treated as errors. The
fixture bytes are copied to `Text.schema` at build time because Java's outer
file class cannot share the inner `Narrow` type's name. The declaration and
wire stay identical. Both checks ride `test-java`.

`make packet-utf8-js` checks both runtime and flat codecs in development and
production. The same emitted scalar-range scan validates both tiers in place.
Runtime reads expose their consumed bits; flat reads check the exact measured
bound and refuse one bit less. Each tier has its own mutation-only failure
check after the sabotaged modules pass syntax checks and import successfully.
Both targets ride `test-js`.

`make packet-utf8-dart` runs JIT with assertions and compiled AOT, plus the
analyzer and formatter. A private helper validates used bytes in place;
accepted reads must fit the exact measured bit bound and refuse one bit
less. The AOT negative control skips only its scan and must fail mutation
agreement. Both targets ride `test-dart`; the nested defaults fixture also
passes its formatter and wire checks with the new read guard.

`make packet-utf8-elixir` validates the received binary with `String.valid?`
before the interior-NUL scan and returns `:error` on malformed content. It
uses the same corpus, mutations and exact-bit/one-bit-short checks. The
unchanged schema is staged as `Text.schema` to keep codec and struct module
names distinct. Its negative control compiles with warnings as errors before
mutation agreement fails the exact marker. Both targets ride `test-elixir`.
