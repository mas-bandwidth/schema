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
# The tool is pinned and probed. Missing or mismatched tools emit the fleet
# TOOL-MISSING record with the runner name and fail closed. A gate that passes
# while its checker is missing has no blade. Install the pin through the fleet play
# (rowan-tools/fleet); never hand-copy a binary onto one machine.

# Pins. The fleet play provisions the same major.minor; a failed probe reports
# the installed version, so a fleet bump is a visible edit and not a silent
# behaviour change.
SCHEMA_CPP_CTW_TIDY_PIN     ?= 18.1
SCHEMA_CPP_CTW_CLANG_PIN    ?= 18.1
SCHEMA_CPP_CTW_CLANG_TIDY   ?= clang-tidy
SCHEMA_CPP_CTW_CLANG        ?= clang++

# The corpus and its work directory. Both are overridable so the prepared
# negative control and the fast lane drive the SAME recipe over their own tree.
SCHEMA_CPP_CTW_CORPUS ?= build/cpp-ctw-corpus
SCHEMA_CPP_CTW_WORK   ?= build/cpp-ctw-work
SCHEMA_CPP_CTW_RATCHET ?= make/checks/schema-cpp-generated-clang-tidy-weverything-gate.ratchet.tsv

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

# Exceptions below cannot apply to the C++17 wire ABI. All other warnings and
# bugprone/cert/cppcoreguidelines checks remain enabled; their measured residue
# belongs in the per-check ratchet, rather than a suppression list.
# c++98-compat, c++98-compat-pedantic, pre-c++17-compat: this port targets C++17.
# padded: wire structs deliberately follow wire layout, not host padding advice.
SCHEMA_CPP_CTW_WARNINGS := \
	-Wno-c++98-compat -Wno-c++98-compat-pedantic -Wno-pre-c++17-compat \
	-Wno-padded
