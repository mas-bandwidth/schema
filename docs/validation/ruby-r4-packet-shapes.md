# Ruby R4 Packet Wire — D1 Code Shape & YJIT Measurement Receipt

Date: 2026-11-18
Machine: `space` (Linux x86_64)
Interpreter: Pinned CRuby `4.0.9` (`dist/ruby-4.0.9/bin/ruby --disable-gems -w`)
Workload: `BenchMixed` (`bench/corpus/Bench.schema`), 64 byte-checked variants (`testdata/wire/bench_mixed.bin` + `bench/corpus/variants/bench_mixed.variants.bin`), 12,800 iterations per run, 5 timed runs after warmup (`TestRubyPacketShapeBench`).

## Measured Throughput

| Code Shape | YJIT | Path | Median `msgs/sec` | Median `MB/s` | Speedup (Straight-Line vs Descriptor) |
|---|---|---|---:|---:|---:|
| Straight-line (`Ref` locals bound) | `--disable-yjit` | `write` | `5,196` | `2.17` | `1.62x` |
| Descriptor walk (`S_DESC`) | `--disable-yjit` | `write` | `3,208` | `1.34` | `1.00x` |
| Straight-line (`Ref` locals bound) | `--disable-yjit` | `round_trip` | `2,925` | `1.22` | `1.98x` |
| Descriptor walk (`S_DESC`) | `--disable-yjit` | `round_trip` | `1,476` | `0.62` | `1.00x` |
| Straight-line (`Ref` locals bound) | `--yjit` | `write` | `18,860` | `7.88` | `1.90x` |
| Descriptor walk (`S_DESC`) | `--yjit` | `write` | `9,910` | `4.14` | `1.00x` |
| Straight-line (`Ref` locals bound) | `--yjit` | `round_trip` | `11,466` | `4.79` | **`2.81x`** |
| Descriptor walk (`S_DESC`) | `--yjit` | `round_trip` | `4,078` | `1.70` | `1.00x` |

## Outcome (Decision D1)

Straight-line generated methods with method-top bound `Serialize::Ref` locals outperform the pure-Ruby descriptor walker on every path with YJIT both off (`1.62x` write, `1.98x` round-trip) and on (`1.90x` write, `2.81x` round-trip), and YJIT accelerates straight-line round-trip throughput by `3.92x` (`1,22 -> 4.79 MB/s`). `internal/codegen/ruby` ships the straight-line emitter as `Generate`, and `TestRubyPacketShapeBench` holds both code shapes to byte-identical wire output across all 64 `BenchMixed` variants under `--disable-yjit` and `--yjit`.
