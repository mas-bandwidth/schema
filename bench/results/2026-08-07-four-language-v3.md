# 2026-08-07 — four-language comparison v3 (the post-merge mains)

**What v3 is**: the day's endpoint. v1 (`2026-08-06-four-language.md`)
established the baseline and the hotspots; v2
(`2026-08-06-four-language-v2.md`) fixed the harness read-alloc asymmetry;
the day's optimization program then landed across all five repos. v3 is the
mains, fresh-pulled, at exactly these commits — that pin is the definition,
and any PR merged after the pull is explicitly out of v3:

| repo | main @ v3 |
|---|---|
| schema | `41c1ff587e1c027364d5813a83de35706c13f3ec` |
| serialize | `378892c4d4574917bf384da284bfafd4fc685f7c` |
| serialize.go | `18b2439aeade8774b9539cb1d7e19aa0b944fe66` |
| serialize.rs | `422e4fa031b58d6837ca0f10df8d43a66e0d5abe` |
| serialize.cs | `e2bda998b234142fd0dae4fe7d845bc20ea06e8e` |

What those mains contain that v2's run did not: schema #3 (union tag-only
init), #5 (rust emitter `#[inline]` + `read_message_into`), #6 (rust
borrow-in-place writes), #7 (C++ bulk-bytes), #8 (const-emit fold);
serialize #25/#26/#27 (const-params surface, fixed-point + 128-bit,
WriteBytes head/tail packing); serialize.go #16/#17 (fixed-point port, read
window inline); serialize.rs #18/#19/#20 (fixed-point port, trait-default
`#[inline]`, `WriteStream::write_bytes`); serialize.cs #1/#2/#3
(fixed-point port, AggressiveInlining, WriteBatch/ReadBatch runtime).

**Host coverage: M2 only. EPYC deferred on Glenn's word** ("stick with m2
for now") — no quiet-gate check, no remote run this pass; the EPYC leg
rejoins a later pass.

Same methodology as v1/v2 (`bench/README.md`), same harness (v2, read-alloc
fixed, now on main): golden + round-trip self-checks gate every runner,
escape barriers, LCG variation, 64 variant read buffers, 1 warmup +
median-of-7, one `bench/run.sh` invocation. **The golden gate held on all
four runners**, and the go/cs alloc notes read 0 allocs / 0 bytes on every
bench. v2 and v3 are directly comparable on both paths (identical harness);
v1 reads are not (v1 measured harness alloc on top of serialize work).

## Apple M2 (arm64, quiet, unpinned)

macbook, Darwin 25.5.0. Apple clang 21.0.0 `-O3 -DNDEBUG -DSERIALIZE_RELEASE
-std=c++17 -ffp-contract=off -fno-rtti`; go1.26.5 (`go run`, default
optimized); cargo/rustc 1.97.1 (`--release`, opt-level 3, no LTO); dotnet
10.0.302 (`-c Release`, workstation GC) — toolchains identical to v1/v2.
Raw CSV: `2026-08-07-four-language-v3-arm64-m2.csv`.

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | Go M msg/s | Go MB/s | C# M msg/s | C# MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 22.71 | 2275 | 26.16 | 2619 | 16.94 | 1696 | 15.52 | 1554 |
| rigidbody_moving | read | 105 | 85.47 | 8558 | 46.73 | 4680 | 23.39 | 2342 | 16.94 | 1696 |
| rigidbody_at_rest | write | 57 | 43.26 | 2352 | 51.20 | 2783 | 29.21 | 1588 | 29.21 | 1588 |
| rigidbody_at_rest | read | 57 | 124.42 | 6763 | 88.39 | 4805 | 41.06 | 2232 | 33.75 | 1835 |
| chat | write | 13 | 104.21 | 1292 | 67.51 | 837 | 31.01 | 384 | 40.16 | 498 |
| chat | read | 13 | 133.12 | 1650 | 94.80 | 1175 | 70.15 | 870 | 79.51 | 986 |
| test | write | 6 | 730.00 | 4177 | 229.95 | 1316 | 65.73 | 376 | 109.91 | 629 |
| test | read | 6 | 557.90 | 3192 | 207.01 | 1184 | 85.04 | 487 | 133.42 | 763 |
| inputpacket | write | 61 | 36.84 | 2143 | 39.87 | 2320 | 12.63 | 735 | 14.13 | 822 |
| inputpacket | read | 61 | 45.93 | 2672 | 19.32 | 1124 | 16.22 | 944 | 15.52 | 903 |
| shipcreate | write | 28 | 66.42 | 1774 | 66.18 | 1767 | 17.49 | 467 | 26.52 | 708 |
| shipcreate | read | 28 | 111.53 | 2978 | 54.94 | 1467 | 23.63 | 631 | 31.58 | 843 |
| ship_shallow | write | 28 | 74.50 | 1989 | 82.04 | 2191 | 19.04 | 508 | 30.53 | 815 |
| ship_shallow | read | 28 | 117.60 | 3140 | 38.27 | 1022 | 25.84 | 690 | 37.63 | 1005 |
| probe_header | write | 10 | 990.53 | 9446 | 192.82 | 1839 | 67.40 | 643 | 90.05 | 859 |
| probe_header | read | 10 | 613.29 | 5849 | 159.92 | 1525 | 77.60 | 740 | 110.63 | 1055 |
| probebits | write | 26 | 94.61 | 2346 | 94.45 | 2342 | 43.58 | 1081 | 59.52 | 1476 |
| probebits | read | 26 | 486.84 | 12071 | 116.52 | 2889 | 62.75 | 1556 | 55.86 | 1385 |
| probearray | write | 47 | 44.51 | 1995 | 36.88 | 1653 | 15.03 | 674 | 21.33 | 956 |
| probearray | read | 47 | 66.86 | 2997 | 66.32 | 2973 | 17.32 | 776 | 23.88 | 1070 |
| testdata | write | 92 | 20.36 | 1786 | 14.99 | 1315 | 6.67 | 585 | 6.68 | 586 |
| testdata | read | 92 | 25.54 | 2241 | 10.03 | 880 | 7.25 | 636 | 7.55 | 663 |
| message_batch | write | 25 | 81.80 | 1950 | 67.52 | 1610 | 34.25 | 816 | 54.32 | 1295 |
| message_batch | read | 25 | 66.95 | 1596 | 22.12 | 527 | 37.94 | 905 | 39.02 | 930 |

