# Elixir pointer-node defaults: a node of the node table reads its declared
# defaults exactly as the root does (docs/SPEC-TABLES.md §3, §3.1, §4). The
# control re-emits graphdemo's loader into build/, removes the node default
# fill from that generated copy, and requires the card's test to go red by
# name on the node values. No tracked file is edited.

ELIXIR_POINTER_NODE_DEFAULTS_NEG := build/elixir-pointer-node-defaults-negative-control

.PHONY: tables-elixir-pointer-node-defaults-negative-control
tables-elixir-pointer-node-defaults-negative-control: bin/schema
	@rm -rf $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)
	@mkdir -p $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/generated
	./bin/schema generate --lang elixir --out $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/generated tables/pointers
	@sed -i '/value = Map.merge(defaults(node_desc), value)/d' $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/generated/Table.ex
	@if env ELIXIR_POINTER_NODE_DEFAULTS_DIR=$(CURDIR)/$(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/generated \
		SCHEMA_SLOW=1 SCHEMA_REQUIRE_CORPUS=1 \
		go test -count=1 -v -run '^TestElixirPointerNodesReadDeclaredDefaults$$' ./internal/codegen/elixirtable/ \
		> $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: removing the node default fill left the pointer-node defaults test green'; \
		cat $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/log; exit 1; fi
	@grep -Fq -- '--- FAIL: TestElixirPointerNodesReadDeclaredDefaults ' $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the test went red, but not by name'; cat $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/log; exit 1; }
	@grep -Fq 'loaded value differs from the engine' $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the test went red, but not on a node value'; cat $(ELIXIR_POINTER_NODE_DEFAULTS_NEG)/log; exit 1; }
	@echo 'pointer node defaults Elixir negative control: removing the node default fill fails the node-value checks'

test-elixir: tables-elixir-pointer-node-defaults-negative-control
