# Performance

Generated-code performance as time relative to C++ (100%; higher is slower), medians across
the corpus on Apple M2:

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| Rust | 177% | 204% | 121% | 153% |
| C# | 199% | 214% | 140% | 175% |
| Go | 323% | 387% | 204% | 198% |

Relative numbers move with compiler and microarchitecture — treat the table as a dated
snapshot, not a verdict. Full tables, an x86 leg, and per-gap analysis:
[bench/results/](bench/results/).

---

Measurement code and the full tables live in [bench/](bench/).
