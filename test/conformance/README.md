# The tables conformance harness — the driver contract

A port of the tables layer is **"make the driver pass"** — and fill its column
on [docs/PORTING.md](../../docs/PORTING.md), the techniques register. The
register is read before the port brief is written; the port PR is not ready
until every technique on it is carried or cited in the port's column; and a
blind read of the port reports two lists against it — methods the port uses
that the register lacks, and methods the register has that the port lacks.

The data lives under `testdata/conformance/tables` and fixes one contract for every language
(`FORMAT.md` there states every file shape). This page states what a language's
DRIVER must do. Registering a port is one driver at `<lang>/driver`: the
harness discovers it, and nothing lists it.

```
make conformance                     run every registered driver, print the matrix
make conformance-generate            rewrite the generated half of the data
make conformance-pin                 rewrite the half the reference leg writes
make conformance-negative-control    prove the harness can go red
```

**Negative controls stand behind it, and each localises a different
thing** — a harness that has never gone red is watching nothing, and one that
goes red everywhere localises nothing:

| control | what it breaks | what must go red | what must stay green |
|---|---|---|---|
| `conformance-negative-control` | one byte of a C++ dump | `cpp / json-write` | `wire` |
| `conformance-negative-control-cpp` | the C++ walk's field offset, in the emitter | `cpp / json-read` | `json-write`, `wire` |
| `conformance-negative-control-block-dump` | one byte INSIDE A ROW of the block image | `block-dump` | `block`, `forgery` |
| `conformance-negative-control-cs` | the C# walker, in the emitter | `cs / json-read` | `json-write` |
| `conformance-negative-control-go` | the Go leg, in the emitter | its own surface | the rest |
| `conformance-negative-control-go-walk` | the Go walk's field offset | `go / json-read` | `json-write` |
| `conformance-negative-control-js-walk` | the bool arm of the generated JavaScript text reader | `js / json-read` | `json-write`, `wire` |
| `conformance-negative-control-java-block` | the array PITCH CHECK in the Java `open` | `java / forgery` | `block`, `block-dump` |
| `conformance-negative-control-java-walk` | the field-key match in the Java `fromJson` | `java / json-read` | `json-write`, `wire` |
| `conformance-negative-control-lua-walk` | the field-key match in the Lua `FromJson`, in the emitter | `lua / json-read`, `lua / json-hostile` | `json-write`, `wire`, `cook-write` |
| `conformance-negative-control-rust-walk` | the field-key match in the Rust emitter's `json_fields` reader | `rust / json-read` | `json-write`, `wire` |

A qualifying negative control changes an accepted value, canonical write-back,
or a required acceptance/refusal. Changing a diagnostic counter or reason alone
must not change the comparison.

Two of those rows are the reason two surfaces exist at all. The row-dump one is
why `block-dump` is separate from `block`: `Open` reads the prologue and the
triples and nothing else, so a byte inside a row cannot move its answer. And the
pitch one is why `forgery` is separate from both: it removes a CHECK rather than
moving a value, so the reader still READS correctly and has stopped REFUSING —
which no valid image can show you.

**A PER-BACKEND WALK CONTROL, one per port with a text form**, because a
walker's break is the backend's and not the data's:
`conformance-negative-control-go-walk` breaks the Go walk's offset arithmetic,
`conformance-negative-control-c` breaks the C walk's field index, and
`conformance-negative-control-java-walk` breaks the Java `fromJson`'s field-key
match. Each has the same second half as every control here, and it is the
point: `json-read` goes red and `json-write` stays green, which is what says
the break is the READER's. (Elixir's text form covers the variable class now;
its walk control still arrives with the full conformance battery.)

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

An expected semantic refusal scores PASS. A historical `.absent` file may
transport that refusal; the feature-absence rules below apply only when the
case requires an accepted value.

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

