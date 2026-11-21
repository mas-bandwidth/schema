---
name: schema
description: Use when writing or changing .schema files, or generating serialization code for games with the schema compiler (packets, backend messages, save games, config, cooked assets) in C, C++, C#, Dart, Elixir, Go, Goose, Java, JavaScript, Lobster, Lua, Luau, Moo, Python, Ruby, Rust or Squirrel.
---

# schema

Schema is the data language for games. `.schema` files declare constants,
enums, flags, types, unions and tables; `schema generate` writes the
serialization code for each language from them.

Before writing or changing a `.schema` file, read the guide for AI assistants:
https://github.com/mas-bandwidth/schema/blob/main/docs/AI.md

It has the whole language on one page, the commands, how each generated
language is called, the rules that trip newcomers, and how to change a schema
without breaking stored data. The normative answers are in
[docs/SPEC.md](../../docs/SPEC.md) and
[docs/SPEC-TABLES.md](../../docs/SPEC-TABLES.md).

## Rules

- The `.schema` files are the source of truth. Never edit generated files;
  change the schema and generate again.
- A unit is one directory of `.schema` files sharing one `package` name.
- `type` is the packet wire: any edit moves the protocol id, so both ends ship
  together. Data that another build must read goes in a `table`.
- A `fixed table` is append-only. Retire a field with `| deprecated`.
- Rename a table field, variant, arm or table with `| was = "old_name"`.
- Append `flags` variants at the end only.
- Keep written values inside their declared bounds; every read refuses values
  outside them.

## The loop

Run after every change, replacing `<dir>` with the unit's directory:

```
schema fmt <dir>
schema check <dir>
schema generate --lang <lang> --out <out> <dir>
```

1. `schema fmt` writes the canonical style. It is the only command that writes
   a `.schema` file.
2. `schema check` prints nothing on success. On failure it prints one
   diagnostic per line, each saying what the input wants: do what it says and
   run it again.
3. `schema generate` once for each language the project uses. `--lang` takes
   `c`, `cpp`, `cs`, `dart`, `elixir`, `go`, `goose`, `java`, `js`, `lobster`,
   `lua`, `luau`, `moo`, `python`, `ruby`, `rust`, `squirrel` or `unity`.
4. Build the project and run its tests.

Before changing a `type`, run `schema id <dir>`; if the id moves, every peer
must be redeployed together. Before changing a table that stored data depends
on, read the "Changing a schema without breaking old data" section of the
guide.

`schema help ai` prints a short version of these rules to paste into a
project's `AGENTS.md` or `CLAUDE.md`.
