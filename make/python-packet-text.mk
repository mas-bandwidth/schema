# make/python-packet-text.mk — the Python packet text kinds' gates
# (docs/PORT-PYTHON.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# python-packet-text-negative-control: the text kinds' own control. A copy of
# the generated Narrow reader and a copy of the runtime it carries are each
# planted with a fault, and TestPythonPacketTextNegativeControl must find the
# corpus verdict red by name. A Go test under the slow gate, as the packet
# core's control is; the probe runs beside the Go reference writer, which
# needs serialize.go (deps-go).
.PHONY: python-packet-text-negative-control packet-utf8-python packet-utf8-python-negative-control
test-python: deps-go packet-utf8-python python-packet-text-negative-control packet-utf8-python-negative-control
python-packet-text-negative-control: python-toolchain-check deps-go
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run TestPythonPacketTextNegativeControl ./internal/codegen/python/

# packet-utf8-python: the shared packet-text sweep (test/packet-text/README.md)
# against the C++ oracle: the 33 corpus rows and every bit flip of every
# accepted row, through test/packet-text/python/main.py on both pins.
# The C++ oracle compiles against the pinned serialize.h beside the checkout,
# so serialize-siblings stages it where no sibling is present; the order-only
# line holds the oracle build behind it under make -j.
build/packet-text/cpp/driver: | serialize-siblings

build/packet-text/python/.stamp: build/schema-python test/packet-text/Narrow.schema
	@rm -rf build/packet-text/python && mkdir -p build/packet-text/python
	./build/schema-python generate --lang python --out build/packet-text/python/generated test/packet-text/Narrow.schema
	@touch $@

packet-utf8-python: python-toolchain-check serialize-siblings build/packet-text/python/.stamp build/packet-text/cpp/driver build/packet-text/harness
	./build/packet-text/harness $(PYTHON) -W error -I -B test/packet-text/python/main.py $(CURDIR)/build/packet-text/python/generated
	./build/packet-text/harness $(PYTHON_FLOOR) -W error -I -B test/packet-text/python/main.py $(CURDIR)/build/packet-text/python/generated

# The sweep's control: the strict UTF-8 decode in a copy of the generated
# runtime is replaced by Latin-1, and the mutation-only sweep must fail its
# verdict marker; the corpus's refusal rows alone cannot satisfy it.
packet-utf8-python-negative-control: packet-utf8-python
	@rm -rf build/packet-text/python-negative && mkdir -p build/packet-text/python-negative
	cp -R build/packet-text/python/generated build/packet-text/python-negative/generated
	sed -i.orig 's/payload.decode("utf-8")/payload.decode("latin-1")/' build/packet-text/python-negative/generated/packettext/_schema/serialize/_core.py
	@if cmp -s build/packet-text/python-negative/generated/packettext/_schema/serialize/_core.py build/packet-text/python-negative/generated/packettext/_schema/serialize/_core.py.orig; then \
		echo 'NEGATIVE CONTROL FAILED: the UTF-8 decode was not found to plant over'; exit 1; fi
	@set -e; for python in "$(PYTHON)" "$(PYTHON_FLOOR)"; do \
		if ./build/packet-text/harness -mutations-only "$$python" -W error -I -B test/packet-text/python/main.py $(CURDIR)/build/packet-text/python-negative/generated \
			> build/packet-text/python-negative/log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: Python UTF-8 removal passed under $$python"; exit 1; fi; \
		grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/python-negative/log || \
			{ cat build/packet-text/python-negative/log; exit 1; }; \
	done
	@echo 'packet UTF-8 Python negative control: removed read validation fails bit-flip agreement on both pins'