**Wire-form coverage is separate from storage coverage.** C++, C, C# and the
independent compiler engine carry the id-table wire (form 1, the variable form) and bitpacked message
form. C and C# also carry variable regions, builders, retained unknowns and
canonical cooks. Their pipe drivers compare values, re-saved bytes, measured
sizes and reports with the independent engine. `make tables-c-wire-fuzz` and
`make tables-c-retain` run C natively and under ASan/UBSan; the corresponding
C# managed, native, builder and retention targets are in make/cs.mk.

Unsupported roots are printed as `N seeds absent (roots the leg has no codec
for)`. Absence is explicit and never counted as a passing fixture.
PORTING.md M20 records the coverage and gates.

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
| `message` | `message` | AnnounceRead the connection, LoadMessages the entire batch, then SaveMessages of every accepted body; an explicit refusal for a row whose message wire is `-` | the complete message golden, or refusal |
| `report` | `report` | `read\n` followed by the complete ToJson value, or `refused\n` alone | `reports.txt`, then `report-values/<case>.json` for accepted reads |
| `framing` | `framing` | the same one verdict and complete accepted value | the hand-authored verdict and value file |
| `retain` | `retain` | LoadRetain at the row's capacities, then `read\n` and the complete known ToJson value, or `refused\n` | `retain-values/<case>.json` after acceptance |
| `retain-save` | the rows that name a save | the same round trip, the SAVED BYTES | the pinned save |
| `json-read` | `instance` | the single text read of `json/<name>.json`, then Save of the accepted value | the wire golden |
| `json-write` | `instance` | Load the wire file, ToJson, the text, as `<name>.json` | `json/<name>.json` |
| `json-hostile` | `json-hostile` | the single text read of `<tree>/<root>.json`, then `read\n` and the complete value, or `refused\n` alone | the standard verdict for every port, then `<tree>.value.json` on acceptance |
| `cook` | `cook` | Open the cook, the canonical node dump | `cook/<case>.dump` |
| `cook-write` | `cook-write` | construct the recipe's value, Cook it in both byte orders as `<instance>` and `<instance>-be` | the tool's two cooked files from the same construction |
| `cook-foreign` | `cook` | byte-swap the file's MAGIC word, Open, `open\n` or `refuse\n` | `refuse\n` |
| `block` | `block` | Open the image, `open\n` or `refuse\n` | `open\n` |
| `block-foreign` | `block` | byte-swap the image's MAGIC word, Open, `open\n` or `refuse\n` | `refuse\n` |
| `block-dump` | `block` | Open the image, the canonical ROW dump | `block/<name>.dump` |
| `forgery` | `forgery` (`block`) | Open the forged file at the claimed extent, `open\n` or `refuse\n` | the verdict in the manifest |
| `cook-forgery` | `forgery` (`cook`) | the same, over the cook battery's 111 | the verdict in the manifest |

`wire` and `json-read` write a file named by the instance; `json-write` writes
`<instance>.json`; the others write a file named by the case.

**Every read has one outcome: acceptance or refusal.** Binary, message,
retaining and text readers return the complete value or refuse the whole read.
An accepted `report`, `framing`, `json-hostile` or `retain` row owes its complete
canonical JSON value after `read\n`. Refused rows emit `refused\n` and no
value. A message read and its write-back cover every body in the batch.

The oracle generates `report-values/<case>.json`, `retain-values/<case>.json`
and `<tree>.value.json`; framing values are authored from the rule. A leg with
no text writer may supply the read verdict plus `<case>.absent`, but its missing
value is incomplete coverage. A wrong verdict cannot be excused by that marker.

Refusal reasons, detection order, counters, retention counts and allocation
measurements are local diagnostics. Legacy seven-field report lines remain
readable transport: an explicit refused or malformed flag means refusal, while
repair counters never select the verdict. Drivers should emit the plain verdict.
The retired tolerant/strict split is not an alternate conformance path.

