# Dart generated-code audit, 2026-11-05

Base: `e00a794e450705bec2db96430e2439fb54c5f038` (rebased onto stream base
`a007c180ba4b09c7d9d5404de6e072043e034777`).

Scope: `internal/codegen/dart`, `internal/codegen/darttable`,
`internal/tablenames/dart.go`, and the committed generated Dart artifacts under
`generated/` and `testdata/golden/dart/`. The packet emitter is self-contained
and emits `Schema.dart` inline; it does not call an external `serialize.dart`
package. Supported targets are the Dart VM, AOT, and `dart2wasm`
(`docs/SPEC.md:2996`). Writer inputs and already checked `cook`/`block`
accelerator regions are trusted by specification (`docs/SECURITY.md:149`,
`docs/SPEC-TABLES.md:4591`). A failed packet read leaves its destination
unspecified (`docs/SPEC.md:2167`). None of those contracts permits accepting
malformed network input or reading past a buffer.

Per the report-only security scope instruction (2026-11-05), suspected security
issues are reported with exact `file:line` locations, untrusted-input reachability,
calibrated severities, and minimal reductions; no exploit payloads, new
hostile-input tests, corpus rows, or proof inputs are added, and no pins are
changed. This audit is report-only: findings are recorded, not fixed here.

## Entry-point table

`T` denotes each generated struct, union, or table declaration, and `<Home>`
denotes the unit's runtime home (`internal/codegen/darttable/darttable.go:75`).
Paths below are relative to `internal/codegen/` unless written in full.

