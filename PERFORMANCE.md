# Performance

Generated-code performance as time relative to C++ (100%; higher is slower), medians across
the corpus on Apple M2 — the 2026-08-14 five-language pass
([raw CSV](bench/results/2026-08-14-arm64-macbook.csv),
[notes](bench/results/2026-08-14-five-language.md)):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | 180% | 200% | 107% | 160% |
| C# | 198% | 216% | 121% | 176% |
| Go | 328% | 387% | 191% | 207% |
| C | 348% | 530% | 239% | 435% |

**Read the C row knowing what is in it.** `serialize.c` is a compiled translation unit,
not a header, so every runtime call in the C leg crosses a TU boundary that the
header-only C++ runtime does not have — and no leg here is built with LTO, because none
of the other four are either. A labelled `-flto` build of the same C runner, same sitting,
recovers a median 1.11x and up to 2.25x on the paths made of many small runtime calls
([the diagnostic CSV](bench/results/2026-08-14-c-lto-diagnostic-arm64-macbook.csv)). That
is the runtime's packaging, not the generated code.

Relative numbers move with compiler and microarchitecture — treat the table as a dated
snapshot, not a verdict. Full tables, an x86 leg, and per-gap analysis:
[bench/results/](bench/results/).

---

Measurement code and the full tables live in [bench/](bench/).
