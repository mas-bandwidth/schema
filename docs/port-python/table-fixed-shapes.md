# Fixed table shapes

The fixed-form shape proof compares the Python writer with a live C++ reference,
loads and re-saves bytes in both directions, and checks decoded values. The
proof covers UTF-8, byte buffers, defaults, nested records and fixed, counted
and keyed arrays. Forged files and a mutated generated writer exercise the
negative controls. Text and writer-bound evolution have additional malformed
input and runtime-fault controls. The generated packages are analyzed with
strict mypy and run under both pinned CPython versions.

`make python-fixed-proof-shapes` runs the proofs;
`make python-fixed-proof-shapes-negative-control` runs the runtime fault controls.
