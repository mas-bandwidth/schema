# make/ruby-packet.mk — the Ruby packet wire gates and negative controls
# (docs/PORT-RUBY.md; docs/plans/ruby-leg.md, R4).

build/packet-text/cpp/driver: | serialize-siblings

build/packet-text/ruby/.stamp: build/schema-ruby test/packet-text/Narrow.schema
	@rm -rf build/packet-text/ruby && mkdir -p build/packet-text/ruby
	./build/schema-ruby generate --lang ruby --out build/packet-text/ruby/generated test/packet-text/Narrow.schema
	@touch $@

.PHONY: packet-utf8-ruby packet-utf8-ruby-negative-control
packet-utf8-ruby: ruby-toolchain-check serialize-siblings build/packet-text/ruby/.stamp build/packet-text/cpp/driver build/packet-text/harness
	./build/packet-text/harness $(RUBY) --disable-gems -w test/packet-text/ruby/main.rb $(CURDIR)/build/packet-text/ruby/generated
	./build/packet-text/harness $(RUBY_FLOOR) --disable-gems -w test/packet-text/ruby/main.rb $(CURDIR)/build/packet-text/ruby/generated

packet-utf8-ruby-negative-control: packet-utf8-ruby
	@rm -rf build/packet-text/ruby-negative && mkdir -p build/packet-text/ruby-negative
	cp -R build/packet-text/ruby/generated build/packet-text/ruby-negative/generated
	sed -i.orig 's/return refuse(INVALID_UTF8) unless text.valid_encoding?/return refuse(INVALID_UTF8) if false/' build/packet-text/ruby-negative/generated/packettext/serialize/core.rb
	@if cmp -s build/packet-text/ruby-negative/generated/packettext/serialize/core.rb build/packet-text/ruby-negative/generated/packettext/serialize/core.rb.orig; then \
		echo 'NEGATIVE CONTROL FAILED: the UTF-8 check was not found to plant over'; exit 1; fi
	@set -e; for interp in "$(RUBY)" "$(RUBY_FLOOR)"; do \
		if ./build/packet-text/harness -mutations-only "$$interp" --disable-gems -w test/packet-text/ruby/main.rb $(CURDIR)/build/packet-text/ruby-negative/generated \
			> build/packet-text/ruby-negative/log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: Ruby UTF-8 removal passed under $$interp"; exit 1; fi; \
		grep -Fq 'FAILED: packet-text verdict on ' build/packet-text/ruby-negative/log || \
			{ cat build/packet-text/ruby-negative/log; exit 1; }; \
	done
	@echo 'packet UTF-8 Ruby negative control: removed read validation fails bit-flip agreement on both pins'

.PHONY: packet-finite-ruby packet-finite-ruby-negative-control
packet-finite-ruby: build/schema-ruby ruby-toolchain-check
	@rm -rf build/packet-finite/ruby && mkdir -p build/packet-finite/ruby
	./build/schema-ruby generate --lang ruby --out build/packet-finite/ruby/generated test/packet-finite/ruby/Finite.schema
	$(RUBY) --disable-gems -w test/packet-finite/ruby/main.rb build/packet-finite/ruby/generated
	$(RUBY_FLOOR) --disable-gems -w test/packet-finite/ruby/main.rb build/packet-finite/ruby/generated

packet-finite-ruby-negative-control: ruby-toolchain-check
	sh test/slowgate/proof packet-finite-ruby-negative-control 'TestRubyPacketFiniteNegativeControl' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketFiniteNegativeControl$$' ./internal/codegen/ruby/

.PHONY: packet-void-ruby packet-void-ruby-negative-control
packet-void-ruby: build/schema-ruby ruby-toolchain-check
	@rm -rf build/packet-void/ruby && mkdir -p build/packet-void/ruby
	./build/schema-ruby generate --lang ruby --out build/packet-void/ruby/generated test/packet-void/ruby/Ruby.schema
	$(RUBY) --disable-gems -w test/packet-void/ruby/main.rb build/packet-void/ruby/generated
	$(RUBY_FLOOR) --disable-gems -w test/packet-void/ruby/main.rb build/packet-void/ruby/generated

packet-void-ruby-negative-control: ruby-toolchain-check
	sh test/slowgate/proof packet-void-ruby-negative-control 'TestRubyPacketVoidNegativeControl' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketVoidNegativeControl$$' ./internal/codegen/ruby/

.PHONY: packet-defaults-ruby packet-defaults-ruby-negative-control
packet-defaults-ruby: ruby-toolchain-check
	sh test/slowgate/proof packet-defaults-ruby 'TestRubyPacketDefaults' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketDefaults$$' ./internal/codegen/ruby/

packet-defaults-ruby-negative-control: ruby-toolchain-check
	sh test/slowgate/proof packet-defaults-ruby-negative-control 'TestRubyPacketDefaultsNegativeControl' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketDefaultsNegativeControl$$' ./internal/codegen/ruby/

.PHONY: ruby-packet-close ruby-packet-close-negative-control ruby-packet-analyzer ruby-packet-analyzer-negative-control
ruby-packet-close: ruby-toolchain-check
	sh test/slowgate/proof ruby-packet-close 'TestRubyPacketClose' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketClose$$' ./internal/codegen/ruby/

ruby-packet-close-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-packet-close-negative-control 'TestRubyPacketCloseNegativeControl' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketCloseNegativeControl$$' ./internal/codegen/ruby/

ruby-packet-analyzer: ruby-toolchain-check
	sh test/slowgate/proof ruby-packet-analyzer 'TestRubyPacketCloseAnalyzer' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketCloseAnalyzer$$' ./internal/codegen/ruby/

ruby-packet-analyzer-negative-control: ruby-toolchain-check
	sh test/slowgate/proof ruby-packet-analyzer-negative-control 'TestRubyPacketCloseAnalyzerNegativeControl' -count=1 -timeout 600s -tags schema_leg_ruby -run '^TestRubyPacketCloseAnalyzerNegativeControl$$' ./internal/codegen/ruby/

.PHONY: bench-ruby-gate bench-ruby-negative-control
bench-ruby-gate: generated/bench/ruby/.stamp ruby-toolchain-check
	RUBY=$(RUBY) bench/ruby/run --gate
	RUBY=$(RUBY_FLOOR) bench/ruby/run --gate

bench-ruby-negative-control: generated/bench/ruby/.stamp ruby-toolchain-check
	RUBY=$(RUBY) bench/ruby/negative-control
	RUBY=$(RUBY_FLOOR) bench/ruby/negative-control
