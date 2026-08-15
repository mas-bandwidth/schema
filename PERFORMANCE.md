# Performance

Generated-code performance as time relative to C++ (100%; higher is slower), medians across
the corpus on an **Apple M3 Ultra**, the 2026-08-15 five-language pass
([raw CSV](bench/results/2026-08-15-arm64-studio-postlane-O3-pass.csv),
[inline verdicts](bench/results/2026-08-15-arm64-studio-postlane-O3-pass.inline)):

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C++ | 100% | 100% | 100% | 100% |
| C | 163% | 142% | 126% | **70%** |
| Rust | 149% | 163% | 107% | 168% |
| C# | 194% | 224% | 169% | 231% |
| Go | 356% | 460% | 317% | 242% |

Interleaved, seven measured rounds, control legs bracketing the pass at a 0.9% delta, every
leg built from a named upstream commit the harness verified against each toolchain's own
resolution before the first measurement. The methodology is normative and lives in
[bench/BENCH-STANDARD.md](bench/BENCH-STANDARD.md); it is stricter than the table, and it
refuses to print a ratio it cannot justify.

## What changed, and why the C row moved

The previous published table (2026-08-14, Apple M2) put C at 348% write and **530%** read,
with batch read at 435%. The gap was never the generated code. It was a compiler heuristic
nobody had named: **LLVM prices an `Ok`/`Err` split at roughly even odds, so a fallible
serialize chain's block frequency decays geometrically, and a few fields in, every remaining
call site is held to the cold inline threshold instead of the hot one.** Reads decay. Writes
never do, because writes cannot fail. That one word — *fallible* — explains a read/write
asymmetry that had been sitting in these libraries since they were written.

The remedies did not transplant even though the mechanism did: Rust wanted its error
constructor pinned cold, C wanted the demand placed on the read spine, and in C++ the same
cold hint activated the machine outliner and cost 25%. Same disease, three prescriptions,
each measured. C now leads C++ on batch read.

Two honest caveats on the comparison. The machine changed (M2 → M3 Ultra), so the two tables
are not a controlled before/after — the C read movement is far outside machine variance and
the mechanism is confirmed by inline verdicts, but the exact figures are not subtraction. And
the C row's remaining gap is packaging: `serialize.c` is a compiled translation unit, not a
header, so every runtime call crosses a boundary the header-only C++ runtime does not have,
and no leg here is built with LTO because none of the other four are.

## Reading the table honestly

**The ratios cross safety contracts, and the harness says so in captions rather than hiding
it.** C++ compiles its debug asserts and bounds checks out; C validates the wire and API
contract in every build; Rust, C# and Go carry bounds, range and sticky-error checks in every
build by contract. A ratio between two of those columns includes the price of a different
promise. The clearest evidence that the price is real: C and Rust — the two per-field-checked
writers — land within 5% of each other on every round-trip write row, from wholly independent
implementations. Two minds arriving at the same cost for the same guarantee.

Relative numbers move with compiler and microarchitecture. Treat the table as a dated
snapshot, not a verdict. Full tables, the pre-campaign baseline of the same day, and
per-gap analysis: [bench/results/](bench/results/).

---

Measurement code and the full tables live in [bench/](bench/).
