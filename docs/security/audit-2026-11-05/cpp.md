# Security audit, C++ leg — 2026-11-05 round

Scope: what the C++ generators (`internal/codegen/cpp/`, `internal/codegen/cpptable/`)
add on top of the serialize runtime, and the read paths they emit: the packet
wire (`<Base>Wire.h`), the table wire (`<Base>Table.h` / `.cpp`: file form,
variable class, message form and announcement), the fixed form, the JSON text
form, the text form, and the generated views. The serialize runtime itself is
audited by its own card and is cited here only where generated code calls it.

Base audited: `sprint/schema-100` at `a007c180ba4b09c7d9d5404de6e072043e034777`.

Mode: REPORT-ONLY (coordinator note, 2026-11-05 13:40). Suspected issues are
reported from reading the generator source and the code it emits. Nothing was
reproduced; no hostile-input test, corpus entry, fuzz seed or proof input was
written. Every finding below is open, and the pin column names what a later
card should add.

Emitted code was read from a scratch `schema generate --lang cpp` of the units
the hostile-corpus runner builds (`make/schema-cpp-runs-hostile-corpus.mk`,
`CPP_HOSTILE_UNITS`) and from the committed trees `generated/cpp/` and
`testdata/golden/tables/`. Line numbers name the GENERATOR source unless the
row says otherwise; each bound is greppable by the name given.

## Entry-point table

### Packet wire (`internal/codegen/cpp/`)

| generated function | input | bound relied on | verdict |
| --- | --- | --- | --- |
| `Read<T>( serialize::ReadStream &, T & )`, ranged int ≤ 32 bits | wire bits | `read_int( stream, v, min, max )` (functions.go:762); serialize refuses outside `[min, max]` | bounded |
| `Read<T>`, ranged int64 | wire bits | `read_int64` (functions.go:764) | bounded |
| `Read<T>`, ranged uint64 with headroom | wire bits | `if ( offset_value > <span>ull ) return false` (functions.go:776) | bounded |
| `Read<T>`, int128 / fixed-point | wire bits | `read_fixed` / `read_int128` with the declared raw bounds (functions.go:723, :733) | bounded |
| `Read<T>`, counted array `[N]` with count | wire count | `read_int( stream, <f>_count, <min>, <N> )` before the loop (functions.go:688); storage `T f[N]` | bounded |
| `Read<T>`, `string(N)` / `bytes(N)` | wire length + payload | `read_int( stream, <f>_length, 0, N )` before `read_bytes` (functions.go:814); storage `char f[N + 1]` / `uint8_t f[N]` | bounded |
| `Read<T>`, `string(N)` content | payload bytes | `schema_interior_null` (nullscan.go:21) then `schema_utf8_valid` (utf8.go:44): truncated, bare continuation, overlong, surrogate, > U+10FFFF all refused | bounded |
| `Read<T>`, `wstring(N)` | wire length + 32-bit groups | `read_int( …_length, 0, N )` (functions.go:832); `group == 0 \|\| group > 0xFFFF` and surrogate pairing refused per group (functions.go:844-857) | bounded |
| `Read<T>`, enum | wire tag | `read_int( stream, enum_value, 0, <Max> )` (functions.go:862); variants dense from 0 | bounded |
| `Read<T>`, union tag | wire tag | `read_int( stream, tag_value, 0, <Type>::Max )` (committed `generated/cpp/WireWire.h:403`) | bounded |
| `Read<T>`, compressed float32 | wire bits | `serialize_compressed_float_precomputed` (functions.go:806), runtime's own step bound | bounded (runtime) |
| `Read<T>`, float32 / float64 | wire bits | `read_float` / `read_double`; the runtime refuses non-finite bits (`double_bits_are_finite`) | bounded (runtime) |
| `Read<T>`, alignment pad | wire bits | `read_align` refuses nonzero padding | bounded (runtime) |
| `Read<T>`, refusal state | any refusal | none: fields read before the refusal stay stored | open finding F6 (info) |

### Table wire, file form and variable class (`internal/codegen/cpptable/`)

