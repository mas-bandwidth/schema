# Python card facade and the nine-surface shared corpus.
CARDS_PYTHON_GENERATED := build/cards-python
CARDS_PYTHON_SURFACES := load,save,render,parse,bounds,upgrade,edit,taskhash,view
.PHONY: cards-python-generate cards-python-conformance cards-python-test cards-python-render-negative-control cards-python-upgrade-negative-control cards-python-hash-negative-control
cards-python-generate: $(CARDS_PYTHON_GENERATED)/.stamp
$(CARDS_PYTHON_GENERATED)/.stamp: bin/schema tables/cards/Card.schema tables/cards/schema.lock
	@mkdir -p $(CARDS_PYTHON_GENERATED)
	./bin/schema generate --lang python --out $(CARDS_PYTHON_GENERATED) tables/cards
	@touch $@

cards-python-test:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonCards' ./internal/codegen/pythontable/

cards-python-conformance: toolchain-python build/conformance-harness cards-python-generate
	PYTHON=$(PYTHON) ./build/conformance-harness cards --work build/cards-python-current --corpus tables/cards/conformance --driver 'tables/cards/python/driver' --require $(CARDS_PYTHON_SURFACES)
	PYTHON=$(PYTHON_FLOOR) ./build/conformance-harness cards --work build/cards-python-floor --corpus tables/cards/conformance --driver 'tables/cards/python/driver' --require $(CARDS_PYTHON_SURFACES)

define cards-python-control
	@mkdir -p build/cards-python-control-$(1)
	@sed '$(2)' tables/cards/python/facade.py > build/cards-python-control-$(1)/cards.py
	@for python in $(PYTHON) $(PYTHON_FLOOR); do \
		name=$$(basename "$$python"); log=build/cards-python-control-$(1)/$$name.log; \
		if CARDS_PYTHON_FACADE=build/cards-python-control-$(1)/cards.py PYTHON="$$python" ./build/conformance-harness cards --work build/cards-python-control-$(1)/$$name --corpus tables/cards/conformance --driver 'tables/cards/python/driver' --require $(CARDS_PYTHON_SURFACES) > "$$log" 2>&1; then echo 'negative control $(1) left conformance green'; exit 1; fi; \
		grep -q 'cards $(1) FAIL' "$$log" || { cat "$$log"; echo 'negative control $(1) failed for another reason'; exit 1; }; \
		for surface in load save render parse bounds upgrade edit taskhash view; do if [ "$$surface" != '$(1)' ]; then grep -q "cards $$surface pass" "$$log" || { cat "$$log"; exit 1; }; fi; done; \
	done
	@echo 'cards Python $(1) negative control detected on both pins'

endef
cards-python-render-negative-control: toolchain-python build/conformance-harness cards-python-generate
	$(call cards-python-control,render,s/return "".join(out)/return "broken"/)
cards-python-upgrade-negative-control: toolchain-python build/conformance-harness cards-python-generate
	$(call cards-python-control,upgrade,s/output = root.save_retain/output = bytes() # root.save_retain/)
cards-python-hash-negative-control: toolchain-python build/conformance-harness cards-python-generate
	$(call cards-python-control,taskhash,s/return hashlib.sha256(data).hexdigest()/return "0" * 64/)

PYTHON_CI_SUPPORT_GROUPS += python-ci-cards
python-ci-cards: cards-python-test cards-python-conformance cards-python-render-negative-control cards-python-upgrade-negative-control cards-python-hash-negative-control
test-python: cards-python-test cards-python-conformance cards-python-render-negative-control cards-python-upgrade-negative-control cards-python-hash-negative-control

.PHONY: cards-python-facade-negative-control
cards-python-facade-negative-control:
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_python -run '^TestPythonCardsFacadeNegativeControl$$' ./internal/codegen/pythontable/
python-ci-cards: cards-python-facade-negative-control
test-python: cards-python-facade-negative-control