SCHEMA_CPP_CTW_TIDY_CHECKS := -*,bugprone-*,cert-*,cppcoreguidelines-*

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
	@set -eu; runner="$${RUNNER_NAME:-unknown}"; \
	if ! command -v "$(SCHEMA_CPP_CTW_CLANG_TIDY)" >/dev/null 2>&1; then \
		printf 'TOOL-MISSING clang-tidy %s missing RUNNER_NAME=%s\n' "$(SCHEMA_CPP_CTW_TIDY_PIN)" "$$runner" >&2; \
		exit 1; fi; \
	actual=$("$(SCHEMA_CPP_CTW_CLANG_TIDY)" --version 2>&1 | head -1); \
	echo "$$actual" | grep -Fq "$(SCHEMA_CPP_CTW_TIDY_PIN)" || { \
		printf 'TOOL-MISSING clang-tidy %s %s RUNNER_NAME=%s\n' "$(SCHEMA_CPP_CTW_TIDY_PIN)" "$$actual" "$$runner" >&2; exit 1; }; \
	printf 'tool verified: clang-tidy %s RUNNER_NAME=%s\n' "$(SCHEMA_CPP_CTW_TIDY_PIN)" "$$runner"
	@set -eu; runner="$${RUNNER_NAME:-unknown}"; \
	if ! command -v "$(SCHEMA_CPP_CTW_CLANG)" >/dev/null 2>&1; then \
		printf 'TOOL-MISSING clang++ %s missing RUNNER_NAME=%s\n' "$(SCHEMA_CPP_CTW_CLANG_PIN)" "$$runner" >&2; \
		exit 1; fi; \
	actual=$("$(SCHEMA_CPP_CTW_CLANG)" --version 2>&1 | head -1); \
	echo "$$actual" | grep -Fq "$(SCHEMA_CPP_CTW_CLANG_PIN)" || { \
		printf 'TOOL-MISSING clang++ %s %s RUNNER_NAME=%s\n' "$(SCHEMA_CPP_CTW_CLANG_PIN)" "$$actual" "$$runner" >&2; exit 1; }; \
	printf 'tool verified: clang++ %s RUNNER_NAME=%s\n' "$(SCHEMA_CPP_CTW_CLANG_PIN)" "$$runner"
	@test -d "$(SCHEMA_CPP_CTW_CORPUS)" || { echo "no cpp gate corpus at $(SCHEMA_CPP_CTW_CORPUS)"; exit 1; }
	@rm -rf "$(SCHEMA_CPP_CTW_WORK)"; mkdir -p "$(SCHEMA_CPP_CTW_WORK)/tu"
	@ls -d "$(SCHEMA_CPP_CTW_CORPUS)"/*/ >/dev/null 2>&1 || { echo "cpp clang gate FAILED: the corpus at $(SCHEMA_CPP_CTW_CORPUS) has no units"; exit 1; }
	@set -e; for unit in "$(SCHEMA_CPP_CTW_CORPUS)"/*/; do \
		name=$$(basename "$$unit"); \
		{ echo '#include <cstddef>'; \
		  for h in "$$unit"*.h; do [ -e "$$h" ] && echo "#include \"$$(basename "$$h")\""; done; \
		} > "$(SCHEMA_CPP_CTW_WORK)/tu/$$name.h.cpp"; \
	done
	@mkdir -p "$(SCHEMA_CPP_CTW_WORK)/logs"
	# Findings are warnings here, then the ratchet decides whether they fail.
	# A nonzero checker exit is an incomplete traversal, even if it printed an
	# ordinary finding before a fatal parse/tool error: refuse it unconditionally.
	@printf '%s\n' '#!/bin/sh' \
		'set -eu; src="$$1"; unit="$$2"; log="$$3"' \
		'"$(SCHEMA_CPP_CTW_CLANG)" -std=c++17 -Weverything $(SCHEMA_CPP_CTW_WARNINGS) -Wno-system-headers -fsyntax-only -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" "$$src" > "$$log.clang" 2>&1 || { cat "$$log.clang"; exit 1; }' \
		'"$(SCHEMA_CPP_CTW_CLANG_TIDY)" --checks="$(SCHEMA_CPP_CTW_TIDY_CHECKS)" --header-filter="$(SCHEMA_CPP_CTW_HEADER_FILTER)" --system-headers=false "$$src" -- -std=c++17 -I"$$unit" -Itest -Itest/tables -isystem "$(SERIALIZE_RUNTIME)" $(SCHEMA_CPP_CTW_WARNINGS) > "$$log.tidy" 2>&1 || { cat "$$log.tidy"; exit 1; }' \
		'exit 0' > "$(SCHEMA_CPP_CTW_WORK)/check-one.sh"

	@: > "$(SCHEMA_CPP_CTW_WORK)/jobs"; index=0; \
	for tu in "$(SCHEMA_CPP_CTW_WORK)"/tu/*.h.cpp; do \
		index=$$((index+1)); echo "$$tu $(SCHEMA_CPP_CTW_CORPUS)/$$(basename "$$tu" .h.cpp) $(SCHEMA_CPP_CTW_WORK)/logs/$$index" >> "$(SCHEMA_CPP_CTW_WORK)/jobs"; \
	done; \
	for unit in "$(SCHEMA_CPP_CTW_CORPUS)"/*/; do \
		for cpp in "$$unit"*.cpp; do \
			if [ -e "$$cpp" ]; then index=$$((index+1)); echo "$$cpp $$unit $(SCHEMA_CPP_CTW_WORK)/logs/$$index" >> "$(SCHEMA_CPP_CTW_WORK)/jobs"; fi; \
		done; \
	done
	@echo "cpp clang gate: $$(wc -l < "$(SCHEMA_CPP_CTW_WORK)/jobs") translation units, $(SCHEMA_CPP_CTW_JOBS) at a time"
	@xargs -P "$(SCHEMA_CPP_CTW_JOBS)" -L 1 sh "$(SCHEMA_CPP_CTW_WORK)/check-one.sh" < "$(SCHEMA_CPP_CTW_WORK)/jobs" || { \
		echo "cpp clang gate FAILED: checker exited before completing its corpus unit"; exit 1; }
	@LC_ALL=C awk ' \
		/:[0-9]+:[0-9]+: (warning|error): / && /\[[^][]+\][[:space:]]*$$/ { \
			check=$$0; sub(/^.*\[/, "", check); sub(/\].*$$/, "", check); \
			n=split(check, parts, ","); check=parts[1]; \
			if (check=="-Werror" && n>1) check=parts[2]; \
			loc=$$0; sub(/: (warning|error): .*$$/, "", loc); \
			if (check !~ /^(bugprone|cert|cppcoreguidelines)-/ && check !~ /^-W/) next; \
			key=check SUBSEP loc; if (!(key in seen)) { seen[key]=1; count[check]++ } \
		} \
		END { for (check in count) print check "\t" count[check] }' "$(SCHEMA_CPP_CTW_WORK)"/logs/* > "$(SCHEMA_CPP_CTW_WORK)/counts.unsorted.tsv"
	@LC_ALL=C sort "$(SCHEMA_CPP_CTW_WORK)/counts.unsorted.tsv" > "$(SCHEMA_CPP_CTW_WORK)/counts.tsv"
	@test -f "$(SCHEMA_CPP_CTW_RATCHET)" || { echo "cpp clang gate FAILED: missing per-check ratchet $(SCHEMA_CPP_CTW_RATCHET)"; exit 1; }
	@awk -F '\t' ' \
		FILENAME==ARGV[1] { if ($$1=="" || $$1 ~ /^#/) next; if (NF!=2 || $$2 !~ /^[0-9]+$$/ || ($$1 in pin)) { print "cpp clang gate FAILED: malformed ratchet entry " $$0; bad=1; next } pin[$$1]=$$2+0; next } \
		{ if (NF!=2 || $$2 !~ /^[0-9]+$$/) { print "cpp clang gate FAILED: malformed measured count " $$0; bad=1; next } seen[$$1]=1; actual=$$2+0; if (!($$1 in pin) || actual>pin[$$1]) { print "cpp clang gate FAILED: " $$1 " pinned " (($$1 in pin)?pin[$$1]:"new") " measured " actual; bad=1 } else if (actual<pin[$$1]) print "cpp clang gate LOWER PIN: " $$1 " pinned " pin[$$1] " measured " actual } \
		END { for (check in pin) if (!(check in seen) && pin[check]>0) print "cpp clang gate LOWER PIN: " check " pinned " pin[check] " measured 0"; if (bad) exit 1 }' "$(SCHEMA_CPP_CTW_RATCHET)" "$(SCHEMA_CPP_CTW_WORK)/counts.tsv"

.PHONY: schema-cpp-generated-clang-tidy-weverything-gate
schema-cpp-generated-clang-tidy-weverything-gate: schema-cpp-generated-clang-tidy-weverything-gate-corpus
	@awk -F '\t' '$$1 !~ /^#/ && NF==2 { found=1 } END { if (!found) { print "cpp clang gate FAILED: uncalibrated whole-corpus ratchet"; exit 1 } }' "$(SCHEMA_CPP_CTW_RATCHET)"
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
	@$(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate-check \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_WORK)/plant-clean" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_WORK)/plant-clean-work" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)" \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-clean.log" 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the clean generated unit did not pass"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-clean.log"; exit 1; }
	@if $(MAKE) --no-print-directory schema-cpp-generated-clang-tidy-weverything-gate-check \
		SCHEMA_CPP_CTW_CORPUS="$(SCHEMA_CPP_CTW_WORK)/plant-bad" \
		SCHEMA_CPP_CTW_WORK="$(SCHEMA_CPP_CTW_WORK)/plant-bad-work" \
		SCHEMA_CPP_CTW_RATCHET="$(SCHEMA_CPP_CTW_WORK)/plant-clean-work/counts.tsv" \
		SCHEMA_CPP_CTW_CLANG="$(SCHEMA_CPP_CTW_CLANG)" \
		SCHEMA_CPP_CTW_CLANG_TIDY="$(SCHEMA_CPP_CTW_CLANG_TIDY)" \
		> "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log" 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the cpp clang gate stayed green with the plant in a generated header"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"; exit 1; fi
	@grep -E '$(SCHEMA_CPP_CTW_PLANT_HEADER):[0-9]+:[0-9]+: (warning|error): .schema_ctw_plant. used after it was moved' "$(SCHEMA_CPP_CTW_WORK)/plant-bad-work"/logs/*.tidy >/dev/null || { \
		echo "NEGATIVE CONTROL FAILED: the gate went red, but not on the plant in the generated header"; \
		cat "$(SCHEMA_CPP_CTW_WORK)/plant-bad.log"; exit 1; }
	@grep -m1 -E '$(SCHEMA_CPP_CTW_PLANT_HEADER):[0-9]+:[0-9]+: (warning|error): .schema_ctw_plant. used after it was moved' "$(SCHEMA_CPP_CTW_WORK)/plant-bad-work"/logs/*.tidy
	@echo "cpp clang gate negative control: the plant in generated $(SCHEMA_CPP_CTW_PLANT_SOURCE)/$(SCHEMA_CPP_CTW_PLANT_HEADER) turns the gate red, the generated unit as emitted stays green"

# The negative-controls tool names a target by the marker in its name; this
# alias is what make/negative-controls.json excludes, because the control needs
# the pinned clang-tidy that only the lens workflow installs.
.PHONY: schema-cpp-generated-clang-tidy-weverything-gate-plant-negative-control
schema-cpp-generated-clang-tidy-weverything-gate-plant-negative-control: schema-cpp-generated-clang-tidy-weverything-gate-plant