| Generated function or surface | Input | Bound relied on / source | Verdict |
| --- | --- | --- | --- |
| `readT(value, ByteData, numBits)` | Packet bits, including unions and nested structs | `dart/functions.go:448,459,464`: logical `numBits <= view.lengthInBytes * 8`; fused static bit runs checked before loads; tail bytes assembled inside `view.lengthInBytes`; range, constant, zero-padding, enum/union tag, UTF-8/UTF-16, and raw float exponent checks remain active in release builds | Bounded |
| Packet `Schema` float helpers and inline float32/float64 reads | Raw IEEE-754 bits in `readT` | `dart/runtime.go:40-63`, `dart/functions.go:853,1582`: raw exponent bits checked for NaN/Infinity (`(bits & 0x7f800000) == 0x7f800000` and `(bits & 0x7ff0000000000000) == 0x7ff0000000000000`) before writing/reading single-isolate static `Float32List`/`Float64List` scratch synchronously | Bounded under Dart single-threaded isolate execution (`sca-dart-6`) |
| Packet `string(N)`, `bytes(N)`, `wstring(N)` readers | Length prefix and payload bytes/code units | `dart/functions.go:1180,1245`, `dart/utf8.go:1`: ranged length validated before loop/allocation; UTF-8 scalar validation and interior zero-byte rejection; UTF-16 code-unit range (`1..0xd7ff`, `0xe000..0xffff`) and surrogate-pair validation | Bounded |
| `tWireLoad(data, [report])` | Form-1 table byte slice (`Uint8List`) | `darttable/wireform.go:3174-3220`: constructs fresh result and `TableWireReader(data, data.length)`; `open` verifies form byte, trailer count (`0 <= count <= (data.length - 9) ~/ 8` and `count <= 4096`), and distinct 64-bit IDs via `Set<int>`; resets to default value on malformed, missing-required, non-finite float, count refusal, or cycle | Bounded |
| `tWireLoadRetain(data, retain, [report])` | Form-1 table bytes plus caller-owned `TableRetain` | `darttable/retainwire.go:583-625`: resets caller retain state on entry; uses `TableWireReader` framing and caller byte/ID capacities; clears retained state and returns fresh defaults on malformed, missing-required, non-finite, count, or cycle failure | Bounded |
| `tWireMeasure(data)` | Form-1 table bytes for region sizing | `darttable/wireform.go:3383-3425`: checks trailer framing and 4,096-ID distinctness (`open(strict: false)`), bounds dynamic counts to `int32` and remaining payload bytes, and accumulates schema record sizes without allocating user structures | Bounded measurement helper; callers must still validate on load |
| `TableWireReader.open`, `.sub`, `.over`, `has`, `readLeb`, `skipKind`, `mapEntryScan` | Form-1 framing, nested bodies, LEB128 lengths, and map entries | `darttable/wireform.go:1155,1200-1215,1218-1285,1305-1345,1495-1535`: `has(n)` uses subtraction (`n >= 0 && n <= limit - pos`); `readLeb` caps at 10 bytes, requires 10th byte `<= 1`, and rejects non-minimal trailing zero bytes (`i > 0 && b == 0`); sub-readers slice within caller-checked limits | Bounded |
| `TableWireReader.scanNodeTable`, pointer resolution, and `cyclic` | Flat pointer node table and reference indices | `darttable/wireform.go:1546,1655,1689`: node count justified by framed records in the last well-framed kind-12 node-table field; each placed node decoded at most once; iterative Kahn topological sort rejects cycles without recursive pointer walks | Bounded |
| `tFromJson(text, [report])` and `_jsonDecodeExact` | UTF-8 JSON text (`String`) | `darttable/textform.go:631-647,1133-1145`, `darttable/textmore.go:112-165`: comment/comma pre-pass and exact recursive-descent parser enforce nesting cap `depth <= 128` (`textmore.go:139`) and full-input consumption; returns fresh default on malformed or strict refusal | Open low numeric-work finding (`sca-dart-3`); base tolerance preserved (`sca-dart-6`) |
| `tFromJsonFields`, `_jsonFixed`, union/array/map/text/blob JSON helpers | Parsed JSON tokens and trees | `darttable/textform.go:218-238`, `darttable/textmore.go:305-375`, `textlists.go`, `unboundedtext.go`, `timestamp.go`: field descriptors, schema bounds, UTF-8 byte boundary clamping, and graph label table checks; `_jsonFixed` uses `BigInt` conversion | Open low numeric-work finding (`sca-dart-3`) |
| `tFixedLoad(values, capacity, bytes, byteLength, plan, [report])` | Form-3 fixed file and caller-owned destination/plan capacity | `darttable/fixedmodule.go:639-810`: rejects `byteLength < 0 || byteLength > bytes.length`; checks 20-byte header, known lineage hash, exact layout bytes, record pitch divisibility, and `count <= capacity`; verifies all record hashes before writes; restores prior decoded records on non-finite float or refusal | Bounded under caller capacity contract |
| `TableFixedLayout.parse`, plan compiler, `tableFixedRun`, and `tFixedDecode` | Validated layout, compiled plan, and framed record bytes | `darttable/fixedruntime.go`, `fixeddart.go`, `fixedlineage.go`: layout parser enforces entry/size/depth ceilings and closed kind set; `tFixedLoad` selects only static lock layouts verified byte-for-byte; raw float bits checked before conversion | Bounded through `tFixedLoad` |
| `announceRead(vocabulary, bytes, report)` | Untrusted form-1 vocabulary announcement (`Uint8List`) | `darttable/messageform_announce.go:185-356`: single-attempt terminal latch (`vocabulary.attempted`), form-1 check, `maxBytes` (default 65,536) and `maxEntries` (default 4,096) checks, and reserved-ID / duplicate-entry rejection; trailer count sign/cap and length additions are open below | Open findings (`sca-dart-1`, `sca-dart-2`) |
| `tableMessageEntryEnd`, `tableMessageShapeEnd`, `tableMessageSkipField`, `tableMessageLeb` | Announcement entry shapes and unknown announcement fields | `darttable/messageform_announce.go:366-596`: closed kind set (`tableMessageKnownKind`), scalar bit-width ceiling (`n.$1 <= tableMessageKindWidth(kind) * 8`), and container element kind checks; LEB canonicality and signed offset additions are open below | Open finding (`sca-dart-2`) |
| `tLoadMessages(values, capacity, bytes, vocabulary, [report])` | Form-2 message batch (`Uint8List`) with accepted vocabulary | `darttable/messageform_batch.go:165-245`: requires `vocabulary.announced`, checks `bytes.length >= 2`, form byte `2`, and batch count `1..256` against `capacity` and `values.length`; resets all touched destination slots on damage or refusal | Bounded when vocabulary is validated |
| `tableMessageBatchOpen`, `tableMessageBatchClose`, `TableBitReader.get/skip/align`, `TableMessageShape.decode/skip` | Batch header, payload bits, and unknown announced shapes | `darttable/messageform_batch.go:1039-1270`: `get` restricts bit width to `0..64` and checks `bitPos + width <= totalBits` with sticky failure; `skip` checks `bits >= 0 && bitPos + bits <= totalBits`; `skipEntry`/`skipBody` enforce `depth <= 128`; `close` verifies zero padding to final byte | Bounded |
| `TBlock.open` / `openWithReason` and row accessors | Trusted same-build block buffer | `darttable/block.go:633-720`: checks non-null base, minimum prologue size, magic, endianness, `tableBuildVersion`, 64-byte alignment, and subtraction-guarded (`offset <= length - span`) array triples before constructing view | Bounded at declared trusted-accelerator boundary |
| `TCook.open` / `openWithReason` and node/text accessors | Trusted same-build cooked buffer | `darttable/cook.go:507-585`, `cookruntime.go:74`: checks header size, magic, endianness, `tableBuildVersion`, reserved zero words, alignment, and subtraction-guarded data/attribution extents before constructing view | Bounded at declared trusted-accelerator boundary |
| `cardDescriptors`, `unitView`, and reflection getters | Generated constant descriptors and caller-owned instances | `darttable/viewform.go`, `unitview.go`: static schema metadata; getters read fields of already constructed objects or checked views and parse no wire bytes | Bounded (no network parser) |
| `dart.Generate`, `darttable.GenerateLineage`, `checkNames` | Checked schema IR and file basenames | `dart/dart.go:60-185,363-369`, `darttable/darttable.go:75-185`, `internal/tablenames/dart.go`: checks `dartReserved` and runtime shadow hazards (`Schema`, `Int128`, `UInt128`, `String`, `List`, `Object`, `Endian`, `ByteData`); raw file basenames in generated `import` directives are open below | Open build-time finding (`sca-dart-4`) |

