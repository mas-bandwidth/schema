# PACKET WRITE text content (docs/SPEC.md §5.1 row 8, docs/PACKET-WIRE-ALGORITHM.md
# §2.4 and §3.6): the writer holds the used bytes of string(N) to no zero byte
# and to well-formed UTF-8, refusing in every build with ArgumentError from
# write_<name>! and an error from its verdict form. test/packet-write-text/cases.txt
# is the shared case list; the driver writes every case, requires each ill-formed
# one refused, and reads each accepted one back byte exact. The generated module
# is built from a copy of the shared schema with the type renamed; the copy still
# declares the same string(8) field. The recipes use $(MIX), $(ELIXIR) and
# $(ELIXIRC_STRICT) from make/elixir.mk, expanded when they run.
build/packet-write-text/elixir/.stamp: bin/schema test/packet-write-text/Text.schema make/elixir-packet-write-text.mk
	@rm -rf build/packet-write-text/elixir
	@mkdir -p build/packet-write-text/elixir/source
	sed 's/^type TextPacket$$/type Packet/' test/packet-write-text/Text.schema > build/packet-write-text/elixir/source/Text.schema
	@grep -q '^type Packet$$' build/packet-write-text/elixir/source/Text.schema || \
		{ echo 'packet-write-text-elixir: the shared schema no longer names type TextPacket'; exit 1; }
	./bin/schema generate --lang elixir --out build/packet-write-text/elixir build/packet-write-text/elixir/source/Text.schema
	@touch $@

.PHONY: packet-write-text-elixir packet-write-text-elixir-negative-control
packet-write-text-elixir: build/packet-write-text/elixir/.stamp
	$(MIX) format --check-formatted build/packet-write-text/elixir/*.ex test/packet-write-text/elixir/*.exs
	$(ELIXIR) test/packet-write-text/elixir/main.exs

packet-write-text-elixir-negative-control: packet-write-text-elixir
	@mkdir -p build/packet-write-text/elixir-negative/beam
	go run ./tools/sabotage -name packet-write-text-elixir-write \
		-out build/packet-write-text/elixir-negative/functions.gotext internal/codegen/elixir/functions.go
	@printf '{"Replace":{"%s/internal/codegen/elixir/functions.go":"%s/build/packet-write-text/elixir-negative/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-text/elixir-negative/overlay.json
	go run -overlay=build/packet-write-text/elixir-negative/overlay.json ./cmd/schema generate --lang elixir \
		--out build/packet-write-text/elixir-negative/generated build/packet-write-text/elixir/source/Text.schema
	$(ELIXIRC_STRICT) -o build/packet-write-text/elixir-negative/beam build/packet-write-text/elixir-negative/generated/*.ex
	@if $(ELIXIR) test/packet-write-text/elixir/main.exs "$(CURDIR)/build/packet-write-text/elixir-negative/generated" \
			> build/packet-write-text/elixir-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the removed write-side text checks passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-write-text refused ' build/packet-write-text/elixir-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Elixir failed for another reason'; cat build/packet-write-text/elixir-negative/log; exit 1; }
	@echo 'packet write text Elixir negative control: removed write-side text checks red the ill-formed cases'

test-elixir: packet-write-text-elixir packet-write-text-elixir-negative-control
