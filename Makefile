# schema — `make` builds the compiler (bin/schema) and nothing else: no
# serialize checkouts, no language toolchains, no generation. The full
# nine-language conformance chain is `make test`, and it needs the sibling
# runtime checkouts and toolchains documented below.
#
# THE TOOLCHAIN GATE (issue #599). `make test` REFUSES BY NAME when a pinned
# toolchain a make/<lang>.mk names does not resolve. It probes EVERY registered
# leg and names every one that does not resolve in the same run, with the path
# each pin looked in, rather than stopping at the first and making a fresh
# clone a queue, and it stops before the chain spends an hour on the legs that
# do resolve. A gate that passes while its toolchain is missing is a gate with
# no blade: that is how a red in the Dart leg once rode a green run, after a
# merge deleted a clone's dist link.
#
# To run the chain without a leg, name the skip ON PURPOSE:
#
#   make test SCHEMA_SKIP_LEGS=js,dart
#
# Every named leg prints its skip in the toolchain gate, in the conformance
# matrix, in the two-language gates and in the leg loop, and is not run.
# Nothing under .github sets the variable: certification installs each
# toolchain and overrides the pins (NODE=node, DART=dart, JAVA=java,
# JAVAC=javac, ELIXIR=elixir, MIX=mix), which resolve and pass the gate.
#
# `make toolchain` runs the gate alone. `make toolchain-negative-control`
# points every registered pin at a path that does not exist, ONE PIN AT A TIME
# (a leg with two pins is proved twice, a leg with three, three times), and
# requires each to refuse by the leg's name and that pin's, then names every
# leg in SCHEMA_SKIP_LEGS and requires the same gate to go green with each
# skip printed by name.

CXX      ?= c++
CXXFLAGS ?= -std=c++17 -Wall -Wextra -Werror -ffp-contract=off

# The formatting authority the C++ and C table legs are held to (issue #424).
CLANG_FORMAT ?= clang-format

# the classic serialize runtime the generated C++ targets (header-only), a
# sibling checkout. Every other language's runtime checkout and toolchain pin
# is that language's own: make/<lang>.mk (docs/CONTRIBUTING.md, "Adding a
# language").
SERIALIZE ?= ../serialize
CXXFLAGS  += -I$(SERIALIZE)

# THE SKIP LIST (issue #599): the legs whose toolchain is absent ON PURPOSE,
# comma separated, empty by default and empty in every .github workflow. A leg
# named here prints its skip and does not run; a leg NOT named here whose pin
# does not resolve stops `make test` by name.
SCHEMA_SKIP_LEGS ?=
skip_comma  := ,
skip_empty  :=
skip_space  := $(skip_empty) $(skip_empty)
# a literal hash, so a message held in a variable can cite an issue by number:
# an unescaped one starts a make comment and truncates the value in silence
skip_hash   := \#
SKIPPED_LEGS := $(subst $(skip_comma),$(skip_space),$(SCHEMA_SKIP_LEGS))

# $(call unless_skipped,<leg>,<text>) is the text, or nothing when the leg is
# named in SCHEMA_SKIP_LEGS. A leg registers its conformance prerequisites
# through it, so a named skip reaches the matrix and not only the leg loop.
unless_skipped = $(if $(filter $(1),$(SKIPPED_LEGS)),,$(2))

# $(call toolchain_probe,<leg>,<PIN>,<what the pin holds>[,<probe>]) is one
# line of a leg's toolchain-<leg> recipe. It says nothing when the pin
# resolves, and refuses by name when it does not: it names the leg, the pin,
# the path the pin looked in, and the three ways out. The default probe
# resolves the pin's own value; a leg whose pin carries an environment prefix
# passes its own probe as the fourth argument.
toolchain_probe = $(if $(4),$(4),command -v $(3)) >/dev/null 2>&1 || { \
		echo "make test REFUSES: the $(1) leg's pinned $(2) does not resolve"; \
		echo '  $(2) = $(3)'; \
		echo "  test-$(1) would have been SKIPPED, and a leg that skips is a gate with no blade (issue $(skip_hash)599)"; \
		echo "  make/$(1).mk names the pinned toolchain and where it comes from; install it,"; \
		echo "  or override the pin ($(2)=... make test), or name the skip on purpose:"; \
		echo "      make test SCHEMA_SKIP_LEGS=$(1)"; \
		exit 1; }

# cmd, internal, AND the public API packages: ir/ and compiler/ are compiled
# into bin/schema like any other source, and leaving them out made an edit to
# the layout model or the build version silently NOT rebuild the compiler every
# gate below then measures.
GO_SOURCES   := $(shell find cmd internal ir compiler -name '*.go') go.mod
# and every file the compiler embeds (go:embed): the runtimes the scripting
# targets copy beside their output. Without them an edit to a runtime left
# bin/schema stale and every generate re-emitted the old runtime.
# bin-schema-embeds checks it against `go list`'s own EmbedFiles.
EMBED_SOURCES := $(shell find internal/codegen -type f \( -path '*/runtime/*' -o -path '*/serialize/*' \) -not -name '*.go')
SCHEMAS      := $(wildcard examples/*.schema)
SCHEMAS128   := $(wildcard examples128/*.schema)
SCHEMAS_BENCH := $(wildcard bench/corpus/*.schema)
SCHEMAS_TABLES := $(wildcard tables/examples/*.schema)
SCHEMAS_TABLES_POINTERS := $(wildcard tables/pointers/*.schema)
SCHEMAS_TABLES_BLOCK := $(wildcard tables/block/*.schema) $(wildcard tables/blockhome/*.schema)
# the MESSAGE corpora (docs/SPEC-TABLES.md §2.6): a union whose arms are tables,
# fixed in tables/messages and with a variable arm in tables/stream
SCHEMAS_TABLES_MESSAGES := $(wildcard tables/messages/*.schema) $(wildcard tables/stream/*.schema)
# the BYTE BUFFER corpus (docs/SPEC-TABLES.md §2.5): a blob at its used size,
# pointed at — C++ and the tool carry it, every port refuses the unit by name
SCHEMAS_TABLES_BLOBS := $(wildcard tables/blobs/*.schema)
SCHEMAS_TABLES_SCALARS := $(wildcard tables/scalars/*.schema)
# the MAP corpus (docs/SPEC-TABLES.md §2.8)
SCHEMAS_TABLES_MAPS := $(wildcard tables/maps/*.schema)
# the UNBOUNDED ARRAY corpus (docs/SPEC-TABLES.md §2.9)
SCHEMAS_TABLES_LISTS := $(wildcard tables/lists/*.schema)
# the UNION-ARM TRAVERSAL corpus (docs/SPEC-TABLES.md §2.6, §2.9, §3.1)
SCHEMAS_TABLES_ARMS := $(wildcard tables/arms/*.schema)
# the MESSAGE FORM's corpora (docs/SPEC-TABLES.md §3.3): the three backend
# messages the ruling measured, and the WIDE-VOCABULARY unit test/vocabgen
# writes, whose vocabulary passes 127 ids so its message names slots on both
# sides of the one-byte boundary
SCHEMAS_TABLES_BACKEND := $(wildcard tables/backend/*.schema)
SCHEMAS_TABLES_VOCAB := $(wildcard tables/vocab/*.schema)
SCHEMAS_TABLES_VOCAB9 := $(wildcard tables/vocab9/*.schema)

# EVERY SOAK IS COUNTED, NOT CLOCKED: each leg's soak takes a number of passes,
# because a pass is what the allocation count is read across, because a count
# runs the same loop on every machine, and because `make shape-gate` refuses a
# timing primitive outside the sanctioned bench. Each default is the release
# act, about an hour on the studio; `make test` runs the short forms with an
# explicit value. (Java's and Dart's sit in make/java.mk and make/dart.mk
# beside their targets.)
TABLES_C_SOAK_ITERATIONS ?= 800000
TABLES_GO_SOAK_ITERATIONS ?= 600000
TABLES_CPP_SOAK_ITERATIONS ?= 1000000
TABLES_CS_SOAK_ITERATIONS ?= 250000000
TABLES_RUST_SOAK_ITERATIONS ?= 800000000

all: bin/schema

# The version the built binary reports: `git describe` over v tags only (a
# main-<shorthash> promotion tag is not a version; docs/VERSIONING.md), the exact
# tag on a release build and tag-commits-hash elsewhere; outside a git checkout
# the stamp is left empty and internal/version falls back to the build info.
VERSION ?= $(shell git describe --tags --match 'v[0-9]*' --dirty 2>/dev/null)
LDFLAGS := $(if $(VERSION),-X github.com/mas-bandwidth/schema/v2/internal/version.version=$(VERSION),)

bin/schema: $(GO_SOURCES) $(EMBED_SOURCES)
	go build -ldflags "$(LDFLAGS)" -o $@ ./cmd/schema

# bin-schema-embeds: for one embedded file of every package that embeds
# (go list's EmbedFiles, so the list cannot drift from the go:embed lines), a
# newer mtime on that file makes bin/schema out of date. `make -q` asks
# without building; the last step rebuilds once.
.PHONY: bin-schema-embeds
bin-schema-embeds: bin/schema
	@n=0; for f in $$(go list -deps -f '{{$$d := .Dir}}{{range .EmbedFiles}}{{$$d}}/{{.}}{{"\n"}}{{break}}{{end}}' ./cmd/schema); do \
		n=$$((n+1)); touch "$$f"; \
		if $(MAKE) --no-print-directory -q bin/schema; then \
			echo "bin-schema-embeds FAILED: touching $$f left bin/schema up to date"; exit 1; fi; \
		touch -r "$$f" bin/schema; \
	done; \
	test $$n -gt 0 || { echo "bin-schema-embeds FAILED: go list named no embedded file"; exit 1; }; \
	echo "bin-schema-embeds: $$n embedding package(s), a newer file in each rebuilds bin/schema"
	@$(MAKE) --no-print-directory bin/schema

test: bin-schema-embeds

# Its control: with EMBED_SOURCES emptied on the command line, a touched
# runtime leaves bin/schema up to date and bin-schema-embeds must go red.
.PHONY: bin-schema-embeds-negative-control
bin-schema-embeds-negative-control: bin/schema
	@mkdir -p build
	@if $(MAKE) --no-print-directory bin-schema-embeds EMBED_SOURCES= > build/bin-schema-embeds-control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: with no embedded file a prerequisite, bin-schema-embeds stayed green"; cat build/bin-schema-embeds-control.log; exit 1; fi
	@grep -q 'bin-schema-embeds FAILED: touching .* left bin/schema up to date' build/bin-schema-embeds-control.log || \
		{ echo "NEGATIVE CONTROL FAILED: bin-schema-embeds went red for another reason"; cat build/bin-schema-embeds-control.log; exit 1; }
	@$(MAKE) --no-print-directory bin/schema
	@echo "negative control: without the embedded files as prerequisites, a touched runtime leaves bin/schema stale and bin-schema-embeds goes RED"

# one generate run emits every header; the stamp carries the dependency
generated/cpp/.stamp: bin/schema $(SCHEMAS)
	./bin/schema generate --lang cpp --out generated/cpp examples
	@touch $@

# the fixed-point + 128-bit unit (examples128/) — all eight targets since the
# serialize ports carry the phase-1 surface; each generated unit gets the same
# module/manifest wiring as its main-corpus twin
generated/cpp/ludicrous/.stamp: bin/schema $(SCHEMAS128)
	./bin/schema generate --lang cpp --out generated/cpp/ludicrous examples128
	@touch $@

build/schema_test: generated/cpp/.stamp test/main.cpp test/second.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp -Itest test/main.cpp test/second.cpp -o $@

# The two-unit guard regression (issue #189): two packages, both with
# string(N) fields, included into ONE translation unit. Its corpus is
# test-only — generated at build time into build/, never part of the
# committed generated/ tree (one package per generate call, SPEC §3.2).
build/guard-generated/.stamp: bin/schema test/guard/Alpha.schema test/guard/Beta.schema
	@mkdir -p build/guard-generated
	./bin/schema generate --lang cpp --out build/guard-generated test/guard/Alpha.schema
	./bin/schema generate --lang cpp --out build/guard-generated test/guard/Beta.schema
	@touch $@

build/schema_test_guard: build/guard-generated/.stamp test/guard/main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Ibuild/guard-generated test/guard/main.cpp -o $@

# The tables corpus (docs/SPEC-TABLES.md): the tabledemo unit plus the
# two-generation evolution pair (tblv1/tblv2), generated at build time into
# build/ — test-only, never part of the committed generated/ tree.
#
# The generate step and the include path are parameterised by generator binary
# and output root, because the big-endian negative control below regenerates
# the WHOLE corpus from a sabotaged emitter: a second copy of these lists would
# be a second corpus, and the gate would stop covering what the leg covers.
# ---- rowan/cpp-versioning-numbers: BEGIN --------------------------------
# THE VERSIONING LAW'S SCHEMAS (docs/FIXED-FORM-VERSIONING-TESTS.md). One PAIR
# per row of the law, differing by EXACTLY that row, same table name in two
# packages so both generations compile into one binary. Every one of them goes
# into ONE generated directory: the emitter names its files after the SCHEMA,
# so 35 units share a directory without colliding and the test binaries take one
# -I instead of 35.
SCHEMAS_VERSIONING := test/tables/VOLD_array_bounded_grow.schema \
	test/tables/VOLD_array_fixed_grow.schema \
	test/tables/VOLD_array_elem_widen.schema \
	test/tables/VOLD_constant_grow.schema \
	test/tables/VOLD_string_grow.schema \
	test/tables/VOLD_bytes_grow.schema \
	test/tables/VOLD_int_widen.schema \
	test/tables/VOLD_uint_widen.schema \
	test/tables/VOLD_float_widen.schema \
	test/tables/VOLD_range_widen.schema \
	test/tables/VOLD_cfloat_range_widen.schema \
	test/tables/VOLD_cfloat_res_refine.schema \
	test/tables/VOLD_bits_grow.schema \
	test/tables/VOLD_fixed_I_grow.schema \
	test/tables/VOLD_fixed_I_grow_element.schema \
	test/tables/VOLD_optional_add.schema \
	test/tables/VNEW_array_bounded_grow.schema \
	test/tables/VNEW_array_fixed_grow.schema \
	test/tables/VNEW_array_elem_widen.schema \
	test/tables/VNEW_constant_grow.schema \
	test/tables/VNEW_string_grow.schema \
	test/tables/VNEW_bytes_grow.schema \
	test/tables/VNEW_int_widen.schema \
	test/tables/VNEW_uint_widen.schema \
	test/tables/VNEW_float_widen.schema \
	test/tables/VNEW_range_widen.schema \
	test/tables/VNEW_cfloat_range_widen.schema \
	test/tables/VNEW_cfloat_res_refine.schema \
	test/tables/VNEW_bits_grow.schema \
	test/tables/VNEW_fixed_I_grow.schema \
	test/tables/VNEW_fixed_I_grow_element.schema \
	test/tables/VNEW_optional_add.schema \
	test/tables/VOLD_hostile_bool.schema \
	test/tables/VNEW_hostile_bool.schema \
	test/tables/VOLD_floor.schema \
	test/tables/VMID_floor.schema \
	test/tables/VNEW_floor.schema \
	test/tables/VOLD_lineage_merge.schema \
	test/tables/VBRA_lineage_merge.schema \
	test/tables/VBRB_lineage_merge.schema \
	test/tables/VNEW_lineage_merge.schema \
	test/tables/VOLD_unknown_census.schema \
	test/tables/VNEW_unknown_census.schema
# ---- rowan/cpp-versioning-numbers: END ----------------------------------

define tables_generate
	$(1) generate --lang cpp --out $(2)/examples tables/examples
	$(1) generate --lang cpp --out $(2)/pointers tables/pointers
	$(1) generate --lang cpp --out $(2)/block tables/block
	$(1) generate --lang cpp --out $(2)/blockhome tables/blockhome
	$(1) generate --lang cpp --out $(2)/messages tables/messages
	$(1) generate --lang cpp --out $(2)/stream tables/stream
	$(1) generate --lang cpp --out $(2)/blobs tables/blobs
	$(1) generate --lang cpp --out $(2)/v1 test/tables/V1.schema
	$(1) generate --lang cpp --out $(2)/v2 test/tables/V2.schema
	$(1) generate --lang cpp --out $(2)/ut1 test/tables/UT1.schema
	$(1) generate --lang cpp --out $(2)/ut2 test/tables/UT2.schema
	$(1) generate --lang cpp --out $(2)/fn1 test/tables/FN1.schema
	$(1) generate --lang cpp --out $(2)/hb test/tables/HB.schema
	$(1) generate --lang cpp --out $(2)/fn2 test/tables/FN2.schema
	$(1) generate --lang cpp --out $(2)/fu1 test/tables/FU1.schema
	$(1) generate --lang cpp --out $(2)/fu2 test/tables/FU2.schema
	$(1) generate --lang cpp --out $(2)/fh1 test/tables/FH1.schema
	$(1) generate --lang cpp --out $(2)/fh2 test/tables/FH2.schema
	$(1) generate --lang cpp --out $(2)/fg1 test/tables/FG1.schema
	$(1) generate --lang cpp --out $(2)/fe1 test/tables/FE1.schema
	$(1) generate --lang cpp --out $(2)/fe2 test/tables/FE2.schema
	$(1) generate --lang cpp --out $(2)/fm1 test/tables/FM1.schema
	$(1) generate --lang cpp --out $(2)/fm2 test/tables/FM2.schema
	$(1) generate --lang cpp --out $(2)/p1 test/tables/P1.schema
	$(1) generate --lang cpp --out $(2)/p2 test/tables/P2.schema
	$(1) generate --lang cpp --out $(2)/p3 test/tables/P3.schema
	$(1) generate --lang cpp --out $(2)/jsonkeys test/tables/JsonKeys.schema
	$(1) generate --lang cpp --out $(2)/m1 test/tables/M1.schema
	$(1) generate --lang cpp --out $(2)/m2 test/tables/M2.schema
	$(1) generate --lang cpp --out $(2)/a1 test/tables/A1.schema
	$(1) generate --lang cpp --out $(2)/g1 test/tables/G1.schema
	$(1) generate --lang cpp --out $(2)/k1 test/tables/K1.schema
	$(1) generate --lang cpp --out $(2)/k2 test/tables/K2.schema
	$(1) generate --lang cpp --out $(2)/a2 test/tables/A2.schema
	$(1) generate --lang cpp --out $(2)/w1 test/tables/W1.schema
	$(1) generate --lang cpp --out $(2)/w2 test/tables/W2.schema
	$(1) generate --lang cpp --out $(2)/r1 test/tables/R1.schema
	$(1) generate --lang cpp --out $(2)/r2 test/tables/R2.schema
	# THE FLOAT BIT-PATTERN PAIR (docs/SPEC-TABLES.md §3, §4, schema#480): the
	# root carrying a signalling NaN and a payload NaN, and the same root one
	# generation on with the float32 fields respelled float64 — the widened rung
	$(1) generate --lang cpp --out $(2)/f1 test/tables/F1.schema
	$(1) generate --lang cpp --out $(2)/f2 test/tables/F2.schema
	# THE LIST UNIT WITH NO MAP IN IT (docs/SPEC-TABLES.md §2.9, schema#380):
	# where the tool's cook half and the reference's are held to one artifact
	$(1) generate --lang cpp --out $(2)/l1 test/tables/L1.schema
	# THE FIXED FORM'S VERSIONING PAIR (docs/SPEC-TABLES.md §3.4): a widened
	# field, a rename under `was`, a field each side does not have, and a whole
	# nested TYPE the older side has no name for
	$(1) generate --lang cpp --out $(2)/fx1 test/tables/FX1.schema
	$(1) generate --lang cpp --out $(2)/fx2 test/tables/FX2.schema
	# ==== BEGIN rowan/cpp-versioning-lists ====
	# THE LIST ROWS OF THE FIXED FORM'S VERSIONING LAW
	# (docs/FIXED-FORM-VERSIONING-TESTS.md). One pair of schemas per row,
	# the SAME table name on both sides (one lineage) and a package each, so
	# both generations compile into one test binary — the FX1/FX2 precedent.
	# The reference's two read columns are test/tables/versioning_lists.cpp.
	$(1) generate --lang cpp --out $(2)/vold_field_append test/tables/VOLD_field_append.schema
	$(1) generate --lang cpp --out $(2)/vnew_field_append test/tables/VNEW_field_append.schema
	$(1) generate --lang cpp --out $(2)/vold_field_deprecate test/tables/VOLD_field_deprecate.schema
	$(1) generate --lang cpp --out $(2)/vnew_field_deprecate test/tables/VNEW_field_deprecate.schema
	$(1) generate --lang cpp --out $(2)/vold_field_undeprecate test/tables/VOLD_field_undeprecate.schema
	$(1) generate --lang cpp --out $(2)/vnew_field_undeprecate test/tables/VNEW_field_undeprecate.schema
	$(1) generate --lang cpp --out $(2)/vold_enum_append test/tables/VOLD_enum_append.schema
	$(1) generate --lang cpp --out $(2)/vnew_enum_append test/tables/VNEW_enum_append.schema
	$(1) generate --lang cpp --out $(2)/vold_enum_width test/tables/VOLD_enum_width.schema
	$(1) generate --lang cpp --out $(2)/vnew_enum_width test/tables/VNEW_enum_width.schema
	$(1) generate --lang cpp --out $(2)/vold_union_append test/tables/VOLD_union_append.schema
	$(1) generate --lang cpp --out $(2)/vnew_union_append test/tables/VNEW_union_append.schema
	$(1) generate --lang cpp --out $(2)/vold_union_arm_payload_widen test/tables/VOLD_union_arm_payload_widen.schema
	$(1) generate --lang cpp --out $(2)/vnew_union_arm_payload_widen test/tables/VNEW_union_arm_payload_widen.schema
	$(1) generate --lang cpp --out $(2)/vold_flags_append test/tables/VOLD_flags_append.schema
	$(1) generate --lang cpp --out $(2)/vnew_flags_append test/tables/VNEW_flags_append.schema
	$(1) generate --lang cpp --out $(2)/vold_keyed_array_enum_append test/tables/VOLD_keyed_array_enum_append.schema
	$(1) generate --lang cpp --out $(2)/vnew_keyed_array_enum_append test/tables/VNEW_keyed_array_enum_append.schema
	$(1) generate --lang cpp --out $(2)/vold_nested_append test/tables/VOLD_nested_append.schema
	$(1) generate --lang cpp --out $(2)/vnew_nested_append test/tables/VNEW_nested_append.schema
	$(1) generate --lang cpp --out $(2)/vold_rename_without_was test/tables/VOLD_rename_without_was.schema
	$(1) generate --lang cpp --out $(2)/vnew_rename_without_was test/tables/VNEW_rename_without_was.schema
	# ==== END rowan/cpp-versioning-lists ====
	$(1) generate --lang cpp --out $(2)/scalars tables/scalars
	$(1) generate --lang cpp --out $(2)/maps tables/maps
	$(1) generate --lang cpp --out $(2)/lists tables/lists
	$(1) generate --lang cpp --out $(2)/arms tables/arms
	$(1) generate --lang cpp --out $(2)/scalars2 test/tables/Scalars2.schema
	$(1) generate --lang cpp --out $(2)/backend tables/backend
	$(1) generate --lang cpp --out $(2)/vocab tables/vocab
	$(1) generate --lang cpp --out $(2)/vocab9 tables/vocab9
	$(1) generate --lang cpp --out $(2)/bases test/tables/Bases.schema
	$(1) generate --lang cpp --out $(2)/rt1 test/tables/RT1.schema
	$(1) generate --lang cpp --out $(2)/rt2 test/tables/RT2.schema
	$(1) generate --lang cpp --out $(2)/rt3 test/tables/RT3.schema
	# ---- rowan/cpp-versioning-numbers: BEGIN ----
	# the versioning law's rows, every unit into ONE directory (see SCHEMAS_VERSIONING)
	$(foreach sc,$(SCHEMAS_VERSIONING),$(1) generate --lang cpp --out $(2)/vnum $(sc)
	)
	# ---- rowan/cpp-versioning-numbers: END ----
endef

tables_includes = -I$(1)/examples -I$(1)/pointers -I$(1)/block -I$(1)/blockhome -Itest/tables \
	-I$(1)/v1 -I$(1)/v2 -I$(1)/p1 -I$(1)/p2 -I$(1)/p3 -I$(1)/jsonkeys \
	-I$(1)/messages -I$(1)/stream -I$(1)/blobs -I$(1)/m1 -I$(1)/m2 -I$(1)/a1 -I$(1)/a2 -I$(1)/g1 -I$(1)/k1 -I$(1)/k2 -I$(1)/w1 -I$(1)/w2 -I$(1)/r1 -I$(1)/r2 -I$(1)/f1 -I$(1)/f2 -I$(1)/l1 -I$(1)/scalars -I$(1)/scalars2 -I$(1)/maps -I$(1)/lists -I$(1)/arms -I$(1)/backend -I$(1)/vocab -I$(1)/vocab9 -I$(1)/bases -I$(1)/rt1 -I$(1)/rt2 -I$(1)/rt3 -I$(SERIALIZE)

build/tables-generated/.stamp: bin/schema $(SCHEMAS_TABLES) $(SCHEMAS_TABLES_POINTERS) $(SCHEMAS_TABLES_BLOCK) $(SCHEMAS_TABLES_MESSAGES) $(SCHEMAS_TABLES_BLOBS) $(SCHEMAS_TABLES_SCALARS) $(SCHEMAS_TABLES_MAPS) $(SCHEMAS_TABLES_LISTS) $(SCHEMAS_TABLES_ARMS) $(SCHEMAS_TABLES_BACKEND) $(SCHEMAS_TABLES_VOCAB) $(SCHEMAS_TABLES_VOCAB9) test/tables/V1.schema test/tables/V2.schema test/tables/P1.schema test/tables/P2.schema test/tables/P3.schema test/tables/JsonKeys.schema test/tables/M1.schema test/tables/M2.schema test/tables/A1.schema test/tables/A2.schema test/tables/G1.schema test/tables/K1.schema test/tables/K2.schema test/tables/W1.schema test/tables/W2.schema test/tables/R1.schema test/tables/R2.schema test/tables/F1.schema test/tables/F2.schema test/tables/L1.schema test/tables/Scalars2.schema test/tables/Bases.schema test/tables/RT1.schema test/tables/RT2.schema test/tables/RT3.schema test/tables/FX1.schema test/tables/FX2.schema test/tables/UT1.schema test/tables/UT2.schema test/tables/FN1.schema test/tables/FN2.schema test/tables/HB.schema test/tables/FU1.schema test/tables/FU2.schema test/tables/FH1.schema test/tables/FH2.schema test/tables/FG1.schema test/tables/FE1.schema test/tables/FE2.schema test/tables/FM1.schema test/tables/FM2.schema test/tables/VOLD_field_append.schema test/tables/VNEW_field_append.schema test/tables/VOLD_field_deprecate.schema test/tables/VNEW_field_deprecate.schema test/tables/VOLD_field_undeprecate.schema test/tables/VNEW_field_undeprecate.schema test/tables/VOLD_enum_append.schema test/tables/VNEW_enum_append.schema test/tables/VOLD_enum_width.schema test/tables/VNEW_enum_width.schema test/tables/VOLD_union_append.schema test/tables/VNEW_union_append.schema test/tables/VOLD_union_arm_payload_widen.schema test/tables/VNEW_union_arm_payload_widen.schema test/tables/VOLD_flags_append.schema test/tables/VNEW_flags_append.schema test/tables/VOLD_keyed_array_enum_append.schema test/tables/VNEW_keyed_array_enum_append.schema test/tables/VOLD_nested_append.schema test/tables/VNEW_nested_append.schema test/tables/VOLD_rename_without_was.schema test/tables/VNEW_rename_without_was.schema $(SCHEMAS_VERSIONING)
	@mkdir -p build/tables-generated
	$(call tables_generate,./bin/schema,build/tables-generated)
	@touch $@

# The ZERO-COST GATE (docs/SPEC-TABLES.md): a table with no pointer in its by-value
# closure must pay NOTHING for the pointer machinery — no builder, no arena, no
# handles, no lifecycle surface, no extra descriptor columns. The pointer-free
# corpus's generated headers must not contain one symbol of it. (The stronger
# one-time proof, byte-identical emission against the pre-pointer baseline,
# lives in git history and in no page of this tree; this is the standing gate.)
#
# THE MAP MACHINERY TAKES THE SAME GATE (docs/SPEC-TABLES.md §2.2, §2.8): "not
# one symbol of the map machinery in a map-free unit's generated headers, held
# by the zero-cost gate's header scan — with the map symbols added to its
# list." A map makes its holder variable-length, so every unit scanned here is
# map-free by construction, and the map symbols below say so mechanically
# rather than by inspection.
#
# THE SCAN IS BY SYMBOL, not by line, and `TableNode` is matched with its whole
# spelling so a node symbol nobody has written yet is still refused. Exactly one
# of those spellings is ALLOWED in a pointer-free unit and it is named below.
# TableRefuseReason IS SANCTIONED, and it is the FORM's vocabulary rather than
# the map's or the list's: every unit's cook Open names its refusal in it
# (docs/SPEC-TABLES.md §7, §11), so every unit carries it. It stays in the list
# below BECAUSE `TableRef`, the pointer handle, is a PREFIX of it, and the
# extraction takes the longest alternative at a position, so naming the whole
# spelling here is what makes the token that comes out `TableRefuseReason`
# rather than `TableRef`, and the sanctioned list below is what then clears it.
# Its two map-and-list values, count_over_length and count_over_extent_cap, are
# out of the list for the same reason the enum is sanctioned: they are the
# enum's members and every unit spells them.
TABLES_ZERO_COST_SYMBOLS := TableArena|TableSlot|TableWorker|TableRef|TableRefuseReason|TableRegion|kTableSegment|kTableSlab|TablePack|[A-Za-z_]*TableNode[A-Za-z_]*|is_pointer|Builder|PackMeasure|LoadMeasure|TableBlob|TableBytesView|TableStringView|AllocBytes|AllocString|BytesEmplace|StringEmplace|TableMap|TableMapHead|TableMapSegment|TableMapOrder|TableMapCursor|TableEntryKey|TableKeyOrder|TableResetMapValue|TableEntrySetKey|kTableMapSegment|TableList|TableListHead|TableListSegment|TableListCursor|TableListElements|TableListPlace|kTableListSegment|TableExtentCarve|TableExtentUnreachedEmpty|TableWireExtent

# THE ONE NODE SPELLING A POINTER-FREE UNIT CARRIES, and it is the FORM's and
# not the pointer machinery's (docs/SPEC-TABLES.md §3, §3.1): the reserved
# node-table id. Every reader of the id-table form owes §3.1's refusal of that
# id inside a NESTED body, whether or not its own closure has a pointer,
# because the body it is handed may have been written by a unit that does have
# one (§4). What it costs a pointer-free unit is one `static const uint64_t`
# and one comparison: no arena, no builder, no handle, no lifecycle surface and
# no extra descriptor column, which is the claim this gate holds. Every other
# TableNode spelling stays refused, and
# tables-zero-cost-negative-control shows that it does.
#
# THE SECOND SANCTIONED SPELLING is the refusal enum, for the reason above it:
# a pointer-free unit's Open answers a TableRefuseReason beside its null, which
# costs the unit one enum and one out-parameter and no machinery at all.
TABLES_ZERO_COST_ALLOWED := kTableNodeTableFieldId|TableRefuseReason

# THE SCANNED UNITS, AND WHY `examples` IS NOT ONE OF THEM ANY MORE (#823).
# The property this gate holds is stated of a UNIT — "in a unit whose tables are
# all fixed-size the generated output carries no builder, no arena, no reference
# type, no lifecycle surface" (docs/SPEC-TABLES.md §2.2) — and under the KEYWORD
# `tables/examples` is no longer such a unit: `Guarded.schema`'s `Patrol` is a
# variable table, because a guarded branch is exactly the construct that keeps it
# one, so the whole unit is a variable unit and carries the machinery by right.
# Scanning it would be this gate asserting something the declaration has made
# false, which is worse than not scanning it.
#
# THE FOLLOW-ON THAT WOULD GIVE THE GATE ITS BIGGEST SUBJECT BACK is to move
# `Guarded.schema` into a UNIT OF ITS OWN — the guarded corpus is the VARIABLE
# class's fixture and does not belong in the pointer-free demo unit — at which
# point `examples` returns to this list unchanged. That is a corpus move with a
# blast radius (a package rename through `test/tables/main.cpp`, the goldens,
# and the tabletext and tablepack suites that load `tables/examples` by name),
# so it is named here rather than done under a wire branch.
TABLES_ZERO_COST_HEADERS := build/tables-generated/v1/*Table.h \
	build/tables-generated/v2/*Table.h build/tables-generated/p1/*Table.h \
	build/tables-generated/p3/*Table.h \
	build/tables-generated/messages/*Table.h build/tables-generated/m1/*Table.h \
	build/tables-generated/m2/*Table.h build/tables-generated/scalars/*Table.h

.PHONY: tables-zero-cost
tables-zero-cost: build/tables-generated/.stamp
	@for f in $(TABLES_ZERO_COST_HEADERS); do \
		if grep -ohE "$(TABLES_ZERO_COST_SYMBOLS)" $$f | grep -vxE "$(TABLES_ZERO_COST_ALLOWED)" | sort -u | grep -q .; then \
			grep -nE "$(TABLES_ZERO_COST_SYMBOLS)" $$f | grep -vE "$(TABLES_ZERO_COST_ALLOWED)"; \
			echo "ZERO-COST GATE FAILED: pointer, map or list machinery leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "tables zero-cost gate: value-only tables carry no pointer, map or list machinery"

# THE NEGATIVE CONTROL. The gate above sanctions ONE node spelling, so it owes a
# demonstration that it still refuses the others: the nearest neighbour of the
# sanctioned constant is planted in a COPY of a scanned header, and the same
# scan must refuse it. Nothing tracked is written to.
.PHONY: tables-zero-cost-negative-control
tables-zero-cost-negative-control: build/tables-generated/.stamp
	@rm -rf build/zero-cost-control && mkdir -p build/zero-cost-control
	@cp build/tables-generated/scalars/ScalarsTable.h build/zero-cost-control/GuardedTable.h
	@printf 'static const TableNodeMap * kPlanted = NULL; /* PLANTED */\n' >> build/zero-cost-control/GuardedTable.h
	@if ! grep -ohE "$(TABLES_ZERO_COST_SYMBOLS)" build/zero-cost-control/GuardedTable.h \
			| grep -vxE "$(TABLES_ZERO_COST_ALLOWED)" | sort -u | grep -q .; then \
		echo "NEGATIVE CONTROL FAILED: a planted node symbol left the zero-cost scan green"; exit 1; \
	fi
	@grep -ohE "$(TABLES_ZERO_COST_SYMBOLS)" build/zero-cost-control/GuardedTable.h | grep -vxE "$(TABLES_ZERO_COST_ALLOWED)" | sort -u
	@echo "negative control: a planted node symbol turns the zero-cost scan RED, and the reserved id alone does not"

# THE ACCESSOR/DESCRIPTOR AGREEMENT GATE, the C++ half of the JavaScript J1
# technique (docs/PORTING.md, schema#421): the generated accessor and the
# generated descriptor are two independent derivations of one layout, so the
# leg reads every field of a block and of a cook both ways and requires
# agreement — including a pointer's SLOT, whose position is what a
# self-relative delta is relative to (§6.3). The two controls move one
# derivation — a generated block projection scalar four bytes, a cook pointer
# slot eight — and each must turn the gate red, or the accessor half could be
# reading the descriptors twice and nobody would know. The C++ half generates,
# compiles and runs a probe of the reference emitter.
.PHONY: tables-accessor-descriptor-agreement tables-accessor-negative-control tables-slot-negative-control
tables-accessor-descriptor-agreement:
	SCHEMA_SLOW=1 go test ./internal/codegen/cpptable -run '^TestCppAccessorDescriptorAgreement$$' -count=1
tables-accessor-negative-control:
	SCHEMA_SLOW=1 go test ./internal/codegen/cpptable -run '^TestCppAccessorDescriptorScalarNegativeControl$$' -count=1
tables-slot-negative-control:
	SCHEMA_SLOW=1 go test ./internal/codegen/cpptable -run '^TestCppAccessorDescriptorSlotNegativeControl$$' -count=1

.PHONY: tables-json-walk
tables-json-walk: build/tables-generated/.stamp
	@rm -rf build/json-walk && mkdir -p build/json-walk
	@for f in build/tables-generated/*/*Table.cpp; do \
		out=build/json-walk/$$(echo $$f | tr / _); \
		awk '/---- json walk: begin ----/,/---- json walk: end ----/' $$f > $$out; \
		if [ ! -s $$out ]; then echo "GENERIC-WALK GATE FAILED: no walker in $$f"; exit 1; fi; \
	done
	@first=""; for f in build/json-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GENERIC-WALK GATE FAILED: the walker in $$f is not the walker in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables generic-walk gate: one walker, byte-identical in $$(ls build/json-walk | wc -l | tr -d ' ') generated .cpp files"

# THE GRAPH-WALK GATE (docs/SPEC-TABLES.md §16.7): the VARIABLE class's half of
# the text form is emitted only in a unit that carries a VARIABLE table, and it
# is ONE half too — the same bytes in every such .cpp of the corpus, on the
# walk's own terms — and none of it reaches an ALL-FIXED unit, which is the
# zero-cost property (§2.2) holding for the text form.
#
# "VARIABLE", not "pointered", and the two stopped being the same word at #823.
# The variable class stores through the arena and the region whether or not it
# has a pointer in it, so a variable table's own `FromJson` names the graph
# half's entry points — which is why `tables/examples` moved from the second
# list to the first here: `Guarded.schema`'s `Patrol` is a variable table, and a
# guarded branch is exactly the construct that keeps it one. The gate is
# UNCHANGED in strength: every unit is still on one list or the other, and the
# byte comparison across the first list now spans one unit more.
.PHONY: tables-json-graph-walk
tables-json-graph-walk: build/tables-generated/.stamp
	@rm -rf build/json-graph-walk && mkdir -p build/json-graph-walk
	@for f in build/tables-generated/pointers/*Table.cpp build/tables-generated/p2/*Table.cpp build/tables-generated/blobs/*Table.cpp build/tables-generated/examples/*Table.cpp; do \
		out=build/json-graph-walk/$$(echo $$f | tr / _); \
		awk '/---- json graph walk: begin ----/,/---- json graph walk: end ----/' $$f > $$out; \
		if [ ! -s $$out ]; then echo "GRAPH-WALK GATE FAILED: no graph half in $$f"; exit 1; fi; \
	done
	@for f in build/tables-generated/v1/*Table.cpp build/tables-generated/p1/*Table.cpp; do \
		if grep -q -- '---- json graph walk: begin ----' $$f; then \
			echo "GRAPH-WALK GATE FAILED: the graph half leaked into the all-fixed $$f"; exit 1; fi; \
	done
	@first=""; for f in build/json-graph-walk/*; do \
		if [ -z "$$first" ]; then first=$$f; else \
			cmp -s $$first $$f || { echo "GRAPH-WALK GATE FAILED: the graph half in $$f is not the one in $$first"; exit 1; }; \
		fi; \
	done
	@echo "tables graph-walk gate: one graph half, byte-identical in $$(ls build/json-graph-walk | wc -l | tr -d ' ') variable-class .cpp files"

# The NEGATIVE CONTROL for the walk (docs/SPEC-TABLES.md §16.5). A green round-trip
# suite proves nothing until the suite is shown capable of going red: the
# walker's field-offset arithmetic is sabotaged by one field width, and the
# round trip must FAIL on the first table with two fields. Attachment is that
# table — two four-byte fields at offsets 0 and 4 — so the sabotage swaps them
# and stays inside the struct.
.PHONY: tables-json-negative-control
tables-json-negative-control: bin/schema test/tables/json_negative_main.cpp
	@rm -rf build/json-sabotage && mkdir -p build/json-sabotage
	./bin/schema generate --lang cpp --out build/json-sabotage tables/examples
	@sed -i.bak 's|const uint8_t \* storage = (const uint8_t \*) base + f->offset;|const uint8_t * storage = (const uint8_t *) base + ( f->offset ^ 4 );|' build/json-sabotage/TablesTable.cpp
	@grep -q 'f->offset ^ 4' build/json-sabotage/TablesTable.cpp || { echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-sabotage test/tables/json_negative_main.cpp build/json-sabotage/TablesTable.cpp -o build/schema_test_json_negative
	./build/schema_test_json_negative

# THE DOC AND TAGS COST OBSERVABLES, AND THE TWO SAME-DECLARATION PAIRS
# (docs/SPEC-TABLES.md §8.7). The two cost claims §8.1 makes about the
# annotation columns are held by observables, not by inspection: a printer
# concatenates every doc with no null test, and every absent doc in a unit
# compares equal by address to the unit's one TableDocNone. The walk has two
# halves — the DESCRIPTOR half over each declaration's closure and the REGISTRY
# half over every row UnitView() hands out (§8.3) — and it holds the general
# ARM pair and the TYPE pair to each other where it meets them. The binary
# carries all four and its own account of what it walked.
.PHONY: tables-doctags
tables-doctags: bin/schema test/tables/doctags_main.cpp
	@rm -rf build/doctags build/doctags-arms && mkdir -p build/doctags
	./bin/schema generate --lang cpp --out build/doctags tables/examples
	./bin/schema generate --lang cpp --out build/doctags-arms tables/arms
	$(CXX) $(CXXFLAGS) -Ibuild/doctags -DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo -DVIEW_EXHIBIT \
		test/tables/doctags_main.cpp build/doctags/TablesTable.cpp build/doctags/TabledemoView.cpp -o build/schema_test_doctags
	./build/schema_test_doctags
	# THE GENERAL ARM PAIR needs a general arm, and the exhibit declares none:
	# adding one to tabledemo would move that unit's union tag range, its wire
	# goldens and its protocol id, which the page forbids the exhibit doing
	# (§8.7). armdemo declares one already, and its `plain` arm carries a doc
	# comment and a tag, so both halves of the pair are non-empty there.
	$(CXX) $(CXXFLAGS) -Ibuild/doctags-arms -Itest/tables -DVIEW_HEADER='"ArmdemoView.h"' -DVIEW_NAMESPACE=armdemo \
		test/tables/doctags_main.cpp build/doctags-arms/CarryTable.cpp build/doctags-arms/GateTable.cpp \
		build/doctags-arms/NestTable.cpp build/doctags-arms/RingTable.cpp build/doctags-arms/ArmdemoView.cpp \
		-o build/schema_test_doctags_arms
	./build/schema_test_doctags_arms

# THE NEGATIVE CONTROLS for the pair above (docs/SPEC-TABLES.md §8.7): a gate
# that cannot go red proves nothing, so each claim is broken in a throwaway
# copy of the generated header and the binary must FAIL.
#
#   1. NULL for an absent doc. The concatenating printer dereferences it, so
#      the run faults or the address check names the row.
#   2. A per-row inline empty literal instead of the unit's one TableDocNone.
#      The text is right and the address is not, which is exactly the failure
#      the shared-string claim exists to have.
#
# Each of the two rides twice, once over the DESCRIPTOR rows in the table
# header and once over the REGISTRY rows in the view file, and two more break
# the two places one declaration's annotation is spelled twice: a ViewType
# against the descriptor its `type` points at, and a general arm's ViewVariant
# row against the field descriptor that row points at.
.PHONY: tables-doctags-negative-controls
tables-doctags-negative-controls: bin/schema test/tables/doctags_main.cpp
	@rm -rf build/doctags-null && mkdir -p build/doctags-null
	./bin/schema generate --lang cpp --out build/doctags-null tables/examples
	@sed -i.bak 's|", TableDocNone, 0, NULL, false }|", NULL, 0, NULL, false }|' build/doctags-null/TablesTable.h
	@grep -q '", NULL, 0, NULL, false }' build/doctags-null/TablesTable.h || \
		{ echo "NEGATIVE CONTROL: the NULL-doc sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/doctags-null -DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo \
		test/tables/doctags_main.cpp build/doctags-null/TablesTable.cpp build/doctags-null/TabledemoView.cpp -o build/schema_test_doctags_null
	@if ./build/schema_test_doctags_null > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a NULL doc column did not take the gate down"; exit 1; \
	fi
	@echo "doc/tags negative control 1: a NULL doc column takes the gate down"
	@rm -rf build/doctags-inline && mkdir -p build/doctags-inline
	./bin/schema generate --lang cpp --out build/doctags-inline tables/examples
	@sed -i.bak 's|, TableDocNone, 0, NULL, false }|, "", 0, NULL, false }|' build/doctags-inline/TablesTable.h
	@grep -q ', "", 0, NULL, false }' build/doctags-inline/TablesTable.h || \
		{ echo "NEGATIVE CONTROL: the inline-empty sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/doctags-inline -DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo \
		test/tables/doctags_main.cpp build/doctags-inline/TablesTable.cpp build/doctags-inline/TabledemoView.cpp -o build/schema_test_doctags_inline
	@if ./build/schema_test_doctags_inline > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a per-row empty literal did not take the shared-string gate down"; exit 1; \
	fi
	@echo "doc/tags negative control 2: a per-row empty literal takes the shared-string gate down"
	@rm -rf build/doctags-registry && mkdir -p build/doctags-registry
	./bin/schema generate --lang cpp --out build/doctags-registry tables/examples
	@sed -i.bak 's|, TableDocNone, 0, NULL },|, NULL, 0, NULL },|' build/doctags-registry/TabledemoView.cpp
	@grep -q ', NULL, 0, NULL },' build/doctags-registry/TabledemoView.cpp || \
		{ echo "NEGATIVE CONTROL: the registry NULL-doc sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/doctags-registry -DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo \
		test/tables/doctags_main.cpp build/doctags-registry/TablesTable.cpp build/doctags-registry/TabledemoView.cpp -o build/schema_test_doctags_registry
	@if ./build/schema_test_doctags_registry > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a NULL doc column on a REGISTRY row did not take the gate down"; exit 1; \
	fi
	@echo "doc/tags negative control 3: a NULL doc column on a registry row takes the gate down"
	@rm -rf build/doctags-registry-inline && mkdir -p build/doctags-registry-inline
	./bin/schema generate --lang cpp --out build/doctags-registry-inline tables/examples
	@sed -i.bak 's|, TableDocNone, 0, NULL },|, "", 0, NULL },|' build/doctags-registry-inline/TabledemoView.cpp
	@grep -q ', "", 0, NULL },' build/doctags-registry-inline/TabledemoView.cpp || \
		{ echo "NEGATIVE CONTROL: the registry inline-empty sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/doctags-registry-inline -DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo \
		test/tables/doctags_main.cpp build/doctags-registry-inline/TablesTable.cpp build/doctags-registry-inline/TabledemoView.cpp \
		-o build/schema_test_doctags_registry_inline
	@if ./build/schema_test_doctags_registry_inline > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a per-row empty literal on a REGISTRY row did not take the shared-string gate down"; exit 1; \
	fi
	@echo "doc/tags negative control 4: a per-row empty literal on a registry row takes the shared-string gate down"
	@rm -rf build/doctags-typepair && mkdir -p build/doctags-typepair
	./bin/schema generate --lang cpp --out build/doctags-typepair tables/examples
	@sed -i.bak 's|WeaponConfigTableType(), "One weapon|WeaponConfigTableType(), "Drifted weapon|' build/doctags-typepair/TabledemoView.cpp
	@grep -q 'WeaponConfigTableType(), "Drifted weapon' build/doctags-typepair/TabledemoView.cpp || \
		{ echo "NEGATIVE CONTROL: the type-pair sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/doctags-typepair -DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo \
		test/tables/doctags_main.cpp build/doctags-typepair/TablesTable.cpp build/doctags-typepair/TabledemoView.cpp -o build/schema_test_doctags_typepair
	@if ./build/schema_test_doctags_typepair > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a ViewType and its descriptor disagreeing about doc did not take the gate down"; exit 1; \
	fi
	@echo "doc/tags negative control 5: the TYPE pair disagreeing takes the gate down"
	@rm -rf build/doctags-armpair && mkdir -p build/doctags-armpair
	./bin/schema generate --lang cpp --out build/doctags-armpair tables/arms
	@sed -i.bak 's|&Carry_view_arm_fields\[0\], "|\&Carry_view_arm_fields[0], "drifted |' build/doctags-armpair/ArmdemoView.cpp
	@grep -q '&Carry_view_arm_fields\[0\], "drifted ' build/doctags-armpair/ArmdemoView.cpp || \
		{ echo "NEGATIVE CONTROL: the arm-pair sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/doctags-armpair -Itest/tables -DVIEW_HEADER='"ArmdemoView.h"' -DVIEW_NAMESPACE=armdemo \
		test/tables/doctags_main.cpp build/doctags-armpair/CarryTable.cpp build/doctags-armpair/GateTable.cpp \
		build/doctags-armpair/NestTable.cpp build/doctags-armpair/RingTable.cpp build/doctags-armpair/ArmdemoView.cpp \
		-o build/schema_test_doctags_armpair
	@if ./build/schema_test_doctags_armpair > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a general arm's ViewVariant row and its field descriptor disagreeing did not take the gate down"; exit 1; \
	fi
	@echo "doc/tags negative control 6: the general ARM pair disagreeing takes the gate down"

# ---------------------------------------------------------------------------
# THE UNIT REGISTRY (docs/SPEC-TABLES.md §8.3), and its corpus gate (§8.7).
#
# THE EDITOR GATE is a dogfood: a program that has the generated view file and
# NOTHING ELSE — no schema files on disk, no compiler, no knowledge of a single
# declaration's name — calls UnitView() and prints the whole build. THE CORPUS
# GATE makes that mechanical: for every unit in the corpus the listing that
# program prints is byte-identical to the listing the compiler produces from
# its own IR, which is the PIN.
#
# The scope is the units the C++ backend emits a view for. The units that are
# ONE schema file under test/tables are the same shapes the corpus dirs carry,
# so the gate reads the corpus dirs and the pin's own list says so.
# ---------------------------------------------------------------------------

# each entry is <generated dir>:<package>
VIEW_CORPUS := examples:tabledemo pointers:graphdemo block:blockdemo blockhome:blockhome \
	messages:messagedemo stream:streamdemo blobs:blobdemo scalars:scalardemo \
	maps:mapdemo lists:listdemo arms:armdemo backend:backenddemo vocab:vocabdemo vocab9:vocab9demo

.PHONY: tables-view
tables-view: build/tables-generated/.stamp test/tables/view_main.cpp
	@rm -rf build/view && mkdir -p build/view
	@set -e; for entry in $(VIEW_CORPUS); do \
		dir=$${entry%%:*}; pkg=$${entry##*:}; \
		cap=$$(printf '%s' "$$pkg" | cut -c1 | tr 'a-z' 'A-Z')$$(printf '%s' "$$pkg" | cut -c2-); \
		$(CXX) $(CXXFLAGS) -Ibuild/tables-generated/$$dir -Itest/tables \
			-DVIEW_HEADER="\"$${cap}View.h\"" -DVIEW_NAMESPACE=$$pkg \
			test/tables/view_main.cpp build/tables-generated/$$dir/$${cap}View.cpp -o build/view/prog-$$pkg; \
		./build/view/prog-$$pkg > build/view/$$pkg.listing; \
	done
	SCHEMA_VIEW_LISTING_DIR=$$PWD/build/view go test ./internal/viewlisting -run TestUnitViewListingMatchesTheIR
	@echo "unit registry corpus gate: $(words $(VIEW_CORPUS)) units listed from UnitView() and byte-identical to the compiler's own"

# THE NEGATIVE CONTROLS for the corpus gate (docs/SPEC-TABLES.md §8.7). A gate
# that cannot go red proves nothing, so each claim is broken in a throwaway
# copy and the comparison must FAIL.
#
#   1. A MULTI-LINE doc printed unflattened. The listing is a line-oriented
#      byte comparison, so a printer that writes the newline verbatim splits
#      the exhibit's doc across lines.
#   2. One declaration dropped from an emitter's registry. Completeness is the
#      count the pin carries.
#   3. A general arm's offset moved. Every arm overlays the union's payload
#      base, so a moved offset prints an overlay the pin does not have.
.PHONY: tables-view-negative-controls
tables-view-negative-controls: build/tables-generated/.stamp test/tables/view_main.cpp
	@rm -rf build/view-negative && mkdir -p build/view-negative
	$(CXX) $(CXXFLAGS) -Ibuild/tables-generated/examples -Itest/tables \
		-DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo -DVIEW_UNFLATTENED \
		test/tables/view_main.cpp build/tables-generated/examples/TabledemoView.cpp -o build/view-negative/prog-unflattened
	./build/view-negative/prog-unflattened > build/view-negative/tabledemo.listing
	@if SCHEMA_VIEW_LISTING_DIR=$$PWD/build/view-negative SCHEMA_VIEW_LISTING_UNITS=tabledemo go test ./internal/viewlisting -run TestUnitViewListingMatchesTheIR > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an unflattened multi-line doc did not take the corpus gate down"; exit 1; \
	fi
	@echo "unit registry negative control 1: a multi-line doc printed unflattened takes the corpus gate down"
	@rm -rf build/view-dropped && mkdir -p build/view-dropped
	./bin/schema generate --lang cpp --out build/view-dropped/examples tables/examples
	@sed -i.bak 's|3, constants };|2, constants };|' build/view-dropped/examples/TabledemoView.cpp
	@grep -q '2, constants };' build/view-dropped/examples/TabledemoView.cpp || \
		{ echo "NEGATIVE CONTROL: the dropped-declaration sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/view-dropped/examples -Itest/tables \
		-DVIEW_HEADER='"TabledemoView.h"' -DVIEW_NAMESPACE=tabledemo \
		test/tables/view_main.cpp build/view-dropped/examples/TabledemoView.cpp -o build/view-dropped/prog
	./build/view-dropped/prog > build/view-negative/tabledemo.listing
	@if SCHEMA_VIEW_LISTING_DIR=$$PWD/build/view-negative SCHEMA_VIEW_LISTING_UNITS=tabledemo go test ./internal/viewlisting -run TestUnitViewListingMatchesTheIR > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a declaration dropped from the registry did not take the corpus gate down"; exit 1; \
	fi
	@echo "unit registry negative control 2: one declaration dropped from the registry takes the corpus gate down"
	@rm -rf build/view-armoffset && mkdir -p build/view-armoffset
	./bin/schema generate --lang cpp --out build/view-armoffset/arms tables/arms
	@sed -i.bak 's|(uint32_t) offsetof( Carry, plain )|( (uint32_t) offsetof( Carry, plain ) ^ 4 )|' build/view-armoffset/arms/ArmdemoView.cpp
	@grep -q 'offsetof( Carry, plain ) ^ 4' build/view-armoffset/arms/ArmdemoView.cpp || \
		{ echo "NEGATIVE CONTROL: the arm-offset sabotage did not apply"; exit 1; }
	$(CXX) $(CXXFLAGS) -Ibuild/view-armoffset/arms -Itest/tables \
		-DVIEW_HEADER='"ArmdemoView.h"' -DVIEW_NAMESPACE=armdemo \
		test/tables/view_main.cpp build/view-armoffset/arms/ArmdemoView.cpp -o build/view-armoffset/prog
	./build/view-armoffset/prog > build/view-negative/armdemo.listing
	@if SCHEMA_VIEW_LISTING_DIR=$$PWD/build/view-negative SCHEMA_VIEW_LISTING_UNITS=armdemo go test ./internal/viewlisting -run TestUnitViewListingMatchesTheIR > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a general arm's moved offset did not take the corpus gate down"; exit 1; \
	fi
	@echo "unit registry negative control 3: a general arm's moved offset takes the corpus gate down"

# THE CONTAINMENT GATE (docs/SPEC-TABLES.md §8.4, §8.7), in §2.2's shape: not
# one of the six registry symbols appears in any generated file but the view
# pair. Nothing selects the view, so what a build pays is what it compiles, and
# that answer only holds while the registry stays inside its own file. The gate
# asks "did the registry leak out of its file?" and never "is there a
# descriptor here?". The descriptor vocabulary is §8.1's and rides where it
# always did, which is why the pattern is the six names and nothing else.
#
# UnitView covers UnitViewInfo, being its prefix.
#
# The grep runs over the CODE, with every // comment stripped first, on the
# same terms as the block zero-cost gate above: a comment naming the registry
# is prose, not a symbol.
VIEW_REGISTRY_SYMBOLS := UnitView|ViewType|ViewVocabulary|ViewVariant|ViewConstant

.PHONY: tables-view-containment
tables-view-containment: build/tables-generated/.stamp
	@$(MAKE) --no-print-directory view-containment-scan VIEW_SCAN_DIR=build/tables-generated
	@echo "unit registry containment gate: no generated file but the view pair carries one of the six registry symbols"

# the scan itself, over one directory, so the gate and its negative control run
# the SAME grep rather than two greps that agree today
.PHONY: view-containment-scan
view-containment-scan:
	@for f in $(VIEW_SCAN_DIR)/*/*; do \
		case $$f in *View.h|*View.cpp) continue;; esac; \
		if sed -E 's://.*$$::' $$f | grep -nE "$(VIEW_REGISTRY_SYMBOLS)"; then \
			echo "VIEW CONTAINMENT GATE FAILED: a registry symbol leaked into $$f"; exit 1; \
		fi; \
	done

.PHONY: tables-view-containment-negative-control
tables-view-containment-negative-control: build/tables-generated/.stamp
	@rm -rf build/view-containment-negative && mkdir -p build/view-containment-negative/examples
	@cp build/tables-generated/examples/*.h build/tables-generated/examples/*.cpp build/view-containment-negative/examples/
	@printf 'const schema_view_leak::ViewVariant * leaked();\n' >> build/view-containment-negative/examples/TablesTable.h
	@grep -q 'ViewVariant \* leaked' build/view-containment-negative/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL: the planted registry symbol did not apply"; exit 1; }
	@if $(MAKE) --no-print-directory view-containment-scan VIEW_SCAN_DIR=build/view-containment-negative > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a registry symbol in a Table header did not take the containment gate down"; exit 1; \
	fi
	@echo "unit registry containment negative control: a registry symbol planted in a non-view file takes the gate down"

# ---------------------------------------------------------------------------
# The BLOCK FORM (docs/SPEC-TABLES.md §19). Nothing declares it: every fixed table
# has one, emitted on the side into <Base>Block.h/.cpp and <Base>Block.cs, and
# a consumer compiles those only if it uses the form.
# ---------------------------------------------------------------------------

# -Wshadow, on all three tables legs (BLOCK here, TABLES and PACK below): the
# POSIX gate for a class the POSIX legs were blind to. A shadowed name in an
# emitted function is a warning nobody sees under -Wall -Wextra — neither gcc
# nor clang says a word without this flag — and cl refuses it outright at
# /W4 /WX, so the estate's Visual C++ requirement turned a silent POSIX green
# into a Windows red (#286).
#
# The two POSIX compilers cover DIFFERENT halves of what cl refuses, and the
# matrix runs both, so the pair is the gate rather than either one:
#   clang -Wshadow  -> a local hiding a local          (cl C4456)
#   gcc   -Wshadow  -> the same, PLUS a constructor parameter hiding a member
#                      (cl C4458), which clang files under -Wshadow-all
# macOS runs clang and ubuntu runs gcc; the big-endian leg is a second gcc.
BLOCK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
BLOCK_INCLUDES := -Ibuild/tables-generated/block
BLOCK_SOURCES = $$(ls build/tables-generated/block/*Block.cpp)

build/schema_test_block: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# The SANITIZED twin (#277's rule). The block form is where a multi-threaded
# fill over disjoint index ranges lives, and a race in that fill is precisely
# what a sanitizer leg exists to find — -Werror cannot see one.
# -fno-sanitize-recover=all is what makes it a GATE.
build/schema_test_block_asan: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# The THREADSANITIZER twin. §19.5 says the multi-threaded fill runs "under the
# sanitizer leg, where a race in the fill is what the leg exists to find" — and
# ASan is not that leg: it finds no races, and byte-identity between a serial
# fill and a DETERMINISTIC four-worker fill finds none either. This is the leg
# that can. It costs under a second, so it rides beside the other two rather
# than waiting for a nightly.
build/schema_test_block_tsan: build/tables-generated/.stamp test/tables/block_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=thread -fno-sanitize-recover=all -g \
		$(BLOCK_INCLUDES) test/tables/block_main.cpp $(BLOCK_SOURCES) -o $@

# Its NEGATIVE CONTROL, because a green TSan run otherwise proves only that the
# leg ran: --race has every worker write ONE row, after a start barrier, many
# times, and the leg must go red on the write-write race that creates.
#
# Overlapping RANGES would be a race too, and that is what this control did at
# first — but whether a sanitizer observes one then depends on how the machine
# happened to schedule four threads over seven thousand rows, and it passed on
# this bench and failed on CI's. Contending on one address from threads that
# start together is the same defect made certain, which is what a control has
# to be.
.PHONY: tables-block-race-negative-control
tables-block-race-negative-control: build/schema_test_block_tsan
	@if ./build/schema_test_block_tsan --race > build/block-race.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: ThreadSanitizer did not see overlapping workers writing one row"; exit 1; \
	fi
	@grep -q "ThreadSanitizer: data race" build/block-race.log || \
		{ echo "NEGATIVE CONTROL FAILED: the leg went red, but not on a data race"; tail -20 build/block-race.log; exit 1; }
	@echo "block race negative control: overlapping workers turn the ThreadSanitizer leg red"

# THE TWO-LANGUAGE GATE (docs/SPEC-TABLES.md §19.5, §12.1): a C++ producer writes a
# block and pins its bytes; a C# consumer opens those very bytes and compares
# every field of every row, twice — through the generated blittable struct and
# through the block descriptors. Sizes and offsets are asserted by GENERATED
# code on both sides, so the pair proves the two agree on the BYTES and not
# merely on the constants.
.PHONY: tables-block
tables-block: build/schema_test_block build/schema_test_block_asan build/schema_test_block_tsan build/tables-generated-cs/.stamp
	./build/schema_test_block
	./build/schema_test_block_asan
	./build/schema_test_block_tsan
	$(MAKE) tables-block-const
	$(MAKE) tables-block-const-negative-control
# THE C# HALF of this two-language gate runs unless the cs leg is named in
# SCHEMA_SKIP_LEGS (issue #599). A skip that reaches the leg loop and not here
# is a skip that stops the chain anyway, on the same missing dotnet.
ifeq ($(filter cs,$(SKIPPED_LEGS)),)
	cd test/cs-block && $(DOTNET) run
else
	@echo "tables-block: the C# half is SKIPPED, SCHEMA_SKIP_LEGS names the cs leg"
endif

# THE CONST READ PATH (docs/SPEC-TABLES.md §19.2, schema#455). A block is
# memory another build wrote, so the consumer half of the form reads bytes it
# does not own: a pinned image is loaded, held as a `const uint8_t *` from that
# moment, and opened WITH NO CAST through the const overload. The rows come
# back read-only, and the same bytes opened writable read the same values, so
# the producer's path is proved unchanged rather than assumed.
#
# It runs SANITIZED beside the plain build on #277's rule: the const view
# points into a buffer sized to the image, so a row read past the extent lands
# in a redzone.
build/schema_test_block_const: build/tables-generated/.stamp test/tables/block_const_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_const_main.cpp $(BLOCK_SOURCES) -o $@

build/schema_test_block_const_asan: build/tables-generated/.stamp test/tables/block_const_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(BLOCK_INCLUDES) test/tables/block_const_main.cpp $(BLOCK_SOURCES) -o $@

.PHONY: tables-block-const
tables-block-const: build/schema_test_block_const build/schema_test_block_const_asan
	./build/schema_test_block_const
	./build/schema_test_block_const_asan

# ITS NEGATIVE COMPILE CONTROL, and it is the half a run cannot hold: a view
# handing back a reference a consumer can WRITE through is a read-only view in
# name only, and no green run of the gate above would ever say so. The same
# program, one assignment through the const view added by -DBLOCK_CONST_WRITE,
# and the COMPILE must go red, on the const qualification and not on some
# other error, which is what the second grep is for.
.PHONY: tables-block-const-negative-control
tables-block-const-negative-control: build/tables-generated/.stamp test/tables/block_const_main.cpp
	@mkdir -p build
	@if $(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) -DBLOCK_CONST_WRITE -fsyntax-only \
			test/tables/block_const_main.cpp > build/block-const-write.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a write through the const row view COMPILED"; exit 1; \
	fi
	@grep -qE "read-only|not assignable|const-qualified|assignment of member" build/block-const-write.log || \
		{ echo "NEGATIVE CONTROL FAILED: the compile went red, but not on the const view"; \
		  cat build/block-const-write.log; exit 1; }
	@grep -m1 -E "read-only|not assignable|const-qualified|assignment of member" build/block-const-write.log
	@echo "negative control: one write through the const row view turns the COMPILE red"

# ---------------------------------------------------------------------------
# THE FORGERY FUZZER (docs/SPEC-TABLES.md §19.2, §19.5). The hand-written battery in
# block_main.cpp and Program.cs is eleven forgeries, one per fact BlockOpen
# checks plus the one this fuzzer found. This is the standing gate beside it:
# valid blocks from the generated builder, mutated, and one oracle over every
# mutant — REFUSE, or OPEN and be WHOLE.
#
# The mutators are seeded and reproducible: every mutant is a pure function of
# ( seed, unit, count vector, pass, index ), and a failing case prints the one
# command that re-runs it alone. Override for a long local run:
#
#   make tables-block-fuzz N=5000000 SEED=1234
#
# N is the RANDOM budget per unit, spread over its count vectors; the
# enumerated passes — every named slot x every width x every boundary value,
# every truncation length, every unaligned base — run whatever N is set to,
# because they are what actually cover the boundaries and they cost nothing.
# ---------------------------------------------------------------------------

# N is chosen against the measurement that matters, which is CI's and not this
# bench's: ubuntu-latest runs the sanitized twin about 3.4x slower than arm64
# clang here, and the sanitized twin is the whole cost. At N=200000 the target
# measured 41.6 s there against a 60 s budget, which meets the bar with too
# little left for a busy runner; at N=100000 it is about 25 s, half the budget.
#
#   leg (at N=100000)                    here (arm64)   ubuntu-latest
#   schema_test_block_fuzz                     1.4 s        ~ 1.8 s
#   schema_test_block_fuzz_asan                6.0 s        ~ 16 s
#   test/cs-block -- --fuzz                    2.2 s        ~ 3 s
#
# The ENUMERATED passes are what cover the boundaries and they run whatever N
# is; N buys random mutants on top, and a long run is what the override is for.
N ?= 100000
SEED ?= 24845619678

BLOCK_FUZZ_INCLUDES := -Ibuild/tables-generated/block -Ibuild/tables-generated/blockhome
BLOCK_FUZZ_SOURCES = $$(ls build/tables-generated/block/*Block.cpp build/tables-generated/blockhome/*Block.cpp)

build/schema_test_block_fuzz: build/tables-generated/.stamp test/tables/block_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_FUZZ_INCLUDES) test/tables/block_fuzz_main.cpp $(BLOCK_FUZZ_SOURCES) -o $@

# The SANITIZED twin (#277's rule), and here it is the point rather than a
# companion: the oracle proves a mutant that OPENED stays inside the extent,
# and only the sanitizer can prove that a mutant BlockOpen refused read nothing
# outside it on the way to refusing. The region is allocated at exactly the
# bytes the caller claims, so the redzone begins at the extent's last byte.
build/schema_test_block_fuzz_asan: build/tables-generated/.stamp test/tables/block_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(BLOCK_FUZZ_INCLUDES) test/tables/block_fuzz_main.cpp \
		$(BLOCK_FUZZ_SOURCES) -o $@

# The seed blocks: one valid block per unit per count vector, written by the
# generated C++ BUILDER, because C# has only the read half of the form.
build/block-fuzz/.stamp: build/schema_test_block_fuzz
	@rm -rf build/block-fuzz && mkdir -p build/block-fuzz
	./build/schema_test_block_fuzz --dump build/block-fuzz
	@touch $@

.PHONY: tables-block-fuzz
tables-block-fuzz: build/schema_test_block_fuzz build/schema_test_block_fuzz_asan build/block-fuzz/.stamp build/tables-generated-cs/.stamp
	SEED=$(SEED) N=$(N) ./build/schema_test_block_fuzz
	SEED=$(SEED) N=$(N) ./build/schema_test_block_fuzz_asan
# the C# half, on the same rule as tables-block above (issue #599)
ifeq ($(filter cs,$(SKIPPED_LEGS)),)
	cd test/cs-block && SEED=$(SEED) N=$(N) $(DOTNET) run -- --fuzz
else
	@echo "tables-block-fuzz: the C# half is SKIPPED, SCHEMA_SKIP_LEGS names the cs leg"
endif

# THE COOK FIXTURES the Rust fuzzer's cook half forges, written by test/cookgen
# with the same chains the conformance harness uses — the fixture generator's
# own facts, which the conformance data deliberately does not carry.
build/cook-fuzz/.stamp: $(wildcard test/cookgen/*.go) $(SCHEMAS_TABLES_POINTERS)
	@rm -rf build/cook-fuzz && mkdir -p build/cook-fuzz
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene    --ref head --chain ListNode --next next --out build/cook-fuzz/Scene.cook
	./build/cookgen --bytes 4096 --root Depot    --ref head --chain ListNode --next next --out build/cook-fuzz/Depot.cook
	./build/cookgen --bytes 4096 --root Album    --ref head --chain ListNode --next next --out build/cook-fuzz/Album.cook
	./build/cookgen --bytes 4096 --root TreeNode --ref left --chain TreeNode --next left --out build/cook-fuzz/TreeNode.cook
	./build/cookgen --bytes 4096 --root ListNode --ref next --chain ListNode --next next --out build/cook-fuzz/ListNode.cook
	@touch $@

# The NEGATIVE CONTROLS. A fuzzer that has never gone red proves nothing about
# the checks it is watching, so each of these REMOVES one of BlockOpen's checks
# from the EMITTER — from both backends at once — and requires the fuzzer to
# find it. The emitter is replaced through `go build -overlay`, so no tracked
# file is edited and the sabotage cannot survive the target that made it.
#
# The gate they hold is the ORACLE's independence: it re-derives every bound
# from the descriptors and from the triples in the instance, so it can disagree
# with BlockOpen. An oracle that shared BlockOpen's arithmetic would stay green
# through both of these.

# The sed program that removes each check from each emitter. They live in
# variables rather than in the $(call) below because a sed address RANGE
# carries a comma, and make would read it as another argument.
BLOCK_FUZZ_SED_CPP_extent := /rows > (uint64_t) bytes - offset_of/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_CS_extent := /rows > (ulong)bytes - offsetOf/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_CPP_maximum := /past the DECLARED MAXIMUM: Begin refuses/,/count > (uint64_t) %sBlock/d
BLOCK_FUZZ_SED_CS_maximum := /count > (ulong)%sMax/d
BLOCK_FUZZ_SED_RUST_extent := /rows > bytes as u64 - offset_of/d; /padding > bytes - used/d
BLOCK_FUZZ_SED_RUST_maximum := /if count > Self::%s_MAX as u64 {/,/on a count the maximum does not bound/d

# how a sabotage is built: $(1) is its name, and the two sed programs above
# named by it are what come out of each emitter. The replacement files take a
# .go.txt suffix on purpose: `go build -overlay` does not care what a
# replacement is called, and `go test ./...` walks build/ and would otherwise
# find two packages sitting in one directory.
define block_fuzz_sabotage
	@rm -rf build/block-fuzz-$(1) && mkdir -p build/block-fuzz-$(1)
	@sed '$(BLOCK_FUZZ_SED_CPP_$(1))' internal/codegen/cpptable/block.go > build/block-fuzz-$(1)/cpptable-block.go.txt
	@sed '$(BLOCK_FUZZ_SED_CS_$(1))' internal/codegen/cstable/block.go > build/block-fuzz-$(1)/cstable-block.go.txt
	@sed '$(BLOCK_FUZZ_SED_RUST_$(1))' internal/codegen/rusttable/block.go > build/block-fuzz-$(1)/rusttable-block.go.txt
	@cmp -s internal/codegen/cpptable/block.go build/block-fuzz-$(1)/cpptable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the C++ emitter sabotage did not apply"; exit 1; } || true
	@cmp -s internal/codegen/cstable/block.go build/block-fuzz-$(1)/cstable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the C# emitter sabotage did not apply"; exit 1; } || true
	@cmp -s internal/codegen/rusttable/block.go build/block-fuzz-$(1)/rusttable-block.go.txt && \
		{ echo "NEGATIVE CONTROL: the Rust emitter sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/block.go":"%s/build/block-fuzz-$(1)/cpptable-block.go.txt","%s/internal/codegen/cstable/block.go":"%s/build/block-fuzz-$(1)/cstable-block.go.txt","%s/internal/codegen/rusttable/block.go":"%s/build/block-fuzz-$(1)/rusttable-block.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > build/block-fuzz-$(1)/overlay.json
	go build -overlay build/block-fuzz-$(1)/overlay.json -o build/block-fuzz-$(1)/schema ./cmd/schema
	@rm -rf build/block-fuzz-$(1)/generated
	./build/block-fuzz-$(1)/schema generate --lang cpp --out build/block-fuzz-$(1)/generated/block tables/block
	./build/block-fuzz-$(1)/schema generate --lang cpp --out build/block-fuzz-$(1)/generated/blockhome tables/blockhome
	./build/block-fuzz-$(1)/schema generate --lang cs --out build/block-fuzz-$(1)/generated/block-cs tables/block
	./build/block-fuzz-$(1)/schema generate --lang cs --out build/block-fuzz-$(1)/generated/blockhome-cs tables/blockhome
	$(CXX) $(BLOCK_CXXFLAGS) -Ibuild/block-fuzz-$(1)/generated/block -Ibuild/block-fuzz-$(1)/generated/blockhome \
		test/tables/block_fuzz_main.cpp \
		$$(ls build/block-fuzz-$(1)/generated/block/*Block.cpp build/block-fuzz-$(1)/generated/blockhome/*Block.cpp) \
		-o build/block-fuzz-$(1)/fuzz
	@if SEED=$(SEED) N=$(N) ./build/block-fuzz-$(1)/fuzz > build/block-fuzz-$(1)/cpp.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C++ fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/cpp.log || \
		{ echo "NEGATIVE CONTROL FAILED: the C++ leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/cpp.log; exit 1; }
	@if [ -n '$(filter cs,$(SKIPPED_LEGS))' ]; then \
		echo "  the C# half of this control is SKIPPED, SCHEMA_SKIP_LEGS names the cs leg"; \
	else \
		if ( cd test/cs-block && SEED=$(SEED) N=$(N) $(DOTNET) run \
				-p:BlockGeneratedDir=../../build/block-fuzz-$(1)/generated/block-cs \
				-p:BlockHomeGeneratedDir=../../build/block-fuzz-$(1)/generated/blockhome-cs -- --fuzz ) \
				> build/block-fuzz-$(1)/cs.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the C# fuzzer stayed green with the $(1) check removed from the emitter"; \
			exit 1; \
		fi; \
		grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/cs.log || \
			{ echo "NEGATIVE CONTROL FAILED: the C# leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/cs.log; exit 1; }; \
	fi
	@rm -f build/tables-generated-rust/.stamp
	./build/block-fuzz-$(1)/schema generate --lang rust --out build/tables-generated-rust/blockdemo/src tables/block
	./build/block-fuzz-$(1)/schema generate --lang rust --out build/tables-generated-rust/blockhome/src tables/blockhome
	cd test/rust-fuzz && PATH="$(RUSTUP_BIN):$$PATH" cargo build --quiet
	@if SEED=$(SEED) N=$(N) ./test/rust-fuzz/target/debug/rust-fuzz > build/block-fuzz-$(1)/rust.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the Rust fuzzer stayed green with the $(1) check removed from the emitter"; \
		exit 1; \
	fi
	@grep -q "^FAILED: an opened block" build/block-fuzz-$(1)/rust.log || \
		{ echo "NEGATIVE CONTROL FAILED: the Rust leg went red, but not on the oracle"; cat build/block-fuzz-$(1)/rust.log; exit 1; }
	@grep -m1 "^FAILED" build/block-fuzz-$(1)/cpp.log
	@grep -m1 "  mutation" build/block-fuzz-$(1)/cpp.log
	@echo "block fuzz $(1) negative control: removing that check from EVERY emitter turns the fuzzer red on every backend"
endef

# The EXTENT check: an array's rows must end inside the bytes the caller
# passed, and the used extent it reports must too. Both spellings go, because
# either one alone still refuses what the other would have.
.PHONY: tables-block-fuzz-extent-negative-control
tables-block-fuzz-extent-negative-control: build/block-fuzz/.stamp build/tables-generated-cs/.stamp build/tables-generated-rust/.stamp build/cook-fuzz/.stamp
	$(call block_fuzz_sabotage,extent)

# THE GO FUZZER, and its two negative controls on the same rule as the C++
# ones: it is the standing gate over the Go accelerators' Open, and a fuzzer
# that has never gone red proves nothing about the checks it is watching.
#
# It runs UNDER -race as well as plain, because a block and a cook are memory
# another language wrote and the Go readers point into it with `unsafe`: the
# race detector is what says the pointing itself is sound, and Go's bounds
# checks — which are on in every configuration — are what the oracle's own walk
# reads through.
# THE GO SOAK (docs/SPEC-TABLES.md, "What allocates, and what never does").
# Correctness tests are necessary and not sufficient: a read path that leaks one
# object per call passes every byte comparison in this repo and takes a server
# down in an afternoon. This reads and writes the WHOLE corpus in a loop and
# watches the ALLOCATION COUNTER, which must not move at all.
#
# A short count of it (the test's own -soak-iterations default) rides `go test
# ./...` so a regression is caught on the way past; TABLES_GO_SOAK_ITERATIONS
# (above, about an hour) is the real one. It found a real defect on its first run: the
# text walk rebuilt a union field's arms table on every call, ten objects per
# ToJson of an instance carrying five unions, which no byte comparison could
# ever have seen.

# The DECLARED MAXIMUM check: a count past the maximum Begin refuses on the
# producer side. This is §19.2's tenth forgery, the one a reader found OPEN,
# and the control is what keeps it closed.
.PHONY: tables-block-fuzz-maximum-negative-control
tables-block-fuzz-maximum-negative-control: build/block-fuzz/.stamp build/tables-generated-cs/.stamp build/tables-generated-rust/.stamp build/cook-fuzz/.stamp
	$(call block_fuzz_sabotage,maximum)

# THE COOK'S NEGATIVE CONTROL (docs/SPEC-TABLES.md §7.5). The hostile battery in
# internal/tablecook is a Go test and rides `go test ./...`; what a battery
# cannot prove about itself is that it would go RED, so this removes PASS ONE
# — `cook-check`'s directory scan — and requires it to.
#
# The sabotage is one inserted `return nil` at the top of checkDirectory, which
# is unambiguous in a way a deleted range is not, and it reaches the build
# through `go test -overlay`, so no tracked file is edited and the sabotage
# cannot survive the target that made it.
.PHONY: tables-cook-fuzz-negative-control
tables-cook-fuzz-negative-control:
	@rm -rf build/cook-fuzz-control && mkdir -p build/cook-fuzz-control
	@sed 's|^func checkDirectory(m \*tabletext.Model, h Header, dir \[\]DirectoryEntry) error {$$|&\n\treturn nil // NEGATIVE CONTROL|' \
		internal/tablecook/check.go > build/cook-fuzz-control/check.go.txt
	@cmp -s internal/tablecook/check.go build/cook-fuzz-control/check.go.txt && \
		{ echo "NEGATIVE CONTROL: the cook-check sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablecook/check.go":"%s/build/cook-fuzz-control/check.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-fuzz-control/overlay.json
	@if SEED=$(SEED) N=$(N) go test -count=1 -overlay=build/cook-fuzz-control/overlay.json \
			-run 'TestCookCheck' ./internal/tablecook/ > build/cook-fuzz-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the cook battery stayed green with the directory scan removed"; \
		exit 1; \
	fi
	@grep -q "FAILED" build/cook-fuzz-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on the bar"; cat build/cook-fuzz-control/log; exit 1; }
	@grep -m1 "FAILED" build/cook-fuzz-control/log

# THE SCALE FIXTURES (docs/SPEC-TABLES.md §7.5). `test/cookgen` writes a synthetic
# region streaming, in O(1) memory, so the OPEN-COST gate the emitter owes —
# open time flat across 1 MB, 100 MB and 1 GB — has inputs the C++ worker can
# regenerate rather than a gigabyte in the tree.
#
# CI runs the first two, under the two-minute rule. THE GIGABYTE IS RUN BY
# HAND (`make tables-cook-scale-1gb`): it writes a 1.6 GB file, which is not
# something a CI runner's disk should meet on every push. What runs here is
# the TOOL's own scan, which is O(R + P log N) and not the O(1) the runtime
# owes; this target's job is to produce the fixtures and prove they check.
COOKGEN_UNIT := tables/pointers
.PHONY: tables-cook-scale
tables-cook-scale: bin/schema
	@mkdir -p build/cook
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1048576   --out build/cook/1mb.cook
	./build/cookgen --bytes 1048576   --byte-order big --out build/cook/1mb-be.cook
	./build/cookgen --bytes 104857600 --out build/cook/100mb.cook
	./bin/schema cook-check --verbose build/cook/1mb.cook    $(COOKGEN_UNIT)
	./bin/schema cook-check --verbose build/cook/1mb-be.cook $(COOKGEN_UNIT)
	./bin/schema cook-check --verbose build/cook/100mb.cook  $(COOKGEN_UNIT)

.PHONY: tables-cook-scale-1gb
tables-cook-scale-1gb: bin/schema
	@mkdir -p build/cook
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --out build/cook/1gb.cook
	./bin/schema cook-check --verbose build/cook/1gb.cook $(COOKGEN_UNIT)

# ---- the COOK's C++ READ SIDE (docs/SPEC-TABLES.md §7) --------------------------
#
# `schema cook` writes the file and the generated <Root>Open points at it, and
# the two were written from the page independently — the tool in Go, the C++
# side from §7 alone, neither reading the other. That is what makes the golden
# mode a CROSS-IMPLEMENTATION gate rather than one implementation agreeing with
# itself: every node the C++ side reaches through its own derefs must be a node
# the cook's ATTRIBUTION part names, at that offset, with that type id, and the
# two sets must be equal.
#
# THE FIXTURES come from test/cookgen, which streams a synthetic region in O(1)
# memory (§7.5), so the 100 MB arm of the O(1) gate is regenerated rather than
# committed. Five roots cover the shapes a region has: a pointer chain, a tree,
# a keyed array of variable tables beside an optional, a cross-file graph
# through a by-value variable table, and a bare chain node as its own root.
COOK_ROOTS := Scene Depot Album TreeNode ListNode
# The FIXED roots. A fixed table's cook is ONE REGION OF ONE NODE (§7) and the
# same header match, and it is where the VALUE crossing lives: a fixed table has
# no pointer, so it has no node table and no kind 17, so this backend's wire and
# the tool's are the same bytes — the C++ side writes a known instance, the tool
# cooks it, and the C++ side reads the values back out of the tool's layout.
# Settings is POINTED AT; Stamp is pointed at by nothing and is declared in a
# file with no variable table of its own, which is the shape a file-scoped
# emission rule forgets.
COOK_FIXED_ROOTS := Settings Stamp
# and the roots whose cook the C++ side WRITES (docs/SPEC-TABLES.md §7.6): the two
# fixed ones, and the pointered Scene graph test/tables/cook_main.cpp builds
COOK_WRITE_ROOTS := Settings Stamp Scene

COOK_INCLUDES := -Ibuild/tables-generated/pointers -Itest/tables
COOK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread
# The SANITIZED twin is a gate and not a nicety here: Open is handed a hostile
# buffer by construction, the driver allocates each fixture at EXACTLY the
# length it claims so a redzone sits on the next byte, and
# -fno-sanitize-recover=all is what stops UBSan printing and continuing.
COOK_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g

build/schema_test_cook: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(CXX) $(COOK_CXXFLAGS) $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

build/schema_test_cook_asan: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(CXX) $(COOK_CXXFLAGS) $(COOK_SANITIZE) $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

# The fixtures the C++ gate opens. Small ones per root for the golden lock, the
# battery and the fuzzer; a big-endian one for the byte-order leg; and the two
# scale arms for the O(1) gate. `schema cook-check` runs over every small one
# first, so a fixture the TOOL itself would refuse can never be what a green
# C++ run is standing on.
build/cook-open/.stamp: bin/schema $(SCHEMAS_TABLES_POINTERS)
	@rm -rf build/cook-open && mkdir -p build/cook-open
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene    --ref head --chain ListNode --next next --out build/cook-open/Scene.cook
	./build/cookgen --bytes 3000 --root Depot    --ref head --chain ListNode --next next --out build/cook-open/Depot.cook
	./build/cookgen --bytes 3000 --root Album    --ref head --chain ListNode --next next --out build/cook-open/Album.cook
	./build/cookgen --bytes 3000 --root TreeNode --ref left --chain TreeNode --next left --out build/cook-open/TreeNode.cook
	./build/cookgen --bytes 3000 --root ListNode --ref next --chain ListNode --next next --out build/cook-open/ListNode.cook
	./build/cookgen --bytes 4096 --root Scene    --byte-order big --out build/cook-open/Scene-be.cook
	./build/cookgen --bytes 1048576   --root Scene --out build/cook-open/1mb.cook
	./build/cookgen --bytes 104857600 --root Scene --out build/cook-open/100mb.cook
	@for r in $(COOK_ROOTS); do \
		./bin/schema cook-check --root $$r build/cook-open/$$r.cook tables/pointers || exit 1; \
	done
	./bin/schema cook-check --root Scene build/cook-open/Scene-be.cook tables/pointers
	@touch $@

# The fixtures the C++ side WRITES: it saves a known instance to the tolerant
# wire — a fixed root's value, or the pointered Scene graph — the tool cooks that
# wire in both orders, the value gate reads a fixed instance back out of the
# cook, and the write gate holds the runtime's own cook to the tool's file. The
# tool's own read report must be SILENT over a wire this backend wrote — a
# crossing in the other direction, for free.
build/cook-open-fixed/.stamp: bin/schema build/schema_test_cook build/cook-open/.stamp
	@rm -rf build/cook-open-fixed && mkdir -p build/cook-open-fixed
	@for r in $(COOK_WRITE_ROOTS); do \
		./build/schema_test_cook write $$r build/cook-open-fixed/$$r.bin || exit 1; \
		./bin/schema cook --root $$r --in build/cook-open-fixed/$$r.bin \
			--out build/cook-open-fixed/$$r.cook --verbose tables/pointers || exit 1; \
		./bin/schema cook-check --root $$r build/cook-open-fixed/$$r.cook tables/pointers || exit 1; \
		./bin/schema cook --root $$r --in build/cook-open-fixed/$$r.bin --byte-order big \
			--out build/cook-open-fixed/$$r-be.cook tables/pointers || exit 1; \
	done
	@touch $@

.PHONY: tables-cook-open tables-cook-valued
tables-cook-open: build/schema_test_cook build/schema_test_cook_asan build/cook-open/.stamp build/cook-open-fixed/.stamp
	@for r in $(COOK_ROOTS); do \
		./build/schema_test_cook      golden $$r build/cook-open/$$r.cook || exit 1; \
		./build/schema_test_cook_asan golden $$r build/cook-open/$$r.cook || exit 1; \
	done
	@for r in $(COOK_FIXED_ROOTS); do \
		./build/schema_test_cook      golden      $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan golden      $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook      fixedvalues $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan fixedvalues $$r build/cook-open-fixed/$$r.cook || exit 1; \
		./build/schema_test_cook_asan forge       $$r build/cook-open-fixed/$$r.cook || exit 1; \
	done
	./build/schema_test_cook      usage Scene build/cook-open/Scene.cook
	./build/schema_test_cook      forge Scene build/cook-open/Scene.cook
	./build/schema_test_cook_asan forge Scene build/cook-open/Scene.cook
	./build/schema_test_cook_asan forge Depot build/cook-open/Depot.cook
	SEED=$(SEED) N=$(N) ./build/schema_test_cook_asan fuzz Scene build/cook-open/Scene.cook
	SEED=$(SEED) N=$(N) ./build/schema_test_cook_asan fuzz TreeNode build/cook-open/TreeNode.cook
	./build/schema_test_cook accept Scene build/cook-open/Scene.cook
	./build/schema_test_cook refuse Scene build/cook-open/Scene-be.cook
	./build/schema_test_cook time Scene build/cook-open/1mb.cook build/cook-open/100mb.cook

# I12 (docs/PORTING.md) — THE DOCUMENTED C++ SURFACE COMPILES AND RUNS. The
# three ```cpp cook examples docs/USAGE.md marks (cpp-cook-usage-read, -write,
# -write-pointered) are pulled out of the page VERBATIM, less their #include
# lines, and test/tables/usage_cpp_main.cpp includes each as a function body
# against the generated `tables/pointers` unit: the read opens the cooked
# Scene and walks its chain, and both writes produce a cook this build opens.
# The day the emitted surface moves, the page goes red with the code.
CPP_USAGE := build/cpp-usage
CPP_USAGE_FRAGMENTS := read:cpp-cook-usage-read write:cpp-cook-usage-write write_pointered:cpp-cook-usage-write-pointered
# The build both use: CPP_USAGE_GEN names the generated tables/pointers
# directory and CPP_USAGE_OUT where the fragments and the binary go.
CPP_USAGE_GEN ?= build/tables-generated/pointers
CPP_USAGE_OUT ?= $(CPP_USAGE)
.PHONY: tables-cpp-usage-build
tables-cpp-usage-build:
	@mkdir -p $(CPP_USAGE_OUT)
	@for pair in $(CPP_USAGE_FRAGMENTS); do \
		out=$${pair%%:*}; marker=$${pair#*:}; \
		sed -n "/<!-- $$marker -->/,/<!-- \/$$marker -->/p" docs/USAGE.md | sed '1,2d' | sed '$$d' | sed '$$d' \
			| sed '/^#include/d' > $(CPP_USAGE_OUT)/$$out.inc; \
		test -s $(CPP_USAGE_OUT)/$$out.inc || { echo "MISSING: docs/USAGE.md carries no $$marker example"; exit 1; }; \
	done
	$(CXX) $(COOK_CXXFLAGS) -I$(CPP_USAGE_GEN) -Itest/tables -I$(CPP_USAGE_OUT) test/tables/usage_cpp_main.cpp -o $(CPP_USAGE_OUT)/usage
.PHONY: tables-cpp-usage
tables-cpp-usage: build/tables-generated/.stamp build/cook-open/.stamp test/tables/usage_cpp_main.cpp docs/USAGE.md
	$(MAKE) --no-print-directory tables-cpp-usage-build CPP_USAGE_GEN=build/tables-generated/pointers CPP_USAGE_OUT=$(CPP_USAGE)
	./$(CPP_USAGE)/usage build/cook-open/Scene.cook

# ITS NEGATIVE CONTROL: the documented surface moves and the page does not.
# The emitter writes a fixed root's measure as <Root>CookSize instead of
# <Root>CookMeasure (the declaration and its one internal caller, so the
# emitted unit stays self-consistent), the pointers unit is regenerated from
# that compiler through `go build -overlay`, and the page's write example must
# fail to compile against it, naming SettingsCookMeasure.
CPP_USAGE_NC := build/cpp-usage-control
.PHONY: tables-cpp-usage-negative-control
tables-cpp-usage-negative-control: test/tables/usage_cpp_main.cpp docs/USAGE.md
	@rm -rf $(CPP_USAGE_NC) && mkdir -p $(CPP_USAGE_NC)
	@sed -e 's|inline int64_t %sCookMeasure( const %s \& value )|inline int64_t %sCookSize( const %s \& value ) /* SABOTAGED: the documented name moved */|' \
	     -e 's|(uint64_t) %sCookMeasure( value )|(uint64_t) %sCookSize( value )|' \
		internal/codegen/cpptable/cookwrite.go > $(CPP_USAGE_NC)/cookwrite.go.txt
	@test "$$(grep -c 'CookSize( ' $(CPP_USAGE_NC)/cookwrite.go.txt)" = 2 || \
		{ echo "NEGATIVE CONTROL FAILED: the rename did not patch both the declaration and its caller"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cookwrite.go":"%s/$(CPP_USAGE_NC)/cookwrite.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CPP_USAGE_NC)/overlay.json
	go build -overlay $(CPP_USAGE_NC)/overlay.json -o $(CPP_USAGE_NC)/schema ./cmd/schema
	$(CPP_USAGE_NC)/schema generate --lang cpp --out $(CPP_USAGE_NC)/pointers tables/pointers > /dev/null
	@grep -q "SettingsCookSize" $(CPP_USAGE_NC)/pointers/GraphTable.h || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter did not move the name"; exit 1; }
	@if $(MAKE) --no-print-directory tables-cpp-usage-build CPP_USAGE_GEN=$(CPP_USAGE_NC)/pointers CPP_USAGE_OUT=$(CPP_USAGE_NC)/out > $(CPP_USAGE_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the documented C++ example compiled against an emitter that moved SettingsCookMeasure"; \
		cat $(CPP_USAGE_NC)/log; exit 1; \
	fi
	@grep -q "SettingsCookMeasure" $(CPP_USAGE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the build went red, but not on the documented name"; cat $(CPP_USAGE_NC)/log; exit 1; }
	@echo "negative control: the emitted surface moved under docs/USAGE.md's C++ example and tables-cpp-usage went red"

# THE WRITE SIDE, against the tool's own bytes (docs/SPEC-TABLES.md §7.6). The
# tool cooked the wire this backend wrote, in BOTH byte orders, and the
# generated <Root>Cook must land on those files exactly — a second cooker with
# its own bytes would be a second format, and a cook is content-addressed by
# (asset hash, build version). The same mode counts allocations across the
# measure and both writes — none for a fixed root; for the pointered Scene,
# the numbering's and only through the TableAllocator handed in, cooked from
# the unlocked builder, a loaded region and the locked region alike — requires
# a short capacity to write nothing, and opens what it wrote. The SANITIZED
# twin runs it too: the writer is handed a buffer of exactly its own measure,
# so a byte past it lands in a redzone.
.PHONY: tables-cook-write
tables-cook-write: build/schema_test_cook build/schema_test_cook_asan build/cook-open-fixed/.stamp
	@for r in $(COOK_WRITE_ROOTS); do \
		./build/schema_test_cook      cookwrite $$r build/cook-open-fixed/$$r.cook build/cook-open-fixed/$$r-be.cook || exit 1; \
		./build/schema_test_cook_asan cookwrite $$r build/cook-open-fixed/$$r.cook build/cook-open-fixed/$$r-be.cook || exit 1; \
	done

# ITS NEGATIVE CONTROL, and it is the ALLOCATION half — the half a byte
# comparison cannot fail on. One allocation inside the measured region, and the
# gate must go red: a zero-allocation claim whose instrument has never fired is
# a claim nobody has checked.
.PHONY: tables-cook-write-negative-control
tables-cook-write-negative-control: build/schema_test_cook build/cook-open-fixed/.stamp
	@mkdir -p build
	@if COOK_WRITE_SABOTAGE=1 ./build/schema_test_cook cookwrite Settings \
			build/cook-open-fixed/Settings.cook build/cook-open-fixed/Settings-be.cook \
			> build/cook-write-control.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one allocation per write left the gate GREEN"; exit 1; \
	fi
	@grep -q "the write side allocated" build/cook-write-control.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the allocation"; \
		  cat build/cook-write-control.log; exit 1; }
	@echo "negative control: one allocation inside the write turns the cook-write gate red"

# AND THE POINTERED HALF's control, which is the HOOKS test's to hold
# (docs/SPEC-TABLES.md §7.6, §13.9): a pointered writer allocates its numbering,
# and every byte of it must go through the pair it was handed. The sabotage
# makes the builder overload of <Root>Cook ignore the builder's pair and take
# the DEFAULT one — the bypass a writer would most plausibly commit — and the
# hooks unit, whose default pair counts and must read zero, has to go red.
COOK_WRITE_HOOKS_SABOTAGE := build/cook-write-hooks-control
.PHONY: tables-cook-write-hooks-negative-control
tables-cook-write-hooks-negative-control: bin/schema test/tables/hooks_main.cpp
	@rm -rf $(COOK_WRITE_HOOKS_SABOTAGE) && mkdir -p $(COOK_WRITE_HOOKS_SABOTAGE)
	@sed -e 's|order, builder.arena.allocator );|order, TableDefaultAllocator() /* SABOTAGED: the builder pair is ignored */ );|' \
		internal/codegen/cpptable/cookwrite.go > $(COOK_WRITE_HOOKS_SABOTAGE)/cookwrite.go.txt
	@cmp -s internal/codegen/cpptable/cookwrite.go $(COOK_WRITE_HOOKS_SABOTAGE)/cookwrite.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the cook-write sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cookwrite.go":"%s/$(COOK_WRITE_HOOKS_SABOTAGE)/cookwrite.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(COOK_WRITE_HOOKS_SABOTAGE)/overlay.json
	@go build -overlay=$(COOK_WRITE_HOOKS_SABOTAGE)/overlay.json -o $(COOK_WRITE_HOOKS_SABOTAGE)/schema ./cmd/schema
	@$(COOK_WRITE_HOOKS_SABOTAGE)/schema generate --lang cpp --out $(COOK_WRITE_HOOKS_SABOTAGE)/pointers tables/pointers > /dev/null
	@$(COOK_WRITE_HOOKS_SABOTAGE)/schema generate --lang cpp --out $(COOK_WRITE_HOOKS_SABOTAGE)/blobs tables/blobs > /dev/null
	@$(CXX) $(TABLES_CXXFLAGS) -I$(COOK_WRITE_HOOKS_SABOTAGE)/pointers -I$(COOK_WRITE_HOOKS_SABOTAGE)/blobs -Itest/tables test/tables/hooks_main.cpp \
		$$(ls $(COOK_WRITE_HOOKS_SABOTAGE)/pointers/*Table.cpp $(COOK_WRITE_HOOKS_SABOTAGE)/blobs/*Table.cpp) -o $(COOK_WRITE_HOOKS_SABOTAGE)/hooks
	@if $(COOK_WRITE_HOOKS_SABOTAGE)/hooks > $(COOK_WRITE_HOOKS_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a cook through the default pair left the hooks test GREEN"; exit 1; \
	fi
	@grep -q "g_fallback_allocs == 0" $(COOK_WRITE_HOOKS_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the hooks test went red, but not on the default pair's count"; \
		  cat $(COOK_WRITE_HOOKS_SABOTAGE)/log; exit 1; }
	@echo "negative control: a pointered cook that ignores the builder's pair turns the hooks test red"

# The BLOB READ PATH's NEGATIVE CONTROL: plant one allocation into the view —
# the read path's only new code — through the same overlay mechanism, and the
# hooks test's frozen counters must go red. The claim "the read path allocates
# nothing" is held by a RUN, so its control must be a run that fails.
BLOB_READ_SABOTAGE := build/blob-read-control
.PHONY: tables-blob-read-hooks-negative-control
tables-blob-read-hooks-negative-control: bin/schema test/tables/hooks_main.cpp
	@rm -rf $(BLOB_READ_SABOTAGE) && mkdir -p $(BLOB_READ_SABOTAGE)
	@sed -e 's|TableBytesView view = { NULL, 0 };|TableBytesView view = { NULL, 0 }; schema_release( schema_allocate( 1 ) ); /* SABOTAGED: an allocation on the read path */|' \
		internal/codegen/cpptable/arena.go > $(BLOB_READ_SABOTAGE)/arena.go.txt
	@cmp -s internal/codegen/cpptable/arena.go $(BLOB_READ_SABOTAGE)/arena.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the read-path sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/arena.go":"%s/$(BLOB_READ_SABOTAGE)/arena.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(BLOB_READ_SABOTAGE)/overlay.json
	@go build -overlay=$(BLOB_READ_SABOTAGE)/overlay.json -o $(BLOB_READ_SABOTAGE)/schema ./cmd/schema
	@$(BLOB_READ_SABOTAGE)/schema generate --lang cpp --out $(BLOB_READ_SABOTAGE)/pointers tables/pointers > /dev/null
	@$(BLOB_READ_SABOTAGE)/schema generate --lang cpp --out $(BLOB_READ_SABOTAGE)/blobs tables/blobs > /dev/null
	@$(CXX) $(TABLES_CXXFLAGS) -I$(BLOB_READ_SABOTAGE)/pointers -I$(BLOB_READ_SABOTAGE)/blobs -Itest/tables test/tables/hooks_main.cpp \
		$$(ls $(BLOB_READ_SABOTAGE)/pointers/*Table.cpp $(BLOB_READ_SABOTAGE)/blobs/*Table.cpp) -o $(BLOB_READ_SABOTAGE)/hooks
	@if $(BLOB_READ_SABOTAGE)/hooks > $(BLOB_READ_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an allocating view left the hooks test GREEN"; exit 1; \
	fi
	@grep -q "fallback_before" $(BLOB_READ_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the hooks test went red, but not on the frozen read-path counter"; \
		  cat $(BLOB_READ_SABOTAGE)/log; exit 1; }
	@echo "negative control: one allocation inside a blob view turns the hooks test red"

# THE SPAN's NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.5, §6.5). A byte buffer
# larger than one 64 KiB slab cannot be bump-allocated in a slab: it takes a
# SPAN of the arena's address space. The sabotage removes exactly that choice —
# the size test that sends a large blob to TableArenaGrabSpan is made never to
# fire, and every other line of the allocator is the tree's — so the blob is
# bump-allocated in a 64 KiB slab and runs off the end of it, over memory the
# arena goes on to hand to the nodes allocated after it. The driver reads the
# blob back after those allocations and the pattern does not survive. It builds
# under the sanitizers, so an overrun that lands outside the slab's own block
# is named at the byte rather than read back as a wrong value.
#
# Both halves run: the tree's own emitter through the same driver must be
# GREEN, so a red below is the sabotage and not the driver.
BLOB_SPAN_SABOTAGE := build/blob-span-control
BLOB_SPAN_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g
.PHONY: tables-blob-span-negative-control
tables-blob-span-negative-control: bin/schema test/tables/blob_span_main.cpp
	@rm -rf $(BLOB_SPAN_SABOTAGE) && mkdir -p $(BLOB_SPAN_SABOTAGE)
	@sed -e 's|if ( bytes > (int64_t) kTableSlabBytes )|if ( false ) /* SABOTAGED: no blob takes a span */|' \
		internal/codegen/cpptable/arena.go > $(BLOB_SPAN_SABOTAGE)/arena.go.txt
	@cmp -s internal/codegen/cpptable/arena.go $(BLOB_SPAN_SABOTAGE)/arena.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the span sabotage did not apply"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/arena.go":"%s/$(BLOB_SPAN_SABOTAGE)/arena.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(BLOB_SPAN_SABOTAGE)/overlay.json
	@go build -overlay=$(BLOB_SPAN_SABOTAGE)/overlay.json -o $(BLOB_SPAN_SABOTAGE)/schema ./cmd/schema
	@$(BLOB_SPAN_SABOTAGE)/schema generate --lang cpp --out $(BLOB_SPAN_SABOTAGE)/blobs tables/blobs > /dev/null
	@./bin/schema generate --lang cpp --out $(BLOB_SPAN_SABOTAGE)/blobs-good tables/blobs > /dev/null
	$(CXX) $(TABLES_CXXFLAGS) $(BLOB_SPAN_SANITIZE) -I$(BLOB_SPAN_SABOTAGE)/blobs-good -Itest/tables \
		test/tables/blob_span_main.cpp $$(ls $(BLOB_SPAN_SABOTAGE)/blobs-good/*Table.cpp) -o $(BLOB_SPAN_SABOTAGE)/span-good
	@$(BLOB_SPAN_SABOTAGE)/span-good > $(BLOB_SPAN_SABOTAGE)/good.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the driver is red on the tree's own emitter"; \
		  cat $(BLOB_SPAN_SABOTAGE)/good.log; exit 1; }
	$(CXX) $(TABLES_CXXFLAGS) $(BLOB_SPAN_SANITIZE) -I$(BLOB_SPAN_SABOTAGE)/blobs -Itest/tables \
		test/tables/blob_span_main.cpp $$(ls $(BLOB_SPAN_SABOTAGE)/blobs/*Table.cpp) -o $(BLOB_SPAN_SABOTAGE)/span
	@if $(BLOB_SPAN_SABOTAGE)/span > $(BLOB_SPAN_SABOTAGE)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a blob bump-allocated in a slab it does not fit left the driver GREEN"; \
		cat $(BLOB_SPAN_SABOTAGE)/log; exit 1; \
	fi
	@grep -q "heap-buffer-overflow\|blob past the slab" $(BLOB_SPAN_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the driver went red, but not on the blob"; \
		  cat $(BLOB_SPAN_SABOTAGE)/log; exit 1; }
	@grep -m1 "heap-buffer-overflow\|blob past the slab" $(BLOB_SPAN_SABOTAGE)/log
	@echo "negative control: a blob larger than a slab, denied its span, overruns the slab"

# THE VALUED FIXTURE, cross-checked by the ENGINE's own uncook (§7.5, §7.4).
#
# `test/cookgen --values` fills every non-pointer leaf, so the conformance
# harness's `SceneValued` dump locks what a reader READS out of a node and not
# only where the node is. That dump is pinned from the C++ leg and byte-compared
# by the C# one; this is the THIRD reader, and it is the Go engine's:
#
#   cookgen --values  ->  uncook  ->  cook   must land on the same bytes
#
# `uncook` walks the region and recovers the root's wire, `cook` lays a region
# out from that wire, and the two regions being byte-identical is every value
# surviving a round trip through an implementation that did not write the
# fixture. A value dropped, widened or misplaced by either half moves a byte
# here — and `cook-check` first, so a fixture the tool itself would refuse can
# never be what a green run is standing on.
.PHONY: tables-cook-valued
tables-cook-valued: bin/schema
	@rm -rf build/cook-valued && mkdir -p build/cook-valued
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene --ref head --chain ListNode --next next --values \
		--out build/cook-valued/SceneValued.cook
	./bin/schema cook-check --root Scene --verbose build/cook-valued/SceneValued.cook tables/pointers
	./bin/schema uncook --root Scene --in build/cook-valued/SceneValued.cook \
		--out build/cook-valued/Scene.wire --verbose tables/pointers
	./bin/schema cook --root Scene --in build/cook-valued/Scene.wire \
		--out build/cook-valued/SceneAgain.cook --verbose tables/pointers
	@cmp build/cook-valued/SceneValued.cook build/cook-valued/SceneAgain.cook || \
		{ echo "the engine's uncook did not recover every value the fixture carries"; exit 1; }
	@echo "valued cook: cookgen --values -> uncook -> cook is byte-identical, so every value survives the engine's own read"

# THE GIGABYTE ARM, by hand (§7.5): a 1.5 GB fixture is not something a CI
# runner's disk should meet on every push, and the two arms above already span
# two orders of magnitude.
.PHONY: tables-cook-open-1gb
tables-cook-open-1gb: build/schema_test_cook build/cook-open/.stamp
	go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 1073741824 --root Scene --out build/cook-open/1gb.cook
	./build/schema_test_cook time Scene build/cook-open/1mb.cook build/cook-open/1gb.cook

# THE NEGATIVE CONTROLS, and a battery whose battery has never gone red is
# watching nothing. Each removes ONE clause of TableCookOpen through a
# `go build -overlay` on the emitter — so no tracked file is ever written to,
# an interrupt cannot leave a sabotaged working tree, and a parallel `make -j`
# cannot compile the sabotage into something else — regenerates the corpus from
# the sabotaged emitter, and requires the C++ gate to go RED.
#
# The two clauses chosen are the two that decide whether Open can hand back
# storage the caller never gave it: the part-length equation, which is what
# refuses a truncated file, and the root-fits check, which is what refuses a
# data part too short to hold the root.
define cook_open_sabotage
	@mkdir -p build
	@rm -rf build/cook-open-$(1) && mkdir -p build/cook-open-$(1)
	@sed 's|^.*$(2).*$$|    $(3) // NEGATIVE CONTROL|' \
		internal/codegen/cpptable/cook.go > build/cook-open-$(1)/cook.gotext
	@cmp -s build/cook-open-$(1)/cook.gotext internal/codegen/cpptable/cook.go && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cook.go":"%s/build/cook-open-$(1)/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-$(1)/overlay.json
	@go build -overlay=build/cook-open-$(1)/overlay.json -o build/cook-open-$(1)/schema ./cmd/schema
	./build/cook-open-$(1)/schema generate --lang cpp --out build/cook-open-$(1)/gen tables/pointers
	$(CXX) $(COOK_CXXFLAGS) $(COOK_SANITIZE) -Ibuild/cook-open-$(1)/gen -Itest/tables \
		test/tables/cook_main.cpp -o build/cook-open-$(1)/test
	@if ./build/cook-open-$(1)/test forge Scene build/cook-open/Scene.cook > build/cook-open-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the battery stayed green with the $(1) check removed"; exit 1; \
	fi
	@grep -q "FAILED" build/cook-open-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the battery went red, but not on a forgery"; cat build/cook-open-$(1)/log; exit 1; }
	@grep -m1 -A3 "FAILED" build/cook-open-$(1)/log
	@echo "negative control: removing the $(1) check turns the cook forgery battery RED"
endef

# The sabotage keeps the local it defeats IN USE, because a generated C++ file
# with an unused local does not compile under -Werror and a control that fails
# to build is not a control that went red.
.PHONY: tables-cook-open-lengths-negative-control
tables-cook-open-lengths-negative-control: build/cook-open/.stamp
	$(call cook_open_sabotage,lengths,attribution_length != length - data_offset - data_length,if ( attribution_length == UINT64_MAX ) { return NULL; })

.PHONY: tables-cook-open-root-negative-control
tables-cook-open-root-negative-control: build/cook-open/.stamp
	$(call cook_open_sabotage,root,data_length < root_size,if ( root_size == UINT64_MAX ) { return NULL; })

# THE WALK CONTROL, for the OPEN-COST gate (§7.5) rather than the forgery
# battery. The sabotage (tools/sabotage, cook-open-walk-cpp) leaves every check
# in place and makes Open sum every word of the region before it returns, which
# is the one thing the gate exists to refuse; the gate must go RED on its band
# over the same two fixtures the certification run times. ITERATIONS is low
# because an Open that walks 100 MB cannot be opened 200000 times in a
# control's budget, and the gate is a ratio, which the count does not move.
.PHONY: tables-cook-open-walk-negative-control
tables-cook-open-walk-negative-control: build/cook-open/.stamp
	@rm -rf build/cook-open-walk && mkdir -p build/cook-open-walk
	@go run ./tools/sabotage -name cook-open-walk-cpp -out build/cook-open-walk/cook.gotext internal/codegen/cpptable/cook.go
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cook.go":"%s/build/cook-open-walk/cook.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cook-open-walk/overlay.json
	@go build -overlay=build/cook-open-walk/overlay.json -o build/cook-open-walk/schema ./cmd/schema
	./build/cook-open-walk/schema generate --lang cpp --out build/cook-open-walk/gen tables/pointers
	$(CXX) $(COOK_CXXFLAGS) -Ibuild/cook-open-walk/gen -Itest/tables test/tables/cook_main.cpp -o build/cook-open-walk/test
	@if ITERATIONS=20 ./build/cook-open-walk/test time Scene build/cook-open/1mb.cook build/cook-open/100mb.cook > build/cook-open-walk/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the open-cost gate stayed green with a walk in Open"; cat build/cook-open-walk/log; exit 1; \
	fi
	@grep -q "FAILED: open time is not flat" build/cook-open-walk/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on its band"; cat build/cook-open-walk/log; exit 1; }
	@grep "cook open is O(1)" build/cook-open-walk/log
	@grep -m1 "FAILED" build/cook-open-walk/log
	@echo "negative control: a walk in Open turns the open-cost gate RED"

# THE COOK ROUND TRIP THROUGH THE CLI (docs/SPEC-TABLES.md §7.5). The Go tests hold
# the engine; this holds the three COMMANDS and their flags, over a pinned pack
# tree, in both byte orders and with the attribution written both ways.
.PHONY: tables-cook-cli
tables-cook-cli: bin/schema
	@rm -rf build/cook-cli && mkdir -p build/cook-cli
	./bin/schema pack --root PackConfig --out build/cook-cli/orig.bin tables/pack/pinned/PackConfig tables/examples
	./bin/schema cook --root PackConfig --in tables/pack/pinned/PackConfig --out build/cook-cli/tree.cook --verbose tables/examples
	./bin/schema cook-check --root PackConfig --verbose build/cook-cli/tree.cook tables/examples
	./bin/schema uncook --root PackConfig --in build/cook-cli/tree.cook --out build/cook-cli/tree.bin tables/examples
	cmp build/cook-cli/orig.bin build/cook-cli/tree.bin
	./bin/schema cook --root PackConfig --in build/cook-cli/orig.bin --out build/cook-cli/be.cook \
		--byte-order big --attribution build/cook-cli/be.attrib --verbose tables/examples
	@if ./bin/schema cook-check build/cook-cli/be.cook tables/examples 2>/dev/null; then \
		echo "FAILED: a cook carrying data alone was checked anyway"; exit 1; \
	fi
	./bin/schema cook-check --root PackConfig --attribution build/cook-cli/be.attrib --verbose build/cook-cli/be.cook tables/examples
	./bin/schema uncook --root PackConfig --in build/cook-cli/be.cook --attribution build/cook-cli/be.attrib \
		--out build/cook-cli/be.bin tables/examples
	cmp build/cook-cli/orig.bin build/cook-cli/be.bin

# THE BLOCK ZERO-COST GATE (docs/SPEC-TABLES.md §2.2, §19), in its two halves.
#
# The first asks "did any block symbol leak into a Table source?" — a grep.
# The second is the property §19 actually states: **the Table sources are
# BYTE-IDENTICAL with or without the block form**. It is MECHANICAL
# (schema#331): a sabotaged emitter with the block form removed is built
# through `go build -overlay` — the mechanism the negative controls below
# already use — the corpus is generated from it, and every Table source must be
# byte-identical to the ordinary build's. The comparison survives any
# legitimate Table-emitter change, because both arms move together, and it goes
# red precisely when the block form leaks into a Table source.
#
# The pins under testdata/golden/tables/ remain ordinary goldens, held by the
# compiler package's TestGoldenTableSource; a move under an unchanged emitter is
# stop-the-line, exactly as it is for every other golden.
#
# The block form's EMISSION is removed at the one line each backend computes
# it: the emitter is handed a nil block surface, so it writes no Block file and
# no block surface at all. The LAYOUT MODEL is untouched, so the build version
# (docs/SPEC-TABLES.md §20), which is computed from it independently, is the same
# in both arms — the comparison measures the block form's emission, not the id.
BLOCKLESS_ROOT   := build/zero-cost-noblock
BLOCKLESS_SCHEMA := $(BLOCKLESS_ROOT)/schema
# The ordinary arm is generated fresh beside it, by the same macros, so both
# arms hold exactly one unit set. The shared build/tables-generated* roots are
# not that arm: other targets add units to them (cards-cs-generate writes
# cards/, the C conformance build writes a1/ and a2/), and a unit the macros
# never generate would read as one the block-less emitter "did not write".
BLOCKFUL_ROOT    := build/zero-cost-block
BLOCKLESS_SABOTAGE := blocks := (*ir.BlockUnit)(nil) // SABOTAGED: the block form removed (schema$(skip_hash)331)

$(BLOCKLESS_SCHEMA): $(GO_SOURCES) Makefile
	@mkdir -p $(BLOCKLESS_ROOT)
	@sed 's|^\tblocks := ir.Blocks(u)|	$(BLOCKLESS_SABOTAGE)|' \
		internal/codegen/cpptable/cpptable.go > $(BLOCKLESS_ROOT)/cpptable.gotext
	@cmp -s $(BLOCKLESS_ROOT)/cpptable.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "ZERO-COST GATE FAILED: the cpp sabotage patched nothing"; exit 1; } || true
	@sed 's|^\tblocks := ir.Blocks(u)|	$(BLOCKLESS_SABOTAGE)|' \
		internal/codegen/cstable/cstable.go > $(BLOCKLESS_ROOT)/cstable.gotext
	@cmp -s $(BLOCKLESS_ROOT)/cstable.gotext internal/codegen/cstable/cstable.go && \
		{ echo "ZERO-COST GATE FAILED: the cs sabotage patched nothing"; exit 1; } || true
	@sed 's|^\tblocks := ir.Blocks(u)|	$(BLOCKLESS_SABOTAGE)|' \
		internal/codegen/ctable/ctable.go > $(BLOCKLESS_ROOT)/ctable.gotext
	@cmp -s $(BLOCKLESS_ROOT)/ctable.gotext internal/codegen/ctable/ctable.go && \
		{ echo "ZERO-COST GATE FAILED: the c sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/$(BLOCKLESS_ROOT)/cpptable.gotext","%s/internal/codegen/cstable/cstable.go":"%s/$(BLOCKLESS_ROOT)/cstable.gotext","%s/internal/codegen/ctable/ctable.go":"%s/$(BLOCKLESS_ROOT)/ctable.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > $(BLOCKLESS_ROOT)/overlay.json
	go build -overlay=$(BLOCKLESS_ROOT)/overlay.json -o $@ ./cmd/schema

# $(call tables_byte_identical,<label>,<ordinary root>,<block-less root>,<find patterns>,<floor>)
# walks the ordinary root's Table sources, requires each in the block-less root
# byte-for-byte, and returns non-zero if one is missing or moved. The floor is
# the glob's own sanity check, not the property.
define tables_byte_identical
n=0; d=0; \
for f in $$(find $(2) -type f \( $(4) \) | sort); do \
	rel=$${f#$(2)/}; \
	n=$$(( n + 1 )); \
	if [ ! -f "$(3)/$$rel" ]; then \
		echo "ZERO-COST GATE FAILED: the block-less $(1) emitter did not write $$rel"; d=$$(( d + 1 )); \
	elif ! cmp -s "$$f" "$(3)/$$rel"; then \
		echo "ZERO-COST GATE FAILED: $(1) $$rel moved when the block form was removed"; d=$$(( d + 1 )); \
	fi; \
done; \
if [ "$$n" -lt $(5) ]; then echo "ZERO-COST GATE FAILED: $(1) compared $$n Table files, expected at least $(5) — the glob, not the property, is what broke"; d=$$(( d + 1 )); fi; \
if [ "$$d" = "0" ]; then echo "block zero-cost gate: $(1) $$n Table sources byte-identical to the block-less emitter"; fi; \
[ "$$d" = "0" ]
endef

.PHONY: tables-block-zero-cost
tables-block-zero-cost: bin/schema build/tables-generated/.stamp build/tables-generated-cs/.stamp build/tables-generated-c/.stamp $(BLOCKLESS_SCHEMA)
	@for f in build/tables-generated/*/*Table.h build/tables-generated/*/*Table.cpp \
	          build/tables-generated-cs/*/*Table.cs; do \
		if sed -E 's://.*$$::' $$f | grep -nE "TableBlock|[A-Za-z0-9_]Block"; then \
			echo "BLOCK ZERO-COST GATE FAILED: the block form leaked into $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: no Table source carries one symbol of the block form"
# The grep runs over the CODE, with every // comment stripped first: what the
# gate holds is that no block CONSTRUCT is in a Table source, and a comment
# naming one (the arena runtime describes its allocator as the shape
# TableBlockAllocator has, §19.1) is prose, not a symbol.
# BuildVersion is NOT in that grep: it is not a block symbol — a build version
# answers "which build?" and not "which form?", both accelerators carry it
# (docs/SPEC-TABLES.md §20.6), and every C++ Table header carries it because every
# table cooks (§7). What holds the block form to zero cost is the line above —
# no Table source carries one BLOCK symbol — and the byte comparison below.
# C# message and cook writers read the announcement's BuildVersion property;
# the accelerator constant itself still belongs in the Block/Cook runtime.
	@for f in build/tables-generated-cs/*/*Table.cs; do \
		if grep -nE "(^|[[:space:]])(const|readonly)([[:space:]]+static)?[[:space:]]+ulong[[:space:]]+BuildVersion([[:space:]]|=|;)" $$f; then \
			echo "BLOCK ZERO-COST GATE FAILED: the C# Table sources declare the accelerator BuildVersion constant: $$f"; exit 1; \
		fi; \
	done
	@echo "block zero-cost gate: the C# Table sources declare no accelerator build-version constant"
	@rm -rf $(BLOCKLESS_ROOT)/cpp $(BLOCKLESS_ROOT)/cs $(BLOCKLESS_ROOT)/c $(BLOCKFUL_ROOT)
	@mkdir -p $(BLOCKLESS_ROOT)/cpp $(BLOCKLESS_ROOT)/cs $(BLOCKLESS_ROOT)/c $(BLOCKFUL_ROOT)/cpp $(BLOCKFUL_ROOT)/cs $(BLOCKFUL_ROOT)/c
	$(call tables_generate,./bin/schema,$(BLOCKFUL_ROOT)/cpp)
	$(call tables_generate_cs,./bin/schema,$(BLOCKFUL_ROOT)/cs)
	$(call tables_generate_c,./bin/schema,$(BLOCKFUL_ROOT)/c)
	$(call tables_generate_c_collections,./bin/schema,$(BLOCKFUL_ROOT)/c)
	$(call tables_generate,$(BLOCKLESS_SCHEMA),$(BLOCKLESS_ROOT)/cpp)
	$(call tables_generate_cs,$(BLOCKLESS_SCHEMA),$(BLOCKLESS_ROOT)/cs)
	$(call tables_generate_c,$(BLOCKLESS_SCHEMA),$(BLOCKLESS_ROOT)/c)
	$(call tables_generate_c_collections,$(BLOCKLESS_SCHEMA),$(BLOCKLESS_ROOT)/c)
	@if find $(BLOCKLESS_ROOT) -name '*Block.*' | grep -q .; then \
		echo "ZERO-COST GATE FAILED: the block-less emitter still wrote a Block file — the sabotage did not take, and the comparison below would be a restatement"; \
		find $(BLOCKLESS_ROOT) -name '*Block.*'; exit 1; \
	fi
	@$(call tables_byte_identical,cpp,$(BLOCKFUL_ROOT)/cpp,$(BLOCKLESS_ROOT)/cpp,-name '*Table.h' -o -name '*Table.cpp',100)
	@$(call tables_byte_identical,cs,$(BLOCKFUL_ROOT)/cs,$(BLOCKLESS_ROOT)/cs,-name '*Table.cs',40)
	@$(call tables_byte_identical,c,$(BLOCKFUL_ROOT)/c,$(BLOCKLESS_ROOT)/c,-name '*Table.h' -o -name '*Table.c',60)

# THE NEGATIVE CONTROL. The block-less comparison above is a gate with no blade
# until it is shown able to refuse a Table source that moved: the nearest
# neighbour of the property is planted into a COPY of the ordinary corpus — a
# block spelling in a Table source — and the SAME comparison must go red.
# Nothing tracked is written to.
.PHONY: tables-block-zero-cost-negative-control
tables-block-zero-cost-negative-control: build/tables-generated/.stamp
	@rm -rf build/zero-cost-leak && cp -R build/tables-generated build/zero-cost-leak
	@printf 'struct TableBlockAllocator; // PLANTED: a block spelling in a Table source (schema#331)\n' \
		>> build/zero-cost-leak/pointers/GraphTable.h
	@grep -q "PLANTED: a block spelling" build/zero-cost-leak/pointers/GraphTable.h || \
		{ echo "NEGATIVE CONTROL FAILED: the plant did not apply"; exit 1; }
	@if $(call tables_byte_identical,cpp,build/tables-generated,build/zero-cost-leak,-name '*Table.h' -o -name '*Table.cpp',100); then \
		echo "NEGATIVE CONTROL FAILED: a planted block symbol left the byte comparison green"; exit 1; \
	fi
	@echo "negative control: a moved Table source turns the block-less comparison RED"

# THE BUILD VERSION IS ONE NUMBER (docs/SPEC-TABLES.md §20.7): the constant each
# backend emits, and the number `schema build-version` prints, are the same
# number or the tuple a store is indexed by means two different things. The
# projection it hashes is pinned beside it as a golden, so a change to how it
# is computed breaks every pinned value loudly.
.PHONY: tables-block-build-version
tables-block-build-version: bin/schema build/tables-generated/.stamp build/tables-generated-cs/.stamp
	@v=$$(./bin/schema build-version tables/block); \
		grep -q "static const uint64_t BuildVersion = $${v}ull;" build/tables-generated/block/RenderBlock.h || \
			{ echo "BUILD VERSION GATE FAILED: the C++ constant is not $$v"; exit 1; }; \
		grep -q "public const ulong BuildVersion = $${v}UL;" build/tables-generated-cs/block/*Block.cs || \
			{ echo "BUILD VERSION GATE FAILED: the C# constant is not $$v"; exit 1; }; \
		echo "block build-version gate: schema build-version, the C++ constant and the C# constant are all $$v"

# THE FILL REFUSER (docs/SPEC-TABLES.md §19.1, §19.5). The multi-threaded fill is an
# OBLIGATION on the implementation, not a permission to the caller: the
# generated fill path — Begin, the array accessors and the row storage they
# hand back — contains no allocation, no lock and no atomic, and the BUILD
# FAILS if one appears. This reads the generated SOURCE between the fill path's
# own markers; test/tables/block_main.cpp watches the RUNNING program with a
# global operator-new counter, and the two together are what the obligation
# stands on.
BLOCK_FORBIDDEN := malloc|calloc|realloc|operator new|[^_a-zA-Z]new |std::mutex|std::lock_guard|std::unique_lock|std::atomic|std::thread|pthread_|__atomic

.PHONY: tables-block-fill-refuser
tables-block-fill-refuser: build/tables-generated/.stamp
	@rm -rf build/block-fill && mkdir -p build/block-fill
	@for f in build/tables-generated/*/*Block.h; do \
		out=build/block-fill/$$(echo $$f | tr / _); \
		awk '/---- block fill path: begin ----/,/---- block fill path: end ----/' $$f > $$out; \
		regions=$$(grep -c "block fill path: begin ----" $$f || true); \
		ends=$$(grep -c "block fill path: end ----" $$f || true); \
		begins=$$(grep -c "^inline bool .*BlockBegin(" $$f || true); \
		lines=$$(wc -l < $$out | tr -d ' '); \
		if [ "$$regions" != "$$ends" ] || [ "$$regions" != "$$begins" ]; then \
			echo "FILL REFUSER FAILED: $$f has $$regions begin markers, $$ends end markers and $$begins Begin functions — the markers, not the property, are what broke"; exit 1; \
		fi; \
		if [ "$$lines" -lt $$(( 40 * $$regions )) ]; then \
			echo "FILL REFUSER FAILED: $$f extracted $$lines lines for $$regions fill paths — the markers, not the property, are what broke"; exit 1; \
		fi; \
	done
	@if grep -nE "$(BLOCK_FORBIDDEN)" build/block-fill/*; then \
		echo "FILL REFUSER FAILED: the generated fill path allocates, locks or takes an atomic (docs/SPEC-TABLES.md §19.1)"; exit 1; \
	fi
	@echo "block fill refuser: the generated fill path allocates nothing, locks nothing, takes no atomic"

# NEGATIVE CONTROL ONE (the brief's (a)): the fill refuser above must be
# CAPABLE of going red. An allocation and a lock are injected into a generated
# fill path, and the gate must refuse. A green gate proves nothing until it has
# been shown able to fail.
.PHONY: tables-block-fill-refuser-negative-control
tables-block-fill-refuser-negative-control: build/tables-generated/.stamp
	@rm -rf build/block-fill-sabotage && mkdir -p build/block-fill-sabotage
	@cp build/tables-generated/block/RenderBlock.h build/block-fill-sabotage/
	@sed -i.bak 's|// ---- block fill path: begin ----|// ---- block fill path: begin ----\n// sabotaged: static std::mutex lock; void * p = malloc( 1 );|' \
		build/block-fill-sabotage/RenderBlock.h
	@grep -q "sabotaged: static std::mutex" build/block-fill-sabotage/RenderBlock.h || \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@awk '/---- block fill path: begin ----/,/---- block fill path: end ----/' \
		build/block-fill-sabotage/RenderBlock.h > build/block-fill-sabotage/region
	@if grep -qE "$(BLOCK_FORBIDDEN)" build/block-fill-sabotage/region; then \
		echo "block fill refuser negative control: the gate goes red on an injected lock and allocation"; \
	else \
		echo "NEGATIVE CONTROL FAILED: the fill refuser did not see an injected lock and allocation"; exit 1; \
	fi

# NEGATIVE CONTROL THREE (the brief's (c)): the C# side's GENERATED PADDING
# FIELDS are load-bearing, not decoration. Delete them from one padded record
# and the generated layout check must go red — every field after the hole lands
# at the wrong offset, which is exactly what §19.3 says Size alone cannot pin.
.PHONY: tables-block-padding-negative-control
tables-block-padding-negative-control: build/tables-generated-cs/.stamp
ifneq ($(filter cs,$(SKIPPED_LEGS)),)
	@echo "tables-block-padding-negative-control: SKIPPED, it is a C# control and SCHEMA_SKIP_LEGS names the cs leg"
else
	@rm -rf build/block-padding-sabotage && cp -R build/tables-generated-cs/block build/block-padding-sabotage
	@sed -i.bak '/private byte _pad/d' build/block-padding-sabotage/PaddedBlock.cs
	@grep -q "private byte _pad" build/block-padding-sabotage/PaddedBlock.cs && \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; } || true
	@rm -f build/block-padding-sabotage/*.bak
	@if ( cd test/cs-block && $(DOTNET) run -p:BlockGeneratedDir=../../build/block-padding-sabotage ) \
		> build/block-padding-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# layout check passed with the padding fields deleted"; \
		cat build/block-padding-sabotage.log; exit 1; \
	fi
	@grep -q "schema block layout" build/block-padding-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: the failure was not the layout check"; cat build/block-padding-sabotage.log; exit 1; }
	@echo "block padding negative control: deleting the generated padding fields turns the layout check red"
endif

# §19.5's FIRST named negative control: "perturb one row type's pitch constant
# on one side only and the two-language test goes red". The constant is the C#
# side's ShipsStride, which the generated layout check asserts against this
# runtime's own sizeof — so a constant that drifted from the row it describes
# is caught where the drift would matter, and the control proves the assert is
# not inert.
.PHONY: tables-block-pitch-negative-control
tables-block-pitch-negative-control: build/tables-generated-cs/.stamp
ifneq ($(filter cs,$(SKIPPED_LEGS)),)
	@echo "tables-block-pitch-negative-control: SKIPPED, it is a C# control and SCHEMA_SKIP_LEGS names the cs leg"
else
	@rm -rf build/block-pitch-sabotage && cp -R build/tables-generated-cs/block build/block-pitch-sabotage
	@sed -i.bak 's|public const long ShipsStride = 88;|public const long ShipsStride = 96; // SABOTAGED|' \
		build/block-pitch-sabotage/RenderBlock.cs
	@grep -q "SABOTAGED" build/block-pitch-sabotage/RenderBlock.cs || \
		{ echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@rm -f build/block-pitch-sabotage/*.bak
	@if ( cd test/cs-block && $(DOTNET) run -p:BlockGeneratedDir=../../build/block-pitch-sabotage ) \
		> build/block-pitch-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C# side accepted a pitch constant that is not its row's sizeof"; \
		cat build/block-pitch-sabotage.log; exit 1; \
	fi
	@grep -q "ShipsStride" build/block-pitch-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the pitch constant"; cat build/block-pitch-sabotage.log; exit 1; }
	@echo "block pitch negative control: a pitch constant perturbed on ONE side turns the C# layout check red"
endif

# §19.5's SECOND named negative control: "perturb one field's offset in the
# compiler's layout model and the generated asserts go red on both backends".
# This one reaches past the emitters into ir.layoutRecord, so it proves the
# thing the other controls cannot: that the asserts check the COMPILER's model
# rather than restating whatever the target compiler already said.
#
# The sabotaged compiler reaches the build through `go build -overlay`, so no
# tracked file is ever written to — the same mechanism the big-endian negative
# control uses, and for the same reason.
#
# THE C# STAMP IS A PREREQUISITE, as it is on the padding and pitch controls
# beside it. The C# half below overrides BlockGeneratedDir alone, so the
# project's BlockHomeGeneratedDir keeps its default and the blockhome sources
# have to be on disk; without them the build fails on an undefined namespace
# and the control refuses, correctly, that C# went red but not on the layout
# check. Inside `make test` an earlier leg had already generated them, which is
# why the omission stayed invisible until every control ran on its own.
.PHONY: tables-block-layout-model-negative-control
tables-block-layout-model-negative-control: bin/schema build/tables-generated-cs/.stamp
	@mkdir -p build
	@sed 's|ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start, Size: offset - start, Align: fieldAlign})|ml.Fields = append(ml.Fields, FieldLayout{Field: f, Offset: start + 8, Size: offset - start, Align: fieldAlign}) // SABOTAGED: one field, moved|' \
		ir/blocklayout.go > build/blocklayout-moved.gotext
	@cmp -s build/blocklayout-moved.gotext ir/blocklayout.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/ir/blocklayout.go":"%s/build/blocklayout-moved.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/blocklayout-overlay.json
	@go build -overlay=build/blocklayout-overlay.json -o build/schema-layout-moved ./cmd/schema
	@rm -rf build/block-model-sabotage && mkdir -p build/block-model-sabotage/cpp build/block-model-sabotage/cs
	@./build/schema-layout-moved generate --lang cpp --out build/block-model-sabotage/cpp tables/block
	@./build/schema-layout-moved generate --lang cs --out build/block-model-sabotage/cs tables/block
	@if $(CXX) $(BLOCK_CXXFLAGS) -fsyntax-only -Ibuild/block-model-sabotage/cpp \
		build/block-model-sabotage/cpp/RenderBlock.cpp > build/block-model-cpp.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: C++ accepted a moved offset from the compiler's model"; exit 1; \
	fi
	@grep -q "static_assert" build/block-model-cpp.log || \
		{ echo "NEGATIVE CONTROL FAILED: C++ went red, but not on a layout static_assert"; cat build/block-model-cpp.log; exit 1; }
	@echo "block layout-model negative control (C++): a moved offset in the compiler's model turns the static_asserts red"
ifneq ($(filter cs,$(SKIPPED_LEGS)),)
	@echo "block layout-model negative control (C#): SKIPPED, SCHEMA_SKIP_LEGS names the cs leg"
else
	@if ( cd test/cs-block && $(DOTNET) run -p:BlockGeneratedDir=../../build/block-model-sabotage/cs ) \
		> build/block-model-cs.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: C# accepted a moved offset from the compiler's model"; \
		cat build/block-model-cs.log; exit 1; \
	fi
	@grep -q "schema block layout" build/block-model-cs.log || \
		{ echo "NEGATIVE CONTROL FAILED: C# went red, but not on the layout check"; cat build/block-model-cs.log; exit 1; }
	@echo "block layout-model negative control (C#): the same moved offset turns the once-run layout check red"
endif

# THE BLOCK HOME's NEGATIVE CONTROL (docs/SPEC-TABLES.md §19.2's C# surface). The
# dogfood found two defects with one root cause — a C# backend that emitted per
# DECLARING FILE and skipped a file with no `table` in it: the unit's shared
# runtime went to the PROTOCOL ID's home, which declares no table in any real
# unit, and a blittable record went to its declaring file's Block.cs, which is
# never written when that file declares only `type`s. Neither produced a
# diagnostic; both produced a unit that does not compile.
#
# tables/blockhome has exactly that shape, and test/cs-block compiles it.
#
# THE HOME IS <Package>Block.cs NOW (docs/SPEC-TABLES.md §19.2), emitted FOR THE UNIT
# when no file of the unit is named for the package — which is what makes the
# home unconditional and this class of defect structural rather than a rule to
# keep. The sabotage is therefore the one line that guarantees it: take away
# the emitted-for-the-unit home, and the runtime and every blittable record
# have nowhere to land. The compile must FAIL.
.PHONY: tables-block-home-negative-control
tables-block-home-negative-control: bin/schema
ifneq ($(filter cs,$(SKIPPED_LEGS)),)
	@echo "tables-block-home-negative-control: SKIPPED, it is a C# control and SCHEMA_SKIP_LEGS names the cs leg"
else
	@mkdir -p build
	@sed -e 's|if home != "" \&\& !runtimeWritten {|if false \&\& !runtimeWritten { // SABOTAGED: no home is emitted for the unit|' \
		internal/codegen/cstable/block.go > build/csblock-declaring-file.gotext
	@cmp -s build/csblock-declaring-file.gotext internal/codegen/cstable/block.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/block.go":"%s/build/csblock-declaring-file.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csblock-overlay.json
	@go build -overlay=build/csblock-overlay.json -o build/schema-csblock-sabotaged ./cmd/schema
	@rm -rf build/blockhome-sabotage && mkdir -p build/blockhome-sabotage
	@./build/schema-csblock-sabotaged generate --lang cs --out build/blockhome-sabotage tables/blockhome
	@if ( cd test/cs-block && $(DOTNET) build -v q --nologo -p:BlockHomeGeneratedDir=../../build/blockhome-sabotage ) \
		> build/blockhome-sabotage.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the unit compiled with its block runtime emitted nowhere"; \
		cat build/blockhome-sabotage.log; exit 1; \
	fi
	@grep -qE "CS0103|CS0234|CS0246" build/blockhome-sabotage.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on an undefined name"; tail -20 build/blockhome-sabotage.log; exit 1; }
	@echo "block home negative control: a unit with no home for its block runtime does not compile"
endif

# THE RUNTIME HOME IS THE PACKAGE (docs/SPEC-TABLES.md §19.2). A unit's shared C#
# runtime — the table runtime and the text form's walk in <Package>Table.cs, the
# block runtime and its blittable records in <Package>Block.cs, the cook runtime
# in <Package>Cook.cs — is emitted ONCE, and every earlier rule picked WHERE off
# the file order. Adding a file that sorts earlier then relocated ~2,000 lines:
# correct output, and a diff nobody can read (issue #347).
#
# The gate adds exactly such a file to a COPY of tables/examples — Aaa.schema,
# ahead of Guarded.schema — and requires the homes not to move. The table
# runtime is byte-identical across the two trees as well as same-named:
# unit-specific announcement bytes live in the Region metadata file, so a unit
# gaining a table cannot move the shared runtime. The block and cook runtimes DO carry the build version, so
# their names are what is checked.
.PHONY: tables-runtime-home
tables-runtime-home: bin/schema
	@rm -rf build/runtime-home && mkdir -p build/runtime-home/src
	@cp tables/examples/*.schema build/runtime-home/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home/src/Aaa.schema
	@./bin/schema generate --lang cs --out build/runtime-home/base tables/examples
	@./bin/schema generate --lang cs --out build/runtime-home/added build/runtime-home/src
	@for surface in Table Block Cook; do \
		base=$$(cd build/runtime-home/base && grep -l "the unit's shared runtime lives here" *$$surface.cs); \
		added=$$(cd build/runtime-home/added && grep -l "the unit's shared runtime lives here" *$$surface.cs); \
		if [ "$$base" != "Tabledemo$$surface.cs" ] || [ "$$added" != "Tabledemo$$surface.cs" ]; then \
			echo "RUNTIME HOME GATE FAILED: the $$surface runtime is in $$base before the added file and $$added after — expected Tabledemo$$surface.cs both times"; exit 1; \
		fi; \
	done
	@cmp -s build/runtime-home/base/TabledemoTable.cs build/runtime-home/added/TabledemoTable.cs || \
		{ echo "RUNTIME HOME GATE FAILED: the table runtime's bytes moved when the unit gained a file"; exit 1; }
	@echo "runtime home gate: the table, block and cook runtimes stay in <Package><Surface>.cs when an earlier-sorting file joins the unit"

.PHONY: tables-runtime-home-negative-control
tables-runtime-home-negative-control: bin/schema tables-runtime-home
	@sed 's|home := capitalize(u.Package)|home := ir.ProtocolIdHome(u) // SABOTAGED: back to the file order|' \
		internal/codegen/cstable/cstable.go > build/csruntime-fileorder.gotext
	@grep -q SABOTAGED build/csruntime-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cstable/cstable.go":"%s/build/csruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csruntime-overlay.json
	@go build -overlay=build/csruntime-overlay.json -o build/schema-csruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home/base-sabotage build/runtime-home/added-sabotage
	@./build/schema-csruntime-sabotaged generate --lang cs --out build/runtime-home/base-sabotage tables/examples
	@./build/schema-csruntime-sabotaged generate --lang cs --out build/runtime-home/added-sabotage build/runtime-home/src
	@base=$$(cd build/runtime-home/base-sabotage && grep -l "the unit's shared runtime lives here" *Table.cs); \
	 added=$$(cd build/runtime-home/added-sabotage && grep -l "the unit's shared runtime lives here" *Table.cs); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the runtime in $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control: the file-order rule moves the runtime from $$base to $$added"

# THE RUNTIME HOME IS THE PACKAGE (docs/PORTING.md J2). The C++ table emitter has
# no <Package>Table.cs: it writes one header per unit file (out[f.Base+"Table.h"])
# and lets an include guard admit the shared runtime exactly once per translation
# unit. The guards are built from the PACKAGE — strings.ToUpper(pkg) in front of
# every _SCHEMA_TABLE_* suffix — so the home is the guard NAME, not the file that
# happens to sort first. The gate adds Aaa.schema, ahead of Guarded.schema, and
# requires the sorted set of _SCHEMA_TABLE_ guards to stay TABLEDEMO_-prefixed and
# identical across the two trees.
.PHONY: tables-cpp-runtime-home
tables-cpp-runtime-home: bin/schema
	@rm -rf build/runtime-home-cpp && mkdir -p build/runtime-home-cpp/src
	@cp tables/examples/*.schema build/runtime-home-cpp/src/
	@printf 'package tabledemo\n\ntable AaaRow\n{\n    tag uint8\n}\n' > build/runtime-home-cpp/src/Aaa.schema
	@./bin/schema generate --lang cpp --out build/runtime-home-cpp/base tables/examples
	@./bin/schema generate --lang cpp --out build/runtime-home-cpp/added build/runtime-home-cpp/src
	@base=$$(cd build/runtime-home-cpp/base && grep -ho '#ifndef [A-Z0-9_]*_SCHEMA_TABLE[A-Z0-9_]*' *Table.h | awk '{print $$2}' | sort -u); \
	 added=$$(cd build/runtime-home-cpp/added && grep -ho '#ifndef [A-Z0-9_]*_SCHEMA_TABLE[A-Z0-9_]*' *Table.h | awk '{print $$2}' | sort -u); \
	 if [ -z "$$base" ]; then echo "RUNTIME HOME GATE FAILED: the base tree has no _SCHEMA_TABLE_ guards — the gate is comparing two nothings"; exit 1; fi; \
	 if [ -z "$$added" ]; then echo "RUNTIME HOME GATE FAILED: the added tree has no _SCHEMA_TABLE_ guards — the gate is comparing two nothings"; exit 1; fi; \
	 for guard in $$base $$added; do \
		case $$guard in TABLEDEMO_*) ;; *) echo "RUNTIME HOME GATE FAILED: guard $$guard is not prefixed TABLEDEMO_"; exit 1;; esac; \
	 done; \
	 if [ "$$base" != "$$added" ]; then echo "RUNTIME HOME GATE FAILED: the guard sets differ — base [$$base] vs added [$$added]"; exit 1; fi
	@# THE BYTES: within one tree every header carries the guarded runtime blocks
	@# byte-for-byte alike, so whichever file a translation unit includes first
	@# defines the same runtime and the include order decides nothing. The two
	@# trees MAY differ from each other: the announcement, the id vocabulary and
	@# BuildVersion are the UNIT's, and the added file is part of the unit.
	@for tree in base added; do \
		all=$$(test/cpp-tables/runtime_home_digest.sh build/runtime-home-cpp/$$tree); \
		files=$$(printf '%s\n' "$$all" | grep -c .); \
		distinct=$$(printf '%s\n' "$$all" | awk '{print $$1, $$2}' | sort -u | grep -c .); \
		if [ "$$files" -lt 2 ]; then echo "RUNTIME HOME GATE FAILED: only $$files headers of the $$tree tree carry a runtime — the gate is comparing nothing"; exit 1; fi; \
		if [ "$$distinct" != 1 ]; then echo "RUNTIME HOME GATE FAILED: the $$tree tree's $$files headers carry $$distinct different runtimes"; printf '%s\n' "$$all"; exit 1; fi; \
	done
	@echo "runtime home gate (cpp): every _SCHEMA_TABLE_ guard is named by the package, the sorted set is identical across the two trees, and within each tree the guarded runtime is byte-identical in every header"

.PHONY: tables-cpp-runtime-home-negative-control
tables-cpp-runtime-home-negative-control: bin/schema tables-cpp-runtime-home
	@sed 's|guard := strings.ToUpper(pkg) + "_SCHEMA_TABLE_PRIMITIVES"|guard := strings.ToUpper(ir.ProtocolIdHome(u)) + "_SCHEMA_TABLE_PRIMITIVES" // SABOTAGED: back to the file order|' \
		internal/codegen/cpptable/cpptable.go > build/cppruntime-fileorder.gotext
	@grep -q SABOTAGED build/cppruntime-fileorder.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cppruntime-fileorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cppruntime-overlay.json
	@go build -overlay=build/cppruntime-overlay.json -o build/schema-cppruntime-sabotaged ./cmd/schema
	@rm -rf build/runtime-home-cpp/base-sabotage build/runtime-home-cpp/added-sabotage
	@./build/schema-cppruntime-sabotaged generate --lang cpp --out build/runtime-home-cpp/base-sabotage tables/examples
	@./build/schema-cppruntime-sabotaged generate --lang cpp --out build/runtime-home-cpp/added-sabotage build/runtime-home-cpp/src
	@base=$$(cd build/runtime-home-cpp/base-sabotage && grep -ho '#ifndef [A-Z0-9_]*_SCHEMA_TABLE_PRIMITIVES[A-Z0-9_]*' *Table.h | awk '{print $$2}' | sort -u); \
	 added=$$(cd build/runtime-home-cpp/added-sabotage && grep -ho '#ifndef [A-Z0-9_]*_SCHEMA_TABLE_PRIMITIVES[A-Z0-9_]*' *Table.h | awk '{print $$2}' | sort -u); \
	 if [ "$$base" = "$$added" ]; then \
		echo "NEGATIVE CONTROL FAILED: the file-order rule kept the guard $$base — the gate is watching nothing"; exit 1; \
	 fi; \
	 echo "runtime home negative control (cpp): the file-order rule moves the guard from $$base to $$added"

# THE BYTES HALF'S CONTROL: one header's guarded runtime made to differ from
# its siblings' — the shape a runtime that follows the file it was emitted into
# would have — in a COPY of the added tree, and the gate's own digest must see it.
.PHONY: tables-cpp-runtime-home-bytes-negative-control
tables-cpp-runtime-home-bytes-negative-control: bin/schema tables-cpp-runtime-home
	@rm -rf build/runtime-home-cpp/bytes-sabotage && cp -R build/runtime-home-cpp/added build/runtime-home-cpp/bytes-sabotage
	@f=$$(ls build/runtime-home-cpp/bytes-sabotage/*Table.h | head -1); \
	 sed 's|^#define \([A-Z0-9_]*_SCHEMA_TABLE_PRIMITIVES\)$$|#define \1 // SABOTAGED: follows the file|' $$f > $$f.sabotaged && mv $$f.sabotaged $$f; \
	 grep -q SABOTAGED $$f || { echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@distinct=$$(test/cpp-tables/runtime_home_digest.sh build/runtime-home-cpp/bytes-sabotage | awk '{print $$1, $$2}' | sort -u | wc -l | tr -d ' '); \
	 rm -rf build/runtime-home-cpp/bytes-sabotage; \
	 if [ "$$distinct" = 1 ]; then echo "NEGATIVE CONTROL FAILED: a runtime that follows its file left the digest unchanged — the bytes half is watching nothing"; exit 1; fi; \
	 echo "runtime home bytes negative control (cpp): a header whose runtime follows its file yields $$distinct distinct digests"

# DEFECT B's NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.7's DEPTH ONE, BOUNDED ONLY).
# The dogfood found the C# blittable emitter projecting a bounded array INSIDE
# a nested record out of line — a sixteen-byte triple where C++ put the whole
# array, and every field after it somewhere else. Nothing said so until
# Verify() threw on the first Open, in the game.
#
# This puts the defect back — through `go build -overlay`, so no tracked file
# is written — and requires the gate to go red. The gate has TWO halves and
# either firing is it working: COMPILING every corpus unit's generated C# is
# half (a projected array is not the struct the rest of the unit indexes), and
# running each unit's Verify() is the other (a projected array moves the sizes
# and offsets). The control reports which one fired.
.PHONY: tables-block-inline-array-negative-control
tables-block-inline-array-negative-control: bin/schema
ifneq ($(filter cs,$(SKIPPED_LEGS)),)
	@echo "tables-block-inline-array-negative-control: SKIPPED, it is a C# control and SCHEMA_SKIP_LEGS names the cs leg"
else
	@mkdir -p build
	@sed -e 's|if projection \&\& ir.BlockOutOfLine(f) {|if ir.BlockOutOfLine(f) { // SABOTAGED: project at every depth|' \
	     -e 's|inline := !projection \|\| !ir.BlockOutOfLine(f)|inline := !ir.BlockOutOfLine(f)|' \
		internal/codegen/cstable/block.go > build/csblock-depth.gotext
	@cmp -s build/csblock-depth.gotext internal/codegen/cstable/block.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cstable/block.go":"%s/build/csblock-depth.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/csblock-depth-overlay.json
	@go build -overlay=build/csblock-depth-overlay.json -o build/schema-depth-sabotaged ./cmd/schema
	@rm -rf build/blockhome-depth && mkdir -p build/blockhome-depth
	@./build/schema-depth-sabotaged generate --lang cs --out build/blockhome-depth tables/blockhome
	@if ( cd test/cs-block && $(DOTNET) run -p:BlockHomeGeneratedDir=../../build/blockhome-depth ) \
		> build/blockhome-depth.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a bounded array projected inside a nested record passed the layout gate"; \
		cat build/blockhome-depth.log; exit 1; \
	fi
	@if grep -q "schema block layout" build/blockhome-depth.log; then \
		echo "block inline-array negative control: the LAYOUT CHECK went red — a projected array inside a nested record moves the offsets"; \
	elif grep -qE "CS1061|CS0246|CS0117" build/blockhome-depth.log; then \
		echo "block inline-array negative control: the COMPILE went red — a projected array is not the struct the rest of the generated unit indexes"; \
	else \
		echo "NEGATIVE CONTROL FAILED: it went red, but not on either half of the gate"; tail -20 build/blockhome-depth.log; exit 1; \
	fi
endif

# GATE 2 (docs/SPEC-TABLES.md §12.1): the MEASURED gate, two numbers, and it is not
# part of `make test` on purpose — a correctness suite whose verdict depends on
# the machine's mood is not a correctness suite, and the estate's bench rules
# want one bench at a time per machine and a quiet window.
#
# The C++ half writes the representative frame it measured into
# build/block_gate2.bin and the C# half reads THE SAME BYTES, so the two
# numbers are taken over one frame rather than over two descriptions of one.
# Each half runs its own golden gate first and REFUSES to bench on a mismatch.
build/schema_test_block_gate2: build/tables-generated/.stamp test/tables/block_gate2_main.cpp
	@mkdir -p build
	$(CXX) -O2 $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_gate2_main.cpp $(BLOCK_SOURCES) -o $@

.PHONY: tables-block-gate2
tables-block-gate2: build/schema_test_block_gate2 build/tables-generated-cs/.stamp
	./build/schema_test_block_gate2
	cd test/cs-block && $(DOTNET) run -c Release -- --gate2

# THE SMOKE, which is what CI runs (certification legs only, never a PR).
# The gate above is a MEASUREMENT and its verdict belongs on a quiet box; a
# gate nothing runs anywhere is a gate that rots, which is the other failure.
# So the correctness half — the two arms agreeing byte for byte across all
# nine sections, and the frame the C# half reads — runs on every push to main
# and nightly, at a small N, with the 5% band NOT enforced and both halves
# saying so. A drift in what the generated form WRITES goes red here; a slow
# runner does not.
.PHONY: tables-block-gate2-smoke
tables-block-gate2-smoke: build/schema_test_block_gate2 build/tables-generated-cs/.stamp
	./build/schema_test_block_gate2 --smoke
	cd test/cs-block && $(DOTNET) run -c Release -- --gate2-smoke
# The NEGATIVE CONTROL for a KEYED object's duplicate counting
# (docs/SPEC-TABLES.md §16.2). Last-wins inside a keyed object was already true, so
# the missing count was invisible to every round-trip test — the value was
# right and only the ledger was wrong. This sabotage removes the increment and
# the fixture must go red; without it, nothing in the suite could see the
# defect the pack engine's two-sided gate found.
.PHONY: tables-json-keyed-dup-negative-control
tables-json-keyed-dup-negative-control: bin/schema test/tables/json_keyed_dup_negative_main.cpp
	@rm -rf build/json-dup-sabotage && mkdir -p build/json-dup-sabotage
	./bin/schema generate --lang cpp --out build/json-dup-sabotage tables/examples
	@sed -i.bak 's|if ( ( seen\[slot >> 6\] \& bit ) != 0 ) { in.report->duplicate++; }|if ( ( seen[slot >> 6] \& bit ) != 0 ) { /* sabotaged: no count */ }|' build/json-dup-sabotage/KeyedTable.cpp
	@grep -q 'sabotaged: no count' build/json-dup-sabotage/KeyedTable.cpp || { echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-dup-sabotage test/tables/json_keyed_dup_negative_main.cpp build/json-dup-sabotage/KeyedTable.cpp -o build/schema_test_json_keyed_dup_negative
	./build/schema_test_json_keyed_dup_negative

# The NEGATIVE CONTROL for the clamp's PREFIX rule (docs/SPEC-TABLES.md §16.2).
# A clamped string keeps a PREFIX of the text, and a scan that resumes after one
# code point fails to fit stores bytes the input never spelled in that order
# while the `clamped` count stays right. That is invisible to any test that
# reads only counters. This sabotage drops the stop and the fixture must go red.
#
# THE CONTROL IS RUN BOTH WAYS, because a binary that only ever meets the
# sabotage cannot show it has a blade: against the sabotaged walker it must SEE
# the wrong bytes, and against the HONEST one it must find nothing to see. The
# second arm is the one that goes red for a control whose verdict is whatever it
# read rather than a reading of the bytes, which is what a control becomes the
# moment a failed read or a moved clamp count counts as success.
.PHONY: tables-json-clamp-prefix-negative-control
tables-json-clamp-prefix-negative-control: bin/schema test/tables/json_clamp_prefix_negative_main.cpp
	@rm -rf build/json-clamp-sabotage && mkdir -p build/json-clamp-sabotage
	./bin/schema generate --lang cpp --out build/json-clamp-sabotage tables/examples
	@sed -i.bak 's|else if ( !clamped \&\& placed + unit_length <= capacity )|else if ( placed + unit_length <= capacity ) // SABOTAGED|' build/json-clamp-sabotage/TablesTable.cpp
	@grep -q 'SABOTAGED' build/json-clamp-sabotage/TablesTable.cpp || { echo "NEGATIVE CONTROL: the sabotage did not apply"; exit 1; }
	@mkdir -p build
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-clamp-sabotage test/tables/json_clamp_prefix_negative_main.cpp build/json-clamp-sabotage/TablesTable.cpp -o build/schema_test_json_clamp_prefix_negative
	./build/schema_test_json_clamp_prefix_negative
	@rm -rf build/json-clamp-honest && mkdir -p build/json-clamp-honest
	@./bin/schema generate --lang cpp --out build/json-clamp-honest tables/examples
	@$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off \
		-Ibuild/json-clamp-honest test/tables/json_clamp_prefix_negative_main.cpp build/json-clamp-honest/TablesTable.cpp -o build/schema_test_json_clamp_prefix_honest
	@if ./build/schema_test_json_clamp_prefix_honest > build/json-clamp-honest.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the control reported the defect against the HONEST walker,"; \
		echo "      so what it prints is not a reading of the bytes"; \
		cat build/json-clamp-honest.log; exit 1; \
	fi
	@echo "negative control: the clamp prefix control sees the defect under the sabotage and nothing without it"

# THE NEGATIVE CONTROL FOR PER-CASE ABSENCE (test/conformance/README.md).
#
# An absence is a driver's own claim, so the mechanism could hide a real hole:
# a leg that went absent on a case it should answer would print a smaller pass
# and no failure at all. What stops that is the rule beside it — THE REFERENCE
# LEG MAY NOT ANSWER ABSENT — and this is the control that shows the rule is
# load-bearing rather than decorative.
#
# It sabotages the C++ driver into claiming absence for every instance whose
# unit is pointered, which is exactly what a port legitimately does, and
# requires the harness to go RED on the reference leg.
# The Go leg is a PREREQUISITE, not an assumption: the second half below runs
# the harness over a substituted registry naming the Go driver, and that driver
# execs build/conformance-go. Within `make test` the Go leg is already built by
# the time this runs, which is why the omission stayed invisible; run this
# target on its own and it fails on a missing binary rather than on its own
# question.
.PHONY: conformance-negative-control-absent
conformance-negative-control-absent: build/conformance-harness build/tables-generated/.stamp build/conformance-go
	@mkdir -p build
	@sed -e 's|if ( variable != NULL )$$|if ( variable != NULL \&\& !spill( out, f[1] + ".absent", "", 0 ) ) { return 1; } /* SABOTAGED */\n        if ( variable != NULL ) { continue; }\n        if ( false )|' \
		test/conformance/cpp/main.cpp > build/conformance-cpp-absent.cpp
	@cmp -s build/conformance-cpp-absent.cpp test/conformance/cpp/main.cpp && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) build/conformance-cpp-absent.cpp \
		$(CONFORMANCE_SOURCES) -o build/conformance-cpp
	@if ./build/conformance-harness run --only cpp > build/conformance-absent.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the REFERENCE leg answered ABSENT and the harness stayed green"; \
		$(MAKE) build/conformance-cpp; exit 1; \
	fi
	@grep -q "REFERENCE leg answered ABSENT" build/conformance-absent.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the reference rule"; \
		  cat build/conformance-absent.log; $(MAKE) build/conformance-cpp; exit 1; }
	@rm -f build/conformance-cpp
	@$(MAKE) build/conformance-cpp
	@echo "negative control: an ABSENCE from the reference leg turns the harness red"
# AND THE OTHER DIRECTION, which is the half a one-sided control would have
# missed and did: the rule belongs to the COMMITTED registry alone. A run handed
# a substituted one — the big-endian leg writes a single-driver file for the Go
# port — is one leg of a port, so its first line is not the reference and its
# absences are ordinary. Without this, the rule fires on every cross-endian run.
	@printf 'go test/conformance/go/driver\n' > build/conformance-solo-drivers.txt
	./build/conformance-harness run --drivers build/conformance-solo-drivers.txt --work build/conformance-solo-work
	@echo "negative control: the same absences under a SUBSTITUTED registry are ordinary, and the leg passes"

# THE SAME RULE AT THE COARSER GRAIN (schema#467): a WHOLE SURFACE the reference
# leg never registers.
#
# A reference leg that drops a surface from `list` — or exits 2 on it — leaves
# every other leg comparing against a surface nothing pins, which is the same
# hole a per-case absence there would leave and is red for the same reason. The
# success footer may not print beside it, and this is the control that says so.
#
# The sabotage is on the DRIVER rather than on an emitter (docs/PORTING.md I2)
# because `list` is the driver's own answer and nothing generates it: a WRAPPER
# around the committed reference driver, filtering one surface out of its list,
# planted as the reference of a scratch DISCOVERED registry — the rule belongs
# to that registry alone, so a substituted one would not exercise it. The
# wrapper refuses rather than filtering nothing, so a control that removed no
# surface says so instead of reading as a pass. No tracked file is written.
CONFORMANCE_REFERENCE_MISSING := build/conformance-reference-missing
CONFORMANCE_REFERENCE_SURFACE := block-dump
.PHONY: conformance-negative-control-reference-surface
conformance-negative-control-reference-surface: build/conformance-harness build/conformance-cpp build/schema_test_cook
	@rm -rf $(CONFORMANCE_REFERENCE_MISSING)
	@mkdir -p $(CONFORMANCE_REFERENCE_MISSING)/registry/cpp
	@printf '#!/bin/sh\nif [ "$$2" != list ]; then exec test/conformance/cpp/driver "$$@"; fi\ntest/conformance/cpp/driver "$$1" list > %s/listed.txt\ngrep -q "^$(CONFORMANCE_REFERENCE_SURFACE)$$" %s/listed.txt || { echo "the reference leg registers no $(CONFORMANCE_REFERENCE_SURFACE) surface: this control removes nothing" >&2; exit 1; }\ngrep -v "^$(CONFORMANCE_REFERENCE_SURFACE)$$" %s/listed.txt\n' \
		"$(CONFORMANCE_REFERENCE_MISSING)" "$(CONFORMANCE_REFERENCE_MISSING)" "$(CONFORMANCE_REFERENCE_MISSING)" \
		> $(CONFORMANCE_REFERENCE_MISSING)/registry/cpp/driver
	@chmod +x $(CONFORMANCE_REFERENCE_MISSING)/registry/cpp/driver
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_REFERENCE_MISSING)/registry \
			--work $(CONFORMANCE_REFERENCE_MISSING)/work > $(CONFORMANCE_REFERENCE_MISSING)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the REFERENCE leg registered no $(CONFORMANCE_REFERENCE_SURFACE) surface and the harness stayed green"; \
		cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; \
	fi
	@grep -q "REFERENCE leg is ABSENT on the whole $(CONFORMANCE_REFERENCE_SURFACE) surface" $(CONFORMANCE_REFERENCE_MISSING)/log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the reference rule"; \
		  cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; }
	@if grep -q "every registered surface passes" $(CONFORMANCE_REFERENCE_MISSING)/log; then \
		echo "NEGATIVE CONTROL FAILED: the success footer printed beside a reference ABSENT"; \
		cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; \
	fi
	@grep -q "wire          pass" $(CONFORMANCE_REFERENCE_MISSING)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_REFERENCE_MISSING)/log; exit 1; }
	@grep -m1 "cpp / $(CONFORMANCE_REFERENCE_SURFACE)" $(CONFORMANCE_REFERENCE_MISSING)/log
	@echo "negative control: a SURFACE the reference leg never registers turns the harness red"

# THE CROSS-IMPLEMENTATION LOCK for the FLAT NODE TABLE (docs/SPEC-TABLES.md
# §3.1), and it is what makes TWO implementations of one wire mean something.
#
# The compiler's engine (internal/tablewire) and the generated C++ backend were
# not written from each other. A golden proves each against its own past; this
# proves each against the OTHER, in both directions, over a graph carrying every
# shape the numbering has to get right — a shared node named from two places, a
# chain, a diamond that closes on a node already numbered, a variable table
# nested by value, and a null in a pointer-shaped slot.
#
#   C++ writes -> the TOOL cooks and uncooks it -> byte-identical
#   the TOOL writes -> C++ loads and re-saves it -> byte-identical
#
# The second direction also checks the SIZES: the region bytes and the
# attribution bytes C++ computes from the framing alone are the data and
# attribution parts the tool's cook writes.
.PHONY: tables-flat-wire
tables-flat-wire: build/tables-generated/.stamp bin/schema test/tables/flatwire_main.cpp
	@mkdir -p build/flatwire
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/pointers -Itest/tables \
		test/tables/flatwire_main.cpp -o build/schema_test_flatwire
	./build/schema_test_flatwire write build/flatwire/cpp.wire
	./bin/schema cook --root Scene --in build/flatwire/cpp.wire --out build/flatwire/cpp.cook --verbose tables/pointers
	./bin/schema uncook --root Scene --in build/flatwire/cpp.cook --out build/flatwire/cpp-back.wire tables/pointers
	cmp build/flatwire/cpp.wire build/flatwire/cpp-back.wire
	@echo "flat wire: C++ wrote it, the tool read it, and the bytes came back identical"
	@go build -o build/cookgen ./test/cookgen
	./build/cookgen --bytes 4096 --root Scene --ref head --chain ListNode --next next --values --out build/flatwire/tool.cook
	./bin/schema uncook --root Scene --in build/flatwire/tool.cook --out build/flatwire/tool.wire --verbose tables/pointers
	./build/schema_test_flatwire reload build/flatwire/tool.wire build/flatwire/tool-back.wire
	cmp build/flatwire/tool.wire build/flatwire/tool-back.wire
	@echo "flat wire: the tool wrote it, C++ read it, and the bytes came back identical"

# ITS NEGATIVE CONTROL: the numbering is a FIRST-VISIT order, and getting the
# order wrong is the defect a byte comparison exists to catch. Numbering a
# node's children before the node itself is a legal-looking walk that produces a
# different, self-consistent wire — so the C++ side still round-trips its own
# bytes, and only the CROSS-IMPLEMENTATION comparison goes red.
.PHONY: tables-flat-wire-negative-control
tables-flat-wire-negative-control: bin/schema test/tables/flatwire_main.cpp
	@mkdir -p build/flatwire
	@sed -e 's|if ( !TableNumberingAppend( numbering, node ) ) { return false; }\\n")|if ( !%sNumber( ctx, numbering, *pointee ) ) { return false; } // SABOTAGED\\n", t)|' \
	     -e 's|if ( !%sNumber( ctx, numbering, \*pointee ) ) { return false; }\\n", t)|if ( !TableNumberingAppend( numbering, node ) ) { return false; }\\n")|' \
		internal/codegen/cpptable/pointers.go > build/pointers-postorder.gotext
	@cmp -s build/pointers-postorder.gotext internal/codegen/cpptable/pointers.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/pointers.go":"%s/build/pointers-postorder.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/pointers-postorder-overlay.json
	@go build -overlay=build/pointers-postorder-overlay.json -o build/schema-postorder ./cmd/schema
	@rm -rf build/tables-postorder && mkdir -p build/tables-postorder
	./build/schema-postorder generate --lang cpp --out build/tables-postorder/pointers tables/pointers
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-postorder/pointers -Itest/tables \
		test/tables/flatwire_main.cpp -o build/schema_test_flatwire_postorder
	./build/schema_test_flatwire_postorder write build/flatwire/postorder.wire
	@if cmp -s build/flatwire/postorder.wire build/flatwire/cpp.wire; then \
		echo "NEGATIVE CONTROL FAILED: numbering a node AFTER its children moved no byte"; exit 1; \
	fi
	@if ./bin/schema cook --root Scene --in build/flatwire/postorder.wire --out build/flatwire/postorder.cook tables/pointers >/dev/null 2>&1 && \
	    ./bin/schema uncook --root Scene --in build/flatwire/postorder.cook --out build/flatwire/postorder-back.wire tables/pointers >/dev/null 2>&1 && \
	    cmp -s build/flatwire/postorder.wire build/flatwire/postorder-back.wire; then \
		echo "NEGATIVE CONTROL FAILED: the tool agreed with a walk that is not first-visit pre-order"; exit 1; \
	fi
	@echo "negative control: numbering a node after its children turns the CROSS-IMPLEMENTATION lock red"

# The NEGATIVE CONTROL for the SHARED NODE (docs/SPEC-TABLES.md §6.2). Lock's
# whole claim is that its walk carries one entry per node, so a node two
# references name is packed ONCE and both references resolve to it. A pack that
# duplicates reads correct through either reference — every field is right, the
# graph is walkable, the region relocates — which is exactly why the defect
# lived under a green suite until the byte count was measured.
#
# It sabotages the EMITTER's identity map so that a CLOSED entry reads as a
# first visit again — which is exactly the defect, and no more than it: a
# reference to an OPEN entry is still the cycle it is, so the sabotage removes
# identity without removing the refusal that keeps the walk terminating. THE
# TABLES SUITE ITSELF must go red. The sabotaged emitter reaches the compiler
# through `go build -overlay`, so no tracked file is ever written to.
.PHONY: tables-shared-node-negative-control
tables-shared-node-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|taken = entry->key != key;|taken = entry->key != key \|\| entry->open == 0; // SABOTAGED: a CLOSED entry is a first visit again|' \
		internal/codegen/cpptable/arena.go > build/cpptable-no-identity.gotext
	@cmp -s build/cpptable-no-identity.gotext internal/codegen/cpptable/arena.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/arena.go":"%s/build/cpptable-no-identity.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-no-identity-overlay.json
	@go build -overlay=build/cpptable-no-identity-overlay.json -o build/schema-no-identity ./cmd/schema
	@rm -rf build/tables-no-identity && mkdir -p build/tables-no-identity
	$(call tables_generate,./build/schema-no-identity,build/tables-no-identity)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-no-identity) \
		test/tables/main.cpp $$(ls build/tables-no-identity/*/*Table.cpp) -o build/schema_test_tables_no_identity
	@if ./build/schema_test_tables_no_identity > build/tables-no-identity.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a pack with no identity map left the tables suite GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/main.cpp" build/tables-no-identity.log || \
		{ echo "NEGATIVE CONTROL FAILED: the suite went red, but not on a CHECK"; cat build/tables-no-identity.log; exit 1; }
	@echo "negative control: a pack with no identity map turns the TABLES SUITE red on $$(grep -c '^FAIL' build/tables-no-identity.log) failures"

# The NEGATIVE CONTROL for a keyed array's ITERATION RANGE (docs/SPEC-TABLES.md
# §2.4). The iteration's whole promise is that it walks EVERY stored slot and
# yields the KEY it holds, 1..E.Max; an off-by-one at either end reads as an
# ordinary walk, because every untouched slot holds the same declared defaults.
#
# It sabotages the EMITTER's begin() past the first stored slot and requires
# THE TABLES SUITE ITSELF — the same test/tables/main.cpp the leg runs, against
# a whole corpus regenerated from the sabotaged compiler — to go red. A
# purpose-written fixture would only prove the sabotage is observable by a
# fixture written for it; what has to be shown is that the GUARDED test reddens.
#
# The sabotaged emitter reaches the compiler through `go build -overlay`, so no
# tracked file is ever written to (the big-endian control's rule).
.PHONY: tables-keyed-iteration-negative-control
tables-keyed-iteration-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|Iterator begin() { return Iterator{ slots, 0 }; }|Iterator begin() { return Iterator{ slots, 1 }; } // SABOTAGED|' \
	     -e 's|ConstIterator begin() const { return ConstIterator{ slots, 0 }; }|ConstIterator begin() const { return ConstIterator{ slots, 1 }; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-first-slot.gotext
	@cmp -s build/cpptable-first-slot.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-first-slot.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-first-slot-overlay.json
	@go build -overlay=build/cpptable-first-slot-overlay.json -o build/schema-first-slot ./cmd/schema
	@rm -rf build/tables-first-slot && mkdir -p build/tables-first-slot
	$(call tables_generate,./build/schema-first-slot,build/tables-first-slot)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-first-slot) \
		test/tables/main.cpp $$(ls build/tables-first-slot/*/*Table.cpp) -o build/schema_test_tables_first_slot
	@if ./build/schema_test_tables_first_slot > build/tables-first-slot.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: begin() past the first stored slot left the tables suite GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/main.cpp" build/tables-first-slot.log || \
		{ echo "NEGATIVE CONTROL FAILED: the suite went red, but not on a CHECK"; cat build/tables-first-slot.log; exit 1; }
	@echo "negative control: begin() past the first stored slot turns the TABLES SUITE red on $$(grep -c '^FAIL' build/tables-first-slot.log) failures"

# THE None REFUSAL, HELD UNDER -DNDEBUG (docs/SPEC-TABLES.md §2.4). The refusal is
# unconditional by ruling: indexing a keyed array by None is a program error in
# EVERY configuration, because the shifted storage has no slot for None and a
# build that let the index through would read one element BEFORE the array.
#
# The tables suite proves the refusal fires, but it compiles with asserts LIVE,
# so it cannot tell an unconditional refusal from an assert. This gate compiles
# ONE translation unit -DNDEBUG — the configuration a game ships, and the one
# that removes an assert — and requires the None index to end the program
# there too.
.PHONY: tables-keyed-none-refusal-ndebug
tables-keyed-none-refusal-ndebug: build/tables-generated/.stamp test/tables/keyed_none_ndebug_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-generated/examples \
		test/tables/keyed_none_ndebug_main.cpp -o build/schema_test_keyed_none_ndebug
	./build/schema_test_keyed_none_ndebug

# THE HOOKS (docs/SPEC-TABLES.md §13.9, docs/USAGE.md): a consumer supplies its
# own assert, its own fatal handler and its own allocate/free pair, and every
# call the table runtime makes has to land in them. This unit defines all four
# before it includes a generated header, so it observes what a consumer
# observes.
#
# It carries its OWN negative control rather than a sabotage build, and that is
# the stronger form here: the DEFAULT pair — schema_allocate / schema_release —
# is where a bypassing malloc, calloc, realloc or free would land, and the test
# defines it to a separate counter that must read ZERO. Put one C-library call
# back anywhere in the arena, the pack walk, the numbering, the region or the
# node directory and that counter fires.
.PHONY: tables-hooks
tables-hooks: build/tables-generated/.stamp test/tables/hooks_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/hooks_main.cpp \
		$$(ls build/tables-generated/pointers/*Table.cpp build/tables-generated/blobs/*Table.cpp) -o build/schema_test_hooks
	./build/schema_test_hooks

# and its NEGATIVE CONTROL: put the refusal back to a bare assert — the shape
# the ruling replaced — and the gate above must go RED, because -DNDEBUG then
# removes it. A gate that only ever passes proves nothing about what it checks.
.PHONY: tables-keyed-none-refusal-negative-control
tables-keyed-none-refusal-negative-control: bin/schema test/tables/keyed_none_ndebug_main.cpp
	@mkdir -p build
	@sed -e 's|            schema_fatal();|            /* SABOTAGED: a debug-only guard again */|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-assert-only.gotext
	@grep -q SABOTAGED build/cpptable-assert-only.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-assert-only.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-assert-only-overlay.json
	@go build -overlay=build/cpptable-assert-only-overlay.json -o build/schema-assert-only ./cmd/schema
	@rm -rf build/tables-assert-only && mkdir -p build/tables-assert-only
	@./build/schema-assert-only generate --lang cpp --out build/tables-assert-only tables/examples
	@$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-assert-only \
		test/tables/keyed_none_ndebug_main.cpp -o build/schema_test_keyed_none_assert_only
	@if ./build/schema_test_keyed_none_assert_only > build/keyed-none-assert-only.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a debug-only guard left the -DNDEBUG gate GREEN"; exit 1; \
	fi
	@grep -q "the refusal was compiled out" build/keyed-none-assert-only.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the refusal"; cat build/keyed-none-assert-only.log; exit 1; }
	@echo "negative control: a debug-only guard turns the -DNDEBUG refusal gate red"

# THE OTHER END OF THE SAME REFUSAL, HELD UNDER -DNDEBUG (docs/SPEC-TABLES.md
# §2.4, issue #377). The storage holds one slot per NAMED variant, so a key
# past Max names a variant this enum does not have: the same program error as
# None, refused at the same compare, in every configuration. A build that let
# it through would read PAST THE END of the storage.
.PHONY: tables-keyed-max-refusal-ndebug
tables-keyed-max-refusal-ndebug: build/tables-generated/.stamp test/tables/keyed_max_ndebug_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-generated/examples \
		test/tables/keyed_max_ndebug_main.cpp -o build/schema_test_keyed_max_ndebug
	./build/schema_test_keyed_max_ndebug

# and its NEGATIVE CONTROL: put the accessor back to the None-ONLY compare —
# what it refused before #377 — and the gate above must go RED, because a key
# past Max then indexes past the end. Deleting the abort would turn both gates
# red at once and prove nothing about THIS end, so the sabotage is the compare
# itself.
.PHONY: tables-keyed-max-refusal-negative-control
tables-keyed-max-refusal-negative-control: bin/schema test/tables/keyed_max_ndebug_main.cpp
	@mkdir -p build
	@sed -e 's|        if ( (uint32_t) ( (int32_t) key - 1 ) >= (uint32_t) kSlots )|        if ( key == E::None ) /* SABOTAGED: the None-only compare again */|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-none-only.gotext
	@grep -q SABOTAGED build/cpptable-none-only.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-none-only.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-none-only-overlay.json
	@go build -overlay=build/cpptable-none-only-overlay.json -o build/schema-none-only ./cmd/schema
	@rm -rf build/tables-none-only && mkdir -p build/tables-none-only
	@./build/schema-none-only generate --lang cpp --out build/tables-none-only tables/examples
	@$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -Ibuild/tables-none-only \
		test/tables/keyed_max_ndebug_main.cpp -o build/schema_test_keyed_max_none_only
	@if ./build/schema_test_keyed_max_none_only > build/keyed-max-none-only.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a None-only compare left the past-Max gate GREEN"; exit 1; \
	fi
	@grep -q "the refusal was compiled out" build/keyed-max-none-only.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the refusal"; cat build/keyed-max-none-only.log; exit 1; }
	@echo "negative control: the None-only compare turns the past-Max refusal gate red"

# The NEGATIVE CONTROL for the SHIFT itself (docs/SPEC-TABLES.md §2.4, owner ruling
# 2026-09-03). The storage holds E.Max slots with the key k at index k-1 and
# nothing for None. Putting the None slot BACK — E.Max + 1 slots, no shift —
# is the exact edit the ruling reversed, and it must not be able to pass
# quietly: the compiler computes the layout from E.Max and both backends assert
# it (§19.3), so a storage type one element wider than the compiler says
# must fail to COMPILE, and the corpus's own sizeof checks must fail too.
#
# The whole point is that this is caught by the GATES already in the tree — the
# block projection's static_asserts and the tables suite — rather than by a
# fixture written to notice it.
.PHONY: tables-keyed-shift-negative-control
tables-keyed-shift-negative-control: bin/schema
	@mkdir -p build
	@sed -e 's|static constexpr int32_t kSlots = (int32_t) E::Max;|static constexpr int32_t kSlots = (int32_t) E::Max + 1; // SABOTAGED: None'"'"'s slot back|' \
	     -e 's|return slots\[ (int32_t) key - 1 \];|return slots[ (int32_t) key ]; // SABOTAGED: no shift|' \
	     -e 's|Entry operator\*() const { return Entry{ (E) ( index + 1 ), slots\[index\] }; }|Entry operator*() const { return Entry{ (E) index, slots[index] }; } // SABOTAGED|' \
	     -e 's|ConstEntry operator\*() const { return ConstEntry{ (E) ( index + 1 ), slots\[index\] }; }|ConstEntry operator*() const { return ConstEntry{ (E) index, slots[index] }; } // SABOTAGED|' \
	     -e 's|Iterator begin() { return Iterator{ slots, 0 }; }|Iterator begin() { return Iterator{ slots, 1 }; } // SABOTAGED|' \
	     -e 's|ConstIterator begin() const { return ConstIterator{ slots, 0 }; }|ConstIterator begin() const { return ConstIterator{ slots, 1 }; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-none-slot.gotext
	@test $$(grep -c SABOTAGED build/cpptable-none-slot.gotext) -eq 7 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched $$(grep -c SABOTAGED build/cpptable-none-slot.gotext) of 7 sites"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-none-slot.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-none-slot-overlay.json
	@go build -overlay=build/cpptable-none-slot-overlay.json -o build/schema-none-slot ./cmd/schema
	@rm -rf build/tables-none-slot && mkdir -p build/tables-none-slot
	$(call tables_generate,./build/schema-none-slot,build/tables-none-slot)
	@# the LAYOUT GATE: the block corpus asserts the compiler's own offsets and
	@# sizes, and a keyed array one element wider moves them
	@if $(CXX) $(BLOCK_CXXFLAGS) -Ibuild/tables-none-slot/block -Ibuild/tables-none-slot/blockhome \
			-fsyntax-only build/tables-none-slot/block/PaddedBlock.cpp > build/tables-none-slot-layout.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the None slot back left the BLOCK LAYOUT GATE green"; exit 1; \
	fi
	@grep -q "projection" build/tables-none-slot-layout.log || \
		{ echo "NEGATIVE CONTROL FAILED: the block build went red, but not on a layout assert"; cat build/tables-none-slot-layout.log; exit 1; }
	@# and the SIZEOF assertions in the tables suite itself
	@if $(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-none-slot) \
			test/tables/main.cpp $$(ls build/tables-none-slot/*/*Table.cpp) -o build/schema_test_tables_none_slot \
			> build/tables-none-slot-build.log 2>&1 && \
		./build/schema_test_tables_none_slot > build/tables-none-slot.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the None slot back left the tables suite GREEN"; exit 1; \
	fi
	@echo "negative control: the None slot back turns the BLOCK LAYOUT GATE and the tables suite red"

# THE CLAMP-AT-STORAGE-LIMITS GATE (docs/SPEC-TABLES.md §4). A read clamps a
# value to the field's declared range; a bound sitting ON its storage type's
# own limit describes a check with no false case, and the emitter does not
# write it — the same test that already drops a bits(N) width clamp when N is
# the storage width.
#
# That rule is a BUILD rule and not only a shape: a tautological comparison is
# an ERROR under the repo's own flags, and the two compilers split the work.
# gcc reds the unsigned half — `decoded_v < 0ull`, "comparison of unsigned
# expression in '< 0' is always false", -Wtype-limits, which -Wextra implies
# and this names anyway. clang reds the signed half — `decoded_v < -128`,
# -Wtautological-type-limit-compare. So a tree carrying such a field can read
# clean on one platform and fail on the other, which is how it stayed
# invisible until the tables bench corpus met it.
#
# tables/examples/Ranges.schema declares every integer width four ways — both
# bounds at the limits, each limit alone, and one value off each end — so all
# four emission shapes are compiled here under both diagnostics.
CLAMP_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wtype-limits -ffp-contract=off

.PHONY: tables-clamp-limits
tables-clamp-limits: build/tables-generated/.stamp
	@mkdir -p build
	@printf '#include "RangesTable.h"\n' > build/clamp-limits-tu.cpp
	$(CXX) $(CLAMP_CXXFLAGS) -Ibuild/tables-generated/examples -fsyntax-only build/clamp-limits-tu.cpp
	@echo "clamp-at-storage-limits gate: the bounded corpus carries no comparison that cannot fire"

# The negative control puts the PRE-FIX emission back through `go build
# -overlay`, so no tracked file is written: the two comparisons that decide
# which ends are live become non-strict, which is always true given the
# checker already refuses a bound outside its storage — every declared end
# written, storage limit or not.
.PHONY: tables-clamp-limits-negative-control
tables-clamp-limits-negative-control: bin/schema
	@mkdir -p build
	@sed 's|return rlo.Cmp(lo) > 0, rhi.Cmp(hi) < 0|return rlo.Cmp(lo) >= 0, rhi.Cmp(hi) <= 0 // SABOTAGED: both ends always written|' \
		internal/codegen/cpptable/codecs.go > build/cpptable-always-clamp.gotext
	@grep -q SABOTAGED build/cpptable-always-clamp.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/codecs.go":"%s/build/cpptable-always-clamp.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-always-clamp-overlay.json
	@go build -overlay=build/cpptable-always-clamp-overlay.json -o build/schema-always-clamp ./cmd/schema
	@rm -rf build/clamp-sabotage && mkdir -p build/clamp-sabotage
	@./build/schema-always-clamp generate --lang cpp --out build/clamp-sabotage tables/examples
	@# the rest of the corpus is UNMOVED by the sabotage — no other declared
	@# bound in it sits on a storage limit, which is why the defect survived
	@cmp -s build/clamp-sabotage/TablesTable.h build/tables-generated/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage moved a file it has no business moving"; exit 1; }
	@printf '#include "RangesTable.h"\n' > build/clamp-limits-tu.cpp
	@if $(CXX) $(CLAMP_CXXFLAGS) -Ibuild/clamp-sabotage -fsyntax-only build/clamp-limits-tu.cpp \
			> build/clamp-limits-negative.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the pre-fix emission compiled clean — this compiler diagnoses neither half"; exit 1; \
	fi
	@grep -q "always false" build/clamp-limits-negative.log || \
		{ echo "NEGATIVE CONTROL FAILED: the build went red, but not on a comparison that cannot fire"; \
		  cat build/clamp-limits-negative.log; exit 1; }
	@echo "negative control: the storage-limit clamps back turn the build red on $$(grep -c 'always false' build/clamp-limits-negative.log) comparisons that cannot fire"

# THE ZERO-RANGE GATE and its NEGATIVE CONTROL (SPEC §4.6). A field whose
# declared range excludes zero and declares no default is born outside its own
# range: zero initialization is the language's rule, so the fresh value is one
# the wire cannot carry. The checker refuses that shape; the refusal's corpus
# case lives in the break-the-language suite.
#
# The control shows the refusal is load-bearing. It takes the gate back out
# through `go build -overlay` (no tracked file is written), generates the
# refused unit, and compiles it against NDEBUG — the shipping configuration,
# where the write-side range assert is gone and the defect is silent. A
# freshly constructed value must fail to survive its own wire.
.PHONY: check-zero-range-negative-control
check-zero-range-negative-control: bin/schema test/zero_range_negative_main.cpp
	@mkdir -p build/zero-range
	@printf 'package rangezero\n\ntype Probe\n{\n    x    uint8 | min = 1, max = 255\n    tail uint8\n}\n' > build/zero-range/RangeZero.schema
	@if ./bin/schema check build/zero-range > build/zero-range-check.log 2>&1; then \
		echo "GATE FAILED: the checker accepted a [1, 255] field with no declared default"; exit 1; \
	fi
	@grep -q "excludes zero" build/zero-range-check.log || \
		{ echo "GATE FAILED: it was refused, but not for excluding zero"; cat build/zero-range-check.log; exit 1; }
	@sed 's|c.requireDefaultInRange(f, out)|// SABOTAGED: the zero-range gate removed|' \
		internal/check/check.go > build/check-no-zero-gate.gotext
	@grep -q SABOTAGED build/check-no-zero-gate.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/check/check.go":"%s/build/check-no-zero-gate.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/check-no-zero-gate-overlay.json
	@go build -overlay=build/check-no-zero-gate-overlay.json -o build/schema-no-zero-gate ./cmd/schema
	@rm -rf build/zero-range-sabotage && mkdir -p build/zero-range-sabotage
	@./build/schema-no-zero-gate generate --lang cpp --out build/zero-range-sabotage build/zero-range
	@grep -q 'uint8_t x = 0; // wire \[1, 255\]' build/zero-range-sabotage/RangeZero.h || \
		{ echo "NEGATIVE CONTROL FAILED: the gateless compiler did not emit the out-of-range initializer"; exit 1; }
	$(CXX) -std=c++17 -Wall -Wextra -Werror -ffp-contract=off -DNDEBUG \
		-I$(SERIALIZE) -Ibuild/zero-range-sabotage \
		test/zero_range_negative_main.cpp -o build/schema_test_zero_range_negative
	./build/schema_test_zero_range_negative

# THE ENUM-BOUND GATE and its NEGATIVE CONTROL (docs/SPEC-TABLES.md §2.4,
# §11, schema#605). A POSITIONAL ARRAY WHOSE BOUND FOLDS FROM AN ENUM is
# refused in a table body and a union arm, on the bound's PROVENANCE rather
# than its spelling: `[E.Max]T`, `[E.Count]T` and `[N]T` under a `const N`
# that folds from either are one bound however it is spelled.
#
# The gate runs the compiler over test/tables/enumbound, one unit a shape, and
# reads three answers back. Every refused shape must be refused and its
# diagnostic must name the field or the arm, the enum, the constant where the
# bound reaches the enum through one, and `[E]T` as the fix. The THREE
# POSITIVE CONTROLS must compile: the packet wire, whose `[E.Max]T` is a plain
# array the connect gate covers; a bound that folds from no enum, which is
# refused for reaching an enum and never for being positional; and the
# `type`-held case, which is schema#606's ruling and not this refusal's.
#
# The NEGATIVE CONTROL is §2.4's own: it REMOVES THE CONSTANT FOLD FROM THE
# BOUND CHECK through `go build -overlay` (no tracked file is written), and
# every row whose bound reaches its enum through a constant must then compile
# clean. It is targeted rather than blanket, so the two DIRECT spellings must
# stay refused under the same sabotage: a control that turned the whole rule
# off would go red for a reason that says nothing about the fold.
#
# An enum has no Count (SPEC §4.2): E.Count is refused as an expression, so
# the corpus shapes that spell E.Count stop at that refusal and their rows pin
# it, apart from the provenance walk. The negative control therefore runs on
# the shapes that fold through Max alone.
.PHONY: check-enum-bound-negative-control
check-enum-bound-negative-control: bin/schema
	@mkdir -p build/enum-bound
	@set -e; for u in BodyMax:ShipType.Max BodyConstMax:SlotCount \
		BodyFolded:SlotCount; do \
		unit=$${u%%:*}; bound=$${u#*:}; \
		if ./bin/schema check test/tables/enumbound/$$unit.schema > build/enum-bound/$$unit.log 2>&1; then \
			echo "GATE FAILED: $$unit.schema compiled in a table body"; exit 1; \
		fi; \
		grep -q "\[$$bound\]int32 is refused in a table body" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema was refused, but not as a table body's own bound"; cat build/enum-bound/$$unit.log; exit 1; }; \
		grep -q "spell it \[ShipType\]int32" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema names no fix"; cat build/enum-bound/$$unit.log; exit 1; }; \
	done
	@set -e; for unit in BodyCount BodyConstCount; do \
		if ./bin/schema check test/tables/enumbound/$$unit.schema > build/enum-bound/$$unit.log 2>&1; then \
			echo "GATE FAILED: $$unit.schema compiled — an enum has no Count"; exit 1; \
		fi; \
		grep -q "enum ShipType has no Count" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema was refused, but not as an enum's own Count"; cat build/enum-bound/$$unit.log; exit 1; }; \
		grep -q "write ShipType.Max" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema names no fix"; cat build/enum-bound/$$unit.log; exit 1; }; \
	done
	@set -e; for u in ArmMax:ShipType.Max ArmConstMax:SlotCount; do \
		unit=$${u%%:*}; bound=$${u#*:}; \
		if ./bin/schema check test/tables/enumbound/$$unit.schema > build/enum-bound/$$unit.log 2>&1; then \
			echo "GATE FAILED: $$unit.schema compiled in a union arm"; exit 1; \
		fi; \
		grep -q "union Payload: arm ships: \[$$bound\]int32 is refused in a union arm" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema was refused, but not at the arm"; cat build/enum-bound/$$unit.log; exit 1; }; \
		grep -q "table Fleet's field payload reaches Payload" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema names no table reaching the union"; cat build/enum-bound/$$unit.log; exit 1; }; \
	done
	@set -e; for unit in ArmCount ArmConstCount ArmFolded; do \
		if ./bin/schema check test/tables/enumbound/$$unit.schema > build/enum-bound/$$unit.log 2>&1; then \
			echo "GATE FAILED: $$unit.schema compiled — an enum has no Count"; exit 1; \
		fi; \
		grep -q "enum ShipType has no Count" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema was refused, but not as an enum's own Count"; cat build/enum-bound/$$unit.log; exit 1; }; \
		grep -q "write ShipType.Max" build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema names no fix"; cat build/enum-bound/$$unit.log; exit 1; }; \
	done
	@set -e; for unit in BodyConstMax BodyFolded ArmConstMax; do \
		grep -q "the bound SlotCount folds from ShipType\." build/enum-bound/$$unit.log || \
			{ echo "GATE FAILED: $$unit.schema names no constant, and the constant is all a reader can see"; cat build/enum-bound/$$unit.log; exit 1; }; \
	done
	@set -e; for unit in ControlPacket ControlPlain ControlTypeHeld; do \
		./bin/schema check test/tables/enumbound/$$unit.schema > build/enum-bound/$$unit.log 2>&1 || \
			{ echo "POSITIVE CONTROL FAILED: $$unit.schema did not compile"; cat build/enum-bound/$$unit.log; exit 1; }; \
	done
	@echo "gate: five shapes refused on the bound's provenance, five on E.Count, three controls compile"
	@sed 's|if entry == nil \|\| entry.decl == nil \|\| visiting\[e.Name\] {|if true { // SABOTAGED: the constant fold removed from the bound check|' \
		internal/check/tablekeyed.go > build/tablekeyed-no-fold.gotext
	@grep -q SABOTAGED build/tablekeyed-no-fold.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; }
	@printf '{"Replace":{"%s/internal/check/tablekeyed.go":"%s/build/tablekeyed-no-fold.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/enum-bound-no-fold-overlay.json
	@go build -overlay=build/enum-bound-no-fold-overlay.json -o build/schema-no-enum-bound-fold ./cmd/schema
	@set -e; for unit in BodyConstMax BodyFolded ArmConstMax; do \
		./build/schema-no-enum-bound-fold check test/tables/enumbound/$$unit.schema > build/enum-bound/$$unit-nofold.log 2>&1 || \
			{ echo "NEGATIVE CONTROL FAILED: $$unit.schema was still refused without the constant fold, so the fold is not what refuses it"; cat build/enum-bound/$$unit-nofold.log; exit 1; }; \
	done
	@set -e; for unit in BodyMax ArmMax; do \
		if ./build/schema-no-enum-bound-fold check test/tables/enumbound/$$unit.schema > build/enum-bound/$$unit-nofold.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: $$unit.schema compiled too, so the sabotage removed the whole rule and not the fold"; exit 1; \
		fi; \
	done
	@echo "negative control: without the constant fold, three folded rows compile clean and the two direct spellings stay refused"

# THE WSTRING REFUSAL'S NEGATIVE CONTROL (SPEC §4.12). `wstring` is a reserved
# word and every spelling of it is refused by name, naming `string` as the fix
# (TestWStringIsRefusedByName). Take the bounded spelling's diagnostic out of
# the parser and the test must go red: `wstring(N)` would otherwise read as a
# silent `string(N)`.
.PHONY: check-wstring-refusal-negative-control
check-wstring-refusal-negative-control:
	@mkdir -p build/wstring-refusal-negative
	@grep -q 'p.errf(t.Pos, WStringRefusal, "string(N)")' internal/parser/parser.go || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage anchor is gone from internal/parser/parser.go"; exit 1; }
	@sed 's|p.errf(t.Pos, WStringRefusal, "string(N)")|_ = WStringRefusal // SABOTAGED: the bounded spelling is not refused|' \
		internal/parser/parser.go > build/wstring-refusal-negative/parser.gotext
	@printf '{"Replace":{"%s/internal/parser/parser.go":"%s/build/wstring-refusal-negative/parser.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/wstring-refusal-negative/overlay.json
	@if go test -count=1 -overlay=build/wstring-refusal-negative/overlay.json ./internal/check -run '^TestWStringIsRefusedByName$$' > build/wstring-refusal-negative/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: TestWStringIsRefusedByName stayed green with the wstring(N) refusal removed"; cat build/wstring-refusal-negative/log; exit 1; \
	fi
	@grep -q 'wstring' build/wstring-refusal-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the test went red, but not on a wstring spelling"; cat build/wstring-refusal-negative/log; exit 1; }
	@echo "negative control: without the parser's wstring(N) diagnostic, TestWStringIsRefusedByName goes red"

# THE VARIANT-ORDER NEGATIVE CONTROL (SPEC §3.1, issue #462). An enum value
# rides as its declaration ordinal and a flags variant as its bit position, so
# the projection carries both declarations' variant names in declaration order:
# without them a reorder is invisible and two builds either side of an
# alphabetized enum hold ONE id while every ordinal means something else.
#
# The control takes the variant lists back out of a COPY of the rendering
# through `go test -overlay` — no tracked file is written — and the reorder
# gate must go RED, on that surface and NOT on the union's, whose order rides
# in its payload types and must stay green under the same sabotage.
.PHONY: projection-variant-order-negative-control
projection-variant-order-negative-control:
	@mkdir -p build
	@sed -e 's|fmt.Fprintf(&b, "  variant %d name=.*, i+1, v)$$|_, _ = i, v // SABOTAGED: the enum variant list removed|' \
		-e 's|fmt.Fprintf(&b, "  bit %d name=.*|_, _ = i, v // SABOTAGED: the flags variant list removed|' \
		ir/projection.go > build/projection-no-variants.gotext
	@[ "$$(grep -c SABOTAGED build/projection-no-variants.gotext)" = "2" ] || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove both variant lists"; exit 1; }
	@printf '{"Replace":{"%s/ir/projection.go":"%s/build/projection-no-variants.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/projection-no-variants-overlay.json
	@if go test -count=1 -overlay=build/projection-no-variants-overlay.json \
		./internal/check -run TestIdMovesUnderVariantOrder > build/projection-no-variants.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the reorder gate passed with the variant lists gone"; \
		cat build/projection-no-variants.log; exit 1; \
	fi
	@grep -q "an enum reordered did NOT move the protocol id" build/projection-no-variants.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the enum reorder"; \
		  cat build/projection-no-variants.log; exit 1; }
	@grep -q "a flags declaration reordered did NOT move the protocol id" build/projection-no-variants.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the flags reorder"; \
		  cat build/projection-no-variants.log; exit 1; }
	@go test -count=1 -overlay=build/projection-no-variants-overlay.json \
		./internal/check -run 'TestUnionId' > build/projection-no-variants-union.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage reddened the union gate too — it is not surgical"; \
		  cat build/projection-no-variants-union.log; exit 1; }
	@echo "negative control: without the variant lists the enum and flags reorders go silent, and the union gate stays green"

# THE CODEC LAW NEGATIVE CONTROL (SPEC §3.1, issue #463). The projection's
# second version line is what a compiler change that moves the BYTES under an
# unchanged shape rides on — the 2026-08-15 rounding amendment moved bytes with
# every id unmoved, which is a false match. Take the line out of a COPY of the
# rendering and both law gates must go red, while the variant-order gate,
# which the line has nothing to do with, stays green.
.PHONY: projection-wire-law-negative-control
projection-wire-law-negative-control:
	@mkdir -p build
	@sed -e 's|fmt.Fprintf(&b, "schema-wire-law .*|// SABOTAGED: the codec law line removed|' \
		ir/projection.go > build/projection-no-wire-law.gotext
	@grep -q SABOTAGED build/projection-no-wire-law.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove the codec law line"; exit 1; }
	@printf '{"Replace":{"%s/ir/projection.go":"%s/build/projection-no-wire-law.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/projection-no-wire-law-overlay.json
	@if go test -count=1 -overlay=build/projection-no-wire-law-overlay.json \
		./internal/check -run TestWireLawLineMovesTheId > build/projection-no-wire-law.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the law gate passed with the line gone"; \
		cat build/projection-no-wire-law.log; exit 1; \
	fi
	@grep -q "the projection must open with its rendering version and then its codec law" build/projection-no-wire-law.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the missing law line"; \
		  cat build/projection-no-wire-law.log; exit 1; }
	@if go test -count=1 -overlay=build/projection-no-wire-law-overlay.json \
		./internal/goldens -run TestWireLawBumpMovesEveryId > build/projection-no-wire-law-corpus.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: every corpus id survived the line's removal in silence"; \
		cat build/projection-no-wire-law-corpus.log; exit 1; \
	fi
	@grep -q "does not carry the codec law line" build/projection-no-wire-law-corpus.log || \
		{ echo "NEGATIVE CONTROL FAILED: the corpus gate went red for another reason"; \
		  cat build/projection-no-wire-law-corpus.log; exit 1; }
	@go test -count=1 -overlay=build/projection-no-wire-law-overlay.json \
		./internal/check -run TestIdMovesUnderVariantOrder > build/projection-no-wire-law-variants.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage reddened the variant-order gate too — it is not surgical"; \
		  cat build/projection-no-wire-law-variants.log; exit 1; }
	@echo "negative control: without the codec law line both law gates go red, and the variant-order gate stays green"

# THE UNION ARM-ORDER NEGATIVE CONTROL (SPEC §3.1, §4.8, issue #491). A union's
# arm order rides in its payload types only while the arms DIFFER in type: two
# arms of one type reorder with every projected type unmoved, so the arm names
# project beside them. Take the names out of a COPY of the rendering and the
# same-typed reorder goes silent, while the enum and flags gate — which the arm
# names have nothing to do with — stays green.
.PHONY: projection-union-arm-order-negative-control
projection-union-arm-order-negative-control:
	@mkdir -p build
	@sed -E -e 's|"  variant %d name=%s payload=|"  variant %d payload=|' \
		-e 's|i\+1, v\.WireName\(\), v\.Type\)|i+1, v.Type) // SABOTAGED: the arm names removed|' \
		ir/projection.go > build/projection-no-arm-names.gotext
	@grep -q SABOTAGED build/projection-no-arm-names.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove the arm names"; exit 1; }
	@printf '{"Replace":{"%s/ir/projection.go":"%s/build/projection-no-arm-names.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/projection-no-arm-names-overlay.json
	@if go test -count=1 -overlay=build/projection-no-arm-names-overlay.json \
		./internal/check -run 'TestUnionId' > build/projection-no-arm-names.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the union gates passed with the arm names gone"; \
		cat build/projection-no-arm-names.log; exit 1; \
	fi
	@grep -q "two arms of one payload type reordered and the id did not move" build/projection-no-arm-names.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the same-typed reorder"; \
		  cat build/projection-no-arm-names.log; exit 1; }
	@grep -q "an arm renamed (arm order is spelled in names) did NOT move the protocol id" build/projection-no-arm-names.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the arm rename"; \
		  cat build/projection-no-arm-names.log; exit 1; }
	@go test -count=1 -overlay=build/projection-no-arm-names-overlay.json \
		./internal/check -run TestIdMovesUnderVariantOrder > build/projection-no-arm-names-variants.log 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage reddened the variant-order gate too — it is not surgical"; \
		  cat build/projection-no-arm-names-variants.log; exit 1; }
	@echo "negative control: without the arm names a same-typed union reorder goes silent, and the variant-order gate stays green"

# Deliberately compiled WITHOUT -I$(SERIALIZE): the generated Table headers
# carry no serialize dependency, and this build proves it stays that way.
#
# TABLES_INCLUDES is shared with the sanitized twin below, so the two builds
# can never drift into covering different code.
TABLES_INCLUDES := $(call tables_includes,build/tables-generated)
TABLES_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off -pthread

# The text form's runtime is a generated TRANSLATION UNIT now, not header
# content (docs/SPEC-TABLES.md §16.1): a consumer that calls FromJson/ToJson
# compiles the generated <Base>Table.cpp, and one that never does compiles
# nothing for it. Expanded in the recipe because these are build-time output.
TABLES_JSON_SOURCES = $$(ls build/tables-generated/*/*Table.cpp)

build/schema_test_tables: build/tables-generated/.stamp test/tables/main.cpp test/tables/message_form.h
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# THE C++ STEADY READ-PATH ALLOCATION GATE (docs/PORTING.md I1). The counting
# global operator new in test/tables/main.cpp is live across Load, the indexed
# and iterated reads, Measure and Save on the richest FIXED root; the run
# prints the count and its in-process control plants one allocation and
# requires the instrument to see it. #562's list audit and #615/#679's retain
# counters hold their own paths; `make test` runs this one in the cpp leg's
# table-gate list, beside the other tables-* gates.
.PHONY: tables-cpp-alloc
tables-cpp-alloc: build/schema_test_tables
	@./build/schema_test_tables > build/tables-cpp-alloc.log 2>&1 || { cat build/tables-cpp-alloc.log; exit 1; }
	@grep -q "^tables allocation audit: 0 allocation" build/tables-cpp-alloc.log || \
		{ echo "C++ allocation gate FAILED: the steady read path allocated"; cat build/tables-cpp-alloc.log; exit 1; }
	@echo "C++ allocation gate: Load, the reads, Measure and Save leave the global allocation count at zero"

# The SANITIZED twin (issue #277). The tables leg is where the pointer
# machinery lives — an arena whose Lock() frees it one way, a packed region
# read through self-relative deltas, a cooked file Open validates by walking
# an offset graph, a builder that goes wide across threads — and those are
# lifetime and bounds questions -Werror cannot see. A heap-use-after-free
# lived here with CI green over it, because the assertion it corrupted
# happened to compare equal every time (#276).
#
# -fno-sanitize-recover=all is what makes it a GATE: UBSan's default is to
# print and continue, which exits 0 and proves nothing.
#
# Both builds run, and that is not duplication: ASan replaces the allocator,
# so the two see different addresses and alignments — and this suite refuses
# unaligned bases and misaligned region references by design. Each build
# reaches states the other does not.
build/schema_test_tables_asan: build/tables-generated/.stamp test/tables/main.cpp test/tables/message_form.h
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# ---- the BIG-ENDIAN leg (docs/SPEC-TABLES.md §3 and §19.1) ----------------------
#
# The wire is little-endian and byte-oriented (§3), and a block is produced in
# the byte order of the build that wrote it (§19.1). Both were rules on a page:
# every host this repo builds on is little-endian, so nothing ever read a
# golden the other way round. This leg builds the tables battery for a
# BIG-ENDIAN target and runs it under an emulator, which turns the two rules
# into gates — the goldens a little-endian host wrote are loaded, re-written
# and byte-compared by a big-endian reader, and a block that crosses the byte
# order is proven to refuse rather than to garble.
#
# The toolchain is not a system binary and is not assumed: CI installs an
# exact pinned version (.github/workflows/ci-full.yml) and these two variables name
# what it installed, so the leg runs anywhere the same pair is on PATH.
BE_CXX ?= s390x-linux-gnu-g++
BE_RUN ?= qemu-s390x

# Same flags and includes as the plain leg, shared through the same variables
# for the same reason the sanitized twin shares them (#278): a twin that
# covers different code is not a twin. -static so the runner needs no sysroot
# and the emulator invocation is just the binary.
build/schema_test_tables_be: build/tables-generated/.stamp test/tables/main.cpp test/tables/message_form.h
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/main.cpp $(TABLES_JSON_SOURCES) -o $@

# THE MAP GATE, for a BIG-ENDIAN target (docs/SPEC-TABLES.md §2.8, §3). Its
# wire goldens were written by a LITTLE-ENDIAN build, so a codec that reached
# for host byte order anywhere in the map's framing — the array's L, its N, an
# entry's L, the key's length — would read them wrong and write them back
# differently. The sorted entry array is the same bytes on both hosts because
# the ORDER is over the wire's bytes and never over a machine word.
build/schema_test_maps_be: build/tables-generated/.stamp test/tables/maps_main.cpp
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/maps_main.cpp $(MAPS_SOURCES) -o $@

# and the LIST gate on the same host: a list's element array is the same bytes
# on both hosts because the count and the reference are the region's own scalars
build/schema_test_lists_be: build/tables-generated/.stamp test/tables/lists_main.cpp
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/lists_main.cpp $(LISTS_SOURCES) -o $@

# and the UNION-ARM gate on the same host: the arrays an arm holds are laid by
# the same walk, and a pointer arm's node is placed by the same numbering
build/schema_test_arms_be: build/tables-generated/.stamp test/tables/arms_main.cpp
	@mkdir -p build
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(TABLES_INCLUDES) test/tables/arms_main.cpp $(ARMS_SOURCES) -o $@

# The COOK's read side, for a BIG-ENDIAN target. A cook is produced in the byte
# order of the build it is cooked for (docs/SPEC-TABLES.md §7), so this is where that
# stops being a sentence: the big-endian build opens the big-endian cook
# NATIVELY — magic, header words, deltas and all, with no fix-up pass anywhere —
# and refuses the little-endian one, and this host does the mirror image.
build/schema_test_cook_be: build/tables-generated/.stamp test/tables/cook_main.cpp
	@mkdir -p build
	$(BE_CXX) $(COOK_CXXFLAGS) -static $(COOK_INCLUDES) test/tables/cook_main.cpp -o $@

# The cross-endian BLOCK driver, built BOTH ways: a block is produced in the
# byte order of the build that wrote it, and every other block test in this
# tree runs producer and consumer in one process. This is the one part of
# §19.1 that needs two builds and a file between them.
build/schema_test_block_endian: build/tables-generated/.stamp test/tables/block_endian_main.cpp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) $(BLOCK_INCLUDES) test/tables/block_endian_main.cpp $(BLOCK_SOURCES) -o $@

build/schema_test_block_endian_be: build/tables-generated/.stamp test/tables/block_endian_main.cpp
	@mkdir -p build
	$(BE_CXX) $(BLOCK_CXXFLAGS) -static $(BLOCK_INCLUDES) test/tables/block_endian_main.cpp $(BLOCK_SOURCES) -o $@

# THE LEG IS INDEPENDENT PIECES, AND EACH IS ITS OWN TARGET (issue #684).
# `make tables-big-endian` still runs all of them, in the same order, off the
# same parallel prerequisite build — that is what `make test`, certify.yml and a
# workstation ask for and none of it changes. What the split adds is that CI can
# ask for ONE of them: the pieces share nothing but bin/schema and the generated
# tree, and one job made the run pay their SUM. The CI job is a matrix of one
# row per piece, so the longest piece is the job instead of the sum.
.PHONY: tables-big-endian-tables
tables-big-endian-tables: build/schema_test_tables_be
	$(BE_RUN) ./build/schema_test_tables_be
	@echo "big-endian leg: the wire crosses the byte order"

.PHONY: tables-big-endian-collections
tables-big-endian-collections: build/schema_test_maps_be build/schema_test_lists_be build/schema_test_arms_be build/schema_test_block_endian build/schema_test_block_endian_be build/schema_test_cook build/schema_test_cook_be build/cook-open/.stamp
	$(BE_RUN) ./build/schema_test_maps_be
	$(BE_RUN) ./build/schema_test_lists_be
	$(BE_RUN) ./build/schema_test_arms_be
	@echo "big-endian leg: a map's framing and its sorted entry array cross the byte order with it, a list's element array and the arrays a union arm holds"
	./build/schema_test_block_endian write build/block-host.bin
	$(BE_RUN) ./build/schema_test_block_endian_be write build/block-target.bin
	$(BE_RUN) ./build/schema_test_block_endian_be accept build/block-target.bin
	$(BE_RUN) ./build/schema_test_block_endian_be refuse build/block-host.bin
	./build/schema_test_block_endian accept build/block-host.bin
	./build/schema_test_block_endian refuse build/block-target.bin
	@echo "big-endian leg: a block does not cross the byte order either — the magic refuses it and the prologue's word names the order that wrote it"
	$(BE_RUN) ./build/schema_test_cook_be accept Scene build/cook-open/Scene-be.cook
	$(BE_RUN) ./build/schema_test_cook_be golden Scene build/cook-open/Scene-be.cook
	$(BE_RUN) ./build/schema_test_cook_be refuse Scene build/cook-open/Scene.cook
	./build/schema_test_cook accept Scene build/cook-open/Scene.cook
	./build/schema_test_cook refuse Scene build/cook-open/Scene-be.cook
	@echo "big-endian leg: a cook opens NATIVELY in the order it was cooked for, whole graph and all, and a cook of the other order is refused by the magic"

# The prerequisites stay HERE, on the umbrella, so one `make -j
# tables-big-endian` still builds all nine binaries in parallel and only then
# runs anything. The pieces are submakes because their prerequisites are
# already standing by the time this recipe runs.
.PHONY: tables-big-endian
tables-big-endian: build/schema_test_tables_be build/schema_test_maps_be build/schema_test_lists_be build/schema_test_arms_be build/schema_test_block_endian build/schema_test_block_endian_be build/schema_test_cook build/schema_test_cook_be build/cook-open/.stamp
	$(MAKE) tables-big-endian-tables
	$(MAKE) tables-big-endian-collections
	$(MAKE) tables-cook-endian
	$(MAKE) tables-c-big-endian

# THE COOK IS THE HOST'S BUSINESS IN NEITHER DIRECTION (docs/SPEC-TABLES.md §7).
# The byte order is settled AT COOK TIME for the TARGET build, so what a cook
# holds must depend on `--byte-order` and on nothing else — least of all on the
# order of the machine that ran the tool. Every host this repo builds on is
# little-endian, so that is an assertion on a page until a big-endian host runs
# the same command: a cooker that reached for host order anywhere would have
# produced byte-identical files on every other leg in this file.
#
# Go cross-compiles, so the target binary is one env var and the pinned
# emulator is already installed for the legs above. The gate is BYTE IDENTITY
# ACROSS HOSTS, both orders, over a fixed root and a pointered one: four files
# from this host and four from s390x, compared pairwise.
.PHONY: tables-cook-endian
tables-cook-endian: bin/schema
	@rm -rf build/cook-endian && mkdir -p build/cook-endian
	GOOS=linux GOARCH=s390x go build -o build/cook-endian/schema-be ./cmd/schema
	GOOS=linux GOARCH=s390x go build -o build/cook-endian/cookgen-be ./test/cookgen
	go build -o build/cook-endian/cookgen ./test/cookgen
	./bin/schema pack --root PackConfig --out build/cook-endian/fixed.bin tables/pack/pinned/PackConfig tables/examples
	@for order in little big; do \
		./bin/schema cook --root PackConfig --in build/cook-endian/fixed.bin \
			--out build/cook-endian/host-$$order.cook --byte-order $$order tables/examples || exit 1; \
		$(BE_RUN) ./build/cook-endian/schema-be cook --root PackConfig --in build/cook-endian/fixed.bin \
			--out build/cook-endian/target-$$order.cook --byte-order $$order tables/examples || exit 1; \
		cmp build/cook-endian/host-$$order.cook build/cook-endian/target-$$order.cook || \
			{ echo "FAILED: a $$order-endian cook differs between a little-endian host and a big-endian one"; exit 1; }; \
		./build/cook-endian/cookgen --bytes 65536 --byte-order $$order --out build/cook-endian/host-gen-$$order.cook || exit 1; \
		$(BE_RUN) ./build/cook-endian/cookgen-be --bytes 65536 --byte-order $$order --out build/cook-endian/target-gen-$$order.cook || exit 1; \
		cmp build/cook-endian/host-gen-$$order.cook build/cook-endian/target-gen-$$order.cook || \
			{ echo "FAILED: a $$order-endian synthetic region differs between hosts"; exit 1; }; \
		$(BE_RUN) ./build/cook-endian/schema-be cook-check --root PackConfig \
			build/cook-endian/host-$$order.cook tables/examples || exit 1; \
		./bin/schema cook-check --root PackConfig build/cook-endian/target-$$order.cook tables/examples || exit 1; \
	done
	@echo "big-endian leg: a cook's bytes are the TARGET's order and never the host's, and either host checks either file"

# Its NEGATIVE CONTROL. Put ONE of the wire's byte-order-neutral stores back to
# a host-order copy — put16, which writes every field id, every enum value and
# every union arm id — and the leg must go RED on the big-endian target. The
# same sabotage stays GREEN on this little-endian host, and that pair is the
# whole argument for the leg: this is precisely the defect class no
# little-endian CI can see. The C twin is tables-c-big-endian-negative-control
# (make/c.mk): it sabotages table_writer_finish's #else arm, because this
# control never reaches that arm. The memcpy arm of that function is
# tables-c-little-endian-negative-control, a host soak.
#
# The sabotaged emitter reaches the compiler through `go build -overlay`, so no
# tracked file is ever written to: an interrupt cannot leave a sabotaged
# working tree, and a parallel `make -j` cannot compile the sabotage into
# something else.
.PHONY: tables-big-endian-negative-control
tables-big-endian-negative-control: tables-big-endian
	$(MAKE) tables-big-endian-control-only

# THE CONTROL'S OWN RECIPE IS ITS OWN TARGET, `tables-big-endian-control-only`.
# It shares nothing with the leg above but the tree it is cut from: it builds a
# SABOTAGED compiler through the overlay, generates its OWN corpus into
# build/tables-host-order, and compiles and runs that — none of the leg's nine
# binaries appear in it. `tables-big-endian-negative-control` still runs the leg
# and then the control, which is what `make test`, certify.yml and a workstation
# ask for; CI asks for the control half in a job of its own, beside the job that
# runs the leg, and the pair is the same pair.
.PHONY: tables-big-endian-control-only
tables-big-endian-control-only:
	@mkdir -p build
	@sed 's|void put16( uint16_t v ) { uint8_t b\[2\] = { uint8_t( v ), uint8_t( v >> 8 ) }; raw( b, 2 ); }|void put16( uint16_t v ) { raw( \&v, 2 ); } // SABOTAGED: host order|' \
		internal/codegen/cpptable/cpptable.go > build/cpptable-host-order.gotext
	@cmp -s build/cpptable-host-order.gotext internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpptable-host-order.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpptable-overlay.json
	@go build -overlay=build/cpptable-overlay.json -o build/schema-host-order ./cmd/schema
	@rm -rf build/tables-host-order && mkdir -p build/tables-host-order
	$(call tables_generate,./build/schema-host-order,build/tables-host-order)
	$(CXX) $(TABLES_CXXFLAGS) $(call tables_includes,build/tables-host-order) \
		test/tables/main.cpp $$(ls build/tables-host-order/*/*Table.cpp) -o build/schema_test_tables_host_order
	$(BE_CXX) $(TABLES_CXXFLAGS) -static $(call tables_includes,build/tables-host-order) \
		test/tables/main.cpp $$(ls build/tables-host-order/*/*Table.cpp) -o build/schema_test_tables_host_order_be
	@./build/schema_test_tables_host_order > /dev/null || \
		{ echo "NEGATIVE CONTROL FAILED: a host-order put16 is visible on a little-endian host — this host is not little-endian, or the sabotage is not the one described"; exit 1; }
	@echo "negative control: a host-order put16 leaves the LITTLE-ENDIAN leg green"
	@if $(BE_RUN) ./build/schema_test_tables_host_order_be > build/host-order-be.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a host-order put16 left the BIG-ENDIAN leg green"; exit 1; \
	fi
	@grep -q "table wire golden" build/host-order-be.log || \
		{ echo "NEGATIVE CONTROL FAILED: the big-endian leg went red, but not on a wire golden"; cat build/host-order-be.log; exit 1; }
	@echo "negative control: the same put16 turns the BIG-ENDIAN leg red, on the wire goldens"

# The PACK GOLDEN (docs/SPEC-TABLES.md §17.4, issue #257). `schema pack` carries an
# IR-driven engine in Go — the compiler cannot run the code it emits — so the
# gate that makes the two ONE WIRE is a byte comparison: the bytes pack builds
# from the directory tree at tables/pack/config must equal PackConfigSave of
# the same instance built by hand in C++, and a C++ Load of pack's bytes must
# report nothing at all.
PACK_TREE := $(shell find tables/pack/config tables/pack/root -type f 2>/dev/null)

build/tables-pack.bin: bin/schema tables/examples/Pack.schema $(PACK_TREE)
	@mkdir -p build
	./bin/schema pack --root PackConfig --out $@ tables/pack/config tables/examples

build/tables-pack-root.bin: bin/schema $(PACK_TREE)
	@mkdir -p build
	./bin/schema pack --root RootConfig --out $@ tables/pack/root tables/examples

# PACK_INCLUDES is shared with the sanitized twins below, so a build and its
# twin can never drift into covering different code (#278's rule).
PACK_INCLUDES := -Ibuild/tables-generated/examples -Ibuild/tables-generated/pointers -Ibuild/tables-generated/scalars -Ibuild/tables-generated/messages -Ibuild/tables-generated/stream -Ibuild/tables-generated/blobs -Itest/tables -I$(SERIALIZE)
# these drivers CALL the text form, so they compile the generated translation
# unit that holds it (docs/SPEC-TABLES.md §16.1) — the same rule any consumer follows
PACK_JSON_SOURCES = $$(ls build/tables-generated/examples/*Table.cpp build/tables-generated/pointers/*Table.cpp build/tables-generated/scalars/*Table.cpp build/tables-generated/messages/*Table.cpp build/tables-generated/stream/*Table.cpp build/tables-generated/blobs/*Table.cpp)
PACK_CXXFLAGS := -std=c++17 -Wall -Wextra -Werror -Wshadow -ffp-contract=off
PACK_SANITIZE := -fsanitize=address,undefined -fno-sanitize-recover=all -fno-omit-frame-pointer -g

build/schema_test_pack: build/tables-generated/.stamp test/tables/pack_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_INCLUDES) test/tables/pack_main.cpp $(PACK_JSON_SOURCES) -o $@

# The SANITIZED twins (#278, applied to this PR's drivers). Both read file
# sizes off disk and size their own buffers from them, and both compare
# byte ranges whose lengths came out of a file or a manifest — bounds and
# lifetime questions -Werror cannot see, and exactly the class #278 stood the
# tables leg up against. -fno-sanitize-recover=all is what makes it a GATE.
build/schema_test_pack_asan: build/tables-generated/.stamp test/tables/pack_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_SANITIZE) $(PACK_INCLUDES) test/tables/pack_main.cpp $(PACK_JSON_SOURCES) -o $@

# §17.1's THIRD golden needs the engine's own text of each root, so unpack
# writes the one-file form beside the bins and the driver compares ToJson to it.
build/pack-text/.stamp: bin/schema build/tables-pack.bin build/tables-pack-root.bin
	@rm -rf build/pack-text
	@mkdir -p build/pack-text
	./bin/schema unpack --one-file --root PackConfig --in build/tables-pack.bin build/pack-text tables/examples
	./bin/schema unpack --one-file --root RootConfig --in build/tables-pack-root.bin build/pack-text tables/examples
	@touch $@

.PHONY: tables-pack
tables-pack: build/schema_test_pack build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin build/pack-text/.stamp
	./build/schema_test_pack build/tables-pack.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json
	./build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json

# The HOSTILE-VALUE gate (docs/SPEC-TABLES.md §16.2, §16.3, §17.5). One tree per rule
# the text form states — malformed number tokens, a value past a bits(N) width,
# a lone surrogate, `null` on a `?T`, a "None" key, duplicate keys — packed by
# the Go engine and then READ BY THE GENERATED BACKEND. The manifest says which
# trees refuse and what report the rest produce; the backend half asserts the
# invariant the report is a promise about, that bytes the engine called clean
# load clean and re-save byte-identically. The Go half lives in
# internal/tablepack's tests and reads the same manifest.
#
# THE CORPUS AND ITS MANIFEST LIVE IN THE CONFORMANCE HARNESS's DATA
# (testdata/conformance/tables): the battery was always data, so it moved there
# whole rather than keeping a registry of its own, and the harness's
# `json-hostile` surface reads the same rows this gate does. One corpus, one set
# of expectations, two gates asking different things of it.
HOSTILE_MANIFEST := testdata/conformance/tables/MANIFEST.txt
HOSTILE_TREES := $(shell find testdata/conformance/tables/json-hostile -type f 2>/dev/null)

build/hostile-values/.stamp: bin/schema $(HOSTILE_MANIFEST) $(HOSTILE_TREES)
	@rm -rf build/hostile-values
	@mkdir -p build/hostile-values
	@grep '^json-hostile ' $(HOSTILE_MANIFEST) | awk '$$6 != "refused"' | \
	while read -r kind name unit root tree verdict; do \
		./bin/schema pack --root $$root --out build/hostile-values/$$name.bin --tolerate \
			$$tree $$(awk -v u=$$unit '$$1 == "unit" && $$2 == u { for ( i = 3; i <= NF; i++ ) printf "%s ", $$i }' $(HOSTILE_MANIFEST)) || exit 1; \
	done
	@touch $@

build/schema_test_hostile: build/tables-generated/.stamp test/tables/hostile_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_INCLUDES) -Ibuild/tables-generated/maps -Ibuild/tables-generated/lists test/tables/hostile_main.cpp $(PACK_JSON_SOURCES) build/tables-generated/maps/TextTable.cpp $(LISTS_SOURCES) -o $@

build/schema_test_hostile_asan: build/tables-generated/.stamp test/tables/hostile_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(PACK_SANITIZE) $(PACK_INCLUDES) -Ibuild/tables-generated/maps -Ibuild/tables-generated/lists test/tables/hostile_main.cpp $(PACK_JSON_SOURCES) build/tables-generated/maps/TextTable.cpp $(LISTS_SOURCES) -o $@

.PHONY: tables-hostile-values
tables-hostile-values: build/schema_test_hostile build/schema_test_hostile_asan build/hostile-values/.stamp
	./build/schema_test_hostile $(HOSTILE_MANIFEST) build/hostile-values
	./build/schema_test_hostile_asan $(HOSTILE_MANIFEST) build/hostile-values

# Its NEGATIVE CONTROL: relax ONE rule of the number grammar — accept a leading
# `+`, which RFC 8259 does not — and the gate must go red, because a tree the
# manifest says is REFUSED starts packing. Same overlay mechanism as the wire
# negative control: no tracked file is ever written to.
.PHONY: tables-hostile-negative-control
tables-hostile-negative-control: tables-hostile-values
	@mkdir -p build
	@sed "s/in.text\[in.pos\] == '-' {/in.text[in.pos] == '-' || in.text[in.pos] == '+' { \/\/ SABOTAGED/" \
		internal/tabletext/read.go > build/read-sabotaged.gotext
	@cmp -s build/read-sabotaged.gotext internal/tabletext/read.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tabletext/read.go":"%s/build/read-sabotaged.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/read-overlay.json
	@go build -overlay=build/read-overlay.json -o build/schema-loose-numbers ./cmd/schema
	@if ./build/schema-loose-numbers pack --root RootConfig --out build/loose.bin --tolerate \
		testdata/conformance/tables/json-hostile/num-leading-plus tables/examples > /dev/null 2>&1; then \
		echo "pack hostile-value negative control: a leading + packs once the grammar is relaxed"; \
	else \
		echo "NEGATIVE CONTROL FAILED: relaxing the number grammar left num-leading-plus refused"; exit 1; \
	fi
	@./bin/schema pack --root RootConfig --out build/loose.bin --tolerate \
		testdata/conformance/tables/json-hostile/num-leading-plus tables/examples > /dev/null 2>&1 && \
		{ echo "NEGATIVE CONTROL FAILED: the real engine accepts a leading + too"; exit 1; } || true

# The NEGATIVE CONTROL for that golden: break ONE framing rule in the Go
# encoder — elide a present `?T`, which §2.3 forbids — and the golden must go
# red. A gate that cannot be made to fail is not a gate, so this target runs the
# POSITIVE one first: a red golden must not be able to pass as a successful
# sabotage.
#
# The sabotaged source lives under build/ and reaches the compiler through
# `go build -overlay`, so no tracked file is ever written to: an interrupt in
# the middle of this target cannot leave a sabotaged working tree, and a
# parallel `make -j` cannot compile the sabotage into something else.
.PHONY: tables-pack-negative-control
tables-pack-negative-control: tables-pack
	@mkdir -p build
	@sed 's/if !fv\.Present {/if true { \/\/ SABOTAGED: a present ?T elides/' \
		internal/tablewire/encode.go > build/encode-sabotaged.gotext
	@cmp -s build/encode-sabotaged.gotext internal/tablewire/encode.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablewire/encode.go":"%s/build/encode-sabotaged.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/sabotage-overlay.json
	@go build -overlay=build/sabotage-overlay.json -o build/schema-sabotaged ./cmd/schema
	@./build/schema-sabotaged pack --root PackConfig --out build/tables-pack-sabotaged.bin \
		tables/pack/config tables/examples
	@if ./build/schema_test_pack build/tables-pack-sabotaged.bin build/tables-pack-root.bin \
		build/pack-text/PackConfig.json build/pack-text/RootConfig.json > /dev/null 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: eliding a present ?T left the golden green"; exit 1; \
	fi
	@echo "pack negative control: eliding a present ?T turns the golden red"

# THE TEXT DIFFERENTIAL AGAINST A THIRD IMPLEMENTATION (docs/PORTING.md I13,
# docs/SPEC-TABLES.md §17.1's third golden, schema#419). `tables-pack` pins TWO
# hand-built instances; this target makes the instance RANDOM — N of them, on a
# seed — so the differential reaches the float ties a pinned golden never does.
# The driver draws each float32 leaf of the known-good PackConfig corpus wire
# from one of two families (random bits, and ±(m+k/2^e) whose decimal expansion
# ends in 5) and emits (wire, text); then the compiler's own Go engine
# (`schema unpack`, written from the spec and from neither backend) reads the
# same wire and writes its text, and the two texts are byte-compared, then the
# other direction. -ffp-contract=off is load-bearing: a contracted multiply-add
# would change a value this row is measuring the spelling of.
#
# JSON_DIFF_N / JSON_DIFF_SEED are this target's own knobs, not the fuzzers'
# global N/SEED (sized for a 100000-mutant run); both overridable on the line.
JSON_DIFF_N ?= 40
JSON_DIFF_SEED ?= 419
# the generated tree the driver compiles against; the negative control below
# points these same variables at a sabotaged copy, so a build and its twin can
# never drift into covering different code
JSON_DIFF_GEN ?= build/tables-generated
JSON_DIFF_INCLUDES := -I$(JSON_DIFF_GEN)/examples -Itest/tables -I$(SERIALIZE)
# Pack.schema names no other file's types, so one .h/.cpp pair is the whole
# dependency (internal/codegen/cpptable/cpptable.go)
JSON_DIFF_SOURCES := $(JSON_DIFF_GEN)/examples/PackTable.cpp

build/schema_test_json_differential: build/tables-generated/.stamp test/tables/json_differential_main.cpp
	@mkdir -p build
	$(CXX) $(PACK_CXXFLAGS) $(JSON_DIFF_INCLUDES) test/tables/json_differential_main.cpp $(JSON_DIFF_SOURCES) -o $@

.PHONY: tables-json-differential
tables-json-differential: build/tables-generated/.stamp build/schema_test_json_differential build/tables-pack.bin bin/schema
	@rm -rf build/json-diff && mkdir -p build/json-diff
	./build/schema_test_json_differential write build/tables-pack.bin $(JSON_DIFF_SEED) $(JSON_DIFF_N) build/json-diff
	@i=0; while [ $$i -lt $(JSON_DIFF_N) ]; do \
		./bin/schema unpack --one-file --root PackConfig --in build/json-diff/$$i.wire build/json-diff/$$i-text tables/examples || exit 1; \
		cmp build/json-diff/$$i.cpp.json build/json-diff/$$i-text/PackConfig.json || exit 1; \
		i=$$((i+1)); \
	done
	./build/schema_test_json_differential read $(JSON_DIFF_N) build/json-diff
	@i=0; while [ $$i -lt $(JSON_DIFF_N) ]; do \
		./bin/schema pack --root PackConfig --out build/json-diff/$$i.back.wire build/json-diff/$$i-text tables/examples || exit 1; \
		cmp build/json-diff/$$i.wire build/json-diff/$$i.back.wire || exit 1; \
		i=$$((i+1)); \
	done
	@echo "json differential: compared $(JSON_DIFF_N) instances — direction one (unpack vs ToJson), direction two (FromJson vs Save), and the engine's back direction (pack) — on seed $(JSON_DIFF_SEED)"

# ITS NEGATIVE CONTROL: break ONE rule of the C++ float writer — widen the
# round-trip check that decides an f32's spelling from strtof to strtod, so
# "shortest that reads back at the field's own width" becomes "shortest that
# reads back as a double", and every non-trivial float32 renders at nine digits
# where the engine writes the short form. A differential that does not move when
# one side's rounding rule is broken is worth nothing, so the target runs the
# POSITIVE one first: a red positive gate can never pass as a successful
# sabotage. The sabotaged source lives under build/ and reaches the compiler
# through `go build -overlay`, so no tracked file is ever written to.
.PHONY: tables-json-differential-negative-control
tables-json-differential-negative-control: tables-json-differential
	@rm -rf build/json-diff-nc && mkdir -p build/json-diff-nc
	@sed 's|if ( (double) strtof( text, NULL ) == value ) { break; }|if ( (double) strtod( text, NULL ) == value ) { break; } // SABOTAGED: an f64 round-trip decides an f32 spelling|' \
		internal/codegen/cpptable/json.go > build/json-diff-nc/json.go.txt
	@cmp -s build/json-diff-nc/json.go.txt internal/codegen/cpptable/json.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@test "$$(grep -c SABOTAGED build/json-diff-nc/json.go.txt)" = "1" || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not land exactly once"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/json.go":"%s/build/json-diff-nc/json.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/json-diff-nc/overlay.json
	@go build -overlay=build/json-diff-nc/overlay.json -o build/json-diff-nc/schema ./cmd/schema
	@./build/json-diff-nc/schema generate --lang cpp --out build/json-diff-nc/examples tables/examples
	@grep -rl SABOTAGED build/json-diff-nc/examples/ || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not reach the generated code"; exit 1; }
	$(CXX) $(PACK_CXXFLAGS) -Ibuild/json-diff-nc/examples -Itest/tables -I$(SERIALIZE) \
		test/tables/json_differential_main.cpp build/json-diff-nc/examples/PackTable.cpp \
		-o build/json-diff-nc/schema_test_json_differential
	@rm -rf build/json-diff-nc/run && mkdir -p build/json-diff-nc/run
	./build/json-diff-nc/schema_test_json_differential write build/tables-pack.bin $(JSON_DIFF_SEED) $(JSON_DIFF_N) build/json-diff-nc/run
	@i=0; while [ $$i -lt $(JSON_DIFF_N) ]; do \
		./bin/schema unpack --one-file --root PackConfig --in build/json-diff-nc/run/$$i.wire build/json-diff-nc/run/$$i-text tables/examples || exit 1; \
		i=$$((i+1)); \
	done
	@if ./build/json-diff-nc/schema_test_json_differential read $(JSON_DIFF_N) build/json-diff-nc/run > build/json-diff-nc/run.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the differential stayed green with strtof widened to strtod"; exit 1; \
	fi
	@grep -q "DIFF instance" build/json-diff-nc/run.log || \
		{ echo "NEGATIVE CONTROL FAILED: the differential went red, but not on a text mismatch"; cat build/json-diff-nc/run.log; exit 1; }
	@echo "json differential negative control: an f64 round-trip deciding an f32 spelling turns the differential red"
	@cat build/json-diff-nc/run.log

build/schema_test_random: generated/cpp/.stamp test/random_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp -Itest test/random_main.cpp -o $@

build/schema_test_ludicrous: generated/cpp/ludicrous/.stamp test/ludicrous_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/cpp/ludicrous test/ludicrous_main.cpp -o $@

# the bench corpus — TWO units sharing one directory (SPEC §3.2: one package
# per unit, so every corpus command below names its unit's file, never the
# directory):
#   bench/corpus/Bench.schema      package bench      — the bench-corpus shapes
#     (BENCH-STANDARD.md §1.3); goldens testdata/wire/bench_*.bin
#   bench/corpus/RealWorld.schema  package realworld  — the §1.7 realistic
#     snapshot (RealPacket); golden testdata/wire/real_packet.bin
# Both are generated into all six languages so the bench shapes have ONE
# definition; the goldens come from the generated C++ (test/bench/main.cpp)
# and every bench runner is §1.5-gated against them. Languages whose module
# layout cannot hold two packages in one directory put the realworld unit in
# its own subdirectory (go, cs) or sibling crate
# (rust — one crate root per unit).
#
# COMPILE gates: generating a unit proves nothing about it compiling — the cs
# realworld unit shipped uncompilable (SerializeFixed had no 8-bit overload,
# issue #80's C# leg was its first consumer) while every gate stayed green.
# The test target builds generated/bench/go and both rust crates directly;
# C# has no unit-local build file, so `cd bench/cs && dotnet build` is the
# compile gate for the realworld unit (the runner is its one consumer). The
# cs Bench unit still has NO compile gate — nothing consumes it.
generated/bench/cpp/.stamp: bin/schema $(SCHEMAS_BENCH)
	./bin/schema generate --lang cpp --out generated/bench/cpp bench/corpus/Bench.schema
	./bin/schema generate --lang cpp --out generated/bench/cpp bench/corpus/RealWorld.schema
	@touch $@

# The TABLES bench corpus (bench/tables/README.md): bench/corpus/BenchTable.schema,
# one representative fixed table mirroring BenchMixed, generated for the
# backends that CARRY tables. It goes into its own directory rather than
# beside the type corpus because every other backend REFUSES a unit that
# declares tables, by name (docs/SPEC-TABLES.md §11) — generating it into
# generated/bench/<lang> would break seven legs' generation the day it landed.
# A port adds its stamp in its make/<lang>.mk, registered in BENCH_TABLES_LEGS,
# beside its bench/tables/<lang>/leg.
generated/bench/tables/cpp/.stamp: bin/schema bench/corpus/BenchTable.schema
	@mkdir -p generated/bench/tables/cpp
	./bin/schema generate --lang cpp --out generated/bench/tables/cpp bench/corpus/BenchTable.schema
	@touch $@

# the C++ producer/verifier of the bench-corpus goldens, and the C twin that
# proves the C emitter compiles under the strict flags AND matches those bytes
build/schema_test_bench: generated/bench/cpp/.stamp test/bench/main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/bench/cpp test/bench/main.cpp -o $@

# the tables bench corpus's producer and oracle (bench/tables/README.md): the
# ONE place that names a field of BenchTable.schema, so no language leg does
build/schema_test_bench_table: generated/bench/tables/cpp/.stamp test/bench/table_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/bench/tables/cpp test/bench/table_main.cpp -o $@

# THE TOOLCHAIN GATE IS THE FIRST PREREQUISITE (issue #599): a pinned toolchain
# that does not resolve stops the chain by name here, before an hour of C++,
# rather than in the middle of a leg with a shell's "command not found".
test: toolchain build/schema_test build/schema_test_guard build/schema_test_tables build/schema_test_block build/schema_test_block_asan build/schema_test_block_fuzz build/schema_test_block_fuzz_asan build/pack-text/.stamp build/schema_test_hostile build/schema_test_hostile_asan build/hostile-values/.stamp build/schema_test_pack build/schema_test_pack_asan build/tables-pack.bin build/tables-pack-root.bin build/schema_test_tables_asan build/schema_test_random build/schema_test_ludicrous build/schema_test_bench build/schema_test_bench_table build/conformance-harness
	# THE TOOLCHAIN GATE's own control (issue #599): the gate above is what
	# stands between a missing toolchain and a green run, so it is held to the
	# same rule as every other gate here. It has to go red on demand, by name.
	$(MAKE) toolchain-negative-control
	./build/schema_test
	./build/schema_test_guard
	$(MAKE) check-zero-range-negative-control
	$(MAKE) check-enum-bound-negative-control
	$(MAKE) check-wstring-refusal-negative-control
	$(MAKE) projection-variant-order-negative-control
	$(MAKE) projection-wire-law-negative-control
	$(MAKE) projection-union-arm-order-negative-control
	./build/schema_test_tables
	./build/schema_test_tables_asan
	# THE ORDINAL SLOT CACHE (docs/SPEC-TABLES.md §3): a save that answers a
	# repeat header out of slot[ordinal] is held to main's own bytes by
	# compiler/tablerefordinal_test.go, and the ONE rule a green pin cannot be
	# read for is truncate taking the slot back — so its control is here.
	$(MAKE) tables-ref-ordinal-negative-control
	$(MAKE) tables-ref-ordinal-shared-negative-control
	# THE ID TABLE'S DISTINCTNESS GATE (docs/SPEC-TABLES.md §3): TableOpen
	# answers "one id in two entries" two ways, and the corpus above takes
	# only one of them, so the gate below builds the other one's tables by
	# hand and holds both against a pairwise reference. Its control removes
	# the probe's comparison and requires the gate to go red.
	$(MAKE) tables-distinctness
	$(MAKE) tables-distinctness-negative-control
	# THE WIRE FUZZER (docs/SPEC-TABLES.md §4.2): the tolerant read on hostile
	# bytes against the engine, plain and sanitized, at a short random pass —
	# the enumerated passes run whole whatever N is — and its two controls at
	# N=0, where the enumerated passes alone turn each one red. The long pass
	# is `make tables-cpp-release`.
	$(MAKE) tables-wire-fuzz N=20000
	$(MAKE) tables-wire-fuzz-negative-control N=0
	# THE RETENTION LEG (docs/SPEC-TABLES.md §6.6): the same mutants through
	# both engines' RETAINING paths, comparing the two retention counters
	# beside the six and the saved bytes beside them, and its own two controls
	# at N=0, one per engine.
	$(MAKE) tables-wire-fuzz-retain N=20000
	$(MAKE) tables-wire-fuzz-retain-negative-control N=0
	# THE MESSAGE FORM (docs/SPEC-TABLES.md §3.3): its rules are refusals and an
	# ORDER, and a green run cannot be read for either, so each control removes
	# one and names the gate that must go red.
	$(MAKE) tables-vocab-schema
	$(MAKE) tables-message-form-negative-control
	$(MAKE) tables-zero-cost
	$(MAKE) tables-zero-cost-negative-control
	# THE ACCESSOR/DESCRIPTOR AGREEMENT (docs/PORTING.md J1, schema#421): the
	# generated accessor and the generated descriptor are two independent
	# derivations of one layout, read both ways on a block and a cook, with a
	# scalar and a pointer-slot control that must each go red.
	$(MAKE) tables-accessor-descriptor-agreement
	$(MAKE) tables-accessor-negative-control
	$(MAKE) tables-slot-negative-control
	$(MAKE) tables-maps
	$(MAKE) tables-maps-measure-refusals
	$(MAKE) tables-json-map-walk
	$(MAKE) tables-json-map-walk-negative-controls
	$(MAKE) tables-maps-negative-controls
	$(MAKE) tables-lists
	# the tool's own cook half for a list, held to the reference byte for byte
	# (schema#380)
	$(MAKE) tables-lists-tool-cook
	$(MAKE) tables-list-measure-refusals
	$(MAKE) tables-json-list-walk
	$(MAKE) tables-json-list-walk-negative-controls
	$(MAKE) tables-lists-negative-controls
	$(MAKE) tables-arms
	$(MAKE) tables-arms-negative-controls
	# A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3): every read and every
	# writer refuses a NaN or an infinity, and -0 rides
	$(MAKE) tables-float-nan
	$(MAKE) tables-float-nan-negative-control
	# RETAIN-UNKNOWN (docs/SPEC-TABLES.md §6.6): the round trip, the five
	# excluded classes a wire can carry to the unknown arm, the two
	# capacities, and the fixed-class refusal, a compile error that has to
	# name itself
	$(MAKE) tables-retain
	$(MAKE) tables-retain-fixed-class-negative-control
	$(MAKE) tables-json-walk
	$(MAKE) tables-json-graph-walk
	$(MAKE) tables-json-negative-control
	$(MAKE) tables-doctags
	$(MAKE) tables-doctags-negative-controls
	$(MAKE) tables-view
	$(MAKE) tables-view-negative-controls
	$(MAKE) tables-view-containment
	$(MAKE) tables-view-containment-negative-control
	$(MAKE) tables-ports-refuse-wide-scalars
	$(MAKE) tables-scalars-block-asserts
	# THE CONFORMANCE HARNESS (test/conformance/README.md): the same corpus as
	# data, one driver per language, and the matrix that says which surfaces a
	# backend has. The reference leg's own negative controls ride beside it;
	# every other leg's ride in that leg's test-<lang> (make/<lang>.mk).
	$(MAKE) conformance
	$(MAKE) conformance-negative-control
	$(MAKE) conformance-negative-control-cpp
	$(MAKE) conformance-negative-control-absent
	$(MAKE) conformance-negative-control-reference-surface
	$(MAKE) conformance-negative-control-block-dump
	$(MAKE) tables-json-keyed-dup-negative-control
	$(MAKE) tables-json-clamp-prefix-negative-control
	$(MAKE) tables-flat-wire
	$(MAKE) tables-flat-wire-negative-control
	$(MAKE) tables-was-negative-control
	$(MAKE) tables-wasrows-negative-control
	$(MAKE) tables-shared-node-negative-control
	$(MAKE) tables-keyed-iteration-negative-control
	$(MAKE) tables-hooks
	$(MAKE) tables-keyed-none-refusal-ndebug
	$(MAKE) tables-keyed-none-refusal-negative-control
	$(MAKE) tables-keyed-max-refusal-ndebug
	$(MAKE) tables-keyed-max-refusal-negative-control
	$(MAKE) tables-keyed-shift-negative-control
	$(MAKE) tables-clamp-limits
	$(MAKE) tables-clamp-limits-negative-control
	$(MAKE) tables-block
	$(MAKE) tables-block-fuzz
	$(MAKE) tables-block-fuzz-extent-negative-control
	$(MAKE) tables-block-fuzz-maximum-negative-control
	$(MAKE) tables-cook-cli
	$(MAKE) tables-cook-scale
	$(MAKE) tables-cook-fuzz-negative-control
	$(MAKE) tables-cook-open
	$(MAKE) tables-cpp-usage
	$(MAKE) tables-cpp-usage-negative-control
	$(MAKE) tables-cook-write
	$(MAKE) tables-cook-write-negative-control
	$(MAKE) tables-cook-write-hooks-negative-control
	$(MAKE) tables-blob-read-hooks-negative-control
	$(MAKE) tables-blob-span-negative-control
	$(MAKE) tables-cook-valued
	$(MAKE) tables-cook-open-lengths-negative-control
	$(MAKE) tables-cook-open-root-negative-control
	$(MAKE) tables-cook-open-walk-negative-control
	$(MAKE) tables-block-zero-cost
	$(MAKE) tables-block-zero-cost-negative-control
	$(MAKE) tables-cpp-names-negative-control
	$(MAKE) tables-block-build-version
	$(MAKE) tables-block-fill-refuser
	$(MAKE) tables-block-fill-refuser-negative-control
	$(MAKE) tables-block-padding-negative-control
	$(MAKE) tables-block-pitch-negative-control
	$(MAKE) tables-block-layout-model-negative-control
	$(MAKE) tables-block-race-negative-control
	$(MAKE) tables-block-home-negative-control
	$(MAKE) tables-runtime-home
	$(MAKE) tables-runtime-home-negative-control
	$(MAKE) tables-cpp-runtime-home
	$(MAKE) tables-cpp-runtime-home-negative-control
	$(MAKE) tables-cpp-runtime-home-bytes-negative-control
	$(MAKE) tables-cpp-alloc
	# the C++ soak (make/cpp.mk): 4096 passes of the count-gated loop, and the
	# control that plants a matched new/delete pair and must turn the count red
	$(MAKE) tables-cpp-soak TABLES_CPP_SOAK_ITERATIONS=4096
	$(MAKE) tables-cpp-soak-negative-control
	$(MAKE) tables-block-inline-array-negative-control
	$(MAKE) tables-pack
	$(MAKE) tables-pack-negative-control
	$(MAKE) tables-json-differential
	$(MAKE) tables-json-differential-negative-control
	$(MAKE) tables-hostile-values
	$(MAKE) tables-hostile-negative-control
	./build/schema_test_random
	./build/schema_test_ludicrous
	./build/schema_test_bench
	# the tables bench corpus's oracle — the generated table unit has no other
	# consumer under `make test`, and a unit that generates but does not compile
	# is issue #80's lesson, so each leg's compile gate rides in its test-<lang>
	./build/schema_test_bench_table
	# EVERY REGISTERED LEG (make/<lang>.mk, TEST_LEGS): its generated trees, its
	# gates and negative controls, its packet and table tests. One sub-make per
	# leg, so a red names the leg. A leg named in SCHEMA_SKIP_LEGS is not run
	# and says so by name (issue #599): a run that measured eight legs and a run
	# that measured four must not read the same.
	@set -e; for leg in $(TEST_LEGS); do \
		case " $(SKIPPED_LEGS) " in \
		*" $${leg#test-} "*) \
			echo "make test SKIPS $$leg: SCHEMA_SKIP_LEGS names the $${leg#test-} leg on purpose"; \
			continue ;; \
		esac; \
		echo "$(MAKE) $$leg"; $(MAKE) $$leg; done
	go test ./...


# ---------------------------------------------------------------------------
# THE MAP GATE (docs/SPEC-TABLES.md §2.8) ----------------------------------
#
# One binary over the `tables/maps` corpus: the builder's five, the sort the
# four writing walks hold, the node extent a region and a cook carry, and every
# reader rule §2.8 states. Its wire goldens are the reference's, pinned like
# every other table golden.
#
# THE SANITIZED TWIN rides beside it for the reason the tables leg's does: the
# map machinery is an arena of segments, an entry array carved from a node's
# own extent and a binary search over mapped bytes, which are lifetime and
# bounds questions -Werror cannot see.

MAPS_SOURCES = $$(ls build/tables-generated/maps/*Table.cpp)

build/schema_test_maps: build/tables-generated/.stamp test/tables/maps_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/maps_main.cpp $(MAPS_SOURCES) -o $@

build/schema_test_maps_asan: build/tables-generated/.stamp test/tables/maps_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-omit-frame-pointer -g \
		$(TABLES_INCLUDES) test/tables/maps_main.cpp $(MAPS_SOURCES) -o $@

.PHONY: tables-maps
tables-maps: build/schema_test_maps build/schema_test_maps_asan
	./build/schema_test_maps
	./build/schema_test_maps_asan

# THE LoadMeasure REFUSALS AT A MAP are a unit test and not a report row
# (§2.8, §6.5): each wire is built in memory with a SYNTHETIC map count, and
# the answer and the REASON are asserted by name, with a clean wire beside
# them that must measure and load.
.PHONY: tables-maps-measure-refusals
tables-maps-measure-refusals: build/schema_test_maps
	./build/schema_test_maps measure-refusals

# THE MAP-WALK GATE (docs/SPEC-TABLES.md §2.8, §16): the map's half of the text
# form is emitted only in a unit that declares one, it is ONE half, the same
# bytes in every map-bearing .cpp, and none of it reaches a map-free unit,
# which is the zero-cost property (§2.2) holding for the text form.
#
# THE MAP-FREE SET IS DERIVED, NOT NAMED. It was two directory names in this
# recipe, and the byte compare covered one directory's .cpp while every
# map-bearing unit of the corpus carries the half. The premise was right and
# the instrument was narrow. `internal/mapwalk` asks the COMPILER'S OWN IR
# instead: a unit is map-free when no table in its closure carries a map
# (§2.8), and a generated entry is a table of that closure, so a nested map
# reaches the answer with no clause of its own. The corpus is the units
# `tables_generate` emits, read from that define, so the scan is every
# generated .cpp of the tree rather than one directory's ten.
.PHONY: tables-json-map-walk
tables-json-map-walk: build/tables-generated/.stamp
	@mkdir -p build
	SCHEMA_MAP_WALK_DIR=$$PWD/build/tables-generated \
		SCHEMA_MAP_WALK_SUMMARY=$$PWD/build/map-walk.summary \
		go test -count=1 ./internal/mapwalk -run TestMapWalkHalfRidesTheMapBearingUnits
	@cat build/map-walk.summary

# THE NEGATIVE CONTROLS for the map-walk gate (docs/SPEC-TABLES.md §2.8, §16).
# A gate that cannot go red proves nothing, and the three ways this one can
# fail to hold are the three ways the half can land wrong:
#
#   1. PLANTED. The half is put into a map-free unit's emitted .cpp in a
#      throwaway copy. Nothing generates it and no emitter is patched, so
#      this control holds the SCAN alone: it must name the file.
#   2. UNGATED. The emitter's own `if anyMap` is removed, so every unit's
#      .cpp carries the real half. This is the control the gate exists for,
#      because it is what §16's zero-cost promise costs a map-free consumer
#      the day the gating goes, and the derived set has to say which units
#      are free.
#   3. DROPPED. The other half of the same switch: the map half is never
#      emitted, so a map-bearing unit's .cpp carries the stub. A gate that
#      only refused leaks would stay green here.
#
# Each control narrows the run with SCHEMA_MAP_WALK_UNITS, so its red is its
# own and not thirty-five absent trees.
#
# The two gating controls share a recipe. $(1) the control's short name, $(2)
# the sed script over the JSON emitter, $(3) the corpus directory the control
# regenerates and scans, $(4) the sentence a reader gets when the gate stayed
# green, $(5) the clause the red has to be on.
define map_walk_gating_control
	@sed -e $(2) internal/codegen/cpptable/json.go > build/map-walk-$(1).gotext
	@cmp -s build/map-walk-$(1).gotext internal/codegen/cpptable/json.go && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/json.go":"%s/build/map-walk-$(1).gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/map-walk-$(1)-overlay.json
	@go build -overlay=build/map-walk-$(1)-overlay.json -o build/schema-map-walk-$(1) ./cmd/schema
	@rm -rf build/map-walk-$(1)-tree && mkdir -p build/map-walk-$(1)-tree
	@./build/schema-map-walk-$(1) generate --lang cpp --out build/map-walk-$(1)-tree/$(3) tables/$(3)
	@if SCHEMA_MAP_WALK_DIR=$$PWD/build/map-walk-$(1)-tree SCHEMA_MAP_WALK_UNITS=$(3) \
			go test -count=1 ./internal/mapwalk -run TestMapWalkHalfRidesTheMapBearingUnits \
			> build/map-walk-$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(4)"; exit 1; \
	fi
	@grep -q "$(5)" build/map-walk-$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the $(1) clause"; cat build/map-walk-$(1).log; exit 1; }
	@echo "negative control: $(1) turns the MAP-WALK GATE red on $$(grep -c 'MAP-WALK GATE FAILED' build/map-walk-$(1).log) named file(s)"
endef

.PHONY: tables-json-map-walk-negative-controls
tables-json-map-walk-negative-controls: bin/schema build/tables-generated/.stamp
	@rm -rf build/map-walk-planted && mkdir -p build/map-walk-planted/examples
	@cp build/tables-generated/examples/TablesTable.cpp build/map-walk-planted/examples/TablesTable.cpp
	@printf '// ---- json map walk: begin ----\n// PLANTED\n// ---- json map walk: end ----\n' \
		>> build/map-walk-planted/examples/TablesTable.cpp
	@if SCHEMA_MAP_WALK_DIR=$$PWD/build/map-walk-planted SCHEMA_MAP_WALK_UNITS=examples \
			go test -count=1 ./internal/mapwalk -run TestMapWalkHalfRidesTheMapBearingUnits \
			> build/map-walk-planted.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the map half planted in a map-free unit left the gate GREEN"; exit 1; \
	fi
	@grep -q "the map half reached the map-free unit.*examples/TablesTable.cpp" build/map-walk-planted.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not by naming the planted file"; cat build/map-walk-planted.log; exit 1; }
	@echo "negative control: a planted map half turns the MAP-WALK GATE red on $$(grep -c 'MAP-WALK GATE FAILED' build/map-walk-planted.log) named file(s)"
	$(call map_walk_gating_control,ungated,'s@mapAdapters := tableJsonNoMapAdapters@mapAdapters := tableJsonMapAdapters // SABOTAGED@',examples,the map half emitted into every unit left the gate GREEN,the map half reached the map-free unit)
	$(call map_walk_gating_control,dropped,'s@mapAdapters = tableJsonMapAdapters@mapAdapters = tableJsonNoMapAdapters // SABOTAGED@',maps,the map half emitted into no unit at all left the gate GREEN,no map half in)

# ---- the NEGATIVE CONTROLS §2.8 names ------------------------------------
#
# Each names the sabotage, patches the GENERATOR through a Go overlay,
# regenerates the corpus, rebuilds the gate and requires it to go RED. A
# sabotage that patches nothing is itself a failure, so a control cannot rot
# into a no-op when the emitter it names moves.
#
# $(1) the control's short name, $(2) the sed script, $(3) the file to patch,
# $(4) the sentence a reader gets when the gate stayed green.
define map_negative_control
	@mkdir -p build
	@sed -e $(2) $(3) > build/map-$(1).gotext
	@cmp -s build/map-$(1).gotext $(3) && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/$(3)":"%s/build/map-$(1).gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/map-$(1)-overlay.json
	@go build -overlay=build/map-$(1)-overlay.json -o build/schema-map-$(1) ./cmd/schema
	@rm -rf build/tables-map-$(1) && mkdir -p build/tables-map-$(1)
	@./build/schema-map-$(1) generate --lang cpp --out build/tables-map-$(1)/maps tables/maps
	@$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-map-$(1)/maps -Itest/tables test/tables/maps_main.cpp \
		build/tables-map-$(1)/maps/*Table.cpp -o build/schema_test_maps_$(1)
	@if ./build/schema_test_maps_$(1) > build/map-$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(4)"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/maps_main.cpp" build/map-$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on a CHECK"; cat build/map-$(1).log; exit 1; }
	@echo "negative control: $(1) turns the MAP GATE red on $$(grep -c '^FAIL' build/map-$(1).log) failures"
endef

.PHONY: tables-maps-builder-cap-negative-control
tables-maps-builder-cap-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,builder-cap,'s@fill.refused = nodes.carve->worker != NULL;@fill.refused = false;@',internal/codegen/cpptable/maps.go,a builder count refusal became damage without turning the map gate red)
	@grep -Fq '!report.malformed' build/map-builder-cap.log
	go run ./tools/sabotage -name map-builder-count-oracle -out build/map-builder-oracle.gotext internal/tablewire/decode.go
	@printf '{"Replace":{"%s/internal/tablewire/decode.go":"%s/build/map-builder-oracle.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/map-builder-oracle.json
	@if go test -overlay=build/map-builder-oracle.json ./internal/tablewire -run '^TestMapBuilderCountCap$$' -count=1 > build/map-builder-oracle.log 2>&1; then echo 'map builder oracle control stayed green'; exit 1; fi
	@grep -Fq 'must refuse before entries without adding damage' build/map-builder-oracle.log

# THE WRITER EMITS INSERTION ORDER instead of sorted. The instance built OUT OF
# KEY ORDER meets it: the byte compare against its pinned wire goes red while
# measure == save still holds, which says the sabotage is the sort and not the
# arithmetic.
.PHONY: tables-maps-sort-negative-control
tables-maps-sort-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,sort,'s@int32_t start = count / 2 - 1@int32_t start = -1@' -e 's@int32_t end = count - 1@int32_t end = 0@',internal/codegen/cpptable/maps.go,the writer emitting insertion order left the map gate GREEN)

# SAVE EMITS A DEAD ENTRY. The instance that ERASES meets it, and the byte
# compare against its pinned wire goes red while measure == save still holds.
.PHONY: tables-maps-dead-entry-negative-control
tables-maps-dead-entry-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,dead,'s@segment->dead\[ i / 32 \] |= 1u << ( i % 32 );@(void) i; // SABOTAGED@',internal/codegen/cpptable/maps.go,a dead entry riding on the wire left the map gate GREEN)

# THE READER'S ASCENDING CHECK IS DROPPED. The SHUFFLED report row meets it,
# and the row's `malformed` flag goes red.
.PHONY: tables-maps-ascending-negative-control
tables-maps-ascending-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,ascending,'s@if ( order > 0 )@if ( order > 0 \&\& false )@',internal/codegen/cpptable/maps.go,a shuffled map left the map gate GREEN)

# THE DUPLICATE RULE IS DROPPED, first wins or both kept. The row whose
# DUPLICATE entry ELIDES a field the first occurrence set meets it, and the
# decoded value goes red — a reader that overlays instead of resetting agrees
# with the rule on every other body.
.PHONY: tables-maps-duplicate-negative-control
tables-maps-duplicate-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,duplicate,'s@slot = TableMapFillLast( fill );@slot = TableMapFillNext( fill ); // SABOTAGED@',internal/codegen/cpptable/maps.go,keeping both occurrences of a duplicate left the map gate GREEN)

# THE KEY-KIND RULE DECODES ANYWAY. The row written under a CHANGED KEY KIND
# meets it, and its five counters go red, because an entry lands under a
# defaulted key where the row says the map is empty.
.PHONY: tables-maps-key-kind-negative-control
tables-maps-key-kind-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,keykind,'s@out.kind_bad = field_kind != %d;@out.kind_bad = ( field_kind != %d ) \&\& false;@',internal/codegen/cpptable/maps.go,decoding under a changed key kind left the map gate GREEN)

# KEYS NEVER CLAMP. The row whose key is longer than the reader's bound meets
# it, and the DECODED VALUE is the half that says it: a clamped key merges two
# entries where the rule drops one, and the `clamped` count alone cannot
# separate the two.
.PHONY: tables-maps-clamp-negative-control
tables-maps-clamp-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,clamp,'s@out.over = key_len > %d;@out.over = ( key_len > %d ) \&\& false;@',internal/codegen/cpptable/maps.go,clamping an over-long key left the map gate GREEN)

# N = 100000 UNDER A SHORT L. The measure-refusals row meets it: a Fleet of a
# few dozen bytes whose map count the body cannot carry answers -1 with the
# reason count_over_length, and that name goes red if the L check is dropped.
# The count sits UNDER the int32 cap on purpose, because the cap is tested
# first and a count past it never reaches the L check.
.PHONY: tables-maps-fit-negative-control
tables-maps-fit-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,fit,'s@if ( n > (uint64_t) ( rest / kTableMapEntryFloor ) ) { reason = count_over_length; return false; }@if ( n > (uint64_t) ( rest / kTableMapEntryFloor ) \&\& false ) { reason = count_over_length; return false; }@',internal/codegen/cpptable/maps.go,an N the map L cannot carry left the map gate GREEN)

# N = 0x80000000, PAST THE INT32 CAP. The measure-refusals row meets it: the
# cap is tested before the L, so the reason is count_over_extent_cap, and a
# dropped cap check lets the L answer count_over_length instead, which the row
# asserts by name.
.PHONY: tables-maps-cap-negative-control
tables-maps-cap-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,cap,'s@if ( n > (uint64_t) INT32_MAX ) { reason = count_over_extent_cap; return false; }@if ( n > (uint64_t) INT32_MAX \&\& false ) { reason = count_over_extent_cap; return false; }@',internal/codegen/cpptable/maps.go,an N past the int32 cap left the map gate GREEN)

# LOADMEASURE OVER A MAP OF MAPS, summed at ONE DEPTH only. The instance whose
# value is itself a map meets it, and the measure goes red against the region
# Load fills.
.PHONY: tables-maps-depth-negative-control
tables-maps-depth-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,depth,'s@if ( inner == NULL ) { return true; }@return true; // SABOTAGED@',internal/codegen/cpptable/maps.go,summing the extent at one depth only left the map gate GREEN)

# A DUPLICATE KEY REPLACES AND THE VALUE HALF IS RESET WHOLE (§2.8), so a
# repeat that fills nothing reads as the declared default and never as the
# first insert's. The sabotage drops that reset at its call site, which leaves
# the value's STORAGE holding what the first insert put there. A TEXT value is
# where it bites hardest: its storage is a PAIR, the buffer and the int32 used
# length, so the repeat rides the
# first insert's bytes AND its length, and the `Text` unit's duplicate row
# names both. The `Fleet` rows name the same rule for a table value.
.PHONY: tables-maps-value-reset-negative-control
tables-maps-value-reset-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,valuereset,'s@TableResetMapValue( \*found ); // a repeated key is LAST-WINS@(void) 0; // SABOTAGED@',internal/codegen/cpptable/maps.go,a duplicate that did not reset the value half left the map gate GREEN)

# TOJSON WRITES ENTRIES IN ASCENDING KEY ORDER, so unpack then pack is
# byte-stable and a diff of two texts is a diff of two maps (§2.8, §17.2). The
# instance built out of key order meets it, and the round trip's byte compare
# goes red if the writer walks the entries any other way.
.PHONY: tables-maps-text-order-negative-control
tables-maps-text-order-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,textorder,'s@const void \* entry = (const void \*) ( entries + (int64_t) i \* f->elem_size );@const void * entry = (const void *) ( entries + (int64_t) ( count - 1 - i ) * f->elem_size ); // SABOTAGED@',internal/codegen/cpptable/json.go,writing a map text out of key order left the map gate GREEN)

# A MAP IS A BY-VALUE EDGE OF THE ONE DECLARATION-ORDER WALK (§2.8, §3.1), so
# the numbering descends every entry and a node named ONLY by a map entry takes
# its index there. The sabotage short-circuits that per-entry descent. The
# `Docs` and `Chunks` rows are what meet it: their `*string` and `*bytes` values
# are the only slots in this corpus that name a node NOTHING ELSE names, so an
# undescended entry leaves a non-null slot the walk never reached and `Save`
# answers the -1 §7.6 gives a pointer in that position. `Fleet.by_id` cannot
# meet it, because `Fleet.flagship` names the same node.
#
# Its sabotage carries the unbalanced parenthesis a $(call) argument cannot, so
# the recipe is spelled out as the keylength one below is. Everything else about
# it is map_negative_control.
.PHONY: tables-maps-entry-node-negative-control
tables-maps-entry-node-negative-control: bin/schema build/tables-generated/.stamp
	@mkdir -p build
	@sed -e 's@if ( !%sNumber( ctx@if ( false \&\& !%sNumber( ctx@' \
		internal/codegen/cpptable/maps.go > build/map-entrynode.gotext
	@cmp -s build/map-entrynode.gotext internal/codegen/cpptable/maps.go && \
		{ echo "NEGATIVE CONTROL FAILED: the entrynode sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/maps.go":"%s/build/map-entrynode.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/map-entrynode-overlay.json
	@go build -overlay=build/map-entrynode-overlay.json -o build/schema-map-entrynode ./cmd/schema
	@rm -rf build/tables-map-entrynode && mkdir -p build/tables-map-entrynode
	@./build/schema-map-entrynode generate --lang cpp --out build/tables-map-entrynode/maps tables/maps
	@$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-map-entrynode/maps -Itest/tables test/tables/maps_main.cpp \
		build/tables-map-entrynode/maps/*Table.cpp -o build/schema_test_maps_entrynode
	@if ./build/schema_test_maps_entrynode > build/map-entrynode.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an entry the numbering never descended left the map gate GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/maps_main.cpp" build/map-entrynode.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on a CHECK"; cat build/map-entrynode.log; exit 1; }
	@echo "negative control: entrynode turns the MAP GATE red on $$(grep -c '^FAIL' build/map-entrynode.log) failures"

# AN UNREACHED NON-EMPTY MAP SLOT IS REFUSED by Cook and by Lock, the same
# refusal §7.6 gives a pointer in that position. The `Depth` instance whose
# counted array holds a map PAST ITS LIVE COUNT meets it, and dropping the
# refusal lets Lock write that map's entries into an extent nothing reserved
# for them. The predicate is the one a list slot answers by (§2.9), so the
# sabotage patches extent.go and each gate names its own red.
.PHONY: tables-maps-unreached-negative-control
tables-maps-unreached-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,unreached,'s@inline bool TableExtentUnreachedEmpty( int64_t extent ) { return extent == 0; }@inline bool TableExtentUnreachedEmpty( int64_t ) { return true; }@',internal/codegen/cpptable/extent.go,writing an unreached non-empty map left the map gate GREEN)

# A KEY IS DATA AND A LENGTH (§2.8, §3), and the length is CARRIED. The rows
# whose keys hold an interior U+0000 meet it: a lookup that measures to the
# first NUL calls "a" and "a", 0, "b" one key, so two entries become one and a
# repeat of the second becomes two, and the report says `duplicate` for a
# deletion it never names.
#
# Its sabotage carries the commas and the unbalanced parenthesis a $(call)
# argument cannot, so the recipe is spelled out rather than taken from
# map_negative_control above. Everything else about it is that define.
.PHONY: tables-maps-key-length-negative-control
tables-maps-key-length-negative-control: bin/schema build/tables-generated/.stamp
	@mkdir -p build
	@sed -e 's@key\.data, key\.length );@key.data, TableKeyLength( key.data, 255 ) ); // SABOTAGED@' \
		internal/codegen/cpptable/maps.go > build/map-keylength.gotext
	@cmp -s build/map-keylength.gotext internal/codegen/cpptable/maps.go && \
		{ echo "NEGATIVE CONTROL FAILED: the keylength sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/maps.go":"%s/build/map-keylength.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/map-keylength-overlay.json
	@go build -overlay=build/map-keylength-overlay.json -o build/schema-map-keylength ./cmd/schema
	@rm -rf build/tables-map-keylength && mkdir -p build/tables-map-keylength
	@./build/schema-map-keylength generate --lang cpp --out build/tables-map-keylength/maps tables/maps
	@$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-map-keylength/maps -Itest/tables test/tables/maps_main.cpp \
		build/tables-map-keylength/maps/*Table.cpp -o build/schema_test_maps_keylength
	@if ./build/schema_test_maps_keylength > build/map-keylength.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: measuring a key to its first NUL left the map gate GREEN"; exit 1; \
	fi
	@grep -q "^FAIL test/tables/maps_main.cpp" build/map-keylength.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on a CHECK"; cat build/map-keylength.log; exit 1; }
	@echo "negative control: keylength turns the MAP GATE red, $$(grep -c '^FAIL' build/map-keylength.log) failures"

# A KEY THE SCAN COULD NOT HOLD WHOLE IS NOT A SHORTER KEY (§2.8). The EdgeRow
# row meets it: with the check dropped, two 256-byte keys that share 255 bytes
# merge into one entry keyed by a prefix the text never spelled, and the `names`
# bound of 300 is wide enough that the declared bound catches neither.
.PHONY: tables-maps-key-identity-negative-control
tables-maps-key-identity-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,keyidentity,'s@else if ( key_over || token_length > key->array_bound )@else if ( token_length > key->array_bound )@',internal/codegen/cpptable/json.go,truncating a key into the walker buffer left the map gate GREEN)

# THE TARGET DOMAIN IS ESTABLISHED BEFORE THE CAST (§16.2). The uint64 key rows
# meet it: read through the interpreter's own signed lane, a magnitude in the
# high half looks like a negative, so 1e19 and 18446744073709551615 both land as
# the key zero and collide, where the kind holds each of them exactly.
.PHONY: tables-maps-key-domain-negative-control
tables-maps-key-domain-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,keydomain,'s@const uint64_t high = bytes >= 8 ? UINT64_MAX@if ( (int64_t) magnitude < 0 ) { return 0; } const uint64_t high = bytes >= 8 ? UINT64_MAX@',internal/codegen/cpptable/json.go,reading an unsigned magnitude through a signed lane left the map gate GREEN)

# AN ALLOCATION FAILURE IS NOT AN OVERSIZED KEY (§2.8, §16.1). The refusal row
# meets it: labeling the arena's refusal `clamped` skips the entry, reads on
# and calls the whole text clean, which is the one outcome the neighboring
# list, blob and pointer paths never give a refusal.
.PHONY: tables-maps-place-failure-negative-control
tables-maps-place-failure-negative-control: bin/schema build/tables-generated/.stamp
	$(call map_negative_control,placefail,'s@if ( place \&\& entry == NULL )@if ( place \&\& entry == NULL \&\& false )@',internal/codegen/cpptable/json.go,reading on past an arena refusal left the map gate GREEN)

.PHONY: tables-maps-negative-controls
tables-maps-negative-controls: tables-maps-sort-negative-control \
	tables-maps-dead-entry-negative-control \
	tables-maps-ascending-negative-control \
	tables-maps-duplicate-negative-control \
	tables-maps-key-kind-negative-control \
	tables-maps-clamp-negative-control \
	tables-maps-fit-negative-control \
	tables-maps-cap-negative-control \
	tables-maps-depth-negative-control \
	tables-maps-text-order-negative-control \
	tables-maps-key-length-negative-control \
	tables-maps-key-identity-negative-control \
	tables-maps-key-domain-negative-control \
	tables-maps-place-failure-negative-control \
	tables-maps-value-reset-negative-control \
	tables-maps-entry-node-negative-control \
	tables-maps-unreached-negative-control

# ---- THE LIST GATE (docs/SPEC-TABLES.md §2.9) ------------------------------
#
# One binary over the `tables/lists` corpus: the builder's three, the four
# writing walks in INDEX order, the node extent a region and a cook carry,
# every reader rule §2.9 states, the migration golden, and the clamp control
# at 100,000. Its wire goldens are the reference's, pinned like every other
# table golden, and the cooks it writes are read by `schema cook-check`, whose
# scan carries §7.4's element-array clause, beside a FORGERY whose list slot
# points its array past the holder's extent, which the tool must refuse.
#
# THE SANITIZED TWIN rides beside it for the map gate's reason: segments, an
# element array carved from a node's own extent and indexing over mapped
# bytes are lifetime and bounds questions -Werror cannot see.

LISTS_SOURCES = $$(ls build/tables-generated/lists/*Table.cpp)

# THE RETAIN-UNKNOWN GATE (docs/SPEC-TABLES.md §6.6): the RT1/RT2/RT3 set, the
# round trip at every depth, the five excluded classes a wire can carry to the
# unknown arm, the two capacities and the walk's verdict on damage inside sound
# outer framing.
build/schema_test_retain: build/tables-generated/.stamp test/tables/retain_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/retain_main.cpp -o $@

build/schema_test_retain_asan: build/tables-generated/.stamp test/tables/retain_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-omit-frame-pointer -g \
		$(TABLES_INCLUDES) test/tables/retain_main.cpp -o $@

.PHONY: tables-retain
tables-retain: build/schema_test_retain build/schema_test_retain_asan
	./build/schema_test_retain
	./build/schema_test_retain_asan

# THE FIXED-CLASS REFUSALS ARE COMPILE ERRORS, and each must name itself (§6.6, §3.3).
# A refusal that were a MISSING SYMBOL would be a linker error with no reason
# in it, which is exactly what §11's rule for a surface a class does not carry
# refuses, so the control asks for the name and greps for the sentence.
.PHONY: tables-retain-fixed-class-negative-control
tables-retain-fixed-class-negative-control: build/tables-generated/.stamp
	@mkdir -p build
	@printf '#include "RT1Table.h"\nint main()\n{\n    tblrt1::Inner value;\n    uint8_t storage[ 64 ];\n    tblrt1::TableRetain retain;\n    retain.bytes = storage;\n    tblrt1::TableReport report;\n    (void) tblrt1::InnerLoadRetain( value, storage, (int64_t) 0, &retain, &report );\n    return 0;\n}\n' > build/retain-fixed-class.cpp
	@if $(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) -c build/retain-fixed-class.cpp -o build/retain-fixed-class.o > build/retain-fixed-class.log 2>&1; then \
		echo "RETAIN GATE FAILED: LoadRetain compiled on a FIXED-class root"; exit 1; \
	fi
	@grep -q "FIXED-class root" build/retain-fixed-class.log || { echo "RETAIN GATE FAILED: the fixed-class refusal was not by name"; cat build/retain-fixed-class.log; exit 1; }
	@echo "the fixed-class root refuses retention BY NAME (docs/SPEC-TABLES.md §6.6)"
# AND THE MESSAGE FORM'S TWO ON THE SAME ROOT (§3.3, schema#680). A fixed-class
# root that declared only the variable form's three would answer LoadRetainMessages
# with a missing symbol, which is a compile error with no reason in it, so the
# row asks for the name and greps for the same sentence.
	@printf '#include "RT1Table.h"\nint main()\n{\n    const tblrt1::Inner * roots[1] = { NULL };\n    int64_t count = 1;\n    uint8_t storage[ 64 ];\n    tblrt1::TableMessageEntry entries[ 1 ];\n    tblrt1::TableVocabulary vocabulary( entries, 1 );\n    tblrt1::TableRetain retain;\n    retain.bytes = storage;\n    tblrt1::TableReport report;\n    (void) tblrt1::InnerLoadRetainMessages( roots, &count, storage, (int64_t) 64, vocabulary, storage, (int64_t) 0, &retain, &report );\n    return 0;\n}\n' > build/retain-fixed-class-message.cpp
	@if $(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) -c build/retain-fixed-class-message.cpp -o build/retain-fixed-class-message.o > build/retain-fixed-class-message.log 2>&1; then \
		echo "RETAIN GATE FAILED: LoadRetainMessages compiled on a FIXED-class root"; exit 1; \
	fi
	@grep -q "FIXED-class root" build/retain-fixed-class-message.log || { echo "RETAIN GATE FAILED: the fixed-class refusal of the message form was not by name"; cat build/retain-fixed-class-message.log; exit 1; }
	@echo "the fixed-class root refuses the message form's retention BY NAME (docs/SPEC-TABLES.md §6.6, §3.3)"

# ---- A FLOAT IS FINITE (docs/SPEC-TABLES.md §3.3; Glenn, 2026-10-27, "tables
# ---- go finite-only too, same rule as packets") -----------------------------
#
# The pin is testdata/wire/tables/floats_nan.bin — a signalling NaN, a NaN with
# a payload past the quiet bit, both signs, at both widths and as array
# elements — FROZEN as the shared MUST-REFUSE vector: no writer can produce it
# any more, and every read refuses it as non_finite_float. This binary carries
# the CLAIM over every form the C++ leg has: the pin refused at both widths
# (the float rung included) and landing nothing, every writer refusing every
# non-finite pattern, a forged fixed record and a forged message refused, and
# -0 and the subnormals riding bit-exact with -0 never elided.
FLOATNAN_SOURCES = $$(ls build/tables-generated/f1/*Table.cpp build/tables-generated/f2/*Table.cpp)

build/schema_test_floatnan: build/tables-generated/.stamp test/tables/floatnan_main.cpp test/tables/floatnan.h
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/f1 -Ibuild/tables-generated/f2 -Itest/tables \
		test/tables/floatnan_main.cpp $(FLOATNAN_SOURCES) -o $@

.PHONY: tables-float-nan
tables-float-nan: build/schema_test_floatnan
	./build/schema_test_floatnan

# ITS NEGATIVE CONTROL: the READ's mask test is the rule, and this makes it
# answer "finite" for every pattern — the read a port gets wrong by testing the
# value after the conversion, or by not testing at all. The emitter is
# sabotaged, ONLY the f1/f2 pair is regenerated from it, and the pin must go
# RED — on the refusal, not on a compile error.
.PHONY: tables-float-nan-negative-control
tables-float-nan-negative-control: bin/schema build/tables-generated/.stamp
	@rm -rf build/float-nan-control && mkdir -p build/float-nan-control
	@sed -e 's@^inline bool TableBitsFinite32( uint32_t bits ) { return .*@inline bool TableBitsFinite32( uint32_t bits ) { (void) bits; return true; } // SABOTAGED: the read never refuses@' \
		-e 's@^inline bool TableBitsFinite64( uint64_t bits ) { return .*@inline bool TableBitsFinite64( uint64_t bits ) { (void) bits; return true; } // SABOTAGED@' \
		internal/codegen/cpptable/cpptable.go > build/float-nan-control/cpptable.go.txt
	@test "$$(grep -c 'SABOTAGED' build/float-nan-control/cpptable.go.txt)" = 2 || \
		{ echo "NEGATIVE CONTROL FAILED: the finite-float sabotage did not patch both mask tests"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/float-nan-control/cpptable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/float-nan-control/overlay.json
	@go build -overlay=build/float-nan-control/overlay.json -o build/schema-float-nan-control ./cmd/schema
	@./build/schema-float-nan-control generate --lang cpp --out build/float-nan-control/f1 test/tables/F1.schema
	@./build/schema-float-nan-control generate --lang cpp --out build/float-nan-control/f2 test/tables/F2.schema
	@$(CXX) $(TABLES_CXXFLAGS) -Ibuild/float-nan-control/f1 -Ibuild/float-nan-control/f2 -Itest/tables \
		test/tables/floatnan_main.cpp build/float-nan-control/f1/*Table.cpp build/float-nan-control/f2/*Table.cpp \
		-o build/schema_test_floatnan_control
	@if ./build/schema_test_floatnan_control > build/float-nan-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a read that never tests the bits left the finite pin GREEN"; exit 1; \
	fi
	@grep -q "floats_nan was not refused as non_finite_float by the TOLERANT read" build/float-nan-control/log && \
	 grep -q "floats_nan was not refused as non_finite_float through the widened rung" build/float-nan-control/log && \
	 grep -q "a fixed record carrying 0x7f800001 was not refused" build/float-nan-control/log && \
	 grep -q "a message carrying 0x7f800001 was not refused" build/float-nan-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the pin went red, but not on every read's refusal"; \
		  cat build/float-nan-control/log; exit 1; }
	@echo "negative control: a read that never tests the bits turns the finite-float pin RED"

build/schema_test_lists: build/tables-generated/.stamp test/tables/lists_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/lists_main.cpp $(LISTS_SOURCES) -o $@

build/schema_test_lists_asan: build/tables-generated/.stamp test/tables/lists_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-omit-frame-pointer -g \
		$(TABLES_INCLUDES) test/tables/lists_main.cpp $(LISTS_SOURCES) -o $@

.PHONY: tables-lists
tables-lists: build/schema_test_lists build/schema_test_lists_asan
	@rm -rf build/lists-cooks && mkdir -p build/lists-cooks
	SCHEMA_LIST_COOK_DIR=build/lists-cooks ./build/schema_test_lists
	./build/schema_test_lists_asan
	# `schema cook-check` reads what the runtime cooked (§7.4): the root's list
	# slot, every element's own slots and companions, and a pointed-at holder's
	# list, and refuses the forgery beside them. The cook whose element holds a
	# MAP is READ now, not refused: §7.4's map-slot clause landed in the tool
	# with schema#380, so the entry array takes the same four clauses a list's
	# does and the keys are read ascending on top of them.
	./bin/schema cook-check --root Save build/lists-cooks/save.cook tables/lists
	./bin/schema cook-check --root Sheet build/lists-cooks/sheet.cook tables/lists
	./bin/schema cook-check --root Army build/lists-cooks/army.cook tables/lists
	@if ./bin/schema cook-check --root Sheet build/lists-cooks/sheet-forged.cook tables/lists > build/lists-cooks/forged.log 2>&1; then \
		echo "LIST GATE FAILED: cook-check accepted a list slot pointing past its holder's extent"; exit 1; \
	fi
	@grep -q "leaves\|extent" build/lists-cooks/forged.log || { echo "LIST GATE FAILED: the forgery was refused, but not on the element-array clause"; cat build/lists-cooks/forged.log; exit 1; }
	@echo "list gate: cook-check reads three cooks the runtime wrote, the map-holding one included, and refuses the forged list slot"

# THE SIX LoadMeasure REFUSALS are a unit test and not a `report` row (§2.8,
# §2.9, §6.5): each wire is built in memory with a SYNTHETIC count, a list's
# and a map's, and the answer and the REASON are asserted, with a clean wire
# beside them that must measure.
.PHONY: tables-list-measure-refusals
tables-list-measure-refusals: build/schema_test_lists
	./build/schema_test_lists measure-refusals

# THE LIST-WALK GATE (docs/SPEC-TABLES.md §2.9, §13.5, §16): the list's half of
# the text form is emitted only in a unit that declares one, it is ONE half,
# the same bytes in every list-bearing .cpp, and none of it reaches a list-free
# unit, which is the zero-cost property (§2.2) holding for the text form.
#
# THE LIST-FREE SET IS DERIVED, NOT NAMED. It was three directory names in this
# recipe until #662 gave `tables/maps` a `map[uint8][]Item`: the unit became
# list-bearing, the emitter emitted the half correctly, and the copy of the
# fact standing here reported that as a leak. The premise was right and the
# instrument was wrong. `internal/listwalk` asks the COMPILER'S OWN IR instead.
# A unit is list-free when no table in its closure carries an unbounded array,
# and a map's generated entry is a table of that closure (§2.8), so `map[K][]T`
# reaches the answer with no clause of its own. The corpus is the units
# `tables_generate` emits, read from that define, so the scan is every
# generated .cpp of the tree rather than one directory's five.
.PHONY: tables-json-list-walk
tables-json-list-walk: build/tables-generated/.stamp
	@mkdir -p build
	SCHEMA_LIST_WALK_DIR=$$PWD/build/tables-generated \
		SCHEMA_LIST_WALK_SUMMARY=$$PWD/build/list-walk.summary \
		go test -count=1 ./internal/listwalk -run TestListWalkHalfRidesTheListBearingUnits
	@cat build/list-walk.summary

# THE NEGATIVE CONTROLS for the list-walk gate (docs/SPEC-TABLES.md §2.9,
# §16). A gate that cannot go red proves nothing, and the three ways this one
# can fail to hold are the three ways the half can land wrong:
#
#   1. PLANTED. The half is put into a list-free unit's emitted .cpp in a
#      throwaway copy. Nothing generates it and no emitter is patched, so
#      this control holds the SCAN alone: it must name the file.
#   2. UNGATED. The emitter's own `if anyList` is removed, so every unit's
#      .cpp carries the real half. This is the control the gate exists for,
#      because it is what §13.5's ruling costs a list-free consumer the day
#      the gating goes, and the derived set has to say which units are free.
#   3. DROPPED. The other half of the same switch: the list half is never
#      emitted, so a list-bearing unit's .cpp carries the stub. A gate that
#      only refused leaks would stay green here.
#
# Each control narrows the run with SCHEMA_LIST_WALK_UNITS, so its red is its
# own and not thirty-five absent trees.
#
# The two gating controls share a recipe. $(1) the control's short name, $(2)
# the sed script over the JSON emitter, $(3) the corpus directory the control
# regenerates and scans, $(4) the sentence a reader gets when the gate stayed
# green, $(5) the clause the red has to be on.
define list_walk_gating_control
	@sed -e $(2) internal/codegen/cpptable/json.go > build/list-walk-$(1).gotext
	@cmp -s build/list-walk-$(1).gotext internal/codegen/cpptable/json.go && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/json.go":"%s/build/list-walk-$(1).gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/list-walk-$(1)-overlay.json
	@go build -overlay=build/list-walk-$(1)-overlay.json -o build/schema-list-walk-$(1) ./cmd/schema
	@rm -rf build/list-walk-$(1)-tree && mkdir -p build/list-walk-$(1)-tree
	@./build/schema-list-walk-$(1) generate --lang cpp --out build/list-walk-$(1)-tree/$(3) tables/$(3)
	@if SCHEMA_LIST_WALK_DIR=$$PWD/build/list-walk-$(1)-tree SCHEMA_LIST_WALK_UNITS=$(3) \
			go test -count=1 ./internal/listwalk -run TestListWalkHalfRidesTheListBearingUnits \
			> build/list-walk-$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(4)"; exit 1; \
	fi
	@grep -q "$(5)" build/list-walk-$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the $(1) clause"; cat build/list-walk-$(1).log; exit 1; }
	@echo "negative control: $(1) turns the LIST-WALK GATE red on $$(grep -c 'LIST-WALK GATE FAILED' build/list-walk-$(1).log) named file(s)"
endef

.PHONY: tables-json-list-walk-negative-controls
tables-json-list-walk-negative-controls: bin/schema build/tables-generated/.stamp
	@rm -rf build/list-walk-planted && mkdir -p build/list-walk-planted/examples
	@cp build/tables-generated/examples/TablesTable.cpp build/list-walk-planted/examples/TablesTable.cpp
	@printf '// ---- json list walk: begin ----\n// PLANTED\n// ---- json list walk: end ----\n' \
		>> build/list-walk-planted/examples/TablesTable.cpp
	@if SCHEMA_LIST_WALK_DIR=$$PWD/build/list-walk-planted SCHEMA_LIST_WALK_UNITS=examples \
			go test -count=1 ./internal/listwalk -run TestListWalkHalfRidesTheListBearingUnits \
			> build/list-walk-planted.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the list half planted in a list-free unit left the gate GREEN"; exit 1; \
	fi
	@grep -q "the list half reached the list-free unit.*examples/TablesTable.cpp" build/list-walk-planted.log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not by naming the planted file"; cat build/list-walk-planted.log; exit 1; }
	@echo "negative control: a planted list half turns the LIST-WALK GATE red on $$(grep -c 'LIST-WALK GATE FAILED' build/list-walk-planted.log) named file(s)"
	$(call list_walk_gating_control,ungated,'s@listAdapters := tableJsonNoListAdapters@listAdapters := tableJsonListAdapters // SABOTAGED@',examples,the list half emitted into every unit left the gate GREEN,the list half reached the list-free unit)
	$(call list_walk_gating_control,dropped,'s@listAdapters = tableJsonListAdapters@listAdapters = tableJsonNoListAdapters // SABOTAGED@',lists,the list half emitted into no unit at all left the gate GREEN,no list half in)

# ---- the NEGATIVE CONTROLS §2.9 names ------------------------------------
#
# The map controls' shape: each names the sabotage, patches the GENERATOR
# through a Go overlay, regenerates the corpus, rebuilds the gate and requires
# it to go RED on a CHECK. A sabotage that patches nothing is itself a failure.
#
# $(1) the control's short name, $(2) the sed script, $(3) the file to patch,
# $(4) the sentence a reader gets when the gate stayed green.
# a comma inside a $(call) argument, spelled so the call does not split on it
comma := ,

define list_negative_control
	@mkdir -p build
	@sed -e $(2) $(3) > build/list-$(1).gotext
	@cmp -s build/list-$(1).gotext $(3) && \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/$(3)":"%s/build/list-$(1).gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/list-$(1)-overlay.json
	@go build -overlay=build/list-$(1)-overlay.json -o build/schema-list-$(1) ./cmd/schema
	@rm -rf build/tables-list-$(1) && mkdir -p build/tables-list-$(1)
	@./build/schema-list-$(1) generate --lang cpp --out build/tables-list-$(1)/lists tables/lists
	@$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-list-$(1)/lists -Itest/tables test/tables/lists_main.cpp \
		build/tables-list-$(1)/lists/*Table.cpp -o build/schema_test_lists_$(1)
	@if ./build/schema_test_lists_$(1) > build/list-$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(4)"; exit 1; \
	fi
	@grep -q "^FAIL" build/list-$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on a CHECK"; cat build/list-$(1).log; exit 1; }
	@echo "negative control: $(1) turns the LIST GATE red: $$(grep -c '^FAIL' build/list-$(1).log) failures"
endef

# THE WRITER EMITS THE ELEMENTS OUT OF ORDER. The cursor is made to step the
# builder's segments from the LAST slot back; `list_scalars` meets it, and the
# byte compare against its pinned wire goes red while measure == save holds.
.PHONY: tables-lists-order-negative-control
tables-lists-order-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,order,'s@return segment->elements + within;@return segment->elements + ( segment->used - 1 - within ); // SABOTAGED@',internal/codegen/cpptable/lists.go,the writer emitting elements out of order left the list gate GREEN)

# `Save` EMITS A DEAD ELEMENT. `list_erased` meets it, an erase from the
# MIDDLE with an add after it, and the byte compare goes red while
# measure == save still holds, which says the sabotage is the skip.
.PHONY: tables-lists-dead-element-negative-control
tables-lists-dead-element-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,dead,'s@if ( !TableListSegmentDead( segment->dead, within ) ) { break; }@break; // SABOTAGED@',internal/codegen/cpptable/lists.go,a dead element riding on the wire left the list gate GREEN)

# THE ELEMENT ARRAY IS LAID OUT AFTER A NESTED CONTAINER'S, breaking the
# pre-order rule in BOTH writers of a list whose element holds a map: the
# pack's extent walk stops reserving the element array ahead of the elements'
# maps, and the cook's extent writer stops stepping past it, so the maps are
# laid where the elements are and the node's extent is short of the array.
# `list_of_maps` meets it, and the two instruments §2.9 names go red
# together: the pinned cook's byte compare, and `schema cook-check`'s
# containment clause on the cook the sabotaged gate wrote, which must be that
# clause and not the map slot's refusal by name.
.PHONY: tables-lists-preorder-negative-control
tables-lists-preorder-negative-control: bin/schema build/tables-generated/.stamp
	@mkdir -p build
	@printf 'func listElementHoldsMap(f *ir.Field) bool {\n\tref := listElementStruct(f)\n\tif ref == nil {\n\t\treturn false\n\t}\n\tfor i := range ref.Fields {\n\t\tif ref.Fields[i].IsMap() {\n\t\t\treturn true\n\t\t}\n\t}\n\treturn false\n}\n' > build/list-preorder-helper.txt
	$(call list_negative_control,preorder,'s@g.pf("%s    at += (int64_t) cursor.count \* %d; // the whole array FIRST\\n", ind, size)@if !listElementHoldsMap(f) { g.pf("%s    at += (int64_t) cursor.count * %d; // the whole array FIRST\\n", ind, size) } // SABOTAGED@' -e 's@g.pf("%s    at += (int64_t) cursor.count \* (int64_t) sizeof( %s ); // the whole array FIRST\\n", ind, elem)@if !listElementHoldsMap(f) { g.pf("%s    at += (int64_t) cursor.count * (int64_t) sizeof( %s ); // the whole array FIRST\\n", ind, elem) } // SABOTAGED@' -e '$$r build/list-preorder-helper.txt',internal/codegen/cpptable/extent.go,laying the element array after a nested container left the list gate GREEN)
	@grep -c "SABOTAGED" build/list-preorder.gotext | grep -qx 2 || \
		{ echo "NEGATIVE CONTROL FAILED: the preorder sabotage did not reach both the pack's and the cook's list branch"; exit 1; }
	@grep -q "^FAIL.*list_of_maps_cook" build/list-preorder.log || \
		{ echo "NEGATIVE CONTROL FAILED: the pinned list_of_maps_cook byte compare stayed GREEN"; cat build/list-preorder.log; exit 1; }
	@rm -rf build/tables-list-preorder/cooks && mkdir -p build/tables-list-preorder/cooks
	@SCHEMA_LIST_COOK_DIR=build/tables-list-preorder/cooks ./build/schema_test_lists_preorder > /dev/null 2>&1 || true
	@if ./bin/schema cook-check --root Army build/tables-list-preorder/cooks/army.cook tables/lists > build/list-preorder-check.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: cook-check accepted the cook the sabotaged writer laid"; exit 1; \
	fi
	@grep -q "the array leaves the node\|overlaps another array" build/list-preorder-check.log || \
		{ echo "NEGATIVE CONTROL FAILED: cook-check refused the sabotaged cook, but not on the containment clause"; cat build/list-preorder-check.log; exit 1; }
	@echo "negative control: preorder turns the pinned list_of_maps_cook compare and cook-check's containment clause red: $$(grep -o 'the array leaves the node\|overlaps another array' build/list-preorder-check.log | head -1)"

# THE WALK VISITS LISTS OUT OF DECLARATION ORDER, grouped after the pointer
# fields: the edge walk is made to take every list field last, so
# `list_before_pointer`'s `cover` reaches the shared node before the list
# does, and the pinned wire goes red on the node numbering.
.PHONY: tables-lists-walk-order-negative-control
tables-lists-walk-order-negative-control: bin/schema build/tables-generated/.stamp
	@mkdir -p build
	@printf 'func listsLast(fields []*ir.Field) []*ir.Field {\n\tvar first, last []*ir.Field\n\tfor _, f := range fields {\n\t\tif f.IsList() {\n\t\t\tlast = append(last, f)\n\t\t} else {\n\t\t\tfirst = append(first, f)\n\t\t}\n\t}\n\treturn append(first, last...)\n}\n' > build/list-walkorder-helper.txt
	$(call list_negative_control,walkorder,'/guards := guardWalk(st$(comma) v.read+".")/$(comma)/^}/ s@for _$(comma) f := range st.Fields {@for _$(comma) f := range listsLast(st.Fields) { // SABOTAGED@' -e '$$r build/list-walkorder-helper.txt',internal/codegen/cpptable/pointers.go,grouping the lists after the pointer fields left the list gate GREEN)

# A SHARED NODE IS WRITTEN TWICE: `list_shared`, whose two slots name one
# node, meets it, and the region's byte count and the text round trip's &node
# resolution go red. The sabotage reaches a fresh map entry per visit.
.PHONY: tables-lists-shared-negative-control
tables-lists-shared-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,shared,'s@const TablePackEntry \* entry = TablePackMapReach( seen, (const void \*) pointee, 0, taken, slot );@const TablePackEntry * entry = TablePackMapReach( seen, (const void *) pointee, 0, taken, slot ); taken = true; // SABOTAGED@',internal/codegen/cpptable/pointers.go,writing a shared node twice left the list gate GREEN)

# THE READER CLAMPS THE COUNT against something: the 100,000-element row
# meets it, and the decoded count goes red. The sabotage clamps at 2^16.
.PHONY: tables-lists-clamp-negative-control
tables-lists-clamp-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,clamp,'s@    fill.capacity = (int32_t) n;@    fill.capacity = n > 65536 ? 65536 : (int32_t) n; // SABOTAGED@',internal/codegen/cpptable/lists.go,clamping the count left the list gate GREEN)

# THE ELEMENT-KIND RULE DECODES ANYWAY: the `Ints`-as-`Floats` row meets it,
# and the decoded values go red.
.PHONY: tables-lists-element-kind-negative-control
tables-lists-element-kind-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,elemkind,'s@else if ( elem_kind != %d ) { r.report->kind_mismatch++; r.offset = body_end; break; }@else if ( elem_kind != %d \&\& false ) { r.report->kind_mismatch++; r.offset = body_end; break; }@',internal/codegen/cpptable/lists.go,decoding under a changed element kind left the list gate GREEN)

# LoadMeasure OVER A LIST OF TABLES HOLDING LISTS, summed at ONE DEPTH only:
# `list_nested` meets it, and the measure goes red against the region Load
# fills.
.PHONY: tables-lists-depth-negative-control
tables-lists-depth-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,depth,'s@if ( inner == NULL ) { return true; } // nothing below an element: one depth is the whole term@return true; // SABOTAGED@',internal/codegen/cpptable/lists.go,summing the extent at one depth only left the list gate GREEN)

# THE FIT CHECK IS DROPPED: an N the list's L cannot carry measures, and the
# refusals battery goes red on the reason.
.PHONY: tables-lists-fit-negative-control
tables-lists-fit-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,fit,'s@if ( n > (uint64_t) ( rest / elem_floor ) ) { reason = count_over_length; return false; }@if ( n > (uint64_t) ( rest / elem_floor ) \&\& false ) { reason = count_over_length; return false; }@',internal/codegen/cpptable/lists.go,an N the list L cannot carry left the list gate GREEN)

# AN UNREACHED NON-EMPTY LIST SLOT IS REFUSED by Cook and by Lock: the `Deck`
# instance whose counted array holds a list PAST ITS LIVE COUNT meets it.
.PHONY: tables-lists-unreached-negative-control
tables-lists-unreached-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,unreached,'s@inline bool TableExtentUnreachedEmpty( int64_t extent ) { return extent == 0; }@inline bool TableExtentUnreachedEmpty( int64_t ) { return true; }@',internal/codegen/cpptable/extent.go,writing an unreached non-empty list left the list gate GREEN)

# `schema cook-check`'S ELEMENT-ARRAY CLAUSE IS DROPPED (§7.4): the forged
# cook whose list slot points past its holder's extent passes the tool, and the
# Go test that holds the clause goes red.
.PHONY: tables-lists-cook-check-negative-control
tables-lists-cook-check-negative-control:
	@rm -rf build/list-cook-check-control && mkdir -p build/list-cook-check-control
	@sed -e 's@start, end, fits := arrayExtent(at, delta, count, size, s.base, s.extent)@start, end, fits := at + delta, at + delta + count * size, true // SABOTAGED@' \
		internal/tablecook/check.go > build/list-cook-check-control/check.go.txt

	@cmp -s internal/tablecook/check.go build/list-cook-check-control/check.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the cook-check list sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablecook/check.go":"%s/build/list-cook-check-control/check.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/list-cook-check-control/overlay.json
	@if go test -count=1 -overlay=build/list-cook-check-control/overlay.json \
			-run 'TestCookCheckListSlot' ./internal/tablecook/ > build/list-cook-check-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: dropping cook-check's element-array clause left its test GREEN"; exit 1; \
	fi
	@grep -q "FAILED: cook-check accepted a list slot" build/list-cook-check-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the test went red, but not on containment"; cat build/list-cook-check-control/log; exit 1; }
	@echo "negative control: dropping cook-check's element-array clause turns its test RED"

# AND THE MAP SLOT'S OWN TWO (schema#380, §7.4). The containment clause is
# SHARED — a list's slot and a map's are one shape — so the sabotage above must
# turn the MAP's test red too; the ascending clause is the map's alone, and it
# gets its own sabotage, because a shared control that never fired on the
# fifth clause would leave it untested.
# The prerequisite creates the shared overlay even when CI runs maps alone.
.PHONY: tables-maps-cook-check-negative-control
tables-maps-cook-check-negative-control: tables-lists-cook-check-negative-control
	@rm -rf build/map-cook-check-control && mkdir -p build/map-cook-check-control
	@if go test -count=1 -overlay=build/list-cook-check-control/overlay.json \
			-run 'TestCookCheckMapSlot' ./internal/tablecook/ > build/map-cook-check-control/shared.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: dropping the shared containment clause left the map-slot test GREEN"; exit 1; \
	fi
	@grep -q -- "--- FAIL: TestCookCheckMapSlot/the_entries_leave_the_node" build/map-cook-check-control/shared.log || \
		{ echo "NEGATIVE CONTROL FAILED: the shared test went red, but not on containment"; cat build/map-cook-check-control/shared.log; exit 1; }
	@sed -e 's@		if order > 0 {@		if order > 0 \&\& false { // SABOTAGED: the entries may descend@' \
		-e 's@		if order == 0 {@		if order == 0 \&\& false { // SABOTAGED: a key may repeat@' \
		internal/tablecook/check.go > build/map-cook-check-control/check.go.txt
	@cmp -s internal/tablecook/check.go build/map-cook-check-control/check.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the ascending sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablecook/check.go":"%s/build/map-cook-check-control/check.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/map-cook-check-control/overlay.json
	@if go test -count=1 -overlay=build/map-cook-check-control/overlay.json \
			-run 'TestCookCheckMapSlot' ./internal/tablecook/ > build/map-cook-check-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: dropping the keys' ascending clause left the map-slot test GREEN"; exit 1; \
	fi
	@grep -q "the keys descend\|the keys repeat" build/map-cook-check-control/log || \
		{ echo "NEGATIVE CONTROL FAILED: the test went red, but not on the keys"; \
		  cat build/map-cook-check-control/log; exit 1; }
	@echo "negative control: dropping cook-check's map-slot clauses turns its test RED, containment and keys"

# ---- THE TWO WRITERS OF ONE LIST COOK (schema#380, §2.9, §7.6) -------------
#
# The tool's COOK and UNCOOK halves carry the unbounded array now, so the
# pairing every other class already has applies to this one: the tool cooks an
# instance of test/tables/L1.schema — a list-bearing unit with NO MAP in it,
# because the tool's cook still refuses a map-bearing one — and the generated
# `SaveCook` has to land on those bytes exactly, in both byte orders.
LISTCOOK_SOURCES = $$(ls build/tables-generated/l1/*Table.cpp)

build/schema_test_listcook: build/tables-generated/.stamp test/tables/listcook_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/l1 -Itest/tables \
		test/tables/listcook_main.cpp $(LISTCOOK_SOURCES) -o $@

.PHONY: tables-lists-tool-cook
tables-lists-tool-cook: build/schema_test_listcook
	@rm -rf build/list-tool-cook && mkdir -p build/list-tool-cook
	SCHEMA_LIST_TOOL_COOK_DIR=$(CURDIR)/build/list-tool-cook go test -count=1 \
		-run 'TestTheToolCooksAndUncooksAList|TestAListRidesInItsHolderNodeExtent|TestTheToolWritesTheReferencesListCook' \
		./internal/tablecook/
	./build/schema_test_listcook build/list-tool-cook
	# and the CLI's own round trip over the same unit, which is where a user meets it
	./bin/schema cook-check --root Save --verbose build/list-tool-cook/l1.cook test/tables/L1.schema
	./bin/schema uncook --root Save --in build/list-tool-cook/l1.cook --out build/list-tool-cook/back.bin test/tables/L1.schema
	cmp build/list-tool-cook/l1.bin build/list-tool-cook/back.bin
	./bin/schema cook --root Save --in build/list-tool-cook/l1.bin --out build/list-tool-cook/again.cook test/tables/L1.schema
	cmp build/list-tool-cook/l1.cook build/list-tool-cook/again.cook

# ITS NEGATIVE CONTROL, in the cook-check control's own shape: the tool's carve
# is made to lay a list's array at the running offset WITHOUT aligning it, and
# the round trip and the extent's shape both have to go red.
.PHONY: tables-lists-tool-cook-negative-control
tables-lists-tool-cook-negative-control:
	@rm -rf build/list-tool-cook-control && mkdir -p build/list-tool-cook-control
	@sed -e 's@	c.at = alignUp(c.at, align)@	c.at = c.at // SABOTAGED: the array is laid unaligned@' \
		internal/tablecook/extent.go > build/list-tool-cook-control/extent.go.txt
	@cmp -s internal/tablecook/extent.go build/list-tool-cook-control/extent.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the carve sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablecook/extent.go":"%s/build/list-tool-cook-control/extent.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/list-tool-cook-control/overlay.json
	@if go test -count=1 -overlay=build/list-tool-cook-control/overlay.json \
			-run 'TestAListRidesInItsHolderNodeExtent' ./internal/tablecook/ > build/list-tool-cook-control/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: an unaligned element array left the tool's cook GREEN"; exit 1; \
	fi
	@echo "negative control: an unaligned element array turns the tool's list cook RED"

# AN ALLOCATION IS PLANTED IN Load: the gate's operator new counter sees it on
# the reading path, and the allocation audit goes red.
.PHONY: tables-lists-allocation-negative-control
tables-lists-allocation-negative-control: bin/schema build/tables-generated/.stamp
	$(call list_negative_control,allocation,'s@        Element \* element = fill.array + fill.list->count;@        Element * element = fill.array + fill.list->count; delete new int; // SABOTAGED@',internal/codegen/cpptable/lists.go,an allocation planted in Load left the allocation audit GREEN)

.PHONY: tables-lists-negative-controls
tables-lists-negative-controls: tables-lists-allocation-negative-control \
	tables-lists-order-negative-control \
	tables-lists-dead-element-negative-control \
	tables-lists-preorder-negative-control \
	tables-lists-walk-order-negative-control \
	tables-lists-shared-negative-control \
	tables-lists-clamp-negative-control \
	tables-lists-element-kind-negative-control \
	tables-lists-depth-negative-control \
	tables-lists-fit-negative-control \
	tables-lists-unreached-negative-control \
	tables-lists-cook-check-negative-control \
	tables-maps-cook-check-negative-control \
	tables-lists-tool-cook-negative-control

# ---- THE UNION-ARM TRAVERSAL GATE (docs/SPEC-TABLES.md §2.6, §2.9, §3.1, §7.6) --
#
# One binary over the `tables/arms` corpus: five shapes where a union arm hides
# a pointer or a collection extent (schema#565), the cross of two of them, and
# two where an array-of-pointers arm sits under a list or an array of unions
# (schema#578), each crossed by Measure and Save, LoadMeasure and Load, the
# tool's path, Lock and a dereference after it,
# a memcpy relocation, and a cook from the arena and from the region, opened
# and walked. Its wire goldens are the reference's, pinned like every other
# table golden, and the Go tool reads every wire it writes back into the text
# form and writes it again: the tool re-derives the numbering from the graph
# alone (internal/tablewire), so the bytes agreeing is the two walks numbering
# the arms' nodes alike. The tool's COOK half does not carry a list-bearing
# unit (schema#380), so the cooks are pinned and walked here and not crossed.
#
# THE SANITIZED TWIN rides beside it for the list gate's reason.

ARMS_SOURCES = $$(ls build/tables-generated/arms/*Table.cpp)

build/schema_test_arms: build/tables-generated/.stamp test/tables/arms_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(TABLES_INCLUDES) test/tables/arms_main.cpp $(ARMS_SOURCES) -o $@

build/schema_test_arms_asan: build/tables-generated/.stamp test/tables/arms_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-omit-frame-pointer -g \
		$(TABLES_INCLUDES) test/tables/arms_main.cpp $(ARMS_SOURCES) -o $@

# root table per pinned wire, for the tool's round trip
ARMS_WIRE_ROOTS := arms_ring:Ring arms_holder:Holder arms_nest:Nest arms_hand:Hand arms_chain:Chain arms_gate:Gate arms_gate_text:Gate arms_rack:Rack arms_tray:Tray

.PHONY: tables-arms
tables-arms: build/schema_test_arms build/schema_test_arms_asan
	@rm -rf build/arms-files && mkdir -p build/arms-files
	SCHEMA_ARMS_DIR=build/arms-files ./build/schema_test_arms
	./build/schema_test_arms_asan
	@for pair in $(ARMS_WIRE_ROOTS); do \
		name=$${pair%%:*}; root=$${pair##*:}; \
		rm -rf build/arms-files/$$name-text; \
		./bin/schema unpack --root $$root --in build/arms-files/$$name.bin --one-file build/arms-files/$$name-text tables/arms || exit 1; \
		./bin/schema pack --root $$root --out build/arms-files/$$name-again.bin build/arms-files/$$name-text tables/arms || exit 1; \
		cmp -s build/arms-files/$$name.bin build/arms-files/$$name-again.bin || \
			{ echo "ARMS GATE FAILED: the tool numbered $$name's nodes differently from the reference"; exit 1; }; \
	done
	@echo "arms gate: the tool reads every pinned wire silently and writes it back byte for byte, so the two walks number the arms' nodes alike"

# ---- THE NEGATIVE CONTROLS the arms gate names (schema#565) --------------
#
# One per shape, and one on the cook's extent check. Each names its sabotage
# in tools/sabotage, patches the GENERATOR through a Go overlay, regenerates
# the arms corpus, rebuilds the gate and requires it to go RED on a CHECK the
# clean tree passes, the one that names the shape.
#
# $(1) the sabotage's name, $(2) the compiler source it patches, $(3) the
# sentence a reader gets when the gate stayed green, $(4) flags the sabotaged
# header needs to compile at all, so the control reaches its CHECK.
define arms_negative_control
	@mkdir -p build
	@go run ./tools/sabotage -name $(1) -out build/$(1).gotext $(2)
	@printf '{"Replace":{"%s/$(2)":"%s/build/$(1).gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/$(1)-overlay.json
	@go build -overlay=build/$(1)-overlay.json -o build/schema-$(1) ./cmd/schema
	@rm -rf build/tables-$(1) && mkdir -p build/tables-$(1)
	@./build/schema-$(1) generate --lang cpp --out build/tables-$(1)/arms tables/arms
	@$(CXX) $(TABLES_CXXFLAGS) $(4) -Ibuild/tables-$(1)/arms -Itest/tables test/tables/arms_main.cpp \
		build/tables-$(1)/arms/*Table.cpp -o build/schema_test_$(1)
	@if ./build/schema_test_$(1) > build/$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: $(3)"; exit 1; \
	fi
	@grep -q "^FAIL" build/$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on a CHECK"; cat build/$(1).log; exit 1; }
	@echo "negative control: $(1) turns the ARMS GATE red: $$(grep -c '^FAIL' build/$(1).log) failures"
endef

# a red line the control must carry: $(1) the control, $(2) the pattern
define arms_control_red_on
	@grep -q "$(2)" build/$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: $(1) went red, but not on $(2)"; cat build/$(1).log; exit 1; }
endef

# A LIST OF UNIONS IS NOT AN EDGE (C1): the walk skips `Ring.items`, so the
# node its elements point at is never numbered, and Measure refuses.
.PHONY: tables-arms-list-edge-negative-control
tables-arms-list-edge-negative-control: bin/schema build/tables-generated/.stamp
	$(call arms_negative_control,arms-list-union-edge,internal/codegen/cpptable/pointers.go,a list of unions left out of the walk left the arms gate GREEN)
	$(call arms_control_red_on,arms-list-union-edge,\[arms_ring\] measured > 0)

# THE COOK SKIPS A TABLE ARM'S CONTAINERS (C2): the layout measured the leaf's
# list and the writer laid nothing for it, so the extent check refuses the
# cook before a header is written, on `Holder`.
.PHONY: tables-arms-cook-arm-negative-control
tables-arms-cook-arm-negative-control: bin/schema build/tables-generated/.stamp
	$(call arms_negative_control,arms-cook-skips-arm,internal/codegen/cpptable/extent.go,the cook skipping a table arm's list left the arms gate GREEN)
	$(call arms_control_red_on,arms-cook-skips-arm,\[arms_holder\] S::Cook)

# THE COOK'S EXTENT CHECK IS DROPPED, with the same skip: the cook loses the
# leaf's list and REPORTS SUCCESS, which is what the check exists to refuse,
# and the pinned cook's byte compare is what catches it instead.
.PHONY: tables-arms-cook-check-negative-control
tables-arms-cook-check-negative-control: bin/schema build/tables-generated/.stamp
	$(call arms_negative_control,arms-cook-check-dropped,internal/codegen/cpptable/extent.go,dropping the cook's extent check left the arms gate GREEN)
	$(call arms_control_red_on,arms-cook-check-dropped,table wire golden arms_holder_cook)
	@if grep -q "\[arms_holder\] S::Cook" build/arms-cook-check-dropped.log; then \
		echo "NEGATIVE CONTROL FAILED: the cook still refused with its extent check dropped"; exit 1; \
	fi

# A NESTED UNION HIDES ITS EXTENT (C8): the question "does this arm reach a
# container" stops one level up, so `Nest`'s leaf list stays an arena
# reference after Lock.
.PHONY: tables-arms-nested-union-negative-control
tables-arms-nested-union-negative-control: bin/schema build/tables-generated/.stamp
	$(call arms_negative_control,arms-nested-union-extent,internal/codegen/cpptable/extent.go,a nested union arm's list left out of the extent left the arms gate GREEN)
	$(call arms_control_red_on,arms-nested-union-extent,\[arms_nest\] ref_inside)

# AN ARRAY OF UNIONS IS FRAMED AS ONE ARM HEADER (C9): LoadMeasure omits
# `Hand`'s leaf list, and Load's carve fails on valid wire.
.PHONY: tables-arms-array-framing-negative-control
tables-arms-array-framing-negative-control: bin/schema build/tables-generated/.stamp
	$(call arms_negative_control,arms-array-of-unions-framing,internal/codegen/cpptable/extent.go,framing an array of unions as one arm header left the arms gate GREEN)
	$(call arms_control_red_on,arms-array-of-unions-framing,\[arms_hand\] a loaded region: the report is not silent)

# A POINTER ARM NAMES NO NODE (C10): `Gate`'s Only leaves the reachable set,
# the load reads its own writer's record as unknown, and the arm resolves null.
.PHONY: tables-arms-reachable-negative-control
tables-arms-reachable-negative-control: bin/schema build/tables-generated/.stamp
	$(call arms_negative_control,arms-reachable-arm,ir/table.go,a pointer arm left out of the reachable set left the arms gate GREEN)
	$(call arms_control_red_on,arms-reachable-arm,\[arms_gate\] ref_inside)

# THE ARM'S SLOT LOOP REUSES THE ELEMENT INDEX: under a list or an array of
# unions the arm's `[..N]*T` walk spells its index `i` as the element loop
# does, so the inner `i` shadows the element index, slot k reads element k's
# node, and `Rack`'s second node of its first element is never numbered, so
# Measure refuses. The gate's own `-Wshadow` refuses the header first, so the
# control compiles without it to reach the CHECK.
.PHONY: tables-arms-slot-index-negative-control
tables-arms-slot-index-negative-control: bin/schema build/tables-generated/.stamp
	$(call arms_negative_control,arms-slot-index-shadows,internal/codegen/cpptable/pointers.go,the arm's slot loop shadowing the element index left the arms gate GREEN,-Wno-shadow)
	$(call arms_control_red_on,arms-slot-index-shadows,\[arms_rack\] measured > 0)

.PHONY: tables-arms-negative-controls
tables-arms-negative-controls: tables-arms-list-edge-negative-control \
	tables-arms-cook-arm-negative-control \
	tables-arms-cook-check-negative-control \
	tables-arms-nested-union-negative-control \
	tables-arms-array-framing-negative-control \
	tables-arms-reachable-negative-control \
	tables-arms-slot-index-negative-control

# Re-pin the goldens DELIBERATELY (SPEC §7.2 gates 1, 2, 7). A wire golden
# breaking under an unchanged schema is stop-the-line, never a quiet re-pin
# (SPEC §3.1) — this target is for intentional emitter/schema changes only.
update-goldens: build/schema_test_retain build/schema_test build/schema_test_ludicrous build/schema_test_bench build/schema_test_bench_table build/schema_test_tables build/schema_test_block build/schema_test_maps build/schema_test_lists build/schema_test_arms build/conformance-harness
	@mkdir -p testdata/golden testdata/wire testdata/wire/tables
	go test ./internal/goldens -update -run 'TestGolden'
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_tables
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_block
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_maps
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_lists
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_arms
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_retain
	@for d in examples pointers block blockhome messages stream blobs scalars maps lists arms; do \
		mkdir -p testdata/golden/tables/$$d; \
		cp build/tables-generated/$$d/*Table.h build/tables-generated/$$d/*Table.cpp testdata/golden/tables/$$d/ 2>/dev/null || true; \
	done
	# every leg's committed table goldens (make/<lang>.mk, GOLDENS_LEGS)
	@set -e; for leg in $(GOLDENS_LEGS); do $(MAKE) $$leg; done
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_ludicrous
	SCHEMA_UPDATE_WIRE_GOLDENS=1 ./build/schema_test_bench
	./build/schema_test_bench_table pin
	# Cook-write snapshots carry the build version too; update both byte orders
	# through the engine after the reference wire pins have been regenerated.
	./build/conformance-harness generate
	go test ./...

# the cross-language serialize profiling harness (bench/README.md): builds and
# runs whichever language runners are available, Release flags, results CSV
# under bench/results/
bench:
	bench/run.sh

# LOCKING C AND C++ TOGETHER (owner, 2026-09-07: "So we should now be able to
# lock C and C++ together now in perf and make sure we don't regress").
# The maintainer's pass: a cpp,c A/A twins pass, seven rounds, into
# bench/results/<date>-<arch>-<host>-lock-pass.csv, then the ledger check over
# the whole committed record. Two teeth fire from that one file — the §2.8 tie
# band between the legs, and each leg's own curve against the BEST of the
# previous three points on the SAME machine (absolute rates do not compare
# across machines, §2), so run it on the box those points came from: the
# Studio. Only files carrying BOTH legs are points on that axis.
#
# `--twins` is not optional here. The lock is a claim about two binaries
# measured in one window, and §2.6.1's A/A legs are what rules out
# state-selective interference dressing up as a separation.
#
# WHAT THE LOCK COVERS: bench_mixed / family gen / round_trip / best rate —
# the headline statistic §2.8 rules on, and the whole of the lock. The
# bitpacker rows and the write path are OUTSIDE it. Not an oversight: in
# bench/results/2026-09-07-arm64-studio-bitpacker-checked-read-pass.csv, a
# `window: OK` pass this lock calls green at 106.1%, bitpacker/read has c at
# 161.4% of cpp. A band over that row is a ruling about a different statistic
# and is not invented here — it would need its own §2.8-style paragraph in
# bench/BENCH-STANDARD.md first.
#
# CI does not run this — it reads the committed CSV. A GitHub-hosted runner is
# too noisy for a window the standard would accept (§2.6, §7), so the lock
# lives in the record a maintainer commits, and the gate reads the record.
# bench/README.md, "Locking C and C++ together".
.PHONY: bench-lock
bench-lock:
	bench/tools/pass-driver.sh --twins --rounds 7 --langs cpp,c \
	  --out "bench/results/$$(date +%F)-$$(uname -m)-$$(hostname -s)-lock-pass.csv"
	go run ./bench/tools ledger --check

# The bench_mixed variant data (issue #191): 64 wire buffers the data-driven
# drivers bench, regenerated from bench/corpus/Bench.schema's generated Go
# codec. Deterministic — a regeneration that changes the committed file means
# the shape or the §2.7 LCG mapping moved, and the tool refuses outright if
# variant 0 stops equalling testdata/wire/bench_mixed.bin. Needs the
# serialize.go checkout ($(SERIALIZE_GO)); the committed data's own gate,
# bench/corpus/variants_test.go, needs nothing and runs in `make test`.
bench-variants: generated/bench/go/.stamp
	cd bench/tools/variantgen && go run .

# ---------------------------------------------------------------------------
# THE TABLES BENCH (bench/tables/README.md) ---------------------------------
#
# One measured shape: a representative fixed table written and read on the
# TOLERANT WIRE (docs/SPEC-TABLES.md §3) — the tables layer's per-language
# release gate, and the number a reader who knows protobuf or flatbuffers
# already has a comparison for. Block-form and cook numbers are NOT here: the
# owner's scope ruling on issue #330 keeps those C++/C# and takes them in the
# game on real render data.
#
# The corpus DATA is produced and verified by build/schema_test_bench_table,
# which `make test` runs. `bench-table-corpus` re-pins it: deterministic, so a
# re-pin that changes the committed files means the shape or the vary mapping
# moved — the same stop-the-line rule the wire goldens carry.
bench-table-corpus: build/schema_test_bench_table
	./build/schema_test_bench_table pin

bench-table-check: build/schema_test_bench_table
	./build/schema_test_bench_table verify

# Matched Fixed Table uses BenchMixed itself, reached through the tiny wrapper.
# The wrapper is never serialized; packet storage remains independently built.
generated/bench/paired/cpp/.stamp: bin/schema bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@mkdir -p generated/bench/paired/cpp
	./bin/schema generate --lang cpp --out generated/bench/paired/cpp bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@touch $@

build/schema_test_bench_paired: generated/bench/paired/cpp/.stamp test/bench/paired_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -Igenerated/bench/paired/cpp test/bench/paired_main.cpp -o $@

bench-paired-corpus: build/schema_test_bench_paired
	./build/schema_test_bench_paired pin

bench-paired-check: build/schema_test_bench_paired
	./build/schema_test_bench_paired verify

test: bench-paired-check

bench-paired-gate:
	go run ./bench/paired -mode gate

.PHONY: bench-paired-corpus bench-paired-check bench-paired-gate

# THE FIXED FORM'S MATCHED BYTE GATE (docs/SPEC-TABLES.md §3.4). The two legs
# that carry form 3 each rebuild bench/paired/corpus/bench_fixed.bin from the
# records they loaded out of it and compare the whole file byte for byte, and
# each compares its OWN vocabulary block against the corpus's before anything
# else — the block settles the positions, the ids, the kinds, the record size
# and the hash, so a leg that matches it is speaking the form and not a near
# miss. `--gate` is the runner's no-clock mode: this is a correctness gate and
# starts no timer, so it belongs in `make test` where the measurement above
# does not.
build/schema_test_bench_paired_table_cpp: generated/bench/paired/cpp/.stamp bench/tables/cpp/table_main.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -O2 -DNDEBUG -DBENCH_MATCHED -Igenerated/bench/paired/cpp bench/tables/cpp/table_main.cpp -o $@

# THE THREE WIRES, SIDE BY SIDE (docs/SPEC-TABLES.md §3.4). The paired bench
# above times the PACKET wire and FORM 1 over identical logical records and does
# NOT time form 3 — its C++ runner calls BenchMixedSave/Load and nothing but a
# conformance test calls the fixed codec. This adds the third row, in ONE
# process, over the SAME 64 committed variants, with the same loop structure,
# the same escape barriers and the same warmup-then-median shape, and it times
# form 3 BOTH WAYS a fixed record travels: batched into one file, where the
# layout is paid once, and sent one record at a time, where it rides with every
# one.
#
# IT IS A DIFFERENT MEASUREMENT FROM bench-fixedform-measure BELOW, which is the
# form's RULING measurement — the plan-driven reader against straight-line
# constant-offset loads, the ratio the one-reader-path decision was bought with.
# That one asks whether the plan costs too much; this one asks what the wire is
# worth against the other two. Neither answers the other's question.
#
# IT IS A MEASUREMENT AND NOT A GATE, so it is not in `make test`: it starts a
# clock, and a clock in a test suite is a flaky test. What makes its numbers
# worth reading is that EVERY PATH IS GATED BEFORE ANY CLOCK STARTS — each must
# reproduce its own bytes, and every form-3 record must re-encode to the packet
# bytes it came from, so a path that decoded wrongly does not get to be fast.
#
#   make bench-fixedform-wires                     five runs
#   make bench-fixedform-wires MEASURE_RUNS=7      seven
build/schema_bench_fixedform_wires: generated/bench/paired/cpp/.stamp test/bench/fixedform_wires.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -O2 -DNDEBUG -Igenerated/bench/paired/cpp -I$(SERIALIZE) \
	    test/bench/fixedform_wires.cpp -o $@

MEASURE_RUNS ?= 5

bench-fixedform-wires: build/schema_bench_fixedform_wires
	./build/schema_bench_fixedform_wires $(MEASURE_RUNS)

.PHONY: bench-fixedform-wires

tables-fixed-matched: build/schema_test_bench_paired_table_cpp build/schema_test_bench_paired_c
	./build/schema_test_bench_paired_table_cpp --gate --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus
	./build/schema_test_bench_paired_c --gate --indexed --wire-dir bench/paired/corpus --variant-dir bench/paired/corpus

.PHONY: tables-fixed-matched

test: tables-fixed-matched

# THE PYTHON LEG'S ROW GATE (mas-bandwidth/schema#1400). The python port is one
# matrix row at a time, each row a self-checking script under
# test/py-packet/<row>/test_<row>.py that exits non-zero on the first failed
# assertion. The target runs EVERY such script, so the next row is gated the
# day it lands without a line here, and it goes red if it ran none: a glob that
# matches nothing is a gate that ran nothing (#1315). PYTHONDONTWRITEBYTECODE
# keeps a run from leaving __pycache__ in the tree. The go-test job in
# ci-full.yml runs it (the stream's go-test job; ci.yml is gone), and `make test`
# carries it.
PY_PACKET_ROWS = $(sort $(wildcard test/py-packet/*/test_*.py))

.PHONY: tables-py-packet
tables-py-packet:
	@n=0; for f in $(PY_PACKET_ROWS); do \
		echo "== $$f"; PYTHONDONTWRITEBYTECODE=1 python3 "$$f" || exit 1; n=$$((n+1)); \
	done; \
	if [ $$n -eq 0 ]; then echo "tables-py-packet: no test/py-packet/*/test_*.py ran" >&2; exit 1; fi; \
	echo "tables-py-packet: $$n row(s) passed"

test: tables-py-packet

# Its control: a copy of the leg with F11's plan-capacity check removed
# (python/packet.py's `if entry_count > len(plan):` becomes `if False:`) must
# turn the F11 row red, or the gate above is not watching the rule it names.
.PHONY: tables-py-packet-negative-control
tables-py-packet-negative-control:
	@rm -rf build/py-packet-negative && mkdir -p build/py-packet-negative/python build/py-packet-negative/test/py-packet
	@cp python/packet.py build/py-packet-negative/python/
	@cp -R test/py-packet/f11 build/py-packet-negative/test/py-packet/
	@sed -i.orig 's/if entry_count > len(plan):/if False:/' build/py-packet-negative/python/packet.py
	@if cmp -s python/packet.py build/py-packet-negative/python/packet.py; then \
		echo "tables-py-packet-negative-control: the sabotage patched nothing" >&2; exit 1; fi
	@if PYTHONDONTWRITEBYTECODE=1 python3 build/py-packet-negative/test/py-packet/f11/test_f11.py >build/py-packet-negative/out.txt 2>&1; then \
		cat build/py-packet-negative/out.txt; \
		echo "tables-py-packet-negative-control: F11 stayed green without the plan-capacity check" >&2; exit 1; fi
	@grep -q 'Expected count=-1' build/py-packet-negative/out.txt || { cat build/py-packet-negative/out.txt; \
		echo "tables-py-packet-negative-control: F11 went red for the wrong reason" >&2; exit 1; }
	@echo "tables-py-packet-negative-control: F11 went red without the plan-capacity check (ok)"

# THE C / C++ FIXED RUNTIME TWIN GATE (bench/paired/TWIN.md). Emit both
# paired headers, normalise the fixed runtimes through the documented token
# map, and diff the rest. Any leftover line that is not one of the three
# named differences is a failure. The negative control plants an extra
# enumerator so a map that swallowed every divergence has no blade.
tables-fixed-twin: generated/bench/paired/c/.stamp generated/bench/paired/cpp/.stamp
	go test ./tools/fixedtwin -count=1
	go run ./tools/fixedtwin generated/bench/paired/c/FixedTableTable.h generated/bench/paired/cpp/FixedTableTable.h
	$(MAKE) tables-fixed-twin-negative-control

.PHONY: tables-fixed-twin

test: tables-fixed-twin

.PHONY: tables-fixed-twin-negative-control
tables-fixed-twin-negative-control: generated/bench/paired/c/.stamp generated/bench/paired/cpp/.stamp
	@mkdir -p build/fixed-twin-nc
	@sed 's/BENCH_K_TABLE_FIXED_COPY    = 0/BENCH_K_TABLE_FIXED_COPY    = 99/' generated/bench/paired/c/FixedTableTable.h > build/fixed-twin-nc/FixedTableTable.h
	@cmp -s generated/bench/paired/c/FixedTableTable.h build/fixed-twin-nc/FixedTableTable.h && { echo 'NEGATIVE CONTROL: the planted op patched nothing'; exit 1; } || true
	@if go run ./tools/fixedtwin build/fixed-twin-nc/FixedTableTable.h generated/bench/paired/cpp/FixedTableTable.h; then \
		echo 'NEGATIVE CONTROL FAILED: planted leftover line passed the twin gate'; \
		exit 1; \
	fi
	@echo 'fixed twin negative control: planted leftover line reds'

# THE FIXED FORM'S RULING MEASUREMENT (docs/SPEC-TABLES.md §3.4). One reader
# path is a design decision with a price, and this is the price: the
# plan-driven reader running its identity plan against straight-line
# constant-offset loads written by hand, over the paired unit's own 64
# records. It prints a ratio; the ruling's bound is ~1.5x. Not part of
# `make test` — it is a clock, and clocks do not gate a build.
build/schema_bench_fixedform: generated/bench/paired/cpp/.stamp test/bench/fixedform_measure.cpp
	@mkdir -p build
	$(CXX) $(CXXFLAGS) -O2 -Igenerated/bench/paired/cpp test/bench/fixedform_measure.cpp -o $@

bench-fixedform-measure: build/schema_bench_fixedform
	./build/schema_bench_fixedform 4000 9

.PHONY: bench-fixedform-measure


# Prove the COMMITTED generated/ tree matches what the current compiler emits
# (issue #30, tightened by #898's gate G3). It used to regenerate in place and
# read `git diff`: in place a rule only writes and never removes, so a file the
# emitter STOPPED writing stayed on disk byte-identical to the index and the
# diff was silent about it — and a rule that never ran at all left its whole
# directory standing and was called green. So the tree is moved aside and the
# compiler emits into NOTHING; the full listing and the bytes are compared both
# ways, with every failing file named. test/generated-tree/verify says how, the
# `generated` job in ci-full.yml and the certify workflow run that same script,
# and it no longer needs the whole `make test` chain to get there: the emission
# takes bin/schema and nothing else.
generated-current: bin/schema build/treelock
	@test/generated-tree/verify

# The link-and-citation gate (schema#337): every relative markdown link in the
# tree resolves to a file and, where anchored, to a heading slug, and every
# "PAGE.md §N.M" citation resolves to a numbered heading in that page. It was
# scratch when docs/ moved (#336); internal/docgate is the standing form, and
# `go test ./...` runs it too.
.PHONY: docs-check
docs-check:
	go test ./internal/docgate -run TestPagesResolve -count=1

# bench/corpus holds two units (one package per unit, SPEC §3.2), so the
# corpus commands name each unit's file rather than the directory
check: bin/schema docs-check
	./bin/schema check examples
	./bin/schema check examples128
	./bin/schema check tables/examples
	./bin/schema check tables/pointers
	./bin/schema check tables/block
	./bin/schema check tables/blockhome
	./bin/schema check tables/messages
	./bin/schema check tables/stream
	./bin/schema check tables/blobs
	./bin/schema check tables/scalars
	./bin/schema check test/tables/V1.schema
	./bin/schema check test/tables/V2.schema
	./bin/schema check test/tables/P1.schema
	./bin/schema check test/tables/P2.schema
	./bin/schema check test/tables/P3.schema
	./bin/schema check test/tables/M1.schema
	./bin/schema check test/tables/M2.schema
	./bin/schema check test/tables/F1.schema
	./bin/schema check test/tables/F2.schema
	./bin/schema check test/tables/L1.schema
	./bin/schema check bench/corpus/Bench.schema
	./bin/schema check bench/corpus/RealWorld.schema
	./bin/schema check bench/corpus/BenchTable.schema
	./bin/schema check bench/corpus/Bench.schema bench/corpus/FixedTable.schema

id: bin/schema
	./bin/schema id examples
	./bin/schema id examples128
	./bin/schema id bench/corpus/Bench.schema
	./bin/schema id bench/corpus/RealWorld.schema

fmt: bin/schema
	./bin/schema fmt examples
	./bin/schema fmt examples128
	./bin/schema fmt tables/examples
	./bin/schema fmt tables/pointers
	./bin/schema fmt tables/block
	./bin/schema fmt tables/blockhome
	./bin/schema fmt tables/messages
	./bin/schema fmt tables/stream
	./bin/schema fmt tables/blobs
	./bin/schema fmt tables/scalars
	./bin/schema fmt test/tables/V1.schema
	./bin/schema fmt test/tables/V2.schema
	./bin/schema fmt test/tables/P1.schema
	./bin/schema fmt test/tables/P2.schema
	./bin/schema fmt test/tables/P3.schema
	./bin/schema fmt test/tables/M1.schema
	./bin/schema fmt test/tables/M2.schema
	./bin/schema fmt test/tables/F1.schema
	./bin/schema fmt test/tables/F2.schema
	./bin/schema fmt test/tables/L1.schema
	./bin/schema fmt bench/corpus/Bench.schema
	./bin/schema fmt bench/corpus/RealWorld.schema
	./bin/schema fmt bench/corpus/BenchTable.schema
	./bin/schema fmt bench/corpus/Bench.schema bench/corpus/FixedTable.schema

# The one-benchmark rule, made mechanical: no hand-coded measurement of a
# schema shape anywhere in this repo except what a SHAPE-GATE.allow names —
# bench/SHAPE-GATE.allow for shared tooling, one beside each leg for its own.
shape-gate:
	go run ./bench/tools/shapegate

clean:
	rm -rf bin build generated

.PHONY: all test check id fmt clean update-goldens bench bench-variants bench-tables bench-table-corpus bench-table-check generated-current shape-gate

# ---------------------------------------------------------------------------
# THE TABLES CONFORMANCE HARNESS (test/conformance/README.md) ----------------
#
# A port of the tables layer is "make the driver pass". The DATA lives under
# testdata/conformance/tables and names no language; the CONTRACT lives in
# test/conformance/README.md; the harness runs every registered driver over
# every surface and prints the matrix. Registering a port is its driver at
# test/conformance/<lang>/driver, which the harness discovers.
#
# The rule this target lives under is the two-minute one (#320). Measured on
# arm64 macOS at the landing, everything already built, median of three:
#
#   all three legs, 260 cases each   10.5 s
#   the cpp leg alone                 0.79 s   (native execs, plus materialising)
#   the cs leg alone                 10.0 s   (`dotnet run` start-ups)
#   the go leg alone                  1.07 s   (one native exec per surface)
#
# The cost is per-PROCESS, not per-case: the C# leg starts a runtime once per
# surface plus once per cook, because test/cs-cook's dump takes one root per
# invocation, and that is where nearly the whole wall is. The Go leg is one
# native exec per surface and no more — its cook and cook-forgery are answered
# in the same binary as everything else — which is the cheapest shape a leg can
# have under this contract. So the budget left for six more languages is most
# of the two minutes, and sharding per language leg, the way the type wire's
# nine legs already are, is what the numbers say to do if that stops holding;
# it is not needed at this size.
CONFORMANCE_INCLUDES := -Ibuild/tables-generated/maps -Ibuild/tables-generated/lists -Ibuild/tables-generated/examples -Ibuild/tables-generated/v1 \
	-Ibuild/tables-generated/v2 -Ibuild/tables-generated/p1 -Ibuild/tables-generated/p3 \
	-Ibuild/tables-generated/block -Ibuild/tables-generated/pointers \
	-Ibuild/tables-generated/p2 -Ibuild/tables-generated/messages -Ibuild/tables-generated/stream \
	-Ibuild/tables-generated/m1 -Ibuild/tables-generated/m2 -Ibuild/tables-generated/a1 -Ibuild/tables-generated/a2 -Ibuild/tables-generated/g1 -Ibuild/tables-generated/k1 -Ibuild/tables-generated/k2 -Ibuild/tables-generated/w1 -Ibuild/tables-generated/w2 -Ibuild/tables-generated/r1 -Ibuild/tables-generated/r2 -Ibuild/tables-generated/blobs -Itest/tables -Ibuild/tables-generated/scalars -Ibuild/tables-generated/scalars2 -Ibuild/tables-generated/backend -Ibuild/tables-generated/vocab -Ibuild/tables-generated/vocab9 -Ibuild/tables-generated/arms -Ibuild/tables-generated/rt1 -I$(SERIALIZE)
CONFORMANCE_SOURCES = $(wildcard build/tables-generated/maps/*Table.cpp) $(wildcard build/tables-generated/lists/*Table.cpp) build/tables-generated/examples/TablesTable.cpp \
	build/tables-generated/w1/W1Table.cpp build/tables-generated/w2/W2Table.cpp \
	build/tables-generated/r1/R1Table.cpp build/tables-generated/r2/R2Table.cpp \
	build/tables-generated/scalars/ScalarsTable.cpp build/tables-generated/scalars2/Scalars2Table.cpp \
	build/tables-generated/examples/WideTable.cpp build/tables-generated/examples/NestedTable.cpp \
	build/tables-generated/examples/KeyedTable.cpp build/tables-generated/examples/PackTable.cpp build/tables-generated/v1/V1Table.cpp \
	build/tables-generated/v2/V2Table.cpp build/tables-generated/p1/P1Table.cpp \
	build/tables-generated/p2/P2Table.cpp build/tables-generated/p3/P3Table.cpp build/tables-generated/block/RenderBlock.cpp \
	build/tables-generated/block/PaddedBlock.cpp build/tables-generated/pointers/GraphTable.cpp \
	build/tables-generated/pointers/MarksTable.cpp build/tables-generated/pointers/PartsTable.cpp \
	build/tables-generated/messages/MessagesTable.cpp build/tables-generated/stream/StreamTable.cpp \
	build/tables-generated/blobs/AssetsTable.cpp \
	build/tables-generated/m1/M1Table.cpp build/tables-generated/m2/M2Table.cpp \
	build/tables-generated/a1/A1Table.cpp build/tables-generated/a2/A2Table.cpp \
	build/tables-generated/k1/K1Table.cpp build/tables-generated/k2/K2Table.cpp \
	build/tables-generated/g1/G1Table.cpp \
	build/tables-generated/backend/BackendTable.cpp build/tables-generated/vocab/VocabTable.cpp build/tables-generated/vocab9/Vocab9Table.cpp

# the harness LINKS the compiler's own engine — internal/tablewire and
# internal/tabletext, reached through compiler/ — so its dependencies are the
# compiler's sources as well as its own. Without that, an edit to the engine
# left a stale binary generating the data every other gate then compares.
build/conformance-harness: $(wildcard test/conformance/harness/*.go) $(GO_SOURCES)
	@mkdir -p build
	go build -o $@ ./test/conformance/harness

build/conformance-cpp: build/tables-generated/.stamp test/conformance/cpp/main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) test/conformance/cpp/main.cpp \
		$(CONFORMANCE_SOURCES) -o $@

# ---- THE WIRE FUZZER (docs/SPEC-TABLES.md §4.2; docs/SECURITY.md) -------------
#
# A table read is untrusted input, so the tolerant read IS the verifier, and
# this is the gate on that claim for the C++ reference: `harness wire-fuzz`
# mutates every pinned wire in the corpus, feeds each mutant to the leg on a
# pipe and to the compiler's own engine, and requires the same report, the
# same decoded value and a LoadMeasure inside the framing's bound, for every
# mutant. The leg runs twice — plain, and under ASan and UBSan — because the
# two see different heaps: the plain build is the divergence oracle at speed,
# and the sanitized one turns a one-byte over-read into a finding on the
# mutant that caused it. SEED and N size the random pass; the enumerated
# passes run whatever N is.
build/wire-fuzz-cpp: build/tables-generated/.stamp test/tables/wire_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -O1 $(CONFORMANCE_INCLUDES) test/tables/wire_fuzz_main.cpp \
		$(CONFORMANCE_SOURCES) -o $@

build/wire-fuzz-cpp-asan: build/tables-generated/.stamp test/tables/wire_fuzz_main.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -O1 -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g $(CONFORMANCE_INCLUDES) test/tables/wire_fuzz_main.cpp \
		$(CONFORMANCE_SOURCES) -o $@

# THE ID TABLE'S DISTINCTNESS GATE (docs/SPEC-TABLES.md §3): a table that
# carries one id twice is malformed, and TableOpen answers that question TWO
# ways — a hashed PROBE over a stack array up to kTableProbeBound entries, the
# pairwise walk above it. The property is that a READER CANNOT TELL WHICH ONE
# RAN, and a corpus cannot say so on its own, because every table this schema
# writes is small and takes the same walk. So this gate builds tables BY HAND,
# drives TableOpen directly and compares every verdict against an independent
# pairwise reference: both sides of the bound, a repeat planted at every
# position, and the ALL-COLLIDING table a hostile writer builds by inverting
# the multiply.
#
# It is built THREE ways because the check is READ SIDE and a read-side check
# never compiles out (§3.4): the debug build, an NDEBUG release build that has
# to reach the same verdict on the same bytes, and a sanitized one, for the
# reason the wire fuzzer carries its own — the probe indexes a stack array.
# The generated header is the whole dependency, which is also what "the codec
# is header-only and names no runtime" means when a leg says it.
.PHONY: tables-distinctness
tables-distinctness: build/tables-generated/.stamp test/tables/distinct_main.cpp
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/examples \
		test/tables/distinct_main.cpp -o build/schema_test_distinct
	./build/schema_test_distinct
	$(CXX) $(TABLES_CXXFLAGS) -DNDEBUG -O2 -Ibuild/tables-generated/examples \
		test/tables/distinct_main.cpp -o build/schema_test_distinct_ndebug
	./build/schema_test_distinct_ndebug
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
		-fno-omit-frame-pointer -g -Ibuild/tables-generated/examples \
		test/tables/distinct_main.cpp -o build/schema_test_distinct_asan
	./build/schema_test_distinct_asan
	@echo "distinctness gate: one verdict across both walks, debug, release and sanitized"

# ITS NEGATIVE CONTROL. A gate over a check that is correct TWICE cannot be
# read for which walk earned the green: the pairwise walk alone would answer
# every case in this test correctly if the bound never sent anything down the
# fast path. So this removes the PROBE's comparison from a copy of the emitter,
# leaves the pairwise walk untouched, and requires the gate to go red ON THE
# PROBE'S OWN CASES — which is also the standing proof that the bound is live.
.PHONY: tables-distinctness-negative-control
tables-distinctness-negative-control: bin/schema test/tables/distinct_main.cpp
	@rm -rf build/distinct-nc && mkdir -p build/distinct-nc
	@sed -e 's|if ( seen\[s\] == id ) { return TableOpenDamaged; }|if ( false \&\& seen[s] == id ) { return TableOpenDamaged; } // SABOTAGED|' \
		internal/codegen/cpptable/cpptable.go > build/distinct-nc/cpptable.go.txt
	@cmp -s build/distinct-nc/cpptable.go.txt internal/codegen/cpptable/cpptable.go && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/distinct-nc/cpptable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/distinct-nc/overlay.json
	go build -overlay=build/distinct-nc/overlay.json -o build/distinct-nc/schema ./cmd/schema
	./build/distinct-nc/schema generate --lang cpp --out build/distinct-nc/examples tables/examples
	@grep -q SABOTAGED build/distinct-nc/examples/TablesTable.h || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter did not reach the header"; exit 1; }
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/distinct-nc/examples \
		test/tables/distinct_main.cpp -o build/distinct-nc/schema_test_distinct
	@if ./build/distinct-nc/schema_test_distinct > build/distinct-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the gate stayed green with the probe's comparison removed"; exit 1; \
	fi
	@grep -q "^FAIL probe:" build/distinct-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on a probe case"; cat build/distinct-nc/log; exit 1; }
	@echo "negative control: removing the probe's comparison turns the distinctness gate RED on the probe's own cases"

.PHONY: tables-wire-fuzz
tables-wire-fuzz: build/conformance-harness build/wire-fuzz-cpp build/wire-fuzz-cpp-asan
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-cpp --seed $(SEED) --n $(N)
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-cpp-asan --seed $(SEED) --n $(N) \
		--failed build/wire-fuzz/failed-asan.bin

# THE WIRE FUZZER'S NEGATIVE CONTROLS (docs/SPEC-TABLES.md §4.2). A fuzzer
# that has never gone red proves nothing about the reader it points at, so
# each control removes ONE check from the EMITTER — through `go build
# -overlay`, so no tracked file moves — regenerates the corpus from the
# sabotaged compiler, builds the same leg against it, and requires the fuzzer
# to go red on the verdict that check guards. Two checks, because the tolerant
# reader and the variable-class loader are two readers: a length check in the
# string read, and the node-index range check in the numbering's resolve.
#
# $(1) the control's name  $(2) the emitter file  $(3) the sed program
# $(4) the verdict the fuzzer must print
define wire_fuzz_control
	@rm -rf build/wire-fuzz-nc-$(1) && mkdir -p build/wire-fuzz-nc-$(1)
	@sed -e '$(3)' $(2) > build/wire-fuzz-nc-$(1)/emitter.go.txt
	@cmp -s $(2) build/wire-fuzz-nc-$(1)/emitter.go.txt && \
		{ echo "NEGATIVE CONTROL: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/$(2)":"%s/build/wire-fuzz-nc-$(1)/emitter.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/wire-fuzz-nc-$(1)/overlay.json
	go build -overlay build/wire-fuzz-nc-$(1)/overlay.json -o build/wire-fuzz-nc-$(1)/schema ./cmd/schema
	$(call tables_generate,./build/wire-fuzz-nc-$(1)/schema,build/wire-fuzz-nc-$(1)/generated)
	$(CXX) $(TABLES_CXXFLAGS) -O1 $(call tables_includes,build/wire-fuzz-nc-$(1)/generated) \
		test/tables/wire_fuzz_main.cpp \
		$(subst build/tables-generated/,build/wire-fuzz-nc-$(1)/generated/,$(CONFORMANCE_SOURCES)) \
		-o build/wire-fuzz-nc-$(1)/leg
	@if ./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-nc-$(1)/leg --seed $(SEED) --n $(N) \
			--failed build/wire-fuzz-nc-$(1)/failed.bin > build/wire-fuzz-nc-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) check is gone and the wire fuzzer stayed green"; \
		cat build/wire-fuzz-nc-$(1)/log; exit 1; \
	fi
	@grep -q "$(4)" build/wire-fuzz-nc-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the wire fuzzer went red, but not on the $(1) check"; \
		  cat build/wire-fuzz-nc-$(1)/log; exit 1; }
	@grep -m1 "FAILED" build/wire-fuzz-nc-$(1)/log
	@echo "negative control: removing the $(1) check from the emitter turns the wire fuzzer RED"
endef

# THE ORDINAL SLOT CACHE'S OWN CONTROL (docs/SPEC-TABLES.md §3). Every id a
# generated field header names is a compile-time constant, so it carries an
# ORDINAL and TableIds::ref_at answers a repeat out of slot[ordinal]. What
# keeps that honest is truncate: an ELIDED field interns its ids and gives them
# back, and a slot left standing over a POPPED entry hands out a reference to
# an entry the file no longer carries. Remove that one clause and
# compiler/tablerefordinal_test.go's byte pin — taken from the emitter that had
# no cache — must go RED. Through `go build -overlay`, so no tracked file moves.
.PHONY: tables-ref-ordinal-negative-control
tables-ref-ordinal-negative-control:
	@rm -rf build/ref-ordinal-nc && mkdir -p build/ref-ordinal-nc
	@sed -e 's|if ( o >= 0 ) { slot\[o\] = -1; }|if ( false \&\& o >= 0 ) { slot[o] = -1; } // NEGATIVE CONTROL: the slot is not taken back|' \
		internal/codegen/cpptable/cpptable.go > build/ref-ordinal-nc/emitter.go.txt
	@cmp -s internal/codegen/cpptable/cpptable.go build/ref-ordinal-nc/emitter.go.txt && \
		{ echo "NEGATIVE CONTROL: the truncate sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/ref-ordinal-nc/emitter.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/ref-ordinal-nc/overlay.json
	@if SCHEMA_SLOW=1 go test -v -overlay build/ref-ordinal-nc/overlay.json -count=1 ./compiler \
			-run TestCppTableRefOrdinalBytes > build/ref-ordinal-nc/log 2>&1; then \
		if grep -q -- '--- SKIP' build/ref-ordinal-nc/log || \
				! grep -q -- '--- PASS' build/ref-ordinal-nc/log; then \
			echo "NEGATIVE CONTROL FAILED: the byte pin did not run (skipped), so this control is watching nothing"; \
			cat build/ref-ordinal-nc/log; exit 1; \
		fi; \
		echo "NEGATIVE CONTROL FAILED: truncate leaves the ordinal slot standing and the byte pin stayed green"; \
		cat build/ref-ordinal-nc/log; exit 1; \
	fi
	@grep -qE "ROUND TRIP MOVED|THE SAVED BYTES MOVED" build/ref-ordinal-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the byte pin went red, but not on the file the save wrote"; \
		  cat build/ref-ordinal-nc/log; exit 1; }
	@grep -m1 -E "ROUND TRIP MOVED|THE SAVED BYTES MOVED" build/ref-ordinal-nc/log
	@echo "negative control: a truncate that leaves the ordinal slot standing names a POPPED entry — the file stops round-tripping"

# THE SECOND HALF OF THE SAME RULE. An id can be interned by the GENERAL path
# first, so the ref_at that follows finds an existing entry. The shared-id
# driver runs the emitted enum helper and a test-local copy that restores its
# shared key to generic ref; enums now use their known ordinals in production.
# Both copies use the same schema and values. Record the ordinal on the miss
# alone and truncate has nothing to clear in the generic-key copy when that
# slot elides: the next slot's field header answers out of a stale cache and
# names the entry the popped one was replaced by, which is the following KEY.
# The file stays self-consistent and the value is silently gone, so
# compiler/tablerefordinal_test.go's generic-key driver must go RED.
.PHONY: tables-ref-ordinal-shared-negative-control
tables-ref-ordinal-shared-negative-control:
	@rm -rf build/ref-ordinal-shared-nc && mkdir -p build/ref-ordinal-shared-nc
	@sed -e 's|const uint64_t r = ref( id );|const int32_t nc_before = count; const uint64_t r = ref( id );|; s|ordinal_of\[ int32_t( r ) - 1 \] = ` + ordinalType + `( ordinal );|if ( count != nc_before ) { ordinal_of[ int32_t( r ) - 1 ] = ` + ordinalType + `( ordinal ); } // NEGATIVE CONTROL: only a fresh append records its ordinal|' \
		internal/codegen/cpptable/cpptable.go > build/ref-ordinal-shared-nc/emitter.go.txt
	@cmp -s internal/codegen/cpptable/cpptable.go build/ref-ordinal-shared-nc/emitter.go.txt && \
		{ echo "NEGATIVE CONTROL: the hit-path sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/ref-ordinal-shared-nc/emitter.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/ref-ordinal-shared-nc/overlay.json
	@if SCHEMA_SLOW=1 go test -v -overlay build/ref-ordinal-shared-nc/overlay.json -count=1 ./compiler \
			-run TestCppTableRefOrdinalSharedId > build/ref-ordinal-shared-nc/log 2>&1; then \
		if grep -q -- '--- SKIP' build/ref-ordinal-shared-nc/log || \
				! grep -q -- '--- PASS' build/ref-ordinal-shared-nc/log; then \
			echo "NEGATIVE CONTROL FAILED: the shared-id driver did not run (skipped), so this control is watching nothing"; \
			cat build/ref-ordinal-shared-nc/log; exit 1; \
		fi; \
		echo "NEGATIVE CONTROL FAILED: only the miss path records the ordinal and the shared-id driver stayed green"; \
		cat build/ref-ordinal-shared-nc/log; exit 1; \
	fi
	@grep -q "not undone by truncate" build/ref-ordinal-shared-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the driver went red, but not on the shared id"; \
		  cat build/ref-ordinal-shared-nc/log; exit 1; }
	@grep -m1 "VALUES MOVED" build/ref-ordinal-shared-nc/log
	@echo "negative control: an ordinal recorded on the MISS alone loses the field an eliding generic-key slot shares its id with"

# THE CPP NAME-CLAIM NEGATIVE CONTROL (docs/SPEC-TABLES.md §11, schema #414).
# Every namespace-scope spelling of the C++ table runtime is registered in
# internal/tablenames for the Cpp backend, and compiler's
# TestCppTableRuntimeNamesAreClaimed holds that both ways over the emitter's
# output. This plants one more unregistered struct in the runtime through `go
# test -overlay` and requires that test to go RED naming it. No tracked file is
# written to.
.PHONY: tables-cpp-names-negative-control
tables-cpp-names-negative-control:
	@rm -rf build/cpp-names-nc && mkdir -p build/cpp-names-nc
	@sed 's|^struct TableWideRange$$|struct TableBogusUnregistered {}\n\nstruct TableWideRange|' \
		internal/codegen/cpptable/cpptable.go > build/cpp-names-nc/cpptable.go.txt
	@cmp -s internal/codegen/cpptable/cpptable.go build/cpp-names-nc/cpptable.go.txt && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage matched nothing — the runtime moved"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cpptable.go":"%s/build/cpp-names-nc/cpptable.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/cpp-names-nc/overlay.json
	@if SCHEMA_SLOW=1 go test -count=1 -overlay build/cpp-names-nc/overlay.json -run TestCppTableRuntimeNamesAreClaimed \
			./compiler > build/cpp-names-nc/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the name-claim test stayed green with an unregistered runtime struct planted"; \
		cat build/cpp-names-nc/log; exit 1; \
	fi
	@grep -q "emits TableBogusUnregistered and internal/tablenames does not register it" build/cpp-names-nc/log || \
		{ echo "NEGATIVE CONTROL FAILED: the test went red for some other reason"; \
		  cat build/cpp-names-nc/log; exit 1; }
	@grep -m1 "TableBogusUnregistered" build/cpp-names-nc/log
	@echo "negative control: one unregistered runtime struct turns the C++ name-claim test RED"

# THE C++ RELEASE GATE: the wire fuzzer at a long random pass, both builds,
# and the retention leg beside it at the same length (docs/SPEC-TABLES.md
# §6.6). certify.yml runs every `tables-<lang>-release` target by name.
#
# THE INT32_MAX BASE64 BEHAVIORAL REPRODUCTION (#746) rides here beside the
# fuzzers: it is the run half of compiler/issue714_test.go's emitted-text pin.
# A 2 GiB input and a ~2.8 GiB output under Clang UBSan cost about twenty
# seconds and do not fit the owner's two-minute per-commit rule, which is why
# the test gates itself on SCHEMA_CERTIFY_INT32_MAX and this target sets it.
.PHONY: tables-cpp-release
tables-cpp-release: tables-cpp-release-battery
	$(MAKE) tables-wire-fuzz N=500000
	$(MAKE) tables-wire-fuzz SEED=2 N=500000
	$(MAKE) tables-wire-fuzz-retain N=500000
	$(MAKE) tables-wire-fuzz-retain SEED=2 N=500000
	SCHEMA_SLOW=1 SCHEMA_CERTIFY_INT32_MAX=1 go test -count=1 -run '^TestIssue746Base64WriterInt32MaxUBSan$$' ./compiler

.PHONY: tables-wire-fuzz-negative-control tables-wire-fuzz-length-negative-control tables-wire-fuzz-index-negative-control tables-wire-fuzz-arm-width-negative-control tables-wire-fuzz-arm-terminator-negative-control tables-wire-fuzz-oracle-negative-control
tables-wire-fuzz-negative-control: tables-wire-fuzz-length-negative-control tables-wire-fuzz-index-negative-control tables-wire-fuzz-arm-width-negative-control tables-wire-fuzz-arm-terminator-negative-control tables-wire-fuzz-oracle-negative-control tables-wire-fuzz-message-text-oracle-negative-control tables-wire-fuzz-message-text-leg-negative-control

# the string read's `room( len )`. THE ONE CONTENT RULE THE WIRE HAS
# (docs/SPEC-TABLES.md §3, §4) reads a kind `12` payload AS IT ARRIVES, over
# the whole of `L` and before the reader's own bound, so `room( len )` is what
# says those bytes are there at all. A sabotaged leg believes a forged `L`: it
# takes the payload over bytes the mutant never carried and steps its cursor by
# a length the body never had, so the fields after it decode out of bytes that
# are not their own and the leg reports counters the oracle does not. A LENGTH
# IS A 64-BIT NUMBER (§3) and every reader of one takes it unsigned, so the
# length this pass plants is 0xFFFFFFFFFFFFFFFF, the largest the wire can spell
# and the one no buffer ever has room for.
tables-wire-fuzz-length-negative-control: build/conformance-harness
	$(call wire_fuzz_control,length,internal/codegen/cpptable/codecs.go,s|if ( !r.getleb( len ) \|\| !r.room( len ) ) { r.report->malformed = true; return false; }|if ( !r.getleb( len ) ) { r.report->malformed = true; return false; } // NEGATIVE CONTROL: the fit check is gone|,the report differs)

# the numbering's `index - 1 >= map.count`: an index past the node table then
# reads a directory entry the region does not hold
tables-wire-fuzz-index-negative-control: build/conformance-harness
	$(call wire_fuzz_control,index,internal/codegen/cpptable/arena.go,s|if ( index - 1 >= (uint64_t) map.count )|if ( false ) // NEGATIVE CONTROL: the range check is gone|,the report differs)

# AN ARM'S `L` IS CHECKED AGAINST ITS KIND'S WIDTH (docs/SPEC-TABLES.md §3):
# the arm header carries the kind, and an `L` that is not that kind's width is
# the arm's own framing damage. The fuzzer moves a fixed-width arm's `L` to
# every width the closed set has and to zero; without the check the arm decodes
# a value out of a payload that is not its own.
tables-wire-fuzz-arm-width-negative-control: build/conformance-harness
	$(call wire_fuzz_control,arm-width,internal/codegen/cpptable/arms.go,s|if ( %s.size != %d ) { %s = %s; r.report->malformed = true|if ( false \&\& %s.size != %d ) { %s = %s; r.report->malformed = true|,differs)

# A BODY'S TERMINATOR IS THE END OF ITS PAYLOAD (§3), for an arm whose payload
# is a body as much as for a kind `13` field: the fuzzer writes the ZERO
# REFERENCE ahead of the payload's last byte, so the body ends inside its own
# `L` and the bytes after it are claimed by no field.
tables-wire-fuzz-arm-terminator-negative-control: build/conformance-harness
	$(call wire_fuzz_control,arm-terminator,internal/codegen/cpptable/arms.go,s|if ( %s.offset != %s.size ) { %s = %s; r.report->malformed = true|if ( false \&\& %s.offset != %s.size ) { %s = %s; r.report->malformed = true|,the decoded value differs)

# THE ORACLE NEVER PANICS, which is what makes it an oracle. The four controls
# above sabotage the C++ EMITTER and ask whether the fuzzer notices a leg that
# lost a check. This one sabotages the ORACLE and asks whether the fuzzer
# notices the engine itself dying on hostile bytes. It is the standing control
# for the pinned vector stream_count_past_body: a body header is read against
# the parent buffer, so a wide count leaves the cursor past the end its own `L`
# set, and the span from cursor to end goes negative. Without the clamp the
# oracle slices with it and panics. The run must go red, on the VECTOR and on
# the panic, and not merely somewhere.
#
# Nothing tracked is written to: the clamp is removed from a COPY and reached
# through a Go build overlay, so an interrupt cannot leave a sabotaged tree.
# The leg is the ordinary one, so this control costs no second C++ build.
ORACLE_NC := build/wire-fuzz-nc-oracle
.PHONY: tables-wire-fuzz-oracle-negative-control
tables-wire-fuzz-oracle-negative-control: build/conformance-harness build/wire-fuzz-cpp
	@rm -rf $(ORACLE_NC) && mkdir -p $(ORACLE_NC)
	@sed -e 's|if end < r.off {|if false { // NEGATIVE CONTROL: the clamp is gone|' \
		internal/tablewire/decode.go > $(ORACLE_NC)/decode.go.txt
	@cmp -s internal/tablewire/decode.go $(ORACLE_NC)/decode.go.txt && \
		{ echo "NEGATIVE CONTROL: the oracle sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablewire/decode.go":"%s/$(ORACLE_NC)/decode.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(ORACLE_NC)/overlay.json
	go build -overlay $(ORACLE_NC)/overlay.json -o $(ORACLE_NC)/harness ./test/conformance/harness
	@if $(ORACLE_NC)/harness wire-fuzz --driver ./build/wire-fuzz-cpp --seed $(SEED) --n 0 \
			--failed $(ORACLE_NC)/failed.bin > $(ORACLE_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the oracle's clamp is gone and the wire fuzzer stayed green"; \
		cat $(ORACLE_NC)/log; exit 1; \
	fi
	@grep -q "the oracle PANICKED" $(ORACLE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the wire fuzzer went red, but not on an oracle panic"; \
		  cat $(ORACLE_NC)/log; exit 1; }
	@grep -q "stream_count_past_body" $(ORACLE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the panic was not on the pinned vector"; \
		  cat $(ORACLE_NC)/log; exit 1; }
	@grep -m1 "FAILED" $(ORACLE_NC)/log
	@echo "negative control: removing the oracle's body-span clamp turns the pinned vector RED"

# DAMAGE DISCARDS THE WHOLE LOAD (Glenn, 2026-10-31; docs/SPEC-TABLES.md §4):
# on a mutant the oracle reads as damaged, the wire fuzzer compares the
# refusal and nothing after it. The control removes the discard from a COPY
# of wirefuzz.go, reached through a Go build overlay, so the re-saved bytes
# and the retention report are compared again on damaged input, and the gate
# TestWireVerdictComparesOnlyTheRefusalOnDamage must go red on exactly that.
# Nothing tracked is written to.
DISCARD_NC := build/wire-fuzz-nc-discard
.PHONY: tables-wire-fuzz-discard-negative-control
tables-wire-fuzz-discard-negative-control:
	@rm -rf $(DISCARD_NC) && mkdir -p $(DISCARD_NC)
	@sed -e 's|discarded := ans.report.Malformed \|\| ans.report.Refused|discarded := false // NEGATIVE CONTROL: damaged loads are compared whole|' \
		test/conformance/harness/wirefuzz.go > $(DISCARD_NC)/wirefuzz.go.txt
	@cmp -s test/conformance/harness/wirefuzz.go $(DISCARD_NC)/wirefuzz.go.txt && \
		{ echo "NEGATIVE CONTROL: the discard sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/test/conformance/harness/wirefuzz.go":"%s/$(DISCARD_NC)/wirefuzz.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(DISCARD_NC)/overlay.json
	@if go test -count=1 -overlay $(DISCARD_NC)/overlay.json -run '^TestWireVerdictComparesOnlyTheRefusalOnDamage$$' \
			./test/conformance/harness > $(DISCARD_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: damaged loads are compared whole and the gate stayed green"; \
		cat $(DISCARD_NC)/log; exit 1; \
	fi
	@grep -q "a leg that refuses the damaged load is red" $(DISCARD_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason"; \
		  cat $(DISCARD_NC)/log; exit 1; }
	@grep -m1 "refuses the damaged load is red" $(DISCARD_NC)/log
	@echo "negative control: comparing a damaged load's saved bytes turns the discard gate RED"

# DAMAGE DISCARDS THE WHOLE LOAD in the C++ reference (docs/SPEC-TABLES.md §4):
# the control removes the discard from a COPY of the emitter's LoadVerdict,
# reached through a Go build overlay, so a fixed root damaged inside a nested
# body hands back what it decoded, and the gate
# TestDamageDiscardsTheWholeLoadCPP must go red on exactly that. Nothing
# tracked is written to.
CPP_DISCARD_NC := build/cpp-discard-nc
.PHONY: tables-cpp-discard-negative-control
tables-cpp-discard-negative-control:
	@rm -rf $(CPP_DISCARD_NC) && mkdir -p $(CPP_DISCARD_NC)
	@sed -e 's|if ( to->malformed ) { %sReset( value ); return TableOpenDamaged; }|if ( false ) { %sReset( value ); return TableOpenDamaged; } // NEGATIVE CONTROL: the discard is gone|' \
		internal/codegen/cpptable/codecs.go > $(CPP_DISCARD_NC)/codecs.go.txt
	@cmp -s internal/codegen/cpptable/codecs.go $(CPP_DISCARD_NC)/codecs.go.txt && \
		{ echo "NEGATIVE CONTROL: the C++ discard sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/codecs.go":"%s/$(CPP_DISCARD_NC)/codecs.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CPP_DISCARD_NC)/overlay.json
	@if go test -count=1 -overlay $(CPP_DISCARD_NC)/overlay.json -run '^TestDamageDiscardsTheWholeLoadCPP$$' \
			./internal/codegen/cpptable > $(CPP_DISCARD_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C++ discard is gone and the gate stayed green"; \
		cat $(CPP_DISCARD_NC)/log; exit 1; \
	fi
	@grep -q "fixed: LoadTolerant answered true on damage" $(CPP_DISCARD_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason"; \
		  cat $(CPP_DISCARD_NC)/log; exit 1; }
	@grep -m1 "LoadTolerant answered true" $(CPP_DISCARD_NC)/log
	@echo "negative control: removing the C++ discard turns the whole-load gate RED"

# The REGION twin of tables-cpp-discard-negative-control: the discard is
# removed from a COPY of the variable-class LoadTolerant in pointers.go, so a
# variable root damaged inside a nested body hands back its region root, and
# TestDamageDiscardsTheWholeLoadCPP must go red on exactly that.
CPP_REGION_DISCARD_NC := build/cpp-region-discard-nc
.PHONY: tables-cpp-region-discard-negative-control
tables-cpp-region-discard-negative-control:
	@rm -rf $(CPP_REGION_DISCARD_NC) && mkdir -p $(CPP_REGION_DISCARD_NC)
	@sed -e 's|if ( out->malformed ) { memset( region, 0, (size_t) region_bytes ); return NULL; }|if ( false ) { memset( region, 0, (size_t) region_bytes ); return NULL; } // NEGATIVE CONTROL: the region discard is gone|' \
		internal/codegen/cpptable/pointers.go > $(CPP_REGION_DISCARD_NC)/pointers.go.txt
	@cmp -s internal/codegen/cpptable/pointers.go $(CPP_REGION_DISCARD_NC)/pointers.go.txt && \
		{ echo "NEGATIVE CONTROL: the C++ region discard sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/codegen/cpptable/pointers.go":"%s/$(CPP_REGION_DISCARD_NC)/pointers.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CPP_REGION_DISCARD_NC)/overlay.json
	@if go test -count=1 -overlay $(CPP_REGION_DISCARD_NC)/overlay.json -run '^TestDamageDiscardsTheWholeLoadCPP$$' \
			./internal/codegen/cpptable > $(CPP_REGION_DISCARD_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C++ region discard is gone and the gate stayed green"; \
		cat $(CPP_REGION_DISCARD_NC)/log; exit 1; \
	fi
	@grep -q "variable: LoadTolerant handed back a root on damage" $(CPP_REGION_DISCARD_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for some other reason"; \
		  cat $(CPP_REGION_DISCARD_NC)/log; exit 1; }
	@grep -m1 "handed back a root" $(CPP_REGION_DISCARD_NC)/log
	@echo "negative control: removing the C++ region discard turns the whole-load gate RED"

# BYTES INSIDE AN ENVELOPE ARE SKIPPED (docs/SPEC-TABLES.md §3, the append
# rule): the standing control for the pinned vector nested_arm_trailing. The
# ORACLE is taught the opposite reading, a nested-union arm whose L runs past
# the whole inner union is malformed, through a Go build overlay on a COPY of
# decode.go, and a replay of that vector must go red on it by name: the leg
# reads the envelope clean and the sabotaged oracle no longer agrees. The
# replay, not a whole run, because the enumerated pass meets an envelope of the
# same shape earlier (message_arms, pass arm #21) and would stop there first.
# Nothing tracked is written to.
NESTED_TRAIL_NC := build/wire-fuzz-nc-nested-arm-trailing
.PHONY: tables-wire-fuzz-nested-arm-trailing-negative-control
tables-wire-fuzz-nested-arm-trailing-negative-control: build/conformance-harness build/wire-fuzz-cpp
	@rm -rf $(NESTED_TRAIL_NC) && mkdir -p $(NESTED_TRAIL_NC)
	@sed -e 's|^\(\t\tok = r.unionCell(&fv.Cell, fv, -1)\)$$|\1; if ok \&\& r.off != n { r.report.Malformed = true; return false } // NEGATIVE CONTROL: the envelope refused|' \
		internal/tablewire/decode.go > $(NESTED_TRAIL_NC)/decode.go.txt
	@cmp -s internal/tablewire/decode.go $(NESTED_TRAIL_NC)/decode.go.txt && \
		{ echo "NEGATIVE CONTROL: the nested-arm sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablewire/decode.go":"%s/$(NESTED_TRAIL_NC)/decode.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(NESTED_TRAIL_NC)/overlay.json
	go build -overlay $(NESTED_TRAIL_NC)/overlay.json -o $(NESTED_TRAIL_NC)/harness ./test/conformance/harness
	@if $(NESTED_TRAIL_NC)/harness wire-fuzz --driver ./build/wire-fuzz-cpp \
			--replay testdata/wire/tables/nested_arm_trailing.bin --unit messagedemo --root ToolMessage \
			--failed $(NESTED_TRAIL_NC)/failed.bin > $(NESTED_TRAIL_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the oracle refuses a nested-union envelope's trailing bytes and the wire fuzzer stayed green"; \
		cat $(NESTED_TRAIL_NC)/log; exit 1; \
	fi
	@grep -q "nested_arm_trailing.bin" $(NESTED_TRAIL_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the wire fuzzer went red, but not on the pinned vector nested_arm_trailing"; \
		  cat $(NESTED_TRAIL_NC)/log; exit 1; }
	@grep -m1 "FAILED" $(NESTED_TRAIL_NC)/log
	@echo "negative control: refusing a nested-union envelope's trailing bytes in the oracle turns the pinned vector RED"

# ---- THE RETENTION LEG (docs/SPEC-TABLES.md §6.6, §4.2) ---------------------
#
# The same mutants through both engines' RETAINING paths, comparing the two
# retention counters beside the six and the saved bytes beside them. The arm's
# roster is the VARIABLE-CLASS FILE ROOTS and nothing else: a fixed-class
# root's LoadRetain is refused by name, and a form-2 SaveRetain refuses by
# name (§3.3).
.PHONY: tables-wire-fuzz-retain
tables-wire-fuzz-retain: build/conformance-harness build/wire-fuzz-cpp build/wire-fuzz-cpp-asan
	./build/conformance-harness wire-fuzz --retain --driver ./build/wire-fuzz-cpp --seed $(SEED) --n $(N)
	./build/conformance-harness wire-fuzz --retain --driver ./build/wire-fuzz-cpp-asan --seed $(SEED) --n $(N) \
		--failed build/wire-fuzz/failed-retain-asan.bin

# TWO NEGATIVE CONTROLS STAND BEHIND THE RETENTION LEG, one per engine,
# because a leg that has never gone red proves nothing about either. Each
# removes ONE check — through `go build -overlay`, so no tracked file moves —
# and requires the leg to go red on the verdict that check guards.
#
# THE ORACLE'S DROP RULE. THE WALK IS AN INTERPRETATION AND ITS VERDICT IS
# STATED (§6.6): a reference above the entry count, a reference at an entry of
# zero, a reference at one of the three reserved ids, a non-canonical length,
# an inner body whose terminator falls short, a nested `L` past its parent, and
# a kind 17 at any depth all DROP THE RECORD. Without the verdict the oracle
# keeps a record the reference drops, and the two retention reports differ on
# the mutant that carried it.
RETAIN_ORACLE_NC := build/wire-fuzz-nc-retain-oracle
.PHONY: tables-wire-fuzz-retain-oracle-negative-control
tables-wire-fuzz-retain-oracle-negative-control: build/conformance-harness build/wire-fuzz-cpp
	@rm -rf $(RETAIN_ORACLE_NC) && mkdir -p $(RETAIN_ORACLE_NC)
	@sed -e 's|if rs.bad \|\| c.off != len(c.buf) {|if c.off != len(c.buf) { // NEGATIVE CONTROL: the walk'"'"'s verdict is ignored|' \
		internal/tablewire/retain.go > $(RETAIN_ORACLE_NC)/retain.go.txt
	@cmp -s internal/tablewire/retain.go $(RETAIN_ORACLE_NC)/retain.go.txt && \
		{ echo "NEGATIVE CONTROL: the retain-oracle sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/internal/tablewire/retain.go":"%s/$(RETAIN_ORACLE_NC)/retain.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(RETAIN_ORACLE_NC)/overlay.json
	go build -overlay $(RETAIN_ORACLE_NC)/overlay.json -o $(RETAIN_ORACLE_NC)/harness ./test/conformance/harness
	@if $(RETAIN_ORACLE_NC)/harness wire-fuzz --retain --driver ./build/wire-fuzz-cpp --seed $(SEED) --n $(N) \
			--failed $(RETAIN_ORACLE_NC)/failed.bin > $(RETAIN_ORACLE_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the oracle's drop rule is gone and the retention leg stayed green"; \
		cat $(RETAIN_ORACLE_NC)/log; exit 1; \
	fi
	@grep -q "the retention report differs" $(RETAIN_ORACLE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the retention leg went red, but not on the two counters"; \
		  cat $(RETAIN_ORACLE_NC)/log; exit 1; }
	@grep -m1 "FAILED" $(RETAIN_ORACLE_NC)/log
	@echo "negative control: removing the oracle's drop rule turns the retention leg RED"

# THE REFERENCE'S `retain_lost` ON AN EXCLUDED CLASS. EVERY EXCLUSION COUNTS
# (§6.6), so a caller that needs to know retention held reads ONE NUMBER and
# never has to reason about the list. `retainLostInline` is the count the
# emitter puts beside `unknown` at every class a wire can carry to the unknown
# arm — an unknown enum variant, an unknown union arm, an unknown keyed slot —
# and without it the reference under-reports every one of them.
.PHONY: tables-wire-fuzz-retain-class-negative-control
tables-wire-fuzz-retain-class-negative-control: build/conformance-harness
	$(call retain_fuzz_control,retain-class,internal/codegen/cpptable/codecs.go,s|return " r.report->retain_lost++;"|return "" // NEGATIVE CONTROL: the class is not counted|,the retention report differs)

# $(1) the control's name  $(2) the emitter file  $(3) the sed program
# $(4) the verdict the leg must print
define retain_fuzz_control
	@rm -rf build/wire-fuzz-nc-$(1) && mkdir -p build/wire-fuzz-nc-$(1)
	@sed -e '$(3)' $(2) > build/wire-fuzz-nc-$(1)/emitter.go.txt
	@cmp -s $(2) build/wire-fuzz-nc-$(1)/emitter.go.txt && \
		{ echo "NEGATIVE CONTROL: the $(1) sabotage patched nothing"; exit 1; } || true
	@printf '{"Replace":{"%s/$(2)":"%s/build/wire-fuzz-nc-$(1)/emitter.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/wire-fuzz-nc-$(1)/overlay.json
	go build -overlay build/wire-fuzz-nc-$(1)/overlay.json -o build/wire-fuzz-nc-$(1)/schema ./cmd/schema
	$(call tables_generate,./build/wire-fuzz-nc-$(1)/schema,build/wire-fuzz-nc-$(1)/generated)
	$(CXX) $(TABLES_CXXFLAGS) -O1 $(call tables_includes,build/wire-fuzz-nc-$(1)/generated) \
		test/tables/wire_fuzz_main.cpp \
		$(subst build/tables-generated/,build/wire-fuzz-nc-$(1)/generated/,$(CONFORMANCE_SOURCES)) \
		-o build/wire-fuzz-nc-$(1)/leg
	@if ./build/conformance-harness wire-fuzz --retain --driver ./build/wire-fuzz-nc-$(1)/leg --seed $(SEED) --n $(N) \
			--failed build/wire-fuzz-nc-$(1)/failed.bin > build/wire-fuzz-nc-$(1)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) check is gone and the retention leg stayed green"; \
		cat build/wire-fuzz-nc-$(1)/log; exit 1; \
	fi
	@grep -q "$(4)" build/wire-fuzz-nc-$(1)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the retention leg went red, but not on the $(1) check"; \
		  cat build/wire-fuzz-nc-$(1)/log; exit 1; }
	@grep -m1 "FAILED" build/wire-fuzz-nc-$(1)/log
	@echo "negative control: removing the $(1) check from the emitter turns the retention leg RED"
endef

.PHONY: tables-wire-fuzz-retain-negative-control
tables-wire-fuzz-retain-negative-control: tables-wire-fuzz-retain-oracle-negative-control tables-wire-fuzz-retain-class-negative-control

# THE WIDE-VOCABULARY UNIT IS GENERATED AND COMMITTED (docs/SPEC-TABLES.md
# §3.3): a hundred and thirty distinct field names typed by hand is a file
# nobody would review, and a golden a generator has to re-derive is not a
# golden. So both are true at once, and this is what holds them together: the
# generator runs into build/ and the committed file must be what it wrote.
#
# THE GENERATOR IS RUN FROM A SCRATCH ROOT, under the committed file's own
# relative path, because the header it writes carries the `--out` it was given:
# a run that wrote somewhere else would differ from the committed file on that
# line whatever else it got right. Under build/vocabgen the path the generator
# sees is the path the committed file has, so `cmp` compares the whole file,
# header included, and the regenerate line in the committed header is checked
# by the same comparison as the tables below it.
.PHONY: tables-vocab-schema
tables-vocab-schema:
	@rm -rf build/vocabgen && mkdir -p build/vocabgen
	cd build/vocabgen && go run ../../test/vocabgen --out tables/vocab/Vocab.schema
	@cmp build/vocabgen/tables/vocab/Vocab.schema tables/vocab/Vocab.schema || \
		{ echo "tables/vocab/Vocab.schema is not what test/vocabgen writes — run: go run ./test/vocabgen --out tables/vocab/Vocab.schema"; exit 1; }
	cd build/vocabgen && go run ../../test/vocabgen --package vocab9demo --tables 20 --out tables/vocab9/Vocab9.schema
	@cmp build/vocabgen/tables/vocab9/Vocab9.schema tables/vocab9/Vocab9.schema || \
		{ echo "tables/vocab9/Vocab9.schema is not what test/vocabgen writes — run: go run ./test/vocabgen --package vocab9demo --tables 20 --out tables/vocab9/Vocab9.schema"; exit 1; }
	@echo "vocabdemo and vocab9demo: the committed schemas are the generator's, byte for byte"

# ---- THE MESSAGE FORM's NEGATIVE CONTROLS (docs/SPEC-TABLES.md §3.3) --------
#
# Every row of the page's test section is a test in test/conformance/harness
# (message_test.go, messagerules_test.go), and a green run cannot be read for
# WHICH rule earned it. So each control below removes exactly one rule from a
# COPY of the compiler's engine (tools/sabotage, applied through a Go overlay
# so the tree is never written) and requires the row's test to go RED. The
# sabotage tool refuses unless its anchor matches exactly once, so an engine
# that drifts fails the control rather than passing it.
#
# $(1) the sabotage's name  $(2) the file it edits  $(3) the test it must redden
define message_form_control
	@mkdir -p build/message-nc
	@go run ./tools/sabotage -name $(1) -out build/message-nc/$(1).gotext $(2)
	@printf '{"Replace":{"%s/$(2)":"%s/build/message-nc/$(1).gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/message-nc/$(1)-overlay.json
	@if go test -count=1 -overlay=build/message-nc/$(1)-overlay.json \
			./test/conformance/harness -run '^$(3)$$' > build/message-nc/$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) sabotage landed and $(3) stayed green"; \
		cat build/message-nc/$(1).log; exit 1; \
	fi
	@grep -q -- "--- FAIL: $(3)" build/message-nc/$(1).log || \
		{ echo "NEGATIVE CONTROL FAILED: the $(1) run went red without $(3) failing"; \
		  cat build/message-nc/$(1).log; exit 1; }
	@echo "negative control ($(1)): $(3) goes red on" \
		"$$(grep -m1 -A1 -- '--- FAIL' build/message-nc/$(1).log | tail -1 | sed 's/^ *//')"
endef

# one line a row: the sabotage, the engine file, the harness test
MESSAGE_FORM_CONTROLS := \
	message-tail-without-node-table:ir/tablewire.go:TestTheTailIsUnconditional \
	message-order-reversed:ir/tablewire.go:TestTheAnnouncementIsTheUnitsOwn \
	message-slot-off-by-one:internal/tablewire/messageencode.go:TestTheTwoFormsRoundTrip \
	message-count-sixteen-bits:internal/tablewire/messageencode.go:TestTheCostRows \
	message-align-between-bodies:internal/tablewire/messageencode.go:TestTheBatch \
	message-no-batch-bound:internal/tablewire/messageencode.go:TestTheFiveAnswers \
	message-string-no-align:internal/tablewire/messageencode.go:TestTheCostRows \
	message-reference-past-entries:internal/tablewire/messagedecode.go:TestAReferenceAtAndAboveTheEntryCount \
	message-read-on-after-damage:internal/tablewire/messagedecode.go:TestDamageIsTerminal \
	message-no-pad-check:internal/tablewire/messagedecode.go:TestThePadAndWhatFollowsIt \
	message-no-capacity-refusal:internal/tablewire/messagedecode.go:TestTheFiveAnswers \
	message-any-sort-names:internal/tablewire/messagedecode.go:TestAReferenceOfTheWrongSort \
	message-any-sort-arms:internal/tablewire/messagedecode.go:TestAReferenceOfTheWrongSort \
	message-offset-past-max-is-damage:internal/tablewire/messagedecode.go:TestARangedOffsetAboveTheSendersMax \
	message-clamp-drops-surplus:internal/tablewire/messagedecode.go:TestAnOverLongArrayOfNonFixedElements \
	message-skip-string-no-align:internal/tablewire/messagedecode.go:TestTheShapesOneRowAKind \
	message-reader-own-width:internal/tablewire/messagedecode.go:TestARangeThatMoved \
	message-reads-own-vocabulary:internal/tablewire/messagedecode.go:TestPerDirectionIndependence \
	message-second-announcement-accepted:internal/tablewire/message.go:TestTheMessageFormRefusesByName \
	message-strict-checks-relaxed:internal/tablewire/message.go:TestTheAnnouncementsTwoStrictChecksAndItsTolerance \
	message-no-entry-bound:internal/tablewire/message.go:TestTheTwoBounds \
	message-no-byte-bound:internal/tablewire/message.go:TestTheTwoBounds \
	message-reserved-in-vocabulary-accepted:internal/tablewire/message.go:TestTheThreeReservedIdsWhereTheyDoNotBelong \
	message-mask-sixty-four:ir/tablemessage.go:TestTheMasksWidth \
	message-list-count-sixteen:ir/tablemessage.go:TestTheCountsTheDataDecide \
	message-bits-unbounded:ir/tablemessage.go:TestAHostileShape \
	message-reference-eight-bits:ir/tablemessage.go:TestAWideVocabulary \
	message-base-zigzag-unsigned:ir/tablemessage.go:TestTheBasesTwoEncodings \
	message-quantize-truncates:ir/tablemessage.go:TestTheQuantizedIndexAcrossTheForms \
	message-dequantize-round-once:ir/tablemessage.go:TestTheQuantizedIndexAcrossTheForms \
	message-refusal-not-terminal:internal/tablewire/message.go:TestARefusedFirstAnnouncement \
	message-width-above-kind:ir/tablemessage.go:TestTheSixFindings \
	message-count-not-offset:internal/tablewire/messageencode.go:TestTheBasesTwoEncodings \
	message-surplus-lands-on-zero:internal/tablewire/messagedecode.go:TestAnOverLongArrayOfNonFixedElements \
	message-wide-reads-raw:internal/tablewire/messagedecode.go:TestTheSixFindings \
	message-narrow-before-clamp:internal/tablewire/messagedecode.go:TestTheSixFindings \
	message-max-above-int32:ir/tablemessage.go:TestAHostileShape \
	message-quantized-index-above-count:internal/tablewire/messagedecode.go:TestTheQuantizedIndexAcrossTheForms \
	message-surplus-walked:internal/tablewire/messagedecode.go:TestAZeroWidthElementUnderAWideCount \
	message-strict-check-refuses:internal/tablewire/message.go:TestTheAnnouncementsTwoStrictChecksAndItsTolerance \
	message-duplicate-entry-accepted:internal/tablewire/message.go:TestAHostileShape \
	message-array-of-text-refused:ir/tablemessage.go:TestAHostileShape \
	message-skipped-variant-unresolved:internal/tablewire/messagedecode.go:TestAReferenceOfTheWrongSort \
	message-text-accepts-ill-formed:internal/tablewire/messagedecode.go:TestTheMessageFormsTextContentRuleAndClamp \
	message-text-clamp-off-boundary:internal/tablewire/messagedecode.go:TestTheMessageFormsTextContentRuleAndClamp

.PHONY: tables-message-form-negative-control
tables-message-form-negative-control: tables-message-form-emitter-negative-control tables-message-form-count-negative-control tables-message-form-text-negative-control
	@for row in $(MESSAGE_FORM_CONTROLS); do \
		name=$${row%%:*}; rest=$${row#*:}; file=$${rest%%:*}; test=$${rest#*:}; \
		$(MAKE) --no-print-directory tables-message-form-one-negative-control \
			SABOTAGE=$$name SABOTAGE_FILE=$$file SABOTAGE_TEST=$$test || exit 1; \
	done

.PHONY: tables-message-form-one-negative-control
tables-message-form-one-negative-control:
	$(call message_form_control,$(SABOTAGE),$(SABOTAGE_FILE),$(SABOTAGE_TEST))

# AND THE SAME IN THE C++ EMITTER, which is the one the reference reads and
# writes. The sabotage is one line of the emitter and the instrument is the
# pinned wire: the golden still LOADS under a moved slot, because the reader
# resolves whatever the writer named, so what goes red is the round trip
# against the corpus's own bytes. $(1) the sabotage, $(2) the emitter file.
define message_form_emitter_control
	@mkdir -p build/message-nc
	@go run ./tools/sabotage -name $(1) -out build/message-nc/$(1).gotext $(2)
	@printf '{"Replace":{"%s/$(2)":"%s/build/message-nc/$(1).gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/message-nc/$(1)-overlay.json
	go build -overlay build/message-nc/$(1)-overlay.json -o build/message-nc/$(1)-schema ./cmd/schema
	@rm -rf build/message-nc/$(1)-generated && mkdir -p build/message-nc/$(1)-generated
	./build/message-nc/$(1)-schema generate --lang cpp --out build/message-nc/$(1)-generated tables/backend
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/message-nc/$(1)-generated -Itest/tables -I$(SERIALIZE) \
		test/tables/message_negative_main.cpp build/message-nc/$(1)-generated/BackendTable.cpp \
		-o build/message-nc/$(1)-control
	@if ./build/message-nc/$(1)-control > build/message-nc/$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) emitter sabotage landed and the pinned message round tripped"; \
		cat build/message-nc/$(1).log; exit 1; \
	fi
	@cat build/message-nc/$(1).log
	@echo "negative control ($(1)): the pinned message goes red"
endef

.PHONY: tables-message-form-emitter-negative-control
tables-message-form-emitter-negative-control: bin/schema test/tables/message_negative_main.cpp build/tables-generated/.stamp
	@mkdir -p build/message-nc
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/backend -Itest/tables -I$(SERIALIZE) \
		test/tables/message_negative_main.cpp build/tables-generated/backend/BackendTable.cpp \
		-o build/message-nc/slot-true
	./build/message-nc/slot-true
	$(call message_form_emitter_control,message-emitter-slot-off-by-one,internal/codegen/cpptable/messagecodec.go)
	$(call message_form_emitter_control,message-emitter-count-sixteen-bits,internal/codegen/cpptable/message.go)

# AND THE COUNT'S OWN WIDTH, which no pinned vector reaches: an array count at
# or above 2^31 narrowed to int32 before it is bounded is negative, passes a
# signed test against the reader's bound untouched, and lands a negative count
# in the caller's storage. Nothing about the body is ill-formed, so the
# instrument is a program that forges the shape and reads the count back.
# $(1) the sabotage, $(2) the emitter file.
define message_form_count_control
	@mkdir -p build/message-nc
	@go run ./tools/sabotage -name $(1) -out build/message-nc/$(1).gotext $(2)
	@printf '{"Replace":{"%s/$(2)":"%s/build/message-nc/$(1).gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/message-nc/$(1)-overlay.json
	go build -overlay build/message-nc/$(1)-overlay.json -o build/message-nc/$(1)-schema ./cmd/schema
	@rm -rf build/message-nc/$(1)-bases && mkdir -p build/message-nc/$(1)-bases
	./build/message-nc/$(1)-schema generate --lang cpp --out build/message-nc/$(1)-bases test/tables/Bases.schema
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/message-nc/$(1)-bases -Itest/tables -I$(SERIALIZE) \
		test/tables/message_count_main.cpp build/message-nc/$(1)-bases/BasesTable.cpp \
		-o build/message-nc/$(1)-control
	@if ./build/message-nc/$(1)-control > build/message-nc/$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) emitter sabotage landed and the wide count still clamped"; \
		cat build/message-nc/$(1).log; exit 1; \
	fi
	@cat build/message-nc/$(1).log
	@echo "negative control ($(1)): a count at or above 2^31 goes red"
endef

.PHONY: tables-message-form-count-negative-control
tables-message-form-count-negative-control: bin/schema test/tables/message_count_main.cpp build/tables-generated/.stamp
	@mkdir -p build/message-nc
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/bases -Itest/tables -I$(SERIALIZE) \
		test/tables/message_count_main.cpp build/tables-generated/bases/BasesTable.cpp \
		-o build/message-nc/count-true
	./build/message-nc/count-true
	$(call message_form_count_control,message-emitter-narrow-count-before-clamp,internal/codegen/cpptable/messageload.go)

# AND THE ONE CONTENT RULE ON A MESSAGE BODY (docs/SPEC-TABLES.md §3.3,
# schema#620): a kind 12 payload that is not well-formed UTF-8, or that carries
# a zero byte, is damage and terminal for the batch, and a clamp cuts at a code
# point boundary. Neither is reachable from a corpus value, because a writer
# produces neither, so the instrument is a batch forged over backenddemo's
# `sku` and a program that reads the report and the stored bytes back. The
# emitter's own copy of each rule is what the two sabotages remove; the ORACLE's
# two ride in MESSAGE_FORM_CONTROLS above. $(1) the sabotage, $(2) the emitter file.
define message_form_text_control
	@mkdir -p build/message-nc
	@go run ./tools/sabotage -name $(1) -out build/message-nc/$(1).gotext $(2)
	@printf '{"Replace":{"%s/$(2)":"%s/build/message-nc/$(1).gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/message-nc/$(1)-overlay.json
	go build -overlay build/message-nc/$(1)-overlay.json -o build/message-nc/$(1)-schema ./cmd/schema
	@rm -rf build/message-nc/$(1)-backend && mkdir -p build/message-nc/$(1)-backend
	./build/message-nc/$(1)-schema generate --lang cpp --out build/message-nc/$(1)-backend tables/backend
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/message-nc/$(1)-backend -Itest/tables -I$(SERIALIZE) \
		test/tables/message_text_main.cpp build/message-nc/$(1)-backend/BackendTable.cpp \
		-o build/message-nc/$(1)-control
	@if ./build/message-nc/$(1)-control > build/message-nc/$(1).log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the $(1) sabotage landed and the message text rows stayed green"; \
		cat build/message-nc/$(1).log; exit 1; \
	fi
	@cat build/message-nc/$(1).log
	@echo "negative control ($(1)): the message form's text rows go red"
endef

# THE MESSAGE FORM IS FIXED TABLES ONLY, IN EVERY BACKEND (docs/SPEC-TABLES.md
# §3.3): compiler/messageformlock_test.go generates one unit with a fixed table
# beside three variable tables through all fifteen backends and refuses any
# line that puts a variable table beside the message form. Its control plants
# one such line in each backend's real output and requires the check to name
# it, and requires the clean output to pass.
.PHONY: message-form-fixed-only-lock-negative-control
message-form-fixed-only-lock-negative-control:
	go test -count=1 ./compiler -run '^TestNoVariableTableMessagePathNegativeControl$$'

.PHONY: tables-message-form-text-negative-control
tables-message-form-text-negative-control: bin/schema test/tables/message_text_main.cpp build/tables-generated/.stamp
	@mkdir -p build/message-nc
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/backend -Itest/tables -I$(SERIALIZE) \
		test/tables/message_text_main.cpp build/tables-generated/backend/BackendTable.cpp \
		-o build/message-nc/text-true
	./build/message-nc/text-true
	$(call message_form_text_control,message-emitter-text-accepts-ill-formed,internal/codegen/cpptable/messageload.go)
	$(call message_form_text_control,message-emitter-text-clamp-off-boundary,internal/codegen/cpptable/messageload.go)

# ILL-FORMED TEXT ON A MESSAGE BODY (docs/SPEC-TABLES.md §3, §3.3; schema#620),
# and the vector message_ill_formed_text is what holds the two engines to one
# answer on it. The four blades in tables-message-form-negative-control hold
# each engine to the PAGE; these two hold the engines to EACH OTHER, which is a
# different question and the one a differential fuzzer exists to ask: before the
# repair both readers accepted the vector, agreeing and agreeing wrongly, so
# nothing here could have gone red. Each control repairs one engine and takes
# the rule back out of the other, and the run must go red ON THE VECTOR.
#
# THE ASSERTION IS THE VECTOR REPLAYED ALONE, not a corpus pass, for the reason
# the node-type control names: an enumerated mutant reaches the same check and
# would name itself instead of the property.
MESSAGE_TEXT_VECTOR := testdata/wire/tables/fuzz-vectors/message_ill_formed_text.bin

MESSAGE_TEXT_ORACLE_NC := build/wire-fuzz-nc-message-text-oracle
.PHONY: tables-wire-fuzz-message-text-oracle-negative-control
tables-wire-fuzz-message-text-oracle-negative-control: build/conformance-harness build/wire-fuzz-cpp
	@rm -rf $(MESSAGE_TEXT_ORACLE_NC) && mkdir -p $(MESSAGE_TEXT_ORACLE_NC)
	@go run ./tools/sabotage -name message-text-accepts-ill-formed \
		-out $(MESSAGE_TEXT_ORACLE_NC)/messagedecode.go.txt internal/tablewire/messagedecode.go
	@printf '{"Replace":{"%s/internal/tablewire/messagedecode.go":"%s/$(MESSAGE_TEXT_ORACLE_NC)/messagedecode.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(MESSAGE_TEXT_ORACLE_NC)/overlay.json
	go build -overlay $(MESSAGE_TEXT_ORACLE_NC)/overlay.json -o $(MESSAGE_TEXT_ORACLE_NC)/harness ./test/conformance/harness
	@if $(MESSAGE_TEXT_ORACLE_NC)/harness wire-fuzz --driver ./build/wire-fuzz-cpp \
			--replay $(MESSAGE_TEXT_VECTOR) --unit backenddemo --root StorePurchase --message \
			--failed $(MESSAGE_TEXT_ORACLE_NC)/failed.bin > $(MESSAGE_TEXT_ORACLE_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the oracle accepts ill-formed message text again and the pinned vector stayed green"; \
		cat $(MESSAGE_TEXT_ORACLE_NC)/log; exit 1; \
	fi
	@grep -q "message_ill_formed_text" $(MESSAGE_TEXT_ORACLE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the wire fuzzer went red, but not on the pinned vector"; \
		  cat $(MESSAGE_TEXT_ORACLE_NC)/log; exit 1; }
	@grep -m1 "FAILED" $(MESSAGE_TEXT_ORACLE_NC)/log
	@echo "negative control: an oracle that accepts ill-formed message text turns the pinned vector RED"

MESSAGE_TEXT_LEG_NC := build/wire-fuzz-nc-message-text-leg
.PHONY: tables-wire-fuzz-message-text-leg-negative-control
tables-wire-fuzz-message-text-leg-negative-control: build/conformance-harness
	@rm -rf $(MESSAGE_TEXT_LEG_NC) && mkdir -p $(MESSAGE_TEXT_LEG_NC)
	@go run ./tools/sabotage -name message-emitter-text-accepts-ill-formed \
		-out $(MESSAGE_TEXT_LEG_NC)/messageload.go.txt internal/codegen/cpptable/messageload.go
	@printf '{"Replace":{"%s/internal/codegen/cpptable/messageload.go":"%s/$(MESSAGE_TEXT_LEG_NC)/messageload.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(MESSAGE_TEXT_LEG_NC)/overlay.json
	go build -overlay $(MESSAGE_TEXT_LEG_NC)/overlay.json -o $(MESSAGE_TEXT_LEG_NC)/schema ./cmd/schema
	$(call tables_generate,./$(MESSAGE_TEXT_LEG_NC)/schema,$(MESSAGE_TEXT_LEG_NC)/generated)
	$(CXX) $(TABLES_CXXFLAGS) -O1 $(call tables_includes,$(MESSAGE_TEXT_LEG_NC)/generated) \
		test/tables/wire_fuzz_main.cpp \
		$(subst build/tables-generated/,$(MESSAGE_TEXT_LEG_NC)/generated/,$(CONFORMANCE_SOURCES)) \
		-o $(MESSAGE_TEXT_LEG_NC)/leg
	@if ./build/conformance-harness wire-fuzz --driver $(MESSAGE_TEXT_LEG_NC)/leg \
			--replay $(MESSAGE_TEXT_VECTOR) --unit backenddemo --root StorePurchase --message \
			--failed $(MESSAGE_TEXT_LEG_NC)/failed.bin > $(MESSAGE_TEXT_LEG_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the emitted reader accepts ill-formed message text again and the pinned vector stayed green"; \
		cat $(MESSAGE_TEXT_LEG_NC)/log; exit 1; \
	fi
	@grep -q "message_ill_formed_text" $(MESSAGE_TEXT_LEG_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the wire fuzzer went red, but not on the pinned vector"; \
		  cat $(MESSAGE_TEXT_LEG_NC)/log; exit 1; }
	@grep -m1 "FAILED" $(MESSAGE_TEXT_LEG_NC)/log
	@echo "negative control: an emitted reader that accepts ill-formed message text turns the pinned vector RED"

# The GENERATED half of the data: the JSON text of every instance and the read
# report of every evolution case, both from the compiler's own engine.
.PHONY: conformance-generate
conformance-generate: build/conformance-harness
	./build/conformance-harness generate

# The PINNED half: the cook's canonical node dump and the block forgery battery
# resolved to byte offsets, both from the reference leg — C++ writes the pins,
# every other leg compares, exactly as the wire goldens work.
.PHONY: conformance-pin
conformance-pin: build/conformance-harness build/conformance-cpp build/schema_test_cook
	./build/conformance-harness pin

# THE NEGATIVE CONTROL, and a harness that has never gone red is watching
# nothing. One byte of ONE dump is flipped in a COPY of the C++ driver — no
# tracked file is written to, so an interrupt cannot leave a sabotaged working
# tree — and the harness must go red, on that surface and on no other. The
# second half is the point: a matrix whose every cell went red would be saying
# "something broke" rather than "the text form broke".
.PHONY: conformance-negative-control
conformance-negative-control: build/conformance-harness build/conformance-cpp
	@rm -rf build/conformance-negative && mkdir -p build/conformance-negative
	@sed 's|if ( !spill( out, f\[1\] + ".json", text.data(), (size_t) size ) ) return 1;|text[0] = (char) ( text[0] ^ 1 ); if ( !spill( out, f[1] + ".json", text.data(), (size_t) size ) ) return 1; // SABOTAGED|' \
		test/conformance/cpp/main.cpp > build/conformance-negative/main.cpp
	@cmp -s build/conformance-negative/main.cpp test/conformance/cpp/main.cpp && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage patched nothing"; exit 1; } || true
	$(CXX) $(TABLES_CXXFLAGS) $(CONFORMANCE_INCLUDES) build/conformance-negative/main.cpp \
		$(CONFORMANCE_SOURCES) -o build/conformance-negative/driver-bin
	@printf '#!/bin/sh\nexec build/conformance-negative/driver-bin "$$@"\n' > build/conformance-negative/driver
	@chmod +x build/conformance-negative/driver
	@printf 'cpp build/conformance-negative/driver\n' > build/conformance-negative/drivers.txt
	@if ./build/conformance-harness run --drivers build/conformance-negative/drivers.txt \
			--work build/conformance-negative/work > build/conformance-negative/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off in a dump left the harness green"; \
		cat build/conformance-negative/log; exit 1; \
	fi
	@grep -q "cpp / json-write" build/conformance-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on the sabotaged surface"; \
		  cat build/conformance-negative/log; exit 1; }
	@grep -q "wire          pass" build/conformance-negative/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat build/conformance-negative/log; exit 1; }
	@grep -m1 "cpp / json-write" build/conformance-negative/log
	@echo "negative control: one byte off in one dump turns the harness RED on that surface alone"

# THE C++ EMITTER-SIDE CONFORMANCE CONTROL (docs/PORTING.md I11). The control
# above flips a byte in a COPY OF THE DRIVER, so it localizes the text form and
# cannot show the harness finding a defect in GENERATED code. This one breaks
# the emitter itself: the walker's read-path storage offset in
# internal/codegen/cpptable/json.go is XORed by four, the whole tree is
# regenerated through a Go build overlay, and the C++ driver is rebuilt against
# what that emitter emitted. The harness must go RED on `json-read` alone —
# `json-write` and `wire` stay green, which is what says the break is the
# READER's. Nothing tracked is written to: the emitter source is patched into a
# COPY under build/ and reached through the overlay, so an interrupt cannot
# leave a sabotaged working tree.
CONFORMANCE_NEGATIVE_CPP := build/conformance-negative-cpp
.PHONY: conformance-negative-control-cpp
conformance-negative-control-cpp: build/conformance-harness
	@rm -rf $(CONFORMANCE_NEGATIVE_CPP) && mkdir -p $(CONFORMANCE_NEGATIVE_CPP)
	go run ./tools/sabotage -name table-cpp-json-read -out $(CONFORMANCE_NEGATIVE_CPP)/json.go.txt internal/codegen/cpptable/json.go
	@printf '{"Replace":{"%s/internal/codegen/cpptable/json.go":"%s/$(CONFORMANCE_NEGATIVE_CPP)/json.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CONFORMANCE_NEGATIVE_CPP)/overlay.json
	go build -overlay $(CONFORMANCE_NEGATIVE_CPP)/overlay.json -o $(CONFORMANCE_NEGATIVE_CPP)/schema ./cmd/schema
	$(call tables_generate,$(CONFORMANCE_NEGATIVE_CPP)/schema,$(CONFORMANCE_NEGATIVE_CPP)/generated)
	@grep -lq SABOTAGED $(CONFORMANCE_NEGATIVE_CPP)/generated/*/*Table.cpp || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotaged emitter emitted an unsabotaged walk"; exit 1; }
	@$(MAKE) $(SERIALIZE_RUNTIME)/serialize.h
	$(CXX) $(TABLES_CXXFLAGS) \
		$(subst -I$(SERIALIZE),-I$(SERIALIZE_RUNTIME),$(subst build/tables-generated/,$(CONFORMANCE_NEGATIVE_CPP)/generated/,$(CONFORMANCE_INCLUDES))) \
		test/conformance/cpp/main.cpp \
		$(subst build/tables-generated/,$(CONFORMANCE_NEGATIVE_CPP)/generated/,$(CONFORMANCE_SOURCES)) \
		-o $(CONFORMANCE_NEGATIVE_CPP)/driver-bin
	@printf '#!/bin/sh\nexec "%s/driver-bin" "$$@"\n' "$(CURDIR)/$(CONFORMANCE_NEGATIVE_CPP)" > $(CONFORMANCE_NEGATIVE_CPP)/driver
	@chmod +x $(CONFORMANCE_NEGATIVE_CPP)/driver
	@printf 'cpp %s/driver\n' "$(CONFORMANCE_NEGATIVE_CPP)" > $(CONFORMANCE_NEGATIVE_CPP)/drivers.txt
	@if ./build/conformance-harness run --drivers $(CONFORMANCE_NEGATIVE_CPP)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_CPP)/work > $(CONFORMANCE_NEGATIVE_CPP)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a sabotaged C++ walker left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_CPP)/log; exit 1; \
	fi
	@grep -Eq '^json-read +FAIL ' $(CONFORMANCE_NEGATIVE_CPP)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on json-read"; \
		  cat $(CONFORMANCE_NEGATIVE_CPP)/log; exit 1; }
	@grep -Eq '^json-write +pass ' $(CONFORMANCE_NEGATIVE_CPP)/log || \
		{ echo "NEGATIVE CONTROL FAILED: json-write went red too, so the control does not localise the READER"; \
		  cat $(CONFORMANCE_NEGATIVE_CPP)/log; exit 1; }
	@grep -Eq '^wire +pass ' $(CONFORMANCE_NEGATIVE_CPP)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_CPP)/log; exit 1; }
	@grep -m1 "cpp / json-read" $(CONFORMANCE_NEGATIVE_CPP)/log
	@echo "negative control: one storage offset off in the C++ emitter turns the harness RED on json-read alone"

# THE NEGATIVE CONTROL FOR THE BLOCK ROW DUMP (docs/SPEC-TABLES.md §19.2), and it
# sabotages neither driver: it flips one byte INSIDE A ROW of the block image
# itself. That is the whole reason the surface exists. `Open` reads the prologue
# and the triples and nothing else (§19.2's O(1) promise), so a byte inside a
# row cannot move its answer — `block` must stay GREEN and `block-dump` must go
# RED, which is the harness saying "the reader opened it and then read it
# wrong". A control that turned both red would be proving the image was
# corrupted, not that anyone reads rows.
#
# The byte is ships row 0's `object_id`: the array starts at 320 and the field
# sits at 64 inside a row, both facts the pinned dump states. If the fixture's
# layout ever moves under it the `cmp` below fails loudly rather than the
# control quietly checking nothing.
CONFORMANCE_NEGATIVE_BLOCK := build/conformance-negative-block

.PHONY: conformance-negative-control-block-dump
conformance-negative-control-block-dump: build/conformance-harness build/conformance-cpp
	@rm -rf $(CONFORMANCE_NEGATIVE_BLOCK) && mkdir -p $(CONFORMANCE_NEGATIVE_BLOCK)
	@cp testdata/wire/tables/block_render.bin $(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin
	@printf '\001' | dd of=$(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin bs=1 seek=384 count=1 conv=notrunc 2>/dev/null
	@cmp -s testdata/wire/tables/block_render.bin $(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin && \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage changed no byte of the image"; exit 1; } || true
	@sed 's|testdata/wire/tables/block_render.bin|$(CONFORMANCE_NEGATIVE_BLOCK)/block_render.bin|' \
		testdata/conformance/tables/MANIFEST.txt > $(CONFORMANCE_NEGATIVE_BLOCK)/MANIFEST.txt
	@printf 'cpp test/conformance/cpp/driver\n' > $(CONFORMANCE_NEGATIVE_BLOCK)/drivers.txt
	@if ./build/conformance-harness run --manifest $(CONFORMANCE_NEGATIVE_BLOCK)/MANIFEST.txt \
			--drivers $(CONFORMANCE_NEGATIVE_BLOCK)/drivers.txt \
			--work $(CONFORMANCE_NEGATIVE_BLOCK)/work > $(CONFORMANCE_NEGATIVE_BLOCK)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: one byte off inside a row left the harness green"; \
		cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; \
	fi
	@grep -q "cpp / block-dump" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the harness went red, but not on block-dump"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -q "^block         pass" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: block went red too, so the control does not localise the ROW READ"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -q "^forgery       pass" $(CONFORMANCE_NEGATIVE_BLOCK)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the whole matrix went red, so it localises nothing"; \
		  cat $(CONFORMANCE_NEGATIVE_BLOCK)/log; exit 1; }
	@grep -m1 "cpp / block-dump" $(CONFORMANCE_NEGATIVE_BLOCK)/log
	@echo "negative control: one byte off INSIDE A ROW turns the harness RED on block-dump alone — block still opens"

.PHONY: conformance-generate conformance-pin conformance-negative-control conformance-negative-control-block-dump

# ---------------------------------------------------------------------------
# THE LANGUAGE REGISTRY (docs/CONTRIBUTING.md, "Adding a language") ----------
#
# Every language is one file, make/<lang>.mk, included here by wildcard and
# named nowhere in this Makefile. A file registers its leg by appending to the
# lists below, and the aggregate targets under the include are the only places
# the lists are read — so a port adds its file and edits nothing here.
#
#   TEST_LEGS          test-<lang>: the leg's whole `make test` half — its
#                      generated trees, its gates and negative controls, its
#                      packet and table tests
#   CONFORMANCE_LEGS   what `make conformance` builds before the harness runs
#   CONFORMANCE_ENV    environment the harness run carries to the drivers
#   BENCH_TABLES_LEGS  the generated unit a leg of `make bench-tables` needs
#   GOLDENS_LEGS       update-goldens-<lang>: the leg's committed table goldens
#   TOOLCHAIN_LEGS     the legs with a pinned toolchain, each of which also
#                      defines toolchain-<lang> (one $(call toolchain_probe)
#                      per pin) and sets TOOLCHAIN_PINS_<lang> to EVERY pin
#                      that target probes. The negative control points each
#                      one in turn at a path that does not exist, so a pin
#                      left off the list is a probe nothing watches
# THE WIDE-SCALAR REFUSAL GATE (docs/SPEC-TABLES.md §3, §15): every scalar the
# type wire carries rides in a table in the C++ reference and the tool, and a
# port that has not landed the kinds yet must REFUSE a unit declaring them, by
# name, rather than emit a second wire for them. tables/scalars is the unit that
# declares each; every DISCOVERED port (make/<lang>.mk, the registry) is run
# over it and either generates the unit — it carries the kinds, and the
# conformance matrix holds it to the corpus — or stops with the diagnostic that
# names the fields and the follow-on. A `schema new-leg` skeleton has
# make/<lang>.mk but no `--lang` yet; that is a skip, not a gate failure.
# Nothing here lists a language.
# THE PORT REGISTRY: every make/<lang>.mk whose stem has no dash. A dashed
# stem (make/<card-id>.mk, make/<lang>-<part>.mk) is a gate or support
# fragment: included like any make file, never probed as a --lang port.
SCHEMA_PORTS := $(sort $(filter-out %-%,$(foreach p,$(patsubst make/%.mk,%,$(wildcard make/*.mk)),$(if $(findstring -,$(p)),,$(p)))))

.PHONY: tables-ports-refuse-wide-scalars
tables-ports-refuse-wide-scalars: bin/schema
	@rm -rf build/tables-wide-refusal && mkdir -p build/tables-wide-refusal
	# Successful probe output is a fixture, outside the compiler's Go module.
	@printf 'module wide-scalar-probe\n\ngo 1.26\n' > build/tables-wide-refusal/go.mod
	@carry=0; refuse=0; \
	for lang in $(SCHEMA_PORTS); do \
		if ./bin/schema generate --lang $$lang --out build/tables-wide-refusal/$$lang tables/scalars > build/tables-wide-refusal/$$lang.log 2>&1; then \
			carry=$$((carry+1)); continue; \
		fi; \
		if grep -q 'is not implemented' build/tables-wide-refusal/$$lang.log && \
		   [ "$$(sed -n '1p' compiler/target_$$lang.go 2>/dev/null)" = "//go:build schema_leg_$$lang" ]; then \
			echo "tables wide-scalar: $$lang has no --lang yet (schema new-leg skeleton)"; \
			continue; \
		fi; \
		grep -q "does not carry the fixed-point and 128-bit table-wire kinds yet" build/tables-wide-refusal/$$lang.log || \
			{ echo "REFUSAL GATE FAILED: the $$lang backend stopped for another reason:"; cat build/tables-wide-refusal/$$lang.log; exit 1; }; \
		grep -q "SimState.reach fixed(112, 16)" build/tables-wide-refusal/$$lang.log || \
			{ echo "REFUSAL GATE FAILED: the $$lang backend does not name the refused field"; exit 1; }; \
		refuse=$$((refuse+1)); \
	done; \
	if [ $$((carry+refuse)) -eq 0 ]; then echo "REFUSAL GATE FAILED: no port discovered under make/"; exit 1; fi; \
	echo "tables wide-scalar refusal gate: $$refuse ports refuse the unit by name, naming every wide field and the follow-on; $$carry carry the kinds"

# THE WIDE-SCALAR LAYOUT ASSERTS (docs/SPEC-TABLES.md §7.2, §19.3): the scalars
# unit's block form is compiled on its own, which is what makes its static_asserts
# — sixteen bytes at sixteen for every 128-bit field, the compiler's one model
# against this compiler's — a build fact rather than a claim.
.PHONY: tables-scalars-block-asserts
tables-scalars-block-asserts: build/tables-generated/.stamp
	@mkdir -p build
	$(CXX) $(BLOCK_CXXFLAGS) -I$(SERIALIZE) -Ibuild/tables-generated/scalars -c build/tables-generated/scalars/ScalarsBlock.cpp -o build/scalars-block.o
	@echo "tables wide-scalar layout asserts: the scalars block form compiles, every sizeof, alignof and offsetof asserted"

include $(wildcard make/*.mk)
# map is not a compiler backend. Include it by path so the registry wildcard
# above stays live ports only (tables-ports-refuse-wide-scalars).
include $(wildcard tools/agentsmap/map.mk)
# roadmap is not a compiler backend either: ROADMAP.md is generated from
# docs/roadmap.sexp by tools/roadmap/render.lisp (make roadmap, roadmap-check).
include $(wildcard tools/roadmap/roadmap.mk)
include make/checks/packet-arm-defaults.mk
include make/checks/packet-void.mk
include make/checks/packet-defaults.mk
include make/checks/packet-finite.mk
include make/checks/packet-text.mk
include make/checks/table-base64.mk
include make/checks/generated-tree.mk
include make/checks/schema-engine-is-the-oracle.mk
include make/checks/schema-shared-hostile-corpus.mk

# THE CONFORMANCE MATRIX (test/conformance/README.md): every discovered driver
# over every surface it lists. The reference leg is C++ and is built here; the
# rest are what the legs registered.
#
# A leg named in SCHEMA_SKIP_LEGS is not built here and its driver is not run
# (issue #599): --skip takes the same names, and the harness prints each one it
# passes over. The reference leg cannot be skipped.
.PHONY: conformance
conformance: build/conformance-harness build/conformance-cpp build/schema_test_cook $(CONFORMANCE_LEGS)
	$(CONFORMANCE_ENV) ./build/conformance-harness run \
		--results build/conformance/results.json \
		$(if $(SKIPPED_LEGS),--skip $(subst $(skip_space),$(skip_comma),$(strip $(SKIPPED_LEGS))))

# THE PUBLISHED MATRIX (issue #581): docs/MATRIX.md is generated from the one
# manifest in the tree, docs/matrix.json, by the Go tool at tools/matrix. The
# page is the board's table at the moment of the release — the
# feature-by-language cells and one maturity state per language — and it ships
# red: a ❌ cell and a language marked coming are honest states, never reasons
# to hold a release or to hide a column.
#
# `matrix-check` is the CI gate (tools/matrix/matrix_test.go): it regenerates
# the page and refuses when the committed one differs, so the published table
# cannot drift from what the tree ships. `go test ./...`, which `make test`
# runs, carries it too. `matrix-print` is what the release notes call: the
# page's state line per language, in manifest order.
.PHONY: matrix matrix-check matrix-print
matrix:
	go run ./tools/matrix write
matrix-check:
	go run ./tools/matrix check
matrix-print:
	@go run ./tools/matrix states

# THE TABLES BENCH PASS (bench/tables/README.md): every leg under
# bench/tables/*/leg, results under bench/tables/results/. A PUBLISHABLE
# number is a box sitting under the estate's bench rules — core 15, server
# stopped, not live, blessed per run; a run on a shared interactive machine is
# a pairing check and the board says which one it is.
.PHONY: bench-tables
bench-tables: generated/bench/tables/cpp/.stamp bench-table-check $(BENCH_TABLES_LEGS)
	bench/tables/run.sh

# What the include registered, one list per line. The registry gate
# (test/conformance/harness/registry_test.go) reads it to prove a planted
# language is discovered without an edit to this file.
.PHONY: registry
registry:
	@echo "test: $(TEST_LEGS)"
	@echo "conformance: $(CONFORMANCE_LEGS)"
	@echo "bench-tables: $(BENCH_TABLES_LEGS)"
	@echo "goldens: $(GOLDENS_LEGS)"
	@echo "toolchain: $(TOOLCHAIN_LEGS)"

# ---------------------------------------------------------------------------
# THE TOOLCHAIN GATE (issue #599), the aggregate ----------------------------
#
# Every leg that registered a pinned toolchain is probed here, before the chain
# spends its hour, and a pin that does not resolve stops `make test` by name.
# A leg named in SCHEMA_SKIP_LEGS is not probed and prints its skip instead, so
# the run says which legs it did not measure rather than passing over them.
# Nothing here lists a language: a leg registers TOOLCHAIN_LEGS, its pin name
# and its own toolchain-<lang> target in make/<lang>.mk, like every other list
# above.
REGISTERED_LEGS := $(patsubst test-%,%,$(TEST_LEGS))
UNKNOWN_SKIPS   := $(filter-out $(REGISTERED_LEGS),$(SKIPPED_LEGS))
SKIPPED_TOOLCHAIN_LEGS := $(filter $(SKIPPED_LEGS),$(TOOLCHAIN_LEGS))
PROBED_TOOLCHAIN_LEGS  := $(filter-out $(SKIPPED_LEGS),$(TOOLCHAIN_LEGS))

# A NAME THAT IS NOT A LEG IS A SKIP THAT SKIPS NOTHING, and it reads exactly
# like one that works, so it is refused before any pin is probed.
.PHONY: toolchain-names
toolchain-names:
ifneq ($(UNKNOWN_SKIPS),)
	@echo "make test REFUSES: SCHEMA_SKIP_LEGS names $(UNKNOWN_SKIPS), which is no registered leg"
	@echo "  the registered legs are: $(REGISTERED_LEGS) (make registry prints them)"
	@exit 1
endif

# EVERY LEG NOT NAMED IN THE SKIP LIST IS PROBED, and every one that does not
# resolve is NAMED, in one run. A leg's probe as a PREREQUISITE stops the gate
# at the first one that fails, turning a fresh clone into a queue: install one
# toolchain, run again, learn the next name. The legs go through sub-makes here
# instead, each leg's own refusal passed through as it printed it, and the
# summary names them together with the skip line that runs the chain without
# them, already carrying the skips in force.
#
# The green line says how many of the registered legs were probed, and when
# that is none it says so as a skip rather than as a claim about toolchains it
# never looked at.
TOOLCHAIN_GREEN = toolchain gate: $(words $(PROBED_TOOLCHAIN_LEGS)) of $(words $(TOOLCHAIN_LEGS)) registered legs probed, $(if $(PROBED_TOOLCHAIN_LEGS),every pinned toolchain resolves,every registered leg is named in SCHEMA_SKIP_LEGS)
.PHONY: toolchain
toolchain: toolchain-names
	@rm -rf build/toolchain && mkdir -p build/toolchain
	@missing=""; \
	$(foreach leg,$(PROBED_TOOLCHAIN_LEGS), \
		$(MAKE) --no-print-directory toolchain-$(leg) \
			> build/toolchain/$(leg).log 2> build/toolchain/$(leg).err \
			|| missing="$$missing $(leg)"; \
		cat build/toolchain/$(leg).log; \
		grep -q "REFUSES: the $(leg) leg" build/toolchain/$(leg).log \
			|| cat build/toolchain/$(leg).err;) \
	$(foreach leg,$(SKIPPED_TOOLCHAIN_LEGS), \
		echo "toolchain gate: the $(leg) leg is SKIPPED on purpose, SCHEMA_SKIP_LEGS names it";) \
	if [ -n "$$missing" ]; then \
		echo "make test REFUSES: the pinned toolchain does not resolve for:$$missing"; \
		echo "  every refusal above names its leg, its pin and the path that pin looked in"; \
		echo "  to run the chain without those legs, name the skips on purpose:"; \
		echo "      make test SCHEMA_SKIP_LEGS=$$(echo $(SKIPPED_LEGS) $$missing | tr ' ' ',')"; \
		exit 1; \
	fi; \
	echo "$(TOOLCHAIN_GREEN)"

# ITS NEGATIVE CONTROL. A gate that has never gone red is a gate with no blade,
# and going red is this one's whole job, so the control makes it go red: EVERY
# PIN OF EVERY registered leg, one pin at a time, pointed at a path that does
# not exist, and each must refuse BY THE LEG'S OWN NAME AND THAT PIN'S. A leg
# with a second pin is exactly where the blade goes blunt unseen: a control
# that points one pin per leg leaves the rest of them watching nothing, and
# deleting their probes keeps it green. Then the POSITIVE half, with every pin
# still pointed at that absent path and SCHEMA_SKIP_LEGS naming every leg: the
# same gate goes green and prints one named skip per leg. Nothing here lists a
# language either.
#
# While one pin is absent, THE LEG'S OTHER PINS POINT AT A PATH THAT RESOLVES,
# so the refusal is attributable to the pin under test rather than to whichever
# pin the bench happens to be missing anyway. A bench with none of the pinned
# toolchains installed is the case this gate exists for, so the control has to
# hold there too, and it does not hold if an earlier probe answers first.
TOOLCHAIN_NC_ABSENT  := $(CURDIR)/build/toolchain-nc/absent
TOOLCHAIN_NC_PRESENT := /bin/sh
TOOLCHAIN_ALL_PINS    = $(foreach leg,$(TOOLCHAIN_LEGS),$(TOOLCHAIN_PINS_$(leg)))
.PHONY: toolchain-negative-control
toolchain-negative-control:
	@rm -rf build/toolchain-nc && mkdir -p build/toolchain-nc
	@$(foreach leg,$(TOOLCHAIN_LEGS),$(foreach pin,$(TOOLCHAIN_PINS_$(leg)), \
		if $(MAKE) --no-print-directory toolchain-$(leg) \
				$(foreach other,$(filter-out $(pin),$(TOOLCHAIN_PINS_$(leg))),$(other)=$(TOOLCHAIN_NC_PRESENT)) \
				$(pin)=$(TOOLCHAIN_NC_ABSENT) \
				> build/toolchain-nc/$(leg)-$(pin).log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $(leg) leg stayed green with $(pin) pointed at a path that does not exist"; \
			cat build/toolchain-nc/$(leg)-$(pin).log; exit 1; \
		fi; \
		grep -q "REFUSES: the $(leg) leg's pinned $(pin) does not resolve" build/toolchain-nc/$(leg)-$(pin).log || \
			{ echo "NEGATIVE CONTROL FAILED: the $(leg) leg went red with $(pin) absent, but not by that pin's name"; \
			  cat build/toolchain-nc/$(leg)-$(pin).log; exit 1; }; \
		sed -n '1,2p' build/toolchain-nc/$(leg)-$(pin).log;))
	@$(MAKE) --no-print-directory toolchain \
		SCHEMA_SKIP_LEGS=$(subst $(skip_space),$(skip_comma),$(strip $(TOOLCHAIN_LEGS))) \
		$(foreach pin,$(TOOLCHAIN_ALL_PINS),$(pin)=$(TOOLCHAIN_NC_ABSENT)) \
		> build/toolchain-nc/skipped.log 2>&1 || \
		{ echo "POSITIVE FAILED: SCHEMA_SKIP_LEGS named every leg and the gate refused anyway"; \
		  cat build/toolchain-nc/skipped.log; exit 1; }
	@$(foreach leg,$(TOOLCHAIN_LEGS), \
		grep -q "the $(leg) leg is SKIPPED on purpose" build/toolchain-nc/skipped.log || \
			{ echo "POSITIVE FAILED: the $(leg) leg was passed over without being named"; \
			  cat build/toolchain-nc/skipped.log; exit 1; };)
	@cat build/toolchain-nc/skipped.log
	@echo "negative control: each of the $(words $(TOOLCHAIN_ALL_PINS)) registered pins ($(TOOLCHAIN_ALL_PINS)) pointed at a path that does not exist turns make test RED, by leg and by pin; naming the leg in SCHEMA_SKIP_LEGS turns it GREEN with the skip printed by name"

# THE `was` CONTROL (docs/SPEC-TABLES.md §5). A table renamed under `was` keeps
# the node type id every stored record carries, so W1's fleet reads under W2's
# Ship in silence, which is what the conformance rows w1_fleet_as_w2 and
# w2_fleet_as_w1 pin, and what a green run cannot be read for. The control
# strips `| was = "Vessel"` from W2 in a build copy, regenerates that unit with
# the SHIPPED compiler, and reads the same golden through the same program:
# every node record is now one the reader cannot name, `unknown` counts, the
# pointers read null, and the home vessel holds its declared default. The
# positive half runs first, against the shipped W2, so the two answers are
# read side by side.
# ---- rowan/cpp-versioning-numbers: BEGIN ----
# THE FLOOR'S TEST-ONLY SETTER, and nothing else, lives behind this define. It
# is NOT `SCHEMA_HAS_FLOOR`: that one says a build HAS a floor, and a shipped
# build that has one must not compile a way to lower it. Only the two fixedform
# test binaries below pass it, so the emitter is to emit
# `T##FixedSetFloorForTest` under this define alone (test/tables/versioning_numbers.cpp,
# `floor_raise_live`; bill §6b, §9).
FIXEDFORM_TEST_HOOKS = -DSCHEMA_FIXED_FLOOR_TEST_HOOKS
# ---- rowan/cpp-versioning-numbers: END ----

.PHONY: tables-was-negative-control
# THE FIXED FORM'S VERSIONING CONFORMANCE (docs/SPEC-TABLES.md §3.4). One
# binary, every case of §3.4's "held by test" row: the identity plan, an older
# writer, a newer writer with an unknown field AND an unknown nested type, a
# rename under `was`, a widened field, an enum variant and a union arm inserted
# in the middle, a keyed array whose keys moved, an optional against a value,
# and the NEGATIVE CONTROLS — the wrong plan, a form byte this reader does not
# carry, a plan that does not fit, and ONE CORRUPTED-LAYOUT CASE PER NAMED RULE
# a reader holds an untrusted peer's layout to.
# ==== BEGIN rowan/cpp-versioning-lists ====
# THE LIST ROWS' GENERATED PAIRS, as one variable so the three recipes that
# need them keep one line each (docs/FIXED-FORM-VERSIONING-TESTS.md).
VLISTS_INCLUDES = -Ibuild/tables-generated/vold_field_append -Ibuild/tables-generated/vnew_field_append -Ibuild/tables-generated/vold_field_deprecate -Ibuild/tables-generated/vnew_field_deprecate -Ibuild/tables-generated/vold_field_undeprecate -Ibuild/tables-generated/vnew_field_undeprecate -Ibuild/tables-generated/vold_enum_append -Ibuild/tables-generated/vnew_enum_append -Ibuild/tables-generated/vold_enum_width -Ibuild/tables-generated/vnew_enum_width -Ibuild/tables-generated/vold_union_append -Ibuild/tables-generated/vnew_union_append -Ibuild/tables-generated/vold_union_arm_payload_widen -Ibuild/tables-generated/vnew_union_arm_payload_widen -Ibuild/tables-generated/vold_flags_append -Ibuild/tables-generated/vnew_flags_append -Ibuild/tables-generated/vold_keyed_array_enum_append -Ibuild/tables-generated/vnew_keyed_array_enum_append -Ibuild/tables-generated/vold_nested_append -Ibuild/tables-generated/vnew_nested_append -Ibuild/tables-generated/vold_rename_without_was -Ibuild/tables-generated/vnew_rename_without_was
# ==== END rowan/cpp-versioning-lists ====

build/schema_test_fixedform: build/tables-generated/.stamp test/tables/fixedform_main.cpp \
                               test/tables/versioning_lists.cpp test/tables/versioning_numbers.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/fx1 -Ibuild/tables-generated/fx2 \
	    -Ibuild/tables-generated/v1 -Ibuild/tables-generated/v2 \
	    -Ibuild/tables-generated/p1 -Ibuild/tables-generated/p3 \
	    -Ibuild/tables-generated/ut1 -Ibuild/tables-generated/ut2 \
	    -Ibuild/tables-generated/fu1 -Ibuild/tables-generated/fu2 \
	    -Ibuild/tables-generated/fh1 -Ibuild/tables-generated/fh2 \
	    -Ibuild/tables-generated/fg1 \
	    -Ibuild/tables-generated/fe1 -Ibuild/tables-generated/fe2 \
	    -Ibuild/tables-generated/vnum \
	    $(VLISTS_INCLUDES) $(FIXEDFORM_TEST_HOOKS) \
	    -I$(SERIALIZE) test/tables/fixedform_main.cpp \
	    test/tables/versioning_lists.cpp test/tables/versioning_numbers.cpp -o $@

# THE SANITIZED TWIN, and it is the point of the byte-flip fuzz inside it. A
# fixed record carries no lengths and no terminators, so every offset the
# reader uses is arithmetic over sizes a STRANGER wrote down. "The reader never
# leaves the buffer" is a claim only a sanitizer can hold.
build/schema_test_fixedform_asan: build/tables-generated/.stamp test/tables/fixedform_main.cpp \
                               test/tables/versioning_lists.cpp test/tables/versioning_numbers.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
	    -fno-omit-frame-pointer -g \
	    -Ibuild/tables-generated/fx1 -Ibuild/tables-generated/fx2 \
	    -Ibuild/tables-generated/v1 -Ibuild/tables-generated/v2 \
	    -Ibuild/tables-generated/p1 -Ibuild/tables-generated/p3 \
	    -Ibuild/tables-generated/ut1 -Ibuild/tables-generated/ut2 \
	    -Ibuild/tables-generated/fu1 -Ibuild/tables-generated/fu2 \
	    -Ibuild/tables-generated/fh1 -Ibuild/tables-generated/fh2 \
	    -Ibuild/tables-generated/fg1 \
	    -Ibuild/tables-generated/fe1 -Ibuild/tables-generated/fe2 \
	    -Ibuild/tables-generated/vnum \
	    $(VLISTS_INCLUDES) $(FIXEDFORM_TEST_HOOKS) \
	    -I$(SERIALIZE) test/tables/fixedform_main.cpp \
	    test/tables/versioning_lists.cpp test/tables/versioning_numbers.cpp -o $@

# THE RUN COPY'S BOUND, ON ITS OWN (test/tables/fixedform_runcopy.cpp). The
# fixtures above check the VALUES a read produces; this one checks the single
# invariant the copy primitive has — every byte it reads or writes is inside the
# run — over EVERY length from 0 to 96 rather than the lengths a schema happens
# to produce. Exact-size heap blocks make the sanitized twin the assertion.
# THE TWO WIRE BYTES A READER MUST NORMALISE, on their own
# (test/tables/fixedform_hostile_bytes.cpp). A `bool` and an optional's PRESENT
# flag are `0` or `1` on the wire and nothing else, and a reader lands them as
# `byte != 0` (docs/FIXED-FORM-ALGORITHM.md §4.5). The reference lands both with
# a plain one-byte copy, so a forged `0x02` becomes a C++ `bool` holding `2` --
# an object whose every LOAD is undefined behaviour, reachable from a lawful
# schema and ONE hostile byte with no refusal and no counter moved. The fixture
# reads the member's byte with memcpy rather than loading it, so the case REPORTS
# instead of being the crash, and it is RED by name against fix 2.
build/schema_test_fixedform_hostile_bytes: build/tables-generated/.stamp test/tables/fixedform_hostile_bytes.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/hb -Ibuild/tables-generated/vnum \
	    -I$(SERIALIZE) test/tables/fixedform_hostile_bytes.cpp -o $@

build/schema_test_fixedform_hostile_bytes_asan: build/tables-generated/.stamp test/tables/fixedform_hostile_bytes.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
	    -fno-omit-frame-pointer -g -Ibuild/tables-generated/hb -Ibuild/tables-generated/vnum \
	    -I$(SERIALIZE) test/tables/fixedform_hostile_bytes.cpp -o $@

build/schema_test_fixedform_runcopy: build/tables-generated/.stamp test/tables/fixedform_runcopy.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -O2 -Ibuild/tables-generated/scalars \
	    -I$(SERIALIZE) test/tables/fixedform_runcopy.cpp -o $@

build/schema_test_fixedform_runcopy_asan: build/tables-generated/.stamp test/tables/fixedform_runcopy.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
	    -fno-omit-frame-pointer -g -Ibuild/tables-generated/scalars \
	    -I$(SERIALIZE) test/tables/fixedform_runcopy.cpp -o $@

# THE ORACLE BYTES ARE A PREREQUISITE OF THE REFERENCE'S OWN TEST (schema#876,
# card 15): the dump writes build/fixedform-corpus and this binary READS fu1.bin
# and fu2.bin back, so the file every port diffs against is held to the same
# values this test states. A corpus the writer produces and no reader checks is
# a golden nobody has read.
tables-fixedform: build/schema_test_fixedform build/schema_test_fixedform_asan \
                  build/schema_test_fixedform_runcopy build/schema_test_fixedform_runcopy_asan \
                  build/schema_test_fixedform_hostile_bytes build/schema_test_fixedform_hostile_bytes_asan \
                  build/fixedform-corpus/.stamp
	./build/schema_test_fixedform build/fixedform-corpus
	./build/schema_test_fixedform_asan build/fixedform-corpus
	./build/schema_test_fixedform_runcopy
	./build/schema_test_fixedform_runcopy_asan
	./build/schema_test_fixedform_hostile_bytes
	./build/schema_test_fixedform_hostile_bytes_asan

test: tables-fixedform

.PHONY: tables-fixedform

# THE C++ LEG'S FIELD-EVOLUTION ROWS (cpp/E8, cpp/R19). The OLD file is built
# in Go from the same IR the emitter lays down (fixedroadmap_t18_test.go), so
# this gate needs build/fixedform-corpus NOT AT ALL: it compiles and runs the
# generated C++ through the leg's own probe. Under SCHEMA_REQUIRE_CORPUS the
# compiler-absent skip is a FAILURE, and the -v transcript names cpp/E8, cpp/R19
# and each of the five appended kinds.
.PHONY: tables-cpp-fixed-t18
tables-cpp-fixed-t18:
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/codegen/cpptable/ -count=1 -timeout 600s -v -run 'TestFixedRoadmapCppT18FieldEvolution'

test: tables-cpp-fixed-t18

# THE RUN COPY'S NEGATIVE CONTROL, BOTH LEGS AT ONCE. The defect this gate is
# for was real and it was SILENT: the 17..31-byte branch anchored its tail move
# at the run's end, so it began 32 - n bytes in front of the run, took a
# neighbour's field with it on both sides of the copy, and read past the record
# body — with every counter and verdict in the report clean. Nothing but a
# sanitizer over exact-size blocks can see it, which is exactly why the gate
# above has to be shown going red.
#
# The sabotage plants that anchor back in BOTH emitters through a Go overlay,
# never in the tree, and the control requires the sanitized run copy test to
# name a heap-buffer-overflow in each leg.
#
# The C compile uses TABLES_CFLAGS, which carries -O2: gcc at -O0 emits the
# generated header's unused JSON wrappers, they call symbols in ScalarsTable.c,
# and the link fails before ASan can name the overflow. The optimiser drops
# them; the control's red is still the sanitizer's.
.PHONY: tables-fixedform-run-copy-negative-control
tables-fixedform-run-copy-negative-control:
	@mkdir -p build/runcopy-negative
	go run ./tools/sabotage -name reference-run-copy-anchor -out build/runcopy-negative/cpp_fixedruntime.gotext internal/codegen/cpptable/fixedruntime.go
	go run ./tools/sabotage -name reference-run-copy-anchor-c -out build/runcopy-negative/c_fixedruntime.gotext internal/codegen/ctable/fixedruntime.go
	@printf '{"Replace":{"%s/internal/codegen/cpptable/fixedruntime.go":"%s/build/runcopy-negative/cpp_fixedruntime.gotext","%s/internal/codegen/ctable/fixedruntime.go":"%s/build/runcopy-negative/c_fixedruntime.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" "$(CURDIR)" > build/runcopy-negative/overlay.json
	go run -overlay=build/runcopy-negative/overlay.json ./cmd/schema generate --lang cpp --out build/runcopy-negative/cpp tables/scalars
	go run -overlay=build/runcopy-negative/overlay.json ./cmd/schema generate --lang c --out build/runcopy-negative/c tables/scalars
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
	    -fno-omit-frame-pointer -g -Ibuild/runcopy-negative/cpp \
	    -I$(SERIALIZE) test/tables/fixedform_runcopy.cpp -o build/runcopy-negative/cpp_driver
	$(CC) $(TABLES_CFLAGS) -fsanitize=address,undefined \
	    -fno-sanitize-recover=all -fno-omit-frame-pointer -g -Ibuild/runcopy-negative/c \
	    -I$(SERIALIZE_C) test/tables/fixedform_runcopy.c -o build/runcopy-negative/c_driver -lm
	@if ./build/runcopy-negative/cpp_driver > build/runcopy-negative/cpp.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C++ run copy anchored at n - 32 did not leave its run"; exit 1; \
	fi
	@grep -q "heap-buffer-overflow" build/runcopy-negative/cpp.log || { \
		echo "NEGATIVE CONTROL FAILED: the C++ driver failed, but not with the overflow this control is for"; \
		tail -20 build/runcopy-negative/cpp.log; exit 1; }
	@if ./build/runcopy-negative/c_driver > build/runcopy-negative/c.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the C run copy anchored at n - 32 did not leave its run"; exit 1; \
	fi
	@grep -q "heap-buffer-overflow" build/runcopy-negative/c.log || { \
		echo "NEGATIVE CONTROL FAILED: the C driver failed, but not with the overflow this control is for"; \
		tail -20 build/runcopy-negative/c.log; exit 1; }
	@echo "run copy negative control: the old n - 32 anchor is a heap-buffer-overflow in both legs"

# THE FIXED FORM'S CROSS-LANGUAGE BYTE ORACLE (docs/SPEC-TABLES.md §3.4).
#
# The C++ backend is the REFERENCE for this form, so the reference is what
# writes the bytes and every port matches them — Glenn's rule for this class,
# and the same shape the paired bench already pins. This target writes one
# form-3 FILE per root into build/fixedform-corpus, with values set by hand so
# nothing passes by accident, and a port's leg proves itself against them two
# ways: reading a file and saving it back has to reproduce it BYTE FOR BYTE,
# and reading a file written under ANOTHER schema's layout is the plan path,
# which is the whole of what §3.4's versioning invariant is worth.
#
# It lives HERE rather than in a language's own make/<lang>.mk because it is
# every port's oracle and none of theirs: a corpus one leg owns is a corpus the
# next leg re-derives, and a golden a generator has to re-derive is not a
# golden.
# The runtime check is ORDER-ONLY: it runs first on every invocation, but a
# phony prerequisite would relink the dump every time, and the corpus rule
# below would then rewrite build/fixedform-corpus under every port's tests
# that are reading it at the same moment (`go test -p N` over the *table
# packages, one of which runs `make tables-rust-fixedform`).
build/schema_test_fixedform_dump: build/tables-generated/.stamp test/tables/fixedform_dump.cpp | tables-fixedform-corpus-check
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/fx1 -Ibuild/tables-generated/fx2 \
	    -Ibuild/tables-generated/p1 -Ibuild/tables-generated/p3 \
	    -Ibuild/tables-generated/fu1 -Ibuild/tables-generated/fu2 \
	    -Ibuild/tables-generated/examples \
	    -Ibuild/tables-generated/vnum \
	    $(VLISTS_INCLUDES) \
	    -I$(SERIALIZE_RUNTIME) test/tables/fixedform_dump.cpp -o $@

# The corpus is written beside the live one and swapped in whole, so a reader
# never sees a directory that is half written.
build/fixedform-corpus/.stamp: build/schema_test_fixedform_dump
	@rm -rf build/fixedform-corpus.next
	@mkdir -p build/fixedform-corpus.next
	./build/schema_test_fixedform_dump build/fixedform-corpus.next
# ---- rowan/corpus-manifest ----
# THE MANIFEST IS PART OF THIS TARGET'S OUTPUT (docs/FIXED-FORM-ALGORITHM.md
# §5.7 step 1, ruling #32): the dump writes it from the same values it writes
# into the bytes, and a corpus without it is a corpus a port has to read the
# emitter to use. Nothing under build/ is committed; the line below only refuses
# to call the corpus finished when the manifest did not arrive.
	@test -s build/fixedform-corpus.next/manifest.txt || { echo "corpus: manifest.txt missing" >&2; exit 1; }
	@touch build/fixedform-corpus.next/.stamp
	@rm -rf build/fixedform-corpus.prev
	@if test -d build/fixedform-corpus; then mv build/fixedform-corpus build/fixedform-corpus.prev; fi
	@mv build/fixedform-corpus.next build/fixedform-corpus
	@rm -rf build/fixedform-corpus.prev

tables-fixedform-corpus: build/fixedform-corpus/.stamp
	@echo "fixed form: the C++ reference's byte oracle is in build/fixedform-corpus"
	@echo "fixed form: what each file holds is in build/fixedform-corpus/manifest.txt ($$(wc -l < build/fixedform-corpus/manifest.txt | tr -d ' ') lines)"

# THE CORPUS FINDS ITS OWN RUNTIME (the "Fix, this attempt" rule). The oracle
# dump links the C++ reference, which includes serialize.h from the checkout
# $(SERIALIZE) (normally a sibling of this repo, the same checkout the C++
# targets are built against). A clean bench workspace has no sibling repo, so
# when no explicit SERIALIZE was named the corpus fetches the pinned runtime
# (serialize @ $(SERIALIZE_PIN), the checkout docs/TUTORIAL.md names) INSIDE the
# workspace and compiles against that. A SERIALIZE named on the command line
# (or in the environment) that is absent still refuses by name, and so does a
# fetch that fails or produces no header: a gate with no blade is the exact
# silence this refusal exists to forbid.
SERIALIZE_PIN     ?= v2.1.3
SERIALIZE_REPO    ?= https://github.com/mas-bandwidth/serialize.git
SERIALIZE_FETCHED := build/serialize
SERIALIZE_RUNTIME := $(if $(wildcard $(SERIALIZE)/serialize.h),$(SERIALIZE),$(SERIALIZE_FETCHED))

# THE GO PROBES FIND THEIR OWN RUNTIME THE SAME WAY. The versioning probes'
# go.mod replaces github.com/mas-bandwidth/serialize.go/v2 with the sibling
# checkout $(SERIALIZE_GO) (make/go.mk's default, ../serialize.go), and
# fixedversioning_test.go:1126 used to hard-code that sibling four levels up
# with nothing staging it. A clean bench has no sibling, so when no explicit
# SERIALIZE_GO was named this fetches the pinned Go runtime (serialize.go @
# $(SERIALIZE_GO_PIN), the tag .github/workflows/certify.yml names) into
# build/serialize.go. A SERIALIZE_GO named on the command line (or in the
# environment) that is absent still refuses by name, and so does a fetch that
# fails or produces no go.mod: a gate with no blade is the exact silence this
# refusal exists to forbid. The check target repeats that refusal in its own
# recipe: once build/serialize.go/go.mod exists, a named-but-absent SERIALIZE_GO
# still resolves SERIALIZE_GO_RUNTIME to the fetched copy, the file prerequisite
# is up to date, and a phony target with only that prerequisite would report
# nothing to be done.
SERIALIZE_GO_PIN     ?= v2.1.1
# Immutable commit resolved from refs/tags/v2.1.1 (annotated tag object: 36717230bfad4d890fe23882d14f87b9658205ab).
SERIALIZE_GO_COMMIT  ?= 97c6825383cffe9f43af64526dea4e0ef10d1662
SERIALIZE_GO_REPO    ?= https://github.com/mas-bandwidth/serialize.go.git
# SERIALIZE_GO_FETCHED and SERIALIZE_GO_RUNTIME are defined once, in make/go.mk
# (included above, before the go.mk rules that name $(SERIALIZE_GO_RUNTIME)/go.mod
# as a prerequisite); internal/ci/makevars_test.go refuses a second definition here.

# THE FLEET'S BENCHES MIRROR THE REPOSITORIES THEY WORK ON, and a local clone is
# offline, unthrottled and byte-for-byte the pinned tag. $NOVA_MIRRORS names a
# directory of bare mirrors laid out as <owner>/<repo>.git (for example
# $NOVA_MIRRORS/mas-bandwidth/serialize.git), which is why the runtime fetches
# below try that layout FIRST and fall back to the network repository
# ($(SERIALIZE_REPO)/$(SERIALIZE_GO_REPO)) only when NOVA_MIRRORS is unset, when
# the mirror is absent, or when the mirror clone fails (for example the mirror
# lacks the pin tag). file:// makes git honour --depth against a local path. A
# failure from BOTH still refuses by name, as before; the pins are unchanged.
#
# fetch-runtime PIN MIRROR-REL REPO DEST WHAT — clone DEST at tag PIN from
# $NOVA_MIRRORS/MIRROR-REL first, else from REPO, saying which source it used.
# WHAT names the runtime ("C" or "Go") in the echo. Returns non-zero when both
# clones fail, so the caller refuses by name.
define fetch-runtime
if test -n "$(NOVA_MIRRORS)" && test -d "$(NOVA_MIRRORS)/$(2)" && \
   git clone --depth 1 --branch "$(1)" "file://$(NOVA_MIRRORS)/$(2)" "$(4)"; then \
	echo "tables $(5) versioning: fetched $(1) from the mirror $(NOVA_MIRRORS)/$(2) into $(4)"; \
else \
	rm -rf "$(4)"; \
	echo "tables $(5) versioning: fetching $(3) @ $(1) into $(4)"; \
	git clone --depth 1 --branch "$(1)" "$(3)" "$(4)"; \
fi
endef

.PHONY: deps
deps:
	@if test -f "$(SERIALIZE_GO)/go.mod"; then \
		actual=$$(git -C "$(SERIALIZE_GO)" describe --tags --exact-match 2>/dev/null || echo untagged); \
		if test "$$actual" != "$(SERIALIZE_GO_PIN)"; then \
			echo "WARNING: serialize.go at $(SERIALIZE_GO) has tag $$actual; pin is $(SERIALIZE_GO_PIN)"; \
		fi; \
	elif test -e "$(SERIALIZE_GO)" || test -L "$(SERIALIZE_GO)"; then \
		echo "make deps: SERIALIZE_GO path '$(SERIALIZE_GO)' exists without go.mod; refusing and preserving it" >&2; \
		exit 1; \
	elif test "$(origin SERIALIZE_GO)" != "file"; then \
		echo "make deps: SERIALIZE_GO is named but the required serialize.go sibling at '$(SERIALIZE_GO)' is absent; refusing to fetch elsewhere" >&2; \
		exit 1; \
	else \
		mkdir -p "$(dir $(SERIALIZE_GO))"; \
		stage=$$(mktemp -d "$(dir $(SERIALIZE_GO)).serialize.go.deps.XXXXXX") || exit 1; \
		runtime="$$stage/serialize.go"; \
		$(call fetch-runtime,$(SERIALIZE_GO_PIN),mas-bandwidth/serialize.go.git,$(SERIALIZE_GO_REPO),$$runtime,Go) \
			|| { echo "make deps: serialize.go fetch failed for pin $(SERIALIZE_GO_PIN)" >&2; rm -rf "$$stage"; exit 1; }; \
		git -C "$$runtime" fetch --depth 1 origin "+refs/tags/$(SERIALIZE_GO_PIN):refs/tags/$(SERIALIZE_GO_PIN)" || { echo "make deps: serialize.go pin $(SERIALIZE_GO_PIN) has no fetchable tag" >&2; rm -rf "$$stage"; exit 1; }; \
		expected=$$(git -C "$$runtime" rev-parse "refs/tags/$(SERIALIZE_GO_PIN)^{commit}" 2>/dev/null) || { echo "make deps: cannot resolve exact serialize.go tag $(SERIALIZE_GO_PIN)" >&2; rm -rf "$$stage"; exit 1; }; \
		if test "$$expected" != "$(SERIALIZE_GO_COMMIT)"; then echo "make deps: serialize.go tag $(SERIALIZE_GO_PIN) resolves to $$expected, expected pinned commit $(SERIALIZE_GO_COMMIT)" >&2; rm -rf "$$stage"; exit 1; fi; \
		git -C "$$runtime" checkout --detach --force "$(SERIALIZE_GO_COMMIT)" || { echo "make deps: cannot check out serialize.go pin $(SERIALIZE_GO_PIN) at $(SERIALIZE_GO_COMMIT)" >&2; rm -rf "$$stage"; exit 1; }; \
		actual=$$(git -C "$$runtime" rev-parse HEAD) || { echo "make deps: cannot resolve fetched serialize.go HEAD" >&2; rm -rf "$$stage"; exit 1; }; \
		if test "$$actual" != "$(SERIALIZE_GO_COMMIT)" || ! test -f "$$runtime/go.mod"; then \
			echo "make deps: fetched serialize.go HEAD $$actual does not match tag $(SERIALIZE_GO_PIN) commit $(SERIALIZE_GO_COMMIT) or has no go.mod" >&2; rm -rf "$$stage"; exit 1; \
		fi; \
		if test -e "$(SERIALIZE_GO)" || test -L "$(SERIALIZE_GO)"; then \
			echo "make deps: SERIALIZE_GO path '$(SERIALIZE_GO)' appeared during fetch; refusing and preserving it" >&2; rm -rf "$$stage"; exit 1; \
		fi; \
		if ! mkdir "$(SERIALIZE_GO)"; then echo "make deps: SERIALIZE_GO path '$(SERIALIZE_GO)' appeared during placement; refusing and preserving it" >&2; rm -rf "$$stage"; exit 1; fi; \
		mv "$$runtime/go.mod" "$$stage/serialize.go.go.mod" || { echo "make deps: could not stage the validated serialize.go go.mod" >&2; rm -rf "$$stage"; exit 1; }; \
		if ! cp -R "$$runtime/." "$(SERIALIZE_GO)/"; then echo "make deps: could not populate reserved SERIALIZE_GO path '$(SERIALIZE_GO)' (left without go.mod)" >&2; rm -rf "$$stage"; exit 1; fi; \
		if ! ln "$$stage/serialize.go.go.mod" "$(SERIALIZE_GO)/go.mod"; then echo "make deps: could not publish validated go.mod at '$(SERIALIZE_GO)' (destination preserved)" >&2; rm -rf "$$stage"; exit 1; fi; \
		rm -rf "$$stage"; \
	fi
	@test -f "$(SERIALIZE_GO)/go.mod" || { echo "make deps: serialize.go at '$(SERIALIZE_GO)' has no go.mod; refusing" >&2; exit 1; }
	@echo "make deps: serialize.go $(SERIALIZE_GO_PIN) at $(SERIALIZE_GO)"

$(SERIALIZE_GO_RUNTIME)/go.mod:
	@if test -f "$(SERIALIZE_GO)/go.mod"; then :; \
	elif test "$(origin SERIALIZE_GO)" != "file"; then \
		echo "tables Go versioning: the required sibling runtime '$(SERIALIZE_GO)' (the serialize.go checkout) is absent; name it as a bench prerequisite" >&2; \
		exit 1; \
	else \
		echo "tables Go versioning: no sibling runtime at '$(SERIALIZE_GO)'; fetching $(SERIALIZE_GO_PIN) into $(SERIALIZE_GO_RUNTIME)"; \
		rm -rf "$(SERIALIZE_GO_RUNTIME)"; \
		mkdir -p "$(dir $(SERIALIZE_GO_RUNTIME))"; \
		$(call fetch-runtime,$(SERIALIZE_GO_PIN),mas-bandwidth/serialize.go.git,$(SERIALIZE_GO_REPO),$(SERIALIZE_GO_RUNTIME),Go) \
			|| { echo "tables Go versioning: the required sibling runtime '$(SERIALIZE_GO)' (the serialize.go checkout) is absent, and the fetch of $(SERIALIZE_GO_REPO) @ $(SERIALIZE_GO_PIN) failed; name it as a bench prerequisite" >&2; exit 1; }; \
	fi
	@test -f "$@" || { echo "tables Go versioning: the required sibling runtime '$(SERIALIZE_GO)' (the serialize.go checkout) is absent, and the fetch produced no $(SERIALIZE_GO_RUNTIME)/go.mod; name it as a bench prerequisite" >&2; exit 1; }

$(SERIALIZE_RUNTIME)/serialize.h:
	@if test -f "$(SERIALIZE)/serialize.h"; then :; \
	elif test "$(origin SERIALIZE)" != "file"; then \
		echo "tables C versioning: the required sibling runtime '$(SERIALIZE)' (the C++ serialize checkout) is absent; name it as a bench prerequisite" >&2; \
		exit 1; \
	else \
		echo "tables C versioning: no sibling runtime at '$(SERIALIZE)'; fetching $(SERIALIZE_PIN) into $(SERIALIZE_RUNTIME)"; \
		rm -rf "$(SERIALIZE_RUNTIME)"; \
		mkdir -p "$(dir $(SERIALIZE_RUNTIME))"; \
		$(call fetch-runtime,$(SERIALIZE_PIN),mas-bandwidth/serialize.git,$(SERIALIZE_REPO),$(SERIALIZE_RUNTIME),C) \
			|| { echo "tables C versioning: the required sibling runtime '$(SERIALIZE)' (the C++ serialize checkout) is absent, and the fetch of $(SERIALIZE_REPO) @ $(SERIALIZE_PIN) failed; name it as a bench prerequisite" >&2; exit 1; }; \
	fi
	@test -f "$@" || { echo "tables C versioning: the required sibling runtime '$(SERIALIZE)' (the C++ serialize checkout) is absent, and the fetch produced no $(SERIALIZE_RUNTIME)/serialize.h; name it as a bench prerequisite" >&2; exit 1; }

.PHONY: tables-fixedform-corpus-check
tables-fixedform-corpus-check: $(SERIALIZE_RUNTIME)/serialize.h $(SERIALIZE_GO_RUNTIME)/go.mod
	@if test "$(origin SERIALIZE_GO)" != "file" && ! test -f "$(SERIALIZE_GO)/go.mod"; then \
		echo "tables Go versioning: the required sibling runtime '$(SERIALIZE_GO)' (the serialize.go checkout) is absent; name it as a bench prerequisite" >&2; \
		exit 1; \
	fi

.PHONY: tables-fixedform-corpus

# THE FIXED FORM'S BENCH CORPUS, also the C++ reference's (docs/SPEC-TABLES.md
# §3.4's "held by test": the PAIRED CORPUS, sixty-four logical records on the
# packet wire and on this one). The reference decodes the canonical packet
# corpus, saves the same values with its form-3 writer, and states the VALUES
# beside the bytes in a JSON oracle — because a reader and a writer that share
# one offset mistake round trip perfectly and are both wrong.
build/fixedform-bench-corpus/.stamp: generated/bench/paired/cpp/.stamp test/bench/fixedform_corpus.cpp bench/corpus/variants/bench_mixed.variants.bin
	@mkdir -p build/fixedform-bench-corpus
	$(CXX) $(CXXFLAGS) -Igenerated/bench/paired/cpp test/bench/fixedform_corpus.cpp -o build/fixedform-bench-corpus/corpus
	./build/fixedform-bench-corpus/corpus bench/corpus/variants/bench_mixed.variants.bin \
		build/fixedform-bench-corpus/bench_fixed.bin build/fixedform-bench-corpus/bench_fixed.oracle.json
	@touch $@

tables-fixedform-bench-corpus: build/fixedform-bench-corpus/.stamp
	@echo "fixed form: the reference's paired bench corpus is in build/fixedform-bench-corpus"

.PHONY: tables-fixedform-bench-corpus

# THE THREE PROPERTIES (docs/SPEC-TABLES.md §3.4). A port that is wrong in both
# directions at once passes every round-trip the versioning set has, and the
# coverage matrix's named reds were found one fixture at a time. This binary
# asks the same three questions of every fixed-form fixture:
#
#   P1  identity plan == compiled plan, on every field and every counter
#   P2  write-read-write is byte-identical
#   P3  every byte of every record, mutated to {00,01,02,7f,80,ff}, both paths:
#       every landed field is within its bound or the read is a named refusal,
#       and a correction moves a counter. Under ASan+UBSan.
#
# A KNOWN-RED is printed by name on a green run. A listed case that starts
# PASSING turns the run red — delete it from known_red[] as part of landing
# the fix. A new red that is not on the list fails the target.
FIXEDFORM_PROP_INCLUDES := \
	-Ibuild/tables-generated/scalars -Ibuild/tables-generated/scalars2 \
	-Ibuild/tables-generated/fx1 -Ibuild/tables-generated/fx2 \
	-Ibuild/tables-generated/ut1 -Ibuild/tables-generated/ut2 \
	-Ibuild/tables-generated/v1 -Ibuild/tables-generated/v2 \
	-Ibuild/tables-generated/p1 -Ibuild/tables-generated/p3 \
	-Ibuild/tables-generated/fn1 -Ibuild/tables-generated/fn2 \
	-Ibuild/tables-generated/fm1 -Ibuild/tables-generated/fm2 \
	-Ibuild/tables-generated/f1 \
	-I$(SERIALIZE)

build/schema_test_fixedform_properties: build/tables-generated/.stamp test/tables/fixedform_properties.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) $(FIXEDFORM_PROP_INCLUDES) \
	    test/tables/fixedform_properties.cpp -o $@

build/schema_test_fixedform_properties_asan: build/tables-generated/.stamp test/tables/fixedform_properties.cpp
	@mkdir -p build
	$(CXX) $(TABLES_CXXFLAGS) -fsanitize=address,undefined -fno-sanitize-recover=all \
	    -fno-omit-frame-pointer -g -DSCHEMA_FIXEDFORM_SANITIZED \
	    $(FIXEDFORM_PROP_INCLUDES) test/tables/fixedform_properties.cpp -o $@

tables-fixed-properties: build/schema_test_fixedform_properties build/schema_test_fixedform_properties_asan
	./build/schema_test_fixedform_properties
	./build/schema_test_fixedform_properties_asan
	@echo "fixed form properties: P1 P2 P3 green, known-reds named"

test: tables-fixed-properties

.PHONY: tables-fixed-properties

tables-was-negative-control: build/tables-generated/.stamp test/tables/was_control_main.cpp
	@mkdir -p build/tables-was-nc
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/w2 -I$(SERIALIZE) test/tables/was_control_main.cpp \
		build/tables-generated/w2/W2Table.cpp -o build/tables-was-nc/with-was
	@./build/tables-was-nc/with-was > build/tables-was-nc/with-was.log
	@cat build/tables-was-nc/with-was.log
	@grep -q '^unknown=0 kind_mismatch=0 malformed=0 flagship=Aurora escorts=2 home_name=untitled$$' build/tables-was-nc/with-was.log || \
		{ echo "CONTROL FAILED: with was, the W1 fleet did not read in silence under W2"; exit 1; }
	@sed -e 's/^fixed table Ship | was = "Vessel"$$/fixed table Ship/' test/tables/W2.schema > build/tables-was-nc/W2.schema
	@cmp -s test/tables/W2.schema build/tables-was-nc/W2.schema && \
		{ echo "NEGATIVE CONTROL: the was sabotage patched nothing"; exit 1; } || true
	@rm -rf build/tables-was-nc/w2 && ./bin/schema generate --lang cpp --out build/tables-was-nc/w2 build/tables-was-nc/W2.schema
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-was-nc/w2 -I$(SERIALIZE) test/tables/was_control_main.cpp \
		build/tables-was-nc/w2/W2Table.cpp -o build/tables-was-nc/without-was
	@./build/tables-was-nc/without-was > build/tables-was-nc/without-was.log
	@cat build/tables-was-nc/without-was.log
	@grep -q '^unknown=2 kind_mismatch=0 malformed=0 flagship=null escorts=2 home_name=untitled$$' build/tables-was-nc/without-was.log || \
		{ echo "NEGATIVE CONTROL FAILED: without was, the W1 fleet did not read as unknown records under W2"; exit 1; }
	@echo "negative control: stripping was from the renamed table turns the cross read RED (unknown counted, the pointers null, the value at its default)"

# THE VOCABULARY `was` CONTROL (docs/SPEC-TABLES.md §5). R1's config, whose
# enum value, union arm, keyed slot and nested type field ride under the
# hashes of Silver, ward, Silver and multiplier, read under R2, where each is
# renamed under `was`. The control strips the four attributes from R2 in a
# build copy, regenerates that unit with the SHIPPED compiler, and reads the
# same golden through the same program: the value reads None, the union
# reads None, the slot is dropped, the field holds its default, and `unknown`
# counts five: the value, the array element that carries the same value, the
# slot, the arm and the field. The positive half runs first, against the
# shipped R2.
.PHONY: tables-wasrows-negative-control
tables-wasrows-negative-control: build/tables-generated/.stamp test/tables/wasrows_control_main.cpp
	@mkdir -p build/tables-wasrows-nc
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-generated/r2 -I$(SERIALIZE) test/tables/wasrows_control_main.cpp \
		build/tables-generated/r2/R2Table.cpp -o build/tables-wasrows-nc/with-was
	@./build/tables-wasrows-nc/with-was > build/tables-wasrows-nc/with-was.log
	@cat build/tables-wasrows-nc/with-was.log
	@grep -q '^unknown=0 kind_mismatch=0 malformed=0 grade=Argent effect=shield charge=2.5 mult=1.5 tally_argent=7$$' build/tables-wasrows-nc/with-was.log || \
		{ echo "CONTROL FAILED: with was, the R1 config did not read in silence under R2"; exit 1; }
	@sed -e 's/^\(    [A-Z][A-Za-z]*\) | was = "[A-Za-z]*"$$/\1,/' -e 's/ *| was = "[A-Za-z]*"$$//' test/tables/R2.schema > build/tables-wasrows-nc/R2.schema
	@test $$(grep -c 'was' build/tables-wasrows-nc/R2.schema) -eq $$(grep -c 'was' test/tables/R2.schema | awk '{print $$1 - 4}') || \
		{ echo "NEGATIVE CONTROL: the was sabotage did not strip exactly four attributes"; exit 1; }
	@rm -rf build/tables-wasrows-nc/r2 && ./bin/schema generate --lang cpp --out build/tables-wasrows-nc/r2 build/tables-wasrows-nc/R2.schema
	$(CXX) $(TABLES_CXXFLAGS) -Ibuild/tables-wasrows-nc/r2 -I$(SERIALIZE) test/tables/wasrows_control_main.cpp \
		build/tables-wasrows-nc/r2/R2Table.cpp -o build/tables-wasrows-nc/without-was
	@./build/tables-wasrows-nc/without-was > build/tables-wasrows-nc/without-was.log
	@cat build/tables-wasrows-nc/without-was.log
	@grep -q '^unknown=5 kind_mismatch=0 malformed=0 grade=None effect=None charge=0 mult=1 tally_argent=0$$' build/tables-wasrows-nc/without-was.log || \
		{ echo "NEGATIVE CONTROL FAILED: without was, the R1 config did not read as unknown names under R2"; exit 1; }
	@echo "negative control: stripping was from the variant, the arms and the type's field turns the cross read RED (unknown counted, the value at its default)"

include make/checks/reference-review.mk

# ---------------------------------------------------------------------------
# THE SLOW-GATE SCAN (schema#988, G5) ---------------------------------------
#
# internal/slowtest.Gate SKIPS a test that shells out to a foreign toolchain or
# reads the C++ reference corpus unless SCHEMA_SLOW=1 or SCHEMA_REQUIRE_CORPUS
# is set — AND A SKIPPED TEST MAKES `go test` EXIT 0. So a make target that runs
# a bare `go test` on such a package GOES GREEN HAVING RUN NOTHING. Fourteen
# positive gate targets did, for a day, while internal/slowtest's own package
# comment said every make gate set the variable. THE COMMENT WAS THE BUG: a
# claim about this Makefile belongs in a check.
#
# slow-gate-scan IS THAT CHECK and it costs milliseconds, so it rides `test`:
# every recipe line in Makefile, make/*.mk and make/checks/*.mk that runs
# `go test` on a package holding gated tests must set SCHEMA_SLOW=1, set
# SCHEMA_REQUIRE_CORPUS, or go through test/slowgate/proof. Anything else fails
# BY NAME. A line deliberately left as it is must be named in
# make/slow-gate-exceptions.txt WITH A REASON, and an entry there that matches
# no line any more fails too — a stale excuse is how the next bare `go test`
# slips in behind a line nobody reads.
# TAGS, made by tooling and never by hand (docs/VERSIONING.md, "The compiler's
# own version"; Glenn, 2026-10-15). promotion-tag tags HEAD main-<shorthash>
# and skips a commit already carrying one; .github/workflows/promotion-tag.yml
# runs it on every push to main. release tags one commit on main vX.Y.Z and
# refuses a version that does not parse, is not greater than the newest v tag,
# or names a commit off main, and before any of that runs the previous-release
# differential below and refuses on its difference; .github/workflows/release.yml
# runs it when dispatched. Without PUSH=1 both print the decision and write nothing.
# VERSION above defaults to `git describe`, which is never a release number:
# release reads VERSION only when it was given on the command line or in the
# environment, so a bare `make release` refuses on the grammar.
RELEASE_VERSION = $(if $(filter command line environment,$(origin VERSION)),$(VERSION))
RELEASE_COMMIT ?= HEAD
# the ref whose ancestry is main; the release gate's own test names HEAD
RELEASE_MAIN ?= origin/main
.PHONY: promotion-tag release
promotion-tag:
	go run ./tools/releasetag promotion-tag -commit $(RELEASE_COMMIT) $(if $(PUSH),-push)

# THE RELEASE LOCK (Glenn, 2026-10-31: "We need to prove this before v3.0.0
# not just hope that it's correct and complete"). release refuses, before any
# tag is made, unless PROOF names the proof report the ci-full `proof` job
# wrote AT THE COMMIT BEING RELEASED and that report is green: no red cell and
# docs/proof.json in step with the results (tools/matrix VerifyRelease). The
# refusal names the red cells. release.yml downloads the report from the
# ci-full run on the commit; a release with no report refuses by name. The
# lock runs first, so a red proof refuses before the previous-release
# differential's build; the differential then runs, and the tagger last.
MATRIX_GOFLAGS ?=
release:
	go run $(MATRIX_GOFLAGS) ./tools/matrix release-check -report '$(PROOF)' -commit $$(git rev-parse '$(RELEASE_COMMIT)^{commit}')
	$(MAKE) --no-print-directory release-differential
	go run ./tools/releasetag release -version '$(RELEASE_VERSION)' -commit $(RELEASE_COMMIT) -main $(RELEASE_MAIN) $(if $(PUSH),-push)

# THE PREVIOUS-RELEASE DIFFERENTIAL (docs/VERSIONING.md, #463): the WIRE bytes
# the newest v tag's compiler and RELEASE_COMMIT's encode for the same
# instances, compared under an equal protocol id. The packet wire through the
# C++ reference (the packet drivers, compiled against each side's generated
# C++, one driver source on both), the table forms through the Go engine each
# compiler carries (`schema pack` over every MANIFEST instance with text). A
# differing byte refuses, naming the unit, the instance and the byte; a unit
# whose id moved is listed, not compared. `release` runs it first and tags
# nothing on a difference; certify.yml runs it nightly. RELEASE_DIFFERENTIAL_PREV
# names another previous release, and RELEASE_DIFFERENTIAL_FLAGS adds flags
# (the release gate's own test hands it prebuilt compilers). It needs the tags
# and their trees (a full clone), Go, a C++ compiler and the serialize C++
# runtime, the sibling $(SERIALIZE) or else the pinned one fetched into build/.
RELEASE_DIFFERENTIAL_PREV ?=
RELEASE_DIFFERENTIAL_FLAGS ?=
RELEASE_DIFFERENTIAL_JOBS ?= $(shell getconf _NPROCESSORS_ONLN 2>/dev/null || echo 4)
.PHONY: release-differential release-differential-negative-control
release-differential: $(SERIALIZE_RUNTIME)/serialize.h
	go run ./tools/releasediff $(if $(RELEASE_DIFFERENTIAL_PREV),-prev $(RELEASE_DIFFERENTIAL_PREV)) -rev $(RELEASE_COMMIT) -serialize $(SERIALIZE_RUNTIME) -j $(RELEASE_DIFFERENTIAL_JOBS) $(RELEASE_DIFFERENTIAL_FLAGS)

# THE RELEASE REFUSES ON IT (release-refuses-on-differential). Two dry runs of
# `make release` itself, against a version past every tag, with main taken as
# HEAD so the branch under test is "on main": the clean compiler against itself
# must reach the tag decision, and the clean compiler against one whose Go
# engine writes a true bool as 2 under an unchanged protocol id must stop at
# the differential, naming the difference, with no tag decision printed.
RELEASE_GATE_TEST ?= build/release-gate-test
# the same Makefile this test was read from, so its control can hand it a copy
RELEASE_GATE_MAKE = $(MAKE) --no-print-directory -f $(firstword $(MAKEFILE_LIST)) release VERSION=v99999.0.0 RELEASE_COMMIT=HEAD RELEASE_MAIN=HEAD
.PHONY: release-refuses-on-differential release-refuses-on-differential-negative-control
release-refuses-on-differential:
	@rm -rf $(RELEASE_GATE_TEST) && mkdir -p $(RELEASE_GATE_TEST)
	go build -o $(RELEASE_GATE_TEST)/schema-clean ./cmd/schema
	go run ./tools/sabotage -name release-differential-table-bool -out $(RELEASE_GATE_TEST)/encode.gotext internal/tablewire/encode.go
	@printf '{"Replace":{"%s/internal/tablewire/encode.go":"%s/$(RELEASE_GATE_TEST)/encode.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(RELEASE_GATE_TEST)/table.json
	go build -overlay $(RELEASE_GATE_TEST)/table.json -o $(RELEASE_GATE_TEST)/schema-table ./cmd/schema
	@$(RELEASE_GATE_MAKE) RELEASE_DIFFERENTIAL_FLAGS="-prev clean -old-bin $(CURDIR)/$(RELEASE_GATE_TEST)/schema-clean -new-bin $(CURDIR)/$(RELEASE_GATE_TEST)/schema-clean -corpus . -work $(RELEASE_GATE_TEST)/guard" > $(RELEASE_GATE_TEST)/guard.out 2>&1 || \
		{ echo "FAILED: make release with no difference did not reach its tag decision"; cat $(RELEASE_GATE_TEST)/guard.out; exit 1; }
	@grep -q '^release: would tag v99999.0.0 ' $(RELEASE_GATE_TEST)/guard.out || \
		{ echo "FAILED: make release with no difference printed no tag decision"; cat $(RELEASE_GATE_TEST)/guard.out; exit 1; }
	@if $(RELEASE_GATE_MAKE) RELEASE_DIFFERENTIAL_FLAGS="-prev clean -old-bin $(CURDIR)/$(RELEASE_GATE_TEST)/schema-clean -new-bin $(CURDIR)/$(RELEASE_GATE_TEST)/schema-table -corpus . -work $(RELEASE_GATE_TEST)/red" > $(RELEASE_GATE_TEST)/red.out 2>&1; then \
		echo "FAILED: make release went through a wire difference under an unchanged protocol id"; cat $(RELEASE_GATE_TEST)/red.out; exit 1; \
	fi
	@grep -q '^DIFFERENCE under an equal protocol id: table variable form, ' $(RELEASE_GATE_TEST)/red.out || \
		{ echo "FAILED: make release refused, but not on the differential's difference"; cat $(RELEASE_GATE_TEST)/red.out; exit 1; }
	@! grep -q 'would tag\|tagged and pushed' $(RELEASE_GATE_TEST)/red.out || \
		{ echo "FAILED: make release reached a tag decision past a difference"; cat $(RELEASE_GATE_TEST)/red.out; exit 1; }
	@echo "release gate: make release reaches its tag decision with no difference, and stops at the differential, tagging nothing, on a wire difference under an unchanged protocol id"

# ITS NEGATIVE CONTROL: the test, green on this Makefile, then the same test
# over a copy whose `release` no longer runs the differential first, which
# must go RED on the release that went through the difference.
RELEASE_GATE_CONTROL := build/release-gate-control
release-refuses-on-differential-negative-control: release-refuses-on-differential
	@rm -rf $(RELEASE_GATE_CONTROL) && mkdir -p $(RELEASE_GATE_CONTROL)
	@sed 's/^release: release-differential$$/release: # SABOTAGED: the differential is not run first/' Makefile > $(RELEASE_GATE_CONTROL)/Makefile
	@! cmp -s Makefile $(RELEASE_GATE_CONTROL)/Makefile || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage no longer matches release's prerequisite line"; exit 1; }
	@if $(MAKE) --no-print-directory -f $(RELEASE_GATE_CONTROL)/Makefile release-refuses-on-differential RELEASE_GATE_TEST=$(RELEASE_GATE_CONTROL)/test > $(RELEASE_GATE_CONTROL)/red.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a release that never runs the differential left release-refuses-on-differential green"; cat $(RELEASE_GATE_CONTROL)/red.out; exit 1; \
	fi
	@grep -q 'FAILED: make release went through a wire difference under an unchanged protocol id' $(RELEASE_GATE_CONTROL)/red.out || \
		{ echo "NEGATIVE CONTROL FAILED: the test went red, but not on the release that went through the difference"; cat $(RELEASE_GATE_CONTROL)/red.out; exit 1; }
	@echo "negative control: a release that does not run the differential first turns release-refuses-on-differential RED on the release that went through the difference"

# ITS NEGATIVE CONTROL: one compiler built from this tree, and two built from
# it under an overlay each (tools/sabotage): release-differential-union-tag
# moves every union arm's tag by one in the C++ packet writer, and
# release-differential-table-bool writes a true bool as 2 in the Go engine's
# variable form. Neither moves a protocol id. The guard leg runs the clean
# compiler against itself and must pass having compared both wires; each red
# leg must refuse on its own wire, naming the unit, the instance and the byte,
# and on no other wire.
RELEASE_DIFFERENTIAL_CONTROL := build/release-differential-control
RELEASE_DIFFERENTIAL_RUN = go run ./tools/releasediff -prev clean -old-bin $(RELEASE_DIFFERENTIAL_CONTROL)/schema-clean -corpus . -serialize $(SERIALIZE) -j $(RELEASE_DIFFERENTIAL_JOBS)
release-differential-negative-control:
	@rm -rf $(RELEASE_DIFFERENTIAL_CONTROL) && mkdir -p $(RELEASE_DIFFERENTIAL_CONTROL)
	go build -o $(RELEASE_DIFFERENTIAL_CONTROL)/schema-clean ./cmd/schema
	go run ./tools/sabotage -name release-differential-union-tag -out $(RELEASE_DIFFERENTIAL_CONTROL)/functions.gotext internal/codegen/cpp/functions.go
	@printf '{"Replace":{"%s/internal/codegen/cpp/functions.go":"%s/$(RELEASE_DIFFERENTIAL_CONTROL)/functions.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(RELEASE_DIFFERENTIAL_CONTROL)/packet.json
	go build -overlay $(RELEASE_DIFFERENTIAL_CONTROL)/packet.json -o $(RELEASE_DIFFERENTIAL_CONTROL)/schema-packet ./cmd/schema
	go run ./tools/sabotage -name release-differential-table-bool -out $(RELEASE_DIFFERENTIAL_CONTROL)/encode.gotext internal/tablewire/encode.go
	@printf '{"Replace":{"%s/internal/tablewire/encode.go":"%s/$(RELEASE_DIFFERENTIAL_CONTROL)/encode.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(RELEASE_DIFFERENTIAL_CONTROL)/table.json
	go build -overlay $(RELEASE_DIFFERENTIAL_CONTROL)/table.json -o $(RELEASE_DIFFERENTIAL_CONTROL)/schema-table ./cmd/schema
	@for s in packet table; do \
		test "$$($(RELEASE_DIFFERENTIAL_CONTROL)/schema-clean id examples)" = "$$($(RELEASE_DIFFERENTIAL_CONTROL)/schema-$$s id examples)" || \
			{ echo "NEGATIVE CONTROL FAILED: the $$s sabotage moved the protocol id, so the gate would list the unit rather than compare it"; exit 1; }; \
	done
	@$(RELEASE_DIFFERENTIAL_RUN) -new-bin $(RELEASE_DIFFERENTIAL_CONTROL)/schema-clean -work $(RELEASE_DIFFERENTIAL_CONTROL)/guard > $(RELEASE_DIFFERENTIAL_CONTROL)/guard.out 2>&1 || \
		{ echo "NEGATIVE CONTROL FAILED: the clean compiler against itself did not pass"; cat $(RELEASE_DIFFERENTIAL_CONTROL)/guard.out; exit 1; }
	@grep -q '^  compared     packet wire, driver examples ' $(RELEASE_DIFFERENTIAL_CONTROL)/guard.out && \
		grep -q '^  compared     table forms: [1-9][0-9]* variable-form and [1-9][0-9]* message-form' $(RELEASE_DIFFERENTIAL_CONTROL)/guard.out || \
		{ echo "NEGATIVE CONTROL FAILED: the guard leg did not compare both wires, so a green verdict would mean nothing"; cat $(RELEASE_DIFFERENTIAL_CONTROL)/guard.out; exit 1; }
	@for s in packet table; do \
		if $(RELEASE_DIFFERENTIAL_RUN) -new-bin $(RELEASE_DIFFERENTIAL_CONTROL)/schema-$$s -work $(RELEASE_DIFFERENTIAL_CONTROL)/$$s > $(RELEASE_DIFFERENTIAL_CONTROL)/$$s.out 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$s sabotage moved wire bytes under an unchanged protocol id and left the differential green"; cat $(RELEASE_DIFFERENTIAL_CONTROL)/$$s.out; exit 1; \
		fi; \
	done
	@grep -q '^DIFFERENCE under an equal protocol id: packet wire, unit [a-zA-Z0-9/. +]*, instance [a-z_0-9/-]*\.bin first differs at byte [0-9]*' $(RELEASE_DIFFERENTIAL_CONTROL)/packet.out || \
		{ echo "NEGATIVE CONTROL FAILED: the union-tag sabotage went red, but naming no instance and its byte"; cat $(RELEASE_DIFFERENTIAL_CONTROL)/packet.out; exit 1; }
	@grep -q '^DIFFERENCE under an equal protocol id: packet wire, unit examples' $(RELEASE_DIFFERENTIAL_CONTROL)/packet.out || \
		{ echo "NEGATIVE CONTROL FAILED: the union-tag sabotage left the examples driver unnamed"; cat $(RELEASE_DIFFERENTIAL_CONTROL)/packet.out; exit 1; }
	@! grep '^DIFFERENCE' $(RELEASE_DIFFERENTIAL_CONTROL)/packet.out | grep -v ': packet wire, ' || \
		{ echo "NEGATIVE CONTROL FAILED: a C++ packet sabotage was named on a table form"; exit 1; }
	@grep -q '^DIFFERENCE under an equal protocol id: table variable form, unit [a-z0-9]*, instance [a-z_0-9]*: first differs at byte [0-9]*' $(RELEASE_DIFFERENTIAL_CONTROL)/table.out || \
		{ echo "NEGATIVE CONTROL FAILED: the table-bool sabotage went red, but not naming a variable-form instance and its byte"; cat $(RELEASE_DIFFERENTIAL_CONTROL)/table.out; exit 1; }
	@! grep '^DIFFERENCE' $(RELEASE_DIFFERENTIAL_CONTROL)/table.out | grep -v ': table variable form, ' || \
		{ echo "NEGATIVE CONTROL FAILED: a variable-form sabotage was named on another wire"; exit 1; }
	@echo "negative control: a union tag moved in the C++ packet writer, and a true bool written as 2 in the Go engine's variable form, each under an unchanged protocol id, turn the previous-release differential RED on that wire alone, naming the unit, the instance and the byte"

# release-proof-refusal holds the lock at the make level: `make release`
# handed a RED proof report at HEAD must refuse FROM THE PROOF GATE, naming a
# red cell, and never reach the tagger. The version is deliberately not a
# version, so a lock that let the release through would still tag nothing:
# the tagger refuses on its grammar, and that refusal is what this check
# tells apart from the lock's. Its control, release-proof-gate-negative-control,
# runs it with VerifyRelease sabotaged to pass a red report and requires it red.
RELEASE_PROOF_CHECK := build/release-proof
.PHONY: release-proof-refusal release-proof-gate-negative-control
release-proof-refusal:
	@mkdir -p $(RELEASE_PROOF_CHECK)
	@printf '{"commit":"%s","cells":2,"red":[{"row":"surface wire","lang":"goose","status":"red","why":"held for this check"}]}\n' "$$(git rev-parse HEAD)" > $(RELEASE_PROOF_CHECK)/red.json
	@if $(MAKE) --no-print-directory release VERSION=not-a-version RELEASE_COMMIT=HEAD PROOF=$(RELEASE_PROOF_CHECK)/red.json MATRIX_GOFLAGS='$(MATRIX_GOFLAGS)' > $(RELEASE_PROOF_CHECK)/out.txt 2>&1; then \
		echo "release-proof-refusal FAILED: make release went through with a red proof"; cat $(RELEASE_PROOF_CHECK)/out.txt; exit 1; fi
	@grep -q 'release REFUSES: the proof at .* surface wire / goose' $(RELEASE_PROOF_CHECK)/out.txt || \
		{ echo "release-proof-refusal FAILED: make release refused, but not from the proof gate naming the red cell"; cat $(RELEASE_PROOF_CHECK)/out.txt; exit 1; }
	@! grep -q 'tools/releasetag' $(RELEASE_PROOF_CHECK)/out.txt || \
		{ echo "release-proof-refusal FAILED: the tagger ran after a red proof"; cat $(RELEASE_PROOF_CHECK)/out.txt; exit 1; }
	@echo "release-proof-refusal: make release refuses a red proof at the commit, naming the cell, before the tagger runs"

release-proof-gate-negative-control:
	@rm -rf $(RELEASE_PROOF_CHECK)-control && mkdir -p $(RELEASE_PROOF_CHECK)-control
	go run ./tools/sabotage -name release-proof-gate -out $(RELEASE_PROOF_CHECK)-control/proof.gotext tools/matrix/proof.go
	@printf '{"Replace":{"%s/tools/matrix/proof.go":"%s/$(RELEASE_PROOF_CHECK)-control/proof.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(RELEASE_PROOF_CHECK)-control/overlay.json
	@if $(MAKE) --no-print-directory release-proof-refusal MATRIX_GOFLAGS=-overlay=$(CURDIR)/$(RELEASE_PROOF_CHECK)-control/overlay.json > $(RELEASE_PROOF_CHECK)-control/red.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a lock that passes a red proof left release-proof-refusal green"; cat $(RELEASE_PROOF_CHECK)-control/red.out; exit 1; fi
	@grep -q 'release-proof-refusal FAILED' $(RELEASE_PROOF_CHECK)-control/red.out || \
		{ echo "NEGATIVE CONTROL FAILED: release-proof-refusal went red for another reason"; cat $(RELEASE_PROOF_CHECK)-control/red.out; exit 1; }
	@echo "negative control: with VerifyRelease sabotaged to pass red cells, make release lets a red proof through and release-proof-refusal goes RED"

# THE PROOF TABLE (docs/MATRIX.md, "Proof: the 3.0.0 gate"; tools/matrix
# proof). `proof` runs the conformance matrix, then computes the table from
# its per-case results and fails, printing the red list, while any cell is red
# or docs/proof.json is not what the results compute. `proof-write`
# regenerates docs/proof.json and the page from the same results.
.PHONY: proof proof-write proof-negative-control
proof: conformance
	go run ./tools/matrix proof -results build/conformance/results.json

proof-write: conformance
	go run ./tools/matrix proof -results build/conformance/results.json -write

# THE PROOF GATE'S NEGATIVE CONTROL, end to end: the Go leg runs the real
# harness twice through a substituted registry, once as it is and once through
# a driver that deletes its answer for one wire case (root_full) after
# answering. The clean run's `surface wire / go` cell must be proven and the
# sabotaged run's must be red NAMING wire/root_full.
PROOF_CONTROL := build/proof-control
proof-negative-control: build/conformance-harness build/conformance-go table-base64-go
	@rm -rf $(PROOF_CONTROL) && mkdir -p $(PROOF_CONTROL)
	@printf '#!/bin/sh\ntest/conformance/go/driver "$$@" || exit $$?\nif [ "$$2" = wire ]; then rm -f "$$3/root_full"; fi\n' > $(PROOF_CONTROL)/deleting-driver
	@chmod +x $(PROOF_CONTROL)/deleting-driver
	@echo "go test/conformance/go/driver" > $(PROOF_CONTROL)/clean.txt
	@echo "go $(PROOF_CONTROL)/deleting-driver" > $(PROOF_CONTROL)/sabotaged.txt
	@./build/conformance-harness run --drivers $(PROOF_CONTROL)/clean.txt --work $(PROOF_CONTROL)/clean-work --results $(PROOF_CONTROL)/clean.json > $(PROOF_CONTROL)/clean.out 2>&1 || \
		{ echo "proof-negative-control: the clean Go leg failed"; cat $(PROOF_CONTROL)/clean.out; exit 1; }
	@go run ./tools/matrix proof -results $(PROOF_CONTROL)/clean.json -cell 'surface wire/go' || \
		{ echo "NEGATIVE CONTROL FAILED: the clean Go leg's surface wire cell is not proven"; exit 1; }
	@./build/conformance-harness run --drivers $(PROOF_CONTROL)/sabotaged.txt --work $(PROOF_CONTROL)/sabotaged-work --results $(PROOF_CONTROL)/sabotaged.json > $(PROOF_CONTROL)/sabotaged.out 2>&1 || true
	@if go run ./tools/matrix proof -results $(PROOF_CONTROL)/sabotaged.json -cell 'surface wire/go' > $(PROOF_CONTROL)/cell.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: with go's wire/root_full answer deleted, the cell stayed proven"; cat $(PROOF_CONTROL)/cell.out; exit 1; fi
	@grep -q 'red .*wire/root_full' $(PROOF_CONTROL)/cell.out || \
		{ echo "NEGATIVE CONTROL FAILED: the cell went red without naming wire/root_full"; cat $(PROOF_CONTROL)/cell.out; exit 1; }
	@echo "negative control: deleting the Go leg's wire/root_full answer turns 'surface wire / go' RED, naming the case"

# THE BUILD VERSION'S FORM GATE (docs/VERSIONING.md, "The release policy"):
# `release` refuses a PATCH release whose commit's ir.BuildVersionForm differs
# from the newest v tag's, and TestReleaseFormGate holds it for a patch, a
# minor and a major. Its negative control hands the gate the tag's form twice
# through a go test overlay (tools/sabotage, release-form-gate) and requires
# that test red on the patch release alone.
RELEASE_FORM_CONTROL := build/release-form-gate-control
.PHONY: release-form-gate-negative-control
release-form-gate-negative-control:
	@rm -rf $(RELEASE_FORM_CONTROL) && mkdir -p $(RELEASE_FORM_CONTROL)
	go run ./tools/sabotage -name release-form-gate -out $(RELEASE_FORM_CONTROL)/releasetag.gotext tools/releasetag/releasetag.go
	@printf '{"Replace":{"%s/tools/releasetag/releasetag.go":"%s/$(RELEASE_FORM_CONTROL)/releasetag.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > $(RELEASE_FORM_CONTROL)/overlay.json
	@if go test -count=1 -overlay $(RELEASE_FORM_CONTROL)/overlay.json -run 'TestReleaseFormGate$$' ./tools/releasetag > $(RELEASE_FORM_CONTROL)/red.out 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a Release that never compares the commit's form left TestReleaseFormGate green"; cat $(RELEASE_FORM_CONTROL)/red.out; exit 1; \
	fi
	@grep -q 'Release(v2.5.1) with the form moved 3 -> 4' $(RELEASE_FORM_CONTROL)/red.out || \
		{ echo "NEGATIVE CONTROL FAILED: TestReleaseFormGate went red, but not on the patch release"; cat $(RELEASE_FORM_CONTROL)/red.out; exit 1; }
	@! grep -q 'Release(v2.6.0)\|Release(v3.0.0)' $(RELEASE_FORM_CONTROL)/red.out || \
		{ echo "NEGATIVE CONTROL FAILED: the minor or the major went red too"; cat $(RELEASE_FORM_CONTROL)/red.out; exit 1; }
	@echo "negative control: a Release that never compares the commit's form tags a patch release that moves the build version's form, and TestReleaseFormGate goes RED on that release alone"

# THE LIST ELEMENT KIND IN THE BUILD VERSION (docs/SPEC-TABLES.md §4.1,
# §20.2). Form 5 puts an unbounded array's element kind on its cook line as
# `elemkind=`. Take the token out of a COPY of the rendering and the retype
# gate must go red, and so must §4.1's row in the frames golden: without the
# token `[]int32` and `[]float32` share a build version.
.PHONY: build-version-elemkind-negative-control
build-version-elemkind-negative-control:
	@mkdir -p build
	@sed -e 's|fmt.Fprintf(&b, " elem=%d elemkind=%d array=unbounded", elementPiece(u, f).size, TableWireElemKind(f))|fmt.Fprintf(\&b, " elem=%d array=unbounded", elementPiece(u, f).size) // SABOTAGED: no element kind|' \
		ir/buildversion.go > build/buildversion-no-elemkind.gotext
	@grep -q SABOTAGED build/buildversion-no-elemkind.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not remove the elemkind token"; exit 1; }
	@printf '{"Replace":{"%s/ir/buildversion.go":"%s/build/buildversion-no-elemkind.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/buildversion-no-elemkind-overlay.json
	@if SCHEMA_SLOW=1 go test -count=1 -overlay=build/buildversion-no-elemkind-overlay.json \
		./ir -run TestTheBuildVersionMovesWhenTheListElementIsRetyped > build/buildversion-no-elemkind.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the retype gate passed with elemkind gone"; \
		cat build/buildversion-no-elemkind.log; exit 1; \
	fi
	@grep -q "scores     \[\]int32 -> scores     \[\]float32 does not move the build version" build/buildversion-no-elemkind.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the same-size retype"; \
		  cat build/buildversion-no-elemkind.log; exit 1; }
	@if SCHEMA_SLOW=1 go test -count=1 -overlay=build/buildversion-no-elemkind-overlay.json \
		./internal/baseline -run 'TestEvolutionTableFrames' > build/buildversion-no-elemkind-frames.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the §4.1 frames golden passed with elemkind gone"; \
		cat build/buildversion-no-elemkind-frames.log; exit 1; \
	fi
	@grep -q "an_unbounded_array's_element_retyped" build/buildversion-no-elemkind-frames.log || \
		{ echo "NEGATIVE CONTROL FAILED: the frames golden went red, but not on the element-retype row"; \
		  cat build/buildversion-no-elemkind-frames.log; exit 1; }
	@echo "negative control: removing elemkind from the list's cook line turns the retype gate and §4.1's row RED"

# THE TEXT TOTAL ON THE ENGINE'S READ (docs/SPEC-TABLES.md §2.13): the engine
# is the oracle, so it keeps the prefix within the reader's total. Make the
# clamp return the decoded count unchanged in a COPY of the decoder and the
# total test must go red.
.PHONY: tables-wire-text-total-negative-control
tables-wire-text-total-negative-control:
	@mkdir -p build
	@sed -e 's@^[[:space:]]*if f.TextTotal <= 0 || !f.IsTextList() {$$@if true { // SABOTAGED: the total is never held@' \
		internal/tablewire/decode.go > build/tablewire-no-total.gotext
	@grep -q SABOTAGED build/tablewire-no-total.gotext || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not patch the total clamp"; exit 1; }
	@printf '{"Replace":{"%s/internal/tablewire/decode.go":"%s/build/tablewire-no-total.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/tablewire-no-total-overlay.json
	@if SCHEMA_SLOW=1 go test -count=1 -overlay=build/tablewire-no-total-overlay.json \
		./internal/tablewire -run TestTextTotalClampsTheReadToAPrefix > build/tablewire-no-total.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the total test passed with the clamp gone"; \
		cat build/tablewire-no-total.log; exit 1; \
	fi
	@grep -q "want clamped 4" build/tablewire-no-total.log || \
		{ echo "NEGATIVE CONTROL FAILED: it went red, but not on the missing clamp"; \
		  cat build/tablewire-no-total.log; exit 1; }
	@echo "negative control: removing the engine's total clamp turns the text-total test RED"

test: release-proof-refusal

.PHONY: slow-gate-scan
slow-gate-scan:
	go run ./tools/slowgatescan

test: slow-gate-scan

# AND THE ACCOUNTING, test by test rather than target by target: a plain
# `go test -v ./...` transcript (the DEFAULT half — no SCHEMA_SLOW, which is the
# point) is read back and every test that skipped at the gate is placed in one
# of three buckets — run by a make target that sets the variable, run only by
# ci-full.yml's SCHEMA_SLOW=1 steps, or run by NOBODY. The third bucket is the
# failure. The whole table lands in build/slowgate/coverage.tsv so the
# accounting is READ, not believed.
#
# It costs one plain `go test ./...` — 27 s with a warm build cache, 2 m 35 s
# cold, measured on the Studio — so it is NOT on `test`: it rides ci-full.yml
# beside the two steps it credits. Nothing is MOVED to nightly by this; the scan
# above, which is the gate, is on `test`.
#
# `|| true` ON THE TRANSCRIPT AND NOWHERE ELSE: this target's job is to read
# which tests the gate skipped, and it must still read that when some unrelated
# test is red. The red itself is `test`'s to report, not this target's, and the
# accounting below exits nonzero on its own finding.
.PHONY: slow-gate-coverage
slow-gate-coverage: deps
	@mkdir -p build/slowgate
	go test -json ./... > build/slowgate/all-plain.log 2>&1 || true
	go run ./tools/slowgatescan -coverage build/slowgate/all-plain.log

# THE SAME ACCOUNTING IN TWO HALVES, for ci-full.yml's two slow-gate rows. The
# verdict is per skip, so the emitter packages and every other package, each
# read on its own, reach the verdict one plain `go test ./...` reaches, at about
# half the wall time each. The package lists are spelled out rather than passed
# in a variable so `slowgatescan`'s static audit still reads them.
.PHONY: slow-gate-coverage-codegen slow-gate-coverage-rest
slow-gate-coverage-codegen: deps
	@mkdir -p build/slowgate
	go test -json ./internal/codegen/... > build/slowgate/codegen-plain.log 2>&1 || true
	go run ./tools/slowgatescan -coverage build/slowgate/codegen-plain.log
slow-gate-coverage-rest: deps
	@mkdir -p build/slowgate
	go test -json $$(go list ./... | grep -v '/internal/codegen/') > build/slowgate/rest-plain.log 2>&1 || true
	go run ./tools/slowgatescan -coverage build/slowgate/rest-plain.log
# THE VULNERABILITY GATE. `make` is the one entry, so the gates both workflows
# run are targets here: `vuln` runs the scanner pinned and direct, and the
# retry-and-classify logic behind the nightly is a committed script
# (tools/vuln/govulncheck.sh) that `make test` holds to its fixtures — not
# lines of YAML duplicated into two files, where the first divergence between
# the copies is nobody's to notice.
#
# THE SCANNER'S PIN LIVES ON THE NEXT LINE AND NOWHERE ELSE, bumped by hand.
# What has to change on its own is the DATABASE, which the tool fetches at run
# time, so a red run can never mean the scanner moved under us.
GOVULNCHECK_VERSION ?= v1.7.0

# `make vuln` is the pull-request gate and runs the scanner PINNED AND DIRECT,
# with no retry and no classifier: any failure — an unreachable database
# included — is RED, because a PR-tier gate that forgives an outage is a gate
# that has silently stopped watching, and a new CVE is the one verdict that
# changes with no commit here. `make vuln-strict` is the nightly certification's
# gate: the same pin through the classifying script, three attempts against a
# transient blip and then RED, because a certificate is a full run at an exact
# SHA and an unreachable database certifies nothing.
.PHONY: vuln
vuln:
	go run golang.org/x/vuln/cmd/govulncheck@$(GOVULNCHECK_VERSION) ./...

.PHONY: vuln-strict
vuln-strict:
	GOVULNCHECK_VERSION=$(GOVULNCHECK_VERSION) tools/vuln/govulncheck.sh run --strict

# The classifier's own table test (tools/vuln/testdata/cases.txt): offline, a
# fraction of a second, and the reason the gate above can be trusted to tell an
# outage from a failure. It rides `make test` like every other control here.
.PHONY: vuln-selftest
vuln-selftest:
	tools/vuln/govulncheck.sh selftest

test: vuln-selftest

# Ensure the Go sibling runtime before any tests can enter a Go gate.
test: deps
