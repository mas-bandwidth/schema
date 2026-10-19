# make/schema-java-generated-checker-signedness.mk — the Java signedness lens.
#
# THE LENS. Schema emits the same algorithm in many languages, so every
# language's strictest checker, run over the generated output for the schema
# repository's test-schema corpus, is a separate lens on bugs. This fragment is
# the Checker Framework SIGNEDNESS lens over the generated Java: unsigned bytes,
# shorts and ints held in signed Java types are a prime bug class, so the
# generator's --java-checker-qual mode emits @Unsigned/@Signed on primitive
# integer storage and this target runs the Signedness Checker over every corpus
# root.
#
# TOOLCHAIN. The Checker Framework, pinned here and installed fleet-wide from
# its official source through rowan-tools/fleet's ansible play — never by hand
# on one machine. The pin is the version the gate certifies; CHECKER_FRAMEWORK_HOME
# is the unpacked release's root, so the runnable javac is
# $(CHECKER_FRAMEWORK_HOME)/checker/bin/javac and the qualifiers are
# $(CHECKER_FRAMEWORK_HOME)/checker/dist/checker-qual.jar.
#
# THE GATE REFUSES A VACUOUS PASS. Without @Unsigned on the generated types the
# Signedness Checker sees every primitive as signed and certifies nothing; this
# target greps the generated corpus for the annotation and refuses by name when
# it is absent, rather than printing a green line over an unannotated tree.
#
# RATCHET. Per-root, per-check counts at head live in
# make/checks/schema-java-generated-checker-signedness.ratchet.tsv, keyed by
# root and check name (never line numbers). Every root is checked without
# stopping at the first; a raise or a new check goes red, a drop is reported.

CHECKER_FRAMEWORK_VERSION ?= 4.3.1
CHECKER_FRAMEWORK_HOME ?= /opt/checker-framework/checker-framework-$(CHECKER_FRAMEWORK_VERSION)
CHECKER_JAVAC           := $(CHECKER_FRAMEWORK_HOME)/checker/bin/javac
CHECKER_QUAL            := $(CHECKER_FRAMEWORK_HOME)/checker/dist/checker-qual.jar
CHECKER_SIGNEDNESS      := org.checkerframework.checker.signedness.SignednessChecker
CHECKER_ANNOTATION      := org.checkerframework.checker.signedness.qual.Unsigned

CHECKER_BUILD   ?= build/schema-java-generated-checker-signedness
CHECKER_RATCHET ?= make/checks/schema-java-generated-checker-signedness.ratchet.tsv
CHECKER_ACTUAL  ?= $(CHECKER_BUILD)/actual.tsv
CHECKER_JOBS    ?= 16

CHECKER_ROOTS := generated/java generated/java-ludicrous generated/bench/java \
	build/tables-generated-java build/java-clean-packet
CHECKER_STAMPS := generated/java/.stamp generated/java-ludicrous/.stamp \
	generated/bench/java/.stamp build/tables-generated-java/.stamp \
	build/java-clean-packet/.stamp

# The tool probe is its own target so the CI probe step and the Go test can ask
# the question without paying for generation. A missing tool is REPORTED as
# `tool missing: <name>` and is not a green line.
.PHONY: schema-java-generated-checker-signedness-tool
schema-java-generated-checker-signedness-tool:
	@test -x "$(CHECKER_JAVAC)" || { \
		echo "tool missing: checker-framework $(CHECKER_FRAMEWORK_VERSION) ($(CHECKER_JAVAC))"; \
		echo "  install it fleet-wide from https://checkerframework.org/checker-framework-$(CHECKER_FRAMEWORK_VERSION).zip"; \
		echo "  through rowan-tools/fleet's ansible play, or override CHECKER_FRAMEWORK_HOME"; \
		exit 1; }
	@test -f "$(CHECKER_QUAL)" || { \
		echo "tool missing: checker-framework $(CHECKER_FRAMEWORK_VERSION) checker-qual.jar ($(CHECKER_QUAL))"; \
		exit 1; }
	@echo "checker-framework $(CHECKER_FRAMEWORK_VERSION): $(CHECKER_JAVAC)"

# Generate the five corpus roots with --java-checker-qual into $(CHECKER_BUILD)/src
# so default generated/ and build/ trees remain unannotated for standard javac.
$(CHECKER_BUILD)/src/.stamp: bin/schema make/java.mk make/schema-java-generated-checker-signedness.mk
	@rm -rf $(CHECKER_BUILD)/src && mkdir -p $(CHECKER_BUILD)/src
	@$(MAKE) --no-print-directory -n -B $(CHECKER_STAMPS) \
		| sed -n 's|^\./bin/schema generate --lang java --out |./bin/schema generate --lang java --java-checker-qual --out $(CHECKER_BUILD)/src/|p' \
		| sh -e
	@touch $@

