# make/goose-message.mk — THE GOOSE LEG's MESSAGE FORM (docs/SPEC-TABLES.md
# §3.3, form byte 2; docs/PORT-GOOSE.md, THE CARD MAP:
# schema-goose-message-form). Included by the Makefile's wildcard include.
#
# internal/codegen/goosetable/messageform_test.go holds the Goose message form
# to the tree's goldens and to the Go reference: every fixed-root `message` row
# of the conformance manifest (the announcement, the save, the measure, the
# load and the re-save byte for byte), every fixed root of the C++ reference's
# fixed corpus crossed with tablewire.DecodeMessages / EncodeMessages, every
# payload shape, and the batch surface's refusals by name. The fixed-corpus
# rows read build/fixedform-corpus, so SCHEMA_REQUIRE_CORPUS=1 turns a missing
# oracle into a failure.
.PHONY: tables-goose-message-form
tables-goose-message-form: build/fixedform-corpus/.stamp
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -timeout 600s -run '^TestGooseMessageForm' ./internal/codegen/goosetable/
	@echo 'tables Goose message form: the goldens, the Go reference and the refusals'

# THE MESSAGE FORM's NEGATIVE CONTROL (make/negative-controls.json, group
# goose): the text align dropped from the writer. The goldens' text rows
# (login_full, store_full, peer_a) cannot see it since the message closure's
# vocabulary was re-pinned (6add01c83): a 5-bit reference and a 6-bit length
# leave their text on a byte boundary already, so the align writes nothing
# there. The shapes gate's Picks[2].Word bytes sit off a byte boundary, so the
# control requires the golden and shapes gates together to go red on EXACTLY
# TestGooseMessageFormCarriesEveryShape, with the Go reference refusing the
# Goose-written batch as damage, and on no golden row.
GOOSE_MESSAGE_NC := build/goose-message-nc
.PHONY: tables-goose-message-form-negative-control
tables-goose-message-form-negative-control:
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@rm -rf $(GOOSE_MESSAGE_NC) && mkdir -p $(GOOSE_MESSAGE_NC)
	@sed '/THE TEXT ALIGN/d' internal/codegen/goosetable/messageform.go > $(GOOSE_MESSAGE_NC)/messageform.go.txt
	@! cmp -s internal/codegen/goosetable/messageform.go $(GOOSE_MESSAGE_NC)/messageform.go.txt || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not apply to messageform.go"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/goosetable/messageform.go":"%s/$(GOOSE_MESSAGE_NC)/messageform.go.txt"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(GOOSE_MESSAGE_NC)/overlay.json
	@if SCHEMA_SLOW=1 go test -count=1 -v -timeout 600s -overlay $(GOOSE_MESSAGE_NC)/overlay.json \
			-run '^TestGooseMessageForm(MatchesTheGoldens|CarriesEveryShape)$$' ./internal/codegen/goosetable/ > $(GOOSE_MESSAGE_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the dropped text align left the Goose message-form gate green"; \
		cat $(GOOSE_MESSAGE_NC)/log; exit 1; \
	fi
	@grep -E -- '--- FAIL: TestGooseMessageForm' $(GOOSE_MESSAGE_NC)/log | awk '{print $$3}' | sort > $(GOOSE_MESSAGE_NC)/failed
	@printf '%s\n' TestGooseMessageFormCarriesEveryShape > $(GOOSE_MESSAGE_NC)/expected
	@cmp -s $(GOOSE_MESSAGE_NC)/failed $(GOOSE_MESSAGE_NC)/expected || \
		{ echo "NEGATIVE CONTROL FAILED: the dropped align reddened other tests than the shapes gate:"; \
		  cat $(GOOSE_MESSAGE_NC)/failed; exit 1; }
	@grep -q 'the Go reference reads the batch as n=0 ok=false err=wire_damaged' $(GOOSE_MESSAGE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for another reason"; cat $(GOOSE_MESSAGE_NC)/log; exit 1; }
	@echo 'negative control: a dropped text align reds the shapes gate, its unaligned bytes refused by the Go reference'

# THE PEER READ's NEGATIVE CONTROL (make/negative-controls.json, group goose):
# §4's widening rule dropped from the runtime's msg_accept (the sed is checked
# to have matched), overlaid onto the embedded runtime/message.goose, so an
# announced kind below this reader's on the same ladder is a kind mismatch.
# The control requires the versioning gate (messageform_versioning_test.go,
# the Go reference over VOLD/VNEW in both directions) to go red on EXACTLY the
# five rows whose newer reader widens — array_elem_widen, bits_grow,
# float_widen, int_widen, uint_widen — and on no other row.
GOOSE_MESSAGE_PEER_NC := build/goose-message-peer-nc
.PHONY: tables-goose-message-peer-negative-control
tables-goose-message-peer-negative-control: build/fixedform-corpus/.stamp
	@test -x "$(GOOSE)" || $(MAKE) dist-goose
	@rm -rf $(GOOSE_MESSAGE_PEER_NC) && mkdir -p $(GOOSE_MESSAGE_PEER_NC)
	@sed 's/    if msg_widens(their_kind, their_elem, kind, elem) {/    if false \&\& msg_widens(their_kind, their_elem, kind, elem) { \/\/ SABOTAGED/' \
		internal/codegen/goosetable/runtime/message.goose > $(GOOSE_MESSAGE_PEER_NC)/message.goose
	@grep -q SABOTAGED $(GOOSE_MESSAGE_PEER_NC)/message.goose || \
		{ echo "NEGATIVE CONTROL FAILED: the sabotage did not apply to runtime/message.goose"; exit 1; }
	@printf '{"Replace":{"%s/internal/codegen/goosetable/runtime/message.goose":"%s/$(GOOSE_MESSAGE_PEER_NC)/message.goose"}}\n' \
		"$(CURDIR)" "$(CURDIR)" > $(GOOSE_MESSAGE_PEER_NC)/overlay.json
	@if SCHEMA_REQUIRE_CORPUS=1 go test -count=1 -v -timeout 600s -overlay $(GOOSE_MESSAGE_PEER_NC)/overlay.json \
			-run '^TestGooseMessageFormVersioning$$' ./internal/codegen/goosetable/ > $(GOOSE_MESSAGE_PEER_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the dropped widening rule left the Goose message versioning gate green"; \
		cat $(GOOSE_MESSAGE_PEER_NC)/log; exit 1; \
	fi
	@grep -E -- '--- FAIL: TestGooseMessageFormVersioning/' $(GOOSE_MESSAGE_PEER_NC)/log | awk '{print $$3}' | sort > $(GOOSE_MESSAGE_PEER_NC)/failed
	@printf '%s\n' TestGooseMessageFormVersioning/new_reads_old/array_elem_widen TestGooseMessageFormVersioning/new_reads_old/bits_grow \
		TestGooseMessageFormVersioning/new_reads_old/float_widen TestGooseMessageFormVersioning/new_reads_old/int_widen TestGooseMessageFormVersioning/new_reads_old/uint_widen > $(GOOSE_MESSAGE_PEER_NC)/expected
	@cmp -s $(GOOSE_MESSAGE_PEER_NC)/failed $(GOOSE_MESSAGE_PEER_NC)/expected || \
		{ echo "NEGATIVE CONTROL FAILED: the dropped widening rule reddened other rows than the five widening rows:"; \
		  cat $(GOOSE_MESSAGE_PEER_NC)/failed; exit 1; }
	@echo 'negative control: a dropped widening rule reds exactly the five widening rows of the Goose message versioning gate'
