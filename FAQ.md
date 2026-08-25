# FAQ

Blunt questions, honest answers. Where something is a real limitation, it says
so, and says who should use something else.

## Isn't this just FlatBuffers?

No, and the difference is the one that matters most for gameplay traffic.

**FlatBuffers is zero-copy; schema is not.** A FlatBuffers buffer is accessed
in place through offsets and a vtable — you never parse it. schema *does*
parse: a read decodes bits into your struct. That is a real cost FlatBuffers
does not pay, and if random access into a large buffer without decoding is what
you need, FlatBuffers is the better tool.

What you get for that cost is **size**. FlatBuffers is byte-aligned and carries
vtables and offsets, because that is what makes in-place access work. schema is
**bit-packed with no framing at all**: a field declared `| min = 0, max = 1000`
occupies 10 bits, an enum with three variants occupies 2, and a branch that is
not taken occupies nothing. There is no vtable, no offset table, no field
identifier on the wire — both sides know the layout because the same compiler
generated both.

**And validation is not a separate artifact that can lag.** This is the
difference that matters most if the data is untrusted. In FlatBuffers,
verifying a buffer is a *distinct* piece of generated or hand-written code per
language — so verifier support varies by port, and a port without one cannot
safely accept a packet from the network at all. You are then choosing between
"trust the bytes" and "do not use this language".

In schema there is nothing to omit. Refusing an out-of-range value is not a
verification pass you run first, it is what the read does: the bound is part of
the type, so the generated reader checks it inline in every language, because
one compiler emitted all five. There is no such thing as a schema port that
reads but cannot validate.

For a 60 Hz gameplay packet where you decode the whole thing anyway, that trade
runs strongly one way. For a memory-mapped asset you want to touch three fields
of, it runs the other — and that case is deliberately not schema's job. schema
serves the realtime wire; generated storage is relocatable and memcpy-able
(see [WIRES.md](WIRES.md)), but the wire is one encoding with one purpose.

## Isn't this just Protobuf?

No — and in one specific respect schema is deliberately *less* capable.

**Protobuf's central design is field numbering and evolution.** Every field
carries a tag, so old readers skip fields they do not know and missing fields
fall back to defaults. That is why Protobuf is the right answer for service
APIs that version independently over years.

**The schema message wire has no field numbers, no tags and no evolution
machinery at all.** Versioning is a **protocol id** — a hash of the schema
itself, checked once at connect time. Two peers on the same id speak identical
bits; two peers on different ids should not talk. That is an intentionally
harsher contract, and it buys the thing Protobuf cannot give you: nothing on
the wire identifies a field, so nothing on the wire is spent identifying one.

If your client and server ship independently and must interoperate across
versions, **use Protobuf** — schema's message wire will fight you. If they ship
together, which is the normal case for a game client and its dedicated server,
the tags were pure overhead and the protocol id is the honest statement of what
was always true.

That narrowness is the design, stated plainly: **schema is deliberately not
an evolution system.** Hardcoded structs, one protocol id, same-or-refuse.
Data that genuinely outlives builds and must survive schema drift wants an
evolution-tolerant format, and Protobuf is a fine one — schema does not
compete for that job.

## Isn't this just Cap'n Proto?

No, and for much the same reason as FlatBuffers. Cap'n Proto's premise is that
the in-memory layout *is* the wire layout, so there is no encode/decode step.
schema encodes.

Two further differences worth being precise about. Cap'n Proto is byte- and
word-aligned by design — alignment is what makes the in-place trick sound —
where schema packs to the bit. And Cap'n Proto brings a large surface: RPC,
promise pipelining, capabilities, an ecosystem. schema brings a language and
five code generators, and nothing else.

On size: on the gameplay message in [COMPARISON.md](COMPARISON.md), Cap'n Proto
is 96 bytes unpacked and **52 packed**, against schema's 28. Packed is the
closest of the three general-purpose formats — its zero-suppression pass is
genuinely good, and on a mostly-zero message it would beat schema's fixed
bit-widths outright. It costs a compression pass that schema's writer does not
run, and it cannot know that your `health` field stops at 1000.

If you want the in-place model or the RPC system, use Cap'n Proto. schema is
the narrower tool.

## Do you have numbers against Protobuf, FlatBuffers or Cap'n Proto?

Yes — [COMPARISON.md](COMPARISON.md). The same gameplay message:

