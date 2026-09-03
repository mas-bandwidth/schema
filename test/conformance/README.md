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

**Seven negative controls stand behind it, and each localises a different
thing** — a harness that has never gone red is watching nothing, and one that
goes red everywhere localises nothing:

| control | what it breaks | what must go red | what must stay green |
|---|---|---|---|
| `conformance-negative-control` | one byte of a C++ dump | `cpp / json-write` | `wire` |
| `conformance-negative-control-block-dump` | one byte INSIDE A ROW of the block image | `block-dump` | `block`, `forgery` |
| `conformance-negative-control-cs` | the C# walker, in the emitter | `cs / json-read` | `json-write` |
| `conformance-negative-control-go` | the Go leg, in the emitter | its own surface | the rest |
| `conformance-negative-control-go-walk` | the Go walk's field offset | `go / json-read` | `json-write` |
| `conformance-negative-control-java` | the Java walk's field index | `java / json-read` | `json-write` |
| `conformance-negative-control-java-block` | the array PITCH CHECK in the Java `open` | `java / forgery` | `block`, `block-dump` |

Two of those rows are the reason two surfaces exist at all. The row-dump one is
why `block-dump` is separate from `block`: `Open` reads the prologue and the
triples and nothing else, so a byte inside a row cannot move its answer. And the
pitch one is why `forgery` is separate from both: it removes a CHECK rather than
moving a value, so the reader still READS correctly and has stopped REFUSING —
which no valid image can show you.

## The shape

A driver is a **command**, not a binary — so a leg can be assembled from what a
backend already has rather than from a second copy of it. Some registered
drivers are shell scripts that dispatch: the C++ one answers the table and block
surfaces from `build/conformance-cpp` and hands the two COOK surfaces to
`build/schema_test_cook`, which already opens that unit and already runs that
battery. Others are one binary that answers everything — see "Registering a
language" below, where both shapes are stated.

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
| `cook-foreign` | `cook` | byte-swap the file's MAGIC word, Open, `open\n` or `refuse\n` | `refuse\n` |
| `block` | `block` | Open the image, `open\n` or `refuse\n` | `open\n` |
| `block-foreign` | `block` | byte-swap the image's MAGIC word, Open, `open\n` or `refuse\n` | `refuse\n` |
| `block-dump` | `block` | Open the image, the canonical ROW dump | `block/<name>.dump` |
| `forgery` | `forgery` (`block`) | Open the forged file at the claimed extent, `open\n` or `refuse\n` | the verdict in the manifest |
| `cook-forgery` | `forgery` (`cook`) | the same, over the cook battery's 111 | the verdict in the manifest |

`wire` and `json-read` write a file named by the instance; `json-write` writes
`<instance>.json`; the others write a file named by the case.

**The two forgery surfaces are one shape and two KINDS**, split so the matrix
can say which reader a backend has: a leg with a block reader and no cook reader
prints `absent` on one and a verdict on the other, rather than one blaming the
other.

**THE TWO FOREIGN SURFACES ARE THE CROSS-ENDIAN REFUSAL, and they are the one
answer a leg can give on ANY host.** A block and a cook are produced in the byte
order of the build that wrote them (§19.1, §7), so a reader of the other order
must REFUSE — and the check that does it is the magic, read first in the
machine's own order precisely so a foreign file stops there. Every other
accelerator surface has a host-dependent expectation and a big-endian leg can
only mark them absent; these two do not, because THE DRIVER MAKES THE FILE
FOREIGN TO ITSELF: it reverses the eight bytes at offset 0 — the magic — and
opens the result. Whatever this build's order is, the magic it now reads is not
this build's, and the verdict is `refuse` for every leg on every host.

It is one word and no builder: the driver reads eight bytes, reverses them,
writes them back, and opens. A leg that has the reader can answer it, which is
what makes it a REFUSAL a big-endian leg reports green rather than an ABSENCE
it reports as a missing feature.

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

