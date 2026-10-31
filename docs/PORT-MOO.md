# The moo port: the design

moo is the deterministic fork of Luau that Glenn's games script in: Virtual Go's
game state is moo, and the point of the language is that every machine computes
the same bits. Its one number is **Q48.16 fixed point** (a raw int64 underneath,
exact for every integer below 2^47); it has no float, no double and no 64-bit
integer, and its VM hashes, collects and formats deterministically. This page is
the port's design. The runtime is [serialize.moo](https://github.com/mas-bandwidth/serialize.moo)
v2.1.2, vendored verbatim; the language is [mas-bandwidth/moo](https://github.com/mas-bandwidth/moo)
v1.3.0, pinned by commit.

## The model it was written from

The generator is modelled on the **Luau** port (`internal/codegen/luau` on the
`dev` branch), not on Lua or Goose. moo *is* Luau with a different number, so
the Luau port already had the right module shape (one `--!strict` module per
schema file, `require("./x")` loading, holders `{ value = ... }`, a
`buffer` per byte field, a Go-hosted harness for a CLI with no `io`), and
serialize.moo follows serialize.luau's API name for name. Lua 5.4 has native
64-bit integers and no analyzer; Goose is a compiled systems language. What
moo does not share with Luau is the value domain, and that is where this port
is its own (THE VALUE MAPPING). The emitter was written fresh against main's
bar (enum `Max` beside the frozen variant table, no `Count`; finite floats;
`Init` and `Zero` beside `New`; `MaxBits` and `MaxBytes`), borrowing Luau's
shape rather than its code.

## THE VALUE MAPPING

The rule: **a moo number holds every value of 32 bits or fewer, and a fixed
point value whose format is moo's own; anything wider is serialize.moo's frozen
wide class; a float is its IEEE bit pattern.**

| schema | moo value | why |
|---|---|---|
| `bool` | `boolean` | |
| `int8` … `int32`, `uint8` … `uint32`, `bits(N ≤ 32)`, an enum, a union tag, a count | `number` | exact: every 32-bit integer is a moo integer |
| `int64`, `timestamp` | `serialize.Int64` | moo has no 64-bit integer; the class is frozen, so a constant is shared, never copied |
| `uint64`, `bits(33..64)`, a `flags` set | `serialize.Uint64` | flags store uint64 in every target (SPEC §4.2); masks combine with `Uint64.bor` |
| `int128` / `uint128` | `serialize.Int128` / `serialize.Uint128` | |
| `fixed(I, 16)` / `ufixed(I, 16)` of 32 or 64 storage bits, bounds within ±(2^47 − 1) | **`number`, in whole units** | moo's own Q48.16: `x fixed(48, 16)` is `v.X = 45.5`, the game-state type, exact both ways (`serializeFixedNumber`) |
| any other `fixed` / `ufixed` | the raw scaled integer: `number` at ≤ 32 bits, `Int64` / `Uint64` at 64, `Int128` / `Uint128` at 128 | the format does not fit Q48.16, so the port does not pretend it does |
| `float32` (raw or compressed) | `number` holding the **binary32 pattern** | SPEC §4.3: every finite float, -0 and the subnormals, bit exact. No moo number holds them; the pattern does. serialize.moo's Bits forms are moo's primary float API |
| `float64` | `serialize.Uint64` holding the **binary64 pattern** | as `float32` |
| `string(N)` | `string` | moo's own text; the write checks UTF-8 and the zero byte, the read refuses them |
| `bytes(N)` | `buffer` of N, and `<Name>Length` | the storage principle: allocated once |
| `wstring(N)` | `buffer` of 2N (UTF-16LE code units), and `<Name>Length` in units | |
| `[N]uint8` | `buffer` of N, 0-based | |
| `[N]T`, `[..N]T` | a 1-based array of N, and `<Name>Count` for a counted one | the count is born at its minimum (SPEC §4.6) |
| a type, a union | a table with every field present from construction; a union `{ Type, <arms> }` | |

Floats never become moo numbers in generated code. Game code that needs one
crosses the boundary on purpose (`serialize.Float32.toNumber`,
`Float32.fromNumber`), which is the boundary moo's design names; the schema
records that game state holds are `fixed` and integer fields, and those are moo
numbers natively. A float `const` is folded to the nearest Q48.16 value (moo's
literal rule, round to nearest, ties to even), written exactly, with the
original value in a trailing comment where they differ; a constant moo cannot
hold is refused by name at generation.

Fields are named by `ir.GoExportName` (PascalCase, Luau's idiom, and the Go
spelling, so the checker's collision claims cover moo). Every module exports a
moo `type` per type and union, so a script is checked against the schema by
`moo-analyze`.

## THE WIRE

`Write<Name>(stream, value)` and `Read<Name>(stream, value)` per packet type
and union, against serialize.moo's streams; there is no private bit writer.
`New<Name>()`, `Init<Name>(value)` (fresh construction values, in place) and
`Zero<Name>(value)` (the §5 zero form) beside them, and `<Name>MaxBits`,
`<Name>MaxBytes` from `ir`.

- **The error idiom is §6.3's bool early-out.** A value outside its contract (a
  wrong storage type, an integer out of range, a fraction where an integer
  goes, a non-finite float pattern, malformed text, a count past its bound)
  makes a write return `false` before any of it rides, unlatched, as the
  JavaScript leg does. serialize.moo traps such a write (IMPLEMENTING.md U9);
  the guards are what keep a trap for real misuse of the stream (a buffer
  smaller than `MaxBytes`).
- **A read refuses** through the stream's one latch, so `stream:refusal()` names
  the reason (`past_end`, `value_out_of_range`, `non_finite_float`,
  `embedded_nul`, `invalid_utf8`, `nonzero_padding`), or, for a rule only
  schema knows (`const`, `reserved`, the wstring rules of §4.12), by returning
  `false` unlatched. Hostile bytes never raise. A packet reader calls
  `stream:serializeEnd()` after the top-level read.
- **Constants are built once.** Wide literals and Float32 declarations live in a
  module-level pool (`K1`, `K2`, …) built at load; a compressed float's steps
  come from `serialize.compressedFloatParams` once per declaration, in the
  runtime's own float32 arithmetic, and ride the precomputed call.
- **The untaken side of an `if`** reads back to its zero form (SPEC §5); a union
  arm selected on read takes fresh declared defaults before its payload.

## LOADING

Every require a generated module makes is `"./<module>"`, a module emitted into
the same directory, the runtime included: never a bare name, never `../`,
never an `@alias`. moo has no `package`, `dofile`, `loadfile` or native loader.
A schema file whose module would collide with a runtime module (folding case)
is refused by name, and so is a require cycle between schema files.

## THE HARNESS

The moo CLI has no `io`. `internal/codegen/moo/moorun` writes inputs as the
module `inputs.moo` (buffer literals), runs the pinned `moo` in the generated
directory and reads stdout (hex lines). Every test that runs moo is behind
`SCHEMA_SLOW` (`internal/slowtest`), as every other leg's shelling-out tests
are; `make test-moo` and CI's emitter rows set it, and then a missing pin
FAILS, never skips. The pin is built by `make dist-moo` (moo ships no binary),
which refuses any other commit; `.github/actions/moo` caches the build by pin,
OS and architecture. No unit test uses real time, a socket or the network.

## THE GATES (layer 1)

| gate | test |
|---|---|
| the fourteen C++-pinned type-wire goldens, byte for byte | `TestMooTypeWireWritesTheGoldens`; its control `TestMooTypeWireWriteNegativeControl` (one bool doubled) |
| every single-instance golden read, `serializeEnd`, and rewritten byte for byte | `TestMooTypeWireReadsTheGoldens` |
| fixed point, ufixed, int128, uint128, degenerate ranges, hostile offsets | `TestMooLudicrousWire` (ludicrous_state, ludicrous_state_untargeted, degenerate_probe, unsigned_probe) |
| every truncation of every golden refused, none raising | `TestMooTypeWireRefusesTruncation` |
| hostile values refused for their reasons | `TestMooTypeWireRefusesHostileValues` |
| out-of-contract writes return false, never trap | `TestMooTypeWireWriteRefusesOutOfContract` |
| `moo-analyze` silent (no error, no lint warning) over every generated corpus | `TestMooGeneratedCodeIsAnalyzerClean`; its control `TestMooAnalyzerGateNegativeControl` |
| the runtime is serialize.moo v2.1.2 verbatim | `TestMooRuntimeIsTheVendoredPin` against `runtime/SERIALIZE_PIN` |

## THE TEXT FORMS (layer 4)

`runtime/json.moo` is ONE WALK over the table descriptors (docs/SPEC-TABLES.md
§8.1, §16), emitted byte-identical into every unit that carries json, and
held to the compiler's own engine (`internal/tabletext`) case for case. Each
table carrying json gets three calls: `<Name>FromJson(value, text, report?)`,
`<Name>ToJsonMeasure(value)` and `<Name>ToJson(value, out)`; the module also
hands the walk the two facts a descriptor lacks (a flags set's names, a text
list's element) through `json.extra`.

**No float on any path.** A binary32/binary64 pattern is spelled as C's
`%.*g` at the fewest digits that read back at the field's width, from the
value's exact decimal expansion rounded half to even; a token is read to the
nearest pattern in one rounding; the wide kinds and fixed point convert
exactly. All of it is integer arithmetic on 24-bit limbs, every limb
operation below 2^47, so no moo number saturates. The read-back the writer's
loop takes is the engine's own (a float32 is read through a double and
narrowed, two roundings, on purpose).

`runtime/textcarrier.moo` is §16.8's base64 carrier (canonical encode, the
`base64_not_canonical` / `base64_damaged` decode, and `saveText` / `loadText`
over a table's Save and strict read). It is not `text.moo` because
`tables/maps/Text.schema` claims that module name.

| gate | test |
|---|---|
| float writer and reader against the engine, both widths | `TestMooJsonFloatWriteMatchesEngine` (6002 patterns), `TestMooJsonFloatReadMatchesEngine` (1500 tokens); controls on the tie rule each way |
| every textual instance reads silent and writes back byte for byte | `TestMooJsonGoldensRoundTrip` |
| json-write on a value the reader did not build; json-read against it | `TestMooJsonWriteAndReadGates`; the walk control `TestMooJsonWalkNegativeControl` (json-read red, json-write green) |
| every json-hostile verdict | `TestMooJsonHostileVerdicts` |
| I13, the text differential | `TestMooJsonTextDifferential`, control `TestMooJsonTextDifferentialNegativeControl` |
| JSONTestSuite (§16.12) | `TestMooJSONTestSuite`, control `TestMooJSONTestSuiteNegativeControl` |
| one walk (M9) | `TestMooJsonOneWalk` |
| the base64 carrier (§16.8) | `TestMooTextCarrier`, control `TestMooTextCarrierNegativeControl`; the generated `<Name>SaveText` / `<Name>LoadText` over the moo wire, `TestMooTextCarrierSurface` |
| json-read and json-write over the moo table wire (the conformance surfaces) | `TestMooJsonWireSurfaces` |

## THE FUZZ DIFFERENTIAL (layer 6)

`internal/codegen/moo/fuzz_test.go` holds the generated moo codecs against the
Go target's, over every packet corpus the leg generates (the analyzer gate's
list: `examples`, `examples128`, `test/packet-*`), the way serialize.moo's
`fuzz/differential_test.go` holds the runtime against serialize.go. Each round
draws random instances of every packet type and union inside their declared
contracts (`fuzzvalue_test.go`), builds each one in both languages from the one
draw, and requires the two writers to emit identical bytes; then both readers
read the Go image and mutants of it (bit flips, overwritten bytes, truncation,
trailing bytes, padding bits, random images), each followed by
`serializeEnd`, and must agree on accept or refuse, on the latched refusal
reason, and on the bytes each writes back. Two reason differences are the
targets' and not the port's, and are not counted: a schema rule's refusal
(Go's `ErrValidation`, moo's unlatched `false`) names no runtime reason, and a
Go flat run reads its whole run before checking any value, so its `past_end`
may stand where moo, field by field, refuses the first bad value.

