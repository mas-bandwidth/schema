# THE C++ REFERENCE READS EVERY messagedemo.ToolMessage INSTANCE (docs/SPEC-TABLES.md
# §3.4, §4.5): the wire surface reads each fixed-form file the corpus carries and
# saves it back to the golden's bytes, and the json-write surface writes each
# one's text. The run covers every surface of the leg, and the others belong to
# their own gates, so its exit status is not this target's verdict: the wire line
# and the json-write line must each read `pass N/N`, with no `+Ka` absence.
CPP_TOOL_MESSAGE := build/conformance-cpp-tool-message
CPP_TOOL_MESSAGE_NEGATIVE := build/conformance-cpp-tool-message-negative

.PHONY: conformance-cpp-tool-message
conformance-cpp-tool-message: build/conformance-harness build/conformance-cpp build/schema_test_cook
	@rm -rf $(CPP_TOOL_MESSAGE) && mkdir -p $(CPP_TOOL_MESSAGE)
	./build/conformance-harness run --only cpp --work $(CPP_TOOL_MESSAGE)/work > $(CPP_TOOL_MESSAGE)/log 2>&1 || true
	@for surface in wire json-write; do \
		grep -Eq "^$$surface +pass ([0-9]+)/\1 *$$" $(CPP_TOOL_MESSAGE)/log || \
			{ echo "FAILED: conformance-cpp-tool-message: the C++ $$surface surface does not read every case"; \
			  grep -E "^$$surface |cpp / $$surface" $(CPP_TOOL_MESSAGE)/log; exit 1; }; \
	done
	@grep -E '^(wire|json-write) ' $(CPP_TOOL_MESSAGE)/log
	@echo "conformance-cpp-tool-message: the C++ reference reads and writes every wire and json-write case"

# ITS CONTROL plants the nested-union fault under build/ alone: a copy of the
# generated messagedemo header whose dead-byte scan judges a nested union's tag
# under an outer arm that is not taken, so a ToolMessage whose outer arm holds
# live bytes there is refused. The driver is linked against that copy, and the
# wire surface must go red while json-write still answers the cases it reads,
# which is the driver going on past a case it cannot load.
.PHONY: conformance-cpp-tool-message-negative-control
conformance-cpp-tool-message-negative-control: build/conformance-harness build/conformance-cpp build/schema_test_cook
	@rm -rf $(CPP_TOOL_MESSAGE_NEGATIVE) && mkdir -p $(CPP_TOOL_MESSAGE_NEGATIVE)
	cp -R build/tables-generated/messages $(CPP_TOOL_MESSAGE_NEGATIVE)/messages
	@test "$$(grep -c 'outer.guard, outer.argw ) != outer.arg ) { break; }' $(CPP_TOOL_MESSAGE_NEGATIVE)/messages/MessagesTable.h)" = 1 || \
		{ echo "NEGATIVE CONTROL FAILED: the nested-union gate is not in the generated header once"; exit 1; }
	sed -e 's/outer.guard, outer.argw ) != outer.arg ) { break; }/outer.guard, outer.argw ) != outer.arg ) { (void) outer.arg; }/' build/tables-generated/messages/MessagesTable.h \
		> $(CPP_TOOL_MESSAGE_NEGATIVE)/messages/MessagesTable.h
	@if cmp -s build/tables-generated/messages/MessagesTable.h $(CPP_TOOL_MESSAGE_NEGATIVE)/messages/MessagesTable.h; then \
		echo "NEGATIVE CONTROL FAILED: the fault was not planted"; exit 1; \
	fi
	$(CXX) $(TABLES_CXXFLAGS) $(subst -Ibuild/tables-generated/messages,-I$(CPP_TOOL_MESSAGE_NEGATIVE)/messages,$(CONFORMANCE_INCLUDES)) \
		-c $(CPP_TOOL_MESSAGE_NEGATIVE)/messages/MessagesTable.cpp -o $(CPP_TOOL_MESSAGE_NEGATIVE)/MessagesTable.o
	$(CXX) $(TABLES_CXXFLAGS) $(subst -Ibuild/tables-generated/messages,-I$(CPP_TOOL_MESSAGE_NEGATIVE)/messages,$(CONFORMANCE_INCLUDES)) \
		test/conformance/cpp/main.cpp \
		$(filter-out build/conformance-cpp-objs/messages/MessagesTable.o,$(CONFORMANCE_OBJECTS)) \
		$(CPP_TOOL_MESSAGE_NEGATIVE)/MessagesTable.o -o $(CPP_TOOL_MESSAGE_NEGATIVE)/driver-bin
	@printf '#!/bin/sh\nexec "%s/driver-bin" "$$@"\n' "$(CURDIR)/$(CPP_TOOL_MESSAGE_NEGATIVE)" > $(CPP_TOOL_MESSAGE_NEGATIVE)/driver
	@chmod +x $(CPP_TOOL_MESSAGE_NEGATIVE)/driver
	@printf 'cpp %s/driver\n' "$(CPP_TOOL_MESSAGE_NEGATIVE)" > $(CPP_TOOL_MESSAGE_NEGATIVE)/drivers.txt
	./build/conformance-harness run --only cpp --drivers $(CPP_TOOL_MESSAGE_NEGATIVE)/drivers.txt \
		--work $(CPP_TOOL_MESSAGE_NEGATIVE)/work > $(CPP_TOOL_MESSAGE_NEGATIVE)/log 2>&1 || true
	@if grep -Eq '^wire +pass ([0-9]+)/\1 *$$' $(CPP_TOOL_MESSAGE_NEGATIVE)/log; then \
		echo "NEGATIVE CONTROL FAILED: the planted nested-union fault left the C++ wire surface green"; \
		cat $(CPP_TOOL_MESSAGE_NEGATIVE)/log; exit 1; \
	fi
	@grep -Eq '^json-write +FAIL [1-9][0-9]*/' $(CPP_TOOL_MESSAGE_NEGATIVE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: json-write did not go on past the cases the planted fault refuses"; \
		  cat $(CPP_TOOL_MESSAGE_NEGATIVE)/log; exit 1; }
	@grep -E '^(wire|json-write) ' $(CPP_TOOL_MESSAGE_NEGATIVE)/log
	@echo "negative control: judging a nested union's tag under an untaken outer arm turns the C++ wire surface RED"

test-cpp: conformance-cpp-tool-message conformance-cpp-tool-message-negative-control