## v1 → v2 → v3 progression (M2, M msg/s medians — what the day bought)

v1→v2 movement is the harness fix (reads only; writes are the control);
v2→v3 movement is the optimization program. Each row names its cause.

**C++** (schema #7 + #8, serialize #27):

| bench | path | v1 | v2 | v3 | v3/v2 | cause |
|---|---|---:|---:|---:|---:|---|
| testdata | read | 13.70 | 14.02 | 25.54 | **1.82x** | schema #7 bulk-bytes (`[17]uint8` per-byte loop → memcpy; PR measured +70–80%) |
| testdata | write | 17.11 | 16.84 | 20.36 | **1.21x** | schema #8 const-emit (+12.5%) + #7/serialize #27 write half |
| probebits | write | 83.34 | 81.56 | 94.61 | **1.16x** | schema #8 fold (PR measured +14.7% — the outlined runtime `SerializeInteger64` dying) |
| message_batch | read | 31.37 | 67.24 | 66.95 | 1.00 | v1→v2 was the harness fix; schema #3's 2.11x is priced for per-message construction, which the hoisted harness loop no longer performs (see finding 4) |

**Rust** (serialize.rs #19/#20, schema #5/#6):

| bench | path | v1 | v2 | v3 | v3/v2 | cause |
|---|---|---:|---:|---:|---:|---|
| inputpacket | write | 19.42 | 18.72 | 39.87 | **2.13x** | #19 trait-default `#[inline]` (1.89x) + #5 emitter `#[inline]` |
| probearray | read | 28.98 | 31.61 | 66.32 | **2.10x** | #19 + #5 (PR-paired 2.16x) |
| ship_shallow | write | 43.16 | 42.39 | 82.04 | **1.94x** | #19 (PR-paired 1.96x) |
| chat | write | 38.96 | 37.54 | 67.51 | **1.80x** | #20 + schema #6 borrow-in-place (PR-paired +76%) |
| probebits | read | 70.31 | 72.60 | 116.52 | **1.60x** | #19 (PR-paired 1.85x) |
| testdata | write | 9.88 | 9.68 | 14.99 | **1.55x** | #19 (PR-paired 1.57x) |
| message_batch | write | 53.57 | 53.15 | 67.52 | **1.27x** | schema #6 (PR-paired +32…+49%) |
| message_batch | read | 21.15 | 20.61 | 22.12 | 1.07 | #5 dispatch `#[inline]` only — `read_message_into` (2.6x, landed) still uncalled by generated dispatch |

**Go** (serialize.go #17; v1→v2 read jumps are the harness fix):

| bench | path | v1 | v2 | v3 | v3/v2 | cause |
|---|---|---:|---:|---:|---:|---|
| inputpacket | read | 5.91 | 8.67 | 16.22 | **1.87x** | #17 read window inline (PR-paired +43% on the v1 harness; more without the alloc overhead) |
| rigidbody_moving | read | 11.27 | 12.90 | 23.39 | **1.81x** | #17 (PR-paired +61%) |
| rigidbody_at_rest | read | 17.64 | 22.86 | 41.06 | **1.80x** | #17 (PR-paired +48%) |
| probebits | read | 30.43 | 44.84 | 62.75 | **1.40x** | #17 |
| test | read | 48.69 | 67.81 | 85.04 | **1.25x** | #17 |
| message_batch | read | 31.76 | 31.15 | 37.94 | **1.22x** | #17 |
| chat | read | 19.80 | 64.16 | 70.15 | 1.09 | v1→v2 was the harness alloc (3.24x); #17 predicted ~0% here (`SerializeString` allocation dominates — the in-flight strings work) |

**C#** (serialize.cs #2; #3's batch runtime is landed but opt-in and not yet
emitted, so it is invisible here by design — its A-vs-B sweep was flat):

| bench | path | v1 | v2 | v3 | v3/v2 | cause |
|---|---|---:|---:|---:|---:|---|
| message_batch | write | 52.82 | 44.72 | 54.32 | **1.21x** | #2 AggressiveInlining (PR-paired 1.17x) |
| test | write | 111.85 | 93.11 | 109.91 | **1.18x** | #2 (PR-paired 1.18x) |
| test | read | 131.82 | 114.27 | 133.42 | **1.17x** | #2 (PR-paired 1.08x) |
| probe_header | read | 108.81 | 96.68 | 110.63 | **1.14x** | #2 (PR-paired 1.19x) |
| probebits | write | 60.31 | 55.71 | 59.52 | 1.07 | #2 (PR-paired 1.20x from a lower same-sitting before; the v2→v3 delta is the honest cross-session number) |

The C# v1→v2 dip (0.87–0.98x across the board) was session drift, noted in
the v2 doc; v3 puts most C# rows back at or above their v1 levels.

## Findings

1. **Rust writes reach C++ parity at the corpus median.** The median
   per-bench write-time ratio across the 11 corpus benches is 1.00x C++
   (was 1.74x in v2): rust now beats C++ outright on rigidbody_moving
   (+15%), rigidbody_at_rest (+18%), inputpacket (+8%), ship_shallow
   (+10%), and ties shipcreate/probebits. The remaining write gaps are the
   tiny-message rows (test 3.2x, probe_header 5.1x — apple-clang's
   whole-header folding) and string/array-heavy chat/testdata.
2. **Go read parity is CLOSED — reads now beat writes on all 12 benches**
   (read/write 1.09–2.26x). The v2 doc's residue ("reads still trail writes
   on rigidbody, inputpacket, probearray, testdata") is refuted by v3:
   those rows now read at 1.38x, 1.28x, 1.15x, 1.09x their writes. #17 plus
   the v2 harness fix did together what neither did alone.
3. **The day's Go program was read-only, and the write column shows it**:
   go writes moved 0.97–1.01x v2→v3 everywhere. Go's remaining gap to C++
   is now write-shaped as much as read-shaped (median relative time 305%
   write vs 386% read, was 297%/594% in v2).
4. **C++ batch read is flat v2→v3 (1.00x) — and that is the correct
   reading of schema #3, not a contradiction.** The v2 harness hoists one
   reused `Message`, so the constructor memset #3 removed was already
   amortized out of the measured loop; #3's 1.80–2.11x (its own PR pairing)
   is what real code that constructs a `Message` per read gains. The
   harness measures steady-state reuse; the PR priced the construction
   pattern.
5. **One isolated win did not survive composition: serialize #27's M2 chat
   write.** #27's own pairing measured chat write +13.1% (102.88→116.37);
   v3 chat write is 104.21 — 0.98x of v2, right where main sat before #27.
   The combined header state (#25+#26+#27 + schema #8's fold) differs from
   #27's isolated base; suspicion (unproven): inlining/layout interaction
   in the composed `serialize.h`. Flagged in the gap ledger as an open
   attribution item with a profile as the next step. testdata write, the
   other #27 beneficiary, DID compose (+21% v2→v3 with #7/#8).
6. **C++ keeps every read column and the batch crown**; its testdata read
   win (finding row 1 above) widens the C++ lead on exactly the bench the
   other emitters still serialize byte-at-a-time — the cross-language
   bulk-bytes adoption item in the ledger.

## Caveats

- 16 of 96 rows carry within-run spreads over 20% (worst: rust probearray
  read 70.9%, go chat read 52.9%, rust probebits write 45.8%, cs test read
  40.7%). In 14 of the 16 the median sits within ~3% of the max and the min
  is a lone outlier run — occasional desched on the unpinned M2, absorbed
  by median-of-7. The two least clean are cs shipcreate write/read (median
  ~8% below max); cs shipcreate's apparent 0.95x/0.91x v2→v3 drift sits
  inside those spreads and is not called as a regression.
- cpp rigidbody_moving write drifted 0.96x v2→v3 (7% spread; generated
  rigidbody write code is untouched by #7/#8 — layout/session noise, same
  class as the 0.83–1.00x write drift the v2 doc recorded between
  sittings).
- The go and cs drivers still dispatch through function values/delegates
  (one indirect call per op); C++ and rust inline via templates/generics —
  unchanged since v1, documented in the runner READMEs.
- EPYC absent by instruction (see preamble); no x86, no g++/linux-clang
  numbers in v3. The v2 EPYC tables remain the latest x86 data.
- v3 is pinned to the SHAs above by definition: anything merged to any of
  the five mains after the pull (a serialize.go strings PR is known to be
  in flight) is out of v3 and waits for the next pass.
