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

# THE FINITE-FLOAT LEG (SPEC §4.3, serialize format 2.0): a packet float32 or
# float64 is finite. test/java/FiniteMain.java holds the write refusals (the
# checkWrite contract, -ea), the read refusals from crafted bytes
# (non_finite_float on the raw bits), and -0 and the subnormals bit exact, over
# test/java/Finite.schema; both modes run. The control drops the read check at
# both widths and must red exactly the twenty-four read vectors.
build/java-finite/.stamp: bin/schema test/java/Finite.schema make/java.mk
	@rm -rf build/java-finite/gen
	./bin/schema generate --lang java --out build/java-finite/gen test/java/Finite.schema
	@touch $@

.PHONY: packet-finite-java packet-finite-java-negative-control
packet-finite-java: build/java-finite/.stamp
	@mkdir -p build/java-finite/classes
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-finite/classes build/java-finite/gen/*.java test/java/FiniteMain.java
	$(JAVA) -ea -cp build/java-finite/classes FiniteMain
	$(JAVA) -cp build/java-finite/classes FiniteMain

packet-finite-java-negative-control: bin/schema
	@rm -rf build/java-finite-negative
	@mkdir -p build/java-finite-negative/classes
	go run ./tools/sabotage -name packet-finite-java-read-check \
		-out build/java-finite-negative/functions.gotext internal/codegen/java/functions.go
	@printf '{"Replace":{"%s/internal/codegen/java/functions.go":"%s/build/java-finite-negative/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/java-finite-negative/overlay.json
	go build -overlay=build/java-finite-negative/overlay.json -o build/java-finite-negative/schema ./cmd/schema
	./build/java-finite-negative/schema generate --lang java --out build/java-finite-negative/gen test/java/Finite.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-finite-negative/classes build/java-finite-negative/gen/*.java test/java/FiniteMain.java
	@if $(JAVA) -ea -cp build/java-finite-negative/classes FiniteMain > build/java-finite-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a non-finite float read passed in Java'; exit 1; fi
	@test "$$(grep -c '^FAILED: non_finite_float read: ' build/java-finite-negative/log)" = 24 && \
		test "$$(grep -c '^FAILED: ' build/java-finite-negative/log)" = 24 || \
		{ echo 'NEGATIVE CONTROL FAILED: Java did not red exactly the 24 read vectors'; cat build/java-finite-negative/log; exit 1; }
	@echo 'packet finite Java negative control: a dropped read check reds the 24 non_finite_float read vectors'

test-java: packet-finite-java packet-finite-java-negative-control

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
# The TABLE WIRE — the fixed form (§3.4) for every table, the message form
# (§3.3) for a `fixed table` root, and the variable form (§3, form byte 1) with
# its retain-unknown surface (§6.6) — rides beside the two accelerators. The
# C++ backend is the reference and the C# one is the worked managed-language
# port; this leg mirrors both.
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
build/tables-generated-java/.stamp: bin/schema make/java.mk $(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_ARMS) $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_BLOCK) $(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_WIDE) test/tables/V1.schema test/tables/V2.schema test/tables/W1.schema test/tables/W2.schema test/tables/R1.schema test/tables/R2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/M1.schema test/tables/M2.schema test/tables/A1.schema test/tables/A2.schema test/tables/G1.schema test/tables/FX1.schema test/tables/FX2.schema test/tables/UT.schema test/tables/FU1.schema test/tables/FU2.schema $(SCHEMAS_TABLES_SCALARS) test/tables/K1.schema test/tables/K2.schema test/tables/Scalars2.schema test/tables/RT1.schema $(SCHEMAS_TABLES_BACKEND) $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9)
	@mkdir -p build/tables-generated-java
	./bin/schema generate --lang java --out build/tables-generated-java/examples tables/examples
	# the POINTERED unit and the BYTE-BUFFER unit: their cook readers and
	# variable-class codecs ride this stamp, and the LIST unit (tables/lists)
	# rides it too: its SaveVariable codec is the list half of the
	# tables-java-variable-class corpus and the soak's three case set.
	./bin/schema generate --lang java --out build/tables-generated-java/pointers tables/pointers
	./bin/schema generate --lang java --out build/tables-generated-java/blobs tables/blobs
	./bin/schema generate --lang java --out build/tables-generated-java/lists tables/lists
	./bin/schema generate --lang java --out build/tables-generated-java/block tables/block
	./bin/schema generate --lang java --out build/tables-generated-java/blockhome tables/blockhome
	./bin/schema generate --lang java --out build/tables-generated-java/v1 test/tables/V1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/v2 test/tables/V2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/w1 test/tables/W1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/w2 test/tables/W2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/r1 test/tables/R1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/r2 test/tables/R2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/p1 test/tables/P1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/p2 test/tables/P2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/p3 test/tables/P3.schema
	./bin/schema generate --lang java --out build/tables-generated-java/m1 test/tables/M1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/m2 test/tables/M2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/a1 test/tables/A1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/a2 test/tables/A2.schema
	# G1 declares `optional ?Wrapped`, an optional over a variable closure: every
	# walk that follows pointer edges gates on its presence companion, so the
	# conformance leg's tblg1.Guarded rows ride it (docs/SPEC-TABLES.md §2.3, §3.1)
	./bin/schema generate --lang java --out build/tables-generated-java/g1 test/tables/G1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/messages tables/messages
	# the VARIABLE-ARM message unit (Makefile SCHEMAS_TABLES_MESSAGES): Feed's
	# union carries a pointer arm and an array of pointers, so it is a region
	# and the conformance leg's stream_* and parts_* rows ride it
	./bin/schema generate --lang java --out build/tables-generated-java/stream tables/stream
	# RT1 is the RETAIN-UNKNOWN reader (docs/SPEC-TABLES.md §6.6): the
	# conformance leg's retain and retain-save rows load RT2's wires with it
	./bin/schema generate --lang java --out build/tables-generated-java/rt1 test/tables/RT1.schema
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
	# K1/K2 is the ENUM-AGAINST-INTEGER pair (docs/SPEC-TABLES.md §3, §4):
	# K1 writes grade=Grade, raw=uint16; K2 reads grade=uint16, raw=Grade.
	# Both fields produce kind_mismatch == 2 in both directions.
	./bin/schema generate --lang java --out build/tables-generated-java/k1 test/tables/K1.schema
	./bin/schema generate --lang java --out build/tables-generated-java/k2 test/tables/K2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/maps tables/maps
	./bin/schema generate --lang java --out build/tables-generated-java/arms tables/arms
	# scalars2 (package scalardemo2) is the later build's view of the wide-kind
	# unit: the conformance report rows read scalars_*.bin through it, counting
	# the kind mismatches and raw-scale clamps of docs/SPEC-TABLES.md §15.
	./bin/schema generate --lang java --out build/tables-generated-java/scalars2 test/tables/Scalars2.schema
	./bin/schema generate --lang java --out build/tables-generated-java/backend tables/backend
	./bin/schema generate --lang java --out build/tables-generated-java/vocab tables/vocab
	./bin/schema generate --lang java --out build/tables-generated-java/vocab9 tables/vocab9
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
			Table*|*Table.java|*Row.java|*Block.java|*Cook.java|*Message.java|BuildVersion.java) \
				echo "ZERO-COST GATE FAILED: a table-free unit emitted $$f"; exit 1;; \
		esac; \
	done
	@cmp -s build/tables-java-zero/Types.java generated/java/Types.java || \
		{ echo "ZERO-COST GATE FAILED: a table-free unit's packet output is not the committed one"; exit 1; }
	@echo "tables Java zero-cost gate: a table-free unit emits no table code at all"

# THE RUNTIME HOME IS FILE-ORDER-PROOF ON JAVA BY CONSTRUCTION, AND NOW IT IS
# ASSERTED (docs/PORTING.md:1412, J2 — "The runtime home, with a file-order
# control"). In every port a unit's shared runtime is named by the PACKAGE and
# never by the file that happens to sort first (docs/SPEC-TABLES.md §19.2), so a
# schema file that sorts earlier relocates nothing. Java meets that by
# construction rather than by a rule: a public type lives in a file of its own
# name, so the shared runtime is ONE FILE PER TYPE — TableBytes.java,
# TableBlockInfo.java, TableCookStorage.java and the rest — and javatable has no
# expression that computes a home a file-order rule could reach (no
# `capitalize(u.Package)`, no `ProtocolIdHome` on this surface). The gate
# therefore asserts the literal names runtimeFileNames() gives, not a grepped
# marker: on Java the file name IS the claim. The packet surface's protocol-id
# home is file-ordered ON PURPOSE (ir.ProtocolIdHome is u.Files[0].Base — the
# trap at internal/codegen/java/java.go:71), and that is not what this gate
# watches.
#
# Nine of the thirteen bodies are CONSTANT SOURCE — TableBytes, TableBlockRows,
# TableBlockInfo, TableBlockFieldInfo, TableCookInfo, TableCookFieldInfo,
# TableCookStorage, TableFieldInfo and TableTypeInfo — so the gate byte-compares
# those across the two trees. The other four (BuildVersion, TableBlockLayout,
# TableCookLayout and TableRegistry) carry unit facts that a new table
# legitimately moves, and are asserted by name only.
.PHONY: tables-java-runtime-home
tables-java-runtime-home: bin/schema
	@rm -rf build/runtime-home-java && mkdir -p build/runtime-home-java/src
	@cp tables/examples/*.schema build/runtime-home-java/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home-java/src/Aaa.schema
	@./bin/schema generate --lang java --out build/runtime-home-java/base tables/examples
	@./bin/schema generate --lang java --out build/runtime-home-java/added build/runtime-home-java/src
	@n=0; for t in TableBytes TableBlockRows TableBlockInfo TableBlockFieldInfo TableBlockLayout TableCookInfo TableCookFieldInfo TableCookStorage TableCookLayout TableFieldInfo TableTypeInfo TableRegistry BuildVersion; do \
		b=0; a=0; \
		[ -f build/runtime-home-java/base/$$t.java ] && b=1 && n=$$((n+1)); \
		[ -f build/runtime-home-java/added/$$t.java ] && a=1; \
		if [ "$$b" != "$$a" ]; then \
			echo "RUNTIME HOME GATE FAILED (Java): $$t.java is in one tree and not the other — the shared runtime moved when an earlier-sorting file joined the unit"; exit 1; \
		fi; \
	done; \
	if [ "$$n" -lt 13 ]; then \
		echo "RUNTIME HOME GATE FAILED (Java): found $$n runtime types, expected at least 13 — the glob, not the property, is what broke"; exit 1; \
	fi
	@for t in TableBytes TableBlockRows TableBlockInfo TableBlockFieldInfo TableCookInfo TableCookFieldInfo TableCookStorage TableFieldInfo TableTypeInfo; do \
		cmp -s build/runtime-home-java/base/$$t.java build/runtime-home-java/added/$$t.java || \
			{ echo "RUNTIME HOME GATE FAILED (Java): $$t.java's bytes moved when the unit gained a file"; exit 1; }; \
	done
	@echo "runtime home gate (Java): every shared runtime type stays in its own <Type>.java, byte for byte, when an earlier-sorting file joins the unit"

# The negative control: the file-order rule put back, in Java's spelling — the
# byte-access runtime named by the file that sorts first instead of by itself.
# The sabotage is the ONE edit, applied through `go build -overlay` exactly as
# the C# (Makefile:1734) and JS (make/js.mk:322) controls do it, so no tracked
# Go file is ever written. `ir` is already imported in javatable.go.
.PHONY: tables-java-runtime-home-negative-control
tables-java-runtime-home-negative-control: bin/schema tables-java-runtime-home
	@sed 's|out\["TableBytes.java"\] = tableBytesFile(u)|out[ir.ProtocolIdHome(u) + "Bytes.java"] = tableBytesFile(u) // SABOTAGED: back to the file order|' \
		internal/codegen/javatable/javatable.go > build/javaruntime-fileorder.gotext
	@[ "$$(grep -c SABOTAGED build/javaruntime-fileorder.gotext)" = "1" ] || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing, or patched more than one line"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/javatable/javatable.go":"%s/build/javaruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/javaruntime-overlay.json
	@go build -overlay=build/javaruntime-overlay.json -o build/schema-javaruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-java/base-sabotage build/runtime-home-java/added-sabotage
	@./build/schema-javaruntime-sabotaged generate --lang java --out build/runtime-home-java/base-sabotage tables/examples
	@./build/schema-javaruntime-sabotaged generate --lang java --out build/runtime-home-java/added-sabotage build/runtime-home-java/src
	@base=$$(cd build/runtime-home-java/base-sabotage && ls *Bytes.java); \
	 added=$$(cd build/runtime-home-java/added-sabotage && ls *Bytes.java); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the byte-access runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (Java): the file-order rule moves the byte-access runtime from $$base to $$added"

# THE GENERATED JAVA IS FORMAT-CANONICAL (issues #424, #547; docs/JAVA-DIALECT.md).
# `google-java-format` is the language's one formatting authority, so an emitter
# that has to be hand-reflowed is an emitter that drifts. The gate holds EVERY
# generated Java unit -- the table corpus above AND the packet-wire units below
# -- to what the pinned formatter writes, in the style the emitters write.
#
# THE FORMATTER IS PINNED beside the JDK, and the recipe never fetches it:
#   google-java-format 1.37.0 (google/google-java-format, the all-deps jar)
#   url:    https://github.com/google/google-java-format/releases/download/v1.37.0/google-java-format-1.37.0-all-deps.jar
#   sha256: 834b2a0c38cb774953322a84b5ca3f2f40dd3156650b3cd44d3b744345962f7a
#           (the jar the fleet toolchains role installed on hetzner1 and hetzner2,
#           ~/sdk/google-java-format-1.37.0/, measured with sha256sum 2026-10-15)
#   Put the jar at dist/google-java-format-1.37.0-all-deps.jar (gitignored) and
#   it runs on the pinned JDK above, with the six --add-exports its README names
#   for JDK 16 and later. Without that jar the gate runs the `google-java-format`
#   launcher on PATH (the benches carry it in ~/sdk/bin), and the probe below
#   holds the launcher to the same release. GOOGLE_JAVA_FORMAT=<launcher>
#   overrides both.
#
# THE STYLE IS --aosp: four-space indent, which is what every emitter writes.
# It is a style option of the same formatter, not a looser tool; the choice and
# the counts behind it are in docs/JAVA-DIALECT.md, and
# `make tables-java-format-census` measures both styles over the same trees.
#
# THE GATE CANNOT SKIP SILENTLY. A formatter that does not resolve at the pinned
# release is a FAILURE, naming the tool, under SCHEMA_REQUIRE_CORPUS=1 or
# JAVA_REQUIRE_FORMATTER=1, and `make test-java` sets the second: the leg's own
# pass never skips its format gate. Run alone without either, it prints SKIP
# naming both. And a glob that matches nothing is not a clean tree: the gate
# counts the files it hands the formatter and refuses below JAVA_CLEAN_MIN.
GOOGLE_JAVA_FORMAT_VERSION := 1.37.0
GOOGLE_JAVA_FORMAT_SHA256 := 834b2a0c38cb774953322a84b5ca3f2f40dd3156650b3cd44d3b744345962f7a
GOOGLE_JAVA_FORMAT_JAR ?= $(CURDIR)/dist/google-java-format-$(GOOGLE_JAVA_FORMAT_VERSION)-all-deps.jar
GOOGLE_JAVA_FORMAT_EXPORTS := $(foreach p,api code file parser tree util,--add-exports=jdk.compiler/com.sun.tools.javac.$(p)=ALL-UNNAMED)
ifneq ($(wildcard $(GOOGLE_JAVA_FORMAT_JAR)),)
GOOGLE_JAVA_FORMAT ?= $(JAVA) $(GOOGLE_JAVA_FORMAT_EXPORTS) -jar $(GOOGLE_JAVA_FORMAT_JAR)
else
GOOGLE_JAVA_FORMAT ?= google-java-format
endif
JAVA_FORMAT_STYLE ?= --aosp
# THE PROBE asks the formatter its version and wants the pinned release's own
# line back, whole. A substring match is not enough: `java -jar` over a MISSING
# jar answers "Unable to access jarfile .../google-java-format-1.37.0-all-deps.jar",
# which carries the version in its path. The first draft of this probe passed
# on exactly that, so the missing-formatter control plants that case.
java_format_probe = $(GOOGLE_JAVA_FORMAT) --version 2>&1 | grep -qx 'google-java-format: Version $(GOOGLE_JAVA_FORMAT_VERSION)'
# deferred, so test-java's own JAVA_REQUIRE_FORMATTER (below) reaches the gate
JAVA_FORMAT_REQUIRED = $(if $(filter 1,$(SCHEMA_REQUIRE_CORPUS) $(JAVA_REQUIRE_FORMATTER)),1,)

# THE PACKET-WIRE UNITS, generated fresh from the emitter rather than read off
# the committed generated/ tree, so the gate watches internal/codegen/java and
# not a checkout that may be stale against it: the examples corpus, the 128-bit
# corpus, the bench corpus, the real-world corpus, the paired bench unit, and
# the packet-defaults fixtures.
build/java-clean-packet/.stamp: bin/schema $(SCHEMAS) $(SCHEMAS128) $(SCHEMAS_BENCH) test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/java.mk
	@rm -rf build/java-clean-packet && mkdir -p build/java-clean-packet
	./bin/schema generate --lang java --out build/java-clean-packet/examples examples
	./bin/schema generate --lang java --out build/java-clean-packet/examples128 examples128
	./bin/schema generate --lang java --out build/java-clean-packet/bench bench/corpus/Bench.schema
	./bin/schema generate --lang java --out build/java-clean-packet/realworld bench/corpus/RealWorld.schema
	./bin/schema generate --lang java --out build/java-clean-packet/paired bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	./bin/schema generate --lang java --out build/java-clean-packet/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang java --out build/java-clean-packet/plain test/packet-defaults/Plain.schema
	@touch $@

# What the gate formats, and the floor under the count: 1872 files on 2026-10-16,
# 1802 in the 33 table units and 70 in the 8 packet units (585 on 2026-10-15,
# before the stream added units). A control points JAVA_CLEAN_PATHS at its
# own tree and lowers the floor; nothing else should.
JAVA_CLEAN_PATHS ?= build/tables-generated-java build/java-clean-packet
JAVA_CLEAN_MIN   ?= 1872
JAVA_CLEAN_LOG   ?= build/java-clean/format.log

.PHONY: tables-java-clean
tables-java-clean: build/tables-generated-java/.stamp build/java-clean-packet/.stamp
	@if ! $(java_format_probe); then \
		if [ -n "$(JAVA_FORMAT_REQUIRED)" ]; then \
			echo "tables-java-clean REFUSES: google-java-format $(GOOGLE_JAVA_FORMAT_VERSION) does not resolve"; \
			echo '  GOOGLE_JAVA_FORMAT = $(GOOGLE_JAVA_FORMAT)'; \
			echo "  make/java.mk names the pinned release and where it comes from; install it under dist/,"; \
			echo "  or override the pin (GOOGLE_JAVA_FORMAT=<launcher>). A gate that skips is a gate with no blade."; \
			exit 1; \
		fi; \
		echo "SKIP tables-java-clean: google-java-format $(GOOGLE_JAVA_FORMAT_VERSION) does not resolve (GOOGLE_JAVA_FORMAT = $(GOOGLE_JAVA_FORMAT)); SCHEMA_REQUIRE_CORPUS=1 or JAVA_REQUIRE_FORMATTER=1 turns this SKIP into a FAILURE"; \
		exit 0; \
	fi; \
	files=$$(find $(JAVA_CLEAN_PATHS) -name '*.java' | sort); \
	n=$$(printf '%s\n' $$files | grep -c '\.java$$'); \
	if [ "$$n" -lt "$(JAVA_CLEAN_MIN)" ]; then \
		echo "FORMAT GATE FAILED: found $$n generated Java files under $(JAVA_CLEAN_PATHS), expected at least $(JAVA_CLEAN_MIN) -- the glob, not the property, is what broke"; exit 1; \
	fi; \
	mkdir -p $$(dirname $(JAVA_CLEAN_LOG)); \
	if ! $(GOOGLE_JAVA_FORMAT) $(JAVA_FORMAT_STYLE) --dry-run --set-exit-if-changed $$files > $(JAVA_CLEAN_LOG) 2>&1; then \
		red=$$(grep -c '\.java$$' $(JAVA_CLEAN_LOG)); \
		echo "tables-java-clean FAILED: $$red of $$n generated Java files drift from google-java-format $(GOOGLE_JAVA_FORMAT_VERSION) $(JAVA_FORMAT_STYLE) (the full list is $(JAVA_CLEAN_LOG)):"; \
		head -40 $(JAVA_CLEAN_LOG); exit 1; \
	fi; \
	echo "tables Java: google-java-format $(GOOGLE_JAVA_FORMAT_VERSION) $(JAVA_FORMAT_STYLE) clean over $$n generated Java files ($(JAVA_CLEAN_PATHS))"
test-java: tables-java-clean
# the leg's own pass REQUIRES the formatter: a test-java that skipped its
# format gate would report a clean leg it never checked.
test-java: JAVA_REQUIRE_FORMATTER = 1
test-java: tables-java-t14
test-java: tables-java-fixed-t18

# THE MEASUREMENT behind the style choice (docs/JAVA-DIALECT.md): the same
# trees under the default Google style and under --aosp, as files red and diff
# lines. It REQUIRES the formatter -- a census of nothing is not a number.
.PHONY: tables-java-format-census
tables-java-format-census: build/tables-generated-java/.stamp build/java-clean-packet/.stamp
	@$(java_format_probe) || \
		{ echo "tables-java-format-census REFUSES: google-java-format $(GOOGLE_JAVA_FORMAT_VERSION) does not resolve (GOOGLE_JAVA_FORMAT = $(GOOGLE_JAVA_FORMAT))"; exit 1; }
	@rm -rf build/java-format-census && mkdir -p build/java-format-census
	@for style in google aosp; do \
		flag=; [ $$style = aosp ] && flag=--aosp; \
		for root in tables-generated-java java-clean-packet; do \
			mkdir -p build/java-format-census/$$style; cp -R build/$$root build/java-format-census/$$style/$$root; \
			files=$$(find build/java-format-census/$$style/$$root -name '*.java' | sort); \
			n=$$(printf '%s\n' $$files | grep -c '\.java$$'); \
			$(GOOGLE_JAVA_FORMAT) $$flag --dry-run $$files > build/java-format-census/$$style-$$root.red 2>&1 || \
				{ echo "tables-java-format-census FAILED: the formatter did not run over $$root ($$style)"; head -20 build/java-format-census/$$style-$$root.red; exit 1; }; \
			red=$$(grep -c '\.java$$' build/java-format-census/$$style-$$root.red); \
			$(GOOGLE_JAVA_FORMAT) $$flag --replace $$files || \
				{ echo "tables-java-format-census FAILED: --replace did not run over $$root ($$style)"; exit 1; }; \
			lines=$$(diff -r build/$$root build/java-format-census/$$style/$$root | grep -c '^[<>]'); \
			echo "census $$style $$root: $$red of $$n files red, $$lines diff lines"; \
		done; \
	done

# THE CONTROLS (make/negative-controls.json). The missing-formatter control is in
# group java; the drift control is in the exclusion list, with its reason, until
# a workflow installs the pinned formatter (it needs the real tool to mean anything).
#
# DRIFT: the emitter writes ONE line of TableBytes.java one space deeper
# (tools/sabotage/table_java_format_drift.go), through `go build -overlay`, and
# tables-java-clean -- the same target, pointed at the sabotaged unit -- must go
# RED naming that file. The guard comes first: the unsabotaged TableBytes.java
# must be clean, or the control would be watching a file that was red anyway.
JAVA_FORMAT_DRIFT := build/java-format-drift
.PHONY: tables-java-clean-drift-negative-control
tables-java-clean-drift-negative-control: build/tables-generated-java/.stamp build/java-clean-packet/.stamp
	@$(java_format_probe) || \
		{ echo "NEGATIVE CONTROL CANNOT RUN: google-java-format $(GOOGLE_JAVA_FORMAT_VERSION) does not resolve (GOOGLE_JAVA_FORMAT = $(GOOGLE_JAVA_FORMAT)), and a drift control without the formatter proves nothing"; exit 1; }
	@rm -rf $(JAVA_FORMAT_DRIFT) && mkdir -p $(JAVA_FORMAT_DRIFT)
	@$(MAKE) --no-print-directory tables-java-clean JAVA_REQUIRE_FORMATTER=1 \
		JAVA_CLEAN_PATHS=build/tables-generated-java/blockhome/TableBytes.java JAVA_CLEAN_MIN=1 \
		JAVA_CLEAN_LOG=$(JAVA_FORMAT_DRIFT)/base.log > $(JAVA_FORMAT_DRIFT)/base.out 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the unsabotaged TableBytes.java is not format-clean, so this control would watch a file that is red anyway"; \
		  cat $(JAVA_FORMAT_DRIFT)/base.out; exit 1; }
	go run ./tools/sabotage -name table-java-format-drift \
		-out $(JAVA_FORMAT_DRIFT)/block.gotext internal/codegen/javatable/block.go
	@printf '{"Replace":{"%s/internal/codegen/javatable/block.go":"%s/$(JAVA_FORMAT_DRIFT)/block.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_FORMAT_DRIFT)/overlay.json
	go build -overlay $(JAVA_FORMAT_DRIFT)/overlay.json -o $(JAVA_FORMAT_DRIFT)/schema ./cmd/schema
	$(JAVA_FORMAT_DRIFT)/schema generate --lang java --out $(JAVA_FORMAT_DRIFT)/generated/blockhome tables/blockhome
	@grep -q '^     private TableBytes() {}' $(JAVA_FORMAT_DRIFT)/generated/blockhome/TableBytes.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter wrote no mis-indented line"; exit 1; }
	@if $(MAKE) --no-print-directory tables-java-clean JAVA_REQUIRE_FORMATTER=1 \
			JAVA_CLEAN_PATHS=$(JAVA_FORMAT_DRIFT)/generated/blockhome/TableBytes.java JAVA_CLEAN_MIN=1 \
			JAVA_CLEAN_LOG=$(JAVA_FORMAT_DRIFT)/drift.log > $(JAVA_FORMAT_DRIFT)/drift.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a mis-indented line left tables-java-clean green"; cat $(JAVA_FORMAT_DRIFT)/drift.out; exit 1; \
	fi
	@grep -q 'tables-java-clean FAILED: 1 of 1 ' $(JAVA_FORMAT_DRIFT)/drift.out && \
	 grep -q 'TableBytes\.java$$' $(JAVA_FORMAT_DRIFT)/drift.log || \
		{ echo "NEGATIVE CONTROL FAILED: tables-java-clean went red, but not on the drifted file"; cat $(JAVA_FORMAT_DRIFT)/drift.out; exit 1; }
	@grep -m1 'tables-java-clean FAILED' $(JAVA_FORMAT_DRIFT)/drift.out
	@echo "negative control: one mis-indented line in the emitted TableBytes.java turns tables-java-clean RED"

# MISSING FORMATTER: the default launcher over a jar that is not there (a path
# carrying the pinned version, the case a substring probe passed on) must be a
# FAILURE naming the tool under SCHEMA_REQUIRE_CORPUS=1; and, the other half of
# the same claim, a launcher that is not there must SKIP naming the variable
# without it. Never a silent pass.
JAVA_FORMAT_MISSING := build/java-format-missing
.PHONY: tables-java-clean-formatter-missing-negative-control
tables-java-clean-formatter-missing-negative-control: build/tables-generated-java/.stamp build/java-clean-packet/.stamp
	@rm -rf $(JAVA_FORMAT_MISSING) && mkdir -p $(JAVA_FORMAT_MISSING)
	@if $(MAKE) --no-print-directory tables-java-clean SCHEMA_REQUIRE_CORPUS=1 JAVA_REQUIRE_FORMATTER= \
			GOOGLE_JAVA_FORMAT="$(JAVA) $(GOOGLE_JAVA_FORMAT_EXPORTS) -jar $(CURDIR)/$(JAVA_FORMAT_MISSING)/google-java-format-$(GOOGLE_JAVA_FORMAT_VERSION)-all-deps.jar" > $(JAVA_FORMAT_MISSING)/required.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: tables-java-clean passed under SCHEMA_REQUIRE_CORPUS=1 with no formatter"; cat $(JAVA_FORMAT_MISSING)/required.out; exit 1; \
	fi
	@grep -q "REFUSES: google-java-format $(GOOGLE_JAVA_FORMAT_VERSION) does not resolve" $(JAVA_FORMAT_MISSING)/required.out || \
		{ echo "NEGATIVE CONTROL FAILED: tables-java-clean went red, but did not name the missing formatter"; cat $(JAVA_FORMAT_MISSING)/required.out; exit 1; }
	@$(MAKE) --no-print-directory tables-java-clean SCHEMA_REQUIRE_CORPUS= JAVA_REQUIRE_FORMATTER= \
		GOOGLE_JAVA_FORMAT=$(CURDIR)/$(JAVA_FORMAT_MISSING)/no-google-java-format > $(JAVA_FORMAT_MISSING)/optional.out 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: without SCHEMA_REQUIRE_CORPUS a missing formatter should SKIP, and the gate failed"; cat $(JAVA_FORMAT_MISSING)/optional.out; exit 1; }
	@grep -q '^SKIP tables-java-clean: .*SCHEMA_REQUIRE_CORPUS=1 or JAVA_REQUIRE_FORMATTER=1' $(JAVA_FORMAT_MISSING)/optional.out || \
		{ echo "NEGATIVE CONTROL FAILED: the skip did not name the variable that makes it a failure"; cat $(JAVA_FORMAT_MISSING)/optional.out; exit 1; }
	@grep -m1 'REFUSES' $(JAVA_FORMAT_MISSING)/required.out
	@echo "negative control: with no google-java-format, tables-java-clean FAILS by name under SCHEMA_REQUIRE_CORPUS=1 and SKIPS by name without it"

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

# and the WHOLE corpus, every unit, under the same flags: every table unit
# above AND every packet-wire unit tables-java-clean formats
# (build/java-clean-packet), each compiled as its own unit so a unit that only
# builds beside another is found. `make tables-java-release` runs it, and
# certify.yml's release-gates job runs that, so it is a gate the tree runs and
# not a name. It counts the units it compiled and refuses below the floor, so a
# glob that matched nothing is a failure and not a pass.
TABLES_JAVA_COMPILE_ALL_MIN ?= 25
.PHONY: tables-java-compile-all
JAVA_COMPILE_ALL_DIRS ?= build/tables-generated-java/*/ build/java-clean-packet/*/
JAVA_COMPILE_ALL_OUT ?= build/tables-java-classes-all
.PHONY: tables-java-compile-all-run
tables-java-compile-all: build/tables-generated-java/.stamp build/java-clean-packet/.stamp
	$(MAKE) --no-print-directory tables-java-compile-all-run
tables-java-compile-all-run:
	@rm -rf $(JAVA_COMPILE_ALL_OUT)
	@n=0; for d in $(JAVA_COMPILE_ALL_DIRS); do \
		unit=$$(basename $$(dirname $$d))-$$(basename $$d); \
		mkdir -p $(JAVA_COMPILE_ALL_OUT)/$$unit; \
		$(JAVAC) --release 17 -Xlint:all -Werror -d $(JAVA_COMPILE_ALL_OUT)/$$unit $$d*.java || \
			{ echo "JAVA COMPILE GATE FAILED: unit $$unit"; exit 1; }; \
		n=$$((n+1)); \
	done; \
	if [ "$$n" -lt "$(TABLES_JAVA_COMPILE_ALL_MIN)" ]; then \
		echo "JAVA COMPILE GATE FAILED: compiled $$n units, expected at least $(TABLES_JAVA_COMPILE_ALL_MIN) -- the glob, not the property, is what broke"; exit 1; \
	fi; \
	echo "tables Java compile gate: all $$n generated units (table and packet) compile under --release 17 -Xlint:all -Werror"

# THE TABLES LEG'S SOURCES, beside the generated units on every classpath that
# runs them.
JAVA_TABLES_LEG_SOURCES := test/java-tables/src/Main.java test/java-tables/src/AgreeAccessors.java \
	test/java-tables/src/BlockLead.java test/java-tables/src/AllocPaths.java test/java-tables/src/Soak.java \
	test/java-tables/src/LayoutContract.java

# THE JAVA TABLES LEG (test/java-tables/src/Main.java): the gates the
# conformance harness does not hold, because none of them is a case — the
# readers' fuzz oracle, the reference extent gate and the byte-order leg.
build/java-tables/.stamp: build/tables-generated-java/.stamp $(JAVA_TABLES_LEG_SOURCES)
	@rm -rf build/java-tables && mkdir -p build/java-tables
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-tables \
		build/tables-generated-java/examples/*.java build/tables-generated-java/pointers/*.java \
		build/tables-generated-java/blobs/*.java build/tables-generated-java/arms/*.java \
		build/tables-generated-java/lists/*.java build/tables-generated-java/block/*.java $(JAVA_TABLES_LEG_SOURCES)
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
JAVA_FUZZ_SABOTAGE_SED := s|if (rows > bytes - offsetOf) { return -8; }|if (rows > bytes - offsetOf) { } // SABOTAGED|
JAVA_FUZZ_SABOTAGE_SED2 := s|if (padding > bytes - used) { return -6; }|if (padding > bytes - used) { } // SABOTAGED|
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
	$(JAVA_FUZZ_SABOTAGE)/schema generate --lang java --out $(JAVA_FUZZ_SABOTAGE)/generated/blobs tables/blobs
	$(JAVA_FUZZ_SABOTAGE)/schema generate --lang java --out $(JAVA_FUZZ_SABOTAGE)/generated/arms tables/arms
	$(JAVA_FUZZ_SABOTAGE)/schema generate --lang java --out $(JAVA_FUZZ_SABOTAGE)/generated/lists tables/lists
	@grep -lq SABOTAGED $(JAVA_FUZZ_SABOTAGE)/generated/block/*Block.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged Open"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_FUZZ_SABOTAGE)/classes \
		$(JAVA_FUZZ_SABOTAGE)/generated/*/*.java test/java-tables/src/Main.java test/java-tables/src/AllocPaths.java
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

