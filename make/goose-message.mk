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
# goose): the text align dropped from the writer. The gate it reds is
# TestGooseMessageFormCarriesEveryShape, whose Shapes root carries a string(N)
# whose length ends mid-byte, so the Go reference refuses the unaligned save. The conformance goldens cannot carry
# this control: since the batches were re-pinned over the message closure,
# every golden text (login_full, store_full, peer_a) already starts on a byte
# boundary, so the align writes no bit there and dropping it moves nothing.
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
			-run '^TestGooseMessageFormCarriesEveryShape$$' ./internal/codegen/goosetable/ > $(GOOSE_MESSAGE_NC)/log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the dropped text align left the Goose message-form gate green"; \
		cat $(GOOSE_MESSAGE_NC)/log; exit 1; \
	fi
	@grep -q -- '--- FAIL: TestGooseMessageFormCarriesEveryShape ' $(GOOSE_MESSAGE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the every-shape row did not go red"; cat $(GOOSE_MESSAGE_NC)/log; exit 1; }
	@grep -q 'the Go reference reads the batch as n=0 ok=false' $(GOOSE_MESSAGE_NC)/log || \
		{ echo "NEGATIVE CONTROL FAILED: the gate went red for another reason"; cat $(GOOSE_MESSAGE_NC)/log; exit 1; }
	@echo 'negative control: a dropped text align makes the Goose save of every shape a batch the Go reference refuses'

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
