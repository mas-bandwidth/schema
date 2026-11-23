# Closed compressed-float bounds, SPEC §5.1 row 10.
PACKET_CFLOAT_ENDPOINT_DIR := build/packet-cfloat-endpoints/go
PACKET_CFLOAT_FIXTURE := test/packet-cfloat-endpoints

.PHONY: packet-cfloat-endpoints-go
packet-cfloat-endpoints-go: bin/schema tables-go-sibling-runtime
	./bin/schema generate --lang go --out $(PACKET_CFLOAT_ENDPOINT_DIR) $(PACKET_CFLOAT_FIXTURE)/PacketRatio.schema
	@printf 'module cfloat\n\ngo 1.26\n\nrequire github.com/mas-bandwidth/serialize.go/v2 v2.0.0\n\nreplace github.com/mas-bandwidth/serialize.go/v2 => "%s"\n' "$(abspath $(SERIALIZE_GO))" > $(PACKET_CFLOAT_ENDPOINT_DIR)/go.mod
	cp $(PACKET_CFLOAT_FIXTURE)/go/ratio_test.gotext $(PACKET_CFLOAT_ENDPOINT_DIR)/ratio_test.go
	cp $(PACKET_CFLOAT_FIXTURE)/ratio-endpoints.txt $(PACKET_CFLOAT_ENDPOINT_DIR)/ratio-endpoints.txt
	cd $(PACKET_CFLOAT_ENDPOINT_DIR) && go test -count=1 -v .

test-go: packet-cfloat-endpoints-go
