# C: the fixed read against its own packet read — both lanes FAIL

Card `c-fixed-read-beats-its-packet-read` (roadmap tasks `gates/packet-read/c/identity` and
`gates/packet-read/c/plan`). Both GATE verdicts are **FAIL**, in two sittings with an OK window,
so the roadmap tasks stay open. They are not flipped.

## The C line of the board

Each value is the best round of 7, in ns/record. The full boards and every round are in
[board.txt](board.txt).

| sitting | identity read | plan read | packet read | GATE identity | GATE plan | plan/identity (reported) |
|---|---:|---:|---:|---|---|---:|
| 1 | 250.2 | 705.9 | 164.2 | FAIL | FAIL | 2.82 |
| 2 | 250.5 | 703.3 | 164.2 | FAIL | FAIL | 2.81 |

Spread is (max − min) / median, taken over the rates (§2.3). In sitting 1 it was 5.9% for
identity, 3.4% for plan and 0.2% for packet. In sitting 2 it was 5.4%, 3.9% and 0.4%. No row is
marked noisy or invalid. The driver printed both GATE verdicts (`faster()`, best round against
best round, each lane against C's own packet read). This file only copies them.

- **The identity lane is 1.52x slower than the packet read** (250 vs 164 ns/record).
- **The plan lane is 4.3x slower than the packet read** (704 vs 164 ns/record).

## Declared

- **Host:** amd-3 (its hostname is `b3`), an AMD Ryzen 7 255, linux/amd64. At the start it was
  the quietest of amd-2, amd-3 and hulk: load1 was 0.13. hulk was given up after 28 attempts,
  because another worker's dotnet/C++ build-and-bench loop held it.
- **Pinning:** `taskset -c 5`, recorded as lane 5 on the board. The governor was `powersave`
  (amd-pstate), declared and not changed.
- **Window:** OK in both sittings. The quiet window was enforced by the driver, with a
  competing-process refusal at each sample. The window refused one attempt (`make` during
  round 6, kept as `refused-1-1`) and an earlier hulk sitting (`go`, then `dotnet`). Neither is
  used here.
- **Load1:** 2.95–7.55 in sitting 1 and 1.56–2.95 in sitting 2. The load is declared. No
  competing build or bench process was running by name.
- **Control:** the C packet control leg ran at the start and the end of each sitting. Its delta
  was 0.03% in sitting 1 and 0.28% in sitting 2 (the bound is 5%).
- **Receipts:** the receipt is the worker's own READY/START line, recorded in each board's
  footer. It says outright that there was no human operator.
- **Revision:** schema `b339d654571dd5cf92ad13479a7252da8ae134bf`. The only dirty files were the
  build-regenerated `generated/` headers, named on the board. serialize.c was at `ae609eba`.
  The compiler was gcc 15.2.0 at `-O3`, using the C table leg's flag line plus `-DBENCH_PLAN`.
- **Corpus:** one invocation hashes one corpus. `corpus_id` was `0930f96c8320a75d` for the
  identity, plan and packet rows alike: it covers the table corpus, the packet pair and the
  lineage pair. The ordinary passes' ids were `table 066b2822b3e5d487` and
  `packet fd0748942dc6ae09`.
- **Iterations:** 4,000,000 records per sample per wire. Each round was 1 discarded warmup plus
  1 measured run, for 7 rounds per sitting, and the wire order rotated by round.
- **Bytes per record:** fixed 1264.3, lineage file 995.4, packet 438.
- **Packet read:** it was measured, not derived. It is one read stream plus the generated
  `read_bench_mixed` over the packet leg's own 64 variants, timed alone.
- **Packet read build:** it is compiled in the table runner's translation unit, so it gets that
  unit's `-ffp-contract=off`. The packet leg itself does not get it. Checks are `removed` on
  both builds (NDEBUG).
- **Plan lane:** it reads the Rust leg's lineage corpus (`bench/tables/rust/lineage/` on
  337aaacf). The copy is byte for byte, and its blob ids are identical. It is a load of 64
  records one generation back, through the lock entry's static plan. The unit is generated a
  second time beside `schema.lock`.
- **Plan lane gates:** all of them run before the clock:
  - the lineage is the lock's;
  - the read is clean except for `widened`, which is 4096, i.e. 64 per record;
  - a 0xA5-poisoned destination saves the same bytes;
  - the answer differs from the identity corpus.

## The cause, and the profile that would confirm it

The following are diagnostics, not certified. They were taken on amd-3 core 5, 3 rounds each,
outside the window, with the build edited in `tmp/` only and nothing committed.

