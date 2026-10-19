# make/schema-java-generated-errorprone-nullaway-gate.mk — the Java strict-checker
# lens (ideas roadmap, schema-per-language-lenses): Error Prone and NullAway as
# javac plugins over the generated Java for the table corpus. The corpus is the
# one `build/tables-generated-java/.stamp` (make/java.mk) already generates; this
# file adds no second corpus.
#
# Tool pin. Error Prone 2.36.0 and NullAway 0.12.3, from Maven Central, which is
# the projects' official distribution. Jars are cached under build/ and every
# download is checked against the sha256 pin below; a miss or a checksum mismatch
# is "tool missing", never a pass. The fleet install is the ansible play in
# rowan-tools/fleet; this cache is the build-local copy of the same official
# artifacts (the play is not reachable from the wall this card ran behind).
JAVA_CHECKER_EP_VERSION  := 2.36.0
JAVA_CHECKER_NA_VERSION  := 0.12.3
# make/java.mk pins a repo-local JDK; fall back to the PATH JDK when that
# unpacked tree is absent (CI and the bench install the same major).
ifeq ($(wildcard $(JAVAC)),)
JAVAC := javac
JAVA  := java
endif
JAVA_CHECKER_JARS        ?= $(CURDIR)/build/java-checker-jars
JAVA_CHECKER_CORPUS      ?= build/tables-generated-java

JAVA_CHECKER_ARTIFACTS := \
  com/google/errorprone/error_prone_core/2.36.0/error_prone_core-2.36.0.jar:8cc1b8c5ca4ab484e828a2a763a0bd8e7a43a29fd4f0233edd15bc4cfd07ae39 \
  com/google/googlejavaformat/google-java-format/1.19.1/google-java-format-1.19.1.jar:ff15abac529f6ecb97878aa2ae271a4d3b15f526c1f823757dd8781c0b11c73b \
  org/pcollections/pcollections/4.0.1/pcollections-4.0.1.jar:1f82766d7c3221930854033bebff5073ea46b43f27326074bbe15d148c18bfb3 \
  com/google/guava/guava/32.1.1-jre/guava-32.1.1-jre.jar:91fbba37f1c8b251cf9ea9e7d3a369eb79eb1e6a5df1d4bbf483dd0380740281 \
  com/google/guava/failureaccess/1.0.1/failureaccess-1.0.1.jar:a171ee4c734dd2da837e4b16be9df4661afab72a41adaf31eb84dfdaf936ca26 \
  com/google/guava/listenablefuture/9999.0-empty-to-avoid-conflict-with-guava/listenablefuture-9999.0-empty-to-avoid-conflict-with-guava.jar:b372a037d4230aa57fbeffdef30fd6123f9c0c2db85d0aced00c91b974f33f99 \
  com/google/auto/auto-common/1.2.2/auto-common-1.2.2.jar:f50b1ce8a41fad31a8a819c052f8ffa362ea0a3dbe9ef8f7c7dc9a36d4738a59 \
  io/github/eisop/dataflow-errorprone/3.41.0-eisop1/dataflow-errorprone-3.41.0-eisop1.jar:10434fba4e53f55fa9c76904cde414b918932548c9dfc4e2d634ac05ff7a7d10 \
  com/google/auto/value/auto-value-annotations/1.9/auto-value-annotations-1.9.jar:fa5469f4c44ee598a2d8f033ab0a9dcbc6498a0c5e0c998dfa0c2adf51358044 \
  com/google/protobuf/protobuf-java/3.25.5/protobuf-java-3.25.5.jar:8540247fad9e06baefa8fb45eb313802d019f485f14300e0f9d6b556ed88e753 \
  com/google/auto/service/auto-service-annotations/1.0.1/auto-service-annotations-1.0.1.jar:c7bec54b7b5588b5967e870341091c5691181d954cf2039f1bf0a6eeb837473b \
  org/jspecify/jspecify/1.0.0/jspecify-1.0.0.jar:1fad6e6be7557781e4d33729d49ae1cdc8fdda6fe477bb0cc68ce351eafdfbab \
  javax/inject/javax.inject/1/javax.inject-1.jar:91c77044a50c481636c32d916fd89c9118a72195390452c81065080f957de7ff \
  com/google/errorprone/error_prone_check_api/2.36.0/error_prone_check_api-2.36.0.jar:80dc394440aed46854120ba42be0ab505b940d9f32b77baa8fc5790636f8f8f6 \
  io/github/java-diff-utils/java-diff-utils/4.12/java-diff-utils-4.12.jar:9990a2039778f6b4cc94790141c2868864eacee0620c6c459451121a901cd5b5 \
  com/github/kevinstern/software-and-algorithms/1.0/software-and-algorithms-1.0.jar:61ab82439cef37343b14f53154c461619375373a56b9338e895709fb54e0864c \
  com/github/ben-manes/caffeine/caffeine/3.0.5/caffeine-3.0.5.jar:8a9b54d3506a3b92ee46b217bcee79196b21ca6d52dc2967c686a205fb2f9c15 \
  org/checkerframework/checker-qual/3.19.0/checker-qual-3.19.0.jar:a827c49183f3a632277d27a0a4673686cb341507447b9d570261094bd748aa68 \
  com/google/errorprone/error_prone_annotations/2.10.0/error_prone_annotations-2.10.0.jar:a249d4d25dfb86d41e6c82fc335df580189f0c9feeabdc53233fc1e5060724a1 \
  com/google/errorprone/error_prone_annotation/2.36.0/error_prone_annotation-2.36.0.jar:3702887ce0fa4d0b4cd7c5545138919b101002a883be861f2c6f30ca5562635f \
  com/google/errorprone/error_prone_type_annotations/2.36.0/error_prone_type_annotations-2.36.0.jar:dc15687f4afebb73ed263771ed42b15cd9cf4f9aee15f9c997afc2e8e009087d \
  com/uber/nullaway/nullaway/0.12.3/nullaway-0.12.3.jar:5c96ca96386d6341759985733c5025069033999f8b27f34881d4ecf2bd9827c2 \
  org/checkerframework/dataflow-nullaway/3.48.0/dataflow-nullaway-3.48.0.jar:8710f503cdc769543ee0fe0fd44c7913d89de62c6238807173346512de7f2e87

