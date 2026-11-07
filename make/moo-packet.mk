# make/moo-packet.mk — the moo leg of every packet-surface gate
# (docs/PORT-MOO.md, THE LAYERS, layer 2). A dashed stem, so a fragment: it is
# included like any make file and never probed as a --lang port; make/moo.mk is
# the leg, and its test-moo picks these targets up below.
#
# THE DRIVER. The moo CLI has no io, so a moo program reads no file and no
# stdin. test/packet-text/moo/driver generates each -schema with
# internal/codegen/moo into one fresh directory, puts the gate's moo main
# beside the modules, hands the inputs in as the module inputs.moo (files, a
# directory of goldens, or stdin as one buffer), runs the pinned moo there and
# relays stdout, stderr and the verdict, so the packet-text harness speaks to
# it as to any other driver. -analyze first requires moo-analyze to be silent
# over the generated modules and the main (no error, no lint warning). The
# driver calls the emitter directly, as internal/codegen/moo's own gates do:
# the compiler's moo target stays behind the schema_leg_moo tag until the port
# joins the builtins.
#
# THE NEGATIVE CONTROLS build the same driver with `go build -overlay` over a
# copy of one internal/codegen/moo file that tools/sabotage broke (one named
# edit, matched exactly once), so the modules it generates are the sabotaged
# emitter's. Each sabotaged corpus is still analyzer-silent, and each control
# must go red for exactly its reason.
#
# packet-void stays C and C++ only (make/checks/packet-void.mk), as it is for
# every other port, so moo carries no leg of it.
MOO_PACKET_DRIVER := build/moo-packet/driver

.PHONY: moo-packet-driver
moo-packet-driver:
	@mkdir -p build/moo-packet
	go build -o $(MOO_PACKET_DRIVER) ./test/packet-text/moo/driver

# moo-packet-sabotaged-driver: $(1) the control's build directory, $(2) the
# tools/sabotage edit, $(3) the internal/codegen/moo file it breaks. Leaves
# $(1)/driver, built over the sabotaged copy.
define moo-packet-sabotaged-driver
	@mkdir -p $(1)
	go run ./tools/sabotage -name $(2) -out $(1)/$(3).txt internal/codegen/moo/$(3)
	@printf '{"Replace":{"%s/internal/codegen/moo/$(3)":"%s/$(1)/$(3).txt"}}\n' "$(CURDIR)" "$(CURDIR)" > $(1)/overlay.json
	go build -overlay=$(1)/overlay.json -o $(1)/driver ./test/packet-text/moo/driver
endef

# PACKET DEFAULTS (test/packet-defaults/README.md): constructors, in-place
# Init and the distinct Zero form, explicit-empty and short literals, the
# unsigned 33/64-bit masks, the defaultless twin, counted backing storage,
# reused prefixes and repeated union selection, and the eight C++ byte and bit
# pins read back at their consumed bits. The control drops only the
# constructor's byte copy and must fail the constructor-bytes marker.
MOO_PACKET_DEFAULTS_RUN = -analyze -schema test/packet-defaults/Defaults.schema -schema test/packet-defaults/Plain.schema \
	-inputs testdata/wire/packet-defaults -main test/packet-defaults/moo/main.moo

.PHONY: packet-defaults-moo packet-defaults-moo-negative-control
packet-defaults-moo: toolchain-moo moo-packet-driver packet-defaults-cpp
	$(MOO_PACKET_DRIVER) $(MOO_PACKET_DEFAULTS_RUN)

packet-defaults-moo-negative-control: packet-defaults-moo
	$(call moo-packet-sabotaged-driver,build/packet-defaults/moo-negative,packet-defaults-moo-constructor-bytes,moo.go)
	@if ./build/packet-defaults/moo-negative/driver $(MOO_PACKET_DEFAULTS_RUN) > build/packet-defaults/moo-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: moo constructor bytes disappeared and the runtime check stayed green'; exit 1; fi
	@grep -Fxq 'FAILED: packet-default constructor bytes' build/packet-defaults/moo-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: moo failed for another reason'; cat build/packet-defaults/moo-negative/log; exit 1; }
	@echo 'packet defaults moo negative control: missing constructor bytes fail the runtime check'

# PACKET FINITE (SPEC §4.3): every float slot (bare float32 and float64, a
# fixed float32 element, a counted float64 element, the compressed float32)
# refuses NaN and the infinities on write (an unlatched false) and on read
# (non_finite_float), and carries -0 and the subnormals bit exact. Two
# controls, one per direction, each required to fail exactly the vectors in
# its .expected list beside the main; the other direction stays green.
MOO_PACKET_FINITE_RUN = -analyze -schema test/packet-finite/moo/Finite.schema -main test/packet-finite/moo/main.moo

