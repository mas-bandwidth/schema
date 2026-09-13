# make/java.mk — the Java leg (docs/CONTRIBUTING.md, "Adding a language").
# Included by the Makefile's wildcard include; the Makefile names no language.
# The leg registers itself at the end of this file.

# The JDK, pinned per project (generated Java is self-contained — no runtime
# checkout — so the JDK is the only Java dependency; same pin as the
# serialize.java port). The defaults point at the repo-local unpacked JDK;
# CI installs the same major and overrides with JAVA=java JAVAC=javac. To
# populate dist/ (gitignored):
#   Temurin JDK 21.0.12.1+1 (Eclipse Adoptium, macos aarch64)
#   url:    https://github.com/adoptium/temurin21-binaries/releases/download/jdk-21.0.12.1%2B1/OpenJDK21U-jdk_aarch64_mac_hotspot_21.0.12.1_1.tar.gz
#   sha256: 3623232f33a9c3baadf304480b2535f9a3cba8a58d42ecbb438ba267315d9998
#   untar into dist/ and rename to dist/jdk-21.0.12.1
# Generated code and the test legs compile with --release 17.
JAVA  ?= $(CURDIR)/dist/jdk-21.0.12.1/Contents/Home/bin/java
JAVAC ?= $(CURDIR)/dist/jdk-21.0.12.1/Contents/Home/bin/javac

# THE TOOLCHAIN GATE, this leg's half (issue #599; the Makefile's header and
# docs/CONTRIBUTING.md, "Adding a language"). BOTH pins are probed: a JDK whose
# java runs and whose javac does not is a leg that compiles nothing and says so
# forty minutes in.
.PHONY: toolchain-java
toolchain-java:
	@$(call toolchain_probe,java,JAVA,$(JAVA))
	@$(call toolchain_probe,java,JAVAC,$(JAVAC))
build/packet-defaults/java/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/java.mk
	./bin/schema generate --lang java --out build/packet-defaults/java/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang java --out build/packet-defaults/java/plain test/packet-defaults/Plain.schema
	@touch $@

.PHONY: packet-defaults-java packet-defaults-java-negative-control
packet-defaults-java: build/packet-defaults/java/.stamp packet-defaults-cpp
	@mkdir -p build/packet-defaults/java/classes
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-defaults/java/classes build/packet-defaults/java/defaults/*.java build/packet-defaults/java/plain/*.java test/packet-defaults/java/Main.java
	$(JAVA) -ea -cp build/packet-defaults/java/classes Main testdata/wire/packet-defaults
	$(JAVA) -cp build/packet-defaults/java/classes Main testdata/wire/packet-defaults

packet-defaults-java-negative-control: packet-defaults-java
	@mkdir -p build/packet-defaults/java-negative/classes
	go run ./tools/sabotage -name packet-defaults-java-constructor-bytes \
		-out build/packet-defaults/java-negative/java.gotext internal/codegen/java/java.go
	@printf '{"Replace":{"%s/internal/codegen/java/java.go":"%s/build/packet-defaults/java-negative/java.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/java-negative/overlay.json
	go build -overlay=build/packet-defaults/java-negative/overlay.json -o build/packet-defaults/java-negative/schema ./cmd/schema
	./build/packet-defaults/java-negative/schema generate --lang java --out build/packet-defaults/java-negative/generated test/packet-defaults/Defaults.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-defaults/java-negative/classes build/packet-defaults/java-negative/generated/*.java build/packet-defaults/java/plain/*.java test/packet-defaults/java/Main.java
	@if $(JAVA) -cp build/packet-defaults/java-negative/classes Main testdata/wire/packet-defaults > build/packet-defaults/java-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in Java'; exit 1; fi
	@grep -Fq 'packet-default constructor bytes' build/packet-defaults/java-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Java failed for another reason'; cat build/packet-defaults/java-negative/log; exit 1; }
	@echo 'packet defaults Java negative control: missing constructor bytes fail the runtime check'

test-java: packet-defaults-java packet-defaults-java-negative-control

# the Java target: generated classes only, no wiring file at all — generated
# Java is self-contained (the bitpacker is inlined per issue #156), so there
# is no runtime checkout and no build file; the test legs compile the
# generated sources beside their Main.java directly
generated/java/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang java --out generated/java examples
	@touch $@

generated/java-ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang java --out generated/java-ludicrous examples128
	@touch $@

# ---------------------------------------------------------------------------
# The JAVA table backend (internal/codegen/javatable, docs/SPEC-TABLES.md). The
# two ACCELERATORS' read halves — the block form (§19) and the cook (§7), their
# record accessors and reflection descriptors — and the build-version check.
# Java emits no table wire: the port that wrote the wire's previous form was
# removed (schema#517 brings the id-table form). The C++ backend is the
# reference and the C# one is the worked managed-language port; this leg
# mirrors both.
#
# Java's unit scope is the PACKAGE and a public type lives in a file of its own
# name, so the shared runtime is one file per type rather than one home file —
# which is what makes "where does the runtime live" a question with no rule to
# state and no file order to depend on.
# ---------------------------------------------------------------------------

# The same corpus through the Java table backend: the tables corpus plus the
# evolution pair, generated at build time into build/ — test-only, never part of
# the committed generated/ tree. The full unit is generated (packet .java +
# <Table>Block.java + <Table>Cook.java + the Row accessors and the runtime
# types), because a record's descriptors name the packet emitter's own enums.
build/tables-generated-java/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema test/tables/FX1.schema test/tables/FX2.schema test/tables/UT.schema test/tables/FU1.schema test/tables/FU2.schema $(SCHEMAS_TABLES_SCALARS)
	@mkdir -p build/tables-generated-java
	./bin/schema generate --lang java --out build/tables-generated-java/examples tables/examples
	# the POINTERED unit: its cook readers are the reason it is here — the two
	# ACCELERATORS need no codec (§7, §19), so a pointered unit's cooks open.
	./bin/schema generate --lang java --out build/tables-generated-java/pointers tables/pointers
	./bin/schema generate --lang java --out build/tables-generated-java/block tables/block
	./bin/schema generate --lang java --out build/tables-generated-java/blockhome tables/blockhome
	./bin/schema generate --lang java --out build/tables-generated-java/v1 test/tables/V1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/v2 test/tables/V2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/p1 test/tables/P1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/p3 test/tables/P3.schema
	# THE FIXED FORM's versioning pair (docs/SPEC-TABLES.md §3.4): FX1 is the
	# old side and FX2 the new, and every edit between them lands through a
	# plan compiled from the other side's layout.
	./bin/schema generate --lang java --out build/tables-generated-java/fx1 test/tables/FX1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/fx2 test/tables/FX2.schema
	# TEXT AND AN ENUM UNDER A UNION'S SECOND ARM: the shape the compiled plan
	# had no fixture for, and the one that catches an op borrowing the guard's
	# own byte (test/tables/UT.schema).
	./bin/schema generate --lang java --out build/tables-generated-java/ut test/tables/UT.schema
	# FU1/FU2 is the TEXT-UNDER-AN-ARM pair whose compiled path is a TRAILING
	# FIELD, not a slid ordinal: FU1's second arm carries a string(8), FU2 appends
	# `extra` so a read of FU1's bytes is a compiled plan, and the two reads have
	# to agree on the text (reference-fix 12). C++ already generates the pair;
	# this stamp did not.
	./bin/schema generate --lang java --out build/tables-generated-java/fu1 test/tables/FU1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/fu2 test/tables/FU2.schema
	# THE WIDE-KIND UNIT (docs/SPEC-TABLES.md §15): its int128, uint128 and
	# fixed-point fields cost it the two ACCELERATORS, so the FIXED FORM is the
	# whole of its generated table surface — and that form spells a 128-bit
	# value `new UInt128(hi, lo)`, so the unit is also where the emitter's
	# Int128/UInt128 support files have to land or the package does not compile.
	# The C, C# and Go legs have generated this unit all along; the Java leg is
	# the one that did not, which is why nothing here said so.
	./bin/schema generate --lang java --out build/tables-generated-java/scalars tables/scalars
	@touch $@

# The Java twin of the C++ "no serialize include path" build: a generated
# <Table>Block.java or <Table>Cook.java must stand alone on the JDK, so nothing
# in it may name the serialize runtime — and nothing may name a THIRD-PARTY JSON
# library either: the readers are this backend's own, over the descriptors.
.PHONY: tables-java-standalone
tables-java-standalone: build/tables-generated-java/.stamp
	@n=$$(ls build/tables-generated-java/*/*Block.java build/tables-generated-java/*/*Cook.java 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 12 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated Block and Cook sources, expected at least 12 — the glob, not the property, is what broke"; exit 1; \
		fi
	@for f in build/tables-generated-java/*/*Block.java build/tables-generated-java/*/*Cook.java build/tables-generated-java/*/*Row.java; do \
		[ -e "$$f" ] || continue; \
		if grep -n "Serialize\|com\.fasterxml\|com\.google\.gson\|org\.json" $$f; then \
			echo "STANDALONE GATE FAILED: a runtime dependency leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables Java standalone gate: generated Block, Cook and Row sources name no runtime and no JSON library"

