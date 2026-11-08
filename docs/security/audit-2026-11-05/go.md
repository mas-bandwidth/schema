# Go leg — guard audit, 2026-11-05 (card sca-go, report-only)

Scope audited at base `a007c180ba4b09c7d9d5404de6e072043e034777`: the Go packet
generator (`internal/codegen/golang/**`) and the readers it emits under
`generated/go/**` and `testdata/golden/go/**`; the Go table generator
(`internal/codegen/gotable/**`), whose runtime is Go source carried in generator
string literals and whose committed generated instance is
`internal/codegen/gotable/fuzzunit/` (pinned by `TestGoFuzzUnitIsCurrent`;
there is no `testdata/golden/tables/examples-go*` tree); the Go claimed-name
registry (`internal/tablenames/go.go`); `tables/goruntime/goruntime.go`; and
the Go hostile-corpus runner, conformance and fuzz harnesses
(`test/go-hostile/**`, `make/go.mk`, `make/schema-go-runs-hostile-corpus.mk`,
`test/conformance/go/**`, `test/go-tables/**`,
`internal/ci/hostilecorpus/corpus/**`). The `serialize.go` packet bitstream
runtime is out of scope (its own card); only what the generated code adds on
top of it and how it calls it is audited here. Cooked and block forms are the
trusted class (`docs/SECURITY.md`); their `Open` entry points are listed
because they validate caller-supplied buffers before dereferencing offsets.

Method (report-only ruling, coordinator 2026-11-05): code reading only. No
reproductions, no new hostile-input tests, no fuzz corpora and no proof inputs
were created, and no generator change was made, so every finding below is
**open** and every "pin" is named, not created. Findings marked *suspected*
follow a path read in the code that was not executed. Every `file:line` is at
the base commit and grep-verifiable; line numbers inside `gotable` refer to the
generator source line that carries the emitted text (the emitted runtime is
one-statement-per-line Go inside raw strings).

Verdicts: **bounded** = the cited check refuses or clamps before the hostile
value can drive an out-of-range index (a Go panic), an integer wrap, an
input-sized allocation, unbounded recursion or unbounded work; **open** = a
finding below owns the row; **trusted** = contents are trusted by the
documented threat model, only the header is checked.

## Entry-point table

### Packet wire (`internal/codegen/golang`)

158 `func Read*` functions are emitted across `generated/go/*.go` and
`testdata/golden/go/*.go`; the rows below cover every shape that emits them.

| # | Generated function (instance) | Untrusted input | Bound relied on (`file:line`) | Verdict |
|---|---|---|---|---|
| G1 | `Read<Struct>(*serialize.ReadStream, *<Struct>) error`, e.g. `generated/go/ArmDefaults.go:73` `ReadDefaultArm` | packet bitstream | Emitted at `internal/codegen/golang/functions.go:104`; items walked by `emitReadItems` (`functions.go:205`); every ranged read checks `stream.Err()` then the headroom (`functions.go:500`–`functions.go:503`); recursive types are refused by the checker (`internal/check/check.go:949`), so the nested `Read<T>` call (`functions.go:1097`, `functions.go:1099`) cannot recurse without bound | bounded (partial value on error: F-GO-15) |
| G2 | `Read<Union>`, e.g. `generated/go/ArmDefaults.go:179` `ReadDefaultChoice` | tag bits + arm bits | Tag refused above `Max` through `emitReadRangedFold32` (`functions.go:64`, headroom check `functions.go:502`); selected arm reset to `New<T>()` / `<T>{}` before its read (`functions.go:75`, `functions.go:77`, instance `generated/go/ArmDefaults.go:191`) | bounded |
| G3 | Flat word runs inside `Read<Struct>` (`emitFlatReadRun`) | up to `maxRunBits` packed bits | Emitted at `internal/codegen/golang/flat.go:330`; one `stream.Err()` check after the chunk reads (`flat.go:342`), then per-piece range guards `flatRangeGuard` (`flat.go:969`–`flat.go:975`) | bounded |
| G4 | Branch `if value.<cond> { … } else { … }` | guard field + branch bits | `internal/codegen/golang/functions.go:254`–`functions.go:270`: the untaken side is zeroed by `emitZeroItems` (`functions.go:327`) | bounded |
| G5 | `const(V, B)` and `reserved(B)` | wire constant / reserved bits | `functions.go:294` (`constValue != V` → `ErrValidation`) and `functions.go:320` (`reservedValue != 0` → `ErrValidation`), each after a `stream.Err()` check | bounded |
| G6 | `align` | padding bits | `functions.go:253` emits `stream.SerializeAlign()`, which refuses nonzero padding in the runtime (instance `generated/go/Wire.go:145`) | bounded (runtime) |
| G7 | Counted array `<field>Count` + element loop, e.g. `generated/go/Clauses.go:65` | wire count + elements | Count read through `emitReadRangedFold32` with headroom refusal into `[min, bound]` (`functions.go:931`–`functions.go:938`); loop limit is that count over fixed `[bound]T` storage (`functions.go:940`, `functions.go:674`–`functions.go:699`) | bounded |
| G8 | Fixed array `[N]T` loop and byte-aligned `[N]uint8` bulk read | elements / bytes | Compile-time `N` limit (`functions.go:924`–`functions.go:943`); bulk path `stream.SerializeBytes(name[:])` (`functions.go:927`) | bounded |
| G9 | `string(N)` | wire length + bytes | Length in `[0, N]` (`functions.go:1071`–`functions.go:1075`), bytes into `[N]byte` storage (`functions.go:1076`), `stream.Err()` first (`functions.go:1081`), then `utf8.Valid` and an interior-NUL loop (`functions.go:1083`–`functions.go:1086`, instance `generated/go/Clauses.go:1017`) | bounded |
| G10 | `bytes(N)` | wire length + bytes | Same length bound as G9 (`functions.go:1071`–`functions.go:1076`) | bounded |
| G11 | `wstring(N)` | wire length + 32-bit groups | `internal/codegen/golang/wstring.go:16`–`wstring.go:30`: length in `[0, N]`, `stream.Err()` per group, `group == 0 \|\| group > 0xFFFF` refused, unpaired or trailing surrogate refused | bounded |
| G12 | Ranged int (`int32`/`int64` folds and the wide `uint64` path), `bits(N)`, bare ints, `fixed`, `int128`/`uint128`, enum, compressed float, `float32`/`float64`, `bool`, flags | encoded scalar bits | Headroom refusal `offsetValue > diff` (`functions.go:502`, `functions.go:536`, `functions.go:1041`); enum tag `<= Max` (`functions.go:1089`–`functions.go:1093`); compressed float index `<= steps` (`functions.go:610`) and non-finite dequantization refused (`functions.go:638`, instance `generated/go/Types.go:454`); `fixed` and 128-bit ranges delegate to the runtime's range-checked calls (`functions.go:974`–`functions.go:986`, `functions.go:998`); flags read the declared wire width only (`functions.go:1131`–`functions.go:1144`, no member-mask check is emitted); raw `float32`/`float64` are passed to the runtime unchanged (`functions.go:1066`, `functions.go:1068`) | bounded (raw float specials are the runtime's) |

