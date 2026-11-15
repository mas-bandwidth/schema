# Packet bare-integer write width (SPEC §5.1 row 1): the generated packet
# writer refuses a bare intN/uintN value outside its declared width, and a
# value that is not an integer, in every build; each width's min and max
# write and read back. The driver reads the shared cases list.
# The recipes use $(MIX), $(ELIXIR) and $(ELIXIRC_STRICT) from make/elixir.mk,
# expanded when they run.
build/packet-write-bareint/elixir/.stamp: bin/schema test/packet-write-bareint/BareInt.schema make/elixir-packet-write-bareint.mk
	@mkdir -p build/packet-write-bareint/elixir
	./bin/schema generate --lang elixir --out build/packet-write-bareint/elixir test/packet-write-bareint/BareInt.schema
	@touch $@

.PHONY: packet-write-bareint-elixir packet-write-bareint-elixir-negative-control
packet-write-bareint-elixir: build/packet-write-bareint/elixir/.stamp
	$(MIX) format --check-formatted build/packet-write-bareint/elixir/*.ex test/packet-write-bareint/elixir/*.exs
	@mkdir -p build/packet-write-bareint/elixir-beam
	$(ELIXIRC_STRICT) -o build/packet-write-bareint/elixir-beam build/packet-write-bareint/elixir/*.ex
	$(ELIXIR) test/packet-write-bareint/elixir/main.exs test/packet-write-bareint/cases.txt

packet-write-bareint-elixir-negative-control: packet-write-bareint-elixir
	@mkdir -p build/packet-write-bareint/elixir-negative/beam
	go run ./tools/sabotage -name packet-write-bareint-elixir -out build/packet-write-bareint/elixir-negative/functions.gotext internal/codegen/elixir/functions.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/functions.go":"%s/build/packet-write-bareint/elixir-negative/functions.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-write-bareint/elixir-negative/overlay.json
	go run -overlay=build/packet-write-bareint/elixir-negative/overlay.json ./cmd/schema generate --lang elixir --out build/packet-write-bareint/elixir-negative/generated test/packet-write-bareint/BareInt.schema
	$(ELIXIRC_STRICT) -o build/packet-write-bareint/elixir-negative/beam build/packet-write-bareint/elixir-negative/generated/*.ex
	@if $(ELIXIR) test/packet-write-bareint/elixir/main.exs test/packet-write-bareint/cases.txt "$(CURDIR)/build/packet-write-bareint/elixir-negative/generated" > build/packet-write-bareint/elixir-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: removing the bare-integer write check left packet-write-bareint-elixir green'; exit 1; fi
	@grep -Fq 'FAILED: packet-write-bareint int8-above-max refused' build/packet-write-bareint/elixir-negative/log && \
		grep -Fq 'FAILED: packet-write-bareint uint64-negative refused' build/packet-write-bareint/elixir-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: packet-write-bareint-elixir went red, but not on the out-of-width cases'; cat build/packet-write-bareint/elixir-negative/log; exit 1; }
	@if grep -F 'FAILED: ' build/packet-write-bareint/elixir-negative/log | grep -Fq 'reads back'; then cat build/packet-write-bareint/elixir-negative/log; echo 'NEGATIVE CONTROL FAILED: a boundary case went red with the write check removed'; exit 1; fi
	@echo 'packet-write-bareint-elixir negative control: removing the bare-integer write check reds packet-write-bareint-elixir on the out-of-width cases'

test-elixir: packet-write-bareint-elixir packet-write-bareint-elixir-negative-control
