# make/schema-java-generated-spotbugs-gate.mk -- the SpotBugs lens on the
# generated Java (ROADMAP schema-per-language-lenses, Java/SpotBugs).
#
# SpotBugs plus find-sec-bugs over the COMPILED generated Java of the corpus
# the Java leg's own generate targets read: the tables corpus
# (build/tables-generated-java/.stamp), examples (generated/java) and
# examples128 (generated/java-ludicrous). No second corpus is invented.
#
# Strictness: -effort:max at the medium threshold, EVERY category. Low is
# noise-dominated; high drops find-sec-bugs' medium detectors. There is no
# category include filter: every finding at medium either fails the gate or is
# suppressed in make/schema-java-generated-spotbugs-exclude.xml, one Match per
# bug pattern narrowed to the exact generated method or field names it was
# triaged on, each with its reason.
#
# Tool pins (official releases, installed fleet-wide by rowan-tools
# fleet/packages.yml into ~/sdk/spotbugs-<v>, find-sec-bugs' jar beside it).
# The target PROBES: a machine without them says "tool missing: <name>" and
# stays red rather than passing over nothing.
SPOTBUGS_VERSION      ?= 4.10.5
FINDSECBUGS_VERSION   ?= 1.14.0
SPOTBUGS_HOME         ?= $(HOME)/sdk/spotbugs-$(SPOTBUGS_VERSION)
SPOTBUGS              ?= $(SPOTBUGS_HOME)/bin/spotbugs
SPOTBUGS_PLUGIN       ?= $(SPOTBUGS_HOME)/findsecbugs-plugin.jar
SPOTBUGS_EXCLUDE      := make/schema-java-generated-spotbugs-exclude.xml
SPOTBUGS_WORK         ?= build/schema-java-spotbugs
SPOTBUGS_CLASSES       = $(SPOTBUGS_WORK)/classes
SPOTBUGS_FINDINGS      = $(SPOTBUGS_WORK)/findings.xml
SPOTBUGS_SOURCES       = $(SPOTBUGS_WORK)/sources.txt
# java.mk pins JAVAC at a repo-local dist/ JDK; without it use javac on PATH.
SPOTBUGS_JAVAC        := $(if $(shell test -x "$(JAVAC)" && echo yes),$(JAVAC),javac)
# The corpus and the stamps that build it. The negative control's per-push run
# narrows both to generated/java; the gate itself always takes the whole corpus.
SPOTBUGS_CORPUS       ?= build/tables-generated-java generated/java generated/java-ludicrous
SPOTBUGS_STAMPS       ?= build/tables-generated-java/.stamp generated/java/.stamp generated/java-ludicrous/.stamp
SPOTBUGS_MIN_CLASSES  ?= 1000
# SPOTBUGS_PLANT=1 swaps ONE real generated class (generated/java/Clauses.java)
# for a copy carrying two planted methods -- `new java.util.Random()` and
# `d == Double.NaN` -- inside the corpus target's own find/javac/spotbugs
# steps, so the control exercises the exact path that certifies.
SPOTBUGS_PLANT        ?=
SPOTBUGS_PLANT_FILE   := generated/java/Clauses.java

