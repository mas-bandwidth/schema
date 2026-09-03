# The tables conformance harness — the driver contract

A port of the tables layer is **"make the driver pass"**.

The data lives under `testdata/conformance/tables` and names no language
(`FORMAT.md` there states every file shape). This page states what a language's
DRIVER must do. Registering a port is one line in `drivers.txt` plus one driver;
nothing else moves.

```
make conformance                     run every registered driver, print the matrix
make conformance-generate            rewrite the generated half of the data
make conformance-pin                 rewrite the half the reference leg writes
make conformance-negative-control    prove the harness can go red
```

## The shape

A driver is a **command**, not a binary — so a leg can be assembled from what a
backend already has rather than from a second copy of it. Both registered
drivers are shell scripts that dispatch: the C++ one answers the table surfaces
from `build/conformance-cpp` and hands the cook's node dump to
`build/schema_test_cook`, which already produces it.

```
<command> <manifest> list
<command> <manifest> <surface> <outdir>
```

- **The working directory is the repository root.** Every path in the manifest
  is repo-relative, so a driver never resolves one itself.
- **`<manifest>` is a DERIVED manifest** the harness writes into `build/`: the
  committed one with the materialised fixture paths folded in and **every
  expected answer removed**. A driver cannot pass by reading the answer.
- **`list`** prints the surfaces this backend implements, one per line, to
  stdout.
- **A surface run** writes ONE FILE PER CASE into `<outdir>`, named by the case,
  and prints nothing that matters. The harness holds the expectations and does
  the comparing.

### Exit codes

| code | meaning |
|---|---|
| 0 | the surface ran |
| 2 | this backend does not implement it — the matrix prints ABSENT |
| anything else | the driver failed; the matrix prints FAIL and the harness prints stderr |

**Absent is not failure, and the distinction is the whole reason the matrix
exists.** A backend with no text form is missing a FEATURE; a backend whose text
form writes the wrong bytes is failing a TEST. A harness that printed both the
same way would be telling a port to implement nothing in particular.

## The surfaces

One process per surface, so a runtime starts once rather than once per case.

| surface | for each | the driver writes | the harness compares against |
|---|---|---|---|
| `wire` | `instance` | Load the wire file, Save, the bytes | the wire golden |
| `report` | `report` | Load the wire file, the report as `u,k,c,d,m\n` | `reports.txt` |
| `json-read` | `instance` | FromJson `json/<name>.json`, Save, the bytes | the wire golden |
| `json-write` | `instance` | Load the wire file, ToJson, the text, as `<name>.json` | `json/<name>.json` |
| `cook` | `cook` | Open the cook, the canonical node dump | `cook/<root>.dump` |
| `block` | `block` | Open the image, `open\n` or `refuse\n` | `open\n` |
| `forgery` | `forgery` | Open the forged file at the claimed extent, `open\n` or `refuse\n` | the verdict in the manifest |

`wire` and `json-read` write a file named by the instance; `json-write` writes
`<instance>.json`; the others write a file named by the case.

**A forgery line carries an EXTENT**, which is the length the caller claims and
may be larger than the file: two rows of the block battery are about exactly
that, and a file alone cannot hold it. `-1` means the file's own length. A
driver allocates the claim and copies the file into it.

### Why there is no `dump-read`

The contract asks a driver to write a dump and never to parse one. `wire` proves
the writer against the reader; `json-write` proves the reader's VALUES against a
text a third implementation wrote; `json-read` proves the writer against an
instance the driver did not build. A dump parser would add a component to every
port and cover nothing those three do not. Where a backend has no text form, the
gap is real and the matrix says so rather than a parser papering over it.

## Registering a language

1. Write a driver that answers `list` and the surfaces the backend has.
2. Add `<language> <command>` to `drivers.txt`.
3. `make conformance`.

The first driver in the registry is the **reference leg**: `make conformance-pin`
takes the cook dumps and the block forgery offsets from it. That is C++ and it
is the repo's standing convention — C++ writes the pins, every other leg
compares.

## The budget

`make conformance` runs under the two-minute rule (#320). Measured on arm64
macOS at the landing, everything already built, median of three:

| leg | wall |
|---|---|
| both, 80 cases | 3.96 s |
| `cpp` alone | 0.36 s |
| `cs` alone | 4.00 s |

The cost is per-PROCESS, not per-case: the C++ leg is eight native execs at
~40 ms total, and the C# leg starts a runtime eleven times — `list`, four
surfaces, and one per cook, because `test/cs-cook`'s dump takes one root per
invocation.

So the data can grow a great deal before it matters, and the budget left for
seven more languages is nearly the whole two minutes. Nine languages each
starting a runtime per surface lands near 20 s. Sharding per language leg — the
way the type wire's nine legs already are — is what the numbers say to do if
that stops holding; it is not needed at this size.

## What is not here yet

Named, with the reason, so a port knows what it is not being asked for:

- **The cook forgery battery as data.** `test/tables/cook_main.cpp` runs 111
  forgeries and `test/cs-cook` runs the same 111; the block battery's eleven are
  extracted here and the cook's are not. Most of them are patches this format
  already carries; the arm that is not is the one placing the file at an
  UNALIGNED base, which is a pointer fact rather than a file fact and needs a
  column of its own. It rides with the first port that has a cook reader.
- **The block ROW dump.** A block's rows are read through the typed accessors in
  each leg, and a generic row dump needs projection offsets in the descriptors,
  which no backend emits today (SPEC-TABLES.md §19.2 describes them). It is an
  emitter change and this harness does not own the emitters.
- **The JSON hostile battery.** 67 cases already live as data at
  `tables/pack/hostile-values/`, with a manifest carrying the expected report of
  each, and `test/tables/hostile_main.cpp` is already a data-driven driver over
  them. Folding that battery into this harness's surfaces is a move, not a
  build.
- **The FIXED-root cooks** (`Settings`, `Stamp`). Their fixtures are written by
  the C++ leg's own builder rather than by `test/cookgen`, so the harness cannot
  materialise them without coupling itself to one backend.
