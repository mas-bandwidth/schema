package ci

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Section 6 (the two lives of a table, load paths, retain-unknown) pins.

// Each pin names the test that holds the sentence in that port; an owed entry

// names the port and why this card found no test yet. The digest binds each

// pin to the row text as the splitter reads it.

var _ = registerSpecPins("internal/ci/schema_spec_pin_s6_test.go",
	specPin{Row: "T6-1", Port: "compiler", Test: "compiler/tables_test.go:TestTablesMoveNoGeneratedPacketByte", Digest: "dcc4e36a62110ad8"},
	specPin{Row: "T6-3", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3Cpp", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3C", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3Cs", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3Rust", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3Java", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3Js", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3Dart", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row3Elixir", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-4", Port: "cpp", Test: "test/tables/retain_main.cpp:round_trip", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row4C", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row4Cs", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "go", Test: "internal/tablewire/cycle_test.go:TestForgedPointerCycleLoadsSilentlyAndRefusesEveryWalk", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row4Rust", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row4Java", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row4Js", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row4Dart", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row4Elixir", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-5", Port: "cpp", Test: "test/tables/retain_main.cpp:round_trip", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row5C", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row5Cs", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "go", Test: "internal/tablewire/cycle_test.go:TestForgedPointerCycleLoadsSilentlyAndRefusesEveryWalk", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row5Rust", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row5Java", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row5Js", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row5Dart", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row5Elixir", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-7", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7Cpp", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7C", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7Cs", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "go", Test: "internal/tablewire/map_count_test.go:TestMapBuilderCountCap", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7Rust", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7Java", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7Js", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7Dart", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row7Elixir", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-8", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8Cpp", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8C", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8Cs", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8Rust", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8Java", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8Js", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8Dart", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row8Elixir", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-9", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9Cpp", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9C", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9Cs", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9Rust", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9Java", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9Js", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9Dart", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row9Elixir", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-17", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17Cpp", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17C", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17Cs", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17Rust", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17Java", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17Js", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17Dart", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row17Elixir", Digest: "54240751280f75ff"},
	specPin{Row: "T6-18", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18Cpp", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18C", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18Cs", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18Rust", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18Java", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18Js", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18Dart", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row18Elixir", Digest: "657c91d202829833"},
	specPin{Row: "T6-19", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19Cpp", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19C", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19Cs", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19Rust", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19Java", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19Js", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19Dart", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row19Elixir", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-21", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21Cpp", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21C", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21Cs", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21Rust", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21Java", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21Js", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21Dart", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row21Elixir", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-22", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22Cpp", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22C", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22Cs", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22Rust", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22Java", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22Js", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22Dart", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row22Elixir", Digest: "53303213a80161f9"},
	specPin{Row: "T6-23", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23Cpp", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23C", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23Cs", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23Rust", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23Java", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23Js", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23Dart", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row23Elixir", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-24", Port: "cpp", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24Cpp", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "c", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24C", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "cs", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24Cs", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "rust", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24Rust", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "java", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24Java", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "js", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24Js", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "dart", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24Dart", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "elixir", Owed: "the port carries the feature and this card found no test pinning this sentence", Want: "TestSpecPinS6Row24Elixir", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-25", Port: "cpp", Test: "test/tables/retain_main.cpp:round_trip", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "c", Test: "internal/codegen/ctable/retain_emit_test.go:TestRetainedCodeAnchors", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row25Cs", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "rust", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row25Rust", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "java", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row25Java", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "js", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row25Js", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "dart", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row25Dart", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "elixir", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row25Elixir", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-27", Port: "compiler", Test: "compiler/tables_test.go:TestTableRuntimeNamesAreClaimed", Digest: "299badc0cbcd22f1"},
	specPin{Row: "T6-29", Port: "cpp", Test: "test/tables/retain_main.cpp:round_trip", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "c", Test: "internal/codegen/ctable/retain_emit_test.go:TestRetainedCodeAnchors", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row29Cs", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "rust", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row29Rust", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "java", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row29Java", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "js", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row29Js", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "dart", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row29Dart", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "elixir", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row29Elixir", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-30", Port: "cpp", Test: "test/tables/retain_main.cpp:capacities", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "c", Test: "internal/codegen/ctable/retain_emit_test.go:TestRetainedCodeAnchors", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row30Cs", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainCapacities", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "rust", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row30Rust", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "java", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row30Java", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "js", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row30Js", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "dart", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row30Dart", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "elixir", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row30Elixir", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-31", Port: "cpp", Test: "test/tables/retain_main.cpp:excluded_classes", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "c", Owed: "the port carries retain-unknown but has no test for this exclusion", Want: "TestSpecPinS6Row31C", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row31Cs", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainExcludedClasses", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "rust", Owed: "the port carries retain-unknown but has no test for this exclusion", Want: "TestSpecPinS6Row31Rust", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "java", Owed: "the port carries retain-unknown but has no test for this exclusion", Want: "TestSpecPinS6Row31Java", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "js", Owed: "the port carries retain-unknown but has no test for this exclusion", Want: "TestSpecPinS6Row31Js", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "dart", Owed: "the port carries retain-unknown but has no test for this exclusion", Want: "TestSpecPinS6Row31Dart", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "elixir", Owed: "the port carries retain-unknown but has no test for this exclusion", Want: "TestSpecPinS6Row31Elixir", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-32", Port: "cpp", Test: "test/tables/retain_main.cpp:round_trip", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "c", Test: "internal/codegen/ctable/retain_emit_test.go:TestRetainedCodeAnchors", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row32Cs", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "rust", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row32Rust", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "java", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row32Java", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "js", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row32Js", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "dart", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row32Dart", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "elixir", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row32Elixir", Digest: "af014f659b30836a"},
	specPin{Row: "T6-33", Port: "cpp", Test: "test/tables/retain_main.cpp:round_trip", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "c", Test: "internal/codegen/ctable/retain_emit_test.go:TestRetainedCodeAnchors", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row33Cs", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "rust", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row33Rust", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "java", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row33Java", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "js", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row33Js", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "dart", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row33Dart", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "elixir", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row33Elixir", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-35", Port: "cpp", Test: "test/tables/retain_main.cpp:capacities", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "c", Test: "internal/codegen/ctable/retain_emit_test.go:TestRetainedCodeAnchors", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row35Cs", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainCapacities", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "rust", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row35Rust", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "java", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row35Java", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "js", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row35Js", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "dart", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row35Dart", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "elixir", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row35Elixir", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-36", Port: "cpp", Test: "test/tables/retain_main.cpp:capacities", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "c", Test: "internal/codegen/ctable/retain_emit_test.go:TestRetainedCodeAnchors", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "cs", Owed: "no C# retain test exists; the port carries the feature and the row is owed", Want: "TestSpecPinS6Row36Cs", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainCapacities", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "rust", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row36Rust", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "java", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row36Java", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "js", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row36Js", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "dart", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row36Dart", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "elixir", Owed: "the port's generated-code test skips; a kept test is owed", Want: "TestSpecPinS6Row36Elixir", Digest: "161425b746e86c76"},
)

// TestSpecPinS6PlantedUnpinnedSentence is the section's planted-bad-pattern
// witness: the real spec is green, and a new normative sentence in §6.6 with
// no row id turns the same guard red. It is the section's own copy of the
// design's witness, so a regression that only affects this section's rows
// cannot pass by riding the design file's plants.
func TestSpecPinS6PlantedUnpinnedSentence(t *testing.T) {
	t.Parallel()
	root := repoRoot(t)
	base := os.DirFS(root)
	spec, err := fs.ReadFile(base, "docs/SPEC-TABLES.md")
	if err != nil {
		t.Fatal(err)
	}
	in := specPinsInput{fsys: base, spec: "docs/SPEC-TABLES.md", roadmap: "docs/roadmap.sexp", pins: specPinRegistry}
	if _, findings := checkSpecPins(in); len(findings) != 0 {
		t.Fatalf("the real spec is not clean; the plant proves nothing: %v", findings)
	}
	const heading = "### 6.6 Retain-unknown: an opt-in that keeps what this build cannot name\n"
	if !strings.Contains(string(spec), heading) {
		t.Fatalf("the spec no longer carries the §6.6 heading; the plant needs it")
	}
	planted := strings.Replace(string(spec), heading, heading+"\nA table must retain a field this build cannot name.\n", 1)
	in.fsys = overlayFS{FS: base, over: map[string][]byte{"docs/SPEC-TABLES.md": []byte(planted)}}
	_, findings := checkSpecPins(in)
	all := strings.Join(findings, "\n")
	if !strings.Contains(all, "normative sentence without a row id") || !strings.Contains(all, "6.6 Retain-unknown") {
		t.Fatalf("planted an unpinned rule in §6.6; the guard stayed green: %v", findings)
	}
}