# Compare measured per-root per-check counts against the committed ratchet.
# A raise or a new check turns the gate RED; a drop is reported.
.PHONY: schema-java-generated-checker-signedness-ratchet
schema-java-generated-checker-signedness-ratchet:
	@awk -F'\t' ' \
		NR==FNR { if ($$0 ~ /^#/ || NF < 3) next; \
			pinned[$$1 SUBSEP $$2]=$$3 + 0; keys[$$1 SUBSEP $$2]=1; next } \
		NF >= 3 { measured[$$1 SUBSEP $$2]=$$3 + 0; keys[$$1 SUBSEP $$2]=1 } \
		END { \
			bad=0; \
			for (k in keys) { \
				split(k, a, SUBSEP); \
				p = (k in pinned) ? pinned[k] : 0; \
				m = (k in measured) ? measured[k] : 0; \
				if (m > p) { \
					printf "RATCHET FAILED: %s: %s count %d > pinned %d\n", a[1], a[2], m, p; \
					bad=1; \
				} else if (m < p) { \
					printf "RATCHET DROP: %s: %s count %d < pinned %d\n", a[1], a[2], m, p; \
				} \
			} \
			if (bad) exit 1; \
		}' "$(CHECKER_RATCHET)" "$(CHECKER_ACTUAL)"

