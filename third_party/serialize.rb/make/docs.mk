# The documentation's checks, offline, under the floor. docs-check runs every
# ruby block of README.md and DESIGN.md against lib/ under ruby -w and resolves
# every relative link; its negative control plants a block that raises, a
# block that warns and a broken link in copies of README.md, and requires
# docs-check red by the name of each.

DOCS_CHECKED := README.md DESIGN.md

.PHONY: docs-check docs-check-negative-control
docs-check:
	$(call need,RUBY_FLOOR)
	$(ISOLATED) $(RUBY_FLOOR) -w test/docs_check.rb $(DOCS_CHECKED)

docs-check-negative-control:
	$(call need,RUBY_FLOOR)
	$(ISOLATED) $(RUBY_FLOOR) -w test/docs_check.rb --negative-control README.md
