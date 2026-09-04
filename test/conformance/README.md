# The tables conformance harness — the driver contract

A port of the tables layer is **"make the driver pass"** — and fill its column
on [docs/PORTING.md](../../docs/PORTING.md), the techniques register. The
register is read before the port brief is written; the port PR is not ready
until every technique on it is carried or cited in the port's column; and a
blind read of the port reports two lists against it — methods the port uses
that the register lacks, and methods the register has that the port lacks.

The data lives under `testdata/conformance/tables` and names no language
(`FORMAT.md` there states every file shape). This page states what a language's
DRIVER must do. Registering a port is one driver at `<lang>/driver`: the
harness discovers it, and nothing lists it.

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

**A PER-BACKEND WALK CONTROL, one per port with a text form**, because a
walker's break is the backend's and not the data's:
`conformance-negative-control-go-walk` breaks the Go walk's offset arithmetic
and `conformance-negative-control-elixir` breaks the Elixir walk's KEY, which is
what stands in for an offset in a language whose fields have none — the read
places every scalar under a name the instance does not have. Each has the same
second half, and it is the point: `json-read` goes red and `json-write` stays
green, which is what says the break is the READER's.

## The shape

A driver is a **command**, not a binary — so a leg can be assembled from what a
backend already has rather than from a second copy of it. Some registered
drivers are shell scripts that dispatch: the C++ one answers the table and block
surfaces from `build/conformance-cpp` and hands the two COOK surfaces to
`build/schema_test_cook`, which already opens that unit and already runs that
battery. Others are one binary that answers everything, and the JavaScript one
answers every surface from one module, because a node process starts in tens of
milliseconds and there was nothing to assemble — see "Registering a
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

**A CASE may be absent too, and it is said the same way.** A driver that
cannot answer one case of a surface it otherwise implements writes
`<case>.absent` — an empty file, beside where the answer would go — and the
harness counts it. The cell then reads `pass 16/16 +4a`: what the leg answered,
and how many it said it cannot. The distinction is the surface-level one at a
finer grain, and the corpus needed it the day it gained VARIABLE-class
instances: a backend with no variable class still answers the wire surface over
every FIXED instance, and a leg that failed the whole surface for them would say
nothing about what it does carry.

**THE REFERENCE LEG MAY NOT ANSWER ABSENT, AT EITHER GRAIN**, and that rule is
what makes absence safe rather than a place to hide: an absence from the
reference leg — `cpp`, first in the discovered registry, and the one
`conformance-pin` takes its pins from — is the corpus losing its own
expectation, not a port's missing feature. All three ways of saying it are a
FAILURE there, named in the matrix and in the failure list: a surface left out
of `list`, exit code 2 on a surface, and `<case>.absent` for a single case. The
harness prints `FAIL absent` for the two coarse ones, and the success footer
cannot print beside any of them. The rule belongs to that registry alone: a run
handed a SUBSTITUTED one with `--drivers`, as the big-endian leg does for its
single Go driver, is one leg of a port and not the matrix, so its first line is
not the reference and its absences are ordinary.

**THE MATRIX IS THE COMPLETION TRACKER.** Green cells over total cells is what
"done" means for the tables layer, and an absence — per surface or per case — is
the row of work that is left, named where it can be seen. The eight ports read
`+28a` on the wire surfaces and `+26a` on the text surfaces today — the four
variable-class instances, the fifteen of the message class (a union whose
arms are tables, an array of unions, and an optional array,
docs/SPEC-TABLES.md §2.6, §2.3), the three of the wide-scalar class (§3) and
the six of the byte buffer class (§2.5, one of them wire-only) — and the
reference answers them all;
they become part of the count as schema#349 and the per-construct follow-ons
land them, one language at a time, with nothing in this data or this contract
moving as they do.

**Absent is not failure, and the distinction is the whole reason the matrix
exists.** A backend with no text form is missing a FEATURE; a backend whose text
form writes the wrong bytes is failing a TEST. A harness that printed both the
same way would be telling a port to implement nothing in particular. The C#
leg is the worked example: it registered with `json-read` and `json-write`
ABSENT, and both cells moved to `pass 18/18` when the C# walk landed — nothing
in the data or in this contract moved with them.

**A DRIVER MAY TAKE ITS GENERATED TREE FROM THE ENVIRONMENT**, and the
JavaScript leg does (`SCHEMA_JS_GENERATED`). That is not a contract change — the
harness still hands a manifest, a surface and an output directory and nothing
else — but it is what lets `conformance-negative-control-js` point the SAME
driver at a sabotaged copy of the generated modules and require the matrix to go
red. A leg whose generated tree is baked in has nothing for a control to aim at.

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
| `cook-write` | `cook-write` | Load the instance's wire, `Cook` it in each byte order, the bytes, as `<instance>` and `<instance>-be` | the two files `schema cook` wrote |
| `cook-foreign` | `cook` | byte-swap the file's MAGIC word, Open, `open\n` or `refuse\n` | `refuse\n` |
| `block` | `block` | Open the image, `open\n` or `refuse\n` | `open\n` |
| `block-foreign` | `block` | byte-swap the image's MAGIC word, Open, `open\n` or `refuse\n` | `refuse\n` |
| `block-dump` | `block` | Open the image, the canonical ROW dump | `block/<name>.dump` |
| `forgery` | `forgery` (`block`) | Open the forged file at the claimed extent, `open\n` or `refuse\n` | the verdict in the manifest |
| `cook-forgery` | `forgery` (`cook`) | the same, over the cook battery's 111 | the verdict in the manifest |

