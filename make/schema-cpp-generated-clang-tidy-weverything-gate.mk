# make/schema-cpp-generated-clang-tidy-weverything-gate.mk — the C++ LENS: the
# strictest C++ checkers over the cpp output the generate targets read, so a
# portability or lifetime bug is seen in the generated reader and writer before
# it is seen in a downstream project.
#
# THE CORPUS is not invented here. It is every schema directory a `generate
# --lang cpp` line in the Makefile or make/*.mk reads (examples, examples128,
# examples-wide, the tables corpus, bench/corpus, test/guard and the whole of
# test/tables). Each source is generated into its own unit, so two schemas that
# share a package name never collide the way one shared output directory would.
#
# TWO CHECKERS, ONE TARGET:
#   * clang-tidy with bugprone-*, cert-* and cppcoreguidelines-* (the families
#     that find lifetime, bounds and portability bugs);
#   * clang -Weverything -Werror, with the justified exceptions below.
#
# THE EXCEPTIONS ARE NAMED ONE WARNING AT A TIME. There is no -w and no
# --disable-all here; each -Wno- below is a verdict on one family, with the
# reason beside it, and a new family from a newer clang is red until somebody
# triages it.
#
# The tool is pinned and probed. When clang-tidy is absent the target prints
# `tool missing: clang-tidy` and exits non-zero: a gate that passes while its
# checker is missing has no blade. Install the pin through the fleet play
# (rowan-tools/fleet); never hand-copy a binary onto one machine.

# Pins. The workflow installs the same major.minor from Ubuntu's official
# archive; a probe that fails prints the version it found, so a fleet bump is a
# visible edit here and not a silent behaviour change.
SCHEMA_CPP_CTW_TIDY_PIN     ?= 18.1
SCHEMA_CPP_CTW_CLANG_PIN    ?= 18.1
SCHEMA_CPP_CTW_CLANG_TIDY   ?= clang-tidy
SCHEMA_CPP_CTW_CLANG        ?= clang++

# The corpus and its work directory. Both are overridable so the prepared
# negative control and the fast lane drive the SAME recipe over their own tree.
SCHEMA_CPP_CTW_CORPUS ?= build/cpp-ctw-corpus
SCHEMA_CPP_CTW_WORK   ?= build/cpp-ctw-work

# Every directory a `generate --lang cpp` line reads. Keep this list beside the
# generate rules: a directory added there without a line here is a corpus hole,
# and the gate's "units" line makes the hole visible.
SCHEMA_CPP_CTW_CORPUS_DIRS ?= examples examples128 examples-wide \
	tables/arms tables/backend tables/blobs tables/block tables/blockhome \
	tables/cards tables/examples tables/lists tables/maps tables/messages \
	tables/pointers tables/scalars tables/stream tables/vocab tables/vocab9

