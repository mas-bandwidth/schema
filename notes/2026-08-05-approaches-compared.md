# schema and Atlas — the two approaches, side by side

*Rowan, 2026-08-05. A plain-language comparison, written after reading Atlas's evaluation of
our spec. Neither approach is wrong; they start from opposite ends and are each strongest
where the other is early.*

- **Starting point.** schema starts at the network wire — realtime game packets — and may
  move toward general-purpose data later. Atlas started at tools, assets, and editors, and
  is moving toward networking over time.

- **Compile time vs runtime.** schema is a compiler: declarations become generated source
  code, and nothing interprets a schema while the game runs. Atlas keeps schemas alive in
  memory at runtime, where they drive parsing, editing, and inspection directly — generated
  code is faster on a hot path, a live schema powers editors.

- **How many languages.** schema targets C++, C#, Go, and Rust, with byte-identical wire
  output across all four as the core contract, locked in by a conformance suite as the
  compiler is built. Atlas has one language and one reader, so cross-language agreement is
  a problem it simply doesn't have.

- **The wire itself.** schema bitpacks — every field costs exactly the bits it declares,
  floats are rounded to a declared precision, and every read is validated. Atlas's codec
  uses plain fixed-width bytes: simple and easy to compare, not size-optimized, with
  bitpacking available later if the bytes ever matter.

- **The protocol id.** schema hashes the schema files themselves: any edit moves the id, it
  fails safe, and a compiler upgrade can't move it. Atlas's preferred design hashes its
  live, in-memory description of the types (today the id is still bumped by hand):
  whitespace edits wouldn't move it, but the id depends on the code that computes it
  staying stable.

- **Versioning.** schema has none on the wire, on purpose — two sides at the same protocol
  id can talk, otherwise they can't, and everything downstream gets simpler. Atlas's asset
  side is built for data that evolves and composes over time, which is what asset data
  actually needs.

- **Editors.** Atlas drives a generic inspector and editors straight from its schemas — its
  standout capability, shipped and proven across its asset pipeline. schema has no editor
  story in v1 and doesn't want one yet; the network focus comes first.

- **Where they're converging.** Both land on the same idea: declare a type once, generate
  the boilerplate. Atlas is adding code generation (what schema started with); schema may
  later add data-layer maturity (what Atlas started with) — and Atlas's hand-written
  networking pain list matches schema's object-layer feature list item for item, decent
  evidence the shared destination is right.

- **Where they'll actually meet.** The likely integration point is the render objects
  level — e.g. RenderShip. schema stays the source of truth for the shared types, messages,
  and config/assets; Atlas represents the client-side world (client-only state, UI), and a
  shim or compatibility layer lets Atlas import and work with schema's types and refer to
  its config/assets. Shape and ownership of that layer: TBD.
