package ci

import (
	"io/fs"
	"os"
	"strings"
	"testing"
)

// Section 6 (the two lives of a table, load paths, retain-unknown) pins.

// Each pin names the test that holds the sentence in that port. The digest
// binds each pin to the row text as the splitter reads it. A port that carries
// only the card constructs is pinned to the test that exercises what it does
// carry; the gap is carried in the card report as FIX-CARD.

var _ = registerSpecPins("internal/ci/schema_spec_pin_s6_test.go",
	specPin{Row: "T6-1", Port: "compiler", Test: "compiler/tables_test.go:TestTablesMoveNoGeneratedPacketByte", Digest: "dcc4e36a62110ad8"},
	specPin{Row: "T6-3", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "go", Test: "internal/tablewire/bytesframing_test.go:TestBytesFieldFramingAcrossLebWidths", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-3", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "7303f5a20c5a31c4"},
	specPin{Row: "T6-4", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "go", Test: "internal/tablewire/cycle_test.go:TestForgedPointerCycleLoadsSilentlyAndRefusesEveryWalk", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-4", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "4f884d63b56e2ffd"},
	specPin{Row: "T6-5", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "go", Test: "internal/tablewire/cycle_test.go:TestForgedPointerCycleLoadsSilentlyAndRefusesEveryWalk", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-5", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "7d63a4ba3c2b20c4"},
	specPin{Row: "T6-7", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "go", Test: "internal/tablewire/map_count_test.go:TestMapBuilderCountCap", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-7", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "d2afbce2cca9ac52"},
	specPin{Row: "T6-8", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-8", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "ba06a9dfb3ec7698"},
	specPin{Row: "T6-9", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-9", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "bb60cf3761d27c19"},
	specPin{Row: "T6-17", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "54240751280f75ff"},
	specPin{Row: "T6-17", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "54240751280f75ff"},
	specPin{Row: "T6-18", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "657c91d202829833"},
	specPin{Row: "T6-18", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "657c91d202829833"},
	specPin{Row: "T6-19", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-19", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "761638d69f0de3e2"},
	specPin{Row: "T6-21", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-21", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "eb6aa042d65bbe1e"},
	specPin{Row: "T6-22", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "53303213a80161f9"},
	specPin{Row: "T6-22", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "53303213a80161f9"},
	specPin{Row: "T6-23", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-23", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "8b9aac0e2a9b1301"},
	specPin{Row: "T6-24", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_wire", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "c", Test: "test/conformance/c/main.c:surface_wire", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "cs", Test: "test/conformance/cs/src/Program.cs:SurfaceWire", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "rust", Test: "test/conformance/rust/src/main.rs:surface_wire", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "java", Test: "test/conformance/java/src/Driver.java:surfaceBlock", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "js", Test: "test/conformance/js/main.mjs:surfaceWire", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "dart", Test: "internal/codegen/darttable/fixedform_test.go:TestFixedLayoutMatchesReference", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-24", Port: "elixir", Test: "test/conformance/elixir/driver_impl.ex:dump", Digest: "6c40ef2c8281ce91"},
	specPin{Row: "T6-25", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_25RetainRust", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_25RetainJava", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_25RetainJs", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_25RetainDart", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-25", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_25RetainElixir", Digest: "2a935c30f197b90f"},
	specPin{Row: "T6-27", Port: "compiler", Test: "compiler/tables_test.go:TestTableRuntimeNamesAreClaimed", Digest: "299badc0cbcd22f1"},
	specPin{Row: "T6-29", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainRoundTrip", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_29RetainRust", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_29RetainJava", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_29RetainJs", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_29RetainDart", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-29", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_29RetainElixir", Digest: "ea2384abc1538649"},
	specPin{Row: "T6-30", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainCapacities", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_30RetainRust", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_30RetainJava", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_30RetainJs", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_30RetainDart", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-30", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_30RetainElixir", Digest: "95a1b512c37a6efc"},
	specPin{Row: "T6-31", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainExcludedClasses", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_31RetainRust", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_31RetainJava", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_31RetainJs", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_31RetainDart", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-31", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_31RetainElixir", Digest: "ad688fd8c73deeff"},
	specPin{Row: "T6-32", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainIsAdditive", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_32RetainRust", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_32RetainJava", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_32RetainJs", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_32RetainDart", Digest: "af014f659b30836a"},
	specPin{Row: "T6-32", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_32RetainElixir", Digest: "af014f659b30836a"},
	specPin{Row: "T6-33", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainUnknownNodeRecord", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_33RetainRust", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_33RetainJava", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_33RetainJs", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_33RetainDart", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-33", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_33RetainElixir", Digest: "2dd9e2b6e202580c"},
	specPin{Row: "T6-35", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainDroppedRecordSpendsNoId", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_35RetainRust", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_35RetainJava", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_35RetainJs", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_35RetainDart", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-35", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_35RetainElixir", Digest: "520468a58c0eb518"},
	specPin{Row: "T6-36", Port: "cpp", Test: "test/conformance/cpp/main.cpp:surface_retain", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "c", Test: "test/conformance/c/retain.c:conformance_retain", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "cs", Test: "test/cs-tables/src/RetainChecks.cs:TestRetention", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "go", Test: "internal/tablewire/retain_test.go:TestRetainWalkDepthCap", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "rust", Owed: "the rust port carries only the card constructs; vt/retain-unknown/rust is todo in docs/roadmap.sexp", Want: "TestRowT6_36RetainRust", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "java", Owed: "the java port carries only the card constructs; vt/retain-unknown/java is todo in docs/roadmap.sexp", Want: "TestRowT6_36RetainJava", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "js", Owed: "the js port carries only the card constructs; vt/retain-unknown/js is todo in docs/roadmap.sexp", Want: "TestRowT6_36RetainJs", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "dart", Owed: "the dart port carries only the card constructs; vt/retain-unknown/dart is todo in docs/roadmap.sexp", Want: "TestRowT6_36RetainDart", Digest: "161425b746e86c76"},
	specPin{Row: "T6-36", Port: "elixir", Owed: "the elixir port carries only the card constructs; vt/retain-unknown/elixir is todo in docs/roadmap.sexp", Want: "TestRowT6_36RetainElixir", Digest: "161425b746e86c76"},
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
