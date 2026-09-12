# Arm report — Common Lisp, Rowan

| field | value |
|---|---|
| `arm` | Common Lisp (SBCL 2.6.8), coordinator Rowan |
| `delivered_at` | 2026-09-12T23:42:01Z |
| `frozen_at` | 2026-09-13T00:12:01Z (arithmetic). **The work stopped early: the frozen commit is 23:52:56Z**, so about 19 minutes of the box went unspent |
| `setup_minutes` | 2 (branch; `make tables-fixedform-corpus`, incl. symlinking the sibling `serialize` repo) |
| `reading_minutes` | 3 (this packet WHOLE; D2 WHOLE; D1 §5.1–§5.4 whole — **§5.5–§5.9 were NOT read inside the clock**) |
| `implementation_minutes` | 4 |
| `debug_minutes` | 1 (`sb-ext:process-exit` is not a condition; `was =` ids; union/array paths; C9's reorder edit; C12's applicability) |
| `review_minutes` | 1 (all 18 training rows swept; determinism; clean-checkout run; the red-before-green run below) |
| `in_box_total` | **11** — measured against the wall clock, not the budget |
| `blocked_minutes` | 0 — the first corpus build failed for want of the sibling `serialize` repo (`test/tables/fixedform_dump.cpp` includes `serialize.h` from `../serialize`); repaired in-box by symlinking `/Users/glenn/serialize` beside the workspace, so no pause is claimed |
| `elapsed_minutes` | 10.9 (23:42:01Z delivered → 23:52:56Z frozen commit) |
| `tokens` | n/a — the harness exposes no per-model count in this session; no estimate is offered |
| `checks_passed_training` | **12 of 12.** Every one of the twelve is written and every one is green on every training row where it applies; zero `fail` and zero unexpected `refuse:` across all 18 training pairs, always twelve lines, always exit 0 |
| `runner_path` | `challenge/run-checks` — `#!/opt/homebrew/bin/sbcl --script`, shell-free, SBCL standard library only, runs from a clean checkout of this branch |
| `poison_form` | **byte pointer** (D1 §5.9 #35's first target): the destination is one flat byte vector of the reader's record images, allocated filled with `0x5A`, and C4/C8 assert every byte is still `0x5A` after the refusal |
| `plan_shape` | §5.9 #3's **SECOND** shape — plans are built ONCE at image start, before any load, from layout bytes HANDED IN; `LOAD` selects by index on the header's hash taken as given, memcmps the layout against the known bytes, and parses nothing. C11 is a **proof, not a promise**: `parse-layout` signals if it is ever entered while `*in-load*` is bound, and it never is |
| `reds_named` | **(1) The lock is not in the packet**, so the lineage's layout bytes, record body sizes and wire hashes are handed in from the pinned corpus pair's own headers at plan-build time rather than from `schema.lock` — a named deviation from §5.2, not a load-path parse. **(2) `clamped` never moves**: no bounds pass was written, since every expectation in the slice is `clamped == 0`; a row that should clamp would go unnoticed by this reader. **(3) The value surface models only integer, bool and enum leaves inside tables.** Arrays, keyed arrays, unions, optionals, text and floats are NOT modelled: a manifest key naming one resolves to nothing and the check reports `n/a` with the reason, never `fail`. That is where an adjudicator should read this arm's `n/a` hardest — it is a genuine inapplicability of THIS reader, not of the packet's shape. **(4) `layout_unsupported`, `batch_too_large`, `plan_too_large`, `layout_record_too_large` and the form-byte refusals are written but untested** — no fixture in the slice reaches them. **(5) D1 §5.5–§5.9 unread**, so the closure rule, the retirement floor, the two-pass guard split and the aux lane are absent by ignorance as much as by scope |

## Evidence the checks can reject a wrong reader

A copy of the reader with ONE change — the ladder widen ZERO-extends instead of taking the sign from the
WRITER's kind, which is the clamping reader's first error — fails C2 on the manifest's own value and passes
C1 unchanged:

```
CHECK C1 pass returns 1, unknown=0 kind_mismatch=0 widened=0 clamped=0
CHECK C2 fail r0.v landed 65535 expected -1
```

A second copy with ONE change — the prefill moved BEFORE the per-record hash gate, which is the C++
reference's documented divergence (D1 §5.8 row 9) — fails C8 while C4 stays green, so this arm's C8 does
separate "implemented the page" from "copied the reference":

```
CHECK C4 pass layout_newer, poison 0x5A intact, nothing written
CHECK C8 fail no_layout but the prefill ran first
```

## The twelve lines, `int_widen` (training)

```
CHECK C1 pass returns 1, unknown=0 kind_mismatch=0 widened=0 clamped=0
CHECK C2 pass sign-extended, unknown=0 kind_mismatch=0 widened=3 clamped=0
CHECK C3 n/a row int_widen appends no field with a nonzero declared default
CHECK C4 pass layout_newer, poison 0x5A intact, nothing written
CHECK C5 n/a row int_widen moves a layout byte, so it has no byte-identical pair
CHECK C6 n/a row int_widen moves a layout byte, so the two hashes differ
CHECK C7 pass layout_malformed on a flipped layout byte under a known hash
CHECK C8 pass no_layout before the prefill, poison 0x5A intact
CHECK C9 pass 6 of 6 edits refused by name with both values; int16 accepted
CHECK C10 pass truncated and ragged both malformed, no reason
CHECK C11 pass plans built once at image start from handed layout bytes; the layout parser SIGNALS if reached from LOAD and never is; LOAD selects by index on a handed hash and memcmps
CHECK C12 n/a row int_widen has no appended enum variant
```

C9's six findings, verbatim, on `int_widen`:

```
(a) IntWiden: v: narrowed (int16 -> int8)
(b) IntWiden: v: signedness (int16 -> uint16)
(c) IntWiden: v: ladder (int16 -> float32)
(d) IntWiden: v: field removed (v -> absent)
(e) IntWiden: lead: fields reordered (lead,v,trail -> lead,trail,v)
(f) IntWiden: v2: field inserted not at the end (lead,v,trail -> lead,v2,v,trail)
```

`nested_append` lands `C3 pass w=88 prefilled`; `rename_without_was` lands `C5 pass` (four reads, the
identity plan asserted in all four) and `C6 pass`; `field_deprecate` lands `C5`/`C6` pass; `enum_append`
lands `C12 pass`, and `enum_width` correctly reports C12 `n/a` because the ordinal WIDTH grew, which is not
the append case.

Familiarity is disclosed: Rowan has worked this slice and its lane. This is not a cold read and is not
reported as one.