# THE ZERO-COST GATE, Java side (docs/SPEC-TABLES.md §2.2): a unit that declares
# no table emits not one byte of table code — no runtime type, no Row, no
# Block, no Cook, no BuildVersion — so a consumer that never wrote `table` pays
# nothing for the form existing.
.PHONY: tables-java-zero-cost
tables-java-zero-cost: bin/schema
	@rm -rf build/tables-java-zero && mkdir -p build
	./bin/schema generate --lang java --out build/tables-java-zero examples
	@for f in build/tables-java-zero/*.java; do \
		case $$(basename $$f) in \
			Table*|*Table.java|*Row.java|*Block.java|*Cook.java|BuildVersion.java) \
				echo "ZERO-COST GATE FAILED: a table-free unit emitted $$f"; exit 1;; \
		esac; \
	done
	@cmp -s build/tables-java-zero/Types.java generated/java/Types.java || \
		{ echo "ZERO-COST GATE FAILED: a table-free unit's packet output is not the committed one"; exit 1; }
	@echo "tables Java zero-cost gate: a table-free unit emits no table code at all"

# EVERY generated unit compiles, warnings as errors, under the CONSUMER's javac
# — the same flags the type wire's Java legs use, so a warning here is a build
# failure in a consumer's tree and not only in ours.
#
# IT COMPILES THE UNITS THE CONFORMANCE LEG DOES NOT, and no others. That leg's
# classpath already carries seven of the eight under these very flags
# (build-conformance-java), so compiling them twice buys nothing and costs the
# `make test` budget eleven seconds. `blockhome` is the one unit no other target
# touches — a unit whose protocol id lives in a table-free file — so it is the
# one this gate exists for. TABLES_JAVA_UNITS names it, so the day another unit
# leaves the conformance classpath it is added here rather than going unbuilt.
TABLES_JAVA_UNITS ?= blockhome
.PHONY: tables-java-compile
tables-java-compile: build/tables-generated-java/.stamp
	@rm -rf build/tables-java-classes
	@for unit in $(TABLES_JAVA_UNITS); do \
		mkdir -p build/tables-java-classes/$$unit; \
		$(JAVAC) --release 17 -Xlint:all -Werror -d build/tables-java-classes/$$unit \
			build/tables-generated-java/$$unit/*.java || \
			{ echo "JAVA COMPILE GATE FAILED: unit $$unit"; exit 1; }; \
	done
	@echo "tables Java compile gate: $(TABLES_JAVA_UNITS) compiles under -Xlint:all -Werror (the conformance leg builds the rest)"

# and the WHOLE corpus, every unit, for a release pass or a hand check — the
# gate above's superset, run by name.
.PHONY: tables-java-compile-all
tables-java-compile-all: build/tables-generated-java/.stamp
	@rm -rf build/tables-java-classes-all
	@for d in build/tables-generated-java/*/; do \
		unit=$$(basename $$d); \
		mkdir -p build/tables-java-classes-all/$$unit; \
		$(JAVAC) --release 17 -Xlint:all -Werror -d build/tables-java-classes-all/$$unit $$d*.java || \
			{ echo "JAVA COMPILE GATE FAILED: unit $$unit"; exit 1; }; \
	done
	@echo "tables Java compile gate: every generated unit compiles under -Xlint:all -Werror"

