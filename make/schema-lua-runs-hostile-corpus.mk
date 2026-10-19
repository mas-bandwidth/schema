# Run every shared hostile-corpus input through the generated Lua table reader.
.PHONY: schema-lua-runs-hostile-corpus
schema-lua-runs-hostile-corpus:
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/ci/ -count=1 -timeout 600s \
		-run '^TestSchemaLua(RunsHostileCorpus|HostileCorpusNegativeControl)$$'
	@echo 'lua hostile corpus: every verdict, named reason and decoded-value digest agrees'

.PHONY: schema-lua-runs-hostile-corpus-negative-control
schema-lua-runs-hostile-corpus-negative-control:
	SCHEMA_REQUIRE_CORPUS=1 go test ./internal/ci/ -count=1 -timeout 600s \
		-run '^TestSchemaLuaHostileCorpusNegativeControl$$'

# The corpus reader is part of the Lua port's normal unit tier.
test-lua: schema-lua-runs-hostile-corpus
