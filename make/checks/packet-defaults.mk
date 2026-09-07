# The shared packet-default oracle. Later ports consume these same C++ bytes
# and bit counts. The plain twin has the same values and no specified defaults.
PACKET_DEFAULT_SCHEMAS := test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema

build/packet-defaults/cpp/.stamp: bin/schema $(PACKET_DEFAULT_SCHEMAS) make/checks/packet-defaults.mk
	@mkdir -p build/packet-defaults/cpp
	./bin/schema generate --lang cpp --out build/packet-defaults/cpp/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang cpp --out build/packet-defaults/cpp/plain test/packet-defaults/Plain.schema
	@touch $@

build/schema_test_packet_defaults: build/packet-defaults/cpp/.stamp test/packet-defaults/cpp_main.cpp $(SERIALIZE)/serialize.h
	$(CXX) $(CXXFLAGS) -Ibuild/packet-defaults/cpp/defaults -Ibuild/packet-defaults/cpp/plain test/packet-defaults/cpp_main.cpp -o $@

.PHONY: packet-defaults-cpp
packet-defaults-cpp: build/schema_test_packet_defaults
	env -u SCHEMA_UPDATE_WIRE_GOLDENS ./build/schema_test_packet_defaults testdata/wire/packet-defaults

# Golden updates are explicit. Ordinary test runs only compare checked-in bytes.
.PHONY: packet-defaults-goldens
packet-defaults-goldens: build/schema_test_packet_defaults
	@mkdir -p testdata/wire/packet-defaults
	./build/schema_test_packet_defaults testdata/wire/packet-defaults --write-goldens

update-goldens: packet-defaults-goldens
test: packet-defaults-cpp

# Packet defaults are construction only. The C++ oracle and defaultless twin
# establish the bytes independently of this port's constructors and codecs.
build/packet-defaults/go/.stamp: bin/schema test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema make/checks/packet-defaults.mk
	@mkdir -p build/packet-defaults/go
	./bin/schema generate --lang go --out build/packet-defaults/go/defaults test/packet-defaults/Defaults.schema
	./bin/schema generate --lang go --out build/packet-defaults/go/plain test/packet-defaults/Plain.schema
	@printf 'module packetdefaults\n\ngo 1.23\n' > build/packet-defaults/go/defaults/go.mod
	@printf 'module packetplain\n\ngo 1.23\n' > build/packet-defaults/go/plain/go.mod
	@touch $@

.PHONY: packet-defaults-go
packet-defaults-go: build/packet-defaults/go/.stamp packet-defaults-cpp
	cd test/packet-defaults/go && go run . ../../../testdata/wire/packet-defaults

# Comment out the emitted byte-array assignment, preserving its length and the
# rest of the constructor. The generated code must compile and fail on bytes.
.PHONY: packet-defaults-go-negative-control
packet-defaults-go-negative-control: packet-defaults-go
	@mkdir -p build/packet-defaults/go-negative
	go run ./tools/sabotage -name packet-defaults-go-constructor-bytes \
		-out build/packet-defaults/go-negative/golang.go.txt internal/codegen/golang/golang.go
	@printf '{"Replace":{"%s/internal/codegen/golang/golang.go":"%s/build/packet-defaults/go-negative/golang.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/go-negative/overlay.json
	go build -overlay build/packet-defaults/go-negative/overlay.json -o build/packet-defaults/go-negative/schema ./cmd/schema
	./build/packet-defaults/go-negative/schema generate --lang go --out build/packet-defaults/go-negative/generated test/packet-defaults/Defaults.schema
	@printf 'module packetdefaults\n\ngo 1.23\n' > build/packet-defaults/go-negative/generated/go.mod
	@sed 's|=> ../../../build/packet-defaults/go/defaults|=> $(CURDIR)/build/packet-defaults/go-negative/generated|' test/packet-defaults/go/go.mod > build/packet-defaults/go-negative/go.mod.txt
	@printf '{"Replace":{"%s/test/packet-defaults/go/go.mod":"%s/build/packet-defaults/go-negative/go.mod.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/go-negative/modoverlay.json
	cd test/packet-defaults/go && go build -overlay ../../../build/packet-defaults/go-negative/modoverlay.json -o ../../../build/packet-defaults/go-negative/checker .
	@if env -u SCHEMA_UPDATE_WIRE_GOLDENS ./build/packet-defaults/go-negative/checker testdata/wire/packet-defaults > build/packet-defaults/go-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: constructor bytes disappeared and the runtime check stayed green'; exit 1; fi
	@grep -Fxq 'FAILED: packet-default constructor bytes' build/packet-defaults/go-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: failure did not name constructor bytes'; cat build/packet-defaults/go-negative/log; exit 1; }
	@grep -Fxm1 'FAILED: packet-default constructor bytes' build/packet-defaults/go-negative/log
	@echo 'packet defaults Go negative control: missing constructor bytes fail the runtime check'

test-go: packet-defaults-go packet-defaults-go-negative-control

build/packet-defaults/c/.stamp: bin/schema $(PACKET_DEFAULT_SCHEMAS) make/checks/packet-defaults.mk
	@mkdir -p build/packet-defaults/c
	./bin/schema generate --lang c --out build/packet-defaults/c test/packet-defaults/Defaults.schema
	@touch $@

build/packet-defaults/c-test: build/packet-defaults/c/.stamp test/packet-defaults/c_main.c $(SERIALIZE_C)/serialize.h $(SERIALIZE_C)/serialize.c
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Ibuild/packet-defaults/c -I$(SERIALIZE_C) \
		test/packet-defaults/c_main.c $(SERIALIZE_C)/serialize.c -o $@ -lm

.PHONY: packet-defaults-c packet-defaults-c-negative-control
packet-defaults-c: build/packet-defaults/c-test packet-defaults-cpp
	./build/packet-defaults/c-test testdata/wire/packet-defaults

packet-defaults-c-negative-control: packet-defaults-c
	@mkdir -p build/packet-defaults/c-negative
	go run ./tools/sabotage -name packet-defaults-c-constructor-bytes \
		-out build/packet-defaults/c-negative/dispatch.gotext internal/codegen/c/dispatch.go
	@printf '{"Replace":{"%s/internal/codegen/c/dispatch.go":"%s/build/packet-defaults/c-negative/dispatch.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-defaults/c-negative/overlay.json
	go build -overlay=build/packet-defaults/c-negative/overlay.json -o build/packet-defaults/c-negative/schema ./cmd/schema
	./build/packet-defaults/c-negative/schema generate --lang c --out build/packet-defaults/c-negative/generated test/packet-defaults/Defaults.schema
	$(CC) -std=c99 -Wall -Wextra -Werror -Wtype-limits $(C_TAUTOLOGICAL) \
		-O2 -ffp-contract=off -Ibuild/packet-defaults/c-negative/generated -I$(SERIALIZE_C) \
		test/packet-defaults/c_main.c $(SERIALIZE_C)/serialize.c -o build/packet-defaults/c-negative/checker -lm
	@if ./build/packet-defaults/c-negative/checker testdata/wire/packet-defaults > build/packet-defaults/c-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: missing constructor bytes passed in C'; exit 1; fi
	@grep -Fxq 'FAILED: packet-default constructor bytes' build/packet-defaults/c-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: C failed for another reason'; cat build/packet-defaults/c-negative/log; exit 1; }
	@grep -Fxm1 'FAILED: packet-default constructor bytes' build/packet-defaults/c-negative/log

# make test invokes test-c through TEST_LEGS in make/c.mk.
test-c: packet-defaults-c packet-defaults-c-negative-control
