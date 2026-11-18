# serialize.rb: the design

This page says what serialize.rb promises and why it is built the way it is.
Every rule below names the code that keeps it and the test that holds it, and
every `ruby` block on this page runs, under `ruby -w`, against `lib/` in
`make docs-check`: a block that raises or warns turns it red.

**The sources.** The wire is [STANDARD.md](STANDARD.md), format version 2.0.
The uniform behaviour rules U1 to U16 are [IMPLEMENTING.md](IMPLEMENTING.md)'s.
Both, the corpus in [conformance/](conformance/) and its negative controls in
[conformance-controls/](conformance-controls/) are vendored verbatim from
mas-bandwidth/serialize `v2.1.1` (commit `bd3d67e23c8a`), each file held to its
sha256 in [VENDORED](VENDORED) (`test/test_vendored.rb`,
`test_the_vendored_files_are_the_pin`).

**The oracle.** The corpus. `test/test_conformance.rb`, `test_corpus`, runs every
vector of `conformance/*.txt` and requires its bytes, its value and its stated
refusal reason; `test_controls` runs each sabotage control (a copy of
`lib/serialize/core.rb` with one refusal rule deleted) and requires it to fail
exactly the vectors its `.expected` file lists, so a corpus runner that checks
nothing cannot pass.

**The interpreter.** CRuby 3.4 and newer, held at two pins: the floor, CRuby
3.4.12, the oldest series in normal maintenance, and the current, CRuby 4.0.9
(`make test-floor`, `make test-current`). The library is one file,
`lib/serialize/core.rb`, entered by `require 'serialize'`; it uses only the core
classes (Integer, Float, String, Array) and requires nothing, not even the
standard library. Every file is silent under `ruby -w`: `test/run.rb` fails a run
in which Ruby printed a warning.

## The API

One serialize function per message, written once against `Serialize::Stream`,
reads, writes and measures. A scalar travels in a `Serialize::Ref`: on write and
measure the call takes `ref.value`; on read it stores the value there on
success and leaves it untouched on refusal. Every serialize call returns `true`
or `false`, so a message is a chain of `&&`.

```ruby
require 'serialize'

def serialize_player(s, id, health, name, position)
  s.serialize_bits(id, 16) &&
    s.serialize_int(health, 0, 100) &&
    s.serialize_string(name, 32) &&
    s.serialize_object do |st|
      st.serialize_compressed_float(position, -100.0, 100.0, 0.01)
    end
end

ref = Serialize::Ref
m = Serialize::MeasureStream.new
serialize_player(m, ref.new(7), ref.new(87), ref.new('golden'), ref.new(12.5)) or raise
w = Serialize::WriteStream.new(64)
serialize_player(w, ref.new(7), ref.new(87), ref.new('golden'), ref.new(12.5)) or raise
w.serialize_end
w.flush
raise 'the measure is a bound' unless m.bits_processed >= w.bits_processed

r = Serialize::ReadStream.new(w.data)
id, health, name, position = Array.new(4) { ref.new }
serialize_player(r, id, health, name, position) && r.serialize_end or raise r.refusal.to_s
raise unless [id.value, health.value, name.value] == [7, 87, 'golden']
raise unless (position.value - 12.5).abs <= 0.01
```

### The entry: `require 'serialize'`

