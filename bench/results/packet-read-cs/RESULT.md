# C#: the fixed read against its own packet read (gates/packet-read/cs/identity, gates/packet-read/cs/plan)

**Verdict: no verdict. Neither window was valid.** In both sittings the identity read
measured about **10.6x slower** than the same leg's packet read: **3075.4 vs 289.9
ns/record** (best of the rounds completed). That is a FAIL by a wide margin, but it comes
from INVALID windows, so it is reported here and not credited. The plan lane is
**absent** because this tree has no lineage corpus. Neither roadmap task is flipped.

## The C# line (copied from board.txt, which the driver printed)

| | attempt A (taskset -c 3) | attempt B (taskset -c 3,4) |
|---|---|---|
| window | **INVALID**: the C# packet control write moved 76.8% (> 5%) | **INVALID**: a competing `go` process (CI, 39.5% CPU) during round 3 |
| load1 during the window | (see board) | 0.49–1.28 |
| rounds | 7 of 7 | 3 of 7 (stopped by the refusal) |
| identity read, best / median / worst ns | 3095.1 / 3302.6 / 3606.4 (spread 15.1%, noisy) | 3075.4 / 3151.5 / 3241.4 (spread 5.2%) |
| packet read, best / median / worst ns | 294.7 / 297.0 / 598.6 (spread 51.2%, invalid) | 289.9 / 291.6 / 292.2 (spread 0.8%) |
| GATE identity | — (window INVALID) | — (incomplete) |
| GATE plan | — (absent) | — (absent) |

- **host**: hetzner1, linux/amd64, AMD Ryzen 7 PRO 8700GE, 16 threads, no isolated cores. Attempt A was pinned to one core. Running the tiered JIT on one core made the rounds bimodal: packet read 3.38M against 1.85M records/s. Attempt B was pinned to two physical cores (3,4) and was stable. The hetzner1 CI runner kept the host busy for most of the 3h I had: see `sitting.log` in the job directory. Each sitting started only after 8 consecutive 15s samples with load1 < 1.5 and no build or bench process running.
- **toolchain**: `dotnet --list-sdks` = 10.0.401; runtime 10.0.12; linux-x64; JIT defaults (tiered, PGO on). The board was built with `dotnet build bench/paired/cs -c Release` and DefineConstants BENCH_MATCHED.
- **revisions**: schema **e08aed470201a26e7bc6ee00b102cbae20be7329**. This is a SCRATCH commit and is not pushed: this branch's c9b8ab2b6 plus the proposed emitter fix below. serialize 93b8ea2a958e; serialize.cs v1.9.1 (1bf2b19), the CI pin. The tree was dirty only in build-regenerated `generated/` files, as the board names.
- **corpus_id(identity)**: `12224b99745dc057`, the union of both wires' files, because one invocation loads both. The plan lane has no corpus id.
- **iterations**: 4,000,000 records per sample per wire. Each round is 1 discarded warmup plus 1 measured run, with the two wires alternating which runs first.
- **bytes per record**: fixed 1264.3, packet 438. Checks: table `contract`, packet `removed`.

## Why the measurement needed a scratch commit (base red, not this card's)

At BASE (`d713c7d66`) the C# paired table leg does not compile. The run reproduced it with
`dotnet build bench/paired/cs` on the unchanged base, with SDK 10.0.401 and serialize.cs v1.9.1:

    generated/bench/paired/cs/BenchTable.cs(8088,28): error CS9342: Operator resolution is ambiguous
    between 'Int128Value.operator <(Int128Value, Int128Value)' and 'Int128.operator <(Int128, Int128)'

The fixed-form clamp compares an `Int128Value` field against a `System.Int128` literal. The
runtime converts implicitly in both directions, so the compiler cannot pick an operator. The fix
lives in `internal/codegen/cstable/fixedbounds.go`, which is outside this card's PATHS, so it
is proposed here and not committed. With it, `go test ./internal/codegen/cstable/` is green:

