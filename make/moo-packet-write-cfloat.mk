# Packet compressed-float writes in moo (SPEC §5.1 row 10): a NaN, an infinity
# or a finite value off [min, max] is refused before quantizing, and a lawful
# endpoint or interior value writes and reads back to its quantized value.
# The recipes use $(MOO_PACKET_DRIVER) and moo-packet-sabotaged-driver from
# make/moo-packet.mk, expanded when they run.
MOO_PACKET_CFLOAT_RUN = -analyze -schema test/packet-write-cfloat/CFloat.schema -input cases=test/packet-write-cfloat/cases.txt -main test/packet-write-cfloat/moo/main.moo

.PHONY: packet-write-cfloat-moo
packet-write-cfloat-moo: toolchain-moo moo-packet-driver
	$(MOO_PACKET_DRIVER) $(MOO_PACKET_CFLOAT_RUN)

.PHONY: packet-write-cfloat-moo-negative-control
packet-write-cfloat-moo-negative-control: packet-write-cfloat-moo
	$(call moo-packet-sabotaged-driver,build/packet-write-cfloat/moo-negative,packet-write-cfloat-moo-range,wire.go)
	@if ./build/packet-write-cfloat/moo-negative/driver $(MOO_PACKET_CFLOAT_RUN) > build/packet-write-cfloat/moo-negative/log 2>&1; then echo 'NEGATIVE CONTROL FAILED: range check removed'; exit 1; fi
	@grep -Fq 'FAILED: above-max: write verdict' build/packet-write-cfloat/moo-negative/log || { cat build/packet-write-cfloat/moo-negative/log; exit 1; }
	@echo 'packet cfloat moo negative control: removed range check fails'

test-moo: packet-write-cfloat-moo packet-write-cfloat-moo-negative-control
