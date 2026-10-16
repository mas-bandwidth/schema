# JavaScript: the fixed read against its own packet read (identity lane)

## Attempt w3 (2026-10-16, hulk): the in-place identity read is landed; on hulk it is level with the packet read, so identity is NOT flipped

**Verdict: FAIL** for the gate on hulk: FIXED-FASTER in sitting 2 only. The
committed codec's in-place read is 708.9 / 672.0 ns/record (best, sitting 1 /
sitting 2), and the same leg's packet read is 677.6 / 677.3. Sitting 1 is 4.6%
slower and sitting 2 is 0.8% faster. The brief flips identity only on two
FIXED-FASTER sittings, so `gates/packet-read/js/identity` stays `:unknown` and
the roadmap is unchanged.

What landed (the owner's decision of 2026-10-16, which reopened §3.4's
no-fast-path ruling): w2's proposed emitter diff, now in
`internal/codegen/jstable/`. It is the in-place identity read, one-pass nested
structs, BigInt-free 64/128-bit clamps, and 32-bit layout and hash compares.
Alongside it: docs/SPEC-TABLES.md §3.4 THE IN-PLACE IDENTITY READ, the general
per-language rule with javascript listed; the three ruling guards narrowed to
the spec's list; and fixedform_inplace_test.go, which holds the in-place and
shared paths value- and byte-identical. generated/bench/paired/js was
regenerated.

| sitting | order | before the change (`base`, control) | shared path, same codec (`shared`, control) | **in-place (`in`, committed)** | **packet `bench_mixed_read`** | gate |
|---|---|---|---|---|---|---|
| 1 | in, shared, base, packet | 1468.8 / 1475.9 | 1306.1 / 1309.8 | **708.9 / 711.2** | **677.6 / 679.7** | FAIL |
| 2 (control) | packet, base, shared, in | 1475.3 / 1488.6 | 1306.5 / 1311.2 | **672.0 / 675.8** | **677.3 / 684.0** | pass |

These are best / median ns/record for `bench_fixed_read` and `bench_mixed_read`,
copied from the runners' output in [board.txt](board.txt), w3 section. Every
spread is under §2.3's 15%: 1.82% to 4.25% on the table legs and 1.09% / 1.92%
on the packet leg.

**Diagnostic sitting 3, declared and NOT counted toward the verdict.** It ran
after the two sittings, in the order in, packet, in, packet. The in-place read
was 711.2 then 678.8, and the packet read was 677.4 then 678.1. The in-place
read is bimodal on this host, at about 672 to 679 or about 709 to 711 per
process, while the packet read holds at 677 to 678. So on hulk the two reads
are level, and the order of the sitting does not decide which side wins.

### The w3 sitting

- **Host:** hulk, x86_64, AMD Ryzen Threadripper 2990WX 32-Core (Zen+, SMT,
  4 NUMA nodes), kernel 6.8.0-142-generic. It was chosen as the quietest of
  amd-2, amd-3 and hulk at 02:52Z. amd-2 had two 100% jobs and a 1-min load
  of 2.24. amd-3 was compiling, with a 5-min load of 3.45. hulk's 1-min load
  was 0.68. The bench was never the studio Mac.
- **Node** v26.7.0. **serialize.js** v1.4.2 (de0591c9e6fddbbc5d4512b4558f091ffa2158ad),
  sibling checkout at `SERIALIZE_JS=../serialize.js`. The packet runner ran
  in `NODE_ENV=production`.
- **Pin:** `taskset -c 4` (NUMA node 0, SMT sibling 36) for every process. In
  every sitting, core 4's busy-jiffy delta equals its total delta, so the core
  was ours. Core 36's busy delta is 2 to 4 jiffies over the window, so the
  sibling was idle.
- **Load, declared:** sitting 1 ran 03:00:23Z to 03:00:57Z, 1-min load
  0.17 to 1.11. Sitting 2 ran 03:00:57Z to 03:01:30Z, 1.11 to 1.18. The
  diagnostic sitting 3 started at load 0.99 and ended at 1.37. The script
  waited for a 1-min load under 1.5 before each sitting. The `top` lines at
  each end show nothing above 2.2% CPU (board.txt). **Window verdict: valid.**
  The pinned core and its sibling were ours, the host was otherwise idle, and
  every spread is under 15%.
- **Legs:** four node processes per sitting, run back to back by one script,
  in reversed order in the control. The legs are:
  - `in`: the committed tree at 752c99c17, dirty-files 0. The measured code
    is the same as at the branch head, which changes only docs and results.
  - `shared`: the same commit with the door closed (`const identity = false;`
    in FixedTableTable.js). This is the shared path that every compiled plan
    takes.
  - `base`: d1f6577c4 regenerated with its own emitter. This is the codec
    before this card.
  - `packet`: the packet runner.

  The sha256 of each leg's generated files is in board.txt. Corpus ids are
  `066b2822b3e5d487` for the table legs and `fd0748942dc6ae09` for the packet
  leg. Iterations are 400000 records for the table legs and 4000000 for the
  packet leg, with 1 discarded warmup plus 7 runs. Every process passed its
  golden gates before the clock (`rc=0` on every leg), and `--gate --plan-read`
  passes on both runners on hulk.

### What the two hosts say together