`wire` and `json-read` write a file named by the instance; `json-write` writes
`<instance>.json`; the others write a file named by the case.

**`cook-write` IS THE ONE SURFACE WHERE A LANGUAGE WRITES AN ACCELERATOR RATHER
THAN READING ONE, and the expectation is the TOOL's file.** Every other cook
surface asks whether a reader agrees about bytes somebody else produced; this
one asks whether a WRITER produces the bytes `schema cook` produces, in both
byte orders, from the same wire (SPEC-TABLES.md §7.6). It needs no big-endian
host on either side, because the order is a parameter of the write rather than a
fact of the machine. A leg with no writer prints ABSENT, which is the whole
point of the distinction: C++ answers it today and every other leg is missing a
FEATURE rather than failing a test.

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

### What the BLOCK surfaces do not cover, by design

§19's block form is a FIXED-size table's third layout, and it does not admit a
variable one: a block is one contiguous image of rows at a stride, and a
pointered table has no stride. So the block surfaces carry no variable case and
never will — that is ABSENT BY DESIGN and not a gap anyone is tracking, which
is why it is written here rather than left to look like an oversight in the
matrix. The variable class's accelerator is the COOK (§7), and the `cook`,
`cook-foreign` and `cook-forgery` surfaces have carried pointered roots since
they landed.

### Why there is no `dump-read`

The contract asks a driver to write a dump and never to parse one. `wire` proves
the writer against the reader; `json-write` proves the reader's VALUES against a
text a third implementation wrote; `json-read` proves the writer against an
instance the driver did not build. A dump parser would add a component to every
port and cover nothing those three do not. Where a backend has no text form, the
gap is real and the matrix says so rather than a parser papering over it.

## Registering a language

1. Write a driver at `test/conformance/<lang>/driver` that answers `list` and
   the surfaces the backend has.
2. Write `test/conformance/<lang>/ci.json`, the leg's row in the pull-request
   matrix.
3. `make conformance`.
4. Fill the language's column on `docs/PORTING.md`; `go test ./compiler`
   holds it to the tree.

**THE REGISTRY IS DISCOVERED, NOT LISTED.** The harness reads every
`test/conformance/<lang>/driver` that exists, and `harness matrix` reads every
`ci.json` beside one; no file names every language, so a port adds its
directory and touches nothing another port touches (`docs/CONTRIBUTING.md`,
"Adding a language"). The reference leg is `cpp` — the harness's own constant,
sorted first — and the rest follow by name.

**`ci.json` is one JSON object of strings**, and `.github/workflows/ci.yml`'s
conformance job reads it as the leg's matrix row:

| key | what it is |
|---|---|
| `targets` | the make targets that build the leg (required) |
| `env` | `VAR=value` assignments the make and run steps carry — a toolchain on PATH rather than in `dist/` |
| `runtime`, `runtime_tag` | the sibling checkout the leg needs (`serialize.cs`) and the workflow variable that pins it (`SERIALIZE_CS_TAG`) |
| `node`, `rust`, `dart`, `java`, `otp` + `elixir` | the toolchain step to run, and the version it installs |
| `dotnet` | the .NET SDK step to run, and the file holding the version — `.github/dotnet-version`, the repo's one pin, because nine `csproj` files under `test/` and `bench/` target it and `certify.yml` builds all of them (issue #470) |

A leg that names a toolchain with no step yet adds one step to the workflow,
keyed on its new field; that is the one edit a port makes there. A driver with
no `ci.json` fails `harness matrix`, and the registry gate
(`harness/registry_test.go`) plants a fake language in a copy of the tree and
requires the harness, the bench pass and the Makefile to discover it with no
shared file edited.

**THE ELIXIR LEG's shape, because its cost is all start-up.** Its driver's own
modules are compiled to `.beam` WITH the unit corpus rather than at every start:
an `.exs` compiled per invocation cost 0.4 s on top of the BEAM's own 0.26 s
boot, twelve times over. The leg answers all twelve surfaces in 5.6 s, and none
of that is the cases.

The reference leg is `cpp`, and the harness sorts it first: `make conformance-pin`
takes the cook dumps, the block row dumps and both forgery batteries' offsets
from it. That is the repo's standing convention — C++ writes the
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

## The wire-fuzz driver

`harness wire-fuzz` (docs/SPEC-TABLES.md §4.2) is the tolerant wire's fuzzer,
and its leg is a different shape from the surface drivers above: ONE process
for the whole run, on a pipe, that only reads. The harness generates every
mutant and holds the oracle; the leg loads each mutant through the generated
tolerant read, saves whatever it decoded, and answers what happened. It
decides nothing about what a mutant means.