Every port owes the same standard verdict. Legacy `carried-by` and `uncarried`
metadata cannot permit an invalid acceptance. Counter-only and reason-only
surfaces have no cross-language obligation; cook and block forgeries compare
acceptance/refusal rather than a reason name or the first detected condition.

**`message` IS THE ONE SURFACE THAT READS TWO FILES**, because a message's id
table is somewhere else (docs/SPEC-TABLES.md §3.3): the CONNECTION's
announcement carries it, the driver reads that first into one direction's
VOCABULARY, and the batch resolves against it. The manifest's `connection` line
names the announcement and the `message` line names the pair of wires, so the
driver joins the two by key and never derives a vocabulary of its own. **The
verbs are PLURAL**, because the form's primitive is a BATCH of bodies of one
root and a single message is the batch of one, so a `message` case whose batch
wire holds one body still drives `LoadMessages` and `SaveMessages` with a count
of one. A driver without the required codec reports a feature absence, which
remains incomplete coverage. An expected semantic refusal is an answered case
and scores PASS, independently of its diagnostic text.

**Retention keeps known values independent of its local storage layout.**
`retain` compares the complete known value after a successful retaining read;
`retain-save` additionally compares retained write-back bytes where the row
pins them. The byte capacity is local: `full` must fit the input, and `short`
means one byte less than this implementation used in a roomy load. The id-list
capacity is a count. Neither retention counter is compared. Retain-store
capacity does not change the schema's read verdict; they determine which unknown
records can survive a later retaining save. The fuzz retention roster contains
variable file roots only and uses roomy stores on both sides.

**Cook writes use explicit value construction.** The committed recipe names
its instance and the two output pins, with optional `input=<JSON path>` and
`share=<source>:<alias>` construction options. The derived driver line has
exactly five fields:

```
cook-write <instance> <JSON input or -> <source field or -> <alias field or ->
```

The harness resolves the input to the explicit JSON path or the instance's
JSON; `-` means use the `no-text` instance's wire. After reading that value,
the optional source and alias fields identify populated blobs that must share
one in-memory object. A sharing recipe requires an explicit input. This lets a
cook test legal in-memory sharing without loading binary data the wire reader
must refuse. The driver writes both byte orders; golden output paths are never
sent to it.

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

**`ci.json` is one JSON object of strings**, and `.github/workflows/ci-full.yml`'s
conformance job reads it as the leg's matrix row:

