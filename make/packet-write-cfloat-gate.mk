# PACKET WRITE COMPRESSED FLOAT: ONE SHARED CORPUS (2026-11-24, #4909).
# test/packet-write-cfloat/cases.txt names every case once. Every leg's
# driver, test/packet-write-cfloat/<leg>/, reads it and answers each case on a
# line of its own ("case <name> ok" or "case <name> FAILED: <why>");
# tools/casegate runs the driver and refuses a case with no answer, a FAILED
# answer, and an owed line (test/packet-write-cfloat/owed.txt) for a case the
# leg answers ok. The case list comes from the case file, never from the
# driver, so a driver that skips a case is red naming the case.
#
# packet_write_cfloat_gate: $(1) the leg, $(2) the run's name. Paths are
# absolute: the Go and Luau recipes run from another directory.
packet_write_cfloat_gate = $(CURDIR)/$(CASEGATE) -name $(2) -leg $(1) -cases $(CURDIR)/test/packet-write-cfloat/cases.txt -owed $(CURDIR)/test/packet-write-cfloat/owed.txt --
