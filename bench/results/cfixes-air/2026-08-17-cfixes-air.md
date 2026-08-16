# cfixes-air — C vs C++ on current mains, as shipped (2026-08-17, MacBook Air)

**Era: `cfixes-air`.** Apple M2 (MacBook Air, macbook), Darwin 25.5.0, Apple clang 21.0.0
both legs, `-O3`, NO flags anywhere — what ships is what measured. AC power (charging)
throughout. Mains as-is: **serialize `119f98f`** (#76 read spine demands unconditionally),
**serialize.c `b6d6af5`** (#26 fused `write_bytes` + bulk-copy `read_bytes`, atop #25
header-only), **schema branch `cfixes-air` @ `5b77947`** — bench sources identical to
origin/main `2e77623` (#58 C leg `hdr`); the branch adds measurement tooling only.
Era-marked data: **not comparable** to any other era or host (§5.3 rule 8).

Raw: [pass CSV](2026-08-17-arm64-macbook-air-power-cfixes-mains-pair-O3-twin-pass-r2.csv),
[inline verdicts](2026-08-17-arm64-macbook-air-power-cfixes-mains-pair-O3-twin-pass-r2.inline),
[twin-gate log](2026-08-17-arm64-macbook-air-power-cfixes-mains-pair-O3-twin-pass-r2.twingate),
[twin aggregates](2026-08-17-cfixes-r2-twin-a.csv) ([b](2026-08-17-cfixes-r2-twin-b.csv)),
[bespoke surfaces](2026-08-17-own-bench-serialize.c-interleaved.txt)
([serialize -O3](2026-08-17-own-bench-serialize-cpp-O3.txt)).

**Convention: C = 100%** (Glenn, 2026-08-17: *"make C the reference. It is the 100%. C++ is
measured against C."*). Every percentage below is **C++'s time as a percentage of C's time**
on the §2.2 headline (best rate): **higher = C++ slower, lower = C++ faster.** The `legacy`
column gives the prior eras' convention (C's time as % of C++'s) for continuity with
shipped-air and earlier tables. Both are exact reciprocals; nothing was re-measured to flip.

## The headline (corpus medians, `rel` tool, first C=100 table)

| backend | write | read | batch write | batch read |
|---|---:|---:|---:|---:|
| C | 100% | 100% | 100% | 100% |
| C++ | **155%** | **100%** | **183%** | **103%** |

(§2.3 exclusions from the medians: rigidbody_at_rest write, probearray write — noisy.
Batch cells carry the §1.7 caption: the 4096-message mix is ≈74% bulk bits — those two
columns lean toward memcpy, not the serializer.)

Reads are at parity as shipped. Writes: C leads the corpus 1.55x, batch write 1.83x —
one named absence dominates (below).

## The full table — every row, attributed

C++ % = C++ time as % of C time (C = 100, lower = C++ faster). Legacy = prior convention.
Attribution required outside ±10%.

| bench | path | C best M/s | C++ best M/s | C++ % | legacy | attribution |
|---|---|---:|---:|---:|---:|---|
| rigidbody_moving | write | 605.1 | 57.8 | **1047%** | 10 | **#55** held write-spine demand (WriteVec3 cost 330 / WriteQuat 455 refused at linkonce_odr sites) + quantized write |
| rigidbody_moving | read | 454.8 | 304.3 | **149%** | 67 | quantized-read class (stable since ≥ shipped-air; both sides `full` inline) |
| rigidbody_at_rest | write | 815.7 | 105.0 | **777%** | 13 | **#55** |
| rigidbody_at_rest | read | 481.8 | 481.1 | 100% | 100 | — |
| chat | write | 121.7 | 96.7 | **126%** | 79 | **flipped by #26** (was legacy ~133): C fused write_bytes; C++ WriteBytes still byte-loop + out-of-line (cost 345 > 250) — candidate C, in flight |
| chat | read | 223.9 | 223.8 | 100% | 100 | **moved to parity by #26** (was legacy ~120): bulk-copy read_bytes |
| test | write | 737.4 | 735.2 | 100% | 100 | — |
| test | read | 1029.4 | 1026.8 | 100% | 100 | — |
| inputpacket | write | 100.6 | 65.1 | **155%** | 65 | **#55** (WriteInputPacket cost 565) |
| inputpacket | read | 240.3 | 135.2 | **178%** | 56 | quantized-read class |
| shipcreate | write | 441.4 | 98.4 | **448%** | 22 | **#55** (WriteShipCreate cost 1075) |
| shipcreate | read | 242.5 | 214.6 | **113%** | 88 | quantized-read class (position/rotation/velocity quantized) |
| ship_shallow | write | 415.7 | 119.8 | **347%** | 29 | **#55** (cost 885) |
| ship_shallow | read | 278.3 | 263.8 | 106% | 95 | — |
| probe_header | write | 1030.6 | 1033.2 | 100% | 100 | — |
| probe_header | read | 1409.2 | 1410.5 | 100% | 100 | — |
| probebits | write | 573.3 | 185.2 | **310%** | 32 | **#55** (WriteProbeBits cost 445) |
| probebits | read | 765.0 | 766.8 | 100% | 100 | — |
| probearray | write | 80.3 | 73.3 | 110% | 91 | boundary; C row noisy (18.9%), excluded from medians |
| probearray | read | 140.3 | 137.9 | 102% | 98 | — |
| testdata | write | 36.4 | 27.7 | **131%** | 76 | **#55** (WriteTestData cost 2595; cpp `partial:2`) + C++ byte-loop WriteBytes; C side +25% from #26 widened it |
| testdata | read | 57.2 | 68.6 | **83%** | 120 | **candidate A, verified unlanded**: C emits per-byte loops for byte-aligned `[N]uint8` both directions (generated/c/WireWire.h:772,985) where C++ emits bulk read_bytes (generated/cpp/WireWire.h:495). The one row where C++ leads reads. |
| message_batch | write | 177.0 | 96.6 | **183%** | 55 | **#55** (WriteMessage dispatch cost 1555, cpp `partial:2`) + 74% bulk share favors C's fused write path. NOISY both sides (37/30%) |
| message_batch | read | 148.4 | 143.9 | 103% | 97 | C caught up (+21% via #26 D); was legacy 113. NOISY both sides |
| bench_packet | write | 116.7 | 82.9 | **141%** | 71 | C fused blob write (#26) vs C++ byte-loop WriteBytes (`partial:1`) |
| bench_packet | read | 138.8 | 133.7 | 104% | 96 | — (C row noisy 17.4%) |
| bench_ints | write | 189.2 | 183.9 | 103% | 97 | — |
| bench_ints | read | 176.5 | 188.6 | 94% | 107 | — |
| bench_bits | write | 190.4 | 186.7 | 102% | 98 | — |
| bench_bits | read | 220.4 | 236.3 | 93% | 107 | — |
| bench_mixed | write | 157.3 | 151.6 | 104% | 96 | — |
| bench_mixed | read | 171.8 | 182.7 | 94% | 106 | — |
| bitpacker | write | 59.5k p/s | 59.4k p/s | 100% | 100 | — |
| bitpacker | read | 56.4k p/s | 91.7k p/s | **61%** | 163 | serialize.c bitpacker-read residual — the one raw-packer row where C++ leads; #24's affine cursor took legacy 177→163 (shipped-air), unchanged today (163 exactly) — #25/#26 do not touch this chain |

**No row is UNATTRIBUTED.** Honesty note on the quantized-read class (rigidbody_moving,
inputpacket, shipcreate reads): the class is named and stable era-over-era with both sides
fully inlined, but a #55-grade instruction-level receipt has not been produced for it yet —
that is read-campaign work, flagged.

**The bistable plateau did NOT appear.** Watched for explicitly on the bits-heavy victim
rows (collapsed signatures near 37-38 / 40-43 / 89-94 M msgs/s): bench_bits write 190/187,
bench_mixed write 157/152, probebits read 765/767 — all at full height in every round of
both passes, including the refused r1's best values.

## Delta vs shipped-air (the most recent prior air era) — the effect of #25+#26

C++ side (same configuration, banded): **all 34 rows band-ok**, deltas −4.3%…+4.0% —
the window is certified stable cross-pass, so the C-side deltas below are real movement.
C side (configuration changed: hdr+tu→hdr at #25/#58, serialize.c de097d6→b6d6af5): no
§2.6.1 band exists by rule (first outing of this configuration); deltas are the measured
effect of #25+#26 combined:

| C row | shipped-air → cfixes-air (best M/s) | delta | what moved it |
|---|---|---:|---|
| chat write | 75.1 → 121.7 | **+62%** | #26 B: fused word-wise write_bytes body (string body) |
| message_batch write | 110.3 → 177.0 | **+60%** | #26 B (batch mix ≈74% bulk bits) |
| bench_packet write | 85.0 → 116.7 | **+37%** | #26 B (17-byte blob) |
| testdata write | 29.0 → 36.4 | **+25%** | #26 B (string/bytes members; the [17]uint8 stays per-byte — candidate A is read AND write side in C) |
| message_batch read | 122.5 → 148.4 | **+21%** | #26 D: SERIALIZE_BULK_COPY read_bytes payload |
| chat read | 189.0 → 223.9 | **+19%** | #26 D |
| testdata read | 55.7 → 57.2 | +2.6% | expected mover that did NOT move — its bulk is emitter-side ([N]uint8 element loop, candidate A), not read_bytes; **flagged loudly** |
| all other C rows | — | −4.5%…+0.7% | window skew (matches C++ side −4.3%…+0.4%); i.e. #25's header-only flip cost nothing measurable on non-bulk rows |

Today's string-fix visibility is via chat/testdata only — the family benches still have
**no string row and no wstring row** (known gap, next on the board), and RealPacket is
mid-campaign (chunks 1-2 only).

## Window certification (§2.6, §2.6.1 — first routine outing of the twin gates)

**Published pass (r2):** window **OK** — control legs (C++ family gen, same binary, same
inode) at **0.6%** delta. **Twin gate OK** — first routine §2.6.1 outing: A/A twin legs for
BOTH binaries (7 rounds × [cpp,c,cpp,c], twin positions alternating per round, same inode
verified by `--reuse-build`), all **68/68 rows** within their spread bands. **Bands:** all
34 C++ rows inside their shipped-air envelope; 34 C rows `no-band` (configuration's first
outing — correct, not a pass). Zero §2.3-invalid rows; noisy (>15%) rows: message_batch
all four (25.8-37.4% — historically the noisiest row), probearray write (C), bench_packet
read (C), rigidbody_at_rest write (C++) — all marked, excluded from medians per policy.
Power: AC (charging) throughout. Load: 2.7 start / 4.5 max, foreign_procs 5 (two standing
qemu VMs ~12% each + the operator's own session; named in the noise line).

**Refusal on record (r1):** the first window certified OK on BOTH §2.6 control legs (2.6%)
AND the §2.6.1 twin gate — and was refused anyway by §2.3: c/rigidbody_moving/write spread
46.6% (INVALID), 12 further rows noisy, foreign_procs 7 — `duetexpertd` burned ~a full core
through the early rounds. One-sided dips in mins/medians with bests intact: classic
contention, not the bistable state. Archived at
`../invalid/2026-08-17-...-twin-pass-REFUSED-r1.csv` with the reason appended. The window
re-ran quieter; r2 published.

## Bespoke surfaces (the libraries' own harnesses, their own methodologies)

**serialize.c's own bench** (its Makefile design: C `-O2` vs C++ `-O2` like-for-like,
best-of-5-in-binary × 5 interleaved rounds; NOT comparable to the schema rows — different
level, different harness, and the corpus overlaps but the variation discipline differs):

| row | C best | C++ best | C++ % of C time |
|---|---:|---:|---:|
| bitpacker write (MB/s) | 2093 | 2092 | 100% |
| bitpacker read (MB/s) | 1781 | 2634 | **68%** |
| stream write (M pkt/s) | 113.4 | 82.9 | **137%** |
| stream read | 336.6 | 137.6 | **245%** |
| stream measure | 849.6 | 842.8 | 101% |
| int packet write / read | 178.0 / 367.4 | 170.2 / 187.4 | 105% / **196%** |
| bits packet write / read | 188.6 / 533.6 | 185.3 / 239.6 | 102% / **223%** |
| mixed packet write / read | 147.4 / 425.9 | 147.3 / 188.7 | 100% / **226%** |

The 2-2.5x C read leads are this harness's answer to its own question: its TU boundary is
GONE (#25 header-only), and at `-O2` the fully-inlined C chain beats the C++ templates —
the §0-documented level-dependence, now inverted in C's favor at this level. At `-O3`
(schema rows above) the same shapes sit at 93-104%. Both are true; the level is the story.
C++'s own `-O3` numbers for this harness are recorded beside it (546/759/570 M pkt/s reads)
in the raw file — same binary shapes, 2.9x its own `-O2` reads: this harness's C++ leg is
extremely level-sensitive and its `-O2` comparison should not be read as a language claim.

## What cannot be measured yet (honest gaps)

1. **No string row, no wstring row** in any family bench — today's #26 string-body fix is
   visible only through chat (85% bulk share) and testdata. Rows are queued (measure-first
   ruling: rows land before or with the next optimization).
2. **RealPacket** (§1.7 realistic snapshot shape) is mid-campaign, chunks 1-2 only — the
   1000-2000-bit individually-serialized headline row does not exist yet; today's headline
   still leans on the legacy corpus.
3. **serialize.c own-bench has no string/blob-shape row beyond the stream packet's 17-byte
   blob** — named by worker 1 at #26; joins the same rows-first queue.
4. **Quantized-read class** lacks an instruction-level receipt (see attribution note).

## Deviations from the campaign brief (the standard won, as it must)

- The brief ordered a **refusal** on any row outside its historical band; **§2.6.1 says
  band-break "publishes only with the mark"** — the standard's disposition was implemented
  (moot here: zero band-breaks).
- The brief listed "**testdata read — bulk [N]uint8 emitter path missing in C**" as
  candidate A; source inspection shows the C emitter's per-byte loop is **both directions**
  (write too), so A's write half also exists — testdata write's C number will move again
  when A lands.