| key | what it is |
|---|---|
| `targets` | the make targets that build the leg (required) |
| `env` | `VAR=value` assignments the make and run steps carry — a toolchain on PATH rather than in `dist/` |
| `runtime`, `runtime_tag` | the sibling checkout the leg needs (`serialize.cs`) and the workflow variable that pins it (`SERIALIZE_CS_TAG`) |
| `node`, `rust`, `dart`, `java`, `otp` + `elixir` | the toolchain step to run, and the version it installs |
| `moo` | the moo pin step to run (`./.github/actions/moo`, which builds `make/moo.mk`'s `MOO_PIN` into `dist/moo/` and caches it): the field's value is the release the pin names, held equal to a negative-control group's `moo` by `tools/negativecontrols` |
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
boot, twelve times over. The leg answers all twelve surfaces it carries in
5.6 s, and none of that is the cases.

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

## The nightly

`.github/workflows/nightly-conformance.yml` checks cross-language compatibility
every night at 05:41 UTC, on the fleet's runners and never on the host named
space, and on `gh workflow run nightly-conformance.yml --ref <branch>`.
Every registered port joins through `harness matrix --ports`.

The current completion target is the original nine: C++, C, C#, Go, Rust,
Java, JavaScript, Dart and Elixir. Required gaps in any of these remain open;
results for other registered ports do not substitute for them.

1. **C++ writes and validates one canonical corpus.** `cpp-writes` produces
   the wire, text, message, block, cook and fixed-form corpus, checks it against
   the committed pins, and runs `harness run --only cpp`. The reference reads
   the corpus and its answers must match the expected bytes. The job publishes
   the shared `cpp-corpus` and its own `reference-report` coverage artifact.
2. **Every port reads that same corpus and matches its output.** Each
   `port (<lang>)` job runs `harness run --only <lang> --claims` and its
   fixed-form gate. Every writer's answer must equal the canonical bytes;
   missing answers and mismatches fail, while named unsupported cases remain
   visible. The job saves its writes and coverage as `port-<lang>-writes`.

**Work is O(n) in languages, not O(n²).** If every writer produces the same
bytes and every reader accepts those bytes, every writer's output is
interoperable with every reader on the covered corpus. The reference need
not read a separate identical copy from each writer. The `coverage` job
records the reference and port results together. Its
`coverage --require-original-nine` gate requires C++, C, C#, Go, Rust, Java,
JavaScript, Dart and Elixir to cover every manifest-defined surface/class and
case count. The reference rows are checked independently against that shape,
so a missing reference row cannot shrink the denominator. Accepted and refused observations both count as answered cases. Missing
features, named absences, failures and uncarried cases do not count as complete. Each of the eight ports must also record a passing
fixed-form gate. These corpus results are necessary, but they cannot by
themselves certify a full implementation.

`coverage --require-original-nine` is the unified full-implementation gate. It
also requires all of the following, independently of what a port chooses to
claim:

- Every required construct in `compiler.CarryConstructs()` must be supported
  by every original-nine backend. Missing features remain failures even when
  the current corpus contains no example of them.
- Required features must have executable corpus witnesses and successful
  code generation. Declaring an unrelated feature in an unused table does
  not cover it. The `implementation --lang <lang>` command checks these
  prerequisites; it is not a substitute for native execution.
- Every original-nine language, including C++, must pass
  `test/conformance/nightly-readiness`. This runs the mandatory native,
  packet, fixed-form, strict fuzz and retention acceptance gates. Unsupported
  valid roots/forms, omitted comparisons, live skipped obligations and failed
  controls cannot count as completion. Named retired tests are reported
  explicitly; their replacement obligations still have to pass.
- Each language must supply exactly one passing `readiness-<lang>.txt` and
  an adjacent `readiness-<lang>.revision` matching the current Git checkout.
  Missing, failed, duplicate or stale receipts fail. Nightly writes these
  only from the actual acceptance command and uploads the complete logs.
- The roadmap renderer must validate the current graph and evidence overlays,
  find all three sections and all nine columns complete, and confirm that
  `ROADMAP.md` matches the data. A manually completed task cannot override a
  failed implementation or runtime check. SBCL and a Git checkout are required;
  absence of either fails closed.

Full implementation is the prerequisite for success. Neither a smaller
passing corpus nor a green roadmap can substitute for it. The six expansion
languages remain visible but are not part of this completion obligation. The
table and all failure diagnostics are printed and uploaded even when the gate
fails; ordinary `coverage` remains an informational report.
Every confirmed defect found by these gates must leave a permanent regression.
Keep the original trigger and seed, minimize it, and commit the expected decoded
values, canonical bytes or refusal report. Language-neutral cases belong in the
shared corpus; language-specific API behavior also needs a native regression.
Record that the broken implementation fails and the repair passes, and wire the
case into nightly acceptance. An uncommitted workbench reproducer does not close
the defect, and re-pinning incorrect bytes is not a repair.

The `harness cross-read` command remains available for individual diagnostics,
but is not repeated per language in the nightly.

**A CLAIMED CASE MAY NOT BE ABSENT; AN UNCLAIMED ONE IS COVERAGE.** A port
claims a case when the case's unit uses at least one construct of the
compiler's carries registry (`compiler/carries_<target>.go`, read through
`compiler.UnitConstructs`) and its target carries every one of them — the
registration is the port naming, construct by construct, what it carries. A
class a port lands one at a time (variable, message, wide-scalar, byte
buffer) is not a construct there, so its cases are coverage until it is
registered as one. Under
`--claims` a claimed case answered `<case>.absent`, on a surface the leg
runs, fails the night by name (the reversed witness is
`harness/claims_test.go`); a case the port does not claim, and a whole
surface it does not run, remain informational in `run --claims`. The final
original-nine completion gate is stricter: those gaps fail completion even
when no individual feature was claimed. Expansion-language gaps remain
visible in the per-port table of answered, failed, claimed-absent and
not-carried cases per surface and class. The harness is built with every `schema_leg_<lang>` tag there, because
a target behind a tag registers only under it, and a leg with no
registration is refused rather than read as claiming nothing.