### Table wire, fixed form, cook and block (`internal/codegen/gotable`)

| # | Generated function / runtime | Untrusted input | Bound relied on (`file:line`) | Verdict |
|---|---|---|---|---|
| T1 | `tableOpen` / `tableOpenFramed` | whole file | `internal/codegen/gotable/wire.go:392`, `wire.go:445`: form byte, `len >= 9`, id count `n <= (len-9)/8` (`wire.go:399`) and `n <= 4096` (`wire.go:403`), distinct ids on fixed stack tables (`wire.go:420`) | bounded |
| T2 | `TableReader` primitives (`Leb`, `Has`, `Room`, `Body`, `Resolve`, `Skip`) | LEB, lengths, refs | `wire.go:279`–`wire.go:347`: `Has`/`Room` subtract in `int64`, canonical 10-byte LEB check (`wire.go:312`–`wire.go:315`), `Resolve` bounded by the id count; `Skip` is iterative | bounded |
| T3 | `<T>LoadBody`, fixed-class body | field TLVs | `internal/codegen/gotable/read.go:13`, loop `read.go:42`: `Resolve` and `Has(1)` before every `Get8` (`read.go:80`), reserved ids refused (`read.go:91`), text validated by `tableUTF8Valid` before clamping (`read.go:274`), arrays clamp to the bound (`read.go:357`) | open (F-GO-03, F-GO-05) |
| T4 | `<T>LoadTolerant` / `<T>Load` | file | `read.go:175`, `read.go:240`: T3 plus `Offset == len` after the root (`read.go:220`) and the early-end probe (`read.go:206`) | open (F-GO-03, F-GO-07) |
| T5 | Array, keyed array, union arm, scalar, wide and wstring reads | element bodies | `read.go:301`–`read.go:411` (keyed slot via `TableEnumValue`, so `int(slot)-1` is in range, `read.go:329`), `read.go:414`–`read.go:558` (`Void` arm requires an empty payload), raw-bit NaN/Inf refusal (`read.go:613`–`read.go:618`), `wide.go:28`, `wstring.go:5` (odd length, unpaired surrogate and zero unit refused) | bounded (F-GO-05) |
| T6 | `<T>LoadMeasure` / `<T>LoadMeasureReason` → `tableRegionMeasureReason` | variable-root file | `internal/codegen/gotable/regions.go:377`: strict framing probe, `tableWireExtent`, list `count <= MaxInt32` and `count <= remaining/floor` (`internal/codegen/gotable/lists.go:171`), blob size cap | open (F-GO-04) |
| T7 | `<T>LoadTolerant` (region) → `tableRegionLoad` | file + caller region | `regions.go:379`–`regions.go:388`: `n < 0`, `len(region) < n` and alignment checked before any write; directory sized from the same scan; `clear(region)` on malformed or non-finite | open (F-GO-04, F-GO-07) |
| T8 | `<T>LoadBuilderTolerant` / `<T>LoadBuilder` / `<T>ReadStrict` → `tableBuilderLoad` | file | `regions.go:438`–`regions.go:445`: directory `(records+1)*16` with records at most `len(wire)/2` (`regions.go:440`), blob `<= MaxUint32` (`regions.go:443`), arena capped at 4096 × 1 MiB segments (`regions.go:266`–`regions.go:268`), root reset on failure | open (F-GO-06, F-GO-07) |
| T9 | `tableCycleCheck` | decoded slot graph | `internal/codegen/gotable/cycle.go:10`: iterative Kahn walk, slot index `<= len(dir)` (`cycle.go:26`), in-degree overflow guard | bounded |
| T10 | List and map reads (`emitListRead`, `emitMapRead`, `tableContainerFill`) | element count + payload | `lists.go:32`, `internal/codegen/gotable/maps.go:87`: `count <= MaxInt32` (`lists.go:125`), storage carved inside the measured extent (`lists.go:124`) | open (F-GO-03, F-GO-05) |
| T11 | `<T>LoadRetain` → `tableRegionLoadRetain`, `capture` | file from a newer schema + caller retain buffers | `internal/codegen/gotable/retain.go:27`, `retain.go:137`: T7's checks, capture bounded by the caller's buffer, walk depth `tableRetainWalkDepthMax = 64` (`retain.go:189`, checked `retain.go:235`, `retain.go:269`) | open (F-GO-08, F-GO-09) |
| T12 | `tableIntegrityOpen` / `Holds` | CRC-32C trailer | `internal/codegen/gotable/integrity.go:17`, `integrity.go:27`: `len >= 5`, CRC over the prefix checked before decode (integrity only, not authentication) | bounded |
| T13 | `<T>FixedLoad` / `<T>FixedLoadTolerant` | fixed-form file | `internal/codegen/gotable/fixedform.go:1230`, `fixedform.go:1235`: form byte, reserved bytes zero (`fixedform.go:1276`), layout length in `int64` (`fixedform.go:1279`), lineage hash and byte-compare against the locked layout, `rest % record == 0`, `n <= len(values)`, a text and count pre-scan over the whole batch before any write (`fixedform.go:1339`–`fixedform.go:1344`), batch reset on a non-finite float (`fixedform.go:1369`–`fixedform.go:1371`) | open (F-GO-10) |
| T14 | `tableFixedRun` / `TextRefusal` / `ForgedBounds` | record bytes | `internal/codegen/gotable/fixedruntime.go:416`, `fixedruntime.go:580`, `fixedruntime.go:725`: plan offsets are build-time, record size `<= tableFixedRecordMaxBytes` (`fixedruntime.go:65`), counts, lengths and UTF-8/UTF-16 refused first | bounded |
| T15 | `<T>Cook.Open` | cooked region | `internal/codegen/gotable/cook.go:556`: magic, byte order, build version, reserved words, power-of-two alignment in `[8, 64]`, `dataLength <= bytes - dataOffset`, attribution equality, reset on error | trusted (header bounded; F-GO-18) |
| T16 | `<T>Block.Open` | block region | `internal/codegen/gotable/block.go:618`: per array stride equality, `count <= Max`, offset bounds, `rows = count*stride` bounded before use, tail padding | trusted (header bounded; F-GO-18) |