# One generate call per schema where a directory holds more than one package
# (SPEC §3.2), so these are named per file rather than per directory.
SCHEMA_CPP_CTW_CORPUS_SCHEMAS ?= $(wildcard test/tables/*.schema)

# The fast lane: two representative units (packet and table, readers and
# writers) and the planted control. The whole corpus is the nightly target.
SCHEMA_CPP_CTW_FAST_CORPUS ?= build/cpp-ctw-fast-corpus
SCHEMA_CPP_CTW_FAST_WORK   ?= build/cpp-ctw-fast-work
SCHEMA_CPP_CTW_FAST_DIRS   ?= examples tables/examples

# JUSTIFIED -Weverything EXCEPTIONS.
#
# c++98-compat, c++98-compat-pedantic, pre-c++17-compat
#   The generated output targets C++17 deliberately; these warn that a C++98
#   compiler could not read it, which is not a property this port promises.
# old-style-cast
#   The generator emits C-style casts at wire-width boundaries on purpose, so
#   the cast and the width are read together; C++ casts add no safety here.
# padded
#   The wire structs are laid out for the wire, not for the host's padding
#   preference; a padded TableWriter is a deliberate ABI choice.
# covered-switch-default, switch-default, switch-enum
#   EnumName carries a default label for an OUT-OF-SET value (SPEC §4.8), and
#   the wire switches deliberately omit default so a new arm is a compile-time
#   hole rather than a silent fallthrough.
# comma
#   `( offset += n, true )` is the one-line bounds helper's deliberate comma
#   expression, not a dropped statement.
# cast-align
#   TableArenaAt returns storage aligned to the arena's maximum alignment; the
#   compiler cannot see that through the byte pointer, the arena contract can.
# implicit-int-conversion, implicit-int-float-conversion, sign-conversion
#   Bit assembly and dequantization narrow by construction: the value was just
#   range-checked against the field's wire width or is the float scale divisor.
# nested-anon-types
#   The wide-text arm is an anonymous struct inside the generated union; it is
#   a GNU extension the wide corpus has used since issue #414 and every target
#   compiler accepts it.
# reserved-identifier
#   Generated locals join a prefix and a schema field name (`cursor_` + `_new`
#   yields `cursor__new`). They are file-local and never ABI-visible; the
#   finding is a naming-standard note, not a lifetime or bounds bug.
# float-equal
#   Default elision compares a decoded value with the schema's declared default
#   literal; the comparison is the wire rule (§2.5), not an approximate test.
# unused-macros
#   A generated header's macros are used by another translation unit; a TU that
#   includes the header without using every macro is still the header's caller.
# unsafe-buffer-usage
#   The generated table writer is the buffer walk itself; the check's advisory
#   rewrite is a C++20 span migration, not a finding this gate can act on.
SCHEMA_CPP_CTW_WARNINGS := \
	-Wno-c++98-compat -Wno-c++98-compat-pedantic -Wno-pre-c++17-compat \
	-Wno-old-style-cast -Wno-padded \
	-Wno-covered-switch-default -Wno-switch-default -Wno-switch-enum \
	-Wno-comma -Wno-cast-align \
	-Wno-implicit-int-conversion -Wno-implicit-int-float-conversion -Wno-sign-conversion \
	-Wno-nested-anon-types -Wno-reserved-identifier -Wno-float-equal \
	-Wno-unused-macros -Wno-unsafe-buffer-usage

# The three families the card names, minus the checks triaged NOT over the
# generated corpus (the whole finding list is in the card's pull request). Each
# suppression is one named check, not a family:
#
# cppcoreguidelines-avoid-magic-numbers
#   Every literal is a wire width, mask, code-point boundary or default from the
#   schema language; naming each one would hide the wire layout for no safety.
# cppcoreguidelines-avoid-c-arrays, cppcoreguidelines-pro-bounds-array-to-pointer-decay
#   The wire structs, table field arrays and JSON token buffers are C arrays on
#   purpose: their layout is the wire format, not a host container choice.
# cppcoreguidelines-pro-bounds-pointer-arithmetic, cppcoreguidelines-pro-bounds-constant-array-index
#   The reader, writer and arena ARE the buffer walk; the bounds are checked at
#   the call that established offset/size, and an index helper is the walk.
# cppcoreguidelines-pro-type-cstyle-cast
#   The generator emits C-style casts at width boundaries so the cast and the
#   wire width are read together; C++ casts add no safety to a width assertion.
# cppcoreguidelines-pro-type-member-init
#   Every field of the records flagged (graph, in, out, count, r) is assigned on
#   the next statements before any read; clang-tidy cannot see the assignment.
# cppcoreguidelines-init-variables
#   The only hit, TableJsonHex4's `digit`, is assigned on every path that does
#   not return -1 first; the checker does not model the exhaustive if/else.
# cppcoreguidelines-pro-type-vararg
#   The vararg calls are the generated snprintf formatters, the port's one
#   string formatter; rewriting them would duplicate the C library.
# cppcoreguidelines-narrowing-conversions, bugprone-narrowing-conversions
#   The only hits are TableJsonPeek's `: 0` becoming a char; 0 fits char on
#   every target, and the signed wire values are int8_t (explicitly signed).
# cert-str34-c, bugprone-signed-char-misuse
#   The signed-char hits are int8_t values widened to int64_t on purpose: an
#   int8_t is explicitly signed, so sign extension is the wire rule, not char's
#   implementation-defined signedness.
# bugprone-easily-swappable-parameters
#   These are the generated public writer/reader signatures; their names and
#   order are the schema's API, and changing them is not a bug fix.
# cppcoreguidelines-avoid-do-while
#   The do-while loops are the generated table/JSON walk macros, whose body must
#   run once for an empty container.
# cppcoreguidelines-interfaces-global-init
#   The flagged table-field lambdas return the address of extern constants
#   defined later in the same unit; taking an address reads no value, and the
#   constants are file-local with no dynamic initializer.
# bugprone-switch-missing-default-case
#   The width switches deliberately have no default: every wire width is one
#   arm and the fall-through return is the out-of-set verdict (SPEC §4.8).
# bugprone-branch-clone
#   The repeated branch is `key_over` and `!fits` both counting kind_mismatch;
#   two distinct wire rules share the same count, which is the specification.
# bugprone-casting-through-void
#   The wide-text arm reads the wire's uint8_t storage as char16_t by design;
#   the arena guarantees alignment, and the cast is the code unit view.
SCHEMA_CPP_CTW_TIDY_CHECKS := -*,bugprone-*,cert-*,cppcoreguidelines-*,\
	-cppcoreguidelines-avoid-magic-numbers,\
	-cppcoreguidelines-avoid-c-arrays,\
	-cppcoreguidelines-pro-bounds-array-to-pointer-decay,\
	-cppcoreguidelines-pro-bounds-pointer-arithmetic,\
	-cppcoreguidelines-pro-bounds-constant-array-index,\
	-cppcoreguidelines-pro-type-cstyle-cast,\
	-cppcoreguidelines-pro-type-member-init,\
	-cppcoreguidelines-init-variables,\
	-cppcoreguidelines-pro-type-vararg,\
	-cppcoreguidelines-narrowing-conversions,\
	-bugprone-narrowing-conversions,\
	-cert-str34-c,\
	-bugprone-signed-char-misuse,\
	-bugprone-easily-swappable-parameters,\
	-cppcoreguidelines-avoid-do-while,\
	-cppcoreguidelines-interfaces-global-init,\
	-bugprone-switch-missing-default-case,\
	-bugprone-branch-clone,\
	-bugprone-casting-through-void

.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-corpus
schema-cpp-generated-clang-tidy-weverything-gate-corpus: bin/schema
	@$(MAKE) --no-print-directory "$(SERIALIZE_RUNTIME)/serialize.h"
	@rm -rf "$(SCHEMA_CPP_CTW_CORPUS)"
	@mkdir -p "$(SCHEMA_CPP_CTW_CORPUS)"
	@set -e; for src in $(SCHEMA_CPP_CTW_CORPUS_DIRS); do \
		slug=$$(echo "$$src" | sed 's/[^A-Za-z0-9]/_/g'); \
		./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_CORPUS)/$$slug" "$$src"; \
	done
	@set -e; for src in $(SCHEMA_CPP_CTW_CORPUS_SCHEMAS); do \
		slug=$$(echo "$$src" | sed 's/[^A-Za-z0-9]/_/g'); \
		./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_CORPUS)/$$slug" "$$src"; \
	done
	@mkdir -p "$(SCHEMA_CPP_CTW_CORPUS)/guard"
	@./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_CORPUS)/guard" test/guard/Alpha.schema
	@./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_CORPUS)/guard" test/guard/Beta.schema
	# The bench corpus is THREE units, and the paired one is two files: the
	# fixed-table wrapper's body names BenchMixed from its sibling, so the pair
	# is generated together (one package per unit, SPEC §3.2).
	@mkdir -p "$(SCHEMA_CPP_CTW_CORPUS)/bench_paired" "$(SCHEMA_CPP_CTW_CORPUS)/bench_table" "$(SCHEMA_CPP_CTW_CORPUS)/bench_realworld"
	@./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_CORPUS)/bench_paired" bench/corpus/Bench.schema bench/corpus/FixedTable.schema
	@./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_CORPUS)/bench_table" bench/corpus/BenchTable.schema
	@./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_CORPUS)/bench_realworld" bench/corpus/RealWorld.schema
	@echo "cpp clang gate corpus: $$(find "$(SCHEMA_CPP_CTW_CORPUS)" -name '*.h' | wc -l) headers in $$(find "$(SCHEMA_CPP_CTW_CORPUS)" -mindepth 1 -maxdepth 1 -type d | wc -l) units"

# The checker half. It reads an already generated corpus so the prepared
# negative control can hand it a tree the generator never saw; the main target
# below generates first.
.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-check
schema-cpp-generated-clang-tidy-weverything-gate-check:
	@command -v "$(SCHEMA_CPP_CTW_CLANG_TIDY)" >/dev/null 2>&1 || { \
		echo "tool missing: clang-tidy"; \
		echo "  pin: $(SCHEMA_CPP_CTW_TIDY_PIN)"; \
		echo "  install it through the fleet play (rowan-tools/fleet)"; \
		exit 1; }
	@command -v "$(SCHEMA_CPP_CTW_CLANG)" >/dev/null 2>&1 || { \
		echo "tool missing: clang"; echo "  pin: $(SCHEMA_CPP_CTW_CLANG_PIN)"; exit 1; }
	@if ! "$(SCHEMA_CPP_CTW_CLANG_TIDY)" --version 2>&1 | grep -q "$(SCHEMA_CPP_CTW_TIDY_PIN)"; then \
		echo "tool version mismatch: clang-tidy"; \
		"$(SCHEMA_CPP_CTW_CLANG_TIDY)" --version 2>&1 | head -1; \
		echo "  want: $(SCHEMA_CPP_CTW_TIDY_PIN)"; exit 1; fi
	@if ! "$(SCHEMA_CPP_CTW_CLANG)" --version 2>&1 | grep -q "$(SCHEMA_CPP_CTW_CLANG_PIN)"; then \
		echo "tool version mismatch: clang"; \
		"$(SCHEMA_CPP_CTW_CLANG)" --version 2>&1 | head -1; \
		echo "  want: $(SCHEMA_CPP_CTW_CLANG_PIN)"; exit 1; fi
	@test -d "$(SCHEMA_CPP_CTW_CORPUS)" || { echo "no cpp gate corpus at $(SCHEMA_CPP_CTW_CORPUS)"; exit 1; }
	@rm -rf "$(SCHEMA_CPP_CTW_WORK)"; mkdir -p "$(SCHEMA_CPP_CTW_WORK)/tu"
	@set -e; for unit in "$(SCHEMA_CPP_CTW_CORPUS)"/*/; do \
		name=$$(basename "$$unit"); \
		{ echo '#include <cstddef>'; \
		  for h in "$$unit"*.h; do [ -e "$$h" ] && echo "#include \"$$(basename "$$h")\""; done; \
		} > "$(SCHEMA_CPP_CTW_WORK)/tu/$$name.h.cpp"; \
	done
	@set -e; fail=0; \
	for tu in "$(SCHEMA_CPP_CTW_WORK)"/tu/*.h.cpp; do \
		name=$$(basename "$$tu" .h.cpp); unit="$(SCHEMA_CPP_CTW_CORPUS)/$$name"; \
		"$(SCHEMA_CPP_CTW_CLANG)" -std=c++17 -Weverything -Werror $(SCHEMA_CPP_CTW_WARNINGS) \
			-Wno-system-headers -fsyntax-only -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" "$$tu" || fail=1; \
		"$(SCHEMA_CPP_CTW_CLANG_TIDY)" --checks="$(SCHEMA_CPP_CTW_TIDY_CHECKS)" \
			--warnings-as-errors='*' --system-headers=false "$$tu" -- \
			-std=c++17 -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" $(SCHEMA_CPP_CTW_WARNINGS) || fail=1; \
	done; \
	for unit in "$(SCHEMA_CPP_CTW_CORPUS)"/*/; do \
		name=$$(basename "$$unit"); \
		for cpp in "$$unit"*.cpp; do \
			[ -e "$$cpp" ] || continue; \
			"$(SCHEMA_CPP_CTW_CLANG)" -std=c++17 -Weverything -Werror $(SCHEMA_CPP_CTW_WARNINGS) \
				-Wno-system-headers -fsyntax-only -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" "$$cpp" || fail=1; \
			"$(SCHEMA_CPP_CTW_CLANG_TIDY)" --checks="$(SCHEMA_CPP_CTW_TIDY_CHECKS)" \
				--warnings-as-errors='*' --system-headers=false "$$cpp" -- \
				-std=c++17 -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" $(SCHEMA_CPP_CTW_WARNINGS) || fail=1; \
		done; \
	done; \
	exit $$fail

.PHONY: schema-cpp-generated-clang-tidy-weverything-gate
schema-cpp-generated-clang-tidy-weverything-gate: schema-cpp-generated-clang-tidy-weverything-gate-corpus
	@$(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate-check \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_CORPUS)" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_WORK)" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)"

