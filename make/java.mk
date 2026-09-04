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
# READING TIER: the tolerant wire, the text form, the reflection descriptors,
# and the two accelerators' read halves. The C++ backend is the reference and
# the C# one is the worked managed-language port; this leg mirrors both.
#
# Java's unit scope is the PACKAGE and a public type lives in a file of its own
# name, so the shared runtime is one file per type rather than one home file —
# which is what makes "where does the runtime live" a question with no rule to
# state and no file order to depend on.
# ---------------------------------------------------------------------------

# The same corpus through the Java table backend: the tables corpus plus the
# evolution pair, generated at build time into build/ — test-only, never part of
# the committed generated/ tree. The full unit is generated (packet .java +
# <Base>Table.java + the runtime), because a table's closure decodes into the
# packet emitter's own classes.
build/tables-generated-java/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P3.schema
	@mkdir -p build/tables-generated-java
	./bin/schema generate --lang java --out build/tables-generated-java/examples tables/examples
	# the POINTERED unit: its Java WIRE surface is refused by name (§11) and its
	# two ACCELERATORS are emitted all the same, because neither needs a codec
	# (§7, §19). This is where the cook's Java read side comes from.
	./bin/schema generate --lang java --out build/tables-generated-java/pointers tables/pointers
	./bin/schema generate --lang java --out build/tables-generated-java/block tables/block
	./bin/schema generate --lang java --out build/tables-generated-java/blockhome tables/blockhome
	./bin/schema generate --lang java --out build/tables-generated-java/v1 test/tables/V1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/v2 test/tables/V2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/p1 test/tables/P1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/p3 test/tables/P3.schema
	@touch $@

# THE GENERIC-WALK GATE, Java side (docs/SPEC-TABLES.md §16). One walker per unit
# and every walker in the corpus the same bytes — the property the C++ and C#
# gates hold, over the shape Java forces: the walk is a public class, so its
# home is TableJson.java and there is exactly one of them per package.
.PHONY: tables-java-json-walk
tables-java-json-walk: build/tables-generated-java/.stamp
	@rm -rf build/json-walk-java && mkdir -p build/json-walk-java
	@for d in build/tables-generated-java/*/; do \
		unit=$$(basename $$d); \
		[ -e "$$d/TableJson.java" ] || continue; \
		awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$d/TableJson.java > build/json-walk-java/$$unit; \
		[ -s build/json-walk-java/$$unit ] || \
			{ echo "GENERIC-WALK GATE FAILED: unit $$unit carries a TableJson.java with no walker in it"; exit 1; }; \
		n=$$(ls $$d*Table.java 2>/dev/null | wc -l | tr -d ' '); \
		[ "$$n" -gt 0 ] || \
			{ echo "GENERIC-WALK GATE FAILED: unit $$unit has a walker and no table source"; exit 1; }; \
	done
	@if [ -z "$$(ls build/json-walk-java 2>/dev/null)" ]; then \
		echo "GENERIC-WALK GATE FAILED: no walker in any generated .java"; exit 1; fi
	@first=""; for f in build/json-walk-java/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables Java generic-walk gate: one walker per unit, byte-identical across $$(ls build/json-walk-java | wc -l | tr -d ' ') units"

# The Java twin of the C++ "no serialize include path" build: a generated
# <Base>Table.java must stand alone on the JDK, so nothing in it may name the
# serialize runtime — and nothing may name a THIRD-PARTY JSON library either,
# because the text form is this backend's own walk over the descriptors (§16).
.PHONY: tables-java-standalone
tables-java-standalone: build/tables-generated-java/.stamp
	@n=$$(ls build/tables-generated-java/*/*Table.java 2>/dev/null | wc -l | tr -d ' '); \
		if [ "$$n" -lt 8 ]; then \
			echo "STANDALONE GATE FAILED: found $$n generated Table sources, expected 8 — the glob, not the property, is what broke"; exit 1; \
		fi
	@for f in build/tables-generated-java/*/*Table.java build/tables-generated-java/*/TableJson.java; do \
		[ -e "$$f" ] || continue; \
		if grep -n "Serialize\|com\.fasterxml\|com\.google\.gson\|org\.json" $$f; then \
			echo "STANDALONE GATE FAILED: a runtime dependency leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables Java standalone gate: generated Table sources name no runtime and no JSON library"

