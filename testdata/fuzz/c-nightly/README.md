# Saved C reader fuzz inputs

`make schema-c-nightly-fuzz` writes libFuzzer crash artifacts under
`build/schema-c-nightly-fuzz/artifacts/`. A finding is minimised with the
fuzzer's own `-minimize_crash` and committed here as
`<generated-unit>/<name>`, where `<generated-unit>` is the directory the
harness generator made for the schema (for example `tables_examples`).

`make schema-c-nightly-fuzz-replay` compiles only the units named by a
subdirectory here and runs every saved input through that unit's load, strict
and JSON drivers, with ASan and UBSan, no fuzzer runtime and no clock. That is
the fast regression half of the nightly run and it rides every push.

The planted negative control keeps its own red line under `planted/`; that
directory is replayed against the planted reader, not the shipped one.
