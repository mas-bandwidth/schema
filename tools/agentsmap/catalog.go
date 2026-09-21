package main

// catalog is the hand-written half of the agent map: one row per directory
// that the live tree is allowed to contain at the mapped layer. tools/agentsmap
// walks the tree, fills purpose/guard/command from this table, and writes
// AGENTS.md pages. go test ./tools/agentsmap fails when the pages drift, when
// a mapped directory appears that has no row, or when a row names a directory
// that is gone.
//
// Adding a directory therefore costs a row here and `make map`. Forgetting
// either is a red test, not a silent hole in the map.

// Entry is one mapped directory.
type Entry struct {
	Path    string // slash-separated, no trailing slash
	Purpose string
	Guard   string
	Command string
	Page    bool // write Path/AGENTS.md listing this directory's children
}

func e(path, purpose, guard, command string) Entry {
	return Entry{Path: path, Purpose: purpose, Guard: guard, Command: command}
}

func page(path, purpose, guard, command string) Entry {
	n := e(path, purpose, guard, command)
	n.Page = true
	return n
}

// catalog is the map. Top-level directories must all appear; a Page directory's
// immediate child directories must all appear.
var catalog = []Entry{
	e(".github", "CI workflows", "go test ./internal/ci", "make test"),
	page("bench", "cross-language serialize bench", "go test ./bench/corpus", "bench/run.sh"),
	e("cmd", "schema CLI", "go test ./cmd/schema", "go test ./cmd/schema"),
	e("comparison", "Cap'n/PB/FlatBuffers packet numbers", "none", "comparison/measure.sh"),
	e("compiler", "public driver API", "go test ./compiler", "go test ./compiler"),
	e("docs", "spec, tutorial, contributing", "go test ./compiler", "go test ./compiler"),
	e("examples", "type-wire corpus", "go test ./internal/goldens", "make check"),
	e("examples-wide", "wide-text corpus", "go test ./internal/goldens", "make check"),
	e("examples128", "int128/fixed corpus", "go test ./internal/goldens", "make check"),
	e("generated", "committed nine-language output", "test/generated-tree/verify", "make generated-current"),
	e("images", "README art", "none", "none"),
	page("internal", "compiler internals", "go test ./internal/...", "go test ./internal/..."),
	e("ir", "public IR", "go test ./ir", "go test ./ir"),
	e("make", "per-language make fragments", "go test ./tools/negativecontrols", "make registry"),
	e("notes", "non-normative history", "none", "none"),
	page("tables", "table-wire corpora", "make check", "make check"),
	page("test", "language test harnesses", "make test", "make test"),
	page("testdata", "goldens, wire pins, conformance", "go test ./internal/goldens", "make update-goldens"),
	page("tools", "maintainer tools", "go test ./tools/...", "go test ./tools/..."),
	e("working", "scratch; not normative", "none", "none"),

	// bench/
	e("bench/c", "C serialize runner", "bench/run.sh --only c", "bench/run.sh --only c"),
	e("bench/corpus", "bench schemas and weighting gate", "go test ./bench/corpus", "go test ./bench/corpus"),
	e("bench/cpp", "C++ reference runner", "bench/run.sh --only cpp", "bench/run.sh --only cpp"),
	e("bench/cs", "C# runner", "bench/run.sh --only cs", "bench/run.sh --only cs"),
	e("bench/dart", "Dart runner", "bench/run.sh --only dart", "bench/run.sh --only dart"),
	e("bench/elixir", "Elixir runner", "bench/run.sh --only elixir", "bench/run.sh --only elixir"),
	e("bench/go", "Go runner", "bench/run.sh --only go", "bench/run.sh --only go"),
	e("bench/java", "Java runner", "bench/run.sh --only java", "bench/run.sh --only java"),
	e("bench/js", "JavaScript runner", "bench/run.sh --only js", "bench/run.sh --only js"),
	e("bench/paired", "paired before/after + fixed-form bytes", "make bench-paired-check", "make bench-paired-gate"),
	e("bench/results", "committed CSV ledgers", "none", "bench/run.sh"),
	e("bench/rust", "Rust runner", "bench/run.sh --only rust", "bench/run.sh --only rust"),
	e("bench/tables", "tables bench pass", "make bench-tables", "make bench-tables"),
	e("bench/tools", "bench aggregation and shape gate", "go test ./bench/tools", "go test ./bench/tools"),

	// internal/
	e("internal/ast", "parsed *.schema form", "go test ./internal/parser", "go test ./internal/parser"),
	e("internal/baseline", "tables baseline pins", "go test ./internal/baseline", "go test ./internal/baseline"),
	e("internal/check", "resolve, check, lower to IR", "go test ./internal/check", "go test ./internal/check"),
	e("internal/ci", "workflow pin and SDK gates", "go test ./internal/ci", "go test ./internal/ci"),
	page("internal/codegen", "nine language emitters", "go test ./internal/codegen/...", "go test ./internal/codegen/..."),
	e("internal/format", "schemafmt", "go test ./internal/format", "go test ./internal/format"),
	e("internal/fuzz", "seeded fuzz corpus", "go test ./internal/fuzz", "go test ./internal/fuzz"),
	e("internal/goldens", "source, id, and wire pins", "go test ./internal/goldens", "make update-goldens"),
	e("internal/listwalk", "unbounded-array walk", "go test ./internal/listwalk", "go test ./internal/listwalk"),
	e("internal/lockfile", "schema.lock lineage", "go test ./internal/lockfile", "go test ./internal/lockfile"),
	e("internal/parser", "recursive-descent parser", "go test ./internal/parser", "go test ./internal/parser"),
	e("internal/publicapi", "external-module API gate", "go test ./internal/publicapi", "go test ./internal/publicapi"),
	e("internal/scanner", "tokenizer", "go test ./internal/parser", "go test ./internal/parser"),
	e("internal/slowtest", "1-2 minute unit-test gate", "make slow-gate-scan", "make slow-gate-scan"),
	e("internal/tablecook", "cook form", "go test ./internal/tablecook", "go test ./internal/tablecook"),
	e("internal/tablenames", "per-language claimed names", "go test ./compiler", "go test ./compiler"),
	e("internal/tablepack", "pack/unpack", "go test ./internal/tablepack", "go test ./internal/tablepack"),
	e("internal/tabletext", "table JSON text", "go test ./internal/tabletext", "go test ./internal/tabletext"),
	e("internal/tablewire", "table wire codecs", "go test ./internal/tablewire", "go test ./internal/tablewire"),
	e("internal/version", "binary version stamp", "go test ./internal/version", "go test ./internal/version"),
	e("internal/viewlisting", "view listing vs IR", "go test ./internal/viewlisting", "go test ./internal/viewlisting"),

	// internal/codegen/
	e("internal/codegen/c", "C packet emitter", "go test ./internal/codegen/c", "go test ./internal/codegen/c"),
	e("internal/codegen/ccommon", "C/C++ shared emit bits", "go test ./internal/codegen/c", "go test ./internal/codegen/c"),
	e("internal/codegen/cpp", "C++ packet emitter (reference)", "go test ./internal/codegen/cpp", "go test ./internal/codegen/cpp"),
	e("internal/codegen/cpptable", "C++ table emitter (reference)", "go test ./internal/codegen/cpptable", "go test ./internal/codegen/cpptable"),
	e("internal/codegen/csharp", "C# packet emitter", "go test ./internal/codegen/csharp", "go test ./internal/codegen/csharp"),
	e("internal/codegen/cstable", "C# table emitter", "go test ./internal/codegen/cstable", "go test ./internal/codegen/cstable"),
	e("internal/codegen/ctable", "C table emitter", "go test ./internal/codegen/ctable", "go test ./internal/codegen/ctable"),
	e("internal/codegen/dart", "Dart packet emitter", "go test ./internal/codegen/dart", "go test ./internal/codegen/dart"),
	e("internal/codegen/darttable", "Dart table emitter", "go test ./internal/codegen/darttable", "go test ./internal/codegen/darttable"),
	e("internal/codegen/elixir", "Elixir packet emitter", "go test ./internal/codegen/elixirtable", "go test ./internal/codegen/elixirtable"),
	e("internal/codegen/elixirtable", "Elixir table emitter", "go test ./internal/codegen/elixirtable", "go test ./internal/codegen/elixirtable"),
	e("internal/codegen/golang", "Go packet emitter", "go test ./internal/codegen/golang", "go test ./internal/codegen/golang"),
	e("internal/codegen/gotable", "Go table emitter", "go test ./internal/codegen/gotable", "go test ./internal/codegen/gotable"),
	e("internal/codegen/java", "Java packet emitter", "go test ./internal/codegen/java", "go test ./internal/codegen/java"),
	e("internal/codegen/javatable", "Java table emitter", "go test ./internal/codegen/javatable", "go test ./internal/codegen/javatable"),
	e("internal/codegen/js", "JS packet emitter", "go test ./internal/codegen/js", "go test ./internal/codegen/js"),
	e("internal/codegen/jstable", "JS table emitter", "go test ./internal/codegen/jstable", "go test ./internal/codegen/jstable"),
	e("internal/codegen/rust", "Rust packet emitter", "go test ./internal/codegen/rust", "go test ./internal/codegen/rust"),
	e("internal/codegen/rusttable", "Rust table emitter", "go test ./internal/codegen/rusttable", "go test ./internal/codegen/rusttable"),

	// tables/
	e("tables/arms", "union-arm traversal corpus", "make check", "bin/schema check tables/arms"),
	e("tables/backend", "message-form backend corpus", "make check", "bin/schema check tables/backend"),
	e("tables/blobs", "byte-buffer corpus", "make check", "bin/schema check tables/blobs"),
	e("tables/block", "block-form corpus", "make check", "bin/schema check tables/block"),
	e("tables/blockhome", "block-home corpus", "make check", "bin/schema check tables/blockhome"),
	e("tables/examples", "table examples corpus", "make check", "bin/schema check tables/examples"),
	e("tables/lists", "unbounded-array corpus", "make check", "bin/schema check tables/lists"),
	e("tables/maps", "map corpus", "make check", "bin/schema check tables/maps"),
	e("tables/messages", "message-union corpus", "make check", "bin/schema check tables/messages"),
	e("tables/pack", "pack fixtures", "make tables-pack", "make tables-pack"),
	e("tables/pointers", "pointer corpus", "make check", "bin/schema check tables/pointers"),
	e("tables/scalars", "scalar corpus", "make check", "bin/schema check tables/scalars"),
	e("tables/stream", "variable message-arm corpus", "make check", "bin/schema check tables/stream"),
	e("tables/vocab", "wide-vocabulary corpus", "make check", "bin/schema check tables/vocab"),
	e("tables/vocab9", "vocab-9 corpus", "make check", "bin/schema check tables/vocab9"),

	// testdata/
	e("testdata/conformance", "table conformance dumps", "make conformance", "make conformance"),
	e("testdata/golden", "generated-source goldens", "go test ./internal/goldens", "make update-goldens"),
	e("testdata/wire", "pinned wire bytes", "go test ./internal/goldens", "make update-goldens"),

	// tools/
	e("tools/agentsmap", "AGENTS.md generator and drift guard", "go test ./tools/agentsmap", "make map"),
	e("tools/fixedtwin", "fixed-form twin", "go test ./tools/fixedtwin", "go test ./tools/fixedtwin"),
	e("tools/negativecontrols", "negative-control enumerator", "go test ./tools/negativecontrols", "go run ./tools/negativecontrols check"),
	e("tools/roadmap", "ROADMAP.md table honesty", "go test ./tools/roadmap", "go test ./tools/roadmap"),
	e("tools/sabotage", "negative-control sabotages", "go test ./tools/negativecontrols", "go run ./tools/sabotage"),
	e("tools/slowgatescan", "SCHEMA_SLOW recipe scan", "make slow-gate-scan", "make slow-gate-scan"),
	e("tools/treelock", "generated-tree lock", "go test ./tools/treelock", "go test ./tools/treelock"),
	e("tools/vuln", "govulncheck wrapper", "make vuln-selftest", "make vuln-selftest"),

	// test/
	e("test/bench", "C++ bench drivers", "make test", "./build/schema_test_bench"),
	e("test/c", "C packet tests", "make test-c", "make test-c"),
	e("test/c-ludicrous", "C int128 tests", "make test-c", "make test-c"),
	e("test/c-tables", "C table tests", "make test-c", "make test-c"),
	e("test/conformance", "cross-language table matrix", "make conformance", "make conformance"),
	e("test/cookgen", "cook fixture generator", "make tables-cook-scale", "go build -o build/cookgen ./test/cookgen"),
	e("test/cs", "C# packet tests", "make test-cs", "make test-cs"),
	e("test/cs-block", "C# block tests", "make test-cs", "make test-cs"),
	e("test/cs-cook", "C# cook tests", "make test-cs", "make test-cs"),
	e("test/cs-ludicrous", "C# int128 tests", "make test-cs", "make test-cs"),
	e("test/cs-tables", "C# table tests", "make test-cs", "make test-cs"),
	e("test/cs-view", "C# view tests", "make test-cs", "make test-cs"),
	e("test/dart", "Dart packet tests", "make test-dart", "make test-dart"),
	e("test/dart-ludicrous", "Dart int128 tests", "make test-dart", "make test-dart"),
	e("test/dart-tables", "Dart table tests", "make test-dart", "make test-dart"),
	e("test/elixir", "Elixir packet tests", "make test-elixir", "make test-elixir"),
	e("test/elixir-fixedform", "Elixir fixed-form tests", "make test-elixir", "make test-elixir"),
	e("test/elixir-ludicrous", "Elixir int128 tests", "make test-elixir", "make test-elixir"),
	e("test/generated-tree", "committed generated/ verify", "test/generated-tree/verify", "make generated-current"),
	e("test/go", "Go packet tests", "make test-go", "make test-go"),
	e("test/go-ludicrous", "Go int128 tests", "make test-go", "make test-go"),
	e("test/go-tables", "Go table tests", "make test-go", "make test-go"),
	e("test/guard", "C++ include-guard test", "./build/schema_test_guard", "./build/schema_test_guard"),
	e("test/java", "Java packet tests", "make test-java", "make test-java"),
	e("test/java-fixedform", "Java fixed-form tests", "make test-java", "make test-java"),
	e("test/java-ludicrous", "Java int128 tests", "make test-java", "make test-java"),
	e("test/java-tables", "Java table tests", "make test-java", "make test-java"),
	e("test/js", "JS packet tests", "make test-js", "make test-js"),
	e("test/js-ludicrous", "JS int128 tests", "make test-js", "make test-js"),
	e("test/js-tables", "JS table tests", "make test-js", "make test-js"),
	e("test/packet-defaults", "packet default-value gates", "make test", "make test"),
	e("test/packet-text", "packet UTF-8 gates", "make test", "make test"),
	e("test/packet-void", "packet void-arm gates", "make test", "make test"),
	e("test/packet-wide", "packet wide-text gates", "make test", "make test"),
	e("test/rust", "Rust packet tests", "make test-rust", "make test-rust"),
	e("test/rust-fixedform", "Rust fixed-form tests", "make test-rust", "make test-rust"),
	e("test/rust-fuzz", "Rust fuzz driver", "make test-rust", "make test-rust"),
	e("test/rust-ludicrous", "Rust int128 tests", "make test-rust", "make test-rust"),
	e("test/slowgate", "SCHEMA_SLOW proof wrapper", "make slow-gate-scan", "make slow-gate-scan"),
	e("test/table-base64", "table base64 gates", "make test", "make test"),
	e("test/tables", "C++ table tests", "make test", "./build/schema_test_tables"),
	e("test/vocabgen", "wide-vocabulary generator", "make check", "go run ./test/vocabgen"),
	e("test/wide", "wide-text C++ tests", "./build/schema_test_wide", "./build/schema_test_wide"),
}
