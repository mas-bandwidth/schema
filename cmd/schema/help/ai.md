# schema: notes for AI coding assistants

This project uses schema, the data language for games. Each `.schema` file
declares constants, enums, flags, types, unions and tables, and the schema
compiler generates the serialization code for them in each language the
project uses.

Rules:
- The `.schema` files are the source of truth. Change a data shape there.
- Never edit a generated file by hand. Change the `.schema` file and run
  generate again.
- A unit is one directory of `.schema` files with one `package` name.
- Write values inside their declared bounds: min/max, string, bytes and
  array capacities, enum and union ranges. Every read refuses a value
  outside them.
- A `type` rides the packet wire. Any edit to a type moves the protocol id,
  so client and server ship together. Data another build must read (saves,
  config, assets, backend messages) goes in a `table`.
- A `fixed table` is append-only: add fields at the bottom, retire a field
  with `| deprecated`, never reorder or remove one.
- Rename a table field, an enum variant, a union arm or a table with
  `| was = "old_name"`; a bare rename orphans the stored data.
- `flags` variants are bit positions: add new ones at the end only.

Commands (replace <dir> with the directory of .schema files):
  schema fmt <dir>        format in place; the only command that writes .schema files
  schema check <dir>      parse and check the unit
  schema generate --lang <lang> --out <out> <dir>
  schema id <dir>         print the protocol id
  schema help             list every command

--lang takes: c, cpp, cs, dart, elixir, go, goose, java, js, lobster, lua,
luau, python, ruby, rust, squirrel, unity

After every change to a .schema file:
  1. schema fmt <dir>
  2. schema check <dir>   fix every diagnostic; each one says what it wants
  3. schema generate      once for each language the project uses
  4. build and run the project's tests

The full guide for AI assistants:
https://github.com/mas-bandwidth/schema/blob/main/docs/AI.md
