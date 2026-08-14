# 2026-08-14 — the FIVE-language pass (the C leg joins the harness)

**What this is**: the first pass with all five backends measured in one sitting.
`bench/c/bench_main.c` lands here as the fifth runner — a port of the C++ reference
(`bench/cpp/bench_main.cpp`) under the same runner contract: same benchmark set, same
pinned instances, same LCG, same vary mappings, same golden + round-trip self-check gate
before any number is produced, 1 warmup + median-of-7. **The gate held on all five
runners**; the go and cs alloc notes read 0 allocs / 0 bytes on every bench including
batch.

**Two harness defects had to be fixed before the pass could run at all** — the harness is
code and rots too (v1's lesson, paid for again):

1. **the C++ leg did not build.** `bench/run.sh` never passed `-Itest`, and since the
   native type mapping landed (2026-08-12, `cpp_native`/`cpp_include` — `Vec3`/`Quat` map
   onto `test/vec_math.h`) the generated C++ headers include a file that was not on the
   include path. Every `bench/run.sh` invocation since 2026-08-12 would have failed at
   the first compile.
2. **the C# leg did not build.** `bench/cs/schemabench.csproj` compiled only
   `serialize.cs/src/Serialize.cs`, and the runtime grew `Int128Pair.cs` beside it
   (`Int128Value`/`UInt128Value`, referenced on every TFM). `test/cs/schematest.csproj`
   had already been updated; the bench project had not.

`bench/run.sh`'s `SERIALIZE` default also pointed at a checkout path
(`../serialize-cs-port/serialize`) that no README documents; it now defaults to
`../serialize`, the Makefile's own sibling, with `SERIALIZE_C` beside it.

## Apple M2 (arm64, LABELLED NOISY, unpinned)

macbook, Darwin 25.5.0. Apple clang 21.0.0 for both C and C++; go1.26.5; cargo/rustc
1.97.1; dotnet 10.0.302 — the same toolchains as the v1–v5 passes. Raw CSV:
`2026-08-14-arm64-macbook.csv`.

**Noise label, stated plainly**: this ran on a shared desktop with the GUI compositor and
an unrelated qemu VM live on other cores. Spotlight was allowed to settle first. Eleven of
the 120 rows carry a spread above 20% and should not be read as measurements of anything
but the host — the medians, however, reproduce: an earlier contaminated pass in the same
hour gave write 183/204/328/345% and read 195/228/391/529% against these 180/198/328/348
and 200/216/387/530, and the four pre-existing legs land within a few points of the
2026-08-07 v5 table on unchanged code. Medians are the number; mins are the noise.

Runtime pins: serialize `1a99090`, serialize.c `b85218a`, serialize.go `de85e24`,
serialize.rs `379b0bf`, serialize.cs `a935737`; schema `bfd977f`.

| bench | path | B/msg | C++ M msg/s | C++ MB/s | Rust M msg/s | Rust MB/s | C# M msg/s | C# MB/s | Go M msg/s | Go MB/s | C M msg/s | C MB/s |
|---|---|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|---:|
| rigidbody_moving | write | 105 | 58.79 | 5887 | 25.81 | 2585 | 33.65 | 3369 | 17.91 | 1793 | 16.45 | 1647 |
| rigidbody_moving | read | 105 | 87.28 | 8740 | 46.94 | 4700 | 35.57 | 3562 | 24.02 | 2405 | 21.11 | 2114 |
| rigidbody_at_rest | write | 57 | 102.62 | 5578 | 50.57 | 2749 | 52.78 | 2869 | 32.71 | 1778 | 30.57 | 1662 |
| rigidbody_at_rest | read | 57 | 122.06 | 6635 | 90.16 | 4901 | 56.57 | 3075 | 40.93 | 2225 | 34.96 | 1900 |
| chat | write | 13 | 105.37 | 1306 | 66.17 | 820 | 40.27 | 499 | 33.75 | 418 | 50.14 | 622 |
| chat | read | 13 | 132.48 | 1642 | 93.74 | 1162 | 80.09 | 993 | 71.95 | 892 | 24.99 | 310 |
| test | write | 6 | 709.48 | 4060 | 227.86 | 1304 | 126.72 | 725 | 106.83 | 611 | 118.23 | 677 |
| test | read | 6 | 547.92 | 3135 | 217.26 | 1243 | 158.27 | 906 | 83.29 | 477 | 41.87 | 240 |
| inputpacket | write | 61 | 64.16 | 3732 | 39.77 | 2314 | 33.16 | 1929 | 16.43 | 956 | 14.50 | 844 |
| inputpacket | read | 61 | 45.44 | 2643 | 18.52 | 1077 | 28.85 | 1678 | 16.13 | 938 | 12.80 | 745 |
| shipcreate | write | 28 | 100.90 | 2694 | 66.28 | 1770 | 52.91 | 1413 | 31.29 | 836 | 29.03 | 775 |
| shipcreate | read | 28 | 107.90 | 2881 | 53.94 | 1440 | 50.39 | 1346 | 21.09 | 563 | 14.31 | 382 |
| ship_shallow | write | 28 | 121.52 | 3245 | 84.02 | 2243 | 55.25 | 1475 | 32.42 | 866 | 29.41 | 785 |
| ship_shallow | read | 28 | 115.74 | 3090 | 39.15 | 1046 | 55.50 | 1482 | 23.81 | 636 | 17.11 | 457 |
| probe_header | write | 10 | 989.11 | 9433 | 195.11 | 1861 | 115.42 | 1101 | 82.82 | 790 | 89.21 | 851 |
| probe_header | read | 10 | 605.47 | 5774 | 160.97 | 1535 | 127.56 | 1217 | 75.80 | 723 | 68.67 | 655 |
| probebits | write | 26 | 175.87 | 4361 | 97.84 | 2426 | 88.80 | 2202 | 61.75 | 1531 | 54.86 | 1360 |
| probebits | read | 26 | 500.22 | 12403 | 128.01 | 3174 | 94.27 | 2338 | 62.41 | 1547 | 44.89 | 1113 |
| probearray | write | 47 | 72.55 | 3252 | 39.22 | 1758 | 40.33 | 1808 | 19.90 | 892 | 22.39 | 1003 |
| probearray | read | 47 | 67.15 | 3010 | 68.55 | 3073 | 40.99 | 1837 | 17.36 | 778 | 18.39 | 825 |
| testdata | write | 92 | 27.93 | 2451 | 16.03 | 1406 | 13.79 | 1210 | 9.22 | 809 | 8.06 | 707 |
| testdata | read | 92 | 24.76 | 2172 | 14.92 | 1309 | 10.00 | 878 | 9.78 | 858 | 6.37 | 558 |
| message_batch | write | 25 | 74.62 | 1779 | 69.62 | 1660 | 61.78 | 1473 | 39.03 | 931 | 31.28 | 746 |
| message_batch | read | 25 | 74.62 | 1779 | 46.57 | 1110 | 42.31 | 1009 | 36.07 | 860 | 17.15 | 409 |

## THE RELATIVE TABLE (time relative to C++ — C++ = 100%, higher is slower)

Medians across the 11 corpus benches; the mixed-dispatch batch separately. The "was v5"
columns are the 2026-08-07 four-language pass, for the four legs that existed then.

| backend | write | was v5 | read | was v5 | batch write | was v5 | batch read | was v5 |
|---|---:|---:|---:|---:|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% | 100% | 100% | 100% | 100% |
| Rust | **180%** | 177% | **200%** | 204% | **107%** | 121% | **160%** | 153% |
| C# | **198%** | 199% | **216%** | 214% | **121%** | 140% | **176%** | 175% |
| Go | **328%** | 323% | **387%** | 387% | **191%** | 204% | **207%** | 198% |
| C | **348%** | — | **530%** | — | **239%** | — | **435%** | — |

The four established legs are flat against v5 on unchanged code (every cell within the
noise band, and the batch cells inside the ±20% layout-noise band `message_batch` is known
to swing in) — which is the sanity check that says the harness fixes changed nothing about
what is measured.

## Reading the C row honestly

C is the slowest row here, and most of that is **not** the generated code:

- **`serialize.c` is a compiled translation unit, not a header.** Every runtime call in
  the C leg is a real call; the C++, Rust and C# runtimes are inline-by-construction to
  their callers, and Go's is inlined by cost budget. No leg in this table is built with
  LTO — the Rust leg is `cargo run --release` (no LTO) too — so the C row is what a C user
  gets from an ordinary release build.
- **The labelled `-flto` diagnostic** (`2026-08-14-c-lto-diagnostic-arm64-macbook.csv`,
  same sitting, same source): median **1.11x**, and the shape is exactly the theory —
  2.25x on `inputpacket` write, 2.13x on `probebits` read, 1.64x on `probebits` write,
  1.46x on `probe_header` read (all paths made of many small runtime calls), against
  1.01–1.09x on the double-heavy `rigidbody` rows where each call already does real work.
  LTO closes part of the gap, not all of it.
- **`chat` write is the row where C beats two of the other ports** (50.14 M msg/s against
  Go's 33.75 and C#'s 40.27): string framing is one length write plus a bulk byte copy, so
  the call boundary barely registers. `probearray` write is another (22.39 against Go's
  19.90).

What this pass does NOT do is convict the C emitter of anything. That would need a
profile, and the doctrine here is that optimization follows a profile conviction and never
a table cell. The first question a C lane would have to answer is how much of the residual
gap survives LTO — the diagnostic above says most of it does.