# THE BASE-ALIGNMENT GATE (docs/PORTING.md I5, #387): every committed block
# image at every lead in 0..64, and the alignment rule exactly — 0 and 64 open
# (and a row reads), 1..63 refuse. §19.2 checks the base's alignment and this
# leg carries it as the OFFSET a caller passes beside its array, so a stated
# fact nothing checks would be a comment rather than a check.
.PHONY: tables-java-block-lead
tables-java-block-lead: build/java-tables/.stamp
	$(JAVA) -cp build/java-tables BlockLead testdata/wire/tables/block_render.bin testdata/wire/tables/block_padded.bin

# ITS NEGATIVE CONTROL, on the EMITTER for the fuzz control's reason: drop the
# residue check and the gate must go red at lead 1 naming the image.
JAVA_BLOCK_LEAD_SABOTAGE := build/java-block-lead-sabotage
JAVA_BLOCK_LEAD_SABOTAGE_SED := s|if ((offset %% TableBlockInfo.alignment) != 0) { return -7; }|if ((offset %% TableBlockInfo.alignment) != 0) { } // SABOTAGED|
.PHONY: tables-java-block-lead-negative-control
tables-java-block-lead-negative-control: build/java-tables/.stamp
	@rm -rf $(JAVA_BLOCK_LEAD_SABOTAGE) && mkdir -p $(JAVA_BLOCK_LEAD_SABOTAGE)
	@sed '$(JAVA_BLOCK_LEAD_SABOTAGE_SED)' internal/codegen/javatable/block.go > $(JAVA_BLOCK_LEAD_SABOTAGE)/javatable-block.go.txt
	@cmp -s internal/codegen/javatable/block.go $(JAVA_BLOCK_LEAD_SABOTAGE)/javatable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Java block-lead sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/javatable/block.go":"%s/$(JAVA_BLOCK_LEAD_SABOTAGE)/javatable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_BLOCK_LEAD_SABOTAGE)/overlay.json
	go build -overlay $(JAVA_BLOCK_LEAD_SABOTAGE)/overlay.json -o $(JAVA_BLOCK_LEAD_SABOTAGE)/schema ./cmd/schema
	$(JAVA_BLOCK_LEAD_SABOTAGE)/schema generate --lang java --out $(JAVA_BLOCK_LEAD_SABOTAGE)/generated/block tables/block
	@grep -lq SABOTAGED $(JAVA_BLOCK_LEAD_SABOTAGE)/generated/block/*Block.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged Open"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_BLOCK_LEAD_SABOTAGE)/classes \
		$(JAVA_BLOCK_LEAD_SABOTAGE)/generated/block/*.java test/java-tables/src/BlockLead.java
	@if $(JAVA) -cp $(JAVA_BLOCK_LEAD_SABOTAGE)/classes BlockLead testdata/wire/tables/block_render.bin testdata/wire/tables/block_padded.bin \
			> $(JAVA_BLOCK_LEAD_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a block reader with no alignment check left the base-alignment gate green"; \
		cat $(JAVA_BLOCK_LEAD_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "block_render at lead 1 answered open" $(JAVA_BLOCK_LEAD_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the alignment at lead 1"; \
		  cat $(JAVA_BLOCK_LEAD_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_BLOCK_LEAD_SABOTAGE)/log
	@echo "negative control: one missing alignment check in the Java block Open turns the base-alignment gate RED at lead 1"

# THE ACCESSOR/DESCRIPTOR AGREEMENT GATE (docs/PORTING.md J1). Every row of
# every committed block image and every node of the cook is read twice — once
# through the generated accessor named for the field, once through the
# descriptor's own offset, storage kind and element size — and the two must
# agree. Two independent derivations of one layout, held against each other, so
# a moved accessor or a moved descriptor offset is seen without a pinned dump
# that happens to cover the field.
.PHONY: tables-java-accessor-agreement
tables-java-accessor-agreement: build/java-tables/.stamp build/cook-open/.stamp
	$(JAVA) -cp build/java-tables AgreeAccessors \
		testdata/wire/tables/block_render.bin testdata/wire/tables/block_padded.bin build/cook-open/Scene.cook

# ITS TWO NEGATIVE CONTROLS, built the way the fuzz control builds its overlay:
# the sabotage edits a COPY of rows.go through `go build -overlay`, the
# sabotaged compiler regenerates the block and cook units, and the agreement
# gate must go red — the first on a moved scalar accessor, the second on a moved
# pointer slot — each naming the row and the field.
JAVA_AGREE_ACCESSOR_SABOTAGE := build/java-agree-accessor-sabotage
JAVA_AGREE_SLOT_SABOTAGE := build/java-agree-slot-sabotage
.PHONY: tables-java-accessor-negative-control tables-java-slot-negative-control
tables-java-accessor-negative-control: build/cook-open/.stamp
	@rm -rf $(JAVA_AGREE_ACCESSOR_SABOTAGE) && mkdir -p $(JAVA_AGREE_ACCESSOR_SABOTAGE)
	go run ./tools/sabotage -name java-agree-accessor-scalar \
		-out $(JAVA_AGREE_ACCESSOR_SABOTAGE)/rows.gotext internal/codegen/javatable/rows.go
	@grep -q SABOTAGED $(JAVA_AGREE_ACCESSOR_SABOTAGE)/rows.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/javatable/rows.go":"%s/$(JAVA_AGREE_ACCESSOR_SABOTAGE)/rows.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_AGREE_ACCESSOR_SABOTAGE)/overlay.json
	go build -overlay $(JAVA_AGREE_ACCESSOR_SABOTAGE)/overlay.json -o $(JAVA_AGREE_ACCESSOR_SABOTAGE)/schema ./cmd/schema
	$(JAVA_AGREE_ACCESSOR_SABOTAGE)/schema generate --lang java --out $(JAVA_AGREE_ACCESSOR_SABOTAGE)/generated/block tables/block
	$(JAVA_AGREE_ACCESSOR_SABOTAGE)/schema generate --lang java --out $(JAVA_AGREE_ACCESSOR_SABOTAGE)/generated/pointers tables/pointers
	@grep -lq SABOTAGED $(JAVA_AGREE_ACCESSOR_SABOTAGE)/generated/block/*.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged accessor"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_AGREE_ACCESSOR_SABOTAGE)/classes \
		$(JAVA_AGREE_ACCESSOR_SABOTAGE)/generated/block/*.java $(JAVA_AGREE_ACCESSOR_SABOTAGE)/generated/pointers/*.java \
		test/java-tables/src/AgreeAccessors.java
	@if $(JAVA) -cp $(JAVA_AGREE_ACCESSOR_SABOTAGE)/classes AgreeAccessors \
			testdata/wire/tables/block_render.bin testdata/wire/tables/block_padded.bin build/cook-open/Scene.cook \
			> $(JAVA_AGREE_ACCESSOR_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a moved scalar accessor left the agreement gate green"; \
		cat $(JAVA_AGREE_ACCESSOR_SABOTAGE)/log; exit 1; \
	fi
	@grep -qE "disagree|is not the descriptor's|has a descriptor and no accessor|has an accessor and no descriptor" $(JAVA_AGREE_ACCESSOR_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the disagreement"; \
		  cat $(JAVA_AGREE_ACCESSOR_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_AGREE_ACCESSOR_SABOTAGE)/log
	@echo "negative control: a scalar accessor moved four bytes turns the agreement gate RED"
tables-java-slot-negative-control: build/cook-open/.stamp
	@rm -rf $(JAVA_AGREE_SLOT_SABOTAGE) && mkdir -p $(JAVA_AGREE_SLOT_SABOTAGE)
	go run ./tools/sabotage -name java-agree-slot-offset \
		-out $(JAVA_AGREE_SLOT_SABOTAGE)/rows.gotext internal/codegen/javatable/rows.go
	@grep -q SABOTAGED $(JAVA_AGREE_SLOT_SABOTAGE)/rows.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/javatable/rows.go":"%s/$(JAVA_AGREE_SLOT_SABOTAGE)/rows.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(JAVA_AGREE_SLOT_SABOTAGE)/overlay.json
	go build -overlay $(JAVA_AGREE_SLOT_SABOTAGE)/overlay.json -o $(JAVA_AGREE_SLOT_SABOTAGE)/schema ./cmd/schema
	$(JAVA_AGREE_SLOT_SABOTAGE)/schema generate --lang java --out $(JAVA_AGREE_SLOT_SABOTAGE)/generated/block tables/block
	$(JAVA_AGREE_SLOT_SABOTAGE)/schema generate --lang java --out $(JAVA_AGREE_SLOT_SABOTAGE)/generated/pointers tables/pointers
	@grep -lq SABOTAGED $(JAVA_AGREE_SLOT_SABOTAGE)/generated/pointers/*Row.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged slot"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_AGREE_SLOT_SABOTAGE)/classes \
		$(JAVA_AGREE_SLOT_SABOTAGE)/generated/block/*.java $(JAVA_AGREE_SLOT_SABOTAGE)/generated/pointers/*.java \
		test/java-tables/src/AgreeAccessors.java
	@if $(JAVA) -cp $(JAVA_AGREE_SLOT_SABOTAGE)/classes AgreeAccessors \
			testdata/wire/tables/block_render.bin testdata/wire/tables/block_padded.bin build/cook-open/Scene.cook \
			> $(JAVA_AGREE_SLOT_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a moved pointer slot left the agreement gate green"; \
		cat $(JAVA_AGREE_SLOT_SABOTAGE)/log; exit 1; \
	fi
	@grep -qE "disagree|is not the descriptor's|has a descriptor and no accessor|has an accessor and no descriptor" $(JAVA_AGREE_SLOT_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the disagreement"; \
		  cat $(JAVA_AGREE_SLOT_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_AGREE_SLOT_SABOTAGE)/log
	@echo "negative control: a pointer slot moved eight bytes turns the agreement gate RED"
test-java: tables-java-accessor-agreement tables-java-accessor-negative-control tables-java-slot-negative-control

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

# THE MESSAGE FORM, form byte 2 (docs/SPEC-TABLES.md §3.3;
# internal/codegen/javatable/messageform_announce_test.go,
# internal/codegen/javatable/messageform_batch_test.go). The announcement wire
# (TableVocabulary) and the bitpacked message batch (TableMessage and
# <Type>Message) round-trip against the C++ reference bytes for the backend,
# vocab and vocab9 corpora, land versioned batches across v1/v2, and refuse
# every malformed and out-of-range message batch by name.
.PHONY: tables-java-message-batch
tables-java-message-batch:
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -run 'TestMessageFormBatchJava|TestMessageFormAnnounceJava'
	@echo 'tables Java message form: announcement and batch round-trip, versioning and refusals pass'

# THE FIXED-FORM ROADMAP T12 GATE (union-field-arms/java/valid-data,
# payload-free-arms/java/valid-data, union-arrays/java/valid-data,
# optional-arrays/java/valid-data; docs/roadmap.sexp fixed-tables). Each
# subtest generates through the registered Java target (`schema generate --lang
# java`), compiles and runs the capability probe, and compares against the C++
# reference bytes in build/fixedform-corpus (fu1.bin, fu2.bin).
.PHONY: tables-java-fixed-roadmap
tables-java-fixed-roadmap: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -v -run 'TestFixedRoadmapJavaT12BatchesUnions'
	@echo 'tables Java fixed roadmap (T12): union arms, payload-free arms, union arrays and optional arrays pass through the registered target'

# THE FIXED TABLES' SHARED BYTE ORACLE AND HOSTILE INPUT (java/W13, java/R27,
# java/P3, java/P4; docs/roadmap.sexp fixed-tables). The driven functional tier
# reads the C++ reference's own byte oracle out of build/fixedform-corpus and
# runs the generated Java reader under the JVM as the sanitizer: W13 compares
# the header hash and layout bytes in every reference file the Java leg emits
# against the leg's own; R27 loads each manifest row the target emits through
# the generated load and compares value for value; P3 sweeps every byte of a
# record, one bit at a time, demanding each load refuse, read malformed, or
# land values — never a fourth answer; P4 feeds a record body the wrong compiled
# plan and demands the read go red. This target sets SCHEMA_REQUIRE_CORPUS=1
# so a missing oracle turns the skip into a FAILURE.
.PHONY: tables-java-fixed-t18
tables-java-fixed-t18: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -v -run 'TestFixedRoadmapJavaT18OracleHostile'
	@echo 'tables Java fixed t18: the shared byte oracle and hostile input rows pass (java/W13, java/R27, java/P3, java/P4)'

# THE WHOLE JAVATABLE PACKAGE, ONCE, WITH THE CORPUS ALREADY BUILT. Ten of the
# package's test files read the C++ reference's byte oracle out of
# build/fixedform-corpus (corpusDir in fixedversioning_test.go), which only
# `tables-fixedform-corpus` writes, so on a fresh clone every TestFixed* goes
# red until that target is run by hand. `test-java` does depend on the corpus but
# runs only named -run subsets, never the whole package; card gates and the
# heavy reads run the whole package, and this is the one target that builds the
# corpus first and then runs it all with SCHEMA_REQUIRE_CORPUS=1, so a missing
# oracle can never pass silently.
.PHONY: tables-java-package
tables-java-package: tables-fixedform-corpus
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 900s
	@echo 'tables Java package: the whole javatable package passes with the fixed-form corpus built first'


# THE COMPRESSED-FLOAT FP-CONTRACT GATE (java/R30, T14): the witness that the
# compressed float's dequantize is the SPEC's order in float32 — one rounding
# per operation, never a fused multiply-add (SPEC.md "Floating-point
# discipline"). It runs the leg's own generated writer and reader end to end
# with an off-grid float32 and asserts the unfused witness bits 0xbf4ccccc.
.PHONY: tables-java-t14
tables-java-t14:
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test -count=1 -timeout 600s -v ./internal/codegen/javatable/ -run '^TestFixedRoadmapJavaT14CompressedFloats$$'
	@echo 'tables Java T14: the compressed float rides as the float, unfused dequantize (java/R30)'

# I13 (docs/PORTING.md, issue #419): the Java leg's text differential against a
# third implementation. A Java probe draws 200 random float instances of a
# VARIABLE-form root (seed 24845619678, splitmix64; scalar float32/float64, a
# nested table, []float32, []float64 and a list of tables) and writes each as
# (wire, text) with the generated save and toJson; the Go engine
# (internal/tabletext, internal/tablepack) writes its own text from the leg's
# wire, byte-compared, packs the leg's text back to wire, byte-compared, and the
# leg's fromJson reads the engine's text back to the same wire
# (TestTextDifferentialJava). The control, run inside this target, caps the
# emitted float32 window at six significant digits in memory and must go red on
# the TEXT comparison (TestTextDifferentialJavaNegativeControl).
# SCHEMA_REQUIRE_CORPUS=1 makes a missing JDK a FAILURE rather than a skip.
.PHONY: tables-java-json-differential tables-java-json-differential-negative-control
tables-java-json-differential:
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test -count=1 -timeout 600s -v ./internal/codegen/javatable/ -run '^(TestTextDifferentialJava|TestTextDifferentialJavaNegativeControl)$$'
	@echo 'tables Java text differential: 200 random float instances agree with the Go engine byte for byte, both directions, and a six-digit float32 writer is red'
tables-java-json-differential-negative-control:
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test -count=1 -timeout 600s -v ./internal/codegen/javatable/ -run '^TestTextDifferentialJavaNegativeControl$$'
	@echo 'tables Java text differential negative control: a six-digit float32 writer is red on the text comparison'

test-java: tables-java-json-differential

# THE BLOCK BUILDER AND COOK WRITER GATE (docs/SPEC-TABLES.md §7.6, §19.1):
# the Java block builder produces the committed block_render and block_padded
# images byte-for-byte, and the Java cook writer produces the committed
# little-endian and big-endian cooked images byte-for-byte.
.PHONY: tables-java-block-build
tables-java-block-build:
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -v -run '^TestBlockBuildAndCookWriteJava$$'
	@echo 'tables Java block builder and cook writer: block_render, block_padded and cook-write match the committed reference bytes'

test-java: tables-java-block-build

# THE VARIABLE-CLASS GATE (docs/SPEC-TABLES.md §2, §3, §16), the Java twin of
# `tables-go-variable-class`: the variable form's codecs for the pointered,
# blob and list corpora round-trip against the committed reference bytes. It is
# the variable class beyond the card constructs — *bytes blobs, unbounded
# arrays of scalars/strings/enums, arrays of pointers and pointers outside a
# list element, the 64-bit id-table escape kind and the §16.7 sharing text.
# `SCHEMA_REQUIRE_CORPUS=1` turns a missing-oracle skip into a FAILURE, so under
# this name a missing oracle can never pass silently.
.PHONY: tables-java-variable-class
tables-java-variable-class:
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -run 'TestVariableClassBeyondCardConstructsJava|TestUnboundedArraysRoundTripJava|TestEscapeKind31SkippedAndCountedJava'
	@echo 'tables Java variable class: the pointered, blob and list corpora round-trip against the committed reference bytes'

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

# THE KEYED ACCESSOR, Java side (docs/SPEC-TABLES.md §2.4, docs/PORTING.md M5).
# An enum-keyed array indexes by KEY: key k at slot k-1, None (0) and a key past
# E.Max refused BY NAME in every build, with one unsigned compare and never
# `assert`. TestKeyedArrayIndexingJava compiles a three-variant probe under the
# consumer's flags and holds both ends, the reach of Max and the placement on
# the saved wire, under -da and -ea; TestKeyedFormWireCodeMatchesUnkeyedInTree
# holds that the key form is ADDITIVE, in the same tree with no committed pin:
# every keyed unit generated keyed and with its keys lowered to `[E.Max]T`,
# both compiled, one corpus through each, the fixed bodies byte-identical and
# cross-decoding, and each form's message and the keyed variable wire held
# byte for byte to the Go codec's encoding of the same value. Under
# SCHEMA_REQUIRE_CORPUS=1 a missing JDK is a failure, not a skip, and EACH test
# must run to its own PASS: a name the -run pattern no longer matches would
# otherwise leave the gate green having run nothing.
.PHONY: tables-java-keyed tables-java-keyed-negative-control
tables-java-keyed:
	@mkdir -p build/java-keyed
	@SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -v -run 'TestKeyedArrayIndexingJava|TestKeyedFormWireCodeMatchesUnkeyedInTree' \
		> build/java-keyed/log 2>&1 || { tail -40 build/java-keyed/log; exit 1; }
	@if grep -q -- '--- SKIP' build/java-keyed/log; then \
		echo "tables-java-keyed: a keyed test SKIPPED under SCHEMA_REQUIRE_CORPUS=1"; grep -- '--- SKIP' build/java-keyed/log; exit 1; \
	fi
	@grep -q -- '--- PASS: TestKeyedArrayIndexingJava ' build/java-keyed/log || \
		{ echo "tables-java-keyed: TestKeyedArrayIndexingJava did not run to a PASS"; tail -20 build/java-keyed/log; exit 1; }
	@grep -q -- '--- PASS: TestKeyedFormWireCodeMatchesUnkeyedInTree ' build/java-keyed/log || \
		{ echo "tables-java-keyed: TestKeyedFormWireCodeMatchesUnkeyedInTree did not run to a PASS"; tail -20 build/java-keyed/log; exit 1; }
	@echo 'tables Java: the keyed accessor refuses None and Max+1 by name and places key k at slot k-1 (§2.4)'
	@echo 'tables Java: the keyed form is additive, and its message and variable wires match the Go codec (§2.4, §3.2)'
test-java: tables-java-keyed

# ITS NEGATIVE CONTROL, one end at a time. The overlay rewrites the ONE compare
# in the emitter (javaKeyRefused, fixedform.go) — first to `key < 1`, which
# drops the upper bound, then to `key > Max`, which drops the lower — and each
# must red TestKeyedArrayIndexingJava ON ITS OWN END AND ONLY THERE: a control
# that reds both ends at once could not tell a symmetric guard from a broken one.
JAVA_KEYED_SABOTAGE := build/java-keyed-sabotage
JAVA_KEYED_COMPARE := "Integer.compareUnsigned(%s - 1, %d) >= 0", key, max)
tables-java-keyed-negative-control:
	@rm -rf $(JAVA_KEYED_SABOTAGE) && mkdir -p $(JAVA_KEYED_SABOTAGE)
	@set -e; for end in upper lower; do \
		case $$end in \
		upper) to='"%s < 1", key) // SABOTAGED: the upper bound is gone'; red='keyed Max+1 end:'; green='keyed None end:' ;; \
		lower) to='"%s > %d", key, max) // SABOTAGED: the lower bound is gone'; red='keyed None end:'; green='keyed Max+1 end:' ;; \
		esac; \
		d=$(JAVA_KEYED_SABOTAGE)/$$end; mkdir -p $$d; \
		sed "s|$(subst ",\",$(JAVA_KEYED_COMPARE))|$$to|" internal/codegen/javatable/fixedform.go > $$d/fixedform.gotext; \
		grep -q SABOTAGED $$d/fixedform.gotext || { echo "NEGATIVE CONTROL FAILED: the $$end sabotage patched nothing"; exit 1; }; \
		printf '{"Replace":{"%s/internal/codegen/javatable/fixedform.go":"%s/%s/fixedform.gotext"}}\n' \
			"$(CURDIR)" "$(CURDIR)" "$$d" > $$d/overlay.json; \
		if SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
				go test -overlay=$$d/overlay.json ./internal/codegen/javatable/ -count=1 -timeout 600s \
				-run 'TestKeyedArrayIndexingJava' > $$d/log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: dropping the $$end bound left the keyed accessor green"; tail -20 $$d/log; exit 1; \
		fi; \
		grep -q -- "$$red" $$d/log || { echo "NEGATIVE CONTROL FAILED: the $$end sabotage went red, but not on its own end"; tail -20 $$d/log; exit 1; }; \
		if grep -q -- "$$green" $$d/log; then echo "NEGATIVE CONTROL FAILED: the $$end sabotage also redded the other end"; grep -m3 -- "$$green" $$d/log; exit 1; fi; \
		if grep -q -- "keyed in-range" $$d/log; then echo "NEGATIVE CONTROL FAILED: the $$end sabotage moved an in-range key"; grep -m3 -- "keyed in-range" $$d/log; exit 1; fi; \
		grep -m1 -- "$$red" $$d/log; \
		echo "negative control: dropping the $$end bound reds the keyed accessor on that end only"; \
	done

# FINITE TABLE FLOATS, Java side (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27:
# "Tables go finite-only too, same rule as packets"): every read refuses a NaN
# or an infinity on its raw bits as non_finite_float, the tolerant one too,
# naming the field (variable, retaining, message batch, fixed form before its
# bounds); a retained unknown kind 10/11 entry is tested like a known one; every
# writer refuses one (variable, retaining, message, fixed, the cook's plan); a
# block or cook record's float accessor refuses one by name; -0 and the
# subnormals ride bit for bit. The gate is TestJavaFiniteFloats, one line per
# check. Its NEGATIVE CONTROL removes each check from the generated code in turn
# (the variable read, the retained unknown, the message read, the fixed read,
# the variable writer, the accessor) and requires exactly that check's lines
# red and every other line green. Under SCHEMA_REQUIRE_CORPUS=1 a missing JDK
# is a failure, not a skip.
.PHONY: tables-java-finite-float tables-java-finite-float-negative-control
tables-java-finite-float:
	@mkdir -p build/java-finite
	@SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -v -run '^TestJavaFiniteFloats$$' \
		> build/java-finite/log 2>&1 || { tail -40 build/java-finite/log; exit 1; }
	@grep -q -- '--- PASS: TestJavaFiniteFloats ' build/java-finite/log || \
		{ echo "tables-java-finite-float: TestJavaFiniteFloats did not run to a PASS"; tail -20 build/java-finite/log; exit 1; }
	@echo 'tables Java: every table read and writer refuses a NaN or an infinity as non_finite_float, -0 and subnormals exact (§3.3)'
test-java: tables-java-finite-float

tables-java-finite-float-negative-control:
	@mkdir -p build/java-finite
	@SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) \
		go test ./internal/codegen/javatable/ -count=1 -timeout 600s -v -run '^TestJavaFiniteFloatNegativeControl$$' \
		> build/java-finite/control.log 2>&1 || { tail -40 build/java-finite/control.log; exit 1; }
	@grep -q -- '--- PASS: TestJavaFiniteFloatNegativeControl ' build/java-finite/control.log || \
		{ echo "NEGATIVE CONTROL FAILED: TestJavaFiniteFloatNegativeControl did not run to a PASS"; tail -20 build/java-finite/control.log; exit 1; }
	@grep 'negative control: with the' build/java-finite/control.log

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

# java_tables_sabotage DIR,GOFILES,SEDVAR,SOURCES builds a compiler over COPIES of the
# emitter files GOFILES with the sed script in the variable SEDVAR applied (each
# copy must change), regenerates the tables leg's six units with it, and
# compiles them with SOURCES (the leg's, or none) into DIR/classes. The controls below are
# each one call of it and one run of the gate they break.
define java_tables_sabotage
	@rm -rf $(1) && mkdir -p $(1)
	@printf '{"Replace":{' > $(1)/overlay.json; sep=; for f in $(2); do \
		c=$(CURDIR)/$(1)/$$(echo $$f | tr / _).txt; sed '$($(3))' $$f > $$c; \
		if cmp -s $$f $$c; then echo "NEGATIVE CONTROL: the sabotage did not apply to $$f"; exit 1; fi; \
		printf '%s"%s/%s":"%s"' "$$sep" "$(CURDIR)" "$$f" "$$c" >> $(1)/overlay.json; sep=,; \
	done; printf '}}\n' >> $(1)/overlay.json
	go build -overlay $(1)/overlay.json -o $(1)/schema ./cmd/schema
	@for u in examples pointers blobs arms lists block; do \
		$(1)/schema generate --lang java --out $(1)/generated/$$u tables/$$u > $(1)/generate.log 2>&1 || \
			{ cat $(1)/generate.log; exit 1; }; done
	@grep -rlq SABOTAGED $(1)/generated || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted nothing sabotaged"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(1)/classes $(1)/generated/*/*.java $(4)
