# docs/USAGE.md's Ruby fragments (docs/PORTING.md I12).

.PHONY: tables-ruby-usage tables-ruby-usage-negative-control
tables-ruby-usage: ruby-toolchain-check

tables-ruby-usage-negative-control: ruby-toolchain-check

test-ruby: tables-ruby-usage tables-ruby-usage-negative-control