| generated function | input | bound relied on | verdict |
| --- | --- | --- | --- |
| `TableOpen` (every file-form load) | form byte + trailer | `count > bytes / 8` and `count > kTableProbeBound` (4096) refused before any entry (cpptable.go:1594, :1621) | bounded |
| `TableReader::getleb` | LEB128 | ten bytes max, tenth byte ≤ 1, non-minimal refused, cursor restored on refusal | bounded |
| `TableReader::skip`, `room` | wire lengths | `room( uint64_t )` compares unsigned against what remains (cpptable.go:1396); kind 34 and unknown kinds are damage | bounded |
| `TableBodyEndsEarly` | body terminator | walked before the body is decoded (cpptable.go:1698) | bounded |
| `<T>Load`, `<T>LoadTolerant`, `<T>LoadVerdict`, `<T>LoadBody` | file wire | `has` / `room` per field; kind 12 text `TableUtf8Valid` (text.go:36) before `TableUtf8Clamp` (never above the bound) | bounded |
| `<T>LoadMeasure` | file wire | the exact region the framing commands, returned before allocation (pointers.go:1713) | bounded |
| `<T>ReadStrict`, `<T>LoadBuilder` | file wire | `LoadMeasure` first, then the caller's allocator (pointers.go:1283, :1246) | bounded |
| map / list fill (`TableMapFillBegin`, list fill) | wire count | `n > INT32_MAX` refused; `pad + bytes > carve->left` refused (maps.go:677, :806; lists.go:599, :702) | bounded |
| `<T>LoadRetain`, `<T>MeasureRetain` | file wire | `kTableRetainDepthMax` on the retained path (retain.go:158) | bounded |

### Table wire, message form

| generated function | input | bound relied on | verdict |
| --- | --- | --- | --- |
| `AnnounceRead` / `AnnounceReadOnce` | announcement file | `vocabulary.max_bytes`, `max_entries` (message.go:874, :902); entry shapes: string max ≤ `INT32_MAX`, array min/max ≤ `kTableMessageListMax` (message.go:625-636) | open finding F1 (medium): vocabulary framing length compared signed (message.go:852, :866) |
| `TableMessageBatchOpen`, `<T>LoadMessages`, `<T>LoadMessageBody` | message batch | bit reader `has( n )` with `n >= 0`; string/bytes counts bounded by the announced max (≤ `INT32_MAX`), so `(int64_t) n * 8` cannot overflow; batch above the caller's storage refused (`batch_too_large`) | bounded |
| `TableMessageSkip`, `TableMessageSkipBody`, `TableMessageSkipElement` | unknown fields | `depth > 128` refused (message.go:1047, :1109, :1178) | bounded |

### Fixed form

| generated function | input | bound relied on | verdict |
| --- | --- | --- | --- |
| `<T>FixedLoad`, `<T>FixedLoadTolerant` | fixed wire + layout | `TableFixedParseLayout` is the whole of what is trusted: entry count fits the layout length exactly (fixedruntime.go:1147); `kTableFixedMaxDepth = 64` on the check walk (fixedruntime.go:757); refusal names per `fixedrefusalnames_test.go` | bounded |

### Text forms

| generated function | input | bound relied on | verdict |
| --- | --- | --- | --- |
| `<T>FromJson`, `<T>FromJsonTolerant` | JSON text | `kTableJsonMaxDepth = 128`, `kTableJsonMaxKey = 256`, `kTableJsonMaxNumber = 512` (json.go:871-878); numbers scanned into a fixed buffer refused at capacity | bounded; duplicate tracking window is 4096 fields per table (F8, info) |
| `<T>LoadTextForm`, `<T>EditTextForm` | text form | `depth > 128` → `text_limit` (text.go:395); converts to JSON then the `FromJson` bounds; strict on unknown, kind mismatch, duplicate and clamp (text.go:1720, :1743) | bounded |

### Trusted class and registry

