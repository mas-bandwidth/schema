# PACKET BARE INTEGERS HOLD THEIR WIDTH (docs/SPEC.md §5.1 row 11): a bare
# intN/uintN below 64 bits lives in a 64-bit Dart int, so the writer asserts
# the value inside its declared width before the mask lands its low N bits.
# The shared cases run on the checked twin, where every refused case must
# fire the assert; the release twin trusts the caller by design, so it writes
# the same values and requires only the lawful ones to read back. A refused
# 64-bit case has no Dart int to stand for it: the driver names it as not
# representable, by case, and fails on any case neither run nor named.
build/packet-write-bareint/dart/.stamp: bin/schema test/packet-write-bareint/BareInt.schema make/dart-packet-write-bareint.mk
	./bin/schema generate --lang dart --out build/packet-write-bareint/dart test/packet-write-bareint/BareInt.schema
	@touch $@

.PHONY: packet-write-bareint-dart packet-write-bareint-dart-negative-control
packet-write-bareint-dart: build/packet-write-bareint/dart/.stamp
	$(DART) analyze build/packet-write-bareint/dart test/packet-write-bareint/dart
	$(DART) format --set-exit-if-changed --output=none build/packet-write-bareint/dart test/packet-write-bareint/dart
	$(DART) --enable-asserts test/packet-write-bareint/dart/main.dart test/packet-write-bareint/cases.txt
	$(DART) compile exe -o build/packet-write-bareint/dart/checker test/packet-write-bareint/dart/main.dart >/dev/null
	./build/packet-write-bareint/dart/checker test/packet-write-bareint/cases.txt

packet-write-bareint-dart-negative-control: packet-write-bareint-dart
	@rm -rf build/packet-write-bareint/dart-negative && mkdir -p build/packet-write-bareint/dart-negative
	go run ./tools/sabotage -name packet-write-bareint-dart-width -out build/packet-write-bareint/dart-negative/functions.gotext internal/codegen/dart/functions.go
	@printf '{"Replace":{"%s/internal/codegen/dart/functions.go":"%s/build/packet-write-bareint/dart-negative/functions.gotext"}}\n' "$(CURDIR)" "$(CURDIR)" > build/packet-write-bareint/dart-negative/overlay.json
	go run -overlay=build/packet-write-bareint/dart-negative/overlay.json ./cmd/schema generate --lang dart --out build/packet-write-bareint/dart-negative/generated test/packet-write-bareint/BareInt.schema
	@sed 's|../../../build/packet-write-bareint/dart/|$(CURDIR)/build/packet-write-bareint/dart-negative/generated/|' test/packet-write-bareint/dart/main.dart > build/packet-write-bareint/dart-negative/main.dart
	@if $(DART) --enable-asserts build/packet-write-bareint/dart-negative/main.dart test/packet-write-bareint/cases.txt > build/packet-write-bareint/dart-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the width check is removed and the driver stayed green'; \
		cat build/packet-write-bareint/dart-negative/log; exit 1; fi
	@grep -Fq 'FAILED: packet-write-bareint int8-above-max: out-of-width value was written' build/packet-write-bareint/dart-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: int8-above-max was not the failure'; cat build/packet-write-bareint/dart-negative/log; exit 1; }
	@grep -Fq 'FAILED: packet-write-bareint int32-above-max: out-of-width value was written' build/packet-write-bareint/dart-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: int32-above-max was not the failure'; cat build/packet-write-bareint/dart-negative/log; exit 1; }
	@grep -Fq 'FAILED: packet-write-bareint uint32-negative: out-of-width value was written' build/packet-write-bareint/dart-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: uint32-negative was not the failure'; cat build/packet-write-bareint/dart-negative/log; exit 1; }
	@if grep -F 'FAILED: ' build/packet-write-bareint/dart-negative/log | grep -Fvq ': out-of-width value was written'; then \
		echo 'NEGATIVE CONTROL FAILED: a case other than an out-of-width value went red'; \
		cat build/packet-write-bareint/dart-negative/log; exit 1; fi
	@grep -m1 'FAILED: packet-write-bareint int8-above-max: out-of-width value was written' build/packet-write-bareint/dart-negative/log
	@sed "/'uint64-negative':$$/{N;d;}" test/packet-write-bareint/dart/main.dart > build/packet-write-bareint/dart-negative/unnamed.dart
	@if $(DART) --enable-asserts build/packet-write-bareint/dart-negative/unnamed.dart test/packet-write-bareint/cases.txt > build/packet-write-bareint/dart-negative/unnamed.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a case neither run nor named not representable stayed green'; \
		cat build/packet-write-bareint/dart-negative/unnamed.log; exit 1; fi
	@grep -Fq 'FAILED: packet-write-bareint uint64-negative: neither run nor named not representable in Dart' build/packet-write-bareint/dart-negative/unnamed.log || \
		{ echo 'NEGATIVE CONTROL FAILED: uint64-negative was not the failure'; cat build/packet-write-bareint/dart-negative/unnamed.log; exit 1; }
	@echo 'negative control: with no width check, the out-of-width cases are written and nothing else goes red; an unnamed unrun case goes red by name'

test-dart: packet-write-bareint-dart packet-write-bareint-dart-negative-control
