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
