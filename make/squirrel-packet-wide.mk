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

test-squirrel: packet-utf8-squirrel packet-utf8-squirrel-negative-control packet-defaults-squirrel packet-defaults-squirrel-negative-control packet-end-squirrel packet-end-squirrel-negative-control

build/packet-end/squirrel/.stamp: bin/schema test/packet-end/End.schema make/squirrel-packet-wide.mk
	@rm -rf build/packet-end/squirrel
	./bin/schema generate --lang squirrel --out build/packet-end/squirrel test/packet-end/End.schema
	@touch $@

.PHONY: packet-end-squirrel packet-end-squirrel-negative-control
packet-end-squirrel: build/packet-end/squirrel/.stamp toolchain-squirrel
	"$(SQ)" test/packet-end/squirrel/main.nut testdata/wire/packet-end build/packet-end/squirrel > build/packet-end/squirrel/stdout 2> build/packet-end/squirrel/stderr
	@cat build/packet-end/squirrel/stdout
	@if [ -s build/packet-end/squirrel/stderr ] || grep -q '^FAILED:' build/packet-end/squirrel/stdout; then \
		cat build/packet-end/squirrel/stderr >&2; exit 1; fi

# THE END-CHECK CONTROL: remove the generated read's end call, so the public
# read accepts a whole byte left after the object and nonzero unused bits in
# the final byte, and require the conformance driver to red both by name.
packet-end-squirrel-negative-control: packet-end-squirrel
	@rm -rf build/packet-end/squirrel-negative
	@mkdir -p build/packet-end/squirrel-negative
	go run ./tools/sabotage -name packet-end-squirrel-end -out build/packet-end/squirrel-negative/read.gotext internal/codegen/squirrel/read.go
	@printf '{"Replace":{"%s/internal/codegen/squirrel/read.go":"%s/build/packet-end/squirrel-negative/read.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-end/squirrel-negative/overlay.json
	go run -overlay=build/packet-end/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-end/squirrel-negative/gen test/packet-end/End.schema
	"$(SQ)" test/packet-end/squirrel/main.nut testdata/wire/packet-end build/packet-end/squirrel-negative/gen > build/packet-end/squirrel-negative/log 2>&1 || true
	@grep -Fq 'FAILED: trailing packet accepted' build/packet-end/squirrel-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Squirrel end check removal accepted the trailing byte'; cat build/packet-end/squirrel-negative/log; exit 1; }
	@grep -Fq 'FAILED: padding packet accepted' build/packet-end/squirrel-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Squirrel end check removal accepted the padding'; cat build/packet-end/squirrel-negative/log; exit 1; }
	@grep -Fm1 'FAILED: trailing packet accepted' build/packet-end/squirrel-negative/log
	@echo 'packet end Squirrel negative control: a missing end check accepts the trailing byte and the padding'


# THE BARE-INTEGER WIDTH CHECK (test/packet-write-bareint/cases.txt; SPEC §5.1
# row 11): the packet writer refuses a bare intN/uintN value its width cannot
# carry, in every build, and writes each boundary twin back exactly.
build/packet-write-bareint/squirrel/.stamp: bin/schema test/packet-write-bareint/BareInt.schema test/packet-write-bareint/squirrel/Carriers.schema make/squirrel-packet-wide.mk
	@rm -rf build/packet-write-bareint/squirrel
	./bin/schema generate --lang squirrel --out build/packet-write-bareint/squirrel/bareint test/packet-write-bareint/BareInt.schema
	./bin/schema generate --lang squirrel --out build/packet-write-bareint/squirrel/carriers test/packet-write-bareint/squirrel/Carriers.schema
	@touch $@

.PHONY: packet-write-bareint-squirrel packet-write-bareint-squirrel-negative-control
packet-write-bareint-squirrel: build/packet-write-bareint/squirrel/.stamp toolchain-squirrel
	"$(SQ)" test/packet-write-bareint/squirrel/main.nut test/packet-write-bareint/cases.txt build/packet-write-bareint/squirrel/bareint build/packet-write-bareint/squirrel/carriers > build/packet-write-bareint/squirrel/stdout 2> build/packet-write-bareint/squirrel/stderr
	@cat build/packet-write-bareint/squirrel/stdout
	@if [ -s build/packet-write-bareint/squirrel/stderr ] || grep -q '^FAILED:' build/packet-write-bareint/squirrel/stdout; then \
		cat build/packet-write-bareint/squirrel/stderr >&2; exit 1; fi

# THE WIDTH CONTROL: the writer keeps only its integer check, so an
# out-of-width bare value reaches the wire and the driver must fail by name.
packet-write-bareint-squirrel-negative-control: packet-write-bareint-squirrel
	@rm -rf build/packet-write-bareint/squirrel-negative
	@mkdir -p build/packet-write-bareint/squirrel-negative
	go run ./tools/sabotage -name packet-write-bareint-squirrel-width -out build/packet-write-bareint/squirrel-negative/write.gotext internal/codegen/squirrel/write.go
	@printf '{"Replace":{"%s/internal/codegen/squirrel/write.go":"%s/build/packet-write-bareint/squirrel-negative/write.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-write-bareint/squirrel-negative/overlay.json
	go run -overlay=build/packet-write-bareint/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-write-bareint/squirrel-negative/bareint test/packet-write-bareint/BareInt.schema
	go run -overlay=build/packet-write-bareint/squirrel-negative/overlay.json ./cmd/schema generate --lang squirrel --out build/packet-write-bareint/squirrel-negative/carriers test/packet-write-bareint/squirrel/Carriers.schema
	@if "$(SQ)" test/packet-write-bareint/squirrel/main.nut test/packet-write-bareint/cases.txt build/packet-write-bareint/squirrel-negative/bareint build/packet-write-bareint/squirrel-negative/carriers > build/packet-write-bareint/squirrel-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Squirrel unchecked bare-integer width passed'; cat build/packet-write-bareint/squirrel-negative/log; exit 1; fi
	@grep -Fq 'FAILED: int8-above-max: refused' build/packet-write-bareint/squirrel-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: no out-of-width case failed by name'; cat build/packet-write-bareint/squirrel-negative/log; exit 1; }
	@grep -Fxm1 'FAILED: int8-above-max: refused' build/packet-write-bareint/squirrel-negative/log
	@echo 'packet write bare-int Squirrel negative control: an unchecked width fails the shared cases'

test-squirrel: packet-write-bareint-squirrel packet-write-bareint-squirrel-negative-control
