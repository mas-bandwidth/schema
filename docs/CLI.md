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

`schema testgen --lang go|lua|squirrel` writes the generated round-trip and
damage tests of every root table beside what `generate` wrote: one
`<Base>Table_gen_test.go` for Go, one `<Base>Table_gen_test.lua` for Lua (run
under the pinned Lua 5.4.8 interpreter, `make dist-lua`) and one
`<Base>Table_gen_test.nut` for Squirrel per schema file (SPEC-TABLES.md
§16.11). A table whose `forms` leave out `wire` or `json` is refused by name;
every other `--lang` is refused by name.