| generated function | input | bound relied on | verdict |
| --- | --- | --- | --- |
| `<T>Open`, `<T>BlockOpen`, `TableCookOpen` | cooked / block image | trusted class by contract (SPEC-TABLES §7.4, §19): opened at a build version, integrity by signature; size and alignment checked (cook.go:187) | out of scope (trusted class) |
| `<Package>View.h` / `.cpp` `UnitView()` | none (compiled-in registry) | no untrusted input | not a read path |

### Generator (hostile schema)

| generated text | schema input | bound relied on | verdict |
| --- | --- | --- | --- |
| identifiers (types, fields, arms, variants, constants, package) | names | scanner identifier rule; `reservedLanguages` C++ row refuses keywords (internal/check/check.go:3552) | bounded for code injection; macro names not refused (F7, info) |
| `// doc` lines | `///` doc comments | none: emitted verbatim (ir/doctags.go:22) | open finding F2 (medium) |
| `#include "<cpp_include>"` | `cpp_include` string | none beyond "a string literal" (internal/check/check.go:1107) | open finding F3 (medium) |
| descriptor JSON key literal | `json = "<key>"` | none beyond non-empty (internal/check/check.go:2203) | open finding F4 (low) |
| descriptor doc / tag literals | doc comments, tags | Go `strconv.Quote` (ir/doctags.go:40, :52) | open finding F5 (info) |
| output file names | schema file base name, package | identifier-shaped package (`viewBase`, view.go) | bounded |

## Findings

Format per finding: file:line; what attacker input does there; severity; the
reduction that removes it; the pin a later card should add. All are open.

### F1 — announcement vocabulary length compared as signed

- **Where:** internal/codegen/cpptable/message.go:852 and :866, emitted into
  `AnnounceReadOnce` in every `<Base>Table.h` of a unit with a message
  vocabulary (49 committed copies; e.g.
  testdata/golden/tables/arms/CarryTable.h:2488).
- **Attacker input:** the announcement is read from the peer. The vocabulary
  field's framed length is a 64-bit LEB128 and is tested with
  `r.has( (int64_t) framed )`. A length of 2^63 or more turns negative and
  passes `has`; a length in `[2^63 - offset, 2^63)` makes `offset + bytes`
  overflow a signed 64-bit integer, which is undefined behaviour. The same
  block then computes `end = r.offset + (int64_t) framed` and
  `at + (int64_t) length != end`, two more signed additions over wire numbers.
  Today the read fails closed only because the following `begin >= end` test
  happens to reject a wrapped `end`; with the overflow being UB, a compiler may
  fold that test into `framed <= 0`, after which `TableMessageLeb` runs with
  `end` behind `at`. The file's own `room()` comment names this exact bug
  class and the fix.
- **Severity:** medium (undefined behaviour in a bounds check on a
  network-facing read; no out-of-bounds access shown).
- **Reduction:** `!r.room( framed )` in place of `!r.has( (int64_t) framed )`,
  and `length != (uint64_t) ( end - at )` in place of
  `at + (int64_t) length != end`. Same refusal, no new behaviour.
- **Pin:** an `announce` carrier row whose vocabulary length is
  `0x7FFFFFFFFFFFFFF0`, run under UBSan by `make schema-cpp-runs-hostile-corpus`,
  or the matching mutant in `tables-wire-fuzz`.
- **Why not fixed here:** the one-line generator change regenerates 49
  committed headers under testdata/golden/tables/ and generated/bench/, which
  are outside this card's paths. The C leg's emitter should be checked for the
  same spelling by its own audit.
- `PATHS-PROPOSED: internal/codegen/cpptable/message.go, testdata/golden/tables/**, generated/bench/paired/cpp/**, generated/bench/tables/cpp/**`

### F2 — doc comment text can end its own comment in generated C++

- **Where:** ir/doctags.go:22 (`DocComment`), called from
  internal/codegen/cpp/cpp.go:340, :427, :454, :480, :553, :572 and
  internal/codegen/cpptable/codecs.go:216, :231. The scanner keeps every byte
  of a doc line but `\n` (internal/scanner/scanner.go:241-245; `TrimRight`
  drops only TRAILING space, tab and CR).
