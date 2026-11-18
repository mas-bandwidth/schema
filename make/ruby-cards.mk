# make/ruby-cards.mk — Ruby card facade and nine-surface shared corpus
# (docs/CARDS.md; docs/PORT-RUBY.md; docs/plans/ruby-leg.md, R13).

CARDS_RUBY_GENERATED := build/cards-ruby
CARDS_RUBY_SURFACES := load,save,render,parse,bounds,upgrade,edit,taskhash,view

.PHONY: cards-ruby-generate cards-ruby-conformance cards-ruby-test cards-ruby-render-negative-control cards-ruby-upgrade-negative-control cards-ruby-hash-negative-control cards-ruby-facade-negative-control ruby-ci-cards

cards-ruby-generate: $(CARDS_RUBY_GENERATED)/.stamp
$(CARDS_RUBY_GENERATED)/.stamp: build/schema-ruby tables/cards/Card.schema tables/cards/schema.lock
	@rm -rf $(CARDS_RUBY_GENERATED) && mkdir -p $(CARDS_RUBY_GENERATED)
	./build/schema-ruby generate --lang ruby --out $(CARDS_RUBY_GENERATED) tables/cards
	@touch $@

cards-ruby-test: ruby-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyCards$$' ./internal/codegen/rubytable/

cards-ruby-conformance: toolchain-ruby build/conformance-harness cards-ruby-generate
	RUBY=$(RUBY) ./build/conformance-harness cards --work build/cards-ruby-current --corpus tables/cards/conformance --driver 'tables/cards/ruby/driver' --require $(CARDS_RUBY_SURFACES)
	RUBY=$(RUBY_FLOOR) ./build/conformance-harness cards --work build/cards-ruby-floor --corpus tables/cards/conformance --driver 'tables/cards/ruby/driver' --require $(CARDS_RUBY_SURFACES)

define cards-ruby-control
	@rm -rf build/cards-ruby-control-$(1) && mkdir -p build/cards-ruby-control-$(1)
	@sed '$(2)' tables/cards/ruby/facade.rb > build/cards-ruby-control-$(1)/facade.rb
	@cmp -s tables/cards/ruby/facade.rb build/cards-ruby-control-$(1)/facade.rb && { echo 'NEGATIVE CONTROL FAILED: $(1) sed changed nothing'; exit 1; } || true
	@for interp in "$(RUBY)" "$(RUBY_FLOOR)"; do \
		name=$$(basename $$(dirname $$(dirname "$$interp"))); log=build/cards-ruby-control-$(1)/$$name.log; \
		if CARDS_RUBY_FACADE=build/cards-ruby-control-$(1)/facade.rb RUBY="$$interp" ./build/conformance-harness cards --work build/cards-ruby-control-$(1)/$$name --corpus tables/cards/conformance --driver 'tables/cards/ruby/driver' --require $(CARDS_RUBY_SURFACES) > "$$log" 2>&1; then echo 'negative control $(1) left conformance green'; exit 1; fi; \
		grep -q 'cards $(1) FAIL' "$$log" || { cat "$$log"; echo 'negative control $(1) failed for another reason'; exit 1; }; \
		for surface in load save render parse bounds upgrade edit taskhash view; do if [ "$$surface" != '$(1)' ]; then grep -q "cards $$surface pass" "$$log" || { cat "$$log"; exit 1; }; fi; done; \
	done
	@echo 'cards Ruby $(1) negative control detected on both pins'
endef

cards-ruby-render-negative-control: toolchain-ruby build/conformance-harness cards-ruby-generate
	$(call cards-ruby-control,render,s/out.join/"broken"/)

cards-ruby-upgrade-negative-control: toolchain-ruby build/conformance-harness cards-ruby-generate
	$(call cards-ruby-control,upgrade,s/output = root.save_retain/output = "".b # root.save_retain/)

cards-ruby-hash-negative-control: toolchain-ruby build/conformance-harness cards-ruby-generate
	$(call cards-ruby-control,taskhash,s/Digest::SHA256.hexdigest(data)/"0" * 64/)

cards-ruby-facade-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyCardsFacadeNegativeControl$$' ./internal/codegen/rubytable/

ruby-ci-cards: cards-ruby-test cards-ruby-conformance cards-ruby-render-negative-control cards-ruby-upgrade-negative-control cards-ruby-hash-negative-control cards-ruby-facade-negative-control
test-ruby: ruby-ci-cards
