# SPEC §4.8: selecting a packet union arm constructs its payload, including
# the defaults in unused backing capacity. The existing packet harnesses read
# the independent 7-bit 0x51 vector twice, poisoning the selected arm before
# each read. A zero-only reader must fail their selected-entry assertion.
#
# Each control overlays one COPY of an emitter, regenerates into its own
# miniature checkout under build/, and builds the unchanged packet harness
# there. Imports, manifests and golden paths retain their ordinary layout.
# No tracked source or generated output is rewritten. A compiler error does
# not satisfy a control: the runtime must fail AND print its semantic assertion.
# Elixir stores only the transmitted list entries, so it has no changed
# unused-capacity behavior to sabotage.

ARM_NC_ROOT := build/packet-arm-nc
ARM_NC_BACKING_FAILURE := FAILED: DefaultChoice reconstructs both backing entries, including repeated tags
ARM_NC_MANAGED_FAILURE := FAILED: selected unused backing entry receives its defaults
ARM_NC_LEGS := cpp c go rust cs java dart js-runtime js-flat
ARM_NC_TARGETS := $(addprefix packet-arm-defaults-,$(addsuffix -negative-control,$(ARM_NC_LEGS)))

# $(1) control suffix, $(2) emitter source, $(3) generator language,
# $(4) generated subdirectory (Rust's crate needs the additional src/).
define packet_arm_prepare
	@rm -rf $(ARM_NC_ROOT)/$(1)
	@mkdir -p $(ARM_NC_ROOT)/$(1)/schema/test $(ARM_NC_ROOT)/$(1)/schema/bin
	@go run ./tools/sabotage -name packet-arm-zero-$(1) \
		-out $(ARM_NC_ROOT)/$(1)/emitter.gotext $(2)
	@printf '{"Replace":{"%s/$(2)":"%s/$(ARM_NC_ROOT)/$(1)/emitter.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(ARM_NC_ROOT)/$(1)/overlay.json
	@go build -overlay=$(ARM_NC_ROOT)/$(1)/overlay.json -o $(ARM_NC_ROOT)/$(1)/compiler ./cmd/schema
	@./$(ARM_NC_ROOT)/$(1)/compiler generate --lang $(3) \
		--out $(ARM_NC_ROOT)/$(1)/schema/generated/$(4) examples
	@ln -s "$(CURDIR)/testdata" $(ARM_NC_ROOT)/$(1)/schema/testdata
endef

# Managed packet harnesses also import the existing benchmark corpus.
# $(1) control suffix, $(2) generator language.
define packet_arm_bench
	@./$(ARM_NC_ROOT)/$(1)/compiler generate --lang $(2) \
		--out $(ARM_NC_ROOT)/$(1)/schema/generated/bench/$(2) bench/corpus/Bench.schema
	@./$(ARM_NC_ROOT)/$(1)/compiler generate --lang $(2) \
		--out $(ARM_NC_ROOT)/$(1)/schema/generated/bench/$(2)/realworld bench/corpus/RealWorld.schema
endef

# $(1) control suffix, $(2) runtime invocation, $(3) exact semantic failure.
# Compilation is a preceding recipe command and must have succeeded already.
# The C++ pinner checks updater-variable presence, so even a value of 0 writes.
# Remove the flag for every copied harness before it can reach fixture data.
define packet_arm_expect
	@if ( unset SCHEMA_UPDATE_WIRE_GOLDENS && $(2) ) > $(ARM_NC_ROOT)/$(1)/runtime.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(1) packet arms started at zero and the harness stayed green"; \
		cat $(ARM_NC_ROOT)/$(1)/runtime.log; exit 1; \
	fi
	@grep -Fq '$(3)' $(ARM_NC_ROOT)/$(1)/runtime.log || \
		{ echo "NEGATIVE CONTROL FAILED: $(1) failed without the selected-arm default assertion"; \
		  cat $(ARM_NC_ROOT)/$(1)/runtime.log; exit 1; }
	@echo "negative control (packet-arm-zero-$(1)): selected-arm defaults go RED"
	@grep -Fm1 '$(3)' $(ARM_NC_ROOT)/$(1)/runtime.log
endef

.PHONY: $(ARM_NC_TARGETS) packet-arm-defaults-negative-controls

