# AGENTS.md — generated map of test/

Do not edit. `make map` regenerates this file. Root: [AGENTS.md](../AGENTS.md). Rules: [CONTRIBUTING.md](../docs/CONTRIBUTING.md).

| dir | purpose | guard | command |
| --- | --- | --- | --- |
| `bench/` | C++ bench drivers | `make test` | `./build/schema_test_bench` |
| `c/` | C packet tests | `make test-c` | `make test-c` |
| `c-ludicrous/` | C int128 tests | `make test-c` | `make test-c` |
| `c-tables/` | C table tests | `make test-c` | `make test-c` |
| `conformance/` | cross-language table matrix | `make conformance` | `make conformance` |
| `cookgen/` | cook fixture generator | `make tables-cook-scale` | `go build -o build/cookgen ./test/cookgen` |
| `cs/` | C# packet tests | `make test-cs` | `make test-cs` |
| `cs-block/` | C# block tests | `make test-cs` | `make test-cs` |
| `cs-cook/` | C# cook tests | `make test-cs` | `make test-cs` |
| `cs-ludicrous/` | C# int128 tests | `make test-cs` | `make test-cs` |
| `cs-tables/` | C# table tests | `make test-cs` | `make test-cs` |
| `cs-view/` | C# view tests | `make test-cs` | `make test-cs` |
| `dart/` | Dart packet tests | `make test-dart` | `make test-dart` |
| `dart-ludicrous/` | Dart int128 tests | `make test-dart` | `make test-dart` |
| `dart-tables/` | Dart table tests | `make test-dart` | `make test-dart` |
| `elixir/` | Elixir packet tests | `make test-elixir` | `make test-elixir` |
| `elixir-fixedform/` | Elixir fixed-form tests | `make test-elixir` | `make test-elixir` |
| `elixir-ludicrous/` | Elixir int128 tests | `make test-elixir` | `make test-elixir` |
| `generated-tree/` | committed generated/ verify | `test/generated-tree/verify` | `make generated-current` |
| `go/` | Go packet tests | `make test-go` | `make test-go` |
| `go-ludicrous/` | Go int128 tests | `make test-go` | `make test-go` |
| `go-tables/` | Go table tests | `make test-go` | `make test-go` |
| `guard/` | C++ include-guard test | `./build/schema_test_guard` | `./build/schema_test_guard` |
| `java/` | Java packet tests | `make test-java` | `make test-java` |
| `java-fixedform/` | Java fixed-form tests | `make test-java` | `make test-java` |
| `java-ludicrous/` | Java int128 tests | `make test-java` | `make test-java` |
| `java-tables/` | Java table tests | `make test-java` | `make test-java` |
| `js/` | JS packet tests | `make test-js` | `make test-js` |
| `js-ludicrous/` | JS int128 tests | `make test-js` | `make test-js` |
| `js-tables/` | JS table tests | `make test-js` | `make test-js` |
| `packet-defaults/` | packet default-value gates | `make test` | `make test` |
| `packet-text/` | packet UTF-8 gates | `make test` | `make test` |
| `packet-void/` | packet void-arm gates | `make test` | `make test` |
| `packet-wide/` | packet wide-text gates | `make test` | `make test` |
| `rust/` | Rust packet tests | `make test-rust` | `make test-rust` |
| `rust-fixedform/` | Rust fixed-form tests | `make test-rust` | `make test-rust` |
| `rust-fuzz/` | Rust fuzz driver | `make test-rust` | `make test-rust` |
| `rust-ludicrous/` | Rust int128 tests | `make test-rust` | `make test-rust` |
| `slowgate/` | SCHEMA_SLOW proof wrapper | `make slow-gate-scan` | `make slow-gate-scan` |
| `table-base64/` | table base64 gates | `make test` | `make test` |
| `tables/` | C++ table tests | `make test` | `./build/schema_test_tables` |
| `vocabgen/` | wide-vocabulary generator | `make check` | `go run ./test/vocabgen` |
| `wide/` | wide-text C++ tests | `./build/schema_test_wide` | `./build/schema_test_wide` |
