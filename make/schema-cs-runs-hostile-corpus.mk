# make/schema-cs-runs-hostile-corpus.mk — the C# leg of the shared hostile
# corpus (docs/HOSTILE-CORPUS.md). Every raw input is decoded by the generated
# C# reader and compared with the manifest's verdict, named reason and decoded
# value digest. The corpus is data; this file only builds and runs the reader.

HOSTILE_CS_CORPUS ?= internal/ci/hostilecorpus/corpus
HOSTILE_CS_PROBE := build/tables-generated-cs/probe
HOSTILE_CS_DRIVER := $(CURDIR)/test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll

.PHONY: schema-cs-runs-hostile-corpus schema-cs-runs-hostile-corpus-negative-control

$(HOSTILE_CS_PROBE)/.stamp: bin/schema internal/ci/hostilecorpus/probe.schema
	@mkdir -p $(HOSTILE_CS_PROBE)
	./bin/schema generate --lang cs --out $(HOSTILE_CS_PROBE) internal/ci/hostilecorpus/probe.schema
	@touch $@

schema-cs-runs-hostile-corpus: build/tables-generated-cs/.stamp $(HOSTILE_CS_PROBE)/.stamp cards-cs-generate
	cd test/conformance/cs && $(DOTNET) build -v q --nologo
	$(DOTNET) $(HOSTILE_CS_DRIVER) corpus $(HOSTILE_CS_CORPUS)

schema-cs-runs-hostile-corpus-negative-control: schema-cs-runs-hostile-corpus
	SCHEMA_CS_HOSTILE_RUNNER=$(HOSTILE_CS_DRIVER) $(GO) test -count=1 -timeout 600s ./internal/ci -run '^TestSchemaCSRunsHostileCorpusNegativeControl$$'

test-cs: schema-cs-runs-hostile-corpus
