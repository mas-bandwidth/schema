# THE JAVA LEG'S COOK-WRITE SURFACE (docs/SPEC-TABLES.md §7.2, §7.6;
# test/conformance/README.md). The Java leg alone through the harness, and only
# its cook-write line is this target's verdict (the leg's other surfaces are
# other gates): every row answered, the fixed roots' included, with the
# engine's bytes, so the line reads `pass N/N` with no `+Ka` absence suffix,
# and no case of it is relaxed. A fixed root's value is the one its form 3 read
# gives (the driver's fixedCookValue); an absent optional's payload and a
# counted array's slots past its count are zero (fixed-dead-bytes-are-zero).
JAVA_COOK_WRITE := build/conformance-java-cook-write
.PHONY: conformance-java-cook-write conformance-java-cook-write-negative-control
conformance-java-cook-write: build/conformance-harness build-conformance-java
	@rm -rf $(JAVA_COOK_WRITE) && mkdir -p $(JAVA_COOK_WRITE)
	JAVA=$(JAVA) ./build/conformance-harness run --only java --work $(JAVA_COOK_WRITE)/work > $(JAVA_COOK_WRITE)/log 2>&1 || true
	@grep -E '^cook-write ' $(JAVA_COOK_WRITE)/log || \
		{ cat $(JAVA_COOK_WRITE)/log; echo "conformance-java-cook-write: the run printed no cook-write line"; exit 1; }
	@grep -qE '^cook-write +pass ([0-9]+)/\1 *$$' $(JAVA_COOK_WRITE)/log || \
		{ cat $(JAVA_COOK_WRITE)/log; echo "conformance-java-cook-write: the Java cook-write line is not pass N/N with no absence"; exit 1; }
	@! grep -E 'RELAXED .* java / cook-write' $(JAVA_COOK_WRITE)/log || \
		{ echo "conformance-java-cook-write: a Java cook-write case is relaxed"; exit 1; }
	@echo "conformance-java-cook-write: the Java leg answers every cook-write row with the engine's bytes"

# ITS NEGATIVE CONTROL: the tabledemo unit's cook writer recompiled under
# build/ with a counted array's dead slots written again (each slot past the
# count holds the element record's declared defaults, where the engine leaves
# zero). root_full's weapons hold fewer than their bound, so the harness must
# fail that row, and the cook-write line must stop reading pass N/N.
JAVA_COOK_WRITE_SABOTAGE := build/conformance-java-cook-write-sabotage
JAVA_COOK_WRITE_SED := s@int slots = f.countOffset >= 0 ? used : f.arrayBound;@int slots = f.arrayBound; /* SABOTAGED */@
conformance-java-cook-write-negative-control: build/conformance-harness build-conformance-java
	@rm -rf $(JAVA_COOK_WRITE_SABOTAGE) && mkdir -p $(JAVA_COOK_WRITE_SABOTAGE)/src $(JAVA_COOK_WRITE_SABOTAGE)/classes
	@sed '$(JAVA_COOK_WRITE_SED)' build/tables-generated-java/examples/TableCookWrite.java > $(JAVA_COOK_WRITE_SABOTAGE)/src/TableCookWrite.java
	@test "$$(grep -c SABOTAGED $(JAVA_COOK_WRITE_SABOTAGE)/src/TableCookWrite.java)" = 1 || \
		{ echo "NEGATIVE CONTROL: the dead-slot sabotage did not apply to the cook writer"; exit 1; }
	$(JAVAC) --release 17 -nowarn -cp build/conformance-java -d $(JAVA_COOK_WRITE_SABOTAGE)/classes $(JAVA_COOK_WRITE_SABOTAGE)/src/TableCookWrite.java
	@printf '#!/bin/sh\nexec %s -cp %s/classes:%s/build/conformance-java Driver "$$@"\n' "$(JAVA)" "$(CURDIR)/$(JAVA_COOK_WRITE_SABOTAGE)" "$(CURDIR)" > $(JAVA_COOK_WRITE_SABOTAGE)/driver
	@chmod +x $(JAVA_COOK_WRITE_SABOTAGE)/driver
	@printf 'java %s/driver\n' "$(CURDIR)/$(JAVA_COOK_WRITE_SABOTAGE)" > $(JAVA_COOK_WRITE_SABOTAGE)/drivers.txt
	./build/conformance-harness run --drivers $(JAVA_COOK_WRITE_SABOTAGE)/drivers.txt \
		--work $(JAVA_COOK_WRITE_SABOTAGE)/work > $(JAVA_COOK_WRITE_SABOTAGE)/log 2>&1 || true
	@if grep -qE '^cook-write +pass ([0-9]+)/\1 *$$' $(JAVA_COOK_WRITE_SABOTAGE)/log; then \
		echo "NEGATIVE CONTROL FAILED: a cook writer that fills dead counted slots left the Java cook-write line green"; \
		cat $(JAVA_COOK_WRITE_SABOTAGE)/log; exit 1; \
	fi
	@grep -E 'root_full' $(JAVA_COOK_WRITE_SABOTAGE)/log | grep -m1 'cook-write' || \
		{ echo "NEGATIVE CONTROL FAILED: the cook-write line went red, but not on root_full"; \
		  cat $(JAVA_COOK_WRITE_SABOTAGE)/log; exit 1; }
	@echo "negative control: a cook writer that fills a counted array's dead slots turns Java cook-write RED on root_full"

test-java: conformance-java-cook-write conformance-java-cook-write-negative-control