- **Attacker input:** a schema someone else wrote. A lone CR inside a `///`
  line is a line end to the C++ compilers, so the text after it leaves the
  `//` comment and is compiled as code or read as a preprocessor line. A doc
  line ending in a backslash is a line splice, so the NEXT generated line (a
  storage member, an enum variant) joins the comment and disappears.
- **Severity:** medium under SECURITY.md's compiler clause ("any path where a
  schema file causes the compiler to … execute anything"); not a read-path
  issue and not reachable from a trusted schema.
- **Reduction:** refuse at the first point it is seen: the scanner refuses a
  control byte other than tab inside any comment or string literal, and
  refuses a doc line whose last byte is a backslash. Nothing is escaped or
  repaired in the emitter.
- **Pin:** scanner unit vectors (a CR inside `///`, a `///` line ending in
  `\`) that must be refused by name.
- `PATHS-PROPOSED: internal/scanner/scanner.go, ir/doctags.go`

### F3 — `cpp_include` emitted unescaped into `#include`

- **Where:** internal/codegen/cpp/cpp.go:190 and the twin in
  internal/codegen/cpptable/cpptable.go (the `nativeIncludes` loop at
  :2201) write `#include "<value>"`; internal/check/check.go:1107 accepts any
  string literal.
- **Attacker input:** a schema someone else wrote. A lone CR in the value ends
  the directive and starts a new source line, which is the same injection as
  F2 at file scope. The value is otherwise taken as a path as written
  (absolute, `..`), which is by design: it names the user's own header.
- **Severity:** medium (compiler clause, as F2).
- **Reduction:** refuse a `cpp_include` holding a control byte or a byte
  outside a header-path set (letters, digits, `_ . / -`), in the checker.
- **Pin:** checker diagnostics test vectors refused by name.
- `PATHS-PROPOSED: internal/check/check.go`

### F4 — table JSON key emitted unescaped inside a C++ string literal

- **Where:** internal/codegen/cpptable/codecs.go:3144 builds
  `"\"" + key + "\""`, written into the field-info row at :3409;
  internal/check/check.go:2203 accepts any non-empty literal.
- **Attacker input:** a schema someone else wrote. A key ending in a backslash
  escapes the generator's closing quote, so the literal swallows the following
  columns on that line, and later schema-controlled text on the same row (the
  quoted doc column) lands in code context. At least a compile failure; code
  injection is plausible but needs contrivance.
- **Severity:** low.
- **Reduction:** refuse a `json` key holding a backslash or a control byte in
  the checker. Escaping in the emitter was not taken: it is the repair shape,
  and Go's escapes are not exact C++ escapes (F5).
- **Pin:** checker diagnostics test vector refused by name.
- `PATHS-PROPOSED: internal/check/check.go`

### F5 — Go string escapes used for C++ literals

- **Where:** ir/doctags.go:40 (`QuoteDoc`) and :52 (`QuotedTags`), used by
  internal/codegen/cpptable/codecs.go:3126 and the tag statics.
- **Attacker input:** a non-printable byte in a doc comment or tag becomes
  `\xNN`; a hex digit after it extends the C++ escape (`\x01ab` is one escape),
  giving a compile error or a different descriptor string. No injection: the
  quote and backslash are escaped.
- **Severity:** info.
- **Reduction:** the F2 scanner refusal removes the input; otherwise emit
  octal escapes for C-family targets.
- **Pin:** the F2 scanner vectors.
- `PATHS-PROPOSED: ir/doctags.go, internal/scanner/scanner.go`

### F6 — a refused packet read leaves a half-filled value

- **Where:** internal/codegen/cpp/functions.go:814-826 (and every
  `Read<T>` body): fields are read straight into the caller's value; for
  `string(N)` the payload and `<f>_length` are stored before the interior-null
  and UTF-8 checks, and the terminator `f[length] = 0` is written only on
  success.
- **Attacker input:** any refused buffer. A caller that ignores `false` and
  reuses the value sees earlier fields from the hostile buffer and a string
  whose terminator may not sit at its length.
- **Severity:** info (the contract is `false` = discard; nothing is read out
  of bounds; the hot path is not restyled without fleet bench numbers).
- **Reduction:** state in USAGE/SPEC that the value is unspecified after a
  refused read; no code change proposed.
- **Pin:** none (documentation).
- `PATHS-PROPOSED: docs/USAGE.md`

### F7 — C++ reserved list holds keywords, not macro names

- **Where:** internal/check/check.go:3552.
- **Attacker input:** a schema name equal to an object-like macro the
  generated code's headers define (`NULL`, `INT32_MAX`, `EOF`, `errno` on some
  C libraries). The expansion breaks the declaration: a compile failure, fail
  closed. No weakened check was found.