## The wire-fuzz driver

`harness wire-fuzz` checks the single read contract in one process per leg.
The harness generates every mutant and runs the independent oracle. The leg
reports whether its generated reader accepted the whole input and, only after
acceptance, the complete canonical write-back. The comparator checks that one
verdict first, then exact accepted bytes. It never compares diagnostic reasons,
counters, retention counts or measured allocation sizes. Every mutant is
compared; `--finite-owed` is retired and rejected.

```
make tables-wire-fuzz                      the C++ reference, plain and sanitized
make tables-wire-fuzz-negative-control     both controls: a check removed, the fuzzer red
make tables-wire-fuzz-retain               the same mutants through the RETAINING paths
make tables-wire-fuzz-retain-negative-control   its own pair, one control per engine
./build/conformance-harness wire-fuzz --driver <cmd> [--seed S] [--n N]
./build/conformance-harness wire-fuzz --driver <cmd> --replay <file> --unit <key> --root <table>
```

**The stream, little-endian throughout.** The working directory is the
repository root; the driver is a command, split on whitespace, as every
driver is.

| direction | what | when |
|---|---|---|
| in | `u32` roster count, then per root: `u16 n`, the unit key, `u16 n`, the root table's name, `u8` the FORM, `u8` RETAIN | once, first |
| out | one byte per roster entry: `1` when this leg has a codec for it, else `0` | once, in reply |
| in | per mutant: `u32` roster index, `u32` length, the bytes | until EOF |
| out | per mutant: `u8 loaded`; `i32 unknown, kind_mismatch, widened, clamped, duplicate`; `u8 malformed`; `u8 refused`; `i64 measure`; `i32 retained, retain_lost`; `i64 saved`, then that many bytes | one reply per mutant, flushed before the next is read |

The reply header remains 47 bytes for transport compatibility. Its semantic
acceptance bit is `loaded && !refused && !malformed`; no counter affects it.

- **`loaded`** is the reader's success result for every root class. Preserve it
  if a later writer fails; a save failure must not disguise a read acceptance.
- **`measure`** is diagnostic transport only. Local allocation checks and caller
  resource limits belong outside the schema verdict. An allocation failure or
  driver crash is an infrastructure failure, not a successful refusal.
- **`saved`** is the canonical write-back length. After refusal it is `-1` and
  no bytes follow; do not save defaults or partial state. After acceptance a
  writer failure is also `-1`, which fails comparison because the accepted
  value owes a complete write-back. Legacy negative reason codes are diagnostics.
- **Every buffer is allocated at exactly its size** — the mutant, the region,
  the save — so that a sanitized build's redzone begins at the last byte a
  reader may touch, and **every reply is flushed before the next mutant is
  read**, so a crash is attributed to the mutant that caused it. A buffer of
  ZERO bytes is the one exception to the sizing: it is allocated at one byte,
  because `malloc(0)` may answer null and null is how a leg reports an
  allocation that failed.