| name | what |
|---|---|
| `Serialize::FORMAT_VERSION` | `'2.0'`, the wire's format version |
| `Serialize::Ref.new(value = nil)` | the holder; `#value`, `#value=` |
| `Serialize::WriteStream.new(capacity)` | a writer into `capacity` bytes, a non-negative multiple of 8 (U1); anything else raises `WriteMisuse` (`test_u1_write_buffer_is_a_multiple_of_8_bytes`) |
| `Serialize::ReadStream.new(data)` | a reader over a String's bytes, which it copies; data that is not a String latches `:value_out_of_range` at once (U7, `test_u7_a_read_stream_over_no_bytes_refuses`) |
| `Serialize::MeasureStream.new` | a measure: the bits a message costs from any start position (U6) |
| `Serialize::Stream` | the class the three streams share |
| `Serialize::Refusal::PAST_END` ... `REJECTED` | the nine refusal reasons, Symbols named as STANDARD.md spells them; the same nine are also constants of `Serialize` itself |
| `Serialize::REASONS` | the nine, frozen, in STANDARD.md's order |
| `Serialize::WriteMisuse` | the write and measure trap, an `ArgumentError` (`test_write_misuse_is_an_argument_error`) |
| `Serialize.refusal_name(reason)` | the family's spelling (U14): `'none'` for `nil`, the reason's name for one of the nine, `''` for anything else (`test_u14_refusal_names`) |
| `Serialize.copy_string(source, dest_size)` | text bounded for `serialize_string(ref, dest_size)` (U15): the bytes before the first NUL, cut to `dest_size - 1` and backed off to a whole character (`test_u15_copy_string`) |
| `Serialize.bits_required(min, max)` | the bits a range costs, 0 when `min == max`, at every width (`test_bits_required`) |
| `Serialize.compressed_float_params(min, max, res)` | `[max_integer_value, bits, delta]` for `serialize_compressed_float_precomputed`, or `nil` for a declaration with no wire meaning (`test_precomputed_compressed_float_is_wire_identical`) |
| `Serialize.float32_round(x)`, `float32_bits(x)`, `float32_from_bits(bits)` | the float32 rounding and bit casts Ruby lacks (a language necessity, U15; below) |
| `Serialize.float64_bits(x)`, `float64_from_bits(bits)` | the float64 bit casts |
| `Serialize.float_bits_finite?(bits)`, `double_bits_finite?(bits)` | whether a pattern's exponent field is not all ones |

### The stream methods

W is WriteStream, R ReadStream, M MeasureStream.

| method | streams | what |
|---|---|---|
| `writing?`, `reading?` | W R M | W and M are writing, R is reading |
| `serialize_bits(ref, bits)` | W R M | `bits` in [1, 64]; the value an Integer in [0, 2^bits) |
| `serialize_bool(ref)` | W R M | `true` or `false`, one bit |
| `serialize_uint8(ref)` ... `serialize_uint64(ref)` | W R M | `serialize_bits` at 8, 16, 32 and 64 bits |
| `serialize_int(ref, min, max)` | W R M | `min` and `max` within int32 (U5, `test_u5_int_bounds_are_int32`) |
| `serialize_int64(ref, min, max)`, `serialize_int128(ref, min, max)` | W R M | the same within int64 and int128 |
| `serialize_uint128(ref)` | W R M | an Integer in [0, 2^128) |
| `serialize_fixed(ref, integer_bits, fraction_bits, min, max)` | W R M | the raw Q value (the number times 2^fraction_bits) of a fixed point field whose storage `integer_bits + fraction_bits` is 8, 16, 32, 64 or 128; `min` and `max` in whole units |
| `serialize_int_relative(previous, ref)` | W R M | a value above `previous`, both in [0, 2^31) |
| `serialize_float(ref)`, `serialize_double(ref)` | W R M | a Float (an Integer is taken at its Float value on write) |
| `serialize_compressed_float(ref, min, max, res)` | W R M | a Float quantized over [min, max] at `res` |
| `serialize_compressed_float_precomputed(ref, max_integer_value, bits, delta, min)` | W R M | the same with the constants `compressed_float_params` derives, wire identical |
| `serialize_bytes(ref, count)` | W R M | exactly `count` bytes; the count is not sent |
| `serialize_string(ref, buffer_size)` | W R M | UTF-8 text of at most `buffer_size - 1` bytes |
| `serialize_align` | W R M | pads to a byte boundary |
| `serialize_end` | W R M | R: refuses a whole byte left over, then set padding bits; W and M: `true` (U12) |
| `serialize_object { \|s\| ... }` | W R M | succeeds when the block returns `true` (and, on R, nothing latched) |
| `reject` | W R M | R: latches `:rejected`, returns `false` (U2); W and M: raises `WriteMisuse` |
| `fail(reason = nil)` | W R M | R: latches a listed reason as given and anything else as `:rejected`; W and M: returns `false`, records nothing (U14) |
| `bits_processed`, `bytes_processed`, `align_bits` | W R M | M's `align_bits` is always 7 |
| `flush`, `data`, `bits_available` | W | `flush` before `data` (U11); `data` is `bytes_processed` bytes, a binary String |
| `refusal`, `bits_remaining` | R | the first refusal's reason, or `nil` while healthy |

No stream is re-initialized (U13 makes it optional): make a new stream for new
bytes. A stream has no context slot (U15 allows one; none is carried).

