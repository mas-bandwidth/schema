# THE PACKET WRITE-REFUSAL SURFACE, Java leg (docs/SPEC.md §5.1; the Go leg is
# TestWriteRefusalPacket in test/conformance/harness): the generated packet type
# carries one field per row, and the driver changes ONE field of the default
# value per case in test/packet-write-refusal/cases.txt. Java's write checks are
# debug asserts, so the driver runs under -ea and reads an AssertionError as the
# refusal; the lawful twin at the same boundary must be written. The control
# moves the emitter's upper-bound comparison and must red exactly the six
# at-the-maximum twins by name while every refused row stays green.
build/packet-write-refusal/java/.stamp: bin/schema test/packet-write-refusal/RefusalPacket.schema make/java-packet-write-refusal.mk
	@rm -rf build/packet-write-refusal/java/gen
	./bin/schema generate --lang java --out build/packet-write-refusal/java/gen test/packet-write-refusal/RefusalPacket.schema
	@touch $@

.PHONY: packet-write-refusal-java packet-write-refusal-java-negative-control
packet-write-refusal-java: build/packet-write-refusal/java/.stamp
	@mkdir -p build/packet-write-refusal/java/classes
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-write-refusal/java/classes build/packet-write-refusal/java/gen/*.java test/packet-write-refusal/java/Main.java
	$(JAVA) -ea -cp build/packet-write-refusal/java/classes Main test/packet-write-refusal/cases.txt

packet-write-refusal-java-negative-control: packet-write-refusal-java
	@rm -rf build/packet-write-refusal/java-negative && mkdir -p build/packet-write-refusal/java-negative
	go run ./tools/sabotage -name packet-write-refusal-java-upper -out build/packet-write-refusal/java-negative/functions.gotext internal/codegen/java/functions.go
	@printf '{"Replace":{"%s/internal/codegen/java/functions.go":"%s/build/packet-write-refusal/java-negative/functions.gotext"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > build/packet-write-refusal/java-negative/overlay.json
	go build -overlay=build/packet-write-refusal/java-negative/overlay.json -o build/packet-write-refusal/java-negative/schema ./cmd/schema
	./build/packet-write-refusal/java-negative/schema generate --lang java --out build/packet-write-refusal/java-negative/gen test/packet-write-refusal/RefusalPacket.schema
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/packet-write-refusal/java-negative/classes build/packet-write-refusal/java-negative/gen/*.java test/packet-write-refusal/java/Main.java
	@if $(JAVA) -ea -cp build/packet-write-refusal/java-negative/classes Main test/packet-write-refusal/cases.txt > build/packet-write-refusal/java-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the upper-bound comparison moved and the Java packet surface stayed green'; exit 1; fi
	@sed -n 's/^FAILED: write-refusal-packet \([a-z_]*\): .*/\1/p' build/packet-write-refusal/java-negative/log | LC_ALL=C sort > build/packet-write-refusal/java-negative/failed
	@printf '%s\n' bits_at_max enum_at_max scalar_at_max text_at_max union_tag_at_max > build/packet-write-refusal/java-negative/expected
	@diff -u build/packet-write-refusal/java-negative/expected build/packet-write-refusal/java-negative/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the moved upper bound did not red exactly the at-maximum twins'; cat build/packet-write-refusal/java-negative/log; exit 1; }
	@grep -m1 '^FAILED: write-refusal-packet scalar_at_max' build/packet-write-refusal/java-negative/log
	@echo 'packet write-refusal Java negative control: a moved upper bound reds the at-maximum twins by name'

test-java: packet-write-refusal-java packet-write-refusal-java-negative-control
