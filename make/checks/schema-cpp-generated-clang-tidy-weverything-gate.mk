# make/checks/schema-cpp-generated-clang-tidy-weverything-gate.mk — the C++ LENS: the
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
#
# THE HEADER FINDINGS. The generated readers and writers are headers, and
# clang-tidy reports nothing from a header without --header-filter: before the
# filter was added every one of these was "Suppressed ... in non-user code" and
# the gate was green over a corpus it had not read. Triaged one check at a time
# (the counts are unique file:line hits over the whole corpus, clang-tidy 18.1.8):
#
# cert-dcl16-c (8986)
#   The literal suffix is `ll`, emitted for every 64-bit constant; a lowercase
#   suffix is a readability rule, and the generator never emits a bare `l`.
# cppcoreguidelines-pro-type-union-access (8026)
#   The wire unions (the packet arms and the wide-text arm) are read through the
#   discriminator the decoder just checked; the union IS the wire layout.
# cppcoreguidelines-avoid-const-or-ref-data-members (3885)
#   The reference members belong to generated local helper structs (field
#   cursors, quantizer views, the depth guard) that never outlive the call.
# cppcoreguidelines-missing-std-forward (3750)
#   Every hit is an unnamed `auto &&` parameter of a generated visitor lambda
#   that ignores the argument; there is nothing to forward.
# bugprone-implicit-widening-of-multiplication-result (2736)
#   The products are loop index times a constant stride, with the loop bounded
#   by the schema's declared count, so the int product is far below INT_MAX.
# cppcoreguidelines-macro-usage (1782)
#   The macros are the generated wire helpers (TABLE_FLOAT_FORCE_ROUND and kin)
#   shared with the C port's text; they are the port's one portable spelling.
# cppcoreguidelines-owning-memory, cppcoreguidelines-no-malloc (1570, 572)
#   The arena and block allocators are the raw-memory layer on purpose: calloc
#   and free behind table_block_default_alloc, placement new into arena slabs.
# cppcoreguidelines-use-default-member-init (869)
#   The constructors initialise every member in one list beside the wire order;
#   splitting the defaults into the class body is a style rewrite.
# cert-err58-cpp (480)
#   The flagged statics are constant TableFieldInfo/TableInfo arrays built from
#   literals and addresses; nothing in their initialisers can throw.
# cppcoreguidelines-special-member-functions (443)
#   The builders define copy and rely on it for move; TableFixedDepth is a
#   one-instance local scope guard that is never copied or moved.
# bugprone-incorrect-roundings (293)
#   `(uint32_t)( scaled + 0.5f )` after TABLE_FLOAT_FORCE_ROUND and a clamp to
#   [0, count] is the cross-port quantization rule; every port must produce the
#   same index bit for bit, and lround would change ties against the other ports.
# bugprone-multi-level-implicit-pointer-conversion (142)
#   The `T **` to `void *` conversions are memcpy/memset of pointer arrays in the
#   node-entry tables, where the bytes are copied, not dereferenced.
# bugprone-reserved-identifier, cert-dcl37-c, cert-dcl51-cpp (70)
#   `cursor__new` and kin join a prefix and a schema field name. A double
#   underscore is reserved in every scope, so this is a real naming-standard
#   breach, but a benign one: the names are function-local locals no standard
#   library or compiler defines, so no collision is possible in practice (the
#   same verdict as -Wreserved-identifier); renaming is generator work.
# bugprone-sizeof-expression (46)
#   `sizeof( segment->dead ) / sizeof( segment->dead[0] )` is the array element
#   count idiom over a fixed C array member, not sizeof of a pointer.
# cppcoreguidelines-pro-type-reinterpret-cast (42)
#   char to uint8_t views for the UTF-8 and interior-NUL validators; the two
#   types alias the same bytes by the standard's char rule.
# bugprone-misplaced-widening-cast (20)
#   `(size_t)( 2 * value.text_length )` follows a schema_assert that bounds the
#   length by the declared size (single digits), so the int product cannot wrap.
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
	-bugprone-casting-through-void,\
	-cert-dcl16-c,\
	-cppcoreguidelines-pro-type-union-access,\
	-cppcoreguidelines-avoid-const-or-ref-data-members,\
	-cppcoreguidelines-missing-std-forward,\
	-bugprone-implicit-widening-of-multiplication-result,\
	-cppcoreguidelines-macro-usage,\
	-cppcoreguidelines-owning-memory,\
	-cppcoreguidelines-no-malloc,\
	-cppcoreguidelines-use-default-member-init,\
	-cert-err58-cpp,\
	-cppcoreguidelines-special-member-functions,\
	-bugprone-incorrect-roundings,\
	-bugprone-multi-level-implicit-pointer-conversion,\
	-bugprone-reserved-identifier,\
	-cert-dcl37-c,\
	-cert-dcl51-cpp,\
	-bugprone-sizeof-expression,\
	-cppcoreguidelines-pro-type-reinterpret-cast,\
	-bugprone-misplaced-widening-cast

