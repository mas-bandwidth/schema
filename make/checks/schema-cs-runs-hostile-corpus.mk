# make/checks/schema-cs-runs-hostile-corpus.mk — the C# leg of the shared
# hostile corpus (docs/HOSTILE-CORPUS.md). Every raw input is decoded by the
# generated C# reader and compared with the manifest's verdict, named reason and
# decoded value digest. The corpus and manifest are committed data under
# internal/ci/hostilecorpus (schema-shared-hostile-corpus); this file only
# builds and runs the reader.
#
# It lives under make/checks, not make/, because the wide-scalar registry reads
# every make/*.mk as a compiler backend and runs `schema generate --lang <name>`
# for it. This is a gate, not a backend.

HOSTILE_CS_CORPUS ?= internal/ci/hostilecorpus/corpus
HOSTILE_CS_PROBE_SCHEMA := build/hostile-cs/probe.schema
HOSTILE_CS_PROBE := build/tables-generated-cs/probe
HOSTILE_CS_CARDS := build/tables-generated-cs/cards
HOSTILE_CS_DRIVER := $(CURDIR)/test/conformance/cs/bin/Debug/net10.0/schemaconformance.dll

.PHONY: schema-cs-runs-hostile-corpus schema-cs-runs-hostile-corpus-negative-control

# The probe schema is one Go constant, never a hand copy: the corpus generator
# writes the same text from internal/ci/hostilecorpus.ProbeSchema.
$(HOSTILE_CS_PROBE_SCHEMA): internal/ci/hostilecorpus/corpus.go
	@mkdir -p $(dir $@)
	go run ./test/conformance/cs/writeprobe -out $@

$(HOSTILE_CS_PROBE)/.stamp: bin/schema $(HOSTILE_CS_PROBE_SCHEMA)
	@mkdir -p $(HOSTILE_CS_PROBE)
	./bin/schema generate --lang cs --out $(HOSTILE_CS_PROBE) $(HOSTILE_CS_PROBE_SCHEMA)
	@touch $@

# The driver compiles the cards unit beside the corpus units; its own stamp
# keeps a second gate run from rewriting sources and rebuilding the assembly.
$(HOSTILE_CS_CARDS)/.stamp: bin/schema tables/cards
	./bin/schema generate --lang cs --out $(HOSTILE_CS_CARDS) tables/cards
	@touch $@

schema-cs-runs-hostile-corpus: build/tables-generated-cs/.stamp $(HOSTILE_CS_PROBE)/.stamp $(HOSTILE_CS_CARDS)/.stamp
	cd test/conformance/cs && $(DOTNET) build -v q --nologo
	$(DOTNET) $(HOSTILE_CS_DRIVER) corpus $(HOSTILE_CS_CORPUS)

# THE PLANTED-BAD-PATTERN CONTROL: the runner is rebuilt with a test-only
# reader bug — a decoded value corrupted for the surrogate_pair case — and the
# same corpus must go red on that case by name; without the env switch the
# unplanted corpus must be green. No manifest or fixture is changed.
schema-cs-runs-hostile-corpus-negative-control: schema-cs-runs-hostile-corpus
	SCHEMA_CS_HOSTILE_RUNNER=$(HOSTILE_CS_DRIVER) go test -count=1 -timeout 600s ./internal/ci -run '^TestSchemaCSRunsHostileCorpusNegativeControl$$'

test-cs: schema-cs-runs-hostile-corpus
