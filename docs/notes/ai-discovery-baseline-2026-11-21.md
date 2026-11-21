# Can an AI assistant find this project? Method and baseline (2026-11-21)

This note fixes the test so it can be run again the same way. The re-run is
the roadmap task `reach/measure-again/rerun` in [docs/roadmap.sexp](../roadmap.sexp):
it waits on the rename and on the listings in the same table, and its count is
recorded below the baseline here.

## Method

Six AI assistants, each with web search, were each given one of the six
questions below, cold. Each was told:

- use only web search and its own training;
- report the queries it ran, the pages it read, the candidates it considered
  and the one it picked;
- report anything already in its context that named a candidate.

Question E names the project; the other five are open questions a game
developer might ask without knowing the project exists. The score is how many
of the five open questions surface the project at all, and whether E finds it
and how many queries that takes.

**A flaw in the baseline run, to fix in the re-run.** The six assistants shared
a session context that mentioned the project. Two of them said they ignored
it. A clean re-run uses assistants with no project context at all.

## The six questions, verbatim

A. "I'm writing a multiplayer game in Unity (C#) with a dedicated server in C++. I need to send compact bit-packed packets between them and I don't want to hand-write the serialization in two languages. What should I use?"

B. "What's a good alternative to Protocol Buffers or FlatBuffers that's designed for games? I need small packets and code generation for C++, C#, Go and Rust."

C. "My game has a Rust server, a JavaScript web client and Luau scripts. Is there a schema language that generates serialization code for all three from one definition?"

D. "I need a save-game file format that still loads in future builds of my game after fields are added, and a way to cook assets into a compact binary for runtime. The engine is C++ and the tools are Go. What should I use to define these formats?"

E. (by name) "Someone mentioned a tool called 'Schema', described as 'the data language for games'. I don't know who makes it. Can you find it and tell me what it is, where it lives, how to install it, and whether it supports C#?"

F. "I've been reading the Gaffer On Games articles on reading and writing packets and serialization strategies. Is there a modern tool that does that kind of bit-packing for me from a definition file, in more than one language, so I don't write it by hand?"

When the product is renamed, question E is asked with the new name and the
same tagline, and the change is recorded with the result.

## Baseline, 2026-11-21

Found in **0 of 5** open questions. By name (E), found only on a query that
combined the tagline with "github", on the assistant's fourth query. Each
assistant ran 4 to 5 searches.

| Question | Pick | This project found? |
|---|---|---|
| A | write a generator by hand | no (the hand-written `serialize` libraries of this family appeared) |
| B | shortlist: Beschi, Fast Binary Encoding, Bebop | no |
| C | Protocol Buffers | no |
| D | FlatBuffers | no |
| E | the repository, on the fourth query | yes, by name, with the tagline and "github" |
| F | ArPack | no (the hand-written `serialize` libraries of this family appeared) |

## Re-runs

None yet.
