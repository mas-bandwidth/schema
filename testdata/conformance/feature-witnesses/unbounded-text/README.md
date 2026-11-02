# Unbounded text oracle regressions

The list and map fixtures preserve the smallest observed loss of a nonempty
unbounded value in the neutral JSON and wire readers. Before the repair, the
readers interpreted the internal zero size as a declared zero-byte bound:
`["live"]` became `[""]`, and the map entry with key `"live"` disappeared.

`Root.bin` and `Root.json` are pinned expected values, not output from that
lossy reader. The equivalent bounded spelling `string(8)` independently
reproduces the same bytes, as required by SPEC-TABLES section 2.13.1.
`TestUnboundedTextOracleFixtures` checks both inputs, exact output bytes and
clean reports. Additional permanent tests cover the generated-Go golden,
long Unicode keys with a common prefix, duplicate-key identity, optional
values, element APIs, bounded lengths/counts, invalid UTF-8 and embedded NUL.

The full shared harness runs once in original-nine nightly readiness. These
fixtures are neutral oracle regressions; they do not claim that every port
already implements unbounded text or replace per-language feature witnesses.
