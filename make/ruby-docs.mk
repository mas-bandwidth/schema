# docs/USAGE.md's Ruby fragments (docs/PORTING.md I12).

.PHONY: tables-ruby-usage tables-ruby-usage-negative-control
tables-ruby-usage: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run '^TestRubyUsageFragmentsRun$$' ./internal/ci/

tables-ruby-usage-negative-control: ruby-toolchain-check
	SCHEMA_SLOW=1 $(GOTESTRUN) -count=1 -timeout 600s -run '^TestRubyUsageFragmentsRunNegativeControl$$' ./internal/ci/

test-ruby: tables-ruby-usage tables-ruby-usage-negative-control
