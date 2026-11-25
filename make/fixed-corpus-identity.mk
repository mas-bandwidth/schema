# make/fixed-corpus-identity.mk — THE SHARED FIXED CORPUS'S IDENTITY GATE.
#
# test/conformance/fixed-corpus-identity.py holds a consumer of the C++
# reference's fixed-form corpora (build/fixedform-corpus and
# build/fixedform-bench-corpus) to the bytes the producer wrote: `snapshot`
# records every file's digest, and `verify` refuses a consumer tree with a
# file missing, extra or changed. The nightly conformance workflow runs it
# around each leg's build and harness run; this target runs it under the one
# driver, by name (test/registry: harness-run.make-fixed-corpus-identity.*):
#
#   1. the corpora are made, then copied aside as the reference and snapshotted;
#   2. the checkout's own tree verifies against the snapshot (the copy is whole);
#   3. make rebuilds both corpus stamps, the consumer the checkout runs most,
#      and the tree verifies again: a rebuild that rewrote, dropped or added a
#      corpus file is red here, by the file's name.
#
# The script's own refusals (a changed, missing or extra file, a changed
# reference) are held by internal/ci's TestNightlyFixedCorpusMetadata.

FIXED_CORPUS_IDENTITY_DIR := build/fixed-corpus-identity

.PHONY: fixed-corpus-identity
fixed-corpus-identity: build/fixedform-corpus/.stamp build/fixedform-bench-corpus/.stamp test/conformance/fixed-corpus-identity.py
	@rm -rf $(FIXED_CORPUS_IDENTITY_DIR) && mkdir -p $(FIXED_CORPUS_IDENTITY_DIR)/reference
	cp -R build/fixedform-corpus build/fixedform-bench-corpus $(FIXED_CORPUS_IDENTITY_DIR)/reference/
	python3 -I test/conformance/fixed-corpus-identity.py snapshot $(FIXED_CORPUS_IDENTITY_DIR)/reference $(FIXED_CORPUS_IDENTITY_DIR)/receipt.json
	python3 -I test/conformance/fixed-corpus-identity.py verify $(FIXED_CORPUS_IDENTITY_DIR)/reference $(FIXED_CORPUS_IDENTITY_DIR)/receipt.json --consumer build --phase copy
	$(MAKE) --no-print-directory build/fixedform-corpus/.stamp build/fixedform-bench-corpus/.stamp
	python3 -I test/conformance/fixed-corpus-identity.py verify $(FIXED_CORPUS_IDENTITY_DIR)/reference $(FIXED_CORPUS_IDENTITY_DIR)/receipt.json --consumer build --phase rebuild
