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
	@test "$$(grep -Fc 'g.pf("\t%s = [%s]byte{"' internal/codegen/golang/golang.go)" = 1 || \
		{ echo 'NEGATIVE CONTROL FAILED: expected exactly one constructor byte-array assignment anchor'; exit 1; }
	@sed 's|g.pf("\\t%s = \[%s\]byte{"|g.pf("\\t// %s = [%s]byte{"|' internal/codegen/golang/golang.go > build/packet-defaults/go-negative/golang.go.txt
	@if cmp -s internal/codegen/golang/golang.go build/packet-defaults/go-negative/golang.go.txt; then \
		echo 'NEGATIVE CONTROL FAILED: constructor sabotage did not apply'; exit 1; fi
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