### JSON text, text form, message form and views (`internal/codegen/gotable`)

| # | Generated function / runtime | Untrusted input | Bound relied on (`file:line`) | Verdict |
|---|---|---|---|---|
| J1 | `<T>FromJSON` / `<T>FromJSONTolerant` / `<T>FromJSONWith` → `tableJSONRead` | JSON text | `internal/codegen/gotable/json.go:2485`, wrappers `json.go:92`–`json.go:150`: depth cap `tableJSONMaxDepth = 128` (`json.go:465`), key buffer 255 bytes (`json.go:467`), RFC 8259 number grammar (`json.go:1519`) with saturation (`json.go:1598`), trailing bytes after the root refused (`json.go:2497`), output reset on error (`json.go:2508`) | open (F-GO-02, F-GO-07, F-GO-16) |
| J2 | `scanText` (string values, keys, wstring) | JSON string | `json.go:1380`: UTF-8 validated, raw control bytes refused, `\u0000` into text storage refused (`json.go:1453`) | open (F-GO-02, F-GO-11) |
| J3 | `skipValue` / `skipContainer` | unknown members | `json.go:1643`, `json.go:1686`: same depth cap and number grammar, linear time | bounded |
| J4 | `tableJSONReadScalar` / `tableJSONReadField` (scalars, enums, unions, arrays, keyed arrays, base64 `bytes`) | values | `json.go:1732`, `json.go:2055`: exact-range clamps; array excess counted and skipped; base64 validated before storage (`json.go:2076`–`json.go:2160`) | open (F-GO-11) |
| J5 | `tableJSONReadWide` (128-bit, fixed point) | numbers | `internal/codegen/gotable/jsonwide.go:70`: exponent capped, `point` in `[-40, 40]`, digit buffer guarded, overflow saturates | bounded |
| J6 | `tableJSONReadPointer` / `ReadLabel` / `ReadBlob` / `DropLabel` (`&node` graph text) | labelled graph | `internal/codegen/gotable/jsongraph.go:38`–`jsongraph.go:70`: depth +1 per pointer, a reference to an open label refused (`jsongraph.go:52`), label ids overflow-checked (`jsongraph.go:40`), arena-capped | open (F-GO-17) |
| J7 | `tableJSONReadList` / `tableJSONReadMap` / `tableJSONReadText` / text-list element and array root | lists, maps, unbounded text | `lists.go:258`, `maps.go:146`, `internal/codegen/gotable/unboundedtext.go:190` (count, then alloc, then copy), `internal/codegen/gotable/textlist.go:206`, `textlist.go:231` | open (F-GO-11, F-GO-14, F-GO-17) |
| J8 | `<T>LoadTextForm` → `tableTextFormRead` | hand-edited text form | `internal/codegen/gotable/textform.go:1508`: 65,536 bytes, 8,192 tokens, 4,096 nodes, 65,536 alloc, depth 128 (`textform.go:186`–`textform.go:226`), lone surrogates refused (`textform.go:627`–`textform.go:638`), big-int magnitude capped before `big.Rat` (`textform.go:1138`) | bounded |
| M1 | `AnnounceRead` → `tableAnnounceRead` | vocabulary announcement | `internal/codegen/gotable/message.go:189`–`message.go:200`: second announcement refused (`message.go:189`), words length `<= MaxBytes` (default 64 KiB) before parse (`message.go:195`), entries `<= len(v.Entries)` (`message.go:200`), failure latches `Refused` | open (F-GO-01, F-GO-12, F-GO-13) |
| M2 | `tableMessageShapeRead` / `tableMessageEntryRead` | announced shapes | `message.go:223`–`message.go:232`: packing, kind and width checks; string `Max <= MaxInt32` (`message.go:228`); list `Min`, `Max <= MaxUint32`, `Min <= Max` (`message.go:229`) | open (F-GO-01, F-GO-12) |
| M3 | `tableMessageSkip` / `tableMessageSkipBody` | unknown or mismatched fields | `message.go:234`–`message.go:247`: depth 128, every skip bounds-checked, progress guard on the per-element skip loop | open (F-GO-12) |
| M4 | `<T>LoadMessageBody` | batch body bits | `internal/codegen/gotable/messageread.go:11`: announced kind must match the local kind or widen (`messageread.go:53`–`messageread.go:59`), string and wstring lengths checked against remaining bits, clamps counted | open (F-GO-01) |
| M5 | `<T>LoadMessages` / `<T>LoadMessagesTolerant` / batch open and close | batch | `messageread.go:35`–`messageread.go:50`: count `<= 256` and `count <= len(values)` before any body (`messageread.go:47`), zero pad bits and exact end on close, integrity CRC first, bodies reset on failure | bounded (except via F-GO-01) |
| M6 | `TableBitReader.Get` / `Skip` | bits | `tables/goruntime/goruntime.go:1930`, `goruntime.go:1959`: bound check subtracts `Offset`, no overflow | bounded |
| V1 | `view.go` unit views | none (static registry) | `internal/codegen/gotable/view.go:268` | not an input surface |

