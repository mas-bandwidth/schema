# make/ruby-compressed-negzero.mk — the Ruby leg's compressed -0 writer corpus
# (docs/SPEC-TABLES.md §3.3, docs/rulings.json fixed-compressed-negzero).
# Included by the Makefile's wildcard include.
#
# conformance-ruby-compressed-negzero  every row of
#                                      testdata/conformance/feature-witnesses/compressed-negzero-go
#                                      run through the generated Ruby unit on
#                                      both pins: a compressed float's -0 saves
#                                      to its +0 twin's bytes in the variable
#                                      and fixed forms, and in every other Ruby
#                                      writer (retain save, message form, JSON,
#                                      cook); the plain-float rows keep -0
#                                      bit-exact. internal/codegen/rubytable's
#                                      TestRubyCompressedFloatStoresAreGuarded
#                                      is its fast twin in the ordinary go test
#                                      run.
.PHONY: conformance-ruby-compressed-negzero
conformance-ruby-compressed-negzero: ruby-toolchain-check
	sh test/slowgate/proof conformance-ruby-compressed-negzero 'TestRubyCompressedNegZeroCorpus' \
		-count=1 -timeout 600s ./internal/codegen/rubytable/ -run '^TestRubyCompressedNegZeroCorpus$$'

test-ruby: conformance-ruby-compressed-negzero
