# examples/ — the corpus

The corpus is the language's proving ground (SPEC §7.3): realistic protocol
shapes, drawn from a real game's serialize usage with names and tuning values
genericized — these files carry the *structure* of a real protocol, not any
game's actual protocol.

**The standing check: every file here must compile under docs/SPEC.md as written.**
A corpus example the spec's own rules reject is a finding — in one document or
the other. `make check` runs it; the golden pins (SPEC §7.2) hold every
target's generated output and wire bytes to these files.

## The files — aspect-oriented layout

One aspect per file (a convention, not a rule — order-free cross-file
resolution makes it free):

| file | holds | exercises |
|---|---|---|
| `Constants.schema` | every `const` | composition (`const C = A * B`), `Team.Max` enum references, order-free cross-file refs |
| `Enums.schema` | the enum family | both forms, comma-separated variants: `enum` (with-None) and `flags` (uint64, bit-per-variant) |
| `Types.schema` | every `type` | tagged user types (`Vec3 \| vec3`, `Quat \| quat4` — tags inert in v1), quantized-int types, prefix arrays `[..N]T`, bool-gated `if` with a `flags` field, and `RigidBody` — the serialize README's own example, schema-specified: the velocity group rides only when `!at_rest`, and §5's zero-on-untaken-branch rule is the hand-written `else if ( Stream::IsReading )` zeroing made contract |
| `Render.schema` | the render blob | parallel scatter/gather: trivially-copyable generated types built independently by N workers and gathered by concatenation |
| `Wire.schema` | wire-construct coverage | the constructs the other files don't reach — typed constants, `union` (first-class one-of), `const`/`reserved`/`align` wire items, compressed floats, empty bodies, unified `string(N)`/`bytes(N)` (fixed buffers, used-length wire) — so every emitter codepath is exercised in the corpus |
| `Degenerate.schema` | the degenerate arrangements | the shapes realism masks (issue #203): a fixed scalar array as the whole message (`[2]float64`, `[2]uint64`, `[1]T`, `[N]T` on a chunk boundary, an array plus a trailing field, two arrays), a bare two-float unit with no other math user, and a nested struct as the only field, as the first field, and straddling a run cap. Every type is a whole number of bytes wide, and none reaches a target's float-bits helpers — both properties are load bearing; the file's header says why |
| `Clauses.schema` | element widths whose clauses disagree | the arrangements Degenerate's whole-byte property puts out of reach: widths where an emitter that groups array elements picks a DIFFERENT count on the write and the read side (13 bits is four against three), so both clause boundaries land mid-byte. Counts run 0, below a clause, exactly a clause, one past it, and the bound. Plus a fixed mid-byte array, grouping across a nested struct boundary, a union of empty arms behind a tag, and `string`/`bytes` at zero, partial and full length behind a 5-bit lead so the align inside them is a real barrier |
| `Joins.schema` | where a static offset is given up and regained | the same blind spot aimed at the static-offset state machine: branch arms that agree and disagree on width, a branch with no `else`, a branch inside a branch, an align that regains staticness on one path only, an array that gives it up on one path only, unions of unequal arms at mid-byte offsets, and a long static run after an align |
