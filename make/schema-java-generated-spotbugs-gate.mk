# make/schema-java-generated-spotbugs-gate.mk -- the SpotBugs lens on the
# generated Java (ROADMAP schema-per-language-lenses, Java/SpotBugs).
#
# SpotBugs is run at the strictest effort (-effort:max) and the medium
# threshold: high would drop PREDICTABLE_RANDOM, the find-sec-bugs detector
# this gate is proven to catch, and low is noise-dominated (6957 findings over
# this corpus, nearly all design/style reports over generated value classes).
# The include filter scopes the lens to CORRECTNESS and SECURITY -- a bug lens
# -- and the exclude filter carries three method-level suppressions, each with
# its reason in the file.
#
# Tool pins. Both are official releases: SpotBugs from github.com/spotbugs and
# find-sec-bugs' plugin from Maven Central, the project's published artifact.
# The target PROBES instead of installing: a bench without the tools says
# "tool missing: <name>" and stays red rather than passing over nothing.
SPOTBUGS_VERSION      ?= 4.10.5
FINDSECBUGS_VERSION   ?= 1.14.0
SPOTBUGS_HOME         ?=
SPOTBUGS              ?= $(if $(SPOTBUGS_HOME),$(SPOTBUGS_HOME)/bin/spotbugs,spotbugs)
SPOTBUGS_PLUGIN       ?= $(if $(SPOTBUGS_HOME),$(SPOTBUGS_HOME)/findsecbugs-plugin.jar,findsecbugs-plugin.jar)
SPOTBUGS_INCLUDE      := make/schema-java-generated-spotbugs-include.xml
SPOTBUGS_EXCLUDE      := make/schema-java-generated-spotbugs-exclude.xml
SPOTBUGS_WORK         := build/schema-java-spotbugs
SPOTBUGS_CLASSES      := $(SPOTBUGS_WORK)/classes
SPOTBUGS_FINDINGS     := $(SPOTBUGS_WORK)/findings.xml
SPOTBUGS_SOURCES      := $(SPOTBUGS_WORK)/sources.txt
JAVA                  ?= java
JAVAC                 ?= javac
# java.mk pins JAVAC at a repo-local dist/ JDK; on a bench without it fall back
# to the javac on PATH rather than failing 127.
SPOTBUGS_JAVAC        := $(if $(shell test -x "$(JAVAC)" && echo yes),$(JAVAC),javac)
SPOTBUGS_MIN_CLASSES  ?= 1000

# One invocation of the tool, shared by the gate and its negative control so a
# control cannot prove a different command from the one that certifies.
define spotbugs_run
	$(SPOTBUGS) -textui -effort:max -medium -exitcode -xml:withMessages \
		-include $(SPOTBUGS_INCLUDE) -exclude $(SPOTBUGS_EXCLUDE) \
		-pluginList $(SPOTBUGS_PLUGIN) -output $(1) $(2)
endef

