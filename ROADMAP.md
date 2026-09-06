# Schema roadmap

Schema lets a game team define its shared data once and generate the code that
reads and writes it in every language the game uses. Today that is true for the
packet wire, as released in 2.4.0, in nine languages. We are extending it into versioned messages,
save games and cooked assets, and into eleven more languages. This page is the
work, the state it is in, and what comes next.

## The idea

A game is written in more than one language. The client, the server, the
tools, the web team, the mobile builds and the scripting layer each have their
own, and every one of them has to agree on the same data: the packet, the
message, the asset, the save game.

Today each team stitches that together by hand. A bitpacker for the packets,
Protobuf for the backend, something else for assets and saves, and a pile of
schema files and generators that drift apart one language at a time. Every new
language the team adopts multiplies the problem.

Schema's answer is one small language. You write your constants, enums, flags,
types and tables once. The compiler generates the code that reads and writes
them, at the size and speed a game needs. The client and the server agree on
every bit because they were generated from the same file, and on every push
one test corpus runs through the generated code in every language to prove the
generators agree.

The bar is simple. Nobody should have a reason to reach for a different
serialization system if they are using schema for their game. Not for packets,
not for messages, not for assets, and not because their language is missing.
That is the destination. The rest of this page is how far along the road we
are.

## Where it stands

Schema has two wires, and they are at different stages.