### Harness

| # | Harness | Input | Bound relied on | Verdict |
|---|---|---|---|---|
| H1 | Go hostile-corpus runner `test/go-hostile/runner.go.txt`, generated by `test/go-hostile/gen` | every `manifest.tsv` row | dispatch at `test/go-hostile/gen/main.go:116`–`main.go:146`; carriers `text` (via `FromJSONTolerant`), `wire` (`LoadTolerant` / `LoadMeasureReason`), `announce`, `message`, `message_novocab`; reports `0 skipped` | bounded (coverage gaps: F-GO-19) |

## Findings (all open; report-only — no fix attempted)

### F-GO-01 — Message-form typed array read can be driven through ~2^32 zero-bit iterations (suspected)
- Where: `internal/codegen/gotable/message.go:229` (`tableMessageShapeRead`, list `Min`/`Max` up to `MaxUint32`), `message.go:228` (string element `Max` may be `0`), `message.go:233` (`tableMessageCount` reads `bits(Max-Min)` bits and returns `raw+Min`), and the emitted loop at `internal/codegen/gotable/messageread.go:134` (`walk:=n;if element.Shape.Bits>=0{walk=keep}`).
- What untrusted input does: an announcement may declare, for a field whose local type is an array of `string` or `wstring`, a list shape with `Min = Max = 0xFFFFFFFF` and an element shape with `Max = 0`. The count then costs zero bits and decodes as 4,294,967,295; because a string element has `Bits < 0`, the loop walks every one of them rather than skipping past `keep`, and each zero-length element consumes no bits after the first alignment. A few bytes of announcement plus one batch body hold one reader in a ~4.3e9-iteration loop per such field, per body (up to 256 bodies per batch). Memory-safe (excess elements land in a stack scratch); availability only. The skip path for mismatched fields has a progress guard (`message.go:243`); the typed path does not. Whether every leg admits this announcement was not checked.
- Severity: **high** (suspected; unauthenticated CPU exhaustion on a network-facing reader).
- Suggested reduction: refuse in `tableAnnounceRead` any list shape whose announced `Min` exceeds the local field's bound (a count the reader can never store is damage at announcement time), and in the typed loop at `messageread.go:134` refuse (`Malformed`) an element read that does not advance `r.Bits.Offset` once `i >= keep`.
- Pin that would fix it: a `gotable` message-form unit test announcing that shape for a `[N]string` field and expecting a refusal; plus an `announce` row in the shared corpus and a `FuzzAnnounceRead` seed. Named only.

