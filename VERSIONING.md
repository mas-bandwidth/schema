# Versioning

```bash
schema version
```

The compiler follows [semantic versioning](https://semver.org/), but the thing
being versioned needs saying precisely, because for a code generator two
different things can "change": the compiler the user runs, and the wire their
schemas produce. Each has its own version, and they are not the same number.

## Two versions, two jobs

**The compiler's semver answers: does upgrading break the user?** Major, minor
and patch are defined below entirely in terms of the user's world — their
`.schema` files, the API of the code generated from them, and the release
notes they have to read.

**The protocol id is the wire's own version, decoupled from semver entirely.**
It is a hash of the schema's wire shape: it changes when the wire changes and
only then, independently of what the compiler's version number does. Two peers
whose builds carry the same protocol id interoperate; two whose ids differ
refuse each other instead of misreading each other. No compiler release number
participates in that decision.

## What each number means

**Major** — the user's world breaks: existing `.schema` files stop compiling
or change meaning, or the generated API breaks (code written against the
generated types and functions stops building or changes behavior). Expect a
migration note. Nothing less than this earns a major.

**Minor** — additive features: new language features, new attributes, new
backends, better diagnostics, generated code that is faster or cleaner. New
syntax you have not used cannot affect you. A minor release may also carry a
wire change, WITH its protocol id bump: the bytes and the id move together, so
deployed peers refuse newly built ones rather than misread them — and the
release notes state the protocol id bump first and loudly, before anything
else in the entry.

**Patch** — bug fixes and documentation, and one promise, kept verbatim:
**"no PATCH release will break protocol id."** Take any schema, rebuild it
with a newer patch release of the same minor, and its protocol id is the same
id. Patch releases are always safe to take.

This is what the pinned goldens in CI exist to enforce. A change that moves
the wire moves a golden, and moving a golden is a deliberate act that has to
be argued for in review — it cannot happen by accident, and the release that
carries it wears the number these rules assign.

## History: the v2 line

An early v2.0.0 and v2.1.0 were re-versioned into the 1.x line as v1.6.0 and
their tags retired; the 2.x line that exists today starts at the v2.0.0 that
follows v1.16.0, and — per Go's major-version rule — carries the module path
`github.com/mas-bandwidth/schema/v2`.

## Recorded wire-affecting amendments

The rules above are policy; this section records the concrete instances, so
the history of "a release moved bits" lives where the compatibility rules do.

**2026-08-15 — fixed-point rounding unified: half away from zero.** The
generated fixed-point narrowing of that era changed from the bare arithmetic
shift (ties toward +infinity) to the one fixed-point rounding rule, ties
away from zero. This moved the bytes generated code produced **only on
exact ties of negative raw values in that narrowing**, and the protocol id
did NOT move — a rounding rule is not wire shape, so the id cannot see this
class of change. It rode the next release loudly, with this note.

The standing risk calculus for this and every future wire-affecting
amendment: both sides of every connection redeploy together, so an amendment
that moves bytes is priced by that doctrine, not by the fiction that deployed
halves must interoperate across the change.

## What is not covered

**The protocol id is not a version number.** It is a hash of your schema's
wire shape, and it changes when *your* schema's wire changes, independently of
the compiler's version. Two peers built from the same schema by compilers that
generate the same wire have the same protocol id and interoperate. That is the
decoupling stated above, doing its job.

**Generated files do not record the compiler version.** This is deliberate.
Stamping it would mean every release produces a diff in every generated file in
every downstream repository — churn that says nothing about whether the wire
actually changed. Generated code carries the protocol id instead, which is the
thing that governs compatibility. If you want to know which compiler produced a
tree, that belongs in your build system, not in every file.

**The Go API under `compiler/` and `ir/` IS covered; `internal/` is not.** The
compiler is also a library — `github.com/mas-bandwidth/schema/v2/compiler` loads
and checks units and generates through registered generators;
`github.com/mas-bandwidth/schema/v2/ir` is the checked unit those generators read.
From the first release that carries them, their exported surface follows the
rules above: breaking it is a major, adding to it is a minor. Everything under
`internal/` — the scanner, parser, AST, checker and the six per-language
emitters — carries no promise and may change in any
release. Building on the compiler means `compiler.Generator` and `ir`, which is
the same door the built-in backends come through.

The tables baseline (SPEC-TABLES.md §18) adds to that covered surface:
`compiler.TablesBaselineText` and `compiler.UpdateTablesBaseline`, the
`TablesBaseline` and `OnWarn` policy fields on `compiler.Compiler`, and in `ir`
the table-wire kind vocabulary — the `TableKind*` constants and
`TableScalarKind` / `TableFieldKind` / `TableElemKind`, which are wire law and
therefore frozen (SPEC-TABLES.md §3). The baseline FILE's own rendering carries
its own version on its first line, independent of these: a bump there makes
every committed baseline stale at once and is repaired with
`schema tables-baseline --update --reason "..."`, which preserves each file's
history section.

**The SPEC is versioned with the compiler.** [SPEC.md](SPEC.md) is normative;
where the compiler and the SPEC disagree, one of them is a bug.

## The serialize runtimes version separately

Generated code targets a small runtime per language, and those are their own
projects with their own version numbers:

| runtime | |
|---|---|
| [serialize](https://github.com/mas-bandwidth/serialize) | C++ |
| [serialize.c](https://github.com/mas-bandwidth/serialize.c) | C |
| [serialize.cs](https://github.com/mas-bandwidth/serialize.cs) | C# |
| [serialize.go](https://github.com/mas-bandwidth/serialize.go) | Go |
| [serialize.js](https://github.com/mas-bandwidth/serialize.js) | JavaScript |
| [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) | Rust |

They share a wire standard (`STANDARD.md`, kept identical across all six) and
are checked against each other. A given compiler release states the minimum
runtime version it needs; newer runtimes keep working with older generated
code.

## Release process

Releases are tagged `vMAJOR.MINOR.PATCH` on `main`. CI must be green on the
tagged commit — the cross-language corpus and the seeded fuzz corpus both.

The build stamps `git describe` into the binary, so `schema version` on a
release build reports the exact tag, and on a development build reports the tag
plus commits-since plus hash. Please quote that line in bug reports.
