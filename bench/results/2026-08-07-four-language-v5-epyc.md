# 2026-08-07 — four-language v5, EPYC leg (IN FLIGHT — WHERE I STOPPED)

**State: the measurement pass is RUNNING on the EPYC, unharvested.** The
session ended mid-pass with the box's network flaking (ssh timeouts in both
directions all afternoon). Everything below is what a next session needs to
finish without re-deriving anything. Nothing in this file is a measured v5
EPYC number yet — the reference numbers quoted are prior records, labelled.

## What this pass is

The deferred x86 leg of the two-day optimization program, authorized by
Glenn verbatim: *"You can do the 'ssh space' pass now, as long as you gate
one profile at a time it should work OK on the one available core. That
core does no work for the space game, it is reserved (core 0)."* Core 0 is
RESERVED for bench (his word); the game server owns isolated cores 1–15.

## The pins (fresh-pulled mains, 2026-08-07 ~15:30Z, GitHub ssh flaky — 3 retries)

| repo | main | note |
|---|---|---|
| schema | `914f43b96` | the v5 merge exactly — no lanes moved past v5, so this stays a **v5** pass |
| serialize | `040d28647` | #31 docs-only on top of #30 restrict |
| serialize.go | `68ce62432` | |
| serialize.rs | `422e4fa03` | |
| serialize.cs | `e2bda998b` | |

schema main vs the M2 v5 pin (`3baa6fd`) differs by results/docs only
(verified `git diff --stat` — bench/run.sh + results files), so the code
measured here is exactly the code the M2 v5 tables measured.

**Suite gate: GREEN on the mac at this pin** — `make SERIALIZE=../serialize
test` at `epyc-pass` (= main + the `--only` harness flag), all four language
test legs OK, exit 0, before anything was staged.

## What is running on the box (launched 2026-08-07 ~16:05Z)

