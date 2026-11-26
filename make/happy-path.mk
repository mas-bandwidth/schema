# make/happy-path.mk — THE PER-PR HAPPY PATH (the owner, 2026-11-25: per pull
# request, schema runs ONLY compile + one simple happy path per language, one
# job of two minutes or less; everything else is nightly).
#
# happy-<lang> generates test/happy/Happy.schema for one language into a fresh
# build/happy/<lang>, compiles it with the program in test/happy/<lang>, and
# runs one plain round trip: a ShipCreate written, read back into a fresh
# value, every field compared. The program prints `happy <lang>: ok (N bytes)`
# and any failure is a non-zero exit. Each target does that and nothing more:
# the gates, corpora, fuzz, lints and conformance legs run nightly
# (ci-fast-nightly.yml, ci-full.yml and the per-language nightly workflows).
#
# ci-fast.yml's one job runs `make happy-<lang>` for every leg `make -s
# happy-legs` prints, and times each. internal/ci TestHappyPathCoversEveryLeg
# holds HAPPY_LEGS to the languages of test/registry/registry.jsonl and to the
# targets below, so a leg added there without its happy path is red.
HAPPY_LEGS := c cpp cs dart elixir go goose java js lobster lua luau moo python ruby rust squirrel unity

.PHONY: happy happy-legs
happy: $(addprefix happy-,$(HAPPY_LEGS))

happy-legs:
	@echo $(HAPPY_LEGS)

# C: the generated headers and serialize.c only, -Werror as the leg builds.
.PHONY: happy-c
happy-c: bin/schema
	@rm -rf build/happy/c && mkdir -p build/happy/c
	./bin/schema generate --lang c --out build/happy/c test/happy/Happy.schema
	$(CC) -std=c99 -Wall -Wextra -Werror -ffp-contract=off -Ibuild/happy/c -I$(SERIALIZE_C) \
		test/happy/c/main.c $(SERIALIZE_C)/serialize.c -o build/happy/c/happy -lm
	./build/happy/c/happy

# C++: one translation unit; the serialize runtime is header-only.
.PHONY: happy-cpp
happy-cpp: bin/schema
	@rm -rf build/happy/cpp && mkdir -p build/happy/cpp
	./bin/schema generate --lang cpp --out build/happy/cpp test/happy/Happy.schema
	$(CXX) $(CXXFLAGS) -Ibuild/happy/cpp test/happy/cpp/main.cpp -o build/happy/cpp/happy
	./build/happy/cpp/happy