# The generated readers and writers are HEADERS. clang-tidy only reports from
# the main file unless --header-filter names the headers, so without this every
# finding in the corpus is dropped as "non-user code" and the gate is green over
# nothing. Every non-system header is checked; serialize.h stays -isystem.
SCHEMA_CPP_CTW_HEADER_FILTER ?= .*

# The translation units are independent, so the check fans out: the whole
# corpus is ~570 units and takes about an hour one at a time. The default is
# every core; a shared bench passes a smaller number.
SCHEMA_CPP_CTW_JOBS ?= $(shell nproc 2>/dev/null || sysctl -n hw.ncpu 2>/dev/null || echo 2)

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
		echo "tool missing: clang"; \
		echo "  pin: $(SCHEMA_CPP_CTW_CLANG_PIN)"; \
		echo "  install it through the fleet play (rowan-tools/fleet)"; exit 1; }
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
	@ls -d "$(SCHEMA_CPP_CTW_CORPUS)"/*/ >/dev/null 2>&1 || { echo "cpp clang gate FAILED: the corpus at $(SCHEMA_CPP_CTW_CORPUS) has no units"; exit 1; }
	@set -e; for unit in "$(SCHEMA_CPP_CTW_CORPUS)"/*/; do \
		name=$$(basename "$$unit"); \
		{ echo '#include <cstddef>'; \
		  for h in "$$unit"*.h; do [ -e "$$h" ] && echo "#include \"$$(basename "$$h")\""; done; \
		} > "$(SCHEMA_CPP_CTW_WORK)/tu/$$name.h.cpp"; \
	done
	@printf '%s\n' '#!/bin/sh' \
		'# one translation unit: clang -Weverything, then clang-tidy, both always run' \
		'src="$$1"; unit="$$2"; fail=0' \
		'"$(SCHEMA_CPP_CTW_CLANG)" -std=c++17 -Weverything -Werror $(SCHEMA_CPP_CTW_WARNINGS) -Wno-system-headers -fsyntax-only -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" "$$src" || fail=1' \
		'"$(SCHEMA_CPP_CTW_CLANG_TIDY)" --checks="$(SCHEMA_CPP_CTW_TIDY_CHECKS)" --warnings-as-errors="*" --header-filter="$(SCHEMA_CPP_CTW_HEADER_FILTER)" --system-headers=false "$$src" -- -std=c++17 -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" $(SCHEMA_CPP_CTW_WARNINGS) || fail=1' \
		'exit $$fail' > "$(SCHEMA_CPP_CTW_WORK)/check-one.sh"
	@: > "$(SCHEMA_CPP_CTW_WORK)/jobs"; \
	for tu in "$(SCHEMA_CPP_CTW_WORK)"/tu/*.h.cpp; do \
		echo "$$tu $(SCHEMA_CPP_CTW_CORPUS)/$$(basename "$$tu" .h.cpp)" >> "$(SCHEMA_CPP_CTW_WORK)/jobs"; \
	done; \
	for unit in "$(SCHEMA_CPP_CTW_CORPUS)"/*/; do \
		for cpp in "$$unit"*.cpp; do \
			if [ -e "$$cpp" ]; then echo "$$cpp $$unit" >> "$(SCHEMA_CPP_CTW_WORK)/jobs"; fi; \
		done; \
	done
	@echo "cpp clang gate: $$(wc -l < "$(SCHEMA_CPP_CTW_WORK)/jobs") translation units, $(SCHEMA_CPP_CTW_JOBS) at a time"
	@xargs -P "$(SCHEMA_CPP_CTW_JOBS)" -L 1 sh "$(SCHEMA_CPP_CTW_WORK)/check-one.sh" < "$(SCHEMA_CPP_CTW_WORK)/jobs" || { \
		echo "cpp clang gate FAILED: a translation unit above has a finding"; exit 1; }

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