# The Java VARIABLE-CLASS REFUSAL (docs/SPEC-TABLES.md §2.2, §11), the C# gate's
# twin: no Table source at all for a pointered unit, and every source the unit
# does emit opening with a banner that names each refused table and the
# follow-on. The two ACCELERATORS need no codec, so the cooks still open.
.PHONY: tables-java-refuses-pointers
tables-java-refuses-pointers: bin/schema
	@rm -rf build/tables-java-refusal && mkdir -p build
	./bin/schema generate --lang java --out build/tables-java-refusal tables/pointers
	@if ls build/tables-java-refusal/*Table.java >/dev/null 2>&1; then \
		echo "REFUSAL GATE FAILED: the Java backend emitted a wire surface for a pointered unit"; exit 1; \
	fi
	@if [ -e build/tables-java-refusal/TableJson.java ] || [ -e build/tables-java-refusal/TableReader.java ]; then \
		echo "REFUSAL GATE FAILED: the Java backend emitted the wire runtime for a pointered unit"; exit 1; \
	fi
	@for f in build/tables-java-refusal/*Cook.java build/tables-java-refusal/*Block.java; do \
		grep -q "THE JAVA WIRE SURFACE OF THIS UNIT IS REFUSED, BY NAME" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not carry the refusal banner"; exit 1; }; \
		grep -q "is a named follow-on" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name the follow-on"; exit 1; }; \
		grep -q "Album, Depot, Layer, ListNode, Marker, Scene and TreeNode" $$f || \
			{ echo "REFUSAL GATE FAILED: $$f does not name every refused table"; exit 1; }; \
	done
	@n=$$(ls build/tables-java-refusal/*Cook.java | wc -l | tr -d ' '); \
		if [ "$$n" -lt 3 ]; then \
			echo "REFUSAL GATE FAILED: found $$n Cook sources for the pointered unit, expected 3 — the glob, not the property, is what broke"; exit 1; \
		fi
	@echo "tables Java refusal gate: a pointered unit's WIRE half is refused by name, in every source it does emit, and its cooks still open"

# THE ZERO-COST GATE, Java side (docs/SPEC-TABLES.md §2.2): a unit that declares
# no table emits not one byte of table code — no <Base>Table.java, no runtime
# type, no Row, no Block, no Cook — so a consumer that never wrote `table` pays
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

# THE JAVA TABLES LEG (test/java-tables/src/Main.java): the three gates the
# conformance harness does not hold, because none of them is a case — the
# readers' fuzz oracle, the allocation measurement and the soak.
build/java-tables/.stamp: build/tables-generated-java/.stamp test/java-tables/src/Main.java
	@rm -rf build/java-tables && mkdir -p build/java-tables
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-tables \
		build/tables-generated-java/examples/*.java build/tables-generated-java/pointers/*.java \
		build/tables-generated-java/block/*.java test/java-tables/src/Main.java
	@touch $@

# THE ALLOCATION GATE, and it is a MEASUREMENT of a COUNT rather than an
# inference from a heap.
#
# A gate on heap drift is a LEAK instrument: it sees storage that is RETAINED and
# is blind to a per-iteration allocation that is collected — which on a read path
# is the defect that matters, because a codec allocating a byte per field keeps a
# flat heap and ruins a frame budget. So the number here is BYTES PER RECORD,
# from the JVM's own per-thread allocation counter, over a window that follows a
# warm-up, with a NAMED floor per path:
#
#   the tolerant wire's read and save        EXACTLY 0
#   the block's row walk, the cook's read    EXACTLY 0
#   either accelerator's open                one handle, per FILE not per row
#   the text form (§16)                      a stated ceiling: it allocates by
#                                            nature, and what it allocates is
#                                            named in the source
#
# The exact zeros are the contract (§3): the caller owns the value, the buffer
# and the report, and this port adds the READER and the WRITER to that list,
# because a nested body moves the reader's limit instead of slicing a sub-reader.
# SCHEMA_ALLOC_SCALE sizes the measured window. The default is what a per-build
# gate needs — enough passes to carry every path past its tiers and read a
# steady number; a release pass raises it.
JAVA_ALLOC_SCALE ?= 25
.PHONY: tables-java-alloc
tables-java-alloc: build/java-tables/.stamp build/cook-open/.stamp
	SCHEMA_ALLOC_SCALE=$(JAVA_ALLOC_SCALE) $(JAVA) -cp build/java-tables Main alloc testdata/wire/tables \
		testdata/wire/tables/block_render.bin build/cook-open/Scene.cook

# ITS NEGATIVE CONTROL: ONE extra allocation per record on the wire read path —
# a `new byte[1]`, the smallest thing the language can be asked for — and the
# exact-zero floor must catch it. A gate that could not see twenty-four bytes a
# record is a gate that would not have seen the defect it exists for.
#
# The other half is the localisation, as it is for every control here: the wire
# READ row goes red and every other row stays green, so the gate says WHICH path
# allocated rather than "something did".
.PHONY: tables-java-alloc-negative-control
tables-java-alloc-negative-control: build/java-tables/.stamp build/cook-open/.stamp
	@if SCHEMA_ALLOC_SABOTAGE=1 SCHEMA_ALLOC_SCALE=$(JAVA_ALLOC_SCALE) $(JAVA) -cp build/java-tables Main alloc testdata/wire/tables \
			testdata/wire/tables/block_render.bin build/cook-open/Scene.cook \
			> build/java-alloc-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one allocation per record left the gate green"; \
		cat build/java-alloc-negative.log; exit 1; \
	fi
	@grep -q "wire read.*EXPECTED 0/record" build/java-alloc-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the sabotaged path"; \
		  cat build/java-alloc-negative.log; exit 1; }
	@grep -q "wire save .*== 0" build/java-alloc-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: every row went red, so it localises nothing"; \
		  cat build/java-alloc-negative.log; exit 1; }
	@grep -m1 "wire read" build/java-alloc-negative.log
	@echo "negative control: one allocation per record turns the Java allocation gate RED on that path alone"

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

# THE SOAK's OWN NEGATIVE CONTROL. The soak is the gate this port leads with, and
# its planted allocation was unreachable from soak mode — the flag was read
# inside the alloc mode alone, so SCHEMA_ALLOC_SABOTAGE was a silent no-op here
# and the gate had never once been red. A short soak is enough to prove it fires:
# the plant is per-record and the first sample lands after warm-up.
.PHONY: tables-java-soak-negative-control
tables-java-soak-negative-control: build/java-tables/.stamp build/cook-open/.stamp
	@if SCHEMA_ALLOC_SABOTAGE=1 $(JAVA) -Xmx256m -cp build/java-tables Main soak 22 testdata/wire/tables \
			testdata/wire/tables/block_render.bin build/cook-open/Scene.cook \
			> build/java-soak-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one allocation per record left the soak green"; \
		cat build/java-soak-negative.log; exit 1; \
	fi
	@grep -q "^SABOTAGED" build/java-soak-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak never read the sabotage flag"; \
		  cat build/java-soak-negative.log; exit 1; }
	@grep -q "wire read.*EXPECTED 0/record" build/java-soak-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak went red, but not on the sabotaged path"; \
		  cat build/java-soak-negative.log; exit 1; }
	@grep -q "1 breach" build/java-soak-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the soak did not count the breach"; \
		  cat build/java-soak-negative.log; exit 1; }
	@grep -m1 "wire read" build/java-soak-negative.log
	@echo "negative control: one allocation per record turns the Java SOAK RED, on that path alone"

# THE SOAK, and what it gates on is the ALLOCATION TABLE re-measured at every
# sample, not only the heap — a heap-flat gate cannot see a per-iteration
# allocation that is collected, and an allocation that appears an hour in (a
# descriptor rebuilt, a cache that grew a wrapper) is exactly the case it would
# miss. The heap check stands beside it as the second, weaker instrument: it is
# the one that sees RETENTION.
#
# `make test` runs a short one so the property is exercised on every
# build; the RELEASE soak is an hour and is run by hand:
#
#     make tables-java-soak JAVA_SOAK_SECONDS=3600
#
JAVA_SOAK_SECONDS ?= 60
.PHONY: tables-java-soak
tables-java-soak: build/java-tables/.stamp build/cook-open/.stamp
	$(JAVA) -Xmx256m -cp build/java-tables Main soak $(JAVA_SOAK_SECONDS) testdata/wire/tables \
		testdata/wire/tables/block_render.bin build/cook-open/Scene.cook

# THE JAVA LEG's RELEASE PASS: everything `make test` cannot afford.
#
# `make test` on CI sits at about fourteen minutes against a fifteen-minute
# timeout, and that headroom was thin before this backend existed — this leg's
# gates cost about twenty seconds there and the soak alone would cost thirty
# more. So the expensive half is here, by name, and the cheap half rides every
# build. The split is a budget decision and is written down as one rather than
# left as an absence.
#
#     make tables-java-release                     the default 60 s soak
#     make tables-java-release JAVA_SOAK_SECONDS=3600   the hour
.PHONY: tables-java-release
tables-java-release:
	$(MAKE) tables-java-compile-all
	$(MAKE) conformance-negative-control-java-block
	$(MAKE) tables-java-fuzz-negative-control
	$(MAKE) tables-java-cook-extent-negative-control
	$(MAKE) tables-java-soak-negative-control
	$(MAKE) tables-java-alloc JAVA_ALLOC_SCALE=100
	$(MAKE) tables-java-soak JAVA_SOAK_SECONDS=$(JAVA_SOAK_SECONDS)

generated/bench/tables/java/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/java
	./bin/schema generate --lang java --out generated/bench/tables/java bench/corpus/BenchTable.schema
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

# THE JAVA LEG. One command answers all ten surfaces: the wire units, the block
# unit and the pointered unit are packages of ONE classpath, so a single JVM
# start-up covers the cook's node dump and the cook forgery battery too — which
# the C# leg hands to a second project because its cook side is a second
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

# THE JAVA LEG's NEGATIVE CONTROL, and it is the C# one's twin because the two
# ports have the same shape: the walk is EMITTER SOURCE — one constant in
# internal/codegen/javatable/json.go — so the sabotage lands in the emitter and
# the leg is generated afresh from it. No tracked file is written to: the sed
# lands in build/, a Go build overlay points the compiler at it, and the driver
# below is compiled against what that compiler generated.
#
# THE SABOTAGE is the C# control's: the FIELD INDEX the read path looks a
# descriptor up by, so one key's value lands in its neighbour's field. It is
# bounded on purpose, so a table with an odd field count cannot turn the control
# into an exception rather than a wrong answer, and it touches the READ path
# only — which is what makes json-write staying green the statement that the
# break is the READER's.
.PHONY: conformance-negative-control-java
conformance-negative-control-java:
	@echo "conformance-negative-control-java: dormant — the surface it turns red is absent while this port writes the wire's previous form (docs/SPEC-TABLES.md §3, schema#517)"

# THE SECOND JAVA NEGATIVE CONTROL, and it localises a DIFFERENT reader: the
# block form's Open. The three controls above all move a value; this one removes
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

# THE JAVA LEG of `make test`. ONE conformance negative control rides here, the
# C# and Go legs' twin; the second one, the fuzz oracle's control and the SOAK
# are `make tables-java-release`, because `make test` has no budget for them
# (see that target). Then the packet tests, with and without -ea.
.PHONY: test-java
test-java: generated/java/.stamp generated/java-ludicrous/.stamp generated/bench/java/.stamp build/java-test/.stamp build/java-test-ludicrous/.stamp build/java-bench/.stamp
	$(MAKE) conformance-negative-control-java
	$(MAKE) tables-java-compile
	$(MAKE) tables-java-json-walk
	$(MAKE) tables-java-standalone
	$(MAKE) tables-java-refuses-pointers
	$(MAKE) tables-java-zero-cost
	$(MAKE) tables-java-alloc
	$(MAKE) tables-java-alloc-negative-control
	$(MAKE) tables-java-fuzz
	$(MAKE) tables-java-order
	$(MAKE) tables-java-cook-extent
	cd test/java && $(JAVA) -ea -cp ../../build/java-test Main
	cd test/java && $(JAVA) -cp ../../build/java-test Main
	cd test/java-ludicrous && $(JAVA) -ea -cp ../../build/java-test-ludicrous Main
	cd test/java-ludicrous && $(JAVA) -cp ../../build/java-test-ludicrous Main

TEST_LEGS         += test-java
CONFORMANCE_LEGS  += build-conformance-java
CONFORMANCE_ENV   += JAVA=$(JAVA)
BENCH_TABLES_LEGS += generated/bench/tables/java/.stamp
