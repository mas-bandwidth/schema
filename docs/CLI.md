# The `schema` command line

The commands are listed by `schema` with no arguments. Each command's flags are
documented where the command is: [USAGE.md](USAGE.md).

`schema pin <version> <dir>...` rewrites the compiler pin in every `generate.go`
`go:generate` line under each directory to `<version>`, printing one line per
file changed; the line's module path moves to the one the version's major
requires (`schema/vN` from v2 on). `--mixed-ok` may sit before or after the
directories; it accepts a tree whose pins already differ and moves them
together. A pseudo-version is a commit, and is accepted when it is one Go resolves (a real UTC timestamp, a legal base form, a twelve-digit lower-case revision).
The command refuses by name a version that is not a Go pseudo-version
or a semver tag; every v3 tag cut before the module path moved to `/v3`, under
any spelling (the boundary is `internal/modulemove.FirstV3ModuleTag`; while it
is unset, every v3 tag), pointing at the prebuilt binaries and at
`go install` of main instead; any version with build metadata such as `+incompatible` (which Go
cannot resolve); a directory that
does not exist or holds no pin line, and a mixed tree without `--mixed-ok`,
exiting 1 without changing any file. A write that fails part way also exits 1
with every file as it was: the tree ends all old or all new. A `generate.go`
that is a symbolic link is followed: the file it names is rewritten and the
link is left as it is. See USAGE.md's Go
adoption notes for the bump recipe.

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
