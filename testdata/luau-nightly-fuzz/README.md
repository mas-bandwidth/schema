# Luau nightly fuzz saved inputs

Every crash the nightly fuzzer (`make luau-nightly-fuzz`) finds is minimised
and committed here as `<name>.bin` with a sibling `<name>.bin.meta`
(`seed=`, `iteration=`, `kind=`, `unit=`, `root=`, `reason=`). The fast lane
replays every saved input with `make luau-nightly-fuzz-replay`; the seed and
iteration make each one reproducible from the fuzzer's own arguments.
