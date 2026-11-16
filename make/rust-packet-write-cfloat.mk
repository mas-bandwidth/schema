
# Packet compressed-float writes (SPEC §5.1 row 10): a NaN, an infinity or a
# finite value off [min, max] is held by a debug assert before quantizing, and
# a lawful endpoint or interior value writes and reads back to its quantized
# value. The recipes use $(SERIALIZE_RS) and $(RUSTUP_BIN) from make/rust.mk,
# expanded when they run.
PACKET_CFLOAT_RUST_DIR ?= build/packet-write-cfloat/rust
build/packet-write-cfloat/rust/.stamp: bin/schema test/packet-write-cfloat/CFloat.schema make/rust-packet-write-cfloat.mk
	@mkdir -p build/packet-write-cfloat/rust/cfloat/src
	./bin/schema generate --lang rust --out build/packet-write-cfloat/rust/cfloat/src test/packet-write-cfloat/CFloat.schema
	@printf '[package]\nname = "packetcfloat"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\nserialize = { package = "serialize-official", path = "%s/$(SERIALIZE_RS)" }\n' "$(CURDIR)" > build/packet-write-cfloat/rust/cfloat/Cargo.toml
	@touch $@

.PHONY: packet-write-cfloat-rust packet-write-cfloat-rust-negative-control
packet-write-cfloat-rust: build/packet-write-cfloat/rust/.stamp
	@mkdir -p $(PACKET_CFLOAT_RUST_DIR)/driver/src
	@cp test/packet-write-cfloat/rust/src/main.rs $(PACKET_CFLOAT_RUST_DIR)/driver/src/main.rs
	@printf '[package]\nname = "packet-write-cfloat-test"\nversion = "0.0.0"\nedition = "2024"\n\n[dependencies]\npacketcfloat = { path = "%s/$(PACKET_CFLOAT_RUST_DIR)/cfloat" }\nserialize = { package = "serialize-official", path = "%s/$(SERIALIZE_RS)" }\n' "$(CURDIR)" "$(CURDIR)" > $(PACKET_CFLOAT_RUST_DIR)/driver/Cargo.toml
	PATH="$(RUSTUP_BIN):$$PATH" cargo run --offline --quiet --manifest-path $(PACKET_CFLOAT_RUST_DIR)/driver/Cargo.toml -- test/packet-write-cfloat/cases.txt

packet-write-cfloat-rust-negative-control: packet-write-cfloat-rust
	@rm -rf build/packet-write-cfloat/rust-negative
	@mkdir -p build/packet-write-cfloat/rust-negative
	@cp -R build/packet-write-cfloat/rust/cfloat build/packet-write-cfloat/rust-negative/cfloat
	@sed -i.bak '/debug_assert!.*is_finite()/d' build/packet-write-cfloat/rust-negative/cfloat/src/cfloat.rs
	@! cmp -s build/packet-write-cfloat/rust/cfloat/src/cfloat.rs build/packet-write-cfloat/rust-negative/cfloat/src/cfloat.rs || { echo 'negative control sabotage did not apply'; exit 1; }
	@if $(MAKE) packet-write-cfloat-rust PACKET_CFLOAT_RUST_DIR=build/packet-write-cfloat/rust-negative > build/packet-write-cfloat/rust-negative/log 2>&1; then echo 'negative control failed: packet-write-cfloat-rust passed'; exit 1; fi
	@grep -Fq 'FAILED: above-max was written but must be refused' build/packet-write-cfloat/rust-negative/log || { cat build/packet-write-cfloat/rust-negative/log; exit 1; }
	@echo 'packet-write-cfloat-rust negative control: removed range check is detected'

test-rust: packet-write-cfloat-rust packet-write-cfloat-rust-negative-control
