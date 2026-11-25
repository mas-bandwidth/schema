# Input-family accounting rows (retired ci-fast.yml input-family job).
# Each target runs one reader leg of one input family under
# tools/inputaccount exactly as the old job did: the leg must open and
# judge every input of the family, and the planted extra and wrong
# inputs must each turn the run red. The registry rows in
# test/registry/registry.jsonl (tooling.inputaccount.*) run these
# targets in the pr group; test/registry/retired.tsv names them.

.PHONY: inputaccount-packet-write-bareint-js
inputaccount-packet-write-bareint-js:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg js NODE=node
	go run ./tools/inputaccount -family packet-write-bareint -leg js -canary extra NODE=node
	go run ./tools/inputaccount -family packet-write-bareint -leg js -canary wrong NODE=node

.PHONY: inputaccount-packet-write-bareint-dart
inputaccount-packet-write-bareint-dart:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg dart DART=dart
	go run ./tools/inputaccount -family packet-write-bareint -leg dart -canary extra DART=dart
	go run ./tools/inputaccount -family packet-write-bareint -leg dart -canary wrong DART=dart

.PHONY: inputaccount-packet-write-bareint-elixir
inputaccount-packet-write-bareint-elixir:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg elixir BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))
	go run ./tools/inputaccount -family packet-write-bareint -leg elixir -canary extra BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))
	go run ./tools/inputaccount -family packet-write-bareint -leg elixir -canary wrong BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))

.PHONY: inputaccount-packet-write-bareint-lua
inputaccount-packet-write-bareint-lua:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg lua
	go run ./tools/inputaccount -family packet-write-bareint -leg lua -canary extra
	go run ./tools/inputaccount -family packet-write-bareint -leg lua -canary wrong

.PHONY: inputaccount-packet-write-bareint-luau
inputaccount-packet-write-bareint-luau:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg luau
	go run ./tools/inputaccount -family packet-write-bareint -leg luau -canary extra
	go run ./tools/inputaccount -family packet-write-bareint -leg luau -canary wrong

.PHONY: inputaccount-packet-write-bareint-moo
inputaccount-packet-write-bareint-moo:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg moo
	go run ./tools/inputaccount -family packet-write-bareint -leg moo -canary extra
	go run ./tools/inputaccount -family packet-write-bareint -leg moo -canary wrong

.PHONY: inputaccount-packet-write-bareint-squirrel
inputaccount-packet-write-bareint-squirrel:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg squirrel
	go run ./tools/inputaccount -family packet-write-bareint -leg squirrel -canary extra
	go run ./tools/inputaccount -family packet-write-bareint -leg squirrel -canary wrong