- Staged by rsync (git-archive exports, no .git):
  `~/rowan-bench/v5-epyc/{schema, serialize-cs-port/{serialize,serialize.go,serialize.rs,serialize.cs}, serialize-pre-restrict, pass.sh}`.
  `serialize-pre-restrict` = serialize @ `561e81d` (the commit before the
  #30 merge): verified 0 "restrict qualified this" sites vs main's 3 — the
  A/B isolates #30 exactly.
- Toolchains verified on the box before launch, all identical to every
  prior table: g++ 13.3.0, clang 18.1.3, go1.26.5, cargo/rustc 1.97.1
  (needs `RUSTUP_HOME=~/rowan-bench/rustup CARGO_HOME=~/rowan-bench/cargo`),
  dotnet 10.0.302 (`DOTNET_ROOT=~/rowan-bench/dotnet`).
- Quiet window verified at launch: launcher's busy threads all on PSR 1–15,
  none of ours running, core 0 clean.
- `nohup ~/rowan-bench/v5-epyc/pass.sh` (pid 97642) runs EIGHT legs
  STRICTLY SERIALLY — Glenn's one-profile-at-a-time gate — each leg its own
  `bench/run.sh --only LANG` invocation (the flag added this branch;
  orchestration only, measurement code and flags untouched), with a ps
  quiet-window gate + core-0 /proc/stat snapshot between legs, all logged
  to `~/rowan-bench/v5-epyc/results/pass.log`:

| # | leg | csv | what it answers |
|---|---|---|---|
| 1 | cpp-gcc-start | `results/cpp-gcc-start.csv` | main-table C++ rows + **write-control A** |
| 2 | go | `results/go.csv` | main table |
| 3 | rust | `results/rust.csv` | main table |
| 4 | cs | `results/cs.csv` | main table |
| 5 | cpp-clang | `results/cpp-clang.csv` | open item (c), clang pair |
| 6 | cpp-gcc-prerestrict | `results/cpp-gcc-prerestrict.csv` | open item (a): #30 A/B on g++/Zen 4 |
| 7 | cpp-clang-prerestrict | `results/cpp-clang-prerestrict.csv` | open item (a): #30 A/B on clang-18/Zen 4 |
| 8 | cpp-gcc-end | `results/cpp-gcc-end.csv` | **write-control B** — window stability per the house law |

`~/rowan-bench/v5-epyc/results/PASS.done` appears when all eight finish;
`FAILED-<leg>` markers on any failure. BENCH_NOISE label in every preamble:
core 0 reserved for bench, no game work, still the sole general-purpose
core (irqs, ssh, systemd).

## To finish (the next session's checklist)

1. `rsync space:rowan-bench/v5-epyc/results/ <somewhere>` (retry — the
   network drops for minutes at a time; the run itself is immune, it is
   nohup'd on the box).
2. Check `pass.log` quiet-window entries + no FAILED markers; golden gate
   is per-runner (a runner that fails self-check refuses to bench).
3. Write-control: cpp-gcc-start vs cpp-gcc-end, per-row ratios within
   spread ⇒ window stable.
4. Main table CSV = legs 1–4 concatenated →
   `2026-08-07-four-language-v5-x86_64-epyc.csv` (+ `-clang`,
   `-restrict-ab`, `-write-control` files); replace this doc with the real
   tables (absolute + THE RELATIVE TABLE, C++ = 100%, M2 v5 beside it).
5. Relative-table math (validated this session against the published M2 v5
   table — reproduces 177/204/121/153 // 199/214/140/175 // 323/387/204/198
   exactly): per bench, ratio = cpp_median / lang_median × 100; column =
   median of the 11 corpus ratios; batch cells separately. Release rows
   only (the 2026-08-06 gcc morning CSV carries a Debug section — filter on
   the `# build:` preamble).

## Reference numbers the verdicts compare against (prior records, labelled)

- **(a) restrict x86**: serialize #30 isolated pairing (arm64/apple-clang)
  +152% rigidbody_moving write, +128% at_rest, +86% probebits, +76%
  inputpacket, +74% ship_shallow, +68% probearray, +52% shipcreate, +38%
  testdata, ~0 chat/test/probe_header/batch (predicted byte-copy/inline
  dominated); composed v3→v4 M2: +158/+143/+91/+76/+68/+65/+53/+38%.
  Theory: boundary-aliasing, compiler-dependent — legs 6/7 vs 1/5 price it
  on g++/Zen 4 and clang-18/Zen 4.
- **(c) the g++-vs-clang tiny gap** (morning 2026-08-06 baseline, EPYC,
  pre-everything, `2026-08-06-x86_64-ryzen-{gcc,clang}.csv` — filenames say
  ryzen, preambles say EPYC): clang/g++ probe_header write **4.01x**, test
  write **3.62x**, probebits read 2.16x; g++ AHEAD on probebits write
  (0.74x) and batch write (0.87x). Question: post-const-emit (schema #8),
  is it still 4x? Legs 1 vs 5.
- **(b) lane composition on x86**: EPYC v2 (2026-08-06, pre-everything,
  g++) relative table computed this session from
  `2026-08-06-four-language-v2-x86_64-epyc.csv`: Rust 139/313/171/258,
  C# 293/320/502/193, Go 291/490/254/131 (write/read/batch-write/batch-read
  vs that run's C++). M2 v5 finals beside it: Rust 177/204/121/153,
  C# 199/214/140/175, Go 323/387/204/198. Go write rows were flat v1→v4 on
  M2, C# write rows flat v1→v4, rust testdata read flat v1→v4 — so on the
  EPYC, v2→v5 per-row deltas on those rows read as the lanes' composed
  effect (stated with that provenance, not as isolated pairings).

## Judgment calls on record

- `--only` added to `bench/run.sh` (this branch) so the serial gate could
  be honored per-leg without touching measurement code; each leg carries
  the full preamble.
- Both compilers for C++ (g++ primary, clang-18 pair) per the original
  baseline; the restrict A/B runs under BOTH — the theory says the win is
  compiler-dependent, so one compiler would not answer (a).
- Naming stays **v5** (mains did not move past the v5 merge).
- MSBuild node-reuse disabled + `dotnet build-server shutdown` after the
  cs leg so lingering build servers cannot trip the between-leg ps gate.
