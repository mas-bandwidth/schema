
# THE PACKET WRITE TEXT LEG (docs/SPEC.md §5.1 row 8,
# docs/PACKET-WIRE-ALGORITHM.md §2.4): a string(N) the writer accepts holds no
# zero byte and well-formed UTF-8 in its used bytes. Java's write contracts are
# debug asserts, so the driver runs under -ea. The control removes the write
# content contract and must red the seven ill-formed cases by name.
build/packet-write-text/java/.stamp: bin/schema test/packet-write-text/java/PacketText.schema make/java-packet-write-text.mk
	@rm -rf build/packet-write-text/java/gen
	./bin/schema generate --lang java --out build/packet-write-text/java/gen test/packet-write-text/java/PacketText.schema
	@touch $@

.PHONY: packet-write-text-java packet-write-text-java-negative-control
packet-write-text-java: build/packet-write-text/java/.stamp
	@mkdir -p build/packet-write-text/java/classes
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-write-text/java/classes build/packet-write-text/java/gen/*.java test/packet-write-text/java/Main.java
	$(JAVA) -ea -cp build/packet-write-text/java/classes Main test/packet-write-text/cases.txt

packet-write-text-java-negative-control: packet-write-text-java
	@mkdir -p build/packet-write-text/java-negative
	go run ./tools/sabotage -name packet-write-text-java-check -out build/packet-write-text/java-negative/functions.gotext internal/codegen/java/functions.go
	@printf '{"Replace":{"%s/internal/codegen/java/functions.go":"%s/build/packet-write-text/java-negative/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-text/java-negative/overlay.json
	go build -overlay=build/packet-write-text/java-negative/overlay.json -o build/packet-write-text/java-negative/schema ./cmd/schema
	@rm -rf build/packet-write-text/java-negative/gen
	./build/packet-write-text/java-negative/schema generate --lang java --out build/packet-write-text/java-negative/gen test/packet-write-text/java/PacketText.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-write-text/java-negative/classes build/packet-write-text/java-negative/gen/*.java test/packet-write-text/java/Main.java
	@if $(JAVA) -ea -cp build/packet-write-text/java-negative/classes Main test/packet-write-text/cases.txt > build/packet-write-text/java-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a Java packet writer with no text contract accepted an ill-formed string'; exit 1; fi
	@test "$$(grep -c '^FAILED: .* write accepted$$' build/packet-write-text/java-negative/log)" = 7 && \
		test "$$(grep -c '^FAILED: ' build/packet-write-text/java-negative/log)" = 7 || \
		{ echo 'NEGATIVE CONTROL FAILED: Java did not red exactly the seven ill-formed writes'; cat build/packet-write-text/java-negative/log; exit 1; }
	@echo 'packet write text Java negative control: a removed write text contract accepts the seven ill-formed strings'

test-java: packet-write-text-java packet-write-text-java-negative-control
