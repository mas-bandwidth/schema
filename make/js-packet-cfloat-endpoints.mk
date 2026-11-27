# Closed compressed-float bounds, SPEC §5.1 row 10, through both generated
# JavaScript packet paths on the normal test-js entry.
PACKET_CFLOAT_ENDPOINT_JS_DIR := build/packet-cfloat-endpoints/js
PACKET_CFLOAT_ENDPOINT_FIXTURE := test/packet-cfloat-endpoints

$(PACKET_CFLOAT_ENDPOINT_JS_DIR)/.stamp: bin/schema $(PACKET_CFLOAT_ENDPOINT_FIXTURE)/PacketRatio.schema make/js-packet-cfloat-endpoints.mk
	./bin/schema generate --lang js --out $(PACKET_CFLOAT_ENDPOINT_JS_DIR) $(PACKET_CFLOAT_ENDPOINT_FIXTURE)/PacketRatio.schema
	@touch $@

.PHONY: packet-cfloat-endpoints-js
packet-cfloat-endpoints-js: build/mustrun $(PACKET_CFLOAT_ENDPOINT_JS_DIR)/.stamp
	$(MUSTRUN) -name packet-cfloat-endpoints-js -count 4 -- $(NODE) $(PACKET_CFLOAT_ENDPOINT_FIXTURE)/js/main.mjs $(PACKET_CFLOAT_ENDPOINT_FIXTURE)/ratio-endpoints.txt $(PACKET_CFLOAT_ENDPOINT_JS_DIR)
	NODE_ENV=production $(MUSTRUN) -name packet-cfloat-endpoints-js-production -count 4 -- $(NODE) $(PACKET_CFLOAT_ENDPOINT_FIXTURE)/js/main.mjs $(PACKET_CFLOAT_ENDPOINT_FIXTURE)/ratio-endpoints.txt $(PACKET_CFLOAT_ENDPOINT_JS_DIR)

test-js: packet-cfloat-endpoints-js