**The packet wire is released.** It is for data both sides ship together: client
and server packets, replicated state, anything where every bit counts and
nobody needs to read old data with new code. It is bitpacked. A field with a
range of 0 to 1000 costs ten bits, not thirty-two. On the packet measured in
[COMPARISON.md](docs/COMPARISON.md), schema writes 28 bytes where Protobuf
writes 56 and FlatBuffers 72. It ships in nine languages: C, C++, C#, Dart,
Elixir, Go, Java, JavaScript and Rust. Every push runs one test corpus through
the generated code in all nine, and the
[conformance job](.github/workflows/ci.yml) fails the push if any language
writes a different byte for anything it implements. The README carries a
single-sweep speed number for all nine. The benchmark standard's pass, seven
interleaved rounds with control legs, has been run for five, in
[PERFORMANCE.md](docs/PERFORMANCE.md), and that pass is what measured means on
this page. This shipped as schema 2, now at
[2.4.0](https://github.com/mas-bandwidth/schema/releases). One packet-wire
feature added since, wstring(N), is in C++ only and goes to the other eight in
the sweep.

**The table wire is being built.** It is for data that has to outlive the
build that wrote it: messages between the game and a backend, assets a tool
builds and the runtime loads, save games, anything versioned. In plain terms:
a new build reads old data, an old tool opens a new file, the runtime opens a
cooked asset in place without unpacking it, and an editor inspects any table
without a schema file at hand. Tables can point at tables, so trees and graphs
are tables too. The table wire is built in C++ first, as the reference every
other language is measured against, and C++ has 26 of its 29 features today.
The other eight languages carry the first six, and C the seventh. Finishing
it in every language is the schema 3 gate.

## What we are building, in order

**Finishing the C++ reference.** Three features remain. Unknown fields kept
through a round trip, so an old tool can edit a new file without silently
dropping what it does not understand. Doc comments and tags carried into the
reflection descriptors, so editors can show them. Integers widened on read,
with a named reason whenever a read is refused. Beside them, the message form
is in its second round: messages sent as batches, bodies bitpacked. Sized from
that round's specification, a batch of the three backend messages we track is
244 bytes against 285 for Protobuf. Small single messages come out a few bytes
larger than Protobuf. The batch is what a backend sends.

**Fixing the reference once, not nine times.** Every table feature will be
built eight more times from the C++ reference and its specification, so the
reference is under an independent source review before the ports begin. The
first pass found five traversal defects
([#565](https://github.com/mas-bandwidth/schema/issues/565)), repaired in
[#578](https://github.com/mas-bandwidth/schema/pull/578). Two sets of findings
are still open, on the JSON walker
([#566](https://github.com/mas-bandwidth/schema/issues/566)) and the message
codec ([#571](https://github.com/mas-bandwidth/schema/issues/571)), and the
ports of those parts wait on them.

**The sweep.** Every missing feature in the eight shipped languages, mirrored
from the specification pages and the reference, each held to the reference
corpus bit for bit, each reviewed by someone who did not write it. This is
most of the remaining work in the nine.

**The performance floor.** Every feature in every language is measured on
the benchmark board, and the result is published. A port that is
correct but slow is not production ready. The method is in
[bench/BENCH-STANDARD.md](bench/BENCH-STANDARD.md), and it refuses to print a
ratio it cannot justify.

**Eleven more languages.** Swift, TypeScript, Lua, Clojure, Python, Ruby,
Kotlin, GDScript, Zig, Odin and Haxe, the languages a game team has around it.
Each begins with a proof rather than a plan: the packet wire in that language,
bit-identical to the corpus, with a measured speed, before any table work
starts. Tracked on
[issue #381](https://github.com/mas-bandwidth/schema/issues/381).

**The end state.** One schema file, every wire, every language a game uses,
with the numbers published so you never have to take our word for it.

## The matrix

The table wire, as one table. Rows are its features. Columns are languages.
A ✅ means the feature ships in that language, held to the C++ reference by
tests that run on every push. A ❌ means it does not exist yet. The packet
wire is not in the table. Its status is the release above. The rows use the
specification's names. The **fixed class** is a table with no pointers, a
struct on the table wire, versioned. The **variable class** is a table that
can hold pointers to other tables. The **block form** is a table laid out to
be used in place. A **cook** builds that binary, and **cook open** is the
runtime opening it. The **message form** is the table wire for messages. Its
✅ marks the first round, byte-framed bodies. The second round, batches with
bitpacked bodies, is the one sized above and is still being built. The
definitions are in [SPEC-TABLES.md](docs/SPEC-TABLES.md), and the live
table, with an issue behind every cell, is
[issue #366](https://github.com/mas-bandwidth/schema/issues/366).

| feature | cpp | c | rust | go | cs | java | js | dart | elixir | swift | ts | lua | clojure | python | ruby | kotlin | gdscript | zig | odin | haxe |
|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|---|
| fixed class, tolerant wire | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| text form, fixed class | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| reflection descriptors | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| block form, read side | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| cook open | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| build version | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| variable class (pointers, the flat node table) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| text form, variable class | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| block form, build side | ✅ | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| cook write in the runtime | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| wide scalars (128-bit) on the table wire | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| unions whose arms are tables | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| arrays of pointers | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| arrays of unions | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| optional arrays | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| blobs (*bytes, *string) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the wire fuzzer gate | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| maps | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| union arms of any field type | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| string, bytes and flags defaults, and renaming a table | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| renaming variants, arms and type fields | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the 64-bit id-table wire, enum and escape kinds | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| JSON in and out of one table instance | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| wstring(N) | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| unbounded arrays | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| the message form | ✅ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| retain-unknown | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| doc comments and tags in the descriptors | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |
| widening on read, and the refusal reasons | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ | ❌ |

75 of 580 cells are done. That is a count of features, not of effort, since the
cells are not equal in size. The table will be printed as it stands with every
release from here.

Each language is in one of three states, applied to each wire on its own.

- **Performant and production ready.** Every feature is done. The test corpus
  is bit-identical on every push. The speed is measured against C++ on the
  benchmark standard and published.
- **Done, but not yet mature.** Every feature is done and the tests are green.
  The speed measurement is still owed.
- **Coming.** Not every feature is done.

On the packet wire as released in 2.4.0, C, C++, Rust, C# and Go are
performant and production ready, and Java, JavaScript, Dart and Elixir are
done but not yet mature. On the table wire every language is coming, C++ at 26
of 29 and the rest as the table shows.

## How the work is done

Numbers you can check, and code you own. Nothing lands without
[tests that run on every push](.github/workflows/ci.yml). No speed is claimed
that the benchmark did not measure, and no size the specification does not
work out to the bit. The compiler is
AGPL-3.0. The code it generates is yours, under any terms, and the
[LICENSE](LICENSE) says so in writing.

Schema is built by Glenn Fiedler, who has written about how multiplayer games
work and given the code away for twenty years, together with an AI
collaborator that does much of the building, testing and porting. Glenn owns
every design decision. Every month a
[public ledger](https://github.com/mas-bandwidth/patreon#public-ledgers) shows
where the AI collaborator's tokens went, by repository, and what they bought.

## Fund this work

If you write games in more than one language, this is being built for you. If
you have ever kept two schema systems in step by hand, or shipped a client and
a server that disagreed about one field, this is the fix we are building.

Your support pays for the tokens the AI collaborator runs on and the machines
the benchmarks run on, and the ledger shows you where every one of them went.

**[Become a supporter](https://www.patreon.com/MasBandwidth/membership)**