**The public surface** is the two tables above. Everything else `lib/` defines
(`Serialize::Encode`, the declaration checks such as `Serialize.number?`, and
the numeric constants such as `INT32_MIN` and `TIERS`) is internal, promised
nothing, and may change in any release; making it private is on the
[roadmap](ROADMAP.md) (IMPLEMENTING.md U15).

## The accumulator, and the bit order

The wire is STANDARD.md's: values are packed least significant bit first into
64-bit words, and every word is little-endian, so the first value written sits
in the lowest bits of the first byte (`test_the_wire_is_little_endian_lsb_first`).

```ruby
require 'serialize'

w = Serialize::WriteStream.new(16)
w.serialize_bits(Serialize::Ref.new(1), 1)
w.serialize_bits(Serialize::Ref.new(0x100), 9)   # bit 9 of the stream
w.serialize_bits(Serialize::Ref.new((1 << 60) - 1), 60)  # crosses the first word
w.flush
raise unless w.bits_processed == 70 && w.bytes_processed == 9
raise unless w.data.bytesize == 9 && w.data.encoding == Encoding::BINARY
raise unless w.data.byteslice(0, 2) == "\x01\xFE".b
```

**The writer** (`WriteStream#put`) keeps an Integer accumulator and the count of
bits it holds. A value is ORed in above the bits held; while 64 or more are
held, the low 64 are appended to a binary String as one little-endian word
(`pack('Q<', buffer:)`) and shifted out. `data` appends the held bits' bytes
to a copy of that String, so `flush` moves nothing; it stays the family's
completion point (U11), and a caller flushes before `data`. Why an Integer:
Ruby's Integer has no fixed width, so a value that crosses a word boundary is
one OR and one shift, with none of the split a fixed 64-bit scratch word needs,
and a 128-bit value is one call. Why the String: the words go straight into
the bytes `data` returns, and a byte block (`serialize_bytes`,
`serialize_string`) is appended whole once the held bits are written out
(`put_aligned_bytes`), never bit by bit. The room check runs before any of
this, for the whole operation (U10,
`test_u10_room_is_checked_for_the_whole_operation`).

The 64-bit word was taken from STANDARD.md's scratch word, not measured, and
the measurement says it is not the fastest. `make bench-choices`
(`test/bench_choices.rb`) packs the same fields through each candidate's inner
loop, compares their bytes, then times them, best of three over 200,000 fields,
in nanoseconds per field, CRuby 4.0.9 on an Apple silicon (arm64) Mac:

| writer inner loop | bits 5, YJIT off | bits 5, YJIT on | bits 32, YJIT off | bits 32, YJIT on |
|---|---:|---:|---:|---:|
| whole 64-bit words (the writer today) | 56 | 34 | 133 | 89 |
| emptied every 32 bits | 50 | 27 | 98 | 59 |
| one byte at a time | 65 | 26 | 185 | 50 |

Emptying every 32 bits is faster than the writer's 64-bit words in all four
columns, with the same bytes; one byte at a time is faster still under YJIT
but the slowest without it. The change to 32 bits is on the
[roadmap](ROADMAP.md).

**The reader** (`ReadStream#take`) holds its bytes as an Array of
little-endian 32-bit words, unpacked once at construction, each an immediate
Integer (CRuby keeps an Integer below 2^62 in the reference itself, with no
allocation). A read of `n` bits shifts the word at the read position and ORs in
the following words until it holds `n`. The words are padded with zero bytes
to a whole word, and those bytes are never read: every read compares its bits
with the stream's length first, so the reader never looks past the caller's
bytes (the buffer contract: the caller owes no slack). The choice was measured
against the two others a Ruby reader can make:

| reader inner loop | bits 5, YJIT off | bits 5, YJIT on | bits 32, YJIT off | bits 32, YJIT on |
|---|---:|---:|---:|---:|
| 32-bit words, immediates (the reader) | 89 | 28 | 95 | 42 |
| 64-bit words, heap Integers above 2^62 | 134 | 58 | 161 | 84 |
| `unpack1('Q<', offset:)` at each read | 179 | 101 | 198 | 120 |

The 32-bit words are twice as fast as 64-bit ones under YJIT (1.5 to 1.7 times
with it off), and two to 3.6 times as fast as unpacking the String at each
read.

## The refusal model

Two mechanisms, never mixed (IMPLEMENTING.md U7 and U9).

