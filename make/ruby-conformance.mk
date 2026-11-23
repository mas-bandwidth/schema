# make/ruby-conformance.mk — the Ruby leg's conformance targets and controls
# (docs/PORT-RUBY.md; docs/plans/ruby-leg.md, R10).

.PHONY: conformance-ruby conformance-negative-control-ruby conformance-negative-control-ruby-walk

conformance-ruby: ruby-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-ruby
	RUBY=$(RUBY) ./build/conformance-harness run --only ruby --work build/conformance-ruby-current
	RUBY=$(RUBY_FLOOR) ./build/conformance-harness run --only ruby --work build/conformance-ruby-floor

conformance-negative-control-ruby: ruby-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-ruby
	@rm -rf build/conformance-ruby-nc && mkdir -p build/conformance-ruby-nc
	@cp -R build/conformance-ruby build/conformance-ruby-nc/gen
	@patched=0; for f in $$(find build/conformance-ruby-nc/gen -name load.rb); do \
		sed -i.orig 's/val = @data.getbyte(@pos)/val = 0/' "$$f"; \
		cmp -s "$$f" "$$f.orig" || patched=$$((patched + 1)); \
		rm -f "$$f.orig"; \
	done; \
	test "$$patched" -gt 0 || { echo 'NEGATIVE CONTROL FAILED: load.rb anchor matched no unit'; exit 1; }
	@if RUBY=$(RUBY) RUBY_CONFORMANCE_GENERATED=build/conformance-ruby-nc/gen \
		./build/conformance-harness run --only ruby --work build/conformance-ruby-nc/work > build/conformance-ruby-nc/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: sabotaged Ruby first-use order check left conformance green'; exit 1; fi
	@grep -q 'ruby / wire:' build/conformance-ruby-nc/log || \
		{ echo 'NEGATIVE CONTROL FAILED: framing did not go red'; cat build/conformance-ruby-nc/log; exit 1; }
	@echo 'conformance-negative-control-ruby: sabotaged first-use order check caught on framing'

conformance-negative-control-ruby-walk: ruby-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook build/conformance-ruby
	@rm -rf build/conformance-ruby-nc-walk && mkdir -p build/conformance-ruby-nc-walk
	@cp -R build/conformance-ruby build/conformance-ruby-nc-walk/gen
	@patched=0; for f in $$(find build/conformance-ruby-nc-walk/gen -name json_scan.rb); do \
		sed -i.orig 's/JsonNumber.new(tok)/JsonNumber.new("0")/' "$$f"; \
		cmp -s "$$f" "$$f.orig" || patched=$$((patched + 1)); \
		rm -f "$$f.orig"; \
	done; \
	test "$$patched" -gt 0 || { echo 'NEGATIVE CONTROL FAILED: json_scan.rb bool anchor matched no unit'; exit 1; }
	@if RUBY=$(RUBY) RUBY_CONFORMANCE_GENERATED=build/conformance-ruby-nc-walk/gen \
		./build/conformance-harness run --only ruby --work build/conformance-ruby-nc-walk/work > build/conformance-ruby-nc-walk/log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: sabotaged Ruby JSON bool reader left conformance green'; exit 1; fi
	@grep -q 'ruby / json-read:' build/conformance-ruby-nc-walk/log || \
		{ echo 'NEGATIVE CONTROL FAILED: json-read did not go red'; cat build/conformance-ruby-nc-walk/log; exit 1; }
	@! grep -q 'ruby / wire:' build/conformance-ruby-nc-walk/log || \
		{ echo 'NEGATIVE CONTROL FAILED: wire also went red'; cat build/conformance-ruby-nc-walk/log; exit 1; }
	@echo 'conformance-negative-control-ruby-walk: sabotaged JSON bool reader caught on json-read while wire stayed green'

test-ruby: conformance-ruby conformance-negative-control-ruby conformance-negative-control-ruby-walk

# THE WRITE CONTRACT's controls on the Ruby leg (docs/SPEC.md 5.1): Ruby
# refuses in every build, so each removes one refusal from a COPY of the
# generated tree and requires its write-refusal surface red while the wire (or
# message) surface stays green.
.PHONY: conformance-negative-control-ruby-write-refusal conformance-negative-control-ruby-write-refusal-fixed conformance-negative-control-ruby-write-refusal-message


conformance-negative-control-ruby-write-refusal: ruby-toolchain-check build/conformance-harness build/conformance-ruby
	RUBY=$(RUBY) sh test/conformance/ruby/negative-control-write-refusal write-refusal

conformance-negative-control-ruby-write-refusal-fixed: ruby-toolchain-check build/conformance-harness build/conformance-ruby
	RUBY=$(RUBY) sh test/conformance/ruby/negative-control-write-refusal write-refusal-fixed

conformance-negative-control-ruby-write-refusal-message: ruby-toolchain-check build/conformance-harness build/conformance-ruby
	RUBY=$(RUBY) sh test/conformance/ruby/negative-control-write-refusal write-refusal-message

test-ruby: conformance-negative-control-ruby-write-refusal conformance-negative-control-ruby-write-refusal-fixed conformance-negative-control-ruby-write-refusal-message
