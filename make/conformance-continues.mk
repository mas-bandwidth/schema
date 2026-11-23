# make/conformance-continues.mk — EVERY CASE GETS AN ANSWER (review
# 2026-11-21; test/conformance/case-outcomes/continues). The Makefile's
# wildcard include pulls this in.
#
# Under `make -k -j4 conformance` the Java, Lobster, Moo and Squirrel drivers
# died partway through json-hostile, write-refusal and write-refusal-json, and
# every case behind the one that killed them went unanswered: a surface of
# 198 rows scored 0/198 for one row. Each gate runs the leg's REAL driver over
# the committed fixture, where every row that stopped a driver comes FIRST and
# a control sits behind it on the same surface, and holds every case to its
# line in expected.txt through the shared check.sh: a surface that exits
# non-zero, or leaves a case unanswered, fails and names the case. A refusal
# is a verdict and the driver moves on. Seconds: one runtime per surface.
CONTINUES_MANIFEST := test/conformance/case-outcomes/continues/manifest.txt
CONTINUES_EXPECTED := test/conformance/case-outcomes/continues/expected.txt

.PHONY: java-case-outcomes-continues lobster-case-outcomes-continues \
	moo-case-outcomes-continues squirrel-case-outcomes-continues

# Java answers the write-refusal surfaces from -ea (the driver passes it):
# its writer's contract checks are debug asserts.
java-case-outcomes-continues: build-conformance-java
	JAVA="$(JAVA)" sh test/conformance/case-outcomes/check.sh test/conformance/java/driver \
		$(CONTINUES_MANIFEST) $(CONTINUES_EXPECTED) build/java-case-outcomes-continues
test-java: java-case-outcomes-continues

lobster-case-outcomes-continues: lobster-dist-present build/conformance-lobster
	sh test/conformance/case-outcomes/check.sh test/conformance/lobster/driver \
		$(CONTINUES_MANIFEST) $(CONTINUES_EXPECTED) build/lobster-case-outcomes-continues
test-lobster: lobster-case-outcomes-continues

# MOO_DRIVER_STAGE is the gate's own, so it never shares a stage with a
# matrix run beside it under make -j.
moo-case-outcomes-continues: toolchain-moo build/conformance-moo
	MOO_DRIVER_STAGE=build/moo-case-outcomes-continues-stage sh test/conformance/case-outcomes/check.sh \
		test/conformance/moo/driver $(CONTINUES_MANIFEST) $(CONTINUES_EXPECTED) build/moo-case-outcomes-continues
test-moo: moo-case-outcomes-continues

squirrel-case-outcomes-continues: toolchain-squirrel build/conformance-squirrel
	sh test/conformance/case-outcomes/check.sh test/conformance/squirrel/driver \
		$(CONTINUES_MANIFEST) $(CONTINUES_EXPECTED) build/squirrel-case-outcomes-continues
test-squirrel: squirrel-case-outcomes-continues
