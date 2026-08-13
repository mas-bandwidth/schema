# Versioning

```bash
schema version
```

The compiler follows [semantic versioning](https://semver.org/), but the thing
being versioned needs saying precisely, because for a code generator the
interesting compatibility question is not the CLI.

## The question that matters

**Does upgrading the compiler change the bits my existing schemas produce?**

For a patch or minor release: **no**, and that is the promise. Take the same
schema files, run a newer 1.x compiler, and the generated code encodes the same
values to the same bits. Your deployed clients keep talking to your deployed
servers.

For a major release: **possibly**, and it will be in the release notes with a
migration note, because we know what we are asking of you.

This promise is what the pinned goldens in CI exist to enforce. A change that
moves the wire moves a golden, and moving a golden is a deliberate act that has
to be argued for in review — it cannot happen by accident.

## What each number means

**Major** — the wire may change, or a language feature may be removed. Expect
to redeploy both sides of every connection, and expect the protocol id to move.
We will not do this casually.

**Minor** — new language features, new attributes, new backends, better
diagnostics, generated code that is faster or cleaner. **The wire for schemas
that already compiled does not change.** New syntax you have not used cannot
affect you.

**Patch** — bug fixes and documentation. If a fix corrects generated code that
was *wrong*, the wire for the affected construct changes by necessity; that is
a bug fix, it will be called out prominently in the release notes, and it is
the one case where a patch release can move bits. The alternative — leaving
known-wrong output in place to protect a version-number promise — would be
worse.

## What is not covered

**The protocol id is not a version number.** It is a hash of your schema, and
it changes when *your* schema changes, independently of the compiler's version.
Two peers built from the same schema by different 1.x compilers have the same
protocol id and interoperate. That is the intended behaviour, and it follows
from the promise above.

**Generated files do not record the compiler version.** This is deliberate.
Stamping it would mean every release produces a diff in every generated file in
every downstream repository — churn that says nothing about whether the wire
actually changed. Generated code carries the protocol id instead, which is the
thing that governs compatibility. If you want to know which compiler produced a
tree, that belongs in your build system, not in every file.

**`internal/` packages have no compatibility promise.** The compiler is
consumed as a binary. There is no supported Go API for embedding it, and the IR
is free to change in any release.

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
| [serialize.rs](https://github.com/mas-bandwidth/serialize.rs) | Rust |

They share a wire standard (`STANDARD.md`, kept identical across all five) and
are checked against each other. A given compiler release states the minimum
runtime version it needs; newer runtimes keep working with older generated
code.

## Release process

Releases are tagged `vMAJOR.MINOR.PATCH` on `main`. CI must be green on the
tagged commit — the cross-language corpus and the seeded fuzz corpus both.

The build stamps `git describe` into the binary, so `schema version` on a
release build reports the exact tag, and on a development build reports the tag
plus commits-since plus hash. Please quote that line in bug reports.
