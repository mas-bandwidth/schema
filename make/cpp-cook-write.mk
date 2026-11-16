# make/cpp-cook-write.mk — the C++ cook-write gate (docs/SPEC-TABLES.md §7.2).
#
# The leg writes every cook-write row byte for byte as the tool does: a counted
# array's slots past its live count are zero, in both byte orders. The leg's
# other surfaces belong to other cards, so the run's own exit status is not
# this target's verdict: it reads the cook-write row and the relaxed list from
# the log it writes.
#
# Its negative control re-emits the C++ table unit with the dead-slot zeroing
# removed, under build/ only, and requires the same gate to go red on it.

CPP_COOK_WRITE := build/conformance-cpp-cook-write
CPP_COOK_WRITE_NC := build/conformance-cpp-cook-write-negative-control
CPP_COOK_WRITE_NC_GEN := $(CPP_COOK_WRITE_NC)/generated
CPP_COOK_WRITE_NC_INCLUDES := $(subst -Ibuild/tables-generated/,-I$(CPP_COOK_WRITE_NC_GEN)/,$(CONFORMANCE_INCLUDES))
CPP_COOK_WRITE_NC_OBJECTS = $(patsubst build/tables-generated/%.cpp,$(CPP_COOK_WRITE_NC)/objs/%.o,$(CONFORMANCE_SOURCES))

.PHONY: conformance-cpp-cook-write conformance-cpp-cook-write-negative-control
conformance-cpp-cook-write: build/tables-generated/.stamp build/conformance-harness build/conformance-cpp build/schema_test_cook
	@rm -rf $(CPP_COOK_WRITE) && mkdir -p $(CPP_COOK_WRITE)
	./build/conformance-harness run --only cpp --work $(CPP_COOK_WRITE)/run > $(CPP_COOK_WRITE)/log 2>&1 || true
	@awk '$$1=="cook-write" && $$2=="pass" { split($$3, n, "/"); if (n[1] != "" && n[1] == n[2] && $$4 == "") ok=1 } END { exit(ok ? 0 : 1) }' \
		$(CPP_COOK_WRITE)/log || { cat $(CPP_COOK_WRITE)/log; echo "conformance-cpp-cook-write FAILED: cook-write is not pass N/N with the same N twice and no +Ka absence suffix"; exit 1; }
	@if grep -Eq '^conformance RELAXED .* cpp / cook-write:' $(CPP_COOK_WRITE)/log; then \
		cat $(CPP_COOK_WRITE)/log; echo "conformance-cpp-cook-write FAILED: cpp / cook-write carries a RELAXED line"; exit 1; fi
	@grep -E '^cook-write ' $(CPP_COOK_WRITE)/log

$(CPP_COOK_WRITE_NC)/objs/%.o: $(CPP_COOK_WRITE_NC_GEN)/%.cpp
	@mkdir -p $(@D)
	$(CXX) $(TABLES_CXXFLAGS) $(CPP_COOK_WRITE_NC_INCLUDES) -c $< -o $@

conformance-cpp-cook-write-negative-control: build/tables-generated/.stamp build/conformance-harness
	@rm -rf $(CPP_COOK_WRITE_NC) && mkdir -p $(CPP_COOK_WRITE_NC)
	@cp -R build/tables-generated $(CPP_COOK_WRITE_NC_GEN)
	@sed -E -e 's|bound := name \+ "_count"|bound := fmt.Sprintf("%d", f.ArrayBound) /* SABOTAGED: dead counted slots written */|' \
		-e 's|bound = name \+ "_count"|bound = fmt.Sprintf("%d", f.ArrayBound) /* SABOTAGED: dead counted slots written */|' \
		internal/codegen/cpptable/cookwrite.go > $(CPP_COOK_WRITE_NC)/cookwrite.go.txt
	@test "$$(grep -c 'SABOTAGED: dead counted slots written' $(CPP_COOK_WRITE_NC)/cookwrite.go.txt)" = 2 || \
		{ echo "NEGATIVE CONTROL FAILED: the dead-slot sabotage did not apply to both array writers"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/cpptable/cookwrite.go":"%s/$(CPP_COOK_WRITE_NC)/cookwrite.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(CPP_COOK_WRITE_NC)/overlay.json
	go build -overlay $(CPP_COOK_WRITE_NC)/overlay.json -o $(CPP_COOK_WRITE_NC)/schema ./cmd/schema
	@rm -rf $(CPP_COOK_WRITE_NC_GEN)/examples && mkdir -p $(CPP_COOK_WRITE_NC_GEN)/examples
	$(CPP_COOK_WRITE_NC)/schema generate --lang cpp --out $(CPP_COOK_WRITE_NC_GEN)/examples tables/examples > $(CPP_COOK_WRITE_NC)/generate.log 2>&1 || \
		{ cat $(CPP_COOK_WRITE_NC)/generate.log; echo "NEGATIVE CONTROL FAILED: the sabotaged emitter did not generate the examples unit"; exit 1; }
	@! sed -n '/^inline bool RootConfigCookBody( uint8_t \* at, const RootConfig & value, TableByteOrder order )$$/,/^}$$/p' \
		$(CPP_COOK_WRITE_NC_GEN)/examples/TablesTable.h | grep -q 'i < value.weapons_count' || \
		{ echo "NEGATIVE CONTROL FAILED: the generated cook writer still stops at the count"; exit 1; }
	@$(MAKE) --no-print-directory $(CPP_COOK_WRITE_NC_OBJECTS)
	$(CXX) $(TABLES_CXXFLAGS) $(CPP_COOK_WRITE_NC_INCLUDES) test/conformance/cpp/main.cpp \
		$(CPP_COOK_WRITE_NC_OBJECTS) -o $(CPP_COOK_WRITE_NC)/conformance-cpp
	@printf '#!/bin/sh\nif [ "$$2" = "list" ]; then\n    echo cook-write\n    exit 0\nfi\nexec %s/conformance-cpp "$$@"\n' \
		"$(CURDIR)/$(CPP_COOK_WRITE_NC)" > $(CPP_COOK_WRITE_NC)/driver
	@chmod +x $(CPP_COOK_WRITE_NC)/driver
	@printf 'cpp %s/driver\n' "$(CURDIR)/$(CPP_COOK_WRITE_NC)" > $(CPP_COOK_WRITE_NC)/drivers
	./build/conformance-harness run --drivers $(CPP_COOK_WRITE_NC)/drivers --work $(CPP_COOK_WRITE_NC)/run > $(CPP_COOK_WRITE_NC)/log 2>&1 || true
	@if awk '$$1=="cook-write" && $$2=="pass" { split($$3, n, "/"); if (n[1] != "" && n[1] == n[2] && $$4 == "") ok=1 } END { exit(ok ? 0 : 1) }' \
		$(CPP_COOK_WRITE_NC)/log; then \
		cat $(CPP_COOK_WRITE_NC)/log; echo "NEGATIVE CONTROL FAILED: writing the dead counted slots left the C++ cook-write gate green"; exit 1; fi
	@grep -q 'root_full' $(CPP_COOK_WRITE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the cook-write line went red, but not on root_full"; cat $(CPP_COOK_WRITE_NC)/log; exit 1; }
	@grep -E '^cook-write ' $(CPP_COOK_WRITE_NC)/log
	@echo "negative control: writing the dead counted slots turns the C++ cook-write gate red"

test-cpp: conformance-cpp-cook-write conformance-cpp-cook-write-negative-control
