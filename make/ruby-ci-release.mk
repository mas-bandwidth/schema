# make/ruby-ci-release.mk — Ruby's CI readiness, tables bench gate and release tier
# (docs/PORT-RUBY.md; docs/plans/ruby-leg.md, R15; docs/PORTING.md, J3 and J5).

.PHONY: ruby-readiness ruby-readiness-negative-control
ruby-readiness: ruby-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestRubyReadiness$$' ./internal/codegen/rubytable/
ruby-readiness-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 go test -count=1 -timeout 600s -run '^TestRubyReadinessNegativeControl$$' ./internal/codegen/rubytable/
test-ruby: ruby-readiness ruby-readiness-negative-control

.PHONY: tables-ruby-bench-gate tables-ruby-bench-gate-negative-control
tables-ruby-bench-gate: bin/schema ruby-toolchain-check
	RUBY=$(RUBY) bench/tables/ruby/leg build
	RUBY=$(RUBY) bench/tables/ruby/leg run --gate
	RUBY=$(RUBY_FLOOR) bench/tables/ruby/leg run --gate

tables-ruby-bench-gate-negative-control: bin/schema ruby-toolchain-check
	@rm -rf build/ruby-bench-negative && mkdir -p build/ruby-bench-negative/golden build/ruby-bench-negative/roundtrip
	@cp testdata/wire/bench_table.bin bench/corpus/variants/bench_table.variants.bin build/ruby-bench-negative/golden/
	@cp testdata/wire/bench_table.bin bench/corpus/variants/bench_table.variants.bin build/ruby-bench-negative/roundtrip/
	@printf '\377' | dd of=build/ruby-bench-negative/golden/bench_table.bin bs=1 seek=100 conv=notrunc 2>/dev/null
	@size=$$(wc -c < bench/corpus/variants/bench_table.variants.bin); \
	printf '\377' | dd of=build/ruby-bench-negative/roundtrip/bench_table.variants.bin bs=1 seek=$$((size / 64 * 3 + 100)) conv=notrunc 2>/dev/null
	@RUBY=$(RUBY) RUBY_BENCH_GEN=build/ruby-bench-negative/gen bench/tables/ruby/leg build
	@for c in golden:WIRE\ GOLDEN\ MISMATCH roundtrip:FAILED:\ bench_table; do \
		name=$${c%%:*}; want=$${c#*:}; \
		if RUBY=$(RUBY) RUBY_BENCH_GEN=build/ruby-bench-negative/gen bench/tables/ruby/leg run --gate \
			--wire-dir build/ruby-bench-negative/$$name --variant-dir build/ruby-bench-negative/$$name \
			> build/ruby-bench-negative/$$name.log 2>&1; then \
			echo "NEGATIVE CONTROL FAILED: the $$name corpus passed the Ruby bench gate"; exit 1; fi; \
		grep -q "$$want" build/ruby-bench-negative/$$name.log || \
			{ echo "NEGATIVE CONTROL FAILED: the $$name corpus went red, but not on $$want"; cat build/ruby-bench-negative/$$name.log; exit 1; }; \
	done
	@echo "tables-ruby-bench-gate negative control: a flipped golden byte and a flipped variant byte each fail the gate by name"

test-ruby: tables-ruby-bench-gate tables-ruby-bench-gate-negative-control

build/conformance-harness-ruby: $(wildcard test/conformance/harness/*.go) $(GO_SOURCES)
	@mkdir -p build
	go build -tags schema_leg_ruby -o $@ ./test/conformance/harness

.PHONY: tables-ruby-release
tables-ruby-release: bin/schema ruby-toolchain-check build/conformance-harness build/conformance-ruby
	$(MAKE) tables-wire-fuzz-release-ruby
	$(MAKE) tables-ruby-wire-fuzz-negative-control
	$(MAKE) tables-ruby-bench-gate
	$(MAKE) tables-ruby-bench-gate-negative-control
	RUBY=$(RUBY) bench/tables/run.sh --only ruby --bare
	@echo "tables ruby release gate: the wire fuzz at release depth with its control, and the bench leg's gate, witnesses and measured pass"
