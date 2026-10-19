# StreamData 1.4.0 (vendored)

`lib/stream_data.ex` and `lib/stream_data/lazy_tree.ex` are the two library
modules of StreamData 1.4.0, the property-based data generation library the
nightly Elixir fuzz harness runs its mutations through.

- source: https://github.com/whatyouhide/stream_data (Hex package `stream_data`)
- package: https://repo.hex.pm/tarballs/stream_data-1.4.0.tar
- tarball sha256: 2b0ee3a340dcce1c8cf6302a763ee757d1e01c54d6e16d9069062509d68b1dc9
- license: Apache-2.0 (per the Hex package metadata)
- vendored: 2026-10-19, only the two library modules; `ExUnitProperties`
  (the ExUnit adapter) is not needed by the harness's plain property runner.

The make fragment compiles these two files into its own ebin, so the nightly
run needs no network, no Hex install and no mix project.
