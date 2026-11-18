# make/ruby-serialize.mk — serialize.rb, the Ruby leg's bit layer
# (docs/PORT-RUBY.md). Included by the Makefile's wildcard include; the stem
# has a dash, so it is a support fragment and never probed as a --lang port.
#
# serialize.rb is the serialize family's Ruby member at format 2.0. It is
# staged in third_party/serialize.rb/, laid out as its own repository, until
# mas-bandwidth/serialize.rb is public and tagged; the runtime vendors it by
# sha256 (SERIALIZE_PIN). Its judge is the family corpus vendored inside it
# (VENDORED pins mas-bandwidth/serialize v2.1.1, commit bd3d67e23c8a): every
# vector, every sabotage control and the runner's own witnesses, under both
# pinned interpreters, plus RuboCop over the library and its tests.
RUBY_SERIALIZE_DIR  := third_party/serialize.rb
RUBY_SERIALIZE_PINS = RUBY_FLOOR=$(RUBY_FLOOR) RUBY=$(RUBY) RUBOCOP=$(RUBOCOP)

.PHONY: ruby-serialize-test ruby-serialize-test-floor ruby-serialize-test-current ruby-serialize-lint ruby-serialize-negative-control
ruby-serialize-test: ruby-serialize-test-floor ruby-serialize-test-current ruby-serialize-lint ruby-serialize-negative-control

ruby-serialize-test-floor: ruby-toolchain-check
	$(MAKE) --no-print-directory -C $(RUBY_SERIALIZE_DIR) test-floor $(RUBY_SERIALIZE_PINS)

ruby-serialize-test-current: ruby-toolchain-check
	$(MAKE) --no-print-directory -C $(RUBY_SERIALIZE_DIR) test-current $(RUBY_SERIALIZE_PINS)

ruby-serialize-lint: ruby-toolchain-check
	$(MAKE) --no-print-directory -C $(RUBY_SERIALIZE_DIR) lint $(RUBY_SERIALIZE_PINS)

# A corpus root with no vectors turns the corpus test red, by name. The
# per-rule sabotage controls run inside the test itself.
ruby-serialize-negative-control: ruby-toolchain-check
	$(MAKE) --no-print-directory -C $(RUBY_SERIALIZE_DIR) negative-control $(RUBY_SERIALIZE_PINS)

# VENDORING. The emitted runtime carries serialize.rb beside _schema.rb
# (internal/codegen/ruby/runtime/serialize.rb and runtime/serialize/core.rb),
# copied from the staged source and held to runtime/SERIALIZE_PIN, one sha256
# per file. internal/codegen/ruby's TestVendoredRubySerializeIsThePin holds the
# copy to the pin and to the staged source, so an edit to either that is not
# re-vendored goes red.
RUBY_SERIALIZE_SRC    := $(RUBY_SERIALIZE_DIR)/lib
RUBY_SERIALIZE_VENDOR := internal/codegen/ruby/runtime
RUBY_SERIALIZE_PIN    := internal/codegen/ruby/runtime/SERIALIZE_PIN

.PHONY: vendor-serialize-ruby
vendor-serialize-ruby:
	@rm -rf $(RUBY_SERIALIZE_VENDOR)/serialize.rb $(RUBY_SERIALIZE_VENDOR)/serialize
	@mkdir -p $(RUBY_SERIALIZE_VENDOR)/serialize
	@echo "# Vendored from $(RUBY_SERIALIZE_SRC), serialize.rb staged in this tree" > $(RUBY_SERIALIZE_PIN)
	@echo "# until mas-bandwidth/serialize.rb is public and tagged (docs/PORT-RUBY.md, THE BIT LAYER)." >> $(RUBY_SERIALIZE_PIN)
	@echo "# Rewrite with: make vendor-serialize-ruby (make/ruby-serialize.mk)" >> $(RUBY_SERIALIZE_PIN)
	@for rel in serialize.rb serialize/core.rb; do \
		f="$(RUBY_SERIALIZE_SRC)/$$rel"; \
		h=$$({ sha256sum "$$f" 2>/dev/null || shasum -a 256 "$$f"; } | cut -d" " -f1); \
		echo "sha256  $$h  $$rel" >> $(RUBY_SERIALIZE_PIN); \
		cp "$$f" "$(RUBY_SERIALIZE_VENDOR)/$$rel"; \
	done
	@echo "vendor-serialize-ruby: wrote $(RUBY_SERIALIZE_PIN) from $(RUBY_SERIALIZE_SRC)"
