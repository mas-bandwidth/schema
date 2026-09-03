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

Three negative controls stand behind it, and each localises a different thing:
`conformance-negative-control` flips a byte of a C++ dump (a wrong ANSWER),
`conformance-negative-control-cs` breaks the C# walker in the emitter (a wrong
WALK), and `conformance-negative-control-block-dump` flips a byte INSIDE A ROW
of the block image — which `Open` cannot see, so `block` stays green while
`block-dump` goes red. That last one is the whole reason `block-dump` exists.

## The shape

A driver is a **command**, not a binary — so a leg can be assembled from what a
backend already has rather than from a second copy of it. Both registered
drivers are shell scripts that dispatch: the C++ one answers the table and block
surfaces from `build/conformance-cpp` and hands the two COOK surfaces to
`build/schema_test_cook`, which already opens that unit and already runs that
battery.

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
same way would be telling a port to implement nothing in particular. The C#
leg is the worked example: it registered with `json-read` and `json-write`
ABSENT, and both cells moved to `pass 18/18` when the C# walk landed — nothing
in the data or in this contract moved with them.

## The surfaces

One process per surface, so a runtime starts once rather than once per case.

| surface | for each | the driver writes | the harness compares against |
|---|---|---|---|
| `wire` | `instance` | Load the wire file, Save, the bytes | the wire golden |
| `report` | `report` | Load the wire file, the report as `u,k,c,d,m\n` | `reports.txt` |
| `json-read` | `instance` | FromJson `json/<name>.json`, Save, the bytes | the wire golden |
| `json-write` | `instance` | Load the wire file, ToJson, the text, as `<name>.json` | `json/<name>.json` |
| `json-hostile` | `json-hostile` | FromJson `<tree>/<root>.json`, the report as `u,k,c,d,m\n`, or `refused\n` | the verdict in the manifest |
| `cook` | `cook` | Open the cook, the canonical node dump | `cook/<case>.dump` |
| `block` | `block` | Open the image, `open\n` or `refuse\n` | `open\n` |
| `block-dump` | `block` | Open the image, the canonical ROW dump | `block/<name>.dump` |
| `forgery` | `forgery` (`block`) | Open the forged file at the claimed extent, `open\n` or `refuse\n` | the verdict in the manifest |
| `cook-forgery` | `forgery` (`cook`) | the same, over the cook battery's 111 | the verdict in the manifest |

`wire` and `json-read` write a file named by the instance; `json-write` writes
`<instance>.json`; the others write a file named by the case.

**The two forgery surfaces are one shape and two KINDS**, split so the matrix
can say which reader a backend has: a leg with a block reader and no cook reader
prints `absent` on one and a verdict on the other, rather than one blaming the
other.

**A forgery line carries an EXTENT and a POINTER**, and neither is a fact a file
can hold. The extent is the length the caller CLAIMS: larger than the file — two
rows of the block battery are about exactly that — or shorter, which is what a
truncation is. The pointer is the buffer that caller holds: `0` an aligned base,
`1`..`63` that many bytes past one, `null` no buffer at all. A driver allocates
EXACTLY the claim, places the base as the pointer column says, copies what fits
and zeroes the rest. `-1` as the extent means the file's own length.

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
takes the cook dumps, the block row dumps and both forgery batteries' offsets
from it. That is C++ and it is the repo's standing convention — C++ writes the
pins, every other leg compares. The two batteries print their manifest rows on
stdout rather than editing the manifest: a manifest that rewrites itself is a
manifest nobody reviews.

**A leg may be assembled from more than one binary, and it may be one.** The
C++ and C# drivers dispatch the cook's dump to a binary each backend already
had; the Go driver is a single binary that answers every surface in process,
which is why its `cook` surface costs one exec rather than five. Both shapes
satisfy the contract, because the contract is a COMMAND.

## The budget

`make conformance` runs under the two-minute rule (#320). Measured on arm64
macOS, everything already built, median of three:

| leg | wall |
|---|---|
| all three, 260 cases per leg | 10.65 s |
| `cpp` alone | 0.48 s |
| `rust` alone | 0.60 s |
| `cs` alone | 7.36 s |

The cost is per-PROCESS, not per-case, and the numbers say so plainly. The
battery grew from 80 cases per leg to 260 — the cook's 111 forgeries, the 66
hostile trees, a sixth cook, two block row dumps — and the wall went from about
5 s to about 7 s for two legs. The C++ leg is a handful of native execs and
answers all 260 in under half a second; the Rust leg is ten native execs and
answers them in six tenths, its cook node dump and block row dump in the same
binary; the C# leg starts a runtime once per surface plus once per cook,
because `test/cs-cook`'s dump takes one root per invocation, and that is where
nearly the whole wall is. Everything this round added rides inside a process
that was already starting.

**A third leg cost three seconds, and none of it was the cases.** The two
NATIVE legs together answer 520 cases in about a second; the rest of the wall
is one runtime's start-ups. That is the shape the budget projection assumed and
it is now measured rather than assumed.

**The Go leg is ONE process per surface and no more**, because its `cook` and
`cook-forgery` are answered in the same binary as everything else rather than
delegated: ten execs of a native binary, about a second for all 260 cases. It
is the cheapest shape a leg can have under this contract, and the C++ and C#
legs cost what they do because each was assembled from a binary that already
existed — which the contract exists to allow.

So the data can grow a great deal before it matters, and the budget left for
six more languages is most of the two minutes. Nine languages each starting a
runtime per surface lands near 20 s. Sharding per language leg — the way the
type wire's nine legs already are — is what the numbers say to do if that stops
holding; it is not needed at this size.

## What is not here yet

Named, with the reason, so a port knows what it is not being asked for:

- **The FIXED-root cooks** (`Settings`, `Stamp`). `test/cookgen` writes a root
  followed by a chain, so a root nothing points from has no fixture here, and
  the two fixed roots' value crossing stays where it is — in
  `test/tables/cook_main.cpp`'s `fixedvalues` mode and its C# twin, which read a
  cook the C++ side wrote the wire for.
- **The block form's fuzzers** (`test/tables/block_fuzz_main.cpp` and its C#
  twin) and the cook's. A fuzzer is a search, not a case, and the finds it
  produces land here as forgery rows — `block_offset_overflow` is one.