.PHONY: schema-java-generated-checker-signedness
schema-java-generated-checker-signedness: schema-java-generated-checker-signedness-tool $(CHECKER_BUILD)/src/.stamp
	@rm -rf $(CHECKER_BUILD)/classes $(CHECKER_BUILD)/logs && mkdir -p $(CHECKER_BUILD)/classes $(CHECKER_BUILD)/logs
	@: > $(CHECKER_ACTUAL)
	@if ! grep -rlq '$(CHECKER_ANNOTATION)' $(addprefix $(CHECKER_BUILD)/src/,$(CHECKER_ROOTS)); then \
		echo "schema-java-generated-checker-signedness: REFUSES a vacuous pass: the generated Java carries no @Unsigned annotations"; \
		echo "  the Signedness Checker sees every primitive as signed, so a green line here would certify nothing"; \
		exit 1; \
	fi
	@set -e; for root in $(CHECKER_ROOTS); do \
		srcdir="$(CHECKER_BUILD)/src/$$root"; \
		tag=$$(echo $$root | tr / -); \
		src=$$(find "$$srcdir" -name '*.java' -print | sort); \
		[ -n "$$src" ] || { echo "schema-java-generated-checker-signedness: no generated Java under $$root"; exit 1; }; \
		echo "signedness checker: $$root ($$(echo "$$src" | wc -l | tr -d ' ') files)"; \
		if [ "$$root" = "build/tables-generated-java" ] || [ "$$root" = "build/java-clean-packet" ]; then \
			mkdir -p "$(CHECKER_BUILD)/classes/$$tag" "$(CHECKER_BUILD)/logs/$$tag"; \
			find "$$srcdir" -mindepth 1 -maxdepth 1 -type d | sort | xargs -P $(CHECKER_JOBS) -I{} sh -c ' \
				u=$$(basename "$$1"); \
				mkdir -p "$(CHECKER_BUILD)/classes/'"$$tag"'/$$u"; \
				"$(CHECKER_JAVAC)" -cp "$(CHECKER_QUAL)" \
					-processor $(CHECKER_SIGNEDNESS) -Anomsgtext -Xmaxerrs 100000 -Xmaxwarns 100000 \
					-d "$(CHECKER_BUILD)/classes/'"$$tag"'/$$u" "$$1"/*.java \
					> "$(CHECKER_BUILD)/logs/'"$$tag"'/$$u.log" 2>&1 || true \
			' _ {}; \
			cat "$(CHECKER_BUILD)/logs/$$tag"/*.log > "$(CHECKER_BUILD)/$$tag.log"; \
		else \
			mkdir -p "$(CHECKER_BUILD)/classes/$$tag"; \
			"$(CHECKER_JAVAC)" -cp "$(CHECKER_QUAL)" \
				-processor $(CHECKER_SIGNEDNESS) -Anomsgtext -Xmaxerrs 100000 -Xmaxwarns 100000 \
				-d "$(CHECKER_BUILD)/classes/$$tag" $$src \
				> "$(CHECKER_BUILD)/$$tag.log" 2>&1 || true; \
		fi; \
		sed -nE 's/^.*: (error|warning): \[([a-z0-9._]+)\].*$$/\2/p' "$(CHECKER_BUILD)/$$tag.log" \
			| sort | uniq -c \
			| awk -v r="$$root" '{printf "%s\t%s\t%d\n", r, $$2, $$1}' >> "$(CHECKER_ACTUAL)"; \
	done
	@$(MAKE) --no-print-directory schema-java-generated-checker-signedness-ratchet
	@echo "signedness checker: all five generated Java corpus roots are within the ratchet ($(CHECKER_RATCHET))"

# THE NEGATIVE CONTROL. Part 1 plants a comparison.unsignedlhs and an unsigned-
# to-signed return in a scratch file and proves the Signedness Checker goes red
# and names both SignednessPlant.java and [comparison.unsignedlhs]. Part 2
# perturbs the ratchet's comparison.unsignedlhs count on generated/java by +1
# and proves the ratchet gate goes red naming comparison.unsignedlhs and both
# counts, while the unperturbed ratchet stays green.
CHECKER_PLANT := $(CHECKER_BUILD)/plant/SignednessPlant.java
.PHONY: schema-java-generated-checker-signedness-negative-control
schema-java-generated-checker-signedness-negative-control: schema-java-generated-checker-signedness-tool
	@rm -rf $(CHECKER_BUILD)/plant && mkdir -p $(CHECKER_BUILD)/plant/classes
	@printf '%s\n' \
		'import org.checkerframework.checker.signedness.qual.Unsigned;' \
		'' \
		'final class SignednessPlant {' \
		'    static boolean badCompare(@Unsigned int wire) {' \
		'        return wire < 0; // plant: signed ordering on an @Unsigned int' \
		'    }' \
		'    static int widen(@Unsigned int wire) {' \
		'        return wire; // plant: unsigned wire into a signed Java int' \
		'    }' \
		'}' > $(CHECKER_PLANT)
	@if "$(CHECKER_JAVAC)" -cp "$(CHECKER_QUAL)" \
			-processor $(CHECKER_SIGNEDNESS) -Anomsgtext \
			-d $(CHECKER_BUILD)/plant/classes $(CHECKER_PLANT) \
			> $(CHECKER_BUILD)/plant.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the signedness checker accepted a planted comparison.unsignedlhs"; \
		cat $(CHECKER_BUILD)/plant.log; exit 1; \
	fi
	@grep -q 'SignednessPlant.java' $(CHECKER_BUILD)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the checker went red, but did not name the plant"; \
		cat $(CHECKER_BUILD)/plant.log; exit 1; }
	@grep -q '\[comparison\.unsignedlhs\]' $(CHECKER_BUILD)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the checker went red, but did not report [comparison.unsignedlhs]"; \
		cat $(CHECKER_BUILD)/plant.log; exit 1; }
	@awk -F'\t' 'BEGIN{OFS="\t"} $$0 ~ /^#/ || NF < 3 {next} \
		$$1=="generated/java" && $$2=="comparison.unsignedlhs" {$$3=$$3+1} {print}' \
		$(CHECKER_RATCHET) > $(CHECKER_BUILD)/plant/actual-plus-one.tsv
	@awk -F'\t' 'BEGIN{OFS="\t"} $$0 ~ /^#/ || NF < 3 {next} {print}' \
		$(CHECKER_RATCHET) > $(CHECKER_BUILD)/plant/actual-at-pin.tsv
	@if $(MAKE) --no-print-directory schema-java-generated-checker-signedness-ratchet \
			CHECKER_ACTUAL=$(CHECKER_BUILD)/plant/actual-plus-one.tsv \
			> $(CHECKER_BUILD)/plant/ratchet-red.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the ratchet accepted a raised comparison.unsignedlhs count"; \
		cat $(CHECKER_BUILD)/plant/ratchet-red.log; exit 1; \
	fi
	@grep -q 'RATCHET FAILED: generated/java: comparison.unsignedlhs count 5 > pinned 4' $(CHECKER_BUILD)/plant/ratchet-red.log || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet did not name comparison.unsignedlhs and both counts"; \
		cat $(CHECKER_BUILD)/plant/ratchet-red.log; exit 1; }
	@awk -F'\t' 'BEGIN{OFS="\t"} $$0 ~ /^#/ || NF < 3 {next} \
		$$1=="generated/java" && $$2=="comparison.unsignedlhs" {$$3=$$3-1} {print}' \
		$(CHECKER_RATCHET) > $(CHECKER_BUILD)/plant/actual-minus-one.tsv
	@$(MAKE) --no-print-directory schema-java-generated-checker-signedness-ratchet \
		CHECKER_ACTUAL=$(CHECKER_BUILD)/plant/actual-minus-one.tsv \
		> $(CHECKER_BUILD)/plant/ratchet-drop.log 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet failed on a dropped count"; \
		cat $(CHECKER_BUILD)/plant/ratchet-drop.log; exit 1; }
	@grep -q 'RATCHET DROP: generated/java: comparison.unsignedlhs count 3 < pinned 4' $(CHECKER_BUILD)/plant/ratchet-drop.log || { \
		echo "NEGATIVE CONTROL FAILED: the ratchet did not report a dropped count"; \
		cat $(CHECKER_BUILD)/plant/ratchet-drop.log; exit 1; }
	@$(MAKE) --no-print-directory schema-java-generated-checker-signedness-ratchet \
		CHECKER_ACTUAL=$(CHECKER_BUILD)/plant/actual-at-pin.tsv \
		> $(CHECKER_BUILD)/plant/ratchet-green.log 2>&1 || { \
		echo "NEGATIVE CONTROL FAILED: the unperturbed ratchet pins did not pass"; \
		cat $(CHECKER_BUILD)/plant/ratchet-green.log; exit 1; }
	@grep -m1 'SignednessPlant.java' $(CHECKER_BUILD)/plant.log
	@echo "negative control: comparison.unsignedlhs and unsigned-to-signed return turn the signedness checker and ratchet RED and name the plant"
