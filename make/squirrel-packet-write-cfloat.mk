# Compressed floats (SPEC §5.1 row 10, PACKET-WIRE-ALGORITHM §2.5): the
# generated Squirrel packet writer refuses non-finite and off-range values
# before handing them to the compressed-float runtime.
build/packet-write-cfloat/squirrel/.stamp: build/schema-squirrel test/packet-write-cfloat/CFloat.schema test/packet-write-cfloat/squirrel/main.nut
	@rm -rf build/packet-write-cfloat/squirrel
	./build/schema-squirrel generate --lang squirrel --out build/packet-write-cfloat/squirrel test/packet-write-cfloat/CFloat.schema
	@touch $@

SQUIRREL_CFLOAT_DIR ?= build/packet-write-cfloat/squirrel

.PHONY: packet-write-cfloat-squirrel packet-write-cfloat-squirrel-negative-control
packet-write-cfloat-squirrel: build/packet-write-cfloat/squirrel/.stamp toolchain-squirrel
	@"$(SQ)" test/packet-write-cfloat/squirrel/main.nut test/packet-write-cfloat/cases.txt $(SQUIRREL_CFLOAT_DIR) > build/packet-write-cfloat/squirrel/stdout 2> build/packet-write-cfloat/squirrel/stderr; status=$$?; \
	cat build/packet-write-cfloat/squirrel/stdout; \
	if [ $$status -ne 0 ] || [ -s build/packet-write-cfloat/squirrel/stderr ] || ! grep -Fxq 'packet-write-cfloat-squirrel: shared cases passed' build/packet-write-cfloat/squirrel/stdout; then cat build/packet-write-cfloat/squirrel/stderr >&2; exit 1; fi

packet-write-cfloat-squirrel-negative-control: packet-write-cfloat-squirrel
	@rm -rf build/packet-write-cfloat/squirrel-negative
	@mkdir -p build/packet-write-cfloat/squirrel-negative
	go run ./tools/sabotage -name packet-write-cfloat-squirrel -out build/packet-write-cfloat/squirrel-negative/write.gotext internal/codegen/squirrel/write.go
	@printf '{"Replace":{"%s/internal/codegen/squirrel/write.go":"%s/build/packet-write-cfloat/squirrel-negative/write.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-write-cfloat/squirrel-negative/overlay.json
	go run -overlay=build/packet-write-cfloat/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-write-cfloat/squirrel-negative/generated test/packet-write-cfloat/CFloat.schema
	@if $(MAKE) packet-write-cfloat-squirrel SQUIRREL_CFLOAT_DIR=build/packet-write-cfloat/squirrel-negative/generated > build/packet-write-cfloat/squirrel-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Squirrel compressed-float range check removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-write-cfloat nan:' build/packet-write-cfloat/squirrel-negative/log || { cat build/packet-write-cfloat/squirrel-negative/log; exit 1; }
	@grep -Fq 'FAILED: packet-write-cfloat above-max:' build/packet-write-cfloat/squirrel-negative/log || { cat build/packet-write-cfloat/squirrel-negative/log; exit 1; }
	@test "$$(grep -c '^FAILED: packet-write-cfloat ' build/packet-write-cfloat/squirrel-negative/log)" -eq 6 || { cat build/packet-write-cfloat/squirrel-negative/log; exit 1; }
	@echo 'packet compressed-float Squirrel negative control: removed range check fails all six unlawful cases'

test-squirrel: packet-write-cfloat-squirrel packet-write-cfloat-squirrel-negative-control
