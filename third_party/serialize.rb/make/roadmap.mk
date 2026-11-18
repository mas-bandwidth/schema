# Regenerates ROADMAP.md from docs/roadmap.sexp with test/roadmap.rb (standard
# library only, offline; the sexp is read as data and never evaluated). Never
# edit ROADMAP.md by hand: test/test_docs.rb holds it to what this writes.

.PHONY: roadmap
roadmap:
	$(call need,RUBY_FLOOR)
	$(ISOLATED) $(RUBY_FLOOR) -w test/roadmap.rb --file docs/roadmap.sexp --out ROADMAP.md