# C#: no msbuild (a project build costs seconds before it compiles a line).
# The SDK's own Roslyn compiler (csc.dll) builds the program in one call
# against the SDK's reference pack, and the host runs it from a
# runtimeconfig.json.
.PHONY: happy-cs
happy-cs: bin/schema toolchain-cs
	@rm -rf build/happy/cs && mkdir -p build/happy/cs
	./bin/schema generate --lang cs --out build/happy/cs test/happy/Happy.schema
	@set -e; \
	root=$$(dirname "$$(readlink -f "$$(command -v $(DOTNET))")"); \
	sdk=$$($(DOTNET) --version); \
	tfm=net$${sdk%%.*}.0; \
	ref=$$(ls -d "$$root"/packs/Microsoft.NETCore.App.Ref/$${sdk%%.*}.*/ref/$$tfm | sort -V | tail -1); \
	[ -d "$$ref" ] || { echo "happy-cs: no $$tfm reference pack under $$root/packs"; exit 1; }; \
	echo "csc ($$sdk, $$tfm) -> build/happy/cs/happy.dll"; \
	$(DOTNET) exec "$$root/sdk/$$sdk/Roslyn/bincore/csc.dll" -nologo -noconfig \
		-langversion:latest -nullable:enable -optimize- -define:DEBUG -warnaserror \
		$$(for r in "$$ref"/*.dll; do printf ' -r:%s' "$$r"; done) \
		-out:build/happy/cs/happy.dll \
		$(SERIALIZE_CS)/src/Serialize.cs $(SERIALIZE_CS)/src/Int128Pair.cs \
		build/happy/cs/Happy.cs test/happy/cs/Main.cs; \
	printf '{"runtimeOptions":{"tfm":"%s","framework":{"name":"Microsoft.NETCore.App","version":"%s"}}}\n' \
		"$$tfm" "$$(basename "$$(dirname "$$(dirname "$$ref")")")" > build/happy/cs/happy.runtimeconfig.json
	$(DOTNET) exec build/happy/cs/happy.dll

# Dart: straight from source under the JIT, asserts on, no pub get.
.PHONY: happy-dart
happy-dart: bin/schema toolchain-dart
	@rm -rf build/happy/dart && mkdir -p build/happy/dart
	./bin/schema generate --lang dart --out build/happy/dart test/happy/Happy.schema
	$(DART) --enable-asserts test/happy/dart/main.dart

# Elixir: the script loads the generated modules with Code.require_file; no mix.
.PHONY: happy-elixir
happy-elixir: bin/schema
	@$(call toolchain_probe,elixir,ELIXIR,$(ELIXIR),PATH="$(BEAM_PATH):$$PATH" command -v $(lastword $(ELIXIR)))
	@rm -rf build/happy/elixir && mkdir -p build/happy/elixir
	./bin/schema generate --lang elixir --out build/happy/elixir test/happy/Happy.schema
	$(ELIXIR) test/happy/elixir/main.exs build/happy/elixir

# Go: test/happy/go is a module whose replaces name the generated package and
# the serialize.go sibling; GOPROXY=off, so nothing is fetched.
.PHONY: happy-go
happy-go: bin/schema
	@rm -rf build/happy/go && mkdir -p build/happy/go
	./bin/schema generate --lang go --out build/happy/go test/happy/Happy.schema
	@printf 'module happy\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => "%s/$(SERIALIZE_GO)"\n' "$(CURDIR)" > build/happy/go/go.mod
	cd test/happy/go && GOPROXY=off go build -o ../../../build/happy/go/happy-bin .
	./build/happy/go/happy-bin

# Goose: `goose run` compiles and runs in one step; imports are relative to
# the source file, so the main sits beside the generated modules.
.PHONY: happy-goose
happy-goose: bin/schema toolchain-goose
	@rm -rf build/happy/goose && mkdir -p build/happy/goose
	./bin/schema generate --lang goose --out build/happy/goose test/happy/Happy.schema
	cp test/happy/goose/main.goose build/happy/goose/main.goose
	$(GOOSE) run build/happy/goose/main.goose

# Java: javac with every lint as an error, then the run with asserts on.
.PHONY: happy-java
happy-java: bin/schema toolchain-java
	@rm -rf build/happy/java && mkdir -p build/happy/java
	./bin/schema generate --lang java --out build/happy/java test/happy/Happy.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/happy/java/classes \
		build/happy/java/Happy.java test/happy/java/Main.java
	$(JAVA) -ea -cp build/happy/java/classes Main

# JS: node on the generated ESM and the serialize.js sibling.
.PHONY: happy-js
happy-js: bin/schema
	@$(call toolchain_probe,js,NODE,$(NODE))
	@rm -rf build/happy/js && mkdir -p build/happy/js
	./bin/schema generate --lang js --out build/happy/js test/happy/Happy.schema
	$(NODE) test/happy/js/main.mjs

# Lobster: the main runs beside the generated modules.
.PHONY: happy-lobster
happy-lobster: bin/schema toolchain-lobster
	@rm -rf build/happy/lobster && mkdir -p build/happy/lobster
	./bin/schema generate --lang lobster --out build/happy/lobster test/happy/Happy.schema
	cp test/happy/lobster/main.lobster build/happy/lobster/main.lobster
	cd build/happy/lobster && $(LOBSTER) main.lobster

.PHONY: happy-lua
happy-lua: bin/schema toolchain-lua
	@rm -rf build/happy/lua && mkdir -p build/happy/lua
	./bin/schema generate --lang lua --out build/happy/lua test/happy/Happy.schema
	$(LUA) test/happy/lua/main.lua build/happy/lua

# Luau: requires are relative to the source file.
.PHONY: happy-luau
happy-luau: bin/schema toolchain-luau
	@rm -rf build/happy/luau && mkdir -p build/happy/luau
	./bin/schema generate --lang luau --out build/happy/luau test/happy/Happy.schema
	cp test/happy/luau/main.luau build/happy/luau/main.luau
	cd build/happy/luau && $(LUAU) main.luau

# moo: the compiler has no moo target yet, so the leg's own packet driver
# (make/moo-packet.mk) generates the modules, requires moo-analyze silent over
# them and the main, and runs the main.
.PHONY: happy-moo
happy-moo: toolchain-moo
	@rm -rf build/happy/moo && mkdir -p build/happy/moo
	go build -o build/happy/moo/driver ./test/packet-text/moo/driver
	./build/happy/moo/driver -analyze -keep build/happy/moo/gen -schema test/happy/Happy.schema -main test/happy/moo/main.moo

# Python: the interpreter alone (toolchain-python also stages mypy, nightly's).
.PHONY: happy-python
happy-python: bin/schema
	@$(call toolchain_probe,python,PYTHON,$(PYTHON))
	@rm -rf build/happy/python && mkdir -p build/happy/python
	./bin/schema generate --lang python --out build/happy/python test/happy/Happy.schema
	PYTHONPATH=build/happy/python $(PYTHON) -B test/happy/python/main.py

# Ruby: the interpreter alone (toolchain-ruby also probes the analyzers, nightly's).
.PHONY: happy-ruby
happy-ruby: bin/schema
	@$(call toolchain_probe,ruby,RUBY,$(RUBY))
	@rm -rf build/happy/ruby && mkdir -p build/happy/ruby
	./bin/schema generate --lang ruby --out build/happy/ruby test/happy/Happy.schema
	$(RUBY) --disable-gems -I build/happy/ruby test/happy/ruby/main.rb

# Rust: rustc directly, no cargo. The runtime, the generated crate and the
# driver are three debug units, and nothing is resolved or fetched.
HAPPY_RUSTC = PATH="$(RUSTUP_BIN):$$PATH" rustc --edition 2024 -D warnings --out-dir build/happy/rust

.PHONY: happy-rust
happy-rust: bin/schema
	@rm -rf build/happy/rust && mkdir -p build/happy/rust
	./bin/schema generate --lang rust --out build/happy/rust/src test/happy/Happy.schema
	$(HAPPY_RUSTC) --crate-type rlib --crate-name serialize $(SERIALIZE_RS)/src/lib.rs
	$(HAPPY_RUSTC) --crate-type rlib --crate-name happy -L build/happy/rust \
		--extern serialize=build/happy/rust/libserialize.rlib build/happy/rust/src/lib.rs
	$(HAPPY_RUSTC) --crate-type bin --crate-name happy_rust -L build/happy/rust \
		--extern serialize=build/happy/rust/libserialize.rlib \
		--extern happy=build/happy/rust/libhappy.rlib test/happy/rust/main.rs
	./build/happy/rust/happy_rust

# Squirrel: sq exits 0 on a runtime error, so the recipe also requires the
# program's ok line as the last line of its output.
HAPPY_SQUIRREL_LOG := build/happy/squirrel/run.log

.PHONY: happy-squirrel
happy-squirrel: bin/schema toolchain-squirrel
	@rm -rf build/happy/squirrel && mkdir -p build/happy/squirrel
	./bin/schema generate --lang squirrel --out build/happy/squirrel test/happy/Happy.schema
	"$(SQ)" test/happy/squirrel/main.nut internal/codegen/squirrel/serialize build/happy/squirrel > $(HAPPY_SQUIRREL_LOG) 2>&1 || { cat $(HAPPY_SQUIRREL_LOG); exit 1; }
	@cat $(HAPPY_SQUIRREL_LOG)
	@tail -n 1 $(HAPPY_SQUIRREL_LOG) | grep -E '^happy squirrel: ok \([0-9]+ bytes\)$$' >/dev/null || { echo "happy-squirrel: FAILED: no ok line"; exit 1; }

# Unity: the SDK's csc.dll builds the two assemblies test/unity/UnityDialect.props
# describes (serialize.cs as its own netstandard2.1 library, then the
# generated code with the host Main at C# 9 over netstandard2.1, warnings as
# errors, unsafe off), and the .NET host runs it. The .NET analyzers are the
# nightly Unity gates', not this path's.
.PHONY: happy-unity
happy-unity: bin/schema toolchain-unity
	@rm -rf build/happy/unity && mkdir -p build/happy/unity
	./bin/schema generate --lang unity --out build/happy/unity test/happy/Happy.schema
	@set -e; \
	root=$$(dirname "$$(readlink -f "$$(command -v $(DOTNET))")"); \
	sdk=$$($(DOTNET) --version); \
	ns=$$(ls "$$root"/packs/NETStandard.Library.Ref/2.1.*/ref/netstandard2.1/netstandard.dll | sort -V | tail -1); \
	[ -f "$$ns" ] || { echo "happy-unity: no netstandard2.1 reference pack under $$root/packs"; exit 1; }; \
	rt=$$(ls -d "$$root"/shared/Microsoft.NETCore.App/$${sdk%%.*}.* | sort -V | tail -1); \
	[ -d "$$rt" ] || { echo "happy-unity: no .NET $${sdk%%.*} runtime under $$root/shared"; exit 1; }; \
	csc="$(DOTNET) exec $$root/sdk/$$sdk/Roslyn/bincore/csc.dll -nologo -noconfig -langversion:9 -optimize- -define:DEBUG -warnaserror -unsafe- -r:$$ns"; \
	echo "csc ($$sdk, netstandard2.1, C# 9) -> build/happy/unity/Serialize.dll happy.dll"; \
	$$csc -target:library -nullable:enable -out:build/happy/unity/Serialize.dll \
		$(SERIALIZE_CS)/src/Serialize.cs $(SERIALIZE_CS)/src/Int128Pair.cs; \
	$$csc -target:exe -nullable:disable -r:build/happy/unity/Serialize.dll -out:build/happy/unity/happy.dll \
		build/happy/unity/Happy.cs test/happy/unity/Main.cs; \
	printf '{"runtimeOptions":{"tfm":"net%s.0","framework":{"name":"Microsoft.NETCore.App","version":"%s"}}}\n' \
		"$${sdk%%.*}" "$$(basename "$$rt")" > build/happy/unity/happy.runtimeconfig.json
	$(DOTNET) exec build/happy/unity/happy.dll
