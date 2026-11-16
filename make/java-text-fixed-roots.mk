# THE JAVA LEG'S TEXT FORM FOR A FIXED ROOT (docs/SPEC-TABLES.md §16, §3.4;
# test/conformance/README.md). The Java leg alone through the harness, and only
# its json-read and json-write lines are this target's verdict (the leg's other
# surfaces are other gates): every row answered, the fixed roots' included,
# with the reference's bytes, so both lines read `pass N/N` with no `+Ka`
# absence suffix. A fixed root's value is the one its form 3 read gives, carried
# into the variable form's value by the emitter's <Root>Variable.fromFixed and
# written by the variable form's text writer, so every §16 rule — wide integers
# as strings, the hostile-input rules — holds for the fixed root unchanged.
JAVA_TEXT_FIXED_ROOTS := build/conformance-java-text-fixed-roots
.PHONY: conformance-java-text-fixed-roots conformance-java-text-fixed-roots-negative-control
conformance-java-text-fixed-roots: build/conformance-harness build-conformance-java
	@rm -rf $(JAVA_TEXT_FIXED_ROOTS) && mkdir -p $(JAVA_TEXT_FIXED_ROOTS)
	JAVA=$(JAVA) ./build/conformance-harness run --only java --work $(JAVA_TEXT_FIXED_ROOTS)/work > $(JAVA_TEXT_FIXED_ROOTS)/log 2>&1 || true
	@grep -E '^json-(read|write) ' $(JAVA_TEXT_FIXED_ROOTS)/log || \
		{ cat $(JAVA_TEXT_FIXED_ROOTS)/log; echo "conformance-java-text-fixed-roots: the run printed no json-read or json-write line"; exit 1; }
	@grep -qE '^json-read +pass ([0-9]+)/\1 *$$' $(JAVA_TEXT_FIXED_ROOTS)/log || \
		{ cat $(JAVA_TEXT_FIXED_ROOTS)/log; echo "conformance-java-text-fixed-roots: the Java json-read line is not pass N/N with no absence"; exit 1; }
	@grep -qE '^json-write +pass ([0-9]+)/\1 *$$' $(JAVA_TEXT_FIXED_ROOTS)/log || \
		{ cat $(JAVA_TEXT_FIXED_ROOTS)/log; echo "conformance-java-text-fixed-roots: the Java json-write line is not pass N/N with no absence"; exit 1; }
	@echo "conformance-java-text-fixed-roots: the Java leg answers every json-read and json-write row with the reference's bytes"

# ITS NEGATIVE CONTROL: the tabledemo unit's RootConfigVariable text writer
# recompiled under build/ with a fixed root's weapons field dropped from the
# carry. The committed root_full text holds a non-empty weapons array, so the
# harness must fail that row, and the json-write line must stop reading
# pass N/N, while json-read — the reader, which never uses fromFixed — stays
# green.
JAVA_TEXT_FIXED_ROOTS_SABOTAGE := build/conformance-java-text-fixed-roots-sabotage
JAVA_TEXT_FIXED_ROOTS_SED := s@v\.weapons\.add(WeaponConfigVariable\.fromFixed(fixed\.weapons\[i\]));@// SABOTAGED: weapons dropped from the fixed-root text write@
conformance-java-text-fixed-roots-negative-control: build/conformance-harness build-conformance-java
	@rm -rf $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE) && mkdir -p $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/src $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/classes
	@sed '$(JAVA_TEXT_FIXED_ROOTS_SED)' build/tables-generated-java/examples/RootConfigVariable.java > $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/src/RootConfigVariable.java
	@test "$$(grep -c SABOTAGED $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/src/RootConfigVariable.java)" = 1 || \
		{ echo "NEGATIVE CONTROL: the field-drop sabotage did not apply to the fixed root's text writer"; exit 1; }
	$(JAVAC) --release 17 -nowarn -cp build/conformance-java -d $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/classes $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/src/RootConfigVariable.java
	@printf '#!/bin/sh\nexec %s -cp %s/classes:%s/build/conformance-java Driver "$$@"\n' "$(JAVA)" "$(CURDIR)/$(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)" "$(CURDIR)" > $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/driver
	@chmod +x $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/driver
	@printf 'java %s/driver\n' "$(CURDIR)/$(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)" > $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/drivers.txt
	./build/conformance-harness run --drivers $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/drivers.txt \
		--work $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/work > $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/log 2>&1 || true
	@if grep -qE '^json-write +pass ([0-9]+)/\1 *$$' $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/log; then \
		echo "NEGATIVE CONTROL FAILED: a fixed root's text writer with a field dropped left the Java json-write line green"; \
		cat $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/log; exit 1; \
	fi
	@grep -qE '^json-read +pass ([0-9]+)/\1 *$$' $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: dropping a field from the text writer also redded json-read"; \
		  cat $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/log; exit 1; }
	@grep -E 'root_full\.json' $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/log | grep -m1 'json-write' || \
		{ echo "NEGATIVE CONTROL FAILED: the json-write line went red, but not on root_full"; \
		  cat $(JAVA_TEXT_FIXED_ROOTS_SABOTAGE)/log; exit 1; }
	@echo "negative control: a fixed root's text writer with a field dropped turns Java json-write RED on root_full"

test-java: conformance-java-text-fixed-roots conformance-java-text-fixed-roots-negative-control
