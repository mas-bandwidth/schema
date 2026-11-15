# The Python port: the design

The request to support Python, recorded 2026-11-03, starts this design. This
page is the Python leg’s design. Every layer of the leg is built to
it, and a layer that needs to depart from it changes this page first. The
layers, bottom up, and what secures each are in
[plans/python-leg.md](plans/python-leg.md); the roadmap work-set is
`port/python` in `docs/roadmap.sexp`, one task per layer.

The leg is built to the one-read contract from its first line (the strict-only
spec, PR #2790):

- **One read.** A read returns the whole value or refuses, with no partial
  value. The leg has no tolerant read.
- **The verdict is the cross-language contract.** An input is accepted, and
  its value written back is byte-identical to the canonical wire, or it is
  refused. Refusal reasons, counters and sizes are Python's own diagnostics
  and are never compared across languages.
- **Writes are deterministic.** The binary and JSON writers produce one
  encoding per value.

## THE DECISIONS

The plan’s eight decisions were answered on 2026-11-03: “Yes, as
recommended.”

| | decision | ruling |
|---|---|---|
| D1 | what runs | **Pure Python, standard library only.** In the packet layer the generator emits two code shapes for one type (straight-line code with locals bound, and a descriptor walk) and the leg keeps whichever measures faster. A mypyc build of the same source is a later add-on, shipped only if a benchmark justifies it. No C extension, no Cython |
| D2 | which Python | **3.11 is the floor**, tested on 3.11 and on the newest stable CPython. Generated code and the runtime use nothing newer than 3.11 |
| D3 | the toolchain | **CPython built from python.org source, pinned by sha256, into `dist/`**, as the Lua and Squirrel legs build theirs. The analyzer is mypy, installed from a hash-locked list. No system interpreter anywhere |
| D4 | the bit layer | **serialize.py**, a serialize-family sibling at format 2.0 |
| D5 | packaging | **The runtime ships beside the generated code.** Nothing to install. Publishing to PyPI remains a separate, later decision. |
| D6 | errors | **A read returns the value or raises `Refused`** (a `ValueError`), with no partial value. **A write raises `InvalidValue`** (a `ValueError`). Enums are plain ints |
| D7 | the JSON reader | **A port of the engine's reader** (`internal/tabletext`), never Python's `json` module |
| D8 | the release | **Python does not block v3.0.0.** It is held to the same bar as every leg, on its own schedule |

## THE PIN

Two interpreters and one analyzer, all under `dist/` (gitignored), none found
on `PATH`. `make dist-python` builds them; `make/python-toolchain.mk` holds the
pins.

| pin | what | from |
|---|---|---|
| `PYTHON_FLOOR` | CPython 3.11.18, the newest 3.11 release | `Python-3.11.18.tgz`, sha256 `5f62711b12470fd435f83f1451c1b1ef4a179aebc715c716874b0967567d814e` |
| `PYTHON` | CPython 3.15.2, the newest stable release at pin time | `Python-3.15.2.tgz`, sha256 `f74e1db0eabd651cd01e4e2a00201bd41866abe74869ab716f9bf33cbbaa0943` |
| `MYPY` | mypy 1.18.2, in the floor interpreter | `make/python-mypy.lock`, one sha256 per wheel |

The sums are the ones python.org publishes for each tarball, and the download
is checked against them before anything is unpacked: any other sha256 is
refused by name. Each interpreter is `./configure --prefix=dist/python-<v>`
and `make`, with no optimization flags. Profile-guided optimization is a later
decision, made by measurement. The two build in parallel; on a 32-core Apple
Silicon machine both take 73 seconds together. The leg needs `struct`, `sys`,
`os` and `math`, none of which depends on an optional system library. The
build needs a C compiler and the zlib headers: pip unpacks the analyzer's
wheels through `zipfile`, which needs CPython's `zlib` module. Modules that
need other system libraries (`ssl`, `sqlite3`, `tkinter`) are built where the
host has their headers, and nothing in the leg imports them.

**mypy 1.18.2** is the newest mypy whose dependencies all ship pure-Python
wheels: on PyPI, mypy 1.19.0 requires `librt` and 2.0.0 also requires
`ast-serialize`, and neither publishes a `py3-none-any` wheel. The lock names
the `py3-none-any` wheel of each package (`mypy-1.18.2-py3-none-any.whl`, not
mypy's compiled platform wheels), and a pure-Python wheel has one sha256 on
every OS, so the lock needs one line per package. pip installs the lock offline (`--no-index --require-hashes`) into the floor interpreter. mypy checks code as Python 3.11
(`--strict --python-version 3.11`), because 3.11 is the language the leg
promises.

Every interpreter the leg starts runs **isolated** (`-I`): no `PYTHON*`
environment variable, no user site directory, and no script directory on
`sys.path`.

The gates over the pin are `make toolchain-python` (each pin resolves, or
`make test` refuses by the leg's name and the pin's), `make python-toolchain-check` (each interpreter's own `sys.version_info` is its
pinned release, mypy's own `--version` is the lock's, and no file of the leg
starts an interpreter from `PATH`), and the controls of both.

## THE VALUE MAPPING

The rule: **a Python value is the schema value, exactly. Integers of every
width are `int`, checked against their declared range on write; floats are
`float`; text is `str`; bytes read as `bytes`. A value of the wrong Python type
or outside its contract is refused on write, never coerced.**

Python's `int` is arbitrary precision, so the 64-bit and 128-bit kinds need no
lanes and no wrapping helpers: a `uint64` is a non-negative `int` below 2^64,
an `int128` is an `int` in [-2^127, 2^127). The range check on write is what
holds a value to its width.

### Packet types (SPEC §4)

| schema | Python value | born as |
|---|---|---|
| `bool` | `bool` | `False`, or the default |
| `int8` … `int64`, `uint8` … `uint64`, ranged or bare, `timestamp` | `int` | `0`, or the default |
| `int128`, `uint128` | `int` | `0`, or the default |
| `bits(N)` | `int` in [0, 2^N) | `0` |
| `fixed(I, F)`, `ufixed(I, F)` | `int`: the raw scaled integer, as every leg stores it | the default's raw value |
| `float32`, raw or compressed | `float` holding a value a float32 represents exactly | the default rounded to float32 |
| `float64` | `float` | `0.0`, or the default |
| an enum `E` | `int` | the default variant's number, else `0` |
| a `flags` set `F` | `int`: the uint64 mask | `0`, or the default mask |
| `string(N)` | `str` | `""`, or the default |
| `bytes(N)` | `bytes` on read; `bytes`, `bytearray` or a byte-format `memoryview` on write | `b""`, or the default |
| `[N]T` | `list` of exactly N | each element at its default |
| `[..N]T`, `[A..N]T` | `list`; its length is the count, in [A, N] | A elements at their defaults (SPEC §4.6) |
| a type `T` | an instance of the generated class `T` | `T()` |
| a union `U` | an instance of `U`: `type`, the arm number, and one attribute per payload arm | `type = 0`, every payload arm built |
| `const` | a module constant, the schema expression in a trailing comment | — |
| `reserved`, `align` | no storage | — |
| `if` / `else` branches (§4.4) | both sides' fields are stored; a successful read sets the untaken side to its zero values, not its defaults (SPEC §5) | both at their defaults |

**Enums are plain ints.** An enum is a class of `int` constants, one per
variant spelled as the schema spells it, from 1, beside `None = 0` and `Max`,
the spellings every leg uses (SPEC §4.2), with a name table. It is not an
`enum.IntEnum`, which is slower to construct and to compare than an `int` on a
hot path. A read refuses a value above `Max` (SPEC §4.2: no unnamed enum value
exists), so a plain `int` never holds one either. A flags set is a class of
mask constants with a name table, the same way.

**Classes.** Every type, table and union is a generated class with
`__slots__`, so an attribute that is not a field is an `AttributeError`, not a
silent new attribute. `__init__` builds every field at its default (SPEC §5),
every nested value and union arm its own object, never one object shared by two
slots. Generated classes define `__eq__` and `__repr__`, and are not hashable.
`__eq__` compares floats by their bits, so `-0.0` and `0.0` differ, as their
bytes on the wire do.

### Tables (SPEC-TABLES §2)

A table has the same value shape as a type; the rows below are what tables
add.

| schema | Python value | born as |
|---|---|---|
| a table by value | an instance of its class | `T()` |
| `*T` | an instance, or `None` | `None` |
| `*bytes`, `*string` | `bytes` or `str`, or `None` | `None` |
| `?T` | the value, or `None` for absent | `None` |
| `[]T`, text lists | `list` | `[]` |
| `[E]T` (§2.4) | the generated keyed-array class over a `list` of exactly `E.Max`, the element for variant k at index k − 1; its `[k]` refuses `None` and any key above `Max` with `KeyError`, as §2.4 requires of every port, so a plain negative index can never reach the last slot | each element at its default |
| `map[K]V` | `dict`; the save writes it sorted by key | `{}` |
| a union with table arms | as a packet union | `type = 0` |

Each table class also carries its **descriptor**, the table as data
(SPEC-TABLES §8.1), as a class attribute built once at import.

## THE NUMBER MODEL

- **Integers.** `int` everywhere. A write checks `type(v) is int` (a `bool` is
  an `int` subclass and is refused where an integer is declared) and the
  declared range, and raises `InvalidValue` naming the field when either fails.
- **Booleans.** A `bool` field takes `type(v) is bool`; `0` and `1` are
  refused.
- **Floats.** A float field takes `type(v) is float`; an `int` is refused, as a
  float where an integer belongs is.
- **float32.** Python has no float32. A float32 field holds the `float` that is
  exactly the float32 value. A write first refuses a value that is not finite
  (`math.isfinite`), then rounds it to float32 as Go's `float32(v)` does
  (`struct.pack("<f")`: IEEE round to nearest, ties to even). A finite value
  beyond the float32 range makes `struct.pack` raise `OverflowError`, which is
  not a `ValueError`, so the writer catches it and raises `InvalidValue`. No
  write refuses a double for having more precision than a float32: it rounds,
  as every leg's writer does.
- **Compressed float32.** The quantization (the float32 arithmetic of SPEC
  §4.3's step index) is serialize.py's `serialize_compressed_float`, held to the
  family corpus. Generated code never quantizes on its own.
- **Finite only.** A NaN or an infinity is refused on write as `InvalidValue`,
  and on read, by its raw bits, before any conversion, as `Refused`
  (`non_finite_float`). Negative zero and the subnormals ride bit for bit.
- **Bits.** A float's bits move through `struct` with explicit little-endian
  formats (`"<f"`, `"<d"`, `"<I"`, `"<Q"`), so no read or write consults the
  host's byte order.

## TEXT AND BYTES

- **Text** is `str`, UTF-8 on the wire, encoded and decoded strictly. A lone
  surrogate is refused: `str.encode("utf-8")` refuses it, so a write raises
  `InvalidValue`, and a read of an encoded surrogate refuses as `Refused`
  (`invalid_utf8`). A packet `string(N)` refuses an embedded NUL, and its bound
  is N bytes of UTF-8, not N characters.
- **Bytes.** A read returns `bytes`, which the caller owns, never a view that
  pins the input buffer. A write accepts `bytes`, `bytearray`, or a
  `memoryview` whose format is `"B"`; any other `memoryview` is refused rather
  than reinterpreted.

## ERRORS

```python
p = Snapshot.read(data)        # a packet: the value, or raises Refused
data = p.write()               # the bytes, or raises InvalidValue
v = Player.load(data)          # a table: the value, or raises Refused
data = v.save()                # the bytes, or raises InvalidValue
```

- A read takes `bytes`, `bytearray` or a `"B"`-format `memoryview`, and reads
  all of it: a packet read ends with serialize's `end`, so trailing bytes and a
  set padding bit refuse, and a table load refuses input past the record as its
  form defines.
- `Refused` and `InvalidValue` are `ValueError` subclasses, so code that
  catches `ValueError` catches both.
- `Refused.reason` is the refusal's name (`past_end`, `non_finite_float`, ...),
  a diagnostic. It is never compared across languages; only the verdict is.
- A refusal carries no value and does not touch the caller's objects. A read
  builds a new value and returns it only when the whole input has been read.
- A table load takes an optional `report=`, a `LoadReport` from the runtime,
  which receives the load's counters (`unknown`, `widened`, `retained`,
  `retain_lost`) on success only. A refused load leaves it as it was. A packet
  read has no report: the packet wire has nothing to count.
- A writer validates the whole value before it writes a byte. `InvalidValue`
  names the field path and the rule.

Exceptions cost nothing on the success path from CPython 3.11 (zero-cost
`try`), which is part of why 3.11 is the floor.

## THE BIT LAYER: serialize.py

serialize.py is the serialize family's Python member at format 2.0: a port of
`serialize.h` to [STANDARD.md](https://github.com/mas-bandwidth/serialize/blob/main/STANDARD.md),
judged by the family corpus. It is staged in this tree at
`third_party/serialize.py/`, laid out as its own repository, until
`mas-bandwidth/serialize.py` exists. Creating that public repository is a later task.
When it exists, the directory moves there as it stands, is released, joins
COMPATIBILITY.md, and is vendored back into the leg runtime by sha256,
following the other language legs.

- **The surface** mirrors serialize.js in Python spelling: `WriteStream`,
  `ReadStream`, `MeasureStream`, one `serialize_<op>` per operation on the
  shared `Stream` surface, each scalar carried in a `Ref`.
- **A read refusal is a value.** A read returns `False`, leaves its `Ref`
  unwritten, and the stream keeps the first refusal's reason from the family's
  closed list (`refusal()`). Every later read refuses too. A reader never
  raises on any input. The generated codec turns the first `False` into
  `Refused(reason)`.
- **Write misuse raises** `WriteMisuse`, a `ValueError`, at the call and
  before any bit is written. Generated writers validate first, so a
  `WriteMisuse` reaching a caller is a generator bug; the codec re-raises it as
  `InvalidValue`.
- **The accumulator** is a Python `int` with no fixed width, which appends
  whole 64-bit words to a `bytearray`. There is no fixed word buffer and no
  reused index arithmetic, so the bit layer needs no layout model.

## THE RUNTIME LAYOUT

`schema generate --lang python --out <dir> <unit>` writes one Python package
per unit, named for the unit's package:

```
<dir>/<package>/__init__.py      re-exports the unit's types, Refused and InvalidValue
<dir>/<package>/<Base>.py        one module per schema file
<dir>/<package>/_schema/         the runtime: serialize.py and the table runtime
```

- **Relative imports only.** A module imports its siblings and the runtime as
  `from . import Wire` and `from ._schema import ...`, so the package works
  wherever the caller puts it, and two generated packages never share or shadow
  each other's runtime.
- **`Refused` per package.** Each package's runtime defines its own `Refused`
  and `InvalidValue`, re-exported as `<package>.Refused` and
  `<package>.InvalidValue`. The runtime is not a top-level `schema` package:
  that name belongs to an unrelated, widely installed project on PyPI. A caller
  handling several packages catches `ValueError`.
- **Module names.** A schema file's base name is its module name, so it must
  be a Python identifier and no keyword (`my-file` and `class` are refused by
  name). Two files whose names differ only in case are refused, because macOS
  and Windows fold case. A module named for the runtime (`_schema`) is refused.
  A module named like a standard library module (`json`, `types`) is legal: it
  lives inside the package and is reached only by relative import, so it
  shadows nothing, and the identifier tests hold that.

| file | what it is |
|---|---|
| `compiler/target_python.go` | the target, behind the `schema_leg_python` build tag until the packet surface has claims; the shared refusal chain, then the emitters |
| `compiler/carries_python.go` | the carried constructs, registered once from `pythonCarried`; a later layer adds `var _ = carryPython(<construct>)` in its own file |
| `internal/codegen/python` | the type surface's emitter (`Generate`), the identifier rules (`CheckNames`) and, from the packet layer, its codec |
| `internal/codegen/python/runtime/_schema/` | the runtime, embedded and emitted verbatim: the errors, and serialize.py vendored by sha256 (`runtime/SERIALIZE_PIN`, `make vendor-serialize-python`) |
| `internal/codegen/python/pyrun` | the test helper (THE HARNESS) |
| `internal/codegen/pythontable` | the table surface's emitter and codec, from the table layers (P5) |
| `internal/tablenames/python.go` | the table runtime's claimed names, from the table layers (P5) |
| `make/python-toolchain.mk`, `make/python.mk`, `make/python-*.mk` | the pin, the leg, and one fragment per gate |
| `test/conformance/python/` | the conformance driver and `ci.json` |

## THE TABLE FRAME

The table core (P5/P6, cut so the other table cards run beside it) emits a
generated class per table and per map entry, each with its descriptor
(SPEC-TABLES §8.1), and one runtime under `<package>/_schema/`.

- **One module per field kind.** `_schema/kinds.py` is the dispatch table;
  `kind_bool.py`, `kind_int.py`, `kind_map.py`, `kind_table.py` and
  `kind_pointer.py` register the kinds this cut carries. A later card adds its
  module and one registration
  line, never an edit to the body walk. `kind_unsupported.py` is the named
  placeholder for a construct whose descriptor exists but whose codec has not
  landed; a read or write that reaches it refuses by name.
- **The frame is form 1.** `load_table`, `save_table` and `measure_table` carry
  the whole value or raise `Refused`, validate a write before writing a byte
  and raise `InvalidValue`, write the flat node table for `*T`, and fill a
  `LoadReport` on success only. `Measure` runs the same walk into a counting
  sink, so measure and save agree by construction.
- **The shape is measured.** A table whose fields are all bools and integers
  gets the straight-line generated save/load pair, unrolled with each field
  descriptor bound to a local; every table, and every construct a later card
  adds, keeps the descriptor walk over `_schema/kinds.py`. The two shapes run
  on the same table, under both pinned interpreters, in
  `TestPythonTableCoreShapeBench` (internal/codegen/pythontable). The
  straight-line pair is kept: its save is faster on both pins, and its load is
  faster on the floor.
- **Maps are dicts, written sorted.** A `map[K]V` is a `dict` born empty; the
  save writes its entries in ascending key order (text by its UTF-8 bytes,
  integers by value), and the read refuses a descending or a repeated key as
  `Refused("wire_damaged")`, because no conforming writer emits either. A
  request that does not arrive in that order is not a map this leg accepts;
  the whole read refuses and returns nothing. `kind_map.py` owns the entry
  framing, the sort and the order check.
- **Scope.** The runtime carries bool, the full-width integers (8 to 128 bits),
  by-value tables, `*T` pointers to tables and `map[K]V` for the base key and
  value kinds, for both the fixed and the variable class. `*bytes`, `*string`,
  `?T`, `[]T`, `[E]T`, `?map[K]V` and unions with table arms have descriptors
  in this cut and refuse by name through `kind_unsupported.py` until their
  cards land.
- **`[E]T` rides through `kind_keyed.py`** (SPEC-TABLES §2.4, §3.2) for
  elements that are bools, integers, fixed-point or tables by value; any other
  element keeps `kind_unsupported.py`. The field's descriptor carries `elem`,
  the element's own descriptor, and `keys`, the key variants' wire ids in slot
  order. The write emits present slots in ascending variant order, elides a
  default slot (a table slot whose body is only its terminator) and writes no
  field when no slot is present; each slot's key id is used before its
  element's ids, and an elided slot's ids are taken back, so the id table is
  the engine's byte for byte. The read places each triple by its key's id,
  skips and counts a key it cannot name (checking a scalar slot's width), and
  refuses a key reference of 0 or past the id table, a repeated key, an
  element kind off the keyed list, a different element kind, and any byte left
  inside a slot's or the body's L. Slot order is not judged on read: the Go
  engine accepts any order, and the verdict is the engine's. A container kind
  that holds tables by value answers `tables()`, and `save.py` validates and
  numbers the pointers of those tables in place. The keyed array keeps the
  descriptor walk, as every construct after the frame does.
- **A table field's id is used before its body's.** The header rides first,
  so the id table's first-use order is the field's id, then the ids inside the
  body, as the Go engine writes it; a nested table holding fields saves to
  the engine's bytes.
- **A packet-typed field is refused by name.** A table field whose type is a
  packet `type` reaches `refusePacketValues` and is refused naming the field
  and the type, until P4 carries packet types into tables.
- **The compiler keeps the shared carry checks.** Before table dispatch it
  refuses constructs outside the registered carry set, just as the packet
  target does. The table emitter can describe future codec layers directly;
  those descriptors do not claim that the compiler carries their constructs.

### Bench

The shape bench runs a ten-field fixed-point table (the five storage widths,
signed and unsigned) under both pins, on the fleet host. Each figure is the
minimum of five rounds of 10,000 iterations after warm-up, in nanoseconds per
operation:

| operation | 3.15.2 generated | 3.15.2 descriptor | 3.11.18 generated | 3.11.18 descriptor |
|---|---:|---:|---:|---:|
| save | 11,560 | 12,332 | 10,277 | 12,544 |
| load | 12,700 | 12,455 | 13,590 | 14,159 |

The generated save is 6% faster on 3.15.2 and 18% faster on 3.11.18; the
generated load is 2% slower on 3.15.2 and 4% faster on 3.11.18. Save and load
together, the generated pair is 2% faster on 3.15.2 and 11% faster on 3.11.18,
so the pair is kept. `TestPythonTableCoreShapeBench` re-measures it and holds
both shapes to the same value.

### Maps bench

A table that holds a map is variable, so it never takes the straight-line
pair: its load and save are the descriptor walk. Inside the walk the map's
save has two shapes. The first builds an entry object per key and walks it
through `write_body`; the second writes each entry body straight from the key
and the value, the same bytes (the key, then the value, each elided at its
default, then the end mark) without the object. `TestPythonMapShapeBench`
(internal/codegen/pythontable) times both on a 64-entry `map[uint32]int32`
and a 16-entry `map[uint16]*Node`, and the load of each, under both pins.
Each figure is the minimum of five rounds of 2,000 iterations after warm-up,
taken over six sittings, in microseconds per operation. They were measured on
a shared fleet bench whose load average stood between 67 and 112 on 16 cores,
so single sittings swing by up to 2x; the minima are the figures to compare:

| operation | 3.15.2 | 3.11.18 |
|---|---:|---:|
| scalar map save, direct entry bodies (kept) | 141.1 | 135.9 |
| scalar map save, entry objects | 162.0 | 155.2 |
| pointer map save, direct entry bodies (kept) | 73.1 | 67.5 |
| pointer map save, entry objects | 75.6 | 75.8 |
| scalar map load | 184.3 | 202.0 |
| pointer map load | 82.7 | 88.2 |

The direct save is 13% faster on the scalar map under 3.15.2 and 12% under
3.11.18, and 3% and 11% faster on the pointer map, whose time goes mostly to
the node table; in 21 of the 24 comparisons across both pins it was the faster
of the two. `kind_map.py` writes the direct shape. The load reads each entry
through `read_body`, which holds the unknown-field skip, the repeated-field
and kind checks and the pointer fix-up in one place, so it has one shape.

## IDENTIFIERS

Fields keep the schema's spelling (`max_players`), and types, tables and
unions keep theirs (`PlayerState`). Nothing is mangled: SPEC §4.6's
target-name safety has every port refuse a reserved word, never escape it (Go's
escape aside), so **Python joins the checker's one reserved-word table**
(`internal/check`, `reservedLanguages`) with a row of its own: its hard
keywords (`keyword.kwlist`). The soft keywords (`match`, `case`, `type`,
`lazy`, `_`) are identifiers in every position the generated code spells a
name, so they are not in the row.

The row refuses those names in every unit, whatever the target, as the other
rows do, and nothing is escaped. That changes which schema names are legal: a
unit with a field named `pass`, `lambda` or `def` is refused. The shared
corpus had one such name, `PackConfig.global` in `tables/examples/Pack.schema`;
it is `settings`, declared `| was = "global"`, so its wire id is unchanged and
its JSON key is `settings`. An enum or union variant spelled `None` draws only
the implicit-variant rule's refusal (SPEC §4.2, §4.8), not a second one from
the row.

Then, by rule, in the Python target:

- **A name starting with `__`** is refused by name: Python mangles `__x` inside
  a class body and reserves `__x__`.
- **A declaration named like a Python builtin** (`int`, `list`, `bytes`,
  `object`, ... : every name in `builtins` of either pinned interpreter) is
  refused by name. A declaration is a module-level name, and the generated
  module names builtins unqualified. Fields are attributes and shadow nothing,
  so a field named `list` or `range` is legal.
- **A field named for a generated member** (`read`, `write`, `load`, `save`,
  `descriptor`) is refused by name. The emitter holds the list
  (`generatedMembers`); the table layers claim their runtime's names in
  `internal/tablenames/python.go`, as every backend does.
- **Enum variants** spelled `None` or `Max` are already refused by the checker
  (they are every enum's own names); a variant otherwise keeps its spelling.
- **The package name** must be a Python identifier, no keyword, and no module
  in the standard library of either pinned interpreter (the union of
  `sys.stdlib_module_names` of 3.11 and of the newest pin, so a module 3.13
  removed still counts), so that `import <package>` never resolves to the
  standard library.
- **Generated helpers** are named with the `_s_` prefix, and the Python target
  refuses a schema name that starts with it, so no schema name collides with
  a helper.

## THE HARNESS

Tests generate into `t.TempDir()`, run the pinned interpreter there, and read
answers from stdout. **Inputs go in as files beside the program; answers come
out as lowercase hex lines.**

- **`pyrun.Run(t, dir, main, inputs)`** writes each input as `dir/<name>`, runs
  `$PYTHON -I -B <main>` with `dir` as the working directory, and returns
  stdout. Under `-I` neither the script's directory nor the working directory
  is on `sys.path`, so the probe `main` first inserts its own directory
  (`sys.path.insert(0, os.path.dirname(os.path.abspath(__file__)))`) and then
  imports the generated package: the one path the test chose, and nothing from
  the environment. `pyrun.RunFloor` runs `$PYTHON_FLOOR` the same way, and every packet
  and table gate runs under both. An interpreter that does not resolve fails
  the test naming `make dist-python`; it never skips.
- **No real time, no sockets, no network, no outside service** in a unit
  test. Anything timed (the bench, a soak) is a functional test.
- Every test calls `t.Parallel()`.
- **The conformance driver** runs one interpreter per surface:
  `test/conformance/python/main.py` reads the manifest and its files and
  writes one answer file per case.
- **The analyzer gate** runs mypy in strict mode over every generated tree and
  the runtime. A finding is a failure.

## THE REGISTER

The `python` column of [PORTING.md](PORTING.md) holds every technique ❌,
citing #381 (the language list) and the `port/python` layer that carries it,
or `—` with the platform's reason:

- **M2 and M3**: every CPython `int` and `float` is a heap object, so there is
  no unboxed form to keep.
- **M4**: Python has no inlining directive or aliasing qualifier; M4b, the
  self-contained codec, is the shape (P4).
- **M10**: CPython owns allocation, and a pure-Python codec has no allocator to
  hook; the leg allocates inside the VM and says so. An allocation count over
  `tracemalloc` (I1) is the instrument a later layer can pin, as the Lua leg
  pinned its own.
- **M21**: retired by the finite-only rule, as for Lua: the float32
  conversion only ever sees a finite value, and `struct` converts every finite
  float32 exactly.

A cell becomes ✅ when the layer that carries it lands with its proof.

## THE CARD MAP

| layer | what | roadmap task |
|---|---|---|
| P0 | this page, and no Python code in the tree that does not follow it | `port/python/p0-design` |
| P1 | the pinned interpreters and analyzer, `toolchain-python` and its control | `port/python/p1-toolchain` |
| P2 | serialize.py at format 2.0, staged in this tree, judged by the family corpus under both interpreters and mypy | `port/python/p2-serialize` |
| P3 | the skeleton: the target behind its tag, the emitter packages, the register and roadmap columns | `port/python/p3-skeleton` |
| P4 | the packet wire against the C++ goldens; the build tag dropped | `port/python/p4-packet` |
| P5 | fixed tables, form 3 | `port/python/p5-fixed` |
| P6 | variable tables: the one read | `port/python/p6-variable` |
| P7 | the message form and retain-unknown | `port/python/p7-message-retain` |
| P8 | JSON, the hand-editable text and base64 | `port/python/p8-text` |
| P9 | block and cook accelerators, the unit view, descriptors | `port/python/p9-accel` |
| P10 | the conformance driver, every surface | `port/python/p10-conformance` |
| P11 | the wire-fuzz differential | `port/python/p11-wirefuzz` |
| P12 | the hostile corpus runner and resource bounds | `port/python/p12-hostile` |
| P13 | cards | `port/python/p13-cards` |
| P14 | the strict analyzer gate and every negative control | `port/python/p14-lint-controls` |
| P15 | CI, nightly fuzz, readiness, bench legs, release tier | `port/python/p15-ci` |
| P16 | USAGE fragments, per-language prose, the register complete | `port/python/p16-docs` |

Each layer is secured before the one above it starts: its gates green, each
with a negative control that goes red when the rule it guards is broken.

Where this page and [plans/python-leg.md](plans/python-leg.md) differ, this
page is the design as built. The pin lives in `make/python-toolchain.mk`, a
support fragment, because `make/python.mk` is the leg itself and the registry
probes every `make/<lang>.mk` as a `--lang` port; it arrives with the target
in P3. serialize.py is staged in `third_party/serialize.py/` and secured there
by the family corpus, because its own repository does not exist yet; the
runtime vendors it by sha256 from that directory (`SERIALIZE_PIN`), and from
its releases once the repository exists.

## THE PACKET CORE CUT

The packet layer (P4 above) lands in steps. The target carries the wire's core
today and refuses every other packet kind by name. This section records where
the core stands against the P4 row; the rules of this page are unchanged.

- **The class surface is complete; the codec is not.** Every packet
  declaration is emitted now: module constants, the enum and flags classes
  with their name tables, a class per type and per union with every field
  born at its default, `__eq__` by value (a float's bits included) and a
  readable `__repr__`. The codec carries `bool`, `int8` to `uint64` (bare or
  ranged), enums, flags, types by value, unions, and `if`/`else` branches. A
  field or item of any other kind (`bits`, `fixed`, `float32`, `float64`,
  `string`, `bytes`, an array, `const`, `reserved`, `align`, or a 128-bit
  integer) is refused at generate time by its kind, naming P4 the packet
  wire. The class surface knows how to birth and compare those kinds already;
  only their wire emitters are still to come.
- **The implicit enum zero is `None_`.** Python will not let a class body
  assign to `None`, so the zero every leg spells `None` is spelled `None_`
  in generated code. The name tables keep the schema spelling `"None"`, and
  a schema variant may not be named `None_` (it would replace the member).
  Flags keep their variant spelling; a flags set exports `Count` and a name
  table, and a variant may not be named `Count` or `NAMES`.
- **A union's tag attribute is `type`.** The value mapping's `type` is the
  attribute; a union arm may not be named `type`, `read`, `write`, `load`,
  `save` or `descriptor`, the members the generated class defines, or export
  `None_`, `Max` or `NAMES` on `<Union>Type`.
- **The golden harness reads single-root pins.** A C++-pinned packet golden
  that is one value with one root type is read and written back; the
  multi-root concatenations (`clauses`, `joins`, `degenerate`) and the table
  wire's pins are listed as skipped with the reason. Every packet root whose
  kinds are all carried is read from its pin and must write back byte for
  byte; a root blocked by an uncarried kind names that kind.

## THE PACKET CORE BENCH

D1 names two code shapes for the hot path: straight-line generated code with
locals bound, and a descriptor walk. Each method binds its reusable `Ref`
holders, `stream.serialize_*` callables and `type`/`int`/`bool` checks into
method locals once at entry, and validates scalar types and integer bounds
inline before constructing an error path. The core keeps the straight-line
shape, because it measured faster on both sides of the wire under both pinned
interpreters. The instrument is the `BenchInts` fixture (ten ranged `int32`
fields, 110 wire bits), read from `testdata/wire/bench_ints.bin` and written
back, 200,000 iterations after a 1,000-iteration warmup, over five interleaved
sittings per interpreter:

| interpreter | shape | read | write |
|---|---|---|---|
| CPython 3.11.18 (floor) | straight-line generated code (kept) | 7.33–7.48 µs/op | 6.80–7.03 µs/op |
| CPython 3.11.18 (floor) | descriptor walk (prototype loop) | 7.80–8.10 µs/op | 7.51–7.81 µs/op |
| CPython 3.15.2 (current) | straight-line generated code (kept) | 7.68–8.00 µs/op | 7.02–7.11 µs/op |
| CPython 3.15.2 (current) | descriptor walk (prototype loop) | 8.28–8.61 µs/op | 7.56–7.95 µs/op |

The straight-line reader is 3–8% faster and the writer 7–11% faster across
every sitting on both pins, because attribute load and store on `__slots__`
avoid `getattr`/`setattr` and loop dispatch, so the generator emits it and no
descriptor walk ships.

## THE PACKET FLOATS

The codec now also carries `float32`, `float64`, the compressed `float32` and
`fixed(I, F)` / `ufixed(I, F)` (P4's floats, compressed-float and fixed-point
rows); the core cut's list of refused kinds above no longer includes them.
Each kind is its own emitter file (`kind_float.go`, `kind_fixed.go`)
registered in the core's kind table.

- **Reads go through the bit layer alone.** `serialize_float` and
  `serialize_double` refuse a non-finite pattern by its raw bits
  (`non_finite_float`); a compressed float is read by
  `serialize_compressed_float_precomputed`, so serialize.py's own quantizer
  dequantizes it and refuses an index in the bit headroom; a fixed point field
  is read by `serialize_fixed`, which refuses an offset past the raw span and
  reads a zero-bit range as its one value.
- **Writes validate first, inline.** `_s_validate` holds each float field to
  `type(v) is float` and a chained range test against the largest finite
  value of its width (a compressed float: its declared `[min, max]` rounded
  to float32). A value that passes is finite and in range, so the common case
  never calls out; one that fails goes to `validate_float32`,
  `validate_float64` or `validate_compressed_float` in the runtime, which
  decide exactly (a double just above the largest float32 that rounds down to
  it is accepted and rides as that float32) and raise `InvalidValue` naming
  the field. A fixed point field is an `int` held to `[min << F, max << F]`.
- **The quantizer is the runtime's.** The generator folds only the
  declaration's constants (the step count, the wire width, the float32
  `delta` and `min`) into the `_precomputed` call; the runtime checks the
  tuple against the declaration's shape on every call and does every float32
  operation of the step index itself.

D1's two shapes were measured for the float hot path on a fleet bench (Linux
x86_64, a shared bench under other load), on a type of four `float32`, two
`float64` and two compressed `float32` fields, 100,000 reads and writes after
a 1,000-iteration warmup, five interleaved sittings per interpreter, in
µs/op as minimum and median over the sittings:

| interpreter | shape | read min / median | write min / median |
|---|---|---|---|
| CPython 3.11.18 | inline check, precomputed compressed (kept) | 23.8 / 27.0 | 29.2 / 30.7 |
| CPython 3.11.18 | inline check, declaration compressed | 25.6 / 26.8 | 28.9 / 31.5 |
| CPython 3.11.18 | runtime check on every field | 24.9 / 25.3 | 33.5 / 35.2 |
| CPython 3.15.2 | inline check, precomputed compressed (kept) | 24.4 / 26.0 | 29.4 / 30.2 |
| CPython 3.15.2 | inline check, declaration compressed | 24.3 / 26.3 | 30.3 / 32.2 |
| CPython 3.15.2 | runtime check on every field | 24.2 / 25.3 | 34.3 / 36.6 |

The inline check writes 15–21% faster than a runtime call per field on both
pins; reads do not validate, so the read columns differ only by noise. The
precomputed compressed call writes 3–7% faster at the median than the
declaration call, which re-derives the step count from `(min, max,
resolution)` on every field. The generator emits the kept shape.

The gates are `TestPythonPacketFloats` (fixture schemas of its own: every
float kind alone and beside `bool` and `uint8`, its wire laid out
independently in Go from SPEC §4.3 and written byte for byte; every
out-of-contract write refused naming the field; every truncated or hostile
input refused; the C++-pinned goldens whose kinds are all carried, today
`compressed_probe`, `rigidbody_at_rest` and `rigidbody_moving`), and
`packet-finite-python` (test/packet-finite/python/main.py under both pins),
with `packet-finite-python-negative-control`. A golden that also needs a kind
another packet card carries stays on the harness's skip list, named by that
kind.