endef

# ITS TWO NEGATIVE CONTROLS, one per check the gate holds.
#
# tables-java-order-negative-control (docs/PORTING.md I7): the cook Open's
# ORDER-WORD check planted out. The cook of the other order still refuses on
# its magic, so only the forged cook — magic intact, the other order word —
# can see it, and the gate must go red on exactly that cook.
#
# tables-java-order-le-negative-control (docs/PORTING.md M12): TableBytes.i64
# read through a ByteBuffer in the JVM's default order instead of composed
# little-endian byte by byte. The magic then reads back reversed and the gate
# must go red on this reader's OWN cook.
JAVA_ORDER_WORD_SABOTAGE := build/java-order-word-sabotage
JAVA_ORDER_WORD_SED := s@if (TableBytes.i64(data, offset + 16) != TableCookInfo.byteOrder) { return -1; }@if (false) { return -1; } // SABOTAGED: no order-word check@
JAVA_ORDER_LE_SABOTAGE := build/java-order-le-sabotage
JAVA_ORDER_LE_SED := s@return (i32(data, at) & 0xffffffffL) | ((long) i32(data, at + 4) << 32);@return java.nio.ByteBuffer.wrap(data).getLong(at); // SABOTAGED: the JVM default order@
.PHONY: tables-java-order-negative-control tables-java-order-le-negative-control
tables-java-order-negative-control: build/cook-open/.stamp
	$(call java_tables_sabotage,$(JAVA_ORDER_WORD_SABOTAGE),internal/codegen/javatable/cook.go,JAVA_ORDER_WORD_SED,$(JAVA_TABLES_LEG_SOURCES))
	@if $(JAVA) -cp $(JAVA_ORDER_WORD_SABOTAGE)/classes Main order build/cook-open/Scene.cook build/cook-open/Scene-be.cook \
			> $(JAVA_ORDER_WORD_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a cook Open with no order-word check left the order gate green"; \
		cat $(JAVA_ORDER_WORD_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "the order word was not checked" $(JAVA_ORDER_WORD_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the forged order word"; \
		  cat $(JAVA_ORDER_WORD_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_ORDER_WORD_SABOTAGE)/log
	@echo "negative control: the order-word check planted out turns the order gate RED on the forged cook alone"
tables-java-order-le-negative-control: build/cook-open/.stamp
	$(call java_tables_sabotage,$(JAVA_ORDER_LE_SABOTAGE),internal/codegen/javatable/block.go,JAVA_ORDER_LE_SED,$(JAVA_TABLES_LEG_SOURCES))
	@if $(JAVA) -cp $(JAVA_ORDER_LE_SABOTAGE)/classes Main order build/cook-open/Scene.cook build/cook-open/Scene-be.cook \
			> $(JAVA_ORDER_LE_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a reader in the JVM's default order left the order gate green"; \
		cat $(JAVA_ORDER_LE_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "a cook of this reader's own byte order did not open" $(JAVA_ORDER_LE_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on this reader's own cook"; \
		  cat $(JAVA_ORDER_LE_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_ORDER_LE_SABOTAGE)/log
	@echo "negative control: a word read in the JVM's default order turns the order gate RED on this reader's own cook"
test-java: tables-java-order-negative-control tables-java-order-le-negative-control

# THE LAYOUT CONTRACT GATE (docs/PORTING.md M11, docs/SPEC-TABLES.md §19.3).
# TableBlockLayout and TableCookLayout hold each record's accessor offsets
# against its descriptor offsets once, before the first open. The gate opens
# this build's block and cook, then compiles a copy of the block and pointers
# units with ONE accessor offset moved four bytes (RenderCameraRow's and
# SceneRow's first field) and requires each open to THROW naming the record
# before any row is read. tables-java-layout-planted is that second half alone,
# over the units in JAVA_LAYOUT_GEN.
JAVA_LAYOUT_BLOCK := testdata/wire/tables/block_render.bin
JAVA_LAYOUT_GEN ?= build/tables-generated-java
JAVA_LAYOUT_DIR ?= build/java-layout
.PHONY: tables-java-layout-contract tables-java-layout-planted tables-java-layout-contract-negative-control
tables-java-layout-contract: build/java-tables/.stamp build/cook-open/.stamp
	$(JAVA) -cp build/java-tables LayoutContract clean $(JAVA_LAYOUT_BLOCK) build/cook-open/Scene.cook
	$(MAKE) --no-print-directory tables-java-layout-planted JAVA_LAYOUT_GEN=build/tables-generated-java JAVA_LAYOUT_DIR=build/java-layout
tables-java-layout-planted:
	@rm -rf $(JAVA_LAYOUT_DIR) && mkdir -p $(JAVA_LAYOUT_DIR)/planted
	@cp -R $(JAVA_LAYOUT_GEN)/block $(JAVA_LAYOUT_GEN)/pointers $(JAVA_LAYOUT_DIR)/planted/
	@for f in block/RenderCameraRow.java pointers/SceneRow.java; do \
		sed -i.orig 's/static final int\[\] offsets = {0, /static final int[] offsets = {4, /' $(JAVA_LAYOUT_DIR)/planted/$$f; \
		if cmp -s $(JAVA_LAYOUT_DIR)/planted/$$f $(JAVA_LAYOUT_DIR)/planted/$$f.orig; then \
			echo "LAYOUT GATE: the planted offset did not apply to $$f"; exit 1; fi; \
		rm $(JAVA_LAYOUT_DIR)/planted/$$f.orig; done
	$(JAVAC) --release 17 -nowarn -d $(JAVA_LAYOUT_DIR)/classes \
		$(JAVA_LAYOUT_DIR)/planted/*/*.java test/java-tables/src/LayoutContract.java
	$(JAVA) -cp $(JAVA_LAYOUT_DIR)/classes LayoutContract planted $(JAVA_LAYOUT_BLOCK) build/cook-open/Scene.cook
test-java: tables-java-layout-contract tables-java-layout-contract-negative-control

# ITS NEGATIVE CONTROL: both halves' comparison planted out of the emitters
# (block.go's TableBlockLayout and cook.go's TableCookLayout), and the planted
# half of the gate must go red: the moved offset opens.
JAVA_LAYOUT_SABOTAGE := build/java-layout-sabotage
JAVA_LAYOUT_SED := s@if (accessors != descriptors) {@if (false) { // SABOTAGED: the layout contract compares nothing@
tables-java-layout-contract-negative-control: build/cook-open/.stamp
	$(call java_tables_sabotage,$(JAVA_LAYOUT_SABOTAGE),internal/codegen/javatable/block.go internal/codegen/javatable/cook.go,JAVA_LAYOUT_SED,$(JAVA_TABLES_LEG_SOURCES))
	@if $(MAKE) --no-print-directory tables-java-layout-planted JAVA_LAYOUT_GEN=$(JAVA_LAYOUT_SABOTAGE)/generated \
			JAVA_LAYOUT_DIR=$(JAVA_LAYOUT_SABOTAGE)/layout > $(JAVA_LAYOUT_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a layout contract that compares nothing left the layout gate green"; \
		cat $(JAVA_LAYOUT_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "compiled with a moved accessor offset answered open" $(JAVA_LAYOUT_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the moved offset opening"; \
		  cat $(JAVA_LAYOUT_SABOTAGE)/log; exit 1; }
	@grep -m1 "FAILED:" $(JAVA_LAYOUT_SABOTAGE)/log
	@echo "negative control: a layout contract that compares nothing turns the layout gate RED: the moved offset opens"

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
	$(JAVA_EXTENT_SABOTAGE)/schema generate --lang java --out $(JAVA_EXTENT_SABOTAGE)/generated/blobs tables/blobs
	$(JAVA_EXTENT_SABOTAGE)/schema generate --lang java --out $(JAVA_EXTENT_SABOTAGE)/generated/arms tables/arms
	$(JAVA_EXTENT_SABOTAGE)/schema generate --lang java --out $(JAVA_EXTENT_SABOTAGE)/generated/lists tables/lists
	@grep -lq SABOTAGED $(JAVA_EXTENT_SABOTAGE)/generated/pointers/SceneCook.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged at"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_EXTENT_SABOTAGE)/classes \
		$(JAVA_EXTENT_SABOTAGE)/generated/*/*.java test/java-tables/src/Main.java test/java-tables/src/AllocPaths.java
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

# THE ALLOCATION GATE (docs/PORTING.md I1, M1, I14). The gate reads the JDK's
# feature version and the JVM's per-thread allocation counter, and refuses to
# certify on any JDK other than the pin — or with no counter at all — without
# measuring. SCHEMA_JAVA_ALLOC_PINNED_FEATURE overrides the pin for the negative
# control; SCHEMA_JAVA_ALLOC_ANY_JDK=1 is the escape hatch: it reports and does
# not certify. On the pin it measures the ROWS (test/java-tables/src/AllocPaths.java),
# each a real path through the generated code over a committed file, each
# warmed until its per-iteration count settles and gated on its ceiling:
#   1 block-walk    open + every row of block_render.bin: one handle, 0 per row
#   2 cook-walk     open + every node of graph_deep.cook: one handle, 0 per node
#   3 load-measure  the variable form's loadMeasure over the wires below
#   4 load, save    over the same wires, at the measured floor, reason printed
JAVA_ALLOC_BLOCK := testdata/wire/tables/block_render.bin
JAVA_ALLOC_COOK := testdata/conformance/tables/cook-write/graph_deep.cook
JAVA_ALLOC_WIRES := testdata/wire/tables/graph_tree.bin testdata/wire/tables/graph_deep.bin \
	testdata/wire/tables/graph_shared.bin testdata/wire/tables/blob_small.bin \
	testdata/wire/tables/keyed_config.bin testdata/wire/tables/arms_gate.bin
JAVA_ALLOC_ARGS := $(JAVA_ALLOC_BLOCK) $(JAVA_ALLOC_COOK) $(JAVA_ALLOC_WIRES)
.PHONY: tables-java-allocator tables-java-allocator-runtime-negative-control tables-java-alloc-negative-control
tables-java-allocator: build/java-tables/.stamp
	@if [ "$${SCHEMA_JAVA_ALLOC_ANY_JDK:-}" = 1 ]; then echo "Java allocation observation mode: NOT CERTIFIED"; fi
	$(JAVA) -cp build/java-tables Main allocator $(JAVA_ALLOC_ARGS)

tables-java-allocator-runtime-negative-control: build/java-tables/.stamp
	@mkdir -p build/java-allocator-runtime-negative-control
	@if SCHEMA_JAVA_ALLOC_PINNED_FEATURE=17 SCHEMA_JAVA_ALLOC_ANY_JDK=0 $(JAVA) -cp build/java-tables Main allocator $(JAVA_ALLOC_ARGS) > build/java-allocator-runtime-negative-control/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the allocation gate certified on a JDK that is not the pin'; \
		cat build/java-allocator-runtime-negative-control/log; exit 1; \
	fi
	@grep -Fq 'allocation certification requires JDK 17, running 21' build/java-allocator-runtime-negative-control/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the allocation gate refused for another reason'; cat build/java-allocator-runtime-negative-control/log; exit 1; }
	@! grep -q '^alloc ' build/java-allocator-runtime-negative-control/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the allocation gate measured before it refused'; cat build/java-allocator-runtime-negative-control/log; exit 1; }
	@echo 'java allocation gate refuses a JDK that is not the pin (wants 21, ran 17), before measuring'

# THE JAVA TABLES SOAK (docs/PORTING.md I9, #517). A functional soak, never a
# unit test: it loops the variable form's load, loadMeasure and save over the
# committed wires the tables-java-variable-class target round-trips (the blob,
# list and pointer units on this classpath) for TABLES_JAVA_SOAK_ITERATIONS
# passes (a count, not a clock: the default is the release act, about an hour
# on the studio; `make test` runs the short form), and
# gates on the per-iteration ALLOCATION COUNT being flat after warmup — a Java
# load builds a Value tree, so the floor is not zero and the gate is on
# STABILITY, not on zero — with resident-heap drift reported beside it as the
# weaker instrument that sees retention. It refuses to certify off the pinned
# JDK exactly as modeAllocator does, and honours SCHEMA_JAVA_ALLOC_ANY_JDK=1
# as observation only (reports, does not certify).
TABLES_JAVA_SOAK_ITERATIONS ?= 134217728
.PHONY: tables-java-soak tables-java-soak-negative-control
tables-java-soak: build/java-tables/.stamp
	@if [ "$${SCHEMA_JAVA_ALLOC_ANY_JDK:-}" = 1 ]; then echo "Java soak observation mode: NOT CERTIFIED"; fi
	$(JAVA) -cp build/java-tables Soak --selftest
	$(JAVA) -cp build/java-tables Soak $(TABLES_JAVA_SOAK_ITERATIONS) testdata/wire/tables/blob_small.bin testdata/wire/tables/list_scalars.bin testdata/wire/tables/graph_tree.bin

# ITS NEGATIVE CONTROL: a matched allocation pair planted per iteration by an
# environment variable read in Soak.java (never a change to generated code) for
# the measured loop alone, so the settled clean floor stays put and the count
# must go red while the drift line stays green.
tables-java-soak-negative-control: build/java-tables/.stamp
	@if SCHEMA_JAVA_SOAK_PLANT=1 $(JAVA) -cp build/java-tables Soak 65536 testdata/wire/tables/blob_small.bin testdata/wire/tables/list_scalars.bin testdata/wire/tables/graph_tree.bin > build/java-soak-negative.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a planted per-iteration allocation left the soak count green'; \
		cat build/java-soak-negative.log; exit 1; \
	fi
	@grep -Fq 'per-iteration allocation moved from' build/java-soak-negative.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the soak went red, but not on the COUNT'; cat build/java-soak-negative.log; exit 1; }
	@grep -Fq 'soak: drift OK' build/java-soak-negative.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the drift line was not green beside the red COUNT'; cat build/java-soak-negative.log; exit 1; }
	@echo 'java tables soak negative control: a planted per-iteration allocation turns the COUNT red'

# ITS LOCALIZING CONTROL (I1: a control must say WHERE). One allocation per row
# is planted in row 1's walk — in the harness, behind
# SCHEMA_JAVA_ALLOC_PLANT_ROW_WALK, never in generated code — and the gate must
# go red on row 1 BY NAME while row 2, the cook walk, stays green. A gate that
# went red everywhere, or on the wrong row, would be an instrument that cannot
# tell the paths apart.
tables-java-alloc-negative-control: build/java-tables/.stamp
	@mkdir -p build/java-alloc-negative-control
	@if SCHEMA_JAVA_ALLOC_PLANT_ROW_WALK=1 $(JAVA) -cp build/java-tables Main allocator $(JAVA_ALLOC_ARGS) > build/java-alloc-negative-control/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: an allocation planted per row left the allocation gate green'; \
		cat build/java-alloc-negative-control/log; exit 1; \
	fi
	@grep -q '^alloc PLANTED' build/java-alloc-negative-control/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant did not arm'; cat build/java-alloc-negative-control/log; exit 1; }
	@grep -q '^alloc 1 block-walk .* RED$$' build/java-alloc-negative-control/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the gate went red, but not on row 1 (the block walk)'; cat build/java-alloc-negative-control/log; exit 1; }
	@grep -q '^alloc 2 cook-walk .* OK$$' build/java-alloc-negative-control/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the plant reddened row 2 (the cook walk) too: the gate does not localize'; cat build/java-alloc-negative-control/log; exit 1; }
	@test "$$(grep -c ' RED$$' build/java-alloc-negative-control/log)" = 1 || \
		{ echo 'NEGATIVE CONTROL FAILED: more than the planted row went red'; cat build/java-alloc-negative-control/log; exit 1; }
	@grep '^alloc 1 block-walk' build/java-alloc-negative-control/log
	@grep '^alloc 2 cook-walk' build/java-alloc-negative-control/log
	@echo 'negative control: one allocation planted per row reds row 1 (the block walk) alone; row 2 (the cook walk) stays green'

test-java: tables-java-allocator tables-java-allocator-runtime-negative-control tables-java-alloc-negative-control tables-java-soak-short tables-java-soak-negative-control

# the short form `make test` reaches (524288 passes, ~13 s on the studio); the
# hour is `make tables-java-soak`
# The stamp is the parent's prerequisite, so under -j the sub-make finds it
# built and never races a sibling recipe that is writing build/java-tables.
.PHONY: tables-java-soak-short
tables-java-soak-short: build/java-tables/.stamp
	$(MAKE) tables-java-soak TABLES_JAVA_SOAK_ITERATIONS=524288

# I12 (docs/PORTING.md) — THE DOCUMENTED SURFACE COMPILES AND RUNS, Java side.
# The cook example is pulled from docs/USAGE.md into a class that reads the
# fixture cook and supplies the page's `use` hook, compiled with the generated
# `tables/pointers` package under the consumer's `-Xlint:all -Werror`, and run.
.PHONY: tables-java-usage
JAVA_USAGE_POINTERS ?= build/tables-generated-java/pointers
JAVA_USAGE_DIR ?= build/java-usage
.PHONY: tables-java-usage-run tables-java-usage-negative-control
tables-java-usage: build/tables-generated-java/.stamp build/cook-open/.stamp docs/USAGE.md
	$(MAKE) --no-print-directory tables-java-usage-run JAVA_USAGE_POINTERS=build/tables-generated-java/pointers JAVA_USAGE_DIR=build/java-usage
tables-java-usage-run:
	@rm -rf $(JAVA_USAGE_DIR) && mkdir -p $(JAVA_USAGE_DIR)
	printf 'package graphdemo;\n\nimport java.nio.file.Files;\nimport java.nio.file.Path;\n\npublic final class Usage {\n    static int used;\n\n    static void use(int value) { used++; }\n\n    public static void main(String[] args) throws Exception {\n        byte[] bytes = Files.readAllBytes(Path.of(args[0]));\n\n' > $(JAVA_USAGE_DIR)/Usage.java
	sed -n '/<!-- java-table-usage -->/,/<!-- \/java-table-usage -->/p' docs/USAGE.md \
		| sed '1,2d' | sed '$$d' | sed '$$d' >> $(JAVA_USAGE_DIR)/Usage.java
	printf '\n        if (used == 0) { throw new IllegalStateException("the cook walk read no node"); }\n        System.out.println("usage: docs/USAGE.md Java cook example opened the Scene cook and walked " + used + " node(s)");\n    }\n}\n' >> $(JAVA_USAGE_DIR)/Usage.java
	grep -q 'SceneCook.open' $(JAVA_USAGE_DIR)/Usage.java || \
		{ echo "MISSING: docs/USAGE.md carries no java-table-usage example"; exit 1; }
	$(JAVAC) --release 17 -Xlint:all -Werror -d $(JAVA_USAGE_DIR)/classes \
		$(JAVA_USAGE_POINTERS)/*.java $(JAVA_USAGE_DIR)/Usage.java
	$(JAVA) -cp $(JAVA_USAGE_DIR)/classes graphdemo.Usage $(CURDIR)/build/cook-open/Scene.cook

# ITS NEGATIVE CONTROL: the cook's documented `root()` renamed in the emitter
# (cook.go), the code drifting from the page. The page's example must stop
# compiling, and the gate must go red on Usage.java itself.
JAVA_USAGE_SABOTAGE := build/java-usage-sabotage
JAVA_USAGE_SED := s@public int root() { return region; }@public int rootRegion() { return region; } // SABOTAGED: the documented root() is gone@
tables-java-usage-negative-control: build/cook-open/.stamp docs/USAGE.md
	$(call java_tables_sabotage,$(JAVA_USAGE_SABOTAGE),internal/codegen/javatable/cook.go,JAVA_USAGE_SED)
	@if $(MAKE) --no-print-directory tables-java-usage-run JAVA_USAGE_POINTERS=$(JAVA_USAGE_SABOTAGE)/generated/pointers \
			JAVA_USAGE_DIR=$(JAVA_USAGE_SABOTAGE)/usage > $(JAVA_USAGE_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the documented root() renamed in the code left the usage gate green"; \
		cat $(JAVA_USAGE_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "Usage.java.*cannot find symbol" $(JAVA_USAGE_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the page's example"; \
		  cat $(JAVA_USAGE_SABOTAGE)/log; exit 1; }
	@grep -m1 "cannot find symbol" $(JAVA_USAGE_SABOTAGE)/log
	@echo "negative control: the documented root() renamed in the emitter turns the usage gate RED on the page's example"
test-java: tables-java-usage-negative-control

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
	$(MAKE) tables-java-usage
	$(MAKE) tables-java-fixedform-negative-control
	$(MAKE) tables-java-fixedform-arm-negative-control
	# THE VERSIONING GATE'S OWN CONTROL, beside the byte gate's. It was written
	# and registered in make/negative-controls.json and then hung off NO recipe,
	# so nothing but the negative-controls matrix ever ran it and `make` itself
	# could not. It sits HERE and not in `test-java` because it rebuilds the
	# compiler over a sabotaged emitter and runs both columns of every §5.7 row
	# behind it — the expensive half, which is what this target is for (above).
	$(MAKE) tables-java-versioning-negative-control
	# THE KEYED ACCESSOR'S CONTROL (docs/SPEC-TABLES.md §2.4): two overlay test runs,
	# one per dropped bound, beside the versioning control for the same reason.
	$(MAKE) tables-java-keyed-negative-control
	$(MAKE) conformance-negative-control-java-block
	$(MAKE) conformance-negative-control-java-walk
	$(MAKE) tables-java-fuzz-negative-control
	$(MAKE) tables-java-block-lead-negative-control
	$(MAKE) tables-java-cook-extent-negative-control
	$(MAKE) tables-java-runtime-home-negative-control
	# THE PROOF TABLE'S CONTROLS that `make test` cannot afford (docs/PORTING.md I4,
	# J3, J4, M21): the per-case absent gate and its control each run the
	# harness over the Java leg, and the rest rebuild the compiler.
	$(MAKE) conformance-java-absent
	$(MAKE) conformance-java-absent-negative-control
	$(MAKE) tables-java-release-negative-control
	$(MAKE) tables-java-clean-drift-negative-control
	$(MAKE) tables-java-clean-formatter-missing-negative-control
	$(MAKE) tables-java-finite-float-negative-control

# THE RELEASE TIER'S NEGATIVE CONTROL (docs/PORTING.md J3). The instrument only
# this tier runs is the whole-tree compile under -Xlint:all -Werror
# (tables-java-compile-all). The control plants a REDUNDANT CAST in the emitted
# TableBytes.u8 (block.go), which every unit carries and which compiles and
# reads exactly as before, and the compile gate must go red on the warning.
JAVA_RELEASE_SABOTAGE := build/java-release-sabotage
JAVA_RELEASE_SED := s@return data\[at\] & 0xff;@return (int) (data[at] \& 0xff); // SABOTAGED: a redundant cast@
.PHONY: tables-java-release-negative-control
tables-java-release-negative-control: build/cook-open/.stamp
	$(call java_tables_sabotage,$(JAVA_RELEASE_SABOTAGE),internal/codegen/javatable/block.go,JAVA_RELEASE_SED,$(JAVA_TABLES_LEG_SOURCES))
	@if $(MAKE) --no-print-directory tables-java-compile-all-run JAVA_COMPILE_ALL_DIRS='$(JAVA_RELEASE_SABOTAGE)/generated/*/' \
			JAVA_COMPILE_ALL_OUT=$(JAVA_RELEASE_SABOTAGE)/all TABLES_JAVA_COMPILE_ALL_MIN=6 > $(JAVA_RELEASE_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a redundant cast in every unit left the release compile gate green"; \
		cat $(JAVA_RELEASE_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "redundant cast" $(JAVA_RELEASE_SABOTAGE)/log && grep -q "JAVA COMPILE GATE FAILED" $(JAVA_RELEASE_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the planted warning"; \
		  cat $(JAVA_RELEASE_SABOTAGE)/log; exit 1; }
	@grep -m1 "JAVA COMPILE GATE FAILED" $(JAVA_RELEASE_SABOTAGE)/log
	@echo "negative control: one redundant cast in the emitted TableBytes turns the release tier's compile gate RED"

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
# block surfaces, the cook surfaces and the WIRE-CARRYING surfaces (wire,
# report, json-read, json-write) the variable form (docs/SPEC-TABLES.md §3)
# carries for the roots this leg's variable wire answers: graphdemo's Scene and
# blobdemo's Catalog. The corpus units, the block unit and the pointered unit
# are packages of ONE classpath, so a single JVM start-up covers the cook's
# node dump and the cook forgery battery too — which the C# leg hands to a
# second project because its cook side is a second assembly. `blockhome` is not
# on this classpath and does not need to be; the tables-java-compile gate is
# what proves it builds.
.PHONY: build-conformance-java
build-conformance-java: build/tables-generated-java/.stamp test/conformance/java/src/Driver.java test/conformance/java/src/TablesWireFuzz.java
	@rm -rf build/conformance-java && mkdir -p build/conformance-java
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/conformance-java \
		build/tables-generated-java/examples/*.java build/tables-generated-java/pointers/*.java \
		build/tables-generated-java/blobs/*.java \
		build/tables-generated-java/maps/*.java build/tables-generated-java/arms/*.java \
		build/tables-generated-java/scalars/*.java build/tables-generated-java/scalars2/*.java \
		build/tables-generated-java/messages/*.java \
		build/tables-generated-java/stream/*.java build/tables-generated-java/lists/*.java \
		build/tables-generated-java/rt1/*.java \
		build/tables-generated-java/block/*.java build/tables-generated-java/v1/*.java \
		build/tables-generated-java/v2/*.java build/tables-generated-java/w1/*.java \
		build/tables-generated-java/w2/*.java build/tables-generated-java/r1/*.java \
		build/tables-generated-java/r2/*.java build/tables-generated-java/p1/*.java \
		build/tables-generated-java/p2/*.java build/tables-generated-java/p3/*.java \
		build/tables-generated-java/m1/*.java build/tables-generated-java/m2/*.java \
		build/tables-generated-java/a1/*.java build/tables-generated-java/a2/*.java \
		build/tables-generated-java/g1/*.java \
		build/tables-generated-java/k1/*.java build/tables-generated-java/k2/*.java \
		build/tables-generated-java/backend/*.java build/tables-generated-java/vocab/*.java \
		build/tables-generated-java/vocab9/*.java \
		test/conformance/java/src/Driver.java test/conformance/java/src/TablesWireFuzz.java

# THE PER-CASE ABSENT GATE (docs/PORTING.md I4; test/conformance/README.md).
# The Java leg alone through the harness: it must be green, and at least one
# surface it otherwise answers must carry a per-case absence (`pass N/M +Ka`
# with N > 0), which is the case grain the method names, as opposed to a whole
# surface left out of `list`.
JAVA_ABSENT := build/conformance-java-absent
.PHONY: conformance-java-absent conformance-java-absent-negative-control
conformance-java-absent: build/conformance-harness build-conformance-java
	@rm -rf $(JAVA_ABSENT) && mkdir -p $(JAVA_ABSENT)
	@printf 'java %s/test/conformance/java/driver\n' "$(CURDIR)" > $(JAVA_ABSENT)/drivers.txt
	JAVA=$(JAVA) ./build/conformance-harness run --drivers $(JAVA_ABSENT)/drivers.txt \
		--work $(JAVA_ABSENT)/work > $(JAVA_ABSENT)/log 2>&1 || \
		{ cat $(JAVA_ABSENT)/log; echo "conformance-java-absent: the Java leg is not green"; exit 1; }
	@grep -qE ' pass [1-9][0-9]*/[0-9]+ \+[1-9][0-9]*a' $(JAVA_ABSENT)/log || \
		{ cat $(JAVA_ABSENT)/log; echo "conformance-java-absent: no surface carries a per-case absence"; exit 1; }
	@grep -E ' pass [1-9][0-9]*/[0-9]+ \+[1-9][0-9]*a' $(JAVA_ABSENT)/log | head -3
	@echo "conformance-java-absent: the Java leg is green and answers absent per case"

# ITS NEGATIVE CONTROL: the driver's two absence writers (spillAbsent and
# spillAbsentReason) write no marker. Every case the leg cannot answer then has
# no answer at all, and the gate must go red on "the driver wrote nothing".
JAVA_ABSENT_SABOTAGE := build/conformance-java-absent-sabotage
JAVA_ABSENT_SED := s@Files.write(Path.of(out, name + ".absent"), @if (false) Files.write(Path.of(out, name + ".absent"), /* SABOTAGED */ @
conformance-java-absent-negative-control: build/conformance-harness build-conformance-java
	@rm -rf $(JAVA_ABSENT_SABOTAGE) && mkdir -p $(JAVA_ABSENT_SABOTAGE)/src $(JAVA_ABSENT_SABOTAGE)/classes
	@sed '$(JAVA_ABSENT_SED)' test/conformance/java/src/Driver.java > $(JAVA_ABSENT_SABOTAGE)/src/Driver.java
	@test "$$(grep -c SABOTAGED $(JAVA_ABSENT_SABOTAGE)/src/Driver.java)" = 2 || \
		{ echo "NEGATIVE CONTROL: the absence sabotage did not apply to both writers"; exit 1; }
	$(JAVAC) --release 17 -nowarn -cp build/conformance-java -d $(JAVA_ABSENT_SABOTAGE)/classes $(JAVA_ABSENT_SABOTAGE)/src/Driver.java
	@printf '#!/bin/sh\nexec %s -cp %s/classes:%s/build/conformance-java Driver "$$@"\n' "$(JAVA)" "$(CURDIR)/$(JAVA_ABSENT_SABOTAGE)" "$(CURDIR)" > $(JAVA_ABSENT_SABOTAGE)/driver
	@chmod +x $(JAVA_ABSENT_SABOTAGE)/driver
	@printf 'java %s/driver\n' "$(CURDIR)/$(JAVA_ABSENT_SABOTAGE)" > $(JAVA_ABSENT_SABOTAGE)/drivers.txt
	@if ./build/conformance-harness run --drivers $(JAVA_ABSENT_SABOTAGE)/drivers.txt \
			--work $(JAVA_ABSENT_SABOTAGE)/work > $(JAVA_ABSENT_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a driver that writes no absence marker left the harness green"; \
		cat $(JAVA_ABSENT_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "the driver wrote nothing" $(JAVA_ABSENT_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on a missing answer"; \
		  cat $(JAVA_ABSENT_SABOTAGE)/log; exit 1; }
	@grep -m1 "the driver wrote nothing" $(JAVA_ABSENT_SABOTAGE)/log
	@echo "negative control: a driver that writes no absence marker turns the harness RED on the cases it cannot answer"

# THE JAVA LEG OF THE TOLERANT WIRE'S DIFFERENTIAL FUZZER (docs/PORTING.md
# I15; test/conformance/README.md, "The wire-fuzz driver"). One JVM on a pipe,
# test/conformance/java/src/TablesWireFuzz.java on the conformance classpath:
# every manifest root the leg carries, its file form (load/loadMeasure/save),
# its message form against the unit's own announced vocabulary, and the
# retention arm's pair. The harness owns the mutants and the oracle
# (internal/tablewire, the compiler's own engine).
#
# The gate runs every ENUMERATED pass and every pinned vector over BOTH
# arms: the plain pair, and the retaining pair (--retain, §6.6), whose own
# pinned vectors it also replays. The seeded RANDOM pass stays at N = 0: at
# the shared SEED its first finding in each arm is DAMAGED input (both
# reports malformed, or the oracle discarding a damaged load the leg reads
# clean: stream_arm_first random #22918 on the plain arm, parts_count_past_bound
# random #4485 on the retaining arm), parked under the discard-on-damage
# ruling, and each stops its pass before it reaches the mutants behind it.
JAVA_WIRE_FUZZ_N ?= 0
JAVA_WIRE_FUZZ_DRIVER = $(JAVA) -cp $(CURDIR)/build/conformance-java TablesWireFuzz
JAVA_RETAIN_VECTORS := retain_declared_default:graphdemo:Scene retain_node_record_unknown:graphdemo:Scene retain_node_flags:tblw1:Fleet retain_count_over_length_refused:listdemo:Mixed retain_scalar_array_default:mapdemo:Cells retain_map_emptied_unplaced:listdemo:Army
# THE PLAIN ARM'S JAVA-ONLY VECTORS: a keyed header and a map header whose foreign
# element kind carries a count spelled past the field's L (docs/SPEC-TABLES.md §2.9, §3.2):
# the oracle reads the count against the parent and counts one kind_mismatch,
# no damage. They join INDEX.txt, which every leg replays, when every leg reads
# these headers against the parent.
JAVA_PLAIN_VECTORS := keyed_header_count_past_length:tabledemo:KeyedConfig map_header_count_past_length:mapdemo:Slots
.PHONY: tables-java-wire-fuzz tables-java-wire-fuzz-negative-control tables-java-retain-fuzz-negative-control
tables-java-wire-fuzz: build/conformance-harness build-conformance-java
	@mkdir -p build/wire-fuzz
	./build/conformance-harness wire-fuzz --finite-owed tables-finite/java --driver "$(JAVA_WIRE_FUZZ_DRIVER)" \
		--seed $(SEED) --n $(JAVA_WIRE_FUZZ_N) --failed build/wire-fuzz/failed-java.bin
	./build/conformance-harness wire-fuzz --finite-owed tables-finite/java --driver "$(JAVA_WIRE_FUZZ_DRIVER)" --retain \
		--seed $(SEED) --n $(JAVA_WIRE_FUZZ_N) --failed build/wire-fuzz/failed-java-retain.bin
	@set -e; for v in $(JAVA_PLAIN_VECTORS); do \
		name=$${v%%:*}; rest=$${v#*:}; unit=$${rest%%:*}; root=$${rest#*:}; \
		./build/conformance-harness wire-fuzz --driver "$(JAVA_WIRE_FUZZ_DRIVER)" \
			--replay testdata/wire/tables/fuzz-vectors/$$name.bin --unit $$unit --root $$root; \
	done
	@set -e; for v in $(JAVA_RETAIN_VECTORS); do \
		name=$${v%%:*}; rest=$${v#*:}; unit=$${rest%%:*}; root=$${rest#*:}; \
		./build/conformance-harness wire-fuzz --driver "$(JAVA_WIRE_FUZZ_DRIVER)" --retain \
			--replay testdata/wire/tables/fuzz-vectors/$$name.bin --unit $$unit --root $$root; \
	done

test-java: tables-java-wire-fuzz

# Its negative control (docs/PORTING.md I2, I15): a scratch copy of the
# generated units whose LEB128 reader accepts a non-minimal spelling, compiled
# with the same leg. The ENUMERATED canonical pass plants exactly those
# spellings, so the differential must go red on the read report, and the
# unplanted gate above must be green.
JAVA_WIRE_FUZZ_UNITS := examples pointers blobs maps arms scalars scalars2 messages wide stream lists rt1 \
	block v1 v2 w1 w2 r1 r2 p1 p2 p3 m1 m2 a1 a2 g1 k1 k2 backend vocab vocab9
JAVA_WIRE_FUZZ_SABOTAGE := build/java-wire-sabotage
tables-java-wire-fuzz-negative-control: tables-java-wire-fuzz
	@rm -rf $(JAVA_WIRE_FUZZ_SABOTAGE) && mkdir -p $(JAVA_WIRE_FUZZ_SABOTAGE)/generated $(JAVA_WIRE_FUZZ_SABOTAGE)/classes
	@for u in $(JAVA_WIRE_FUZZ_UNITS); do cp -R build/tables-generated-java/$$u $(JAVA_WIRE_FUZZ_SABOTAGE)/generated/$$u || exit 1; done
	@for f in $(JAVA_WIRE_FUZZ_SABOTAGE)/generated/*/TableVariable.java; do \
		sed 's/if (i > 0 \&\& b == 0) {/if (false) { \/\/ SABOTAGED canonical LEB128/' "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f" || exit 1; \
	done
	@grep -q 'SABOTAGED canonical LEB128' $(JAVA_WIRE_FUZZ_SABOTAGE)/generated/examples/TableVariable.java || \
		{ echo 'NEGATIVE CONTROL: the Java canonical LEB128 sabotage did not apply'; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_WIRE_FUZZ_SABOTAGE)/classes \
		$(JAVA_WIRE_FUZZ_SABOTAGE)/generated/*/*.java test/conformance/java/src/Driver.java test/conformance/java/src/TablesWireFuzz.java
	@if ./build/conformance-harness wire-fuzz --finite-owed tables-finite/java \
			--driver "$(JAVA) -cp $(CURDIR)/$(JAVA_WIRE_FUZZ_SABOTAGE)/classes TablesWireFuzz" --seed 1 --n 0 \
			--failed $(JAVA_WIRE_FUZZ_SABOTAGE)/failed.bin > $(JAVA_WIRE_FUZZ_SABOTAGE)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a Java reader that accepts noncanonical LEB128 left the differential green'; exit 1; \
	fi
	@grep -qE 'the report differs|the oracle discards a damaged load' $(JAVA_WIRE_FUZZ_SABOTAGE)/log || { cat $(JAVA_WIRE_FUZZ_SABOTAGE)/log; exit 1; }
	@echo 'Java wire negative control: accepting noncanonical LEB128 changes the read report'

test-java: tables-java-wire-fuzz-negative-control

# THE RETENTION ARM'S CONTROL (docs/PORTING.md I15; docs/SPEC-TABLES.md §4.2,
# §6.6): a scratch copy of the generated units whose retaining save tests
# graphdemo.Meta's `build` against zero instead of its declared default 1, the
# fault the pinned vector retain_declared_default was found on. The vector's
# retaining replay must go red, and the unplanted gate above is green.
JAVA_RETAIN_FUZZ_SABOTAGE := build/java-retain-sabotage
tables-java-retain-fuzz-negative-control: tables-java-wire-fuzz
	@rm -rf $(JAVA_RETAIN_FUZZ_SABOTAGE) && mkdir -p $(JAVA_RETAIN_FUZZ_SABOTAGE)/generated $(JAVA_RETAIN_FUZZ_SABOTAGE)/classes
	@for u in $(JAVA_WIRE_FUZZ_UNITS); do cp -R build/tables-generated-java/$$u $(JAVA_RETAIN_FUZZ_SABOTAGE)/generated/$$u || exit 1; done
	sed 's/if (v\.build != 1) {/if (v.build != 0) { \/\/ SABOTAGED declared default/' \
		build/tables-generated-java/pointers/MetaRetain.java > $(JAVA_RETAIN_FUZZ_SABOTAGE)/generated/pointers/MetaRetain.java
	@grep -q 'SABOTAGED declared default' $(JAVA_RETAIN_FUZZ_SABOTAGE)/generated/pointers/MetaRetain.java || \
		{ echo 'NEGATIVE CONTROL: the Java retaining-default sabotage did not apply'; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_RETAIN_FUZZ_SABOTAGE)/classes \
		$(JAVA_RETAIN_FUZZ_SABOTAGE)/generated/*/*.java test/conformance/java/src/Driver.java test/conformance/java/src/TablesWireFuzz.java
	@if ./build/conformance-harness wire-fuzz --retain \
			--driver "$(JAVA) -cp $(CURDIR)/$(JAVA_RETAIN_FUZZ_SABOTAGE)/classes TablesWireFuzz" \
			--replay testdata/wire/tables/fuzz-vectors/retain_declared_default.bin --unit graphdemo --root Scene \
			> $(JAVA_RETAIN_FUZZ_SABOTAGE)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a retaining save that ignores a declared default left the pinned vector green'; exit 1; \
	fi
	@grep -q 'the decoded value differs' $(JAVA_RETAIN_FUZZ_SABOTAGE)/log || { cat $(JAVA_RETAIN_FUZZ_SABOTAGE)/log; exit 1; }
	@echo 'Java retain negative control: a retaining save that ignores a declared default changes the saved bytes'

# THE NODE-RECORD RETENTION CONTROL (docs/SPEC-TABLES.md §6.6: retained "in the
# root body and in every node record"): a scratch copy whose retaining load
# keys no node record, so the save cannot find the node's retained fields and
# writes its body without them. The retaining replay of
# retain_node_record_unknown must go red.
JAVA_RETAIN_NODE_SABOTAGE := build/java-retain-node-sabotage
.PHONY: tables-java-retain-node-negative-control
tables-java-retain-node-negative-control: tables-java-wire-fuzz
	@rm -rf $(JAVA_RETAIN_NODE_SABOTAGE) && mkdir -p $(JAVA_RETAIN_NODE_SABOTAGE)/generated $(JAVA_RETAIN_NODE_SABOTAGE)/classes
	@for u in $(JAVA_WIRE_FUZZ_UNITS); do cp -R build/tables-generated-java/$$u $(JAVA_RETAIN_NODE_SABOTAGE)/generated/$$u || exit 1; done
	sed 's/retain\.nodeKeys\.put(rec\.table, i);/\/\/ SABOTAGED no node key/' \
		build/tables-generated-java/pointers/SceneRetain.java > $(JAVA_RETAIN_NODE_SABOTAGE)/generated/pointers/SceneRetain.java
	@grep -q 'SABOTAGED no node key' $(JAVA_RETAIN_NODE_SABOTAGE)/generated/pointers/SceneRetain.java || \
		{ echo 'NEGATIVE CONTROL: the Java node-record retention sabotage did not apply'; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_RETAIN_NODE_SABOTAGE)/classes \
		$(JAVA_RETAIN_NODE_SABOTAGE)/generated/*/*.java test/conformance/java/src/Driver.java test/conformance/java/src/TablesWireFuzz.java
	@if ./build/conformance-harness wire-fuzz --retain \
			--driver "$(JAVA) -cp $(CURDIR)/$(JAVA_RETAIN_NODE_SABOTAGE)/classes TablesWireFuzz" \
			--replay testdata/wire/tables/fuzz-vectors/retain_node_record_unknown.bin --unit graphdemo --root Scene \
			> $(JAVA_RETAIN_NODE_SABOTAGE)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a save that cannot find a node record'"'"'s retained fields left the pinned vector green'; exit 1; \
	fi
	@grep -q 'FAILED after' $(JAVA_RETAIN_NODE_SABOTAGE)/log || { cat $(JAVA_RETAIN_NODE_SABOTAGE)/log; exit 1; }
	@grep -m1 'differs' $(JAVA_RETAIN_NODE_SABOTAGE)/log
	@echo 'Java node-record retention control: a save that keys no node record drops its retained field'

test-java: tables-java-retain-node-negative-control

test-java: tables-java-retain-fuzz-negative-control

# THE FOREIGN-KIND MAP COUNT'S CONTROL (docs/SPEC-TABLES.md §2.8, §4): a scratch
# copy whose map reader reads a foreign element kind's count inside the map's
# own L again. Its replay is map_header_count_past_length (mapdemo.Slots): the
# oracle reads that map as one kind_mismatch with NO damage, so the counters are
# compared. (map_foreign_kind_count_past_body, the vector the fault was found
# on, is damaged elsewhere, and under the discard-on-damage ruling the harness
# no longer compares a damaged load's counters, so it cannot see this fault.)
JAVA_MAP_COUNT_SABOTAGE := build/java-map-count-sabotage
.PHONY: tables-java-map-count-negative-control
tables-java-map-count-negative-control: tables-java-wire-fuzz
	@rm -rf $(JAVA_MAP_COUNT_SABOTAGE) && mkdir -p $(JAVA_MAP_COUNT_SABOTAGE)/generated $(JAVA_MAP_COUNT_SABOTAGE)/classes
	@for u in $(JAVA_WIRE_FUZZ_UNITS); do cp -R build/tables-generated-java/$$u $(JAVA_MAP_COUNT_SABOTAGE)/generated/$$u || exit 1; done
	@for f in $(JAVA_MAP_COUNT_SABOTAGE)/generated/lists/*Variable.java $(JAVA_MAP_COUNT_SABOTAGE)/generated/maps/*Variable.java; do \
		sed 's/long n = ek != 13 ? ar\.count(r\.limit) : ar\.leb();/long n = ar.leb(); \/\/ SABOTAGED map count/' "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f" || exit 1; \
	done
	@grep -q 'SABOTAGED map count' $(JAVA_MAP_COUNT_SABOTAGE)/generated/maps/SlotsVariable.java || \
		{ echo 'NEGATIVE CONTROL: the Java map-count sabotage did not apply'; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_MAP_COUNT_SABOTAGE)/classes \
		$(JAVA_MAP_COUNT_SABOTAGE)/generated/*/*.java test/conformance/java/src/Driver.java test/conformance/java/src/TablesWireFuzz.java
	@if ./build/conformance-harness wire-fuzz \
			--driver "$(JAVA) -cp $(CURDIR)/$(JAVA_MAP_COUNT_SABOTAGE)/classes TablesWireFuzz" \
			--replay testdata/wire/tables/fuzz-vectors/map_header_count_past_length.bin --unit mapdemo --root Slots \
			--failed $(JAVA_MAP_COUNT_SABOTAGE)/failed.bin > $(JAVA_MAP_COUNT_SABOTAGE)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a map reader that reads a foreign kind'"'"'s count inside L left the differential green'; exit 1; \
	fi
	@grep -q 'the report differs' $(JAVA_MAP_COUNT_SABOTAGE)/log || { cat $(JAVA_MAP_COUNT_SABOTAGE)/log; exit 1; }
	@echo 'Java map-count negative control: reading a foreign kind'"'"'s count inside L loses the kind mismatch'

test-java: tables-java-map-count-negative-control

# THE KEYED-COUNT CONTROL: a scratch copy of the generated units whose keyed
# reader reads the count inside the field's own L again. The replay of
# keyed_header_count_past_length must go red on the read report.
JAVA_KEYED_COUNT_SABOTAGE := build/java-keyed-count-sabotage
.PHONY: tables-java-keyed-count-negative-control
tables-java-keyed-count-negative-control: tables-java-wire-fuzz
	@rm -rf $(JAVA_KEYED_COUNT_SABOTAGE) && mkdir -p $(JAVA_KEYED_COUNT_SABOTAGE)/generated $(JAVA_KEYED_COUNT_SABOTAGE)/classes
	@for u in $(JAVA_WIRE_FUZZ_UNITS); do cp -R build/tables-generated-java/$$u $(JAVA_KEYED_COUNT_SABOTAGE)/generated/$$u || exit 1; done
	@for f in $(JAVA_KEYED_COUNT_SABOTAGE)/generated/*/*Variable.java; do \
		sed 's/long count = ar\.count(r\.limit);/long count = ar.leb(); \/\/ SABOTAGED keyed count/' "$$f" > "$$f.tmp" && mv "$$f.tmp" "$$f" || exit 1; \
	done
	@grep -q 'SABOTAGED keyed count' $(JAVA_KEYED_COUNT_SABOTAGE)/generated/examples/KeyedConfigVariable.java || \
		{ echo 'NEGATIVE CONTROL: the Java keyed-count sabotage did not apply'; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(JAVA_KEYED_COUNT_SABOTAGE)/classes \
		$(JAVA_KEYED_COUNT_SABOTAGE)/generated/*/*.java test/conformance/java/src/Driver.java test/conformance/java/src/TablesWireFuzz.java
	@if ./build/conformance-harness wire-fuzz --driver "$(JAVA) -cp $(CURDIR)/$(JAVA_KEYED_COUNT_SABOTAGE)/classes TablesWireFuzz" \
			--replay testdata/wire/tables/fuzz-vectors/keyed_header_count_past_length.bin --unit tabledemo --root KeyedConfig \
			--failed $(JAVA_KEYED_COUNT_SABOTAGE)/failed.bin > $(JAVA_KEYED_COUNT_SABOTAGE)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a keyed reader that reads the count inside L left the replay green'; exit 1; \
	fi
	@grep -q 'oracle: report=0,1,' $(JAVA_KEYED_COUNT_SABOTAGE)/log || { cat $(JAVA_KEYED_COUNT_SABOTAGE)/log; exit 1; }
	@echo 'Java keyed-count negative control: reading the count inside L loses the kind mismatch'

test-java: tables-java-keyed-count-negative-control

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
CONFORMANCE_NEGATIVE_JAVA_BLOCK_SED := s|if (stride != %sStride) { return -8; }|if (stride != %sStride) { } // SABOTAGED|
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
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/blobs tables/blobs
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/block tables/block
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/v1 test/tables/V1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/v2 test/tables/V2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/p1 test/tables/P1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/p3 test/tables/P3.schema
	# the driver names tblrt1 directly (the retain surfaces), so RT1 is on every classpath
	$(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_BLOCK)/generated/rt1 test/tables/RT1.schema
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

# THE JAVA TEXT-WALK NEGATIVE CONTROL (docs/PORTING.md I11): it breaks the
# generated fromJson's FIELD-KEY match, so every key falls through to the
# unknown branch and the reader populates nothing. The harness must go RED on
# `java / json-read` while `json-write` and `wire` stay green — a control that
# turned json-write red too would be proving the reader was broken, not that
# it CHECKS.
CONFORMANCE_NEGATIVE_JAVA_WALK := build/conformance-negative-java-walk
.PHONY: conformance-negative-control-java-walk
conformance-negative-control-java-walk: build/conformance-harness
	@rm -rf $(CONFORMANCE_NEGATIVE_JAVA_WALK) && mkdir -p $(CONFORMANCE_NEGATIVE_JAVA_WALK)
	@go run ./tools/sabotage -name java-text-walk-field-match \
		-out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/variableform.go.txt internal/codegen/javatable/variableform.go
	@cmp -s internal/codegen/javatable/variableform.go $(CONFORMANCE_NEGATIVE_JAVA_WALK)/variableform.go.txt && \
		{ echo "NEGATIVE CONTROL: the Java text-walk sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/javatable/variableform.go":"%s/$(CONFORMANCE_NEGATIVE_JAVA_WALK)/variableform.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CONFORMANCE_NEGATIVE_JAVA_WALK)/overlay.json
	go build -overlay $(CONFORMANCE_NEGATIVE_JAVA_WALK)/overlay.json -o $(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema ./cmd/schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/examples tables/examples
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/pointers tables/pointers
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/blobs tables/blobs
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/maps tables/maps
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/arms tables/arms
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/scalars tables/scalars
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/scalars2 test/tables/Scalars2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/block tables/block
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/v1 test/tables/V1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/v2 test/tables/V2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/p1 test/tables/P1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/p3 test/tables/P3.schema
	# the driver names tblrt1 directly (the retain surfaces), so RT1 is on every classpath
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/rt1 test/tables/RT1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/k1 test/tables/K1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/k2 test/tables/K2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/messages tables/messages
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/w1 test/tables/W1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/w2 test/tables/W2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/r1 test/tables/R1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/r2 test/tables/R2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/p2 test/tables/P2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/m1 test/tables/M1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/m2 test/tables/M2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/a1 test/tables/A1.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/a2 test/tables/A2.schema
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/backend tables/backend
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/vocab tables/vocab
	$(CONFORMANCE_NEGATIVE_JAVA_WALK)/schema generate --lang java --out $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/vocab9 tables/vocab9
	@grep -lq SABOTAGED $(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/*/*.java || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged fromJson"; exit 1; }
	$(JAVAC) --release 17 -nowarn -d $(CONFORMANCE_NEGATIVE_JAVA_WALK)/classes \
		$(CONFORMANCE_NEGATIVE_JAVA_WALK)/generated/*/*.java test/conformance/java/src/Driver.java
	@printf '#!/bin/sh\nexec %s -cp %s/classes Driver "$$@"\n' "$(JAVA)" "$(CURDIR)/$(CONFORMANCE_NEGATIVE_JAVA_WALK)" > $(CONFORMANCE_NEGATIVE_JAVA_WALK)/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_JAVA_WALK)/driver
	@printf 'java %s/driver\n' "$(CONFORMANCE_NEGATIVE_JAVA_WALK)" > $(CONFORMANCE_NEGATIVE_JAVA_WALK)/drivers.txt
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_NEGATIVE_JAVA_WALK)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_JAVA_WALK)/work > $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a Java reader with no field-key match left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log; exit 1; \
	fi
	@grep -q "java / json-read" $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the json-read battery"; \
		  cat $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log; exit 1; }
	@grep -q "^json-write    pass" $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: json-write went red too, so the control does not localise the READER"; \
		  cat $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log; exit 1; }
	@grep -q "^wire          pass" $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log; exit 1; }
	@grep -m1 "java / json-read" $(CONFORMANCE_NEGATIVE_JAVA_WALK)/log
	@echo "negative control: one field-key match off in the Java text walk turns the harness RED on json-read alone"

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
	$(MAKE) tables-java-runtime-home
	$(MAKE) tables-java-fuzz
	$(MAKE) tables-java-block-lead
	$(MAKE) tables-java-block-lead-negative-control
	$(MAKE) tables-java-order
	$(MAKE) tables-java-cook-extent
	# THE FIXED FORM against the C++ reference's own bytes (docs/SPEC-TABLES.md §3.4)
	$(MAKE) tables-java-fixedform
	$(MAKE) tables-java-fixedform-bench
	# THE MESSAGE FORM against the C++ reference's own bytes (docs/SPEC-TABLES.md §3.3)
	$(MAKE) tables-java-message-batch
	# THE VARIABLE CLASS against the committed reference bytes (docs/SPEC-TABLES.md §2, §3, §16)
	$(MAKE) tables-java-variable-class
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



test-java:

# ---------------------------------------------------------------------------
# THE CARD WIRE-FUZZ GATE (docs/SPEC-TABLES.md §2, §3)
# ---------------------------------------------------------------------------
# These targets are the Java side of the cards wire-fuzz gate: they mirror the
# Go leg's cards-generate and cards-wire-fuzz (make/go.mk) against the same
# oracle harness and the same seed, so the Java Card codec is held to the
# identical byte-for-byte contract. They live in make/java.mk (NOT a separate
# make/cards-java.mk) because the wide-scalar refusal gate
# (tables-ports-refuse-wide-scalars) discovers every make/*.mk as a live port,
# and a fragment named cards-java.mk would be refused there as a phantom port.
# The retain arm (cards-java-retain-fuzz) lives here too, NOT in a separate
# make/cards-java-retain.mk, for the same reason (the dart leg's own retain
# fragment was reddened by the same wildcard: docs/SPEC-TABLES.md §6.6).
.PHONY: cards-java-generate cards-java-wire-fuzz cards-java-retain-fuzz

# cards-java-generate writes the cards unit's Java into build/ (test-only, never
# part of the committed generated/ tree).
cards-java-generate: bin/schema
	@rm -rf build/tables-generated-java/cards
	@mkdir -p build/tables-generated-java/cards
	./bin/schema generate --lang java --out build/tables-generated-java/cards tables/cards

# The wire-fuzz driver: the generated cards unit beside test/conformance/java/
# src/WireFuzz.java, one class on one classpath, answering the harness's pipe
# protocol for the Card root only.
build/cards-java-wire-fuzz/.stamp: cards-java-generate test/conformance/java/src/WireFuzz.java make/java.mk
	@rm -rf build/cards-java-wire-fuzz && mkdir -p build/cards-java-wire-fuzz
	$(JAVAC) --release 17 -nowarn -d build/cards-java-wire-fuzz \
		build/tables-generated-java/cards/*.java test/conformance/java/src/WireFuzz.java
	@printf '#!/bin/sh\nexec %s -cp %s/build/cards-java-wire-fuzz WireFuzz "$$@"\n' \
		"$(JAVA)" "$(CURDIR)" > build/cards-java-wire-fuzz/driver
	@chmod +x build/cards-java-wire-fuzz/driver
	@touch $@

CARDS_JAVA_FUZZ_TIME ?= 30s
# The seeds the gate holds the Java Card codec to: the long-standing reference
# seed and the four the oracle fuzzer found this port's node-table census on
# (§3.1) — mutant #104 of seed 777 counted three unknowns where the oracle
# counts one, and mutant #748 of seed 2 counted one kind mismatch from a
# *string pointer against a numbering that failed to read whole, so each seed
# and its neighbours ride here as a regression.
CARDS_JAVA_FUZZ_SEEDS := 24845619678 100 777 200 3 100 42 1000 2 1000
cards-java-wire-fuzz: build/conformance-harness build/cards-java-wire-fuzz/.stamp
	@rm -f build/cards-java-wire-fuzz.log
	@set -- $(CARDS_JAVA_FUZZ_SEEDS); \
		while [ "$$#" -ge 2 ]; do \
			seed=$$1; n=$$2; shift 2; \
			timeout $(CARDS_JAVA_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
				--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
				--driver "$(CURDIR)/build/cards-java-wire-fuzz/driver" --seed $$seed --n $$n \
				>> build/cards-java-wire-fuzz.log 2>&1; \
			status=$$?; \
			if [ $$status -ne 0 ]; then cat build/cards-java-wire-fuzz.log; exit $$status; fi; \
		done
	@cat build/cards-java-wire-fuzz.log
	@grep -q '0 divergences' build/cards-java-wire-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-java-wire-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-java-wire-fuzz.log

test-java: cards-java-wire-fuzz

# cards-java-retain-fuzz is the SAME fuzzer through the RETENTION ARM
# (docs/SPEC-TABLES.md §6.6): every mutant is read and written back through the
# Card codec's retaining pair, and the two retention counters ride beside the
# six. The seed, count and coverage greps are cards-java-wire-fuzz's, plus
# --retain, so a retained save is held to the Go reference leg byte for byte.
cards-java-retain-fuzz: build/conformance-harness build/cards-java-wire-fuzz/.stamp
	@rm -f build/cards-java-retain-fuzz.log
	@set -- $(CARDS_JAVA_FUZZ_SEEDS); \
		while [ "$$#" -ge 2 ]; do \
			seed=$$1; n=$$2; shift 2; \
			timeout $(CARDS_JAVA_FUZZ_TIME) ./build/conformance-harness wire-fuzz \
				--manifest tables/cards/fuzz/MANIFEST.txt --json tables/cards/fuzz --vectors '' \
				--driver "$(CURDIR)/build/cards-java-wire-fuzz/driver" --seed $$seed --n $$n --retain \
				>> build/cards-java-retain-fuzz.log 2>&1; \
			status=$$?; \
			if [ $$status -ne 0 ]; then cat build/cards-java-retain-fuzz.log; exit $$status; fi; \
		done
	@cat build/cards-java-retain-fuzz.log
	@grep -q '0 divergences' build/cards-java-retain-fuzz.log
	@grep -qE '[1-9][0-9]* seeds over [1-9][0-9]* roots' build/cards-java-retain-fuzz.log
	@grep -qE '[1-9][0-9]* mutants' build/cards-java-retain-fuzz.log

test-java: cards-java-retain-fuzz

# ---------------------------------------------------------------------------
# THE CARD FACADE LEG (docs/SPEC-TABLES.md §2, §16; tables/cards/java,
# tables/cards/conformance/FORMAT.md "The leg contract"). The Java facade
# (cards.Cards) owns the card record, its wire copy, the customer bounds and the
# brief text parsed once and rendered many times; the driver answers the shared
# corpus's five surfaces against the Go reference leg's committed answers.
# These targets live in make/java.mk's cards section, never a separate
# make/cards-java.mk (the widest-scalar gate probes every make/*.mk as a port).
# ---------------------------------------------------------------------------
CARDS_JAVA_REQUIRE ?= load,save,render,parse,bounds,upgrade,edit,taskhash,view

build/cards-java-facade/.stamp: cards-java-generate \
		tables/cards/java/src/cards/Cards.java tables/cards/java/src/cards/Driver.java \
		tables/cards/java/test/CardsTest.java make/java.mk
	@rm -rf build/cards-java-facade && mkdir -p build/cards-java-facade
	$(JAVAC) --release 17 -nowarn -d build/cards-java-facade \
		build/tables-generated-java/cards/*.java \
		tables/cards/java/src/cards/*.java tables/cards/java/test/CardsTest.java
	@touch $@

.PHONY: cards-java-test
cards-java-test: build/cards-java-facade/.stamp
	$(JAVA) -XX:+UseSerialGC -XX:ActiveProcessorCount=1 -cp build/cards-java-facade CardsTest

.PHONY: cards-java-conformance
cards-java-conformance: build/conformance-harness build/cards-java-facade/.stamp
	./build/conformance-harness cards --corpus tables/cards/conformance \
		--driver tables/cards/java/driver --require $(CARDS_JAVA_REQUIRE)

.PHONY: cards-java-conformance-negative-control
cards-java-conformance-negative-control: build/conformance-harness build/cards-java-facade/.stamp
	@rm -f build/cards-java-conformance-neg.log
	@if CARDS_JAVA_NEGATIVE_RENDER=1 ./build/conformance-harness cards \
			--corpus tables/cards/conformance --driver tables/cards/java/driver \
			--require $(CARDS_JAVA_REQUIRE) > build/cards-java-conformance-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the card check stayed green with one render byte changed'; \
		cat build/cards-java-conformance-neg.log; exit 1; fi
	@grep -qE 'cards render .*FAIL' build/cards-java-conformance-neg.log || \
		{ echo 'NEGATIVE CONTROL FAILED: render did not go red'; cat build/cards-java-conformance-neg.log; exit 1; }
	@grep -qE 'cards (load|save|parse|bounds) pass' build/cards-java-conformance-neg.log || \
		{ echo 'NEGATIVE CONTROL FAILED: the other surfaces did not stay green'; cat build/cards-java-conformance-neg.log; exit 1; }
	@echo 'cards Java negative control: one changed render byte turns exactly render red'

.PHONY: cards-java-conformance-upgrade-negative-control
cards-java-conformance-upgrade-negative-control: build/conformance-harness build/cards-java-facade/.stamp
	@rm -f build/cards-java-conformance-upgrade-neg.log
	@if CARDS_JAVA_NEGATIVE_UPGRADE=1 ./build/conformance-harness cards \
			--corpus tables/cards/conformance --driver tables/cards/java/driver \
			--require $(CARDS_JAVA_REQUIRE) > build/cards-java-conformance-upgrade-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: upgrade stayed green with one retained field dropped'; \
		cat build/cards-java-conformance-upgrade-neg.log; exit 1; fi
	@grep -qE 'cards upgrade .*FAIL' build/cards-java-conformance-upgrade-neg.log || \
		{ echo 'NEGATIVE CONTROL FAILED: upgrade did not go red'; cat build/cards-java-conformance-upgrade-neg.log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|edit|taskhash|view) pass' build/cards-java-conformance-upgrade-neg.log || \
		{ echo 'NEGATIVE CONTROL FAILED: another surface went red'; cat build/cards-java-conformance-upgrade-neg.log; exit 1; }
	@echo 'cards Java upgrade negative control: one dropped retained field turns exactly upgrade red'

.PHONY: cards-java-conformance-hash-negative-control
cards-java-conformance-hash-negative-control: build/conformance-harness build/cards-java-facade/.stamp
	@rm -f build/cards-java-conformance-hash-neg.log
	@if CARDS_JAVA_NEGATIVE_HASH=1 ./build/conformance-harness cards \
			--corpus tables/cards/conformance --driver tables/cards/java/driver \
			--require $(CARDS_JAVA_REQUIRE) > build/cards-java-conformance-hash-neg.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: taskhash stayed green with needs added to the hash'; \
		cat build/cards-java-conformance-hash-neg.log; exit 1; fi
	@grep -qE 'cards taskhash .*FAIL' build/cards-java-conformance-hash-neg.log || \
		{ echo 'NEGATIVE CONTROL FAILED: taskhash did not go red'; cat build/cards-java-conformance-hash-neg.log; exit 1; }
	@grep -qE 'cards (load|save|render|parse|bounds|upgrade|edit|view) pass' build/cards-java-conformance-hash-neg.log || \
		{ echo 'NEGATIVE CONTROL FAILED: another surface went red'; cat build/cards-java-conformance-hash-neg.log; exit 1; }
	@echo 'cards Java hash negative control: needs added to the hash turns exactly taskhash red'

test-java: cards-java-test cards-java-conformance cards-java-conformance-negative-control \
	cards-java-conformance-upgrade-negative-control cards-java-conformance-hash-negative-control

# The earlier card-subset refusal control is retired: maps, optional/keyed
# values, unions, wide values, fixed-class roots and was renames now have
# positive native codec gates.
.PHONY: tables-java-variable-arms tables-java-variable-fixed-class
tables-java-variable-arms: bin/schema
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) go test ./internal/codegen/javatable/ \
		-count=1 -timeout 600s -v -run 'TestMapsAndUnionArmsOnTheTableWireJava|TestOtherVariableConstructsAreRefusedByName|TestJavaVariable|TestJavaFixedGeneralArmPayloads'

tables-java-variable-fixed-class: bin/schema
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) go test ./internal/codegen/javatable/ \
		-count=1 -timeout 600s -v -run '^TestFixedClassOnTableWireJava$$'

test-java: tables-java-variable-arms tables-java-variable-fixed-class

# tables-c-view through the Java surface: for every VIEW_CORPUS unit, the
# listing test/java-view/View.java prints from TableRegistry.unitView() must be
# byte-identical to the compiler's own (internal/viewlisting). The printer names
# no declaration; its one import line is rewritten to the unit's package. The
# doc and tag annotations of every row are what hold the doc-tags cell, and the
# NEGATIVE CONTROL damages one doc in a copy of tabledemo's listing and requires
# the comparison to go red.
.PHONY: tables-java-view
tables-java-view: bin/schema test/java-view/View.java
	@rm -rf build/java-view && mkdir -p build/java-view
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		source=tables/$$dir; \
		./bin/schema generate --lang java --out build/java-view/generated/$$dir $$source > build/java-view/$$pkg.generate.log 2>&1 || { cat build/java-view/$$pkg.generate.log; exit 1; }; \
		mkdir -p build/java-view/src-$$pkg build/java-view/classes-$$pkg; \
		sed "s/^import tabledemo\.\*; \/\/ UNIT$$/import $$pkg.*; \/\/ UNIT/" test/java-view/View.java > build/java-view/src-$$pkg/View.java; \
		grep -q "^import $$pkg\.\*; // UNIT$$" build/java-view/src-$$pkg/View.java || { echo "tables-java-view: the unit import was not rewritten for $$pkg"; exit 1; }; \
		$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-view/classes-$$pkg \
			build/java-view/generated/$$dir/*.java build/java-view/src-$$pkg/View.java; \
		$(JAVA) -cp build/java-view/classes-$$pkg View > build/java-view/$$pkg.listing; \
	done
	SCHEMA_VIEW_LISTING_DIR="$(CURDIR)/build/java-view" go test -count=1 -timeout 600s ./internal/viewlisting -run TestUnitViewListingMatchesTheIR
	@mkdir -p build/java-view/negative
	@sed '0,/ doc=[^ ]/s/ doc=\([^ ]\)/ doc=X\1/' build/java-view/tabledemo.listing > build/java-view/negative/tabledemo.listing
	@if cmp -s build/java-view/tabledemo.listing build/java-view/negative/tabledemo.listing; then echo "tables-java-view negative control damaged nothing"; exit 1; fi
	@if SCHEMA_VIEW_LISTING_DIR="$(CURDIR)/build/java-view/negative" SCHEMA_VIEW_LISTING_UNITS=tabledemo go test -count=1 -timeout 600s ./internal/viewlisting -run TestUnitViewListingMatchesTheIR > build/java-view/negative.log 2>&1; then echo "Java UnitView negative control escaped"; exit 1; fi
	@grep -q "listing is not the compiler's" build/java-view/negative.log
	@echo "Java UnitView: $(words $(VIEW_CORPUS)) units match the compiler's listing; a damaged doc is detected"

# The doc and tag columns of every descriptor and registry row (§8.1, §8.3):
# the Java runtime's strings are compared with the unit the compiler read, over
# an annotated unit and an unannotated one. It rides tables-java-view because
# the listing already prints the same two columns; this test names them one by
# one and says which declaration a wrong string belongs to.
.PHONY: tables-java-descriptor-doc
tables-java-descriptor-doc: bin/schema
	SCHEMA_REQUIRE_CORPUS=1 JAVAC=$(JAVAC) JAVA=$(JAVA) go test ./internal/codegen/javatable/ \
		-count=1 -timeout 600s -v -run '^TestJavaDescriptorDocAndTags$$'

test-java: tables-java-view tables-java-descriptor-doc

# THE FRAMING SURFACE, its Java leg (docs/LAYERS/1-framing.md): the Java driver
# answers every framing row and passes it, none absent
# (test/conformance/framing/gate). Its negative control recompiles the tblr1
# unit with the id table's 4096-entry cap taken out of trailerBase (§3, THE
# DISTINCTNESS CHECK; rule F6) and requires the framing surface to go RED on
# frame_entries_4097.
JAVA_FRAMING_CONTROL := build/conformance-java-framing-control
.PHONY: conformance-java-framing conformance-negative-control-java-framing
conformance-java-framing: build/conformance-harness build-conformance-java
	sh test/conformance/framing/gate java "$(JAVA) -cp $(CURDIR)/build/conformance-java Driver" build/conformance-java-framing
conformance-negative-control-java-framing: build/conformance-harness build-conformance-java
	@rm -rf $(JAVA_FRAMING_CONTROL) && mkdir -p $(JAVA_FRAMING_CONTROL)/src $(JAVA_FRAMING_CONTROL)/classes
	@cp build/tables-generated-java/r1/*.java $(JAVA_FRAMING_CONTROL)/src/
	@sed -i.orig 's/count < 0 || count > 4096 || count/count < 0 || count/' $(JAVA_FRAMING_CONTROL)/src/TableVariable.java
	@! cmp -s $(JAVA_FRAMING_CONTROL)/src/TableVariable.java $(JAVA_FRAMING_CONTROL)/src/TableVariable.java.orig || \
		{ echo "NEGATIVE CONTROL: the 4096-entry cap sabotage did not apply"; exit 1; }
	@rm $(JAVA_FRAMING_CONTROL)/src/TableVariable.java.orig
	$(JAVAC) --release 17 -nowarn -cp build/conformance-java -d $(JAVA_FRAMING_CONTROL)/classes $(JAVA_FRAMING_CONTROL)/src/*.java
	sh test/conformance/framing/gate java "$(JAVA) -cp $(CURDIR)/$(JAVA_FRAMING_CONTROL)/classes:$(CURDIR)/build/conformance-java Driver" \
		$(JAVA_FRAMING_CONTROL)/run frame_entries_4097
test-java: conformance-java-framing
