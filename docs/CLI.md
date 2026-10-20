# The `schema` command line

The commands are listed by `schema` with no arguments. Each command's flags are
documented where the command is: [USAGE.md](USAGE.md).

`schema pin <version> <dir>...` rewrites the compiler pin in every `generate.go`
`go:generate` line under each directory to `<version>`, printing one line per
file changed. `--mixed-ok` may sit before or after the directories; it accepts a
tree whose pins already differ and moves them together. The command refuses by
name a version that is not a Go pseudo-version or semver tag, a directory that
does not exist or holds no pin line, and a mixed tree without `--mixed-ok`,
exiting 1 without changing any file. See USAGE.md's Go adoption notes for the
bump recipe.

`generate --lang go` emits Go that imports the shared table runtime
`github.com/mas-bandwidth/schema/v2/tables/goruntime` by default; the compiler
option `runtime=standalone` embeds that machinery in the unit instead
(`compiler.Options{"runtime": "standalone"}`; SPEC-TABLES.md §8.4). Any other
`runtime` value is refused by name, and the refusal names shared as the
default.

`schema testgen --lang lua` writes `<Base>Table_gen_test.lua` beside the Lua
table module `generate --lang lua` writes, in a build tagged `schema_leg_lua`
(`make build/schema-lua`); the generated tests run under the pinned Lua 5.4.8
interpreter (`make dist-lua`). The untagged build carries `--lang go` only and
refuses every other testgen target by name (docs/SPEC-TABLES.md §16.11).