### F-GO-02 — JSON reader stores a lone UTF-16 surrogate escape as U+FFFD instead of refusing it
- Where: `internal/codegen/gotable/json.go:1444`–`json.go:1449` (`scanText`); shared by values, keys, skipped strings and the wide path.
- What untrusted input does: `"\ud800"` (or a low surrogate alone, or an out-of-order pair) is accepted and stored as U+FFFD under a clean report, so it is indistinguishable from a literal `"�"`. `docs/SPEC-TABLES.md:16100`–`SPEC-TABLES.md:16114` and `SPEC-TABLES.md:17111` require the refusal `string_lone_surrogate: <key>`; the Go text-form reader already refuses (`internal/codegen/gotable/textform.go:600`–`textform.go:625`). The shared hostile corpus currently records the accept verdict for these rows, sourced from the Go engine (`internal/tabletext/read.go:385`–`read.go:386`, which names a sibling card for this change).
- Severity: **medium** (cross-language verdict divergence on a SECURITY.md-listed refusal; memory-safe).
- Suggested reduction: in `scanText` refuse any unpaired surrogate with `Reason = "string_lone_surrogate: <key>"`, in lockstep with the engine and the corpus rows. This is strict-only work already in flight elsewhere; not redone here.
- Pin that would fix it: the existing lone-surrogate corpus rows flipped to `refuse` with the engine. Named only.
- PATHS-PROPOSED: internal/tabletext/read.go, internal/ci/hostilecorpus/corpus/manifest.tsv

### F-GO-03 — Strict wire `Load` keeps the last of a repeated field id, keyed slot or map key (suspected)
- Where: `internal/codegen/gotable/read.go:92`–`read.go:145` (no per-body seen set; only `required` fields are flagged, `read.go:35`), keyed slots `read.go:329`–`read.go:351`, map keys `internal/codegen/gotable/maps.go:102` (`Duplicate++`, merged into the prior entry); `TableReport.Err()` (`internal/codegen/gotable/refusal.go:141`–`refusal.go:196`) has no clause for `Duplicate`.
- What untrusted input does: a body that carries the same field, keyed slot or map key twice is accepted by the strict `Load` with the last occurrence winning. `docs/SPEC-TABLES.md:9829` lists a repeated field id or keyed slot as damage and `SPEC-TABLES.md:14901` lists last-wins as overturned, so a Go reader accepts what a strict leg refuses — a SECURITY.md cross-language divergence. The base's own red `TestJSONTestSuiteGenerated` (`y_object_duplicated_key.json`, now `refuse (dialect)` in `SPEC-TABLES.md:17163`) is the JSON face of the same rule.
- Severity: **medium** (suspected; verdict divergence, memory-safe).
- Suggested reduction: a per-body seen mask in `LoadBody`, the keyed loop and the map loop, mapped to `Malformed`; `Err()` refuses `Duplicate`. Strict-only work in flight elsewhere; not redone here.
- Pin that would fix it: a `gotable` test with a duplicated scalar id, keyed slot and map key, each expecting `Load == nil` and `LoadMeasure == -1`. Named only.

### F-GO-04 — A variable-root node record body is not required to end at its terminator (suspected)
- Where: `internal/codegen/gotable/regions.go:377` (strict measure calls `tableWireExtent` per node with no `body.Offset == len(body.Buffer)` check); loads at `regions.go:385` and `regions.go:445` ignore leftover bytes after `LoadBody`. The root (`regions.go:377` probe) and nested bodies (`lists.go:163`, `lists.go:179`) are covered.
- What untrusted input does: a node record whose body carries bytes after its `0` terminator is accepted, where the same bytes at the root are refused. `docs/SPEC-TABLES.md:4976` treats an early terminator as damage.
- Severity: **low** (acceptance gap; memory-safe).
- Suggested reduction: check the offset after `tableWireExtent` in the measure and after `LoadBody` in both loaders.
- Pin that would fix it: a node body with trailing bytes expecting `wire_damaged` from `LoadMeasure` and `Load`. Named only.

