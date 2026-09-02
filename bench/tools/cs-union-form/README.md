# cs-union-form — the C# table-union decision probe

A **one-off decision probe**, not a bench-standard leg. It emits no CSV row,
`bench/run.sh` does not know about it, `make test` does not run it, and it
measures no shape from `bench/corpus/`. It exists so the numbers behind one
recorded decision are reproducible instead of anecdotal.

## The question

C# has no native union. A fixed table with a union therefore has no
transliteration of the C++ tagged union, and the owner licensed two candidate
spellings (schema#248, #262). This probe measures them against each other on a
representative shape — the space per-frame render table: a 256-record frame,
each record carrying a union of three blittable arms, decoded every frame.

- **A** — all arms inline, max-of-arms via `[StructLayout(LayoutKind.Explicit)]`.
- **B** — a tag beside one reference per arm, allocated at construction and
  reused on every read. This is the C# packet emitter's existing union
  spelling, and it is what the table backend ships.
- **C** — one reference per arm, allocated **on read**: the literal reading of
  the owner's option (b).

## Running

    cd bench/tools/cs-union-form && dotnet run -c Release

It warms the tiers, then reports median-of-7 ns/frame and
`GC.GetAllocatedBytesForCurrentThread` per frame for each form, plus each
form's construction footprint.

## The sitting behind the decision

2026-09-02, .NET 10.0.400, Release, arm64 (Apple silicon), two sittings:

| form | ns/frame | alloc/frame | construction per frame |
|---|---|---|---|
| A — arms inline | 1056 | 0 B | 16 440 B |
| **B — pre-allocated arms (shipped)** | 1051 / 1068 | 0 B | 47 160 B |
| C — one reference per arm on read | 1561 / 1575 | 7 504 B | 47 160 B |

B/A was 0.996 and 1.011 — indistinguishable. C is 1.48–1.49x slower and
allocates 7.5 KB per frame, which is 60 Hz garbage in the Unity client.

A and B tie on the read path, so the decision rests on what does not show up
in the table: A's explicit layout is legal only while every arm stays
blittable, so an arm gaining a string, an array or a nested table forces the
form to change; and A cannot decode into the packet emitter's existing arm
classes, which a table's closure requires. **B ships.**

These are ONE MACHINE'S numbers from a probe, not a certification run under
`bench/BENCH-STANDARD.md` — nothing here may be divided against a bench CSV
row. What the standard's contract does hold for is the tables bench leg that
schema#262 names as a gate item — a fixed table beside its equivalent type on
the ledger. That leg is schema#270, and this probe does not close it: it
measures two storage forms against each other, not a table against its type.