| schema | Cap'n Proto (packed) | Protobuf | FlatBuffers |
|---:|---:|---:|---:|
| **28** | 52 | 56 | 72 |

Every number is produced by running the real encoder — `protoc`, `capnp`,
`flatc` and schema's own writer — and the schemas, values and script are
committed in [`comparison/`](comparison/) so you can re-run it rather than
trust it.

It also says what those extra bytes buy, because they are not waste: Protobuf's
overhead is field-number evolution, FlatBuffers' and Cap'n Proto's is zero-copy
access. If you need either, that is a fair price and schema does not offer it.
The values encoded are deliberately large and non-zero — a mostly-zero message
would favour Cap'n Proto's packing far more than it favours schema.

## So what is actually novel here?

Honestly: not the idea of generating serializers from a schema. That is old.
Three things in combination are unusual:

1. **Bit-level bounds as part of the type.** `| min, max` is not validation
   bolted on — it determines the wire width. Most formats give you a `uint32`
   and store 32 bits.
2. **Fixed point as a first-class type.** `fixed(48, 16)` — and its unsigned
   sibling `ufixed(48, 16)` — is declared like any
   other field, and the compiler owns storage and wire. Floating point is not
   bit-identical across compilers and architectures, so lockstep simulation,
   rollback and deterministic replay cannot be built on floats. No mainstream
   format offers a fixed-point type; you store an int and remember the scale
   yourself, in every language, forever.
3. **Five languages proven identical mechanically.** Every CI run generates the
   corpus in C, C++, C#, Go and Rust and compares the emitted wire against pinned
   goldens. Cross-language agreement is checked, not asserted.

## Is bit-packing worth the CPU? Bandwidth is cheap.

Sometimes it is not, and you should know which case you are in.

Bit-packing costs shifts and masks and saves bytes. If your bottleneck is CPU
and you have bandwidth to spare, that is the wrong trade — and if you are
sending a few large messages rather than many small ones, the saving is small
anyway.

It pays when you are sending small, highly-constrained values at high frequency
to many peers, which is what gameplay state is: a health that cannot exceed
1000 is 10 bits, not 32, and at 60 Hz × N players that difference is the
bandwidth bill. It also pays where bandwidth is genuinely scarce — mobile,
console certification limits, egress pricing.

The honest framing: schema makes the trade *available and cheap to express*.
Declaring a bound is a few characters, and the compiler does the rest. Whether
your workload wants it is your call.

## Your own benchmarks say Go is 3× slower than C++. Why would I use this in Go?

Because the alternative in Go is not C++ — it is hand-written Go, or reflection.

The table in [PERFORMANCE.md](PERFORMANCE.md) is *relative to C++*, and C++ is
the fastest thing in the comparison. Generated Go is straight-line code over a
reused buffer with no reflection and no allocation, which is materially faster
than `encoding/gob`, `encoding/json`, or reflect-based Protobuf paths — and
crucially it is *identical on the wire* to the C++ your client runs.

If raw serialization throughput is your server's bottleneck, the honest advice
is that language choice matters more than serializer choice.

## What happens when I change a schema?

The protocol id changes, and peers on the old id will refuse the new one. That
is the design: the id is a hash of the schema, so a changed format is a changed
identity.

Practically this means **client and server deploy together** for wire
changes. If that is unacceptable for your deployment model, schema is the
wrong tool and you want an evolution system like Protobuf.

The id hashes a **wire shape projection**, not the source text, so an edit
that moves no bytes does not move the id: a comment, a blank line, a renamed
file, a renamed enum variant. Run `schema projection` to see exactly what it
depends on — it is deliberately printable, because a wire-affecting fact
missing from that text would be a fact the id ignores.

## Why AGPL? My lawyer will hate this.

The **compiler** is AGPL-3.0. **The code it generates is explicitly not**, and
that carve-out is intentional and permanent — running the compiler over schemas
you own does not make your generated serializers derivative works, and they are
yours under whatever terms you ship.

The plain reading: use it in a closed-source game freely; modify the compiler
itself and run it as a service, and the AGPL applies to those modifications.

**That carve-out is not a README paragraph — it is an ADDITIONAL PERMISSION at
the top of [LICENSE](LICENSE) itself**, which is where it has legal force. Point
your legal team at the file rather than at this answer. It is modelled on the
long-standing practice for compiler-like tools whose output is not covered by
the tool's own licence: the Bison parser exception and the GCC Runtime Library
Exception.

If you need something beyond that — a commercial licence, or a written
assurance for a procurement process — open an issue.