```
make tables-wire-fuzz                      the C++ reference, plain and sanitized
make tables-wire-fuzz-negative-control     both controls: a check removed, the fuzzer red
./build/conformance-harness wire-fuzz --driver <cmd> [--seed S] [--n N]
./build/conformance-harness wire-fuzz --driver <cmd> --replay <file> --unit <key> --root <table>
```

**The stream, little-endian throughout.** The working directory is the
repository root; the driver is a command, split on whitespace, as every
driver is.

| direction | what | when |
|---|---|---|
| in | `u32` roster count, then per root: `u16 n`, the unit key, `u16 n`, the root table's name | once, first |
| out | one byte per roster entry: `1` when this leg has a codec for it, else `0` | once, in reply |
| in | per mutant: `u32` roster index, `u32` length, the bytes | until EOF |
| out | per mutant: `u8 loaded`; `i32 unknown, kind_mismatch, clamped, duplicate`; `u8 malformed`; `i64 measure`; `i64 saved`, then that many bytes | one reply per mutant, flushed before the next is read |

- **`loaded`** is whether a root came back. A FIXED root always loads — its
  `Load` fills a value and reports — so the byte is `1` and the report says the
  rest. A VARIABLE root's `Load` returns NULL when the caller's region was
  wrong, and a `0` here fails the run: the leg sized the region from
  `LoadMeasure`, so a refusal is the measure and the load disagreeing.
- **`measure`** is what `LoadMeasure` asked for, region bytes in total; a FIXED
  root answers `-1`. The harness holds it to the framing's bound (§4.2, §6.5).
- **`saved`** is the length `Save` wrote, or `-1` when `Measure` or `Save`
  refused the value. The bytes follow only when it is positive.
- **Every buffer is allocated at exactly its size** — the mutant, the region,
  the save — so that a sanitized build's redzone begins at the last byte a
  reader may touch, and **every reply is flushed before the next mutant is
  read**, so a crash is attributed to the mutant that caused it. A buffer of
  ZERO bytes is the one exception to the sizing: it is allocated at one byte,
  because `malloc(0)` may answer null and null is how a leg reports an
  allocation that failed.
- **A root the leg cannot name is a `0` in the roster and nothing more**: the
  harness never sends it a mutant, and the line it prints says how many seeds
  were absent. A port with no variable class registers its fixed roots and is
  fuzzed over them.

The C++ leg is `test/tables/wire_fuzz_main.cpp`: a codec table of
`(unit, root, run)` triples, a fixed and a variable template, and the stream.
A port's leg is that file's shape in its own language and nothing else — the
mutators, the oracle and the comparison never move.

## The budget

`make conformance` runs under the two-minute rule (#320). Measured on arm64
macOS, everything already built, median of three, **one sitting** — the whole
table re-measured together when the C leg registered, because a table whose rows
come from different sittings can say the whole costs less than one of its parts.
**The laptop was NOT quiet for this sitting** (three sibling ports building
alongside, load average near 12), so every row here is an upper bound and every
row is inflated by the same load: read the RATIOS, and read the totals as a
ceiling. The quiet-laptop numbers this replaces are in git history:

| leg | wall |
|---|---|
| all seven, 268 cases per leg | 22.8 s |
| `java` alone | 0.63 s |
| `go` alone | 0.74 s |
| `c` alone | 0.76 s |
| `rust` alone | 0.88 s |
| `cpp` alone | 1.00 s |
| `dart` alone | 2.86 s |
| `cs` alone | 18.0 s |

**The cost is per-PROCESS, not per-case, and the numbers say so plainly.** The
battery grew from 80 cases per leg to 268 — the cook's 111 forgeries, the 66
hostile trees, a sixth cook, two block row dumps and the two FOREIGN surfaces —
and the six NATIVE legs still answer all 268 each in under three seconds. The C#
leg starts a runtime once per surface plus once per cook, because
`test/cs-cook`'s dump takes one root per invocation, and that is where nearly
the whole wall is: of the 1,876 cases in this table, the 1,608 the six NATIVE
legs answer cost about seven seconds between them — under three seconds each,
even under this load — and the last 268 cost eighteen.

**`cook-write` added 40 cases to the C++ leg alone** — twenty instances in two
byte orders, the four VARIABLE ones among them — and the table above is NOT
re-priced for them: they are answered inside `build/conformance-cpp`, a process
that was already starting for the wire surfaces, so what they cost is a wire
load, a numbering walk for a pointered root and a memset per case in a leg the
table already measures at a second. The row is left as it was measured rather
than adjusted by arithmetic, which is what the sitting rule means.

**The C leg answers all 268 from one native binary**, which is the cheapest
shape the contract allows and what puts its row among the fastest. A sixth leg
cost the whole run less than the run's own spread across three repeats.

The DART leg is AOT-COMPILED for exactly that reason: `dart run` would pay a JIT
start-up per surface, and the leg answers all twelve out of one binary —
thirteen execs of something that starts in milliseconds, which is what puts its
row among the native legs rather than beside the C# one.

**Six native legs cost about seven seconds together.** Adding the two
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
