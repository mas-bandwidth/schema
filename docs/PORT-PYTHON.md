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
| `wstring(N)` | `str`, at most N UTF-16 code units | `""` (a `wstring` takes no default) |
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
- **Wide text** (`wstring(N)`) is `str` whose UTF-16 encoding is at most N code
  units. A write refuses a lone surrogate and an interior zero code unit (SPEC
  §4.12), and a read refuses both.
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
| `internal/codegen/python` | the type surface's emitter and, from the packet layer, its codec |
| `internal/codegen/pythontable` | the table surface's emitter and, from the table layers, its codec |
| `internal/codegen/python/pyrun` | the test helper (THE HARNESS) |
| `internal/tablenames/python.go` | the runtime's claimed names |
| `make/python-toolchain.mk`, `make/python.mk`, `make/python-*.mk` | the pin, the leg, and one fragment per gate |
| `test/conformance/python/` | the conformance driver and `ci.json` |

## IDENTIFIERS

Fields keep the schema's spelling (`max_players`), and types, tables and
unions keep theirs (`PlayerState`). Nothing is mangled: SPEC §4.6's
target-name safety has every port refuse a reserved word, never escape it (Go's
escape aside), so **Python joins the checker's one reserved-word table**
(`internal/check`, `reservedLanguages`) with a row of its own: its keywords
(`False None True and as assert async await break class continue def del elif else except finally for from global if import in is lambda nonlocal not or pass raise return try while with yield`) and its soft keywords (`match case type _`).

The row refuses those names in every unit, whatever the target, as the other
rows do. That changes which schema names are legal: a unit with a field named
`pass`, `lambda` or `def` that checks today is refused once the row lands (P3).
Then, by rule, in the Python target:

- **A name starting with `__`** is refused by name: Python mangles `__x` inside
  a class body and reserves `__x__`.
- **A declaration named like a Python builtin** (`int`, `list`, `bytes`,
  `object`, ... : every name in `builtins` of either pinned interpreter) is
  refused by name. A declaration is a module-level name, and the generated
  module names builtins unqualified. Fields are attributes and shadow nothing,
  so a field named `list` or `range` is legal.
- **A field named for a generated member** (`read`, `write`, `load`, `save`,
  `descriptor`, ...) is refused by name. The members are claimed in
  `internal/tablenames/python.go`, as every backend claims its runtime's names.
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

The `python` column of [PORTING.md](PORTING.md) starts with every technique
❌, each citing the `port/python` layer that carries it, or `—` with the
platform's reason. Already known to be `—`: M3, because every CPython `float`
is a heap object; M4, because Python has no inlining directive or aliasing
qualifier, so M4b (the self-contained codec) is the shape the leg carries.
M10 is the managed-backend case: the leg allocates inside CPython and says
so, and an allocation count over `tracemalloc` (I1) is the instrument a later
layer can pin, as the Lua leg pinned its own. A cell becomes ✅ when the layer
that carries it lands with its proof.

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
