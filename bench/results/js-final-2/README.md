# era js-final-2 — the flat codec's closing measurement (UNCERTIFIED quick tier)

2026-08-18, MacBook Air (M2, arm64), 3 interleaved rounds × (cpp, c, js flat,
js runtime), golden-gated every leg-round. schema at the js-fastest-correct branch
head (merged as #74); serialize dd24915+, serialize.c dcbf47a+, serialize.js v1.1.0.

Headline (best-of-3): flat codec blended ~17x off C++ across 22 generated rows
(runtime codec: ~69x); real_packet 1.45 W / 6.05 R M msg/s — read 4.4x off C++.
Write legs are harness-bound (the bench's JS data-generation dominates; probe's
varyonly control in the wf_b9ef0d78-e47 journal). The emitter matched or beat the
hand-written probe on every row. Ruling enacted: "Whichever correct implementation
is fastest is the one we use for JavaScript" — the flat codec ships (#74).