## Findings

Line numbers refer to the audited source (`e00a794e4507` / `a007c180ba4b`;
`internal/codegen/dart/` and `internal/codegen/darttable/` are identical across
both commits).

| ID | Location (`file:line`) | Untrusted input reachability and effect | Severity | Status and suggested reduction |
| --- | --- | --- | --- | --- |
| `sca-dart-1` | `internal/codegen/darttable/messageform_announce.go:204-221` | Untrusted form-1 announcement bytes passed to `announceRead`. `view.getUint64(bytes.length - 8, Endian.little)` returns a 64-bit signed Dart `int`, and `:205` checks only `trailerCount > (bytes.length - 9) ~/ 8`. An 8-byte trailer count with bit 63 set (`0x8000000000000000`..`0xFFFFFFFFFFFFFFFF`) is negative, bypasses `:205`, and computes `end = bytes.length - 8 - trailerCount * 8` (which for `-1` is `bytes.length` and for `-9223372036854775808` wraps to `bytes.length - 8`), allowing the body loop (`:231`) to scan into the trailer bytes. For positive counts, `:205` does not enforce the 4,096-ID ceiling from SPEC-TABLES §3, and `:214-221` performs a quadratic `O(trailerCount^2)` duplicate scan before the `maxBytes` (`:280`) and `maxEntries` (`:317`) checks. Mitigated to one invocation per `TableVocabulary` by `vocabulary.attempted` (`:186-189`). | **medium** | **Open**. Guard `if (trailerCount < 0 || trailerCount > (bytes.length - 9) ~/ 8 || trailerCount > 4096)` and `if (end < 1)` before allocating `ids`, and use a `Set<int>` for duplicate detection matching `TableWireReader.open` (`internal/codegen/darttable/wireform.go:1500-1513`). |
| `sca-dart-2` | `internal/codegen/darttable/messageform_announce.go:263,268,277,282,542,558,583-596` | Untrusted form-1 announcement bytes in `announceRead`, `tableMessageSkipField`, and `tableMessageLeb`. (1) `tableMessageLeb` (`:583-596`) does not reject 10th-byte overflow (`shift == 63 && b > 1`) or non-minimal trailing zero bytes (`shift > 0 && b == 0`), unlike `TableWireReader.readLeb` (`wireform.go:1200-1215`). (2) `announceRead` (`:268`, `:282`) and `tableMessageSkipField` (`:542`, `:558`) evaluate `at + fieldLength.$1` and `n.$2 + n.$1 > end` using signed 64-bit addition instead of subtraction against remaining bytes (`fieldLength.$1 > bodyEnd - at`). A 63-bit LEB length near `0x7fffffffffffffff` wraps `at + fieldLength.$1` to a negative `int`, bypassing `if (fieldEnd > bodyEnd)` at `:269` and reading `bytes[at]` at `:272` without checking `at < bodyEnd`. | **medium** | **Open**. In `tableMessageLeb`, enforce `if (shift == 63 && b > 1) return (0, at, false);` and `if (shift > 0 && b == 0) return (0, at, false);` (preserving signed zigzag bases where valid). In `announceRead` (`:263-290`) and `tableMessageSkipField` (`:542,558`), replace signed additions with subtraction bounds (`fieldLength.$1 > bodyEnd - at`, `at >= fieldEnd`, `length.$1 != fieldEnd - at`, and `n.$1 > end - n.$2`). |
| `sca-dart-3` | `internal/codegen/darttable/textmore.go:333-359` | Untrusted JSON text passed to `tFromJson` for a fixed-point or wide integer field. `_jsonFixed` strips leading and trailing `'0'` characters via repeated `digits.substring(1)` and `digits.substring(0, digits.length - 1)` allocations (`:335-341`) and parses the full digit string with `BigInt.parse(digits)` (`:354`). While `point > 40` / `point < -40` (`:347-352`) prevent unbounded exponent-only `pow` growth, a multi-kilobyte digit token causes quadratic substring copying and large `BigInt` arithmetic before range clamping. | **low** | **Open**. Scan leading and trailing `'0'` indices in `digits` before taking a single substring slice, and bound significant digits (or apply any settled §16.2 numeric token ceiling) before calling `BigInt.parse`. |
| `sca-dart-4` | `internal/codegen/dart/dart.go:363,369`; `internal/codegen/darttable/darttable.go:75-83`, `wireform.go:73`, `fixedmodule.go:74`, `cook.go:68`, `block.go:154` | Untrusted schema filenames at build time (`ir.File.Base` derived from `filepath.Base` in `compiler/compiler.go:163`). `writeImport` emits `import '%s.dart'` using raw `base` without verifying that the file basename is an ASCII identifier or escaping single quotes and line breaks. Not reachable from network bytes. | **low** | **Open (shared compiler ownership)**. Validate schema source basenames against the identifier grammar at the compiler entry (`compiler/compiler.go`, `internal/check/check.go`) before backend code generation. |
| `sca-dart-5` | `internal/codegen/darttable/mapsarms_test.go:110`; `internal/codegen/darttable/fixedclass_test.go:25`; `test/conformance/dart/main.dart:1681` | Test-only fixture drift after `strict-spec` (`b64ea4816`) migrated fixed-root `instance` rows in `testdata/conformance/tables/MANIFEST.txt` to form-3 files under `testdata/conformance/tables/wire/*.bin` while preserving the original form-1 oracle wires under `testdata/wire/tables/*.bin`. `TestFixedClassOnTableWireDart`, `TestMapsAndUnionArmsOnTheTableWireDart`, and `TestCookWriteAnswersEveryCookWriteRowDart` (`surfaceCookWrite` in `test/conformance/dart/main.dart`) exercise the form-1 `<Home>Wire.dart` codecs (`<root>WireLoad`/`<root>WireSave` and `<Root>CookWire`), which refused the form-3 files. | **info** | **Open** (report-only; pin not added). The manifest wire paths in `manifestRows` (`internal/codegen/darttable/mapsarms_test.go:110`) and `surfaceCookWrite` (`test/conformance/dart/main.dart:1681`) are left as the manifest names them; no rewrite onto `testdata/wire/tables/` is applied here. The resulting `internal/codegen/darttable` failures on `TestFixedClassOnTableWireDart` and `TestMapsAndUnionArmsOnTheTableWireDart` are the base's own (see Gate verification) and are not fixed in this audit. |
| `sca-dart-6` | `internal/codegen/dart/runtime.go:40-63`; `internal/codegen/darttable/wireform.go:1166-1189`, `textform.go:218-238` | (a) Shared static typed-data scratch (`Schema.f32`/`f64`/`u32`/`u64` and `_conv`/`_convData`) is written and read within a single synchronous expression after raw NaN/Infinity bit checks; Dart isolates are single-threaded on VM, AOT, and `dart2wasm`, so no intra-isolate data race can occur. (b) Documented base tolerance (clamping, kind mismatch recovery, unknown fields, duplicate JSON keys) is part of the audited base contract; strict-only migration is tracked separately. | **info** | **Accepted base contract**. No change required. |