define packet-finite-moo-control
	$(call moo-packet-sabotaged-driver,build/packet-finite/moo-negative-$(1),packet-finite-moo-$(1),wire.go)
	@if ./build/packet-finite/moo-negative-$(1)/driver $(MOO_PACKET_FINITE_RUN) > build/packet-finite/moo-negative-$(1)/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the moo $(1) finite guard was removed and the finite checker stayed green'; exit 1; fi
	@sed -n 's|^FAILED: \([^:]*\):.*|\1|p' build/packet-finite/moo-negative-$(1)/log | LC_ALL=C sort > build/packet-finite/moo-negative-$(1)/failed
	@diff -u test/packet-finite/moo/$(1).expected build/packet-finite/moo-negative-$(1)/failed || \
		{ echo 'NEGATIVE CONTROL FAILED: the moo $(1) sabotage did not fail exactly its vectors'; cat build/packet-finite/moo-negative-$(1)/log; exit 1; }
	@echo "packet finite moo negative control ($(1)): the removed guard fails exactly $$(wc -l < build/packet-finite/moo-negative-$(1)/failed | tr -d ' ') vectors"
endef

.PHONY: packet-finite-moo packet-finite-moo-negative-control
packet-finite-moo: toolchain-moo moo-packet-driver
	$(MOO_PACKET_DRIVER) $(MOO_PACKET_FINITE_RUN)

packet-finite-moo-negative-control: packet-finite-moo
	$(call packet-finite-moo-control,read)
	$(call packet-finite-moo-control,write)

# PACKET TEXT (test/packet-text/README.md): the 33 applicable string corpus
# rows and every single-bit mutation of each accepted row against the C++
# oracle. The control reads the text raw at the emitter (the runtime's UTF-8
# check bypassed, the zero-byte rule kept) and must fail mutation agreement.
MOO_PACKET_TEXT_RUN = -stdin -schema test/packet-text/Narrow.schema -main test/packet-text/moo/main.moo

.PHONY: packet-utf8-moo packet-utf8-moo-negative-control
packet-utf8-moo: toolchain-moo moo-packet-driver build/packet-text/cpp/driver build/packet-text/harness
	./build/packet-text/harness $(MOO_PACKET_DRIVER) -analyze $(MOO_PACKET_TEXT_RUN)

packet-utf8-moo-negative-control: packet-utf8-moo
	$(call moo-packet-sabotaged-driver,build/packet-text/moo-negative,packet-utf8-moo-read,wire.go)
	@if ./build/packet-text/harness -mutations-only ./build/packet-text/moo-negative/driver -analyze $(MOO_PACKET_TEXT_RUN) > build/packet-text/moo-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: moo UTF-8 removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/moo-negative/log || { cat build/packet-text/moo-negative/log; exit 1; }
	@echo 'packet UTF-8 moo negative control: the unchecked read fails bit-flip agreement'

# PACKET WIDE (test/packet-wide/README.md): the composition contracts, then
# the 26 wstring corpus rows, the six SPEC §4.12 interop cases and their 1,016
# single-bit mutations against the C++ oracle. The control drops the surrogate
# pairing check and must fail mutation agreement.
MOO_PACKET_WIDE_SCHEMA = -schema build/packet-wide/source/WideText.schema
MOO_PACKET_WIDE_HARNESS = ./build/packet-text/harness -wide -corpus testdata/conformance/text/wstring.txt -oracle build/packet-wide/cpp/driver

.PHONY: packet-wide-moo packet-wide-moo-negative-control
packet-wide-moo: toolchain-moo moo-packet-driver build/packet-wide/source/WideText.schema build/packet-wide/cpp/driver build/packet-text/harness
	$(MOO_PACKET_DRIVER) -analyze $(MOO_PACKET_WIDE_SCHEMA) -schema test/packet-wide/Shapes.schema -main test/packet-wide/moo/contracts.moo
	$(MOO_PACKET_WIDE_HARNESS) $(MOO_PACKET_DRIVER) -analyze -stdin $(MOO_PACKET_WIDE_SCHEMA) -main test/packet-wide/moo/main.moo

packet-wide-moo-negative-control: packet-wide-moo
	$(call moo-packet-sabotaged-driver,build/packet-wide/moo-negative,packet-wide-moo-pairing,moo.go)
	@if $(MOO_PACKET_WIDE_HARNESS) -mutations-only ./build/packet-wide/moo-negative/driver -analyze -stdin $(MOO_PACKET_WIDE_SCHEMA) -main test/packet-wide/moo/main.moo > build/packet-wide/moo-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: moo wide pairing removal passed'; exit 1; fi
	@grep -Fq 'FAILED: packet-text verdict on ' build/packet-wide/moo-negative/log || { cat build/packet-wide/moo-negative/log; exit 1; }
	@echo 'packet wide moo negative control: removed pairing fails bit-flip agreement'

# Every gate and its control ride test-moo (make/moo.mk), which make test
# reaches through TEST_LEGS.
test-moo: packet-defaults-moo packet-defaults-moo-negative-control \
	packet-finite-moo packet-finite-moo-negative-control \
	packet-utf8-moo packet-utf8-moo-negative-control \
	packet-wide-moo packet-wide-moo-negative-control
