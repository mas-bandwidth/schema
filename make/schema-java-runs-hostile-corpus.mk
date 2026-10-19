# Every shared hostile input through the generated Java table reader. Absence
# of the independently landed corpus is a loud dependency failure; this target
# never substitutes or silently narrows its manifest.
.PHONY: schema-java-runs-hostile-corpus
schema-java-runs-hostile-corpus: bin/schema toolchain-java
	@rm -rf build/java-hostile-corpus && mkdir -p build/java-hostile-corpus/generated build/java-hostile-corpus/classes build/java-hostile-corpus/probe
	@printf '%s\n' 'package probe' '' 'table Root' '{' '    entries map[int32]int32' '}' > build/java-hostile-corpus/probe/probe.schema
	@for schema in tables/blobs tables/examples tables/messages tables/pointers tables/scalars tables/stream build/java-hostile-corpus/probe/probe.schema; do \
		name=$$(basename "$$schema" .schema); \
		./bin/schema generate --lang java --out "build/java-hostile-corpus/generated/$$name" "$$schema"; \
	done
	$(JAVAC) --release 17 -Xlint:all -Werror -d build/java-hostile-corpus/classes \
		$$(find build/java-hostile-corpus/generated -name '*.java' -print) \
		test/conformance/java/src/HostileCorpus.java
	$(JAVA) -ea -cp build/java-hostile-corpus/classes HostileCorpus \
		$(CURDIR) $(CURDIR)/internal/ci/hostilecorpus/corpus

.PHONY: schema-java-runs-hostile-corpus-negative-control
schema-java-runs-hostile-corpus-negative-control: schema-java-runs-hostile-corpus
	@rm -rf build/java-hostile-corpus/negative && cp -R internal/ci/hostilecorpus/corpus build/java-hostile-corpus/negative
	@python3 -c 'from pathlib import Path; p=Path("build/java-hostile-corpus/negative/manifest.tsv"); s=p.read_text(); old="5b245b9db93e60dd2f95abcd25c582e677281c775f5b16e4acb9ce10bcb85fd9"; assert s.count(old)==1; p.write_text(s.replace(old,"0"*64))'
	@if $(JAVA) -ea -cp build/java-hostile-corpus/classes HostileCorpus \
		$(CURDIR) $(CURDIR)/build/java-hostile-corpus/negative > build/java-hostile-corpus/negative.log 2>&1; then \
		echo 'NEGATIVE CONTROL FAILED: a planted accepted-value digest passed'; exit 1; \
	fi
	@grep -F 'FAIL conformance-arm-pointer-null: digest' build/java-hostile-corpus/negative.log
	@echo 'Java hostile-corpus negative control: a planted accepted-value digest turns the runner red'

# The corpus is part of the Java port's ordinary unit tier.
test-java: schema-java-runs-hostile-corpus