- **Severity:** info.
- **Reduction:** add the macro names the generated headers depend on to the
  C++ row.
- **Pin:** `internal/check/reservedtargets_test.go` vectors.
- `PATHS-PROPOSED: internal/check/check.go`

### F8 — JSON duplicate-key tracking is windowed

- **Where:** internal/codegen/cpptable/json.go:2356-2357, the comment on
  `TableJsonRequireSeen` and its seen bitmap: a table with more than 4096
  fields keeps the old reading for a duplicate past the window.
- **Attacker input:** a duplicate key on a field past index 4096 of a very
  wide table is not counted as `duplicate`.
- **Severity:** info (documented; needs a schema with more than 4096 fields).
- **Reduction:** refuse such a table at check time, or size the bitmap by the
  table.
- **Pin:** a generator test on a 4097-field table.
- `PATHS-PROPOSED: internal/codegen/cpptable/json.go, internal/check/check.go`

## Findings table

| id | file:line | effect | severity | status | commit | pin |
| --- | --- | --- | --- | --- | --- | --- |
| F1 | internal/codegen/cpptable/message.go:852, :866 | signed reinterpretation / overflow of the announced vocabulary length | medium | open | — | `announce` row, vocabulary length 0x7FFFFFFFFFFFFFF0, under UBSan |
| F2 | ir/doctags.go:22; internal/scanner/scanner.go:241-245 | CR or trailing backslash in a doc comment escapes the generated `//` comment | medium | open | — | scanner refusal vectors |
| F3 | internal/codegen/cpp/cpp.go:190; internal/check/check.go:1107 | CR in `cpp_include` injects a source line | medium | open | — | checker refusal vectors |
| F4 | internal/codegen/cpptable/codecs.go:3144 | backslash in a `json` key breaks the descriptor literal | low | open | — | checker refusal vector |
| F5 | ir/doctags.go:40, :52 | Go `\x` escapes read greedily by C++ | info | open | — | F2 vectors |
| F6 | internal/codegen/cpp/functions.go:814-826 | refused read leaves partial value | info | open | — | documentation |
| F7 | internal/check/check.go:3552 | macro names not reserved; compile failure | info | open | — | reserved-name vectors |
| F8 | internal/codegen/cpptable/json.go:2356 | duplicate keys past 4096 fields uncounted | info | open | — | 4097-field test |

## Gate

`go test -count=1 -timeout 600s ./internal/ci/ -run 'HostileCorpus|HostileRefusal'`
and the leg's generator tests (`./internal/codegen/cpp/...`,
`./internal/codegen/cpptable/...`) were run at the base and again on this
card's head; this card changes no code, so the two runs agree. Two generator
tests are red at the base, before this card, on a macOS host:
`TestDamageDiscardsTheWholeLoadCPP` ("Root: Inner's body is not where this
test expects it") and `TestMessageFormHeldCPP` ("<T>Load refused the
file-form golden", twelve subtests). Those reds belong to the base.

## Not done

- No fix landed: F1's surgical fix moves committed headers outside this card's
  paths, and F2-F5, F7 and F8 belong in the scanner, the checker or `ir/`.
- No hostile-corpus row, fuzz seed, reproduction or proof input (report-only
  scope).
- `make schema-cpp-runs-hostile-corpus` and `make tables-wire-fuzz` were not
  run: they need a sibling serialize checkout and a Linux bench, and this
  audit changes no code.
- TLA+: no state or index arithmetic was changed.
