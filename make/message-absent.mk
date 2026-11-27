# make/message-absent.mk — THE DEAD-ROOT LEDGERS of the message-fuzz roster
# (docs/SPEC-CONFORMANCE.md MF-ROSTER).
#
# `harness message-fuzz` asks every leg about every root of ONE roster
# (messageFuzzTargets in test/conformance/harness/messagefuzz.go), and a root
# the leg answers 0 for is DEAD: no mutant of it runs. A dead root fails the
# run unless the leg holds test/conformance/message-absent/<leg>.tsv, one
# `<unit>.<Root><TAB><why>` line per root, matched as an exact set (a root
# the leg now answers is red until its line comes off). $(call
# MESSAGE_ABSENT,<leg>) passes the ledger when the file exists and nothing
# when it does not, so a leg whose last root comes off deletes its file.
MESSAGE_ABSENT_DIR := test/conformance/message-absent
MESSAGE_ABSENT = $(if $(wildcard $(MESSAGE_ABSENT_DIR)/$(1).tsv),--dead-root-ledger $(MESSAGE_ABSENT_DIR)/$(1).tsv)
