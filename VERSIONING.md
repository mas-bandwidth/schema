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

**Major** — the wire may change, a language feature may be removed, **or the
protocol id moves for schemas that did not change**. Expect to redeploy both
sides of every connection. We will not do this casually.

That last case is worth stating on its own, because it is the one that looks
harmless and is not: if the id moves, deployed peers refuse newly built ones
even when every byte they would exchange is identical. The operational cost is
the same as a wire change, so it earns the same signal. v2.0.0 was exactly
this — the id changed, the wire did not.

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

## Recorded wire-affecting amendments

The cases above are policy; this section records the concrete instances, so
the history of "a release moved bits" lives where the compatibility promise
does.

**2026-08-15 — fixed-point rounding unified: half away from zero.** SPEC §4.8
rule 2b's generated shallow narrowing changed from the bare arithmetic shift
(ties toward +infinity) to the one fixed-point rounding rule, ties away from
zero — the rule the data compiler and rule 4 already used. This moves the
bytes generated code produces **only on exact ties of negative raw values in
shallow narrowing**, and the protocol id does NOT move — a rounding rule is
not wire shape, so the id cannot see this class of change. It rides the next
release loudly, with this note. The negative-tie conformance vector pinned in
the ludicrous corpus is the tripwire that keeps five ports on the unified
rule.

The standing risk calculus for this and every future wire-affecting
amendment, Glenn's words (2026-08-15): *"I will always deploy client and
server together on any breakage. so this is no concern."* Both sides of every
connection redeploy together; an amendment that moves bytes is priced by that
doctrine, not by the fiction that deployed halves must interoperate across
the change.

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