# The JDK module openings Error Prone's javac plugin needs on JDK 17+.
JAVA_CHECKER_JVM_ARGS := \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.api=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.file=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.main=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.model=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.parser=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.processing=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.tree=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.util=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.code=ALL-UNNAMED \
  -J--add-exports=jdk.compiler/com.sun.tools.javac.comp=ALL-UNNAMED \
  -J--add-opens=jdk.compiler/com.sun.tools.javac.code=ALL-UNNAMED \
  -J--add-opens=jdk.compiler/com.sun.tools.javac.comp=ALL-UNNAMED

# fetch the pinned official artifacts, verifying every sha256 pin
.PHONY: java-checker-jars
java-checker-jars:
	@mkdir -p $(JAVA_CHECKER_JARS)
	@set -e; for e in $(JAVA_CHECKER_ARTIFACTS); do \
		rel=$${e%%:*}; sha=$${e##*:}; f=$(JAVA_CHECKER_JARS)/$$(basename $$rel); \
		if [ -f $$f ] && echo "$$sha  $$f" | sha256sum -c - >/dev/null 2>&1; then continue; fi; \
		rm -f $$f.tmp; \
		curl -fsSL "https://repo1.maven.org/maven2/$$rel" -o $$f.tmp || { \
			echo "tool missing: error-prone $(JAVA_CHECKER_EP_VERSION) / nullaway $(JAVA_CHECKER_NA_VERSION)"; \
			echo "  no artifact at https://repo1.maven.org/maven2/$$rel"; exit 1; }; \
		echo "$$sha  $$f.tmp" | sha256sum -c - >/dev/null || { \
			echo "tool missing: error-prone $(JAVA_CHECKER_EP_VERSION) / nullaway $(JAVA_CHECKER_NA_VERSION)"; \
			echo "  sha256 mismatch for $$rel"; exit 1; }; \
		mv $$f.tmp $$f; \
	done
	@command -v $(JAVAC) >/dev/null 2>&1 || { \
		echo "tool missing: javac (the Error Prone/NullAway javac plugin needs a JDK)"; exit 1; }

# $(call java_checker_run,<sources glob>,<output dir>,<log>) compiles <sources>
# with Error Prone (every enabled check an error) and NullAway over every
# generated package, keeping the findings in <log>.
define java_checker_run
	@rm -rf $(2) && mkdir -p $(2)
	@CP=$$(ls $(JAVA_CHECKER_JARS)/*.jar | tr '\n' ':'); \
	PKGS=$$(grep -h '^package ' $(1) | sed 's/package //;s/;.*//' | sort -u | paste -sd, -); \
	$(JAVAC) $(JAVA_CHECKER_JVM_ARGS) -cp "$$CP" -processorpath "$$CP" \
		-XDcompilePolicy=simple -XDshould-stop.ifError=FLOW \
		-Xplugin:"ErrorProne -Werror -Xep:NullAway:ERROR -XepOpt:NullAway:AnnotatedPackages=$$PKGS" \
		-d $(2) $(1) > $(3) 2>&1
endef

# The whole table corpus the Java leg already generates, with warnings as errors.
# Heavy (about 2m30s on the bench), so it is the scheduled half of the workflow.
.PHONY: java-generated-errorprone-nullaway-gate
java-generated-errorprone-nullaway-gate: bin/schema build/tables-generated-java/.stamp java-checker-jars
	$(call java_checker_run,$(JAVA_CHECKER_CORPUS)/*/*.java,$(CURDIR)/build/java-checker-out,$(CURDIR)/build/java-checker.log)
	@cat $(CURDIR)/build/java-checker.log
	@! grep -q 'error:' $(CURDIR)/build/java-checker.log || { \
		echo "java Error Prone/NullAway gate: findings above are errors"; exit 1; }
	@echo "java Error Prone/NullAway gate: the generated corpus is clean"

# The fast half for push: the same plugins over the smallest three corpus units
# (about 27 s on the bench). It is still the real tool, not a stand-in.
.PHONY: java-generated-errorprone-nullaway-gate-fast
java-generated-errorprone-nullaway-gate-fast: bin/schema build/tables-generated-java/.stamp java-checker-jars
	$(call java_checker_run,$(JAVA_CHECKER_CORPUS)/examples/*.java $(JAVA_CHECKER_CORPUS)/pointers/*.java $(JAVA_CHECKER_CORPUS)/blobs/*.java,$(CURDIR)/build/java-checker-fast-out,$(CURDIR)/build/java-checker-fast.log)
	@cat $(CURDIR)/build/java-checker-fast.log
	@! grep -q 'error:' $(CURDIR)/build/java-checker-fast.log || { \
		echo "java Error Prone/NullAway gate (fast): findings above are errors"; exit 1; }
	@echo "java Error Prone/NullAway gate (fast): the three fast units are clean"

# THE PLANTED-BAD-PATTERN CONTROL. A clean file that must be GREEN and a planted
# null dereference that must be RED on the NullAway line, both through the same
# javac invocation the gate uses. The plant is never in generated output.
.PHONY: java-generated-errorprone-nullaway-gate-negative-control
java-generated-errorprone-nullaway-gate-negative-control: java-checker-jars
	@rm -rf $(CURDIR)/build/java-checker-plant && mkdir -p $(CURDIR)/build/java-checker-plant/src/checkerplant
	@printf 'package checkerplant;\npublic final class Clean {\n  private Clean() {}\n  public static int sizeOf(String s) { return s.length(); }\n}\n' \
		> $(CURDIR)/build/java-checker-plant/src/checkerplant/Clean.java
	@printf 'package checkerplant;\npublic final class Planted {\n  private Planted() {}\n  public static int sizeOf() { String s = null; return s.length(); }\n}\n' \
		> $(CURDIR)/build/java-checker-plant/src/checkerplant/Planted.java
	@CP=$$(ls $(JAVA_CHECKER_JARS)/*.jar | tr '\n' ':'); \
	$(JAVAC) $(JAVA_CHECKER_JVM_ARGS) -cp "$$CP" -processorpath "$$CP" \
		-XDcompilePolicy=simple -XDshould-stop.ifError=FLOW \
		-Xplugin:"ErrorProne -Werror -Xep:NullAway:ERROR -XepOpt:NullAway:AnnotatedPackages=checkerplant" \
		-d $(CURDIR)/build/java-checker-plant/clean-out \
		$(CURDIR)/build/java-checker-plant/src/checkerplant/Clean.java > $(CURDIR)/build/java-checker-plant/clean.log 2>&1
	@if grep -q 'error:' $(CURDIR)/build/java-checker-plant/clean.log; then \
		echo "NEGATIVE CONTROL FAILED: the clean file was red"; cat $(CURDIR)/build/java-checker-plant/clean.log; exit 1; fi
	@if CP=$$(ls $(JAVA_CHECKER_JARS)/*.jar | tr '\n' ':'); \
	$(JAVAC) $(JAVA_CHECKER_JVM_ARGS) -cp "$$CP" -processorpath "$$CP" \
		-XDcompilePolicy=simple -XDshould-stop.ifError=FLOW \
		-Xplugin:"ErrorProne -Werror -Xep:NullAway:ERROR -XepOpt:NullAway:AnnotatedPackages=checkerplant" \
		-d $(CURDIR)/build/java-checker-plant/planted-out \
		$(CURDIR)/build/java-checker-plant/src/checkerplant/Planted.java > $(CURDIR)/build/java-checker-plant/planted.log 2>&1; then \
		echo "NEGATIVE CONTROL FAILED: the planted null dereference stayed green"; \
		cat $(CURDIR)/build/java-checker-plant/planted.log; exit 1; fi
	@grep -Fq '[NullAway]' $(CURDIR)/build/java-checker-plant/planted.log || { \
		echo "NEGATIVE CONTROL FAILED: the plant went red, but not on NullAway"; \
		cat $(CURDIR)/build/java-checker-plant/planted.log; exit 1; }
	@grep -m1 'error:' $(CURDIR)/build/java-checker-plant/planted.log
	@echo "negative control: a planted null dereference is RED on NullAway; the clean file is GREEN"
