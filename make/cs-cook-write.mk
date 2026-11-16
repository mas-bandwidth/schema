# make/cs-cook-write.mk — the C# cook-write gate (docs/SPEC-TABLES.md §7.2).
#
# The leg writes every cook-write row byte for byte as the tool does: an absent
# optional's payload is zero and a counted array's slots past its live count
# are zero, in both byte orders. The other surfaces of this leg belong to other
# cards, so the run's own exit status is not this target's verdict: it reads
# the cook-write row and the relaxed list from the log it writes.
#
# Its negative control re-emits the C# cook writer with the dead-slot zeroing
# removed, under build/ only, and requires the same gate to go red on it.

CS_COOK_WRITE := build/conformance-cs-cook-write
CS_COOK_WRITE_NC := build/conformance-cs-cook-write-negative-control

.PHONY: conformance-cs-cook-write conformance-cs-cook-write-negative-control
conformance-cs-cook-write: build-conformance-cs build/conformance-harness
	@mkdir -p $(CS_COOK_WRITE)
	./build/conformance-harness run --only cs --work $(CS_COOK_WRITE)/run > $(CS_COOK_WRITE)/log 2>&1 || true
	@awk '$$1=="cook-write" && $$2=="pass" { split($$3, n, "/"); if (n[1] != "" && n[1] == n[2] && $$4 == "") ok=1 } END { exit(ok ? 0 : 1) }' \
		$(CS_COOK_WRITE)/log || { cat $(CS_COOK_WRITE)/log; echo "conformance-cs-cook-write FAILED: cook-write is not pass N/N with the same N twice and no +Ka absence suffix"; exit 1; }
	@if grep -Eq '^conformance RELAXED .* cs / cook-write:' $(CS_COOK_WRITE)/log; then \
		cat $(CS_COOK_WRITE)/log; echo "conformance-cs-cook-write FAILED: cs / cook-write carries a RELAXED line"; exit 1; fi
	@grep -E '^cook-write ' $(CS_COOK_WRITE)/log

conformance-cs-cook-write-negative-control: build-conformance-cs build/conformance-harness
	@rm -rf $(CS_COOK_WRITE_NC) && mkdir -p $(CS_COOK_WRITE_NC)/proj
	@cp -R build/tables-generated-cs $(CS_COOK_WRITE_NC)/generated
	@for f in $(CS_COOK_WRITE_NC)/generated/examples/TabledemoTable.cs \
		$(CS_COOK_WRITE_NC)/generated/examples/TabledemoRegion.cs; do \
		perl -pi -e 's/\Qint count = f.IsArray ? live : 1;\E/int count = f.IsArray ? f.Dynamic ? live : f.ArrayBound : 1; \/* SABOTAGED: dead counted slots written *\//' $$f; \
	done
	@grep -rqF 'SABOTAGED: dead counted slots written' $(CS_COOK_WRITE_NC)/generated/examples || \
		{ echo "NEGATIVE CONTROL FAILED: the dead-slot sabotage did not apply"; exit 1; }
	@cp -R test/conformance/cs/src test/conformance/cs/schemaconformance.csproj $(CS_COOK_WRITE_NC)/proj/
	@$(DOTNET) build $(CS_COOK_WRITE_NC)/proj --nologo -v quiet \
		-p:TablesGeneratedDir=$(CURDIR)/$(CS_COOK_WRITE_NC)/generated \
		-p:CardsGeneratedDir=$(CURDIR)/build/tables-generated-cs/cards \
		-o $(CS_COOK_WRITE_NC)/checker > $(CS_COOK_WRITE_NC)/build.log 2>&1 || \
		{ cat $(CS_COOK_WRITE_NC)/build.log; echo "NEGATIVE CONTROL FAILED: the sabotaged driver does not compile"; exit 1; }
	@printf 'cs %s %s/checker/schemaconformance.dll\n' "$(DOTNET)" "$(CURDIR)/$(CS_COOK_WRITE_NC)" > $(CS_COOK_WRITE_NC)/drivers
	@./build/conformance-harness run --only cs --drivers $(CS_COOK_WRITE_NC)/drivers --work $(CS_COOK_WRITE_NC)/run > $(CS_COOK_WRITE_NC)/log 2>&1 || true
	@if awk '$$1=="cook-write" && $$2=="pass" { split($$3, n, "/"); if (n[1] != "" && n[1] == n[2] && $$4 == "") ok=1 } END { exit(ok ? 0 : 1) }' \
		$(CS_COOK_WRITE_NC)/log; then \
		cat $(CS_COOK_WRITE_NC)/log; echo "NEGATIVE CONTROL FAILED: writing the dead counted slots left the C# cook-write gate green"; exit 1; fi
	@grep -E '^cook-write ' $(CS_COOK_WRITE_NC)/log
	@echo "negative control: writing the dead counted slots turns the C# cook-write gate red"

test-cs: conformance-cs-cook-write conformance-cs-cook-write-negative-control