.PHONY: schema-java-generated-spotbugs-gate
schema-java-generated-spotbugs-gate: $(SPOTBUGS_STAMPS)
	@test -x "$(SPOTBUGS)" || { \
		echo "tool missing: spotbugs $(SPOTBUGS_VERSION) on $$(hostname) (probe: $(SPOTBUGS))"; \
		echo "  converge rowan-tools fleet/packages.yml (--tags spotbugs), or set SPOTBUGS_HOME"; exit 1; }
	@test -f "$(SPOTBUGS_PLUGIN)" || { \
		echo "tool missing: find-sec-bugs $(FINDSECBUGS_VERSION) on $$(hostname) (probe: $(SPOTBUGS_PLUGIN))"; exit 1; }
	@for d in $(SPOTBUGS_CORPUS); do test -d $$d || { echo "SPOTBUGS GATE FAILED: corpus root $$d is missing"; exit 1; }; done
	@rm -rf $(SPOTBUGS_WORK) && mkdir -p $(SPOTBUGS_CLASSES)
	find $(SPOTBUGS_CORPUS) -name '*.java' | sort > $(SPOTBUGS_SOURCES)
	@if [ -n "$(SPOTBUGS_PLANT)" ]; then \
		grep -qx '$(SPOTBUGS_PLANT_FILE)' $(SPOTBUGS_SOURCES) || { echo "SPOTBUGS PLANT FAILED: $(SPOTBUGS_PLANT_FILE) is not in the corpus"; exit 1; }; \
		mkdir -p $(SPOTBUGS_WORK)/plant; \
		sed '$$d' $(SPOTBUGS_PLANT_FILE) > $(SPOTBUGS_WORK)/plant/Clauses.java; \
		printf '%s\n' \
			'    // SPOTBUGS PLANT: two known-bad patterns the lens must name.' \
			'    public static int spotbugsPlantRandom() { return new java.util.Random().nextInt(); }' \
			'    public static boolean spotbugsPlantNaN(double d) { return d == Double.NaN; }' \
			'}' >> $(SPOTBUGS_WORK)/plant/Clauses.java; \
		sed -i.bak 's#^$(SPOTBUGS_PLANT_FILE)$$#$(SPOTBUGS_WORK)/plant/Clauses.java#' $(SPOTBUGS_SOURCES) && rm -f $(SPOTBUGS_SOURCES).bak; \
		echo "schema-java SpotBugs gate: PLANTED into a copy of $(SPOTBUGS_PLANT_FILE)"; \
	fi
	@n=$$(wc -l < $(SPOTBUGS_SOURCES) | tr -d ' '); \
		if [ "$$n" -lt $(SPOTBUGS_MIN_CLASSES) ]; then \
			echo "SPOTBUGS GATE FAILED: found $$n generated Java sources, expected at least $(SPOTBUGS_MIN_CLASSES) -- the corpus, not the property, is what broke"; \
			exit 1; \
		fi; \
		echo "schema-java SpotBugs gate: compiling $$n generated Java sources"
	$(SPOTBUGS_JAVAC) --release 17 -nowarn -d $(SPOTBUGS_CLASSES) @$(SPOTBUGS_SOURCES)
	@test -n "$$(find $(SPOTBUGS_CLASSES) -name '*.class' -print -quit)" || { echo "SPOTBUGS GATE FAILED: javac wrote no classes"; exit 1; }
	$(SPOTBUGS) -textui -effort:max -medium -exitcode -xml:withMessages \
		-exclude $(SPOTBUGS_EXCLUDE) -pluginList $(SPOTBUGS_PLUGIN) \
		-output $(SPOTBUGS_FINDINGS) $(SPOTBUGS_CLASSES) || { \
		echo "SPOTBUGS GATE FAILED: findings over the generated Java (full list: $(SPOTBUGS_FINDINGS)):"; \
		grep -o '<BugInstance type="[A-Z0-9_]*"' $(SPOTBUGS_FINDINGS) | sed 's/<BugInstance //' | sort | uniq -c; exit 1; }
	@echo "schema-java SpotBugs gate: clean (-effort:max -medium, every category, $(SPOTBUGS_EXCLUDE)) over $$(wc -l < $(SPOTBUGS_SOURCES) | tr -d ' ') generated sources"

# ITS NEGATIVE CONTROL runs the corpus target itself with SPOTBUGS_PLANT=1: it
# must exit non-zero and name BOTH planted patterns in example.Clauses. Per push
# it is narrowed to the committed generated/java (SPOTBUGS_CORPUS and
# SPOTBUGS_STAMPS below); nightly it runs over the whole corpus
# (SPOTBUGS_NC_CORPUS=full).
SPOTBUGS_NC_WORK ?= build/schema-java-spotbugs-negative-control
SPOTBUGS_NC_ARGS := $(if $(filter full,$(SPOTBUGS_NC_CORPUS)),,SPOTBUGS_CORPUS=generated/java SPOTBUGS_STAMPS=generated/java/.stamp SPOTBUGS_MIN_CLASSES=9)
.PHONY: schema-java-generated-spotbugs-gate-negative-control
schema-java-generated-spotbugs-gate-negative-control:
	@rm -rf $(SPOTBUGS_NC_WORK) && mkdir -p $(dir $(SPOTBUGS_NC_WORK))
	@if $(MAKE) --no-print-directory schema-java-generated-spotbugs-gate SPOTBUGS_PLANT=1 SPOTBUGS_WORK=$(SPOTBUGS_NC_WORK) $(SPOTBUGS_NC_ARGS) > $(SPOTBUGS_NC_WORK).log 2>&1; then \
		cat $(SPOTBUGS_NC_WORK).log; echo "NEGATIVE CONTROL FAILED: the planted class left the SpotBugs gate green"; exit 1; fi
	@grep -q 'PLANTED into a copy of $(SPOTBUGS_PLANT_FILE)' $(SPOTBUGS_NC_WORK).log || { cat $(SPOTBUGS_NC_WORK).log; echo "NEGATIVE CONTROL FAILED: the gate went red before planting"; exit 1; }
	@grep -q 'SPOTBUGS GATE FAILED: findings' $(SPOTBUGS_NC_WORK).log || { cat $(SPOTBUGS_NC_WORK).log; echo "NEGATIVE CONTROL FAILED: red, but not on findings"; exit 1; }
	@for p in PREDICTABLE_RANDOM FE_TEST_IF_EQUAL_TO_NOT_A_NUMBER; do \
		python3 -c 'import sys,xml.etree.ElementTree as E; sys.exit(0 if any(b.get("type")==sys.argv[2] and b.find("Class").get("classname")=="example.Clauses" for b in E.parse(sys.argv[1]).getroot().iter("BugInstance")) else 1)' $(SPOTBUGS_NC_WORK)/findings.xml $$p || \
			{ cat $(SPOTBUGS_NC_WORK).log; echo "NEGATIVE CONTROL FAILED: the gate did not name $$p in example.Clauses"; exit 1; }; \
		echo "schema-java SpotBugs negative control: the plant in a real generated class turns the gate RED on $$p"; \
	done
