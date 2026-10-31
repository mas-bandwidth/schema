# make/schema-java-runs-hostile-corpus.mk — the Java port runs the shared
# hostile-input corpus (schema-java-runs-hostile-corpus).
#
# The corpus is the committed portable directory docs/HOSTILE-CORPUS.md names:
# internal/ci/hostilecorpus/corpus/manifest.tsv plus one raw input per line.
# This target generates the units the manifest names, compiles them with the
# pinned JDK (--release 17, every lint an error), and hands every input to the
# port's own reader (test/java-hostile/Runner.java). Reads files only: no
# network, no clock.
#
# A disagreement the port already owes is carried, by name, in
# test/java-hostile/known-findings.tsv, and printed as a FIX-CARD. A
# disagreement no FIX-CARD names is a failure, and a FIX-CARD that has gone
# green is a failure too, so the list cannot rot into silence.
#
# The planted-bad-pattern control changes the generated text reader in a
# scratch copy so an accepted input is refused, and requires the runner to go
# red before the unplanted target is green again.

JAVA_HOSTILE_CORPUS ?= internal/ci/hostilecorpus/corpus
JAVA_HOSTILE_BUILD  ?= build/java-hostile
JAVA_HOSTILE_FINDINGS ?= test/java-hostile/known-findings.tsv

JAVA_HOSTILE_UNITS := tabledemo:tables/examples \
	messagedemo:tables/messages blobdemo:tables/blobs \
	graphdemo:tables/pointers scalardemo:tables/scalars \
	streamdemo:tables/stream mapdemo:tables/maps listdemo:tables/lists probe:test/java-hostile/probe.schema

$(JAVA_HOSTILE_BUILD)/generated/.stamp: bin/schema make/schema-java-runs-hostile-corpus.mk \
		$(wildcard tables/examples/*.schema tables/messages/*.schema tables/blobs/*.schema \
			tables/pointers/*.schema tables/scalars/*.schema tables/stream/*.schema \
			tables/maps/*.schema tables/lists/*.schema) \
		test/java-hostile/probe.schema
	@rm -rf $(JAVA_HOSTILE_BUILD)/generated && mkdir -p $(JAVA_HOSTILE_BUILD)/generated
	@for unit in $(JAVA_HOSTILE_UNITS); do \
		name=$${unit%%:*}; path=$${unit#*:}; \
		./bin/schema generate --lang java --out $(JAVA_HOSTILE_BUILD)/generated/$$name $$path || exit 1; \
	done
	@touch $@

$(JAVA_HOSTILE_BUILD)/classes/.stamp: $(JAVA_HOSTILE_BUILD)/generated/.stamp test/java-hostile/Runner.java
	@rm -rf $(JAVA_HOSTILE_BUILD)/classes && mkdir -p $(JAVA_HOSTILE_BUILD)/classes
	$(JAVAC) --release 17 -Xlint:all -Werror -d $(JAVA_HOSTILE_BUILD)/classes \
		$(JAVA_HOSTILE_BUILD)/generated/*/*.java test/java-hostile/Runner.java
	@touch $@

.PHONY: schema-java-runs-hostile-corpus
schema-java-runs-hostile-corpus: $(JAVA_HOSTILE_BUILD)/classes/.stamp
	$(JAVA) -cp $(JAVA_HOSTILE_BUILD)/classes Runner "$(JAVA_HOSTILE_CORPUS)" "$(JAVA_HOSTILE_FINDINGS)"

# The port's normal unit target runs the corpus: this file is included after
# make/java.mk by the Makefile's wildcard, so the prerequisite lands here and
# make/java.mk itself is untouched.
test-java: schema-java-runs-hostile-corpus

.PHONY: schema-java-runs-hostile-corpus-negative-control
schema-java-runs-hostile-corpus-negative-control: $(JAVA_HOSTILE_BUILD)/classes/.stamp
	@rm -rf $(JAVA_HOSTILE_BUILD)/plant && mkdir -p $(JAVA_HOSTILE_BUILD)/plant/generated $(JAVA_HOSTILE_BUILD)/plant/classes
	@cp -R $(JAVA_HOSTILE_BUILD)/generated/* $(JAVA_HOSTILE_BUILD)/plant/generated/
	@for f in $(JAVA_HOSTILE_BUILD)/plant/generated/*/*Variable.java; do \
		sed 's|^        if (in.bad) {$$|        if (!in.bad) {|' "$$f" > "$$f.planted" && mv "$$f.planted" "$$f" || exit 1; \
	done
	@if cmp -s $(JAVA_HOSTILE_BUILD)/generated/tabledemo/RootConfigVariable.java $(JAVA_HOSTILE_BUILD)/plant/generated/tabledemo/RootConfigVariable.java; then \
		echo 'NEGATIVE CONTROL FAILED: the planted reader bug did not apply'; exit 1; fi
	$(JAVAC) --release 17 -Xlint:all -Werror -d $(JAVA_HOSTILE_BUILD)/plant/classes \
		$(JAVA_HOSTILE_BUILD)/plant/generated/*/*.java test/java-hostile/Runner.java
	@if $(JAVA) -cp $(JAVA_HOSTILE_BUILD)/plant/classes Runner "$(JAVA_HOSTILE_CORPUS)" "$(JAVA_HOSTILE_FINDINGS)" \
			> $(JAVA_HOSTILE_BUILD)/plant/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a text reader that refuses accepted inputs left the corpus green'; \
		cat $(JAVA_HOSTILE_BUILD)/plant/log; exit 1; fi
	@grep -q '^FAIL ' $(JAVA_HOSTILE_BUILD)/plant/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the runner went red without naming an unexplained case'; \
		  cat $(JAVA_HOSTILE_BUILD)/plant/log; exit 1; }
	@$(MAKE) --no-print-directory schema-java-runs-hostile-corpus
	@echo 'java hostile-corpus negative control: a text reader that refuses accepted inputs reds the corpus; unplanted it is green'