packet-arm-defaults-cpp-negative-control:
	$(call packet_arm_prepare,cpp,internal/codegen/cpp/functions.go,cpp,cpp)
	@cp test/main.cpp test/second.cpp test/vec_math.h $(ARM_NC_ROOT)/cpp/schema/test/
	$(CXX) $(CXXFLAGS) -I$(ARM_NC_ROOT)/cpp/schema/generated/cpp \
		-I$(ARM_NC_ROOT)/cpp/schema/test $(ARM_NC_ROOT)/cpp/schema/test/main.cpp \
		$(ARM_NC_ROOT)/cpp/schema/test/second.cpp -o $(ARM_NC_ROOT)/cpp/schema/bin/packet-test
	$(call packet_arm_expect,cpp,cd $(ARM_NC_ROOT)/cpp/schema && ./bin/packet-test,FAILED: selected_arm_defaults( out.first ))

packet-arm-defaults-c-negative-control:
	$(call packet_arm_prepare,c,internal/codegen/c/c.go,c,c)
	@mkdir -p $(ARM_NC_ROOT)/c/schema/test/c
	@cp test/c/main.c $(ARM_NC_ROOT)/c/schema/test/c/
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -I$(ARM_NC_ROOT)/c/schema/generated/c -I$(SERIALIZE_C) \
		$(ARM_NC_ROOT)/c/schema/test/c/main.c $(SERIALIZE_C)/serialize.c \
		-o $(ARM_NC_ROOT)/c/schema/bin/packet-test -lm
	$(call packet_arm_expect,c,cd $(ARM_NC_ROOT)/c/schema/test/c && ../../bin/packet-test,$(ARM_NC_BACKING_FAILURE))

packet-arm-defaults-go-negative-control:
	$(call packet_arm_prepare,go,internal/codegen/golang/functions.go,go,go)
	@mkdir -p $(ARM_NC_ROOT)/go/schema/test/go
	@cp test/go/main.go test/go/go.mod $(ARM_NC_ROOT)/go/schema/test/go/
	@ln -s "$(abspath $(SERIALIZE_GO))" $(ARM_NC_ROOT)/go/serialize.go
	@printf 'module example\n\ngo 1.23\n\nrequire github.com/mas-bandwidth/serialize.go v0.0.0\n\nreplace github.com/mas-bandwidth/serialize.go => "%s"\n' \
		"$(abspath $(SERIALIZE_GO))" > $(ARM_NC_ROOT)/go/schema/generated/go/go.mod
	cd $(ARM_NC_ROOT)/go/schema/test/go && go build -o ../../bin/packet-test .
	$(call packet_arm_expect,go,cd $(ARM_NC_ROOT)/go/schema/test/go && ../../bin/packet-test,$(ARM_NC_BACKING_FAILURE))

packet-arm-defaults-rust-negative-control:
	$(call packet_arm_prepare,rust,internal/codegen/rust/functions.go,rust,rust/src)
	@mkdir -p $(ARM_NC_ROOT)/rust/schema/test/rust/src
	@cp test/rust/Cargo.toml $(ARM_NC_ROOT)/rust/schema/test/rust/
	@cp test/rust/src/main.rs $(ARM_NC_ROOT)/rust/schema/test/rust/src/
	@ln -s "$(abspath $(SERIALIZE_RS))" $(ARM_NC_ROOT)/rust/serialize.rs
	@printf '[package]\nname = "example"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "../../../serialize.rs" }\n' \
		> $(ARM_NC_ROOT)/rust/schema/generated/rust/Cargo.toml
	cd $(ARM_NC_ROOT)/rust/schema/test/rust && PATH="$(RUSTUP_BIN):$$PATH" \
		cargo build --quiet --target-dir ../../bin/rust
	$(call packet_arm_expect,rust,cd $(ARM_NC_ROOT)/rust/schema/test/rust && ../../bin/rust/debug/schematest,$(ARM_NC_BACKING_FAILURE))