```diff
diff --git a/internal/codegen/cstable/fixedbounds.go b/internal/codegen/cstable/fixedbounds.go
index f00fac725..cbfa16674 100644
--- a/internal/codegen/cstable/fixedbounds.go
+++ b/internal/codegen/cstable/fixedbounds.go
@@ -364,13 +364,29 @@ func fixedWriterClampEnds(f *ir.Field, widthBytes int, lo, hi *big.Int) (low, hi
 // csClampBoth is a two-ended clamp: the count first, then the clamp, so the
 // count reads the value the wire carried and not the one the clamp just wrote.
 func (g *tableGen) csClampBoth(expr, lo, hi, ind string) {
-	g.pf("%sif (%s < %s) { %s = %s; clamped++; }\n", ind, expr, lo, expr, lo)
-	g.pf("%selse if (%s > %s) { %s = %s; clamped++; }\n", ind, expr, hi, expr, hi)
+	g.pf("%sif (%s < %s) { %s = %s; clamped++; }\n", ind, wideOperand(expr, lo), lo, expr, lo)
+	g.pf("%selse if (%s > %s) { %s = %s; clamped++; }\n", ind, wideOperand(expr, hi), hi, expr, hi)
+}
+
+// wideOperand spells the compared side of a 128-bit clamp in the bound's own
+// System type. A field stored as the runtime's Int128Value/UInt128Value
+// converts implicitly to AND from System.Int128/UInt128, so `value < bound`
+// with the bound a System literal is ambiguous (CS9342); naming the System
+// type on the field's side picks one operator, and is a no-op cast for a field
+// already stored as System.Int128/UInt128.
+func wideOperand(expr, bound string) string {
+	switch {
+	case strings.HasPrefix(bound, "unchecked((Int128)"):
+		return "((System.Int128)" + expr + ")"
+	case strings.HasPrefix(bound, "unchecked((UInt128)"):
+		return "((System.UInt128)" + expr + ")"
+	}
+	return expr
 }
 
 // csClampEnd is the one-ended twin: an end the storage's own width already holds
 // is an end the emitter drops (tableClampEnds), so a great many bounded fields
 // spend one compare and not two.
 func (g *tableGen) csClampEnd(expr, op, bound, ind string) {
-	g.pf("%sif (%s %s %s) { %s = %s; clamped++; }\n", ind, expr, op, bound, expr, bound)
+	g.pf("%sif (%s %s %s) { %s = %s; clamped++; }\n", ind, wideOperand(expr, bound), op, bound, expr, bound)
 }
```

## The cause, from a timing split (scratch `--split`, not committed; hetzner1, taskset -c 3,4, after the sitting)

| what is timed, per record | best ns |
|---|---:|
| A: the shipped `FixedTableFixedLoad` over the whole file (the arm's op) | 3126.7 |
| B: `TableFixedWire.Run` alone (the identity plan, interpreted) | 2894.5 |
| C: `FixedTableFixedClamp` alone (the read-side bounds) | 86.4 |
| D: the same 307-entry plan walk with raw loads and **no** delegate call | 249.7 |
| E: one `slots[dst].SetRaw` delegate call per plan entry, with no walk | 2562.5 |

The C# identity lane is a generic interpreter. For each of the 307 plan entries it makes one
`Action<T, ulong>` delegate call into a per-slot lambda (`(t, v) => t.Value.X = ...`).
Those calls cost about 2560 of the 3127 ns. The walk itself, D, costs 250 ns, and that alone
is already below the packet read's 290. The clamp costs 86 ns. Unlike C++, this is not a
missing inlining attribute or a bounds check: a delegate call cannot be inlined at all.

**The profile that would confirm it**: `DOTNET_JitDisasmSummary=1` plus
`DOTNET_JitStdOutFile` shows whether `Run<FixedTable>` reached tier 1 and whether the
lambdas were devirtualised by guarded PGO. `dotnet-trace collect --profile cpu-sampling`
(or `perf` with `DOTNET_PerfMapEnabled=1`, on a host that permits perf; hetzner1 does not)
over `paired.dll --read-arm --iterations 8000000` would attribute cycles to the
`<>c.<FixedTableFixedSlots>b__*` lambdas and their delegate-invoke stubs. The
likely fix is in the emitter: emit a straight-line `FixedTableFixedReadBody` for the
identity plan (the twin of `FixedTableFixedWriteBody`) and keep the interpreter for
lineage plans only. That change is outside this card.

## What this branch carries

- `09f01d5a5`: the C++ read arm (`bb445934b`), cherry-picked because no read arm exists on sprint/schema-cs.
- `c9b8ab2b6`: the C# read arm. `bench/tables/cs/src/Program.cs --read-arm`; `-mode plan-read -langs cs`; `nine.sh --read-arm --leg cs`; readarm tests. These files are OUTSIDE this card's PATHS (the coordinator directed the read-arm build).
- this commit: `board.txt` and this file.

Gate: `go test -count=1 -timeout 600s ./bench/paired/ ./internal/ci/` passes at c9b8ab2b6 and at the unchanged base d713c7d66 (run on hetzner2).

## Not done

- A valid window: both attempts were INVALID, so no GATE verdict was taken. A rerun needs a hetzner1 window with CI drained, pinned to two cores.
- The plan lane: there is no lineage corpus (`bench/corpus/VOLD_bench_lineage.schema`, `bench/corpus/schema.lock`, `bench/paired/corpus/bench_lineage.{bin,layout}` are absent). It is reported absent, not invented.
- The base-red emitter fix (proposed above). The LAND-AFTER card `cs-fixed-class-bodies-take-the-inlining-attribute-measured` had not landed, so this measurement does not follow it.
- The roadmap: no flips, so `make roadmap` was not run.
