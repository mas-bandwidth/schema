# AGENTS.md — generated map of test/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `bench/` | C++ bench drivers | `make test` | `./build/schema_test_bench` |
| `c/` | C packet tests | `make test-c` | `make test-c` |
| `c-ludicrous/` | C int128 tests | `make test-c` | `make test-c` |
| `c-tables/` | C table tests | `make test-c` | `make test-c` |
| `c-two-units/` | C two units in one TU | `make tables-c-two-units` | `make tables-c-two-units` |
| `conformance/` | cross-language table matrix | `make conformance` | `make conformance` |
| `cookgen/` | cook fixture generator | `make tables-cook-scale` | `go build -o build/cookgen ./test/cookgen` |
| `cpp-tables/` | C++ table runtime-home digest | `make tables-cpp-runtime-home` | `make tables-cpp-runtime-home` |
| `cs/` | C# packet tests | `make test-cs` | `make test-cs` |
| `cs-block/` | C# block tests | `make test-cs` | `make test-cs` |
| `cs-cook/` | C# cook tests | `make test-cs` | `make test-cs` |
| `cs-ludicrous/` | C# int128 tests | `make test-cs` | `make test-cs` |
| `cs-tables/` | C# table tests | `make test-cs` | `make test-cs` |
| `cs-view/` | C# view tests | `make test-cs` | `make test-cs` |
| `dart/` | Dart packet tests | `make test-dart` | `make test-dart` |
| `dart-hostile/` | Dart hostile-corpus runner | `make schema-dart-runs-hostile-corpus` | `make schema-dart-runs-hostile-corpus` |
| `dart-ludicrous/` | Dart int128 tests | `make test-dart` | `make test-dart` |
| `dart-tables/` | Dart table tests | `make test-dart` | `make test-dart` |
| `elixir/` | Elixir packet tests | `make test-elixir` | `make test-elixir` |
| `elixir-alloc/` | Elixir allocation tests | `make test-elixir` | `make test-elixir` |
| `elixir-filemodule/` | Elixir file-module tests | `make test-elixir` | `make test-elixir` |
| `elixir-fixedform/` | Elixir fixed-form tests | `make test-elixir` | `make test-elixir` |
| `elixir-hostile/` | Elixir hostile-corpus runner | `make schema-elixir-runs-hostile-corpus` | `make schema-elixir-runs-hostile-corpus` |
| `elixir-ludicrous/` | Elixir int128 tests | `make test-elixir` | `make test-elixir` |
| `elixir-soak/` | Elixir tables soak, allocation count | `make tables-elixir-soak` | `make tables-elixir-soak` |
| `elixir-tables/` | Elixir table tests | `make test-elixir` | `make test-elixir` |
| `elixir-view/` | Elixir view tests | `make test-elixir` | `make test-elixir` |
| `elixir-warning-control/` | Elixir warning-control tests | `make test-elixir` | `make test-elixir` |
| `generated-tree/` | committed generated/ verify | `test/generated-tree/verify` | `make generated-current` |
| `go/` | Go packet tests | `make test-go` | `make test-go` |
| `go-ludicrous/` | Go int128 tests | `make test-go` | `make test-go` |
| `go-tables/` | Go table tests | `make test-go` | `make test-go` |
| `goose/` | Goose packet tests | `make test-goose` | `make test-goose` |
| `goose-hostile/` | Goose hostile-corpus runner | `make schema-goose-runs-hostile-corpus` | `make schema-goose-runs-hostile-corpus` |
| `goose-ludicrous/` | Goose int128 tests | `make test-goose` | `make test-goose` |
| `guard/` | C++ include-guard test | `./build/schema_test_guard` | `./build/schema_test_guard` |
| `java/` | Java packet tests | `make test-java` | `make test-java` |
| `java-fixedform/` | Java fixed-form tests | `make test-java` | `make test-java` |
| `java-hostile/` | Java hostile-corpus runner | `make schema-java-runs-hostile-corpus` | `make schema-java-runs-hostile-corpus` |
| `java-ludicrous/` | Java int128 tests | `make test-java` | `make test-java` |
| `java-tables/` | Java table tests | `make test-java` | `make test-java` |
| `java-view/` | Java unit-view listing | `make tables-java-view` | `make tables-java-view` |
| `js/` | JS packet tests | `make test-js` | `make test-js` |
| `js-ludicrous/` | JS int128 tests | `make test-js` | `make test-js` |
| `js-tables/` | JS table tests | `make test-js` | `make test-js` |
| `js-view/` | JS view listing program | `make tables-js-view` | `make tables-js-view` |
| `lobster-runtime-home/` | Lobster runtime-home check | `make tables-lobster-runtime-home` | `make tables-lobster-runtime-home` |
| `lua/` | Lua interpreter harness | `make test-lua` | `make test-lua` |
| `luau-ludicrous/` | Luau int128 tests | `make test-luau` | `make test-luau` |
| `luau-runtime-home/` | Luau runtime-home check | `make tables-luau-runtime-home` | `make tables-luau-runtime-home` |
| `moo-runtime-home/` | moo runtime-home check | `make tables-moo-runtime-home` | `make tables-moo-runtime-home` |
| `packet-defaults/` | packet default-value gates | `make test` | `make test` |
| `packet-finite/` | packet finite-float gates | `make test-go, make test-dart, make test-elixir, make test-lua, make test-luau` | `make packet-finite-go packet-finite-dart packet-finite-elixir packet-finite-lua packet-finite-luau` |
| `packet-text/` | packet UTF-8 gates | `make test` | `make test` |
| `packet-void/` | packet void-arm gates | `make test` | `make test` |
| `packet-wide/` | packet wide-text gates | `make test` | `make test` |
| `py-packet/` | python packet rows | `make tables-py-packet` | `make tables-py-packet` |
| `rust/` | Rust packet tests | `make test-rust` | `make test-rust` |
| `rust-agree/` | Rust accessor/descriptor agreement gate | `make tables-rust-accessor-descriptor-agreement` | `make tables-rust-accessor-descriptor-agreement` |
| `rust-alloc/` | Rust allocation gate, per read path | `make tables-rust-alloc` | `make tables-rust-alloc` |
| `rust-fixedform/` | Rust fixed-form tests | `make test-rust` | `make test-rust` |
| `rust-fuzz/` | Rust fuzz driver | `make test-rust` | `make test-rust` |
| `rust-ludicrous/` | Rust int128 tests | `make test-rust` | `make test-rust` |
| `rust-soak/` | Rust tables soak, allocator call count | `make tables-rust-soak` | `make tables-rust-soak` |
| `rust-view/` | Rust unit-view listing driver | `make tables-rust-view` | `make tables-rust-view` |
| `slowgate/` | SCHEMA_SLOW proof wrapper | `make slow-gate-scan` | `make slow-gate-scan` |
| `table-base64/` | table base64 gates | `make test` | `make test` |
| `tables/` | C++ table tests | `make test` | `./build/schema_test_tables` |
| `vocabgen/` | wide-vocabulary generator | `make check` | `go run ./test/vocabgen` |
| `wide/` | wide-text C++ tests | `./build/schema_test_wide` | `./build/schema_test_wide` |
