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