| host | before | shared path | in-place | packet |
|---|---|---|---|---|
| hetzner2 (w2, Ryzen 9 7950X3D) | 833.2 / 844.4 | 736.7 / 751.7 | 338.4 / 348.9 | 346.7 / 353.4 |
| hulk (w3, Threadripper 2990WX) | 1468.8 / 1475.3 | 1306.1 / 1306.5 | 708.9 / 672.0 | 677.6 / 677.3 |

The change halves the fixed read on both hosts. On hetzner2 the w2 numbers
were taken with this same diff as scratch, and the in-place read is
FIXED-FASTER by 1.3% to 2.4%. On hulk the in-place read is level with the
packet read. The gate needs a margin that holds on any quiet host, and this
change does not give one. The next causes are the ones w2 named: BigInt
allocation and its write barrier (~95 ns on hetzner2), and the 80-stat loop
(~145 ns). w2's hand-tuned `p6-mz` scratch codec (local clamps, byte arrays in
32-bit lanes, small-u64 `BigInt(Number)`) was 4% faster than the emitted diff
on hetzner2. Profile it with `node --cpu-prof` on the `in` leg, pinned on hulk,
and compare the `--trace-deopt` output of a 709 ns process with that of a
672 ns one.

### Tests

These ran on hulk with go1.26.6 and node v26.7.0:

- `go test -count=1 -timeout 600s ./internal/codegen/jstable/` with
  `SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1`, after `make tables-fixedform-corpus`
  (serialize v1.16.2 sibling, the tag certify.yml pins): `ok`.