PATHS-PROPOSED (`sca-dart-1`, `sca-dart-2`):
`internal/codegen/darttable/messageform_announce.go`,
`internal/codegen/darttable/messageform_test.go`,
`tla/DartAnnouncementBounds.tla`,
`tla/MCDartAnnouncementBounds.cfg`,
`tla/MCDartAnnouncementSignedTrailer.cfg`,
`tla/MCDartAnnouncementAddedLength.cfg`

PATHS-PROPOSED (`sca-dart-3`):
`internal/codegen/darttable/textmore.go`,
`internal/codegen/darttable/textform_test.go`

PATHS-PROPOSED (`sca-dart-4`, shared compiler ownership):
`compiler/compiler.go`,
`internal/check/check.go`,
`internal/codegen/dart/dart.go`,
`internal/codegen/darttable/darttable.go`

## Gate verification

Executed on `bench-11` (darwin/arm64, `go1.26.10`, Dart SDK `3.13.2` via
`DART=<work>/sdk/bin/dart`) with `GOCACHE` in the workdir,
`GOFLAGS=-mod=readonly`, `NOVA_TEST_NO_HOST=1`, and `-p 4 -count=1 -timeout 600s`.
Zero warnings in every run below.

- `go build ./cmd/schema` — **PASS** (exit 0), before and after this repair.
- `go test -p 4 -count=1 -timeout 600s ./internal/ci/ -run 'HostileCorpus|HostileRefusal'` — **PASS** (`ok ... internal/ci 0.496s` at the continuation head; the combined run below re-confirms it after the repair).
- `go test -p 4 -count=1 -timeout 600s ./internal/codegen/dart/... ./internal/codegen/darttable/... ./internal/ci/ -run 'HostileCorpus|HostileRefusal'` — **PASS** before and after the repair (after: `ok internal/codegen/dart 0.109s [no tests to run]`; `ok internal/codegen/darttable 0.194s [no tests to run]`; `ok internal/ci 0.519s`; exit 0).
- Full generator leg, `go test -p 4 -count=1 -timeout 600s ./internal/codegen/dart/... ./internal/codegen/darttable/...`:
  - Base `a007c180ba4b` (checked in a scratch extraction with the real Dart SDK): `internal/codegen/darttable` **FAIL** on `TestFixedClassOnTableWireDart` and `TestMapsAndUnionArmsOnTheTableWireDart` (`wide`, `messages`, `scalars`) with `resaved 10 bytes, oracle <N>, first difference at byte 0` due to `sca-dart-5` (exit 1). This failure is the base's own; it is recorded here and deliberately not fixed in this report-only audit.
  - After this repair (both manifest-path rewrites reverted, files byte-identical to base `a007c180ba4b` for those hunks): `internal/codegen/dart` **PASS** (`ok 0.143s`); `internal/codegen/darttable` **FAIL** on the same two tests with the same `resaved 10 bytes, oracle <N>, first difference at byte 0` signature (exit 1) — the base red above, unchanged. At the superseded continuation head `348c65145`, which carried the now-reverted rewrite, both tests passed (`ok internal/codegen/darttable 1.078s`); that green was the rewrite's, and it is withdrawn with the rewrite.
- TLC: **NOT RUN**. This repair adds no TLA+ models; the announcement bounds models remain `PATHS-PROPOSED` above and no bench TLC run was available for them here.
