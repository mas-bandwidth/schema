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

# THE DRIVER'S CASE OUTCOMES (review F-086, F-088, F-089; the fixture and its
# answers are test/conformance/case-outcomes). On both pins, over the real
# generated tree: an absent codec is an EMPTY <case>.absent, a present codec's
# refusal is the surface's refusal answer, a driver error names its case and
# reds the surface after the rest have run. Over an EMPTY tree, the same rows
# all answer absent and no surface aborts, the must-refuse rows among them.
.PHONY: conformance-ruby-case-outcomes conformance-ruby-empty-tree
conformance-ruby-case-outcomes: ruby-toolchain-check build/conformance-ruby
	@mkdir -p build/conformance-ruby-empty-gen
	@test -z "$$(ls -A build/conformance-ruby-empty-gen)" || { echo 'build/conformance-ruby-empty-gen is not empty'; exit 1; }
	@for ruby in $(RUBY) $(RUBY_FLOOR); do \
		RUBY=$$ruby sh test/conformance/case-outcomes/check.sh test/conformance/ruby/driver \
			test/conformance/case-outcomes/manifest.txt test/conformance/case-outcomes/expected.txt \
			build/conformance-ruby-case-outcomes || exit 1; \
		RUBY=$$ruby RUBY_CONFORMANCE_GENERATED=build/conformance-ruby-empty-gen sh test/conformance/case-outcomes/check.sh \
			test/conformance/ruby/driver test/conformance/case-outcomes/manifest.txt \
			test/conformance/case-outcomes/expected-empty.txt build/conformance-ruby-case-outcomes-empty || exit 1; \
	done

# THE WHOLE CORPUS OVER AN EMPTY TREE (review F-086): every surface, every
# row, through the harness, with no Ruby codec at all. The run is green (a
# missing codec is absence, never a failure or a pass) and every answer the
# driver leaves is an EMPTY <case>.absent.
conformance-ruby-empty-tree: ruby-toolchain-check build/conformance-harness build/conformance-cpp build/schema_test_cook
	@mkdir -p build/conformance-ruby-empty-gen
	@test -z "$$(ls -A build/conformance-ruby-empty-gen)" || { echo 'build/conformance-ruby-empty-gen is not empty'; exit 1; }
	@RUBY=$(RUBY) RUBY_CONFORMANCE_GENERATED=build/conformance-ruby-empty-gen \
		./build/conformance-harness run --only ruby --work build/conformance-ruby-empty > build/conformance-ruby-empty.log 2>&1 || \
		{ echo 'conformance-ruby-empty-tree: the run over an empty tree is not green'; tail -40 build/conformance-ruby-empty.log; exit 1; }
	@answers=$$(find build/conformance-ruby-empty/out/ruby -type f | wc -l | tr -d ' '); \
	stray=$$(find build/conformance-ruby-empty/out/ruby -type f \( ! -name '*.absent' -o -size +0 \) | head -5); \
	test "$$answers" -gt 0 || { echo 'conformance-ruby-empty-tree: the driver answered nothing'; exit 1; }; \
	test -z "$$stray" || { echo "conformance-ruby-empty-tree: answers beside a missing codec that are not an EMPTY .absent: $$stray"; exit 1; }; \
	echo "conformance-ruby-empty-tree: $$answers cases, every one an EMPTY .absent, run green"

test-ruby: conformance-ruby-case-outcomes conformance-ruby-empty-tree