- **The FORM selects the read path**: `1` is the file path (whose input may
  use form 1 or form 3 according to the declared root), and `2` is a batch.
  They are separate roster entries over one root, because they are two readers. A MESSAGE mutant resolves against the
  CONNECTION's announced VOCABULARY, and the leg derives that vocabulary from
  its OWN unit's announcement, the compile-time constant its backend emits, read
  through the same `AnnounceRead` a receiver uses. The vocabulary is a pure
  function of the build version, so both sides derive the same one and the
  roster carries no announcement. **A form-`2` mutant is driven through
  `LoadMessages` and `SaveMessages`**, the plural verbs, because a batch is the
  form's primitive. Every accepted body is saved, in order; body one alone
  is incomplete even when all later bodies were validated. A leg with no
  message codec answers `0` for that entry, exactly as it does for a root it
  cannot name.
- **The RETAIN byte says which path this entry is driven through**
  (docs/SPEC-TABLES.md §6.6). `0` is the ordinary run: `Load` and `Save`, with
  retention off, which is what leaves the round-trip requirement the one §4.2
  states. `1` is the retention arm: `LoadRetain`, `MeasureRetain` and
  `SaveRetain` over one `TableRetain` the leg owns, with **both capacities
  declared large**, because a record's byte cost is the port's own and two
  engines at one tight capacity would drop different records. The arm's roster
  is the VARIABLE-CLASS FILE ROOTS and nothing else: a fixed-class root's
  `LoadRetain` is refused by name and a form-2 `SaveRetain` refuses by name
  (§6.6, §3.3), so those entries never carry a `1`, and the line says how many
  seeds sat outside the arm.
- **`retained` and `retain_lost`** remain in the header as local diagnostics.
  They do not affect acceptance or the comparison. The retaining arm compares
  complete accepted write-back bytes.
- **A root the leg cannot name is a `0` in the roster and nothing more**: the
  harness never sends it a mutant, and the line it prints says how many seeds
  were absent. A port with no variable class registers its fixed roots and is
  fuzzed over them.

The C++ leg is `test/tables/wire_fuzz_main.cpp`: a codec table of
`(unit, root, run)` triples, a fixed and a variable template, and the stream.
A port's leg is that file's shape in its own language and nothing else — the
mutators, the oracle and the comparison never move.

**The pinned vectors.** `testdata/wire/tables/fuzz-vectors/INDEX.txt` names
mutants the fuzzer has already gone red on, one per line as `<name> <unit>
<root> <file>`. Each rides in every run, exactly as it is and before the random
pass, so a red it pins is a SEEK rather than a search. They are seeds like any
other but are never mutated and never drawn from by the random pass, which is
what keeps the mutant sequence a function of the corpus and the seed alone.
Pinning a vector cannot move a red that was already there. A vector earns its
place by having been a red, and the fix that closed it owes a negative control
naming it.

**A failure prints what reproduces it**: the run seed, the corpus seed and pass
it came from, and the mutant's own bytes as hex, restored with `xxd -r -p`. A
red in a CI log a person cannot re-run is still a red they can replay. A mutant
past 4 KB prints its SHA-256 instead and the run seed carries the reproduction.
The whole mutant is on disk at `--failed` either way.

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
start-up per surface, and the leg answers all twelve it carries out of one binary,
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
and the Java leg starts none, because its generated units — the block unit and
the pointered unit — are packages of a single classpath, so both
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

The optional `wire-fuzz --builder` arm uses mutable file readers over variable
file roots. `--mixed-builder` preserves the full roster and uses the mutable
oracle only for variable file roots, keeping fixed and message roots. These
profiles share the same acceptance and exact write-back comparison. `--builder`
cannot be combined with `--message` or `--mixed-builder`. `--retain` keeps the
variable-file retention domain; no profile compares allocation or retention
counters. Native resource and allocator tests remain separate gates.

The `wire-refused` instance marker specifies refusal for every port. Legacy
`go-refusal=<reason>` and `refused-by=<ports>` spellings are accepted as
compatibility metadata; they do not select a language or a shared reason. Drivers may emit `refused` or the historical `<case>.absent` file; the
file's diagnostic text is ignored. This expected semantic refusal scores PASS.
Emitting value bytes, or emitting nothing at all, fails. An ordinary absence
for a missing codec remains incomplete coverage.