### F-GO-05 — Further strict acceptance gaps on the table wire
- Where: leftover bytes after the counted elements of an array, keyed array, list or map body are not checked (`internal/codegen/gotable/read.go:370`–`read.go:411`, `lists.go:48`–`lists.go:68`, `maps.go:102`–`maps.go:108`, `lists.go:173`); kind 32 with a nonzero length is skipped rather than refused (`internal/codegen/gotable/wire.go:367`); nesting deeper than 128 inside unknown bytes is skipped (iteratively, so safely) rather than refused.
- What untrusted input does: carries bytes the strict spec calls damage (`docs/SPEC-TABLES.md:9829`) through a clean load.
- Severity: **low** (acceptance gaps; memory-safe, no unbounded work).
- Suggested reduction: refuse each case as `Malformed` at the point it is seen.
- Pin that would fix it: one `gotable` refusal test per case. Named only.

### F-GO-06 — `<T>ReadStrict` builds from a wire the strict measure already refused
- Where: `internal/codegen/gotable/regions.go:248` (`size:=<T>LoadMeasure(wire);size=max(size,0)` then `<T>LoadBuilderTolerant` runs regardless).
- What untrusted input does: a small wire with broken framing and many minimal node records is still materialised into the builder arena (capped at 4096 MiB, `regions.go:266`–`regions.go:268`) before the strict error is returned.
- Severity: **low** (bounded amplification before refusal).
- Suggested reduction: return the measure's refusal when `size < 0`, before `initSized`.
- Pin that would fix it: a test asserting zero allocator calls for a measure-refused wire. Named only.

### F-GO-07 — Partial output is visible after some strict refusals
- Where: region `<T>Load` returns nil on `Err() != nil` without `clear(region)` (`internal/codegen/gotable/regions.go:243`); `<T>LoadBuilder` leaves the builder root populated on a strict refusal such as a clamp (`regions.go:245`); variable-table `<T>FromJSON` returns `ok && read.Err() == nil` with the tolerant result left in the builder root (`internal/codegen/gotable/json.go:110`), whereas the fixed-table strict read resets the value (`json.go:147`); fixed-class `LoadTolerant` keeps the decoded prefix on `BodyStopped` (`read.go:215`–`read.go:218`, documented at `read.go:81`–`read.go:89`).
- What untrusted input does: a caller that ignores the boolean or error sees the clamped or partially decoded value.
- Severity: **low** (API-contract hazard; memory-safe).
- Suggested reduction: reset the region, root or value in each strict wrapper when it refuses, as `json.go:147` already does.
- Pin that would fix it: per wrapper, a refused input followed by an assertion that the output is at its defaults. Named only.

### F-GO-08 — Retain walk depth is 64 where the spec states 128
- Where: `internal/codegen/gotable/retain.go:189` (`const tableRetainWalkDepthMax = 64`), against `docs/SPEC-TABLES.md:10233` (`retain_walk_depth value=128`); `SPEC-TABLES.md:8601` also adds a `binary_max_depth` marker with no Go limit entry.
- What untrusted input does: a retained unknown subtree nested 65–128 framed levels deep is refused by Go and accepted by a leg following the spec. Fail-closed, so not exploitable; a verdict divergence. Pinned red at base by `TestSpecLimitsMatchGo` (`internal/codegen/gotable/speclimits_test.go:427`).
- Severity: **low**.
- Suggested reduction: take the spec's value (or the spec takes Go's) and add the `binary_max_depth` entry to the Go limit table; strict-spec follow-up, not redone here.
- Pin that would fix it: the existing `TestSpecLimitsMatchGo`, green.

### F-GO-09 — Retained unknown floats are checked for non-finite values only at the top level
- Where: `internal/codegen/gotable/retain.go:215` (`capture` tests raw bits only for a top-level kind 10 or 11; the nested walk `tableRetainIn.payload`, `retain.go:244`, has no finite check).
- What untrusted input does: a NaN or infinity nested inside a retained kind 13, 14 or 16 record is kept and can be re-emitted by a later write, where the same value at the top level is refused.
- Severity: **low** (memory-safe; laundering of a refused value through retention).
- Suggested reduction: apply the raw-bit finite check during the nested capture walk.
- Pin that would fix it: a retain test with a nested non-finite float expecting `non_finite_float`. Named only.

### F-GO-10 — `uint32` wrap in the fixed-form layout slice bounds (theoretical)
- Where: `internal/codegen/gotable/fixedform.go:1280` and `fixedform.go:1323` (`TableFixedHeaderBytes+4+layoutBytes` with `layoutBytes` a `uint32` from `tableFixedGet32`, `internal/codegen/gotable/fixedruntime.go:388`); the guard at `fixedform.go:1279` is computed in `int64`.
- What untrusted input does: `layoutBytes >= 0xFFFFFFEC` in a fixed-form file of at least 4 GiB passes the `int64` guard, the slice bound wraps below the low bound, and the slice expression panics before the layout byte-compare.
- Severity: **low** (needs a ≥ 4 GiB input; a panic, not a memory error).
- Suggested reduction: compute both slice bounds as `int64(layoutBytes)+TableFixedHeaderBytes+4`.
- Pin that would fix it: an emitted-text test on the slice expression, or a sparse-buffer test. Named only.