# THE PLANTED CONTROL, IN THE REAL GENERATED OUTPUT. The plant unit is the
# `examples` corpus generated fresh by bin/schema into scratch (the build tree
# is never touched). The clean twin is that generated unit as it is, and must
# pass the same check recipe. The bad twin is a copy with one use-after-move
# appended to a real generated HEADER ($(SCHEMA_CPP_CTW_PLANT_HEADER)), so the
# control proves the checker reads the generated headers through the same
# all-header translation unit the corpus uses; a plant in a hand-written .cpp
# would pass even when every header finding is dropped. The bad twin must go
# red on bugprone-use-after-move in that header, naming schema_ctw_plant.
# The Go test in internal/ci drives the check recipe with fake tools on every
# `go test`; the workflow drives this target with the real pinned checker.
SCHEMA_CPP_CTW_PLANT_SOURCE ?= examples
SCHEMA_CPP_CTW_PLANT_HEADER ?= Wire.h

.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-plant
schema-cpp-generated-clang-tidy-weverything-gate-plant: bin/schema
	@$(MAKE) --no-print-directory "$(SERIALIZE_RUNTIME)/serialize.h"
	@rm -rf "$(SCHEMA_CPP_CTW_WORK)/plant-bad" "$(SCHEMA_CPP_CTW_WORK)/plant-clean"
	@mkdir -p "$(SCHEMA_CPP_CTW_WORK)/plant-clean/plant"
	@./bin/schema generate --lang cpp --out "$(SCHEMA_CPP_CTW_WORK)/plant-clean/plant" $(SCHEMA_CPP_CTW_PLANT_SOURCE)
	@test -f "$(SCHEMA_CPP_CTW_WORK)/plant-clean/plant/$(SCHEMA_CPP_CTW_PLANT_HEADER)" || { \
		echo "NEGATIVE CONTROL FAILED: $(SCHEMA_CPP_CTW_PLANT_SOURCE) generated no $(SCHEMA_CPP_CTW_PLANT_HEADER) to plant in"; exit 1; }
	@cp -R "$(SCHEMA_CPP_CTW_WORK)/plant-clean" "$(SCHEMA_CPP_CTW_WORK)/plant-bad"
	@printf '%s\n' '' '#include <utility>' \
		'struct schema_ctw_plant_record { int x; };' \
		'inline int schema_ctw_plant_use( schema_ctw_plant_record & schema_ctw_plant )' \
		'{' \
		'    schema_ctw_plant_record moved = std::move( schema_ctw_plant );' \
		'    (void) moved;' \
		'    return schema_ctw_plant.x;' \
		'}' \
		>> "$(SCHEMA_CPP_CTW_WORK)/plant-bad/plant/$(SCHEMA_CPP_CTW_PLANT_HEADER)"
	@if $(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate-check \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_WORK)/plant-bad" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_WORK)/plant-bad-work" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)" \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log" 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the cpp clang gate stayed green with the plant in a generated header"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"; exit 1; fi
	@grep -E '$(SCHEMA_CPP_CTW_PLANT_HEADER):[0-9]+:[0-9]+: (warning|error): .schema_ctw_plant. used after it was moved' "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log" >/dev/null || { \
		echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the plant in the generated header"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"; exit 1; }
	@$(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate-check \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_WORK)/plant-clean" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_WORK)/plant-clean-work" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)" \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-clean.log" 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the clean generated unit did not pass"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-clean.log"; exit 1; }
	@grep -m1 -E '$(SCHEMA_CPP_CTW_PLANT_HEADER):[0-9]+:[0-9]+: (warning|error): .schema_ctw_plant. used after it was moved' "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"
	@echo "cpp clang gate negative control: the plant in generated $(SCHEMA_CPP_CTW_PLANT_SOURCE)/$(SCHEMA_CPP_CTW_PLANT_HEADER) turns the gate red, the generated unit as emitted stays green"

# The negative-controls tool names a target by the marker in its name; this
# alias is what make/negative-controls.json excludes, because the control needs
# the pinned clang-tidy that only the lens workflow installs.
.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-plant-negative-control
schema-cpp-generated-clang-tidy-weverything-gate-plant-negative-control: schema-cpp-generated-clang-tidy-weverything-gate-plant
