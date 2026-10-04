# The C fixed-table reference build on darwin/arm64, measured inside a card

What was measured: whether the C fixed-table reference **builds** on the **air** bench
(Apple M2 MacBook Air, darwin/arm64, 8 cores, Darwin 25.6.0) inside a sandboxed card, at
head `ed0c7917` of `main`. Three steps, in order: the generator (`make bin/schema`), the
generated C fixed-table sources (`make build/tables-generated-c/.stamp`), and the link of
one C fixed-table reference binary (`make build/schema_test_c_variable`).

This is a **build result only**. No benchmark was run, no test binary was executed, and
there is no timing in this file. It carries no claim about correctness, parity or speed
of the C leg — only that the toolchain on this machine produces the artefacts. The
question is worth asking on its own because every card bench in the fleet was linux/x64
until today, and the C leg's `-Werror` surface has never been put in front of Apple
clang inside the wall before.

Compiler: **Apple clang version 21.0.0 (clang-2100.1.1.101)**, target
`arm64-apple-darwin25.6.0`. Make: **GNU Make 3.81** (the macOS system make). Go:
**go1.27.1 darwin/arm64**, reached by granting the sandbox
`--read /opt/homebrew/Cellar/go/1.27.1`; the bare wall refuses it (see the air's bench
leg inventory for that measurement). `uname -sm`: `Darwin arm64`.

| step | command | rc | verdict | artefact / note |
| --- | --- | --- | --- | --- |
| generator | `make bin/schema` | 0 | built | `bin/schema`, 15,373,794 bytes, stamped `v2.5.0-335-ged0c7917` |
| generated C sources | `make build/tables-generated-c/.stamp` | 0 | built | 426 `.c`/`.h` files under `build/tables-generated-c` |
| C fixed-table reference | `make build/schema_test_c_variable` | 0 | built | `build/schema_test_c_variable`, 159,256 bytes |

`file build/schema_test_c_variable` →
`build/schema_test_c_variable: Mach-O 64-bit executable arm64`

The link step ran at the leg's full warning surface and passed with nothing suppressed:

    cc -std=c99 -Wall -Wextra -Werror -Wshadow -Wtype-limits \
       -Wtautological-type-limit-compare -O2 -ffp-contract=off \
       -Ibuild/tables-generated-c/pointers \
       test/c-tables/variable_main.c \
       build/tables-generated-c/pointers/GraphTable.c \
       build/tables-generated-c/pointers/MarksTable.c \
       build/tables-generated-c/pointers/PartsTable.c \
       -o build/schema_test_c_variable -lm

`-Wtautological-type-limit-compare` is feature-tested by the Makefile rather than
assumed, and Apple clang 21 accepts it, so the generated headers were compiled under it.

Two generator warnings were emitted during the generate step and are reproduced here
because they are part of the honest build record, not failures:

* `table RenderFrame: a fixed table's record body is 7533108 bytes, past the 65536-byte
  fixed-form ceiling (docs/SPEC-TABLES.md §3.4) — THE FIXED FORM IS NOT EMITTED FOR IT`
* `table PartFrame: a fixed table's record body is 10892 bytes, past the 4096-byte
  advisory bound (docs/SPEC-TABLES.md §3.4)`

Nothing under `build/` or `bin/` was committed; `git status --short` was checked for
them and only the file you are reading was added.

## How these numbers were made

Inside `nova-sandbox run --name c9703 --size 10g --timeout 30m --read <jobs>
--read /opt/homebrew/Cellar/go/1.27.1` (backend `sandbox-exec`, one disposable APFS
volume per card), after cloning the repository into the volume and checking out
`origin/main`:

    cc --version; make --version | head -1; go version; uname -sm

    make bin/schema;                          echo "rc=$?"; ls -l bin/schema
    make build/tables-generated-c/.stamp;     echo "rc=$?"
    find build/tables-generated-c -name '*.c' -o -name '*.h' | wc -l
    make build/schema_test_c_variable;        echo "rc=$?"
    ls -l build/schema_test_c_variable; file build/schema_test_c_variable
