# make/squirrel-packet-wide.mk — the SQUIRREL legs of the packet text
# and value-defaults checks (test/packet-text/README.md,
# test/packet-defaults/README.md; SPEC §4.2, §4.7, §4.8). The Makefile's
# wildcard include pulls this in.

build/packet-text/squirrel/.stamp: bin/schema test/packet-text/Narrow.schema make/squirrel-packet-wide.mk
	@rm -rf build/packet-text/squirrel
	./bin/schema generate --lang squirrel --out build/packet-text/squirrel test/packet-text/Narrow.schema
	@touch $@

.PHONY: packet-utf8-squirrel packet-utf8-squirrel-negative-control
packet-utf8-squirrel: build/packet-text/squirrel/.stamp build/packet-text/cpp/driver build/packet-text/harness toolchain-squirrel
	./build/packet-text/harness -oracle build/packet-text/cpp/driver "$(SQ)" test/packet-text/squirrel/main.nut

# THE UTF-8 READ CONTROL: the generated reader stops validating UTF-8 on
# string(N), so the mutated corpus must disagree with the C++ oracle.
packet-utf8-squirrel-negative-control: packet-utf8-squirrel
	@rm -rf build/packet-text/squirrel-negative
	@mkdir -p build/packet-text/squirrel-negative
	go run ./tools/sabotage -name packet-utf8-squirrel-read -out build/packet-text/squirrel-negative/read.gotext internal/codegen/squirrel/read.go
	@printf '{"Replace":{"%s/internal/codegen/squirrel/read.go":"%s/build/packet-text/squirrel-negative/read.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-text/squirrel-negative/overlay.json
	go run -overlay=build/packet-text/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-text/squirrel-negative/gen test/packet-text/Narrow.schema
	@if ./build/packet-text/harness -oracle build/packet-text/cpp/driver -mutations-only "$(SQ)" test/packet-text/squirrel/main.nut "$(CURDIR)/build/packet-text/squirrel-negative/gen" > build/packet-text/squirrel-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: Squirrel unchecked UTF-8 read passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/squirrel-negative/log || { cat build/packet-text/squirrel-negative/log; exit 1; }
	@echo 'packet UTF-8 Squirrel negative control: unchecked read fails bit-flip agreement'

build/packet-defaults/squirrel/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/squirrel-packet-wide.mk
	@rm -rf build/packet-defaults/squirrel
	./bin/schema generate --lang squirrel --out build/packet-defaults/squirrel/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang squirrel --out build/packet-defaults/squirrel/plain test/packet-defaults/Plain.schema
	@touch $@

.PHONY: packet-defaults-squirrel packet-defaults-squirrel-negative-control
packet-defaults-squirrel: build/packet-defaults/squirrel/.stamp packet-defaults-cpp toolchain-squirrel
	"$(SQ)" test/packet-defaults/squirrel/main.nut testdata/wire/packet-defaults > build/packet-defaults/squirrel/stdout 2> build/packet-defaults/squirrel/stderr
	@cat build/packet-defaults/squirrel/stdout
	@if [ -s build/packet-defaults/squirrel/stderr ] || grep -q '^FAILED:' build/packet-defaults/squirrel/stdout; then \
		cat build/packet-defaults/squirrel/stderr >&2; exit 1; fi

# THE CONSTRUCTOR-BYTES CONTROL: drop only the emitted byte-literal assignments
# in the constructor and require the runtime check to fail on constructor bytes.
packet-defaults-squirrel-negative-control: packet-defaults-squirrel
	@rm -rf build/packet-defaults/squirrel-negative
	@mkdir -p build/packet-defaults/squirrel-negative
	go run ./tools/sabotage -name packet-defaults-squirrel-constructor-bytes -out build/packet-defaults/squirrel-negative/squirrel.gotext internal/codegen/squirrel/squirrel.go
	@printf '{"Replace":{"%s/internal/codegen/squirrel/squirrel.go":"%s/build/packet-defaults/squirrel-negative/squirrel.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-defaults/squirrel-negative/overlay.json
	go run -overlay=build/packet-defaults/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-defaults/squirrel-negative/generated test/packet-defaults/Defaults.schema
	"$(SQ)" test/packet-defaults/squirrel/main.nut testdata/wire/packet-defaults build/packet-defaults/squirrel-negative/generated build/packet-defaults/squirrel/plain > build/packet-defaults/squirrel-negative/log 2>&1
	@grep -Fxq 'FAILED: packet-default constructor bytes' build/packet-defaults/squirrel-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Squirrel constructor bytes did not fail the runtime check'; cat build/packet-defaults/squirrel-negative/log; exit 1; }
	@grep -Fxm1 'FAILED: packet-default constructor bytes' build/packet-defaults/squirrel-negative/log
	@echo 'packet defaults Squirrel negative control: missing constructor bytes fail the runtime check'

test-squirrel: packet-utf8-squirrel packet-utf8-squirrel-negative-control packet-defaults-squirrel packet-defaults-squirrel-negative-control

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