# THE FAST LANE: two units on push. It is the same checker and the same
# exceptions, over the packet and table proving grounds, so a compiler or
# checker bump is seen in a minute rather than at the next nightly.
.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-fast
schema-cpp-generated-clang-tidy-weverything-gate-fast:
	@$(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_FAST_CORPUS)" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_FAST_WORK)" \
		SCHEMA_CPP_CTW_CORPUS_DIRS="$(SCHEMA_CPP_CTW_FAST_DIRS)" \
		SCHEMA_CPP_CTW_CORPUS_SCHEMAS="" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)"

# THE PLANTED CONTROL. A scratch corpus (the generated tree is never touched)
# carries one use-after-move, a bugprone lifetime pattern the pinned checker
# reports on the line that names schema_ctw_plant. The same check target must
# exit non-zero and name the plant; the clean twin must exit zero. The Go test
# in internal/ci drives this recipe with fake tools on every `go test`, so the
# plumbing is red/green without a checker on the host; the nightly workflow
# drives it with the real pinned checker.
.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-plant
schema-cpp-generated-clang-tidy-weverything-gate-plant:
	@rm -rf "$(SCHEMA_CPP_CTW_WORK)/plant-bad" "$(SCHEMA_CPP_CTW_WORK)/plant-clean"
	@mkdir -p "$(SCHEMA_CPP_CTW_WORK)/plant-bad/plant" "$(SCHEMA_CPP_CTW_WORK)/plant-clean/plant"
	@printf '%s\n' '#include <utility>' \
		'struct schema_ctw_plant_record { int x; };' \
		'inline int schema_ctw_plant_use( schema_ctw_plant_record & record )' \
		'{' \
		'    schema_ctw_plant_record moved = std::move( record );' \
		'    (void) moved;' \
		'    return record.x;' \
		'}' \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-bad/plant/plant.cpp"
	@printf '%s\n' '// the plant unit has a header so the all-header TU is built' > "$(SCHEMA_CPP_CTW_WORK)/plant-bad/plant/plant.h"
	@printf '%s\n' 'struct schema_ctw_clean_record { int x; };' \
		'inline int schema_ctw_clean_use( schema_ctw_clean_record & record ) { return record.x; }' \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-clean/plant/plant.cpp"
	@printf '%s\n' '// the clean unit has a header so the all-header TU is built' > "$(SCHEMA_CPP_CTW_WORK)/plant-clean/plant/plant.h"
	@if $(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate-check \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_WORK)/plant-bad" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_WORK)/plant-bad-work" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)" \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log" 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the cpp clang gate stayed green with the plant in"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"; exit 1; fi
	@grep -q 'schema_ctw_plant' "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log" || { \
		echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the named plant"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"; exit 1; }
	@$(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate-check \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_WORK)/plant-clean" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_WORK)/plant-clean-work" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)" \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-clean.log" 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the clean twin did not pass"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-clean.log"; exit 1; }
	@grep -m1 'schema_ctw_plant' "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"
	@echo "cpp clang gate negative control: the named plant turns the gate red, the clean twin stays green"

# The negative-controls tool names a target by the marker in its name; this
# alias is what make/negative-controls.json excludes, because the control needs
# the pinned clang-tidy that only the lens workflow installs.
.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-plant-negative-control
schema-cpp-generated-clang-tidy-weverything-gate-plant-negative-control: schema-cpp-generated-clang-tidy-weverything-gate-plant