- **The clamp pass is most of the identity read.** The straight-line read-side bounds pass,
  `schema_bench_fixed_table_fixed_clamp_`, runs after the plan loop on both plans. With it
  disabled:
  - identity falls from about 250 to **72–80 ns/record**, faster than the packet read's 164;
  - plan falls from about 704 to about 549 ns/record.

  This points to the clamp pass, which is about 170 ns of every record, as the identity lane's
  cause. My best guess is the serial `(*clamped) +=` add chain across roughly 1000 bounded
  fields, which a compiler cannot vectorize as written. I have not confirmed that.
- **Aliasing is not what costs time in C.** I restrict-qualified the `clamped`/`damaged`
  counter pointers in all seven clamp bodies, which is the remedy proposed for C++. It changed
  nothing: identity stayed at 250 ns and plan at 704 ns. The C emitter already keeps the
  counters local, written back once, and passes them by pointer into `always_inline` bodies,
  so GCC resolves the alias on its own. So the C++ card `cpp-fixed-clamp-counters-are-restrict`
  does not reproduce for C. The clamp cost does, but it is not an aliasing cost.
- **The plan lane's remaining gap** after the clamp is about 549 vs 164 ns. It is the plan loop
  itself: 42 identity entries against an older layout's static plan, plus the widening of 64
  stats per record.
- **The profile that would confirm both** is
  `perf record -e cycles:pp` + `perf annotate fixed_table_fixed_load` on build/paired/plan-c,
  pinned. A cheaper complement is `llvm-mca` over the inlined clamp body. perf is blocked for
  this worker on amd-3 (`perf_event_paranoid = 4`), and changing that is the owner's call.

## What the card built

- **bench/tables/c/table_main.c `--read-arm`:**
  - `bench_fixed_read` is the identity lane.
  - `bench_mixed_read` is the packet read, using the same generated reader as the packet leg.
  - with `-DBENCH_PLAN`, `bench_plan_read` is added.
  - every gate runs before the first clock, and the order rotates by `--round`.
  - the SHAPE-GATE ledger count went from 9 to 10 (one `BenchMixed` typedef).
- **bench/paired:**
  - `-mode plan-read` takes `-langs cpp` or `-langs c`, one leg per sitting.
  - `-mode build` builds `build/paired/plan-c` beside the lineage lock when the lineage is on
    the tree, and records the binary, its generated headers and the lineage files.
  - the C packet control leg brackets the sitting.
  - GATE identity and GATE plan are each judged against the leg's own packet read.
  - `-lane` is recorded for plan-read.
  - `nine.sh --read-arm --leg c --pin CORE`.
- **The C++ driver half** came verbatim from 2ac615b86 as its own commit, 1f77d6987, so the two
  branches merge cleanly in the carried part.
- **bench/tables/c/lineage/:** the Rust lineage corpus, byte for byte.

## Gate

The gate is `go test -count=1 -timeout 600s ./bench/paired/ ./internal/ci/`, run on amd-2 at
b339d6545:

- `ok  github.com/mas-bandwidth/schema/v2/bench/paired 2.213s`
- `ok  github.com/mas-bandwidth/schema/v2/internal/ci 0.923s`

`go run ./bench/tools/shapegate` is clean: 46 files carry shape knowledge and every one is on the
ledger.

## Not done, and proposals

- **The roadmap flips are not done.** Both lanes FAIL, so `gates/packet-read/c/identity` and
  `gates/packet-read/c/plan` stay open.
- **The C build was not blocked at BASE.** The C `uint0_t` fix card and the `test/c-tables`
  `table_fixed_run` arity red (owned by `schema-ci-legs-green-on-dev`) did not stop the paired C
  runner from building.
- **The C++ runner is missing on this branch.** `-mode plan-read -langs cpp` refuses at its
  read-arm gate here until that card's `bench/tables/cpp/table_main.cpp` lands. That file is
  outside this card's PATHS.
- PATHS-PROPOSED: `bench/corpus/VOLD_bench_lineage.schema`, `bench/corpus/schema.lock`,
  `bench/paired/corpus/bench_lineage.{bin,layout}`. This would give the lineage pair its standard
  home (§1.10, items 1–6) once, and retire the two leg-local copies, the Rust one and this one.
- PATHS-PROPOSED: `bench/BENCH-STANDARD.md`. This would add a state line under the work list:
  the C leg's `-mode plan-read -langs c` and `--read-arm` have landed, including the plan lane
  (items 8–9 for C).
- PATHS-PROPOSED: `bench/c/bench_main.c` / main.go `cFlags`. This would build the packet leg
  with `-ffp-contract=off` like the table leg, so the comparand inside the table unit and the
  packet leg share one flag line.
