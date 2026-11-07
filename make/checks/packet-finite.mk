# The finite float rule on the Go packet target (SPEC §4.3, serialize format
# 2.0). test/packet-finite/Finite.schema holds a shape the Go emitter packs as
# one flat run, whose generated code places float bits itself, and two
# single-float shapes that keep the per-field form and reach serialize.go's own
# float calls. The checker needs no oracle: the patterns are the IEEE-754 bits.
build/packet-finite/go/.stamp: bin/schema test/packet-finite/Finite.schema make/checks/packet-finite.mk
	@mkdir -p build/packet-finite/go
	./bin/schema generate --lang go --out build/packet-finite/go/generated test/packet-finite/Finite.schema
	@printf 'module packetfinite\n\ngo 1.23\n' > build/packet-finite/go/generated/go.mod
	@touch $@

.PHONY: packet-finite-go
packet-finite-go: build/packet-finite/go/.stamp
	cd test/packet-finite/go && go run .

# Two controls, one per direction: each drops the flat form's guard and must
# fail exactly the flat vectors of that direction, named in the .expected list
# beside the checker. The per-field shapes reach the runtime and stay green.
# $(1) is the direction: read or write.
define packet-finite-go-control
	@mkdir -p build/packet-finite/go-negative-$(1)
	go run ./tools/sabotage -name packet-finite-go-$(1) \
		-out build/packet-finite/go-negative-$(1)/flat.go.txt internal/codegen/golang/flat.go
	@printf '{"Replace":{"%s/internal/codegen/golang/flat.go":"%s/build/packet-finite/go-negative-$(1)/flat.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-finite/go-negative-$(1)/overlay.json
	go build -overlay build/packet-finite/go-negative-$(1)/overlay.json -o build/packet-finite/go-negative-$(1)/schema ./cmd/schema
	./build/packet-finite/go-negative-$(1)/schema generate --lang go --out build/packet-finite/go-negative-$(1)/generated test/packet-finite/Finite.schema
	@printf 'module packetfinite\n\ngo 1.23\n' > build/packet-finite/go-negative-$(1)/generated/go.mod
	@sed 's|=> ../../../build/packet-finite/go/generated|=> "$(CURDIR)/build/packet-finite/go-negative-$(1)/generated"|' test/packet-finite/go/go.mod > build/packet-finite/go-negative-$(1)/go.mod.txt
	@printf '{"Replace":{"%s/test/packet-finite/go/go.mod":"%s/build/packet-finite/go-negative-$(1)/go.mod.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-finite/go-negative-$(1)/modoverlay.json
	cd test/packet-finite/go && go build -overlay ../../../build/packet-finite/go-negative-$(1)/modoverlay.json -o ../../../build/packet-finite/go-negative-$(1)/checker .
	@if ./build/packet-finite/go-negative-$(1)/checker > build/packet-finite/go-negative-$(1)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Go flat $(1) guard was removed and the finite checker stayed green'; exit 1; fi
	@sed -n 's|^FAILED: \([^:]*\):.*|\1|p' build/packet-finite/go-negative-$(1)/log | LC_ALL=C sort > build/packet-finite/go-negative-$(1)/failed
	@diff -u test/packet-finite/go/$(1).expected build/packet-finite/go-negative-$(1)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the Go flat $(1) sabotage did not fail exactly its vectors'; cat build/packet-finite/go-negative-$(1)/log; exit 1; }
	@echo "packet finite Go negative control ($(1)): the removed flat guard fails exactly $$(wc -l < build/packet-finite/go-negative-$(1)/failed | tr -d ' ') vectors"
endef

.PHONY: packet-finite-go-negative-control
packet-finite-go-negative-control: packet-finite-go
	$(call packet-finite-go-control,read)
	$(call packet-finite-go-control,write)

test-go: packet-finite-go packet-finite-go-negative-control
