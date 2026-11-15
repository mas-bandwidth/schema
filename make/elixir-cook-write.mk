# THE ELIXIR COOK WRITE (docs/SPEC-TABLES.md §7.2, §7.6): an absent optional's
# payload and a counted array's slots past its live count are zero bytes in
# the cooked image, in both byte orders. The target runs the harness's Elixir
# leg and holds its cook-write surface whole: test/conformance/elixir/cook-write-gate
# reads the log and the work directory (the leg's other surfaces are other
# gates', so the run's own exit status is not this target's verdict).
CONFORMANCE_ELIXIR_COOK_WRITE := build/conformance-elixir-cook-write

.PHONY: conformance-elixir-cook-write conformance-elixir-cook-write-negative-control
conformance-elixir-cook-write: build/conformance-harness build-conformance-elixir
	@rm -rf $(CONFORMANCE_ELIXIR_COOK_WRITE) && mkdir -p $(CONFORMANCE_ELIXIR_COOK_WRITE)
	@./build/conformance-harness run --only elixir --work $(CONFORMANCE_ELIXIR_COOK_WRITE)/work \
		> $(CONFORMANCE_ELIXIR_COOK_WRITE)/log 2>&1 || true
	@sh test/conformance/elixir/cook-write-gate $(CONFORMANCE_ELIXIR_COOK_WRITE)/log $(CONFORMANCE_ELIXIR_COOK_WRITE)/work

# Its control re-emits the leg's cook writer, under build/ only, with a counted
# array's dead record slots written with the element's declared defaults, and
# the gate must go red on root_full.
CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE := build/elixir-conformance-negative-cook-write
ELIXIR_COOK_WRITE_DEAD_SED := -e 's|if not counted and f.elem_t == :record and|if f.elem_t == :record and|'

conformance-elixir-cook-write-negative-control: build/conformance-harness
	$(call ELIXIR_SABOTAGED_BUILD,$(CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE),internal/codegen/elixirtable/cookwrite.go,ELIXIR_COOK_WRITE_DEAD_SED)
	@ELIXIR_TABLES_EBIN=$(CURDIR)/$(CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE)/ebin ./build/conformance-harness run --only elixir \
		--work $(CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE)/work > $(CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE)/log 2>&1 || true
	@if sh test/conformance/elixir/cook-write-gate $(CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE)/log $(CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE)/work; then \
		echo "NEGATIVE CONTROL FAILED: dead counted slots written nonzero left the cook-write gate green"; exit 1; \
	fi
	@grep -q "elixir / cook-write: root_full:" $(CONFORMANCE_NEGATIVE_ELIXIR_COOK_WRITE)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the cook-write gate went red, but not on root_full"; exit 1; }
	@echo "elixir cook-write negative control: dead counted slots written nonzero red the cook-write gate on root_full"

test-elixir: conformance-elixir-cook-write conformance-elixir-cook-write-negative-control