- `make shape-gate`: clean, 46 files on the ledger.
- `go test -count=1 -timeout 600s ./bench/paired/ ./internal/ci/`: both `ok`.
- Reverse witness: with the door projecting at `at + 10` instead of `at + 8`,
  two tests go red. TestJSFixedInPlaceEqualsSharedPath fails, and so does the
  narrowed guard TestDecodeClampsEnumOrdinalAndRangedInt ("must be exactly
  §3.4's shape").

### The plan lane

`gates/packet-read/js/plan` stays `:unknown`, for the reason w1 gives below:
there is no lineage corpus from BENCH-STANDARD §1.10 (items 4 to 6) and no
paired-driver plan-read mode. On the plan lane the JS reader takes the shared
path, which is the `shared` column above.

---

## Attempt w2 (2026-10-16): the cause is fixed in a proposed emitter diff, measured, and HELD

**Verdict for the shipped codec: FAIL**, still, in both w2 sittings (833.2 / 844.4
ns/record against the packet read's 346.7 / 353.4). **With the proposed emitter
diff the fixed read is FASTER than the packet read in both sittings** (338.4 /
348.9 against 346.7 / 353.4), but the diff is generated-code and emitter work
outside this card's PATHS, it turns three ruling guards red, and its margin is
thin. So identity is **not** flipped, and the card HOLDs for a ruling.

| sitting | order | shipped | proposed, copy kept (`emitted-noid`) | **proposed (`emitted`)** | scratch upper bound (`p6-mz`) | **packet `bench_mixed_read`** |
|---|---|---|---|---|---|---|
| 1 | tables, then packet | 833.2 / 836.8 | 736.7 / 743.5 | **338.4 / 340.2** | 324.9 / 331.8 | **346.7 / 351.4** |
| 2 (control) | packet, then tables | 844.4 / 847.2 | 751.7 / 754.1 | **348.9 / 350.0** | 327.4 / 329.3 | **353.4 / 367.2** |

ns/record, best / median, `bench_fixed_read` for the four table columns,
copied from the runners' output in [board.txt](board.txt) (w2 section). Spreads
2.6% to 3.8% on every table row, 2.0% and 8.7% on the packet row: all under
§2.3's 15%. `emitted` beats the packet best by 8.3 ns (2.4%) and 4.5 ns (1.3%):
FIXED-FASTER on the letter of the gate, but by less than one sitting's packet
spread, so it is reported as a pass of a scratch codec and nothing more.

### The w2 sitting

- **Host:** hetzner2, x86_64, AMD Ryzen 9 7950X3D, kernel 6.8.0-139-generic, no
  SMT. hetzner1 (w1's host) was not usable for a clean window: its pinned
  core's SMT sibling was carrying another job, which doubled every number
  intermittently (w2 saw 1428 ns for the shipped read on cpu11 while cpu3 ran a
  dotnet job at 100%). Never the studio Mac.
- **Node** v26.7.0, **serialize.js** v1.4.2 (de0591c9e6fddbbc5d4512b4558f091ffa2158ad).
- **Pin** `taskset -c 12`; core 12's busy-jiffy delta equals its total delta in
  both sittings, so the core was ours for the window.
- **Window:** sitting 1 2026-10-16T02:43:35Z–02:43:54Z, load 0.91 → 0.94
  (1-min); sitting 2 02:43:54Z–02:44:13Z, load 0.94 → 1.40. The script waited
  for a 1-min load under 1.5 before starting.
- **Five node processes per sitting**, back to back from one script: the
  shipped table runner, the same committed table runner over three scratch
  codecs (a copy of the tree whose `generated/bench/paired/js` was replaced),
  and the packet runner. Corpus ids as in w1 (`066b2822b3e5d487` table,
  `fd0748942dc6ae09` packet); iterations as in w1 (400000 / 4000000 records, 1
  warmup + 7 runs). Every table run passes the four golden gates before the
  clock and the write-back proof after it (`rc=0` on every process).

### The cause, and what removes it

w1's split stands: the read is two passes, a copy of the 1236-byte body into
the plan image (~415 ns) and the object projection (~395 ns). w2 profiled the
projection (`node --prof`, and stubbing pieces in scratch to price them): of
~385 ns with the copy gone, the 80 stats cost ~145, the 8 entities ~100, the
BigInt fields ~95; `RecordWriteSaveFP` (the write barrier on BigInt stores) is
12% of ticks against the packet read's 7%; nested `MixedEntityFixedDecode` was
not inlined by TurboFan; the nested type's text check ran twice. The
working set is not the cause (64 destinations and 1 destination time the same).

The proposed emitter diff (appendix; also tested, see below) does, in
`internal/codegen/jstable/`:

1. **fixedmodule.go — the identity plan projects straight from the file.**
   `identity = entries === <T>FixedIdentity && holeN === 0`; when true the
   record loop calls `<T>FixedDecode(values[k], srcView, at + 8, stage)` and
   skips the prefill and the copy. This is the ~415 ns.
2. **fixedjs.go — one straight projection.** A nested struct is inlined into
   its parent's decode instead of called (so its text is checked once, at the
   top), an array of structs walks with its array and count in locals and a
   running offset, loop variables are depth-unique (`i`, `i1`, ...) so inlined
   loops cannot shadow.
3. **fixedjs.go — wide integers.** A 64- or 128-bit ranged clamp is decided on
   the u32 lanes as a Number (exact: monotone rounding, only used when every
   bound is below 2^53, and for 128 bits a multiple of 2^64), so no BigInt is
   made only to be compared; a 128-bit value whose high half is zero is its low
   half; an int128 takes its high half signed instead of `BigInt.asIntN`.
4. **fixedmodule.go / fixedruntime.go — per call.** The layout compare and the
   per-record hash read in 32-bit lanes through the file's view
   (`TableFixedKnownLayout.layoutView`, made once at module load).

`emitted-noid` (everything but item 1) is 736.7 / 751.7 ns: **the projection
work alone does not come close; the copy has to go.**

### Why it HOLDs: the ruling item 1 reopens

Item 1 is a second path. §3.4 and the emitter quote the owner's ruling: "no
strict flag, no fast path and no second reader", because "a form whose cost
moved when a peer shipped would be a performance cliff". After item 1 a peer
on this build's own layout reads at ~340 ns and a peer on any other layout
still pays the copy (~750 ns). Three tests pin that ruling and go red on the
diff, honestly, by name:

- `fixedform_test.go:209` TestDecodeClampsEnumOrdinalAndRangedInt — "the load grew a second reader"
- `fixedform_test.go:308` TestFixedFormLoadPrefillsThePlanHoles — "the load loop still branches on identity"
- `fixedform_test.go:367` TestFixedGuardComparedAtArgW — "the load grew a second reader"

`go test -count=1 -timeout 600s ./internal/codegen/jstable/` with
`SCHEMA_SLOW=1` and the C++ byte oracle built (`make tables-fixedform-corpus`,
serialize v1.17.0-2-g6c692bf sibling) on hetzner2: **base 64 pass / 0 fail /
0 skip; proposed 61 pass / 3 fail / 0 skip**, the three above and nothing else
(the fixed-versioning, refuse-is-total, float32 NaN and node conformance tests
all pass on the proposed emission). A float32 change (read non-NaN values with
`getFloat32`) was tried and withdrawn because `float32_nan_bits_test.go`
forbids that call textually.

The two ways forward, for Glenn:

- **Rule that identity may project in place** (the cliff is bounded: the
  compiled path is unchanged, ~10% faster than today from items 2 to 4), then
  land the diff, regenerate `generated/bench/paired/js`, rewrite the three
  guards to pin the new rule, and re-run this card's two sittings.
- **Keep the ruling**, and make the compiled plan project from the file too
  (the plan becomes per-field source offsets for the decode rather than a copy
  into an image). That is a plan-shape change, larger, and not measured here.

Either way the margin is thin (1.3% to 2.4% for the emitted diff, 6% to 7% for
the hand-tuned `p6-mz`, whose extra wins were local clamps, byte arrays moved
in 32-bit lanes and a small-u64 `BigInt(Number)` read). The next cause, if a
wider margin is wanted: BigInt allocation and its write barrier (~95 ns), and
the 80-stat loop (~145 ns), which the bitpacked packet read also pays.

---

## Attempt w1 (2026-10-16, hetzner1)

**Verdict: FAIL**, in both sittings. The fixed form's identity read is slower
than the same leg's packet read of the same 64 logical records, each side
measured alone (BENCH-STANDARD §1.10, Glenn's ruling of 2026-09-11: slower is a
FAIL for that leg).

| sitting | order | `bench_fixed_read` ns/record (best / median) | `bench_mixed_read` ns/record (best / median) | spread fixed / packet | gate |
|---|---|---|---|---|---|
| 1 | table, then packet | 861.4 / 903.5 | 364.4 / 369.4 | 10.47% / 3.16% | **FAIL** |
| 2 (control) | packet, then table | 881.9 / 908.1 | 362.2 / 372.8 | 8.19% / 5.81% | **FAIL** |

Every number above is copied from the runners' own output in
[board.txt](board.txt); none is recomputed. Both spreads are under §2.3's 15%,
so both rows publish and the verdict stands.

### The sitting

- **Host:** hetzner1, x86_64, AMD Ryzen 7 PRO 8700GE, kernel 6.8.0-139-generic.
  Never the studio Mac.
- **Node:** v26.7.0. **serialize.js:** v1.4.2
  (de0591c9e6fddbbc5d4512b4558f091ffa2158ad), the sibling checkout at
  `SERIALIZE_JS=../serialize.js`. Packet runner in `NODE_ENV=production`
  (checks `contract`), the standard leg.
- **Pin:** `taskset -c 11` for both processes of both sittings.
- **Window:** sitting 1 2026-10-16T01:56:28Z–01:56:43Z, load 14.06 → 12.80
  (1-min); sitting 2 01:56:49Z–01:57:04Z, load 11.94 → 9.96. The host is a
  shared bench (other jobs on other cores, named in board.txt's `top` lines);
  the pinned core's busy-jiffy delta equals its total delta in both sittings,
  so core 11 was ours for the whole window. The load is declared, not hidden:
  this is not the quiet game-server window, and the verdict is not close
  (2.4x), so the load does not decide it.
- **Two processes.** The table runner imports no runtime and the packet runner
  imports the serialize.js sibling, so the two halves are two node processes
  run back to back by one script invocation on one host and one core. They
  therefore carry two `corpus_id`s:
  - `bench_fixed_read`: `066b2822b3e5d487` (the five paired table files,
    `--indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus`,
    exactly what bench/paired passes the js table leg);
  - `bench_mixed_read`: `fd0748942dc6ae09` (`testdata/wire/bench_mixed.bin` and
    `bench/corpus/variants/bench_mixed.variants.bin`).
  The paired corpus is derived from the packet variants (bench/paired, §1.9),
  so the 64 logical records are the same ones.
- **Iterations:** each runner's fixed standard count, 1 discarded warmup plus 7
  measured runs in-process (§2.1): table 400000 records per run (bytes_per_op
  1264.296875, one LOAD per 64 records), packet 4000000 records per run
  (bytes_per_op 438). Every run is over the 200 ms floor. Interleaving (§2.4)
  is across the two sittings: the order of the two processes is reversed in
  the control.
- **Gates before the clock:** the table runner's four (layout is the
  corpus's, the file loads, it writes back byte for byte, reused storage
  round-trips) plus the tolerant corpus's framing; the packet runner's two
  (variant 0 is the wire golden; all 64 variants decode flat, agree with the
  runtime tier field for field, and re-encode byte for byte). Both runners
  also prove after the clock, untimed, that the timed destination still writes
  back to what was read. `--gate --plan-read` passes on both runners on
  hetzner1.

### The cause, from a timing split

A scratch split (not committed: it re-imports the generated modules and times
the pieces of `FixedTableFixedLoad` separately, pinned to the same core, two
runs) on hetzner1, 2026-10-16T01:57Z:

| arm | run 1 best / median | run 2 best / median |
|---|---|---|
| A: the whole shipped LOAD | 899.6 / 915.6 | 901.6 / 926.2 |
| B: the plan run alone (the identity COPY of the 1236-byte body into the plan image, 32-bit DataView lanes) | 414.6 / 420.8 | 423.7 / 433.0 |
| C: the object projection alone (`FixedTableFixedDecode` from the image into the JS objects) | 392.4 / 399.6 | 403.0 / 414.4 |

(ns/record.) The JS read is **two full passes over the record**: it copies the
whole body into the plan's image and then projects the image into JavaScript
objects. Either pass alone is already slower than the packet wire's entire
read (about 363 ns), which decodes the bits straight into the objects in one
pass. So this is not noise and not one hot spot: the read pays a copy the C
legs read in place, and its projection alone costs more than the whole
bitpacked read.

What would move it, named for the follow-up and not done here (it is
generated code, outside this card's PATHS):

1. on the identity lane, project straight from the source view at the
   record's offset and skip the copy into the image (the hole list is already
   empty; arm B is the price of not doing this);
2. then profile the projection itself (`node --cpu-prof` on arm C, and
   `--trace-deopt` on `BenchMixedFixedDecode`) for per-field DataView calls,
   text validation and nested-object stores; even with the copy gone, arm C
   alone does not beat the packet read.

### The plan lane

`gates/packet-read/js/plan` stays `:unknown`. It needs BENCH-STANDARD §1.10's
lineage corpus (items 4 to 6 there: the VOLD schema's records written by the
C++ dump, re-pinned under `bench/paired/corpus/`, and the lock) and the paired
driver's `-mode plan-read`, and neither is on this branch. The Rust card's
corpus (`bench/tables/rust/lineage/` at 337aaacf5: the VOLD schema, the
969-byte-record `bench_lineage.bin` and `.layout`, and a `schema.lock` with the
VOLD layout before the identity) has the §1.10 shape, but it is not on this
branch, and the JS codec cannot read it as generated: this tree's
`generated/bench/paired/js/FixedTableTable.js` knows ONE layout (the identity,
`FixedTableFixedKnown` has one entry), so a JS plan read needs the paired JS
codec regenerated against that lock (`generated/`, the Makefile's paired
generation), which is outside this card's PATHS. So the JS plan lane is
reported absent and no second corpus is authored. Proposed as a card: promote
that corpus to `bench/paired/corpus/` as §1.10 items 1 to 7 name, generate
every leg's paired codec against its lock, and give the paired driver its
plan-read mode; then the JS `bench_plan_read` row is the same loop as
`bench_fixed_read` over `bench_lineage.bin`.

### The roadmap

`gates/packet-read/js/identity` is **not** flipped: the brief flips it only on
two FIXED-FASTER sittings, and both are FAIL. docs/roadmap.sexp and ROADMAP.md
are unchanged.

## Appendix: the proposed emitter diff (against c704b7f43; not applied, outside PATHS)

```diff
diff --git a/internal/codegen/jstable/fixedjs.go b/internal/codegen/jstable/fixedjs.go
index 38e1526a5..264acb56f 100644
--- a/internal/codegen/jstable/fixedjs.go
+++ b/internal/codegen/jstable/fixedjs.go
@@ -178,6 +178,22 @@ type fixedGen struct {
 	// an absent optional lands its payload's declared default, and for a
 	// nested type that default is that type's own Init.
 	need func(cls, typeName string)
+	// loops is how deep the decode's emitted loops nest. A nested struct is
+	// INLINED into its parent's projection (one straight function, the way the
+	// flat packet reader is), so a loop inside an inlined element must not
+	// shadow the loop variable its base offset is written in.
+	loops int
+	// inl numbers the locals an inlined struct element binds.
+	inl int
+}
+
+// loopVar names the loop variable at the current decode nesting depth: `i` at
+// the top, so a flat type's emission is unchanged, then i1, i2, ...
+func (g *fixedGen) loopVar() string {
+	if g.loops == 0 {
+		return "i"
+	}
+	return fmt.Sprintf("i%d", g.loops)
 }
 
 func (g *fixedGen) require(cls, typeName string) {
@@ -238,11 +254,16 @@ func (g *fixedGen) store(ind, view, at string, t ir.FieldType, expr string) {
 
 func (g *fixedGen) load(ind, lhs, view, at string, t ir.FieldType) {
 	if fixedStorageBytes(t) == 16 {
-		g.pf("%s%s = %s.getBigUint64(%s, true) | (%s.getBigUint64(%s, true) << 64n);\n",
-			ind, lhs, view, at, view, off(at, 8))
+		// A ZERO HIGH HALF IS THE LOW HALF, with no shift and no OR: two BigInts
+		// fewer per field. Otherwise a signed value takes its high half SIGNED,
+		// which is the same number BigInt.asIntN(128, ...) made, one BigInt sooner.
+		hi := "getBigUint64"
 		if fixedSigned(t) {
-			g.pf("%s%s = BigInt.asIntN(128, %s);\n", ind, lhs, lhs)
+			hi = "getBigInt64"
 		}
+		g.pf("%s%s = (%s.getUint32(%s, true) | %s.getUint32(%s, true)) === 0\n", ind, lhs, view, off(at, 8), view, off(at, 12))
+		g.pf("%s  ? %s.getBigUint64(%s, true)\n", ind, view, at)
+		g.pf("%s  : (%s.%s(%s, true) << 64n) | %s.getBigUint64(%s, true);\n", ind, view, hi, off(at, 8), view, at)
 		return
 	}
 	if t.Kind == ir.TFloat32 {
@@ -497,6 +518,7 @@ func (g *fixedGen) emitDecodeBody(st *ir.Struct) {
 	// text violation in the image refuses by name before a single property of
 	// value is touched.
 	g.emitCheckTextStruct(st, "at", 0, "  ")
+	g.inl, g.loops = 0, 0
 	var cur int64
 	for _, f := range st.Fields {
 		g.emitDecodeField(f, "at", cur, "value", "  ")
@@ -670,18 +692,21 @@ func (g *fixedGen) emitDecodePayload(f *ir.Field, base string, at int64, val, in
 		// Content validity over the used units was checked before any write to
 		// value in emitCheckTextStruct (§4.5 fix 11, §5.3).
 		g.emitDecodeCount(ind, val+"."+name+"Length", off(base, at), f.Type.Size)
-		g.pf("%sfor (let i = 0; i < %d; i++) { %s.%s[i] = view.getUint8(%s + i); }\n",
-			ind, f.Type.Size, val, name, off(base, at+fixedCountBytes))
+		iv := g.loopVar()
+		g.pf("%sfor (let %s = 0; %s < %d; %s++) { %s.%s[%s] = view.getUint8(%s + %s); }\n",
+			ind, iv, iv, f.Type.Size, iv, val, name, iv, off(base, at+fixedCountBytes), iv)
 	case f.Type.Kind == ir.TBytes:
 		g.emitDecodeCount(ind, val+"."+name+"Length", off(base, at), f.Type.Size)
-		g.pf("%sfor (let i = 0; i < %d; i++) { %s.%s[i] = view.getUint8(%s + i); }\n",
-			ind, f.Type.Size, val, name, off(base, at+fixedCountBytes))
+		iv := g.loopVar()
+		g.pf("%sfor (let %s = 0; %s < %d; %s++) { %s.%s[%s] = view.getUint8(%s + %s); }\n",
+			ind, iv, iv, f.Type.Size, iv, val, name, iv, off(base, at+fixedCountBytes), iv)
 	case f.Type.Kind == ir.TWString:
 		// Content validity over the used units was checked before any write to
 		// value in emitCheckTextStruct (§4.5 fix 11, §5.3).
 		g.emitDecodeCount(ind, val+"."+name+"Length", off(base, at), f.Type.Size)
-		g.pf("%sfor (let i = 0; i < %d; i++) { %s.%s[i] = view.getUint16(%s + i * 2, true); }\n",
-			ind, f.Type.Size, val, name, off(base, at+fixedCountBytes))
+		iv := g.loopVar()
+		g.pf("%sfor (let %s = 0; %s < %d; %s++) { %s.%s[%s] = view.getUint16(%s + %s * 2, true); }\n",
+			ind, iv, iv, f.Type.Size, iv, val, name, iv, off(base, at+fixedCountBytes), iv)
 	default:
 		g.emitDecodeElement(f, base, at, val+"."+name, ind)
 	}
@@ -711,8 +736,26 @@ func (g *fixedGen) emitDecodeCount(ind, dst, at string, bound int64) {
 
 func (g *fixedGen) emitDecodeLoop(f *ir.Field, base string, at int64, count, expr, ind string) {
 	elem := fixedElementBytes(f)
-	g.pf("%sfor (let i = 0; i < %s; i++) {\n", ind, count)
-	g.emitDecodeElement(f, fmt.Sprintf("%s + i * %d", off(base, at), elem), 0, expr+"[i]", ind+"  ")
+	iv := g.loopVar()
+	if _, ok := f.Type.Ref.(*ir.Struct); ok && f.Type.Kind == ir.TNamed {
+		// AN ARRAY OF STRUCTS IS WALKED WITH ITS ARRAY AND ITS COUNT IN LOCALS
+		// and a RUNNING offset, so the inlined element body reads the image at
+		// o + k: no property load per element for the array or the bound, and
+		// no multiply per field.
+		g.pf("%s{\n", ind)
+		g.pf("%s  const a%s = %s, n%s = %s;\n", ind, iv, expr, iv, count)
+		g.pf("%s  for (let %s = 0, o%s = %s; %s < n%s; %s++, o%s += %d) {\n", ind, iv, iv, off(base, at), iv, iv, iv, iv, elem)
+		g.loops++
+		g.emitDecodeElement(f, "o"+iv, 0, "a"+iv+"["+iv+"]", ind+"    ")
+		g.loops--
+		g.pf("%s  }\n", ind)
+		g.pf("%s}\n", ind)
+		return
+	}
+	g.pf("%sfor (let %s = 0; %s < %s; %s++) {\n", ind, iv, iv, count, iv)
+	g.loops++
+	g.emitDecodeElement(f, fmt.Sprintf("%s + %s * %d", off(base, at), iv, elem), 0, expr+"["+iv+"]", ind+"  ")
+	g.loops--
 	g.pf("%s}\n", ind)
 }
 
@@ -720,7 +763,22 @@ func (g *fixedGen) emitDecodeElement(f *ir.Field, base string, at int64, expr, i
 	if f.Type.Kind == ir.TNamed {
 		switch r := f.Type.Ref.(type) {
 		case *ir.Struct:
-			g.pf("%s%sFixedDecode(%s, view, %s, report);\n", ind, r.Name, expr, off(base, at))
+			// A NESTED STRUCT IS INLINED, not called: the projection is ONE
+			// straight function, the way the flat packet reader is written. The
+			// content check already ran over this element's text at the top of
+			// the outermost decode (emitCheckTextStruct recurses), so the inlined
+			// fields are the loads alone and nothing is validated twice.
+			// <T>FixedDecode is still emitted per struct for any other caller.
+			g.inl++
+			v := fmt.Sprintf("e%d", g.inl)
+			g.pf("%s{\n", ind)
+			g.pf("%s  const %s = %s;\n", ind, v, expr)
+			cur := at
+			for _, sf := range r.Fields {
+				g.emitDecodeField(sf, base, cur, v, ind+"  ")
+				cur += fixedFieldBytes(sf)
+			}
+			g.pf("%s}\n", ind)
 			return
 		case *ir.Union:
 			tag := fixedUnionTagBytes(r)
@@ -776,10 +834,109 @@ func (g *fixedGen) emitDecodeElement(f *ir.Field, base string, at int64, expr, i
 			return
 		}
 	}
+	if g.emitBigClampLanes(ind, expr, off(base, at), f) {
+		return
+	}
 	g.load(ind, expr, "view", off(base, at), f.Type)
 	g.emitDecodeBounds(ind, expr, f)
 }
 
+// emitBigClampLanes is §4.6's ranged clamp for a 64- or 128-bit integer,
+// decided on the image's u32 LANES AS A NUMBER before any BigInt exists: a
+// clamped value lands its bound's literal and an in-range one is loaded once,
+// so no BigInt is made only to be compared and no BigInt comparison runs.
+//
+// THE DECISION IS EXACT. The Number is hi * 2^32 + lo (for 128 bits, the high
+// 64-bit half), which rounds MONOTONICALLY, and it is only used when every
+// bound it is compared against is an integer below 2^53 in magnitude: a value
+// past such a bound rounds to a Number at or past it, never back across it,
+// and a value at the bound is exact. A 128-bit bound must also be a whole
+// multiple of 2^64 so the low half decides only the one tie it can. Anything
+// else answers false and takes the BigInt path unchanged.
+func (g *fixedGen) emitBigClampLanes(ind, expr, at string, f *ir.Field) bool {
+	if !fixedIsBig(f.Type) {
+		return false
+	}
+	if _, _, ok := fixedFloatClampEnds(f); ok {
+		return false
+	}
+	w := fixedStorageBytes(f.Type)
+	rlo, rhi, ok := ir.TableRawRange(f)
+	var low, high bool
+	switch {
+	case f.Type.Kind == ir.TBits:
+		// a bits(N) that does not fill its storage clamps at 2^N - 1 and only
+		// there (emitDecodeBounds' last case); one with a declared range too is
+		// left to the BigInt path
+		if ok || int64(f.Type.Width) >= 8*w {
+			return false
+		}
+		rlo = big.NewInt(0)
+		rhi = new(big.Int).Sub(new(big.Int).Lsh(big.NewInt(1), uint(f.Type.Width)), big.NewInt(1))
+		low, high = false, true
+	case ok:
+		low, high = fixedClampEnds(f)
+	default:
+		return false
+	}
+	if !low && !high {
+		return false
+	}
+	safe := new(big.Int).Lsh(big.NewInt(1), 53)
+	inSafe := func(n *big.Int) bool { return new(big.Int).Abs(n).Cmp(safe) < 0 }
+	hiGet := "getUint32"
+	if fixedSigned(f.Type) {
+		hiGet = "getInt32"
+	}
+	var nExpr, loCond, hiCond string
+	switch w {
+	case 8:
+		if (low && !inSafe(rlo)) || (high && !inSafe(rhi)) {
+			return false
+		}
+		nExpr = fmt.Sprintf("view.%s(%s, true) * 4294967296 + view.getUint32(%s, true)", hiGet, off(at, 4), at)
+		loCond = fmt.Sprintf("n < %s", rlo.String())
+		hiCond = fmt.Sprintf("n > %s", rhi.String())
+	case 16:
+		two64 := new(big.Int).Lsh(big.NewInt(1), 64)
+		ql, rl := new(big.Int), new(big.Int)
+		qh, rh := new(big.Int), new(big.Int)
+		ql.DivMod(rlo, two64, rl) // Euclidean: the remainder is in [0, 2^64)
+		qh.DivMod(rhi, two64, rh)
+		if (low && (rl.Sign() != 0 || !inSafe(ql))) || (high && (rh.Sign() != 0 || !inSafe(qh))) {
+			return false
+		}
+		nExpr = fmt.Sprintf("view.%s(%s, true) * 4294967296 + view.getUint32(%s, true)", hiGet, off(at, 12), off(at, 8))
+		// below L = qL * 2^64 exactly when the high half is below qL; above
+		// H = qH * 2^64 when the high half is past qH, or equal with a low half
+		// that is not zero
+		loCond = fmt.Sprintf("n < %s", ql.String())
+		hiCond = fmt.Sprintf("n > %s || (n === %s && (view.getUint32(%s, true) | view.getUint32(%s, true)) !== 0)",
+			qh.String(), qh.String(), at, off(at, 4))
+	default:
+		return false
+	}
+	g.pf("%s{\n", ind)
+	g.pf("%s  const n = %s;\n", ind, nExpr)
+	first := true
+	if low {
+		g.pf("%s  if (%s) { %s = %s; report.clamped++; }\n", ind, loCond, expr, jsIntLit(rlo, true))
+		first = false
+	}
+	if high {
+		kw := "if"
+		if !first {
+			kw = "else if"
+		}
+		g.pf("%s  %s (%s) { %s = %s; report.clamped++; }\n", ind, kw, hiCond, expr, jsIntLit(rhi, true))
+	}
+	g.pf("%s  else {\n", ind)
+	g.load(ind+"    ", expr, "view", at, f.Type)
+	g.pf("%s  }\n", ind)
+	g.pf("%s}\n", ind)
+	return true
+}
+
 // emitDecodeBounds is §4.6's ranged-scalar clamp, inline: a declared min/max
 // (and a bits(N) width that does not fill storage) is held AFTER the load, on
 // the value the consumer will see. The plan never does this. Vacuous ends —
diff --git a/internal/codegen/jstable/fixedmodule.go b/internal/codegen/jstable/fixedmodule.go
index f0905fffe..add792de4 100644
--- a/internal/codegen/jstable/fixedmodule.go
+++ b/internal/codegen/jstable/fixedmodule.go
@@ -376,8 +376,20 @@ func (g *fixedModule) emitRoot(st *ir.Struct) {
 	// at read time at all: no stranger's layout is ever walked.
 	g.pf("  const known = %sFixedKnown[pick];\n", st.Name)
 	g.pf("  if (layoutBytes !== known.layoutBytes) { report.refused = TableFixedRefusal.LayoutMalformed; return -1; }\n")
-	g.pf("  for (let i = 0; i < layoutBytes; i++) {\n")
-	g.pf("    if (bytes[layoutAt + i] !== known.layout[i]) { report.refused = TableFixedRefusal.LayoutMalformed; return -1; }\n")
+	// ONE VIEW OVER THE FILE, MADE ONCE PER READ AND NEVER PER RECORD: the
+	// layout compare, the record hashes and the identity projection all read
+	// through it, in 32-bit lanes where they move words (§3.4).
+	g.pf("  const srcView = new DataView(bytes.buffer, bytes.byteOffset, bytes.length);\n")
+	g.pf("  {\n")
+	g.pf("    // THE LAYOUT COMPARE MOVES IN 32-BIT LANES, then a tail of at most three bytes.\n")
+	g.pf("    const lv = known.layoutView;\n")
+	g.pf("    let i = 0;\n")
+	g.pf("    for (; i + 4 <= layoutBytes; i += 4) {\n")
+	g.pf("      if (srcView.getUint32(layoutAt + i, true) !== lv.getUint32(i, true)) { report.refused = TableFixedRefusal.LayoutMalformed; return -1; }\n")
+	g.pf("    }\n")
+	g.pf("    for (; i < layoutBytes; i++) {\n")
+	g.pf("      if (bytes[layoutAt + i] !== known.layout[i]) { report.refused = TableFixedRefusal.LayoutMalformed; return -1; }\n")
+	g.pf("    }\n")
 	g.pf("  }\n")
 	// STEP 8: the plan this peer resolves to — laid down at module load, never
 	// here — and the record size FROM THE LOCK and never from the file.
@@ -418,21 +430,31 @@ func (g *fixedModule) emitRoot(st *ir.Struct) {
 	g.pf("  // and the declared default is what it keeps. Same loop either way. There is\n")
 	g.pf("  // no identity flag in the record loop — an empty list is what skips the work.\n")
 	g.pf("  const holeN = TableFixedHoles(entries, entryCount, plan.cover, plan.holes);\n")
-	g.pf("  // ONE VIEW OVER THE FILE, MADE ONCE PER READ AND NEVER PER RECORD: the\n")
-	g.pf("  // read loop moves a run in 32-bit lanes and a DataView is what has an\n")
-	g.pf("  // unaligned word move in this language (§3.4).\n")
-	g.pf("  const srcView = new DataView(bytes.buffer, bytes.byteOffset, bytes.length);\n")
 	g.pf("  // §4.5, §5.3: stage the record loop's counters so a refusal leaves the\n")
 	g.pf("  // caller's clamped and widened untouched; commit after the loop.\n")
 	g.pf("  const stage = TableFixedResetReport(plan.stage);\n")
+	// PROPOSED (js-fixed-read-beats-its-packet-read w2): THE IDENTITY PLAN IS
+	// ONE COPY OF THE WHOLE BODY, SO ITS IMAGE IS THE FILE ITSELF. Projecting
+	// straight from the file's view skips the copy (~415 ns of a ~860 ns read
+	// on hetzner1). THIS IS A SECOND PATH and it reopens the owner's ruling
+	// that §3.4 quotes — no fast path, a form's cost must not move when a peer
+	// ships — because a compiled plan still pays the copy. It needs that
+	// ruling changed before it lands; fixedform_test.go's identity-door guard
+	// says so by going red.
+	g.pf("  const identity = entries === %sFixedIdentity && holeN === 0;\n", st.Name)
 	g.pf("  let at = layoutAt + layoutBytes;\n")
 	g.pf("  for (let k = 0; k < n; k++) {\n")
 	g.pf("    // A RECORD WHOSE HASH NAMES NO LAYOUT THIS READER HOLDS IS A REFUSAL BY\n")
 	g.pf("    // NAME, never a guess and never damage: nothing is decoded, no counter moves.\n")
-	g.pf("    if (((bytes[at] | (bytes[at + 1] << 8) | (bytes[at + 2] << 16) | (bytes[at + 3] << 24)) >>> 0) !== hashLo ||\n")
-	g.pf("        ((bytes[at + 4] | (bytes[at + 5] << 8) | (bytes[at + 6] << 16) | (bytes[at + 7] << 24)) >>> 0) !== hashHi) {\n")
+	g.pf("    if (srcView.getUint32(at, true) !== hashLo || srcView.getUint32(at + 4, true) !== hashHi) {\n")
 	g.pf("      report.refused = TableFixedRefusal.NoLayout; return -1;\n")
 	g.pf("    }\n")
+	g.pf("    if (identity) {\n")
+	g.pf("      %sFixedDecode(values[k], srcView, at + 8, stage);\n", st.Name)
+	g.pf("      if (stage.refused !== 0) { report.refused = stage.refused; report.malformed = stage.malformed; return -1; }\n")
+	g.pf("      at += recordBytes;\n")
+	g.pf("      continue;\n")
+	g.pf("    }\n")
 	g.pf("    for (let h = 0; h < holeN; h++) {\n")
 	g.pf("      const o = plan.holes[h * 2], z = plan.holes[h * 2 + 1];\n")
 	g.pf("      for (let i = 0; i < z; i++) { image[o + i] = %sFixedPrefill[o + i]; }\n", st.Name)
diff --git a/internal/codegen/jstable/fixedruntime.go b/internal/codegen/jstable/fixedruntime.go
index 4b84e41e1..cf11ed0ed 100644
--- a/internal/codegen/jstable/fixedruntime.go
+++ b/internal/codegen/jstable/fixedruntime.go
@@ -1280,6 +1280,9 @@ export class TableFixedKnownLayout {
     this.layout = layout;
     this.layoutBytes = layoutBytes | 0;
     this.recordBytes = recordBytes | 0;
+    // the lock's bytes as a view, made ONCE at module load, so the per-read
+    // layout compare moves in 32-bit lanes and allocates nothing
+    this.layoutView = new DataView(layout.buffer, layout.byteOffset, layout.length);
   }
 }
 
```
