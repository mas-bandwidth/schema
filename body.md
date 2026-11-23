Adds the `write-refusal-packet` surface (docs/SPEC.md §5.1) to the assert legs: C, C++, C#, C# (unity), Rust, Java and Dart. The Go leg already answered it.

- Shared schema and cases in `test/packet-write-refusal/` (the same rows as `TestWriteRefusalPacket`), one driver per leg under that folder.
- Each driver runs in its debug build, where the assert firing is the refusal, and requires the lawful twin at each boundary to be written. It fails on any case it neither runs nor names not representable (Rust: the union tag).
- Each leg has `packet-write-refusal-<lang>` and a `-negative-control` that removes one emitted check and requires a named case to go red; registered like the packet-write-text gates.

Real bugs fixed: the C# and C# (unity) packet writers did not check a `bits(N)` value wider than N (row 2); the unity writer also did not check a `string(N)` for a zero byte or ill-formed UTF-8 (row 8). Generated trees and goldens regenerated; `make generated-current` is clean. SPEC §5.1 and the conformance README updated.

🤖 Generated with [Claude Code](https://claude.com/claude-code)
