# The Ruby port: the design

The request to support Ruby, recorded 2026-11-10, starts this design. This
page is the Ruby leg's design. Every layer of the leg is built to it, and a
layer that needs to depart from it changes this page first. The layers, bottom
up, and what secures each are in [plans/ruby-leg.md](plans/ruby-leg.md); the
roadmap work-set is `port/ruby` in `docs/roadmap.sexp`, one task per layer.

The leg is built to the one-read contract from its first line:

- **One read.** A read returns the whole value or refuses, with no partial
  value. The leg has no tolerant read.
- **The verdict is the cross-language contract.** An input is accepted, and
  its value written back is byte-identical to the canonical wire, or it is
  refused. Refusal reasons, counters and sizes are Ruby's own diagnostics and
  are never compared across languages.
- **Writes are deterministic.** The binary and JSON writers produce one
  encoding per value.

## THE DECISIONS

The plan's eight decisions are written with their options and a
recommendation. Each is marked **recommended, awaiting the ruling** until the
owner rules on it; nothing is built on an unruled decision.

| | decision | options | recommendation |
|---|---|---|---|
| D1 | what runs | (a) pure Ruby, standard library only; (b) (a) plus an optional C extension; (c) a hand-written or generated C extension, or the C leg's codecs through a foreign-function interface | **Pure Ruby, standard library only, with two code shapes measured against each other, YJIT on and off, and a C-extension comparison in R4.** The packet layer emits straight-line code with locals bound and a descriptor walk for one type, and the leg keeps whichever measures faster. The same pinned interpreter benchmarks a Ruby C API extension around the generated C codec, including the Ruby call and value conversion costs, against the same byte-checked workload. R4 records all three comparisons; shipping an optional extension is a separate, later decision, made only if those numbers justify it; recommended, awaiting the ruling |
| D2 | which Ruby | (a) one current CRuby; (b) a floor and a current pin; (c) a different implementation, such as JRuby or TruffleRuby | **A floor and a current pin.** The floor is the oldest CRuby series still in normal maintenance at pin time; the current pin is the newest stable release at pin time. Both are confirmed on ruby-lang.org on the day they are pinned, never recalled, and the generated code and runtime use nothing newer than the floor; recommended, awaiting the ruling |
| D3 | the toolchain | (a) the ruby-lang.org source tarball, pinned by sha256, built into `dist/`; (b) a third party's prebuilt archives; (c) the interpreter found on `PATH` | **The ruby-lang.org source tarball, pinned by sha256 into `dist/`**, as the Python and Lua legs build theirs. The analyzers (RuboCop and Steep with RBS) come from a hash-locked list, installed offline into the pinned interpreter; recommended, awaiting the ruling |
| D4 | the bit layer | (a) serialize.rb, a serialize-family sibling at format 2.0 in its own repository; (b) no sibling, with the schema runtime carrying its own bit reader and writer | **serialize.rb**, a serialize-family sibling at format 2.0, judged by the family corpus before schema stands on it. It is staged in this tree until its public repository exists, then vendored by sha256; recommended, awaiting the ruling |
| D5 | packaging | (a) the runtime ships beside the generated code, one namespace per unit, `require_relative` only, nothing to install; (b) a gem the generated code depends on; (c) both | **The runtime ships beside the generated code**, one namespace per unit, with `require_relative` only and nothing to install. Publishing a gem is a separate, later decision; recommended, awaiting the ruling |
| D6 | errors | (a) a read returns the value or raises `Refused`, a write raises `InvalidValue`, both `StandardError` subclasses; (b) a `(value, error)` pair; (c) `nil` plus a report | **A read returns the value or raises `Refused`; a write raises `InvalidValue`.** Both are `StandardError` subclasses named by the design, and a refusal carries no partial value. Enums are plain `Integer` constants; recommended, awaiting the ruling |
| D7 | the JSON reader | (a) a port of the engine's reader (`internal/tabletext`); (b) Ruby's `json` library with hooks | **A port of the engine's reader**, never the `json` library. The library accepts `NaN` and `Infinity`, keeps the last duplicate key and decodes lone-surrogate escapes, and hooks cannot catch the third; recommended, awaiting the ruling |
| D8 | the release | (a) Ruby joins the v3.0.0 gate; (b) Ruby is a post-3.0.0 port, held to the same bar but not blocking the release | **Ruby does not block v3.0.0.** It is held to the same bar as every leg, on its own schedule; recommended, awaiting the ruling |

