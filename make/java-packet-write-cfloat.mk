# COMPRESSED FLOAT ON THE PACKET WRITE (SPEC §5.1 row 10,
# docs/PACKET-WIRE-ALGORITHM.md §2.5). A NaN, an infinity, or a finite value
# outside [min, max] is a debug assert in checkWrite, active under -ea and
# only there. The integer clamp after rounding stays. The type is named
# CFloat, and this emitter's outer class is the schema file's base name, so
# the shared schema is copied under another base; the bytes are the shared
# file's. test/packet-write-cfloat/java/Main.java reads the shared cases.
# The recipes use $(JAVAC) and $(JAVA) from make/java.mk, expanded when they run.
build/packet-write-cfloat/java/.stamp: bin/schema test/packet-write-cfloat/CFloat.schema test/packet-write-cfloat/cases.txt make/java-packet-write-cfloat.mk
	@rm -rf build/packet-write-cfloat/java/gen build/packet-write-cfloat/java/source
	@mkdir -p build/packet-write-cfloat/java/source
	cp test/packet-write-cfloat/CFloat.schema build/packet-write-cfloat/java/source/PacketCFloat.schema
	./bin/schema generate --lang java --out build/packet-write-cfloat/java/gen build/packet-write-cfloat/java/source/PacketCFloat.schema
	@touch $@

.PHONY: packet-write-cfloat-java packet-write-cfloat-java-negative-control
packet-write-cfloat-java: build/packet-write-cfloat/java/.stamp
	@mkdir -p build/packet-write-cfloat/java/classes
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-write-cfloat/java/classes build/packet-write-cfloat/java/gen/*.java test/packet-write-cfloat/java/Main.java
	$(JAVA) -ea -cp build/packet-write-cfloat/java/classes Main test/packet-write-cfloat/cases.txt

packet-write-cfloat-java-negative-control: packet-write-cfloat-java
	@rm -rf build/packet-write-cfloat/java-negative
	@mkdir -p build/packet-write-cfloat/java-negative/classes build/packet-write-cfloat/java-negative/source
	go run ./tools/sabotage -name packet-write-cfloat-java-range \
		-out build/packet-write-cfloat/java-negative/functions.gotext internal/codegen/java/functions.go
	@printf '{"Replace":{"%s/internal/codegen/java/functions.go":"%s/build/packet-write-cfloat/java-negative/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-cfloat/java-negative/overlay.json
	go build -overlay=build/packet-write-cfloat/java-negative/overlay.json -o build/packet-write-cfloat/java-negative/schema ./cmd/schema
	cp test/packet-write-cfloat/CFloat.schema build/packet-write-cfloat/java-negative/source/PacketCFloat.schema
	./build/packet-write-cfloat/java-negative/schema generate --lang java --out build/packet-write-cfloat/java-negative/generated build/packet-write-cfloat/java-negative/source/PacketCFloat.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-write-cfloat/java-negative/classes build/packet-write-cfloat/java-negative/generated/*.java test/packet-write-cfloat/java/Main.java
	@if $(JAVA) -ea -cp build/packet-write-cfloat/java-negative/classes Main test/packet-write-cfloat/cases.txt > build/packet-write-cfloat/java-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: packet-write-cfloat-java passed without the range assert'; exit 1; fi
	@grep -Fq 'FAILED: above-max:' build/packet-write-cfloat/java-negative/log && \
		grep -Fq 'FAILED: below-min:' build/packet-write-cfloat/java-negative/log && \
		grep -Fq 'FAILED: largest-finite-float32:' build/packet-write-cfloat/java-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: Java did not red the off-range cases by name'; cat build/packet-write-cfloat/java-negative/log; exit 1; }
	@test "$$(grep -c '^FAILED: ' build/packet-write-cfloat/java-negative/log)" = 3 || \
		{ echo 'NEGATIVE CONTROL FAILED: a check other than the range went red'; cat build/packet-write-cfloat/java-negative/log; exit 1; }
	@echo 'packet-write-cfloat Java negative control: without the range assert, above-max, below-min and largest-finite-float32 are written'

test-java: packet-write-cfloat-java packet-write-cfloat-java-negative-control
