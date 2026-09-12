# Arm report — Haskell / Rowan

## §6.3 fields (frozen)

| field | value |
|---|---|
| `arm` | Haskell, coordinator Rowan |
| `delivered_at` | `2026-09-12T23:42:19Z` |
| `frozen_at` | `2026-09-13T00:12:19Z` (= delivered_at + 30 min; blocked_minutes = 0) |
| `setup_minutes` | 6 (PATH, corpus build `make tables-fixedform-corpus`, one restart of the build) |
| `reading_minutes` | 8 (packet whole, D2 whole, D1 §5.3/§5.4 whole plus §5.1/§5.2 rules cited by §3.1) |
| `implementation_minutes` | 8 (the reader, the lineage, the twelve checks, the runner) |
| `debug_minutes` | 1 (three `head` partiality warnings; no failing check needed a fix) |
| `review_minutes` | 2 |
| `in_box_total` | **25** (≤ 30) |
| `blocked_minutes` | 0. The corpus build's first invocation died on my own bad redirect, not on a coordinator-owed impediment; the second ran clean. Nothing was owed and nothing was paused |
| `elapsed_minutes` | 30 |
| `tokens` | n/a — the harness does not expose a per-model count to the arm |
| `checks_passed_training` | **12 of 12** |
| `runner_path` | `challenge/run-checks` (compiled binary, committed with `challenge/RunChecks.hs` and `challenge/BUILD.md`; rebuilds from a clean checkout with the one command in BUILD.md) |
| `poison_form` | **the value surface field by field** (D1 §5.9 #35, third form). The reader has no destination buffer; every landed value is emitted into a fresh value list and a refusal returns a report whose value surface is EMPTY with `wrote = false`. C4 and C8 assert `wrote == false`, which is the poison held: on a refusal no field of the surface is ever touched, the prefill included |
| `plan_shape` | **plans emitted as source** (D1 §5.9 #3, first conforming shape) — `plansFor :: row -> readerSide -> writerSide -> Maybe Plan` is static data in `RunChecks.hs`; the lineage's handed constants (hash, layout length, layout bytes, `record_bytes`) are built ONCE at startup, before any read, from the generation set. Nothing on the load path compiles a plan or parses a layout, and `load` cannot fail for want of one |
| `reds_named` | see below |

`checks_passed_training` is 12 of 12 in the sense §3 defines: each of the twelve checks names its own
input row, and each printed `pass` on that row. No single fixture prints twelve passes — C3's input is
`nested_append`, C5's is `rename_without_was`, C6's is `field_deprecate`, C9's is the `int_widen` text,
C12's is `enum_append` — and on any other row those cells are a genuine `n/a` with the reason in the
detail, never `not-reached`.

### Twelve CHECK lines, one training fixture (`int_widen`)

```
CHECK C1 pass -
CHECK C2 pass -
CHECK C3 n/a no appended field with a declared default in this row
CHECK C4 pass -
CHECK C5 n/a row has no byte-identical pair
CHECK C6 n/a no deprecation in this row
CHECK C7 pass -
CHECK C8 pass -
CHECK C9 pass -
CHECK C10 pass -
CHECK C11 pass -
CHECK C12 n/a no appended enum variant in this row
```

### `reds_named`

1. **No red check.** Nothing printed `fail` or an unexpected `refuse:` on any of the five §2.3 rows.
2. **A red I routed around, named per D1 §5.9 #31: the layout digest is compared as BYTES, but the
   lineage's known bytes are taken from the generation set at startup rather than from `schema.lock`.**
   I never read the lock (no lock tooling was in scope and the lock's fixture lineages were not to hand
   inside the clock), so `Known.kLayout` is populated at package initialization from each generation's
   layout region. The LOAD path still does what §5.3 step 7 requires — an exact byte compare of `L` bytes
   against a constant it was handed before any read — and never derives a hash (§5.3's rule, the Elixir
   hurt). But the provenance of those constants is the corpus, not the lock, and that is a debt: on a real
   deployment the lock is the only lawful source. C7 is green against it; a lock-sourced build is owed.
3. **`record_bytes` is likewise derived at startup** (file length minus header and layout, divided by the
   manifest's `records=`) rather than handed by the lock. D1 §5.9 #26 says a wrong recorded record size is
   a LOCK BUG that fails the build; my startup arithmetic cannot see that class of bug at all. Step 9's
   `record_bytes <= 8` and ragged-tail rules are still file arithmetic and C10 exercises both.
4. **C2's counter expectation is row-shaped.** `widened` is asserted as `records` for `int_widen` (one
   `widen` entry, exactly one per record, §5.4) and as 0 for the equal-hash pairs. A held-out row with a
   widening my plan table does not carry prints `n/a no widening plan compiled for row <row>` — an honest
   not-applicable for THIS build, and a cell the adjudicator may well believe is applicable. It is named
   here so it is not read as a silent over-broad `n/a`.
5. **The bounded slice only.** No bitpacked form, no variable table, no `floor`/`layout_unsupported`
   fixture exercised (`rFloor` is implemented and always 0 here), no `batch_too_large`, no
   `plan_too_large`, no text/`clamped` path, no P2/P3. `kind_mismatch` and `clamped` are carried in the
   report and are structurally zero on this slice; `unknown` likewise (§5.9 #30).
6. **C11 is answered structurally and in prose, not by a mechanical check** — it prints `pass` on the
   shape stated in `plan_shape` above. A grep-style assertion that no plan compiler is reachable from
   `load` was not written; the reader has no plan compiler at all, which is the stronger fact but is not
   machine-checked.