## THE PIN

Two interpreters and one analyzer lock, all under `dist/` (gitignored), none
found on `PATH`. `make dist-ruby` builds them; `make/ruby-toolchain.mk` holds
the pins. The pin does not name a version from memory: P1 confirms both
releases on ruby-lang.org on the day it pins them and fills these rows from
that page.

| pin | what | from |
|---|---|---|
| `RUBY_FLOOR` | the oldest CRuby series still in normal maintenance on the pin day | the source tarball ruby-lang.org publishes for it, its sha256 checked before anything is unpacked |
| `RUBY` | the newest stable CRuby release on the pin day | the source tarball ruby-lang.org publishes for it, its sha256 checked before anything is unpacked |
| `RUBY_ANALYZERS` | RuboCop and Steep with RBS, at the lock's versions | `make/ruby-analyzers.lock`, one sha256 per gem |

The sums are the ones ruby-lang.org publishes for each tarball, and the
download is checked against them before anything is unpacked: any other sha256
is refused by name. Each interpreter is `./configure --prefix=dist/ruby-<v>`
and `make`, with no optimization flags. The current pin is built with YJIT
enabled when the release supports it, and every measurement runs it twice,
with `--yjit` and with `--disable-yjit`; the floor is measured the same way
where its release supports YJIT. YJIT's own build inputs are pinned with the
interpreter and never fetched at run time. The leg needs the standard
library's `string`, `array`, `hash`, `enumerator`, `io` and `encoding`
modules, none of which needs an optional system library; the build needs a C
compiler and, for YJIT, a Rust toolchain taken from the bench's offline
registry.

The lock names every analyzer gem and its sha256. Bundler installs the lock
offline (`bundle install --local --frozen`) into the pinned interpreter's gem
directory. RuboCop checks the generated code and the runtime against the leg's
config, and Steep checks them in strict mode against the RBS the emitter
writes; a finding from either is a failure, as is one warning from `ruby -w`.

Every interpreter the leg starts runs isolated: `RUBYOPT` is empty,
`GEM_HOME` and `GEM_PATH` are unset, and the process starts with
`--disable-gems`, so no user gem, environment option or `PATH` lookup can
change what runs.

R1 exposes `make toolchain-ruby` and a standalone control in the support
fragment. It does not yet add Ruby to `TOOLCHAIN_LEGS`: the aggregate's skip
names come from `TEST_LEGS`, which arrives with the skeleton in R3. R3
registers `test-ruby`, `TOOLCHAIN_LEGS` and `TOOLCHAIN_PINS_ruby` together in
`make/ruby.mk`, then runs the aggregate toolchain control.

The gates over the pin are `make toolchain-ruby` (each pin resolves, or
the gate refuses by the leg's name and the pin's), `make
ruby-toolchain-check` (each interpreter's own `RUBY_VERSION` is its pinned
release, the analyzers' own `--version` output is the lock's, and no file of
the leg starts an interpreter from `PATH`), and the controls of both.

## THE VALUE MAPPING

The rule: **a Ruby value is the schema value, exactly. Integers of every width
are `Integer`, checked against their declared range on write; floats are
`Float`; text is a UTF-8 `String`; bytes read as an ASCII-8BIT `String`. A
value of the wrong Ruby type, or outside its contract, is refused on write,
never coerced.**

Ruby's `Integer` is arbitrary precision, so the 64-bit and 128-bit kinds need
no lanes and no wrapping helpers: a `uint64` is a non-negative `Integer` below
2^64, an `int128` is an `Integer` in [-2^127, 2^127). The range check on write
is what holds a value to its width.

### Packet types (SPEC §4)

