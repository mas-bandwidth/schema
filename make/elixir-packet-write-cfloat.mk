# Packet compressed-float writes (SPEC §5.1 row 10): a NaN, an infinity or a
# finite value off [min, max] is refused before quantizing, and a lawful
# endpoint or interior value writes and reads back to its quantized value.
# The recipes use $(MIX), $(ELIXIR) and $(ELIXIRC_STRICT) from make/elixir.mk,
# expanded when they run.
build/packet-write-cfloat/elixir/.stamp: bin/schema test/packet-write-cfloat/CFloat.schema
	./bin/schema generate --lang elixir --out build/packet-write-cfloat/elixir test/packet-write-cfloat/CFloat.schema
	@touch $@

.PHONY: packet-write-cfloat-elixir packet-write-cfloat-elixir-negative-control
packet-write-cfloat-elixir: build/packet-write-cfloat/elixir/.stamp
	$(MIX) format --check-formatted build/packet-write-cfloat/elixir/*.ex test/packet-write-cfloat/elixir/*.exs
	$(ELIXIR) test/packet-write-cfloat/elixir/main.exs

packet-write-cfloat-elixir-negative-control: packet-write-cfloat-elixir
	@mkdir -p build/packet-write-cfloat/elixir-negative/beam
	go run ./tools/sabotage -name packet-write-cfloat-elixir-range -out build/packet-write-cfloat/elixir-negative/functions.gotext internal/codegen/elixir/functions.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/functions.go":"%s/build/packet-write-cfloat/elixir-negative/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-cfloat/elixir-negative/overlay.json
	go run -overlay=build/packet-write-cfloat/elixir-negative/overlay.json ./cmd/schema generate --lang elixir --out build/packet-write-cfloat/elixir-negative/generated test/packet-write-cfloat/CFloat.schema
	$(ELIXIRC_STRICT) -o build/packet-write-cfloat/elixir-negative/beam build/packet-write-cfloat/elixir-negative/generated/*.ex
	@if $(ELIXIR) test/packet-write-cfloat/elixir/main.exs "$(CURDIR)/build/packet-write-cfloat/elixir-negative/generated" > build/packet-write-cfloat/elixir-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Elixir compressed-float range refusal removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-write-cfloat ' build/packet-write-cfloat/elixir-negative/log || \
		{ cat build/packet-write-cfloat/elixir-negative/log; echo 'NEGATIVE CONTROL FAILED: Elixir failed for another reason'; exit 1; }
	@echo 'packet write compressed float Elixir negative control: removed range refusal fails the off-range write cases'

test-elixir: packet-write-cfloat-elixir packet-write-cfloat-elixir-negative-control
