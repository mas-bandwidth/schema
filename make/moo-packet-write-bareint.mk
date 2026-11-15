# PACKET WRITE BARE-INT (test/packet-write-bareint/cases.txt, SPEC §5.1 row
# 11): every case is written through the generated Write<Type>, plus a fixed
# array element, a counted array element and a union arm's payload
# (test/packet-write-bareint/moo/Carriers.schema). A refused case returns
# false and never raises; a written case reads back exactly; a fraction, a
# string and a boolean are refused at every width. The control drops the
# signed width check from writeInt (only the integer check kept) and must fail
# the out-of-width signed cases by name. The recipes use $(MOO_PACKET_DRIVER)
# and moo-packet-sabotaged-driver from make/moo-packet.mk, expanded when they
# run.
MOO_PACKET_BAREINT_RUN = -analyze -schema test/packet-write-bareint/BareInt.schema \
	-schema test/packet-write-bareint/moo/Carriers.schema \
	-input cases=test/packet-write-bareint/cases.txt -main test/packet-write-bareint/moo/main.moo

.PHONY: packet-write-bareint-moo packet-write-bareint-moo-negative-control
packet-write-bareint-moo: toolchain-moo moo-packet-driver
	$(MOO_PACKET_DRIVER) $(MOO_PACKET_BAREINT_RUN)

packet-write-bareint-moo-negative-control: packet-write-bareint-moo
	$(call moo-packet-sabotaged-driver,build/packet-write-bareint/moo-negative,packet-write-bareint-moo-signed-width,wire.go)
	@if ./build/packet-write-bareint/moo-negative/driver $(MOO_PACKET_BAREINT_RUN) > build/packet-write-bareint/moo-negative/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: the moo signed bare-integer width check disappeared and the gate stayed green'; \
		cat build/packet-write-bareint/moo-negative/log; exit 1; fi
	@grep -Fxq 'FAILED: packet-write-bareint int8-above-max refused' build/packet-write-bareint/moo-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the bare int8 above its width was not named'; cat build/packet-write-bareint/moo-negative/log; exit 1; }
	@grep -Fxq 'FAILED: packet-write-bareint fixed-element-int8-above-max refused' build/packet-write-bareint/moo-negative/log || \
		{ echo 'NEGATIVE CONTROL FAILED: the fixed-element int8 above its width was not named'; cat build/packet-write-bareint/moo-negative/log; exit 1; }
	@grep -F 'FAILED: ' build/packet-write-bareint/moo-negative/log
	@echo 'packet write bare-int moo negative control: the removed signed width check fails the out-of-width signed cases by name'

test-moo: packet-write-bareint-moo packet-write-bareint-moo-negative-control