| schema | Ruby value | born as |
|---|---|---|
| `bool` | `true` or `false` | `false`, or the default |
| `int8` … `int64`, `uint8` … `uint64`, ranged or bare, `timestamp` | `Integer` | `0`, or the default |
| `int128`, `uint128` | `Integer` | `0`, or the default |
| `bits(N)` | `Integer` in [0, 2^N) | `0` |
| `fixed(I, F)`, `ufixed(I, F)` | `Integer`: the raw scaled integer, as every leg stores it | the default's raw value |
| `float32`, raw or compressed | `Float` holding a value a float32 represents exactly | the default rounded to float32 |
| `float64` | `Float` | `0.0`, or the default |
| an enum `E` | `Integer` | the default variant's number, else `0` |
| a `flags` set `F` | `Integer`: the uint64 mask | `0`, or the default mask |
| `string(N)` | `String`, UTF-8 | frozen `""`, or the default |
| `bytes(N)` | `String`, ASCII-8BIT | frozen `"".b`, or the default |
| `[N]T` | `Array` of exactly N | each element at its default |
| `[..N]T`, `[A..N]T` | `Array`; its length is the count, in [A, N] | A elements at their defaults (SPEC §4.6) |
| a type `T` | an instance of the generated class `T` | `T.new` |
| a union `U` | an instance of `U`: `type`, the arm number, and one reader per payload arm | `type = 0`, every payload arm built |
| `const` | a module constant, the schema expression in a trailing comment | — |
| `reserved`, `align` | no storage | — |
| `if` / `else` branches (§4.4) | both sides' fields are stored; a successful read sets the untaken side to its zero values, not its defaults (SPEC §5) | both at their defaults |

**Enums are plain `Integer` constants.** An enum is a module of integer
constants, one per variant spelled as the schema spells it, from 1, beside
`None = 0` and `Max`, the spellings every leg uses (SPEC §4.2), with a frozen
name table. It is not a Ruby `Enum` (there is no such built-in) and not an
object per variant, both of which cost construction and comparison on a hot
path. A read refuses a value above `Max` (SPEC §4.2: no unnamed enum value
exists), so a plain `Integer` never holds one either. A flags set is a module
of mask constants with a name table, the same way.

**Classes.** Every type, table and union is a generated class whose fields
are methods. `initialize` builds every field at its default (SPEC §5), every
nested value and union arm its own object, never one object shared by two
slots. Generated classes define `==` and `inspect`; they do not define
`eql?` or `hash` by value, so a mutable value is never a hash key by value, as
Python's unhashable classes are not. `==` compares floats by their bits, so
`-0.0` and `0.0` differ, as their bytes on the wire do. The classes are not
frozen; their default strings, constants and name tables are.

### Tables (SPEC-TABLES §2)

A table has the same value shape as a type; the rows below are what tables
add.

| schema | Ruby value | born as |
|---|---|---|
| a table by value | an instance of its class | `T.new` |
| `*T` | an instance, or `nil` | `nil` |
| `*bytes`, `*string` | an ASCII-8BIT `String` or a UTF-8 `String`, or `nil` | `nil` |
| `?T` | the value, or `nil` for absent | `nil` |
| `[]T`, text lists | `Array` | `[]` |
| `[E]T` (§2.4) | the generated keyed-array class over an `Array` of exactly `E::Max`, the element for variant k at index k − 1; every accessor, including `[]` and `[]=`, first requires an `Integer` key in `1..E::Max`, otherwise raises `IndexError`, and only then subtracts one; this refuses `None = 0`, negative keys, `nil` and keys above `Max`, as §2.4 requires | each element at its default |
| `map[K]V` | `Hash`; the save writes it sorted by key | `{}` |
| a union with table arms | as a packet union | `type = 0` |

Each table class also carries its **descriptor**, the table as data
(SPEC-TABLES §8.1), as a class method built once when the unit loads.

## THE NUMBER MODEL

- **Integers.** `Integer` everywhere. A write checks `value.is_a?(Integer)`
  (`true` and `false` are not integers in Ruby) and the declared range, and
  raises `InvalidValue` naming the field when either fails.
- **Booleans.** A `bool` field takes `true` or `false`; `0` and `1` are
  refused.
- **Floats.** A float field takes `Float`; an `Integer` is refused, as a float
  where an integer belongs is.