| gate | test / target |
|---|---|
| a seeded round, every root drawn, every refusal reached | `TestMooWireFuzzAgreesWithTheGoTarget`, `make moo-wire-fuzz` (in `make test-moo`) |
| a planted writer bug (an 8-bit field's top bit dropped) goes red | `TestMooWireFuzzWriteNegativeControl` |
| a planted reader bug (a union tag's bound dropped) goes red | `TestMooWireFuzzReadNegativeControl` |
| the long run: 100 rounds of 2000 instances a unit, seeded by the run id | `make schema-moo-nightly-fuzz`, `.github/workflows/schema-moo-nightly-fuzz.yml` |
| the nightly's wiring | `TestSchemaMooNightlyFuzzWiring` and its control |

THE TABLE-WIRE FUZZ is the harness's (`build/conformance-harness wire-fuzz`,
docs/SPEC-TABLES.md §4.2): its mutants and its oracle (`internal/tablewire`)
are the harness's own, and the leg is ONE process on a pipe,
`test/conformance/moo/wirefuzz.moo` under the native host `tools/moofuzzhost`
(the pinned CLI has no io library, so `make/moo-fuzz.mk` builds the host from
the pin's own source, the VM and compiler linked as the CLI links them).

| gate | test / target |
|---|---|
| the file form on the pipe; the roots the leg answers 0 for, pinned by name | `tables-moo-wire-fuzz-plain`, `test/conformance/moo/wire-fuzz-absent.txt` |
| the retaining pair (`--retain`) | `tables-moo-wire-fuzz-retain` |
| each mutant the leg diverged on, replayed alone | `tables-moo-wire-fuzz-pinned`, `test/conformance/moo/wire-fuzz-pinned.txt` |
| a string frame past its body not counted malformed goes red | `tables-moo-wire-fuzz-negative-control` |
| both arms on seeds 1-3 at the release count | `tables-wire-fuzz-release-moo` (make/wire-fuzz-release.mk), in `tables-moo-release` |

The shared hostile corpus (`internal/ci/hostilecorpus`) has no moo row yet.

## THE LAYERS

The port lands in layers, each green on main before the layer that depends on
it. Layer 1 is the foundation every other layer stands on; the layers above it
are independent of each other and run in parallel, each on its own branch
stacked on layer 1, landed one at a time against current main.

1. **The packet surface** (this page): the generator for the type wire, the
   harness, the toolchain and its CI step, the analyzer gate, the register
   column and the roadmap. The `schema_leg_moo` build tag stays on
   `compiler/target_moo.go` until the port joins the builtins.
2. **The packet gates**: moo legs of `test/packet-defaults`, `packet-finite`,
   `packet-text` and `packet-wide`, with their negative controls; the
   `generated/moo` tree and its goldens.
3. **Tables**: the fixed form and the variable wire (`internal/codegen/mootable`).
4. **The text forms**: JSON and text, both ways.
5. **The conformance driver** answering every surface the manifest asks.
6. **The hostile corpus and the fuzz differential** against the Go engine.
7. **Lint and negative controls**: the strict gate as a make target in
   `make/negative-controls.json`, every control registered and failing exactly
   its rows.

Every layer is built: the packet surface and its gates, the table surface
(the variable wire, the fixed, message, cook and block forms, retention), the
text forms, the conformance driver (every surface the manifest asks but
`framing`), the table-wire fuzz on both arms, and the strict analyzer over the
packet and table output with every control registered in
`make/negative-controls.json`. Each layer flips its cells in the register
(docs/PORTING.md, the `moo` column) and the roadmap (`port/moo/*`); the cells
still open there are named with what they owe. A runtime gap found on the way is fixed in
serialize.moo, never worked around here.

## RETAIN-UNKNOWN (layer 3, docs/SPEC-TABLES.md §6.6)

`runtime/table_retain.moo` carries the opt-in. The surface is the runtime's,
beside `ReadStrict` and `SaveMessages`, taking the root by name:

- `R.newStore(capacity, idCapacity)` is the caller's two stores, by capacity;
  `R.used(store)` and `R.ids(store)` read them back.
- `R.LoadRetain(root, bytes, report, store)` is the tolerant load into a
  region with the store on, `R.MeasureRetain(root, value, store)` and
  `R.SaveRetain(root, value, store, report)` write the retained fields back at
  the end of their own body. The report accumulates across the pair
  (`report.retained`, `report.retainLost`), `SaveRetain` refuses a nil report
  (`retain_needs_report`), and all three refuse a fixed-class root by name
  (`fixed_class_root`).
- `R.LoadRetainMessages(vocabulary, root, data, capacity, stores, report)` is a
  form-2 batch with a store a body; each body is saved as a FILE, the one
  direction retention crosses the forms.

A retained record is reader-private: the field's id and kind and its payload
with every reference resolved, lengths at eight bytes written after their
content, cost nine bytes plus the payload (the Go engine's own layout, so the
`short` capacity rows drop the same record). It belongs to the table instance
the load filled, so the walks that build a body also say what a later legal
occurrence supersedes: `table_load.moo`, `table_save.moo` and
`table_message.moo` carry hooks that are nil on every plain read and save. The
ids the unit can spell (`ir.TableWireIds`) are registered by every table
module (`retainform.go`), and decide which store an id takes at save.

Held by `retain_test.go` (the manifest's `retain` rows, the evolved pairs read
through the variable class, the pair's promises), `retain_occurrence_test.go`
(the occurrence rules against hand-built wires), `retain_message_test.go` (the
`retain-message` rows and every construct as sender, read by a reader that
names none of its fields), each against the Go engine's store bytes, counters
and saved bytes; each gate has a negative control.