**A read refusal is a value.** A read that cannot accept its bits returns
`false`, leaves its `Ref` unwritten (`test_a_refused_read_leaves_its_destination_unwritten`)
and latches its reason, a Symbol of the closed list, which `refusal` reports.
The first reason is kept, and every later read refuses too, consumes nothing
and writes nothing, zero-bit reads included
(`test_u2_reject_latches_rejected_and_the_first_reason_wins`). A reader never
raises on any input (`test_reads_of_hostile_bytes_never_raise`), and caller
misuse on read, a bad declaration such as `min > max` or a `bits` count of 0,
refuses as `:value_out_of_range` before any bit moves
(`test_u7_u16_read_misuse_refuses_and_never_raises`).

```ruby
require 'serialize'

r = Serialize::ReadStream.new("\x0F".b)
v = Serialize::Ref.new(:untouched)
raise if r.serialize_int(v, 0, 8)            # the four bits hold 15, above 8
raise unless r.refusal == Serialize::Refusal::VALUE_OUT_OF_RANGE
raise unless v.value == :untouched
raise if r.serialize_bits(v, 1)              # terminal: every later read refuses
raise if r.reject                            # and the first reason is kept
raise unless Serialize.refusal_name(r.refusal) == 'value_out_of_range'

r = Serialize::ReadStream.new("\x01".b)
raise if r.serialize_object { |_| false }    # an object that refuses on its own
raise unless r.refusal == :rejected
```

Why a value and not an exception: the bytes a reader sees come from the
network, and STANDARD.md makes a trap on malformed input non-conforming, so a
refusal is an ordinary outcome the caller tests, not a control-flow escape. The
latch is the state STANDARD.md requires ("Failure is terminal"):
`ReadStream#refuse` keeps the first reason, which every read tests before it
does anything, and also sets the readable length to -1, so a refused stream
fails every past-end comparison as well. The reasons are Symbols because a Symbol is one immediate object that
compares by identity and prints as its own name, so the reason is its family
spelling.

**Write and measure misuse raises `Serialize::WriteMisuse`**, at the call and
before any bit of the operation is written: a value outside its range or bit
width (U8), a non-finite float, text that is not valid UTF-8, holds a NUL or
does not fit its buffer (U3, U4), a bad declaration (U16), a write that does
not fit the room left (U10), and `reject` on a writer or a measure (U9).
Misuse never latches: a write or a measure stream has no refusal state, and the
next correct write proceeds as if the misuse never happened
(`test_u9_write_misuse_never_latches`, `test_u8_over_wide_values_trap`).

```ruby
require 'serialize'

w = Serialize::WriteStream.new(8)
begin
  w.serialize_int(Serialize::Ref.new(9), 0, 7)
  raise 'not raised'
rescue Serialize::WriteMisuse => e
  raise unless e.is_a?(ArgumentError)
end
raise unless w.bits_processed.zero?         # nothing was written
raise unless w.serialize_int(Serialize::Ref.new(5), 0, 7)
w.flush
raise unless w.data == "\x05".b
raise if w.fail                             # fail on a writer records nothing
```

Why raise: the values a writer sends are the program's own, so misuse is a bug
in the caller, and IMPLEMENTING.md U9 gives every implementation one
write-misuse mechanism, the trap. A compiled member of the family can strip
its checks from a release build; Ruby has no release build, so these checks run
on every call, and `WriteMisuse` subclasses `ArgumentError` because every one of
them is an argument the call cannot carry.

**The measure** (`MeasureStream`) makes every check a write makes except room,
raises the same way, and refuses nothing (U6). It charges 7 bits for each
operation that aligns (`serialize_align`, `serialize_bytes`, `serialize_string`),
so its count bounds the bits from any start position
(`test_measure_bounds_every_start_offset`, `test_u6_measure_charges_seven_bits_per_align`).

```ruby
require 'serialize'

m = Serialize::MeasureStream.new
m.serialize_bits(Serialize::Ref.new(1), 8)
m.serialize_align
m.serialize_bits(Serialize::Ref.new(1), 8)
raise unless m.bits_processed == 23 && m.align_bits == 7
```

## float32