### F-GO-11 — `int32` length counter in `scanText`, and input-sized allocation before the field bound
- Where: `placed := int32(0)` in `scanText` (`internal/codegen/gotable/json.go:1386`); consumers `internal/codegen/gotable/unboundedtext.go:190` and `internal/codegen/gotable/jsongraph.go:65`; base64 scratch `raw = make([]byte, rawLength)` (`json.go:2090`) before the `bytes(N)` bound applies.
- What untrusted input does: a decoded JSON string of 2 GiB or more wraps `placed`; most consumers then refuse through `Alloc(n <= 0)`, but a decoded length in `[2^32-8, 2^32-1]` would reach `unsafe.Slice(…, int(n))` with a bad length and panic. The base64 path allocates proportional to the text (not amplified) before refusing on the bound, which SECURITY.md lists as a thing to refuse before it drives an allocation.
- Severity: **low** (multi-GiB inputs only; no amplification).
- Suggested reduction: saturate `placed` at `MaxInt32` and refuse; cap `rawLength` at `ceil(N/3)*4 + 4` before allocating.
- Pin that would fix it: an emitted-text or allocation-count test. Named only.

### F-GO-12 — A nested-list message entry is skipped with a zero-width element shape (suspected)
- Where: `internal/codegen/gotable/message.go:232` (`tableMessageEntryRead` reads a list's element shape but, for an element that is itself a list, its inner element is not recorded) and `message.go:243` (the skip builds `TableMessageEntry{Shape: e.Element}` with a zero `Element`).
- What untrusted input does: an announced list-of-list field the reader does not know is skipped as zero width, desynchronising the bit stream so later fields decode wrongly or the batch is refused. Whether the schema can express such a field was not confirmed.
- Severity: **low** (suspected).
- Suggested reduction: refuse a list element shape of kind 14 or 16 at announcement time, or record and honour the inner shape.
- Pin that would fix it: an announced unknown list-of-list entry followed by a known field. Named only.

### F-GO-13 — `tableMessageCount` does not refuse a count above the announced `Max`
- Where: `internal/codegen/gotable/message.go:233` (returns `raw+Min` with `bits(Max-Min)` bits, so slack values above `Max` are possible).
- What untrusted input does: a count past the announced bound is clamped locally and counted (strict refuses as `clamped`), and skips are bounds-checked; a divergence only if announced bounds are binding.
- Severity: **low**.
- Suggested reduction: refuse `n > Max` in `tableMessageCount`.
- Pin that would fix it: a batch with count `Max+1` in the slack bits. Named only.

### F-GO-14 — A failed region JSON read leaves its allocations in the builder arena
- Where: `internal/codegen/gotable/regions.go:400`–`regions.go:403` (root reset; graph, list, blob and label allocations remain); arena cap `regions.go:266`–`regions.go:268`.
- What untrusted input does: a long-lived builder reused across hostile reads is driven to allocation failure.
- Severity: **low**.
- Suggested reduction: reset the arena on failure, or document builders as single-use.
- Pin that would fix it: repeated failed reads into one builder followed by a successful read. Named only.

### F-GO-15 — Packet `Read<T>` leaves a partially filled value on error
- Where: `internal/codegen/golang/functions.go:205`–`functions.go:272` (fields are assigned in wire order and the function returns at the first refusal), `functions.go:64`–`functions.go:79` (union `Type` set before the arm is read), `internal/codegen/golang/flat.go:330`–`flat.go:383` (a flat run assigns earlier pieces before a later piece's range guard).
- What untrusted input does: a caller that reuses the value after a non-nil error sees fields from a refused packet. The godoc (`functions.go:126`–`functions.go:128`) says the read validates and returns an error but not that the value is unspecified on error.
- Severity: **info** (Go convention; memory-safe).
- Suggested reduction: state in the emitted godoc that the value is unspecified when an error is returned.
- Pin that would fix it: a golden of the godoc line. Named only.

### F-GO-16 — Over-long JSON keys are truncated, not skipped as the comment says
- Where: `internal/codegen/gotable/json.go:466`–`json.go:467` (comment: a key over 255 bytes is skipped as unknown) against `json.go:1500` (key truncated to 255 bytes, `Clamped` incremented).
- What untrusted input does: a long unknown key makes strict report `clamped` instead of `unknown`, and a truncated key could match a field named with exactly 255 bytes.
- Severity: **info**.
- Suggested reduction: treat an over-long key as unknown and skip its value.
- Pin that would fix it: a 256-byte key whose first 255 bytes name a field. Named only.

### F-GO-17 — Amplification bounded only by the arena
- Where: JSON `{}` or `0,` sequences allocate one record or element per 2–3 bytes of text (`lists.go:258`, `maps.go:146`, `jsongraph.go:43`) with no per-text region cap like the file form's `LoadMeasure`; a list of unions has a 1-byte wire floor per element (`lists.go:171`), looser than the two bytes `docs/SECURITY.md` states; `tableOpen` zeroes ~72 KiB of stack for any trailer with 8 or more ids (`wire.go:420`–`wire.go:421`); a reference to a label defined inside a skipped key reads as null (`jsongraph.go:52`).
- Severity: **info** (bounded by the 4096 MiB arena cap).
- Suggested reduction: a per-read element budget proportional to the input, and the SECURITY.md floor corrected.
- Pin that would fix it: an allocation-count test. Named only.

### F-GO-18 — Cook and block contents are not validated per node (by design)
- Where: `internal/codegen/gotable/cook.go:281`–`cook.go:285`, `internal/codegen/gotable/block.go:618`.
- What it means: a hostile cooked region yields arbitrary self-relative dereference through the `At` accessors; documented trusted class (`docs/SECURITY.md`).
- Severity: **info**.

### F-GO-19 — Hostile-corpus and fuzz coverage gaps for the Go leg
- Where: `internal/ci/hostilecorpus/corpus/manifest.tsv` carries no `packet` and no `wire_retain` rows; the Go runner (`test/go-hostile/gen/main.go:116`–`main.go:146`) exercises `FromJSONTolerant` but not strict `FromJSON`, has no text-form carrier, and the 7 `announce`/`message*` rows hold no hostile shapes (F-GO-01, F-GO-12 uncovered). Fuzz targets exist for text, JSON, messages, open, load, strict read and fixed load (`internal/codegen/gotable/fuzzunit`), block and cook forgery (`test/go-tables/fuzz_test.go`), wire and retain (`test/conformance/go/wire_fuzz.go`, `test/conformance/go/retain_fuzz.go`); nothing fuzzes `AnnounceRead` or the text form, and `FuzzLoadMessages` uses only the unit's own announcement.
- Severity: **info** (adding rows and seeds is outside this report-only card).
- Suggested reduction: a `FuzzAnnounceRead` target, a fuzzed vocabulary in `FuzzLoadMessages`, and `packet`, `wire_retain` and strict-`text` rows in a future corpus card.

## Generator audit notes (no finding)

- **Identifier and path safety.** Schema identifiers are ASCII-only at the scanner (`internal/scanner/scanner.go:298`–`scanner.go:302`); the checker refuses names that shadow Go's universe block (`internal/check/check.go:3533`), the package name `main` (`check.go:313`) and union members that collide after Go export mapping (`check.go:1233`, `check.go:1254`); the Go table runtime's own names are claimed in `internal/tablenames/go.go`. Doc text is emitted only as `//` line comments, one per `ir.DocLines` line (`ir/doctags.go:22`–`doctags.go:34`, `internal/codegen/golang/golang.go:168`–`golang.go:181`), so it cannot end the comment. Every generated file is parsed back by `tidy` before it is returned (`golang.go:46`–`golang.go:49`), so malformed output fails the compile. Output file names derive from `f.Base` (`golang.go:50`); no import, module path or command is built from schema text.
- **Write side vs read side.** Packet writers refuse out-of-range values with `serialize.ErrValueOutOfRange` (`functions.go:39`–`functions.go:40`); every packet read bound (G1–G12) is unconditional.
- **No panic path found** in the packet readers: every slice expression is over fixed `[N]` storage with a length already refused into `[0, N]`.

## Gate at base (this card changes only this document)

- `go test -count=1 -timeout 600s ./internal/ci/ -run 'HostileCorpus|HostileRefusal'`: exit 0 (`TestSchemaSharedHostileCorpus`, `…Generate`, `…NegativeControl`, `…Sources`, `…Reasons`, `TestHostileRefusalReasonFailsClosed` pass; the Lua, Luau and Squirrel runner tests skip without their hosts).
- `go test -count=1 -timeout 600s ./internal/codegen/golang/...`: exit 0.
- `go test -count=1 -timeout 600s ./internal/codegen/gotable/...`: exit 1, red at the base commit with no change from this card. Three tests, all from the strict spec landed ahead of the Go implementation (strict-only work in flight elsewhere, not redone here):
  - `TestDamageDiscardsTheWholeLoadGo` — `discard_test.go:91: Root: Inner's body is not where this test expects it`
  - `TestJSONTestSuiteGenerated` — `jsontestsuite_test.go:108: … y_object_duplicated_key.json: verdict "refuse (dialect)" is not accept, accept (dialect) or refuse` (see F-GO-03)
  - `TestSpecLimitsMatchGo` — `speclimits_test.go:427: … binary_max_depth has no Go limit entry; … retain_walk_depth spec=128 (128) != go=64` (see F-GO-08)
- Not run: the leg's Linux hostile-corpus and fuzz make targets (`make/schema-go-runs-hostile-corpus.mk`), and TLC (no state machine or index arithmetic was changed, so no model was extended).
