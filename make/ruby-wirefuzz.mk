# make/ruby-wirefuzz.mk — Ruby's generated-code verdict pipe and planted controls
# (docs/PORT-RUBY.md; docs/plans/ruby-leg.md, R11).

.PHONY: tables-ruby-wire-fuzz tables-ruby-wire-fuzz-negative-control tables-ruby-wire-fuzz-retain-negative-control ruby-wirefuzz-retain-loss-negative-control ruby-wirefuzz-release-check ruby-wirefuzz-ci-list

RUBY_WIRE_FUZZ_DIR ?= build/ruby-wirefuzz
RUBY_WIRE_FUZZ_ROWS := $(foreach pin,current floor,$(foreach arm,plain mixed retain,ruby-wirefuzz-$(pin)-$(arm)))
RUBY_WIRE_FUZZ_CI_SHARDS := 0 1 2 3 4 5 6 7
RUBY_WIRE_FUZZ_CI_ROWS := $(foreach row,$(RUBY_WIRE_FUZZ_ROWS),$(foreach shard,$(RUBY_WIRE_FUZZ_CI_SHARDS),$(row)-shard-$(shard)))
.PHONY: $(RUBY_WIRE_FUZZ_ROWS) $(RUBY_WIRE_FUZZ_CI_ROWS)

ruby-wirefuzz-ci-list:
	@printf '%s\n' $(RUBY_WIRE_FUZZ_CI_ROWS)

define ruby_wirefuzz_row
ruby-wirefuzz-$(1)-$(2): build/conformance-harness build/conformance-ruby ruby-toolchain-check
	@set -eu; \
	if find build/conformance-ruby -name '*.refused' -print | grep .; then echo 'wire-fuzz: a corpus unit was refused'; exit 1; fi; \
	mkdir -p $(RUBY_WIRE_FUZZ_DIR); log=$(RUBY_WIRE_FUZZ_DIR)/$(1)-$(2).log; \
	sh test/conformance/wire-fuzz-complete "$$$$log" ./build/conformance-harness wire-fuzz $(if $(filter mixed,$(2)),--mixed-builder,$(if $(filter retain,$(2)),--retain,)) \
	  --driver '$(if $(filter current,$(1)),$(RUBY),$(RUBY_FLOOR)) --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby' \
	  --seed $(SEED) --n $(N) --failed $(RUBY_WIRE_FUZZ_DIR)/$(1)-$(2).bin
endef
$(foreach pin,current floor,$(foreach arm,plain mixed retain,$(eval $(call ruby_wirefuzz_row,$(pin),$(arm)))))

define ruby_wirefuzz_ci_shard
ruby-wirefuzz-$(1)-$(2)-shard-$(3): build/conformance-harness build/conformance-ruby ruby-toolchain-check
	@set -eu; \
	if find build/conformance-ruby -name '*.refused' -print | grep .; then echo 'wire-fuzz: a corpus unit was refused'; exit 1; fi; \
	mkdir -p $(RUBY_WIRE_FUZZ_DIR); log=$(RUBY_WIRE_FUZZ_DIR)/$(1)-$(2)-shard-$(3).log; \
	sh test/conformance/wire-fuzz-complete "$$$$log" ./build/conformance-harness wire-fuzz $(if $(filter mixed,$(2)),--mixed-builder,$(if $(filter retain,$(2)),--retain,)) \
	  --driver '$(if $(filter current,$(1)),$(RUBY),$(RUBY_FLOOR)) --disable-gems -w test/conformance/ruby/wirefuzz.rb build/conformance-ruby' \
	  --seed $(SEED) --n $(N) --shard $(3) --shards $(words $(RUBY_WIRE_FUZZ_CI_SHARDS)) --failed $(RUBY_WIRE_FUZZ_DIR)/$(1)-$(2)-shard-$(3).bin; \
	grep -q ', shard $(3)/$(words $(RUBY_WIRE_FUZZ_CI_SHARDS))' "$$$$log" || { echo 'wire-fuzz: wrong or missing shard identity'; exit 1; }
endef
$(foreach pin,current floor,$(foreach arm,plain mixed retain,$(foreach shard,$(RUBY_WIRE_FUZZ_CI_SHARDS),$(eval $(call ruby_wirefuzz_ci_shard,$(pin),$(arm),$(shard))))))

tables-ruby-wire-fuzz: $(RUBY_WIRE_FUZZ_ROWS)

tables-ruby-wire-fuzz-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run '^TestRubyWireFuzzVerdictPipeNegativeControl$$' ./internal/codegen/rubytable/

tables-ruby-wire-fuzz-retain-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run '^TestRubyWireFuzzRetainNegativeControl$$' ./internal/codegen/rubytable/

ruby-wirefuzz-retain-loss-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run '^TestRubyWireFuzzRetainLoss$$' ./internal/codegen/rubytable/

ruby-wirefuzz-release-check: build/conformance-ruby
	@if find build/conformance-ruby -name '*.refused' -print | grep .; then echo 'wire-fuzz: a corpus unit was refused'; exit 1; fi

test-ruby: $(RUBY_WIRE_FUZZ_CI_ROWS) tables-ruby-wire-fuzz-negative-control tables-ruby-wire-fuzz-retain-negative-control ruby-wirefuzz-retain-loss-negative-control