packet-arm-defaults-cs-negative-control:
	$(call packet_arm_prepare,cs,internal/codegen/csharp/functions.go,cs,cs)
	$(call packet_arm_bench,cs,cs)
	@mkdir -p $(ARM_NC_ROOT)/cs/schema/test/cs/src
	@cp test/cs/schematest.csproj $(ARM_NC_ROOT)/cs/schema/test/cs/
	@cp test/cs/src/Program.cs $(ARM_NC_ROOT)/cs/schema/test/cs/src/
	@ln -s "$(abspath $(SERIALIZE_CS))" $(ARM_NC_ROOT)/cs/serialize.cs
	cd $(ARM_NC_ROOT)/cs/schema/test/cs && dotnet build --nologo -v quiet -o ../../bin/cs
	$(call packet_arm_expect,cs,cd $(ARM_NC_ROOT)/cs/schema/test/cs && dotnet ../../bin/cs/schematest.dll,$(ARM_NC_MANAGED_FAILURE))

packet-arm-defaults-java-negative-control:
	$(call packet_arm_prepare,java,internal/codegen/java/functions.go,java,java)
	$(call packet_arm_bench,java,java)
	@mkdir -p $(ARM_NC_ROOT)/java/schema/test/java $(ARM_NC_ROOT)/java/schema/bin/java
	@cp test/java/Main.java $(ARM_NC_ROOT)/java/schema/test/java/
	$(JAVAC) --release 17 -Xlint:all -Werror -d $(ARM_NC_ROOT)/java/schema/bin/java \
		$(ARM_NC_ROOT)/java/schema/generated/java/*.java \
		$(ARM_NC_ROOT)/java/schema/generated/bench/java/*.java \
		$(ARM_NC_ROOT)/java/schema/generated/bench/java/realworld/*.java \
		$(ARM_NC_ROOT)/java/schema/test/java/Main.java
	$(call packet_arm_expect,java,cd $(ARM_NC_ROOT)/java/schema/test/java && $(JAVA) -ea -cp ../../bin/java Main,$(ARM_NC_MANAGED_FAILURE))

packet-arm-defaults-dart-negative-control:
	$(call packet_arm_prepare,dart,internal/codegen/dart/functions.go,dart,dart)
	$(call packet_arm_bench,dart,dart)
	@mkdir -p $(ARM_NC_ROOT)/dart/schema/test/dart
	@cp test/dart/main.dart $(ARM_NC_ROOT)/dart/schema/test/dart/
	cd $(ARM_NC_ROOT)/dart/schema/test/dart && $(DART) compile exe -o ../../bin/packet-test main.dart
	$(call packet_arm_expect,dart,cd $(ARM_NC_ROOT)/dart/schema/test/dart && ../../bin/packet-test,$(ARM_NC_MANAGED_FAILURE))

# Runtime and flat selection are sabotaged independently: each copy imports
# both tiers, and must fail the assertion belonging to the altered tier.
define packet_arm_js
	$(call packet_arm_prepare,js-$(1),internal/codegen/js/$(2),js,js)
	$(call packet_arm_bench,js-$(1),js)
	@mkdir -p $(ARM_NC_ROOT)/js-$(1)/schema/test/js
	@cp test/js/main.mjs $(ARM_NC_ROOT)/js-$(1)/schema/test/js/
	@ln -s "$(abspath ../serialize.js)" $(ARM_NC_ROOT)/js-$(1)/serialize.js
	@printf '{"type":"module"}\n' > $(ARM_NC_ROOT)/js-$(1)/schema/package.json
	$(call packet_arm_expect,js-$(1),cd $(ARM_NC_ROOT)/js-$(1)/schema/test/js && $(NODE) main.mjs,FAILED: $(1) default arm restores both unused entries on every selection 0)
endef

packet-arm-defaults-js-runtime-negative-control:
	$(call packet_arm_js,runtime,functions.go)

packet-arm-defaults-js-flat-negative-control:
	$(call packet_arm_js,flat,flat.go)

# The standalone group follows the normal test driver's sequential leg loop.
packet-arm-defaults-negative-controls:
	@set -e; for target in $(ARM_NC_TARGETS); do $(MAKE) $$target; done

# Additional prerequisites preserve each existing test recipe. The root test
# already invokes the registered language legs; no duplicate registration.
test: packet-arm-defaults-cpp-negative-control
test-c: packet-arm-defaults-c-negative-control
test-go: packet-arm-defaults-go-negative-control
test-rust: packet-arm-defaults-rust-negative-control
test-cs: packet-arm-defaults-cs-negative-control
test-java: packet-arm-defaults-java-negative-control
test-dart: packet-arm-defaults-dart-negative-control
test-js: packet-arm-defaults-js-runtime-negative-control packet-arm-defaults-js-flat-negative-control
