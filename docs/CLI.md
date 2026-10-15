# The `schema` command line

The commands are listed by `schema` with no arguments. Each command's flags are
documented where the command is: [USAGE.md](USAGE.md).

`schema generate --lang go --surface <list>` narrows the Go target's table output
to the named surfaces (wire, fixed, json, cook, block, view, message, retain);
the default is all of them. The flag, its dependencies and its refusal on every
other `--lang` are defined in USAGE.md's generate section and SPEC-TABLES.md §8.4.