- **float32.** Ruby has no float32. A float32 field holds the `Float` that is
  exactly the float32 value. A write first refuses a value that is not finite
  (`Float#finite?`), then rounds it to float32 as Go's `float32(v)` does with
  `[v].pack("e")`: IEEE round to nearest, ties to even. The rounded value is
  unpacked again and checked; a finite value whose float32 rounding is not
  finite is refused as `InvalidValue`, because it is outside the float32
  range. No write refuses a double for having more precision than a float32:
  it rounds, as every leg's writer does.
- **Compressed float32.** The quantization (the float32 arithmetic of SPEC
  §4.3's step index) is serialize.rb's `serialize_compressed_float`, held to
  the family corpus. Generated code never quantizes on its own.
- **Finite only.** A NaN or an infinity is refused on write as
  `InvalidValue`, and on read, by its raw bits, before any conversion, as
  `Refused` (`non_finite_float`). Negative zero and the subnormals ride bit
  for bit.
- **Bits.** A float's bits move through `Array#pack` and `String#unpack1` with
  explicit little-endian directives (`"e"` for float32, `"E"` for float64,
  `"V"` for uint32, `"Q<"` for uint64), so no read or write consults the
  host's byte order. The same directives carry every multi-byte integer.

## TEXT AND BYTES

- **Text** is `String`, UTF-8 on the wire, encoded and decoded strictly. A
  text field takes a `String` whose encoding is UTF-8 or US-ASCII; any other
  encoding is refused rather than transcoded. A write validates
  `valid_encoding?` and refuses a value with invalid bytes, including a lone
  surrogate, as `InvalidValue`; a read forces UTF-8 on the bytes, checks
  `valid_encoding?`, and refuses an invalid sequence as `Refused`
  (`invalid_utf8`). A packet `string(N)` refuses an embedded NUL, and its bound
  is N bytes of UTF-8, not N characters.
- **No wide text type.** `wstring` and every spelling derived from it are
  refused by the parser (SPEC §4.12). The Ruby leg carries only UTF-8 text;
  conversion from another encoding belongs at the application's boundary.
  serialize format 2.0 has no wide-string operation.
- **Bytes.** A read returns a new ASCII-8BIT `String`, which the caller owns,
  never a view that pins the input buffer. A write accepts a `String` whose
  encoding is ASCII-8BIT; a string with another encoding is refused rather
  than transcoded, so text is never silently reinterpreted as bytes.
- **Frozen literals.** Every generated file starts with
  `# frozen_string_literal: true`. Default strings, constants and name tables
  are frozen. Mutable defaults, arrays, hashes and nested values, are built
  per instance and never shared.

## ERRORS

```ruby
p = Snapshot.read(data)        # a packet: the value, or raises Refused
p, bits = Snapshot.read_bits(data, bit_offset: 0) # also reports bits consumed
data = p.write                 # the bytes, or raises InvalidValue
v = Player.load(data)          # a table: the value, or raises Refused
data = v.save                  # the bytes, or raises InvalidValue
```

- A packet read takes a `String` and consumes exactly the encoded bits.
  `Snapshot.read_bits(data, bit_offset: 0)` returns `[value, bits_consumed]`,
  where the count is relative to the supplied starting bit offset. It
  validates the offset and all fields and padding inside that packet, and
  raises `Refused` without returning a value or count on failure. It does
  not call serialize's `end`: remaining bits and bytes belong to the caller
  (SPEC §5). In a bit-packed stream, the next read starts at
  `bit_offset + bits_consumed`. Byte-framed callers instead advance by their
  frame length, including any final padding from the writer.
  `Snapshot.read(data)` is the convenience form at offset zero that returns
  only the value, with the same trailing-input policy. A table load refuses
  input past the record as its form defines.
- `Refused` and `InvalidValue` are `StandardError` subclasses, so code that
  catches `StandardError` catches both. Each unit defines its own pair in its
  namespace; a caller handling several units catches `StandardError`.
- `Refused#reason` is the refusal's name (`past_end`, `non_finite_float`, ...),
  a diagnostic. It is never compared across languages; only the verdict is.
- A refusal carries no value and does not touch the caller's objects. A read
  builds a new value and returns it only when the whole packet or table
  record has been read.
- A table load takes an optional report object, a `LoadReport` from the
  runtime, which receives the load's counters (`unknown`, `widened`,
  `retained`, `retain_lost`) on success only. A refused load leaves it as it
  was. A packet read has no report: the packet wire has nothing to count.