The registry today:

```
cpp  test/conformance/cpp/driver
cs   test/conformance/cs/driver
go   test/conformance/go/driver
rust test/conformance/rust/driver
java test/conformance/java/driver
```

The first driver in the registry is the **reference leg**: `make conformance-pin`
takes the cook dumps, the block row dumps and both forgery batteries' offsets
from it. That is C++ and it is the repo's standing convention — C++ writes the
pins, every other leg compares. The two batteries print their manifest rows on
stdout rather than editing the manifest: a manifest that rewrites itself is a
manifest nobody reviews.

**A leg may be assembled from more than one binary, and it may be one.** The
C++ and C# drivers dispatch the cook's dump to a binary each backend already
had; the Go driver is a single binary that answers every surface in process,
which is why its `cook` surface costs one exec rather than five, and the Java
driver is one class on one classpath for the same reason — its generated units,
the wire ones and the block and pointered ones, are packages of a single
classpath, so the cook's node dump and the 111-row cook forgery battery ride
inside processes that were already starting. Both shapes satisfy the contract,
because the contract is a COMMAND.

## The budget

`make conformance` runs under the two-minute rule (#320). Measured on arm64
macOS, everything already built, median of three, **one sitting on an otherwise
quiet laptop** — the whole table re-measured together, because a table whose
rows come from different sittings can say the whole costs less than one of its
parts:

| leg | wall |
|---|---|
| all five, 268 cases per leg | 13.8 s |
| `go` alone | 0.57 s |
| `rust` alone | 0.59 s |
| `cpp` alone | 0.65 s |
| `java` alone | 2.2 s |
| `cs` alone | 11.7 s |

**The cost is per-PROCESS, not per-case, and the numbers say so plainly.** The
battery grew from 80 cases per leg to 268 — the cook's 111 forgeries, the 66
hostile trees, a sixth cook, two block row dumps and the two FOREIGN surfaces —
and the three NATIVE legs still answer all 268 each in under a second. The C#
leg starts a runtime once per surface plus once per cook, because
`test/cs-cook`'s dump takes one root per invocation, and that is where nearly
the whole wall is: of the 1,340 cases in this table, the 804 the native legs
answer cost under two seconds between them, the 268 Java answers cost two, and
the last 268 cost twelve.

**Three native legs cost under two seconds together.** Adding the two
foreign surfaces cost each native leg two more execs of a binary that starts in
milliseconds, and cost the C# leg two more runtime starts — which is the whole
shape of this table in one edit. A leg that answers every surface in ONE binary
is the cheapest shape the contract allows, and `cook` and `cook-forgery`
answered in the same binary as everything else rather than delegated is what
makes it so; the C++ and C# legs cost what they do because each was assembled
from a binary that already existed, which the contract exists to allow.

**The two MANAGED legs differ by seven times, and the difference is the SHAPE
rather than the runtime.** Both start a process per surface; the C# leg starts
one more per cook, because `test/cs-cook`'s dump takes one root per invocation,
and the Java leg starts none, because its generated units — the wire ones, the
block unit and the pointered unit — are packages of a single classpath, so both
cook surfaces ride inside processes that were already starting. Twelve JVM
starts is 2.2 s; twelve runtime starts plus six more is 11.7 s. A managed leg
pays for its process starts and nothing else, so the number of them is the
whole design decision.

**So what the remaining languages cost depends on the SHAPE each leg takes
rather than on the corpus**, and the two shapes measured here differ by twenty
times. A managed leg costs roughly 0.9 s per process start, times the twelve
surfaces plus one per cook. Nine legs of the native shape is about 8 s; nine of
the managed shape is about 150 s, which is past the two-minute rule on its own.
So the rule holds comfortably while ports take the cheap shape, and the leg
that does not is the one to shard — per language leg, the way the type wire's
nine legs already are. Not needed at this size, and the number to watch is the
per-start cost of the managed legs, not the case count.

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
