# What schema is for

**"We aim to build the best cross-language data type system for games."**

A game is written in several languages: the engine in C++, the client in
C#, the tools and the backend in Go, the website in something else. They all
pass the same data around. schema is one place to declare that data, and a
compiler that generates the code every language needs to read and write it.

Here is what people use it for.

## Packets between a client and a server

The client and the server ship together. Every packet has to be as small as
possible, and both sides have to agree on every bit.

Declare the packet as a `type`. schema generates a bitpacked reader and
writer in nine languages, and a protocol id from the declaration. Two builds
with different ids refuse to talk to each other, so a mismatch never turns
into a misread.

See [SPEC.md](SPEC.md).

## Data between tools, a website, and a backend

Tools, the website, and the backend each ship on their own schedule. A file
written by last month's tool has to load in this month's backend.

Declare the data as a `table`. The table wire carries field ids and lengths,
so a reader skips fields it does not know, fills in defaults for fields that
are missing, and reports what it saw. Nothing is fatal in either direction.

See [SPEC-TABLES.md](SPEC-TABLES.md) §3.

## Save games

A save file written by a build nobody has any more has to load in a build
its writer never saw. Every edit to the schema in between must be survivable.

A save game is a `table`, so it gets the same tolerance. On top of that, a
committed baseline file refuses at compile time the few edits the wire
cannot detect, and changing it on purpose requires a reason that is recorded
in the file.

See [SPEC-TABLES.md](SPEC-TABLES.md) §18.

## Assets cooked for a build

Tools build the assets. The game should not parse them at load; it should
map the file and point at it.

`schema cook` writes a table's data in the exact memory layout of one build,
in that build's byte order. The game checks a header and points. Open cost
does not grow with the file, so a gigabyte of assets opens as fast as a
kilobyte. A cook only opens in the build it was cooked for; every other
build refuses it.

See [SPEC-TABLES.md](SPEC-TABLES.md) §7.

## Render data from C++ to C#

Every frame, C++ writes a large block of render data and C# reads it, at
sixty frames a second or more. Neither side can afford a copy or a parse.

Every fixed `table` has a block form: its arrays laid out at a fixed pitch,
with the offsets at the front. C++ fills it from several threads. C# opens it
and reads the rows as spans over the same memory. Both sides are generated
from the one declaration, and the layout is asserted at compile time in both
languages, so they cannot drift apart.

See [SPEC-TABLES.md](SPEC-TABLES.md) §19.

## JSON packed into binary tables

Data is authored or exported as JSON. It needs to become a compact binary
that several languages can read.

`schema pack` takes a directory of JSON files shaped like the table and
writes the table wire. `schema unpack` writes it back out. Any language with
a table reader loads the result.

See [SPEC-TABLES.md](SPEC-TABLES.md) §16 and §17.

## Editors and debug views

An editor wants to show any table as a property tree. A debug view wants to
inspect what the game is holding this frame.

Every table comes with reflection descriptors: field names, types, offsets,
enum and union vocabularies. A tool walks any table it has never seen, and
a debug view walks the live instance in memory.

See [SPEC-TABLES.md](SPEC-TABLES.md) §8.

## Config from a backend into a running server

A server takes its config from a backend and reloads it without a
redeploy. The backend and the server are different builds.

The config is a `table`, delivered as bytes. The server reads it with the
same tolerance a save game gets, and the read report says exactly which
fields it could not name.

See [SPEC-TABLES.md](SPEC-TABLES.md) §3 and §4.

## The shapes, side by side

| you want | declare | you get |
|---|---|---|
| the smallest packet, same build both ends | `type` | bitpacked wire, protocol id |
| data that survives schema changes | `table` | tolerant wire, read report |
| a file the game points at | `table`, then `schema cook` | memory-mapped, one build |
| rows shared between C++ and C# every frame | fixed `table`, block form | pointed-at rows, no copy |
