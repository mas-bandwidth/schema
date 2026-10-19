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
SPOTBUGS_WORK         := build/schema-java-spotbugs
SPOTBUGS_INCLUDE      := $(SPOTBUGS_WORK)/include.xml
SPOTBUGS_EXCLUDE      := $(SPOTBUGS_WORK)/exclude.xml
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

# The include and exclude filters are regenerated into build/ from here, so
# this lens is one self-contained make fragment with no sibling XML files. The
# include scopes the lens to the bug categories (CORRECTNESS and SECURITY) and
# the exclude carries three method-level suppressions, each with its reason.
$(SPOTBUGS_WORK):
	@mkdir -p $@

$(SPOTBUGS_INCLUDE): | $(SPOTBUGS_WORK)
	@printf '%s\n' \
		'<!-- The Java SpotBugs lens is a BUG lens, so it includes the two categories' \
		'     that name a defect in the analysed code: SpotBugs own CORRECTNESS and the' \
		'     SECURITY category find-sec-bugs writes into. Effort stays at max; the' \
		'     threshold is medium because the one security pattern this gate is proven' \
		'     on (PREDICTABLE_RANDOM) is a medium-priority detector. At that threshold' \
		'     the whole tree reports 4077 findings, 4074 of them STYLE, BAD_PRACTICE,' \
		'     PERFORMANCE and MALICIOUS_CODE design reports over generated public value' \
		'     classes, noise for a bug lens. Scoping the include to the bug categories' \
		'     is the gate definition, not a suppression: a CORRECTNESS or SECURITY' \
		'     finding is always reported. -->' \
		'<FindBugsFilter>' \
		'    <Bug category="CORRECTNESS"/>' \
		'    <Bug category="SECURITY"/>' \
		'</FindBugsFilter>' > $@

$(SPOTBUGS_EXCLUDE): | $(SPOTBUGS_WORK)
	@printf '%s\n' \
		'<!-- Each suppression is a single method, named with its reason. There are no' \
		'     pattern-wide or package-wide disables here. -->' \
		'<FindBugsFilter>' \
		'    <!-- NOT a bug: EmptyA is a zero-byte struct, so emptyAMaxBytes is 0 and' \
		'         `data.length >= 0` is vacuously true. It is the writer buffer bound,' \
		'         emitted from the struct own ceiling, and not a behavioural defect.' \
		'         The live bounds for non-empty structs stay gated. -->' \
		'    <Match>' \
		'        <Bug pattern="INT_BAD_COMPARISON_WITH_NONNEGATIVE_VALUE"/>' \
		'        <Class name="example.Clauses"/>' \
		'        <Method name="checkWriteEmptyA"/>' \
		'    </Match>' \
		'    <!-- NOT a bug: EmptyB is a zero-byte struct; same vacuous ceiling as' \
		'         checkWriteEmptyA above. -->' \
		'    <Match>' \
		'        <Bug pattern="INT_BAD_COMPARISON_WITH_NONNEGATIVE_VALUE"/>' \
		'        <Class name="example.Clauses"/>' \
		'        <Method name="checkWriteEmptyB"/>' \
		'    </Match>' \
		'    <!-- NOT a bug: Heartbeat is a zero-byte struct; heartbeatMaxBytes is 0. -->' \
		'    <Match>' \
		'        <Bug pattern="INT_BAD_COMPARISON_WITH_NONNEGATIVE_VALUE"/>' \
		'        <Class name="example.Wire"/>' \
		'        <Method name="checkWriteHeartbeat"/>' \
		'    </Match>' \
		'</FindBugsFilter>' > $@

# THE CORPUS is the one the Java leg's own generate targets read: the tables
# corpus (build/tables-generated-java/.stamp), examples (generated/java) and
# examples128 (generated/java-ludicrous). Nothing here invents a second corpus.
.PHONY: schema-java-generated-spotbugs-gate
schema-java-generated-spotbugs-gate: $(SPOTBUGS_INCLUDE) $(SPOTBUGS_EXCLUDE) build/tables-generated-java/.stamp generated/java/.stamp generated/java-ludicrous/.stamp
	@command -v $(SPOTBUGS) >/dev/null 2>&1 || { \
		echo "tool missing: spotbugs $(SPOTBUGS_VERSION) (probe: $(SPOTBUGS))"; \
		echo "  install it fleet-wide from github.com/spotbugs/spotbugs, or set SPOTBUGS_HOME=/path/to/spotbugs-$(SPOTBUGS_VERSION)"; \
		exit 1; }
	@test -f "$(SPOTBUGS_PLUGIN)" || { \
		echo "tool missing: find-sec-bugs $(FINDSECBUGS_VERSION) (probe: $(SPOTBUGS_PLUGIN))"; \
		echo "  install the plugin from Maven Central com.h3xstream.findsecbugs:findsecbugs-plugin:$(FINDSECBUGS_VERSION), or set SPOTBUGS_PLUGIN=/path/to/findsecbugs-plugin.jar"; \
		exit 1; }
	@rm -rf $(SPOTBUGS_CLASSES) && mkdir -p $(SPOTBUGS_CLASSES)
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
schema-java-generated-spotbugs-gate-negative-control: $(SPOTBUGS_INCLUDE) $(SPOTBUGS_EXCLUDE)
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
