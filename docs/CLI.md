# The `schema` command line

The commands are listed by `schema` with no arguments. Each command's flags are
documented where the command is: [USAGE.md](USAGE.md).

**No command damages a file it rewrites.** Every command that changes a file
(`fmt`, `tables-baseline --update`, `lock`, `generate`, `testgen`, `pack`,
`unpack`, `cook`, `uncook`, `pin`) writes the new content to a temporary file
beside it and renames that into place, so a write that fails (a full disk, a
file-size limit, a read-only directory) exits 1 with the file byte for byte as
it was. A command that changes several entries (`fmt` over several sources or a
directory, `generate` into a directory, `unpack` into a tree, `pin` across
packages, `pack --message --announce` and `cook --attribution` with their two
files) lands all of them or none: every file is staged before any is
replaced, and a failure puts back the entries already moved. `fmt` formats
every source before it writes any, so a source that does not parse leaves
every one as it was. The files pack, cook and uncook write go
into a directory that is already there: a missing one is refused, not made. A
path that is a symbolic link is followed and the link stays; an existing file
keeps its mode. A file made read-only is not replaced: the
command is refused with nothing changed. `new-leg` only creates files and
never replaces one. One run writes to an output at a time: schema takes no
lock.

`schema pin <version> <dir>...` rewrites the compiler pin in every `generate.go`
`go:generate` line under each directory to `<version>`, printing one line per
file changed; the line's module path moves to the one the version's major
requires (`schema/vN` from v2 on). `--mixed-ok` may sit before or after the
directories; it accepts a tree whose pins already differ and moves them
together. A pseudo-version is a commit: pin checks its UTC date, its base form
and its 12 lower-case hex characters offline; Go verifies the commit, its
timestamp and the base when it resolves the version.
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