## Who maintains this? What if you stop?

It is a small team's library, used in a real game rather than written as a
demo. That is worth exactly as much as you think it is.

Two things that reduce the risk if it were abandoned: the output is **ordinary
source code in your repo** with no runtime dependency on the compiler, and the
generated C, C++, C#, Go and Rust reads like the code you would have written. If the
project stopped tomorrow, you would still have working serializers and could
maintain them by hand. That is a materially different exposure from depending
on a runtime library.

## Only five languages. What about Python, TypeScript, Java, Swift?

Not supported today. The five exist because they are what the authors ship in:
C++ engine, C# for Unity, Go for backend services, Rust for tooling.

A new backend is a Go package that walks the same IR the existing five consume,
and the cross-language test harness would tell you immediately whether it
agrees with the others bit for bit. That is the mechanism, but it is real work
and nobody should pretend otherwise.

## Is it safe to put on an internet-facing packet path?

That is what it is designed for, and the specific guarantee is: **a read
refuses out-of-range input rather than clamping or trusting it.** Ranged
values, array counts past their bound, string and bytes lengths past their
maximum, enum values that are not variants, and reads that run past the end of
the buffer all fail the read, and the same rules hold in all five languages
because one compiler emitted all five.

What it does *not* do: it is not a transport, so it does nothing about replay,
amplification, rate limiting or authentication. Those belong to the layer
below.

The compiler is fuzzed — a native Go fuzz harness drives parse → check →
generate across all five backends, and every crasher ever found is committed as
a permanent regression input. Generated readers are exercised against
hand-crafted hostile bytes in the cross-language test corpus.

## Do writes validate like reads do?

No. **The guarantee is on reads** — that is where untrusted input arrives, and
it holds in all five languages.

On the write side each language uses its own correctness idiom: C++ has
`assert`/`NDEBUG`, a check that disappears in release, so that is what it uses;
Go has no assert idiom, so it returns `ErrValueOutOfRange`, and Rust and C# do
likewise. A language should verify correctness the way that language verifies
correctness — which means the write side is not uniform across targets, and you
should not build on it. Keep values inside
their declared bounds when you write them — your code already knows they are,
and in a game shipping at 60 Hz re-checking every field on the write path is a
cost with no buyer. See
[USAGE.md](USAGE.md#writes-are-the-callers-responsibility).

## Is everything supported in all five languages?

**Yes.** The wire is generated for C, C++, C#, Go and Rust from one IR, and
checked against each other in CI on every push. Every target's output is held
to the same pinned goldens.

C was the last to arrive and reached parity in v1.6.0: fixed point, 128-bit
integers, objects and their quantize pair, and message dispatch. Rust's
`#[repr(C)]` storage is what makes relocatability actually true there rather
than incidental.

## What does the language NOT have?

Worth knowing before you adopt rather than after:

- **No maps.** Use a counted array of key/value pairs.
- **No recursive types.** `type Node { children [..4]Node }` is rejected as a
  composition cycle — generated storage is by value, with no pointers, which is
  what makes it relocatable and memcpy-able.
- **No optional / nullable**, except the honest version: a bool and a branch,
  or a `union` whose first variant is the absence.
- **No schema evolution.** No field numbers, no unknown-field skipping, no
  cross-version bridging — one protocol id, same-or-refuse, on purpose.
- **No zero-copy access.** Reads decode into your struct.

Some of these are scope (a game's packet does not need maps), some are
consequences of relocatable storage (recursion), and one is the design's
spine (evolution). The list is here so you can tell which of your
requirements are unmet before you find out the hard way.

## Do I need the serialize runtimes?

Yes — generated code targets a small runtime per language
([serialize](https://github.com/mas-bandwidth/serialize),
[serialize.c](https://github.com/mas-bandwidth/serialize.c),
[serialize.cs](https://github.com/mas-bandwidth/serialize.cs),
[serialize.go](https://github.com/mas-bandwidth/serialize.go),
[serialize.rs](https://github.com/mas-bandwidth/serialize.rs)). They are small,
open source, and doing the bit-level stream work the generated code calls into.

## How do I get out if I regret it?

Delete the compiler and keep the generated files. They are plain source in your
repo with no dependency on the compiler at build time or run time; from that
point they are ordinary hand-maintained serializers. The runtime dependency
stays, or you inline the parts you use.

---

Something not answered here? Open an issue — questions that recur belong in
this file.
