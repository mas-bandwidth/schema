# make/schema-java-nightly-fuzz.mk — the Java port's coverage-guided nightly
# fuzz of the generated table readers (Jazzer).
#
# The corpus is the one make/java.mk already generates (test/java-fuzz/corpus.txt
# lists it, and this file refuses a path that is missing), so there is no second
# corpus. The harness is generated from that tree: one Jazzer target for the
# variable load, one for the fixed strict read, and one for the text/JSON parse
# of every table.
#
# Jazzer is pinned from its official release (CodeIntelligenceTesting/jazzer).
# The target PROBES for it and refuses by name when it is absent; it never
# installs by hand. The fleet play (rowan-tools/fleet) owns the install. On CI
# the workflow downloads this exact pinned release and checks its sha256.
JAZZER_VERSION ?= 0.30.0
JAZZER_SHA256 ?= 6eeaf0026d75599b07527d93b576593ce847cfca8b337a579971a5a9cdf792d0
JAZZER_JAR     ?=
JAZZER         ?= jazzer
JAZZER_CMD      = $(if $(JAZZER_JAR),$(JAVA) -jar $(JAZZER_JAR),$(JAZZER))

# The nightly time budget, per port, split evenly across the generated targets
# so every table gets its share. The replay target takes no clock at all.
JAVA_FUZZ_TIME    ?= 600
JAVA_FUZZ          = build/java-fuzz
JAVA_FUZZ_SEEDS    = $(JAVA_FUZZ)/seeds
JAVA_FUZZ_ARTIFACTS = $(JAVA_FUZZ)/artifacts
JAVA_FUZZ_SAVED    = test/java-fuzz/corpus

.PHONY: schema-java-nightly-fuzz-tool schema-java-nightly-fuzz-harness
.PHONY: schema-java-nightly-fuzz schema-java-nightly-fuzz-replay
.PHONY: schema-java-nightly-fuzz-negative-control

schema-java-nightly-fuzz-tool:
	@if [ -n "$(JAZZER_JAR)" ]; then \
		[ -f "$(JAZZER_JAR)" ] || { \
			echo "tool missing: jazzer (JAZZER_JAR=$(JAZZER_JAR) does not exist; pin $(JAZZER_VERSION))"; \
			echo "  install it fleet-wide through the fleet play in rowan-tools/fleet"; exit 1; }; \
	elif ! command -v $(firstword $(JAZZER)) >/dev/null 2>&1; then \
		echo "tool missing: jazzer (pin $(JAZZER_VERSION))"; \
		echo "  install it fleet-wide through the fleet play in rowan-tools/fleet,"; \
		echo "  or point JAZZER_JAR at the official release's jazzer_standalone.jar"; \
		exit 1; \
	fi
	@echo "jazzer: pinned $(JAZZER_VERSION)"

# The generated readers plus the generated Jazzer entry points, compiled once.
# The corpus list is checked here so a removed directory fails by name rather
# than making a smaller fuzz look green.
$(JAVA_FUZZ)/classes/.stamp: build/tables-generated-java/.stamp test/java-fuzz/gen/main.go test/java-fuzz/corpus.txt
	@while IFS= read -r p; do \
		case "$$p" in ''|\#*) continue;; esac; \
		[ -e "$$p" ] || { echo "CORPUS MISSING: $$p"; exit 1; }; \
	done < test/java-fuzz/corpus.txt
	@mkdir -p $(JAVA_FUZZ)
	go run ./test/java-fuzz/gen -generated build/tables-generated-java -out $(JAVA_FUZZ)
	@rm -rf $(JAVA_FUZZ)/classes && mkdir -p $(JAVA_FUZZ)/classes
	find build/tables-generated-java -name '*.java' > $(JAVA_FUZZ)/generated-sources.txt
	find $(JAVA_FUZZ)/src -name '*.java' > $(JAVA_FUZZ)/harness-sources.txt
	$(JAVAC) --release 17 -nowarn -d $(JAVA_FUZZ)/classes \
		@$(JAVA_FUZZ)/generated-sources.txt @$(JAVA_FUZZ)/harness-sources.txt
	@touch $@

schema-java-nightly-fuzz-harness: $(JAVA_FUZZ)/classes/.stamp

# The seeds: the Go fuzz corpus (internal/fuzz/testdata) and the conformance
# vectors (testdata/wire, testdata/conformance). Flattened with a numeric
# prefix because libFuzzer wants a directory of files, and this never invents a
# seed of its own.
$(JAVA_FUZZ)/seeds/.stamp:
	@rm -rf $(JAVA_FUZZ_SEEDS) && mkdir -p $(JAVA_FUZZ_SEEDS)
	@n=0; for f in $$(find internal/fuzz/testdata testdata/wire testdata/conformance -type f 2>/dev/null); do \
		n=$$((n+1)); cp "$$f" "$(JAVA_FUZZ_SEEDS)/seed-$$n.bin"; \
	done; \
	[ "$$n" -gt 0 ] || { echo "SEEDS MISSING: no Go fuzz or conformance vectors"; exit 1; }; \
	echo "seeds: $$n file(s) from internal/fuzz/testdata, testdata/wire and testdata/conformance"
	@touch $@

