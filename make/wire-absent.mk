# make/wire-absent.mk — THE ABSENT-ROOT LEDGERS of the wire-fuzz roster
# (docs/SPEC-TABLES.md §4.2, docs/SPEC-CONFORMANCE.md W-ROSTER).
#
# `harness wire-fuzz` fails any seed whose root the leg has no codec for. A
# leg and arm that may leave roots absent hold them in
# test/conformance/wire-absent/<leg>-<arm>.tsv, one `<unit>.<Root>[@message]
# <TAB><why>` line per root, matched as an exact set. $(call WIRE_ABSENT,<leg>-<arm>)
# passes the ledger when the file exists and nothing when it does not, so a
# leg whose last root comes off deletes its file and runs with no exception.
WIRE_ABSENT_DIR := test/conformance/wire-absent
WIRE_ABSENT = $(if $(wildcard $(WIRE_ABSENT_DIR)/$(1).tsv),--absent-ledger $(WIRE_ABSENT_DIR)/$(1).tsv)