- A writer validates the whole value before it writes a byte. `InvalidValue`
  names the field path and the rule.

## THE BIT LAYER: serialize.rb

serialize.rb is the serialize family's Ruby member at format 2.0: a port of
`serialize.h` to [STANDARD.md](https://github.com/mas-bandwidth/serialize/blob/main/STANDARD.md),
judged by the family corpus. It is staged in this tree at
`third_party/serialize.rb/`, laid out as its own repository, until
`mas-bandwidth/serialize.rb` exists. Creating that public repository is a
later task. When it exists, the directory moves there as it stands, is
released, joins COMPATIBILITY.md, and is vendored back into the leg runtime by
sha256, following the other language legs.

- **The surface** mirrors serialize.js in Ruby spelling: `WriteStream`,
  `ReadStream`, `MeasureStream`, one `serialize_<op>` per operation on the
  shared `Stream` surface, each scalar carried in a `Ref` object.
- **A read refusal is a value.** A read returns `false`, leaves its `Ref`
  unwritten, and the stream keeps the first refusal's reason from the family's
  closed list (`refusal`). Every later read refuses too. A reader never raises
  on any input. The generated codec turns the first `false` into
  `Refused.new(reason)`.
- **Write misuse raises** `WriteMisuse`, a `StandardError`, at the call and
  before any bit is written. Generated writers validate first, so a
  `WriteMisuse` reaching a caller is a generator bug; the codec re-raises it as
  `InvalidValue`.
- **The accumulator** is a Ruby `Integer` with no fixed width, which appends
  whole 64-bit words to an ASCII-8BIT `String` buffer. There is no fixed word
  buffer and no reused index arithmetic, so the bit layer needs no layout
  model.

## THE RUNTIME LAYOUT

`schema generate --lang ruby --out <dir> <unit>` writes one namespace per
unit, named for the unit's package:

```
<dir>/<package>.rb                  defines the unit module; requires the runtime and every file
<dir>/<package>/<Base>.rb           one file per schema file, reopening the unit module
<dir>/<package>/_schema.rb          the runtime: Refused, InvalidValue, the table runtime
<dir>/<package>/serialize.rb        serialize.rb, vendored by sha256
```

- **Relative requires only.** A module reaches its siblings and the runtime
  with `require_relative`, so the unit works wherever the caller puts it, and
  two generated units never share or shadow each other's runtime.
- **`Refused` per unit.** Each unit's runtime defines its own `Refused` and
  `InvalidValue`, reached as `<Unit>::Refused` and `<Unit>::InvalidValue`. The
  runtime is not a top-level gem or module: that name could belong to an
  unrelated installed library. A caller handling several units catches
  `StandardError`.
- **Module names.** The package ident names the directory and file verbatim;
  the Ruby module constant is its Ruby spelling, split on `_` and capitalized
  per segment (`tabledemo` becomes `Tabledemo`, `my_game` becomes `MyGame`).
  A package that maps to a constant already defined by the interpreter, or
  that collides with another package's mapping in the unit, is refused by
  name. A schema file's base name is its file name and must be a valid Ruby
  identifier; two files whose names differ only in case are refused, because
  macOS and Windows fold case. A file named for the runtime (`_schema`) is
  refused, as is `serialize`, because `serialize.rb` is emitted beside the
  schema files. These reserved basenames are checked case-insensitively,
  before any file is written.

| file | what it is |
|---|---|
| `compiler/target_ruby.go` | the target, behind the `schema_leg_ruby` build tag until the packet surface has claims; the shared refusal chain, then the emitters |
| `compiler/carries_ruby.go` | the carried constructs, registered once from `rubyCarried`; a later layer adds `var _ = carryRuby(<construct>)` in its own file |
| `internal/codegen/ruby` | the type surface's emitter (`Generate`), the identifier rules (`CheckNames`) and, from the packet layer, its codec |
| `internal/codegen/ruby/runtime/` | the runtime, embedded and emitted verbatim: the errors, and serialize.rb vendored by sha256 (`runtime/SERIALIZE_PIN`, `make vendor-serialize-ruby`) |
| `internal/codegen/ruby/rubyrun` | the test helper (THE HARNESS) |
| `internal/codegen/rubytable` | the table surface's emitter and codec, from the table layers (R5) |
| `internal/tablenames/ruby.go` | the table runtime's claimed names, from the table layers (R5) |
| `make/ruby-toolchain.mk`, `make/ruby.mk`, `make/ruby-*.mk` | the pin, the leg, and one fragment per gate |
| `test/conformance/ruby/` | the conformance driver and `ci.json` |

## IDENTIFIERS

Fields keep the schema's spelling (`max_players`), and types, tables and
unions keep theirs (`PlayerState`). Nothing is mangled: SPEC §4.6's
target-name safety has every port refuse a reserved word, never escape it (Go's
escape aside), so **Ruby joins the checker's one reserved-word table**
(`internal/check`, `reservedLanguages`) with a row of its own: its hard
keywords (`alias`, `and`, `begin`, `break`, `case`, `class`, `def`, `defined?`,
`do`, `else`, `elsif`, `end`, `ensure`, `false`, `for`, `if`, `in`, `module`,
`next`, `nil`, `not`, `or`, `redo`, `rescue`, `retry`, `return`, `self`,
`super`, `then`, `true`, `undef`, `unless`, `until`, `when`, `while`, `yield`,
and the `__FILE__`, `__LINE__`, `__ENCODING__`, `BEGIN` and `END` words). The
soft spellings a method may still take, such as `class` in a receiver
position, are not in the row, because the generated code spells every field as
a method definition or a plain call.

The row refuses those names in every unit, whatever the target, as the other
rows do, and nothing is escaped. Then, by rule, in the Ruby target:

- **Constant names must be valid Ruby constants.** A type, table, union, enum
  or flags declaration must start with an uppercase ASCII letter and otherwise
  use only letters, digits and `_`; one that does not is refused by name. An
  enum or flags variant is also a constant, so it takes the same rule. The
  spellings `None` and `Max` are already refused by the checker (they are
  every enum's own names).
- **A declaration named like a core constant or a generated runtime name** is
  refused by name: the constants generated code spells unqualified (`Array`,
  `Hash`, `Integer`, `Float`, `String`, `StandardError`, `Encoding`, `Math`,
  ...), and the runtime's own (`Refused`, `InvalidValue`, `LoadReport`,
  `Stream`, `WriteStream`, `ReadStream`, `MeasureStream`). A type's name is a
  module-level constant, and the generated module names these unqualified.
- **A field must be a valid Ruby method name and not a generated member.**
  Fields keep snake_case and become methods, so a field named for a generated
  member (`read`, `read_bits`, `write`, `load`, `save`, `descriptor`, `initialize`,
  `inspect`, `eql?`, `hash`, `class`, `freeze`, `dup`, `clone`, `send`,
  `method`, `object_id`) is refused by name. The emitter holds the list
  (`generatedMembers`); the table layers claim their runtime's names in
  `internal/tablenames/ruby.go`, as every backend does. A field named `list`,
  `range` or `string` shadows nothing generated and is legal.
- **A name starting with `__`** is refused by name: Ruby reserves the
  double-underscore spellings for the interpreter and for reflection.
- **Generated helpers** are named with the `_s_` prefix, and the Ruby target
  refuses a schema name that starts with it, so no schema name collides with a
  helper.
- **The package's module constant** must not be an existing top-level
  constant of the pinned interpreter. A package whose Ruby spelling is
  `Object`, `String`, `File`, `Dir`, `Math` or another defined constant would
  reopen that class or module rather than create a namespace, so it is refused
  by name, as a Python package that names a standard library module is.

## THE HARNESS

Tests generate into `t.TempDir()`, run the pinned interpreter there, and read
answers from stdout. **Inputs go in as files beside the program; answers come
out as lowercase hex lines.**

- **`rubyrun.Run(t, dir, main, inputs)`** writes each input as `dir/<name>`,
  runs `$RUBY --disable-gems -w <main>` with `dir` as the working directory,
  and returns stdout. The probe `main` loads the generated unit with
  `require_relative`; the one path the test chose, and nothing from the
  environment. `rubyrun.RunFloor` runs `$RUBY_FLOOR` the same way, and every
  packet and table gate runs under both. An interpreter that does not resolve
  fails the test naming `make dist-ruby`; it never skips.
- **No real time, no sockets, no network, no outside service** in a unit
  test. Anything timed (the bench, a soak) is a functional test.
- Every test calls `t.Parallel()`.
- **The conformance driver** runs one interpreter per surface:
  `test/conformance/ruby/main.rb` reads the manifest and its files and writes
  one answer file per case.
- **The analyzer gates** run `ruby -w`, RuboCop and Steep in strict mode over
  every generated tree and the runtime. A warning or finding is a failure.

## THE REGISTER

The `ruby` column of [PORTING.md](PORTING.md) holds every technique ❌,
citing #381 (the language list) and the `port/ruby` layer that carries it, or
`—` with the platform's reason:

- **M2 and M3**: Ruby's small integers are immediates and every `Float` is a
  heap object, so there is no unboxed form to keep.
- **M4**: Ruby has no inlining directive or aliasing qualifier; M4b, the
  self-contained codec, is the shape (R4). YJIT may inline at run time, but
  the source states no directive the leg can carry.
- **M10**: Ruby's GC owns allocation, and a pure-Ruby codec has no allocator to
  hook; the leg allocates inside the VM and says so. An allocation count over
  `ObjectSpace` or `GC.stat` (I1) is the instrument a later layer can pin, as
  the Lua leg pinned its own.
- **M21**: retired by the finite-only rule, as for Lua and Python: the float32
  conversion only ever sees a finite value, and `pack("e")` converts every
  finite float32 exactly.

A cell becomes ✅ when the layer that carries it lands with its proof.

## THE CARD MAP

| layer | what | roadmap task |
|---|---|---|
| R0 | this page, and no Ruby code in the tree that does not follow it | `port/ruby/r0-design` |
| R1 | the pinned interpreters and analyzer lock, `toolchain-ruby` and its control | `port/ruby/r1-toolchain` |
| R2 | serialize.rb at format 2.0, staged in this tree, judged by the family corpus under both interpreters and the analyzers | `port/ruby/r2-serialize` |
| R3 | the skeleton: the target behind its tag, the emitter packages, the register and roadmap columns | `port/ruby/r3-skeleton` |
| R4 | the packet wire against the C++ goldens; the build tag dropped; YJIT measured on and off | `port/ruby/r4-packet` |
| R5 | fixed tables, form 3 | `port/ruby/r5-fixed` |
| R6 | variable tables: the one read | `port/ruby/r6-variable` |
| R7 | the message form and retain-unknown | `port/ruby/r7-message-retain` |
| R8 | JSON, the hand-editable text and base64 | `port/ruby/r8-text` |
| R9 | block and cook accelerators, the unit view, descriptors | `port/ruby/r9-accel` |
| R10 | the conformance driver, every surface | `port/ruby/r10-conformance` |
| R11 | the wire-fuzz differential | `port/ruby/r11-wirefuzz` |
| R12 | the hostile corpus runner and resource bounds | `port/ruby/r12-hostile` |
| R13 | cards | `port/ruby/r13-cards` |
| R14 | the strict analyzer gate and every negative control | `port/ruby/r14-lint-controls` |
| R15 | CI, nightly fuzz, readiness, bench legs, release tier | `port/ruby/r15-ci` |
| R16 | USAGE fragments, per-language prose, the register complete | `port/ruby/r16-docs` |

Each layer is secured before the one above it starts: its gates green, each
with a negative control that goes red when the rule it guards is broken.

Where this page and [plans/ruby-leg.md](plans/ruby-leg.md) differ, this page
is the design as built. The pin lives in `make/ruby-toolchain.mk`, a support
fragment, because `make/ruby.mk` is the leg itself and the registry probes
every `make/<lang>.mk` as a `--lang` port; it arrives with the target in R3.
serialize.rb is staged in `third_party/serialize.rb/` and secured there by the
family corpus, because its own repository does not exist yet; the runtime
vendors it by sha256 from that directory (`SERIALIZE_PIN`), and from its
releases once the repository exists.
