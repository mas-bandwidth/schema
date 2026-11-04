# make/python-packet.mk — the Python leg of the packet-surface gates
# (docs/PORT-PYTHON.md, THE LAYERS: P4). A dashed stem, so a fragment: it is
# included like any make file and never probed as a --lang port; make/python.mk
# is the leg, and its test-python picks these targets up below.
#
# The python target is behind the schema_leg_python build tag, so the gates
# generate with build/schema-python, the compiler built with the tag. Every
# gate's main runs under BOTH pinned interpreters, isolated (-I), with the
# generated package's directory as its first argument.
#
# THE NEGATIVE CONTROLS patch one rule out of a copy of the generated package
# (the vendored serialize.py the rule lives in, or the emitted module) with
# sed, refuse a sabotage that patched nothing, and require the gate to fail
# exactly the vectors its .expected list names.
PYTHON_SCHEMA := build/schema-python

$(PYTHON_SCHEMA): $(GO_SOURCES) $(wildcard internal/codegen/python/runtime/_schema/*.py) $(wildcard internal/codegen/python/runtime/_schema/serialize/*.py)
	@mkdir -p build
	go build -tags schema_leg_python -o $@ ./cmd/schema

# python_run runs a gate main under both pins: $(1) the main, $(2) its arguments.
define python_run
	$(PYTHON) -I -B $(1) $(2)
	$(PYTHON_FLOOR) -I -B $(1) $(2)
endef

# python_sabotage copies a generated package and patches one file of the
# copy: $(1) the source directory, $(2) the copy, $(3) the file inside it,
# $(4) the sed program. A patch that changes nothing is refused.
define python_sabotage
	@rm -rf $(2) && mkdir -p $(dir $(2)) && cp -R $(1) $(2)
	@sed -e '$(4)' $(1)/$(3) > $(2)/$(3)
	@if cmp -s $(1)/$(3) $(2)/$(3); then echo 'NEGATIVE CONTROL FAILED: the sabotage of $(3) patched nothing'; exit 1; fi
endef

# python_expected runs a sabotaged gate and requires it to fail exactly the
# vectors of $(3): $(1) the gate's run, $(2) the control's directory.
define python_expected
	@if $(PYTHON) -I -B $(1) > $(2)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: $(2): the sabotaged gate stayed green'; exit 1; fi
	@sed -n 's|^FAILED: \([^:]*\):.*|\1|p' $(2)/log | LC_ALL=C sort > $(2)/failed
	@diff -u $(3) $(2)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: $(2): the sabotage did not fail exactly its vectors'; cat $(2)/log; exit 1; }
	@echo "negative control $(2): the sabotage fails exactly $$(wc -l < $(2)/failed | tr -d ' ') vectors"
endef

# PACKET FINITE (SPEC §4.3): every float slot refuses NaN and the infinities
# on write (InvalidValue) and on read on their raw bits (non_finite_float),
# and carries -0, the subnormals and the largest finite values bit exact. The
# rule lives in serialize.py, so each control patches the vendored copy: the
# read control drops the reader's two raw-bits checks, the write control the
# writer's two, and each must fail exactly its direction's vectors.
build/packet-finite/python/.stamp: $(PYTHON_SCHEMA) test/packet-finite/python/Finite.schema
	@rm -rf build/packet-finite/python && mkdir -p build/packet-finite/python
	./$(PYTHON_SCHEMA) generate --lang python --out build/packet-finite/python/generated test/packet-finite/python/Finite.schema
	@touch $@

PYTHON_FINITE_CORE := packetfinite/_schema/serialize/_core.py

.PHONY: packet-finite-python packet-finite-python-negative-control
packet-finite-python: python-toolchain-check build/packet-finite/python/.stamp
	$(call python_run,test/packet-finite/python/main.py,build/packet-finite/python/generated)

packet-finite-python-negative-control: packet-finite-python
	$(call python_sabotage,build/packet-finite/python/generated,build/packet-finite/python-negative-read/generated,$(PYTHON_FINITE_CORE),s/if (bits & 0x7F800000) == 0x7F800000:/if False:/;s/if (bits & 0x7FF0000000000000) == 0x7FF0000000000000:/if False:/)
	$(call python_expected,test/packet-finite/python/main.py build/packet-finite/python-negative-read/generated,build/packet-finite/python-negative-read,test/packet-finite/python/read.expected)
	$(call python_sabotage,build/packet-finite/python/generated,build/packet-finite/python-negative-write/generated,$(PYTHON_FINITE_CORE),s/    if not float_bits_are_finite(bits):/    if False:/;s/    if not double_bits_are_finite(bits):/    if False:/)
	$(call python_expected,test/packet-finite/python/main.py build/packet-finite/python-negative-write/generated,build/packet-finite/python-negative-write,test/packet-finite/python/write.expected)

test-python: packet-finite-python packet-finite-python-negative-control

# PACKET DEFAULTS (test/packet-defaults/README.md): constructors at the
# declared string, bytes and flags defaults, explicit-empty and short
# literals, the 33-bit and 64-bit masks, the defaultless twin, counted arrays
# born at their minimum, fresh defaults for every union selection, and the
# eight C++ byte and bit pins written and read back. The control drops the
# constructor's string default from a copy of the emitted module and must fail
# the constructor-values marker.
build/packet-defaults/python/.stamp: $(PYTHON_SCHEMA) test/packet-defaults/Defaults.schema test/packet-defaults/Plain.schema
	@rm -rf build/packet-defaults/python && mkdir -p build/packet-defaults/python
	./$(PYTHON_SCHEMA) generate --lang python --out build/packet-defaults/python/generated test/packet-defaults/Defaults.schema
	./$(PYTHON_SCHEMA) generate --lang python --out build/packet-defaults/python/generated test/packet-defaults/Plain.schema
	@touch $@

PYTHON_DEFAULTS_RUN = test/packet-defaults/python/main.py build/packet-defaults/python/generated testdata/wire/packet-defaults

.PHONY: packet-defaults-python packet-defaults-python-negative-control
packet-defaults-python: python-toolchain-check build/packet-defaults/python/.stamp
	$(call python_run,$(PYTHON_DEFAULTS_RUN))

packet-defaults-python-negative-control: packet-defaults-python
	$(call python_sabotage,build/packet-defaults/python/generated,build/packet-defaults/python-negative/generated,packetdefaults/Defaults.py,s/self.name: str = "é𐀀"/self.name: str = ""/)
	@if $(PYTHON) -I -B test/packet-defaults/python/main.py build/packet-defaults/python-negative/generated testdata/wire/packet-defaults > build/packet-defaults/python-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the Python constructor default disappeared and the gate stayed green'; exit 1; fi
	@grep -Fxq 'FAILED: packet-default constructor values' build/packet-defaults/python-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the Python defaults gate failed for another reason'; cat build/packet-defaults/python-negative/log; exit 1; }
	@echo 'packet defaults python negative control: a dropped constructor default fails the constructor check'

test-python: packet-defaults-python packet-defaults-python-negative-control

# PACKET TEXT (test/packet-text/README.md): the applicable string corpus rows
# and every single-bit mutation of each accepted row against the C++ oracle,
# through test/packet-text/python/driver. The control decodes text as Latin-1
# in a copy of the vendored serialize.py (the UTF-8 check bypassed, the zero
# byte rule kept) and must fail mutation agreement.
build/packet-text/python/.stamp: $(PYTHON_SCHEMA) test/packet-text/Narrow.schema
	@rm -rf build/packet-text/python && mkdir -p build/packet-text/python
	./$(PYTHON_SCHEMA) generate --lang python --out build/packet-text/python/generated test/packet-text/Narrow.schema
	@touch $@

PYTHON_TEXT_RUN = test/packet-text/python/driver test/packet-text/python/main.py

.PHONY: packet-utf8-python packet-utf8-python-negative-control
packet-utf8-python: python-toolchain-check build/packet-text/python/.stamp build/packet-text/cpp/driver build/packet-text/harness
	./build/packet-text/harness $(PYTHON_TEXT_RUN) build/packet-text/python/generated
	PYTHON=$(PYTHON_FLOOR) ./build/packet-text/harness $(PYTHON_TEXT_RUN) build/packet-text/python/generated

packet-utf8-python-negative-control: packet-utf8-python
	$(call python_sabotage,build/packet-text/python/generated,build/packet-text/python-negative/generated,packettext/_schema/serialize/_core.py,s/text = payload.decode("utf-8")/text = payload.decode("latin-1")/)
	@if ./build/packet-text/harness -mutations-only $(PYTHON_TEXT_RUN) build/packet-text/python-negative/generated > build/packet-text/python-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Python UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/python-negative/log || { cat build/packet-text/python-negative/log; exit 1; }
	@echo 'packet UTF-8 python negative control: the unchecked read fails bit-flip agreement'

# PACKET WIDE (test/packet-wide/README.md): the composition contracts, then
# the wstring corpus rows, the SPEC §4.12 interop cases and their single-bit
# mutations against the C++ oracle. The control drops the surrogate pairing
# check from a copy of the emitted runtime and must fail mutation agreement.
build/packet-wide/python/.stamp: $(PYTHON_SCHEMA) build/packet-wide/source/WideText.schema test/packet-wide/Shapes.schema
	@rm -rf build/packet-wide/python && mkdir -p build/packet-wide/python
	./$(PYTHON_SCHEMA) generate --lang python --out build/packet-wide/python/generated build/packet-wide/source/WideText.schema
	./$(PYTHON_SCHEMA) generate --lang python --out build/packet-wide/python/generated test/packet-wide/Shapes.schema
	@touch $@

PYTHON_WIDE_RUN = test/packet-wide/python/driver test/packet-wide/python/main.py
PYTHON_WIDE_HARNESS = ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver

.PHONY: packet-wide-python packet-wide-python-negative-control
packet-wide-python: python-toolchain-check build/packet-wide/python/.stamp build/packet-wide/cpp/driver build/packet-text/harness
	$(call python_run,test/packet-wide/python/contracts.py,build/packet-wide/python/generated)
	$(PYTHON_WIDE_HARNESS) $(PYTHON_WIDE_RUN) build/packet-wide/python/generated
	PYTHON=$(PYTHON_FLOOR) $(PYTHON_WIDE_HARNESS) $(PYTHON_WIDE_RUN) build/packet-wide/python/generated

packet-wide-python-negative-control: packet-wide-python
	$(call python_sabotage,build/packet-wide/python/generated,build/packet-wide/python-negative/generated,wide/_schema/__init__.py,s/return data.decode("utf-16-le")/return data.decode("utf-16-le", "surrogatepass")/)
	@if $(PYTHON_WIDE_HARNESS) -mutations-only $(PYTHON_WIDE_RUN) build/packet-wide/python-negative/generated > build/packet-wide/python-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: Python wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/python-negative/log || { cat build/packet-wide/python-negative/log; exit 1; }
	@echo 'packet wide python negative control: removed pairing fails bit-flip agreement'

test-python: packet-utf8-python packet-utf8-python-negative-control packet-wide-python packet-wide-python-negative-control

# THE CODE SHAPE (docs/PORT-PYTHON.md, D1): the emitted straight-line codecs
# against a descriptor walk over the same serialize.py calls and guards, on
# representative types of examples/, in microseconds per write and per read
# under both pins. A measurement with the clock, so a functional run and never
# part of make test; the page records its numbers.
.PHONY: bench-python-shape
bench-python-shape: python-toolchain-check $(PYTHON_SCHEMA)
	@rm -rf build/bench-python && mkdir -p build/bench-python
	./$(PYTHON_SCHEMA) generate --lang python --out build/bench-python/generated examples
	$(call python_run,bench/python/shape.py,build/bench-python/generated)
