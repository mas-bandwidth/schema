# make/schema-java-generated-checker-signedness.mk — the Java lens.
#
# THE LENS. Schema emits the same algorithm in many languages, so every
# language's strictest checker, run over the generated output for the schema
# repository's test-schema corpus, is a separate lens on bugs. This fragment is
# the Checker Framework SIGNEDNESS lens over the generated Java: unsigned bytes,
# shorts and ints held in signed Java types are a prime bug class, so the
# generator's output must carry @Unsigned/@Signed and this target must let the
# checker prove every widening, comparison and shift over it.
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
# THE CORPUS IS THE MAKEFILE'S, NOT A SECOND ONE. Every schema the Java generate
# targets read is covered by the five generation stamps below: examples,
# examples128, the bench corpus, the tables corpus (every unit
# build/tables-generated-java/.stamp names) and the packet corpus
# (build/java-clean-packet/.stamp). Checker runs per output root, because each
# root is a self-contained set of packages.
#
# COST. The checker over the whole generated tree is minutes, so it is a nightly
# instrument in .github/workflows/schema-java-generated-checker-signedness.yml;
# the per-push half is the hermetic Go test that holds this fragment's wiring
# (internal/ci/schema_java_generated_checker_signedness_test.go).

CHECKER_FRAMEWORK_VERSION ?= 4.3.1
CHECKER_FRAMEWORK_HOME ?= /opt/checker-framework/checker-framework-$(CHECKER_FRAMEWORK_VERSION)
CHECKER_JAVAC           := $(CHECKER_FRAMEWORK_HOME)/checker/bin/javac
CHECKER_QUAL            := $(CHECKER_FRAMEWORK_HOME)/checker/dist/checker-qual.jar
CHECKER_SIGNEDNESS      := org.checkerframework.checker.signedness.SignednessChecker
CHECKER_ANNOTATION      := org.checkerframework.checker.signedness.qual.Unsigned

CHECKER_BUILD := build/schema-java-generated-checker-signedness
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

.PHONY: schema-java-generated-checker-signedness
schema-java-generated-checker-signedness: schema-java-generated-checker-signedness-tool $(CHECKER_STAMPS)
	@rm -rf $(CHECKER_BUILD) && mkdir -p $(CHECKER_BUILD)/classes
	@if ! grep -rlq '$(CHECKER_ANNOTATION)' $(CHECKER_ROOTS); then \
		echo "schema-java-generated-checker-signedness: REFUSES a vacuous pass: the generated Java carries no @Unsigned annotations"; \
		echo "  the Signedness Checker sees every primitive as signed, so a green line here would certify nothing"; \
		echo "  PATHS-PROPOSED: internal/codegen/java, internal/codegen/javatable (annotate the emitted types)"; \
		exit 1; \
	fi
	@set -e; for root in $(CHECKER_ROOTS); do \
		src=$$(find $$root -name '*.java' -print | sort); \
		[ -n "$$src" ] || { echo "schema-java-generated-checker-signedness: no generated Java under $$root"; exit 1; }; \
		mkdir -p $(CHECKER_BUILD)/classes/$$(echo $$root | tr / -); \
		echo "signedness checker: $$root ($$(echo "$$src" | wc -l | tr -d ' ') files)"; \
		if ! "$(CHECKER_JAVAC)" -cp "$(CHECKER_QUAL)" \
				-processor $(CHECKER_SIGNEDNESS) -Anomsgtext -Xmaxerrs 10000 \
				-d $(CHECKER_BUILD)/classes/$$(echo $$root | tr / -) $$src \
				> $(CHECKER_BUILD)/$$(echo $$root | tr / -).log 2>&1; then \
			cat $(CHECKER_BUILD)/$$(echo $$root | tr / -).log; \
			echo "schema-java-generated-checker-signedness: FINDINGS under $$root"; \
			exit 1; \
		fi; \
		cat $(CHECKER_BUILD)/$$(echo $$root | tr / -).log; \
	done
	@echo "signedness checker: the whole generated Java corpus passes"

# THE NEGATIVE CONTROL, and it plants what the tool is for: a @Unsigned int
# returned where a @Signed int is expected, in a scratch file beside a scratch
# classpath — never in generated code. The checker must go red and name the
# planted file, so a green main target is a tool that looked, not a tool that
# was absent or asleep.
CHECKER_PLANT := $(CHECKER_BUILD)/plant/SignednessPlant.java
.PHONY: schema-java-generated-checker-signedness-negative-control
schema-java-generated-checker-signedness-negative-control: schema-java-generated-checker-signedness-tool
	@rm -rf $(CHECKER_BUILD)/plant && mkdir -p $(CHECKER_BUILD)/plant/classes
	@printf '%s\n' \
		'import org.checkerframework.checker.signedness.qual.Unsigned;' \
		'' \
		'final class SignednessPlant {' \
		'    static int widen(@Unsigned int wire) {' \
		'        return wire; // plant: unsigned wire into a signed Java int' \
		'    }' \
		'}' > $(CHECKER_PLANT)
	@if "$(CHECKER_JAVAC)" -cp "$(CHECKER_QUAL)" \
			-processor $(CHECKER_SIGNEDNESS) -Anomsgtext \
			-d $(CHECKER_BUILD)/plant/classes $(CHECKER_PLANT) \
			> $(CHECKER_BUILD)/plant.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the signedness checker accepted a planted unsigned-to-signed widening"; \
		cat $(CHECKER_BUILD)/plant.log; exit 1; \
	fi
	@grep -q 'SignednessPlant.java' $(CHECKER_BUILD)/plant.log || { \
		echo "NEGATIVE CONTROL FAILED: the checker went red, but did not name the plant"; \
		cat $(CHECKER_BUILD)/plant.log; exit 1; }
	@grep -m1 'SignednessPlant.java' $(CHECKER_BUILD)/plant.log
	@echo "negative control: an unsigned int returned as a signed int turns the signedness checker RED and names the plant"
