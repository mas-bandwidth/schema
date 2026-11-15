# A FIELD ID TWICE IN ONE MESSAGE BODY IS DAMAGE (docs/SPEC-TABLES.md §3.3),
# in a body the reader names exactly as in one it skips. Each pinned body
# repeats a declared field's reference: one moved by a reference rewrite, one
# by a single flipped bit. The gate replays both against the engine, which
# refuses them; the C read must refuse them too.
C_MESSAGE_FIELD_TWICE := test/c-tables/wire-vectors

.PHONY: tables-c-message-field-twice
tables-c-message-field-twice: build/conformance-harness build/wire-fuzz-c
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-c --replay $(C_MESSAGE_FIELD_TWICE)/message_field_twice_reference.bin --unit backenddemo --root MatchResult --message
	./build/conformance-harness wire-fuzz --driver ./build/wire-fuzz-c --replay $(C_MESSAGE_FIELD_TWICE)/message_field_twice_bit.bin --unit backenddemo --root StorePurchase --message

test-c: tables-c-message-field-twice