Ruby has one floating type, a float64. A `float` field carries the float32
nearest the value, rounded as IEEE 754 rounds (to nearest, ties to even), and a
read returns the Float whose value is exactly that float32.
`Serialize.float32_round` is that rounding: one `pack('e')`, the C conversion
to float32, which is round to nearest even, then a correction for the one case
it gets wrong: a finite value above the largest float32 and below the halfway
point to 2^128 narrows to an infinity there, where IEEE 754 rounds it to the
largest float32. The halfway point itself is a tie, and rounds to even, the
infinity (`test_float32_round_overflow_and_ties`,
`test_float32_round_at_the_largest_float32`).

```ruby
require 'serialize'

raise unless Serialize.float32_bits(1.0 + 2.0**-24) == 0x3F800000      # a tie, to even
raise unless Serialize.float32_bits(1.0 + 3 * 2.0**-24) == 0x3F800002
largest = Serialize.float32_from_bits(0x7F7FFFFF)
raise unless Serialize.float32_bits(largest.next_float) == 0x7F7FFFFF  # not an infinity
raise unless Serialize.float32_round(1e39) == Float::INFINITY

w = Serialize::WriteStream.new(8)
w.serialize_float(Serialize::Ref.new(0.1))
w.flush
r = Serialize::ReadStream.new(w.data)
got = Serialize::Ref.new
r.serialize_float(got) or raise
raise unless got.value == Serialize.float32_round(0.1) && got.value != 0.1
```

Why `pack` and a correction rather than rounding by integer arithmetic on the
bits: the conversion is one C call, it is the platform's own IEEE rounding, and
the correction runs only when the result is an infinity.

**compressed_float's arithmetic is float32** (STANDARD.md: "the roundings are
part of the format"). Every float32 step is the float64 operation followed by
`float32_round`, on the declaration (`delta`, `delta / res`), on write
(normalize, scale, add a half) and on read (normalize, scale, add `min`). That
is exact, not an approximation: for +, -, * and / of float32 operands, the
float64 result rounded to float32 is the correctly rounded float32 result,
because float64 carries more than 2 * 24 + 2 bits
(`test_compressed_float_rounds_twice_on_write` holds STANDARD.md's worked
values). `min`, `max` and `res` are rounded to float32 first, as the family's
float32 parameters are.

## Finite values only

Every float on the wire is finite (STANDARD.md, "Finite values only").

- **A write** tests the bits it would send: a `float` after the float32
  rounding, so a finite Float above the float32 range, which rounds to an
  infinity, raises `WriteMisuse` as NaN and the infinities do; a `double`
  tests its own 64 bits. A `compressed_float` value that is not finite after
  rounding, and a declaration whose top, `min + delta`, is not finite in
  float32, raise too (`test_finite_floats_only_on_write`).
- **A read** tests the raw bits before it converts anything: a `float` or
  `double` pattern whose exponent field is all ones refuses as
  `:non_finite_float`, and so does a compressed decode that is not finite.
  Negative zero and every subnormal are finite and move bit for bit
  (`test_every_operation_from_every_start_offset`).

```ruby
require 'serialize'

w = Serialize::WriteStream.new(16)
[Float::NAN, Float::INFINITY, 1e39].each do |v|
  w.serialize_float(Serialize::Ref.new(v))
  raise "#{v} was written"
rescue Serialize::WriteMisuse
  nil
end
raise unless w.serialize_double(Serialize::Ref.new(1e39))  # finite as a double

r = Serialize::ReadStream.new([0x7F800000].pack('L<'))     # float32 +infinity
raise if r.serialize_float(Serialize::Ref.new)
raise unless r.refusal == :non_finite_float
```

Why both sides test bits rather than Floats: the read side must refuse before
it makes a value, and testing the pattern (`bits & 0x7F800000 == 0x7F800000`)
needs no conversion, so a NaN's payload never becomes a Ruby object; the write
side tests what it will send, which is the rounded pattern, not the caller's
Float.

## String encodings

A Ruby String is bytes and an encoding, so the library uses the encoding to
tell text from bytes.

- **`string`** writes a String whose encoding is UTF-8 or US-ASCII (a subset of
  UTF-8 Ruby gives some literals) and whose bytes are valid UTF-8, with no NUL,
  of at most `buffer_size - 1` bytes. A binary String, a UTF-16 String, invalid
  bytes, a NUL and text too long raise `WriteMisuse`
  (`test_u3_u4_text_misuse_traps_on_write_and_measure`). The writer never
  transcodes: what the caller holds is what is sent (U3). A read returns a
  UTF-8 String, and refuses a NUL anywhere as `:embedded_nul` first, then
  ill-formed UTF-8 as `:invalid_utf8`, so the text a reader hands over is
  always valid (`test_reads_return_binary_bytes_and_utf8_text`).
