# Framing-driven region measurement

These 34 literal file-wire vectors pin region bytes before value decoding:
23 canonical values and 11 wrong-kind, count, framing, bounded-overflow, or unknown-ID mutations.
The canonical vectors cover maps, optional map values, keyed table arrays,
nested unions, bounded and unbounded union arrays, and reachable table/blob nodes.

The region includes the root, reachable record storage, and attribution directory;
it excludes the 64-byte cooked file header. Malformed file framing or infeasible
unbounded element counts return -1. Incomplete nested payloads retain the storage
reserved by complete outer framing. The arm payload kind does not change sizing
of a known arm; value readers diagnose that mismatch separately.

The values were checked independently by generated Go LoadMeasure and the
compiler-side FileRegionMeasure. Canonical wires additionally equal Encode and
their measurements equal Cook size minus 64. TestRegionClosureElixir applies
the same literal pins to actual native Elixir.