.PHONY: inputaccount-packet-write-bareint-lobster
inputaccount-packet-write-bareint-lobster:
	rm -f test/packet-write-bareint/zz-inputaccount-canary* test/packet-write-bareint/*/zz-inputaccount-canary*
	rm -rf build/packet-write-bareint build/java-finite
	go run ./tools/inputaccount -family packet-write-bareint -leg lobster
	go run ./tools/inputaccount -family packet-write-bareint -leg lobster -canary extra
	go run ./tools/inputaccount -family packet-write-bareint -leg lobster -canary wrong

.PHONY: inputaccount-packet-write-cfloat-cpp
inputaccount-packet-write-cfloat-cpp:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg cpp
	go run ./tools/inputaccount -family packet-write-cfloat -leg cpp -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg cpp -canary wrong

.PHONY: inputaccount-packet-write-cfloat-c
inputaccount-packet-write-cfloat-c:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg c
	go run ./tools/inputaccount -family packet-write-cfloat -leg c -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg c -canary wrong

.PHONY: inputaccount-packet-write-cfloat-rust
inputaccount-packet-write-cfloat-rust:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg rust RUSTUP_BIN=$$(dirname $$(command -v cargo))
	go run ./tools/inputaccount -family packet-write-cfloat -leg rust -canary extra RUSTUP_BIN=$$(dirname $$(command -v cargo))
	go run ./tools/inputaccount -family packet-write-cfloat -leg rust -canary wrong RUSTUP_BIN=$$(dirname $$(command -v cargo))

.PHONY: inputaccount-packet-write-cfloat-js
inputaccount-packet-write-cfloat-js:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg js NODE=node
	go run ./tools/inputaccount -family packet-write-cfloat -leg js -canary extra NODE=node
	go run ./tools/inputaccount -family packet-write-cfloat -leg js -canary wrong NODE=node

.PHONY: inputaccount-packet-write-cfloat-cs
inputaccount-packet-write-cfloat-cs:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg cs
	go run ./tools/inputaccount -family packet-write-cfloat -leg cs -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg cs -canary wrong

.PHONY: inputaccount-packet-write-cfloat-java
inputaccount-packet-write-cfloat-java:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg java JAVA=java JAVAC=javac
	go run ./tools/inputaccount -family packet-write-cfloat -leg java -canary extra JAVA=java JAVAC=javac
	go run ./tools/inputaccount -family packet-write-cfloat -leg java -canary wrong JAVA=java JAVAC=javac

.PHONY: inputaccount-packet-write-cfloat-dart
inputaccount-packet-write-cfloat-dart:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg dart DART=dart
	go run ./tools/inputaccount -family packet-write-cfloat -leg dart -canary extra DART=dart
	go run ./tools/inputaccount -family packet-write-cfloat -leg dart -canary wrong DART=dart

.PHONY: inputaccount-packet-write-cfloat-elixir
inputaccount-packet-write-cfloat-elixir:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg elixir BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))
	go run ./tools/inputaccount -family packet-write-cfloat -leg elixir -canary extra BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))
	go run ./tools/inputaccount -family packet-write-cfloat -leg elixir -canary wrong BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))

.PHONY: inputaccount-packet-write-cfloat-lua
inputaccount-packet-write-cfloat-lua:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg lua
	go run ./tools/inputaccount -family packet-write-cfloat -leg lua -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg lua -canary wrong

.PHONY: inputaccount-packet-write-cfloat-luau
inputaccount-packet-write-cfloat-luau:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg luau
	go run ./tools/inputaccount -family packet-write-cfloat -leg luau -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg luau -canary wrong

.PHONY: inputaccount-packet-write-cfloat-moo
inputaccount-packet-write-cfloat-moo:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg moo
	go run ./tools/inputaccount -family packet-write-cfloat -leg moo -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg moo -canary wrong

.PHONY: inputaccount-packet-write-cfloat-squirrel
inputaccount-packet-write-cfloat-squirrel:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg squirrel
	go run ./tools/inputaccount -family packet-write-cfloat -leg squirrel -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg squirrel -canary wrong

.PHONY: inputaccount-packet-write-cfloat-goose
inputaccount-packet-write-cfloat-goose:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg goose
	go run ./tools/inputaccount -family packet-write-cfloat -leg goose -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg goose -canary wrong

.PHONY: inputaccount-packet-write-cfloat-lobster
inputaccount-packet-write-cfloat-lobster:
	rm -f test/packet-write-cfloat/zz-inputaccount-canary* test/packet-write-cfloat/*/zz-inputaccount-canary*
	rm -rf build/packet-write-cfloat build/java-finite
	go run ./tools/inputaccount -family packet-write-cfloat -leg lobster
	go run ./tools/inputaccount -family packet-write-cfloat -leg lobster -canary extra
	go run ./tools/inputaccount -family packet-write-cfloat -leg lobster -canary wrong

