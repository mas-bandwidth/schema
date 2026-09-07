# Table JSON Base64

`Bytes.schema` declares a single `bytes(16384)` field. Each native driver calls
its generated public JSON reader and writer. The Go harness supplies 990 cases
and compares decoded bytes, report counters, and canonical writer output with
independent expectations from Go's standard Base64 and JSON libraries.

Coverage includes every alphabet symbol in each of the four positions, all
other byte values, lengths 0–257, larger boundary lengths through 16400,
truncation to the declared bound, missing/interspersed padding, and incomplete
groups. Invalid Base64 retains the existing kind-mismatch/default behavior;
JSON framing damage remains a separate report. The tests pass against the
pre-optimization emitters too. The C++ table suite additionally exercises the
variable-size `*bytes` reader, including NUL rejection.

Run from the repository root with the usual language toolchain variables:

```sh
make table-base64-cpp table-base64-c table-base64-go table-base64-rust \
     table-base64-cs table-base64-java table-base64-js table-base64-dart \
     table-base64-elixir
```

These checks also run under `make test` / each corresponding `test-<lang>`.
No runtime repository supplies Base64; these codecs belong to the schema
emitters. The lookup tables replace per-character alphabet searches. Elixir
also discards consumed accumulator bits (avoiding a growing bignum) and uses
`Base.encode64/1` for canonical output. Decoder padding/partial-group behavior
is intentionally retained.

Performance measurements belong to the repository's data-driven benchmark
harness (`bench/README.md`). This suite imposes no wall-clock gate and makes no
binary packet or whole-application performance claim.