# THE CORPUS is the one the Java leg's own generate targets read: the tables
# corpus (build/tables-generated-java/.stamp), examples (generated/java) and
# examples128 (generated/java-ludicrous). Nothing here invents a second corpus.
.PHONY: schema-java-generated-spotbugs-gate
schema-java-generated-spotbugs-gate: build/tables-generated-java/.stamp generated/java/.stamp generated/java-ludicrous/.stamp
	@command -v $(SPOTBUGS) >/dev/null 2>&1 || { \
		echo "tool missing: spotbugs $(SPOTBUGS_VERSION) (probe: $(SPOTBUGS))"; \
		echo "  install it fleet-wide from github.com/spotbugs/spotbugs, or set SPOTBUGS_HOME=/path/to/spotbugs-$(SPOTBUGS_VERSION)"; \
		exit 1; }
	@test -f "$(SPOTBUGS_PLUGIN)" || { \
		echo "tool missing: find-sec-bugs $(FINDSECBUGS_VERSION) (probe: $(SPOTBUGS_PLUGIN))"; \
		echo "  install the plugin from Maven Central com.h3xstream.findsecbugs:findsecbugs-plugin:$(FINDSECBUGS_VERSION), or set SPOTBUGS_PLUGIN=/path/to/findsecbugs-plugin.jar"; \
		exit 1; }
	@rm -rf $(SPOTBUGS_WORK) && mkdir -p $(SPOTBUGS_CLASSES)
	find build/tables-generated-java generated/java generated/java-ludicrous -name '*.java' | sort > $(SPOTBUGS_SOURCES)
	@n=$$(wc -l < $(SPOTBUGS_SOURCES) | tr -d ' '); \
		if [ "$$n" -lt $(SPOTBUGS_MIN_CLASSES) ]; then \
			echo "SPOTBUGS GATE FAILED: found $$n generated Java sources, expected at least $(SPOTBUGS_MIN_CLASSES) — the corpus, not the property, is what broke"; \
			exit 1; \
		fi; \
		echo "schema-java SpotBugs gate: compiling $$n generated Java sources"
	$(SPOTBUGS_JAVAC) --release 17 -nowarn -d $(SPOTBUGS_CLASSES) @$(SPOTBUGS_SOURCES)
	$(call spotbugs_run,$(SPOTBUGS_FINDINGS),$(SPOTBUGS_CLASSES))
	@echo "schema-java SpotBugs gate: (-effort:max -medium, CORRECTNESS + SECURITY) clean over $$(wc -l < $(SPOTBUGS_SOURCES) | tr -d ' ') generated sources; findings: $(SPOTBUGS_FINDINGS)"

# ITS NEGATIVE CONTROL. A tiny Java class plants `new Random()`, the pattern
# find-sec-bugs calls PREDICTABLE_RANDOM, and the SAME invocation the gate uses
# must exit non-zero and name it. The class is scratch input, never generated
# output and never a change to the emitter.
SPOTBUGS_NC := build/schema-java-spotbugs-negative-control
.PHONY: schema-java-generated-spotbugs-gate-negative-control
schema-java-generated-spotbugs-gate-negative-control:
	@command -v $(SPOTBUGS) >/dev/null 2>&1 || { \
		echo "tool missing: spotbugs $(SPOTBUGS_VERSION) (probe: $(SPOTBUGS))"; exit 1; }
	@test -f "$(SPOTBUGS_PLUGIN)" || { \
		echo "tool missing: find-sec-bugs $(FINDSECBUGS_VERSION) (probe: $(SPOTBUGS_PLUGIN))"; exit 1; }
	@rm -rf $(SPOTBUGS_NC) && mkdir -p $(SPOTBUGS_NC)/classes
	@printf '%s\n' \
		'public final class SpotbugsPlant {' \
		'    private static final java.util.Random RND = new java.util.Random();' \
		'    public static int token() { return RND.nextInt(); }' \
		'}' > $(SPOTBUGS_NC)/SpotbugsPlant.java
	$(SPOTBUGS_JAVAC) --release 17 -nowarn -d $(SPOTBUGS_NC)/classes $(SPOTBUGS_NC)/SpotbugsPlant.java
	@if $(call spotbugs_run,$(SPOTBUGS_NC)/findings.xml,$(SPOTBUGS_NC)/classes) > $(SPOTBUGS_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a planted new Random() left the SpotBugs gate green"; \
		cat $(SPOTBUGS_NC)/log; exit 1; \
	fi
	@grep -q 'type="PREDICTABLE_RANDOM"' $(SPOTBUGS_NC)/findings.xml || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red, but not on PREDICTABLE_RANDOM"; \
		  cat $(SPOTBUGS_NC)/log; exit 1; }
	@grep -m1 'PREDICTABLE_RANDOM' $(SPOTBUGS_NC)/findings.xml
	@echo "schema-java SpotBugs negative control: a planted new Random() turns the gate RED on PREDICTABLE_RANDOM"