# THE JAVA TABLES LEG (test/java-tables/src/Main.java): the gates the
# conformance harness does not hold, because none of them is a case — the
# readers' fuzz oracle, the reference extent gate and the byte-order leg.
build/java-tables/.stamp: build/tables-generated-java/.stamp test/java-tables/src/Main.java
	@rm -rf build/java-tables && mkdir -p build/java-tables
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-tables \
		build/tables-generated-java/examples/*.java build/tables-generated-java/pointers/*.java \
		build/tables-generated-java/block/*.java test/java-tables/src/Main.java
	@touch $@

# THE READERS' ORACLE (docs/SPEC-TABLES.md §7.5, §19.2). Mutants of a block image
# and a cooked file go to the generated Open, and the answer must be a REFUSAL or
# a read that stays inside the array it was given. The C++ leg holds the same
# claim with ASan's redzone; Java's instrument is the language — an index out of
# bounds throws, and an exception escaping into a caller that asked a question is
# what this refuses.
.PHONY: tables-java-fuzz
tables-java-fuzz: build/java-tables/.stamp build/cook-open/.stamp
	$(JAVA) -cp build/java-tables Main fuzz testdata/wire/tables/block_render.bin build/cook-open/Scene.cook

# ITS NEGATIVE CONTROL, and it removes CHECKS rather than moving a value: the
# block Open's bound on an array's rows against the extent the caller passed,
# AND the used-extent bound behind it. BOTH, and the reason is the finding the
# first attempt at this control turned up — removing the rows bound alone leaves
# the fuzz GREEN, because the padding check downstream computes `bytes - used`
# and refuses on the negative slack. The checks are layered, which is the
# reader's own property and worth having written down; the control has to reach
# past the layer to make the escape the oracle exists to catch.
JAVA_FUZZ_SABOTAGE := build/java-fuzz-sabotage
JAVA_FUZZ_SABOTAGE_SED := s|if (rows > bytes - offsetOf) { return null; }|if (rows > bytes - offsetOf) { } // SABOTAGED|
JAVA_FUZZ_SABOTAGE_SED2 := s|if (padding > bytes - used) { return null; }|if (padding > bytes - used) { } // SABOTAGED|
.PHONY: tables-java-fuzz-negative-control
tables-java-fuzz-negative-control: build/cook-open/.stamp
	@rm -rf $(JAVA_FUZZ_SABOTAGE) && mkdir -p $(JAVA_FUZZ_SABOTAGE)
	@sed -e '$(JAVA_FUZZ_SABOTAGE_SED)' -e '$(JAVA_FUZZ_SABOTAGE_SED2)' internal/codegen/javatable/block.go > $(JAVA_FUZZ_SABOTAGE)/javatable-block.go.txt
	@cmp -s internal/codegen/javatable/block.go $(JAVA_FUZZ_SABOTAGE)/javatable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Java block sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/javatable/block.go":"%s/$(JAVA_FUZZ_SABOTAGE)/javatable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_FUZZ_SABOTAGE)/overlay.json
	go build -overlay $(JAVA_FUZZ_SABOTAGE)/overlay.json -o $(JAVA_FUZZ_SABOTAGE)/schema ./cmd/schema
	$(JAVA_FUZZ_SABOTAGE)/schema generate --lang java --out $(JAVA_FUZZ_SABOTAGE)/generated/examples tables/examples
	$(JAVA_FUZZ_SABOTAGE)/schema generate --lang java --out $(JAVA_FUZZ_SABOTAGE)/generated/pointers tables/pointers
	$(JAVA_FUZZ_SABOTAGE)/schema generate --lang java --out $(JAVA_FUZZ_SABOTAGE)/generated/block tables/block
	@grep -lq SABOTAGED $(JAVA_FUZZ_SABOTAGE)/generated/block/*Block.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged Open"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_FUZZ_SABOTAGE)/classes \
		$(JAVA_FUZZ_SABOTAGE)/generated/*/*.java test/java-tables/src/Main.java
	@if $(JAVA) -cp $(JAVA_FUZZ_SABOTAGE)/classes Main fuzz testdata/wire/tables/block_render.bin \
			build/cook-open/Scene.cook > $(JAVA_FUZZ_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a block reader with no extent bounds left the fuzz green"; \
		cat $(JAVA_FUZZ_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "escaped an exception rather than refusing" $(JAVA_FUZZ_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fuzz went red, but not on the oracle"; \
		  cat $(JAVA_FUZZ_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_FUZZ_SABOTAGE)/log
	@echo "negative control: the Java block Open with its two extent bounds removed turns the fuzz oracle RED"

# ---------------------------------------------------------------------------
# THE FIXED FORM, form byte 3 (docs/SPEC-TABLES.md §3.4)
# ---------------------------------------------------------------------------
#
# THE BYTES ARE THE C++ REFERENCE'S, and that is the whole of this gate. The
# reference writes the corpus (`make tables-fixedform-corpus`, one form-3 FILE
# per root) and this leg reads every one of them, checks the VALUES it decoded,
# saves them back, and the bytes must be IDENTICAL. A byte this port encodes
# differently is a byte that does not come back.
#
# Beside it rides the VERSIONING CONFORMANCE — the FX1/FX2 pair, the V1/V2
# slide and P1 against P3, and FU1/FU2 (text under an arm: FU2 appends `extra`
# so a read of FU1's bytes is a compiled plan) — the cases the C++ and C legs
# run — and the negative controls, of which the one §3.4 names is a reader
# given the WRONG PLAN for a record, which must come out wrong, and one
# CORRUPTED-LAYOUT case per named rule a reader holds an untrusted peer's
# layout to.
build/java-fixedform/.stamp: build/tables-generated-java/.stamp test/java-fixedform/src/Main.java
	@rm -rf build/java-fixedform && mkdir -p build/java-fixedform
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-fixedform \
		build/tables-generated-java/fx1/*.java build/tables-generated-java/fx2/*.java \
		build/tables-generated-java/p1/*.java build/tables-generated-java/p3/*.java \
		build/tables-generated-java/v1/*.java build/tables-generated-java/v2/*.java \
		build/tables-generated-java/ut/*.java \
		build/tables-generated-java/fu1/*.java build/tables-generated-java/fu2/*.java \
		build/tables-generated-java/scalars/*.java \
		build/tables-generated-java/examples/*.java test/java-fixedform/src/Main.java
	@touch $@

# BOTH ASSERTION MODES, and the reason is this form's own: the generated
# writer's caller contracts are DEBUG ONLY, so -ea proves they fire and the
# default proves they are gone and the bytes are still the reference's.
.PHONY: tables-java-fixedform
tables-java-fixedform: build/java-fixedform/.stamp build/fixedform-corpus/.stamp
	$(JAVA) -ea -cp build/java-fixedform Main build/fixedform-corpus
	$(JAVA)     -cp build/java-fixedform Main build/fixedform-corpus

# THE VERSIONING HALF OF THE FIXED FORM ON THE JAVA LEG (§5 of
# docs/FIXED-FORM-ALGORITHM.md, the rows of docs/FIXED-FORM-VERSIONING-TESTS.md).
# internal/codegen/javatable/fixedversioning_test.go reads the C++ reference's
# byte oracle out of build/fixedform-corpus — `old_<row>.bin`, `new_<row>.bin`
# and the floor, hash and lineage-merge files — generates ONE JAVA PROBE PER ROW
# PER COLUMN (§5.9 #18) beside this leg's own generator, compiles it with this
# leg's `javac` and runs it: NEW-READS-OLD lands every old value with §5.4's
# counters, and OLD-REFUSES-NEW answers `layout_newer` before any record, on the
# file's hash alone (§5.3).
#
# THIS TARGET EXISTS BECAUSE `go test ./...` ASSERTS NOTHING HERE. The harness
# SKIPS itself when build/fixedform-corpus is absent, which is right for a bare
# `go test ./...` on a tree that never built the oracle — and is exactly how a §5
# regression rides into a green CI, since the go-test job runs nothing but
# `go test ./...`. So the target BUILDS THE ORACLE FIRST (the reference's own
# dump) and then sets SCHEMA_REQUIRE_CORPUS=1, which turns that skip into a
# FAILURE: under this name a missing corpus can never pass silently. It is the
# Java twin of `tables-go-versioning` (make/go.mk) and it is hung off the same
# job, for the same reason.
.PHONY: tables-java-versioning
tables-java-versioning: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -run 'TestFixedVersioning'
	@echo 'tables Java versioning: §5 read both columns of every row against the C++ reference bytes'

# ITS NEGATIVE CONTROL, and it is the gate's first: until this target existed
# `make tables-java-versioning` could have been asserting nothing and reporting
# green, which is the failure the Go and Rust legs' own controls are for.
#
# WHAT IT SABOTAGES is §5.3's one verdict: a file whose hash matches no entry of
# the lock's lineage is refused BY THE NAME `layoutNewer`, carrying the hash, and
# that name is the entire answer — it is what tells an operator to ship a newer
# reader instead of suspecting the bytes. The overlay puts `noLayout` there, the
# name for a file with no layout at all. Every counter still reads refused, so
# only a suite that checks the NAME can see it, and this control requires the
# suite to go red and to say which name it wanted.
#
# IT REPLACES THE RETIRED `plan_too_large` CONTROL. That one watched the
# grow-and-retry cap in lineagePlans, which is gone: the plan is sized from the
# lock's own entry counts by a stated formula (§5.9 #21), so there is no cap to
# red any more, and the control moved here with its case.
JAVA_VERSIONING_SABOTAGE := build/java-versioning-sabotage
.PHONY: tables-java-versioning-negative-control
tables-java-versioning-negative-control: tables-fixedform-corpus
	@rm -rf $(JAVA_VERSIONING_SABOTAGE) && mkdir -p $(JAVA_VERSIONING_SABOTAGE)
	go run ./tools/sabotage -name fixed-form-java-refusal-name \
		-out $(JAVA_VERSIONING_SABOTAGE)/fixedform.gotext internal/codegen/javatable/fixedform.go
	@grep -q SABOTAGED $(JAVA_VERSIONING_SABOTAGE)/fixedform.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/javatable/fixedform.go":"%s/$(JAVA_VERSIONING_SABOTAGE)/fixedform.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_VERSIONING_SABOTAGE)/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
			go test -overlay=$(JAVA_VERSIONING_SABOTAGE)/overlay.json \
			./internal/codegen/javatable/ -count=1 -run 'TestFixedVersioning' \
			> $(JAVA_VERSIONING_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the wrong name for a file outside the lineage left §5 green"; \
		tail -20 $(JAVA_VERSIONING_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "want a refusal named layoutNewer" $(JAVA_VERSIONING_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: §5 went red, but not on the refusal name this control is for"; \
		  tail -20 $(JAVA_VERSIONING_SABOTAGE)/log; exit 1; }
	@grep -m1 "want a refusal named layoutNewer" $(JAVA_VERSIONING_SABOTAGE)/log
	@echo 'negative control: the wrong refusal name for a file outside the lineage reds the Java versioning gate'

# THE PAIRED BENCH CORPUS on this form: the same sixty-four logical records the
# matched bench uses, written by the C++ reference with its form-3 writer and
# stated beside as a VALUE ORACLE, because a reader and a writer that share one
# offset mistake round trip perfectly and are both wrong. bench/paired has no
# Java half to run the matched gate through, so this is that gate's shape for
# this port: the reference is the oracle, at gate time, over the same corpus.
# It is also where the WIDE KINDS ride — a `fixed(24, 8)`, a `ufixed(8, 8)`, a
# bare `uint128` and a ranged `int128` — which no other fixed-form corpus has.
build/java-fixed-corpus/.stamp: generated/bench/paired/cpp/.stamp test/bench/fixedform_corpus.cpp bench/corpus/variants/bench_mixed.variants.bin
	@mkdir -p build/java-fixed-corpus
	$(CXX) $(CXXFLAGS) -Igenerated/bench/paired/cpp test/bench/fixedform_corpus.cpp -o build/java-fixed-corpus/corpus
	./build/java-fixed-corpus/corpus bench/corpus/variants/bench_mixed.variants.bin \
		build/java-fixed-corpus/bench_fixed.bin build/java-fixed-corpus/bench_fixed.oracle.json
	@touch $@

build/java-fixed-bench/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema test/java-fixedform/src/BenchFixed.java make/java.mk
	@rm -rf build/java-fixed-bench && mkdir -p build/java-fixed-bench/src
	./bin/schema generate --lang java --out build/java-fixed-bench/src bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-fixed-bench \
		build/java-fixed-bench/src/*.java test/java-fixedform/src/BenchFixed.java
	@touch $@

.PHONY: tables-java-fixedform-bench
tables-java-fixedform-bench: build/java-fixed-bench/.stamp build/java-fixed-corpus/.stamp
	$(JAVA) -ea -cp build/java-fixed-bench BenchFixed build/java-fixed-corpus
	$(JAVA)     -cp build/java-fixed-bench BenchFixed build/java-fixed-corpus

# ITS NEGATIVE CONTROL, and it moves a BYTE rather than a check: one text
# length written one too high in the fixed writer's template, which the C++
# reference's corpus must then refuse to match. Without this the byte
# comparison could be comparing a file with itself and nobody would know.
JAVA_FIXED_SABOTAGE := build/java-fixed-sabotage
.PHONY: tables-java-fixedform-negative-control
tables-java-fixedform-negative-control: build/java-fixed-corpus/.stamp
	@rm -rf $(JAVA_FIXED_SABOTAGE) && mkdir -p $(JAVA_FIXED_SABOTAGE)
	go run ./tools/sabotage -name fixed-form-java-text-length \
		-out $(JAVA_FIXED_SABOTAGE)/fixedform.gotext internal/codegen/javatable/fixedform.go
	@grep -q SABOTAGED $(JAVA_FIXED_SABOTAGE)/fixedform.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/javatable/fixedform.go":"%s/$(JAVA_FIXED_SABOTAGE)/fixedform.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_FIXED_SABOTAGE)/overlay.json
	go run -overlay=$(JAVA_FIXED_SABOTAGE)/overlay.json ./cmd/schema generate --lang java \
		--out $(JAVA_FIXED_SABOTAGE)/src bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@grep -q "Length + 1" $(JAVA_FIXED_SABOTAGE)/src/BenchMixedFixed.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged writer"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_FIXED_SABOTAGE)/classes \
		$(JAVA_FIXED_SABOTAGE)/src/*.java test/java-fixedform/src/BenchFixed.java
	@if $(JAVA) -cp $(JAVA_FIXED_SABOTAGE)/classes BenchFixed build/java-fixed-corpus \
			> $(JAVA_FIXED_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a text length one byte off left the fixed form green"; \
		cat $(JAVA_FIXED_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "the first byte differing from the C++ reference" $(JAVA_FIXED_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the fixed form went red, but not on the reference byte match"; \
		  cat $(JAVA_FIXED_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_FIXED_SABOTAGE)/log
	@echo "negative control: one byte off the fixed write template reds the reference byte match"

# THE ARM-TEXT CONTROL, and it plants the ENCODING this form replaced: the
# text op's flavour written over `arg`, which is the ARM ORDINAL a guarded
# entry is selected by. Under it a `string(N)` under a union's SECOND arm is
# dropped on every compiled-plan read — the field never lands and no counter
# moves — and nothing else about the record changes, which is why it went
# unseen. So the control requires the arm-text conformance case
# (test/tables/UT.schema) to go RED and to name the check that caught it.
JAVA_ARM_SABOTAGE := build/java-arm-sabotage
.PHONY: tables-java-fixedform-arm-negative-control
tables-java-fixedform-arm-negative-control: bin/schema build/fixedform-corpus/.stamp
	@rm -rf $(JAVA_ARM_SABOTAGE) && mkdir -p $(JAVA_ARM_SABOTAGE)
	go run ./tools/sabotage -name fixed-form-java-arm-text-flavour \
		-out $(JAVA_ARM_SABOTAGE)/fixedruntime.gotext internal/codegen/javatable/fixedruntime.go
	@grep -q SABOTAGED $(JAVA_ARM_SABOTAGE)/fixedruntime.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/javatable/fixedruntime.go":"%s/$(JAVA_ARM_SABOTAGE)/fixedruntime.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_ARM_SABOTAGE)/overlay.json
	go build -overlay $(JAVA_ARM_SABOTAGE)/overlay.json -o $(JAVA_ARM_SABOTAGE)/schema ./cmd/schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/ut test/tables/UT.schema
	@grep -q "SABOTAGED" $(JAVA_ARM_SABOTAGE)/src/ut/TableFixed.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged runtime"; exit 1; }
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/fu1 test/tables/FU1.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/fu2 test/tables/FU2.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/fx1 test/tables/FX1.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/fx2 test/tables/FX2.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/p1 test/tables/P1.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/p3 test/tables/P3.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/v1 test/tables/V1.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/v2 test/tables/V2.schema
	$(JAVA_ARM_SABOTAGE)/schema generate --lang java --out $(JAVA_ARM_SABOTAGE)/src/examples tables/examples
	$(JAVAC) --release 17 -nowarn -d $(JAVA_ARM_SABOTAGE)/classes \
		$(JAVA_ARM_SABOTAGE)/src/*/*.java test/java-fixedform/src/Main.java
	@if $(JAVA) -cp $(JAVA_ARM_SABOTAGE)/classes Main build/fixedform-corpus \
			> $(JAVA_ARM_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a text entry on the guard's own lane left the fixed form green"; \
		cat $(JAVA_ARM_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "arm text: a COMPILED plan lands a text field under the SECOND arm" $(JAVA_ARM_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on the arm's text"; \
		  cat $(JAVA_ARM_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_ARM_SABOTAGE)/log
	@echo "negative control: a text entry that borrows the guard's own byte drops the SECOND arm's text and reds"

# THE BYTE-ORDER LEG. Java reads a block and a cook explicitly little-endian, so
# this reader's order is a CONSTANT rather than the host's — and a file of the
# other order is refused twice: its magic reads back byte-swapped and its order
# word is not this reader's.
.PHONY: tables-java-order
tables-java-order: build/java-tables/.stamp build/cook-open/.stamp
	$(JAVA) -cp build/java-tables Main order build/cook-open/Scene.cook build/cook-open/Scene-be.cook

# THE REFERENCE EXTENT GATE (§6.3, §7.4), and it is the forged delta a blind read
# of this port found. §7.1 blesses a cook that carries data alone, so the region
# ends at the array's end and no directory bytes absorb an overrun; a reference
# whose target STARTS inside the region and whose RECORD does not fit must be
# refused by `at` and not one call later, in the caller's first field read.
.PHONY: tables-java-cook-extent
tables-java-cook-extent: build/java-tables/.stamp build/cook-open/.stamp
	$(JAVA) -cp build/java-tables Main extent build/cook-open/Scene.cook

# ITS NEGATIVE CONTROL: put the bound back on the target's START, which is where
# it was, and the gate must go red. This is the defect written as a test — a
# start bound passes every check the reader makes and then throws in the caller.
JAVA_EXTENT_SABOTAGE := build/java-extent-sabotage
JAVA_EXTENT_SABOTAGE_SED := s|long high = (long) region + regionLength - size - slot;|long high = (long) region + regionLength - 1 - slot; // SABOTAGED: the START, not the RECORD|
.PHONY: tables-java-cook-extent-negative-control
tables-java-cook-extent-negative-control: build/cook-open/.stamp
	@rm -rf $(JAVA_EXTENT_SABOTAGE) && mkdir -p $(JAVA_EXTENT_SABOTAGE)
	@sed '$(JAVA_EXTENT_SABOTAGE_SED)' internal/codegen/javatable/cook.go > $(JAVA_EXTENT_SABOTAGE)/javatable-cook.go.txt
	@cmp -s internal/codegen/javatable/cook.go $(JAVA_EXTENT_SABOTAGE)/javatable-cook.go.txt && \
		{ echo "NEGATIVE CONTROL: the Java cook sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/javatable/cook.go":"%s/$(JAVA_EXTENT_SABOTAGE)/javatable-cook.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_EXTENT_SABOTAGE)/overlay.json
	go build -overlay $(JAVA_EXTENT_SABOTAGE)/overlay.json -o $(JAVA_EXTENT_SABOTAGE)/schema ./cmd/schema
	$(JAVA_EXTENT_SABOTAGE)/schema generate --lang java --out $(JAVA_EXTENT_SABOTAGE)/generated/examples tables/examples
	$(JAVA_EXTENT_SABOTAGE)/schema generate --lang java --out $(JAVA_EXTENT_SABOTAGE)/generated/pointers tables/pointers
	$(JAVA_EXTENT_SABOTAGE)/schema generate --lang java --out $(JAVA_EXTENT_SABOTAGE)/generated/block tables/block
	@grep -lq SABOTAGED $(JAVA_EXTENT_SABOTAGE)/generated/pointers/SceneCook.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged at"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_EXTENT_SABOTAGE)/classes \
		$(JAVA_EXTENT_SABOTAGE)/generated/*/*.java test/java-tables/src/Main.java
	@if $(JAVA) -cp $(JAVA_EXTENT_SABOTAGE)/classes Main extent build/cook-open/Scene.cook \
			> $(JAVA_EXTENT_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a start-only bound left the extent gate green"; \
		cat $(JAVA_EXTENT_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "the bound is on the START and not on the RECORD" $(JAVA_EXTENT_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the extent"; \
		  cat $(JAVA_EXTENT_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_EXTENT_SABOTAGE)/log
	@echo "negative control: bounding a reference's START rather than its RECORD turns the extent gate RED"

# THE JAVA LEG's RELEASE PASS: everything `make test` cannot afford.
#
# `make test` on CI sits at about fourteen minutes against a fifteen-minute
# timeout, and that headroom was thin before this backend existed — this leg's
# gates cost about twenty seconds there. So the expensive half is here, by
# name — every unit compiled under -Werror, and the three planted controls,
# each of which rebuilds the compiler over a sabotaged emitter — and the cheap
# half rides every build. The split is a budget decision and is written down
# as one rather than left as an absence.
#
#     make tables-java-release
.PHONY: tables-java-release
tables-java-release:
	$(MAKE) tables-java-compile-all
	$(MAKE) tables-java-fixedform-negative-control
	$(MAKE) tables-java-fixedform-arm-negative-control
	# THE VERSIONING GATE'S OWN CONTROL, beside the byte gate's. It was written
	# and registered in make/negative-controls.json and then hung off NO recipe,
	# so nothing but the negative-controls matrix ever ran it and `make` itself
	# could not. It sits HERE and not in `test-java` because it rebuilds the
	# compiler over a sabotaged emitter and runs both columns of every §5.7 row
	# behind it — the expensive half, which is what this target is for (above).
	$(MAKE) tables-java-versioning-negative-control
	$(MAKE) conformance-negative-control-java-block
	$(MAKE) tables-java-fuzz-negative-control
	$(MAKE) tables-java-cook-extent-negative-control

# THE PAIRED UNIT, in the discovered generation graph beside the four the
# driver already tracks (c, cpp, cs, go each have this rule in their own .mk).
# Java's tree was committed without one, so `make generated/*/.stamp` — which
# is what CI's `generated` job discovers and runs — walked past it and the
# committed Java went stale against its own emitter without turning anything
# red. The paired driver regenerates this tree on `-mode build`, which is how
# the staleness surfaced at all: as a dirty checkpoint refusing a fast pass.
generated/bench/paired/java/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema make/java.mk
	@mkdir -p generated/bench/paired/java
	./bin/schema generate --lang java --out generated/bench/paired/java bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@touch $@

generated/bench/java/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang java --out generated/bench/java bench/corpus/Bench.schema
	./bin/schema generate --lang java --out generated/bench/java/realworld bench/corpus/RealWorld.schema

# The Java test legs compile the generated sources beside their Main.java —
# -Xlint:all -Werror on purpose: generated sources are compiled by the
# CONSUMER's javac, so a warning here is a build failure in their tree, not
# ours. Both modes then run: -ea (the checked twin — writer contracts fire)
# and default (the release shape, issue #156's target).
build/java-test/.stamp: generated/java/.stamp generated/bench/java/.stamp test/java/Main.java
	@mkdir -p build/java-test
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-test \
		generated/java/*.java generated/bench/java/*.java \
		generated/bench/java/realworld/*.java test/java/Main.java
	@touch $@

build/java-test-ludicrous/.stamp: generated/java-ludicrous/.stamp test/java-ludicrous/Main.java
	@mkdir -p build/java-test-ludicrous
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-test-ludicrous \
		generated/java-ludicrous/*.java test/java-ludicrous/Main.java
	@touch $@

# the Java bench runner's compile gate (the twin of `dart analyze bench/dart`);
# the timed run is by hand — bench/java/Main.java documents it
build/java-bench/.stamp: generated/bench/java/.stamp bench/java/Main.java
	@mkdir -p build/java-bench
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-bench \
		generated/bench/java/*.java bench/java/Main.java
	@touch $@

# THE JAVA LEG. One command answers every surface this port carries — the
# block surfaces and the cook surfaces; the five wire-carrying surfaces are
# ABSENT, because Java emits no table wire (schema#517). The corpus units, the
# block unit and the pointered unit are packages of ONE classpath, so a single
# JVM start-up covers the cook's node dump and the cook forgery battery too —
# which the C# leg hands to a second project because its cook side is a second
# assembly. `blockhome` is not on this classpath and does not need to be; the
# tables-java-compile gate is what proves it builds.
.PHONY: build-conformance-java
build-conformance-java: build/tables-generated-java/.stamp test/conformance/java/src/Driver.java
	@rm -rf build/conformance-java && mkdir -p build/conformance-java
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/conformance-java \
		build/tables-generated-java/examples/*.java build/tables-generated-java/pointers/*.java \
		build/tables-generated-java/block/*.java build/tables-generated-java/v1/*.java \
		build/tables-generated-java/v2/*.java build/tables-generated-java/p1/*.java \
		build/tables-generated-java/p3/*.java test/conformance/java/src/Driver.java

# THE JAVA CONFORMANCE NEGATIVE CONTROL, and it localises the block form's
# Open. The fuzz and extent controls above each remove a bound; this one removes
# a CHECK — the array's pitch against this build's own — so the forged image
# `block_pitch` opens where it must refuse.
#
# The half that matters is which cells move: `forgery` goes RED and `block` and
# `block-dump` stay GREEN, because the two valid images carry this build's pitch
# and open either way. A control that turned block red too would be proving the
# reader was broken, not that it CHECKS.
CONFORMANCE_NEGATIVE_JAVA_BLOCK := build/conformance-negative-java-block
CONFORMANCE_NEGATIVE_JAVA_BLOCK_SED := s|if (stride != %sStride) { return null; }|if (stride != %sStride) { } // SABOTAGED|
.PHONY: conformance-negative-control-java-block
conformance-negative-control-java-block: build/conformance-harness
	@rm -rf $(CONFORMANCE_NEGATIVE_JAVA_BLOCK) && mkdir -p $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)
	@sed '$(CONFORMANCE_NEGATIVE_JAVA_BLOCK_SED)' internal/codegen/javatable/block.go > $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/javatable-block.go.txt
	@cmp -s internal/codegen/javatable/block.go $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/javatable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Java block sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/javatable/block.go":"%s/$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/javatable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/overlay.json
	go build -overlay $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/overlay.json -o $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema ./cmd/schema
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/examples tables/examples
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/pointers tables/pointers
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/block tables/block
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/v1 test/tables/V1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/v2 test/tables/V2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/p1 test/tables/P1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/p3 test/tables/P3.schema
	@grep -lq SABOTAGED $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/block/*Block.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged Open"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/classes \
		$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/*/*.java test/conformance/java/src/Driver.java
	@printf '#!/bin/sh\nexec %s -cp %s/classes Driver "$$@"\n' "$(JAVA)" "$(CURDIR)/$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)" > $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/driver
	@printf 'java %s/driver\n' "$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)" > $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/drivers.txt
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/work > $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a block reader with no pitch check left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log; exit 1; \
	fi
	@grep -q "java / forgery" $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the forgery battery"; \
		  cat $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log; exit 1; }
	@grep -q "^block         pass" $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: block went red too, so the control does not localise the CHECK"; \
		  cat $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log; exit 1; }
	@grep -q "^block-dump    pass" $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: block-dump went red too, so the control does not localise the CHECK"; \
		  cat $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log; exit 1; }
	@grep -m1 "java / forgery" $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/log
	@echo "negative control: one missing pitch check in the Java block Open turns the harness RED on forgery alone"

# THE JAVA LEG of `make test`: the compile, standalone and zero-cost gates, the
# readers' fuzz oracle, the byte-order leg and the reference extent gate. The
# three planted controls are `make tables-java-release`, because `make test`
# has no budget for them (see that target). Then the packet tests, with and
# without -ea.
.PHONY: test-java
test-java: toolchain-java generated/java/.stamp generated/java-ludicrous/.stamp generated/bench/java/.stamp build/java-test/.stamp build/java-test-ludicrous/.stamp build/java-bench/.stamp
	$(MAKE) tables-java-compile
	$(MAKE) tables-java-standalone
	$(MAKE) tables-java-zero-cost
	$(MAKE) tables-java-fuzz
	$(MAKE) tables-java-order
	$(MAKE) tables-java-cook-extent
	# THE FIXED FORM against the C++ reference's own bytes (docs/SPEC-TABLES.md §3.4)
	$(MAKE) tables-java-fixedform
	$(MAKE) tables-java-fixedform-bench
	cd test/java && $(JAVA) -ea -cp ../../build/java-test Main
	cd test/java && $(JAVA) -cp ../../build/java-test Main
	cd test/java-ludicrous && $(JAVA) -ea -cp ../../build/java-test-ludicrous Main
	cd test/java-ludicrous && $(JAVA) -cp ../../build/java-test-ludicrous Main

TEST_LEGS          += test-java
TOOLCHAIN_LEGS     += java
TOOLCHAIN_PINS_java := JAVA JAVAC
CONFORMANCE_LEGS   += $(call unless_skipped,java,build-conformance-java)
CONFORMANCE_ENV    += JAVA=$(JAVA)
# Packet UTF-8 content validation, including a compiled mutation control.
build/packet-text/java/.stamp: bin/schema test/packet-text/Narrow.schema
	@mkdir -p build/packet-text/java/source
	cp test/packet-text/Narrow.schema build/packet-text/java/source/Text.schema
	./bin/schema generate --lang java --out build/packet-text/java build/packet-text/java/source/Text.schema
	@touch $@

.PHONY: packet-utf8-java packet-utf8-java-negative-control
packet-utf8-java: build/packet-text/java/.stamp build/packet-text/cpp/driver build/packet-text/harness
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-text/java/classes build/packet-text/java/*.java test/packet-text/java/Main.java
	./build/packet-text/harness $(JAVA) -ea -cp build/packet-text/java/classes Main
	./build/packet-text/harness $(JAVA) -cp build/packet-text/java/classes Main

packet-utf8-java-negative-control: packet-utf8-java
	@mkdir -p build/packet-text/java-negative
	go run ./tools/sabotage -name packet-utf8-java-read -out build/packet-text/java-negative/utf8.gotext internal/codegen/java/utf8.go
	@printf '{"Replace":{"%s/internal/codegen/java/utf8.go":"%s/build/packet-text/java-negative/utf8.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/java-negative/overlay.json
	go run -overlay=build/packet-text/java-negative/overlay.json ./cmd/schema generate --lang java --out build/packet-text/java-negative build/packet-text/java/source/Text.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-text/java-negative/classes build/packet-text/java-negative/*.java test/packet-text/java/Main.java
	@if ./build/packet-text/harness -mutations-only $(JAVA) -cp build/packet-text/java-negative/classes Main > build/packet-text/java-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Java UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/java-negative/log || { cat build/packet-text/java-negative/log; exit 1; }
	@echo 'packet UTF-8 Java negative control: removed read validation fails bit-flip agreement'

test-java: packet-utf8-java packet-utf8-java-negative-control


build/packet-wide/java/.stamp: bin/schema build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema
	./bin/schema generate --lang java --out build/packet-wide/java/wide build/packet-wide/source/WideText.schema
	./bin/schema generate --lang java --out build/packet-wide/java/shapes test/packet-wide/Shapes.schema
	@touch $@

.PHONY: packet-wide-java packet-wide-java-negative-control
packet-wide-java: build/packet-wide/java/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-wide/java/classes build/packet-wide/java/wide/*.java build/packet-wide/java/shapes/*.java test/packet-wide/java/*.java
	$(JAVA) -ea -cp build/packet-wide/java/classes Main --contracts
	$(JAVA) -cp build/packet-wide/java/classes Main --contracts
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver $(JAVA) -ea -cp build/packet-wide/java/classes Main
	./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver $(JAVA) -cp build/packet-wide/java/classes Main

packet-wide-java-negative-control: packet-wide-java
	@mkdir -p build/packet-wide/java-negative
	go run ./tools/sabotage -name packet-wide-java-pairing -out build/packet-wide/java-negative/wstring.gotext internal/codegen/java/wstring.go
	@printf '{"Replace":{"%s/internal/codegen/java/wstring.go":"%s/build/packet-wide/java-negative/wstring.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-wide/java-negative/overlay.json
	go run -overlay=build/packet-wide/java-negative/overlay.json ./cmd/schema generate --lang java --out build/packet-wide/java-negative build/packet-wide/source/WideText.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-wide/java-negative/classes build/packet-wide/java-negative/*.java build/packet-wide/java/shapes/*.java test/packet-wide/java/*.java
	@if ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver -mutations-only $(JAVA) -cp build/packet-wide/java-negative/classes Main > build/packet-wide/java-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Java wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/java-negative/log || { cat build/packet-wide/java-negative/log; exit 1; }
	@echo 'packet wide Java negative control: removed pairing fails bit-flip agreement'

test-java: packet-wide-java packet-wide-java-negative-control