- **`bytes`** writes the bytes of any String whose `bytesize` is the count, and
  a read returns a binary (ASCII-8BIT) String: bytes carry no encoding claim.
- **The reader copies its input**, once, as a frozen binary String, so a caller
  that changes its String afterwards changes nothing the reader reads
  (`test_the_reader_copies_its_bytes`). `WriteStream#data` is a binary String.

```ruby
require 'serialize'

ref = Serialize::Ref
w = Serialize::WriteStream.new(64)
w.serialize_string(ref.new('碁石'), 16) or raise
w.serialize_string(ref.new('ab'.encode('US-ASCII')), 16) or raise
w.serialize_bytes(ref.new('hi'), 2) or raise          # any String's bytes
['ab'.b, 'ab'.encode('UTF-16LE'), "a\xFFb", "a\u0000b", 'x' * 16].each do |text|
  w.serialize_string(ref.new(text), 16)
  raise "#{text.inspect} was written"
rescue Serialize::WriteMisuse
  nil
end
w.flush

r = Serialize::ReadStream.new(w.data)
a, b, c = Array.new(3) { ref.new }
r.serialize_string(a, 16) && r.serialize_string(b, 16) && r.serialize_bytes(c, 2) or raise
raise unless a.value == '碁石' && a.value.encoding == Encoding::UTF_8
raise unless c.value == 'hi'.b && c.value.encoding == Encoding::BINARY

# a length of 2 (two bits, then the align), then the payload; the NUL is found first
[["\x02a\x00", :embedded_nul], ["\x02\xFF\x00", :embedded_nul],
 ["\x02\xC3\x28", :invalid_utf8]].each do |data, reason|
  r = Serialize::ReadStream.new(data.b)
  raise if r.serialize_string(ref.new, 4)
  raise unless r.refusal == reason
end
raise unless Serialize.copy_string('碁石', 6) == '碁'   # five bytes fit; three make a character
```

Why encodings and not a separate bytes type: Ruby already marks every String
with one, and the caller already knows whether it holds text; refusing a binary
String on a `string` write keeps unchecked bytes from being sent as text, and
returning ASCII-8BIT from a `bytes` read keeps wire bytes from being taken for
text by accident.

## YJIT

The library runs with YJIT off and on, and needs nothing from it; `make bench`
(`test/bench.rb`) measures both. The README's table, CRuby 4.0.9, nanoseconds
per call:

| operation | write, YJIT off | write, YJIT on | read, YJIT off | read, YJIT on |
|---|---:|---:|---:|---:|
| bits 5 | 224 | 68 | 188 | 107 |
| int [-1000, 1000] | 321 | 117 | 294 | 69 |
| float | 441 | 221 | 251 | 128 |
| bytes 16 | 281 | 70 | 225 | 213 |

YJIT makes every row of the README's table 1.4 to 4.3 times faster but the
`bytes 16` read, which is one String slice either way. The code is shaped for
it: each stream method is one flat method per wire operation, a straight line
of checks in STANDARD.md's order (the reason `.rubocop.yml` turns the size
metrics off: splitting them would add a call to every read and write); the bit
primitives (`WriteStream#put`, `ReadStream#take`) are `while` loops over local
variables, with no block; and the reader's words are immediates (above), which
is where YJIT gains most: 32-bit words read twice as fast as 64-bit ones under
YJIT. YJIT is the caller's choice (`ruby --yjit` or
`RUBY_YJIT_ENABLE=1`); the library never turns it on.

## Checking this page

- `make docs-check` runs every `ruby` block of README.md and this page, each in
  its own interpreter under `ruby -w` with `lib/` on the load path, and resolves
  every relative link; `make docs-check-negative-control` plants a block that
  raises, a block that warns and a broken link in copies of README.md and
  requires each red by name (`test/docs_check.rb`).
- [ROADMAP.md](ROADMAP.md) is generated from [docs/roadmap.sexp](docs/roadmap.sexp)
  by `make roadmap` (`test/roadmap.rb`, standard library only, the file read as
  data and never evaluated); `test/test_docs.rb`,
  `test_roadmap_is_generated_from_the_sexp`, holds the page to the data. Ideas
  and wanted features go in the sexp, never in issues.
