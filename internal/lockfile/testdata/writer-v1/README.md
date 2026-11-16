# Historical writer provenance

The `wide` and `attribution` fixtures were emitted by the pre-v2 metadata
compiler at `948ec717c6d047f7e45f6309d47f2bc312a6b1ca` on Linux. Their saved
rendering8 locks are historical bytes, not regeneration targets for v2.

The separately migrated owned corpus locks retain their original keys and
received historical Writer snapshots through the strict import API from an
archive of main `68a273746bfd60abec07397761d8af0184fcd1f0`. This is explicit
provenance for these test schemas, not authentication of old bounds from a
legacy hash and not a policy for user data. All94 admitted roots matched the
recorded v1 digest, layout, body and raw key; no layout-only trust option was
used. `tables/block/RenderFrame` and `tables/examples/WideBlob` were not admitted
fixed-form roots under that archived declaration and keep unknown snapshots.
All original wire facts, order and retirement were preserved.

The genuine pre-lineage sibling fixture `../writer-v6` was emitted by compiler
`04392d220d13f56dff76121778c6bdbe0d084e8c`. Its identity pin was obtained by
that compiler's own layout-only hash helper. It exercises recovery before a
prospective identity is appended; constrained source enrichment requires the
separate explicit `--legacy-layout-only` trust decision.

Two historical writers that shared a legacy key and file bytes remain
indistinguishable even when both source declarations are known. These fixtures
do not claim otherwise.

The three `wide/*.bin` files were captured with the actual pre-v2 compiler's
`tablewire.EncodeFixed` at948ec717c6, not produced by switching the new compiler
back to legacy mode. All82 bytes carry key0xcd3d85d3dddbe9d3. `good.bin` is the
small writer's exact2^80 upper bound. `forged-small.bin` changes its body to
2^80+1. `lawful-colliding-large.bin` is a genuine old writer with upper bound
2^81 legally emitting2^80+1; it is byte-identical to the forged smaller-writer
file. A reader cannot accept that lawful larger-writer file while refusing the
identical smaller-writer forgery from the bytes alone.