.PHONY: inputaccount-packet-cfloat-endpoints-go
inputaccount-packet-cfloat-endpoints-go:
	rm -f test/packet-cfloat-endpoints/zz-inputaccount-canary* test/packet-cfloat-endpoints/*/zz-inputaccount-canary*
	rm -rf build/packet-cfloat-endpoints build/java-finite
	go run ./tools/inputaccount -family packet-cfloat-endpoints -leg go
	go run ./tools/inputaccount -family packet-cfloat-endpoints -leg go -canary extra
	go run ./tools/inputaccount -family packet-cfloat-endpoints -leg go -canary wrong

.PHONY: inputaccount-table-write-bounds-go
inputaccount-table-write-bounds-go:
	rm -f testdata/conformance/table-write-bounds/zz-inputaccount-canary*
	rm -rf build/table-write-bounds build/java-finite
	go run ./tools/inputaccount -family table-write-bounds -leg go
	go run ./tools/inputaccount -family table-write-bounds -leg go -canary extra
	go run ./tools/inputaccount -family table-write-bounds -leg go -canary wrong

.PHONY: inputaccount-packet-write-text-cpp
inputaccount-packet-write-text-cpp:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg cpp
	go run ./tools/inputaccount -family packet-write-text -leg cpp -canary extra
	go run ./tools/inputaccount -family packet-write-text -leg cpp -canary wrong

.PHONY: inputaccount-packet-write-text-c
inputaccount-packet-write-text-c:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg c
	go run ./tools/inputaccount -family packet-write-text -leg c -canary extra
	go run ./tools/inputaccount -family packet-write-text -leg c -canary wrong

.PHONY: inputaccount-packet-write-text-rust
inputaccount-packet-write-text-rust:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg rust RUSTUP_BIN=$$(dirname $$(command -v cargo))
	go run ./tools/inputaccount -family packet-write-text -leg rust -canary extra RUSTUP_BIN=$$(dirname $$(command -v cargo))
	go run ./tools/inputaccount -family packet-write-text -leg rust -canary wrong RUSTUP_BIN=$$(dirname $$(command -v cargo))

.PHONY: inputaccount-packet-write-text-js
inputaccount-packet-write-text-js:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg js NODE=node
	go run ./tools/inputaccount -family packet-write-text -leg js -canary extra NODE=node
	go run ./tools/inputaccount -family packet-write-text -leg js -canary wrong NODE=node

.PHONY: inputaccount-packet-write-text-cs
inputaccount-packet-write-text-cs:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg cs
	go run ./tools/inputaccount -family packet-write-text -leg cs -canary extra
	go run ./tools/inputaccount -family packet-write-text -leg cs -canary wrong

.PHONY: inputaccount-packet-write-text-java
inputaccount-packet-write-text-java:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg java JAVA=java JAVAC=javac
	go run ./tools/inputaccount -family packet-write-text -leg java -canary extra JAVA=java JAVAC=javac
	go run ./tools/inputaccount -family packet-write-text -leg java -canary wrong JAVA=java JAVAC=javac

.PHONY: inputaccount-packet-write-text-dart
inputaccount-packet-write-text-dart:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg dart DART=dart
	go run ./tools/inputaccount -family packet-write-text -leg dart -canary extra DART=dart
	go run ./tools/inputaccount -family packet-write-text -leg dart -canary wrong DART=dart

.PHONY: inputaccount-packet-write-text-elixir
inputaccount-packet-write-text-elixir:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg elixir BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))
	go run ./tools/inputaccount -family packet-write-text -leg elixir -canary extra BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))
	go run ./tools/inputaccount -family packet-write-text -leg elixir -canary wrong BEAM_PATH=$$(dirname $$(command -v erl)):$$(dirname $$(command -v elixir))

.PHONY: inputaccount-packet-write-text-lua
inputaccount-packet-write-text-lua:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg lua
	go run ./tools/inputaccount -family packet-write-text -leg lua -canary extra
	go run ./tools/inputaccount -family packet-write-text -leg lua -canary wrong

.PHONY: inputaccount-packet-write-text-luau
inputaccount-packet-write-text-luau:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg luau
	go run ./tools/inputaccount -family packet-write-text -leg luau -canary extra
	go run ./tools/inputaccount -family packet-write-text -leg luau -canary wrong

.PHONY: inputaccount-packet-write-text-squirrel
inputaccount-packet-write-text-squirrel:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg squirrel
	go run ./tools/inputaccount -family packet-write-text -leg squirrel -canary extra
	go run ./tools/inputaccount -family packet-write-text -leg squirrel -canary wrong

.PHONY: inputaccount-packet-write-text-goose
inputaccount-packet-write-text-goose:
	rm -f test/packet-write-text/zz-inputaccount-canary* test/packet-write-text/*/zz-inputaccount-canary*
	rm -rf build/packet-write-text build/java-finite
	go run ./tools/inputaccount -family packet-write-text -leg goose
	go run ./tools/inputaccount -family packet-write-text -leg goose -canary extra
	go run ./tools/inputaccount -family packet-write-text -leg goose -canary wrong


