# THE NIGHTLY TARGET. Every target gets an even share of $(JAVA_FUZZ_TIME)
# seconds. A Jazzer finding is a nonzero exit here; its artifact stays in
# $(JAVA_FUZZ_ARTIFACTS) and the message says where to commit it.
schema-java-nightly-fuzz: schema-java-nightly-fuzz-tool $(JAVA_FUZZ)/classes/.stamp $(JAVA_FUZZ)/seeds/.stamp
	@mkdir -p $(JAVA_FUZZ_ARTIFACTS)
	@n=$$(wc -l < $(JAVA_FUZZ)/manifest.txt | tr -d ' '); \
	  per=$$(( $(JAVA_FUZZ_TIME) / (n > 0 ? n : 1) )); \
	  [ "$$per" -ge 1 ] || per=1; \
	  echo "jazzer: $$n target(s), $${per}s each"; \
	  while read -r fqcn unit table kind; do \
		[ -n "$$fqcn" ] || continue; \
		echo "== $$fqcn ($$unit $$table $$kind)"; \
		$(JAZZER_CMD) --cp=$(JAVA_FUZZ)/classes --target_class=$$fqcn \
			-max_total_time=$$per -artifact_prefix=$(JAVA_FUZZ_ARTIFACTS)/ \
			$(JAVA_FUZZ_SEEDS) || { status=$$?; \
			echo "JAZZER FINDING in $$fqcn (exit $$status); artifact in $(JAVA_FUZZ_ARTIFACTS)/"; \
			echo "  commit it as $(JAVA_FUZZ_SAVED)/$${fqcn##*.}__<artifact>.bin"; \
			exit $$status; }; \
	  done < $(JAVA_FUZZ)/manifest.txt
	@echo "schema Java nightly fuzz: no finding in $$(wc -l < $(JAVA_FUZZ)/manifest.txt) target(s) in $(JAVA_FUZZ_TIME)s"

# THE FAST REPLAY, on every push. It runs no fuzzer and reads no clock: every
# input committed to test/java-fuzz/corpus is handed to the target its name
# names, and an exception is a red.
schema-java-nightly-fuzz-replay: $(JAVA_FUZZ)/classes/.stamp
	$(JAVA) -cp $(JAVA_FUZZ)/classes NightlyReplay $(JAVA_FUZZ)/manifest.txt $(JAVA_FUZZ_SAVED)

# THE NEGATIVE CONTROL. A scratch copy of one generated reader (blockdemo's
# TableVariable) drops the empty-input bounds check, and the same Jazzer target
# that is green above must find it inside a short time budget; replaying the
# artifact must then fail. The generated tree itself is never edited.
JAVA_FUZZ_NC = build/java-fuzz-negative-control
schema-java-nightly-fuzz-negative-control: schema-java-nightly-fuzz-tool $(JAVA_FUZZ)/classes/.stamp
	@rm -rf $(JAVA_FUZZ_NC) && mkdir -p $(JAVA_FUZZ_NC)/src/blockdemo $(JAVA_FUZZ_NC)/classes $(JAVA_FUZZ_NC)/seeds $(JAVA_FUZZ_NC)/artifacts
	@cp build/tables-generated-java/block/*.java $(JAVA_FUZZ_NC)/src/blockdemo/
	@sed -i 's/if (data.length < 1) {/if (false) { \/\/ SABOTAGED/' $(JAVA_FUZZ_NC)/src/blockdemo/TableVariable.java
	@grep -q SABOTAGED $(JAVA_FUZZ_NC)/src/blockdemo/TableVariable.java || \
		{ echo "NEGATIVE CONTROL FAILED: the plant did not apply"; exit 1; }
	@cp $(JAVA_FUZZ)/src/blockdemo/NightlyPaddedRowLoadFuzz.java $(JAVA_FUZZ_NC)/src/blockdemo/
	@cp $(JAVA_FUZZ)/src/NightlyReplay.java $(JAVA_FUZZ_NC)/src/
	@printf 'blockdemo.NightlyPaddedRowLoadFuzz\n' > $(JAVA_FUZZ_NC)/manifest.txt
	@printf '' > $(JAVA_FUZZ_NC)/seeds/empty
	$(JAVAC) --release 17 -nowarn -d $(JAVA_FUZZ_NC)/classes $(JAVA_FUZZ_NC)/src/blockdemo/*.java $(JAVA_FUZZ_NC)/src/NightlyReplay.java
	@if $(JAZZER_CMD) --cp=$(JAVA_FUZZ_NC)/classes --target_class=blockdemo.NightlyPaddedRowLoadFuzz \
			-max_total_time=25 -artifact_prefix=$(JAVA_FUZZ_NC)/artifacts/ $(JAVA_FUZZ_NC)/seeds \
			> $(JAVA_FUZZ_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: a reader with its empty-input bound removed left Jazzer green"; \
		cat $(JAVA_FUZZ_NC)/log; exit 1; \
	fi
	@artifact=$$(find $(JAVA_FUZZ_NC)/artifacts -type f | head -1); \
	  [ -n "$$artifact" ] || { echo "NEGATIVE CONTROL FAILED: Jazzer went red with no saved input"; cat $(JAVA_FUZZ_NC)/log; exit 1; }; \
	  mkdir -p $(JAVA_FUZZ_SAVED); \
	  cp "$$artifact" "$(JAVA_FUZZ_SAVED)/NightlyPaddedRowLoadFuzz__$$(basename $$artifact).bin"; \
	  echo "negative control: planted empty-input bound is found by Jazzer; saved $$artifact"
	@if $(JAVA) -cp $(JAVA_FUZZ_NC)/classes NightlyReplay $(JAVA_FUZZ_NC)/manifest.txt $(JAVA_FUZZ_SAVED) \
			> $(JAVA_FUZZ_NC)/replay.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the saved input replays green against the planted reader"; \
		cat $(JAVA_FUZZ_NC)/replay.log; exit 1; \
	fi
	@grep -q 'REPLAY FAILED: NightlyPaddedRowLoadFuzz__' $(JAVA_FUZZ_NC)/replay.log
	@rm -f $(JAVA_FUZZ_SAVED)/NightlyPaddedRowLoadFuzz__*.bin
	@echo "negative control: the saved Jazzer input turns the replay RED (the red line is kept; the transient input is removed)"
